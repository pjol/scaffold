package docs

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// Exports: a page (optionally with the subpages and database rows the
// exporter can open) as Markdown, a zip of Markdown + CSV + files, a
// self-contained HTML page or zip, or a Word document. PDF is the web app's
// print view (see FEATURE.md). Nothing the exporter can't open is included:
// every page goes through access like the page itself.

const (
	maxExportPages = 500
	maxExportRows  = 10000
	maxExportBytes = 200 << 20 // files gathered into one export
)

var exportFormats = map[string]bool{"markdown": true, "zip": true, "html": true, "html_zip": true, "docx": true}

// exportNode is one page of an export with what it needs to render.
type exportNode struct {
	page     *Page
	detail   *PageDetail
	rows     []Row
	rowRefs  Refs
	rowPages []*exportNode // database rows with content (subpages exports)
	children []*exportNode
	dir      string // folder in the zip
	base     string // file name without extension
	files    []exportFile
}

type exportFile struct {
	id, name, contentType string
	data                  []byte
	path                  string // in the zip, or "" when left out
}

// exporter gathers pages and files within budget.
type exporter struct {
	s        *Service
	me       uuid.UUID
	subpages bool
	pages    int
	bytes    int64
	notes    []string
}

// export: GET /docs/pages/{id}/export/{format}?subpages=true (view, 20/min).
func (s *Service) export(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	format := chi.URLParam(r, "format")
	if !exportFormats[format] {
		return httpx.BadRequest("format must be markdown, zip, html, html_zip or docx.")
	}
	p, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	subpages := r.URL.Query().Get("subpages") == "true" || format == "zip" || format == "html_zip"
	if format == "markdown" || format == "html" || format == "docx" {
		subpages = r.URL.Query().Get("subpages") == "true" && format != "markdown"
	}
	ex := &exporter{s: s, me: me, subpages: subpages}
	root, err := ex.collect(ctx, p, 0)
	if err != nil {
		return err
	}
	name := exportName(displayTitle(p.Title))
	var body []byte
	var ctype, ext string
	switch format {
	case "markdown":
		md, err := s.pageAsMarkdown(ctx, me, p, false)
		if err != nil {
			return err
		}
		body, ctype, ext = []byte(md), "text/markdown; charset=utf-8", ".md"
	case "zip":
		if body, err = ex.markdownZip(ctx, root); err != nil {
			return err
		}
		ctype, ext = "application/zip", ".zip"
	case "html":
		ex.loadFiles(ctx, root, true, 20<<20)
		body = []byte(ex.htmlDocument(root, htmlOpts{inline: true}))
		ctype, ext = "text/html; charset=utf-8", ".html"
	case "html_zip":
		if body, err = ex.htmlZip(ctx, root); err != nil {
			return err
		}
		ctype, ext = "application/zip", ".zip"
	case "docx":
		ex.loadFiles(ctx, root, ex.subpages, 50<<20)
		if body, err = ex.docx(root); err != nil {
			return err
		}
		ctype, ext = "application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx"
	}
	out, err := s.storeExport(ctx, me, name+ext, ctype, body)
	if err != nil {
		return err
	}
	out.Notes = ex.notes
	if out.Notes == nil {
		out.Notes = []string{}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, out)
	return nil
}

// ExportResult is a finished export: a one-hour download link (the file is
// saved under its name) and what was left out.
type ExportResult struct {
	URL       string    `json:"url"`
	Name      string    `json:"name"`
	Size      int       `json:"size"`
	ExpiresAt time.Time `json:"expires_at"`
	Notes     []string  `json:"notes"`
}

// ExportPurpose is the private storage purpose of finished exports. Only
// the server writes them (its upload cap is one byte, so clients can't).
const ExportPurpose = "docs_export"

const exportTTL = time.Hour

// storeExport saves an export to storage and signs a download.
func (s *Service) storeExport(ctx context.Context, me uuid.UUID, filename, ctype string, body []byte) (*ExportResult, error) {
	if s.d.Storage == nil {
		return nil, errStorageUnavailable
	}
	key, err := storage.NewKey(ExportPurpose, me, strings.SplitN(ctype, ";", 2)[0])
	if err != nil {
		return nil, err
	}
	if err := s.d.Storage.Put(ctx, key, ctype, body); err != nil {
		return nil, err
	}
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO docs_exports (user_id, key) VALUES ($1, $2)`, me, key); err != nil {
		return nil, err
	}
	url, err := s.d.Storage.PresignGetWith(ctx, key, exportTTL, storage.GetOptions{
		ContentType: ctype, Disposition: mime.FormatMediaType("attachment", map[string]string{"filename": filename}),
	})
	if err != nil {
		return nil, err
	}
	return &ExportResult{URL: url, Name: filename, Size: len(body), ExpiresAt: time.Now().Add(exportTTL)}, nil
}

// cleanupExports deletes exports older than their link.
func (s *Service) cleanupExports(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `DELETE FROM docs_exports WHERE created_at < now() - interval '70 minutes' RETURNING key`)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, keys)
	return nil
}

var exportNameRE = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

// exportName makes a title safe as a file name (kept readable: spaces and
// letters stay).
func exportName(title string) string {
	n := strings.TrimSpace(exportNameRE.ReplaceAllString(title, "-"))
	n = strings.Trim(n, ". ")
	if utf8.RuneCountInString(n) > 80 {
		n = string([]rune(n)[:80])
	}
	if n == "" {
		n = "Untitled"
	}
	return n
}

// collect loads a page's detail, a database's rows and (with subpages) the
// subpages and row pages the exporter can open.
func (ex *exporter) collect(ctx context.Context, p *Page, depth int) (*exportNode, error) {
	ex.pages++
	if ex.pages > maxExportPages {
		return nil, httpx.NewError(http.StatusConflict, "too_large", fmt.Sprintf("That's more than %d pages to export at once. Export a smaller part.", maxExportPages))
	}
	detail, err := ex.s.pageDetail(ctx, ex.me, p, true)
	if err != nil {
		return nil, err
	}
	n := &exportNode{page: p, detail: detail}
	if p.Kind == "database" {
		cursor := ""
		for len(n.rows) < maxExportRows {
			rp, err := ex.s.queryRows(ctx, rowQuery{db: p, limit: 500, cursor: cursor, me: ex.me, agent: access.ViaAgent(ctx)})
			if err != nil {
				return nil, err
			}
			n.rows = append(n.rows, rp.Items...)
			mergeRefs(&n.rowRefs, rp.Refs)
			if rp.NextCursor == nil {
				break
			}
			cursor = *rp.NextCursor
		}
		if ex.subpages {
			for _, row := range n.rows {
				if ex.pages >= maxExportPages {
					ex.notes = append(ex.notes, "Some rows' pages were left out (too many pages).")
					break
				}
				rp, err := loadPage(ctx, ex.s.d.Pool, row.ID, false)
				if err != nil {
					continue
				}
				rp.MyLevel = row.MyLevel
				var count int
				if err := ex.s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_blocks WHERE page_id = $1`, row.ID).Scan(&count); err != nil || count == 0 {
					continue
				}
				child, err := ex.collect(ctx, rp, depth+1)
				if err != nil {
					return nil, err
				}
				n.rowPages = append(n.rowPages, child)
			}
		}
	}
	if ex.subpages && depth < 32 && p.Kind == "page" {
		kids, err := ex.s.childPages(ctx, ex.s.d.Pool, ex.me, p.ID)
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			kp, err := loadPage(ctx, ex.s.d.Pool, k.ID, false)
			if err != nil {
				continue
			}
			kp.MyLevel = k.MyLevel
			child, err := ex.collect(ctx, kp, depth+1)
			if err != nil {
				return nil, err
			}
			n.children = append(n.children, child)
		}
	}
	return n, nil
}

func mergeRefs(into *Refs, from Refs) {
	if into.Users == nil {
		into.Users = map[string]RefUser{}
	}
	if into.Pages == nil {
		into.Pages = map[string]RefPage{}
	}
	for k, v := range from.Users {
		into.Users[k] = v
	}
	for k, v := range from.Pages {
		into.Pages[k] = v
	}
}

// fileIDsOf lists the uploads a page's blocks and cover show.
func fileIDsOf(n *exportNode) []string {
	var ids []string
	for _, b := range n.detail.Blocks {
		if id := propString(b.Props, "file_id"); id != "" {
			ids = append(ids, id)
		}
	}
	if c := n.page.Cover; c != nil && c.FileID != nil {
		ids = append(ids, c.FileID.String())
	}
	if ci := n.page.CustomIcon; ci != nil && ci.FileID != nil {
		ids = append(ids, ci.FileID.String())
	}
	return ids
}

// loadFiles reads the files of the tree (one page, or all with deep) into
// memory within limit bytes; the rest are linked, and noted.
func (ex *exporter) loadFiles(ctx context.Context, n *exportNode, deep bool, limit int64) {
	ex.loadPageFiles(ctx, n, limit)
	if !deep {
		return
	}
	for _, k := range append(append([]*exportNode{}, n.children...), n.rowPages...) {
		ex.loadFiles(ctx, k, deep, limit)
	}
}

func (ex *exporter) loadPageFiles(ctx context.Context, n *exportNode, limit int64) {
	seen := map[string]bool{}
	for _, id := range fileIDsOf(n) {
		if seen[id] {
			continue
		}
		seen[id] = true
		var name, ct, key string
		var size int64
		if err := ex.s.d.Pool.QueryRow(ctx, `SELECT name, content_type, key, size FROM docs_files WHERE id = $1 AND page_id = $2`, id, n.page.ID).
			Scan(&name, &ct, &key, &size); err != nil {
			continue
		}
		f := exportFile{id: id, name: name, contentType: ct}
		if ex.bytes+size > limit || ex.s.d.Storage == nil {
			ex.notes = append(ex.notes, name+" wasn't included (the export would be too large).")
			n.files = append(n.files, f)
			continue
		}
		rc, err := ex.s.d.Storage.Open(ctx, key)
		if err != nil {
			if !errors.Is(err, storage.ErrObjectNotFound) {
				slogWarn(ctx, "docs: export file", err)
			}
			n.files = append(n.files, f)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, size+1))
		rc.Close()
		if err != nil {
			n.files = append(n.files, f)
			continue
		}
		ex.bytes += int64(len(data))
		f.data = data
		n.files = append(n.files, f)
	}
}

func (n *exportNode) file(id string) *exportFile {
	for i := range n.files {
		if n.files[i].id == id {
			return &n.files[i]
		}
	}
	return nil
}

// ---- zip layout ------------------------------------------------------------------

// assignPaths names every page's file in its folder: "Title.md" with its
// subpages, rows and files in "Title/", names made unique per folder.
func assignPaths(n *exportNode, dir string, used map[string]bool) {
	base := exportName(displayTitle(n.page.Title))
	name := base
	for i := 2; used[path.Join(dir, strings.ToLower(name))]; i++ {
		name = fmt.Sprintf("%s (%d)", base, i)
	}
	used[path.Join(dir, strings.ToLower(name))] = true
	n.dir, n.base = dir, name
	sub := path.Join(dir, name)
	for _, k := range n.children {
		assignPaths(k, sub, used)
	}
	for _, k := range n.rowPages {
		assignPaths(k, sub, used)
	}
}

// relPath is the path from page n's folder to target (both in the zip).
func relPath(fromDir, target string) string {
	from := strings.Split(strings.Trim(fromDir, "/"), "/")
	to := strings.Split(strings.Trim(target, "/"), "/")
	if fromDir == "" {
		from = nil
	}
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	parts := []string{}
	for range from[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, to[i:]...)
	return (&urlPath{strings.Join(parts, "/")}).String()
}

type urlPath struct{ p string }

func (u *urlPath) String() string {
	segs := strings.Split(u.p, "/")
	for i, s := range segs {
		segs[i] = strings.ReplaceAll(strings.ReplaceAll(s, "%", "%25"), " ", "%20")
		segs[i] = strings.NewReplacer("(", "%28", ")", "%29", "#", "%23", "?", "%3F").Replace(segs[i])
	}
	return strings.Join(segs, "/")
}

// indexNodes maps page ids to nodes (for links between exported pages).
func indexNodes(n *exportNode, into map[string]*exportNode) {
	into[n.page.ID.String()] = n
	for _, k := range n.children {
		indexNodes(k, into)
	}
	for _, k := range n.rowPages {
		indexNodes(k, into)
	}
}

// filePaths gives each loaded file a path in its page's "files" folder.
func filePaths(n *exportNode) {
	used := map[string]bool{}
	for i := range n.files {
		f := &n.files[i]
		if f.data == nil {
			continue
		}
		base := exportName(f.name)
		name := base
		for j := 2; used[strings.ToLower(name)]; j++ {
			ext := path.Ext(base)
			name = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(base, ext), j, ext)
		}
		used[strings.ToLower(name)] = true
		f.path = path.Join(n.dir, n.base, "files", name)
	}
}

func (ex *exporter) markdownZip(ctx context.Context, root *exportNode) ([]byte, error) {
	assignPaths(root, "", map[string]bool{})
	ex.loadFiles(ctx, root, true, maxExportBytes)
	byID := map[string]*exportNode{}
	indexNodes(root, byID)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var write func(n *exportNode) error
	write = func(n *exportNode) error {
		filePaths(n)
		md := ex.pageMarkdown(n, byID)
		if err := zipPut(zw, path.Join(n.dir, n.base+".md"), []byte(md)); err != nil {
			return err
		}
		if n.page.Kind == "database" {
			if err := zipPut(zw, path.Join(n.dir, n.base+".csv"), rowsCSV(n.page.Schema, n.rows, n.rowRefs)); err != nil {
				return err
			}
		}
		for _, f := range n.files {
			if f.data != nil {
				if err := zipPut(zw, f.path, f.data); err != nil {
					return err
				}
			}
		}
		for _, k := range n.children {
			if err := write(k); err != nil {
				return err
			}
		}
		for _, k := range n.rowPages {
			if err := write(k); err != nil {
				return err
			}
		}
		return nil
	}
	if err := write(root); err != nil {
		return nil, err
	}
	if len(ex.notes) > 0 {
		_ = zipPut(zw, "Export notes.txt", []byte(strings.Join(ex.notes, "\n")+"\n"))
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func zipPut(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// pageMarkdown renders one page for the zip: links to exported pages and
// files are relative; other pages link to the app.
func (ex *exporter) pageMarkdown(n *exportNode, byID map[string]*exportNode) string {
	var out strings.Builder
	title := displayTitle(n.page.Title)
	if n.page.Icon != nil {
		title = *n.page.Icon + " " + title
	}
	out.WriteString("# " + title + "\n\n")
	if n.page.IsRow && n.detail.Database != nil {
		for _, prop := range n.detail.Database.Schema {
			if prop.Type == "title" {
				continue
			}
			if v := formatValue(prop, n.page.Props[prop.ID], n.detail.Refs); v != "" {
				out.WriteString(prop.Name + ": " + v + "\n")
			}
		}
		out.WriteString("\n")
	}
	web := strings.TrimRight(ex.s.webURL(), "/")
	opt := mdOptions{names: namesFor(n.detail.Refs), pageLink: func(id string) string {
		if t, ok := byID[id]; ok {
			return relPath(n.dir, path.Join(t.dir, t.base+".md"))
		}
		return web + "/docs/" + id
	}}
	if len(n.detail.Blocks) > 0 {
		md := blocksToMarkdown(n.detail.Blocks, opt)
		for _, f := range n.files {
			if f.path != "" {
				md = strings.ReplaceAll(md, "/docs/files/"+f.id, relPath(n.dir, f.path))
			} else {
				md = strings.ReplaceAll(md, "/docs/files/"+f.id, web+"/docs/files/"+f.id)
			}
		}
		md = rewritePageLinks(md, func(id string) string { return opt.pageLink(id) })
		out.WriteString(md)
	}
	if n.page.Kind == "database" {
		out.WriteString("\n" + rowsTable(n.page.Schema, &RowPage{Items: n.rows, Refs: n.rowRefs}))
	}
	if len(n.children) > 0 {
		out.WriteString("\n## Pages inside\n\n")
		for _, k := range n.children {
			out.WriteString("- [" + mdEscaper.Replace(displayTitle(k.page.Title)) + "](" + relPath(n.dir, path.Join(k.dir, k.base+".md")) + ")\n")
		}
	}
	return out.String()
}

var docsLinkRE = regexp.MustCompile(`\]\(/docs/([0-9a-f-]{36})\)`)

// rewritePageLinks points Markdown links to /docs/<id> (page mentions) at
// the exported file or the app.
func rewritePageLinks(md string, link func(id string) string) string {
	return docsLinkRE.ReplaceAllStringFunc(md, func(m string) string {
		id := docsLinkRE.FindStringSubmatch(m)[1]
		return "](" + link(id) + ")"
	})
}

// rowsCSV renders a database's rows as CSV (property names as the header).
func rowsCSV(schema []Property, rows []Row, refs Refs) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf") // Excel reads UTF-8 with a BOM
	w := csv.NewWriter(&buf)
	head := make([]string, len(schema))
	for i, p := range schema {
		head[i] = p.Name
	}
	_ = w.Write(head)
	for _, r := range rows {
		rec := make([]string, len(schema))
		for i, p := range schema {
			if p.Type == "title" {
				rec[i] = displayTitle(r.Title)
			} else {
				rec[i] = formatValue(p, r.Props[p.ID], refs)
			}
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return buf.Bytes()
}

// webURL is the web app's origin for links that leave an export.
func (s *Service) webURL() string {
	if s.d.Cfg != nil && s.d.Cfg.PublicWebURL != "" {
		return s.d.Cfg.PublicWebURL
	}
	return ""
}
