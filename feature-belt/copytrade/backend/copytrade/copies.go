package copytrade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/notify"
)

// StartInput is POST /copytrade/copies.
type StartInput struct {
	LeaderID          string        `json:"leader_id"`
	Allocation        string        `json:"allocation"`
	Settings          SettingsPatch `json:"settings"` // absent fields take the defaults
	DisclosureVersion string        `json:"disclosure_version"`
}

// purpose names a copy's sub-account on the exchange.
func purpose(copyID uuid.UUID) string { return "copy:" + copyID.String() }

// StartCopy begins copying a leader: limits and disclosure, a sub-account
// funded from the follower's main account, and (copy_open_positions) the
// leader's current positions entered now.
func (rt *runtime) StartCopy(ctx context.Context, follower uuid.UUID, in StartInput) (Copy, error) {
	s := rt.s
	if s.paused {
		return Copy{}, errPaused
	}
	leaderID, err := uuid.Parse(in.LeaderID)
	if err != nil {
		return Copy{}, invalid("leader_id must be a user id.")
	}
	if leaderID == follower {
		return Copy{}, errSelfCopy
	}
	if in.DisclosureVersion != DisclosureVersion {
		return Copy{}, errDisclosure
	}
	alloc, err := decimal.NewFromString(in.Allocation)
	if err != nil {
		return Copy{}, invalid("allocation must be a decimal string.")
	}
	rt.startMu.Lock()
	defer rt.startMu.Unlock()
	lr, ok, err := getLeader(ctx, s.d.Pool, leaderID)
	if err != nil {
		return Copy{}, err
	}
	if !ok || !lr.Enabled || !lr.Eligible {
		return Copy{}, errNotAccepting
	}
	if okNow, _, err := s.eligible(ctx, leaderID); err != nil || !okNow {
		return Copy{}, errNotAccepting
	}
	leaderAcct, ok, err := rt.ex.MainAccount(leaderID)
	if err != nil {
		return Copy{}, err
	}
	if !ok {
		return Copy{}, errNotAccepting
	}
	var active int
	var already bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*), COALESCE(bool_or(leader_id = $2), false) FROM copytrade_copies
		WHERE follower_id = $1 AND status <> 'stopped'`, follower, leaderID).Scan(&active, &already); err != nil {
		return Copy{}, err
	}
	if already {
		return Copy{}, errAlreadyCopying
	}
	if active >= s.maxCopiesPerFollower {
		return Copy{}, errStatus(http.StatusUnprocessableEntity, "copy_limit",
			fmt.Sprintf("You already copy the most traders you can (%d).", s.maxCopiesPerFollower))
	}
	if n := len(rt.copiesOf(leaderID)); n >= min(lr.MaxCopiers, s.maxCopiersPerLeader) {
		return Copy{}, errLeaderFull
	}
	_, dp := rt.ex.Quote()
	alloc = alloc.Round(dp)
	minAlloc := decimal.Max(s.minAllocation, lr.MinAlloc)
	if alloc.LessThan(minAlloc) {
		return Copy{}, invalid("allocation must be at least %s for this trader.", s.amount(minAlloc))
	}
	main, err := rt.ex.UserAccountState(follower)
	if err != nil {
		return Copy{}, apiErr(err)
	}
	if alloc.GreaterThan(main.FreeCollateral) {
		return Copy{}, errStatus(http.StatusUnprocessableEntity, "insufficient_collateral",
			fmt.Sprintf("Only %s of free collateral can be allocated.", s.amount(main.FreeCollateral.Truncate(dp))))
	}
	settings, err := in.Settings.Apply(DefaultSettings(alloc))
	if err != nil {
		return Copy{}, apiErr(err)
	}

	id := uuid.Must(uuid.NewV7())
	acct, err := rt.ex.OpenSubaccount(follower, purpose(id))
	if err != nil {
		return Copy{}, apiErr(err)
	}
	leader := person(ctx, s.d.Pool, leaderID)
	if err := rt.ex.Transfer(main.ID, acct, alloc, exchange.TransferInternal, "copy "+leader.DisplayName); err != nil {
		_ = rt.ex.CloseSubaccount(acct, 0)
		return Copy{}, apiErr(err)
	}
	now := time.Now()
	rec := copyRec{ID: id, FollowerID: follower, LeaderID: leaderID, AccountID: acct, Status: StatusActive, Settings: settings,
		NetDeposits: alloc, GrossDeposits: alloc, PeakEquity: alloc, Equity: alloc, ProfitShare: lr.ProfitSharePct.Div(d100),
		LastSettledAt: now, Ignored: map[string]int{}, CloseOnStop: true, IdentityVisible: lr.ShowCopiers,
		DisclosureVersion: in.DisclosureVersion, DisclosureAt: now, CreatedAt: now, UpdatedAt: now}
	if !settings.CopyOpenPositions {
		if st, err := rt.ex.AccountState(leaderAcct); err == nil {
			for sym, p := range st.Positions {
				rec.Ignored[sym] = p.Sz.Sign()
			}
		}
	}
	if err := insertCopy(ctx, s.d.Pool, rec); err != nil {
		if cerr := rt.ex.CloseSubaccount(acct, 0); cerr != nil { // sweeps the allocation back
			slog.ErrorContext(ctx, "copytrade: returning an allocation failed", "account", acct, "err", cerr)
		}
		if errors.Is(err, errActivePair) {
			return Copy{}, errAlreadyCopying
		}
		return Copy{}, err
	}
	cs := &copyState{rec: rec, leaderAcct: leaderAcct, retries: map[string][]time.Time{}, persisted: alloc, persistedEq: alloc}
	rt.index(cs)
	cs.mu.Lock()
	start := rt.newEvent(cs, EventStart, "")
	start.Reason = fmt.Sprintf("Started copying %s with %s (%s accepted).", leader.DisplayName, s.amount(alloc), in.DisclosureVersion)
	moved := rt.newEvent(cs, EventTransfer, "")
	moved.Notional, moved.Reason = decPtr(alloc), fmt.Sprintf("Moved %s from your main account.", s.amount(alloc))
	rt.record(ctx, cs, []eventRec{start, moved})
	cs.mu.Unlock()
	s.notify(ctx, notify.Notification{UserID: follower, Kind: KindCopyStarted, Title: "Copy started",
		Body: fmt.Sprintf("You're copying %s with %s.", leader.DisplayName, s.amount(alloc)), Link: "/copy/" + id.String(),
		Data: map[string]any{"copy_id": id.String()}})
	s.notify(ctx, notify.Notification{UserID: leaderID, Kind: KindCopierJoined, Title: "New copier",
		Body: "Someone started copying your trades.", Link: "/settings/copy-trading"})
	if settings.CopyOpenPositions {
		rt.enqueueSync(cs)
	}
	s.invalidateLeaders()
	return rt.viewState(ctx, cs)
}

// Stop stops a copy: open orders cancelled, positions closed when asked,
// profit share settled and funds returned to the follower's main account. A
// close that can't finish (no liquidity) leaves it stopping; the monitor
// retries.
func (rt *runtime) Stop(ctx context.Context, cs *copyState, reason string, closePositions bool) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return rt.stopLocked(ctx, cs, reason, closePositions)
}

func (rt *runtime) stopLocked(ctx context.Context, cs *copyState, reason string, closePositions bool) error {
	rec := &cs.rec
	if rec.Status == StatusStopped {
		return nil
	}
	if rec.Status != StatusStopping {
		rec.Status, rec.StopReason, rec.PauseReason, rec.CloseOnStop = StatusStopping, reason, "", closePositions
		rt.persist(ctx, cs)
		e := rt.newEvent(cs, EventStop, "")
		e.Reason = stopText(reason, closePositions)
		rt.record(ctx, cs, []eventRec{e})
	}
	if _, err := rt.ex.CancelAllAs(ctx, rec.AccountID, ""); err != nil && !errors.Is(err, engine.ErrNotFound) {
		slog.WarnContext(ctx, "copytrade: cancel on stop failed", "copy", rec.ID, "err", err)
	}
	st, err := rt.ex.AccountState(rec.AccountID)
	if err != nil {
		if errors.Is(err, engine.ErrNotFound) { // the sub-account is gone (closed elsewhere)
			return rt.finishStopLocked(ctx, cs, st, false)
		}
		return err
	}
	if rec.CloseOnStop && len(st.Positions) > 0 {
		var evs []eventRec
		for sym := range st.Positions {
			e := rt.newEvent(cs, EventOrder, sym)
			ov, fills, err := rt.ex.ClosePositionAs(ctx, rec.AccountID, sym, "",
				exchange.PlaceOpts{Source: engine.SourceCopy, ClientRef: e.ID.String(), AvoidMakers: []string{cs.leaderAcct}})
			e.At = time.Now()
			if err != nil {
				code, msg := errCode(err)
				e.Kind, e.SkipClass = EventSkip, SkipRejected
				e.Reason = fmt.Sprintf("Closing on stop was rejected (%s): %s", code, msg)
			} else {
				filled, notional := fillTotals(fills)
				e.Side, e.Sz, e.Notional, e.OrderID = ov.Side, decPtr(filled), decPtr(notional.Round(2)), ov.ID
				e.Reason = "Closed on stop."
			}
			evs = append(evs, e)
		}
		rt.record(ctx, cs, evs)
		if st, err = rt.ex.AccountState(rec.AccountID); err != nil || len(st.Positions) > 0 {
			return nil // still stopping: the monitor retries
		}
	}
	return rt.finishStopLocked(ctx, cs, st, true)
}

func stopText(reason string, closed bool) string {
	what := map[string]string{
		StopUser: "You stopped the copy", StopLoss: "Stop loss reached", StopDrawdown: "Drawdown limit reached",
		StopLeaderDisabled: "The trader stopped offering copies", StopLeaderLeft: "The trader left",
		StopStaff: "Stopped by the team", StopAccountClosed: "The copy's account was closed",
		StopBlocked: "You and the trader can no longer interact",
	}[reason]
	if what == "" {
		what = "Stopped"
	}
	if closed {
		return what + ": closing positions and returning funds."
	}
	return what + ": positions kept; free funds returned."
}

func (rt *runtime) finishStopLocked(ctx context.Context, cs *copyState, st exchange.AccountState, live bool) error {
	rec := &cs.rec
	var evs []eventRec
	if live {
		rt.settleLocked(ctx, cs)
		if st2, err := rt.ex.AccountState(rec.AccountID); err == nil {
			st = st2
		}
		rec.Equity = st.AccountValue
		if len(st.Positions) == 0 {
			// flat: sweep everything back and close the sub-account
			if err := rt.ex.CloseSubaccount(rec.AccountID, 0); err != nil {
				slog.WarnContext(ctx, "copytrade: closing a copy account failed", "copy", rec.ID, "err", err)
			} else {
				now := time.Now()
				rec.AccountClosedAt, rec.Equity = &now, decimal.Zero
				if back := st.Balance; back.IsPositive() {
					rec.NetDeposits = rec.NetDeposits.Sub(back)
					e := rt.newEvent(cs, EventTransfer, "")
					e.Notional, e.Reason = decPtr(back.Neg()), fmt.Sprintf("Returned %s to your main account.", rt.s.amount(back))
					evs = append(evs, e)
				}
			}
		} else if free := st.FreeCollateral.Truncate(2); free.IsPositive() {
			if main, ok, err := rt.ex.MainAccount(rec.FollowerID); err == nil && ok {
				if err := rt.ex.Transfer(rec.AccountID, main, free, exchange.TransferInternal, "copy stopped"); err == nil {
					rec.NetDeposits, rec.Equity = rec.NetDeposits.Sub(free), rec.Equity.Sub(free)
					e := rt.newEvent(cs, EventTransfer, "")
					e.Notional, e.Reason = decPtr(free.Neg()), fmt.Sprintf("Returned %s of free funds to your main account.", rt.s.amount(free))
					evs = append(evs, e)
				}
			}
		}
	}
	now := time.Now()
	rec.Status, rec.StoppedAt = StatusStopped, &now
	rt.persist(ctx, cs)
	rt.record(ctx, cs, evs)
	rt.unindex(rec.ID, rec.LeaderID)
	leader := rt.leaderName(ctx, cs)
	rt.s.notify(ctx, notify.Notification{UserID: rec.FollowerID, Kind: KindCopyStopped, Title: "Copy stopped",
		Body: fmt.Sprintf("Your copy of %s stopped. %s", leader, stopText(rec.StopReason, rec.CloseOnStop)), Link: "/copy/" + rec.ID.String(),
		Data: map[string]any{"copy_id": rec.ID.String(), "reason": rec.StopReason}})
	rt.s.notify(ctx, notify.Notification{UserID: rec.LeaderID, Kind: KindCopierLeft, Title: "A copier left",
		Body: "Someone stopped copying your trades.", Link: "/settings/copy-trading"})
	rt.s.invalidateLeaders()
	return nil
}

// settleLocked pays the leader's profit share above the high-water mark.
func (rt *runtime) settleLocked(ctx context.Context, cs *copyState) {
	rec := &cs.rec
	st, err := rt.ex.AccountState(rec.AccountID)
	if err != nil {
		return
	}
	set := SettleFee(rec.ProfitShare, st.AccountValue, rec.NetDeposits, rec.HWMGain, st.FreeCollateral)
	now := time.Now()
	if set.Fee.IsPositive() {
		leaderAcct, ok, err := rt.ex.MainAccount(rec.LeaderID)
		if err != nil || !ok {
			return
		}
		if err := rt.ex.Transfer(rec.AccountID, leaderAcct, set.Fee, exchange.TransferProfitShare, "copy "+rec.ID.String()); err != nil {
			slog.WarnContext(ctx, "copytrade: profit share transfer failed", "copy", rec.ID, "err", err)
			return
		}
		rec.ProfitSharePaid = rec.ProfitSharePaid.Add(set.Fee)
		// A share is a flow, not a loss: take it off the drawdown peak as a
		// withdrawal would, so paying it never trips max_drawdown_pct.
		after := st.AccountValue.Sub(set.Fee)
		rec.PeakEquity = decimal.Max(after, rec.PeakEquity.Sub(set.Fee))
		rec.Equity = after
		e := rt.newEvent(cs, EventProfitShare, "")
		e.Notional = decPtr(set.Fee)
		e.Reason = fmt.Sprintf("Paid a %s%% profit share of %s on %s gained above the high-water mark.",
			rec.ProfitShare.Mul(d100).String(), rt.s.amount(set.Fee), rt.s.amount(set.Gain.Sub(rec.HWMGain)))
		rt.record(ctx, cs, []eventRec{e})
		rt.s.notify(ctx, notify.Notification{UserID: rec.LeaderID, Kind: KindProfitShare, Title: "Profit share received",
			Body: fmt.Sprintf("You received %s from a copier.", rt.s.amount(set.Fee)), Link: "/settings/copy-trading"})
	}
	rec.HWMGain, rec.LastSettledAt = set.HWM, now
	rt.persist(ctx, cs)
}

// Funds adds (amount > 0) or withdraws (amount < 0). A withdrawal settles
// the profit share first and is limited to free collateral.
func (rt *runtime) Funds(ctx context.Context, cs *copyState, amount decimal.Decimal) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	rec := &cs.rec
	_, dp := rt.ex.Quote()
	amount = amount.Round(dp)
	if amount.IsZero() {
		return invalid("amount must not be zero.")
	}
	if rec.Status != StatusActive && rec.Status != StatusPaused {
		return errCopyStopped
	}
	main, ok, err := rt.ex.MainAccount(rec.FollowerID)
	if err != nil {
		return err
	}
	if !ok {
		return apiErr(engine.ErrNoAccount)
	}
	e := rt.newEvent(cs, EventTransfer, "")
	if amount.IsPositive() {
		if err := rt.ex.Transfer(main, rec.AccountID, amount, exchange.TransferInternal, "copy funds"); err != nil {
			return apiErr(err)
		}
		rec.NetDeposits = rec.NetDeposits.Add(amount)
		rec.GrossDeposits = rec.GrossDeposits.Add(amount)
		rec.PeakEquity = rec.PeakEquity.Add(amount)
		e.Reason = fmt.Sprintf("Added %s from your main account.", rt.s.amount(amount))
	} else {
		out := amount.Neg()
		rt.settleLocked(ctx, cs)
		if err := rt.ex.Transfer(rec.AccountID, main, out, exchange.TransferInternal, "copy withdrawal"); err != nil {
			return apiErr(err)
		}
		rec.NetDeposits = rec.NetDeposits.Sub(out)
		if st, err := rt.ex.AccountState(rec.AccountID); err == nil {
			rec.PeakEquity = decimal.Max(st.AccountValue, rec.PeakEquity.Sub(out))
		}
		e.Reason = fmt.Sprintf("Withdrew %s to your main account.", rt.s.amount(out))
	}
	e.Notional = decPtr(amount)
	if st, err := rt.ex.AccountState(rec.AccountID); err == nil {
		rec.Equity = st.AccountValue
	}
	rt.persist(ctx, cs)
	rt.record(ctx, cs, []eventRec{e})
	return nil
}

// Patch changes settings and/or pauses or resumes. A resume brings every
// engaged market back to target.
func (rt *runtime) Patch(ctx context.Context, cs *copyState, settings *SettingsPatch, status string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	rec := &cs.rec
	if rec.Status == StatusStopped || rec.Status == StatusStopping {
		return errCopyStopped
	}
	var evs []eventRec
	if settings != nil {
		next, err := settings.Apply(rec.Settings)
		if err != nil {
			return apiErr(err)
		}
		rec.Settings = next
		e := rt.newEvent(cs, EventSettings, "")
		e.Reason = "You changed the copy's settings."
		evs = append(evs, e)
	}
	resume := false
	switch status {
	case "":
	case StatusPaused:
		if rec.Status == StatusActive {
			rec.Status, rec.PauseReason = StatusPaused, PauseUser
			e := rt.newEvent(cs, EventPause, "")
			e.Reason = "You paused the copy: new leader trades aren't copied; stops still apply."
			evs = append(evs, e)
		}
	case StatusActive:
		if rec.Status == StatusPaused {
			if rec.PauseReason == PauseLeaderIneligible {
				return errLeaderPause
			}
			if rt.s.paused {
				return errPaused
			}
			rec.Status, rec.PauseReason, resume = StatusActive, "", true
			e := rt.newEvent(cs, EventResume, "")
			e.Reason = "You resumed the copy: positions move back to the leader's."
			evs = append(evs, e)
		}
	default:
		return invalid(`status must be "active" or "paused".`)
	}
	rt.persist(ctx, cs)
	rt.record(ctx, cs, evs)
	if resume {
		go rt.enqueueSync(cs)
	}
	return nil
}

// ClosePosition closes (part of) a copy's position by hand. The market then
// stays out of mirroring until the leader goes flat or flips, so reconcile
// doesn't reopen it. On a stopped copy, a flat account is swept and closed.
func (rt *runtime) ClosePosition(ctx context.Context, cs *copyState, symbol, sz string) (engine.OrderView, []engine.Fill, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	rec := &cs.rec
	e := rt.newEvent(cs, EventOrder, symbol)
	ov, fills, err := rt.ex.ClosePositionAs(ctx, rec.AccountID, symbol, sz,
		exchange.PlaceOpts{Source: engine.SourceUser, ClientRef: e.ID.String(), AvoidMakers: []string{cs.leaderAcct}})
	if err != nil {
		return ov, fills, apiErr(err)
	}
	if rec.Status != StatusStopped && cs.leaderAcct != "" {
		if st, err := rt.ex.AccountState(cs.leaderAcct); err == nil {
			if lp, ok := st.Positions[ov.Symbol]; ok {
				rec.Ignored[ov.Symbol] = lp.Sz.Sign()
			}
		}
	}
	filled, notional := fillTotals(fills)
	e.Symbol, e.Side, e.Sz, e.Notional, e.OrderID, e.At = ov.Symbol, ov.Side, decPtr(filled), decPtr(notional.Round(2)), ov.ID, time.Now()
	e.Reason = "You closed this position; it isn't mirrored again until the trader goes flat or flips."
	evs := []eventRec{e}
	if rec.Status == StatusStopped && rec.AccountClosedAt == nil {
		if st, err := rt.ex.AccountState(rec.AccountID); err == nil && len(st.Positions) == 0 {
			if err := rt.ex.CloseSubaccount(rec.AccountID, 0); err == nil {
				now := time.Now()
				rec.AccountClosedAt, rec.Equity = &now, decimal.Zero
				rec.NetDeposits = rec.NetDeposits.Sub(st.Balance)
				t := rt.newEvent(cs, EventTransfer, "")
				t.Notional, t.Reason = decPtr(st.Balance.Neg()), fmt.Sprintf("Returned %s to your main account.", rt.s.amount(st.Balance))
				evs = append(evs, t)
			}
		}
	}
	rt.persist(ctx, cs)
	rt.record(ctx, cs, evs)
	return ov, fills, nil
}

// ---- views ----

func (rt *runtime) viewState(ctx context.Context, cs *copyState) (Copy, error) {
	cs.mu.Lock()
	rec := cs.rec.clone()
	cs.mu.Unlock()
	return rt.view(ctx, rec, nil)
}

func (rt *runtime) view(ctx context.Context, rec copyRec, sum *engine.AccountSummary) (Copy, error) {
	if sum == nil && rec.AccountClosedAt == nil {
		if x, err := rt.ex.Summary(rec.AccountID); err == nil {
			sum = &x
		}
	}
	eq := rec.Equity
	if sum != nil {
		eq = dec(sum.AccountValue)
		sum.FaucetAvailableAt = nil
	}
	pnl := eq.Sub(rec.NetDeposits)
	pnlPct := decimal.Zero
	if rec.GrossDeposits.IsPositive() { // after withdrawals the net can be tiny: the % is of what went in
		pnlPct = pnl.Div(rec.GrossDeposits).Mul(d100)
	}
	ratio := decimal.Zero
	if rec.Status != StatusStopped {
		if l, err := rt.ex.UserAccountState(rec.LeaderID); err == nil {
			ratio = Ratio(eq, l.AccountValue)
		}
	}
	skips, err := skipCounts(ctx, rt.s.d.Pool, rec.ID, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		return Copy{}, err
	}
	peak := decimal.Max(rec.PeakEquity, eq)
	if rec.Status == StatusStopped {
		peak = rec.PeakEquity
	}
	_, dp := rt.ex.Quote()
	return Copy{ID: rec.ID.String(), Leader: person(ctx, rt.s.d.Pool, rec.LeaderID), Status: rec.Status, StopReason: rec.StopReason,
		PauseReason: rec.PauseReason, Allocation: rec.NetDeposits.StringFixed(dp), Equity: eq.StringFixed(dp),
		Pnl: pnl.StringFixed(dp), PnlPct: pnlPct.StringFixed(2), PeakEquity: peak.StringFixed(dp), Ratio: ratio.StringFixed(4),
		Settings: rec.Settings, Account: sum, ProfitSharePct: rec.ProfitShare.Mul(d100).String(),
		ProfitSharePaid: rec.ProfitSharePaid.StringFixed(dp), Skipped30d: skips, IdentityVisible: rec.IdentityVisible,
		CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt, StoppedAt: rec.StoppedAt}, nil
}

// copyFor returns the follower's copy (open copies from memory, stopped ones
// from the database); another person's copy is not found.
func (rt *runtime) copyFor(ctx context.Context, follower uuid.UUID, rawID string) (*copyState, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, errNotFound
	}
	if cs, ok := rt.state(id); ok {
		cs.mu.Lock()
		f := cs.rec.FollowerID
		cs.mu.Unlock()
		if f != follower {
			return nil, errNotFound
		}
		return cs, nil
	}
	rec, ok, err := getCopy(ctx, rt.s.d.Pool, id)
	if err != nil {
		return nil, err
	}
	if !ok || rec.FollowerID != follower {
		return nil, errNotFound
	}
	acct, _, _ := rt.ex.MainAccount(rec.LeaderID)
	return &copyState{rec: rec, leaderAcct: acct, retries: map[string][]time.Time{}}, nil
}
