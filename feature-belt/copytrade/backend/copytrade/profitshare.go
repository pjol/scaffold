package copytrade

import "github.com/shopspring/decimal"

// Profit share. Per copy the gain is G = equity − net deposits; adding or
// withdrawing funds moves both together, so flows never change G. The leader
// is owed pct × max(0, G − H), where H is the high-water mark of gain after
// shares paid (starting at 0), capped at the copy's free collateral. Paying
// fee lowers G by fee, so H becomes G − fee: losses are recovered before any
// further share is owed.

// Settlement is the outcome of SettleFee.
type Settlement struct {
	Gain decimal.Decimal // G
	Fee  decimal.Decimal // what to transfer to the leader
	HWM  decimal.Decimal // H after the payment
}

// SettleFee computes a settlement; pct is a fraction (0.10 = 10 %).
func SettleFee(pct, equity, netDeposits, hwm, free decimal.Decimal) Settlement {
	g := equity.Sub(netDeposits)
	s := Settlement{Gain: g, Fee: decimal.Zero, HWM: hwm}
	if !pct.IsPositive() {
		if g.GreaterThan(hwm) {
			s.HWM = g // no share: the mark still follows gains
		}
		return s
	}
	owed := pct.Mul(decimal.Max(decimal.Zero, g.Sub(hwm))).Truncate(2)
	fee := decimal.Min(owed, decimal.Max(decimal.Zero, free)).Truncate(2)
	if !fee.IsPositive() {
		return s
	}
	s.Fee = fee
	s.HWM = g.Sub(fee)
	return s
}
