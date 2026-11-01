package moderation

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
)

// SeedDev adds one block, an open case about a person with two reports, and
// a dismissed case, so the queue and the blocked list have something to
// show. users are admin, sam, jordan, priya (auth.DevIdentities order).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO moderation_blocks (blocker_id, blocked_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, priya, jordan); err != nil {
			return err
		}
		var seeded bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM moderation_cases WHERE target_kind = 'user' AND target_id = $1)`,
			jordan.String()).Scan(&seeded); err != nil || seeded {
			return err
		}
		snapOf := func(id uuid.UUID) ([]byte, error) {
			snap, err := s.userSnapshot(ctx, id.String())
			if err != nil {
				return nil, err
			}
			return json.Marshal(snap)
		}
		jordanSnap, err := snapOf(jordan)
		if err != nil {
			return err
		}
		priyaSnap, err := snapOf(priya)
		if err != nil {
			return err
		}

		var open uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO moderation_cases (target_kind, target_id, author_id, snapshot, report_count, created_at, last_reported_at)
			VALUES ('user', $1, $2, $3, 2, now() - interval '5 hours', now() - interval '2 hours') RETURNING id`,
			jordan.String(), jordan, jordanSnap).Scan(&open); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO moderation_reports (case_id, reporter_id, target_kind, target_id, reason, note, snapshot, created_at)
			VALUES ($1, $2, 'user', $4, 'harassment', 'Keeps messaging me after I asked him to stop.', $5, now() - interval '5 hours'),
			       ($1, $3, 'user', $4, 'spam', '', $5, now() - interval '2 hours')`,
			open, sam, priya, jordan.String(), jordanSnap); err != nil {
			return err
		}

		var closed uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO moderation_cases (target_kind, target_id, author_id, snapshot, report_count, status, action,
			  resolution_note, resolved_by, resolved_at, created_at, last_reported_at)
			VALUES ('user', $1, $2, $3, 1, 'dismissed', 'dismiss', 'Profile is fine.', $4,
			  now() - interval '2 days', now() - interval '3 days', now() - interval '3 days') RETURNING id`,
			priya.String(), priya, priyaSnap, admin).Scan(&closed); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO moderation_reports (case_id, reporter_id, target_kind, target_id, reason, note, snapshot,
			  outcome_notified_at, created_at)
			VALUES ($1, $2, 'user', $3, 'other', 'Fake profile?', $4, now() - interval '2 days', now() - interval '3 days')`,
			closed, jordan, priya.String(), priyaSnap)
		return err
	})
}
