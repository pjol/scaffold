package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeExpo answers /send: the first batch fails with 500 every time, later
// batches accept every token except those ending in "dead" (DeviceNotRegistered)
// or "big" (MessageTooBig). The first attempt of every batch after the first
// gets a 429, so retries are exercised too.
func fakeExpo(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	batch := map[string]int{} // first token of a batch → attempts
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var msgs []expoMessage
		if err := json.NewDecoder(r.Body).Decode(&msgs); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if len(msgs) > expoBatchSize {
			t.Errorf("batch of %d exceeds %d", len(msgs), expoBatchSize)
		}
		first := msgs[0].To
		batch[first]++
		if first == "ExponentPushToken[t000]" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if batch[first] == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		tickets := make([]map[string]any, len(msgs))
		for i, m := range msgs {
			switch {
			case len(m.To) > 5 && m.To[len(m.To)-5:] == "dead]":
				tickets[i] = map[string]any{"status": "error", "message": m.To + " is not registered", "details": map[string]string{"error": "DeviceNotRegistered"}}
			case len(m.To) > 4 && m.To[len(m.To)-4:] == "big]":
				tickets[i] = map[string]any{"status": "error", "message": "too big", "details": map[string]string{"error": "MessageTooBig"}}
			default:
				tickets[i] = map[string]any{"status": "ok", "id": "ticket-" + m.To}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": tickets})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestSendAllContinuesPastFailures(t *testing.T) {
	srv, calls := fakeExpo(t)
	c := newExpoClient("", "", false)
	c.sendURL, c.retryBase = srv.URL, time.Millisecond

	// 250 tokens = batches of 100, 100, 50. Batch 1 always fails; batch 2
	// has a dead and a too-big token.
	msgs := make([]expoMessage, 250)
	for i := range msgs {
		msgs[i].To = fmt.Sprintf("ExponentPushToken[t%03d]", i)
	}
	msgs[150].To = "ExponentPushToken[t150-dead]"
	msgs[151].To = "ExponentPushToken[t151-big]"

	out := c.sendAll(context.Background(), "test", msgs)
	if len(out) != len(msgs) {
		t.Fatalf("got %d outcomes, want %d", len(out), len(msgs))
	}
	var failed, dead, ok int
	for i, o := range out {
		if o.Token != msgs[i].To {
			t.Fatalf("outcome %d is for %s, want %s", i, o.Token, msgs[i].To)
		}
		switch {
		case o.Dead:
			dead++
		case o.Err != "":
			failed++
		case o.TicketID != "":
			ok++
		}
	}
	if failed != 101 || dead != 1 || ok != 148 {
		t.Fatalf("failed=%d dead=%d ok=%d, want 101/1/148", failed, dead, ok)
	}
	if out[151].Err != "MessageTooBig" {
		t.Fatalf("rejected message error = %q, want the code only", out[151].Err)
	}
	// Batch 1: 1 + 2 retries; batches 2 and 3: a 429 then success.
	if got := calls.Load(); got != 7 {
		t.Fatalf("expo called %d times, want 7", got)
	}
}

func TestDevelopmentModes(t *testing.T) {
	cases := []struct {
		token, sink string
		dev         bool
		want        string
	}{
		{"", "", false, modeExpo},
		{"tok", "", true, modeExpo},
		{"", ".data/push", true, modeSink},
		{"", "", true, modeLog},
	}
	for _, tc := range cases {
		if got := newExpoClient(tc.token, tc.sink, tc.dev).mode; got != tc.want {
			t.Errorf("token=%q sink=%q dev=%v: mode %s, want %s", tc.token, tc.sink, tc.dev, got, tc.want)
		}
	}
}

func TestSinkWritesAndReportsSent(t *testing.T) {
	dir := t.TempDir()
	c := newExpoClient("", dir, true)
	out := c.sendAll(context.Background(), "chat.message-1", []expoMessage{{To: "ExponentPushToken[a]", Title: "Hi"}})
	if len(out) != 1 || out[0].Err != "" || out[0].TicketID != "" {
		t.Fatalf("sink outcome = %+v", out)
	}
}
