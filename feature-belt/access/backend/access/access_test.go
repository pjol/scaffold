package access

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"scaffoldapp/backend/internal/auth"
)

func TestRefuseAgentWrites(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := refuseAgentWrites(ok)
	cases := []struct {
		method string
		agent  bool
		want   int
	}{
		{http.MethodGet, true, http.StatusNoContent},     // agents read
		{http.MethodPost, false, http.StatusNoContent},   // people write
		{http.MethodPost, true, http.StatusForbidden},    // agents never share
		{http.MethodPut, true, http.StatusForbidden},     // or change general access
		{http.MethodPatch, true, http.StatusForbidden},   // or levels
		{http.MethodDelete, true, http.StatusForbidden},  // or remove anyone
		{http.MethodDelete, false, http.StatusNoContent}, // people may
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "/v1/access/resources/docs.page/x/grants", nil)
		if c.agent {
			r = r.WithContext(auth.WithAgent(r.Context(), auth.Agent{Via: "mcp", Client: "test"}))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s agent=%v: got %d, want %d", c.method, c.agent, w.Code, c.want)
		}
		if c.want == http.StatusForbidden {
			var body struct {
				Error struct{ Code string } `json:"error"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body.Error.Code != "agent_forbidden" {
				t.Errorf("%s agent: code %q, want agent_forbidden", c.method, body.Error.Code)
			}
		}
	}
}

func TestLevelJSON(t *testing.T) {
	for l, name := range levelNames {
		b, err := json.Marshal(l)
		if err != nil || string(b) != `"`+name+`"` {
			t.Errorf("marshal %d: %s %v", l, b, err)
		}
		var back Level
		if err := json.Unmarshal(b, &back); err != nil || back != l {
			t.Errorf("unmarshal %s: %v %v", b, back, err)
		}
	}
	var l Level
	if err := json.Unmarshal([]byte(`"owner"`), &l); err == nil {
		t.Error("owner is not a level")
	}
	if !(None < View && View < Comment && Comment < Edit && Edit < Manage) {
		t.Error("levels must be ordered")
	}
}

func TestHandles(t *testing.T) {
	for in, want := range map[string]string{"Design & Brand": "design-brand", "Café Ünion": "cafe-union", "A": "team-a", "everyone": "team-everyone"} {
		if got := handleFrom(in); got != want {
			t.Errorf("handleFrom(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"a", "-x", "x-", "a--b", "with space", "all"} {
		if _, err := cleanHandle(bad); err == nil {
			t.Errorf("cleanHandle(%q) accepted", bad)
		}
	}
	if h, err := cleanHandle("@Design"); err != nil || h != "design" {
		t.Errorf("cleanHandle(@Design) = %q, %v", h, err)
	}
}
