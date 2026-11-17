package aauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// testEpoch is a fixed instant for tests; every test pins the clock to it.
const testEpoch int64 = 1_790_600_000

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock { return &testClock{t: time.Unix(testEpoch, 0)} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeResponse is one canned answer.
type fakeResponse struct {
	status int
	body   []byte
	header http.Header
	err    error
}

// fakeNet is an HTTP transport serving canned documents and counting hits. It lets
// Discovery run its real cache and egress logic with no network.
type fakeNet struct {
	mu     sync.Mutex
	routes map[string]fakeResponse
	hits   map[string]int
}

func newFakeNet() *fakeNet {
	return &fakeNet{routes: map[string]fakeResponse{}, hits: map[string]int{}}
}

func (f *fakeNet) set(url string, r fakeResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[url] = r
}

func (f *fakeNet) count(url string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[url]
}

func (f *fakeNet) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	url := r.URL.String()
	f.hits[url]++
	route, ok := f.routes[url]
	if !ok {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}, Request: r}, nil
	}
	if route.err != nil {
		return nil, route.err
	}
	status := route.status
	if status == 0 {
		status = 200
	}
	h := route.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(route.body)), Header: h, Request: r}, nil
}

// world is a set of in-memory issuers behind a real Discovery.
type world struct {
	t     *testing.T
	clock *testClock
	net   *fakeNet
	disc  *Discovery
}

func newWorld(t *testing.T) *world {
	t.Helper()
	clock := newTestClock()
	net := newFakeNet()
	return &world{
		t: t, clock: clock, net: net,
		disc: NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: net}, Clock: clock.Now}),
	}
}

func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// publish serves metadata and a JWKS for issuer under dwk.
func (w *world) publish(issuer, dwk string, keys ...JWK) {
	w.t.Helper()
	jwksURI := issuer + "/.well-known/jwks.json"
	w.net.set(MetadataURL(issuer, dwk), fakeResponse{body: jsonBytes(w.t, map[string]string{"issuer": issuer, "jwks_uri": jwksURI})})
	w.net.set(jwksURI, fakeResponse{body: jsonBytes(w.t, JWKS{Keys: keys})})
}

func mustKey(t *testing.T, alg Alg, kid string) *PrivateKey {
	t.Helper()
	k, err := GenerateKey(alg, kid)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

const (
	testAP       = "https://agent.example"
	testPS       = "https://ps.example"
	testAS       = "https://as.example"
	testResource = "https://resource.example"
	testSS       = "https://ss.example"
	testAgentSub = "aauth:assistant@agent.example"
)

// parties holds the issuers' keys, published on a world.
type parties struct {
	*world
	apKey, psKey, asKey, resourceKey, ssKey *PrivateKey
	agentKey                                *PrivateKey
}

func newParties(t *testing.T) *parties {
	t.Helper()
	w := newWorld(t)
	p := &parties{
		world:       w,
		apKey:       mustKey(t, AlgEd25519, "ap-1"),
		psKey:       mustKey(t, AlgEd25519, "ps-1"),
		asKey:       mustKey(t, AlgES256, "as-1"),
		resourceKey: mustKey(t, AlgEd25519, "res-1"),
		ssKey:       mustKey(t, AlgEd25519, "ss-1"),
		agentKey:    mustKey(t, AlgEd25519, "agent-eph"),
	}
	w.publish(testAP, DwkAgent, p.apKey.PublicJWK())
	w.publish(testPS, DwkPerson, p.psKey.PublicJWK())
	w.publish(testAS, DwkAccess, p.asKey.PublicJWK())
	w.publish(testResource, DwkResource, p.resourceKey.PublicJWK())
	w.publish(testSS, DwkSupervisor, p.ssKey.PublicJWK())
	return p
}

func (p *parties) tokens() *TokenVerifier {
	return &TokenVerifier{Keys: p.disc, Clock: p.clock.Now}
}

func (p *parties) nowClaims(iss string, life time.Duration) Claims {
	now := p.clock.Now().Unix()
	return Claims{Iss: iss, Jti: NewJTI(), Iat: now, Exp: now + int64(life/time.Second)}
}

func (p *parties) agentClaims() AgentClaims {
	c := p.nowClaims(testAP, time.Hour)
	c.Dwk = DwkAgent // set here too, for tests that sign claims directly
	return AgentClaims{
		Claims: c,
		Sub:    testAgentSub,
		Cnf:    ConfirmKey(p.agentKey.PublicJWK()),
		PS:     testPS,
	}
}

func (p *parties) agentToken(t *testing.T) string {
	t.Helper()
	tok, err := TokenIssuer{Key: p.apKey}.Agent(p.agentClaims())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (p *parties) personClaims() PersonClaims {
	c := p.nowClaims(testPS, time.Hour)
	c.Dwk = DwkPerson
	return PersonClaims{
		Claims: c,
		Aud:    testResource,
		Sub:    "8f14e45fceea167a5a36dedd4bea2543",
		Cnf:    ConfirmKey(p.agentKey.PublicJWK()),
	}
}

func (p *parties) personToken(t *testing.T, mutate ...func(*PersonClaims)) (string, PersonClaims) {
	t.Helper()
	c := p.personClaims()
	for _, m := range mutate {
		m(&c)
	}
	tok, err := TokenIssuer{Key: p.psKey}.Person(c)
	if err != nil {
		t.Fatal(err)
	}
	return tok, c
}

func (p *parties) resourceClaims(person PersonClaims) ResourceClaims {
	tp, _ := p.agentKey.PublicJWK().Thumbprint()
	return ResourceClaims{
		Claims:       p.nowClaims(testResource, 5*time.Minute),
		Aud:          testPS,
		PS:           person.Iss,
		Sub:          person.Sub,
		PresentedJti: person.Jti,
		AgentJKT:     tp,
		Scope:        "data.read",
		MissionS256:  person.MissionS256,
		Tenant:       person.Tenant,
	}
}

func (p *parties) resourceToken(t *testing.T, c ResourceClaims) string {
	t.Helper()
	tok, err := TokenIssuer{Key: p.resourceKey}.Resource(c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (p *parties) authClaims() AuthClaims {
	c := Claims{}
	now := p.clock.Now().Unix()
	c.Iss, c.Dwk, c.Jti, c.Iat, c.Exp = testPS, DwkPerson, NewJTI(), now, now+3600
	return AuthClaims{
		Claims: c,
		Aud:    testResource,
		PS:     testPS,
		Sub:    "8f14e45fceea167a5a36dedd4bea2543",
		Cnf:    ConfirmKey(p.agentKey.PublicJWK()),
		Scope:  "data.read",
	}
}

func (p *parties) authToken(t *testing.T, c AuthClaims) string {
	t.Helper()
	key := p.psKey
	if c.Dwk == DwkAccess {
		key = p.asKey
	}
	tok, err := TokenIssuer{Key: key}.Auth(c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// signedAgentRequest builds a server-side request signed with the agent's key.
func (p *parties) signedAgentRequest(t *testing.T, token, target string, body []byte) *http.Request {
	t.Helper()
	return signedRequest(t, NewAgentSigner(token, p.agentKey, p.clock.Now), target, body)
}

// signedRequest signs a client request, then rebuilds it as a server would see it.
func signedRequest(t *testing.T, signer *RequestSigner, target string, body []byte) *http.Request {
	t.Helper()
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	out, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		out.Header.Set("Content-Type", ContentTypeJSON)
	}
	if err := signer.SignRequest(out, body); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	return toServer(out, body)
}

func toServer(out *http.Request, body []byte) *http.Request {
	in := httptest.NewRequest(out.Method, out.URL.RequestURI(), bytes.NewReader(body))
	in.Host = out.URL.Host
	in.Header = out.Header.Clone()
	return in
}

func wantFailure(t *testing.T, err error, code SignatureErrorCode) *SignatureFailure {
	t.Helper()
	var f *SignatureFailure
	if !errors.As(err, &f) {
		t.Fatalf("want failure %s, got %v", code, err)
	}
	if f.Code != code {
		t.Fatalf("want failure %s, got %s (%v)", code, f.Code, f)
	}
	return f
}

func wantTokenSig(t *testing.T, err error, code SignatureErrorCode) *TokenError {
	t.Helper()
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("want token error %s, got %v", code, err)
	}
	if te.Sig != code {
		t.Fatalf("want token error %s, got %s (%v)", code, te.Sig, te)
	}
	return te
}

func wantTokenCode(t *testing.T, err error, code ErrorCode) *TokenError {
	t.Helper()
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("want token error %s, got %v", code, err)
	}
	if te.Code != code {
		t.Fatalf("want token error %s, got %s (%v)", code, te.Code, te)
	}
	return te
}

// signRawJWT signs arbitrary header and payload values, for the shapes TokenIssuer refuses to
// produce.
func signRawJWT(t *testing.T, key Signer, header, payload any) string {
	t.Helper()
	input := Base64URL(jsonBytes(t, header)) + "." + Base64URL(jsonBytes(t, payload))
	sig, err := key.Sign([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + Base64URL(sig)
}

func cnfMap(k JWK) map[string]any { return map[string]any{"jwk": k} }
