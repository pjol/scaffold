package docs

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"scaffoldapp/backend/internal/httpx"
)

// Word (.docx) import, parsed here from the OOXML: headings (styles, also
// through basedOn and outline levels), bulleted, numbered and nested lists
// (numbering.xml), to-dos ("☐ "/"☑ "), quotes, code (code styles or
// monospace runs), callouts and dividers as this app exports them, tables,
// images, links (relationships and HYPERLINK fields) and bold, italic,
// underline and strikethrough. Tracked deletions are left out.

const (
	nsW = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsA = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsV = "urn:schemas-microsoft-com:vml"
)

var nsShort = map[string]string{nsW: "w", nsR: "r", nsA: "a", nsV: "v"}

// xnode is a parsed XML element ("w:p"), its attributes ("w:val") and children.
type xnode struct {
	name  string
	attrs map[string]string
	kids  []*xnode
	text  string // character data of w:t, w:instrText and similar leaves
}

func xname(n xml.Name) string {
	if p, ok := nsShort[n.Space]; ok {
		return p + ":" + n.Local
	}
	return n.Local
}

// parseXML reads an XML part into a tree (bounded depth and size).
func parseXML(data []byte) (*xnode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	root := &xnode{name: "#root"}
	stack := []*xnode{root}
	count := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			count++
			if count > 2_000_000 || len(stack) > 200 {
				return nil, httpx.BadRequest("the document is too large to import")
			}
			n := &xnode{name: xname(t.Name), attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[xname(a.Name)] = a.Value
			}
			parent := stack[len(stack)-1]
			parent.kids = append(parent.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top := stack[len(stack)-1]
			switch top.name {
			case "w:t", "w:instrText", "w:delText":
				top.text += string(t)
			}
		}
	}
	return root, nil
}

func (n *xnode) child(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

func (n *xnode) find(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
		if f := k.find(name); f != nil {
			return f
		}
	}
	return nil
}

func (n *xnode) val() string {
	if n == nil {
		return ""
	}
	return n.attrs["w:val"]
}

// on reads a toggle property (<w:b/>, <w:b w:val="0"/>).
func (n *xnode) on() bool {
	if n == nil {
		return false
	}
	v := strings.ToLower(n.val())
	return v != "0" && v != "false" && v != "none" && v != "off"
}

type docxStyle struct {
	name    string
	basedOn string
	outline int // 0: none, else heading level
	mono    bool
	numID   string
}

type docxPkg struct {
	zip    map[string]*zip.File
	rels   map[string]string // rId → target (a URL, or a part path for media)
	styles map[string]docxStyle
	numFmt map[string]map[int]string // numId → level → numFmt
	docKey string
}

var codeFonts = []string{"consolas", "courier", "courier new", "menlo", "monaco", "source code pro", "fira code", "jetbrains mono", "sf mono", "roboto mono", "lucida console"}

func readZipPart(files map[string]*zip.File, name string, limit int64) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, nil
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, httpx.BadRequest("the document is too large to import")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

// docxBlocks parses an imported Word document into blocks; embedded images
// are registered as blobs for the page.
func (im *importer) docxBlocks(n *importNode, data []byte) ([]*mdBlock, error) {
	blocks, title, err := parseDocx(data, n.displayPath(), func(ref, name string, b []byte) {
		im.blobs[ref] = importBlob{name: name, data: b}
	})
	if err != nil {
		return nil, err
	}
	if title != "" && n.page != nil && title != n.page.Title {
		im.rename(n, title)
	}
	return blocks, nil
}

// parseDocx returns the blocks and the document's title (a Title-styled
// first paragraph). addBlob receives each embedded image.
func parseDocx(data []byte, docKey string, addBlob func(ref, name string, b []byte)) ([]*mdBlock, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", httpx.BadRequest("that Word file can't be opened")
	}
	pkg := &docxPkg{zip: map[string]*zip.File{}, rels: map[string]string{}, styles: map[string]docxStyle{}, numFmt: map[string]map[int]string{}, docKey: docKey}
	for _, f := range zr.File {
		pkg.zip[strings.TrimPrefix(f.Name, "/")] = f
	}
	const partLimit = 40 << 20
	doc, err := readZipPart(pkg.zip, "word/document.xml", partLimit)
	if err != nil || doc == nil {
		return nil, "", httpx.BadRequest("that Word file has no document in it")
	}
	if raw, _ := readZipPart(pkg.zip, "word/_rels/document.xml.rels", 4<<20); raw != nil {
		if t, err := parseXML(raw); err == nil {
			for _, r := range t.find("Relationships").kidsOrSelf() {
				if r.name != "Relationship" {
					continue
				}
				target := r.attrs["Target"]
				if r.attrs["TargetMode"] != "External" {
					target = path.Clean(path.Join("word", target))
				}
				pkg.rels[r.attrs["Id"]] = target
			}
		}
	}
	if raw, _ := readZipPart(pkg.zip, "word/styles.xml", 8<<20); raw != nil {
		if t, err := parseXML(raw); err == nil {
			for _, st := range t.find("w:styles").kidsOrSelf() {
				if st.name != "w:style" {
					continue
				}
				s := docxStyle{name: strings.ToLower(st.child("w:name").val()), basedOn: st.child("w:basedOn").val()}
				if ppr := st.child("w:pPr"); ppr != nil {
					if ol := ppr.child("w:outlineLvl"); ol != nil {
						if v, err := strconv.Atoi(ol.val()); err == nil && v < 9 {
							s.outline = v + 1
						}
					}
					if np := ppr.child("w:numPr"); np != nil {
						s.numID = np.child("w:numId").val()
					}
				}
				if rpr := st.child("w:rPr"); rpr != nil {
					s.mono = docxMonoFont(rpr)
				}
				pkg.styles[st.attrs["w:styleId"]] = s
			}
		}
	}
	if raw, _ := readZipPart(pkg.zip, "word/numbering.xml", 8<<20); raw != nil {
		if t, err := parseXML(raw); err == nil {
			abs := map[string]map[int]string{}
			root := t.find("w:numbering")
			for _, a := range root.kidsOrSelf() {
				if a.name != "w:abstractNum" {
					continue
				}
				levels := map[int]string{}
				for _, l := range a.kids {
					if l.name != "w:lvl" {
						continue
					}
					i, _ := strconv.Atoi(l.attrs["w:ilvl"])
					levels[i] = l.child("w:numFmt").val()
				}
				abs[a.attrs["w:abstractNumId"]] = levels
			}
			for _, num := range root.kidsOrSelf() {
				if num.name != "w:num" {
					continue
				}
				pkg.numFmt[num.attrs["w:numId"]] = abs[num.child("w:abstractNumId").val()]
			}
		}
	}
	tree, err := parseXML(doc)
	if err != nil {
		return nil, "", httpx.BadRequest("that Word file is damaged")
	}
	body := tree.find("w:body")
	if body == nil {
		return []*mdBlock{}, "", nil
	}
	c := &docxConv{pkg: pkg, addBlob: addBlob}
	c.body(body.kids)
	c.flushCode()
	return c.out, c.title, nil
}

func (n *xnode) kidsOrSelf() []*xnode {
	if n == nil {
		return nil
	}
	return n.kids
}

func docxMonoFont(rpr *xnode) bool {
	f := rpr.child("w:rFonts")
	if f == nil {
		return false
	}
	for _, k := range []string{"w:ascii", "w:hAnsi", "w:cs"} {
		if slices.Contains(codeFonts, strings.ToLower(f.attrs[k])) {
			return true
		}
	}
	return false
}

// docxConv builds blocks, nesting list items by level.
type docxConv struct {
	pkg     *docxPkg
	addBlob func(ref, name string, b []byte)
	out     []*mdBlock
	list    []*mdBlock // open list items by level
	code    []string   // consecutive code lines
	title   string
	images  int
	first   bool
}

func (c *docxConv) body(kids []*xnode) {
	for _, k := range kids {
		switch k.name {
		case "w:p":
			c.paragraph(k)
		case "w:tbl":
			c.flushCode()
			c.list = nil
			if t := c.table(k); t != nil {
				c.out = append(c.out, t)
			}
		case "w:sdt":
			c.body(k.child("w:sdtContent").kidsOrSelf())
		case "w:customXml", "w:ins", "w:smartTag":
			c.body(k.kids)
		}
	}
}

func (c *docxConv) flushCode() {
	if len(c.code) == 0 {
		return
	}
	code := strings.Join(c.code, "\n")
	if len(code) > maxCodeChars {
		code = code[:maxCodeChars]
	}
	c.out = append(c.out, &mdBlock{typ: "code", props: map[string]any{"code": code, "language": "plain"}})
	c.code = nil
}

// styleChain follows basedOn to answer heading level, code and numbering.
func (c *docxConv) styleInfo(id string) (heading int, quote, mono bool, numID string, name string) {
	for i := 0; id != "" && i < 10; i++ {
		s, ok := c.pkg.styles[id]
		if !ok {
			break
		}
		if name == "" {
			name = s.name
		}
		if heading == 0 {
			switch {
			case s.name == "title":
				heading = -1
			case strings.HasPrefix(s.name, "heading "):
				if v, err := strconv.Atoi(strings.TrimPrefix(s.name, "heading ")); err == nil {
					heading = v
				}
			case s.outline > 0:
				heading = s.outline
			}
		}
		if strings.Contains(s.name, "quote") {
			quote = true
		}
		if s.mono || strings.Contains(s.name, "code") || strings.Contains(s.name, "source") || strings.Contains(s.name, "preformatted") || strings.Contains(s.name, "html pre") {
			mono = true
		}
		if numID == "" {
			numID = s.numID
		}
		id = s.basedOn
	}
	return
}

func headingType(level int) string {
	switch {
	case level <= 1:
		return "heading_1"
	case level == 2:
		return "heading_2"
	case level == 3:
		return "heading_3"
	}
	if slices.Contains(allTypes, "heading_4") {
		return "heading_4"
	}
	return "heading_3"
}

func (c *docxConv) paragraph(p *xnode) {
	ppr := p.child("w:pPr")
	styleID := ppr.child("w:pStyle").val()
	heading, quote, mono, numID, styleName := c.styleInfo(styleID)
	ilvl := 0
	if np := ppr.child("w:numPr"); np != nil {
		if v := np.child("w:numId").val(); v != "" {
			numID = v
		}
		ilvl, _ = strconv.Atoi(np.child("w:ilvl").val())
	}
	if ol := ppr.child("w:outlineLvl"); ol != nil && heading == 0 {
		if v, err := strconv.Atoi(ol.val()); err == nil && v < 9 {
			heading = v + 1
		}
	}
	spans, images, allMono := c.runs(p.kids)
	text := plainOf(spans)
	hasText := strings.TrimSpace(text) != ""
	// Code: a code style, or every run in a monospace font.
	if (mono || (allMono && hasText)) && heading == 0 && (numID == "" || numID == "0") {
		c.list = nil
		c.code = append(c.code, text)
		return
	}
	c.flushCode()
	emit := func(b *mdBlock) {
		c.out = append(c.out, b)
	}
	if !hasText && len(images) == 0 {
		if ppr.find("w:pBdr") != nil && ppr.find("w:bottom") != nil {
			c.list = nil
			emit(&mdBlock{typ: "divider", props: map[string]any{}})
		}
		return
	}
	spans = trimSpans(spans)
	var b *mdBlock
	switch {
	case !hasText:
	case heading == -1 && !c.first && c.title == "":
		c.title = strings.TrimSpace(text)
		c.first = true
		for _, img := range images {
			emit(img)
		}
		return
	case heading != 0:
		lvl := heading
		if lvl < 0 {
			lvl = 1
		}
		b = &mdBlock{typ: headingType(lvl), props: map[string]any{"text": spans}}
	case numID != "" && numID != "0" || strings.Contains(styleName, "list bullet") || strings.Contains(styleName, "list number"):
		typ := "bulleted_list"
		if f := c.pkg.numFmt[numID][ilvl]; f != "" && f != "bullet" && f != "none" {
			typ = "numbered_list"
		} else if f == "" && strings.Contains(styleName, "list number") {
			typ = "numbered_list"
		}
		b = &mdBlock{typ: typ, props: map[string]any{"text": spans}}
		if t, checked, rest, ok := checkboxPrefix(spans); ok {
			b.typ, b.props["text"], b.props["checked"] = t, rest, checked
		}
		c.first = true
		c.addListItem(b, min(ilvl, maxBlockDepth-2))
		for _, img := range images {
			emit(img)
		}
		return
	case quote:
		b = &mdBlock{typ: "quote", props: map[string]any{"text": spans}}
	default:
		b = &mdBlock{typ: "paragraph", props: map[string]any{"text": spans}}
		if t, checked, rest, ok := checkboxPrefix(spans); ok {
			b.typ, b.props["text"], b.props["checked"] = t, rest, checked
		} else if ppr.child("w:shd") != nil {
			if icon, rest, ok := startsWithEmoji(text); ok && rest != "" {
				b.typ, b.props["icon"] = "callout", icon
				b.props["text"] = trimSpans(sliceSpans(spans, utf8.RuneCountInString(icon), utf8.RuneCountInString(text)))
			}
		}
	}
	c.first = true
	c.list = nil
	if b != nil {
		emit(b)
	}
	for _, img := range images {
		emit(img)
	}
}

// checkboxPrefix: "☐ " / "☑ " / "☒ " opening a paragraph makes a to-do.
func checkboxPrefix(spans []Span) (string, bool, []Span, bool) {
	text := plainOf(spans)
	for _, p := range []struct {
		mark    string
		checked bool
	}{{"☐", false}, {"☑", true}, {"☒", true}, {"[ ]", false}, {"[x]", true}} {
		if strings.HasPrefix(text, p.mark) {
			n := utf8.RuneCountInString(p.mark)
			rest := trimSpans(sliceSpans(spans, n, utf8.RuneCountInString(text)))
			return "to_do", p.checked, rest, true
		}
	}
	return "", false, nil, false
}

// addListItem nests an item under the open item one level up.
func (c *docxConv) addListItem(b *mdBlock, level int) {
	if level < 0 {
		level = 0
	}
	if level > len(c.list) {
		level = len(c.list)
	}
	c.list = c.list[:level]
	if level == 0 {
		c.out = append(c.out, b)
	} else {
		parent := c.list[level-1]
		parent.children = append(parent.children, b)
	}
	c.list = append(c.list, b)
}

// runs collects a paragraph's text spans and the images it holds.
func (c *docxConv) runs(kids []*xnode) ([]Span, []*mdBlock, bool) {
	var spans []Span
	var monoAt []bool // spans in a code font
	var images []*mdBlock
	allMono, any := true, false
	add := func(s Span, mono bool) {
		spans = append(spans, s)
		monoAt = append(monoAt, mono)
	}
	var field struct {
		active    bool
		instr     string
		separated bool
		link      string
	}
	var walk func(kids []*xnode, link string)
	walk = func(kids []*xnode, link string) {
		for _, k := range kids {
			switch k.name {
			case "w:r":
				st := Span{Link: link}
				rpr := k.child("w:rPr")
				if rpr != nil {
					st.Bold = rpr.child("w:b").on()
					st.Italic = rpr.child("w:i").on()
					st.Underline = rpr.child("w:u") != nil && rpr.child("w:u").val() != "none"
					st.Strike = rpr.child("w:strike").on() || rpr.child("w:dstrike").on()
				}
				mono := rpr != nil && docxMonoFont(rpr)
				if rs := rpr.child("w:rStyle").val(); rs != "" {
					if s, ok := c.pkg.styles[rs]; ok && (s.mono || strings.Contains(s.name, "code")) {
						mono = true
					}
				}
				for _, rk := range k.kids {
					switch rk.name {
					case "w:fldChar":
						switch rk.attrs["w:fldCharType"] {
						case "begin":
							field.active, field.instr, field.separated, field.link = true, "", false, ""
						case "separate":
							field.separated = true
							if f := strings.Fields(field.instr); len(f) >= 2 && strings.EqualFold(f[0], "HYPERLINK") {
								field.link = safeLink(strings.Trim(f[1], `"`))
							}
						case "end":
							field.active = false
							field.link = ""
						}
					case "w:instrText":
						if field.active && !field.separated {
							field.instr += rk.text
						}
					case "w:t":
						if field.active && !field.separated {
							continue
						}
						s := st
						s.Text = rk.text
						if field.link != "" {
							s.Link = field.link
						}
						if s.Text != "" {
							any = true
							if !mono {
								allMono = false
							}
							add(s, mono)
						}
					case "w:tab":
						add(Span{Text: "\t", Link: st.Link}, false)
					case "w:br", "w:cr":
						if rk.attrs["w:type"] != "page" {
							add(Span{Text: "\n"}, false)
						}
					case "w:noBreakHyphen":
						add(Span{Text: "-"}, false)
					case "w:drawing", "w:pict", "mc:AlternateContent", "AlternateContent":
						if img := c.image(rk); img != nil {
							images = append(images, img)
						}
					}
				}
			case "w:hyperlink":
				target := ""
				if id := k.attrs["r:id"]; id != "" {
					target = safeLink(c.pkg.rels[id])
				}
				walk(k.kids, target)
			case "w:ins", "w:smartTag", "w:customXml", "w:fldSimple":
				l := link
				if k.name == "w:fldSimple" {
					if f := strings.Fields(k.attrs["w:instr"]); len(f) >= 2 && strings.EqualFold(f[0], "HYPERLINK") {
						l = safeLink(strings.Trim(f[1], `"`))
					}
				}
				walk(k.kids, l)
			case "w:sdt":
				walk(k.child("w:sdtContent").kidsOrSelf(), link)
			}
		}
	}
	walk(kids, "")
	if !any {
		allMono = false
	}
	// Runs in a code font inside ordinary text are inline code; a paragraph
	// that is all code font becomes a code block (the caller decides).
	if !allMono {
		for i := range spans {
			spans[i].Code = monoAt[i] && spans[i].Text != ""
		}
	}
	return mergeAdjacent(spans), images, allMono
}

// image turns a drawing's embedded picture into an image block.
func (c *docxConv) image(n *xnode) *mdBlock {
	blip := n.find("a:blip")
	id := ""
	if blip != nil {
		id = blip.attrs["r:embed"]
	} else if im := n.find("v:imagedata"); im != nil {
		id = im.attrs["r:id"]
	}
	target, ok := c.pkg.rels[id]
	if !ok || strings.Contains(target, "://") {
		return nil
	}
	data, err := readZipPart(c.pkg.zip, target, 30<<20)
	if err != nil || len(data) == 0 {
		return nil
	}
	c.images++
	ref := fmt.Sprintf("docx:%s:%s", c.pkg.docKey, target)
	name := path.Base(target)
	caption := ""
	if d := n.find("wp:docPr"); d != nil {
		caption = d.attrs["descr"]
	} else if d := n.find("docPr"); d != nil {
		caption = d.attrs["descr"]
	}
	c.addBlob(ref, name, data)
	return fileRefBlock(ref, name, textSpans(strings.TrimSpace(caption)))
}

// table converts a Word table (first row as the header).
func (c *docxConv) table(t *xnode) *mdBlock {
	var rows [][][]Span
	for _, tr := range t.kids {
		if tr.name != "w:tr" {
			continue
		}
		var cells [][]Span
		for _, tc := range tr.kids {
			if tc.name != "w:tc" && tc.name != "w:sdt" {
				continue
			}
			var cell []Span
			for _, p := range tc.kids {
				if p.name != "w:p" {
					continue
				}
				spans, _, _ := c.runs(p.kids)
				if len(cell) > 0 && len(spans) > 0 {
					cell = append(cell, Span{Text: "\n"})
				}
				cell = append(cell, spans...)
			}
			if len(cells) < maxTableCols {
				cells = append(cells, trimSpans(mergeAdjacent(cell)))
			}
		}
		if len(cells) > 0 && len(rows) < maxTableRows {
			rows = append(rows, cells)
		}
	}
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
	return &mdBlock{typ: "table", props: map[string]any{"rows": rows, "header_row": len(rows) > 1}}
}
