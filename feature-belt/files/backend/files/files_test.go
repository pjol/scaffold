package files

import (
	"strings"
	"testing"
)

var pngHead = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x10")

func TestClassify(t *testing.T) {
	cases := []struct {
		name, declared, file string
		head                 []byte
		want                 string
		text, mismatch       bool
	}{
		{"png ok", "image/png", "a.png", pngHead, "image/png", false, false},
		{"html pretending to be png", "image/png", "a.png", []byte("<html><script>alert(1)</script></html>"), "", false, true},
		{"text pretending to be pdf", "application/pdf", "a.pdf", []byte("hello"), "", false, true},
		{"svg is text", "image/svg+xml", "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "image/svg+xml", true, false},
		{"empty type from extension", "", "notes.md", []byte("# Hi"), "text/markdown", true, false},
		{"ts reported as video", "video/mp2t", "app.ts", []byte("export const x = 1;\n"), "text/typescript", true, false},
		{"unknown binary", "", "blob.dat", []byte{0, 1, 2, 3, 0xff}, "application/octet-stream", false, false},
		{"unknown text", "application/octet-stream", "README", []byte("plain words"), "text/plain", true, false},
		{"html stays html", "text/html", "page.html", []byte("<!doctype html><p>x</p>"), "text/html", true, false},
		{"heic unknown to the sniffer", "image/heic", "photo.heic", []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, "image/heic", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mt, text, err := classify(c.declared, c.file, c.head)
			if c.mismatch {
				if err == nil {
					t.Fatalf("want a mismatch, got %q", mt)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mt != c.want || text != c.text {
				t.Fatalf("got (%q, %v), want (%q, %v)", mt, text, c.want, c.text)
			}
		})
	}
}

func TestServePolicy(t *testing.T) {
	cases := []struct {
		mt, name string
		text     bool
		inline   bool
		wantCT   string
		wantDisp string
	}{
		{"image/svg+xml", "logo.svg", true, true, "application/octet-stream", "attachment"},
		{"image/svg+xml", "logo.svg", true, false, "application/octet-stream", "attachment"},
		{"text/html", "page.html", true, true, "application/octet-stream", "attachment"},
		{"application/octet-stream", "x.bin", false, true, "application/octet-stream", "attachment"},
		{"image/png", "a.png", false, true, "image/png", "inline"},
		{"image/png", "a.png", false, false, "image/png", "attachment"},
		{"application/pdf", "a.pdf", false, true, "application/pdf", "inline"},
		{"text/markdown", "a.md", true, true, "text/plain; charset=utf-8", "inline"},
		{"text/markdown", "a.md", true, false, "application/octet-stream", "attachment"},
		{"text/csv", "a.csv", true, false, "text/csv", "attachment"},
		{"text/javascript", "a.js", true, true, "application/octet-stream", "attachment"},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "a.docx", false, true,
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "attachment"},
	}
	for _, c := range cases {
		ct, cd := servePolicy(c.mt, c.name, c.text, c.inline)
		if ct != c.wantCT || !strings.HasPrefix(cd, c.wantDisp) {
			t.Errorf("%s inline=%v: got (%q, %q), want (%q, %s…)", c.name, c.inline, ct, cd, c.wantCT, c.wantDisp)
		}
		if !strings.Contains(cd, "filename") {
			t.Errorf("%s: disposition has no filename: %q", c.name, cd)
		}
	}
	if _, cd := servePolicy("text/plain", "Plan — draft.txt", true, false); !strings.Contains(cd, "filename*=utf-8''Plan%20%E2%80%94%20draft.txt") {
		t.Errorf("non-ASCII filename not encoded: %q", cd)
	}
}

func TestPreviewKind(t *testing.T) {
	cases := map[string]string{
		"image/png|a.png|0":          "image",
		"application/pdf|a.pdf|0":    "pdf",
		"video/mp4|a.mp4|0":          "video",
		"audio/mpeg|a.mp3|0":         "audio",
		"text/markdown|a.md|1":       "markdown",
		"text/csv|a.csv|1":           "csv",
		"text/plain|a.txt|1":         "text",
		"text/x-go|main.go|1":        "code",
		"image/svg+xml|a.svg|1":      "svg",
		"image/heic|a.heic|0":        "other",
		"application/zip|a.zip|0":    "other",
		"application/msword|a.doc|0": "office",
	}
	for in, want := range cases {
		p := strings.Split(in, "|")
		if got := previewKind(TypeFile, p[0], p[1], p[2] == "1"); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
	if previewKind("repo", "", "x", false) != "repo" || previewKind(TypeFolder, "", "x", false) != "folder" {
		t.Error("non-file types preview as their type")
	}
}

func TestReplaceLines(t *testing.T) {
	doc := parseText([]byte("one\ntwo\nthree\nfour\n"))
	out, before, after, err := doc.replaceLines(2, 3, "TWO\nTHREE\nextra\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(before, "|") != "two|three" || strings.Join(after, "|") != "TWO|THREE|extra" {
		t.Fatalf("before %q after %q", before, after)
	}
	if got := string(out.bytes()); got != "one\nTWO\nTHREE\nextra\nfour\n" {
		t.Fatalf("got %q", got)
	}

	// Insert before line 1, append after the last line, delete a range.
	if out, _, _, _ = doc.replaceLines(1, 0, "zero"); string(out.bytes()) != "zero\none\ntwo\nthree\nfour\n" {
		t.Fatalf("insert: %q", out.bytes())
	}
	if out, _, _, _ = doc.replaceLines(5, 4, "five"); string(out.bytes()) != "one\ntwo\nthree\nfour\nfive\n" {
		t.Fatalf("append: %q", out.bytes())
	}
	if out, before, _, _ = doc.replaceLines(2, 3, ""); string(out.bytes()) != "one\nfour\n" || len(before) != 2 {
		t.Fatalf("delete: %q", out.bytes())
	}

	// CRLF files keep their line endings; a missing final newline stays missing.
	crlf := parseText([]byte("a\r\nb\r\nc"))
	if out, _, _, _ = crlf.replaceLines(2, 2, "B"); string(out.bytes()) != "a\r\nB\r\nc" {
		t.Fatalf("crlf: %q", out.bytes())
	}

	// Out of range.
	if _, _, _, err = doc.replaceLines(0, 1, "x"); err == nil {
		t.Fatal("start 0 accepted")
	}
	if _, _, _, err = doc.replaceLines(2, 9, "x"); err == nil {
		t.Fatal("end past the file accepted")
	}
	if _, _, _, err = doc.replaceLines(3, 1, "x"); err == nil {
		t.Fatal("end before start-1 accepted")
	}

	// Empty file.
	empty := parseText(nil)
	if out, _, _, _ = empty.replaceLines(1, 0, "first"); string(out.bytes()) != "first\n" {
		t.Fatalf("empty: %q", out.bytes())
	}
}

func TestCleanName(t *testing.T) {
	for _, bad := range []string{"", "  ", ".", "..", "a/b", `a\b`, "line\nbreak", strings.Repeat("x", 256)} {
		if _, err := cleanName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if n, err := cleanName("  Q3 plan.pdf "); err != nil || n != "Q3 plan.pdf" {
		t.Errorf("got %q, %v", n, err)
	}
	if got := numberedName("report.pdf", 2, true); got != "report (2).pdf" {
		t.Errorf("got %q", got)
	}
	if got := numberedName("Photos", 1, false); got != "Photos (1)" {
		t.Errorf("got %q", got)
	}
	if got := numberedName(".env", 1, true); got != ".env (1)" {
		t.Errorf("got %q", got)
	}
}

func TestLooksText(t *testing.T) {
	if !looksText([]byte("héllo")) || looksText([]byte{'a', 0, 'b'}) || looksText([]byte{0xff, 0xfe, 'a', 'b', 'c'}) {
		t.Error("basic text detection")
	}
	// A rune cut at the end of the sample is still text.
	if !looksText([]byte("caf\xc3")) {
		t.Error("cut rune rejected")
	}
}
