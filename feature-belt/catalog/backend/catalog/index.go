package catalog

import (
	"encoding/json"
	"math/bits"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// rec is one item as the index and the sync hold it: compact, with the
// attributes kept as JSON until someone asks for the item.
type rec struct {
	key, name, category, subtitle, imageURL string
	aliases                                 []string
	attrs                                   string // canonical JSON object; "" = none
	price                                   string // canonical decimal; "" = none
	currency                                string
	retiredBy                               string // "" while active
	retiredAt                               time.Time
	firstSeen, updatedAt                    time.Time
}

func (r rec) active() bool { return r.retiredBy == "" }

// index is an immutable view of the whole catalog. The Service swaps a new
// one in atomically after every change; readers keep the one they loaded.
type index struct {
	version  int64
	loaded   bool
	loadedAt time.Time

	recs  []rec    // by (name key, key)
	keys  []string // name keys, parallel to recs
	byKey map[string]int32
	byCat map[string][]int32 // category → items, in item order

	names   map[string][]int32 // name key → items
	aliases map[string][]int32 // alias key → items

	alias       []aliasEntry // every alias key, in item order
	aliasSorted []int32      // alias entries by key (prefix ranges)

	nameBlob, aliasBlob blob // substring scans

	fuzz  []fuzzEntry      // every name and alias key
	byLen map[int][]int32  // rune count → fuzz entries
	cats  []Category       // active items per category, largest first
	nAct  int              // active items
}

type aliasEntry struct {
	key  string
	item int32
}

type fuzzEntry struct {
	key  string
	mask uint64
	item int32
}

func emptyIndex() *index {
	x, _ := buildIndex(nil, 0)
	x.loaded = false
	return x
}

// buildIndex indexes recs (any order; keys must be unique).
func buildIndex(recs []rec, version int64) (*index, error) {
	nameKeys := make([]string, len(recs))
	order := make([]int, len(recs))
	for i, r := range recs {
		nameKeys[i], order[i] = NameKey(r.name), i
	}
	sort.Slice(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if nameKeys[i] != nameKeys[j] {
			return nameKeys[i] < nameKeys[j]
		}
		return recs[i].key < recs[j].key
	})
	n := len(recs)
	x := &index{version: version, loaded: true, loadedAt: time.Now().UTC(),
		recs: make([]rec, n), keys: make([]string, n), byKey: make(map[string]int32, n), byCat: map[string][]int32{},
		names: make(map[string][]int32, n), aliases: map[string][]int32{}, byLen: map[int][]int32{}}
	counts := map[string]int{}
	addFuzz := func(k string, i int32) {
		l := utf8.RuneCountInString(k)
		x.byLen[l] = append(x.byLen[l], int32(len(x.fuzz)))
		x.fuzz = append(x.fuzz, fuzzEntry{key: k, mask: runeMask(k), item: i})
	}
	for j, src := range order {
		i, r, k := int32(j), recs[src], nameKeys[src]
		if _, dup := x.byKey[r.key]; dup {
			return nil, errDuplicateKey(r.key)
		}
		x.recs[j], x.keys[j], x.byKey[r.key] = r, k, i
		x.byCat[r.category] = append(x.byCat[r.category], i)
		if r.active() {
			x.nAct++
			if r.category != "" {
				counts[r.category]++
			}
		}
		if k != "" {
			x.names[k] = append(x.names[k], i)
			addFuzz(k, i)
		}
		seen := []string{k}
		for _, a := range r.aliases {
			ak := NameKey(a)
			if ak == "" || slices.Contains(seen, ak) {
				continue
			}
			seen = append(seen, ak)
			x.aliases[ak] = append(x.aliases[ak], i)
			x.alias = append(x.alias, aliasEntry{ak, i})
			addFuzz(ak, i)
		}
	}
	x.aliasSorted = make([]int32, len(x.alias))
	for i := range x.aliasSorted {
		x.aliasSorted[i] = int32(i)
	}
	sort.SliceStable(x.aliasSorted, func(a, b int) bool { return x.alias[x.aliasSorted[a]].key < x.alias[x.aliasSorted[b]].key })
	x.nameBlob = newBlob(len(x.keys), func(i int) string { return x.keys[i] })
	x.aliasBlob = newBlob(len(x.alias), func(i int) string { return x.alias[i].key })
	for name, c := range counts {
		x.cats = append(x.cats, Category{Name: name, Count: c})
	}
	sort.Slice(x.cats, func(a, b int) bool {
		if x.cats[a].Count != x.cats[b].Count {
			return x.cats[a].Count > x.cats[b].Count
		}
		return x.cats[a].Name < x.cats[b].Name
	})
	return x, nil
}

type errDuplicateKey string

func (e errDuplicateKey) Error() string { return "catalog: duplicate key " + string(e) }

// item is recs[i] as the public type.
func (x *index) item(i int32) Item { return recItem(x.recs[i]) }

// recItem is a row as the public type.
func recItem(r rec) Item {
	it := Item{Key: r.key, Name: r.name, Aliases: r.aliases, Category: r.category, Subtitle: r.subtitle, ImageURL: r.imageURL,
		Attrs: map[string]string{}, Currency: r.currency, Retired: !r.active(), RetiredBy: r.retiredBy,
		FirstSeenAt: r.firstSeen, UpdatedAt: r.updatedAt}
	if it.Aliases == nil {
		it.Aliases = []string{}
	}
	if r.attrs != "" {
		_ = json.Unmarshal([]byte(r.attrs), &it.Attrs)
	}
	if r.price != "" {
		if d, err := decimal.NewFromString(r.price); err == nil {
			it.RefPrice = &d
		}
	}
	if !r.active() {
		at := r.retiredAt
		it.RetiredAt = &at
	}
	return it
}

func (x *index) items(is []int32) []Item {
	out := make([]Item, 0, len(is))
	for _, i := range is {
		out = append(out, x.item(i))
	}
	return out
}

func (x *index) categories() []Category { return slices.Clone(x.cats) }

func (x *index) keep(i int32, f Filter) bool {
	r := &x.recs[i]
	return (f.Retired || r.active()) && (f.Category == "" || r.category == f.Category)
}

// ---- Lookup ----

const (
	maxCandidates = 5
	fuzzyMinRunes = 6
	fuzzyMaxDist  = 2
)

func (x *index) lookup(q Query) Result {
	k := NameKey(q.Name)
	if k == "" {
		return Result{Reason: ReasonUnknown, Candidates: []Item{}}
	}
	if is := x.names[k]; len(is) > 0 {
		return x.pick(is, MatchExact)
	}
	if is := x.aliases[k]; len(is) > 0 {
		return x.pick(is, MatchAlias)
	}
	return x.fuzzy(k)
}

// one is the item a set of matches names: the only one, or the only one that
// isn't retired (a retired item never makes a name ambiguous).
func (x *index) one(is []int32) (int32, bool) {
	if len(is) == 1 {
		return is[0], true
	}
	found, n := int32(-1), 0
	for _, i := range is {
		if x.recs[i].active() {
			found, n = i, n+1
		}
	}
	return found, n == 1
}

func (x *index) pick(is []int32, match string) Result {
	if i, ok := x.one(is); ok {
		return Result{Item: x.item(i), OK: true, Match: match, Candidates: []Item{}}
	}
	return Result{Reason: ReasonAmbiguous, Candidates: x.items(is[:min(len(is), maxCandidates)])}
}

// fuzzy matches k within fuzzyMaxDist edits of any name or alias when one
// item holds the closest; otherwise k is unknown and the closest items (a
// tie) lead the candidates.
func (x *index) fuzzy(k string) Result {
	kr := []rune(k)
	var tied []int32
	if len(kr) >= fuzzyMinRunes {
		km := runeMask(k)
		best := fuzzyMaxDist + 1
		buf := make([]rune, 0, 64)
		for l := len(kr) - fuzzyMaxDist; l <= len(kr)+fuzzyMaxDist; l++ {
			for _, fi := range x.byLen[l] {
				e := x.fuzz[fi]
				lim := min(best, fuzzyMaxDist)
				if bits.OnesCount64(km^e.mask) > 2*lim {
					continue
				}
				buf = buf[:0]
				for _, r := range e.key {
					buf = append(buf, r)
				}
				d := distance(kr, buf, lim+1)
				if d > lim {
					continue
				}
				if d < best {
					best, tied = d, tied[:0]
				}
				if !slices.Contains(tied, e.item) {
					tied = append(tied, e.item)
				}
			}
		}
		slices.Sort(tied)
		if i, ok := x.one(tied); ok && len(tied) > 0 {
			return Result{Item: x.item(i), OK: true, Match: MatchFuzzy, Candidates: []Item{}}
		}
	}
	return Result{Reason: ReasonUnknown, Candidates: x.items(x.candidates(k, tied))}
}

// candidates are up to 5 suggestions for an unresolved k: the tied fuzzy
// matches, then names starting with k, aliases starting with k, names
// containing k, aliases containing k.
func (x *index) candidates(k string, tied []int32) []int32 {
	out := make([]int32, 0, maxCandidates)
	seen := map[int32]bool{}
	add := func(i int32) bool {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
		return len(out) < maxCandidates
	}
	for _, i := range tied {
		if !add(i) {
			return out
		}
	}
	for i := x.namePrefixStart(k); i < len(x.keys) && strings.HasPrefix(x.keys[i], k); i++ {
		if !add(int32(i)) {
			return out
		}
	}
	for _, i := range x.aliasPrefixItems(k) {
		if !add(i) {
			return out
		}
	}
	full := false
	x.nameBlob.each(k, func(i int32) bool { full = !add(i); return !full })
	if full {
		return out
	}
	x.aliasBlob.each(k, func(e int32) bool { return add(x.alias[e].item) })
	return out
}

func (x *index) namePrefixStart(k string) int { return sort.SearchStrings(x.keys, k) }

// aliasPrefixItems are the items with an alias starting with k, in item order.
func (x *index) aliasPrefixItems(k string) []int32 {
	lo := sort.Search(len(x.aliasSorted), func(j int) bool { return x.alias[x.aliasSorted[j]].key >= k })
	var out []int32
	for j := lo; j < len(x.aliasSorted); j++ {
		e := x.alias[x.aliasSorted[j]]
		if !strings.HasPrefix(e.key, k) {
			break
		}
		out = append(out, e.item)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ---- Search ----

// Ranks, best first.
const (
	rankExact     = iota // the name
	rankPrefix           // the name starts with q
	rankWord             // a later word of the name starts with q
	rankSubstring        // the name contains q
	rankAlias            // an alias is q or starts with it
	rankAliasSub         // an alias contains q
)

type hit struct {
	i    int32
	rank int
}

func (x *index) search(q string, f Filter, limit int) []hit {
	k := NameKey(q)
	if utf8.RuneCountInString(k) < 2 {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 50)
	out := make([]hit, 0, limit)
	seen := map[int32]bool{}
	add := func(i int32, rank int) bool {
		if !seen[i] && x.keep(i, f) {
			seen[i] = true
			out = append(out, hit{i, rank})
		}
		return len(out) < limit
	}
	for _, i := range x.names[k] {
		if !add(i, rankExact) {
			return out
		}
	}
	for i := x.namePrefixStart(k); i < len(x.keys) && strings.HasPrefix(x.keys[i], k); i++ {
		if !add(int32(i), rankPrefix) {
			return out
		}
	}
	// a later word, then anywhere: one scan, two buckets
	need := limit - len(out)
	var word, sub []int32
	x.nameBlob.each(k, func(i int32) bool {
		if seen[i] || !x.keep(i, f) {
			return true
		}
		if wordStart(x.keys[i], k) {
			word = append(word, i)
		} else if len(sub) < need {
			sub = append(sub, i)
		}
		return len(word) < need
	})
	for _, i := range word {
		if !add(i, rankWord) {
			return out
		}
	}
	for _, i := range sub {
		if !add(i, rankSubstring) {
			return out
		}
	}
	for _, i := range x.aliasPrefixItems(k) {
		if !add(i, rankAlias) {
			return out
		}
	}
	x.aliasBlob.each(k, func(e int32) bool { return add(x.alias[e].item, rankAliasSub) })
	return out
}

// wordStart reports whether k occurs in key right after a non-letter (a later word).
func wordStart(key, k string) bool {
	for off := 0; ; {
		i := strings.Index(key[off:], k)
		if i < 0 {
			return false
		}
		i += off
		if i == 0 {
			return true
		}
		prev, _ := utf8.DecodeLastRuneInString(key[:i])
		if !unicode.IsLetter(prev) && !unicode.IsDigit(prev) && prev != '\'' {
			return true
		}
		off = i + 1
	}
}

// browse lists items in name order after the cursor (name key, key).
func (x *index) browse(f Filter, afterName, afterKey string, limit int) []int32 {
	after := func(i int32) bool {
		return x.keys[i] > afterName || (x.keys[i] == afterName && x.recs[i].key > afterKey)
	}
	n, at := len(x.recs), func(j int) int32 { return int32(j) }
	if f.Category != "" {
		pool := x.byCat[f.Category]
		n, at = len(pool), func(j int) int32 { return pool[j] }
	}
	start := 0
	if afterKey != "" {
		start = sort.Search(n, func(j int) bool { return after(at(j)) })
	}
	out := make([]int32, 0, limit)
	for j := start; j < n && len(out) < limit; j++ {
		if i := at(j); x.keep(i, f) {
			out = append(out, i)
		}
	}
	return out
}

// ---- substring scans ----

// blob is a list of keys joined by "\n" (keys never contain one), so one
// strings.Index call scans them all.
type blob struct {
	s      string
	starts []int32 // where entry i starts in s
}

func newBlob(n int, key func(int) string) blob {
	var b strings.Builder
	starts := make([]int32, n)
	for i := 0; i < n; i++ {
		starts[i] = int32(b.Len())
		b.WriteString(key(i))
		b.WriteByte('\n')
	}
	return blob{s: b.String(), starts: starts}
}

// each calls fn with every entry that contains k, in entry order, until fn
// returns false.
func (b blob) each(k string, fn func(entry int32) bool) {
	if k == "" || strings.Contains(k, "\n") {
		return
	}
	for off := 0; off < len(b.s); {
		j := strings.Index(b.s[off:], k)
		if j < 0 {
			return
		}
		pos := int32(off + j)
		e := sort.Search(len(b.starts), func(m int) bool { return b.starts[m] > pos }) - 1
		if !fn(int32(e)) || e+1 >= len(b.starts) {
			return
		}
		off = int(b.starts[e+1])
	}
}
