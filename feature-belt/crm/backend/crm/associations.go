package crm

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Associations are the records linked to one record that the caller can view.
type Associations struct {
	Companies []AssocCompany `json:"companies"`
	Contacts  []AssocContact `json:"contacts"`
	Deals     []AssocDeal    `json:"deals"`
}

type AssocCompany struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Domain    *string   `json:"domain"`
	IsPrimary bool      `json:"is_primary"`
}

type AssocContact struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Email    *string   `json:"email"`
	Phone    *string   `json:"phone"`
	JobTitle string    `json:"job_title"`
}

type AssocDeal struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Amount       *int64    `json:"amount"`
	Currency     string    `json:"currency"`
	StageName    string    `json:"stage_name"`
	StageOutcome string    `json:"stage_outcome"`
	CloseDate    *string   `json:"close_date"`
}

const assocLimit = 100

// associations lists what a record is linked to, keeping only records the
// caller can view (agent-capped through $2).
func (s *Service) associations(ctx context.Context, q db.Querier, userID uuid.UUID, o objectType, id uuid.UUID) (*Associations, error) {
	a := &Associations{Companies: []AssocCompany{}, Contacts: []AssocContact{}, Deals: []AssocDeal{}}
	b := newSQL(ctx, userID)
	idArg := b.arg(id)
	var companySQL, contactSQL, dealSQL string
	switch o {
	case objCompany:
		contactSQL = `SELECT c.id, ` + contactTitle("c") + `, c.emails[1], c.phones[1], c.job_title FROM crm_contact_companies cc
			JOIN crm_contacts c ON c.id = cc.contact_id WHERE cc.company_id = ` + idArg
		dealSQL = `SELECT d.id, d.name, d.amount, d.currency, st.name, st.outcome, to_char(d.close_date, 'YYYY-MM-DD') FROM crm_deals d
			JOIN crm_stages st ON st.id = d.stage_id WHERE d.company_id = ` + idArg
	case objContact:
		companySQL = `SELECT co.id, co.name, co.domain, cc.is_primary FROM crm_contact_companies cc
			JOIN crm_companies co ON co.id = cc.company_id WHERE cc.contact_id = ` + idArg
		dealSQL = `SELECT d.id, d.name, d.amount, d.currency, st.name, st.outcome, to_char(d.close_date, 'YYYY-MM-DD') FROM crm_deal_contacts dc
			JOIN crm_deals d ON d.id = dc.deal_id JOIN crm_stages st ON st.id = d.stage_id WHERE dc.contact_id = ` + idArg
	case objDeal:
		companySQL = `SELECT co.id, co.name, co.domain, true FROM crm_deals d JOIN crm_companies co ON co.id = d.company_id WHERE d.id = ` + idArg
		contactSQL = `SELECT c.id, ` + contactTitle("c") + `, c.emails[1], c.phones[1], c.job_title FROM crm_deal_contacts dc
			JOIN crm_contacts c ON c.id = dc.contact_id WHERE dc.deal_id = ` + idArg
	}
	if companySQL != "" {
		rows, err := q.Query(ctx, companySQL+` AND access_level($1, 'crm.company', co.id, $2) >= 1 ORDER BY 4 DESC, lower(co.name), co.id LIMIT `+strconv.Itoa(assocLimit), b.args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c AssocCompany
			if err := rows.Scan(&c.ID, &c.Name, &c.Domain, &c.IsPrimary); err != nil {
				rows.Close()
				return nil, err
			}
			a.Companies = append(a.Companies, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if contactSQL != "" {
		rows, err := q.Query(ctx, contactSQL+` AND access_level($1, 'crm.contact', c.id, $2) >= 1 ORDER BY lower(c.first_name || ' ' || c.last_name), c.id LIMIT `+strconv.Itoa(assocLimit), b.args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c AssocContact
			if err := rows.Scan(&c.ID, &c.Name, &c.Email, &c.Phone, &c.JobTitle); err != nil {
				rows.Close()
				return nil, err
			}
			a.Contacts = append(a.Contacts, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if dealSQL != "" {
		rows, err := q.Query(ctx, dealSQL+` AND access_level($1, 'crm.deal', d.id, $2) >= 1 ORDER BY (st.outcome = 'open') DESC, d.close_date NULLS LAST, d.id LIMIT `+strconv.Itoa(assocLimit), b.args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d AssocDeal
			if err := rows.Scan(&d.ID, &d.Name, &d.Amount, &d.Currency, &d.StageName, &d.StageOutcome, &d.CloseDate); err != nil {
				rows.Close()
				return nil, err
			}
			a.Deals = append(a.Deals, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func contactTitle(alias string) string {
	return `COALESCE(NULLIF(btrim(` + alias + `.first_name || ' ' || ` + alias + `.last_name), ''), ` + alias + `.emails[1], 'Unnamed contact')`
}

// ---- linking (edit on the record being changed, view on the other) -----------

// requireOther checks the caller can view a record to link, in the same org.
func (s *Service) requireOther(ctx context.Context, q db.Querier, userID, orgID uuid.UUID, o objectType, id uuid.UUID) (string, error) {
	lvl, err := access.LevelOf(ctx, q, userID, o.kind(), id)
	if err != nil {
		return "", err
	}
	var org uuid.UUID
	var title string
	if lvl >= access.View {
		err = q.QueryRow(ctx, `SELECT r.org_id, `+o.titleExpr()+` FROM `+o.table()+` r WHERE r.id = $1`, id).Scan(&org, &title)
		if err != nil && !db.IsNoRows(err) {
			return "", err
		}
	}
	if lvl < access.View || org != orgID {
		return "", verr("Choose a %s you can see in this %s.", o.label(), orgs.Noun())
	}
	return title, nil
}

func (s *Service) linkContactCompany(ctx context.Context, tx pgx.Tx, userID, orgID, contactID, companyID uuid.UUID, primary bool) error {
	companyName, err := s.requireOther(ctx, tx, userID, orgID, objCompany, companyID)
	if err != nil {
		return err
	}
	var exists, isPrimary bool
	err = tx.QueryRow(ctx, `SELECT true, is_primary FROM crm_contact_companies WHERE contact_id = $1 AND company_id = $2`, contactID, companyID).Scan(&exists, &isPrimary)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if exists && (!primary || isPrimary) {
		return nil // already linked as asked
	}
	if exists {
		if _, err := tx.Exec(ctx, `UPDATE crm_contact_companies SET is_primary = false WHERE contact_id = $1 AND is_primary`, contactID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE crm_contact_companies SET is_primary = true WHERE contact_id = $1 AND company_id = $2`, contactID, companyID)
		return err
	}
	var hasPrimary bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm_contact_companies WHERE contact_id = $1 AND is_primary)`, contactID).Scan(&hasPrimary); err != nil {
		return err
	}
	if primary || !hasPrimary {
		if _, err := tx.Exec(ctx, `UPDATE crm_contact_companies SET is_primary = false WHERE contact_id = $1 AND is_primary`, contactID); err != nil {
			return err
		}
		primary = true
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO crm_contact_companies (contact_id, company_id, org_id, is_primary, created_by) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (contact_id, company_id) DO NOTHING`,
		contactID, companyID, orgID, primary, userID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	contactName, _ := recordTitle(ctx, tx, objContact, contactID)
	if err := addEvent(ctx, tx, orgID, objContact, contactID, "associated", userID, map[string]any{"other_type": objCompany, "other_id": companyID, "other_name": companyName}); err != nil {
		return err
	}
	return addEvent(ctx, tx, orgID, objCompany, companyID, "associated", userID, map[string]any{"other_type": objContact, "other_id": contactID, "other_name": contactName})
}

func (s *Service) linkDealContact(ctx context.Context, tx pgx.Tx, userID, orgID, dealID, contactID uuid.UUID) error {
	contactName, err := s.requireOther(ctx, tx, userID, orgID, objContact, contactID)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO crm_deal_contacts (deal_id, contact_id, org_id, created_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, dealID, contactID, orgID, userID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	dealName, _ := recordTitle(ctx, tx, objDeal, dealID)
	if err := addEvent(ctx, tx, orgID, objDeal, dealID, "associated", userID, map[string]any{"other_type": objContact, "other_id": contactID, "other_name": contactName}); err != nil {
		return err
	}
	return addEvent(ctx, tx, orgID, objContact, contactID, "associated", userID, map[string]any{"other_type": objDeal, "other_id": dealID, "other_name": dealName})
}

type linkReq struct {
	CompanyID uuid.UUID `json:"company_id"`
	ContactID uuid.UUID `json:"contact_id"`
	Primary   bool      `json:"primary"`
}

// addContactCompany: POST /v1/crm/contacts/{id}/companies {company_id, primary}
func (s *Service) addContactCompany(w http.ResponseWriter, r *http.Request) error {
	var req linkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.CompanyID == uuid.Nil {
		return httpx.BadRequest("Choose a company.")
	}
	return s.relink(w, r, objContact, func(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) ([]uuid.UUID, error) {
		return []uuid.UUID{req.CompanyID}, s.linkContactCompany(ctx, tx, me(r), orgID, id, req.CompanyID, req.Primary)
	}, objCompany)
}

// setPrimaryCompany: PUT /v1/crm/contacts/{id}/companies/{companyID}/primary
func (s *Service) setPrimaryCompany(w http.ResponseWriter, r *http.Request) error {
	companyID, err := idParam(r, "companyID")
	if err != nil {
		return err
	}
	return s.relink(w, r, objContact, func(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) ([]uuid.UUID, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm_contact_companies WHERE contact_id = $1 AND company_id = $2)`, id, companyID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, httpx.NotFound("That company isn't associated with this contact.")
		}
		return nil, s.linkContactCompany(ctx, tx, me(r), orgID, id, companyID, true)
	}, objCompany)
}

// removeContactCompany: DELETE /v1/crm/contacts/{id}/companies/{companyID}
func (s *Service) removeContactCompany(w http.ResponseWriter, r *http.Request) error {
	companyID, err := idParam(r, "companyID")
	if err != nil {
		return err
	}
	return s.relink(w, r, objContact, func(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) ([]uuid.UUID, error) {
		name, err := s.requireOther(ctx, tx, me(r), orgID, objCompany, companyID)
		if err != nil {
			return nil, err
		}
		var wasPrimary bool
		err = tx.QueryRow(ctx, `DELETE FROM crm_contact_companies WHERE contact_id = $1 AND company_id = $2 RETURNING is_primary`, id, companyID).Scan(&wasPrimary)
		if db.IsNoRows(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if wasPrimary {
			if _, err := tx.Exec(ctx, `UPDATE crm_contact_companies SET is_primary = true WHERE (contact_id, company_id) =
				(SELECT contact_id, company_id FROM crm_contact_companies WHERE contact_id = $1 ORDER BY created_at, company_id LIMIT 1)`, id); err != nil {
				return nil, err
			}
		}
		contactName, _ := recordTitle(ctx, tx, objContact, id)
		if err := addEvent(ctx, tx, orgID, objContact, id, "dissociated", me(r), map[string]any{"other_type": objCompany, "other_id": companyID, "other_name": name}); err != nil {
			return nil, err
		}
		return []uuid.UUID{companyID}, addEvent(ctx, tx, orgID, objCompany, companyID, "dissociated", me(r), map[string]any{"other_type": objContact, "other_id": id, "other_name": contactName})
	}, objCompany)
}

// addDealContact: POST /v1/crm/deals/{id}/contacts {contact_id}
func (s *Service) addDealContact(w http.ResponseWriter, r *http.Request) error {
	var req linkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ContactID == uuid.Nil {
		return httpx.BadRequest("Choose a contact.")
	}
	return s.relink(w, r, objDeal, func(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) ([]uuid.UUID, error) {
		return []uuid.UUID{req.ContactID}, s.linkDealContact(ctx, tx, me(r), orgID, id, req.ContactID)
	}, objContact)
}

// removeDealContact: DELETE /v1/crm/deals/{id}/contacts/{contactID}
func (s *Service) removeDealContact(w http.ResponseWriter, r *http.Request) error {
	contactID, err := idParam(r, "contactID")
	if err != nil {
		return err
	}
	return s.relink(w, r, objDeal, func(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) ([]uuid.UUID, error) {
		name, err := s.requireOther(ctx, tx, me(r), orgID, objContact, contactID)
		if err != nil {
			return nil, err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM crm_deal_contacts WHERE deal_id = $1 AND contact_id = $2`, id, contactID)
		if err != nil || tag.RowsAffected() == 0 {
			return nil, err
		}
		dealName, _ := recordTitle(ctx, tx, objDeal, id)
		if err := addEvent(ctx, tx, orgID, objDeal, id, "dissociated", me(r), map[string]any{"other_type": objContact, "other_id": contactID, "other_name": name}); err != nil {
			return nil, err
		}
		return []uuid.UUID{contactID}, addEvent(ctx, tx, orgID, objContact, contactID, "dissociated", me(r), map[string]any{"other_type": objDeal, "other_id": id, "other_name": dealName})
	}, objContact)
}

// relink runs an association change on record {id} of o (edit needed) and
// answers with the fresh associations.
func (s *Service) relink(w http.ResponseWriter, r *http.Request, o objectType, fn func(ctx context.Context, tx pgx.Tx, orgID, id uuid.UUID) ([]uuid.UUID, error), other objectType) error {
	ctx := r.Context()
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var touched []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		orgID, _, err := s.requireRecord(ctx, tx, me(r), o, id, access.Edit)
		if err != nil {
			return err
		}
		touched, err = fn(ctx, tx, orgID, id)
		return asHTTP(err)
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, o, id, true)
	for _, t := range touched {
		s.recordChanged(ctx, other, t, true)
	}
	a, err := s.associations(ctx, s.d.Pool, me(r), o, id)
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}
