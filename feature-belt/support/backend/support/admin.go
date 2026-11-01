package support

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// UserRef is the ticket's owner as staff see it.
type UserRef struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
}

// StaffRef is a staff member (assignee, assignee picker).
type StaffRef struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	AdminRole   string    `json:"admin_role,omitempty"`
}

// AdminTicket is a queue row.
type AdminTicket struct {
	Ticket
	User         UserRef   `json:"user"`
	Assignee     *StaffRef `json:"assignee"`
	MessageCount int       `json:"message_count"`
}

// AdminTicketDetail is a ticket with the full thread, internal notes included.
type AdminTicketDetail struct {
	AdminTicket
	Messages []Message `json:"messages"`
}

const adminFrom = ` FROM support_tickets t
	JOIN users u ON u.id = t.user_id
	LEFT JOIN users a ON a.id = t.assignee_id`

const adminCols = ticketCols + `, u.id, u.email, u.display_name, u.status, a.id, a.email, a.display_name,
	(SELECT count(*) FROM support_messages m WHERE m.ticket_id = t.id)`

func scanAdminTicket(row pgx.Row) (AdminTicket, error) {
	var t AdminTicket
	var aID *uuid.UUID
	var aEmail, aName *string
	err := scanTicket(row, &t.Ticket, &t.User.ID, &t.User.Email, &t.User.DisplayName, &t.User.Status,
		&aID, &aEmail, &aName, &t.MessageCount)
	if err == nil && aID != nil {
		t.Assignee = &StaffRef{ID: *aID, Email: deref(aEmail), DisplayName: deref(aName)}
	}
	return t, err
}

// adminList is the queue. Filters: status, category, assignee (me | none |
// <uuid>), user_id, q (subject, email or #number). sort=oldest works the
// queue first-in first-out; the default is newest first.
func (s *Service) adminList(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := r.URL.Query()
	status, category := q.Get("status"), q.Get("category")
	if status != "" && !oneOf(status, statuses) {
		return httpx.BadRequest("Status must be open, answered or closed.")
	}
	if category != "" && !oneOf(category, categories) {
		return httpx.BadRequest("Unknown category.")
	}
	assignee := q.Get("assignee")
	switch assignee {
	case "", "none":
	case "me":
		assignee = staff.UserID.String()
	default:
		if _, err := uuid.Parse(assignee); err != nil {
			return httpx.BadRequest("Assignee must be me, none or a user id.")
		}
	}
	var userID *uuid.UUID
	if v := q.Get("user_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		userID = &id
	}
	search := strings.TrimSpace(q.Get("q"))
	order, cmp := "DESC", "<"
	if q.Get("sort") == "oldest" {
		order, cmp = "ASC", ">"
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+adminCols+adminFrom+`
		WHERE ($1 = '' OR t.status = $1) AND ($2 = '' OR t.category = $2)
		  AND (CASE $3 WHEN '' THEN true WHEN 'none' THEN t.assignee_id IS NULL ELSE t.assignee_id::text = $3 END)
		  AND ($4::uuid IS NULL OR t.user_id = $4)
		  AND ($5 = '' OR t.subject ILIKE '%' || $5 || '%' OR u.email ILIKE '%' || $5 || '%'
		       OR t.number::text = ltrim($5, '#'))
		  AND ($6::timestamptz IS NULL OR (t.created_at, t.id) `+cmp+` ($6, $7))
		ORDER BY t.created_at `+order+`, t.id `+order+` LIMIT $8`,
		status, category, assignee, userID, search, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminTicket, error) { return scanAdminTicket(row) })
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, p.Limit, func(t AdminTicket) any { return cursor{t.CreatedAt, t.ID} }))
	return nil
}

func (s *Service) adminDetail(ctx context.Context, id uuid.UUID) (*AdminTicketDetail, error) {
	t, err := scanAdminTicket(s.d.Pool.QueryRow(ctx, `SELECT `+adminCols+adminFrom+` WHERE t.id = $1`, id))
	if db.IsNoRows(err) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := &AdminTicketDetail{AdminTicket: t}
	if d.Messages, err = s.thread(ctx, id, true); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) adminGet(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	d, err := s.adminDetail(r.Context(), id)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

type adminReplyReq struct {
	Body     string `json:"body"`
	Internal bool   `json:"internal"` // a staff note the user never sees
	Close    bool   `json:"close"`    // close the ticket after replying
}

// adminReply posts a staff reply or internal note. A reply marks the ticket
// answered (or closed with close=true), assigns it to the replier when
// unassigned, and emails and notifies the user after commit.
func (s *Service) adminReply(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	ticketID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req adminReplyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, err := paragraph("Reply", req.Body, 1, maxStaffBodyRunes)
	if err != nil {
		return err
	}
	if req.Internal && req.Close {
		return httpx.BadRequest("Close the ticket with a reply or from the status menu, not with a note.")
	}
	var messageID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM support_tickets WHERE id = $1 FOR UPDATE`, ticketID).Scan(&status); err != nil {
			if db.IsNoRows(err) {
				return httpx.ErrNotFound
			}
			return err
		}
		var err error
		if messageID, err = insertMessage(ctx, tx, ticketID, staff.UserID, "staff", req.Internal, body, nil); err != nil {
			return err
		}
		action := "support.ticket.note"
		if !req.Internal {
			action = "support.ticket.reply"
			next := "answered"
			if req.Close {
				next = "closed"
			}
			if _, err := tx.Exec(ctx, `
				UPDATE support_tickets SET status = $2, last_message_at = now(), updated_at = now(),
				  closed_at = CASE WHEN $2 = 'closed' THEN now() END,
				  assignee_id = COALESCE(assignee_id, $3)
				WHERE id = $1`, ticketID, next, staff.UserID); err != nil {
				return err
			}
			status = next
		}
		return audit.Log(ctx, tx, staff.UserID, action, "support_ticket", ticketID.String(),
			map[string]any{"message_id": messageID, "status": status})
	})
	if err != nil {
		return err
	}
	d, err := s.adminDetail(ctx, ticketID)
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	if req.Internal {
		s.publish(bg, realtime.AdminLiveTopic, EventTicket, d.Ticket)
	} else {
		s.publishMessage(bg, ticketID, messageID)
		s.publishTicket(bg, d.Ticket)
		go s.deliverReply(bg, d.AdminTicket, staff.DisplayName, body)
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

// deliverReply emails the reply to the user and sends a support.reply
// notification. Best-effort.
func (s *Service) deliverReply(ctx context.Context, t AdminTicket, staffName, body string) {
	if t.User.Status == "deleted" {
		return
	}
	s.emailReply(ctx, t, staffName, body)
	err := s.d.Notify.Send(ctx, notify.Notification{
		UserID: t.User.ID,
		Kind:   KindReply,
		Title:  "Support replied: " + t.Subject,
		Body:   snippet(body, 140),
		Link:   userTicketPath + t.ID.String(),
		Data:   map[string]any{"ticket_id": t.ID.String()},
	})
	if err != nil {
		slog.WarnContext(ctx, "support: notify reply", "ticket_id", t.ID, "err", err)
	}
}

type adminUpdateReq struct {
	Status     *string `json:"status"`      // open | answered | closed
	AssigneeID *string `json:"assignee_id"` // staff user id, or "" to unassign
}

// adminUpdate changes status and/or assignee. Audited with before and after.
func (s *Service) adminUpdate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	ticketID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req adminUpdateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Status == nil && req.AssigneeID == nil {
		return httpx.BadRequest("Send a status or an assignee.")
	}
	if req.Status != nil && !oneOf(*req.Status, statuses) {
		return httpx.BadRequest("Status must be open, answered or closed.")
	}
	var assignee *uuid.UUID
	if req.AssigneeID != nil && *req.AssigneeID != "" {
		id, err := uuid.Parse(*req.AssigneeID)
		if err != nil {
			return httpx.BadRequest("Invalid assignee_id.")
		}
		var ok bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND admin_role IS NOT NULL AND status = 'active')`,
			id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpx.BadRequest("Assign tickets to an active staff member.")
		}
		assignee = &id
	}
	changed := false
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var before struct {
			Status   string     `json:"status"`
			Assignee *uuid.UUID `json:"assignee_id"`
		}
		if err := tx.QueryRow(ctx, `SELECT status, assignee_id FROM support_tickets WHERE id = $1 FOR UPDATE`, ticketID).
			Scan(&before.Status, &before.Assignee); err != nil {
			if db.IsNoRows(err) {
				return httpx.ErrNotFound
			}
			return err
		}
		after := before
		if req.Status != nil {
			after.Status = *req.Status
		}
		if req.AssigneeID != nil {
			after.Assignee = assignee
		}
		if after.Status == before.Status && sameID(after.Assignee, before.Assignee) {
			return nil
		}
		changed = true
		if _, err := tx.Exec(ctx, `
			UPDATE support_tickets SET status = $2, assignee_id = $3, updated_at = now(),
			  closed_at = CASE WHEN $2 = 'closed' THEN COALESCE(closed_at, now()) END
			WHERE id = $1`, ticketID, after.Status, after.Assignee); err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "support.ticket.update", "support_ticket", ticketID.String(),
			map[string]any{"before": before, "after": after})
	})
	if err != nil {
		return err
	}
	d, err := s.adminDetail(ctx, ticketID)
	if err != nil {
		return err
	}
	if changed {
		s.publishTicket(context.WithoutCancel(ctx), d.Ticket)
	}
	httpx.OK(w, d)
	return nil
}

// Stats is GET /v1/admin/support/stats (queue badges).
type Stats struct {
	Open           int        `json:"open"`
	Answered       int        `json:"answered"`
	Closed         int        `json:"closed"`
	UnassignedOpen int        `json:"unassigned_open"`
	MineOpen       int        `json:"mine_open"`
	OldestOpenAt   *time.Time `json:"oldest_open_at"` // longest-waiting open ticket's last message
}

func (s *Service) adminStats(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	var out Stats
	err := s.d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FILTER (WHERE status = 'open'), count(*) FILTER (WHERE status = 'answered'),
		       count(*) FILTER (WHERE status = 'closed'),
		       count(*) FILTER (WHERE status = 'open' AND assignee_id IS NULL),
		       count(*) FILTER (WHERE status = 'open' AND assignee_id = $1),
		       min(last_message_at) FILTER (WHERE status = 'open')
		FROM support_tickets`, staff.UserID).
		Scan(&out.Open, &out.Answered, &out.Closed, &out.UnassignedOpen, &out.MineOpen, &out.OldestOpenAt)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// adminAssignees lists active staff for the assignee picker.
func (s *Service) adminAssignees(w http.ResponseWriter, r *http.Request) error {
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT id, email, display_name, admin_role FROM users
		WHERE admin_role IS NOT NULL AND status = 'active'
		ORDER BY lower(display_name), email LIMIT 200`)
	if err != nil {
		return err
	}
	staff, err := pgx.CollectRows(rows, pgx.RowToStructByPos[StaffRef])
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": staff})
	return nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func snippet(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
