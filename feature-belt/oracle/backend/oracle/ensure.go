package oracle

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
)

// Feeds from code: other modules (the baskets feature) create and switch
// basket feeds inside their own transactions. The leader's catalog watcher
// loads a change within 5 s of commit; Reload makes it sooner.

// BasketPart is one part of a basket feed: a single feed's key and how many
// units of it the basket holds.
type BasketPart struct {
	Key      string
	Quantity float64
}

// BasketFeed is a basket feed as code defines it (EnsureBasketFeed).
type BasketFeed struct {
	Key      string // feed key: 1–48 of A–Z, 0–9, '.', '_', '-'
	Name     string // 1–120 characters
	Category string // up to 60 characters
	Quote    string // "" = USD
	Decimals int32  // decimals of the published price, 0–10
	// Listed shows the feed on the Prices screen; unlisted feeds are still
	// served by key (GET /v1/oracle/feeds/{key}).
	Listed bool
	Parts  []BasketPart // 1–100 single feeds, each once
}

// ErrNotBasket is returned when the key is held by a single feed.
var ErrNotBasket = errors.New("oracle: that key belongs to a single feed")

// ErrUnknownFeed is returned for a key no feed has.
var ErrUnknownFeed = errors.New("oracle: no feed with that key")

// checkBasketFeed validates b without the database and returns it tidied.
func checkBasketFeed(b BasketFeed) (BasketFeed, error) {
	b.Key = strings.ToUpper(strings.TrimSpace(b.Key))
	b.Name = strings.TrimSpace(b.Name)
	b.Category = strings.TrimSpace(b.Category)
	b.Quote = strings.ToUpper(strings.TrimSpace(b.Quote))
	if b.Quote == "" {
		b.Quote = "USD"
	}
	switch {
	case !feedKeyOK(b.Key):
		return b, fmt.Errorf("oracle: basket key %q must be 1–48 of A–Z, 0–9, '.', '_', '-'", b.Key)
	case b.Name == "" || len([]rune(b.Name)) > 120:
		return b, fmt.Errorf("oracle: basket %s: name must be 1–120 characters", b.Key)
	case len([]rune(b.Category)) > 60:
		return b, fmt.Errorf("oracle: basket %s: category must be up to 60 characters", b.Key)
	case len(b.Quote) > 12:
		return b, fmt.Errorf("oracle: basket %s: quote must be up to 12 characters", b.Key)
	case b.Decimals < 0 || b.Decimals > 10:
		return b, fmt.Errorf("oracle: basket %s: decimals must be 0–10", b.Key)
	case len(b.Parts) == 0 || len(b.Parts) > 100:
		return b, fmt.Errorf("oracle: basket %s: takes 1 to 100 parts", b.Key)
	}
	seen := map[string]bool{}
	parts := make([]BasketPart, len(b.Parts))
	for i, p := range b.Parts {
		p.Key = strings.ToUpper(strings.TrimSpace(p.Key))
		switch {
		case p.Key == b.Key:
			return b, fmt.Errorf("oracle: basket %s can't hold itself", b.Key)
		case seen[p.Key]:
			return b, fmt.Errorf("oracle: basket %s: part %s appears twice", b.Key, p.Key)
		case !(p.Quantity > 0) || math.IsInf(p.Quantity, 0):
			return b, fmt.Errorf("oracle: basket %s: part %s needs a positive quantity", b.Key, p.Key)
		}
		seen[p.Key] = true
		parts[i] = p
	}
	b.Parts = parts
	return b, nil
}

// EnsureBasketFeed creates the basket feed b.Key, or brings an existing basket
// feed's name, category, quote, decimals, listing and parts in line with b,
// inside the caller's transaction (q). It refuses a key held by a single
// (ErrNotBasket) and parts that are unknown or baskets themselves. It never
// changes whether the feed is enabled (SetFeedEnabled does), and changes
// nothing when the feed already matches. Call Reload after commit.
func (s *Service) EnsureBasketFeed(ctx context.Context, q db.Querier, in BasketFeed) error {
	b, err := checkBasketFeed(in)
	if err != nil {
		return err
	}
	keys := make([]string, len(b.Parts))
	for i, p := range b.Parts {
		keys[i] = p.Key
	}
	rows, err := q.Query(ctx, `SELECT key, id, kind FROM oracle_feeds WHERE key = ANY($1)`, keys)
	if err != nil {
		return err
	}
	ids := map[string]uuid.UUID{}
	for rows.Next() {
		var key, kind string
		var id uuid.UUID
		if err := rows.Scan(&key, &id, &kind); err != nil {
			rows.Close()
			return err
		}
		if kind != FeedSingle {
			rows.Close()
			return fmt.Errorf("oracle: basket %s: part %s is a basket; parts are single feeds", b.Key, key)
		}
		ids[key] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	parts := make([]partIn, len(b.Parts))
	for i, p := range b.Parts {
		id, ok := ids[p.Key]
		if !ok {
			return fmt.Errorf("oracle: basket %s: no feed %s", b.Key, p.Key)
		}
		parts[i] = partIn{id: id, qty: p.Quantity}
	}

	var (
		id                    uuid.UUID
		kind, name, cat, quot string
		dec                   int32
		listed                bool
	)
	err = q.QueryRow(ctx, `SELECT id, kind, name, category, quote, decimals, listed FROM oracle_feeds WHERE key = $1 FOR UPDATE`, b.Key).
		Scan(&id, &kind, &name, &cat, &quot, &dec, &listed)
	switch {
	case db.IsNoRows(err):
		p := Profiles["intraday"]
		if err := q.QueryRow(ctx, `INSERT INTO oracle_feeds (key, name, category, quote, decimals, kind, half_life_s, stale_after_s,
 max_move_bps_per_min, lookback_s, ref_window_s, burst_window_s, listed) VALUES ($1,$2,$3,$4,$5,'basket',$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
			b.Key, b.Name, b.Category, b.Quote, b.Decimals, int64(p.HalfLife/time.Second), int64(p.StaleAfter/time.Second),
			p.MaxMoveBpsPerMin, int64(p.Lookback/time.Second), int64(p.RefWindow/time.Second), int64(p.BurstWindow/time.Second), b.Listed).Scan(&id); err != nil {
			return err
		}
		return writeParts(ctx, q, id, parts)
	case err != nil:
		return err
	case kind != FeedBasket:
		return ErrNotBasket
	}
	current, err := loadParts(ctx, q, &id)
	if err != nil {
		return err
	}
	sameParts := len(current) == len(parts)
	if sameParts {
		for i, p := range current {
			if p.FeedID != parts[i].id || p.Quantity != parts[i].qty {
				sameParts = false
				break
			}
		}
	}
	if sameParts && name == b.Name && cat == b.Category && quot == b.Quote && dec == b.Decimals && listed == b.Listed {
		return nil
	}
	if _, err := q.Exec(ctx, `UPDATE oracle_feeds SET name = $2, category = $3, quote = $4, decimals = $5, listed = $6, updated_at = now() WHERE id = $1`,
		id, b.Name, b.Category, b.Quote, b.Decimals, b.Listed); err != nil {
		return err
	}
	if sameParts {
		return nil
	}
	return writeParts(ctx, q, id, parts)
}

// writeParts replaces a basket's parts (the admin path's insertParts, on any Querier).
func writeParts(ctx context.Context, q db.Querier, basketID uuid.UUID, parts []partIn) error {
	if _, err := q.Exec(ctx, `DELETE FROM oracle_basket_parts WHERE basket_id = $1`, basketID); err != nil {
		return err
	}
	for i, p := range parts {
		if _, err := q.Exec(ctx, `INSERT INTO oracle_basket_parts (basket_id, feed_id, quantity, position) VALUES ($1,$2,$3,$4)`, basketID, p.id, p.qty, i); err != nil {
			return err
		}
	}
	return nil
}

// SetFeedEnabled turns a feed on or off inside the caller's transaction (what
// an admin's PATCH enabled does, without the audit row: audit staff actions
// yourself). An off feed publishes status off; a basket with an off part goes
// stale. Unknown keys return ErrUnknownFeed. Call Reload after commit.
func (s *Service) SetFeedEnabled(ctx context.Context, q db.Querier, key string, enabled bool) error {
	key = strings.ToUpper(strings.TrimSpace(key))
	tag, err := q.Exec(ctx, `UPDATE oracle_feeds SET enabled = $2, updated_at = now() WHERE key = $1 AND enabled <> $2`, key, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM oracle_feeds WHERE key = $1)`, key).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrUnknownFeed
	}
	return nil
}
