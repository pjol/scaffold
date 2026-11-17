package engine

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// orderReq is a parsed, validated-shape order.
type orderReq struct {
	sym, side, typ        string
	px                    decimal.Decimal // limit price
	sz, szQuote           decimal.Decimal // one of
	lev                   int
	reduceOnly, postOnly  bool
	slippageBps           int
	ephemeral             bool
	skipMargin, skipBands bool
}

func parseDec(s, field string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return d, reject("invalid", fmt.Sprintf("%s must be a decimal string.", field))
	}
	return d, nil
}

func (r PlaceOrder) parse() (orderReq, error) {
	q := orderReq{sym: strings.ToUpper(strings.TrimSpace(r.Symbol)), side: r.Side, typ: r.Type, lev: r.Leverage,
		reduceOnly: r.ReduceOnly, postOnly: r.PostOnly, slippageBps: r.SlippageBps}
	var err error
	if r.Px != "" {
		if q.px, err = parseDec(r.Px, "px"); err != nil {
			return q, err
		}
	}
	switch {
	case r.Sz != "":
		if q.sz, err = parseDec(r.Sz, "sz"); err != nil {
			return q, err
		}
	case r.SzQuote != "":
		if q.szQuote, err = parseDec(r.SzQuote, "sz_quote"); err != nil {
			return q, err
		}
	default:
		return q, reject("invalid", "sz or sz_quote is required.")
	}
	if q.sz.IsNegative() || q.szQuote.IsNegative() {
		return q, reject("invalid", "Sizes must be positive.")
	}
	return q, nil
}

// PlaceOrder validates, matches and (for limits) rests an order.
func (e *Engine) PlaceOrder(accountID string, req PlaceOrder) (OrderView, []Fill, error) {
	q, err := req.parse()
	if err != nil {
		return OrderView{}, nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	if a == nil {
		return OrderView{}, nil, ErrNotFound
	}
	if a.Status != AccountActive {
		return OrderView{}, nil, ErrClosed
	}
	c := e.begin()
	o, fills, err := e.place(c, a, q)
	if err != nil {
		return OrderView{}, nil, err
	}
	if err := e.commit(c); err != nil {
		return OrderView{}, nil, err
	}
	return e.orderView(o), fills, nil
}

func (e *Engine) nextSeq() int64 { e.seq++; return e.seq }

// reducible is how much an order on side can reduce the account's position.
func reducible(a *Account, sym, side string) decimal.Decimal {
	p := a.Positions[sym]
	if p == nil {
		return decimal.Zero
	}
	if side == Buy && p.Sz.IsNegative() {
		return p.Sz.Abs()
	}
	if side == Sell && p.Sz.IsPositive() {
		return p.Sz
	}
	return decimal.Zero
}

func (e *Engine) position(a *Account, m *market) *Position {
	p := a.Positions[m.spec.Symbol]
	if p == nil {
		p = &Position{Symbol: m.spec.Symbol, MarketID: m.spec.ID, Leverage: 1}
		a.Positions[m.spec.Symbol] = p
	}
	return p
}

func (e *Engine) place(c *cmd, a *Account, q orderReq) (*Order, []Fill, error) {
	m := e.markets[q.sym]
	if m == nil {
		return nil, nil, reject("invalid", "Unknown market "+q.sym+".")
	}
	if q.side != Buy && q.side != Sell {
		return nil, nil, reject("invalid", `side must be "buy" or "sell".`)
	}
	if q.typ != TypeMarket && q.typ != TypeLimit {
		return nil, nil, reject("invalid", `type must be "market" or "limit".`)
	}
	if m.spec.Status == MarketHalted {
		return nil, nil, reject("market_halted", "Trading in "+q.sym+" is paused.")
	}
	if q.lev == 0 || q.reduceOnly {
		// reduce-only orders never change the market's leverage setting
		q.lev = a.leverage(q.sym, min(e.p.DefaultLeverage, m.spec.MaxLeverage))
	}
	if q.lev < 1 || q.lev > m.spec.MaxLeverage {
		return nil, nil, reject("invalid", fmt.Sprintf("Leverage must be 1–%d for %s.", m.spec.MaxLeverage, q.sym))
	}
	if !m.mark.IsPositive() || !m.index.IsPositive() {
		return nil, nil, reject("price_unavailable", q.sym+" has no index price yet.")
	}
	var refPx decimal.Decimal
	if q.typ == TypeLimit {
		if !q.px.IsPositive() {
			return nil, nil, reject("invalid", "px is required for limit orders.")
		}
		q.px = roundTick(q.px, m.tick)
		if !q.px.IsPositive() {
			return nil, nil, reject("invalid", "px rounds to zero at this tick size.")
		}
		refPx = q.px
	} else {
		refPx = m.mark
		if q.postOnly {
			return nil, nil, reject("invalid", "post_only applies to limit orders.")
		}
	}
	if q.sz.IsZero() && q.szQuote.IsPositive() {
		q.sz = q.szQuote.Div(refPx)
	}
	q.sz = q.sz.Truncate(m.szDec)
	if !q.sz.IsPositive() {
		return nil, nil, reject("min_notional", "Order size rounds to zero.")
	}
	pos := decimal.Zero
	if p := a.Positions[q.sym]; p != nil {
		pos = p.Sz
	}
	if q.reduceOnly {
		allowed := reducible(a, q.sym, q.side)
		if !allowed.IsPositive() {
			return nil, nil, reject("reduce_only", "A reduce-only order must shrink a position.")
		}
		q.sz = decimal.Min(q.sz, allowed)
	} else if q.sz.Mul(refPx).LessThan(m.minNotional) {
		return nil, nil, reject("min_notional", fmt.Sprintf("The smallest order is %s.", e.money(m.minNotional)))
	}
	signed := q.sz
	if q.side == Sell {
		signed = signed.Neg()
	}
	delta := pos.Add(signed).Abs().Sub(pos.Abs()) // exposure added if fully filled
	increases := !q.reduceOnly && delta.IsPositive()

	if increases && !a.IsSystem() {
		switch {
		case m.spec.Status == MarketReduceOnly:
			return nil, nil, reject("market_reduce_only", q.sym+" accepts only orders that reduce a position.")
		case m.indexStatus == IndexStale:
			return nil, nil, reject("index_stale", "The index price is stale; only reducing orders are accepted.")
		}
	}
	var worst decimal.Decimal
	if q.typ == TypeLimit {
		if !q.skipBands {
			band := m.index.Mul(e.p.PriceBand)
			if q.px.Sub(m.index).Abs().GreaterThan(band) {
				return nil, nil, reject("price_band", fmt.Sprintf("Limit price must be within %s%% of the index price.",
					e.p.PriceBand.Mul(hundred).String()))
			}
		}
		if q.postOnly {
			if q.side == Buy {
				if ask, ok := m.bestAsk(); ok && q.px.GreaterThanOrEqual(ask) {
					return nil, nil, reject("post_only_would_cross", "A post-only order can't cross the book.")
				}
			} else if bid, ok := m.bestBid(); ok && q.px.LessThanOrEqual(bid) {
				return nil, nil, reject("post_only_would_cross", "A post-only order can't cross the book.")
			}
		}
		worst = q.px
	} else {
		bps := q.slippageBps
		if bps <= 0 {
			bps = e.p.DefaultSlippageBps
		}
		bps = min(bps, 5000)
		slip := decimal.NewFromInt(int64(bps)).Div(decimal.NewFromInt(10_000))
		if q.side == Buy {
			worst = m.mark.Mul(one.Add(slip))
			worst = worst.Div(m.tick).Floor().Mul(m.tick)
		} else {
			worst = m.mark.Mul(one.Sub(slip))
			worst = worst.Div(m.tick).Ceil().Mul(m.tick)
		}
		if !e.hasLiquidity(m, a.ID, q.side, worst) {
			return nil, nil, reject("no_liquidity", "No liquidity within the slippage limit.")
		}
	}
	now := e.nowMs()
	o := &Order{
		ID: newID(), AccountID: a.ID, Symbol: q.sym, MarketID: m.spec.ID, Side: q.side, Type: q.typ, Px: worst, Sz: q.sz,
		Status: StatusOpen, ReduceOnly: q.reduceOnly, PostOnly: q.postOnly, Leverage: q.lev,
		CreatedMs: now, UpdatedMs: now, Ephemeral: q.ephemeral,
	}
	if !a.IsSystem() && !q.skipMargin {
		before := e.risk(a, nil, "", 0)
		after := e.risk(a, o, q.sym, q.lev)
		reqBefore := before.im.Add(before.reserved)
		reqAfter := after.im.Add(after.reserved)
		if reqAfter.GreaterThan(reqBefore) && after.av.LessThan(reqAfter) {
			return nil, nil, reject("insufficient_margin", fmt.Sprintf("Not enough margin: this needs %s and the account is worth %s.",
				e.money(reqAfter), e.money(after.av)))
		}
	}
	// The backstop and the simulated market maker (who must keep both sides of
	// the book quoted) are exempt from the open interest cap.
	if increases && a.Kind != KindBackstop && a.Kind != KindMM {
		if m.oi.Add(delta).Mul(m.mark).GreaterThan(m.oiCap) {
			return nil, nil, reject("oi_cap", "This market has reached its open interest cap.")
		}
	}
	// accepted: mutate
	o.Seq = e.nextSeq()
	p := e.position(a, m)
	if p.Leverage != q.lev {
		p.Leverage = q.lev
		c.touchPosition(a, p)
	}
	var fills []Fill
	if !q.postOnly {
		fills = e.match(c, m, o, a, false)
	}
	switch {
	case !o.Remaining().IsPositive():
		o.Status = StatusFilled
	case o.Type == TypeMarket:
		o.Status = StatusCancelled // IOC remainder
	case o.ReduceOnly && !reducible(a, q.sym, q.side).IsPositive():
		o.Status = StatusCancelled
	default:
		m.insert(o)
		a.Orders[o.ID] = o
		e.orders[o.ID] = o
		c.books[q.sym] = true
	}
	o.UpdatedMs = now
	c.touchOrder(o)
	e.emitOrder(c, a, o)
	e.updateMark(m)
	return o, fills, nil
}

func (e *Engine) hasLiquidity(m *market, self, side string, worst decimal.Decimal) bool {
	book := m.asks
	if side == Sell {
		book = m.bids
	}
	for _, o := range book {
		if !crosses(side, worst, o.Px) {
			return false
		}
		if o.AccountID != self {
			return true
		}
	}
	return false
}

func (e *Engine) emitOrder(c *cmd, a *Account, o *Order) {
	if a.Kind != KindUser {
		return
	}
	v := e.orderView(o)
	id := a.ID
	c.emit(func(p Publisher) { p.AccountEvent(id, AccountEvent{Type: "order", Order: &v}) })
}

// match crosses taker against the opposite book. Self-trades expire the maker.
func (e *Engine) match(c *cmd, m *market, taker *Order, ta *Account, liq bool) []Fill {
	var fills []Fill
	for taker.Remaining().IsPositive() {
		book := m.asks
		if taker.Side == Sell {
			book = m.bids
		}
		if len(book) == 0 {
			break
		}
		mk := book[0]
		if !crosses(taker.Side, taker.Px, mk.Px) {
			break
		}
		if mk.AccountID == taker.AccountID {
			e.cancel(c, mk) // self-trade prevention: expire maker
			continue
		}
		ma := e.accounts[mk.AccountID]
		qty := decimal.Min(taker.Remaining(), mk.Remaining())
		if taker.ReduceOnly {
			r := reducible(ta, m.spec.Symbol, taker.Side)
			if !r.IsPositive() {
				break
			}
			qty = decimal.Min(qty, r)
		}
		if mk.ReduceOnly {
			r := reducible(ma, m.spec.Symbol, mk.Side)
			if !r.IsPositive() {
				e.cancel(c, mk)
				continue
			}
			qty = decimal.Min(qty, r)
		}
		fills = append(fills, e.execute(c, m, taker, ta, mk.ID, mk, ma, qty, mk.Px, liq, e.p.TakerFee, e.p.MakerFee))
		c.books[m.spec.Symbol] = true
		if !mk.Remaining().IsPositive() {
			mk.Status = StatusFilled
			e.unrest(m, ma, mk)
			c.touchOrder(mk)
			e.emitOrder(c, ma, mk)
		} else if mk.ReduceOnly && !reducible(ma, m.spec.Symbol, mk.Side).IsPositive() {
			e.cancel(c, mk)
		} else {
			c.touchOrder(mk)
			e.emitOrder(c, ma, mk)
		}
	}
	return fills
}

func (e *Engine) unrest(m *market, a *Account, o *Order) {
	m.remove(o)
	delete(a.Orders, o.ID)
	delete(e.orders, o.ID)
}

func (e *Engine) cancel(c *cmd, o *Order) {
	m := e.markets[o.Symbol]
	a := e.accounts[o.AccountID]
	e.unrest(m, a, o)
	o.Status = StatusCancelled
	o.UpdatedMs = e.nowMs()
	c.touchOrder(o)
	c.books[o.Symbol] = true
	e.emitOrder(c, a, o)
}

// execute books one trade between taker and maker (maker nil = backstop takeover).
func (e *Engine) execute(c *cmd, m *market, taker *Order, ta *Account, makerOrderID string, maker *Order, ma *Account,
	qty, px decimal.Decimal, liq bool, takerFee, makerFee decimal.Decimal) Fill {
	now := e.nowMs()
	tradeID := newID()
	tf := e.applyFill(c, m, ta, taker.ID, taker.Side, qty, px, takerFee, "taker", liq, tradeID, now)
	taker.Filled = taker.Filled.Add(qty)
	taker.UpdatedMs = now
	e.applyFill(c, m, ma, makerOrderID, opposite(taker.Side), qty, px, makerFee, "maker", liq, tradeID, now)
	if maker != nil {
		maker.Filled = maker.Filled.Add(qty)
		maker.UpdatedMs = now
	}
	t := TradeRec{ID: tradeID, Symbol: m.spec.Symbol, MarketID: m.spec.ID, Px: px, Sz: qty, Side: taker.Side, Liquidation: liq, Ts: now}
	if taker.Side == Buy {
		t.BuyerID, t.SellerID, t.BuyerSim, t.SellerSim = ta.ID, ma.ID, ta.IsSim(), ma.IsSim()
	} else {
		t.BuyerID, t.SellerID, t.BuyerSim, t.SellerSim = ma.ID, ta.ID, ma.IsSim(), ta.IsSim()
	}
	m.pushTrade(t)
	m.lastPx, m.hasLast = px, true
	c.b.Trades = append(c.b.Trades, t)
	e.candleTrade(c, m, qty, px, now, ta.IsSim() || ma.IsSim())
	return tf
}

func (e *Engine) applyFill(c *cmd, m *market, a *Account, orderID, side string, qty, px, feeRate decimal.Decimal,
	liquidity string, liq bool, tradeID string, now int64) Fill {
	p := e.position(a, m)
	old := p.Sz
	signed := qty
	if side == Sell {
		signed = qty.Neg()
	}
	realized := p.apply(signed, px)
	fee := qty.Mul(px).Mul(feeRate).Round(8)
	a.Balance = a.Balance.Add(realized).Sub(fee)
	// The fee account collects fees and pays maker rebates (a negative fee), so
	// trading never creates or destroys collateral.
	if !fee.IsZero() && e.fee != nil && a != e.fee {
		e.fee.Balance = e.fee.Balance.Add(fee)
		c.touchAccount(e.fee)
	}
	m.oi = m.oi.Add(decimal.Max(p.Sz, decimal.Zero)).Sub(decimal.Max(old, decimal.Zero))
	if a.IsSim() {
		m.simOI = m.simOI.Add(p.Sz.Abs()).Sub(old.Abs())
	}
	e.noteTrader(a, now)
	c.touchAccount(a)
	c.touchPosition(a, p)
	fr := FillRec{ID: newID(), AccountID: a.ID, OrderID: orderID, TradeID: tradeID, Symbol: m.spec.Symbol, MarketID: m.spec.ID,
		Side: side, Px: px, Sz: qty, Fee: fee, RealizedPnl: realized, Liquidity: liquidity, Liquidation: liq, Ts: now}
	f := e.fillView(m, fr)
	if a.keepsHistory() {
		c.b.Fills = append(c.b.Fills, fr)
		if !fee.IsZero() { // a rebate is a positive fee row
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerFee, Amount: fee.Neg(), Symbol: m.spec.Symbol, MarketID: m.spec.ID, Ts: now})
		}
		if !realized.IsZero() {
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerRealizedPnl, Amount: realized, Symbol: m.spec.Symbol, MarketID: m.spec.ID, Ts: now})
		}
		id := a.ID
		c.emit(func(p Publisher) { p.AccountEvent(id, AccountEvent{Type: "fill", Fill: &f}) })
	}
	return f
}

func (e *Engine) fillView(m *market, f FillRec) Fill {
	return Fill{ID: f.ID, OrderID: f.OrderID, Symbol: f.Symbol, Side: f.Side, Px: f.Px.StringFixed(m.pxDec),
		Sz: f.Sz.StringFixed(m.szDec), Fee: e.money4(f.Fee), RealizedPnl: e.money4(f.RealizedPnl), Liquidity: f.Liquidity,
		Liquidation: f.Liquidation, CreatedAt: msTime(f.Ts)}
}

// FillViewOf formats a stored fill.
func (e *Engine) FillViewOf(f FillRec) Fill {
	e.mu.RLock()
	m := e.markets[f.Symbol]
	e.mu.RUnlock()
	if m == nil {
		return Fill{ID: f.ID, OrderID: f.OrderID, Symbol: f.Symbol, Side: f.Side, Px: f.Px.String(), Sz: f.Sz.String(),
			Fee: e.money4(f.Fee), RealizedPnl: e.money4(f.RealizedPnl), Liquidity: f.Liquidity, Liquidation: f.Liquidation,
			CreatedAt: msTime(f.Ts)}
	}
	return e.fillView(m, f)
}

// Cancel cancels one open order.
func (e *Engine) Cancel(accountID, orderID string) (OrderView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	o := e.orders[orderID]
	if o == nil || o.AccountID != accountID {
		return OrderView{}, &Error{Status: 404, Code: "not_found", Message: "No open order with that id."}
	}
	c := e.begin()
	e.cancel(c, o)
	e.updateMark(e.markets[o.Symbol])
	if err := e.commit(c); err != nil {
		return OrderView{}, err
	}
	return e.orderView(o), nil
}

// CancelAll cancels the account's open orders (in one market if symbol != "").
func (e *Engine) CancelAll(accountID, symbol string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	if a == nil {
		return 0, ErrNotFound
	}
	c := e.begin()
	n := e.cancelAll(c, a, symbol)
	if err := e.commit(c); err != nil {
		return 0, err
	}
	return n, nil
}

func (e *Engine) cancelAll(c *cmd, a *Account, symbol string) int {
	n := 0
	for _, o := range a.Orders {
		if symbol == "" || o.Symbol == symbol {
			e.cancel(c, o)
			n++
		}
	}
	return n
}

// ClosePosition market-closes sz (default all) of a position.
func (e *Engine) ClosePosition(accountID, symbol string, sz string) (OrderView, []Fill, error) {
	symbol = strings.ToUpper(symbol)
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	if a == nil {
		return OrderView{}, nil, ErrNotFound
	}
	p := a.Positions[symbol]
	if p == nil || p.Sz.IsZero() {
		return OrderView{}, nil, reject("invalid", "No open position in "+symbol+".")
	}
	q := orderReq{sym: symbol, side: Sell, typ: TypeMarket, sz: p.Sz.Abs(), lev: p.Leverage, reduceOnly: true}
	if p.Sz.IsNegative() {
		q.side = Buy
	}
	if sz != "" {
		d, err := parseDec(sz, "sz")
		if err != nil {
			return OrderView{}, nil, err
		}
		if !d.IsPositive() {
			return OrderView{}, nil, reject("invalid", "sz must be positive.")
		}
		q.sz = decimal.Min(d, q.sz)
	}
	c := e.begin()
	o, fills, err := e.place(c, a, q)
	if err != nil {
		return OrderView{}, nil, err
	}
	if err := e.commit(c); err != nil {
		return OrderView{}, nil, err
	}
	return e.orderView(o), fills, nil
}

// Quote is one market-maker level.
type Quote struct {
	Side   string
	Px, Sz decimal.Decimal
}

// ReplaceQuotes atomically cancels a system account's orders in one market
// and posts new quotes (ephemeral). A quote that crosses a resting order
// trades against it at that order's price first, so limit orders fill when
// fair value moves through them; the remainder rests.
func (e *Engine) ReplaceQuotes(accountID, symbol string, quotes []Quote, lev int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	m := e.markets[symbol]
	if a == nil || m == nil {
		return ErrNotFound
	}
	c := e.begin()
	for _, o := range a.Orders {
		if o.Symbol == symbol {
			e.unrest(m, a, o)
			o.Status = StatusCancelled
		}
	}
	c.books[symbol] = true
	for _, q := range quotes {
		_, _, _ = e.place(c, a, orderReq{sym: symbol, side: q.Side, typ: TypeLimit, px: q.Px, sz: q.Sz, lev: lev,
			ephemeral: true})
	}
	e.updateMark(m)
	return e.commit(c)
}
