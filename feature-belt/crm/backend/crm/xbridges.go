package crm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Bridges between the app's CRM and a connected one: a company linked to a
// platform company or account (only the link is stored; each viewer sees
// the platform side through their own connection), and a one-time import
// of selected platform records into the app's CRM.

// ---- links -----------------------------------------------------------------------

// CompanyLink is a link from one of the app's companies to a platform
// record, with what the viewer's own connection shows of it.
type CompanyLink struct {
	ID         uuid.UUID `json:"id"`
	Provider   string    `json:"provider"`
	AccountID  string    `json:"account_id"`
	ExternalID string    `json:"external_id"`
	CreatedAt  time.Time `json:"created_at"`
	// Status for this viewer: ok, no_connection (they haven't connected
	// that account), not_visible (their account can't see the record),
	// error (the platform didn't answer).
	Status    string          `json:"status"`
	Message   string          `json:"message,omitempty"`
	Source    *app.SourceRef  `json:"source,omitempty"`
	Record    xrecordish      `json:"record,omitempty"`
	Deals     []XAssocDeal    `json:"deals"`
	Contacts  []XAssocContact `json:"contacts"`
	Timeline  []*XActivity    `json:"timeline"`
	CanRemove bool            `json:"can_remove"`
	// Capability is what the viewer would connect to see it (no_connection).
	Capability string `json:"capability,omitempty"`
}

const linkTimelineSize = 5

type linkRow struct {
	id                              uuid.UUID
	provider, accountID, externalID string
	createdAt                       time.Time
}

// listLinks: GET /v1/crm/companies/{id}/links (view on the company).
func (s *Service) listLinks(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	orgID, lvl, err := s.requireRecord(ctx, s.d.Pool, me(r), objCompany, id, access.View)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, provider, account_id, external_id, created_at FROM crm_external_links
		WHERE company_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return err
	}
	var links []linkRow
	for rows.Next() {
		var l linkRow
		if err := rows.Scan(&l.id, &l.provider, &l.accountID, &l.externalID, &l.createdAt); err != nil {
			rows.Close()
			return err
		}
		links = append(links, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	out := []CompanyLink{}
	for _, l := range links {
		out = append(out, s.linkView(ctx, me(r), orgID, l, lvl >= access.Edit && !access.ViaAgent(ctx)))
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// linkView reads the linked record live through the viewer's own
// connection to the same platform account.
func (s *Service) linkView(ctx context.Context, userID, orgID uuid.UUID, l linkRow, canRemove bool) CompanyLink {
	v := CompanyLink{ID: l.id, Provider: l.provider, AccountID: l.accountID, ExternalID: l.externalID, CreatedAt: l.createdAt,
		Deals: []XAssocDeal{}, Contacts: []XAssocContact{}, Timeline: []*XActivity{}, CanRemove: canRemove}
	p, _ := providerByID(l.provider)
	v.Capability = p.Read
	st, err := s.sourceSettings(ctx, orgID)
	if err != nil || !st.allows(l.provider) {
		v.Status, v.Message = "off", p.Label+" isn't connected to the CRM in this workspace."
		return v
	}
	conns, err := s.connectedFor(ctx, userID, orgID, st)
	if err != nil {
		v.Status, v.Message = "error", "Your connections couldn't be read."
		return v
	}
	writable := s.writable(ctx, userID, orgID, st)
	var src Source
	for _, c := range conns {
		if c.Provider != l.provider || (c.Status != "" && c.Status != "active") {
			continue
		}
		cand, err := s.openSource(ctx, userID, orgID, c, writable[c.ID])
		if err != nil {
			continue
		}
		if cand.AccountID(ctx) == l.accountID {
			src = cand
			break
		}
	}
	if src == nil {
		v.Status, v.Message = "no_connection", "Connect "+p.Label+" to see the linked record."
		return v
	}
	d, err := src.Get(ctx, objCompany, l.externalID)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			v.Status, v.Message = "not_visible", "Your "+p.Label+" account can't see the linked record."
		} else {
			v.Status, v.Message = "error", p.Label+" didn't answer."
			if errors.As(err, &he) {
				v.Message = he.Message
			}
		}
		return v
	}
	v.Status, v.Record, v.Source = "ok", d.Record, d.Record.xbase().Source
	v.Deals, v.Contacts = d.Associations.Deals, d.Associations.Contacts
	if tl, err := src.Timeline(ctx, objCompany, l.externalID, "activities", "", linkTimelineSize); err == nil {
		for _, it := range tl.Items {
			v.Timeline = append(v.Timeline, it.Activity)
		}
	}
	return v
}

// createLink: POST /v1/crm/companies/{id}/links {record_id: <xref>} (edit
// on the company; the creator's own connection must see the platform record).
func (s *Service) createLink(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		RecordID string `json:"record_id"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	connID, pid, ok := parseXRef(req.RecordID)
	if !ok {
		return httpx.BadRequest("record_id is a connected company (from search).")
	}
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, me(r), objCompany, id, access.Edit)
	if err != nil {
		return err
	}
	src, err := s.source(ctx, me(r), orgID, connID)
	if err != nil {
		return err
	}
	if _, err := src.Get(ctx, objCompany, pid); err != nil {
		return err
	}
	account := src.AccountID(ctx)
	if account == "" {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "The platform didn't say which account this is. Try again.")
	}
	var l linkRow
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO crm_external_links (org_id, company_id, provider, account_id, external_id, created_by)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (company_id, provider, account_id) DO UPDATE SET external_id = EXCLUDED.external_id, created_by = EXCLUDED.created_by, created_at = now()
			RETURNING id, provider, account_id, external_id, created_at`,
			orgID, id, src.Connection().Provider, account, pid, me(r)).Scan(&l.id, &l.provider, &l.accountID, &l.externalID, &l.createdAt)
		if err != nil {
			return err
		}
		return addEvent(ctx, tx, orgID, objCompany, id, "updated", me(r), map[string]any{"changes": []change{{
			Key: "link", Label: "Linked record", Before: "", After: providerLabel(l.provider)}}})
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, objCompany, id, true)
	httpx.JSON(w, http.StatusCreated, s.linkView(ctx, me(r), orgID, l, true))
	return nil
}

func providerLabel(id string) string {
	if p, ok := providerByID(id); ok {
		return p.Label
	}
	return id
}

// deleteLink: DELETE /v1/crm/companies/{id}/links/{linkID} (edit on the company).
func (s *Service) deleteLink(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	linkID, err := idParam(r, "linkID")
	if err != nil {
		return err
	}
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, me(r), objCompany, id, access.Edit)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var provider string
		err := tx.QueryRow(ctx, `DELETE FROM crm_external_links WHERE id = $1 AND company_id = $2 RETURNING provider`, linkID, id).Scan(&provider)
		if db.IsNoRows(err) {
			return httpx.NotFound("That link is already gone.")
		}
		if err != nil {
			return err
		}
		return addEvent(ctx, tx, orgID, objCompany, id, "updated", me(r), map[string]any{"changes": []change{{
			Key: "link", Label: "Linked record", Before: providerLabel(provider), After: ""}}})
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, objCompany, id, true)
	httpx.NoContent(w)
	return nil
}

// ---- import into the app ------------------------------------------------------------

const maxImportIDs = 200

// SourceImportResult is what an import from a connected CRM did.
type SourceImportResult struct {
	Created int                 `json:"created"`
	Updated int                 `json:"updated"`
	Skipped int                 `json:"skipped"`
	Rows    []SourceImportRow   `json:"rows"`
	Errors  []SourceImportError `json:"errors"`
}

type SourceImportRow struct {
	SourceID string     `json:"source_id"`
	Action   string     `json:"action"` // created | updated | skipped
	Record   *RecordRef `json:"record,omitempty"`
}

type SourceImportError struct {
	SourceID string `json:"source_id"`
	Message  string `json:"message"`
}

// importFromSource: POST /v1/crm/orgs/{orgID}/sources/{sourceID}/import
// {object_type, ids: [xref or platform id…], dedupe: update|skip|create}
// (crm.import). A one-time copy: the new records are the app's own from then
// on, owned by the importer unless the platform owner's email is a member's.
func (s *Service) importFromSource(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID, userID := orgParam(r), me(r)
	var req struct {
		ObjectType string   `json:"object_type"`
		IDs        []string `json:"ids"`
		Dedupe     string   `json:"dedupe"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	o, ok := parseObject(req.ObjectType)
	if !ok {
		return httpx.BadRequest("object_type is company, contact or deal.")
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxImportIDs {
		return httpx.BadRequest("Pick 1 to 200 records to import.")
	}
	switch req.Dedupe {
	case "":
		req.Dedupe = "update"
	case "update", "skip", "create":
	default:
		return httpx.BadRequest("dedupe is update, skip or create.")
	}
	st, err := s.sourceSettings(ctx, orgID)
	if err != nil {
		return err
	}
	if !st.allowsApp() {
		return errAppSourceOff()
	}
	connID := chi.URLParam(r, "sourceID")
	src, err := s.source(ctx, userID, orgID, connID)
	if err != nil {
		return err
	}
	res := &SourceImportResult{Rows: []SourceImportRow{}, Errors: []SourceImportError{}}
	for _, raw := range req.IDs {
		pid := raw
		if c, p, ok := parseXRef(raw); ok {
			if c != connID {
				res.Errors = append(res.Errors, SourceImportError{SourceID: raw, Message: "That record is from another connection."})
				continue
			}
			pid = p
		}
		row, err := s.importOne(ctx, userID, orgID, src, o, pid, req.Dedupe)
		if err != nil {
			msg := "Couldn't import it."
			var he *httpx.Error
			if errors.As(err, &he) {
				msg = he.Message
				if app.ConnectionErrorCode(err) != "" {
					return err
				}
			}
			res.Errors = append(res.Errors, SourceImportError{SourceID: xref(connID, pid), Message: msg})
			continue
		}
		res.Rows = append(res.Rows, *row)
		switch row.Action {
		case "created":
			res.Created++
		case "updated":
			res.Updated++
		default:
			res.Skipped++
		}
	}
	if err := audit.Log(ctx, s.d.Pool, userID, "crm.import_from_source", "org", orgID.String(), map[string]any{
		"provider": src.Connection().Provider, "object": o, "requested": len(req.IDs), "created": res.Created, "updated": res.Updated,
		"skipped": res.Skipped, "errors": len(res.Errors), "dedupe": req.Dedupe}); err != nil {
		return err
	}
	httpx.OK(w, res)
	return nil
}

// importOne copies one platform record into the app's CRM.
func (s *Service) importOne(ctx context.Context, userID, orgID uuid.UUID, src Source, o objectType, pid, dedupe string) (*SourceImportRow, error) {
	d, err := src.Get(ctx, o, pid)
	if err != nil {
		return nil, err
	}
	in := map[string]json.RawMessage{}
	set := func(k string, v any) {
		if v == nil {
			return
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) == "" {
				return
			}
		case []string:
			if len(t) == 0 {
				return
			}
		}
		b, _ := json.Marshal(v)
		in[k] = b
	}
	var owner *XUserRef
	var companyDomain *string
	switch rec := d.Record.(type) {
	case *XCompany:
		set("name", rec.Name)
		if rec.Domain != nil {
			set("domain", *rec.Domain)
		}
		set("industry", rec.Industry)
		if rec.Employees != nil {
			set("employees", *rec.Employees)
		}
		set("phone", rec.Phone)
		set("city", rec.City)
		set("country", rec.Country)
		set("description", rec.Description)
		owner = rec.Owner
	case *XContact:
		set("first_name", rec.FirstName)
		set("last_name", rec.LastName)
		set("emails", rec.Emails)
		set("phones", rec.Phones)
		set("job_title", rec.JobTitle)
		for _, op := range lifecycleOptions {
			if op.Value == rec.LifecycleStage && rec.LifecycleStage != "other" {
				set("lifecycle_stage", rec.LifecycleStage)
			}
		}
		set("city", rec.City)
		set("country", rec.Country)
		owner = rec.Owner
		for _, c := range d.Associations.Companies {
			if c.IsPrimary || companyDomain == nil {
				companyDomain = c.Domain
			}
		}
	case *XDeal:
		set("name", rec.Name)
		if rec.Amount != nil {
			set("amount", *rec.Amount)
			set("currency", rec.Currency)
		}
		if rec.CloseDate != nil {
			set("close_date", *rec.CloseDate)
		}
		owner = rec.Owner
		if stage, err := s.matchStage(ctx, orgID, rec.Stage.Name); err == nil && stage != uuid.Nil {
			set("stage", stage.String())
		}
		for _, c := range d.Associations.Companies {
			if c.IsPrimary || companyDomain == nil {
				companyDomain = c.Domain
			}
		}
	}
	if owner != nil && owner.Email != "" {
		var member uuid.UUID
		err := s.d.Pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN org_members m ON m.user_id = u.id AND m.org_id = $1
			WHERE lower(u.email) = lower($2) AND u.status <> 'deleted' LIMIT 1`, orgID, owner.Email).Scan(&member)
		if err == nil {
			set("owner", member.String())
		}
	}
	if companyDomain != nil && o != objCompany {
		var companyID uuid.UUID
		b := newSQL(ctx, userID)
		err := s.d.Pool.QueryRow(ctx, `SELECT r.id FROM crm_companies r WHERE r.org_id = `+b.arg(orgID)+` AND r.domain = `+b.arg(*companyDomain)+`
			AND access_level($1, 'crm.company', r.id, $2) >= 1 ORDER BY r.created_at LIMIT 1`, b.args...).Scan(&companyID)
		if err == nil {
			if o == objContact {
				set("company_id", companyID.String())
			} else {
				set("company", companyID.String())
			}
		}
	}
	if dedupe == "create" {
		in["allow_duplicate"] = json.RawMessage("true")
	}
	rec, err := s.create(ctx, userID, orgID, o, cloneFields(in), true)
	if err == nil {
		b := rec.base()
		return &SourceImportRow{SourceID: xref(src.Connection().ID, pid), Action: "created", Record: &RecordRef{Type: o, ID: b.ID, Name: b.Title}}, nil
	}
	var he *httpx.Error
	if !errors.As(err, &he) || he.Code != "duplicate" {
		return nil, err
	}
	dups, _ := he.Details["matches"].([]Duplicate)
	if dedupe == "skip" || len(dups) == 0 {
		row := &SourceImportRow{SourceID: xref(src.Connection().ID, pid), Action: "skipped"}
		if len(dups) > 0 {
			row.Record = &dups[0].RecordRef
		}
		return row, nil
	}
	upd := cloneFields(in)
	delete(upd, "owner")
	delete(upd, "company_id")
	delete(upd, "allow_duplicate")
	updated, err := s.update(ctx, userID, o, dups[0].ID, upd)
	if err != nil {
		return nil, err
	}
	b := updated.base()
	return &SourceImportRow{SourceID: xref(src.Connection().ID, pid), Action: "updated", Record: &RecordRef{Type: o, ID: b.ID, Name: b.Title}}, nil
}

func cloneFields(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// matchStage finds the app's stage with the same name in the default
// pipeline (uuid.Nil: none, the deal lands in the first stage).
func (s *Service) matchStage(ctx context.Context, orgID uuid.UUID, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT st.id FROM crm_stages st JOIN crm_pipelines p ON p.id = st.pipeline_id
		WHERE p.org_id = $1 AND lower(st.name) = lower($2) ORDER BY p.position, p.created_at, st.position LIMIT 1`, orgID, strings.TrimSpace(name)).Scan(&id)
	if db.IsNoRows(err) {
		return uuid.Nil, nil
	}
	return id, err
}
