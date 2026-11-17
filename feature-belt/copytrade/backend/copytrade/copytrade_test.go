package copytrade

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

var d = decimal.RequireFromString

func baseIn() PlanInput {
	s := DefaultSettings(d("5000"))
	s.MaxTradeNotional = "10000"
	return PlanInput{LeaderSz: d("20"), LeaderLev: 2, LeaderAV: d("100000"), CopySz: decimal.Zero, CopyAV: d("5000"),
		CopyFree: d("5000"), Mark: d("100"), SzDecimals: 2, MinNotional: d("10"), MarketMaxLev: 5, Settings: s}
}

func oneOrder(t *testing.T, p Plan) PlannedOrder {
	t.Helper()
	if len(p.Orders) != 1 {
		t.Fatalf("want one order, got %+v", p)
	}
	return p.Orders[0]
}

// Ratio and target with truncation to the size step; the margin, trade and
// position clips; minimum-order skips except closes; the flip split.
func TestSizeTargetsAndClips(t *testing.T) {
	p := Size(baseIn()) // ratio 0.05: 20 × 0.05 = 1.00
	if o := oneOrder(t, p); o.Side != engine.Buy || !o.Sz.Equal(d("1")) || o.ReduceOnly || o.Lev != 2 || !p.Ratio.Equal(d("0.05")) {
		t.Fatalf("plain open: %+v %+v", p, o)
	}
	in := baseIn()
	in.LeaderSz, in.CopyAV = d("7"), d("3333.33") // 7 × 0.0333333 = 0.2333 → 0.23, never rounded up
	if o := oneOrder(t, Size(in)); !o.Sz.Equal(d("0.23")) {
		t.Fatalf("truncation: %s", o.Sz)
	}
	in = baseIn()
	in.Settings.MaxPositionNotional = "50" // the position limit clips the target itself
	if p = Size(in); !p.Target.Equal(d("0.5")) || !oneOrder(t, p).Sz.Equal(d("0.5")) {
		t.Fatalf("position clip: %+v", p)
	}
	in = baseIn()
	in.Settings.MaxTradeNotional = "60" // the per-trade limit clips the order
	if p = Size(in); !oneOrder(t, p).Sz.Equal(d("0.6")) || len(p.Notes) == 0 || p.Skip != "" {
		t.Fatalf("trade clip: %+v", p)
	}
	in = baseIn()
	in.CopyFree = d("20") // free × leverage × 0.95 ÷ mark = 20 × 2 × 0.95 ÷ 100 = 0.38
	if o := oneOrder(t, Size(in)); !o.Sz.Equal(d("0.38")) {
		t.Fatalf("margin clip: %s", o.Sz)
	}
	in.CopyFree = d("4") // a clip that leaves less than the minimum is a risk-limit skip
	if p = Size(in); len(p.Orders) != 0 || p.Skip != SkipRiskLimit {
		t.Fatalf("risk-limit skip: %+v", p)
	}
	in = baseIn()
	in.LeaderSz = d("1") // target 0.05 × 100 = 5, below the minimum of 10
	if p = Size(in); len(p.Orders) != 0 || p.Skip != SkipTooSmall || p.Reason == "" {
		t.Fatalf("too small: %+v", p)
	}
	in.CopySz, in.LeaderSz = d("0.05"), decimal.Zero // …but closing 5 always goes
	if o := oneOrder(t, Size(in)); !o.ReduceOnly || o.Side != engine.Sell || !o.Sz.Equal(d("0.05")) {
		t.Fatalf("reduce-only close: %+v", o)
	}
	in = baseIn()
	in.CopySz, in.LeaderSz = d("1"), d("19") // a partial reduction of 5 is noise
	if p := Size(in); len(p.Orders) != 0 || p.Skip != SkipTooSmall {
		t.Fatalf("tiny reduction: %+v", p)
	}
	in = baseIn()
	in.CopySz, in.LeaderSz = d("1"), d("10")
	if o := oneOrder(t, Size(in)); !o.ReduceOnly || o.Side != engine.Sell || !o.Sz.Equal(d("0.5")) {
		t.Fatalf("reduce: %+v", o)
	}
	in = baseIn()
	in.CopySz, in.LeaderSz = d("1"), d("-20") // a flip is a reduce-only close, then an open
	p = Size(in)
	if len(p.Orders) != 2 || !p.Orders[0].ReduceOnly || p.Orders[0].Side != engine.Sell || !p.Orders[0].Sz.Equal(d("1")) ||
		p.Orders[1].ReduceOnly || p.Orders[1].Side != engine.Sell || !p.Orders[1].Sz.Equal(d("1")) {
		t.Fatalf("flip: %+v", p.Orders)
	}
	in = baseIn()
	in.LeaderLev, in.Settings.MaxLeverage, in.MarketMaxLev = 5, 3, 4
	if o := oneOrder(t, Size(in)); o.Lev != 3 {
		t.Fatalf("leverage %d", o.Lev)
	}
	in.LeaderLev, in.MarketMaxLev = 5, 2
	if o := oneOrder(t, Size(in)); o.Lev != 2 {
		t.Fatalf("leverage capped by the market: %d", o.Lev)
	}
	in = baseIn()
	in.CopySz = d("1")
	if p := Size(in); len(p.Orders) != 0 || p.Skip != "" {
		t.Fatalf("on target: %+v", p)
	}
	in = baseIn()
	in.Mark = decimal.Zero
	if p := Size(in); p.Skip != SkipRejected {
		t.Fatalf("no price: %+v", p)
	}
}

// The chase guard holds opens once the mark is more than 2 % adverse to the
// leader's fill; reductions always go.
func TestSizeChaseGuard(t *testing.T) {
	in := baseIn()
	in.RefPx, in.Mark = d("100"), d("102.01")
	if p := Size(in); p.Skip != SkipSlippage || len(p.Orders) != 0 {
		t.Fatalf("buy chased: %+v", p)
	}
	in.Mark = d("101.99")
	if p := Size(in); p.Skip != "" || len(p.Orders) != 1 {
		t.Fatalf("buy within 2%%: %+v", p)
	}
	in.Mark = d("97") // a better price is never a chase
	if p := Size(in); len(p.Orders) != 1 {
		t.Fatalf("buy below the fill: %+v", p)
	}
	in = baseIn()
	in.LeaderSz, in.RefPx, in.Mark = d("-20"), d("100"), d("97.9")
	if p := Size(in); p.Skip != SkipSlippage {
		t.Fatalf("sell chased: %+v", p)
	}
	in.CopySz, in.LeaderSz = d("-1"), decimal.Zero // closing a short ignores the guard
	if p := Size(in); len(p.Orders) != 1 || !p.Orders[0].ReduceOnly {
		t.Fatalf("close under the guard: %+v", p)
	}
}

// High-water-mark profit share: gains, losses, recovery, withdrawals, the
// free-collateral cap.
func TestProfitShareHighWaterMark(t *testing.T) {
	pct := d("0.10")
	s := SettleFee(pct, d("6000"), d("5000"), decimal.Zero, d("6000")) // 10 % of 1,000; H = 900
	if !s.Fee.Equal(d("100")) || !s.HWM.Equal(d("900")) {
		t.Fatalf("gain: %+v", s)
	}
	s = SettleFee(pct, d("5500"), d("5000"), d("900"), d("5500")) // G 500 < H 900: nothing
	if !s.Fee.IsZero() || !s.HWM.Equal(d("900")) {
		t.Fatalf("loss: %+v", s)
	}
	s = SettleFee(pct, d("6200"), d("5000"), d("900"), d("6200")) // only the 300 above the mark
	if !s.Fee.Equal(d("30")) || !s.HWM.Equal(d("1170")) {
		t.Fatalf("recovery: %+v", s)
	}
	s = SettleFee(pct, d("4170"), d("3000"), d("1170"), d("4170")) // a withdrawal moves both: nothing owed
	if !s.Fee.IsZero() || !s.Gain.Equal(d("1170")) {
		t.Fatalf("withdrawal: %+v", s)
	}
	s = SettleFee(pct, d("6000"), d("5000"), decimal.Zero, d("40")) // capped at free collateral
	if !s.Fee.Equal(d("40")) || !s.HWM.Equal(d("960")) {
		t.Fatalf("cap: %+v", s)
	}
	s = SettleFee(decimal.Zero, d("6000"), d("5000"), decimal.Zero, d("6000")) // no share: the mark still follows
	if !s.Fee.IsZero() || !s.HWM.Equal(d("1000")) {
		t.Fatalf("zero pct: %+v", s)
	}
}

func TestSettingsDefaultsAndValidation(t *testing.T) {
	base := DefaultSettings(d("250"))
	if base.MaxPositionNotional != "750" || base.MaxLeverage != 3 || *base.StopLossPct != "30" || base.MaxDrawdownPct != nil ||
		base.SlippageBps != 100 || base.OnLeaderExit != "close" || base.MaxTradeNotional != "500" {
		t.Fatalf("defaults %+v", base)
	}
	for i, raw := range []string{`{"max_leverage": 11}`, `{"max_trade_notional": "5"}`, `{"stop_loss_pct": "95"}`,
		`{"max_drawdown_pct": "4"}`, `{"on_leader_exit": "flee"}`, `{"slippage_bps": 600}`, `{"max_position_notional": "x"}`} {
		var p SettingsPatch
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Apply(base); err == nil {
			t.Fatalf("case %d accepted: %s", i, raw)
		}
	}
	var p SettingsPatch
	_ = json.Unmarshal([]byte(`{"stop_loss_pct": null, "max_drawdown_pct": "20"}`), &p)
	got, err := p.Apply(base)
	if err != nil || got.StopLossPct != nil || got.MaxDrawdownPct == nil || *got.MaxDrawdownPct != "20" || got.MaxLeverage != 3 {
		t.Fatalf("null stop loss, drawdown on, the rest kept: %+v %v", got, err)
	}
	if allocationBucket(d("99")) != "lt_100" || allocationBucket(d("100")) != "100_1k" || allocationBucket(d("5000")) != "1k_10k" ||
		allocationBucket(d("10000")) != "gt_10k" {
		t.Fatal("allocation buckets")
	}
}

// Returns are time-weighted: deposits and withdrawals never count as gains.
func TestLeaderStatsIgnoreFlows(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n) }
	pts := []snapPoint{
		{day(0), d("1000"), d("1000")},
		{day(10), d("1100"), d("1000")}, // +10 %
		{day(20), d("2100"), d("2000")}, // a 1,000 deposit, no gain
		{day(30), d("1890"), d("2000")}, // −10 % of 2,100
		{day(40), d("2079"), d("2000")}, // +10 % of 1,890
	}
	st := computeStats(pts, day(40))
	if st.r30 == nil || *st.r30 != "-1.00" { // from day 10, the last snapshot at or before the window: 0.9 × 1.1 = 0.99
		t.Fatalf("30 day return %v", st.r30)
	}
	if st.r90 == nil || *st.r90 != "8.90" { // whole series: 1.1 × 0.9 × 1.1 = 1.089
		t.Fatalf("90 day return %v", *st.r90)
	}
	if st.dd90 == nil || *st.dd90 != "10.00" {
		t.Fatalf("drawdown %v", *st.dd90)
	}
	if st.days != 41 {
		t.Fatalf("history days %d", st.days)
	}
	if s := computeStats(pts[:1], day(0)); s.r30 != nil || s.dd90 != nil {
		t.Fatal("one snapshot has no returns")
	}
}
