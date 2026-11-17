package copytrade

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

// The pipeline's loops. They run only on the instance that runs the exchange
// engine (one at a time), so they need no lease:
//
//	reconcile   60 s   drift > max(10, 10 % of target) per engaged market, ≤ 3 tries per market an hour
//	monitor     15 s   stop loss, drawdown, unfinished stops; peaks and equity for the directory
//	leaders     1 min  leader disabled or gone (stop), ineligible (pause; resume when eligible)
//	settle      1 h    profit share of copies last settled ≥ COPYTRADE_SETTLE_INTERVAL ago
//	sweep       5 min  stopped copies that kept positions: close the account once it is flat
func (rt *runtime) loops(ctx context.Context) {
	rt.sweepOrphans(ctx)
	rec := time.NewTicker(time.Minute)
	mon := time.NewTicker(15 * time.Second)
	lead := time.NewTicker(time.Minute)
	settle := time.NewTicker(time.Hour)
	sweep := time.NewTicker(5 * time.Minute)
	defer rec.Stop()
	defer mon.Stop()
	defer lead.Stop()
	defer settle.Stop()
	defer sweep.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rec.C:
			rt.reconcile(ctx)
		case <-mon.C:
			rt.monitor(ctx)
		case <-lead.C:
			rt.syncLeaders(ctx)
		case <-settle.C:
			rt.settleDue(ctx)
		case <-sweep.C:
			rt.sweepStopped(ctx)
		}
	}
}

// reconcile corrects drift on every active copy: residuals of partial fills
// and rejections, fills the tap dropped, missed restarts.
func (rt *runtime) reconcile(ctx context.Context) {
	if rt.s.paused {
		return
	}
	for _, cs := range rt.openStates() {
		cs.mu.Lock()
		if cs.rec.Status == StatusActive {
			rt.record(ctx, cs, rt.syncAll(ctx, cs, EventReconcile, true))
		}
		cs.mu.Unlock()
	}
}

// monitor checks every open copy against its stops and finishes stops that
// are still closing positions. Stops run even while copy trading is paused.
func (rt *runtime) monitor(ctx context.Context) {
	for _, cs := range rt.openStates() {
		cs.mu.Lock()
		rt.monitorLocked(ctx, cs)
		cs.mu.Unlock()
	}
}

func (rt *runtime) monitorLocked(ctx context.Context, cs *copyState) {
	rec := &cs.rec
	if rec.Status == StatusStopping {
		_ = rt.stopLocked(ctx, cs, rec.StopReason, rec.CloseOnStop)
		return
	}
	if rec.Status != StatusActive && rec.Status != StatusPaused {
		return
	}
	st, err := rt.ex.AccountState(rec.AccountID)
	if err != nil {
		if err == engine.ErrNotFound {
			_ = rt.stopLocked(ctx, cs, StopAccountClosed, false)
		}
		return
	}
	if st.Status != engine.AccountActive {
		_ = rt.stopLocked(ctx, cs, StopAccountClosed, false)
		return
	}
	eq := st.AccountValue
	rec.Equity = eq
	if x, ok := pctOf(rec.Settings.StopLossPct); ok && rec.NetDeposits.IsPositive() && eq.LessThanOrEqual(rec.NetDeposits.Mul(one.Sub(x))) {
		_ = rt.stopLocked(ctx, cs, StopLoss, true)
		return
	}
	if eq.GreaterThan(rec.PeakEquity) {
		rec.PeakEquity = eq
	}
	if x, ok := pctOf(rec.Settings.MaxDrawdownPct); ok && rec.PeakEquity.IsPositive() && eq.LessThanOrEqual(rec.PeakEquity.Mul(one.Sub(x))) {
		_ = rt.stopLocked(ctx, cs, StopDrawdown, true)
		return
	}
	// write peak and equity when either moved by ≥ 1 % (or 1) since the last write
	step := func(base decimal.Decimal) decimal.Decimal {
		return decimal.Max(one, base.Abs().Mul(decimal.RequireFromString("0.01")))
	}
	if rec.PeakEquity.Sub(cs.persisted).GreaterThanOrEqual(step(cs.persisted)) || eq.Sub(cs.persistedEq).Abs().GreaterThanOrEqual(step(cs.persistedEq)) {
		rt.persist(ctx, cs)
	}
	rt.markFrame(rec.ID)
}

// syncLeaders applies leader changes made anywhere: disabled or deleted
// leaders' copies stop; ineligible leaders' copies pause and resume when the
// leader is eligible again.
func (rt *runtime) syncLeaders(ctx context.Context) {
	rt.syncRows(ctx)
	for _, leader := range rt.leadersWithCopies() {
		lr, ok, err := getLeader(ctx, rt.s.d.Pool, leader)
		if err != nil {
			continue
		}
		var status string
		_ = rt.s.d.Pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, leader).Scan(&status)
		switch {
		case status == "deleted" || !ok:
			rt.stopAllOf(ctx, leader, StopLeaderLeft)
			continue
		case !lr.Enabled:
			rt.stopAllOf(ctx, leader, StopLeaderDisabled)
			continue
		}
		for _, cs := range rt.copiesOf(leader) {
			cs.mu.Lock()
			rec := &cs.rec
			switch {
			case !lr.Eligible && rec.Status == StatusActive:
				rec.Status, rec.PauseReason = StatusPaused, PauseLeaderIneligible
				e := rt.newEvent(cs, EventPause, "")
				e.Reason = "Paused: the trader can't lead right now. The copy resumes when they can; stops still apply."
				rt.persist(ctx, cs)
				rt.record(ctx, cs, []eventRec{e})
			case lr.Eligible && rec.Status == StatusPaused && rec.PauseReason == PauseLeaderIneligible:
				rec.Status, rec.PauseReason = StatusActive, ""
				e := rt.newEvent(cs, EventResume, "")
				e.Reason = "Resumed: the trader can lead again."
				rt.persist(ctx, cs)
				rt.record(ctx, cs, []eventRec{e})
				rt.enqueueSync(cs)
			}
			cs.mu.Unlock()
		}
	}
}

// syncRows applies changes other instances made to open copies' rows: a row
// marked stopping (StopBetween) is stopped here, a deleted row is forgotten.
func (rt *runtime) syncRows(ctx context.Context) {
	asOf := time.Now()
	rows, err := rt.s.d.Pool.Query(ctx, `SELECT id, status, stop_reason, close_on_stop FROM copytrade_copies WHERE status <> 'stopped'`)
	if err != nil {
		return
	}
	type row struct {
		status, reason string
		closeIt        bool
	}
	stored := map[uuid.UUID]row{}
	for rows.Next() {
		var id uuid.UUID
		var r row
		if rows.Scan(&id, &r.status, &r.reason, &r.closeIt) == nil {
			stored[id] = r
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return
	}
	for _, cs := range rt.openStates() {
		cs.mu.Lock()
		r, ok := stored[cs.rec.ID]
		switch {
		case !ok && cs.rec.CreatedAt.Before(asOf): // (a copy started since the query isn't in it)
			cs.mu.Unlock()
			rt.unindex(cs.rec.ID, cs.rec.LeaderID)
			continue
		case r.status == StatusStopping && cs.rec.Status != StatusStopping:
			_ = rt.stopLocked(ctx, cs, r.reason, r.closeIt)
		}
		cs.mu.Unlock()
	}
}

// settleDue pays the profit share of every open copy last settled at least
// COPYTRADE_SETTLE_INTERVAL ago.
func (rt *runtime) settleDue(ctx context.Context) {
	due := time.Now().Add(-rt.s.settleEvery)
	for _, cs := range rt.openStates() {
		cs.mu.Lock()
		if (cs.rec.Status == StatusActive || cs.rec.Status == StatusPaused) && !cs.rec.LastSettledAt.After(due) {
			rt.settleLocked(ctx, cs)
		}
		cs.mu.Unlock()
	}
}

// sweepStopped closes the accounts of stopped copies that kept positions,
// once they are flat (closed by hand or liquidated): their balance goes back
// to the follower.
func (rt *runtime) sweepStopped(ctx context.Context) {
	recs, err := listCopies(ctx, rt.s.d.Pool, `WHERE status = 'stopped' AND account_closed_at IS NULL ORDER BY stopped_at LIMIT 200`)
	if err != nil {
		slog.WarnContext(ctx, "copytrade: listing stopped copies failed", "err", err)
		return
	}
	for _, rec := range recs {
		st, err := rt.ex.AccountState(rec.AccountID)
		if err == nil && (len(st.Positions) > 0 || st.Status != engine.AccountActive) {
			continue
		}
		if err == nil {
			if err := rt.ex.CloseSubaccount(rec.AccountID, 0); err != nil {
				continue
			}
			rec.NetDeposits = rec.NetDeposits.Sub(st.Balance)
		}
		now := time.Now()
		rec.AccountClosedAt, rec.Equity, rec.UpdatedAt = &now, decimal.Zero, now
		if err := updateCopy(ctx, rt.s.d.Pool, rec); err != nil {
			slog.WarnContext(ctx, "copytrade: saving a swept copy failed", "copy", rec.ID, "err", err)
		}
	}
}

// sweepOrphans closes copy sub-accounts left without a copy row (a crash
// between funding the account and recording the copy), returning their funds.
func (rt *runtime) sweepOrphans(ctx context.Context) {
	rows, err := rt.s.d.Pool.Query(ctx, `SELECT a.id::text FROM exchange_accounts a
		WHERE a.kind = 'sub' AND a.status = 'active' AND a.label LIKE 'copy:%'
		  AND a.created_at < now() - interval '1 minute'
		  AND NOT EXISTS (SELECT 1 FROM copytrade_copies c WHERE c.account_id = a.id)`)
	if err != nil {
		slog.WarnContext(ctx, "copytrade: orphan sweep failed", "err", err)
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if err := rt.ex.CloseSubaccount(id, 0); err != nil {
			slog.WarnContext(ctx, "copytrade: closing an orphaned copy account failed", "account", id, "err", err)
		}
	}
}

// forgetFollower drops a deleted follower's copies from memory (their rows
// are gone; the exchange closes the accounts).
func (rt *runtime) forgetFollower(follower uuid.UUID) {
	for _, cs := range rt.openStates() {
		cs.mu.Lock()
		hit, leader := cs.rec.FollowerID == follower, cs.rec.LeaderID
		cs.mu.Unlock()
		if hit {
			rt.unindex(cs.rec.ID, leader)
		}
	}
}
