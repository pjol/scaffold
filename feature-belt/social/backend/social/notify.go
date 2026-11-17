package social

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
)

// noticeEvery is how often one follower can notify one person: following,
// unfollowing and following again within it sends nothing new.
const noticeEvery = time.Hour

// notifyFollow tells followee that follower followed them, after the
// request (bg.Go), at most once per follower per hour, never across a block.
func (s *Service) notifyFollow(ctx context.Context, follower, followee uuid.UUID) {
	bg.Go(ctx, "social.notify_follow", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		var due bool
		err := s.d.Pool.QueryRow(ctx, `
			WITH up AS (
				INSERT INTO social_follow_notices (follower_id, followee_id) VALUES ($1, $2)
				ON CONFLICT (follower_id, followee_id) DO UPDATE SET sent_at = now()
				WHERE social_follow_notices.sent_at < now() - $3::interval
				RETURNING 1)
			SELECT EXISTS (SELECT 1 FROM up)`, follower, followee, interval(noticeEvery)).Scan(&due)
		if err != nil || !due {
			return err
		}
		if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, follower, followee); err != nil || blocked {
			return err
		}
		refs, err := Refs(ctx, s.d.Pool, []uuid.UUID{follower})
		if err != nil {
			return err
		}
		ref, ok := refs[follower]
		if !ok || ref.Handle == "" {
			return nil
		}
		name := ref.DisplayName
		if name == "" {
			name = "@" + ref.Handle
		}
		if err := s.d.Notify.Send(ctx, notify.Notification{
			UserID: followee, Kind: KindFollowNotice,
			Title: name + " followed you", Body: "@" + ref.Handle, Link: "/u/" + ref.Handle,
			Data: map[string]any{"follower_id": follower.String(), "followee_id": followee.String(), "handle": ref.Handle},
		}); err != nil {
			slog.WarnContext(ctx, "social: follow notification", "err", err)
		}
		return nil
	})
}
