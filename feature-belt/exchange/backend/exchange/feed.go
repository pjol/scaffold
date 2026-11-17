package exchange

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

// Price is one index price observation from a price feed.
type Price struct {
	Value decimal.Decimal
	At    time.Time // when the feed observed it (zero = now)
}

// PriceFeed supplies index prices. The app, or the oracle feature through
// stitching, installs one with SetPriceFeed; the engine polls it every
// EXCHANGE_PRICE_POLL_INTERVAL for every market's feed key (the market's
// price_feed, or its symbol). Keys the feed doesn't know are left out of the
// result. A feed that can push changes as they happen calls PushPrice too.
//
// Without a feed, markets trade on admin-set manual prices, and in
// development on their catalog seed prices; otherwise orders are refused with
// price_unavailable.
type PriceFeed interface {
	Prices(ctx context.Context, keys []string) (map[string]Price, error)
}

// PriceFeedFunc adapts a function to PriceFeed.
type PriceFeedFunc func(ctx context.Context, keys []string) (map[string]Price, error)

func (f PriceFeedFunc) Prices(ctx context.Context, keys []string) (map[string]Price, error) {
	return f(ctx, keys)
}

// DailyBar is one day of the asset's price history, before the market listed.
type DailyBar struct {
	Day        time.Time // UTC midnight
	O, H, L, C decimal.Decimal
}

// PriceHistory is optionally implemented by a PriceFeed that knows daily
// history: the exchange stores it once a day per market and shows it on the
// 1d chart before the market's first live candle (marked backfill).
type PriceHistory interface {
	DailyHistory(ctx context.Context, key string) ([]DailyBar, error)
}

var feedMu sync.RWMutex
var installedFeed PriceFeed
var pushTarget func(key string, p Price)

// SetPriceFeed installs the price feed (call from stitching code, in an init
// or before the server starts). Passing nil removes it.
func SetPriceFeed(f PriceFeed) {
	feedMu.Lock()
	installedFeed = f
	feedMu.Unlock()
}

func currentFeed() PriceFeed {
	feedMu.RLock()
	defer feedMu.RUnlock()
	return installedFeed
}

// PushPrice delivers a price for a feed key as soon as the feed has it. It
// never blocks; it is a no-op on instances that aren't running the engine
// (the poll picks the price up there).
func PushPrice(key string, p Price) {
	feedMu.RLock()
	f := pushTarget
	feedMu.RUnlock()
	if f != nil {
		f(key, p)
	}
}

func setPushTarget(f func(key string, p Price)) {
	feedMu.Lock()
	pushTarget = f
	feedMu.Unlock()
}

// applyPrice queues a feed price for every market that reads key.
func applyPrice(eng *engine.Engine, keys map[string][]string, key string, p Price) {
	if !p.Value.IsPositive() {
		return
	}
	at := p.At
	if at.IsZero() {
		at = time.Now()
	}
	for _, sym := range keys[key] {
		eng.OnIndex(engine.IndexUpdate{Symbol: sym, Price: p.Value, Status: engine.IndexLive, Ms: at.UnixMilli()})
	}
}

// pollPrices asks the feed for every market's price on an interval.
func (s *Service) pollPrices(ctx context.Context, eng *engine.Engine) {
	t := time.NewTicker(s.pricePoll)
	defer t.Stop()
	for {
		feed := currentFeed()
		if feed != nil {
			keys := eng.FeedKeys()
			list := make([]string, 0, len(keys))
			for k := range keys {
				list = append(list, k)
			}
			pctx, cancel := context.WithTimeout(ctx, max(s.pricePoll, 5*time.Second))
			prices, err := feed.Prices(pctx, list)
			cancel()
			if err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "exchange: price feed failed", "err", err)
			}
			for k, p := range prices {
				applyPrice(eng, keys, k, p)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// refreshHistory stores the feed's daily history for every market, once a
// day, when the feed implements PriceHistory.
func (s *Service) refreshHistory(ctx context.Context, eng *engine.Engine) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		if h, ok := currentFeed().(PriceHistory); ok {
			for _, sym := range eng.Symbols() {
				spec, ok := eng.Market(sym)
				if !ok {
					continue
				}
				key := sym
				if k := s.feedKey(ctx, spec.ID); k != "" {
					key = k
				}
				hctx, cancel := context.WithTimeout(ctx, time.Minute)
				bars, err := h.DailyHistory(hctx, key)
				if err == nil {
					err = s.store.ReplaceBackfill(hctx, spec.ID, cleanBars(bars))
				}
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.WarnContext(ctx, "exchange: price history failed", "symbol", sym, "err", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) feedKey(ctx context.Context, marketID string) string {
	var k string
	_ = s.d.Pool.QueryRow(ctx, `SELECT price_feed FROM exchange_markets WHERE id = $1`, marketID).Scan(&k)
	return k
}

// cleanBars drops invalid bars, floors days to UTC midnight and keeps the last
// bar of a duplicated day.
func cleanBars(bars []DailyBar) []DailyBar {
	byDay := map[int64]int{}
	out := make([]DailyBar, 0, len(bars))
	for _, b := range bars {
		if !b.O.IsPositive() || !b.H.IsPositive() || !b.L.IsPositive() || !b.C.IsPositive() {
			continue
		}
		b.Day = b.Day.UTC().Truncate(24 * time.Hour)
		b.H = decimal.Max(b.H, b.O, b.C)
		b.L = decimal.Min(b.L, b.O, b.C)
		k := b.Day.UnixMilli()
		if i, dup := byDay[k]; dup {
			out[i] = b
			continue
		}
		byDay[k] = len(out)
		out = append(out, b)
	}
	return out
}
