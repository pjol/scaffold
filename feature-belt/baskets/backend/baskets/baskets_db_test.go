package baskets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/oracle"
	"scaffoldapp/backend/internal/realtime"
)

// Database tests: a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE) with every migration applied, shared by the tests below.

var (
	dbOnce sync.Once
	dbPool *pgxpool.Pool
	dbErr  error
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	dbOnce.Do(func() {
		ctx := context.Background()
		admin, err := pgx.Connect(ctx, base)
		if err != nil {
			dbErr = err
			return
		}
		defer admin.Close(ctx)
		name := fmt.Sprintf("baskets_test_%d", time.Now().UnixNano())
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			dbErr = err
			return
		}
		cfg, err := pgxpool.ParseConfig(base)
		if err != nil {
			dbErr = err
			return
		}
		cfg.ConnConfig.Database = name
		if dbPool, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
			dbErr = err
			return
		}
		dbErr = db.Migrate(ctx, dbPool, false)
	})
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	return dbPool
}

type fixture struct {
	s       *Service
	tag     string // unique per test: market symbols and feed keys
	a, b    string // two listed markets with oracle single feeds
	u1, u2  uuid.UUID
	prices  map[string]string
	oracle_ *oracle.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	t.Setenv("ORACLE_SOURCES_ENABLED", "false")
	t.Setenv("BASKETS_PER_DAY", "2")
	d := &app.Deps{Pool: pool, Cfg: &config.Config{Env: "development"}, Broker: realtime.NewMemory()}
	o := oracle.New(d)
	s := New(d)
	d.Modules = []app.Module{o, s}
	f := &fixture{s: s, oracle_: o, tag: fmt.Sprintf("T%d", time.Now().UnixNano()%1e9)}
	f.a, f.b = f.tag+"A", f.tag+"B"
	f.prices = map[string]string{f.a: "100", f.b: "2.5"}
	for _, sym := range []string{f.a, f.b} {
		if _, err := pool.Exec(ctx, `INSERT INTO oracle_feeds (key, name, kind, half_life_s, stale_after_s, max_move_bps_per_min, lookback_s, ref_window_s, burst_window_s)
 VALUES ($1, $2, 'single', 3600, 3600, 10, 86400, 86400, 3600)`, sym+"-F", "Feed "+sym); err != nil {
			t.Fatal(err)
		}
	}
	if err := exchange.UpsertCatalog(ctx, pool, []exchange.CatalogMarket{
		{Symbol: f.a, Name: "Alpha " + f.tag, PriceFeed: f.a + "-F", MaxLeverage: 5, SzDecimals: 2, TickSize: "0.01", MinNotional: "10", OICap: "100000"},
		{Symbol: f.b, Name: "Beta " + f.tag, PriceFeed: f.b + "-F", MaxLeverage: 5, SzDecimals: 2, TickSize: "0.01", MinNotional: "10", OICap: "100000"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*uuid.UUID{&f.u1, &f.u2} {
		if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $2, 'Sam') RETURNING id`,
			"test_"+uuid.NewString(), uuid.NewString()+"@example.com").Scan(u); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// eval evaluates picks and prices the lines from the fixture (no oracle pipeline runs in tests).
func (f *fixture) eval(t *testing.T, picks ...Pick) *evaluation {
	t.Helper()
	ev, err := f.s.evaluate(context.Background(), Input{Picks: picks})
	if err != nil {
		t.Fatal(err)
	}
	for i := range ev.items {
		it := &ev.items[i]
		it.price, it.priced = d(f.prices[it.symbol]), true
	}
	ev.totals()
	return ev
}

func (f *fixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.s.d.Pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func httpCode(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he.Code
	}
	return ""
}

// Create lists once (basket, lines, entry, oracle basket feed, exchange
// market); the same lines from someone else reuse it; archiving halts the
// market and turns the feed off; creating again revives both.
func TestCreateReuseArchiveRevive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ev := f.eval(t, Pick{f.a, 1}, Pick{f.b, 40})
	if ev.unresolved != 0 || len(ev.items) != 2 || ev.value.String() != "200" {
		t.Fatalf("evaluation: unresolved %d, items %d, value %s, lines %+v", ev.unresolved, len(ev.items), ev.value, ev.lines)
	}
	res, b, err := f.s.commit(ctx, f.u1, "web", ev, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || !res.Joined || b.Entries != 1 || b.Name != "Alpha "+f.tag+" & Beta "+f.tag {
		t.Fatalf("created: %+v, basket %+v", res, b)
	}
	if n := f.count(t, `SELECT count(*) FROM exchange_markets WHERE symbol = $1 AND price_feed = $1 AND category = 'Baskets' AND status = 'active'`, b.Symbol); n != 1 {
		t.Fatal("exchange market not listed with the basket feed")
	}
	if n := f.count(t, `SELECT count(*) FROM oracle_basket_parts p JOIN oracle_feeds o ON o.id = p.basket_id WHERE o.key = $1 AND o.kind = 'basket'`, b.Symbol); n != 2 {
		t.Fatalf("oracle basket feed has %d parts", n)
	}

	// The same lines in another order, from someone else: same market, an entry.
	res2, b2, err := f.s.commit(ctx, f.u2, "ios", f.eval(t, Pick{f.b, 40}, Pick{f.a, 1}), "Other name", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Created || !res2.Joined || b2.Symbol != b.Symbol || b2.Entries != 2 || b2.Name != b.Name {
		t.Fatalf("reuse: %+v, basket %+v", res2, b2)
	}
	if res3, _, err := f.s.commit(ctx, f.u2, "ios", f.eval(t, Pick{f.a, 1}, Pick{f.b, 40}), "", "", nil); err != nil || res3.Joined {
		t.Fatalf("second entry by the same person: %+v, %v", res3, err)
	}

	// Archive (as the lifecycle job does), then revive by creating again.
	if err := db.WithTx(ctx, f.s.d.Pool, func(tx pgx.Tx) error { return f.s.setStatus(ctx, tx, b, StatusArchived) }); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM exchange_markets WHERE symbol = $1 AND status = 'halted'`, b.Symbol); n != 1 {
		t.Fatal("archived basket's market isn't halted")
	}
	if n := f.count(t, `SELECT count(*) FROM oracle_feeds WHERE key = $1 AND NOT enabled`, b.Symbol); n != 1 {
		t.Fatal("archived basket's feed is still on")
	}
	res4, b4, err := f.s.commit(ctx, f.u1, "web", f.eval(t, Pick{f.a, 1}, Pick{f.b, 40}), "", "", nil)
	if err != nil || !res4.Revived || b4.Status != StatusActive {
		t.Fatalf("revive: %+v, %+v, %v", res4, b4, err)
	}
	if n := f.count(t, `SELECT count(*) FROM exchange_markets m JOIN oracle_feeds o ON o.key = m.symbol WHERE m.symbol = $1 AND m.status = 'active' AND o.enabled`, b.Symbol); n != 1 {
		t.Fatal("revived basket's market or feed is still off")
	}

	// Staff-hidden baskets never revive.
	if _, err := f.s.d.Pool.Exec(ctx, `UPDATE baskets SET hidden = true, status = 'archived' WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.commit(ctx, f.u2, "web", f.eval(t, Pick{f.a, 1}, Pick{f.b, 40}), "", "", nil); httpCode(err) != "basket_hidden" {
		t.Fatalf("hidden: err = %v", err)
	}
}

// Per-person limits count new listings, not joins; deletion keeps the market
// and drops the person.
func TestLimitsAndDeletion(t *testing.T) {
	f := newFixture(t) // BASKETS_PER_DAY=2
	ctx := context.Background()
	var last basketRow
	for i := 1; i <= 2; i++ {
		_, b, err := f.s.commit(ctx, f.u1, "web", f.eval(t, Pick{f.a, i}, Pick{f.b, 10}), "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		last = b
	}
	if _, _, err := f.s.commit(ctx, f.u1, "web", f.eval(t, Pick{f.a, 3}, Pick{f.b, 10}), "", "", nil); httpCode(err) != "baskets_rate_limited" {
		t.Fatalf("third basket today: err = %v", err)
	}
	if _, _, err := f.s.commit(ctx, f.u1, "web", f.eval(t, Pick{f.a, 2}, Pick{f.b, 10}), "", "", nil); err != nil {
		t.Fatal("joining an existing basket counted against the limit:", err)
	}
	if _, _, err := f.s.commit(ctx, f.u2, "web", f.eval(t, Pick{f.a, 2}, Pick{f.b, 10}), "", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.WithTx(ctx, f.s.d.Pool, func(tx pgx.Tx) error { return f.s.DeleteAccount(ctx, tx, f.u2) }); err != nil {
		t.Fatal(err)
	}
	if n := f.count(t, `SELECT count(*) FROM basket_entries WHERE user_id = $1`, f.u2); n != 0 {
		t.Fatal("entries survived deletion")
	}
	if n := f.count(t, `SELECT entries FROM baskets WHERE id = $1`, last.ID); n != 1 {
		t.Fatalf("entries count after deletion = %d, want 1", n)
	}
	if err := db.WithTx(ctx, f.s.d.Pool, func(tx pgx.Tx) error { return f.s.DeleteAccount(ctx, tx, f.u1) }); err != nil {
		t.Fatal(err)
	}
	b, err := basketBy(ctx, f.s.d.Pool, "id", last.ID.String())
	if err != nil || b.CreatedBy != nil || b.Creator != "Deleted user" {
		t.Fatalf("after the creator's deletion: %+v, %v", b, err)
	}
}

// Preview names the symbol a new basket would get, and the existing one after.
func TestPreviewExisting(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p, err := f.s.Preview(ctx, f.u1, Input{Text: fmt.Sprintf("1 %s\n%s x 40\nunobtainium", f.a, f.b)})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lines) != 3 || p.Lines[2].Status != LineUnresolved || p.Creatable || p.Existing != nil {
		t.Fatalf("preview: %+v", p)
	}
	if p.Lines[0].Status != LineUnpriced { // no oracle prices published in tests
		t.Fatalf("line 1: %+v", p.Lines[0])
	}
	_, b, err := f.s.commit(ctx, f.u1, "web", f.eval(t, Pick{f.a, 1}, Pick{f.b, 40}), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err = f.s.Preview(ctx, f.u2, Input{Text: fmt.Sprintf("%s, 40\n%s", f.b, f.a)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Existing == nil || p.Existing.Symbol != b.Symbol || p.Existing.Entered || !p.Creatable || p.Symbol == nil || *p.Symbol != b.Symbol {
		t.Fatalf("preview of an existing basket: %+v (existing %+v)", p, p.Existing)
	}
	if p, _ = f.s.Preview(ctx, f.u1, Input{Picks: []Pick{{b.Symbol, 1}, {f.a, 1}}}); p.Lines[0].Reason != ReasonBasket {
		t.Fatalf("a basket as a line: %+v", p.Lines[0])
	}
}

// Discovery pages by keyset in both orders and leaves hidden baskets out.
func TestListPaging(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var syms []string
	for i := 1; i <= 2; i++ {
		_, b, err := f.s.commit(ctx, f.u1, "web", f.eval(t, Pick{f.a, i}, Pick{f.b, 3}), "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		syms = append(syms, b.Symbol)
	}
	for _, sort := range []string{"new", "popular"} {
		seen := map[string]bool{}
		var after *listCursor
		for page := 0; page < 50; page++ {
			rows, err := f.s.list(ctx, listQuery{sort: sort, status: "all", q: f.tag, limit: 1, after: after})
			if err != nil {
				t.Fatalf("%s page %d: %v", sort, page, err)
			}
			if len(rows) == 0 {
				break
			}
			b := rows[0]
			if seen[b.Symbol] {
				t.Fatalf("%s: %s twice", sort, b.Symbol)
			}
			seen[b.Symbol] = true
			after = &listCursor{T: b.CreatedAt, ID: b.ID, P: b.Popularity.String()}
		}
		for _, s := range syms {
			if !seen[s] {
				t.Fatalf("%s: %s never listed (%v)", sort, s, seen)
			}
		}
	}
	if _, err := f.s.d.Pool.Exec(ctx, `UPDATE baskets SET hidden = true WHERE symbol = $1`, syms[0]); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.list(ctx, listQuery{sort: "new", status: "all", q: f.tag, limit: 10})
	if err != nil || len(rows) != 1 || rows[0].Symbol != syms[1] {
		t.Fatalf("hidden basket listed: %d rows, %v", len(rows), err)
	}
}
