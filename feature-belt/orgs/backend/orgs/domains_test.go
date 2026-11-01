package orgs

import (
	"context"
	"net"
	"strings"
	"testing"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
)

func testService(env string) *Service {
	public := map[string]bool{}
	for _, d := range publicEmailDomains {
		public[d] = true
	}
	return &Service{d: &app.Deps{Cfg: &config.Config{Env: env}}, resolver: net.DefaultResolver, publicDomains: public}
}

func TestCleanDomain(t *testing.T) {
	ok := map[string]string{
		"acme.com":                "acme.com",
		"  Acme.COM. ":            "acme.com",
		"@acme.com":               "acme.com",
		"sam@eng.acme.co.uk":      "eng.acme.co.uk",
		"https://acme.com/about":  "acme.com",
		"xn--bcher-kva.de":        "xn--bcher-kva.de",
		"a-b.example-domain.io":   "a-b.example-domain.io",
		"verify-orgs.test":        "verify-orgs.test",
		"http://acme.com?x=1#top": "acme.com",
	}
	for in, want := range ok {
		got, err := cleanDomain(in)
		if err != nil || got != want {
			t.Errorf("cleanDomain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "com", "acme", "-acme.com", "acme-.com", "ac me.com", "bücher.de", "10.0.0.1", "acme..com", strings.Repeat("a", 64) + ".com"} {
		if got, err := cleanDomain(in); err == nil {
			t.Errorf("cleanDomain(%q) = %q, want an error", in, got)
		}
	}
}

func TestClaimable(t *testing.T) {
	prod, dev := testService("production"), testService("development")
	for _, d := range []string{"gmail.com", "outlook.com", "icloud.com", "yahoo.com", "proton.me", "localhost.localhost", "corp.local", "x.example"} {
		if prod.claimable(d) == nil {
			t.Errorf("%s should not be claimable", d)
		}
	}
	if prod.claimable("verify-orgs.test") == nil {
		t.Error(".test should not be claimable outside development")
	}
	if err := dev.claimable("verify-orgs.test"); err != nil {
		t.Errorf(".test should be claimable in development: %v", err)
	}
	if err := prod.claimable("acme.com"); err != nil {
		t.Errorf("acme.com should be claimable: %v", err)
	}
	if got := prod.emailDomain("sam@GMAIL.com"); got != "" {
		t.Errorf("public providers never match, got %q", got)
	}
	if got := prod.emailDomain("sam@Acme.com"); got != "acme.com" {
		t.Errorf("emailDomain = %q, want acme.com", got)
	}
}

func TestCheckTXT(t *testing.T) {
	s := testService("production")
	orig := lookupTXT
	t.Cleanup(func() { lookupTXT = orig })
	var asked string
	stub := func(records []string, err error) {
		lookupTXT = func(_ context.Context, _ *net.Resolver, name string) ([]string, error) {
			asked = name
			return records, err
		}
	}

	stub([]string{"other=1", "scaffoldapp-verify=abc123"}, nil)
	if err := s.checkTXT(context.Background(), "acme.com", "abc123"); err != nil {
		t.Fatalf("matching record: %v", err)
	}
	if asked != "_scaffoldapp-verify.acme.com." {
		t.Fatalf("looked up %q", asked)
	}

	stub([]string{`"scaffoldapp-verify=ABC123"`}, nil)
	if err := s.checkTXT(context.Background(), "acme.com", "abc123"); err != nil {
		t.Fatalf("quoted, upper-case record should match: %v", err)
	}

	stub([]string{"scaffoldapp-verify=wrong"}, nil)
	if err := s.checkTXT(context.Background(), "acme.com", "abc123"); err == nil || !strings.Contains(err.Error(), "doesn't match") {
		t.Fatalf("wrong value: %v", err)
	}

	stub(nil, &net.DNSError{Err: "no such host", Name: "_scaffoldapp-verify.acme.com", IsNotFound: true})
	if err := s.checkTXT(context.Background(), "acme.com", "abc123"); err == nil || !strings.Contains(err.Error(), "No TXT record") {
		t.Fatalf("missing record: %v", err)
	}

	stub(nil, &net.DNSError{Err: "i/o timeout", IsTimeout: true})
	if err := s.checkTXT(context.Background(), "acme.com", "abc123"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: %v", err)
	}
}
