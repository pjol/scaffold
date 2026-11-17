package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- schema ----------------------------------------------------------------------

type schemaReq struct {
	Properties []Property `json:"properties"`
	TZ         string     `json:"tz"`
}

// putSchema: PUT /docs/databases/{id}/schema (edit) replaces the property
// list. Values of removed properties, removed options and properties whose
// type changed are cleared (text kinds convert, currencies rescale); views
// drop references to removed properties; relations get or lose their other
// side; formulas and rollups are checked and typed; a new unique ID numbers
// the existing rows.
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
	out, err := s.saveSchema(ctx, me, dbp, req.Properties, req.TZ)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// saveSchema validates and stores a database's new properties (the route,
// CSV import and agent tools).
func (s *Service) saveSchema(ctx context.Context, me uuid.UUID, dbp *Page, props []Property, tz string) (*Page, error) {
	schema, err := validateSchema(props)
	if err != nil {
		return nil, err
	}
	agent := access.ViaAgent(ctx)
	var touched []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := loadPage(ctx, tx, dbp.ID, true)
		if err != nil {
			return err
		}
		// Keep property renames flowing into formulas that name them.
		for _, o := range cur.Schema {
			if n, ok := findProp(schema, o.ID); ok && n.Name != o.Name {
				for i, p := range schema {
					if p.Type == "formula" && p.Formula != nil {
						fc := *p.Formula
						fc.Expression = renameFormulaProp(fc.Expression, o.Name, n.Name)
						schema[i].Formula = &fc
					}
				}
			}
		}
		ch, err := s.applyRelations(ctx, tx, me, agent, cur, cur.Schema, &schema)
		if err != nil {
			return err
		}
		touched = ch.touched
		if len(schema) > maxProperties {
			return httpx.BadRequest(fmt.Sprintf("A database has 1 to %d properties.", maxProperties))
		}
		if err := s.checkComputed(ctx, memberViewer(ctx, me, tz), dbp.ID, schema); err != nil {
			return err
		}
		if err := s.migrateValues(ctx, tx, dbp.ID, cur.Schema, schema); err != nil {
			return err
		}
		raw, _ := json.Marshal(schema)
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now(), updated_by = $3 WHERE id = $1`, dbp.ID, raw, me); err != nil {
			return err
		}
		return s.cleanViews(ctx, tx, dbp.ID, schema)
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, pageTopic(dbp.ID), EventSchema, map[string]any{"database_id": dbp.ID})
	for _, t := range dedupeIDs(touched) {
		s.publish(ctx, pageTopic(t), EventSchema, map[string]any{"database_id": t})
	}
	s.notifyRelated(ctx, dbp.ID)
	out, err := loadPage(ctx, s.d.Pool, dbp.ID, false)
	if err != nil {
		return nil, err
	}
	out.MyLevel = dbp.MyLevel
	c := s.newComputer(ctx, memberViewer(ctx, me, tz))
	out.Schema = c.annotateSchema(out.ID, out.Schema)
	return out, nil
}

// migrateValues clears or converts row values the new schema can't hold.
func (s *Service) migrateValues(ctx context.Context, tx pgx.Tx, dbID uuid.UUID, old, next []Property) error {
	textish := func(t string) bool { return t == "text" || t == "url" || t == "email" || t == "phone" }
	for _, o := range old {
		if o.Type == "title" {
			continue
		}
		n, ok := findProp(next, o.ID)
		sameRelation := ok && o.Type == "relation" && n.Type == "relation" && o.Relation != nil && n.Relation != nil && o.Relation.DatabaseID == n.Relation.DatabaseID
		switch {
		case !ok || (n.Type != o.Type && !(textish(n.Type) && textish(o.Type))) || isComputed(n) || (o.Type == "relation" && !sameRelation):
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = props - $2::text WHERE parent_id = $1 AND is_row AND props ? $2::text`, dbID, o.ID); err != nil {
				return err
			}
			if o.Type == "date" {
				if _, err := tx.Exec(ctx, `DELETE FROM docs_row_reminders WHERE property_id = $2 AND row_id IN (SELECT id FROM docs_pages WHERE parent_id = $1)`, dbID, o.ID); err != nil {
					return err
				}
			}
			if o.Type == "unique_id" {
				if _, err := tx.Exec(ctx, `DELETE FROM docs_counters WHERE database_id = $1 AND property_id = $2`, dbID, o.ID); err != nil {
					return err
				}
			}
		case n.Type == "number":
			if from, to := numberScale(o), numberScale(n); from != to {
				if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], to_jsonb(round((props->>$2::text)::numeric * $3::numeric, $4::int)))
					WHERE parent_id = $1 AND is_row AND jsonb_typeof(props->$2::text) = 'number'`, dbID, o.ID, to/from, scaleDigits(n)); err != nil {
					return err
				}
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
	// Number every row for a new unique ID (by creation, oldest first).
	for _, n := range next {
		if n.Type != "unique_id" {
			continue
		}
		if o, ok := findProp(old, n.ID); ok && o.Type == "unique_id" {
			continue
		}
		if err := numberRows(ctx, tx, dbID, n.ID); err != nil {
			return err
		}
	}
	return nil
}

// scaleDigits: decimals kept when rescaling into p's unit (0 for minor
// units, a few otherwise).
func scaleDigits(p Property) int {
	if _, cur := currencyExp(p.NumberFormat); cur {
		return 0
	}
	return 6
}

func optionIDs(p Property) []string {
	out := make([]string, len(p.Options))
	for i, o := range p.Options {
		out[i] = o.ID
	}
	return out
}

// numberRows gives every row (templates too) a unique ID in creation order
// and sets the counter past them.
func numberRows(ctx context.Context, tx pgx.Tx, dbID uuid.UUID, propID string) error {
	var n int64
	err := tx.QueryRow(ctx, `
		WITH ordered AS (
		    SELECT id, row_number() OVER (ORDER BY created_at, id) AS n FROM docs_pages WHERE parent_id = $1 AND is_row AND NOT is_template
		), done AS (
		    UPDATE docs_pages p SET props = jsonb_set(p.props, ARRAY[$2::text], to_jsonb(o.n)) FROM ordered o WHERE p.id = o.id RETURNING o.n
		)
		SELECT COALESCE(max(n), 0) FROM done`, dbID, propID).Scan(&n)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO docs_counters (database_id, property_id, last) VALUES ($1, $2, $3)
		ON CONFLICT (database_id, property_id) DO UPDATE SET last = GREATEST(docs_counters.last, EXCLUDED.last)`, dbID, propID, n)
	return err
}

// assignUniqueIDs numbers a new row for each unique ID property (race-safe:
// the counter row is updated atomically).
func assignUniqueIDs(ctx context.Context, tx pgx.Tx, dbID, rowID uuid.UUID, schema []Property) error {
	for _, p := range schema {
		if p.Type != "unique_id" {
			continue
		}
		var n int64
		if err := tx.QueryRow(ctx, `INSERT INTO docs_counters (database_id, property_id, last) VALUES ($1, $2, 1)
			ON CONFLICT (database_id, property_id) DO UPDATE SET last = docs_counters.last + 1 RETURNING last`, dbID, p.ID).Scan(&n); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], to_jsonb($3::bigint)) WHERE id = $1`, rowID, p.ID, n); err != nil {
			return err
		}
	}
	return nil
}

// rowCopyKey marks a copy made by insertRow (templates), which numbers and
// links the new row itself.
type rowCopyKey struct{}

// afterRowCopy fixes a copied row (duplicate.go): files values point at the
// copy's own uploads, unique IDs are fresh, and two-way relations point back
// at it from the other side. The row's database is its parent.
func (s *Service) afterRowCopy(ctx context.Context, tx pgx.Tx, newRowID uuid.UUID, fileMap map[string]string) error {
	var dbID uuid.UUID
	var raw, rawProps []byte
	err := tx.QueryRow(ctx, `SELECT d.id, d.schema, r.props FROM docs_pages r JOIN docs_pages d ON d.id = r.parent_id AND d.kind = 'database' WHERE r.id = $1`, newRowID).Scan(&dbID, &raw, &rawProps)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var schema []Property
	_ = json.Unmarshal(raw, &schema)
	props := map[string]any{}
	_ = json.Unmarshal(rawProps, &props)
	changed := false
	for _, p := range schema {
		list, ok := props[p.ID].([]any)
		if p.Type != "files" || !ok {
			continue
		}
		kept := []any{}
		for _, x := range list {
			m, _ := x.(map[string]any)
			if id, _ := m["file_id"].(string); id != "" {
				nid, ok := fileMap[id]
				if !ok {
					continue // an upload that didn't come along
				}
				m["file_id"] = nid
			}
			kept = append(kept, m)
		}
		props[p.ID], changed = kept, true
	}
	if changed {
		b, _ := json.Marshal(props)
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = $2 WHERE id = $1`, newRowID, b); err != nil {
			return err
		}
	}
	if ctx.Value(rowCopyKey{}) != nil {
		return nil
	}
	if err := assignUniqueIDs(ctx, tx, dbID, newRowID, schema); err != nil {
		return err
	}
	for _, p := range schema {
		if p.Type != "relation" || p.Relation == nil || p.Relation.MirrorProperty == "" {
			continue
		}
		ids := relationValues(props[p.ID])
		if len(ids) == 0 {
			continue
		}
		if _, err := syncMirror(ctx, tx, newRowID, p, s.mirrorOf(ctx, tx, p), nil, ids); err != nil {
			return err
		}
	}
	return nil
}

// mirrorOf returns the other side of a two-way relation.
func (s *Service) mirrorOf(ctx context.Context, q db.Querier, p Property) Property {
	if p.Relation == nil || p.Relation.MirrorProperty == "" {
		return Property{}
	}
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT schema FROM docs_pages WHERE id = $1`, p.Relation.DatabaseID).Scan(&raw); err != nil {
		return Property{}
	}
	var schema []Property
	_ = json.Unmarshal(raw, &schema)
	m, _ := findProp(schema, p.Relation.MirrorProperty)
	return m
}

// notifyRelated tells open views of databases whose relations point at db
// that their computed values may have changed.
func (s *Service) notifyRelated(ctx context.Context, dbID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM docs_pages WHERE kind = 'database' AND id <> $1 AND deleted_at IS NULL
		AND schema @> jsonb_build_array(jsonb_build_object('relation', jsonb_build_object('database_id', $1::text)))`, dbID)
	if err != nil {
		return
	}
	ids, _ := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	for _, id := range ids {
		s.publish(ctx, pageTopic(id), EventRows, map[string]any{"database_id": id})
	}
}

// databaseDetail adds what a page's detail needs for databases: a row's
// computed values and related rows' titles for the viewer, and computed
// properties' settings (rollup targets, formula errors) on the schema. A
// public viewer (member false) gets no related rows: the rows list, which
// knows the link, shows those the link opens.
func (s *Service) databaseDetail(ctx context.Context, me uuid.UUID, member bool, out *PageDetail) error {
	v := memberViewer(ctx, me, "")
	if !member {
		v = viewer{token: "-", loc: time.UTC}
	}
	p := out.Page
	if p.Kind == "database" && p.Schema != nil {
		c := s.newComputer(ctx, v)
		p.Schema = c.annotateSchema(p.ID, p.Schema)
	}
	if p.IsRow && out.Database != nil {
		if err := s.withComputed(ctx, v, p, out.Database.Schema, &out.Refs); err != nil {
			return err
		}
		c := s.newComputer(ctx, v)
		out.Database.Schema = c.annotateSchema(out.Database.ID, out.Database.Schema)
	}
	return nil
}

// ---- row values ------------------------------------------------------------------

type rowValuesReq struct {
	Title    *string                    `json:"title"`
	Values   map[string]json.RawMessage `json:"values"`
	AfterID  *uuid.UUID                 `json:"after_id"` // reorder: after this row (nil with first=true: first)
	First    bool                       `json:"first"`
	Position string                     `json:"position"` // or an explicit position
	TZ       string                     `json:"tz"`       // the writer's time zone (computed values in the answer)
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

type relChange struct {
	p          Property
	prev, next []uuid.UUID
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
	agent := access.ViaAgent(ctx)
	set := map[string]any{}
	unset := []string{}
	var rels []relChange
	var changed []string
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
		var v any
		if p.Type == "relation" {
			ids, err := s.cleanRelation(ctx, s.d.Pool, me, agent, p, row.Props[p.ID], raw)
			if err != nil {
				return nil, err
			}
			rels = append(rels, relChange{p: p, prev: relationValues(row.Props[p.ID]), next: relationValues(ids)})
			if len(ids) > 0 {
				v = ids
			}
		} else if v, err = s.cleanRowValue(ctx, row.OrgID, row.ID, p, raw); err != nil {
			return nil, err
		}
		changed = append(changed, key)
		if v == nil {
			unset = append(unset, key)
		} else {
			set[key] = v
		}
	}
	var touchedRows []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if len(set) > 0 || len(unset) > 0 {
			b, _ := json.Marshal(set)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = (props - $3::text[]) || $2, updated_at = now(), updated_by = $4 WHERE id = $1`,
				row.ID, b, unset, me); err != nil {
				return err
			}
		}
		for _, rc := range rels {
			mirror := s.mirrorOf(ctx, tx, rc.p)
			ids, err := syncMirror(ctx, tx, row.ID, rc.p, mirror, rc.prev, rc.next)
			if err != nil {
				return err
			}
			touchedRows = append(touchedRows, ids...)
		}
		for key := range set {
			if p, _ := findProp(dbp.Schema, key); p.Type == "date" {
				if err := setReminder(ctx, tx, row.ID, p, set[key], me); err != nil {
					return err
				}
			}
		}
		for _, key := range unset {
			if _, err := tx.Exec(ctx, `DELETE FROM docs_row_reminders WHERE row_id = $1 AND property_id = $2`, row.ID, key); err != nil {
				return err
			}
		}
		if req.Title != nil {
			t, err := cleanTitle(*req.Title)
			if err != nil {
				return err
			}
			if t != row.Title {
				changed = append(changed, "title")
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
	if len(rels) > 0 {
		targets := map[uuid.UUID]bool{}
		for _, rc := range rels {
			if rc.p.Relation != nil && rc.p.Relation.MirrorProperty != "" && rc.p.Relation.DatabaseID != dbp.ID {
				targets[rc.p.Relation.DatabaseID] = true
			}
		}
		for t := range targets {
			s.publish(ctx, pageTopic(t), EventRows, map[string]any{"database_id": t})
		}
		for _, id := range dedupeIDs(touchedRows) {
			s.publish(ctx, pageTopic(id), EventPage, map[string]any{"row_id": id, "reload": true})
		}
	}
	s.notifyRelated(ctx, dbp.ID)
	if len(changed) > 0 {
		s.rowChanged(ctx, dbp, row, out, changed, me)
	}
	if err := s.withComputed(ctx, memberViewer(ctx, me, req.TZ), out, dbp.Schema, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// setReminder stores (or clears) the reminder of a date value: it notifies
// the person who set it, at 09:00 on the day for dates without a time.
func setReminder(ctx context.Context, tx pgx.Tx, rowID uuid.UUID, p Property, v any, me uuid.UUID) error {
	m, _ := v.(map[string]any)
	remind, _ := m["remind"].(string)
	at, label, ok := reminderAt(m)
	if remind == "" || !ok || at.Before(time.Now()) {
		_, err := tx.Exec(ctx, `DELETE FROM docs_row_reminders WHERE row_id = $1 AND property_id = $2`, rowID, p.ID)
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO docs_row_reminders (row_id, property_id, user_id, fire_at, label) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (row_id, property_id) DO UPDATE SET user_id = EXCLUDED.user_id, fire_at = EXCLUDED.fire_at, label = EXCLUDED.label`,
		rowID, p.ID, me, at, label)
	return err
}

// reminderAt is when a date value's reminder fires, and the date as text.
func reminderAt(m map[string]any) (time.Time, string, bool) {
	start, _ := m["start"].(string)
	remind, _ := m["remind"].(string)
	tz, _ := m["tz"].(string)
	off, ok := reminderOffsets[remind]
	if !ok || start == "" {
		return time.Time{}, "", false
	}
	loc := loadTZ(tz)
	var t time.Time
	var err error
	if len(start) == 10 {
		t, err = time.ParseInLocation("2006-01-02 15:04", start+" 09:00", loc)
	} else {
		t, err = time.ParseInLocation("2006-01-02T15:04", start, loc)
	}
	if err != nil {
		return time.Time{}, "", false
	}
	return t.Add(-off), strings.Replace(start, "T", " ", 1), true
}

// ---- new rows --------------------------------------------------------------------

type createRowReq struct {
	Title      string                     `json:"title"`
	Values     map[string]json.RawMessage `json:"values"`
	AfterID    *uuid.UUID                 `json:"after_id"`
	First      bool                       `json:"first"`
	TemplateID *uuid.UUID                 `json:"template_id"` // start from a row template of this database
	ParentRow  *uuid.UUID                 `json:"parent_row"`  // a sub-item of this row (the view's sub-items relation)
	ViewID     *uuid.UUID                 `json:"view_id"`     // the view adding it (its default template and sub-items)
	TZ         string                     `json:"tz"`
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
	return s.insertRow(ctx, me, dbp, req, false)
}

// insertRow adds a row to dbp (the caller checked edit): values, a
// template's values and content, a parent row, unique IDs, mirrors, and the
// row-added automations. asTemplate makes a row template instead.
func (s *Service) insertRow(ctx context.Context, me uuid.UUID, dbp *Page, req createRowReq, asTemplate bool) (*Page, error) {
	agent := access.ViaAgent(ctx)
	if req.ViewID != nil && !asTemplate {
		var raw []byte
		if err := s.d.Pool.QueryRow(ctx, `SELECT config FROM docs_views WHERE id = $1 AND database_id = $2`, *req.ViewID, dbp.ID).Scan(&raw); err == nil {
			var cfg ViewConfig
			_ = json.Unmarshal(raw, &cfg)
			if req.TemplateID == nil {
				req.TemplateID = cfg.TemplateID
			}
			if req.ParentRow != nil && cfg.SubItems != nil && selfRelation(dbp.Schema, dbp.ID, cfg.SubItems.Property) {
				if req.Values == nil {
					req.Values = map[string]json.RawMessage{}
				}
				req.Values[cfg.SubItems.Property], _ = json.Marshal([]uuid.UUID{*req.ParentRow})
			}
		}
	}
	var tpl *Page
	if req.TemplateID != nil {
		t, err := loadPage(ctx, s.d.Pool, *req.TemplateID, false)
		if err != nil || !t.IsTemplate || t.ParentID == nil || *t.ParentID != dbp.ID || t.DeletedAt != nil {
			return nil, httpx.NotFound("Template not found.")
		}
		tpl = t
	}
	title := req.Title
	if title == "" && tpl != nil {
		title = tpl.Title
	}
	title, err := cleanTitle(title)
	if err != nil {
		return nil, err
	}
	values := map[string]any{}
	var rels []relChange
	if tpl != nil {
		// Template values as they still fit the schema; @me / @today resolve now.
		for key, v := range tpl.Props {
			p, ok := findProp(dbp.Schema, key)
			if !ok || !writable(p) {
				continue
			}
			raw, _ := json.Marshal(resolveDynamic(v, me, req.TZ))
			if p.Type == "relation" {
				ids, err := s.cleanRelation(ctx, s.d.Pool, me, agent, p, nil, raw)
				if err == nil && len(ids) > 0 {
					values[key] = ids
				}
				continue
			}
			if cv, err := s.cleanValue(ctx, dbp.OrgID, p, raw); err == nil && cv != nil {
				values[key] = cv
			}
		}
	}
	for key, raw := range req.Values {
		p, ok := findProp(dbp.Schema, key)
		if !ok || p.Type == "title" {
			continue
		}
		if p.Type == "relation" {
			ids, err := s.cleanRelation(ctx, s.d.Pool, me, agent, p, nil, raw)
			if err != nil {
				return nil, err
			}
			if len(ids) > 0 {
				values[key] = ids
			} else {
				delete(values, key)
			}
			continue
		}
		v, err := s.cleanValue(ctx, dbp.OrgID, p, raw)
		if err != nil {
			return nil, err
		}
		if v != nil {
			values[key] = v
		} else {
			delete(values, key)
		}
	}
	for key, v := range values {
		if p, ok := findProp(dbp.Schema, key); ok && p.Type == "relation" {
			rels = append(rels, relChange{p: p, next: relationValues(v)})
		}
	}
	var rowID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM docs_pages WHERE parent_id = $1 AND is_row AND deleted_at IS NULL`, dbp.ID).Scan(&n); err != nil {
			return err
		}
		if n >= maxRows {
			return httpx.NewError(http.StatusConflict, "database_full", fmt.Sprintf("A database holds up to %d rows.", maxRows))
		}
		var pos string
		if req.First {
			var first string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(min(position), '') FROM docs_pages WHERE parent_id = $1`, dbp.ID).Scan(&first); err != nil {
				return err
			}
			pos = keyBetween("", first)
		} else {
			var err error
			if pos, err = siblingPosition(ctx, tx, dbp.OrgID, &dbp.ID, req.AfterID); err != nil {
				return err
			}
		}
		rowID = uuid.New()
		if tpl != nil {
			// The template's content (blocks, files, subpages) first; values below.
			if _, err := tx.Exec(ctx, `
				INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, icon, position, props, created_by, updated_by, version)
				VALUES ($1, $2, $3, 'page', true, $4, $5, $6, '{}', $7, $7, 1)`, rowID, dbp.OrgID, dbp.ID, title, tpl.Icon, pos, me); err != nil {
				return err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: rowID, OrgID: dbp.OrgID, OwnerID: me, Parent: &access.Ref{Kind: Kind, ID: dbp.ID}}); err != nil {
				return err
			}
			budget := &dupBudget{pages: maxDuplicatePages, blocks: maxDuplicateBlocks}
			if err := s.copyInto(context.WithValue(ctx, rowCopyKey{}, true), tx, me, tpl.ID, rowID, budget); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET is_template = false, repeat = NULL, repeat_next_at = NULL WHERE id = $1`, rowID); err != nil {
				return err
			}
			// Files the template holds were copied to the new row (afterRowCopy re-aimed them).
			var copied []byte
			if err := tx.QueryRow(ctx, `SELECT props FROM docs_pages WHERE id = $1`, rowID).Scan(&copied); err != nil {
				return err
			}
			var cp map[string]any
			_ = json.Unmarshal(copied, &cp)
			for _, p := range dbp.Schema {
				if _, set := values[p.ID]; p.Type == "files" && !set && cp[p.ID] != nil {
					values[p.ID] = cp[p.ID]
				}
			}
		} else {
			if _, err := tx.Exec(ctx, `
				INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, position, props, created_by, updated_by)
				VALUES ($1, $2, $3, 'page', true, $4, $5, '{}', $6, $6)`, rowID, dbp.OrgID, dbp.ID, title, pos, me); err != nil {
				return err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: rowID, OrgID: dbp.OrgID, OwnerID: me, Parent: &access.Ref{Kind: Kind, ID: dbp.ID}}); err != nil {
				return err
			}
		}
		b, _ := json.Marshal(values)
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = $2, title = $3, is_template = $4 WHERE id = $1`, rowID, b, title, asTemplate); err != nil {
			return err
		}
		if !asTemplate {
			if err := assignUniqueIDs(ctx, tx, dbp.ID, rowID, dbp.Schema); err != nil {
				return err
			}
			for _, rc := range rels {
				if _, err := syncMirror(ctx, tx, rowID, rc.p, s.mirrorOf(ctx, tx, rc.p), nil, rc.next); err != nil {
					return err
				}
			}
			for key, v := range values {
				if p, _ := findProp(dbp.Schema, key); p.Type == "date" {
					if err := setReminder(ctx, tx, rowID, p, v, me); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out, err := loadPage(ctx, s.d.Pool, rowID, false)
	if err != nil {
		return nil, err
	}
	out.MyLevel = dbp.MyLevel
	if asTemplate {
		s.publish(ctx, pageTopic(dbp.ID), EventSchema, map[string]any{"database_id": dbp.ID, "templates": true})
		return out, nil
	}
	s.publish(ctx, pageTopic(dbp.ID), EventRows, map[string]any{"database_id": dbp.ID, "row_id": rowID})
	for _, rc := range rels {
		if rc.p.Relation != nil && rc.p.Relation.MirrorProperty != "" && rc.p.Relation.DatabaseID != dbp.ID {
			s.publish(ctx, pageTopic(rc.p.Relation.DatabaseID), EventRows, map[string]any{"database_id": rc.p.Relation.DatabaseID})
		}
	}
	s.notifyRelated(ctx, dbp.ID)
	s.rowAdded(ctx, dbp, out, me)
	if err := s.withComputed(ctx, memberViewer(ctx, me, req.TZ), out, dbp.Schema, nil); err != nil {
		return nil, err
	}
	return out, nil
}

// resolveDynamic turns a template's or button's "@me", "@today" and "@now"
// into values.
func resolveDynamic(v any, me uuid.UUID, tz string) any {
	loc := loadTZ(tz)
	switch x := v.(type) {
	case string:
		switch x {
		case "@me":
			return []string{me.String()}
		case "@today":
			return map[string]any{"start": time.Now().In(loc).Format("2006-01-02")}
		case "@now":
			out := map[string]any{"start": time.Now().In(loc).Format("2006-01-02T15:04")}
			if tz != "" {
				out["tz"] = tz
			}
			return out
		}
	case []any:
		for i, e := range x {
			if s, _ := e.(string); s == "@me" {
				x[i] = me.String()
			}
		}
	case map[string]any:
		if s, _ := x["start"].(string); s == "@today" || s == "@now" {
			d := resolveDynamic(s, me, tz).(map[string]any)
			for k, val := range x {
				if k != "start" {
					d[k] = val
				}
			}
			return d
		}
	}
	return v
}

// ---- new databases ------------------------------------------------------------------

type createDatabaseReq struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Title    string     `json:"title"`
	Icon     *string    `json:"icon"`
	Inline   bool       `json:"inline"`
	Shared   bool       `json:"shared"`
	AfterID  *uuid.UUID `json:"after_id"`
	ViewType string     `json:"view_type"`
}

// DatabaseCreated is a new database with its first view.
type DatabaseCreated struct {
	Page  *Page  `json:"page"`
	Views []View `json:"views"`
}

// createDatabase: POST /docs/orgs/{orgID}/databases (member; edit on the
// parent): a database whose first view is view_type (the editor's /board
// view, /form… commands), with the properties that view needs.
func (s *Service) createDatabase(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID, err := httpx.URLParamUUID(r, "orgID")
	if err != nil {
		return err
	}
	var req createDatabaseReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ViewType == "" {
		req.ViewType = "table"
	}
	if !slices.Contains(viewTypes, req.ViewType) {
		return httpx.BadRequest("Choose a view type: " + strings.Join(viewTypes, ", ") + ".")
	}
	p, err := s.create(ctx, me, orgID, createPageReq{ParentID: req.ParentID, Kind: "database", Title: req.Title, Icon: req.Icon, Shared: req.Shared, AfterID: req.AfterID, Inline: req.Inline})
	if err != nil {
		return err
	}
	if req.ViewType != "table" {
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			cur, err := loadPage(ctx, tx, p.ID, true)
			if err != nil {
				return err
			}
			schema, changed := ensureViewProps(cur.Schema, req.ViewType)
			if req.ViewType == "form" {
				// A form starts with a few questions to answer.
				if !slices.ContainsFunc(schema, func(x Property) bool { return x.Type == "text" }) {
					schema, changed = append(schema, Property{ID: shortID(), Name: "Details", Type: "text"}), true
				}
			}
			if changed {
				raw, _ := json.Marshal(schema)
				if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2 WHERE id = $1`, p.ID, raw); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM docs_views WHERE database_id = $1`, p.ID); err != nil {
				return err
			}
			cfg := ViewConfig{}
			if req.ViewType == "form" {
				f := &FormConfig{}
				for _, x := range schema {
					if formAnswerable(x) {
						f.Questions = append(f.Questions, FormQuestion{Property: x.ID, Required: x.Type == "title"})
					}
				}
				cfg.Form = f
			}
			_, err = s.insertView(ctx, tx, me, cur, schema, viewLabel(req.ViewType), req.ViewType, cfg, nil, nil)
			return err
		})
		if err != nil {
			return err
		}
	}
	out, err := loadPage(ctx, s.d.Pool, p.ID, false)
	if err != nil {
		return err
	}
	out.MyLevel = p.MyLevel
	views, err := loadViews(ctx, s.d.Pool, p.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, DatabaseCreated{Page: out, Views: views})
	return nil
}

// ---- buttons ----------------------------------------------------------------------

type buttonReq struct {
	TZ string `json:"tz"`
}

// ButtonResult says what a button did; open_url steps are for the client.
type ButtonResult struct {
	Row     *Page    `json:"row"`
	Created []string `json:"created"` // ids of rows it added
	Open    []string `json:"open"`    // links to open
}

// pressButton: POST /docs/pages/{id}/buttons/{propID} (edit on the row):
// runs a button property's steps as the caller — each step needs the
// caller's own access (edit on this row, edit on the database a row is
// added to).
func (s *Service) pressButton(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req buttonReq
	_ = httpx.Decode(r, &req)
	row, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if !row.IsRow || row.ParentID == nil {
		return httpx.BadRequest("Only database rows have buttons.")
	}
	dbp, err := loadPage(ctx, s.d.Pool, *row.ParentID, false)
	if err != nil {
		return err
	}
	p, ok := findProp(dbp.Schema, chi.URLParam(r, "propID"))
	if !ok || p.Type != "button" || p.Button == nil {
		return httpx.NotFound("Button not found.")
	}
	out := ButtonResult{Created: []string{}, Open: []string{}}
	set := map[string]json.RawMessage{}
	for _, a := range p.Button.Actions {
		switch a.Type {
		case "set_property":
			var v any
			_ = json.Unmarshal(a.Value, &v)
			raw, _ := json.Marshal(resolveDynamic(v, me, req.TZ))
			set[a.Property] = raw
		case "add_row":
			if a.DatabaseID == nil {
				continue
			}
			created, err := s.newRow(ctx, me, *a.DatabaseID, createRowReq{Values: a.Values, TemplateID: a.TemplateID, TZ: req.TZ})
			if err != nil {
				return err
			}
			out.Created = append(out.Created, created.ID.String())
		case "open_url":
			out.Open = append(out.Open, a.URL)
		}
	}
	if len(set) > 0 {
		if _, err := s.setRow(ctx, me, row, rowValuesReq{Values: set, TZ: req.TZ}); err != nil {
			return err
		}
	}
	if out.Row, err = loadPage(ctx, s.d.Pool, row.ID, false); err != nil {
		return err
	}
	out.Row.MyLevel = row.MyLevel
	if err := s.withComputed(ctx, memberViewer(ctx, me, req.TZ), out.Row, dbp.Schema, nil); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}
