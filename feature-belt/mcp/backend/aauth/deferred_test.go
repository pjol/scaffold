package aauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// protocol, Pending Response.
func TestWritePending(t *testing.T) {
	w := httptest.NewRecorder()
	if err := WritePending(w, Pending{Location: "/pending/f7a3b9c", RetryAfter: 0}); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d", w.Code)
	}
	for k, v := range map[string]string{
		"Location":      "/pending/f7a3b9c",
		"Retry-After":   "0",
		"Cache-Control": "no-store",
		"Content-Type":  "application/json",
	} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s: %q", k, got)
		}
	}
	if w.Header().Get(HeaderAAuthRequirement) != "" {
		t.Error("requirement with none given")
	}
	if w.Body.String() != `{"status":"pending"}` {
		t.Errorf("body %s", w.Body)
	}

	// With a requirement, and the example's fields.
	w = httptest.NewRecorder()
	err := WritePending(w, Pending{
		Location:    "/pending/abc123",
		Requirement: &Requirement{Value: RequirementClarification},
		Body:        map[string]any{"clarification": "Why?", "timeout": 120, "status": "ignored"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Header().Get(HeaderAAuthRequirement); got != "requirement=clarification" {
		t.Errorf("requirement %s", got)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "pending" || body["clarification"] != "Why?" || body["timeout"] != float64(120) {
		t.Errorf("body %v", body)
	}

	w = httptest.NewRecorder()
	if err := WritePending(w, Pending{Location: "/p", Status: PendingStatusInteracting, RetryAfter: 5}); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != `{"status":"interacting"}` || w.Header().Get("Retry-After") != "5" {
		t.Errorf("%s %v", w.Body, w.Header())
	}

	// Nothing is written when a required part can't be produced.
	for name, p := range map[string]Pending{
		"no location":     {},
		"negative retry":  {Location: "/p", RetryAfter: -1},
		"bad requirement": {Location: "/p", Requirement: &Requirement{Value: RequirementAuthToken}},
		"bad body":        {Location: "/p", Body: map[string]any{"x": make(chan int)}},
	} {
		w := httptest.NewRecorder()
		if err := WritePending(w, p); err == nil {
			t.Errorf("%s: no error", name)
		}
		if len(w.Header()) != 0 || w.Body.Len() != 0 {
			t.Errorf("%s: wrote a response", name)
		}
	}

	w = httptest.NewRecorder()
	WriteGone(w)
	if w.Code != http.StatusGone {
		t.Errorf("status %d", w.Code)
	}
}

func TestValidatePendingLocation(t *testing.T) {
	const origin = "https://ps.example"
	for _, ok := range []string{"/pending/x", "https://ps.example/pending/x", "pending/x"} {
		if err := ValidatePendingLocation(origin, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "https://other.example/pending/x", "http://ps.example/pending/x", "//other.example/pending/x", "https://ps.example:8443/x", "https://ps.example.evil.example/x"} {
		if err := ValidatePendingLocation(origin, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// RFC 7240, and the examples in protocol, Initial Request.
func TestParsePreferWait(t *testing.T) {
	cases := []struct {
		values []string
		want   int
		ok     bool
	}{
		{[]string{"wait=45"}, 45, true},
		{[]string{"wait=0"}, 0, true},
		{[]string{"WAIT=30"}, 30, true},
		{[]string{`wait="30"`}, 30, true},
		{[]string{"wait = 30"}, 30, true},
		{[]string{"respond-async, wait=30"}, 30, true},
		{[]string{"respond-async", "wait=10"}, 10, true},
		{[]string{"wait=10;foo=bar"}, 10, true},
		{[]string{"wait=10, wait=20"}, 10, true},
		{[]string{`foo="a,wait=99", wait=5`}, 5, true},
		{nil, 0, false},
		{[]string{"respond-async"}, 0, false},
		{[]string{"wait"}, 0, false},
		{[]string{"wait=-1"}, 0, false},
		{[]string{"wait=soon"}, 0, false},
		{[]string{"nowait=5"}, 0, false},
	}
	for _, c := range cases {
		got, ok := ParsePreferWait(c.values)
		if got != c.want || ok != c.ok {
			t.Errorf("%v: %d %v, want %d %v", c.values, got, ok, c.want, c.ok)
		}
	}
	if PreferenceAppliedWait(30) != "wait=30" {
		t.Error(PreferenceAppliedWait(30))
	}
}
