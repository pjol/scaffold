package baskets

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"scaffoldapp/backend/internal/exchange"
)

// Candidate is a listed exchange market a line may name.
type Candidate struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Category string `json:"category"`
	ImageURL string `json:"image_url"`
}

// How a line resolved (Resolution.Match).
const (
	MatchSymbol = "symbol" // the market's symbol
	MatchName   = "name"   // the market's name
	MatchAlias  = "alias"  // another name the resolver knows (the default: the price feed key)
	MatchFuzzy  = "fuzzy"  // within 2 edits of one name (names of 6+ characters); warned
	MatchPicked = "picked" // picked from search
)

// Why a line didn't resolve (Resolution.Reason).
const (
	ReasonUnknown   = "unknown"    // nothing matches
	ReasonAmbiguous = "ambiguous"  // several markets match equally well
	ReasonNotListed = "not_listed" // the resolver named a market the exchange doesn't list
	ReasonBasket    = "basket"     // a basket can't hold another basket
	ReasonHalted    = "halted"     // the market isn't trading
)

// Resolution is how one line resolved. Exactly one of OK or Reason is set.
type Resolution struct {
	Market     Candidate // set when OK (Symbol is an exchange symbol)
	OK         bool
	Match      string
	Reason     string
	Candidates []Candidate // up to 5 suggestions when not OK
}

// Resolver finds the market a pasted name means. The default resolves
// against the exchange's listed markets; SetResolver plugs in a larger
// reference catalog. Both run as the server, never with user credentials.
type Resolver interface {
	Resolve(ctx context.Context, text string) (Resolution, error)
	// Search ranks markets for a picker (q may be empty: the first markets).
	Search(ctx context.Context, q string, limit int) ([]Candidate, error)
}

// ---- normalisation and edit distance ----

var fold = strings.NewReplacer("æ", "ae", "œ", "oe", "ø", "o", "ł", "l", "đ", "d", "ß", "ss", "ı", "i", "&", " and ")

// normKey folds a name for matching: accents dropped, lower case,
// punctuation and symbols as spaces, whitespace collapsed ("BTC/USD" and
// "btc-usd" both become "btc usd").
func normKey(s string) string {
	s = fold.Replace(strings.ToLower(norm.NFKD.String(s)))
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		default:
			if !space {
				b.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// distance is the optimal-string-alignment (restricted Damerau-Levenshtein)
// distance between a and b, or limit when it is at least limit.
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

// ---- the default resolver: the exchange's markets ----

// marketIndex is a snapshot of the exchange's catalog for resolving and
// searching, without baskets (a basket can't be a line).
type marketIndex struct {
	markets  []exchange.ListedMarket
	bySymbol map[string]int                   // exact symbol
	symKeys  map[string][]int                 // normKey(symbol)
	names    map[string][]int                 // normKey(name)
	aliases  map[string][]int                 // normKey(price feed key)
	baskets  map[string]exchange.ListedMarket // basket markets by symbol (never lines)
	keys     []string                         // normKey(name) per market, for fuzzy and candidates
}

func newMarketIndex(ms []exchange.ListedMarket, basketSymbols map[string]bool) *marketIndex {
	ix := &marketIndex{bySymbol: map[string]int{}, symKeys: map[string][]int{}, names: map[string][]int{}, aliases: map[string][]int{},
		baskets: map[string]exchange.ListedMarket{}}
	for _, m := range ms {
		if basketSymbols[m.Symbol] {
			ix.baskets[m.Symbol] = m
			continue
		}
		i := len(ix.markets)
		ix.markets = append(ix.markets, m)
		ix.bySymbol[m.Symbol] = i
		k := normKey(m.Name)
		ix.keys = append(ix.keys, k)
		ix.symKeys[normKey(m.Symbol)] = append(ix.symKeys[normKey(m.Symbol)], i)
		ix.names[k] = append(ix.names[k], i)
		if m.PriceFeed != "" && m.PriceFeed != m.Symbol {
			a := normKey(m.PriceFeed)
			ix.aliases[a] = append(ix.aliases[a], i)
		}
	}
	return ix
}

func candidateOf(m exchange.ListedMarket) Candidate {
	return Candidate{Symbol: m.Symbol, Name: m.Name, Category: m.Category, ImageURL: m.ImageURL}
}

func (ix *marketIndex) cands(is []int) []Candidate {
	out := make([]Candidate, 0, min(len(is), 5))
	for _, i := range is {
		if len(out) == 5 {
			break
		}
		out = append(out, candidateOf(ix.markets[i]))
	}
	return out
}

// Resolve: symbol key, then name, then alias, then fuzzy, else up to 5 candidates.
func (ix *marketIndex) Resolve(_ context.Context, text string) (Resolution, error) {
	k := normKey(text)
	if k == "" {
		return Resolution{Reason: ReasonUnknown}, nil
	}
	for _, step := range []struct {
		m     map[string][]int
		match string
	}{{ix.symKeys, MatchSymbol}, {ix.names, MatchName}, {ix.aliases, MatchAlias}} {
		switch is := step.m[k]; len(is) {
		case 0:
		case 1:
			return Resolution{OK: true, Match: step.match, Market: candidateOf(ix.markets[is[0]])}, nil
		default:
			return Resolution{Reason: ReasonAmbiguous, Candidates: ix.cands(is)}, nil
		}
	}
	if kr := []rune(k); len(kr) >= 6 {
		best, hits := 3, map[int]bool{}
		for i, n := range ix.keys {
			d := distance(kr, []rune(n), min(best, 2)+1)
			if d > min(best, 2) {
				continue
			}
			if d < best {
				best, hits = d, map[int]bool{}
			}
			hits[i] = true
		}
		if best <= 2 && len(hits) == 1 {
			for i := range hits {
				return Resolution{OK: true, Match: MatchFuzzy, Market: candidateOf(ix.markets[i])}, nil
			}
		}
	}
	return Resolution{Reason: ReasonUnknown, Candidates: ix.suggest(k)}, nil
}

// suggest is up to 5 candidates for an unmatched key: names starting with
// it, then containing it, then names within 2 edits (short typos: "silvr").
func (ix *marketIndex) suggest(k string) []Candidate {
	out := ix.search(k, 5)
	if len(out) == 5 {
		return out
	}
	seen := map[string]bool{}
	for _, c := range out {
		seen[c.Symbol] = true
	}
	kr := []rune(k)
	for d := 1; d <= 2 && len(out) < 5; d++ {
		for i, n := range ix.keys {
			if len(out) == 5 {
				break
			}
			if m := ix.markets[i]; !seen[m.Symbol] && distance(kr, []rune(n), d+1) == d {
				seen[m.Symbol] = true
				out = append(out, candidateOf(m))
			}
		}
	}
	return out
}

// Search: symbol or name prefix, then a word of the name starting with q,
// then anywhere in the symbol or name; catalog order within each rank.
func (ix *marketIndex) Search(_ context.Context, q string, limit int) ([]Candidate, error) {
	return ix.search(normKey(q), limit), nil
}

func (ix *marketIndex) search(k string, limit int) []Candidate {
	limit = max(1, min(limit, 50))
	type hit struct{ i, rank int }
	var hits []hit
	for i, m := range ix.markets {
		sk, nk := normKey(m.Symbol), ix.keys[i]
		rank := -1
		switch {
		case k == "":
			rank = 3
		case strings.HasPrefix(sk, k) || strings.HasPrefix(nk, k):
			rank = 0
		case strings.Contains(" "+nk, " "+k):
			rank = 1
		case strings.Contains(sk, k) || strings.Contains(nk, k):
			rank = 2
		}
		if rank >= 0 {
			hits = append(hits, hit{i, rank})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].rank < hits[b].rank })
	out := make([]Candidate, 0, min(len(hits), limit))
	for _, h := range hits {
		if len(out) == limit {
			break
		}
		out = append(out, candidateOf(ix.markets[h.i]))
	}
	return out
}

// marketCache keeps the index for 30 s; creating a basket clears it.
type marketCache struct {
	mu sync.Mutex
	at time.Time
	ix *marketIndex
}

func (c *marketCache) clear() {
	c.mu.Lock()
	c.ix = nil
	c.mu.Unlock()
}

// index returns the current market index, rebuilding it when older than 30 s.
func (s *Service) index(ctx context.Context) (*marketIndex, error) {
	s.markets.mu.Lock()
	defer s.markets.mu.Unlock()
	if s.markets.ix != nil && time.Since(s.markets.at) < 30*time.Second {
		return s.markets.ix, nil
	}
	ms, err := exchange.Markets(ctx, s.d.Pool)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT symbol FROM baskets`)
	if err != nil {
		return nil, err
	}
	bs := map[string]bool{}
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			rows.Close()
			return nil, err
		}
		bs[sym] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.markets.ix, s.markets.at = newMarketIndex(ms, bs), time.Now()
	return s.markets.ix, nil
}

// resolver is the app's Resolver, or the exchange's markets.
func (s *Service) resolver(ix *marketIndex) Resolver {
	if r := appResolver(); r != nil {
		return r
	}
	return ix
}
