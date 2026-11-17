package engine

import (
	"sort"

	"github.com/shopspring/decimal"
)

type risk struct {
	upnl     decimal.Decimal
	av       decimal.Decimal // balance + upnl
	im       decimal.Decimal // Σ initial margin of positions
	mm       decimal.Decimal // Σ maintenance margin
	reserved decimal.Decimal // margin reserved by open orders
}

func (e *Engine) maintRate(m *market) decimal.Decimal {
	return one.Div(decimal.NewFromInt(int64(2 * m.spec.MaxLeverage)))
}

func (a *Account) leverage(sym string, def int) int {
	if p := a.Positions[sym]; p != nil && p.Leverage > 0 {
		return p.Leverage
	}
	return max(1, def)
}

// risk computes the account's margin state at current marks. extra is a
// prospective order counted as resting; levSym/lev overrides one market's leverage.
func (e *Engine) risk(a *Account, extra *Order, levSym string, lev int) risk {
	var r risk
	levOf := func(sym string) int {
		if sym == levSym && lev > 0 {
			return lev
		}
		return a.leverage(sym, 1)
	}
	for sym, p := range a.Positions {
		if p.Sz.IsZero() {
			continue
		}
		m := e.markets[sym]
		if m == nil {
			continue
		}
		r.upnl = r.upnl.Add(p.Sz.Mul(m.mark.Sub(p.EntryPx)))
		notional := p.Sz.Abs().Mul(m.mark)
		r.im = r.im.Add(notional.Div(decimal.NewFromInt(int64(levOf(sym)))))
		r.mm = r.mm.Add(notional.Mul(e.maintRate(m)))
	}
	type agg struct{ buys, sells decimal.Decimal }
	open := map[string]*agg{}
	add := func(o *Order) {
		if o.ReduceOnly {
			return
		}
		g := open[o.Symbol]
		if g == nil {
			g = &agg{}
			open[o.Symbol] = g
		}
		if o.Side == Buy {
			g.buys = g.buys.Add(o.Remaining())
		} else {
			g.sells = g.sells.Add(o.Remaining())
		}
	}
	for _, o := range a.Orders {
		add(o)
	}
	if extra != nil {
		add(extra)
	}
	for sym, g := range open {
		m := e.markets[sym]
		if m == nil {
			continue
		}
		pos := decimal.Zero
		if p := a.Positions[sym]; p != nil {
			pos = p.Sz
		}
		worst := decimal.Max(pos.Add(g.buys).Abs(), pos.Sub(g.sells).Abs())
		if ext := worst.Sub(pos.Abs()); ext.IsPositive() {
			r.reserved = r.reserved.Add(ext.Mul(m.mark).Div(decimal.NewFromInt(int64(levOf(sym)))))
		}
	}
	r.av = a.Balance.Add(r.upnl)
	return r
}

func (e *Engine) positionView(p *Position, r risk) PositionView {
	m := e.markets[p.Symbol]
	lev := decimal.NewFromInt(int64(max(1, p.Leverage)))
	notional := p.Sz.Abs().Mul(m.mark)
	upnl := p.Sz.Mul(m.mark.Sub(p.EntryPx))
	entryMargin := p.Sz.Abs().Mul(p.EntryPx).Div(lev)
	roe := decimal.Zero
	if entryMargin.IsPositive() {
		roe = upnl.Div(entryMargin).Mul(hundred)
	}
	v := PositionView{
		Symbol:           p.Symbol,
		Sz:               p.Sz.StringFixed(m.szDec),
		EntryPx:          p.EntryPx.StringFixed(m.pxDec + 2),
		MarkPx:           m.mark.StringFixed(m.pxDec),
		Notional:         e.money(notional),
		UnrealizedPnl:    e.money(upnl),
		RoePct:           roe.StringFixed(2),
		Leverage:         p.Leverage,
		MarginUsed:       e.money(notional.Div(lev)),
		FundingSinceOpen: e.money(p.FundingSinceOpen),
	}
	// Cross-margin liquidation price: the mark of this market at which account
	// value meets maintenance margin, other marks held constant.
	rate := e.maintRate(m)
	mmOthers := r.mm.Sub(notional.Mul(rate))
	var px decimal.Decimal
	if p.Sz.IsPositive() {
		den := p.Sz.Mul(one.Sub(rate))
		px = p.Sz.Mul(m.mark).Add(mmOthers).Sub(r.av).Div(den)
	} else {
		sz := p.Sz.Abs()
		den := sz.Mul(one.Add(rate))
		px = r.av.Add(sz.Mul(m.mark)).Sub(mmOthers).Div(den)
	}
	if px.IsPositive() {
		s := px.StringFixed(m.pxDec)
		v.LiquidationPx = &s
	}
	return v
}

func (e *Engine) orderView(o *Order) OrderView {
	m := e.markets[o.Symbol]
	pd, sd := int32(8), int32(8)
	if m != nil {
		pd, sd = m.pxDec, m.szDec
	}
	v := OrderView{
		ID: o.ID, Symbol: o.Symbol, Side: o.Side, Type: o.Type,
		Sz: o.Sz.StringFixed(sd), FilledSz: o.Filled.StringFixed(sd), Status: o.Status,
		ReduceOnly: o.ReduceOnly, PostOnly: o.PostOnly, Leverage: o.Leverage, Liquidation: o.Liquidation,
		CreatedAt: msTime(o.CreatedMs), UpdatedAt: msTime(o.UpdatedMs),
	}
	if o.Type == TypeLimit {
		s := o.Px.StringFixed(pd)
		v.Px = &s
	}
	return v
}

// OrderViewOf formats a stored order (history rows).
func (e *Engine) OrderViewOf(o *Order) OrderView {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.orderView(o)
}

func (e *Engine) summary(a *Account) AccountSummary {
	r := e.risk(a, nil, "", 0)
	s := AccountSummary{
		Balance:           e.money(a.Balance),
		Deposits:          e.money(a.Deposits),
		AccountValue:      e.money(r.av),
		Pnl:               e.money(r.av.Sub(a.Deposits)),
		UnrealizedPnl:     e.money(r.upnl),
		MarginUsed:        e.money(r.im),
		MaintenanceMargin: e.money(r.mm),
		FreeCollateral:    e.money(decimal.Max(decimal.Zero, r.av.Sub(r.im).Sub(r.reserved))),
		Positions:         []PositionView{},
		OpenOrders:        []OrderView{},
	}
	for _, sym := range e.symbols {
		if p := a.Positions[sym]; p != nil && !p.Sz.IsZero() {
			s.Positions = append(s.Positions, e.positionView(p, r))
		}
	}
	orders := make([]*Order, 0, len(a.Orders))
	for _, o := range a.Orders {
		orders = append(orders, o)
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].Seq > orders[j].Seq })
	for _, o := range orders {
		s.OpenOrders = append(s.OpenOrders, e.orderView(o))
	}
	if e.p.FaucetAmount.IsPositive() && a.LastFaucetMs > 0 {
		if next := a.LastFaucetMs + e.p.FaucetCooldown.Milliseconds(); next > e.nowMs() {
			t := msTime(next)
			s.FaucetAvailableAt = &t
		}
	}
	return s
}

// Summary returns the account summary.
func (e *Engine) Summary(accountID string) (AccountSummary, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	a := e.accounts[accountID]
	if a == nil {
		return AccountSummary{}, ErrNotFound
	}
	return e.summary(a), nil
}

// ActiveUserAccounts lists user accounts with an open position or order
// (their summaries change with the mark; the realtime fan-out refreshes them).
func (e *Engine) ActiveUserAccounts() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []string
	for id, a := range e.accounts {
		if a.Kind == KindUser && a.Status == AccountActive && (hasPosition(a) || len(a.Orders) > 0) {
			out = append(out, id)
		}
	}
	return out
}

// UserOf returns the user id that owns an account ("" for none).
func (e *Engine) UserOf(accountID string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if a := e.accounts[accountID]; a != nil {
		return a.UserID
	}
	return ""
}
