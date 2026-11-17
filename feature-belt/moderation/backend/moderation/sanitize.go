package moderation

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Delimiters around user text in the decision model's state. Sanitize
// removes them (and their look-alikes) from user text, so quoted text can
// never close its own quote.
const (
	delimOpen  = "⟦"
	delimClose = "⟧"
)

// invisibleOrDelim reports runes Sanitize drops: format characters (zero-width
// spaces and joiners, bidi embeddings/overrides/isolates, the BOM, soft
// hyphens, Unicode tag characters), private-use and surrogate code points,
// variation selectors, the combining grapheme joiner, blank Hangul fillers,
// and the quote delimiters with their look-alikes.
func invisibleOrDelim(r rune) bool {
	switch {
	case r == '⟦' || r == '⟧' || r == '〚' || r == '〛':
		return true
	case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Co, r), unicode.Is(unicode.Cs, r):
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF, r == 0x034F:
		return true
	case r == 0x115F || r == 0x1160 || r == 0x3164 || r == 0xFFA0:
		return true
	}
	return false
}

// Sanitize prepares user text for a quoted state field: NFKC-normalised,
// invisible and bidi control characters and the ⟦ ⟧ delimiters removed,
// every run of whitespace (and other control characters) collapsed to one
// space.
func Sanitize(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if invisibleOrDelim(r) {
			continue
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// clip keeps at most limit runes: the first head and the last limit−head,
// joined by " … ". head ≥ limit keeps the start only.
func clip(s string, limit, head int) string {
	rs := []rune(s)
	if limit <= 0 || len(rs) <= limit {
		return s
	}
	if head >= limit {
		return string(rs[:limit]) + " …"
	}
	return string(rs[:head]) + " … " + string(rs[len(rs)-(limit-head):])
}

// quote sanitises, clips and wraps user text: ⟦BEGIN QUOTED <WHAT>⟧ … ⟦END QUOTED <WHAT>⟧.
func quote(what, s string, limit, head int) string {
	return delimOpen + "BEGIN QUOTED " + what + delimClose + " " + clip(Sanitize(s), limit, head) +
		" " + delimOpen + "END QUOTED " + what + delimClose
}

// tame keeps the app's own text (a place description) to letters, digits,
// spaces and light punctuation, at most limit runes. It is not user text, but
// it still never carries delimiters or control characters into the state.
func tame(s string, limit int) string {
	var b strings.Builder
	for _, r := range Sanitize(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" -,'():&./", r) {
			b.WriteRune(r)
		}
	}
	return clip(strings.TrimSpace(b.String()), limit, limit)
}
