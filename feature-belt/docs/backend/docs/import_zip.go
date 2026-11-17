package docs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

// notionIDRE matches the id Notion appends to exported file and folder
// names ("Roadmap 1a2b…32 hex").
var notionIDRE = regexp.MustCompile(`\s+[0-9a-f]{32}$`)

// zipIndex is an uploaded zip with limits applied (entries, sizes, paths).
type zipIndex struct {
	files   []*zip.File
	byName  map[string]*zip.File
	fileMax int64
	total   int64 // bytes read so far, against the expansion cap
}

func openZip(data []byte, fileMax int64) (*zipIndex, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, httpx.BadRequest("that .zip file can't be opened")
	}
	if len(zr.File) > maxImportEntries {
		return nil, httpx.BadRequest(fmt.Sprintf("it has %d files; import up to %d at a time", len(zr.File), maxImportEntries))
	}
	z := &zipIndex{byName: map[string]*zip.File{}, fileMax: fileMax}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		clean := path.Clean("/" + name)[1:]
		if clean == "" || strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
			continue
		}
		if strings.HasPrefix(path.Base(clean), "._") || strings.HasPrefix(clean, "__MACOSX/") || path.Base(clean) == ".DS_Store" {
			continue
		}
		if strings.HasSuffix(name, "/") {
			continue
		}
		f.Name = clean
		z.files = append(z.files, f)
		z.byName[clean] = f
	}
	sort.Slice(z.files, func(i, j int) bool { return z.files[i].Name < z.files[j].Name })
	return z, nil
}

func (z *zipIndex) file(name string) *zip.File {
	return z.byName[path.Clean("/" + name)[1:]]
}

// read returns an entry's bytes, refusing ones that expand past the file
// limit or past 8× the per-file limit in total (zip bombs).
func (z *zipIndex) read(f *zip.File) ([]byte, error) {
	limit := max(z.fileMax, 20<<20)
	if f.UncompressedSize64 > uint64(limit) {
		return nil, httpx.BadRequest(fmt.Sprintf("it's over %d MB", limit>>20))
	}
	rc, err := f.Open()
	if err != nil {
		return nil, httpx.BadRequest("it can't be read from the zip")
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, httpx.BadRequest("it can't be read from the zip")
	}
	if int64(len(data)) > limit {
		return nil, httpx.BadRequest(fmt.Sprintf("it's over %d MB", limit>>20))
	}
	z.total += int64(len(data))
	if z.total > 8*max(z.fileMax, 64<<20) {
		return nil, httpx.BadRequest("the zip expands to more than an import can hold")
	}
	return data, nil
}

var docExts = map[string]string{
	".md": "markdown", ".markdown": "markdown", ".mdown": "markdown", ".txt": "text",
	".html": "html", ".htm": "html", ".docx": "docx", ".csv": "csv",
}

func (z *zipIndex) isDoc(name string) bool {
	_, ok := docExts[strings.ToLower(path.Ext(name))]
	return ok
}

// skip: files that are part of an export's machinery, not content
// (Confluence's styles and scripts).
func (z *zipIndex) skip(name string) bool {
	lower := strings.ToLower(name)
	ext := path.Ext(lower)
	return ext == ".css" || ext == ".js" || ext == ".map" || strings.Contains(lower, "/styles/") || strings.HasPrefix(lower, "styles/")
}

// folder is the directory a node's subpages live in: Notion and Markdown
// exports put "Page.md"'s subpages in "Page/".
func (n *importNode) folder() string {
	if n.format == "folder" {
		return n.path
	}
	return strings.TrimSuffix(n.path, path.Ext(n.path))
}

// zipTree builds the page tree: each document is a page; a folder named like
// a document holds its subpages; other folders become pages of their own. A
// single top-level page (after one wrapping folder) is the import's root;
// otherwise a page named after the zip holds them. Confluence space exports
// (index.html listing the page tree) keep their hierarchy.
func (im *importer) zipTree(zipName string) (*importNode, error) {
	z := im.zip
	if root, ok, err := im.confluenceTree(zipName); ok || err != nil {
		return root, err
	}
	nodes := map[string]*importNode{} // by folder path
	docs := map[string]*importNode{}  // by file path
	var csvAll = map[string]bool{}
	for _, f := range z.files {
		if strings.HasSuffix(strings.ToLower(f.Name), "_all.csv") {
			csvAll[strings.TrimSuffix(f.Name, "_all.csv")+".csv"] = true
		}
	}
	var folder func(dir string) *importNode
	folder = func(dir string) *importNode {
		if dir == "." || dir == "" {
			dir = ""
		}
		if n, ok := nodes[dir]; ok {
			return n
		}
		n := &importNode{path: dir, title: cleanImportTitle(path.Base(dir)), format: "folder"}
		nodes[dir] = n
		if dir != "" {
			parent := folder(path.Dir(dir))
			parent.children = append(parent.children, n)
		}
		return n
	}
	for _, f := range z.files {
		format, ok := docExts[strings.ToLower(path.Ext(f.Name))]
		if !ok {
			continue
		}
		name := f.Name
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, "_all.csv") {
			continue
		}
		if format == "csv" && csvAll[name] {
			// Notion: "X_all.csv" has every row of the database; read that one.
			if all := z.file(strings.TrimSuffix(name, ".csv") + "_all.csv"); all != nil {
				f = all
			}
		}
		if path.Base(lower) == "index.html" && format == "html" && len(z.files) > 1 {
			continue // a site's index page lists the others
		}
		data, err := z.read(f)
		if err != nil {
			im.fail(name, importMessage(err))
			continue
		}
		base := strings.TrimSuffix(path.Base(name), path.Ext(name))
		n := &importNode{path: name, title: cleanImportTitle(base), format: format, data: data}
		docs[name] = n
		parent := folder(path.Dir(name))
		parent.children = append(parent.children, n)
	}
	// A folder named like a document (same path without the extension) is
	// that document's subpages; Notion's database folders hold its rows.
	for _, n := range docs {
		dir := strings.TrimSuffix(n.path, path.Ext(n.path))
		sub, ok := nodes[dir]
		if !ok {
			continue
		}
		parent := nodes[path.Dir(n.path)]
		if path.Dir(n.path) == "." {
			parent = nodes[""]
		}
		if parent != nil {
			parent.children = without(parent.children, sub)
		}
		if n.format == "csv" {
			n.rowDocs = map[string]*importNode{}
			collectRowDocs(sub, n.rowDocs)
			continue
		}
		n.children = append(n.children, sub.children...)
		nodes[dir] = n // files in that folder attach to this page
	}
	top := nodes[""]
	if top == nil {
		return nil, httpx.BadRequest("the zip has no Markdown, text, HTML, Word or CSV files")
	}
	// Unwrap a single folder (a zipped folder).
	for len(top.children) == 1 && top.children[0].format == "folder" {
		top = top.children[0]
	}
	if countNodes(top) == 1 && top.format == "folder" {
		return nil, httpx.BadRequest("the zip has no Markdown, text, HTML, Word or CSV files")
	}
	sortNodes(top)
	if len(top.children) == 1 && top.format == "folder" {
		return top.children[0], nil
	}
	if top.path == "" {
		top.title = cleanImportTitle(zipName)
	}
	return top, nil
}

func without(list []*importNode, n *importNode) []*importNode {
	out := list[:0]
	for _, x := range list {
		if x != n {
			out = append(out, x)
		}
	}
	return out
}

func collectRowDocs(n *importNode, into map[string]*importNode) {
	for _, k := range n.children {
		if k.format == "markdown" {
			into[strings.ToLower(k.title)] = k
		}
		collectRowDocs(k, into)
	}
}

// sortNodes orders subpages by title (exports don't keep the order), with
// folders after documents.
func sortNodes(n *importNode) {
	sort.SliceStable(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if (a.format == "folder") != (b.format == "folder") {
			return b.format == "folder"
		}
		return strings.ToLower(a.title) < strings.ToLower(b.title)
	})
	for _, k := range n.children {
		sortNodes(k)
	}
}

// ---- Markdown inside an import ---------------------------------------------------

var (
	mdLinkRE     = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	loneImageRE  = regexp.MustCompile(`^\s*!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)\s*$`)
	loneLinkRE   = regexp.MustCompile(`^\s*\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)\s*$`)
	placeholdRE  = regexp.MustCompile(`^\{\{file:(\d+)\}\}$`)
	firstTitleRE = regexp.MustCompile(`^#\s+(.+?)\s*#*\s*$`)
)

// resolveLocal turns a relative link in a document at docPath into a path in
// the upload ("" when it isn't relative).
func resolveLocal(docPath, target string) string {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "data:") {
		return ""
	}
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if u, err := url.PathUnescape(target); err == nil {
		target = u
	}
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)[1:]
	}
	return path.Clean(path.Join(path.Dir(docPath), target))
}

// markdownBlocks parses an imported Markdown document: a leading "# Title"
// names the page (and isn't repeated), links to other imported documents
// become page mentions, and images and files from the upload on lines of
// their own become image and file blocks.
func (im *importer) markdownBlocks(n *importNode, md string) []*mdBlock {
	md = cleanText(strings.ReplaceAll(md, "\r\n", "\n"))
	lines := strings.Split(md, "\n")
	// The title line.
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if m := firstTitleRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			if n.page != nil && strings.TrimSpace(m[1]) != "" && strings.TrimSpace(m[1]) != n.page.Title {
				im.rename(n, m[1])
			}
			lines = lines[i+1:]
		}
		break
	}
	var refs []*mdBlock
	inFence := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if inFence {
			continue
		}
		if m := loneImageRE.FindStringSubmatch(l); m != nil {
			if p := resolveLocal(n.path, m[2]); p != "" && im.zip != nil && im.zip.file(p) != nil {
				refs = append(refs, fileRefBlock(p, path.Base(p), textSpans(m[1])))
				lines[i] = leadingSpace(l) + fmt.Sprintf("{{file:%d}}", len(refs)-1)
				continue
			}
		}
		if m := loneLinkRE.FindStringSubmatch(l); m != nil {
			if p := resolveLocal(n.path, m[2]); p != "" && im.zip != nil && im.zip.file(p) != nil && !im.zip.isDoc(p) {
				refs = append(refs, fileRefBlock(p, path.Base(p), nil))
				lines[i] = leadingSpace(l) + fmt.Sprintf("{{file:%d}}", len(refs)-1)
				continue
			}
		}
		lines[i] = mdLinkRE.ReplaceAllStringFunc(l, func(link string) string {
			m := mdLinkRE.FindStringSubmatch(link)
			if m[1] == "!" {
				return link
			}
			if p := resolveLocal(n.path, m[3]); p != "" {
				if id, ok := im.byPath[p]; ok {
					return "[" + m[2] + "](/docs/" + id.String() + ")"
				}
				// A link to a folder's page ("Page/") or a page without its extension.
				for _, ext := range []string{".md", ".html", ".htm", ".docx", ".csv", ".txt"} {
					if id, ok := im.byPath[strings.TrimSuffix(p, "/")+ext]; ok {
						return "[" + m[2] + "](/docs/" + id.String() + ")"
					}
				}
				return m[2] // a local link that goes nowhere here
			}
			return link
		})
	}
	blocks := markdownToBlocks(strings.Join(lines, "\n"), 4000)
	return replaceFileRefs(blocks, refs)
}

func leadingSpace(l string) string {
	return l[:len(l)-len(strings.TrimLeft(l, " \t"))]
}

// replaceFileRefs swaps {{file:N}} paragraphs (at any depth) for refs[N].
func replaceFileRefs(list []*mdBlock, refs []*mdBlock) []*mdBlock {
	if len(refs) == 0 {
		return list
	}
	out := list[:0]
	for _, b := range list {
		if b.typ == "paragraph" {
			if spans, ok := b.props["text"].([]Span); ok {
				if m := placeholdRE.FindStringSubmatch(strings.TrimSpace(plainOf(spans))); m != nil {
					var i int
					_, _ = fmt.Sscanf(m[1], "%d", &i)
					if i >= 0 && i < len(refs) {
						out = append(out, refs[i])
					}
					continue
				}
			}
		}
		b.children = replaceFileRefs(b.children, refs)
		out = append(out, b)
	}
	return out
}

// rename sets an imported page's title from its content (the first heading
// of a Markdown file, an HTML <title>).
func (im *importer) rename(n *importNode, title string) {
	t, err := cleanTitle(title)
	if err != nil || t == "" || n.page == nil {
		return
	}
	icon, rest, ok := startsWithEmoji(t)
	var iconPtr *string
	if ok && rest != "" {
		iconPtr, t = &icon, rest
	}
	if _, err := im.s.d.Pool.Exec(im.ctx, `UPDATE docs_pages SET title = $2, icon = COALESCE($3, icon) WHERE id = $1`, n.page.ID, t, iconPtr); err == nil {
		n.page.Title = t
	}
}

// pageLinkFor maps an upload path to a page link target when it was imported.
func (im *importer) pageLinkFor(docPath, target string) (uuid.UUID, bool) {
	p := resolveLocal(docPath, target)
	if p == "" {
		return uuid.Nil, false
	}
	id, ok := im.byPath[p]
	return id, ok
}
