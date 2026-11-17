package files

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/image/draw"
)

// ---- fixtures built in memory ----------------------------------------------------------

func zipOf(t *testing.T, parts map[string]string, bin map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	for name, body := range bin {
		w, _ := zw.Create(name)
		_, _ = w.Write(body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const relNS = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

func docxFixture(t *testing.T) []byte {
	return zipOf(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
			<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Quarterly plan</w:t></w:r></w:p>
			<w:p><w:r><w:t xml:space="preserve">Revenue grew </w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>twelve</w:t></w:r><w:r><w:t xml:space="preserve"> percent.</w:t></w:r></w:p>
			<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Owner</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Sam</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
			</w:body></w:document>`,
		"word/footnotes.xml": `<w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:footnote><w:p><w:r><w:t>See appendix</w:t></w:r></w:p></w:footnote></w:footnotes>`,
	}, nil)
}

func xlsxFixture(t *testing.T) []byte {
	return zipOf(t, map[string]string{
		"xl/workbook.xml": `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` + relNS + `><sheets>
			<sheet name="Budget" sheetId="1" r:id="rId1"/><sheet name="Team" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			<Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="/xl/worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Hosting</t></si><si><r><t>Design</t></r><r><t xml:space="preserve"> work</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
			<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"><v>1200.5</v></c></row>
			<row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2"><f>B1*2</f><v>2401</v></c></row>
			<row r="3"><c r="A3" t="inlineStr"><is><t>Total</t></is></c><c r="B3" t="b"><v>1</v></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
			<row r="1"><c r="A1" t="inlineStr"><is><t>Priya</t></is></c></row></sheetData></worksheet>`,
	}, nil)
}

func pptxFixture(t *testing.T) []byte {
	slide := func(title, body string) string {
		return `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree>
			<p:sp><p:txBody><a:p><a:r><a:t>` + title + `</a:t></a:r></a:p></p:txBody></p:sp>
			<p:sp><p:txBody><a:p><a:r><a:t>` + body + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	return zipOf(t, map[string]string{
		"ppt/presentation.xml": `<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` + relNS + `><p:sldIdLst>
			<p:sldId id="257" r:id="rId3"/><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			<Relationship Id="rId2" Target="slides/slide1.xml"/><Relationship Id="rId3" Target="slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml": slide("Roadmap", "Thumbnails first"),
		"ppt/slides/slide2.xml": slide("Agenda", "Kickoff"),
	}, nil)
}

func odtFixture(t *testing.T, thumb []byte) []byte {
	return zipOf(t, map[string]string{
		"content.xml": `<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"><office:body><office:text>
			<text:h text:outline-level="1">Charter</text:h><text:p>Scope<text:s text:c="3"/>and<text:tab/>goals</text:p>
			<office:annotation><text:p>private note</text:p></office:annotation></office:text></office:body></office:document-content>`,
	}, map[string][]byte{"Thumbnails/thumbnail.png": thumb})
}

// pdfFixture writes a minimal two-page PDF with Helvetica text.
func pdfFixture(pages ...string) []byte {
	var objs []string
	add := func(s string) int { objs = append(objs, s); return len(objs) }
	font := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	pagesID := len(objs) + 1 + 2*len(pages)
	var kids []string
	for _, text := range pages {
		stream := "BT /F1 24 Tf 72 720 Td (" + text + ") Tj ET"
		c := add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
		p := add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", pagesID, c, font))
		kids = append(kids, fmt.Sprintf("%d 0 R", p))
	}
	add(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))
	cat := add(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesID))
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&out, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, cat, x)
	return out.Bytes()
}

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// ---- extraction -----------------------------------------------------------------------

func TestExtractText(t *testing.T) {
	thumb := encodePNG(t, solid(8, 8, color.White))
	cases := []struct {
		name, mime string
		data       []byte
		want       []string
		not        []string
	}{
		{"plan.docx", "", docxFixture(t), []string{"Quarterly plan", "Revenue grew twelve percent.", "Owner\tSam", "See appendix"}, nil},
		{"budget.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", xlsxFixture(t),
			[]string{"Budget", "Hosting\t1200.5", "Design work\t2401", "Total\t1", "Team", "Priya"}, nil},
		{"deck.pptx", "", pptxFixture(t), []string{"Slide 1\nAgenda", "Slide 2\nRoadmap", "Thumbnails first"}, nil},
		{"charter.odt", "application/vnd.oasis.opendocument.text", odtFixture(t, thumb), []string{"Charter", "Scope and\tgoals"}, []string{"private note"}},
		{"report.pdf", "application/pdf", pdfFixture("Quarterly report grew", "Page two hiring"), []string{"Quarterly report grew", "Page two hiring"}, nil},
	}
	for _, c := range cases {
		got, err := extractText(c.name, c.mime, c.data)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in %q", c.name, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("%s: should not contain %q", c.name, n)
			}
		}
	}
	// Order follows the presentation's slide list, not file names.
	got, _ := extractText("deck.pptx", "", pptxFixture(t))
	if strings.Index(got, "Agenda") > strings.Index(got, "Roadmap") {
		t.Errorf("slides out of order: %q", got)
	}
}

func TestExtractRefusesJunk(t *testing.T) {
	for _, c := range []struct{ name, mime string }{{"x.docx", ""}, {"x.pdf", "application/pdf"}, {"x.zip", "application/zip"}, {"x.png", "image/png"}} {
		if _, err := extractText(c.name, c.mime, []byte("not a document")); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}
	if _, err := extractText("empty.docx", "", zipOf(t, map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body/></w:document>`}, nil)); err != errNoText {
		t.Errorf("empty document: got %v, want errNoText", err)
	}
}

func TestTidyText(t *testing.T) {
	got := tidyText("  a    b  \n\n\n\nc\r\n  \n d ")
	if got != "a b\n\nc\n\n d" {
		t.Errorf("got %q", got)
	}
	long := tidyText(strings.Repeat("é", maxExtractedText))
	if len(long) > maxExtractedText || !strings.HasPrefix(long, "é") || strings.ContainsRune(long, '�') {
		t.Errorf("cut badly: %d bytes", len(long))
	}
}

func TestZipBombCapped(t *testing.T) {
	// A part bigger than maxZipPart is read only up to the cap.
	huge := strings.Repeat("<w:p><w:r><w:t>x</w:t></w:r></w:p>", (maxZipPart/34)+1000)
	data := zipOf(t, map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:body>` + huge + `</w:body></w:document>`}, nil)
	got, err := extractText("bomb.docx", "", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > maxExtractedText {
		t.Errorf("extracted %d bytes, cap %d", len(got), maxExtractedText)
	}
}

// ---- thumbnails ------------------------------------------------------------------------

// A 1×1 lossless WebP.
const webp1x1 = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func TestMakeThumb(t *testing.T) {
	var jp bytes.Buffer
	_ = jpeg.Encode(&jp, solid(2000, 1000, color.RGBA{200, 10, 10, 255}), nil)
	var gf bytes.Buffer
	_ = gif.Encode(&gf, solid(100, 300, color.Black), nil)
	webp, _ := base64.StdEncoding.DecodeString(webp1x1)
	alpha := encodePNG(t, solid(50, 50, color.RGBA{0, 0, 0, 0}))
	cases := []struct {
		name   string
		src    []byte
		w, h   int
		mime   string
	}{
		{"jpeg landscape", jp.Bytes(), 640, 320, "image/jpeg"},
		{"gif portrait", gf.Bytes(), 100, 300, "image/jpeg"},
		{"webp", webp, 1, 1, ""},
		{"png alpha", alpha, 50, 50, "image/png"},
	}
	for _, c := range cases {
		out, ct, err := makeThumb(c.src)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if c.mime != "" && ct != c.mime {
			t.Errorf("%s: type %s, want %s", c.name, ct, c.mime)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
		if err != nil {
			t.Errorf("%s: output doesn't decode: %v", c.name, err)
			continue
		}
		if cfg.Width != c.w || cfg.Height != c.h {
			t.Errorf("%s: %dx%d, want %dx%d", c.name, cfg.Width, cfg.Height, c.w, c.h)
		}
	}
	if _, _, err := makeThumb([]byte("<svg onload=alert(1)>")); err != errNotImage {
		t.Errorf("svg: got %v, want errNotImage", err)
	}
	// A header claiming 100000×100000 pixels is refused before decoding.
	var big bytes.Buffer
	_ = png.Encode(&big, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := big.Bytes()
	copy(b[16:24], []byte{0, 1, 0x86, 0xa0, 0, 1, 0x86, 0xa0})
	if _, _, err := makeThumb(b); err != errNotImage {
		t.Errorf("pixel bomb: got %v, want errNotImage", err)
	}
}

func TestEmbeddedThumbnail(t *testing.T) {
	thumb := encodePNG(t, solid(4, 4, color.White))
	if got := embeddedThumbnail(odtFixture(t, thumb)); !bytes.Equal(got, thumb) {
		t.Error("odt thumbnail not found")
	}
	if got := embeddedThumbnail(docxFixture(t)); got != nil {
		t.Error("docx without a thumbnail returned one")
	}
	withThumb := zipOf(t, map[string]string{"word/document.xml": "<x/>"}, map[string][]byte{"docProps/thumbnail.jpeg": []byte("jpeg")})
	if got := embeddedThumbnail(withThumb); string(got) != "jpeg" {
		t.Errorf("docProps thumbnail: %q", got)
	}
}

// ---- locks, requests, comments, search ------------------------------------------------

func TestLockedForOthers(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	it := &Item{}
	if lockedForOthers(it, me) != nil {
		t.Error("unlocked item refused")
	}
	it.Lock = &LockInfo{By: &Person{ID: me, Name: "Sam"}, At: time.Now()}
	if lockedForOthers(it, me) != nil {
		t.Error("my own lock refused me")
	}
	err := lockedForOthers(it, other)
	if err == nil || !strings.Contains(err.Error(), "item_locked") || !strings.Contains(err.Error(), "Sam checked this file out") {
		t.Errorf("other person: %v", err)
	}
	it.Lock.By = nil // the locker's account was deleted
	if lockedForOthers(it, me) == nil {
		t.Error("an orphaned lock should still refuse")
	}
}

func TestRequestOpen(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	two := 2
	by := &Person{ID: uuid.New()}
	cases := []struct {
		name string
		fr   FileRequest
		want bool
	}{
		{"open", FileRequest{CreatedBy: by}, true},
		{"expired", FileRequest{CreatedBy: by, ExpiresAt: &past}, false},
		{"not yet expired", FileRequest{CreatedBy: by, ExpiresAt: &future}, true},
		{"closed", FileRequest{CreatedBy: by, ClosedAt: &past}, false},
		{"under the file limit", FileRequest{CreatedBy: by, MaxFiles: &two, FilesReceived: 1}, true},
		{"at the file limit", FileRequest{CreatedBy: by, MaxFiles: &two, FilesReceived: 2}, false},
		{"creator deleted", FileRequest{}, false},
	}
	for _, c := range cases {
		if got := requestOpen(&c.fr, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRequestFileLimit(t *testing.T) {
	s := &Service{cfg: settings{maxUpload: 250 << 20}}
	small := int64(10 << 20)
	huge := int64(1 << 40)
	if got := s.requestFileLimit(&FileRequest{}); got != 250<<20 {
		t.Errorf("default: %d", got)
	}
	if got := s.requestFileLimit(&FileRequest{MaxFileBytes: &small}); got != small {
		t.Errorf("smaller limit: %d", got)
	}
	if got := s.requestFileLimit(&FileRequest{MaxFileBytes: &huge}); got != 250<<20 {
		t.Errorf("limit above the server's: %d", got)
	}
}

func TestValidUploader(t *testing.T) {
	if _, _, err := validUploader(" Ana ", "ana@example.com", true); err != nil {
		t.Error(err)
	}
	for _, c := range []struct{ name, email string }{{"", "a@b.co"}, {"Ana", ""}, {"Ana", "not an email"}, {"Ana", "Ana <a@b.co>"}, {strings.Repeat("x", 101), "a@b.co"}} {
		if _, _, err := validUploader(c.name, c.email, true); err == nil {
			t.Errorf("%q %q: want an error", c.name, c.email)
		}
	}
	if _, _, err := validUploader("Ana", "", false); err != nil {
		t.Errorf("email optional: %v", err)
	}
}

func TestRequestToken(t *testing.T) {
	tok, hash := newRequestToken()
	if !strings.HasPrefix(tok, requestPrefix) || len(tok) != len(requestPrefix)+43 || !bytes.Equal(hash, hashRequestToken(tok)) {
		t.Errorf("token %q", tok)
	}
}

func TestCommentAnchor(t *testing.T) {
	p, bad, s, neg := 3, 0, 12.5, -1.0
	for _, c := range []struct {
		a    *CommentAnchor
		want bool
	}{
		{nil, true}, {&CommentAnchor{Page: &p}, true}, {&CommentAnchor{Seconds: &s}, true},
		{&CommentAnchor{}, false}, {&CommentAnchor{Page: &p, Seconds: &s}, false}, {&CommentAnchor{Page: &bad}, false}, {&CommentAnchor{Seconds: &neg}, false},
	} {
		if got := c.a.valid(); got != c.want {
			t.Errorf("%+v: got %v", c.a, got)
		}
	}
}

func TestSearchFilters(t *testing.T) {
	args := []any{uuid.New()}
	after := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	folder := uuid.New()
	sql, err := SearchFilters{Kind: "spreadsheet", Owner: "me", ModifiedAfter: &after, Location: folder.String(), Starred: true}.where(&args)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"i.mime_type = ANY($2)", "COALESCE(i.owner_id, i.created_by) = $1", "i.updated_at >= $3", "parent_id = $4", "files_stars"} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in %s", want, sql)
		}
	}
	if len(args) != 4 || args[3] != folder {
		t.Errorf("args %v", args)
	}
	for _, f := range []SearchFilters{{Kind: "nope"}, {Owner: "someone"}, {Location: "../etc"}} {
		if _, err := f.where(&[]any{uuid.New()}); err == nil {
			t.Errorf("%+v: want an error", f)
		}
	}
	if sql, _ := (SearchFilters{}).where(&[]any{}); sql != "" || !(SearchFilters{}).empty() {
		t.Error("empty filters should add nothing")
	}
	f, err := parseSearchFilters(params{"modified_after": "2026-03-01", "starred": "1", "kind": "pdf"}.get)
	if err != nil || f.ModifiedAfter == nil || !f.ModifiedAfter.Equal(after) || !f.Starred || f.Kind != "pdf" {
		t.Errorf("parsed %+v, %v", f, err)
	}
	if _, err := parseSearchFilters(params{"modified_before": "yesterday"}.get); err == nil {
		t.Error("bad date accepted")
	}
}

type params map[string]string

func (p params) get(k string) string { return p[k] }

func TestExtractFormat(t *testing.T) {
	for in, want := range map[string]string{
		"a.pdf|": "pdf", "a.docx|": "docx", "x|application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx",
		"a.pptx|": "pptx", "a.ods|": "odf", "a.doc|application/msword": "", "a.txt|text/plain": "",
	} {
		p := strings.SplitN(in, "|", 2)
		if got := extractFormat(p[0], p[1]); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}
