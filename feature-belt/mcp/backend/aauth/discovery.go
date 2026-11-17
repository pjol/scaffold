package aauth

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"scaffoldapp/backend/internal/safehttp"
)

// Discovery limits. Each is a default for a DiscoveryConfig field.
const (
	// minJWKSFetchInterval is the floor: "MUST NOT fetch a given issuer's JWKS more
	// frequently than once per minute". Configuration can raise it, never lower it.
	minJWKSFetchInterval = time.Minute
	// maxJWKSCacheAge: "SHOULD be discarded after a maximum of 24 hours regardless of
	// cache headers".
	maxJWKSCacheAge      = 24 * time.Hour
	defaultJWKSTTL       = 15 * time.Minute
	defaultMaxBackoff    = time.Hour
	defaultDiscoveryBody = 256 << 10
	defaultDiscoveryTime = 5 * time.Second
	defaultMaxIssuers    = 4096
)

// DiscoveryError is a failure to resolve an issuer's metadata or keys. Code is the
// Signature-Error code the protocol assigns: issuer_missing, issuer_mismatch, or
// unknown_key. Blocked marks a refusal by egress admission rather than a remote fault.
type DiscoveryError struct {
	Code    SignatureErrorCode
	Issuer  string
	Blocked bool
	// Status is the HTTP status a fetch answered with, when it got that far. A 404 on a metadata URL says the document isn't published, and an outage doesn't.
	Status int
	Cause  error
}

func (e *DiscoveryError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: issuer %s: %v", e.Code, e.Issuer, e.Cause)
	}
	return fmt.Sprintf("%s: issuer %s", e.Code, e.Issuer)
}

// Unwrap returns the underlying cause.
func (e *DiscoveryError) Unwrap() error { return e.Cause }

// MetadataDocument is a fetched metadata document that has passed the issuer checks.
// Raw is the document as served; use Decode for a typed view.
type MetadataDocument struct {
	Issuer  string
	JWKSURI string
	Raw     []byte
}

// Decode unmarshals the document, ignoring members the target does not model.
func (d *MetadataDocument) Decode(v any) error { return json.Unmarshal(d.Raw, v) }

// DiscoveryConfig configures a Discovery.
type DiscoveryConfig struct {
	// Client is the HTTP client. Nil builds one with NewEgressClient. A client passed
	// here keeps every check this package makes for itself, and has its redirects
	// refused, but its dial-time address check is its own affair. Tests use it.
	Client *http.Client
	// Clock is the cache's time. Nil uses time.Now.
	Clock func() time.Time
	// Policy is the identifier policy.
	Policy IdentifierPolicy
	// MinFetchInterval raises the once-per-minute floor. A smaller value is ignored.
	MinFetchInterval time.Duration
	// DefaultTTL is how long a response with no cache headers stays fresh.
	DefaultTTL time.Duration
	// MaxIssuers bounds the cache: "Cache entries are created by unauthenticated
	// callers, so an unbounded cache is a resource exhaustion attack".
	MaxIssuers int
	// Timeout bounds each fetch.
	Timeout time.Duration
	// MaxBodyBytes bounds each document.
	MaxBodyBytes int64
	// AdmitCrossOriginJWKS decides whether a jwks_uri on another host is admitted:
	// "Treat cross-origin jwks_uri URLs as requiring explicit deployment admission". Nil
	// refuses them.
	AdmitCrossOriginJWKS func(issuer, jwksURI string) bool
}

// Discovery fetches issuer metadata and JWKS under egress admission, and caches them by
// the rules of protocol section "JWKS Discovery and Caching". It implements KeyResolver
// and is safe for concurrent use.
type Discovery struct {
	cfg     DiscoveryConfig
	client  *http.Client
	mu      sync.Mutex
	entries map[string]*list.Element
	lru     *list.List
	// jwks holds the latest fetch of each issuer's JWKS, by issuer and URL. Entries are per
	// metadata document, and two documents of one issuer can name one JWKS, as with a PS and an AS
	// on one host. This is what keeps that JWKS to the floor.
	jwks map[string]*jwksFetch
}

// jwksFetch is one fetch of a JWKS. Its result is read after done closes.
type jwksFetch struct {
	at   time.Time
	done chan struct{}
	body []byte
	ttl  time.Duration
	err  error
}

// NewDiscovery returns a Discovery with the configured limits.
func NewDiscovery(cfg DiscoveryConfig) *Discovery {
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.MinFetchInterval < minJWKSFetchInterval {
		cfg.MinFetchInterval = minJWKSFetchInterval
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = defaultJWKSTTL
	}
	if cfg.MaxIssuers <= 0 {
		cfg.MaxIssuers = defaultMaxIssuers
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultDiscoveryTime
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultDiscoveryBody
	}
	client := cfg.Client
	if client == nil {
		client = NewEgressClient(cfg.Policy, EgressOptions{Timeout: cfg.Timeout, MaxBody: cfg.MaxBodyBytes})
	}
	// Whatever client is used, redirects are refused: "Refuse or constrain redirects".
	// A metadata document must also come from the URL its issuer is checked against.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Discovery{cfg: cfg, client: &c, entries: map[string]*list.Element{}, lru: list.New(), jwks: map[string]*jwksFetch{}}
}

// EgressOptions shapes an egress-admitted client.
type EgressOptions struct {
	Timeout time.Duration // whole exchange, default 10s
	MaxBody int64         // response cap in bytes, default 2 MiB
}

// NewEgressClient returns the client for "Every outbound fetch to a URL someone else
// named": HTTPS only, size and time limits, no redirects, and private, loopback and
// link-local addresses refused at dial time on the resolved address (internal/safehttp).
// Under AllowInsecureLoopback it also reaches http and private addresses, for
// development only.
func NewEgressClient(policy IdentifierPolicy, o EgressOptions) *http.Client {
	return safehttp.Client(safehttp.Options{
		Timeout:      o.Timeout,
		MaxBody:      o.MaxBody,
		MaxRedirects: 0,
		AllowPrivate: policy.AllowInsecureLoopback,
		AllowHTTP:    policy.AllowInsecureLoopback,
		// These calls say Prefer: wait, and "a wait of 30 seconds is RECOMMENDED". A cap on headers below that turns a slow
		// review into an unavailable SS. Timeout still bounds the exchange.
		LongHeaders: true,
	})
}

// CheckEgressURL judges a URL before a fetch: the scheme, credentials, a literal
// blocked address and known local names. The dial-time check still runs on every
// connection made through NewEgressClient.
func CheckEgressURL(raw string, policy IdentifierPolicy) (*url.URL, error) {
	u, err := safehttp.CheckURL(raw, policy.AllowInsecureLoopback, policy.AllowInsecureLoopback)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" && !isLoopbackHostPort(u.Host) {
		return nil, safehttp.ErrInsecure
	}
	return u, nil
}

// MetadataURL returns {id}/.well-known/{dwk}.
func MetadataURL(id, dwk string) string { return id + "/.well-known/" + dwk }

// dwkRE keeps a dwk a single well-known name. It comes from a token or a header, and
// `../` in it would turn a metadata fetch into a fetch of anywhere on the host.
var dwkRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// entry is one (issuer, dwk) pair's cached state.
type entry struct {
	id, dwk string

	mu          sync.Mutex
	doc         *MetadataDocument // nil until a fetch succeeds
	jwks        []byte
	fetchedAt   time.Time // last success
	freshUntil  time.Time
	nextAttempt time.Time // the floor, and backoff after a failure
	failures    int
	lastErr     error
	inflight    chan struct{} // non-nil while a fetch runs
}

func (d *Discovery) entry(id, dwk string) *entry {
	key := id + "\n" + dwk
	d.mu.Lock()
	defer d.mu.Unlock()
	if el, ok := d.entries[key]; ok {
		d.lru.MoveToFront(el)
		return el.Value.(*entry)
	}
	e := &entry{id: id, dwk: dwk}
	d.entries[key] = d.lru.PushFront(e)
	for d.lru.Len() > d.cfg.MaxIssuers {
		old := d.lru.Back()
		d.lru.Remove(old)
		oe := old.Value.(*entry)
		delete(d.entries, oe.id+"\n"+oe.dwk)
	}
	return e
}

// refresh fetches if the floor allows it, and waits out a fetch already running. It
// reports whether this call or one it waited for attempted a fetch.
func (d *Discovery) refresh(ctx context.Context, e *entry) bool {
	e.mu.Lock()
	if wait := e.inflight; wait != nil {
		e.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
		}
		return true
	}
	now := d.cfg.Clock()
	if now.Before(e.nextAttempt) {
		e.mu.Unlock()
		return false
	}
	done := make(chan struct{})
	e.inflight = done
	e.nextAttempt = now.Add(d.cfg.MinFetchInterval)
	e.mu.Unlock()

	doc, jwks, ttl, err := d.fetch(ctx, e.id, e.dwk)

	e.mu.Lock()
	now = d.cfg.Clock()
	if err == nil {
		e.doc, e.jwks = doc, jwks
		e.fetchedAt = now
		e.freshUntil = now.Add(ttl)
		e.failures = 0
		e.lastErr = nil
		e.nextAttempt = now.Add(d.cfg.MinFetchInterval)
	} else {
		e.failures++
		e.lastErr = err
		var de *DiscoveryError
		if errors.As(err, &de) && (de.Code == SigIssuerMissing || de.Code == SigIssuerMismatch) {
			// The document no longer binds to this identity. That is a verdict on the
			// issuer rather than a fetch failure, so a cached copy isn't served past it.
			e.doc, e.jwks = nil, nil
			e.freshUntil = time.Time{}
		}
		// "SHOULD retry with exponential backoff", never faster than the floor.
		backoff := d.cfg.MinFetchInterval
		for i := 1; i < e.failures && backoff < defaultMaxBackoff; i++ {
			backoff *= 2
		}
		if backoff > defaultMaxBackoff {
			backoff = defaultMaxBackoff
		}
		e.nextAttempt = now.Add(backoff)
	}
	e.inflight = nil
	e.mu.Unlock()
	close(done)
	return true
}

// lookup reads the selected key from the cached JWKS if the cache may serve it.
func (d *Discovery) lookup(e *entry, kid string) (key JWK, found, usable, fresh bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.jwks == nil {
		return JWK{}, false, false, false, nil
	}
	now := d.cfg.Clock()
	if now.Sub(e.fetchedAt) >= maxJWKSCacheAge {
		e.doc, e.jwks = nil, nil
		return JWK{}, false, false, false, nil
	}
	key, found, err = SelectJWK(e.jwks, kid)
	return key, found, true, now.Before(e.freshUntil), err
}

// Key implements KeyResolver. It serves a fresh cached key, refreshes on an unknown kid
// or an expired entry within the floor, and falls back to the stale copy when a fetch
// fails: "If a JWKS fetch fails, implementations SHOULD use the cached JWKS".
func (d *Discovery) Key(ctx context.Context, iss, dwk, kid string) (JWK, error) {
	if err := d.checkTarget(iss, dwk); err != nil {
		return JWK{}, err
	}
	e := d.entry(iss, dwk)
	key, found, _, fresh, err := d.lookup(e, kid)
	if err == nil && found && fresh {
		return key, nil
	}
	d.refresh(ctx, e)
	key, found, usable, _, err := d.lookup(e, kid)
	if err != nil {
		return JWK{}, selectError(iss, err)
	}
	if found {
		return key, nil
	}
	return JWK{}, d.notFound(e, iss, usable)
}

// selectError keeps the invalid_key answer for a selected member that can't be parsed.
func selectError(iss string, err error) error {
	var ke *KeyError
	if errors.As(err, &ke) {
		return &DiscoveryError{Code: ke.Code, Issuer: iss, Cause: err}
	}
	return &DiscoveryError{Code: SigUnknownKey, Issuer: iss, Cause: err}
}

func (d *Discovery) notFound(e *entry, iss string, usable bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !usable && e.lastErr != nil {
		var de *DiscoveryError
		if errors.As(e.lastErr, &de) {
			return de
		}
		return &DiscoveryError{Code: SigUnknownKey, Issuer: iss, Cause: e.lastErr}
	}
	return &DiscoveryError{Code: SigUnknownKey, Issuer: iss}
}

// RefreshKey implements KeyResolver: one refresh after a signature failed against a
// cached key, within the floor.
func (d *Discovery) RefreshKey(ctx context.Context, iss, dwk, kid string) (JWK, bool, error) {
	if err := d.checkTarget(iss, dwk); err != nil {
		return JWK{}, false, err
	}
	e := d.entry(iss, dwk)
	if !d.refresh(ctx, e) {
		return JWK{}, false, nil
	}
	e.mu.Lock()
	failed := e.lastErr != nil
	lastErr := e.lastErr
	e.mu.Unlock()
	if failed {
		var de *DiscoveryError
		if errors.As(lastErr, &de) && (de.Code == SigIssuerMissing || de.Code == SigIssuerMismatch) {
			return JWK{}, false, de
		}
		// The fetch failed for a transport reason: nothing new was learned, so the caller
		// keeps the verification failure it already has.
		return JWK{}, false, nil
	}
	key, found, _, _, err := d.lookup(e, kid)
	if err != nil {
		return JWK{}, false, selectError(iss, err)
	}
	if !found {
		return JWK{}, true, &DiscoveryError{Code: SigUnknownKey, Issuer: iss}
	}
	return key, true, nil
}

// Metadata returns the issuer's metadata document, from the cache when fresh, within
// the same floor as its JWKS. Use it for documents with no jwks_uri as well.
func (d *Discovery) Metadata(ctx context.Context, id, dwk string) (*MetadataDocument, error) {
	if err := d.checkTarget(id, dwk); err != nil {
		return nil, err
	}
	e := d.entry(id, dwk)
	serve := func() (*MetadataDocument, bool) {
		e.mu.Lock()
		defer e.mu.Unlock()
		now := d.cfg.Clock()
		if e.doc == nil || now.Sub(e.fetchedAt) >= maxJWKSCacheAge {
			return nil, false
		}
		return e.doc, now.Before(e.freshUntil)
	}
	if doc, fresh := serve(); doc != nil && fresh {
		return doc, nil
	}
	d.refresh(ctx, e)
	if doc, _ := serve(); doc != nil {
		return doc, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var de *DiscoveryError
	if errors.As(e.lastErr, &de) {
		return nil, de
	}
	return nil, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Cause: e.lastErr}
}

func (d *Discovery) checkTarget(id, dwk string) error {
	if err := d.cfg.Policy.ValidateServerIdentifier(id); err != nil {
		return &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: err}
	}
	if !dwkRE.MatchString(dwk) {
		return &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: fmt.Errorf("dwk %q is not a well-known name", dwk)}
	}
	return nil
}

// fetch makes one metadata and JWKS round. A document with no jwks_uri has only its
// metadata cached.
func (d *Discovery) fetch(ctx context.Context, id, dwk string) (*MetadataDocument, []byte, time.Duration, error) {
	metaURL := MetadataURL(id, dwk)
	raw, ttl, err := d.get(ctx, id, metaURL)
	if err != nil {
		return nil, nil, 0, err
	}
	var parsed struct {
		Issuer  *json.RawMessage `json:"issuer"`
		JWKSURI string           `json:"jwks_uri"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Cause: fmt.Errorf("metadata is not JSON: %w", err)}
	}
	// "A document with no issuer MUST be rejected with issuer_missing; one whose issuer
	// does not match MUST be rejected with issuer_mismatch", "compared by byte equality".
	if parsed.Issuer == nil || string(*parsed.Issuer) == "null" {
		return nil, nil, 0, &DiscoveryError{Code: SigIssuerMissing, Issuer: id}
	}
	var issuer string
	if err := json.Unmarshal(*parsed.Issuer, &issuer); err != nil || issuer != id {
		return nil, nil, 0, &DiscoveryError{Code: SigIssuerMismatch, Issuer: id}
	}
	doc := &MetadataDocument{Issuer: issuer, JWKSURI: parsed.JWKSURI, Raw: raw}
	if parsed.JWKSURI == "" {
		return doc, []byte(`{"keys":[]}`), ttl, nil
	}
	if err := d.cfg.Policy.ValidateHTTPSURL(parsed.JWKSURI); err != nil {
		return nil, nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: err}
	}
	u, err := url.Parse(parsed.JWKSURI)
	if err != nil {
		return nil, nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: err}
	}
	if u.Host != ServerHost(id) && (d.cfg.AdmitCrossOriginJWKS == nil || !d.cfg.AdmitCrossOriginJWKS(id, parsed.JWKSURI)) {
		return nil, nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: fmt.Errorf("cross-origin jwks_uri %q is not admitted", parsed.JWKSURI)}
	}
	jwks, jwksTTL, err := d.getJWKS(ctx, id, parsed.JWKSURI)
	if err != nil {
		return nil, nil, 0, err
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(jwks, &set); err != nil || set.Keys == nil {
		return nil, nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Cause: errors.New("JWKS is malformed")}
	}
	return doc, jwks, min(ttl, jwksTTL), nil
}

// getJWKS applies "MUST NOT fetch a given issuer's JWKS more frequently than once per minute" to the
// document itself: inside the floor it answers with the last fetch, or waits for one still running.
func (d *Discovery) getJWKS(ctx context.Context, id, target string) ([]byte, time.Duration, error) {
	key := id + "\n" + target
	now := d.cfg.Clock()
	d.mu.Lock()
	if f, ok := d.jwks[key]; ok && now.Sub(f.at) < d.cfg.MinFetchInterval {
		d.mu.Unlock()
		select {
		case <-f.done:
			return f.body, f.ttl, f.err
		case <-ctx.Done():
			return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Cause: ctx.Err()}
		}
	}
	f := &jwksFetch{at: now, done: make(chan struct{})}
	d.jwks[key] = f
	d.pruneJWKSFetches(now)
	d.mu.Unlock()

	f.body, f.ttl, f.err = d.get(ctx, id, target)
	close(f.done)
	return f.body, f.ttl, f.err
}

// pruneJWKSFetches bounds the record, since an unauthenticated caller can name issuers. A record past
// the floor has no further use, and at the cap the oldest goes. Callers hold d.mu.
func (d *Discovery) pruneJWKSFetches(now time.Time) {
	if len(d.jwks) <= d.cfg.MaxIssuers {
		return
	}
	oldestKey, oldest := "", now
	for k, f := range d.jwks {
		if now.Sub(f.at) >= d.cfg.MinFetchInterval {
			delete(d.jwks, k)
			continue
		}
		if f.at.Before(oldest) || oldestKey == "" {
			oldestKey, oldest = k, f.at
		}
	}
	if len(d.jwks) > d.cfg.MaxIssuers {
		delete(d.jwks, oldestKey)
	}
}

// get fetches one URL through the egress checks. It returns the body and how long it
// may be treated as fresh.
func (d *Discovery) get(ctx context.Context, id, target string) ([]byte, time.Duration, error) {
	if _, err := CheckEgressURL(target, d.cfg.Policy); err != nil {
		return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: err}
	}
	ctx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: true, Cause: err}
	}
	req.Header.Set("Accept", ContentTypeJSON)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Blocked: safehttp.IsBlocked(err), Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Status: resp.StatusCode, Cause: fmt.Errorf("GET %s answered %d", target, resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, d.cfg.MaxBodyBytes+1))
	if err != nil {
		return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Cause: err}
	}
	if int64(len(body)) > d.cfg.MaxBodyBytes {
		return nil, 0, &DiscoveryError{Code: SigUnknownKey, Issuer: id, Cause: safehttp.ErrTooLarge}
	}
	return body, d.freshness(resp), nil
}

// freshness applies "SHOULD respect HTTP cache headers", clamped between the floor and
// the 24-hour maximum. no-store and no-cache still cache for the floor, because
// "Implementations MUST cache JWKS responses" and may not fetch faster than that anyway.
func (d *Discovery) freshness(resp *http.Response) time.Duration {
	ttl := d.cfg.DefaultTTL
	explicit := false
	for _, part := range strings.Split(resp.Header.Get("Cache-Control"), ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), "=")
		switch name {
		case "no-store", "no-cache":
			ttl, explicit = 0, true
		case "max-age":
			if n, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64); err == nil && n >= 0 && !explicit {
				ttl, explicit = time.Duration(n)*time.Second, true
			}
		}
	}
	if !explicit {
		if exp, err := http.ParseTime(resp.Header.Get("Expires")); err == nil {
			ttl = exp.Sub(d.cfg.Clock())
		}
	}
	if age, err := strconv.ParseInt(resp.Header.Get("Age"), 10, 64); err == nil && age > 0 {
		ttl -= time.Duration(age) * time.Second
	}
	if ttl < d.cfg.MinFetchInterval {
		ttl = d.cfg.MinFetchInterval
	}
	if ttl > maxJWKSCacheAge {
		ttl = maxJWKSCacheAge
	}
	return ttl
}
