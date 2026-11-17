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
	for _, q := range []string{
		`DELETE FROM identity_verifications WHERE user_id = $1`,
		`DELETE FROM identity_badges WHERE user_id = $1`,
		`DELETE FROM identity_badge_applications WHERE user_id = $1`,
		`DELETE FROM identity_domains WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
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

type exportBadge struct {
	Kind          string     `json:"kind"`
	Label         string     `json:"label"`
	Category      *string    `json:"category"`
	Source        string     `json:"source"`
	GrantedAt     time.Time  `json:"granted_at"`
	ExpiresAt     *time.Time `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	RevokedReason string     `json:"revoked_reason"`
}

// ExportAccount returns the current status, every attempt, and every badge,
// badge application and domain proof (ended ones included).
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
	brows, err := s.d.Pool.Query(ctx, `SELECT kind, label, category, source, granted_at, expires_at, revoked_at, revoked_reason
		FROM identity_badges WHERE user_id = $1 ORDER BY granted_at`, userID)
	if err != nil {
		return nil, err
	}
	badges, err := pgx.CollectRows(brows, pgx.RowToStructByPos[exportBadge])
	if err != nil {
		return nil, err
	}
	apps, err := applicationsOf(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	domains, err := domainsOf(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": st, "attempts": attempts, "badges": nonNilSlice(badges),
		"badge_applications": apps, "domains": domains}, nil
}

func nonNilSlice[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// ---- housekeeping ----------------------------------------------------------------

// abandonAfter is how long a manual attempt may wait for its photos before
// housekeeping expires it and deletes whatever was uploaded for it.
const abandonAfter = 24 * time.Hour

// housekeeping runs every 10 minutes on one instance: delete review photos of
// decided attempts, expire manual attempts abandoned before submission (and
// their uploads), lapse verifications past their expiry, redact provider
// inquiries owed after account deletions, end badges past their expiry, and
// show paid badges withheld until their holders became eligible.
func (s *Service) housekeeping(ctx context.Context) error {
	return errors.Join(s.purgeDocuments(ctx), s.expireAbandoned(ctx), s.expire(ctx), s.redact(ctx),
		s.expireBadges(ctx), s.grantWithheldPaid(ctx))
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
	}, {
		Name: "identity_badges",
		Description: "The caller's verification badges (official, org, notable, paid) with their expiry, whether they can get " +
			"the paid badge and what's missing, their badge applications with staff notes, and their domain proofs with the DNS TXT record to add.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}, "additionalProperties": false}`),
		ReadOnly:    true,
		Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
			if caller == nil {
				return nil, errors.New("sign in to read badges")
			}
			return s.badgesStatus(ctx, s.d.Pool, caller.UserID)
		},
	}}
}

// ---- development data ------------------------------------------------------------

// SeedDev marks Jordan verified and Priya rejected through manual review (no
// photos are kept after a decision, so none are seeded). Sam and the admin
// start with no verification to try the flow. Badges: the admin holds the
// official badge, Jordan an org badge from an approved application with a
// proved domain, and Priya's notable application waits in the queue.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	if err := s.seedBadges(ctx, users); err != nil {
		return err
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

// seedBadges adds the development badges (idempotent: fixed ids, no updates).
func (s *Service) seedBadges(ctx context.Context, users []uuid.UUID) error {
	admin, jordan, priya := users[0], users[2], users[3]
	var org, notable *Category
	for i := range s.badges.categories {
		c := &s.badges.categories[i]
		if c.Kind == BadgeOrg && org == nil {
			org = c
		}
		if c.Kind == BadgeNotable && notable == nil {
			notable = c
		}
	}
	id := func(name string) uuid.UUID {
		return uuid.NewSHA1(uuid.NameSpaceURL, []byte("scaffold-dev-identity-badge:"+name))
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO identity_badges (user_id, kind, label, source, granted_by, granted_at)
			VALUES ($1, 'official', 'Scaffold App team', 'admin', $1, now() - interval '90 days')
			ON CONFLICT (user_id, kind) DO NOTHING`, admin); err != nil {
			return err
		}
		if org != nil {
			ev := `[{"kind": "domain", "ref": "riversidebikes.example", "verified": true},
				{"kind": "url", "ref": "https://maps.example/riverside-bikes", "verified": false}]`
			if _, err := tx.Exec(ctx, `INSERT INTO identity_domains (user_id, domain, token, verified_at, checked_at)
				VALUES ($1, 'riversidebikes.example', 'devtoken0riverside', now() - interval '40 days', now() - interval '40 days')
				ON CONFLICT (user_id, domain) DO NOTHING`, jordan); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO identity_badge_applications (id, user_id, category, display_label, status, evidence,
				  note, reviewer_note, reviewed_by, submitted_at, decided_at, created_at)
				VALUES ($1, $2, $3, 'Riverside Bikes', 'approved', $4, 'I run the shop with my sister.', '', $5,
				  now() - interval '40 days', now() - interval '38 days', now() - interval '40 days')
				ON CONFLICT DO NOTHING`, id("jordan-org"), jordan, org.Key, ev, admin); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO identity_badges (user_id, kind, label, category, source, application_id,
				  granted_by, granted_at, expires_at)
				VALUES ($1, 'org', 'Riverside Bikes', $2, 'review', $3, $4, now() - interval '38 days', now() + interval '327 days')
				ON CONFLICT (user_id, kind) DO NOTHING`, jordan, org.Key, id("jordan-org"), admin); err != nil {
				return err
			}
		}
		if notable != nil {
			ev := `[{"kind": "url", "ref": "https://video.example/@priyacooks", "verified": false},
				{"kind": "text", "ref": "Weekly cooking show, 120k subscribers.", "verified": false}]`
			if _, err := tx.Exec(ctx, `INSERT INTO identity_badge_applications (id, user_id, category, display_label, evidence, note,
				  submitted_at, created_at)
				VALUES ($1, $2, $3, 'Priya Cooks', $4, 'Happy to send anything else you need.', now() - interval '2 days', now() - interval '2 days')
				ON CONFLICT DO NOTHING`, id("priya-notable"), priya, notable.Key, ev); err != nil {
				return err
			}
		}
		return nil
	})
}
