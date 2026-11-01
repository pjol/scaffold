// Package agents applies each user's AI agent scope: how far an agent acting
// for them (an MCP client on their OAuth grant or API key, or the in-app
// assistant) may go, per feature, per external service and per resource.
// Levels are off, read (the default), suggest and edit; a scope only ever
// narrows the user's own permissions, and agents never get sharing,
// permission or credential changes.
//
// The Service implements app.AgentPolicy: the mcp and assistant features
// find it in Deps.Modules and gate every tool call through GateTool. With
// suggest, a write is stored as a suggestion, the user is notified, and
// accepting it re-runs the tool as the user. The access feature's SQL
// helpers read the same scope through access_agent_cap (this feature's
// migration replaces access's stub), so list queries and search narrow too.
package agents

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/realtime"
)

// KindSuggestion is the notification a user gets when an agent suggests a change.
const KindSuggestion = "agents.suggestion"

// Realtime events on the user's own topic (user:<id>).
const (
	EventSuggestion        = "agents.suggestion"         // {suggestion}: a new one
	EventSuggestionUpdated = "agents.suggestion_updated" // {suggestion}: accepted, rejected, failed or expired
	EventScopesChanged     = "agents.scopes_changed"     // {}: settings or an override changed
)

const (
	maxResourceScopes = 2000             // overrides per user
	maxResolveBatch   = 50               // refs in one GET /agents/scope
	toolTimeout       = 30 * time.Second // accept runs the tool this long at most
	previewTimeout    = 10 * time.Second
	dynamicTimeout    = 10 * time.Second // asking DynamicToolProviders
	maxResultBytes    = 16 << 10         // tool output kept on an accepted suggestion
	maxInputBytes     = 64 << 10         // a suggestion's stored input
	maxBulk           = 50               // suggestions one bulk call decides
	pushFoldWindow    = 2 * time.Minute  // later suggestions from one agent go to the inbox without a push
	stuckAfter        = 10 * time.Minute // an 'accepting' row older than this failed mid-run

	scopeWritesPerHour  = 300
	decisionsPerHour    = 600
	bulkPerHour         = 60
	adminWritesPerHour  = 120
	clientTouchInterval = time.Minute
)

// Service is the agents module.
type Service struct {
	d          *app.Deps
	ttl        time.Duration // pending suggestions expire after this
	maxPending int           // pending suggestions per user
	perHour    int           // suggestions created per user per hour
	retention  time.Duration // decided suggestions are deleted after this

	toolsOnce sync.Once
	tools     map[string]indexedTool     // ToolProvider tools by name
	toolKinds map[string]map[string]bool // feature → other features' kinds its tools target

	touched sync.Map // "<user>|<via>|<client_key>" → time.Time of the last agents_clients write
}

// New reads the settings and registers the notification kind and the push
// fold (a burst of suggestions from one agent pushes once).
func New(d *app.Deps) *Service {
	s := &Service{
		d:          d,
		ttl:        config.Duration("AGENTS_SUGGESTION_TTL", 168*time.Hour),
		maxPending: config.Int("AGENTS_MAX_PENDING", 50),
		perHour:    config.Int("AGENTS_SUGGESTIONS_PER_HOUR", 120),
		retention:  config.Duration("AGENTS_SUGGESTION_RETENTION", 90*24*time.Hour),
	}
	if s.ttl < time.Hour || s.ttl > 90*24*time.Hour {
		config.Problem("AGENTS_SUGGESTION_TTL must be between 1h and 2160h (got %s)", s.ttl)
	}
	if s.maxPending < 1 || s.maxPending > 1000 {
		config.Problem("AGENTS_MAX_PENDING must be between 1 and 1000 (got %d)", s.maxPending)
	}
	if s.perHour < 1 || s.perHour > 10000 {
		config.Problem("AGENTS_SUGGESTIONS_PER_HOUR must be between 1 and 10000 (got %d)", s.perHour)
	}
	if s.retention < 24*time.Hour {
		config.Problem("AGENTS_SUGGESTION_RETENTION must be at least 24h (got %s)", s.retention)
	}
	notifications.RegisterKind(notifications.Kind{ID: KindSuggestion, Label: "Agent suggestions", Push: true})
	notifications.AddDeliveryFilter("agents.suggestion_burst", s.foldPushes)
	return s
}

func (s *Service) Name() string { return "agents" }

func (s *Service) MountUser(r chi.Router) {
	r = r.With(refuseAgents)
	active := r.With(auth.RequireActive)
	scopeWrite := active.With(auth.RateLimit(scopeWritesPerHour, time.Hour))
	decide := active.With(auth.RateLimit(decisionsPerHour, time.Hour))

	r.Get("/agents/settings", httpx.Handler(s.getSettings))
	scopeWrite.Put("/agents/settings", httpx.Handler(s.putSettings))
	r.Get("/agents/services", httpx.Handler(s.listServices))
	r.Get("/agents/scope", httpx.Handler(s.resolveScopes))
	scopeWrite.Put("/agents/scope", httpx.Handler(s.putScope))
	r.Get("/agents/scopes", httpx.Handler(s.listResourceScopes))
	scopeWrite.Delete("/agents/scopes/{id}", httpx.Handler(s.deleteResourceScope))

	r.Get("/agents/summary", httpx.Handler(s.summary))
	r.Get("/agents/suggestions", httpx.Handler(s.listSuggestions))
	r.Get("/agents/suggestions/{id}", httpx.Handler(s.getSuggestion))
	decide.Post("/agents/suggestions/{id}/accept", httpx.Handler(s.acceptRoute))
	decide.Post("/agents/suggestions/{id}/reject", httpx.Handler(s.rejectRoute))
	active.With(auth.RateLimit(bulkPerHour, time.Hour)).Post("/agents/suggestions/bulk", httpx.Handler(s.bulkRoute))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/agents/stats", httpx.Handler(s.adminStats))
	r.Get("/agents/users/{userID}", httpx.Handler(s.adminUser))
	r.With(auth.RequireAdminRole("admin"), auth.RateLimit(adminWritesPerHour, time.Hour)).
		Put("/agents/users/{userID}/paused", httpx.Handler(s.adminSetPaused))
}

// Start runs the upkeep: expiry, stuck accepts, retention, orphaned overrides.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "agents.upkeep", 5*time.Minute, s.upkeep)
}

// errAgentForbidden answers agent calls to this feature's routes: agents
// never change their own scope or decide their own suggestions.
var errAgentForbidden = httpx.NewError(http.StatusForbidden, "agent_forbidden",
	"AI agents can't change agent access or decide suggestions. Do it yourself in the app.")

// refuseAgents is a backstop: agents reach the app through tools, and no
// tool of this feature writes.
func refuseAgents(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.AgentFrom(r.Context()); ok {
			httpx.Fail(w, r, errAgentForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- side effects ------------------------------------------------------------

func (s *Service) publish(ctx context.Context, userID uuid.UUID, event string, data any) {
	if s.d.Broker == nil {
		return
	}
	if err := s.d.Broker.Publish(ctx, realtime.UserTopic(userID), event, data); err != nil {
		slog.WarnContext(ctx, "agents: realtime publish failed", "event", event, "err", err)
	}
}

func (s *Service) scopesChanged(ctx context.Context, userID uuid.UUID) {
	s.publish(ctx, userID, EventScopesChanged, map[string]any{})
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}
