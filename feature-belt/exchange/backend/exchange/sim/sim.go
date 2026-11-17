// Package sim makes a demo exchange feel alive with real engine trades: a
// market maker quoting every market around the index price, and a dozen
// simulated traders taking liquidity with mild momentum and mean reversion.
// Everything goes through the engine like any other account, and every
// simulated account is flagged: trades show sim, stats show the simulated
// share of volume and open interest. Turned on with EXCHANGE_SIM_ENABLED.
package sim

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

type Sim struct {
	eng  *engine.Engine
	log  *slog.Logger
	rng  *rand.Rand // the market maker's goroutine
	brng *rand.Rand // the traders' goroutine
	mm   string
	bot  []*bot

	drift   map[string]float64 // per-market fair-value wander of the market maker (fraction of index)
	history map[string][]sample
}

type sample struct {
	t    time.Time
	mark float64
}

type style int

const (
	momentum style = iota
	meanRevert
	noise
)

type bot struct {
	id    string
	style style
}

// Traders is how many simulated traders run.
const Traders = 12

// mmCredit is the market maker's collateral; it quotes both sides everywhere.
var mmCredit = decimal.NewFromInt(50_000_000)

// New creates (or finds) the simulation's accounts. rng may be nil.
func New(eng *engine.Engine, log *slog.Logger, rng *rand.Rand) (*Sim, error) {
	if rng == nil {
		rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))
	}
	s := &Sim{eng: eng, log: log, rng: rng, brng: rand.New(rand.NewPCG(rng.Uint64(), rng.Uint64())),
		drift: map[string]float64{}, history: map[string][]sample{}}
	mm, err := eng.EnsureSystem(engine.KindMM, "market-maker", mmCredit)
	if err != nil {
		return nil, err
	}
	s.mm = mm
	styles := []style{momentum, meanRevert, noise}
	for i := 0; i < Traders; i++ {
		id, err := eng.EnsureSystem(engine.KindBot, fmt.Sprintf("trader-%02d", i+1), decimal.NewFromInt(1000))
		if err != nil {
			return nil, err
		}
		s.bot = append(s.bot, &bot{id: id, style: styles[i%len(styles)]})
	}
	return s, nil
}

// Start runs the market maker and traders until ctx ends.
func (s *Sim) Start(ctx context.Context) {
	for _, sym := range s.eng.Symbols() {
		s.quote(sym)
	}
	go s.runMM(ctx)
	go s.runBots(ctx)
}

// ---- market maker ----

func (s *Sim) runMM(ctx context.Context) {
	next := map[string]time.Time{}
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	signals := s.eng.QuoteSignals()
	for {
		select {
		case <-ctx.Done():
			return
		case sym := <-signals:
			s.quote(sym)
			next[sym] = time.Now().Add(jitter(s.rng, 2*time.Second, 5*time.Second))
		case now := <-t.C:
			for _, sym := range s.eng.Symbols() { // markets listed later are picked up here
				if at, ok := next[sym]; !ok || now.After(at) {
					s.quote(sym)
					next[sym] = now.Add(jitter(s.rng, 2*time.Second, 5*time.Second))
				}
			}
		}
	}
}

func jitter(r *rand.Rand, lo, hi time.Duration) time.Duration {
	return lo + time.Duration(r.Int64N(int64(hi-lo)))
}

func f(d decimal.Decimal) float64 { v, _ := d.Float64(); return v }

// quote re-posts five levels per side around index × (1 + drift − inventory skew).
// Floats only shape the simulated quotes; every price and size handed to the
// engine is a decimal rounded to the market's tick and size step.
func (s *Sim) quote(sym string) {
	info, ok := s.eng.SimInfo(s.mm, sym)
	if !ok || !info.Index.IsPositive() || info.Status == engine.MarketHalted {
		return
	}
	index := f(info.Index)
	d := s.drift[sym]
	d += s.rng.NormFloat64()*0.0012 - 0.01*d // mean-reverting walk, stationary σ ≈ 0.8%, capped ±2%
	d = math.Max(-0.02, math.Min(0.02, d))
	s.drift[sym] = d

	minN := math.Max(f(info.MinNotional), 1)
	maxInv := minN * 500 // inventory (quote currency) at which skew saturates
	inv := f(info.Pos) * index
	skew := 0.01 * math.Max(-1, math.Min(1, inv/maxInv))
	center := index * (1 + d - skew)

	spread := 0.006
	if index < 50 {
		spread = 0.012
	}
	tick := f(info.Tick)
	step := math.Max(spread/3, 0.0015)
	var quotes []engine.Quote
	lastBid, lastAsk := math.Inf(1), math.Inf(-1)
	for i := 0; i < 5; i++ {
		off := spread/2 + float64(i)*step
		bid := math.Floor(center*(1-off)/tick) * tick
		ask := math.Ceil(center*(1+off)/tick) * tick
		if bid >= lastBid {
			bid = lastBid - tick
		}
		if ask <= lastAsk {
			ask = lastAsk + tick
		}
		lastBid, lastAsk = bid, ask
		notional := minN * (6 + s.rng.Float64()*30) * (1 + float64(i)*0.9)
		notional = math.Min(notional, minN*200)
		mk := func(side string, px float64, n float64) {
			if px <= 0 {
				return
			}
			pxd := decimal.NewFromFloat(px).Div(info.Tick).Round(0).Mul(info.Tick)
			if !pxd.IsPositive() {
				return
			}
			sz := decimal.NewFromFloat(n / px).Truncate(info.SzDec)
			if sz.Mul(pxd).LessThan(info.MinNotional) {
				sz = info.MinNotional.Div(pxd).Truncate(info.SzDec).Add(decimal.New(1, -info.SzDec))
			}
			quotes = append(quotes, engine.Quote{Side: side, Px: pxd, Sz: sz})
		}
		bidN, askN := notional, notional
		if inv > maxInv { // long too much: lean on asks
			bidN *= 0.3
		} else if inv < -maxInv {
			askN *= 0.3
		}
		mk(engine.Buy, bid, bidN)
		mk(engine.Sell, ask, askN)
	}
	if err := s.eng.ReplaceQuotes(s.mm, sym, quotes, info.MaxLeverage); err != nil {
		s.log.Warn("exchange sim: quote failed", "symbol", sym, "err", err)
	}
}

// ---- traders ----

func (s *Sim) runBots(ctx context.Context) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.recordMarks()
			if syms := s.eng.Symbols(); len(syms) > 0 {
				s.act(s.bot[s.brng.IntN(len(s.bot))], syms[s.brng.IntN(len(syms))])
			}
			timer.Reset(jitter(s.brng, 600*time.Millisecond, 2200*time.Millisecond))
		}
	}
}

func (s *Sim) recordMarks() {
	now := time.Now()
	for sym, m := range s.eng.Marks() {
		h := s.history[sym]
		if n := len(h); n > 0 && now.Sub(h[n-1].t) < 5*time.Second {
			continue
		}
		h = append(h, sample{now, f(m)})
		if len(h) > 60 {
			h = h[len(h)-60:]
		}
		s.history[sym] = h
	}
}

// trend is the mark change over roughly the last 90 s as a fraction.
func (s *Sim) trend(sym string) float64 {
	h := s.history[sym]
	if len(h) < 3 {
		return 0
	}
	cut := time.Now().Add(-90 * time.Second)
	i := 0
	for i < len(h)-1 && h[i].t.Before(cut) {
		i++
	}
	if h[i].mark == 0 {
		return 0
	}
	return (h[len(h)-1].mark - h[i].mark) / h[i].mark
}

// takerSlippageBps caps simulated market orders at 0.6% from mark.
const takerSlippageBps = 60

func (s *Sim) act(b *bot, sym string) {
	for _, o := range s.eng.OpenOrders(b.id) { // tidy: cancel resting orders older than 90 s
		if time.Since(time.UnixMilli(o.CreatedMs)) > 90*time.Second {
			_, _ = s.eng.Cancel(b.id, o.ID)
		}
	}
	info, ok := s.eng.SimInfo(b.id, sym)
	if !ok || !info.Mark.IsPositive() || info.IndexStatus == engine.IndexStale || info.Status != engine.MarketActive {
		return
	}
	minN := math.Max(f(info.MinNotional), 1)
	if f(info.AV) < minN*25 {
		_ = s.eng.Faucet(b.id, true)
		return
	}
	mark := f(info.Mark)
	posN := f(info.Pos) * mark
	bound := minN * 120

	var dir float64
	switch b.style {
	case momentum:
		t := s.trend(sym)
		dir = sign(t)
		if math.Abs(t) < 0.0005 || s.brng.Float64() < 0.25 {
			dir = sign(s.brng.Float64() - 0.5)
		}
	case meanRevert:
		dev := (mark - f(info.Index)) / f(info.Index)
		dir = -sign(dev)
		if math.Abs(dev) < 0.003 || s.brng.Float64() < 0.3 {
			dir = sign(s.brng.Float64() - 0.5)
		}
	default:
		dir = sign(s.brng.Float64() - 0.5)
	}
	reduce := false
	if math.Abs(posN) > bound || (math.Abs(posN) > bound/2 && s.brng.Float64() < 0.3) {
		dir = -sign(posN)
		reduce = true
	}
	side := engine.Buy
	if dir < 0 {
		side = engine.Sell
	}
	notional := minN * (2 + s.brng.Float64()*23)
	lev := 2 + s.brng.IntN(max(1, min(5, info.MaxLeverage)-1))
	lev = min(lev, info.MaxLeverage)
	req := engine.PlaceOrder{Symbol: sym, Side: side, Type: engine.TypeMarket, SzQuote: decimal.NewFromFloat(notional).StringFixed(2),
		Leverage: lev, ReduceOnly: reduce, SlippageBps: takerSlippageBps}
	if !reduce && s.brng.Float64() < 0.2 {
		off := 0.001 + s.brng.Float64()*0.005
		px := mark * (1 - off)
		if side == engine.Sell {
			px = mark * (1 + off)
		}
		req.Type = engine.TypeLimit
		req.Px = decimal.NewFromFloat(px).String()
		req.SlippageBps = 0
	}
	_, _, err := s.eng.PlaceOrder(b.id, req)
	var ee *engine.Error
	if errors.As(err, &ee) && ee.Code == "insufficient_margin" && info.Pos.Sign() != 0 {
		closeSide := engine.Sell
		if info.Pos.IsNegative() {
			closeSide = engine.Buy
		}
		_, _, _ = s.eng.PlaceOrder(b.id, engine.PlaceOrder{Symbol: sym, Side: closeSide, Type: engine.TypeMarket,
			Sz: info.Pos.Abs().String(), Leverage: lev, ReduceOnly: true, SlippageBps: takerSlippageBps})
	}
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}
