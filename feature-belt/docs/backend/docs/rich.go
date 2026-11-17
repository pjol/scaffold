package docs

import (
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Span is one run of rich text with the same formatting. Text blocks,
// captions, table cells and comments are []Span. The JSON matches DocsSpan
// in shared/types.ts.
type Span struct {
	Text      string   `json:"text"`
	Bold      bool     `json:"bold,omitempty"`
	Italic    bool     `json:"italic,omitempty"`
	Underline bool     `json:"underline,omitempty"`
	Strike    bool     `json:"strike,omitempty"`
	Code      bool     `json:"code,omitempty"`
	Link      string   `json:"link,omitempty"`     // http(s) or mailto only
	Color     string   `json:"color,omitempty"`    // text colour ("red") or highlight ("red_background")
	Equation  bool     `json:"equation,omitempty"` // the text is TeX shown as one inline equation
	Mention   *Mention `json:"mention,omitempty"`  // the span is one mention chip
}

// Mention points at a person in the org, a page, or a date. The span's text
// is what was shown when it was made; clients render the live name.
type Mention struct {
	Type string `json:"type"`           // user | page | date
	ID   string `json:"id,omitempty"`   // user or page id
	Date string `json:"date,omitempty"` // YYYY-MM-DD
	Time string `json:"time,omitempty"` // HH:MM (local to whoever set it), dates only
	// RemindAt (RFC 3339, UTC) makes a date a reminder: whoever set it is
	// notified then (reminders.go).
	RemindAt string `json:"remind_at,omitempty"`
}

const (
	maxBlockChars   = 20000 // characters in one block's text
	maxSpans        = 2000
	maxLinkLen      = 2000
	maxMentionChars = 200
	maxInlineTeX    = 2000
)

func (a Span) sameMarks(b Span) bool {
	return a.Mention == nil && b.Mention == nil && !a.Equation && !b.Equation && a.Bold == b.Bold && a.Italic == b.Italic &&
		a.Underline == b.Underline && a.Strike == b.Strike && a.Code == b.Code && a.Link == b.Link && a.Color == b.Color
}

// cleanText drops control characters other than newline and tab and
// normalises line endings.
func cleanText(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return '\n'
		case unicode.IsControl(r), r == 0x2028, r == 0x2029, r == 0xFEFF:
			return -1
		}
		return r
	}, s)
}

// safeLink returns the link when it is an http(s) URL with a host or a
// mailto address, else "".
func safeLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxLinkLen || strings.ContainsAny(raw, " \n\t<>\"") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return ""
		}
	case "mailto":
		if u.Opaque == "" && u.Path == "" {
			return ""
		}
	default:
		return ""
	}
	return u.String()
}

var (
	dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRE = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)
)

func validDate(s string) bool {
	if !dateRE.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// spanRules decides which mentions survive sanitising (people must be in
// the page's org; nil accepts any id that parses).
type spanRules struct {
	validUser func(uuid.UUID) bool
}

// sanitizeSpans cleans rich text: strips control characters, drops unsafe
// links and invalid mentions (they become plain text), merges neighbours
// with the same formatting and drops empty runs. It returns the cleaned
// spans and their character count.
func sanitizeSpans(in []Span, rules spanRules) ([]Span, int) {
	out := make([]Span, 0, len(in))
	n := 0
	for _, s := range in {
		s.Text = cleanText(s.Text)
		if s.Text == "" {
			continue
		}
		s.Link = safeLink(s.Link)
		if !validColor(s.Color) {
			s.Color = ""
		}
		if s.Equation {
			// One inline equation: TeX on one line, no link or code mark.
			s.Mention, s.Link, s.Code = nil, "", false
			s.Text = strings.TrimSpace(strings.ReplaceAll(s.Text, "\n", " "))
			if utf8.RuneCountInString(s.Text) > maxInlineTeX {
				s.Text = string([]rune(s.Text)[:maxInlineTeX])
			}
			if s.Text == "" {
				continue
			}
		}
		if s.Mention != nil {
			m := *s.Mention
			ok := false
			switch m.Type {
			case "user", "page":
				id, err := uuid.Parse(m.ID)
				ok = err == nil && (m.Type == "page" || rules.validUser == nil || rules.validUser(id))
				m.ID, m.Date = id.String(), ""
			case "date":
				ok = validDate(m.Date)
				m.ID = ""
				if !timeRE.MatchString(m.Time) {
					m.Time = ""
				}
				m.RemindAt = normalizeRemindAt(m.RemindAt)
			}
			if ok {
				s.Mention = &m
				s.Code, s.Link, s.Equation = false, "", false
				s.Text = strings.ReplaceAll(s.Text, "\n", " ")
				if utf8.RuneCountInString(s.Text) > maxMentionChars {
					s.Text = string([]rune(s.Text)[:maxMentionChars])
				}
			} else {
				s.Mention = nil
			}
		}
		if len(out) > 0 && out[len(out)-1].sameMarks(s) {
			out[len(out)-1].Text += s.Text
		} else {
			out = append(out, s)
		}
		n += utf8.RuneCountInString(s.Text)
	}
	return out, n
}

// normalizeRemindAt keeps a reminder time that parses (as UTC), within ten
// years either way; anything else drops the reminder, not the date.
func normalizeRemindAt(raw string) string {
	if raw == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil || t.Before(time.Now().AddDate(-10, 0, 0)) || t.After(time.Now().AddDate(10, 0, 0)) {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func plainOf(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func textSpans(s string) []Span {
	if s == "" {
		return []Span{}
	}
	return []Span{{Text: s}}
}

// mentionIDs returns the user and page ids mentioned in spans.
func mentionIDs(spans []Span) (users, pages []uuid.UUID) {
	for _, s := range spans {
		if s.Mention == nil {
			continue
		}
		id, err := uuid.Parse(s.Mention.ID)
		if err != nil {
			continue
		}
		switch s.Mention.Type {
		case "user":
			users = append(users, id)
		case "page":
			pages = append(pages, id)
		}
	}
	return users, pages
}

// ---- Markdown (inline) -----------------------------------------------------------

var mdEscaper = mdEscape{strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "~", `\~`, "<", `\<`)}

// mdEscape escapes Markdown punctuation, and a $ only where it would open an
// inline equation ($ then a non-space, non-digit), so prices stay "$5".
type mdEscape struct{ r *strings.Replacer }

var dollarOpenRE = regexp.MustCompile(`\$[^\s\d$]`)

func (e mdEscape) Replace(s string) string {
	s = e.r.Replace(s)
	if !strings.Contains(s, "$") {
		return s
	}
	return dollarOpenRE.ReplaceAllStringFunc(s, func(m string) string { return `\` + m })
}

// mdNames resolves mention labels when rendering Markdown (live names).
type mdNames struct {
	user func(id string) string
	page func(id string) string
}

// spansToMarkdown renders rich text as inline Markdown. Underline uses
// <u>…</u>, colours <span color="red">…</span>, inline equations $TeX$;
// page mentions become links to /docs/<id>; people become @Name.
func spansToMarkdown(spans []Span, names mdNames) string {
	var b strings.Builder
	for _, s := range spans {
		if s.Equation {
			tex := strings.ReplaceAll(s.Text, "$", `\$`)
			if r, _ := utf8.DecodeRuneInString(tex); unicode.IsDigit(r) {
				tex = "{}" + tex // "$2x$" would read as money
			}
			b.WriteString("$" + tex + "$")
			continue
		}
		if s.Mention != nil {
			switch s.Mention.Type {
			case "user":
				name := strings.TrimPrefix(s.Text, "@")
				if names.user != nil {
					if n := names.user(s.Mention.ID); n != "" {
						name = n
					}
				}
				b.WriteString("@" + mdEscaper.Replace(name))
			case "page":
				title := s.Text
				if names.page != nil {
					if t := names.page(s.Mention.ID); t != "" {
						title = t
					}
				}
				b.WriteString("[" + mdEscaper.Replace(title) + "](/docs/" + s.Mention.ID + ")")
			default:
				b.WriteString(mdEscaper.Replace(s.Text))
			}
			continue
		}
		text := s.Text
		lead, trail := "", ""
		// Markdown delimiters can't hug whitespace: keep it outside.
		trimmed := strings.TrimLeft(text, " \t")
		lead = text[:len(text)-len(trimmed)]
		core := strings.TrimRight(trimmed, " \t")
		trail = trimmed[len(core):]
		if core == "" {
			b.WriteString(text)
			continue
		}
		var out string
		if s.Code {
			fence := "`"
			if strings.Contains(core, "`") {
				fence = "``"
			}
			out = fence + core + fence
		} else {
			out = strings.ReplaceAll(mdEscaper.Replace(core), "\n", "  \n")
		}
		if s.Strike {
			out = "~~" + out + "~~"
		}
		if s.Italic {
			out = "_" + out + "_"
		}
		if s.Bold {
			out = "**" + out + "**"
		}
		if s.Underline {
			out = "<u>" + out + "</u>"
		}
		if s.Color != "" {
			out = `<span color="` + s.Color + `">` + out + "</span>"
		}
		if s.Link != "" {
			out = "[" + out + "](" + s.Link + ")"
		}
		b.WriteString(lead + out + trail)
	}
	return b.String()
}

type inlineState struct {
	bold, italic, strike, underline bool
	link, color                     string
}

// markdownToSpans parses inline Markdown: **bold**, __bold__, *italic*,
// _italic_, ~~strike~~, `code`, <u>underline</u>, [text](url), <url>, and
// mentions written as @[Name](user:<id>), [Title](page:<id>) or
// [Title](/docs/<id>). Unsafe links keep their text without the link.
func markdownToSpans(s string) []Span {
	var out []Span
	parseInline([]rune(s), inlineState{}, &out)
	cleaned, _ := sanitizeSpans(out, spanRules{})
	return cleaned
}

func emit(out *[]Span, st inlineState, text string, code bool) {
	if text == "" {
		return
	}
	*out = append(*out, Span{Text: text, Bold: st.bold, Italic: st.italic, Strike: st.strike, Underline: st.underline, Code: code, Link: st.link, Color: st.color})
}

func hasAhead(r []rune, from int, delim string) int {
	d := []rune(delim)
	for i := from; i+len(d) <= len(r); i++ {
		if r[i] == '\\' {
			i++
			continue
		}
		if string(r[i:i+len(d)]) == delim {
			return i
		}
	}
	return -1
}

func parseInline(r []rune, st inlineState, out *[]Span) {
	var buf strings.Builder
	flush := func() {
		emit(out, st, buf.String(), false)
		buf.Reset()
	}
	for i := 0; i < len(r); i++ {
		c := r[i]
		rest := string(r[i:min(len(r), i+4)])
		switch {
		case c == '\\' && i+1 < len(r):
			buf.WriteRune(r[i+1])
			i++
		case c == '`':
			n := 1
			if i+1 < len(r) && r[i+1] == '`' {
				n = 2
			}
			fence := strings.Repeat("`", n)
			if end := hasAhead(r, i+n, fence); end > i+n-1 {
				flush()
				emit(out, st, string(r[i+n:end]), true)
				i = end + n - 1
			} else {
				buf.WriteRune(c)
			}
		case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
			delim := string(r[i : i+2])
			if end := hasAhead(r, i+2, delim); end > i+2 {
				flush()
				inner := st
				inner.bold = true
				parseInline(r[i+2:end], inner, out)
				i = end + 1
			} else {
				buf.WriteString(delim)
				i++
			}
		case strings.HasPrefix(rest, "~~"):
			if end := hasAhead(r, i+2, "~~"); end > i+2 {
				flush()
				inner := st
				inner.strike = true
				parseInline(r[i+2:end], inner, out)
				i = end + 1
			} else {
				buf.WriteString("~~")
				i++
			}
		case c == '$' && i+1 < len(r) && r[i+1] != '$' && !unicode.IsSpace(r[i+1]) && !unicode.IsDigit(r[i+1]):
			// $TeX$: opens before a non-space, non-digit (so "$5" is money);
			// the closing $ follows a non-space and isn't followed by a digit.
			end := -1
			for j := i + 1; j < len(r); j++ {
				if r[j] == '\\' {
					j++
					continue
				}
				if r[j] == '\n' {
					break
				}
				if r[j] == '$' && !unicode.IsSpace(r[j-1]) && (j+1 >= len(r) || !unicode.IsDigit(r[j+1])) {
					end = j
					break
				}
			}
			if end > i+1 {
				flush()
				*out = append(*out, Span{Text: strings.ReplaceAll(string(r[i+1:end]), `\$`, "$"), Equation: true})
				i = end
			} else {
				buf.WriteRune(c)
			}
		case strings.HasPrefix(string(r[i:min(len(r), i+13)]), `<span color="`):
			if m := spanColorRE.FindStringSubmatch(string(r[i:min(len(r), i+40)])); m != nil {
				open := len([]rune(m[0]))
				if end := hasAhead(r, i+open, "</span>"); end >= i+open {
					flush()
					inner := st
					if validColor(m[1]) {
						inner.color = m[1]
					}
					parseInline(r[i+open:end], inner, out)
					i = end + len("</span>") - 1
					continue
				}
			}
			buf.WriteRune(c)
		case strings.HasPrefix(rest, "<u>"):
			if end := hasAhead(r, i+3, "</u>"); end >= i+3 {
				flush()
				inner := st
				inner.underline = true
				parseInline(r[i+3:end], inner, out)
				i = end + 3
			} else {
				buf.WriteRune(c)
			}
		case c == '<':
			if end := hasAhead(r, i+1, ">"); end > i+1 {
				if href := safeLink(string(r[i+1 : end])); href != "" {
					flush()
					inner := st
					inner.link = href
					emit(out, inner, string(r[i+1:end]), false)
					i = end
					continue
				}
			}
			buf.WriteRune(c)
		case (c == '*' || c == '_') && i+1 < len(r) && !unicode.IsSpace(r[i+1]):
			wordStart := i == 0 || !(unicode.IsLetter(r[i-1]) || unicode.IsDigit(r[i-1]))
			end := hasAhead(r, i+1, string(c))
			if end > i+1 && (c == '*' || wordStart) {
				flush()
				inner := st
				inner.italic = true
				parseInline(r[i+1:end], inner, out)
				i = end
			} else {
				buf.WriteRune(c)
			}
		case c == '@' && i+1 < len(r) && r[i+1] == '[':
			if label, target, next, ok := linkAt(r, i+1); ok && strings.HasPrefix(target, "user:") {
				flush()
				*out = append(*out, Span{Text: "@" + label, Mention: &Mention{Type: "user", ID: strings.TrimPrefix(target, "user:")}})
				i = next - 1
			} else {
				buf.WriteRune(c)
			}
		case c == '[':
			label, target, next, ok := linkAt(r, i)
			if !ok {
				buf.WriteRune(c)
				continue
			}
			flush()
			if id, isPage := pageLinkTarget(target); isPage {
				*out = append(*out, Span{Text: label, Mention: &Mention{Type: "page", ID: id}})
			} else {
				inner := st
				inner.link = safeLink(target)
				parseInline([]rune(label), inner, out)
			}
			i = next - 1
		default:
			buf.WriteRune(c)
		}
	}
	flush()
}

// linkAt reads [label](target) starting at r[i] == '['. next is the index
// after the closing parenthesis.
func linkAt(r []rune, i int) (label, target string, next int, ok bool) {
	depth := 0
	for j := i; j < len(r); j++ {
		switch r[j] {
		case '\\':
			j++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				if j+1 >= len(r) || r[j+1] != '(' {
					return "", "", 0, false
				}
				end := hasAhead(r, j+2, ")")
				if end < 0 {
					return "", "", 0, false
				}
				return string(r[i+1 : j]), strings.TrimSpace(string(r[j+2 : end])), end + 1, true
			}
		}
	}
	return "", "", 0, false
}

var spanColorRE = regexp.MustCompile(`^<span color="([a-z_]{1,20})">`)

var docsPathRE = regexp.MustCompile(`^(?:page:|/docs/)([0-9a-fA-F-]{36})$`)

func pageLinkTarget(target string) (string, bool) {
	m := docsPathRE.FindStringSubmatch(target)
	if m == nil {
		return "", false
	}
	if _, err := uuid.Parse(m[1]); err != nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// ---- merging ---------------------------------------------------------------------

func spansEqual(a, b []Span) bool {
	x, _ := sanitizeSpans(a, spanRules{})
	y, _ := sanitizeSpans(b, spanRules{})
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i].Text != y[i].Text || x[i].Bold != y[i].Bold || x[i].Italic != y[i].Italic || x[i].Underline != y[i].Underline ||
			x[i].Strike != y[i].Strike || x[i].Code != y[i].Code || x[i].Link != y[i].Link || x[i].Color != y[i].Color || x[i].Equation != y[i].Equation {
			return false
		}
		if (x[i].Mention == nil) != (y[i].Mention == nil) || (x[i].Mention != nil && *x[i].Mention != *y[i].Mention) {
			return false
		}
	}
	return true
}

// sliceSpans returns the spans covering runes [start, end); a mention partly
// inside is kept whole.
func sliceSpans(spans []Span, start, end int) []Span {
	var out []Span
	at := 0
	for _, s := range spans {
		r := []rune(s.Text)
		a, b := at, at+len(r)
		at = b
		if b <= start || a >= end {
			continue
		}
		if s.Mention != nil || s.Equation {
			if a >= start && b <= end {
				out = append(out, s)
			}
			continue
		}
		s.Text = string(r[max(0, start-a):min(len(r), end-a)])
		out = append(out, s)
	}
	return out
}

type textRegion struct {
	start, end, insertLen int
	insert                []Span
}

func diffRegion(base, next []rune, nextSpans []Span) textRegion {
	p := 0
	n := min(len(base), len(next))
	for p < n && base[p] == next[p] {
		p++
	}
	s := 0
	for s < n-p && base[len(base)-1-s] == next[len(next)-1-s] {
		s++
	}
	return textRegion{start: p, end: len(base) - s, insertLen: len(next) - s - p, insert: sliceSpans(nextSpans, p, len(next)-s)}
}

// mergeSpans is a three-way merge of a block's text: local (the incoming
// edit) and remote (what the block holds now) both started from base. Changes
// in different places both apply, keeping local's formatting; overlapping
// changes keep local. The shared TS docsMergeText is the same algorithm.
func mergeSpans(base, local, remote []Span) []Span {
	if spansEqual(local, base) {
		return remote
	}
	if spansEqual(remote, base) || spansEqual(remote, local) {
		return local
	}
	bp, lp, rp := []rune(plainOf(base)), []rune(plainOf(local)), []rune(plainOf(remote))
	l := diffRegion(bp, lp, local)
	r := diffRegion(bp, rp, remote)
	if l.start < r.end && r.start < l.end {
		return local
	}
	if r.start == r.end && r.insertLen == 0 {
		return local
	}
	shift := l.insertLen - (l.end - l.start)
	rs, re := r.start, r.end
	if r.start >= l.end {
		rs, re = r.start+shift, r.end+shift
	}
	out := append(append(sliceSpans(local, 0, rs), r.insert...), sliceSpans(local, re, len(lp)+1)...)
	merged, _ := sanitizeSpans(out, spanRules{})
	return merged
}
