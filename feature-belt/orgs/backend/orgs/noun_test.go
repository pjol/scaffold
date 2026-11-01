package orgs

import "testing"

func TestNoun(t *testing.T) {
	defer SetNoun("organization", "organizations")
	if Noun() != "organization" || NounA() != "an organization" || NounsTitle() != "Organizations" {
		t.Fatalf("default: %q %q %q", Noun(), NounA(), NounsTitle())
	}
	SetNoun(" Workspace ", "Workspaces")
	if Noun() != "workspace" || Nouns() != "workspaces" || NounTitle() != "Workspace" || NounsTitle() != "Workspaces" || NounA() != "a workspace" {
		t.Fatalf("workspace: %q %q %q %q %q", Noun(), Nouns(), NounTitle(), NounsTitle(), NounA())
	}
	if errOrgNotFound.Message != "Workspace not found." || errDomainTaken.Message != "That domain belongs to another workspace." {
		t.Fatalf("sentinels: %q / %q", errOrgNotFound.Message, errDomainTaken.Message)
	}
	SetNoun("office", "offices")
	if NounA() != "an office" {
		t.Fatalf("article: %q", NounA())
	}
}
