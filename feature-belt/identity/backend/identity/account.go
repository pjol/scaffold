package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/notify"
)

// ---- account deletion / export ---------------------------------------------------

// DeleteAccount deletes every attempt. Review photos live under the user's
// storage prefix, which core removes after the transaction; provider-side
// inquiries are queued for redaction (housekeeping calls the provider).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO identity_redactions (provider, inquiry_id)
		SELECT provider, inquiry_id FROM identity_verifications WHERE user_id = $1 AND inquiry_id IS NOT NULL
		ON CONFLICT DO NOTHING`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM identity_verifications WHERE user_id = $1`, userID)
	return err
}

type exportAttempt struct {
	ID          uuid.UUID  `json:"id"`
	Provider    string     `json:"provider"`
	InquiryID   *string    `json:"inquiry_id"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason"`
	SubmittedAt *time.Time `json:"submitted_at"`
	DecidedAt   *time.Time `json:"decided_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ExportAccount returns the current status and every attempt.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	st, err := s.loadStatus(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, provider, inquiry_id, status, reason, submitted_at, decided_at,
		expires_at, created_at FROM identity_verifications WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	attempts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportAttempt])
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": st, "attempts": attempts}, nil
}

// ---- housekeeping ----------------------------------------------------------------

// abandonAfter is how long a manual attempt may wait for its photos before
// housekeeping expires it and deletes whatever was uploaded for it.
const abandonAfter = 24 * time.Hour

// housekeeping runs every 10 minutes on one instance: delete review photos of
// decided attempts, expire manual attempts abandoned before submission (and
// their uploads), lapse verifications past their expiry, and redact provider
// inquiries owed after account deletions.
func (s *Service) housekeeping(ctx context.Context) error {
	return errors.Join(s.purgeDocuments(ctx), s.expireAbandoned(ctx), s.expire(ctx), s.redact(ctx))
}

// purgeDocuments deletes the review photos of decided attempts, then clears
// the keys. With no attempt still open, every identity_document upload of the
// user goes (photos picked but never submitted included); while a newer attempt
// is open, only the decided attempt's own keys.
func (s *Service) purgeDocuments(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT v.id, v.user_id, v.document_key, v.selfie_key,
		       EXISTS (SELECT 1 FROM identity_verifications o WHERE o.user_id = v.user_id AND o.status = 'pending')
		FROM identity_verifications v
		WHERE v.status <> 'pending' AND (v.document_key IS NOT NULL OR v.selfie_key IS NOT NULL)
		ORDER BY v.decided_at NULLS FIRST LIMIT 200`)
	if err != nil {
		return err
	}
	type pending struct {
		ID          uuid.UUID
		UserID      uuid.UUID
		Doc, Selfie *string
		Open        bool
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pending])
	if err != nil {
		return err
	}
	var errs []error
	for _, p := range list {
		var derr error
		if p.Open {
			for _, k := range []*string{p.Doc, p.Selfie} {
				if k != nil {
					derr = errors.Join(derr, s.d.Storage.Delete(ctx, *k))
				}
			}
		} else {
			derr = s.d.Storage.DeleteOwnerPurpose(ctx, p.UserID, documentPurpose)
		}
		if derr != nil {
			errs = append(errs, fmt.Errorf("delete photos of %s: %w", p.ID, derr))
			continue
		}
		if _, err := s.d.Pool.Exec(ctx, `UPDATE identity_verifications SET document_key = NULL, selfie_key = NULL,
			updated_at = now() WHERE id = $1`, p.ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// expireAbandoned expires manual attempts that never got their photos within
// abandonAfter and deletes the user's identity_document uploads. A later
// submit fails ("Start verification first") and the user starts again.
func (s *Service) expireAbandoned(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		UPDATE identity_verifications SET status = 'expired', updated_at = now()
		WHERE provider = 'manual' AND status = 'pending' AND submitted_at IS NULL AND created_at < $1
		RETURNING user_id`, time.Now().Add(-abandonAfter))
	if err != nil {
		return err
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	var errs []error
	for _, u := range users {
		if err := s.d.Storage.DeleteOwnerPurpose(ctx, u, documentPurpose); err != nil {
			errs = append(errs, fmt.Errorf("delete abandoned uploads of %s: %w", u, err))
		}
		if st, err := s.loadStatus(ctx, s.d.Pool, u); err == nil {
			s.publish(ctx, u, st)
		}
	}
	return errors.Join(errs...)
}

// expire lapses verified attempts past expires_at and tells the user.
func (s *Service) expire(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `UPDATE identity_verifications SET status = 'expired', updated_at = now()
		WHERE status = 'verified' AND expires_at IS NOT NULL AND expires_at <= now() RETURNING user_id`)
	if err != nil {
		return err
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, u := range users {
		if ok, err := IsVerified(ctx, s.d.Pool, u); err != nil || ok {
			continue
		}
		if st, err := s.loadStatus(ctx, s.d.Pool, u); err == nil {
			s.publish(ctx, u, st)
		}
		if err := s.d.Notify.Send(ctx, notify.Notification{UserID: u, Kind: "identity.status",
			Title: "Verification expired", Body: "Verify your identity again to keep your badge.",
			Link: "/settings/identity", Data: map[string]any{"status": "expired"}}); err != nil {
			slog.WarnContext(ctx, "identity: notify expiry", "user_id", u, "err", err)
		}
	}
	return nil
}

// redact asks the provider to erase inquiries of deleted accounts (20 tries).
func (s *Service) redact(ctx context.Context) error {
	red, ok := s.provider.(Redactor)
	if !ok {
		return nil // manual mode, or the provider was removed: rows wait until it is configured again
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT inquiry_id FROM identity_redactions
		WHERE provider = $1 AND completed_at IS NULL AND attempts < 20 ORDER BY created_at LIMIT 50`, s.provider.Name())
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		rerr := red.Redact(ctx, id)
		msg := ""
		if rerr != nil {
			msg = rerr.Error()
			errs = append(errs, rerr)
		}
		if _, err := s.d.Pool.Exec(ctx, `UPDATE identity_redactions SET attempts = attempts + 1, last_error = $3,
			completed_at = CASE WHEN $3 = '' THEN now() END WHERE provider = $1 AND inquiry_id = $2`,
			s.provider.Name(), id, msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ---- agent tools -----------------------------------------------------------------

func (s *Service) Tools() []app.Tool {
	return []app.Tool{{
		Name:        "identity_status",
		Description: "The caller's identity verification status: none, pending (and whether it was submitted), verified, rejected with the reason, or expired.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		ReadOnly:    true,
		Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
			if caller == nil {
				return nil, errors.New("sign in to read verification status")
			}
			return s.loadStatus(ctx, s.d.Pool, caller.UserID)
		},
	}}
}

// ---- development data ------------------------------------------------------------

// SeedDev marks Jordan verified and Priya rejected through manual review (no
// photos are kept after a decision, so none are seeded). Sam and the admin
// start with no verification to try the flow.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	admin, jordan, priya := users[0], users[2], users[3]
	seed := []struct {
		user         uuid.UUID
		slug, status string
		reason       string
	}{
		{jordan, "jordan", "verified", ""},
		{priya, "priya", "rejected", "The ID photo was too blurry to read. Take it again in good light."},
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, r := range seed {
			id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("scaffold-dev-identity:"+r.slug))
			if _, err := tx.Exec(ctx, `
				INSERT INTO identity_verifications (id, user_id, provider, status, reason, submitted_at, decided_at, reviewed_by)
				VALUES ($1, $2, 'manual', $3, $4, now() - interval '2 days', now() - interval '1 day', $5)
				ON CONFLICT (id) DO NOTHING`, id, r.user, r.status, strings.TrimSpace(r.reason), admin); err != nil {
				return err
			}
		}
		return nil
	})
}
