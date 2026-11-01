package agents

import (
	"context"
	"encoding/json"
	"testing"

	"scaffoldapp/backend/internal/app"
)

func TestForbiddenTool(t *testing.T) {
	cases := []struct {
		feature, name string
		want          bool
	}{
		{"docs", "docs_share_page", true},
		{"docs", "docs_update_page", false},
		{"channels", "channels_add_member", true},
		{"channels", "channels_post_message", false},
		{"calendar", "calendar_create_booking_link", false},
		{"orgs", "orgs_change_role", true},
		{"connectors:6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e", "github__addCollaboratorPermission", true},
		{"connectors:6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e", "linear__create_issue", false},
		{"connectors:6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e", "vault__rotateApiKey", true},
		{"access", "access_rename_team", true},
		{"mcp", "mcp_anything", true},
		{"agents", "agents_anything", true},
		{"crm", "crm_set_visibility", true},
		{"crm", "crm_update_contact", false},
	}
	for _, c := range cases {
		if got := forbiddenTool(c.feature, c.name); got != c.want {
			t.Errorf("forbiddenTool(%q, %q) = %v, want %v", c.feature, c.name, got, c.want)
		}
	}
}

func TestNameWords(t *testing.T) {
	got := nameWords("github__addCollaborator-permission.v2")
	want := []string{"github", "add", "collaborator", "permission", "v2"}
	if len(got) != len(want) {
		t.Fatalf("nameWords = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("nameWords = %v, want %v", got, want)
		}
	}
}

func TestToolTitle(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"calendar", "calendar_create_event"}:    "Create event",
		{"connectors:x", "linear__create_issue"}: "Create issue",
		{"notes", "notes_update_body"}:           "Update body",
		{"notes", "notes"}:                       "Notes",
	} {
		if got := toolTitle(in[0], in[1]); got != want {
			t.Errorf("toolTitle(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestLevels(t *testing.T) {
	for _, l := range []Level{LevelOff, LevelRead, LevelSuggest, LevelEdit} {
		if !l.Valid() || LevelOf(l.Agent()) != l {
			t.Errorf("level %q does not round-trip", l)
		}
	}
	if Level("admin").Valid() || Level("admin").Agent() != LevelOff.Agent() {
		t.Error("an unknown level must be invalid and act as off")
	}
	for _, f := range []string{"docs", "connectors:6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e"} {
		if !validFeature(f) {
			t.Errorf("validFeature(%q) = false", f)
		}
	}
	for _, f := range []string{"", "Docs", "connectors:nope", "a,b"} {
		if validFeature(f) {
			t.Errorf("validFeature(%q) = true", f)
		}
	}
}

// A respelled UUID names the same item (so its own setting applies), and a
// kind with an id that isn't one is reported, never read as "no target".
func TestTargetOfCanonicalises(t *testing.T) {
	tool := app.Tool{Name: "docs_update_block", Target: func(in json.RawMessage) (string, string) {
		var v struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(in, &v)
		return "docs.page", v.ID
	}}
	const id = "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e"
	for _, spelled := range []string{id, "6F1C0F1E-8A7B-4C62-9D55-1F2A3B4C5D6E", "{" + id + "}", "urn:uuid:" + id, "6f1c0f1e8a7b4c629d551f2a3b4c5d6e", " " + id + " "} {
		in, _ := json.Marshal(map[string]string{"id": spelled})
		kind, got, bad := targetOf(context.Background(), tool, in)
		if kind != "docs.page" || got != id || bad {
			t.Errorf("targetOf(%q) = %q, %q, bad=%v; want docs.page, %s", spelled, kind, got, bad, id)
		}
	}
	for in, wantBad := range map[string]bool{`{"id":"not an id!"}`: true, `{}`: false} {
		kind, got, bad := targetOf(context.Background(), tool, json.RawMessage(in))
		if kind != "" || got != "" || bad != wantBad {
			t.Errorf("targetOf(%s) = %q, %q, bad=%v; want no target, bad=%v", in, kind, got, bad, wantBad)
		}
	}
}
