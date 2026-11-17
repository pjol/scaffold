package engine

import "time"

// Wire types: exactly packages/shared/src/exchange/types.ts. Prices, sizes and
// amounts are decimal strings; times are RFC 3339 except candle t (unix ms).

type MarketStats struct {
	Symbol         string     `json:"symbol"`
	MarkPx         string     `json:"mark_px"`
	IndexPx        string     `json:"index_px"`
	IndexStatus    string     `json:"index_status"`
	IndexUpdatedAt *time.Time `json:"index_updated_at"`
	MidPx          *string    `json:"mid_px"`
	PrevDayPx      string     `json:"prev_day_px"`
	DayChangePct   string     `json:"day_change_pct"`
	DayVolume      string     `json:"day_volume"` // quote currency
	// DayVolumeSimPct: share of 24h volume with a simulated account on at least one side ("0.0"–"100.0").
	DayVolumeSimPct   string    `json:"day_volume_sim_pct"`
	OpenInterest      string    `json:"open_interest"`
	OpenInterestValue string    `json:"open_interest_value"`
	FundingRate       string    `json:"funding_rate"` // per hour
	NextFundingAt     time.Time `json:"next_funding_at"`
}

type MarketView struct {
	ID          string `json:"id"`
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	Status      string `json:"status"`
	MaxLeverage int    `json:"max_leverage"`
	// MaintenanceMarginRatio = 1 / (2 × max_leverage); fees are fractions of notional.
	MaintenanceMarginRatio string      `json:"maintenance_margin_ratio"`
	TakerFee               string      `json:"taker_fee"`
	MakerFee               string      `json:"maker_fee"`
	SzDecimals             int         `json:"sz_decimals"`
	TickSize               string      `json:"tick_size"`
	MinNotional            string      `json:"min_notional"`
	OICap                  string      `json:"oi_cap"`
	Stats                  MarketStats `json:"stats"`
}

type BookLevel struct {
	Px string `json:"px"`
	Sz string `json:"sz"`
	N  int    `json:"n"`
}

type Book struct {
	Symbol string      `json:"symbol"`
	Bids   []BookLevel `json:"bids"`
	Asks   []BookLevel `json:"asks"`
	At     time.Time   `json:"at"`
}

// Trade is a public trade. It never names the accounts, only whether a
// simulated account was on either side.
type Trade struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Px          string    `json:"px"`
	Sz          string    `json:"sz"`
	Side        string    `json:"side"` // taker side
	Sim         bool      `json:"sim"`  // a simulated account on either side
	Liquidation bool      `json:"liquidation"`
	At          time.Time `json:"at"`
}

type Candle struct {
	T        int64  `json:"t"` // bucket open, unix ms
	O        string `json:"o"`
	H        string `json:"h"`
	L        string `json:"l"`
	C        string `json:"c"`
	V        string `json:"v"`
	Backfill bool   `json:"backfill"` // a daily bar from the price feed's history, before the market listed
}

type OrderView struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"`
	Type        string    `json:"type"`
	Px          *string   `json:"px"`
	Sz          string    `json:"sz"`
	FilledSz    string    `json:"filled_sz"`
	Status      string    `json:"status"`
	ReduceOnly  bool      `json:"reduce_only"`
	PostOnly    bool      `json:"post_only"`
	Leverage    int       `json:"leverage"`
	Liquidation bool      `json:"liquidation"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Fill struct {
	ID          string    `json:"id"`
	OrderID     string    `json:"order_id"`
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"`
	Px          string    `json:"px"`
	Sz          string    `json:"sz"`
	Fee         string    `json:"fee"`
	RealizedPnl string    `json:"realized_pnl"`
	Liquidity   string    `json:"liquidity"`
	Liquidation bool      `json:"liquidation"`
	CreatedAt   time.Time `json:"created_at"`
}

type PositionView struct {
	Symbol           string  `json:"symbol"`
	Sz               string  `json:"sz"`
	EntryPx          string  `json:"entry_px"`
	MarkPx           string  `json:"mark_px"`
	Notional         string  `json:"notional"`
	UnrealizedPnl    string  `json:"unrealized_pnl"`
	RoePct           string  `json:"roe_pct"`
	Leverage         int     `json:"leverage"`
	MarginUsed       string  `json:"margin_used"`
	LiquidationPx    *string `json:"liquidation_px"`
	FundingSinceOpen string  `json:"funding_since_open"`
}

type AccountSummary struct {
	Balance           string         `json:"balance"`
	Deposits          string         `json:"deposits"`
	AccountValue      string         `json:"account_value"`
	Pnl               string         `json:"pnl"` // account value − deposits
	UnrealizedPnl     string         `json:"unrealized_pnl"`
	MarginUsed        string         `json:"margin_used"`
	MaintenanceMargin string         `json:"maintenance_margin"`
	FreeCollateral    string         `json:"free_collateral"`
	Positions         []PositionView `json:"positions"`
	OpenOrders        []OrderView    `json:"open_orders"`
	// FaucetAvailableAt is when the faucet can next be claimed (null: now, or no faucet).
	FaucetAvailableAt *time.Time `json:"faucet_available_at"`
}

type FundingPayment struct {
	Symbol    string    `json:"symbol"`
	Rate      string    `json:"rate"`
	Sz        string    `json:"sz"`
	Payment   string    `json:"payment"`
	CreatedAt time.Time `json:"created_at"`
}

type LedgerEntry struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Amount    string    `json:"amount"`
	Symbol    string    `json:"symbol,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AccountEvent is the data of an exchange.account realtime event.
type AccountEvent struct {
	Type    string          `json:"type"` // order | fill | funding | liquidation | transfer
	Fill    *Fill           `json:"fill,omitempty"`
	Order   *OrderView      `json:"order,omitempty"`
	Payment *FundingPayment `json:"payment,omitempty"`
	Symbol  string          `json:"symbol,omitempty"`
	Sz      string          `json:"sz,omitempty"`
	Px      string          `json:"px,omitempty"`
	Amount  string          `json:"amount,omitempty"`
	Status  string          `json:"status,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

// PlaceOrder is the POST /v1/exchange/orders body.
type PlaceOrder struct {
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Type        string `json:"type"`
	Sz          string `json:"sz,omitempty"`       // base units
	SzQuote     string `json:"sz_quote,omitempty"` // or a quote-currency amount, converted at the limit price (market: mark)
	Px          string `json:"px,omitempty"`
	Leverage    int    `json:"leverage,omitempty"`
	ReduceOnly  bool   `json:"reduce_only,omitempty"`
	PostOnly    bool   `json:"post_only,omitempty"`
	SlippageBps int    `json:"slippage_bps,omitempty"`
}

// Publisher receives engine events (the realtime fan-out). Calls happen inside
// the engine lock and must not block or call back into the engine.
type Publisher interface {
	Trades(symbol string, trades []Trade)
	BookChanged(symbol string)
	AccountChanged(accountID string)
	AccountEvent(accountID string, ev AccountEvent)
}

type nopPublisher struct{}

func (nopPublisher) Trades(string, []Trade)            {}
func (nopPublisher) BookChanged(string)                {}
func (nopPublisher) AccountChanged(string)             {}
func (nopPublisher) AccountEvent(string, AccountEvent) {}

func msTime(ms int64) time.Time { return time.UnixMilli(ms).UTC() }
