package docs

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/gif"  // image sizes for Word
	_ "image/jpeg" // image sizes for Word
	_ "image/png"  // image sizes for Word
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Word export: a .docx written here (WordprocessingML): styles for the title,
// headings, quote and code, real bulleted and numbered lists (numbering
// restarts per list), tables, embedded images (PNG, JPEG, GIF), links, and
// to-dos as ☐/☑ (the importer reads them back). Subpages, when included,
// follow on new pages.

type docxWriter struct {
	ex       *exporter
	body     strings.Builder
	rels     []string // extra relationships (images, links)
	media    []docxMedia
	nums     int          // list instances (each list restarts its numbering)
	numbered map[int]bool // list instance → numbered (else bulleted)
	nextID   int
	imgID    int
}

type docxMedia struct {
	name string
	data []byte
}

func xmlEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (d *docxWriter) rel(typ, target string, external bool) string {
	d.nextID++
	id := "rId" + strconv.Itoa(100+d.nextID)
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	d.rels = append(d.rels, `<Relationship Id="`+id+`" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/`+typ+`" Target="`+xmlEsc(target)+`"`+mode+`/>`)
	return id
}

// docx renders the export tree as a Word document.
func (ex *exporter) docx(root *exportNode) ([]byte, error) {
	d := &docxWriter{ex: ex}
	var page func(n *exportNode, first bool)
	page = func(n *exportNode, first bool) {
		if !first {
			d.body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		}
		title := displayTitle(n.page.Title)
		if n.page.Icon != nil {
			title = *n.page.Icon + " " + title
		}
		d.para("Title", "", []Span{{Text: title}}, nil)
		if n.page.IsRow && n.detail.Database != nil {
			for _, p := range n.detail.Database.Schema {
				if p.Type == "title" {
					continue
				}
				if v := formatValue(p, n.page.Props[p.ID], n.detail.Refs); v != "" {
					d.para("", "", []Span{{Text: p.Name + ": ", Bold: true}, {Text: v}}, nil)
				}
			}
		}
		d.blocks(n, n.detail.Blocks)
		if n.page.Kind == "database" {
			d.rowsTable(n)
		}
		if ex.subpages {
			for _, k := range append(append([]*exportNode{}, n.children...), n.rowPages...) {
				page(k, false)
			}
		}
	}
	page(root, true)
	return d.pack()
}

// runs writes spans as runs (links as hyperlinks).
func (d *docxWriter) runs(spans []Span, n *exportNode) string {
	var b strings.Builder
	for _, s := range spans {
		text := s.Text
		link := safeLink(s.Link)
		if s.Mention != nil {
			switch s.Mention.Type {
			case "user":
				if n != nil {
					if u, ok := n.detail.Refs.Users[s.Mention.ID]; ok {
						text = "@" + u.Name
					}
				}
			case "page":
				if n != nil {
					if p, ok := n.detail.Refs.Pages[s.Mention.ID]; ok && p.Title != nil {
						text = *p.Title
					}
				}
				if web := d.ex.s.webURL(); web != "" {
					link = strings.TrimRight(web, "/") + "/docs/" + s.Mention.ID
				}
			}
		}
		var rpr strings.Builder
		if link != "" {
			rpr.WriteString(`<w:rStyle w:val="Hyperlink"/>`)
		}
		if s.Code || s.Equation {
			rpr.WriteString(`<w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/>`)
		}
		if s.Bold {
			rpr.WriteString(`<w:b/>`)
		}
		if s.Italic {
			rpr.WriteString(`<w:i/>`)
		}
		if s.Strike {
			rpr.WriteString(`<w:strike/>`)
		}
		if s.Underline && link == "" {
			rpr.WriteString(`<w:u w:val="single"/>`)
		}
		var run strings.Builder
		for i, line := range strings.Split(text, "\n") {
			if i > 0 {
				run.WriteString(`<w:r>` + wrapRPr(rpr.String()) + `<w:br/></w:r>`)
			}
			for j, part := range strings.Split(line, "\t") {
				if j > 0 {
					run.WriteString(`<w:r>` + wrapRPr(rpr.String()) + `<w:tab/></w:r>`)
				}
				if part != "" {
					run.WriteString(`<w:r>` + wrapRPr(rpr.String()) + `<w:t xml:space="preserve">` + xmlEsc(part) + `</w:t></w:r>`)
				}
			}
		}
		if link != "" {
			b.WriteString(`<w:hyperlink r:id="` + d.rel("hyperlink", link, true) + `">` + run.String() + `</w:hyperlink>`)
		} else {
			b.WriteString(run.String())
		}
	}
	return b.String()
}

func wrapRPr(s string) string {
	if s == "" {
		return ""
	}
	return "<w:rPr>" + s + "</w:rPr>"
}

// para writes a paragraph with a style, extra paragraph properties and spans.
func (d *docxWriter) para(style, ppr string, spans []Span, n *exportNode) {
	d.body.WriteString("<w:p>")
	if style != "" || ppr != "" {
		d.body.WriteString("<w:pPr>")
		if style != "" {
			d.body.WriteString(`<w:pStyle w:val="` + style + `"/>`)
		}
		d.body.WriteString(ppr + "</w:pPr>")
	}
	d.body.WriteString(d.runs(spans, n))
	d.body.WriteString("</w:p>")
}

func indent(depth int) string {
	if depth <= 0 {
		return ""
	}
	return `<w:ind w:left="` + strconv.Itoa(360*depth) + `"/>`
}

func (d *docxWriter) blocks(n *exportNode, blocks []Block) {
	kids := map[uuid.UUID][]Block{}
	ids := map[uuid.UUID]bool{}
	for _, b := range blocks {
		ids[b.ID] = true
	}
	var roots []Block
	for _, b := range blocks {
		if b.ParentID != nil && ids[*b.ParentID] {
			kids[*b.ParentID] = append(kids[*b.ParentID], b)
		} else {
			roots = append(roots, b)
		}
	}
	var walk func(list []Block, depth int, listNum map[string]int)
	walk = func(list []Block, depth int, listNum map[string]int) {
		prev := ""
		for _, b := range list {
			if depth > 12 {
				return
			}
			text := textOf(b.Props)
			switch b.Type {
			case "heading_1", "heading_2", "heading_3", "heading_4":
				d.para("Heading"+b.Type[len(b.Type)-1:], "", text, n)
			case "bulleted_list", "numbered_list":
				// A run of items of one kind is one list (numbering restarts).
				if prev != b.Type || listNum[b.Type] == 0 {
					d.nums++
					listNum[b.Type] = d.nums
					if d.numbered == nil {
						d.numbered = map[int]bool{}
					}
					d.numbered[d.nums] = b.Type == "numbered_list"
				}
				id := listNum[b.Type]
				d.body.WriteString(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="` + strconv.Itoa(min(depth, 8)) + `"/><w:numId w:val="` + strconv.Itoa(id) + `"/></w:numPr></w:pPr>` + d.runs(text, n) + `</w:p>`)
				walk(kids[b.ID], depth+1, map[string]int{})
				prev = b.Type
				continue
			case "to_do":
				mark := "☐ "
				if propBool(b.Props, "checked") {
					mark = "☑ "
				}
				d.para("", indent(depth), append([]Span{{Text: mark}}, text...), n)
			case "toggle":
				d.para("", indent(depth), append([]Span{{Text: "▸ "}}, text...), n)
				walk(kids[b.ID], depth+1, map[string]int{})
				prev = b.Type
				continue
			case "quote":
				d.para("Quote", indent(depth), text, n)
			case "callout":
				icon := propString(b.Props, "icon")
				// Shading on the paragraph itself marks it as a callout for the importer.
				d.para("Callout", `<w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/>`+indent(depth), append([]Span{{Text: icon + " "}}, text...), n)
			case "code":
				for _, line := range strings.Split(propString(b.Props, "code"), "\n") {
					d.para("Code", indent(depth), []Span{{Text: line}}, nil)
				}
			case "equation":
				d.para("Code", indent(depth), []Span{{Text: propString(b.Props, "expression")}}, nil)
			case "divider":
				d.body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="auto"/></w:pBdr></w:pPr></w:p>`)
			case "image":
				d.image(n, b)
			case "file", "video", "audio", "pdf":
				name := propString(b.Props, "name")
				if name == "" {
					name = "File"
				}
				link := ""
				if web := d.ex.s.webURL(); web != "" && propString(b.Props, "file_id") != "" {
					link = strings.TrimRight(web, "/") + "/docs/files/" + propString(b.Props, "file_id")
				} else {
					link = propString(b.Props, "url")
				}
				d.para("", indent(depth), []Span{{Text: "📎 " + name, Link: link}}, n)
			case "bookmark", "embed":
				u := propString(b.Props, "url")
				title := propString(b.Props, "title")
				if title == "" {
					title = u
				}
				d.para("", indent(depth), []Span{{Text: title, Link: u}}, n)
			case "page", "database", "link_to_page":
				key := "page_id"
				if b.Type == "database" {
					key = "database_id"
				}
				id := propString(b.Props, key)
				title := "Untitled"
				if p, ok := n.detail.Refs.Pages[id]; ok && p.Title != nil {
					title = *p.Title
				}
				link := ""
				if web := d.ex.s.webURL(); web != "" {
					link = strings.TrimRight(web, "/") + "/docs/" + id
				}
				d.para("", indent(depth), []Span{{Text: "📄 " + title, Link: link}}, n)
			case "connected_page":
				d.para("", indent(depth), []Span{{Text: "🔗 " + propString(b.Props, "title"), Link: propString(b.Props, "url")}}, n)
			case "table":
				raw, _ := json.Marshal(b.Props["rows"])
				var rows [][][]Span
				_ = json.Unmarshal(raw, &rows)
				d.table(rows, propBool(b.Props, "header_row"), n)
			case "column_list", "column", "synced_block":
				walk(kids[b.ID], depth, map[string]int{})
				prev = b.Type
				continue
			case "table_of_contents", "breadcrumb", "button":
			default:
				if len(text) > 0 || b.Type == "paragraph" {
					d.para("", indent(depth), text, n)
				}
			}
			walk(kids[b.ID], depth+1, map[string]int{})
			prev = b.Type
		}
	}
	walk(roots, 0, map[string]int{})
}

func (d *docxWriter) table(rows [][][]Span, header bool, n *exportNode) {
	if len(rows) == 0 {
		return
	}
	d.body.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/><w:tblBorders>` +
		`<w:top w:val="single" w:sz="4" w:color="auto"/><w:left w:val="single" w:sz="4" w:color="auto"/><w:bottom w:val="single" w:sz="4" w:color="auto"/>` +
		`<w:right w:val="single" w:sz="4" w:color="auto"/><w:insideH w:val="single" w:sz="4" w:color="auto"/><w:insideV w:val="single" w:sz="4" w:color="auto"/>` +
		`</w:tblBorders></w:tblPr>`)
	for i, row := range rows {
		d.body.WriteString("<w:tr>")
		if i == 0 && header {
			d.body.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for _, cell := range row {
			spans := cell
			if i == 0 && header {
				spans = make([]Span, len(cell))
				for j, s := range cell {
					s.Bold = true
					spans[j] = s
				}
			}
			d.body.WriteString("<w:tc><w:p>" + d.runs(spans, n) + "</w:p></w:tc>")
		}
		d.body.WriteString("</w:tr>")
	}
	d.body.WriteString("</w:tbl><w:p/>")
}

func (d *docxWriter) rowsTable(n *exportNode) {
	head := make([][]Span, len(n.page.Schema))
	for i, p := range n.page.Schema {
		head[i] = []Span{{Text: p.Name}}
	}
	rows := [][][]Span{head}
	for _, r := range n.rows {
		cells := make([][]Span, len(n.page.Schema))
		for i, p := range n.page.Schema {
			v := displayTitle(r.Title)
			if p.Type != "title" {
				v = formatValue(p, r.Props[p.ID], n.rowRefs)
			}
			cells[i] = []Span{{Text: v}}
		}
		rows = append(rows, cells)
	}
	d.table(rows, true, nil)
}

// image embeds an uploaded PNG, JPEG or GIF (scaled to the text width); other
// images are linked.
func (d *docxWriter) image(n *exportNode, b Block) {
	f := n.file(propString(b.Props, "file_id"))
	caption := spansOf(b.Props, "caption")
	if f == nil || f.data == nil {
		name := propString(b.Props, "name")
		if name == "" {
			name = "Image"
		}
		link := ""
		if web := d.ex.s.webURL(); web != "" {
			link = strings.TrimRight(web, "/") + "/docs/files/" + propString(b.Props, "file_id")
		}
		d.para("", "", []Span{{Text: "🖼 " + name, Link: link}}, n)
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(f.data))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		d.para("", "", []Span{{Text: "🖼 " + f.name}}, n)
		return
	}
	ext := map[string]string{"png": "png", "jpeg": "jpeg", "gif": "gif"}[format]
	if ext == "" {
		d.para("", "", []Span{{Text: "🖼 " + f.name}}, n)
		return
	}
	d.imgID++
	name := fmt.Sprintf("image%d.%s", d.imgID, ext)
	d.media = append(d.media, docxMedia{name: name, data: f.data})
	rid := d.rel("image", "media/"+name, false)
	const emuPerPx = 9525
	const maxWidth = 5943600 // 6.5 in
	cx, cy := int64(cfg.Width)*emuPerPx, int64(cfg.Height)*emuPerPx
	if cx > maxWidth {
		cy = cy * maxWidth / cx
		cx = maxWidth
	}
	descr := xmlEsc(plainOf(caption))
	id := strconv.Itoa(d.imgID)
	d.body.WriteString(`<w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="` + strconv.FormatInt(cx, 10) + `" cy="` + strconv.FormatInt(cy, 10) + `"/>` +
		`<wp:docPr id="` + id + `" name="Picture ` + id + `" descr="` + descr + `"/><a:graphic xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` +
		`<a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">` +
		`<pic:nvPicPr><pic:cNvPr id="` + id + `" name="` + name + `"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="` + rid + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>` +
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="` + strconv.FormatInt(cx, 10) + `" cy="` + strconv.FormatInt(cy, 10) + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>` +
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`)
	if len(caption) > 0 {
		d.para("Caption", "", caption, n)
	}
}

const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:spacing w:after="240"/></w:pPr><w:rPr><w:b/><w:sz w:val="48"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="280" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="60"/><w:outlineLvl w:val="3"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="8" w:color="auto"/></w:pBdr><w:ind w:left="240"/></w:pPr><w:rPr><w:i/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Callout"><w:name w:val="Callout"/><w:basedOn w:val="Normal"/><w:pPr><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/><w:ind w:left="120" w:right="120"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="caption"/><w:basedOn w:val="Normal"/><w:rPr><w:i/><w:sz w:val="18"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="40"/><w:ind w:left="720"/><w:contextualSpacing/></w:pPr></w:style>
<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>
<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblCellMar><w:left w:w="108" w:type="dxa"/><w:right w:w="108" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>
</w:styles>`

func numberingXML(numbered map[int]bool, count int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + `<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	bullets := []string{"•", "◦", "▪"}
	for abs, decimal := range []bool{false, true} {
		b.WriteString(`<w:abstractNum w:abstractNumId="` + strconv.Itoa(abs) + `"><w:multiLevelType w:val="hybridMultilevel"/>`)
		for l := 0; l < 9; l++ {
			fmtVal, text := "bullet", bullets[l%3]
			if decimal {
				fmtVal, text = []string{"decimal", "lowerLetter", "lowerRoman"}[l%3], "%"+strconv.Itoa(l+1)+"."
			}
			b.WriteString(`<w:lvl w:ilvl="` + strconv.Itoa(l) + `"><w:start w:val="1"/><w:numFmt w:val="` + fmtVal + `"/><w:lvlText w:val="` + xmlEsc(text) + `"/><w:lvlJc w:val="left"/>` +
				`<w:pPr><w:ind w:left="` + strconv.Itoa(720+360*l) + `" w:hanging="360"/></w:pPr></w:lvl>`)
		}
		b.WriteString(`</w:abstractNum>`)
	}
	for id := 1; id <= count; id++ {
		abs := 0
		if numbered[id] {
			abs = 1
		}
		b.WriteString(`<w:num w:numId="` + strconv.Itoa(id) + `"><w:abstractNumId w:val="` + strconv.Itoa(abs) + `"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`)
	}
	b.WriteString(`</w:numbering>`)
	return b.String()
}

func (d *docxWriter) pack() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, content string) error { return zipPut(zw, name, []byte(content)) }
	if err := put("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Default Extension="jpeg" ContentType="image/jpeg"/><Default Extension="gif" ContentType="image/gif"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/></Types>`); err != nil {
		return nil, err
	}
	if err := put("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`); err != nil {
		return nil, err
	}
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` +
		strings.Join(d.rels, "") + `</Relationships>`
	if err := put("word/_rels/document.xml.rels", rels); err != nil {
		return nil, err
	}
	if err := put("word/styles.xml", docxStyles); err != nil {
		return nil, err
	}
	if err := put("word/numbering.xml", numberingXML(d.numbered, d.nums)); err != nil {
		return nil, err
	}
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>` +
		d.body.String() + `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`
	if err := put("word/document.xml", doc); err != nil {
		return nil, err
	}
	for _, m := range d.media {
		if err := zipPut(zw, "word/media/"+m.name, m.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
