package search

// Operators across providers. A query like "launch from:@sam in:#general
// type:message during:week" is parsed once (app.ParseSearchQuery). Each
// provider then gets what it can use:
//
//   - a provider that reads the grammar from its text (rawOperators: the
//     channels feature) gets the query with the operators it knows;
//   - an app.SearchOperatorProvider gets the free text, and the filter in
//     its context (app.SearchFilterFrom) for the operators it declared;
//   - every other provider gets the free text.
//
// Search applies the rest to the hits itself: type: on Kind, dates on At,
// in: on Context, from: on AuthorID. An operator that neither side can
// apply (is:, has:, with:, to: for most features) skips the provider, and
// the response says why; so does a query with no words for a provider that
// can't list by operators alone.

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

var (
	rawMu sync.RWMutex
	// rawOperators: modules whose Search reads the query grammar from its
	// text, and the operators they apply. channels parses in/from/with/to/
	// has/is and dates in its own search.
	rawOperators = map[string][]string{
		"channels": {"from", "in", "is", "has", "with", "to", "date"},
	}
)

// SetRawOperators declares that module's Search reads the query grammar
// from its text and applies ops ("from", "in", "type", "is", "has", "with",
// "to", "date"); nil ops removes it. Prefer app.SearchOperatorProvider in
// new providers.
func SetRawOperators(module string, ops ...string) {
	rawMu.Lock()
	defer rawMu.Unlock()
	if len(ops) == 0 {
		delete(rawOperators, module)
		return
	}
	rawOperators[module] = ops
}

func rawFor(module string) ([]string, bool) {
	rawMu.RLock()
	defer rawMu.RUnlock()
	ops, ok := rawOperators[module]
	return ops, ok
}

var searchOpTokenRE = regexp.MustCompile(app.SearchOperatorPattern)

// generic are the operators search can apply to any provider's hits.
var generic = []string{"type", "date", "in", "from"}

// plan is how one provider takes part in a query.
type plan struct {
	text    string          // what its Search gets
	handled map[string]bool // operators it applies itself
	skip    string          // why it is left out ("" = asked)
}

// opKeys maps the grammar's keys to the operator names providers declare.
var opKeys = map[string]string{"from": "from", "in": "in", "type": "type", "is": "is", "has": "has", "with": "with", "to": "to",
	"before": "date", "after": "date", "on": "date", "during": "date"}

func planFor(m app.Module, raw string, q app.SearchQuery) plan {
	if !q.Filtered() {
		return plan{text: raw, handled: map[string]bool{}}
	}
	pl := plan{text: q.Text, handled: map[string]bool{}}
	raws, isRaw := rawFor(m.Name())
	if isRaw {
		for _, op := range raws {
			pl.handled[op] = true
		}
		pl.text = keepOperators(raw, pl.handled)
	} else if op, ok := m.(app.SearchOperatorProvider); ok {
		for _, o := range op.SearchOperators() {
			pl.handled[o] = true
		}
	}
	for _, op := range q.Ops {
		if !pl.handled[op] && !slices.Contains(generic, op) {
			pl.skip = op + ": isn't supported"
			return pl
		}
	}
	// Free text is what a plain provider searches with; operators alone
	// need a provider that applies at least one of them itself.
	if len([]rune(q.Text)) < MinQueryChars && len(pl.handled) == 0 {
		pl.skip = "needs words to search for"
	} else if len([]rune(q.Text)) < MinQueryChars && !isRaw {
		any := false
		for _, op := range q.Ops {
			any = any || pl.handled[op]
		}
		if !any {
			pl.skip = "needs words to search for"
		}
	}
	return pl
}

// keepOperators rebuilds raw with only the operators a raw provider applies
// (the others would read as words to it).
func keepOperators(raw string, keep map[string]bool) string {
	out := searchOpTokenRE.ReplaceAllStringFunc(raw, func(tok string) string {
		m := searchOpTokenRE.FindStringSubmatch(tok)
		if keep[opKeys[strings.ToLower(m[2])]] {
			return tok
		}
		return m[1]
	})
	return strings.Join(strings.Fields(out), " ")
}

// applyOperators keeps the hits that pass the operators the provider didn't
// apply itself.
func applyOperators(hits []app.SearchResult, q app.SearchQuery, fromIDs []uuid.UUID, handled map[string]bool) []app.SearchResult {
	if !q.Filtered() {
		return hits
	}
	from := map[string]bool{}
	for _, id := range fromIDs {
		from[id.String()] = true
	}
	out := hits[:0]
	for _, h := range hits {
		switch {
		case q.Uses("type") && !handled["type"] && !typeMatches(h.Kind, q.Types):
		case q.Uses("date") && !handled["date"] && ((q.After != nil && h.At.Before(*q.After)) || (q.Before != nil && !h.At.Before(*q.Before))):
		case q.Uses("in") && !handled["in"] && !contextMatches(h.Context, q.In):
		case q.Uses("from") && !handled["from"] && !from[strings.ToLower(h.AuthorID)]:
		default:
			out = append(out, h)
		}
	}
	return out
}

// typeAliases maps words people type to a kind's last part.
var typeAliases = map[string]string{
	"doc": "page", "document": "page", "note": "page", "msg": "message", "post": "message", "dm": "message",
	"pr": "pull_request", "pull": "pull_request", "pullrequest": "pull_request", "repo": "repository",
	"person": "contact", "people": "contact", "organization": "company", "org": "company", "call": "meeting",
	"booking": "booking_link", "folder": "item",
}

// typeMatches: kind is "docs.page"; a value matches the kind, its feature
// or its last part, singular or plural, or an alias of it.
func typeMatches(kind string, values []string) bool {
	module, last, _ := strings.Cut(kind, ".")
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		single := singular(v)
		if a, ok := typeAliases[single]; ok {
			single = a
		}
		if v == kind || v == module || single == last || (single == "file" && module == "files") {
			return true
		}
	}
	return false
}

func singular(v string) string {
	switch {
	case strings.HasSuffix(v, "ies"):
		return strings.TrimSuffix(v, "ies") + "y"
	case strings.HasSuffix(v, "ches"), strings.HasSuffix(v, "shes"), strings.HasSuffix(v, "sses"), strings.HasSuffix(v, "xes"):
		return strings.TrimSuffix(v, "es")
	case strings.HasSuffix(v, "s") && !strings.HasSuffix(v, "ss"):
		return strings.TrimSuffix(v, "s")
	}
	return v
}

// contextMatches: where the hit lives ("Acme · #general", "Acme · Drive ·
// Q3") contains every word of one in: value, case-insensitively.
func contextMatches(context string, values []string) bool {
	ctx := strings.ToLower(context)
	for _, v := range values {
		ok := v != ""
		for _, w := range strings.Fields(strings.ToLower(v)) {
			ok = ok && strings.Contains(ctx, strings.TrimPrefix(w, "#"))
		}
		if ok {
			return true
		}
	}
	return false
}

// resolveFrom turns from: names into people: "me" is the caller; a name
// matches active accounts whose display name or email starts with it (the
// channels rule). Matching ids only narrows hits the caller may already
// see, so it reveals no one.
func (s *Service) resolveFrom(ctx context.Context, caller *auth.Identity, values []string) ([]uuid.UUID, []FromFilter, error) {
	var ids []uuid.UUID
	out := []FromFilter{}
	for _, v := range values {
		f := FromFilter{Value: v, Names: []string{}}
		if v == "me" {
			ids = append(ids, caller.UserID)
			f.Names = append(f.Names, "You")
			out = append(out, f)
			continue
		}
		like := escapeLike(v)
		rows, err := s.d.Pool.Query(ctx, `
			SELECT id, display_name FROM users
			WHERE status IN ('active', 'suspended')
			  AND (lower(display_name) LIKE $1 || '%' ESCAPE '\' OR lower(display_name) LIKE '% ' || $1 || '%' ESCAPE '\'
			       OR lower(split_part(email, '@', 1)) LIKE $1 || '%' ESCAPE '\')
			ORDER BY display_name LIMIT 20`, like)
		if err != nil {
			return ids, out, err
		}
		type person struct {
			id   uuid.UUID
			name string
		}
		people, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (person, error) {
			var p person
			return p, r.Scan(&p.id, &p.name)
		})
		if err != nil {
			return ids, out, err
		}
		for _, p := range people {
			ids = append(ids, p.id)
			if len(f.Names) < 3 {
				f.Names = append(f.Names, p.name)
			}
		}
		out = append(out, f)
	}
	return ids, out, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(s))
}

// windowSnippets starts a long snippet near the first word of the query it
// contains, so the match is visible (clients highlight the words).
func windowSnippets(hits []app.SearchResult, text string) {
	words := strings.Fields(strings.ToLower(text))
	for i := range hits {
		sn := hits[i].Snippet
		r := []rune(sn)
		if len(r) < 120 || len(words) == 0 {
			continue
		}
		lower := []rune(strings.ToLower(sn))
		at := -1
		for _, w := range words {
			if len([]rune(w)) < 2 {
				continue
			}
			if idx := runeIndex(lower, []rune(w)); idx >= 0 && (at < 0 || idx < at) {
				at = idx
			}
		}
		if at > 60 {
			start := at - 40
			for start < at && r[start] != ' ' { // start on a word
				start++
			}
			hits[i].Snippet = "…" + strings.TrimSpace(string(r[start:]))
		}
	}
}

func runeIndex(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if string(s[i:i+len(sub)]) == string(sub) {
			return i
		}
	}
	return -1
}
