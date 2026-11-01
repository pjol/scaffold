// Package jobs runs periodic background work on any number of instances.
//
// Each job has a row in job_leases. Every instance ticks every interval (the
// first tick after a random delay, so a boot doesn't start every job at
// once), and a tick claims the job with one statement that succeeds only
// once the previous lease has expired. The claim covers the run (at least
// minLease, so a slow run isn't cut off); when the run ends, the holder
// shortens the lease to the next due time, just under one interval after the
// run started. So each job runs once per interval wherever it runs, runs
// never overlap, an instance that stops between runs hands its jobs on
// within one interval (within minLease if it dies mid-run), and no database
// connection is held while a job works.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/db"
)

const (
	maxStartDelay = 10 * time.Second // the first tick lands within min(interval, this)
	maxSlack      = 30 * time.Second // next due = start + interval - min(interval/10, this)
	minLease      = 5 * time.Minute  // a run may take this long even when the interval is shorter
	finishTimeout = 5 * time.Second
	maxErrorBytes = 1000
)

// holder names this process in job_leases.holder.
var holder = func() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%d:%08x", host, os.Getpid(), rand.Uint32())
}()

// Every runs fn about once per interval until ctx is done, on one instance
// at a time across every instance sharing the database. name keys the lease
// and the logs; keep it stable ("<module>.<job>"). fn's context ends when
// the lease does (the longer of 5 minutes and just under the interval):
// work in batches and return by then. An error is logged and recorded on
// the lease row; the next tick tries again. Use it from a module's Start:
// `go jobs.Every(ctx, d.Pool, "chat.prune", time.Hour, s.prune)`.
func Every(ctx context.Context, pool *pgxpool.Pool, name string, interval time.Duration, fn func(context.Context) error) {
	every(ctx, pool, holder, name, interval, fn)
}

func every(ctx context.Context, pool *pgxpool.Pool, holder, name string, interval time.Duration, fn func(context.Context) error) {
	if interval <= 0 {
		panic("jobs: non-positive interval for " + name)
	}
	cadence := interval - min(interval/10, maxSlack)
	lease := max(cadence, minLease)
	select {
	case <-ctx.Done():
		return
	case <-time.After(rand.N(min(interval, maxStartDelay))):
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		tick(ctx, pool, holder, name, cadence, lease, fn)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// tick claims the job and runs it once if this instance got the lease.
func tick(ctx context.Context, pool *pgxpool.Pool, holder, name string, cadence, lease time.Duration, fn func(context.Context) error) {
	started, ok, err := claim(ctx, pool, holder, name, lease)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, "job lease unavailable", "job", name, "err", err)
		}
		return
	}
	if !ok {
		return // another instance has this interval
	}
	t0 := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, lease)
	err = run(runCtx, name, fn)
	expired := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	cancel()
	if ctx.Err() != nil {
		// Shutting down: hand the job on at its next due time.
		if err != nil {
			err = errors.New("stopped: instance shutting down")
		}
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		finish(sctx, pool, holder, name, started, cadence, err)
		return
	}
	dur := time.Since(t0).Milliseconds()
	if err != nil {
		if expired {
			err = fmt.Errorf("did not finish within its lease (%s): %w", lease, err)
		}
		slog.ErrorContext(ctx, "job failed", "job", name, "dur_ms", dur, "err", err)
	} else {
		slog.DebugContext(ctx, "job ran", "job", name, "dur_ms", dur)
	}
	finish(ctx, pool, holder, name, started, cadence, err)
}

// claim takes the job's lease for holder when it is free (never taken, or
// expired) and reports whether it did: the RETURNING row exists only then.
// One statement, so the connection goes straight back to the pool.
func claim(ctx context.Context, q db.Querier, holder, name string, lease time.Duration) (time.Time, bool, error) {
	var started time.Time
	err := q.QueryRow(ctx, `
		INSERT INTO job_leases AS l (name, holder, expires_at)
		VALUES ($1, $2, now() + $3 * interval '1 millisecond')
		ON CONFLICT (name) DO UPDATE
		   SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at, started_at = now(), finished_at = NULL
		 WHERE l.expires_at <= now()
		RETURNING started_at`, name, holder, lease.Milliseconds()).Scan(&started)
	if db.IsNoRows(err) {
		return time.Time{}, false, nil
	}
	return started, err == nil, err
}

// finish records the outcome on the lease row if this run still owns it and
// shortens the lease to the next due time (started + cadence, or now if the
// run took longer). Not to now: an instance whose ticker is offset would then
// run the job a second time in the same interval.
func finish(ctx context.Context, q db.Querier, holder, name string, started time.Time, cadence time.Duration, runErr error) {
	msg := ""
	if runErr != nil {
		msg = runErr.Error()
		if len(msg) > maxErrorBytes {
			msg = strings.ToValidUTF8(msg[:maxErrorBytes], "")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, finishTimeout)
	defer cancel()
	if _, err := q.Exec(ctx, `
		UPDATE job_leases SET finished_at = now(), last_error = $4,
		       expires_at = LEAST(expires_at, GREATEST(started_at + $5 * interval '1 millisecond', now()))
		 WHERE name = $1 AND holder = $2 AND started_at = $3`,
		name, holder, started, msg, cadence.Milliseconds()); err != nil {
		slog.WarnContext(ctx, "job outcome not recorded", "job", name, "err", err)
	}
}

// run calls fn, turning a panic into an error so one bad job can't take the
// server down.
func run(ctx context.Context, name string, fn func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(ctx, "job panicked", "job", name, "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn(ctx)
}

// LockKey derives a stable advisory-lock key from a name, for
// `SELECT pg_advisory_xact_lock($1)` as the first statement of the
// transaction that does the work (concrete/database.md).
func LockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("job:" + name))
	return int64(h.Sum64() >> 1)
}
