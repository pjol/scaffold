package docs

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// CSV in and out of databases. Export writes a view's rows (its filters,
// sorts and shown properties) or everything, as the viewer sees them —
// computed values included. Import into an existing database maps columns
// to properties (or new ones, typed from their values), runs in the
// background as an import run (docs_imports) with a report of rows it
// couldn't take. Parsing and type inference are shared with the page
// importer (import_csv.go); csvCellValue converts one cell for any type.

// csvCellValue converts a CSV cell to the stored value of p (nil: empty or
// unreadable). Currencies become minor units; options match by name.
func csvCellValue(p Property, cell string) any {
	cell = strings.TrimSpace(cleanText(cell))
	if cell == "" && p.Type != "checkbox" {
		return nil
	}
	opt := func(name string) string {
		for _, o := range p.Options {
			if strings.EqualFold(o.Name, name) {
				return o.ID
			}
		}
		return ""
	}
	switch p.Type {
	case "checkbox":
		b, _ := csvBool(cell)
		return b
	case "number":
		f, ok := parseCSVNumber(cell)
		if !ok {
			return nil
		}
		if _, cur := currencyExp(p.NumberFormat); cur {
			return math.Round(f * numberScale(p))
		}
		return f
	case "date":
		d, ok := parseCSVDate(cell)
		if !ok {
			return nil
		}
		return d
	case "select", "status":
		if id := opt(cell); id != "" {
			return id
		}
		return nil
	case "multi_select":
		var ids []string
		for _, part := range splitList(cell) {
			if id := opt(part); id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		return ids
	case "url":
		u := cell
		if !strings.Contains(u, "://") {
			u = "https://" + u
		}
		if u = safeLink(u); u == "" || strings.HasPrefix(u, "mailto:") {
			return nil
		}
		return u
	case "email":
		if !emailRE.MatchString(cell) {
			return nil
		}
		return cell
	case "phone":
		if !phoneRE.MatchString(cell) {
			return nil
		}
		return cell
	case "files":
		var out []map[string]any
		for _, part := range splitList(cell) {
			if u := safeLink(part); u != "" && !strings.HasPrefix(u, "mailto:") && len(out) < maxFiles {
				out = append(out, map[string]any{"url": u, "name": part})
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case "text":
		if utf8.RuneCountInString(cell) > maxTextValue {
			cell = string([]rune(cell)[:maxTextValue])
		}
		return cell
	}
	return nil // people, relations: resolved against the workspace (importer)
}

// splitList reads "a, b, c" (Notion's export joins lists with ", ").
func splitList(cell string) []string {
	var out []string
	for _, part := range strings.Split(cell, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ---- export ------------------------------------------------------------------------

// exportCSV: GET /docs/databases/{id}/csv?view=&all=1&tz= (view on the
// database): the view's rows and shown properties (all=1: every row and
// property), as the caller can open them.
func (s *Service) exportCSV(w http.ResponseWriter, r *http.Request) error {
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
	q := r.URL.Query()
	all := q.Get("all") == "1"
	views, err := s.viewsFor(ctx, id, q.Get("view"))
	if err != nil {
		return err
	}
	rq, err := rowsRequest(r, dbp, views)
	if err != nil {
		return err
	}
	rq.me, rq.agent, rq.token, rq.noMeta = me, access.ViaAgent(ctx), "", true
	if all {
		rq.filters, rq.sorts, rq.search = nil, nil, ""
	}
	data, err := s.CSVExport(ctx, rq, all)
	if err != nil {
		return err
	}
	name := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) || r < 32 {
			return '-'
		}
		return r
	}, displayTitle(dbp.Title))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, strings.ReplaceAll(name, `"`, "")))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
	return nil
}

// CSVExport renders a row query as CSV (with a byte-order mark for
// spreadsheets): the title first, then the view's shown properties in its
// order (every property when all), values as text the way the reader sees
// them. Exports and other features call it.
func (s *Service) CSVExport(ctx context.Context, rq rowQuery, all bool) ([]byte, error) {
	schema := rq.db.Schema
	cols := exportColumns(schema, rq.view, all)
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // byte-order mark: spreadsheets read UTF-8
	cw := csv.NewWriter(&buf)
	header := make([]string, len(cols))
	for i, p := range cols {
		header[i] = p.Name
	}
	_ = cw.Write(header)
	rq.limit = 1000
	n := 0
	for {
		page, err := s.queryRows(ctx, rq)
		if err != nil {
			return nil, err
		}
		for _, row := range page.Items {
			rec := make([]string, len(cols))
			for i, p := range cols {
				rec[i] = csvText(p, row, page.Refs)
			}
			_ = cw.Write(rec)
			n++
		}
		if page.NextCursor == nil || n >= maxRows {
			break
		}
		rq.cursor = *page.NextCursor
	}
	cw.Flush()
	return buf.Bytes(), cw.Error()
}

func exportColumns(schema []Property, v *View, all bool) []Property {
	var cols []Property
	if title, ok := findProp(schema, "title"); ok {
		cols = append(cols, title)
	}
	if v != nil && !all && len(v.Config.Columns) > 0 {
		for _, c := range v.Config.Columns {
			if p, ok := findProp(schema, c.Property); ok && !c.Hidden && p.Type != "title" && p.Type != "button" {
				cols = append(cols, p)
			}
		}
		for _, p := range schema {
			if p.Type != "title" && p.Type != "button" && !slices.ContainsFunc(v.Config.Columns, func(c Column) bool { return c.Property == p.ID }) {
				cols = append(cols, p)
			}
		}
		return cols
	}
	for _, p := range schema {
		if p.Type != "title" && p.Type != "button" {
			cols = append(cols, p)
		}
	}
	return cols
}

// csvText is a row value as export text: option and people's names,
// related rows' titles (only those the reader can open), currencies in
// major units, dates as YYYY-MM-DD[ HH:MM] (ranges "a → b").
func csvText(p Property, row Row, refs Refs) string {
	switch p.Type {
	case "title":
		return row.Title
	case "created_time":
		return row.CreatedAt.UTC().Format("2006-01-02 15:04")
	case "last_edited_time":
		return row.UpdatedAt.UTC().Format("2006-01-02 15:04")
	case "created_by", "last_edited_by":
		id := row.CreatedBy
		if p.Type == "last_edited_by" {
			id = row.UpdatedBy
		}
		if id != nil {
			return refs.Users[id.String()].Name
		}
		return ""
	}
	return valueText(p, row.Props[p.ID], refs)
}

// valueText is a stored or computed value as text, for every property type
// (exports, Markdown, agent tools): option and people's names, related rows'
// titles the reader can open, currencies in major units, "TASK-12", files'
// links or names, formula and rollup results.
func valueText(p Property, v any, refs Refs) string {
	if v == nil {
		return ""
	}
	optName := func(id string) string {
		for _, o := range p.Options {
			if o.ID == id {
				return o.Name
			}
		}
		return ""
	}
	switch p.Type {
	case "select", "status":
		s, _ := v.(string)
		return optName(s)
	case "multi_select", "person":
		var names []string
		for _, s := range fStringList(v) {
			if p.Type == "person" {
				if u, ok := refs.Users[s]; ok {
					names = append(names, u.Name)
				}
			} else if n := optName(s); n != "" {
				names = append(names, n)
			}
		}
		return strings.Join(names, ", ")
	case "checkbox":
		if b, _ := v.(bool); b {
			return "Yes"
		}
		return "No"
	case "date":
		m, _ := v.(map[string]any)
		start, _ := m["start"].(string)
		out := strings.Replace(start, "T", " ", 1)
		if end, _ := m["end"].(string); end != "" {
			out += " → " + strings.Replace(end, "T", " ", 1)
		}
		return out
	case "relation":
		var titles []string
		for _, id := range relationValues(v) {
			if ref, ok := refs.Pages[id.String()]; ok && ref.Title != nil {
				titles = append(titles, *ref.Title)
			}
		}
		return strings.Join(titles, ", ")
	case "files":
		var names []string
		if list, ok := v.([]any); ok {
			for _, x := range list {
				m, _ := x.(map[string]any)
				if u, _ := m["url"].(string); u != "" {
					names = append(names, u)
				} else if n, _ := m["name"].(string); n != "" {
					names = append(names, n)
				}
			}
		}
		return strings.Join(names, ", ")
	case "unique_id":
		if f, ok := fToFloat(v); ok {
			prefix := ""
			if p.UniqueID != nil && p.UniqueID.Prefix != "" {
				prefix = p.UniqueID.Prefix + "-"
			}
			return prefix + fFmtNum(f)
		}
		return ""
	case "number":
		if f, ok := fToFloat(v); ok {
			return fFmtNum(f / numberScale(p))
		}
		return ""
	case "formula", "rollup":
		return jsonValueText(v, refs)
	}
	s, _ := v.(string)
	return s
}

// jsonValueText reads a computed value's JSON as text.
func jsonValueText(v any, refs Refs) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return fFmtNum(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if t := jsonValueText(e, refs); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if start, ok := x["start"].(string); ok {
			s := strings.Replace(start, "T", " ", 1)
			if end, _ := x["end"].(string); end != "" {
				s += " → " + strings.Replace(end, "T", " ", 1)
			}
			return s
		}
		if id, ok := x["person"].(string); ok {
			return refs.Users[id].Name
		}
		if id, ok := x["page"].(string); ok {
			if ref, ok := refs.Pages[id]; ok && ref.Title != nil {
				return *ref.Title
			}
		}
	}
	return ""
}

// ---- import -------------------------------------------------------------------------

type csvSource struct {
	Text string `json:"text"` // up to about 1 MB inline
	Key  string `json:"key"`  // or an upload (purpose docs_import)
}

func (s *Service) csvRecords(ctx context.Context, me uuid.UUID, src csvSource) ([][]string, error) {
	data := []byte(src.Text)
	if src.Key != "" {
		if !storage.KeyOwnedBy(src.Key, ImportPurpose, me) {
			return nil, httpx.BadRequest("That upload isn't yours.")
		}
		var err error
		if data, err = s.readImport(ctx, src.Key); err != nil {
			return nil, err
		}
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, httpx.BadRequest("The CSV is empty.")
	}
	tab := bytes.Count(data[:min(len(data), 4096)], []byte("\t")) > bytes.Count(data[:min(len(data), 4096)], []byte(","))
	records, err := readCSV(data, tab)
	if err != nil {
		return nil, httpx.BadRequest("That file isn't CSV we can read.")
	}
	if len(records) < 1 {
		return nil, httpx.BadRequest("The CSV has no header row.")
	}
	if len(records)-1 > maxRows {
		return nil, httpx.BadRequest(fmt.Sprintf("A database holds up to %d rows; this CSV has %d.", maxRows, len(records)-1))
	}
	return records, nil
}

// CSVColumn is a column as the import preview reads it.
type CSVColumn struct {
	Index  int      `json:"index"`
	Name   string   `json:"name"`
	Type   string   `json:"type"`   // inferred property type
	Format string   `json:"format"` // number format, when a number
	Sample []string `json:"sample"`
	Match  string   `json:"match"` // the property with the same name, if any
}

// CSVPreview is what an import would read.
type CSVPreview struct {
	Columns     []CSVColumn `json:"columns"`
	Rows        int         `json:"rows"`
	TitleColumn int         `json:"title_column"`
}

// previewCSV: POST /docs/databases/{id}/csv/preview (edit): columns with
// inferred types and matching properties, to map before importing.
func (s *Service) previewCSV(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var src csvSource
	if err := httpx.Decode(r, &src); err != nil {
		return err
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.Edit)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	records, err := s.csvRecords(ctx, me, src)
	if err != nil {
		return err
	}
	httpx.OK(w, previewRecords(dbp.Schema, records))
	return nil
}

func previewRecords(schema []Property, records [][]string) CSVPreview {
	header, rows := records[0], records[1:]
	out := CSVPreview{Columns: []CSVColumn{}, Rows: len(rows), TitleColumn: 0}
	title, _ := findProp(schema, "title")
	for i, h := range header {
		n := strings.ToLower(strings.TrimSpace(h))
		if n == strings.ToLower(title.Name) || n == "name" || n == "title" {
			out.TitleColumn = i
			break
		}
	}
	for i, h := range header {
		name := strings.TrimSpace(cleanText(h))
		if name == "" {
			name = fmt.Sprintf("Column %d", i+1)
		}
		var vals, sample []string
		for _, r := range rows {
			if i < len(r) && strings.TrimSpace(r[i]) != "" {
				vals = append(vals, strings.TrimSpace(r[i]))
				if len(sample) < 3 {
					sample = append(sample, strings.TrimSpace(r[i]))
				}
			}
		}
		typ, format, _ := inferType(name, vals)
		col := CSVColumn{Index: i, Name: name, Type: typ, Format: format, Sample: sample}
		if sample == nil {
			col.Sample = []string{}
		}
		if p, ok := findPropByName(schema, name); ok && writable(p) {
			col.Match = p.ID
		}
		out.Columns = append(out.Columns, col)
	}
	return out
}

// CSVMapping maps one column: to a property, to a new property, or skipped.
type CSVMapping struct {
	Column   int    `json:"column"`
	Property string `json:"property,omitempty"` // an existing property id
	New      *struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"new,omitempty"`
}

type csvImportReq struct {
	csvSource
	TitleColumn int          `json:"title_column"`
	Mapping     []CSVMapping `json:"mapping"`
}

// importCSV: POST /docs/databases/{id}/csv/import (edit): adds the CSV's
// rows in the background; poll GET /docs/imports/{id} for progress and the
// report.
func (s *Service) importCSV(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req csvImportReq
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
	records, err := s.csvRecords(ctx, me, req.csvSource)
	if err != nil {
		return err
	}
	run, err := s.CSVImport(ctx, me, dbp, records, req.TitleColumn, req.Mapping)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, run)
	return nil
}

// CSVImport adds records (header first) to an existing database: new
// properties are created (select options from the values), missing options
// are added, then rows go in batches in the background. It returns the
// import run to poll.
func (s *Service) CSVImport(ctx context.Context, me uuid.UUID, dbp *Page, records [][]string, titleCol int, mapping []CSVMapping) (*ImportRun, error) {
	header, rows := records[0], records[1:]
	if titleCol < 0 || titleCol >= len(header) {
		titleCol = -1
	}
	schema := append([]Property(nil), dbp.Schema...)
	var maps []csvCol
	for _, m := range mapping {
		if m.Column < 0 || m.Column >= len(header) || m.Column == titleCol {
			continue
		}
		switch {
		case m.New != nil:
			typ := m.New.Type
			if !slices.Contains([]string{"text", "number", "select", "multi_select", "status", "date", "checkbox", "url", "email", "phone", "files"}, typ) {
				return nil, httpx.BadRequest("A new column becomes text, number, select, multi-select, status, date, checkbox, URL, email, phone or files.")
			}
			name := strings.TrimSpace(cleanText(m.New.Name))
			if name == "" {
				name = strings.TrimSpace(header[m.Column])
			}
			p := Property{ID: shortID(), Name: uniquePropName(schema, name), Type: typ}
			if typ == "number" {
				var vals []string
				for _, r := range rows {
					if m.Column < len(r) {
						vals = append(vals, r[m.Column])
					}
				}
				_, p.NumberFormat, _ = inferType(name, vals)
			}
			if typ == "status" {
				p.Options = statusProperty(name).Options
			}
			schema = append(schema, p)
			maps = append(maps, csvCol{m.Column, p.ID})
		case m.Property != "":
			p, ok := findProp(schema, m.Property)
			if !ok || (!writable(p) && p.Type != "title") {
				return nil, httpx.BadRequest("Column " + header[m.Column] + ": choose a property values can be written to.")
			}
			if p.Type == "title" {
				titleCol = m.Column
				continue
			}
			maps = append(maps, csvCol{m.Column, p.ID})
		}
	}
	// Options the values name that the properties don't have yet.
	for _, cm := range maps {
		for i := range schema {
			p := &schema[i]
			if p.ID != cm.prop || (p.Type != "select" && p.Type != "multi_select" && p.Type != "status") {
				continue
			}
			for _, r := range rows {
				if cm.col >= len(r) {
					continue
				}
				names := []string{strings.TrimSpace(r[cm.col])}
				if p.Type == "multi_select" {
					names = splitList(r[cm.col])
				}
				for _, n := range names {
					if n == "" || len(p.Options) >= maxOptions || slices.ContainsFunc(p.Options, func(o Option) bool { return strings.EqualFold(o.Name, n) }) {
						continue
					}
					o := Option{ID: shortID(), Name: string([]rune(cleanText(n))[:min(utf8.RuneCountInString(n), 100)]), Color: optionColors[1+len(p.Options)%9]}
					if p.Type == "status" {
						o.Group = statusGroupOf(n)
					}
					p.Options = append(p.Options, o)
				}
			}
		}
	}
	if len(schema) != len(dbp.Schema) || !slices.EqualFunc(schema, dbp.Schema, func(a, b Property) bool { return len(a.Options) == len(b.Options) }) {
		saved, err := s.saveSchema(ctx, me, dbp, schema, "")
		if err != nil {
			return nil, err
		}
		dbp.Schema = saved.Schema
	}
	var runID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `INSERT INTO docs_imports (org_id, user_id, parent_id, name, format, status, pages_total, root_page_id)
		VALUES ($1, $2, $3, $4, 'csv', 'running', $5, $3) RETURNING id`, dbp.OrgID, me, dbp.ID, displayTitle(dbp.Title)+".csv", len(rows)).Scan(&runID)
	if err != nil {
		return nil, err
	}
	agent := access.ViaAgent(ctx)
	bg.Go(ctx, "docs.csv_import", func(ctx context.Context) error {
		ctx = context.WithValue(ctx, csvImportKey{}, true)
		s.runCSVImport(ctx, me, agent, runID, dbp, rows, titleCol, maps)
		return nil
	})
	return scanImport(s.d.Pool.QueryRow(ctx, `SELECT `+importCols+` FROM docs_imports WHERE id = $1`, runID))
}

type csvImportKey struct{}

type csvCol struct {
	col  int
	prop string
}

// runCSVImport inserts the rows in batches of 500 (one transaction each),
// resolving people by name or email in the workspace and relations by the
// related rows' titles the importer can open; cells it can't read are
// reported (the row still goes in).
func (s *Service) runCSVImport(ctx context.Context, me uuid.UUID, agent bool, runID uuid.UUID, dbp *Page, rows [][]string, titleCol int, maps []csvCol) {
	var errs []ImportError
	report := func(i int, msg string) {
		if len(errs) < maxImportErrors {
			errs = append(errs, ImportError{Path: fmt.Sprintf("Row %d", i+2), Message: msg})
		}
	}
	people := map[string]string{}
	if pr, err := s.d.Pool.Query(ctx, `SELECT u.id, lower(u.display_name), lower(COALESCE(u.email, '')) FROM users u JOIN org_members m ON m.user_id = u.id WHERE m.org_id = $1`, dbp.OrgID); err == nil {
		for pr.Next() {
			var id uuid.UUID
			var name, email string
			if pr.Scan(&id, &name, &email) == nil {
				people[name] = id.String()
				if email != "" {
					people[email] = id.String()
				}
			}
		}
		pr.Close()
	}
	related := map[uuid.UUID]map[string]string{}
	for _, cm := range maps {
		p, _ := findProp(dbp.Schema, cm.prop)
		if p.Type != "relation" || p.Relation == nil || related[p.Relation.DatabaseID] != nil {
			continue
		}
		titles := map[string]string{}
		if rr, err := s.d.Pool.Query(ctx, `SELECT id, lower(title) FROM docs_pages WHERE parent_id = $1 AND is_row AND NOT is_template AND deleted_at IS NULL
			AND access_level($2, 'docs.page', id, $3) >= 1`, p.Relation.DatabaseID, me, agent); err == nil {
			for rr.Next() {
				var id uuid.UUID
				var t string
				if rr.Scan(&id, &t) == nil {
					if _, dup := titles[t]; !dup {
						titles[t] = id.String()
					}
				}
			}
			rr.Close()
		}
		related[p.Relation.DatabaseID] = titles
	}
	done := 0
	fail := func(msg string) {
		raw, _ := json.Marshal(errs)
		_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE docs_imports SET status = 'failed', message = $2, errors = $3, pages_done = $4, finished_at = now(), heartbeat_at = now() WHERE id = $1`,
			runID, msg, raw, done)
	}
	var rowIDs []uuid.UUID
	for start := 0; start < len(rows); start += 500 {
		batch := rows[start:min(start+500, len(rows))]
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM docs_pages WHERE parent_id = $1 AND is_row AND deleted_at IS NULL`, dbp.ID).Scan(&n); err != nil {
				return err
			}
			if n+len(batch) > maxRows {
				return httpx.NewError(http.StatusConflict, "database_full", fmt.Sprintf("A database holds up to %d rows.", maxRows))
			}
			var last string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(max(position), '') FROM docs_pages WHERE parent_id = $1`, dbp.ID).Scan(&last); err != nil {
				return err
			}
			keys := keysAfter(last, "", len(batch))
			for j, rec := range batch {
				i := start + j
				title := ""
				if titleCol >= 0 && titleCol < len(rec) {
					title = strings.TrimSpace(rec[titleCol])
				}
				values := map[string]any{}
				for _, cm := range maps {
					if cm.col >= len(rec) || strings.TrimSpace(rec[cm.col]) == "" {
						continue
					}
					p, ok := findProp(dbp.Schema, cm.prop)
					if !ok {
						continue
					}
					cell := strings.TrimSpace(rec[cm.col])
					var v any
					switch p.Type {
					case "person":
						var ids []string
						for _, part := range splitList(cell) {
							if id, ok := people[strings.ToLower(part)]; ok && !slices.Contains(ids, id) {
								ids = append(ids, id)
							}
						}
						if len(ids) > 0 {
							v = ids
						}
					case "relation":
						var ids []string
						for _, part := range splitList(cell) {
							if id, ok := related[p.Relation.DatabaseID][strings.ToLower(part)]; ok && !slices.Contains(ids, id) {
								ids = append(ids, id)
							}
						}
						if p.Relation.Limit == 1 && len(ids) > 1 {
							ids = ids[:1]
						}
						if len(ids) > 0 {
							v = ids
						}
					default:
						v = csvCellValue(p, cell)
					}
					if v == nil {
						report(i, fmt.Sprintf("%s: couldn't read “%s”.", p.Name, truncate(cell, 60)))
						continue
					}
					values[p.ID] = v
				}
				t, err := cleanTitle(title)
				if err != nil {
					t = truncate(strings.ReplaceAll(title, "\n", " "), maxTitleChars)
				}
				id, err := insertRowID(ctx, tx, dbp, me, t, keys[j], values)
				if err != nil {
					return err
				}
				if err := assignUniqueIDs(ctx, tx, dbp.ID, id, dbp.Schema); err != nil {
					return err
				}
				for _, p := range dbp.Schema {
					if p.Type == "relation" && p.Relation != nil && p.Relation.MirrorProperty != "" && values[p.ID] != nil {
						if _, err := syncMirror(ctx, tx, id, p, s.mirrorOf(ctx, tx, p), nil, relationValues(values[p.ID])); err != nil {
							return err
						}
					}
				}
				rowIDs = append(rowIDs, id)
			}
			return nil
		})
		if err != nil {
			fail(importMessage(err))
			s.publish(ctx, pageTopic(dbp.ID), EventRows, map[string]any{"database_id": dbp.ID})
			return
		}
		done += len(batch)
		raw, _ := json.Marshal(errs)
		_, _ = s.d.Pool.Exec(ctx, `UPDATE docs_imports SET pages_done = $2, errors = $3, heartbeat_at = now() WHERE id = $1`, runID, done, raw)
	}
	raw, _ := json.Marshal(errs)
	if errs == nil {
		raw = []byte("[]")
	}
	_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE docs_imports SET status = 'done', pages_done = $2, errors = $3, finished_at = now(), heartbeat_at = now() WHERE id = $1`,
		runID, done, raw)
	s.publish(ctx, pageTopic(dbp.ID), EventRows, map[string]any{"database_id": dbp.ID})
	s.notifyRelated(ctx, dbp.ID)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
