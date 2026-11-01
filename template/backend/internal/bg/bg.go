// Package bg runs best-effort work that must outlive the request that
// started it (notification fan-out, push, email, provider calls) and lets
// the process wait for that work before it closes the pool: cmd/server
// waits on shutdown, cmd/seed before it exits.
package bg

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

var std = newRunner()

// Go runs fn in a new goroutine with ctx's values but not its cancellation,
// so the work survives the end of the request that started it. A returned
// error is logged as a warning and a panic is recovered and logged with its
// stack; neither reaches the caller. name is "<module>.<what>" for the logs.
// fn bounds its own run time (context.WithTimeout); its context is
// cancelled only when Wait gives up at shutdown.
func Go(ctx context.Context, name string, fn func(ctx context.Context) error) { std.Go(ctx, name, fn) }

// Wait blocks until every task started with Go has returned (including tasks
// those tasks started), or until ctx is done; then it cancels the tasks still
// running and returns an error naming them. Call it once, at process exit.
func Wait(ctx context.Context) error { return std.Wait(ctx) }

type runner struct {
	mu      sync.Mutex
	n       int
	running map[string]int
	idle    chan struct{} // closed while n == 0
	stop    context.Context
	cancel  context.CancelFunc // ends every task's context
}

func newRunner() *runner {
	stop, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	close(idle)
	return &runner{running: map[string]int{}, idle: idle, stop: stop, cancel: cancel}
}

func (r *runner) Go(ctx context.Context, name string, fn func(ctx context.Context) error) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	unhook := context.AfterFunc(r.stop, cancel)
	r.mu.Lock()
	if r.n == 0 {
		r.idle = make(chan struct{})
	}
	r.n++
	r.running[name]++
	r.mu.Unlock()

	go func() {
		defer r.done(name)
		defer unhook()
		defer cancel()
		defer func() {
			if p := recover(); p != nil {
				slog.ErrorContext(ctx, "background task panicked", "task", name, "panic", p, "stack", string(debug.Stack()))
			}
		}()
		if err := fn(ctx); err != nil {
			slog.WarnContext(ctx, "background task failed", "task", name, "err", err)
		}
	}()
}

func (r *runner) done(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[name]--; r.running[name] == 0 {
		delete(r.running, name)
	}
	if r.n--; r.n == 0 {
		close(r.idle)
	}
}

func (r *runner) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		idle := r.idle
		r.mu.Unlock()
		select {
		case <-idle:
			r.mu.Lock()
			n := r.n
			r.mu.Unlock()
			if n == 0 {
				return nil
			}
			// Something started after the last task ended: wait for it too.
		case <-ctx.Done():
			r.cancel()
			r.mu.Lock()
			defer r.mu.Unlock()
			names := make([]string, 0, len(r.running))
			for name, n := range r.running {
				names = append(names, fmt.Sprintf("%s x%d", name, n))
			}
			sort.Strings(names)
			return fmt.Errorf("bg: %d tasks still running, cancelled: %s", r.n, strings.Join(names, ", "))
		}
	}
}
