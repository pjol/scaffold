// Package assistant is an AI chat assistant: provider-neutral (Anthropic
// Messages API by default, OpenAI Responses API, a scripted mock in
// development), with the app's agent tools scoped to the signed-in user,
// guardrails that treat the model as a guest, streamed replies that survive
// a dropped connection, per-user quotas, and recorded consent before any
// content goes to the provider. Distilled from ChatMTG's harness
// (internal/{llm,agent,chat}).
package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/moderation"
)

// TargetKind is the moderation target for reporting an assistant reply.
const TargetKind = "assistant.message"

type settings struct {
	provider        string // configured name: anthropic | openai | mock
	model           string
	titleModel      string
	effort          string
	allowWrites     bool
	maxToolRounds   int
	contextTokens   int
	dailyMessages   int
	dailyTokens     int
	maxMessageChars int
	turnTimeout     time.Duration
}

// Service is the assistant module.
type Service struct {
	d   *app.Deps
	cfg settings

	provider    Provider // nil when unconfigured
	unavailable string   // why provider is nil (logged at boot)

	turns *turnSet

	regOnce sync.Once
	reg     *registry

	canaryMu sync.Mutex
	canary   *canary
}

// New reads ASSISTANT_* settings and picks the provider.
func New(d *app.Deps) *Service {
	s := &Service{d: d, turns: newTurnSet()}
	s.cfg = settings{
		provider:        strings.ToLower(config.String("ASSISTANT_PROVIDER", "anthropic")),
		effort:          strings.ToLower(config.String("ASSISTANT_EFFORT", "low")),
		allowWrites:     config.Bool("ASSISTANT_ALLOW_WRITES", false),
		maxToolRounds:   max(1, config.Int("ASSISTANT_MAX_TOOL_ROUNDS", 10)),
		contextTokens:   max(8000, config.Int("ASSISTANT_CONTEXT_TOKENS", 150000)),
		dailyMessages:   max(0, config.Int("ASSISTANT_DAILY_MESSAGES", 200)),
		dailyTokens:     max(0, config.Int("ASSISTANT_DAILY_TOKENS", 2000000)),
		maxMessageChars: max(100, config.Int("ASSISTANT_MAX_MESSAGE_CHARS", 8000)),
		turnTimeout:     max(30*time.Second, config.Duration("ASSISTANT_TURN_TIMEOUT", 5*time.Minute)),
	}
	defaultModel, defaultTitle := "", ""
	switch s.cfg.provider {
	case "anthropic":
		defaultModel, defaultTitle = "claude-sonnet-5-5", "claude-haiku-5-5"
		if key := config.String("ANTHROPIC_API_KEY", ""); key != "" {
			s.provider = newAnthropic(key, config.String("ANTHROPIC_BASE_URL", anthropicDefaultBaseURL),
				config.Bool("ASSISTANT_ANTHROPIC_FALLBACK", true))
		} else {
			s.unavailable = "ANTHROPIC_API_KEY is not set"
		}
	case "openai":
		defaultModel, defaultTitle = "gpt-6-sol", "gpt-6-luna"
		if key := config.String("OPENAI_API_KEY", ""); key != "" {
			s.provider = newOpenAI(key, config.String("OPENAI_BASE_URL", openAIDefaultBaseURL))
		} else {
			s.unavailable = "OPENAI_API_KEY is not set"
		}
	case "mock":
		defaultModel, defaultTitle = "mock", "mock"
		if d.Cfg.IsDevelopment() {
			s.provider = mockProvider{}
		} else {
			// The scripted provider would answer real users with canned text.
			s.unavailable = "ASSISTANT_PROVIDER=mock is only allowed when APP_ENV=development"
			config.Problem("%s", s.unavailable)
		}
	default:
		s.unavailable = "ASSISTANT_PROVIDER must be anthropic, openai or mock (got " + s.cfg.provider + ")"
		config.Problem("%s", s.unavailable)
	}
	s.cfg.model = config.String("ASSISTANT_MODEL", defaultModel)
	s.cfg.titleModel = config.String("ASSISTANT_TITLE_MODEL", defaultTitle)
	switch s.cfg.effort {
	case "none", "low", "medium", "high", "xhigh", "max":
	default:
		config.Problem("ASSISTANT_EFFORT must be low, medium, high, xhigh or max (got %q)", s.cfg.effort)
		s.cfg.effort = "low"
	}
	moderation.RegisterTarget(TargetKind, messageTarget{s: s})
	return s
}

func (s *Service) Name() string { return "assistant" }

func (s *Service) MountUser(r chi.Router) {
	r.Get("/assistant/status", httpx.Handler(s.status))
	r.With(auth.RequireActive).Post("/assistant/consent", httpx.Handler(s.acceptConsent))
	r.Delete("/assistant/consent", httpx.Handler(s.withdrawConsent))

	r.Get("/assistant/conversations", httpx.Handler(s.listConversations))
	r.With(auth.RequireActive, auth.RateLimit(30, time.Minute)).Post("/assistant/conversations", httpx.Handler(s.createConversation))
	r.Get("/assistant/conversations/{id}", httpx.Handler(s.getConversation))
	r.With(auth.RequireActive).Patch("/assistant/conversations/{id}", httpx.Handler(s.renameConversation))
	r.Delete("/assistant/conversations/{id}", httpx.Handler(s.deleteConversation))
	r.Post("/assistant/conversations/{id}/stop", httpx.Handler(s.stop))
	r.With(auth.RequireActive, auth.RateLimit(60, time.Minute)).Post("/assistant/conversations/{id}/tool-calls/{call_id}", httpx.Handler(s.decideToolCall))
}

// MountStream holds the routes that answer with server-sent events (same
// guards as user routes, no request timeout). GET lists messages as JSON, or
// resumes a running turn when the client asks for text/event-stream.
func (s *Service) MountStream(r chi.Router) {
	r.Get("/assistant/conversations/{id}/messages", httpx.Handler(s.messagesOrResume))
	r.With(auth.RequireActive, auth.RateLimit(20, time.Minute)).Post("/assistant/conversations/{id}/messages", httpx.Handler(s.sendMessage))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/assistant/usage", httpx.Handler(s.adminUsage))
}

// Start sweeps turns orphaned by a crash and cancels local turns on shutdown.
func (s *Service) Start(ctx context.Context) {
	if s.provider != nil {
		slog.Info("assistant: provider ready", "provider", s.provider.Name(), "model", s.cfg.model,
			"title_model", s.cfg.titleModel, "allow_writes", s.cfg.allowWrites)
	} else {
		slog.Warn("assistant: no provider; message routes return 503", "reason", s.unavailable)
	}
	_ = s.registry() // logs refused tools once at boot
	go jobs.Every(ctx, s.d.Pool, "assistant.sweep_stale", time.Minute, s.sweepStale)
	go func() {
		<-ctx.Done()
		s.turns.cancelAll(errShutdown)
	}()
}

// ---- helpers ---------------------------------------------------------------

var errUnconfigured = httpx.NewError(http.StatusServiceUnavailable, "assistant_unconfigured",
	"The assistant isn't set up on this server.")

func (s *Service) requireProvider() error {
	if s.provider == nil {
		return errUnconfigured
	}
	return nil
}

// userHash is a stable, non-reversible id for the provider (abuse tooling,
// per-user cache keys). Never the raw id or an email.
func userHash(id uuid.UUID) string {
	sum := sha256.Sum256([]byte("scaffoldapp-assistant-user-v1:" + id.String()))
	return hex.EncodeToString(sum[:16])
}

// nextUTCMidnight is when daily quotas reset.
func nextUTCMidnight(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
}

// ---- status, consent, usage ------------------------------------------------

// ConsentState says whether the user has agreed to send content to the
// configured provider (Apple 5.1.2(i)).
type ConsentState struct {
	Required   bool       `json:"required"`
	Provider   string     `json:"provider"`
	AcceptedAt *time.Time `json:"accepted_at"`
}

// UsageToday is the caller's usage against the daily limits. A limit of 0
// means unlimited.
type UsageToday struct {
	Day          string    `json:"day"`
	Messages     int       `json:"messages"`
	MessageLimit int       `json:"message_limit"`
	Tokens       int64     `json:"tokens"`
	TokenLimit   int       `json:"token_limit"`
	CostUSD      string    `json:"cost_usd"`
	Unlimited    bool      `json:"unlimited"`
	ResetsAt     time.Time `json:"resets_at"`
}

// Status is GET /v1/assistant/status: everything the client needs before the
// chat screen renders.
type Status struct {
	Available       bool          `json:"available"`
	Provider        *providerInfo `json:"provider"`
	Model           string        `json:"model"`
	Mock            bool          `json:"mock"`
	AllowWrites     bool          `json:"allow_writes"`
	MaxMessageChars int           `json:"max_message_chars"`
	Consent         ConsentState  `json:"consent"`
	Usage           UsageToday    `json:"usage"`
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	st := Status{Available: s.provider != nil, AllowWrites: s.cfg.allowWrites, MaxMessageChars: s.cfg.maxMessageChars}
	if s.provider != nil {
		st.Provider = &providerInfo{Name: s.provider.Name(), Label: s.provider.Label()}
		st.Model = s.cfg.model
		st.Mock = s.provider.Name() == "mock"
		c, err := s.consent(r.Context(), id.UserID)
		if err != nil {
			return err
		}
		st.Consent = c
	}
	u, err := s.usageToday(r.Context(), id)
	if err != nil {
		return err
	}
	st.Usage = u
	httpx.OK(w, st)
	return nil
}

func (s *Service) consent(ctx context.Context, userID uuid.UUID) (ConsentState, error) {
	c := ConsentState{Required: true}
	if s.provider == nil {
		return c, nil
	}
	c.Provider = s.provider.Name()
	var at time.Time
	err := s.d.Pool.QueryRow(ctx, `SELECT created_at FROM assistant_consents WHERE user_id = $1 AND provider = $2`,
		userID, c.Provider).Scan(&at)
	switch {
	case err == nil:
		c.Required, c.AcceptedAt = false, &at
	case !isNoRows(err):
		return c, err
	}
	return c, nil
}

func errConsentRequired(p Provider) *httpx.Error {
	e := httpx.NewError(http.StatusForbidden, "ai_consent_required",
		"Allow sending your messages to "+p.Label()+" to use the assistant.")
	e.Details = map[string]any{"provider": p.Name(), "provider_label": p.Label()}
	return e
}

type consentReq struct {
	Provider string `json:"provider"`
}

func (s *Service) acceptConsent(w http.ResponseWriter, r *http.Request) error {
	if err := s.requireProvider(); err != nil {
		return err
	}
	id := auth.MustFromContext(r.Context())
	var req consentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Provider != s.provider.Name() {
		// The dialog named a different provider than the server now uses.
		return httpx.Conflict("The AI provider changed. Review it and accept again.")
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO assistant_consents (user_id, provider) VALUES ($1, $2)
		ON CONFLICT (user_id, provider) DO NOTHING`, id.UserID, req.Provider); err != nil {
		return err
	}
	c, err := s.consent(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, c)
	return nil
}

// withdrawConsent removes every provider consent; the next message asks again.
func (s *Service) withdrawConsent(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM assistant_consents WHERE user_id = $1`, id.UserID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) usageToday(ctx context.Context, id *auth.Identity) (UsageToday, error) {
	now := time.Now().UTC()
	u := UsageToday{Day: now.Format(time.DateOnly), MessageLimit: s.cfg.dailyMessages, TokenLimit: s.cfg.dailyTokens,
		CostUSD: "0.000000", Unlimited: id.IsAdmin(), ResetsAt: nextUTCMidnight(now)}
	err := s.d.Pool.QueryRow(ctx, `
		SELECT messages, input_tokens + cache_write_tokens + output_tokens, cost_usd::text
		FROM assistant_usage_daily WHERE user_id = $1 AND day = current_date`, id.UserID,
	).Scan(&u.Messages, &u.Tokens, &u.CostUSD)
	if err != nil && !isNoRows(err) {
		return u, err
	}
	return u, nil
}

// checkQuota enforces the daily limits (admins are unlimited). The check is
// advisory: concurrent turns may overshoot by one.
func (s *Service) checkQuota(ctx context.Context, id *auth.Identity) error {
	if id.IsAdmin() {
		return nil
	}
	u, err := s.usageToday(ctx, id)
	if err != nil {
		return err
	}
	kind, limit, used := "", 0, int64(0)
	switch {
	case s.cfg.dailyMessages > 0 && u.Messages >= s.cfg.dailyMessages:
		kind, limit, used = "messages", s.cfg.dailyMessages, int64(u.Messages)
	case s.cfg.dailyTokens > 0 && u.Tokens >= int64(s.cfg.dailyTokens):
		kind, limit, used = "tokens", s.cfg.dailyTokens, u.Tokens
	default:
		return nil
	}
	e := httpx.NewError(http.StatusTooManyRequests, "quota_exceeded",
		"You've reached today's assistant limit. It resets at midnight UTC.")
	e.Details = map[string]any{"kind": kind, "limit": limit, "used": used, "resets_at": u.ResetsAt}
	return e
}

// addUsage records tokens (and a message when messages is 1) for today.
// Best effort: a failure is logged. Nothing is written for a deleted user.
func (s *Service) addUsage(ctx context.Context, userID uuid.UUID, messages int, u Usage, cost float64) {
	_, err := s.d.Pool.Exec(ctx, `
		INSERT INTO assistant_usage_daily (user_id, day, messages, input_tokens, cache_read_tokens, cache_write_tokens, output_tokens, cost_usd)
		SELECT $1, current_date, $2, $3, $4, $5, $6, $7::numeric
		WHERE EXISTS (SELECT 1 FROM users WHERE id = $1 AND status <> 'deleted')
		ON CONFLICT (user_id, day) DO UPDATE SET
		  messages = assistant_usage_daily.messages + EXCLUDED.messages,
		  input_tokens = assistant_usage_daily.input_tokens + EXCLUDED.input_tokens,
		  cache_read_tokens = assistant_usage_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
		  cache_write_tokens = assistant_usage_daily.cache_write_tokens + EXCLUDED.cache_write_tokens,
		  output_tokens = assistant_usage_daily.output_tokens + EXCLUDED.output_tokens,
		  cost_usd = assistant_usage_daily.cost_usd + EXCLUDED.cost_usd,
		  updated_at = now()`,
		userID, messages, u.InputTokens, u.CacheReadTokens, u.CacheWriteTokens, u.OutputTokens, formatCost(cost))
	if err != nil {
		slog.WarnContext(ctx, "assistant: recording usage failed", "user_id", userID, "err", err)
	}
}

// ---- admin -----------------------------------------------------------------

// DailyTotals is one day of assistant usage across all users.
type DailyTotals struct {
	Day              string `json:"day"`
	Users            int    `json:"users"`
	Messages         int64  `json:"messages"`
	InputTokens      int64  `json:"input_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CostUSD          string `json:"cost_usd"`
}

// adminUsage is GET /v1/admin/assistant/usage?days=30: daily totals, newest
// first (at most 90 days, so one page).
func (s *Service) adminUsage(w http.ResponseWriter, r *http.Request) error {
	days := 30
	if v, ok, err := httpx.QueryFloat(r, "days"); err != nil {
		return err
	} else if ok {
		days = min(90, max(1, int(v)))
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT day::text, count(*)::int, sum(messages), sum(input_tokens), sum(cache_read_tokens),
		       sum(cache_write_tokens), sum(output_tokens), sum(cost_usd)::text
		FROM assistant_usage_daily WHERE day > current_date - $1::int
		GROUP BY day ORDER BY day DESC`, days)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []DailyTotals{}
	for rows.Next() {
		var t DailyTotals
		if err := rows.Scan(&t.Day, &t.Users, &t.Messages, &t.InputTokens, &t.CacheReadTokens,
			&t.CacheWriteTokens, &t.OutputTokens, &t.CostUSD); err != nil {
			return err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[DailyTotals]{Items: out})
	return nil
}
