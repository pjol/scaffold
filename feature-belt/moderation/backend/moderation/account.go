package moderation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount runs inside the account-deletion transaction:
//   - reports the user filed stay (other people's cases need them) but lose
//     the link to the reporter;
//   - reports and cases about the user's content stay as safety records with
//     the user's text and media scrubbed from every snapshot, and open cases
//     about them close (the content is gone with the account);
//   - every block involving the user is removed.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE moderation_reports SET reporter_id = NULL, updated_at = now() WHERE reporter_id = $1`, userID); err != nil {
		return err
	}
	const scrub = `snapshot || jsonb_build_object('text', '', 'media_keys', '[]'::jsonb, 'redacted', true)`
	if _, err := tx.Exec(ctx, `
		UPDATE moderation_reports SET snapshot = `+scrub+`, updated_at = now()
		WHERE case_id IN (SELECT id FROM moderation_cases WHERE author_id = $1)`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE moderation_cases SET snapshot = `+scrub+`,
		  status = CASE WHEN status = 'open' THEN 'resolved' ELSE status END,
		  action = CASE WHEN status = 'open' THEN 'resolve' ELSE action END,
		  resolution_note = CASE WHEN status = 'open' THEN 'Closed automatically: the author deleted their account.'
		                         ELSE resolution_note END,
		  resolved_at = CASE WHEN status = 'open' THEN now() ELSE resolved_at END,
		  updated_at = now()
		WHERE author_id = $1`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM moderation_blocks WHERE blocker_id = $1 OR blocked_id = $1`, userID)
	return err
}

// ExportAccount returns the reports the user filed and the people they
// blocked. Reported content is other people's data and is not included.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type report struct {
		TargetKind string    `json:"target_kind"`
		TargetID   string    `json:"target_id"`
		Reason     string    `json:"reason"`
		Note       string    `json:"note"`
		Outcome    string    `json:"outcome"`
		CreatedAt  time.Time `json:"created_at"`
	}
	type block struct {
		UserID    uuid.UUID `json:"user_id"`
		CreatedAt time.Time `json:"created_at"`
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT r.target_kind, r.target_id, r.reason, r.note,
		       CASE WHEN c.status = 'open' THEN 'open'
		            WHEN c.action IN ('remove', 'warn', 'suspend', 'ban') THEN 'actioned'
		            ELSE 'closed' END,
		       r.created_at
		FROM moderation_reports r JOIN moderation_cases c ON c.id = r.case_id
		WHERE r.reporter_id = $1 ORDER BY r.created_at`, userID)
	if err != nil {
		return nil, err
	}
	reports, err := pgx.CollectRows(rows, pgx.RowToStructByPos[report])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT blocked_id, created_at FROM moderation_blocks
		WHERE blocker_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	blocks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[block])
	if err != nil {
		return nil, err
	}
	if reports == nil {
		reports = []report{}
	}
	if blocks == nil {
		blocks = []block{}
	}
	return map[string]any{"reports": reports, "blocks": blocks}, nil
}
