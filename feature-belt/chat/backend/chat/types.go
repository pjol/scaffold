package chat

import (
	"bytes"
	"context"
	"log/slog"
	"mime"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/storage"
)

// UserRef is a person as chat shows them. A deleted account reads "Deleted user".
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

// ReadMark is a read pointer: the newest message someone has seen.
type ReadMark struct {
	MessageID uuid.UUID `json:"message_id"`
	At        time.Time `json:"at"` // that message's created_at
}

func readMark(id *uuid.UUID, at *time.Time) *ReadMark {
	if id == nil || at == nil {
		return nil
	}
	return &ReadMark{MessageID: *id, At: *at}
}

// Conversation is one inbox row, from the caller's point of view.
type Conversation struct {
	ID            uuid.UUID  `json:"id"`
	Kind          string     `json:"kind"`  // direct | group
	Title         string     `json:"title"` // group title, or the other person's name
	AvatarURL     *string    `json:"avatar_url"`
	Peer          *UserRef   `json:"peer"`    // direct only
	Blocked       bool       `json:"blocked"` // direct only: a block either way; sending is refused
	Role          string     `json:"role"`    // owner | admin | member
	MemberCount   int        `json:"member_count"`
	Unread        int        `json:"unread"`
	Muted         bool       `json:"muted"`
	MutedUntil    *time.Time `json:"muted_until"` // null when not muted or muted until unmuted
	Archived      bool       `json:"archived"`
	LastRead      *ReadMark  `json:"last_read"`   // the caller's read pointer
	OthersRead    *ReadMark  `json:"others_read"` // read by every other member up to here
	LastMessage   *Message   `json:"last_message"`
	LastMessageAt time.Time  `json:"last_message_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Member is one active member of a conversation.
type Member struct {
	User     UserRef   `json:"user"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
	LastRead *ReadMark `json:"last_read"`
}

// ConversationDetail is a conversation with its members. HiddenUserIDs are
// members with a block either way with the caller: their messages are left
// out of reads, and clients drop their live events (topic events are not
// filtered per viewer).
type ConversationDetail struct {
	Conversation
	Members       []Member    `json:"members"`
	HiddenUserIDs []uuid.UUID `json:"hidden_user_ids"`
}

// Message is one message. System messages carry Event instead of Body.
type Message struct {
	ID             uuid.UUID     `json:"id"`
	ConversationID uuid.UUID     `json:"conversation_id"`
	Sender         UserRef       `json:"sender"`
	Kind           string        `json:"kind"` // text | system
	Body           string        `json:"body"`
	Event          *string       `json:"event"`   // system: created, member_added, member_removed, member_left, title_changed, avatar_changed, owner_changed
	Subject        *UserRef      `json:"subject"` // system: the member the event is about
	ClientID       *string       `json:"client_id"`
	ReplyTo        *ReplyPreview `json:"reply_to"`
	Attachments    []Attachment  `json:"attachments"`
	Reactions      []Reaction    `json:"reactions"`
	EditedAt       *time.Time    `json:"edited_at"`
	Deleted        bool          `json:"deleted"`
	CreatedAt      time.Time     `json:"created_at"`
	DeletedBy      *string       `json:"deleted_by,omitempty"` // staff view only: sender | staff | account_deleted

	deletedBy *string
}

// ReplyPreview is the quoted message shown above a reply.
type ReplyPreview struct {
	ID             uuid.UUID `json:"id"`
	Sender         UserRef   `json:"sender"`
	Body           string    `json:"body"` // first 200 characters
	HasAttachments bool      `json:"has_attachments"`
	Deleted        bool      `json:"deleted"`
}

// Attachment is a file on a message. URL is a short-lived signed link;
// GET /v1/chat/attachments/{id} issues a fresh one.
type Attachment struct {
	ID           uuid.UUID `json:"id"`
	Kind         string    `json:"kind"` // image | video | audio | file
	ContentType  string    `json:"content_type"`
	FileName     string    `json:"file_name"`
	SizeBytes    *int64    `json:"size_bytes"`
	Width        *int      `json:"width"`
	Height       *int      `json:"height"`
	DurationMS   *int      `json:"duration_ms"`
	URL          string    `json:"url"`
	URLExpiresAt time.Time `json:"url_expires_at"`

	key string
}

// Reaction is one emoji on a message and who added it, oldest first.
type Reaction struct {
	Emoji   string      `json:"emoji"`
	Count   int         `json:"count"`
	UserIDs []uuid.UUID `json:"user_ids"`
}

// MessagePage is a window of a conversation, oldest first.
type MessagePage struct {
	Items    []*Message `json:"items"`
	HasOlder bool       `json:"has_older"`
	HasNewer bool       `json:"has_newer"`
}

const (
	kindText   = "text"
	kindSystem = "system"
	kindDirect = "direct"
	kindGroup  = "group"

	roleOwner  = "owner"
	roleAdmin  = "admin"
	roleMember = "member"
)

// ---- message loading ---------------------------------------------------------

// messageSelect reads messages with their sender, subject and reply preview.
// Callers append WHERE/ORDER BY on alias m.
const messageSelect = `
	SELECT m.id, m.conversation_id, m.kind, m.body, m.client_id, m.edited_at, m.deleted_at, m.deleted_by, m.created_at,
	       m.sender_id, su.display_name, su.avatar_url, su.status,
	       m.subject_user_id, sj.display_name, sj.avatar_url, sj.status,
	       r.id, r.body, r.deleted_at, r.sender_id, ru.display_name, ru.avatar_url, ru.status,
	       (r.id IS NOT NULL AND EXISTS (SELECT 1 FROM chat_attachments ra WHERE ra.message_id = r.id))
	FROM chat_messages m
	JOIN users su ON su.id = m.sender_id
	LEFT JOIN users sj ON sj.id = m.subject_user_id
	LEFT JOIN chat_messages r ON r.id = m.reply_to_id
	LEFT JOIN users ru ON ru.id = r.sender_id`

// scanMessages reads messageSelect rows. Hidden or deleted messages come back
// as tombstones, except in the staff view, which keeps what is left of them.
func scanMessages(rows pgx.Rows, staff bool) ([]*Message, error) {
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		var (
			m                               Message
			body                            string
			deletedAt                       *time.Time
			senderID                        uuid.UUID
			senderName, senderStatus        string
			senderAvatar                    *string
			subjectID                       *uuid.UUID
			subjectName, subjectStatus      *string
			subjectAvatar                   *string
			replyID, replySender            *uuid.UUID
			replyBody, replyName, replyStat *string
			replyAvatar                     *string
			replyDeleted                    *time.Time
			replyFiles                      bool
		)
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Kind, &body, &m.ClientID, &m.EditedAt, &deletedAt, &m.deletedBy, &m.CreatedAt,
			&senderID, &senderName, &senderAvatar, &senderStatus,
			&subjectID, &subjectName, &subjectAvatar, &subjectStatus,
			&replyID, &replyBody, &replyDeleted, &replySender, &replyName, &replyAvatar, &replyStat, &replyFiles); err != nil {
			return nil, err
		}
		m.Sender = userRef(senderID, senderName, senderAvatar, senderStatus)
		m.Deleted = deletedAt != nil
		if m.Kind == kindSystem {
			ev := body
			m.Event = &ev
		} else if !m.Deleted || staff {
			m.Body = body
		}
		if subjectID != nil {
			u := userRef(*subjectID, deref(subjectName), subjectAvatar, deref(subjectStatus))
			m.Subject = &u
		}
		if replyID != nil && replySender != nil {
			p := &ReplyPreview{ID: *replyID, Sender: userRef(*replySender, deref(replyName), replyAvatar, deref(replyStat)),
				Deleted: replyDeleted != nil}
			if !p.Deleted {
				p.Body = truncate(deref(replyBody), 200)
				p.HasAttachments = replyFiles
			}
			m.ReplyTo = p
		}
		m.Attachments, m.Reactions = []Attachment{}, []Reaction{}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// hydrate loads attachments (with signed URLs) and reactions for msgs
// (staff: for hidden messages too).
func (s *Service) hydrate(ctx context.Context, q db.Querier, msgs []*Message, staff bool) error {
	byID := map[uuid.UUID]*Message{}
	ids := []uuid.UUID{}
	for _, m := range msgs {
		if m.Kind == kindText && (!m.Deleted || staff) {
			byID[m.ID] = m
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `
		SELECT id, message_id, kind, storage_key, content_type, file_name, size_bytes, width, height, duration_ms
		FROM chat_attachments WHERE message_id = ANY($1) ORDER BY message_id, position`, ids)
	if err != nil {
		return err
	}
	expires := time.Now().Add(attachmentURLTTL).UTC()
	for rows.Next() {
		var a Attachment
		var msgID uuid.UUID
		if err := rows.Scan(&a.ID, &msgID, &a.Kind, &a.key, &a.ContentType, &a.FileName, &a.SizeBytes, &a.Width, &a.Height, &a.DurationMS); err != nil {
			rows.Close()
			return err
		}
		if a.URL, err = s.signAttachment(ctx, a.key, a.ContentType, a.FileName, attachmentURLTTL); err != nil {
			slog.WarnContext(ctx, "chat: presign attachment", "attachment_id", a.ID, "err", err)
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
		FROM chat_reactions WHERE message_id = ANY($1)
		GROUP BY message_id, emoji ORDER BY message_id, min(created_at), emoji`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var msgID uuid.UUID
		var r Reaction
		if err := rows.Scan(&msgID, &r.Emoji, &r.UserIDs); err != nil {
			return err
		}
		r.Count = len(r.UserIDs)
		byID[msgID].Reactions = append(byID[msgID].Reactions, r)
	}
	return rows.Err()
}

// messagesByID loads and hydrates messages by id.
func (s *Service) messagesByID(ctx context.Context, q db.Querier, ids []uuid.UUID, staff bool) (map[uuid.UUID]*Message, error) {
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
	if err := s.hydrate(ctx, q, msgs, staff); err != nil {
		return nil, err
	}
	for _, m := range msgs {
		out[m.ID] = m
	}
	return out, nil
}

// message loads one hydrated message.
func (s *Service) message(ctx context.Context, q db.Querier, id uuid.UUID, staff bool) (*Message, error) {
	m, err := s.messagesByID(ctx, q, []uuid.UUID{id}, staff)
	if err != nil {
		return nil, err
	}
	if msg, ok := m[id]; ok {
		return msg, nil
	}
	return nil, errMessageNotFound
}

// ---- membership --------------------------------------------------------------

// member is the caller's active membership with the conversation's head.
type member struct {
	ConversationID uuid.UUID
	Kind           string
	Title          string
	AvatarKey      *string
	UserLow        *uuid.UUID
	UserHigh       *uuid.UUID
	Role           string
	ClearedAt      *time.Time
}

// peer is the other person in a direct conversation (uuid.Nil in groups).
func (m *member) peer(me uuid.UUID) uuid.UUID {
	if m.UserLow == nil || m.UserHigh == nil {
		return uuid.Nil
	}
	if *m.UserLow == me {
		return *m.UserHigh
	}
	return *m.UserLow
}

func (m *member) canManage() bool { return m.Role == roleOwner || m.Role == roleAdmin }

// loadMember returns the caller's active membership or errConversationNotFound.
// lock takes the conversation row FOR UPDATE (membership changes serialise on it).
func loadMember(ctx context.Context, q db.Querier, convID, userID uuid.UUID, lock bool) (*member, error) {
	sql := `
		SELECT c.id, c.kind, c.title, c.avatar_key, c.user_low, c.user_high, mb.role, mb.cleared_at
		FROM chat_members mb JOIN chat_conversations c ON c.id = mb.conversation_id
		WHERE mb.conversation_id = $1 AND mb.user_id = $2 AND mb.left_at IS NULL`
	if lock {
		sql += ` FOR UPDATE OF c`
	}
	var m member
	err := q.QueryRow(ctx, sql, convID, userID).Scan(&m.ConversationID, &m.Kind, &m.Title, &m.AvatarKey,
		&m.UserLow, &m.UserHigh, &m.Role, &m.ClearedAt)
	if db.IsNoRows(err) {
		return nil, errConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ---- small helpers -------------------------------------------------------------

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

// orderedPair returns a, b ordered the way Postgres orders uuids (bytewise).
func orderedPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	if bytes.Compare(a[:], b[:]) < 0 {
		return a, b
	}
	return b, a
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

// signAttachment presigns a download that opens what the browser can show
// and saves the rest under the attachment's name (not the storage key's).
func (s *Service) signAttachment(ctx context.Context, key, contentType, fileName string, ttl time.Duration) (string, error) {
	disp := "inline"
	if d := mime.FormatMediaType("inline", map[string]string{"filename": fileName}); fileName != "" && d != "" {
		disp = d
	}
	return s.d.Storage.PresignGetWith(ctx, key, ttl, storage.GetOptions{ContentType: contentType, Disposition: disp})
}
