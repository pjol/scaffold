package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Client ID metadata documents (draft-ietf-oauth-client-id-metadata-document,
// which the 2025-11-25 MCP spec asks authorization servers to support): a
// client_id that is an https URL names a JSON document listing the client's
// name and redirect URIs. Fetching a URL a stranger chose is an SSRF surface,
// so the fetch resolves the host itself, refuses every non-public address,
// dials the address it checked (no DNS rebinding), follows no redirects, and
// reads at most 5 KB within 5 seconds.

const (
	metadataMaxBytes = 5 << 10
	metadataTimeout  = 5 * time.Second
	metadataMinTTL   = 5 * time.Minute
	metadataMaxTTL   = 24 * time.Hour
	metadataDefault  = time.Hour
)

type clientMetadata struct {
	name         string
	redirectURIs []string
}

// validMetadataURL applies the draft's rules to a client_id URL: https, a
// path other than "/", no dot segments, no fragment, no credentials.
func validMetadataURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || len(raw) > maxURILength {
		return nil, false
	}
	if u.Path == "" || u.Path == "/" {
		return nil, false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return nil, false
		}
	}
	return u, true
}

// nonPublic lists ranges netip's helpers don't cover: shared address space,
// IETF protocol assignments, benchmarking, reserved, NAT64 and documentation.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// publicAddr reports whether an address is safe to fetch from: not loopback,
// private, link-local, multicast, unspecified or any reserved range.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// guardedDial resolves the host and dials the first public address, refusing
// the connection when any resolved address is not public.
func guardedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%s has no addresses", host)
	}
	for _, ip := range ips {
		if !publicAddr(ip) {
			return nil, fmt.Errorf("%s resolves to a non-public address", host)
		}
	}
	d := net.Dialer{Timeout: metadataTimeout}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].Unmap().String(), port))
}

func newMetadataClient() *http.Client {
	return &http.Client{
		Timeout: metadataTimeout,
		Transport: &http.Transport{
			DialContext:            guardedDial,
			TLSHandshakeTimeout:    metadataTimeout,
			ResponseHeaderTimeout:  metadataTimeout,
			MaxResponseHeaderBytes: 16 << 10,
			DisableKeepAlives:      true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type metadataDoc struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientSecret            *string  `json:"client_secret"`
}

// fetchClientMetadata fetches and validates a metadata document, returning
// it with how long to cache it (Cache-Control max-age, clamped 5 min–24 h).
func (s *Service) fetchClientMetadata(ctx context.Context, clientID string) (*clientMetadata, time.Duration, error) {
	if _, ok := validMetadataURL(clientID); !ok {
		return nil, 0, errors.New("client_id is not a valid metadata document URL")
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.metadataHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("metadata document answered %d", res.StatusCode)
	}
	if mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type")); mt != "application/json" && !strings.HasSuffix(mt, "+json") {
		return nil, 0, fmt.Errorf("metadata document is %q, not JSON", mt)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, metadataMaxBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > metadataMaxBytes {
		return nil, 0, errors.New("metadata document is over 5 KB")
	}
	doc, err := parseMetadataDoc(clientID, body)
	if err != nil {
		return nil, 0, err
	}
	return doc, cacheTTL(res.Header.Get("Cache-Control")), nil
}

// parseMetadataDoc validates a document against the client_id it was
// fetched from.
func parseMetadataDoc(clientID string, body []byte) (*clientMetadata, error) {
	var d metadataDoc
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, errors.New("metadata document is not a JSON object")
	}
	if d.ClientID != clientID {
		return nil, errors.New("metadata document's client_id doesn't match its URL")
	}
	if d.ClientSecret != nil {
		return nil, errors.New("metadata documents can't carry a client secret")
	}
	if m := d.TokenEndpointAuthMethod; m != "" && m != "none" {
		return nil, fmt.Errorf("token_endpoint_auth_method %q is not supported", m)
	}
	if len(d.RedirectURIs) == 0 || len(d.RedirectURIs) > maxRedirectURIs {
		return nil, errors.New("metadata document needs 1 to 10 redirect_uris")
	}
	for _, u := range d.RedirectURIs {
		if _, ok := redirectKind(u); !ok {
			return nil, fmt.Errorf("redirect URI %q is not allowed", u)
		}
	}
	if len(d.GrantTypes) > 0 && !slices.Contains(d.GrantTypes, "authorization_code") {
		return nil, errors.New("metadata document doesn't allow the authorization_code grant")
	}
	if len(d.ResponseTypes) > 0 && !slices.Contains(d.ResponseTypes, "code") {
		return nil, errors.New("metadata document doesn't allow the code response type")
	}
	return &clientMetadata{name: cleanName(d.ClientName), redirectURIs: d.RedirectURIs}, nil
}

// cacheTTL reads max-age from Cache-Control; no-store and no-cache mean the
// minimum.
func cacheTTL(header string) time.Duration {
	ttl := metadataDefault
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), "=")
		switch k {
		case "no-store", "no-cache":
			return metadataMinTTL
		case "max-age":
			if n, err := strconv.Atoi(strings.Trim(v, `"`)); err == nil {
				ttl = time.Duration(n) * time.Second
			}
		}
	}
	return min(max(ttl, metadataMinTTL), metadataMaxTTL)
}
