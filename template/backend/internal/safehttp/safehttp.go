// Package safehttp is the outbound HTTP client for URLs a person typed or a
// remote server advertised: link previews and bookmarks, embeds' oEmbed
// lookups, calendar subscriptions, webhooks to customer endpoints. Each of
// those is an SSRF surface by construction; this client is what keeps it
// from being one in fact. Promoted from the connectors feature's httpsafe.go
// (which keeps its own copy for its MCP discovery).
//
//   - The check runs at dial time against the resolved address actually
//     being connected to, so a hostname that resolves to 127.0.0.1 or a
//     DNS-rebinding answer is caught (checking the name is the version that
//     looks right and does nothing).
//   - Link-local and cloud metadata addresses are refused always, even in
//     development. Loopback, private, CGNAT and other non-public ranges are
//     refused unless Options.AllowPrivate (callers set it only in development).
//   - Plain http is refused unless Options.AllowHTTP, on every request and on
//     every redirect hop.
//   - No proxy from the environment (it would make the dial check inspect
//     the proxy instead of the target).
//   - Timeouts on dial, TLS, headers and the whole exchange; response bodies
//     are capped and a larger one fails instead of being truncated quietly.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrBlocked is returned when a URL resolves somewhere the server must not
// connect to.
var ErrBlocked = errors.New("that address is not reachable from this server")

// ErrInsecure is returned for http:// (and other schemes) unless AllowHTTP.
var ErrInsecure = errors.New("only https URLs are allowed")

// ErrTooLarge is returned when a response body passes the cap.
var ErrTooLarge = errors.New("the response was too large")

// Options shape one client.
type Options struct {
	Timeout      time.Duration // whole exchange, body included (default 10s)
	MaxBody      int64         // response body cap in bytes (default 2 MiB; <0 for none)
	MaxRedirects int           // 0 refuses redirects
	AllowPrivate bool          // loopback and private ranges: development only
	AllowHTTP    bool          // plain http: development only
	// LongHeaders lets the server take the whole Timeout to answer, for a caller that asked it to hold: Prefer: wait.
	// Otherwise headers must arrive within 20 seconds.
	LongHeaders bool
}

// Client returns an http.Client whose every connection and redirect passes
// the guard.
func Client(o Options) *http.Client {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.MaxBody == 0 {
		o.MaxBody = 2 << 20
	}
	return newGuard(o.AllowPrivate, o.AllowHTTP).client(clientOptions{timeout: o.Timeout, maxBody: o.MaxBody, maxRedirects: o.MaxRedirects, longHeaders: o.LongHeaders})
}

// CheckURL rejects what can be judged before a request (scheme, credentials,
// fragments, literal blocked IPs, well-known local names) so a form can say
// so at once. The dial check still runs on every connection.
func CheckURL(raw string, allowPrivate, allowHTTP bool) (*url.URL, error) {
	return newGuard(allowPrivate, allowHTTP).checkURL(raw)
}

// IsBlocked reports whether err comes from the guard (blocked address or
// insecure scheme), for honest error codes.
func IsBlocked(err error) bool {
	return errors.Is(err, ErrBlocked) || errors.Is(err, ErrInsecure)
}

var (
	// alwaysBlocked: link-local (every cloud metadata service lives here) and
	// the metadata addresses outside it.
	alwaysBlocked = prefixes(
		"169.254.0.0/16",     // IPv4 link-local, AWS/GCP/Azure/OCI metadata
		"fe80::/10",          // IPv6 link-local
		"fd00:ec2::254/128",  // AWS metadata over IPv6
		"100.100.100.200/32", // Alibaba Cloud metadata
		"0.0.0.0/8",          // "this host"
		"::/128",             // unspecified
		"224.0.0.0/4",        // multicast
		"ff00::/8",           // multicast
		"255.255.255.255/32", // broadcast
	)
	// privateBlocked: everything that isn't the public internet.
	privateBlocked = prefixes(
		"10.0.0.0/8",      // private
		"100.64.0.0/10",   // carrier-grade NAT
		"127.0.0.0/8",     // loopback
		"172.16.0.0/12",   // private
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"192.168.0.0/16",  // private
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"240.0.0.0/4",     // reserved
		"::1/128",         // loopback
		"fc00::/7",        // unique local
		"2001:db8::/32",   // documentation
		"64:ff9b::/96",    // NAT64 (can reach private v4)
		"64:ff9b:1::/48",  // local-use NAT64 (RFC 8215)
		"::/96",           // IPv4-compatible (deprecated; embeds a v4 address)
		"2002::/16",       // 6to4 (deprecated; embeds a v4 address)
	)
	// blockedSuffixes resolve locally on most machines; refusing them up front
	// gives a clear sentence instead of a dial error. Not a security
	// boundary: the dial check is.
	blockedSuffixes = []string{".localhost", ".local", ".internal", ".localdomain", ".home.arpa"}
	metadataNames   = []string{"metadata.google.internal", "metadata.goog", "metadata"}
)

func prefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}

// guard decides which addresses and schemes outbound requests may use.
type guard struct {
	allowPrivate bool // loopback and private ranges (development only)
	allowHTTP    bool // plain http (development only)
	resolver     interface {
		LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	}
	dialTimeout time.Duration
}

func newGuard(allowPrivate, allowHTTP bool) *guard {
	return &guard{allowPrivate: allowPrivate, allowHTTP: allowHTTP, resolver: net.DefaultResolver, dialTimeout: 5 * time.Second}
}

// blocked reports whether ip may not be dialled.
func (g *guard) blocked(ip netip.Addr) bool {
	if !ip.IsValid() {
		return true
	}
	ip = ip.Unmap() // a v4 address dressed as ::ffff:a.b.c.d is tested as v4
	for _, p := range alwaysBlocked {
		if p.Contains(ip) {
			return true
		}
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if g.allowPrivate {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	for _, p := range privateBlocked {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// dial resolves addr and connects to the first permitted address. The
// address checked is the address dialled.
func (g *guard) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = g.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("can't resolve %s: %w", host, err)
		}
	}
	d := &net.Dialer{Timeout: g.dialTimeout, KeepAlive: 30 * time.Second}
	var firstErr error
	for _, ip := range ips {
		if g.blocked(ip) {
			if firstErr == nil {
				firstErr = fmt.Errorf("%w (%s resolves to %s)", ErrBlocked, host, ip)
			}
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = fmt.Errorf("%w (%s has no address)", ErrBlocked, host)
	}
	return nil, firstErr
}

// checkURL rejects what can be judged before a request: the scheme,
// credentials in the URL, literal blocked IPs and well-known local names.
// The dial check still runs on every connection.
func (g *guard) checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%q is not an absolute URL", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !g.allowHTTP {
			return nil, ErrInsecure
		}
	default:
		return nil, ErrInsecure
	}
	if u.User != nil {
		return nil, errors.New("URLs with credentials in them are not accepted")
	}
	if u.Fragment != "" {
		return nil, errors.New("URLs with a fragment (#…) are not accepted")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if g.blocked(ip) {
			return nil, fmt.Errorf("%w (%s)", ErrBlocked, ip)
		}
		return u, nil
	}
	for _, m := range metadataNames {
		if host == m {
			return nil, fmt.Errorf("%w (%s)", ErrBlocked, host)
		}
	}
	if !g.allowPrivate {
		if host == "localhost" {
			return nil, fmt.Errorf("%w (%s)", ErrBlocked, host)
		}
		for _, s := range blockedSuffixes {
			if strings.HasSuffix(host, s) {
				return nil, fmt.Errorf("%w (%s)", ErrBlocked, host)
			}
		}
	}
	return u, nil
}

// clientOptions shape one guarded client.
type clientOptions struct {
	timeout      time.Duration // whole exchange, body included
	maxBody      int64         // response body cap
	maxRedirects int           // 0 refuses redirects
	longHeaders  bool          // headers may take the whole exchange
}

// headerTimeout is how long a server has to start answering.
func (o clientOptions) headerTimeout() time.Duration {
	if o.longHeaders {
		return o.timeout
	}
	return min(o.timeout, 20*time.Second)
}

// client returns an http.Client whose every connection passes the guard.
func (g *guard) client(o clientOptions) *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           g.dial,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: o.headerTimeout(),
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
	}
	return &http.Client{
		Timeout:   o.timeout,
		Transport: &guardedTransport{g: g, base: tr, maxBody: o.maxBody},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > o.maxRedirects {
				if o.maxRedirects == 0 {
					return fmt.Errorf("%s answered with a redirect, which isn't followed", via[0].URL.Host)
				}
				return errors.New("too many redirects")
			}
			if _, err := g.checkURL(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}

// guardedTransport re-checks every request URL (the first hop and each
// redirect) and caps response bodies.
type guardedTransport struct {
	g       *guard
	base    http.RoundTripper
	maxBody int64
}

func (t *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := t.g.checkURL(req.URL.String()); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if t.maxBody > 0 {
		if resp.ContentLength > t.maxBody {
			_ = resp.Body.Close()
			return nil, ErrTooLarge
		}
		resp.Body = &cappedBody{rc: resp.Body, left: t.maxBody}
	}
	return resp, nil
}

// cappedBody fails with ErrTooLarge once more than the cap has been read.
type cappedBody struct {
	rc   io.ReadCloser
	left int64
}

func (c *cappedBody) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// One more byte means the body is over the cap.
		var one [1]byte
		if n, _ := c.rc.Read(one[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.rc.Read(p)
	c.left -= int64(n)
	return n, err
}

func (c *cappedBody) Close() error { return c.rc.Close() }
