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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Relations store row ids in the row's props, on both sides of a two-way
// relation; the server keeps the mirror side in step in the same
// transaction. Rollups and footer calculations share aggregate.

// rollupFunctions are Notion's rollup aggregations; footer calculations use
// the same names.
var rollupFunctions = []string{"show_original", "show_unique", "count_all", "count_values", "count_unique_values", "count_empty",
	"count_not_empty", "percent_empty", "percent_not_empty", "sum", "average", "median", "min", "max", "range",
	"earliest_date", "latest_date", "date_range", "checked", "unchecked", "percent_checked", "percent_unchecked"}

// rollupType is the result type of an aggregation over values of type t.
func rollupType(fn string, t fType) fType {
	switch fn {
	case "show_original", "show_unique":
		if t.Kind == fList && t.Elem != nil {
			return tList(*t.Elem)
		}
		return tList(t)
	case "earliest_date", "latest_date":
		return tDate
	}
	return tNumber
}

// rollupAllowed: numbers for arithmetic, dates for date aggregations,
// checkboxes for checked counts; counting and showing fit everything.
func rollupAllowed(fn string, t fType) bool {
	base := t
	if t.Kind == fList && t.Elem != nil {
		base = *t.Elem
	}
	switch fn {
	case "sum", "average", "median", "min", "max", "range":
		return base.Kind == fNumber
	case "earliest_date", "latest_date", "date_range":
		return base.Kind == fDate
	case "checked", "unchecked", "percent_checked", "percent_unchecked":
		return base.Kind == fBool
	}
	return slices.Contains(rollupFunctions, fn)
}

// leaves flattens list values (one level per call, recursively).
func leaves(vals []fValue) []fValue {
	var out []fValue
	for _, v := range vals {
		if v.Kind == fList {
			out = append(out, leaves(v.List)...)
		} else if v.Kind != fEmpty {
			out = append(out, v)
		}
	}
	return out
}

func leafKey(v fValue) string {
	switch v.Kind {
	case fNumber:
		return "n" + fFmtNum(v.Num)
	case fText:
		return "t" + v.Str
	case fBool:
		return "b" + strconv.FormatBool(v.Bool)
	case fDate:
		k := "d" + strconv.FormatInt(v.Date.Start.UnixMilli(), 10)
		if v.Date.End != nil {
			k += "-" + strconv.FormatInt(v.Date.End.UnixMilli(), 10)
		}
		return k
	case fPerson, fPage:
		return "i" + v.ID.String()
	}
	return ""
}

func blank(v fValue) bool {
	return fIsBlank(v)
}

func pct(n, of int) fValue {
	if of == 0 {
		return vEmpty
	}
	return vNum(math.Round(float64(n)*10000/float64(of)) / 100)
}

// aggregate applies an aggregation to one value per row (vals) — a rollup
// over related rows or a footer calculation over a view's rows.
func aggregate(fn string, vals []fValue) fValue {
	n := len(vals)
	switch fn {
	case "show_original":
		return vList(leaves(vals))
	case "show_unique":
		seen := map[string]bool{}
		var out []fValue
		for _, v := range leaves(vals) {
			if k := leafKey(v); !seen[k] {
				seen[k] = true
				out = append(out, v)
			}
		}
		return vList(out)
	case "count_all":
		return vNum(float64(n))
	case "count_values":
		return vNum(float64(len(leaves(vals))))
	case "count_unique_values":
		seen := map[string]bool{}
		for _, v := range leaves(vals) {
			seen[leafKey(v)] = true
		}
		return vNum(float64(len(seen)))
	case "count_empty", "count_not_empty", "percent_empty", "percent_not_empty":
		empty := 0
		for _, v := range vals {
			if blank(v) {
				empty++
			}
		}
		switch fn {
		case "count_empty":
			return vNum(float64(empty))
		case "count_not_empty":
			return vNum(float64(n - empty))
		case "percent_empty":
			return pct(empty, n)
		}
		return pct(n-empty, n)
	case "sum", "average", "median", "min", "max", "range":
		var nums []float64
		for _, v := range leaves(vals) {
			if v.Kind == fNumber && !math.IsNaN(v.Num) && !math.IsInf(v.Num, 0) {
				nums = append(nums, v.Num)
			}
		}
		if fn == "sum" {
			t := 0.0
			for _, x := range nums {
				t += x
			}
			return vNum(t)
		}
		if len(nums) == 0 {
			return vEmpty
		}
		switch fn {
		case "average":
			t := 0.0
			for _, x := range nums {
				t += x
			}
			return vNum(t / float64(len(nums)))
		case "median":
			return fMedian(nums)
		}
		lo, hi := nums[0], nums[0]
		for _, x := range nums {
			lo, hi = math.Min(lo, x), math.Max(hi, x)
		}
		switch fn {
		case "min":
			return vNum(lo)
		case "max":
			return vNum(hi)
		}
		return vNum(hi - lo)
	case "earliest_date", "latest_date", "date_range":
		var first, last *fValue
		for _, v := range leaves(vals) {
			if v.Kind != fDate {
				continue
			}
			v := v
			if first == nil || v.Date.Start.Before(first.Date.Start) {
				first = &v
			}
			end := v.Date.Start
			if v.Date.End != nil {
				end = *v.Date.End
			}
			if last == nil || end.After(lastEnd(*last)) {
				last = &v
			}
		}
		if first == nil {
			return vEmpty
		}
		switch fn {
		case "earliest_date":
			return fValue{Kind: fDate, Date: fDateValue{Start: first.Date.Start, HasTime: first.Date.HasTime}}
		case "latest_date":
			return fValue{Kind: fDate, Date: fDateValue{Start: lastEnd(*last), HasTime: last.Date.HasTime}}
		}
		return vNum(math.Floor(lastEnd(*last).Sub(first.Date.Start).Hours() / 24))
	case "checked", "unchecked", "percent_checked", "percent_unchecked":
		on := 0
		for _, v := range vals {
			if v.Kind == fBool && v.Bool {
				on++
			}
		}
		switch fn {
		case "checked":
			return vNum(float64(on))
		case "unchecked":
			return vNum(float64(n - on))
		case "percent_checked":
			return pct(on, n)
		}
		return pct(n-on, n)
	}
	return vEmpty
}

func lastEnd(v fValue) time.Time {
	if v.Date.End != nil {
		return *v.Date.End
	}
	return v.Date.Start
}

// ---- writing relation values ---------------------------------------------------------

// cleanRelation validates a relation value written by me (agent-capped when
// agent): new ids must be rows of the related database the caller can open;
// ids already there that the caller can't open are kept (they can't see
// them to remove them); a limit of one keeps the newest choice.
func (s *Service) cleanRelation(ctx context.Context, q db.Querier, me uuid.UUID, agent bool, p Property, old any, raw json.RawMessage) ([]string, error) {
	bad := func(msg string) error { return httpx.BadRequest(p.Name + ": " + msg) }
	if p.Relation == nil {
		return nil, bad("isn't linked to a database.")
	}
	var ids []uuid.UUID
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &ids); err != nil {
			var one uuid.UUID
			if json.Unmarshal(raw, &one) != nil {
				return nil, bad("choose rows by id.")
			}
			ids = []uuid.UUID{one}
		}
	}
	ids = dedupeIDs(ids)
	if len(ids) > maxRelated {
		return nil, bad(fmt.Sprintf("links up to %d rows.", maxRelated))
	}
	prev := relationValues(old)
	isPrev := map[uuid.UUID]bool{}
	for _, id := range prev {
		isPrev[id] = true
	}
	// Who can open what, among the old and new ids.
	all := dedupeIDs(append(append([]uuid.UUID{}, prev...), ids...))
	open := map[uuid.UUID]bool{}
	if len(all) > 0 {
		rows, err := q.Query(ctx, `SELECT p.id FROM docs_pages p WHERE p.id = ANY ($1) AND p.parent_id = $2 AND p.is_row AND NOT p.is_template
			AND p.deleted_at IS NULL AND access_level($3, 'docs.page', p.id, $4) >= 1`, all, p.Relation.DatabaseID, me, agent)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			open[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := []string{}
	for _, id := range ids {
		if !open[id] && !isPrev[id] {
			return nil, bad("choose rows of its database that you can open.")
		}
		out = append(out, id.String())
	}
	for _, id := range prev {
		if !open[id] && !slices.Contains(out, id.String()) {
			out = append(out, id.String()) // kept: the caller can't see it
		}
	}
	if p.Relation.Limit == 1 && len(out) > 1 {
		if len(ids) > 0 {
			out = []string{ids[len(ids)-1].String()}
		} else {
			out = out[:1]
		}
	}
	return out, nil
}

// syncMirror updates the mirror side of a two-way relation after a row's
// relation p went from prev to next. mirrorLimit is the mirror property's
// limit: with one, a newly linked row drops its previous link, and that
// row's forward value drops it too. It returns the rows it changed.
func syncMirror(ctx context.Context, tx pgx.Tx, rowID uuid.UUID, p Property, mirror Property, prev, next []uuid.UUID) ([]uuid.UUID, error) {
	if p.Relation == nil || p.Relation.MirrorProperty == "" || mirror.ID == "" {
		return nil, nil
	}
	m := mirror.ID
	inNext := map[uuid.UUID]bool{}
	for _, id := range next {
		inNext[id] = true
	}
	inPrev := map[uuid.UUID]bool{}
	for _, id := range prev {
		inPrev[id] = true
	}
	var added, removed []uuid.UUID
	for _, id := range next {
		if !inPrev[id] {
			added = append(added, id)
		}
	}
	for _, id := range prev {
		if !inNext[id] {
			removed = append(removed, id)
		}
	}
	me := rowID.String()
	if len(removed) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], COALESCE((
			SELECT jsonb_agg(x) FROM jsonb_array_elements(props->$2::text) x WHERE x <> to_jsonb($3::text)), '[]'::jsonb))
			WHERE id = ANY ($1) AND jsonb_typeof(props->$2::text) = 'array'`, removed, m, me); err != nil {
			return nil, err
		}
	}
	if len(added) > 0 {
		if mirror.Relation != nil && mirror.Relation.Limit == 1 {
			// Each newly linked row points back only at this row; its old
			// partner loses it.
			rows, err := tx.Query(ctx, `SELECT id, COALESCE(props->$2::text, '[]'::jsonb) FROM docs_pages WHERE id = ANY ($1)`, added, m)
			if err != nil {
				return nil, err
			}
			olds := map[uuid.UUID][]uuid.UUID{}
			for rows.Next() {
				var id uuid.UUID
				var raw []byte
				if err := rows.Scan(&id, &raw); err != nil {
					rows.Close()
					return nil, err
				}
				var list []any
				_ = json.Unmarshal(raw, &list)
				olds[id] = relationValues(list)
			}
			rows.Close()
			for id, list := range olds {
				for _, partner := range list {
					if partner == rowID {
						continue
					}
					if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], COALESCE((
						SELECT jsonb_agg(x) FROM jsonb_array_elements(props->$2::text) x WHERE x <> to_jsonb($3::text)), '[]'::jsonb))
						WHERE id = $1 AND jsonb_typeof(props->$2::text) = 'array'`, partner, p.ID, id.String()); err != nil {
						return nil, err
					}
					removed = append(removed, partner)
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], jsonb_build_array($3::text)) WHERE id = ANY ($1)`, added, m, me); err != nil {
				return nil, err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, ARRAY[$2::text], COALESCE(props->$2::text, '[]'::jsonb) || to_jsonb($3::text))
			WHERE id = ANY ($1) AND NOT (COALESCE(props->$2::text, '[]'::jsonb) ? $3::text)`, added, m, me); err != nil {
			return nil, err
		}
	}
	return append(added, removed...), nil
}

// ---- relations in the schema ------------------------------------------------------------

// relationChange is what applyRelations did to other databases.
type relationChange struct {
	touched []uuid.UUID // databases whose schema or values changed
}

// applyRelations brings relation settings of database dbp from old to next:
// checks each relation's database (same workspace, the caller can open it),
// keeps server-managed mirror ids, creates mirror properties for new two-way
// relations (edit on the other database), removes the other side of
// relations that were deleted or made one-way (or makes it one-way when the
// caller can't edit that database) and fills mirror values for relations
// that just became two-way. next is changed in place (mirrors on the same
// database are appended to it).
func (s *Service) applyRelations(ctx context.Context, tx pgx.Tx, me uuid.UUID, agent bool, dbp *Page, old []Property, next *[]Property) (*relationChange, error) {
	ch := &relationChange{}
	oldByID := map[string]Property{}
	for _, p := range old {
		oldByID[p.ID] = p
	}
	nextByID := map[string]Property{}
	for _, p := range *next {
		nextByID[p.ID] = p
	}
	// The other side of each old two-way relation that goes away.
	for _, po := range old {
		if po.Type != "relation" || po.Relation == nil || po.Relation.MirrorProperty == "" {
			continue
		}
		pn, ok := nextByID[po.ID]
		gone := !ok || pn.Type != "relation" || pn.Relation == nil || pn.Relation.DatabaseID != po.Relation.DatabaseID || !pn.Relation.TwoWay
		if !gone {
			continue
		}
		if po.Relation.DatabaseID == dbp.ID {
			// Self relation: drop the mirror from next unless it's being kept as its own property.
			if _, kept := nextByID[po.Relation.MirrorProperty]; kept {
				for i, x := range *next {
					if x.ID == po.Relation.MirrorProperty && x.Relation != nil {
						x.Relation.MirrorProperty, x.Relation.TwoWay = "", false
						(*next)[i] = x
					}
				}
			}
			continue
		}
		if err := s.dropOtherSide(ctx, tx, me, agent, po.Relation.DatabaseID, po.Relation.MirrorProperty, ch); err != nil {
			return nil, err
		}
	}
	for i := range *next {
		pn := (*next)[i]
		if pn.Type != "relation" || pn.Relation == nil {
			continue
		}
		target := pn.Relation.DatabaseID
		if target != dbp.ID {
			tp, err := loadPage(ctx, tx, target, false)
			if err != nil || tp.Kind != "database" || tp.OrgID != dbp.OrgID || tp.DeletedAt != nil {
				return nil, httpx.BadRequest(pn.Name + ": choose a database in this workspace.")
			}
			if _, err := access.Require(ctx, tx, me, Kind, target, access.View); err != nil {
				return nil, httpx.BadRequest(pn.Name + ": choose a database you can open.")
			}
		}
		po, had := oldByID[pn.ID]
		same := had && po.Type == "relation" && po.Relation != nil && po.Relation.DatabaseID == target
		mirror := ""
		if same {
			mirror = po.Relation.MirrorProperty
		}
		if mirror != "" && target == dbp.ID {
			if _, ok := nextByID[mirror]; !ok {
				mirror = "" // its self mirror was deleted
			}
		}
		pn.Relation.MirrorProperty = mirror
		if same && !po.Relation.TwoWay && mirror != "" {
			pn.Relation.TwoWay = true // a mirror created on the other side
		}
		if pn.Relation.TwoWay && mirror == "" {
			id, err := s.createMirror(ctx, tx, me, agent, dbp, pn, next, ch)
			if err != nil {
				return nil, err
			}
			pn.Relation.MirrorProperty = id
			if same {
				if err := fillMirror(ctx, tx, dbp.ID, pn, target); err != nil {
					return nil, err
				}
			}
		}
		if !pn.Relation.TwoWay {
			pn.Relation.MirrorProperty = ""
		}
		pn.Relation.MirrorName = ""
		(*next)[i] = pn
	}
	return ch, nil
}

// createMirror adds the other side of a two-way relation to the related
// database (or to next, for a self relation) and returns its id.
func (s *Service) createMirror(ctx context.Context, tx pgx.Tx, me uuid.UUID, agent bool, dbp *Page, pn Property, next *[]Property, ch *relationChange) (string, error) {
	target := pn.Relation.DatabaseID
	name := pn.Relation.MirrorName
	if name == "" {
		name = "Related to " + displayTitle(dbp.Title)
		if target == dbp.ID {
			name = pn.Name + " (back)"
		}
	}
	mirror := Property{ID: shortID(), Type: "relation", Relation: &RelationConfig{DatabaseID: dbp.ID, TwoWay: true, MirrorProperty: pn.ID}}
	if target == dbp.ID {
		mirror.Name = uniquePropName(*next, name)
		*next = append(*next, mirror)
		return mirror.ID, nil
	}
	tp, err := requirePage(ctx, tx, me, target, access.Edit)
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusForbidden {
			return "", httpx.Forbidden(pn.Name + ": a two-way relation adds a property to " + displayTitle(tp0Title(ctx, tx, target)) + ", which you can't edit. Make it one-way.")
		}
		return "", err
	}
	if err := editable(tp, true); err != nil {
		return "", err
	}
	cur, err := loadPage(ctx, tx, target, true)
	if err != nil {
		return "", err
	}
	if len(cur.Schema) >= maxProperties {
		return "", httpx.Conflict(displayTitle(cur.Title) + " already has " + strconv.Itoa(maxProperties) + " properties.")
	}
	mirror.Name = uniquePropName(cur.Schema, name)
	schema := append(cur.Schema, mirror)
	raw, _ := json.Marshal(schema)
	if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, target, raw); err != nil {
		return "", err
	}
	ch.touched = append(ch.touched, target)
	return mirror.ID, nil
}

func tp0Title(ctx context.Context, q db.Querier, id uuid.UUID) string {
	t, _ := pageTitle(ctx, q, id)
	return t
}

// dropOtherSide removes the mirror property propID from database target
// (values and views too) when the caller can edit it; otherwise it becomes
// a one-way relation that keeps its values.
func (s *Service) dropOtherSide(ctx context.Context, tx pgx.Tx, me uuid.UUID, agent bool, target uuid.UUID, propID string, ch *relationChange) error {
	cur, err := loadPage(ctx, tx, target, true)
	if err != nil {
		return nil // already gone
	}
	canEdit := false
	if lvl, err := access.Require(ctx, tx, me, Kind, target, access.Edit); err == nil && lvl >= access.Edit && cur.DeletedAt == nil {
		canEdit = true
	}
	var schema []Property
	for _, p := range cur.Schema {
		if p.ID == propID {
			if canEdit {
				continue
			}
			if p.Relation != nil {
				r := *p.Relation
				r.MirrorProperty, r.TwoWay = "", false
				p.Relation = &r
			}
		}
		schema = append(schema, p)
	}
	raw, _ := json.Marshal(schema)
	if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, target, raw); err != nil {
		return err
	}
	if canEdit {
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = props - $2::text WHERE parent_id = $1 AND is_row AND props ? $2::text`, target, propID); err != nil {
			return err
		}
		if err := s.cleanViews(ctx, tx, target, schema); err != nil {
			return err
		}
	}
	ch.touched = append(ch.touched, target)
	return nil
}

// fillMirror writes the mirror values of a relation that just became
// two-way: each related row lists the rows of db that point at it.
func fillMirror(ctx context.Context, tx pgx.Tx, dbID uuid.UUID, p Property, target uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE docs_pages t SET props = jsonb_set(t.props, ARRAY[$3::text], COALESCE((
		    SELECT jsonb_agg(to_jsonb(r.id::text) ORDER BY r.position, r.id) FROM docs_pages r
		    WHERE r.parent_id = $1 AND r.is_row AND jsonb_typeof(r.props->$2::text) = 'array' AND r.props->$2::text ? t.id::text), '[]'::jsonb))
		WHERE t.parent_id = $4 AND t.is_row`, dbID, p.ID, p.Relation.MirrorProperty, target)
	return err
}

func uniquePropName(schema []Property, name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(schema, func(p Property) bool { return strings.EqualFold(p.Name, n) })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !taken(n) {
			return n
		}
	}
}

// ---- computed properties in the schema ----------------------------------------------

// checkComputed validates rollups and formulas of next (database dbID)
// against their relations and each other, and stores their result types.
func (s *Service) checkComputed(ctx context.Context, v viewer, dbID uuid.UUID, next []Property) error {
	c := s.newComputer(ctx, v)
	c.setSchema(dbID, next)
	// Cycles among the database's own formulas and self rollups.
	if err := computedCycles(next, dbID); err != nil {
		return err
	}
	setRollups := func(final bool) error {
		for i, p := range next {
			if p.Type != "rollup" {
				continue
			}
			rc := *p.Rollup
			rel, ok := findPropByName(next, rc.Relation)
			if !ok || rel.Type != "relation" || rel.Relation == nil {
				return httpx.BadRequest(p.Name + ": choose one of this database's relations.")
			}
			rc.Relation = rel.ID
			ts, ok := c.Schema(rel.Relation.DatabaseID)
			if !ok {
				return httpx.BadRequest(p.Name + ": its relation's database is gone.")
			}
			tp, ok := findPropByName(ts, rc.Property)
			if !ok || (rel.Relation.DatabaseID == dbID && tp.ID == p.ID) {
				return httpx.BadRequest(p.Name + ": choose a property of the related database.")
			}
			rc.Property = tp.ID
			t, ok := propFType(tp)
			if !ok {
				return httpx.BadRequest(p.Name + ": " + tp.Name + " can't be rolled up.")
			}
			if final && !rollupAllowed(rc.Function, t) {
				return httpx.BadRequest(p.Name + ": " + rollupLabel(rc.Function) + " doesn't work on " + tp.Name + ".")
			}
			rc.Type = rollupType(rc.Function, t).String()
			next[i].Rollup = &rc
		}
		c.setSchema(dbID, next)
		return nil
	}
	if err := setRollups(false); err != nil {
		return err
	}
	// Formulas may use each other: compile until their types settle, then
	// once more to report the first error.
	formulas := 0
	for _, p := range next {
		if p.Type == "formula" {
			formulas++
		}
	}
	compileAll := func(report bool) (bool, error) {
		changed := false
		for i, p := range next {
			if p.Type != "formula" {
				continue
			}
			t := tEmpty.String()
			if p.Formula.Expression != "" {
				f, err := compileFormula(p.Formula.Expression, dbID, p.ID, c)
				if err != nil {
					if !report {
						continue
					}
					he := httpx.NewError(http.StatusBadRequest, "formula_invalid", p.Name+": "+err.Error())
					pos := 0
					if e, ok := err.(*FormulaError); ok {
						pos = e.Pos
					}
					he.Details = map[string]any{"property": p.ID, "pos": pos}
					return false, he
				}
				t = f.Type.String()
			}
			if p.Formula.Type != t {
				fc := *p.Formula
				fc.Type = t
				next[i].Formula = &fc
				changed = true
				c.setSchema(dbID, next)
				c.formulas = map[valKey]*Formula{}
			}
		}
		return changed, nil
	}
	for pass := 0; pass <= formulas; pass++ {
		if changed, _ := compileAll(false); !changed {
			break
		}
	}
	if _, err := compileAll(true); err != nil {
		return err
	}
	return setRollups(true)
}

// computedCycles refuses formulas that use each other in a loop.
func computedCycles(schema []Property, dbID uuid.UUID) error {
	deps := map[string][]string{}
	for _, p := range schema {
		switch p.Type {
		case "formula":
			if p.Formula == nil || p.Formula.Expression == "" {
				continue
			}
			// Read references cheaply: compile without types would need the
			// schema; prop("…") names are enough here.
			for _, name := range formulaRefNames(p.Formula.Expression) {
				if q, ok := findPropByName(schema, name); ok {
					deps[p.ID] = append(deps[p.ID], q.ID)
				}
			}
		case "rollup":
			if p.Rollup == nil {
				continue
			}
			if rel, ok := findProp(schema, p.Rollup.Relation); ok && rel.Relation != nil && rel.Relation.DatabaseID == dbID {
				if q, ok := findPropByName(schema, p.Rollup.Property); ok {
					deps[p.ID] = append(deps[p.ID], q.ID)
				}
			}
		}
	}
	state := map[string]int{} // 1 visiting, 2 done
	var path []string
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			i := slices.Index(path, id)
			names := []string{}
			for _, x := range path[i:] {
				if p, ok := findProp(schema, x); ok {
					names = append(names, "“"+p.Name+"”")
				}
			}
			return httpx.BadRequest(strings.Join(names, " and ") + " use each other. Break the loop.")
		case 2:
			return nil
		}
		state[id] = 1
		path = append(path, id)
		for _, d := range deps[id] {
			if err := visit(d); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[id] = 2
		return nil
	}
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := visit(k); err != nil {
			return err
		}
	}
	return nil
}

// formulaRefNames lists the names a formula's own prop("…") calls use.
func formulaRefNames(expr string) []string {
	var out []string
	toks, err := lexFormula(expr)
	if err != nil {
		return nil
	}
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].kind == tkIdent && toks[i].text == "prop" && toks[i+1].kind == tkOp && toks[i+1].text == "(" && toks[i+2].kind == tkStr && toks[i+3].kind == tkOp && toks[i+3].text == ")" {
			if i > 0 && toks[i-1].kind == tkOp && toks[i-1].text == "." {
				continue // page.prop(): another database
			}
			out = append(out, toks[i+2].text)
		}
	}
	return out
}

var rollupLabels = map[string]string{
	"show_original": "Show original", "show_unique": "Show unique values", "count_all": "Count all", "count_values": "Count values",
	"count_unique_values": "Count unique values", "count_empty": "Count empty", "count_not_empty": "Count not empty",
	"percent_empty": "Percent empty", "percent_not_empty": "Percent not empty", "sum": "Sum", "average": "Average", "median": "Median",
	"min": "Min", "max": "Max", "range": "Range", "earliest_date": "Earliest date", "latest_date": "Latest date", "date_range": "Date range",
	"checked": "Checked", "unchecked": "Unchecked", "percent_checked": "Percent checked", "percent_unchecked": "Percent unchecked",
}

func rollupLabel(fn string) string {
	if l, ok := rollupLabels[fn]; ok {
		return l
	}
	return fn
}
