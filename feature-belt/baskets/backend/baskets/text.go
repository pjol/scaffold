package baskets

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"scaffoldapp/backend/internal/httpx"
)

// Names and descriptions are user content other people see. Before they are
// stored they pass a charset and link check, the reserved terms, the
// BASKETS_BLOCKED_TERMS list, and the app's own check (SetTextCheck: the
// moderation feature's filter, when installed). Staff hide and rename in
// Admin → Baskets; people report from a basket's page.

var (
	reservedTerms = [][]string{{"official"}, {"verified"}, {"guaranteed"}, {"riskless"}, {"risk", "free"}, {"airdrop"}}
	reLink        = regexp.MustCompile(`(?i)(https?://|www\.|\.[a-z]{2,}/)`)
	reNameSpaces  = regexp.MustCompile(`\s+`)
	typography    = strings.NewReplacer("’", "'", "‘", "'", "ʼ", "'", "“", `"`, "”", `"`, "–", "-", "—", "-", "‐", "-")
	leet          = strings.NewReplacer("0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "@", "a", "$", "s")

	textCheckMu sync.RWMutex
	textCheck   func(ctx context.Context, text string) error
)

// SetTextCheck adds the app's own check for names and descriptions (call it
// from an init()). An error that is an *httpx.Error goes to the client as
// is; any other error rejects it (name_rejected or description_rejected).
// With the moderation feature: baskets.SetTextCheck(moderation.Check).
func SetTextCheck(fn func(ctx context.Context, text string) error) {
	textCheckMu.Lock()
	defer textCheckMu.Unlock()
	textCheck = fn
}

type textFilter struct{ terms [][]string }

func newTextFilter(terms []string) *textFilter {
	f := &textFilter{}
	for _, t := range terms {
		if ws := strings.Fields(normKey(t)); len(ws) > 0 {
			f.terms = append(f.terms, ws)
		}
	}
	return f
}

// hasTerm reports whether any term appears as whole words in text, as typed
// or with digit and symbol swaps undone ("fr33" reads as "free").
func hasTerm(text string, terms [][]string) bool {
	for _, variant := range []string{normKey(text), normKey(leet.Replace(strings.ToLower(text)))} {
		words := strings.Fields(variant)
		for _, term := range terms {
			for i := 0; i+len(term) <= len(words); i++ {
				match := true
				for j, w := range term {
					if words[i+j] != w {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}

func nameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == ' ' || strings.ContainsRune("-'&.,:!?()/+#", r)
}

func rejected(code, msg string) *httpx.Error {
	return httpx.NewError(http.StatusUnprocessableEntity, code, msg)
}

func (s *Service) appCheck(ctx context.Context, text, code string) error {
	textCheckMu.RLock()
	fn := textCheck
	textCheckMu.RUnlock()
	if fn == nil || text == "" {
		return nil
	}
	if err := fn(ctx, text); err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			return he
		}
		return rejected(code, "That wording isn't allowed. Choose another.")
	}
	return nil
}

// cleanName checks a basket name and returns it tidied: 3–48 characters of
// letters, digits, spaces and - ' & . , : ! ? ( ) / + #; no links, no @, no
// reserved or blocked terms.
func (s *Service) cleanName(ctx context.Context, v string) (string, error) {
	v = strings.TrimSpace(reNameSpaces.ReplaceAllString(norm.NFC.String(typography.Replace(v)), " "))
	switch n := len([]rune(v)); {
	case n < 3 || n > 48:
		return "", rejected("name_rejected", "Names are 3–48 characters.")
	case reLink.MatchString(v):
		return "", rejected("name_rejected", "Names can't contain links.")
	case strings.ContainsRune(v, '@'):
		return "", rejected("name_rejected", "Names can't contain @.")
	}
	for _, r := range v {
		if !nameRune(r) {
			return "", rejected("name_rejected", "Names can use letters, digits, spaces and - ' & . , : ! ? ( ) / + #.")
		}
	}
	if hasTerm(v, reservedTerms) {
		return "", rejected("name_rejected", "Names can't claim to be official, verified, guaranteed or risk-free.")
	}
	if hasTerm(v, s.filter.terms) {
		return "", rejected("name_rejected", "That wording isn't allowed. Choose another.")
	}
	if err := s.appCheck(ctx, v, "name_rejected"); err != nil {
		return "", err
	}
	return v, nil
}

// cleanDescription: at most 500 characters of plain text and 5 line breaks,
// no links, no blocked terms.
func (s *Service) cleanDescription(ctx context.Context, v string) (string, error) {
	v = norm.NFC.String(strings.ReplaceAll(v, "\r\n", "\n"))
	var b strings.Builder
	for _, r := range v {
		if r == '\n' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	v = strings.TrimSpace(b.String())
	switch {
	case len([]rune(v)) > 500:
		return "", rejected("description_rejected", "Descriptions are at most 500 characters.")
	case strings.Count(v, "\n") > 5:
		return "", rejected("description_rejected", "Descriptions have at most 5 line breaks.")
	case reLink.MatchString(v):
		return "", rejected("description_rejected", "Descriptions can't contain links.")
	case hasTerm(v, reservedTerms):
		return "", rejected("description_rejected", "Descriptions can't claim to be official, verified, guaranteed or risk-free.")
	case hasTerm(v, s.filter.terms):
		return "", rejected("description_rejected", "That wording isn't allowed. Choose another.")
	}
	if err := s.appCheck(ctx, v, "description_rejected"); err != nil {
		return "", err
	}
	return v, nil
}
