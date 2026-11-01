package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/db"
)

// testPool creates a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE), applies every migration, and drops it afterwards.
func testPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("jobs_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = maxConns
	db.UseUTC(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, true); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestClaimAndFinish(t *testing.T) {
	pool := testPool(t, 4)
	ctx := context.Background()
	const lease, cadence = time.Minute, 400 * time.Millisecond

	started, ok, err := claim(ctx, pool, "a", "test.claim", lease)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v; want it taken", ok, err)
	}
	if _, ok, err := claim(ctx, pool, "b", "test.claim", lease); err != nil || ok {
		t.Fatalf("claim during a's run = %v, %v; want refused", ok, err)
	}
	finish(ctx, pool, "b", "test.claim", started, cadence, errors.New("not b's run"))
	finish(ctx, pool, "a", "test.claim", started, cadence, errors.New("boom"))
	var lastErr string
	var finished *time.Time
	var dueIn time.Duration
	if err := pool.QueryRow(ctx, `SELECT last_error, finished_at, expires_at - started_at FROM job_leases WHERE name = 'test.claim'`).
		Scan(&lastErr, &finished, &dueIn); err != nil {
		t.Fatal(err)
	}
	if lastErr != "boom" || finished == nil || dueIn != cadence {
		t.Fatalf("after a's finish: last_error %q, finished_at %v, due %s after start; want a's outcome, due after %s",
			lastErr, finished, dueIn, cadence)
	}
	if _, ok, _ := claim(ctx, pool, "b", "test.claim", lease); ok {
		t.Fatal("a quick run handed the job on before its next due time")
	}
	time.Sleep(cadence)
	started, ok, err = claim(ctx, pool, "b", "test.claim", lease)
	if err != nil || !ok {
		t.Fatalf("claim at the next due time = %v, %v; want taken", ok, err)
	}

	// A run longer than the cadence hands the job on as soon as it ends.
	time.Sleep(cadence + 50*time.Millisecond)
	finish(ctx, pool, "b", "test.claim", started, cadence, nil)
	if _, ok, err := claim(ctx, pool, "a", "test.claim", lease); err != nil || !ok {
		t.Fatalf("claim after a long run = %v, %v; want taken", ok, err)
	}
}

// Two instances × many jobs on a 2-connection pool: each job needs its own
// connection while it runs, so holding one per job (the old advisory-lock
// design) would starve the pool. Each job must still run about once per
// interval in total, never twice at once.
func TestEveryAcrossInstances(t *testing.T) {
	pool := testPool(t, 2)
	const (
		interval = 300 * time.Millisecond
		window   = 3 * time.Second
		jobCount = 8
	)
	ctx, cancel := context.WithTimeout(context.Background(), window)
	defer cancel()

	var runs, active [jobCount]atomic.Int32
	var overlap atomic.Bool
	var wg sync.WaitGroup
	for _, h := range []string{"instance-a", "instance-b"} {
		for j := range jobCount {
			wg.Go(func() {
				every(ctx, pool, h, fmt.Sprintf("test.job%d", j), interval, func(ctx context.Context) error {
					if active[j].Add(1) > 1 {
						overlap.Store(true)
					}
					defer active[j].Add(-1)
					runs[j].Add(1)
					_, err := pool.Exec(ctx, `SELECT pg_sleep(0.02)`)
					return err
				})
			})
		}
	}
	wg.Wait()

	if overlap.Load() {
		t.Error("a job ran on both instances at once")
	}
	// The first tick lands within one interval; after that a run starts at
	// most once per lease (0.9 × interval) and at least once per interval.
	lo, hi := int32(window/interval)-3, int32(window/(interval-interval/10))+1
	for j := range jobCount {
		n := runs[j].Load()
		t.Logf("test.job%d ran %d times in %s across both instances", j, n, window)
		if n < lo || n > hi {
			t.Errorf("test.job%d ran %d times in %s; want %d–%d (once per %s across both instances)", j, n, window, lo, hi, interval)
		}
	}
}
