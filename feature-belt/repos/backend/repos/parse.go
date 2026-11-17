package repos

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Pure text rules: issue references and closing keywords, mentions, the
// issue search syntax, suggestion blocks and CODEOWNERS. Unit-tested in
// parse_test.go.

// ---- references ----------------------------------------------------------------

// TextRef is "#12" written in text; Closes when a closing keyword precedes it
// ("Fixes #12", "closes: #3", "Resolved #4").
type TextRef struct {
	Number int
	Closes bool
}

var (
	fencedCode = regexp.MustCompile("(?s)(^|\n)(```|~~~)[^\n]*\n.*?(\n(```|~~~)[ \t]*(\n|$)|$)")
	inlineCode = regexp.MustCompile("`[^`\n]*`")
	refRe      = regexp.MustCompile(`(?i)(?:^|[^\w&/#])(?:(close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+)?#(\d{1,9})\b`)
	mentionRe  = regexp.MustCompile(`<@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>`)
)

// stripCode removes fenced code blocks and inline code spans (references and
// mentions inside code don't count).
func stripCode(text string) string {
	text = fencedCode.ReplaceAllString(text, "\n")
	return inlineCode.ReplaceAllString(text, " ")
}

// ParseRefs lists the issue and pull request numbers text refers to, in
// order of first appearance; a number written with a closing keyword
// anywhere closes.
func ParseRefs(text string) []TextRef {
	text = stripCode(text)
	idx := map[int]int{}
	var out []TextRef
	for _, m := range refRe.FindAllStringSubmatch(text, 200) {
		n, err := strconv.Atoi(m[2])
		if err != nil || n < 1 {
			continue
		}
		closes := m[1] != ""
		if i, ok := idx[n]; ok {
			out[i].Closes = out[i].Closes || closes
			continue
		}
		idx[n] = len(out)
		out = append(out, TextRef{Number: n, Closes: closes})
	}
	return out
}

// ParseMentions lists the people mentioned with <@uuid> tokens, once each.
func ParseMentions(text string) []uuid.UUID {
	text = stripCode(text)
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, m := range mentionRe.FindAllStringSubmatch(text, 100) {
		id, err := uuid.Parse(m[1])
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ---- search syntax -------------------------------------------------------------

// IssueQuery is a parsed filter like `is:open label:bug assignee:@me milestone:"v1.0" crash`.
type IssueQuery struct {
	State         string   // open | closed | merged | "" (any)
	Kind          string   // issue | pr | ""
	Labels        []string // all of them (lowercase)
	NotLabels     []string
	NoLabel       bool
	Assignee      string // "@me", a name/email, or ""
	NoAssignee    bool
	Author        string
	Mentions      string
	Milestone     string // title (lowercase)
	NoMilestone   bool
	Reason        string // completed | not_planned
	Draft         *bool
	Locked        *bool
	Pinned        bool
	ReviewRequest string // "@me" or a name
	Sort          string // created | created-asc | updated | updated-asc | comments
	Text          string // free words
	Unknown       []string
}

// tokenize splits on spaces outside double quotes; qualifiers keep their
// quoted value together (label:"good first issue").
func tokenize(q string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range q {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t' || r == '\n') && !quoted:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// ParseIssueQuery reads the search syntax shared by the issue and pull
// request lists. Unknown qualifiers are kept as text-free notes (Unknown)
// so the screen can say they were ignored.
func ParseIssueQuery(q string) IssueQuery {
	var out IssueQuery
	var words []string
	t := true
	f := false
	for _, tok := range tokenize(q) {
		neg := strings.HasPrefix(tok, "-")
		key, val, ok := strings.Cut(strings.TrimPrefix(tok, "-"), ":")
		if !ok || key == "" || val == "" {
			words = append(words, tok)
			continue
		}
		key = strings.ToLower(key)
		lval := strings.ToLower(val)
		switch key {
		case "is", "state":
			switch lval {
			case "open", "closed", "merged":
				out.State = lval
			case "issue":
				out.Kind = "issue"
			case "pr", "pull", "pull-request":
				out.Kind = "pr"
			case "draft":
				if neg {
					out.Draft = &f
				} else {
					out.Draft = &t
				}
			case "locked":
				out.Locked = &t
			case "unlocked":
				out.Locked = &f
			case "pinned":
				out.Pinned = true
			default:
				out.Unknown = append(out.Unknown, tok)
			}
		case "label":
			for _, l := range strings.Split(lval, ",") {
				if l = strings.TrimSpace(l); l == "" {
					continue
				}
				if neg {
					out.NotLabels = append(out.NotLabels, l)
				} else {
					out.Labels = append(out.Labels, l)
				}
			}
		case "no":
			switch lval {
			case "label", "labels":
				out.NoLabel = true
			case "assignee", "assignees":
				out.NoAssignee = true
			case "milestone":
				out.NoMilestone = true
			default:
				out.Unknown = append(out.Unknown, tok)
			}
		case "assignee":
			out.Assignee = lval
		case "author":
			out.Author = lval
		case "mentions":
			out.Mentions = lval
		case "milestone":
			out.Milestone = lval
		case "reason":
			switch strings.ReplaceAll(strings.ReplaceAll(lval, " ", "_"), "-", "_") {
			case "completed":
				out.Reason = "completed"
			case "not_planned":
				out.Reason = "not_planned"
			default:
				out.Unknown = append(out.Unknown, tok)
			}
		case "draft":
			v := lval == "true" || lval == "yes"
			out.Draft = &v
		case "review-requested":
			out.ReviewRequest = lval
		case "sort":
			switch lval {
			case "created", "created-desc":
				out.Sort = "created"
			case "created-asc", "oldest":
				out.Sort = "created-asc"
			case "updated", "updated-desc":
				out.Sort = "updated"
			case "updated-asc":
				out.Sort = "updated-asc"
			case "comments", "comments-desc":
				out.Sort = "comments"
			default:
				out.Unknown = append(out.Unknown, tok)
			}
		default:
			words = append(words, tok)
		}
	}
	out.Text = strings.Join(words, " ")
	return out
}

// ---- suggested changes ---------------------------------------------------------

var suggestionRe = regexp.MustCompile("(?s)(?:^|\n)```suggestion[ \t]*\r?\n(.*?)(?:\r?\n)?```")

// ParseSuggestion returns the replacement text of the first ```suggestion
// block in a comment (ok=false when there is none). An empty block deletes
// the lines.
func ParseSuggestion(body string) (string, bool) {
	m := suggestionRe.FindStringSubmatch(body)
	if m == nil {
		return "", false
	}
	return strings.ReplaceAll(m[1], "\r\n", "\n"), true
}

// ApplySuggestion replaces lines start..end (1-based, inclusive) of text.
func ApplySuggestion(text string, start, end int, repl string) (string, bool) {
	trailing := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	if start < 1 || end < start || end > len(lines) {
		return "", false
	}
	var mid []string
	if repl != "" {
		mid = strings.Split(strings.TrimSuffix(repl, "\n"), "\n")
	}
	out := append(append(append([]string{}, lines[:start-1]...), mid...), lines[end:]...)
	s := strings.Join(out, "\n")
	if trailing && len(out) > 0 {
		s += "\n"
	}
	return s, true
}

// ---- CODEOWNERS ----------------------------------------------------------------

// CodeOwnersRule is one line of a CODEOWNERS file.
type CodeOwnersRule struct {
	Pattern string
	Owners  []string // emails and @team handles, as written
	Line    int
}

// codeOwnersPaths are where GitHub looks, in order.
var codeOwnersPaths = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

// ParseCodeOwners reads a CODEOWNERS file (comments with #, one pattern and
// its owners per line; a pattern with no owners un-owns its paths).
func ParseCodeOwners(src string) []CodeOwnersRule {
	var out []CodeOwnersRule
	for i, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if j := strings.Index(line, " #"); j >= 0 {
			line = strings.TrimSpace(line[:j])
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		r := CodeOwnersRule{Pattern: strings.ReplaceAll(fields[0], `\ `, " "), Line: i + 1}
		for _, o := range fields[1:] {
			if strings.Contains(o, "@") {
				r.Owners = append(r.Owners, o)
			}
		}
		out = append(out, r)
	}
	return out
}

// OwnersOf returns the owners of a path: the last matching rule wins.
func OwnersOf(rules []CodeOwnersRule, p string) []string {
	for i := len(rules) - 1; i >= 0; i-- {
		if codeOwnersMatch(rules[i].Pattern, p) {
			return rules[i].Owners
		}
	}
	return nil
}

// codeOwnersMatch follows gitignore-style rules: a leading / anchors at the
// root, a pattern without a slash matches a name at any depth, a trailing /
// matches a directory's contents, ** spans directories, * and ? stay in one
// segment.
func codeOwnersMatch(pattern, p string) bool {
	if pattern == "*" || pattern == "**" || pattern == "/**" {
		return true
	}
	dir := strings.HasSuffix(pattern, "/")
	pattern = strings.TrimSuffix(pattern, "/")
	anchored := strings.HasPrefix(pattern, "/") || strings.Contains(strings.TrimPrefix(pattern, "/"), "/")
	pattern = strings.TrimPrefix(pattern, "/")
	if pattern == "" {
		return true
	}
	if !anchored {
		pattern = "**/" + pattern
	}
	// A match on a directory owns everything under it.
	if globMatch(pattern+"/**", p) {
		return true
	}
	if dir {
		return false
	}
	return globMatch(pattern, p)
}

// globMatch matches segment by segment; "**" matches zero or more segments.
func globMatch(pattern, p string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// ---- status aggregation --------------------------------------------------------

// CombinedState folds commit statuses into one: failure if any failed or
// errored, pending if any is pending, success when all succeeded, "" with none.
func CombinedState(states []string) string {
	if len(states) == 0 {
		return ""
	}
	pending := false
	for _, s := range states {
		switch s {
		case "failure", "error":
			return "failure"
		case "pending":
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "success"
}

// MissingChecks lists required contexts without a success, in order: the
// ones never reported, then pending or failing ones.
func MissingChecks(required []string, latest map[string]string) (missing, failing, pending []string) {
	for _, c := range required {
		switch latest[c] {
		case "success":
		case "":
			missing = append(missing, c)
		case "pending":
			pending = append(pending, c)
		default:
			failing = append(failing, c)
		}
	}
	sort.Strings(missing)
	return missing, failing, pending
}
