package connectors

import (
	"net/url"
	"slices"
	"testing"
)

func testProvider(t *testing.T, id string) *provider {
	t.Helper()
	for _, p := range registry() {
		if p.ID == id {
			p.vars = map[string]string{"base": "https://gitlab.example.com", "login": "https://login.salesforce.com", "tenant": "common"}
			return p
		}
	}
	t.Fatalf("no provider %s", id)
	return nil
}

func TestScopesIncrementalAndUnion(t *testing.T) {
	g := testProvider(t, "google")
	granted := []string{"openid", "email", "https://www.googleapis.com/auth/drive.readonly"}
	if m := g.missingScopes(granted, []string{"https://www.googleapis.com/auth/userinfo.email"}); len(m) != 0 {
		t.Fatalf("email alias should count as granted, missing %v", m)
	}
	need := g.missingScopes(granted, []string{"https://www.googleapis.com/auth/calendar.readonly"})
	req := g.requestScopes(granted, need)
	if slices.Contains(req, "https://www.googleapis.com/auth/drive.readonly") {
		t.Fatalf("incremental request must not repeat earlier grants: %v", req)
	}
	if !slices.Contains(req, "https://www.googleapis.com/auth/calendar.readonly") {
		t.Fatalf("request lacks the new scope: %v", req)
	}
	// Broader scopes imply narrower ones.
	if m := g.missingScopes([]string{"https://www.googleapis.com/auth/drive"}, []string{"https://www.googleapis.com/auth/drive.readonly"}); len(m) != 0 {
		t.Fatalf("drive implies drive.readonly, missing %v", m)
	}
	gh := testProvider(t, "github")
	req = gh.requestScopes([]string{"repo"}, []string{"workflow"})
	if !slices.Contains(req, "repo") || !slices.Contains(req, "workflow") {
		t.Fatalf("GitHub re-auth must ask for the union: %v", req)
	}
	ms := testProvider(t, "microsoft")
	if m := ms.missingScopes([]string{"https://graph.microsoft.com/Files.Read.All"}, []string{"files.read"}); len(m) != 0 {
		t.Fatalf("Microsoft scopes fold case and the Graph prefix, missing %v", m)
	}
	n := testProvider(t, "notion")
	if m := n.missingScopes(nil, []string{"anything"}); m != nil {
		t.Fatalf("Notion has no scopes: %v", m)
	}
}

func TestHostRules(t *testing.T) {
	gl := testProvider(t, "gitlab")
	rules := gl.rules(gl.API, "https://git.acme.io/gitlab", "")
	if len(rules) != 1 || rules[0].Host != "git.acme.io" || rules[0].Prefix != "/gitlab/api/" {
		t.Fatalf("self-managed rule: %+v", rules)
	}
	if !rules[0].match("git.acme.io", "/gitlab/api/v4/projects") || rules[0].match("git.acme.io", "/gitlab/users/sign_in") || rules[0].match("gitlab.com", "/gitlab/api/v4") {
		t.Fatal("self-managed rule matches wrongly")
	}
	g := testProvider(t, "google")
	r := g.rules(g.API, "", "")[0]
	if !r.match("www.googleapis.com", "/drive/v3/files") || r.match("googleapis.com.evil.io", "/") {
		t.Fatal("suffix rule matches wrongly")
	}
	o, _ := url.Parse("http://127.0.0.1:9/g")
	g.override = o
	r = g.rules(g.API, "", "")[0]
	if !r.match("127.0.0.1:9", "/g/www.googleapis.com/drive/v3/files") || r.match("127.0.0.1:9", "/g/evil.example.com/x") || r.match("127.0.0.1:9", "/gh/api.github.com/user") {
		t.Fatalf("override suffix rule matches wrongly: %+v", r)
	}
	if got := g.expand(g.TokenURL, "", "", nil); got != "http://127.0.0.1:9/g/oauth2.googleapis.com/token" {
		t.Fatalf("override rewrite: %s", got)
	}
	sf := testProvider(t, "salesforce")
	if err := sf.checkInstanceURL("https://acme.my.salesforce.com"); err != nil {
		t.Fatal(err)
	}
	if err := sf.checkInstanceURL("https://evil.example.com"); err == nil {
		t.Fatal("non-Salesforce instance accepted")
	}
	if err := sf.checkInstanceURL("http://acme.my.salesforce.com"); err == nil {
		t.Fatal("http instance accepted")
	}
}

func TestSafeReturnTo(t *testing.T) {
	for in, want := range map[string]string{"/files?x=1": "/files?x=1", "//evil.com": "", "https://evil.com": "", "/\\evil": "", "files": "", "": ""} {
		if got := safeReturnTo(in); got != want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitPath(t *testing.T) {
	for p, ok := range map[string]bool{"/acme/app.git/info/refs": true, "/acme/app.git/git-upload-pack": true, "/acme/app.git/git-receive-pack": false, "/settings": false} {
		if gitPath.MatchString(p) != ok {
			t.Errorf("gitPath %s", p)
		}
	}
}
