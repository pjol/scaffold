package aauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Cross-implementation proof, static so the suite needs no Node. testdata/README.md says where
// each file came from and how to regenerate it.
//
//   - sdk-signed.json holds requests signed by @aauth/agent and @hellocoop/httpsig. The Go verifier
//     must accept them, or answer the failure a case names where the SDK departs from the spec.
//   - go-signed.json holds what the Go signer produces, and js-verdicts.json holds what
//     @hellocoop/httpsig and jose said about exactly those bytes. Tests below check that the signer
//     still produces them, so the verdicts still apply.

const (
	sdkSignedPath  = "testdata/sdk-signed.json"
	goSignedPath   = "testdata/go-signed.json"
	jsVerdictsPath = "testdata/js-verdicts.json"
)

type interopIssuer struct {
	Issuer string `json:"issuer"`
	Dwk    string `json:"dwk"`
	JWKS   JWKS   `json:"jwks"`
}

func interopDiscovery(clock func() time.Time, issuers []interopIssuer, t *testing.T) *Discovery {
	t.Helper()
	net := newFakeNet()
	for _, is := range issuers {
		jwksURI := is.Issuer + "/.well-known/jwks.json"
		net.set(MetadataURL(is.Issuer, is.Dwk), fakeResponse{body: jsonBytes(t, map[string]string{"issuer": is.Issuer, "jwks_uri": jwksURI})})
		net.set(jwksURI, fakeResponse{body: jsonBytes(t, is.JWKS)})
	}
	return NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: net}, Clock: clock})
}

// sdkCase is one request signed by the SDK.
type sdkCase struct {
	Name    string      `json:"name"`
	How     string      `json:"how"`
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers [][2]string `json:"headers"`
	BodyB64 string      `json:"body_b64"`
	// Policy selects the verifier: agent, resource or server.
	Policy string `json:"policy"`
	// Origin anchors @authority. Empty means the request's Host, for the loopback captures.
	Origin string `json:"origin"`
	Expect struct {
		Alg     string `json:"alg"`
		Typ     string `json:"typ"`
		Sub     string `json:"sub"`
		Server  string `json:"server"`
		Failure string `json:"failure"`
	} `json:"expect"`
	Note string `json:"note"`
}

type sdkFile struct {
	Provenance map[string]any  `json:"provenance"`
	Now        int64           `json:"now"`
	Issuers    []interopIssuer `json:"issuers"`
	Cases      []sdkCase       `json:"cases"`
	Headers    struct {
		Requirement []struct {
			Input Requirement `json:"input"`
			Field string      `json:"field"`
		} `json:"requirement"`
		Capabilities struct {
			Input []string `json:"input"`
			Field string   `json:"field"`
		} `json:"capabilities"`
		SignatureError []struct {
			Code          string   `json:"code"`
			RequiredInput []string `json:"required_input"`
			Field         string   `json:"field"`
		} `json:"signature_error"`
		AcceptScheme struct {
			Input []string `json:"input"`
			Field string   `json:"field"`
		} `json:"accept_scheme"`
		AcceptAlg struct {
			Input []string `json:"input"`
			Field string   `json:"field"`
		} `json:"accept_alg"`
	} `json:"headers"`
	Thumbprints []struct {
		JWK        JWK    `json:"jwk"`
		Thumbprint string `json:"thumbprint"`
	} `json:"thumbprints"`
	Digests []struct {
		BodyB64 string `json:"body_b64"`
		Field   string `json:"field"`
	} `json:"digests"`
}

func readJSON(t *testing.T, path string, v any) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s is missing; see testdata/README.md: %v", path, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return raw
}

func b64Decode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serverRequest rebuilds a captured request as net/http's server would present it.
func serverRequest(method, rawURL string, headers [][2]string, body []byte) *http.Request {
	target := rawURL
	if i := strings.Index(rawURL, "://"); i >= 0 {
		if j := strings.Index(rawURL[i+3:], "/"); j >= 0 {
			target = rawURL[i+3+j:]
		} else {
			target = "/"
		}
	}
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	if i := strings.Index(rawURL, "://"); i >= 0 {
		host := rawURL[i+3:]
		if j := strings.Index(host, "/"); j >= 0 {
			host = host[:j]
		}
		r.Host = host
	}
	r.Header = http.Header{}
	for _, h := range headers {
		if strings.EqualFold(h[0], "host") {
			r.Host = h[1]
			continue
		}
		r.Header.Add(h[0], h[1])
	}
	return r
}

func sdkVerifier(t *testing.T, c sdkCase, disc KeyResolver, clock func() time.Time) *Verifier {
	t.Helper()
	cfg := VerifierConfig{Keys: disc, Clock: clock, Origin: c.Origin}
	switch c.Policy {
	case "agent":
		cfg.Scheme = AgentPolicy()
	case "resource":
		cfg.Scheme = ResourcePolicy(TypAgent, TypPerson, TypAuth)
		cfg.Audience = testResource
		cfg.AllowUncoveredBody = true
	case "server":
		cfg.Scheme = ServerPolicy(DwkPerson, DwkAgent)
	default:
		t.Fatalf("policy %q", c.Policy)
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Requests signed by the real SDK verify under the Go verifier.
func TestInteropSDKRequestsVerify(t *testing.T) {
	var file sdkFile
	readJSON(t, sdkSignedPath, &file)
	if len(file.Cases) == 0 {
		t.Fatal("no cases")
	}
	clock := func() time.Time { return time.Unix(file.Now, 0) }
	algs := map[string]bool{}
	bodies := 0
	for _, c := range file.Cases {
		t.Run(c.Name, func(t *testing.T) {
			disc := interopDiscovery(clock, file.Issuers, t)
			body := b64Decode(t, c.BodyB64)
			r := serverRequest(c.Method, c.URL, c.Headers, body)
			res, err := sdkVerifier(t, c, disc, clock).Verify(ctx, r)
			if c.Expect.Failure != "" {
				wantFailure(t, err, SignatureErrorCode(c.Expect.Failure))
				return
			}
			if err != nil {
				t.Fatalf("%s\nverify: %v", c.How, err)
			}
			if string(res.Alg) != c.Expect.Alg {
				t.Errorf("alg %s", res.Alg)
			}
			algs[string(res.Alg)] = true
			if len(body) > 0 {
				bodies++
			}
			switch c.Expect.Typ {
			case TypAgent:
				if res.Agent == nil || res.Agent.Claims.Sub != c.Expect.Sub {
					t.Errorf("agent %+v", res.Agent)
				}
			case TypPerson:
				if res.Person == nil || res.Person.Claims.Sub != c.Expect.Sub {
					t.Errorf("person %+v", res.Person)
				}
			}
			if c.Expect.Server != "" && (res.Server == nil || res.Server.ID != c.Expect.Server) {
				t.Errorf("server %+v", res.Server)
			}
			if !bytes.Equal(res.Body, body) {
				t.Errorf("body %s", res.Body)
			}
		})
	}
	if !algs["Ed25519"] || !algs["ES256"] {
		t.Errorf("both algorithms must be proven: %v", algs)
	}
	if bodies == 0 {
		t.Error("no case carried a body")
	}
}

// The clock really is what the fixtures rest on, and tampering a real SDK request is caught.
func TestInteropSDKNegatives(t *testing.T) {
	var file sdkFile
	readJSON(t, sdkSignedPath, &file)
	var post *sdkCase
	for i := range file.Cases {
		c := &file.Cases[i]
		if c.Expect.Failure == "" && c.Policy == "agent" && c.BodyB64 != "" && c.Origin != "" {
			post = c
			break
		}
	}
	if post == nil {
		t.Fatal("no signed POST among the cases")
	}
	run := func(now int64, mutate func(*http.Request)) error {
		clock := func() time.Time { return time.Unix(now, 0) }
		disc := interopDiscovery(clock, file.Issuers, t)
		r := serverRequest(post.Method, post.URL, post.Headers, b64Decode(t, post.BodyB64))
		if mutate != nil {
			mutate(r)
		}
		_, err := sdkVerifier(t, *post, disc, clock).Verify(ctx, r)
		return err
	}
	if err := run(file.Now, nil); err != nil {
		t.Fatal(err)
	}
	wantFailure(t, run(file.Now+120, nil), SigInvalidSignature)
	wantFailure(t, run(file.Now-120, nil), SigClockSkew)
	wantFailure(t, run(file.Now, func(r *http.Request) { r.Method = "PUT" }), SigInvalidSignature)
	wantFailure(t, run(file.Now, func(r *http.Request) {
		r.Body = bodyOf(`{"resource":"https://evil.example"}`)
	}), SigInvalidSignature)
	wantFailure(t, run(file.Now, func(r *http.Request) { r.Header.Del(HeaderSignatureKey) }), SigInvalidSignature)
	wantFailure(t, run(file.Now, func(r *http.Request) { r.RequestURI = "/token" }), SigInvalidSignature)
}

// The one departure the SDK's signer has from RFC 9421, pinned to its exact cause: it signs "@query"
// without the leading question mark. Section 2.2.7: "including the leading ? character".
func TestInteropSDKQueryDifference(t *testing.T) {
	var file sdkFile
	readJSON(t, sdkSignedPath, &file)
	var c *sdkCase
	for i := range file.Cases {
		if file.Cases[i].Name == "difference-query-component" {
			c = &file.Cases[i]
		}
	}
	if c == nil {
		t.Fatal("case missing")
	}
	r := serverRequest(c.Method, c.URL, c.Headers, nil)
	msg := NewRequestMessage(r)
	inputs, err := ParseSignatureInput(r.Header.Values(HeaderSignatureInput))
	if err != nil {
		t.Fatal(err)
	}
	base, err := SignatureBase(&msg, nil, inputs[0].Params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(base, `"@query": ?x=1`) {
		t.Fatalf("our base:\n%s", base)
	}
	sigs, err := ParseSignatures(r.Header.Values(HeaderSignature))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParseSignatureKey(r.Header.Values(HeaderSignatureKey))
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := ParseJWT(keys.Members[0].JWT)
	if err != nil {
		t.Fatal(err)
	}
	var claims AgentClaims
	if err := json.Unmarshal(jwt.Payload, &claims); err != nil {
		t.Fatal(err)
	}
	key := claims.Cnf.JWK
	if err := key.Verify([]byte(base), sigs["sig"]); err == nil {
		t.Fatal("the SDK now signs the RFC's value; drop this case and the note in testdata/README.md")
	}
	if err := key.Verify([]byte(strings.Replace(base, `"@query": ?x=1`, `"@query": x=1`, 1)), sigs["sig"]); err != nil {
		t.Fatalf("the difference is not just the question mark: %v", err)
	}
}

func bodyOf(s string) *readCloser { return &readCloser{strings.NewReader(s)} }

type readCloser struct{ *strings.Reader }

func (*readCloser) Close() error { return nil }

// Headers built by the SDK parse in Go, and Go serializes the same bytes.
func TestInteropSDKHeadersAndPrimitives(t *testing.T) {
	var file sdkFile
	readJSON(t, sdkSignedPath, &file)
	if len(file.Headers.Requirement) == 0 {
		t.Fatal("no requirement fixtures")
	}
	for _, c := range file.Headers.Requirement {
		got, err := ParseRequirement([]string{c.Field})
		if err != nil {
			t.Errorf("%s: %v", c.Field, err)
			continue
		}
		if got != c.Input {
			t.Errorf("%s: %+v, want %+v", c.Field, got, c.Input)
		}
		field, err := c.Input.Field()
		if err != nil {
			t.Errorf("%+v: %v", c.Input, err)
			continue
		}
		if field != c.Field {
			t.Errorf("Go serialized %s, SDK %s", field, c.Field)
		}
	}
	if got := ParseCapabilities([]string{file.Headers.Capabilities.Field}); !reflect.DeepEqual(got, file.Headers.Capabilities.Input) {
		t.Errorf("capabilities %v", got)
	}
	if field, _ := CapabilitiesField(file.Headers.Capabilities.Input); field != file.Headers.Capabilities.Field {
		t.Errorf("capabilities %s", field)
	}
	for _, c := range file.Headers.SignatureError {
		f := &SignatureFailure{Code: SignatureErrorCode(c.Code), RequiredInput: c.RequiredInput}
		if got := f.SignatureErrorField(); got != c.Field {
			t.Errorf("Go %s, SDK %s", got, c.Field)
		}
	}
	schemes := make([]Scheme, len(file.Headers.AcceptScheme.Input))
	for i, s := range file.Headers.AcceptScheme.Input {
		schemes[i] = Scheme(s)
	}
	if got := schemesField(schemes); got != file.Headers.AcceptScheme.Field {
		t.Errorf("Go %s, SDK %s", got, file.Headers.AcceptScheme.Field)
	}
	algs := make([]Alg, len(file.Headers.AcceptAlg.Input))
	for i, s := range file.Headers.AcceptAlg.Input {
		algs[i] = Alg(s)
	}
	if got := algsField(algs); got != file.Headers.AcceptAlg.Field {
		t.Errorf("Go %s, SDK %s", got, file.Headers.AcceptAlg.Field)
	}
	if len(file.Thumbprints) == 0 || len(file.Digests) == 0 {
		t.Fatal("no primitive fixtures")
	}
	for _, c := range file.Thumbprints {
		if got, err := c.JWK.Thumbprint(); err != nil || got != c.Thumbprint {
			t.Errorf("thumbprint %s %v, SDK %s", got, err, c.Thumbprint)
		}
	}
	for _, c := range file.Digests {
		body := b64Decode(t, c.BodyB64)
		if got := ContentDigest(body); got != c.Field {
			t.Errorf("digest %s, SDK %s", got, c.Field)
		}
		if err := VerifyContentDigest([]string{c.Field}, body); err != nil {
			t.Error(err)
		}
	}
}

// goFile is what the Go side offers the SDK.
type goFile struct {
	Provenance map[string]any             `json:"provenance"`
	Now        int64                      `json:"now"`
	Keys       map[string]json.RawMessage `json:"keys"`
	Issuers    []interopIssuer            `json:"issuers"`
	Tokens     []goToken                  `json:"tokens"`
	Requests   []goRequest                `json:"requests"`
	Responses  []goResponse               `json:"responses"`
	Headers    map[string]string          `json:"headers"`
}

type goToken struct {
	Name      string `json:"name"`
	Typ       string `json:"typ"`
	Alg       string `json:"alg"`
	JWT       string `json:"jwt"`
	IssuerKey string `json:"issuer_key"`
}

type goRequest struct {
	Name      string      `json:"name"`
	Alg       string      `json:"alg"`
	Scheme    string      `json:"scheme"`
	Method    string      `json:"method"`
	URL       string      `json:"url"`
	Authority string      `json:"authority"`
	Path      string      `json:"path"`
	Query     string      `json:"query"`
	Headers   [][2]string `json:"headers"`
	BodyB64   string      `json:"body_b64"`
	Base      string      `json:"base"`
	SignerKey string      `json:"signer_key"`
}

type goResponse struct {
	Name      string      `json:"name"`
	Alg       string      `json:"alg"`
	Request   goRequest   `json:"request"`
	Status    int         `json:"status"`
	Headers   [][2]string `json:"headers"`
	BodyB64   string      `json:"body_b64"`
	Base      string      `json:"base"`
	SignerKey string      `json:"signer_key"`
	Issuer    string      `json:"issuer"`
	Dwk       string      `json:"dwk"`
}

func headerPairs(h http.Header) [][2]string {
	var names []string
	for k := range h {
		names = append(names, k)
	}
	sortStrings(names)
	var out [][2]string
	for _, k := range names {
		for _, v := range h[k] {
			out = append(out, [2]string{strings.ToLower(k), v})
		}
	}
	return out
}

// goKeySet is the throwaway keys the Go fixtures are made with.
type goKeySet map[string]*PrivateKey

func newGoKeySet(t *testing.T) goKeySet {
	return goKeySet{
		"ap_ed25519":    mustKey(t, AlgEd25519, "ap-ed"),
		"ap_es256":      mustKey(t, AlgES256, "ap-ec"),
		"ps_ed25519":    mustKey(t, AlgEd25519, "ps-ed"),
		"ps_es256":      mustKey(t, AlgES256, "ps-ec"),
		"as_es256":      mustKey(t, AlgES256, "as-ec"),
		"res_ed25519":   mustKey(t, AlgEd25519, "res-ed"),
		"ss_ed25519":    mustKey(t, AlgEd25519, "ss-ed"),
		"ss_es256":      mustKey(t, AlgES256, "ss-ec"),
		"agent_ed25519": mustKey(t, AlgEd25519, "agent-ed"),
		"agent_es256":   mustKey(t, AlgES256, "agent-ec"),
	}
}

func signRequestFixture(t *testing.T, name, scheme, signerKey string, signer *RequestSigner, method, url string, body []byte, extra map[string]string) (goRequest, *http.Request) {
	t.Helper()
	out, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		out.Header.Set("Content-Type", ContentTypeJSON)
	}
	for k, v := range extra {
		out.Header.Set(k, v)
	}
	if err := signer.SignRequest(out, body); err != nil {
		t.Fatal(err)
	}
	msg := NewRequestMessage(out)
	inputs, err := ParseSignatureInput(out.Header.Values(HeaderSignatureInput))
	if err != nil {
		t.Fatal(err)
	}
	base, err := SignatureBase(&msg, nil, inputs[0].Params)
	if err != nil {
		t.Fatal(err)
	}
	return goRequest{
		Name: name, Alg: signer.key.PublicJWK().Alg, Scheme: scheme, Method: method, URL: url,
		Authority: msg.Authority, Path: msg.Path, Query: msg.RawQuery,
		Headers: headerPairs(out.Header), BodyB64: base64.StdEncoding.EncodeToString(body),
		Base: base, SignerKey: signerKey,
	}, out
}

// buildGoFixtures makes every Go-signed fixture from the keys. Times and identifiers are fixed, so
// with the same keys the only differences are ES256 signatures, which are randomized.
func buildGoFixtures(t *testing.T, keys goKeySet) *goFile {
	t.Helper()
	const now = testEpoch
	clock := func() time.Time { return time.Unix(now, 0) }
	pub := func(name string) JWK { return keys[name].PublicJWK() }
	f := &goFile{Now: now, Keys: map[string]json.RawMessage{}, Headers: map[string]string{}}
	for name, k := range keys {
		raw, err := k.MarshalPrivateJWK()
		if err != nil {
			t.Fatal(err)
		}
		f.Keys[name] = raw
	}
	f.Issuers = []interopIssuer{
		{Issuer: testAP, Dwk: DwkAgent, JWKS: JWKS{Keys: []JWK{pub("ap_ed25519"), pub("ap_es256")}}},
		{Issuer: testPS, Dwk: DwkPerson, JWKS: JWKS{Keys: []JWK{pub("ps_ed25519"), pub("ps_es256")}}},
		{Issuer: testAS, Dwk: DwkAccess, JWKS: JWKS{Keys: []JWK{pub("as_es256")}}},
		{Issuer: testResource, Dwk: DwkResource, JWKS: JWKS{Keys: []JWK{pub("res_ed25519")}}},
		{Issuer: testSS, Dwk: DwkSupervisor, JWKS: JWKS{Keys: []JWK{pub("ss_ed25519"), pub("ss_es256")}}},
	}
	claims := func(iss, jti string) Claims { return Claims{Iss: iss, Jti: jti, Iat: now, Exp: now + 3000} }
	token := func(name, typ, issuerKey, raw string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		f.Tokens = append(f.Tokens, goToken{Name: name, Typ: typ, Alg: keys[issuerKey].PublicJWK().Alg, JWT: raw, IssuerKey: issuerKey})
		return raw
	}
	agentClaims := func(key, jti string) AgentClaims {
		return AgentClaims{Claims: claims(testAP, jti), Sub: testAgentSub, Cnf: ConfirmKey(pub(key)), PS: testPS}
	}
	raw, err := TokenIssuer{Key: keys["ap_ed25519"]}.Agent(agentClaims("agent_ed25519", "fixture-agent-ed"))
	agentEd := token("agent-token-ed25519", TypAgent, "ap_ed25519", raw, err)
	raw, err = TokenIssuer{Key: keys["ap_es256"]}.Agent(agentClaims("agent_es256", "fixture-agent-ec"))
	agentEC := token("agent-token-es256", TypAgent, "ap_es256", raw, err)
	personClaims := PersonClaims{Claims: claims(testPS, "fixture-person"), Aud: testResource, Sub: "8f14e45fceea167a5a36dedd4bea2543", Cnf: ConfirmKey(pub("agent_ed25519")), MissionS256: S256([]byte("fixture mission")), Tenant: "acme.example"}
	raw, err = TokenIssuer{Key: keys["ps_ed25519"]}.Person(personClaims)
	token("person-token", TypPerson, "ps_ed25519", raw, err)
	tp, err := pub("agent_ed25519").Thumbprint()
	if err != nil {
		t.Fatal(err)
	}
	rc := Claims{Iss: testResource, Jti: "fixture-resource", Iat: now, Exp: now + 300}
	raw, err = TokenIssuer{Key: keys["res_ed25519"]}.Resource(ResourceClaims{
		Claims: rc, Aud: testPS, PS: testPS, Sub: personClaims.Sub, PresentedJti: personClaims.Jti, AgentJKT: tp,
		Scope: "data.read", MissionS256: personClaims.MissionS256, Tenant: personClaims.Tenant,
		R3URI: testResource + "/r3/a", R3S256: S256([]byte("fixture r3")),
		Budget: &Budget{Amount: 5000000, Unit: "USD", Decimals: 6},
	})
	token("resource-token", TypResource, "res_ed25519", raw, err)
	ac := claims(testPS, "fixture-auth-ps")
	ac.Dwk = DwkPerson
	raw, err = TokenIssuer{Key: keys["ps_ed25519"]}.Auth(AuthClaims{Claims: ac, Aud: testResource, PS: testPS, Sub: personClaims.Sub, Cnf: ConfirmKey(pub("agent_ed25519")), Scope: "data.read"})
	token("auth-token-ps", TypAuth, "ps_ed25519", raw, err)
	ac = claims(testAS, "fixture-auth-as")
	ac.Dwk = DwkAccess
	raw, err = TokenIssuer{Key: keys["as_es256"]}.Auth(AuthClaims{Claims: ac, Aud: testResource, PS: testPS, Sub: personClaims.Sub, Cnf: ConfirmKey(pub("agent_es256")), Scope: "data.read"})
	token("auth-token-as", TypAuth, "as_es256", raw, err)

	body := []byte(`{"resource":"https://resource.example","capabilities":["interaction"]}`)
	revoke := []byte(`{"jti":"unique-token-identifier","exp":1788727813}`)
	agentSigner := func(tok, key string) *RequestSigner { return NewAgentSigner(tok, keys[key], clock) }
	serverSigner := func(iss, dwk, key string) *RequestSigner { return NewServerSigner(iss, dwk, keys[key], clock) }
	add := func(r goRequest, _ *http.Request) { f.Requests = append(f.Requests, r) }
	add(signRequestFixture(t, "agent-ed25519-post", "jwt", "agent_ed25519", agentSigner(agentEd, "agent_ed25519"), "POST", testPS+"/person", body, nil))
	add(signRequestFixture(t, "agent-ed25519-get", "jwt", "agent_ed25519", agentSigner(agentEd, "agent_ed25519"), "GET", testPS+"/pending/abc?x=1", nil, nil))
	add(signRequestFixture(t, "agent-es256-post", "jwt", "agent_es256", agentSigner(agentEC, "agent_es256"), "POST", testPS+"/person", body, nil))
	add(signRequestFixture(t, "agent-es256-get", "jwt", "agent_es256", agentSigner(agentEC, "agent_es256"), "GET", testPS+"/pending/abc", nil, nil))
	add(signRequestFixture(t, "server-ed25519-post", "jwks_uri", "ps_ed25519", serverSigner(testPS, DwkPerson, "ps_ed25519"), "POST", testResource+"/revoke", revoke, nil))
	add(signRequestFixture(t, "server-es256-get", "jwks_uri", "ps_es256", serverSigner(testPS, DwkPerson, "ps_es256"), "GET", testAS+"/pending/xyz", nil, nil))

	addResponse := func(name, ssKey string, req goRequest, clientReq *http.Request, status int, respBody []byte, iss, dwk string, components ...Component) {
		t.Helper()
		in := serverRequest(req.Method, req.URL, req.Headers, b64Decode(t, req.BodyB64))
		msg := NewRequestMessage(in)
		signed, err := serverSigner(iss, dwk, ssKey).SignResponse(&msg, status, http.Header{"Content-Type": {ContentTypeJSON}}, respBody, components...)
		if err != nil {
			t.Fatal(err)
		}
		inputs, err := ParseSignatureInput(signed.Header.Values(HeaderSignatureInput))
		if err != nil {
			t.Fatal(err)
		}
		clientMsg := NewRequestMessage(clientReq)
		base, err := SignatureBase(&clientMsg, &ResponseMessage{Status: status, Header: signed.Header}, inputs[0].Params)
		if err != nil {
			t.Fatal(err)
		}
		f.Responses = append(f.Responses, goResponse{
			Name: name, Alg: keys[ssKey].PublicJWK().Alg, Request: req, Status: status,
			Headers: headerPairs(signed.Header), BodyB64: base64.StdEncoding.EncodeToString(respBody),
			Base: base, SignerKey: ssKey, Issuer: iss, Dwk: dwk,
		})
	}
	decision := []byte(`{"sdi":"d-4d2a91","decision":"allow","scope":"calendar.read"}`)
	request := []byte(`{"sdi":"d-4d2a91","act":"person_token"}`)
	accept := map[string]string{HeaderAcceptSignature: AcceptSignatureForDecision(true)}
	r1, c1 := signRequestFixture(t, "decision-request", "jwks_uri", "ps_ed25519", serverSigner(testPS, DwkPerson, "ps_ed25519"), "POST", testSS+"/decision", request, accept)
	addResponse("decision-ed25519", "ss_ed25519", r1, c1, 200, decision, testSS, DwkSupervisor)
	addResponse("decision-es256", "ss_es256", r1, c1, 200, decision, testSS, DwkSupervisor)
	r2, c2 := signRequestFixture(t, "decision-poll", "jwks_uri", "ps_ed25519", serverSigner(testPS, DwkPerson, "ps_ed25519"), "GET", testSS+"/pending/q7x2", nil, nil)
	addResponse("decision-on-poll", "ss_ed25519", r2, c2, 200, decision, testSS, DwkSupervisor)
	r3, c3 := signRequestFixture(t, "usage-request", "jwks_uri", "ps_ed25519", serverSigner(testPS, DwkPerson, "ps_ed25519"), "POST", testResource+"/usage", []byte(`{"sub":"8f14e45fceea167a5a36dedd4bea2543"}`), nil)
	addResponse("usage-response", "res_ed25519", r3, c3, 200, []byte(`{"as_of":1790600000,"aud":"https://ps.example","unit":"USD","decimals":6}`), testResource, DwkResource,
		Comp("@status"), Comp("content-type"), Comp(HeaderContentDigest), ReqComp("@authority"), ReqComp("@path"))

	// Header fields, for the SDK's parsers.
	field := func(q Requirement) string {
		s, err := q.Field()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	f.Headers["requirement_auth_token"] = field(Requirement{Value: RequirementAuthToken, ResourceToken: "eyJhbGc.e30.sig"})
	f.Headers["requirement_interaction"] = field(Requirement{Value: RequirementInteraction, URL: "https://example.com/interact", Code: "A1B2-C3D4"})
	f.Headers["requirement_approval"] = field(Requirement{Value: RequirementApproval})
	f.Headers["requirement_person_token"] = field(Requirement{Value: RequirementPersonToken})
	caps, _ := CapabilitiesField([]string{CapabilityInteraction, CapabilityClarification, CapabilityPayment})
	f.Headers["capabilities"] = caps
	f.Headers["signature_error_input"] = (&SignatureFailure{Code: SigInvalidInput, RequiredInput: []string{"@method", "@authority", "@path", "signature-key", "content-digest", "content-type"}}).SignatureErrorField()
	f.Headers["signature_error_plain"] = (&SignatureFailure{Code: SigRevokedJWT}).SignatureErrorField()
	f.Headers["accept_scheme"] = schemesField([]Scheme{SchemeJWT})
	f.Headers["accept_alg"] = algsField(SupportedAlgs())
	return f
}

// TestInteropWriteGoFixtures regenerates go-signed.json with fresh keys when asked. The SDK's
// verdicts must then be regenerated too; see testdata/README.md.
func TestInteropWriteGoFixtures(t *testing.T) {
	if os.Getenv("AAUTH_INTEROP_WRITE") != "1" {
		t.Skip("set AAUTH_INTEROP_WRITE=1 to regenerate " + goSignedPath)
	}
	f := buildGoFixtures(t, newGoKeySet(t))
	f.Provenance = map[string]any{
		"generator":  "TestInteropWriteGoFixtures in internal/aauth/interop_test.go",
		"package":    "scaffoldapp/backend/internal/aauth",
		"go":         runtime.Version(),
		"date":       time.Now().UTC().Format("2006-01-02"),
		"keys":       "Throwaway keys generated for this file. They protect nothing.",
		"regenerate": "See testdata/README.md. Regenerate js-verdicts.json after this file.",
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goSignedPath, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadGoFile(t *testing.T) (*goFile, []byte, goKeySet) {
	t.Helper()
	var f goFile
	raw := readJSON(t, goSignedPath, &f)
	keys := goKeySet{}
	for name, jwk := range f.Keys {
		k, err := ParsePrivateJWK(jwk)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		keys[name] = k
	}
	return &f, raw, keys
}

func segments(jwt string) (string, string) {
	parts := strings.Split(jwt, ".")
	return parts[0] + "." + parts[1], parts[2]
}

// The Go signer still produces what the SDK judged: byte for byte where the algorithm is
// deterministic, and everything but the signature where it is not. A change to the wire format
// fails here, which is the prompt to regenerate and re-run the SDK.
func TestInteropGoSignerUnchanged(t *testing.T) {
	stored, _, keys := loadGoFile(t)
	if len(keys) == 0 {
		t.Fatal("no keys")
	}
	built := buildGoFixtures(t, keys)
	if stored.Now != built.Now || !reflect.DeepEqual(stored.Headers, built.Headers) {
		t.Errorf("headers changed:\n%v\n%v", stored.Headers, built.Headers)
	}
	if !reflect.DeepEqual(jsonBytes(t, stored.Issuers), jsonBytes(t, built.Issuers)) {
		t.Error("issuers changed")
	}
	if len(stored.Tokens) != len(built.Tokens) || len(stored.Requests) != len(built.Requests) || len(stored.Responses) != len(built.Responses) {
		t.Fatalf("fixture count changed")
	}
	// Tokens: an ES256 issuer's signature is randomized, so its token differs only there, and any
	// request presenting it differs in Signature-Key and the base by exactly that token.
	swap := map[string]string{}
	for i, s := range stored.Tokens {
		b := built.Tokens[i]
		si, ss := segments(s.JWT)
		bi, bs := segments(b.JWT)
		if si != bi {
			t.Errorf("%s: token changed", s.Name)
		}
		if s.Alg == "Ed25519" && ss != bs {
			t.Errorf("%s: deterministic signature changed", s.Name)
		}
		swap[b.JWT] = s.JWT
	}
	normalize := func(s string) string {
		for builtTok, storedTok := range swap {
			s = strings.ReplaceAll(s, builtTok, storedTok)
		}
		return s
	}
	compare := func(name, alg, base, wantBase string, have, want [][2]string, public JWK) {
		t.Helper()
		if normalize(wantBase) != base {
			t.Errorf("%s: base changed:\n%s\n%s", name, base, wantBase)
		}
		if len(have) != len(want) {
			t.Errorf("%s: header count changed", name)
			return
		}
		for i := range have {
			if have[i][0] != want[i][0] {
				t.Errorf("%s: header %s became %s", name, want[i][0], have[i][0])
				continue
			}
			if have[i][0] == "signature" {
				if alg == "Ed25519" && want[i][1] != have[i][1] {
					t.Errorf("%s: deterministic signature changed", name)
				}
				continue
			}
			if normalize(want[i][1]) != have[i][1] {
				t.Errorf("%s: %s changed:\n%s\n%s", name, have[i][0], have[i][1], want[i][1])
			}
		}
		// Whichever way it was made, the stored signature verifies over the stored base.
		for _, h := range have {
			if h[0] == "signature" {
				sigs, err := ParseSignatures([]string{h[1]})
				if err != nil {
					t.Fatal(err)
				}
				if err := public.Verify([]byte(base), sigs["sig"]); err != nil {
					t.Errorf("%s: stored signature: %v", name, err)
				}
			}
		}
	}
	for i, s := range stored.Requests {
		b := built.Requests[i]
		compare(s.Name, s.Alg, s.Base, b.Base, s.Headers, b.Headers, keys[s.SignerKey].PublicJWK())
	}
	for i, s := range stored.Responses {
		b := built.Responses[i]
		compare(s.Name, s.Alg, s.Base, b.Base, s.Headers, b.Headers, keys[s.SignerKey].PublicJWK())
	}
}

// The Go verifier accepts what the Go signer stored, by the same paths the SDK's requests take.
func TestInteropGoFixturesVerifyInGo(t *testing.T) {
	f, _, _ := loadGoFile(t)
	clock := func() time.Time { return time.Unix(f.Now, 0) }
	disc := interopDiscovery(clock, f.Issuers, t)
	for _, c := range f.Requests {
		t.Run(c.Name, func(t *testing.T) {
			cfg := VerifierConfig{Keys: disc, Clock: clock, Scheme: AgentPolicy()}
			if c.Scheme == "jwks_uri" {
				cfg.Scheme = ServerPolicy(DwkPerson)
			}
			v, err := NewVerifier(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := v.Verify(ctx, serverRequest(c.Method, c.URL, c.Headers, b64Decode(t, c.BodyB64))); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, c := range f.Responses {
		t.Run(c.Name, func(t *testing.T) {
			in := serverRequest(c.Request.Method, c.Request.URL, c.Request.Headers, b64Decode(t, c.Request.BodyB64))
			msg := NewRequestMessage(in)
			h := http.Header{}
			for _, p := range c.Headers {
				h.Add(p[0], p[1])
			}
			got, err := VerifySignedResponse(ctx, &msg, &ResponseMessage{Status: c.Status, Header: h}, b64Decode(t, c.BodyB64), ResponseVerifyOptions{Keys: disc, Issuer: c.Issuer, Dwk: c.Dwk})
			if err != nil {
				t.Fatal(err)
			}
			if got.SignatureBase != c.Base {
				t.Errorf("base:\n%s\n%s", got.SignatureBase, c.Base)
			}
		})
	}
	tv := &TokenVerifier{Keys: disc, Clock: clock}
	for _, c := range f.Tokens {
		var err error
		switch c.Typ {
		case TypAgent:
			_, err = tv.VerifyAgentToken(ctx, c.JWT, AgentTokenOptions{})
		case TypPerson:
			_, err = tv.VerifyPersonToken(ctx, c.JWT, PersonTokenOptions{Audience: testResource})
		case TypAuth:
			_, err = tv.VerifyAuthToken(ctx, c.JWT, AuthTokenOptions{Audience: testResource})
		case TypResource:
			tp := ""
			var jwt *JWT
			jwt, err = ParseJWT(c.JWT)
			if err == nil {
				var rc ResourceClaims
				_ = json.Unmarshal(jwt.Payload, &rc)
				tp = rc.AgentJKT
				_, err = tv.VerifyResourceToken(ctx, c.JWT, ResourceTokenOptions{Audience: testPS, ExpectedPS: testPS, AgentThumbprint: tp})
			}
		}
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
	}
}

// jsVerdicts is what the SDK said about go-signed.json.
type jsVerdicts struct {
	Provenance    map[string]any `json:"provenance"`
	GoSignedSHA   string         `json:"go_signed_sha256"`
	Tokens        []jsVerdict    `json:"tokens"`
	Requests      []jsVerdict    `json:"requests"`
	Responses     []jsVerdict    `json:"responses"`
	ResponsesHigh []jsVerdict    `json:"responses_high_level"`
	Headers       []jsVerdict    `json:"headers"`
}

type jsVerdict struct {
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
	Via      string `json:"via"`
	Error    string `json:"error"`
	Base     string `json:"base"`
}

// The SDK accepted exactly the bytes the Go signer stored.
func TestInteropSDKVerdictsOnGoFixtures(t *testing.T) {
	f, raw, _ := loadGoFile(t)
	var v jsVerdicts
	readJSON(t, jsVerdictsPath, &v)
	sum := sha256.Sum256(raw)
	if v.GoSignedSHA != hex.EncodeToString(sum[:]) {
		t.Fatalf("%s judged a different %s; regenerate it, see testdata/README.md", jsVerdictsPath, goSignedPath)
	}
	check := func(kind string, want int, got []jsVerdict) {
		t.Helper()
		if len(got) != want {
			t.Errorf("%s: %d verdicts for %d fixtures", kind, len(got), want)
		}
		for _, c := range got {
			if !c.Verified {
				t.Errorf("%s %s: refused by the SDK: %s", kind, c.Name, c.Error)
			}
		}
	}
	check("token", len(f.Tokens), v.Tokens)
	check("request", len(f.Requests), v.Requests)
	check("response", len(f.Responses), v.Responses)
	if len(v.Headers) == 0 {
		t.Error("no header verdicts")
	}
	for _, c := range v.Headers {
		if !c.Verified {
			t.Errorf("header %s: %s", c.Name, c.Error)
		}
	}
	// The SDK built the same signature base from the same message, on its own.
	bases := map[string]string{}
	for _, c := range f.Responses {
		bases[c.Name] = c.Base
	}
	for _, c := range v.Responses {
		if c.Base != bases[c.Name] {
			t.Errorf("%s: SDK-side base differs:\n%s\n%s", c.Name, c.Base, bases[c.Name])
		}
	}
	// A recorded gap in the SDK, kept honest: verify() takes no response target at all. If a newer
	// version gains it, this fails and the note in the report can go.
	if len(v.ResponsesHigh) == 0 {
		t.Error("no record of verify() on responses")
	}
	for _, c := range v.ResponsesHigh {
		if c.Verified {
			t.Errorf("%s: verify() now handles responses; update testdata/README.md", c.Name)
		}
		if c.Error == "" {
			t.Errorf("%s: no error recorded", c.Name)
		}
	}
}
