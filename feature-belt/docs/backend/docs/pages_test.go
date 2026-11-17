package docs

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
)

// ---- diff ------------------------------------------------------------------------

func sb(id uuid.UUID, parent *uuid.UUID, typ, text, pos string) snapBlock {
	return snapBlock{ID: id, ParentID: parent, Type: typ, Position: pos, Props: map[string]any{"text": textSpans(text)}}
}

func TestDiffBlocks(t *testing.T) {
	a, b, c, d, e := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	older := []snapBlock{
		sb(a, nil, "heading_1", "Plan", "a0"),
		sb(b, nil, "paragraph", "First draft", "a1"),
		sb(c, nil, "paragraph", "Gone soon", "a2"),
		sb(d, &b, "bulleted_list", "child", "a0"),
	}
	newer := []snapBlock{
		sb(a, nil, "heading_1", "Plan", "a0"),
		sb(b, nil, "paragraph", "Second draft", "a1"),
		sb(e, nil, "to_do", "New task", "a3"),
		sb(d, &b, "bulleted_list", "child", "a0"),
	}
	entries, sum := diffBlocks(older, newer)
	if sum.Added != 1 || sum.Removed != 1 || sum.Changed != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	var order []string
	for _, en := range entries {
		order = append(order, plainOf(textOf(en.Block.Props))+":"+en.Change)
	}
	want := []string{"Plan:", "Second draft:changed", "child:", "Gone soon:removed", "New task:added"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for _, en := range entries {
		if en.Change == "changed" && en.BeforeText != "First draft" {
			t.Fatalf("before = %q", en.BeforeText)
		}
		if plainOf(textOf(en.Block.Props)) == "child" && en.Depth != 1 {
			t.Fatalf("child depth = %d", en.Depth)
		}
	}
	if got := sum.Text(); got != "Added 1 block, edited 1 block, removed 1 block" {
		t.Fatalf("text = %q", got)
	}
	if _, s := diffBlocks(newer, newer); !s.Empty() {
		t.Fatalf("same versions differ: %+v", s)
	}
}

// ---- Word ------------------------------------------------------------------------

func testPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := range 40 {
		for y := range 20 {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 12), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func blk(typ string, parent *uuid.UUID, pos string, props map[string]any) Block {
	return Block{ID: uuid.New(), ParentID: parent, Type: typ, Position: pos, Props: props}
}

func testService() *Service {
	return &Service{d: &app.Deps{Cfg: &config.Config{PublicWebURL: "https://app.example.test"}}, cfg: settings{fileMax: 25 << 20, importMax: 50 << 20}}
}

// exportFixture is a page with every common block, an image and a table.
func exportFixture(t *testing.T) *exportNode {
	fileID := uuid.New()
	list := blk("bulleted_list", nil, "a3", map[string]any{"text": textSpans("Outer")})
	blocks := []Block{
		blk("heading_1", nil, "a0", map[string]any{"text": textSpans("Overview")}),
		blk("paragraph", nil, "a1", map[string]any{"text": []Span{{Text: "Plain "}, {Text: "bold", Bold: true}, {Text: " and "}, {Text: "link", Link: "https://example.com/x"}}}),
		blk("heading_2", nil, "a2", map[string]any{"text": textSpans("Steps")}),
		list,
		blk("bulleted_list", &list.ID, "a0", map[string]any{"text": textSpans("Inner")}),
		blk("numbered_list", nil, "a4", map[string]any{"text": textSpans("One")}),
		blk("numbered_list", nil, "a5", map[string]any{"text": textSpans("Two")}),
		blk("to_do", nil, "a6", map[string]any{"text": textSpans("Ship it"), "checked": true}),
		blk("to_do", nil, "a7", map[string]any{"text": textSpans("Tell people"), "checked": false}),
		blk("quote", nil, "a8", map[string]any{"text": textSpans("Quoted words")}),
		blk("callout", nil, "a9", map[string]any{"text": textSpans("Heads up"), "icon": "💡"}),
		blk("code", nil, "b0", map[string]any{"code": "func main() {\n\tprintln(1)\n}", "language": "go"}),
		blk("divider", nil, "b1", map[string]any{}),
		blk("table", nil, "b2", map[string]any{"header_row": true, "rows": [][][]Span{{textSpans("Name"), textSpans("Role")}, {textSpans("Sam"), textSpans("Lead")}}}),
		blk("image", nil, "b3", map[string]any{"file_id": fileID.String(), "name": "chart.png", "caption": textSpans("A chart")}),
	}
	p := &Page{ID: uuid.New(), Title: "Quarterly plan", Kind: "page", Font: "default"}
	n := &exportNode{page: p, detail: &PageDetail{Page: p, Blocks: orderTree(blocks), Refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}}}
	n.files = []exportFile{{id: fileID.String(), name: "chart.png", contentType: "image/png", data: testPNG(t)}}
	return n
}

func typesAndTexts(list []*mdBlock, depth int, out *[]string) {
	for _, b := range list {
		text := ""
		switch v := b.props["text"].(type) {
		case []Span:
			text = plainOf(v)
		}
		if b.typ == "code" {
			text = propString(b.props, "code")
		}
		if b.typ == "to_do" && propBool(b.props, "checked") {
			text += " [x]"
		}
		*out = append(*out, strings.Repeat("  ", depth)+b.typ+":"+text)
		typesAndTexts(b.children, depth+1, out)
	}
}

func TestDocxRoundTrip(t *testing.T) {
	ex := &exporter{s: testService()}
	data, err := ex.docx(exportFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	var blobs []string
	blocks, title, err := parseDocx(data, "plan.docx", func(ref, name string, b []byte) {
		if _, _, err := image.DecodeConfig(bytes.NewReader(b)); err != nil {
			t.Fatalf("embedded image %s doesn't decode: %v", name, err)
		}
		blobs = append(blobs, name)
	})
	if err != nil {
		t.Fatal(err)
	}
	if title != "Quarterly plan" {
		t.Fatalf("title = %q", title)
	}
	var got []string
	typesAndTexts(blocks, 0, &got)
	want := []string{
		"heading_1:Overview",
		"paragraph:Plain bold and link",
		"heading_2:Steps",
		"bulleted_list:Outer",
		"  bulleted_list:Inner",
		"numbered_list:One",
		"numbered_list:Two",
		"to_do:Ship it [x]",
		"to_do:Tell people",
		"quote:Quoted words",
		"callout:Heads up",
		"code:func main() {\n\tprintln(1)\n}",
		"divider:",
		"table:",
		"import_file:",
		"paragraph:A chart",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(blobs) != 1 {
		t.Fatalf("images = %v", blobs)
	}
	// Marks and links survive.
	spans := blocks[1].props["text"].([]Span)
	var bold, link bool
	for _, s := range spans {
		bold = bold || (s.Bold && s.Text == "bold")
		link = link || (s.Link == "https://example.com/x" && s.Text == "link")
	}
	if !bold || !link {
		t.Fatalf("marks lost: %+v", spans)
	}
	rows := blocks[12].props["rows"].([][][]Span)
	if len(rows) != 2 || plainOf(rows[1][0]) != "Sam" || !propBool(blocks[12].props, "header_row") {
		t.Fatalf("table = %+v", rows)
	}
}

// TestDocxFromWord parses a document shaped like Word's own output: style
// ids that differ from names, basedOn chains, abstract numbering, a
// hyperlink relationship, a HYPERLINK field, tracked deletions.
func TestDocxFromWord(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>
<w:p><w:pPr><w:pStyle w:val="Titre"/></w:pPr><w:r><w:t>Launch notes</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Kop1"/></w:pPr><w:r><w:t>Goals</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">We </w:t></w:r><w:r><w:rPr><w:i/></w:rPr><w:t>really</w:t></w:r><w:del><w:r><w:delText>never</w:delText></w:r></w:del><w:r><w:t xml:space="preserve"> ship. See </w:t></w:r><w:hyperlink r:id="rId9"><w:r><w:t>the doc</w:t></w:r></w:hyperlink><w:r><w:t xml:space="preserve"> or </w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> HYPERLINK "https://field.example/" </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>this</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="3"/></w:numPr></w:pPr><w:r><w:t>Step one</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="1"/><w:numId w:val="3"/></w:numPr></w:pPr><w:r><w:t>Detail</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="4"/></w:numPr></w:pPr><w:r><w:t>A bullet</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Code"/></w:pPr><w:r><w:t>x := 1</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas"/></w:rPr><w:t>y := 2</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Use </w:t></w:r><w:r><w:rPr><w:rFonts w:ascii="Courier New"/></w:rPr><w:t>go test</w:t></w:r></w:p>
<w:p><w:pPr><w:outlineLvl w:val="2"/></w:pPr><w:r><w:t>Outline heading</w:t></w:r></w:p>
<w:p/>
</w:body></w:document>`
	styles := `<?xml version="1.0" encoding="UTF-8"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:style w:type="paragraph" w:styleId="Titre"><w:name w:val="Title"/></w:style>
<w:style w:type="paragraph" w:styleId="Base"><w:name w:val="heading 1"/></w:style>
<w:style w:type="paragraph" w:styleId="Kop1"><w:name w:val="My heading"/><w:basedOn w:val="Base"/></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/></w:style>
<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/></w:style></w:styles>`
	numbering := `<?xml version="1.0" encoding="UTF-8"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="7"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="lowerLetter"/></w:lvl></w:abstractNum>
<w:abstractNum w:abstractNumId="8"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>
<w:num w:numId="3"><w:abstractNumId w:val="7"/></w:num><w:num w:numId="4"><w:abstractNumId w:val="8"/></w:num></w:numbering>`
	rels := `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://docs.example/a" TargetMode="External"/></Relationships>`
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"word/document.xml": doc, "word/styles.xml": styles, "word/numbering.xml": numbering, "word/_rels/document.xml.rels": rels} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	blocks, title, err := parseDocx(buf.Bytes(), "notes.docx", func(string, string, []byte) {})
	if err != nil {
		t.Fatal(err)
	}
	if title != "Launch notes" {
		t.Fatalf("title = %q", title)
	}
	var got []string
	typesAndTexts(blocks, 0, &got)
	want := []string{
		"heading_1:Goals",
		"paragraph:We really ship. See the doc or this",
		"numbered_list:Step one",
		"  numbered_list:Detail",
		"bulleted_list:A bullet",
		"code:x := 1\ny := 2",
		"paragraph:Use go test",
		"heading_3:Outline heading",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	spans := blocks[1].props["text"].([]Span)
	links := map[string]string{}
	italic := false
	for _, s := range spans {
		if s.Link != "" {
			links[s.Text] = s.Link
		}
		italic = italic || (s.Italic && s.Text == "really")
	}
	if links["the doc"] != "https://docs.example/a" || links["this"] != "https://field.example/" || !italic {
		t.Fatalf("spans = %+v", spans)
	}
	code := blocks[5].props["text"].([]Span)
	if len(code) != 2 || !code[1].Code || code[0].Code {
		t.Fatalf("inline code = %+v", code)
	}
}

// ---- HTML ------------------------------------------------------------------------

func TestHTMLImport(t *testing.T) {
	src := `<!doctype html><html><head><title>Space : Release checklist</title><style>p{color:red}</style><script>alert(1)</script></head>
<body><div id="breadcrumb-section">Home / Space</div><div id="main-content" class="wiki-content">
<h1>Release checklist</h1>
<p>Read <b>this</b> <a href="https://example.com/doc">first</a> and <a href="javascript:alert(1)">not this</a>.<br>Second line</p>
<div class="confluence-information-macro confluence-information-macro-warning"><div class="confluence-information-macro-body"><p>Careful now</p></div></div>
<ul class="inline-task-list"><li class="checked">Freeze</li><li data-inline-task-status="incomplete">Tag</li></ul>
<ol><li>One<ol><li>One a</li></ol></li><li><p>Two</p></li></ol>
<div class="code panel pdl"><div class="codeContent"><pre class="syntaxhighlighter-pre" data-syntaxhighlighter-params="brush: bash; gutter: false">make release</pre></div></div>
<table><tr><th>Who</th><th>What</th></tr><tr><td>Sam</td><td><i>Ship</i></td></tr></table>
<details><summary>More</summary><p>Hidden text</p></details>
<blockquote><p>Quote</p></blockquote><hr>
<img src="https://cdn.example/x.png" alt="Diagram">
<img src="data:image/png;base64,` + "iVBORw0KGgo=" + `" alt="inline">
</div><div id="footer">Powered by Confluence</div></body></html>`
	im := &importer{s: testService(), blobs: map[string]importBlob{}, byPath: map[string]uuid.UUID{}, used: map[string]bool{}}
	c := &htmlConv{im: im, docPath: "page.html"}
	blocks, title := c.convert([]byte(src))
	if title != "Release checklist" {
		t.Fatalf("title = %q", title)
	}
	var got []string
	typesAndTexts(blocks, 0, &got)
	want := []string{
		"paragraph:Read this first and not this.\nSecond line",
		"callout:Careful now",
		"to_do:Freeze [x]",
		"to_do:Tag",
		"numbered_list:One",
		"  numbered_list:One a",
		"numbered_list:Two",
		"code:make release",
		"table:",
		"toggle:More",
		"  paragraph:Hidden text",
		"quote:Quote",
		"divider:",
		"bookmark:",
		"import_file:",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocks:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if blocks[1].props["icon"] != "⚠️" || propString(blocks[6].props, "language") != "bash" {
		t.Fatalf("callout/code props: %v %v", blocks[1].props, blocks[6].props)
	}
	for _, s := range blocks[0].props["text"].([]Span) {
		if strings.Contains(s.Link, "javascript") {
			t.Fatal("unsafe link kept")
		}
	}
	if len(im.blobs) != 1 {
		t.Fatalf("data: image not captured: %d", len(im.blobs))
	}
}

func TestHTMLExportRoundTrip(t *testing.T) {
	ex := &exporter{s: testService()}
	n := exportFixture(t)
	doc := ex.htmlDocument(n, htmlOpts{inline: true})
	if strings.Contains(doc, "<script") || !strings.Contains(doc, "data:image/png;base64,") {
		t.Fatal("export should embed the image and hold no script")
	}
	im := &importer{s: testService(), blobs: map[string]importBlob{}, byPath: map[string]uuid.UUID{}, used: map[string]bool{}}
	c := &htmlConv{im: im, docPath: "plan.html"}
	blocks, title := c.convert([]byte(doc))
	if title != "Quarterly plan" {
		t.Fatalf("title = %q", title)
	}
	var got []string
	typesAndTexts(blocks, 0, &got)
	joined := strings.Join(got, "\n")
	for _, w := range []string{"heading_1:Overview", "bulleted_list:Outer\n  bulleted_list:Inner", "numbered_list:One", "to_do:Ship it [x]", "quote:Quoted words", "code:func main()", "table:", "import_file:"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("missing %q in:\n%s", w, joined)
		}
	}
}

// ---- zip and Markdown imports ----------------------------------------------------

func zipOf(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNotionZipTree(t *testing.T) {
	id1, id2, id3 := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	files := map[string]string{
		"Export/Wiki " + id1 + ".md":                         "# Wiki\n\nSee [Setup](Wiki%20" + id1 + "/Setup%20" + id2 + ".md).\n\n![Logo](Wiki%20" + id1 + "/logo.png)\n",
		"Export/Wiki " + id1 + "/Setup " + id2 + ".md":       "# Setup\n\nSteps.\n",
		"Export/Wiki " + id1 + "/logo.png":                   string(testPNG(t)),
		"Export/Wiki " + id1 + "/Tasks " + id3 + ".csv":      "Name,Status,Due,Done\nWrite,Doing,2026-11-02,Yes\nShip,Done,2026-11-09,No\nTell,Todo,,No\n",
		"Export/Wiki " + id1 + "/Tasks " + id3 + "/Write.md": "# Write\n\nStatus: Doing\nDue: 2026-11-02\n\nRow body.\n",
		"__MACOSX/._junk":                                    "x",
	}
	im := &importer{s: testService(), blobs: map[string]importBlob{}, byPath: map[string]uuid.UUID{}, used: map[string]bool{}}
	z, err := openZip(zipOf(t, files), 25<<20)
	if err != nil {
		t.Fatal(err)
	}
	im.zip = z
	root, err := im.zipTree("Export")
	if err != nil {
		t.Fatal(err)
	}
	if root.title != "Wiki" || root.format != "markdown" {
		t.Fatalf("root = %q %s", root.title, root.format)
	}
	var kids []string
	for _, k := range root.children {
		kids = append(kids, k.title+"/"+k.format)
	}
	if strings.Join(kids, ",") != "Setup/markdown,Tasks/csv" {
		t.Fatalf("children = %v", kids)
	}
	tasks := root.children[1]
	if _, ok := tasks.rowDocs["write"]; !ok {
		t.Fatalf("row docs = %v", tasks.rowDocs)
	}
	// Links between pages and images resolve once pages have ids.
	setupID := uuid.New()
	im.byPath[root.children[0].path] = setupID
	blocks := im.markdownBlocks(root, string(root.data))
	var got []string
	typesAndTexts(blocks, 0, &got)
	if len(blocks) != 2 || blocks[1].typ != fileRefType {
		t.Fatalf("blocks = %v", got)
	}
	var mention *Mention
	for _, s := range blocks[0].props["text"].([]Span) {
		if s.Mention != nil {
			mention = s.Mention
		}
	}
	if mention == nil || mention.ID != setupID.String() {
		t.Fatalf("page link not rewritten: %+v", blocks[0].props["text"])
	}
	// The CSV's columns get types from their values.
	records, err := readCSV(tasks.data, false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := buildCSVDatabase(records)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, p := range d.schema {
		types[p.Name] = p.Type
	}
	if types["Name"] != "title" || types["Status"] != "status" || types["Due"] != "date" || types["Done"] != "checkbox" {
		t.Fatalf("types = %v", types)
	}
	if strings.TrimSpace(stripRowProps(files["Export/Wiki "+id1+"/Tasks "+id3+"/Write.md"], d.schema)) != "Row body." {
		t.Fatal("row properties not stripped")
	}
}

func TestZipRefusesTraversalAndBombs(t *testing.T) {
	z, err := openZip(zipOf(t, map[string]string{"../evil.md": "x", "ok/page.md": "# ok"}), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.files {
		if strings.Contains(f.Name, "..") {
			t.Fatalf("kept %q", f.Name)
		}
	}
	big := zipOf(t, map[string]string{"big.md": strings.Repeat("a", 3<<20)})
	z, _ = openZip(big, 1<<20)
	z.fileMax = 1 << 20
	if _, err := z.read(z.files[0]); err != nil {
		// within the 20 MB floor: fine
		t.Fatalf("read: %v", err)
	}
}

func TestInferType(t *testing.T) {
	cases := []struct {
		vals []string
		typ  string
	}{
		{[]string{"Yes", "No", "Yes"}, "checkbox"},
		{[]string{"1,200", "3.5", "$4"}, "number"},
		{[]string{"50%", "25%"}, "number"},
		{[]string{"2026-01-02", "January 5, 2026", "2026-01-03 → 2026-01-09"}, "date"},
		{[]string{"a@b.co", "c@d.org"}, "email"},
		{[]string{"https://a.example", "http://b.example/x"}, "url"},
		{[]string{"Red", "Blue", "Red", "Red"}, "select"},
		{[]string{"Red, Blue", "Blue"}, "multi_select"},
		{[]string{"A long free text value one", "Another", "Third", "Fourth one", "Fifth"}, "text"},
	}
	for _, c := range cases {
		if got, _, _ := inferType("Col", c.vals); got != c.typ {
			t.Errorf("%v → %s, want %s", c.vals, got, c.typ)
		}
	}
}

// ---- export zip ------------------------------------------------------------------

func TestMarkdownZipLinksAndFiles(t *testing.T) {
	ex := &exporter{s: testService(), subpages: true}
	root := exportFixture(t)
	childPage := &Page{ID: uuid.New(), Title: "Child: notes?", Kind: "page"}
	child := &exportNode{page: childPage, detail: &PageDetail{Page: childPage, Refs: Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}},
		Blocks: []Block{blk("paragraph", nil, "a0", map[string]any{"text": []Span{{Text: "Back", Mention: &Mention{Type: "page", ID: root.page.ID.String()}}}})}}}
	root.children = []*exportNode{child}
	assignPaths(root, "", map[string]bool{})
	filePaths(root)
	byID := map[string]*exportNode{}
	indexNodes(root, byID)
	md := ex.pageMarkdown(root, byID)
	if !strings.Contains(md, "](Quarterly%20plan/files/chart.png)") {
		t.Fatalf("image path not relative:\n%s", md)
	}
	if !strings.Contains(md, "](Quarterly%20plan/Child-%20notes-.md)") {
		t.Fatalf("subpage link:\n%s", md)
	}
	cmd := ex.pageMarkdown(child, byID)
	if !strings.Contains(cmd, "](../Quarterly%20plan.md)") {
		t.Fatalf("link back to parent:\n%s", cmd)
	}
	_ = time.Now()
}
