// Package api assembles every module into one chi router. Modules own their
// routes; this package decides where they mount and which guards apply.
// Change it deliberately: every feature relies on the groups below.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/accounts"
	"scaffoldapp/backend/internal/admin"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/legal"
	"scaffoldapp/backend/internal/realtime"
)

// App is the assembled API.
type App struct {
	Handler http.Handler
	Modules []app.Module
	Hub     *realtime.Hub
}

// Readiness backs /readyz.
type Readiness func(ctx context.Context) error

// Build runs dependency providers, constructs core and feature modules, and
// builds the router. cmd/server and cmd/seed both use it.
func Build(d *app.Deps, ready Readiness) *App {
	for _, provide := range depProviders {
		provide(d)
	}
	acc := accounts.New(d)
	mods := []app.Module{acc, admin.New(d, acc)}
	for _, ctor := range moduleCtors {
		mods = append(mods, ctor(d))
	}
	d.Modules = mods

	hub := realtime.NewHub(d.Broker, authorizer(mods), realtime.WithAllowedOrigins(d.Cfg.CORSOrigins))
	if ready == nil {
		ready = func(ctx context.Context) error { return d.Pool.Ping(ctx) }
	}
	return &App{Handler: router(d, mods, hub, ready), Modules: mods, Hub: hub}
}

// Start runs the realtime hub and, when workers is true, every module's
// background workers. It returns immediately.
func (a *App) Start(ctx context.Context, workers bool) {
	go a.Hub.Run(ctx)
	if !workers {
		return
	}
	for _, m := range a.Modules {
		if w, ok := m.(app.Worker); ok {
			w.Start(ctx)
		}
	}
}

// authorizer asks each module's TopicAuthorizer in turn; the first that
// recognises the topic decides.
func authorizer(mods []app.Module) realtime.Authorizer {
	return func(ctx context.Context, userID uuid.UUID, isAdmin bool, topic string) bool {
		for _, m := range mods {
			if ta, ok := m.(app.TopicAuthorizer); ok {
				if handled, allowed := ta.AuthorizeTopic(ctx, userID, isAdmin, topic); handled {
					return allowed
				}
			}
		}
		return false
	}
}

func router(d *app.Deps, mods []app.Module, hub *realtime.Hub, ready Readiness) http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)
	r.Use(httpx.ClientIP(d.Cfg.TrustedProxyHops))
	r.Use(httpx.Logger)
	r.Use(httpx.Recoverer)
	r.Use(httpx.SecurityHeaders)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: d.Cfg.CORSOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Client-Platform", "X-Client-Version", "X-Request-ID"},
		ExposedHeaders: []string{"X-Request-ID"},
		MaxAge:         600,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { httpx.OK(w, map[string]string{"status": "ok"}) })
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			httpx.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "error": err.Error()})
			return
		}
		httpx.OK(w, map[string]string{"status": "ready"})
	})

	// Development-only local object storage (no S3_BUCKET).
	r.Handle("/uploads/*", d.Storage.LocalHandler())

	// Webhooks: raw bodies, provider signatures, no auth, no request timeout.
	r.Route("/webhooks", func(r chi.Router) {
		for _, m := range mods {
			if wr, ok := m.(app.WebhookRoutes); ok {
				wr.MountWebhooks(r)
			}
		}
	})

	// Root paths some clients expect (/mcp, /.well-known/*): IP rate limit,
	// the module authenticates.
	r.Group(func(r chi.Router) {
		r.Use(httprate.LimitBy(d.Cfg.RateLimitPerMinuteIP, time.Minute, httpx.KeyByClientIP,
			httprate.WithLimitHandler(httpx.RateLimitedHandler)))
		for _, m := range mods {
			if rr, ok := m.(app.RootRoutes); ok {
				rr.MountRoot(r)
			}
		}
	})

	// WebSocket: long-lived, outside the timeout group; connects are rate
	// limited per IP before auth, and the hub caps sockets per user.
	wsLimit := httprate.LimitBy(d.Cfg.RateLimitWSPerMinuteIP, time.Minute, httpx.KeyByClientIP,
		httprate.WithLimitHandler(httpx.RateLimitedHandler))
	r.With(wsLimit, d.Auth.Middleware).Get("/v1/ws", hub.ServeWS)

	r.Route("/v1", func(r chi.Router) {
		r.Use(httprate.LimitBy(d.Cfg.RateLimitPerMinuteIP, time.Minute, httpx.KeyByClientIP,
			httprate.WithLimitHandler(httpx.RateLimitedHandler)))
		timeout := middleware.Timeout(d.Cfg.RequestTimeout)

		r.Group(func(r chi.Router) {
			r.Use(timeout)
			for _, m := range mods {
				if pr, ok := m.(app.PublicRoutes); ok {
					pr.MountPublic(r)
				}
			}
		})

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Middleware)
			r.Use(auth.RateLimit(d.Cfg.RateLimitPerMinuteUser, time.Minute))
			r.Use(legal.RequireConsent)

			// Streams (server-sent events): same guards, no request timeout.
			for _, m := range mods {
				if sr, ok := m.(app.StreamRoutes); ok {
					sr.MountStream(r)
				}
			}

			r.Group(func(r chi.Router) {
				r.Use(timeout)
				for _, m := range mods {
					if ur, ok := m.(app.UserRoutes); ok {
						ur.MountUser(r)
					}
				}
				r.Route("/admin", func(r chi.Router) {
					r.Use(auth.RequireAdminRole("support"))
					r.Use(auth.RequireActive)
					for _, m := range mods {
						if ar, ok := m.(app.AdminRoutes); ok {
							ar.MountAdmin(r)
						}
					}
				})
			})
		})
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) { httpx.Fail(w, req, httpx.ErrNotFound) })
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		httpx.Fail(w, req, httpx.NewError(http.StatusMethodNotAllowed, "invalid_request", "Method not allowed."))
	})
	return r
}
