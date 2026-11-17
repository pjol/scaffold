package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Database agent tools. They run as the caller with access.ViaAgent in every
// query: rows, related rows, formulas and rollups see exactly what the
// person can open, capped by their agent scope. Writes are one row at a
// time with Target (docs.page: the row, or the database a row is added to)
// and an exact Preview of the stored values. Formulas, rollups, created and
// edited values, unique IDs and buttons are read-only for agents.

func (s *Service) databaseTools() []app.Tool {
	target := func(field string) func(json.RawMessage) (string, string) {
		return func(input json.RawMessage) (string, string) {
			var m map[string]any
			_ = json.Unmarshal(input, &m)
			id, _ := m[field].(string)
			return Kind, id
		}
	}
	filterSchema := `{"type":"array","maxItems":20,"items":{"type":"object","properties":{"property":{"type":"string"},"op":{"type":"string"},"value":{},
		"conj":{"type":"string","enum":["and","or"]},"filters":{"type":"array"}}}}`
	return []app.Tool{
		{
			Name: "docs_query_database",
			Description: "List rows of a database with their property values (formulas and rollups included), optionally filtered and sorted. " +
				"filters: [{property: name or id, op, value}] or groups {conj: and|or, filters: [...]} (three deep); filter_conj joins the top level (default and). " +
				"Ops by type: text/url/email/phone is, is_not, contains, not_contains, starts_with, ends_with; number and ID eq, neq, gt, gte, lt, lte (amounts in major units); " +
				"select and status is, is_not (value: option name); multi_select, person, created_by, last_edited_by and relation contains, not_contains " +
				"(option name; person \"me\", an id or an email; relation a row id or title); date, created_time, last_edited_time is, before, after, on_or_before, on_or_after " +
				"(YYYY-MM-DD, today, tomorrow, yesterday, one_week_ago, one_week_from_now, one_month_ago, one_month_from_now) and is_within " +
				"(past_week, past_month, past_year, next_week, next_month, next_year, this_week, this_month, this_year); checkbox is (true/false); " +
				"formulas and rollups by their result type; any type is_empty, is_not_empty. sorts: [{property, direction: asc|desc}] (also created_time, last_edited_time). " +
				"Pass view_id to use a saved view's filters and sorts. Related rows the user can't open are never shown or counted. The answer lists the database's row templates.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"database_id":{"type":"string","format":"uuid"},"view_id":{"type":"string","format":"uuid"},
				"filters":` + filterSchema + `,"filter_conj":{"type":"string","enum":["and","or"]},
				"sorts":{"type":"array","maxItems":5,"items":{"type":"object","properties":{"property":{"type":"string"},"direction":{"type":"string","enum":["asc","desc"]}},"required":["property"]}},
				"q":{"type":"string","maxLength":200},"limit":{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string"}},"required":["database_id"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolQueryDatabase,
		},
		{
			Name: "docs_set_property",
			Description: "Set one property of a database row the user can edit. value by type: text/url/email/phone a string, number a number (amounts in major units: 12.50), " +
				"select/status an option name, multi_select a list of option names, date \"YYYY-MM-DD\", \"YYYY-MM-DDTHH:MM\" or {start, end, tz}, " +
				"person a list of user ids or emails, checkbox true/false, files a list of {url, name}, relation a list of row ids or titles of rows in the related database " +
				"the user can open, the title property a string; null clears it. Formulas, rollups, created/edited values, IDs and buttons can't be set.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"row_id":{"type":"string","format":"uuid"},"property":{"type":"string"},"value":{}},"required":["row_id","property","value"],"additionalProperties":false}`),
			Target:      target("row_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				pl, err := s.planSetProperty(ctx, caller, input, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return app.ToolPreview{Summary: fmt.Sprintf("Set %s of “%s”", pl.prop.Name, displayTitle(pl.row.Title)),
					Before: pl.before, After: pl.after, Link: "/docs/" + pl.row.ID.String()}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				pl, err := s.planSetProperty(ctx, caller, input, access.Edit)
				if err != nil {
					return nil, err
				}
				req := rowValuesReq{Values: map[string]json.RawMessage{pl.prop.ID: pl.raw}}
				if _, err := s.setRow(ctx, caller.UserID, pl.row, req); err != nil {
					return nil, err
				}
				return map[string]any{"row_id": pl.row.ID, "property": pl.prop.Name, "value": pl.after}, nil
			},
		},
		{
			Name: "docs_add_row",
			Description: "Add a row to a database the user can edit: a title, property values by name (as docs_set_property takes them) and optionally a row template " +
				"(id or name, from docs_query_database) whose values and content it starts from.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"database_id":{"type":"string","format":"uuid"},"title":{"type":"string","maxLength":300},
				"values":{"type":"object"},"template":{"type":"string"}},"required":["database_id"],"additionalProperties":false}`),
			Target: target("database_id"),
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				pl, err := s.planAddRow(ctx, caller, input, access.View)
				if err != nil {
					return app.ToolPreview{}, err
				}
				summary := fmt.Sprintf("Add “%s” to %s", displayTitle(pl.title), displayTitle(pl.db.Title))
				if pl.template != nil {
					summary += " from the template “" + displayTitle(pl.template.Title) + "”"
				}
				return app.ToolPreview{Summary: summary, After: pl.after, Link: "/docs/" + pl.db.ID.String()}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				pl, err := s.planAddRow(ctx, caller, input, access.Edit)
				if err != nil {
					return nil, err
				}
				req := createRowReq{Title: pl.title, Values: pl.values}
				if pl.template != nil {
					req.TemplateID = &pl.template.ID
				}
				row, err := s.newRow(ctx, caller.UserID, pl.db.ID, req)
				if err != nil {
					return nil, err
				}
				return map[string]any{"row_id": row.ID, "title": displayTitle(row.Title), "link": "/docs/" + row.ID.String()}, nil
			},
		},
	}
}

// ---- query ---------------------------------------------------------------------------

func (s *Service) toolQueryDatabase(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		DatabaseID uuid.UUID  `json:"database_id"`
		ViewID     *uuid.UUID `json:"view_id"`
		Filters    []Filter   `json:"filters"`
		FilterConj string     `json:"filter_conj"`
		Sorts      []Sort     `json:"sorts"`
		Q          string     `json:"q"`
		Limit      int        `json:"limit"`
		Cursor     string     `json:"cursor"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	dbp, err := requirePage(ctx, s.d.Pool, caller.UserID, in.DatabaseID, access.View)
	if err != nil {
		return nil, err
	}
	if dbp.Kind != "database" {
		return nil, errNotDatabase
	}
	if in.Limit <= 0 || in.Limit > 200 {
		in.Limit = 50
	}
	rq := rowQuery{db: dbp, limit: in.Limit, cursor: in.Cursor, search: in.Q, me: caller.UserID, agent: access.ViaAgent(ctx), noMeta: true}
	if in.ViewID != nil {
		views, err := s.viewsFor(ctx, dbp.ID, in.ViewID.String())
		if err != nil {
			return nil, err
		}
		for i, v := range views {
			if v.ID == *in.ViewID {
				rq.filters, rq.conj, rq.sorts, rq.view = v.Config.Filters, v.Config.FilterConj, v.Config.Sorts, &views[i]
			}
		}
	}
	if len(in.Filters) > 0 {
		resolved, err := s.agentFilters(ctx, caller, dbp, in.Filters, 1)
		if err != nil {
			return nil, err
		}
		n := 0
		rq.filters, rq.conj = cleanFilters(dbp.Schema, resolved, 1, &n), in.FilterConj
	}
	if len(in.Sorts) > 0 {
		rq.sorts = nil
		for _, srt := range in.Sorts {
			if srt.Property == "created_time" || srt.Property == "last_edited_time" {
				rq.sorts = append(rq.sorts, srt)
				continue
			}
			p, ok := findPropByName(dbp.Schema, srt.Property)
			if !ok {
				return nil, httpx.BadRequest("No property called " + srt.Property + ".")
			}
			rq.sorts = append(rq.sorts, Sort{Property: p.ID, Direction: srt.Direction})
		}
	}
	res, err := s.queryRows(ctx, rq)
	if err != nil {
		return nil, err
	}
	props := make([]map[string]any, 0, len(dbp.Schema))
	for _, p := range dbp.Schema {
		entry := map[string]any{"name": p.Name, "type": p.Type}
		if len(p.Options) > 0 {
			opts := make([]string, len(p.Options))
			for i, o := range p.Options {
				opts[i] = o.Name
			}
			entry["options"] = opts
		}
		if p.Formula != nil {
			entry["formula"], entry["result"] = p.Formula.Expression, p.Formula.Type
		}
		if p.Rollup != nil {
			entry["rollup"] = rollupLabel(p.Rollup.Function)
		}
		if p.Relation != nil {
			entry["related_database_id"] = p.Relation.DatabaseID
		}
		if !writable(p) && p.Type != "title" {
			entry["read_only"] = true
		}
		props = append(props, entry)
	}
	rows := make([]map[string]any, 0, len(res.Items))
	for _, r := range res.Items {
		values := map[string]any{}
		for _, p := range dbp.Schema {
			if p.Type == "title" || p.Type == "button" {
				continue
			}
			if v := csvText(p, r, res.Refs); v != "" {
				values[p.Name] = v
			}
		}
		rows = append(rows, map[string]any{"id": r.ID, "title": displayTitle(r.Title), "link": "/docs/" + r.ID.String(), "values": values})
	}
	templates := []map[string]any{}
	trows, err := s.d.Pool.Query(ctx, `SELECT id, title FROM docs_pages WHERE parent_id = $1 AND is_template AND is_row AND deleted_at IS NULL
		AND access_level($2, 'docs.page', id, $3) >= 1 ORDER BY position, id`, dbp.ID, caller.UserID, access.ViaAgent(ctx))
	if err == nil {
		for trows.Next() {
			var id uuid.UUID
			var t string
			if trows.Scan(&id, &t) == nil {
				templates = append(templates, map[string]any{"id": id, "name": displayTitle(t)})
			}
		}
		trows.Close()
	}
	out := map[string]any{"database": displayTitle(dbp.Title), "properties": props, "rows": rows, "next_cursor": res.NextCursor, "templates": templates}
	if res.Total != nil {
		out["total"] = *res.Total
	}
	return out, nil
}

// agentFilters turns names into ids: properties, options, people (email or
// "me") and related rows (title, among rows the caller can open).
func (s *Service) agentFilters(ctx context.Context, caller *auth.Identity, dbp *Page, in []Filter, depth int) ([]Filter, error) {
	out := make([]Filter, 0, len(in))
	for _, f := range in {
		if f.isGroup() {
			if depth >= maxFilterDepth {
				return nil, httpx.BadRequest("Filter groups nest three deep at most.")
			}
			sub, err := s.agentFilters(ctx, caller, dbp, f.Filters, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, Filter{Conj: f.Conj, Filters: sub})
			continue
		}
		p, ok := findPropByName(dbp.Schema, f.Property)
		if !ok {
			return nil, httpx.BadRequest("No property called " + f.Property + ".")
		}
		f.Property = p.ID
		var v string
		if json.Unmarshal(f.Value, &v) == nil && v != "" {
			switch p.Type {
			case "select", "status", "multi_select":
				for _, o := range p.Options {
					if strings.EqualFold(o.Name, v) {
						f.Value, _ = json.Marshal(o.ID)
					}
				}
			case "person", "created_by", "last_edited_by":
				if v != "me" {
					if _, err := uuid.Parse(v); err != nil {
						id, err := s.memberByEmail(ctx, dbp.OrgID, v)
						if err != nil {
							return nil, err
						}
						f.Value, _ = json.Marshal(id.String())
					}
				}
			case "relation":
				if _, err := uuid.Parse(v); err != nil && p.Relation != nil {
					ids, err := s.rowsByTitle(ctx, caller.UserID, p.Relation.DatabaseID, []string{v})
					if err != nil {
						return nil, err
					}
					f.Value, _ = json.Marshal(ids[0])
				}
			}
		}
		out = append(out, f)
	}
	return out, nil
}

func (s *Service) memberByEmail(ctx context.Context, orgID uuid.UUID, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT u.id FROM users u JOIN org_members m ON m.user_id = u.id AND m.org_id = $1 WHERE lower(u.email) = lower($2)`, orgID, strings.TrimSpace(email)).Scan(&id)
	if err != nil {
		return uuid.Nil, httpx.BadRequest("No one in this " + orgs.Noun() + " has the email " + email + ".")
	}
	return id, nil
}

// rowsByTitle resolves titles to row ids of a database among rows the caller
// can open (agent-capped); an unknown or ambiguous title is refused.
func (s *Service) rowsByTitle(ctx context.Context, me, dbID uuid.UUID, titles []string) ([]string, error) {
	out := make([]string, 0, len(titles))
	for _, t := range titles {
		rows, err := s.d.Pool.Query(ctx, `SELECT id FROM docs_pages WHERE parent_id = $1 AND is_row AND NOT is_template AND deleted_at IS NULL
			AND lower(title) = lower($2) AND access_level($3, 'docs.page', id, $4) >= 1 LIMIT 2`, dbID, strings.TrimSpace(t), me, access.ViaAgent(ctx))
		if err != nil {
			return nil, err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		switch len(ids) {
		case 0:
			return nil, httpx.BadRequest("No row called “" + t + "” in the related database (that you can open).")
		case 2:
			return nil, httpx.BadRequest("More than one row is called “" + t + "”. Use its id.")
		}
		out = append(out, ids[0].String())
	}
	return out, nil
}

// ---- set a property ----------------------------------------------------------------

type propPlan struct {
	row           *Page
	prop          Property
	raw           json.RawMessage
	before, after string
}

// agentValue converts an agent's value for p (names, emails, titles, major
// units) into what the REST route stores.
func (s *Service) agentValue(ctx context.Context, caller *auth.Identity, orgID uuid.UUID, p Property, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return raw, nil
	}
	if !writable(p) && p.Type != "title" {
		return nil, httpx.BadRequest(p.Name + " is worked out for you and can't be set.")
	}
	list := func() []string {
		var l []string
		if json.Unmarshal(raw, &l) != nil {
			var one string
			if json.Unmarshal(raw, &one) == nil {
				l = []string{one}
			}
		}
		return l
	}
	optionID := func(v string) (string, error) {
		for _, o := range p.Options {
			if o.ID == v || strings.EqualFold(o.Name, strings.TrimSpace(v)) {
				return o.ID, nil
			}
		}
		return "", httpx.BadRequest(p.Name + " has no option " + v + ".")
	}
	switch p.Type {
	case "select", "status":
		var v string
		if json.Unmarshal(raw, &v) == nil {
			id, err := optionID(v)
			if err != nil {
				return nil, err
			}
			return json.Marshal(id)
		}
	case "multi_select":
		ids := []string{}
		for _, v := range list() {
			id, err := optionID(v)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return json.Marshal(ids)
	case "date":
		var d string
		if json.Unmarshal(raw, &d) == nil {
			return json.Marshal(map[string]string{"start": relativeDate(d)})
		}
	case "number":
		var n float64
		if json.Unmarshal(raw, &n) == nil {
			return json.Marshal(n * numberScale(p)) // agents name amounts in major units
		}
	case "person":
		ids := []string{}
		for _, v := range list() {
			if _, err := uuid.Parse(v); err == nil {
				ids = append(ids, v)
				continue
			}
			id, err := s.memberByEmail(ctx, orgID, v)
			if err != nil {
				return nil, err
			}
			ids = append(ids, id.String())
		}
		return json.Marshal(ids)
	case "relation":
		var ids, titles []string
		for _, v := range list() {
			if _, err := uuid.Parse(v); err == nil {
				ids = append(ids, v)
			} else {
				titles = append(titles, v)
			}
		}
		if len(titles) > 0 && p.Relation != nil {
			more, err := s.rowsByTitle(ctx, caller.UserID, p.Relation.DatabaseID, titles)
			if err != nil {
				return nil, err
			}
			ids = append(ids, more...)
		}
		if ids == nil {
			ids = []string{}
		}
		return json.Marshal(ids)
	}
	return raw, nil
}

// planSetProperty resolves names to ids and validates the value, so Preview
// shows what will be stored.
func (s *Service) planSetProperty(ctx context.Context, caller *auth.Identity, input json.RawMessage, min access.Level) (*propPlan, error) {
	var in struct {
		RowID    uuid.UUID       `json:"row_id"`
		Property string          `json:"property"`
		Value    json.RawMessage `json:"value"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	row, err := requirePage(ctx, s.d.Pool, caller.UserID, in.RowID, min)
	if err != nil {
		return nil, err
	}
	if !row.IsRow || row.ParentID == nil {
		return nil, httpx.BadRequest("That page isn't a database row.")
	}
	dbp, err := loadPage(ctx, s.d.Pool, *row.ParentID, false)
	if err != nil {
		return nil, err
	}
	prop, found := findPropByName(dbp.Schema, in.Property)
	if !found {
		names := make([]string, len(dbp.Schema))
		for i, p := range dbp.Schema {
			names[i] = p.Name
		}
		return nil, httpx.BadRequest("No property called " + in.Property + ". It has: " + strings.Join(names, ", ") + ".")
	}
	raw, err := s.agentValue(ctx, caller, row.OrgID, prop, in.Value)
	if err != nil {
		return nil, err
	}
	pl := &propPlan{row: row, prop: prop, raw: raw}
	if prop.Type == "title" {
		var t string
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, httpx.BadRequest("The title is text.")
		}
		clean, err := cleanTitle(t)
		if err != nil {
			return nil, err
		}
		pl.before, pl.after = row.Title, clean
		return pl, nil
	}
	var after any
	if prop.Type == "relation" {
		ids, err := s.cleanRelation(ctx, s.d.Pool, caller.UserID, access.ViaAgent(ctx), prop, row.Props[prop.ID], raw)
		if err != nil {
			return nil, err
		}
		after = toAny(ids)
	} else {
		v, err := s.cleanRowValue(ctx, row.OrgID, row.ID, prop, raw)
		if err != nil {
			return nil, err
		}
		after = toAny(v)
	}
	refs, err := s.valueRefs(ctx, caller, prop, row.Props[prop.ID], after)
	if err != nil {
		return nil, err
	}
	pl.before, pl.after = valueText(prop, row.Props[prop.ID], refs), valueText(prop, after, refs)
	if prop.Type == "relation" {
		// Related rows the person can't open stay linked, unnamed.
		ids, named := relationValues(after), 0
		for _, id := range ids {
			if r, ok := refs.Pages[id.String()]; ok && r.Title != nil {
				named++
			}
		}
		if hidden := len(ids) - named; hidden > 0 {
			pl.after = strings.TrimPrefix(pl.after+fmt.Sprintf(", and %d you can't open", hidden), ", ")
		}
	}
	return pl, nil
}

// toAny round-trips a value through JSON (stored values read as JSON does).
func toAny(v any) any {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// valueRefs names people and related rows (only rows the caller can open)
// in a before and after value.
func (s *Service) valueRefs(ctx context.Context, caller *auth.Identity, p Property, values ...any) (Refs, error) {
	var people, pages []uuid.UUID
	for _, v := range values {
		switch p.Type {
		case "person":
			people = append(people, relationValues(v)...)
		case "relation":
			pages = append(pages, relationValues(v)...)
		}
	}
	return s.resolveRefs(ctx, caller.UserID, true, people, pages)
}

// ---- add a row ---------------------------------------------------------------------

type addRowPlan struct {
	db       *Page
	title    string
	values   map[string]json.RawMessage
	template *Page
	after    string
}

func (s *Service) planAddRow(ctx context.Context, caller *auth.Identity, input json.RawMessage, min access.Level) (*addRowPlan, error) {
	var in struct {
		DatabaseID uuid.UUID                  `json:"database_id"`
		Title      string                     `json:"title"`
		Values     map[string]json.RawMessage `json:"values"`
		Template   string                     `json:"template"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	dbp, err := requirePage(ctx, s.d.Pool, caller.UserID, in.DatabaseID, min)
	if err != nil {
		return nil, err
	}
	if dbp.Kind != "database" {
		return nil, errNotDatabase
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return nil, err
	}
	pl := &addRowPlan{db: dbp, title: title, values: map[string]json.RawMessage{}}
	if t := strings.TrimSpace(in.Template); t != "" {
		var id uuid.UUID
		err := s.d.Pool.QueryRow(ctx, `SELECT id FROM docs_pages WHERE parent_id = $1 AND is_template AND is_row AND deleted_at IS NULL
			AND (id::text = $2 OR lower(title) = lower($2)) AND access_level($3, 'docs.page', id, $4) >= 1 ORDER BY position LIMIT 1`,
			dbp.ID, t, caller.UserID, access.ViaAgent(ctx)).Scan(&id)
		if err != nil {
			return nil, httpx.BadRequest("No template called “" + t + "” in this database.")
		}
		if pl.template, err = loadPage(ctx, s.d.Pool, id, false); err != nil {
			return nil, err
		}
		if pl.title == "" {
			pl.title = pl.template.Title
		}
	}
	var lines []string
	for name, raw := range in.Values {
		p, ok := findPropByName(dbp.Schema, name)
		if !ok {
			return nil, httpx.BadRequest("No property called " + name + ".")
		}
		if p.Type == "title" {
			var t string
			if json.Unmarshal(raw, &t) == nil && pl.title == "" {
				pl.title = t
			}
			continue
		}
		v, err := s.agentValue(ctx, caller, dbp.OrgID, p, raw)
		if err != nil {
			return nil, err
		}
		var stored any
		if p.Type == "relation" {
			ids, err := s.cleanRelation(ctx, s.d.Pool, caller.UserID, access.ViaAgent(ctx), p, nil, v)
			if err != nil {
				return nil, err
			}
			stored = toAny(ids)
		} else {
			cv, err := s.cleanValue(ctx, dbp.OrgID, p, v)
			if err != nil {
				return nil, err
			}
			stored = toAny(cv)
		}
		pl.values[p.ID] = v
		refs, err := s.valueRefs(ctx, caller, p, stored)
		if err != nil {
			return nil, err
		}
		if t := valueText(p, stored, refs); t != "" {
			lines = append(lines, p.Name+": "+t)
		}
	}
	lines = append([]string{"Title: " + displayTitle(pl.title)}, lines...)
	pl.after = strings.Join(lines, "\n")
	return pl, nil
}
