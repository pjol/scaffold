package crm

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// LinkCard previews a CRM link (/crm/<type>/<id>, /crm/quotes/<id>) as one
// viewer may see it, for other features' link cards (a chat unfurler). Nil
// when the viewer can't open it or it doesn't exist, so nothing leaks.
type LinkCard struct {
	Kind    string     // crm.company, crm.contact, crm.deal, crm.ticket, crm.quote
	Title   string     // the record's name as the viewer sees it
	Context string     // "<org> · CRM · Deal"
	Snippet string     // stage and amount, email, status and priority…
	Icon    string     // lucide icon name
	Link    string     // app path
	At      *time.Time // last activity or update
}

// unfurlService backs the package-level Unfurl (set by New).
var unfurlService atomic.Pointer[Service]

// Unfurl is (*Service).Unfurl for stitching that holds no Service (the
// app's api builds it): nil, nil before New.
func Unfurl(ctx context.Context, viewer *auth.Identity, path string) (*LinkCard, error) {
	s := unfurlService.Load()
	if s == nil {
		return nil, nil
	}
	return s.Unfurl(ctx, viewer, path)
}

// Unfurl returns the card for path (an app path) as viewer sees it, with the
// same checks as the record routes (agent-capped when ctx carries an agent).
// Stitching registers it with a messaging feature's link-card seam.
func (s *Service) Unfurl(ctx context.Context, viewer *auth.Identity, path string) (*LinkCard, error) {
	rest, ok := strings.CutPrefix(strings.TrimSuffix(path, "/"), "/crm/")
	if !ok || viewer == nil {
		return nil, nil
	}
	seg, rawID, ok := strings.Cut(rest, "/")
	if !ok || strings.Contains(rawID, "/") {
		return nil, nil
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, nil
	}
	if seg == "quotes" {
		qt, _, err := s.requireQuote(ctx, s.d.Pool, viewer.UserID, id, access.View)
		if err != nil {
			return nil, nil
		}
		_, totals, err := quoteItems(ctx, s.d.Pool, id)
		if err != nil {
			return nil, err
		}
		status := qt.Status
		if qt.Expired {
			status = "expired"
		}
		at := qt.UpdatedAt
		return &LinkCard{Kind: KindQuote, Title: "Quote " + qt.Number + " · " + qt.Title, Context: s.cardContext(ctx, qt.OrgID, "Quote"),
			Snippet: fmt.Sprintf("%s · %s · %s", strings.ToUpper(status[:1])+status[1:], formatMoney(totals.Total, qt.Currency), qt.Deal.Name),
			Icon:    "file-text", Link: "/crm/quotes/" + id.String(), At: &at}, nil
	}
	o, ok := parseObject(seg)
	if !ok || o.path() != seg {
		return nil, nil
	}
	if _, _, err := s.requireRecord(ctx, s.d.Pool, viewer.UserID, o, id, access.View); err != nil {
		return nil, nil
	}
	rec, err := s.getOne(ctx, s.d.Pool, viewer.UserID, o, id)
	if err != nil {
		return nil, nil
	}
	b := rec.base()
	card := &LinkCard{Kind: o.kind(), Title: b.Title, Context: s.cardContext(ctx, b.OrgID, o.titleCase()), Icon: o.icon(), Link: o.link(id), At: &b.UpdatedAt}
	if b.LastActivityAt != nil {
		card.At = b.LastActivityAt
	}
	switch r := rec.(type) {
	case *Company:
		card.Snippet = strings.Join(nonEmpty(deref(r.Domain), r.Industry, r.City), " · ")
	case *Contact:
		parts := nonEmpty(r.JobTitle)
		if r.Company != nil {
			parts = append(parts, r.Company.Name)
		}
		if len(r.Emails) > 0 {
			parts = append(parts, r.Emails[0])
		}
		card.Snippet = strings.Join(parts, " · ")
	case *Deal:
		parts := []string{r.Stage.Name}
		if r.Amount != nil {
			parts = append(parts, formatMoney(*r.Amount, r.Currency))
		}
		if r.CloseDate != nil {
			parts = append(parts, "closes "+*r.CloseDate)
		}
		card.Snippet = strings.Join(parts, " · ")
	case *Ticket:
		card.Snippet = strings.Join(nonEmpty(r.Stage.Name, optionLabel(priorityOptions, r.Priority)+" priority", r.Category), " · ")
	}
	return card, nil
}

func (s *Service) cardContext(ctx context.Context, orgID uuid.UUID, what string) string {
	var name string
	if err := s.d.Pool.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, orgID).Scan(&name); err != nil && !db.IsNoRows(err) {
		name = ""
	}
	parts := nonEmpty(name, "CRM", what)
	return strings.Join(parts, " · ")
}
