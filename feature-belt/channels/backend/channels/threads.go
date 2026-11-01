package channels

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

// readableSQL limits channels alias c to ones $1 may read (org member, and a
// channel member unless public).
const readableSQL = `
	EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = c.org_id AND om.user_id = $1)
	AND (c.kind = 'public' OR EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = c.id AND x.user_id = $1))`

// ReadMark is a read pointer: the newest message someone has seen.
type ReadMark struct {
	MessageID uuid.UUID `json:"message_id"`
	At        time.Time `json:"at"`
}

func readMark(id *uuid.UUID, at *time.Time) *ReadMark {
	if id == nil || at == nil {
		return nil
	}
	return &ReadMark{MessageID: *id, At: *at}
}

// Thread is GET /channels/messages/{id}/thread.
type Thread struct {
	Root      *Message     `json:"root"`
	Replies   *MessagePage `json:"replies"`
	Channel   BriefChan    `json:"channel"`
	Following bool         `json:"following"`
	LastRead  *ReadMark    `json:"last_read"` // your read pointer in the thread
}

// GET /channels/messages/{id}/thread?before=|after=|around=&limit= — a root
// and its replies (a reply's id opens its thread).
func (s *Service) getThread(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	win, err := parseWindow(r)
	if err != nil {
		return err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	rootID := id
	if m.ThreadRootID != nil {
		rootID = *m.ThreadRootID
	} else if m.Kind == msgSystem {
		return errMessageNotFound
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	root, err := s.message(ctx, s.d.Pool, rootID, me)
	if err != nil {
		return err
	}
	if root.senderID != nil && uuidSet(blocked)[*root.senderID] {
		return errMessageNotFound
	}
	win.channelID, win.rootID, win.blocked = m.ChannelID, &rootID, blocked
	t := Thread{Root: root}
	if t.Replies, err = s.pageMessages(ctx, s.d.Pool, win, me); err != nil {
		return err
	}
	refs, err := s.channelRefs(ctx, me, []uuid.UUID{m.ChannelID})
	if err != nil {
		return err
	}
	t.Channel = refs[m.ChannelID]
	var readID *uuid.UUID
	var readAt *time.Time
	err = s.d.Pool.QueryRow(ctx, `SELECT following, last_read_reply_id, last_read_at FROM channel_thread_follows WHERE root_id = $1 AND user_id = $2`,
		rootID, me).Scan(&t.Following, &readID, &readAt)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	t.LastRead = readMark(readID, readAt)
	httpx.OK(w, t)
	return nil
}

type threadReadReq struct {
	MessageID *uuid.UUID `json:"message_id"` // newest reply shown; omitted = the newest
}

// POST /channels/messages/{id}/thread/read — moves your read pointer in a
// thread you follow forward.
func (s *Service) markThreadRead(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req threadReadReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if m.ThreadRootID != nil {
		return httpx.BadRequest("Send the thread's first message id.")
	}
	var replyID uuid.UUID
	var at time.Time
	if req.MessageID != nil {
		err = s.d.Pool.QueryRow(ctx, `SELECT id, created_at FROM channel_messages WHERE id = $1 AND thread_root_id = $2`, *req.MessageID, id).Scan(&replyID, &at)
		if db.IsNoRows(err) {
			return errThreadScope
		}
	} else {
		err = s.d.Pool.QueryRow(ctx, `SELECT id, created_at FROM channel_messages WHERE thread_root_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, id).Scan(&replyID, &at)
		if db.IsNoRows(err) {
			httpx.NoContent(w)
			return nil
		}
	}
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_thread_follows SET last_read_reply_id = $3, last_read_at = $4, updated_at = now()
		WHERE root_id = $1 AND user_id = $2
		  AND (last_read_at IS NULL OR (last_read_at, COALESCE(last_read_reply_id, `+nilUUID+`)) < ($4, $3))`, id, me, replyID, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.threadEvent(ctx, me, m.OrgID, m.ChannelID, id, "read")
	}
	httpx.NoContent(w)
	return nil
}

// PUT /channels/messages/{id}/follow
func (s *Service) follow(w http.ResponseWriter, r *http.Request) error {
	return s.setFollow(w, r, true)
}

// DELETE /channels/messages/{id}/follow — later replies and mentions don't
// bring it back; replying in it does.
func (s *Service) unfollow(w http.ResponseWriter, r *http.Request) error {
	return s.setFollow(w, r, false)
}

func (s *Service) setFollow(w http.ResponseWriter, r *http.Request, following bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if m.ThreadRootID != nil || m.Kind == msgSystem {
		return httpx.BadRequest("Follow a thread by its first message.")
	}
	if following {
		if err := m.requireMember(); err != nil {
			return err
		}
	}
	// Following starts with everything so far read.
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO channel_thread_follows (root_id, user_id, following, last_read_reply_id, last_read_at)
		SELECT $1, $2, $3, lr.id, lr.created_at
		FROM (SELECT 1) one
		LEFT JOIN LATERAL (SELECT id, created_at FROM channel_messages WHERE thread_root_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1) lr ON true
		ON CONFLICT (root_id, user_id) DO UPDATE SET following = EXCLUDED.following, updated_at = now()`, id, me, following); err != nil {
		return err
	}
	s.threadEvent(ctx, me, m.OrgID, m.ChannelID, id, "follow")
	httpx.OK(w, map[string]bool{"following": following})
	return nil
}

// ---- the Threads view -----------------------------------------------------------------

// ThreadSummary is one row of the Threads view.
type ThreadSummary struct {
	Root          *Message   `json:"root"`
	Channel       BriefChan  `json:"channel"`
	Unread        int        `json:"unread"` // replies after your read pointer
	LastRead      *ReadMark  `json:"last_read"`
	LatestReplies []*Message `json:"latest_replies"` // up to 2, oldest first
}

// GET /channels/orgs/{orgID}/threads?cursor= — threads you follow, latest
// reply first.
func (s *Service) threads(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := s.threadSummaries(ctx, me, m.OrgID, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(t ThreadSummary) any { return cursor{deref(t.Root.LastReplyAt), t.Root.ID} }))
	return nil
}

func (s *Service) threadSummaries(ctx context.Context, me, orgID uuid.UUID, c cursor, limit int) ([]ThreadSummary, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT m.id, m.channel_id, f.last_read_reply_id, f.last_read_at,
		       (SELECT count(*) FROM channel_messages x WHERE x.thread_root_id = m.id AND x.deleted_at IS NULL
		          AND (x.created_at, x.id) > (COALESCE(f.last_read_at, '-infinity'), COALESCE(f.last_read_reply_id, `+nilUUID+`))
		          AND (x.sender_id IS NULL OR (x.sender_id <> $1 AND x.sender_id <> ALL($3))))::int
		FROM channel_thread_follows f
		JOIN channel_messages m ON m.id = f.root_id
		JOIN channels c ON c.id = m.channel_id
		WHERE f.user_id = $1 AND f.following AND c.org_id = $2 AND m.reply_count > 0 AND m.last_reply_at IS NOT NULL
		  AND (m.sender_id IS NULL OR m.sender_id <> ALL($3))
		  AND `+readableSQL+`
		  AND ($4::timestamptz IS NULL OR (m.last_reply_at, m.id) < ($4, $5))
		ORDER BY m.last_reply_at DESC, m.id DESC LIMIT $6`, me, orgID, blocked, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		root, channel uuid.UUID
		readID        *uuid.UUID
		readAt        *time.Time
		unread        int
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.root, &x.channel, &x.readID, &x.readAt, &x.unread)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	rootIDs, channelIDs := make([]uuid.UUID, len(list)), make([]uuid.UUID, len(list))
	for i, x := range list {
		rootIDs[i], channelIDs[i] = x.root, x.channel
	}
	roots, err := s.messagesByID(ctx, s.d.Pool, rootIDs, me, false)
	if err != nil {
		return nil, err
	}
	refs, err := s.channelRefs(ctx, me, channelIDs)
	if err != nil {
		return nil, err
	}
	// The latest 2 replies of each thread.
	replyRows, err := s.d.Pool.Query(ctx, `
		SELECT x.id FROM unnest($1::uuid[]) AS t(root)
		CROSS JOIN LATERAL (
		    SELECT y.id FROM channel_messages y WHERE y.thread_root_id = t.root
		      AND (y.sender_id IS NULL OR y.sender_id <> ALL($2))
		    ORDER BY y.created_at DESC, y.id DESC LIMIT 2
		) x`, rootIDs, blocked)
	if err != nil {
		return nil, err
	}
	replyIDs, err := pgx.CollectRows(replyRows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	replies, err := s.messagesByID(ctx, s.d.Pool, replyIDs, me, false)
	if err != nil {
		return nil, err
	}
	byRoot := map[uuid.UUID][]*Message{}
	for _, id := range replyIDs {
		if rm, ok := replies[id]; ok && rm.ThreadRootID != nil {
			byRoot[*rm.ThreadRootID] = append([]*Message{rm}, byRoot[*rm.ThreadRootID]...)
		}
	}
	out := make([]ThreadSummary, 0, len(list))
	for _, x := range list {
		root, ok := roots[x.root]
		if !ok {
			continue
		}
		latest := byRoot[x.root]
		if latest == nil {
			latest = []*Message{}
		}
		out = append(out, ThreadSummary{Root: root, Channel: refs[x.channel], Unread: x.unread, LastRead: readMark(x.readID, x.readAt), LatestReplies: latest})
	}
	return out, nil
}

// threadsUnread counts followed threads in an org with unread replies.
func (s *Service) threadsUnread(ctx context.Context, me, orgID uuid.UUID) (int, error) {
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.d.Pool.QueryRow(ctx, `
		SELECT count(*) FROM channel_thread_follows f
		JOIN channel_messages m ON m.id = f.root_id
		JOIN channels c ON c.id = m.channel_id
		WHERE f.user_id = $1 AND f.following AND c.org_id = $2 AND `+readableSQL+`
		  AND EXISTS (SELECT 1 FROM channel_messages x WHERE x.thread_root_id = m.id AND x.deleted_at IS NULL
		          AND (x.created_at, x.id) > (COALESCE(f.last_read_at, '-infinity'), COALESCE(f.last_read_reply_id, `+nilUUID+`))
		          AND (x.sender_id IS NULL OR (x.sender_id <> $1 AND x.sender_id <> ALL($3))))`, me, orgID, blocked).Scan(&n)
	return n, err
}

// ---- Later (saved items) ------------------------------------------------------------------

type laterReq struct {
	Done bool `json:"done"`
}

// PUT /channels/messages/{id}/later {done} — save a message, or mark it done.
func (s *Service) saveLater(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req laterReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if m.Kind == msgSystem || m.Deleted {
		return httpx.Conflict("This message can't be saved.")
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO channel_saved (user_id, message_id, completed_at) VALUES ($1, $2, CASE WHEN $3 THEN now() END)
		ON CONFLICT (user_id, message_id) DO UPDATE SET completed_at = CASE WHEN $3 THEN COALESCE(channel_saved.completed_at, now()) END,
		  updated_at = now()`, me, id, req.Done); err != nil {
		return err
	}
	s.sidebarEvent(ctx, me, m.OrgID, m.ChannelID, "later", nil, false)
	httpx.NoContent(w)
	return nil
}

// DELETE /channels/messages/{id}/later
func (s *Service) unsaveLater(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var orgID, channelID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		DELETE FROM channel_saved sv USING channel_messages m, channels c
		WHERE sv.user_id = $1 AND sv.message_id = $2 AND m.id = sv.message_id AND c.id = m.channel_id
		RETURNING c.org_id, c.id`, me, id).Scan(&orgID, &channelID)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if err == nil {
		s.sidebarEvent(ctx, me, orgID, channelID, "later", nil, false)
	}
	httpx.NoContent(w)
	return nil
}

// LaterItem is a saved message.
type LaterItem struct {
	Message     *Message   `json:"message"`
	Channel     BriefChan  `json:"channel"`
	SavedAt     time.Time  `json:"saved_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// GET /channels/orgs/{orgID}/later?state=open|done&cursor= — newest saved first.
func (s *Service) later(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	done := r.URL.Query().Get("state") == "done"
	rows, err := s.d.Pool.Query(ctx, `
		SELECT sv.message_id, m.channel_id, sv.created_at, sv.completed_at
		FROM channel_saved sv JOIN channel_messages m ON m.id = sv.message_id JOIN channels c ON c.id = m.channel_id
		WHERE sv.user_id = $1 AND c.org_id = $2 AND (sv.completed_at IS NOT NULL) = $3 AND `+readableSQL+`
		  AND ($4::timestamptz IS NULL OR (sv.created_at, sv.message_id) < ($4, $5))
		ORDER BY sv.created_at DESC, sv.message_id DESC LIMIT $6`, me, m.OrgID, done, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	type row struct {
		msg, channel uuid.UUID
		saved        time.Time
		completed    *time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.msg, &x.channel, &x.saved, &x.completed)
		return x, err
	})
	if err != nil {
		return err
	}
	ids, chans := make([]uuid.UUID, len(list)), make([]uuid.UUID, len(list))
	for i, x := range list {
		ids[i], chans[i] = x.msg, x.channel
	}
	msgs, err := s.messagesByID(ctx, s.d.Pool, ids, me, false)
	if err != nil {
		return err
	}
	refs, err := s.channelRefs(ctx, me, chans)
	if err != nil {
		return err
	}
	items := make([]LaterItem, 0, len(list))
	for _, x := range list {
		if msg, ok := msgs[x.msg]; ok {
			items = append(items, LaterItem{Message: msg, Channel: refs[x.channel], SavedAt: x.saved, CompletedAt: x.completed})
		}
	}
	httpx.OK(w, httpx.NewPage(items, p.Limit, func(i LaterItem) any { return cursor{i.SavedAt, i.Message.ID} }))
	return nil
}
