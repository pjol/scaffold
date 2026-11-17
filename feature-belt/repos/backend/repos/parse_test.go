package repos

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestParseRefs(t *testing.T) {
	cases := []struct {
		in   string
		want []TextRef
	}{
		{"See #12 and #3.", []TextRef{{12, false}, {3, false}}},
		{"Fixes #12", []TextRef{{12, true}}},
		{"fixed: #4, closes #5 and resolves #6", []TextRef{{4, true}, {5, true}, {6, true}}},
		{"Close #7\n\nAlso #7 again", []TextRef{{7, true}}},
		{"mentions #8 then fixes #8", []TextRef{{8, true}}},
		{"no ref in `#9` or a URL http://x.test/#10 or &#11; or abc#12", nil},
		{"```\nfixes #13\n```\noutside #14", []TextRef{{14, false}}},
		{"#15abc is not one; (#16) is", []TextRef{{16, false}}},
		{"Resolve #0", nil},
	}
	for _, c := range cases {
		if got := ParseRefs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseRefs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseMentions(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got := ParseMentions("hi <@" + a.String() + "> and <@" + b.String() + "> and again <@" + a.String() + "> `<@" + uuid.NewString() + ">`")
	if !reflect.DeepEqual(got, []uuid.UUID{a, b}) {
		t.Fatalf("got %v", got)
	}
}

func TestParseIssueQuery(t *testing.T) {
	q := ParseIssueQuery(`is:open label:bug label:"good first issue" -label:wontfix assignee:@me milestone:"v1.0" crash on start sort:comments`)
	if q.State != "open" || !reflect.DeepEqual(q.Labels, []string{"bug", "good first issue"}) || !reflect.DeepEqual(q.NotLabels, []string{"wontfix"}) ||
		q.Assignee != "@me" || q.Milestone != "v1.0" || q.Text != "crash on start" || q.Sort != "comments" {
		t.Fatalf("got %+v", q)
	}
	q = ParseIssueQuery(`is:closed reason:"not planned" no:assignee no:milestone is:pr -is:draft author:sam review-requested:@me is:bogus`)
	if q.State != "closed" || q.Reason != "not_planned" || !q.NoAssignee || !q.NoMilestone || q.Kind != "pr" || q.Draft == nil || *q.Draft ||
		q.Author != "sam" || q.ReviewRequest != "@me" || len(q.Unknown) != 1 {
		t.Fatalf("got %+v", q)
	}
	if q := ParseIssueQuery("http://example.test/x"); q.Text != "http://example.test/x" {
		t.Fatalf("a URL is text: %+v", q)
	}
}

func TestSuggestions(t *testing.T) {
	body := "Looks off.\n```suggestion\nfoo := 2\nbar := 3\n```\nthanks"
	repl, ok := ParseSuggestion(body)
	if !ok || repl != "foo := 2\nbar := 3" {
		t.Fatalf("got %q %v", repl, ok)
	}
	if _, ok := ParseSuggestion("```go\nx\n```"); ok {
		t.Fatal("a go block isn't a suggestion")
	}
	if repl, ok := ParseSuggestion("```suggestion\n```"); !ok || repl != "" {
		t.Fatalf("empty suggestion deletes: %q %v", repl, ok)
	}
	src := "a\nb\nc\nd\n"
	if out, ok := ApplySuggestion(src, 2, 3, "B\nC\nC2"); !ok || out != "a\nB\nC\nC2\nd\n" {
		t.Fatalf("got %q", out)
	}
	if out, ok := ApplySuggestion(src, 4, 4, ""); !ok || out != "a\nb\nc\n" {
		t.Fatalf("delete: %q", out)
	}
	if out, ok := ApplySuggestion("x", 1, 1, "y"); !ok || out != "y" {
		t.Fatalf("no trailing newline kept: %q", out)
	}
	if _, ok := ApplySuggestion(src, 3, 9, "z"); ok {
		t.Fatal("out of range must fail")
	}
}

func TestCodeOwners(t *testing.T) {
	rules := ParseCodeOwners(`# owners
*                 lead@acme.test
*.go              @backend
/docs/            @docs-team writer@acme.test
web/**/*.tsx      @org/frontend
apps/legacy/                       # nobody owns this anymore
README.md         sam@acme.test   # trailing comment
`)
	cases := map[string][]string{
		"main.go":                 {"@backend"},
		"internal/x/y.go":         {"@backend"},
		"docs/guide/setup.md":     {"@docs-team", "writer@acme.test"},
		"web/src/app/page.tsx":    {"@org/frontend"},
		"web/page.tsx":            {"@org/frontend"},
		"apps/legacy/old.js":      nil,
		"README.md":               {"sam@acme.test"},
		"pkg/README.md":           {"sam@acme.test"},
		"Makefile":                {"lead@acme.test"},
		"other/docs/notanchor.md": {"lead@acme.test"},
	}
	for p, want := range cases {
		if got := OwnersOf(rules, p); !reflect.DeepEqual(got, want) {
			t.Errorf("OwnersOf(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestStatusAggregation(t *testing.T) {
	if CombinedState(nil) != "" || CombinedState([]string{"success", "success"}) != "success" ||
		CombinedState([]string{"success", "pending"}) != "pending" || CombinedState([]string{"pending", "error"}) != "failure" {
		t.Fatal("combined state")
	}
	missing, failing, pending := MissingChecks([]string{"build", "test", "lint", "e2e"}, map[string]string{"build": "success", "test": "failure", "lint": "pending"})
	if !reflect.DeepEqual(missing, []string{"e2e"}) || !reflect.DeepEqual(failing, []string{"test"}) || !reflect.DeepEqual(pending, []string{"lint"}) {
		t.Fatalf("got %v %v %v", missing, failing, pending)
	}
}

func TestSignWebhook(t *testing.T) {
	// RFC 4231 test case 2: key "Jefe", data "what do ya want for nothing?".
	got := SignWebhook("Jefe", []byte("what do ya want for nothing?"))
	want := "sha256=5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if SignWebhook("other", []byte("what do ya want for nothing?")) == want {
		t.Fatal("a different secret must sign differently")
	}
}

func TestQueryTokenizeQuotes(t *testing.T) {
	if got := tokenize(`label:"good first issue" crash`); !reflect.DeepEqual(got, []string{"label:good first issue", "crash"}) {
		t.Fatalf("got %q", got)
	}
}
