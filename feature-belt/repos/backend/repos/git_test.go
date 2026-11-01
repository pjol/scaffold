package repos

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newTestGit needs git ≥ 2.38 on PATH; the test is skipped without it.
func newTestGit(t *testing.T) *Git {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	g, err := NewGit("git", t.TempDir(), 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := g.Version(context.Background()); err != nil || !ok {
		t.Skip("git too old for merge-tree --write-tree")
	}
	return g
}

func TestEngineEditMergeDiff(t *testing.T) {
	ctx := context.Background()
	g := newTestGit(t)
	id := uuid.New()
	if err := g.Init(ctx, id, "main"); err != nil {
		t.Fatal(err)
	}
	who := Person{Name: "Sam", Email: "sam@example.test"}

	// First commit on an empty repository.
	c1, err := g.CommitFiles(ctx, id, "", []importFile{
		{Path: "README.md", Content: []byte("# Demo\n")},
		{Path: "src/main.go", Content: []byte("package main\n\nfunc main() {}\n")},
	}, "Initial commit", who, who)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.UpdateRef(ctx, id, "refs/heads/main", c1, ZeroOID, "init"); err != nil {
		t.Fatal(err)
	}
	if sha, err := g.ResolveCommit(ctx, id, "main"); err != nil || sha != c1 {
		t.Fatalf("resolve main = %q, %v", sha, err)
	}

	// Edit, add in a new folder, delete: only touched trees change.
	tree1, _ := g.TreeOf(ctx, id, c1)
	tree2, err := g.ApplyChanges(ctx, id, tree1, []Change{
		{Op: "put", Path: "src/main.go", Content: []byte("package main\n\nfunc main() { println(\"hi\") }\n")},
		{Op: "put", Path: "docs/guide/intro.md", Content: []byte("Hello\n")},
		{Op: "delete", Path: "README.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := g.CommitTree(ctx, id, tree2, []string{c1}, "Edit", who, who)
	if err != nil {
		t.Fatal(err)
	}
	// A stale writer loses: main is still at c1, so expecting c2 fails.
	if err := g.UpdateRef(ctx, id, "refs/heads/main", c2, c2, "x"); !errors.Is(err, ErrRefMoved) {
		t.Fatalf("stale update-ref = %v, want ErrRefMoved", err)
	}
	if err := g.UpdateRef(ctx, id, "refs/heads/main", c2, c1, "edit"); err != nil {
		t.Fatal(err)
	}
	entries, err := g.ListTree(ctx, id, c2, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got := strings.Join(names, ","); got != "docs,src" {
		t.Fatalf("root = %s", got)
	}
	// Deleting the only file of a folder removes the folder.
	tree3, err := g.ApplyChanges(ctx, id, tree2, []Change{{Op: "delete", Path: "docs/guide/intro.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if e := parseLsTreeOut(t, g, id, tree3); strings.Contains(e, "docs") {
		t.Fatalf("docs should be gone: %s", e)
	}
	// Bad changes are explained.
	var ce *ChangeError
	if _, err := g.ApplyChanges(ctx, id, tree2, []Change{{Op: "put", Path: "src/main.go/x", Content: []byte("x")}}); !errors.As(err, &ce) {
		t.Fatalf("file as folder = %v", err)
	}
	if _, err := g.ApplyChanges(ctx, id, tree2, []Change{{Op: "delete", Path: "nope.txt"}}); !errors.As(err, &ce) {
		t.Fatalf("delete missing = %v", err)
	}

	// Diff c1..c2.
	d, err := g.DiffCommits(ctx, id, c1, c2, defaultDiffLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Files) != 3 || d.Additions != 2 || d.Deletions != 2 {
		t.Fatalf("diff = %d files +%d -%d", len(d.Files), d.Additions, d.Deletions)
	}
	for _, f := range d.Files {
		if f.Path == "src/main.go" && (len(f.Hunks) != 1 || f.Status != "modified") {
			t.Fatalf("main.go diff = %+v", f)
		}
	}

	// Branch and conflicting change → merge-tree reports the conflict.
	featTree, _ := g.ApplyChanges(ctx, id, tree2, []Change{{Op: "put", Path: "src/main.go", Content: []byte("package main\n\nfunc main() { println(\"feature\") }\n")}})
	feat, _ := g.CommitTree(ctx, id, featTree, []string{c2}, "Feature", who, who)
	mainTree, _ := g.ApplyChanges(ctx, id, tree2, []Change{{Op: "put", Path: "src/main.go", Content: []byte("package main\n\nfunc main() { println(\"main\") }\n")}})
	mainC, _ := g.CommitTree(ctx, id, mainTree, []string{c2}, "Main", who, who)
	res, err := g.MergeTrees(ctx, id, mainC, feat)
	if err != nil {
		t.Fatal(err)
	}
	if res.Clean || len(res.Conflicts) != 1 || res.Conflicts[0] != "src/main.go" {
		t.Fatalf("merge = %+v", res)
	}
	// A clean merge.
	otherTree, _ := g.ApplyChanges(ctx, id, tree2, []Change{{Op: "put", Path: "NOTES.md", Content: []byte("notes\n")}})
	other, _ := g.CommitTree(ctx, id, otherTree, []string{c2}, "Notes", who, who)
	res, err = g.MergeTrees(ctx, id, mainC, other)
	if err != nil || !res.Clean {
		t.Fatalf("clean merge = %+v, %v", res, err)
	}
	if ok, _ := g.IsAncestor(ctx, id, c2, other); !ok {
		t.Fatal("c2 should be an ancestor of other")
	}

	// Grep, log, blame, last commits.
	matches, _, err := g.Grep(ctx, id, c2, "PRINTLN", 50, 5)
	if err != nil || len(matches) != 1 || matches[0].Path != "src/main.go" || matches[0].Line != 3 {
		t.Fatalf("grep = %+v, %v", matches, err)
	}
	log, err := g.Log(ctx, id, c2, "", 0, 10)
	if err != nil || len(log) != 2 || log[0].Subject != "Edit" {
		t.Fatalf("log = %+v, %v", log, err)
	}
	blame, err := g.Blame(ctx, id, c2, "src/main.go")
	if err != nil || len(blame) == 0 {
		t.Fatalf("blame = %+v, %v", blame, err)
	}
	last, err := g.LastCommits(ctx, id, c2, "", []string{"src", "docs"}, 100)
	if err != nil || last["src"].SHA != c2 || last["docs"].SHA != c2 {
		t.Fatalf("last = %+v, %v", last, err)
	}
	if err := g.CheckRefFormat(ctx, "heads", "feature/ok"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"-x", "a..b", "a b", "x.lock", "HEAD", "a~1"} {
		if g.CheckRefFormat(ctx, "heads", bad) == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
	if _, err := CleanPath("../etc/passwd"); err == nil {
		t.Fatal("dot-dot path accepted")
	}
	if _, err := CleanPath("a/.git/config"); err == nil {
		t.Fatal(".git path accepted")
	}
}

func parseLsTreeOut(t *testing.T, g *Git, id uuid.UUID, tree string) string {
	t.Helper()
	out, err := g.output(context.Background(), id, "ls-tree", "-r", "--name-only", tree)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
