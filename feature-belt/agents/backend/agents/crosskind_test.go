package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// fakeRepos keeps repositories as drive items: its tools target "files.item".
type fakeRepos struct{}

func (fakeRepos) Name() string { return "repos" }

func (fakeRepos) Tools() []app.Tool {
	target := func(in json.RawMessage) (string, string) {
		var x struct {
			RepoID string `json:"repo_id"`
		}
		_ = json.Unmarshal(in, &x)
		return "files.item", x.RepoID
	}
	noop := func(context.Context, *auth.Identity, json.RawMessage) (any, error) { return map[string]any{}, nil }
	schema := json.RawMessage(`{"type":"object"}`)
	return []app.Tool{
		{Name: "repos_list", ReadOnly: true, InputSchema: schema, Handler: noop},
		{Name: "repos_commit", ReadOnly: false, InputSchema: schema, Handler: noop, Target: target},
		{Name: "repos_own", ReadOnly: false, InputSchema: schema, Handler: noop,
			Target: func(json.RawMessage) (string, string) { return "repos.branch", "" }},
	}
}

func TestForeignKinds(t *testing.T) {
	s := &Service{d: &app.Deps{Modules: []app.Module{fakeRepos{}}}}
	if got := s.foreignKinds("repos"); !reflect.DeepEqual(got, []string{"files.item"}) {
		t.Fatalf("foreignKinds(repos) = %v, want [files.item] (learned from Target, own kinds excluded)", got)
	}
	SetFeatureKinds("exports_test", "crm.record", "exports_test.job", "not a kind")
	if got := s.foreignKinds("exports_test"); !reflect.DeepEqual(got, []string{"crm.record"}) {
		t.Fatalf("foreignKinds(exports_test) = %v, want [crm.record]", got)
	}
	if !s.featureOwnsKind("repos", "files.item") || !s.featureOwnsKind("repos", "repos.branch") || s.featureOwnsKind("repos", "docs.page") {
		t.Fatal("featureOwnsKind: repos owns files.item (targeted) and repos.* only")
	}
}

// testPool creates a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE), applies every migration, and drops it afterwards.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("agents_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	return pool
}

// An Edit override on one drive item lists the repos feature's write tools
// (the headline case: edit one shared repository, read the rest) without
// widening any other drive item.
func TestCrossKindFeatureLevel(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	d := &app.Deps{Pool: pool}
	s := New(d)
	d.Modules = []app.Module{fakeRepos{}, s}

	var uid uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email) VALUES ('test_sam', 'sam@example.test') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	caller := &auth.Identity{UserID: uid, Status: "active"}
	repo, other := uuid.New(), uuid.New()

	hidden := func() bool { // the mcp/assistant tools/list rule for a write tool
		_, top, err := s.FeatureLevel(ctx, caller, "repos")
		if err != nil {
			t.Fatal(err)
		}
		return top < app.AgentSuggest
	}
	if !hidden() {
		t.Fatal("at the default (read), repos write tools must be hidden")
	}

	// Set from the drive's ⋮ menu, so the row's feature is "files".
	if _, err := pool.Exec(ctx, `INSERT INTO agents_resource_scopes (user_id, feature, kind, resource_id, level)
		VALUES ($1, 'files', 'files.item', $2, 'edit')`, uid, repo.String()); err != nil {
		t.Fatal(err)
	}
	base, top, err := s.FeatureLevel(ctx, caller, "repos")
	if err != nil {
		t.Fatal(err)
	}
	if base != app.AgentRead || top != app.AgentEdit {
		t.Fatalf("FeatureLevel(repos) = (%v, %v), want (read, edit)", base, top)
	}
	if hidden() {
		t.Fatal("an Edit override on a repository must list the repos write tools")
	}
	if _, docsTop, _ := s.FeatureLevel(ctx, caller, "docs"); docsTop != app.AgentRead {
		t.Fatalf("an unrelated feature must stay at read, got %v", docsTop)
	}

	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test", ClientID: "test-client"})
	commit := s.toolIndex()["repos_commit"].Tool
	dec, err := s.GateTool(agentCtx, caller, "repos", commit, json.RawMessage(fmt.Sprintf(`{"repo_id":%q}`, repo)))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != app.ToolRun {
		t.Fatalf("commit on the Edit repository: %+v, want run", dec)
	}
	dec, err = s.GateTool(agentCtx, caller, "repos", commit, json.RawMessage(fmt.Sprintf(`{"repo_id":%q}`, other)))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != app.ToolRefuse || dec.Code != CodeScopeReadOnly {
		t.Fatalf("commit on another drive item: %+v, want refuse %s", dec, CodeScopeReadOnly)
	}
}

// An item's own Off can't be stepped around by respelling its id, and a write
// naming an item by an id that isn't one is refused rather than run at the
// feature's level.
func TestGateToolRespelledTarget(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	d := &app.Deps{Pool: pool}
	s := New(d)
	d.Modules = []app.Module{s}

	var uid uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email) VALUES ('test_pat', 'pat@example.test') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	caller := &auth.Identity{UserID: uid, Status: "active"}
	item := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO agents_settings (user_id, default_level) VALUES ($1, 'edit')`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents_resource_scopes (user_id, feature, kind, resource_id, level)
		VALUES ($1, 'notes', 'notes.note', $2, 'off')`, uid, item.String()); err != nil {
		t.Fatal(err)
	}
	write := app.Tool{Name: "notes_update", InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(context.Context, *auth.Identity, json.RawMessage) (any, error) { return nil, nil },
		Target: func(in json.RawMessage) (string, string) {
			var x struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(in, &x)
			return "notes.note", x.ID
		}}
	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test", ClientID: "test-client"})
	upper := strings.ToUpper(item.String())
	for _, id := range []string{item.String(), upper, "{" + item.String() + "}", "urn:uuid:" + item.String()} {
		dec, err := s.GateTool(agentCtx, caller, "notes", write, json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Action != app.ToolRefuse || dec.Code != CodeScopeOff {
			t.Errorf("write to the Off item spelled %q: %+v, want refuse %s", id, dec, CodeScopeOff)
		}
	}
	dec, err := s.GateTool(agentCtx, caller, "notes", write, json.RawMessage(`{"id":"not an id!"}`))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != app.ToolRefuse || dec.Code != "invalid_input" {
		t.Errorf("write with a malformed id: %+v, want refuse invalid_input", dec)
	}
	dec, err = s.GateTool(agentCtx, caller, "notes", write, json.RawMessage(fmt.Sprintf(`{"id":%q}`, uuid.New())))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != app.ToolRun {
		t.Errorf("write to another item at the default (edit): %+v, want run", dec)
	}
}
