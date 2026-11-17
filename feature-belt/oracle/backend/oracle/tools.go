package oracle

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
)

// Tools: read-only access to feeds, prices, observations and history for
// agents (the mcp and assistant features, and agent-web's WebMCP).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "oracle_list_feeds",
			Description: "List price feeds with their published price, status (live, stale, seed) and venues. Prices are decimal strings in each feed's quote currency. Use query to match a key or name.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Text to match in the feed key or name"},"kind":{"type":"string","enum":["single","basket"]},"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Query string `json:"query"`
					Kind  string `json:"kind"`
					Limit int    `json:"limit"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, httpx.BadRequest("Invalid input.")
				}
				if a.Limit <= 0 {
					a.Limit = 50
				}
				items, err := s.listFeeds(ctx, feedQuery{q: a.Query, kind: a.Kind, limit: min(a.Limit, 100)})
				if err != nil {
					return nil, err
				}
				return map[string]any{"items": items}, nil
			},
		},
		{
			Name:        "oracle_get_feed",
			Description: "Get one price feed by key: published price and target, status, slew limit, each venue's last price and health, basket parts, the last day of published prices and the 20 latest screened observations with verdicts and reasons.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Feed key, e.g. BTC-USD"}},"required":["key"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Key string `json:"key"`
				}
				if err := json.Unmarshal(in, &a); err != nil || strings.TrimSpace(a.Key) == "" {
					return nil, httpx.BadRequest("key is required.")
				}
				fi, err := s.feedByKey(ctx, a.Key)
				if err != nil {
					return nil, err
				}
				return s.feedDetail(ctx, fi, 20)
			},
		},
		{
			Name:        "oracle_list_observations",
			Description: "List the latest screened price observations, newest first, optionally for one feed and one verdict (accepted, downweighted, rejected). Each has the venue, raw and normalised price, manipulation score, weight and the reasons for its verdict.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Feed key; omit for every feed"},"verdict":{"type":"string","enum":["accepted","downweighted","rejected"]},"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Key     string `json:"key"`
					Verdict string `json:"verdict"`
					Limit   int    `json:"limit"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return nil, httpx.BadRequest("Invalid input.")
				}
				q := obsQuery{verdict: a.Verdict, limit: min(max(a.Limit, 1), 100)}
				if a.Limit <= 0 {
					q.limit = 30
				}
				if a.Key != "" {
					fi, err := s.feedByKey(ctx, a.Key)
					if err != nil {
						return nil, err
					}
					if q.feeds, err = s.feedIDsFor(ctx, fi); err != nil {
						return nil, err
					}
				} else {
					ids, err := s.visibleFeedIDs(ctx)
					if err != nil {
						return nil, err
					}
					q.feeds = ids
				}
				items, err := s.queryObservations(ctx, q)
				if err != nil {
					return nil, err
				}
				return map[string]any{"items": items}, nil
			},
		},
		{
			Name:        "oracle_get_history",
			Description: "Get a feed's daily bars (open, high, low, close) for up to 365 days, oldest first. Days before the oracle published are backfilled reference prices (source names where they came from).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"days":{"type":"integer","minimum":1,"maximum":365}},"required":["key"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, _ *auth.Identity, in json.RawMessage) (any, error) {
				var a struct {
					Key  string `json:"key"`
					Days int    `json:"days"`
				}
				if err := json.Unmarshal(in, &a); err != nil || strings.TrimSpace(a.Key) == "" {
					return nil, httpx.BadRequest("key is required.")
				}
				fi, err := s.feedByKey(ctx, a.Key)
				if err != nil {
					return nil, err
				}
				if a.Days <= 0 {
					a.Days = 90
				}
				bars, err := s.HistoryBars(ctx, fi, a.Days, fi.priceF)
				if err != nil {
					return nil, err
				}
				return map[string]any{"key": fi.Key, "quote": fi.Quote, "items": bars}, nil
			},
		},
	}
}

// ---- retention ----

// prune drops published prices and observations past their retention, and
// drained inbox rows after a week.
func (s *Service) prune(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM oracle_prices WHERE at < now() - make_interval(secs => $1)`, s.cfg.priceRetention.Seconds()); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM oracle_observations WHERE observed_at < now() - make_interval(secs => $1)`, s.cfg.obsRetention.Seconds()); err != nil {
		return err
	}
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM oracle_inbox WHERE taken_at < now() - interval '7 days'`)
	return err
}

func (s *Service) startPrune(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "oracle.prune", 6*time.Hour, s.prune)
}

// ---- account deletion and export ----

// DeleteAccount keeps feeds and prints (prices other people rely on) and
// drops who created or posted them.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`UPDATE oracle_feeds SET created_by = NULL WHERE created_by = $1`,
		`UPDATE oracle_observations SET actor_id = NULL WHERE actor_id = $1`,
		`UPDATE oracle_inbox SET actor_id = NULL WHERE actor_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount lists the feeds a person created and the prices they posted.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type feedOut struct {
		Key       string    `json:"key"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}
	type printOut struct {
		Feed       string    `json:"feed"`
		Venue      string    `json:"venue"`
		Kind       string    `json:"kind"`
		Price      string    `json:"price"`
		ObservedAt time.Time `json:"observed_at"`
		Verdict    string    `json:"verdict"`
	}
	out := struct {
		Feeds  []feedOut  `json:"feeds_created"`
		Prints []printOut `json:"prices_posted"`
	}{Feeds: []feedOut{}, Prints: []printOut{}}
	rows, err := s.d.Pool.Query(ctx, `SELECT key, name, created_at FROM oracle_feeds WHERE created_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f feedOut
		if err := rows.Scan(&f.Key, &f.Name, &f.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Feeds = append(out.Feeds, f)
	}
	rows.Close()
	rows, err = s.d.Pool.Query(ctx, `SELECT f.key, o.venue, o.kind, o.raw_price::text, o.observed_at, o.verdict
 FROM oracle_observations o JOIN oracle_feeds f ON f.id = o.feed_id WHERE o.actor_id = $1 ORDER BY o.observed_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p printOut
		if err := rows.Scan(&p.Feed, &p.Venue, &p.Kind, &p.Price, &p.ObservedAt, &p.Verdict); err != nil {
			return nil, err
		}
		out.Prints = append(out.Prints, p)
	}
	return out, rows.Err()
}
