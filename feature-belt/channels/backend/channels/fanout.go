package channels

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
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// ChannelEvent is published as channels.channel on the channel topic: its
// details, members or pins changed (clients refetch what they show).
type ChannelEvent struct {
	ChannelID uuid.UUID  `json:"channel_id"`
	Reason    string     `json:"reason"` // updated | members | pins
	UserID    *uuid.UUID `json:"user_id,omitempty"`
}

// SidebarMessage is the part of a new message the sidebar needs.
type SidebarMessage struct {
	ID            uuid.UUID  `json:"id"`
	SenderID      *uuid.UUID `json:"sender_id"`
	CreatedAt     time.Time  `json:"created_at"`
	ThreadRootID  *uuid.UUID `json:"thread_root_id"`
	AlsoInChannel bool       `json:"also_in_channel"`
}

// SidebarEvent is published as channels.sidebar on a member's user topic, so
// unread and mention counts stay current without subscribing to every channel.
type SidebarEvent struct {
	OrgID     uuid.UUID       `json:"org_id"`
	ChannelID uuid.UUID       `json:"channel_id"`
	Reason    string          `json:"reason"` // message | added | removed | updated | read | later
	Message   *SidebarMessage `json:"message,omitempty"`
	Mention   bool            `json:"mention"` // reason message: it mentions you (or it's a DM)
}

// ThreadEvent is published as channels.thread on a follower's user topic.
type ThreadEvent struct {
	OrgID     uuid.UUID       `json:"org_id"`
	ChannelID uuid.UUID       `json:"channel_id"`
	RootID    uuid.UUID       `json:"root_id"`
	Reason    string          `json:"reason"` // reply | read | follow
	Message   *SidebarMessage `json:"message,omitempty"`
	Mention   bool            `json:"mention"`
}

func (s *Service) publish(ctx context.Context, channelID uuid.UUID, event string, data any) {
	ctx = context.WithoutCancel(ctx)
	if err := s.d.Broker.Publish(ctx, realtime.Topic(topicKind, channelID), event, data); err != nil {
		slog.WarnContext(ctx, "channels: publish", "event", event, "channel_id", channelID, "err", err)
	}
}

func (s *Service) toUser(ctx context.Context, userID uuid.UUID, event string, data any) {
	ctx = context.WithoutCancel(ctx)
	if err := s.d.Broker.Publish(ctx, realtime.UserTopic(userID), event, data); err != nil {
		slog.WarnContext(ctx, "channels: publish to user", "event", event, "err", err)
	}
}

func (s *Service) sidebarEvent(ctx context.Context, userID, orgID, channelID uuid.UUID, reason string, msg *SidebarMessage, mention bool) {
	s.toUser(ctx, userID, EventSidebar, SidebarEvent{OrgID: orgID, ChannelID: channelID, Reason: reason, Message: msg, Mention: mention})
}

func (s *Service) threadEvent(ctx context.Context, userID, orgID, channelID, rootID uuid.UUID, reason string) {
	s.toUser(ctx, userID, EventThread, ThreadEvent{OrgID: orgID, ChannelID: channelID, RootID: rootID, Reason: reason})
}

// sidebarAll tells every member of a channel to refresh its sidebar row.
func (s *Service) sidebarAll(ctx context.Context, channelID uuid.UUID, reason string) {
	ctx = context.WithoutCancel(ctx)
	var orgID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT org_id FROM channels WHERE id = $1`, channelID).Scan(&orgID); err != nil {
		return
	}
	for _, r := range s.recipients(ctx, channelID) {
		s.sidebarEvent(ctx, r.id, orgID, channelID, reason, nil, false)
	}
}

type recipient struct {
	id     uuid.UUID
	notify string
	muted  bool
}

func (s *Service) recipients(ctx context.Context, channelID uuid.UUID) []recipient {
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id, notify, muted FROM channel_members WHERE channel_id = $1`, channelID)
	if err != nil {
		slog.WarnContext(ctx, "channels: list recipients", "channel_id", channelID, "err", err)
		return nil
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (recipient, error) {
		var x recipient
		err := r.Scan(&x.id, &x.notify, &x.muted)
		return x, err
	})
	if err != nil {
		slog.WarnContext(ctx, "channels: list recipients", "channel_id", channelID, "err", err)
	}
	return list
}

// announce loads committed messages (system events) by id and announces them.
func (s *Service) announce(ctx context.Context, channelID uuid.UUID, ids ...uuid.UUID) {
	if len(ids) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	var a access
	if err := s.d.Pool.QueryRow(ctx, `SELECT id, org_id, kind, layout, COALESCE(name, '') FROM channels WHERE id = $1`, channelID).
		Scan(&a.ChannelID, &a.OrgID, &a.Kind, &a.Layout, &a.Name); err != nil {
		return
	}
	byID, err := s.messagesByID(ctx, s.d.Pool, ids, uuid.Nil, false)
	if err != nil {
		slog.WarnContext(ctx, "channels: load messages to announce", "channel_id", channelID, "err", err)
		return
	}
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			s.announceNew(ctx, &a, m, mentions{})
		}
	}
}

func summary(m *Message) *SidebarMessage {
	return &SidebarMessage{ID: m.ID, SenderID: m.senderID, CreatedAt: m.CreatedAt, ThreadRootID: m.ThreadRootID, AlsoInChannel: m.AlsoInChannel}
}

// announceNew publishes a new message on the channel topic, updates members'
// sidebars and thread followers, and notifies the people who should hear.
// Clients drop topic events from people they have a block with; sidebar
// events and notifications are filtered here.
func (s *Service) announceNew(ctx context.Context, a *access, m *Message, ments mentions) {
	ctx = context.WithoutCancel(ctx)
	s.publish(ctx, a.ChannelID, EventMessage, m)
	var blocked map[uuid.UUID]bool
	if m.senderID != nil && m.Kind != msgSystem {
		ids, err := moderation.BlockedIDs(ctx, s.d.Pool, *m.senderID)
		if err != nil {
			slog.WarnContext(ctx, "channels: blocked ids", "err", err)
		}
		blocked = uuidSet(ids)
	}
	direct := a.direct()
	members := s.recipients(ctx, a.ChannelID)
	sum := summary(m)
	timeline := m.ThreadRootID == nil || m.AlsoInChannel
	if timeline {
		for _, r := range members {
			if !blocked[r.id] {
				s.sidebarEvent(ctx, r.id, a.OrgID, a.ChannelID, "message", sum, m.Kind != msgSystem && (direct || ments.includes(r.id)))
			}
		}
	}
	followers := map[uuid.UUID]bool{}
	if m.ThreadRootID != nil {
		if root, err := s.message(ctx, s.d.Pool, *m.ThreadRootID, uuid.Nil); err == nil {
			s.publish(ctx, a.ChannelID, EventMessageUpdated, root)
		}
		rows, err := s.d.Pool.Query(ctx, `SELECT user_id FROM channel_thread_follows WHERE root_id = $1 AND following`, *m.ThreadRootID)
		if err == nil {
			ids, _ := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			for _, id := range ids {
				followers[id] = true
				if !blocked[id] {
					s.toUser(ctx, id, EventThread, ThreadEvent{OrgID: a.OrgID, ChannelID: a.ChannelID, RootID: *m.ThreadRootID, Reason: "reply",
						Message: sum, Mention: ments.includes(id)})
				}
			}
		}
	}
	if m.Kind == msgSystem {
		return
	}
	type target struct {
		id   uuid.UUID
		kind string
	}
	var targets []target
	for _, r := range members {
		if (m.senderID != nil && r.id == *m.senderID) || blocked[r.id] || r.muted {
			continue
		}
		pref := r.notify
		if pref == "default" {
			pref = "mentions"
			if direct {
				pref = "all"
			}
		}
		if pref == "nothing" {
			continue
		}
		mentioned := direct || ments.includes(r.id)
		switch {
		case mentioned:
			targets = append(targets, target{r.id, KindMention})
		case m.ThreadRootID != nil && followers[r.id]:
			targets = append(targets, target{r.id, KindThread})
		case timeline && pref == "all":
			targets = append(targets, target{r.id, KindMessage})
		}
	}
	if len(targets) == 0 {
		return
	}
	bg.Go(ctx, "channels.notify", func(ctx context.Context) error {
		nctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		title, body := s.notificationText(nctx, a, m)
		link := fmt.Sprintf("/channels/%s?message=%s", a.ChannelID, m.ID)
		if m.ThreadRootID != nil {
			link = fmt.Sprintf("/channels/%s?thread=%s&message=%s", a.ChannelID, *m.ThreadRootID, m.ID)
		}
		for _, t := range targets {
			ttl := title
			if direct && a.Kind == kindGroupDM {
				ttl = s.dmTitleFor(nctx, a.ChannelID, t.id)
			}
			err := s.d.Notify.Send(nctx, notify.Notification{
				UserID: t.id, Kind: t.kind, Title: ttl, Body: body, Link: link,
				Data: map[string]any{"org_id": a.OrgID.String(), "channel_id": a.ChannelID.String(), "message_id": m.ID.String()},
			})
			if err != nil {
				slog.WarnContext(nctx, "channels: notify", "channel_id", a.ChannelID, "err", err)
			}
		}
		return nil
	})
}

// notificationText: channels are titled "#name" (or "Thread in #name") and
// say who spoke; a 1:1 DM is titled with the sender.
func (s *Service) notificationText(ctx context.Context, a *access, m *Message) (title, body string) {
	sender := deref(m.BotName)
	if m.Sender != nil {
		sender = m.Sender.DisplayName
	}
	text := preview(m)
	switch {
	case a.Kind == kindDM:
		return sender, text
	case a.Kind == kindGroupDM:
		return "Group message", sender + ": " + text
	case m.ThreadRootID != nil:
		return "Thread in #" + a.Name, sender + ": " + text
	case m.Title != nil:
		return "New post in #" + a.Name, sender + ": " + *m.Title
	}
	return "#" + a.Name, sender + ": " + text
}

// dmTitleFor names a group DM from one member's point of view.
func (s *Service) dmTitleFor(ctx context.Context, channelID, userID uuid.UUID) string {
	people, err := s.dmPeople(ctx, s.d.Pool, userID, []uuid.UUID{channelID})
	if err != nil {
		return "Group message"
	}
	return dmTitle(people[channelID])
}

// preview is a message in one line: its first 80 characters, or what was sent.
func preview(m *Message) string {
	if t := plainText(m.Body, m.Mentions); t != "" {
		return truncate(t, 80)
	}
	switch n := len(m.Attachments); {
	case n == 1 && m.Attachments[0].Kind == "image":
		return "Sent a photo"
	case n == 1:
		return "Sent a file"
	case n > 1:
		return fmt.Sprintf("Sent %d files", n)
	}
	return "Sent a message"
}

// publishUpdated announces a changed message (edit, delete, reactions, pin)
// on the topic and, when counts may have changed, to every member's sidebar.
func (s *Service) publishUpdated(ctx context.Context, a *access, m *Message, sidebar bool) {
	ctx = context.WithoutCancel(ctx)
	s.publish(ctx, a.ChannelID, EventMessageUpdated, m)
	if sidebar {
		s.sidebarAll(ctx, a.ChannelID, "updated")
	}
}

// ---- stored files of deleted rows ------------------------------------------------

// kickObjectCleanup drains the queue in the background after a delete
// commits; the worker retries whatever this misses.
func (s *Service) kickObjectCleanup(ctx context.Context) {
	bg.Go(ctx, "channels.delete_objects", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		if err := s.deleteQueuedObjects(ctx); err != nil {
			slog.WarnContext(ctx, "channels: delete stored files (will retry)", "err", err)
		}
		return nil
	})
}

// deleteQueuedObjects removes queued objects from storage and stamps their rows.
func (s *Service) deleteQueuedObjects(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT storage_key FROM channel_deleted_objects
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
				UPDATE channel_deleted_objects SET attempts = attempts + 1, last_error = $2, updated_at = now()
				WHERE storage_key = $1`, k, err.Error()); uerr != nil {
				return uerr
			}
			continue
		}
		if _, err := s.d.Pool.Exec(ctx, `
			UPDATE channel_deleted_objects SET deleted_at = now(), last_error = '', updated_at = now() WHERE storage_key = $1`, k); err != nil {
			return err
		}
	}
	return lastErr
}
