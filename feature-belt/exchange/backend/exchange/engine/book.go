package engine

import (
	"sort"

	"github.com/shopspring/decimal"
)

// insert places a resting order by price-time priority.
func (m *market) insert(o *Order) {
	if o.Side == Buy {
		i := sort.Search(len(m.bids), func(i int) bool {
			b := m.bids[i]
			return b.Px.LessThan(o.Px) || (b.Px.Equal(o.Px) && b.Seq > o.Seq)
		})
		m.bids = append(m.bids, nil)
		copy(m.bids[i+1:], m.bids[i:])
		m.bids[i] = o
		return
	}
	i := sort.Search(len(m.asks), func(i int) bool {
		a := m.asks[i]
		return a.Px.GreaterThan(o.Px) || (a.Px.Equal(o.Px) && a.Seq > o.Seq)
	})
	m.asks = append(m.asks, nil)
	copy(m.asks[i+1:], m.asks[i:])
	m.asks[i] = o
}

func (m *market) remove(o *Order) {
	side := &m.asks
	if o.Side == Buy {
		side = &m.bids
	}
	s := *side
	for i, x := range s {
		if x == o {
			*side = append(s[:i], s[i+1:]...)
			return
		}
	}
}

func (m *market) bestBid() (decimal.Decimal, bool) {
	if len(m.bids) == 0 {
		return decimal.Zero, false
	}
	return m.bids[0].Px, true
}

func (m *market) bestAsk() (decimal.Decimal, bool) {
	if len(m.asks) == 0 {
		return decimal.Zero, false
	}
	return m.asks[0].Px, true
}

func (m *market) mid() (decimal.Decimal, bool) {
	b, ok1 := m.bestBid()
	a, ok2 := m.bestAsk()
	if !ok1 || !ok2 {
		return decimal.Zero, false
	}
	return b.Add(a).Div(two), true
}

// crosses reports whether a taker on side with limit px would trade at makerPx.
func crosses(side string, limit, makerPx decimal.Decimal) bool {
	if side == Buy {
		return makerPx.LessThanOrEqual(limit)
	}
	return makerPx.GreaterThanOrEqual(limit)
}

// impactPx is the average price to trade notional on one side of the book
// (sell into bids when side == Sell). ok is false when depth is short.
func (m *market) impactPx(side string, notional decimal.Decimal) (decimal.Decimal, bool) {
	levels := m.asks
	if side == Sell {
		levels = m.bids
	}
	var qty, cost decimal.Decimal
	for _, o := range levels {
		rem := o.Remaining()
		n := rem.Mul(o.Px)
		if cost.Add(n).GreaterThanOrEqual(notional) {
			need := notional.Sub(cost).Div(o.Px)
			qty = qty.Add(need)
			cost = notional
			return cost.Div(qty), true
		}
		qty = qty.Add(rem)
		cost = cost.Add(n)
	}
	return decimal.Zero, false
}

func (m *market) levels(depth int) (bids, asks []BookLevel) {
	agg := func(orders []*Order) []BookLevel {
		out := []BookLevel{}
		var cur decimal.Decimal
		for _, o := range orders {
			px := o.Px.StringFixed(m.pxDec)
			if n := len(out); n > 0 && out[n-1].Px == px {
				cur = cur.Add(o.Remaining())
				out[n-1].Sz = cur.StringFixed(m.szDec)
				out[n-1].N++
				continue
			}
			if len(out) == depth {
				break
			}
			cur = o.Remaining()
			out = append(out, BookLevel{Px: px, Sz: cur.StringFixed(m.szDec), N: 1})
		}
		return out
	}
	return agg(m.bids), agg(m.asks)
}

func (m *market) pushTrade(t TradeRec) {
	m.trades = append(m.trades, t)
	if len(m.trades) > tradeKeep*2 {
		m.trades = append([]TradeRec(nil), m.trades[len(m.trades)-tradeKeep:]...)
	}
}
