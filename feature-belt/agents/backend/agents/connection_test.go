package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// fakeDirectory is the connectors feature's ConnectionDirectory, as far as
// agents needs it.
type fakeDirectory struct{ conns []app.Connection }

func (fakeDirectory) Name() string { return "connectors" }

func (f fakeDirectory) All(context.Context, uuid.UUID) ([]app.Connection, error) { return f.conns, nil }

// A write to an item on a connected platform ("crm.external",
// "<connection>:<id>") takes the lower of the item's level and the
// connection's own level (its setting, else the default): CRM at Edit with
// HubSpot at Suggest suggests; HubSpot at Read or Off refuses; reads follow
// the same rule.
func TestGateToolCapsByConnection(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	conn := uuid.New().String()
	d := &app.Deps{Pool: pool}
	s := New(d)
	d.Modules = []app.Module{s, fakeDirectory{conns: []app.Connection{{ID: conn, Provider: "hubspot", Platform: "HubSpot", Account: "acme.com"}}}}

	var uid uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email) VALUES ('test_conn', 'conn@example.test') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	caller := &auth.Identity{UserID: uid, Status: "active"}
	if _, err := pool.Exec(ctx, `INSERT INTO agents_feature_scopes (user_id, feature, level) VALUES ($1, 'crm', 'edit')`, uid); err != nil {
		t.Fatal(err)
	}
	setConn := func(level string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO agents_feature_scopes (user_id, feature, level) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, feature) DO UPDATE SET level = EXCLUDED.level`, uid, app.ConnectionScope(conn), level); err != nil {
			t.Fatal(err)
		}
	}
	target := func(in json.RawMessage) (string, string) {
		var x struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(in, &x)
		return "crm.external", x.ID
	}
	noop := func(context.Context, *auth.Identity, json.RawMessage) (any, error) { return map[string]any{}, nil }
	write := app.Tool{Name: "crm_update_external_deal", Source: "HubSpot", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: noop, Target: target,
		Preview: func(context.Context, *auth.Identity, json.RawMessage) (app.ToolPreview, error) {
			return app.ToolPreview{Summary: "Rename the HubSpot deal"}, nil
		}}
	read := app.Tool{Name: "crm_get_external_deal", ReadOnly: true, InputSchema: json.RawMessage(`{"type":"object"}`), Handler: noop, Target: target}
	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test", ClientID: "test-client"})
	in := json.RawMessage(fmt.Sprintf(`{"id":%q}`, app.ExternalTarget(conn, "deal-42")))
	gate := func(tl app.Tool, input json.RawMessage) app.ToolDecision {
		t.Helper()
		dec, err := s.GateTool(agentCtx, caller, "crm", tl, input)
		if err != nil {
			t.Fatal(err)
		}
		return dec
	}

	// No setting on the connection: the default (read) caps it.
	if dec := gate(write, in); dec.Action != app.ToolRefuse || dec.Code != CodeScopeReadOnly || !strings.Contains(dec.Message, "HubSpot · acme.com") {
		t.Fatalf("connection at the default (read): %+v, want refuse %s naming the account", dec, CodeScopeReadOnly)
	}
	setConn("suggest")
	if dec := gate(write, in); dec.Action != app.ToolSuggested || dec.SuggestionID == "" {
		t.Fatalf("CRM edit + HubSpot suggest: %+v, want a suggestion", dec)
	}
	setConn("edit")
	if dec := gate(write, in); dec.Action != app.ToolRun {
		t.Fatalf("CRM edit + HubSpot edit: %+v, want run", dec)
	}
	// Respelling the connection id doesn't step around anything.
	upper := json.RawMessage(fmt.Sprintf(`{"id":%q}`, strings.ToUpper(conn)+":deal-42"))
	setConn("off")
	for _, input := range []json.RawMessage{in, upper} {
		if dec := gate(write, input); dec.Action != app.ToolRefuse || dec.Code != CodeScopeOff {
			t.Fatalf("HubSpot off (%s): %+v, want refuse %s", input, dec, CodeScopeOff)
		}
		if dec := gate(read, input); dec.Action != app.ToolRefuse || dec.Code != CodeScopeOff {
			t.Fatalf("read with HubSpot off (%s): %+v, want refuse %s", input, dec, CodeScopeOff)
		}
	}
	setConn("read")
	if dec := gate(read, in); dec.Action != app.ToolRun {
		t.Fatalf("read with HubSpot read: %+v, want run", dec)
	}
	if dec := gate(write, in); dec.Action != app.ToolRefuse || dec.Code != CodeScopeReadOnly {
		t.Fatalf("CRM edit + HubSpot read: %+v, want refuse %s", dec, CodeScopeReadOnly)
	}
	// The feature's own level still caps a more permissive connection.
	setConn("edit")
	if _, err := pool.Exec(ctx, `UPDATE agents_feature_scopes SET level = 'read' WHERE user_id = $1 AND feature = 'crm'`, uid); err != nil {
		t.Fatal(err)
	}
	if dec := gate(write, in); dec.Action != app.ToolRefuse || dec.Code != CodeScopeReadOnly {
		t.Fatalf("CRM read + HubSpot edit: %+v, want refuse %s", dec, CodeScopeReadOnly)
	}
	// A target that isn't external isn't capped by any connection.
	plain := app.Tool{Name: "crm_update_contact", InputSchema: json.RawMessage(`{"type":"object"}`), Handler: noop,
		Target: func(json.RawMessage) (string, string) { return "crm.contact", conn + ":x" }}
	if _, err := pool.Exec(ctx, `UPDATE agents_feature_scopes SET level = 'edit' WHERE user_id = $1 AND feature = 'crm'`, uid); err != nil {
		t.Fatal(err)
	}
	setConn("off")
	if dec := gate(plain, json.RawMessage(`{}`)); dec.Action != app.ToolRun {
		t.Fatalf("non-external target with the connection off: %+v, want run", dec)
	}

	// Settings → AI agents lists the account with its level.
	setConn("suggest")
	got := s.serviceCatalog(ctx, caller)
	if len(got) != 1 || got[0].Feature != app.ConnectionScope(conn) || got[0].Label != "HubSpot · acme.com" || got[0].Platform != "hubspot" {
		t.Fatalf("serviceCatalog = %+v, want the HubSpot account", got)
	}
}
