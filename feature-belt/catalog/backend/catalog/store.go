package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

// watch loads the catalog, then reloads it whenever the stored version moves
// (a sync or a staff change on any instance), checking every
// CATALOG_RELOAD_INTERVAL and at once when this instance changed it.
func (s *Service) watch(ctx context.Context) {
	t := time.NewTicker(s.cfg.reloadEvery)
	defer t.Stop()
	for {
		if err := s.refresh(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("catalog: load failed; retrying", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.reload:
		}
	}
}

// reloadSoon asks the watcher to check the version now.
func (s *Service) reloadSoon() {
	select {
	case s.reload <- struct{}{}:
	default:
	}
}

// refresh rebuilds the index when the stored version differs from the
// loaded one, and swaps it in.
func (s *Service) refresh(ctx context.Context) error {
	s.loadMu.Lock()
	defer s.loadMu.Unlock()
	var v int64
	if err := s.d.Pool.QueryRow(ctx, `SELECT version FROM catalog_state`).Scan(&v); err != nil {
		return err
	}
	if cur := s.idx.Load(); cur.loaded && cur.version == v {
		return nil
	}
	started := time.Now()
	x, err := s.loadIndex(ctx)
	if err != nil {
		return err
	}
	s.idx.Store(x)
	s.log.Info("catalog: loaded", "items", len(x.recs), "active", x.nAct, "version", x.version, "took", time.Since(started).Round(time.Millisecond))
	return nil
}

const itemCols = `key, name, aliases, category, subtitle, image_url, attrs::text, coalesce(ref_price::text, ''), currency,
  coalesce(retired_by, ''), retired_at, first_seen_at, updated_at`

func scanRec(row interface{ Scan(...any) error }) (rec, error) {
	var r rec
	var retiredAt *time.Time
	err := row.Scan(&r.key, &r.name, &r.aliases, &r.category, &r.subtitle, &r.imageURL, &r.attrs, &r.price, &r.currency,
		&r.retiredBy, &retiredAt, &r.firstSeen, &r.updatedAt)
	if err != nil {
		return r, err
	}
	// the forms normalize writes, so an unchanged item compares equal (jsonb
	// and numeric print their own way: {"a": "b"}, 2.50)
	var attrs map[string]string
	if err := json.Unmarshal([]byte(r.attrs), &attrs); err != nil {
		return r, err
	}
	r.attrs = canonAttrs(attrs)
	if r.price != "" {
		d, err := decimal.NewFromString(r.price)
		if err != nil {
			return r, err
		}
		r.price = d.String()
	}
	if retiredAt != nil {
		r.retiredAt = retiredAt.UTC()
	}
	r.firstSeen, r.updatedAt = r.firstSeen.UTC(), r.updatedAt.UTC()
	return r, nil
}

// canonAttrs is attrs as compact JSON with sorted keys; "" for none.
func canonAttrs(attrs map[string]string) string {
	if len(attrs) == 0 {
		return ""
	}
	b, _ := json.Marshal(attrs)
	return string(b)
}

// loadIndex reads the version and every item from one snapshot.
func (s *Service) loadIndex(ctx context.Context) (*index, error) {
	tx, err := s.d.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var v int64
	if err := tx.QueryRow(ctx, `SELECT version FROM catalog_state`).Scan(&v); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT `+itemCols+` FROM catalog_items`)
	if err != nil {
		return nil, err
	}
	var recs []rec
	for rows.Next() {
		r, err := scanRec(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		recs = append(recs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildIndex(recs, v)
}

func bumpVersion(ctx context.Context, q db.Querier) error {
	_, err := q.Exec(ctx, `UPDATE catalog_state SET version = version + 1, changed_at = now()`)
	return err
}

// ---- applying a sync ----

const chunk = 2000

// columns as text arrays, unnested in SQL: aliases as a JSON array, attrs as
// a JSON object, price as a decimal or ''.
type cols struct{ key, name, aliases, category, subtitle, image, attrs, price, currency []string }

func toCols(rs []rec) cols {
	var c cols
	for _, r := range rs {
		al, _ := json.Marshal(r.aliases)
		if r.aliases == nil {
			al = []byte("[]")
		}
		attrs := r.attrs
		if attrs == "" {
			attrs = "{}"
		}
		c.key, c.name, c.aliases = append(c.key, r.key), append(c.name, r.name), append(c.aliases, string(al))
		c.category, c.subtitle, c.image = append(c.category, r.category), append(c.subtitle, r.subtitle), append(c.image, r.imageURL)
		c.attrs, c.price, c.currency = append(c.attrs, attrs), append(c.price, r.price), append(c.currency, r.currency)
	}
	return c
}

func (c cols) args(now time.Time) []any {
	return []any{c.key, c.name, c.aliases, c.category, c.subtitle, c.image, c.attrs, c.price, c.currency, now}
}

const unnestRows = `unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[])
  AS t(key, name, aliases, category, subtitle, image_url, attrs, price, currency)`

const aliasArray = `ARRAY(SELECT e.a FROM jsonb_array_elements_text(t.aliases::jsonb) WITH ORDINALITY AS e(a, n) ORDER BY e.n)`

const insertItems = `INSERT INTO catalog_items (key, name, aliases, category, subtitle, image_url, attrs, ref_price, currency, first_seen_at, updated_at)
SELECT t.key, t.name, ` + aliasArray + `, t.category, t.subtitle, t.image_url, t.attrs::jsonb, NULLIF(t.price, '')::numeric, t.currency, $10, $10
FROM ` + unnestRows + `
ON CONFLICT (key) DO NOTHING`

// updateItems rewrites content only: retirement is the restore and retire
// statements' business, guarded so a staff change made during the sync wins.
const updateItems = `UPDATE catalog_items c SET name = t.name, aliases = ` + aliasArray + `, category = t.category,
  subtitle = t.subtitle, image_url = t.image_url, attrs = t.attrs::jsonb, ref_price = NULLIF(t.price, '')::numeric,
  currency = t.currency, updated_at = $10
FROM ` + unnestRows + `
WHERE c.key = t.key`

// applyPlan writes a plan in tx and returns how many items it actually
// retired and restored.
func applyPlan(ctx context.Context, tx pgx.Tx, p plan) (retired, restored int, err error) {
	now := time.Now().UTC()
	for i := 0; i < len(p.insert); i += chunk {
		if _, err := tx.Exec(ctx, insertItems, toCols(p.insert[i:min(i+chunk, len(p.insert))]).args(now)...); err != nil {
			return 0, 0, err
		}
	}
	for i := 0; i < len(p.update); i += chunk {
		if _, err := tx.Exec(ctx, updateItems, toCols(p.update[i:min(i+chunk, len(p.update))]).args(now)...); err != nil {
			return 0, 0, err
		}
	}
	for i := 0; i < len(p.restore); i += chunk {
		tag, err := tx.Exec(ctx, `UPDATE catalog_items SET retired_at = NULL, retired_by = NULL, updated_at = $2
 WHERE key = ANY($1::text[]) AND retired_by = 'source'`, p.restore[i:min(i+chunk, len(p.restore))], now)
		if err != nil {
			return 0, 0, err
		}
		restored += int(tag.RowsAffected())
	}
	for i := 0; i < len(p.retire); i += chunk {
		tag, err := tx.Exec(ctx, `UPDATE catalog_items SET retired_at = $2, retired_by = 'source', updated_at = $2
 WHERE key = ANY($1::text[]) AND retired_at IS NULL`, p.retire[i:min(i+chunk, len(p.retire))], now)
		if err != nil {
			return 0, 0, err
		}
		retired += int(tag.RowsAffected())
	}
	return retired, restored, nil
}

// ---- run rows ----

const runCols = `id, trigger, source, forced, status, started_at, finished_at, fetched, inserted, updated, retired, restored,
  unchanged, skipped, error, notes`

func scanRun(row interface{ Scan(...any) error }) (SyncRun, error) {
	var r SyncRun
	err := row.Scan(&r.ID, &r.Trigger, &r.Source, &r.Forced, &r.Status, &r.StartedAt, &r.FinishedAt, &r.Fetched, &r.Inserted,
		&r.Updated, &r.Retired, &r.Restored, &r.Unchanged, &r.Skipped, &r.Error, &r.Notes)
	if r.Notes == nil {
		r.Notes = []string{}
	}
	return r, err
}

func (s *Service) finishRun(ctx context.Context, q db.Querier, r SyncRun) error {
	notes := r.Notes
	if notes == nil {
		notes = []string{}
	}
	_, err := q.Exec(ctx, `UPDATE catalog_sync_runs SET status = $2, finished_at = now(), heartbeat_at = now(), fetched = $3,
  inserted = $4, updated = $5, retired = $6, restored = $7, unchanged = $8, skipped = $9, error = $10, notes = $11
 WHERE id = $1`, r.ID, r.Status, r.Fetched, r.Inserted, r.Updated, r.Retired, r.Restored, r.Unchanged, r.Skipped, r.Error, notes)
	return err
}
