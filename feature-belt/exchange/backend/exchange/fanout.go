package exchange

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/realtime"
)

// fanout is the engine's Publisher. The engine calls it inside its lock, so
// it only records what changed; run publishes to the realtime broker from its
// own goroutine: books and account summaries coalesced to 4 a second, trades
// and account events as they come, each market's ticker every second, every
// market's stats every 2 seconds, and open accounts' summaries every 3
// seconds (their PnL moves with the mark).
type fanout struct {
	s   *Service
	eng *engine.Engine

	mu       sync.Mutex
	books    map[string]bool
	trades   map[string][]engine.Trade
	accounts map[string]bool
	events   []accountEvent
	wake     chan struct{}
}

type accountEvent struct {
	account string
	ev      engine.AccountEvent
}

func newFanout(s *Service, eng *engine.Engine) *fanout {
	return &fanout{s: s, eng: eng, books: map[string]bool{}, trades: map[string][]engine.Trade{}, accounts: map[string]bool{},
		wake: make(chan struct{}, 1)}
}

func (f *fanout) poke() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

func (f *fanout) Trades(symbol string, trades []engine.Trade) {
	f.mu.Lock()
	f.trades[symbol] = append(f.trades[symbol], trades...)
	f.mu.Unlock()
	f.poke()
}

func (f *fanout) BookChanged(symbol string) {
	f.mu.Lock()
	f.books[symbol] = true
	f.mu.Unlock()
}

func (f *fanout) AccountChanged(accountID string) {
	f.mu.Lock()
	f.accounts[accountID] = true
	f.mu.Unlock()
}

func (f *fanout) AccountEvent(accountID string, ev engine.AccountEvent) {
	f.mu.Lock()
	f.events = append(f.events, accountEvent{accountID, ev})
	f.mu.Unlock()
	f.poke()
}

func (f *fanout) run(ctx context.Context) {
	flush := time.NewTicker(250 * time.Millisecond)
	second := time.NewTicker(time.Second)
	defer flush.Stop()
	defer second.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.wake:
			f.flushEvents(ctx)
		case <-flush.C:
			f.flushEvents(ctx)
			f.flushBooks(ctx)
		case <-second.C:
			n++
			f.tickers(ctx)
			if n%2 == 0 {
				f.publish(ctx, MarketsTopic, EventTickers, map[string]any{"stats": f.eng.AllStats()})
			}
			if n%3 == 0 {
				f.mu.Lock()
				for _, id := range f.eng.ActiveUserAccounts() {
					f.accounts[id] = true
				}
				f.mu.Unlock()
			}
		}
	}
}

func (f *fanout) publish(ctx context.Context, topic, event string, data any) {
	if err := f.s.d.Broker.Publish(ctx, topic, event, data); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "exchange: publish", "event", event, "err", err)
	}
}

func (f *fanout) userTopic(accountID string) (string, bool) {
	uid := f.eng.UserOf(accountID)
	if uid == "" {
		return "", false
	}
	id, err := uuid.Parse(uid)
	if err != nil {
		return "", false
	}
	return realtime.UserTopic(id), true
}

func (f *fanout) flushEvents(ctx context.Context) {
	f.mu.Lock()
	trades, events := f.trades, f.events
	f.trades, f.events = map[string][]engine.Trade{}, nil
	f.mu.Unlock()
	for sym, ts := range trades {
		id, ok := f.eng.MarketID(sym)
		if !ok {
			continue
		}
		// newest first, like GET /trades
		out := make([]engine.Trade, len(ts))
		for i, t := range ts {
			out[len(ts)-1-i] = t
		}
		f.publish(ctx, MarketTopic(id), EventTrades, map[string]any{"symbol": sym, "trades": out})
	}
	for _, e := range events {
		if topic, ok := f.userTopic(e.account); ok {
			f.publish(ctx, topic, EventAccount, e.ev)
		}
	}
}

func (f *fanout) flushBooks(ctx context.Context) {
	f.mu.Lock()
	books, accounts := f.books, f.accounts
	f.books, f.accounts = map[string]bool{}, map[string]bool{}
	f.mu.Unlock()
	for sym := range books {
		id, ok := f.eng.MarketID(sym)
		if !ok {
			continue
		}
		if b, ok := f.eng.Book(sym, 20); ok {
			f.publish(ctx, MarketTopic(id), EventBook, b)
		}
	}
	for acct := range accounts {
		topic, ok := f.userTopic(acct)
		if !ok {
			continue
		}
		if sum, err := f.eng.Summary(acct); err == nil {
			f.publish(ctx, topic, EventSummary, sum)
		}
	}
}

func (f *fanout) tickers(ctx context.Context) {
	for _, sym := range f.eng.Symbols() {
		id, ok := f.eng.MarketID(sym)
		if !ok {
			continue
		}
		stats, ok := f.eng.Stats(sym)
		if !ok {
			continue
		}
		candles, _ := f.eng.CurrentCandles(sym)
		f.publish(ctx, MarketTopic(id), EventTicker, map[string]any{"stats": stats, "candles": candles})
	}
}
