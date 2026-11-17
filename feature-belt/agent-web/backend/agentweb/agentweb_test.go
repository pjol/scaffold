package agentweb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
)

type fakeModule struct{ name string }

func (f fakeModule) Name() string { return f.name }

func (f fakeModule) Tools() []app.Tool {
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"],"additionalProperties":false}`)
	return []app.Tool{
		{Name: f.name + "_read", Description: "Read things. More detail.", InputSchema: schema, ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, in json.RawMessage) (any, error) {
				a, ok := auth.AgentFrom(ctx)
				var args map[string]string
				_ = json.Unmarshal(in, &args)
				return map[string]any{"q": args["q"], "via": a.Via, "agent": ok, "user": caller.Email}, nil
			}},
		{Name: f.name + "_write", Description: "Change things.", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: false,
			Handler: func(context.Context, *auth.Identity, json.RawMessage) (any, error) { return "changed", nil }},
	}
}

// offPolicy turns the "hidden" feature off and lets everything else run.
type offPolicy struct{ fakeModule }

func (offPolicy) FeatureLevel(_ context.Context, _ *auth.Identity, feature string) (app.AgentLevel, app.AgentLevel, error) {
	if feature == "hidden" {
		return app.AgentOff, app.AgentOff, nil
	}
	return app.AgentRead, app.AgentRead, nil
}
func (offPolicy) ResourceLevel(context.Context, *auth.Identity, string, string, string) (app.AgentLevel, error) {
	return app.AgentRead, nil
}
func (offPolicy) ResourceLevels(_ context.Context, _ *auth.Identity, _, _ string, ids []string) (map[string]app.AgentLevel, error) {
	out := map[string]app.AgentLevel{}
	for _, id := range ids {
		out[id] = app.AgentRead
	}
	return out, nil
}
func (offPolicy) GateTool(_ context.Context, _ *auth.Identity, feature string, _ app.Tool, _ json.RawMessage) (app.ToolDecision, error) {
	if feature == "hidden" {
		return app.ToolDecision{Action: app.ToolRefuse, Code: "agent_scope_off", Message: "Off."}, nil
	}
	return app.ToolDecision{Action: app.ToolRun}, nil
}

func newTestService() (*Service, http.Handler) {
	d := &app.Deps{Cfg: &config.Config{PublicAPIURL: "https://api.example.test"}}
	s := New(d)
	d.Modules = []app.Module{fakeModule{"notes"}, fakeModule{"hidden"}, offPolicy{fakeModule{"agents"}}, s}
	r := chi.NewRouter()
	r.Group(func(r chi.Router) { s.MountPublic(r) })
	r.Group(func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				id := &auth.Identity{UserID: uuid.New(), Email: "sam@example.test", Status: "active"}
				next.ServeHTTP(w, req.WithContext(auth.WithIdentity(req.Context(), id)))
			})
		})
		s.MountUser(r)
	})
	return s, r
}

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestManifestListsEveryTool(t *testing.T) {
	_, h := newTestService()
	code, m := do(t, h, "GET", "/agent-web/manifest", "")
	if code != 200 {
		t.Fatalf("manifest: %d", code)
	}
	tools := m["tools"].([]any)
	if len(tools) != 6 || m["mcp"] != nil {
		t.Fatalf("manifest = %v", m)
	}
	first := tools[0].(map[string]any)
	if first["name"] != "agents_read" || first["description"] != "Read things." {
		t.Fatalf("first tool = %v", first)
	}
}

func TestWebMCPOffersOnlyVisibleReadTools(t *testing.T) {
	_, h := newTestService()
	_, out := do(t, h, "GET", "/agent-web/tools", "")
	var names []string
	for _, it := range out["items"].([]any) {
		names = append(names, it.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "agents_read,notes_read" {
		t.Fatalf("tools = %v (want read tools of features the scope allows)", names)
	}
}

func TestCallRunsAsTheAgentOfThePerson(t *testing.T) {
	_, h := newTestService()
	code, out := do(t, h, "POST", "/agent-web/tools/notes_read", `{"arguments":{"q":"hi"}}`)
	res, _ := out["result"].(map[string]any)
	if code != 200 || out["status"] != "ok" || res["q"] != "hi" || res["via"] != "webmcp" || res["agent"] != true || res["user"] != "sam@example.test" {
		t.Fatalf("call = %d %v", code, out)
	}
	if code, out := do(t, h, "POST", "/agent-web/tools/notes_read", `{"arguments":{}}`); code != 400 || out["error"].(map[string]any)["code"] != "invalid_input" {
		t.Fatalf("missing argument = %d %v", code, out)
	}
	if code, _ := do(t, h, "POST", "/agent-web/tools/notes_write", `{}`); code != 404 {
		t.Fatalf("write tool reachable: %d", code)
	}
	if code, out := do(t, h, "POST", "/agent-web/tools/hidden_read", `{"arguments":{"q":"x"}}`); code != 403 || out["error"].(map[string]any)["code"] != "agent_scope_off" {
		t.Fatalf("scope off = %d %v", code, out)
	}
}
