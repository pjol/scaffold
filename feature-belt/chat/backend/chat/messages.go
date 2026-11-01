package chat

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"regexp"
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

// window selects part of a conversation: the newest page, or before, after
// or around a message id (jump to message).
type window struct {
	conversationID        uuid.UUID
	clearedAt             *time.Time
	blocked               []uuid.UUID // nil = no filter (staff)
	staff                 bool        // staff view: hidden content stays readable
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

// GET /chat/conversations/{id}/messages?before=|after=|around=<message id>&limit=
func (s *Service) listMessages(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	win, err := parseWindow(r)
	if err != nil {
		return err
	}
	mb, err := loadMember(r.Context(), s.d.Pool, convID, me, false)
	if err != nil {
		return err
	}
	blocked, err := moderation.BlockedIDs(r.Context(), s.d.Pool, me)
	if err != nil {
		return err
	}
	win.conversationID, win.clearedAt, win.blocked = convID, mb.ClearedAt, blocked
	page, err := s.pageMessages(r.Context(), s.d.Pool, win)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// pageMessages returns a window, oldest first, keyset on (created_at, id).
func (s *Service) pageMessages(ctx context.Context, q db.Querier, w window) (*MessagePage, error) {
	resolve := func(id uuid.UUID) (*time.Time, error) {
		var t time.Time
		err := q.QueryRow(ctx, `SELECT created_at FROM chat_messages WHERE id = $1 AND conversation_id = $2`,
			id, w.conversationID).Scan(&t)
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
	if err := s.hydrate(ctx, q, page.Items, w.staff); err != nil {
		return nil, err
	}
	return page, nil
}

// fetchMessages reads up to limit messages compared to (t, id) with cmp,
// a constant ("<", ">", ">="). t nil = from the newest.
func (s *Service) fetchMessages(ctx context.Context, q db.Querier, w window, cmp string, t *time.Time, id uuid.UUID, desc bool, limit int) ([]*Message, error) {
	order := " ASC"
	if desc {
		order = " DESC"
	}
	rows, err := q.Query(ctx, messageSelect+`
		WHERE m.conversation_id = $1
		  AND ($2::timestamptz IS NULL OR m.created_at > $2)
		  AND ($3::uuid[] IS NULL OR m.kind = 'system' OR m.sender_id <> ALL($3))
		  AND ($4::timestamptz IS NULL OR (m.created_at, m.id) `+cmp+` ($4, $5::uuid))
		ORDER BY m.created_at`+order+`, m.id`+order+`
		LIMIT $6`, w.conversationID, w.clearedAt, w.blocked, t, id, limit)
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

// ---- sending -------------------------------------------------------------------

type attachmentIn struct {
	Key        string `json:"key"` // from POST /v1/uploads with purpose chat_attachment
	FileName   string `json:"file_name"`
	SizeBytes  *int64 `json:"size_bytes"`
	Width      *int   `json:"width"`
	Height     *int   `json:"height"`
	DurationMS *int   `json:"duration_ms"`
}

type sendReq struct {
	ClientID    string         `json:"client_id"` // client-generated; a retry with the same id returns the first message
	Body        string         `json:"body"`
	ReplyToID   *uuid.UUID     `json:"reply_to_id"`
	Attachments []attachmentIn `json:"attachments"`
}

type attachmentRow struct {
	key, kind, contentType, fileName string
	size                             *int64
	width, height, duration          *int
}

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// POST /chat/conversations/{id}/messages — 201 with the message; 200 with
// the original when client_id was already used for this conversation.
func (s *Service) sendMessage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req sendReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body := strings.TrimSpace(req.Body)
	switch {
	case req.ClientID != "" && !clientIDPattern.MatchString(req.ClientID):
		return httpx.BadRequest("client_id must be 1 to 64 letters, digits, dashes or underscores.")
	case body == "" && len(req.Attachments) == 0:
		return httpx.BadRequest("Write a message or attach a file.")
	case utf8.RuneCountInString(body) > s.cfg.maxChars:
		return httpx.BadRequest(fmt.Sprintf("Messages can be up to %d characters.", s.cfg.maxChars))
	case len(req.Attachments) > maxAttachments:
		return httpx.BadRequest(fmt.Sprintf("Attach up to %d files per message.", maxAttachments))
	}
	atts, err := s.parseAttachments(me, req.Attachments)
	if err != nil {
		return err
	}
	if body != "" {
		if err := moderation.Check(ctx, body); err != nil {
			return err
		}
	}
	if req.ClientID != "" {
		if msg, err := s.existingSend(ctx, me, convID, req.ClientID); err != nil || msg != nil {
			if err == nil {
				httpx.OK(w, msg)
			}
			return err
		}
	}

	var head convHead
	var msgID uuid.UUID
	retried := false
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		mb, err := loadMember(ctx, tx, convID, me, false)
		if err != nil {
			return err
		}
		head = mb.head()
		if mb.Kind == kindDirect {
			if err := checkDirectSend(ctx, tx, convID, me, mb.peer(me)); err != nil {
				return err
			}
		}
		if req.ReplyToID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM chat_messages WHERE id = $1 AND conversation_id = $2
				               AND ($3::timestamptz IS NULL OR created_at > $3))`,
				*req.ReplyToID, convID, mb.ClearedAt).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return errReplyScope
			}
		}
		if len(atts) > 0 {
			keys := make([]string, len(atts))
			for i, a := range atts {
				keys[i] = a.key
			}
			var gone bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM chat_deleted_objects WHERE storage_key = ANY($1))`, keys).Scan(&gone); err != nil {
				return err
			}
			if gone {
				return httpx.Conflict("That file was deleted. Upload it again.")
			}
		}
		var at time.Time
		err = tx.QueryRow(ctx, `
			INSERT INTO chat_messages (conversation_id, sender_id, kind, body, reply_to_id, client_id)
			VALUES ($1, $2, 'text', $3, $4, NULLIF($5, ''))
			ON CONFLICT (sender_id, client_id) WHERE client_id IS NOT NULL DO NOTHING
			RETURNING id, created_at`, convID, me, body, req.ReplyToID, req.ClientID).Scan(&msgID, &at)
		if db.IsNoRows(err) { // a concurrent retry with the same client_id won
			retried = true
			return nil
		}
		if err != nil {
			return err
		}
		for i, a := range atts {
			_, err := tx.Exec(ctx, `
				INSERT INTO chat_attachments (message_id, position, kind, storage_key, content_type, file_name, size_bytes, width, height, duration_ms)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
				msgID, i, a.kind, a.key, a.contentType, a.fileName, a.size, a.width, a.height, a.duration)
			if db.IsUniqueViolation(err) {
				return httpx.Conflict("That file is already attached to another message.")
			}
			if err != nil {
				return err
			}
		}
		// GREATEST: overlapping sends can commit out of timestamp order.
		if _, err := tx.Exec(ctx, `
			UPDATE chat_conversations SET last_message_at = GREATEST(last_message_at, $2), updated_at = now() WHERE id = $1`,
			convID, at); err != nil {
			return err
		}
		// A new message brings the conversation back for anyone who archived
		// or deleted it, and counts as read for the sender.
		if _, err := tx.Exec(ctx, `
			UPDATE chat_members SET archived_at = NULL, updated_at = now()
			WHERE conversation_id = $1 AND left_at IS NULL AND archived_at IS NOT NULL`, convID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE chat_members SET last_read_message_id = $3, last_read_at = $4, updated_at = now()
			WHERE conversation_id = $1 AND user_id = $2
			  AND (last_read_at IS NULL OR (last_read_at, COALESCE(last_read_message_id, `+nilUUID+`)) < ($4, $3))`,
			convID, me, msgID, at)
		return err
	})
	if err != nil {
		return err
	}
	if retried {
		msg, err := s.existingSend(ctx, me, convID, req.ClientID)
		if err != nil {
			return err
		}
		httpx.OK(w, msg)
		return nil
	}
	msg, err := s.message(ctx, s.d.Pool, msgID, false)
	if err != nil {
		return err
	}
	s.announceMessages(ctx, head, msg)
	httpx.JSON(w, http.StatusCreated, msg)
	return nil
}

// existingSend returns the caller's message with clientID, nil if there is none.
func (s *Service) existingSend(ctx context.Context, me, convID uuid.UUID, clientID string) (*Message, error) {
	var id, conv uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT id, conversation_id FROM chat_messages WHERE sender_id = $1 AND client_id = $2`,
		me, clientID).Scan(&id, &conv)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if conv != convID {
		return nil, httpx.Conflict("That client_id was already used for another message.")
	}
	return s.message(ctx, s.d.Pool, id, false)
}

// checkDirectSend refuses a direct message to someone gone or blocked.
func checkDirectSend(ctx context.Context, q db.Querier, convID, me, peer uuid.UUID) error {
	var status string
	var member bool
	err := q.QueryRow(ctx, `
		SELECT u.status, EXISTS (SELECT 1 FROM chat_members WHERE conversation_id = $2 AND user_id = u.id AND left_at IS NULL)
		FROM users u WHERE u.id = $1`, peer, convID).Scan(&status, &member)
	if db.IsNoRows(err) || status == "deleted" || status == "banned" || (err == nil && !member) {
		return errRecipientGone
	}
	if err != nil {
		return err
	}
	blocked, err := moderation.IsBlocked(ctx, q, me, peer)
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
	for _, a := range in {
		key := strings.TrimSpace(a.Key)
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
		positive := func(p *int, limit int) bool { return p == nil || (*p > 0 && *p <= limit) }
		if !positive(a.Width, 50000) || !positive(a.Height, 50000) ||
			(a.DurationMS != nil && *a.DurationMS < 0) ||
			(a.SizeBytes != nil && (*a.SizeBytes <= 0 || *a.SizeBytes > s.cfg.attachmentMax)) {
			return nil, httpx.BadRequest("File size, width, height and duration must be positive.")
		}
		out = append(out, attachmentRow{key: key, kind: info.kind, contentType: info.contentType,
			fileName: cleanFileName(a.FileName), size: a.SizeBytes, width: a.Width, height: a.Height, duration: a.DurationMS})
	}
	return out, nil
}

// ---- editing and deleting ------------------------------------------------------

type editReq struct {
	Body string `json:"body"`
}

// PATCH /chat/messages/{id} — the sender, within CHAT_EDIT_WINDOW.
func (s *Service) editMessage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	msgID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req editReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body := strings.TrimSpace(req.Body)
	if utf8.RuneCountInString(body) > s.cfg.maxChars {
		return httpx.BadRequest(fmt.Sprintf("Messages can be up to %d characters.", s.cfg.maxChars))
	}
	st, err := s.messageState(ctx, me, msgID)
	if err != nil {
		return err
	}
	switch {
	case st.sender != me:
		return httpx.Forbidden("You can only edit your own messages.")
	case st.kind != kindText || st.deleted:
		return httpx.Conflict("This message can't be edited.")
	case body == "" && !st.hasFiles:
		return httpx.BadRequest("Write a message, or delete it instead.")
	case s.cfg.editWindow <= 0:
		return httpx.NewError(http.StatusConflict, "edit_window_closed", "Messages can't be edited.")
	}
	if body != st.body {
		if body != "" {
			if err := moderation.Check(ctx, body); err != nil {
				return err
			}
		}
		// The window is checked in the UPDATE so it can't pass and then expire.
		tag, err := s.d.Pool.Exec(ctx, `
			UPDATE chat_messages SET body = $2, edited_at = now(), updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL AND created_at > now() - make_interval(secs => $3)`,
			msgID, body, s.cfg.editWindow.Seconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NewError(http.StatusConflict, "edit_window_closed",
				"Messages can be edited for "+humanDuration(s.cfg.editWindow)+" after sending.")
		}
	}
	msg, err := s.message(ctx, s.d.Pool, msgID, false)
	if err != nil {
		return err
	}
	if body != st.body {
		s.publishUpdated(ctx, msg, true)
	}
	httpx.OK(w, msg)
	return nil
}

// DELETE /chat/messages/{id} — the sender deletes for everyone. The text and
// files are erased; the message stays as a "Message deleted" tombstone.
func (s *Service) deleteMessage(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	msgID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	st, err := s.messageState(ctx, me, msgID)
	if err != nil {
		return err
	}
	switch {
	case st.sender != me:
		return httpx.Forbidden("You can only delete your own messages.")
	case st.kind != kindText:
		return httpx.Conflict("This message can't be deleted.")
	case st.deleted:
		httpx.NoContent(w)
		return nil
	}
	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		return eraseMessages(ctx, tx, []uuid.UUID{msgID}, "sender")
	}); err != nil {
		return err
	}
	if msg, err := s.message(ctx, s.d.Pool, msgID, false); err == nil {
		s.publishUpdated(ctx, msg, true)
	}
	s.kickObjectCleanup()
	httpx.NoContent(w)
	return nil
}

type messageState struct {
	conversationID uuid.UUID
	sender         uuid.UUID
	kind, body     string
	deleted        bool
	hasFiles       bool
}

// messageState reads a message the caller can see as a member, or 404.
func (s *Service) messageState(ctx context.Context, me, msgID uuid.UUID) (*messageState, error) {
	var st messageState
	err := s.d.Pool.QueryRow(ctx, `
		SELECT m.conversation_id, m.sender_id, m.kind, m.body, m.deleted_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM chat_attachments a WHERE a.message_id = m.id)
		FROM chat_messages m
		JOIN chat_members mb ON mb.conversation_id = m.conversation_id AND mb.user_id = $2 AND mb.left_at IS NULL
		WHERE m.id = $1 AND (mb.cleared_at IS NULL OR m.created_at > mb.cleared_at)`, msgID, me).
		Scan(&st.conversationID, &st.sender, &st.kind, &st.body, &st.deleted, &st.hasFiles)
	if db.IsNoRows(err) {
		return nil, errMessageNotFound
	}
	return &st, err
}

// eraseMessages turns messages into tombstones and destroys their content:
// text blanked, attachment rows deleted (objects queued), reactions removed.
// Messages already hidden by staff are erased too.
func eraseMessages(ctx context.Context, tx pgx.Tx, ids []uuid.UUID, by string) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_deleted_objects (storage_key)
		SELECT storage_key FROM chat_attachments WHERE message_id = ANY($1)
		ON CONFLICT (storage_key) DO NOTHING`, ids); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_attachments WHERE message_id = ANY($1)`, ids); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chat_reactions WHERE message_id = ANY($1)`, ids); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE chat_messages SET body = '', edited_at = NULL, client_id = NULL,
		  deleted_at = COALESCE(deleted_at, now()), deleted_by = COALESCE(deleted_by, $2), updated_at = now()
		WHERE id = ANY($1) AND kind = 'text'`, ids, by)
	return err
}

// hideMessage is staff removal: members see a tombstone, but the text and
// files stay for the moderation record (and Restore). Reports return false
// when the message was already deleted or hidden.
func hideMessage(ctx context.Context, q db.Querier, id uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE chat_messages SET deleted_at = now(), deleted_by = 'staff', updated_at = now()
		WHERE id = $1 AND kind = 'text' AND deleted_at IS NULL`, id)
	return tag.RowsAffected() > 0, err
}

func humanDuration(d time.Duration) string {
	switch {
	case d%time.Hour == 0 && d >= time.Hour:
		return plural(int(d/time.Hour), "hour")
	case d%time.Minute == 0 && d >= time.Minute:
		return plural(int(d/time.Minute), "minute")
	default:
		return plural(int(d.Round(time.Second)/time.Second), "second")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

// ---- reactions -------------------------------------------------------------------

type reactionReq struct {
	Emoji string `json:"emoji"`
}

// validEmoji accepts free-form emoji (including sequences and keycaps) and
// refuses words.
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

// POST /chat/messages/{id}/reactions {emoji} — returns the message.
func (s *Service) addReaction(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	msgID, err := httpx.URLParamUUID(r, "id")
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
	st, err := s.messageState(ctx, me, msgID)
	if err != nil {
		return err
	}
	if st.kind != kindText || st.deleted {
		return httpx.Conflict("You can't react to this message.")
	}
	if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, me, st.sender); err != nil || blocked {
		if err == nil {
			err = errMessageNotFound
		}
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `
		INSERT INTO chat_reactions (message_id, user_id, emoji)
		SELECT $1, $2, $3
		WHERE (SELECT count(*) FROM chat_reactions WHERE message_id = $1 AND user_id = $2) < $4
		ON CONFLICT (message_id, user_id, emoji) DO NOTHING`, msgID, me, emoji, maxReactionsPerUser)
	if err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, msgID, false)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 && !slices.ContainsFunc(msg.Reactions, func(x Reaction) bool {
		return x.Emoji == emoji && slices.Contains(x.UserIDs, me)
	}) {
		return httpx.BadRequest(fmt.Sprintf("You can add up to %d reactions to a message.", maxReactionsPerUser))
	}
	if tag.RowsAffected() > 0 {
		s.publishUpdated(ctx, msg, false)
	}
	httpx.OK(w, msg)
	return nil
}

// DELETE /chat/messages/{id}/reactions?emoji= — returns the message.
func (s *Service) removeReaction(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	msgID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	emoji := strings.TrimSpace(r.URL.Query().Get("emoji"))
	if emoji == "" {
		return httpx.BadRequest("Choose an emoji.")
	}
	if _, err := s.messageState(ctx, me, msgID); err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM chat_reactions WHERE message_id = $1 AND user_id = $2 AND emoji = $3`,
		msgID, me, emoji)
	if err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, msgID, false)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publishUpdated(ctx, msg, false)
	}
	httpx.OK(w, msg)
	return nil
}

// ---- attachments -----------------------------------------------------------------

// AttachmentURL is a fresh signed link to a private attachment.
type AttachmentURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// GET /chat/attachments/{id} — members only.
func (s *Service) attachmentURL(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var key string
	var sender uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		SELECT a.storage_key, m.sender_id
		FROM chat_attachments a
		JOIN chat_messages m ON m.id = a.message_id
		JOIN chat_members mb ON mb.conversation_id = m.conversation_id AND mb.user_id = $2 AND mb.left_at IS NULL
		WHERE a.id = $1 AND m.deleted_at IS NULL AND (mb.cleared_at IS NULL OR m.created_at > mb.cleared_at)`, id, me).
		Scan(&key, &sender)
	notFound := httpx.NotFound("File not found.")
	if db.IsNoRows(err) {
		return notFound
	}
	if err != nil {
		return err
	}
	if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, me, sender); err != nil || blocked {
		if err == nil {
			err = notFound
		}
		return err
	}
	u, err := s.d.Storage.PresignGet(ctx, key, attachmentURLTTL)
	if err != nil {
		return err
	}
	httpx.OK(w, AttachmentURL{URL: u, ExpiresAt: time.Now().Add(attachmentURLTTL).UTC()})
	return nil
}
