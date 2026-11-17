package channels

// Message retention: a workspace keeps messages for N days (owners and admins
// set it, audited); a channel can override it. The channels.retention job
// deletes older messages for good: replies past the cutoff, then posts whose
// whole thread is past it. Files go through the clean-up queue.

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
)

const retentionBatch = 5000

// applyRetention is the job.
func (s *Service) applyRetention(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT c.id, COALESCE(c.retention_days, ws.retention_days)
		FROM channels c LEFT JOIN channel_workspace_settings ws ON ws.org_id = c.org_id
		WHERE COALESCE(c.retention_days, ws.retention_days) IS NOT NULL`)
	if err != nil {
		return err
	}
	type target struct {
		id   uuid.UUID
		days int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (target, error) {
		var t target
		return t, r.Scan(&t.id, &t.days)
	})
	if err != nil {
		return err
	}
	deleted := 0
	for _, t := range list {
		if ctx.Err() != nil {
			break
		}
		n, err := s.expireChannel(ctx, t.id, time.Now().AddDate(0, 0, -t.days))
		if err != nil {
			slog.WarnContext(ctx, "channels: retention", "channel_id", t.id, "err", err)
			continue
		}
		deleted += n
	}
	if deleted > 0 {
		s.kickObjectCleanup(ctx)
	}
	return nil
}

// expireChannel deletes one batch of messages older than cutoff in a channel.
func (s *Service) expireChannel(ctx context.Context, channelID uuid.UUID, cutoff time.Time) (int, error) {
	total := 0
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			DELETE FROM channel_messages WHERE id IN (
			    SELECT id FROM channel_messages WHERE channel_id = $1 AND thread_root_id IS NOT NULL AND created_at < $2 LIMIT $3)
			RETURNING thread_root_id`, channelID, cutoff, retentionBatch)
		if err != nil {
			return err
		}
		roots, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		total += len(roots)
		if len(roots) > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE channel_messages r SET reply_count = (SELECT count(*) FROM channel_messages x WHERE x.thread_root_id = r.id AND x.deleted_at IS NULL),
				  updated_at = now()
				WHERE r.id = ANY($1)`, dedupe(roots)); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM channel_messages WHERE id IN (
			    SELECT m.id FROM channel_messages m WHERE m.channel_id = $1 AND m.thread_root_id IS NULL AND m.created_at < $2
			      AND NOT EXISTS (SELECT 1 FROM channel_messages x WHERE x.thread_root_id = m.id)
			    LIMIT $3)`, channelID, cutoff, retentionBatch)
		if err != nil {
			return err
		}
		total += int(tag.RowsAffected())
		return nil
	})
	if err == nil && total > 0 {
		s.publish(ctx, channelID, EventChannel, ChannelEvent{ChannelID: channelID, Reason: "retention"})
	}
	return total, err
}

func auditLog(ctx context.Context, q db.Querier, actor uuid.UUID, action, targetType, targetID string, data any) error {
	return audit.Log(ctx, q, actor, action, targetType, targetID, data)
}
