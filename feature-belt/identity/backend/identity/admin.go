package identity

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// documentURLTTL is how long a reviewer's photo links work.
const documentURLTTL = 5 * time.Minute

// AdminVerification is an attempt as staff see it.
type AdminVerification struct {
	ID           uuid.UUID  `json:"id"`
	User         AdminUser  `json:"user"`
	Provider     string     `json:"provider"`
	InquiryID    *string    `json:"inquiry_id"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason"`
	SubmittedAt  *time.Time `json:"submitted_at"`
	DecidedAt    *time.Time `json:"decided_at"`
	ReviewedBy   *uuid.UUID `json:"reviewed_by"`
	ExpiresAt    *time.Time `json:"expires_at"`
	HasDocuments bool       `json:"has_documents"`
	ProviderURL  *string    `json:"provider_url"` // the inquiry in the provider's dashboard
	CreatedAt    time.Time  `json:"created_at"`
}

// AdminUser identifies the person being verified.
type AdminUser struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

// AdminVerificationDetail adds short-lived links to manual-review photos.
type AdminVerificationDetail struct {
	AdminVerification
	DocumentURL  *string    `json:"document_url"`
	SelfieURL    *string    `json:"selfie_url"`
	URLsExpireAt *time.Time `json:"urls_expire_at"`
}

const adminCols = `v.id, v.user_id, u.email, u.display_name, v.provider, v.inquiry_id, v.status, v.reason, v.submitted_at,
	v.decided_at, v.reviewed_by, v.expires_at, v.document_key, v.selfie_key, v.created_at`

func scanAdmin(row pgx.Row) (AdminVerification, *string, *string, error) {
	var a AdminVerification
	var doc, selfie *string
	err := row.Scan(&a.ID, &a.User.ID, &a.User.Email, &a.User.DisplayName, &a.Provider, &a.InquiryID, &a.Status,
		&a.Reason, &a.SubmittedAt, &a.DecidedAt, &a.ReviewedBy, &a.ExpiresAt, &doc, &selfie, &a.CreatedAt)
	a.HasDocuments = doc != nil || selfie != nil
	if a.Provider == "persona" && a.InquiryID != nil {
		u := personaDashboardURL + *a.InquiryID
		a.ProviderURL = &u
	}
	return a, doc, selfie, err
}

type adminCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// adminList is GET /v1/admin/identity/verifications?status=. The default is
// the review queue: pending attempts the user has submitted, oldest first.
// status=pending|verified|rejected|expired lists that status (pending
// includes unsubmitted attempts), newest first except pending.
func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	queue := status == ""
	switch status {
	case "":
		status = "pending"
	case "pending", "verified", "rejected", "expired":
	default:
		return httpx.BadRequest("status must be pending, verified, rejected or expired.")
	}
	p := httpx.ParsePage(r)
	var c adminCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	order, cmp := "DESC", "<"
	if status == "pending" {
		order, cmp = "ASC", ">"
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+adminCols+`
		FROM identity_verifications v JOIN users u ON u.id = v.user_id
		WHERE v.status = $1 AND (NOT $2 OR v.submitted_at IS NOT NULL)
		  AND ($3::timestamptz IS NULL OR (v.created_at, v.id) `+cmp+` ($3, $4))
		ORDER BY v.created_at `+order+`, v.id `+order+` LIMIT $5`, status, queue, after, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminVerification, error) {
		a, _, _, err := scanAdmin(row)
		return a, err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a AdminVerification) any { return adminCursor{a.CreatedAt, a.ID} }))
	return nil
}

// adminGet returns one attempt; for manual review it signs 5-minute links to
// the photos and records the view in the audit log.
func (s *Service) adminGet(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, doc, selfie, err := scanAdmin(s.d.Pool.QueryRow(ctx, `SELECT `+adminCols+`
		FROM identity_verifications v JOIN users u ON u.id = v.user_id WHERE v.id = $1`, id))
	if db.IsNoRows(err) {
		return httpx.NotFound("Verification not found.")
	}
	if err != nil {
		return err
	}
	out := AdminVerificationDetail{AdminVerification: a}
	if doc != nil || selfie != nil {
		if err := audit.Log(ctx, s.d.Pool, staff.UserID, "identity.documents.view", "identity_verification", id.String(),
			map[string]any{"user_id": a.User.ID}); err != nil {
			return err
		}
		sign := func(key *string) (*string, error) {
			if key == nil {
				return nil, nil
			}
			u, err := s.d.Storage.PresignGet(ctx, *key, documentURLTTL)
			return &u, err
		}
		if out.DocumentURL, err = sign(doc); err != nil {
			return err
		}
		if out.SelfieURL, err = sign(selfie); err != nil {
			return err
		}
		exp := time.Now().Add(documentURLTTL).UTC()
		out.URLsExpireAt = &exp
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, out)
	return nil
}

func (s *Service) adminApprove(w http.ResponseWriter, r *http.Request) error {
	return s.decide(w, r, change{state: stateVerified})
}

type rejectReq struct {
	Reason string `json:"reason"`
}

// adminReject declines a submitted attempt or revokes a verified one.
func (s *Service) adminReject(w http.ResponseWriter, r *http.Request) error {
	var req rejectReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	reason := strings.TrimSpace(req.Reason)
	if n := utf8.RuneCountInString(reason); n < 3 || n > 500 {
		return httpx.BadRequest("Give a reason of 3 to 500 characters. The user sees it.")
	}
	return s.decide(w, r, change{state: stateRejected, reason: reason})
}

// decide applies a staff decision with an audit row; review photos are
// deleted right after it commits.
func (s *Service) decide(w http.ResponseWriter, r *http.Request, c change) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	c.reviewer = &staff.UserID
	var res result
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var v verification
		err := tx.QueryRow(ctx, `SELECT `+verificationCols+` FROM identity_verifications WHERE id = $1 FOR UPDATE`, id).
			Scan(v.targets()...)
		if db.IsNoRows(err) {
			return httpx.NotFound("Verification not found.")
		}
		if err != nil {
			return err
		}
		if v.UserID == staff.UserID {
			return httpx.Forbidden("Another staff member has to review your own verification.")
		}
		from := v.Status
		switch {
		case c.state == stateVerified && (v.Status != "pending" || v.SubmittedAt == nil):
			return httpx.Conflict("Only a submitted, pending verification can be approved.")
		case c.state == stateRejected && !(v.Status == "pending" && v.SubmittedAt != nil) && v.Status != "verified":
			return httpx.Conflict("Only a submitted or verified verification can be rejected.")
		}
		if res, err = s.transition(ctx, tx, &v, c); err != nil {
			return err
		}
		action := map[string]string{stateVerified: "identity.approve", stateRejected: "identity.reject"}[c.state]
		data := map[string]any{"from": from, "user_id": v.UserID, "provider": v.Provider}
		if c.reason != "" {
			data["reason"] = c.reason
		}
		return audit.Log(ctx, tx, staff.UserID, action, "identity_verification", id.String(), data)
	})
	if err != nil {
		return err
	}
	s.after(context.WithoutCancel(ctx), res)
	a, _, _, err := scanAdmin(s.d.Pool.QueryRow(ctx, `SELECT `+adminCols+`
		FROM identity_verifications v JOIN users u ON u.id = v.user_id WHERE v.id = $1`, id))
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}
