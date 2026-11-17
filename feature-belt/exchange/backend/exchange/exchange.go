// Package exchange is an off-chain exchange for perpetual futures on any
// priced asset: markets from the app's catalog, price-time priority order
// books, market and limit orders (reduce-only, post-only), a collateral
// ledger with an optional test-funds faucet, cross margin with per-market max
// leverage, mark price, hourly funding, liquidations with a backstop, open
// interest caps, fees, candles, REST and realtime market data, and an
// optional simulated market maker and traders for demos.
//
// The matching engine (package engine) is in-memory. Exactly one API
// instance runs it at a time, elected with a Postgres advisory lock; the
// others answer engine-backed routes with 503 exchange_standby and take over
// when the leader goes away. History (orders, fills, funding, ledger,
// candles) is read from the database on any instance.
//
// Prices come from a PriceFeed the app installs (SetPriceFeed / PushPrice);
// see feed.go.
package exchange

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/exchange/sim"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/realtime"
)

// Realtime events.
const (
	// On exchange:<market id>.
	EventBook   = "exchange.book"   // Book (top 20 levels a side), at most 4 a second
	EventTrades = "exchange.trades" // {symbol, trades: Trade[]} newest first
	EventTicker = "exchange.ticker" // {stats, candles: {interval: Candle}} every second
	// On exchange:00000000-0000-0000-0000-000000000000 (MarketsTopic).
	EventTickers = "exchange.tickers" // {stats: MarketStats[]} every 2 seconds
	// On the user's own user:<id> topic.
	EventAccount = "exchange.account" // AccountEvent: order, fill, funding, liquidation, transfer
	EventSummary = "exchange.summary" // AccountSummary, after changes and every few seconds while positions are open
)

// MarketsTopic carries every market's stats (the markets list).
var MarketsTopic = realtime.Topic("exchange", uuid.Nil)

// MarketTopic carries one market's book, trades and ticker.
func MarketTopic(marketID string) string { return "exchange:" + marketID }

// Service is the exchange module.
type Service struct {
	d      *app.Deps
	store  *pgStore
	params engine.Params

	quoteCurrency string
	simEnabled    bool
	catalogFile   string
	pricePoll     time.Duration
	retention     time.Duration

	eng atomic.Pointer[engine.Engine] // nil while this instance is on standby
	sp  sparkCache

	// Other modules' hooks (modules.go).
	hooksMu     sync.Mutex
	fillHooks   []func(FillEvent)
	engineHooks []func(context.Context)
	engineCtx   context.Context // set while this instance runs the engine
	fillDrops   atomic.Int64
}

// leaderLockKey is the session advisory lock the engine's process holds.
var leaderLockKey = jobs.LockKey("exchange.engine")

func New(d *app.Deps) *Service {
	p := engine.DefaultParams()
	p.TakerFee = decimalEnv("EXCHANGE_TAKER_FEE", "0.00045", decimal.Zero, decimal.RequireFromString("0.05"))
	p.MakerFee = decimalEnv("EXCHANGE_MAKER_FEE", "0.00015", decimal.RequireFromString("-0.01"), decimal.RequireFromString("0.05"))
	if p.MakerFee.IsNegative() && p.MakerFee.Neg().GreaterThan(p.TakerFee) {
		config.Problem("EXCHANGE_MAKER_FEE: a maker rebate can't exceed the taker fee")
	}
	p.FaucetAmount = decimalEnv("EXCHANGE_FAUCET_AMOUNT", "0", decimal.Zero, decimal.NewFromInt(1_000_000_000))
	p.WelcomeCredit = p.FaucetAmount
	p.FaucetCooldown = durationEnv("EXCHANGE_FAUCET_COOLDOWN", time.Hour, time.Minute, 30*24*time.Hour)
	p.LiqPartialThreshold = decimalEnv("EXCHANGE_LIQ_PARTIAL_NOTIONAL", "10000", decimal.NewFromInt(1), decimal.NewFromInt(1_000_000_000_000))
	p.FundingInterval = durationEnv("EXCHANGE_FUNDING_INTERVAL", time.Hour, time.Minute, 8*time.Hour)
	p.IndexStaleAfter = durationEnv("EXCHANGE_PRICE_STALE_AFTER", 5*time.Minute, 10*time.Second, 24*time.Hour)
	p.DefaultLeverage = config.Int("EXCHANGE_DEFAULT_LEVERAGE", 3)
	if p.DefaultLeverage < 1 || p.DefaultLeverage > 100 {
		config.Problem("EXCHANGE_DEFAULT_LEVERAGE must be 1–100 (got %d)", p.DefaultLeverage)
	}
	qd := config.Int("EXCHANGE_QUOTE_DECIMALS", 2)
	if qd < 0 || qd > 8 {
		config.Problem("EXCHANGE_QUOTE_DECIMALS must be 0–8 (got %d)", qd)
	}
	p.QuoteDecimals = int32(qd)
	p.AllowSeedPrices = d.Cfg.IsDevelopment()
	s := &Service{
		d: d, store: &pgStore{pool: d.Pool}, params: p,
		quoteCurrency: strings.TrimSpace(config.String("EXCHANGE_QUOTE_CURRENCY", "USD")),
		simEnabled:    config.Bool("EXCHANGE_SIM_ENABLED", false),
		catalogFile:   config.String("EXCHANGE_CATALOG_FILE", ""),
		pricePoll:     durationEnv("EXCHANGE_PRICE_POLL_INTERVAL", 2*time.Second, 250*time.Millisecond, time.Hour),
		retention:     durationEnv("EXCHANGE_TRADE_RETENTION", 7*24*time.Hour, time.Hour, 10*365*24*time.Hour),
	}
	if s.quoteCurrency == "" || len(s.quoteCurrency) > 12 {
		config.Problem("EXCHANGE_QUOTE_CURRENCY must be 1–12 characters (got %q)", s.quoteCurrency)
	}
	if s.catalogFile != "" {
		if _, err := readCatalogFile(s.catalogFile); err != nil {
			config.Problem("EXCHANGE_CATALOG_FILE: %v", err)
		}
	}
	return s
}

func decimalEnv(key, def string, lo, hi decimal.Decimal) decimal.Decimal {
	raw := config.String(key, def)
	v, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || v.LessThan(lo) || v.GreaterThan(hi) {
		config.Problem("%s must be a decimal between %s and %s (got %q)", key, lo, hi, raw)
		return decimal.RequireFromString(def)
	}
	return v
}

func durationEnv(key string, def, lo, hi time.Duration) time.Duration {
	v := config.Duration(key, def)
	if v < lo || v > hi {
		config.Problem("%s must be between %s and %s (got %s)", key, lo, hi, v)
		return def
	}
	return v
}

func (s *Service) Name() string { return "exchange" }

// engine returns the running engine, or nil on a standby instance.
func (s *Service) engine() *engine.Engine { return s.eng.Load() }

var errStandby = httpx.NewError(503, "exchange_standby", "The exchange is starting up. Try again in a moment.")

func (s *Service) running() (*engine.Engine, error) {
	if e := s.engine(); e != nil {
		return e, nil
	}
	return nil, errStandby
}

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/exchange/config", httpx.Handler(s.configHandler))
	r.Get("/exchange/markets", httpx.Handler(s.marketsHandler))
	r.Get("/exchange/markets/{symbol}", httpx.Handler(s.marketHandler))
	r.Get("/exchange/markets/{symbol}/book", httpx.Handler(s.bookHandler))
	r.Get("/exchange/markets/{symbol}/trades", httpx.Handler(s.tradesHandler))
	r.Get("/exchange/markets/{symbol}/candles", httpx.Handler(s.candlesHandler))
	r.Get("/exchange/sparklines", httpx.Handler(s.sparklinesHandler))
}

func (s *Service) MountUser(r chi.Router) {
	write := r.With(auth.RequireActive)
	r.Get("/exchange/account", httpx.Handler(s.accountHandler))
	r.Get("/exchange/orders", httpx.Handler(s.ordersHandler))
	r.Get("/exchange/fills", httpx.Handler(s.fillsHandler))
	r.Get("/exchange/funding", httpx.Handler(s.fundingHandler))
	r.Get("/exchange/ledger", httpx.Handler(s.ledgerHandler))
	orders := write.With(auth.RateLimit(120, time.Minute))
	orders.Post("/exchange/orders", httpx.Handler(s.placeHandler))
	orders.Delete("/exchange/orders/{id}", httpx.Handler(s.cancelHandler))
	orders.Delete("/exchange/orders", httpx.Handler(s.cancelAllHandler))
	orders.Post("/exchange/positions/{symbol}/close", httpx.Handler(s.closeHandler))
	write.With(auth.RateLimit(10, time.Hour)).Post("/exchange/faucet", httpx.Handler(s.faucetHandler))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/exchange/overview", httpx.Handler(s.adminOverview))
	r.Get("/exchange/markets", httpx.Handler(s.adminMarkets))
	admin := r.With(auth.RequireActive, auth.RequireAdminRole("admin"))
	admin.Post("/exchange/markets", httpx.Handler(s.adminCreateMarket))
	admin.Put("/exchange/markets/{symbol}", httpx.Handler(s.adminUpdateMarket))
	admin.Put("/exchange/markets/{symbol}/status", httpx.Handler(s.adminSetStatus))
	admin.Put("/exchange/markets/{symbol}/price", httpx.Handler(s.adminSetPrice))
}

// AuthorizeTopic allows the markets topic and any listed market's topic to
// every signed-in user (market data is public).
func (s *Service) AuthorizeTopic(ctx context.Context, _ uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, "exchange:")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	if id == uuid.Nil {
		return true, true
	}
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exchange_markets WHERE id = $1)`, id).Scan(&exists); err != nil {
		return true, false
	}
	return true, exists
}

// ---- engine lifecycle ----

// Start campaigns for the engine (one instance runs it) and runs the
// retention job.
func (s *Service) Start(ctx context.Context) {
	go s.campaign(ctx)
	go jobs.Every(ctx, s.d.Pool, "exchange.retention", time.Hour, s.prune)
}

// campaign tries to take the engine lock every 10 s; while held, the engine
// runs on this instance.
func (s *Service) campaign(ctx context.Context) {
	for {
		if err := s.tryLead(ctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "exchange: engine stopped", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (s *Service) tryLead(ctx context.Context) error {
	conn, err := s.d.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leaderLockKey).Scan(&got); err != nil || !got {
		return err
	}
	defer func() {
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = conn.Exec(uctx, `SELECT pg_advisory_unlock($1)`, leaderLockKey)
		cancel()
	}()
	lctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() { // lose the lock connection → stop the engine
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-lctx.Done():
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(lctx, 5*time.Second)
				err := conn.Ping(pctx)
				cancel()
				if err != nil && lctx.Err() == nil {
					slog.Error("exchange: lost the engine lock connection; stopping the engine", "err", err)
					stop()
					return
				}
			}
		}
	}()
	return s.lead(lctx, s.d.Pool)
}

// lead runs the engine until ctx ends.
func (s *Service) lead(ctx context.Context, pool *pgxpool.Pool) error {
	if s.catalogFile != "" {
		list, err := readCatalogFile(s.catalogFile)
		if err != nil {
			return err
		}
		if err := UpsertCatalog(ctx, pool, list); err != nil {
			return err
		}
	}
	specs, synced, err := loadSpecs(ctx, pool)
	if err != nil {
		return err
	}
	eng := engine.New(s.params, s.store, slog.Default(), specs)
	if err := eng.Load(ctx); err != nil {
		return err
	}
	out := newFanout(s, eng)
	eng.SetPublisher(out)
	fills := make(chan engine.FillEvent, 10_000)
	eng.SetFillHook(func(f engine.FillEvent) { // inside the engine lock: never block
		select {
		case fills <- f:
		default:
			s.fillDrops.Add(1)
		}
	})
	go s.deliverFills(ctx, fills)
	setPushTarget(func(key string, p Price) { applyPrice(eng, eng.FeedKeys(), key, p) })
	s.eng.Store(eng)
	s.engineStarted(ctx)
	defer s.engineStopped()
	slog.InfoContext(ctx, "exchange: engine running", "markets", len(specs), "sim", s.simEnabled, "price_feed", currentFeed() != nil)
	defer func() {
		s.eng.Store(nil)
		setPushTarget(nil)
	}()

	go out.run(ctx)
	go s.pollPrices(ctx, eng)
	go s.refreshHistory(ctx, eng)
	if s.simEnabled {
		sm, err := sim.New(eng, slog.Default(), nil)
		if err != nil {
			return err
		}
		sm.Start(ctx)
	}
	in := &inbox{s: s, eng: eng, pool: pool, synced: synced}
	in.poll(ctx)
	eng.Run(ctx, func() { in.poll(ctx) })
	return nil
}
