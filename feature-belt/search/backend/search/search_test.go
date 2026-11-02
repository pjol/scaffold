package search

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

func TestNormalizeQuery(t *testing.T) {
	for in, want := range map[string]string{"  hello   world ": "hello world", "a\tb": "a b", "ab": "ab"} {
		if got, err := NormalizeQuery(in); err != nil || got != want {
			t.Errorf("NormalizeQuery(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", " a ", "\n"} {
		if _, err := NormalizeQuery(in); err == nil {
			t.Errorf("NormalizeQuery(%q) accepted", in)
		}
	}
	if got, _ := NormalizeQuery(strings.Repeat("x", 500)); len(got) != maxQueryChars {
		t.Errorf("not capped: %d", len(got))
	}
}

func TestMergeOrdersByScoreThenRecency(t *testing.T) {
	now := time.Now()
	a := []app.SearchResult{{Kind: "chat.message", ID: "1", Score: 0.5, At: now}, {Kind: "chat.message", ID: "2", Score: 0.9, At: now.Add(-time.Hour)}}
	b := []app.SearchResult{{Kind: "docs.page", ID: "3", Score: 0.5, At: now.Add(time.Minute)}, {Kind: "chat.message", ID: "1", Score: 0.5, At: now}}
	got := merge([][]app.SearchResult{a, b}, 10)
	ids := []string{}
	for _, h := range got {
		ids = append(ids, h.ID)
	}
	if strings.Join(ids, ",") != "2,3,1" {
		t.Errorf("order = %v, want 2,3,1 (duplicate dropped)", ids)
	}
	if len(merge([][]app.SearchResult{a, b}, 1)) != 1 {
		t.Error("limit not applied")
	}
}

func TestSanitizeEnforcesContract(t *testing.T) {
	in := []app.SearchResult{
		{Kind: "chat.message", ID: "1", Title: "ok", Link: "/chat/1", Score: 7},
		{Kind: "docs.page", ID: "2", Title: "wrong module", Link: "/docs/2"},
		{Kind: "chat.message", ID: "3", Title: "external", Link: "https://evil.example"},
		{Kind: "chat.message", ID: "4", Title: "protocol relative", Link: "//evil.example"},
		{Kind: "chat.message", ID: "", Title: "no id", Link: "/chat/5"},
		{Kind: "chat.message", ID: "6", Title: "  ", Link: "/chat/6"},
	}
	out := sanitize("chat", in, Query{Limit: 10})
	if len(out) != 1 || out[0].ID != "1" || out[0].Score != 1 {
		t.Errorf("sanitize = %+v", out)
	}
	if got := sanitize("chat", in[:1], Query{Limit: 10, Kinds: []string{"chat.thread"}}); len(got) != 0 {
		t.Error("kind filter not applied")
	}
}

func TestWants(t *testing.T) {
	if !wants(nil, "chat", "") || !wants([]string{"chat.message"}, "chat", "") || wants([]string{"docs"}, "chat", "") {
		t.Error("module selection")
	}
	if !wants([]string{"chat"}, "chat", "chat.message") || wants([]string{"chat.thread"}, "chat", "chat.message") {
		t.Error("kind selection")
	}
}

type fakeProvider struct {
	name  string
	delay time.Duration
	err   error
	panic bool
}

func (f fakeProvider) Name() string { return f.name }
func (f fakeProvider) Search(ctx context.Context, _ *auth.Identity, q string, _ int) ([]app.SearchResult, error) {
	if f.panic {
		panic("boom")
	}
	time.Sleep(f.delay) // ignores ctx on purpose
	return []app.SearchResult{{Kind: f.name + ".item", ID: "x", Title: q, Link: "/" + f.name, Score: 0.5}}, f.err
}

func TestRunReportsPartialResults(t *testing.T) {
	s := &Service{providerTimeout: 100 * time.Millisecond, d: &app.Deps{Modules: []app.Module{
		fakeProvider{name: "fast"},
		fakeProvider{name: "slow", delay: time.Second},
		fakeProvider{name: "broken", err: errors.New("down")},
		fakeProvider{name: "panics", panic: true},
	}}}
	start := time.Now()
	res := s.Run(context.Background(), &auth.Identity{}, Query{Text: "hello", Limit: 10})
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("a slow provider held the request for %s", time.Since(start))
	}
	if !res.Partial || len(res.Items) != 1 || res.Items[0].Kind != "fast.item" {
		t.Errorf("res = %+v", res)
	}
	got := map[string]string{}
	for _, p := range res.Providers {
		got[p.Name] = p.Status
	}
	if got["fast"] != "ok" || got["slow"] != "timeout" || got["broken"] != "error" || got["panics"] != "error" {
		t.Errorf("statuses = %v", got)
	}
}

// offPolicy is an app.AgentPolicy whose person turned the "hidden" feature
// off for agents (and whose lookups fail for "broken").
type offPolicy struct{}

func (offPolicy) Name() string { return "agents" }
func (offPolicy) FeatureLevel(_ context.Context, _ *auth.Identity, feature string) (app.AgentLevel, app.AgentLevel, error) {
	switch feature {
	case "hidden":
		return app.AgentOff, app.AgentOff, nil
	case "broken":
		return app.AgentOff, app.AgentOff, errors.New("down")
	}
	return app.AgentRead, app.AgentRead, nil
}
func (offPolicy) ResourceLevel(context.Context, *auth.Identity, string, string, string) (app.AgentLevel, error) {
	return app.AgentRead, nil
}
func (offPolicy) ResourceLevels(_ context.Context, _ *auth.Identity, _, _ string, ids []string) (map[string]app.AgentLevel, error) {
	out := map[string]app.AgentLevel{}
	for _, id := range ids {
		out[id] = app.AgentRead
	}
	return out, nil
}
func (offPolicy) GateTool(context.Context, *auth.Identity, string, app.Tool, json.RawMessage) (app.ToolDecision, error) {
	return app.ToolDecision{Action: app.ToolRun}, nil
}

func TestRunSkipsFeaturesOffForAgents(t *testing.T) {
	s := &Service{providerTimeout: time.Second, d: &app.Deps{Modules: []app.Module{
		offPolicy{}, fakeProvider{name: "open"}, fakeProvider{name: "hidden"}, fakeProvider{name: "broken"},
	}}}
	names := func(res Response) string {
		var out []string
		for _, h := range res.Items {
			out = append(out, h.Kind)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	if got := names(s.Run(context.Background(), &auth.Identity{}, Query{Text: "hello", Limit: 10})); got != "broken.item,hidden.item,open.item" {
		t.Errorf("person search = %s, want every feature", got)
	}
	ctx := auth.WithAgent(context.Background(), auth.Agent{Via: "mcp", Client: "Test"})
	res := s.Run(ctx, &auth.Identity{}, Query{Text: "hello", Limit: 10})
	if got := names(res); got != "open.item" {
		t.Errorf("agent search = %s, want only open.item (hidden is off, broken fails closed)", got)
	}
	for _, p := range res.Providers {
		if p.Name == "hidden" || p.Name == "broken" {
			t.Errorf("provider %s was asked for an agent", p.Name)
		}
	}
}

// orgProvider returns one hit per org and one in no org, and records the
// scope it saw.
type orgProvider struct{ saw *uuid.UUID }

func (orgProvider) Name() string { return "teams" }
func (p orgProvider) Search(ctx context.Context, _ *auth.Identity, q string, _ int) ([]app.SearchResult, error) {
	if id, ok := app.SearchOrg(ctx); ok {
		*p.saw = id
	}
	return []app.SearchResult{
		{Kind: "teams.team", ID: "a", Title: q, Link: "/a", OrgID: orgA.String(), Context: app.SearchContext("Org A", "", "Design")},
		{Kind: "teams.team", ID: "b", Title: q, Link: "/b", OrgID: strings.ToUpper(orgB.String())},
		{Kind: "teams.team", ID: "none", Title: q, Link: "/none"},
	}, nil
}

var (
	orgA = uuid.MustParse("6f1c1d1e-0000-4000-8000-00000000000a")
	orgB = uuid.MustParse("6f1c1d1e-0000-4000-8000-00000000000b")
)

func TestRunScopesToOrg(t *testing.T) {
	var saw uuid.UUID
	s := &Service{providerTimeout: time.Second, d: &app.Deps{Modules: []app.Module{orgProvider{&saw}}}}
	ids := func(res Response) string {
		var out []string
		for _, h := range res.Items {
			out = append(out, h.ID)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	all := s.Run(context.Background(), &auth.Identity{}, Query{Text: "hello", Limit: 10})
	if got := ids(all); got != "a,b,none" || saw != uuid.Nil {
		t.Errorf("unscoped = %s (saw %s), want a,b,none and no scope", got, saw)
	}
	if all.Items[0].Context != "Org A · Design" && all.Items[1].Context != "Org A · Design" && all.Items[2].Context != "Org A · Design" {
		t.Errorf("context not kept: %+v", all.Items)
	}
	if got := ids(s.Run(context.Background(), &auth.Identity{}, Query{Text: "hello", Limit: 10, OrgID: orgB})); got != "b,none" || saw != orgB {
		t.Errorf("scoped to B = %s (saw %s), want b,none (case-insensitive ids, no-org hits kept)", got, saw)
	}
	if _, err := parseOrg("nope"); err == nil {
		t.Error("parseOrg accepted a non-uuid")
	}
}

func TestSourcesSanitizedFacetedAndFiltered(t *testing.T) {
	now := time.Now()
	conn := app.Connection{ID: "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e", Provider: "google", Account: "sam@acme.com"}
	in := []app.SearchResult{
		{Kind: "files.file", ID: "1", Title: "Plan", Link: "/files/1", At: now, Score: 0.5},
		{Kind: "files.file", ID: "2", Title: "Plan (Drive)", Link: "/files?source=x", At: now, Score: 0.9, Source: app.ConnectedSource(conn, "drive", "javascript:alert(1)")},
		{Kind: "files.file", ID: "3", Title: "Broken source", Link: "/files/3", At: now, Source: &app.SourceRef{Provider: "Not A Provider"}},
		{Kind: "files.file", ID: "4", Title: "Plan (GitHub)", Link: "/repos", At: now, Score: 0.7, Source: &app.SourceRef{Type: "connected", Provider: "github", ConnectionID: "c2", Account: "octo"}},
	}
	clean := sanitize("files", in, Query{Limit: 10})
	if len(clean) != 3 {
		t.Fatalf("a malformed source must drop the hit, got %d hits", len(clean))
	}
	for _, h := range clean {
		if h.ID == "2" && h.Source.URL != "" {
			t.Fatalf("non-https source URL kept: %q", h.Source.URL)
		}
	}
	facets := sourceFacets(clean)
	if len(facets) != 3 || facets[0].ID != "app" || facets[0].Count != 1 || facets[1].ID != "provider:google" {
		t.Fatalf("facets: %+v", facets)
	}
	for filter, want := range map[string]int{"": 3, "app": 1, "connected": 2, "provider:google": 1, "provider:zoom": 0, conn.ID: 1} {
		n := 0
		for _, h := range clean {
			if sourceMatches(filter, h) {
				n++
			}
		}
		if n != want {
			t.Errorf("source %q kept %d, want %d", filter, n, want)
		}
	}
	for _, bad := range []string{"provider:", "provider:GOOGLE!", "x"} {
		if _, err := parseSource(bad); err == nil {
			t.Errorf("parseSource(%q) accepted", bad)
		}
	}
}
