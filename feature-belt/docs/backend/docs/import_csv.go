package docs

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// CSV becomes a database: the "Name" (or "Title") column, else the first, is
// each row's title; every other column becomes a property whose type is
// read from its values (checkbox, number, date, email, URL, select,
// multi-select, status, else text). A Notion export's row pages (Markdown
// files in the database's folder) become the rows' content.

const maxCSVRows = 10000

// csvColumn is an inferred property for one CSV column.
type csvColumn struct {
	name    string
	typ     string
	format  string
	options []string // in first-seen order
	index   int
}

var (
	numberClean = strings.NewReplacer(",", "", "$", "", "€", "", "£", "", "%", "", " ", "")
	emailCellRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	urlCellRE   = regexp.MustCompile(`^https?://\S+$`)
)

var dateLayouts = []string{
	"2006-01-02", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05Z07:00", "2006/01/02",
	"January 2, 2006", "January 2, 2006 3:04 PM", "Jan 2, 2006", "Jan 2, 2006 3:04 PM", "2 January 2006", "02/01/2006", "01/02/2006", "1/2/2006",
}

// parseCSVDate reads a date ("start → end" ranges too) as YYYY-MM-DD[THH:MM].
func parseCSVDate(v string) (map[string]any, bool) {
	v = strings.TrimSpace(v)
	start, end, isRange := strings.Cut(v, "→")
	parse := func(s string) (string, bool) {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "(UTC)"))
		for _, l := range dateLayouts {
			if t, err := time.Parse(l, s); err == nil {
				if strings.Contains(l, "15") || strings.Contains(l, "3:04") {
					return t.Format("2006-01-02T15:04"), true
				}
				return t.Format("2006-01-02"), true
			}
		}
		return "", false
	}
	a, ok := parse(start)
	if !ok {
		return nil, false
	}
	out := map[string]any{"start": a}
	if isRange {
		b, ok := parse(end)
		if !ok {
			return nil, false
		}
		out["end"] = b
	}
	return out, true
}

func parseCSVNumber(v string) (float64, bool) {
	c := numberClean.Replace(strings.TrimSpace(v))
	if c == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(c, 64)
	return f, err == nil
}

func csvBool(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "✓", "✔", "x", "checked", "1":
		return true, true
	case "no", "false", "", "unchecked", "0":
		return false, true
	}
	return false, false
}

// inferColumns reads property types from the values.
func inferColumns(header []string, rows [][]string, titleCol int) []csvColumn {
	var cols []csvColumn
	for i, h := range header {
		if i == titleCol {
			continue
		}
		name := strings.TrimSpace(cleanText(h))
		if name == "" {
			name = fmt.Sprintf("Column %d", i+1)
		}
		if utf8.RuneCountInString(name) > 100 {
			name = string([]rune(name)[:100])
		}
		var vals []string
		for _, r := range rows {
			if i < len(r) && strings.TrimSpace(r[i]) != "" {
				vals = append(vals, strings.TrimSpace(r[i]))
			}
		}
		col := csvColumn{name: name, typ: "text", index: i}
		col.typ, col.format, col.options = inferType(name, vals)
		cols = append(cols, col)
		if len(cols) >= maxProperties-1 {
			break
		}
	}
	return cols
}

func inferType(name string, vals []string) (typ, format string, options []string) {
	if len(vals) == 0 {
		return "text", "", nil
	}
	all := func(fn func(string) bool) bool {
		for _, v := range vals {
			if !fn(v) {
				return false
			}
		}
		return true
	}
	if all(func(v string) bool { _, ok := csvBool(v); return ok && v != "1" && v != "0" }) {
		return "checkbox", "", nil
	}
	if all(func(v string) bool { _, ok := parseCSVNumber(v); return ok }) {
		switch {
		case all(func(v string) bool { return strings.HasSuffix(strings.TrimSpace(v), "%") }):
			return "number", "percent", nil
		case all(func(v string) bool { return strings.HasPrefix(strings.TrimSpace(v), "$") }):
			return "number", "dollar", nil
		case all(func(v string) bool { return strings.HasPrefix(strings.TrimSpace(v), "€") }):
			return "number", "euro", nil
		case all(func(v string) bool { return strings.HasPrefix(strings.TrimSpace(v), "£") }):
			return "number", "pound", nil
		}
		return "number", "number", nil
	}
	if all(func(v string) bool { _, ok := parseCSVDate(v); return ok }) {
		return "date", "", nil
	}
	if all(emailCellRE.MatchString) {
		return "email", "", nil
	}
	if all(urlCellRE.MatchString) {
		return "url", "", nil
	}
	// Few distinct short values: a select (a "Status" column: status).
	seen := map[string]bool{}
	var distinct []string
	short := true
	multi := false
	for _, v := range vals {
		parts := []string{v}
		if strings.Contains(v, ", ") {
			parts = strings.Split(v, ", ")
			multi = true
		}
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if utf8.RuneCountInString(p) > 40 {
				short = false
			}
			if p != "" && !seen[p] {
				seen[p] = true
				distinct = append(distinct, p)
			}
		}
	}
	limit := max(3, min(30, len(vals)/2+1))
	if short && len(distinct) <= limit && len(distinct) <= maxOptions && (len(vals) >= 3 || multi) {
		switch {
		case multi:
			return "multi_select", "", distinct
		case strings.EqualFold(name, "status"):
			return "status", "", distinct
		default:
			return "select", "", distinct
		}
	}
	return "text", "", nil
}

func statusGroupOf(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "done") || strings.Contains(n, "complete") || strings.Contains(n, "shipped") || strings.Contains(n, "closed") || strings.Contains(n, "fixed"):
		return "done"
	case strings.Contains(n, "progress") || strings.Contains(n, "doing") || strings.Contains(n, "review") || strings.Contains(n, "active"):
		return "doing"
	}
	return "todo"
}

// readCSV parses CSV (or TSV) text, dropping a byte-order mark and blank rows.
func readCSV(data []byte, tab bool) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	if tab {
		r.Comma = '\t'
	}
	var out [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, httpx.BadRequest("the CSV can't be read: " + err.Error())
		}
		empty := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if !empty {
			out = append(out, rec)
		}
		if len(out) > maxCSVRows+1 {
			return nil, httpx.BadRequest(fmt.Sprintf("it has more than %d rows", maxCSVRows))
		}
	}
	if len(out) == 0 {
		return nil, httpx.BadRequest("the CSV is empty")
	}
	return out, nil
}

// csvDatabase builds the schema and row values for a CSV.
type csvDatabase struct {
	schema []Property
	titles []string
	values []map[string]any
}

func buildCSVDatabase(records [][]string) (*csvDatabase, error) {
	header, rows := records[0], records[1:]
	titleCol := 0
	for i, h := range header {
		if n := strings.ToLower(strings.TrimSpace(h)); n == "name" || n == "title" {
			titleCol = i
			break
		}
	}
	titleName := strings.TrimSpace(cleanText(header[titleCol]))
	if titleName == "" {
		titleName = "Name"
	}
	cols := inferColumns(header, rows, titleCol)
	out := &csvDatabase{schema: []Property{{ID: "title", Name: titleName, Type: "title"}}}
	props := make([]Property, len(cols))
	for i, c := range cols {
		p := Property{ID: shortID(), Name: c.name, Type: c.typ}
		if c.typ == "number" {
			p.NumberFormat = c.format
		}
		for j, o := range c.options {
			opt := Option{ID: shortID(), Name: o, Color: optionColors[(j+1)%len(optionColors)]}
			if c.typ == "status" {
				opt.Group = statusGroupOf(o)
			}
			p.Options = append(p.Options, opt)
		}
		if p.Options == nil && (c.typ == "select" || c.typ == "multi_select" || c.typ == "status") {
			p.Options = []Option{}
		}
		props[i] = p
	}
	out.schema = append(out.schema, props...)
	clean, err := validateSchema(out.schema)
	if err != nil {
		return nil, err
	}
	out.schema = clean
	byName := map[string]Property{}
	for _, p := range clean {
		byName[p.Name] = p
	}
	for _, r := range rows {
		title := ""
		if titleCol < len(r) {
			title = strings.TrimSpace(r[titleCol])
		}
		vals := map[string]any{}
		for _, c := range cols {
			if c.index >= len(r) {
				continue
			}
			p, ok := byName[c.name]
			if !ok {
				continue
			}
			if v := csvValue(p, r[c.index]); v != nil {
				vals[p.ID] = v
			}
		}
		out.titles = append(out.titles, title)
		out.values = append(out.values, vals)
	}
	return out, nil
}

// csvValue converts a cell to the stored value for p (nil: empty).
func csvValue(p Property, cell string) any {
	cell = strings.TrimSpace(cleanText(cell))
	if cell == "" && p.Type != "checkbox" {
		return nil
	}
	opt := func(name string) string {
		for _, o := range p.Options {
			if o.Name == name {
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
		// Percent stores the number shown (50 is "50%"); currencies store
		// integer minor units (numberScale, properties.go).
		if sc := numberScale(p); sc != 1 {
			f = math.Round(f * sc)
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
		for _, part := range strings.Split(cell, ", ") {
			if id := opt(strings.TrimSpace(part)); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		return ids
	}
	if utf8.RuneCountInString(cell) > maxTextValue {
		cell = string([]rune(cell)[:maxTextValue])
	}
	return cell
}

// fillCSV gives an imported database page its schema, a table view and its
// rows; Notion row pages fill the rows' content.
func (im *importer) fillCSV(ctx context.Context, n *importNode) error {
	records, err := readCSV(n.data, n.format == "tsv")
	if err != nil {
		return err
	}
	d, err := buildCSVDatabase(records)
	if err != nil {
		return err
	}
	rowIDs := make([]uuid.UUID, len(d.titles))
	err = db.WithTx(ctx, im.s.d.Pool, func(tx pgx.Tx) error {
		raw, _ := json.Marshal(d.schema)
		if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2, updated_at = now() WHERE id = $1`, n.page.ID, raw); err != nil {
			return err
		}
		keys := keysAfter("", "", len(d.titles))
		for i := range d.titles {
			id, err := insertRowID(ctx, tx, n.page, im.me, d.titles[i], keys[i], d.values[i])
			if err != nil {
				return err
			}
			rowIDs[i] = id
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(n.rowDocs) == 0 {
		return nil
	}
	for i, title := range d.titles {
		doc, ok := n.rowDocs[strings.ToLower(cleanImportTitle(title))]
		if !ok {
			continue
		}
		row, err := loadPage(ctx, im.s.d.Pool, rowIDs[i], false)
		if err != nil {
			return err
		}
		blocks := im.markdownBlocks(&importNode{path: doc.path, title: title, page: nil}, stripRowProps(string(doc.data), d.schema))
		if err := im.writeBlocks(ctx, row, doc.path, blocks); err != nil {
			im.fail(doc.path, importMessage(err))
		}
		im.used[doc.path] = true
	}
	return nil
}

// stripRowProps drops a Notion row page's title line and the "Property:
// value" lines under it (they're the row's values).
func stripRowProps(md string, schema []Property) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && firstTitleRE.MatchString(strings.TrimSpace(lines[i])) {
		i++
	}
	names := map[string]bool{}
	for _, p := range schema {
		names[strings.ToLower(p.Name)] = true
	}
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			i++
			continue
		}
		k, _, ok := strings.Cut(t, ":")
		if !ok || !names[strings.ToLower(strings.TrimSpace(k))] {
			break
		}
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// insertRowID is insertRow returning the new row's id.
func insertRowID(ctx context.Context, tx pgx.Tx, dbp *Page, me uuid.UUID, title, pos string, values map[string]any) (uuid.UUID, error) {
	t, err := cleanTitle(title)
	if err != nil {
		t = string([]rune(strings.ReplaceAll(title, "\n", " "))[:maxTitleChars])
	}
	id := uuid.New()
	b, _ := json.Marshal(values)
	if _, err := tx.Exec(ctx, `
		INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, position, props, created_by, updated_by)
		VALUES ($1, $2, $3, 'page', true, $4, $5, $6, $7, $7)`, id, dbp.OrgID, dbp.ID, t, pos, b, me); err != nil {
		return uuid.Nil, err
	}
	return id, access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: id, OrgID: dbp.OrgID, OwnerID: me, Parent: &access.Ref{Kind: Kind, ID: dbp.ID}})
}
