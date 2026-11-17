package baskets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/oracle"
)

// CreateRequest is POST /v1/baskets.
type CreateRequest struct {
	Input
	Name        string  `json:"name"`        // "" = generated from the largest lines
	Description string  `json:"description"` // optional
	EndsOn      *string `json:"ends_on"`     // optional YYYY-MM-DD: winds down after it (BASKETS_END_GRACE_DAYS)
}

// CreateResult says what create did.
type CreateResult struct {
	Basket   Detail   `json:"basket"`
	Created  bool     `json:"created"` // a new market was listed
	Revived  bool     `json:"revived"` // an archived basket with these lines reopened
	Joined   bool     `json:"joined"`  // the caller's entry was recorded now (false: they had one)
	Warnings []string `json:"warnings"`
}

// Errors clients branch on (codes are stable).
var (
	errUnresolved = httpx.NewError(http.StatusUnprocessableEntity, "unresolved_lines", "Some lines didn't match a market. Fix them or remove them.")
	errTooSmall   = httpx.NewError(http.StatusUnprocessableEntity, "basket_too_small", "This basket is too small to list.")
	errUnpriced   = httpx.NewError(http.StatusUnprocessableEntity, "basket_unpriced", "Some markets in this basket have no price yet. Try again later.")
	errTooLarge   = httpx.NewError(http.StatusUnprocessableEntity, "basket_too_large", "This basket holds too many markets.")
	errHidden     = httpx.NewError(http.StatusConflict, "basket_hidden", "Staff removed this basket. It can't be listed again.")
	errCapacity   = httpx.NewError(http.StatusServiceUnavailable, "baskets_capacity", "No new baskets can be listed right now. Try again later.")
	errEndsOn     = httpx.NewError(http.StatusBadRequest, "invalid_request", "ends_on must be a date (YYYY-MM-DD) from today to 365 days ahead.")
)

// rateLimited says when the caller may create again.
func rateLimited(n int, window string, wait time.Duration) *httpx.Error {
	e := httpx.NewError(http.StatusTooManyRequests, "baskets_rate_limited",
		fmt.Sprintf("You've created %d baskets in the last %s. Try again in %s.", n, window, waitWords(wait)))
	e.Details = map[string]any{"retry_after_seconds": max(1, int(wait.Seconds()))}
	return e
}

func waitWords(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24)+1)
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours())+1)
	case d >= time.Hour:
		return "an hour"
	}
	return fmt.Sprintf("%d minutes", max(1, int(d.Minutes())+1))
}

// defaultName: the largest lines by value ("Gold & Silver", "Gold, Silver +3").
func defaultName(ev *evaluation) string {
	items := append([]bitem(nil), ev.items...)
	val := func(it bitem) decimal.Decimal { return it.price.Mul(decimal.NewFromInt(int64(it.qty))) }
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && val(items[j]).GreaterThan(val(items[j-1])); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	var n []string
	for _, it := range items {
		n = append(n, it.name)
	}
	switch len(n) {
	case 0:
		return ""
	case 1:
		return n[0]
	case 2:
		return n[0] + " & " + n[1]
	}
	return fmt.Sprintf("%s, %s +%d", n[0], n[1], len(n)-2)
}

// parseEndsOn checks an optional end date: today to 365 days ahead (UTC).
func parseEndsOn(v *string, now time.Time) (*time.Time, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}
	day, err := time.Parse(time.DateOnly, strings.TrimSpace(*v))
	today := now.UTC().Truncate(24 * time.Hour)
	if err != nil || day.Before(today) || day.After(today.AddDate(0, 0, 365)) {
		return nil, errEndsOn
	}
	return &day, nil
}

// limits checks the caller's creations in the last 24 hours and 30 days.
func (s *Service) limits(ctx context.Context, q db.Querier, user uuid.UUID, now time.Time) error {
	rows, err := q.Query(ctx, `SELECT created_at FROM baskets WHERE created_by = $1 AND created_at > $2 ORDER BY created_at DESC`,
		user, now.Add(-30*24*time.Hour))
	if err != nil {
		return err
	}
	var times []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		times = append(times, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return checkLimits(times, now, s.cfg.perDay, s.cfg.perMonth)
}

// checkLimits: times are the caller's creations in the last 30 days, newest first.
func checkLimits(times []time.Time, now time.Time, perDay, perMonth int) error {
	day := 0
	for _, t := range times {
		if now.Sub(t) < 24*time.Hour {
			day++
		}
	}
	if day >= perDay {
		// the perDay-th newest leaves the window first
		return rateLimited(perDay, "24 hours", times[perDay-1].Add(24*time.Hour).Sub(now))
	}
	if len(times) >= perMonth {
		return rateLimited(perMonth, "30 days", times[perMonth-1].Add(30*24*time.Hour).Sub(now))
	}
	return nil
}

func (s *Service) capacityLeft(ctx context.Context, q db.Querier) error {
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM baskets WHERE status <> 'archived'`).Scan(&n); err != nil {
		return err
	}
	if n >= s.cfg.capacity {
		return errCapacity
	}
	return nil
}

// marketFor is the exchange catalog entry of a basket.
func (s *Service) marketFor(sym, name, description string, ev *evaluation) exchange.CatalogMarket {
	sp := s.cfg.SpecFor(ev.value, ev.maxShare)
	var parts []string
	for _, it := range ev.items {
		parts = append(parts, fmt.Sprintf("%d %s", it.qty, it.name))
	}
	desc := "Holds " + strings.Join(parts, ", ") + "."
	if description != "" {
		desc = description + "\n\n" + desc
	}
	if len(desc) > 2000 {
		desc = desc[:1997] + "…"
	}
	return exchange.CatalogMarket{Symbol: sym, Name: name, Category: category, Description: desc, PriceFeed: sym,
		MaxLeverage: sp.MaxLeverage, SzDecimals: sp.SzDecimals, TickSize: sp.TickSize, MinNotional: sp.MinNotional, OICap: sp.OICap,
		SeedPrice: ev.value.Round(feedDecimals).String(), Sort: 1000}
}

func basketFeed(sym, name string, items []bitem) oracle.BasketFeed {
	parts := make([]oracle.BasketPart, len(items))
	for i, it := range items {
		parts[i] = oracle.BasketPart{Key: it.feed, Quantity: float64(it.qty)}
	}
	return oracle.BasketFeed{Key: sym, Name: name, Category: category, Decimals: feedDecimals, Parts: parts}
}

// setStatus moves a basket and its market (and feed) in q.
func (s *Service) setStatus(ctx context.Context, q db.Querier, b basketRow, to string) error {
	o := s.oracle()
	switch to {
	case StatusActive:
		if err := exchange.SetMarketStatus(ctx, q, b.Symbol, engine.MarketActive); err != nil {
			return err
		}
		if err := o.SetFeedEnabled(ctx, q, b.Symbol, true); err != nil {
			return err
		}
	case StatusReduceOnly:
		if err := exchange.SetMarketStatus(ctx, q, b.Symbol, engine.MarketReduceOnly); err != nil {
			return err
		}
	case StatusArchived:
		if err := exchange.SetMarketStatus(ctx, q, b.Symbol, engine.MarketHalted); err != nil {
			return err
		}
		if err := o.SetFeedEnabled(ctx, q, b.Symbol, false); err != nil {
			return err
		}
	}
	_, err := q.Exec(ctx, `UPDATE baskets SET status = $2, status_at = now(), updated_at = now() WHERE id = $1`, b.ID, to)
	return err
}

// addEntry records the person's entry once; it reports whether it was new.
func addEntry(ctx context.Context, q db.Querier, basketID, user uuid.UUID, source string) (bool, error) {
	tag, err := q.Exec(ctx, `INSERT INTO basket_entries (basket_id, user_id, source) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, basketID, user, source)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	_, err = q.Exec(ctx, `UPDATE baskets SET entries = entries + 1, popularity = popularity + $2, updated_at = now() WHERE id = $1`,
		basketID, entryWeight)
	return true, err
}

// Create lists the basket an input describes, or reuses the one with the same
// lines (reviving it when archived), and records the caller's entry. Every
// write (basket, lines, entry, oracle feed, exchange market, status) is one
// transaction.
func (s *Service) Create(ctx context.Context, user uuid.UUID, source string, req CreateRequest) (CreateResult, error) {
	ev, err := s.evaluate(ctx, req.Input)
	if err != nil {
		return CreateResult{}, err
	}
	switch n := len(ev.items); {
	case ev.unresolved > 0:
		return CreateResult{}, errUnresolved
	case n < s.cfg.minLines:
		return CreateResult{}, httpx.NewError(errTooSmall.Status, errTooSmall.Code, fmt.Sprintf("A basket needs at least %d different markets.", s.cfg.minLines))
	case n > s.cfg.maxLines || len(ev.overfull) > 0:
		return CreateResult{}, errTooLarge
	}
	endsOn, err := parseEndsOn(req.EndsOn, time.Now())
	if err != nil {
		return CreateResult{}, err
	}
	desc, err := s.cleanDescription(ctx, req.Description)
	if err != nil {
		return CreateResult{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name != "" {
		if name, err = s.cleanName(ctx, name); err != nil {
			return CreateResult{}, err
		}
	}

	res, b, err := s.commit(ctx, user, source, ev, name, desc, endsOn)
	if err != nil {
		return CreateResult{}, err
	}
	if res.Basket, err = s.detail(ctx, b, user); err != nil {
		return CreateResult{}, err
	}
	return res, nil
}

// commit writes a checked evaluation: reuse (or revive) the basket with the
// same hash, or list a new one, and record the caller's entry.
func (s *Service) commit(ctx context.Context, user uuid.UUID, source string, ev *evaluation, name, desc string, endsOn *time.Time) (CreateResult, basketRow, error) {
	now := time.Now()
	res := CreateResult{Warnings: ev.warnings}
	var b basketRow
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// One creation per person at a time, one listing per basket.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('baskets.user:' || $1::text, 0))`, user); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('baskets.hash:' || $1, 0))`, ev.hash); err != nil {
			return err
		}
		ex, err := basketByLock(ctx, tx, "hash", ev.hash, true)
		switch {
		case err == nil:
			b = ex
			return s.reuse(ctx, tx, &b, user, source, &res)
		case !isNotFound(err):
			return err
		}
		if len(ev.unpriced) > 0 {
			return errUnpriced
		}
		if ev.value.LessThan(s.cfg.minValue) {
			return httpx.NewError(errTooSmall.Status, errTooSmall.Code, fmt.Sprintf("A basket must be worth at least %s. This one is worth %s.",
				s.cfg.minValue.StringFixed(2), ev.value.StringFixed(2)))
		}
		if err := s.limits(ctx, tx, user, now); err != nil {
			return err
		}
		if err := s.capacityLeft(ctx, tx); err != nil {
			return err
		}
		taken, err := takenSymbols(ctx, tx)
		if err != nil {
			return err
		}
		sym, err := s.chooseSymbol(ctx, tx, ev.hash, taken)
		if err != nil {
			return err
		}
		if name == "" {
			if name, err = s.cleanName(ctx, defaultName(ev)); err != nil {
				name = "Basket " + sym
			}
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO baskets (symbol, hash, canonical, name, description, created_by, source, value_at_create,
 max_share, ends_on, entries, popularity) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11) RETURNING id`,
			sym, ev.hash, Canonical(ev.canonical()), name, desc, user, source, ev.value.String(), ev.maxShare.Round(6).String(), endsOn, entryWeight).Scan(&id); err != nil {
			return err
		}
		for _, it := range ev.items {
			if _, err := tx.Exec(ctx, `INSERT INTO basket_lines (basket_id, market, feed, name, quantity, price_at_create) VALUES ($1,$2,$3,$4,$5,$6)`,
				id, it.symbol, it.feed, it.name, it.qty, it.price.String()); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO basket_entries (basket_id, user_id, source) VALUES ($1, $2, $3)`, id, user, source); err != nil {
			return err
		}
		if err := s.oracle().EnsureBasketFeed(ctx, tx, basketFeed(sym, name, ev.items)); err != nil {
			return err
		}
		if err := exchange.UpsertCatalog(ctx, tx, []exchange.CatalogMarket{s.marketFor(sym, name, desc, ev)}); err != nil {
			return err
		}
		res.Created, res.Joined = true, true
		b, err = basketBy(ctx, tx, "id", id.String())
		return err
	})
	if err != nil {
		return res, b, err
	}
	if res.Created || res.Revived {
		s.markets.clear()
		s.oracle().Reload()
		s.log.Info("baskets: listed", "symbol", b.Symbol, "lines", len(ev.items), "value", ev.value.StringFixed(2), "revived", res.Revived)
	}
	s.publish(ctx, b)
	return res, b, nil
}

// reuse records the caller's entry on an existing basket, reviving it when
// archived (within capacity). Staff-hidden baskets never revive.
func (s *Service) reuse(ctx context.Context, tx pgx.Tx, b *basketRow, user uuid.UUID, source string, res *CreateResult) error {
	if b.Hidden {
		return errHidden
	}
	if b.Status == StatusArchived {
		if err := s.capacityLeft(ctx, tx); err != nil {
			return err
		}
		if err := s.setStatus(ctx, tx, *b, StatusActive); err != nil {
			return err
		}
		res.Revived = true
	}
	joined, err := addEntry(ctx, tx, b.ID, user, source)
	if err != nil {
		return err
	}
	res.Joined = joined
	fresh, err := basketBy(ctx, tx, "id", b.ID.String())
	if err != nil {
		return err
	}
	*b = fresh
	return nil
}

// publish tells followers of a basket's topic that it changed (best-effort).
func (s *Service) publish(ctx context.Context, b basketRow) {
	sum := s.summary(b, uuid.Nil)
	bg.Go(ctx, "baskets.publish", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := s.d.Broker.Publish(ctx, basketTopic(b.ID), EventBasket, sum); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})
}
