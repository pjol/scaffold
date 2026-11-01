package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// This file is the one visible-records query. Lists, totals, the board, the
// dashboard, exports, search and agent tools all start from visibleFrom, which
// joins access_visible(user, kind, via_agent): a row exists only when access
// lets the caller open the record (agent scope included), so no count or sum
// can include a record the caller can't see.

// sqlb collects positional arguments. $1 is always the caller, $2 whether an
// AI agent is acting for them.
type sqlb struct{ args []any }

func newSQL(ctx context.Context, userID uuid.UUID) *sqlb {
	return &sqlb{args: []any{userID, access.ViaAgent(ctx)}}
}

func (b *sqlb) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// visibleFrom is FROM … for records of o the caller can open, with the access
// row (owner), owner user, team and, for deals, stage and pipeline. v.level is
// the caller's level.
func visibleFrom(o objectType) string {
	return `FROM ` + o.table() + ` r
		JOIN access_visible($1, '` + o.kind() + `', $2) v ON v.id = r.id
		` + recordJoins(o)
}

// oneFrom is the same for a single record by id (cheaper than access_visible).
func oneFrom(o objectType) string {
	return `FROM ` + o.table() + ` r
		CROSS JOIN LATERAL (SELECT access_level($1, '` + o.kind() + `', r.id, $2) AS level) v
		` + recordJoins(o)
}

func recordJoins(o objectType) string {
	j := `JOIN access_resources ar ON ar.kind = '` + o.kind() + `' AND ar.id = r.id
		LEFT JOIN users ou ON ou.id = ar.owner_id AND ou.status <> 'deleted'
		LEFT JOIN access_teams t ON t.id = r.team_id`
	if o == objDeal {
		j += `
		JOIN crm_stages st ON st.id = r.stage_id
		JOIN crm_pipelines pl ON pl.id = r.pipeline_id`
	}
	return j
}

// detailJoins adds the associations a row shows, only when the caller can
// view them.
func detailJoins(o objectType) string {
	switch o {
	case objContact:
		return `
		LEFT JOIN LATERAL (SELECT co.id, co.name FROM crm_contact_companies cc JOIN crm_companies co ON co.id = cc.company_id
			WHERE cc.contact_id = r.id AND cc.is_primary AND access_level($1, 'crm.company', co.id, $2) >= 1) pc ON true`
	case objDeal:
		return `
		LEFT JOIN LATERAL (SELECT co.id, co.name FROM crm_companies co
			WHERE co.id = r.company_id AND access_level($1, 'crm.company', co.id, $2) >= 1) dc ON true`
	}
	return ""
}

const commonCols = `r.team_id, t.name, t.handle, r.tag_ids, r.props, r.created_by, r.created_at, r.updated_at, r.last_activity_at,
	ar.owner_id, ou.display_name, ou.avatar_url, v.level`

func selectCols(o objectType) string {
	switch o {
	case objCompany:
		return `r.id, r.org_id, r.name, r.domain, r.industry, r.employees, r.phone, r.city, r.country, r.description, ` + commonCols
	case objContact:
		return `r.id, r.org_id, r.first_name, r.last_name, r.emails, r.phones, r.job_title, r.lifecycle_stage, r.city, r.country,
			pc.id, pc.name, ` + commonCols
	default:
		return `r.id, r.org_id, r.name, r.amount, r.currency, r.pipeline_id, pl.name, st.id, st.name, st.probability, st.outcome, st.position,
			to_char(r.close_date, 'YYYY-MM-DD'), dc.id, dc.name, r.stage_entered_at, r.closed_at, ` + commonCols
	}
}

// scanRecord reads one row of selectCols(o) (plus extra destinations).
func scanRecord(o objectType, row pgx.Row, extra ...any) (recordish, error) {
	var (
		rec      Record
		teamID   *uuid.UUID
		teamName *string
		handle   *string
		props    []byte
		ownerID  *uuid.UUID
		owner    *string
		avatar   *string
		level    int
	)
	common := []any{&teamID, &teamName, &handle, &rec.tagIDs, &props, &rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt, &rec.LastActivityAt,
		&ownerID, &owner, &avatar, &level}
	var out recordish
	var dest []any
	switch o {
	case objCompany:
		c := &Company{}
		dest = []any{&rec.ID, &rec.OrgID, &c.Name, &c.Domain, &c.Industry, &c.Employees, &c.Phone, &c.City, &c.Country, &c.Description}
		out = c
	case objContact:
		c := &Contact{}
		var coID *uuid.UUID
		var coName *string
		dest = []any{&rec.ID, &rec.OrgID, &c.FirstName, &c.LastName, &c.Emails, &c.Phones, &c.JobTitle, &c.LifecycleStage, &c.City, &c.Country, &coID, &coName}
		out = c
		defer func() {
			if coID != nil && coName != nil {
				c.Company = &RecordRef{Type: objCompany, ID: *coID, Name: *coName}
			}
		}()
	default:
		d := &Deal{}
		var coID *uuid.UUID
		var coName *string
		dest = []any{&rec.ID, &rec.OrgID, &d.Name, &d.Amount, &d.Currency, &d.PipelineID, &d.PipelineName,
			&d.Stage.ID, &d.Stage.Name, &d.Stage.Probability, &d.Stage.Outcome, &d.Stage.Position,
			&d.CloseDate, &coID, &coName, &d.StageEnteredAt, &d.ClosedAt}
		out = d
		defer func() {
			if coID != nil && coName != nil {
				d.Company = &RecordRef{Type: objCompany, ID: *coID, Name: *coName}
			}
		}()
	}
	dest = append(append(dest, common...), extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	rec.Type, rec.MyLevel = o, access.Level(level)
	if teamID != nil && teamName != nil {
		rec.Team = &TeamRef{ID: *teamID, Name: *teamName, Handle: deref(handle)}
	}
	if ownerID != nil && owner != nil {
		rec.Owner = &UserRef{ID: *ownerID, Name: *owner, AvatarURL: avatar}
	}
	rec.Props = map[string]any{}
	_ = json.Unmarshal(props, &rec.Props)
	rec.Tags, rec.PropsDisplay = []Tag{}, map[string]string{}
	switch v := out.(type) {
	case *Company:
		v.Record = rec
		v.Record.Title = v.Name
	case *Contact:
		v.Record = rec
		if v.Emails == nil {
			v.Emails = []string{}
		}
		if v.Phones == nil {
			v.Phones = []string{}
		}
		v.Name = contactName(v.FirstName, v.LastName, v.Emails)
		v.Record.Title = v.Name
	case *Deal:
		v.Record = rec
		v.Record.Title = v.Name
		if v.Amount != nil {
			w := *v.Amount * int64(v.Stage.Probability) / 100
			v.Weighted = &w
		}
	}
	return out, nil
}

func contactName(first, last string, emails []string) string {
	n := strings.TrimSpace(first + " " + last)
	if n == "" && len(emails) > 0 {
		return emails[0]
	}
	if n == "" {
		return "Unnamed contact"
	}
	return n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// hydrate fills tags and display text for person-valued custom properties.
func (s *Service) hydrate(ctx context.Context, q db.Querier, props []Property, recs []recordish) error {
	tagSet := map[uuid.UUID]bool{}
	userSet := map[string]bool{}
	var userKeys []string
	for _, p := range props {
		if !p.Builtin && p.Type == "user" {
			userKeys = append(userKeys, p.Key)
		}
	}
	for _, r := range recs {
		b := r.base()
		for _, t := range b.tagIDs {
			tagSet[t] = true
		}
		for _, k := range userKeys {
			if v, ok := b.Props[k].(string); ok {
				userSet[v] = true
			}
		}
	}
	tags := map[uuid.UUID]Tag{}
	if len(tagSet) > 0 {
		ids := make([]uuid.UUID, 0, len(tagSet))
		for id := range tagSet {
			ids = append(ids, id)
		}
		rows, err := q.Query(ctx, `SELECT id, name, tone FROM crm_tags WHERE id = ANY ($1)`, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var t Tag
			if err := rows.Scan(&t.ID, &t.Name, &t.Tone); err != nil {
				rows.Close()
				return err
			}
			tags[t.ID] = t
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	people, err := s.peopleNames(ctx, q, userSet)
	if err != nil {
		return err
	}
	for _, r := range recs {
		b := r.base()
		for _, id := range b.tagIDs {
			if t, ok := tags[id]; ok {
				b.Tags = append(b.Tags, t)
			}
		}
		for _, k := range userKeys {
			if v, ok := b.Props[k].(string); ok {
				if n, ok := people[v]; ok {
					b.PropsDisplay[k] = n
				} else {
					b.PropsDisplay[k] = "Former member"
				}
			}
		}
	}
	return nil
}

func (s *Service) peopleNames(ctx context.Context, q db.Querier, set map[string]bool) (map[string]string, error) {
	out := map[string]string{}
	if len(set) == 0 {
		return out, nil
	}
	ids := []uuid.UUID{}
	for k := range set {
		if id, err := uuid.Parse(k); err == nil {
			ids = append(ids, id)
		}
	}
	rows, err := q.Query(ctx, `SELECT id, display_name FROM users WHERE id = ANY ($1) AND status <> 'deleted'`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id.String()] = n
	}
	return out, rows.Err()
}

// ---- list specs ----------------------------------------------------------------

// Filter is one condition of a list (all conditions must hold).
type Filter struct {
	Key   string          `json:"key"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}

// listSpec is a list request.
type listSpec struct {
	Filters []Filter
	Q       string
	SortKey string
	SortDir string
	Cursor  string
	Limit   int
	TZ      string
	// where adds SQL fragments built with the same sqlb (board columns).
	where []func(b *sqlb) string
}

const maxFilters = 25

// parseListSpec reads ?filters=<json>&q=&sort=&dir=&cursor=&limit=&tz= .
func parseListSpec(get func(string) string) (listSpec, error) {
	spec := listSpec{Q: strings.TrimSpace(get("q")), SortKey: get("sort"), SortDir: get("dir"), Cursor: get("cursor"), TZ: get("tz")}
	if f := get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &spec.Filters); err != nil {
			return spec, httpx.BadRequest("filters must be a JSON list of {key, op, value}.")
		}
	}
	spec.Limit = 50
	if v, err := strconv.Atoi(get("limit")); err == nil && v > 0 {
		spec.Limit = min(v, 100)
	}
	return spec, nil
}

type cursorVal struct {
	V  *string   `json:"v"`
	ID uuid.UUID `json:"id"`
}

// buildWhere returns the WHERE clause (filters, search, extra) for o in org.
func buildWhere(b *sqlb, o objectType, props []Property, orgID uuid.UUID, spec listSpec, userID uuid.UUID) (string, error) {
	conds := []string{"r.org_id = " + b.arg(orgID)}
	if len(spec.Filters) > maxFilters {
		return "", httpx.BadRequest(fmt.Sprintf("Use at most %d filters.", maxFilters))
	}
	tz, err := location(spec.TZ)
	if err != nil {
		return "", err
	}
	for _, f := range spec.Filters {
		p, ok := findProp(props, f.Key)
		if !ok || !p.Filterable {
			return "", httpx.BadRequest("You can't filter on \"" + f.Key + "\".")
		}
		c, err := filterSQL(b, o, p, f, userID, tz)
		if err != nil {
			return "", err
		}
		conds = append(conds, c)
	}
	if spec.Q != "" {
		pat := b.arg(likePattern(clip(spec.Q, 200)))
		switch o {
		case objCompany:
			conds = append(conds, "(r.name ILIKE "+pat+" OR COALESCE(r.domain, '') ILIKE "+pat+")")
		case objContact:
			conds = append(conds, "((r.first_name || ' ' || r.last_name) ILIKE "+pat+" OR array_to_string(r.emails, ' ') ILIKE "+pat+
				" OR array_to_string(r.phones, ' ') ILIKE "+pat+")")
		default:
			conds = append(conds, "r.name ILIKE "+pat)
		}
	}
	for _, w := range spec.where {
		conds = append(conds, w(b))
	}
	return "WHERE " + strings.Join(conds, " AND "), nil
}

func location(tz string) (string, error) {
	if tz == "" {
		return "UTC", nil
	}
	if _, err := time.LoadLocation(tz); err != nil || len(tz) > 64 {
		return "", httpx.BadRequest("tz must be an IANA time zone like Europe/Berlin.")
	}
	return tz, nil
}

func filterErr(p Property, msg string) error {
	return httpx.BadRequest("Filter on " + p.Label + ": " + msg)
}

// filterSQL turns one filter into a condition. Values are always parameters.
func filterSQL(b *sqlb, o objectType, p Property, f Filter, userID uuid.UUID, tz string) (string, error) {
	e := p.expr
	if strings.Contains(e, "{K}") {
		e = strings.ReplaceAll(e, "{K}", b.arg(p.Key)+"::text")
	}
	str := func() (string, error) {
		var v string
		if err := json.Unmarshal(f.Value, &v); err != nil {
			return "", filterErr(p, "the value must be text.")
		}
		return v, nil
	}
	list := func() ([]string, error) {
		var v []string
		if err := json.Unmarshal(f.Value, &v); err != nil || len(v) == 0 || len(v) > 200 {
			return nil, filterErr(p, "choose at least one value.")
		}
		return v, nil
	}
	uuids := func() ([]uuid.UUID, error) {
		vals, err := list()
		if err != nil {
			return nil, err
		}
		out := make([]uuid.UUID, 0, len(vals))
		for _, s := range vals {
			if s == "me" {
				out = append(out, userID)
				continue
			}
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, filterErr(p, "the values must be ids.")
			}
			out = append(out, id)
		}
		return out, nil
	}
	bad := func() (string, error) { return "", filterErr(p, "\""+f.Op+"\" isn't a condition for this field.") }

	switch p.Type {
	case "text", "long_text", "url", "email", "phone", "domain", "currency_code":
		t := "COALESCE(" + e + ", '')"
		switch f.Op {
		case "empty":
			return t + " = ''", nil
		case "not_empty":
			return t + " <> ''", nil
		}
		v, err := str()
		if err != nil {
			return "", err
		}
		switch f.Op {
		case "eq":
			return "lower(" + t + ") = lower(" + b.arg(v) + ")", nil
		case "neq":
			return "lower(" + t + ") <> lower(" + b.arg(v) + ")", nil
		case "contains":
			return t + " ILIKE " + b.arg(likePattern(v)), nil
		case "not_contains":
			return t + " NOT ILIKE " + b.arg(likePattern(v)), nil
		}
		return bad()
	case "emails", "phones":
		switch f.Op {
		case "empty":
			return "cardinality(" + e + ") = 0", nil
		case "not_empty":
			return "cardinality(" + e + ") > 0", nil
		}
		v, err := str()
		if err != nil {
			return "", err
		}
		switch f.Op {
		case "eq":
			if p.Type == "emails" {
				v = strings.ToLower(strings.TrimSpace(v))
			}
			return b.arg(v) + " = ANY (" + e + ")", nil
		case "contains":
			return "array_to_string(" + e + ", ' ') ILIKE " + b.arg(likePattern(v)), nil
		}
		return bad()
	case "number", "currency":
		switch f.Op {
		case "empty":
			return e + " IS NULL", nil
		case "not_empty":
			return e + " IS NOT NULL", nil
		}
		var n json.Number
		dec := json.NewDecoder(strings.NewReader(string(f.Value)))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return "", filterErr(p, "the value must be a number.")
		}
		if _, err := n.Float64(); err != nil {
			return "", filterErr(p, "the value must be a number.")
		}
		ops := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		op, ok := ops[f.Op]
		if !ok {
			return bad()
		}
		return "(" + e + ")::numeric " + op + " " + b.arg(n.String()) + "::numeric", nil
	case "date", "datetime":
		switch f.Op {
		case "empty":
			return e + " IS NULL", nil
		case "not_empty":
			return e + " IS NOT NULL", nil
		}
		zone := ""
		z := func() string { // the time zone parameter, added only when used
			if zone == "" {
				zone = b.arg(tz) + "::text"
			}
			return zone
		}
		d := "(" + e + ")::date"
		if p.Type == "datetime" {
			d = "((" + e + ") AT TIME ZONE " + z() + ")::date"
		}
		today := func() string { return "(now() AT TIME ZONE " + z() + ")::date" }
		switch f.Op {
		case "on", "before", "after":
			v, err := str()
			if err != nil || !validDate(v) {
				return "", filterErr(p, "the value must be a date like 2026-11-30.")
			}
			op := map[string]string{"on": "=", "before": "<", "after": ">"}[f.Op]
			return d + " " + op + " " + b.arg(v) + "::date", nil
		case "last_days", "next_days":
			var n int
			if err := json.Unmarshal(f.Value, &n); err != nil || n < 0 || n > 3650 {
				return "", filterErr(p, "the value must be a number of days.")
			}
			na := b.arg(n)
			if f.Op == "last_days" {
				return d + " BETWEEN " + today() + " - " + na + "::int AND " + today(), nil
			}
			return d + " BETWEEN " + today() + " AND " + today() + " + " + na + "::int", nil
		}
		return bad()
	case "select", "user":
		t := "(" + e + ")::text"
		switch f.Op {
		case "empty":
			return e + " IS NULL", nil
		case "not_empty":
			return e + " IS NOT NULL", nil
		}
		vals, err := list()
		if err != nil {
			return "", err
		}
		if p.Type == "user" {
			for i, v := range vals {
				if v == "me" {
					vals[i] = userID.String()
				}
			}
		}
		switch f.Op {
		case "is_any":
			return t + " = ANY (" + b.arg(vals) + "::text[])", nil
		case "is_none":
			return "(" + e + " IS NULL OR NOT (" + t + " = ANY (" + b.arg(vals) + "::text[])))", nil
		}
		return bad()
	case "team", "pipeline", "stage":
		switch f.Op {
		case "empty":
			return e + " IS NULL", nil
		case "not_empty":
			return e + " IS NOT NULL", nil
		}
		ids, err := uuids()
		if err != nil {
			return "", err
		}
		switch f.Op {
		case "is_any":
			return e + " = ANY (" + b.arg(ids) + "::uuid[])", nil
		case "is_none":
			return "(" + e + " IS NULL OR NOT (" + e + " = ANY (" + b.arg(ids) + "::uuid[])))", nil
		}
		return bad()
	case "company":
		assoc := "r.company_id"
		if o == objContact {
			switch f.Op {
			case "empty":
				return "NOT EXISTS (SELECT 1 FROM crm_contact_companies cc WHERE cc.contact_id = r.id)", nil
			case "not_empty":
				return "EXISTS (SELECT 1 FROM crm_contact_companies cc WHERE cc.contact_id = r.id)", nil
			}
			ids, err := uuids()
			if err != nil {
				return "", err
			}
			in := "EXISTS (SELECT 1 FROM crm_contact_companies cc WHERE cc.contact_id = r.id AND cc.company_id = ANY (" + b.arg(ids) + "::uuid[]))"
			switch f.Op {
			case "is_any":
				return in, nil
			case "is_none":
				return "NOT " + in, nil
			}
			return bad()
		}
		switch f.Op {
		case "empty":
			return assoc + " IS NULL", nil
		case "not_empty":
			return assoc + " IS NOT NULL", nil
		}
		ids, err := uuids()
		if err != nil {
			return "", err
		}
		switch f.Op {
		case "is_any":
			return assoc + " = ANY (" + b.arg(ids) + "::uuid[])", nil
		case "is_none":
			return "(" + assoc + " IS NULL OR NOT (" + assoc + " = ANY (" + b.arg(ids) + "::uuid[])))", nil
		}
		return bad()
	case "tags":
		switch f.Op {
		case "empty":
			return "cardinality(r.tag_ids) = 0", nil
		case "not_empty":
			return "cardinality(r.tag_ids) > 0", nil
		}
		ids, err := uuids()
		if err != nil {
			return "", err
		}
		a := b.arg(ids) + "::uuid[]"
		switch f.Op {
		case "has_any":
			return "r.tag_ids && " + a, nil
		case "has_all":
			return "r.tag_ids @> " + a, nil
		case "has_none":
			return "NOT (r.tag_ids && " + a + ")", nil
		}
		return bad()
	case "multi_select":
		j := "COALESCE(" + e + ", '[]'::jsonb)"
		switch f.Op {
		case "empty":
			return "jsonb_array_length(" + j + ") = 0", nil
		case "not_empty":
			return "jsonb_array_length(" + j + ") > 0", nil
		}
		vals, err := list()
		if err != nil {
			return "", err
		}
		a := b.arg(vals) + "::text[]"
		switch f.Op {
		case "has_any":
			return j + " ?| " + a, nil
		case "has_all":
			return j + " ?& " + a, nil
		case "has_none":
			return "NOT (" + j + " ?| " + a + ")", nil
		}
		return bad()
	case "checkbox":
		switch f.Op {
		case "is_true":
			return e + " = true", nil
		case "is_false":
			return e + " = false", nil
		}
		return bad()
	}
	return bad()
}

// orderBy returns the sort expression, its type, the ORDER BY clause and the
// keyset condition for a cursor (empty when there is none).
func orderBy(b *sqlb, props []Property, spec listSpec) (sortExpr, orderSQL, keyset string, err error) {
	key, dir := spec.SortKey, strings.ToLower(spec.SortDir)
	if key == "" {
		key = "created_at"
	}
	if dir == "" {
		dir = "desc"
		if key == "name" || key == "first_name" || key == "last_name" {
			dir = "asc"
		}
	}
	if dir != "asc" && dir != "desc" {
		return "", "", "", httpx.BadRequest("dir must be asc or desc.")
	}
	p, ok := findProp(props, key)
	if !ok || !p.Sortable {
		return "", "", "", httpx.BadRequest("You can't sort by \"" + key + "\".")
	}
	sortExpr = p.sortExpr
	if strings.Contains(sortExpr, "{K}") {
		sortExpr = strings.ReplaceAll(sortExpr, "{K}", b.arg(p.Key)+"::text")
	}
	cmp := "<"
	if dir == "asc" {
		cmp = ">"
	}
	orderSQL = fmt.Sprintf("ORDER BY (%s) %s NULLS LAST, r.id %s", sortExpr, dir, dir)
	if spec.Cursor == "" {
		return sortExpr, orderSQL, "", nil
	}
	var c cursorVal
	if err := httpx.DecodeCursor(spec.Cursor, &c); err != nil {
		return "", "", "", err
	}
	id := b.arg(c.ID)
	if c.V == nil {
		keyset = fmt.Sprintf("((%s) IS NULL AND r.id %s %s)", sortExpr, cmp, id)
	} else {
		v := b.arg(*c.V) + "::" + p.sortType
		keyset = fmt.Sprintf("((%[1]s) %[2]s %[3]s OR ((%[1]s) = %[3]s AND r.id %[2]s %[4]s) OR (%[1]s) IS NULL)", sortExpr, cmp, v, id)
	}
	return sortExpr, orderSQL, keyset, nil
}

// listPage is a keyset page with, on the first page, the number of matches.
type listPage struct {
	Items      []recordish `json:"items"`
	NextCursor *string     `json:"next_cursor"`
	Total      *int        `json:"total"`
}

// queryRecords runs a list: visible rows of o in org matching spec. total is
// counted on the first page when withTotal is set.
func (s *Service) queryRecords(ctx context.Context, q db.Querier, userID, orgID uuid.UUID, o objectType, props []Property, spec listSpec, withTotal bool) (*listPage, error) {
	b := newSQL(ctx, userID)
	where, err := buildWhere(b, o, props, orgID, spec, userID)
	if err != nil {
		return nil, err
	}
	countSQL, countArgs := `SELECT count(*) `+visibleFrom(o)+` `+where, append([]any(nil), b.args...)
	sortExpr, order, keyset, err := orderBy(b, props, spec)
	if err != nil {
		return nil, err
	}
	if keyset != "" {
		where += " AND " + keyset
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = 50
	}
	sql := `SELECT ` + selectCols(o) + `, (` + sortExpr + `)::text ` + visibleFrom(o) + detailJoins(o) + ` ` + where + ` ` + order + ` LIMIT ` + b.arg(limit+1)
	rows, err := q.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	var (
		items []recordish
		keys  []*string
	)
	for rows.Next() {
		var sv *string
		rec, err := scanRecord(o, rows, &sv)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, rec)
		keys = append(keys, sv)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page := &listPage{Items: []recordish{}}
	if len(items) > limit {
		items, keys = items[:limit], keys[:limit]
		last := items[limit-1].base()
		page.NextCursor = httpx.EncodeCursor(cursorVal{V: keys[limit-1], ID: last.ID})
	}
	page.Items = append(page.Items, items...)
	if err := s.hydrate(ctx, q, props, page.Items); err != nil {
		return nil, err
	}
	if withTotal && spec.Cursor == "" {
		var n int
		if err := q.QueryRow(ctx, countSQL, countArgs...).Scan(&n); err != nil {
			return nil, err
		}
		page.Total = &n
	}
	return page, nil
}

// getOne loads one record the caller can view (404 otherwise).
func (s *Service) getOne(ctx context.Context, q db.Querier, userID uuid.UUID, o objectType, id uuid.UUID) (recordish, error) {
	b := newSQL(ctx, userID)
	row := q.QueryRow(ctx, `SELECT `+selectCols(o)+` `+oneFrom(o)+detailJoins(o)+` WHERE r.id = `+b.arg(id)+` AND v.level >= 1`, b.args...)
	rec, err := scanRecord(o, row)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound(o.titleCase() + " not found.")
	}
	if err != nil {
		return nil, err
	}
	props, err := s.loadProperties(ctx, q, rec.base().OrgID, o)
	if err != nil {
		return nil, err
	}
	if err := s.hydrate(ctx, q, props, []recordish{rec}); err != nil {
		return nil, err
	}
	return rec, nil
}

// visibleRefs returns which of ids (one type) the caller can view, by name.
func (s *Service) visibleRefs(ctx context.Context, q db.Querier, userID uuid.UUID, o objectType, ids []uuid.UUID) (map[uuid.UUID]RecordRef, error) {
	out := map[uuid.UUID]RecordRef{}
	if len(ids) == 0 {
		return out, nil
	}
	b := newSQL(ctx, userID)
	rows, err := q.Query(ctx, `SELECT r.id, `+o.titleExpr()+` FROM `+o.table()+` r
		WHERE r.id = ANY (`+b.arg(ids)+`) AND access_level($1, '`+o.kind()+`', r.id, $2) >= 1`, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		ref := RecordRef{Type: o}
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		out[ref.ID] = ref
	}
	return out, rows.Err()
}
