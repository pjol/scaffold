package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The pipeline never waits on the database: writes queue and a writer flushes
// them in batches once a second (or every 500 statements). A full queue drops
// writes and logs how many; the next tick rewrites prices and state anyway.

type dbWrite struct {
	sql  string
	args []any
}

func (s *Service) queue(sql string, args ...any) {
	select {
	case s.writes <- dbWrite{sql, args}:
	default:
		s.dropped.Add(1)
	}
}

func (s *Service) writer(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var pending []dbWrite
	flush := func() {
		if len(pending) == 0 {
			return
		}
		b := &pgx.Batch{}
		for _, w := range pending {
			b.Queue(w.sql, w.args...)
		}
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		err := s.d.Pool.SendBatch(fctx, b).Close()
		cancel()
		if err != nil {
			s.log.Warn("oracle: persist failed", "err", err, "statements", len(pending))
		}
		pending = pending[:0]
		if n := s.dropped.Swap(0); n > 0 {
			s.log.Warn("oracle: persist queue full; dropped writes", "n", n)
		}
	}
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case w := <-s.writes:
					pending = append(pending, w)
				default:
					flush()
					return
				}
			}
		case w := <-s.writes:
			pending = append(pending, w)
			if len(pending) >= 500 {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

const insertObsSQL = `INSERT INTO oracle_observations
 (id, feed_id, source, venue, kind, price, raw_price, premium, qty, observed_at, received_at, verdict, score, reasons, model, weight, actor_id, dedupe_key)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) ON CONFLICT (feed_id, dedupe_key) DO NOTHING`

func (s *Service) persistObs(r *rec, actor *uuid.UUID) {
	o := r.obs
	reasons, _ := json.Marshal(o.Reasons)
	var qty any
	if r.qty > 0 {
		qty = r.qty
	}
	s.queue(insertObsSQL, o.ID, o.FeedID, o.Source, o.Venue, o.Kind, r.px, r.raw, o.Premium, qty, o.ObservedAt, o.ReceivedAt,
		o.Verdict, o.Score, string(reasons), o.Model, o.Weight, actor, r.dedupe)
}

func nullPrice(x float64) any {
	if x <= 0 {
		return nil
	}
	return x
}

func (s *Service) persistState(pp PricePoint, f *feed) {
	var lastAccepted any
	live := false
	if f.kind == FeedSingle {
		live = f.live
		if !f.lastAccepted.IsZero() {
			lastAccepted = f.lastAccepted
		}
	} else {
		live = pp.Status == StatusLive || pp.Status == StatusStale
	}
	s.queue(`INSERT INTO oracle_feed_state (feed_id, price, target, status, live, last_accepted_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT (feed_id) DO UPDATE SET price=EXCLUDED.price, target=EXCLUDED.target, status=EXCLUDED.status, live=EXCLUDED.live,
 last_accepted_at=EXCLUDED.last_accepted_at, updated_at=EXCLUDED.updated_at`,
		pp.FeedID, nullPrice(pp.Price), nullPrice(pp.Target), pp.Status, live, lastAccepted, pp.At)
}

func (s *Service) persistPrice(pp PricePoint) {
	s.queue(`INSERT INTO oracle_prices (feed_id, at, price, target, status) VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		pp.FeedID, pp.At, pp.Price, nullPrice(pp.Target), pp.Status)
}

// persistDailyClose keeps today's bar from the published price: the oracle's
// own close replaces any backfilled point for the day.
func (s *Service) persistDailyClose(pp PricePoint) {
	s.queue(`INSERT INTO oracle_history (feed_id, day, close, low, high, source) VALUES ($1,$2,$3,$3,$3,'oracle')
 ON CONFLICT (feed_id, day) DO UPDATE SET close = EXCLUDED.close,
   low = CASE WHEN oracle_history.source = 'oracle' THEN least(oracle_history.low, EXCLUDED.low) ELSE EXCLUDED.low END,
   high = CASE WHEN oracle_history.source = 'oracle' THEN greatest(oracle_history.high, EXCLUDED.high) ELSE EXCLUDED.high END,
   source = 'oracle'`, pp.FeedID, utcDay(pp.At), pp.Price)
}

func (s *Service) persistPremium(feedID uuid.UUID, venue string, ps *premiumState, learned float64) {
	logs, _ := json.Marshal(ps.logs)
	s.queue(`INSERT INTO oracle_venue_premiums (feed_id, venue, logs, premium, updated_at) VALUES ($1,$2,$3,$4,now())
 ON CONFLICT (feed_id, venue) DO UPDATE SET logs=EXCLUDED.logs, premium=EXCLUDED.premium, updated_at=now()`,
		feedID, venue, string(logs), learned)
}

// ---- fetch log (last fetch per source task, persisted) ----

type fetchRec struct {
	last  time.Time
	value string
}

func fkey(source, key string) string { return source + "\x00" + key }

func (s *Service) lastFetch(source, key string) (time.Time, bool) {
	s.fmu.Lock()
	defer s.fmu.Unlock()
	r, ok := s.fetchLog[fkey(source, key)]
	return r.last, ok && !r.last.IsZero()
}

func (s *Service) recordFetch(source, key string, at time.Time) {
	s.fmu.Lock()
	r := s.fetchLog[fkey(source, key)]
	r.last = at
	s.fetchLog[fkey(source, key)] = r
	s.fmu.Unlock()
	s.queue(`INSERT INTO oracle_fetch_log (source, fetch_key, last_at) VALUES ($1,$2,$3)
 ON CONFLICT (source, fetch_key) DO UPDATE SET last_at=EXCLUDED.last_at`, source, key, at)
}

func (s *Service) clearFetch(source, key string) {
	s.fmu.Lock()
	delete(s.fetchLog, fkey(source, key))
	s.fmu.Unlock()
	s.queue(`DELETE FROM oracle_fetch_log WHERE source = $1 AND fetch_key = $2`, source, key)
}

// persistHealth records a lane's state: rate limited or not, last success.
func (s *Service) persistHealth(source, lane string, lim *limiter) {
	ok, note, lastOK, lastErrAt := lim.state(time.Now())
	s.queue(`INSERT INTO oracle_source_health (source, lane, ok, note, last_ok_at, last_error_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,now())
 ON CONFLICT (source, lane) DO UPDATE SET ok=EXCLUDED.ok, note=EXCLUDED.note, last_ok_at=EXCLUDED.last_ok_at,
 last_error_at=EXCLUDED.last_error_at, updated_at=now()`,
		source, lane, ok, nullString(note), nullTime(lastOK), nullTime(lastErrAt))
}

// persistRefHealth records the outcome of a task for each source row in it,
// which is what a feed's venue shows.
func (s *Service) persistRefHealth(t Task, err error, fails int, retry time.Duration) {
	for _, r := range t.Refs {
		if err == nil {
			s.queue(`INSERT INTO oracle_ref_health (ref_id, ok, last_ok_at, fails, updated_at) VALUES ($1, true, now(), 0, now())
 ON CONFLICT (ref_id) DO UPDATE SET ok = true, last_ok_at = now(), fails = 0, updated_at = now()`, r.ID)
			continue
		}
		msg := truncate(err.Error(), 200)
		if retry > 0 {
			msg = fmt.Sprintf("%s; retrying in %s", msg, retry.Round(time.Second))
		}
		s.queue(`INSERT INTO oracle_ref_health (ref_id, ok, last_error, last_error_at, fails, updated_at) VALUES ($1, false, $2, now(), $3, now())
 ON CONFLICT (ref_id) DO UPDATE SET ok = false, last_error = EXCLUDED.last_error, last_error_at = now(), fails = EXCLUDED.fails, updated_at = now()`,
			r.ID, msg, fails)
	}
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// restore loads persisted pipeline state when this instance becomes leader:
// published prices (so a restart doesn't snap back to the seed), recent
// observations (screening context and dedupe), learned premiums, the fetch
// log and source health. Errors are logged, never fatal.
func (s *Service) restore(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	pool := s.d.Pool

	rows, err := pool.Query(ctx, `SELECT feed_id, coalesce(price, 0)::float8, coalesce(target, 0)::float8, live, last_accepted_at FROM oracle_feed_state`)
	if err != nil {
		s.log.Warn("oracle: restore state", "err", err)
		return
	}
	n := 0
	for rows.Next() {
		var id uuid.UUID
		var price, target float64
		var live bool
		var last *time.Time
		if rows.Scan(&id, &price, &target, &live, &last) != nil {
			continue
		}
		f := s.feeds[id]
		if f == nil || f.kind != FeedSingle || price <= 0 {
			continue
		}
		if f.seed > 0 && (price > 3*f.seed || price < f.seed/3) {
			s.log.Warn("oracle: ignoring persisted price far from the seed (seed changed?)", "feed", f.key, "price", price, "seed", f.seed)
			continue
		}
		f.price, f.target, f.live = price, target, live
		if target <= 0 {
			f.target = price
		}
		if last != nil {
			f.lastAccepted = *last
		}
		n++
	}
	rows.Close()

	rows, err = pool.Query(ctx, `SELECT id, feed_id, source, venue, kind, price::float8, raw_price::float8, premium::float8, coalesce(qty, 0)::float8,
 observed_at, received_at, verdict, score::float8, reasons, model, weight::float8, dedupe_key FROM (
   SELECT *, row_number() OVER (PARTITION BY feed_id ORDER BY observed_at DESC) AS rn FROM oracle_observations
 ) t WHERE rn <= $1 ORDER BY observed_at`, maxRecsPerFeed)
	if err != nil {
		s.log.Warn("oracle: restore observations", "err", err)
		return
	}
	nObs := 0
	for rows.Next() {
		var o Observation
		var px, raw, qty float64
		var reasons []byte
		var dedupe string
		if err := rows.Scan(&o.ID, &o.FeedID, &o.Source, &o.Venue, &o.Kind, &px, &raw, &o.Premium, &qty, &o.ObservedAt, &o.ReceivedAt,
			&o.Verdict, &o.Score, &reasons, &o.Model, &o.Weight, &dedupe); err != nil {
			s.log.Warn("oracle: restore observation", "err", err)
			continue
		}
		f := s.feeds[o.FeedID]
		if f == nil || f.kind != FeedSingle {
			continue
		}
		_ = json.Unmarshal(reasons, &o.Reasons)
		if o.Reasons == nil {
			o.Reasons = []string{}
		}
		o.FeedKey = f.key
		o.Price, o.RawPrice = fmtPrice(px, f.decimals+2), fmtRaw(raw)
		if qty > 0 {
			q := qty
			o.Qty = &q
		}
		r := &rec{obs: o, dedupe: dedupe, px: px, raw: raw, qty: qty, observed: o.ObservedAt}
		r.baseW = restoredWeight(f, r)
		f.add(r)
		nObs++
	}
	rows.Close()

	rows, err = pool.Query(ctx, `SELECT feed_id, venue, logs FROM oracle_venue_premiums`)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			var venue string
			var logs []byte
			if rows.Scan(&id, &venue, &logs) != nil {
				continue
			}
			if f := s.feeds[id]; f != nil {
				ps := &premiumState{}
				if json.Unmarshal(logs, &ps.logs) == nil {
					f.premiums[venue] = ps
				}
			}
		}
		rows.Close()
	}

	rows, err = pool.Query(ctx, `SELECT source, fetch_key, last_at, coalesce(value, '') FROM oracle_fetch_log`)
	if err == nil {
		s.fmu.Lock()
		for rows.Next() {
			var src, key, val string
			var at time.Time
			if rows.Scan(&src, &key, &at, &val) == nil {
				s.fetchLog[fkey(src, key)] = fetchRec{last: at, value: val}
			}
		}
		s.fmu.Unlock()
		rows.Close()
	}
	rows, err = pool.Query(ctx, `SELECT source, lane, last_ok_at FROM oracle_source_health WHERE last_ok_at IS NOT NULL`)
	if err == nil {
		for rows.Next() {
			var src, lane string
			var at time.Time
			if rows.Scan(&src, &lane, &at) == nil {
				s.restoredOK[laneKey(src, lane)] = at
			}
		}
		rows.Close()
	}
	s.log.Info("oracle: restored", "feeds", n, "observations", nObs)
}

// restoredWeight rebuilds a record's undecayed weight from its verdict.
func restoredWeight(f *feed, r *rec) float64 {
	_, factor := verdictFor(r.obs.Score)
	switch r.obs.Verdict {
	case VerdictRejected:
		factor = 0
	case VerdictAccepted:
		factor = 1
	}
	vw := 1.0
	if ref := f.refFor(r.obs.Venue); ref != nil && ref.Weight > 0 {
		vw = ref.Weight
	}
	return kindWeight(r.obs.Kind, r.qty) * vw * factor
}
