package repos

import (
	"net/http"
	"testing"

	"scaffoldapp/backend/internal/app"
)

func TestExtID(t *testing.T) {
	ok := map[string]string{
		"github~6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e~acme~web":          "acme/web",
		"gitlab~6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e~acme~platform~api": "acme/platform/api",
		"gitlab~c1~group~sub.group~my-project_2":                        "group/sub.group/my-project_2",
	}
	for s, path := range ok {
		x, good := parseExtID(s)
		if !good || x.Path != path || x.String() != s {
			t.Errorf("parseExtID(%q) = %+v, %v", s, x, good)
		}
	}
	for _, s := range []string{
		"", "github~c1~acme", "github~c1~a~b~c", "bitbucket~c1~a~b", "github~c/1~a~b", "github~c1~..~b",
		"github~c1~a b~c", "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e", "gitlab~c1~a~%2e%2e",
	} {
		if _, good := parseExtID(s); good {
			t.Errorf("parseExtID(%q) accepted", s)
		}
	}
	x, _ := newExtID(ProviderGitLab, "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e", "acme/platform/api")
	if got := x.target(); got != "6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e:acme/platform/api" {
		t.Errorf("target = %q", got)
	}
	if conn, ok := app.TargetConnection(ExternalKind, x.target()); !ok || conn != x.ConnID {
		t.Errorf("TargetConnection = %q, %v", conn, ok)
	}
	if isExternalID("6f1c0f1e-8a7b-4c62-9d55-1f2a3b4c5d6e") || !isExternalID(x.String()) {
		t.Error("isExternalID")
	}
}

func TestPages(t *testing.T) {
	h := http.Header{}
	h.Set("Link", `<https://api.github.com/user/repos?per_page=30&page=3>; rel="next", <https://api.github.com/user/repos?per_page=30&page=5>; rel="last"`)
	if nextPage(h) != 3 || lastPage(h) != 5 {
		t.Errorf("github pages: %d %d", nextPage(h), lastPage(h))
	}
	g := http.Header{}
	g.Set("X-Next-Page", "2")
	g.Set("X-Total", "61")
	if nextPage(g) != 2 || lastPage(g) != 61 {
		t.Errorf("gitlab pages: %d %d", nextPage(g), lastPage(g))
	}
	g.Set("X-Next-Page", "")
	if nextPage(g) != 0 {
		t.Error("gitlab last page")
	}
}

func TestPlatformMessage(t *testing.T) {
	cases := map[string]string{
		`{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for acme:x."}]}`: "Validation Failed: A pull request already exists for acme:x.",
		`{"message":"405 Method Not Allowed"}`:         "405 Method Not Allowed",
		`{"message":{"source_branch":["is invalid"]}}`: "source_branch is invalid",
		`Retry later`: "Retry later",
	}
	for in, want := range cases {
		if got := platformMessage([]byte(in)); got != want {
			t.Errorf("platformMessage(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildDiff(t *testing.T) {
	d := buildDiff([]patchFile{
		{Path: "a.go", Status: "modified", Additions: 1, Deletions: 1, Patch: "@@ -1,2 +1,2 @@ func x\n a\n-b\n+c"},
		{Path: "img.png", Status: "added"},
		{Path: "big.txt", Status: "modified", Additions: 900},
		{Path: "logo.bin", Status: "modified"},
	}, false, "")
	if len(d.Files) != 4 || d.Additions != 901 || d.Deletions != 1 {
		t.Fatalf("diff totals: %+v", d)
	}
	if h := d.Files[0].Hunks; len(h) != 1 || h[0].Header != "func x" || len(h[0].Lines) != 3 || h[0].Lines[2].New != 2 {
		t.Errorf("hunks: %+v", h)
	}
	if !d.Files[1].Binary || !d.Files[2].Truncated || !d.Files[3].Binary {
		t.Errorf("truncated/binary: %+v %+v", d.Files[2], d.Files[3])
	}
	if one := buildDiff([]patchFile{{Path: "a"}, {Path: "b"}}, false, "b"); len(one.Files) != 1 {
		t.Error("onlyPath")
	}
}

func TestEndpoints(t *testing.T) {
	api, web, gql := endpoints(app.Connection{Provider: ProviderGitLab, Endpoints: map[string]string{"web": "https://git.example.com/gitlab/"}})
	if api != "https://git.example.com/gitlab/api/v4" || web != "https://git.example.com/gitlab" || gql != "https://git.example.com/gitlab/api/graphql" {
		t.Errorf("self-managed gitlab: %s %s %s", api, web, gql)
	}
	api, web, _ = endpoints(app.Connection{Provider: ProviderGitHub})
	if api != "https://api.github.com" || web != "https://github.com" {
		t.Errorf("github: %s %s", api, web)
	}
	api, _, _ = endpoints(app.Connection{Provider: ProviderGitHub, Endpoints: map[string]string{"web": "https://ghe.example.com"}})
	if api != "https://ghe.example.com/api/v3" {
		t.Errorf("ghes: %s", api)
	}
}

func TestLevelsAndBranches(t *testing.T) {
	if levelFrom(true, true, true, false) != "view" || levelFrom(false, true, true, true) != "edit" || levelFrom(true, true, true, true) != "manage" {
		t.Error("levelFrom")
	}
	for b, want := range map[string]bool{"feature/x": true, "main": true, "-x": false, "a..b": false, "a b": false, ".hidden/x": false, "x.lock": false, "a//b": false} {
		if validBranchName(b) != want {
			t.Errorf("validBranchName(%q) != %v", b, want)
		}
	}
}
