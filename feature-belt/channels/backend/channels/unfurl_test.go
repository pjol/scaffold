package channels

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExtractLinks(t *testing.T) {
	body := "See https://example.com/a, and (https://en.wikipedia.org/wiki/Rust_(programming_language)).\n" +
		"[docs](https://docs.example.com/x) `https://in-code.example` and\n```\nhttps://in-block.example\n```\n" +
		"dup https://example.com/a again; also http://plain.example/p?q=1! and ftp://nope.example"
	got := extractLinks(body, 10)
	want := []string{
		"https://example.com/a",
		"https://en.wikipedia.org/wiki/Rust_(programming_language)",
		"https://docs.example.com/x",
		"http://plain.example/p?q=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractLinks = %q\nwant %q", got, want)
	}
	if got := extractLinks(body, 2); len(got) != 2 {
		t.Fatalf("limit: got %d", len(got))
	}
	if got := extractLinks("https://user:pass@example.com/x", 3); len(got) != 0 {
		t.Fatalf("credentials in URL accepted: %q", got)
	}
}

func TestNormalizeLink(t *testing.T) {
	a, ha, ok := normalizeLink("HTTPS://Example.COM:443/Path?x=1#frag")
	if !ok || a != "https://example.com/Path?x=1" {
		t.Fatalf("normalize = %q %v", a, ok)
	}
	b, hb, _ := normalizeLink("https://example.com/Path?x=1")
	if a != b || ha != hb {
		t.Fatal("same link, different keys")
	}
	if _, _, ok := normalizeLink("javascript:alert(1)"); ok {
		t.Fatal("javascript: accepted")
	}
	if n, _, _ := normalizeLink("https://example.com"); n != "https://example.com/" {
		t.Fatalf("empty path = %q", n)
	}
}

func TestParsePageMetaOpenGraph(t *testing.T) {
	base, _ := url.Parse("https://news.example.com/articles/42")
	m := parsePageMeta(fixture(t, "og_article.html"), base)
	if m.Title != `Climbing gyms add "bouldering" nights` {
		t.Errorf("title = %q", m.Title)
	}
	if m.Description != "Local gyms are opening late for members." {
		t.Errorf("description = %q", m.Description)
	}
	if m.SiteName != "Example News" {
		t.Errorf("site = %q", m.SiteName)
	}
	if m.Image != "https://news.example.com/images/hero.jpg" {
		t.Errorf("image = %q", m.Image)
	}
	if m.Icon != "https://cdn.example.com/icon-32.png" {
		t.Errorf("icon = %q (PNG icons win)", m.Icon)
	}
	if m.OEmbed != "https://news.example.com/oembed?url=x" {
		t.Errorf("oembed = %q", m.OEmbed)
	}
}

func TestParsePageMetaFallbacks(t *testing.T) {
	base, _ := url.Parse("https://blog.example.org/post")
	m := parsePageMeta(fixture(t, "twitter_only.html"), base)
	if m.Title != "Release notes 4.2" || m.Description != "Faster sync" || m.Image != "https://img.example.org/card.png" {
		t.Errorf("twitter card = %+v", m)
	}
	if m.Icon != "https://blog.example.org/favicon.ico" {
		t.Errorf("default icon = %q", m.Icon)
	}
	p := parsePageMeta(fixture(t, "plain.html"), base)
	if p.Title != "Just a title" || p.Description != "" || p.Image != "" {
		t.Errorf("plain = %+v", p)
	}
}

func TestParsePageMetaCapsAndSchemes(t *testing.T) {
	long := strings.Repeat("x", 500)
	doc := `<head><meta property="og:title" content="` + long + `"><meta property="og:image" content="javascript:alert(1)"><meta property="og:description" content="a` + "\x00\x07" + `b"></head>`
	m := parsePageMeta(doc, nil)
	if n := len([]rune(m.Title)); n != 201 { // 200 + ellipsis
		t.Errorf("title not capped: %d runes", n)
	}
	if m.Image != "" {
		t.Errorf("javascript image accepted: %q", m.Image)
	}
	if m.Description != "ab" {
		t.Errorf("control characters kept: %q", m.Description)
	}
}

func TestParseOEmbed(t *testing.T) {
	base, _ := url.Parse("https://videosite.example/oembed")
	m, ok := parseOEmbed([]byte(fixture(t, "oembed.json")), base)
	if !ok || m.Title != "Route setting timelapse" || m.SiteName != "VideoSite" || m.Image != "https://i.videosite.example/t/abc.jpg" || m.Description != "Riverside" {
		t.Fatalf("oembed = %+v %v", m, ok)
	}
	if _, ok := parseOEmbed([]byte(`{"html":"<b>"}`), base); ok {
		t.Fatal("oembed without a title accepted")
	}
	if _, ok := parseOEmbed([]byte(`not json`), base); ok {
		t.Fatal("bad json accepted")
	}
}

func TestThumbnail(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1200, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1200; x++ {
			src.Set(x, y, color.NRGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	th := thumbnail(src, 600, color.White)
	if b := th.Bounds(); b.Dx() != 600 || b.Dy() != 300 {
		t.Fatalf("thumbnail size = %v", b)
	}
	if c := th.RGBAAt(10, 10); c.R < 190 || c.G > 20 {
		t.Errorf("colour lost: %+v", c)
	}
	small := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	if b := thumbnail(small, 600, nil).Bounds(); b.Dx() != 40 || b.Dy() != 20 {
		t.Errorf("small image enlarged: %v", b)
	}
	// Transparent pixels over white for JPEG.
	clear := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	if c := thumbnail(clear, 600, color.White).RGBAAt(0, 0); c.R != 255 || c.A != 255 {
		t.Errorf("transparent over white = %+v", c)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, thumbnail(src, 64, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestTrimLinkTail(t *testing.T) {
	cases := map[string]string{
		"https://a.example/x.":        "https://a.example/x",
		"https://a.example/x)":        "https://a.example/x",
		"https://a.example/f(o)":      "https://a.example/f(o)",
		"https://a.example/x?y=1!?":   "https://a.example/x?y=1",
		"https://a.example/x](":       "https://a.example/x](",
		"https://a.example/**bold**": "https://a.example/**bold",
	}
	for in, want := range cases {
		if got := trimLinkTail(in); got != want {
			t.Errorf("trimLinkTail(%q) = %q, want %q", in, got, want)
		}
	}
}
