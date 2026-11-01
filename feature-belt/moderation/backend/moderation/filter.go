package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// filter is the pre-post check: a normalised blocked-terms list plus an
// optional provider.
type filter struct {
	terms    [][]string // each term as normalised words
	provider Provider
}

// Provider is an external moderation classifier. Flagged returns true when
// text must not be posted; an error means "couldn't tell" and Check lets the
// text through (fail open) after logging.
type Provider interface {
	Name() string
	Flagged(ctx context.Context, text string) (bool, error)
}

func newFilter(terms []string, p Provider) *filter {
	f := &filter{provider: p}
	for _, t := range terms {
		if words := tokens(t); len(words) > 0 {
			f.terms = append(f.terms, words)
		}
	}
	return f
}

// matchTerm reports whether any blocked term appears in text as whole
// words, after normalisation.
func (f *filter) matchTerm(text string) bool {
	if len(f.terms) == 0 {
		return false
	}
	words := tokens(text)
	for _, term := range f.terms {
		for i := 0; i+len(term) <= len(words); i++ {
			ok := true
			for j, w := range term {
				if !wordMatch(words[i+j], w) {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

// leetDigits and leetSymbols map common swaps back to letters. Digits
// count only next to a letter ("h3ll0", not "1000"); '@', '$', '€', '£' next
// to a letter on either side ("a$$"); '!' and '|' only before a letter or
// digit, so a trailing "!" stays punctuation ("sh!t", not "wow!").
var (
	leetDigits  = map[rune]rune{'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '8': 'b'}
	leetSymbols = map[rune]rune{'@': 'a', '$': 's', '€': 'e', '£': 'l', '!': 'i', '|': 'i'}
)

// normalize lowercases, strips accents and invisible characters, and undoes
// leetspeak: "Ḧ3LL0" → "hello".
func normalize(s string) string {
	rs := make([]rune, 0, len(s))
	for _, r := range norm.NFKD.String(s) {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Cf, r) {
			continue // combining accents, zero-width joiners and friends
		}
		rs = append(rs, unicode.ToLower(r))
	}
	letter := func(i int) bool { return i >= 0 && i < len(rs) && unicode.IsLetter(rs[i]) }
	for i, r := range rs {
		if m, ok := leetDigits[r]; ok && (letter(i-1) || letter(i+1)) {
			rs[i] = m
			continue
		}
		m, ok := leetSymbols[r]
		if !ok {
			continue
		}
		if r == '!' || r == '|' {
			if letter(i+1) || (i+1 < len(rs) && unicode.IsDigit(rs[i+1])) {
				rs[i] = m
			}
		} else if letter(i-1) || letter(i+1) {
			rs[i] = m
		}
	}
	return string(rs)
}

// tokens splits normalised text into words, then joins runs of single
// letters so "f r e e" and "f.r.e.e" read as "free".
func tokens(s string) []string {
	raw := strings.FieldsFunc(normalize(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := make([]string, 0, len(raw))
	var run strings.Builder
	runLen := 0
	flush := func() {
		if runLen > 0 {
			out = append(out, run.String())
		}
		run.Reset()
		runLen = 0
	}
	for _, w := range raw {
		if len([]rune(w)) == 1 {
			run.WriteString(w)
			runLen++
			continue
		}
		flush()
		out = append(out, w)
	}
	flush()
	return out
}

// wordMatch compares a text word with a term word. Stretched spellings
// ("baaad") match when squeezing repeated letters gives the same word, but a
// shorter word never matches a longer term ("as" is not "ass").
func wordMatch(word, term string) bool {
	if word == term {
		return true
	}
	return len(word) > len(term) && squeeze(word) == squeeze(term)
}

func squeeze(s string) string {
	var b strings.Builder
	var prev rune = -1
	for _, r := range s {
		if r != prev {
			b.WriteRune(r)
		}
		prev = r
	}
	return b.String()
}

// ---- OpenAI moderation -----------------------------------------------------

// OpenAI calls the OpenAI moderation endpoint (free to use with any API key).
type OpenAI struct {
	APIKey string
	Model  string // default omni-moderation-latest
	Client *http.Client
	URL    string // default https://api.openai.com/v1/moderations
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Flagged(ctx context.Context, text string) (bool, error) {
	if strings.TrimSpace(text) == "" {
		return false, nil
	}
	model, url := o.Model, o.URL
	if model == "" {
		model = "omni-moderation-latest"
	}
	if url == "" {
		url = "https://api.openai.com/v1/moderations"
	}
	body, err := json.Marshal(map[string]any{"model": model, "input": text})
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return false, fmt.Errorf("openai moderation: status %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out struct {
		Results []struct {
			Flagged bool `json:"flagged"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return false, fmt.Errorf("openai moderation: decode: %w", err)
	}
	for _, r := range out.Results {
		if r.Flagged {
			return true, nil
		}
	}
	return false, nil
}
