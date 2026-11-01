package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Row is a database row as views list it.
type Row struct {
	ID        uuid.UUID      `json:"id"`
	Title     string         `json:"title"`
	Icon      *string        `json:"icon"`
	Props     map[string]any `json:"props"`
	Position  string         `json:"position"`
	CreatedBy *uuid.UUID     `json:"created_by"`
	UpdatedBy *uuid.UUID     `json:"updated_by"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	MyLevel   access.Level   `json:"my_level"`
}

// RowPage is one page of a view's rows, with names for person values.
type RowPage struct {
	Items      []Row   `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Refs       Refs    `json:"refs"`
}

// rowQuery selects rows of one database. Exactly one audience applies: a
// member (me, with the agent cap when agent) or a public link viewer.
type rowQuery struct {
	db      *Page
	filters []Filter
	sorts   []Sort
	search  string
	limit   int
	cursor  string
	me      uuid.UUID
	agent   bool
	public  bool
}

type rowCursor struct {
	V  []*string `json:"v"`
	ID uuid.UUID `json:"id"`
}

// sqlb accumulates SQL arguments.
type sqlb struct{ args []any }

func (b *sqlb) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// sortKey is one ORDER BY component with the cast its cursor value needs.
type sortKey struct {
	expr string
	desc bool
	cast string // text | numeric | boolean | int
}

// valueExpr returns a SQL expression for a property's value suited to
// comparing and sorting (NULL when unset).
func valueExpr(b *sqlb, p Property, forSort bool) (string, string) {
	if p.Type == "title" {
		if forSort {
			return "NULLIF(lower(p.title), '')", "text"
		}
		return "p.title", "text"
	}
	k := b.arg(p.ID) + "::text"
	switch p.Type {
	case "number":
		return fmt.Sprintf("(CASE WHEN jsonb_typeof(p.props->%s) = 'number' THEN (p.props->>%s)::numeric END)", k, k), "numeric"
	case "checkbox":
		return fmt.Sprintf("COALESCE(p.props->>%s = 'true', false)", k), "boolean"
	case "date":
		return fmt.Sprintf("(p.props->%s->>'start')", k), "text"
	case "select", "status":
		if forSort {
			return fmt.Sprintf("array_position(%s::text[], p.props->>%s)", b.arg(optionIDs(p)), k), "int"
		}
		return fmt.Sprintf("(p.props->>%s)", k), "text"
	case "multi_select":
		if forSort {
			return fmt.Sprintf("array_position(%s::text[], p.props->%s->>0)", b.arg(optionIDs(p)), k), "int"
		}
		return fmt.Sprintf("COALESCE(p.props->%s, '[]'::jsonb)", k), "jsonb"
	case "person":
		if forSort {
			return fmt.Sprintf("(SELECT lower(u.display_name) FROM users u WHERE u.id::text = p.props->%s->>0)", k), "text"
		}
		return fmt.Sprintf("COALESCE(p.props->%s, '[]'::jsonb)", k), "jsonb"
	}
	if forSort {
		return fmt.Sprintf("NULLIF(lower(p.props->>%s), '')", k), "text"
	}
	return fmt.Sprintf("COALESCE(p.props->>%s, '')", k), "text"
}

// relativeDate turns today / tomorrow / yesterday into a date.
func relativeDate(s string) string {
	now := time.Now().UTC()
	switch s {
	case "today":
		return now.Format("2006-01-02")
	case "tomorrow":
		return now.AddDate(0, 0, 1).Format("2006-01-02")
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return s
}

// filterSQL renders one condition, or "" when it is incomplete (a view may
// hold a filter someone is still filling in; it doesn't narrow anything).
func filterSQL(b *sqlb, schema []Property, f Filter, me uuid.UUID) string {
	p, ok := findProp(schema, f.Property)
	if !ok {
		return ""
	}
	expr, _ := valueExpr(b, p, false)
	var str string
	var num float64
	var flag bool
	hasStr := json.Unmarshal(f.Value, &str) == nil
	hasNum := json.Unmarshal(f.Value, &num) == nil
	hasBool := json.Unmarshal(f.Value, &flag) == nil
	switch f.Op {
	case "is_empty", "is_not_empty":
		var empty string
		switch p.Type {
		case "multi_select", "person":
			empty = fmt.Sprintf("jsonb_array_length(%s) = 0", expr)
		case "title", "text", "url", "email":
			empty = fmt.Sprintf("%s = ''", expr)
		case "checkbox":
			empty = fmt.Sprintf("NOT %s", expr)
		default:
			empty = fmt.Sprintf("%s IS NULL", expr)
		}
		if f.Op == "is_not_empty" {
			return "NOT (" + empty + ")"
		}
		return empty
	}
	switch p.Type {
	case "title", "text", "url", "email":
		if !hasStr || str == "" {
			return ""
		}
		v := b.arg(likeEscape(str))
		switch f.Op {
		case "contains":
			return fmt.Sprintf("%s ILIKE '%%' || %s || '%%'", expr, v)
		case "not_contains":
			return fmt.Sprintf("%s NOT ILIKE '%%' || %s || '%%'", expr, v)
		case "starts_with":
			return fmt.Sprintf("%s ILIKE %s || '%%'", expr, v)
		case "is":
			return fmt.Sprintf("lower(%s) = lower(%s)", expr, b.arg(str))
		case "is_not":
			return fmt.Sprintf("lower(%s) <> lower(%s)", expr, b.arg(str))
		}
	case "number":
		if !hasNum {
			return ""
		}
		ops := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		if op, ok := ops[f.Op]; ok {
			if f.Op == "neq" {
				return fmt.Sprintf("%s IS DISTINCT FROM %s::numeric", expr, b.arg(num))
			}
			return fmt.Sprintf("%s %s %s::numeric", expr, op, b.arg(num))
		}
	case "select", "status":
		if !hasStr || str == "" {
			return ""
		}
		if f.Op == "is_not" {
			return fmt.Sprintf("%s IS DISTINCT FROM %s", expr, b.arg(str))
		}
		return fmt.Sprintf("%s = %s", expr, b.arg(str))
	case "multi_select", "person":
		if !hasStr || str == "" {
			return ""
		}
		if p.Type == "person" && str == "me" {
			str = me.String()
		}
		if f.Op == "not_contains" {
			return fmt.Sprintf("NOT (%s ? %s)", expr, b.arg(str))
		}
		return fmt.Sprintf("%s ? %s", expr, b.arg(str))
	case "date":
		if !hasStr {
			return ""
		}
		d := relativeDate(str)
		if !validDate(d) {
			return ""
		}
		day := fmt.Sprintf("left(%s, 10)", expr)
		ops := map[string]string{"is": "=", "before": "<", "after": ">", "on_or_before": "<=", "on_or_after": ">="}
		if op, ok := ops[f.Op]; ok {
			return fmt.Sprintf("%s %s %s", day, op, b.arg(d))
		}
	case "checkbox":
		if !hasBool {
			return ""
		}
		return fmt.Sprintf("%s = %s", expr, b.arg(flag))
	}
	return ""
}

// queryRows runs a row query with keyset pagination over the view's sorts
// (unset values last), then manual order, then id.
func (s *Service) queryRows(ctx context.Context, rq rowQuery) (*RowPage, error) {
	b := &sqlb{}
	where := []string{"p.parent_id = " + b.arg(rq.db.ID), "p.is_row", "p.deleted_at IS NULL"}
	level := "1"
	if rq.public {
		where = append(where, "EXISTS (SELECT 1 FROM access_resources ar WHERE ar.kind = 'docs.page' AND ar.id = p.id AND ar.inherits)")
	} else {
		level = fmt.Sprintf("access_level(%s, 'docs.page', p.id, %s)", b.arg(rq.me), b.arg(rq.agent))
		where = append(where, level+" >= 1")
	}
	for _, f := range rq.filters {
		// Build on a copy: an incomplete filter adds nothing, not even its arguments
		// (Postgres refuses parameters a query never uses).
		sub := &sqlb{args: append([]any(nil), b.args...)}
		if cond := filterSQL(sub, rq.db.Schema, f, rq.me); cond != "" {
			b.args = sub.args
			where = append(where, cond)
		}
	}
	if q := strings.TrimSpace(rq.search); q != "" {
		where = append(where, fmt.Sprintf("p.title ILIKE '%%' || %s || '%%'", b.arg(likeEscape(q))))
	}
	var keys []sortKey
	for _, srt := range rq.sorts {
		desc := srt.Direction == "desc"
		switch srt.Property {
		case "created_time":
			keys = append(keys, sortKey{"p.created_at", desc, "timestamptz"})
			continue
		case "last_edited_time":
			keys = append(keys, sortKey{"p.updated_at", desc, "timestamptz"})
			continue
		}
		p, ok := findProp(rq.db.Schema, srt.Property)
		if !ok {
			continue
		}
		expr, cast := valueExpr(b, p, true)
		keys = append(keys, sortKey{expr, desc, cast})
	}
	// Each key sorts unset values last: (expr IS NULL) ASC, then expr.
	type comp struct {
		expr, cast string
		desc       bool
	}
	var comps []comp
	for _, k := range keys {
		comps = append(comps, comp{"(" + k.expr + ") IS NULL", "boolean", false}, comp{k.expr, k.cast, k.desc})
	}
	comps = append(comps, comp{"p.position", "text", false})
	selVals := make([]string, len(comps))
	order := make([]string, len(comps))
	for i, c := range comps {
		selVals[i] = "(" + c.expr + ")::text"
		dir := "ASC"
		if c.desc {
			dir = "DESC"
		}
		order[i] = c.expr + " " + dir
	}
	order = append(order, "p.id ASC")
	if rq.cursor != "" {
		var cur rowCursor
		if err := httpx.DecodeCursor(rq.cursor, &cur); err != nil {
			return nil, err
		}
		if len(cur.V) != len(comps) {
			return nil, httpx.BadRequest("The view changed. Reload it.")
		}
		var ors []string
		var eqs []string
		for i, c := range comps {
			val := "NULL"
			if cur.V[i] != nil {
				val = b.arg(*cur.V[i]) + "::" + c.cast
			}
			op := ">"
			if c.desc {
				op = "<"
			}
			if cur.V[i] != nil {
				ors = append(ors, "("+strings.Join(append(slicesClone(eqs), fmt.Sprintf("(%s) %s %s", c.expr, op, val)), " AND ")+")")
			}
			eqs = append(eqs, fmt.Sprintf("(%s) IS NOT DISTINCT FROM %s", c.expr, val))
		}
		ors = append(ors, "("+strings.Join(append(eqs, "p.id > "+b.arg(cur.ID)), " AND ")+")")
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	limit := rq.limit
	sql := fmt.Sprintf(`SELECT p.id, p.title, p.icon, p.props, p.position, p.created_by, p.updated_by, p.created_at, p.updated_at, %s, ARRAY[%s]
		FROM docs_pages p WHERE %s ORDER BY %s LIMIT %s`,
		level, strings.Join(selVals, ", "), strings.Join(where, " AND "), strings.Join(order, ", "), b.arg(limit+1))
	rows, err := s.d.Pool.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scanned struct {
		row  Row
		vals []*string
	}
	var list []scanned
	for rows.Next() {
		var sc scanned
		var props []byte
		var lvl int
		if err := rows.Scan(&sc.row.ID, &sc.row.Title, &sc.row.Icon, &props, &sc.row.Position, &sc.row.CreatedBy, &sc.row.UpdatedBy,
			&sc.row.CreatedAt, &sc.row.UpdatedAt, &lvl, &sc.vals); err != nil {
			return nil, err
		}
		sc.row.Props = map[string]any{}
		_ = json.Unmarshal(props, &sc.row.Props)
		sc.row.MyLevel = access.Level(lvl)
		if rq.public {
			sc.row.MyLevel = access.View
			sc.row.CreatedBy, sc.row.UpdatedBy = nil, nil
		}
		list = append(list, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := &RowPage{Items: []Row{}}
	for i, sc := range list {
		if i == limit {
			last := list[limit-1]
			out.NextCursor = httpx.EncodeCursor(rowCursor{V: last.vals, ID: last.row.ID})
			break
		}
		out.Items = append(out.Items, sc.row)
	}
	var people []uuid.UUID
	for _, r := range out.Items {
		people = append(people, personValues(rq.db.Schema, r.Props)...)
	}
	refs, err := s.resolveRefs(ctx, rq.me, !rq.public, people, nil)
	if err != nil {
		return nil, err
	}
	out.Refs = refs
	return out, nil
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

// rowsRequest reads ?view=&cursor=&limit=&q= and the optional ad-hoc
// filters/sorts (JSON) that override the view's.
func rowsRequest(r *http.Request, dbp *Page, views []View) (rowQuery, error) {
	q := r.URL.Query()
	rq := rowQuery{db: dbp, cursor: q.Get("cursor"), search: q.Get("q"), limit: 100}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		rq.limit = min(v, 1000)
	}
	if vid := q.Get("view"); vid != "" {
		found := false
		for _, v := range views {
			if v.ID.String() == vid {
				rq.filters, rq.sorts, found = v.Config.Filters, v.Config.Sorts, true
			}
		}
		if !found {
			return rq, httpx.NotFound("View not found.")
		}
	}
	if f := q.Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &rq.filters); err != nil || len(rq.filters) > 20 {
			return rq, httpx.BadRequest("filters must be a JSON list of {property, op, value}.")
		}
	}
	if srt := q.Get("sorts"); srt != "" {
		if err := json.Unmarshal([]byte(srt), &rq.sorts); err != nil || len(rq.sorts) > 5 {
			return rq, httpx.BadRequest("sorts must be a JSON list of {property, direction}.")
		}
	}
	return rq, nil
}

// listRows: GET /docs/databases/{id}/rows?view=&filters=&sorts=&q=&cursor=&limit=
// (view on the database; each row also needs view).
func (s *Service) listRows(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	views, err := loadViews(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	rq, err := rowsRequest(r, dbp, views)
	if err != nil {
		return err
	}
	rq.me, rq.agent = me, access.ViaAgent(ctx)
	out, err := s.queryRows(ctx, rq)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
