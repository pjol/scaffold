package search

import (
	"context"
	"strings"
	"testing"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// opsProvider records the text it was asked with and returns fixed hits.
type opsProvider struct {
	name string
	ops  []string
	hits []app.SearchResult
	got  *string
}

func (p opsProvider) Name() string { return p.name }
func (p opsProvider) Search(ctx context.Context, _ *auth.Identity, q string, _ int) ([]app.SearchResult, error) {
	*p.got = q
	return p.hits, nil
}

type opsProviderWithOps struct{ opsProvider }

func (p opsProviderWithOps) SearchOperators() []string { return p.ops }

func TestTypeMatches(t *testing.T) {
	cases := map[string]bool{
		"docs.page|page": true, "docs.page|pages": true, "docs.page|doc": true, "docs.page|docs": true,
		"channels.message|messages": true, "channels.message|channel": false, "channels.channel|channels": true,
		"files.item|file": true, "repos.pull_request|pr": true, "crm.company|companies": true, "crm.deal|page": false,
	}
	for k, want := range cases {
		kind, v, _ := strings.Cut(k, "|")
		if got := typeMatches(kind, []string{v}); got != want {
			t.Errorf("typeMatches(%s, %s) = %v", kind, v, got)
		}
	}
}

func TestKeepOperators(t *testing.T) {
	got := keepOperators(`launch from:@sam type:page in:"Q3 plan" during:week`, map[string]bool{"from": true, "date": true})
	if got != "launch from:@sam during:week" {
		t.Errorf("keepOperators = %q", got)
	}
}

func TestRunWithOperators(t *testing.T) {
	now := time.Now()
	var plainGot, rawGot, opsGot string
	plain := opsProvider{name: "docs", got: &plainGot, hits: []app.SearchResult{
		{Kind: "docs.page", ID: "1", Title: "Launch plan", Link: "/docs/1", At: now, Context: "Acme · Engineering"},
		{Kind: "docs.page", ID: "2", Title: "Launch notes", Link: "/docs/2", At: now.Add(-60 * 24 * time.Hour), Context: "Acme · Engineering"},
		{Kind: "docs.page", ID: "3", Title: "Launch", Link: "/docs/3", At: now, Context: "Acme · Sales"},
	}}
	raw := opsProvider{name: "channels", got: &rawGot, hits: []app.SearchResult{{Kind: "channels.message", ID: "m", Title: "Sam", Link: "/channels/c", At: now}}}
	withOps := opsProviderWithOps{opsProvider{name: "crm", ops: []string{"is"}, got: &opsGot, hits: []app.SearchResult{{Kind: "crm.deal", ID: "d", Title: "Launch deal", Link: "/crm/d", At: now}}}}
	s := &Service{providerTimeout: time.Second, d: &app.Deps{Modules: []app.Module{plain, raw, withOps}}}

	res := s.Run(context.Background(), &auth.Identity{}, Query{Text: "launch in:engineering during:month type:page", Limit: 20})
	if plainGot != "launch" {
		t.Errorf("plain provider got %q", plainGot)
	}
	if rawGot != "launch in:engineering during:month" {
		t.Errorf("raw provider got %q", rawGot)
	}
	ids := []string{}
	for _, h := range res.Items {
		ids = append(ids, h.ID)
	}
	if strings.Join(ids, ",") != "1" {
		t.Errorf("items = %v (want only page 1: in Engineering, this month, a page)", ids)
	}

	// is: only crm and channels can apply; docs is skipped and says why.
	res = s.Run(context.Background(), &auth.Identity{}, Query{Text: "launch is:open", Limit: 20})
	status := map[string]ProviderStatus{}
	for _, p := range res.Providers {
		status[p.Name] = p
	}
	if status["docs"].Status != "skipped" || !strings.Contains(status["docs"].Reason, "is:") || status["crm"].Status != "ok" || res.Partial {
		t.Errorf("providers = %+v partial=%v", res.Providers, res.Partial)
	}
	if opsGot != "launch" {
		t.Errorf("operator provider got %q", opsGot)
	}

	// Operators alone: a plain provider has nothing to search with.
	res = s.Run(context.Background(), &auth.Identity{}, Query{Text: "during:week", Limit: 20})
	for _, p := range res.Providers {
		if p.Name == "docs" && p.Status != "skipped" {
			t.Errorf("docs should be skipped without words: %+v", p)
		}
		if p.Name == "channels" && p.Status != "ok" {
			t.Errorf("channels reads operators alone: %+v", p)
		}
	}
	if _, err := NormalizeQuery("during:week"); err != nil {
		t.Errorf("operators alone refused: %v", err)
	}
}

func TestWindowSnippets(t *testing.T) {
	long := strings.Repeat("filler words here ", 8) + "the launch checklist is ready for review"
	hits := []app.SearchResult{{Snippet: long}}
	windowSnippets(hits, "launch")
	if !strings.HasPrefix(hits[0].Snippet, "…") || !strings.Contains(hits[0].Snippet, "launch checklist") {
		t.Errorf("snippet = %q", hits[0].Snippet)
	}
}
