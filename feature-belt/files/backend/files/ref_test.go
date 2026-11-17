package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

func TestItemIDFromPath(t *testing.T) {
	id := uuid.New()
	for path, want := range map[string]bool{
		"/files/" + id.String():                     true,
		"/files/" + id.String() + "/":               true,
		"/files/":                                   false,
		"/files/recent":                             false,
		"/files/trash":                              false,
		"/files/c/" + id.String():                   false,
		"/files/c/conn/" + id.String():              false,
		"/files/" + id.String() + "/versions":       false,
		"/docs/" + id.String():                      false,
		"/f/" + id.String():                         false,
		"/files/not-a-uuid":                         false,
		"/files/" + id.String() + "?comment=x":      false,
		"/repos/" + id.String() + "/pulls/1":        false,
		"/files/" + id.String() + "/" + id.String(): false,
	} {
		got, ok := ItemIDFromPath(path)
		if ok != want || (ok && got != id) {
			t.Errorf("ItemIDFromPath(%q) = %v, %v; want ok=%v", path, got, ok, want)
		}
	}
}

func TestItemRefRefusesWithoutViewer(t *testing.T) {
	// No database is touched: the guard answers before any query.
	if _, err := ItemRef(context.Background(), nil, nil, uuid.New()); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("nil viewer: %v", err)
	}
	if _, err := ItemRef(context.Background(), nil, &auth.Identity{UserID: uuid.New()}, uuid.Nil); !errors.Is(err, ErrItemNotFound) {
		t.Fatalf("nil item id: %v", err)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("\n   \n  # Roadmap \t for  Q3 \nsecond", 140); got != "# Roadmap for Q3" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine("abcdef", 4); got != "abc…" {
		t.Fatalf("firstLine cut = %q", got)
	}
	if got := firstLine(" \n\t", 10); got != "" {
		t.Fatalf("blank = %q", got)
	}
}

// refTestPool creates a throwaway database on TEST_DATABASE_URL (a role that
// may CREATE DATABASE), applies every migration, and drops it afterwards.
func refTestPool(t *testing.T) *pgxpool.Pool {
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
	name := fmt.Sprintf("files_test_%d", time.Now().UnixNano())
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

// ItemRef answers what GET /files/items/{id} would let each viewer open:
// the owner and people it's shared with see it (the folder named only when
// they can open it), a colleague without access, an outsider, a trashed or
// missing item and an agent whose scope is off get not found, and reading
// records nothing.
func TestItemRefAccess(t *testing.T) {
	pool := refTestPool(t)
	ctx := context.Background()
	access.RegisterKind(access.Kind{Name: Kind, Label: "item", Link: itemLink,
		Title: func(context.Context, db.Querier, uuid.UUID) (string, error) { return "Item", nil }})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	user := func(name string) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $1 || '@example.test', $1) RETURNING id`,
			"test_"+name).Scan(&id))
		return id
	}
	owner, friend, colleague, outsider := user("owner"), user("friend"), user("colleague"), user("outsider")
	var org uuid.UUID
	must(pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Acme', 'acme-files') RETURNING id`).Scan(&org))
	for _, u := range []uuid.UUID{owner, friend, colleague} {
		_, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, u)
		must(err)
	}
	item := func(parent *uuid.UUID, typ, name, mime string, text *string, trashed bool) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `
			INSERT INTO files_items (org_id, parent_id, type, name, drive, owner_id, created_by, mime_type, size, text_like, content_text)
			VALUES ($1, $2, $3, $4, 'my', $5, $5, $6, $7, $8, $9) RETURNING id`,
			org, parent, typ, name, owner, mime, len(name)*10, text != nil, text).Scan(&id))
		if trashed {
			_, err := pool.Exec(ctx, `UPDATE files_items SET trashed_at = now(), trashed_by = $2, trash_root = id WHERE id = $1`, id, owner)
			must(err)
		}
		r := access.NewResource{Kind: Kind, ID: id, OrgID: org, OwnerID: owner}
		if parent != nil {
			r.Parent = &access.Ref{Kind: Kind, ID: *parent}
		}
		must(access.CreateResource(ctx, pool, r))
		return id
	}
	body := "\n  # Roadmap  \nShip the thing."
	folder := item(nil, TypeFolder, "Plans", "", nil, false)
	file := item(&folder, TypeFile, "Roadmap.md", "text/markdown", &body, false)
	trashed := item(&folder, TypeFile, "Old.md", "text/markdown", &body, true)
	must(access.AddGrant(ctx, pool, access.Ref{Kind: Kind, ID: file}, access.SubjectRef{Type: "user", ID: friend}, access.View, owner))

	as := func(u uuid.UUID) *auth.Identity { return &auth.Identity{UserID: u} }

	got, err := ItemRef(ctx, pool, as(owner), file)
	must(err)
	if got.Name != "Roadmap.md" || got.Type != TypeFile || got.Context != "Acme · Plans" || got.Snippet != "# Roadmap" ||
		got.Path != "/files/"+file.String() || got.Icon != "file-text" || got.Level != access.Manage || got.MimeType != "text/markdown" || got.Size == 0 {
		t.Fatalf("owner's file card: %+v", got)
	}
	if got, err := ItemRef(ctx, pool, as(owner), folder); err != nil || got.Type != TypeFolder || got.Icon != "folder" ||
		got.Context != "Acme · My drive" || got.Size != 0 || got.Snippet != "" {
		t.Fatalf("owner's folder card: %+v, %v", got, err)
	}
	// Shared the file alone: the folder they can't open isn't named.
	if got, err := ItemRef(ctx, pool, as(friend), file); err != nil || got.Context != "Acme · Shared with me" || got.Level != access.View {
		t.Fatalf("friend's card: %+v, %v", got, err)
	}
	for name, c := range map[string]struct {
		viewer uuid.UUID
		id     uuid.UUID
	}{
		"colleague without access": {colleague, file},
		"colleague, folder":        {colleague, folder},
		"friend, folder":           {friend, folder},
		"outsider":                 {outsider, file},
		"trashed":                  {owner, trashed},
		"missing":                  {owner, uuid.New()},
	} {
		if got, err := ItemRef(ctx, pool, as(c.viewer), c.id); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("%s: %+v, %v; want ErrItemNotFound", name, got, err)
		}
	}

	// An agent acting for the owner: read by default, nothing once the
	// owner turns files off for agents (needs the agents feature's tables).
	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test"})
	var hasAgents bool
	must(pool.QueryRow(ctx, `SELECT to_regclass('agents_feature_scopes') IS NOT NULL`).Scan(&hasAgents))
	if hasAgents {
		if got, err := ItemRef(agentCtx, pool, as(owner), file); err != nil || got.Level != access.View {
			t.Fatalf("agent at read: %+v, %v (level capped at view)", got, err)
		}
		_, err := pool.Exec(ctx, `INSERT INTO agents_feature_scopes (user_id, feature, level) VALUES ($1, 'files', 'off')`, owner)
		must(err)
		if got, err := ItemRef(agentCtx, pool, as(owner), file); !errors.Is(err, ErrItemNotFound) {
			t.Fatalf("agent with files off: %+v, %v", got, err)
		}
		if _, err := ItemRef(ctx, pool, as(owner), file); err != nil {
			t.Fatalf("the owner still reads it: %v", err)
		}
	} else {
		t.Log("agents feature not installed: agent scope part skipped")
	}

	var recents int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM files_recents`).Scan(&recents))
	if recents != 0 {
		t.Fatalf("ItemRef recorded %d recent views", recents)
	}
}
