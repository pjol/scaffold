package docs

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Database logic that needs no database: aggregations, filters (Go and
// SQL), relation/rollup visibility in the computer, schema and view
// cleaning, CSV cells, schedules, groups, cursors.

func num(n float64) fValue { return vNum(n) }

func TestAggregate(t *testing.T) {
	day := func(s string) fValue {
		d, _ := time.Parse("2006-01-02", s)
		return fValue{Kind: fDate, Date: fDateValue{Start: d}}
	}
	vals := []fValue{num(3), num(1), vEmpty, num(2), vList([]fValue{num(4)})}
	cases := []struct {
		fn   string
		in   []fValue
		want any
	}{
		{"count_all", vals, 5.0},
		{"count_values", vals, 4.0},
		{"count_empty", vals, 1.0},
		{"count_not_empty", vals, 4.0},
		{"percent_empty", vals, 20.0},
		{"percent_not_empty", vals, 80.0},
		{"sum", vals, 10.0},
		{"average", vals, 2.5},
		{"median", vals, 2.5},
		{"min", vals, 1.0},
		{"max", vals, 4.0},
		{"range", vals, 3.0},
		{"count_unique_values", []fValue{vText("a"), vText("a"), vText("b")}, 2.0},
		{"sum", nil, 0.0},
		{"average", nil, nil},
		{"percent_empty", nil, nil},
		{"checked", []fValue{vBool(true), vBool(false), vBool(true)}, 2.0},
		{"unchecked", []fValue{vBool(true), vBool(false), vBool(true)}, 1.0},
		{"percent_checked", []fValue{vBool(true), vBool(false), vBool(true)}, 66.67},
		{"date_range", []fValue{day("2026-01-01"), day("2026-01-11"), vEmpty}, 10.0},
	}
	for _, c := range cases {
		got := fValueJSON(aggregate(c.fn, c.in))
		if (got == nil) != (c.want == nil) || (got != nil && math.Abs(got.(float64)-c.want.(float64)) > 0.001) {
			t.Errorf("%s: got %v, want %v", c.fn, got, c.want)
		}
	}
	if got := fValueJSON(aggregate("earliest_date", []fValue{day("2026-03-01"), day("2026-01-05")})); got.(map[string]any)["start"] != "2026-01-05" {
		t.Errorf("earliest_date: %v", got)
	}
	uniq := aggregate("show_unique", []fValue{vList([]fValue{vText("x"), vText("y")}), vText("x")})
	if len(uniq.List) != 2 {
		t.Errorf("show_unique: %v", fValueJSON(uniq))
	}
}

func TestRollupTypes(t *testing.T) {
	if got := rollupType("sum", tNumber).String(); got != "number" {
		t.Errorf("sum type %s", got)
	}
	if got := rollupType("show_original", tList(tText)).String(); got != "list:text" {
		t.Errorf("show_original of a list: %s", got)
	}
	if got := rollupType("latest_date", tDate).String(); got != "date" {
		t.Errorf("latest_date: %s", got)
	}
	if rollupAllowed("sum", tText) || !rollupAllowed("sum", tList(tNumber)) || rollupAllowed("checked", tNumber) || !rollupAllowed("count_all", tPage) {
		t.Error("rollupAllowed rules")
	}
}

// testComputer is a computer whose rows and schemas are preloaded (no
// database calls are made for these values).
func testComputer(schemas map[uuid.UUID][]Property) *computer {
	c := (&Service{}).newComputer(context.Background(), viewer{me: uuid.New(), loc: time.UTC})
	for id, s := range schemas {
		c.setSchema(id, s)
	}
	return c
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestGoFilters(t *testing.T) {
	db := uuid.New()
	schema := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "price", Name: "Price", Type: "number", NumberFormat: "usd"},
		{ID: "stage", Name: "Stage", Type: "select", Options: []Option{{ID: "a", Name: "Lead"}, {ID: "b", Name: "Won"}}},
		{ID: "tags", Name: "Tags", Type: "multi_select", Options: []Option{{ID: "x", Name: "X"}, {ID: "y", Name: "Y"}}},
		{ID: "done", Name: "Done", Type: "checkbox"},
		{ID: "due", Name: "Due", Type: "date"},
		{ID: "double", Name: "Double", Type: "formula", Formula: &FormulaConfig{Expression: `prop("Price") * 2`, Type: "number"}},
		{ID: "label", Name: "Label", Type: "formula", Formula: &FormulaConfig{Expression: `prop("Name") + "!"`, Type: "text"}},
	}
	c := testComputer(map[uuid.UUID][]Property{db: schema})
	today := time.Now().UTC().Format("2006-01-02")
	r := &crow{ID: uuid.New(), DB: db, Title: "Acme Deal", Props: map[string]any{
		"price": 1250.0, "stage": "b", "tags": []any{"x"}, "done": true, "due": map[string]any{"start": today},
	}}
	c.addRow(r)
	empty := &crow{ID: uuid.New(), DB: db, Title: "", Props: map[string]any{}}
	c.addRow(empty)
	cond := func(prop, op string, v any) Filter { return Filter{Property: prop, Op: op, Value: raw(v)} }
	cases := []struct {
		name string
		f    Filter
		row  *crow
		want bool
	}{
		{"title contains (case)", cond("title", "contains", "acme"), r, true},
		{"title ends_with", cond("title", "ends_with", "DEAL"), r, true},
		{"title is not", cond("title", "is_not", "acme deal"), r, false},
		{"price gt in major units", cond("price", "gt", 12), r, true},
		{"price eq major", cond("price", "eq", 12.5), r, true},
		{"price neq on empty", cond("price", "neq", 5), empty, true},
		{"price gt on empty", cond("price", "gt", 5), empty, false},
		{"select is", cond("stage", "is", "b"), r, true},
		{"select is_not", cond("stage", "is_not", "b"), r, false},
		{"select is_empty", cond("stage", "is_empty", nil), empty, true},
		{"multi contains", cond("tags", "contains", "x"), r, true},
		{"multi not_contains", cond("tags", "not_contains", "y"), r, true},
		{"checkbox", cond("done", "is", false), r, false},
		{"checkbox on empty", cond("done", "is", false), empty, true},
		{"date is today", cond("due", "is", "today"), r, true},
		{"date before tomorrow", cond("due", "before", "tomorrow"), r, true},
		{"date within this week", cond("due", "is_within", "this_week"), r, true},
		{"date within past year", cond("due", "is_within", "past_year"), r, true},
		{"date is_empty", cond("due", "is_empty", nil), empty, true},
		{"formula number", cond("double", "eq", 25), r, true},
		{"formula text", cond("label", "is", "acme deal!"), r, true},
		{"incomplete narrows nothing", cond("title", "contains", ""), r, true},
		{"or group", Filter{Conj: "or", Filters: []Filter{cond("stage", "is", "a"), cond("done", "is", true)}}, r, true},
		{"and group", Filter{Conj: "and", Filters: []Filter{cond("stage", "is", "a"), cond("done", "is", true)}}, r, false},
		{"nested", Filter{Conj: "and", Filters: []Filter{cond("done", "is", true), {Conj: "or", Filters: []Filter{cond("stage", "is", "a"), cond("price", "lt", 100)}}}}, r, true},
		{"empty group", Filter{Conj: "or", Filters: []Filter{}}, r, true},
	}
	for _, tc := range cases {
		if got := c.matches(db, schema, tc.row, tc.f); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSQLFilters(t *testing.T) {
	schema := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "price", Name: "Price", Type: "number", NumberFormat: "usd"},
		{ID: "owner", Name: "Owner", Type: "person"},
		{ID: "due", Name: "Due", Type: "date"},
		{ID: "f", Name: "F", Type: "formula", Formula: &FormulaConfig{Expression: "1", Type: "number"}},
	}
	me := uuid.New()
	v := viewer{me: me, loc: time.UTC}
	b := &sqlb{}
	cond, ok := filterSQL(b, schema, Filter{Property: "price", Op: "gte", Value: raw(12.5)}, v)
	if !ok || !strings.Contains(cond, ">=") || b.args[len(b.args)-1] != 1250.0 {
		t.Errorf("currency filter: %q %v", cond, b.args)
	}
	b = &sqlb{}
	cond, _ = filterSQL(b, schema, Filter{Property: "owner", Op: "contains", Value: raw("me")}, v)
	if !strings.Contains(cond, "?") || b.args[len(b.args)-1] != me.String() {
		t.Errorf("me: %q %v", cond, b.args)
	}
	b = &sqlb{}
	cond, ok = filterSQL(b, schema, Filter{Conj: "or", Filters: []Filter{
		{Property: "title", Op: "contains", Value: raw("a")}, {Property: "due", Op: "is_within", Value: raw("this_month")}}}, v)
	if !ok || !strings.Contains(cond, " OR ") || !strings.Contains(cond, "BETWEEN") {
		t.Errorf("or group: %q", cond)
	}
	if _, ok := filterSQL(&sqlb{}, schema, Filter{Property: "f", Op: "eq", Value: raw(1)}, v); ok {
		t.Error("formula filters must not be SQL")
	}
	if _, ok := filterSQL(&sqlb{}, schema, Filter{Conj: "and", Filters: []Filter{{Property: "title", Op: "is_empty"}, {Property: "f", Op: "eq", Value: raw(1)}}}, v); ok {
		t.Error("a group with a formula must not be SQL")
	}
	for _, op := range []string{"is", "is_not", "contains", "starts_with"} {
		b = &sqlb{}
		cond, _ := filterSQL(b, schema, Filter{Property: "title", Op: op, Value: raw("Acme")}, v)
		if n := strings.Count(cond, "$"); n != len(b.args) {
			t.Errorf("title %s: %d placeholders, %d args (%q)", op, n, len(b.args), cond)
		}
	}
	b = &sqlb{}
	filterSQL(b, schema, Filter{Property: "price", Op: "eq", Value: raw(19.99)}, v)
	if b.args[len(b.args)-1] != 1999.0 {
		t.Errorf("currency rounding: %v", b.args)
	}
	b = &sqlb{}
	if cond, ok := filterSQL(b, schema, Filter{Property: "title", Op: "contains", Value: raw("")}, v); !ok || cond != "" || len(b.args) != 0 {
		t.Errorf("incomplete: %q %v", cond, b.args)
	}
	rq := rowQuery{db: &Page{ID: uuid.New(), Schema: schema}, filters: []Filter{{Property: "f", Op: "eq", Value: raw(1)}}}
	if !rq.needsGo() {
		t.Error("formula filter needs the Go path")
	}
	rq = rowQuery{db: &Page{ID: uuid.New(), Schema: schema}, filters: []Filter{{Property: "price", Op: "eq", Value: raw(1)}}, sorts: []Sort{{Property: "price"}}}
	if rq.needsGo() {
		t.Error("stored filters and sorts stay in SQL")
	}
}

// TestRelationVisibility: rollups, formulas and titles see only related
// rows the viewer can open.
func TestRelationVisibility(t *testing.T) {
	projects, tasks := uuid.New(), uuid.New()
	taskSchema := []Property{{ID: "title", Name: "Name", Type: "title"}, {ID: "hours", Name: "Hours", Type: "number"}, {ID: "done", Name: "Done", Type: "checkbox"}}
	open1, open2, hidden := uuid.New(), uuid.New(), uuid.New()
	projSchema := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "tasks", Name: "Tasks", Type: "relation", Relation: &RelationConfig{DatabaseID: tasks}},
		{ID: "n", Name: "Count", Type: "rollup", Rollup: &RollupConfig{Relation: "tasks", Property: "hours", Function: "count_all", Type: "number"}},
		{ID: "sum", Name: "Total", Type: "rollup", Rollup: &RollupConfig{Relation: "tasks", Property: "hours", Function: "sum", Type: "number"}},
		{ID: "names", Name: "Names", Type: "rollup", Rollup: &RollupConfig{Relation: "tasks", Property: "title", Function: "show_original", Type: "list:text"}},
		{ID: "open", Name: "Open hours", Type: "formula", Formula: &FormulaConfig{Expression: `prop("Tasks").filter(not current.prop("Done")).map(current.prop("Hours")).sum()`, Type: "number"}},
	}
	c := testComputer(map[uuid.UUID][]Property{projects: projSchema, tasks: taskSchema})
	p := &crow{ID: uuid.New(), DB: projects, Title: "Launch", Props: map[string]any{"tasks": []any{open1.String(), hidden.String(), open2.String()}}}
	c.addRow(p)
	c.addRow(&crow{ID: open1, DB: tasks, Title: "Design", Props: map[string]any{"hours": 3.0, "done": true}})
	c.addRow(&crow{ID: open2, DB: tasks, Title: "Build", Props: map[string]any{"hours": 5.0}})
	// The hidden task exists but the viewer can't open it: loadRows would mark it so.
	c.noAccess[hidden] = true
	c.relDone[relKey{projects, "tasks"}] = map[uuid.UUID]bool{p.ID: true}
	if got := fValueJSON(c.Value(projects, p.ID, projSchema[2])); got != 2.0 {
		t.Errorf("count_all counted a row the viewer can't open: %v", got)
	}
	if got := fValueJSON(c.Value(projects, p.ID, projSchema[3])); got != 8.0 {
		t.Errorf("sum: %v", got)
	}
	names := fValueText(c.Value(projects, p.ID, projSchema[4]), c)
	if names != "Design, Build" {
		t.Errorf("show_original: %q", names)
	}
	if got := fValueJSON(c.Value(projects, p.ID, projSchema[5])); got != 5.0 {
		t.Errorf("formula over the relation: %v", got)
	}
	refs := Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	c.refsFor(projects, []*crow{p}, &refs)
	if ref, ok := refs.Pages[hidden.String()]; !ok || ref.Title != nil {
		t.Errorf("a hidden related row is listed without a title: %+v", ref)
	}
	if ref := refs.Pages[open1.String()]; ref.Title == nil || *ref.Title != "Design" {
		t.Errorf("visible related row title: %+v", ref)
	}
	props := c.overlay(projects, p)
	if props["n"] != 2.0 || props["sum"] != 8.0 {
		t.Errorf("overlay: %v", props)
	}
	if _, stored := p.Props["n"]; stored {
		t.Error("overlay changed the stored props")
	}
}

func TestValidateSchemaTypes(t *testing.T) {
	in := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{Name: "Price", Type: "number", NumberFormat: "dollar", NumberDisplay: "bar"},
		{Name: "ID", Type: "unique_id", UniqueID: &UniqueIDConfig{Prefix: "task"}},
		{Name: "Go", Type: "button", Button: &ButtonConfig{Actions: []ButtonAction{{Type: "open_url", URL: "example.com"}}}},
		{Name: "Tags", Type: "multi_select", Options: []Option{{Name: "A", Color: "purple"}, {Name: "B", Color: "chartreuse"}}},
	}
	out, err := validateSchema(in)
	if err != nil {
		t.Fatal(err)
	}
	if out[1].NumberFormat != "usd" || out[1].NumberMax == nil || *out[1].NumberMax != 100 {
		t.Errorf("number settings: %+v", out[1])
	}
	if out[2].UniqueID.Prefix != "TASK" {
		t.Errorf("prefix: %+v", out[2].UniqueID)
	}
	if out[3].Button.Label != "Go" || out[3].Button.Actions[0].URL != "https://example.com" {
		t.Errorf("button: %+v", out[3].Button)
	}
	if out[4].Options[0].Color != "purple" || out[4].Options[1].Color != "default" {
		t.Errorf("option colours: %+v", out[4].Options)
	}
	bad := [][]Property{
		{{ID: "title", Name: "Name", Type: "title"}, {Name: "name", Type: "text"}},
		{{ID: "title", Name: "Name", Type: "title"}, {Name: "R", Type: "relation"}},
		{{ID: "title", Name: "Name", Type: "title"}, {Name: "ID", Type: "unique_id", UniqueID: &UniqueIDConfig{Prefix: "TOO-LONG-PREFIX"}}},
		{{ID: "title", Name: "Name", Type: "title"}, {Name: "B", Type: "button", Button: &ButtonConfig{Actions: []ButtonAction{{Type: "explode"}}}}},
	}
	for i, b := range bad {
		if _, err := validateSchema(b); err == nil {
			t.Errorf("case %d should be refused", i)
		}
	}
}

func TestCleanConfig(t *testing.T) {
	db := uuid.New()
	schema := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "n", Name: "N", Type: "number"},
		{ID: "d", Name: "D", Type: "date"},
		{ID: "s", Name: "S", Type: "status", Options: []Option{{ID: "o1", Name: "A"}}},
		{ID: "p", Name: "Parent", Type: "relation", Relation: &RelationConfig{DatabaseID: db}},
		{ID: "o", Name: "Other", Type: "relation", Relation: &RelationConfig{DatabaseID: uuid.New()}},
		{ID: "pe", Name: "Who", Type: "person"},
	}
	cfg := cleanConfigFor(schema, db, "form", ViewConfig{
		Calculations: map[string]string{"n": "sum", "title": "sum", "d": "earliest_date", "s": "count_all"},
		SubItems:     &SubItemsConfig{Property: "o", Display: "nested"},
		Form:         &FormConfig{Questions: []FormQuestion{{Property: "title", Required: true}, {Property: "pe"}, {Property: "n"}, {Property: "n"}}},
	})
	if cfg.Calculations["n"] != "sum" || cfg.Calculations["d"] != "earliest_date" || cfg.Calculations["s"] != "count_all" || cfg.Calculations["title"] != "" {
		t.Errorf("calculations: %v", cfg.Calculations)
	}
	if cfg.SubItems != nil {
		t.Error("sub-items need a self relation")
	}
	if len(cfg.Form.Questions) != 2 {
		t.Errorf("form questions (people and duplicates dropped): %+v", cfg.Form.Questions)
	}
	tl := cleanConfigFor(schema, db, "timeline", ViewConfig{Timeline: &TimelineConfig{Zoom: "eons", Dependency: "p"}})
	if tl.DateProperty != "d" || tl.Timeline.Zoom != "month" || tl.Timeline.Dependency != "p" {
		t.Errorf("timeline: %+v %+v", tl.DateProperty, tl.Timeline)
	}
	ch := cleanConfigFor(schema, db, "chart", ViewConfig{Chart: &ChartConfig{Type: "donut", X: "d", Y: "sum", YProperty: "n", Series: "s"}})
	if ch.Chart.XBy != "month" || ch.Chart.Y != "sum" || ch.Chart.Series != "" {
		t.Errorf("chart: %+v", ch.Chart)
	}
	board := cleanConfigFor(schema, db, "board", ViewConfig{})
	if board.GroupBy != "s" || board.Group == nil {
		t.Errorf("board group: %+v", board)
	}
	n := 0
	deep := cleanFilters(schema, []Filter{{Conj: "or", Filters: []Filter{{Conj: "and", Filters: []Filter{{Conj: "or", Filters: []Filter{{Property: "n", Op: "eq"}}}}}}}}, 1, &n)
	if len(deep[0].Filters[0].Filters) != 0 {
		t.Errorf("groups nest three deep: %+v", deep)
	}
}

func TestCSVCells(t *testing.T) {
	usd := Property{Type: "number", NumberFormat: "usd"}
	if v := csvCellValue(usd, "$1,234.56"); v != 123456.0 {
		t.Errorf("currency: %v", v)
	}
	if v := csvCellValue(Property{Type: "number", NumberFormat: "percent"}, "25%"); v != 25.0 {
		t.Errorf("percent: %v", v)
	}
	ms := Property{Type: "multi_select", Options: []Option{{ID: "a", Name: "Red"}, {ID: "b", Name: "Blue"}}}
	if v, _ := csvCellValue(ms, "red, Blue, Green").([]string); len(v) != 2 {
		t.Errorf("multi-select: %v", v)
	}
	if v := csvCellValue(Property{Type: "url"}, "example.com/x"); v != "https://example.com/x" {
		t.Errorf("url: %v", v)
	}
	if v := csvCellValue(Property{Type: "phone"}, "+1 (415) 555-0100"); v == nil {
		t.Error("phone")
	}
	if v := csvCellValue(Property{Type: "phone"}, "call me"); v != nil {
		t.Errorf("bad phone: %v", v)
	}
	if v := csvCellValue(Property{Type: "checkbox"}, "Yes"); v != true {
		t.Errorf("checkbox: %v", v)
	}
}

func TestSchedules(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	from := time.Date(2026, 11, 4, 10, 0, 0, 0, ny) // a Wednesday
	if got := nextRepeat(Repeat{Every: "day", Interval: 1, Time: "09:00", TZ: "America/New_York"}, from); !got.Equal(time.Date(2026, 11, 5, 9, 0, 0, 0, ny)) {
		t.Errorf("daily: %v", got)
	}
	if got := nextRepeat(Repeat{Every: "week", Interval: 1, Weekdays: []int{0, 4}, Time: "08:30", TZ: "America/New_York"}, from); !got.Equal(time.Date(2026, 11, 6, 8, 30, 0, 0, ny)) {
		t.Errorf("weekly: %v", got)
	}
	if got := nextRepeat(Repeat{Every: "month", Interval: 1, Day: 1, Time: "09:00", TZ: "America/New_York"}, from); !got.Equal(time.Date(2026, 12, 1, 9, 0, 0, 0, ny)) {
		t.Errorf("monthly: %v", got)
	}
	at, label, ok := reminderAt(map[string]any{"start": "2026-11-20", "remind": "1d", "tz": "America/New_York"})
	if !ok || !at.Equal(time.Date(2026, 11, 19, 9, 0, 0, 0, ny)) || label != "2026-11-20" {
		t.Errorf("reminder: %v %q %v", at, label, ok)
	}
	if _, err := cleanRepeat(Repeat{Every: "week", Time: "09:00", TZ: "UTC"}); err == nil {
		t.Error("weekly needs weekdays")
	}
}

func TestComputedCycles(t *testing.T) {
	db := uuid.New()
	schema := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "a", Name: "A", Type: "formula", Formula: &FormulaConfig{Expression: `prop("B") + 1`}},
		{ID: "b", Name: "B", Type: "formula", Formula: &FormulaConfig{Expression: `prop('a') * 2`}},
	}
	if err := computedCycles(schema, db); err == nil || !strings.Contains(err.Error(), "use each other") {
		t.Errorf("cycle not found: %v", err)
	}
	schema[2].Formula.Expression = `prop("Name").length()`
	if err := computedCycles(schema, db); err != nil {
		t.Errorf("no cycle: %v", err)
	}
	if got := formulaRefNames(`prop("X") + current.prop("Y") + prop('Z')`); len(got) != 2 || got[0] != "X" || got[1] != "Z" {
		t.Errorf("refs: %v", got)
	}
}

func TestGroupsAndCursor(t *testing.T) {
	db := uuid.New()
	schema := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "s", Name: "Status", Type: "status", Options: []Option{{ID: "t", Name: "To do", Color: "gray"}, {ID: "d", Name: "Done", Color: "green"}}},
		{ID: "n", Name: "N", Type: "number"},
	}
	c := testComputer(map[uuid.UUID][]Property{db: schema})
	rows := []*crow{
		{ID: uuid.New(), DB: db, Props: map[string]any{"s": "d", "n": 12.0}},
		{ID: uuid.New(), DB: db, Props: map[string]any{"s": "d", "n": 3.0}},
		{ID: uuid.New(), DB: db, Props: map[string]any{}},
	}
	for _, r := range rows {
		c.addRow(r)
	}
	g := c.groupInfos(db, schema, rows, "s", nil)
	if len(g) != 3 || g[0].Key != "" || g[1].Key != "t" || g[2].Key != "d" || g[2].Count != 2 || g[1].Count != 0 {
		t.Errorf("status groups: %+v", g)
	}
	g = c.groupInfos(db, schema, rows, "s", &GroupConfig{HideEmpty: true})
	if len(g) != 2 {
		t.Errorf("hide empty: %+v", g)
	}
	nb := c.groupInfos(db, schema, rows, "n", &GroupConfig{Step: 10})
	if len(nb) != 3 || nb[1].Key != "n:0" || nb[2].Key != "n:10" {
		t.Errorf("number buckets: %+v", nb)
	}
	// Empty values sort last both ways; the cursor resumes after a row.
	sorts := []Sort{{Property: "n", Direction: "desc"}}
	sr := func(r *crow) sortedRow { return sortedRow{r: r, keys: []fValue{c.sortValue(db, schema, r, "n")}} }
	if lessRows(sr(rows[0]), sr(rows[1]), sorts) >= 0 || lessRows(sr(rows[2]), sr(rows[1]), sorts) <= 0 {
		t.Error("desc order with empties last")
	}
	k := toCursorKey(c.sortValue(db, schema, rows[1], "n"))
	if fromCursorKey(k).Num != 3 {
		t.Errorf("cursor key: %+v", k)
	}
	if !triggerValueMatches([]json.RawMessage{raw("d")}, "d") || triggerValueMatches([]json.RawMessage{raw("t")}, "d") || !triggerValueMatches([]json.RawMessage{raw(true)}, true) {
		t.Error("trigger values")
	}
}
