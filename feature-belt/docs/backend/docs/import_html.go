package docs

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTML import: the document is parsed (never stored or rendered) and its
// structure mapped to blocks: headings, paragraphs, lists with check boxes,
// quotes, code, rules, tables, images, toggles (<details>) and inline bold,
// italic, underline, strike, code and links. Scripts, styles, forms and
// navigation are dropped. Confluence exports add callout macros, code
// panels, embedded attachments and their page tree (index.html).

// htmlConv carries one document's conversion.
type htmlConv struct {
	im      *importer
	docPath string
	blobs   int
	title   string
}

var (
	confluenceBrushRE = regexp.MustCompile(`brush:\s*([a-z0-9+#-]+)`)
	langClassRE       = regexp.MustCompile(`(?:^|\s)(?:language|lang)-([a-z0-9+#.-]+)`)
	dataURIRE         = regexp.MustCompile(`^data:(image/(?:png|jpeg|gif|webp));base64,(.+)$`)
)

// htmlBlocks parses an imported HTML document into blocks (renaming the
// page after its <title> or first heading when it came from a file name).
func (im *importer) htmlBlocks(n *importNode, data []byte) []*mdBlock {
	c := &htmlConv{im: im, docPath: n.path}
	blocks, title := c.convert(data)
	if n.page != nil && title != "" && title != n.page.Title {
		im.rename(n, title)
	}
	return blocks
}

// convert returns the blocks and the document's title.
func (c *htmlConv) convert(data []byte) ([]*mdBlock, string) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return textToBlocks(string(data)), ""
	}
	title := ""
	if t := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Title }); t != nil {
		title = strings.TrimSpace(textContent(t))
		// Confluence: "Space name : Page title".
		if _, after, ok := strings.Cut(title, " : "); ok {
			title = strings.TrimSpace(after)
		}
	}
	root := doc
	if m := findFirst(doc, func(n *html.Node) bool { return attr(n, "id") == "main-content" }); m != nil {
		root = m
	} else if b := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body }); b != nil {
		root = b
	}
	if title == "" {
		if h := findFirst(root, func(n *html.Node) bool { return n.DataAtom == atom.H1 }); h != nil {
			title = strings.TrimSpace(textContent(h))
		}
	}
	blocks := c.blocks(root, 0)
	// The title shouldn't open the page twice.
	if len(blocks) > 0 && blocks[0].typ == "heading_1" {
		if spans, ok := blocks[0].props["text"].([]Span); ok && strings.EqualFold(strings.TrimSpace(plainOf(spans)), title) {
			blocks = blocks[1:]
		}
	}
	return blocks, title
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

func findFirst(n *html.Node, fn func(*html.Node) bool) *html.Node {
	if fn(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findFirst(c, fn); f != nil {
			return f
		}
	}
	return nil
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		if n.DataAtom == atom.Br {
			b.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// skippedHTML are elements whose content never becomes blocks.
func skippedHTML(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Head, atom.Nav, atom.Form, atom.Button, atom.Select,
		atom.Textarea, atom.Iframe, atom.Object, atom.Embed, atom.Svg, atom.Math, atom.Canvas, atom.Audio, atom.Video, atom.Title, atom.Meta, atom.Link:
		return true
	}
	switch attr(n, "id") {
	case "breadcrumb-section", "footer", "header", "navigation", "comments-section", "likes-and-labels-container", "page-metadata-banner":
		return true
	}
	return hasClass(n, "page-metadata") || hasClass(n, "pageSection") && strings.Contains(strings.ToLower(textContent(n)), "attachments:")
}

func isBlockElem(n *html.Node) bool {
	switch n.DataAtom {
	case atom.P, atom.Div, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Ul, atom.Ol, atom.Li, atom.Blockquote, atom.Pre,
		atom.Hr, atom.Table, atom.Figure, atom.Details, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer, atom.Aside,
		atom.Dl, atom.Dt, atom.Dd, atom.Body, atom.Html, atom.Center, atom.Address, atom.Fieldset:
		return true
	case atom.Img:
		return true
	}
	return false
}

// blocks converts n's children into blocks, gathering runs of inline
// content into paragraphs.
func (c *htmlConv) blocks(n *html.Node, depth int) []*mdBlock {
	var out []*mdBlock
	var inline []*html.Node
	flush := func() {
		if len(inline) == 0 {
			return
		}
		spans := c.inlineSpans(inline)
		inline = nil
		if strings.TrimSpace(plainOf(spans)) != "" {
			out = append(out, &mdBlock{typ: "paragraph", props: map[string]any{"text": trimSpans(spans)}})
		}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.CommentNode || (ch.Type == html.ElementNode && skippedHTML(ch)) {
			continue
		}
		if ch.Type == html.ElementNode && isBlockElem(ch) {
			flush()
			out = append(out, c.block(ch, depth)...)
			continue
		}
		inline = append(inline, ch)
	}
	flush()
	return out
}

func (c *htmlConv) block(n *html.Node, depth int) []*mdBlock {
	if depth > 40 {
		return nil
	}
	text := func(typ string) []*mdBlock {
		spans := trimSpans(c.inlineSpans(childNodes(n)))
		if strings.TrimSpace(plainOf(spans)) == "" {
			return nil
		}
		return []*mdBlock{{typ: typ, props: map[string]any{"text": spans}}}
	}
	switch n.DataAtom {
	case atom.H1:
		return text("heading_1")
	case atom.H2:
		return text("heading_2")
	case atom.H3:
		return text("heading_3")
	case atom.H4, atom.H5, atom.H6:
		if slices.Contains(allTypes, "heading_4") {
			return text("heading_4")
		}
		return text("heading_3")
	case atom.P, atom.Dt, atom.Dd, atom.Address:
		if hasBlockChild(n) {
			return c.blocks(n, depth+1)
		}
		return text("paragraph")
	case atom.Hr:
		return []*mdBlock{{typ: "divider", props: map[string]any{}}}
	case atom.Pre:
		return []*mdBlock{c.codeBlock(n)}
	case atom.Blockquote:
		inner := c.blocks(n, depth+1)
		return []*mdBlock{asContainer("quote", inner)}
	case atom.Ul, atom.Ol:
		return c.list(n, depth)
	case atom.Li:
		return c.list(&html.Node{Type: html.ElementNode, DataAtom: atom.Ul, FirstChild: n}, depth)
	case atom.Table:
		return c.table(n)
	case atom.Img:
		if b := c.image(n, ""); b != nil {
			return []*mdBlock{b}
		}
		return nil
	case atom.Figure:
		img := findFirst(n, func(x *html.Node) bool { return x.DataAtom == atom.Img })
		caption := ""
		if fc := findFirst(n, func(x *html.Node) bool { return x.DataAtom == atom.Figcaption }); fc != nil {
			caption = strings.TrimSpace(textContent(fc))
		}
		if img != nil {
			if b := c.image(img, caption); b != nil {
				return []*mdBlock{b}
			}
		}
		return c.blocks(n, depth+1)
	case atom.Details:
		summary := ""
		var body []*mdBlock
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.DataAtom == atom.Summary {
				summary = strings.TrimSpace(textContent(ch))
				continue
			}
			if ch.Type == html.ElementNode && isBlockElem(ch) {
				body = append(body, c.block(ch, depth+1)...)
			} else if ch.Type == html.TextNode && strings.TrimSpace(ch.Data) != "" {
				body = append(body, textBlock("paragraph", mdEscaper.Replace(strings.TrimSpace(ch.Data))))
			}
		}
		t := &mdBlock{typ: "toggle", props: map[string]any{"text": textSpans(summary)}, children: body}
		return []*mdBlock{t}
	case atom.Div, atom.Section, atom.Aside:
		// Confluence macros.
		switch {
		case hasClass(n, "confluence-information-macro"):
			icon := "ℹ️"
			cls := attr(n, "class")
			switch {
			case strings.Contains(cls, "macro-tip"):
				icon = "💡"
			case strings.Contains(cls, "macro-warning"):
				icon = "⚠️"
			case strings.Contains(cls, "macro-note"):
				icon = "📝"
			}
			body := n
			if b := findFirst(n, func(x *html.Node) bool { return hasClass(x, "confluence-information-macro-body") }); b != nil {
				body = b
			}
			inner := c.blocks(body, depth+1)
			cb := asContainer("callout", inner)
			cb.props["icon"] = icon
			return []*mdBlock{cb}
		case hasClass(n, "code") && hasClass(n, "panel"):
			if pre := findFirst(n, func(x *html.Node) bool { return x.DataAtom == atom.Pre }); pre != nil {
				return []*mdBlock{c.codeBlock(pre)}
			}
		case hasClass(n, "callout") || hasClass(n, "admonition") || attr(n, "role") == "note":
			icon, body := "💡", n
			// This app's own export: <span class="ci">icon</span><div>…</div>.
			if ci := findFirst(n, func(x *html.Node) bool { return hasClass(x, "ci") }); ci != nil {
				if t := strings.TrimSpace(textContent(ci)); t != "" {
					icon = t
				}
				for sib := ci.NextSibling; sib != nil; sib = sib.NextSibling {
					if sib.DataAtom == atom.Div {
						body = sib
						break
					}
				}
			}
			cb := asContainer("callout", c.blocks(body, depth+1))
			cb.props["icon"] = icon
			return []*mdBlock{cb}
		}
		return c.blocks(n, depth+1)
	}
	return c.blocks(n, depth+1)
}

func childNodes(n *html.Node) []*html.Node {
	var out []*html.Node
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		out = append(out, ch)
	}
	return out
}

func hasBlockChild(n *html.Node) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && isBlockElem(ch) && ch.DataAtom != atom.Img {
			return true
		}
	}
	return false
}

// asContainer makes a quote or callout from inner blocks: the first
// paragraph's text becomes its text, the rest nest inside.
func asContainer(typ string, inner []*mdBlock) *mdBlock {
	b := &mdBlock{typ: typ, props: map[string]any{"text": []Span{}}}
	if len(inner) > 0 && inner[0].typ == "paragraph" {
		b.props["text"] = inner[0].props["text"]
		inner = inner[1:]
	}
	b.children = inner
	return b
}

func (c *htmlConv) codeBlock(pre *html.Node) *mdBlock {
	lang := "plain"
	classes := attr(pre, "class") + " " + attr(pre, "data-syntaxhighlighter-params")
	if code := findFirst(pre, func(x *html.Node) bool { return x.DataAtom == atom.Code }); code != nil {
		classes += " " + attr(code, "class")
	}
	if code := findFirst(pre, func(x *html.Node) bool { return x.DataAtom == atom.Code }); code != nil && languageRE.MatchString(attr(code, "data-language")) {
		lang = attr(code, "data-language")
	} else if m := langClassRE.FindStringSubmatch(classes); m != nil {
		lang = m[1]
	} else if m := confluenceBrushRE.FindStringSubmatch(classes); m != nil {
		lang = m[1]
	}
	code := strings.TrimRight(textContent(pre), "\n")
	if len(code) > maxCodeChars {
		code = code[:maxCodeChars]
	}
	return &mdBlock{typ: "code", props: map[string]any{"code": code, "language": lang}}
}

// list converts <ul>/<ol> items (nested lists nest; an item opening with a
// checkbox is a to-do).
func (c *htmlConv) list(n *html.Node, depth int) []*mdBlock {
	typ := "bulleted_list"
	if n.DataAtom == atom.Ol {
		typ = "numbered_list"
	}
	var out []*mdBlock
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.DataAtom != atom.Li {
			if li.Type == html.ElementNode && (li.DataAtom == atom.Ul || li.DataAtom == atom.Ol) && len(out) > 0 {
				last := out[len(out)-1]
				last.children = append(last.children, c.list(li, depth+1)...)
			}
			continue
		}
		var inline []*html.Node
		var kids []*mdBlock
		itemType := typ
		checked := false
		for ch := li.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.ElementNode && ch.DataAtom == atom.Input && strings.EqualFold(attr(ch, "type"), "checkbox") {
				itemType = "to_do"
				_, checked = attrPresent(ch, "checked")
				continue
			}
			if ch.Type == html.ElementNode && (ch.DataAtom == atom.Ul || ch.DataAtom == atom.Ol) {
				kids = append(kids, c.list(ch, depth+1)...)
				continue
			}
			if ch.Type == html.ElementNode && isBlockElem(ch) && ch.DataAtom != atom.Img {
				if ch.DataAtom == atom.P && len(inline) == 0 && len(kids) == 0 {
					inline = append(inline, childNodes(ch)...)
					continue
				}
				kids = append(kids, c.block(ch, depth+1)...)
				continue
			}
			inline = append(inline, ch)
		}
		// Confluence and GitHub task lists mark items with classes.
		if hasClass(li, "checked") || attr(li, "data-inline-task-status") == "complete" {
			itemType, checked = "to_do", true
		} else if hasClass(n, "inline-task-list") || hasClass(li, "unchecked") || hasClass(li, "task-list-item") || attr(li, "data-inline-task-id") != "" {
			itemType = "to_do"
		}
		spans := trimSpans(c.inlineSpans(inline))
		b := &mdBlock{typ: itemType, props: map[string]any{"text": spans}, children: kids}
		if itemType == "to_do" {
			b.props["checked"] = checked
		}
		out = append(out, b)
	}
	return out
}

func attrPresent(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// table converts a table to a simple table block (header row when the first
// row has <th> cells).
func (c *htmlConv) table(n *html.Node) []*mdBlock {
	var rows [][][]Span
	header := false
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.DataAtom == atom.Table && x != n {
			return // nested tables flatten into their cell's text
		}
		if x.DataAtom == atom.Tr {
			var cells [][]Span
			for td := x.FirstChild; td != nil; td = td.NextSibling {
				if td.DataAtom != atom.Td && td.DataAtom != atom.Th {
					continue
				}
				if len(rows) == 0 && td.DataAtom == atom.Th {
					header = true
				}
				cell := trimSpans(c.inlineSpans(childNodes(td)))
				if len(cells) < maxTableCols {
					cells = append(cells, cell)
				}
			}
			if len(cells) > 0 && len(rows) < maxTableRows {
				rows = append(rows, cells)
			}
			return
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	if len(rows) == 0 {
		return nil
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	for i := range rows {
		for len(rows[i]) < width {
			rows[i] = append(rows[i], []Span{})
		}
	}
	return []*mdBlock{{typ: "table", props: map[string]any{"rows": rows, "header_row": header}}}
}

// image: an image from the upload (or a data: URI) becomes an image block; a
// web image becomes a bookmark to it (nothing is fetched).
func (c *htmlConv) image(n *html.Node, caption string) *mdBlock {
	src := strings.TrimSpace(attr(n, "src"))
	if caption == "" {
		caption = strings.TrimSpace(attr(n, "alt"))
	}
	if src == "" {
		return nil
	}
	if m := dataURIRE.FindStringSubmatch(src); m != nil {
		data, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			return nil
		}
		c.blobs++
		ext := strings.TrimPrefix(m[1], "image/")
		if ext == "jpeg" {
			ext = "jpg"
		}
		ref := fmt.Sprintf("html:%s:%d", c.docPath, c.blobs)
		name := fmt.Sprintf("image-%d.%s", c.blobs, ext)
		c.im.blobs[ref] = importBlob{name: name, data: data}
		return fileRefBlock(ref, name, textSpans(caption))
	}
	if p := resolveLocal(c.docPath, src); p != "" && c.im.zip != nil && c.im.zip.file(p) != nil {
		return fileRefBlock(p, path.Base(p), textSpans(caption))
	}
	if link := safeLink(src); link != "" && !strings.HasPrefix(link, "mailto:") {
		title := caption
		if title == "" {
			title = path.Base(link)
		}
		return &mdBlock{typ: "bookmark", props: map[string]any{"url": link, "title": title}}
	}
	return nil
}

// inlineSpans flattens inline content into spans with marks and links.
func (c *htmlConv) inlineSpans(nodes []*html.Node) []Span {
	var out []Span
	var walk func(n *html.Node, st Span)
	walk = func(n *html.Node, st Span) {
		switch n.Type {
		case html.TextNode:
			t := collapseSpace(n.Data)
			if t != "" {
				s := st
				s.Text = t
				out = append(out, s)
			}
			return
		case html.ElementNode:
		default:
			return
		}
		if skippedHTML(n) {
			return
		}
		switch n.DataAtom {
		case atom.Br:
			out = append(out, Span{Text: "\n"})
			return
		case atom.B, atom.Strong:
			st.Bold = true
		case atom.I, atom.Em, atom.Cite:
			st.Italic = true
		case atom.U, atom.Ins:
			st.Underline = true
		case atom.S, atom.Del, atom.Strike:
			st.Strike = true
		case atom.Code, atom.Kbd, atom.Samp, atom.Tt:
			st.Code = true
		case atom.A:
			href := strings.TrimSpace(attr(n, "href"))
			if id, ok := c.im.pageLinkFor(c.docPath, href); ok {
				label := strings.TrimSpace(textContent(n))
				if label == "" {
					label = "Untitled"
				}
				out = append(out, Span{Text: label, Mention: &Mention{Type: "page", ID: id.String()}})
				return
			}
			if l := safeLink(href); l != "" {
				st.Link = l
			}
		case atom.Img:
			if alt := strings.TrimSpace(attr(n, "alt")); alt != "" {
				out = append(out, Span{Text: alt})
			}
			return
		case atom.Input:
			return
		}
		if style := strings.ReplaceAll(attr(n, "style"), " ", ""); style != "" {
			// Google Docs and Word HTML mark formatting with inline styles.
			if strings.Contains(style, "font-weight:bold") || strings.Contains(style, "font-weight:700") {
				st.Bold = true
			}
			if strings.Contains(style, "font-style:italic") {
				st.Italic = true
			}
			if strings.Contains(style, "text-decoration:underline") {
				st.Underline = true
			}
			if strings.Contains(style, "text-decoration:line-through") {
				st.Strike = true
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch, st)
		}
	}
	for _, n := range nodes {
		walk(n, Span{})
	}
	return mergeAdjacent(out)
}

var htmlSpaceRE = regexp.MustCompile(`[ \t\r\n\f]+`)

func collapseSpace(s string) string {
	return htmlSpaceRE.ReplaceAllString(s, " ")
}

// trimSpans trims leading and trailing whitespace across spans and around
// line breaks.
func trimSpans(spans []Span) []Span {
	for len(spans) > 0 {
		t := strings.TrimLeft(spans[0].Text, " ")
		if t == "" && spans[0].Mention == nil {
			spans = spans[1:]
			continue
		}
		spans[0].Text = t
		break
	}
	for len(spans) > 0 {
		last := len(spans) - 1
		t := strings.TrimRight(spans[last].Text, " \n")
		if t == "" && spans[last].Mention == nil {
			spans = spans[:last]
			continue
		}
		spans[last].Text = t
		break
	}
	for i := range spans {
		spans[i].Text = strings.ReplaceAll(strings.ReplaceAll(spans[i].Text, " \n", "\n"), "\n ", "\n")
	}
	if spans == nil {
		return []Span{}
	}
	return spans
}

func mergeAdjacent(in []Span) []Span {
	var out []Span
	for _, s := range in {
		if len(out) > 0 && out[len(out)-1].sameMarks(s) {
			prev := &out[len(out)-1]
			if strings.HasSuffix(prev.Text, " ") && strings.HasPrefix(s.Text, " ") {
				s.Text = s.Text[1:]
			}
			prev.Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

// ---- Confluence space exports ------------------------------------------------------

// confluenceTree recognises a Confluence HTML space export (index.html with
// the page tree as nested lists of links to the page files) and builds the
// tree from it. ok is false for other zips.
func (im *importer) confluenceTree(zipName string) (*importNode, bool, error) {
	for _, f := range im.zip.files {
		if path.Base(f.Name) != "index.html" {
			continue
		}
		data, err := im.zip.read(f)
		if err != nil || !bytes.Contains(bytes.ToLower(data), []byte("confluence")) {
			return nil, false, nil
		}
		doc, err := html.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, false, nil
		}
		return im.confluenceFromIndex(doc, f.Name, zipName)
	}
	return nil, false, nil
}

func (im *importer) confluenceFromIndex(doc *html.Node, indexPath, zipName string) (*importNode, bool, error) {
	// The page tree is the first <ul> whose links point at .html files.
	tree := findFirst(doc, func(n *html.Node) bool {
		if n.DataAtom != atom.Ul {
			return false
		}
		a := findFirst(n, func(x *html.Node) bool { return x.DataAtom == atom.A })
		return a != nil && strings.HasSuffix(strings.ToLower(attr(a, "href")), ".html")
	})
	if tree == nil {
		return nil, false, nil
	}
	seen := map[string]bool{}
	var build func(ul *html.Node, depth int) []*importNode
	build = func(ul *html.Node, depth int) []*importNode {
		var out []*importNode
		for li := ul.FirstChild; li != nil; li = li.NextSibling {
			if li.DataAtom != atom.Li {
				continue
			}
			a := findFirst(li, func(x *html.Node) bool { return x.DataAtom == atom.A })
			if a == nil {
				continue
			}
			p := resolveLocal(indexPath, attr(a, "href"))
			f := im.zip.file(p)
			if f == nil || seen[p] {
				continue
			}
			seen[p] = true
			data, err := im.zip.read(f)
			if err != nil {
				im.fail(p, importMessage(err))
				continue
			}
			n := &importNode{path: p, title: cleanImportTitle(strings.TrimSpace(textContent(a))), format: "html", data: data}
			if depth < 20 {
				for ch := li.FirstChild; ch != nil; ch = ch.NextSibling {
					if ch.DataAtom == atom.Ul {
						n.children = append(n.children, build(ch, depth+1)...)
					}
				}
			}
			out = append(out, n)
		}
		return out
	}
	nodes := build(tree, 0)
	if len(nodes) == 0 {
		return nil, false, nil
	}
	if len(nodes) == 1 {
		return nodes[0], true, nil
	}
	return &importNode{title: cleanImportTitle(zipName), format: "folder", children: nodes}, true, nil
}
