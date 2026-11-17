// Package reputation gives each person a transparent, risk-adjusted 0–100
// score from a series the app supplies (account value, money in and out,
// activity), with an explainer that shows the exact numbers the score is
// made of (rep-v1, score.go).
//
// The series comes from:
//   - any installed module implementing Source (found in Deps.Modules, so
//     the two never import each other): sampled every
//     REPUTATION_SNAPSHOT_INTERVAL and rolled up into day rows;
//   - RecordDay, for apps that push day rows themselves.
//
// Returns are time-weighted: (close − open − flows) / max(open, floor) per
// day, so a deposit never offsets a loss. Integrity flags (wash pairs, vote
// rings, or any module's Flag) put a person under review until staff
// decide. Other modules read a person's standing with Get.
package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
)

// Sample is one person's cumulative counters at the moment a Source is read.
type Sample struct {
	UserID uuid.UUID
	// Value is what the person holds now (an account value).
	Value decimal.Decimal
	// NetFlows is all money the person ever put in minus taken out (deposits
	// − withdrawals, test credits included). Never returns.
	NetFlows decimal.Decimal
	// Activity counts scored actions so far (trades, fills); Volume is their
	// total size; Incidents counts bad outcomes (liquidations).
	Activity  int64
	Volume    decimal.Decimal
	Incidents int64
	// FirstActivity is when the person first acted; zero when unknown.
	FirstActivity time.Time
	// Exposed: the person holds open risk now (an open position), so the day
	// counts as active even without new activity.
	Exposed bool
}

// Source is a module that supplies the series. The reputation finds it among
// the installed modules; implementing it needs no import of this package
// beyond the Sample type. Return every person the app scores. With several
// sources, a person's samples are summed.
type Source interface {
	ReputationSamples(ctx context.Context) ([]Sample, error)
}

// CommunitySignals returns one person's community standing since a time.
// Plug one in with SetCommunitySignals; without it the community component
// is unscored and its weight goes to the others.
type CommunitySignals func(ctx context.Context, q db.Querier, userID uuid.UUID, since time.Time) (Community, error)

var (
	seamMu     sync.RWMutex
	communityF CommunitySignals
	pairsF     PairSource
	votesF     VoteSource
)

// SetCommunitySignals plugs in the community component (call from an init()
// in stitching code).
func SetCommunitySignals(f CommunitySignals) { seamMu.Lock(); communityF = f; seamMu.Unlock() }

// SetPairSource plugs in the wash-pair detector's pair statistics.
func SetPairSource(f PairSource) { seamMu.Lock(); pairsF = f; seamMu.Unlock() }

// SetVoteSource plugs in the vote-ring detector's upvotes.
func SetVoteSource(f VoteSource) { seamMu.Lock(); votesF = f; seamMu.Unlock() }

func seams() (CommunitySignals, PairSource, VoteSource) {
	seamMu.RLock()
	defer seamMu.RUnlock()
	return communityF, pairsF, votesF
}

// Summary is a person's standing as other modules read it.
type Summary struct {
	Score  *int   `json:"score"`  // nil unless rated
	Status string `json:"status"` // rated | unrated | under_review
	Tier   string `json:"tier"`   // "" unless rated
}

// Get returns a person's stored standing (unrated when they have none). It
// reads one row, so it is cheap enough for list rendering and eligibility
// checks inside a transaction.
func Get(ctx context.Context, q db.Querier, userID uuid.UUID) (Summary, error) {
	var s Summary
	var tier *string
	err := q.QueryRow(ctx, `SELECT status, score, tier FROM reputation_scores WHERE user_id = $1`, userID).Scan(&s.Status, &s.Score, &tier)
	if db.IsNoRows(err) {
		return Summary{Status: StatusUnrated}, nil
	}
	if err != nil {
		return Summary{}, err
	}
	if s.Status != StatusRated || s.Score == nil {
		s.Score = nil
		return s, nil
	}
	if tier != nil {
		s.Tier = *tier
	}
	return s, nil
}

// Flag raises an integrity flag and puts the person under review at once
// (in q, so it commits with the caller's transaction). Raising a flag that
// is already open for the same person, kind and counterparty does nothing.
func Flag(ctx context.Context, q db.Querier, f IntegrityFlag) error {
	if f.UserID == uuid.Nil {
		return errors.New("reputation: flag without a person")
	}
	if !validKind(f.Kind) {
		return fmt.Errorf("reputation: flag kind %q must be lowercase snake_case, 40 characters at most", f.Kind)
	}
	if _, err := insertFlag(ctx, q, f); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO reputation_scores (user_id, status) VALUES ($1, 'under_review')
		ON CONFLICT (user_id) DO UPDATE SET status = 'under_review', score = NULL, tier = NULL`, f.UserID)
	return err
}

// insertFlag stores one open flag unless the same one is open; it reports
// whether a row was added.
func insertFlag(ctx context.Context, q db.Querier, f IntegrityFlag) (bool, error) {
	m := f.Metrics
	if m == nil {
		m = map[string]string{}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	var cp *uuid.UUID
	if f.CounterpartyID != uuid.Nil {
		cp = &f.CounterpartyID
	}
	tag, err := q.Exec(ctx, `INSERT INTO reputation_flags (user_id, kind, counterparty_id, metrics) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, kind, COALESCE(counterparty_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE status = 'open'
		DO NOTHING`, f.UserID, f.Kind, cp, b)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RecordDay stores one day of a person's series that the app computed
// itself (Day.Ret and Day.Source are ignored). The rollup never overwrites
// it; the person is rescored within one snapshot interval.
func RecordDay(ctx context.Context, q db.Querier, userID uuid.UUID, d Day) error {
	if userID == uuid.Nil {
		return errors.New("reputation: day without a person")
	}
	if d.Activity < 0 || d.Incidents < 0 || d.Volume.IsNegative() {
		return errors.New("reputation: activity, volume and incidents can't be negative")
	}
	day := dayStart(d.Day)
	active := d.Active || d.Activity > 0
	if _, err := q.Exec(ctx, `INSERT INTO reputation_days (user_id, day, open_value, close_value, flows, active, activity, volume, incidents, source)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pushed')
		ON CONFLICT (user_id, day) DO UPDATE SET open_value = EXCLUDED.open_value, close_value = EXCLUDED.close_value,
		  flows = EXCLUDED.flows, active = EXCLUDED.active, activity = EXCLUDED.activity, volume = EXCLUDED.volume,
		  incidents = EXCLUDED.incidents, source = 'pushed', updated_at = now()`,
		userID, day, d.Open, d.Close, d.Flows, active, d.Activity, d.Volume, d.Incidents); err != nil {
		return err
	}
	if !active {
		return nil
	}
	_, err := q.Exec(ctx, `INSERT INTO reputation_scores (user_id, first_activity_at) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET first_activity_at = LEAST(COALESCE(reputation_scores.first_activity_at, EXCLUDED.first_activity_at), EXCLUDED.first_activity_at)`,
		userID, day)
	return err
}

// Service is the module: snapshot and daily jobs, routes, tools, seeds.
type Service struct {
	d        *app.Deps
	model    Model
	interval time.Duration
	board    bool
	log      *slog.Logger
}

func decimalEnv(name string, def decimal.Decimal, ok func(decimal.Decimal) bool, rule string) decimal.Decimal {
	raw := config.String(name, "")
	if raw == "" {
		return def
	}
	v, err := decimal.NewFromString(raw)
	if err != nil || !ok(v) {
		config.Problem("%s must be %s (got %q)", name, rule, raw)
		return def
	}
	return v
}

func New(d *app.Deps) *Service {
	m := DefaultModel.clone()
	m.MinVolume = decimalEnv("REPUTATION_MIN_VOLUME", m.MinVolume, func(v decimal.Decimal) bool { return !v.IsNegative() }, "a number 0 or more")
	m.ReturnFloor = decimalEnv("REPUTATION_RETURN_FLOOR", m.ReturnFloor, func(v decimal.Decimal) bool { return v.IsPositive() }, "a number above 0")
	interval := config.Duration("REPUTATION_SNAPSHOT_INTERVAL", 15*time.Minute)
	if interval < time.Minute || interval > 24*time.Hour {
		config.Problem("REPUTATION_SNAPSHOT_INTERVAL must be between 1m and 24h (got %s)", interval)
		interval = 15 * time.Minute
	}
	if len(m.Weights) == 0 || len(m.Tiers) == 0 {
		config.Problem("reputation: DefaultModel needs weights and tiers")
	}
	return &Service{d: d, model: m, interval: interval, board: config.Bool("REPUTATION_LEADERBOARD", true),
		log: slog.Default().With("module", "reputation")}
}

func (s *Service) Name() string { return "reputation" }

func (s *Service) MountUser(r chi.Router) {
	r.Get("/reputation/me", httpx.Handler(s.meHandler))
	r.Get("/reputation/people", httpx.Handler(s.peopleHandler))
	r.Get("/reputation/people/{id}", httpx.Handler(s.personHandler))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/reputation/flags", httpx.Handler(s.flagsHandler))
	r.Post("/reputation/flags/{id}/decision", httpx.Handler(s.decideHandler))
	r.Get("/reputation/people/{id}", httpx.Handler(s.adminPersonHandler))
}

// Start runs the jobs, each once across instances: snapshots, rollup and
// rescoring of changed people every REPUTATION_SNAPSHOT_INTERVAL; once a day
// a full rescore (the window and ages move), the integrity detectors and
// pruning.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "reputation.snapshot", s.interval, s.tick)
	go jobs.Every(ctx, s.d.Pool, "reputation.daily", 24*time.Hour, s.daily)
}

// LeaderboardBoard and LeaderboardScores make the reputation a source for
// the leaderboard feature when it is installed (it finds them among the
// modules): a "Reputation" board of rated people. REPUTATION_LEADERBOARD=false
// leaves it out.
func (s *Service) LeaderboardBoard() (id, title, unit string) {
	if !s.board {
		return "", "", ""
	}
	return "reputation", "Reputation", ""
}

func (s *Service) LeaderboardScores(ctx context.Context) (map[uuid.UUID]decimal.Decimal, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id, score FROM reputation_scores WHERE status = 'rated' AND score IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]decimal.Decimal{}
	for rows.Next() {
		var id uuid.UUID
		var score int
		if err := rows.Scan(&id, &score); err != nil {
			return nil, err
		}
		out[id] = decimal.NewFromInt(int64(score))
	}
	return out, rows.Err()
}
