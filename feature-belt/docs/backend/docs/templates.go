package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Templates: the built-in gallery (templates_builtin.go) and workspace
// templates. A workspace template is a page tree marked is_template, shared
// through access like any page (the Share dialog decides who can use it);
// using one copies the parts of the tree the person can open.

// Template is a gallery entry.
type Template struct {
	ID        string     `json:"id"`     // builtin:<key>, or the template page's id
	Source    string     `json:"source"` // builtin | workspace
	Name      string     `json:"name"`
	Icon      *string    `json:"icon"`
	Category  string     `json:"category"`
	Info      string     `json:"info,omitempty"`
	Kind      string     `json:"kind"` // page | database
	Cover     *Cover     `json:"cover,omitempty"`
	CreatedBy *RefUser   `json:"created_by,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	MyLevel   string     `json:"my_level,omitempty"` // workspace: the caller's level (edit to change it)

	CustomIcon *CustomIcon `json:"custom_icon,omitempty"`
}

func builtinByKey(key string) (*builtinTemplate, bool) {
	for i := range builtinTemplates {
		if builtinTemplates[i].Key == key {
			return &builtinTemplates[i], true
		}
	}
	return nil, false
}

func (t *builtinTemplate) entry() Template {
	icon := t.Icon
	out := Template{ID: "builtin:" + t.Key, Source: "builtin", Name: t.Name, Icon: &icon, Category: t.Category, Info: t.Info, Kind: "page"}
	if t.Database != nil {
		out.Kind = "database"
	}
	if t.Cover != "" {
		out.Cover = &Cover{Gallery: t.Cover, Position: 50}
	}
	return out
}

// Templates is GET /docs/orgs/{orgID}/templates.
type Templates struct {
	Builtin   []Template `json:"builtin"`
	Workspace []Template `json:"workspace"`
}

// listTemplates: GET /docs/orgs/{orgID}/templates (member): the built-in
// gallery and the workspace templates the caller can open.
func (s *Service) listTemplates(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	out, err := s.templates(ctx, me, m.OrgID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) templates(ctx context.Context, me, orgID uuid.UUID) (*Templates, error) {
	out := &Templates{Builtin: make([]Template, 0, len(builtinTemplates)), Workspace: []Template{}}
	for i := range builtinTemplates {
		out.Builtin = append(out.Builtin, builtinTemplates[i].entry())
	}
	rows, err := s.d.Pool.Query(ctx, `
		WITH v AS MATERIALIZED (SELECT id, level FROM access_visible($1, 'docs.page', $3))
		SELECT p.id, p.title, p.icon, p.custom_icon, p.kind, p.cover, p.updated_at, v.level, u.id, u.display_name
		FROM docs_pages p JOIN v ON v.id = p.id
		LEFT JOIN users u ON u.id = p.created_by AND u.status <> 'deleted'
		WHERE p.org_id = $2 AND p.is_template AND p.parent_id IS NULL AND p.deleted_at IS NULL
		ORDER BY lower(p.title), p.id LIMIT 200`, me, orgID, access.ViaAgent(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t Template
		var id uuid.UUID
		var cover, ci []byte
		var updated time.Time
		var lvl int
		var uid *uuid.UUID
		var uname *string
		if err := rows.Scan(&id, &t.Name, &t.Icon, &ci, &t.Kind, &cover, &updated, &lvl, &uid, &uname); err != nil {
			return nil, err
		}
		t.ID, t.Source, t.Name, t.UpdatedAt, t.Category = id.String(), "workspace", displayTitle(t.Name), &updated, "Workspace"
		t.CustomIcon = decodeCustomIcon(ci)
		t.MyLevel = access.Level(lvl).String()
		if len(cover) > 0 {
			_ = json.Unmarshal(cover, &t.Cover)
		}
		if uid != nil && uname != nil {
			t.CreatedBy = &RefUser{ID: *uid, Name: *uname}
		}
		out.Workspace = append(out.Workspace, t)
	}
	return out, rows.Err()
}

// TemplatePreview is GET /docs/orgs/{orgID}/templates/{templateID}/preview.
type TemplatePreview struct {
	Template Template `json:"template"`
	Blocks   []Block  `json:"blocks"`
	Refs     Refs     `json:"refs"`
	Views    []string `json:"views"`    // a database template's view names
	Subpages []string `json:"subpages"` // titles of the pages it adds inside
}

// previewTemplate: GET /docs/orgs/{orgID}/templates/{templateID}/preview
// (member; a workspace template needs view): blocks to show before using it.
// Inline databases preview as a table of their columns and example rows.
func (s *Service) previewTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	id := templateParam(r)
	out := TemplatePreview{Blocks: []Block{}, Views: []string{}, Subpages: []string{}, Refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}}
	if key, ok := strings.CutPrefix(id, "builtin:"); ok {
		t, ok := builtinByKey(key)
		if !ok {
			return httpx.NotFound("Template not found.")
		}
		out.Template = t.entry()
		out.Blocks = previewBlocks(t)
		if t.Database != nil {
			for _, v := range t.Database.Views {
				out.Views = append(out.Views, v.Name)
			}
		}
		for _, sp := range t.Subpages {
			out.Subpages = append(out.Subpages, sp.Icon+" "+sp.Name)
		}
		httpx.OK(w, out)
		return nil
	}
	pid, err := uuid.Parse(id)
	if err != nil {
		return httpx.NotFound("Template not found.")
	}
	p, err := requirePage(ctx, s.d.Pool, me, pid, access.View)
	if err != nil {
		return err
	}
	if !p.IsTemplate || p.OrgID != m.OrgID {
		return httpx.NotFound("Template not found.")
	}
	detail, err := s.pageDetail(ctx, me, p, true)
	if err != nil {
		return err
	}
	out.Template = Template{ID: p.ID.String(), Source: "workspace", Name: displayTitle(p.Title), Icon: p.Icon, CustomIcon: p.CustomIcon, Kind: p.Kind, Cover: p.Cover, Category: "Workspace"}
	out.Blocks, out.Refs = detail.Blocks, detail.Refs
	for _, v := range detail.Views {
		out.Views = append(out.Views, v.Name)
	}
	kids, err := s.childPages(ctx, s.d.Pool, me, p.ID)
	if err != nil {
		return err
	}
	for _, k := range kids {
		out.Subpages = append(out.Subpages, displayTitle(k.Title))
	}
	httpx.OK(w, out)
	return nil
}

// previewBlocks renders a built-in template's Markdown as blocks (fresh ids),
// with databases as tables and subpage links as text.
func previewBlocks(t *builtinTemplate) []Block {
	var list []*mdBlock
	if t.Database != nil {
		list = []*mdBlock{databasePreviewTable(*t.Database)}
	} else {
		list = expandPlaceholders(templateBlocks(t.Markdown), func(kind, key string) *mdBlock {
			if kind == "database" {
				for _, d := range t.Databases {
					if d.Key == key {
						return databasePreviewTable(d)
					}
				}
			}
			for _, sp := range t.Subpages {
				if sp.Key == key {
					return textBlock("paragraph", sp.Icon+" **"+sp.Name+"**")
				}
			}
			return nil
		})
	}
	var out []Block
	var walk func(list []*mdBlock, parent *uuid.UUID)
	walk = func(list []*mdBlock, parent *uuid.UUID) {
		keys := keysAfter("", "", len(list))
		for i, b := range list {
			id := uuid.New()
			out = append(out, Block{ID: id, ParentID: parent, Type: b.typ, Props: b.props, Position: keys[i]})
			if len(b.children) > 0 {
				pid := id
				walk(b.children, &pid)
			}
		}
	}
	walk(list, nil)
	if out == nil {
		out = []Block{}
	}
	return out
}

// databasePreviewTable is a database's columns and example rows as a table.
func databasePreviewTable(d tplDatabase) *mdBlock {
	head := []string{"Name"}
	for _, p := range d.Props {
		head = append(head, p.Name)
	}
	rows := [][][]Span{cellsOf(head)}
	for _, r := range d.Rows {
		cells := []string{fmt.Sprint(r["title"])}
		for _, p := range d.Props {
			cells = append(cells, previewValue(p, r[p.Name]))
		}
		rows = append(rows, cellsOf(cells))
	}
	return &mdBlock{typ: "table", props: map[string]any{"rows": rows, "header_row": true}}
}

func cellsOf(texts []string) [][]Span {
	out := make([][]Span, len(texts))
	for i, t := range texts {
		out[i] = textSpans(t)
	}
	return out
}

func previewValue(p tplProp, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if p.Type == "date" {
			if d, ok := templateDay(x); ok {
				return d
			}
		}
		if x == "@me" {
			return "You"
		}
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, fmt.Sprint(e))
		}
		return strings.Join(parts, ", ")
	case bool:
		if x {
			return "Yes"
		}
		return "No"
	case float64:
		if p.Format == "percent" {
			return fmt.Sprintf("%.0f%%", x)
		}
		return fmt.Sprint(x)
	}
	return fmt.Sprint(v)
}

// templateDay turns "+Nd" into a date N days from today (YYYY-MM-DD).
func templateDay(s string) (string, bool) {
	var n int
	if _, err := fmt.Sscanf(s, "+%dd", &n); err != nil || n < 0 || n > 3650 {
		return "", false
	}
	return time.Now().AddDate(0, 0, n).Format("2006-01-02"), true
}

// templateBlocks parses a template's Markdown and turns "@today" into a date
// mention.
func templateBlocks(md string) []*mdBlock {
	list := markdownToBlocks(md, 2000)
	today := time.Now().Format("2006-01-02")
	var walk func([]*mdBlock)
	walk = func(list []*mdBlock) {
		for _, b := range list {
			if spans, ok := b.props["text"].([]Span); ok {
				b.props["text"] = expandToday(spans, today)
			}
			walk(b.children)
		}
	}
	walk(list)
	return list
}

func expandToday(spans []Span, today string) []Span {
	var out []Span
	for _, s := range spans {
		if s.Mention != nil || !strings.Contains(s.Text, "@today") {
			out = append(out, s)
			continue
		}
		parts := strings.Split(s.Text, "@today")
		for i, part := range parts {
			if part != "" {
				c := s
				c.Text = part
				out = append(out, c)
			}
			if i < len(parts)-1 {
				out = append(out, Span{Text: today, Mention: &Mention{Type: "date", Date: today}})
			}
		}
	}
	return out
}

// expandPlaceholders replaces top-level paragraphs that are exactly
// {{database:key}} or {{page:key}} with what fn returns (nil drops them).
func expandPlaceholders(list []*mdBlock, fn func(kind, key string) *mdBlock) []*mdBlock {
	out := list[:0:0]
	for _, b := range list {
		if b.typ == "paragraph" {
			if spans, ok := b.props["text"].([]Span); ok {
				t := strings.TrimSpace(plainOf(spans))
				if inner, ok := strings.CutPrefix(t, "{{"); ok {
					if inner, ok = strings.CutSuffix(inner, "}}"); ok {
						if kind, key, ok := strings.Cut(inner, ":"); ok && (kind == "database" || kind == "page") {
							if nb := fn(kind, key); nb != nil {
								out = append(out, nb)
							}
							continue
						}
					}
				}
			}
		}
		out = append(out, b)
	}
	return out
}

// templateParam is the {templateID} path segment, unescaped ("builtin%3Aokrs"
// from clients that encode the colon).
func templateParam(r *http.Request) string {
	raw := chi.URLParam(r, "templateID")
	if v, err := url.PathUnescape(raw); err == nil {
		return v
	}
	return raw
}

// ---- using a template ------------------------------------------------------------

type useTemplateReq struct {
	ParentID *uuid.UUID `json:"parent_id"`
	Shared   bool       `json:"shared"`
	TeamID   *uuid.UUID `json:"team_id"`
	Title    string     `json:"title"` // optional: overrides the template's name
}

// useTemplate: POST /docs/orgs/{orgID}/templates/{templateID}/use (member,
// 120/h) {parent_id?, shared?, team_id?, title?}: a new page from a
// template, placed like a new page.
func (s *Service) useTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req useTemplateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := s.createFromTemplate(ctx, me, m.OrgID, templateParam(r), req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

// createFromTemplate is used by the route and the agent tool.
func (s *Service) createFromTemplate(ctx context.Context, me, orgID uuid.UUID, templateID string, req useTemplateReq) (*Page, error) {
	if key, ok := strings.CutPrefix(templateID, "builtin:"); ok {
		t, ok := builtinByKey(key)
		if !ok {
			return nil, httpx.NotFound("Template not found.")
		}
		p, err := s.buildBuiltin(ctx, me, orgID, t, req, 0)
		if err != nil {
			return nil, err
		}
		s.treeChanged(ctx, orgID)
		return loadPageWithLevel(ctx, s, me, p.ID)
	}
	tid, err := uuid.Parse(templateID)
	if err != nil {
		return nil, httpx.NotFound("Template not found.")
	}
	src, err := requirePage(ctx, s.d.Pool, me, tid, access.View)
	if err != nil {
		return nil, err
	}
	if !src.IsTemplate || src.ParentID != nil || src.OrgID != orgID || src.DeletedAt != nil {
		return nil, httpx.NotFound("Template not found.")
	}
	// Placement and rights exactly as for a new page: create an empty page
	// where it goes, then fill it from the template.
	title := src.Title
	if strings.TrimSpace(req.Title) != "" {
		title = req.Title
	}
	dst, err := s.create(ctx, me, orgID, createPageReq{ParentID: req.ParentID, Kind: src.Kind, Title: title, Icon: src.Icon, Shared: req.Shared, TeamID: req.TeamID})
	if err != nil {
		return nil, err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		budget := &dupBudget{pages: maxDuplicatePages, blocks: maxDuplicateBlocks}
		return s.copyInto(ctx, tx, me, src.ID, dst.ID, budget)
	})
	if err != nil {
		return nil, err
	}
	s.treeChanged(ctx, orgID)
	return loadPageWithLevel(ctx, s, me, dst.ID)
}

// buildBuiltin creates a built-in template's page (and its databases and
// subpages) where req says.
func (s *Service) buildBuiltin(ctx context.Context, me, orgID uuid.UUID, t *builtinTemplate, req useTemplateReq, depth int) (*Page, error) {
	if depth > 3 {
		return nil, fmt.Errorf("template nests too deep")
	}
	title := t.Name
	if depth == 0 && strings.TrimSpace(req.Title) != "" {
		title = req.Title
	}
	icon := t.Icon
	kind := "page"
	if t.Database != nil {
		kind = "database"
	}
	p, err := s.create(ctx, me, orgID, createPageReq{ParentID: req.ParentID, Kind: kind, Title: title, Icon: &icon, Shared: req.Shared, TeamID: req.TeamID})
	if err != nil {
		return nil, err
	}
	if t.Cover != "" || t.FullWidth {
		var cover []byte
		if t.Cover != "" {
			cover = jsonOrNil(Cover{Gallery: t.Cover, Position: 50})
		}
		if _, err := s.d.Pool.Exec(ctx, `UPDATE docs_pages SET cover = COALESCE($2, cover), full_width = $3 WHERE id = $1`, p.ID, cover, t.FullWidth); err != nil {
			return nil, err
		}
	}
	if t.Database != nil {
		if err := s.fillDatabase(ctx, me, p, *t.Database); err != nil {
			return nil, err
		}
		return p, nil
	}
	dbIDs := map[string]uuid.UUID{}
	for _, d := range t.Databases {
		dicon := d.Icon
		dp, err := s.create(ctx, me, orgID, createPageReq{ParentID: &p.ID, Kind: "database", Inline: true, Title: d.Title, Icon: &dicon})
		if err != nil {
			return nil, err
		}
		if err := s.fillDatabase(ctx, me, dp, d); err != nil {
			return nil, err
		}
		dbIDs[d.Key] = dp.ID
	}
	pageIDs := map[string]uuid.UUID{}
	for i := range t.Subpages {
		sp, err := s.buildBuiltin(ctx, me, orgID, &t.Subpages[i], useTemplateReq{ParentID: &p.ID}, depth+1)
		if err != nil {
			return nil, err
		}
		pageIDs[t.Subpages[i].Key] = sp.ID
	}
	blocks := expandPlaceholders(templateBlocks(t.Markdown), func(kind, key string) *mdBlock {
		if kind == "database" {
			if id, ok := dbIDs[key]; ok {
				return &mdBlock{typ: "database", props: map[string]any{"database_id": id.String()}}
			}
			return nil
		}
		if id, ok := pageIDs[key]; ok {
			return &mdBlock{typ: "page", props: map[string]any{"page_id": id.String()}}
		}
		return nil
	})
	if ops := blockOps(blocks, nil, "", ""); len(ops) > 0 {
		for start := 0; start < len(ops); start += maxOpsPerBatch {
			end := min(start+maxOpsPerBatch, len(ops))
			if _, err := s.apply(ctx, me, p, ops[start:end], "template"); err != nil {
				return nil, fmt.Errorf("template blocks: %w", err)
			}
		}
	}
	return p, nil
}

// fillDatabase gives a new database a template's properties, views and rows
// in one transaction (replacing the default schema and view).
func (s *Service) fillDatabase(ctx context.Context, me uuid.UUID, dbp *Page, d tplDatabase) error {
	return s.fillDatabaseTx(ctx, me, dbp, d.Props, d.Views, d.Rows)
}

func (s *Service) fillDatabaseTx(ctx context.Context, me uuid.UUID, dbp *Page, props []tplProp, views []tplView, rows []map[string]any) error {
	schema := []Property{{ID: "title", Name: "Name", Type: "title"}}
	byName := map[string]Property{}
	for _, tp := range props {
		p := Property{ID: shortID(), Name: tp.Name, Type: tp.Type, NumberFormat: tp.Format}
		colors := optionColors
		for i, o := range tp.Options {
			name, group, _ := strings.Cut(o, "|")
			opt := Option{ID: shortID(), Name: name, Color: colors[(i+1)%len(colors)]}
			if tp.Type == "status" {
				opt.Group = group
				switch group {
				case "todo":
					opt.Color = "neutral"
				case "doing":
					opt.Color = "info"
				case "done":
					opt.Color = "success"
				}
			}
			p.Options = append(p.Options, opt)
		}
		if p.Options == nil && (tp.Type == "select" || tp.Type == "multi_select" || tp.Type == "status") {
			p.Options = []Option{}
		}
		schema = append(schema, p)
		byName[p.Name] = p
	}
	clean, err := validateSchema(schema)
	if err != nil {
		return err
	}
	for _, p := range clean {
		byName[p.Name] = p
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		raw, _ := json.Marshal(clean)
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, dbp.ID, raw); err != nil {
			return err
		}
		if len(views) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM docs_views WHERE database_id = $1`, dbp.ID); err != nil {
				return err
			}
			keys := keysAfter("", "", len(views))
			for i, v := range views {
				cfg := ViewConfig{}
				if p, ok := byName[v.GroupBy]; ok {
					cfg.GroupBy = p.ID
				}
				if p, ok := byName[v.Date]; ok {
					cfg.DateProperty = p.ID
				}
				if p, ok := byName[v.Sort]; ok {
					dir := "asc"
					if v.Desc {
						dir = "desc"
					}
					cfg.Sorts = []Sort{{Property: p.ID, Direction: dir}}
				}
				cfg = cleanConfig(clean, v.Type, cfg)
				b, _ := json.Marshal(cfg)
				if _, err := tx.Exec(ctx, `INSERT INTO docs_views (database_id, name, type, position, config, created_by) VALUES ($1, $2, $3, $4, $5, $6)`,
					dbp.ID, v.Name, v.Type, keys[i], b, me); err != nil {
					return err
				}
			}
		}
		keys := keysAfter("", "", len(rows))
		for i, row := range rows {
			values := map[string]any{}
			for name, v := range row {
				p, ok := byName[name]
				if !ok || p.Type == "title" {
					continue
				}
				if cv := templateValue(p, v, me); cv != nil {
					values[p.ID] = cv
				}
			}
			title, _ := row["title"].(string)
			if err := insertRow(ctx, tx, dbp, me, title, keys[i], values); err != nil {
				return err
			}
		}
		return nil
	})
}

// insertRow adds a row page to a database inside tx (values already clean).
func insertRow(ctx context.Context, tx pgx.Tx, dbp *Page, me uuid.UUID, title, pos string, values map[string]any) error {
	_, err := insertRowID(ctx, tx, dbp, me, title, pos, values)
	return err
}

// templateValue turns a template row value into the stored form for p.
func templateValue(p Property, v any, me uuid.UUID) any {
	optID := func(name string) string {
		for _, o := range p.Options {
			if strings.EqualFold(o.Name, name) {
				return o.ID
			}
		}
		return ""
	}
	switch p.Type {
	case "select", "status":
		if s, ok := v.(string); ok {
			if id := optID(s); id != "" {
				return id
			}
		}
	case "multi_select":
		var out []string
		switch x := v.(type) {
		case []any:
			for _, e := range x {
				if s, ok := e.(string); ok {
					if id := optID(s); id != "" {
						out = append(out, id)
					}
				}
			}
		case string:
			if id := optID(x); id != "" {
				out = append(out, id)
			}
		}
		if len(out) > 0 {
			return out
		}
	case "person":
		if s, ok := v.(string); ok && s == "@me" {
			return []string{me.String()}
		}
	case "date":
		if s, ok := v.(string); ok {
			if d, ok := templateDay(s); ok {
				return map[string]any{"start": d}
			}
			if validDateValue(s) {
				return map[string]any{"start": s}
			}
		}
	case "number":
		switch x := v.(type) {
		case float64:
			return x
		case int:
			return float64(x)
		}
	case "checkbox":
		if b, ok := v.(bool); ok {
			return b
		}
	case "text", "url", "email":
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return nil
}

// ---- workspace templates ---------------------------------------------------------

type saveTemplateReq struct {
	Shared bool `json:"shared"` // everyone in the workspace can open (and use) it
}

// saveAsTemplate: POST /docs/pages/{id}/template (view on the page; 30/h)
// {shared?}: a copy of the page and the subpages the caller can open becomes
// a workspace template they own (private, or for everyone with
// docs.create_shared). The original stays as it is.
func (s *Service) saveAsTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req saveTemplateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	src, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if src.DeletedAt != nil {
		return errInTrash
	}
	if src.IsRow {
		return httpx.BadRequest("Rows use their database's templates.")
	}
	if src.IsTemplate {
		return httpx.BadRequest("This page is already a template.")
	}
	if role, err := orgs.RoleOf(ctx, s.d.Pool, src.OrgID, me); err != nil {
		return err
	} else if role == "" {
		return errPageNotFound
	}
	general := access.None
	if req.Shared {
		ok, err := access.Allowed(ctx, s.d.Pool, src.OrgID, me, PermCreateShared)
		if err != nil {
			return err
		}
		if !ok {
			return permissionRequired(PermCreateShared)
		}
		general = access.View
	}
	var newID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		pos, err := siblingPosition(ctx, tx, src.OrgID, nil, nil)
		if err != nil {
			return err
		}
		budget := &dupBudget{pages: maxDuplicatePages, blocks: maxDuplicateBlocks}
		if newID, err = s.copyTree(ctx, tx, me, src.ID, nil, pos, budget); err != nil {
			return err
		}
		if general > access.None {
			if err := access.SetGrant(ctx, tx, access.Ref{Kind: Kind, ID: newID}, access.SubjectRef{Type: "org", ID: src.OrgID}, general, me); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
			WITH RECURSIVE tree (id, depth) AS (
				SELECT $1::uuid, 0
				UNION ALL
				SELECT c.id, t.depth + 1 FROM tree t JOIN docs_pages c ON c.parent_id = t.id WHERE t.depth < 64
			)
			UPDATE docs_pages SET is_template = true WHERE id IN (SELECT id FROM tree) AND NOT is_row`, newID)
		return err
	})
	if err != nil {
		return err
	}
	out, err := loadPageWithLevel(ctx, s, me, newID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// stopTemplate: DELETE /docs/pages/{id}/template (edit): the template becomes
// an ordinary page again (it appears in the tree and search).
func (s *Service) stopTemplate(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if !p.IsTemplate || p.ParentID != nil {
		return httpx.BadRequest("That page isn't a template.")
	}
	if _, err := s.d.Pool.Exec(ctx, `
		WITH RECURSIVE tree (id, depth) AS (
			SELECT $1::uuid, 0
			UNION ALL
			SELECT c.id, t.depth + 1 FROM tree t JOIN docs_pages c ON c.parent_id = t.id WHERE t.depth < 64
		)
		UPDATE docs_pages SET is_template = false, updated_at = now() WHERE id IN (SELECT id FROM tree) AND NOT is_row`, id); err != nil {
		return err
	}
	out, err := loadPageWithLevel(ctx, s, me, id)
	if err != nil {
		return err
	}
	s.pageChanged(ctx, out, true)
	httpx.OK(w, out)
	return nil
}
