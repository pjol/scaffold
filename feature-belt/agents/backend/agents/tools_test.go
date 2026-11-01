package agents

import (
	"encoding/json"
	"testing"

	"scaffoldapp/backend/internal/app"
)

func TestResultIDKeepsOnlyTheTopLevelID(t *testing.T) {
	for in, want := range map[string]string{
		`{"id":"4b6f…","title":"Q3 plan","related":[{"title":"Board notes"}]}`: "4b6f…",
		`{"id":42,"name":"x"}`:                  "42",
		`{"items":[{"id":"a"}]}`:                "",
		`[{"id":"a"}]`:                          "",
		`"done"`:                                "",
		``:                                      "",
		`{"event":{"id":"nested","title":"x"}}`: "",
	} {
		if got := resultID(json.RawMessage(in), ""); got != want {
			t.Errorf("resultID(%s) = %q, want %q", in, got, want)
		}
	}
	if got := resultID(json.RawMessage(`{"event_id":"e1","title":"x","attendees":[{"id":"u1"}]}`), "calendar.event"); got != "e1" {
		t.Errorf("resultID with a calendar.event target = %q, want e1", got)
	}
}

func TestToolSuggestionCarriesNoOutput(t *testing.T) {
	sg := &Suggestion{Status: StatusAccepted, Tool: "docs_create_page", Summary: "Create “Q3 plan”",
		Result: json.RawMessage(`{"id":"p1","title":"Q3 plan","related":[{"title":"Private board notes"}]}`)}
	b, err := json.Marshal((&Service{d: &app.Deps{}}).toToolSuggestion(sg))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if _, ok := out["result"]; ok {
		t.Fatalf("agent view carries the person's output: %s", b)
	}
	if out["result_id"] != "p1" {
		t.Fatalf("result_id = %v, want p1", out["result_id"])
	}
}

func TestHiddenFeaturesAreAdjustable(t *testing.T) {
	if !featureHidden("search") || featureHidden("docs") {
		t.Fatal("default hidden set: search hidden, docs listed")
	}
	HideFeature("docs")
	ShowFeature("search")
	defer func() { ShowFeature("docs"); HideFeature("search") }()
	if featureHidden("search") || !featureHidden("docs") {
		t.Fatal("HideFeature / ShowFeature didn't change the set")
	}
}
