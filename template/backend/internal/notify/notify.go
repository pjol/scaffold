// Package notify is the one way modules tell a user something happened.
// Core ships Realtime: an in-app event on the user's realtime topic. The
// notifications feature replaces Deps.Notify with a sender that also keeps an
// inbox, sends push and email, and honours preferences — modules never change.
package notify

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/realtime"
)

// Notification is channel-neutral. Kind is a stable dotted id the
// preferences screen groups by ("chat.message", "feed.comment").
type Notification struct {
	UserID uuid.UUID      `json:"user_id"`
	Kind   string         `json:"kind"`
	Title  string         `json:"title"`
	Body   string         `json:"body"`
	Link   string         `json:"link,omitempty"` // app path, e.g. /chat/<id>
	Data   map[string]any `json:"data,omitempty"`
}

// Sender delivers notifications. Send is best-effort: callers log the error
// and never fail the request because a notification did not go out.
type Sender interface {
	Send(ctx context.Context, n Notification) error
}

// Realtime publishes {"type":"notification"} on the recipient's user topic.
type Realtime struct{ Broker realtime.Broker }

func (s Realtime) Send(ctx context.Context, n Notification) error {
	err := s.Broker.Publish(ctx, realtime.UserTopic(n.UserID), realtime.EventNotification, n)
	if err != nil {
		slog.WarnContext(ctx, "notify: realtime publish failed", "kind", n.Kind, "err", err)
	}
	return err
}
