package engine

import "context"

// Store persists engine state. Commit is called inside the engine lock with
// everything one command changed; implementations write it in one transaction
// and must copy what they need before returning (the engine keeps mutating
// the same objects afterwards).
type Store interface {
	Load(ctx context.Context) (*Snapshot, error)
	Commit(ctx context.Context, b *Batch) error
}

// Snapshot is the state rebuilt on boot.
type Snapshot struct {
	Accounts   []*Account // with Positions populated; Orders filled from OpenOrders by the engine
	OpenOrders []*Order   // status open
	Trades     []TradeRec // recent trades, oldest first
	Candles    []CandleRec
	MaxSeq     int64
}

type posKey struct{ account, symbol string }

// Batch is everything one engine command changed.
type Batch struct {
	NewAccounts  []*Account
	Accounts     map[string]*Account
	Positions    map[posKey]*Position
	Orders       map[string]*Order
	Trades       []TradeRec
	Fills        []FillRec
	Ledger       []LedgerRec
	Funding      []FundingRec
	FundingRates []FundingRateRec
	Liquidations []LiquidationRec
	Candles      []CandleRec
	Transfers    []TransferResult
	Closed       []string // accounts whose close-out finished (status closed)
}

func newBatch() *Batch {
	return &Batch{
		Accounts:  map[string]*Account{},
		Positions: map[posKey]*Position{},
		Orders:    map[string]*Order{},
	}
}

// PositionsList returns the dirty positions with their owners.
func (b *Batch) PositionsList() (accounts []string, positions []*Position) {
	for k, p := range b.Positions {
		accounts = append(accounts, k.account)
		positions = append(positions, p)
	}
	return
}

func (b *Batch) Empty() bool {
	return len(b.NewAccounts) == 0 && len(b.Accounts) == 0 && len(b.Positions) == 0 && len(b.Orders) == 0 &&
		len(b.Trades) == 0 && len(b.Fills) == 0 && len(b.Ledger) == 0 && len(b.Funding) == 0 &&
		len(b.FundingRates) == 0 && len(b.Liquidations) == 0 && len(b.Candles) == 0 && len(b.Transfers) == 0 &&
		len(b.Closed) == 0
}

// MemStore is an in-memory Store for tests: it records commits and loads nothing.
type MemStore struct{ Commits []*Batch }

func (s *MemStore) Load(context.Context) (*Snapshot, error) { return &Snapshot{}, nil }
func (s *MemStore) Commit(_ context.Context, b *Batch) error {
	s.Commits = append(s.Commits, b)
	return nil
}
