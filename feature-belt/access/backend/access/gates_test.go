package access

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

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
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
	name := fmt.Sprintf("access_test_%d", time.Now().UnixNano())
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

// gateWorld is an org with an owner, an admin and a member, and two kinds:
// one gated by an org permission members lack, one whose public links need a
// second permission members lack.
type gateWorld struct {
	pool                *pgxpool.Pool
	org                 uuid.UUID
	owner, admin, sam   uuid.UUID
	outsider            uuid.UUID
	gated, linked, tree Ref
}

func newGateWorld(t *testing.T) *gateWorld {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	New(&app.Deps{Pool: pool, Cfg: &config.Config{PublicWebURL: "https://app.test"}})
	title := func(context.Context, db.Querier, uuid.UUID) (string, error) { return "Thing", nil }
	link := func(id uuid.UUID) string { return "/things/" + id.String() }
	RegisterPermission(Permission{Key: "gatetest.use", Title: "Use things", DefaultRoles: []string{"owner", "admin"}})
	RegisterPermission(Permission{Key: "gatetest.publish", Title: "Publish things", DefaultRoles: []string{"owner", "admin"}})
	RegisterKind(Kind{Name: "gatetest.secret", Label: "secret", Title: title, Link: link, Permission: "gatetest.use"})
	RegisterKind(Kind{Name: "gatetest.page", Label: "page", Title: title, Link: link, LinkPermission: "gatetest.publish",
		PublicPath: func(token string) string { return "/p/" + token }})

	w := &gateWorld{pool: pool}
	user := func(name string) uuid.UUID {
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $1 || '@example.test', $1) RETURNING id`,
			"test_"+name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	w.owner, w.admin, w.sam, w.outsider = user("owner"), user("admin"), user("sam"), user("outsider")
	if err := pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Gate test', 'gate-test') RETURNING id`).Scan(&w.org); err != nil {
		t.Fatal(err)
	}
	for u, role := range map[uuid.UUID]string{w.owner: "owner", w.admin: "admin", w.sam: "member"} {
		if _, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`, w.org, u, role); err != nil {
			t.Fatal(err)
		}
	}
	w.gated = Ref{Kind: "gatetest.secret", ID: uuid.New()}
	w.linked = Ref{Kind: "gatetest.page", ID: uuid.New()}
	for _, r := range []Ref{w.gated, w.linked} {
		if err := CreateResource(ctx, pool, NewResource{Kind: r.Kind, ID: r.ID, OrgID: w.org, OwnerID: w.owner, General: View}); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func code(err error) string {
	var he *httpx.Error
	if errors.As(err, &he) {
		return fmt.Sprintf("%d %s", he.Status, he.Code)
	}
	if err == nil {
		return "ok"
	}
	return err.Error()
}

// Kind.Permission: general access lets every member view, but LevelOf and
// Require answer none/404 to members without the kind's org permission. The
// SQL helper is unchanged (features gate their routes by permission).
func TestKindPermissionGate(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	if l, err := LevelOf(ctx, w.pool, w.sam, w.gated.Kind, w.gated.ID); err != nil || l != None {
		t.Fatalf("member without gatetest.use: LevelOf = %v, %v; want none", l, err)
	}
	if _, err := Require(ctx, w.pool, w.sam, w.gated.Kind, w.gated.ID, View); code(err) != "404 not_found" {
		t.Fatalf("member without gatetest.use: Require = %s; want 404 not_found", code(err))
	}
	if l, err := LevelOf(ctx, w.pool, w.admin, w.gated.Kind, w.gated.ID); err != nil || l != View {
		t.Fatalf("admin with gatetest.use: LevelOf = %v, %v; want view (general access)", l, err)
	}
	var sqlLevel int
	if err := w.pool.QueryRow(ctx, `SELECT access_level($1, $2, $3)`, w.sam, w.gated.Kind, w.gated.ID).Scan(&sqlLevel); err != nil || sqlLevel != int(View) {
		t.Fatalf("access_level SQL = %d, %v; want 1 (helpers don't apply kind permissions)", sqlLevel, err)
	}
	// Giving the member the permission by name opens it.
	if _, err := w.pool.Exec(ctx, `INSERT INTO access_permission_policies (org_id, key, roles) VALUES ($1, 'gatetest.use', '{owner,admin}')`, w.org); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pool.Exec(ctx, `INSERT INTO access_permission_subjects (org_id, key, subject_type, subject_id) VALUES ($1, 'gatetest.use', 'user', $2)`,
		w.org, w.sam); err != nil {
		t.Fatal(err)
	}
	if l, err := LevelOf(ctx, w.pool, w.sam, w.gated.Kind, w.gated.ID); err != nil || l != View {
		t.Fatalf("member granted gatetest.use: LevelOf = %v, %v; want view", l, err)
	}
	// Outsiders still see nothing; ungated kinds are untouched.
	if l, _ := LevelOf(ctx, w.pool, w.outsider, w.gated.Kind, w.gated.ID); l != None {
		t.Fatalf("outsider: LevelOf = %v; want none", l)
	}
	if l, _ := LevelOf(ctx, w.pool, w.sam, w.linked.Kind, w.linked.ID); l != View {
		t.Fatalf("ungated kind: LevelOf = %v; want view", l)
	}
}

// Kind.LinkPermission: making a link needs it besides access.share_public,
// and links stop resolving, live, when the creator loses it.
func TestLinkPermission(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	if _, err := CreateLink(ctx, w.pool, w.linked, View, nil, w.sam); code(err) != "403 permission_required" {
		t.Fatalf("member without gatetest.publish: CreateLink = %s; want 403 permission_required", code(err))
	}
	l, err := CreateLink(ctx, w.pool, w.linked, View, nil, w.admin)
	if err != nil {
		t.Fatal(err)
	}
	if l.URL == nil || *l.URL != "https://app.test/p/"+*l.Token {
		t.Fatalf("CreateLink URL = %v; want https://app.test/p/<token>", l.URL)
	}
	if got, err := ResolveLink(ctx, w.pool, *l.Token); err != nil || got.Ref != w.linked || got.Level != View {
		t.Fatalf("ResolveLink = %+v, %v; want the page at view", got, err)
	}
	if lv, err := LinkLevel(ctx, w.pool, *l.Token, w.linked.Kind, w.linked.ID); err != nil || lv != View {
		t.Fatalf("LinkLevel = %v, %v; want view", lv, err)
	}
	// The org takes publishing away from admins: the admin's link stops.
	if _, err := w.pool.Exec(ctx, `INSERT INTO access_permission_policies (org_id, key, roles) VALUES ($1, 'gatetest.publish', '{owner}')`, w.org); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLink(ctx, w.pool, *l.Token); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("ResolveLink after losing gatetest.publish = %v; want ErrLinkNotFound", err)
	}
	if lv, err := LinkLevel(ctx, w.pool, *l.Token, w.linked.Kind, w.linked.ID); err != nil || lv != None {
		t.Fatalf("LinkLevel after losing gatetest.publish = %v, %v; want none", lv, err)
	}
	if missing, err := missingLinkPermission(ctx, w.pool, mustKind(t, w.linked.Kind), &w.org, w.admin); err != nil || missing != "gatetest.publish" {
		t.Fatalf("missingLinkPermission = %q, %v; want gatetest.publish", missing, err)
	}
	if _, err := CreateLink(ctx, w.pool, w.linked, View, nil, w.owner); err != nil {
		t.Fatalf("owner still holds gatetest.publish: CreateLink = %v", err)
	}
}

func mustKind(t *testing.T, name string) Kind {
	k, ok := KindOf(name)
	if !ok {
		t.Fatalf("kind %s not registered", name)
	}
	return k
}

// AddGrant raises only, SetGrant sets exactly, RemoveGrant clears; all apply
// the share dialog's subject checks. SetOwnerTree moves a same-kind subtree.
func TestGrantAndOwnerHelpers(t *testing.T) {
	w := newGateWorld(t)
	ctx := context.Background()
	ref := w.linked
	levelOfGrant := func(typ string, id uuid.UUID) int {
		var l int
		err := w.pool.QueryRow(ctx, `SELECT level FROM access_grants WHERE resource_kind = $1 AND resource_id = $2 AND subject_type = $3 AND subject_id = $4`,
			ref.Kind, ref.ID, typ, id).Scan(&l)
		if db.IsNoRows(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	sam := SubjectRef{Type: "user", ID: w.sam}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(AddGrant(ctx, w.pool, ref, sam, Edit, w.owner))
	must(AddGrant(ctx, w.pool, ref, sam, View, w.owner))
	if l := levelOfGrant("user", w.sam); l != int(Edit) {
		t.Fatalf("AddGrant lowered a grant: %d", l)
	}
	must(SetGrant(ctx, w.pool, ref, sam, Comment, uuid.Nil))
	if l := levelOfGrant("user", w.sam); l != int(Comment) {
		t.Fatalf("SetGrant = %d; want comment", l)
	}
	must(RemoveGrant(ctx, w.pool, ref, sam))
	if l := levelOfGrant("user", w.sam); l != 0 {
		t.Fatalf("RemoveGrant left level %d", l)
	}
	must(SetGrant(ctx, w.pool, ref, SubjectRef{Type: "org", ID: w.org}, Edit, uuid.Nil))
	if l := levelOfGrant("org", w.org); l != int(Edit) {
		t.Fatalf("org SetGrant = %d; want edit", l)
	}
	for name, err := range map[string]error{
		"outsider":      AddGrant(ctx, w.pool, ref, SubjectRef{Type: "user", ID: w.outsider}, View, w.owner),
		"org at manage": SetGrant(ctx, w.pool, ref, SubjectRef{Type: "org", ID: w.org}, Manage, uuid.Nil),
		"another org":   SetGrant(ctx, w.pool, ref, SubjectRef{Type: "org", ID: uuid.New()}, View, uuid.Nil),
		"unknown team":  SetGrant(ctx, w.pool, ref, SubjectRef{Type: "team", ID: uuid.New()}, View, uuid.Nil),
		"invalid level": SetGrant(ctx, w.pool, ref, sam, None, uuid.Nil),
		"bad subject":   SetGrant(ctx, w.pool, ref, SubjectRef{Type: "group", ID: w.sam}, View, uuid.Nil),
	} {
		if code(err) != "400 invalid_request" {
			t.Errorf("%s: %s; want 400 invalid_request", name, code(err))
		}
	}
	if err := AddGrant(ctx, w.pool, ref, SubjectRef{Type: "user", ID: w.owner}, Edit, w.owner); err != nil || levelOfGrant("user", w.owner) != 0 {
		t.Fatalf("a grant to the owner must be skipped: %v", err)
	}

	// A three-deep tree of one kind with a restricted middle, plus a child of
	// another kind that keeps its owner.
	root, mid, leaf, other := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	must(CreateResource(ctx, w.pool, NewResource{Kind: ref.Kind, ID: root, OrgID: w.org, OwnerID: w.sam}))
	must(CreateResource(ctx, w.pool, NewResource{Kind: ref.Kind, ID: mid, OrgID: w.org, OwnerID: w.sam, Parent: &Ref{ref.Kind, root}, Restricted: true}))
	must(CreateResource(ctx, w.pool, NewResource{Kind: ref.Kind, ID: leaf, OrgID: w.org, OwnerID: w.sam, Parent: &Ref{ref.Kind, mid}}))
	must(CreateResource(ctx, w.pool, NewResource{Kind: w.gated.Kind, ID: other, OrgID: w.org, OwnerID: w.sam, Parent: &Ref{ref.Kind, root}}))
	must(SetOwnerTree(ctx, w.pool, Ref{ref.Kind, root}, uuid.Nil))
	for id, want := range map[uuid.UUID]*uuid.UUID{root: nil, mid: nil, leaf: nil, other: &w.sam} {
		r, err := Lookup(ctx, w.pool, Ref{Kind: map[bool]string{true: w.gated.Kind, false: ref.Kind}[id == other], ID: id})
		must(err)
		if !equalUUIDPtr(r.OwnerID, want) {
			t.Errorf("SetOwnerTree: %s owner %v; want %v", id, r.OwnerID, want)
		}
	}
	if err := SetOwnerTree(ctx, w.pool, Ref{ref.Kind, uuid.New()}, w.sam); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("SetOwnerTree on a missing resource = %v; want ErrNotRegistered", err)
	}
}
