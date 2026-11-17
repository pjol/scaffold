package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
)

// SeedDev adds development feeds once (a feed that exists is left alone):
//
//   - GOLD-OZ and SILVER-OZ, priced by staff prints from two dealers: three
//     days of prints with noise, dealer-b 3% above dealer-a (the reference,
//     fixed premium 0), which the oracle learns and removes,
//     one 40% outlier (rejected) and one +18% print (downweighted), plus 120
//     days of imported daily history;
//   - METALS-BASKET = 1 GOLD-OZ + 40 SILVER-OZ;
//   - BTC-USD from three public exchange APIs over the http source (no keys;
//     it shows source errors when the machine is offline) with a year of
//     daily history from one of them.
//
// Prints go through oracle_inbox, so the leader screens them like any other.
func (s *Service) SeedDev(ctx context.Context, _ []uuid.UUID) error {
	rng := rand.New(rand.NewPCG(7, 11))
	now := time.Now().UTC()
	daily := Profiles["daily"]
	metals := []struct {
		key, name string
		start     float64
		drift     float64 // per day
		vol       float64
	}{
		{"GOLD-OZ", "Gold, troy ounce", 2640, 0.002, 0.004},
		{"SILVER-OZ", "Silver, troy ounce", 31.2, 0.001, 0.008},
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, m := range metals {
			id, created, err := seedFeed(ctx, tx, m.key, m.name, "Metals", FeedSingle, &m.start, daily, true)
			if err != nil {
				return err
			}
			if !created {
				continue
			}
			// dealer-a is the reference market (fixed premium 0); dealer-b's premium is learned.
			if _, err := tx.Exec(ctx, `INSERT INTO oracle_feed_sources (feed_id, source, venue, premium) VALUES ($1, $2, 'dealer-a', 0), ($1, $2, 'dealer-b', NULL)`,
				id, ManualSource); err != nil {
				return err
			}
			// 120 days of history before the prints start, as staff would import it.
			px := m.start * math.Pow(1+m.drift, -120)
			for d := 123; d >= 4; d-- {
				px *= 1 + m.drift + m.vol*rng.NormFloat64()
				day := utcDay(now.AddDate(0, 0, -d))
				if _, err := tx.Exec(ctx, `INSERT INTO oracle_history (feed_id, day, close, source) VALUES ($1,$2,$3,'manual') ON CONFLICT DO NOTHING`, id, day, roundTo(px, 2)); err != nil {
					return err
				}
			}
			// Three days of prints, every 2 h from each dealer; dealer-b runs 3% above.
			fair := px
			start := now.Add(-72 * time.Hour)
			n := 0
			for t := start; !t.After(now); t = t.Add(2 * time.Hour) {
				fair *= 1 + m.drift/12 + m.vol/4*rng.NormFloat64()
				for vi, v := range []string{"dealer-a", "dealer-b"} {
					p := fair * (1 + 0.003*rng.NormFloat64())
					if vi == 1 {
						p *= 1.03
					}
					at := t.Add(time.Duration(vi*17+rng.IntN(20)) * time.Minute)
					if at.After(now) {
						continue
					}
					kind := KindSale
					if n%6 == 0 {
						kind = KindMarket
					}
					note := ""
					switch n {
					case 40:
						p, note = fair*1.4, "seeded outlier"
					case 52:
						p, note = fair*1.18, "seeded spike"
					}
					n++
					if err := pushTx(ctx, tx, id, ManualSource, Print{Venue: v, Kind: kind, Price: roundTo(p, 2), Qty: 1, At: at,
						ID: fmt.Sprintf("seed:%s:%s:%d", m.key, v, n), Note: note}, nil); err != nil {
						return err
					}
				}
			}
		}

		basket, created, err := seedFeed(ctx, tx, "METALS-BASKET", "Metals basket", "Metals", FeedBasket, nil, daily, true)
		if err != nil {
			return err
		}
		if created {
			for i, p := range []struct {
				key string
				qty float64
			}{{"GOLD-OZ", 1}, {"SILVER-OZ", 40}} {
				if _, err := tx.Exec(ctx, `INSERT INTO oracle_basket_parts (basket_id, feed_id, quantity, position)
 SELECT $1, id, $3, $4 FROM oracle_feeds WHERE key = $2`, basket, p.key, p.qty, i); err != nil {
					return err
				}
			}
		}

		btc, created, err := seedFeed(ctx, tx, "BTC-USD", "Bitcoin", "Crypto", FeedSingle, nil, Profiles["realtime"], true)
		if err != nil || !created {
			return err
		}
		refs := []struct {
			venue, url string
			params     map[string]any
		}{
			{"coinbase", "https://api.coinbase.com/v2/prices/BTC-USD/spot", map[string]any{"price_path": "data.amount", "every": "60s"}},
			{"kraken", "https://api.kraken.com/0/public/Ticker?pair=XBTUSD", map[string]any{"price_path": "result.XXBTZUSD.c.0", "every": "60s",
				"history_url": "https://api.kraken.com/0/public/OHLC?pair=XBTUSD&interval=1440&since={from_unix}", "history_path": "result.XXBTZUSD",
				"history_day_path": "0", "history_close_path": "4", "history_low_path": "3", "history_high_path": "2"}},
			{"bitstamp", "https://www.bitstamp.net/api/v2/ticker/btcusd/", map[string]any{"price_path": "last", "time_path": "timestamp", "every": "60s"}},
		}
		for _, r := range refs {
			params, _ := json.Marshal(r.params)
			if _, err := tx.Exec(ctx, `INSERT INTO oracle_feed_sources (feed_id, source, venue, ref, params) VALUES ($1,$2,$3,$4,$5)`,
				btc, httpSourceName, r.venue, r.url, string(params)); err != nil {
				return err
			}
		}
		return nil
	})
}

func seedFeed(ctx context.Context, tx pgx.Tx, key, name, category, kind string, seed *float64, p Params, listed bool) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO oracle_feeds (key, name, category, kind, seed_price, half_life_s, stale_after_s, max_move_bps_per_min,
 lookback_s, ref_window_s, burst_window_s, listed) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
 ON CONFLICT (key) DO NOTHING RETURNING id`, key, name, category, kind, seed, int64(p.HalfLife/time.Second), int64(p.StaleAfter/time.Second),
		p.MaxMoveBpsPerMin, int64(p.Lookback/time.Second), int64(p.RefWindow/time.Second), int64(p.BurstWindow/time.Second), listed).Scan(&id)
	if db.IsNoRows(err) {
		return id, false, nil
	}
	return id, err == nil, err
}
