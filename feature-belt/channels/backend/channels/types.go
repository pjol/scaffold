package channels

import (
	"context"
	"log/slog"
	"mime"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/storage"
)

const (
	kindPublic  = "public"
	kindPrivate = "private"
	kindDM      = "dm"
	kindGroupDM = "group_dm"

	layoutChat  = "chat"
	layoutForum = "forum"

	msgText   = "text"
	msgSystem = "system"
	msgBot    = "bot"

	roleAdmin  = "admin"
	roleMember = "member"
)

// UserRef is a person as channels show them. A deleted account reads "Deleted user".
type UserRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	Deleted     bool      `json:"deleted"`
}

func userRef(id uuid.UUID, name string, avatar *string, status string) UserRef {
	if status == "deleted" {
		return UserRef{ID: id, DisplayName: "Deleted user", Deleted: true}
	}
	if strings.TrimSpace(name) == "" {
		name = "Member"
	}
	return UserRef{ID: id, DisplayName: name, AvatarURL: avatar}
}

// Message is one message, from the caller's point of view.
type Message struct {
	ID            uuid.UUID    `json:"id"`
	ChannelID     uuid.UUID    `json:"channel_id"`
	Kind          string       `json:"kind"`     // text | system | bot
	Sender        *UserRef     `json:"sender"`   // null for bot messages
	BotName       *string      `json:"bot_name"` // bot messages
	Body          string       `json:"body"`     // markdown with mention tokens; "" once deleted
	Event         *string      `json:"event"`    // system: created, joined, left, added, removed, renamed, archived, unarchived, topic, converted
	Subject       *UserRef     `json:"subject"`  // system: who the event is about
	ThreadRootID  *uuid.UUID   `json:"thread_root_id"`
	AlsoInChannel bool         `json:"also_in_channel"`
	Title         *string      `json:"title"` // forum posts
	Tags          []string     `json:"tags"`
	Mentions      []UserRef    `json:"mentions"` // people the body's <@id> tokens name, with current names
	ReplyCount    int          `json:"reply_count"`
	LastReplyAt   *time.Time   `json:"last_reply_at"`
	ReplyUsers    []UserRef    `json:"reply_users"` // up to 4 most recent repliers
	Attachments   []Attachment `json:"attachments"`
	Reactions     []Reaction   `json:"reactions"`
	Pinned        bool         `json:"pinned"`
	Saved         bool         `json:"saved"` // in the caller's Later list
	ClientID      *string      `json:"client_id"`
	EditedAt      *time.Time   `json:"edited_at"`
	Deleted       bool         `json:"deleted"`
	CreatedAt     time.Time    `json:"created_at"`
	DeletedBy     *string      `json:"deleted_by,omitempty"` // staff view only

	deletedBy *string
	senderID  *uuid.UUID
}

// Attachment is a file on a message. URL is a short-lived signed link;
// GET /v1/channels/attachments/{id} issues a fresh one.
type Attachment struct {
	ID           uuid.UUID `json:"id"`
	Kind         string    `json:"kind"` // image | video | audio | file
	ContentType  string    `json:"content_type"`
	FileName     string    `json:"file_name"`
	SizeBytes    *int64    `json:"size_bytes"`
	Width        *int      `json:"width"`
	Height       *int      `json:"height"`
	URL          string    `json:"url"`
	URLExpiresAt time.Time `json:"url_expires_at"`
}

// Reaction is one emoji on a message and who added it, oldest first.
type Reaction struct {
	Emoji   string      `json:"emoji"`
	Count   int         `json:"count"`
	UserIDs []uuid.UUID `json:"user_ids"`
}

// MessagePage is a window of a timeline or thread, oldest first.
type MessagePage struct {
	Items    []*Message `json:"items"`
	HasOlder bool       `json:"has_older"`
	HasNewer bool       `json:"has_newer"`
}

// ---- message loading -----------------------------------------------------------

// messageSelect reads messages with their sender and subject. Callers append
// WHERE/ORDER BY on alias m.
const messageSelect = `
	SELECT m.id, m.channel_id, m.kind, m.body, m.bot_name, m.thread_root_id, m.also_in_channel, m.title, m.tags,
	       m.reply_count, m.last_reply_at, m.pinned_at IS NOT NULL, m.client_id, m.edited_at, m.deleted_at, m.deleted_by, m.created_at,
	       m.sender_id, su.display_name, su.avatar_url, su.status,
	       m.subject_user_id, sj.display_name, sj.avatar_url, sj.status
	FROM channel_messages m
	LEFT JOIN users su ON su.id = m.sender_id
	LEFT JOIN users sj ON sj.id = m.subject_user_id`

// scanMessages reads messageSelect rows. Deleted or hidden messages come back
// as tombstones, except in the staff view, which keeps what is left of them.
func scanMessages(rows pgx.Rows, staff bool) ([]*Message, error) {
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		var (
			m                           Message
			body                        string
			deletedAt                   *time.Time
			senderID, subjectID         *uuid.UUID
			senderName, senderStatus    *string
			subjectName, subjectStatus  *string
			senderAvatar, subjectAvatar *string
		)
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Kind, &body, &m.BotName, &m.ThreadRootID, &m.AlsoInChannel, &m.Title, &m.Tags,
			&m.ReplyCount, &m.LastReplyAt, &m.Pinned, &m.ClientID, &m.EditedAt, &deletedAt, &m.deletedBy, &m.CreatedAt,
			&senderID, &senderName, &senderAvatar, &senderStatus,
			&subjectID, &subjectName, &subjectAvatar, &subjectStatus); err != nil {
			return nil, err
		}
		m.senderID = senderID
		if senderID != nil {
			u := userRef(*senderID, deref(senderName), senderAvatar, deref(senderStatus))
			m.Sender = &u
		}
		if subjectID != nil {
			u := userRef(*subjectID, deref(subjectName), subjectAvatar, deref(subjectStatus))
			m.Subject = &u
		}
		m.Deleted = deletedAt != nil
		switch {
		case m.Kind == msgSystem:
			ev := body
			m.Event = &ev
		case !m.Deleted || staff:
			m.Body = body
		}
		if m.Deleted && !staff {
			m.Title, m.Tags = nil, []string{}
		}
		if m.Tags == nil {
			m.Tags = []string{}
		}
		m.Mentions, m.ReplyUsers, m.Attachments, m.Reactions = []UserRef{}, []UserRef{}, []Attachment{}, []Reaction{}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// hydrate fills attachments (signed), reactions, mentioned people, recent
// repliers and the viewer's saved flag (staff: for hidden messages too).
func (s *Service) hydrate(ctx context.Context, q db.Querier, msgs []*Message, viewer uuid.UUID, staff bool) error {
	byID := map[uuid.UUID]*Message{}
	ids, roots := []uuid.UUID{}, []uuid.UUID{}
	mentioned := map[uuid.UUID]bool{}
	for _, m := range msgs {
		if m.Kind == msgSystem || (m.Deleted && !staff) {
			continue
		}
		byID[m.ID] = m
		ids = append(ids, m.ID)
		if m.ReplyCount > 0 {
			roots = append(roots, m.ID)
		}
		for _, id := range mentionedUserIDs(m.Body) {
			mentioned[id] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `
		SELECT id, message_id, kind, storage_key, content_type, file_name, size_bytes, width, height
		FROM channel_attachments WHERE message_id = ANY($1) ORDER BY message_id, position`, ids)
	if err != nil {
		return err
	}
	expires := time.Now().Add(attachmentURLTTL).UTC()
	for rows.Next() {
		var a Attachment
		var msgID uuid.UUID
		var key string
		if err := rows.Scan(&a.ID, &msgID, &a.Kind, &key, &a.ContentType, &a.FileName, &a.SizeBytes, &a.Width, &a.Height); err != nil {
			rows.Close()
			return err
		}
		if a.URL, err = s.signAttachment(ctx, key, a.ContentType, a.FileName, attachmentURLTTL); err != nil {
			slog.WarnContext(ctx, "channels: presign attachment", "attachment_id", a.ID, "err", err)
		}
		a.URLExpiresAt = expires
		byID[msgID].Attachments = append(byID[msgID].Attachments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	rows, err = q.Query(ctx, `
		SELECT message_id, emoji, array_agg(user_id ORDER BY created_at, user_id)
		FROM channel_reactions WHERE message_id = ANY($1)
		GROUP BY message_id, emoji ORDER BY message_id, min(created_at), emoji`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var msgID uuid.UUID
		var r Reaction
		if err := rows.Scan(&msgID, &r.Emoji, &r.UserIDs); err != nil {
			rows.Close()
			return err
		}
		r.Count = len(r.UserIDs)
		byID[msgID].Reactions = append(byID[msgID].Reactions, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(roots) > 0 {
		rows, err = q.Query(ctx, `
			SELECT r.root, u.id, u.display_name, u.avatar_url, u.status
			FROM unnest($1::uuid[]) AS r(root)
			CROSS JOIN LATERAL (
			    SELECT x.sender_id, max(x.created_at) AS at FROM channel_messages x
			    WHERE x.thread_root_id = r.root AND x.sender_id IS NOT NULL AND x.deleted_at IS NULL
			    GROUP BY x.sender_id ORDER BY at DESC LIMIT 4
			) p
			JOIN users u ON u.id = p.sender_id
			ORDER BY r.root, p.at DESC`, roots)
		if err != nil {
			return err
		}
		for rows.Next() {
			var root, uid uuid.UUID
			var name, status string
			var avatar *string
			if err := rows.Scan(&root, &uid, &name, &avatar, &status); err != nil {
				rows.Close()
				return err
			}
			byID[root].ReplyUsers = append(byID[root].ReplyUsers, userRef(uid, name, avatar, status))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	if len(mentioned) > 0 {
		mids := make([]uuid.UUID, 0, len(mentioned))
		for id := range mentioned {
			mids = append(mids, id)
		}
		users, err := loadUsers(ctx, q, mids)
		if err != nil {
			return err
		}
		for _, m := range byID {
			for _, id := range mentionedUserIDs(m.Body) {
				if u, ok := users[id]; ok {
					m.Mentions = append(m.Mentions, u)
				}
			}
		}
	}

	if viewer != uuid.Nil {
		rows, err = q.Query(ctx, `SELECT message_id FROM channel_saved WHERE user_id = $1 AND message_id = ANY($2) AND completed_at IS NULL`, viewer, ids)
		if err != nil {
			return err
		}
		saved, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range saved {
			byID[id].Saved = true
		}
	}
	return nil
}

// messagesByID loads and hydrates messages by id.
func (s *Service) messagesByID(ctx context.Context, q db.Querier, ids []uuid.UUID, viewer uuid.UUID, staff bool) (map[uuid.UUID]*Message, error) {
	out := map[uuid.UUID]*Message{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, messageSelect+` WHERE m.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	msgs, err := scanMessages(rows, staff)
	if err != nil {
		return nil, err
	}
	if err := s.hydrate(ctx, q, msgs, viewer, staff); err != nil {
		return nil, err
	}
	for _, m := range msgs {
		out[m.ID] = m
	}
	return out, nil
}

// message loads one hydrated message.
func (s *Service) message(ctx context.Context, q db.Querier, id, viewer uuid.UUID) (*Message, error) {
	m, err := s.messagesByID(ctx, q, []uuid.UUID{id}, viewer, false)
	if err != nil {
		return nil, err
	}
	if msg, ok := m[id]; ok {
		return msg, nil
	}
	return nil, errMessageNotFound
}

// loadUsers returns UserRefs for ids (missing ids are left out).
func loadUsers(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]UserRef, error) {
	out := map[uuid.UUID]UserRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT id, display_name, avatar_url, status FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&id, &name, &avatar, &status); err != nil {
			return nil, err
		}
		out[id] = userRef(id, name, avatar, status)
	}
	return out, rows.Err()
}

// ---- attachments ---------------------------------------------------------------------

// attachmentTypes: photos, video, audio and documents.
var attachmentTypes = append([]string{
	"image/jpeg", "image/png", "image/heic", "image/webp", "image/gif",
	"video/mp4", "video/quicktime", "audio/mp4", "audio/mpeg",
}, storage.DocumentTypes...)

// attachmentExt maps a storage key's extension to its type and kind. Keys are
// issued by storage.PresignPut, so the extension is the validated type.
var attachmentExt = map[string]struct{ contentType, kind string }{
	"jpg": {"image/jpeg", "image"}, "png": {"image/png", "image"}, "heic": {"image/heic", "image"},
	"webp": {"image/webp", "image"}, "gif": {"image/gif", "image"},
	"mp4": {"video/mp4", "video"}, "mov": {"video/quicktime", "video"},
	"m4a": {"audio/mp4", "audio"}, "mp3": {"audio/mpeg", "audio"},
	"pdf": {"application/pdf", "file"}, "txt": {"text/plain", "file"}, "csv": {"text/csv", "file"},
	"zip":  {"application/zip", "file"},
	"docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "file"},
	"xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "file"},
	"pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation", "file"},
}

// ---- small helpers -------------------------------------------------------------------

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

const nilUUID = `'00000000-0000-0000-0000-000000000000'::uuid`

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// truncate cuts s to n runes, adding an ellipsis when it cut.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

// escapeLike escapes LIKE wildcards in user input.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// cleanFileName strips paths, control characters and quotes, and caps the length.
func cleanFileName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if name == "." || name == "/" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\'' || r == '`' {
			return -1
		}
		return r
	}, name)
	if utf8.RuneCountInString(name) > 160 {
		name = string([]rune(name)[:160])
	}
	return name
}

func uuidSet(ids []uuid.UUID) map[uuid.UUID]bool {
	m := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func dedupe(ids []uuid.UUID, except ...uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{uuid.Nil: true}
	for _, e := range except {
		seen[e] = true
	}
	out := []uuid.UUID{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

var clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// signAttachment presigns a download that opens what the browser can show
// and saves the rest under the attachment's name (not the storage key's).
func (s *Service) signAttachment(ctx context.Context, key, contentType, fileName string, ttl time.Duration) (string, error) {
	disp := "inline"
	if d := mime.FormatMediaType("inline", map[string]string{"filename": fileName}); fileName != "" && d != "" {
		disp = d
	}
	return s.d.Storage.PresignGetWith(ctx, key, ttl, storage.GetOptions{ContentType: contentType, Disposition: disp})
}
