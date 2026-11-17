package oracle

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
)

func TestCheckBasketFeed(t *testing.T) {
	ok := BasketFeed{Key: " bkt-ab12 ", Name: "Metals", Decimals: 4, Parts: []BasketPart{{Key: "gold-oz", Quantity: 1}, {Key: "SILVER-OZ", Quantity: 40}}}
	b, err := checkBasketFeed(ok)
	if err != nil {
		t.Fatal(err)
	}
	if b.Key != "BKT-AB12" || b.Quote != "USD" || b.Parts[0].Key != "GOLD-OZ" {
		t.Fatalf("not tidied: %+v", b)
	}
	bad := map[string]BasketFeed{
		"key":       {Key: "-X", Name: "x", Parts: ok.Parts},
		"name":      {Key: "B1", Name: " ", Parts: ok.Parts},
		"decimals":  {Key: "B1", Name: "x", Decimals: 11, Parts: ok.Parts},
		"no parts":  {Key: "B1", Name: "x"},
		"self":      {Key: "B1", Name: "x", Parts: []BasketPart{{Key: "b1", Quantity: 1}}},
		"twice":     {Key: "B1", Name: "x", Parts: []BasketPart{{Key: "A", Quantity: 1}, {Key: "a", Quantity: 2}}},
		"zero qty":  {Key: "B1", Name: "x", Parts: []BasketPart{{Key: "A", Quantity: 0}}},
		"nan qty":   {Key: "B1", Name: "x", Parts: []BasketPart{{Key: "A", Quantity: math.NaN()}}},
		"inf qty":   {Key: "B1", Name: "x", Parts: []BasketPart{{Key: "A", Quantity: math.Inf(1)}}},
		"long cat":  {Key: "B1", Name: "x", Category: strings.Repeat("c", 61), Parts: ok.Parts},
		"long name": {Key: "B1", Name: strings.Repeat("n", 121), Parts: ok.Parts},
	}
	for name, b := range bad {
		if _, err := checkBasketFeed(b); err == nil {
			t.Errorf("%s: accepted %+v", name, b)
		}
	}
}

// basketUpdatedAt reads a feed's updated_at (the catalog signature input).
func basketUpdatedAt(t *testing.T, s *Service, key string) time.Time {
	t.Helper()
	var at time.Time
	if err := s.d.Pool.QueryRow(context.Background(), `SELECT updated_at FROM oracle_feeds WHERE key = $1`, key).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func ensureTx(s *Service, b BasketFeed) error {
	ctx := context.Background()
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return s.EnsureBasketFeed(ctx, tx, b) })
}

// EnsureBasketFeed creates once, is a no-op when nothing changed, replaces
// parts when they change, and refuses singles and basket parts.
func TestEnsureBasketFeed(t *testing.T) {
	s := testDBService(t)
	ctx := context.Background()
	a, _ := manualFeed(t, s, "daily", "desk")
	b, _ := manualFeed(t, s, "daily", "desk")
	key := "BKT-" + a
	in := BasketFeed{Key: key, Name: "Test basket", Category: "Baskets", Decimals: 4, Parts: []BasketPart{{Key: a, Quantity: 2}, {Key: b, Quantity: 3}}}
	if err := ensureTx(s, in); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, `SELECT count(*) FROM oracle_basket_parts p JOIN oracle_feeds f ON f.id = p.basket_id WHERE f.key = $1`, key); n != 2 {
		t.Fatalf("%d parts, want 2", n)
	}
	if n := count(t, s, `SELECT count(*) FROM oracle_feeds WHERE key = $1 AND kind = 'basket' AND NOT listed AND enabled AND decimals = 4`, key); n != 1 {
		t.Fatal("basket feed not created as an unlisted, enabled basket with 4 decimals")
	}
	before := basketUpdatedAt(t, s, key)
	time.Sleep(10 * time.Millisecond)
	if err := ensureTx(s, in); err != nil {
		t.Fatal(err)
	}
	if after := basketUpdatedAt(t, s, key); !after.Equal(before) {
		t.Fatalf("unchanged basket was touched: %s → %s", before, after)
	}
	in.Parts = []BasketPart{{Key: a, Quantity: 5}}
	if err := ensureTx(s, in); err != nil {
		t.Fatal(err)
	}
	var qty float64
	if err := s.d.Pool.QueryRow(ctx, `SELECT sum(quantity)::float8 FROM oracle_basket_parts p JOIN oracle_feeds f ON f.id = p.basket_id WHERE f.key = $1`, key).Scan(&qty); err != nil || qty != 5 {
		t.Fatalf("parts not replaced: qty %v, err %v", qty, err)
	}
	if after := basketUpdatedAt(t, s, key); !after.After(before) {
		t.Fatal("changed basket kept its updated_at (the leader would never reload it)")
	}
	if err := ensureTx(s, BasketFeed{Key: a, Name: "x", Parts: []BasketPart{{Key: b, Quantity: 1}}}); !errors.Is(err, ErrNotBasket) {
		t.Fatalf("a single's key: err = %v, want ErrNotBasket", err)
	}
	if err := ensureTx(s, BasketFeed{Key: key + "-2", Name: "x", Parts: []BasketPart{{Key: key, Quantity: 1}}}); err == nil {
		t.Fatal("a basket as a part was accepted")
	}
	if err := ensureTx(s, BasketFeed{Key: key + "-3", Name: "x", Parts: []BasketPart{{Key: "NO-SUCH-FEED-X", Quantity: 1}}}); err == nil {
		t.Fatal("an unknown part was accepted")
	}
}

// SetFeedEnabled switches a feed; CurrentMany reports kinds and off status
// and leaves unknown keys out.
func TestSetFeedEnabledAndCurrentMany(t *testing.T) {
	s := testDBService(t)
	ctx := context.Background()
	a, _ := manualFeed(t, s, "daily", "desk")
	key := "BKT-E" + a
	if err := ensureTx(s, BasketFeed{Key: key, Name: "Off and on", Decimals: 4, Parts: []BasketPart{{Key: a, Quantity: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFeedEnabled(ctx, s.d.Pool, key, false); err != nil {
		t.Fatal(err)
	}
	m, err := s.CurrentMany(ctx, []string{a, key, "NO-SUCH-FEED-X"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m["NO-SUCH-FEED-X"]; ok || len(m) != 2 {
		t.Fatalf("CurrentMany = %+v", m)
	}
	if m[a].Kind != FeedSingle || m[key].Kind != FeedBasket || m[key].Status != StatusOff {
		t.Fatalf("kinds/status: %+v", m)
	}
	if err := s.SetFeedEnabled(ctx, s.d.Pool, key, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFeedEnabled(ctx, s.d.Pool, key, true); err != nil {
		t.Fatal("enabling an enabled feed should be a no-op:", err)
	}
	if err := s.SetFeedEnabled(ctx, s.d.Pool, "NO-SUCH-FEED-X", false); !errors.Is(err, ErrUnknownFeed) {
		t.Fatalf("unknown key: err = %v", err)
	}
	if pp, ok, err := s.Current(ctx, key); err != nil || !ok || pp.Status == StatusOff {
		t.Fatalf("Current after enabling = %+v, %v, %v", pp, ok, err)
	}
}
