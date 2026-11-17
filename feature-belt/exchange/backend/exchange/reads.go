package exchange

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange/engine"
)

// Reads and catalog writes for other modules (baskets, reputation, app
// code). They go through the database, so they work on any instance; the
// running engine picks catalog changes up within seconds.

// ListedMarket is one catalog row with its operational state.
type ListedMarket struct {
	CatalogMarket
	ID string `json:"id"`
}

// Markets returns every listed market (any status), ordered like the catalog.
func Markets(ctx context.Context, q db.Querier) ([]ListedMarket, error) {
	rows, err := q.Query(ctx, `SELECT id::text, symbol, name, category, description, image_url, price_feed, max_leverage,
		  sz_decimals, tick_size::text, min_notional::text, oi_cap::text, COALESCE(seed_price::text, ''), status, sort
		FROM exchange_markets ORDER BY sort, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ListedMarket
	for rows.Next() {
		var m ListedMarket
		if err := rows.Scan(&m.ID, &m.Symbol, &m.Name, &m.Category, &m.Description, &m.ImageURL, &m.PriceFeed, &m.MaxLeverage,
			&m.SzDecimals, &m.TickSize, &m.MinNotional, &m.OICap, &m.SeedPrice, &m.Status, &m.Sort); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMarketStatus moves a market to active, reduce_only or halted (what
// Admin → Exchange does, without the audit row: the caller audits staff
// actions itself). Unknown symbols return engine.ErrUnknownMarket.
func SetMarketStatus(ctx context.Context, q db.Querier, symbol, status string) error {
	switch status {
	case engine.MarketActive, engine.MarketReduceOnly, engine.MarketHalted:
	default:
		return fmt.Errorf("exchange: status %q is not active, reduce_only or halted", status)
	}
	tag, err := q.Exec(ctx, `UPDATE exchange_markets SET status = $2, updated_at = now() WHERE symbol = $1 AND status <> $2`,
		strings.ToUpper(strings.TrimSpace(symbol)), status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := marketIDBySymbol(ctx, q, symbol); err != nil {
			return err
		}
	}
	return nil
}

// MarketUsage is how much people (not simulated or system accounts) use a
// market; a person's sub-accounts count as that person.
type MarketUsage struct {
	// OpenInterest is the notional (|size| × entry price) of people's open positions.
	OpenInterest decimal.Decimal
	// Holders is how many people hold a position.
	Holders int
	// Volume is people's traded notional since the given time (both sides counted once per fill).
	Volume decimal.Decimal
}

// UsageOf reports people's open interest in a market and their volume since a
// time. Unknown symbols return engine.ErrUnknownMarket.
func UsageOf(ctx context.Context, q db.Querier, symbol string, since time.Time) (MarketUsage, error) {
	var u MarketUsage
	var oi, vol string
	var exists bool
	err := q.QueryRow(ctx, `
		WITH m AS (SELECT id FROM exchange_markets WHERE symbol = $1)
		SELECT
		  COALESCE((SELECT sum(abs(p.sz) * p.entry_px) FROM exchange_positions p JOIN exchange_accounts a ON a.id = p.account_id
		    WHERE p.market_id = (SELECT id FROM m) AND p.sz <> 0 AND a.kind IN ('user', 'sub')), 0)::text,
		  (SELECT count(DISTINCT COALESCE(a.user_id, a.owner_id, a.id)) FROM exchange_positions p JOIN exchange_accounts a ON a.id = p.account_id
		    WHERE p.market_id = (SELECT id FROM m) AND p.sz <> 0 AND a.kind IN ('user', 'sub')),
		  COALESCE((SELECT sum(f.px * f.sz) FROM exchange_fills f
		    WHERE f.market_id = (SELECT id FROM m) AND f.created_at >= $2), 0)::text,
		  EXISTS (SELECT 1 FROM m)`,
		strings.ToUpper(strings.TrimSpace(symbol)), since).Scan(&oi, &u.Holders, &vol, &exists)
	if err != nil {
		return u, err
	}
	if !exists {
		return u, engine.ErrUnknownMarket
	}
	u.OpenInterest, u.Volume = dec(oi), dec(vol)
	return u, nil
}

// Activity is one person's cumulative trading on the exchange.
type Activity struct {
	UserID       uuid.UUID
	Fills        int
	Volume       decimal.Decimal // traded notional
	Liquidations int
	FirstFill    time.Time // zero when the person never traded
}

// ActivityTotals returns every person's cumulative fills, volume and
// liquidations (accounts with a user only). Pair it with Scores for account
// value and net deposits (a reputation source, for example).
func ActivityTotals(ctx context.Context, q db.Querier) (map[uuid.UUID]Activity, error) {
	rows, err := q.Query(ctx, `
		SELECT a.user_id,
		  (SELECT count(*) FROM exchange_fills f WHERE f.account_id = a.id),
		  COALESCE((SELECT sum(f.px * f.sz) FROM exchange_fills f WHERE f.account_id = a.id), 0)::text,
		  (SELECT count(*) FROM exchange_liquidations l WHERE l.account_id = a.id),
		  (SELECT min(f.created_at) FROM exchange_fills f WHERE f.account_id = a.id)
		FROM exchange_accounts a
		WHERE a.kind = 'user' AND a.user_id IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]Activity{}
	for rows.Next() {
		var a Activity
		var vol string
		var first *time.Time
		if err := rows.Scan(&a.UserID, &a.Fills, &vol, &a.Liquidations, &first); err != nil {
			return nil, err
		}
		a.Volume = dec(vol)
		if first != nil {
			a.FirstFill = *first
		}
		out[a.UserID] = a
	}
	return out, rows.Err()
}
