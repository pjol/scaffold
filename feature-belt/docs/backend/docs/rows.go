package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Rows are read one of two ways. When every filter and sort can be said in
// SQL (stored values, created/edited columns) rows come from a keyset query.
// When one needs a computed value (formula, rollup, a relation's titles), or
// the view groups rows or shows calculations, the SQL narrows by access and
// the conditions it can express, and Go computes, filters, sorts, groups and
// pages over the database's rows (at most maxRows). Either way only rows
// the viewer can open are read, and computed values see only related rows
// they can open.

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
	// The row page's cover, and for gallery and feed views the start of its
	// content.
	Cover   *Cover `json:"cover,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	// Grouped views: the group (and sub-group) keys the row falls in.
	Groups    []string `json:"groups,omitempty"`
	SubGroups []string `json:"sub_groups,omitempty"`
	// Nested sub-items: how many sub-items the viewer can open.
	SubCount *int `json:"sub_count,omitempty"`
	// Connected rows: where the row comes from, its platform id and page.
	Source *app.SourceRef `json:"source,omitempty"`
	Ref    string         `json:"ref,omitempty"`
	URL    string         `json:"url,omitempty"`
}

// GroupInfo is one group of a grouped view, in display order.
type GroupInfo struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Color  string `json:"color,omitempty"`
	Count  int    `json:"count"`
	Hidden bool   `json:"hidden,omitempty"`
}

// RowPage is one page of a view's rows, with names for what values point at.
// The first page also carries the total, groups and calculations.
type RowPage struct {
	Items        []Row          `json:"items"`
	NextCursor   *string        `json:"next_cursor"`
	Refs         Refs           `json:"refs"`
	Total        *int           `json:"total,omitempty"`
	Groups       []GroupInfo    `json:"groups,omitempty"`
	SubGroups    []GroupInfo    `json:"sub_groups,omitempty"`
	Calculations map[string]any `json:"calculations,omitempty"`
}

// rowQuery selects rows of one database. Exactly one audience applies: a
// member (me, with the agent cap when agent) or a public link viewer
// (public, with the link's token).
type rowQuery struct {
	db      *Page
	filters []Filter
	conj    string // top-level filters: and (default) | or
	sorts   []Sort
	search  string
	limit   int
	cursor  string
	me      uuid.UUID
	agent   bool
	public  bool
	token   string
	tz      string
	view    *View      // grouping, calculations, sub-items
	parent  *uuid.UUID // nested sub-items of this row
	noMeta  bool       // no groups or calculations (exports)
}

func (rq rowQuery) viewer() viewer {
	v := viewer{me: rq.me, agent: rq.agent, loc: loadTZ(rq.tz)}
	if rq.public {
		v.me, v.agent, v.token = uuid.Nil, false, rq.token
		if v.token == "" {
			v.token = "-" // public without a token opens no related rows
		}
	}
	return v
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
	cast string // text | numeric | boolean | int | timestamptz
}

// sqlType: the property's values can be compared and sorted in SQL.
func sqlType(p Property) bool { return p.Type != "formula" && p.Type != "rollup" && p.Type != "button" }

// valueExpr returns a SQL expression for a property's value suited to
// comparing and sorting (NULL when unset), and its cast.
func valueExpr(b *sqlb, p Property, forSort bool) (string, string) {
	switch p.Type {
	case "title":
		if forSort {
			return "NULLIF(lower(p.title), '')", "text"
		}
		return "p.title", "text"
	case "created_time":
		return "p.created_at", "timestamptz"
	case "last_edited_time":
		return "p.updated_at", "timestamptz"
	case "created_by", "last_edited_by":
		col := "p.created_by"
		if p.Type == "last_edited_by" {
			col = "p.updated_by"
		}
		if forSort {
			return fmt.Sprintf("(SELECT lower(u.display_name) FROM users u WHERE u.id = %s)", col), "text"
		}
		return fmt.Sprintf("COALESCE(jsonb_build_array(%s::text), '[]'::jsonb)", col), "jsonb"
	}
	k := b.arg(p.ID) + "::text"
	switch p.Type {
	case "number", "unique_id":
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
	case "relation", "files":
		if forSort {
			return fmt.Sprintf("NULLIF(jsonb_array_length(COALESCE(p.props->%s, '[]'::jsonb)), 0)", k), "int"
		}
		return fmt.Sprintf("COALESCE(p.props->%s, '[]'::jsonb)", k), "jsonb"
	}
	if forSort {
		return fmt.Sprintf("NULLIF(lower(p.props->>%s), '')", k), "text"
	}
	return fmt.Sprintf("COALESCE(p.props->>%s, '')", k), "text"
}

// ---- filter values ------------------------------------------------------------------

type filterArg struct {
	str     string
	num     float64
	flag    bool
	hasStr  bool
	hasNum  bool
	hasBool bool
}

func readFilterArg(raw json.RawMessage) filterArg {
	var a filterArg
	a.hasStr = json.Unmarshal(raw, &a.str) == nil
	a.hasNum = json.Unmarshal(raw, &a.num) == nil
	if !a.hasNum && a.hasStr {
		if n, err := strconv.ParseFloat(strings.TrimSpace(a.str), 64); err == nil {
			a.num, a.hasNum = n, true
		}
	}
	a.hasBool = json.Unmarshal(raw, &a.flag) == nil
	return a
}

// relativeDate turns a relative day into YYYY-MM-DD in loc: today,
// tomorrow, yesterday, one_week_ago, one_week_from_now, one_month_ago,
// one_month_from_now; other values pass through.
func relativeDate(s string) string { return relativeDay(s, time.UTC) }

func relativeDay(s string, loc *time.Location) string {
	now := time.Now().In(loc)
	switch s {
	case "today":
		return now.Format("2006-01-02")
	case "tomorrow":
		return now.AddDate(0, 0, 1).Format("2006-01-02")
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02")
	case "one_week_ago":
		return now.AddDate(0, 0, -7).Format("2006-01-02")
	case "one_week_from_now":
		return now.AddDate(0, 0, 7).Format("2006-01-02")
	case "one_month_ago":
		return now.AddDate(0, -1, 0).Format("2006-01-02")
	case "one_month_from_now":
		return now.AddDate(0, 1, 0).Format("2006-01-02")
	}
	return s
}

// withinRange: the days (inclusive) "is within" covers.
func withinRange(s string, loc *time.Location) (string, string, bool) {
	now := time.Now().In(loc)
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch s {
	case "past_week":
		return day(today.AddDate(0, 0, -7)), day(today), true
	case "past_month":
		return day(today.AddDate(0, -1, 0)), day(today), true
	case "past_year":
		return day(today.AddDate(-1, 0, 0)), day(today), true
	case "next_week":
		return day(today), day(today.AddDate(0, 0, 7)), true
	case "next_month":
		return day(today), day(today.AddDate(0, 1, 0)), true
	case "next_year":
		return day(today), day(today.AddDate(1, 0, 0)), true
	case "this_week":
		wd := (int(today.Weekday()) + 6) % 7 // Monday first
		start := today.AddDate(0, 0, -wd)
		return day(start), day(start.AddDate(0, 0, 6)), true
	case "this_month":
		start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, loc)
		return day(start), day(start.AddDate(0, 1, -1)), true
	case "this_year":
		return fmt.Sprintf("%d-01-01", today.Year()), fmt.Sprintf("%d-12-31", today.Year()), true
	}
	return "", "", false
}

// ---- SQL filters --------------------------------------------------------------------

// filterSQL renders a condition or group as SQL. ok is false when it needs
// computed values; cond "" means it doesn't narrow anything (a filter
// someone is still filling in).
func filterSQL(b *sqlb, schema []Property, f Filter, v viewer) (cond string, ok bool) {
	if f.isGroup() {
		var parts []string
		for _, x := range f.Filters {
			sub := &sqlb{args: append([]any(nil), b.args...)}
			c, ok := filterSQL(sub, schema, x, v)
			if !ok {
				return "", false
			}
			if c != "" {
				b.args = sub.args
				parts = append(parts, "("+c+")")
			}
		}
		if len(parts) == 0 {
			return "", true
		}
		op := " AND "
		if f.Conj == "or" {
			op = " OR "
		}
		return strings.Join(parts, op), true
	}
	p, found := findProp(schema, f.Property)
	if !found {
		return "", true
	}
	if !sqlType(p) {
		return "", false
	}
	return conditionSQL(b, p, f, v), true
}

func conditionSQL(b *sqlb, p Property, f Filter, v viewer) string {
	expr, _ := valueExpr(b, p, false)
	a := readFilterArg(f.Value)
	switch f.Op {
	case "is_empty", "is_not_empty":
		var empty string
		switch p.Type {
		case "multi_select", "person", "relation", "files", "created_by", "last_edited_by":
			empty = fmt.Sprintf("jsonb_array_length(%s) = 0", expr)
		case "title", "text", "url", "email", "phone":
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
	case "title", "text", "url", "email", "phone":
		if !a.hasStr || a.str == "" {
			return ""
		}
		// Only the operator's own argument is added (Postgres refuses unused ones).
		like := func() string { return b.arg(likeEscape(a.str)) }
		switch f.Op {
		case "contains":
			return fmt.Sprintf("%s ILIKE '%%' || %s || '%%'", expr, like())
		case "not_contains":
			return fmt.Sprintf("%s NOT ILIKE '%%' || %s || '%%'", expr, like())
		case "starts_with":
			return fmt.Sprintf("%s ILIKE %s || '%%'", expr, like())
		case "ends_with":
			return fmt.Sprintf("%s ILIKE '%%' || %s", expr, like())
		case "is":
			return fmt.Sprintf("lower(%s) = lower(%s)", expr, b.arg(a.str))
		case "is_not":
			return fmt.Sprintf("lower(%s) <> lower(%s)", expr, b.arg(a.str))
		}
	case "number", "unique_id":
		if !a.hasNum {
			return ""
		}
		n := a.num * numberScale(p) // filters name amounts in major units
		if _, cur := currencyExp(p.NumberFormat); cur {
			n = math.Round(n) // stored as integer minor units
		}
		ops := map[string]string{"eq": "=", "neq": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
		if op, ok := ops[f.Op]; ok {
			if f.Op == "neq" {
				return fmt.Sprintf("%s IS DISTINCT FROM %s::numeric", expr, b.arg(n))
			}
			return fmt.Sprintf("%s %s %s::numeric", expr, op, b.arg(n))
		}
	case "select", "status":
		if !a.hasStr || a.str == "" {
			return ""
		}
		if f.Op == "is_not" {
			return fmt.Sprintf("%s IS DISTINCT FROM %s", expr, b.arg(a.str))
		}
		return fmt.Sprintf("%s = %s", expr, b.arg(a.str))
	case "multi_select", "person", "relation", "created_by", "last_edited_by":
		if !a.hasStr || a.str == "" {
			return ""
		}
		val := a.str
		if val == "me" && p.Type != "multi_select" && p.Type != "relation" {
			if v.me == uuid.Nil {
				return "false"
			}
			val = v.me.String()
		}
		if f.Op == "not_contains" {
			return fmt.Sprintf("NOT (%s ? %s)", expr, b.arg(val))
		}
		return fmt.Sprintf("%s ? %s", expr, b.arg(val))
	case "date", "created_time", "last_edited_time":
		if !a.hasStr {
			return ""
		}
		day := fmt.Sprintf("left(%s, 10)", expr)
		if p.Type != "date" {
			day = fmt.Sprintf("to_char(%s AT TIME ZONE %s, 'YYYY-MM-DD')", expr, b.arg(v.loc.String()))
		}
		if f.Op == "is_within" {
			from, to, ok := withinRange(a.str, v.loc)
			if !ok {
				return ""
			}
			return fmt.Sprintf("%s BETWEEN %s AND %s", day, b.arg(from), b.arg(to))
		}
		d := relativeDay(a.str, v.loc)
		if !validDate(d) {
			return ""
		}
		ops := map[string]string{"is": "=", "before": "<", "after": ">", "on_or_before": "<=", "on_or_after": ">="}
		if op, ok := ops[f.Op]; ok {
			return fmt.Sprintf("%s %s %s", day, op, b.arg(d))
		}
	case "checkbox":
		if !a.hasBool {
			return ""
		}
		return fmt.Sprintf("%s = %s", expr, b.arg(a.flag))
	}
	return ""
}

// ---- Go filters ---------------------------------------------------------------------

// matches evaluates a filter (or group) on one row; incomplete conditions
// don't narrow anything. It agrees with filterSQL on stored values.
func (c *computer) matches(dbID uuid.UUID, schema []Property, r *crow, f Filter) bool {
	ok, _ := c.match(dbID, schema, r, f)
	return ok
}

func (c *computer) match(dbID uuid.UUID, schema []Property, r *crow, f Filter) (result, complete bool) {
	if f.isGroup() {
		any := false
		or := f.Conj == "or"
		hit := !or
		for _, x := range f.Filters {
			m, done := c.match(dbID, schema, r, x)
			if !done {
				continue
			}
			any = true
			if or && m {
				hit = true
			}
			if !or && !m {
				hit = false
			}
		}
		if !any {
			return true, false
		}
		return hit, true
	}
	p, ok := findProp(schema, f.Property)
	if !ok {
		return true, false
	}
	return c.condition(dbID, r, p, f)
}

func (c *computer) condition(dbID uuid.UUID, r *crow, p Property, f Filter) (bool, bool) {
	a := readFilterArg(f.Value)
	raw := r.Props[p.ID]
	ids := func() []string {
		switch p.Type {
		case "created_by":
			if r.CreatedBy != nil {
				return []string{r.CreatedBy.String()}
			}
			return nil
		case "last_edited_by":
			if r.UpdatedBy != nil {
				return []string{r.UpdatedBy.String()}
			}
			return nil
		}
		return fStringList(raw)
	}
	switch p.Type {
	case "select", "status":
		s, _ := raw.(string)
		switch f.Op {
		case "is_empty":
			return s == "", true
		case "is_not_empty":
			return s != "", true
		}
		if !a.hasStr || a.str == "" {
			return true, false
		}
		if f.Op == "is_not" {
			return s != a.str, true
		}
		return s == a.str, true
	case "multi_select", "person", "relation", "created_by", "last_edited_by", "files":
		list := ids()
		if p.Type == "files" {
			n := 0
			if l, ok := raw.([]any); ok {
				n = len(l)
			}
			switch f.Op {
			case "is_empty":
				return n == 0, true
			case "is_not_empty":
				return n > 0, true
			}
			return true, false
		}
		switch f.Op {
		case "is_empty":
			return len(list) == 0, true
		case "is_not_empty":
			return len(list) > 0, true
		}
		if !a.hasStr || a.str == "" {
			return true, false
		}
		val := a.str
		if val == "me" && p.Type != "multi_select" && p.Type != "relation" {
			if c.v.me == uuid.Nil {
				return false, true
			}
			val = c.v.me.String()
		}
		has := slices.Contains(list, val)
		if f.Op == "not_contains" {
			return !has, true
		}
		return has, true
	case "checkbox":
		b, _ := raw.(bool)
		if !a.hasBool {
			return true, false
		}
		return b == a.flag, true
	}
	return valueCondition(c.Value(dbID, r.ID, p), p, f, a, c)
}

// valueCondition tests a typed value (text, numbers, dates, formula and
// rollup results).
func valueCondition(v fValue, p Property, f Filter, a filterArg, c *computer) (bool, bool) {
	switch f.Op {
	case "is_empty":
		return blank(v), true
	case "is_not_empty":
		return !blank(v), true
	}
	switch v.Kind {
	case fEmpty:
		// Unset values: "is not" / "doesn't contain" / "≠" match, others don't.
		switch f.Op {
		case "is_not", "not_contains", "neq":
			return true, a.hasStr || a.hasNum
		}
		switch {
		case f.Op == "is" && p.Type == "checkbox":
			return !a.flag, a.hasBool
		case a.hasStr || a.hasNum || a.hasBool:
			return false, true
		}
		return true, false
	case fBool:
		if !a.hasBool {
			return true, false
		}
		return v.Bool == a.flag, true
	case fNumber:
		if !a.hasNum {
			return true, false
		}
		n, num := a.num, v.Num
		if p.Type == "number" {
			// Currencies compare in whole minor units, as the SQL path does.
			if _, cur := currencyExp(p.NumberFormat); cur {
				s := numberScale(p)
				n, num = math.Round(n*s), math.Round(num*s)
			}
		}
		switch f.Op {
		case "eq":
			return num == n, true
		case "neq":
			return num != n, true
		case "gt":
			return num > n, true
		case "gte":
			return num >= n, true
		case "lt":
			return num < n, true
		case "lte":
			return num <= n, true
		}
		return true, false
	case fDate:
		if !a.hasStr {
			return true, false
		}
		day := v.Date.Start.In(c.v.loc).Format("2006-01-02")
		if !v.Date.HasTime {
			day = v.Date.Start.Format("2006-01-02")
		}
		if f.Op == "is_within" {
			from, to, ok := withinRange(a.str, c.v.loc)
			if !ok {
				return true, false
			}
			return day >= from && day <= to, true
		}
		d := relativeDay(a.str, c.v.loc)
		if !validDate(d) {
			return true, false
		}
		switch f.Op {
		case "is":
			return day == d, true
		case "before":
			return day < d, true
		case "after":
			return day > d, true
		case "on_or_before":
			return day <= d, true
		case "on_or_after":
			return day >= d, true
		}
		return true, false
	case fList, fPerson, fPage:
		if !a.hasStr || a.str == "" {
			return true, false
		}
		needle := strings.ToLower(a.str)
		var hit bool
		items := v.List
		if v.Kind != fList {
			items = []fValue{v}
		}
		for _, x := range leaves(items) {
			if (x.Kind == fPerson || x.Kind == fPage) && x.ID.String() == a.str {
				hit = true
				break
			}
			if a.str == "me" && x.Kind == fPerson && x.ID == c.v.me && c.v.me != uuid.Nil {
				hit = true
				break
			}
			if strings.Contains(strings.ToLower(fValueText(x, c)), needle) {
				hit = true
				break
			}
		}
		if f.Op == "not_contains" {
			return !hit, true
		}
		return hit, true
	}
	// Text.
	if !a.hasStr || a.str == "" {
		return true, false
	}
	s := strings.ToLower(fValueText(v, c))
	n := strings.ToLower(a.str)
	switch f.Op {
	case "is":
		return s == n, true
	case "is_not":
		return s != n, true
	case "contains":
		return strings.Contains(s, n), true
	case "not_contains":
		return !strings.Contains(s, n), true
	case "starts_with":
		return strings.HasPrefix(s, n), true
	case "ends_with":
		return strings.HasSuffix(s, n), true
	}
	return true, false
}

// ---- sorting ------------------------------------------------------------------------

// sortValue is what a row sorts by for p: option order for selects, names
// for people and related rows, lower-case text, else the value.
func (c *computer) sortValue(dbID uuid.UUID, schema []Property, r *crow, prop string) fValue {
	switch prop {
	case "created_time":
		return fValue{Kind: fDate, Date: fDateValue{Start: r.CreatedAt, HasTime: true}}
	case "last_edited_time":
		return fValue{Kind: fDate, Date: fDateValue{Start: r.UpdatedAt, HasTime: true}}
	}
	p, ok := findProp(schema, prop)
	if !ok {
		return vEmpty
	}
	optIndex := func(id string) fValue {
		for i, o := range p.Options {
			if o.ID == id {
				return vNum(float64(i))
			}
		}
		return vEmpty
	}
	switch p.Type {
	case "select", "status":
		s, _ := r.Props[p.ID].(string)
		return optIndex(s)
	case "multi_select":
		if l := fStringList(r.Props[p.ID]); len(l) > 0 {
			return optIndex(l[0])
		}
		return vEmpty
	case "title":
		if r.Title == "" {
			return vEmpty
		}
		return vText(strings.ToLower(r.Title))
	}
	v := c.Value(dbID, r.ID, p)
	if v.Kind == fList {
		if len(v.List) == 0 {
			return vEmpty
		}
		v = v.List[0]
	}
	switch v.Kind {
	case fPerson:
		return vText(strings.ToLower(c.PersonName(v.ID)))
	case fPage:
		if rr, ok := c.rows[v.ID]; ok {
			return vText(strings.ToLower(rr.Title))
		}
		return vEmpty
	case fText:
		return vText(strings.ToLower(v.Str))
	}
	return v
}

// goCursor continues a Go-paged view: the last row's sort values, position
// and id.
type goCursor struct {
	Go bool        `json:"go"`
	K  []cursorKey `json:"k"`
	P  string      `json:"p"`
	ID uuid.UUID   `json:"id"`
}

type cursorKey struct {
	E bool    `json:"e,omitempty"`
	K fKind   `json:"t,omitempty"`
	N float64 `json:"n,omitempty"`
	S string  `json:"s,omitempty"`
	B bool    `json:"b,omitempty"`
}

func toCursorKey(v fValue) cursorKey {
	switch v.Kind {
	case fNumber:
		return cursorKey{K: fNumber, N: v.Num}
	case fText:
		return cursorKey{K: fText, S: v.Str}
	case fBool:
		return cursorKey{K: fBool, B: v.Bool}
	case fDate:
		return cursorKey{K: fDate, N: float64(v.Date.Start.UnixMilli())}
	case fPerson, fPage:
		return cursorKey{K: fText, S: v.ID.String()}
	}
	return cursorKey{E: true}
}

func fromCursorKey(k cursorKey) fValue {
	switch {
	case k.E:
		return vEmpty
	case k.K == fNumber:
		return vNum(k.N)
	case k.K == fBool:
		return vBool(k.B)
	case k.K == fDate:
		return fValue{Kind: fDate, Date: fDateValue{Start: time.UnixMilli(int64(k.N)).UTC(), HasTime: true}}
	}
	return vText(k.S)
}

type sortedRow struct {
	r    *crow
	keys []fValue
}

// lessRows orders by the sorts (unset values last either way), then manual
// order, then id.
func lessRows(a, b sortedRow, sorts []Sort) int {
	for i, s := range sorts {
		x, y := a.keys[i], b.keys[i]
		xe, ye := x.Kind == fEmpty, y.Kind == fEmpty
		switch {
		case xe && ye:
			continue
		case xe:
			return 1
		case ye:
			return -1
		}
		cmp := fCompare(x, y)
		if x.Kind == fDate && y.Kind == fDate {
			cmp = compareTime(x.Date.Start, y.Date.Start)
		}
		if cmp != 0 {
			if s.Direction == "desc" {
				return -cmp
			}
			return cmp
		}
	}
	if c := strings.Compare(a.r.Position, b.r.Position); c != 0 {
		return c
	}
	return strings.Compare(a.r.ID.String(), b.r.ID.String())
}

func compareTime(a, b time.Time) int {
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	}
	return 0
}

// ---- the query ----------------------------------------------------------------------

// needsGo: the query can't run as a single keyset SQL query.
func (rq rowQuery) needsGo() bool {
	schema := rq.db.Schema
	b := &sqlb{}
	for _, f := range rq.filters {
		if _, ok := filterSQL(b, schema, f, rq.viewer()); !ok {
			return true
		}
	}
	for _, s := range rq.sorts {
		if s.Property == "created_time" || s.Property == "last_edited_time" {
			continue
		}
		if p, ok := findProp(schema, s.Property); ok && (!sqlType(p) || p.Type == "relation") {
			return true
		}
	}
	if rq.view != nil && !rq.noMeta {
		cfg := rq.view.Config
		if cfg.GroupBy != "" || len(cfg.Calculations) > 0 {
			return true
		}
	}
	return false
}

// baseWhere: rows of the database the audience can open, not templates.
func (rq rowQuery) baseWhere(b *sqlb) ([]string, string) {
	where := []string{"p.parent_id = " + b.arg(rq.db.ID), "p.is_row", "NOT p.is_template", "p.deleted_at IS NULL"}
	level := "1"
	if rq.public {
		where = append(where, "EXISTS (SELECT 1 FROM access_resources ar WHERE ar.kind = 'docs.page' AND ar.id = p.id AND ar.inherits)",
			"p.public_blocked_at IS NULL")
	} else {
		level = fmt.Sprintf("access_level(%s, 'docs.page', p.id, %s)", b.arg(rq.me), b.arg(rq.agent))
		where = append(where, level+" >= 1")
	}
	if q := strings.TrimSpace(rq.search); q != "" {
		where = append(where, fmt.Sprintf("p.title ILIKE '%%' || %s || '%%'", b.arg(likeEscape(q))))
	}
	if si := rq.subItems(); si != nil {
		k := b.arg(si.Property) + "::text"
		if rq.parent != nil {
			where = append(where, fmt.Sprintf("COALESCE(p.props->%s, '[]'::jsonb) ? %s", k, b.arg(rq.parent.String())))
		} else {
			where = append(where, fmt.Sprintf("jsonb_array_length(COALESCE(p.props->%s, '[]'::jsonb)) = 0", k))
		}
	}
	return where, level
}

// subItems: the view nests sub-items under parents.
func (rq rowQuery) subItems() *SubItemsConfig {
	if rq.view == nil || rq.view.Config.SubItems == nil || rq.view.Config.SubItems.Display != "nested" {
		return nil
	}
	if !selfRelation(rq.db.Schema, rq.db.ID, rq.view.Config.SubItems.Property) {
		return nil
	}
	return rq.view.Config.SubItems
}

// queryRows runs a row query for its audience.
func (s *Service) queryRows(ctx context.Context, rq rowQuery) (*RowPage, error) {
	if rq.limit <= 0 {
		rq.limit = 100
	}
	if rq.conj != "or" {
		rq.conj = "and"
	}
	// Sub-items of one row ignore the view's filters (they show under their parent).
	if rq.parent != nil {
		rq.filters = nil
	}
	if rq.needsGo() {
		return s.queryRowsGo(ctx, rq)
	}
	return s.queryRowsSQL(ctx, rq)
}

func (s *Service) queryRowsSQL(ctx context.Context, rq rowQuery) (*RowPage, error) {
	b := &sqlb{}
	where, level := rq.baseWhere(b)
	v := rq.viewer()
	root := Filter{Conj: rq.conj, Filters: rq.filters}
	if len(rq.filters) > 0 {
		sub := &sqlb{args: append([]any(nil), b.args...)}
		if cond, _ := filterSQL(sub, rq.db.Schema, root, v); cond != "" {
			b.args = sub.args
			where = append(where, "("+cond+")")
		}
	}
	// The total counts with the filters only (sort keys add arguments).
	countWhere := append([]string(nil), where...)
	countArgs := append([]any(nil), b.args...)
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
				ors = append(ors, "("+strings.Join(append(slices.Clone(eqs), fmt.Sprintf("(%s) %s %s", c.expr, op, val)), " AND ")+")")
			}
			eqs = append(eqs, fmt.Sprintf("(%s) IS NOT DISTINCT FROM %s", c.expr, val))
		}
		ors = append(ors, "("+strings.Join(append(eqs, "p.id > "+b.arg(cur.ID)), " AND ")+")")
		where = append(where, "("+strings.Join(ors, " OR ")+")")
	}
	limit := rq.limit
	sql := fmt.Sprintf(`SELECT p.id, p.title, p.icon, p.cover, p.props, p.position, p.created_by, p.updated_by, p.created_at, p.updated_at, %s, ARRAY[%s]
		FROM docs_pages p WHERE %s ORDER BY %s LIMIT %s`,
		level, strings.Join(selVals, ", "), strings.Join(where, " AND "), strings.Join(order, ", "), b.arg(limit+1))
	rows, err := s.d.Pool.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type scanned struct {
		row  *crow
		vals []*string
	}
	var list []scanned
	for rows.Next() {
		var sc scanned
		r := &crow{DB: rq.db.ID}
		var props, cover []byte
		var lvl int
		if err := rows.Scan(&r.ID, &r.Title, &r.Icon, &cover, &props, &r.Position, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &lvl, &sc.vals); err != nil {
			return nil, err
		}
		if len(cover) > 0 {
			_ = json.Unmarshal(cover, &r.Cover)
		}
		r.Props = map[string]any{}
		_ = json.Unmarshal(props, &r.Props)
		r.Level = access.Level(lvl)
		sc.row = r
		list = append(list, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	out := &RowPage{Items: []Row{}}
	var page []*crow
	for i, sc := range list {
		if i == limit {
			last := list[limit-1]
			out.NextCursor = httpx.EncodeCursor(rowCursor{V: last.vals, ID: last.row.ID})
			break
		}
		page = append(page, sc.row)
	}
	if rq.cursor == "" && !rq.noMeta {
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_pages p WHERE `+strings.Join(countWhere, " AND "), countArgs...).Scan(&n); err != nil {
			return nil, err
		}
		out.Total = &n
	}
	c := s.newComputer(ctx, v)
	c.setSchema(rq.db.ID, rq.db.Schema)
	for _, r := range page {
		c.addRow(r)
	}
	return out, s.finishPage(ctx, rq, c, page, out)
}

// queryRowsGo reads every matching row the audience can open, then filters,
// sorts, groups and pages in Go.
func (s *Service) queryRowsGo(ctx context.Context, rq rowQuery) (*RowPage, error) {
	all, c, err := s.matchingRows(ctx, rq)
	if err != nil {
		return nil, err
	}
	schema := rq.db.Schema
	sorted := make([]sortedRow, len(all))
	for i, r := range all {
		keys := make([]fValue, len(rq.sorts))
		for j, srt := range rq.sorts {
			keys[j] = c.sortValue(rq.db.ID, schema, r, srt.Property)
		}
		sorted[i] = sortedRow{r: r, keys: keys}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return lessRows(sorted[i], sorted[j], rq.sorts) < 0 })
	start := 0
	if rq.cursor != "" {
		var cur goCursor
		if err := httpx.DecodeCursor(rq.cursor, &cur); err != nil || !cur.Go || len(cur.K) != len(rq.sorts) {
			return nil, httpx.BadRequest("The view changed. Reload it.")
		}
		mark := sortedRow{r: &crow{ID: cur.ID, Position: cur.P}, keys: make([]fValue, len(cur.K))}
		for i, k := range cur.K {
			mark.keys[i] = fromCursorKey(k)
		}
		start = sort.Search(len(sorted), func(i int) bool { return lessRows(sorted[i], mark, rq.sorts) > 0 })
	}
	out := &RowPage{Items: []Row{}}
	end := min(start+rq.limit, len(sorted))
	var page []*crow
	for _, sr := range sorted[start:end] {
		page = append(page, sr.r)
	}
	if end < len(sorted) && end > start {
		last := sorted[end-1]
		cur := goCursor{Go: true, P: last.r.Position, ID: last.r.ID}
		for _, k := range last.keys {
			cur.K = append(cur.K, toCursorKey(k))
		}
		out.NextCursor = httpx.EncodeCursor(cur)
	}
	if rq.cursor == "" && !rq.noMeta {
		n := len(all)
		out.Total = &n
		if rq.view != nil {
			cfg := rq.view.Config
			if cfg.GroupBy != "" {
				out.Groups = c.groupInfos(rq.db.ID, schema, all, cfg.GroupBy, cfg.Group)
			}
			if cfg.SubGroupBy != "" {
				out.SubGroups = c.groupInfos(rq.db.ID, schema, all, cfg.SubGroupBy, cfg.SubGroup)
			}
			if len(cfg.Calculations) > 0 {
				out.Calculations = c.calculations(rq.db.ID, schema, all, cfg.Calculations)
			}
		}
	}
	return out, s.finishPage(ctx, rq, c, page, out)
}

// matchingRows loads the rows the audience can open that match the query's
// filters (SQL narrows by what it can express, Go checks the whole tree).
func (s *Service) matchingRows(ctx context.Context, rq rowQuery) ([]*crow, *computer, error) {
	b := &sqlb{}
	where, level := rq.baseWhere(b)
	v := rq.viewer()
	if rq.conj != "or" {
		for _, f := range rq.filters {
			sub := &sqlb{args: append([]any(nil), b.args...)}
			if cond, ok := filterSQL(sub, rq.db.Schema, f, v); ok && cond != "" {
				b.args = sub.args
				where = append(where, "("+cond+")")
			}
		}
	}
	sql := fmt.Sprintf(`SELECT p.id, p.title, p.icon, p.cover, p.props, p.position, p.created_by, p.updated_by, p.created_at, p.updated_at, %s
		FROM docs_pages p WHERE %s ORDER BY p.position, p.id LIMIT %d`, level, strings.Join(where, " AND "), maxRows+1)
	rows, err := s.d.Pool.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	c := s.newComputer(ctx, v)
	c.setSchema(rq.db.ID, rq.db.Schema)
	var all []*crow
	for rows.Next() {
		r := &crow{DB: rq.db.ID}
		var props, cover []byte
		var lvl int
		if err := rows.Scan(&r.ID, &r.Title, &r.Icon, &cover, &props, &r.Position, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &lvl); err != nil {
			return nil, nil, err
		}
		if len(cover) > 0 {
			_ = json.Unmarshal(cover, &r.Cover)
		}
		r.Props = map[string]any{}
		_ = json.Unmarshal(props, &r.Props)
		r.Level = access.Level(lvl)
		if rq.public {
			r.Level = access.View
		}
		all = append(all, r)
		c.addRow(r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()
	if len(rq.filters) > 0 {
		root := Filter{Conj: rq.conj, Filters: rq.filters}
		kept := all[:0]
		for _, r := range all {
			if c.matches(rq.db.ID, rq.db.Schema, r, root) {
				kept = append(kept, r)
			}
		}
		all = kept
	}
	return all, c, c.err
}

// finishPage turns computed rows into the answer: computed values, group
// keys, sub-item counts and refs.
func (s *Service) finishPage(ctx context.Context, rq rowQuery, c *computer, page []*crow, out *RowPage) error {
	schema := rq.db.Schema
	out.Refs = Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	var people []uuid.UUID
	for _, r := range page {
		row := Row{ID: r.ID, Title: r.Title, Icon: r.Icon, Cover: r.Cover, Props: c.overlay(rq.db.ID, r), Position: r.Position,
			CreatedBy: r.CreatedBy, UpdatedBy: r.UpdatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, MyLevel: r.Level}
		if rq.public {
			row.MyLevel = access.View
			row.CreatedBy, row.UpdatedBy = nil, nil
		}
		if rq.view != nil && !rq.noMeta {
			cfg := rq.view.Config
			if cfg.GroupBy != "" {
				for _, k := range c.groupKeys(rq.db.ID, schema, r, cfg.GroupBy, cfg.Group) {
					row.Groups = append(row.Groups, k.key)
				}
			}
			if cfg.SubGroupBy != "" {
				for _, k := range c.groupKeys(rq.db.ID, schema, r, cfg.SubGroupBy, cfg.SubGroup) {
					row.SubGroups = append(row.SubGroups, k.key)
				}
			}
		}
		people = append(people, personValues(schema, r.Props)...)
		if !rq.public {
			for _, id := range []*uuid.UUID{r.CreatedBy, r.UpdatedBy} {
				if id != nil {
					people = append(people, *id)
				}
			}
		}
		out.Items = append(out.Items, row)
	}
	people = append(people, c.refsFor(rq.db.ID, page, &out.Refs)...)
	if rq.view != nil && (rq.view.Type == "gallery" || rq.view.Type == "feed") && len(page) > 0 {
		snips, err := s.snippets(ctx, page)
		if err != nil {
			return err
		}
		for i := range out.Items {
			out.Items[i].Snippet = snips[out.Items[i].ID]
		}
	}
	if si := rq.subItems(); si != nil && len(page) > 0 {
		counts, err := s.subCounts(ctx, rq, si, page)
		if err != nil {
			return err
		}
		for i := range out.Items {
			n := counts[out.Items[i].ID.String()]
			out.Items[i].SubCount = &n
		}
	}
	users, err := s.resolveRefs(ctx, rq.me, !rq.public, people, nil)
	if err != nil {
		return err
	}
	out.Refs.Users = users.Users
	return c.err
}

// snippets: the first text of each row page (top-level blocks in order), for
// gallery and feed cards. The rows are ones the audience can open.
func (s *Service) snippets(ctx context.Context, page []*crow) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, len(page))
	for i, r := range page {
		ids[i] = r.ID
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT ON (page_id) page_id, left(plain, 280) FROM docs_blocks
		WHERE page_id = ANY ($1) AND parent_id IS NULL AND plain <> '' ORDER BY page_id, position, id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

// subCounts counts each row's sub-items the audience can open.
func (s *Service) subCounts(ctx context.Context, rq rowQuery, si *SubItemsConfig, page []*crow) (map[string]int, error) {
	ids := make([]string, len(page))
	for i, r := range page {
		ids[i] = r.ID.String()
	}
	b := &sqlb{}
	cond := "true"
	if rq.public {
		cond = "EXISTS (SELECT 1 FROM access_resources ar WHERE ar.kind = 'docs.page' AND ar.id = c.id AND ar.inherits)"
	} else {
		cond = fmt.Sprintf("access_level(%s, 'docs.page', c.id, %s) >= 1", b.arg(rq.me), b.arg(rq.agent))
	}
	sql := fmt.Sprintf(`SELECT e, count(*) FROM docs_pages c, jsonb_array_elements_text(COALESCE(c.props->%s::text, '[]'::jsonb)) e
		WHERE c.parent_id = %s AND c.is_row AND NOT c.is_template AND c.deleted_at IS NULL AND e = ANY (%s) AND %s GROUP BY e`,
		b.arg(si.Property), b.arg(rq.db.ID), b.arg(ids), cond)
	rows, err := s.d.Pool.Query(ctx, sql, b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// ---- groups -----------------------------------------------------------------------

type groupKey struct {
	key, label, color string
	order             fValue // sorts groups
}

// groupKeys lists the groups a row falls in for property id (several for
// multi-selects, people and relations; one "" group when empty).
func (c *computer) groupKeys(dbID uuid.UUID, schema []Property, r *crow, id string, g *GroupConfig) []groupKey {
	p, ok := findProp(schema, id)
	if !ok {
		return nil
	}
	if g == nil {
		g = cleanGroup(nil, p)
	}
	none := []groupKey{{key: "", label: "No " + strings.ToLower(p.Name), order: vEmpty}}
	opt := func(oid string) (groupKey, bool) {
		for i, o := range p.Options {
			if o.ID == oid {
				return groupKey{key: o.ID, label: o.Name, color: o.Color, order: vNum(float64(i))}, true
			}
		}
		return groupKey{}, false
	}
	switch p.Type {
	case "select", "status":
		s, _ := r.Props[p.ID].(string)
		if k, ok := opt(s); ok {
			return []groupKey{k}
		}
		return none
	case "multi_select":
		var out []groupKey
		for _, oid := range fStringList(r.Props[p.ID]) {
			if k, ok := opt(oid); ok {
				out = append(out, k)
			}
		}
		if len(out) == 0 {
			return none
		}
		return out
	case "checkbox":
		if b, _ := r.Props[p.ID].(bool); b {
			return []groupKey{{key: "true", label: "Checked", order: vNum(1)}}
		}
		return []groupKey{{key: "false", label: "Unchecked", order: vNum(0)}}
	}
	v := c.Value(dbID, r.ID, p)
	items := []fValue{v}
	if v.Kind == fList {
		items = leaves(v.List)
	}
	var out []groupKey
	for _, x := range items {
		switch x.Kind {
		case fPerson:
			name := c.PersonName(x.ID)
			out = append(out, groupKey{key: x.ID.String(), label: name, order: vText(strings.ToLower(name))})
		case fPage:
			title := "Untitled"
			if rr, ok := c.rows[x.ID]; ok {
				title = displayTitle(rr.Title)
			}
			out = append(out, groupKey{key: x.ID.String(), label: title, order: vText(strings.ToLower(title))})
		case fBool:
			if x.Bool {
				out = append(out, groupKey{key: "true", label: "Checked", order: vNum(1)})
			} else {
				out = append(out, groupKey{key: "false", label: "Unchecked", order: vNum(0)})
			}
		case fNumber:
			step := g.Step
			if step <= 0 {
				step = 10
			}
			lo := math.Floor(x.Num/step) * step
			out = append(out, groupKey{key: "n:" + fFmtNum(lo), label: fFmtNum(lo) + " – " + fFmtNum(lo+step), order: vNum(lo)})
		case fDate:
			out = append(out, c.dateGroup(x.Date, g.By))
		case fText:
			out = append(out, groupKey{key: "t:" + x.Str, label: x.Str, order: vText(strings.ToLower(x.Str))})
		}
	}
	if len(out) == 0 {
		return none
	}
	return out
}

// dateGroup buckets a date: relative (today, yesterday, tomorrow, the last
// or next 7 and 30 days, then months), day, week, month or year.
func (c *computer) dateGroup(d fDateValue, by string) groupKey {
	t := d.Start
	if d.HasTime {
		t = t.In(c.v.loc)
	}
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	now := c.now
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	month := groupKey{key: "m:" + day.Format("2006-01"), label: day.Format("January 2006"), order: vNum(float64(day.Year()*12 + int(day.Month())))}
	switch by {
	case "day":
		return groupKey{key: "d:" + day.Format("2006-01-02"), label: day.Format("Jan 2, 2006"), order: vNum(float64(day.Unix()))}
	case "week":
		wd := (int(day.Weekday()) + 6) % 7
		start := day.AddDate(0, 0, -wd)
		return groupKey{key: "w:" + start.Format("2006-01-02"), label: "Week of " + start.Format("Jan 2, 2006"), order: vNum(float64(start.Unix()))}
	case "month":
		return month
	case "year":
		return groupKey{key: "y:" + strconv.Itoa(day.Year()), label: strconv.Itoa(day.Year()), order: vNum(float64(day.Year() * 12))}
	}
	diff := int(math.Round(day.Sub(today).Hours() / 24))
	rel := func(key, label string, rank int) groupKey {
		return groupKey{key: "r:" + key, label: label, order: vNum(float64(today.Year()*12+int(today.Month())) + float64(rank)/100)}
	}
	switch {
	case diff == 0:
		return rel("today", "Today", 0)
	case diff == -1:
		return rel("yesterday", "Yesterday", -1)
	case diff == 1:
		return rel("tomorrow", "Tomorrow", 1)
	case diff < 0 && diff >= -7:
		return rel("past_7", "Last 7 days", -2)
	case diff > 0 && diff <= 7:
		return rel("next_7", "Next 7 days", 2)
	case diff < 0 && diff >= -30:
		return rel("past_30", "Last 30 days", -3)
	case diff > 0 && diff <= 30:
		return rel("next_30", "Next 30 days", 3)
	}
	return month
}

// groupInfos lists a view's groups with counts, in order: option order (or
// asc/desc, or a custom order), "no value" first; options without rows show
// unless empty groups are hidden.
func (c *computer) groupInfos(dbID uuid.UUID, schema []Property, rows []*crow, id string, g *GroupConfig) []GroupInfo {
	p, ok := findProp(schema, id)
	if !ok {
		return nil
	}
	if g == nil {
		g = cleanGroup(nil, p)
	}
	type acc struct {
		k groupKey
		n int
	}
	byKey := map[string]*acc{}
	var keys []string
	add := func(k groupKey, n int) {
		if a, ok := byKey[k.key]; ok {
			a.n += n
			return
		}
		byKey[k.key] = &acc{k: k, n: n}
		keys = append(keys, k.key)
	}
	add(groupKey{key: "", label: "No " + strings.ToLower(p.Name), order: vEmpty}, 0)
	switch p.Type {
	case "select", "status", "multi_select":
		for i, o := range p.Options {
			add(groupKey{key: o.ID, label: o.Name, color: o.Color, order: vNum(float64(i))}, 0)
		}
	case "checkbox":
		add(groupKey{key: "false", label: "Unchecked", order: vNum(0)}, 0)
		add(groupKey{key: "true", label: "Checked", order: vNum(1)}, 0)
	}
	for _, r := range rows {
		for _, k := range c.groupKeys(dbID, schema, r, id, g) {
			add(k, 1)
		}
	}
	sort.SliceStable(keys, func(i, j int) bool {
		a, b := byKey[keys[i]].k, byKey[keys[j]].k
		if (a.key == "") != (b.key == "") {
			return a.key == ""
		}
		cmp := fCompare(a.order, b.order)
		if g.Sort == "desc" {
			cmp = -cmp
		}
		return cmp < 0
	})
	if len(g.Order) > 0 {
		rank := map[string]int{}
		for i, k := range g.Order {
			rank[k] = i
		}
		sort.SliceStable(keys, func(i, j int) bool {
			ri, oki := rank[keys[i]]
			rj, okj := rank[keys[j]]
			if oki && okj {
				return ri < rj
			}
			return oki && !okj
		})
	}
	out := []GroupInfo{}
	for _, k := range keys {
		a := byKey[k]
		if a.n == 0 && (g.HideEmpty || (k == "" && p.Type != "select" && p.Type != "status")) {
			continue
		}
		out = append(out, GroupInfo{Key: k, Label: a.k.label, Color: a.k.color, Count: a.n, Hidden: slices.Contains(g.Hidden, k)})
	}
	return out
}

// calculations works out footer calculations over every matching row.
func (c *computer) calculations(dbID uuid.UUID, schema []Property, rows []*crow, calcs map[string]string) map[string]any {
	out := map[string]any{}
	for id, fn := range calcs {
		p, ok := findProp(schema, id)
		if !ok {
			continue
		}
		vals := make([]fValue, len(rows))
		for i, r := range rows {
			vals[i] = c.Value(dbID, r.ID, p)
		}
		if v := fValueJSON(aggregate(fn, vals)); v != nil {
			out[id] = v
		}
	}
	return out
}

// ---- requests -----------------------------------------------------------------------

// rowsRequest reads ?view=&cursor=&limit=&q=&tz=&parent= and the optional
// ad-hoc filters/filter_conj/sorts (JSON) that override the view's. views
// are the database's own; a linked view id is looked up by the caller.
func rowsRequest(r *http.Request, dbp *Page, views []View) (rowQuery, error) {
	q := r.URL.Query()
	rq := rowQuery{db: dbp, cursor: q.Get("cursor"), search: q.Get("q"), limit: 100, tz: q.Get("tz"), token: chi.URLParam(r, "token")}
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 {
		rq.limit = min(v, 1000)
	}
	if vid := q.Get("view"); vid != "" {
		found := false
		for i, v := range views {
			if v.ID.String() == vid {
				rq.filters, rq.conj, rq.sorts, rq.view, found = v.Config.Filters, v.Config.FilterConj, v.Config.Sorts, &views[i], true
			}
		}
		if !found {
			return rq, httpx.NotFound("View not found.")
		}
	}
	if f := q.Get("filters"); f != "" {
		var list []Filter
		if err := json.Unmarshal([]byte(f), &list); err != nil {
			return rq, httpx.BadRequest("filters must be a JSON list of {property, op, value} or {conj, filters}.")
		}
		n := 0
		rq.filters = cleanFilters(dbp.Schema, list, 1, &n)
		rq.conj = q.Get("filter_conj")
	}
	if srt := q.Get("sorts"); srt != "" {
		if err := json.Unmarshal([]byte(srt), &rq.sorts); err != nil || len(rq.sorts) > 5 {
			return rq, httpx.BadRequest("sorts must be a JSON list of {property, direction}.")
		}
	}
	if p := q.Get("parent"); p != "" {
		id, err := uuid.Parse(p)
		if err != nil {
			return rq, httpx.BadRequest("parent must be a row id.")
		}
		rq.parent = &id
	}
	return rq, nil
}

// viewsFor returns the database's views plus the linked view a request names
// (a linked view's database is checked by the caller already).
func (s *Service) viewsFor(ctx context.Context, dbID uuid.UUID, viewID string) ([]View, error) {
	views, err := loadViews(ctx, s.d.Pool, dbID)
	if err != nil || viewID == "" {
		return views, err
	}
	if slices.ContainsFunc(views, func(v View) bool { return v.ID.String() == viewID }) {
		return views, nil
	}
	id, err := uuid.Parse(viewID)
	if err != nil {
		return views, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+viewCols+` FROM docs_views WHERE id = $1 AND database_id = $2`, id, dbID)
	if err != nil {
		return nil, err
	}
	more, err := scanViews(rows)
	if err != nil {
		return nil, err
	}
	return append(views, more...), nil
}

// listRows: GET /docs/databases/{id}/rows?view=&filters=&filter_conj=&sorts=&q=&tz=&parent=&cursor=&limit=
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
	views, err := s.viewsFor(ctx, id, r.URL.Query().Get("view"))
	if err != nil {
		return err
	}
	rq, err := rowsRequest(r, dbp, views)
	if err != nil {
		return err
	}
	rq.me, rq.agent, rq.token = me, access.ViaAgent(ctx), ""
	out, err := s.queryRows(ctx, rq)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
