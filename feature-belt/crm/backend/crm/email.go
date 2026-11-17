package crm

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Email from the CRM through each person's own connected mailbox: send to a
// contact (templates with merge fields), and the sent email lands on the
// timeline; recent mail with a contact is read live from the mailbox and only
// what the person chooses to log is stored. No open or click tracking.

const (
	mailSendsPerHour = 200
	mailLogsPerHour  = 600
	recentMailLimit  = 20
	maxRecipients    = 10
)

// MailAccount is one of the caller's connected mailboxes the CRM can use.
type MailAccount struct {
	ConnectionID string `json:"connection_id"`
	Provider     string `json:"provider"`
	Account      string `json:"account"`
	Status       string `json:"status"`
	CanSend      bool   `json:"can_send"`
	CanRead      bool   `json:"can_read"`
}

// MailAccounts is GET /v1/crm/mail/accounts.
type MailAccounts struct {
	Accounts []MailAccount         `json:"accounts"`
	Offers   []app.ConnectionOffer `json:"offers"`
	// Whether the product wired a calendar for "Log meeting" and "Insert booking link".
	Meetings     bool `json:"meetings"`
	BookingLinks bool `json:"booking_links"`
}

func (s *Service) mailAccounts(ctx context.Context, userID, orgID uuid.UUID) (*MailAccounts, error) {
	out := &MailAccounts{Accounts: []MailAccount{}, Offers: []app.ConnectionOffer{}, Meetings: currentMeetingSource() != nil, BookingLinks: currentBookingLinks() != nil}
	cs := app.FindConnections(s.d.Modules)
	if cs == nil {
		return out, nil
	}
	byID := map[string]*MailAccount{}
	var order []string
	for _, c := range mailCaps {
		list, err := cs.For(ctx, userID, orgID, c.Key)
		if err != nil {
			return nil, err
		}
		for _, conn := range list {
			a := byID[conn.ID]
			if a == nil {
				a = &MailAccount{ConnectionID: conn.ID, Provider: c.Provider, Account: conn.Account, Status: conn.Status}
				byID[conn.ID] = a
				order = append(order, conn.ID)
			}
			active := conn.Status == "" || conn.Status == "active"
			if c.Write {
				a.CanSend = a.CanSend || active
			} else {
				a.CanRead = a.CanRead || active
			}
		}
	}
	for _, id := range order {
		out.Accounts = append(out.Accounts, *byID[id])
	}
	offers, err := cs.Offers(ctx, userID, orgID, "crm")
	if err == nil {
		for _, o := range offers {
			if strings.Contains(o.Capability, ".mail_") {
				out.Offers = append(out.Offers, o)
			}
		}
	}
	return out, nil
}

// getMailAccounts: GET /v1/crm/mail/accounts?org_id= (member + crm.access).
func (s *Service) getMailAccounts(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID, err := uuid.Parse(r.URL.Query().Get("org_id"))
	if err != nil {
		return httpx.BadRequest("org_id is required.")
	}
	if err := s.requireOrgCRM(ctx, orgID, me(r)); err != nil {
		return err
	}
	out, err := s.mailAccounts(ctx, me(r), orgID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// openMailbox opens one of userID's connections for reading or sending in orgID.
func (s *Service) openMailbox(ctx context.Context, userID, orgID uuid.UUID, connID string, send bool) (mailbox, app.Connection, error) {
	cs := app.FindConnections(s.d.Modules)
	if cs == nil {
		return nil, app.Connection{}, httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "Connect a Gmail or Outlook account in Settings → Connections first.")
	}
	ctx = app.WithConnectionOrg(ctx, orgID)
	var lastErr error
	for _, provider := range []string{mailProviderGoogle, mailProviderMicrosoft} {
		hc, conn, err := cs.Client(ctx, userID, connID, mailCap(provider, send))
		if err != nil {
			lastErr = err
			continue
		}
		if conn.Provider != "" && conn.Provider != provider {
			continue
		}
		account := strings.ToLower(conn.Account)
		if provider == mailProviderGoogle {
			return &gmailBox{hc: hc, base: gmailBase(conn), account: account}, conn, nil
		}
		base := strings.TrimRight(conn.Endpoints["graph"], "/")
		if base == "" {
			base = "https://graph.microsoft.com/v1.0"
		}
		return &graphBox{hc: hc, base: base, account: account}, conn, nil
	}
	// Prefer the answer that names the fix (grant the capability, reconnect).
	if lastErr == nil {
		lastErr = errNoConnection()
	}
	return nil, app.Connection{}, lastErr
}

// contactEmails is a contact's addresses (the caller can view it).
func contactEmails(c *Contact) []string {
	out := []string{}
	for _, e := range c.Emails {
		if e != "" {
			out = append(out, strings.ToLower(e))
		}
	}
	return out
}

type sendMailReq struct {
	ConnectionID string     `json:"connection_id"`
	To           []string   `json:"to"`
	CC           []string   `json:"cc"`
	Subject      string     `json:"subject"`
	Body         string     `json:"body"`
	DealID       *uuid.UUID `json:"deal_id"`
	TicketID     *uuid.UUID `json:"ticket_id"`
	DraftID      *uuid.UUID `json:"draft_id"` // sent from a draft: the draft goes
}

// sendMail: POST /v1/crm/contacts/{id}/mail (comment on the contact): send
// from the caller's mailbox and log it on the contact (and the deal or
// ticket named, and the contact's primary company).
func (s *Service) sendMail(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	contactID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req sendMailReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, ok := auth.AgentFrom(ctx); ok {
		return httpx.Forbidden("AI agents write drafts; the person sends them.")
	}
	userID := me(r)
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, userID, objContact, contactID, access.Comment)
	if err != nil {
		return err
	}
	rec, err := s.getOne(ctx, s.d.Pool, userID, objContact, contactID)
	if err != nil {
		return err
	}
	contact := rec.(*Contact)
	to, err := validAddresses(req.To, "To")
	if err != nil {
		return httpx.BadRequest(err.Error() + ".")
	}
	cc, err := validAddresses(req.CC, "Cc")
	if err != nil {
		return httpx.BadRequest(err.Error() + ".")
	}
	if len(to) == 0 {
		return httpx.BadRequest("Add who it's to.")
	}
	if len(to)+len(cc) > maxRecipients {
		return httpx.BadRequest("Send to at most 10 people at once.")
	}
	mine := contactEmails(contact)
	hasContact := false
	for _, e := range to {
		hasContact = hasContact || slices.Contains(mine, e)
	}
	if !hasContact {
		return httpx.BadRequest("Send it to one of " + contact.Name + "'s email addresses (others can be added too).")
	}
	subject := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(req.Subject))
	body := strings.TrimRight(req.Body, " \n")
	if subject == "" {
		return httpx.BadRequest("Add a subject.")
	}
	if len([]rune(subject)) > 200 || len([]rune(body)) > maxNoteChars {
		return httpx.BadRequest("The subject can be at most 200 characters and the message 20,000.")
	}
	if strings.TrimSpace(body) == "" {
		return httpx.BadRequest("Write the message first.")
	}
	if left := unknownMergeFields(subject + " " + body); len(left) > 0 {
		return httpx.BadRequest("Fill in the merge fields first: {{" + strings.Join(left, "}}, {{") + "}}.")
	}
	also, err := s.mailLinks(ctx, userID, orgID, contactID, req.DealID, req.TicketID)
	if err != nil {
		return err
	}
	if req.DraftID != nil {
		var owner uuid.UUID
		if err := s.d.Pool.QueryRow(ctx, `SELECT user_id FROM crm_email_drafts WHERE id = $1`, *req.DraftID).Scan(&owner); err != nil || owner != userID {
			return httpx.NotFound("Draft not found.")
		}
	}
	box, conn, err := s.openMailbox(ctx, userID, orgID, req.ConnectionID, true)
	if err != nil {
		return err
	}
	msgID, threadID, err := box.send(ctx, outMail{To: to, CC: cc, Subject: subject, Body: body})
	if err != nil {
		return err
	}
	// Sent: from here on the email exists in the mailbox, so log it even if
	// the request is cancelled.
	ctx = context.WithoutCancel(ctx)
	dir := "outbound"
	meta := &EmailMeta{Provider: conn.Provider, ConnectionID: conn.ID, MessageID: msgID, ThreadID: threadID,
		From: strings.ToLower(conn.Account), To: to, CC: cc}
	if meta.Provider == "" {
		meta.Provider = providerOfBox(box)
	}
	if msgID == "" {
		meta.MessageID = "sent:" + uuid.NewString()
	}
	a, err := s.logActivity(ctx, userID, objContact, contactID, activityReq{Type: "email", Subject: &subject, Body: &body, Direction: &dir,
		AlsoOn: also, email: meta})
	if err != nil {
		return err
	}
	if req.DraftID != nil {
		_, _ = s.d.Pool.Exec(ctx, `DELETE FROM crm_email_drafts WHERE id = $1 AND user_id = $2`, *req.DraftID, userID)
	}
	httpx.JSON(w, http.StatusCreated, a)
	return nil
}

func providerOfBox(b mailbox) string {
	if _, ok := b.(*gmailBox); ok {
		return mailProviderGoogle
	}
	return mailProviderMicrosoft
}

// mailLinks is the records an email also shows on: the deal or ticket named
// (comment needed) and the contact's primary company when the caller can
// comment there.
func (s *Service) mailLinks(ctx context.Context, userID, orgID, contactID uuid.UUID, dealID, ticketID *uuid.UUID) ([]recordInput, error) {
	var out []recordInput
	if dealID != nil {
		if _, _, err := s.requireRecord(ctx, s.d.Pool, userID, objDeal, *dealID, access.Comment); err != nil {
			return nil, err
		}
		out = append(out, recordInput{Type: string(objDeal), ID: *dealID})
	}
	if ticketID != nil {
		if _, _, err := s.requireRecord(ctx, s.d.Pool, userID, objTicket, *ticketID, access.Comment); err != nil {
			return nil, err
		}
		out = append(out, recordInput{Type: string(objTicket), ID: *ticketID})
	}
	var companyID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT company_id FROM crm_contact_companies WHERE contact_id = $1 AND is_primary`, contactID).Scan(&companyID)
	if err == nil {
		if lvl, err := access.LevelOf(ctx, s.d.Pool, userID, KindCompany, companyID); err == nil && lvl >= access.Comment {
			out = append(out, recordInput{Type: string(objCompany), ID: companyID})
		}
	} else if !db.IsNoRows(err) {
		return nil, err
	}
	return out, nil
}

// recentMail: GET /v1/crm/contacts/{id}/mail/recent?connection_id= (view on
// the contact): the latest messages with the contact's addresses, read live
// from the caller's mailbox; nothing is stored.
func (s *Service) recentMail(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	contactID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	userID := me(r)
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, userID, objContact, contactID, access.View)
	if err != nil {
		return err
	}
	rec, err := s.getOne(ctx, s.d.Pool, userID, objContact, contactID)
	if err != nil {
		return err
	}
	emails := contactEmails(rec.(*Contact))
	if len(emails) == 0 {
		httpx.OK(w, map[string]any{"items": []MailSummary{}})
		return nil
	}
	box, conn, err := s.openMailbox(ctx, userID, orgID, r.URL.Query().Get("connection_id"), false)
	if err != nil {
		return err
	}
	list, err := box.recent(ctx, emails, recentMailLimit)
	if err != nil {
		return err
	}
	ids := make([]string, len(list))
	for i, m := range list {
		ids[i] = m.MessageID
	}
	logged := map[string]bool{}
	rows, err := s.d.Pool.Query(ctx, `SELECT email->>'message_id' FROM crm_activities WHERE org_id = $1 AND email IS NOT NULL
		AND email->>'connection_id' = $2 AND email->>'message_id' = ANY ($3)`, orgID, conn.ID, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			logged[id] = true
		}
	}
	rows.Close()
	for i := range list {
		list[i].Logged = logged[list[i].MessageID]
	}
	httpx.OK(w, map[string]any{"items": list, "account": conn.Account})
	return nil
}

type logMailReq struct {
	ConnectionID string     `json:"connection_id"`
	MessageID    string     `json:"message_id"`
	DealID       *uuid.UUID `json:"deal_id"`
	TicketID     *uuid.UUID `json:"ticket_id"`
}

// logMail: POST /v1/crm/contacts/{id}/mail/log (comment on the contact):
// fetch one message from the caller's mailbox and keep it on the timeline.
// The message must involve one of the contact's addresses.
func (s *Service) logMail(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	contactID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req logMailReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.MessageID) == "" || len(req.MessageID) > 300 {
		return httpx.BadRequest("Choose an email to log.")
	}
	userID := me(r)
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, userID, objContact, contactID, access.Comment)
	if err != nil {
		return err
	}
	rec, err := s.getOne(ctx, s.d.Pool, userID, objContact, contactID)
	if err != nil {
		return err
	}
	emails := contactEmails(rec.(*Contact))
	also, err := s.mailLinks(ctx, userID, orgID, contactID, req.DealID, req.TicketID)
	if err != nil {
		return err
	}
	box, conn, err := s.openMailbox(ctx, userID, orgID, req.ConnectionID, false)
	if err != nil {
		return err
	}
	m, err := box.get(ctx, req.MessageID)
	if err != nil {
		return err
	}
	involved := slices.Contains(emails, m.From)
	for _, e := range append(append([]string{}, m.To...), m.CC...) {
		involved = involved || slices.Contains(emails, e)
	}
	if !involved {
		return httpx.BadRequest("That email isn't to or from " + rec.base().Title + ".")
	}
	subject := clip(strings.TrimSpace(m.Subject), 200)
	body := strings.TrimSpace(m.Body)
	if len([]rune(body)) > maxNoteChars {
		body = string([]rune(body)[:maxNoteChars-1]) + "…"
	}
	if subject == "" && body == "" {
		subject = "(no subject)"
	}
	dir := "outbound"
	if m.Inbound {
		dir = "inbound"
	}
	at := m.At
	if at.IsZero() || at.After(time.Now().Add(time.Hour)) {
		at = time.Now()
	}
	meta := &EmailMeta{Provider: providerOfBox(box), ConnectionID: conn.ID, MessageID: m.MessageID, ThreadID: m.ThreadID, From: m.From, To: m.To, CC: m.CC}
	a, err := s.logActivity(ctx, userID, objContact, contactID, activityReq{Type: "email", Subject: &subject, Body: &body, Direction: &dir,
		OccurredAt: &at, AlsoOn: also, email: meta})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, a)
	return nil
}

// ---- templates ------------------------------------------------------------------------

// EmailTemplate is a crm_email_templates row.
type EmailTemplate struct {
	ID        uuid.UUID  `json:"id"`
	OrgID     uuid.UUID  `json:"org_id"`
	Owner     *UserRef   `json:"owner"`
	Shared    bool       `json:"shared"`
	Name      string     `json:"name"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body"`
	CanEdit   bool       `json:"can_edit"`
	UpdatedAt time.Time  `json:"updated_at"`
	ownerID   *uuid.UUID `json:"-"`
}

func (s *Service) templateCanEdit(ctx context.Context, t *EmailTemplate, userID uuid.UUID) bool {
	if t.ownerID != nil && *t.ownerID == userID && !t.Shared {
		return true
	}
	return allowedHere(ctx, s.d.Pool, t.OrgID, userID, PermManageSettings) || (t.ownerID != nil && *t.ownerID == userID)
}

func (s *Service) loadTemplates(ctx context.Context, orgID, userID uuid.UUID, id *uuid.UUID) ([]*EmailTemplate, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT t.id, t.org_id, t.owner_id, u.display_name, u.avatar_url, t.shared, t.name, t.subject, t.body, t.updated_at
		FROM crm_email_templates t LEFT JOIN users u ON u.id = t.owner_id AND u.status <> 'deleted'
		WHERE t.org_id = $1 AND (t.shared OR t.owner_id = $2) AND ($3::uuid IS NULL OR t.id = $3)
		ORDER BY lower(t.name), t.id LIMIT 500`, orgID, userID, id)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*EmailTemplate, error) {
		t := &EmailTemplate{}
		var name, avatar *string
		err := row.Scan(&t.ID, &t.OrgID, &t.ownerID, &name, &avatar, &t.Shared, &t.Name, &t.Subject, &t.Body, &t.UpdatedAt)
		if t.ownerID != nil && name != nil {
			t.Owner = &UserRef{ID: *t.ownerID, Name: *name, AvatarURL: avatar}
		}
		return t, err
	})
	if err != nil {
		return nil, err
	}
	for _, t := range out {
		t.CanEdit = s.templateCanEdit(ctx, t, userID)
	}
	if out == nil {
		out = []*EmailTemplate{}
	}
	return out, nil
}

// listTemplates: GET /v1/crm/orgs/{orgID}/mail/templates — the caller's and shared ones.
func (s *Service) listTemplates(w http.ResponseWriter, r *http.Request) error {
	list, err := s.loadTemplates(r.Context(), orgParam(r), me(r), nil)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list, "merge_fields": MergeFields})
	return nil
}

type templateReq struct {
	Name    *string `json:"name"`
	Subject *string `json:"subject"`
	Body    *string `json:"body"`
	Shared  *bool   `json:"shared"`
}

func (req templateReq) apply(t *EmailTemplate) error {
	if req.Name != nil {
		t.Name = strings.TrimSpace(*req.Name)
	}
	if req.Subject != nil {
		t.Subject = strings.TrimSpace(*req.Subject)
	}
	if req.Body != nil {
		t.Body = strings.TrimRight(*req.Body, " \n")
	}
	if req.Shared != nil {
		t.Shared = *req.Shared
	}
	if t.Name == "" || len([]rune(t.Name)) > 120 {
		return httpx.BadRequest("Give the template a name of up to 120 characters.")
	}
	if len([]rune(t.Subject)) > 300 || len([]rune(t.Body)) > maxNoteChars {
		return httpx.BadRequest("The subject can be at most 300 characters and the message 20,000.")
	}
	if bad := unknownMergeFields(t.Subject + " " + t.Body); len(bad) > 0 {
		return httpx.BadRequest("Unknown merge field: {{" + strings.Join(bad, "}}, {{") + "}}.")
	}
	return nil
}

// createTemplate: POST /v1/crm/orgs/{orgID}/mail/templates (shared ones need crm.manage_settings).
func (s *Service) createTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req templateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	t := &EmailTemplate{OrgID: orgID}
	if err := req.apply(t); err != nil {
		return err
	}
	if t.Shared && !allowedHere(ctx, s.d.Pool, orgID, me(r), PermManageSettings) {
		return permissionRequired(PermManageSettings)
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_email_templates WHERE org_id = $1 AND owner_id = $2`, orgID, me(r)).Scan(&n); err != nil {
		return err
	}
	if n >= 200 {
		return httpx.BadRequest("You can have at most 200 templates.")
	}
	var id uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `INSERT INTO crm_email_templates (org_id, owner_id, shared, name, subject, body) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		orgID, me(r), t.Shared, t.Name, t.Subject, t.Body).Scan(&id); err != nil {
		return err
	}
	list, err := s.loadTemplates(ctx, orgID, me(r), &id)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.JSON(w, http.StatusCreated, list[0])
	return nil
}

// updateTemplate / deleteTemplate: PATCH, DELETE /v1/crm/orgs/{orgID}/mail/templates/{templateID}
// (the owner; shared ones also crm.manage_settings).
func (s *Service) updateTemplate(w http.ResponseWriter, r *http.Request) error {
	return s.changeTemplate(w, r, false)
}

func (s *Service) deleteTemplate(w http.ResponseWriter, r *http.Request) error {
	return s.changeTemplate(w, r, true)
}

func (s *Service) changeTemplate(w http.ResponseWriter, r *http.Request, del bool) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "templateID")
	if err != nil {
		return err
	}
	list, err := s.loadTemplates(ctx, orgID, me(r), &id)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return httpx.NotFound("Template not found.")
	}
	t := list[0]
	owner := t.ownerID != nil && *t.ownerID == me(r)
	manager := allowedHere(ctx, s.d.Pool, orgID, me(r), PermManageSettings)
	if !(owner && !t.Shared) && !manager {
		if !owner {
			return httpx.Forbidden("Only its owner or someone who manages CRM settings can change this template.")
		}
		return permissionRequired(PermManageSettings)
	}
	if del {
		if _, err := s.d.Pool.Exec(ctx, `DELETE FROM crm_email_templates WHERE id = $1`, id); err != nil {
			return err
		}
		httpx.NoContent(w)
		return nil
	}
	var req templateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.apply(t); err != nil {
		return err
	}
	if t.Shared && !manager {
		return permissionRequired(PermManageSettings)
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE crm_email_templates SET name = $2, subject = $3, body = $4, shared = $5, updated_at = now() WHERE id = $1`,
		id, t.Name, t.Subject, t.Body, t.Shared); err != nil {
		return err
	}
	list, err = s.loadTemplates(ctx, orgID, me(r), &id)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.OK(w, list[0])
	return nil
}

// ---- merge rendering ---------------------------------------------------------------------

type renderReq struct {
	ContactID uuid.UUID  `json:"contact_id"`
	DealID    *uuid.UUID `json:"deal_id"`
	TicketID  *uuid.UUID `json:"ticket_id"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body"`
}

// Rendered is a subject and body with merge fields filled.
type Rendered struct {
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	Missing []string `json:"missing"`
}

// renderMail: POST /v1/crm/mail/render (view on each record named).
func (s *Service) renderMail(w http.ResponseWriter, r *http.Request) error {
	var req renderReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	out, err := s.render(r.Context(), me(r), req)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// mergeValues collects merge values from records the caller can view.
func (s *Service) mergeValues(ctx context.Context, userID, contactID uuid.UUID, dealID, ticketID *uuid.UUID) (map[string]string, uuid.UUID, error) {
	vals := map[string]string{}
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, userID, objContact, contactID, access.View)
	if err != nil {
		return nil, uuid.Nil, err
	}
	rec, err := s.getOne(ctx, s.d.Pool, userID, objContact, contactID)
	if err != nil {
		return nil, uuid.Nil, err
	}
	c := rec.(*Contact)
	vals["contact.first_name"], vals["contact.last_name"], vals["contact.job_title"] = c.FirstName, c.LastName, c.JobTitle
	vals["contact.name"] = strings.TrimSpace(c.FirstName + " " + c.LastName)
	if len(c.Emails) > 0 {
		vals["contact.email"] = c.Emails[0]
	}
	if c.Company != nil {
		if co, err := s.getOne(ctx, s.d.Pool, userID, objCompany, c.Company.ID); err == nil {
			vals["company.name"], vals["company.domain"] = co.(*Company).Name, deref(co.(*Company).Domain)
		}
	}
	if dealID != nil {
		if _, _, err := s.requireRecord(ctx, s.d.Pool, userID, objDeal, *dealID, access.View); err != nil {
			return nil, uuid.Nil, err
		}
		d, err := s.getOne(ctx, s.d.Pool, userID, objDeal, *dealID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		deal := d.(*Deal)
		vals["deal.name"], vals["deal.stage"], vals["deal.close_date"] = deal.Name, deal.Stage.Name, deref(deal.CloseDate)
		if deal.Amount != nil {
			vals["deal.amount"] = formatMoney(*deal.Amount, deal.Currency)
		}
		if vals["company.name"] == "" && deal.Company != nil {
			vals["company.name"] = deal.Company.Name
		}
	}
	if ticketID != nil {
		if _, _, err := s.requireRecord(ctx, s.d.Pool, userID, objTicket, *ticketID, access.View); err != nil {
			return nil, uuid.Nil, err
		}
		t, err := s.getOne(ctx, s.d.Pool, userID, objTicket, *ticketID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		vals["ticket.name"], vals["ticket.status"] = t.(*Ticket).Name, t.(*Ticket).Stage.Name
	}
	var name, email string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name, COALESCE(email, '') FROM users WHERE id = $1`, userID).Scan(&name, &email)
	vals["sender.name"], vals["sender.first_name"], vals["sender.email"] = name, firstName(name), email
	if links, err := s.bookingLinksFor(ctx, userID, orgID); err == nil && len(links) > 0 {
		vals["booking_link"] = links[0].URL
	}
	return vals, orgID, nil
}

func (s *Service) render(ctx context.Context, userID uuid.UUID, req renderReq) (*Rendered, error) {
	if req.ContactID == uuid.Nil {
		return nil, httpx.BadRequest("contact_id is required.")
	}
	vals, _, err := s.mergeValues(ctx, userID, req.ContactID, req.DealID, req.TicketID)
	if err != nil {
		return nil, err
	}
	subj, m1 := renderMerge(req.Subject, vals)
	body, m2 := renderMerge(req.Body, vals)
	missing := append(m1, m2...)
	slices.Sort(missing)
	return &Rendered{Subject: subj, Body: body, Missing: slices.Compact(missing)}, nil
}

// ---- drafts ---------------------------------------------------------------------------------

// EmailDraft is an email written in the app (by the person or their AI
// agent) waiting for the person to send it. Only its author sees it.
type EmailDraft struct {
	ID        uuid.UUID  `json:"id"`
	OrgID     uuid.UUID  `json:"org_id"`
	Contact   RecordRef  `json:"contact"`
	Deal      *RecordRef `json:"deal"`
	Ticket    *RecordRef `json:"ticket"`
	To        []string   `json:"to"`
	CC        []string   `json:"cc"`
	Subject   string     `json:"subject"`
	Body      string     `json:"body"`
	Agent     *string    `json:"agent"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (s *Service) loadDrafts(ctx context.Context, userID uuid.UUID, where string, args ...any) ([]*EmailDraft, error) {
	b := newSQL(ctx, userID)
	for range args {
		b.args = append(b.args, nil)
	}
	copy(b.args[2:], args)
	rows, err := s.d.Pool.Query(ctx, `SELECT d.id, d.org_id, d.contact_id, `+contactTitle("c")+`, d.deal_id, dl.name, d.ticket_id, tk.name,
		d.to_emails, d.cc_emails, d.subject, d.body, d.agent, d.created_at, d.updated_at
		FROM crm_email_drafts d JOIN crm_contacts c ON c.id = d.contact_id
		LEFT JOIN crm_deals dl ON dl.id = d.deal_id AND access_level($1, 'crm.deal', dl.id, $2) >= 1
		LEFT JOIN crm_tickets tk ON tk.id = d.ticket_id AND access_level($1, 'crm.ticket', tk.id, $2) >= 1
		WHERE d.user_id = $1 AND access_level($1, 'crm.contact', d.contact_id, $2) >= 1 AND `+where+`
		ORDER BY d.updated_at DESC, d.id DESC LIMIT 100`, b.args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*EmailDraft, error) {
		d := &EmailDraft{Contact: RecordRef{Type: objContact}}
		var dealID, ticketID *uuid.UUID
		var dealName, ticketName *string
		err := row.Scan(&d.ID, &d.OrgID, &d.Contact.ID, &d.Contact.Name, &dealID, &dealName, &ticketID, &ticketName,
			&d.To, &d.CC, &d.Subject, &d.Body, &d.Agent, &d.CreatedAt, &d.UpdatedAt)
		if dealID != nil && dealName != nil {
			d.Deal = &RecordRef{Type: objDeal, ID: *dealID, Name: *dealName}
		}
		if ticketID != nil && ticketName != nil {
			d.Ticket = &RecordRef{Type: objTicket, ID: *ticketID, Name: *ticketName}
		}
		return d, err
	})
	if out == nil {
		out = []*EmailDraft{}
	}
	return out, err
}

// listDrafts: GET /v1/crm/orgs/{orgID}/mail/drafts?contact_id= — the caller's own.
func (s *Service) listDrafts(w http.ResponseWriter, r *http.Request) error {
	where, args := "d.org_id = $3", []any{orgParam(r)}
	if v := r.URL.Query().Get("contact_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("contact_id must be a contact id.")
		}
		where, args = where+" AND d.contact_id = $4", append(args, id)
	}
	list, err := s.loadDrafts(r.Context(), me(r), where, args...)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type draftReq struct {
	To       []string   `json:"to"`
	CC       []string   `json:"cc"`
	Subject  *string    `json:"subject"`
	Body     *string    `json:"body"`
	DealID   *uuid.UUID `json:"deal_id"`
	TicketID *uuid.UUID `json:"ticket_id"`
}

// saveDraft creates (draftID nil) or updates one of the caller's drafts on
// a contact they can comment on. REST and the agent tool share it.
func (s *Service) saveDraft(ctx context.Context, userID, contactID uuid.UUID, draftID *uuid.UUID, req draftReq) (*EmailDraft, error) {
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, userID, objContact, contactID, access.Comment)
	if err != nil {
		return nil, err
	}
	to, err := validAddresses(req.To, "To")
	if err != nil {
		return nil, httpx.BadRequest(err.Error() + ".")
	}
	cc, err := validAddresses(req.CC, "Cc")
	if err != nil {
		return nil, httpx.BadRequest(err.Error() + ".")
	}
	if len(to)+len(cc) > maxRecipients {
		return nil, httpx.BadRequest("A draft can go to at most 10 people.")
	}
	if len(to) == 0 {
		rec, err := s.getOne(ctx, s.d.Pool, userID, objContact, contactID)
		if err != nil {
			return nil, err
		}
		if e := contactEmails(rec.(*Contact)); len(e) > 0 {
			to = e[:1]
		} else {
			return nil, httpx.BadRequest("This contact has no email address yet.")
		}
	}
	subject, body := strings.TrimSpace(deref(req.Subject)), strings.TrimRight(deref(req.Body), " \n")
	if len([]rune(subject)) > 300 || len([]rune(body)) > maxNoteChars {
		return nil, httpx.BadRequest("The subject can be at most 300 characters and the message 20,000.")
	}
	if subject == "" && strings.TrimSpace(body) == "" {
		return nil, httpx.BadRequest("Write a subject or a message first.")
	}
	for _, x := range []struct {
		o  objectType
		id *uuid.UUID
	}{{objDeal, req.DealID}, {objTicket, req.TicketID}} {
		if x.id == nil {
			continue
		}
		org, _, err := s.requireRecord(ctx, s.d.Pool, userID, x.o, *x.id, access.View)
		if err != nil {
			return nil, err
		}
		if org != orgID {
			return nil, httpx.BadRequest("Associate records from the same " + orgs.Noun() + ".")
		}
	}
	var id uuid.UUID
	if draftID == nil {
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_email_drafts WHERE user_id = $1`, userID).Scan(&n); err != nil {
			return nil, err
		}
		if n >= 500 {
			return nil, httpx.BadRequest("You have 500 drafts. Send or discard some first.")
		}
		err = s.d.Pool.QueryRow(ctx, `INSERT INTO crm_email_drafts (org_id, user_id, contact_id, deal_id, ticket_id, to_emails, cc_emails, subject, body, agent)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`, orgID, userID, contactID, req.DealID, req.TicketID, to, cc, subject, body,
			agentName(ctx)).Scan(&id)
	} else {
		id = *draftID
		var tag interface{ RowsAffected() int64 }
		tag, err = s.d.Pool.Exec(ctx, `UPDATE crm_email_drafts SET to_emails = $3, cc_emails = $4, subject = $5, body = $6, deal_id = $7, ticket_id = $8,
			updated_at = now() WHERE id = $1 AND user_id = $2 AND contact_id = $9`, id, userID, to, cc, subject, body, req.DealID, req.TicketID, contactID)
		if err == nil && tag.RowsAffected() == 0 {
			return nil, httpx.NotFound("Draft not found.")
		}
	}
	if err != nil {
		return nil, err
	}
	list, err := s.loadDrafts(ctx, userID, "d.id = $3", id)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, httpx.NotFound("Draft not found.")
	}
	return list[0], nil
}

// createDraft: POST /v1/crm/contacts/{id}/mail/drafts (comment on the contact).
func (s *Service) createDraft(w http.ResponseWriter, r *http.Request) error {
	contactID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req draftReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	d, err := s.saveDraft(r.Context(), me(r), contactID, nil, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

// updateDraft / deleteDraft: PATCH, DELETE /v1/crm/mail/drafts/{draftID} (its author).
func (s *Service) updateDraft(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "draftID")
	if err != nil {
		return err
	}
	var contactID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT contact_id FROM crm_email_drafts WHERE id = $1 AND user_id = $2`, id, me(r)).Scan(&contactID); err != nil {
		if db.IsNoRows(err) {
			return httpx.NotFound("Draft not found.")
		}
		return err
	}
	var req draftReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	d, err := s.saveDraft(ctx, me(r), contactID, &id, req)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) deleteDraft(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "draftID")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM crm_email_drafts WHERE id = $1 AND user_id = $2`, id, me(r))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Draft not found.")
	}
	httpx.NoContent(w)
	return nil
}
