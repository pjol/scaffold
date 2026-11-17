package copytrade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
	"scaffoldapp/backend/internal/notify"
)

type mirrorIn struct {
	symbol    string
	leaderSz  decimal.Decimal
	leaderLev int
	leaderAV  decimal.Decimal
	refPx     decimal.Decimal
	mkt       marketParams
	fillID    string
	fillAt    time.Time
	kind      string // EventOrder (fan-out) | EventReconcile
}

// engagedLocked applies the engaged-market rule: a position the leader held
// when the copy started (copy_open_positions off) is ignored until it goes
// flat (the next open engages) or flips (which engages at once). Caller
// holds cs.mu.
func (rt *runtime) engagedLocked(ctx context.Context, cs *copyState, symbol string, leaderSz decimal.Decimal) bool {
	sign, ignored := cs.rec.Ignored[symbol]
	if !ignored {
		return true
	}
	switch {
	case leaderSz.IsZero():
		delete(cs.rec.Ignored, symbol)
		rt.persist(ctx, cs)
		return false
	case leaderSz.Sign() != sign:
		delete(cs.rec.Ignored, symbol)
		rt.persist(ctx, cs)
		return true
	}
	return false
}

// mirror sizes and places this copy's order(s) for one market. Caller holds cs.mu.
func (rt *runtime) mirror(ctx context.Context, cs *copyState, in mirrorIn) []eventRec {
	if !rt.engagedLocked(ctx, cs, in.symbol, in.leaderSz) {
		return nil
	}
	if !in.mkt.ok {
		if in.mkt = rt.market(in.symbol); !in.mkt.ok {
			return nil
		}
	}
	st, err := rt.ex.AccountState(cs.rec.AccountID)
	if err != nil {
		return nil
	}
	cur := st.Positions[in.symbol].Sz
	plan := Size(PlanInput{LeaderSz: in.leaderSz, LeaderLev: in.leaderLev, LeaderAV: in.leaderAV, CopySz: cur,
		CopyAV: st.AccountValue, CopyFree: st.FreeCollateral, Mark: in.mkt.mark, RefPx: in.refPx, SzDecimals: in.mkt.szDec,
		MinNotional: in.mkt.minNotional, MarketMaxLev: in.mkt.maxLev, Settings: cs.rec.Settings})
	return rt.execute(ctx, cs, in, plan, cur)
}

func (rt *runtime) newEvent(cs *copyState, kind, symbol string) eventRec {
	return eventRec{ID: uuid.Must(uuid.NewV7()), CopyID: cs.rec.ID, Kind: kind, Symbol: symbol, At: time.Now()}
}

func (rt *runtime) execute(ctx context.Context, cs *copyState, in mirrorIn, plan Plan, cur decimal.Decimal) []eventRec {
	rec := &cs.rec
	var evs []eventRec
	for _, o := range plan.Orders {
		now := time.Now()
		if !o.ReduceOnly && !rt.allowOrderLocked(cs, now) {
			e := rt.newEvent(cs, EventSkip, in.symbol)
			e.Side, e.Sz, e.SkipClass = o.Side, decPtr(o.Sz), SkipRiskLimit
			e.Reason = fmt.Sprintf("Rate limit: at most %d copied orders a minute.", ordersPerMinute)
			return append(evs, e)
		}
		e := rt.newEvent(cs, in.kind, in.symbol)
		ov, fills, err := rt.ex.PlaceOrderAs(ctx, rec.AccountID, engine.PlaceOrder{Symbol: in.symbol, Side: o.Side,
			Type: engine.TypeMarket, Sz: o.Sz.String(), Leverage: o.Lev, ReduceOnly: o.ReduceOnly, SlippageBps: rec.Settings.SlippageBps},
			exchange.PlaceOpts{Source: engine.SourceCopy, ClientRef: e.ID.String(), AvoidMakers: []string{cs.leaderAcct}})
		done := time.Now()
		e.At, e.Side, e.LeaderFillID = done, o.Side, in.fillID
		if !in.fillAt.IsZero() {
			lat := max(0, done.Sub(in.fillAt).Milliseconds())
			e.LatencyMs = &lat
		}
		cs.orderTimes = append(cs.orderTimes, done)
		if err != nil {
			code, msg := errCode(err)
			e.Kind, e.SkipClass, e.Sz = EventSkip, SkipRejected, decPtr(o.Sz)
			e.Reason = fmt.Sprintf("Order rejected (%s): %s", code, msg)
			if in.kind == EventReconcile {
				e.Reason = "Reconciling: " + e.Reason
			}
			evs = append(evs, e)
			cs.failures++
			if cs.failures == 3 {
				rt.s.notify(ctx, notify.Notification{UserID: rec.FollowerID, Kind: KindOrdersFailing, Title: "Copied orders are failing",
					Body: fmt.Sprintf("The last 3 orders copying %s were rejected (%s).", rt.leaderName(ctx, cs), code),
					Link: "/copy/" + rec.ID.String(), Data: map[string]any{"copy_id": rec.ID.String()}})
			}
			return evs // a failed close never opens the other way
		}
		cs.failures = 0
		filled, notional := fillTotals(fills)
		e.Sz, e.Notional, e.OrderID = decPtr(filled), decPtr(notional.Round(2)), ov.ID
		var why []string
		switch {
		case in.kind == EventReconcile:
			why = append(why, "Reconciled with the leader")
		case o.ReduceOnly && plan.Target.IsZero():
			why = append(why, "The leader closed: closed")
		case o.ReduceOnly:
			why = append(why, "The leader reduced: reduced")
		default:
			why = append(why, "Mirrored the leader")
		}
		why[0] += fmt.Sprintf(" (target %s %s, ratio %s)", plan.Target, in.symbol, plan.Ratio.StringFixed(4))
		if filled.LessThan(o.Sz) {
			why = append(why, fmt.Sprintf("filled %s of %s (thin book); reconcile corrects the gap", filled, o.Sz))
		}
		if !o.ReduceOnly {
			why = append(why, plan.Notes...)
		}
		e.Reason = strings.Join(why, "; ") + "."
		evs = append(evs, e)
	}
	if plan.Skip != "" {
		e := rt.newEvent(cs, EventSkip, in.symbol)
		e.SkipClass, e.Reason = plan.Skip, plan.Reason
		e.Side = sideOf(plan.Target.Sub(cur))
		evs = append(evs, e)
	}
	return evs
}

func fillTotals(fills []engine.Fill) (decimal.Decimal, decimal.Decimal) {
	filled, notional := decimal.Zero, decimal.Zero
	for _, f := range fills {
		px, sz := dec(f.Px), dec(f.Sz)
		filled, notional = filled.Add(sz), notional.Add(px.Mul(sz))
	}
	return filled, notional
}

// allowOrderLocked keeps a copy at ≤ ordersPerMinute opening orders a minute.
func (rt *runtime) allowOrderLocked(cs *copyState, now time.Time) bool {
	keep := cs.orderTimes[:0]
	for _, t := range cs.orderTimes {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	cs.orderTimes = keep
	return len(keep) < ordersPerMinute
}

// syncAll brings every engaged market to its target (a start with
// copy_open_positions, a resume, reconcile). With drift, only markets whose
// gap exceeds max(10, 10 % of the target) are touched, at most 3 times per
// market an hour. Caller holds cs.mu.
func (rt *runtime) syncAll(ctx context.Context, cs *copyState, kind string, drift bool) []eventRec {
	rec := &cs.rec
	leader, err := rt.ex.AccountState(cs.leaderAcct)
	if err != nil {
		return nil
	}
	own, err := rt.ex.AccountState(rec.AccountID)
	if err != nil {
		return nil
	}
	syms := map[string]bool{}
	for sym := range leader.Positions {
		syms[sym] = true
	}
	for sym := range own.Positions {
		syms[sym] = true
	}
	ordered := make([]string, 0, len(syms))
	for sym := range syms {
		ordered = append(ordered, sym)
	}
	sort.Strings(ordered)
	var evs []eventRec
	now := time.Now()
	for _, sym := range ordered {
		lp := leader.Positions[sym]
		if !rt.engagedLocked(ctx, cs, sym, lp.Sz) {
			continue
		}
		mkt := rt.market(sym)
		if !mkt.ok {
			continue
		}
		if drift {
			target := lp.Sz.Mul(Ratio(own.AccountValue, leader.AccountValue)).Truncate(int32(mkt.szDec))
			if maxPos := rec.Settings.dec(rec.Settings.MaxPositionNotional); maxPos.IsPositive() && mkt.mark.IsPositive() {
				if lim := maxPos.Div(mkt.mark).Truncate(int32(mkt.szDec)); target.Abs().GreaterThan(lim) {
					target = lim.Mul(decimal.NewFromInt(int64(target.Sign())))
				}
			}
			gap := target.Sub(own.Positions[sym].Sz).Abs().Mul(mkt.mark)
			threshold := decimal.Max(d10, target.Abs().Mul(mkt.mark).Mul(decimal.RequireFromString("0.10")))
			if !gap.GreaterThan(threshold) || !rt.retryAllowedLocked(cs, sym, now) {
				continue
			}
		}
		ref := rt.leaderPx(cs.leaderAcct, sym)
		if !ref.IsPositive() {
			ref = lp.EntryPx // no recent fill: chase against the leader's entry
		}
		evs = append(evs, rt.mirror(ctx, cs, mirrorIn{symbol: sym, leaderSz: lp.Sz, leaderLev: lp.Leverage,
			leaderAV: leader.AccountValue, refPx: ref, mkt: mkt, kind: kind})...)
	}
	return evs
}

func (rt *runtime) retryAllowedLocked(cs *copyState, sym string, now time.Time) bool {
	keep := cs.retries[sym][:0]
	for _, t := range cs.retries[sym] {
		if now.Sub(t) < time.Hour {
			keep = append(keep, t)
		}
	}
	if len(keep) >= 3 {
		cs.retries[sym] = keep
		return false
	}
	cs.retries[sym] = append(keep, now)
	return true
}

// record stores events and sends them to the follower.
func (rt *runtime) record(ctx context.Context, cs *copyState, evs []eventRec) {
	if len(evs) == 0 {
		return
	}
	if err := insertEvents(ctx, rt.s.d.Pool, evs); err != nil {
		slog.WarnContext(ctx, "copytrade: storing events failed", "copy", cs.rec.ID, "err", err)
	}
	for _, e := range evs {
		rt.s.publish(ctx, cs.rec.FollowerID, EventActivity, eventView(e))
	}
	rt.markFrame(cs.rec.ID)
}

// persist writes the copy row. A row that is gone (the follower deleted their
// account) drops the copy from memory.
func (rt *runtime) persist(ctx context.Context, cs *copyState) {
	cs.rec.UpdatedAt = time.Now()
	cs.persisted, cs.persistedEq = cs.rec.PeakEquity, cs.rec.Equity
	err := updateCopy(ctx, rt.s.d.Pool, cs.rec)
	if errors.Is(err, errRowGone) {
		rt.unindex(cs.rec.ID, cs.rec.LeaderID)
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "copytrade: saving a copy failed", "copy", cs.rec.ID, "err", err)
	}
}

func (rt *runtime) leaderName(ctx context.Context, cs *copyState) string {
	return person(ctx, rt.s.d.Pool, cs.rec.LeaderID).DisplayName
}

func eventView(e eventRec) Event {
	v := Event{ID: e.ID.String(), CopyID: e.CopyID.String(), Kind: e.Kind, Symbol: e.Symbol, Side: e.Side, SkipClass: e.SkipClass,
		Reason: e.Reason, LatencyMs: e.LatencyMs, CreatedAt: e.At}
	if e.Sz != nil {
		s := e.Sz.String()
		v.Sz = &s
	}
	if e.Notional != nil {
		s := e.Notional.StringFixed(2)
		v.Notional = &s
	}
	if e.OrderID != "" {
		s := e.OrderID
		v.OrderID = &s
	}
	return v
}
