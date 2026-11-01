package calendar

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Subscribed calendar URLs are user input fetched by the server, so every
// fetch goes through a client that can only reach the public internet:
// the address is checked after DNS resolution, at dial time, for every
// connection (redirects included), which also defeats DNS rebinding.
// Development may allow private addresses (CALENDAR_ALLOW_PRIVATE_URLS) to
// test against a local file server.

// errBlockedHost is returned when a URL resolves somewhere private.
var errBlockedHost = errors.New("address is on a private network")

var blockedNets = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
		"224.0.0.0/4", "240.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "64:ff9b::/96", "2001:db8::/32",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// blockedIP reports private, loopback, link-local (cloud metadata),
// multicast and reserved addresses. IPv4-mapped IPv6 is normalised first.
func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func newSafeClient(timeout time.Duration, allowPrivate func() bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var firstErr error
		for _, ip := range ips {
			if !allowPrivate() && blockedIP(ip.IP) {
				if firstErr == nil {
					firstErr = fmt.Errorf("%w: %s", errBlockedHost, ip.IP)
				}
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("%w: %s", errBlockedHost, host)
		}
		return nil, firstErr
	}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if _, err := checkFeedURL(req.URL.String(), allowPrivate()); err != nil {
				return err
			}
			return nil
		},
		Transport: &http.Transport{
			Proxy:                 nil, // a proxy would dial for us and skip the check
			DialContext:           dial,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: timeout,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// checkFeedURL normalises a calendar address (webcal:// → https://) and
// rejects anything not shaped like a public http(s) URL. The dial-time
// check is the real guarantee; this gives a clear answer on paste.
func checkFeedURL(raw string, allowPrivate bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return nil, errors.New("address too long")
	}
	if len(raw) >= 9 && strings.EqualFold(raw[:9], "webcal://") {
		raw = "https://" + raw[9:]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("not a URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("must be http or https")
	}
	if u.User != nil {
		return nil, errors.New("credentials in the address aren't accepted")
	}
	host := strings.ToLower(u.Hostname())
	if allowPrivate {
		return u, nil
	}
	if ip := net.ParseIP(host); ip != nil && blockedIP(ip) {
		return nil, errBlockedHost
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".localdomain") || !strings.Contains(host, ".") {
		return nil, errBlockedHost
	}
	return u, nil
}

// ---- secrets at rest ----------------------------------------------------------

var errNoKey = errors.New("calendar: CALENDAR_SECRET_KEY is not set")

// seal encrypts with AES-256-GCM: nonce || ciphertext.
func (s *Service) seal(plain string) ([]byte, error) {
	if s.secretKey == nil {
		return nil, errNoKey
	}
	block, err := aes.NewCipher(s.secretKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), []byte("calendar-subscription")), nil
}

func (s *Service) open(sealed []byte) (string, error) {
	if s.secretKey == nil {
		return "", errNoKey
	}
	block, err := aes.NewCipher(s.secretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", errors.New("calendar: sealed value too short")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte("calendar-subscription"))
	return string(plain), err
}
