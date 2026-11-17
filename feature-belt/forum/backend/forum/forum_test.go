package forum

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHot(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	if h := Hot(0, at, false); h != 0 {
		t.Fatalf("Hot at the epoch with no votes = %v", h)
	}
	if Hot(10, at, false) <= Hot(1, at, false) {
		t.Fatal("more votes must rank higher")
	}
	if Hot(-10, at, false) >= Hot(0, at, false) {
		t.Fatal("downvoted posts must rank lower")
	}
	// 12.5 hours of age is worth one order of magnitude of votes
	if a, b := Hot(10, at, false), Hot(1, at.Add(45000*time.Second), false); a-b > 1e-9 || b-a > 1e-9 {
		t.Fatalf("time and votes out of balance: %v %v", a, b)
	}
	if Hot(5, at, true) != Hot(5, at, false)-2 {
		t.Fatal("off-topic must cost 2")
	}
}

func TestExcerpt(t *testing.T) {
	got := Excerpt("# Title\n\nSee [the guide](https://example.org) and **bold** `code`  ![pic](x.png)")
	if got != "Title See the guide and bold code pic" {
		t.Fatalf("excerpt %q", got)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{"Bread & Baking!": "bread-baking", "  Hello   World ": "hello-world"} {
		if got := slugify(in); got != want || !slugRe.MatchString(got) {
			t.Fatalf("slugify(%q) = %q", in, got)
		}
	}
	if got := slugify("¡¿!"); !slugRe.MatchString(got) {
		t.Fatalf("fallback slug %q", got)
	}
}

func TestSearchQuery(t *testing.T) {
	if got := searchQuery("Sour-dough  starter!"); got != "sour:* & dough:* & starter:*" {
		t.Fatalf("searchQuery %q", got)
	}
	if searchQuery("!!") != "" {
		t.Fatal("nothing searchable")
	}
}

func TestMergeLabels(t *testing.T) {
	got := mergeLabels([]string{"off_topic"}, []string{"off_topic", "Bad Label", "holds_position"})
	if len(got) != 2 || got[1] != "holds_position" {
		t.Fatalf("labels %v", got)
	}
}

func row(id, parent uuid.UUID, depth int, author uuid.UUID, status string, score int, at time.Time) commentRow {
	r := commentRow{ID: id, PostID: uuid.Nil, Depth: depth, AuthorID: author, AuthorName: "x", Body: "text", Status: status, Score: score, CreatedAt: at}
	if parent != uuid.Nil {
		p := parent
		r.ParentID = &p
	}
	return r
}

func TestBuildTree(t *testing.T) {
	me, other, blocked := uuid.New(), uuid.New(), uuid.New()
	at := time.Now()
	a, b, c, d, e, f := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	rows := []commentRow{
		row(a, uuid.Nil, 0, other, StatusVisible, 5, at),
		row(b, a, 1, other, StatusDeleted, 0, at.Add(time.Minute)),             // deleted with a reply: placeholder
		row(c, b, 2, me, StatusVisible, 1, at.Add(2*time.Minute)),              //
		row(d, uuid.Nil, 0, other, StatusHeld, 9, at.Add(3*time.Minute)),       // held, no replies: hidden from others
		row(e, uuid.Nil, 0, me, StatusHeld, 0, at.Add(4*time.Minute)),          // my held comment: shown to me
		row(f, uuid.Nil, 0, blocked, StatusVisible, 20, at.Add(5*time.Minute)), // blocked, no replies: gone
	}
	tree := buildTree(rows, treeOpts{Viewer: me, Blocked: map[uuid.UUID]bool{blocked: true}})
	if len(tree) != 2 || tree[0].ID != a || tree[1].ID != e {
		t.Fatalf("top level %v", ids(tree))
	}
	ph := tree[0].Replies[0]
	if ph.ID != b || ph.Author != nil || ph.Body != "" || len(ph.Replies) != 1 {
		t.Fatalf("placeholder %+v", ph)
	}
	if tree[1].Body == "" || tree[1].Status != StatusHeld {
		t.Fatalf("own held comment %+v", tree[1])
	}

	staff := buildTree(rows, treeOpts{Viewer: uuid.New(), Staff: true})
	if len(staff) != 4 {
		t.Fatalf("staff see held and blocked-free trees: %v", ids(staff))
	}
}

func TestBuildTreeMoreReplies(t *testing.T) {
	root := uuid.New()
	rows := []commentRow{row(root, uuid.Nil, 0, uuid.New(), StatusVisible, 0, time.Now())}
	for i := 0; i < 5; i++ {
		rows = append(rows, row(uuid.New(), root, 1, uuid.New(), StatusVisible, i, time.Now()))
	}
	tree := buildTree(rows, treeOpts{MaxChildren: 3})
	if len(tree[0].Replies) != 3 || tree[0].MoreReplies != 2 || tree[0].Replies[0].Score != 4 {
		t.Fatalf("replies %d, more %d", len(tree[0].Replies), tree[0].MoreReplies)
	}
	all := buildTree(rows, treeOpts{Root: &root, Sort: "old"})
	if len(all) != 5 {
		t.Fatalf("replies of root %d", len(all))
	}
}

func TestRenderOneReply(t *testing.T) {
	parent := uuid.New()
	r := row(uuid.New(), parent, 1, uuid.New(), StatusVisible, 0, time.Now())
	c, ok := renderOne(r, treeOpts{Staff: true})
	if !ok || c.ParentID == nil || *c.ParentID != parent || c.Body == "" {
		t.Fatalf("a reply rendered alone: %+v %v", c, ok)
	}
}

func TestDepthFor(t *testing.T) {
	if p, d := depthFor(nil); p != nil || d != 0 {
		t.Fatal("top level")
	}
	gp := uuid.New()
	deep := commentRow{ID: uuid.New(), ParentID: &gp, Depth: MaxDepth}
	if p, d := depthFor(&deep); p == nil || *p != gp || d != MaxDepth {
		t.Fatal("a reply at the cap attaches beside its parent")
	}
	mid := commentRow{ID: uuid.New(), Depth: 3}
	if p, d := depthFor(&mid); *p != mid.ID || d != 4 {
		t.Fatal("a reply goes one deeper")
	}
}

func TestPageComments(t *testing.T) {
	at := time.Now()
	var list []Comment
	for i := 0; i < 7; i++ {
		list = append(list, Comment{ID: uuid.New(), Score: 10 - i, CreatedAt: at})
	}
	seen := map[uuid.UUID]bool{}
	var after *commentCursor
	for pages := 0; ; pages++ {
		page, next := pageComments(list, "top", after, 3)
		for _, c := range page {
			if seen[c.ID] {
				t.Fatal("a comment came twice")
			}
			seen[c.ID] = true
		}
		if next == nil {
			break
		}
		after = next
		if pages > 5 {
			t.Fatal("paging doesn't end")
		}
	}
	if len(seen) != 7 {
		t.Fatalf("saw %d of 7", len(seen))
	}
}

func ids(cs []Comment) []uuid.UUID {
	out := []uuid.UUID{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}
