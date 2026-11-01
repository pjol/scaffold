package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/notify"
)

func withFilters(t *testing.T, add func()) {
	t.Helper()
	filtersMu.Lock()
	saved := filters
	filters = nil
	filtersMu.Unlock()
	t.Cleanup(func() {
		filtersMu.Lock()
		filters = saved
		filtersMu.Unlock()
	})
	add()
}

func TestFiltersOnlyRemoveChannels(t *testing.T) {
	n := notify.Notification{UserID: uuid.New(), Kind: "chat.message", Title: "Hi", Data: map[string]any{"k": "v"}}
	withFilters(t, func() {
		AddDeliveryFilter("hold-push", func(context.Context, notify.Notification) (Hold, error) { return Hold{Push: true}, nil })
		AddDeliveryFilter("hold-nothing", func(context.Context, notify.Notification) (Hold, error) { return Hold{}, nil })
	})
	if push, email := applyFilters(context.Background(), n, true, true); push || !email {
		t.Fatalf("push=%v email=%v, want push held and email kept", push, email)
	}
	// A channel already off stays off whatever filters say.
	if push, email := applyFilters(context.Background(), n, false, false); push || email {
		t.Fatalf("push=%v email=%v, want both off", push, email)
	}
}

func TestFiltersFailOpen(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the filter timeout")
	}
	n := notify.Notification{UserID: uuid.New(), Kind: "chat.message", Title: "Hi"}
	withFilters(t, func() {
		AddDeliveryFilter("errors", func(context.Context, notify.Notification) (Hold, error) {
			return Hold{Push: true, Email: true}, errors.New("lookup failed")
		})
		AddDeliveryFilter("panics", func(context.Context, notify.Notification) (Hold, error) { panic("boom") })
		AddDeliveryFilter("slow", func(ctx context.Context, _ notify.Notification) (Hold, error) {
			select {
			case <-time.After(10 * time.Second):
			case <-ctx.Done():
			}
			return Hold{Push: true, Email: true}, nil
		})
	})
	if push, email := applyFilters(context.Background(), n, true, true); !push || !email {
		t.Fatalf("push=%v email=%v, want both kept when filters fail", push, email)
	}
}

func TestFilterGetsACopyOfData(t *testing.T) {
	n := notify.Notification{UserID: uuid.New(), Kind: "chat.message", Title: "Hi", Data: map[string]any{"k": "v"}}
	withFilters(t, func() {
		AddDeliveryFilter("mutates", func(_ context.Context, n notify.Notification) (Hold, error) {
			n.Data["k"] = "changed"
			return Hold{}, nil
		})
	})
	applyFilters(context.Background(), n, true, true)
	if n.Data["k"] != "v" {
		t.Fatalf("filter changed the caller's data: %v", n.Data)
	}
}

func TestAddDeliveryFilterReplacesByName(t *testing.T) {
	withFilters(t, func() {
		AddDeliveryFilter("x", func(context.Context, notify.Notification) (Hold, error) { return Hold{Push: true}, nil })
		AddDeliveryFilter("x", func(context.Context, notify.Notification) (Hold, error) { return Hold{}, nil })
	})
	if len(filters) != 1 {
		t.Fatalf("got %d filters, want 1", len(filters))
	}
	if push, _ := applyFilters(context.Background(), notify.Notification{Kind: "k"}, true, true); !push {
		t.Fatal("the replaced filter still ran")
	}
}
