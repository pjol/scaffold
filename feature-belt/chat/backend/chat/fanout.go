package chat

// Everything here runs after a commit and is best-effort: a failure is
// logged and the request still succeeds. Clients refetch on reconnect.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// convHead is what announcements need to know about a conversation.
type convHead struct {
	ID    uuid.UUID
	Kind  string
	Title string
}

func (m *member) head() convHead { return convHead{ID: m.ConversationID, Kind: m.Kind, Title: m.Title} }

// InboxEvent is published as chat.inbox on each member's user topic, so the
// inbox and badge stay current without subscribing to every conversation.
type InboxEvent struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Reason         string    `json:"reason"` // message | message_updated | added | removed | read | updated
	Message        *Message  `json:"message,omitempty"`
}

func (s *Service) publish(ctx context.Context, convID uuid.UUID, event string, data any) {
	ctx = context.WithoutCancel(ctx)
	if err := s.d.Broker.Publish(ctx, realtime.Topic(topicKind, convID), event, data); err != nil {
		slog.WarnContext(ctx, "chat: publish", "event", event, "conversation_id", convID, "err", err)
	}
}

func (s *Service) inboxEvent(ctx context.Context, userID, convID uuid.UUID, reason string, msg *Message) {
	ctx = context.WithoutCancel(ctx)
	ev := InboxEvent{ConversationID: convID, Reason: reason, Message: msg}
	if err := s.d.Broker.Publish(ctx, realtime.UserTopic(userID), EventInbox, ev); err != nil {
		slog.WarnContext(ctx, "chat: publish inbox", "conversation_id", convID, "err", err)
	}
}

type recipient struct {
	id    uuid.UUID
	muted bool
}

func (s *Service) recipients(ctx context.Context, convID uuid.UUID) ([]recipient, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT user_id, COALESCE(muted_until > now(), false)
		FROM chat_members WHERE conversation_id = $1 AND left_at IS NULL`, convID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (recipient, error) {
		var r recipient
		err := row.Scan(&r.id, &r.muted)
		return r, err
	})
}

// blockedWith is everyone with a block either way with userID, as a set.
func (s *Service) blockedWith(ctx context.Context, q db.Querier, userID uuid.UUID) map[uuid.UUID]bool {
	ids, err := moderation.BlockedIDs(ctx, q, userID)
	if err != nil {
		slog.WarnContext(ctx, "chat: blocked ids", "err", err)
	}
	return uuidSet(ids)
}

// announce loads committed messages by id and announces them.
func (s *Service) announce(ctx context.Context, head convHead, ids ...uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	byID, err := s.messagesByID(ctx, s.d.Pool, ids, false)
	if err != nil {
		slog.WarnContext(ctx, "chat: load messages to announce", "conversation_id", head.ID, "err", err)
		return
	}
	msgs := make([]*Message, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			msgs = append(msgs, m)
		}
	}
	s.announceMessages(ctx, head, msgs...)
}

// announceMessages publishes new messages on the conversation topic, updates
// each member's inbox, and notifies the people who should hear about them.
// Clients drop topic events from senders they have a block with; inbox
// events and notifications are filtered here.
func (s *Service) announceMessages(ctx context.Context, head convHead, msgs ...*Message) {
	ctx = context.WithoutCancel(ctx)
	members, err := s.recipients(ctx, head.ID)
	if err != nil {
		slog.WarnContext(ctx, "chat: list recipients", "conversation_id", head.ID, "err", err)
	}
	for _, m := range msgs {
		s.publish(ctx, head.ID, EventMessage, m)
		blocked := map[uuid.UUID]bool{}
		if m.Kind == kindText {
			blocked = s.blockedWith(ctx, s.d.Pool, m.Sender.ID)
		}
		var notifyTo []uuid.UUID
		for _, r := range members {
			if blocked[r.id] {
				continue
			}
			s.inboxEvent(ctx, r.id, head.ID, "message", m)
			if r.id != m.Sender.ID && !r.muted && notifies(m, r.id) {
				notifyTo = append(notifyTo, r.id)
			}
		}
		if len(notifyTo) > 0 {
			bg.Go(ctx, "chat.notify", func(ctx context.Context) error {
				s.notify(ctx, head, m, notifyTo)
				return nil
			})
		}
	}
}

// notifies reports whether m is worth a notification to userID: every text
// message, and being added to a group.
func notifies(m *Message, userID uuid.UUID) bool {
	switch {
	case m.Kind == kindText:
		return true
	case m.Event == nil:
		return false
	case *m.Event == "created":
		return true
	case *m.Event == "member_added":
		return m.Subject != nil && m.Subject.ID == userID
	}
	return false
}

func (s *Service) notify(ctx context.Context, head convHead, m *Message, to []uuid.UUID) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	title, body := notificationText(head, m)
	for _, uid := range to {
		err := s.d.Notify.Send(ctx, notify.Notification{
			UserID: uid, Kind: notifyKind, Title: title, Body: body,
			Link: "/chat/" + head.ID.String(),
			Data: map[string]any{"conversation_id": head.ID.String(), "message_id": m.ID.String()},
		})
		if err != nil {
			slog.WarnContext(ctx, "chat: notify", "conversation_id", head.ID, "err", err)
		}
	}
}

// notificationText: a direct message is titled with the sender and reads as
// what they sent; a group is titled with the group and says who spoke.
func notificationText(head convHead, m *Message) (title, body string) {
	sender := m.Sender.DisplayName
	if m.Kind == kindSystem {
		return head.Title, sender + " added you"
	}
	if head.Kind == kindGroup {
		return head.Title, sender + ": " + preview(m)
	}
	return sender, preview(m)
}

// preview is a message in one line: its first 80 characters, or what was sent.
func preview(m *Message) string {
	if m.Body != "" {
		return truncate(m.Body, 80)
	}
	n := len(m.Attachments)
	kind := "file"
	if n > 0 {
		kind = m.Attachments[0].Kind
		for _, a := range m.Attachments {
			if a.Kind != kind {
				kind = "file"
			}
		}
	}
	words := map[string][2]string{
		"image": {"Sent a photo", "Sent %d photos"},
		"video": {"Sent a video", "Sent %d videos"},
		"audio": {"Sent an audio file", "Sent %d audio files"},
		"file":  {"Sent a file", "Sent %d files"},
	}[kind]
	if n > 1 {
		return fmt.Sprintf(words[1], n)
	}
	return words[0]
}

// publishUpdated announces a changed message (edit, delete, reactions) on the
// topic and, when inbox is set, to each member's inbox.
func (s *Service) publishUpdated(ctx context.Context, m *Message, inbox bool) {
	ctx = context.WithoutCancel(ctx)
	s.publish(ctx, m.ConversationID, EventMessageUpdated, m)
	if !inbox {
		return
	}
	members, err := s.recipients(ctx, m.ConversationID)
	if err != nil {
		slog.WarnContext(ctx, "chat: list recipients", "conversation_id", m.ConversationID, "err", err)
		return
	}
	blocked := s.blockedWith(ctx, s.d.Pool, m.Sender.ID)
	for _, r := range members {
		if !blocked[r.id] {
			s.inboxEvent(ctx, r.id, m.ConversationID, "message_updated", m)
		}
	}
}

// ---- stored files of deleted rows ------------------------------------------------

// queueObjects marks storage objects for deletion (in the caller's transaction).
func queueObjects(ctx context.Context, q db.Querier, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO chat_deleted_objects (storage_key) SELECT unnest($1::text[])
		ON CONFLICT (storage_key) DO NOTHING`, keys)
	return err
}

// kickObjectCleanup drains the queue in the background after a delete
// commits; the worker retries whatever this misses.
func (s *Service) kickObjectCleanup(ctx context.Context) {
	bg.Go(ctx, "chat.delete_objects", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if err := s.deleteQueuedObjects(ctx); err != nil {
			slog.WarnContext(ctx, "chat: delete stored files (will retry)", "err", err)
		}
		return nil
	})
}

// deleteQueuedObjects removes queued objects from storage and stamps their rows.
func (s *Service) deleteQueuedObjects(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT storage_key FROM chat_deleted_objects
		WHERE deleted_at IS NULL AND attempts < 50 ORDER BY created_at LIMIT 200`)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var lastErr error
	for _, k := range keys {
		if err := s.d.Storage.Delete(ctx, k); err != nil {
			lastErr = err
			if _, uerr := s.d.Pool.Exec(ctx, `
				UPDATE chat_deleted_objects SET attempts = attempts + 1, last_error = $2, updated_at = now()
				WHERE storage_key = $1`, k, err.Error()); uerr != nil {
				return uerr
			}
			continue
		}
		if _, err := s.d.Pool.Exec(ctx, `
			UPDATE chat_deleted_objects SET deleted_at = now(), last_error = '', updated_at = now()
			WHERE storage_key = $1`, k); err != nil {
			return err
		}
	}
	return lastErr
}
