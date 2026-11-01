package crm

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
)

// ---- duplicates ------------------------------------------------------------------------

// Duplicate is a visible record that matches a new one.
type Duplicate struct {
	RecordRef
	Match string `json:"match"` // the email or domain that matched
}

// findDuplicates returns up to 5 records the caller can view that share an
// email (contacts) or a domain (companies) with cols. Records the caller
// can't see are never reported.
func (s *Service) findDuplicates(ctx context.Context, q db.Querier, userID, orgID uuid.UUID, o objectType, cols map[string]any, exclude uuid.UUID) ([]Duplicate, error) {
	b := newSQL(ctx, userID)
	var cond, match string
	switch o {
	case objContact:
		emails, _ := cols["emails"].([]string)
		if len(emails) == 0 {
			return nil, nil
		}
		a := b.arg(emails)
		cond = "r.emails && " + a + "::text[]"
		match = "(SELECT e FROM unnest(r.emails) e WHERE e = ANY (" + a + "::text[]) LIMIT 1)"
	case objCompany:
		domain, _ := cols["domain"].(string)
		if domain == "" {
			return nil, nil
		}
		a := b.arg(domain)
		cond, match = "r.domain = "+a, "r.domain"
	default:
		return nil, nil
	}
	rows, err := q.Query(ctx, `SELECT r.id, `+o.titleExpr()+`, `+match+` FROM `+o.table()+` r
		WHERE r.org_id = `+b.arg(orgID)+` AND `+cond+` AND r.id <> `+b.arg(exclude)+`
		  AND access_level($1, '`+o.kind()+`', r.id, $2) >= 1
		ORDER BY r.created_at, r.id LIMIT 5`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Duplicate{}
	for rows.Next() {
		d := Duplicate{RecordRef: RecordRef{Type: o}}
		if err := rows.Scan(&d.ID, &d.Name, &d.Match); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func duplicateMessage(o objectType, dups []Duplicate) string {
	if o == objContact {
		return "A contact with " + dups[0].Match + " already exists: " + dups[0].Name + "."
	}
	return "A company with the domain " + dups[0].Match + " already exists: " + dups[0].Name + "."
}

// duplicates: GET /v1/crm/orgs/{orgID}/duplicates?object=contact&emails=a@x.com,b@y.com
// or ?object=company&domain=x.com, optional &exclude=<id> — for the warning
// while someone types.
func (s *Service) duplicates(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := r.URL.Query()
	o, ok := parseObject(q.Get("object"))
	if !ok || o == objDeal {
		return httpx.BadRequest("object is company or contact.")
	}
	cols := map[string]any{}
	if o == objContact {
		var emails []string
		for _, e := range strings.Split(q.Get("emails"), ",") {
			if c, err := cleanEmail(e); err == nil {
				emails = append(emails, c)
			}
		}
		cols["emails"] = emails
	} else if d, err := cleanDomain(q.Get("domain")); err == nil {
		cols["domain"] = d
	}
	exclude := uuid.Nil
	if v := q.Get("exclude"); v != "" {
		exclude, _ = uuid.Parse(v)
	}
	dups, err := s.findDuplicates(ctx, s.d.Pool, me(r), orgParam(r), o, cols, exclude)
	if err != nil {
		return err
	}
	if dups == nil {
		dups = []Duplicate{}
	}
	httpx.OK(w, map[string]any{"matches": dups})
	return nil
}

// ---- bulk actions ----------------------------------------------------------------------

type bulkReq struct {
	ObjectType string      `json:"object_type"`
	IDs        []uuid.UUID `json:"ids"`
	Action     string      `json:"action"` // assign_owner | add_tag | remove_tag | move_stage | delete
	OwnerID    *uuid.UUID  `json:"owner_id"`
	TagID      *uuid.UUID  `json:"tag_id"`
	StageID    *uuid.UUID  `json:"stage_id"`
}

// BulkResult says what a bulk action changed and what it skipped.
type BulkResult struct {
	Updated int           `json:"updated"`
	Skipped []BulkSkipped `json:"skipped"`
}

type BulkSkipped struct {
	ID     uuid.UUID `json:"id"`
	Reason string    `json:"reason"`
}

// bulk: POST /v1/crm/orgs/{orgID}/bulk — up to 200 records; each needs edit
// (and delete needs crm.delete). Records the caller can't edit are skipped.
func (s *Service) bulk(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req bulkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	o, ok := parseObject(req.ObjectType)
	if !ok {
		return httpx.BadRequest("object_type is company, contact or deal.")
	}
	ids := []uuid.UUID{}
	for _, id := range req.IDs {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > maxBulkIDs {
		return httpx.BadRequest("Choose 1 to 200 records.")
	}
	res := BulkResult{Skipped: []BulkSkipped{}}
	var in map[string]json.RawMessage
	switch req.Action {
	case "assign_owner":
		v := json.RawMessage("null")
		if req.OwnerID != nil {
			v = json.RawMessage(`"` + req.OwnerID.String() + `"`)
		}
		in = map[string]json.RawMessage{"owner": v}
	case "move_stage":
		if o != objDeal || req.StageID == nil {
			return httpx.BadRequest("Moving stage needs deals and a stage_id.")
		}
		in = map[string]json.RawMessage{"stage": json.RawMessage(`"` + req.StageID.String() + `"`)}
	case "add_tag", "remove_tag":
		if req.TagID == nil {
			return httpx.BadRequest("Choose a tag.")
		}
		var exists bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm_tags WHERE id = $1 AND org_id = $2)`, *req.TagID, orgID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpx.BadRequest("Choose a tag from this " + orgs.Noun() + ".")
		}
	case "delete":
		if !allowedHere(ctx, s.d.Pool, orgID, me(r), PermDelete) {
			return permissionRequired(PermDelete)
		}
		return s.bulkDelete(w, r, orgID, o, ids)
	default:
		return httpx.BadRequest("action is assign_owner, add_tag, remove_tag, move_stage or delete.")
	}
	assigned := 0
	for _, id := range ids {
		fields := in
		if req.Action == "add_tag" || req.Action == "remove_tag" {
			var tags []uuid.UUID
			err := s.d.Pool.QueryRow(ctx, `SELECT tag_ids FROM `+o.table()+` WHERE id = $1 AND org_id = $2`, id, orgID).Scan(&tags)
			if db.IsNoRows(err) {
				res.Skipped = append(res.Skipped, BulkSkipped{ID: id, Reason: "not_found"})
				continue
			}
			if err != nil {
				return err
			}
			has := slices.Contains(tags, *req.TagID)
			if (req.Action == "add_tag") == has {
				res.Updated++ // already as asked
				continue
			}
			if req.Action == "add_tag" {
				tags = append(tags, *req.TagID)
			} else {
				tags = slices.DeleteFunc(tags, func(t uuid.UUID) bool { return t == *req.TagID })
			}
			b, _ := json.Marshal(tags)
			fields = map[string]json.RawMessage{"tags": b}
		}
		if _, err := s.updateWith(ctx, me(r), o, id, fields, true); err != nil {
			reason := "error"
			if e, ok := err.(*httpx.Error); ok {
				switch {
				case e.Status == http.StatusNotFound:
					reason = "not_found"
				case e.Status == http.StatusForbidden:
					reason = "no_edit_access"
				default:
					reason = e.Message
				}
			} else {
				return err
			}
			res.Skipped = append(res.Skipped, BulkSkipped{ID: id, Reason: reason})
			continue
		}
		res.Updated++
		assigned++
	}
	if req.Action == "assign_owner" && req.OwnerID != nil && *req.OwnerID != me(r) && assigned > 0 {
		s.notifyBulkAssigned(ctx, me(r), *req.OwnerID, o, assigned)
	}
	httpx.OK(w, res)
	return nil
}

func (s *Service) notifyBulkAssigned(ctx context.Context, actor, owner uuid.UUID, o objectType, n int) {
	var actorName string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, actor).Scan(&actorName)
	label := o.label()
	if n != 1 {
		label = o.labelPlural()
	}
	s.send(ctx, notify.Notification{UserID: owner, Kind: NotifyAssigned, Title: actorName + " assigned you " + strconv.Itoa(n) + " " + label,
		Link: "/crm/" + o.path() + "?owner=me", Data: map[string]any{"type": o, "count": n}})
}

func (s *Service) bulkDelete(w http.ResponseWriter, r *http.Request, orgID uuid.UUID, o objectType, ids []uuid.UUID) error {
	ctx := r.Context()
	res := BulkResult{Skipped: []BulkSkipped{}}
	var deleted []uuid.UUID
	var pipelines []uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		titles := []string{}
		for _, id := range ids {
			org, _, err := s.requireRecord(ctx, tx, me(r), o, id, access.Edit)
			if err != nil || org != orgID {
				reason := "not_found"
				if e, ok := err.(*httpx.Error); ok && e.Status == http.StatusForbidden {
					reason = "no_edit_access"
				} else if err != nil && !ok {
					return err
				}
				res.Skipped = append(res.Skipped, BulkSkipped{ID: id, Reason: reason})
				continue
			}
			t, _ := recordTitle(ctx, tx, o, id)
			if o == objDeal {
				var p uuid.UUID
				if err := tx.QueryRow(ctx, `SELECT pipeline_id FROM crm_deals WHERE id = $1`, id).Scan(&p); err == nil {
					pipelines = append(pipelines, p)
				}
			}
			if err := deleteRecordTx(ctx, tx, o, id); err != nil {
				return err
			}
			titles = append(titles, t)
			deleted = append(deleted, id)
		}
		if len(deleted) == 0 {
			return nil
		}
		return audit.Log(ctx, tx, me(r), "crm.record.bulk_delete", "org", orgID.String(), map[string]any{
			"object": o, "count": len(deleted), "ids": deleted, "titles": titles,
		})
	})
	if err != nil {
		return err
	}
	res.Updated = len(deleted)
	s.afterDelete(ctx, o, deleted, pipelines...)
	httpx.OK(w, res)
	return nil
}

// ---- merge ------------------------------------------------------------------------------

type mergeReq struct {
	ObjectType  string    `json:"object_type"`
	PrimaryID   uuid.UUID `json:"primary_id"`
	SecondaryID uuid.UUID `json:"secondary_id"`
}

// merge: POST /v1/crm/orgs/{orgID}/merge {object_type, primary_id,
// secondary_id} (crm.delete + edit on both). The primary keeps its values and
// takes the secondary's where it has none; associations, activities and the
// timeline move over; the secondary is deleted (its own sharing goes with it).
func (s *Service) merge(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req mergeReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	o, ok := parseObject(req.ObjectType)
	if !ok {
		return httpx.BadRequest("object_type is company, contact or deal.")
	}
	if req.PrimaryID == uuid.Nil || req.SecondaryID == uuid.Nil || req.PrimaryID == req.SecondaryID {
		return httpx.BadRequest("Choose two different records to merge.")
	}
	userID := me(r)
	var pipelines []uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, id := range []uuid.UUID{req.PrimaryID, req.SecondaryID} {
			if _, err := tx.Exec(ctx, `SELECT 1 FROM `+o.table()+` WHERE id = $1 FOR UPDATE`, id); err != nil {
				return err
			}
			org, _, err := s.requireRecord(ctx, tx, userID, o, id, access.Edit)
			if err != nil {
				return err
			}
			if org != orgID {
				return httpx.NotFound(o.titleCase() + " not found.")
			}
		}
		primary, err := s.getOne(ctx, tx, userID, o, req.PrimaryID)
		if err != nil {
			return err
		}
		secondary, err := s.getOne(ctx, tx, userID, o, req.SecondaryID)
		if err != nil {
			return err
		}
		cols := mergedColumns(primary, secondary)
		if err := moveAssociations(ctx, tx, o, req.PrimaryID, req.SecondaryID); err != nil {
			return err
		}
		if err := moveTimeline(ctx, tx, o, req.PrimaryID, req.SecondaryID); err != nil {
			return err
		}
		if len(cols) > 0 {
			if err := updateColumns(ctx, tx, o, req.PrimaryID, cols); err != nil {
				return err
			}
		}
		if d, ok := secondary.(*Deal); ok {
			pipelines = append(pipelines, d.PipelineID, primary.(*Deal).PipelineID)
		}
		secondaryTitle := secondary.base().Title
		if err := deleteRecordTx(ctx, tx, o, req.SecondaryID); err != nil {
			return err
		}
		if err := addEvent(ctx, tx, orgID, o, req.PrimaryID, "merged", userID, map[string]any{"merged_name": secondaryTitle}); err != nil {
			return err
		}
		return audit.Log(ctx, tx, userID, "crm.record.merge", o.kind(), req.PrimaryID.String(), map[string]any{
			"org_id": orgID, "merged_id": req.SecondaryID, "merged_name": secondaryTitle, "kept_name": primary.base().Title,
		})
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, o, req.PrimaryID, true)
	s.afterDelete(ctx, o, []uuid.UUID{req.SecondaryID}, pipelines...)
	d, err := s.recordDetail(ctx, userID, o, req.PrimaryID)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// mergedColumns fills the primary's empty fields from the secondary.
func mergedColumns(primary, secondary recordish) map[string]any {
	cols := map[string]any{}
	fillText := func(col, p, s string) {
		if strings.TrimSpace(p) == "" && strings.TrimSpace(s) != "" {
			cols[col] = s
		}
	}
	pb, sb := primary.base(), secondary.base()
	props := map[string]any{}
	for k, v := range pb.Props {
		props[k] = v
	}
	changedProps := false
	for k, v := range sb.Props {
		if _, ok := props[k]; !ok {
			props[k], changedProps = v, true
		}
	}
	if changedProps {
		cols["props"] = props
	}
	tags := append([]uuid.UUID{}, pb.tagIDs...)
	for _, t := range sb.tagIDs {
		if !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	if len(tags) != len(pb.tagIDs) {
		cols["tag_ids"] = tags
	}
	switch p := primary.(type) {
	case *Company:
		s := secondary.(*Company)
		if p.Domain == nil && s.Domain != nil {
			cols["domain"] = *s.Domain
		}
		if p.Employees == nil && s.Employees != nil {
			cols["employees"] = *s.Employees
		}
		fillText("industry", p.Industry, s.Industry)
		fillText("phone", p.Phone, s.Phone)
		fillText("city", p.City, s.City)
		fillText("country", p.Country, s.Country)
		fillText("description", p.Description, s.Description)
	case *Contact:
		s := secondary.(*Contact)
		if strings.TrimSpace(p.FirstName+p.LastName) == "" {
			fillText("first_name", p.FirstName, s.FirstName)
			fillText("last_name", p.LastName, s.LastName)
		}
		emails := append([]string{}, p.Emails...)
		for _, e := range s.Emails {
			if !slices.Contains(emails, e) && len(emails) < 10 {
				emails = append(emails, e)
			}
		}
		if len(emails) != len(p.Emails) {
			cols["emails"] = emails
		}
		phones := append([]string{}, p.Phones...)
		for _, ph := range s.Phones {
			if !slices.Contains(phones, ph) && len(phones) < 10 {
				phones = append(phones, ph)
			}
		}
		if len(phones) != len(p.Phones) {
			cols["phones"] = phones
		}
		fillText("job_title", p.JobTitle, s.JobTitle)
		fillText("city", p.City, s.City)
		fillText("country", p.Country, s.Country)
	case *Deal:
		s := secondary.(*Deal)
		if p.Amount == nil && s.Amount != nil {
			cols["amount"], cols["currency"] = *s.Amount, s.Currency
		}
		if p.CloseDate == nil && s.CloseDate != nil {
			cols["close_date"] = *s.CloseDate
		}
	}
	return cols
}

func moveAssociations(ctx context.Context, tx pgx.Tx, o objectType, keep, drop uuid.UUID) error {
	var stmts []string
	switch o {
	case objCompany:
		// Contacts whose primary company was the dropped one get the kept one as primary.
		var hadPrimary []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(contact_id), '{}') FROM crm_contact_companies WHERE company_id = $1 AND is_primary`, drop).Scan(&hadPrimary); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO crm_contact_companies (contact_id, company_id, org_id, is_primary, created_by, created_at)
			 SELECT contact_id, $1::uuid, org_id, false, created_by, created_at FROM crm_contact_companies WHERE company_id = $2::uuid
			 ON CONFLICT DO NOTHING`,
			`DELETE FROM crm_contact_companies WHERE company_id = $2`,
			`UPDATE crm_deals SET company_id = $1, updated_at = now() WHERE company_id = $2`,
		} {
			if _, err := tx.Exec(ctx, q, keep, drop); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE crm_contact_companies y SET is_primary = true WHERE y.company_id = $1 AND y.contact_id = ANY ($2)
			AND NOT EXISTS (SELECT 1 FROM crm_contact_companies z WHERE z.contact_id = y.contact_id AND z.is_primary)`, keep, hadPrimary)
		return err
	case objContact:
		stmts = []string{
			`INSERT INTO crm_contact_companies (contact_id, company_id, org_id, is_primary, created_by, created_at)
			 SELECT $1::uuid, company_id, org_id, false, created_by, created_at FROM crm_contact_companies WHERE contact_id = $2::uuid
			 ON CONFLICT DO NOTHING`,
			`UPDATE crm_contact_companies SET is_primary = true WHERE contact_id = $1 AND company_id =
			 (SELECT company_id FROM crm_contact_companies WHERE contact_id = $1 AND company_id <> $2 ORDER BY created_at, company_id LIMIT 1)
			 AND NOT EXISTS (SELECT 1 FROM crm_contact_companies WHERE contact_id = $1 AND is_primary)`,
			`INSERT INTO crm_deal_contacts (deal_id, contact_id, org_id, created_by, created_at)
			 SELECT deal_id, $1::uuid, org_id, created_by, created_at FROM crm_deal_contacts WHERE contact_id = $2::uuid ON CONFLICT DO NOTHING`,
		}
	case objDeal:
		stmts = []string{
			`INSERT INTO crm_deal_contacts (deal_id, contact_id, org_id, created_by, created_at)
			 SELECT $1::uuid, contact_id, org_id, created_by, created_at FROM crm_deal_contacts WHERE deal_id = $2::uuid ON CONFLICT DO NOTHING`,
			`UPDATE crm_deals d SET company_id = s.company_id FROM crm_deals s WHERE d.id = $1 AND s.id = $2 AND d.company_id IS NULL`,
		}
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q, keep, drop); err != nil {
			return err
		}
	}
	return nil
}

func moveTimeline(ctx context.Context, tx pgx.Tx, o objectType, keep, drop uuid.UUID) error {
	ot := string(o)
	stmts := []struct {
		sql  string
		args []any
	}{
		// The merged record's own "created" entry would read as a second creation.
		{`DELETE FROM crm_events WHERE record_type = $1 AND record_id = $2 AND kind IN ('created', 'imported')`, []any{ot, drop}},
		{`INSERT INTO crm_activity_records (activity_id, record_type, record_id, org_id, created_at)
		  SELECT activity_id, record_type, $2, org_id, created_at FROM crm_activity_records WHERE record_type = $1 AND record_id = $3
		  ON CONFLICT DO NOTHING`, []any{ot, keep, drop}},
		{`DELETE FROM crm_activity_records WHERE record_type = $1 AND record_id = $2`, []any{ot, drop}},
		{`UPDATE crm_activities SET record_id = $2, updated_at = now() WHERE record_type = $1 AND record_id = $3`, []any{ot, keep, drop}},
		{`UPDATE crm_events SET record_id = $2 WHERE record_type = $1 AND record_id = $3`, []any{ot, keep, drop}},
		{`UPDATE ` + o.table() + ` p SET last_activity_at = GREATEST(p.last_activity_at, s.last_activity_at)
		  FROM ` + o.table() + ` s WHERE p.id = $1 AND s.id = $2`, []any{keep, drop}},
	}
	for _, st := range stmts {
		if _, err := tx.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return nil
}
