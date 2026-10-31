package support

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// ---- account deletion and export -------------------------------------------

// DeleteAccount deletes the user's tickets (messages, notes and attachment
// rows cascade; the files go with the user's storage prefix, which core
// removes after commit) and unassigns tickets the user worked as staff. A
// deleted staff member's replies stay in other users' threads under the
// anonymised account.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE support_tickets SET assignee_id = NULL, updated_at = now() WHERE assignee_id = $1`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM support_tickets WHERE user_id = $1`, userID)
	return err
}

type exportAttachment struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type exportMessage struct {
	AuthorRole  string             `json:"author_role"`
	Body        string             `json:"body"`
	Attachments []exportAttachment `json:"attachments"`
	CreatedAt   time.Time          `json:"created_at"`
}

type exportTicket struct {
	Ticket
	Messages []exportMessage `json:"messages"`
}

// ExportAccount returns the user's tickets with their thread (staff notes
// excluded) and attachment metadata.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	tickets, err := s.ticketsOf(ctx, userID, "", cursor{}, 10000)
	if err != nil {
		return nil, err
	}
	out := make([]exportTicket, 0, len(tickets))
	for _, t := range tickets {
		msgs, err := s.threadMeta(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, exportTicket{Ticket: t, Messages: msgs})
	}
	return map[string]any{"tickets": out}, nil
}

// threadMeta is the user-visible thread without download links.
func (s *Service) threadMeta(ctx context.Context, ticketID uuid.UUID) ([]exportMessage, error) {
	msgs, err := s.thread(ctx, ticketID, false)
	if err != nil {
		return nil, err
	}
	out := make([]exportMessage, 0, len(msgs))
	for _, m := range msgs {
		em := exportMessage{AuthorRole: m.AuthorRole, Body: m.Body, CreatedAt: m.CreatedAt, Attachments: []exportAttachment{}}
		for _, a := range m.Attachments {
			em.Attachments = append(em.Attachments, exportAttachment{FileName: a.FileName, ContentType: a.ContentType, SizeBytes: a.SizeBytes})
		}
		out = append(out, em)
	}
	return out, nil
}

// ---- development seed ------------------------------------------------------

var seedNS = uuid.MustParse("0c1e6a52-3f0b-4d61-8a2e-7d9b5c4f0400")

// SeedDev gives Sam (users[1]) an open bug report with an internal note and
// an answered question, both handled by the dev admin (users[0]).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 2 {
		return nil
	}
	admin, sam := users[0], users[1]
	now := time.Now().UTC()
	type msg struct {
		key      string
		author   uuid.UUID
		role     string
		internal bool
		body     string
		age      time.Duration
	}
	tickets := []struct {
		key, category, subject, status string
		assignee                       *uuid.UUID
		platform, version              string
		age                            time.Duration
		msgs                           []msg
	}{
		{
			key: "bug", category: "bug", subject: "Profile photo doesn't update", status: "open",
			assignee: &admin, platform: "ios", version: "1.0.0", age: 5 * time.Hour,
			msgs: []msg{
				{"bug-1", sam, "user", false, "I picked a new profile photo and saved, but the old one still shows on my profile. I tried twice.", 5 * time.Hour},
				{"bug-2", admin, "staff", true, "Reproduced on the iOS simulator. The upload succeeds; the profile screen keeps the cached image.", 3 * time.Hour},
			},
		},
		{
			key: "question", category: "question", subject: "How do I download my data?", status: "answered",
			assignee: &admin, platform: "web", version: "", age: 50 * time.Hour,
			msgs: []msg{
				{"question-1", sam, "user", false, "I'd like a copy of everything stored about my account. Where do I get it?", 50 * time.Hour},
				{"question-2", admin, "staff", false, "Go to Settings → Account → Download my data. The file includes your profile and everything you've added.", 48 * time.Hour},
			},
		},
	}
	for _, t := range tickets {
		ticketID := uuid.NewSHA1(seedNS, []byte(t.key))
		created := now.Add(-t.age)
		last := now.Add(-t.msgs[len(t.msgs)-1].age)
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO support_tickets (id, user_id, category, subject, status, assignee_id, client_platform, client_version,
			  last_message_at, created_at, updated_at)
			SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $9
			WHERE NOT EXISTS (SELECT 1 FROM support_tickets WHERE id = $1)`, // no row, no ticket number used
			ticketID, sam, t.category, t.subject, t.status, t.assignee, t.platform, t.version, last, created); err != nil {
			return err
		}
		for _, m := range t.msgs {
			if _, err := s.d.Pool.Exec(ctx, `
				INSERT INTO support_messages (id, ticket_id, author_id, author_role, internal, body, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`,
				uuid.NewSHA1(seedNS, []byte(m.key)), ticketID, m.author, m.role, m.internal, m.body, now.Add(-m.age)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- agent tools -----------------------------------------------------------

// Tools are read-only: opening a ticket emails the support inbox, so agents
// don't get that.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "support_list_tickets",
			Description: "List the user's support requests, newest first, with status (open, answered, closed).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","enum":["open","answered","closed"]},"limit":{"type":"integer","minimum":1,"maximum":50,"default":20}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Status string `json:"status"`
					Limit  int    `json:"limit"`
				}
				if len(input) > 0 {
					if err := json.Unmarshal(input, &in); err != nil {
						return nil, err
					}
				}
				if in.Status != "" && !oneOf(in.Status, statuses) {
					return nil, httpx.BadRequest("Status must be open, answered or closed.")
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				items, err := s.ticketsOf(ctx, caller.UserID, in.Status, cursor{}, in.Limit)
				if err != nil {
					return nil, err
				}
				return map[string]any{"items": items}, nil
			},
		},
		{
			Name:        "support_get_ticket",
			Description: "Show one of the user's support requests with its conversation with staff.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"ticket_id":{"type":"string","format":"uuid"}},"required":["ticket_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					TicketID uuid.UUID `json:"ticket_id"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return nil, httpx.BadRequest("ticket_id must be a ticket id.")
				}
				d, err := s.detailFor(ctx, in.TicketID, caller.UserID)
				if err != nil {
					return nil, err
				}
				msgs, err := s.threadMeta(ctx, d.ID)
				if err != nil {
					return nil, err
				}
				return exportTicket{Ticket: d.Ticket, Messages: msgs}, nil
			},
		},
	}
}
