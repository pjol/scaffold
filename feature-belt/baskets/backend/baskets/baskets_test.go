package baskets

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/httpx"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestParseLines(t *testing.T) {
	text := "# my basket\n2 Gold\nSilver x3\nBTC-USD, 1\nx4 Wheat\n- Crude oil\n1. Copper\n\n3x  Platinum bar\nGold\t2\n\"Palladium\";5\n"
	got, warns, err := Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	want := []Line{{2, "Gold", 2}, {3, "Silver", 3}, {4, "BTC-USD", 1}, {5, "Wheat", 4}, {6, "Crude oil", 1}, {7, "Copper", 1},
		{9, "Platinum bar", 3}, {10, "Gold", 2}, {11, "Palladium", 5}}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseWarnings(t *testing.T) {
	got, warns, err := Parse("1.5 Gold\n0 Silver\n20000 Oil\nGold x2.5\nCopper\n" + strings.Repeat("y", 130))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "Copper" {
		t.Fatalf("lines: %+v", got)
	}
	if len(warns) != 5 {
		t.Fatalf("want 5 warnings, got %d: %v", len(warns), warns)
	}
	for _, w := range warns {
		if !strings.HasPrefix(w, "Line ") || !strings.HasSuffix(w, "Skipped.") {
			t.Errorf("warning not in words: %q", w)
		}
	}
	if _, _, err := Parse(strings.Repeat("Gold\n", 251)); !errors.Is(err, errTooLong) {
		t.Fatalf("251 lines: err = %v", err)
	}
}

func TestParseCSV(t *testing.T) {
	got, warns, err := Parse("Name,Quantity,Note\nGold,2,x\n\"Silver, fine\",,\nOil,1.5,\n,,\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Line{2, "Gold", 2}) || got[1] != (Line{3, "Silver, fine", 1}) {
		t.Fatalf("lines: %+v", got)
	}
	if len(warns) != 1 || !strings.HasPrefix(warns[0], "Row 4") {
		t.Fatalf("warnings: %v", warns)
	}
	// A symbol column wins over a name column; tabs work too.
	got, _, err = Parse("symbol\tname\tqty\nGOLD\tGold bar\t3\n")
	if err != nil || len(got) != 1 || got[0].Text != "GOLD" || got[0].Quantity != 3 {
		t.Fatalf("tsv: %+v, %v", got, err)
	}
}

func TestCanonicalAndSymbol(t *testing.T) {
	a := []Item{{"SILVER", 40}, {"GOLD", 1}}
	b := []Item{{"GOLD", 1}, {"SILVER", 40}}
	if Canonical(a) != "basket-v1\nGOLD\t1\nSILVER\t40\n" {
		t.Fatalf("canonical = %q", Canonical(a))
	}
	if Hash(a) != Hash(b) {
		t.Fatal("order changed the hash")
	}
	if Hash(a) == Hash([]Item{{"GOLD", 1}, {"SILVER", 41}}) {
		t.Fatal("quantity didn't change the hash")
	}
	h := Hash(a)
	s6, s8 := SymbolFor("BKT", h, 6), SymbolFor("BKT", h, 8)
	if len(s6) != 10 || !strings.HasPrefix(s8, s6) || strings.ContainsAny(s6[4:], "ILOU") {
		t.Fatalf("symbols %s %s", s6, s8)
	}
	if !symbolOK(SymbolFor("ABCDEFGHIJK", h, 12)) {
		t.Fatal("longest prefix + 12 breaks the exchange's symbol rule")
	}
}

func symbolOK(s string) bool {
	if len(s) == 0 || len(s) > 24 {
		return false
	}
	for i, c := range s {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || (i > 0 && c == '-')) {
			return false
		}
	}
	return true
}

func TestSpec(t *testing.T) {
	c := settings{maxLeverage: 3, minOrder: d("10"), oiMin: d("5000"), oiMax: d("50000")}
	sp := c.SpecFor(d("312"), d("0.3"))
	if sp.TickSize != "0.02" || sp.SzDecimals != 2 || sp.MaxLeverage != 3 || sp.OICap != "31000" || sp.MinNotional != "10" {
		t.Fatalf("312: %+v", sp)
	}
	sp = c.SpecFor(d("4123.5"), d("0.8"))
	if sp.TickSize != "0.2" || sp.SzDecimals != 3 || sp.MaxLeverage != 2 || sp.OICap != "25000" {
		t.Fatalf("concentrated 4123.5: %+v", sp)
	}
	if sp = c.SpecFor(d("21"), d("0.5")); sp.TickSize != "0.002" || sp.OICap != "5000" || sp.MaxLeverage != 3 {
		t.Fatalf("21: %+v", sp)
	}
	if TickFor(d("3")).String() != "0.001" {
		t.Fatal("tick floor")
	}
}

func TestResolveDefault(t *testing.T) {
	ms := []exchange.ListedMarket{
		{CatalogMarket: exchange.CatalogMarket{Symbol: "GOLD", Name: "Gold", PriceFeed: "GOLD-OZ"}},
		{CatalogMarket: exchange.CatalogMarket{Symbol: "SILVER", Name: "Silver", PriceFeed: "SILVER-OZ"}},
		{CatalogMarket: exchange.CatalogMarket{Symbol: "BTC-USD", Name: "Bitcoin"}},
		{CatalogMarket: exchange.CatalogMarket{Symbol: "PLAT", Name: "Platinum bar"}},
		{CatalogMarket: exchange.CatalogMarket{Symbol: "PALL", Name: "Palladium bar"}},
		{CatalogMarket: exchange.CatalogMarket{Symbol: "CAFE", Name: "Café crème"}},
		{CatalogMarket: exchange.CatalogMarket{Symbol: "BKT-AAAAAA", Name: "Gold & Silver"}},
	}
	ix := newMarketIndex(ms, map[string]bool{"BKT-AAAAAA": true})
	ctx := context.Background()
	cases := []struct{ in, sym, match, reason string }{
		{"gold", "GOLD", MatchSymbol, ""},
		{"BITCOIN", "BTC-USD", MatchName, ""},
		{"btc/usd", "BTC-USD", MatchSymbol, ""},
		{"Silver-oz", "SILVER", MatchAlias, ""},
		{"cafe", "CAFE", MatchSymbol, ""},
		{"CAFE CREME", "CAFE", MatchName, ""},
		{"Platnum bar", "PLAT", MatchFuzzy, ""},
		{"Pla bar", "", "", ReasonUnknown},
		{"Unobtainium", "", "", ReasonUnknown},
	}
	for _, c := range cases {
		r, _ := ix.Resolve(ctx, c.in)
		if r.OK != (c.sym != "") || r.Market.Symbol != c.sym || r.Match != c.match || r.Reason != c.reason {
			t.Errorf("%q: %+v", c.in, r)
		}
	}
	if r, _ := ix.Resolve(ctx, "Pal bar"); r.OK {
		t.Errorf("short fuzzy resolved: %+v", r)
	}
	if r, _ := ix.Resolve(ctx, "Silvr"); r.OK || len(r.Candidates) != 1 || r.Candidates[0].Symbol != "SILVER" {
		t.Fatalf("short typo candidates: %+v", r)
	}
	r, _ := ix.Resolve(ctx, "bar")
	if r.OK || len(r.Candidates) != 2 {
		t.Fatalf("candidates for bar: %+v", r)
	}
	if got := ix.search("", 50); len(got) != 6 {
		t.Fatalf("baskets must not be searchable lines: %d", len(got))
	}
	if res, _ := resolveSymbol(ix, "BKT-AAAAAA", MatchPicked); res.Reason != ReasonBasket {
		t.Fatalf("basket pick: %+v", res)
	}
}

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{{"gold", "gold", 0}, {"glod", "gold", 1}, {"platnum", "platinum", 1}, {"abc", "xyz", 3}, {"silver", "sliver", 1}}
	for _, c := range cases {
		if got := distance([]rune(c.a), []rune(c.b), 3); got != c.want {
			t.Errorf("%s/%s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestTextFilter(t *testing.T) {
	s := &Service{filter: newTextFilter([]string{"scam coin", "rugpull"})}
	ctx := context.Background()
	good := []string{"Gold & silver", "Precious metals (2026)", "Café #1"}
	for _, n := range good {
		if _, err := s.cleanName(ctx, n); err != nil {
			t.Errorf("%q rejected: %v", n, err)
		}
	}
	bad := []string{"ab", "see www.example.com", "mail @me", "Official gold", "R1SK FREE metals", "Scam  Coin basket", "rugpull", "<b>gold</b>",
		strings.Repeat("g", 49)}
	for _, n := range bad {
		if _, err := s.cleanName(ctx, n); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
	if v, err := s.cleanName(ctx, "  Gold   “bars”  "); err == nil {
		t.Errorf("quotes accepted: %q", v)
	}
	if v, err := s.cleanName(ctx, "Gold’s   best"); err != nil || v != "Gold's best" {
		t.Errorf("tidy: %q, %v", v, err)
	}
	if _, err := s.cleanDescription(ctx, "line\n\n\n\n\n\nmore"); err == nil {
		t.Error("6 line breaks accepted")
	}
	SetTextCheck(func(_ context.Context, text string) error {
		if strings.Contains(text, "nope") {
			return errors.New("flagged")
		}
		return nil
	})
	defer SetTextCheck(nil)
	_, err := s.cleanDescription(ctx, "nope")
	var he *httpx.Error
	if !errors.As(err, &he) || he.Code != "description_rejected" {
		t.Fatalf("app check: %v", err)
	}
}

func TestLimits(t *testing.T) {
	now := time.Now()
	ago := func(h float64) time.Time { return now.Add(-time.Duration(h * float64(time.Hour))) }
	if err := checkLimits([]time.Time{ago(1), ago(2)}, now, 5, 20); err != nil {
		t.Fatal(err)
	}
	err := checkLimits([]time.Time{ago(1), ago(2), ago(3), ago(4), ago(20), ago(30)}, now, 5, 20)
	var he *httpx.Error
	if !errors.As(err, &he) || he.Code != "baskets_rate_limited" {
		t.Fatalf("daily: %v", err)
	}
	if secs := he.Details["retry_after_seconds"].(int); secs < 3*3600 || secs > 4*3600 {
		t.Fatalf("retry after %d s, want about 4 h (the 5th newest, 20 h ago, leaves the window)", secs)
	}
	var month []time.Time
	for i := range 20 {
		month = append(month, ago(float64(48+i*24)))
	}
	if err := checkLimits(month, now, 5, 20); !errors.As(err, &he) || !strings.Contains(he.Message, "30 days") {
		t.Fatalf("monthly: %v", err)
	}
}

func TestNextStatus(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	idle, grace := 14*24*time.Hour, 30*24*time.Hour
	day := func(n int) time.Time { return now.AddDate(0, 0, n) }
	ptr := func(t time.Time) *time.Time { return &t }
	zero, some := decimal.Zero, d("100")
	cases := []struct {
		name    string
		status  string
		since   time.Time
		ends    *time.Time
		hidden  bool
		holders int
		volume  decimal.Decimal
		want    string
	}{
		{"new and idle", StatusActive, day(-3), nil, false, 0, zero, ""},
		{"idle 15 days", StatusActive, day(-15), nil, false, 0, zero, StatusArchived},
		{"traded", StatusActive, day(-15), nil, false, 0, some, ""},
		{"held", StatusActive, day(-15), nil, false, 2, zero, ""},
		{"ends later, idle", StatusActive, day(-30), ptr(day(5)), false, 0, zero, ""},
		{"ended, in grace", StatusActive, day(-60), ptr(day(-10)), false, 1, zero, ""},
		{"past grace, held", StatusActive, day(-60), ptr(day(-31)), false, 1, some, StatusReduceOnly},
		{"past grace, empty", StatusActive, day(-60), ptr(day(-31)), false, 0, some, StatusArchived},
		{"hidden, held", StatusActive, day(-1), nil, true, 3, some, StatusReduceOnly},
		{"winding down, held", StatusReduceOnly, day(-1), nil, false, 1, zero, ""},
		{"winding down, closed", StatusReduceOnly, day(-1), nil, false, 0, some, StatusArchived},
		{"archived stays", StatusArchived, day(-100), nil, false, 0, zero, ""},
	}
	for _, c := range cases {
		if got := nextStatus(c.status, c.since, c.ends, c.hidden, c.holders, c.volume, now, idle, grace); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDefaultName(t *testing.T) {
	ev := &evaluation{items: []bitem{
		{symbol: "A", name: "Silver", qty: 40, price: d("30"), priced: true},
		{symbol: "B", name: "Gold", qty: 1, price: d("2600"), priced: true},
		{symbol: "C", name: "Oil", qty: 1, price: d("70"), priced: true},
		{symbol: "D", name: "Wheat", qty: 1, price: d("5"), priced: true},
	}}
	if got := defaultName(ev); got != "Gold, Silver +2" {
		t.Fatalf("got %q", got)
	}
	ev.items = ev.items[:2]
	if got := defaultName(ev); got != "Gold & Silver" {
		t.Fatalf("got %q", got)
	}
}
