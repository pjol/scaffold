package notifications

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// fakePolicy answers agent levels from a table: "feature|kind|id" for items,
// "feature||" for a whole feature; anything else is read.
type fakePolicy map[string]app.AgentLevel

func (fakePolicy) Name() string { return "fakepolicy" }

func (p fakePolicy) level(feature, kind, id string) app.AgentLevel {
	if l, ok := p[feature+"|"+kind+"|"+id]; ok {
		return l
	}
	if l, ok := p[feature+"||"]; ok {
		return l
	}
	return app.AgentRead
}

func (p fakePolicy) FeatureLevel(_ context.Context, _ *auth.Identity, f string) (app.AgentLevel, app.AgentLevel, error) {
	return p.level(f, "", ""), app.AgentEdit, nil
}

func (p fakePolicy) ResourceLevel(_ context.Context, _ *auth.Identity, f, kind, id string) (app.AgentLevel, error) {
	return p.level(f, kind, id), nil
}

func (p fakePolicy) ResourceLevels(_ context.Context, _ *auth.Identity, f, kind string, ids []string) (map[string]app.AgentLevel, error) {
	out := map[string]app.AgentLevel{}
	for _, id := range ids {
		out[id] = p.level(f, kind, id)
	}
	return out, nil
}

func (fakePolicy) GateTool(context.Context, *auth.Identity, string, app.Tool, json.RawMessage) (app.ToolDecision, error) {
	return app.ToolDecision{Action: app.ToolRun}, nil
}

func TestAgentFilter(t *testing.T) {
	RegisterKind(Kind{ID: "testchan.mention", AgentTarget: DataTarget("testchan.channel", "channel_id")})
	open, closed, page := uuid.NewString(), uuid.NewString(), uuid.NewString()
	items := []Item{
		{ID: uuid.New(), Kind: "testchan.mention", Data: map[string]any{"channel_id": open}},                   // 0: channel readable
		{ID: uuid.New(), Kind: "testchan.mention", Data: map[string]any{"channel_id": closed}},                 // 1: channel off
		{ID: uuid.New(), Kind: "testdocs.comment", Data: map[string]any{"page_id": page}},                      // 2: feature off, no target
		{ID: uuid.New(), Kind: "testshare.shared", Data: map[string]any{"kind": "testpages.page", "id": page}}, // 3: item readable, own feature read
		{ID: uuid.New(), Kind: "testoff.shared", Data: map[string]any{"kind": "testpages.page", "id": page}},   // 4: item readable, own feature off
		{ID: uuid.New(), Kind: "testchan.mention", Data: map[string]any{"channel_id": "not an id!"}},           // 5: bad target: its feature decides (off)
	}
	policy := fakePolicy{
		"testchan|testchan.channel|" + closed: app.AgentOff,
		"testdocs||":                          app.AgentOff,
		"testoff||":                           app.AgentOff,
		"testchan||":                          app.AgentOff, // the item's own level wins over its feature
		"testchan|testchan.channel|" + open:   app.AgentRead,
	}
	s := &Service{d: &app.Deps{Modules: []app.Module{policy}}}
	caller := &auth.Identity{UserID: uuid.New()}

	got, err := s.agentFilter(context.Background(), caller, items)
	if err != nil || len(got) != len(items) {
		t.Fatalf("person: got %d items (err %v), want all %d", len(got), err, len(items))
	}

	ctx := auth.WithAgent(context.Background(), auth.Agent{Via: "mcp", Client: "test"})
	got, err = s.agentFilter(ctx, caller, items)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]bool{items[0].ID: true, items[3].ID: true}
	if len(got) != len(want) {
		t.Fatalf("agent: got %d items, want %d: %+v", len(got), len(want), got)
	}
	for _, it := range got {
		if !want[it.ID] {
			t.Errorf("agent saw %s %v", it.Kind, it.Data)
		}
	}
}
