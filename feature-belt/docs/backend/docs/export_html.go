package docs

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// HTML export: semantic HTML with a small stylesheet that uses only system
// colours (Canvas, CanvasText and mixes of them), so it follows the
// reader's light or dark setting and prints cleanly; no scripts, nothing
// fetched. A single page embeds its images; the zip keeps files beside the
// pages and links pages to each other.

type htmlOpts struct {
	inline bool                   // embed images as data: URIs
	byID   map[string]*exportNode // pages in the export (zip)
}

const exportCSS = `:root{color-scheme:light dark;--fg:CanvasText;--bg:Canvas;--muted:color-mix(in srgb,CanvasText 62%,Canvas);--line:color-mix(in srgb,CanvasText 14%,Canvas);--soft:color-mix(in srgb,CanvasText 5%,Canvas)}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.6 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:46rem;margin:0 auto;padding:3rem 1.5rem 5rem}main.wide{max-width:72rem}body.serif{font-family:Georgia,"Times New Roman",serif}body.mono{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}body.small{font-size:14px}
h1.title{font-size:2.25rem;line-height:1.2;margin:.5rem 0 1.5rem}.icon{font-size:3rem;line-height:1}h1{font-size:1.875rem}h2{font-size:1.5rem}h3{font-size:1.25rem}h4{font-size:1.1rem}h1,h2,h3,h4{line-height:1.3;margin:1.6em 0 .4em}
p{margin:.35em 0}a{color:inherit;text-decoration-color:var(--muted);text-underline-offset:3px}ul,ol{margin:.25em 0;padding-left:1.6em}li{margin:.15em 0}
blockquote{margin:.6em 0;padding:.1em 1em;border-left:3px solid var(--fg)}.callout{display:flex;gap:.75em;margin:.6em 0;padding:1em;border-radius:6px;background:var(--soft)}.callout>.ci{flex:none}
pre{margin:.6em 0;padding:1em;border-radius:6px;background:var(--soft);overflow:auto;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.88em;padding:.1em .3em;border-radius:4px;background:var(--soft)}pre code{padding:0;background:none}
hr{border:0;border-top:1px solid var(--line);margin:1.5em 0}table{border-collapse:collapse;margin:.8em 0;width:100%}td,th{border:1px solid var(--line);padding:.4em .6em;text-align:left;vertical-align:top}th{background:var(--soft)}
figure{margin:1em 0}figure img{max-width:100%;border-radius:4px}figcaption{color:var(--muted);font-size:.875em}.todo{list-style:none;padding-left:.3em}.todo input{margin-right:.5em}.done{color:var(--muted);text-decoration:line-through}
details{margin:.3em 0}summary{cursor:pointer}.file,.bookmark,.page-link{display:block;margin:.4em 0;padding:.6em .8em;border:1px solid var(--line);border-radius:6px;text-decoration:none}.muted{color:var(--muted)}.mention{font-weight:500}
.columns{display:flex;gap:2rem}.columns>div{flex:1;min-width:0}.cover{height:12rem;background:var(--soft);margin:0}.cover img{width:100%;height:100%;object-fit:cover}.props{margin:0 0 1.5rem;color:var(--muted)}.props dt{float:left;width:9rem;clear:left}.props dd{margin:0 0 .3rem 9.5rem;color:var(--fg)}
nav.subpages{margin-top:2.5rem;border-top:1px solid var(--line);padding-top:1rem}@media print{main{padding:0}a{text-decoration:none}}`

func esc(s string) string { return html.EscapeString(s) }

// htmlDocument renders a page (and, inline, its subpages after it) as one
// HTML document.
func (ex *exporter) htmlDocument(n *exportNode, o htmlOpts) string {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	b.WriteString("<title>" + esc(displayTitle(n.page.Title)) + "</title><style>" + exportCSS + "</style></head>")
	classes := []string{}
	if n.page.Font == "serif" || n.page.Font == "mono" {
		classes = append(classes, n.page.Font)
	}
	if n.page.SmallText {
		classes = append(classes, "small")
	}
	b.WriteString("<body class=\"" + strings.Join(classes, " ") + "\">")
	ex.htmlPage(&b, n, o, true)
	if o.byID == nil && ex.subpages {
		var all func(k *exportNode)
		all = func(k *exportNode) {
			for _, c := range append(append([]*exportNode{}, k.children...), k.rowPages...) {
				b.WriteString("<hr>")
				ex.htmlPage(&b, c, o, false)
				all(c)
			}
		}
		all(n)
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

func (ex *exporter) htmlPage(b *strings.Builder, n *exportNode, o htmlOpts, top bool) {
	if top && n.page.Cover != nil && n.page.Cover.FileID != nil {
		if src := ex.fileSrc(n, n.page.Cover.FileID.String(), o); src != "" {
			b.WriteString(`<figure class="cover"><img alt="" src="` + esc(src) + `" style="object-position:center ` + strconv.Itoa(n.page.Cover.Position) + `%"></figure>`)
		}
	}
	cls := ""
	if n.page.FullWidth || n.page.Kind == "database" {
		cls = ` class="wide"`
	}
	b.WriteString("<main" + cls + " id=\"page-" + n.page.ID.String() + "\">")
	if n.page.Icon != nil {
		b.WriteString(`<div class="icon">` + esc(*n.page.Icon) + `</div>`)
	}
	b.WriteString(`<h1 class="title">` + esc(displayTitle(n.page.Title)) + `</h1>`)
	if n.page.IsRow && n.detail.Database != nil {
		b.WriteString(`<dl class="props">`)
		for _, p := range n.detail.Database.Schema {
			if p.Type == "title" {
				continue
			}
			if v := formatValue(p, n.page.Props[p.ID], n.detail.Refs); v != "" {
				b.WriteString("<dt>" + esc(p.Name) + "</dt><dd>" + esc(v) + "</dd>")
			}
		}
		b.WriteString("</dl>")
	}
	r := htmlRenderer{ex: ex, n: n, o: o, refs: n.detail.Refs}
	r.blocks(b, n.detail.Blocks)
	if n.page.Kind == "database" {
		r.rowsTable(b)
	}
	if o.byID != nil && len(n.children) > 0 {
		b.WriteString(`<nav class="subpages"><p class="muted">Pages inside</p>`)
		for _, k := range n.children {
			b.WriteString(`<a class="page-link" href="` + esc(relPath(n.dir, path.Join(k.dir, k.base+".html"))) + `">` + esc(displayTitle(k.page.Title)) + `</a>`)
		}
		b.WriteString("</nav>")
	}
	b.WriteString("</main>")
}

// fileSrc is where an exported page finds a file: embedded, in the zip, or
// (left out) the app.
func (ex *exporter) fileSrc(n *exportNode, id string, o htmlOpts) string {
	f := n.file(id)
	if f == nil || f.data == nil {
		if web := ex.s.webURL(); web != "" {
			return strings.TrimRight(web, "/") + "/docs/files/" + id
		}
		return ""
	}
	if o.inline || f.path == "" {
		if !strings.HasPrefix(f.contentType, "image/") || f.contentType == "image/svg+xml" {
			return ""
		}
		return "data:" + f.contentType + ";base64," + base64.StdEncoding.EncodeToString(f.data)
	}
	return relPath(n.dir, f.path)
}

type htmlRenderer struct {
	ex   *exporter
	n    *exportNode
	o    htmlOpts
	refs Refs
}

func (r *htmlRenderer) pageHref(id string) string {
	if r.o.byID != nil {
		if t, ok := r.o.byID[id]; ok {
			return relPath(r.n.dir, path.Join(t.dir, t.base+".html"))
		}
	}
	if !r.o.inline || r.o.byID != nil {
		if web := r.ex.s.webURL(); web != "" {
			return strings.TrimRight(web, "/") + "/docs/" + id
		}
	}
	if r.ex.subpages {
		return "#page-" + id
	}
	if web := r.ex.s.webURL(); web != "" {
		return strings.TrimRight(web, "/") + "/docs/" + id
	}
	return ""
}

func (r *htmlRenderer) spans(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		t := esc(s.Text)
		t = strings.ReplaceAll(t, "\n", "<br>")
		if s.Mention != nil {
			switch s.Mention.Type {
			case "user":
				name := s.Text
				if u, ok := r.refs.Users[s.Mention.ID]; ok {
					name = "@" + u.Name
				}
				b.WriteString(`<span class="mention">` + esc(name) + `</span>`)
			case "page":
				title := s.Text
				if p, ok := r.refs.Pages[s.Mention.ID]; ok && p.Title != nil {
					title = *p.Title
				}
				if href := r.pageHref(s.Mention.ID); href != "" {
					b.WriteString(`<a class="mention" href="` + esc(href) + `">` + esc(title) + `</a>`)
				} else {
					b.WriteString(`<span class="mention">` + esc(title) + `</span>`)
				}
			case "date":
				b.WriteString(`<time datetime="` + esc(s.Mention.Date) + `">` + esc(s.Text) + `</time>`)
			default:
				b.WriteString(t)
			}
			continue
		}
		if s.Code || s.Equation {
			t = "<code>" + t + "</code>"
		}
		if s.Bold {
			t = "<strong>" + t + "</strong>"
		}
		if s.Italic {
			t = "<em>" + t + "</em>"
		}
		if s.Underline {
			t = "<u>" + t + "</u>"
		}
		if s.Strike {
			t = "<s>" + t + "</s>"
		}
		if l := safeLink(s.Link); l != "" {
			t = `<a href="` + esc(l) + `">` + t + `</a>`
		}
		b.WriteString(t)
	}
	return b.String()
}

func (r *htmlRenderer) blocks(b *strings.Builder, blocks []Block) {
	kids := map[uuid.UUID][]Block{}
	ids := map[uuid.UUID]bool{}
	for _, bl := range blocks {
		ids[bl.ID] = true
	}
	var roots []Block
	for _, bl := range blocks {
		if bl.ParentID != nil && ids[*bl.ParentID] {
			kids[*bl.ParentID] = append(kids[*bl.ParentID], bl)
		} else {
			roots = append(roots, bl)
		}
	}
	var headings []Block
	for _, bl := range blocks {
		if strings.HasPrefix(bl.Type, "heading_") {
			headings = append(headings, bl)
		}
	}
	var list func(items []Block, depth int)
	list = func(items []Block, depth int) {
		for i := 0; i < len(items); i++ {
			bl := items[i]
			// Runs of list items share one list element.
			if bl.Type == "bulleted_list" || bl.Type == "numbered_list" || bl.Type == "to_do" {
				tag, cls := "ul", ""
				if bl.Type == "numbered_list" {
					tag = "ol"
				}
				if bl.Type == "to_do" {
					cls = ` class="todo"`
				}
				b.WriteString("<" + tag + cls + ">")
				for ; i < len(items) && items[i].Type == bl.Type; i++ {
					it := items[i]
					text := r.spans(textOf(it.Props))
					if it.Type == "to_do" {
						checked := ""
						if propBool(it.Props, "checked") {
							checked = " checked"
							text = `<span class="done">` + text + `</span>`
						}
						text = `<input type="checkbox" disabled` + checked + `>` + text
					}
					b.WriteString("<li>" + text)
					if depth < 32 {
						list(kids[it.ID], depth+1)
					}
					b.WriteString("</li>")
				}
				i--
				b.WriteString("</" + tag + ">")
				continue
			}
			r.block(b, bl, kids, headings, func(children []Block) {
				if depth < 32 {
					list(children, depth+1)
				}
			})
		}
	}
	list(roots, 0)
}

func (r *htmlRenderer) block(b *strings.Builder, bl Block, kids map[uuid.UUID][]Block, headings []Block, children func([]Block)) {
	text := r.spans(textOf(bl.Props))
	anchor := ` id="b-` + bl.ID.String()[:8] + `"`
	switch bl.Type {
	case "heading_1", "heading_2", "heading_3", "heading_4":
		lvl := bl.Type[len(bl.Type)-1:]
		if propBool(bl.Props, "toggleable") {
			b.WriteString(`<details open><summary><h` + lvl + anchor + ` style="display:inline">` + text + `</h` + lvl + `></summary>`)
			children(kids[bl.ID])
			b.WriteString("</details>")
			return
		}
		b.WriteString("<h" + lvl + anchor + ">" + text + "</h" + lvl + ">")
	case "paragraph":
		if text == "" {
			b.WriteString("<p>&nbsp;</p>")
		} else {
			b.WriteString("<p>" + text + "</p>")
		}
	case "toggle":
		b.WriteString("<details><summary>" + text + "</summary>")
		children(kids[bl.ID])
		b.WriteString("</details>")
		return
	case "quote":
		b.WriteString("<blockquote><p>" + text + "</p>")
		children(kids[bl.ID])
		b.WriteString("</blockquote>")
		return
	case "callout":
		b.WriteString(`<div class="callout"><span class="ci">` + esc(propString(bl.Props, "icon")) + `</span><div><p>` + text + `</p>`)
		children(kids[bl.ID])
		b.WriteString("</div></div>")
		return
	case "code":
		b.WriteString(`<pre><code data-language="` + esc(propString(bl.Props, "language")) + `">` + esc(propString(bl.Props, "code")) + "</code></pre>")
	case "equation":
		b.WriteString(`<pre><code data-language="tex">` + esc(propString(bl.Props, "expression")) + "</code></pre>")
	case "divider":
		b.WriteString("<hr>")
	case "image":
		src := r.ex.fileSrc(r.n, propString(bl.Props, "file_id"), r.o)
		if src == "" {
			src = safeLink(propString(bl.Props, "url"))
		}
		caption := r.spans(spansOf(bl.Props, "caption"))
		if src != "" {
			b.WriteString(`<figure><img alt="` + esc(plainOf(spansOf(bl.Props, "caption"))) + `" src="` + esc(src) + `">`)
			if caption != "" {
				b.WriteString("<figcaption>" + caption + "</figcaption>")
			}
			b.WriteString("</figure>")
		}
	case "file", "video", "audio", "pdf":
		name := propString(bl.Props, "name")
		if name == "" {
			name = "File"
		}
		href := ""
		if f := r.n.file(propString(bl.Props, "file_id")); f != nil && f.path != "" && !r.o.inline {
			href = relPath(r.n.dir, f.path)
		} else if web := r.ex.s.webURL(); web != "" && propString(bl.Props, "file_id") != "" {
			href = strings.TrimRight(web, "/") + "/docs/files/" + propString(bl.Props, "file_id")
		} else {
			href = safeLink(propString(bl.Props, "url"))
		}
		if href != "" {
			b.WriteString(`<a class="file" href="` + esc(href) + `">📎 ` + esc(name) + `</a>`)
		} else {
			b.WriteString(`<p class="file">📎 ` + esc(name) + `</p>`)
		}
	case "bookmark", "embed":
		u := safeLink(propString(bl.Props, "url"))
		title := propString(bl.Props, "title")
		if title == "" {
			title = u
		}
		if u != "" {
			b.WriteString(`<a class="bookmark" href="` + esc(u) + `">` + esc(title) + `<br><span class="muted">` + esc(u) + `</span></a>`)
		}
	case "page", "database", "link_to_page":
		key := "page_id"
		if bl.Type == "database" {
			key = "database_id"
		}
		id := propString(bl.Props, key)
		title := "Untitled"
		if p, ok := r.refs.Pages[id]; ok && p.Title != nil {
			title = *p.Title
		} else if ok {
			title = "No access"
		}
		if href := r.pageHref(id); href != "" {
			b.WriteString(`<a class="page-link" href="` + esc(href) + `">📄 ` + esc(title) + `</a>`)
		} else {
			b.WriteString(`<p class="page-link">📄 ` + esc(title) + `</p>`)
		}
	case "connected_page":
		u := safeLink(propString(bl.Props, "url"))
		title := propString(bl.Props, "title")
		if title == "" {
			title = "Untitled"
		}
		b.WriteString(`<a class="page-link" href="` + esc(u) + `">🔗 ` + esc(title) + `</a>`)
	case "table":
		raw, _ := json.Marshal(bl.Props["rows"])
		var rows [][][]Span
		_ = json.Unmarshal(raw, &rows)
		header := propBool(bl.Props, "header_row")
		b.WriteString("<table>")
		for i, row := range rows {
			b.WriteString("<tr>")
			tag := "td"
			if i == 0 && header {
				tag = "th"
			}
			for _, cell := range row {
				b.WriteString("<" + tag + ">" + r.spans(cell) + "</" + tag + ">")
			}
			b.WriteString("</tr>")
		}
		b.WriteString("</table>")
	case "table_of_contents":
		if len(headings) == 0 {
			return
		}
		b.WriteString(`<nav class="toc"><ul>`)
		for _, h := range headings {
			b.WriteString(`<li><a href="#b-` + h.ID.String()[:8] + `">` + esc(plainOf(textOf(h.Props))) + `</a></li>`)
		}
		b.WriteString("</ul></nav>")
	case "column_list":
		b.WriteString(`<div class="columns">`)
		for _, col := range kids[bl.ID] {
			b.WriteString("<div>")
			children(kids[col.ID])
			b.WriteString("</div>")
		}
		b.WriteString("</div>")
		return
	case "column", "synced_block":
		children(kids[bl.ID])
		return
	case "breadcrumb", "button":
		return
	default:
		if text != "" {
			b.WriteString("<p>" + text + "</p>")
		}
	}
	children(kids[bl.ID])
}

func (r *htmlRenderer) rowsTable(b *strings.Builder) {
	schema := r.n.page.Schema
	b.WriteString("<table><tr>")
	for _, p := range schema {
		b.WriteString("<th>" + esc(p.Name) + "</th>")
	}
	b.WriteString("</tr>")
	for _, row := range r.n.rows {
		b.WriteString("<tr>")
		for _, p := range schema {
			v := ""
			if p.Type == "title" {
				v = esc(displayTitle(row.Title))
				if href := r.pageHref(row.ID.String()); href != "" && r.o.byID != nil {
					if _, ok := r.o.byID[row.ID.String()]; ok {
						v = `<a href="` + esc(href) + `">` + v + `</a>`
					}
				}
			} else {
				v = esc(formatValue(p, row.Props[p.ID], r.n.rowRefs))
			}
			b.WriteString("<td>" + v + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
}

// htmlZip writes each page as an HTML file (subpages in a folder named
// after the page, files beside them) and an index.html opening the first.
func (ex *exporter) htmlZip(ctx context.Context, root *exportNode) ([]byte, error) {
	assignPaths(root, "", map[string]bool{})
	ex.loadFiles(ctx, root, true, maxExportBytes)
	byID := map[string]*exportNode{}
	indexNodes(root, byID)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var write func(n *exportNode) error
	write = func(n *exportNode) error {
		filePaths(n)
		doc := ex.htmlDocument(n, htmlOpts{byID: byID})
		if err := zipPut(zw, path.Join(n.dir, n.base+".html"), []byte(doc)); err != nil {
			return err
		}
		for _, f := range n.files {
			if f.data != nil {
				if err := zipPut(zw, f.path, f.data); err != nil {
					return err
				}
			}
		}
		if n.page.Kind == "database" {
			if err := zipPut(zw, path.Join(n.dir, n.base+".csv"), rowsCSV(n.page.Schema, n.rows, n.rowRefs)); err != nil {
				return err
			}
		}
		for _, k := range append(append([]*exportNode{}, n.children...), n.rowPages...) {
			if err := write(k); err != nil {
				return err
			}
		}
		return nil
	}
	if err := write(root); err != nil {
		return nil, err
	}
	index := `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0; url=` + esc(relPath("", root.base+".html")) + `"><title>` + esc(displayTitle(root.page.Title)) + `</title><a href="` + esc(relPath("", root.base+".html")) + `">` + esc(displayTitle(root.page.Title)) + `</a>`
	if strings.ToLower(root.base) != "index" {
		_ = zipPut(zw, "index.html", []byte(index))
	}
	if len(ex.notes) > 0 {
		_ = zipPut(zw, "Export notes.txt", []byte(strings.Join(ex.notes, "\n")+"\n"))
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
