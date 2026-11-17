package copytrade

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
)

// ---- the caller's own leading ----

func (s *Service) leaderRec(ctx context.Context, userID uuid.UUID) (leaderRec, error) {
	lr, ok, err := getLeader(ctx, s.d.Pool, userID)
	if err != nil {
		return lr, err
	}
	if !ok {
		lr = leaderRec{UserID: userID, ProfitSharePct: decimal.Zero, MinAlloc: s.minAllocation, MaxCopiers: min(100, s.maxCopiersPerLeader)}
	}
	return lr, nil
}

// LeaderStatus is GET /copytrade/leader.
func (s *Service) LeaderStatus(ctx context.Context, userID uuid.UUID) (LeaderStatus, error) {
	lr, err := s.leaderRec(ctx, userID)
	if err != nil {
		return LeaderStatus{}, err
	}
	ok, reqs, err := s.eligible(ctx, userID)
	if err != nil {
		return LeaderStatus{}, err
	}
	st := LeaderStatus{Settings: lr.settings(), Eligible: ok, Requirements: reqs}
	var aum, earned string
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status <> 'stopped'),
		  COALESCE(sum(equity) FILTER (WHERE status <> 'stopped'), 0)::text, COALESCE(sum(profit_share_paid), 0)::text
		FROM copytrade_copies WHERE leader_id = $1`, userID).Scan(&st.Copiers, &aum, &earned); err != nil {
		return LeaderStatus{}, err
	}
	st.Aum, st.ProfitShareEarned = s.fixed(dec(aum)), s.fixed(dec(earned))
	return st, nil
}

func (s *Service) fixed(d decimal.Decimal) string {
	dp := int32(2)
	if ex := s.exchange(); ex != nil {
		_, dp = ex.Quote()
	}
	return d.StringFixed(dp)
}

// PutLeader saves leader settings. Turning copies on needs eligibility;
// turning them off stops every copy (positions closed or kept per each
// copier's on_leader_exit).
func (s *Service) PutLeader(ctx context.Context, userID uuid.UUID, in LeaderSettings) (LeaderStatus, error) {
	ps, err := decIn(in.ProfitSharePct, "profit_share_pct", decimal.Zero, s.maxProfitShare)
	if err != nil {
		return LeaderStatus{}, apiErr(err)
	}
	mn, err := decIn(in.MinAllocation, "min_allocation", s.minAllocation, d1m)
	if err != nil {
		return LeaderStatus{}, apiErr(err)
	}
	if in.MaxCopiers < 1 || in.MaxCopiers > s.maxCopiersPerLeader {
		return LeaderStatus{}, apiErr(invalid("max_copiers must be 1–%d.", s.maxCopiersPerLeader))
	}
	old, err := s.leaderRec(ctx, userID)
	if err != nil {
		return LeaderStatus{}, err
	}
	eligible := old.Eligible
	if in.Enabled {
		ok, reqs, err := s.eligible(ctx, userID)
		if err != nil {
			return LeaderStatus{}, err
		}
		if !ok {
			var unmet []string
			for _, r := range reqs {
				if !r.Met {
					unmet = append(unmet, strings.ToLower(r.Label)+" ("+r.Progress+")")
				}
			}
			return LeaderStatus{}, errStatus(http.StatusUnprocessableEntity, "leader_ineligible",
				"You can't offer copy trading yet: "+strings.Join(unmet, "; ")+".")
		}
		eligible = true
	}
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO copytrade_leaders (user_id, enabled, profit_share_pct, min_allocation, max_copiers,
		  show_copiers, eligible, checked_at, enabled_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), CASE WHEN $2 THEN now() END)
		ON CONFLICT (user_id) DO UPDATE SET enabled = EXCLUDED.enabled, profit_share_pct = EXCLUDED.profit_share_pct,
		  min_allocation = EXCLUDED.min_allocation, max_copiers = EXCLUDED.max_copiers, show_copiers = EXCLUDED.show_copiers,
		  eligible = EXCLUDED.eligible, checked_at = now(),
		  enabled_at = CASE WHEN EXCLUDED.enabled AND NOT copytrade_leaders.enabled THEN now()
		                    WHEN EXCLUDED.enabled THEN copytrade_leaders.enabled_at END,
		  updated_at = now()`,
		userID, in.Enabled, ps.String(), mn.String(), in.MaxCopiers, in.ShowCopiers, eligible); err != nil {
		return LeaderStatus{}, err
	}
	if old.Enabled && !in.Enabled {
		if rt := s.rt.Load(); rt != nil {
			rt.stopAllOf(context.WithoutCancel(ctx), userID, StopLeaderDisabled)
		} // elsewhere the pipeline's leader sync stops them within a minute
	}
	s.invalidateLeaders()
	return s.LeaderStatus(ctx, userID)
}

// stopAllOf stops every open copy of leader, each per its on_leader_exit.
func (rt *runtime) stopAllOf(ctx context.Context, leader uuid.UUID, reason string) {
	for _, cs := range rt.copiesOf(leader) {
		cs.mu.Lock()
		closeIt := cs.rec.Settings.closeOnExit()
		cs.mu.Unlock()
		if err := rt.Stop(ctx, cs, reason, closeIt); err != nil {
			slog.WarnContext(ctx, "copytrade: stopping a copy failed", "copy", cs.rec.ID, "reason", reason, "err", err)
		}
	}
}

// StopBetween stops copies between two people in either direction (call it
// when one blocks the other); positions close or stay per each copy's
// on_leader_exit. It works on any instance: the pipeline applies it at once
// here, or within a minute where the engine runs.
func (s *Service) StopBetween(ctx context.Context, a, b uuid.UUID) error {
	rows, err := s.d.Pool.Query(ctx, `UPDATE copytrade_copies SET status = 'stopping', stop_reason = $3, pause_reason = '',
		  close_on_stop = (settings->>'on_leader_exit') IS DISTINCT FROM 'keep', updated_at = now()
		WHERE status IN ('active', 'paused') AND ((follower_id = $1 AND leader_id = $2) OR (follower_id = $2 AND leader_id = $1))
		RETURNING id`, a, b, StopBlocked)
	if err != nil {
		return err
	}
	ids, err := collectUUIDs(rows)
	if err != nil {
		return err
	}
	if rt := s.rt.Load(); rt != nil {
		for _, id := range ids {
			if cs, ok := rt.state(id); ok {
				cs.mu.Lock()
				_ = rt.stopLocked(context.WithoutCancel(ctx), cs, StopBlocked, cs.rec.Settings.closeOnExit())
				cs.mu.Unlock()
			}
		}
	}
	return nil
}

// Copiers is the leader's copier list, newest first: allocations bucketed,
// people named only when they started while the leader showed copiers.
func (s *Service) Copiers(ctx context.Context, leader uuid.UUID, c pageCursor, limit int) ([]Copier, error) {
	where, args := keyset(`WHERE c.leader_id = $1 AND c.status <> 'stopped'`, []any{leader}, c, "c.created_at", "c.id")
	args = append(args, limit)
	rows, err := s.d.Pool.Query(ctx, `SELECT c.id::text, c.created_at, c.net_deposits::text, c.identity_visible, u.id, u.display_name, u.avatar_url
		FROM copytrade_copies c JOIN users u ON u.id = c.follower_id `+where+
		` ORDER BY c.created_at DESC, c.id DESC LIMIT $`+itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Copier{}
	for rows.Next() {
		var x Copier
		var nd string
		var visible bool
		var p Person
		var pid uuid.UUID
		if err := rows.Scan(&x.id, &x.Since, &nd, &visible, &pid, &p.DisplayName, &p.AvatarURL); err != nil {
			return nil, err
		}
		x.Allocation = allocationBucket(dec(nd))
		if visible {
			p.ID = pid.String()
			x.User = &p
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---- directory ----

type leaderCacheEntry struct {
	at    time.Time
	cards []LeaderCard
}

func (s *Service) invalidateLeaders() {
	s.lcMu.Lock()
	s.leaderCache = map[string]leaderCacheEntry{}
	s.lcMu.Unlock()
}

// directoryLimit caps the leader directory.
const directoryLimit = 200

// Leaders is the directory: enabled, eligible leaders, sorted by
// return_90d (default), return_30d, copiers or aum; cached 30 s. Copying and
// IsMe are filled per caller.
func (s *Service) Leaders(ctx context.Context, viewer uuid.UUID, sortBy string) ([]LeaderCard, error) {
	s.lcMu.Lock()
	e, ok := s.leaderCache[sortBy]
	s.lcMu.Unlock()
	if !ok || time.Since(e.at) > 30*time.Second {
		cards, err := s.loadLeaders(ctx, nil)
		if err != nil {
			return nil, err
		}
		sortCards(cards, sortBy)
		if len(cards) > directoryLimit {
			cards = cards[:directoryLimit]
		}
		e = leaderCacheEntry{at: time.Now(), cards: cards}
		s.lcMu.Lock()
		s.leaderCache[sortBy] = e
		s.lcMu.Unlock()
	}
	out := make([]LeaderCard, len(e.cards))
	copy(out, e.cards)
	copying, err := s.copyingSet(ctx, viewer)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].IsMe = out[i].User.ID == viewer.String()
		out[i].Copying = copying[out[i].User.ID]
	}
	return out, nil
}

// Leader is one directory card (also for leaders who stopped accepting).
func (s *Service) Leader(ctx context.Context, viewer, leader uuid.UUID) (LeaderCard, bool, error) {
	cards, err := s.loadLeaders(ctx, &leader)
	if err != nil || len(cards) == 0 {
		return LeaderCard{}, false, err
	}
	c := cards[0]
	copying, err := s.copyingSet(ctx, viewer)
	if err != nil {
		return c, false, err
	}
	c.IsMe, c.Copying = c.User.ID == viewer.String(), copying[c.User.ID]
	return c, true, nil
}

func (s *Service) copyingSet(ctx context.Context, viewer uuid.UUID) (map[string]bool, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT leader_id::text FROM copytrade_copies WHERE follower_id = $1 AND status <> 'stopped'`, viewer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// loadLeaders builds cards for every enabled leader (or one, enabled or not).
func (s *Service) loadLeaders(ctx context.Context, one *uuid.UUID) ([]LeaderCard, error) {
	where := `WHERE l.enabled AND l.eligible AND u.status = 'active'`
	args := []any{}
	if one != nil {
		where, args = `WHERE l.user_id = $1`, []any{*one}
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT l.user_id, u.display_name, u.avatar_url, l.enabled AND l.eligible AND u.status = 'active',
		  l.profit_share_pct::text, l.min_allocation::text, l.max_copiers, l.show_copiers, COALESCE(l.enabled_at, l.created_at),
		  (SELECT count(*) FROM copytrade_copies c WHERE c.leader_id = l.user_id AND c.status <> 'stopped'),
		  (SELECT COALESCE(sum(c.equity), 0)::text FROM copytrade_copies c WHERE c.leader_id = l.user_id AND c.status <> 'stopped')
		FROM copytrade_leaders l JOIN users u ON u.id = l.user_id `+where+` LIMIT 1000`, args...)
	if err != nil {
		return nil, err
	}
	var cards []LeaderCard
	var ids []uuid.UUID
	for rows.Next() {
		var c LeaderCard
		var id uuid.UUID
		var listed bool
		var ps, mn, aum string
		var maxCopiers int
		if err := rows.Scan(&id, &c.User.DisplayName, &c.User.AvatarURL, &listed, &ps, &mn, &maxCopiers, &c.ShowCopiers,
			&c.LeadingSince, &c.Copiers, &aum); err != nil {
			rows.Close()
			return nil, err
		}
		c.User.ID = id.String()
		c.ProfitSharePct = dec(ps).String()
		c.MinAllocation = s.fixed(decimal.Max(dec(mn), s.minAllocation))
		c.Aum = s.fixed(dec(aum))
		c.Accepting = listed && !s.paused && c.Copiers < min(maxCopiers, s.maxCopiersPerLeader)
		cards = append(cards, c)
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	series, err := loadSnapshots(ctx, s.d.Pool, ids, time.Now().AddDate(0, 0, -92))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range cards {
		st := computeStats(series[ids[i]], now)
		cards[i].Return30dPct, cards[i].Return90dPct, cards[i].MaxDrawdown90dPct, cards[i].HistoryDays =
			st.r30, st.r90, st.dd90, st.days
	}
	return cards, nil
}

func sortCards(cards []LeaderCard, by string) {
	num := func(p *string) decimal.Decimal {
		if p == nil {
			return decimal.NewFromInt(-1_000_000)
		}
		return dec(*p)
	}
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		switch by {
		case "copiers":
			if a.Copiers != b.Copiers {
				return a.Copiers > b.Copiers
			}
		case "aum":
			if !dec(a.Aum).Equal(dec(b.Aum)) {
				return dec(a.Aum).GreaterThan(dec(b.Aum))
			}
		case "return_30d":
			if !num(a.Return30dPct).Equal(num(b.Return30dPct)) {
				return num(a.Return30dPct).GreaterThan(num(b.Return30dPct))
			}
		default:
			if !num(a.Return90dPct).Equal(num(b.Return90dPct)) {
				return num(a.Return90dPct).GreaterThan(num(b.Return90dPct))
			}
		}
		return a.User.DisplayName < b.User.DisplayName
	})
}

// ---- statistics from daily snapshots ----

type snapPoint struct {
	day          time.Time
	value, flows decimal.Decimal // account value, net deposits
}

func loadSnapshots(ctx context.Context, q db.Querier, ids []uuid.UUID, since time.Time) (map[uuid.UUID][]snapPoint, error) {
	out := map[uuid.UUID][]snapPoint{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT user_id, day, account_value::text, deposits::text FROM copytrade_snapshots
		WHERE user_id = ANY($1) AND day >= $2::date ORDER BY user_id, day`, ids, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var p snapPoint
		var v, f string
		if err := rows.Scan(&id, &p.day, &v, &f); err != nil {
			return nil, err
		}
		p.value, p.flows = dec(v), dec(f)
		out[id] = append(out[id], p)
	}
	return out, rows.Err()
}

type leaderStats struct {
	r30, r90, dd90 *string
	days           int
}

// computeStats chains a time-weighted index from daily snapshots: each day's
// growth is the change in PnL (value − net deposits) over the previous day's
// value, so deposits and withdrawals never count as returns. Returns run from
// the last snapshot at or before the window's start (else the first one).
func computeStats(points []snapPoint, now time.Time) leaderStats {
	var st leaderStats
	if len(points) < 2 {
		return st
	}
	st.days = int(points[len(points)-1].day.Sub(points[0].day).Hours()/24) + 1
	idx := make([]decimal.Decimal, len(points))
	idx[0] = one
	for i := 1; i < len(points); i++ {
		prev, cur := points[i-1], points[i]
		idx[i] = idx[i-1]
		if prev.value.IsPositive() {
			gain := cur.value.Sub(cur.flows).Sub(prev.value.Sub(prev.flows))
			idx[i] = idx[i-1].Mul(one.Add(gain.Div(prev.value)))
		}
	}
	last := len(points) - 1
	ret := func(days int) *string {
		start := now.AddDate(0, 0, -days)
		base := 0
		for i, p := range points {
			if !p.day.After(start) {
				base = i
			}
		}
		if base == last || !idx[base].IsPositive() {
			return nil
		}
		v := idx[last].Div(idx[base]).Sub(one).Mul(d100).StringFixed(2)
		return &v
	}
	st.r30, st.r90 = ret(30), ret(90)
	peak, dd := idx[0], decimal.Zero
	for _, x := range idx {
		peak = decimal.Max(peak, x)
		if peak.IsPositive() {
			dd = decimal.Max(dd, peak.Sub(x).Div(peak))
		}
	}
	v := dd.Mul(d100).StringFixed(2)
	st.dd90 = &v
	return st
}

// ---- jobs (database only, once across instances) ----

// refreshLeaders re-checks every enabled leader's eligibility.
func (s *Service) refreshLeaders(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id FROM copytrade_leaders WHERE enabled`)
	if err != nil {
		return err
	}
	ids, err := collectUUIDs(rows)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ok, _, err := s.eligible(ctx, id)
		if err != nil {
			slog.WarnContext(ctx, "copytrade: eligibility check failed", "leader", id, "err", err)
			continue
		}
		if _, err := s.d.Pool.Exec(ctx, `UPDATE copytrade_leaders SET eligible = $2, checked_at = now() WHERE user_id = $1`, id, ok); err != nil {
			return err
		}
	}
	s.invalidateLeaders()
	return nil
}

// snapshot records today's main-account value and net deposits for everyone
// who has traded (leader statistics need history from before they opt in).
func (s *Service) snapshot(ctx context.Context) error {
	scores, err := exchange.Scores(ctx, s.d.Pool)
	if err != nil {
		return err
	}
	day := time.Now().UTC().Format("2006-01-02")
	for _, sc := range scores {
		if !sc.Traded {
			continue
		}
		if _, err := s.d.Pool.Exec(ctx, `INSERT INTO copytrade_snapshots (user_id, day, account_value, deposits) VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, day) DO UPDATE SET account_value = EXCLUDED.account_value, deposits = EXCLUDED.deposits, updated_at = now()`,
			sc.UserID, day, sc.AccountValue.String(), sc.Deposits.String()); err != nil {
			return err
		}
	}
	return nil
}

// prune drops copy events older than COPYTRADE_EVENT_RETENTION and snapshots older than 120 days.
func (s *Service) prune(ctx context.Context) error {
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM copytrade_events WHERE id IN (
		SELECT id FROM copytrade_events WHERE created_at < $1 LIMIT 50000)`, time.Now().Add(-s.eventRetention)); err != nil {
		return err
	}
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM copytrade_snapshots WHERE day < current_date - 120`)
	return err
}

type rowsLike interface {
	Next() bool
	Scan(...any) error
	Close()
	Err() error
}

func collectUUIDs(rows rowsLike) ([]uuid.UUID, error) {
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
