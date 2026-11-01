package channels

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/storage"
)

// ---- reading -----------------------------------------------------------------

// window selects part of a channel timeline or a thread: the newest page, or
// before, after or around a message id (jump to message).
type window struct {
	channelID             uuid.UUID
	rootID                *uuid.UUID  // a thread's replies instead of the timeline
	blocked               []uuid.UUID // nil = no filter (staff)
	staff                 bool
	before, after, around *uuid.UUID
	limit                 int
}

func parseWindow(r *http.Request) (window, error) {
	q := r.URL.Query()
	w := window{limit: 50}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		w.limit = min(v, 100)
	}
	set := 0
	for _, p := range []struct {
		name string
		dst  **uuid.UUID
	}{{"before", &w.before}, {"after", &w.after}, {"around", &w.around}} {
		v := q.Get(p.name)
		if v == "" {
			continue
		}
		id, err := uuid.Parse(v)
		if err != nil {
			return w, httpx.BadRequest("Invalid " + p.name + ".")
		}
		*p.dst = &id
		set++
	}
	if set > 1 {
		return w, httpx.BadRequest("Send only one of before, after or around.")
	}
	return w, nil
}

// GET /channels/channels/{id}/messages?before=|after=|around=<id>&limit= — the timeline.
func (s *Service) listMessages(w http.ResponseWriter, r *http.Request) error {
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
	if _, err := s.loadAccess(ctx, s.d.Pool, id, me); err != nil {
		return err
	}
	if win.blocked, err = moderation.BlockedIDs(ctx, s.d.Pool, me); err != nil {
		return err
	}
	win.channelID = id
	page, err := s.pageMessages(ctx, s.d.Pool, win, me)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// pageMessages returns a window, oldest first, keyset on (created_at, id).
func (s *Service) pageMessages(ctx context.Context, q db.Querier, w window, viewer uuid.UUID) (*MessagePage, error) {
	resolve := func(id uuid.UUID) (*time.Time, error) {
		var t time.Time
		err := q.QueryRow(ctx, `SELECT created_at FROM channel_messages WHERE id = $1 AND channel_id = $2`, id, w.channelID).Scan(&t)
		if db.IsNoRows(err) {
			return nil, errCursorScope
		}
		return &t, err
	}
	page := &MessagePage{Items: []*Message{}}
	switch {
	case w.before != nil || w.after != nil:
		anchor := deref(w.before)
		if w.after != nil {
			anchor = *w.after
		}
		t, err := resolve(anchor)
		if err != nil {
			return nil, err
		}
		if w.before != nil {
			older, err := s.fetchMessages(ctx, q, w, "<", t, anchor, true, w.limit+1)
			if err != nil {
				return nil, err
			}
			page.HasOlder, page.HasNewer = len(older) > w.limit, true
			page.Items = reversed(older[:min(len(older), w.limit)])
		} else {
			newer, err := s.fetchMessages(ctx, q, w, ">", t, anchor, false, w.limit+1)
			if err != nil {
				return nil, err
			}
			page.HasOlder, page.HasNewer = true, len(newer) > w.limit
			page.Items = newer[:min(len(newer), w.limit)]
		}
	case w.around != nil:
		t, err := resolve(*w.around)
		if err != nil {
			return nil, err
		}
		half := w.limit / 2
		older, err := s.fetchMessages(ctx, q, w, "<", t, *w.around, true, half+1)
		if err != nil {
			return nil, err
		}
		newer, err := s.fetchMessages(ctx, q, w, ">=", t, *w.around, false, w.limit-half+1)
		if err != nil {
			return nil, err
		}
		page.HasOlder, page.HasNewer = len(older) > half, len(newer) > w.limit-half
		page.Items = append(reversed(older[:min(len(older), half)]), newer[:min(len(newer), w.limit-half)]...)
	default:
		latest, err := s.fetchMessages(ctx, q, w, "<", nil, uuid.Nil, true, w.limit+1)
		if err != nil {
			return nil, err
		}
		page.HasOlder = len(latest) > w.limit
		page.Items = reversed(latest[:min(len(latest), w.limit)])
	}
	if err := s.hydrate(ctx, q, page.Items, viewer, w.staff); err != nil {
		return nil, err
	}
	return page, nil
}

// fetchMessages reads up to limit messages compared to (t, id) with cmp, a
// constant ("<", ">", ">="). t nil = from the newest.
func (s *Service) fetchMessages(ctx context.Context, q db.Querier, w window, cmp string, t *time.Time, id uuid.UUID, desc bool, limit int) ([]*Message, error) {
	order := " ASC"
	if desc {
		order = " DESC"
	}
	scope := `m.channel_id = $1 AND (m.thread_root_id IS NULL OR m.also_in_channel)`
	args := []any{w.channelID, w.blocked, t, id, limit}
	if w.rootID != nil {
		scope = `m.channel_id = $1 AND m.thread_root_id = $6`
		args = append(args, *w.rootID)
	}
	rows, err := q.Query(ctx, messageSelect+`
		WHERE `+scope+`
		  AND ($2::uuid[] IS NULL OR m.kind = 'system' OR m.sender_id IS NULL OR m.sender_id <> ALL($2))
		  AND ($3::timestamptz IS NULL OR (m.created_at, m.id) `+cmp+` ($3, $4::uuid))
		ORDER BY m.created_at`+order+`, m.id`+order+`
		LIMIT $5`, args...)
	if err != nil {
		return nil, err
	}
	return scanMessages(rows, w.staff)
}

func reversed(in []*Message) []*Message {
	out := slices.Clone(in)
	slices.Reverse(out)
	if out == nil {
		out = []*Message{}
	}
	return out
}

// GET /channels/channels/{id}/topics?tag=&cursor= — a forum channel's posts,
// latest activity first.
func (s *Service) listTopics(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.loadAccess(ctx, s.d.Pool, id, me); err != nil {
		return err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, me)
	if err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	rows, err := s.d.Pool.Query(ctx, messageSelect+`
		WHERE m.channel_id = $1 AND m.thread_root_id IS NULL AND m.kind <> 'system'
		  AND (m.deleted_at IS NULL OR m.reply_count > 0)
		  AND (m.sender_id IS NULL OR m.sender_id <> ALL($2))
		  AND ($3 = '' OR $3 = ANY(m.tags))
		  AND ($4::timestamptz IS NULL OR (m.activity_at, m.id) < ($4, $5))
		ORDER BY m.activity_at DESC, m.id DESC LIMIT $6`, id, blocked, tag, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	msgs, err := scanMessages(rows, false)
	if err != nil {
		return err
	}
	if err := s.hydrate(ctx, s.d.Pool, msgs, me, false); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(msgs, p.Limit, func(m *Message) any {
		at := m.CreatedAt
		if m.LastReplyAt != nil && m.LastReplyAt.After(at) {
			at = *m.LastReplyAt
		}
		return cursor{at, m.ID}
	}))
	return nil
}

// MessageContext is GET /channels/messages/{id}: a message and where it lives
// (message links open the channel, or the thread for a reply).
type MessageContext struct {
	Message *Message  `json:"message"`
	Channel BriefChan `json:"channel"`
}

// BriefChan names a channel in links, search hits and Later items.
type BriefChan struct {
	ID     uuid.UUID `json:"id"`
	OrgID  uuid.UUID `json:"org_id"`
	Kind   string    `json:"kind"`
	Layout string    `json:"layout"`
	Name   string    `json:"name"` // channel handle or the DM's people
}

func (s *Service) getMessage(w http.ResponseWriter, r *http.Request) error {
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
	if m.SenderID != nil {
		if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, me, *m.SenderID); err != nil || blocked {
			if err == nil {
				err = errMessageNotFound
			}
			return err
		}
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	refs, err := s.channelRefs(ctx, me, []uuid.UUID{m.ChannelID})
	if err != nil {
		return err
	}
	httpx.OK(w, MessageContext{Message: msg, Channel: refs[m.ChannelID]})
	return nil
}

// channelRefs names channels for the caller (DMs: their other people).
func (s *Service) channelRefs(ctx context.Context, me uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]BriefChan, error) {
	out := map[uuid.UUID]BriefChan{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, org_id, kind, layout, COALESCE(name, '') FROM channels WHERE id = ANY($1)`, dedupe(ids))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[BriefChan])
	if err != nil {
		return nil, err
	}
	var dms []uuid.UUID
	for _, c := range list {
		out[c.ID] = c
		if c.Kind == kindDM || c.Kind == kindGroupDM {
			dms = append(dms, c.ID)
		}
	}
	people, err := s.dmPeople(ctx, s.d.Pool, me, dms)
	if err != nil {
		return nil, err
	}
	for id, p := range people {
		c := out[id]
		c.Name = dmTitle(p)
		out[id] = c
	}
	return out, nil
}

// ---- sending -------------------------------------------------------------------

type attachmentIn struct {
	Key       string `json:"key"` // from POST /v1/uploads with purpose channel_attachment
	FileName  string `json:"file_name"`
	SizeBytes *int64 `json:"size_bytes"`
	Width     *int   `json:"width"`
	Height    *int   `json:"height"`
}

type sendReq struct {
	ClientID          string         `json:"client_id"`
	Body              string         `json:"body"`
	ThreadRootID      *uuid.UUID     `json:"thread_root_id"`
	AlsoSendToChannel bool           `json:"also_send_to_channel"`
	Title             string         `json:"title"` // forum channels: required for a new post
	Tags              []string       `json:"tags"`
	Attachments       []attachmentIn `json:"attachments"`
}

type attachmentRow struct {
	key, kind, contentType, fileName string
	size                             *int64
	width, height                    *int
}

// POST /channels/channels/{id}/messages — 201 with the message; 200 with the
// original when client_id was already used.
func (s *Service) sendMessage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	channelID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req sendReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	msg, created, err := s.post(ctx, me, channelID, req)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, msg)
	return nil
}

// post sends a message as me with every check the route applies; created is
// false when client_id was already used (the original comes back). The
// agent tool posts through here too.
func (s *Service) post(ctx context.Context, me, channelID uuid.UUID, req sendReq) (*Message, bool, error) {
	body := strings.TrimSpace(req.Body)
	title := strings.Join(strings.Fields(req.Title), " ")
	switch {
	case req.ClientID != "" && !clientIDPattern.MatchString(req.ClientID):
		return nil, false, httpx.BadRequest("client_id must be 1 to 64 letters, digits, dashes or underscores.")
	case body == "" && len(req.Attachments) == 0:
		return nil, false, httpx.BadRequest("Write a message or attach a file.")
	case utf8.RuneCountInString(body) > s.cfg.maxChars:
		return nil, false, httpx.BadRequest(fmt.Sprintf("Messages can be up to %d characters.", s.cfg.maxChars))
	case len(req.Attachments) > maxAttachments:
		return nil, false, httpx.BadRequest(fmt.Sprintf("Attach up to %d files per message.", maxAttachments))
	case utf8.RuneCountInString(title) > 200:
		return nil, false, httpx.BadRequest("Titles can be up to 200 characters.")
	}
	atts, err := s.parseAttachments(me, req.Attachments)
	if err != nil {
		return nil, false, err
	}
	if err := checkText(ctx, body, title); err != nil {
		return nil, false, err
	}
	if req.ClientID != "" {
		if msg, err := s.existingSend(ctx, me, channelID, req.ClientID); err != nil || msg != nil {
			return msg, false, err
		}
	}
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, me)
	if err != nil {
		return nil, false, err
	}
	if err := a.requireWritable(); err != nil {
		return nil, false, err
	}
	if a.Kind == kindDM {
		if err := s.checkDirectSend(ctx, s.d.Pool, channelID, me); err != nil {
			return nil, false, err
		}
	}
	var tags []string
	reply := req.ThreadRootID != nil
	switch {
	case reply && (title != "" || len(req.Tags) > 0):
		return nil, false, httpx.BadRequest("Replies don't have a title or tags.")
	case !reply && req.AlsoSendToChannel:
		return nil, false, httpx.BadRequest("Only thread replies can also be sent to the channel.")
	case a.Layout == layoutForum && !reply:
		if title == "" {
			return nil, false, httpx.BadRequest("Give your post a title.")
		}
		var err error
		if tags, err = cleanTags(req.Tags); err != nil {
			return nil, false, err
		}
		for _, t := range tags {
			if !slices.Contains(a.ForumTags, t) {
				return nil, false, httpx.BadRequest("Choose tags from this forum's list.")
			}
		}
	case a.Layout == layoutForum && req.AlsoSendToChannel:
		return nil, false, httpx.BadRequest("Forum replies stay in their post.")
	case title != "" || len(req.Tags) > 0:
		return nil, false, httpx.BadRequest("Only posts in forum channels have a title and tags.")
	}
	ments, err := s.resolveMentions(ctx, s.d.Pool, a, &me, body)
	if err != nil {
		return nil, false, err
	}
	tags = append([]string{}, tags...)

	var msgID uuid.UUID
	var rootAuthor *uuid.UUID
	retried := false
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if reply {
			var deleted bool
			var root *uuid.UUID
			err := tx.QueryRow(ctx, `
				SELECT sender_id, deleted_at IS NOT NULL, thread_root_id FROM channel_messages
				WHERE id = $1 AND channel_id = $2 AND kind <> 'system' FOR UPDATE`, *req.ThreadRootID, channelID).
				Scan(&rootAuthor, &deleted, &root)
			if db.IsNoRows(err) || (err == nil && root != nil) {
				return errThreadScope
			}
			if err != nil {
				return err
			}
			if deleted {
				return httpx.Conflict("That message was deleted.")
			}
		}
		if len(atts) > 0 {
			keys := make([]string, len(atts))
			for i, x := range atts {
				keys[i] = x.key
			}
			var gone bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM channel_deleted_objects WHERE storage_key = ANY($1))`, keys).Scan(&gone); err != nil {
				return err
			}
			if gone {
				return httpx.Conflict("That file was deleted. Upload it again.")
			}
		}
		var at time.Time
		err := tx.QueryRow(ctx, `
			INSERT INTO channel_messages (channel_id, sender_id, kind, body, thread_root_id, also_in_channel, title, tags, client_id)
			VALUES ($1, $2, 'text', $3, $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''))
			ON CONFLICT (sender_id, client_id) WHERE client_id IS NOT NULL DO NOTHING
			RETURNING id, created_at`, channelID, me, body, req.ThreadRootID, req.AlsoSendToChannel, title, tags, req.ClientID).Scan(&msgID, &at)
		if db.IsNoRows(err) { // a concurrent retry with the same client_id won
			retried = true
			return nil
		}
		if err != nil {
			return err
		}
		for i, x := range atts {
			_, err := tx.Exec(ctx, `
				INSERT INTO channel_attachments (message_id, position, kind, storage_key, content_type, file_name, size_bytes, width, height)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, msgID, i, x.kind, x.key, x.contentType, x.fileName, x.size, x.width, x.height)
			if db.IsUniqueViolation(err) {
				return httpx.Conflict("That file is already attached to another message.")
			}
			if err != nil {
				return err
			}
		}
		if err := saveMentions(ctx, tx, msgID, ments); err != nil {
			return err
		}
		return afterInsert(ctx, tx, channelID, msgID, at, &me, req.ThreadRootID, req.AlsoSendToChannel, rootAuthor, ments)
	})
	if err != nil {
		return nil, false, err
	}
	if retried {
		msg, err := s.existingSend(ctx, me, channelID, req.ClientID)
		return msg, false, err
	}
	msg, err := s.message(ctx, s.d.Pool, msgID, me)
	if err != nil {
		return nil, false, err
	}
	s.announceNew(ctx, a, msg, ments)
	return msg, true, nil
}

// afterInsert keeps counters, follows and read pointers in step with a new
// message (in the sender's transaction). sender is nil for bot messages.
func afterInsert(ctx context.Context, tx pgx.Tx, channelID, msgID uuid.UUID, at time.Time, sender, rootID *uuid.UUID, alsoInChannel bool, rootAuthor *uuid.UUID, ments mentions) error {
	timeline := rootID == nil || alsoInChannel
	if rootID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE channel_messages SET reply_count = reply_count + 1, last_reply_at = GREATEST(COALESCE(last_reply_at, $2), $2),
			  activity_at = GREATEST(activity_at, $2), updated_at = now()
			WHERE id = $1`, *rootID, at); err != nil {
			return err
		}
		// Whoever replies follows (again); the post's author and the people
		// mentioned start following unless they unfollowed.
		if sender != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO channel_thread_follows (root_id, user_id, following, last_read_reply_id, last_read_at) VALUES ($1, $2, true, $3, $4)
				ON CONFLICT (root_id, user_id) DO UPDATE SET following = true, last_read_reply_id = EXCLUDED.last_read_reply_id,
				  last_read_at = EXCLUDED.last_read_at, updated_at = now()`, *rootID, *sender, msgID, at); err != nil {
				return err
			}
		}
		followers := []uuid.UUID{}
		if rootAuthor != nil {
			followers = append(followers, *rootAuthor)
		}
		for id := range ments.Users {
			followers = append(followers, id)
		}
		if len(followers) > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO channel_thread_follows (root_id, user_id) SELECT $1, u FROM unnest($2::uuid[]) u
				ON CONFLICT (root_id, user_id) DO NOTHING`, *rootID, followers); err != nil {
				return err
			}
		}
	}
	if !timeline {
		return nil
	}
	// GREATEST: overlapping sends can commit out of timestamp order.
	if _, err := tx.Exec(ctx, `UPDATE channels SET last_message_at = GREATEST(last_message_at, $2), updated_at = now() WHERE id = $1`, channelID, at); err != nil {
		return err
	}
	// A new message brings a closed DM back for everyone.
	if _, err := tx.Exec(ctx, `UPDATE channel_members SET hidden_at = NULL, updated_at = now() WHERE channel_id = $1 AND hidden_at IS NOT NULL`, channelID); err != nil {
		return err
	}
	if sender == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE channel_members SET last_read_message_id = $3, last_read_at = $4, updated_at = now()
		WHERE channel_id = $1 AND user_id = $2
		  AND (last_read_at IS NULL OR (last_read_at, COALESCE(last_read_message_id, `+nilUUID+`)) < ($4, $3))`, channelID, *sender, msgID, at)
	return err
}

// existingSend returns the caller's message with clientID, nil if none.
func (s *Service) existingSend(ctx context.Context, me, channelID uuid.UUID, clientID string) (*Message, error) {
	var id, ch uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT id, channel_id FROM channel_messages WHERE sender_id = $1 AND client_id = $2`, me, clientID).Scan(&id, &ch)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ch != channelID {
		return nil, httpx.Conflict("That client_id was already used for another message.")
	}
	return s.message(ctx, s.d.Pool, id, me)
}

// checkDirectSend refuses a 1:1 DM to someone gone or blocked either way.
func (s *Service) checkDirectSend(ctx context.Context, q db.Querier, channelID, me uuid.UUID) error {
	var peer *uuid.UUID
	var status *string
	if err := q.QueryRow(ctx, `
		SELECT u.id, u.status FROM channel_members cm JOIN users u ON u.id = cm.user_id
		WHERE cm.channel_id = $1 AND cm.user_id <> $2 LIMIT 1`, channelID, me).Scan(&peer, &status); err != nil && !db.IsNoRows(err) {
		return err
	}
	if peer == nil {
		return nil // a note to yourself
	}
	if s := deref(status); s == "deleted" || s == "banned" {
		return httpx.NewError(http.StatusConflict, "recipient_unavailable", "This person can't receive messages.")
	}
	blocked, err := moderation.IsBlocked(ctx, q, me, *peer)
	if err != nil {
		return err
	}
	if blocked {
		return errBlocked
	}
	return nil
}

func (s *Service) parseAttachments(me uuid.UUID, in []attachmentIn) ([]attachmentRow, error) {
	out := make([]attachmentRow, 0, len(in))
	seen := map[string]bool{}
	for _, x := range in {
		key := strings.TrimSpace(x.Key)
		if !storage.KeyOwnedBy(key, PurposeAttachment, me) {
			return nil, httpx.BadRequest("Upload the file first, then send its key.")
		}
		if seen[key] {
			return nil, httpx.BadRequest("Each file can be attached once.")
		}
		seen[key] = true
		info, ok := attachmentExt[strings.ToLower(strings.TrimPrefix(path.Ext(key), "."))]
		if !ok {
			return nil, httpx.BadRequest("That file type isn't supported.")
		}
		positive := func(p *int) bool { return p == nil || (*p > 0 && *p <= 50000) }
		if !positive(x.Width) || !positive(x.Height) || (x.SizeBytes != nil && (*x.SizeBytes <= 0 || *x.SizeBytes > s.cfg.attachmentMax)) {
			return nil, httpx.BadRequest("File size, width and height must be positive.")
		}
		out = append(out, attachmentRow{key: key, kind: info.kind, contentType: info.contentType, fileName: cleanFileName(x.FileName),
			size: x.SizeBytes, width: x.Width, height: x.Height})
	}
	return out, nil
}

// ---- editing and deleting ------------------------------------------------------

type editReq struct {
	Body  *string   `json:"body"`
	Title *string   `json:"title"` // forum posts
	Tags  *[]string `json:"tags"`
}

// PATCH /channels/messages/{id} — the sender (within CHANNELS_EDIT_WINDOW
// when set). Mentions are re-read.
func (s *Service) editMessage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req editReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	switch {
	case m.SenderID == nil || *m.SenderID != me:
		return httpx.Forbidden("You can only edit your own messages.")
	case m.Kind != msgText || m.Deleted:
		return httpx.Conflict("This message can't be edited.")
	case m.Archived:
		return errArchived
	case s.cfg.editWindow > 0 && time.Since(m.CreatedAt) > s.cfg.editWindow:
		return httpx.NewError(http.StatusConflict, "edit_window_closed", "Messages can be edited for "+s.cfg.editWindow.String()+" after sending.")
	}
	body := m.Body
	if req.Body != nil {
		body = strings.TrimSpace(*req.Body)
	}
	if body == "" && !m.HasFiles {
		return httpx.BadRequest("Write a message, or delete it instead.")
	}
	if utf8.RuneCountInString(body) > s.cfg.maxChars {
		return httpx.BadRequest(fmt.Sprintf("Messages can be up to %d characters.", s.cfg.maxChars))
	}
	title := deref(m.Title)
	if req.Title != nil {
		if m.Title == nil {
			return httpx.BadRequest("Only forum posts have a title.")
		}
		title = strings.Join(strings.Fields(*req.Title), " ")
		if title == "" || utf8.RuneCountInString(title) > 200 {
			return httpx.BadRequest("Titles are 1 to 200 characters.")
		}
	}
	var tags []string
	if req.Tags != nil {
		if m.Title == nil {
			return httpx.BadRequest("Only forum posts have tags.")
		}
		if tags, err = cleanTags(*req.Tags); err != nil {
			return err
		}
		for _, t := range tags {
			if !slices.Contains(m.ForumTags, t) {
				return httpx.BadRequest("Choose tags from this forum's list.")
			}
		}
	}
	if err := checkText(ctx, body, title); err != nil {
		return err
	}
	ments, err := s.resolveMentions(ctx, s.d.Pool, m.access, &me, body)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE channel_messages SET body = $2, title = CASE WHEN title IS NULL THEN NULL ELSE $3 END,
			  tags = COALESCE($4, tags), edited_at = now(), updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL`, id, body, title, tags); err != nil {
			return err
		}
		return saveMentions(ctx, tx, id, ments)
	})
	if err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	s.publishUpdated(ctx, m.access, msg, true)
	httpx.OK(w, msg)
	return nil
}

// DELETE /channels/messages/{id} — the sender, or a channel or org admin
// (ActEdit). Text and files are erased; a "Message deleted" tombstone stays.
func (s *Service) deleteMessage(w http.ResponseWriter, r *http.Request) error {
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
	if m.Kind == msgSystem {
		return httpx.Conflict("This message can't be deleted.")
	}
	if m.Deleted {
		httpx.NoContent(w)
		return nil
	}
	by := "sender"
	if m.SenderID == nil || *m.SenderID != me {
		ok, err := s.can(ctx, s.d.Pool, m.access, ActEdit)
		if err != nil {
			return err
		}
		if !ok || !m.Member {
			return httpx.Forbidden("You can only delete your own messages.")
		}
		by = "moderator"
	}
	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := eraseMessages(ctx, tx, []uuid.UUID{id}, by); err != nil {
			return err
		}
		if m.ThreadRootID != nil {
			_, err := tx.Exec(ctx, `UPDATE channel_messages SET reply_count = GREATEST(reply_count - 1, 0), updated_at = now() WHERE id = $1`, *m.ThreadRootID)
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if msg, err := s.message(ctx, s.d.Pool, id, me); err == nil {
		s.publishUpdated(ctx, m.access, msg, true)
	}
	if m.ThreadRootID != nil {
		if root, err := s.message(ctx, s.d.Pool, *m.ThreadRootID, me); err == nil {
			s.publishUpdated(ctx, m.access, root, false)
		}
	}
	s.kickObjectCleanup(ctx)
	httpx.NoContent(w)
	return nil
}

// eraseMessages turns messages into tombstones and destroys their content:
// text and title blanked, attachments deleted (objects queued by trigger),
// reactions, mentions and pins removed.
func eraseMessages(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, by string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, sql := range []string{
		`DELETE FROM channel_attachments WHERE message_id = ANY($1)`,
		`DELETE FROM channel_reactions WHERE message_id = ANY($1)`,
		`DELETE FROM channel_mentions WHERE message_id = ANY($1)`,
	} {
		if _, err := tx.Exec(ctx, sql, ids); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `
		UPDATE channel_messages SET body = '', title = CASE WHEN title IS NULL THEN NULL ELSE 'Deleted post' END, tags = '{}',
		  edited_at = NULL, client_id = NULL, mention_channel = false, pinned_at = NULL, pinned_by = NULL,
		  deleted_at = COALESCE(deleted_at, now()), deleted_by = COALESCE(deleted_by, $2), updated_at = now()
		WHERE id = ANY($1) AND kind <> 'system'`, ids, by)
	return err
}

// hideMessage is staff removal: members see a tombstone, but the text and
// files stay for the moderation record (and Restore).
func hideMessage(ctx context.Context, q db.Querier, id uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE channel_messages SET deleted_at = now(), deleted_by = 'staff', updated_at = now()
		WHERE id = $1 AND kind <> 'system' AND deleted_at IS NULL`, id)
	return tag.RowsAffected() > 0, err
}

// ---- reactions and pins -------------------------------------------------------------

type reactionReq struct {
	Emoji string `json:"emoji"`
}

// validEmoji accepts free-form emoji (sequences, keycaps, flags) and refuses words.
func validEmoji(e string) bool {
	if e == "" || utf8.RuneCountInString(e) > 16 || len(e) > 64 {
		return false
	}
	symbol := false
	for _, r := range e {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
		symbol = symbol || r > 0x7f
	}
	return symbol
}

// reactable loads a message the caller may react to or pin.
func (s *Service) reactable(ctx context.Context, me, id uuid.UUID) (*messageAccess, error) {
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return nil, err
	}
	if err := m.requireWritable(); err != nil {
		return nil, err
	}
	if m.Kind == msgSystem || m.Deleted {
		return nil, httpx.Conflict("You can't do that to this message.")
	}
	if m.SenderID != nil {
		if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, me, *m.SenderID); err != nil || blocked {
			if err == nil {
				err = errMessageNotFound
			}
			return nil, err
		}
	}
	return m, nil
}

// POST /channels/messages/{id}/reactions {emoji} — returns the message.
func (s *Service) addReaction(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req reactionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	emoji := strings.TrimSpace(req.Emoji)
	if !validEmoji(emoji) {
		return httpx.BadRequest("Choose an emoji.")
	}
	m, err := s.reactable(ctx, me, id)
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `
		INSERT INTO channel_reactions (message_id, user_id, emoji)
		SELECT $1, $2, $3
		WHERE (SELECT count(*) FROM channel_reactions WHERE message_id = $1 AND user_id = $2) < $4
		ON CONFLICT (message_id, user_id, emoji) DO NOTHING`, id, me, emoji, maxReactionsPerUser)
	if err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 && !slices.ContainsFunc(msg.Reactions, func(x Reaction) bool {
		return x.Emoji == emoji && slices.Contains(x.UserIDs, me)
	}) {
		return httpx.BadRequest(fmt.Sprintf("You can add up to %d reactions to a message.", maxReactionsPerUser))
	}
	if tag.RowsAffected() > 0 {
		s.publishUpdated(ctx, m.access, msg, false)
	}
	httpx.OK(w, msg)
	return nil
}

// DELETE /channels/messages/{id}/reactions?emoji= — returns the message.
func (s *Service) removeReaction(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	emoji := strings.TrimSpace(r.URL.Query().Get("emoji"))
	if emoji == "" {
		return httpx.BadRequest("Choose an emoji.")
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM channel_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`, id, me, emoji)
	if err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publishUpdated(ctx, m.access, msg, false)
	}
	httpx.OK(w, msg)
	return nil
}

// PUT /channels/messages/{id}/pin — any member pins.
func (s *Service) pin(w http.ResponseWriter, r *http.Request) error { return s.setPinned(w, r, true) }

// DELETE /channels/messages/{id}/pin
func (s *Service) unpin(w http.ResponseWriter, r *http.Request) error {
	return s.setPinned(w, r, false)
}

func (s *Service) setPinned(w http.ResponseWriter, r *http.Request, pinned bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	m, err := s.reactable(ctx, me, id)
	if err != nil {
		return err
	}
	sql, args := `UPDATE channel_messages SET pinned_at = now(), pinned_by = $2, updated_at = now() WHERE id = $1 AND pinned_at IS NULL`, []any{id, me}
	if !pinned {
		sql, args = `UPDATE channel_messages SET pinned_at = NULL, pinned_by = NULL, updated_at = now() WHERE id = $1 AND pinned_at IS NOT NULL`, []any{id}
	}
	tag, err := s.d.Pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publishUpdated(ctx, m.access, msg, false)
		s.publish(ctx, m.ChannelID, EventChannel, ChannelEvent{ChannelID: m.ChannelID, Reason: "pins"})
	}
	httpx.OK(w, msg)
	return nil
}

// ---- attachments -------------------------------------------------------------------

// AttachmentURL is a fresh signed link to a private attachment.
type AttachmentURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GET /channels/attachments/{id} — anyone who may read the channel.
func (s *Service) attachmentURL(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var key, contentType, fileName string
	var msgID uuid.UUID
	var sender *uuid.UUID
	notFound := httpx.NotFound("File not found.")
	err = s.d.Pool.QueryRow(ctx, `
		SELECT a.storage_key, a.content_type, a.file_name, m.id, m.sender_id FROM channel_attachments a JOIN channel_messages m ON m.id = a.message_id
		WHERE a.id = $1 AND m.deleted_at IS NULL`, id).Scan(&key, &contentType, &fileName, &msgID, &sender)
	if db.IsNoRows(err) {
		return notFound
	}
	if err != nil {
		return err
	}
	if _, err := s.loadMessage(ctx, s.d.Pool, msgID, me); err != nil {
		return notFound
	}
	if sender != nil {
		if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, me, *sender); err != nil || blocked {
			if err == nil {
				err = notFound
			}
			return err
		}
	}
	u, err := s.signAttachment(ctx, key, contentType, fileName, attachmentURLTTL)
	if err != nil {
		return err
	}
	httpx.OK(w, AttachmentURL{URL: u, ExpiresAt: time.Now().Add(attachmentURLTTL).UTC()})
	return nil
}
