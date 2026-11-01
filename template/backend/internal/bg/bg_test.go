package bg

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitCoversTasksAndTheirChildren(t *testing.T) {
	r := newRunner()
	var done atomic.Int32
	r.Go(context.Background(), "test.parent", func(ctx context.Context) error {
		time.Sleep(20 * time.Millisecond)
		r.Go(ctx, "test.child", func(context.Context) error {
			time.Sleep(20 * time.Millisecond)
			done.Add(1)
			return nil
		})
		done.Add(1)
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if got := done.Load(); got != 2 {
		t.Fatalf("Wait returned with %d of 2 tasks done", got)
	}
}

func TestTaskOutlivesCallerContext(t *testing.T) {
	r := newRunner()
	type key struct{}
	caller, cancelCaller := context.WithCancel(context.WithValue(context.Background(), key{}, "req-1"))
	result := make(chan error, 1)
	r.Go(caller, "test.detached", func(ctx context.Context) error {
		time.Sleep(20 * time.Millisecond)
		if ctx.Value(key{}) != "req-1" {
			result <- errors.New("lost the caller's values")
		} else {
			result <- ctx.Err()
		}
		return nil
	})
	cancelCaller()
	if err := <-result; err != nil {
		t.Fatalf("task context: %v", err)
	}
}

func TestPanicAndErrorStayInTheTask(t *testing.T) {
	r := newRunner()
	r.Go(context.Background(), "test.panic", func(context.Context) error { panic("boom") })
	r.Go(context.Background(), "test.error", func(context.Context) error { return errors.New("nope") })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWaitDeadlineCancelsAndNamesStragglers(t *testing.T) {
	r := newRunner()
	stopped := make(chan struct{})
	r.Go(context.Background(), "test.stuck", func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := r.Wait(ctx)
	if err == nil || !strings.Contains(err.Error(), "test.stuck x1") {
		t.Fatalf("Wait = %v, want an error naming test.stuck", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("straggler's context was not cancelled")
	}
}
