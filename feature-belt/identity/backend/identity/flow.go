package identity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
	"scaffoldapp/backend/internal/webhooks"
)

// providerRejectedReason is shown when the provider declines an inquiry.
const providerRejectedReason = "We couldn't verify your identity. You can try again."

// StartResult is POST /v1/identity/start.
type StartResult struct {
	Status Status `json:"status"`
	// Flow is the provider flow to open (hosted URL or embedded session);
	// nil in manual mode and when there is nothing for the user to do.
	Flow *Flow `json:"flow"`
}

// Flow opens a provider inquiry.
type Flow struct {
	Provider      string `json:"provider"`
	InquiryID     string `json:"inquiry_id"`
	SessionToken  string `json:"session_token"`
	HostedURL     string `json:"hosted_url"`
	EnvironmentID string `json:"environment_id"`
}

type startReq struct {
	RedirectURI string `json:"redirect_uri"` // hosted flow return: a web origin of this app or scaffoldapp://
}

// start creates or resumes the caller's verification. Starts are serialised
// per user by a transaction-scoped advisory lock.
func (s *Service) start(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var req startReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	redirect, err := s.checkRedirect(req.RedirectURI)
	if err != nil {
		return err
	}
	var out StartResult
	retiredManual := false
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('identity:' || $1::text, 0))`, me.UserID); err != nil {
			return err
		}
		v, rejected, err := current(ctx, tx, me.UserID)
		if err != nil {
			return err
		}
		if v != nil {
			switch {
			case v.Status == "verified" && !v.lapsed(time.Now()):
				out.Status = s.statusOf(v, rejected)
				return nil
			case v.Status == "pending" && v.SubmittedAt != nil:
				out.Status = s.statusOf(v, rejected) // waiting on a decision
				return nil
			case v.Status == "pending" && v.Provider == s.mode():
				if s.provider == nil {
					out.Status = s.statusOf(v, rejected) // manual: the client shows the upload form
					return nil
				}
				if v.InquiryID != nil {
					return s.resume(ctx, tx, v, redirect, rejected, &out)
				}
				if _, err := tx.Exec(ctx, `UPDATE identity_verifications SET status = 'expired', updated_at = now()
					WHERE id = $1`, v.ID); err != nil {
					return err
				}
			case v.Status == "expired" && s.provider != nil && v.Provider == s.mode() &&
				v.InquiryID != nil && v.SubmittedAt == nil && v.ExpiresAt == nil:
				return s.resume(ctx, tx, v, redirect, rejected, &out) // an abandoned inquiry revives
			case v.Status == "pending":
				// The provider changed since this attempt began: retire it.
				if _, err := tx.Exec(ctx, `UPDATE identity_verifications SET status = 'expired', updated_at = now()
					WHERE id = $1`, v.ID); err != nil {
					return err
				}
				retiredManual = v.Provider == "manual"
			}
		}
		if rejected >= s.cfg.maxAttempts {
			return httpx.NewError(http.StatusForbidden, "attempts_exhausted", fmt.Sprintf(
				"Verification didn't succeed after %d attempts. Contact %s to finish.", s.cfg.maxAttempts, s.d.Cfg.SupportEmail))
		}
		var inq *Inquiry
		var inquiryID *string
		if s.provider != nil {
			if inq, err = s.provider.StartInquiry(ctx, StartInput{UserID: me.UserID, RedirectURI: redirect}); err != nil {
				return providerFailure{err}
			}
			inquiryID = &inq.ID
		}
		var nv verification
		if err := tx.QueryRow(ctx, `INSERT INTO identity_verifications (user_id, provider, inquiry_id)
			VALUES ($1, $2, $3) RETURNING `+verificationCols, me.UserID, s.mode(), inquiryID).Scan(nv.targets()...); err != nil {
			return err
		}
		if inq != nil && inq.State == stateSubmitted {
			if err := tx.QueryRow(ctx, `UPDATE identity_verifications SET submitted_at = now() WHERE id = $1
				RETURNING submitted_at`, nv.ID).Scan(&nv.SubmittedAt); err != nil {
				return err
			}
		}
		out.Status = s.statusOf(&nv, rejected)
		out.Flow = s.flow(inq)
		return nil
	})
	if err != nil {
		return providerError(ctx, err)
	}
	if retiredManual {
		// Photos picked for the retired manual attempt can never be submitted now.
		if err := s.d.Storage.DeleteOwnerPurpose(context.WithoutCancel(ctx), me.UserID, documentPurpose); err != nil {
			slog.WarnContext(ctx, "identity: delete retired uploads", "user_id", me.UserID, "err", err)
		}
	}
	httpx.OK(w, out)
	return nil
}

// resume opens a new session on the attempt's inquiry.
func (s *Service) resume(ctx context.Context, tx pgx.Tx, v *verification, redirect string, rejected int, out *StartResult) error {
	inq, err := s.provider.StartInquiry(ctx, StartInput{UserID: v.UserID, InquiryID: *v.InquiryID, RedirectURI: redirect})
	if err != nil {
		return providerFailure{err}
	}
	if _, err := s.transition(ctx, tx, v, change{state: inq.State}); err != nil {
		return err
	}
	out.Status = s.statusOf(v, rejected)
	if v.Status == "pending" && v.SubmittedAt == nil {
		out.Flow = s.flow(inq)
	}
	return nil
}

func (s *Service) flow(inq *Inquiry) *Flow {
	if inq == nil {
		return nil
	}
	return &Flow{Provider: s.mode(), InquiryID: inq.ID, SessionToken: inq.SessionToken, HostedURL: inq.HostedURL,
		EnvironmentID: s.cfg.environmentID}
}

// providerFailure marks an error from the identity provider.
type providerFailure struct{ err error }

func (p providerFailure) Error() string { return p.err.Error() }
func (p providerFailure) Unwrap() error { return p.err }

// providerError hides provider failures behind a retryable 503.
func providerError(ctx context.Context, err error) error {
	var pf providerFailure
	if errors.As(err, &pf) {
		slog.ErrorContext(ctx, "identity: provider call failed", "err", pf.err)
		return httpx.NewError(http.StatusServiceUnavailable, "provider_unavailable",
			"Identity verification is unavailable right now. Try again shortly.")
	}
	return err
}

// checkRedirect allows a hosted-flow return to one of this app's web origins
// or its mobile scheme.
func (s *Service) checkRedirect(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 {
		return "", httpx.BadRequest("redirect_uri isn't a valid URL.")
	}
	if u.Scheme == appScheme {
		return raw, nil
	}
	for _, origin := range s.d.Cfg.WebOrigins() {
		if auth.NormalizeOrigin(origin) != "" && auth.NormalizeOrigin(raw) == auth.NormalizeOrigin(origin) {
			return raw, nil
		}
	}
	return "", httpx.BadRequest("redirect_uri must point at this app.")
}

// ---- manual review -----------------------------------------------------------------

type documentsReq struct {
	DocumentKey string `json:"document_key"` // photo of a government ID
	SelfieKey   string `json:"selfie_key"`
}

// submitDocuments is POST /v1/identity/documents (manual mode): the caller
// uploaded both photos with POST /v1/uploads (purpose identity_document).
func (s *Service) submitDocuments(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var req documentsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !storage.KeyOwnedBy(req.DocumentKey, documentPurpose, me.UserID) ||
		!storage.KeyOwnedBy(req.SelfieKey, documentPurpose, me.UserID) || req.DocumentKey == req.SelfieKey {
		return httpx.BadRequest("Upload the ID photo and the selfie first (purpose identity_document), then send both keys.")
	}
	var id uuid.UUID
	var st Status
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('identity:' || $1::text, 0))`, me.UserID); err != nil {
			return err
		}
		v, rejected, err := current(ctx, tx, me.UserID)
		if err != nil {
			return err
		}
		if v == nil || v.Status != "pending" || v.Provider != "manual" {
			return httpx.Conflict("Start verification first.")
		}
		if v.SubmittedAt != nil {
			return httpx.Conflict("Your photos are already with our team.")
		}
		if err := tx.QueryRow(ctx, `UPDATE identity_verifications SET document_key = $2, selfie_key = $3,
			submitted_at = now(), updated_at = now() WHERE id = $1 RETURNING `+verificationCols,
			v.ID, req.DocumentKey, req.SelfieKey).Scan(v.targets()...); err != nil {
			return err
		}
		id, st = v.ID, s.statusOf(v, rejected)
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, me.UserID, st)
	s.announce(ctx, id)
	httpx.OK(w, st)
	return nil
}

// ---- Persona webhook ---------------------------------------------------------------

// personaWebhook is POST /webhooks/persona. It verifies the signature,
// re-fetches the inquiry's current status from Persona (so retried or
// out-of-order deliveries can't regress it) and applies it once per event.
// Staff decisions are final: the webhook never overrides them.
func (s *Service) personaWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Unreadable body."))
		return
	}
	if s.provider == nil || s.provider.Name() != "persona" {
		httpx.Fail(w, r, httpx.NewError(http.StatusUnauthorized, "bad_signature", "Signature check failed."))
		return
	}
	ev, err := s.provider.ParseWebhook(r.Header, body)
	if errors.Is(err, ErrBadSignature) {
		slog.WarnContext(r.Context(), "identity: persona webhook rejected")
		httpx.Fail(w, r, httpx.NewError(http.StatusUnauthorized, "bad_signature", "Signature check failed."))
		return
	}
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Unexpected payload."))
		return
	}
	ctx := context.WithoutCancel(r.Context())
	if err := s.applyEvent(ctx, ev); err != nil {
		httpx.Fail(w, r, err) // 5xx: Persona retries the delivery
		return
	}
	httpx.OK(w, map[string]bool{"received": true})
}

func (s *Service) applyEvent(ctx context.Context, ev *WebhookEvent) error {
	if ev.InquiryID == "" {
		return nil
	}
	var known bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM identity_verifications
		WHERE provider = $1 AND inquiry_id = $2)`, s.provider.Name(), ev.InquiryID).Scan(&known); err != nil {
		return err
	}
	if !known {
		return nil // an inquiry this app didn't start (another environment, a deleted account)
	}
	inq, err := s.provider.FetchInquiry(ctx, ev.InquiryID)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var res result
	_, err = webhooks.Once(ctx, s.d.Pool, s.provider.Name(), ev.ID, func(tx pgx.Tx) error {
		var v verification
		if err := tx.QueryRow(ctx, `SELECT `+verificationCols+` FROM identity_verifications
			WHERE provider = $1 AND inquiry_id = $2 FOR UPDATE`, s.provider.Name(), ev.InquiryID).Scan(v.targets()...); err != nil {
			return err
		}
		if v.ReviewedBy != nil {
			return nil
		}
		var err error
		res, err = s.transition(ctx, tx, &v, change{state: inq.State})
		return err
	})
	if err != nil {
		return err
	}
	s.after(ctx, res)
	return nil
}

// ---- transitions -------------------------------------------------------------------

// change is a requested state for one attempt.
type change struct {
	state    string     // provider-neutral state constant
	reason   string     // rejected: user-facing reason ("" = default)
	reviewer *uuid.UUID // staff decisions
}

// result says what to do after a transition commits.
type result struct {
	userID    uuid.UUID
	changed   bool
	decided   string // verified | rejected when this change decided the attempt
	reason    string
	submitted uuid.UUID // attempt newly awaiting review
}

// transition moves a locked attempt to c.state and updates v in place.
func (s *Service) transition(ctx context.Context, tx pgx.Tx, v *verification, c change) (result, error) {
	res := result{userID: v.UserID}
	if c.reviewer == nil && v.Status == "expired" && v.ExpiresAt != nil {
		return res, nil // a lapsed verification stays lapsed; the user verifies again
	}
	status, submitted, decided := v.Status, v.SubmittedAt != nil, false
	var reason string
	var expires *time.Time
	switch c.state {
	case statePending:
		if v.Status == "verified" || v.Status == "rejected" {
			return res, nil // a decided attempt never reopens
		}
		status = "pending"
	case stateSubmitted:
		if v.Status != "pending" && v.Status != "expired" {
			return res, nil
		}
		status, submitted = "pending", true
	case stateVerified:
		status, decided = "verified", true
		if s.cfg.ttl > 0 {
			t := time.Now().Add(s.cfg.ttl)
			expires = &t
		}
	case stateRejected:
		status, decided = "rejected", true
		reason = c.reason
		if reason == "" {
			reason = providerRejectedReason
		}
	case stateExpired:
		if v.Status != "pending" {
			return res, nil
		}
		status = "expired"
	default:
		return res, nil // a status this code doesn't know: leave the attempt alone
	}
	if status == v.Status && submitted == (v.SubmittedAt != nil) && !decided {
		return res, nil
	}
	if decided && status == v.Status && c.reviewer == nil {
		return res, nil // provider repeating its decision
	}
	err := tx.QueryRow(ctx, `
		UPDATE identity_verifications SET
		  status = $2,
		  submitted_at = CASE WHEN $3 THEN COALESCE(submitted_at, now()) ELSE submitted_at END,
		  decided_at = CASE WHEN $4 THEN now() ELSE decided_at END,
		  reason = CASE WHEN $4 OR $2 = 'pending' THEN $5 ELSE reason END,
		  expires_at = CASE WHEN $4 THEN $6 ELSE expires_at END,
		  reviewed_by = COALESCE($7, reviewed_by),
		  updated_at = now()
		WHERE id = $1 RETURNING `+verificationCols,
		v.ID, status, submitted, decided, reason, expires, c.reviewer).Scan(v.targets()...)
	if err != nil {
		return res, err
	}
	res.changed = true
	if decided {
		res.decided, res.reason = status, reason
	}
	if submitted && status == "pending" && !decided {
		res.submitted = v.ID
	}
	return res, nil
}

// after runs a committed transition's side effects, best-effort: delete
// review photos, tell the user, refresh the admin queue.
func (s *Service) after(ctx context.Context, res result) {
	if !res.changed {
		return
	}
	if res.decided != "" {
		if err := s.purgeDocuments(ctx); err != nil {
			slog.WarnContext(ctx, "identity: delete review photos (housekeeping retries)", "err", err)
		}
	}
	st, err := s.loadStatus(ctx, s.d.Pool, res.userID)
	if err != nil {
		slog.WarnContext(ctx, "identity: load status", "user_id", res.userID, "err", err)
		return
	}
	s.publish(ctx, res.userID, st)
	if res.submitted != uuid.Nil {
		s.announce(ctx, res.submitted)
	}
	if res.decided != "" {
		n := notify.Notification{UserID: res.userID, Kind: "identity.status", Link: "/settings/identity",
			Data: map[string]any{"status": res.decided}}
		if res.decided == "verified" {
			n.Title, n.Body = "Identity verified", "Your identity is verified."
		} else {
			n.Title, n.Body = "Identity not verified", res.reason
		}
		if err := s.d.Notify.Send(ctx, n); err != nil {
			slog.WarnContext(ctx, "identity: notify", "user_id", res.userID, "err", err)
		}
	}
}

// publish sends the new status on the user's topic (event identity.status).
func (s *Service) publish(ctx context.Context, userID uuid.UUID, st Status) {
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), realtime.UserTopic(userID), "identity.status", st); err != nil {
		slog.WarnContext(ctx, "identity: publish status", "user_id", userID, "err", err)
	}
}

// announce tells the admin console an attempt awaits review.
func (s *Service) announce(ctx context.Context, id uuid.UUID) {
	if err := s.d.Broker.Publish(context.WithoutCancel(ctx), realtime.AdminLiveTopic, "identity.submitted",
		map[string]uuid.UUID{"id": id}); err != nil {
		slog.WarnContext(ctx, "identity: publish submitted", "verification_id", id, "err", err)
	}
}
