package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Pure rules of the authorization server. The flows themselves are verified
// against a running stack with a real MCP client (FEATURE.md → Provenance).

func TestRedirectKind(t *testing.T) {
	for raw, want := range map[string]string{
		"https://claude.ai/api/mcp/auth_callback":      "web",
		"http://127.0.0.1:33418/callback":              "loopback",
		"http://localhost:6274/oauth/callback":         "loopback",
		"http://[::1]:8080/cb":                         "loopback",
		"cursor://anysphere.cursor-retrieval/oauth/cb": "app",
		"com.example.app:/oauth2redirect":              "app",
		"http://evil.example/cb":                       "",
		"https://claude.ai/cb#frag":                    "",
		"javascript:alert(1)":                          "",
		"data:text/html,hi":                            "",
		"https://user:pw@example.com/cb":               "",
		"file:///etc/passwd":                           "",
		"":                                             "",
		"https://exa mple.com/cb":                      "",
		"https://" + strings.Repeat("a", maxURILength): "",
	} {
		got, ok := redirectKind(raw)
		if !ok {
			got = ""
		}
		if got != want {
			t.Errorf("redirectKind(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestMatchRedirect(t *testing.T) {
	reg := []string{"http://127.0.0.1:33418/callback", "https://app.example/cb"}
	ok := []string{"http://127.0.0.1:33418/callback", "http://127.0.0.1:50123/callback", "https://app.example/cb"}
	bad := []string{
		"https://app.example/cb/../x", "https://app.example/cb?x=1", "https://app.example/CB",
		"http://127.0.0.1:33418/callback/extra", "http://localhost:33418/callback", "http://127.0.0.1:1/callback?x=1",
		"https://evil.example/cb", "", "http://127.0.0.1:33418/callback#f",
	}
	for _, u := range ok {
		if _, match := matchRedirect(reg, u); !match {
			t.Errorf("%q should match", u)
		}
	}
	for _, u := range bad {
		if _, match := matchRedirect(reg, u); match {
			t.Errorf("%q should not match", u)
		}
	}
	if got, match := matchRedirect([]string{"https://one.example/cb"}, ""); !match || got != "https://one.example/cb" {
		t.Errorf("a single registered URI should be the default, got %q %v", got, match)
	}
}

func TestPKCE(t *testing.T) {
	verifier := strings.Repeat("abcDEF123-._~", 4) // 52 chars
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !verifyPKCE(verifier, challenge) {
		t.Fatal("valid verifier refused")
	}
	for _, v := range []string{"", "short", verifier + "x", strings.Repeat("a", 129), strings.Repeat("a", 42) + " "} {
		if verifyPKCE(v, challenge) {
			t.Errorf("verifier %q accepted", v)
		}
	}
	if verifyPKCE(verifier, "") {
		t.Error("empty challenge accepted")
	}
}

func TestParseScopes(t *testing.T) {
	cases := map[string]string{
		"":                          "read",
		"read":                      "read",
		"write":                     "read write",
		"read write":                "read write",
		"openid profile read":       "read",
		"read write offline_access": "read write",
	}
	for in, want := range cases {
		got, err := parseScopes(in)
		if err != nil || strings.Join(got, " ") != want {
			t.Errorf("parseScopes(%q) = %v, %v; want %q", in, got, err, want)
		}
	}
	if _, err := parseScopes("read admin"); err == nil {
		t.Error("unknown scope accepted")
	}
}

func TestCanonicalResource(t *testing.T) {
	for in, want := range map[string]string{
		"https://API.example.com/mcp/": "https://api.example.com/mcp",
		"https://api.example.com":      "https://api.example.com",
		"HTTPS://api.example.com/mcp":  "https://api.example.com/mcp",
		"http://localhost:20000/mcp":   "http://localhost:20000/mcp",
	} {
		if got, ok := canonicalResource(in); !ok || got != want {
			t.Errorf("canonicalResource(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"api.example.com", "https://api.example.com/mcp#x", "https://api.example.com/mcp?x=1", "/mcp"} {
		if _, ok := canonicalResource(bad); ok {
			t.Errorf("canonicalResource(%q) accepted", bad)
		}
	}
}

func TestPublicAddr(t *testing.T) {
	for _, a := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1",
		"0.0.0.0", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "64:ff9b::a00:1", "224.0.0.1", "198.18.0.1"} {
		if publicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s treated as public", a)
		}
	}
	for _, a := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !publicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s treated as non-public", a)
		}
	}
}

func TestMetadataURLAndDocument(t *testing.T) {
	for _, bad := range []string{"http://app.example/c.json", "https://app.example", "https://app.example/", "https://app.example/a/../c.json",
		"https://u:p@app.example/c.json", "https://app.example/c.json#x"} {
		if _, ok := validMetadataURL(bad); ok {
			t.Errorf("%q accepted as a metadata document URL", bad)
		}
	}
	id := "https://app.example/oauth/client.json"
	if _, ok := validMetadataURL(id); !ok {
		t.Fatal("valid metadata URL refused")
	}
	good := `{"client_id":"` + id + `","client_name":"Example​  App","redirect_uris":["http://127.0.0.1:3000/cb"],"token_endpoint_auth_method":"none"}`
	doc, err := parseMetadataDoc(id, []byte(good))
	if err != nil || doc.name != "Example App" || len(doc.redirectURIs) != 1 {
		t.Fatalf("good document: %+v %v", doc, err)
	}
	for name, body := range map[string]string{
		"mismatched id": `{"client_id":"https://other.example/c.json","redirect_uris":["https://a.example/cb"]}`,
		"secret":        `{"client_id":"` + id + `","client_secret":"x","redirect_uris":["https://a.example/cb"]}`,
		"no redirects":  `{"client_id":"` + id + `","redirect_uris":[]}`,
		"bad redirect":  `{"client_id":"` + id + `","redirect_uris":["http://evil.example/cb"]}`,
		"jwt auth":      `{"client_id":"` + id + `","redirect_uris":["https://a.example/cb"],"token_endpoint_auth_method":"private_key_jwt"}`,
		"no code grant": `{"client_id":"` + id + `","redirect_uris":["https://a.example/cb"],"grant_types":["client_credentials"]}`,
		"not an object": `[]`,
	} {
		if _, err := parseMetadataDoc(id, []byte(body)); err == nil {
			t.Errorf("%s: document accepted", name)
		}
	}
}

func TestCacheTTL(t *testing.T) {
	for h, want := range map[string]time.Duration{
		"":                     metadataDefault,
		"max-age=60":           metadataMinTTL,
		"public, max-age=7200": 2 * time.Hour,
		"max-age=999999":       metadataMaxTTL,
		"no-store":             metadataMinTTL,
	} {
		if got := cacheTTL(h); got != want {
			t.Errorf("cacheTTL(%q) = %s, want %s", h, got, want)
		}
	}
}

func TestCleanName(t *testing.T) {
	if got := cleanName("  Claude‮\n Desktop "); got != "Claude Desktop" {
		t.Errorf("cleanName = %q", got)
	}
	if got := cleanName(strings.Repeat("x", 200)); len([]rune(got)) != 80 {
		t.Errorf("cleanName didn't cap length: %d", len(got))
	}
}
