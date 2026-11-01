package support

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"scaffoldapp/backend/internal/mail"
)

// alertNewTicket emails SUPPORT_EMAIL about a new ticket. Best-effort.
func (s *Service) alertNewTicket(ctx context.Context, t Ticket, userEmail, body string, attachments int) {
	to := strings.TrimSpace(s.d.Cfg.SupportEmail)
	if to == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client := strings.TrimSpace(t.ClientPlatform + " " + t.ClientVersion)
	if client == "" {
		client = "unknown"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "New support request #%d.\n\n", t.Number)
	fmt.Fprintf(&b, "Category: %s\nFrom: %s\nApp: %s\nAttachments: %d\n", t.Category, userEmail, client, attachments)
	if link := s.webLink(staffTicketPath + t.ID.String()); link != "" {
		fmt.Fprintf(&b, "Open: %s\n", link)
	}
	fmt.Fprintf(&b, "\nSubject: %s\n\n%s\n", t.Subject, body)
	err := s.d.Mail.Send(ctx, mail.Message{
		To:      to,
		Subject: truncateRunes(fmt.Sprintf("Support request #%d: %s", t.Number, t.Subject), 160),
		Text:    b.String(),
	})
	if err != nil {
		slog.WarnContext(ctx, "support: new ticket alert failed", "ticket_id", t.ID, "err", err)
	}
}

// emailReply sends a staff reply to the ticket's owner. Best-effort.
func (s *Service) emailReply(ctx context.Context, t AdminTicket, staffName, body string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	from := strings.TrimSpace(staffName)
	if from == "" {
		from = "Scaffold App support"
	} else {
		from += " from Scaffold App support"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s replied to your request #%d (%s):\n\n%s\n\n", from, t.Number, t.Subject, body)
	if link := s.webLink(userTicketPath + t.ID.String()); link != "" {
		fmt.Fprintf(&b, "Reply or view the request: %s\n", link)
	} else {
		b.WriteString("Reply in Scaffold App: Settings → Support requests.\n")
	}
	err := s.d.Mail.Send(ctx, mail.Message{
		To:      t.User.Email,
		Subject: truncateRunes(fmt.Sprintf("Re: %s [#%d]", t.Subject, t.Number), 160),
		Text:    b.String(),
		ReplyTo: s.d.Cfg.SupportEmail,
	})
	if err != nil {
		slog.WarnContext(ctx, "support: reply email failed", "ticket_id", t.ID, "err", err)
	}
}

func (s *Service) webLink(path string) string {
	base := strings.TrimRight(s.d.Cfg.PublicWebURL, "/")
	if base == "" {
		return ""
	}
	return base + path
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
