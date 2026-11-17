package engine

import (
	"github.com/shopspring/decimal"
)

func (e *Engine) stats(m *market, now int64) MarketStats {
	s := MarketStats{
		Symbol:            m.spec.Symbol,
		MarkPx:            m.mark.StringFixed(m.pxDec),
		IndexPx:           m.index.StringFixed(m.pxDec),
		IndexStatus:       m.indexStatus,
		OpenInterest:      m.oi.StringFixed(m.szDec),
		OpenInterestValue: e.money(m.oi.Mul(m.mark)),
		FundingRate:       e.hourlyRate(m.avgPremium()).String(),
		NextFundingAt:     msTime(e.nextFunding),
	}
	if m.indexMs > 0 {
		t := msTime(m.indexMs)
		s.IndexUpdatedAt = &t
	}
	if mid, ok := m.mid(); ok {
		v := mid.StringFixed(m.pxDec + 1)
		s.MidPx = &v
	}
	prev, vol, vsim := m.dayStats(now)
	s.PrevDayPx = prev.StringFixed(m.pxDec)
	s.DayVolume = e.money(vol)
	s.DayVolumeSimPct = share(vsim, vol)
	chg := decimal.Zero
	if prev.IsPositive() {
		chg = m.mark.Sub(prev).Div(prev).Mul(hundred)
	}
	s.DayChangePct = chg.StringFixed(2)
	return s
}

// refreshDay recomputes the closed-candle part of the 24h stats once per
// minute (called from Tick under the write lock).
func (m *market) refreshDay(now int64) {
	key := now / 60_000
	if m.dayKey != key || m.dayN != len(m.candles) {
		since := now - 24*60*60*1000
		p, v, vs := decimal.Zero, decimal.Zero, decimal.Zero
		found := false
		for i := len(m.candles) - 1; i >= 0; i-- {
			cd := m.candles[i]
			if cd.T < since {
				p, found = cd.C, true
				break
			}
			v = v.Add(cd.VQuote)
			vs = vs.Add(cd.VSim)
			p, found = cd.O, true
		}
		m.dayKey, m.dayN, m.dayPrev, m.dayHas, m.dayVol, m.dayVSim = key, len(m.candles), p, found, v, vs
	}
}

// dayStats returns the mark 24h ago, the 24h quote volume and its simulated
// part (cached closed candles + the open one).
func (m *market) dayStats(now int64) (prev, vol, vsim decimal.Decimal) {
	prev, vol, vsim = m.dayPrev, m.dayVol, m.dayVSim
	if !m.dayHas {
		prev = m.mark
		if m.cur != nil {
			prev = m.cur.O
		}
	}
	if m.cur != nil {
		vol = vol.Add(m.cur.VQuote)
		vsim = vsim.Add(m.cur.VSim)
	}
	return prev, vol, vsim
}

// share is part/whole as a percentage with one decimal ("0.0" when whole is 0).
func share(part, whole decimal.Decimal) string {
	if !whole.IsPositive() {
		return "0.0"
	}
	return clamp(part.Div(whole).Mul(hundred), decimal.Zero, hundred).StringFixed(1)
}

// PlatformStats are exchange-wide totals.
type PlatformStats struct {
	DayVolume          string `json:"day_volume"`
	DayVolumeSimPct    string `json:"day_volume_sim_pct"`
	OpenInterestValue  string `json:"open_interest_value"`
	OpenInterestSimPct string `json:"open_interest_sim_pct"`
	Traders24h         int    `json:"traders_24h"`
	SimTraders24h      int    `json:"sim_traders_24h"`
	BackstopBalance    string `json:"backstop_balance"`
	FeesCollected      string `json:"fees_collected"`
	Accounts           int    `json:"accounts"`
}

// Platform aggregates every market's counters.
func (e *Engine) Platform() PlatformStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	now := e.nowMs()
	var vol, vsim, oiV, simV decimal.Decimal
	for _, m := range e.markets {
		_, v, vs := m.dayStats(now)
		vol, vsim = vol.Add(v), vsim.Add(vs)
		oiV = oiV.Add(m.oi.Mul(m.mark))
		simV = simV.Add(m.simOI.Mul(m.mark))
	}
	users := 0
	for _, a := range e.accounts {
		if a.Kind == KindUser && a.Status == AccountActive {
			users++
		}
	}
	ps := PlatformStats{
		DayVolume: e.money(vol), DayVolumeSimPct: share(vsim, vol),
		OpenInterestValue: e.money(oiV), OpenInterestSimPct: share(simV, oiV.Mul(two)),
		Traders24h: e.traders24h, SimTraders24h: e.simTraders24h, Accounts: users,
	}
	if e.backstop != nil {
		ps.BackstopBalance = e.money(e.backstop.Balance)
	}
	if e.fee != nil {
		ps.FeesCollected = e.money(e.fee.Balance)
	}
	return ps
}

func (e *Engine) marketView(m *market, now int64) MarketView {
	return MarketView{
		ID: m.spec.ID, Symbol: m.spec.Symbol, Name: m.spec.Name, Category: m.spec.Category, Description: m.spec.Description,
		ImageURL: m.spec.ImageURL, Status: m.spec.Status, MaxLeverage: m.spec.MaxLeverage,
		MaintenanceMarginRatio: e.maintRate(m).Round(8).String(),
		TakerFee:               e.p.TakerFee.String(), MakerFee: e.p.MakerFee.String(),
		SzDecimals: m.spec.SzDecimals, TickSize: m.spec.TickSize.String(), MinNotional: m.spec.MinNotional.String(),
		OICap: m.spec.OICap.String(), Stats: e.stats(m, now),
	}
}

// Markets returns every market in display order.
func (e *Engine) Markets() []MarketView {
	e.mu.RLock()
	defer e.mu.RUnlock()
	now := e.nowMs()
	out := make([]MarketView, 0, len(e.symbols))
	for _, s := range e.symbols {
		out = append(out, e.marketView(e.markets[s], now))
	}
	return out
}

// Market returns one market.
func (e *Engine) Market(symbol string) (MarketView, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := e.markets[symbol]
	if m == nil {
		return MarketView{}, false
	}
	return e.marketView(m, e.nowMs()), true
}

// Stats returns one market's live stats.
func (e *Engine) Stats(symbol string) (MarketStats, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := e.markets[symbol]
	if m == nil {
		return MarketStats{}, false
	}
	return e.stats(m, e.nowMs()), true
}

// AllStats returns every market's stats in display order.
func (e *Engine) AllStats() []MarketStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	now := e.nowMs()
	out := make([]MarketStats, 0, len(e.symbols))
	for _, s := range e.symbols {
		out = append(out, e.stats(e.markets[s], now))
	}
	return out
}

// HasMarket reports whether symbol is listed.
func (e *Engine) HasMarket(symbol string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.markets[symbol]
	return ok
}

// MarketID returns a market's id.
func (e *Engine) MarketID(symbol string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if m := e.markets[symbol]; m != nil {
		return m.spec.ID, true
	}
	return "", false
}

// Symbols lists markets in display order.
func (e *Engine) Symbols() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]string(nil), e.symbols...)
}

// Book returns an L2 snapshot.
func (e *Engine) Book(symbol string, depth int) (Book, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := e.markets[symbol]
	if m == nil {
		return Book{}, false
	}
	if depth <= 0 {
		depth = 20
	}
	bids, asks := m.levels(depth)
	return Book{Symbol: symbol, Bids: bids, Asks: asks, At: msTime(e.nowMs())}, true
}

func (e *Engine) tradeView(t TradeRec) Trade {
	v := Trade{ID: t.ID, Symbol: t.Symbol, Px: t.Px.String(), Sz: t.Sz.String(), Side: t.Side,
		Sim: t.BuyerSim || t.SellerSim, Liquidation: t.Liquidation, At: msTime(t.Ts)}
	if m := e.markets[t.Symbol]; m != nil {
		v.Px, v.Sz = t.Px.StringFixed(m.pxDec), t.Sz.StringFixed(m.szDec)
	}
	return v
}

// Trades returns a market's recent trades, newest first.
func (e *Engine) Trades(symbol string, limit int) ([]Trade, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := e.markets[symbol]
	if m == nil {
		return nil, false
	}
	out := []Trade{}
	for i := len(m.trades) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, e.tradeView(m.trades[i]))
	}
	return out, true
}

// Intervals maps candle interval names to milliseconds.
var Intervals = map[string]int64{
	"1m": 60_000, "5m": 300_000, "15m": 900_000, "1h": 3_600_000, "4h": 14_400_000, "1d": 86_400_000,
}

// IntervalNames lists the intervals, shortest first.
var IntervalNames = []string{"1m", "5m", "15m", "1h", "4h", "1d"}

// CandleView formats a candle at the market's precision.
func (e *Engine) CandleView(symbol string, c CandleRec) Candle {
	e.mu.RLock()
	m := e.markets[symbol]
	e.mu.RUnlock()
	pd, sd := int32(8), int32(8)
	if m != nil {
		pd, sd = m.pxDec, m.szDec
	}
	return Candle{T: c.T, O: c.O.StringFixed(pd), H: c.H.StringFixed(pd), L: c.L.StringFixed(pd), C: c.C.StringFixed(pd), V: c.V.StringFixed(sd)}
}

// CurrentCandles returns the open bucket of every interval, aggregated from
// in-memory 1m candles (the chart's live bar).
func (e *Engine) CurrentCandles(symbol string) (map[string]Candle, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := e.markets[symbol]
	if m == nil || m.cur == nil {
		return nil, false
	}
	out := make(map[string]Candle, len(Intervals))
	for name, iv := range Intervals {
		c := currentCandle(m, iv)
		out[name] = Candle{T: c.T, O: c.O.StringFixed(m.pxDec), H: c.H.StringFixed(m.pxDec), L: c.L.StringFixed(m.pxDec),
			C: c.C.StringFixed(m.pxDec), V: c.V.StringFixed(m.szDec)}
	}
	return out, true
}

// CurrentCandle aggregates the open bucket of one interval.
func (e *Engine) CurrentCandle(symbol string, ivMs int64) (CandleRec, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := e.markets[symbol]
	if m == nil || m.cur == nil {
		return CandleRec{}, false
	}
	return currentCandle(m, ivMs), true
}

func currentCandle(m *market, ivMs int64) CandleRec {
	start := m.cur.T / ivMs * ivMs
	out := CandleRec{Symbol: m.spec.Symbol, MarketID: m.spec.ID, T: start}
	first := true
	merge := func(cd CandleRec) {
		if first {
			out.O, out.H, out.L = cd.O, cd.H, cd.L
			first = false
		}
		out.H = decimal.Max(out.H, cd.H)
		out.L = decimal.Min(out.L, cd.L)
		out.C = cd.C
		out.V = out.V.Add(cd.V)
		out.VQuote = out.VQuote.Add(cd.VQuote)
		out.VSim = out.VSim.Add(cd.VSim)
	}
	i := len(m.candles)
	for i > 0 && m.candles[i-1].T >= start {
		i--
	}
	for ; i < len(m.candles); i++ {
		merge(m.candles[i])
	}
	merge(*m.cur)
	return out
}

// MergeCandles appends/merges the open bucket into history (oldest first).
func MergeCandles(hist []CandleRec, cur CandleRec) []CandleRec {
	if n := len(hist); n > 0 && hist[n-1].T == cur.T {
		hist[n-1] = cur // cur aggregates the whole bucket from memory
		return hist
	} else if n > 0 && hist[n-1].T > cur.T {
		return hist
	}
	return append(hist, cur)
}

// SimInfo is what the simulation needs about one market and one account.
type SimInfo struct {
	Symbol      string
	Status      string
	Index       decimal.Decimal
	IndexStatus string
	Mark        decimal.Decimal
	Tick        decimal.Decimal
	SzDec       int32
	MinNotional decimal.Decimal
	MaxLeverage int
	Pos         decimal.Decimal // the account's signed position
	AV          decimal.Decimal // the account's account value
	FreeMargin  decimal.Decimal
}

func (e *Engine) SimInfo(accountID, symbol string) (SimInfo, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m, a := e.markets[symbol], e.accounts[accountID]
	if m == nil || a == nil {
		return SimInfo{}, false
	}
	s := SimInfo{Symbol: symbol, Status: m.spec.Status, Index: m.index, IndexStatus: m.indexStatus, Mark: m.mark, Tick: m.tick,
		SzDec: m.szDec, MinNotional: m.minNotional, MaxLeverage: m.spec.MaxLeverage}
	if p := a.Positions[symbol]; p != nil {
		s.Pos = p.Sz
	}
	r := e.risk(a, nil, "", 0)
	s.AV = r.av
	s.FreeMargin = r.av.Sub(r.im).Sub(r.reserved)
	return s, true
}

// Marks returns every market's mark price.
func (e *Engine) Marks() map[string]decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]decimal.Decimal, len(e.markets))
	for s, m := range e.markets {
		out[s] = m.mark
	}
	return out
}

// OpenOrders returns a copy of the account's open orders.
func (e *Engine) OpenOrders(accountID string) []Order {
	e.mu.RLock()
	defer e.mu.RUnlock()
	a := e.accounts[accountID]
	if a == nil {
		return nil
	}
	out := make([]Order, 0, len(a.Orders))
	for _, o := range a.Orders {
		out = append(out, *o)
	}
	return out
}
