package exchange

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/httpx"
)

// AdminMarket is a catalog row as admins see it.
type AdminMarket struct {
	ID string `json:"id"`
	CatalogMarket
	ManualPrice *string   `json:"manual_price"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Stats is the live state (null on a standby instance).
	Stats *engine.MarketStats `json:"stats"`
}

// Overview is GET /v1/admin/exchange/overview.
type Overview struct {
	Running bool                  `json:"running"`
	Markets int                   `json:"markets"`
	Stats   *engine.PlatformStats `json:"stats"` // null on a standby instance
	Config  Config                `json:"config"`
	// PendingTransfers are collateral transfers the engine hasn't settled yet.
	PendingTransfers int `json:"pending_transfers"`
}

func (s *Service) adminOverview(w http.ResponseWriter, r *http.Request) error {
	o := Overview{Config: s.config()}
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM exchange_markets`).Scan(&o.Markets); err != nil {
		return err
	}
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM exchange_transfers WHERE status = 'pending'`).Scan(&o.PendingTransfers); err != nil {
		return err
	}
	if eng := s.engine(); eng != nil {
		st := eng.Platform()
		o.Running, o.Stats = true, &st
	}
	return okJSON(w, o)
}

func (s *Service) loadAdminMarkets(r *http.Request, symbol string) ([]AdminMarket, error) {
	q := `SELECT id::text, symbol, name, category, description, image_url, price_feed, max_leverage, sz_decimals,
		  tick_size::text, min_notional::text, oi_cap::text, COALESCE(seed_price::text, ''), status, sort,
		  manual_price::text, created_at, updated_at
		FROM exchange_markets`
	args := []any{}
	if symbol != "" {
		q += ` WHERE symbol = $1`
		args = append(args, symbol)
	}
	rows, err := s.d.Pool.Query(r.Context(), q+` ORDER BY sort, symbol`, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminMarket, error) {
		var m AdminMarket
		err := row.Scan(&m.ID, &m.Symbol, &m.Name, &m.Category, &m.Description, &m.ImageURL, &m.PriceFeed, &m.MaxLeverage,
			&m.SzDecimals, &m.TickSize, &m.MinNotional, &m.OICap, &m.SeedPrice, &m.Status, &m.Sort, &m.ManualPrice,
			&m.CreatedAt, &m.UpdatedAt)
		return m, err
	})
	if err != nil {
		return nil, err
	}
	if eng := s.engine(); eng != nil {
		for i := range out {
			if st, ok := eng.Stats(out[i].Symbol); ok {
				out[i].Stats = &st
			}
		}
	}
	return out, nil
}

func (s *Service) adminMarkets(w http.ResponseWriter, r *http.Request) error {
	out, err := s.loadAdminMarkets(r, "")
	if err != nil {
		return err
	}
	return okJSON(w, map[string]any{"items": out})
}

func (s *Service) adminMarket(w http.ResponseWriter, r *http.Request, symbol string) error {
	out, err := s.loadAdminMarkets(r, symbol)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		return httpx.NotFound("Unknown market.")
	}
	return okJSON(w, out[0])
}

func (s *Service) adminCreateMarket(w http.ResponseWriter, r *http.Request) error {
	var body CatalogMarket
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	p, problem := body.validate()
	if problem != "" {
		return httpx.NewError(http.StatusUnprocessableEntity, "invalid_market", problem)
	}
	staff := auth.MustFromContext(r.Context())
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(r.Context(), `
			INSERT INTO exchange_markets (symbol, name, category, description, image_url, price_feed, max_leverage, sz_decimals,
			  tick_size, min_notional, oi_cap, seed_price, status, sort)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id::text`,
			p.Symbol, p.Name, p.Category, p.Description, p.ImageURL, p.PriceFeed, p.MaxLeverage, p.SzDecimals,
			p.tick.String(), p.minNotional.String(), p.oiCap.String(), nullDec(p.seed), p.Status, p.Sort).Scan(&id)
		if db.IsUniqueViolation(err) {
			return httpx.Conflict("A market with that symbol exists.")
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "exchange.market.create", "exchange_market", id, map[string]any{"symbol": p.Symbol})
	})
	if err != nil {
		return err
	}
	return s.adminMarket(w, r, p.Symbol)
}

// adminUpdateMarket replaces a market's parameters (the symbol can't change).
func (s *Service) adminUpdateMarket(w http.ResponseWriter, r *http.Request) error {
	symbol := symbolParam(r)
	var body CatalogMarket
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	body.Symbol = symbol
	p, problem := body.validate()
	if problem != "" {
		return httpx.NewError(http.StatusUnprocessableEntity, "invalid_market", problem)
	}
	staff := auth.MustFromContext(r.Context())
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(r.Context(), `
			UPDATE exchange_markets SET name = $2, category = $3, description = $4, image_url = $5, price_feed = $6,
			  max_leverage = $7, sz_decimals = $8, tick_size = $9, min_notional = $10, oi_cap = $11, seed_price = $12,
			  status = $13, sort = $14, updated_at = now()
			WHERE symbol = $1 RETURNING id::text`,
			symbol, p.Name, p.Category, p.Description, p.ImageURL, p.PriceFeed, p.MaxLeverage, p.SzDecimals,
			p.tick.String(), p.minNotional.String(), p.oiCap.String(), nullDec(p.seed), p.Status, p.Sort).Scan(&id)
		if db.IsNoRows(err) {
			return httpx.NotFound("Unknown market.")
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "exchange.market.update", "exchange_market", id, body)
	})
	if err != nil {
		return err
	}
	s.syncNow(r)
	return s.adminMarket(w, r, symbol)
}

func (s *Service) adminSetStatus(w http.ResponseWriter, r *http.Request) error {
	symbol := symbolParam(r)
	var body struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	switch body.Status {
	case engine.MarketActive, engine.MarketReduceOnly, engine.MarketHalted:
	default:
		return httpx.BadRequest("status must be active, reduce_only or halted.")
	}
	staff := auth.MustFromContext(r.Context())
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var id, before string
		err := tx.QueryRow(r.Context(), `
			UPDATE exchange_markets m SET status = $2, updated_at = now() FROM exchange_markets old
			WHERE m.id = old.id AND m.symbol = $1 RETURNING m.id::text, old.status`, symbol, body.Status).Scan(&id, &before)
		if db.IsNoRows(err) {
			return httpx.NotFound("Unknown market.")
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "exchange.market.status", "exchange_market", id,
			map[string]any{"symbol": symbol, "from": before, "to": body.Status})
	})
	if err != nil {
		return err
	}
	s.syncNow(r)
	return s.adminMarket(w, r, symbol)
}

// adminSetPrice sets or clears (price "") a manual index price, which
// overrides the price feed until cleared.
func (s *Service) adminSetPrice(w http.ResponseWriter, r *http.Request) error {
	symbol := symbolParam(r)
	var body struct {
		Price string `json:"price"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	var price any
	if p := strings.TrimSpace(body.Price); p != "" {
		d, err := decimal.NewFromString(p)
		if err != nil || !d.IsPositive() {
			return httpx.BadRequest("price must be a positive decimal, or empty to clear it.")
		}
		price = d.String()
	}
	staff := auth.MustFromContext(r.Context())
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(r.Context(), `UPDATE exchange_markets SET manual_price = $2, updated_at = now() WHERE symbol = $1 RETURNING id::text`,
			symbol, price).Scan(&id)
		if db.IsNoRows(err) {
			return httpx.NotFound("Unknown market.")
		}
		if err != nil {
			return err
		}
		return audit.Log(r.Context(), tx, staff.UserID, "exchange.market.price", "exchange_market", id,
			map[string]any{"symbol": symbol, "price": price})
	})
	if err != nil {
		return err
	}
	s.syncNow(r)
	return s.adminMarket(w, r, symbol)
}

// syncNow applies the catalog to the engine right away when this instance
// runs it (otherwise the leader picks the change up within seconds).
func (s *Service) syncNow(r *http.Request) {
	eng := s.engine()
	if eng == nil {
		return
	}
	if specs, _, err := loadSpecs(r.Context(), s.d.Pool); err == nil {
		eng.SyncMarkets(specs)
	}
}
