package docs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/storage"
)

// Imports turn an uploaded file into pages in the background: Markdown,
// plain text, HTML (Confluence exports too), Word .docx, CSV (a database)
// or a zip of them (a Markdown or Notion export: folders become the page
// tree, images and files are uploaded, links between pages are kept). The
// person polls the run for progress and its error report.

const (
	// ImportPurpose is the private upload purpose of files to import.
	ImportPurpose = "docs_import"

	maxImportPages   = 1000
	maxImportEntries = 5000
	maxImportErrors  = 200
	importTimeout    = 20 * time.Minute
)

// importTypes are what the import upload accepts. Markdown and HTML go up
// as text/plain (storage knows no .md or .html); the name says which.
var importTypes = []string{
	"text/plain", "text/csv", "application/zip",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// ImportRun is an import as its owner sees it.
type ImportRun struct {
	ID         uuid.UUID     `json:"id"`
	OrgID      uuid.UUID     `json:"org_id"`
	ParentID   *uuid.UUID    `json:"parent_id"`
	Name       string        `json:"name"`
	Format     string        `json:"format"`
	Status     string        `json:"status"` // running | done | failed
	PagesTotal int           `json:"pages_total"`
	PagesDone  int           `json:"pages_done"`
	FilesDone  int           `json:"files_done"`
	Errors     []ImportError `json:"errors"`
	Message    *string       `json:"message"`
	RootPageID *uuid.UUID    `json:"root_page_id"`
	CreatedAt  time.Time     `json:"created_at"`
	FinishedAt *time.Time    `json:"finished_at"`
}

// ImportError is one line of a run's report: what couldn't be imported.
type ImportError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

const importCols = `id, org_id, parent_id, name, format, status, pages_total, pages_done, files_done, errors, message, root_page_id, created_at, finished_at`

func scanImport(row pgx.Row) (*ImportRun, error) {
	var r ImportRun
	var errs []byte
	if err := row.Scan(&r.ID, &r.OrgID, &r.ParentID, &r.Name, &r.Format, &r.Status, &r.PagesTotal, &r.PagesDone, &r.FilesDone, &errs,
		&r.Message, &r.RootPageID, &r.CreatedAt, &r.FinishedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(errs, &r.Errors)
	if r.Errors == nil {
		r.Errors = []ImportError{}
	}
	return &r, nil
}

// importFormat picks the format from the file name and its first bytes.
func importFormat(name string, head []byte) (string, error) {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
	isZip := bytes.HasPrefix(head, []byte("PK\x03\x04"))
	switch ext {
	case "md", "markdown", "mdown":
		return "markdown", nil
	case "txt", "text":
		return "text", nil
	case "html", "htm", "xhtml":
		return "html", nil
	case "csv", "tsv":
		return "csv", nil
	case "docx":
		if !isZip {
			return "", httpx.BadRequest("That .docx file is damaged. Save it again from Word and retry.")
		}
		return "docx", nil
	case "zip":
		if !isZip {
			return "", httpx.BadRequest("That .zip file is damaged.")
		}
		return "zip", nil
	case "doc":
		return "", httpx.BadRequest("Old Word (.doc) files can't be read. Save it as .docx and import that.")
	}
	return "", httpx.BadRequest("Import Markdown, text, HTML, Word (.docx), CSV, or a .zip of them (a Notion or Confluence export works).")
}

type startImportReq struct {
	Key      string     `json:"key"`
	Name     string     `json:"name"`
	ParentID *uuid.UUID `json:"parent_id"`
	Shared   bool       `json:"shared"`
}

// startImport: POST /docs/orgs/{orgID}/imports (member, 30/h) {key, name,
// parent_id?, shared?}: import an upload (purpose docs_import) into a new
// page under parent_id, or at the top level. Answers 202 with the run.
func (s *Service) startImport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req startImportReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !storage.KeyOwnedBy(req.Key, ImportPurpose, me) {
		return httpx.BadRequest("Upload the file first.")
	}
	name := strings.TrimSpace(cleanText(path.Base(strings.ReplaceAll(req.Name, `\`, "/"))))
	if name == "" || name == "." {
		return httpx.BadRequest("name is the file's name, with its extension.")
	}
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	if s.d.Storage == nil {
		return errStorageUnavailable
	}
	info, err := s.d.Storage.Stat(ctx, req.Key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return httpx.BadRequest("Upload the file first.")
	}
	if err != nil {
		return err
	}
	if info.Size > s.cfg.importMax {
		return httpx.BadRequest(fmt.Sprintf("Imports can be up to %d MB.", s.cfg.importMax>>20))
	}
	head, err := s.d.Storage.ReadPrefix(ctx, req.Key, 8)
	if err != nil {
		return err
	}
	format, err := importFormat(name, head)
	if err != nil {
		return err
	}
	if req.ParentID != nil {
		parent, err := requirePage(ctx, s.d.Pool, me, *req.ParentID, access.Edit)
		if err != nil {
			return err
		}
		if parent.OrgID != m.OrgID {
			return errPageNotFound
		}
		if err := editable(parent, false); err != nil {
			return err
		}
		if parent.Kind == "database" {
			return httpx.BadRequest("Import into a page, not a database.")
		}
	} else if req.Shared {
		ok, err := access.Allowed(ctx, s.d.Pool, m.OrgID, me, PermCreateShared)
		if err != nil {
			return err
		}
		if !ok {
			return permissionRequired(PermCreateShared)
		}
	}
	var running int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_imports WHERE user_id = $1 AND status = 'running'`, me).Scan(&running); err != nil {
		return err
	}
	if running >= 3 {
		return httpx.Conflict("Three imports are running already. Wait for one to finish.")
	}
	run, err := scanImport(s.d.Pool.QueryRow(ctx, `
		INSERT INTO docs_imports (org_id, user_id, parent_id, shared, key, name, format) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+importCols, m.OrgID, me, req.ParentID, req.Shared && req.ParentID == nil, req.Key, name, format))
	if err != nil {
		return err
	}
	runID := run.ID
	bg.Go(ctx, "docs.import", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, importTimeout)
		defer cancel()
		s.runImport(ctx, runID)
		return nil
	})
	httpx.JSON(w, http.StatusAccepted, run)
	return nil
}

// getImport: GET /docs/imports/{importID} (the person who started it).
func (s *Service) getImport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "importID")
	if err != nil {
		return err
	}
	run, err := scanImport(s.d.Pool.QueryRow(ctx, `SELECT `+importCols+` FROM docs_imports WHERE id = $1 AND user_id = $2`, id, me))
	if db.IsNoRows(err) {
		return httpx.NotFound("Import not found.")
	}
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, run)
	return nil
}

// listImports: GET /docs/orgs/{orgID}/imports (member): the caller's last 20.
func (s *Service) listImports(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	rows, err := s.d.Pool.Query(ctx, `SELECT `+importCols+` FROM docs_imports WHERE user_id = $1 AND org_id = $2 ORDER BY created_at DESC, id DESC LIMIT 20`, me, m.OrgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []ImportRun{}
	for rows.Next() {
		run, err := scanImport(rows)
		if err != nil {
			return err
		}
		out = append(out, *run)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// ---- the run ---------------------------------------------------------------------

// importNode is a page to create: a document (blocks parsed after every
// page exists, so links between them resolve), a CSV database, or a folder.
type importNode struct {
	path     string // in the upload, for the report and links; "" for a single file
	title    string
	icon     *string
	format   string // markdown | text | html | docx | csv | folder
	data     []byte
	children []*importNode
	rowDocs  map[string]*importNode // csv: row pages by title (Notion's export)
	id       uuid.UUID
	page     *Page
}

// importer carries one run's state.
type importer struct {
	ctx      context.Context
	s        *Service
	runID    uuid.UUID
	me       uuid.UUID
	orgID    uuid.UUID
	zip      *zipIndex             // the upload when it is a zip
	blobs    map[string]importBlob // embedded media (Word images, data: URIs) by ref
	byPath   map[string]uuid.UUID  // page ids by upload path, for links
	used     map[string]bool       // zip files a page refers to
	errs     []ImportError
	pages    int
	files    int
	lastSave time.Time
}

type importBlob struct {
	name string
	data []byte
}

func (im *importer) fail(p, msg string) {
	if len(im.errs) < maxImportErrors {
		im.errs = append(im.errs, ImportError{Path: p, Message: msg})
	}
}

// progress saves counts and the report (at most every second unless final).
func (im *importer) progress(ctx context.Context, final bool) {
	if !final && time.Since(im.lastSave) < time.Second {
		return
	}
	im.lastSave = time.Now()
	raw, _ := json.Marshal(im.errs)
	if _, err := im.s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE docs_imports SET pages_done = $2, files_done = $3, errors = $4, heartbeat_at = now() WHERE id = $1`,
		im.runID, im.pages, im.files, raw); err != nil {
		slogWarn(ctx, "docs: import progress", err)
	}
}

func (s *Service) runImport(ctx context.Context, runID uuid.UUID) {
	var orgID, me uuid.UUID
	var parent *uuid.UUID
	var shared bool
	var key, name, format string
	if err := s.d.Pool.QueryRow(ctx, `SELECT org_id, user_id, parent_id, shared, COALESCE(key, ''), name, format FROM docs_imports WHERE id = $1`, runID).
		Scan(&orgID, &me, &parent, &shared, &key, &name, &format); err != nil {
		slogWarn(ctx, "docs: import load", err)
		return
	}
	im := &importer{ctx: ctx, s: s, runID: runID, me: me, orgID: orgID, blobs: map[string]importBlob{}, byPath: map[string]uuid.UUID{}, used: map[string]bool{}}
	finish := func(root *uuid.UUID, failure error) {
		status, msg := "done", (*string)(nil)
		if failure != nil {
			status = "failed"
			m := "The import stopped: " + importMessage(failure)
			msg = &m
		}
		raw, _ := json.Marshal(im.errs)
		if _, err := s.d.Pool.Exec(context.WithoutCancel(ctx), `
			UPDATE docs_imports SET status = $2, message = $3, root_page_id = $4, pages_done = $5, files_done = $6, errors = $7,
			  finished_at = now(), heartbeat_at = now(), key = NULL WHERE id = $1`,
			runID, status, msg, root, im.pages, im.files, raw); err != nil {
			slogWarn(ctx, "docs: import finish", err)
		}
		if key != "" && s.d.Storage != nil {
			if err := s.d.Storage.Delete(context.WithoutCancel(ctx), key); err != nil {
				slogWarn(ctx, "docs: delete import upload", err)
			}
		}
		s.treeChanged(ctx, orgID)
	}
	data, err := s.readImport(ctx, key)
	if err != nil {
		finish(nil, err)
		return
	}
	root, err := im.build(name, format, data)
	if err != nil {
		finish(nil, err)
		return
	}
	total := countNodes(root)
	if total > maxImportPages {
		finish(nil, httpx.BadRequest(fmt.Sprintf("it has %d pages; import up to %d at a time", total, maxImportPages)))
		return
	}
	_, _ = s.d.Pool.Exec(ctx, `UPDATE docs_imports SET pages_total = $2, heartbeat_at = now() WHERE id = $1`, runID, total)
	if err := im.createPages(ctx, root, parent, shared, 0); err != nil {
		var rootID *uuid.UUID
		if root.id != uuid.Nil {
			rootID = &root.id
		}
		finish(rootID, err)
		return
	}
	err = im.fillPages(ctx, root)
	if err == nil {
		im.attachLeftovers(ctx, root)
	}
	finish(&root.id, err)
}

func importMessage(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		return strings.TrimSuffix(he.Message, ".") + "."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "it took longer than 20 minutes. Split it into smaller imports."
	}
	return "something went wrong on our side. Try again; the pages imported so far are kept."
}

func (s *Service) readImport(ctx context.Context, key string) ([]byte, error) {
	if s.d.Storage == nil {
		return nil, errStorageUnavailable
	}
	rc, err := s.d.Storage.Open(ctx, key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return nil, httpx.BadRequest("the uploaded file is gone")
	}
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, s.cfg.importMax+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.cfg.importMax {
		return nil, httpx.BadRequest(fmt.Sprintf("imports can be up to %d MB", s.cfg.importMax>>20))
	}
	return data, nil
}

func countNodes(n *importNode) int {
	c := 1
	for _, k := range n.children {
		c += countNodes(k)
	}
	return c
}

// build turns the upload into the page tree.
func (im *importer) build(name, format string, data []byte) (*importNode, error) {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	if format != "zip" {
		n := &importNode{title: cleanImportTitle(base), format: format, data: data}
		if format == "csv" && strings.HasSuffix(strings.ToLower(name), ".tsv") {
			n.format = "tsv"
		}
		return n, nil
	}
	z, err := openZip(data, im.s.cfg.fileMax)
	if err != nil {
		return nil, err
	}
	im.zip = z
	return im.zipTree(base)
}

// createPages makes every page first (parents before children), so links
// between pages resolve while their content is written.
func (im *importer) createPages(ctx context.Context, n *importNode, parent *uuid.UUID, shared bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	kind := "page"
	if n.format == "csv" || n.format == "tsv" {
		kind = "database"
	}
	title := n.title
	if utf8.RuneCountInString(title) > maxTitleChars {
		title = string([]rune(title)[:maxTitleChars])
	}
	p, err := im.s.create(ctx, im.me, im.orgID, createPageReq{ParentID: parent, Kind: kind, Title: title, Icon: n.icon, Shared: shared && depth == 0})
	if err != nil {
		return err
	}
	n.id, n.page = p.ID, p
	if n.path != "" {
		im.byPath[n.path] = p.ID
	}
	if depth >= maxBlockDepth*2 {
		return nil
	}
	for _, k := range n.children {
		if kind == "database" {
			break // rows come from the CSV
		}
		pid := p.ID
		if err := im.createPages(ctx, k, &pid, false, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// fillPages writes each page's content.
func (im *importer) fillPages(ctx context.Context, n *importNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.page != nil {
		if err := im.fill(ctx, n); err != nil {
			var he *httpx.Error
			if !errors.As(err, &he) {
				return err
			}
			im.fail(n.displayPath(), he.Message)
		}
		im.pages++
		im.progress(ctx, false)
	}
	for _, k := range n.children {
		if k.page == nil {
			continue
		}
		if err := im.fillPages(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (n *importNode) displayPath() string {
	if n.path == "" {
		return n.title
	}
	return n.path
}

func (im *importer) fill(ctx context.Context, n *importNode) error {
	var blocks []*mdBlock
	switch n.format {
	case "folder":
		return nil
	case "csv", "tsv":
		return im.fillCSV(ctx, n)
	case "markdown":
		blocks = im.markdownBlocks(n, string(n.data))
	case "text":
		blocks = textToBlocks(string(n.data))
	case "html":
		blocks = im.htmlBlocks(n, n.data)
	case "docx":
		var err error
		if blocks, err = im.docxBlocks(n, n.data); err != nil {
			return err
		}
	}
	return im.writeBlocks(ctx, n.page, n.displayPath(), blocks)
}

// writeBlocks uploads the files blocks refer to, then applies the blocks.
func (im *importer) writeBlocks(ctx context.Context, p *Page, where string, blocks []*mdBlock) error {
	blocks = im.resolveFiles(ctx, p, where, blocks)
	if n := countBlocks(blocks); n > maxBlocks {
		im.fail(where, fmt.Sprintf("Only the first %d blocks fit on one page.", maxBlocks))
		blocks = truncateBlocks(blocks, maxBlocks)
	}
	ops := blockOps(blocks, nil, "", "")
	for start := 0; start < len(ops); start += maxOpsPerBatch {
		end := min(start+maxOpsPerBatch, len(ops))
		res, err := im.s.apply(ctx, im.me, p, ops[start:end], "import")
		if err != nil {
			return err
		}
		for _, rj := range res.Rejected {
			im.fail(where, "A block was skipped: "+rj.Message)
		}
	}
	return nil
}

func truncateBlocks(list []*mdBlock, limit int) []*mdBlock {
	n := 0
	var cut func([]*mdBlock) []*mdBlock
	cut = func(list []*mdBlock) []*mdBlock {
		var out []*mdBlock
		for _, b := range list {
			if n >= limit {
				break
			}
			n++
			b.children = cut(b.children)
			out = append(out, b)
		}
		return out
	}
	return cut(list)
}

// fileRef marks a block that becomes an image or file once its bytes are
// uploaded: props {"ref": zip path or blob key, "name", "caption"?}.
const fileRefType = "import_file"

func fileRefBlock(ref, name string, caption []Span) *mdBlock {
	props := map[string]any{"ref": ref, "name": name}
	if len(caption) > 0 {
		props["caption"] = caption
	}
	return &mdBlock{typ: fileRefType, props: props}
}

// resolveFiles uploads referenced files to the page and turns their blocks
// into image or file blocks; files that can't go on a page are reported and
// dropped.
func (im *importer) resolveFiles(ctx context.Context, p *Page, where string, list []*mdBlock) []*mdBlock {
	out := list[:0]
	for _, b := range list {
		if b.typ == fileRefType {
			ref, _ := b.props["ref"].(string)
			name, _ := b.props["name"].(string)
			nb, err := im.uploadRef(ctx, p, ref, name)
			if err != nil {
				im.fail(where, name+": "+importMessage(err))
				continue
			}
			if c, ok := b.props["caption"]; ok {
				nb.props["caption"] = c
			}
			out = append(out, nb)
			continue
		}
		b.children = im.resolveFiles(ctx, p, where, b.children)
		out = append(out, b)
	}
	return out
}

// uploadRef stores a zip entry or embedded blob as a file of page p.
func (im *importer) uploadRef(ctx context.Context, p *Page, ref, name string) (*mdBlock, error) {
	var data []byte
	if blob, ok := im.blobs[ref]; ok {
		data = blob.data
		if name == "" {
			name = blob.name
		}
	} else if im.zip != nil {
		f := im.zip.file(ref)
		if f == nil {
			return nil, httpx.BadRequest("it isn't in the upload")
		}
		var err error
		if data, err = im.zip.read(f); err != nil {
			return nil, err
		}
		im.used[ref] = true
		if name == "" {
			name = path.Base(ref)
		}
	} else {
		return nil, httpx.BadRequest("it isn't in the upload")
	}
	ct, err := classifyUpload("", name, data[:min(512, len(data))])
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > im.s.cfg.fileMax {
		return nil, httpx.BadRequest(fmt.Sprintf("it's over %d MB", im.s.cfg.fileMax>>20))
	}
	if im.s.d.Storage == nil {
		return nil, errStorageUnavailable
	}
	key, err := storage.NewKey(FilePurpose, im.me, ct)
	if err != nil {
		return nil, err
	}
	if err := im.s.d.Storage.Put(ctx, key, ct, data); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(cleanText(path.Base(name)))
	if utf8.RuneCountInString(name) > 200 {
		name = string([]rune(name)[:200])
	}
	var fileID uuid.UUID
	if err := im.s.d.Pool.QueryRow(ctx, `
		INSERT INTO docs_files (org_id, page_id, key, name, content_type, size, uploaded_by) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		p.OrgID, p.ID, key, name, ct, len(data), im.me).Scan(&fileID); err != nil {
		return nil, err
	}
	im.files++
	return &mdBlock{typ: fileBlockFor(ct), props: map[string]any{"file_id": fileID.String()}}, nil
}

// fileBlockFor is the block that shows an upload of type ct: image, video,
// audio and PDF blocks where the editor has them, else a file block.
func fileBlockFor(ct string) string {
	for typ, prefix := range fileBlockTypes {
		if prefix != "" && strings.HasPrefix(ct, prefix) && slices.Contains(allTypes, typ) {
			return typ
		}
	}
	return "file"
}

// attachLeftovers adds zip files no page referred to (pictures, PDFs next to
// the pages) to the page of their folder, so nothing is silently lost.
func (im *importer) attachLeftovers(ctx context.Context, root *importNode) {
	if im.zip == nil {
		return
	}
	byDir := map[string][]string{}
	for _, f := range im.zip.files {
		p := f.Name
		if im.used[p] || im.zip.isDoc(p) || strings.HasSuffix(p, "/") || im.zip.skip(p) {
			continue
		}
		byDir[path.Dir(p)] = append(byDir[path.Dir(p)], p)
	}
	if len(byDir) == 0 {
		return
	}
	var walk func(n *importNode)
	pageForDir := map[string]*importNode{}
	walk = func(n *importNode) {
		if n.page != nil && n.page.Kind == "page" {
			pageForDir[n.folder()] = n
		}
		for _, k := range n.children {
			walk(k)
		}
	}
	walk(root)
	for dir, files := range byDir {
		target := root
		for d := dir; ; d = path.Dir(d) {
			if n, ok := pageForDir[d]; ok {
				target = n
				break
			}
			if d == "." || d == "/" || d == "" {
				break
			}
		}
		if target.page == nil || target.page.Kind != "page" {
			continue
		}
		var blocks []*mdBlock
		for _, f := range files {
			blocks = append(blocks, fileRefBlock(f, path.Base(f), nil))
		}
		im.appendBlocks(ctx, target, blocks)
	}
	im.progress(ctx, true)
}

// appendBlocks adds blocks after a page's last top-level block.
func (im *importer) appendBlocks(ctx context.Context, n *importNode, blocks []*mdBlock) {
	blocks = im.resolveFiles(ctx, n.page, n.displayPath(), blocks)
	if len(blocks) == 0 {
		return
	}
	var last string
	if err := im.s.d.Pool.QueryRow(ctx, `SELECT COALESCE(max(position), '') FROM docs_blocks WHERE page_id = $1 AND parent_id IS NULL`, n.page.ID).Scan(&last); err != nil {
		im.fail(n.displayPath(), importMessage(err))
		return
	}
	if _, err := im.s.apply(ctx, im.me, n.page, blockOps(blocks, nil, last, ""), "import"); err != nil {
		im.fail(n.displayPath(), importMessage(err))
	}
}

// textToBlocks: paragraphs separated by blank lines keep their line breaks.
func textToBlocks(text string) []*mdBlock {
	text = cleanText(strings.ReplaceAll(text, "\r\n", "\n"))
	var out []*mdBlock
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.Trim(para, "\n")
		if strings.TrimSpace(para) == "" {
			continue
		}
		for len([]rune(para)) > maxBlockChars {
			r := []rune(para)
			out = append(out, &mdBlock{typ: "paragraph", props: map[string]any{"text": textSpans(string(r[:maxBlockChars]))}})
			para = string(r[maxBlockChars:])
		}
		out = append(out, &mdBlock{typ: "paragraph", props: map[string]any{"text": textSpans(para)}})
		if len(out) >= maxBlocks {
			break
		}
	}
	return out
}

// cleanImportTitle drops Notion's " <32 hex id>" suffix and tidies a file
// or folder name into a title.
func cleanImportTitle(s string) string {
	s = notionIDRE.ReplaceAllString(s, "")
	s = strings.TrimSpace(strings.ReplaceAll(cleanText(s), "\n", " "))
	if s == "" {
		return "Untitled"
	}
	return s
}

// cleanupImports fails runs whose worker stopped (a restart) and deletes
// finished runs after 30 days.
func (s *Service) cleanupImports(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		WITH stale AS (
			SELECT id, key FROM docs_imports WHERE status = 'running' AND heartbeat_at < now() - interval '30 minutes' FOR UPDATE SKIP LOCKED
		)
		UPDATE docs_imports d SET status = 'failed', finished_at = now(), key = NULL,
		  message = 'The import stopped when the server restarted. Import the file again; the pages already made are kept.'
		FROM stale WHERE d.id = stale.id
		RETURNING stale.key`)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[*string])
	if err != nil {
		return err
	}
	for _, k := range keys {
		if k != nil && s.d.Storage != nil {
			_ = s.d.Storage.Delete(context.WithoutCancel(ctx), *k)
		}
	}
	_, err = s.d.Pool.Exec(ctx, `DELETE FROM docs_imports WHERE status <> 'running' AND created_at < now() - interval '30 days'`)
	return err
}
