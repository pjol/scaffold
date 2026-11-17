package aauth

import (
	"errors"
	"strings"
	"testing"
)

// The lists in protocol section "Server Identifiers", plus the rules they illustrate.
func TestServerIdentifierSpecExamples(t *testing.T) {
	valid := []string{
		"https://agent.example",
		"https://xn--nxasmq6b.example",
		"https://ps.sayso.app",
	}
	for _, id := range valid {
		if err := ValidateServerIdentifier(id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	invalid := []string{
		"http://agent.example",       // not HTTPS
		"https://Agent.Example",      // not lowercase
		"https://agent.example:8443", // contains port
		"https://agent.example/v1",   // contains path
		"https://agent.example/",     // trailing slash
		"https://agent.example?q=1",  // query
		"https://agent.example#f",    // fragment
		"https://user@agent.example", // credentials
		"HTTPS://agent.example",      // scheme not lowercase
		"https://agent.example.",     // trailing dot
		"https://exämple.example",    // not A-label form
		"https://-agent.example",     // bad label
		"https://127.0.0.1",          // an address, not a domain
		"https://2130706433",         // an address in decimal
		"https://0x7f.0.0.1",         // an address in hex
		"https://[::1]",              // an address
		"https://",                   // no host
		"agent.example",              // no scheme
		"",                           // empty
	}
	for _, id := range invalid {
		if err := ValidateServerIdentifier(id); !errors.Is(err, ErrInvalidIdentifier) {
			t.Errorf("%q: want ErrInvalidIdentifier, got %v", id, err)
		}
	}
}

func TestLoopbackIssuersOnlyUnderPolicy(t *testing.T) {
	dev := IdentifierPolicy{AllowInsecureLoopback: true}
	for _, id := range []string{"http://localhost:22260", "http://127.0.0.1:22260", "http://localhost", "http://[::1]:22260", "https://localhost:8443"} {
		if err := dev.ValidateServerIdentifier(id); err != nil {
			t.Errorf("dev %s: %v", id, err)
		}
		if err := ValidateServerIdentifier(id); err == nil {
			t.Errorf("production accepted %s", id)
		}
	}
	for _, id := range []string{"http://example.com:22260", "http://localhost:22260/v1", "http://localhost:0", "http://localhost:99999", "http://10.0.0.1:80", "ftp://localhost:21"} {
		if err := dev.ValidateServerIdentifier(id); err == nil {
			t.Errorf("dev accepted %s", id)
		}
	}
	// The production rule still holds under the dev policy.
	if err := dev.ValidateServerIdentifier("https://agent.example"); err != nil {
		t.Error(err)
	}
}

// The lists in protocol section "Agent Identifiers".
func TestAgentIdentifierSpecExamples(t *testing.T) {
	for _, id := range []string{
		"aauth:assistant-v2@agent.example",
		"aauth:planner.7f3c@vendor.example",
		"aauth:planner.7f3c+search1@vendor.example",
		"aauth:Agent@agent.example", // PS-104: uppercase is valid
		"aauth:a_b.C-1@agent.example",
	} {
		if err := ValidateAgentIdentifier(id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for _, id := range []string{
		"My Agent@agent.example",                               // space, and no scheme
		"aauth:My Agent@agent.example",                         // space in local part
		"@agent.example",                                       // empty local part, no scheme
		"aauth:@agent.example",                                 // empty local part
		"agent@http://agent.example",                           // domain includes scheme
		"aauth:agent@http://agent.example",                     // domain includes scheme
		"aauth:agent@agent.example:8443",                       // port
		"aauth:agent@Agent.Example",                            // uppercase domain
		"aauth:a@b@agent.example",                              // two @
		"aauth:agent",                                          // no domain
		"aauth:a/b@agent.example",                              // bad character
		"aauth:" + strings.Repeat("a", 256) + "@agent.example", // too long
		"",
	} {
		if err := ValidateAgentIdentifier(id); !errors.Is(err, ErrInvalidIdentifier) {
			t.Errorf("%q: want ErrInvalidIdentifier, got %v", id, err)
		}
	}
	if err := ValidateAgentIdentifier("aauth:" + strings.Repeat("a", 255) + "@agent.example"); err != nil {
		t.Errorf("255 characters: %v", err)
	}
}

// PS-104: "Compare exactly and don't case-fold."
func TestAgentIdentifierComparisonIsCaseSensitive(t *testing.T) {
	if SameAgentIdentifier("aauth:Agent@agent.example", "aauth:agent@agent.example") {
		t.Fatal("identifiers differing in case compared equal")
	}
	a, err := ParseAgentIdentifier("aauth:AgEnT@agent.example")
	if err != nil {
		t.Fatal(err)
	}
	if a.Local != "AgEnT" || a.String() != "aauth:AgEnT@agent.example" {
		t.Fatalf("local part was changed: %+v", a)
	}
	if SameServerIdentifier("https://a.example", "https://A.example") {
		t.Fatal("server identifiers differing in case compared equal")
	}
}

// "For protocol decisions, verifiers rely on parent_agent, not on parsing the local part". A `+` is in the character set and says nothing here, however many there are. It's a rule of how an agent provider names what it issues.
func TestVerifiersReadNothingIntoAPlus(t *testing.T) {
	for _, id := range []string{"aauth:a+b@agent.example", "aauth:+x@agent.example", "aauth:x+@agent.example", "aauth:a+b+c@agent.example"} {
		if err := ValidateAgentIdentifier(id); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestAgentPlusRules(t *testing.T) {
	p := IdentifierPolicy{}
	if err := p.ValidateTopLevelAgentIdentifier("aauth:planner.7f3c+search1@vendor.example"); err == nil {
		t.Error("a top-level agent must not contain +")
	}
	if err := p.ValidateTopLevelAgentIdentifier("aauth:planner.7f3c@vendor.example"); err != nil {
		t.Error(err)
	}
	if err := p.ValidateSubAgentIdentifier("aauth:planner.7f3c+search1@vendor.example", "aauth:planner.7f3c@vendor.example"); err != nil {
		t.Error(err)
	}
	for _, c := range [][2]string{
		{"aauth:other+search1@vendor.example", "aauth:planner.7f3c@vendor.example"},
		{"aauth:planner.7f3c+search1@other.example", "aauth:planner.7f3c@vendor.example"},
		{"aauth:planner.7f3c@vendor.example", "aauth:planner.7f3c@vendor.example"},
		{"aauth:a+b@vendor.example", "aauth:x+y@vendor.example"},
		{"aauth:planner.7f3c+a+b@vendor.example", "aauth:planner.7f3c@vendor.example"},
	} {
		if err := p.ValidateSubAgentIdentifier(c[0], c[1]); err == nil {
			t.Errorf("%s accepted as sub-agent of %s", c[0], c[1])
		}
	}
}

func TestAgentBelongsToIssuer(t *testing.T) {
	p := IdentifierPolicy{}
	if !p.AgentBelongsToIssuer("aauth:a@agent.example", "https://agent.example") {
		t.Error("same domain")
	}
	if p.AgentBelongsToIssuer("aauth:a@good.example", "https://evil.example") {
		t.Error("an issuer must not speak for another domain's agents")
	}
	dev := IdentifierPolicy{AllowInsecureLoopback: true}
	if !dev.AgentBelongsToIssuer("aauth:a@localhost", "http://localhost:22261") ||
		!dev.AgentBelongsToIssuer("aauth:a@localhost:22261", "http://localhost:22261") {
		t.Error("loopback agents with and without the port")
	}
}

// protocol, Endpoint and Other URLs.
func TestEndpointURLs(t *testing.T) {
	if err := ValidateEndpointURL("https://ps.example/token"); err != nil {
		t.Error(err)
	}
	for _, u := range []string{"http://ps.example/token", "https://ps.example/token?x=1", "https://ps.example/token#f", "https://ps.example/token?", "/token", "https://u:p@ps.example/token"} {
		if err := ValidateEndpointURL(u); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	if err := ValidateHTTPSURL("https://ps.example/jwks?x=1"); err != nil {
		t.Errorf("jwks_uri may carry a query: %v", err)
	}
	if err := ValidateHTTPSURL("http://ps.example/jwks"); err == nil {
		t.Error("jwks_uri must use https")
	}
}
