package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"scaffoldapp/backend/internal/notify"
)

// Hold names the channels a DeliveryFilter holds back for one notification.
// The zero value holds nothing.
type Hold struct {
	Push  bool
	Email bool
}

// DeliveryFilter decides whether to hold back push and email for one
// notification. The inbox row and the realtime event always go out; filters
// only ever remove channels the kind and the user's preferences turned on.
// It runs in the background after Send returns, with a context that ends
// after filterTimeout. An error, a panic or a timeout is logged and ignored:
// the user's preferences stand.
type DeliveryFilter func(ctx context.Context, n notify.Notification) (Hold, error)

// filterTimeout bounds each filter call.
const filterTimeout = 2 * time.Second

type namedFilter struct {
	name string
	fn   DeliveryFilter
}

var (
	filtersMu sync.RWMutex
	filters   []namedFilter
)

// AddDeliveryFilter registers a filter under a name used in logs
// ("presence.dnd"). Registering a name again replaces that filter. Call it
// from the registering feature's New or its feature_<name>.go; the list is
// read at send time, so construction order never matters.
func AddDeliveryFilter(name string, f DeliveryFilter) {
	name = strings.TrimSpace(name)
	if name == "" || f == nil {
		panic("notifications: AddDeliveryFilter needs a name and a function")
	}
	filtersMu.Lock()
	defer filtersMu.Unlock()
	for i := range filters {
		if filters[i].name == name {
			filters[i].fn = f
			return
		}
	}
	filters = append(filters, namedFilter{name: name, fn: f})
}

func hasFilters() bool {
	filtersMu.RLock()
	defer filtersMu.RUnlock()
	return len(filters) > 0
}

// applyFilters runs every filter in registration order and returns the
// channels still on. It stops early once both are held.
func applyFilters(ctx context.Context, n notify.Notification, push, email bool) (bool, bool) {
	filtersMu.RLock()
	list := append([]namedFilter(nil), filters...)
	filtersMu.RUnlock()
	for _, f := range list {
		if !push && !email {
			break
		}
		h, err := runFilter(ctx, f, n)
		if err != nil {
			slog.WarnContext(ctx, "notifications: delivery filter failed, preferences stand",
				"filter", f.name, "kind", n.Kind, "user_id", n.UserID, "err", err)
			continue
		}
		if h.Push && push {
			push = false
			slog.DebugContext(ctx, "notifications: push held", "filter", f.name, "kind", n.Kind, "user_id", n.UserID)
		}
		if h.Email && email {
			email = false
			slog.DebugContext(ctx, "notifications: email held", "filter", f.name, "kind", n.Kind, "user_id", n.UserID)
		}
	}
	return push, email
}

// runFilter calls one filter with a deadline, turning a panic or a missed
// deadline into an error. A filter that ignores its context keeps running
// in its goroutine, but delivery no longer waits for it.
func runFilter(ctx context.Context, f namedFilter, n notify.Notification) (Hold, error) {
	ctx, cancel := context.WithTimeout(ctx, filterTimeout)
	defer cancel()
	type result struct {
		h   Hold
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- result{err: fmt.Errorf("panic: %v", p)}
			}
		}()
		// The filter gets a copy of Data so it can't change what other
		// filters or the push payload see.
		c := n
		if n.Data != nil {
			c.Data = make(map[string]any, len(n.Data))
			for k, v := range n.Data {
				c.Data[k] = v
			}
		}
		h, err := f.fn(ctx, c)
		done <- result{h, err}
	}()
	select {
	case r := <-done:
		return r.h, r.err
	case <-ctx.Done():
		return Hold{}, fmt.Errorf("timed out after %s", filterTimeout)
	}
}
