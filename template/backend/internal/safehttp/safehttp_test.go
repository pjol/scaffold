package safehttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestCheckURL(t *testing.T) {
	cases := []struct {
		url          string
		private, ok  bool
		allowHTTP    bool
		wantInsecure bool
	}{
		{url: "https://example.com/a", ok: true},
		{url: "http://example.com/a", wantInsecure: true},
		{url: "http://example.com/a", allowHTTP: true, ok: true},
		{url: "ftp://example.com/a", wantInsecure: true},
		{url: "https://user:pw@example.com/"},
		{url: "https://example.com/#x"},
		{url: "https://127.0.0.1/"},
		{url: "https://127.0.0.1/", private: true, ok: true},
		{url: "https://169.254.169.254/latest/meta-data", private: true},
		{url: "https://metadata.google.internal/", private: true},
		{url: "https://localhost/"},
		{url: "https://printer.local/"},
		{url: "https://[::ffff:10.0.0.1]/"},
		{url: "not a url"},
	}
	for _, c := range cases {
		_, err := CheckURL(c.url, c.private, c.allowHTTP)
		if (err == nil) != c.ok {
			t.Errorf("%s (private=%v http=%v): err=%v, want ok=%v", c.url, c.private, c.allowHTTP, err, c.ok)
		}
		if c.wantInsecure && !errors.Is(err, ErrInsecure) {
			t.Errorf("%s: want ErrInsecure, got %v", c.url, err)
		}
	}
}

// A hostname that resolves to a private address is refused at dial time,
// which is the check that matters (DNS rebinding, internal names).
func TestDialRefusesResolvedPrivate(t *testing.T) {
	g := newGuard(false, true)
	g.resolver = fakeResolver{"intranet.example": {netip.MustParseAddr("10.1.2.3")}}
	_, err := g.dial(context.Background(), "tcp", "intranet.example:443")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked, got %v", err)
	}
}

func TestBodyCapAndRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Write([]byte(strings.Repeat("x", 2048)))
		case "/redirect":
			http.Redirect(w, r, "/small", http.StatusFound)
		default:
			w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	c := Client(Options{Timeout: 5 * time.Second, MaxBody: 1024, AllowPrivate: true, AllowHTTP: true})
	resp, err := c.Get(srv.URL + "/big")
	if err == nil {
		_, err = readAll(resp)
	}
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big body: want ErrTooLarge, got %v", err)
	}
	if _, err := c.Get(srv.URL + "/redirect"); err == nil {
		t.Fatal("redirect followed with MaxRedirects 0")
	}
	c = Client(Options{Timeout: 5 * time.Second, MaxRedirects: 2, AllowPrivate: true, AllowHTTP: true})
	resp, err = c.Get(srv.URL + "/redirect")
	if err != nil {
		t.Fatalf("redirect with MaxRedirects 2: %v", err)
	}
	resp.Body.Close()

	// Loopback is refused without AllowPrivate.
	c = Client(Options{Timeout: 5 * time.Second, AllowHTTP: true})
	if _, err := c.Get(srv.URL + "/small"); !IsBlocked(err) {
		t.Fatalf("loopback without AllowPrivate: want blocked, got %v", err)
	}
}

type fakeResolver map[string][]netip.Addr

func (f fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return f[host], nil
}

func readAll(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var out []byte
	buf := make([]byte, 512)
	for {
		n, err := resp.Body.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, err
		}
	}
}

// A client told to wait gets the whole exchange for headers. A PS sends Prefer: wait=30 to a supervision server, and a 20 second
// cap on headers made every review longer than that look like an outage. Found by internal/conformance.
func TestHeaderTimeout(t *testing.T) {
	if got := (clientOptions{timeout: 40 * time.Second}).headerTimeout(); got != 20*time.Second {
		t.Errorf("default = %v", got)
	}
	if got := (clientOptions{timeout: 5 * time.Second}).headerTimeout(); got != 5*time.Second {
		t.Errorf("under the cap = %v", got)
	}
	if got := (clientOptions{timeout: 40 * time.Second, longHeaders: true}).headerTimeout(); got != 40*time.Second {
		t.Errorf("long = %v", got)
	}
}
