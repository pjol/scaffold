package support

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
)

// listMine is the caller's tickets, newest first; ?status= filters.
func (s *Service) listMine(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	status := r.URL.Query().Get("status")
	if status != "" && !oneOf(status, statuses) {
		return httpx.BadRequest("Status must be open, answered or closed.")
	}
	items, err := s.ticketsOf(ctx, id.UserID, status, c, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(items, p.Limit, func(t Ticket) any { return cursor{t.CreatedAt, t.ID} }))
	return nil
}

func (s *Service) ticketsOf(ctx context.Context, userID uuid.UUID, status string, c cursor, limit int) ([]Ticket, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+ticketCols+` FROM support_tickets t
		WHERE t.user_id = $1 AND ($2 = '' OR t.status = $2)
		  AND ($3::timestamptz IS NULL OR (t.created_at, t.id) < ($3, $4))
		ORDER BY t.created_at DESC, t.id DESC LIMIT $5`, userID, status, nullTime(c.T), c.ID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Ticket, error) {
		var t Ticket
		return t, scanTicket(row, &t)
	})
}

type createTicketReq struct {
	Category    string         `json:"category"`
	Subject     string         `json:"subject"`
	Body        string         `json:"body"`
	Attachments []attachmentIn `json:"attachments"`
}

// createTicket opens a ticket. Consent-exempt and limited to 5 an hour; the
// support inbox (SUPPORT_EMAIL) gets an alert.
func (s *Service) createTicket(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	var req createTicketReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !oneOf(req.Category, categories) {
		return httpx.BadRequest("Choose a category: question, bug, account, billing or other.")
	}
	subject, err := line("Subject", req.Subject, 3, maxSubjectRunes)
	if err != nil {
		return err
	}
	body, err := paragraph("Message", req.Body, 1, maxBodyRunes)
	if err != nil {
		return err
	}
	atts, err := checkAttachments(req.Attachments, id.UserID)
	if err != nil {
		return err
	}
	platform, version := clientInfo(r)

	var ticketID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO support_tickets (user_id, category, subject, client_platform, client_version)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			id.UserID, req.Category, subject, platform, version).Scan(&ticketID); err != nil {
			return err
		}
		_, err := insertMessage(ctx, tx, ticketID, id.UserID, "user", false, body, atts)
		return err
	})
	if err != nil {
		return err
	}
	detail, err := s.detailFor(ctx, ticketID, id.UserID)
	if err != nil {
		return err
	}
	s.publish(context.WithoutCancel(ctx), realtime.AdminLiveTopic, EventTicket, detail.Ticket)
	bg.Go(ctx, "support.alert_new_ticket", func(ctx context.Context) error {
		s.alertNewTicket(ctx, detail.Ticket, id.Email, body, len(atts))
		return nil
	})
	httpx.JSON(w, http.StatusCreated, detail)
	return nil
}

// detailFor loads a ticket the user owns, with the user-visible thread.
func (s *Service) detailFor(ctx context.Context, ticketID, userID uuid.UUID) (*TicketDetail, error) {
	var d TicketDetail
	err := scanTicket(s.d.Pool.QueryRow(ctx, `SELECT `+ticketCols+` FROM support_tickets t
		WHERE t.id = $1 AND t.user_id = $2`, ticketID, userID), &d.Ticket)
	if db.IsNoRows(err) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if d.Messages, err = s.thread(ctx, ticketID, false); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) getMine(w http.ResponseWriter, r *http.Request) error {
	ticketID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	d, err := s.detailFor(r.Context(), ticketID, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

type replyReq struct {
	Body        string         `json:"body"`
	Attachments []attachmentIn `json:"attachments"`
}

// replyMine adds the user's message; an answered or closed ticket reopens.
func (s *Service) replyMine(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	ticketID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req replyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, err := paragraph("Message", req.Body, 1, maxBodyRunes)
	if err != nil {
		return err
	}
	atts, err := checkAttachments(req.Attachments, id.UserID)
	if err != nil {
		return err
	}
	var messageID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE support_tickets SET status = 'open', closed_at = NULL, last_message_at = now(), updated_at = now()
			WHERE id = $1 AND user_id = $2`, ticketID, id.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		messageID, err = insertMessage(ctx, tx, ticketID, id.UserID, "user", false, body, atts)
		return err
	})
	if err != nil {
		return err
	}
	d, err := s.detailFor(ctx, ticketID, id.UserID)
	if err != nil {
		return err
	}
	detached := context.WithoutCancel(ctx)
	s.publishMessage(detached, ticketID, messageID)
	s.publishTicket(detached, d.Ticket)
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

func (s *Service) closeMine(w http.ResponseWriter, r *http.Request) error {
	return s.setMineStatus(w, r, `
		UPDATE support_tickets SET status = 'closed', closed_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status <> 'closed'`)
}

func (s *Service) reopenMine(w http.ResponseWriter, r *http.Request) error {
	return s.setMineStatus(w, r, `
		UPDATE support_tickets SET status = 'open', closed_at = NULL, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'closed'`)
}

// setMineStatus runs a status change on the caller's ticket (idempotent: a
// ticket already in the target state is returned unchanged).
func (s *Service) setMineStatus(w http.ResponseWriter, r *http.Request, sql string) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	ticketID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, sql, ticketID, id.UserID)
	if err != nil {
		return err
	}
	d, err := s.detailFor(ctx, ticketID, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		s.publishTicket(context.WithoutCancel(ctx), d.Ticket)
	}
	httpx.OK(w, d)
	return nil
}
