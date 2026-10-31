// Package support is the support feature: users open tickets with staff
// (category, subject, message, attachments), follow the thread and close or
// reopen them; staff work a queue, reply (the user gets an email and a
// notification), keep internal notes, assign tickets and change status.
package support

import (
	"context"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/legal"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// Purpose is the storage purpose for ticket attachments (private).
const Purpose = "support_attachment"

// KindReply is the notification kind sent when staff reply.
const KindReply = "support.reply"

// App paths the web and mobile apps ship (links in email and notifications).
const (
	userTicketPath  = "/settings/support-requests/"
	staffTicketPath = "/admin/support/"
)

// Realtime events. support.message and support.ticket go to the ticket topic
// (support:<id>: its owner and staff); support.ticket also goes to admin:live
// so staff queues refresh. Internal notes are never published on the ticket
// topic.
const (
	EventMessage = "support.message"
	EventTicket  = "support.ticket"
)

const (
	maxAttachments    = 5
	maxAttachmentSize = 25 << 20
	maxSubjectRunes   = 120
	maxBodyRunes      = 5000
	maxStaffBodyRunes = 10000
	maxThread         = 500
	attachmentURLTTL  = 15 * time.Minute
)

var (
	categories = []string{"question", "bug", "account", "billing", "other"}
	statuses   = []string{"open", "answered", "closed"}
)

// attachmentTypes maps the key extensions the purpose allows to content types.
var attachmentTypes = map[string]string{
	"jpg":  "image/jpeg",
	"png":  "image/png",
	"heic": "image/heic",
	"webp": "image/webp",
	"gif":  "image/gif",
	"pdf":  "application/pdf",
	"mp4":  "video/mp4",
	"mov":  "video/quicktime",
	"txt":  "text/plain",
}

type Service struct {
	d *app.Deps
}

func New(d *app.Deps) *Service {
	types := make([]string, 0, len(attachmentTypes))
	for _, t := range attachmentTypes {
		types = append(types, t)
	}
	// Contacting support must work on the consent screen, attachments included.
	storage.RegisterPurpose(storage.Purpose{Name: Purpose, Public: false, Types: types, MaxBytes: maxAttachmentSize, ConsentExempt: true})
	legal.Exempt("/v1/support/tickets")
	return &Service{d: d}
}

func (s *Service) Name() string { return "support" }

func (s *Service) MountUser(r chi.Router) {
	// No RequireActive: suspended users are told to contact support.
	writes := auth.RateLimit(30, time.Hour) // replies, close and reopen share one budget
	r.Get("/support/tickets", httpx.Handler(s.listMine))
	r.With(auth.RateLimit(5, time.Hour)).Post("/support/tickets", httpx.Handler(s.createTicket))
	r.Get("/support/tickets/{id}", httpx.Handler(s.getMine))
	r.With(writes).Post("/support/tickets/{id}/messages", httpx.Handler(s.replyMine))
	r.With(writes).Post("/support/tickets/{id}/close", httpx.Handler(s.closeMine))
	r.With(writes).Post("/support/tickets/{id}/reopen", httpx.Handler(s.reopenMine))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/support/tickets", httpx.Handler(s.adminList))
	r.Get("/support/tickets/{id}", httpx.Handler(s.adminGet))
	r.Post("/support/tickets/{id}/messages", httpx.Handler(s.adminReply))
	r.Patch("/support/tickets/{id}", httpx.Handler(s.adminUpdate))
	r.Get("/support/stats", httpx.Handler(s.adminStats))
	r.Get("/support/assignees", httpx.Handler(s.adminAssignees))
}

// AuthorizeTopic admits the ticket's owner and staff to support:<ticket id>.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, isAdmin bool, topic string) (handled, allowed bool) {
	rest, ok := strings.CutPrefix(topic, "support:")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	if isAdmin {
		return true, true
	}
	var mine bool
	err = s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM support_tickets WHERE id = $1 AND user_id = $2)`,
		id, userID).Scan(&mine)
	return true, err == nil && mine
}

// ---- DTOs ------------------------------------------------------------------

// Ticket is a ticket as its owner sees it.
type Ticket struct {
	ID             uuid.UUID  `json:"id"`
	Number         int64      `json:"number"`
	Category       string     `json:"category"`
	Subject        string     `json:"subject"`
	Status         string     `json:"status"`
	ClientPlatform string     `json:"client_platform"`
	ClientVersion  string     `json:"client_version"`
	LastMessageAt  time.Time  `json:"last_message_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Attachment is a file on a message; url is a short-lived download link.
type Attachment struct {
	ID          uuid.UUID `json:"id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"created_at"`
}

// Message is one entry in a thread. author_id is set for staff views only;
// internal messages (staff notes) never reach the user.
type Message struct {
	ID          uuid.UUID    `json:"id"`
	TicketID    uuid.UUID    `json:"ticket_id"`
	AuthorID    *uuid.UUID   `json:"author_id"`
	AuthorRole  string       `json:"author_role"` // user | staff
	AuthorName  string       `json:"author_name"`
	Internal    bool         `json:"internal"`
	Body        string       `json:"body"`
	Attachments []Attachment `json:"attachments"`
	CreatedAt   time.Time    `json:"created_at"`
}

// TicketDetail is a ticket with its thread.
type TicketDetail struct {
	Ticket
	Messages []Message `json:"messages"`
}

const ticketCols = `t.id, t.number, t.category, t.subject, t.status, t.client_platform, t.client_version,
	t.last_message_at, t.closed_at, t.created_at, t.updated_at`

func scanTicket(row pgx.Row, t *Ticket, extra ...any) error {
	dst := []any{&t.ID, &t.Number, &t.Category, &t.Subject, &t.Status, &t.ClientPlatform, &t.ClientVersion,
		&t.LastMessageAt, &t.ClosedAt, &t.CreatedAt, &t.UpdatedAt}
	return row.Scan(append(dst, extra...)...)
}

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// ---- thread ----------------------------------------------------------------

// thread loads a ticket's messages (internal ones only for staff) with
// attachment download links.
func (s *Service) thread(ctx context.Context, ticketID uuid.UUID, staffView bool) ([]Message, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT m.id, m.ticket_id, m.author_id, m.author_role, u.display_name, m.internal, m.body, m.created_at
		FROM support_messages m JOIN users u ON u.id = m.author_id
		WHERE m.ticket_id = $1 AND ($2 OR NOT m.internal)
		ORDER BY m.created_at, m.id LIMIT $3`, ticketID, staffView, maxThread)
	if err != nil {
		return nil, err
	}
	msgs := []Message{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var m Message
		var author uuid.UUID
		if err := rows.Scan(&m.ID, &m.TicketID, &author, &m.AuthorRole, &m.AuthorName, &m.Internal, &m.Body, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if staffView {
			m.AuthorID = &author
		}
		m.AuthorName = authorName(m.AuthorRole, m.AuthorName)
		m.Attachments = []Attachment{}
		index[m.ID] = len(msgs)
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return msgs, nil
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT id, message_id, storage_key, file_name, content_type, size_bytes, created_at
		FROM support_attachments WHERE ticket_id = $1 ORDER BY created_at, id`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Attachment
		var messageID uuid.UUID
		var key string
		if err := rows.Scan(&a.ID, &messageID, &key, &a.FileName, &a.ContentType, &a.SizeBytes, &a.CreatedAt); err != nil {
			return nil, err
		}
		i, ok := index[messageID]
		if !ok {
			continue // on an internal message hidden from this view
		}
		if a.URL, err = s.d.Storage.PresignGet(ctx, key, attachmentURLTTL); err != nil {
			slog.WarnContext(ctx, "support: presign attachment", "attachment_id", a.ID, "err", err)
		}
		msgs[i].Attachments = append(msgs[i].Attachments, a)
	}
	return msgs, rows.Err()
}

func authorName(role, name string) string {
	name = strings.TrimSpace(name)
	switch {
	case name != "":
		return name
	case role == "staff":
		return "Support"
	}
	return "Deleted user"
}

// ---- writes shared by user and staff ---------------------------------------

type attachmentIn struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type attachmentRow struct {
	key, name, contentType string
	size                   int64
}

// checkAttachments validates attachment keys the user uploaded with purpose
// support_attachment.
func checkAttachments(in []attachmentIn, owner uuid.UUID) ([]attachmentRow, error) {
	if len(in) > maxAttachments {
		return nil, httpx.BadRequest("Attach up to 5 files.")
	}
	out := make([]attachmentRow, 0, len(in))
	seen := map[string]bool{}
	for _, a := range in {
		a.Key = strings.TrimSpace(a.Key)
		if !storage.KeyOwnedBy(a.Key, Purpose, owner) || seen[a.Key] {
			return nil, httpx.BadRequest("Upload each file first, then attach it.")
		}
		seen[a.Key] = true
		ext := strings.TrimPrefix(path.Ext(a.Key), ".")
		ct, ok := attachmentTypes[ext]
		if !ok {
			return nil, httpx.BadRequest("Attach photos, videos, PDFs or text files.")
		}
		if a.Size < 0 || a.Size > maxAttachmentSize {
			return nil, httpx.BadRequest("Each file must be 25 MB or smaller.")
		}
		name := cleanFileName(a.Name)
		if name == "" {
			name = "attachment." + ext
		}
		out = append(out, attachmentRow{key: a.Key, name: name, contentType: ct, size: a.Size})
	}
	return out, nil
}

// insertMessage adds a message and its attachments to a ticket.
func insertMessage(ctx context.Context, tx pgx.Tx, ticketID, authorID uuid.UUID, role string, internal bool,
	body string, atts []attachmentRow) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO support_messages (ticket_id, author_id, author_role, internal, body)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`, ticketID, authorID, role, internal, body).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	for _, a := range atts {
		_, err := tx.Exec(ctx, `
			INSERT INTO support_attachments (ticket_id, message_id, storage_key, file_name, content_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6)`, ticketID, id, a.key, a.name, a.contentType, a.size)
		if db.IsUniqueViolation(err) {
			return uuid.Nil, httpx.Conflict("That file is already attached. Upload it again to attach it here.")
		}
		if err != nil {
			return uuid.Nil, err
		}
	}
	return id, nil
}

// ---- realtime --------------------------------------------------------------

func (s *Service) publish(ctx context.Context, topic, event string, data any) {
	if err := s.d.Broker.Publish(ctx, topic, event, data); err != nil {
		slog.WarnContext(ctx, "support: realtime publish failed", "event", event, "err", err)
	}
}

// publishTicket tells the owner's open thread and staff queues that the
// ticket changed.
func (s *Service) publishTicket(ctx context.Context, t Ticket) {
	s.publish(ctx, realtime.Topic("support", t.ID), EventTicket, t)
	s.publish(ctx, realtime.AdminLiveTopic, EventTicket, t)
}

// publishMessage sends a user-visible message to the ticket topic.
func (s *Service) publishMessage(ctx context.Context, ticketID, messageID uuid.UUID) {
	msgs, err := s.thread(ctx, ticketID, false)
	if err != nil {
		slog.WarnContext(ctx, "support: load message for realtime", "err", err)
		return
	}
	for _, m := range msgs {
		if m.ID == messageID {
			s.publish(ctx, realtime.Topic("support", ticketID), EventMessage, m)
			return
		}
	}
}

func (s *Service) loadTicket(ctx context.Context, id uuid.UUID) (Ticket, error) {
	var t Ticket
	err := scanTicket(s.d.Pool.QueryRow(ctx, `SELECT `+ticketCols+` FROM support_tickets t WHERE t.id = $1`, id), &t)
	if db.IsNoRows(err) {
		return t, httpx.ErrNotFound
	}
	return t, err
}

// ---- validation helpers ----------------------------------------------------

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// line trims a single-line field and checks its length in runes.
func line(field, v string, minRunes, maxRunes int) (string, error) {
	v = strings.Join(strings.Fields(v), " ")
	if n := len([]rune(v)); n < minRunes || n > maxRunes {
		return "", httpx.BadRequest(field + " must be " + strconv.Itoa(minRunes) + " to " + strconv.Itoa(maxRunes) + " characters.")
	}
	return v, nil
}

// paragraph trims a multi-line field, keeps line breaks and checks length.
func paragraph(field, v string, minRunes, maxRunes int) (string, error) {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
	v = strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, v)
	if n := len([]rune(v)); n < minRunes || n > maxRunes {
		return "", httpx.BadRequest(field + " must be " + strconv.Itoa(minRunes) + " to " + strconv.Itoa(maxRunes) + " characters.")
	}
	return v, nil
}

var unsafeName = regexp.MustCompile(`[\x00-\x1f/\\:*?"<>|]+`)

func cleanFileName(name string) string {
	name = strings.TrimSpace(unsafeName.ReplaceAllString(path.Base(strings.ReplaceAll(name, "\\", "/")), "_"))
	if name == "." || name == "/" {
		return ""
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	return name
}

var versionRe = regexp.MustCompile(`^[0-9A-Za-z.+_() -]{1,40}$`)

// clientInfo reads X-Client-Platform and X-Client-Version (ignored when
// malformed).
func clientInfo(r *http.Request) (platform, version string) {
	platform = strings.ToLower(strings.TrimSpace(r.Header.Get("X-Client-Platform")))
	if !oneOf(platform, []string{"web", "ios", "android"}) {
		platform = ""
	}
	version = strings.TrimSpace(r.Header.Get("X-Client-Version"))
	if !versionRe.MatchString(version) {
		version = ""
	}
	return platform, version
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
