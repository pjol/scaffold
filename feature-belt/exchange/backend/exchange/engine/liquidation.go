package engine

import (
	"sort"

	"github.com/shopspring/decimal"
)

// Liquidation:
//
//   - An account whose value falls below maintenance margin has its orders
//     cancelled and is liquidated position by position (largest notional
//     first) with reduce-only market orders into the book, filling within
//     LiqSlippage of mark.
//   - A position whose notional exceeds LiqPartialThreshold only sends
//     LiqPartialFraction of itself, after which the account enters a
//     LiqCooldown; while it runs, further liquidations of that account take
//     whole positions. Smaller positions always go in full.
//   - Liquidation stops as soon as account value is back at or above
//     maintenance margin.
//   - Backstop: if account value is below BackstopRatio of maintenance margin,
//     the backstop account takes every position at mark and keeps whatever
//     margin remains; a negative remainder is bad debt the backstop absorbs.

// checkLiquidations liquidates every user and simulated account below maintenance margin.
func (e *Engine) checkLiquidations(c *cmd) {
	var victims []*Account
	for _, a := range e.accounts {
		if a.IsSystem() || !hasPosition(a) {
			continue
		}
		if r := e.risk(a, nil, "", 0); r.av.LessThan(r.mm) {
			victims = append(victims, a)
		}
	}
	sort.Slice(victims, func(i, j int) bool { return victims[i].ID < victims[j].ID })
	for _, a := range victims {
		e.liquidate(c, a)
	}
}

func hasPosition(a *Account) bool {
	for _, p := range a.Positions {
		if !p.Sz.IsZero() {
			return true
		}
	}
	return false
}

// belowBackstop reports av < BackstopRatio × mm.
func (e *Engine) belowBackstop(r risk) bool {
	return r.av.LessThan(r.mm.Mul(e.p.BackstopRatio))
}

func (e *Engine) liquidate(c *cmd, a *Account) {
	e.cancelAll(c, a, "")
	r := e.risk(a, nil, "", 0)
	e.log.Info("exchange: liquidating", "account", a.ID, "account_value", r.av.StringFixed(2),
		"maintenance", r.mm.StringFixed(2))
	if e.belowBackstop(r) {
		e.takeOver(c, a)
		return
	}
	now := e.nowMs()
	inCooldown := now < a.LiqCooldownUntilMs
	partialSent := false
	for _, p := range e.positionsByNotional(a) {
		if r := e.risk(a, nil, "", 0); !r.av.LessThan(r.mm) {
			break // recovered
		}
		m := e.markets[p.Symbol]
		qty := p.Sz.Abs()
		partial := false
		if !inCooldown && qty.Mul(m.mark).GreaterThan(e.p.LiqPartialThreshold) {
			if chunk := qty.Mul(e.p.LiqPartialFraction).Truncate(m.szDec); chunk.IsPositive() {
				qty, partial = chunk, true
			}
		}
		if filled := e.liquidateIntoBook(c, a, m, p, qty); filled.IsPositive() && partial {
			partialSent = true
		}
	}
	if partialSent {
		a.LiqCooldownUntilMs = now + e.p.LiqCooldown.Milliseconds()
		c.touchAccount(a)
	}
	e.absorbBadDebt(c, a, now)
}

// positionsByNotional returns open positions, largest notional first.
func (e *Engine) positionsByNotional(a *Account) []*Position {
	var ps []*Position
	for _, sym := range e.symbols {
		if p := a.Positions[sym]; p != nil && !p.Sz.IsZero() {
			ps = append(ps, p)
		}
	}
	sort.SliceStable(ps, func(i, j int) bool {
		ni := ps[i].Sz.Abs().Mul(e.markets[ps[i].Symbol].mark)
		nj := ps[j].Sz.Abs().Mul(e.markets[ps[j].Symbol].mark)
		return ni.GreaterThan(nj)
	})
	return ps
}

func (e *Engine) liqOrder(a *Account, m *market, p *Position, qty decimal.Decimal) *Order {
	now := e.nowMs()
	side := Sell
	worst := m.mark.Mul(one.Sub(e.p.LiqSlippage))
	if p.Sz.IsNegative() {
		side = Buy
		worst = m.mark.Mul(one.Add(e.p.LiqSlippage))
	}
	return &Order{
		ID: newID(), AccountID: a.ID, Symbol: m.spec.Symbol, MarketID: m.spec.ID, Side: side, Type: TypeMarket, Px: worst, Sz: qty,
		Status: StatusOpen, ReduceOnly: true, Leverage: p.Leverage, Seq: e.nextSeq(), CreatedMs: now, UpdatedMs: now,
		Liquidation: true,
	}
}

// liquidateIntoBook sends a reduce-only liquidation market order for qty of
// the position and returns how much filled.
func (e *Engine) liquidateIntoBook(c *cmd, a *Account, m *market, p *Position, qty decimal.Decimal) decimal.Decimal {
	sign := p.Sz.Sign()
	o := e.liqOrder(a, m, p, qty)
	t0 := len(c.b.Trades)
	e.match(c, m, o, a, true)
	if !o.Filled.IsPositive() {
		return decimal.Zero // empty book within the band: retried next second; backstop if it gets worse
	}
	var notional decimal.Decimal
	for _, t := range c.b.Trades[t0:] {
		notional = notional.Add(t.Px.Mul(t.Sz))
	}
	avg := notional.Div(o.Filled)
	o.Status = StatusFilled
	if o.Remaining().IsPositive() {
		o.Status = StatusCancelled
	}
	c.touchOrder(o)
	e.emitOrder(c, a, o)
	closed := o.Filled
	if sign < 0 {
		closed = closed.Neg()
	}
	e.recordLiquidation(c, a, m, closed, avg, o.Filled, decimal.Zero)
	e.updateMark(m)
	return o.Filled
}

// takeOver hands every position to the backstop at mark and gives the
// backstop the account's remaining margin (or makes it absorb the deficit).
func (e *Engine) takeOver(c *cmd, a *Account) {
	now := e.nowMs()
	for _, p := range e.positionsByNotional(a) {
		m := e.markets[p.Symbol]
		closed := p.Sz
		qty := p.Sz.Abs()
		o := e.liqOrder(a, m, p, qty)
		bp := e.position(e.backstop, m)
		if bp.Leverage < m.spec.MaxLeverage {
			bp.Leverage = m.spec.MaxLeverage
		}
		e.execute(c, m, o, a, newID(), nil, e.backstop, qty, m.mark, true, decimal.Zero, decimal.Zero)
		o.Status = StatusFilled
		c.touchOrder(o)
		e.emitOrder(c, a, o)
		e.recordLiquidation(c, a, m, closed, m.mark, decimal.Zero, qty)
		e.updateMark(m)
	}
	rem := a.Balance
	switch {
	case rem.IsPositive():
		e.backstop.Balance = e.backstop.Balance.Add(rem)
		if a.keepsHistory() {
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerLiquidation, Amount: rem.Neg(),
				Note: "backstop liquidation: remaining margin taken by the backstop", Ts: now})
		}
	case rem.IsNegative():
		e.backstop.Balance = e.backstop.Balance.Add(rem)
		if a.keepsHistory() {
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerLiquidation, Amount: rem.Neg(),
				Note: "backstop liquidation: bad debt absorbed by the backstop", Ts: now})
		}
		e.markBadDebt(c, rem.Neg())
		e.log.Warn("exchange: bad debt absorbed by the backstop", "account", a.ID, "bad_debt", rem.Neg().StringFixed(2))
	}
	a.Balance = decimal.Zero
	c.touchAccount(a)
	c.touchAccount(e.backstop)
}

// absorbBadDebt moves a negative balance left after closing everything to the backstop.
func (e *Engine) absorbBadDebt(c *cmd, a *Account, now int64) {
	if !a.Balance.IsNegative() || hasPosition(a) {
		return
	}
	bad := a.Balance.Neg()
	e.backstop.Balance = e.backstop.Balance.Sub(bad)
	a.Balance = decimal.Zero
	c.touchAccount(e.backstop)
	c.touchAccount(a)
	if a.keepsHistory() {
		c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerLiquidation, Amount: bad,
			Note: "bad debt absorbed by the backstop", Ts: now})
	}
	e.markBadDebt(c, bad)
	e.log.Warn("exchange: bad debt absorbed by the backstop", "account", a.ID, "bad_debt", bad.StringFixed(2))
}

func (e *Engine) markBadDebt(c *cmd, bad decimal.Decimal) {
	if n := len(c.b.Liquidations); n > 0 {
		c.b.Liquidations[n-1].BadDebt = bad
	}
}

func (e *Engine) recordLiquidation(c *cmd, a *Account, m *market, closed, px, bookSz, backstopSz decimal.Decimal) {
	sym := m.spec.Symbol
	c.b.Liquidations = append(c.b.Liquidations, LiquidationRec{ID: newID(), AccountID: a.ID, Symbol: sym, MarketID: m.spec.ID,
		Sz: closed, Px: px, BookSz: bookSz, BackstopSz: backstopSz, Ts: e.nowMs()})
	if a.Kind != KindUser {
		return
	}
	szs, pxs, id := closed.StringFixed(m.szDec), px.StringFixed(m.pxDec), a.ID
	c.emit(func(p Publisher) {
		p.AccountEvent(id, AccountEvent{Type: "liquidation", Symbol: sym, Sz: szs, Px: pxs})
	})
}
