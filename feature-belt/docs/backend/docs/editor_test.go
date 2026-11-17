package docs

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// toBlocks turns parsed Markdown into stored-looking blocks (normalized
// where that needs no database), for render round trips.
func toBlocks(t *testing.T, list []*mdBlock) []Block {
	t.Helper()
	var out []Block
	var walk func(l []*mdBlock, parent *uuid.UUID)
	walk = func(l []*mdBlock, parent *uuid.UUID) {
		for _, b := range l {
			raw := map[string]json.RawMessage{}
			for k, v := range b.props {
				enc, _ := json.Marshal(v)
				raw[k] = enc
			}
			n, err := normalizeProps(propCtx{}, b.typ, raw)
			if err != nil {
				t.Fatalf("normalize %s: %v", b.typ, err)
			}
			enc, _ := json.Marshal(n.props)
			props := map[string]any{}
			_ = json.Unmarshal(enc, &props)
			id := uuid.New()
			out = append(out, Block{ID: id, ParentID: parent, Type: b.typ, Props: props})
			if holdsChildren(b.typ, props) {
				pid := id
				walk(b.children, &pid)
			}
		}
	}
	walk(list, nil)
	return out
}

func typesOf(list []*mdBlock) string {
	var out []string
	var walk func(l []*mdBlock, depth int)
	walk = func(l []*mdBlock, depth int) {
		for _, b := range l {
			out = append(out, strings.Repeat(">", depth)+b.typ)
			walk(b.children, depth+1)
		}
	}
	walk(list, 0)
	return strings.Join(out, " ")
}

// Every block type that Markdown can carry survives export → import → export.
func TestMarkdownRoundTripPerType(t *testing.T) {
	cases := map[string]string{
		"heading_4":      "#### Small heading\n",
		"toggle heading": "<details>\n<summary>## Open me</summary>\n\nInside\n</details>\n",
		"toggle":         "<details>\n<summary>A toggle</summary>\n\n- item\n</details>\n",
		"equation":       "$$\n\\int_0^1 x^2 \\, dx = \\frac{1}{3}\n$$\n",
		"columns":        "<columns>\n<column width=\"0.3\">\n\nLeft\n\n</column>\n<column>\n\n- right\n\n</column>\n</columns>\n",
		"toc":            "[TOC]\n",
		"breadcrumb":     "<!-- breadcrumb -->\n",
		"embed":          "https://www.youtube.com/watch?v=dQw4w9WgXcQ\n",
		"callout":        "> 🚧 Under construction\n",
		"code":           "```mermaid\ngraph TD; A-->B\n```\n",
		"table":          "| a | b |\n| --- | --- |\n| 1 | 2 |\n",
		"inline":         "Pay $5 now, see $E=mc^2$ and <span color=\"red\">red</span> or <span color=\"blue_background\">marked</span>.\n",
	}
	for name, md := range cases {
		t.Run(name, func(t *testing.T) {
			blocks := toBlocks(t, markdownToBlocks(md, 100))
			got := blocksToMarkdown(blocks, mdOptions{})
			if got != md {
				t.Fatalf("export\n got %q\nwant %q", got, md)
			}
			again := blocksToMarkdown(toBlocks(t, markdownToBlocks(got, 100)), mdOptions{})
			if again != got {
				t.Fatalf("not stable\n got %q\nwant %q", again, got)
			}
		})
	}
}

func TestMarkdownNewTypes(t *testing.T) {
	md := "#### H4\n\n<details>\n<summary>### Fold</summary>\n\nkid\n</details>\n\n$$E=mc^2$$\n\n<columns>\n<column>\n\na\n\n</column>\n<column>\n\nb\n\n</column>\n</columns>\n\n[TOC]\n\nhttps://www.loom.com/share/0123456789abcdef0123456789abcdef\n\nhttps://example.com/not-embeddable\n"
	got := typesOf(markdownToBlocks(md, 100))
	want := "heading_4 heading_3 >paragraph equation column_list >column >>paragraph >column >>paragraph table_of_contents embed paragraph"
	if got != want {
		t.Fatalf("types\n got %s\nwant %s", got, want)
	}
	// A single column is just its content; stray closers are ignored.
	if got := typesOf(markdownToBlocks("<columns>\n<column>\n\nonly\n\n</column>\n</columns>\n</details>\n\nafter\n", 100)); got != "paragraph paragraph" {
		t.Fatalf("single column = %s", got)
	}
}

func TestInlineEquationAndColour(t *testing.T) {
	spans := markdownToSpans("costs $5 and $10, area $\\pi r^2$ <span color=\"green\">ok</span> <span color=\"nope\">x</span>")
	var eq, green, bad int
	for _, s := range spans {
		if s.Equation {
			eq++
			if s.Text != `\pi r^2` {
				t.Fatalf("equation text %q", s.Text)
			}
		}
		if s.Color == "green" {
			green++
		}
		if s.Text == "x" && s.Color != "" {
			bad++
		}
	}
	if eq != 1 || green != 1 || bad != 0 {
		t.Fatalf("spans = %+v", spans)
	}
	if !strings.Contains(plainOf(spans), "costs $5 and $10") {
		t.Fatalf("prices became equations: %q", plainOf(spans))
	}
	// Equations never merge with neighbours and keep one line.
	out, _ := sanitizeSpans([]Span{{Text: "a", Equation: true}, {Text: "b", Equation: true}, {Text: "x\ny", Equation: true, Link: "https://x.io"}}, spanRules{})
	if len(out) != 3 || out[2].Text != "x y" || out[2].Link != "" {
		t.Fatalf("equation sanitize = %+v", out)
	}
	if c, _ := sanitizeSpans([]Span{{Text: "t", Color: "chartreuse"}}, spanRules{}); c[0].Color != "" {
		t.Fatalf("unknown colour kept")
	}
}

func TestDateReminderSanitize(t *testing.T) {
	in := []Span{{Text: "Nov 5", Mention: &Mention{Type: "date", Date: "2026-11-05", Time: "09:30", RemindAt: "2026-11-05T17:30:00Z"}},
		{Text: "bad", Mention: &Mention{Type: "date", Date: "2026-11-05", Time: "25:00", RemindAt: "tomorrow"}}}
	out, _ := sanitizeSpans(in, spanRules{})
	if out[0].Mention.Time != "09:30" || out[0].Mention.RemindAt != "2026-11-05T17:30:00Z" {
		t.Fatalf("kept = %+v", out[0].Mention)
	}
	if out[1].Mention == nil || out[1].Mention.Time != "" || out[1].Mention.RemindAt != "" {
		t.Fatalf("bad time/reminder not dropped: %+v", out[1].Mention)
	}
	blk := Block{Type: "paragraph", Props: map[string]any{"text": out}}
	if r := remindersIn(blk); len(r) != 1 {
		t.Fatalf("reminders = %v", r)
	}
}

func TestNormalizePropsNewTypes(t *testing.T) {
	norm := func(typ string, props map[string]any) (map[string]any, error) {
		raw := map[string]json.RawMessage{}
		for k, v := range props {
			enc, _ := json.Marshal(v)
			raw[k] = enc
		}
		n, err := normalizeProps(propCtx{}, typ, raw)
		return n.props, err
	}
	if p, err := norm("heading_2", map[string]any{"text": []Span{{Text: "x"}}, "toggleable": true, "color": "red_background", "junk": 1}); err != nil || p["toggleable"] != true || p["color"] != "red_background" || p["junk"] != nil {
		t.Fatalf("heading = %v %v", p, err)
	}
	if _, err := norm("paragraph", map[string]any{"color": "teal"}); err == nil {
		t.Fatal("unknown block colour accepted")
	}
	if p, _ := norm("code", map[string]any{"code": "a", "language": "mermaid", "wrap": true, "caption": []Span{{Text: "fig"}}}); p["language"] != "mermaid" || p["wrap"] != true || plainOf(spansOf(p, "caption")) != "fig" {
		t.Fatalf("code = %v", p)
	}
	if p, _ := norm("equation", map[string]any{"expression": "  a+b  "}); p["expression"] != "a+b" {
		t.Fatalf("equation = %v", p)
	}
	if _, err := norm("equation", map[string]any{"expression": strings.Repeat("x", maxEquationChars+1)}); err == nil {
		t.Fatal("long equation accepted")
	}
	if p, err := norm("embed", map[string]any{"url": "https://youtu.be/dQw4w9WgXcQ?t=1m5s", "height": 400}); err != nil || p["provider"] != "youtube" || p["src"] != "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=65" || p["height"] != 400 {
		t.Fatalf("embed = %v %v", p, err)
	}
	if _, err := norm("embed", map[string]any{"url": "https://evil.example/frame"}); err == nil {
		t.Fatal("arbitrary frame accepted")
	}
	if p, _ := norm("column", map[string]any{"width": 0.3333333}); p["width"] != 0.333 {
		t.Fatalf("column = %v", p)
	}
	if p, _ := norm("column", map[string]any{"width": 7}); p["width"] != nil {
		t.Fatalf("bad column width kept: %v", p)
	}
	if p, err := norm("button", map[string]any{"label": "Add task", "actions": []any{
		map[string]any{"type": "insert", "blocks": []any{map[string]any{"type": "to_do", "props": map[string]any{"text": []Span{{Text: "New"}}}, "children": []any{}}}},
		map[string]any{"type": "link", "url": "https://example.com"},
	}}); err != nil || p["label"] != "Add task" {
		t.Fatalf("button = %v %v", p, err)
	} else if acts := p["actions"].([]BlockButtonAction); len(acts) != 2 || acts[0].Blocks[0].Props["checked"] != false {
		t.Fatalf("button actions = %+v", p["actions"])
	}
	if _, err := norm("button", map[string]any{"actions": []any{map[string]any{"type": "insert", "blocks": []any{map[string]any{"type": "image", "props": map[string]any{}}}}}}); err == nil {
		t.Fatal("button inserting an image accepted")
	}
	if _, err := norm("button", map[string]any{"actions": []any{map[string]any{"type": "link", "url": "javascript:alert(1)"}}}); err == nil {
		t.Fatal("button with a script link accepted")
	}
	if p, _ := norm("bookmark", map[string]any{"url": "https://example.com", "title": "T", "description": "D", "site_name": "Ex", "preview_id": "nope"}); p["description"] != "D" || p["site_name"] != "Ex" || p["preview_id"] != nil {
		t.Fatalf("bookmark = %v", p)
	}
}

func TestHoldsChildren(t *testing.T) {
	for _, c := range []struct {
		typ   string
		props map[string]any
		want  bool
	}{
		{"heading_1", nil, false}, {"heading_1", map[string]any{"toggleable": true}, true}, {"column_list", nil, true}, {"column", nil, true},
		{"synced_block", nil, true}, {"synced_block", map[string]any{"source_id": uuid.NewString()}, false}, {"equation", nil, false}, {"embed", nil, false},
	} {
		if got := holdsChildren(c.typ, c.props); got != c.want {
			t.Errorf("holdsChildren(%s, %v) = %v", c.typ, c.props, got)
		}
	}
}

func TestEmbedFor(t *testing.T) {
	ok := map[string]string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":                          "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://youtube.com/shorts/dQw4w9WgXcQ":                               "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"http://youtu.be/dQw4w9WgXcQ":                                          "https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://vimeo.com/76979871":                                           "https://player.vimeo.com/video/76979871?dnt=1",
		"https://www.loom.com/share/0123456789abcdef0123456789abcdef":          "https://www.loom.com/embed/0123456789abcdef0123456789abcdef",
		"https://www.figma.com/design/AbCdEfGhIjKl/Name?node-id=1":             "https://www.figma.com/embed?embed_host=share&url=https%3A%2F%2Fwww.figma.com%2Fdesign%2FAbCdEfGhIjKl%2FName%3Fnode-id%3D1",
		"https://miro.com/app/board/uXjVOabcdEF=/":                             "https://miro.com/app/live-embed/uXjVOabcdEF=/",
		"https://docs.google.com/document/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/edit":  "https://docs.google.com/document/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/preview",
		"https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/":  "https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/preview",
		"https://docs.google.com/presentation/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/e": "https://docs.google.com/presentation/d/1AbCdEfGhIjKlMnOpQrStUvWxYz/embed",
		"https://www.google.com/maps/place/Eiffel+Tower/@48.85,2.29,17z":       "https://maps.google.com/maps?output=embed&q=Eiffel+Tower",
		"https://codepen.io/team/pen/abcXYZ":                                   "https://codepen.io/team/embed/abcXYZ?default-tab=result",
		"https://gist.github.com/octocat/6cad326836d38bd3a7ae":                 "https://gist.github.com/octocat/6cad326836d38bd3a7ae.pibb",
		"https://x.com/someone/status/1234567890123":                           "https://platform.twitter.com/embed/Tweet.html?dnt=true&id=1234567890123",
		"https://open.spotify.com/track/4uLU6hMCjMI75M1A2tKUQC":                "https://open.spotify.com/embed/track/4uLU6hMCjMI75M1A2tKUQC",
		"https://soundcloud.com/artist/track-name":                             "https://w.soundcloud.com/player/?visual=false&url=https%3A%2F%2Fsoundcloud.com%2Fartist%2Ftrack-name",
		"https://acme.typeform.com/to/AbC123":                                  "https://form.typeform.com/to/AbC123",
		"https://airtable.com/appAbCdEfGhIjKlMn/shrAbCdEfGhIjKlMn":             "https://airtable.com/embed/appAbCdEfGhIjKlMn/shrAbCdEfGhIjKlMn",
		"https://calendly.com/sam/30min":                                       "https://calendly.com/sam/30min?embed_type=Inline&hide_gdpr_banner=1",
		"https://replit.com/@sam/my-repl":                                      "https://replit.com/@sam/my-repl?embed=true",
		"https://lucid.app/lucidchart/0f8fad5b-d9cb-469f-a165-70867728950e/":   "https://lucid.app/documents/embedded/0f8fad5b-d9cb-469f-a165-70867728950e",
		"https://whimsical.com/flow-AbC123":                                    "https://whimsical.com/embed/flow-AbC123",
	}
	for in, want := range ok {
		e, matched := embedFor(in)
		if !matched || e.Src != want {
			t.Errorf("embedFor(%q) = %q %v, want %q", in, e.Src, matched, want)
		}
	}
	for _, in := range []string{
		"https://example.com/watch?v=dQw4w9WgXcQ", "https://youtube.com.evil.io/watch?v=dQw4w9WgXcQ", "https://evil.io/?u=https://youtu.be/dQw4w9WgXcQ",
		"javascript:alert(1)", "https://youtube.com:8443/watch?v=dQw4w9WgXcQ", "https://user@youtube.com/watch?v=dQw4w9WgXcQ", "https://www.youtube.com/watch?v=short",
		"https://docs.google.com/document/u/0/", "https://framer.com.evil.io/embed/x",
	} {
		if e, matched := embedFor(in); matched {
			t.Errorf("embedFor(%q) matched %+v", in, e)
		}
	}
}

func TestSubtree(t *testing.T) {
	root, a, b, c, other := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	all := []Block{{ID: root}, {ID: a, ParentID: &root}, {ID: b, ParentID: &a}, {ID: c, ParentID: &root}, {ID: other}}
	got := subtree(all, root)
	if len(got) != 3 || got[0].ID != a || got[0].ParentID != nil || got[1].ParentID == nil || *got[1].ParentID != a || got[2].ParentID != nil {
		t.Fatalf("subtree = %+v", got)
	}
}

func TestClassifyUpload(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	pdf := []byte("%PDF-1.7\n%\xe2\xe3")
	html := []byte("<!doctype html><html><script>alert(1)</script>")
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	for _, c := range []struct {
		declared, name string
		head           []byte
		want           string
		bad            bool
	}{
		{"image/png", "a.png", png, "image/png", false},
		{"", "a.png", png, "image/png", false},
		{"image/png", "evil.png", html, "", true},
		{"application/pdf", "doc.pdf", pdf, "application/pdf", false},
		{"application/pdf", "doc.pdf", html, "", true},
		{"image/svg+xml", "logo.svg", svg, "image/svg+xml", false},
		{"image/svg+xml", "logo.svg", png, "", true},
		{"", "notes.md", []byte("# Hi\n"), "text/markdown", false},
		{"text/plain", "x.txt", []byte{0, 1, 2}, "", true},
		{"application/octet-stream", "data.bin", []byte{0, 1, 2}, "application/octet-stream", false},
		{"video/mp4", "clip.mp4", html, "", true},
		{"audio/flac", "song.flac", []byte("fLaC\x00\x00\x00\x22"), "audio/flac", false},
		{"text/html", "page.html", html, "text/html", false},
	} {
		got, err := classifyUpload(c.declared, c.name, c.head)
		if (err != nil) != c.bad || got != c.want {
			t.Errorf("classify(%q, %q) = %q, %v", c.declared, c.name, got, err)
		}
	}
	for mt, want := range map[string]string{
		"image/png":       "image/png|inline",
		"image/svg+xml":   "image/svg+xml|attachment",
		"text/html":       "application/octet-stream|attachment",
		"text/markdown":   "text/plain; charset=utf-8|inline",
		"application/pdf": "application/pdf|inline",
		"application/zip": "application/zip|attachment",
		"video/webm":      "video/webm|inline",
	} {
		ct, cd := servePolicy(mt, "f")
		if got := ct + "|" + strings.SplitN(cd, ";", 2)[0]; got != want {
			t.Errorf("servePolicy(%s) = %s, want %s", mt, got, want)
		}
	}
}

func TestParseMeta(t *testing.T) {
	base, _ := url.Parse("https://www.example.com/post/1")
	doc := `<html><head><title>Fallback &amp; title</title>
		<meta property="og:title" content="The &quot;Real&quot; Title">
		<meta name="description" content="A   description
		over lines">
		<meta property="og:image" content="/img/card.png">
		<link rel="icon" href="/favicon.svg" type="image/svg+xml">
		<link rel="apple-touch-icon" href="https://cdn.example.com/touch.png">
		</head><body><meta property="og:title" content="ignored"></body></html>`
	m := parseMeta(doc, base)
	if m.title != `The "Real" Title` || m.desc != "A description over lines" || m.site != "example.com" ||
		m.image != "https://www.example.com/img/card.png" || m.icon != "https://cdn.example.com/touch.png" {
		t.Fatalf("meta = %+v", m)
	}
	if m := parseMeta(`<title>Only</title><meta property="og:image" content="javascript:alert(1)">`, base); m.title != "Only" || m.image != "" {
		t.Fatalf("fallback = %+v", m)
	}
}

func notionBlock(t *testing.T, raw string) *nBlock {
	t.Helper()
	var b nBlock
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}
	return &b
}

func TestNotionNativeBlocks(t *testing.T) {
	refs := &Refs{Users: map[string]RefUser{}, Pages: map[string]RefPage{}}
	typ, props, ro := convertNotionBlock(notionBlock(t, `{"id":"b1","type":"heading_2","heading_2":{"rich_text":[{"type":"text","text":{"content":"Hi"},"plain_text":"Hi","annotations":{"color":"red"}}],"is_toggleable":true,"color":"blue_background"}}`), refs)
	if typ != "heading_2" || ro || props["toggleable"] != true || props["color"] != "blue_background" || spansValue(props["text"])[0].Color != "red" {
		t.Fatalf("heading = %s %v %v", typ, props, ro)
	}
	if typ, props, _ := convertNotionBlock(notionBlock(t, `{"id":"b2","type":"equation","equation":{"expression":"e^{i\\pi}+1=0"}}`), refs); typ != "equation" || props["expression"] != `e^{i\pi}+1=0` {
		t.Fatalf("equation = %s %v", typ, props)
	}
	if typ, props, _ := convertNotionBlock(notionBlock(t, `{"id":"b3","type":"video","video":{"type":"external","external":{"url":"https://youtu.be/dQw4w9WgXcQ"}}}`), refs); typ != "embed" || props["provider"] != "youtube" {
		t.Fatalf("video link = %s %v", typ, props)
	}
	if typ, _, _ := convertNotionBlock(notionBlock(t, `{"id":"b4","type":"pdf","pdf":{"type":"file","file":{"url":"https://s3.example/x.pdf"}}}`), refs); typ != "pdf" {
		t.Fatalf("pdf = %s", typ)
	}
	if typ, _, _ := convertNotionBlock(notionBlock(t, `{"id":"b5","type":"embed","embed":{"url":"https://evil.example/frame"}}`), refs); typ != "bookmark" {
		t.Fatalf("unknown embed = %s", typ)
	}
	// Inline equations and colours go back to Notion as its own.
	rt, err := notionRich([]Span{{Text: "x^2", Equation: true}, {Text: " red", Color: "red"}}, nil)
	if err != nil || rt[0]["type"] != "equation" || rt[1]["annotations"].(map[string]any)["color"] != "red" {
		t.Fatalf("rich = %v %+v", err, rt)
	}
	nt, body, err := notionPayload("heading_1", map[string]any{"text": []Span{{Text: "T"}}, "toggleable": true, "color": "green"}, nil)
	if err != nil || nt != "heading_1" || body["is_toggleable"] != true || body["color"] != "green" {
		t.Fatalf("payload = %s %v %v", nt, body, err)
	}
}
