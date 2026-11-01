package connectors

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if ips, ok := f[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func TestBlockedAddresses(t *testing.T) {
	strict := newGuard(false, false)
	dev := newGuard(true, true)
	cases := []struct {
		ip            string
		strict, devOK bool // blocked in production; allowed in development
	}{
		{"169.254.169.254", true, false}, // cloud metadata: always blocked
		{"fd00:ec2::254", true, false},
		{"100.100.100.200", true, false},
		{"fe80::1", true, false},
		{"0.0.0.0", true, false},
		{"127.0.0.1", true, true},
		{"::1", true, true},
		{"::ffff:127.0.0.1", true, true}, // v4 dressed as v6
		{"10.0.0.7", true, true},
		{"172.20.1.1", true, true},
		{"192.168.1.1", true, true},
		{"100.64.0.1", true, true},
		{"fc00::1", true, true},
		{"93.184.215.14", false, true},
		{"2606:4700::1111", false, true},
	}
	for _, c := range cases {
		ip := netip.MustParseAddr(c.ip)
		if got := strict.blocked(ip); got != c.strict {
			t.Errorf("strict.blocked(%s) = %v, want %v", c.ip, got, c.strict)
		}
		if got := !dev.blocked(ip); got != c.devOK {
			t.Errorf("dev allows %s = %v, want %v", c.ip, got, c.devOK)
		}
	}
}

func TestCheckURL(t *testing.T) {
	strict := newGuard(false, false)
	dev := newGuard(true, true)
	refusedStrict := []string{
		"http://example.com/mcp", // http outside development
		"http://169.254.169.254/latest/meta-data/",
		"https://169.254.169.254/",
		"https://127.0.0.1/mcp",
		"https://localhost/mcp",
		"https://db.internal/mcp",
		"https://user:pass@example.com/mcp",
		"https://metadata.google.internal/",
		"ftp://example.com/",
		"https://example.com/mcp#frag",
		"/relative",
	}
	for _, u := range refusedStrict {
		if _, err := strict.checkURL(u); err == nil {
			t.Errorf("strict.checkURL(%q) accepted", u)
		}
	}
	if _, err := strict.checkURL("https://mcp.example.com/mcp"); err != nil {
		t.Errorf("public https refused: %v", err)
	}
	if _, err := dev.checkURL("http://127.0.0.1:9000/mcp"); err != nil {
		t.Errorf("development refused a loopback http URL: %v", err)
	}
	for _, u := range []string{"http://169.254.169.254/", "https://metadata.google.internal/"} {
		if _, err := dev.checkURL(u); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("development accepted %q (err %v)", u, err)
		}
	}
}

func TestDialRefusesNamesResolvingPrivate(t *testing.T) {
	g := newGuard(false, false)
	g.resolver = fakeResolver{
		"evil.example":   {netip.MustParseAddr("10.1.2.3")},
		"meta.example":   {netip.MustParseAddr("169.254.169.254")},
		"mixed.example":  {netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("192.168.0.9")},
		"rebind.example": {netip.MustParseAddr("::ffff:10.0.0.1")},
	}
	for host := range g.resolver.(fakeResolver) {
		_, err := g.dial(context.Background(), "tcp", host+":443")
		if !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("dial(%s) err = %v, want ErrBlockedAddress", host, err)
		}
	}
}

func TestClientBlocksLoopbackOutsideDevelopment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer srv.Close()

	strict := newGuard(false, true) // http allowed so only the address decides
	_, err := strict.client(clientOptions{timeout: 5 * time.Second, maxBody: 1 << 10}).Get(srv.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback request err = %v, want ErrBlockedAddress", err)
	}
	dev := newGuard(true, true)
	resp, err := dev.client(clientOptions{timeout: 5 * time.Second, maxBody: 1 << 10}).Get(srv.URL)
	if err != nil {
		t.Fatalf("development loopback request failed: %v", err)
	}
	resp.Body.Close()
}

func TestRedirectToMetadataRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()
	g := newGuard(true, true) // even development refuses metadata
	_, err := g.client(clientOptions{timeout: 5 * time.Second, maxBody: 1 << 10, maxRedirects: 3}).Get(srv.URL)
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("redirect err = %v, want ErrBlockedAddress", err)
	}
	_, err = g.client(clientOptions{timeout: 5 * time.Second, maxBody: 1 << 10}).Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("no-redirect client err = %v, want a refused redirect", err)
	}
}

func TestBodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // no Content-Length: the cap must trip while reading
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	}))
	defer srv.Close()
	g := newGuard(true, true)
	resp, err := g.client(clientOptions{timeout: 5 * time.Second, maxBody: 1024}).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, errTooLarge) {
		t.Fatalf("read err = %v, want errTooLarge", err)
	}
}
