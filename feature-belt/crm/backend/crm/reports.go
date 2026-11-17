package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Reports: count, sum or average of a property over the records the viewer
// can open, grouped by a property or a date bucket, with the list filters.
// Every number starts from visibleFrom (access_visible, agent-capped), so a
// shared dashboard shows each viewer the totals of what they can see.
// Dashboards are access resources (crm.dashboard) shared through access.

// ReportSpec defines one report.
type ReportSpec struct {
	Object   string   `json:"object"`   // company | contact | deal | ticket
	Measure  string   `json:"measure"`  // count | sum | avg
	Property string   `json:"property"` // the number summed or averaged
	GroupBy  string   `json:"group_by"` // a property key ("" = one total)
	Bucket   string   `json:"bucket"`   // date group_by: day | week | month | quarter | year
	Filters  []Filter `json:"filters"`
	Chart    string   `json:"chart"`    // bar | line | donut | table
	Currency string   `json:"currency"` // money sums: the currency (default the org's)
}

// ReportRow is one group.
type ReportRow struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Count int     `json:"count"`
}

// ReportResult is a report computed for the caller.
type ReportResult struct {
	Rows      []ReportRow `json:"rows"`
	Total     float64     `json:"total"`
	Count     int         `json:"count"`
	ValueType string      `json:"value_type"` // count | money | number
	Currency  string      `json:"currency,omitempty"`
	Truncated bool        `json:"truncated"` // more groups than shown
	Spec      ReportSpec  `json:"spec"`
}

const (
	maxReportGroups = 50
	maxDateGroups   = 400
)

var groupTypes = []string{"select", "user", "team", "pipeline", "stage", "tags", "checkbox", "date", "datetime", "text", "company", "domain",
	"currency_code", "multi_select"}

// validate fills defaults and checks spec against o's properties.
func (spec *ReportSpec) validate(props []Property) (objectType, Property, Property, error) {
	var none Property
	o, ok := parseObject(spec.Object)
	if !ok {
		return "", none, none, httpx.BadRequest("object is company, contact, deal or ticket.")
	}
	spec.Object = string(o)
	if spec.Chart == "" {
		spec.Chart = "bar"
	}
	if !slices.Contains([]string{"bar", "line", "donut", "table", "number"}, spec.Chart) {
		return "", none, none, httpx.BadRequest("chart is bar, line, donut, table or number.")
	}
	if len(spec.Filters) > maxFilters {
		return "", none, none, httpx.BadRequest(fmt.Sprintf("Use at most %d filters.", maxFilters))
	}
	var measure Property
	switch spec.Measure {
	case "", "count":
		spec.Measure, spec.Property = "count", ""
	case "sum", "avg":
		p, ok := findProp(props, spec.Property)
		if !ok || (p.Type != "number" && p.Type != "currency") || p.expr == "" {
			return "", none, none, httpx.BadRequest("Sum and average need a number or amount property.")
		}
		measure = p
	default:
		return "", none, none, httpx.BadRequest("measure is count, sum or avg.")
	}
	var group Property
	if spec.GroupBy != "" {
		p, ok := findProp(props, spec.GroupBy)
		if !ok || !p.Filterable || !slices.Contains(groupTypes, p.Type) {
			return "", none, none, httpx.BadRequest("You can't group by \"" + spec.GroupBy + "\".")
		}
		group = p
		if p.Type == "date" || p.Type == "datetime" {
			if spec.Bucket == "" {
				spec.Bucket = "month"
			}
			if !slices.Contains([]string{"day", "week", "month", "quarter", "year"}, spec.Bucket) {
				return "", none, none, httpx.BadRequest("bucket is day, week, month, quarter or year.")
			}
		} else {
			spec.Bucket = ""
		}
	} else {
		spec.Bucket = ""
	}
	return o, measure, group, nil
}

// runReport computes spec over the records userID can open in orgID.
func (s *Service) runReport(ctx context.Context, userID, orgID uuid.UUID, spec ReportSpec, tz string) (*ReportResult, error) {
	o, ok := parseObject(spec.Object)
	if !ok {
		return nil, httpx.BadRequest("object is company, contact, deal or ticket.")
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
	if err != nil {
		return nil, err
	}
	o, measure, group, err := spec.validate(props)
	if err != nil {
		return nil, err
	}
	zone, err := location(tz)
	if err != nil {
		return nil, err
	}
	b := newSQL(ctx, userID)
	where, err := buildWhere(b, o, props, orgID, listSpec{Filters: spec.Filters, TZ: zone}, userID)
	if err != nil {
		return nil, err
	}
	res := &ReportResult{Rows: []ReportRow{}, ValueType: "count", Spec: spec}
	measureSQL := "count(*)::float8"
	if spec.Measure != "count" {
		e := measure.expr
		if strings.Contains(e, "{K}") {
			e = strings.ReplaceAll(e, "{K}", b.arg(measure.Key)+"::text")
		}
		fn := "sum"
		if spec.Measure == "avg" {
			fn = "avg"
		}
		measureSQL = "COALESCE(" + fn + "((" + e + ")::numeric), 0)::float8"
		res.ValueType = "number"
		if measure.Type == "currency" {
			res.ValueType = "money"
			cur := spec.Currency
			if cur == "" {
				if err := s.d.Pool.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&cur); err != nil {
					return nil, err
				}
			}
			if !currencyRe.MatchString(cur) {
				return nil, httpx.BadRequest("currency must be a 3-letter code like USD.")
			}
			res.Currency = cur
			if o == objDeal { // deal amounts carry their own currency
				where += " AND r.currency = " + b.arg(cur)
			}
		}
	}
	from := visibleFrom(o)
	keySQL, order := "''", "1"
	limit := maxReportGroups
	if spec.GroupBy != "" {
		e := group.expr
		if strings.Contains(e, "{K}") {
			e = strings.ReplaceAll(e, "{K}", b.arg(group.Key)+"::text")
		}
		switch group.Type {
		case "tags":
			from += ` LEFT JOIN LATERAL unnest(CASE WHEN cardinality(r.tag_ids) = 0 THEN ARRAY[NULL::uuid] ELSE r.tag_ids END) tg(id) ON true`
			keySQL = "tg.id::text"
		case "multi_select":
			from += ` LEFT JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(` + e + `) = 'array' AND jsonb_array_length(` + e + `) > 0
				THEN ` + e + ` ELSE '[null]'::jsonb END) ms(v) ON true`
			keySQL = "ms.v"
		case "company":
			if o == objContact {
				keySQL = `(SELECT cc.company_id::text FROM crm_contact_companies cc WHERE cc.contact_id = r.id AND cc.is_primary)`
			} else {
				keySQL = "r.company_id::text"
			}
		case "date", "datetime":
			d := "(" + e + ")::timestamp"
			if group.Type == "datetime" {
				d = "((" + e + ") AT TIME ZONE " + b.arg(zone) + "::text)"
			}
			keySQL = "to_char(date_trunc('" + spec.Bucket + "', " + d + "), 'YYYY-MM-DD')"
			order, limit = "1 NULLS LAST", maxDateGroups
		case "stage":
			keySQL = "r.stage_id::text"
		default:
			keySQL = "(" + e + ")::text"
		}
		if order == "1" {
			order = "3 DESC, 1"
		}
	}
	sql := `SELECT ` + keySQL + ` AS k, count(*), ` + measureSQL + ` ` + from + ` ` + where + ` GROUP BY 1 ORDER BY ` + order + ` LIMIT ` + b.arg(limit+1)
	rows, err := s.d.Pool.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k *string
		var row ReportRow
		if err := rows.Scan(&k, &row.Count, &row.Value); err != nil {
			rows.Close()
			return nil, err
		}
		row.Key = deref(k)
		res.Rows = append(res.Rows, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(res.Rows) > limit {
		res.Rows, res.Truncated = res.Rows[:limit], true
	}
	// Totals over every visible match (groups can overlap for tags).
	tb := newSQL(ctx, userID)
	twhere, err := buildWhere(tb, o, props, orgID, listSpec{Filters: spec.Filters, TZ: zone}, userID)
	if err != nil {
		return nil, err
	}
	tmeasure := "count(*)::float8"
	if spec.Measure != "count" {
		e := measure.expr
		if strings.Contains(e, "{K}") {
			e = strings.ReplaceAll(e, "{K}", tb.arg(measure.Key)+"::text")
		}
		fn := map[string]string{"sum": "sum", "avg": "avg"}[spec.Measure]
		tmeasure = "COALESCE(" + fn + "((" + e + ")::numeric), 0)::float8"
		if res.Currency != "" && o == objDeal {
			twhere += " AND r.currency = " + tb.arg(res.Currency)
		}
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*), `+tmeasure+` `+visibleFrom(o)+` `+twhere, tb.args...).Scan(&res.Count, &res.Total); err != nil {
		return nil, err
	}
	if spec.GroupBy != "" {
		if err := s.labelRows(ctx, orgID, o, group, spec.Bucket, res.Rows); err != nil {
			return nil, err
		}
		if group.Type == "stage" { // pipeline order, not by size
			order := map[string]int{}
			if pls, err := s.loadPipelines(ctx, s.d.Pool, orgID); err == nil {
				n := 0
				for _, p := range pls {
					for _, st := range p.Stages {
						order[st.ID.String()] = n
						n++
					}
				}
			}
			sort.SliceStable(res.Rows, func(i, j int) bool { return order[res.Rows[i].Key] < order[res.Rows[j].Key] })
		}
	}
	return res, nil
}

// labelRows names each group key for people.
func (s *Service) labelRows(ctx context.Context, orgID uuid.UUID, o objectType, p Property, bucket string, rows []ReportRow) error {
	names := map[string]string{}
	keys := []string{}
	for _, r := range rows {
		if r.Key != "" {
			keys = append(keys, r.Key)
		}
	}
	lookup := func(sql string) error {
		rs, err := s.d.Pool.Query(ctx, sql, keys)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var k, n string
			if err := rs.Scan(&k, &n); err != nil {
				return err
			}
			names[k] = n
		}
		return rs.Err()
	}
	switch p.Type {
	case "user":
		if err := lookup(`SELECT id::text, display_name FROM users WHERE id::text = ANY ($1) AND status <> 'deleted'`); err != nil {
			return err
		}
	case "team":
		if err := lookup(`SELECT id::text, name FROM access_teams WHERE id::text = ANY ($1)`); err != nil {
			return err
		}
	case "tags":
		if err := lookup(`SELECT id::text, name FROM crm_tags WHERE id::text = ANY ($1)`); err != nil {
			return err
		}
	case "company":
		if err := lookup(`SELECT id::text, name FROM crm_companies WHERE id::text = ANY ($1)`); err != nil {
			return err
		}
	case "pipeline", "stage":
		pls, err := s.loadPipelines(ctx, s.d.Pool, orgID)
		if err != nil {
			return err
		}
		for _, pl := range pls {
			names[pl.ID.String()] = pl.Name
			for _, st := range pl.Stages {
				names[st.ID.String()] = st.Name
			}
		}
	}
	for i := range rows {
		r := &rows[i]
		switch {
		case r.Key == "":
			r.Label = map[string]string{"user": "No owner", "team": "No team", "tags": "No tags", "company": "No company", "checkbox": "No"}[p.Type]
			if r.Label == "" {
				r.Label = "None"
			}
		case p.Type == "select" || p.Type == "multi_select":
			r.Label = optionLabel(p.Options, r.Key)
		case p.Type == "checkbox":
			r.Label = map[string]string{"true": "Yes", "false": "No"}[r.Key]
		case p.Type == "date" || p.Type == "datetime":
			r.Label = bucketLabel(r.Key, bucket)
		case names[r.Key] != "":
			r.Label = names[r.Key]
		case p.Type == "user":
			r.Label = "Former member"
		default:
			r.Label = r.Key
		}
	}
	return nil
}

// bucketLabel names a date bucket starting on key (YYYY-MM-DD).
func bucketLabel(key, bucket string) string {
	t, err := time.Parse("2006-01-02", key)
	if err != nil {
		return key
	}
	switch bucket {
	case "year":
		return t.Format("2006")
	case "quarter":
		return fmt.Sprintf("Q%d %d", (int(t.Month())-1)/3+1, t.Year())
	case "month":
		return t.Format("Jan 2006")
	case "week":
		return "Week of " + t.Format("2 Jan 2006")
	}
	return t.Format("2 Jan 2006")
}

// runReportRoute: POST /v1/crm/orgs/{orgID}/reports/run {spec} — the builder's preview.
func (s *Service) runReportRoute(w http.ResponseWriter, r *http.Request) error {
	var spec ReportSpec
	if err := httpx.Decode(r, &spec); err != nil {
		return err
	}
	res, err := s.runReport(r.Context(), me(r), orgParam(r), spec, r.URL.Query().Get("tz"))
	if err != nil {
		return err
	}
	httpx.OK(w, res)
	return nil
}

// ---- dashboards -------------------------------------------------------------------------

// ReportDashboard is a crm_dashboards row with its reports (computed for the caller on GET).
type ReportDashboard struct {
	ID        uuid.UUID    `json:"id"`
	OrgID     uuid.UUID    `json:"org_id"`
	Name      string       `json:"name"`
	Owner     *UserRef     `json:"owner"`
	MyLevel   access.Level `json:"my_level"`
	Reports   []*Report    `json:"reports"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// Report is a saved report with, on a dashboard read, its result.
type Report struct {
	ID       uuid.UUID     `json:"id"`
	Name     string        `json:"name"`
	Position int           `json:"position"`
	Spec     ReportSpec    `json:"spec"`
	Result   *ReportResult `json:"result,omitempty"`
	Error    *string       `json:"error,omitempty"` // the report can't run any more (a property was deleted…)
}

// listDashboards: GET /v1/crm/orgs/{orgID}/dashboards — the ones the caller can open.
func (s *Service) listDashboards(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	b := newSQL(ctx, me(r))
	rows, err := s.d.Pool.Query(ctx, `SELECT d.id, d.org_id, d.name, ar.owner_id, u.display_name, u.avatar_url, v.level, d.created_at, d.updated_at,
		(SELECT count(*) FROM crm_reports x WHERE x.dashboard_id = d.id)
		FROM crm_dashboards d JOIN access_visible($1, '`+KindDashboard+`', $2) v ON v.id = d.id
		JOIN access_resources ar ON ar.kind = '`+KindDashboard+`' AND ar.id = d.id
		LEFT JOIN users u ON u.id = ar.owner_id AND u.status <> 'deleted'
		WHERE d.org_id = `+b.arg(orgParam(r))+` ORDER BY lower(d.name), d.id LIMIT 200`, b.args...)
	if err != nil {
		return err
	}
	type item struct {
		ReportDashboard
		ReportCount int `json:"report_count"`
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (item, error) {
		var it item
		var ownerID *uuid.UUID
		var name, avatar *string
		var lvl int
		err := row.Scan(&it.ID, &it.OrgID, &it.Name, &ownerID, &name, &avatar, &lvl, &it.CreatedAt, &it.UpdatedAt, &it.ReportCount)
		if ownerID != nil && name != nil {
			it.Owner = &UserRef{ID: *ownerID, Name: *name, AvatarURL: avatar}
		}
		it.MyLevel, it.Reports = access.Level(lvl), nil
		return it, err
	})
	if err != nil {
		return err
	}
	if list == nil {
		list = []item{}
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) requireDashboard(ctx context.Context, q db.Querier, userID, id uuid.UUID, min access.Level) (*ReportDashboard, error) {
	lvl, err := access.Require(ctx, q, userID, KindDashboard, id, min)
	if err != nil {
		return nil, err
	}
	d := &ReportDashboard{ID: id, MyLevel: lvl, Reports: []*Report{}}
	var ownerID *uuid.UUID
	var name, avatar *string
	err = q.QueryRow(ctx, `SELECT d.org_id, d.name, d.created_at, d.updated_at, ar.owner_id, u.display_name, u.avatar_url
		FROM crm_dashboards d JOIN access_resources ar ON ar.kind = $2 AND ar.id = d.id
		LEFT JOIN users u ON u.id = ar.owner_id AND u.status <> 'deleted' WHERE d.id = $1`, id, KindDashboard).
		Scan(&d.OrgID, &d.Name, &d.CreatedAt, &d.UpdatedAt, &ownerID, &name, &avatar)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Dashboard not found.")
	}
	if err != nil {
		return nil, err
	}
	if ownerID != nil && name != nil {
		d.Owner = &UserRef{ID: *ownerID, Name: *name, AvatarURL: avatar}
	}
	if !allowedHere(ctx, q, d.OrgID, userID, PermAccess) {
		return nil, errNoCRM()
	}
	return d, nil
}

func (s *Service) loadReports(ctx context.Context, q db.Querier, dashboardID uuid.UUID) ([]*Report, error) {
	rows, err := q.Query(ctx, `SELECT id, name, position, spec FROM crm_reports WHERE dashboard_id = $1 ORDER BY position, created_at, id`, dashboardID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Report, error) {
		rp := &Report{}
		var raw []byte
		err := row.Scan(&rp.ID, &rp.Name, &rp.Position, &raw)
		if err == nil {
			_ = json.Unmarshal(raw, &rp.Spec)
		}
		return rp, err
	})
	if list == nil {
		list = []*Report{}
	}
	return list, err
}

// dashboardWithResults is a dashboard and every report computed for userID.
func (s *Service) dashboardWithResults(ctx context.Context, userID, id uuid.UUID, tz string) (*ReportDashboard, error) {
	d, err := s.requireDashboard(ctx, s.d.Pool, userID, id, access.View)
	if err != nil {
		return nil, err
	}
	if d.Reports, err = s.loadReports(ctx, s.d.Pool, id); err != nil {
		return nil, err
	}
	for _, rp := range d.Reports {
		res, err := s.runReport(ctx, userID, d.OrgID, rp.Spec, tz)
		if err != nil {
			var he *httpx.Error
			if e, ok := err.(*httpx.Error); ok && e.Status < 500 {
				he = e
			}
			if he == nil {
				return nil, err
			}
			msg := he.Message
			rp.Error = &msg
			continue
		}
		rp.Result = res
	}
	return d, nil
}

// getDashboard: GET /v1/crm/dashboards/{dashboardID}?tz= (view).
func (s *Service) getDashboard(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "dashboardID")
	if err != nil {
		return err
	}
	d, err := s.dashboardWithResults(r.Context(), me(r), id, r.URL.Query().Get("tz"))
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

type dashboardReq struct {
	Name *string `json:"name"`
}

// createDashboard: POST /v1/crm/orgs/{orgID}/dashboards {name} — private to its
// creator until shared.
func (s *Service) createDashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req dashboardReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(deref(req.Name))
	if name == "" || len([]rune(name)) > 120 {
		return httpx.BadRequest("Give the dashboard a name of up to 120 characters.")
	}
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_dashboards WHERE org_id = $1 AND created_by = $2`, orgID, me(r)).Scan(&n); err != nil {
			return err
		}
		if n >= 100 {
			return httpx.BadRequest("You can make at most 100 dashboards here.")
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm_dashboards (org_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`, orgID, name, me(r)).Scan(&id); err != nil {
			return err
		}
		return access.CreateResource(ctx, tx, access.NewResource{Kind: KindDashboard, ID: id, OrgID: orgID, OwnerID: me(r)})
	})
	if err != nil {
		return err
	}
	d, err := s.dashboardWithResults(ctx, me(r), id, "")
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

// updateDashboard: PATCH /v1/crm/dashboards/{dashboardID} {name} (edit).
func (s *Service) updateDashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "dashboardID")
	if err != nil {
		return err
	}
	var req dashboardReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if _, err := s.requireDashboard(ctx, s.d.Pool, me(r), id, access.Edit); err != nil {
		return err
	}
	name := strings.TrimSpace(deref(req.Name))
	if name == "" || len([]rune(name)) > 120 {
		return httpx.BadRequest("Give the dashboard a name of up to 120 characters.")
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE crm_dashboards SET name = $2, updated_at = now() WHERE id = $1`, id, name); err != nil {
		return err
	}
	d, err := s.dashboardWithResults(ctx, me(r), id, r.URL.Query().Get("tz"))
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// deleteDashboard: DELETE /v1/crm/dashboards/{dashboardID} (manage: its owner or an admin).
func (s *Service) deleteDashboard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "dashboardID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := s.requireDashboard(ctx, tx, me(r), id, access.Manage); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm_dashboards WHERE id = $1`, id); err != nil {
			return err
		}
		return access.DeleteResource(ctx, tx, access.Ref{Kind: KindDashboard, ID: id})
	})
	if err != nil {
		return err
	}
	access.Changed(ctx, access.Ref{Kind: KindDashboard, ID: id}, nil)
	httpx.NoContent(w)
	return nil
}

type reportReq struct {
	Name     *string     `json:"name"`
	Position *int        `json:"position"`
	Spec     *ReportSpec `json:"spec"`
}

// saveReport adds (reportID nil) or changes a report on a dashboard (edit).
func (s *Service) saveReport(ctx context.Context, userID, dashboardID uuid.UUID, reportID *uuid.UUID, req reportReq) (*Report, error) {
	d, err := s.requireDashboard(ctx, s.d.Pool, userID, dashboardID, access.Edit)
	if err != nil {
		return nil, err
	}
	var cur *Report
	if reportID != nil {
		list, err := s.loadReports(ctx, s.d.Pool, dashboardID)
		if err != nil {
			return nil, err
		}
		for _, rp := range list {
			if rp.ID == *reportID {
				cur = rp
			}
		}
		if cur == nil {
			return nil, httpx.NotFound("Report not found.")
		}
	} else {
		cur = &Report{}
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_reports WHERE dashboard_id = $1`, dashboardID).Scan(&n); err != nil {
			return nil, err
		}
		if n >= 30 {
			return nil, httpx.BadRequest("A dashboard can hold at most 30 reports.")
		}
		cur.Position = n
		if req.Spec == nil {
			return nil, httpx.BadRequest("Define the report first.")
		}
	}
	if req.Name != nil {
		cur.Name = strings.TrimSpace(*req.Name)
	}
	if cur.Name == "" || len([]rune(cur.Name)) > 120 {
		return nil, httpx.BadRequest("Give the report a name of up to 120 characters.")
	}
	if req.Position != nil {
		cur.Position = *req.Position
	}
	if req.Spec != nil {
		cur.Spec = *req.Spec
		o, ok := parseObject(cur.Spec.Object)
		if !ok {
			return nil, httpx.BadRequest("object is company, contact, deal or ticket.")
		}
		props, err := s.loadProperties(ctx, s.d.Pool, d.OrgID, o)
		if err != nil {
			return nil, err
		}
		if _, _, _, err := cur.Spec.validate(props); err != nil {
			return nil, err
		}
		// The filters must parse too (same check as a list).
		b := newSQL(ctx, userID)
		if _, err := buildWhere(b, o, props, d.OrgID, listSpec{Filters: cur.Spec.Filters}, userID); err != nil {
			return nil, err
		}
	}
	spec, _ := json.Marshal(cur.Spec)
	if reportID == nil {
		err = s.d.Pool.QueryRow(ctx, `INSERT INTO crm_reports (org_id, dashboard_id, position, name, spec, created_by) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			d.OrgID, dashboardID, cur.Position, cur.Name, spec, userID).Scan(&cur.ID)
	} else {
		_, err = s.d.Pool.Exec(ctx, `UPDATE crm_reports SET name = $2, position = $3, spec = $4, updated_at = now() WHERE id = $1`,
			cur.ID, cur.Name, cur.Position, spec)
	}
	if err != nil {
		return nil, err
	}
	_, _ = s.d.Pool.Exec(ctx, `UPDATE crm_dashboards SET updated_at = now() WHERE id = $1`, dashboardID)
	res, err := s.runReport(ctx, userID, d.OrgID, cur.Spec, "")
	if err == nil {
		cur.Result = res
	}
	return cur, nil
}

// createReport: POST /v1/crm/dashboards/{dashboardID}/reports {name, spec} (edit).
func (s *Service) createReport(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "dashboardID")
	if err != nil {
		return err
	}
	var req reportReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	rp, err := s.saveReport(r.Context(), me(r), id, nil, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, rp)
	return nil
}

// updateReport / deleteReport: PATCH, DELETE /v1/crm/dashboards/{dashboardID}/reports/{reportID} (edit).
func (s *Service) updateReport(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "dashboardID")
	if err != nil {
		return err
	}
	rid, err := idParam(r, "reportID")
	if err != nil {
		return err
	}
	var req reportReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	rp, err := s.saveReport(r.Context(), me(r), id, &rid, req)
	if err != nil {
		return err
	}
	httpx.OK(w, rp)
	return nil
}

func (s *Service) deleteReport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "dashboardID")
	if err != nil {
		return err
	}
	rid, err := idParam(r, "reportID")
	if err != nil {
		return err
	}
	if _, err := s.requireDashboard(ctx, s.d.Pool, me(r), id, access.Edit); err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM crm_reports WHERE id = $1 AND dashboard_id = $2`, rid, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Report not found.")
	}
	httpx.NoContent(w)
	return nil
}
