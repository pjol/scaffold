// Package auth verifies bearer tokens through a Provider (Clerk by default),
// resolves and lazily provisions the local users row, and provides the route
// guards. Swapping identity providers means writing another Provider; the
// users table keys on the provider-neutral auth_subject.
package auth

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Provider is the identity provider.
type Provider interface {
	// Subject verifies a bearer token and returns the provider's user id.
	Subject(ctx context.Context, r *http.Request, token string) (string, error)
	// Profile fetches the user's verified email, name and avatar for
	// first-contact provisioning. Email is "" when none is verified.
	Profile(ctx context.Context, subject string) (Profile, error)
	// DeleteUser removes the user at the provider (account deletion).
	DeleteUser(ctx context.Context, subject string) error
}

type Profile struct {
	Email       string
	DisplayName string
	AvatarURL   string
}

// Identity is the authenticated caller.
type Identity struct {
	UserID      uuid.UUID
	Subject     string
	Email       string
	DisplayName string
	AdminRole   string // "", "support", "admin"
	Status      string // active | suspended | banned
	// Accepted legal versions ("" = never); legal.RequireConsent compares them.
	TermsVersion   string
	PrivacyVersion string
}

func (i *Identity) IsAdmin() bool   { return i.AdminRole == "admin" }
func (i *Identity) IsSupport() bool { return i.AdminRole == "support" || i.AdminRole == "admin" }
func (i *Identity) Active() bool    { return i.Status == "active" }

type ctxKey int

const identityKey ctxKey = iota

func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// FromContext returns the caller; nil outside authenticated routes.
func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityKey).(*Identity)
	return id
}

// MustFromContext returns the caller or panics. Use only in handlers mounted
// behind Middleware (UserRoutes, AdminRoutes).
func MustFromContext(ctx context.Context) *Identity {
	id := FromContext(ctx)
	if id == nil {
		panic("auth: no identity in context (route not behind auth middleware)")
	}
	return id
}

// Authenticator wires the Provider to the users table.
type Authenticator struct {
	pool        *pgxpool.Pool
	provider    Provider
	adminEmails []string
	devBypass   bool
}

func New(pool *pgxpool.Pool, provider Provider, adminEmails []string, devBypass bool) *Authenticator {
	return &Authenticator{pool: pool, provider: provider, adminEmails: adminEmails, devBypass: devBypass}
}

// Provider returns the identity provider (account deletion uses it).
func (a *Authenticator) Provider() Provider { return a.provider }

// Middleware verifies the bearer token and attaches the Identity. Banned
// users are refused; suspended users pass (so GET /v1/me can show their
// status) and mutations guard with RequireActive.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		// Browsers cannot set headers on WebSocket upgrades; accept ?token=.
		if header == "" {
			if t := r.URL.Query().Get("token"); t != "" {
				header = "Bearer " + t
			}
		}
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" {
			httpx.Fail(w, r, httpx.ErrUnauthenticated)
			return
		}
		// Credentials issued to AI agents (MCP OAuth tokens, API keys) work
		// only at the agent endpoint, where every call is a gated tool. They
		// never reach user routes, uploads or the WebSocket, whatever a
		// provider would make of them.
		if IsAgentCredential(token) {
			httpx.Fail(w, r, errAgentCredential)
			return
		}
		var subject, devEmail string
		if a.devBypass {
			subject, devEmail, _ = parseDevToken(token)
		}
		if subject == "" {
			if a.provider == nil {
				httpx.Fail(w, r, httpx.ErrUnauthenticated)
				return
			}
			s, err := a.provider.Subject(r.Context(), r, token)
			if err != nil || s == "" {
				httpx.Fail(w, r, httpx.ErrUnauthenticated)
				return
			}
			subject = s
		}
		id, err := a.resolve(r.Context(), subject, devEmail)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if id.Status == "banned" || id.Status == "deleted" {
			httpx.Fail(w, r, httpx.ErrSuspended)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
	})
}

// DevToken builds the development bearer token for subject/email. dev-up,
// seed scripts and the web/mobile dev sign-in use the same format.
func DevToken(subject, email string) string { return "dev:" + subject + ":" + email }

// parseDevToken parses "dev:<subject>:<email>".
func parseDevToken(token string) (subject, email string, ok bool) {
	rest, found := strings.CutPrefix(token, "dev:")
	if !found {
		return "", "", false
	}
	subject, email, found = strings.Cut(rest, ":")
	if !found || subject == "" || !strings.Contains(email, "@") {
		return "", "", false
	}
	return "dev_" + strings.TrimPrefix(subject, "dev_"), strings.ToLower(email), true
}

func (a *Authenticator) isAdminEmail(email string) bool {
	return slices.Contains(a.adminEmails, strings.ToLower(strings.TrimSpace(email)))
}

// resolve loads the users row for subject, provisioning it on first contact.
// devEmail is set only for dev-bypass tokens.
func (a *Authenticator) resolve(ctx context.Context, subject, devEmail string) (*Identity, error) {
	id := &Identity{Subject: subject}
	var role *string
	err := a.pool.QueryRow(ctx, `
		SELECT id, email, display_name, admin_role, status,
		       COALESCE(terms_version_accepted, ''), COALESCE(privacy_version_accepted, '')
		FROM users WHERE auth_subject = $1`, subject,
	).Scan(&id.UserID, &id.Email, &id.DisplayName, &role, &id.Status, &id.TermsVersion, &id.PrivacyVersion)
	if err == nil {
		if role != nil {
			id.AdminRole = *role
		}
		if id.AdminRole != "admin" && a.isAdminEmail(id.Email) {
			if _, err := a.pool.Exec(ctx, `UPDATE users SET admin_role = 'admin', updated_at = now() WHERE id = $1`, id.UserID); err == nil {
				id.AdminRole = "admin"
			}
		}
		return id, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}

	p := Profile{Email: devEmail}
	if devEmail != "" {
		p.DisplayName = strings.Split(devEmail, "@")[0]
	} else {
		if p, err = a.provider.Profile(ctx, subject); err != nil {
			return nil, err
		}
	}
	if p.Email == "" {
		// ADMIN_EMAILS and invites trust the email, so only verified ones count.
		return nil, errNoVerifiedEmail
	}
	var adminRole *string
	if a.isAdminEmail(p.Email) {
		r := "admin"
		adminRole = &r
	}
	err = a.pool.QueryRow(ctx, `
		INSERT INTO users (auth_subject, email, display_name, avatar_url, admin_role)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		ON CONFLICT (auth_subject) DO UPDATE SET updated_at = now()
		RETURNING id, email, display_name, admin_role, status`,
		subject, strings.ToLower(p.Email), p.DisplayName, p.AvatarURL, adminRole,
	).Scan(&id.UserID, &id.Email, &id.DisplayName, &role, &id.Status)
	if err != nil {
		return nil, err
	}
	if role != nil {
		id.AdminRole = *role
	}
	return id, nil
}

var errNoVerifiedEmail = httpx.NewError(http.StatusForbidden, "email_unverified", "Verify your email address to continue.")

// RateLimit allows each signed-in user (by user id; by client IP when
// signed out) n requests per window on the routes it wraps. Use it on
// expensive or abusable endpoints: sending messages, uploads, invites.
func RateLimit(n int, window time.Duration) func(http.Handler) http.Handler {
	return httprate.LimitBy(n, window, func(r *http.Request) (string, error) {
		if id := FromContext(r.Context()); id != nil {
			return "u:" + id.UserID.String(), nil
		}
		return httpx.KeyByClientIP(r)
	}, httprate.WithLimitHandler(httpx.RateLimitedHandler))
}

// RequireActive blocks suspended accounts from the routes it wraps.
func RequireActive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := FromContext(r.Context()); id == nil || !id.Active() {
			httpx.Fail(w, r, httpx.ErrSuspended)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminRole gates admin routes: "support" admits support and admin,
// "admin" admits admin only.
func RequireAdminRole(minRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := FromContext(r.Context())
			if id == nil || !(id.IsAdmin() || (minRole == "support" && id.IsSupport())) {
				httpx.Fail(w, r, httpx.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NormalizeOrigin reduces a URL to lowercase scheme://host[:port] ("" if it
// is not an absolute http(s) URL). "*" is kept.
func NormalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "*" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}
