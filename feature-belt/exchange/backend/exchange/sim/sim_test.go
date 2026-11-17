package sim

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange/engine"
)

func TestSimulationTradesAndQuotesBothSides(t *testing.T) {
	d := decimal.RequireFromString
	p := engine.DefaultParams()
	p.AllowSeedPrices = true
	e := engine.New(p, &engine.MemStore{}, slog.Default(), []engine.MarketSpec{
		{ID: uuid.NewString(), Symbol: "AAA", Name: "A", MaxLeverage: 10, SzDecimals: 3, TickSize: d("0.1"), MinNotional: d("10"),
			OICap: d("1000000"), SeedPrice: d("2650"), Status: engine.MarketActive},
		{ID: uuid.NewString(), Symbol: "BBB", Name: "B", MaxLeverage: 5, SzDecimals: 1, TickSize: d("0.0025"), MinNotional: d("10"),
			OICap: d("250000"), SeedPrice: d("5.8"), Status: engine.MarketActive},
	})
	if err := e.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := New(e, slog.Default(), rand.New(rand.NewPCG(1, 2)))
	if err != nil {
		t.Fatal(err)
	}
	trades := 0
	for i := 0; i < 2000; i++ {
		sym := []string{"AAA", "BBB"}[i%2]
		if i%5 == 0 {
			s.quote(sym)
		}
		s.act(s.bot[s.brng.IntN(len(s.bot))], sym)
	}
	for _, sym := range []string{"AAA", "BBB"} {
		tr, _ := e.Trades(sym, 200)
		trades += len(tr)
		b, _ := e.Book(sym, 5)
		if len(b.Bids) == 0 || len(b.Asks) == 0 {
			t.Fatalf("%s: the market maker should quote both sides: %+v", sym, b)
		}
		m, _ := e.Market(sym)
		mark := d(m.Stats.MarkPx)
		idx := d(m.Stats.IndexPx)
		if mark.Sub(idx).Abs().Div(idx).GreaterThan(d("0.1")) {
			t.Fatalf("%s: mark %s strayed from index %s", sym, mark, idx)
		}
	}
	if trades < 100 {
		t.Fatalf("expected a lively tape, got %d trades", trades)
	}
}
