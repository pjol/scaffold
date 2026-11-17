package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange/engine"
)

// CatalogMarket is one market as the app's catalog defines it: the
// EXCHANGE_CATALOG_FILE entries, the admin create/update body and the
// argument of UpsertCatalog. Decimals are strings.
type CatalogMarket struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	// PriceFeed is the key the price feed knows the asset by ("" = symbol).
	PriceFeed   string `json:"price_feed,omitempty"`
	MaxLeverage int    `json:"max_leverage"`
	SzDecimals  int    `json:"sz_decimals"`
	TickSize    string `json:"tick_size"`
	MinNotional string `json:"min_notional"`
	OICap       string `json:"oi_cap"`
	// SeedPrice is the reference price used before the feed reports, in development only.
	SeedPrice string `json:"seed_price,omitempty"`
	Status    string `json:"status,omitempty"`
	Sort      int    `json:"sort,omitempty"`
}

var symbolRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,23}$`)

type parsedMarket struct {
	CatalogMarket
	tick, minNotional, oiCap, seed decimal.Decimal
}

// validate checks one entry and returns its parsed decimals, or a
// user-facing message naming the problem.
func (c CatalogMarket) validate() (parsedMarket, string) {
	p := parsedMarket{CatalogMarket: c}
	p.Symbol = strings.ToUpper(strings.TrimSpace(c.Symbol))
	p.Name = strings.TrimSpace(c.Name)
	p.Category = strings.TrimSpace(c.Category)
	if p.Status == "" {
		p.Status = engine.MarketActive
	}
	switch {
	case !symbolRe.MatchString(p.Symbol):
		return p, "Symbol must be 1–24 characters of A–Z, 0–9 and '-'."
	case p.Name == "" || len(p.Name) > 120:
		return p, "Name must be 1–120 characters."
	case len(p.Category) > 40:
		return p, "Category must be at most 40 characters."
	case len(c.Description) > 2000:
		return p, "Description must be at most 2,000 characters."
	case len(c.PriceFeed) > 200:
		return p, "Price feed key must be at most 200 characters."
	case c.MaxLeverage < 1 || c.MaxLeverage > 100:
		return p, "Max leverage must be 1–100."
	case c.SzDecimals < 0 || c.SzDecimals > 8:
		return p, "Size decimals must be 0–8."
	case p.Status != engine.MarketActive && p.Status != engine.MarketReduceOnly && p.Status != engine.MarketHalted:
		return p, "Status must be active, reduce_only or halted."
	}
	if c.ImageURL != "" {
		u, err := url.Parse(c.ImageURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(c.ImageURL) > 2048 {
			return p, "Image URL must be an http(s) URL."
		}
	}
	var err error
	if p.tick, err = decimal.NewFromString(c.TickSize); err != nil || !p.tick.IsPositive() || p.tick.Exponent() < -8 {
		return p, "Tick size must be a positive decimal with at most 8 decimals."
	}
	if p.minNotional, err = decimal.NewFromString(c.MinNotional); err != nil || p.minNotional.IsNegative() {
		return p, "Minimum order must be a decimal of at least 0."
	}
	if p.oiCap, err = decimal.NewFromString(c.OICap); err != nil || !p.oiCap.IsPositive() {
		return p, "Open interest cap must be a positive decimal."
	}
	if c.SeedPrice != "" {
		if p.seed, err = decimal.NewFromString(c.SeedPrice); err != nil || !p.seed.IsPositive() {
			return p, "Seed price must be a positive decimal."
		}
	}
	return p, ""
}

func nullDec(d decimal.Decimal) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

// UpsertCatalog lists new markets and updates the parameters of existing ones
// (matched by symbol). It leaves an existing market's status and manual price
// alone: those are operational state admins own. The running engine picks the
// changes up within seconds. Use it to seed a catalog from app code.
func UpsertCatalog(ctx context.Context, q db.Querier, markets []CatalogMarket) error {
	for _, c := range markets {
		p, problem := c.validate()
		if problem != "" {
			return fmt.Errorf("exchange catalog %q: %s", c.Symbol, problem)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO exchange_markets (symbol, name, category, description, image_url, price_feed, max_leverage, sz_decimals,
			  tick_size, min_notional, oi_cap, seed_price, status, sort)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (symbol) DO UPDATE SET name = EXCLUDED.name, category = EXCLUDED.category,
			  description = EXCLUDED.description, image_url = EXCLUDED.image_url, price_feed = EXCLUDED.price_feed,
			  max_leverage = EXCLUDED.max_leverage, sz_decimals = EXCLUDED.sz_decimals, tick_size = EXCLUDED.tick_size,
			  min_notional = EXCLUDED.min_notional, oi_cap = EXCLUDED.oi_cap, seed_price = EXCLUDED.seed_price,
			  sort = EXCLUDED.sort, updated_at = now()
			WHERE (exchange_markets.name, exchange_markets.category, exchange_markets.description, exchange_markets.image_url,
			       exchange_markets.price_feed, exchange_markets.max_leverage, exchange_markets.sz_decimals,
			       exchange_markets.tick_size, exchange_markets.min_notional, exchange_markets.oi_cap,
			       exchange_markets.seed_price, exchange_markets.sort)
			  IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.category, EXCLUDED.description, EXCLUDED.image_url,
			       EXCLUDED.price_feed, EXCLUDED.max_leverage, EXCLUDED.sz_decimals, EXCLUDED.tick_size,
			       EXCLUDED.min_notional, EXCLUDED.oi_cap, EXCLUDED.seed_price, EXCLUDED.sort)`,
			p.Symbol, p.Name, p.Category, p.Description, p.ImageURL, p.PriceFeed, p.MaxLeverage, p.SzDecimals,
			p.tick.String(), p.minNotional.String(), p.oiCap.String(), nullDec(p.seed), p.Status, p.Sort); err != nil {
			return fmt.Errorf("exchange catalog %s: %w", p.Symbol, err)
		}
	}
	return nil
}

// readCatalogFile parses EXCHANGE_CATALOG_FILE (a JSON array of CatalogMarket).
func readCatalogFile(path string) ([]CatalogMarket, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []CatalogMarket
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for _, c := range out {
		if _, problem := c.validate(); problem != "" {
			return nil, fmt.Errorf("%s: %s: %s", path, c.Symbol, problem)
		}
	}
	return out, nil
}

// loadSpecs reads the catalog from the database.
func loadSpecs(ctx context.Context, q db.Querier) ([]engine.MarketSpec, time.Time, error) {
	rows, err := q.Query(ctx, `SELECT id::text, symbol, name, category, description, image_url, price_feed, max_leverage,
		  sz_decimals, tick_size::text, min_notional::text, oi_cap::text, COALESCE(seed_price, 0)::text,
		  COALESCE(manual_price, 0)::text, status, sort, updated_at
		FROM exchange_markets ORDER BY sort, symbol`)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	var out []engine.MarketSpec
	var latest time.Time
	for rows.Next() {
		var s engine.MarketSpec
		var tick, minN, oi, seed, manual string
		var updated time.Time
		if err := rows.Scan(&s.ID, &s.Symbol, &s.Name, &s.Category, &s.Description, &s.ImageURL, &s.PriceFeed, &s.MaxLeverage,
			&s.SzDecimals, &tick, &minN, &oi, &seed, &manual, &s.Status, &s.Sort, &updated); err != nil {
			return nil, time.Time{}, err
		}
		s.TickSize, s.MinNotional, s.OICap, s.SeedPrice, s.ManualPrice = dec(tick), dec(minN), dec(oi), dec(seed), dec(manual)
		if updated.After(latest) {
			latest = updated
		}
		out = append(out, s)
	}
	return out, latest, rows.Err()
}

// marketIDBySymbol looks a market up in the database (any instance).
func marketIDBySymbol(ctx context.Context, q db.Querier, symbol string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id::text FROM exchange_markets WHERE symbol = $1`, strings.ToUpper(symbol)).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", engine.ErrUnknownMarket
	}
	return id, err
}
