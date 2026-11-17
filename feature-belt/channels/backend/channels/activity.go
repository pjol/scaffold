package channels

// Activity (mentions of you, reactions to your messages, replies in threads
// you follow), All unread (every conversation's unread messages on one
// page) and Mark unread (move a read pointer back).

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/orgs"
)

const activityWindow = "90 days"

// ActivityItem is one thing that happened to the caller.
type ActivityItem struct {
	Kind    string    `json:"kind"` // mention | reaction | thread_reply
	At      time.Time `json:"at"`
	Message *Message  `json:"message"`
	Channel BriefChan `json:"channel"`
	Actor   *UserRef  `json:"actor"` // who mentioned, reacted or replied (null for the app's bots)
	Emoji   string    `json:"emoji"` // reaction
	Unread  bool      `json:"unread"`
}

type activityCursor struct {
	T time.Time `json:"t"`
	M uuid.UUID `json:"m"`
	K string    `json:"k"`
	A uuid.UUID `json:"a"`
}

// activitySQL is the union of activity rows for $1 in org $2 (filter $3).
// Rows from people with a block either way ($4) are left out.
const activitySQL = `
	SELECT a.kind, a.message_id, a.at, a.actor, a.emoji, a.channel_id FROM (
	    SELECT 'mention'::text AS kind, m.id AS message_id, m.created_at AS at, m.sender_id AS actor, ''::text AS emoji, m.channel_id
	    FROM channel_messages m JOIN channels c ON c.id = m.channel_id
	    WHERE c.org_id = $2 AND m.deleted_at IS NULL AND m.kind <> 'system' AND (m.sender_id IS NULL OR m.sender_id <> $1)
	      AND m.created_at > now() - interval '` + activityWindow + `'
	      AND (EXISTS (SELECT 1 FROM channel_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1)
	           OR (m.mention_channel AND EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = c.id AND x.user_id = $1)))
	    UNION ALL
	    SELECT 'reaction', r.message_id, r.created_at, r.user_id, r.emoji, m.channel_id
	    FROM channel_reactions r JOIN channel_messages m ON m.id = r.message_id JOIN channels c ON c.id = m.channel_id
	    WHERE c.org_id = $2 AND m.sender_id = $1 AND r.user_id <> $1 AND m.deleted_at IS NULL
	      AND r.created_at > now() - interval '` + activityWindow + `'
	    UNION ALL
	    SELECT 'thread_reply', m.id, m.created_at, m.sender_id, '', m.channel_id
	    FROM channel_thread_follows f JOIN channel_messages m ON m.thread_root_id = f.root_id JOIN channels c ON c.id = m.channel_id
	    WHERE f.user_id = $1 AND f.following AND c.org_id = $2 AND m.deleted_at IS NULL AND (m.sender_id IS NULL OR m.sender_id <> $1)
	      AND m.created_at > now() - interval '` + activityWindow + `'
	      AND NOT m.mention_channel AND NOT EXISTS (SELECT 1 FROM channel_mentions mm WHERE mm.message_id = m.id AND mm.user_id = $1)
	) a JOIN channels c ON c.id = a.channel_id
	WHERE ` + readableSQL + `
	  AND ($3 = '' OR a.kind = $3)
	  AND (a.actor IS NULL OR a.actor <> ALL($4))`

// GET /channels/orgs/{orgID}/activity?filter=mention|reaction|thread_reply&cursor=
func (s *Service) activity(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	filter := r.URL.Query().Get("filter")
	switch filter {
	case "", "mention", "reaction", "thread_reply":
	default:
		return httpx.BadRequest("filter is mention, reaction or thread_reply.")
	}
	p := httpx.ParsePage(r)
	var c activityCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	prefs, err := prefsOf(ctx, s.d.Pool, me, m.OrgID)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, activitySQL+`
		  AND ($5::timestamptz IS NULL OR (a.at, a.message_id, a.kind, COALESCE(a.actor, `+nilUUID+`)) < ($5, $6, $7, $8))
		ORDER BY a.at DESC, a.message_id DESC, a.kind DESC, COALESCE(a.actor, `+nilUUID+`) DESC
		LIMIT $9`, me, m.OrgID, filter, blocked, nullTime(c.T), c.M, c.K, c.A, p.Limit+1)
	if err != nil {
		return err
	}
	type row struct {
		kind      string
		msg       uuid.UUID
		at        time.Time
		actor     *uuid.UUID
		emoji     string
		channelID uuid.UUID
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.kind, &x.msg, &x.at, &x.actor, &x.emoji, &x.channelID)
	})
	if err != nil {
		return err
	}
	msgIDs, chans, actors := []uuid.UUID{}, []uuid.UUID{}, []uuid.UUID{}
	for _, x := range list {
		msgIDs, chans = append(msgIDs, x.msg), append(chans, x.channelID)
		if x.actor != nil {
			actors = append(actors, *x.actor)
		}
	}
	msgs, err := s.messagesByID(ctx, s.d.Pool, dedupe(msgIDs), me, false)
	if err != nil {
		return err
	}
	refs, err := s.channelRefs(ctx, me, chans)
	if err != nil {
		return err
	}
	users, err := loadUsers(ctx, s.d.Pool, dedupe(actors))
	if err != nil {
		return err
	}
	items := make([]ActivityItem, 0, len(list))
	for _, x := range list {
		msg, ok := msgs[x.msg]
		if !ok {
			continue
		}
		it := ActivityItem{Kind: x.kind, At: x.at, Message: msg, Channel: refs[x.channelID], Emoji: x.emoji,
			Unread: prefs.ActivityReadAt == nil || x.at.After(*prefs.ActivityReadAt)}
		if x.actor != nil {
			if u, ok := users[*x.actor]; ok {
				it.Actor = &u
			}
		}
		items = append(items, it)
	}
	page := httpx.NewPage(items, p.Limit, func(it ActivityItem) any {
		k := activityCursor{T: it.At, M: it.Message.ID, K: it.Kind}
		if it.Actor != nil {
			k.A = it.Actor.ID
		}
		return k
	})
	httpx.OK(w, page)
	return nil
}

// activityUnread counts activity since the caller last opened Activity
// (capped at 100; a week back when they never have).
func (s *Service) activityUnread(ctx context.Context, me, orgID uuid.UUID, readAt *time.Time) (int, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return 0, err
	}
	since := time.Now().Add(-7 * 24 * time.Hour)
	if readAt != nil {
		since = *readAt
	}
	var n int
	err = s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM (`+activitySQL+` AND a.at > $5 LIMIT 100) x`, me, orgID, "", blocked, since).Scan(&n)
	return n, err
}

// POST /channels/orgs/{orgID}/activity/read — everything up to now is seen.
func (s *Service) markActivityRead(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO channel_user_prefs (user_id, org_id, activity_read_at) VALUES ($1, $2, now())
		ON CONFLICT (user_id, org_id) DO UPDATE SET activity_read_at = now(), updated_at = now()`, me, m.OrgID); err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: m.OrgID, Reason: "activity"})
	httpx.NoContent(w)
	return nil
}

// ---- All unread ----------------------------------------------------------------------

// UnreadChannel is one conversation's unread messages, oldest first.
type UnreadChannel struct {
	Channel  SidebarChannel `json:"channel"`
	Messages []*Message     `json:"messages"`
	HasMore  bool           `json:"has_more"`
}

// GET /channels/orgs/{orgID}/unreads — up to 30 unmuted conversations with
// unread messages (most recent activity first), up to 20 messages each.
func (s *Service) unreads(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	list, err := s.sidebar(ctx, me, m.OrgID, nil)
	if err != nil {
		return err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	unread := []SidebarChannel{}
	for _, c := range list {
		if c.Unread > 0 && !c.Muted {
			unread = append(unread, c)
		}
	}
	sortByActivity(unread)
	if len(unread) > 30 {
		unread = unread[:30]
	}
	out := []UnreadChannel{}
	for _, c := range unread {
		var lastID *uuid.UUID
		if err := s.d.Pool.QueryRow(ctx, `SELECT last_read_message_id FROM channel_members WHERE channel_id = $1 AND user_id = $2`, c.ID, me).Scan(&lastID); err != nil {
			return err
		}
		win := window{channelID: c.ID, blocked: blocked, limit: 20, after: lastID}
		page, err := s.pageMessages(ctx, s.d.Pool, win, me)
		if err != nil {
			if lastID == nil {
				return err
			}
			// The read pointer's message is gone: show the newest instead.
			win.after = nil
			if page, err = s.pageMessages(ctx, s.d.Pool, win, me); err != nil {
				return err
			}
		}
		msgs := []*Message{}
		for _, msg := range page.Items {
			if msg.Kind != msgSystem {
				msgs = append(msgs, msg)
			}
		}
		out = append(out, UnreadChannel{Channel: c, Messages: msgs, HasMore: page.HasNewer})
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

func sortByActivity(list []SidebarChannel) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].LastMessageAt.After(list[j-1].LastMessageAt); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// ---- Mark unread ---------------------------------------------------------------------

type unreadReq struct {
	MessageID uuid.UUID `json:"message_id"` // this message and everything after it become unread
}

// POST /channels/channels/{id}/unread {message_id} — the channel (or, for a
// thread reply, the thread) is unread from that message on.
func (s *Service) markUnread(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req unreadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if err := a.requireMember(); err != nil {
		return err
	}
	var at time.Time
	var root *uuid.UUID
	var also bool
	err = s.d.Pool.QueryRow(ctx, `SELECT created_at, thread_root_id, also_in_channel FROM channel_messages WHERE id = $1 AND channel_id = $2`,
		req.MessageID, id).Scan(&at, &root, &also)
	if db.IsNoRows(err) {
		return errCursorScope
	}
	if err != nil {
		return err
	}
	if root != nil && !also {
		// A thread reply: the thread's pointer moves to the reply before it.
		var prevID *uuid.UUID
		var prevAt *time.Time
		if err := s.d.Pool.QueryRow(ctx, `
			SELECT id, created_at FROM channel_messages WHERE thread_root_id = $1 AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC LIMIT 1`, *root, at, req.MessageID).Scan(&prevID, &prevAt); err != nil && !db.IsNoRows(err) {
			return err
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO channel_thread_follows (root_id, user_id, following, last_read_reply_id, last_read_at) VALUES ($1, $2, true, $3, $4)
			ON CONFLICT (root_id, user_id) DO UPDATE SET following = true, last_read_reply_id = $3, last_read_at = $4, updated_at = now()`,
			*root, me, prevID, prevAt); err != nil {
			return err
		}
		s.threadEvent(ctx, me, a.OrgID, id, *root, "read")
		httpx.NoContent(w)
		return nil
	}
	var prevID *uuid.UUID
	var prevAt *time.Time
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT id, created_at FROM channel_messages
		WHERE channel_id = $1 AND (thread_root_id IS NULL OR also_in_channel) AND (created_at, id) < ($2, $3)
		ORDER BY created_at DESC, id DESC LIMIT 1`, id, at, req.MessageID).Scan(&prevID, &prevAt); err != nil && !db.IsNoRows(err) {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_members SET last_read_message_id = $3, last_read_at = $4, updated_at = now() WHERE channel_id = $1 AND user_id = $2`,
		id, me, prevID, prevAt); err != nil {
		return err
	}
	s.sidebarEvent(ctx, me, a.OrgID, id, "read", nil, false)
	httpx.NoContent(w)
	return nil
}
