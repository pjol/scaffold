package engine

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// computeMark is the median of (a) index + EMA(mid − index), (b) the median
// of best bid / best ask / last trade and (c) the index, clamped to index ±
// MarkClamp and rounded to tick. Missing components are skipped.
//
// The book component needs both sides: while one is missing (a sweep, or a
// gap) the last two-sided value is held instead of falling back to the last
// trade, so a single print can't yank the mark.
func (e *Engine) computeMark(m *market) decimal.Decimal {
	o := m.index
	var comps []decimal.Decimal
	if m.emaInit {
		comps = append(comps, o.Add(m.ema))
	}
	bb, okB := m.bestBid()
	ba, okA := m.bestAsk()
	if okB && okA {
		bs := []decimal.Decimal{bb, ba}
		if m.hasLast {
			bs = append(bs, m.lastPx)
		}
		m.bookComp, m.bookCompOK = median(bs), true
	}
	if m.bookCompOK {
		comps = append(comps, m.bookComp)
	}
	comps = append(comps, o)
	mk := median(comps)
	band := o.Mul(e.p.MarkClamp)
	mk = clamp(mk, o.Sub(band), o.Add(band))
	mk = roundTick(mk, m.tick)
	if !mk.IsPositive() {
		mk = o
	}
	return mk
}

// markSmoothing is per-market state for a mark that reads like a moving
// average rather than a tape of single prints.
type markSmoothing struct {
	bookComp   decimal.Decimal // last two-sided book component
	bookCompOK bool
	anchor     decimal.Decimal // mark at the last tick: per-second moves are limited around it
	anchorMs   int64
}

// MarkMaxMovePerSec caps how far the mark may move per second (0.3%); the
// 150 s EMA and the index pull it to fair value anyway.
var MarkMaxMovePerSec = decimal.RequireFromString("0.003")

// updateMark recomputes the mark, limited to MarkMaxMovePerSec × seconds since
// the last tick's anchor, then kept within the index band. Liquidations,
// margin and PnL all use this smoothed mark.
func (e *Engine) updateMark(m *market) {
	if !m.index.IsPositive() {
		return
	}
	mk := e.computeMark(m)
	if m.anchor.IsPositive() {
		secs := max(int64(1), (e.nowMs()-m.anchorMs)/1000)
		step := m.anchor.Mul(MarkMaxMovePerSec).Mul(decimal.NewFromInt(secs))
		lo, hi := m.anchor.Sub(step), m.anchor.Add(step)
		mk = roundTick(clamp(mk, lo, hi), m.tick)
		if mk.GreaterThan(hi) { // round inside the budget
			mk = mk.Sub(m.tick)
		} else if mk.LessThan(lo) {
			mk = mk.Add(m.tick)
		}
		band := m.index.Mul(e.p.MarkClamp)
		mk = clamp(mk, m.index.Sub(band), m.index.Add(band))
	}
	m.mark = mk
}

// anchorMark starts the next second's movement budget (from Tick).
func (m *market) anchorMark(now int64) { m.anchor, m.anchorMs = m.mark, now }

// sampleEMA folds the current (mid − index) into the 150 s EMA.
func (e *Engine) sampleEMA(m *market) {
	mid, ok := m.mid()
	if !ok || !m.index.IsPositive() {
		return
	}
	d := mid.Sub(m.index)
	if !m.emaInit {
		m.ema, m.emaInit = d, true
		return
	}
	m.ema = m.ema.Add(e.p.EMAAlpha.Mul(d.Sub(m.ema))).Round(10)
}

// premium is the instantaneous funding premium from impact prices (falling
// back to mid, then the index, when depth is short).
func (e *Engine) premium(m *market) decimal.Decimal {
	o := m.index
	if !o.IsPositive() {
		return decimal.Zero
	}
	fallback := o
	if mid, ok := m.mid(); ok {
		fallback = mid
	}
	ib, ok := m.impactPx(Sell, e.p.ImpactNotional)
	if !ok {
		ib = fallback
	}
	ia, ok := m.impactPx(Buy, e.p.ImpactNotional)
	if !ok {
		ia = fallback
	}
	up := decimal.Max(ib.Sub(o), decimal.Zero)
	down := decimal.Max(o.Sub(ia), decimal.Zero)
	return up.Sub(down).Div(o)
}

// hourlyRate is the 8h perpetual funding formula paid at 1/8 per hour:
// (P + clamp(I − P, −0.0005, 0.0005)) / 8, capped at ±MaxFundingHourly.
func (e *Engine) hourlyRate(avgPremium decimal.Decimal) decimal.Decimal {
	c := e.p.PremiumClamp8h
	f8 := avgPremium.Add(clamp(e.p.InterestRate8h.Sub(avgPremium), c.Neg(), c))
	h := f8.Div(decimal.NewFromInt(8))
	return clamp(h, e.p.MaxFundingHourly.Neg(), e.p.MaxFundingHourly).Round(10)
}

func (m *market) avgPremium() decimal.Decimal {
	if m.premN == 0 {
		return decimal.Zero
	}
	return m.premSum.Div(decimal.NewFromInt(int64(m.premN)))
}

// settleFunding pays funding on every open position at the index price.
func (e *Engine) settleFunding(c *cmd, now int64) {
	ivHours := decimal.NewFromInt(e.p.FundingInterval.Milliseconds()).Div(decimal.NewFromInt(time.Hour.Milliseconds()))
	for _, sym := range e.symbols {
		m := e.markets[sym]
		avg := m.avgPremium()
		m.premSum, m.premN = decimal.Zero, 0
		if !m.index.IsPositive() {
			continue
		}
		hourly := e.hourlyRate(avg)
		rate := hourly.Mul(ivHours)
		c.b.FundingRates = append(c.b.FundingRates, FundingRateRec{Symbol: sym, MarketID: m.spec.ID, Ts: now, Rate: hourly, Premium: avg, Index: m.index})
		for _, a := range e.accounts {
			p := a.Positions[sym]
			if p == nil || p.Sz.IsZero() {
				continue
			}
			pay := p.Sz.Mul(m.index).Mul(rate).Neg().Round(8) // longs pay when rate > 0
			if pay.IsZero() {
				continue
			}
			a.Balance = a.Balance.Add(pay)
			p.FundingSinceOpen = p.FundingSinceOpen.Add(pay)
			c.touchAccount(a)
			c.touchPosition(a, p)
			if !a.keepsHistory() {
				continue
			}
			fr := FundingRec{ID: newID(), AccountID: a.ID, Symbol: sym, MarketID: m.spec.ID, Ts: now, Rate: hourly, Sz: p.Sz, Payment: pay}
			c.b.Funding = append(c.b.Funding, fr)
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerFunding, Amount: pay, Symbol: sym, MarketID: m.spec.ID, Ts: now})
			v := e.fundingView(fr)
			id := a.ID
			c.emit(func(p Publisher) { p.AccountEvent(id, AccountEvent{Type: "funding", Payment: &v}) })
		}
	}
}

func (e *Engine) fundingView(f FundingRec) FundingPayment {
	sz := f.Sz.String()
	if m := e.markets[f.Symbol]; m != nil {
		sz = f.Sz.StringFixed(m.szDec)
	}
	return FundingPayment{Symbol: f.Symbol, Rate: f.Rate.Round(10).String(), Sz: sz, Payment: e.money4(f.Payment), CreatedAt: msTime(f.Ts)}
}

// FundingViewOf formats a stored payment.
func (e *Engine) FundingViewOf(f FundingRec) FundingPayment {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.fundingView(f)
}

// ---- candles ----

func (e *Engine) candleAt(m *market, now int64) *CandleRec {
	t := now / 60_000 * 60_000
	if m.cur != nil && m.cur.T == t {
		return m.cur
	}
	return nil
}

// rollCandle makes sure the open candle is the one for now, closing the old one.
func (e *Engine) rollCandle(c *cmd, m *market, now int64) *CandleRec {
	t := now / 60_000 * 60_000
	if m.cur != nil && m.cur.T == t {
		return m.cur
	}
	if n := len(m.candles); m.cur == nil && n > 0 && m.candles[n-1].T == t {
		// restarted within the minute the last persisted candle covers: resume it
		cur := m.candles[n-1]
		m.candles = append([]CandleRec(nil), m.candles[:n-1]...) // reallocate: closed candles are append-only
		m.cur = &cur
		return m.cur
	}
	open := m.mark
	if m.cur != nil {
		open = m.cur.C
		closed := *m.cur
		m.candles = append(m.candles, closed)
		if len(m.candles) > candleKeep*2 {
			m.candles = append([]CandleRec(nil), m.candles[len(m.candles)-candleKeep:]...)
		}
		if c != nil {
			c.b.Candles = append(c.b.Candles, closed)
		}
	} else if n := len(m.candles); n > 0 {
		open = m.candles[n-1].C
	}
	m.cur = &CandleRec{Symbol: m.spec.Symbol, MarketID: m.spec.ID, T: t, O: open, H: decimal.Max(open, m.mark),
		L: decimal.Min(open, m.mark), C: m.mark}
	return m.cur
}

func (e *Engine) sampleCandle(c *cmd, m *market, now int64) {
	if !m.mark.IsPositive() {
		return
	}
	cd := e.rollCandle(c, m, now)
	cd.H = decimal.Max(cd.H, m.mark)
	cd.L = decimal.Min(cd.L, m.mark)
	cd.C = m.mark
}

func (e *Engine) candleTrade(c *cmd, m *market, qty, px decimal.Decimal, now int64, sim bool) {
	cd := e.candleAt(m, now)
	if cd == nil {
		cd = e.rollCandle(c, m, now)
	}
	cd.V = cd.V.Add(qty)
	cd.VQuote = cd.VQuote.Add(qty.Mul(px))
	if sim {
		cd.VSim = cd.VSim.Add(qty.Mul(px))
	}
}

// noteTrader counts an account into the 24h trader totals on its first fill
// of the window.
func (e *Engine) noteTrader(a *Account, now int64) {
	if a.IsSystem() {
		return
	}
	if a.Kind == KindSub { // not a separate trader: its owner (or the leader it copies) is
		a.LastTradeMs = now
		return
	}
	if a.LastTradeMs <= now-24*60*60*1000 {
		e.traders24h++
		if a.IsSim() {
			e.simTraders24h++
		}
	}
	a.LastTradeMs = now
}

// recountTraders recomputes the 24h trader totals exactly (expiring accounts
// that stopped trading); once a minute from Tick.
func (e *Engine) recountTraders(now int64) {
	key := now / 60_000
	if e.tradersKey == key {
		return
	}
	e.tradersKey = key
	since := now - 24*60*60*1000
	n, sim := 0, 0
	for _, a := range e.accounts {
		if !a.IsSystem() && a.Kind != KindSub && a.LastTradeMs > since {
			n++
			if a.IsSim() {
				sim++
			}
		}
	}
	e.traders24h, e.simTraders24h = n, sim
}

// ---- tick ----

// Run drives the engine clock (1 s) and applies index updates until ctx ends.
// tick runs after each engine second (the service polls its inbox there).
func (e *Engine) Run(ctx context.Context, tick func()) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			e.flushCandles()
			return
		case u := <-e.indexCh:
			_ = e.applyIndex(u)
		case <-t.C:
			if err := e.Tick(); err != nil {
				e.log.Warn("exchange: tick failed", "err", err)
			}
			if tick != nil {
				tick()
			}
		}
	}
}

// Tick runs one engine second: index staleness, EMA, mark and candles,
// funding samples and settlement, liquidations — atomically.
func (e *Engine) Tick() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.begin()
	now := e.nowMs()
	sample := now-e.lastSample >= e.p.FundingSampleEvery.Milliseconds()
	if sample {
		e.lastSample = now
	}
	for _, sym := range e.symbols {
		m := e.markets[sym]
		if m.indexStatus == IndexLive && e.p.IndexStaleAfter > 0 && now-m.indexMs > e.p.IndexStaleAfter.Milliseconds() {
			m.indexStatus = IndexStale
		}
		e.sampleEMA(m)
		e.updateMark(m)
		m.anchorMark(now)
		e.sampleCandle(c, m, now)
		m.refreshDay(now)
		if sample && m.index.IsPositive() {
			m.premSum = m.premSum.Add(e.premium(m))
			m.premN++
		}
	}
	e.recountTraders(now)
	if now >= e.nextFunding {
		e.settleFunding(c, now)
		e.nextFunding = e.alignedNextFunding(now)
	}
	e.checkLiquidations(c)
	return e.commit(c)
}

// flushCandles persists open candles (shutdown).
func (e *Engine) flushCandles() {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.begin()
	for _, m := range e.markets {
		if m.cur != nil {
			c.b.Candles = append(c.b.Candles, *m.cur)
		}
	}
	_ = e.commit(c)
}

// OnIndex queues an index price (non-blocking; safe from feed callbacks).
func (e *Engine) OnIndex(u IndexUpdate) {
	select {
	case e.indexCh <- u:
	default:
		e.log.Warn("exchange: index queue full; dropping update", "symbol", u.Symbol)
	}
}

// QuoteSignals delivers symbols whose index price was just applied (for the simulation).
func (e *Engine) QuoteSignals() <-chan string { return e.quoteCh }

// SetIndexNow applies an index price synchronously (tests).
func (e *Engine) SetIndexNow(u IndexUpdate) error { return e.applyIndex(u) }

// applyIndex sets a market's index price, recomputes its mark and, when the
// price moved, runs liquidations — as one atomic command. A market with a
// manual price ignores feed updates until the override is cleared.
func (e *Engine) applyIndex(u IndexUpdate) error {
	e.mu.Lock()
	m := e.markets[u.Symbol]
	if m == nil || !u.Price.IsPositive() || (m.indexStatus == IndexManual && u.Status != IndexManual) {
		e.mu.Unlock()
		return nil
	}
	if u.Status == "" {
		u.Status = IndexLive
	}
	if u.Ms == 0 {
		u.Ms = e.nowMs()
	}
	var err error
	if m.index.Equal(u.Price) {
		m.indexStatus, m.indexMs = u.Status, u.Ms
		e.updateMark(m)
	} else {
		c := e.begin()
		m.index, m.indexStatus, m.indexMs = u.Price, u.Status, u.Ms
		e.updateMark(m)
		e.checkLiquidations(c)
		err = e.commit(c)
	}
	e.mu.Unlock()
	select {
	case e.quoteCh <- u.Symbol:
	default:
	}
	return err
}
