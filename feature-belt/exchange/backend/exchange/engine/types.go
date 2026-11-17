package engine

import (
	"github.com/shopspring/decimal"
)

// Account kinds. Users trade; bot and mm are the optional simulation's
// accounts (shown as simulated); backstop and fee are system accounts that are
// never liquidated or ranked.
const (
	KindUser     = "user"
	KindBot      = "bot"      // simulated trader
	KindMM       = "mm"       // simulated market maker
	KindBackstop = "backstop" // takes over positions an account can no longer carry
	KindFee      = "fee"      // collects trading fees
)

// Account statuses. A closing account (its owner deleted their account) is
// closed out by the engine: orders cancelled, positions handed to the
// backstop, balance moved to the backstop.
const (
	AccountActive  = "active"
	AccountClosing = "closing"
	AccountClosed  = "closed"
)

const (
	Buy  = "buy"
	Sell = "sell"

	TypeMarket = "market"
	TypeLimit  = "limit"

	StatusOpen      = "open"
	StatusFilled    = "filled"
	StatusCancelled = "cancelled"
	StatusRejected  = "rejected"
)

// Market statuses: active trades normally, reduce_only accepts only orders
// that shrink a position (a wind-down), halted accepts no orders (cancels and
// liquidations still run).
const (
	MarketActive     = "active"
	MarketReduceOnly = "reduce_only"
	MarketHalted     = "halted"
)

// Index price statuses.
const (
	IndexNone   = "none"   // no price yet: orders are refused
	IndexSeed   = "seed"   // the catalog's seed price (development only)
	IndexLive   = "live"   // from the price feed
	IndexManual = "manual" // set by an admin
	IndexStale  = "stale"  // the feed stopped updating: only reducing orders
)

// Ledger entry kinds.
const (
	LedgerDeposit     = "deposit"
	LedgerWithdrawal  = "withdrawal"
	LedgerFaucet      = "faucet"
	LedgerFee         = "fee"
	LedgerRealizedPnl = "realized_pnl"
	LedgerFunding     = "funding"
	LedgerLiquidation = "liquidation"
	LedgerAdjustment  = "adjustment"
)

// MarketSpec is one listed perpetual, as the app's catalog defines it.
type MarketSpec struct {
	ID          string
	Symbol      string // GOLD, ACME-INDEX
	Name        string
	Category    string // free-form grouping label, may be empty
	Description string
	ImageURL    string
	PriceFeed   string // the key the price feed knows the asset by ("" = Symbol)
	MaxLeverage int
	SzDecimals  int
	TickSize    decimal.Decimal
	MinNotional decimal.Decimal
	OICap       decimal.Decimal // open interest cap, in quote currency
	SeedPrice   decimal.Decimal // reference price before the feed reports (development only); zero = none
	ManualPrice decimal.Decimal // admin override of the index price; zero = none
	Status      string
	Sort        int
}

// FeedKey is the price feed key of the market.
func (s MarketSpec) FeedKey() string {
	if s.PriceFeed != "" {
		return s.PriceFeed
	}
	return s.Symbol
}

// Account is a trader's cross-margin account.
type Account struct {
	ID           string
	UserID       string // users.id for kind user ("" for system and simulated accounts)
	Kind         string
	Label        string
	Status       string
	CreatedMs    int64
	Balance      decimal.Decimal
	Deposits     decimal.Decimal // net collateral put in (PnL = account value − deposits)
	LastFaucetMs int64
	Positions    map[string]*Position // by symbol; kept at sz 0 to remember leverage
	// LiqCooldownUntilMs: after a partial liquidation, further liquidations
	// take whole positions until this time (memory only).
	LiqCooldownUntilMs int64
	// LastTradeMs is the time of the account's latest fill (24h trader counts).
	LastTradeMs int64
	Orders      map[string]*Order // open orders by id
}

// IsSystem reports accounts that are never liquidated or ranked.
func (a *Account) IsSystem() bool {
	return a.Kind == KindMM || a.Kind == KindBackstop || a.Kind == KindFee
}

// IsSim reports the simulation's accounts (their trades are flagged sim).
func (a *Account) IsSim() bool { return a.Kind == KindBot || a.Kind == KindMM }

// keepsHistory reports accounts whose fills and ledger are persisted.
func (a *Account) keepsHistory() bool { return a.Kind == KindUser }

type Position struct {
	Symbol           string
	MarketID         string
	Sz               decimal.Decimal // signed
	EntryPx          decimal.Decimal
	Leverage         int
	FundingSinceOpen decimal.Decimal
}

// apply adds a signed fill to the position and returns realized PnL.
func (p *Position) apply(signed, px decimal.Decimal) decimal.Decimal {
	old := p.Sz
	if old.IsZero() || old.Sign() == signed.Sign() {
		newSz := old.Add(signed)
		p.EntryPx = old.Abs().Mul(p.EntryPx).Add(signed.Abs().Mul(px)).Div(newSz.Abs()).Round(10)
		p.Sz = newSz
		return decimal.Zero
	}
	closeQty := decimal.Min(signed.Abs(), old.Abs())
	realized := closeQty.Mul(px.Sub(p.EntryPx))
	if old.Sign() < 0 {
		realized = realized.Neg()
	}
	newSz := old.Add(signed)
	switch {
	case newSz.IsZero():
		p.EntryPx = decimal.Zero
		p.FundingSinceOpen = decimal.Zero
	case newSz.Sign() != old.Sign():
		p.EntryPx = px
		p.FundingSinceOpen = decimal.Zero
	}
	p.Sz = newSz
	return realized
}

type Order struct {
	ID           string
	AccountID    string
	Symbol       string
	MarketID     string
	Side         string
	Type         string
	Px           decimal.Decimal // limit price; for market orders the worst acceptable price
	Sz           decimal.Decimal
	Filled       decimal.Decimal
	Status       string
	ReduceOnly   bool
	PostOnly     bool
	Leverage     int
	RejectReason string
	Seq          int64
	CreatedMs    int64
	UpdatedMs    int64
	Ephemeral    bool // market-maker quotes: memory only
	Liquidation  bool
}

func (o *Order) Remaining() decimal.Decimal { return o.Sz.Sub(o.Filled) }

// TradeRec is a matched trade (one per maker/taker pair).
type TradeRec struct {
	ID          string
	Symbol      string
	MarketID    string
	Px          decimal.Decimal
	Sz          decimal.Decimal
	Side        string // taker side
	BuyerID     string
	SellerID    string
	BuyerSim    bool
	SellerSim   bool
	Liquidation bool
	Ts          int64
}

// FillRec is one side of a trade, private to its account.
type FillRec struct {
	ID          string
	AccountID   string
	OrderID     string
	TradeID     string
	Symbol      string
	MarketID    string
	Side        string
	Px          decimal.Decimal
	Sz          decimal.Decimal
	Fee         decimal.Decimal
	RealizedPnl decimal.Decimal
	Liquidity   string // maker | taker
	Liquidation bool
	Ts          int64
}

type LedgerRec struct {
	ID        string
	AccountID string
	Kind      string
	Amount    decimal.Decimal
	Symbol    string
	MarketID  string
	Note      string
	Ts        int64
}

type FundingRec struct {
	ID        string
	AccountID string
	Symbol    string
	MarketID  string
	Ts        int64
	Rate      decimal.Decimal // per hour
	Sz        decimal.Decimal
	Payment   decimal.Decimal
}

type FundingRateRec struct {
	Symbol   string
	MarketID string
	Ts       int64
	Rate     decimal.Decimal
	Premium  decimal.Decimal
	Index    decimal.Decimal
}

type LiquidationRec struct {
	ID         string
	AccountID  string
	Symbol     string
	MarketID   string
	Sz         decimal.Decimal
	Px         decimal.Decimal
	BookSz     decimal.Decimal
	BackstopSz decimal.Decimal
	BadDebt    decimal.Decimal
	Ts         int64
}

// CandleRec is a 1m candle (or an aggregate of them).
type CandleRec struct {
	Symbol   string
	MarketID string
	T        int64
	O, H     decimal.Decimal
	L, C     decimal.Decimal
	V        decimal.Decimal // base units
	VQuote   decimal.Decimal // quote volume
	VSim     decimal.Decimal // quote volume with a simulated account on at least one side
}

// Transfer is collateral moving in or out of an account from outside the
// engine (exchange_transfers rows the app inserts).
type Transfer struct {
	ID     string
	UserID string
	Kind   string          // deposit | withdrawal | adjustment
	Amount decimal.Decimal // positive; adjustment may be negative
	Note   string
}

// TransferResult is how the engine settled a Transfer.
type TransferResult struct {
	ID        string
	AccountID string
	Status    string // applied | rejected
	Reason    string
	Ts        int64
}
