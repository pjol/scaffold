package repos

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
)

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
	name := fmt.Sprintf("repos_test_%d", time.Now().UnixNano())
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

// A folder's grants and restricted flag move to the repository's drive item
// through access's helpers, and the repository resource follows its item's
// owner.
func TestCopySharingAndOwnerSync(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	title := func(context.Context, db.Querier, uuid.UUID) (string, error) { return "Item", nil }
	link := func(id uuid.UUID) string { return "/items/" + id.String() }
	access.RegisterKind(access.Kind{Name: itemKind, Label: "item", Title: title, Link: link})
	access.RegisterKind(access.Kind{Name: RepoKind, Label: "repository", Title: title, Link: link})
	s := &Service{d: &app.Deps{Pool: pool}}

	user := func(name, status string) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name, status) VALUES ($1, $1 || '@example.test', $1, $2) RETURNING id`,
			"test_"+name, status).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner, sam, pat, other := user("owner", "active"), user("sam", "active"), user("pat", "suspended"), user("other", "active")
	var org, team uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Repo test', 'repo-test') RETURNING id`).Scan(&org); err != nil {
		t.Fatal(err)
	}
	for _, u := range []uuid.UUID{owner, sam, pat, other} {
		if _, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO access_teams (org_id, name, handle) VALUES ($1, 'Design', 'design') RETURNING id`, org).Scan(&team); err != nil {
		t.Fatal(err)
	}

	parent := access.Ref{Kind: itemKind, ID: uuid.New()}
	folder := access.Ref{Kind: itemKind, ID: uuid.New()}
	item := access.Ref{Kind: itemKind, ID: uuid.New()}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(access.CreateResource(ctx, pool, access.NewResource{Kind: itemKind, ID: parent.ID, OrgID: org, OwnerID: owner, General: access.Edit}))
	must(access.CreateResource(ctx, pool, access.NewResource{Kind: itemKind, ID: folder.ID, OrgID: org, OwnerID: owner, Parent: &parent, Restricted: true}))
	must(access.CreateResource(ctx, pool, access.NewResource{Kind: itemKind, ID: item.ID, OrgID: org, OwnerID: owner, Parent: &parent}))
	must(access.AddGrant(ctx, pool, folder, access.SubjectRef{Type: "user", ID: sam}, access.Edit, owner))
	must(access.AddGrant(ctx, pool, folder, access.SubjectRef{Type: "team", ID: team}, access.View, owner))
	must(access.AddGrant(ctx, pool, folder, access.SubjectRef{Type: "org", ID: org}, access.Comment, owner))
	// A grant made while pat was active, before the suspension.
	if _, err := pool.Exec(ctx, `INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level) VALUES ($1, $2, 'user', $3, 1)`,
		itemKind, folder.ID, pat); err != nil {
		t.Fatal(err)
	}

	// Before: the new item inherits the parent's edit for everyone.
	if l, _ := access.LevelOf(ctx, pool, other, itemKind, item.ID); l != access.Edit {
		t.Fatalf("before copy: other has %s, want edit (inherited)", l)
	}
	must(db.WithTx(ctx, pool, func(tx pgx.Tx) error { return s.copySharing(ctx, tx, folder.ID, item.ID) }))

	want := map[uuid.UUID]access.Level{sam: access.Edit, other: access.Comment, owner: access.Manage, pat: access.Comment}
	for u, lvl := range want {
		if l, err := access.LevelOf(ctx, pool, u, itemKind, item.ID); err != nil || l != lvl {
			t.Errorf("after copy: %s has %s (%v), want %s", u, l, err, lvl)
		}
	}
	var grants int
	must(pool.QueryRow(ctx, `SELECT count(*) FROM access_grants WHERE resource_kind = $1 AND resource_id = $2`, itemKind, item.ID).Scan(&grants))
	if grants != 3 {
		t.Errorf("copied %d grants, want 3 (user, team, org; not the suspended account)", grants)
	}
	if r, _ := access.Lookup(ctx, pool, item); r.Inherits {
		t.Error("the item should be restricted like the folder")
	}

	// Owner sync: the repository resource follows its drive item.
	repo := access.Ref{Kind: RepoKind, ID: item.ID}
	must(access.CreateResource(ctx, pool, access.NewResource{Kind: RepoKind, ID: item.ID, OrgID: org, OwnerID: owner, Parent: &item}))
	must(access.SetOwner(ctx, pool, item, sam))
	must(s.syncOwner(ctx, item.ID))
	if r, _ := access.Lookup(ctx, pool, repo); r.OwnerID == nil || *r.OwnerID != sam {
		t.Errorf("repository owner = %v, want sam", r.OwnerID)
	}
	must(access.SetOwner(ctx, pool, item, uuid.Nil))
	must(s.syncOwner(ctx, item.ID))
	if r, _ := access.Lookup(ctx, pool, repo); r.OwnerID != nil {
		t.Errorf("repository owner = %v, want none", *r.OwnerID)
	}
	must(s.syncOwner(ctx, item.ID)) // already in step: no-op
}
