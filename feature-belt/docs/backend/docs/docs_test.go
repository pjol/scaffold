package docs

import (
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func TestKeyBetweenOrders(t *testing.T) {
	keys := []string{keyBetween("", "")}
	rng := rand.New(rand.NewSource(1))
	for range 2000 {
		i := rng.Intn(len(keys) + 1)
		a, b := "", ""
		if i > 0 {
			a = keys[i-1]
		}
		if i < len(keys) {
			b = keys[i]
		}
		k := keyBetween(a, b)
		if !validKey(k) {
			t.Fatalf("invalid key %q between %q and %q", k, a, b)
		}
		if (a != "" && k <= a) || (b != "" && k >= b) {
			t.Fatalf("key %q not between %q and %q", k, a, b)
		}
		keys = append(keys[:i], append([]string{k}, keys[i:]...)...)
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatal("keys out of order")
	}
	if got := keysAfter("a0", "a1", 5); len(got) != 5 || !sort.StringsAreSorted(append([]string{"a0"}, append(got, "a1")...)) {
		t.Fatalf("keysAfter = %v", got)
	}
	// Equal neighbours fall back to "after a".
	if k := keyBetween("a1", "a1"); k <= "a1" {
		t.Fatalf("fallback key %q", k)
	}
}

func TestSafeLink(t *testing.T) {
	for in, want := range map[string]bool{
		"https://example.com/a?b=1": true, "http://x.io": true, "mailto:a@b.co": true,
		"javascript:alert(1)": false, "data:text/html,hi": false, "//evil.com": false, "https://": false, "ftp://x": false,
	} {
		if got := safeLink(in) != ""; got != want {
			t.Errorf("safeLink(%q) ok=%v, want %v", in, got, want)
		}
	}
}

func TestSanitizeSpans(t *testing.T) {
	in := []Span{{Text: "a\x00b", Bold: true}, {Text: "c", Bold: true}, {Text: ""}, {Text: "x", Link: "javascript:1"},
		{Text: "@Sam", Mention: &Mention{Type: "user", ID: "not-a-uuid"}}}
	out, n := sanitizeSpans(in, spanRules{})
	if len(out) != 2 || out[0].Text != "abc" || !out[0].Bold || out[1].Link != "" || out[1].Text != "x@Sam" || n != 8 {
		t.Fatalf("sanitize = %+v (%d)", out, n)
	}
}

func TestInlineMarkdownRoundTrip(t *testing.T) {
	spans := markdownToSpans("Hello **bold _both_** and `code`, [a link](https://example.com) ~~gone~~ <u>under</u>")
	got := spansToMarkdown(spans, mdNames{})
	want := "Hello **bold** **_both_** and `code`, [a link](https://example.com) ~~gone~~ <u>under</u>"
	if got != want {
		t.Fatalf("round trip\n got %q\nwant %q", got, want)
	}
	if again := spansToMarkdown(markdownToSpans(got), mdNames{}); again != got {
		t.Fatalf("not stable: %q", again)
	}
	unsafe := markdownToSpans("[x](javascript:alert(1))")
	if len(unsafe) != 1 || unsafe[0].Link != "" {
		t.Fatalf("unsafe link kept: %+v", unsafe)
	}
}

func TestMarkdownBlocks(t *testing.T) {
	md := "# Title\n\nPara one\nline two\n\n- a\n  - b\n- [x] done\n1. first\n\n> 💡 tip\n\n> quote\n\n```go\nfmt.Println()\n```\n\n---\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n"
	blocks := markdownToBlocks(md, 100)
	var types []string
	for _, b := range blocks {
		types = append(types, b.typ)
	}
	want := "heading_1 paragraph bulleted_list to_do numbered_list callout quote code divider table"
	if strings.Join(types, " ") != want {
		t.Fatalf("types = %v", types)
	}
	if len(blocks[2].children) != 1 || blocks[2].children[0].typ != "bulleted_list" {
		t.Fatalf("nesting lost: %+v", blocks[2])
	}
	if blocks[5].props["icon"] != "💡" {
		t.Fatalf("callout icon = %v", blocks[5].props["icon"])
	}
}

func TestMergeSpans(t *testing.T) {
	base := []Span{{Text: "The quick fox"}}
	local := []Span{{Text: "The "}, {Text: "very", Bold: true}, {Text: " quick fox"}}
	remote := []Span{{Text: "The quick brown fox"}}
	got := plainOf(mergeSpans(base, local, remote))
	if got != "The very quick brown fox" {
		t.Fatalf("merge = %q", got)
	}
	// Overlapping edits keep local.
	if got := plainOf(mergeSpans(base, []Span{{Text: "The slow fox"}}, []Span{{Text: "The fast fox"}})); got != "The slow fox" {
		t.Fatalf("overlap = %q", got)
	}
}
