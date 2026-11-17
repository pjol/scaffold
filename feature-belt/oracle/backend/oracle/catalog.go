package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
)

// feedRow is one oracle_feeds row.
type feedRow struct {
	ID        uuid.UUID
	Key       string
	Name      string
	Category  string
	Quote     string
	Decimals  int32
	Kind      string
	Seed      *float64
	Params    Params
	Listed    bool
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

const feedColumns = `id, key, name, category, quote, decimals, kind, seed_price::float8, half_life_s, stale_after_s,
 max_move_bps_per_min::float8, lookback_s, ref_window_s, burst_window_s, listed, enabled, created_at, updated_at`

func scanFeedRow(row interface{ Scan(...any) error }) (feedRow, error) {
	var f feedRow
	var hl, st, lb, rw, bw int64
	err := row.Scan(&f.ID, &f.Key, &f.Name, &f.Category, &f.Quote, &f.Decimals, &f.Kind, &f.Seed, &hl, &st,
		&f.Params.MaxMoveBpsPerMin, &lb, &rw, &bw, &f.Listed, &f.Enabled, &f.CreatedAt, &f.UpdatedAt)
	f.Params.HalfLife = time.Duration(hl) * time.Second
	f.Params.StaleAfter = time.Duration(st) * time.Second
	f.Params.Lookback = time.Duration(lb) * time.Second
	f.Params.RefWindow = time.Duration(rw) * time.Second
	f.Params.BurstWindow = time.Duration(bw) * time.Second
	return f, err
}

type refRow struct {
	sourceRef
	FeedID uuid.UUID
}

func loadRefs(ctx context.Context, q db.Querier, feedID *uuid.UUID) ([]refRow, error) {
	rows, err := q.Query(ctx, `SELECT id, feed_id, source, venue, ref, params, weight::float8, premium::float8
 FROM oracle_feed_sources WHERE $1::uuid IS NULL OR feed_id = $1 ORDER BY feed_id, venue, id`, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []refRow
	for rows.Next() {
		var r refRow
		var weight *float64
		var params []byte
		if err := rows.Scan(&r.ID, &r.FeedID, &r.Source, &r.Venue, &r.Ref, &params, &weight, &r.Premium); err != nil {
			return nil, err
		}
		r.Params = json.RawMessage(params)
		if weight != nil {
			r.Weight = *weight
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type partRow struct {
	BasketID uuid.UUID
	FeedID   uuid.UUID
	Quantity float64
}

func loadParts(ctx context.Context, q db.Querier, basketID *uuid.UUID) ([]partRow, error) {
	rows, err := q.Query(ctx, `SELECT basket_id, feed_id, quantity::float8 FROM oracle_basket_parts
 WHERE $1::uuid IS NULL OR basket_id = $1 ORDER BY basket_id, position, feed_id`, basketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []partRow
	for rows.Next() {
		var p partRow
		if err := rows.Scan(&p.BasketID, &p.FeedID, &p.Quantity); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// catalogSignature changes whenever a feed, its sources or its parts change
// (admin writes bump the feed's updated_at in the same transaction).
func catalogSignature(ctx context.Context, q db.Querier) (string, error) {
	var sig string
	err := q.QueryRow(ctx, `SELECT coalesce(md5(string_agg(id::text || ':' || extract(epoch FROM updated_at)::text, ',' ORDER BY id)), '')
 FROM oracle_feeds`).Scan(&sig)
	return sig, err
}

// loadCatalog reads every feed and rebuilds the in-memory set, carrying each
// existing feed's pipeline state over by id.
func (s *Service) loadCatalog(ctx context.Context) error {
	sig, err := catalogSignature(ctx, s.d.Pool)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+feedColumns+` FROM oracle_feeds ORDER BY key`)
	if err != nil {
		return err
	}
	var fr []feedRow
	for rows.Next() {
		f, err := scanFeedRow(rows)
		if err != nil {
			rows.Close()
			return err
		}
		fr = append(fr, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	refs, err := loadRefs(ctx, s.d.Pool, nil)
	if err != nil {
		return err
	}
	parts, err := loadParts(ctx, s.d.Pool, nil)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	feeds := make(map[uuid.UUID]*feed, len(fr))
	byKey := make(map[string]*feed, len(fr))
	order := make([]*feed, 0, len(fr))
	for _, row := range fr {
		seed := 0.0
		if row.Seed != nil {
			seed = *row.Seed
		}
		f := s.feeds[row.ID]
		if f == nil {
			f = newFeed(row.ID, row.Key, seed, row.Params)
		}
		f.key, f.name, f.category, f.quote, f.decimals, f.kind = row.Key, row.Name, row.Category, row.Quote, row.Decimals, row.Kind
		f.listed, f.enabled, f.p = row.Listed, row.Enabled, row.Params
		if f.seed != seed {
			f.seed = seed
			if !f.live {
				f.price, f.target = seed, seed
			}
		}
		f.refs, f.parts, f.baskets = nil, nil, nil
		feeds[f.id] = f
		byKey[f.key] = f
		order = append(order, f)
	}
	for _, r := range refs {
		if f := feeds[r.FeedID]; f != nil && f.kind == FeedSingle {
			f.refs = append(f.refs, r.sourceRef)
		}
	}
	for _, p := range parts {
		b, f := feeds[p.BasketID], feeds[p.FeedID]
		if b == nil || f == nil || b.kind != FeedBasket || f.kind != FeedSingle {
			continue
		}
		b.parts = append(b.parts, part{qty: p.Quantity, f: f})
		f.baskets = append(f.baskets, b)
	}
	// Singles first, so baskets price from this tick's parts.
	sort.SliceStable(order, func(i, j int) bool { return order[i].kind == FeedSingle && order[j].kind != FeedSingle })
	for id := range s.prices {
		if feeds[id] == nil {
			delete(s.prices, id)
			delete(s.lastPub, id)
		}
	}
	s.feeds, s.byKey, s.order = feeds, byKey, order
	s.catalogSig = sig
	return nil
}

// feedKeyOK: upper-case letters, digits, dot, underscore and dash; 1–48 chars.
func feedKeyOK(k string) bool {
	if len(k) == 0 || len(k) > 48 {
		return false
	}
	for i, c := range k {
		switch {
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
