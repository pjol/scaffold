package connectors

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

// Every outbound request this package makes (protected-resource and
// authorization-server metadata, client registration, token and revocation
// endpoints, the MCP servers themselves) goes to a URL a staff member or a
// user typed, or one a remote server advertised. That is an SSRF surface by
// construction; the guard below is what keeps it from being one in fact.
// Distilled from OLEARY/crm internal/integration/httpsafe.go.
//
//   - The check runs at dial time against the resolved address actually
//     being connected to, so a hostname that resolves to 127.0.0.1 or a
//     DNS-rebinding answer is caught (checking the name is the version that
//     looks right and does nothing).
//   - Link-local and cloud metadata addresses are refused always, even in
//     development. Loopback, private, CGNAT and other non-public ranges are
//     refused unless allowPrivate (CONNECTORS_ALLOW_PRIVATE_NETWORKS, which
//     only development accepts).
//   - Plain http is refused outside development, on every request and on
//     every redirect hop.
//   - No proxy from the environment (it would make the dial check inspect
//     the proxy instead of the target).
//   - Timeouts on dial, TLS, headers and the whole exchange; response bodies
//     are capped and a larger one fails instead of being truncated quietly.

// ErrBlockedAddress is returned when a URL resolves somewhere the server
// must not connect to.
var ErrBlockedAddress = errors.New("connectors: that address is not reachable from this server")

// errInsecureURL is returned for http:// outside development.
var errInsecureURL = errors.New("connectors: only https URLs are allowed")

// errTooLarge is returned when a response body passes the cap.
var errTooLarge = errors.New("connectors: the response was too large")

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
	// blockedNames resolve locally on most machines; refusing them up front
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
			return nil, fmt.Errorf("connectors: can't resolve %s: %w", host, err)
		}
	}
	d := &net.Dialer{Timeout: g.dialTimeout, KeepAlive: 30 * time.Second}
	var firstErr error
	for _, ip := range ips {
		if g.blocked(ip) {
			if firstErr == nil {
				firstErr = fmt.Errorf("%w (%s resolves to %s)", ErrBlockedAddress, host, ip)
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
		firstErr = fmt.Errorf("%w (%s has no address)", ErrBlockedAddress, host)
	}
	return nil, firstErr
}

// checkURL rejects what can be judged before a request: the scheme,
// credentials in the URL, literal blocked IPs and well-known local names.
// The dial check still runs on every connection.
func (g *guard) checkURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("connectors: %q is not an absolute URL", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !g.allowHTTP {
			return nil, errInsecureURL
		}
	default:
		return nil, errInsecureURL
	}
	if u.User != nil {
		return nil, errors.New("connectors: URLs with credentials in them are not accepted")
	}
	if u.Fragment != "" {
		return nil, errors.New("connectors: URLs with a fragment (#…) are not accepted")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if g.blocked(ip) {
			return nil, fmt.Errorf("%w (%s)", ErrBlockedAddress, ip)
		}
		return u, nil
	}
	for _, m := range metadataNames {
		if host == m {
			return nil, fmt.Errorf("%w (%s)", ErrBlockedAddress, host)
		}
	}
	if !g.allowPrivate {
		if host == "localhost" {
			return nil, fmt.Errorf("%w (%s)", ErrBlockedAddress, host)
		}
		for _, s := range blockedSuffixes {
			if strings.HasSuffix(host, s) {
				return nil, fmt.Errorf("%w (%s)", ErrBlockedAddress, host)
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
}

// client returns an http.Client whose every connection passes the guard.
func (g *guard) client(o clientOptions) *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           g.dial,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: min(o.timeout, 20*time.Second),
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
					return fmt.Errorf("connectors: %s answered with a redirect, which isn't followed", via[0].URL.Host)
				}
				return errors.New("connectors: too many redirects")
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
			return nil, errTooLarge
		}
		resp.Body = &cappedBody{rc: resp.Body, left: t.maxBody}
	}
	return resp, nil
}

// cappedBody fails with errTooLarge once more than the cap has been read.
type cappedBody struct {
	rc   io.ReadCloser
	left int64
}

func (c *cappedBody) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// One more byte means the body is over the cap.
		var one [1]byte
		if n, _ := c.rc.Read(one[:]); n > 0 {
			return 0, errTooLarge
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

// isBlocked reports whether err comes from the guard (for honest codes).
func isBlocked(err error) bool {
	return errors.Is(err, ErrBlockedAddress) || errors.Is(err, errInsecureURL)
}
