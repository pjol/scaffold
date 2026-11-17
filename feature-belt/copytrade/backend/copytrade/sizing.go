package copytrade

import (
	"fmt"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

// Target-position sizing. Pure, so every clip and skip is table-tested.

// PlanInput is everything one copy's order for one market is sized from.
type PlanInput struct {
	LeaderSz  decimal.Decimal // signed
	LeaderLev int             // the leader's leverage in this market
	LeaderAV  decimal.Decimal // the leader's account value
	CopySz    decimal.Decimal // signed
	CopyAV    decimal.Decimal
	CopyFree  decimal.Decimal

	Mark  decimal.Decimal
	RefPx decimal.Decimal // the leader's fill price (chase guard); zero = no guard

	SzDecimals   int
	MinNotional  decimal.Decimal
	MarketMaxLev int

	Settings Settings
}

// PlannedOrder is one market order to place.
type PlannedOrder struct {
	Side       string
	Sz         decimal.Decimal
	ReduceOnly bool
	Lev        int
}

// Plan is the sizing outcome: up to two orders (a flip is a reduce-only close
// followed by an open) and, when the target can't be (fully) reached, the
// skip class and reason recorded for it.
type Plan struct {
	Ratio  decimal.Decimal
	Target decimal.Decimal
	Lev    int
	Orders []PlannedOrder
	Skip   string // "" | SkipTooSmall | SkipRiskLimit | SkipSlippage | SkipRejected
	Reason string
	Notes  []string // clips applied to the placed orders
}

var (
	marginUse  = decimal.RequireFromString("0.95") // use ≤ 95 % of free × leverage for new exposure
	chaseGuard = decimal.RequireFromString("0.02") // skip opens once the mark is 2 % past the leader's fill
	one        = decimal.NewFromInt(1)
)

func sideOf(signed decimal.Decimal) string {
	if signed.IsNegative() {
		return engine.Sell
	}
	return engine.Buy
}

// Ratio is copy equity ÷ leader equity (zero when either is not positive).
func Ratio(copyAV, leaderAV decimal.Decimal) decimal.Decimal {
	if !leaderAV.IsPositive() || !copyAV.IsPositive() {
		return decimal.Zero
	}
	return copyAV.Div(leaderAV)
}

// Size computes the plan:
//
//   - target = trunc(leader size × copy equity ÷ leader equity, size step),
//     clipped to max_position_notional ÷ mark;
//   - leverage = min(the leader's, max_leverage, the market's max);
//   - shrinking toward the target is reduce-only, skipped below the market
//     minimum unless it closes the position (a close always goes); a flip is a
//     reduce-only close, then an open;
//   - an open or increase is held by the chase guard (mark more than 2 %
//     adverse to the leader's fill), clipped to max_trade_notional and to
//     margin (free × leverage × 0.95), and skipped below the minimum order.
func Size(in PlanInput) Plan {
	p := Plan{Ratio: Ratio(in.CopyAV, in.LeaderAV)}
	p.Lev = max(1, min(max(in.LeaderLev, 1), in.Settings.MaxLeverage, max(in.MarketMaxLev, 1)))
	if !in.Mark.IsPositive() {
		p.Skip, p.Reason = SkipRejected, "The market has no price."
		return p
	}
	target := in.LeaderSz.Mul(p.Ratio).Truncate(int32(in.SzDecimals))
	if maxPos := in.Settings.dec(in.Settings.MaxPositionNotional); maxPos.IsPositive() {
		lim := maxPos.Div(in.Mark).Truncate(int32(in.SzDecimals))
		if target.Abs().GreaterThan(lim) {
			target = lim.Mul(decimal.NewFromInt(int64(target.Sign())))
			p.Notes = append(p.Notes, fmt.Sprintf("target clipped to the %s position limit", maxPos))
		}
	}
	p.Target = target
	cur := in.CopySz
	if target.Equal(cur) {
		return p
	}
	flip := !cur.IsZero() && !target.IsZero() && cur.Sign() != target.Sign()
	shrink := !flip && (target.IsZero() || (target.Sign() == cur.Sign() && target.Abs().LessThan(cur.Abs())))
	switch {
	case shrink:
		sz := cur.Sub(target).Abs()
		if !target.IsZero() && sz.Mul(in.Mark).LessThan(in.MinNotional) {
			p.Skip = SkipTooSmall
			p.Reason = fmt.Sprintf("A reduction of %s is below the %s minimum order.", sz.Mul(in.Mark).StringFixed(2), in.MinNotional)
			return p
		}
		p.Orders = append(p.Orders, PlannedOrder{Side: sideOf(target.Sub(cur)), Sz: sz, ReduceOnly: true, Lev: p.Lev})
		return p
	case flip:
		p.Orders = append(p.Orders, PlannedOrder{Side: sideOf(cur.Neg()), Sz: cur.Abs(), ReduceOnly: true, Lev: p.Lev})
		cur = decimal.Zero
	}
	add := target.Sub(cur)
	side := sideOf(add)
	sz := add.Abs()
	if in.RefPx.IsPositive() {
		limit := in.RefPx.Mul(one.Add(chaseGuard))
		adverse := in.Mark.GreaterThan(limit)
		if side == engine.Sell {
			limit = in.RefPx.Mul(one.Sub(chaseGuard))
			adverse = in.Mark.LessThan(limit)
		}
		if adverse {
			p.Skip = SkipSlippage
			p.Reason = fmt.Sprintf("The mark (%s) is more than 2%% past the leader's fill at %s; not chasing.", in.Mark, in.RefPx)
			return p
		}
	}
	wanted := sz
	clipped := false
	if maxTrade := in.Settings.dec(in.Settings.MaxTradeNotional); maxTrade.IsPositive() {
		if lim := maxTrade.Div(in.Mark).Truncate(int32(in.SzDecimals)); sz.GreaterThan(lim) {
			sz, clipped = lim, true
			p.Notes = append(p.Notes, fmt.Sprintf("clipped to the %s per-trade limit", maxTrade))
		}
	}
	free := decimal.Max(decimal.Zero, in.CopyFree)
	if flip { // the close frees the position's margin first
		free = free.Add(in.CopySz.Abs().Mul(in.Mark).Div(decimal.NewFromInt(int64(p.Lev))))
	}
	if lim := free.Mul(decimal.NewFromInt(int64(p.Lev))).Mul(marginUse).Div(in.Mark).Truncate(int32(in.SzDecimals)); sz.GreaterThan(lim) {
		sz, clipped = lim, true
		p.Notes = append(p.Notes, "clipped to free margin")
	}
	notional := sz.Mul(in.Mark)
	if notional.LessThan(in.MinNotional) || !sz.IsPositive() {
		p.Skip = SkipTooSmall
		if clipped && wanted.Mul(in.Mark).GreaterThanOrEqual(in.MinNotional) {
			p.Skip = SkipRiskLimit
			p.Reason = fmt.Sprintf("Risk limits leave %s to add, below the %s minimum order.", notional.StringFixed(2), in.MinNotional)
		} else {
			p.Reason = fmt.Sprintf("The copied order of %s is below the %s minimum order.", wanted.Mul(in.Mark).StringFixed(2), in.MinNotional)
		}
		return p
	}
	p.Orders = append(p.Orders, PlannedOrder{Side: side, Sz: sz, Lev: p.Lev})
	return p
}
