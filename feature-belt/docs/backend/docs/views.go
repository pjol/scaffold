package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Views of a database: a type and a config (filters, sorts, groups,
// properties shown, calculations and the type's own settings). Base views
// belong to the database (edit on it changes them); a linked view belongs to
// a database block on another page (edit on that page changes it) and reads
// the source's rows with the viewer's own access, checked live.

// Filter is a condition ({property, op, value}) or a group of conditions
// ({conj: and|or, filters}); groups nest three deep.
type Filter struct {
	Property string          `json:"property,omitempty"`
	Op       string          `json:"op,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Conj     string          `json:"conj,omitempty"`
	Filters  []Filter        `json:"filters,omitempty"`
}

func (f Filter) isGroup() bool { return f.Filters != nil || f.Conj != "" }

type Sort struct {
	Property  string `json:"property"`  // a property id, or created_time / last_edited_time
	Direction string `json:"direction"` // asc | desc
}

type Column struct {
	Property string `json:"property"`
	Width    int    `json:"width,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
}

// GroupConfig shapes groups (and sub-groups): date buckets (relative, day,
// week, month, year), number bucket size, order, hidden and collapsed groups.
type GroupConfig struct {
	By        string   `json:"by,omitempty"`
	Step      float64  `json:"step,omitempty"`
	Sort      string   `json:"sort,omitempty"` // manual (option order) | asc | desc
	Order     []string `json:"order,omitempty"`
	HideEmpty bool     `json:"hide_empty,omitempty"`
	Hidden    []string `json:"hidden,omitempty"`
	Collapsed []string `json:"collapsed,omitempty"`
}

// SubItemsConfig nests rows under their parent: Property is the self
// relation naming each row's parent.
type SubItemsConfig struct {
	Property string `json:"property"`
	Display  string `json:"display"` // nested | flat
}

// CardConfig: gallery and board cards (preview none, cover, content or a
// files property id; size small, medium, large; fit the image).
type CardConfig struct {
	Preview string `json:"preview,omitempty"`
	Size    string `json:"size,omitempty"`
	Fit     bool   `json:"fit,omitempty"`
}

// TimelineConfig: zoom, the table beside it and a dependency relation
// (a self relation: "blocked by").
type TimelineConfig struct {
	Zoom            string   `json:"zoom,omitempty"` // hours | day | week | month | quarter | year
	ShowTable       bool     `json:"show_table,omitempty"`
	TableProperties []string `json:"table_properties,omitempty"`
	Dependency      string   `json:"dependency,omitempty"`
}

// ChartConfig: type, the property along the axis (with date buckets), what
// each bar measures, a property splitting series, and order.
type ChartConfig struct {
	Type       string `json:"type"`           // bar | horizontal_bar | line | donut
	X          string `json:"x,omitempty"`    // group by property
	XBy        string `json:"x_by,omitempty"` // dates: day | week | month | year
	Y          string `json:"y,omitempty"`    // count | sum | average | median | min | max
	YProperty  string `json:"y_property,omitempty"`
	Series     string `json:"series,omitempty"`
	Sort       string `json:"sort,omitempty"` // manual | x_asc | x_desc | y_asc | y_desc
	HideEmpty  bool   `json:"hide_empty,omitempty"`
	Cumulative bool   `json:"cumulative,omitempty"`
	ShowValues bool   `json:"show_values,omitempty"`
}

// FormConfig: the questions (each a property), copy and who submits.
type FormConfig struct {
	Title          string         `json:"title,omitempty"`
	Description    string         `json:"description,omitempty"`
	SubmitLabel    string         `json:"submit_label,omitempty"`
	SuccessMessage string         `json:"success_message,omitempty"`
	Questions      []FormQuestion `json:"questions"`
	Anonymous      bool           `json:"anonymous,omitempty"` // don't record who submitted
	Closed         bool           `json:"closed,omitempty"`
}

type FormQuestion struct {
	Property    string `json:"property"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type ViewConfig struct {
	Filters        []Filter          `json:"filters"`
	FilterConj     string            `json:"filter_conj,omitempty"` // and (default) | or, for the top level
	Sorts          []Sort            `json:"sorts"`
	GroupBy        string            `json:"group_by,omitempty"`
	Group          *GroupConfig      `json:"group,omitempty"`
	SubGroupBy     string            `json:"sub_group_by,omitempty"`
	SubGroup       *GroupConfig      `json:"sub_group,omitempty"`
	Columns        []Column          `json:"columns"`                 // properties: order, width, hidden
	CardProperties []string          `json:"card_properties"`         // board, list, gallery, feed: properties shown
	DateProperty   string            `json:"date_property,omitempty"` // calendar; timeline start
	EndProperty    string            `json:"end_property,omitempty"`  // timeline end (else the date's end)
	Calculations   map[string]string `json:"calculations,omitempty"`  // property id → aggregation
	Wrap           bool              `json:"wrap,omitempty"`
	FreezeColumns  int               `json:"freeze_columns,omitempty"`
	Density        string            `json:"density,omitempty"`    // compact | default | comfortable
	LoadLimit      int               `json:"load_limit,omitempty"` // 10, 25, 50, 100, 200 (default 50)
	SubItems       *SubItemsConfig   `json:"sub_items,omitempty"`
	Card           *CardConfig       `json:"card,omitempty"`
	Timeline       *TimelineConfig   `json:"timeline,omitempty"`
	Chart          *ChartConfig      `json:"chart,omitempty"`
	Form           *FormConfig       `json:"form,omitempty"`
	TemplateID     *uuid.UUID        `json:"template_id,omitempty"` // default template for new rows here
}

type View struct {
	ID         uuid.UUID  `json:"id"`
	DatabaseID uuid.UUID  `json:"database_id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Position   string     `json:"position"`
	Config     ViewConfig `json:"config"`
	// Linked views: the database block showing them and its page.
	BlockID    *uuid.UUID `json:"block_id,omitempty"`
	HostPageID *uuid.UUID `json:"host_page_id,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

var (
	viewTypes     = []string{"table", "board", "list", "calendar", "gallery", "timeline", "chart", "form", "feed"}
	chartTypes    = []string{"bar", "horizontal_bar", "line", "donut"}
	chartMeasures = []string{"count", "sum", "average", "median", "min", "max"}
	dateBuckets   = []string{"relative", "day", "week", "month", "year"}
	zooms         = []string{"hours", "day", "week", "month", "quarter", "year"}
	loadLimits    = []int{10, 25, 50, 100, 200}
)

const (
	maxFilterConditions = 40
	maxFilterDepth      = 3
)

// filterOpsFor lists the operators a property accepts (formulas and
// rollups by their result type).
func filterOpsFor(p Property) []string {
	text := []string{"is", "is_not", "contains", "not_contains", "starts_with", "ends_with", "is_empty", "is_not_empty"}
	number := []string{"eq", "neq", "gt", "gte", "lt", "lte", "is_empty", "is_not_empty"}
	date := []string{"is", "before", "after", "on_or_before", "on_or_after", "is_within", "is_empty", "is_not_empty"}
	list := []string{"contains", "not_contains", "is_empty", "is_not_empty"}
	switch p.Type {
	case "title", "text", "url", "email", "phone":
		return text
	case "number", "unique_id":
		return number
	case "select", "status":
		return []string{"is", "is_not", "is_empty", "is_not_empty"}
	case "multi_select", "person", "created_by", "last_edited_by", "relation":
		return list
	case "files":
		return []string{"is_empty", "is_not_empty"}
	case "checkbox":
		return []string{"is"}
	case "date", "created_time", "last_edited_time":
		return date
	case "formula", "rollup":
		t, _ := propFType(p)
		switch t.Kind {
		case fText:
			return text
		case fNumber:
			return number
		case fBool:
			return []string{"is"}
		case fDate:
			return date
		case fList, fPerson, fPage:
			return list
		}
	}
	return nil
}

// groupable: properties a view can group by.
func groupable(p Property) bool {
	switch p.Type {
	case "select", "status", "multi_select", "person", "created_by", "last_edited_by", "checkbox", "date", "created_time",
		"last_edited_time", "text", "url", "email", "phone", "number", "relation", "title":
		return true
	case "formula", "rollup":
		t, _ := propFType(p)
		return t.Kind == fText || t.Kind == fNumber || t.Kind == fBool || t.Kind == fDate
	}
	return false
}

func isDateish(p Property) bool {
	if p.Type == "date" || p.Type == "created_time" || p.Type == "last_edited_time" {
		return true
	}
	if p.Type == "formula" || p.Type == "rollup" {
		t, _ := propFType(p)
		return t.Kind == fDate
	}
	return false
}

func isNumeric(p Property) bool {
	if p.Type == "number" || p.Type == "unique_id" {
		return true
	}
	if p.Type == "formula" || p.Type == "rollup" {
		t, _ := propFType(p)
		return t.Kind == fNumber
	}
	return false
}

// selfRelation: a relation from the database to itself.
func selfRelation(schema []Property, dbID uuid.UUID, id string) bool {
	p, ok := findProp(schema, id)
	return ok && p.Type == "relation" && p.Relation != nil && p.Relation.DatabaseID == dbID
}

const viewCols = `id, database_id, name, type, position, config, block_id, host_page_id, created_at, updated_at`

func scanViews(rows pgx.Rows) ([]View, error) {
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		var v View
		var cfg []byte
		if err := rows.Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Position, &cfg, &v.BlockID, &v.HostPageID, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cfg, &v.Config)
		v.Config = normConfig(v.Config)
		out = append(out, v)
	}
	return out, rows.Err()
}

// loadViews returns a database's own views (not linked ones).
func loadViews(ctx context.Context, q db.Querier, dbID uuid.UUID) ([]View, error) {
	rows, err := q.Query(ctx, `SELECT `+viewCols+` FROM docs_views WHERE database_id = $1 AND block_id IS NULL ORDER BY position, id`, dbID)
	if err != nil {
		return nil, err
	}
	return scanViews(rows)
}

// loadBlockViews returns a linked database block's views.
func loadBlockViews(ctx context.Context, q db.Querier, blockID uuid.UUID) ([]View, error) {
	rows, err := q.Query(ctx, `SELECT `+viewCols+` FROM docs_views WHERE block_id = $1 ORDER BY position, id`, blockID)
	if err != nil {
		return nil, err
	}
	return scanViews(rows)
}

func normConfig(c ViewConfig) ViewConfig {
	if c.Filters == nil {
		c.Filters = []Filter{}
	}
	if c.Sorts == nil {
		c.Sorts = []Sort{}
	}
	if c.Columns == nil {
		c.Columns = []Column{}
	}
	if c.CardProperties == nil {
		c.CardProperties = []string{}
	}
	return c
}

// cleanFilters keeps conditions on known properties with valid operators
// and small values, and groups up to maxFilterDepth deep.
func cleanFilters(schema []Property, in []Filter, depth int, count *int) []Filter {
	out := []Filter{}
	for _, f := range in {
		if *count >= maxFilterConditions {
			break
		}
		if f.isGroup() {
			if depth >= maxFilterDepth {
				continue
			}
			g := Filter{Conj: "and", Filters: cleanFilters(schema, f.Filters, depth+1, count)}
			if f.Conj == "or" {
				g.Conj = "or"
			}
			out = append(out, g)
			continue
		}
		p, ok := findProp(schema, f.Property)
		if ok && slices.Contains(filterOpsFor(p), f.Op) && len(f.Value) <= 1024 {
			*count++
			out = append(out, Filter{Property: f.Property, Op: f.Op, Value: f.Value})
		}
	}
	return out
}

func cleanGroup(g *GroupConfig, p Property) *GroupConfig {
	if g == nil {
		g = &GroupConfig{}
	}
	out := &GroupConfig{HideEmpty: g.HideEmpty}
	if isDateish(p) {
		out.By = "relative"
		if slices.Contains(dateBuckets, g.By) {
			out.By = g.By
		}
	}
	if isNumeric(p) {
		out.Step = 10
		if g.Step > 0 && g.Step < 1e12 {
			out.Step = g.Step
		}
	}
	out.Sort = "manual"
	if g.Sort == "asc" || g.Sort == "desc" {
		out.Sort = g.Sort
	}
	cap := func(list []string) []string {
		var o []string
		for _, k := range list {
			if len(k) <= 64 && !slices.Contains(o, k) && len(o) < 200 {
				o = append(o, k)
			}
		}
		return o
	}
	out.Order, out.Hidden, out.Collapsed = cap(g.Order), cap(g.Hidden), cap(g.Collapsed)
	return out
}

// cleanConfig drops references to properties the schema no longer has (or
// that changed type) and fills what the view type needs.
func cleanConfig(schema []Property, typ string, c ViewConfig) ViewConfig {
	return cleanConfigFor(schema, uuid.Nil, typ, c)
}

func cleanConfigFor(schema []Property, dbID uuid.UUID, typ string, c ViewConfig) ViewConfig {
	out := ViewConfig{Sorts: []Sort{}, Columns: []Column{}, CardProperties: []string{}}
	n := 0
	out.Filters = cleanFilters(schema, c.Filters, 1, &n)
	if c.FilterConj == "or" {
		out.FilterConj = "or"
	}
	for _, srt := range c.Sorts {
		p, ok := findProp(schema, srt.Property)
		if (ok && p.Type != "button" && p.Type != "files") || srt.Property == "created_time" || srt.Property == "last_edited_time" {
			if len(out.Sorts) >= 5 || slices.ContainsFunc(out.Sorts, func(x Sort) bool { return x.Property == srt.Property }) {
				continue
			}
			if srt.Direction != "desc" {
				srt.Direction = "asc"
			}
			out.Sorts = append(out.Sorts, srt)
		}
	}
	seen := map[string]bool{}
	for _, col := range c.Columns {
		if _, ok := findProp(schema, col.Property); ok && !seen[col.Property] {
			seen[col.Property] = true
			if col.Width != 0 {
				col.Width = min(max(col.Width, 60), 800)
			}
			out.Columns = append(out.Columns, col)
		}
	}
	for _, id := range c.CardProperties {
		if _, ok := findProp(schema, id); ok && !slices.Contains(out.CardProperties, id) {
			out.CardProperties = append(out.CardProperties, id)
		}
	}
	if p, ok := findProp(schema, c.GroupBy); ok && groupable(p) && (typ != "board" || p.Type != "title") {
		out.GroupBy = c.GroupBy
		out.Group = cleanGroup(c.Group, p)
	}
	if p, ok := findProp(schema, c.SubGroupBy); ok && groupable(p) && out.GroupBy != "" && c.SubGroupBy != out.GroupBy {
		out.SubGroupBy = c.SubGroupBy
		out.SubGroup = cleanGroup(c.SubGroup, p)
	}
	if p, ok := findProp(schema, c.DateProperty); ok && isDateish(p) {
		out.DateProperty = c.DateProperty
	}
	if p, ok := findProp(schema, c.EndProperty); ok && isDateish(p) && c.EndProperty != out.DateProperty {
		out.EndProperty = c.EndProperty
	}
	if len(c.Calculations) > 0 {
		out.Calculations = map[string]string{}
		for id, fn := range c.Calculations {
			p, ok := findProp(schema, id)
			if !ok || p.Type == "button" || !slices.Contains(rollupFunctions, fn) || fn == "show_original" || fn == "show_unique" {
				continue
			}
			if t, ok := calcType(p); ok && rollupAllowed(fn, t) {
				out.Calculations[id] = fn
			}
		}
	}
	out.Wrap = c.Wrap
	out.FreezeColumns = min(max(c.FreezeColumns, 0), 3)
	if c.Density == "compact" || c.Density == "comfortable" {
		out.Density = c.Density
	}
	if slices.Contains(loadLimits, c.LoadLimit) {
		out.LoadLimit = c.LoadLimit
	}
	if c.SubItems != nil && selfRelation(schema, dbID, c.SubItems.Property) {
		d := "nested"
		if c.SubItems.Display == "flat" {
			d = "flat"
		}
		out.SubItems = &SubItemsConfig{Property: c.SubItems.Property, Display: d}
	}
	if c.Card != nil {
		cc := &CardConfig{Fit: c.Card.Fit, Size: "medium", Preview: "none"}
		if c.Card.Size == "small" || c.Card.Size == "large" {
			cc.Size = c.Card.Size
		}
		switch c.Card.Preview {
		case "cover", "content", "none":
			cc.Preview = c.Card.Preview
		default:
			if p, ok := findProp(schema, c.Card.Preview); ok && p.Type == "files" {
				cc.Preview = p.ID
			}
		}
		out.Card = cc
	}
	if c.Timeline != nil || typ == "timeline" {
		t := c.Timeline
		if t == nil {
			t = &TimelineConfig{}
		}
		tc := &TimelineConfig{Zoom: "month", ShowTable: t.ShowTable}
		if slices.Contains(zooms, t.Zoom) {
			tc.Zoom = t.Zoom
		}
		for _, id := range t.TableProperties {
			if _, ok := findProp(schema, id); ok && !slices.Contains(tc.TableProperties, id) {
				tc.TableProperties = append(tc.TableProperties, id)
			}
		}
		if selfRelation(schema, dbID, t.Dependency) {
			tc.Dependency = t.Dependency
		}
		out.Timeline = tc
	}
	if c.Chart != nil || typ == "chart" {
		ch := c.Chart
		if ch == nil {
			ch = &ChartConfig{}
		}
		cc := &ChartConfig{Type: "bar", Y: "count", Sort: "manual", HideEmpty: ch.HideEmpty, Cumulative: ch.Cumulative, ShowValues: ch.ShowValues}
		if slices.Contains(chartTypes, ch.Type) {
			cc.Type = ch.Type
		}
		if p, ok := findProp(schema, ch.X); ok && groupable(p) {
			cc.X = ch.X
			if isDateish(p) {
				cc.XBy = "month"
				if slices.Contains(dateBuckets[1:], ch.XBy) {
					cc.XBy = ch.XBy
				}
			}
		}
		if slices.Contains(chartMeasures, ch.Y) && ch.Y != "count" {
			if p, ok := findProp(schema, ch.YProperty); ok && isNumeric(p) {
				cc.Y, cc.YProperty = ch.Y, ch.YProperty
			}
		}
		if p, ok := findProp(schema, ch.Series); ok && groupable(p) && ch.Series != cc.X && cc.Type != "donut" {
			cc.Series = ch.Series
		}
		if slices.Contains([]string{"x_asc", "x_desc", "y_asc", "y_desc"}, ch.Sort) {
			cc.Sort = ch.Sort
		}
		out.Chart = cc
	}
	if c.Form != nil || typ == "form" {
		out.Form = cleanForm(schema, c.Form)
	}
	if c.TemplateID != nil {
		out.TemplateID = c.TemplateID
	}
	// Fill what the type needs.
	if typ == "board" && out.GroupBy == "" {
		for _, p := range schema {
			if p.Type == "status" || p.Type == "select" {
				out.GroupBy, out.Group = p.ID, cleanGroup(nil, p)
				break
			}
		}
	}
	if (typ == "calendar" || typ == "timeline") && out.DateProperty == "" {
		for _, p := range schema {
			if p.Type == "date" {
				out.DateProperty = p.ID
				break
			}
		}
	}
	return out
}

// calcType is the value type a footer calculation aggregates for p.
func calcType(p Property) (fType, bool) {
	switch p.Type {
	case "title":
		return tText, true
	case "button":
		return tEmpty, false
	}
	return propFType(p)
}

func cleanForm(schema []Property, f *FormConfig) *FormConfig {
	if f == nil {
		f = &FormConfig{}
	}
	trim := func(s string, n int) string {
		s = strings.TrimSpace(cleanText(s))
		if utf8.RuneCountInString(s) > n {
			s = string([]rune(s)[:n])
		}
		return s
	}
	out := &FormConfig{Title: trim(f.Title, 200), Description: trim(f.Description, 2000), SubmitLabel: trim(f.SubmitLabel, 40),
		SuccessMessage: trim(f.SuccessMessage, 500), Anonymous: f.Anonymous, Closed: f.Closed, Questions: []FormQuestion{}}
	for _, q := range f.Questions {
		p, ok := findProp(schema, q.Property)
		if !ok || !formAnswerable(p) || slices.ContainsFunc(out.Questions, func(x FormQuestion) bool { return x.Property == q.Property }) {
			continue
		}
		out.Questions = append(out.Questions, FormQuestion{Property: p.ID, Label: trim(q.Label, 200), Description: trim(q.Description, 500), Required: q.Required})
	}
	if len(f.Questions) == 0 && f.Title == "" {
		// A new form asks for the title first.
		out.Questions = append(out.Questions, FormQuestion{Property: "title", Required: true})
	}
	return out
}

// formAnswerable: properties a form can ask for (stored, not people-facing
// lookups of workspace data like relations and people).
func formAnswerable(p Property) bool {
	switch p.Type {
	case "title", "text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone":
		return true
	}
	return false
}

// ---- view routes ---------------------------------------------------------------------

type viewReq struct {
	Name     *string     `json:"name"`
	Type     string      `json:"type"`
	Config   *ViewConfig `json:"config"`
	Position string      `json:"position"`
}

// ensureViewProps adds what a view type needs to the schema: a board a
// status, a calendar or timeline a date. It returns the schema and whether
// it changed.
func ensureViewProps(schema []Property, typ string) ([]Property, bool) {
	needs := func(types ...string) bool {
		return !slices.ContainsFunc(schema, func(p Property) bool { return slices.Contains(types, p.Type) })
	}
	changed := false
	if typ == "board" && needs("select", "status") {
		schema, changed = append(schema, statusProperty(uniquePropName(schema, "Status"))), true
	}
	if (typ == "calendar" || typ == "timeline") && needs("date") {
		schema, changed = append(schema, Property{ID: shortID(), Name: uniquePropName(schema, "Date"), Type: "date"}), true
	}
	return schema, changed
}

// insertView stores a new view (in tx) and registers a form view with access.
func (s *Service) insertView(ctx context.Context, tx pgx.Tx, me uuid.UUID, dbp *Page, schema []Property, name, typ string, cfg ViewConfig, blockID, hostID *uuid.UUID) (*View, error) {
	var n int
	var last string
	var err error
	if blockID != nil {
		err = tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(position), '') FROM docs_views WHERE block_id = $1`, *blockID).Scan(&n, &last)
	} else {
		err = tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(position), '') FROM docs_views WHERE database_id = $1 AND block_id IS NULL`, dbp.ID).Scan(&n, &last)
	}
	if err != nil {
		return nil, err
	}
	if n >= maxViews {
		return nil, httpx.Conflict(fmt.Sprintf("A database has up to %d views.", maxViews))
	}
	cfg = cleanConfigFor(schema, dbp.ID, typ, cfg)
	b, _ := json.Marshal(cfg)
	v := &View{Config: cfg}
	if err := tx.QueryRow(ctx, `
		INSERT INTO docs_views (database_id, name, type, position, config, created_by, block_id, host_page_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+viewCols, dbp.ID, name, typ, keyBetween(last, ""), b, me, blockID, hostID).Scan(
		&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Position, &b, &v.BlockID, &v.HostPageID, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	if typ == "form" {
		if err := s.registerForm(ctx, tx, me, dbp, v.ID); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func viewName(req *string, typ string) (string, error) {
	name := viewLabel(typ)
	if req != nil {
		if name = strings.TrimSpace(cleanText(*req)); name == "" || utf8.RuneCountInString(name) > 100 {
			return "", httpx.BadRequest("Give the view a name under 100 characters.")
		}
	}
	return name, nil
}

func viewLabel(typ string) string {
	if typ == "" {
		return "View"
	}
	return strings.ToUpper(typ[:1]) + typ[1:]
}

// createView: POST /docs/databases/{id}/views (edit). A board without a
// select or status property gets a Status property; a calendar or timeline
// without a date property gets a Date property.
func (s *Service) createView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req viewReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !slices.Contains(viewTypes, req.Type) {
		return httpx.BadRequest("Choose a view type: " + strings.Join(viewTypes, ", ") + ".")
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	if err := editable(dbp, true); err != nil {
		return err
	}
	name, err := viewName(req.Name, req.Type)
	if err != nil {
		return err
	}
	cfg := ViewConfig{}
	if req.Config != nil {
		cfg = *req.Config
	}
	var view *View
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := loadPage(ctx, tx, id, true)
		if err != nil {
			return err
		}
		schema, changed := ensureViewProps(cur.Schema, req.Type)
		if changed {
			raw, _ := json.Marshal(schema)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, id, raw); err != nil {
				return err
			}
		}
		view, err = s.insertView(ctx, tx, me, cur, schema, name, req.Type, cfg, nil, nil)
		return err
	})
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(id), EventSchema, map[string]any{"database_id": id})
	httpx.JSON(w, http.StatusCreated, view)
	return nil
}

type linkedViewReq struct {
	BlockID    uuid.UUID   `json:"block_id"`
	DatabaseID uuid.UUID   `json:"database_id"`
	Type       string      `json:"type"`
	Name       *string     `json:"name"`
	Config     *ViewConfig `json:"config"`
}

// createLinkedView: POST /docs/pages/{id}/linked-views (edit on the host
// page, view on the source database): a view for a database block on the
// page. The block may still be on its way (operations are queued); views
// whose block never appears are swept after a day.
func (s *Service) createLinkedView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	hostID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req linkedViewReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Type == "" {
		req.Type = "table"
	}
	if !slices.Contains(viewTypes, req.Type) || req.Type == "form" {
		return httpx.BadRequest("Choose a view type (forms belong to their database).")
	}
	if req.BlockID == uuid.Nil {
		return httpx.BadRequest("block_id is required.")
	}
	host, err := requirePage(ctx, s.d.Pool, me, hostID, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(host, true); err != nil {
		return err
	}
	src, err := requirePage(ctx, s.d.Pool, me, req.DatabaseID, access.View)
	if err != nil {
		return err
	}
	if src.Kind != "database" || src.OrgID != host.OrgID {
		return errNotDatabase
	}
	var other uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `SELECT page_id FROM docs_blocks WHERE id = $1`, req.BlockID).Scan(&other)
	if err == nil && other != hostID {
		return httpx.NotFound("Block not found.")
	}
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	name, err := viewName(req.Name, req.Type)
	if err != nil {
		return err
	}
	cfg := ViewConfig{}
	if req.Config != nil {
		cfg = *req.Config
	} else if base, err := loadViews(ctx, s.d.Pool, src.ID); err == nil && len(base) > 0 {
		// Start from the source's first view of that type, or its first.
		pick := base[0]
		for _, v := range base {
			if v.Type == req.Type {
				pick = v
				break
			}
		}
		cfg = pick.Config
		cfg.Form = nil
	}
	var view *View
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		view, err = s.insertView(ctx, tx, me, src, src.Schema, name, req.Type, cfg, &req.BlockID, &hostID)
		return err
	})
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(hostID), EventSchema, map[string]any{"database_id": src.ID, "block_id": req.BlockID})
	httpx.JSON(w, http.StatusCreated, view)
	return nil
}

// LinkedDatabase is a database block's source and views as the viewer sees
// them.
type LinkedDatabase struct {
	Database *Page  `json:"database"` // nil: the viewer can't open the source
	Views    []View `json:"views"`
	Linked   bool   `json:"linked"` // the block shows a database that lives elsewhere
	CanEdit  bool   `json:"can_edit"`
}

// getBlockDatabase: GET /docs/pages/{id}/blocks/{blockID}/database (view on
// the page): the database a block shows, with the block's own views when it
// is linked. The source is checked against the viewer live: without view on
// it the answer has no database.
func (s *Service) getBlockDatabase(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	hostID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	blockID, err := httpx.URLParamUUID(r, "blockID")
	if err != nil {
		return err
	}
	host, err := requirePage(ctx, s.d.Pool, me, hostID, access.View)
	if err != nil {
		return err
	}
	var dbID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `SELECT (props->>'database_id')::uuid FROM docs_blocks WHERE id = $1 AND page_id = $2 AND type = 'database'`, blockID, hostID).Scan(&dbID)
	if db.IsNoRows(err) {
		return httpx.NotFound("Block not found.")
	}
	if err != nil {
		return err
	}
	out := LinkedDatabase{Views: []View{}}
	src, err := requirePage(ctx, s.d.Pool, me, dbID, access.View)
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusNotFound {
			httpx.OK(w, out)
			return nil
		}
		return err
	}
	out.Linked = !(src.Inline && src.ParentID != nil && *src.ParentID == hostID)
	if out.Linked {
		out.CanEdit = host.MyLevel >= access.Edit && host.DeletedAt == nil && !host.Locked
		if out.Views, err = loadBlockViews(ctx, s.d.Pool, blockID); err != nil {
			return err
		}
	} else {
		out.CanEdit = src.MyLevel >= access.Edit && src.DeletedAt == nil && !src.Locked
		if out.Views, err = loadViews(ctx, s.d.Pool, src.ID); err != nil {
			return err
		}
	}
	c := s.newComputer(ctx, memberViewer(ctx, me, ""))
	src.Schema = c.annotateSchema(src.ID, src.Schema)
	out.Database = src
	httpx.OK(w, out)
	return nil
}

// viewFor loads a view and checks the caller's level where it belongs: the
// database for its own views, the host page for linked ones.
func (s *Service) viewFor(ctx context.Context, me, viewID uuid.UUID, min access.Level) (*View, *Page, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+viewCols+` FROM docs_views WHERE id = $1`, viewID)
	if err != nil {
		return nil, nil, err
	}
	list, err := scanViews(rows)
	if err != nil {
		return nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, httpx.NotFound("View not found.")
	}
	v := list[0]
	owner := v.DatabaseID
	if v.HostPageID != nil {
		owner = *v.HostPageID
		if _, err := requirePage(ctx, s.d.Pool, me, v.DatabaseID, access.View); err != nil {
			return nil, nil, httpx.NotFound("View not found.")
		}
	}
	p, err := requirePage(ctx, s.d.Pool, me, owner, min)
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusNotFound {
			return nil, nil, httpx.NotFound("View not found.")
		}
		return nil, nil, err
	}
	return &v, p, nil
}

// updateView: PATCH /docs/views/{viewID} (edit on the database, or on the
// host page for a linked view).
func (s *Service) updateView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	viewID, err := httpx.URLParamUUID(r, "viewID")
	if err != nil {
		return err
	}
	var req viewReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	v, owner, err := s.viewFor(ctx, me, viewID, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(owner, true); err != nil {
		return err
	}
	if req.Name != nil {
		name := strings.TrimSpace(cleanText(*req.Name))
		if name == "" || utf8.RuneCountInString(name) > 100 {
			return httpx.BadRequest("Give the view a name under 100 characters.")
		}
		v.Name = name
	}
	wasForm := v.Type == "form"
	if req.Type != "" {
		if !slices.Contains(viewTypes, req.Type) || (req.Type == "form" && v.BlockID != nil) {
			return httpx.BadRequest("Choose a view type: " + strings.Join(viewTypes, ", ") + ".")
		}
		v.Type = req.Type
	}
	if req.Config != nil {
		v.Config = *req.Config
	}
	if req.Position != "" {
		if !validKey(req.Position) {
			return httpx.BadRequest("That position isn't valid.")
		}
		v.Position = req.Position
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		dbp, err := loadPage(ctx, tx, v.DatabaseID, true)
		if err != nil {
			return err
		}
		schema := dbp.Schema
		if req.Type != "" && v.BlockID == nil {
			var changed bool
			if schema, changed = ensureViewProps(schema, v.Type); changed {
				raw, _ := json.Marshal(schema)
				if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, dbp.ID, raw); err != nil {
					return err
				}
			}
		}
		if v.Config.TemplateID != nil {
			if !s.isTemplateOf(ctx, tx, *v.Config.TemplateID, dbp.ID) {
				v.Config.TemplateID = nil
			}
		}
		v.Config = cleanConfigFor(schema, dbp.ID, v.Type, v.Config)
		b, _ := json.Marshal(v.Config)
		if err := tx.QueryRow(ctx, `UPDATE docs_views SET name = $2, type = $3, config = $4, position = $5, updated_at = now() WHERE id = $1 RETURNING updated_at`,
			v.ID, v.Name, v.Type, b, v.Position).Scan(&v.UpdatedAt); err != nil {
			return err
		}
		switch {
		case v.Type == "form" && !wasForm:
			return s.registerForm(ctx, tx, me, dbp, v.ID)
		case v.Type != "form" && wasForm:
			return access.DeleteResource(ctx, tx, access.Ref{Kind: KindForm, ID: v.ID})
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(owner.ID), EventSchema, map[string]any{"database_id": v.DatabaseID})
	httpx.OK(w, v)
	return nil
}

// deleteView: DELETE /docs/views/{viewID} (edit; a database or a linked
// block keeps at least one).
func (s *Service) deleteView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	viewID, err := httpx.URLParamUUID(r, "viewID")
	if err != nil {
		return err
	}
	v, owner, err := s.viewFor(ctx, me, viewID, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(owner, true); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var tag interface{ RowsAffected() int64 }
		var err error
		if v.BlockID != nil {
			tag, err = tx.Exec(ctx, `DELETE FROM docs_views WHERE id = $1 AND (SELECT count(*) FROM docs_views WHERE block_id = $2) > 1`, v.ID, *v.BlockID)
		} else {
			tag, err = tx.Exec(ctx, `DELETE FROM docs_views WHERE id = $1 AND (SELECT count(*) FROM docs_views WHERE database_id = $2 AND block_id IS NULL) > 1`, v.ID, v.DatabaseID)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.Conflict("A database needs at least one view.")
		}
		if v.Type == "form" {
			return access.DeleteResource(ctx, tx, access.Ref{Kind: KindForm, ID: v.ID})
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(owner.ID), EventSchema, map[string]any{"database_id": v.DatabaseID})
	httpx.NoContent(w)
	return nil
}

// cleanViews re-validates every view of a database (its own and linked
// ones) against a new schema.
func (s *Service) cleanViews(ctx context.Context, tx pgx.Tx, dbID uuid.UUID, schema []Property) error {
	rows, err := tx.Query(ctx, `SELECT `+viewCols+` FROM docs_views WHERE database_id = $1`, dbID)
	if err != nil {
		return err
	}
	views, err := scanViews(rows)
	if err != nil {
		return err
	}
	for _, v := range views {
		cfg := cleanConfigFor(schema, dbID, v.Type, v.Config)
		b, _ := json.Marshal(cfg)
		if _, err := tx.Exec(ctx, `UPDATE docs_views SET config = $2, updated_at = now() WHERE id = $1`, v.ID, b); err != nil {
			return err
		}
	}
	return nil
}

// sweepLinkedViews deletes linked views whose block is gone (a day's grace
// for blocks still being created).
func (s *Service) sweepLinkedViews(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_views v WHERE v.block_id IS NOT NULL AND v.created_at < now() - interval '1 day'
		AND NOT EXISTS (SELECT 1 FROM docs_blocks b WHERE b.id = v.block_id)`)
	return err
}
