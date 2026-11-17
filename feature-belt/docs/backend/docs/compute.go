package docs

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
)

// Computed values (formulas, rollups, relation titles) are worked out here,
// per viewer, every time rows are read: a related row the viewer can't open
// is never loaded, so it can't be counted, titled or rolled up. Related rows
// are loaded in batches per (database, relation) as values ask for them.

// viewer is who reads: a member (me, capped as an agent when agent) or a
// public link viewer (token; me is uuid.Nil).
type viewer struct {
	me    uuid.UUID
	agent bool
	token string
	loc   *time.Location
}

func (v viewer) public() bool { return v.token != "" }

// crow is a row as the computer holds it.
type crow struct {
	ID, DB    uuid.UUID
	Title     string
	Icon      *string
	Cover     *Cover
	Props     map[string]any
	Position  string
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy *uuid.UUID
	UpdatedBy *uuid.UUID
	Level     access.Level
}

type relKey struct {
	db   uuid.UUID
	prop string
}

type valKey struct {
	row  uuid.UUID
	prop string
}

// computer evaluates computed values for one request.
type computer struct {
	s   *Service
	ctx context.Context
	v   viewer
	now time.Time

	schemas  map[uuid.UUID][]Property
	dbOpen   map[uuid.UUID]bool // public viewers: databases the link opens
	rows     map[uuid.UUID]*crow
	noAccess map[uuid.UUID]bool // related rows that exist but the viewer can't open
	byDB     map[uuid.UUID][]uuid.UUID
	relDone  map[relKey]map[uuid.UUID]bool

	formulas map[valKey]*Formula // key: database id + property id
	fErrs    map[valKey]error
	cache    map[valKey]fValue
	busy     map[valKey]bool
	depth    int

	names  map[uuid.UUID]string
	emails map[uuid.UUID]string
	err    error // the first database error (values read as empty)
}

func (s *Service) newComputer(ctx context.Context, v viewer) *computer {
	if v.loc == nil {
		v.loc = time.UTC
	}
	return &computer{s: s, ctx: ctx, v: v, now: time.Now().In(v.loc),
		schemas: map[uuid.UUID][]Property{}, dbOpen: map[uuid.UUID]bool{}, rows: map[uuid.UUID]*crow{},
		noAccess: map[uuid.UUID]bool{}, byDB: map[uuid.UUID][]uuid.UUID{}, relDone: map[relKey]map[uuid.UUID]bool{},
		formulas: map[valKey]*Formula{}, fErrs: map[valKey]error{}, cache: map[valKey]fValue{}, busy: map[valKey]bool{},
		names: map[uuid.UUID]string{}, emails: map[uuid.UUID]string{}}
}

func (c *computer) fail(err error) {
	if c.err == nil && err != nil {
		c.err = err
	}
}

// setSchema registers a database's schema (callers that already loaded it).
func (c *computer) setSchema(dbID uuid.UUID, schema []Property) { c.schemas[dbID] = schema }

// addRow registers a row the viewer can open.
func (c *computer) addRow(r *crow) {
	if _, ok := c.rows[r.ID]; ok {
		return
	}
	c.rows[r.ID] = r
	c.byDB[r.DB] = append(c.byDB[r.DB], r.ID)
}

// Schema implements fSchemas: any database's properties (a schema isn't
// secret: a row's page shows its database's properties).
func (c *computer) Schema(dbID uuid.UUID) ([]Property, bool) {
	if s, ok := c.schemas[dbID]; ok {
		return s, s != nil
	}
	var raw []byte
	err := c.s.d.Pool.QueryRow(c.ctx, `SELECT schema FROM docs_pages WHERE id = $1 AND kind = 'database'`, dbID).Scan(&raw)
	if err != nil {
		c.schemas[dbID] = nil
		return nil, false
	}
	var schema []Property
	_ = json.Unmarshal(raw, &schema)
	c.schemas[dbID] = schema
	return schema, true
}

func (c *computer) Now() time.Time           { return c.now }
func (c *computer) Location() *time.Location { return c.v.loc }

func (c *computer) PersonName(id uuid.UUID) string {
	if n, ok := c.names[id]; ok {
		return n
	}
	c.loadPeople([]uuid.UUID{id})
	return c.names[id]
}

// PersonEmail is shown to org owners and admins only (the directory's rule).
func (c *computer) PersonEmail(id uuid.UUID) string {
	if c.v.public() || c.v.me == uuid.Nil {
		return ""
	}
	if _, ok := c.emails[id]; !ok {
		c.loadPeople([]uuid.UUID{id})
	}
	var shared bool
	err := c.s.d.Pool.QueryRow(c.ctx, `
		SELECT EXISTS (SELECT 1 FROM org_members a JOIN org_members b ON b.org_id = a.org_id
		WHERE a.user_id = $1 AND a.role IN ('owner', 'admin') AND b.user_id = $2)`, c.v.me, id).Scan(&shared)
	if err != nil || !shared {
		return ""
	}
	return c.emails[id]
}

func (c *computer) loadPeople(ids []uuid.UUID) {
	var want []uuid.UUID
	for _, id := range ids {
		if _, ok := c.names[id]; !ok {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return
	}
	rows, err := c.s.d.Pool.Query(c.ctx, `SELECT id, CASE WHEN status = 'deleted' THEN 'Deleted user' ELSE display_name END, email FROM users WHERE id = ANY ($1)`, want)
	if err != nil {
		c.fail(err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var name string
		var email *string
		if err := rows.Scan(&id, &name, &email); err != nil {
			c.fail(err)
			return
		}
		c.names[id] = name
		if email != nil {
			c.emails[id] = *email
		}
	}
	for _, id := range want {
		if _, ok := c.names[id]; !ok {
			c.names[id] = "Deleted user"
		}
	}
}

// dbVisible says whether a public viewer's link opens a database (members
// are checked per row).
func (c *computer) dbVisible(dbID uuid.UUID) bool {
	if !c.v.public() {
		return true
	}
	if ok, seen := c.dbOpen[dbID]; seen {
		return ok
	}
	lvl, err := access.LinkLevel(c.ctx, c.s.d.Pool, c.v.token, Kind, dbID)
	ok := err == nil && lvl >= access.View
	c.dbOpen[dbID] = ok
	return ok
}

// ensureRelation loads, for every row of db registered so far, the related
// rows of relation p the viewer can open (once per row).
func (c *computer) ensureRelation(dbID uuid.UUID, p Property) {
	if p.Relation == nil {
		return
	}
	k := relKey{dbID, p.ID}
	done := c.relDone[k]
	if done == nil {
		done = map[uuid.UUID]bool{}
		c.relDone[k] = done
	}
	var want []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, rid := range c.byDB[dbID] {
		if done[rid] {
			continue
		}
		done[rid] = true
		for _, id := range relationValues(c.rows[rid].Props[p.ID]) {
			if _, ok := c.rows[id]; ok || c.noAccess[id] || seen[id] {
				continue
			}
			seen[id] = true
			want = append(want, id)
		}
	}
	if len(want) > 0 {
		c.loadRows(p.Relation.DatabaseID, want)
	}
}

// loadRows loads rows of database target by id: those the viewer can open
// become rows; existing ones they can't are marked no access; trashed,
// template or foreign ones are ignored (they read as gone).
func (c *computer) loadRows(target uuid.UUID, ids []uuid.UUID) {
	const cols = `p.id, p.parent_id, p.title, p.icon, p.props, p.position, p.created_at, p.updated_at, p.created_by, p.updated_by`
	for start := 0; start < len(ids); start += 1000 {
		chunk := ids[start:min(start+1000, len(ids))]
		var sql string
		var args []any
		if c.v.public() {
			open := c.dbVisible(target)
			sql = `SELECT ` + cols + `, CASE WHEN $3 AND EXISTS (SELECT 1 FROM access_resources ar WHERE ar.kind = 'docs.page' AND ar.id = p.id AND ar.inherits)
				AND p.public_blocked_at IS NULL THEN 1 ELSE 0 END
				FROM docs_pages p WHERE p.id = ANY ($1) AND p.parent_id = $2 AND p.is_row AND NOT p.is_template AND p.deleted_at IS NULL`
			args = []any{chunk, target, open}
		} else {
			sql = `SELECT ` + cols + `, access_level($3, 'docs.page', p.id, $4)
				FROM docs_pages p WHERE p.id = ANY ($1) AND p.parent_id = $2 AND p.is_row AND NOT p.is_template AND p.deleted_at IS NULL`
			args = []any{chunk, target, c.v.me, c.v.agent}
		}
		rows, err := c.s.d.Pool.Query(c.ctx, sql, args...)
		if err != nil {
			c.fail(err)
			return
		}
		for rows.Next() {
			r := &crow{}
			var props []byte
			var lvl int
			if err := rows.Scan(&r.ID, &r.DB, &r.Title, &r.Icon, &props, &r.Position, &r.CreatedAt, &r.UpdatedAt, &r.CreatedBy, &r.UpdatedBy, &lvl); err != nil {
				rows.Close()
				c.fail(err)
				return
			}
			if lvl < int(access.View) {
				c.noAccess[r.ID] = true
				continue
			}
			r.Props = map[string]any{}
			_ = json.Unmarshal(props, &r.Props)
			r.Level = access.Level(lvl)
			if c.v.public() {
				r.CreatedBy, r.UpdatedBy = nil, nil
			}
			c.addRow(r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			c.fail(err)
			return
		}
	}
}

// visibleRow: a related row the viewer can open (after ensureRelation).
func (c *computer) visibleRow(id uuid.UUID) bool { _, ok := c.rows[id]; return ok }

// Value implements fEnv: a row's value for a property as a formula value.
func (c *computer) Value(dbID, rowID uuid.UUID, p Property) fValue {
	r := c.rows[rowID]
	if r == nil {
		return vEmpty
	}
	switch p.Type {
	case "title":
		if r.Title == "" {
			return vEmpty
		}
		return vText(r.Title)
	case "created_time":
		return fromStored(p, r.CreatedAt, c.v.loc, nil)
	case "last_edited_time":
		return fromStored(p, r.UpdatedAt, c.v.loc, nil)
	case "created_by":
		if r.CreatedBy == nil {
			return vEmpty
		}
		return vPerson(*r.CreatedBy)
	case "last_edited_by":
		if r.UpdatedBy == nil {
			return vEmpty
		}
		return vPerson(*r.UpdatedBy)
	case "relation":
		c.ensureRelation(dbID, p)
		return fromStored(p, r.Props[p.ID], c.v.loc, c.visibleRow)
	case "button":
		return vEmpty
	case "formula", "rollup":
		k := valKey{rowID, p.ID}
		if v, ok := c.cache[k]; ok {
			return v
		}
		// A formula or rollup that reaches itself (through relations) reads
		// as empty there; depth bounds long chains.
		if c.busy[k] || c.depth > 8 {
			return vEmpty
		}
		c.busy[k] = true
		c.depth++
		var v fValue
		if p.Type == "formula" {
			if f := c.formula(dbID, p); f != nil {
				v = f.Eval(c, dbID, rowID)
			}
		} else {
			v = c.rollup(dbID, r, p)
		}
		c.depth--
		delete(c.busy, k)
		c.cache[k] = v
		return v
	}
	return fromStored(p, r.Props[p.ID], c.v.loc, nil)
}

// formula compiles (once) a formula property of a database.
func (c *computer) formula(dbID uuid.UUID, p Property) *Formula {
	k := valKey{dbID, p.ID}
	if f, ok := c.formulas[k]; ok {
		return f
	}
	if p.Formula == nil || p.Formula.Expression == "" {
		c.formulas[k] = nil
		return nil
	}
	f, err := compileFormula(p.Formula.Expression, dbID, p.ID, c)
	if err != nil {
		c.fErrs[k] = err
		f = nil
	}
	c.formulas[k] = f
	return f
}

// rollup aggregates the related rows' values the viewer can open.
func (c *computer) rollup(dbID uuid.UUID, r *crow, p Property) fValue {
	cfg := p.Rollup
	if cfg == nil {
		return vEmpty
	}
	schema, _ := c.Schema(dbID)
	rel, ok := findProp(schema, cfg.Relation)
	if !ok || rel.Type != "relation" || rel.Relation == nil {
		return vEmpty
	}
	target := rel.Relation.DatabaseID
	tschema, _ := c.Schema(target)
	tp, ok := findProp(tschema, cfg.Property)
	if !ok {
		return vEmpty
	}
	c.ensureRelation(dbID, rel)
	var vals []fValue
	for _, id := range relationValues(r.Props[rel.ID]) {
		tr := c.rows[id]
		if tr == nil || tr.DB != target {
			continue
		}
		vals = append(vals, c.Value(target, id, tp))
	}
	return aggregate(cfg.Function, vals)
}

// ---- output ------------------------------------------------------------------------

// overlay returns a row's props with its computed values (formulas and
// rollups as JSON) for the viewer. Stored props are copied, never changed.
func (c *computer) overlay(dbID uuid.UUID, r *crow) map[string]any {
	out := make(map[string]any, len(r.Props)+2)
	for k, v := range r.Props {
		out[k] = v
	}
	schema, _ := c.Schema(dbID)
	for _, p := range schema {
		switch p.Type {
		case "formula", "rollup":
			if v := fValueJSON(c.Value(dbID, r.ID, p)); v != nil {
				out[p.ID] = v
			} else {
				delete(out, p.ID)
			}
		case "created_time", "created_by", "last_edited_time", "last_edited_by", "button":
			delete(out, p.ID)
		}
	}
	return out
}

// refsFor adds what a list of rows' values point at to refs: titles of
// related rows the viewer can open (no title for ones they can't; gone ones
// are left out), people named by computed values, and people in person
// properties (resolved by the caller).
func (c *computer) refsFor(dbID uuid.UUID, rows []*crow, refs *Refs) []uuid.UUID {
	schema, _ := c.Schema(dbID)
	var people []uuid.UUID
	addPage := func(id uuid.UUID) {
		key := id.String()
		if _, ok := refs.Pages[key]; ok {
			return
		}
		if r, ok := c.rows[id]; ok {
			t := displayTitle(r.Title)
			refs.Pages[key] = RefPage{ID: id, Title: &t, Icon: r.Icon, Kind: "page"}
		} else if c.noAccess[id] {
			refs.Pages[key] = RefPage{ID: id, Kind: "page"}
		}
	}
	var walk func(v fValue)
	walk = func(v fValue) {
		switch v.Kind {
		case fPage:
			addPage(v.ID)
		case fPerson:
			people = append(people, v.ID)
		case fList:
			for _, x := range v.List {
				walk(x)
			}
		}
	}
	for _, p := range schema {
		switch p.Type {
		case "relation":
			c.ensureRelation(dbID, p)
			for _, r := range rows {
				for _, id := range relationValues(r.Props[p.ID]) {
					addPage(id)
				}
			}
		case "formula", "rollup":
			for _, r := range rows {
				walk(c.Value(dbID, r.ID, p))
			}
		case "created_by", "last_edited_by":
			for _, r := range rows {
				if r.CreatedBy != nil && p.Type == "created_by" {
					people = append(people, *r.CreatedBy)
				}
				if r.UpdatedBy != nil && p.Type == "last_edited_by" {
					people = append(people, *r.UpdatedBy)
				}
			}
		}
	}
	return people
}

// annotateSchema fills what clients need to show computed properties: each
// rollup's related property (Target) and type, and a formula's compile
// error when a property it uses changed under it.
func (c *computer) annotateSchema(dbID uuid.UUID, schema []Property) []Property {
	out := make([]Property, len(schema))
	copy(out, schema)
	for i, p := range out {
		switch p.Type {
		case "rollup":
			if p.Rollup == nil {
				continue
			}
			rc := *p.Rollup
			if rel, ok := findProp(schema, rc.Relation); ok && rel.Relation != nil {
				if ts, ok := c.Schema(rel.Relation.DatabaseID); ok {
					if tp, ok := findProp(ts, rc.Property); ok {
						tp.Rollup, tp.Formula = nil, nil
						tp.Options = append([]Option(nil), tp.Options...)
						rc.Target = &tp
					}
				}
			}
			out[i].Rollup = &rc
		case "formula":
			if p.Formula == nil || p.Formula.Expression == "" {
				continue
			}
			fc := *p.Formula
			c.formula(dbID, p)
			if err := c.fErrs[valKey{dbID, p.ID}]; err != nil {
				fc.Error = err.Error()
			}
			out[i].Formula = &fc
		}
	}
	return out
}

// pageRow turns a loaded page into a computer row.
func pageRow(p *Page) *crow {
	db := uuid.Nil
	if p.ParentID != nil {
		db = *p.ParentID
	}
	return &crow{ID: p.ID, DB: db, Title: p.Title, Icon: p.Icon, Cover: p.Cover, Props: p.Props, Position: p.Position,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, CreatedBy: p.CreatedBy, UpdatedBy: p.UpdatedBy, Level: p.MyLevel}
}

// withComputed overlays a row page's computed values for the viewer and adds
// related rows' titles to refs (a row's detail and write responses).
func (s *Service) withComputed(ctx context.Context, v viewer, p *Page, schema []Property, refs *Refs) error {
	if !p.IsRow || p.ParentID == nil {
		return nil
	}
	c := s.newComputer(ctx, v)
	c.setSchema(*p.ParentID, schema)
	r := pageRow(p)
	c.addRow(r)
	p.Props = c.overlay(*p.ParentID, r)
	if refs != nil {
		if refs.Pages == nil {
			refs.Pages = map[string]RefPage{}
		}
		if refs.Users == nil {
			refs.Users = map[string]RefUser{}
		}
		people := c.refsFor(*p.ParentID, []*crow{r}, refs)
		if len(people) > 0 {
			more, err := s.resolveRefs(ctx, v.me, !v.public(), people, nil)
			if err != nil {
				return err
			}
			for k, u := range more.Users {
				refs.Users[k] = u
			}
		}
	}
	return c.err
}

// memberViewer is the viewer for a signed-in request.
func memberViewer(ctx context.Context, me uuid.UUID, tz string) viewer {
	return viewer{me: me, agent: access.ViaAgent(ctx), loc: loadTZ(tz)}
}

// loadTZ reads an IANA zone name (UTC when empty or unknown).
func loadTZ(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return time.UTC
	}
	if loc := fLoadTZ(name); loc != nil {
		return loc
	}
	return time.UTC
}
