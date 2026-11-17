package catalog

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Name keys. Every name and alias the index knows and every name a caller
// asks for goes through NameKey, so matching compares keys.

var (
	sepRe   = regexp.MustCompile(`\s*(//|/|\|)\s*`)
	spaceRe = regexp.MustCompile(`\s+`)

	// letters NFKD leaves alone, typographic punctuation and odd spaces
	foldMap = map[rune]string{
		'æ': "ae", 'œ': "oe", 'ø': "o", 'ł': "l", 'đ': "d", 'ð': "d", 'þ': "th", 'ß': "ss", 'ı': "i",
		'’': "'", '‘': "'", 'ʼ': "'", '′': "'", '`': "'", '´': "'",
		'“': `"`, '”': `"`, '„': `"`, '″': `"`,
		'–': "-", '—': "-", '‐': "-", '‑': "-", '‒': "-", '―': "-", '−': "-",
		'\u00a0': " ", '\u2007': " ", '\u2009': " ", '\u202f': " ",
	}
)

// NameKey folds a name for matching: NFKD with combining marks removed
// (accents dropped, full-width and ligature forms made plain), lower case,
// æ/œ/ø/ß and friends spelled out, curly quotes and dashes made ASCII,
// zero-width characters dropped, "A/B", "A // B" and "A | B" made "a / b",
// whitespace collapsed and trimmed.
func NameKey(s string) string {
	s = norm.NFKD.String(strings.ToLower(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff':
		default:
			if rep, ok := foldMap[r]; ok {
				b.WriteString(rep)
			} else {
				b.WriteRune(unicode.ToLower(r))
			}
		}
	}
	out := sepRe.ReplaceAllString(b.String(), " / ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(out, " "))
}

// distance is the optimal-string-alignment (restricted Damerau-Levenshtein)
// distance between a and b, or limit when it is at least limit. A
// transposition of neighbours is one edit.
func distance(a, b []rune, limit int) int {
	if d := len(a) - len(b); d >= limit || -d >= limit {
		return limit
	}
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
			rowMin = min(rowMin, cur[j])
		}
		if rowMin >= limit {
			return limit
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return min(prev[len(b)], limit)
}

// runeMask has a bit per character a key contains (a–z and 0–9 their own
// bits, anything else hashed onto the rest). One edit changes at most two
// bits, so keys whose masks differ in more than 2×d bits are more than d
// edits apart: the fuzzy step skips them without computing the distance.
func runeMask(k string) uint64 {
	var m uint64
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z':
			m |= 1 << (r - 'a')
		case r >= '0' && r <= '9':
			m |= 1 << (26 + r - '0')
		default:
			m |= 1 << (36 + uint64(r)%28)
		}
	}
	return m
}
