package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// The catalog maps stable app keys ("pro_monthly") to provider prices, so
// code and clients never carry provider ids. Admins manage it; development
// seeds it from PAYMENTS_PRICES. Amount, currency and interval are read from
// the provider when an entry is saved.

// CatalogPrice is a price as clients see it.
type CatalogPrice struct {
	Key             string  `json:"key"`
	Label           string  `json:"label"`
	Kind            string  `json:"kind"` // one_time | recurring
	AmountMinor     *int64  `json:"amount_minor"`
	Currency        string  `json:"currency"`
	Interval        *string `json:"interval"`
	IntervalCount   *int    `json:"interval_count"`
	Entitlement     *string `json:"entitlement"`
	EntitlementDays *int    `json:"entitlement_days"`
}

// AdminPrice is a catalog entry as staff see it.
type AdminPrice struct {
	CatalogPrice
	ID                uuid.UUID  `json:"id"`
	Provider          string     `json:"provider"`
	ProviderPriceID   string     `json:"provider_price_id"`
	DestinationUserID *uuid.UUID `json:"destination_user_id"`
	Active            bool       `json:"active"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

const priceCols = `key, label, kind, amount_minor, currency, billing_interval, interval_count, entitlement, entitlement_days,
	id, provider, provider_price_id, destination_user_id, active, created_at, updated_at`

func scanPrice(row pgx.Row) (AdminPrice, error) {
	var p AdminPrice
	err := row.Scan(&p.Key, &p.Label, &p.Kind, &p.AmountMinor, &p.Currency, &p.Interval, &p.IntervalCount,
		&p.Entitlement, &p.EntitlementDays, &p.ID, &p.Provider, &p.ProviderPriceID, &p.DestinationUserID,
		&p.Active, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func queryPrices(ctx context.Context, q db.Querier, where string, args ...any) ([]AdminPrice, error) {
	rows, err := q.Query(ctx, `SELECT `+priceCols+` FROM payment_prices `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminPrice{}
	for rows.Next() {
		p, err := scanPrice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// loadPrice returns an active catalog entry for the current provider.
func (s *Service) loadPrice(ctx context.Context, q db.Querier, key string) (*AdminPrice, error) {
	p, err := scanPrice(q.QueryRow(ctx, `SELECT `+priceCols+` FROM payment_prices
		WHERE key = $1 AND active AND provider = $2`, key, s.gw.Name()))
	if db.IsNoRows(err) {
		return nil, errPriceUnavailable
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// priceByProviderID finds the catalog entry (active or not) for a provider
// price, so a plan changed in the billing portal still maps to its key.
func priceByProviderID(ctx context.Context, q db.Querier, provider, providerPriceID string) (*AdminPrice, error) {
	p, err := scanPrice(q.QueryRow(ctx, `SELECT `+priceCols+` FROM payment_prices
		WHERE provider = $1 AND provider_price_id = $2 ORDER BY active DESC, updated_at DESC LIMIT 1`,
		provider, providerPriceID))
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func priceByKey(ctx context.Context, q db.Querier, key string) (*AdminPrice, error) {
	p, err := scanPrice(q.QueryRow(ctx, `SELECT `+priceCols+` FROM payment_prices WHERE key = $1`, key))
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func pricesGranting(ctx context.Context, q db.Querier, entitlement string) ([]CatalogPrice, error) {
	list, err := queryPrices(ctx, q, `WHERE active AND entitlement = $1 ORDER BY kind, amount_minor NULLS LAST, key`, entitlement)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogPrice, 0, len(list))
	for _, p := range list {
		out = append(out, p.CatalogPrice)
	}
	return out, nil
}

// GET /v1/payments/prices[?entitlement=pro]: the active catalog.
func (s *Service) listPrices(w http.ResponseWriter, r *http.Request) error {
	ent := r.URL.Query().Get("entitlement")
	list, err := queryPrices(r.Context(), s.d.Pool, `WHERE active AND provider = $1 AND ($2 = '' OR entitlement = $2)
		ORDER BY kind, amount_minor NULLS LAST, key`, s.gw.Name(), ent)
	if err != nil {
		return err
	}
	out := make([]CatalogPrice, 0, len(list))
	for _, p := range list {
		out = append(out, p.CatalogPrice)
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// ---- admin -----------------------------------------------------------------

func (s *Service) adminListPrices(w http.ResponseWriter, r *http.Request) error {
	list, err := queryPrices(r.Context(), s.d.Pool, `ORDER BY active DESC, key`)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type priceReq struct {
	Key               *string `json:"key"`
	ProviderPriceID   *string `json:"provider_price_id"`
	Label             *string `json:"label"`
	Entitlement       *string `json:"entitlement"`         // "" clears
	EntitlementDays   *int    `json:"entitlement_days"`    // 0 clears
	DestinationUserID *string `json:"destination_user_id"` // "" clears
	Active            *bool   `json:"active"`
}

// fetchProviderPrice reads and validates a provider price for the catalog.
func (s *Service) fetchProviderPrice(ctx context.Context, id string) (*Price, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 255 {
		return nil, httpx.BadRequest("Enter the provider's price id.")
	}
	p, err := s.gw.GetPrice(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, httpx.BadRequest("The provider has no price with that id.")
	}
	if err != nil {
		return nil, providerFailure(ctx, "get_price", err)
	}
	if !p.Active {
		return nil, httpx.BadRequest("That price is archived at the provider. Use an active price.")
	}
	if p.Metered {
		return nil, httpx.BadRequest("Metered prices aren't supported by checkout here.")
	}
	return p, nil
}

func (s *Service) validateDestination(ctx context.Context, raw *string) (*uuid.UUID, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	if !s.cfg.connect {
		return nil, httpx.BadRequest("Turn on PAYMENTS_CONNECT_ENABLED to route a price to a connected account.")
	}
	id, err := uuid.Parse(*raw)
	if err != nil {
		return nil, httpx.BadRequest("Invalid destination_user_id.")
	}
	var ok bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM payment_connected_accounts
		WHERE user_id = $1 AND provider = $2)`, id, s.gw.Name()).Scan(&ok); err != nil {
		return nil, err
	}
	if !ok {
		return nil, httpx.BadRequest("That user has no connected account yet.")
	}
	return &id, nil
}

func validateEntitlement(kind string, ent *string, days *int) error {
	if ent != nil && *ent != "" && !keyPattern.MatchString(*ent) {
		return httpx.BadRequest("Entitlement must be 2 to 63 lowercase letters, digits or underscores, starting with a letter.")
	}
	if days != nil && *days != 0 {
		if *days < 0 || *days > 36500 {
			return httpx.BadRequest("Entitlement days must be between 1 and 36500.")
		}
		if kind != "one_time" {
			return httpx.BadRequest("Entitlement days apply to one-time prices only; subscriptions last while they are paid.")
		}
	}
	return nil
}

func nonEmpty(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}

func nonZero(p *int) *int {
	if p == nil || *p == 0 {
		return nil
	}
	return p
}

func intervalOf(p *Price) (*string, *int) {
	if p.Kind != "recurring" || p.Interval == "" {
		return nil, nil
	}
	iv, n := p.Interval, p.IntervalCount
	return &iv, &n
}

func (s *Service) adminCreatePrice(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	var req priceReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Key == nil || !keyPattern.MatchString(*req.Key) {
		return httpx.BadRequest("Key must be 2 to 63 lowercase letters, digits or underscores, starting with a letter.")
	}
	if req.ProviderPriceID == nil {
		return httpx.BadRequest("Enter the provider's price id.")
	}
	pp, err := s.fetchProviderPrice(r.Context(), *req.ProviderPriceID)
	if err != nil {
		return err
	}
	if err := validateEntitlement(pp.Kind, req.Entitlement, req.EntitlementDays); err != nil {
		return err
	}
	dest, err := s.validateDestination(r.Context(), req.DestinationUserID)
	if err != nil {
		return err
	}
	label := pp.ProductName
	if req.Label != nil {
		label = strings.TrimSpace(*req.Label)
	}
	if len([]rune(label)) > 80 {
		return httpx.BadRequest("Label must be 80 characters or fewer.")
	}
	active := req.Active == nil || *req.Active
	iv, ivn := intervalOf(pp)
	var out AdminPrice
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		p, err := scanPrice(tx.QueryRow(r.Context(), `
			INSERT INTO payment_prices (key, provider, provider_price_id, label, kind, amount_minor, currency,
			  billing_interval, interval_count, entitlement, entitlement_days, destination_user_id, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING `+priceCols,
			*req.Key, s.gw.Name(), pp.ID, label, pp.Kind, pp.UnitAmount, pp.Currency, iv, ivn,
			nonEmpty(req.Entitlement), nonZero(req.EntitlementDays), dest, active))
		if db.IsUniqueViolation(err) {
			return httpx.Conflict("A price with that key already exists.")
		}
		if err != nil {
			return err
		}
		out = p
		return audit.Log(r.Context(), tx, staff.UserID, "payments.price.create", "payment_price", p.Key,
			map[string]any{"provider_price_id": p.ProviderPriceID, "entitlement": p.Entitlement, "active": p.Active})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) adminUpdatePrice(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	key := chi.URLParam(r, "key")
	var req priceReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Key != nil && *req.Key != key {
		return httpx.BadRequest("Keys can't be renamed; code refers to them. Create a new price instead.")
	}
	cur, err := priceByKey(r.Context(), s.d.Pool, key)
	if err != nil {
		return err
	}
	if cur == nil {
		return httpx.ErrNotFound
	}
	kind, providerID, amount, currency := cur.Kind, cur.ProviderPriceID, cur.AmountMinor, cur.Currency
	iv, ivn := cur.Interval, cur.IntervalCount
	if req.ProviderPriceID != nil && strings.TrimSpace(*req.ProviderPriceID) != cur.ProviderPriceID {
		pp, err := s.fetchProviderPrice(r.Context(), *req.ProviderPriceID)
		if err != nil {
			return err
		}
		if pp.Kind != cur.Kind {
			return httpx.BadRequest("The new price must be the same kind (one-time or recurring) as the old one.")
		}
		kind, providerID, amount, currency = pp.Kind, pp.ID, pp.UnitAmount, pp.Currency
		iv, ivn = intervalOf(pp)
	}
	ent, days := cur.Entitlement, cur.EntitlementDays
	if req.Entitlement != nil {
		ent = nonEmpty(req.Entitlement)
	}
	if req.EntitlementDays != nil {
		days = nonZero(req.EntitlementDays)
	}
	if err := validateEntitlement(kind, ent, days); err != nil {
		return err
	}
	dest := cur.DestinationUserID
	if req.DestinationUserID != nil {
		if dest, err = s.validateDestination(r.Context(), req.DestinationUserID); err != nil {
			return err
		}
	}
	label := cur.Label
	if req.Label != nil {
		label = strings.TrimSpace(*req.Label)
		if len([]rune(label)) > 80 {
			return httpx.BadRequest("Label must be 80 characters or fewer.")
		}
	}
	active := cur.Active
	if req.Active != nil {
		active = *req.Active
	}
	var out AdminPrice
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		p, err := scanPrice(tx.QueryRow(r.Context(), `
			UPDATE payment_prices SET provider_price_id = $2, label = $3, amount_minor = $4, currency = $5,
			  billing_interval = $6, interval_count = $7, entitlement = $8, entitlement_days = $9,
			  destination_user_id = $10, active = $11, updated_at = now()
			WHERE key = $1 RETURNING `+priceCols,
			key, providerID, label, amount, currency, iv, ivn, ent, days, dest, active))
		if err != nil {
			return err
		}
		out = p
		return audit.Log(r.Context(), tx, staff.UserID, "payments.price.update", "payment_price", key, req)
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) adminDeletePrice(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	key := chi.URLParam(r, "key")
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var used bool
		if err := tx.QueryRow(r.Context(), `SELECT
			EXISTS (SELECT 1 FROM payment_checkout_sessions WHERE price_key = $1) OR
			EXISTS (SELECT 1 FROM payment_subscriptions WHERE price_key = $1)`, key).Scan(&used); err != nil {
			return err
		}
		if used {
			return httpx.Conflict("This price has purchases. Deactivate it instead.")
		}
		tag, err := tx.Exec(r.Context(), `DELETE FROM payment_prices WHERE key = $1`, key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.ErrNotFound
		}
		return audit.Log(r.Context(), tx, staff.UserID, "payments.price.delete", "payment_price", key, nil)
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- development seed --------------------------------------------------------

// seedEntry is one PAYMENTS_PRICES item: key=price_id[:entitlement].
type seedEntry struct {
	Key, PriceID, Entitlement string
}

// parseSeedPrices parses "pro_monthly=price_123:pro,lifetime=price_456".
func parseSeedPrices(raw string) ([]seedEntry, error) {
	var out []seedEntry
	for item := range strings.SplitSeq(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, rest, ok := strings.Cut(item, "=")
		key, rest = strings.TrimSpace(key), strings.TrimSpace(rest)
		if !ok || !keyPattern.MatchString(key) || rest == "" {
			return nil, fmt.Errorf("PAYMENTS_PRICES: %q must look like key=price_id or key=price_id:entitlement", item)
		}
		priceID, ent, _ := strings.Cut(rest, ":")
		if ent != "" && !keyPattern.MatchString(ent) {
			return nil, fmt.Errorf("PAYMENTS_PRICES: entitlement %q in %q is not a valid key", ent, item)
		}
		out = append(out, seedEntry{Key: key, PriceID: priceID, Entitlement: ent})
	}
	return out, nil
}

// seedCatalog upserts PAYMENTS_PRICES entries with the provider's real
// amounts. Without a provider it has nothing truthful to store and skips.
func (s *Service) seedCatalog(ctx context.Context) error {
	entries, err := parseSeedPrices(s.cfg.seedPrices)
	if err != nil || len(entries) == 0 {
		return err
	}
	if s.gw == nil {
		slog.Warn("payments: PAYMENTS_PRICES is set but STRIPE_SECRET_KEY is not; catalog not seeded")
		return nil
	}
	for _, e := range entries {
		pp, err := s.gw.GetPrice(ctx, e.PriceID)
		if err != nil {
			return fmt.Errorf("PAYMENTS_PRICES %s: %w", e.Key, err)
		}
		iv, ivn := intervalOf(pp)
		var ent *string
		if e.Entitlement != "" {
			ent = &e.Entitlement
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO payment_prices (key, provider, provider_price_id, label, kind, amount_minor, currency,
			  billing_interval, interval_count, entitlement, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (key) DO UPDATE SET provider = EXCLUDED.provider, provider_price_id = EXCLUDED.provider_price_id,
			  label = CASE WHEN payment_prices.label = '' THEN EXCLUDED.label ELSE payment_prices.label END,
			  kind = EXCLUDED.kind, amount_minor = EXCLUDED.amount_minor, currency = EXCLUDED.currency,
			  billing_interval = EXCLUDED.billing_interval, interval_count = EXCLUDED.interval_count,
			  entitlement = COALESCE(EXCLUDED.entitlement, payment_prices.entitlement),
			  active = EXCLUDED.active, updated_at = now()`,
			e.Key, s.gw.Name(), pp.ID, pp.ProductName, pp.Kind, pp.UnitAmount, pp.Currency, iv, ivn, ent, pp.Active); err != nil {
			return err
		}
	}
	slog.Info("payments: catalog seeded from PAYMENTS_PRICES", "prices", len(entries))
	return nil
}
