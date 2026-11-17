package engine

import (
	"slices"
)

// Atomicity: every engine command is all-or-nothing across memory and the
// database. begin() takes a checkpoint of all mutable engine state before the
// command touches anything; if the store transaction fails, commit() restores
// the checkpoint, drops the command's events and returns ErrPersist.
//
// The checkpoint is a full copy of the mutable state rather than an undo log,
// so correctness does not depend on every mutation site remembering to record
// itself. Costs are O(open orders + positions + accounts + markets) per
// command. Closed candles and trade rings are append-only (trimming
// reallocates), so a slice header is an exact snapshot of them.

type checkpoint struct {
	seq, nextFunding, lastSample int64
	traders, simTraders          int
	tradersKey                   int64

	accounts []acctSnap
	markets  []mktSnap
}

type acctSnap struct {
	a         *Account
	v         Account              // scalar fields (maps restored below)
	positions map[string]*Position // the map as it was
	posVals   []posSnap
}

type posSnap struct {
	p *Position
	v Position
}

type mktSnap struct {
	m      *market
	v      market // shallow copy: scalars and slice headers
	bids   []*Order
	asks   []*Order
	cur    *CandleRec // copy of the open candle (nil if none)
	orders []orderSnap
}

type orderSnap struct {
	o *Order
	v Order
}

func (e *Engine) checkpoint() *checkpoint {
	cp := &checkpoint{seq: e.seq, nextFunding: e.nextFunding, lastSample: e.lastSample,
		traders: e.traders24h, simTraders: e.simTraders24h, tradersKey: e.tradersKey}
	cp.accounts = make([]acctSnap, 0, len(e.accounts))
	for _, a := range e.accounts {
		s := acctSnap{a: a, v: *a, positions: make(map[string]*Position, len(a.Positions))}
		for sym, p := range a.Positions {
			s.positions[sym] = p
			s.posVals = append(s.posVals, posSnap{p, *p})
		}
		cp.accounts = append(cp.accounts, s)
	}
	cp.markets = make([]mktSnap, 0, len(e.markets))
	for _, m := range e.markets {
		s := mktSnap{m: m, v: *m, bids: slices.Clone(m.bids), asks: slices.Clone(m.asks)}
		if m.cur != nil {
			c := *m.cur
			s.cur = &c
		}
		s.orders = make([]orderSnap, 0, len(m.bids)+len(m.asks))
		for _, o := range m.bids {
			s.orders = append(s.orders, orderSnap{o, *o})
		}
		for _, o := range m.asks {
			s.orders = append(s.orders, orderSnap{o, *o})
		}
		cp.markets = append(cp.markets, s)
	}
	return cp
}

// restore puts the engine back to the checkpoint. Accounts created since the
// checkpoint are listed in created.
func (e *Engine) restore(cp *checkpoint, created []*Account) {
	for _, a := range created {
		delete(e.accounts, a.ID)
	}
	e.seq, e.nextFunding, e.lastSample = cp.seq, cp.nextFunding, cp.lastSample
	e.traders24h, e.simTraders24h, e.tradersKey = cp.traders, cp.simTraders, cp.tradersKey
	for _, s := range cp.accounts {
		*s.a = s.v
		s.a.Positions = s.positions
		for _, ps := range s.posVals {
			*ps.p = ps.v
		}
		s.a.Orders = map[string]*Order{}
	}
	for _, s := range cp.markets {
		*s.m = s.v
		s.m.bids, s.m.asks, s.m.cur = s.bids, s.asks, s.cur
		for _, os := range s.orders {
			*os.o = os.v
		}
	}
	// Lookup indexes are derived from the accounts and the books.
	e.byUser = map[string]*Account{}
	e.system = map[string]*Account{}
	for _, a := range e.accounts {
		e.indexAccount(a)
	}
	e.orders = map[string]*Order{}
	for _, m := range e.markets {
		for _, side := range [][]*Order{m.bids, m.asks} {
			for _, o := range side {
				e.orders[o.ID] = o
				if a := e.accounts[o.AccountID]; a != nil {
					a.Orders[o.ID] = o
				}
			}
		}
	}
}
