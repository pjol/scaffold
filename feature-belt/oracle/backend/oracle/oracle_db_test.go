package oracle

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
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
		name := fmt.Sprintf("oracle_test_%d", time.Now().UnixNano())
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

func testDBService(t *testing.T) *Service {
	t.Helper()
	pool := testPool(t)
	t.Setenv("ORACLE_SOURCES_ENABLED", "false")
	t.Setenv("ORACLE_PUBLISH_INTERVAL", "500ms")
	return New(&app.Deps{Pool: pool, Cfg: &config.Config{Env: "development"}, Broker: realtime.NewMemory()})
}

// manualFeed creates a single with manual sources at the given venues.
func manualFeed(t *testing.T, s *Service, profile string, venues ...string) (string, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	key := fmt.Sprintf("T%d", time.Now().UnixNano()%1e15)
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		seed := 100.0
		if id, _, err = seedFeed(ctx, tx, key, "Test "+key, "Test", FeedSingle, &seed, Profiles[profile], true); err != nil {
			return err
		}
		for _, v := range venues {
			if _, err := tx.Exec(ctx, `INSERT INTO oracle_feed_sources (feed_id, source, venue, premium) VALUES ($1, $2, $3, 0)`, id, ManualSource, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return key, id
}

func count(t *testing.T, s *Service, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.d.Pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Two feeds served by one venue may reuse its trade ids: dedupe is per feed.
func TestPushTradeIDsArePerFeed(t *testing.T) {
	s := testDBService(t)
	ctx := context.Background()
	a, aID := manualFeed(t, s, "daily", "dealer-a")
	b, bID := manualFeed(t, s, "daily", "dealer-a")
	p := Print{Venue: "dealer-a", Kind: KindSale, Price: 101, At: time.Now(), ID: "trade-42"}
	for range 2 { // the retry is a no-op
		for _, k := range []string{a, b} {
			if err := s.Push(ctx, s.d.Pool, k, ManualSource, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []uuid.UUID{aID, bID} {
		if n := count(t, s, `SELECT count(*) FROM oracle_inbox WHERE feed_id = $1`, id); n != 1 {
			t.Fatalf("feed %s: %d inbox rows, want 1", id, n)
		}
	}
}

// Push refuses prints the pipeline would drop instead of accepting and losing them.
func TestPushChecksTime(t *testing.T) {
	s := testDBService(t)
	ctx := context.Background()
	key, _ := manualFeed(t, s, "realtime", "desk") // 2-day lookback
	for _, at := range []time.Time{time.Now().Add(-72 * time.Hour), time.Now().Add(time.Hour)} {
		err := s.Push(ctx, s.d.Pool, key, ManualSource, Print{Venue: "desk", Kind: KindSale, Price: 100, At: at})
		if err == nil || !strings.Contains(err.Error(), "observed_at") {
			t.Fatalf("Push at %s: err = %v, want an observed_at error", at, err)
		}
	}
	if err := s.Push(ctx, s.d.Pool, key, ManualSource, Print{Venue: "desk", Kind: KindSale, Price: 100, At: time.Now().Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

// Prints pushed the moment a feed is created, before the leader has loaded
// it, are screened once it has (they used to be claimed and dropped).
func TestPushRightAfterFeedCreated(t *testing.T) {
	s := testDBService(t)
	manualFeed(t, s, "daily", "desk") // the leader has a catalog, just not the new feed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	deadline := time.Now().Add(20 * time.Second)
	for {
		s.mu.RLock()
		leading := s.leading
		s.mu.RUnlock()
		if leading {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never took the lease")
		}
		time.Sleep(50 * time.Millisecond)
	}
	key, id := manualFeed(t, s, "realtime", "desk-a", "desk-b")
	const n = 12
	for i := range n {
		venue := []string{"desk-a", "desk-b"}[i%2]
		p := Print{Venue: venue, Kind: KindSale, Price: 100 + float64(i%3)/10, At: time.Now(), ID: fmt.Sprintf("p%d", i)}
		if err := s.Push(ctx, s.d.Pool, key, ManualSource, p); err != nil {
			t.Fatal(err)
		}
	}
	deadline = time.Now().Add(20 * time.Second)
	for count(t, s, `SELECT count(*) FROM oracle_observations WHERE feed_id = $1`, id) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d prints became observations (inbox taken: %d)", count(t, s, `SELECT count(*) FROM oracle_observations WHERE feed_id = $1`, id), n,
				count(t, s, `SELECT count(*) FROM oracle_inbox WHERE feed_id = $1 AND taken_at IS NOT NULL`, id))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pp, ok, err := s.Current(ctx, key); err != nil || !ok || pp.Price <= 0 {
		t.Fatalf("Current(%s) = %+v, %v, %v", key, pp, ok, err)
	}
}
