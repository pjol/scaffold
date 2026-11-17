package repos

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

func TestIssueFromPath(t *testing.T) {
	id := uuid.New()
	for path, want := range map[string]int{
		"/repos/" + id.String() + "/pulls/12":      12,
		"/repos/" + id.String() + "/issues/3/":     3,
		"/repos/" + id.String() + "/pulls/0":       0,
		"/repos/" + id.String() + "/pulls/-1":      0,
		"/repos/" + id.String() + "/pulls/012":     0,
		"/repos/" + id.String() + "/pulls/1/files": 0,
		"/repos/" + id.String() + "/commits/1":     0,
		"/repos/" + id.String():                    0,
		"/repos/github~acme~web/pulls/1":           0,
		"/repos/not-a-uuid/issues/1":               0,
		"/files/" + id.String():                    0,
	} {
		got, n, ok := IssueFromPath(path)
		if ok != (want > 0) || (ok && (got != id || n != want)) {
			t.Errorf("IssueFromPath(%q) = %v, %d, %v; want %d", path, got, n, ok, want)
		}
	}
}

func TestIssueRefRefusesWithoutViewer(t *testing.T) {
	if _, err := IssueRef(context.Background(), nil, nil, uuid.New(), 1); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("nil viewer: %v", err)
	}
	if _, err := IssueRef(context.Background(), nil, &auth.Identity{UserID: uuid.New()}, uuid.New(), 0); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("number 0: %v", err)
	}
}

// IssueRef answers what the pull request and issue routes would let each
// viewer open: the owner and someone the repository is shared with see #n
// (whichever it is, from either path), a colleague without access, an
// outsider, a trashed repository, a missing number and an agent whose repos
// scope is off get ErrIssueNotFound.
func TestIssueRefAccess(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	title := func(context.Context, db.Querier, uuid.UUID) (string, error) { return "Item", nil }
	link := func(id uuid.UUID) string { return "/items/" + id.String() }
	access.RegisterKind(access.Kind{Name: itemKind, Label: "item", Title: title, Link: link})
	access.RegisterKind(access.Kind{Name: RepoKind, Label: "repository", Title: title, Link: link})
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
	must(pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Acme', 'acme-repos') RETURNING id`).Scan(&org))
	for _, u := range []uuid.UUID{owner, friend, colleague} {
		_, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, u)
		must(err)
	}
	repo := func(name string, trashed bool) uuid.UUID {
		var id uuid.UUID
		must(pool.QueryRow(ctx, `INSERT INTO files_items (org_id, type, name, drive, owner_id, created_by) VALUES ($1, 'repo', $2, 'my', $3, $3) RETURNING id`,
			org, name, owner).Scan(&id))
		if trashed {
			_, err := pool.Exec(ctx, `UPDATE files_items SET trashed_at = now(), trashed_by = $2, trash_root = id WHERE id = $1`, id, owner)
			must(err)
		}
		_, err := pool.Exec(ctx, `INSERT INTO repos_repos (id, org_id, slug, created_by) VALUES ($1, $2, $3, $4)`, id, org, name, owner)
		must(err)
		item := access.Ref{Kind: itemKind, ID: id}
		must(access.CreateResource(ctx, pool, access.NewResource{Kind: itemKind, ID: id, OrgID: org, OwnerID: owner}))
		must(access.CreateResource(ctx, pool, access.NewResource{Kind: RepoKind, ID: id, OrgID: org, OwnerID: owner, Parent: &item}))
		_, err = pool.Exec(ctx, `
			INSERT INTO repos_pulls (repo_id, number, title, author_id, head_branch, base_branch, head_sha, base_sha, state, draft)
			VALUES ($1, 1, 'Fix login redirect', $2, 'fix', 'main', 'aaa', 'bbb', 'merged', false)`, id, owner)
		must(err)
		_, err = pool.Exec(ctx, `INSERT INTO repos_issues (repo_id, number, title, author_id, state, state_reason) VALUES ($1, 2, 'Login loops', $2, 'closed', 'completed')`, id, owner)
		must(err)
		return id
	}
	web := repo("web", false)
	old := repo("old", true)
	must(access.AddGrant(ctx, pool, access.Ref{Kind: itemKind, ID: web}, access.SubjectRef{Type: "user", ID: friend}, access.View, owner))

	as := func(u uuid.UUID) *auth.Identity { return &auth.Identity{UserID: u} }
	pull, err := IssueRef(ctx, pool, as(owner), web, 1)
	if err != nil || pull.Kind != IssueKindPull || pull.Title != "Fix login redirect" || pull.State != "merged" || pull.RepoName != "web" ||
		pull.Context != "Acme · web" || pull.Path != fmt.Sprintf("/repos/%s/pulls/1", web) || pull.Level != access.Manage {
		t.Fatalf("owner's pull request: %+v, %v", pull, err)
	}
	issue, err := IssueRef(ctx, pool, as(friend), web, 2)
	if err != nil || issue.Kind != IssueKindIssue || issue.Title != "Login loops" || issue.State != "closed" || issue.StateReason != "completed" ||
		issue.Path != fmt.Sprintf("/repos/%s/issues/2", web) || issue.Level != access.View {
		t.Fatalf("friend's issue: %+v, %v", issue, err)
	}
	for name, c := range map[string]struct {
		viewer uuid.UUID
		repo   uuid.UUID
		n      int
	}{
		"colleague without access": {colleague, web, 1},
		"outsider":                 {outsider, web, 2},
		"trashed repository":       {owner, old, 1},
		"missing number":           {owner, web, 3},
		"missing repository":       {owner, uuid.New(), 1},
	} {
		if got, err := IssueRef(ctx, pool, as(c.viewer), c.repo, c.n); !errors.Is(err, ErrIssueNotFound) {
			t.Errorf("%s: %+v, %v; want ErrIssueNotFound", name, got, err)
		}
	}

	agentCtx := auth.WithAgent(ctx, auth.Agent{Via: "mcp", Client: "Test"})
	var hasAgents bool
	must(pool.QueryRow(ctx, `SELECT to_regclass('agents_feature_scopes') IS NOT NULL`).Scan(&hasAgents))
	if !hasAgents {
		t.Log("agents feature not installed: agent scope part skipped")
		return
	}
	if got, err := IssueRef(agentCtx, pool, as(owner), web, 1); err != nil || got.Level != access.View {
		t.Fatalf("agent at read: %+v, %v (level capped at view)", got, err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO agents_feature_scopes (user_id, feature, level) VALUES ($1, 'repos', 'off')`, owner)
	must(err)
	if got, err := IssueRef(agentCtx, pool, as(owner), web, 1); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("agent with repos off: %+v, %v", got, err)
	}
	if _, err := IssueRef(ctx, pool, as(owner), web, 2); err != nil {
		t.Fatalf("the owner still reads it: %v", err)
	}
}
