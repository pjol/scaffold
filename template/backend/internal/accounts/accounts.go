// Package accounts owns the signed-in user's own surface: profile, consent,
// uploads, data export and account deletion, plus the public client config.
package accounts

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/legal"
	"scaffoldapp/backend/internal/storage"
)

type Service struct {
	d   *app.Deps
	cfg settings
}

type settings struct {
	minIOS, minAndroid string
}

func New(d *app.Deps) *Service {
	return &Service{d: d, cfg: settings{
		minIOS:     config.String("MOBILE_MIN_VERSION_IOS", "1.0.0"),
		minAndroid: config.String("MOBILE_MIN_VERSION_ANDROID", "1.0.0"),
	}}
}

func (s *Service) Name() string { return "accounts" }

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/config", httpx.Handler(s.clientConfig))
}

func (s *Service) MountUser(r chi.Router) {
	r.Get("/me", httpx.Handler(s.me))
	r.With(auth.RequireActive).Patch("/me", httpx.Handler(s.updateMe))
	r.Post("/me/consents", httpx.Handler(s.acceptConsents))
	r.Get("/me/export", httpx.Handler(s.export))
	r.With(auth.RateLimit(3, time.Hour)).Delete("/me", httpx.Handler(s.deleteMe))
	r.With(auth.RequireActive, auth.RateLimit(60, time.Minute)).Post("/uploads", httpx.Handler(s.presignUpload))
}

func (s *Service) MountWebhooks(r chi.Router) {
	r.Post("/clerk", s.clerkWebhook)
}

func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "accounts.finish_deletions", 10*time.Minute, s.finishDeletions)
}

// ---- client config ---------------------------------------------------------

// ClientConfig is GET /v1/config: what clients need before sign-in.
type ClientConfig struct {
	AppName        string            `json:"app_name"`
	TermsVersion   string            `json:"terms_version"`
	PrivacyVersion string            `json:"privacy_version"`
	SupportEmail   string            `json:"support_email"`
	MinAppVersion  map[string]string `json:"min_app_version"` // ios, android: older builds must update
	Modules        []string          `json:"modules"`
}

func (s *Service) clientConfig(w http.ResponseWriter, r *http.Request) error {
	mods := make([]string, 0, len(s.d.Modules))
	for _, m := range s.d.Modules {
		mods = append(mods, m.Name())
	}
	httpx.OK(w, ClientConfig{
		AppName:        "Scaffold App",
		TermsVersion:   legal.TermsVersion,
		PrivacyVersion: legal.PrivacyVersion,
		SupportEmail:   s.d.Cfg.SupportEmail,
		MinAppVersion:  map[string]string{"ios": s.cfg.minIOS, "android": s.cfg.minAndroid},
		Modules:        mods,
	})
	return nil
}

// ---- me --------------------------------------------------------------------

type Me struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	AdminRole   *string   `json:"admin_role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	Consent     Consent   `json:"consent"`
}

type Consent struct {
	Required        bool   `json:"required"`
	TermsVersion    string `json:"terms_version"`
	PrivacyVersion  string `json:"privacy_version"`
	AcceptedTerms   string `json:"accepted_terms"`
	AcceptedPrivacy string `json:"accepted_privacy"`
}

func (s *Service) loadMe(ctx context.Context, id uuid.UUID) (*Me, error) {
	var m Me
	err := s.d.Pool.QueryRow(ctx, `
		SELECT id, email, display_name, avatar_url, admin_role, status, created_at,
		       COALESCE(terms_version_accepted, ''), COALESCE(privacy_version_accepted, '')
		FROM users WHERE id = $1`, id,
	).Scan(&m.ID, &m.Email, &m.DisplayName, &m.AvatarURL, &m.AdminRole, &m.Status, &m.CreatedAt,
		&m.Consent.AcceptedTerms, &m.Consent.AcceptedPrivacy)
	if err != nil {
		return nil, err
	}
	m.Consent.TermsVersion, m.Consent.PrivacyVersion = legal.TermsVersion, legal.PrivacyVersion
	m.Consent.Required = !legal.Current(m.Consent.AcceptedTerms, m.Consent.AcceptedPrivacy)
	return &m, nil
}

func (s *Service) me(w http.ResponseWriter, r *http.Request) error {
	m, err := s.loadMe(r.Context(), auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, m)
	return nil
}

type updateMeReq struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

func (s *Service) updateMe(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req updateMeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if n := len([]rune(name)); n == 0 || n > 60 {
			return httpx.BadRequest("Name must be 1 to 60 characters.")
		}
		req.DisplayName = &name
	}
	if req.AvatarURL != nil && *req.AvatarURL != "" &&
		!s.d.Storage.AvatarURLAllowed(*req.AvatarURL, id.UserID, s.d.Cfg.IsDevelopment()) {
		return httpx.BadRequest("Upload the photo first, then save its URL.")
	}
	_, err := s.d.Pool.Exec(r.Context(), `
		UPDATE users SET
		  display_name = COALESCE($2, display_name),
		  avatar_url   = CASE WHEN $3::text IS NULL THEN avatar_url ELSE NULLIF($3, '') END,
		  updated_at   = now()
		WHERE id = $1`, id.UserID, req.DisplayName, req.AvatarURL)
	if err != nil {
		return err
	}
	return s.me(w, r)
}

type consentReq struct {
	TermsVersion   string `json:"terms_version"`
	PrivacyVersion string `json:"privacy_version"`
	Platform       string `json:"platform"`
}

func (s *Service) acceptConsents(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req consentReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.TermsVersion != legal.TermsVersion || req.PrivacyVersion != legal.PrivacyVersion {
		return httpx.Conflict("The documents changed while you were reading. Review the latest versions.")
	}
	switch req.Platform {
	case "web", "ios", "android":
	default:
		req.Platform = ""
	}
	ip := httpx.ClientIPFrom(r)
	tx, err := s.d.Pool.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck
	if _, err := tx.Exec(r.Context(), `
		INSERT INTO legal_acceptances (user_id, document, version, platform, ip)
		VALUES ($1, 'terms', $2, $4, $5), ($1, 'privacy', $3, $4, $5)`,
		id.UserID, req.TermsVersion, req.PrivacyVersion, req.Platform, ip); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE users SET terms_version_accepted = $2, privacy_version_accepted = $3, updated_at = now()
		WHERE id = $1`, id.UserID, req.TermsVersion, req.PrivacyVersion); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return s.me(w, r)
}

// ---- uploads ---------------------------------------------------------------

type uploadReq struct {
	Purpose     string `json:"purpose"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// presignUpload returns a presigned PUT. The client uploads the bytes, then
// hands the key (or public URL) to the owning endpoint, which must check
// storage.KeyOwnedBy before trusting it.
func (s *Service) presignUpload(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req uploadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !legal.Current(id.TermsVersion, id.PrivacyVersion) && !storage.IsConsentExempt(req.Purpose) {
		return legal.ErrConsentRequired()
	}
	up, err := s.d.Storage.PresignPut(r.Context(), req.Purpose, id.UserID, req.ContentType, req.Size)
	if err != nil {
		return httpx.BadRequest(err.Error())
	}
	httpx.OK(w, up)
	return nil
}
