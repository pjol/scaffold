package reputation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

const (
	chunkSize         = 500
	snapshotRetention = 14 * 24 * time.Hour  // snapshots older than this are pruned
	dayRetention      = 400 * 24 * time.Hour // day rows older than this are pruned
	confirmedReview   = Window * 24 * time.Hour
)

func chunks(ids []uuid.UUID, n int) [][]uuid.UUID {
	var out [][]uuid.UUID
	for len(ids) > 0 {
		k := min(n, len(ids))
		out = append(out, ids[:k])
		ids = ids[k:]
	}
	return out
}

// tick: snapshot every Source, roll the snapshotted people up into today's
// (and any missing) day rows, then rescore everyone whose days changed.
func (s *Service) tick(ctx context.Context) error {
	var errs []error
	ids, err := s.snapshot(ctx, time.Now().UTC())
	if err != nil {
		errs = append(errs, fmt.Errorf("snapshot: %w", err))
	}
	if err := s.rollup(ctx, ids, time.Now().UTC()); err != nil {
		errs = append(errs, fmt.Errorf("rollup: %w", err))
	}
	if err := s.rescoreChanged(ctx); err != nil {
		errs = append(errs, fmt.Errorf("rescore: %w", err))
	}
	return errors.Join(errs...)
}

// daily: prune, rescore everyone (ages and the window move every day) and
// run the integrity detectors.
func (s *Service) daily(ctx context.Context) error {
	now := time.Now().UTC()
	var errs []error
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM reputation_snapshots WHERE at < $1`, now.Add(-snapshotRetention)); err != nil {
		errs = append(errs, fmt.Errorf("prune snapshots: %w", err))
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM reputation_days WHERE day < $1`, dayStart(now.Add(-dayRetention))); err != nil {
		errs = append(errs, fmt.Errorf("prune days: %w", err))
	}
	if n, err := s.runIntegrity(ctx, now); err != nil {
		errs = append(errs, fmt.Errorf("integrity: %w", err))
	} else if n > 0 {
		s.log.WarnContext(ctx, "reputation: integrity flags raised", "flags", n)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT user_id FROM reputation_days WHERE day >= $1
		UNION SELECT user_id FROM reputation_scores`, dayStart(now).AddDate(0, 0, -(Window-1)))
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	if err := s.Rescore(ctx, ids); err != nil {
		errs = append(errs, fmt.Errorf("rescore: %w", err))
	}
	return errors.Join(errs...)
}

// sample reads every Source and sums each person's samples.
func (s *Service) sample(ctx context.Context) (map[uuid.UUID]Sample, bool, error) {
	out := map[uuid.UUID]Sample{}
	found := false
	var errs []error
	for _, m := range s.d.Modules {
		src, ok := m.(Source)
		if !ok {
			continue
		}
		found = true
		samples, err := src.ReputationSamples(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Name(), err))
			continue
		}
		for _, x := range samples {
			if x.UserID == uuid.Nil {
				continue
			}
			cur, seen := out[x.UserID]
			if !seen {
				out[x.UserID] = x
				continue
			}
			cur.Value = cur.Value.Add(x.Value)
			cur.NetFlows = cur.NetFlows.Add(x.NetFlows)
			cur.Activity += x.Activity
			cur.Volume = cur.Volume.Add(x.Volume)
			cur.Incidents += x.Incidents
			cur.Exposed = cur.Exposed || x.Exposed
			if !x.FirstActivity.IsZero() && (cur.FirstActivity.IsZero() || x.FirstActivity.Before(cur.FirstActivity)) {
				cur.FirstActivity = x.FirstActivity
			}
			out[x.UserID] = cur
		}
	}
	return out, found, errors.Join(errs...)
}

// snapshot stores one snapshot per sampled person (people who aren't
// accounts here, or are deleted, are skipped) and returns who was stored.
func (s *Service) snapshot(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	samples, found, serr := s.sample(ctx)
	if !found || len(samples) == 0 {
		return nil, serr
	}
	ids := make([]uuid.UUID, 0, len(samples))
	for id := range samples {
		ids = append(ids, id)
	}
	sortIDs(ids)
	at := now.Truncate(time.Second)
	var stored []uuid.UUID
	for _, ch := range chunks(ids, chunkSize) {
		var (
			vals, flows, vols []string
			acts, incs        []int64
			exposed           []bool
			firstIDs          []uuid.UUID
			firsts            []time.Time
		)
		for _, id := range ch {
			x := samples[id]
			vals, flows, vols = append(vals, x.Value.String()), append(flows, x.NetFlows.String()), append(vols, x.Volume.String())
			acts, incs, exposed = append(acts, x.Activity), append(incs, x.Incidents), append(exposed, x.Exposed)
			if !x.FirstActivity.IsZero() {
				firstIDs, firsts = append(firstIDs, id), append(firsts, x.FirstActivity.UTC())
			}
		}
		rows, err := s.d.Pool.Query(ctx, `
			INSERT INTO reputation_snapshots (user_id, at, value, net_flows, activity, volume, incidents, exposed)
			SELECT x.user_id, $2, x.value::numeric, x.flows::numeric, x.activity, x.volume::numeric, x.incidents, x.exposed
			FROM unnest($1::uuid[], $3::text[], $4::text[], $5::bigint[], $6::text[], $7::bigint[], $8::bool[])
			  AS x(user_id, value, flows, activity, volume, incidents, exposed)
			JOIN users u ON u.id = x.user_id AND u.status <> 'deleted'
			ON CONFLICT (user_id, at) DO UPDATE SET value = EXCLUDED.value, net_flows = EXCLUDED.net_flows,
			  activity = EXCLUDED.activity, volume = EXCLUDED.volume, incidents = EXCLUDED.incidents, exposed = EXCLUDED.exposed
			RETURNING user_id`, ch, at, vals, flows, acts, vols, incs, exposed)
		if err != nil {
			return stored, err
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return stored, err
		}
		stored = append(stored, got...)
		if len(firstIDs) > 0 {
			if _, err := s.d.Pool.Exec(ctx, `
				INSERT INTO reputation_scores (user_id, first_activity_at)
				SELECT x.user_id, x.first FROM unnest($1::uuid[], $2::timestamptz[]) AS x(user_id, first)
				JOIN users u ON u.id = x.user_id AND u.status <> 'deleted'
				ON CONFLICT (user_id) DO UPDATE SET first_activity_at = EXCLUDED.first_activity_at
				WHERE reputation_scores.first_activity_at IS NULL OR EXCLUDED.first_activity_at < reputation_scores.first_activity_at`,
				firstIDs, firsts); err != nil {
				return stored, err
			}
		}
	}
	return stored, serr
}

func (s *Service) floor() decimal.Decimal { return s.model.ReturnFloor }

// rollup recomputes the snapshot day rows of ids from their last one (or
// their first snapshot) up to today. Pushed days are left alone.
func (s *Service) rollup(ctx context.Context, ids []uuid.UUID, now time.Time) error {
	today := dayStart(now)
	for _, ch := range chunks(ids, chunkSize) {
		start := map[uuid.UUID]time.Time{}
		rows, err := s.d.Pool.Query(ctx, `SELECT user_id, max(day) FROM reputation_days
			WHERE user_id = ANY($1) AND source = 'snapshots' GROUP BY 1`, ch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			var d time.Time
			if err := rows.Scan(&id, &d); err != nil {
				rows.Close()
				return err
			}
			start[id] = dayStart(d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		from := today
		for _, id := range ch {
			d, ok := start[id]
			if !ok {
				d = today.Add(-snapshotRetention) // no day rows yet: every snapshot kept
			}
			if d.Before(from) {
				from = d
			}
		}
		snaps, err := s.snapshots(ctx, ch, from)
		if err != nil {
			return err
		}
		b := &pgx.Batch{}
		for _, id := range ch {
			ss := snaps[id]
			if len(ss) == 0 {
				continue
			}
			first, ok := start[id]
			if !ok {
				first = dayStart(ss[0].At)
			}
			for _, d := range RollupDays(ss, first, today, s.floor()) {
				b.Queue(`INSERT INTO reputation_days (user_id, day, open_value, close_value, flows, active, activity, volume, incidents, source)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'snapshots')
					ON CONFLICT (user_id, day) DO UPDATE SET open_value = EXCLUDED.open_value, close_value = EXCLUDED.close_value,
					  flows = EXCLUDED.flows, active = EXCLUDED.active, activity = EXCLUDED.activity, volume = EXCLUDED.volume,
					  incidents = EXCLUDED.incidents, updated_at = now()
					WHERE reputation_days.source = 'snapshots'
					  AND (reputation_days.open_value, reputation_days.close_value, reputation_days.flows, reputation_days.active,
					       reputation_days.activity, reputation_days.volume, reputation_days.incidents)
					  IS DISTINCT FROM (EXCLUDED.open_value, EXCLUDED.close_value, EXCLUDED.flows, EXCLUDED.active,
					       EXCLUDED.activity, EXCLUDED.volume, EXCLUDED.incidents)`,
					id, d.Day, d.Open, d.Close, d.Flows, d.Active, d.Activity, d.Volume, d.Incidents)
			}
		}
		if b.Len() > 0 {
			if err := s.d.Pool.SendBatch(ctx, b).Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// snapshots loads ids' snapshots from `from` on, plus each one's last
// snapshot before it (the first day's open).
func (s *Service) snapshots(ctx context.Context, ids []uuid.UUID, from time.Time) (map[uuid.UUID][]Snapshot, error) {
	rows, err := s.d.Pool.Query(ctx, `
		(SELECT user_id, at, value, net_flows, activity, volume, incidents, exposed FROM reputation_snapshots
		 WHERE user_id = ANY($1) AND at >= $2)
		UNION ALL
		(SELECT DISTINCT ON (user_id) user_id, at, value, net_flows, activity, volume, incidents, exposed FROM reputation_snapshots
		 WHERE user_id = ANY($1) AND at < $2 ORDER BY user_id, at DESC)
		ORDER BY user_id, at`, ids, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]Snapshot{}
	for rows.Next() {
		var id uuid.UUID
		var x Snapshot
		if err := rows.Scan(&id, &x.At, &x.Value, &x.NetFlows, &x.Activity, &x.Volume, &x.Incidents, &x.Exposed); err != nil {
			return nil, err
		}
		out[id] = append(out[id], x)
	}
	return out, rows.Err()
}

// rescoreChanged rescores people whose day rows changed since their last
// computation, and people with a row that was never computed.
func (s *Service) rescoreChanged(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT DISTINCT d.user_id FROM reputation_days d LEFT JOIN reputation_scores s ON s.user_id = d.user_id
		WHERE d.day >= $1 AND (s.computed_at IS NULL OR d.updated_at > s.computed_at)
		UNION SELECT user_id FROM reputation_scores WHERE computed_at IS NULL`,
		dayStart(time.Now()).AddDate(0, 0, -(Window-1)))
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	return s.Rescore(ctx, ids)
}

type personMeta struct {
	created time.Time
	first   *time.Time
}

// Rescore computes and stores the reputation of ids (deleted accounts are skipped).
func (s *Service) Rescore(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	from := dayStart(now).AddDate(0, 0, -(Window - 1))
	communityFn, _, _ := seams()
	for _, ch := range chunks(ids, chunkSize) {
		meta := map[uuid.UUID]personMeta{}
		rows, err := s.d.Pool.Query(ctx, `SELECT u.id, u.created_at, s.first_activity_at FROM users u
			LEFT JOIN reputation_scores s ON s.user_id = u.id WHERE u.id = ANY($1) AND u.status <> 'deleted'`, ch)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			var m personMeta
			if err := rows.Scan(&id, &m.created, &m.first); err != nil {
				rows.Close()
				return err
			}
			meta[id] = m
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		days, err := s.days(ctx, ch, from)
		if err != nil {
			return err
		}
		review, err := underReview(ctx, s.d.Pool, ch, now)
		if err != nil {
			return err
		}
		b := &pgx.Batch{}
		for _, id := range ch {
			m, ok := meta[id]
			if !ok {
				continue
			}
			in := Input{Now: now, Started: m.created, Days: days[id], UnderReview: review[id]}
			if m.first != nil {
				in.Started = *m.first
			}
			if communityFn != nil {
				c, err := communityFn(ctx, s.d.Pool, id, now.AddDate(0, 0, -Window))
				if err != nil {
					s.log.WarnContext(ctx, "reputation: community signals failed", "user", id, "err", err)
				} else {
					in.Community = &c
				}
			}
			r := s.model.Compute(in)
			comps, _ := json.Marshal(r.Components)
			reqs, _ := json.Marshal(r.Requirements)
			st, _ := json.Marshal(r.Stats)
			b.Queue(`INSERT INTO reputation_scores (user_id, version, status, score, tier, components, requirements, stats, computed_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (user_id) DO UPDATE SET version = EXCLUDED.version, status = EXCLUDED.status, score = EXCLUDED.score,
				  tier = EXCLUDED.tier, components = EXCLUDED.components, requirements = EXCLUDED.requirements,
				  stats = EXCLUDED.stats, computed_at = EXCLUDED.computed_at`,
				id, r.Version, r.Status, r.Score, r.Tier, comps, reqs, st, r.ComputedAt)
		}
		if b.Len() > 0 {
			if err := s.d.Pool.SendBatch(ctx, b).Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// days loads ids' day rows from `from` on, with each day's return.
func (s *Service) days(ctx context.Context, ids []uuid.UUID, from time.Time) (map[uuid.UUID][]Day, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id, day, open_value, close_value, flows, active, activity, volume, incidents, source
		FROM reputation_days WHERE user_id = ANY($1) AND day >= $2 ORDER BY user_id, day`, ids, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]Day{}
	for rows.Next() {
		var id uuid.UUID
		var d Day
		if err := rows.Scan(&id, &d.Day, &d.Open, &d.Close, &d.Flows, &d.Active, &d.Activity, &d.Volume, &d.Incidents, &d.Source); err != nil {
			return nil, err
		}
		d.Day = dayStart(d.Day)
		d.Ret = DayReturn(d.Open, d.Close, d.Flows, s.floor())
		out[id] = append(out[id], d)
	}
	return out, rows.Err()
}

// underReview: people with an open flag, or one confirmed in the last 90 days.
func underReview(ctx context.Context, q db.Querier, ids []uuid.UUID, now time.Time) (map[uuid.UUID]bool, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT user_id FROM reputation_flags WHERE user_id = ANY($1)
		AND (status = 'open' OR (status = 'confirmed' AND decided_at > $2))`, ids, now.Add(-confirmedReview))
	if err != nil {
		return nil, err
	}
	got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(got))
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// runIntegrity runs the wash-pair and vote-ring detectors over the last 30
// days (each only when its seam is plugged in), stores new flags and
// rescores the people they put under review. It returns how many flags are new.
func (s *Service) runIntegrity(ctx context.Context, now time.Time) (int, error) {
	_, pairsFn, votesFn := seams()
	since := now.AddDate(0, 0, -detectWindowDays)
	var flags []IntegrityFlag
	var errs []error
	if pairsFn != nil {
		pairs, err := pairsFn(ctx, s.d.Pool, since)
		if err != nil {
			errs = append(errs, fmt.Errorf("pairs: %w", err))
		} else if len(pairs) > 0 {
			vol, err := s.volumeSince(ctx, pairs, since)
			if err != nil {
				return 0, err
			}
			flags = append(flags, DetectWashPairs(pairs, vol)...)
		}
	}
	if votesFn != nil {
		votes, err := votesFn(ctx, s.d.Pool, since)
		if err != nil {
			errs = append(errs, fmt.Errorf("votes: %w", err))
		} else {
			flags = append(flags, DetectVoteRings(votes)...)
		}
	}
	n := 0
	seen := map[uuid.UUID]bool{}
	var ids []uuid.UUID
	for _, f := range flags {
		added, err := insertFlag(ctx, s.d.Pool, f)
		if err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		if added {
			n++
		}
		if !seen[f.UserID] {
			seen[f.UserID] = true
			ids = append(ids, f.UserID)
		}
	}
	if err := s.Rescore(ctx, ids); err != nil {
		errs = append(errs, err)
	}
	return n, errors.Join(errs...)
}

// volumeSince is each paired person's volume over the window, from their day rows.
func (s *Service) volumeSince(ctx context.Context, pairs []PairStat, since time.Time) (map[uuid.UUID]decimal.Decimal, error) {
	set := map[uuid.UUID]bool{}
	for _, p := range pairs {
		set[p.A], set[p.B] = true, true
	}
	ids := make([]uuid.UUID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id, COALESCE(sum(volume), 0) FROM reputation_days
		WHERE user_id = ANY($1) AND day >= $2 GROUP BY 1`, ids, dayStart(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]decimal.Decimal{}
	for rows.Next() {
		var id uuid.UUID
		var v decimal.Decimal
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// load reads a person's stored reputation; ok is false when it was never computed.
func (s *Service) load(ctx context.Context, userID uuid.UUID) (Reputation, bool, error) {
	r := Reputation{UserID: userID.String()}
	var comps, reqs, st []byte
	err := s.d.Pool.QueryRow(ctx, `SELECT version, status, score, tier, components, requirements, stats, computed_at
		FROM reputation_scores WHERE user_id = $1`, userID).Scan(&r.Version, &r.Status, &r.Score, &r.Tier, &comps, &reqs, &st, &r.ComputedAt)
	if db.IsNoRows(err) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if r.ComputedAt == nil {
		return r, false, nil
	}
	_ = json.Unmarshal(comps, &r.Components)
	_ = json.Unmarshal(reqs, &r.Requirements)
	r.Stats = emptyStats()
	_ = json.Unmarshal(st, &r.Stats)
	if r.Components == nil {
		r.Components = []Component{}
	}
	if r.Requirements == nil {
		r.Requirements = []Requirement{}
	}
	if r.Stats.Equity == nil {
		r.Stats.Equity = []EquityPoint{}
	}
	if r.Status != StatusRated {
		r.Score, r.Tier = nil, nil
	}
	return r, true, nil
}

// full is the stored reputation, computed now when there is none yet.
func (s *Service) full(ctx context.Context, userID uuid.UUID) (Reputation, error) {
	r, ok, err := s.load(ctx, userID)
	if err != nil || ok {
		return r, err
	}
	if err := s.Rescore(ctx, []uuid.UUID{userID}); err != nil {
		return r, err
	}
	r, ok, err = s.load(ctx, userID)
	if err == nil && !ok {
		return r, errNoPerson
	}
	return r, err
}

var errNoPerson = errors.New("reputation: no such person")
