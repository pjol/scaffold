package docs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Property is one column of a database. The title property (id "title") is
// the row page's title; every other value lives in the row's props.
type Property struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Options      []Option `json:"options,omitempty"`       // select, multi_select, status
	NumberFormat string   `json:"number_format,omitempty"` // number, percent, dollar, euro, pound
}

// Option is a select, multi-select or status choice. Status options belong
// to a group: todo, doing or done.
type Option struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Group string `json:"group,omitempty"`
}

var (
	propTypes     = []string{"title", "text", "number", "select", "multi_select", "status", "date", "person", "checkbox", "url", "email"}
	numberFormats = []string{"number", "percent", "dollar", "euro", "pound"}
	optionColors  = []string{"neutral", "primary", "accent", "success", "warning", "danger", "info"}
	statusGroups  = []string{"todo", "doing", "done"}
	propIDRE      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,24}$`)
	emailRE       = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	dateValueRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2})?$`)
)

const (
	maxProperties = 50
	maxOptions    = 100
	maxViews      = 30
	maxRows       = 10000
	maxTextValue  = 2000
)

// shortID makes a property or option id.
func shortID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b [8]byte
	_, _ = rand.Read(b[:])
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:])
}

func defaultSchema() []Property {
	return []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: shortID(), Name: "Tags", Type: "multi_select", Options: []Option{}},
	}
}

func statusProperty(name string) Property {
	return Property{ID: shortID(), Name: name, Type: "status", Options: []Option{
		{ID: shortID(), Name: "Not started", Color: "neutral", Group: "todo"},
		{ID: shortID(), Name: "In progress", Color: "info", Group: "doing"},
		{ID: shortID(), Name: "Done", Color: "success", Group: "done"},
	}}
}

func findProp(schema []Property, id string) (Property, bool) {
	for _, p := range schema {
		if p.ID == id {
			return p, true
		}
	}
	return Property{}, false
}

func hasOption(p Property, id string) bool {
	return slices.ContainsFunc(p.Options, func(o Option) bool { return o.ID == id })
}

// personValues returns the people a row's person properties name.
func personValues(schema []Property, props map[string]any) []uuid.UUID {
	var out []uuid.UUID
	for _, p := range schema {
		if p.Type != "person" {
			continue
		}
		list, _ := props[p.ID].([]any)
		for _, v := range list {
			if s, ok := v.(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// ---- schema ----------------------------------------------------------------------

type schemaReq struct {
	Properties []Property `json:"properties"`
}

// putSchema: PUT /docs/databases/{id}/schema (edit) replaces the property
// list. Values of removed properties, removed options and properties whose
// type changed are cleared; views drop references to removed properties.
func (s *Service) putSchema(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req schemaReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
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
	schema, err := validateSchema(req.Properties)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := loadPage(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if err := s.migrateValues(ctx, tx, id, cur.Schema, schema); err != nil {
			return err
		}
		raw, _ := json.Marshal(schema)
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now(), updated_by = $3 WHERE id = $1`, id, raw, me); err != nil {
			return err
		}
		views, err := loadViews(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, v := range views {
			cfg := cleanConfig(schema, v.Type, v.Config)
			b, _ := json.Marshal(cfg)
			if _, err := tx.Exec(ctx, `UPDATE docs_views SET config = $2, updated_at = now() WHERE id = $1`, v.ID, b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(ctx, pageTopic(id), EventSchema, map[string]any{"database_id": id})
	out, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	out.MyLevel = dbp.MyLevel
	httpx.OK(w, out)
	return nil
}

func validateSchema(in []Property) ([]Property, error) {
	if len(in) == 0 || len(in) > maxProperties {
		return nil, httpx.BadRequest(fmt.Sprintf("A database has 1 to %d properties.", maxProperties))
	}
	seen := map[string]bool{}
	titles := 0
	out := make([]Property, 0, len(in))
	for _, p := range in {
		p.Name = strings.TrimSpace(cleanText(p.Name))
		if p.ID == "" {
			p.ID = shortID()
		}
		if !propIDRE.MatchString(p.ID) || seen[p.ID] {
			return nil, httpx.BadRequest("Each property needs its own id.")
		}
		seen[p.ID] = true
		if p.Name == "" || utf8.RuneCountInString(p.Name) > 100 {
			return nil, httpx.BadRequest("Give each property a name under 100 characters.")
		}
		if !slices.Contains(propTypes, p.Type) {
			return nil, httpx.BadRequest("Unknown property type " + strconv.Quote(p.Type) + ".")
		}
		if p.Type == "title" {
			titles++
			if p.ID != "title" {
				return nil, httpx.BadRequest("The title property's id is \"title\".")
			}
		}
		if p.Type == "number" && !slices.Contains(numberFormats, p.NumberFormat) {
			p.NumberFormat = "number"
		}
		if p.Type != "number" {
			p.NumberFormat = ""
		}
		switch p.Type {
		case "select", "multi_select", "status":
			if len(p.Options) > maxOptions {
				return nil, httpx.BadRequest(fmt.Sprintf("A property has up to %d options.", maxOptions))
			}
			ids := map[string]bool{}
			opts := make([]Option, 0, len(p.Options))
			for _, o := range p.Options {
				o.Name = strings.TrimSpace(cleanText(o.Name))
				if o.Name == "" || utf8.RuneCountInString(o.Name) > 100 {
					return nil, httpx.BadRequest("Give each option a name under 100 characters.")
				}
				if o.ID == "" {
					o.ID = shortID()
				}
				if !propIDRE.MatchString(o.ID) || ids[o.ID] {
					return nil, httpx.BadRequest("Each option needs its own id.")
				}
				ids[o.ID] = true
				if !slices.Contains(optionColors, o.Color) {
					o.Color = "neutral"
				}
				if p.Type == "status" {
					if !slices.Contains(statusGroups, o.Group) {
						o.Group = "todo"
					}
				} else {
					o.Group = ""
				}
				opts = append(opts, o)
			}
			p.Options = opts
		default:
			p.Options = nil
		}
		out = append(out, p)
	}
	if titles != 1 {
		return nil, httpx.BadRequest("A database has exactly one title property.")
	}
	return out, nil
}

// migrateValues clears row values the new schema can't hold.
func (s *Service) migrateValues(ctx context.Context, tx pgx.Tx, dbID uuid.UUID, old, next []Property) error {
	for _, o := range old {
		if o.Type == "title" {
			continue
		}
		n, ok := findProp(next, o.ID)
		textish := func(t string) bool { return t == "text" || t == "url" || t == "email" }
		switch {
		case !ok || (n.Type != o.Type && !(textish(n.Type) && textish(o.Type))):
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = props - $2::text WHERE parent_id = $1 AND is_row AND props ? $2::text`, dbID, o.ID); err != nil {
				return err
			}
		case n.Type == "select" || n.Type == "status":
			keep := optionIDs(n)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = props - $2::text
				WHERE parent_id = $1 AND is_row AND props ? $2::text AND NOT (props->>$2::text = ANY ($3))`, dbID, o.ID, keep); err != nil {
				return err
			}
		case n.Type == "multi_select":
			keep := optionIDs(n)
			if _, err := tx.Exec(ctx, `
				UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], COALESCE((
				    SELECT jsonb_agg(v) FROM jsonb_array_elements_text(props->$2::text) v WHERE v = ANY ($3)), '[]'::jsonb))
				WHERE parent_id = $1 AND is_row AND jsonb_typeof(props->$2::text) = 'array'`, dbID, o.ID, keep); err != nil {
				return err
			}
		}
	}
	return nil
}

func optionIDs(p Property) []string {
	out := make([]string, len(p.Options))
	for i, o := range p.Options {
		out[i] = o.ID
	}
	return out
}

// ---- row values ------------------------------------------------------------------

// cleanValue validates one row value for its property. nil clears it.
func (s *Service) cleanValue(ctx context.Context, orgID uuid.UUID, p Property, raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	bad := func(msg string) error { return httpx.BadRequest(p.Name + ": " + msg) }
	switch p.Type {
	case "text", "url", "email":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bad("enter text.")
		}
		v = strings.TrimSpace(cleanText(v))
		if v == "" {
			return nil, nil
		}
		if utf8.RuneCountInString(v) > maxTextValue {
			return nil, bad(fmt.Sprintf("keep it under %d characters.", maxTextValue))
		}
		if p.Type == "url" {
			if !strings.Contains(v, "://") {
				v = "https://" + v
			}
			if v = safeLink(v); v == "" || strings.HasPrefix(v, "mailto:") {
				return nil, bad("enter a web address.")
			}
		}
		if p.Type == "email" && !emailRE.MatchString(v) {
			return nil, bad("enter an email address.")
		}
		return v, nil
	case "number":
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, bad("enter a number.")
		}
		return v, nil
	case "checkbox":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bad("must be true or false.")
		}
		return v, nil
	case "select", "status":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil || !hasOption(p, v) {
			return nil, bad("choose one of its options.")
		}
		return v, nil
	case "multi_select":
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil || len(v) > maxOptions {
			return nil, bad("choose from its options.")
		}
		out := []string{}
		for _, id := range v {
			if !hasOption(p, id) {
				return nil, bad("choose from its options.")
			}
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out, nil
	case "date":
		var v struct {
			Start string `json:"start"`
			End   string `json:"end,omitempty"`
		}
		if err := json.Unmarshal(raw, &v); err != nil || !validDateValue(v.Start) || (v.End != "" && (!validDateValue(v.End) || v.End < v.Start)) {
			return nil, bad("enter a date (and an end after the start).")
		}
		out := map[string]string{"start": v.Start}
		if v.End != "" {
			out["end"] = v.End
		}
		return out, nil
	case "person":
		var v []uuid.UUID
		if err := json.Unmarshal(raw, &v); err != nil || len(v) > 20 {
			return nil, bad("choose up to 20 people.")
		}
		v = dedupeIDs(v)
		if len(v) == 0 {
			return []string{}, nil
		}
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, orgID, v).Scan(&n); err != nil {
			return nil, err
		}
		if n != len(v) {
			return nil, bad("choose people in this " + orgs.Noun() + ".")
		}
		out := make([]string, len(v))
		for i, id := range v {
			out[i] = id.String()
		}
		return out, nil
	}
	return nil, bad("can't be set.")
}

func validDateValue(s string) bool {
	if !dateValueRE.MatchString(s) {
		return false
	}
	layout := "2006-01-02"
	if len(s) > 10 {
		layout = "2006-01-02T15:04"
	}
	_, err := time.Parse(layout, s)
	return err == nil
}

type rowValuesReq struct {
	Title    *string                    `json:"title"`
	Values   map[string]json.RawMessage `json:"values"`
	AfterID  *uuid.UUID                 `json:"after_id"` // reorder: after this row (nil with first=true: first)
	First    bool                       `json:"first"`
	Position string                     `json:"position"` // or an explicit position
}

// setRowValues: PATCH /docs/pages/{id}/properties (edit on the row): set
// property values, the title, or the row's place in its database.
func (s *Service) setRowValues(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req rowValuesReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	row, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	out, err := s.setRow(ctx, me, row, req)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) setRow(ctx context.Context, me uuid.UUID, row *Page, req rowValuesReq) (*Page, error) {
	if !row.IsRow || row.ParentID == nil {
		return nil, httpx.BadRequest("Only database rows have properties.")
	}
	if err := editable(row, false); err != nil {
		return nil, err
	}
	dbp, err := loadPage(ctx, s.d.Pool, *row.ParentID, false)
	if err != nil {
		return nil, err
	}
	if dbp.Locked {
		return nil, errLocked
	}
	set := map[string]any{}
	unset := []string{}
	for key, raw := range req.Values {
		p, ok := findProp(dbp.Schema, key)
		if !ok {
			return nil, httpx.BadRequest("That property isn't in this database any more. Reload and try again.")
		}
		if p.Type == "title" {
			var t string
			if err := json.Unmarshal(raw, &t); err != nil {
				return nil, httpx.BadRequest("Name: enter text.")
			}
			req.Title = &t
			continue
		}
		v, err := s.cleanValue(ctx, row.OrgID, p, raw)
		if err != nil {
			return nil, err
		}
		if v == nil {
			unset = append(unset, key)
		} else {
			set[key] = v
		}
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if len(set) > 0 || len(unset) > 0 {
			b, _ := json.Marshal(set)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = (props - $3::text[]) || $2, updated_at = now(), updated_by = $4 WHERE id = $1`,
				row.ID, b, unset, me); err != nil {
				return err
			}
		}
		if req.Title != nil {
			t, err := cleanTitle(*req.Title)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET title = $2, updated_at = now(), updated_by = $3 WHERE id = $1`, row.ID, t, me); err != nil {
				return err
			}
		}
		if req.Position != "" || req.AfterID != nil || req.First {
			pos := req.Position
			switch {
			case pos != "":
				if !validKey(pos) {
					return httpx.BadRequest("That position isn't valid.")
				}
			case req.AfterID != nil:
				var err error
				if pos, err = siblingPosition(ctx, tx, row.OrgID, row.ParentID, req.AfterID); err != nil {
					return err
				}
			default:
				var first string
				if err := tx.QueryRow(ctx, `SELECT COALESCE(min(position), '') FROM docs_pages WHERE parent_id = $1 AND id <> $2`, *row.ParentID, row.ID).Scan(&first); err != nil {
					return err
				}
				pos = keyBetween("", first)
			}
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET position = $2, updated_at = now(), updated_by = $3 WHERE id = $1`, row.ID, pos, me); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, err := loadPage(ctx, s.d.Pool, row.ID, false)
	if err != nil {
		return nil, err
	}
	out.MyLevel = row.MyLevel
	s.pageChanged(ctx, out, false)
	return out, nil
}

type createRowReq struct {
	Title   string                     `json:"title"`
	Values  map[string]json.RawMessage `json:"values"`
	AfterID *uuid.UUID                 `json:"after_id"`
	First   bool                       `json:"first"`
}

// createRow: POST /docs/databases/{id}/rows (edit on the database).
func (s *Service) createRow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req createRowReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	out, err := s.newRow(ctx, me, id, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) newRow(ctx context.Context, me, dbID uuid.UUID, req createRowReq) (*Page, error) {
	dbp, err := requirePage(ctx, s.d.Pool, me, dbID, access.Edit)
	if err != nil {
		return nil, err
	}
	if dbp.Kind != "database" {
		return nil, errNotDatabase
	}
	if err := editable(dbp, true); err != nil {
		return nil, err
	}
	title, err := cleanTitle(req.Title)
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	for key, raw := range req.Values {
		p, ok := findProp(dbp.Schema, key)
		if !ok || p.Type == "title" {
			continue
		}
		v, err := s.cleanValue(ctx, dbp.OrgID, p, raw)
		if err != nil {
			return nil, err
		}
		if v != nil {
			values[key] = v
		}
	}
	var rowID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM docs_pages WHERE parent_id = $1 AND is_row AND deleted_at IS NULL`, dbID).Scan(&n); err != nil {
			return err
		}
		if n >= maxRows {
			return httpx.NewError(http.StatusConflict, "database_full", fmt.Sprintf("A database holds up to %d rows.", maxRows))
		}
		var pos string
		if req.First {
			var first string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(min(position), '') FROM docs_pages WHERE parent_id = $1`, dbID).Scan(&first); err != nil {
				return err
			}
			pos = keyBetween("", first)
		} else {
			var err error
			if pos, err = siblingPosition(ctx, tx, dbp.OrgID, &dbID, req.AfterID); err != nil {
				return err
			}
		}
		rowID = uuid.New()
		b, _ := json.Marshal(values)
		if _, err := tx.Exec(ctx, `
			INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, position, props, created_by, updated_by)
			VALUES ($1, $2, $3, 'page', true, $4, $5, $6, $7, $7)`, rowID, dbp.OrgID, dbID, title, pos, b, me); err != nil {
			return err
		}
		return access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: rowID, OrgID: dbp.OrgID, OwnerID: me, Parent: &access.Ref{Kind: Kind, ID: dbID}})
	})
	if err != nil {
		return nil, err
	}
	out, err := loadPage(ctx, s.d.Pool, rowID, false)
	if err != nil {
		return nil, err
	}
	out.MyLevel = dbp.MyLevel
	s.publish(ctx, pageTopic(dbID), EventRows, map[string]any{"database_id": dbID, "row_id": rowID})
	return out, nil
}

// ---- views -----------------------------------------------------------------------

// Filter is one condition; a view's filters all have to match (AND).
type Filter struct {
	Property string          `json:"property"`
	Op       string          `json:"op"`
	Value    json.RawMessage `json:"value,omitempty"`
}

type Sort struct {
	Property  string `json:"property"`  // a property id, or created_time / last_edited_time
	Direction string `json:"direction"` // asc | desc
}

type Column struct {
	Property string `json:"property"`
	Width    int    `json:"width,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
}

type ViewConfig struct {
	Filters        []Filter `json:"filters"`
	Sorts          []Sort   `json:"sorts"`
	GroupBy        string   `json:"group_by,omitempty"`      // board: a select or status property
	Columns        []Column `json:"columns"`                 // table: order, width, hidden
	CardProperties []string `json:"card_properties"`         // board and list: properties shown
	DateProperty   string   `json:"date_property,omitempty"` // calendar
}

type View struct {
	ID         uuid.UUID  `json:"id"`
	DatabaseID uuid.UUID  `json:"database_id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Position   string     `json:"position"`
	Config     ViewConfig `json:"config"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

var viewTypes = []string{"table", "board", "list", "calendar"}

// filterOps lists the operators each property type accepts.
var filterOps = map[string][]string{
	"title":        {"contains", "not_contains", "is", "is_not", "starts_with", "is_empty", "is_not_empty"},
	"text":         {"contains", "not_contains", "is", "is_not", "starts_with", "is_empty", "is_not_empty"},
	"url":          {"contains", "not_contains", "is", "is_not", "is_empty", "is_not_empty"},
	"email":        {"contains", "not_contains", "is", "is_not", "is_empty", "is_not_empty"},
	"number":       {"eq", "neq", "gt", "gte", "lt", "lte", "is_empty", "is_not_empty"},
	"select":       {"is", "is_not", "is_empty", "is_not_empty"},
	"status":       {"is", "is_not", "is_empty", "is_not_empty"},
	"multi_select": {"contains", "not_contains", "is_empty", "is_not_empty"},
	"person":       {"contains", "not_contains", "is_empty", "is_not_empty"},
	"date":         {"is", "before", "after", "on_or_before", "on_or_after", "is_empty", "is_not_empty"},
	"checkbox":     {"is"},
}

func loadViews(ctx context.Context, q db.Querier, dbID uuid.UUID) ([]View, error) {
	rows, err := q.Query(ctx, `SELECT id, database_id, name, type, position, config, created_at, updated_at
		FROM docs_views WHERE database_id = $1 ORDER BY position, id`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		var v View
		var cfg []byte
		if err := rows.Scan(&v.ID, &v.DatabaseID, &v.Name, &v.Type, &v.Position, &cfg, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cfg, &v.Config)
		v.Config = normConfig(v.Config)
		out = append(out, v)
	}
	return out, rows.Err()
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

// cleanConfig drops references to properties the schema no longer has (or
// that changed type) and fills what the view type needs.
func cleanConfig(schema []Property, typ string, c ViewConfig) ViewConfig {
	out := ViewConfig{Filters: []Filter{}, Sorts: []Sort{}, Columns: []Column{}, CardProperties: []string{}}
	for _, f := range c.Filters {
		p, ok := findProp(schema, f.Property)
		if ok && slices.Contains(filterOps[p.Type], f.Op) && len(f.Value) <= 1024 && len(out.Filters) < 20 {
			out.Filters = append(out.Filters, f)
		}
	}
	for _, srt := range c.Sorts {
		_, ok := findProp(schema, srt.Property)
		if (ok || srt.Property == "created_time" || srt.Property == "last_edited_time") && len(out.Sorts) < 5 {
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
				col.Width = min(max(col.Width, 80), 800)
			}
			out.Columns = append(out.Columns, col)
		}
	}
	for _, id := range c.CardProperties {
		if _, ok := findProp(schema, id); ok && !slices.Contains(out.CardProperties, id) {
			out.CardProperties = append(out.CardProperties, id)
		}
	}
	if p, ok := findProp(schema, c.GroupBy); ok && (p.Type == "select" || p.Type == "status") {
		out.GroupBy = c.GroupBy
	}
	if p, ok := findProp(schema, c.DateProperty); ok && p.Type == "date" {
		out.DateProperty = c.DateProperty
	}
	if typ == "board" && out.GroupBy == "" {
		for _, p := range schema {
			if p.Type == "status" || p.Type == "select" {
				out.GroupBy = p.ID
				break
			}
		}
	}
	if typ == "calendar" && out.DateProperty == "" {
		for _, p := range schema {
			if p.Type == "date" {
				out.DateProperty = p.ID
				break
			}
		}
	}
	return out
}

type viewReq struct {
	Name     *string     `json:"name"`
	Type     string      `json:"type"`
	Config   *ViewConfig `json:"config"`
	Position string      `json:"position"`
}

// createView: POST /docs/databases/{id}/views (edit). A board without a
// select or status property gets a Status property; a calendar without a
// date property gets a Date property.
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
		return httpx.BadRequest("type must be table, board, list or calendar.")
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
	name := strings.ToUpper(req.Type[:1]) + req.Type[1:]
	if req.Name != nil {
		if name = strings.TrimSpace(cleanText(*req.Name)); name == "" || utf8.RuneCountInString(name) > 100 {
			return httpx.BadRequest("Give the view a name under 100 characters.")
		}
	}
	cfg := ViewConfig{}
	if req.Config != nil {
		cfg = *req.Config
	}
	var view View
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := loadPage(ctx, tx, id, true)
		if err != nil {
			return err
		}
		var n int
		var last string
		if err := tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(position), '') FROM docs_views WHERE database_id = $1`, id).Scan(&n, &last); err != nil {
			return err
		}
		if n >= maxViews {
			return httpx.Conflict(fmt.Sprintf("A database has up to %d views.", maxViews))
		}
		schema := cur.Schema
		needs := func(types ...string) bool {
			return !slices.ContainsFunc(schema, func(p Property) bool { return slices.Contains(types, p.Type) })
		}
		changed := false
		if req.Type == "board" && needs("select", "status") {
			schema, changed = append(schema, statusProperty("Status")), true
		}
		if req.Type == "calendar" && needs("date") {
			schema, changed = append(schema, Property{ID: shortID(), Name: "Date", Type: "date"}), true
		}
		if changed {
			raw, _ := json.Marshal(schema)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, id, raw); err != nil {
				return err
			}
		}
		cfg = cleanConfig(schema, req.Type, cfg)
		b, _ := json.Marshal(cfg)
		return tx.QueryRow(ctx, `
			INSERT INTO docs_views (database_id, name, type, position, config, created_by) VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, database_id, name, type, position, created_at, updated_at`,
			id, name, req.Type, keyBetween(last, ""), b, me).Scan(&view.ID, &view.DatabaseID, &view.Name, &view.Type, &view.Position, &view.CreatedAt, &view.UpdatedAt)
	})
	if err != nil {
		return err
	}
	view.Config = cfg
	s.publish(ctx, pageTopic(id), EventSchema, map[string]any{"database_id": id})
	httpx.JSON(w, http.StatusCreated, view)
	return nil
}

// viewFor loads a view and checks the caller's level on its database.
func (s *Service) viewFor(ctx context.Context, me, viewID uuid.UUID, min access.Level) (*View, *Page, error) {
	var dbID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT database_id FROM docs_views WHERE id = $1`, viewID).Scan(&dbID)
	if db.IsNoRows(err) {
		return nil, nil, httpx.NotFound("View not found.")
	}
	if err != nil {
		return nil, nil, err
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, dbID, min)
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusNotFound {
			return nil, nil, httpx.NotFound("View not found.")
		}
		return nil, nil, err
	}
	views, err := loadViews(ctx, s.d.Pool, dbID)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range views {
		if v.ID == viewID {
			return &v, dbp, nil
		}
	}
	return nil, nil, httpx.NotFound("View not found.")
}

// updateView: PATCH /docs/views/{viewID} (edit on the database).
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
	v, dbp, err := s.viewFor(ctx, me, viewID, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(dbp, true); err != nil {
		return err
	}
	if req.Name != nil {
		name := strings.TrimSpace(cleanText(*req.Name))
		if name == "" || utf8.RuneCountInString(name) > 100 {
			return httpx.BadRequest("Give the view a name under 100 characters.")
		}
		v.Name = name
	}
	if req.Type != "" {
		if !slices.Contains(viewTypes, req.Type) {
			return httpx.BadRequest("type must be table, board, list or calendar.")
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
	v.Config = cleanConfig(dbp.Schema, v.Type, v.Config)
	b, _ := json.Marshal(v.Config)
	if err := s.d.Pool.QueryRow(ctx, `UPDATE docs_views SET name = $2, type = $3, config = $4, position = $5, updated_at = now() WHERE id = $1 RETURNING updated_at`,
		v.ID, v.Name, v.Type, b, v.Position).Scan(&v.UpdatedAt); err != nil {
		return err
	}
	s.publish(ctx, pageTopic(v.DatabaseID), EventSchema, map[string]any{"database_id": v.DatabaseID})
	httpx.OK(w, v)
	return nil
}

// deleteView: DELETE /docs/views/{viewID} (edit; a database keeps at least one).
func (s *Service) deleteView(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	viewID, err := httpx.URLParamUUID(r, "viewID")
	if err != nil {
		return err
	}
	v, dbp, err := s.viewFor(ctx, me, viewID, access.Edit)
	if err != nil {
		return err
	}
	if err := editable(dbp, true); err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM docs_views WHERE id = $1 AND (SELECT count(*) FROM docs_views WHERE database_id = $2) > 1`, v.ID, v.DatabaseID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.Conflict("A database needs at least one view.")
	}
	s.publish(ctx, pageTopic(v.DatabaseID), EventSchema, map[string]any{"database_id": v.DatabaseID})
	httpx.NoContent(w)
	return nil
}
