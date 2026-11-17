package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var d = decimal.RequireFromString

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func testMarkets() []MarketSpec {
	return []MarketSpec{{
		ID: uuid.NewString(), Symbol: "TST-A", Name: "Test A", MaxLeverage: 5, SzDecimals: 2,
		TickSize: d("0.01"), MinNotional: d("10"), OICap: d("100000"), SeedPrice: d("100"), Status: MarketActive,
	}, {
		ID: uuid.NewString(), Symbol: "TST-B", Name: "Test B", MaxLeverage: 3, SzDecimals: 2,
		TickSize: d("0.01"), MinNotional: d("10"), OICap: d("500"), SeedPrice: d("50"), Status: MarketActive,
	}}
}

func testParams() Params {
	p := DefaultParams()
	p.WelcomeCredit = d("1000")
	p.FaucetAmount = d("1000")
	p.AllowSeedPrices = true
	p.LiqPartialThreshold = d("2000")
	return p
}

func newTestEngine(t *testing.T) (*Engine, *fakeClock) { return newTestEngineWith(t, &MemStore{}) }

func newTestEngineWith(t *testing.T, st Store) (*Engine, *fakeClock) {
	t.Helper()
	return newTestEngineParams(t, testParams(), st)
}

func newTestEngineParams(t *testing.T, p Params, st Store) (*Engine, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Date(2026, 10, 14, 12, 0, 30, 0, time.UTC)}
	e := New(p, st, nil, testMarkets())
	e.SetClock(clk.now)
	if err := e.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e, clk
}

var userIDs = map[string]string{}

func user(t *testing.T, e *Engine, name string) string {
	t.Helper()
	uid, ok := userIDs[name]
	if !ok {
		uid = uuid.NewString()
		userIDs[name] = uid
	}
	id, _, err := e.EnsureUser(uid)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func limit(e *Engine, id, side, px, sz string, lev int) (OrderView, []Fill, error) {
	return e.PlaceOrder(id, PlaceOrder{Symbol: "TST-A", Side: side, Type: TypeLimit, Px: px, Sz: sz, Leverage: lev})
}

func mkt(e *Engine, id, side, sz string, lev int) (OrderView, []Fill, error) {
	return e.PlaceOrder(id, PlaceOrder{Symbol: "TST-A", Side: side, Type: TypeMarket, Sz: sz, Leverage: lev})
}

func must(t *testing.T) func(OrderView, []Fill, error) (OrderView, []Fill) {
	return func(v OrderView, f []Fill, err error) (OrderView, []Fill) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return v, f
	}
}

func code(err error) string {
	var ee *Error
	if errors.As(err, &ee) {
		return ee.Code
	}
	return ""
}

func pos(e *Engine, id, sym string) decimal.Decimal {
	if p := e.Account(id).Positions[sym]; p != nil {
		return p.Sz
	}
	return decimal.Zero
}

func TestPriceTimePriorityAndPartialFills(t *testing.T) {
	e, _ := newTestEngine(t)
	m1, m2, m3, tk := user(t, e, "m1"), user(t, e, "m2"), user(t, e, "m3"), user(t, e, "tk")
	o1, _ := must(t)(limit(e, m1, Sell, "101", "1", 1))
	o2, _ := must(t)(limit(e, m2, Sell, "101", "1", 1))
	must(t)(limit(e, m3, Sell, "100.5", "1", 1))

	_, fills := must(t)(mkt(e, tk, Buy, "2.5", 1))
	if len(fills) != 3 {
		t.Fatalf("want 3 fills, got %d", len(fills))
	}
	want := []struct{ px, sz string }{{"100.50", "1.00"}, {"101.00", "1.00"}, {"101.00", "0.50"}}
	for i, w := range want {
		if fills[i].Px != w.px || fills[i].Sz != w.sz || fills[i].Liquidity != "taker" {
			t.Errorf("fill %d = %s@%s, want %s@%s", i, fills[i].Sz, fills[i].Px, w.sz, w.px)
		}
	}
	if !pos(e, m1, "TST-A").Equal(d("-1")) || !pos(e, m2, "TST-A").Equal(d("-0.5")) {
		t.Errorf("time priority violated: m1=%s m2=%s", pos(e, m1, "TST-A"), pos(e, m2, "TST-A"))
	}
	if e.orders[o1.ID] != nil {
		t.Error("m1 order should be filled and off the book")
	}
	if rest := e.orders[o2.ID]; rest == nil || !rest.Remaining().Equal(d("0.5")) {
		t.Error("m2 order should rest with 0.5 remaining")
	}
	ov, fills := must(t)(limit(e, tk, Buy, "101", "1", 1))
	if len(fills) != 1 || ov.Status != StatusOpen || ov.FilledSz != "0.50" {
		t.Fatalf("partial limit: status=%s filled=%s fills=%d", ov.Status, ov.FilledSz, len(fills))
	}
	b, _ := e.Book("TST-A", 10)
	if len(b.Bids) != 1 || b.Bids[0].Px != "101.00" || b.Bids[0].Sz != "0.50" || len(b.Asks) != 0 {
		t.Fatalf("book after partial: %+v", b)
	}
}

func TestBookAggregatesLevels(t *testing.T) {
	e, _ := newTestEngine(t)
	a, b := user(t, e, "agg-a"), user(t, e, "agg-b")
	must(t)(limit(e, a, Buy, "99", "1", 1))
	must(t)(limit(e, b, Buy, "99", "2.5", 1))
	must(t)(limit(e, a, Buy, "98", "1", 1))
	bk, _ := e.Book("TST-A", 10)
	if len(bk.Bids) != 2 || bk.Bids[0].Sz != "3.50" || bk.Bids[0].N != 2 || bk.Bids[1].Px != "98.00" {
		t.Fatalf("levels: %+v", bk.Bids)
	}
}

func TestSelfTradeExpiresMaker(t *testing.T) {
	e, _ := newTestEngine(t)
	a, b := user(t, e, "a"), user(t, e, "b")
	own, _ := must(t)(limit(e, a, Sell, "100", "1", 1))
	must(t)(limit(e, b, Sell, "100.5", "1", 1))
	_, fills := must(t)(mkt(e, a, Buy, "1", 1))
	if len(fills) != 1 || fills[0].Px != "100.50" {
		t.Fatalf("want one fill against b at 100.50, got %+v", fills)
	}
	if e.orders[own.ID] != nil {
		t.Fatal("own resting order should have been cancelled")
	}
}

func TestMarginRejection(t *testing.T) {
	e, _ := newTestEngine(t)
	mk, a := user(t, e, "mk"), user(t, e, "a2")
	must(t)(limit(e, mk, Sell, "100", "5", 5))
	if _, _, err := limit(e, a, Buy, "99", "15", 1); code(err) != "insufficient_margin" {
		t.Fatalf("want insufficient_margin, got %v", err)
	}
	must(t)(limit(e, a, Buy, "99", "15", 2))
	if _, _, err := limit(e, a, Buy, "98", "6", 2); code(err) != "insufficient_margin" {
		t.Fatalf("want insufficient_margin for stacked orders, got %v", err)
	}
	if _, _, err := limit(e, a, Sell, "105", "5", 2); err != nil {
		t.Fatalf("opposite-side order should not need margin: %v", err)
	}
}

func TestReduceOnly(t *testing.T) {
	e, _ := newTestEngine(t)
	mk, a := user(t, e, "mk2"), user(t, e, "a3")
	if _, _, err := e.PlaceOrder(a, PlaceOrder{Symbol: "TST-A", Side: Sell, Type: TypeMarket, Sz: "1", Leverage: 2, ReduceOnly: true}); code(err) != "reduce_only" {
		t.Fatalf("want reduce_only with no position, got %v", err)
	}
	must(t)(limit(e, mk, Sell, "100", "2", 2))
	must(t)(mkt(e, a, Buy, "2", 2))
	must(t)(limit(e, mk, Buy, "99", "10", 2))
	ov, _ := must(t)(e.PlaceOrder(a, PlaceOrder{Symbol: "TST-A", Side: Sell, Type: TypeMarket, Sz: "5", Leverage: 2, ReduceOnly: true}))
	if ov.Sz != "2.00" || ov.FilledSz != "2.00" {
		t.Fatalf("reduce-only should clamp to the position: sz=%s filled=%s", ov.Sz, ov.FilledSz)
	}
	if !pos(e, a, "TST-A").IsZero() {
		t.Fatalf("position should be flat, got %s", pos(e, a, "TST-A"))
	}
}

func TestRejections(t *testing.T) {
	e, _ := newTestEngine(t)
	mk, a := user(t, e, "mk3"), user(t, e, "a4")
	must(t)(limit(e, mk, Sell, "100", "5", 5))
	cases := []struct {
		req  PlaceOrder
		code string
	}{
		{PlaceOrder{Symbol: "TST-A", Side: Buy, Type: TypeLimit, Px: "120", Sz: "1", Leverage: 1}, "price_band"},
		{PlaceOrder{Symbol: "TST-A", Side: Buy, Type: TypeLimit, Px: "100", Sz: "1", Leverage: 1, PostOnly: true}, "post_only_would_cross"},
		{PlaceOrder{Symbol: "TST-A", Side: Buy, Type: TypeLimit, Px: "99", Sz: "0.05", Leverage: 1}, "min_notional"},
		{PlaceOrder{Symbol: "TST-A", Side: Sell, Type: TypeMarket, Sz: "1", Leverage: 1}, "no_liquidity"},
		{PlaceOrder{Symbol: "TST-A", Side: Buy, Type: TypeMarket, Sz: "1", Leverage: 9}, "invalid"},
		{PlaceOrder{Symbol: "TST-B", Side: Buy, Type: TypeLimit, Px: "50", Sz: "11", Leverage: 3}, "oi_cap"},
	}
	for _, c := range cases {
		if _, _, err := e.PlaceOrder(a, c.req); code(err) != c.code {
			t.Errorf("%+v: want %s, got %v", c.req, c.code, err)
		}
	}
	_ = e.SetIndexNow(IndexUpdate{Symbol: "TST-A", Price: d("100"), Status: IndexStale})
	if _, _, err := limit(e, a, Buy, "99", "1", 1); code(err) != "index_stale" {
		t.Errorf("want index_stale, got %v", err)
	}
}

func TestNoPriceOutsideDevelopment(t *testing.T) {
	p := testParams()
	p.AllowSeedPrices = false
	e := New(p, &MemStore{}, nil, testMarkets())
	if err := e.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := user(t, e, "noprice")
	if _, _, err := limit(e, a, Buy, "99", "1", 1); code(err) != "price_unavailable" {
		t.Fatalf("want price_unavailable before the feed reports, got %v", err)
	}
	_ = e.SetIndexNow(IndexUpdate{Symbol: "TST-A", Price: d("100"), Status: IndexLive})
	must(t)(limit(e, a, Buy, "99", "1", 1))
}

func TestMarketStatuses(t *testing.T) {
	e, _ := newTestEngine(t)
	mk, a := user(t, e, "st-mk"), user(t, e, "st-a")
	must(t)(limit(e, mk, Sell, "100", "5", 5))
	must(t)(mkt(e, a, Buy, "1", 2))
	specs := testMarkets()
	specs[0].ID = e.markets["TST-A"].spec.ID
	specs[0].Status = MarketReduceOnly
	e.SyncMarkets(specs[:1])
	if _, _, err := mkt(e, a, Buy, "1", 2); code(err) != "market_reduce_only" {
		t.Fatalf("want market_reduce_only, got %v", err)
	}
	must(t)(limit(e, a, Sell, "100.5", "1", 2)) // reducing is fine
	specs[0].Status = MarketHalted
	e.SyncMarkets(specs[:1])
	if _, _, err := limit(e, a, Sell, "100.5", "0.5", 2); code(err) != "market_halted" {
		t.Fatalf("want market_halted, got %v", err)
	}
}

func setupLongAt100(t *testing.T, e *Engine) (alice string) {
	t.Helper()
	carol := user(t, e, "carol")
	alice = user(t, e, "alice")
	must(t)(limit(e, carol, Sell, "100", "40", 5))
	must(t)(mkt(e, alice, Buy, "40", 5))
	if !pos(e, alice, "TST-A").Equal(d("40")) {
		t.Fatal("setup: alice should be long 40")
	}
	return alice
}

func TestFundingRateFormulaAndCap(t *testing.T) {
	e, _ := newTestEngine(t)
	cases := map[string]string{
		"0":      "0.0000125",
		"0.0005": "0.0000125",
		"0.01":   "0.0011875",
		"-0.01":  "-0.0011875",
		"1":      "0.04",
		"-1":     "-0.04",
	}
	for p, want := range cases {
		if got := e.hourlyRate(d(p)); !got.Equal(d(want)) {
			t.Errorf("hourlyRate(%s) = %s, want %s", p, got, want)
		}
	}
}

func TestFundingSettlementSign(t *testing.T) {
	e, clk := newTestEngine(t)
	alice := setupLongAt100(t, e)
	carol := user(t, e, "carol")
	ab, cb := e.Account(alice).Balance, e.Account(carol).Balance
	clk.t = time.UnixMilli(e.nextFunding).Add(time.Second)
	if err := e.Tick(); err != nil { // empty book → premium 0 → rate = interest, longs pay
		t.Fatal(err)
	}
	pay := d("40").Mul(d("100")).Mul(d("0.0000125"))
	if got := ab.Sub(e.Account(alice).Balance); !got.Equal(pay) {
		t.Fatalf("long should pay %s, paid %s", pay, got)
	}
	if got := e.Account(carol).Balance.Sub(cb); !got.Equal(pay) {
		t.Fatalf("short should receive %s, got %s", pay, got)
	}
}

func TestMarkMedianAndClamp(t *testing.T) {
	e, _ := newTestEngine(t)
	m := e.markets["TST-A"]
	o := func(side, px string) *Order {
		return &Order{ID: newID(), Side: side, Px: d(px), Sz: d("1"), Seq: e.nextSeq()}
	}
	m.index = d("100")
	m.insert(o(Buy, "99"))
	m.insert(o(Sell, "101"))
	if got := e.computeMark(m); !got.Equal(d("100")) {
		t.Fatalf("mark = %s, want 100", got)
	}
	m.ema, m.emaInit = d("5"), true
	m.lastPx, m.hasLast = d("104"), true
	if got := e.computeMark(m); !got.Equal(d("101")) {
		t.Fatalf("mark = %s, want 101", got)
	}
	m.bids, m.asks = nil, nil
	m.insert(o(Buy, "120"))
	m.insert(o(Sell, "125"))
	m.lastPx = d("123")
	m.ema = d("21")
	if got := e.computeMark(m); !got.Equal(d("110")) {
		t.Fatalf("mark = %s, want clamp to 110", got)
	}
}

func TestPositionApplyRealizesPnl(t *testing.T) {
	p := &Position{Symbol: "X"}
	p.apply(d("2"), d("100"))
	p.apply(d("2"), d("110"))
	if !p.EntryPx.Equal(d("105")) {
		t.Fatalf("entry = %s", p.EntryPx)
	}
	if r := p.apply(d("-3"), d("120")); !r.Equal(d("45")) {
		t.Fatalf("realized = %s, want 45", r)
	}
	if r := p.apply(d("-3"), d("100")); !r.Equal(d("-5")) || !p.Sz.Equal(d("-2")) || !p.EntryPx.Equal(d("100")) {
		t.Fatalf("flip: realized=%s sz=%s entry=%s", r, p.Sz, p.EntryPx)
	}
}

func TestQuotesTakeCrossingOrdersThenRest(t *testing.T) {
	e, _ := newTestEngine(t)
	mm, err := e.EnsureSystem(KindMM, "mm", d("1000000"))
	if err != nil {
		t.Fatal(err)
	}
	a := user(t, e, "qa")
	must(t)(limit(e, a, Buy, "102", "1", 2))
	if err := e.ReplaceQuotes(mm, "TST-A", []Quote{{Side: Sell, Px: d("101"), Sz: d("3")}, {Side: Buy, Px: d("99"), Sz: d("3")}}, 5); err != nil {
		t.Fatal(err)
	}
	if !pos(e, a, "TST-A").Equal(d("1")) {
		t.Fatalf("user bid should fill against the crossing quote, pos=%s", pos(e, a, "TST-A"))
	}
	tr, _ := e.Trades("TST-A", 1)
	if len(tr) != 1 || tr[0].Px != "102.00" || tr[0].Side != Sell || !tr[0].Sim {
		t.Fatalf("trade should print at the resting price, flagged sim: %+v", tr)
	}
	b, _ := e.Book("TST-A", 5)
	if len(b.Asks) != 1 || b.Asks[0].Sz != "2.00" || len(b.Bids) != 1 || b.Bids[0].Px != "99.00" {
		t.Fatalf("remainder should rest: %+v", b)
	}
}

// ---- liquidation ----

func pinMark(e *Engine) {
	m := e.markets["TST-A"]
	m.ema, m.emaInit = decimal.Zero, true
}

func indexTo(t *testing.T, e *Engine, px string) {
	t.Helper()
	if err := e.SetIndexNow(IndexUpdate{Symbol: "TST-A", Price: d(px), Status: IndexLive}); err != nil {
		t.Fatal(err)
	}
	if mk := e.markets["TST-A"].mark; !mk.Equal(d(px)) {
		t.Fatalf("mark not pinned: %s", mk)
	}
}

func lastLiquidations(st *MemStore) []LiquidationRec {
	for i := len(st.Commits) - 1; i >= 0; i-- {
		if len(st.Commits[i].Liquidations) > 0 {
			return st.Commits[i].Liquidations
		}
	}
	return nil
}

// alice: long 40 TST-A @ 100 at 5x, balance 998.20 (after the 1.80 taker fee).
// Maintenance margin is 10% of notional (max leverage 5).

func TestLiquidationSmallPositionGoesInFullIntoBook(t *testing.T) {
	e, _ := newTestEngine(t)
	e.p.LiqPartialThreshold = d("10000")
	pinMark(e)
	alice := setupLongAt100(t, e)
	bob := user(t, e, "bob")
	indexTo(t, e, "90")
	must(t)(limit(e, bob, Buy, "80", "40", 5))
	indexTo(t, e, "82") // AV 278.20 < MM 328 (and ≥ 2/3 MM: no backstop)
	if !pos(e, alice, "TST-A").IsZero() || !pos(e, bob, "TST-A").Equal(d("40")) {
		t.Fatalf("whole position should go to bob's bid: alice=%s bob=%s", pos(e, alice, "TST-A"), pos(e, bob, "TST-A"))
	}
	if !pos(e, e.backstop.ID, "TST-A").IsZero() {
		t.Fatal("the backstop must not be involved above its level")
	}
	if bal := e.Account(alice).Balance; !bal.Equal(d("196.76")) { // 998.20 − 800 − 1.44 fee
		t.Fatalf("alice balance = %s", bal)
	}
	tr, _ := e.Trades("TST-A", 1)
	if !tr[0].Liquidation || tr[0].Px != "80.00" {
		t.Fatalf("liquidation trade: %+v", tr[0])
	}
}

func TestPartialLiquidationThenRecovery(t *testing.T) {
	st := &MemStore{}
	e, clk := newTestEngineWith(t, st)
	pinMark(e)
	alice := setupLongAt100(t, e)
	bob := user(t, e, "bob")
	indexTo(t, e, "90")
	must(t)(limit(e, bob, Buy, "83", "8", 5))
	indexTo(t, e, "83") // notional 3,320 > 2,000 ⇒ only 20% (8) goes into the book
	if p := pos(e, alice, "TST-A"); !p.Equal(d("32")) {
		t.Fatalf("want 20%% liquidated (32 left), pos=%s", p)
	}
	liqs := lastLiquidations(st)
	if len(liqs) != 1 || !liqs[0].Sz.Equal(d("8")) || !liqs[0].BookSz.Equal(d("8")) || !liqs[0].BackstopSz.IsZero() {
		t.Fatalf("liquidation record: %+v", liqs)
	}
	if want := clk.t.Add(30 * time.Second).UnixMilli(); e.Account(alice).LiqCooldownUntilMs != want {
		t.Fatal("partial liquidation should start the cooldown")
	}
}

func TestBackstopTakesPositionsAndRemainingMargin(t *testing.T) {
	e, _ := newTestEngine(t)
	pinMark(e)
	alice := setupLongAt100(t, e)
	before := e.backstop.Balance
	indexTo(t, e, "78") // AV 118.20 < 2/3 × MM 312 = 208
	if !pos(e, alice, "TST-A").IsZero() {
		t.Fatal("backstop should close everything")
	}
	bp := e.backstop.Positions["TST-A"]
	if bp == nil || !bp.Sz.Equal(d("40")) || !bp.EntryPx.Equal(d("78")) {
		t.Fatalf("backstop should hold 40 @ 78: %+v", bp)
	}
	if !e.Account(alice).Balance.IsZero() {
		t.Fatalf("maintenance margin is not returned: balance %s", e.Account(alice).Balance)
	}
	if got := e.backstop.Balance.Sub(before); !got.Equal(d("118.2")) {
		t.Fatalf("backstop keeps the remaining 118.20, got %s", got)
	}
}

func TestBackstopAbsorbsBadDebt(t *testing.T) {
	e, _ := newTestEngine(t)
	pinMark(e)
	alice := setupLongAt100(t, e)
	before := e.backstop.Balance
	indexTo(t, e, "75") // AV −1.80
	if !pos(e, alice, "TST-A").IsZero() || !e.Account(alice).Balance.IsZero() {
		t.Fatal("account should be closed out at zero")
	}
	if got := before.Sub(e.backstop.Balance); !got.Equal(d("1.8")) {
		t.Fatalf("backstop absorbs 1.80 bad debt, absorbed %s", got)
	}
}

// ---- transfers, close-out, atomicity ----

func TestTransfers(t *testing.T) {
	e, _ := newTestEngine(t)
	uid := uuid.NewString()
	res, err := e.ApplyTransfers([]Transfer{{ID: "t1", UserID: uid, Kind: "deposit", Amount: d("250")}})
	if err != nil || len(res) != 1 || res[0].Status != "applied" {
		t.Fatalf("deposit: %+v %v", res, err)
	}
	id, ok := e.AccountForUser(uid)
	if !ok || !e.Account(id).Balance.Equal(d("250")) || !e.Account(id).Deposits.Equal(d("250")) {
		t.Fatal("deposit should create a funded account")
	}
	res, _ = e.ApplyTransfers([]Transfer{{ID: "t2", UserID: uid, Kind: "withdrawal", Amount: d("300")}})
	if res[0].Status != "rejected" {
		t.Fatalf("over-withdrawal should be rejected: %+v", res)
	}
	res, _ = e.ApplyTransfers([]Transfer{{ID: "t3", UserID: uid, Kind: "withdrawal", Amount: d("100")}})
	if res[0].Status != "applied" || !e.Account(id).Balance.Equal(d("150")) {
		t.Fatalf("withdrawal: %+v balance %s", res, e.Account(id).Balance)
	}
}

func TestCloseAccountHandsEverythingToTheBackstop(t *testing.T) {
	e, _ := newTestEngine(t)
	alice := setupLongAt100(t, e)
	must(t)(limit(e, alice, Sell, "105", "1", 5))
	uid := e.Account(alice).UserID
	if err := e.CloseAccount(alice); err != nil {
		t.Fatal(err)
	}
	a := e.Account(alice)
	if a.Status != AccountClosed || a.UserID != "" || !a.Balance.IsZero() || hasPosition(a) || len(a.Orders) != 0 {
		t.Fatalf("closed account: %+v", a)
	}
	if _, ok := e.AccountForUser(uid); ok {
		t.Fatal("the user should no longer map to the account")
	}
	if !pos(e, e.backstop.ID, "TST-A").Equal(d("40")) {
		t.Fatal("the backstop should hold the position")
	}
}

type failStore struct {
	MemStore
	fail bool
}

func (s *failStore) Commit(ctx context.Context, b *Batch) error {
	if s.fail {
		return errors.New("injected failure")
	}
	return s.MemStore.Commit(ctx, b)
}

func TestFailedCommitRollsBack(t *testing.T) {
	st := &failStore{}
	e, _ := newTestEngineWith(t, st)
	mk, a := user(t, e, "rb-mk"), user(t, e, "rb-a")
	must(t)(limit(e, mk, Sell, "100", "5", 5))
	before := e.Account(a).Balance
	st.fail = true
	if _, _, err := mkt(e, a, Buy, "2", 2); code(err) != "unavailable" {
		t.Fatalf("want unavailable, got %v", err)
	}
	if !pos(e, a, "TST-A").IsZero() || !e.Account(a).Balance.Equal(before) {
		t.Fatal("state should be rolled back")
	}
	b, _ := e.Book("TST-A", 5)
	if len(b.Asks) != 1 || b.Asks[0].Sz != "5.00" {
		t.Fatalf("book should be restored: %+v", b)
	}
	st.fail = false
	must(t)(mkt(e, a, Buy, "2", 2))
}

func TestMarkMovesAtMostPointThreePercentPerSecond(t *testing.T) {
	e, clk := newTestEngine(t)
	bid, ask := user(t, e, "bidder"), user(t, e, "asker")
	must(t)(limit(e, bid, Buy, "99.5", "2", 5))
	must(t)(limit(e, ask, Sell, "100.5", "2", 5))
	clk.t = clk.t.Add(time.Second)
	_ = e.Tick()
	before := e.markets["TST-A"].mark
	_ = e.SetIndexNow(IndexUpdate{Symbol: "TST-A", Price: d("110"), Status: IndexLive})
	if mv := e.markets["TST-A"].mark.Sub(before).Abs().Div(before); mv.GreaterThan(d("0.003")) {
		t.Fatalf("mark jumped %s within a second", mv)
	}
}

// totalBalance is the collateral held by every account, system ones included.
func totalBalance(e *Engine) decimal.Decimal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	sum := decimal.Zero
	for _, a := range e.accounts {
		sum = sum.Add(a.Balance)
	}
	return sum
}

func ledgerSum(st *MemStore, accountID string) decimal.Decimal {
	sum := decimal.Zero
	for _, b := range st.Commits {
		for _, l := range b.Ledger {
			if l.AccountID == accountID {
				sum = sum.Add(l.Amount)
			}
		}
	}
	return sum
}

// Fees move collateral between traders and the fee account, never create or
// destroy it, and every change shows in the person's ledger.
func TestFeesConserveCollateral(t *testing.T) {
	for _, tc := range []struct {
		name, makerFee, makerBal, feeBal string
	}{
		{"fee", "0.00015", "999.985", "0.06"},     // taker pays 0.045, maker 0.015
		{"rebate", "-0.0001", "1000.01", "0.035"}, // taker pays 0.045, maker earns 0.01
		{"free", "0", "1000", "0.045"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testParams()
			p.MakerFee = d(tc.makerFee)
			st := &MemStore{}
			e, _ := newTestEngineParams(t, p, st)
			maker, taker := user(t, e, "conserve-maker-"+tc.name), user(t, e, "conserve-taker-"+tc.name)
			before := totalBalance(e)
			must(t)(limit(e, maker, Sell, "100", "1", 5))
			must(t)(mkt(e, taker, Buy, "1", 5))
			if got := totalBalance(e); !got.Equal(before) {
				t.Fatalf("total collateral went from %s to %s", before, got)
			}
			if got := e.Account(maker).Balance; !got.Equal(d(tc.makerBal)) {
				t.Fatalf("maker balance %s, want %s", got, tc.makerBal)
			}
			if got := e.Account(taker).Balance; !got.Equal(d("999.955")) {
				t.Fatalf("taker balance %s", got)
			}
			if got := e.fee.Balance; !got.Equal(d(tc.feeBal)) {
				t.Fatalf("fee account %s, want %s", got, tc.feeBal)
			}
			for _, id := range []string{maker, taker} {
				if l, b := ledgerSum(st, id), e.Account(id).Balance; !l.Equal(b) {
					t.Fatalf("ledger sums to %s but the balance is %s", l, b)
				}
			}
		})
	}
}
