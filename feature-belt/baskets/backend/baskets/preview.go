package baskets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/oracle"
)

// Input is a basket as people send it: a pasted list, picked markets, or both.
type Input struct {
	Text  string `json:"text"`
	Picks []Pick `json:"picks"`
}

// Pick is a market chosen from search.
type Pick struct {
	Symbol   string `json:"symbol"`
	Quantity int    `json:"quantity"`
}

// Line statuses (PreviewLine.Status).
const (
	LineResolved   = "resolved"
	LineUnresolved = "unresolved"
	LineUnpriced   = "unpriced"
)

// PreviewLine is one input line after resolution and pricing.
type PreviewLine struct {
	Line        int         `json:"line"` // paste line or CSV row; 0 for picks
	Text        string      `json:"text"`
	Quantity    int         `json:"quantity"`
	Status      string      `json:"status"`
	Match       string      `json:"match,omitempty"`
	Market      *Candidate  `json:"market"`
	Price       *string     `json:"price"`
	PriceStatus string      `json:"price_status,omitempty"` // the oracle's: live, stale, seed, off
	Reason      string      `json:"reason,omitempty"`
	Message     string      `json:"message,omitempty"` // why it didn't resolve or price, in words
	Candidates  []Candidate `json:"candidates"`
}

// BasketLine is one canonical line with its price and weight.
type BasketLine struct {
	Symbol      string  `json:"symbol"`
	Name        string  `json:"name"`
	Quantity    int     `json:"quantity"`
	Price       *string `json:"price"`
	Value       *string `json:"value"`
	Weight      *string `json:"weight"` // share of the basket value, 0–1
	PriceStatus string  `json:"price_status"`
}

// Existing is the basket that already has these exact lines.
type Existing struct {
	Symbol  string `json:"symbol"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Hidden  bool   `json:"hidden"`
	Entries int    `json:"entries"`
	Entered bool   `json:"entered"` // the caller registered it already
}

// Preview is POST /v1/baskets/preview: everything create would do, without writing.
type Preview struct {
	Lines     []PreviewLine `json:"lines"`
	Warnings  []string      `json:"warnings"`
	Basket    []BasketLine  `json:"basket"`
	Value     string        `json:"value"`     // Σ quantity × price over the priced lines
	MaxShare  string        `json:"max_share"` // the largest line's share of the value, 0–1
	Blockers  []string      `json:"blockers"`
	Creatable bool          `json:"creatable"`      // create (or join Existing) would succeed, rate limits aside
	Symbol    *string       `json:"symbol"`         // the symbol it has or would get
	Spec      *Spec         `json:"spec"`           // the market it has or would get
	Existing  *Existing     `json:"existing"`       // set when the same basket exists
	Hash      string        `json:"hash,omitempty"` // canonical hash ("" when nothing resolved)
}

// bitem is one canonical line during evaluation.
type bitem struct {
	symbol, name, feed string
	qty                int
	price              decimal.Decimal
	priced             bool
	status             string
}

type evaluation struct {
	lines      []PreviewLine
	warnings   []string
	items      []bitem // sorted by symbol
	value      decimal.Decimal
	maxShare   decimal.Decimal
	hash       string
	unresolved int
	unpriced   []string // names
	overfull   []string // names whose merged quantity is over the maximum
	ix         *marketIndex
}

// totals computes the value, the largest share, the unpriced and overfull
// lines and the hash from the items.
func (ev *evaluation) totals() {
	ev.value, ev.maxShare, ev.unpriced, ev.overfull, ev.hash = decimal.Zero, decimal.Zero, nil, nil, ""
	for _, it := range ev.items {
		if it.qty > maxQuantity {
			ev.overfull = append(ev.overfull, it.name)
		}
		if it.priced {
			ev.value = ev.value.Add(it.price.Mul(decimal.NewFromInt(int64(it.qty))))
		} else {
			ev.unpriced = append(ev.unpriced, it.name)
		}
	}
	if ev.value.IsPositive() {
		for _, it := range ev.items {
			if v := it.price.Mul(decimal.NewFromInt(int64(it.qty))).Div(ev.value); it.priced && v.GreaterThan(ev.maxShare) {
				ev.maxShare = v
			}
		}
	}
	if len(ev.items) > 0 {
		ev.hash = Hash(ev.canonical())
	}
}

func (ev *evaluation) canonical() []Item {
	out := make([]Item, len(ev.items))
	for i, it := range ev.items {
		out[i] = Item{Symbol: it.symbol, Quantity: it.qty}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// names: "Gold", "Gold and Silver", "Gold, Silver and 3 more".
func names(ns []string) string {
	switch len(ns) {
	case 0:
		return ""
	case 1:
		return ns[0]
	case 2:
		return ns[0] + " and " + ns[1]
	}
	return fmt.Sprintf("%s, %s and %d more", ns[0], ns[1], len(ns)-2)
}

// resolveSymbol checks that a symbol is a listed market a basket may hold.
func resolveSymbol(ix *marketIndex, sym, match string) (Resolution, string) {
	if m, ok := ix.baskets[sym]; ok {
		return Resolution{Reason: ReasonBasket}, fmt.Sprintf("%s is a basket. Baskets hold single markets.", m.Symbol)
	}
	i, ok := ix.bySymbol[sym]
	if !ok {
		return Resolution{Reason: ReasonNotListed}, fmt.Sprintf("%s isn't listed.", sym)
	}
	m := ix.markets[i]
	if m.Status == engine.MarketHalted {
		return Resolution{Reason: ReasonHalted}, fmt.Sprintf("%s isn't trading.", m.Name)
	}
	return Resolution{OK: true, Match: match, Market: candidateOf(m)}, ""
}

// evaluate parses, resolves and prices an input and builds its canonical basket.
func (s *Service) evaluate(ctx context.Context, in Input) (*evaluation, error) {
	var (
		lines []Line
		ev    = &evaluation{lines: []PreviewLine{}, warnings: []string{}}
		err   error
	)
	if strings.TrimSpace(in.Text) != "" {
		if lines, ev.warnings, err = Parse(in.Text); err != nil {
			return nil, err
		}
		if ev.warnings == nil {
			ev.warnings = []string{}
		}
	}
	if len(lines)+len(in.Picks) > maxInputLines {
		return nil, errTooLong
	}
	if ev.ix, err = s.index(ctx); err != nil {
		return nil, err
	}
	r := s.resolver(ev.ix)
	type pending struct {
		pl   PreviewLine
		feed string
	}
	var all []pending
	for _, p := range in.Picks {
		sym := strings.ToUpper(strings.TrimSpace(p.Symbol))
		if p.Quantity < 1 || p.Quantity > maxQuantity {
			ev.warnings = append(ev.warnings, fmt.Sprintf("%s: quantity must be 1–10,000. Skipped.", sym))
			continue
		}
		res, msg := resolveSymbol(ev.ix, sym, MatchPicked)
		all = append(all, pending{pl: PreviewLine{Text: sym, Quantity: p.Quantity, Match: res.Match, Reason: res.Reason, Message: msg}})
		if res.OK {
			all[len(all)-1].pl.Market = &res.Market
		}
	}
	for _, l := range lines {
		pl := PreviewLine{Line: l.N, Text: l.Text, Quantity: l.Quantity, Candidates: []Candidate{}}
		var (
			res Resolution
			msg string
		)
		sym := strings.ToUpper(l.Text)
		if _, listed := ev.ix.bySymbol[sym]; listed {
			res, msg = resolveSymbol(ev.ix, sym, MatchSymbol)
		} else if _, basket := ev.ix.baskets[sym]; basket {
			res, msg = resolveSymbol(ev.ix, sym, MatchSymbol)
		} else {
			if res, err = r.Resolve(ctx, l.Text); err != nil {
				return nil, err
			}
			if res.OK {
				match := res.Match
				if res, msg = resolveSymbol(ev.ix, strings.ToUpper(res.Market.Symbol), match); res.OK && match == MatchFuzzy {
					ev.warnings = append(ev.warnings, fmt.Sprintf("Line %d: “%s” read as %s.", l.N, l.Text, res.Market.Name))
				}
			} else {
				switch res.Reason {
				case ReasonAmbiguous:
					msg = fmt.Sprintf("“%s” matches more than one market. Pick one.", l.Text)
				default:
					msg = fmt.Sprintf("No market matches “%s”.", l.Text)
				}
			}
		}
		pl.Match, pl.Reason, pl.Message = res.Match, res.Reason, msg
		if res.OK {
			pl.Market = &res.Market
		} else if len(res.Candidates) > 0 {
			pl.Candidates = res.Candidates[:min(5, len(res.Candidates))]
		}
		all = append(all, pending{pl: pl})
	}

	// Price every resolved line from its market's oracle feed.
	var keys []string
	for i := range all {
		if m := all[i].pl.Market; m != nil {
			lm := ev.ix.markets[ev.ix.bySymbol[m.Symbol]]
			all[i].feed = lm.PriceFeed
			if all[i].feed == "" {
				all[i].feed = lm.Symbol
			}
			keys = append(keys, all[i].feed)
		}
	}
	prices := map[string]oracle.PricePoint{}
	if len(keys) > 0 {
		o := s.oracle()
		if o == nil {
			return nil, errors.New("baskets: the oracle module isn't installed")
		}
		if prices, err = o.CurrentMany(ctx, keys); err != nil {
			return nil, err
		}
	}
	idx := map[string]int{}
	lineCount := map[string]int{}
	warnedPrice := map[string]bool{}
	for _, p := range all {
		pl := p.pl
		if pl.Market == nil {
			pl.Status = LineUnresolved
			ev.unresolved++
			if pl.Candidates == nil {
				pl.Candidates = []Candidate{}
			}
			ev.lines = append(ev.lines, pl)
			continue
		}
		pp, found := prices[p.feed]
		it := bitem{symbol: pl.Market.Symbol, name: pl.Market.Name, feed: p.feed, qty: pl.Quantity}
		switch {
		case found && pp.Kind == oracle.FeedBasket:
			pl.Status, pl.Reason, pl.Message, pl.Market = LineUnresolved, ReasonBasket, fmt.Sprintf("%s is priced as a basket. Baskets hold single markets.", it.name), nil
			ev.unresolved++
			ev.lines = append(ev.lines, pl)
			continue
		case !found:
			pl.Status, pl.Message = LineUnpriced, fmt.Sprintf("%s has no price feed.", it.name)
		case pp.Status == oracle.StatusOff || pp.Price <= 0:
			pl.Status, pl.PriceStatus, pl.Message = LineUnpriced, pp.Status, fmt.Sprintf("%s has no price yet.", it.name)
		default:
			pl.Status, pl.PriceStatus = LineResolved, pp.Status
			it.price, it.priced, it.status = decimal.NewFromFloat(pp.Price).Round(pp.Decimals), true, pp.Status
			px := it.price.String()
			pl.Price = &px
			if !warnedPrice[it.symbol] {
				switch pp.Status {
				case oracle.StatusStale:
					ev.warnings = append(ev.warnings, fmt.Sprintf("%s's price is stale.", it.name))
				case oracle.StatusSeed:
					ev.warnings = append(ev.warnings, fmt.Sprintf("%s's price isn't live yet.", it.name))
				}
				warnedPrice[it.symbol] = true
			}
		}
		if !found || !it.priced {
			it.status = pp.Status
		}
		ev.lines = append(ev.lines, pl)
		lineCount[it.symbol]++
		if j, ok := idx[it.symbol]; ok {
			ev.items[j].qty += it.qty
			continue
		}
		idx[it.symbol] = len(ev.items)
		ev.items = append(ev.items, it)
	}
	sort.Slice(ev.items, func(i, j int) bool { return ev.items[i].symbol < ev.items[j].symbol })
	for _, it := range ev.items {
		if n := lineCount[it.symbol]; n > 1 {
			ev.warnings = append(ev.warnings, fmt.Sprintf("%s is on %d lines; their quantities were added.", it.name, n))
		}
	}
	ev.totals()
	return ev, nil
}

// blockers say in words why the basket can't be created (or joined when it exists).
func (s *Service) blockers(ev *evaluation, ex *basketRow) []string {
	out := []string{}
	if n := ev.unresolved; n > 0 {
		out = append(out, fmt.Sprintf("%d %s didn't match a market. Fix %s or remove %s.", n, plural(n, "line", "lines"),
			plural(n, "it", "them"), plural(n, "it", "them")))
	}
	n := len(ev.items)
	switch {
	case n == 0 && ev.unresolved == 0:
		out = append(out, fmt.Sprintf("Add at least %d %s.", s.cfg.minLines, plural(s.cfg.minLines, "market", "different markets")))
	case n < s.cfg.minLines:
		out = append(out, fmt.Sprintf("A basket needs at least %d different markets.", s.cfg.minLines))
	case n > s.cfg.maxLines:
		out = append(out, fmt.Sprintf("A basket holds at most %d different markets.", s.cfg.maxLines))
	}
	if len(ev.overfull) > 0 {
		out = append(out, fmt.Sprintf("%s %s up to more than 10,000 units.", names(ev.overfull), plural(len(ev.overfull), "adds", "add")))
	}
	if ex != nil {
		if ex.Hidden {
			out = append(out, "Staff removed this basket. It can't be listed again.")
		}
		return out // an existing basket is joined at its listed spec
	}
	if len(ev.unpriced) > 0 {
		out = append(out, fmt.Sprintf("%s %s no price yet.", names(ev.unpriced), plural(len(ev.unpriced), "has", "have")))
	} else if n > 0 && ev.value.LessThan(s.cfg.minValue) {
		out = append(out, fmt.Sprintf("A basket must be worth at least %s. This one is worth %s.", s.cfg.minValue.StringFixed(2), ev.value.StringFixed(2)))
	}
	return out
}

func strPtr(s string) *string { return &s }

// basketLines are the canonical lines with prices, values and weights.
func basketLines(ev *evaluation) []BasketLine {
	out := make([]BasketLine, 0, len(ev.items))
	for _, it := range ev.items {
		bl := BasketLine{Symbol: it.symbol, Name: it.name, Quantity: it.qty, PriceStatus: it.status}
		if it.priced {
			v := it.price.Mul(decimal.NewFromInt(int64(it.qty)))
			bl.Price, bl.Value = strPtr(it.price.String()), strPtr(v.String())
			if ev.value.IsPositive() {
				bl.Weight = strPtr(v.Div(ev.value).Round(4).String())
			}
		}
		out = append(out, bl)
	}
	return out
}

// Preview resolves, prices and checks an input without writing anything.
func (s *Service) Preview(ctx context.Context, caller uuid.UUID, in Input) (Preview, error) {
	ev, err := s.evaluate(ctx, in)
	if err != nil {
		return Preview{}, err
	}
	p := Preview{Lines: ev.lines, Warnings: ev.warnings, Basket: basketLines(ev), Value: ev.value.StringFixed(2),
		MaxShare: ev.maxShare.Round(4).String(), Hash: ev.hash}
	var ex *basketRow
	if ev.hash != "" {
		row, err := basketBy(ctx, s.d.Pool, "hash", ev.hash)
		switch {
		case err == nil:
			ex = &row
		case !errors.Is(err, errNoBasket):
			return p, err
		}
	}
	p.Blockers = s.blockers(ev, ex)
	p.Creatable = len(p.Blockers) == 0
	switch {
	case ex != nil:
		entered, err := hasEntry(ctx, s.d.Pool, ex.ID, caller)
		if err != nil {
			return p, err
		}
		p.Existing = &Existing{Symbol: ex.Symbol, Name: ex.Name, Status: ex.Status, Hidden: ex.Hidden, Entries: ex.Entries, Entered: entered}
		p.Symbol = &ex.Symbol
		if m, ok := ev.ix.baskets[ex.Symbol]; ok {
			p.Spec = &Spec{TickSize: m.TickSize, SzDecimals: m.SzDecimals, MaxLeverage: m.MaxLeverage, MinNotional: m.MinNotional, OICap: m.OICap}
		}
	case ev.hash != "" && len(ev.unpriced) == 0 && ev.value.IsPositive():
		sp := s.cfg.SpecFor(ev.value, ev.maxShare)
		p.Spec = &sp
		taken := map[string]bool{}
		for sym := range ev.ix.bySymbol {
			taken[sym] = true
		}
		if sym, err := s.chooseSymbol(ctx, s.d.Pool, ev.hash, taken); err == nil {
			p.Symbol = &sym
		}
	}
	return p, nil
}

// chooseSymbol is the shortest symbol for hash that no other basket, market
// (taken) or oracle feed holds.
func (s *Service) chooseSymbol(ctx context.Context, q db.Querier, hash string, taken map[string]bool) (string, error) {
	cands := make([]string, len(SymbolLengths))
	for i, n := range SymbolLengths {
		cands[i] = SymbolFor(s.cfg.prefix, hash, n)
	}
	rows, err := q.Query(ctx, `SELECT symbol, hash FROM baskets WHERE symbol = ANY($1)`, cands)
	if err != nil {
		return "", err
	}
	held := map[string]string{}
	for rows.Next() {
		var sym, h string
		if err := rows.Scan(&sym, &h); err != nil {
			rows.Close()
			return "", err
		}
		held[sym] = h
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	feeds := map[string]oracle.PricePoint{}
	if o := s.oracle(); o != nil {
		if feeds, err = o.CurrentMany(ctx, cands); err != nil {
			return "", err
		}
	}
	for _, c := range cands {
		if h, ok := held[c]; ok {
			if h == hash {
				return c, nil
			}
			continue
		}
		if _, feed := feeds[c]; taken[c] || feed {
			continue
		}
		return c, nil
	}
	return "", fmt.Errorf("baskets: no free symbol for basket %s", hash)
}

// takenSymbols is every exchange market symbol, read in q (create's transaction).
func takenSymbols(ctx context.Context, q db.Querier) (map[string]bool, error) {
	ms, err := exchange.Markets(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ms))
	for _, m := range ms {
		out[m.Symbol] = true
	}
	return out, nil
}
