// Package payments takes money through a swappable provider (Stripe by
// default): customers, hosted checkout for one-time prices and
// subscriptions, the billing portal, entitlements, an append-only ledger,
// refunds and optional Connect payouts. Every provider call goes through
// Gateway (gateway.go); the rest of the app sees only this package's
// functions (HasEntitlement, RequireEntitlement, the ledger readers, From).
//
// State changes come only from the provider: orders are created by the
// webhook (or the reconcile job that re-reads the provider), never by a
// client saying it paid.
package payments

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notifications"
)

// KindFailed is the notification kind for a failed subscription payment.
const KindFailed = "payments.failed"

type Service struct {
	d   *app.Deps
	gw  Gateway // nil without STRIPE_SECRET_KEY: every payments route answers 503
	cfg settings
}

type settings struct {
	connect           bool
	feeBPS            int
	retentionYears    int
	returnPaths       []string
	renewalGrace      time.Duration
	seedPrices        string
	shippingCountries []string
	webURL            string
}

// shared is what the package-level helpers (RequireEntitlement) need. New
// fills it; it is read at request time.
type shared struct {
	pool       *pgxpool.Pool
	configured bool
}

var current atomic.Pointer[shared]

func New(d *app.Deps) *Service {
	key := config.String("STRIPE_SECRET_KEY", "")
	webhookSecret := config.String("STRIPE_WEBHOOK_SECRET", "")
	connectSecret := config.String("STRIPE_CONNECT_WEBHOOK_SECRET", "")
	s := &Service{d: d, cfg: settings{
		connect:        config.Bool("PAYMENTS_CONNECT_ENABLED", false),
		feeBPS:         config.Int("PAYMENTS_PLATFORM_FEE_BPS", 0),
		retentionYears: config.Int("PAYMENTS_RETENTION_YEARS", 7),
		returnPaths:    config.List("PAYMENTS_RETURN_PATHS", "/payments/,/settings"),
		renewalGrace:   config.Duration("PAYMENTS_RENEWAL_GRACE", 24*time.Hour),
		seedPrices:     config.String("PAYMENTS_PRICES", ""),
		// Physical goods: Checkout collects a shipping address in these
		// countries and the order stores it. Empty = no address asked.
		shippingCountries: config.List("PAYMENTS_SHIPPING_COUNTRIES", ""),
		webURL:            webBase(d.Cfg.PublicWebURL, d.Cfg.CORSOrigins),
	}}
	if s.cfg.feeBPS < 0 || s.cfg.feeBPS > 10000 {
		config.Problem("PAYMENTS_PLATFORM_FEE_BPS must be 0 to 10000 (got %d)", s.cfg.feeBPS)
	}
	if s.cfg.renewalGrace < 0 {
		config.Problem("PAYMENTS_RENEWAL_GRACE must not be negative (got %s)", s.cfg.renewalGrace)
	}
	if s.cfg.retentionYears < 1 {
		config.Problem("PAYMENTS_RETENTION_YEARS must be at least 1 (got %d)", s.cfg.retentionYears)
	}
	for _, p := range s.cfg.returnPaths {
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
			config.Problem("PAYMENTS_RETURN_PATHS entries must be app paths starting with / (got %q)", p)
		}
	}
	for i, c := range s.cfg.shippingCountries {
		c = strings.ToUpper(c)
		if !countryPattern.MatchString(c) {
			config.Problem("PAYMENTS_SHIPPING_COUNTRIES entries must be two-letter ISO country codes (got %q)", c)
		}
		s.cfg.shippingCountries[i] = c
	}
	if key != "" {
		if d.Cfg.IsDevelopment() && (strings.HasPrefix(key, "sk_live_") || strings.HasPrefix(key, "rk_live_")) {
			config.Problem("STRIPE_SECRET_KEY is a live key; use a test-mode key (sk_test_…) in development")
		}
		if !d.Cfg.IsDevelopment() {
			// A configured provider with no webhook secret takes money and
			// records nothing; refuse to boot instead.
			config.Required("STRIPE_WEBHOOK_SECRET")
		}
		if webhookSecret == "" {
			slog.Warn("payments: STRIPE_WEBHOOK_SECRET unset; Stripe webhooks are refused (dev-up's payments hook sets it from `stripe listen`)")
		}
		s.gw = newGateway(key, webhookSecret, connectSecret)
	}
	current.Store(&shared{pool: d.Pool, configured: s.gw != nil})
	notifications.RegisterKind(notifications.Kind{ID: KindFailed, Label: "Payment problems", Push: true, Email: true})
	slog.Info("payments", "provider", providerName(s.gw), "connect", s.cfg.connect)
	return s
}

// newGateway picks the provider. Replace Stripe here.
func newGateway(secretKey, webhookSecret, connectSecret string) Gateway {
	return NewStripe(secretKey, webhookSecret, connectSecret)
}

func providerName(gw Gateway) string {
	if gw == nil {
		return "none"
	}
	return gw.Name()
}

func webBase(public string, origins []string) string {
	if public != "" {
		return strings.TrimRight(public, "/")
	}
	if len(origins) > 0 && origins[0] != "*" {
		return strings.TrimRight(origins[0], "/")
	}
	return ""
}

func (s *Service) Name() string { return "payments" }

// From returns the installed payments service (nil if absent). Stitching
// code calls it at request time to start a checkout with an app reference.
func From(d *app.Deps) *Service {
	for _, m := range d.Modules {
		if p, ok := m.(*Service); ok {
			return p
		}
	}
	return nil
}

// Configured reports whether a provider is set up.
func (s *Service) Configured() bool { return s.gw != nil }

// ---- routes ------------------------------------------------------------------

func (s *Service) MountPublic(r chi.Router) {
	r.With(s.requireConfigured).Get("/payments/prices", httpx.Handler(s.listPrices))
}

func (s *Service) MountUser(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.requireConfigured)
		r.With(auth.RequireActive, auth.RateLimit(20, time.Hour), httpx.Detach(30*time.Second)).
			Post("/payments/checkout", httpx.Handler(s.checkout))
		r.Get("/payments/checkout/{id}", httpx.Handler(s.getCheckout))
		r.With(auth.RequireActive, auth.RateLimit(30, time.Hour), httpx.Detach(30*time.Second)).
			Post("/payments/portal", httpx.Handler(s.portal))
		r.Get("/payments/subscription", httpx.Handler(s.getSubscription))
		r.Get("/payments/entitlements", httpx.Handler(s.listEntitlements))
		r.Get("/payments/history", httpx.Handler(s.history))
		// Mounted either way: with Connect off they answer 404 connect_disabled,
		// which clients use to hide payouts.
		r.With(s.requireConnect).Get("/payments/connect/account", httpx.Handler(s.getConnectedAccount))
		r.With(s.requireConnect, auth.RequireActive, auth.RateLimit(10, time.Hour), httpx.Detach(30*time.Second)).
			Post("/payments/connect/onboarding", httpx.Handler(s.connectOnboarding))
	})
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.requireConfigured)
		r.Get("/payments/customers", httpx.Handler(s.adminListCustomers))
		r.Get("/payments/customers/{user_id}", httpx.Handler(s.adminGetCustomer))
		r.Get("/payments/ledger", httpx.Handler(s.adminLedger))
		r.Get("/payments/refunds", httpx.Handler(s.adminListRefunds))
		r.Get("/payments/prices", httpx.Handler(s.adminListPrices))
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAdminRole("admin"))
			r.With(httpx.Detach(45*time.Second)).Post("/payments/refunds", httpx.Handler(s.adminRefund))
			r.With(httpx.Detach(20*time.Second)).Post("/payments/prices", httpx.Handler(s.adminCreatePrice))
			r.With(httpx.Detach(20*time.Second)).Patch("/payments/prices/{key}", httpx.Handler(s.adminUpdatePrice))
			r.Delete("/payments/prices/{key}", httpx.Handler(s.adminDeletePrice))
		})
	})
}

func (s *Service) MountWebhooks(r chi.Router) {
	r.Post("/stripe", s.webhook)
}

// ---- errors ------------------------------------------------------------------

var (
	errUnconfigured     = httpx.NewError(http.StatusServiceUnavailable, "payments_unconfigured", "Payments aren't set up yet.")
	errPriceUnavailable = httpx.NewError(http.StatusNotFound, "price_not_found", "That price isn't available.")
)

// providerFailure turns a gateway error into an API error: refusals the
// provider explained become 400s with its message, the rest 502s.
func providerFailure(ctx context.Context, op string, err error) error {
	var pe *ProviderError
	if errors.As(err, &pe) {
		slog.WarnContext(ctx, "payments: provider refused", "op", op, "code", pe.Code)
		return httpx.NewError(http.StatusBadRequest, "payment_provider_refused", pe.Message)
	}
	slog.ErrorContext(ctx, "payments: provider call failed", "op", op, "err", err)
	return httpx.NewError(http.StatusBadGateway, "payment_provider_error", "The payment provider didn't respond. Try again.")
}

func (s *Service) requireConnect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.connect {
			httpx.Fail(w, r, httpx.NewError(http.StatusNotFound, "connect_disabled", "Payouts aren't turned on."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) requireConfigured(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.gw == nil {
			httpx.Fail(w, r, errUnconfigured)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- entitlements (exported for other features and stitching) --------------

var (
	keyPattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
	countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
)

// HasEntitlement reports whether the user holds key now. It reads only the
// database, so it works on the pool or inside a caller's transaction.
func HasEntitlement(ctx context.Context, q db.Querier, userID uuid.UUID, key string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM payment_entitlements
		               WHERE user_id = $1 AND key = $2 AND (active_until IS NULL OR active_until > now()))`,
		userID, key).Scan(&ok)
	return ok, err
}

// RequireEntitlement guards user routes: callers without key get 402
// payment_required with details.entitlement and details.prices (the active
// catalog prices that grant it), which clients use to open checkout.
func RequireEntitlement(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.FromContext(r.Context())
			if id == nil {
				httpx.Fail(w, r, httpx.ErrUnauthenticated)
				return
			}
			st := current.Load()
			if st == nil {
				httpx.Fail(w, r, errUnconfigured)
				return
			}
			ok, err := HasEntitlement(r.Context(), st.pool, id.UserID, key)
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			if ok {
				next.ServeHTTP(w, r)
				return
			}
			if !st.configured {
				httpx.Fail(w, r, errUnconfigured)
				return
			}
			prices, err := pricesGranting(r.Context(), st.pool, key)
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			e := httpx.NewError(http.StatusPaymentRequired, "payment_required", "A paid plan is required. Choose one to continue.")
			e.Details = map[string]any{"entitlement": key, "prices": prices}
			httpx.Fail(w, r, e)
		})
	}
}
