// Package connectors lets each user connect external MCP servers (a
// company's Linear, GitHub, Notion or Drive) with their own account, so the
// in-app assistant can use exactly what that person can reach there and
// nothing more. Staff curate a catalog; users may add their own servers when
// CONNECTORS_ALLOW_CUSTOM is on.
//
// Connecting follows the MCP authorization spec (2025-11-25) as an OAuth
// client: protected resource and authorization server discovery, client
// registration (pre-registered, Client ID Metadata Document, or Dynamic
// Client Registration), PKCE S256, the resource indicator, single-use state
// bound to the user, and a finish step that only the signed-in user who
// started the flow, in the browser that came back, can complete. Tokens are
// sealed with AES-256-GCM. Every outbound request goes through an SSRF guard.
//
// Remote tools reach the assistant through app.DynamicToolProvider. They are
// never served by the app's own MCP endpoint.
package connectors

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
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
)

// appName is what authorization servers show on their consent screen.
const appName = "Scaffold App"

// mobileReturnURL is where the callback sends the system browser after a
// mobile connect (expo-web-browser's auth session watches for it). Keep it
// in step with the app's scheme (mobile/app.config.ts).
const mobileReturnURL = "scaffoldapp://settings/connections"

const (
	flowTTL          = 10 * time.Minute
	maxCustomPerUser = 10
	maxConnectorsAll = 200
)

var isNoRows = db.IsNoRows

// Service is the connectors module.
type Service struct {
	d *app.Deps

	seal        *sealer
	devKey      bool
	guard       *guard
	metaClient  *http.Client // metadata GETs: 1 MiB, 10 s, 3 redirects re-checked
	tokenClient *http.Client // registration, token, revocation: 256 KiB, 15 s, no redirects
	mcpClient   *http.Client // MCP traffic: 4 MiB, 60 s, no redirects

	allowCustom  bool
	allowPrivate bool
	toolTTL      time.Duration
	toolTimeout  time.Duration

	tools     *toolCache
	refreshMu sync.Map // connection id → *sync.Mutex (refresh one at a time per instance)
}

// New reads CONNECTORS_* settings.
func New(d *app.Deps) *Service {
	s := &Service{
		d:            d,
		allowCustom:  config.Bool("CONNECTORS_ALLOW_CUSTOM", false),
		allowPrivate: config.Bool("CONNECTORS_ALLOW_PRIVATE_NETWORKS", false),
		toolTTL:      max(30*time.Second, config.Duration("CONNECTORS_TOOL_CACHE_TTL", 5*time.Minute)),
		toolTimeout:  min(2*time.Minute, max(5*time.Second, config.Duration("CONNECTORS_TOOL_TIMEOUT", 30*time.Second))),
		tools:        newToolCache(),
	}
	dev := d.Cfg.IsDevelopment()
	if s.allowPrivate && !dev {
		config.Problem("CONNECTORS_ALLOW_PRIVATE_NETWORKS is only allowed when APP_ENV=development")
		s.allowPrivate = false
	}

	var key []byte
	if raw := config.String("CONNECTORS_ENCRYPTION_KEY", ""); raw != "" {
		k, err := parseKey(raw)
		if err != nil {
			config.Problem("CONNECTORS_ENCRYPTION_KEY %v", err)
		}
		key = k
	} else if dev {
		key, s.devKey = devKey(), true
	} else {
		config.Problem("CONNECTORS_ENCRYPTION_KEY is required outside development (32 random bytes: openssl rand -base64 32)")
	}
	var previous [][]byte
	if raw := config.String("CONNECTORS_ENCRYPTION_KEY_PREVIOUS", ""); raw != "" {
		k, err := parseKey(raw)
		if err != nil {
			config.Problem("CONNECTORS_ENCRYPTION_KEY_PREVIOUS %v", err)
		} else {
			previous = append(previous, k)
		}
	}
	if key == nil {
		key = devKey() // the boot fails at config.Check; keep the value usable until then
	}
	seal, err := newSealer(key, previous...)
	if err != nil {
		config.Problem("CONNECTORS_ENCRYPTION_KEY: %v", err)
		seal, _ = newSealer(devKey())
	}
	s.seal = seal

	s.guard = newGuard(s.allowPrivate, dev)
	s.metaClient = s.guard.client(clientOptions{timeout: 10 * time.Second, maxBody: 1 << 20, maxRedirects: 3})
	s.tokenClient = s.guard.client(clientOptions{timeout: 15 * time.Second, maxBody: 256 << 10})
	s.mcpClient = s.guard.client(clientOptions{timeout: 60 * time.Second, maxBody: 4 << 20})
	return s
}

func (s *Service) Name() string { return "connectors" }

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/connectors/oauth/callback", s.callback)
	r.Get("/connectors/oauth/client-metadata.json", s.clientMetadata)
}

func (s *Service) MountUser(r chi.Router) {
	r.Get("/connectors", httpx.Handler(s.listForUser))
	r.With(auth.RequireActive, auth.RateLimit(10, time.Hour)).Post("/connectors/custom", httpx.Handler(s.createCustom))
	r.With(auth.RequireActive).Delete("/connectors/custom/{id}", httpx.Handler(s.deleteCustom))
	r.With(auth.RequireActive, auth.RateLimit(20, 10*time.Minute)).Post("/connectors/{id}/connect", httpx.Handler(s.connect))
	r.With(auth.RequireActive, auth.RateLimit(20, 10*time.Minute)).Post("/connectors/oauth/finish", httpx.Handler(s.finish))

	r.Get("/connectors/connections/{id}", httpx.Handler(s.getConnection))
	r.With(auth.RequireActive).Patch("/connectors/connections/{id}", httpx.Handler(s.updateConnection))
	r.With(auth.RequireActive).Put("/connectors/connections/{id}/tools/{tool}", httpx.Handler(s.setToolEnabled))
	r.With(auth.RateLimit(30, time.Minute)).Get("/connectors/connections/{id}/tools", httpx.Handler(s.connectionTools))
	r.Delete("/connectors/connections/{id}", httpx.Handler(s.disconnect))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/connectors", httpx.Handler(s.adminList))
	r.Get("/connectors/{id}", httpx.Handler(s.adminGet))
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdminRole("admin"))
		r.Post("/connectors", httpx.Handler(s.adminCreate))
		r.Patch("/connectors/{id}", httpx.Handler(s.adminUpdate))
		r.Delete("/connectors/{id}", httpx.Handler(s.adminDelete))
		r.With(auth.RateLimit(30, time.Minute)).Post("/connectors/check", httpx.Handler(s.adminCheck))
	})
}

// Start runs the flow sweeper and the revocation queue.
func (s *Service) Start(ctx context.Context) {
	if s.devKey {
		slog.Warn("connectors: CONNECTORS_ENCRYPTION_KEY unset; tokens are sealed with the public development key")
	}
	slog.Info("connectors: ready", "custom_servers", s.allowCustom, "private_networks", s.allowPrivate,
		"metadata_document", s.canUseMetadataDocument())
	go jobs.Every(ctx, s.d.Pool, "connectors.sweep_flows", 5*time.Minute, s.sweepFlows)
	go jobs.Every(ctx, s.d.Pool, "connectors.revocations", time.Minute, s.processRevocations)
	if len(s.seal.previousKeyIDs()) > 0 {
		go jobs.Every(ctx, s.d.Pool, "connectors.reseal", time.Minute, s.resealPrevious)
	}
}

// lockConnection serialises token refreshes for one connection on this
// instance (the database row lock covers other instances).
func (s *Service) lockConnection(id uuid.UUID) func() {
	m, _ := s.refreshMu.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
