package docs

import (
	"encoding/json"
	"testing"
)

// Pure parts of the connected adapters (the scaffold verifies the adapters
// against mock Notion and Google APIs when it changes them).

func TestTextEditIndexMath(t *testing.T) {
	// "😀 Hello world" starts at index 10; the emoji is two UTF-16 units.
	old := []Span{{Text: "😀 Hello "}, {Text: "world", Bold: true}}
	next := []Span{{Text: "😀 Hello "}, {Text: "brave world", Bold: true}, {Text: "!"}}
	reqs, delta := textEdit(10, old, next)
	if delta != 7 {
		t.Fatalf("delta = %d, want 7", delta)
	}
	b, _ := json.Marshal(reqs)
	got := string(b)
	// The changed middle starts after "😀 Hello " (index 10+2+7 = 19): "world" is
	// replaced by "brave world!", bold except the "!".
	for _, want := range []string{
		`{"deleteContentRange":{"range":{"endIndex":24,"startIndex":19}}}`,
		`{"insertText":{"location":{"index":19},"text":"brave world!"}}`,
		`"range":{"endIndex":30,"startIndex":19},"textStyle":{"bold":true}`,
		`"range":{"endIndex":31,"startIndex":30},"textStyle":{}`,
	} {
		if !contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	// Soft line breaks are vertical tabs in Google Docs.
	reqs, _ = textEdit(1, nil, []Span{{Text: "a\nb"}})
	b, _ = json.Marshal(reqs)
	if !contains(string(b), `"text":"a\u000bb"`) {
		t.Fatalf("soft break: %s", b)
	}
}

func TestNotionRichText(t *testing.T) {
	rt, err := notionRich([]Span{{Text: "Hi "}, {Text: "there", Bold: true, Link: "https://example.com"}, {Text: "@Sam", Mention: &Mention{Type: "user", ID: "u1"}}}, map[string]bool{"u1": true})
	if err != nil || len(rt) != 3 || rt[2]["type"] != "mention" {
		t.Fatalf("rich text: %v %+v", err, rt)
	}
	long := make([]rune, 4500)
	for i := range long {
		long[i] = 'x'
	}
	rt, _ = notionRich([]Span{{Text: string(long)}}, nil)
	if len(rt) != 3 {
		t.Fatalf("text over 2000 characters splits: %d parts", len(rt))
	}
	// Unknown people (app members) become plain text.
	rt, _ = notionRich([]Span{{Text: "@Ana", Mention: &Mention{Type: "user", ID: "app-user"}}}, nil)
	if rt[0]["type"] != "text" {
		t.Fatalf("unknown mention: %+v", rt)
	}
}

func TestNotionFilterTranslation(t *testing.T) {
	schema := []Property{
		{ID: "title", Name: "Name", Type: "title", Source: "title"},
		{ID: "st", Name: "Status", Type: "status", Source: "status", Options: []Option{{ID: "o1", Name: "Done"}}},
		{ID: "f", Name: "Score", Type: "text", Source: "formula", ReadOnly: true},
	}
	v, _ := json.Marshal("o1")
	f := notionFilter(Filter{Property: "st", Op: "is", Value: v}, schema)
	b, _ := json.Marshal(f)
	if string(b) != `{"property":"st","status":{"equals":"Done"}}` {
		t.Fatalf("status filter: %s", b)
	}
	if notionFilter(Filter{Property: "f", Op: "is_empty"}, schema) != nil {
		t.Fatal("read-only properties aren't filtered on the platform")
	}
	q, _ := json.Marshal("plan")
	b, _ = json.Marshal(notionFilter(Filter{Property: "title", Op: "contains", Value: q}, schema))
	if string(b) != `{"property":"title","title":{"contains":"plan"}}` {
		t.Fatalf("title filter: %s", b)
	}
}

func TestCoalesceTyping(t *testing.T) {
	a, _ := json.Marshal([]Span{{Text: "a"}})
	ab, _ := json.Marshal([]Span{{Text: "ab"}})
	ops := coalesce([]Op{{Op: "update", Props: map[string]json.RawMessage{"text": a}}, {Op: "update", Props: map[string]json.RawMessage{"text": ab}}})
	if len(ops) != 1 || string(ops[0].Props["text"]) != string(ab) {
		t.Fatalf("coalesce: %+v", ops)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
