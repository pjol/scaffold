// Package engine is the exchange's matching engine: per-market order books
// with price-time priority, cross margin, mark price, funding, liquidations
// and candles. All money is decimal. Every mutation runs under one mutex and
// its results are written in one store transaction before the lock is
// released; if the write fails, memory is rolled back (checkpoint.go).
//
// The engine is in-memory and must run in exactly one process at a time (the
// exchange service elects one with a Postgres advisory lock).
package engine

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Params are the engine's risk and fee knobs.
type Params struct {
	TakerFee            decimal.Decimal // fraction of notional
	MakerFee            decimal.Decimal
	PriceBand           decimal.Decimal // limit px within ±15% of the index
	MarkClamp           decimal.Decimal // mark within ±10% of the index
	LiqSlippage         decimal.Decimal // liquidation orders fill within ±10% of mark
	LiqPartialThreshold decimal.Decimal // positions above this notional liquidate LiqPartialFraction at a time
	LiqPartialFraction  decimal.Decimal
	LiqCooldown         time.Duration   // after a partial liquidation; whole positions go while it runs
	BackstopRatio       decimal.Decimal // the backstop takes over below 2/3 of maintenance margin
	EMAAlpha            decimal.Decimal // per-second weight of the 150 s (mid − index) EMA
	ImpactNotional      decimal.Decimal // notional for funding impact prices
	InterestRate8h      decimal.Decimal
	PremiumClamp8h      decimal.Decimal
	MaxFundingHourly    decimal.Decimal
	FundingInterval     time.Duration
	FundingSampleEvery  time.Duration
	FaucetAmount        decimal.Decimal // zero: no faucet
	FaucetCooldown      time.Duration
	WelcomeCredit       decimal.Decimal // credited when a user's account is created
	DefaultSlippageBps  int
	DefaultLeverage     int
	QuoteDecimals       int32 // display decimals of the quote currency
	AllowSeedPrices     bool  // trade on catalog seed prices before the feed reports (development)
	IndexStaleAfter     time.Duration
}

func DefaultParams() Params {
	d := decimal.RequireFromString
	return Params{
		TakerFee:            d("0.00045"),
		MakerFee:            d("0.00015"),
		PriceBand:           d("0.15"),
		MarkClamp:           d("0.10"),
		LiqSlippage:         d("0.10"),
		LiqPartialThreshold: d("10000"),
		LiqPartialFraction:  d("0.2"),
		LiqCooldown:         30 * time.Second,
		BackstopRatio:       decimal.NewFromInt(2).Div(decimal.NewFromInt(3)),
		EMAAlpha:            d("0.0066445"), // 1 − e^(−1/150)
		ImpactNotional:      d("1000"),
		InterestRate8h:      d("0.0001"),
		PremiumClamp8h:      d("0.0005"),
		MaxFundingHourly:    d("0.04"),
		FundingInterval:     time.Hour,
		FundingSampleEvery:  5 * time.Second,
		FaucetAmount:        decimal.Zero,
		FaucetCooldown:      time.Hour,
		WelcomeCredit:       decimal.Zero,
		DefaultSlippageBps:  500,
		DefaultLeverage:     3,
		QuoteDecimals:       2,
		IndexStaleAfter:     5 * time.Minute,
	}
}

// Error is an order rejection or client error with a stable code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func reject(code, msg string) *Error {
	st := http.StatusUnprocessableEntity
	if code == "invalid" {
		st = http.StatusBadRequest
	}
	return &Error{Status: st, Code: code, Message: msg}
}

var (
	ErrNotFound      = &Error{Status: http.StatusNotFound, Code: "not_found", Message: "Not found."}
	ErrUnknownMarket = &Error{Status: http.StatusNotFound, Code: "not_found", Message: "Unknown market."}
	ErrClosed        = &Error{Status: http.StatusConflict, Code: "account_closed", Message: "This trading account is closed."}
)

type market struct {
	spec        MarketSpec
	tick        decimal.Decimal
	pxDec       int32
	szDec       int32
	minNotional decimal.Decimal
	oiCap       decimal.Decimal

	bids []*Order // px desc, seq asc
	asks []*Order // px asc, seq asc

	index       decimal.Decimal
	indexStatus string
	indexMs     int64

	mark    decimal.Decimal
	ema     decimal.Decimal
	emaInit bool
	lastPx  decimal.Decimal
	hasLast bool

	premSum decimal.Decimal
	premN   int

	oi    decimal.Decimal // long units (= short units)
	simOI decimal.Decimal // Σ |position| of simulated accounts (both sides)

	candles []CandleRec // closed 1m candles, oldest first (≤ 2 × candleKeep)
	cur     *CandleRec  // open 1m candle

	trades []TradeRec // recent, oldest first

	markSmoothing // book-component hold + per-second mark rate limit (mark.go)

	// 24h stats cache, refreshed by Tick
	dayKey  int64
	dayN    int
	dayPrev decimal.Decimal
	dayVol  decimal.Decimal
	dayVSim decimal.Decimal
	dayHas  bool
}

const (
	candleKeep = 1500
	tradeKeep  = 200
)

var (
	one     = decimal.NewFromInt(1)
	two     = decimal.NewFromInt(2)
	hundred = decimal.NewFromInt(100)
)

// IndexUpdate is an index price from the price feed (or an admin).
type IndexUpdate struct {
	Symbol string
	Price  decimal.Decimal
	Status string // live | manual | stale
	Ms     int64
}

// Engine is safe for concurrent use.
type Engine struct {
	mu      sync.RWMutex
	p       Params
	store   Store
	pub     Publisher
	onFill  func(FillEvent) // modules' fill hook (SetFillHook)
	log     *slog.Logger
	now     func() time.Time
	markets map[string]*market
	symbols []string // display order

	accounts map[string]*Account
	byUser   map[string]*Account
	system   map[string]*Account // kind|label → account
	orders   map[string]*Order   // every open order

	seq         int64
	nextFunding int64 // unix ms
	lastSample  int64

	backstop *Account
	fee      *Account

	// accounts with a fill in the last 24h (maintained on fills, re-counted each minute)
	traders24h, simTraders24h int
	tradersKey                int64

	indexCh chan IndexUpdate
	quoteCh chan string // symbols whose index moved (the simulation re-quotes)
}

// New builds an engine over the given markets. Call Load before use.
func New(p Params, store Store, log *slog.Logger, markets []MarketSpec) *Engine {
	if log == nil {
		log = slog.Default()
	}
	if p.DefaultLeverage < 1 {
		p.DefaultLeverage = 1
	}
	e := &Engine{
		p: p, store: store, pub: nopPublisher{}, log: log, now: time.Now,
		markets:  map[string]*market{},
		accounts: map[string]*Account{},
		byUser:   map[string]*Account{},
		system:   map[string]*Account{},
		orders:   map[string]*Order{},
		indexCh:  make(chan IndexUpdate, 4096),
		quoteCh:  make(chan string, 1024),
	}
	for _, s := range markets {
		e.addMarket(s)
	}
	e.sortSymbols()
	return e
}

// Params returns the engine's parameters.
func (e *Engine) Params() Params { return e.p }

func (e *Engine) addMarket(s MarketSpec) *market {
	m := &market{indexStatus: IndexNone}
	e.applySpec(m, s)
	if !m.index.IsPositive() && e.p.AllowSeedPrices && s.SeedPrice.IsPositive() {
		m.index, m.indexStatus = s.SeedPrice, IndexSeed
	}
	if m.index.IsPositive() {
		m.mark = roundTick(m.index, m.tick)
	}
	e.markets[s.Symbol] = m
	e.symbols = append(e.symbols, s.Symbol)
	return m
}

// applySpec sets a market's parameters (new markets and catalog changes).
func (e *Engine) applySpec(m *market, s MarketSpec) {
	if s.Status == "" {
		s.Status = MarketActive
	}
	if s.MaxLeverage < 1 {
		s.MaxLeverage = 1
	}
	m.spec = s
	m.tick = s.TickSize
	m.pxDec = int32(decimalPlaces(s.TickSize))
	m.szDec = int32(s.SzDecimals)
	m.minNotional = s.MinNotional
	m.oiCap = s.OICap
	if s.ManualPrice.IsPositive() {
		m.index, m.indexStatus, m.indexMs = s.ManualPrice, IndexManual, e.nowMs()
	} else if m.indexStatus == IndexManual {
		// the override was cleared: wait for the feed (or the seed in development)
		m.indexStatus, m.indexMs = IndexNone, 0
		m.index = decimal.Zero
		if e.p.AllowSeedPrices && s.SeedPrice.IsPositive() {
			m.index, m.indexStatus = s.SeedPrice, IndexSeed
		}
	}
}

func (e *Engine) sortSymbols() {
	sort.SliceStable(e.symbols, func(i, j int) bool {
		a, b := e.markets[e.symbols[i]].spec, e.markets[e.symbols[j]].spec
		if a.Sort != b.Sort {
			return a.Sort < b.Sort
		}
		return a.Symbol < b.Symbol
	})
}

func decimalPlaces(d decimal.Decimal) int {
	s := d.String()
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return len(s) - i - 1
	}
	return 0
}

// SetPublisher wires the realtime fan-out.
func (e *Engine) SetPublisher(p Publisher) {
	e.mu.Lock()
	e.pub = p
	e.mu.Unlock()
}

// SetClock overrides time (tests).
func (e *Engine) SetClock(now func() time.Time) { e.now = now }

func (e *Engine) nowMs() int64 { return e.now().UnixMilli() }

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// Load rebuilds books, accounts and candles from the store and creates the
// system accounts if missing.
func (e *Engine) Load(ctx context.Context) error {
	snap, err := e.store.Load(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, a := range snap.Accounts {
		if a.Positions == nil {
			a.Positions = map[string]*Position{}
		}
		a.Orders = map[string]*Order{}
		e.indexAccount(a)
		for sym, p := range a.Positions {
			if m := e.markets[sym]; m != nil {
				if p.Sz.Sign() > 0 {
					m.oi = m.oi.Add(p.Sz)
				}
				if a.IsSim() {
					m.simOI = m.simOI.Add(p.Sz.Abs())
				}
			}
		}
	}
	sort.Slice(snap.OpenOrders, func(i, j int) bool { return snap.OpenOrders[i].Seq < snap.OpenOrders[j].Seq })
	for _, o := range snap.OpenOrders {
		a, m := e.accounts[o.AccountID], e.markets[o.Symbol]
		if a == nil || m == nil {
			continue
		}
		a.Orders[o.ID] = o
		e.orders[o.ID] = o
		m.insert(o)
	}
	e.seq = snap.MaxSeq
	for _, t := range snap.Trades {
		if m := e.markets[t.Symbol]; m != nil {
			m.pushTrade(t)
			m.lastPx, m.hasLast = t.Px, true
		}
	}
	for _, c := range snap.Candles {
		if m := e.markets[c.Symbol]; m != nil {
			m.candles = append(m.candles, c)
			if !m.index.IsPositive() || m.indexStatus == IndexSeed {
				m.mark = c.C
			}
		}
	}
	for _, m := range e.markets {
		if len(m.candles) > candleKeep {
			m.candles = m.candles[len(m.candles)-candleKeep:]
		}
	}
	// Funding intervals that elapsed while the engine was down are skipped, not
	// caught up: there is no premium data for them and traders could not manage
	// positions. The next payment is the next interval boundary after boot.
	e.nextFunding = e.alignedNextFunding(e.nowMs())

	c := e.begin()
	e.backstop = e.ensureSystem(c, KindBackstop, "backstop", decimal.Zero)
	e.fee = e.ensureSystem(c, KindFee, "fees", decimal.Zero)
	return e.commit(c)
}

func (e *Engine) alignedNextFunding(nowMs int64) int64 {
	iv := e.p.FundingInterval.Milliseconds()
	return (nowMs/iv + 1) * iv
}

func (e *Engine) indexAccount(a *Account) {
	e.accounts[a.ID] = a
	if a.UserID != "" {
		e.byUser[a.UserID] = a
	}
	if a.Kind != KindUser && a.Kind != KindSub {
		e.system[a.Kind+"|"+a.Label] = a
	}
}

// ---- commands ----

// cmd collects one command's writes and events, plus the checkpoint to roll
// back to if they can't be persisted.
type cmd struct {
	b       *Batch
	events  []func(Publisher)
	books   map[string]bool
	accts   map[string]bool
	cp      *checkpoint
	created []*Account
}

// begin starts a command. It must be called (under the write lock) before the
// command mutates anything.
func (e *Engine) begin() *cmd {
	return &cmd{b: newBatch(), books: map[string]bool{}, accts: map[string]bool{}, cp: e.checkpoint()}
}

func (c *cmd) touchAccount(a *Account) {
	c.b.Accounts[a.ID] = a
	c.accts[a.ID] = true
}

func (c *cmd) touchPosition(a *Account, p *Position) {
	c.b.Positions[posKey{a.ID, p.Symbol}] = p
	c.accts[a.ID] = true
}

func (c *cmd) touchOrder(o *Order) {
	if !o.Ephemeral {
		c.b.Orders[o.ID] = o
	}
	c.accts[o.AccountID] = true
}

func (c *cmd) emit(f func(Publisher)) { c.events = append(c.events, f) }

// ErrPersist is returned when a command's database write failed; the engine
// state is exactly as it was before the command.
var ErrPersist = &Error{Status: http.StatusServiceUnavailable, Code: "unavailable",
	Message: "Could not save that change. Nothing happened; try again."}

// commit persists the batch in one transaction, then publishes events. If
// the write fails, memory is rolled back to the command's checkpoint, nothing
// is published and ErrPersist is returned.
func (e *Engine) commit(c *cmd) error {
	if !c.b.Empty() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := e.store.Commit(ctx, c.b)
		cancel()
		if err != nil {
			e.restore(c.cp, c.created)
			e.log.Error("exchange engine: persist failed; command rolled back", "err", err)
			return ErrPersist
		}
	}
	for sym := range c.books {
		e.pub.BookChanged(sym)
	}
	if len(c.b.Trades) > 0 {
		bySym := map[string][]Trade{}
		var order []string
		for _, t := range c.b.Trades {
			if _, ok := bySym[t.Symbol]; !ok {
				order = append(order, t.Symbol)
			}
			bySym[t.Symbol] = append(bySym[t.Symbol], e.tradeView(t))
		}
		for _, sym := range order {
			e.pub.Trades(sym, bySym[sym])
		}
	}
	for _, f := range c.events {
		f(e.pub)
	}
	if e.onFill != nil {
		for _, f := range c.b.Fills {
			e.onFill(e.fillEvent(f))
		}
	}
	for id := range c.accts {
		if a := e.accounts[id]; a != nil && a.Kind == KindUser {
			e.pub.AccountChanged(id)
		}
	}
	return nil
}

// ---- helpers ----

func roundTick(px, tick decimal.Decimal) decimal.Decimal {
	if !tick.IsPositive() {
		return px
	}
	return px.Div(tick).Round(0).Mul(tick)
}

func median(xs []decimal.Decimal) decimal.Decimal {
	s := append([]decimal.Decimal(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i].LessThan(s[j]) })
	n := len(s)
	if n == 0 {
		return decimal.Zero
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return s[n/2-1].Add(s[n/2]).Div(two)
}

func clamp(x, lo, hi decimal.Decimal) decimal.Decimal {
	return decimal.Max(lo, decimal.Min(hi, x))
}

func opposite(side string) string {
	if side == Buy {
		return Sell
	}
	return Buy
}

func (e *Engine) money(d decimal.Decimal) string  { return d.StringFixed(e.p.QuoteDecimals) }
func (e *Engine) money4(d decimal.Decimal) string { return d.StringFixed(e.p.QuoteDecimals + 2) }
