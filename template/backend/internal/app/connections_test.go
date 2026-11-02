package app

import "testing"

func TestTargetConnection(t *testing.T) {
	const conn = "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e"
	for _, c := range []struct {
		kind, id, want string
		ok             bool
	}{
		{"crm.external", ExternalTarget(conn, "deal-42"), conn, true},
		{"repos.external", conn + ":group/sub/repo", conn, true},
		{"crm.external", "6F1C0F1E-8A7B-4C62-9D55-1F2A3B4C5D6E:x", conn, true},
		{"crm.record", conn + ":deal-42", "", false},
		{"crm.external", "deal-42", "", false},
		{"crm.external", "not-a-uuid:deal", "", false},
		{"crm.external", "{" + conn + "}:deal", "", false},
	} {
		got, ok := TargetConnection(c.kind, c.id)
		if got != c.want || ok != c.ok {
			t.Errorf("TargetConnection(%q, %q) = %q, %v; want %q, %v", c.kind, c.id, got, ok, c.want, c.ok)
		}
	}
	if l := ConnectionLabel(Connection{Provider: "google", Platform: "Google", Account: "sam@acme.com"}); l != "Google · sam@acme.com" {
		t.Errorf("ConnectionLabel = %q", l)
	}
}
