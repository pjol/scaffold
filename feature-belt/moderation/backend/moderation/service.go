// Package moderation is the safety layer every user-generated-content
// feature requires: reports on any registered content kind or a person,
// blocks in both directions, a pre-post content filter (Check), screening
// with rules and an optional decision model (Screen, Record), strikes, and
// the staff queue that works reports and held items. Other features use
// contract.go, screen.go's Screen/Record and strikes.go's Strikes,
// CheckPosting and RestrictedUntil.
package moderation

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/legal"
	"scaffoldapp/backend/internal/notify"
)

// Notification kinds this module sends.
const (
	KindOutcome = "moderation.outcome" // to reporters: their report was reviewed
	KindNotice  = "moderation.notice"  // to authors: warning, removal, suspension
)

// Realtime events.
const (
	EventCaseOpened    = "moderation.case_opened"    // admin:live, a new case entered the queue
	EventBlocksChanged = "moderation.blocks_changed" // user:<blocker>, refetch the block list
)

const (
	reportsPerHour = 20
	blocksPerHour  = 60
	maxNoteChars   = 1000
)

type Service struct {
	d   *app.Deps
	cfg settings
}

type settings struct {
	autoHideReports int // distinct reporters that hide content; 0 = off
	notifyReporter  bool
}

func New(d *app.Deps) *Service {
	s := &Service{d: d, cfg: settings{
		autoHideReports: max(config.Int("MODERATION_AUTOHIDE_REPORTS", 0), 0),
		notifyReporter:  config.Bool("MODERATION_NOTIFY_REPORTER", true),
	}}

	var provider Provider
	switch p := strings.ToLower(config.String("MODERATION_PROVIDER", "none")); p {
	case "none":
	case "openai":
		provider = &OpenAI{APIKey: config.Required("OPENAI_API_KEY"), Client: &http.Client{Timeout: 6 * time.Second}}
	default:
		config.Problem("MODERATION_PROVIDER must be none or openai (got %q)", p)
	}
	terms := config.List("MODERATION_BLOCKED_TERMS", "")
	checker.Store(newFilter(terms, provider))

	// Screening (Screen): rules, the optional classifier above, the optional
	// decision model, the strike ladder.
	brandDomains := config.List("MODERATION_BRAND_DOMAINS", "")
	if u, err := url.Parse(d.Cfg.PublicWebURL); err == nil && u.Hostname() != "" && !config.IsLocalURL(d.Cfg.PublicWebURL) {
		brandDomains = append(brandDomains, u.Hostname())
	}
	ladder, err := parseLadder(config.String("MODERATION_STRIKE_LIMITS", "3:24h,5:168h"))
	if err != nil {
		config.Problem("MODERATION_STRIKE_LIMITS: %v", err)
		ladder = defaultLadder
	}
	timeout := config.Duration("MODERATION_DECISION_TIMEOUT", 3*time.Second)
	if timeout <= 0 || timeout > 30*time.Second {
		config.Problem("MODERATION_DECISION_TIMEOUT must be between 1ms and 30s (got %s)", timeout)
		timeout = 3 * time.Second
	}
	screening.Store(&screenConfig{
		rules:        NewRules(terms, config.List("MODERATION_LINK_DENYLIST", ""), NewBrands(brandDomains...), appName),
		provider:     provider,
		thresholds:   DefaultThresholds,
		ladder:       ladder,
		modelTimeout: timeout,
		broker:       d.Broker,
	})
	if dm := newDeciderFromEnv(timeout); dm != nil {
		UseDecider(dm)
	}

	// Safety tools work for everyone signed in, including people who haven't
	// accepted updated terms yet: blocking a harasser can't wait.
	legal.Exempt("/v1/moderation/reports", "/v1/moderation/blocks")
	return s
}

func (s *Service) Name() string { return "moderation" }

func (s *Service) MountUser(r chi.Router) {
	r.With(auth.RateLimit(reportsPerHour, time.Hour)).Post("/moderation/reports", httpx.Handler(s.createReport))
	r.Get("/moderation/reports", httpx.Handler(s.listMyReports))
	r.Get("/moderation/blocks", httpx.Handler(s.listBlocks))
	r.Get("/moderation/blocks/ids", httpx.Handler(s.blockedIDs))
	r.With(auth.RateLimit(blocksPerHour, time.Hour)).Post("/moderation/blocks", httpx.Handler(s.block))
	r.Delete("/moderation/blocks/{user_id}", httpx.Handler(s.unblock))
	r.Get("/moderation/content", httpx.Handler(s.myContent))
	r.Get("/moderation/standing", httpx.Handler(s.standing))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/moderation/summary", httpx.Handler(s.summary))
	r.Get("/moderation/cases", httpx.Handler(s.listCases))
	r.Get("/moderation/cases/{id}", httpx.Handler(s.getCase))
	r.Post("/moderation/cases/{id}/actions", httpx.Handler(s.actOnCase))
	r.With(auth.RequireAdminRole("admin")).Post("/moderation/decision-model/reset", httpx.Handler(s.resetDecisionModel))
}

// newDeciderFromEnv builds the HTTP decision model from MODERATION_DECISION_*,
// or nil when MODERATION_DECISION_URL is unset (rules only).
func newDeciderFromEnv(timeout time.Duration) Decider {
	endpoint := strings.TrimSpace(config.String("MODERATION_DECISION_URL", ""))
	key := config.String("MODERATION_DECISION_API_KEY", "")
	model := config.String("MODERATION_DECISION_MODEL", "")
	rps := config.Int("MODERATION_DECISION_MAX_RPS", 5)
	capUSD, err := decimal.NewFromString(strings.TrimSpace(config.String("MODERATION_DECISION_DAILY_USD", "5")))
	if err != nil || capUSD.IsNegative() {
		config.Problem("MODERATION_DECISION_DAILY_USD must be a number of dollars, 0 or more (0 = no cap)")
		capUSD = decimal.NewFromInt(5)
	}
	if rps <= 0 {
		config.Problem("MODERATION_DECISION_MAX_RPS must be a positive number (got %d)", rps)
		rps = 5
	}
	if endpoint == "" {
		return nil
	}
	if u, err := url.Parse(endpoint); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		config.Problem("MODERATION_DECISION_URL must be an http(s) URL (got %q)", endpoint)
		return nil
	}
	if key == "" {
		config.Problem("MODERATION_DECISION_API_KEY is required when MODERATION_DECISION_URL is set")
		return nil
	}
	return newHTTPDecider(endpoint, key, model, rps, timeout, capUSD)
}

// send delivers a notification best-effort.
func (s *Service) send(ctx context.Context, userID uuid.UUID, kind, title, body, link string, data map[string]any) {
	err := s.d.Notify.Send(ctx, notify.Notification{UserID: userID, Kind: kind, Title: title, Body: body, Link: link, Data: data})
	if err != nil {
		slog.WarnContext(ctx, "moderation: notify failed", "kind", kind, "user_id", userID, "err", err)
	}
}

func (s *Service) publish(ctx context.Context, topic, event string, data any) {
	if err := s.d.Broker.Publish(ctx, topic, event, data); err != nil {
		slog.WarnContext(ctx, "moderation: realtime publish failed", "event", event, "err", err)
	}
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
