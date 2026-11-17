// Package copytrade is copy trading on the exchange feature. Leaders opt in;
// a follower starts a copy with an allocation that moves into a sub-account
// of theirs on the exchange, and the copy mirrors the leader's TARGET
// POSITION, leader size × copy equity ÷ leader equity, not individual fills,
// so partial fills, coalesced fills, missed events and restarts all converge.
//
// Loops are impossible by construction: only main accounts lead, and copies
// trade in sub-accounts, whose fills never reach the leader pipeline.
//
// Pipeline (on the instance that runs the exchange engine, exchange.OnEngine):
// the exchange fill hook → a dispatcher that coalesces each (leader, market)
// for 250 ms, reads the leader's position and equity once and shards that
// leader's copies over workers → each worker sizes and places one market
// order per copy (Source "copy", ClientRef = the copy event id, AvoidMakers =
// the leader). reconcile (60 s) repairs drift; monitor (15 s) runs the stops;
// settlement pays the leader's profit share above a high-water mark.
package copytrade

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// Realtime events on the follower's own user:<id> topic.
const (
	EventCopyFrame = "copytrade.copy"  // Copy, at most once a second per copy
	EventActivity  = "copytrade.event" // Event, as it is recorded
)

// Notification kinds (not registered with the notifications feature, which
// copytrade doesn't require: they deliver with its defaults when installed).
const (
	KindCopyStarted   = "copytrade.started"
	KindCopyStopped   = "copytrade.stopped"
	KindOrdersFailing = "copytrade.orders_failing"
	KindCopierJoined  = "copytrade.copier_joined"
	KindCopierLeft    = "copytrade.copier_left"
	KindProfitShare   = "copytrade.profit_share"
)

// ---- leader eligibility seam ----

// EligibilityFunc decides whether a person may lead, with the rules shown to
// them. Hard failures (an error) are logged and treated as not eligible.
type EligibilityFunc func(ctx context.Context, userID uuid.UUID) (ok bool, reqs []Requirement, err error)

var (
	eligMu      sync.RWMutex
	eligibility EligibilityFunc
	// DefaultEligibility's thresholds, from COPYTRADE_LEADER_MIN_AGE_DAYS and
	// COPYTRADE_LEADER_MIN_FILLS (set by New).
	minAgeDays = 30
	minFills   = 1
)

// SetEligibility replaces the leader rules (call from stitching code, e.g. to
// require a reputation; FEATURE.md → Stitching). nil restores the default.
func SetEligibility(f EligibilityFunc) {
	eligMu.Lock()
	eligibility = f
	eligMu.Unlock()
}

// DefaultEligibility: an active account at least COPYTRADE_LEADER_MIN_AGE_DAYS
// old with at least COPYTRADE_LEADER_MIN_FILLS fills on its main account.
func DefaultEligibility(ctx context.Context, q db.Querier, userID uuid.UUID) (bool, []Requirement, error) {
	var status string
	var created time.Time
	if err := q.QueryRow(ctx, `SELECT status, created_at FROM users WHERE id = $1`, userID).Scan(&status, &created); err != nil {
		return false, nil, err
	}
	act, err := exchange.ActivityOf(ctx, q, userID)
	if err != nil {
		return false, nil, err
	}
	age := int(time.Since(created).Hours() / 24)
	reqs := []Requirement{{Key: "active", Label: "Account in good standing", Met: status == "active", Progress: status}}
	if minAgeDays > 0 {
		reqs = append(reqs, Requirement{Key: "account_age", Label: plural(minAgeDays, "Account at least %d day old", "Account at least %d days old"),
			Met: age >= minAgeDays, Progress: progress(min(age, minAgeDays), minAgeDays, "days")})
	}
	if minFills > 0 {
		reqs = append(reqs, Requirement{Key: "traded", Label: plural(minFills, "At least %d trade", "At least %d trades"),
			Met: act.Fills >= minFills, Progress: progress(min(act.Fills, minFills), minFills, "trades")})
	}
	ok := true
	for _, r := range reqs {
		ok = ok && r.Met
	}
	return ok, reqs, nil
}

func (s *Service) eligible(ctx context.Context, userID uuid.UUID) (bool, []Requirement, error) {
	eligMu.RLock()
	f := eligibility
	eligMu.RUnlock()
	if f == nil {
		return DefaultEligibility(ctx, s.d.Pool, userID)
	}
	ok, reqs, err := f(ctx, userID)
	if reqs == nil {
		reqs = []Requirement{}
	}
	return ok, reqs, err
}

// ---- service ----

// Service is the copytrade module.
type Service struct {
	d *app.Deps

	paused               bool
	maxCopiersPerLeader  int
	maxCopiesPerFollower int
	minAllocation        decimal.Decimal
	maxProfitShare       decimal.Decimal
	settleEvery          time.Duration
	eventRetention       time.Duration
	workers              int
	coalesce             time.Duration

	rt atomic.Pointer[runtime] // set while this instance runs the exchange engine

	lcMu        sync.Mutex
	leaderCache map[string]leaderCacheEntry
}

func New(d *app.Deps) *Service {
	s := &Service{d: d,
		paused:               config.Bool("COPYTRADE_PAUSED", false),
		maxCopiersPerLeader:  intEnv("COPYTRADE_MAX_COPIERS_PER_LEADER", 500, 1, 10_000),
		maxCopiesPerFollower: intEnv("COPYTRADE_MAX_COPIES_PER_FOLLOWER", 10, 1, 100),
		minAllocation:        decimalEnv("COPYTRADE_MIN_ALLOCATION", "50", decimal.NewFromInt(1), decimal.NewFromInt(1_000_000)),
		maxProfitShare:       decimalEnv("COPYTRADE_MAX_PROFIT_SHARE_PCT", "20", decimal.Zero, decimal.NewFromInt(50)),
		settleEvery:          durationEnv("COPYTRADE_SETTLE_INTERVAL", 24*time.Hour, time.Hour, 90*24*time.Hour),
		eventRetention:       durationEnv("COPYTRADE_EVENT_RETENTION", 90*24*time.Hour, 24*time.Hour, 10*365*24*time.Hour),
		workers:              intEnv("COPYTRADE_WORKERS", 8, 1, 64),
		coalesce:             250 * time.Millisecond,
		leaderCache:          map[string]leaderCacheEntry{},
	}
	minAgeDays = intEnv("COPYTRADE_LEADER_MIN_AGE_DAYS", 30, 0, 3650)
	minFills = intEnv("COPYTRADE_LEADER_MIN_FILLS", 1, 0, 1_000_000)
	return s
}

func intEnv(key string, def, lo, hi int) int {
	v := config.Int(key, def)
	if v < lo || v > hi {
		config.Problem("%s must be %d–%d (got %d)", key, lo, hi, v)
		return def
	}
	return v
}

func decimalEnv(key, def string, lo, hi decimal.Decimal) decimal.Decimal {
	raw := config.String(key, def)
	v, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || v.LessThan(lo) || v.GreaterThan(hi) {
		config.Problem("%s must be a decimal between %s and %s (got %q)", key, lo, hi, raw)
		return decimal.RequireFromString(def)
	}
	return v
}

func durationEnv(key string, def, lo, hi time.Duration) time.Duration {
	v := config.Duration(key, def)
	if v < lo || v > hi {
		config.Problem("%s must be between %s and %s (got %s)", key, lo, hi, v)
		return def
	}
	return v
}

func (s *Service) Name() string { return "copytrade" }

func (s *Service) exchange() *exchange.Service { return exchange.Find(s.d.Modules) }

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/copytrade/config", httpx.Handler(s.configHandler))
}

func (s *Service) MountUser(r chi.Router) {
	write := r.With(auth.RequireActive)
	r.Get("/copytrade/leaders", httpx.Handler(s.leadersHandler))
	r.Get("/copytrade/leaders/{user_id}", httpx.Handler(s.leaderHandler))
	r.Get("/copytrade/leader", httpx.Handler(s.leaderStatusHandler))
	write.With(auth.RateLimit(30, time.Hour)).Put("/copytrade/leader", httpx.Handler(s.putLeaderHandler))
	r.Get("/copytrade/copiers", httpx.Handler(s.copiersHandler))
	r.Get("/copytrade/copies", httpx.Handler(s.copiesHandler))
	write.With(auth.RateLimit(10, time.Hour)).Post("/copytrade/copies", httpx.Handler(s.startHandler))
	r.Get("/copytrade/copies/{id}", httpx.Handler(s.copyHandler))
	write.With(auth.RateLimit(60, time.Hour)).Patch("/copytrade/copies/{id}", httpx.Handler(s.patchHandler))
	write.With(auth.RateLimit(10, time.Hour)).Post("/copytrade/copies/{id}/funds", httpx.Handler(s.fundsHandler))
	write.With(auth.RateLimit(30, time.Hour)).Post("/copytrade/copies/{id}/stop", httpx.Handler(s.stopHandler))
	write.With(auth.RateLimit(120, time.Minute)).Post("/copytrade/copies/{id}/positions/{symbol}/close", httpx.Handler(s.closeHandler))
	r.Get("/copytrade/copies/{id}/events", httpx.Handler(s.eventsHandler))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/copytrade/overview", httpx.Handler(s.adminOverview))
	r.Get("/copytrade/copies", httpx.Handler(s.adminCopies))
	r.With(auth.RequireActive, auth.RequireAdminRole("admin")).Post("/copytrade/copies/{id}/stop", httpx.Handler(s.adminStop))
}

// Start hooks into the exchange: fills go to the running pipeline, and the
// pipeline runs wherever the engine runs. Statistics and eligibility are
// database work, once across instances.
func (s *Service) Start(ctx context.Context) {
	ex := s.exchange()
	if ex == nil {
		slog.ErrorContext(ctx, "copytrade: the exchange module is not installed")
		return
	}
	ex.OnFill(func(f exchange.FillEvent) {
		if rt := s.rt.Load(); rt != nil {
			rt.tap(f)
		}
	})
	ex.OnEngine(func(ectx context.Context) { s.run(ectx, ex) })
	go jobs.Every(ctx, s.d.Pool, "copytrade.leaders", 5*time.Minute, s.refreshLeaders)
	go jobs.Every(ctx, s.d.Pool, "copytrade.snapshots", time.Hour, s.snapshot)
	go jobs.Every(ctx, s.d.Pool, "copytrade.prune", 24*time.Hour, s.prune)
}

// running returns the pipeline, or the exchange's standby error on an
// instance that doesn't run the engine.
func (s *Service) running() (*runtime, error) {
	if rt := s.rt.Load(); rt != nil {
		return rt, nil
	}
	return nil, exchange.ErrStandby
}

// ---- errors ----

func errStatus(status int, code, msg string) error { return httpx.NewError(status, code, msg) }

var (
	errPaused         = errStatus(http.StatusConflict, "copytrade_paused", "Copy trading is paused right now.")
	errSelfCopy       = errStatus(http.StatusUnprocessableEntity, "self_copy", "You can't copy yourself.")
	errNotAccepting   = errStatus(http.StatusConflict, "leader_not_accepting", "This trader isn't accepting copiers.")
	errLeaderFull     = errStatus(http.StatusConflict, "leader_full", "This trader has the most copiers they take.")
	errDisclosure     = errStatus(http.StatusUnprocessableEntity, "disclosure_required", "Accept the copy trading risk disclosure first.")
	errAlreadyCopying = errStatus(http.StatusConflict, "already_copying", "You already copy this trader.")
	errCopyStopped    = errStatus(http.StatusConflict, "copy_stopped", "This copy has stopped.")
	errLeaderPause    = errStatus(http.StatusConflict, "leader_ineligible", "This trader can't lead right now; the copy resumes when they can.")
	errNotFound       = httpx.NotFound("No copy with that id.")
)

// apiErr maps engine errors onto the API error envelope.
func apiErr(err error) error {
	var ee *engine.Error
	if errors.As(err, &ee) {
		code := ee.Code
		if code == "invalid" {
			code = "invalid_request"
		}
		return httpx.NewError(ee.Status, code, ee.Message)
	}
	return err
}

func errCode(err error) (string, string) {
	var ee *engine.Error
	if errors.As(err, &ee) {
		return ee.Code, ee.Message
	}
	var he *httpx.Error
	if errors.As(err, &he) {
		return he.Code, he.Message
	}
	return "error", "Something went wrong."
}

// ---- helpers ----

func (s *Service) notify(ctx context.Context, n notify.Notification) {
	if s.d.Notify == nil || n.UserID == uuid.Nil {
		return
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "copytrade: notification failed", "kind", n.Kind, "err", err)
	}
}

func (s *Service) publish(ctx context.Context, userID uuid.UUID, event string, data any) {
	if err := s.d.Broker.Publish(ctx, realtime.UserTopic(userID), event, data); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "copytrade: publish", "event", event, "err", err)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return strings.Replace(one, "%d", "1", 1)
	}
	return strings.Replace(many, "%d", itoa(n), 1)
}

func progress(have, need int, unit string) string {
	return itoa(have) + " of " + itoa(need) + " " + unit
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }

func decPtr(d decimal.Decimal) *decimal.Decimal { return &d }

// amount formats a quote-currency amount for event text: "250.00 USD".
func (s *Service) amount(d decimal.Decimal) string {
	cur, dp := "USD", int32(2)
	if ex := s.exchange(); ex != nil {
		cur, dp = ex.Quote()
	}
	return d.StringFixed(dp) + " " + cur
}
