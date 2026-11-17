package exchange

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCatalogValidation(t *testing.T) {
	ok := CatalogMarket{Symbol: "gold", Name: "Gold", MaxLeverage: 20, SzDecimals: 3, TickSize: "0.1", MinNotional: "10", OICap: "1000"}
	p, problem := ok.validate()
	if problem != "" || p.Symbol != "GOLD" || p.Status != "active" {
		t.Fatalf("valid entry: %q %+v", problem, p)
	}
	bad := []CatalogMarket{
		{Symbol: "BAD SYMBOL", Name: "x", MaxLeverage: 1, TickSize: "1", MinNotional: "0", OICap: "1"},
		{Symbol: "X", Name: "", MaxLeverage: 1, TickSize: "1", MinNotional: "0", OICap: "1"},
		{Symbol: "X", Name: "x", MaxLeverage: 0, TickSize: "1", MinNotional: "0", OICap: "1"},
		{Symbol: "X", Name: "x", MaxLeverage: 1, TickSize: "0", MinNotional: "0", OICap: "1"},
		{Symbol: "X", Name: "x", MaxLeverage: 1, TickSize: "0.000000001", MinNotional: "0", OICap: "1"},
		{Symbol: "X", Name: "x", MaxLeverage: 1, TickSize: "1", MinNotional: "-1", OICap: "1"},
		{Symbol: "X", Name: "x", MaxLeverage: 1, TickSize: "1", MinNotional: "0", OICap: "0"},
		{Symbol: "X", Name: "x", MaxLeverage: 1, TickSize: "1", MinNotional: "0", OICap: "1", Status: "open"},
		{Symbol: "X", Name: "x", MaxLeverage: 1, TickSize: "1", MinNotional: "0", OICap: "1", ImageURL: "javascript:alert(1)"},
	}
	for _, c := range bad {
		if _, problem := c.validate(); problem == "" {
			t.Errorf("should be invalid: %+v", c)
		}
	}
}

func TestCleanBars(t *testing.T) {
	d := decimal.RequireFromString
	day := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	out := cleanBars([]DailyBar{
		{Day: day, O: d("10"), H: d("9"), L: d("11"), C: d("10.5")},                // h/l fixed up
		{Day: day.Add(2 * time.Hour), O: d("1"), H: d("1"), L: d("1"), C: d("1")},  // same day: last wins
		{Day: day.Add(24 * time.Hour), O: d("0"), H: d("1"), L: d("1"), C: d("1")}, // invalid
	})
	if len(out) != 1 || !out[0].C.Equal(d("1")) || !out[0].Day.Equal(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("clean: %+v", out)
	}
}
