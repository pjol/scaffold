package feed

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
)

// publish sends a realtime nudge on the audience topic. Payloads carry ids
// and counts, never content: clients refetch through the API, which applies
// blocks and audiences. Best-effort.
func (s *Service) publish(ctx context.Context, audience, event string, data any) {
	topic := AudienceTopic(audience)
	if topic == "" {
		return
	}
	if err := s.d.Broker.Publish(ctx, topic, event, data); err != nil {
		slog.WarnContext(ctx, "feed: realtime publish failed", "event", event, "err", err)
	}
}

// afterCommit runs fn in the background (bg.Go), detached from the request,
// for side effects that must not fail or slow it (notifications, cleanup).
// name is "feed.<what>" for the logs.
func (s *Service) afterCommit(ctx context.Context, name string, fn func(ctx context.Context)) {
	bg.Go(ctx, name, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		fn(ctx)
		return nil
	})
}

// notifyUser sends n unless the recipient is the actor or either has blocked
// the other.
func (s *Service) notifyUser(ctx context.Context, actor uuid.UUID, n notify.Notification) {
	if n.UserID == actor || n.UserID == uuid.Nil {
		return
	}
	blocked, err := moderation.IsBlocked(ctx, s.d.Pool, actor, n.UserID)
	if err != nil {
		slog.WarnContext(ctx, "feed: block check before notify", "kind", n.Kind, "err", err)
		return
	}
	if blocked {
		return
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "feed: notify failed", "kind", n.Kind, "err", err)
	}
}

// fanOut sends n to every member of audience except the actor, users with a
// block either way, and ids in skip.
func (s *Service) fanOut(ctx context.Context, actor uuid.UUID, audience string, skip map[uuid.UUID]bool, n notify.Notification) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, actor)
	if err != nil {
		slog.WarnContext(ctx, "feed: blocks before fan-out", "kind", n.Kind, "err", err)
		return
	}
	exclude := map[uuid.UUID]bool{actor: true}
	for _, b := range blocked {
		exclude[b] = true
	}
	for id := range skip {
		exclude[id] = true
	}
	sent := 0
	err = s.scope.EachMember(ctx, audience, func(ids []uuid.UUID) error {
		for _, id := range ids {
			if exclude[id] {
				continue
			}
			m := n
			m.UserID = id
			if err := s.d.Notify.Send(ctx, m); err != nil {
				slog.WarnContext(ctx, "feed: notify failed", "kind", n.Kind, "err", err)
				continue
			}
			sent++
		}
		return ctx.Err()
	})
	if err != nil {
		slog.WarnContext(ctx, "feed: fan-out stopped", "kind", n.Kind, "sent", sent, "err", err)
		return
	}
	slog.InfoContext(ctx, "feed: fan-out done", "kind", n.Kind, "sent", sent)
}
