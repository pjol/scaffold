package baskets

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"scaffoldapp/backend/internal/httpx"
)

// Line is one line of a pasted list: what the person typed for the market and
// how many units (1 when they didn't say).
type Line struct {
	N        int    // 1-based line of the paste (CSV: the row)
	Text     string // the name or symbol as typed
	Quantity int
}

var errTooLong = httpx.NewError(http.StatusUnprocessableEntity, "list_too_long", "Paste at most 250 lines (20 KB).")

var (
	reQtyFirst  = regexp.MustCompile(`^(\d+)\s*[xX×]?\s+(\S.*)$`)         // "2 Gold", "2x Gold", "2 x Gold"
	reXFirst    = regexp.MustCompile(`^[xX×]\s*(\d+)\s+(\S.*)$`)          // "x2 Gold"
	reQtyLast   = regexp.MustCompile(`^(\S.*?)\s+[xX×]\s*(\d+)$`)         // "Gold x3", "Gold × 3"
	reSepLast   = regexp.MustCompile(`^(\S.*?)\s*[,;\t:]\s*[xX×]?(\d+)$`) // "Gold, 1", "BTC-USD,1", "Gold\t2", "Gold: x2"
	reFraction  = regexp.MustCompile(`^\d+[.,]\d+\s*[xX×]?\s+\S|\s[xX×]\s*\d+[.,]\d+$|[,;\t:]\s*\d+\.\d+$`)
	reBullet    = regexp.MustCompile(`^(?:[-*•·]|\d+[.)])\s+`)
	reSpaces    = regexp.MustCompile(`\s+`)
	nameHeaders = []string{"symbol", "ticker", "market", "name", "asset", "item", "title"}
	qtyHeaders  = []string{"quantity", "qty", "count", "amount", "units", "shares"}
)

// Parse reads a pasted list: one market per line ("2 Gold", "Gold x3",
// "BTC-USD, 1", "x2 Silver", bullets and blank lines allowed, # and //
// comments skipped), or CSV/TSV with a header that names a market column
// (symbol, ticker, market, name, asset, item) and optionally a quantity
// column. Quantities are whole numbers 1–10,000 and default to 1. Lines it
// can't read are skipped with a warning in words.
func Parse(text string) ([]Line, []string, error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if len(text) > maxTextBytes || nonBlank(text) > maxInputLines {
		return nil, nil, errTooLong
	}
	if cols, ok := csvHeader(text); ok {
		return parseCSV(text, cols)
	}
	var (
		out   []Line
		warns []string
	)
	for i, raw := range strings.Split(text, "\n") {
		n := i + 1
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		// A numbered bullet ("1. Gold") is a bullet only when more text follows.
		if loc := reBullet.FindStringIndex(l); loc != nil && !reQtyFirst.MatchString(l) {
			l = strings.TrimSpace(l[loc[1]:])
		}
		if reFraction.MatchString(l) {
			warns = append(warns, fmt.Sprintf("Line %d: quantities are whole numbers. Skipped.", n))
			continue
		}
		name, qs := l, ""
		for _, re := range []*regexp.Regexp{reXFirst, reQtyFirst} {
			if m := re.FindStringSubmatch(l); m != nil {
				qs, name = m[1], m[2]
				break
			}
		}
		if qs == "" {
			for _, re := range []*regexp.Regexp{reQtyLast, reSepLast} {
				if m := re.FindStringSubmatch(l); m != nil {
					name, qs = m[1], m[2]
					break
				}
			}
		}
		if line, warn := makeLine(n, "Line", name, qs); warn != "" {
			warns = append(warns, warn)
		} else {
			out = append(out, line)
		}
	}
	return out, warns, nil
}

// makeLine checks one name and quantity; a non-empty warning means skip it.
func makeLine(n int, what, name, qs string) (Line, string) {
	name = strings.TrimSpace(reSpaces.ReplaceAllString(strings.Trim(strings.TrimSpace(name), `"'`), " "))
	qty := 1
	if qs = strings.TrimSpace(qs); qs != "" {
		v, err := strconv.Atoi(qs)
		if err != nil || v < 1 || v > maxQuantity {
			return Line{}, fmt.Sprintf("%s %d: quantity must be 1–10,000. Skipped.", what, n)
		}
		qty = v
	}
	switch {
	case name == "":
		return Line{}, fmt.Sprintf("%s %d: no market named. Skipped.", what, n)
	case len([]rune(name)) > 120:
		return Line{}, fmt.Sprintf("%s %d: a name is at most 120 characters. Skipped.", what, n)
	}
	return Line{N: n, Text: name, Quantity: qty}, ""
}

func nonBlank(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

type csvCols struct {
	comma     rune
	name, qty int
}

// csvHeader recognises a header row: the first non-blank line, split on
// comma, tab or semicolon, with a market column.
func csvHeader(text string) (csvCols, bool) {
	c := csvCols{name: -1, qty: -1}
	var first string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			first = l
			break
		}
	}
	switch {
	case strings.Contains(first, "\t"):
		c.comma = '\t'
	case strings.Contains(first, ","):
		c.comma = ','
	case strings.Contains(first, ";"):
		c.comma = ';'
	default:
		return c, false
	}
	r := csv.NewReader(strings.NewReader(first))
	r.Comma, r.LazyQuotes, r.FieldsPerRecord = c.comma, true, -1
	cells, err := r.Read()
	if err != nil {
		return c, false
	}
	rank := len(nameHeaders)
	for i, h := range cells {
		h = strings.ToLower(strings.TrimSpace(h))
		for k, want := range nameHeaders {
			if h == want && k < rank {
				c.name, rank = i, k
			}
		}
		for _, want := range qtyHeaders {
			if h == want && c.qty < 0 {
				c.qty = i
			}
		}
	}
	return c, c.name >= 0
}

func parseCSV(text string, cols csvCols) ([]Line, []string, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(text)))
	r.Comma, r.LazyQuotes, r.FieldsPerRecord, r.TrimLeadingSpace = cols.comma, true, -1, true
	if _, err := r.Read(); err != nil {
		return nil, nil, httpx.NewError(http.StatusUnprocessableEntity, "list_unreadable", "That list couldn't be read. Paste one market per line.")
	}
	var (
		out   []Line
		warns []string
	)
	cell := func(rec []string, i int) string {
		if i < 0 || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	row := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		row++
		if err != nil {
			warns = append(warns, fmt.Sprintf("Row %d couldn't be read. Skipped.", row))
			continue
		}
		name := cell(rec, cols.name)
		qs := strings.TrimSpace(strings.Trim(strings.ToLower(cell(rec, cols.qty)), "x×"))
		if name == "" && qs == "" {
			continue
		}
		if strings.ContainsAny(qs, ".,") {
			warns = append(warns, fmt.Sprintf("Row %d: quantities are whole numbers. Skipped.", row))
			continue
		}
		if line, warn := makeLine(row, "Row", name, qs); warn != "" {
			warns = append(warns, warn)
		} else {
			out = append(out, line)
		}
	}
	return out, warns, nil
}
