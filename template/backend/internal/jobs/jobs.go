// Package jobs runs periodic background work. Every instance runs the loop;
// the advisory lock makes one instance do each tick.
package jobs

import (
	"context"
	"hash/fnv"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/db"
)

// Every runs fn every interval until ctx is done, on at most one instance
// at a time. name keys the advisory lock and the logs; keep it stable.
// Use it from a module's Start: `go jobs.Every(ctx, d.Pool, "chat.prune", time.Hour, s.prune)`.
func Every(ctx context.Context, pool *pgxpool.Pool, name string, interval time.Duration, fn func(context.Context) error) {
	key := LockKey(name)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		ran, err := db.TryAdvisoryLock(ctx, pool, key, fn)
		if err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "job failed", "job", name, "err", err)
		} else if ran {
			slog.DebugContext(ctx, "job ran", "job", name)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// LockKey derives a stable advisory-lock key from a name.
func LockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("job:" + name))
	return int64(h.Sum64() >> 1)
}
