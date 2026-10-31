// Package identity verifies that users are who they say they are: a hosted
// Persona inquiry when PERSONA_API_KEY is set, otherwise manual review (the
// user uploads a document photo and a selfie, staff approve or reject in the
// admin queue and the photos are deleted right after). Other modules gate on
// IsVerified; clients show the badge from GET /v1/identity/status and
// GET /v1/identity/users/{id}/badge.
package identity

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/storage"
)

const (
	// documentPurpose holds manual-review photos (private).
	documentPurpose = "identity_document"
	// appScheme is the mobile deep-link scheme allowed as a hosted-flow redirect.
	appScheme = "scaffoldapp"
)

type Service struct {
	d        *app.Deps
	provider Provider // nil = manual review
	cfg      settings
}

type settings struct {
	maxAttempts   int           // rejected attempts before the user must contact support
	ttl           time.Duration // verified status lifetime; 0 = no expiry
	environmentID string
}

func New(d *app.Deps) *Service {
	storage.RegisterPurpose(storage.Purpose{Name: documentPurpose, Public: false, Types: storage.ImageTypes})
	s := &Service{d: d, cfg: settings{
		maxAttempts:   config.Int("IDENTITY_MAX_ATTEMPTS", 3),
		ttl:           config.Duration("IDENTITY_VERIFICATION_TTL", 0),
		environmentID: config.String("PERSONA_ENVIRONMENT_ID", ""),
	}}
	if s.cfg.maxAttempts < 1 {
		config.Problem("IDENTITY_MAX_ATTEMPTS must be 1 or more (got %d)", s.cfg.maxAttempts)
		s.cfg.maxAttempts = 3
	}
	if s.cfg.ttl < 0 {
		config.Problem("IDENTITY_VERIFICATION_TTL must not be negative")
		s.cfg.ttl = 0
	}
	if key := config.String("PERSONA_API_KEY", ""); key != "" {
		s.provider = &Persona{
			APIKey:        key,
			TemplateID:    config.Required("PERSONA_TEMPLATE_ID"),
			WebhookSecret: config.Required("PERSONA_WEBHOOK_SECRET"),
			EnvironmentID: s.cfg.environmentID,
			HTTP:          &http.Client{Timeout: personaTimeout},
		}
	}
	slog.Info("identity configured", "mode", s.mode(), "verification_ttl", s.cfg.ttl.String())
	return s
}

func (s *Service) Name() string { return "identity" }

// mode is the provider a new attempt uses: persona or manual.
func (s *Service) mode() string {
	if s.provider != nil {
		return s.provider.Name()
	}
	return "manual"
}

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/identity/users/{id}/badge", httpx.Handler(s.badge))
}

func (s *Service) MountUser(r chi.Router) {
	r.Get("/identity/status", httpx.Handler(s.status))
	// Each start may create a billable provider inquiry; the provider call
	// runs inside the transaction, so a dropped client must not roll it back.
	r.With(auth.RequireActive, auth.RateLimit(10, time.Hour), httpx.Detach(30*time.Second)).
		Post("/identity/start", httpx.Handler(s.start))
	r.With(auth.RequireActive, auth.RateLimit(10, time.Hour)).Post("/identity/documents", httpx.Handler(s.submitDocuments))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/identity/verifications", httpx.Handler(s.adminList))
	r.Get("/identity/verifications/{id}", httpx.Handler(s.adminGet))
	r.Post("/identity/verifications/{id}/approve", httpx.Handler(s.adminApprove))
	r.Post("/identity/verifications/{id}/reject", httpx.Handler(s.adminReject))
}

func (s *Service) MountWebhooks(r chi.Router) {
	r.Post("/persona", s.personaWebhook)
}

// AccountDeleted asks the provider to redact the deleted user's inquiries
// right away (queued in the deletion transaction); housekeeping retries.
func (s *Service) AccountDeleted(ctx context.Context, _ uuid.UUID) error {
	return s.redact(ctx)
}

func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "identity.housekeeping", 10*time.Minute, s.housekeeping)
}

// ---- status ----------------------------------------------------------------------

// verification is an identity_verifications row.
type verification struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Provider    string
	InquiryID   *string
	Status      string
	Reason      string
	DocumentKey *string
	SelfieKey   *string
	SubmittedAt *time.Time
	DecidedAt   *time.Time
	ReviewedBy  *uuid.UUID
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const verificationCols = `id, user_id, provider, inquiry_id, status, reason, document_key, selfie_key,
	submitted_at, decided_at, reviewed_by, expires_at, created_at, updated_at`

func (v *verification) targets() []any {
	return []any{&v.ID, &v.UserID, &v.Provider, &v.InquiryID, &v.Status, &v.Reason, &v.DocumentKey, &v.SelfieKey,
		&v.SubmittedAt, &v.DecidedAt, &v.ReviewedBy, &v.ExpiresAt, &v.CreatedAt, &v.UpdatedAt}
}

// lapsed reports a verified row past its expiry (before housekeeping flips it).
func (v *verification) lapsed(now time.Time) bool {
	return v.Status == "verified" && v.ExpiresAt != nil && !v.ExpiresAt.After(now)
}

// current returns the row that speaks for the user (verified and unexpired,
// else newest; nil when none) and their rejected-attempt count.
func current(ctx context.Context, q db.Querier, userID uuid.UUID) (*verification, int, error) {
	var v verification
	err := q.QueryRow(ctx, `SELECT `+verificationCols+` FROM identity_verifications WHERE user_id = $1
		ORDER BY (status = 'verified' AND (expires_at IS NULL OR expires_at > now())) DESC, created_at DESC, id DESC
		LIMIT 1`, userID).Scan(v.targets()...)
	var cur *verification
	switch {
	case err == nil:
		cur = &v
	case !db.IsNoRows(err):
		return nil, 0, err
	}
	var rejected int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM identity_verifications WHERE user_id = $1 AND status = 'rejected'`,
		userID).Scan(&rejected); err != nil {
		return nil, 0, err
	}
	return cur, rejected, nil
}

// Status is GET /v1/identity/status (packages/shared/src/identity/types.ts).
type Status struct {
	Status       string     `json:"status"`        // none | pending | verified | rejected | expired
	Provider     string     `json:"provider"`      // persona | manual: the current attempt's, or what a new start uses
	Submitted    bool       `json:"submitted"`     // pending: the user finished their part; awaiting a decision
	Reason       string     `json:"reason"`        // rejected: why, for the user
	VerifiedAt   *time.Time `json:"verified_at"`   // verified
	ExpiresAt    *time.Time `json:"expires_at"`    // verified with IDENTITY_VERIFICATION_TTL
	AttemptsLeft int        `json:"attempts_left"` // starts left before the user must contact support
	UpdatedAt    *time.Time `json:"updated_at"`
}

func (s *Service) statusOf(v *verification, rejected int) Status {
	out := Status{Status: "none", Provider: s.mode(), AttemptsLeft: max(0, s.cfg.maxAttempts-rejected)}
	if v == nil {
		return out
	}
	out.Status, out.Provider, out.UpdatedAt = v.Status, v.Provider, &v.UpdatedAt
	switch {
	case v.lapsed(time.Now()):
		out.Status = "expired"
	case v.Status == "verified":
		out.VerifiedAt, out.ExpiresAt = v.DecidedAt, v.ExpiresAt
	case v.Status == "pending":
		out.Submitted = v.SubmittedAt != nil
	case v.Status == "rejected":
		out.Reason = v.Reason
	}
	if v.Status != "pending" || (v.Provider != s.mode() && v.SubmittedAt == nil) {
		out.Provider = s.mode() // the next start uses the configured provider
	}
	return out
}

func (s *Service) loadStatus(ctx context.Context, q db.Querier, userID uuid.UUID) (Status, error) {
	v, rejected, err := current(ctx, q, userID)
	if err != nil {
		return Status{}, err
	}
	return s.statusOf(v, rejected), nil
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) error {
	st, err := s.loadStatus(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

// Badge is GET /v1/identity/users/{id}/badge. Unknown users read as not
// verified, so the route never reveals whether an account exists.
type Badge struct {
	UserID   uuid.UUID `json:"user_id"`
	Verified bool      `json:"verified"`
}

func (s *Service) badge(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	ok, err := IsVerified(r.Context(), s.d.Pool, id)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpx.OK(w, Badge{UserID: id, Verified: ok})
	return nil
}

// ---- exported helpers for stitching ------------------------------------------------

// IsVerified reports whether the user holds a verified, unexpired identity
// verification. Gate features on it (e.g. require verification to sell):
//
//	if ok, err := identity.IsVerified(ctx, s.d.Pool, me.UserID); err != nil { return err } else if !ok {
//		return identity.ErrVerificationRequired
//	}
func IsVerified(ctx context.Context, q db.Querier, userID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM identity_verifications
		WHERE user_id = $1 AND status = 'verified' AND (expires_at IS NULL OR expires_at > now()))`, userID).Scan(&ok)
	return ok, err
}

// VerifiedSet returns which of ids are verified (for badges on lists).
func VerifiedSet(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	out := make(map[uuid.UUID]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT user_id FROM identity_verifications
		WHERE user_id = ANY($1) AND status = 'verified' AND (expires_at IS NULL OR expires_at > now())`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ErrVerificationRequired is the 403 for routes gated on IsVerified; clients
// branch on the code and open the verification screen.
var ErrVerificationRequired = httpx.NewError(http.StatusForbidden, "verification_required",
	"Verify your identity to continue.")
