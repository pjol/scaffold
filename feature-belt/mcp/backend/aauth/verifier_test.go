package aauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func (p *parties) agentVerifier(t *testing.T, mutate ...func(*VerifierConfig)) *Verifier {
	t.Helper()
	cfg := VerifierConfig{Keys: p.disc, Scheme: AgentPolicy(), Clock: p.clock.Now, Origin: testPS}
	for _, m := range mutate {
		m(&cfg)
	}
	v, err := NewVerifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewVerifierValidatesConfig(t *testing.T) {
	p := newParties(t)
	for name, cfg := range map[string]VerifierConfig{
		"no keys":         {Scheme: AgentPolicy()},
		"no schemes":      {Keys: p.disc},
		"hwk":             {Keys: p.disc, Scheme: SchemePolicy{Schemes: []Scheme{SchemeHWK}}},
		"jwt with no typ": {Keys: p.disc, Scheme: SchemePolicy{Schemes: []Scheme{SchemeJWT}}},
		"person no aud":   {Keys: p.disc, Scheme: ResourcePolicy(TypPerson)},
		"resource token":  {Keys: p.disc, Scheme: ResourcePolicy(TypResource)},
		"server no docs":  {Keys: p.disc, Scheme: ServerPolicy()},
		"bad origin":      {Keys: p.disc, Scheme: AgentPolicy(), Origin: "http://ps.example"},
	} {
		if _, err := NewVerifier(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A request signed by an agent verifies, with the result typed by the token's typ.
func TestVerifyAgentRequest(t *testing.T) {
	p := newParties(t)
	token := p.agentToken(t)
	v := p.agentVerifier(t)
	body := []byte(`{"resource":"https://resource.example"}`)
	r := p.signedAgentRequest(t, token, testPS+"/person", body)
	res, err := v.Verify(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Scheme != SchemeJWT || res.Label != "sig" || res.Alg != AlgEd25519 || res.Agent == nil || res.Agent.Claims.Sub != testAgentSub || res.Person != nil || res.Auth != nil || res.Server != nil {
		t.Fatalf("%+v", res)
	}
	tp, _ := p.agentKey.PublicJWK().Thumbprint()
	if res.Thumbprint != tp || !res.Key.SameKey(p.agentKey.PublicJWK()) {
		t.Errorf("key %+v", res)
	}
	if res.Created != p.clock.Now().Unix() {
		t.Errorf("created %d", res.Created)
	}
	if want := []string{`"@method"`, `"@authority"`, `"@path"`, `"content-type"`, `"content-digest"`, `"signature-key"`}; strings.Join(res.Covered, " ") != strings.Join(want, " ") {
		t.Errorf("covered %v", res.Covered)
	}
	if string(res.Body) != string(body) {
		t.Errorf("body %s", res.Body)
	}
	// The handler can still read the body.
	got, _ := io.ReadAll(r.Body)
	if string(got) != string(body) {
		t.Errorf("restored body %s", got)
	}
	// With no body, content coverage isn't asked for.
	if _, err := v.Verify(ctx, p.signedAgentRequest(t, token, testPS+"/pending/x", nil)); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyES256AgentKey(t *testing.T) {
	p := newParties(t)
	p.agentKey = mustKey(t, AlgES256, "agent-ec")
	res, err := p.agentVerifier(t).Verify(ctx, p.signedAgentRequest(t, p.agentToken(t), testPS+"/person", []byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Alg != AlgES256 {
		t.Fatalf("alg %s", res.Alg)
	}
}

// Step 1: "If any are missing, return invalid_signature."
func TestStep1MissingHeaders(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	r := p.signedAgentRequest(t, p.agentToken(t), testPS+"/person", []byte(`{}`))
	for _, h := range []string{HeaderSignature, HeaderSignatureInput, HeaderSignatureKey} {
		c := r.Clone(ctx)
		c.Header.Del(h)
		_, err := v.Verify(ctx, c)
		wantFailure(t, err, SigInvalidSignature)
	}
	unsigned := httptest.NewRequest("POST", "/person", strings.NewReader(`{}`))
	unsigned.Host = "ps.example"
	_, err := v.Verify(ctx, unsigned)
	wantFailure(t, err, SigInvalidSignature)
}

func TestMalformedSignatureHeaders(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	r := p.signedAgentRequest(t, p.agentToken(t), testPS+"/pending/x", nil)
	cases := []struct {
		header, value string
		code          SignatureErrorCode
	}{
		{HeaderSignatureInput, "not ( a dictionary", SigInvalidSignature},
		{HeaderSignature, "not ( a dictionary", SigInvalidSignature},
		{HeaderSignature, `sig="not bytes"`, SigInvalidSignature},
		{HeaderSignature, `other=:AAAA:`, SigInvalidSignature},
		{HeaderSignatureInput, `sig="not a list"`, SigInvalidSignature},
		{HeaderSignatureInput, ``, SigInvalidSignature},
		{HeaderSignatureInput, `sig=(1 2);created=1790600000`, SigInvalidSignature},
		// After steps 2 and 3, a Signature-Key that can't be parsed is a key that can't be parsed.
		{HeaderSignatureKey, "not ( a dictionary", SigInvalidKey},
		{HeaderSignatureKey, `sig="jwt"`, SigInvalidKey},
		{HeaderSignatureKey, `sig=(a b)`, SigInvalidKey},
		{HeaderSignatureKey, `sig=jwt`, SigInvalidKey},
		{HeaderSignatureKey, `sig=jwt;jwt=token`, SigInvalidKey},
		{HeaderSignatureKey, `other=jwt;jwt="x"`, SigInvalidSignature}, // no member for the label
	}
	for _, c := range cases {
		t.Run(c.header+" "+c.value, func(t *testing.T) {
			m := r.Clone(ctx)
			m.Header.Set(c.header, c.value)
			_, err := v.Verify(ctx, m)
			wantFailure(t, err, c.code)
		})
	}
}

// withInput re-signs nothing: it changes Signature-Input so a covering check can fail first.
func withInput(r *http.Request, value string) *http.Request {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	c := r.Clone(ctx)
	c.Body = io.NopCloser(bytes.NewReader(body))
	c.Header.Set(HeaderSignatureInput, value)
	return c
}

// Step 2: missing coverage is invalid_input with required_input, before anything else is judged.
func TestStep2RequiredComponents(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	created := p.clock.Now().Unix()
	withBody := p.signedAgentRequest(t, p.agentToken(t), testPS+"/person", []byte(`{}`))
	noBody := p.signedAgentRequest(t, p.agentToken(t), testPS+"/pending/x", nil)
	cases := []struct {
		name    string
		r       *http.Request
		covered string
		missing string
	}{
		{"no @method", noBody, `"@authority" "@path" "signature-key"`, "@method"},
		{"no @authority", noBody, `"@method" "@path" "signature-key"`, "@authority"},
		{"no @path", noBody, `"@method" "@authority" "signature-key"`, "@path"},
		{"no signature-key", noBody, `"@method" "@authority" "@path"`, "signature-key"},
		{"empty", noBody, ``, "@method"},
		{"signature-key with a parameter is not signature-key", noBody, `"@method" "@authority" "@path" "signature-key";sf`, "signature-key"},
		{"no content-digest", withBody, `"@method" "@authority" "@path" "content-type" "signature-key"`, "content-digest"},
		{"no content-type", withBody, `"@method" "@authority" "@path" "content-digest" "signature-key"`, "content-type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := withInput(c.r, "sig=("+c.covered+");created="+itoa(created))
			_, err := v.Verify(ctx, r)
			f := wantFailure(t, err, SigInvalidInput)
			if !containsString(f.RequiredInput, c.missing) {
				t.Errorf("required_input %v lacks %s", f.RequiredInput, c.missing)
			}
		})
	}
	// The field on the wire, in the form of the signature-key draft's example.
	_, err := v.Verify(ctx, withInput(withBody, `sig=("@method");created=`+itoa(created)))
	f := wantFailure(t, err, SigInvalidInput)
	want := `error=invalid_input, required_input=("@method" "@authority" "@path" "signature-key" "content-digest" "content-type")`
	if got := f.SignatureErrorField(); got != want {
		t.Errorf("field:\n got %s\nwant %s", got, want)
	}
	// Additional server requirements.
	extra := p.agentVerifier(t, func(c *VerifierConfig) {
		c.RequiredComponents = func(*http.Request) []string { return []string{"x-custom", "@path"} }
	})
	_, err = extra.Verify(ctx, noBody)
	f = wantFailure(t, err, SigInvalidInput)
	if got := strings.Join(f.RequiredInput, " "); got != "@method @authority @path signature-key x-custom" {
		t.Errorf("required_input %s", got)
	}
	// A resource may leave body coverage to its own policy.
	lax := p.agentVerifier(t, func(c *VerifierConfig) { c.AllowUncoveredBody = true })
	bare := httptest.NewRequest("POST", "/person", strings.NewReader(`{}`))
	bare.Host = "ps.example"
	signer := NewAgentSigner(p.agentToken(t), p.agentKey, p.clock.Now)
	client, _ := http.NewRequest("POST", testPS+"/person", nil)
	if err := signer.SignRequest(client, nil); err != nil {
		t.Fatal(err)
	}
	bare.Header = client.Header.Clone()
	if _, err := lax.Verify(ctx, bare); err != nil {
		t.Errorf("uncovered body under AllowUncoveredBody: %v", err)
	}
	_, err = v.Verify(ctx, bare)
	wantFailure(t, err, SigInvalidInput)
}

func itoa(n int64) string { return jsonString(n) }

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Step 3, with its boundaries: created is required, old is invalid_signature, ahead is clock_skew.
func TestStep3Freshness(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	token := p.agentToken(t)
	signed := func(offset time.Duration) *http.Request {
		c := newTestClock()
		c.t = p.clock.Now().Add(offset)
		return signedRequest(t, NewAgentSigner(token, p.agentKey, c.Now), testPS+"/pending/x", nil)
	}
	if _, err := v.Verify(ctx, signed(-60*time.Second)); err != nil {
		t.Errorf("60 seconds old is within the window: %v", err)
	}
	if _, err := v.Verify(ctx, signed(60*time.Second)); err != nil {
		t.Errorf("60 seconds ahead is within the window: %v", err)
	}
	_, err := v.Verify(ctx, signed(-61*time.Second))
	wantFailure(t, err, SigInvalidSignature)
	_, err = v.Verify(ctx, signed(61*time.Second))
	wantFailure(t, err, SigClockSkew)

	// A configured window.
	narrow := p.agentVerifier(t, func(c *VerifierConfig) { c.SignatureWindow = 5 * time.Second })
	_, err = narrow.Verify(ctx, signed(-6*time.Second))
	wantFailure(t, err, SigInvalidSignature)
	wide := p.agentVerifier(t, func(c *VerifierConfig) { c.SignatureWindow = 5 * time.Minute })
	if _, err := wide.Verify(ctx, signed(-4*time.Minute)); err != nil {
		t.Error(err)
	}

	// Missing or malformed created.
	r := signed(0)
	for _, input := range []string{
		`sig=("@method" "@authority" "@path" "signature-key")`,
		`sig=("@method" "@authority" "@path" "signature-key");created="1790600000"`,
		`sig=("@method" "@authority" "@path" "signature-key");created=1.5`,
		`sig=("@method" "@authority" "@path" "signature-key");created`,
	} {
		_, err := v.Verify(ctx, withInput(r, input))
		wantFailure(t, err, SigInvalidSignature)
	}
	// An attacker-chosen huge created must not overflow into the window.
	for _, input := range []string{
		`sig=("@method" "@authority" "@path" "signature-key");created=999999999999999`,
		`sig=("@method" "@authority" "@path" "signature-key");created=-999999999999999`,
	} {
		_, err := v.Verify(ctx, withInput(r, input))
		var f *SignatureFailure
		if !errors.As(err, &f) || (f.Code != SigClockSkew && f.Code != SigInvalidSignature) {
			t.Errorf("%s: %v", input, err)
		}
	}
}

// "servers MUST honor it when present and MUST reject requests where expires is in the past."
func TestExpiresHonoured(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	token := p.agentToken(t)
	sign := func(expires int64) *http.Request {
		out, _ := http.NewRequest("GET", testPS+"/pending/x", nil)
		sk, _ := SerializeSignatureKey(JWTKeyMember("sig", token))
		out.Header.Set(HeaderSignatureKey, sk)
		params := NewSignatureParams([]Component{Comp("@method"), Comp("@authority"), Comp("@path"), Comp("signature-key")}, p.clock.Now())
		params.HasExpires, params.Expires = true, expires
		msg := NewRequestMessage(out)
		base, err := SignatureBase(&msg, nil, params)
		if err != nil {
			t.Fatal(err)
		}
		sig, _ := p.agentKey.Sign([]byte(base))
		input, _ := SignatureInputField("sig", params)
		field, _ := SignatureField("sig", sig)
		out.Header.Set(HeaderSignatureInput, input)
		out.Header.Set(HeaderSignature, field)
		return toServer(out, nil)
	}
	now := p.clock.Now().Unix()
	res, err := v.Verify(ctx, sign(now+30))
	if err != nil {
		t.Fatal(err)
	}
	if res.Expires != now+30 {
		t.Errorf("expires %d", res.Expires)
	}
	_, err = v.Verify(ctx, sign(now-1))
	wantFailure(t, err, SigInvalidSignature)
	_, err = v.Verify(ctx, sign(now))
	wantFailure(t, err, SigInvalidSignature)
	// A non-integer expires is malformed.
	r := sign(now + 30)
	r.Header.Set(HeaderSignatureInput, `sig=("@method" "@authority" "@path" "signature-key");created=`+itoa(now)+`;expires="soon"`)
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)
}

// Step 4: a scheme outside the policy, known or not, is unsupported_scheme naming what is accepted.
func TestStep4SchemeRejection(t *testing.T) {
	p := newParties(t)
	agent := p.agentVerifier(t)
	token := p.agentToken(t)
	created := itoa(p.clock.Now().Unix())
	input := `sig=("@method" "@authority" "@path" "signature-key");created=` + created
	base := p.signedAgentRequest(t, token, testPS+"/pending/x", nil)
	for _, sk := range []string{
		`sig=hwk;kty="OKP";crv="Ed25519";x="abc";alg="Ed25519"`,
		`sig=jwks_uri;id="https://agent.example";dwk="aauth-agent.json";kid="k"`,
		`sig=jkt-jwt;jwt="x.y.z"`,
		`sig=x509;x5u="https://a.example"`,
		`sig=cached;id="abc"`,
		`sig=made-up-scheme;anything="1"`,
	} {
		t.Run(sk, func(t *testing.T) {
			r := withInput(base, input)
			r.Header.Set(HeaderSignatureKey, sk)
			_, err := agent.Verify(ctx, r)
			f := wantFailure(t, err, SigUnsupportedScheme)
			if len(f.AcceptSchemes) != 1 || f.AcceptSchemes[0] != SchemeJWT {
				t.Errorf("accept %v", f.AcceptSchemes)
			}
		})
	}
	// A server endpoint accepts only jwks_uri.
	server, err := NewVerifier(VerifierConfig{Keys: p.disc, Scheme: ServerPolicy(DwkAgent), Clock: p.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = server.Verify(ctx, base)
	f := wantFailure(t, err, SigUnsupportedScheme)
	if f.AcceptSchemes[0] != SchemeJWKSURI {
		t.Errorf("accept %v", f.AcceptSchemes)
	}
	// Step 2 still runs first.
	bad := withInput(base, `sig=("@method");created=`+created)
	bad.Header.Set(HeaderSignatureKey, `sig=hwk;kty="OKP"`)
	_, err = agent.Verify(ctx, bad)
	wantFailure(t, err, SigInvalidInput)
	// And step 3 too.
	stale := withInput(base, `sig=("@method" "@authority" "@path" "signature-key");created=1`)
	stale.Header.Set(HeaderSignatureKey, `sig=hwk;kty="OKP"`)
	_, err = agent.Verify(ctx, stale)
	wantFailure(t, err, SigInvalidSignature)
}

// "A verifier MUST NOT reject a request because a member it did not select names a scheme the
// verifier does not implement".
func TestUnselectedMemberIgnored(t *testing.T) {
	p := newParties(t)
	token := p.agentToken(t)
	out, _ := http.NewRequest("GET", testPS+"/pending/x", nil)
	out.Header.Set(HeaderSignatureKey, `future=hwk;kty="OKP", mine=jwt;jwt="`+token+`"`)
	params := NewSignatureParams([]Component{Comp("@method"), Comp("@authority"), Comp("@path"), Comp("signature-key")}, p.clock.Now())
	msg := NewRequestMessage(out)
	base, err := SignatureBase(&msg, nil, params)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := p.agentKey.Sign([]byte(base))
	list, _ := params.Serialize()
	field, _ := SignatureField("mine", sig)
	out.Header.Set(HeaderSignatureInput, `future=("@method");created=1, mine=`+list)
	out.Header.Set(HeaderSignature, `future=:AAAA:, `+field)
	res, err := p.agentVerifier(t).Verify(ctx, toServer(out, nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Label != "mine" {
		t.Errorf("label %s", res.Label)
	}
}

// Step 5 for the jwt scheme. Token details are covered in tokens_test.go; these prove they reach
// the failure unchanged.
func TestStep5TokenFailures(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	claims := func() AgentClaims { return p.agentClaims() }

	expired := claims()
	expired.Exp = expired.Iat
	expired.Iat -= 10
	rawExpired, _ := SignJWT(p.apKey, TypAgent, expired)
	_, err := v.Verify(ctx, p.signedAgentRequest(t, rawExpired, testPS+"/x", nil))
	wantFailure(t, err, SigExpiredJWT)

	forged, _ := SignJWT(mustKey(t, AlgEd25519, "ap-1"), TypAgent, claims())
	_, err = v.Verify(ctx, p.signedAgentRequest(t, forged, testPS+"/x", nil))
	wantFailure(t, err, SigInvalidJWT)

	future := claims()
	future.Iat += 120
	rawFuture, _ := SignJWT(p.apKey, TypAgent, future)
	_, err = v.Verify(ctx, p.signedAgentRequest(t, rawFuture, testPS+"/x", nil))
	wantFailure(t, err, SigClockSkew)

	unknown, _ := SignJWT(mustKey(t, AlgEd25519, "no-such-kid"), TypAgent, claims())
	_, err = v.Verify(ctx, p.signedAgentRequest(t, unknown, testPS+"/x", nil))
	wantFailure(t, err, SigUnknownKey)

	_, err = v.Verify(ctx, p.signedAgentRequest(t, "not.a.jwt", testPS+"/x", nil))
	wantFailure(t, err, SigInvalidJWT)

	// Wrong typ: a person token at an agent-only endpoint.
	person, _ := p.personToken(t)
	_, err = v.Verify(ctx, p.signedAgentRequest(t, person, testPS+"/x", nil))
	wantFailure(t, err, SigInvalidJWT)

	// Wrong dwk.
	wrongDwk := claims()
	wrongDwk.Dwk = DwkPerson
	rawDwk, _ := SignJWT(p.apKey, TypAgent, wrongDwk)
	_, err = v.Verify(ctx, p.signedAgentRequest(t, rawDwk, testPS+"/x", nil))
	wantFailure(t, err, SigInvalidJWT)

	// Revoked.
	rev := p.agentVerifier(t, func(c *VerifierConfig) {
		c.Revocations = RevocationCheckerFunc(func(context.Context, string, string) (bool, error) { return true, nil })
	})
	_, err = rev.Verify(ctx, p.signedAgentRequest(t, p.agentToken(t), testPS+"/x", nil))
	wantFailure(t, err, SigRevokedJWT)

	// The revocation store being down is 500, not a blame on the token.
	down := p.agentVerifier(t, func(c *VerifierConfig) {
		c.Revocations = RevocationCheckerFunc(func(context.Context, string, string) (bool, error) { return false, errors.New("db") })
	})
	_, err = down.Verify(ctx, p.signedAgentRequest(t, p.agentToken(t), testPS+"/x", nil))
	var f *SignatureFailure
	if !errors.As(err, &f) || !f.Internal {
		t.Fatalf("%v", err)
	}
	w := httptest.NewRecorder()
	f.WriteTo(w)
	if w.Code != 500 || w.Header().Get(HeaderSignatureError) != "" {
		t.Errorf("%d %v", w.Code, w.Header())
	}
}

func TestStep5And6IssuerDiscovery(t *testing.T) {
	// issuer_missing and issuer_mismatch on a token's issuer.
	p := newParties(t)
	token := p.agentToken(t)
	p.net.set(MetadataURL(testAP, DwkAgent), fakeResponse{body: []byte(`{"jwks_uri":"https://agent.example/.well-known/jwks.json"}`)})
	_, err := p.agentVerifier(t).Verify(ctx, p.signedAgentRequest(t, token, testPS+"/x", nil))
	wantFailure(t, err, SigIssuerMissing)

	p2 := newParties(t)
	token2 := p2.agentToken(t)
	p2.net.set(MetadataURL(testAP, DwkAgent), fakeResponse{body: []byte(`{"issuer":"https://other.example","jwks_uri":"https://agent.example/.well-known/jwks.json"}`)})
	_, err = p2.agentVerifier(t).Verify(ctx, p2.signedAgentRequest(t, token2, testPS+"/x", nil))
	wantFailure(t, err, SigIssuerMismatch)
}

// Step 6 on cnf.jwk, including the Accept-Signature-Alg list.
func TestStep6Algorithm(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	mint := func(k JWK) string {
		c := p.agentClaims()
		c.Cnf = &Confirmation{JWK: k}
		raw, err := SignJWT(p.apKey, TypAgent, c)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	with := func(alg string) JWK {
		k := p.agentKey.PublicJWK()
		k.Alg = alg
		return k
	}
	for name, tc := range map[string]struct {
		key  JWK
		code SignatureErrorCode
	}{
		"alg absent":   {with(""), SigUnsupportedAlgorithm},
		"EdDSA":        {with("EdDSA"), SigUnsupportedAlgorithm},
		"none":         {with("none"), SigUnsupportedAlgorithm},
		"ES384":        {JWK{Kty: "EC", Crv: "P-384", X: "a", Y: "b", Alg: "ES384"}, SigUnsupportedAlgorithm},
		"kty mismatch": {with("ES256"), SigInvalidKey},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(ctx, p.signedAgentRequest(t, mint(tc.key), testPS+"/x", nil))
			f := wantFailure(t, err, tc.code)
			if tc.code == SigUnsupportedAlgorithm {
				w := httptest.NewRecorder()
				f.WriteTo(w)
				if got := w.Header().Get(HeaderAcceptSignatureAlg); got != "Ed25519, ES256" {
					t.Errorf("Accept-Signature-Alg %q", got)
				}
			}
		})
	}
	// A narrower accepted set is stated exactly.
	only := p.agentVerifier(t, func(c *VerifierConfig) { c.Algorithms = []Alg{AlgES256} })
	_, err := only.Verify(ctx, p.signedAgentRequest(t, p.agentToken(t), testPS+"/x", nil))
	f := wantFailure(t, err, SigUnsupportedAlgorithm)
	if len(f.AcceptAlgs) != 1 || f.AcceptAlgs[0] != AlgES256 {
		t.Errorf("accept %v", f.AcceptAlgs)
	}
}

// Step 7, the digest check, and the `alg` signature parameter.
func TestStep7Signature(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	token := p.agentToken(t)
	body := []byte(`{"resource":"https://resource.example"}`)
	sign := func() *http.Request { return p.signedAgentRequest(t, token, testPS+"/person", body) }

	// Another key signed.
	other := NewAgentSigner(token, mustKey(t, AlgEd25519, "other"), p.clock.Now)
	_, err := v.Verify(ctx, signedRequest(t, other, testPS+"/person", body))
	wantFailure(t, err, SigInvalidSignature)

	// Covered parts of the message changed in transit.
	r := sign()
	r.Method = "PUT"
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	r = sign()
	r.RequestURI = "/token"
	r.URL.Path = "/token"
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	r = sign()
	r.Header.Set("Content-Type", "text/plain")
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	// A token substituted in Signature-Key is caught by covering it.
	r = sign()
	otherToken := p.agentToken(t)
	sk, _ := SerializeSignatureKey(JWTKeyMember("sig", otherToken))
	r.Header.Set(HeaderSignatureKey, sk)
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	// Tampered body: the digest no longer matches.
	r = sign()
	r.Body = io.NopCloser(strings.NewReader(`{"resource":"https://evil.example"}`))
	_, err = v.Verify(ctx, r)
	f := wantFailure(t, err, SigInvalidSignature)
	if !errors.Is(f, ErrDigestMismatch) {
		t.Errorf("cause %v", f.Cause)
	}

	// Body and digest both replaced: the signature over the digest fails.
	r = sign()
	evil := []byte(`{"resource":"https://evil.example"}`)
	r.Body = io.NopCloser(strings.NewReader(string(evil)))
	r.Header.Set(HeaderContentDigest, ContentDigest(evil))
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	// A signature of the wrong shape.
	r = sign()
	r.Header.Set(HeaderSignature, `sig=:AAAA:`)
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	// The base can't be built: a covered field is absent.
	r = sign()
	input := r.Header.Get(HeaderSignatureInput)
	r.Header.Set(HeaderSignatureInput, strings.Replace(input, `"signature-key")`, `"signature-key" "x-missing")`, 1))
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	// "the req parameter MUST NOT be used for any component in a signature that targets a request".
	r = sign()
	r.Header.Set(HeaderSignatureInput, strings.Replace(input, `"signature-key")`, `"signature-key" "@path";req)`, 1))
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)

	// A body larger than the bound.
	small := p.agentVerifier(t, func(c *VerifierConfig) { c.MaxBodyBytes = 4 })
	_, err = small.Verify(ctx, sign())
	wantFailure(t, err, SigInvalidRequest)
}

// "Agents MUST NOT include the alg signature parameter, and verifiers MUST ignore it if present".
func TestAlgParameterIgnored(t *testing.T) {
	p := newParties(t)
	token := p.agentToken(t)
	out, _ := http.NewRequest("GET", testPS+"/pending/x", nil)
	sk, _ := SerializeSignatureKey(JWTKeyMember("sig", token))
	out.Header.Set(HeaderSignatureKey, sk)
	created := itoa(p.clock.Now().Unix())
	// A misleading alg and an unknown parameter stay in the base, and don't affect the operation. A keyid does the same, as long as it names this key: "the two MUST identify the same key".
	keyid := p.agentKey.PublicJWK().Kid
	input := `sig=("@method" "@authority" "@path" "signature-key");created=` + created + `;alg="hmac-sha256";keyid="` + keyid + `";custom=1`
	params := inputMember(t, input)
	msg := NewRequestMessage(out)
	base, err := SignatureBase(&msg, nil, params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(base, `;alg="hmac-sha256";keyid="`+keyid+`";custom=1`) {
		t.Fatalf("base:\n%s", base)
	}
	sig, _ := p.agentKey.Sign([]byte(base))
	field, _ := SignatureField("sig", sig)
	out.Header.Set(HeaderSignatureInput, input)
	out.Header.Set(HeaderSignature, field)
	res, err := p.agentVerifier(t).Verify(ctx, toServer(out, nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Alg != AlgEd25519 {
		t.Errorf("alg %s", res.Alg)
	}
}

// "If keyid is present for a label that also appears in Signature-Key, the two MUST identify the same key, and the verifier MUST take the key from Signature-Key." Under jwt the key is the token's cnf.jwk, and a kid on it is what
// the keyid can be held to. Its thumbprint names it too, and with no kid there is nothing to say a keyid names some other key.
func TestKeyIDUnderJWTNamesTheConfirmedKey(t *testing.T) {
	p := newParties(t)
	claims := p.agentClaims()
	jwk := p.agentKey.PublicJWK()
	jwk.Kid = "agent-key-1"
	claims.Cnf = ConfirmKey(jwk)
	if claims.Cnf.JWK.Kid != "agent-key-1" {
		t.Fatalf("the fixture has no kid on cnf.jwk: %+v", claims.Cnf.JWK)
	}
	withKid, err := SignJWT(p.apKey, TypAgent, claims)
	if err != nil {
		t.Fatal(err)
	}
	noKidClaims := p.agentClaims()
	bare := p.agentKey.PublicJWK()
	bare.Kid = ""
	noKidClaims.Cnf = ConfirmKey(bare)
	noKid, err := SignJWT(p.apKey, TypAgent, noKidClaims)
	if err != nil {
		t.Fatal(err)
	}
	thumbprint, err := jwk.Thumbprint()
	if err != nil {
		t.Fatal(err)
	}
	send := func(token, keyid string) error {
		out, _ := http.NewRequest("GET", testPS+"/pending/x", nil)
		sk, _ := SerializeSignatureKey(JWTKeyMember("sig", token))
		out.Header.Set(HeaderSignatureKey, sk)
		input := `sig=("@method" "@authority" "@path" "signature-key");created=` + itoa(p.clock.Now().Unix()) + `;keyid="` + keyid + `"`
		msg := NewRequestMessage(out)
		base, err := SignatureBase(&msg, nil, inputMember(t, input))
		if err != nil {
			t.Fatal(err)
		}
		sig, _ := p.agentKey.Sign([]byte(base))
		field, _ := SignatureField("sig", sig)
		out.Header.Set(HeaderSignatureInput, input)
		out.Header.Set(HeaderSignature, field)
		_, err = p.agentVerifier(t).Verify(ctx, toServer(out, nil))
		return err
	}
	for name, c := range map[string]struct {
		token, keyid string
		ok           bool
	}{
		"the kid":           {withKid, "agent-key-1", true},
		"the thumbprint":    {withKid, thumbprint, true},
		"another key":       {withKid, "another", false},
		"no kid to compare": {noKid, "local-name", true},
	} {
		err := send(c.token, c.keyid)
		if c.ok && err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if !c.ok {
			var f *SignatureFailure
			if !errors.As(err, &f) || f.Code != SigInvalidSignature {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}

// The @authority anchor: a signature made for another host fails even with the Host header forged
// to match it.
func TestOriginAnchorsAuthority(t *testing.T) {
	p := newParties(t)
	v := p.agentVerifier(t)
	token := p.agentToken(t)
	r := p.signedAgentRequest(t, token, "https://evil.example/person", []byte(`{}`))
	if r.Host != "evil.example" {
		t.Fatalf("host %s", r.Host)
	}
	_, err := v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)
	// A signature for this server passes whatever Host a proxy delivers.
	r = p.signedAgentRequest(t, token, testPS+"/person", []byte(`{}`))
	r.Host = "internal-pod:8080"
	if _, err := v.Verify(ctx, r); err != nil {
		t.Errorf("with a proxy's Host: %v", err)
	}
	// Default ports and case on the wire normalize the same way.
	r = p.signedAgentRequest(t, token, "https://PS.EXAMPLE:443/person", []byte(`{}`))
	if _, err := v.Verify(ctx, r); err != nil {
		t.Errorf("normalized authority: %v", err)
	}
	// With no Origin, the request's Host is used.
	open := p.agentVerifier(t, func(c *VerifierConfig) { c.Origin = "" })
	r = p.signedAgentRequest(t, token, "https://other.example/person", []byte(`{}`))
	if _, err := open.Verify(ctx, r); err != nil {
		t.Errorf("no anchor: %v", err)
	}
}

// Under the dev policy a loopback issuer verifies end to end, with its port in @authority.
func TestLoopbackOriginUnderPolicy(t *testing.T) {
	dev := IdentifierPolicy{AllowInsecureLoopback: true}
	clock := newTestClock()
	net := newFakeNet()
	disc := NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: net}, Clock: clock.Now, Policy: dev})
	apIss := "http://127.0.0.1:22261"
	psIss := "http://127.0.0.1:22260"
	apKey := mustKey(t, AlgEd25519, "ap-1")
	agentKey := mustKey(t, AlgEd25519, "agent")
	net.set(MetadataURL(apIss, DwkAgent), fakeResponse{body: jsonBytes(t, map[string]string{"issuer": apIss, "jwks_uri": apIss + "/.well-known/jwks.json"})})
	net.set(apIss+"/.well-known/jwks.json", fakeResponse{body: jsonBytes(t, JWKS{Keys: []JWK{apKey.PublicJWK()}})})
	now := clock.Now().Unix()
	token, err := TokenIssuer{Key: apKey, Policy: dev}.Agent(AgentClaims{
		Claims: Claims{Iss: apIss, Jti: NewJTI(), Iat: now, Exp: now + 3600},
		Sub:    "aauth:a@127.0.0.1:22261",
		Cnf:    ConfirmKey(agentKey.PublicJWK()),
		PS:     psIss,
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVerifier(VerifierConfig{Keys: disc, Scheme: AgentPolicy(), Clock: clock.Now, Origin: psIss, Identifiers: dev})
	if err != nil {
		t.Fatal(err)
	}
	signer := NewAgentSigner(token, agentKey, clock.Now)
	res, err := v.Verify(ctx, signedRequest(t, signer, psIss+"/person", []byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent.Claims.Iss != apIss {
		t.Errorf("%+v", res.Agent.Claims)
	}
	// The same token is refused in production.
	prod, err := NewVerifier(VerifierConfig{Keys: NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: net}, Clock: clock.Now}), Scheme: AgentPolicy(), Clock: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = prod.Verify(ctx, signedRequest(t, signer, psIss+"/person", []byte(`{}`)))
	wantFailure(t, err, SigInvalidJWT)
}

// Person and auth tokens at a resource: the result is typed by typ.
func TestResourcePolicyTokenTypes(t *testing.T) {
	p := newParties(t)
	v, err := NewVerifier(VerifierConfig{
		Keys: p.disc, Scheme: ResourcePolicy(TypAgent, TypPerson, TypAuth), Clock: p.clock.Now,
		Origin: testResource, Audience: testResource, AllowUncoveredBody: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	person, _ := p.personToken(t)
	res, err := v.Verify(ctx, p.signedAgentRequest(t, person, testResource+"/api", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Person == nil || res.Agent != nil || res.Auth != nil {
		t.Fatalf("%+v", res)
	}
	auth := p.authToken(t, p.authClaims())
	res, err = v.Verify(ctx, p.signedAgentRequest(t, auth, testResource+"/api", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Auth == nil || res.Person != nil || res.Agent != nil {
		t.Fatalf("%+v", res)
	}
	res, err = v.Verify(ctx, p.signedAgentRequest(t, p.agentToken(t), testResource+"/api", nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Agent == nil {
		t.Fatalf("%+v", res)
	}
	// aud is enforced.
	wrong, _ := p.personToken(t, func(c *PersonClaims) { c.Aud = "https://other.example" })
	_, err = v.Verify(ctx, p.signedAgentRequest(t, wrong, testResource+"/api", nil))
	wantFailure(t, err, SigInvalidJWT)
	// The token's cnf.jwk must be the key that signed: another key can't borrow it.
	borrowed := signedRequest(t, NewAgentSigner(person, mustKey(t, AlgEd25519, "thief"), p.clock.Now), testResource+"/api", nil)
	_, err = v.Verify(ctx, borrowed)
	wantFailure(t, err, SigInvalidSignature)
	// Resource tokens never sign a request.
	if _, err := NewVerifier(VerifierConfig{Keys: p.disc, Scheme: ResourcePolicy(TypResource), Audience: testResource}); err == nil {
		t.Error("accepted resource tokens for signing")
	}
}

func (p *parties) serverVerifier(t *testing.T, dwks ...string) *Verifier {
	t.Helper()
	v, err := NewVerifier(VerifierConfig{Keys: p.disc, Scheme: ServerPolicy(dwks...), Clock: p.clock.Now, Origin: testPS})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// The jwks_uri scheme: step 5 for a server, with the wrong-dwk case and identity from `id`.
func TestServerRequest(t *testing.T) {
	p := newParties(t)
	v := p.serverVerifier(t, DwkAgent, DwkResource)
	signer := NewServerSigner(testAP, DwkAgent, p.apKey, p.clock.Now)
	body := []byte(`{"jti":"x","exp":1790700000}`)
	res, err := v.Verify(ctx, signedRequest(t, signer, testPS+"/revoke", body))
	if err != nil {
		t.Fatal(err)
	}
	if res.Scheme != SchemeJWKSURI || res.Server == nil || res.Server.ID != testAP || res.Server.Dwk != DwkAgent || res.Server.Kid != "ap-1" || res.Agent != nil {
		t.Fatalf("%+v", res)
	}
	// Wrong dwk: this endpoint doesn't take a person server's document.
	bad := NewServerSigner(testPS, DwkPerson, p.psKey, p.clock.Now)
	_, err = v.Verify(ctx, signedRequest(t, bad, testPS+"/revoke", body))
	wantFailure(t, err, SigInvalidKey)
	// Unknown kid.
	unk := NewServerSigner(testAP, DwkAgent, mustKey(t, AlgEd25519, "absent"), p.clock.Now)
	_, err = v.Verify(ctx, signedRequest(t, unk, testPS+"/revoke", body))
	wantFailure(t, err, SigUnknownKey)
	// Copied kid, another key: the signature fails.
	imp := NewServerSigner(testAP, DwkAgent, mustKey(t, AlgEd25519, "ap-1"), p.clock.Now)
	_, err = v.Verify(ctx, signedRequest(t, imp, testPS+"/revoke", body))
	wantFailure(t, err, SigInvalidSignature)
	// Malformed members.
	base := signedRequest(t, signer, testPS+"/revoke", body)
	for _, sk := range []string{
		`sig=jwks_uri;dwk="aauth-agent.json";kid="ap-1"`,
		`sig=jwks_uri;id="https://agent.example";kid="ap-1"`,
		`sig=jwks_uri;id="https://agent.example";dwk="aauth-agent.json"`,
		`sig=jwks_uri;id="http://agent.example";dwk="aauth-agent.json";kid="ap-1"`,
		`sig=jwks_uri;id="https://agent.example:8443";dwk="aauth-agent.json";kid="ap-1"`,
		`sig=jwks_uri;id="https://agent.example";dwk="../evil";kid="ap-1"`,
	} {
		r := base.Clone(ctx)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		r.Header.Set(HeaderSignatureKey, sk)
		_, err := v.Verify(ctx, r)
		wantFailure(t, err, SigInvalidKey)
	}
	// keyid must name the same key.
	r := signedRequest(t, signer, testPS+"/revoke", body)
	input := r.Header.Get(HeaderSignatureInput)
	r.Header.Set(HeaderSignatureInput, input+`;keyid="another"`)
	_, err = v.Verify(ctx, r)
	wantFailure(t, err, SigInvalidSignature)
}

// Step 6 for a key that came from a JWKS. "Only the key the kid selects is subject to this
// requirement".
func TestServerKeyAlgorithmFromJWKS(t *testing.T) {
	p := newParties(t)
	key := p.apKey.PublicJWK()
	noAlg := key
	noAlg.Alg = ""
	eddsa := key
	eddsa.Alg = "EdDSA"
	mismatch := key
	mismatch.Alg = "ES256"
	for _, c := range []struct {
		name string
		key  JWK
		code SignatureErrorCode
	}{
		{"no alg", noAlg, SigUnsupportedAlgorithm},
		{"EdDSA", eddsa, SigUnsupportedAlgorithm},
		{"kty mismatch", mismatch, SigInvalidKey},
	} {
		t.Run(c.name, func(t *testing.T) {
			p2 := newParties(t)
			p2.apKey = p.apKey
			p2.publish(testAP, DwkAgent, c.key)
			v2 := p2.serverVerifier(t, DwkAgent)
			_, err := v2.Verify(ctx, signedRequest(t, NewServerSigner(testAP, DwkAgent, p.apKey, p2.clock.Now), testPS+"/revoke", []byte(`{}`)))
			wantFailure(t, err, c.code)
		})
	}
	// An unusable neighbour in the JWKS doesn't matter.
	p3 := newParties(t)
	p3.net.set(testAP+"/.well-known/jwks.json", fakeResponse{body: []byte(`{"keys":[{"kty":"AKP","kid":"pq","alg":"ML-DSA-65"},` + string(jsonBytes(t, p3.apKey.PublicJWK())) + `]}`)})
	if _, err := p3.serverVerifier(t, DwkAgent).Verify(ctx, signedRequest(t, NewServerSigner(testAP, DwkAgent, p3.apKey, p3.clock.Now), testPS+"/revoke", []byte(`{}`))); err != nil {
		t.Fatal(err)
	}
}

func TestReplayCache(t *testing.T) {
	p := newParties(t)
	cache := NewMemoryReplayCache(p.clock.Now)
	v := p.agentVerifier(t, func(c *VerifierConfig) { c.ReplayCache = cache })
	token := p.agentToken(t)
	if _, err := v.Verify(ctx, p.signedAgentRequest(t, token, testPS+"/pending/x", nil)); err != nil {
		t.Fatal(err)
	}
	_, err := v.Verify(ctx, p.signedAgentRequest(t, token, testPS+"/pending/x", nil))
	wantFailure(t, err, SigInvalidSignature)
	// A different path is a different tuple.
	if _, err := v.Verify(ctx, p.signedAgentRequest(t, token, testPS+"/pending/y", nil)); err != nil {
		t.Fatal(err)
	}
	// A failed signature doesn't poison the cache.
	p.clock.Advance(time.Second)
	forged := signedRequest(t, NewAgentSigner(token, mustKey(t, AlgEd25519, "x"), p.clock.Now), testPS+"/pending/z", nil)
	_, err = v.Verify(ctx, forged)
	wantFailure(t, err, SigInvalidSignature)
	if _, err := v.Verify(ctx, p.signedAgentRequest(t, token, testPS+"/pending/z", nil)); err != nil {
		t.Fatalf("after a forgery: %v", err)
	}
	// Entries lapse after the window.
	key := ReplayKey{Thumbprint: "t", Created: 1, Method: "GET", Authority: "a", Path: "/"}
	if seen, _ := cache.SeenOrAdd(ctx, key, time.Minute); seen {
		t.Fatal("new key seen")
	}
	if seen, _ := cache.SeenOrAdd(ctx, key, time.Minute); !seen {
		t.Fatal("repeat not seen")
	}
	p.clock.Advance(time.Minute)
	if seen, _ := cache.SeenOrAdd(ctx, key, time.Minute); seen {
		t.Fatal("expired key seen")
	}
}

// protocol, Verification: the response format of each failure, and signature-key draft, Response
// Body.
func TestFailureResponseWireFormat(t *testing.T) {
	cases := []struct {
		name    string
		f       *SignatureFailure
		headers map[string]string
		absent  []string
	}{
		{
			name:    "unsupported_scheme, the spec's example",
			f:       &SignatureFailure{Code: SigUnsupportedScheme, AcceptSchemes: []Scheme{SchemeJWT}},
			headers: map[string]string{"Signature-Error": "error=unsupported_scheme", "Accept-Signature-Scheme": "jwt"},
			absent:  []string{"Accept-Signature-Alg"},
		},
		{
			name:    "unsupported_scheme with two schemes",
			f:       &SignatureFailure{Code: SigUnsupportedScheme, AcceptSchemes: []Scheme{SchemeJWKSURI, SchemeJWT}},
			headers: map[string]string{"Accept-Signature-Scheme": "jwks_uri, jwt"},
		},
		{
			name:    "unsupported_algorithm, the spec's example",
			f:       &SignatureFailure{Code: SigUnsupportedAlgorithm, AcceptAlgs: []Alg{AlgEd25519, AlgES256}},
			headers: map[string]string{"Signature-Error": "error=unsupported_algorithm", "Accept-Signature-Alg": "Ed25519, ES256"},
			absent:  []string{"Accept-Signature-Scheme"},
		},
		{
			name:    "invalid_input",
			f:       &SignatureFailure{Code: SigInvalidInput, RequiredInput: []string{"@method", "@authority", "@path", "signature-key", "content-digest"}},
			headers: map[string]string{"Signature-Error": `error=invalid_input, required_input=("@method" "@authority" "@path" "signature-key" "content-digest")`},
		},
		{
			name:    "invalid_signature",
			f:       &SignatureFailure{Code: SigInvalidSignature, Detail: "x"},
			headers: map[string]string{"Signature-Error": "error=invalid_signature"},
			absent:  []string{"Accept-Signature-Alg", "Accept-Signature-Scheme"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteVerificationError(w, c.f)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status %d", w.Code)
			}
			for k, v := range c.headers {
				if got := w.Header().Get(k); got != v {
					t.Errorf("%s: %q, want %q", k, got, v)
				}
			}
			for _, k := range c.absent {
				if w.Header().Get(k) != "" {
					t.Errorf("%s present", k)
				}
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type %q", ct)
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["error"] != string(c.f.Code) || body["type"] != "urn:ietf:params:sig-error:"+string(c.f.Code) || body["status"] != float64(401) {
				t.Errorf("body %v", body)
			}
		})
	}
	// A non-failure error is ours.
	w := httptest.NewRecorder()
	WriteVerificationError(w, errors.New("boom"))
	if w.Code != 500 {
		t.Errorf("status %d", w.Code)
	}
}

// Every Signature-Error code in the draft's registry (signature-key draft, Initial Registry Contents).
func TestSignatureErrorCodes(t *testing.T) {
	want := []SignatureErrorCode{
		"unsupported_algorithm", "unsupported_scheme", "cache_miss", "invalid_signature", "invalid_input",
		"invalid_request", "invalid_key", "unknown_key", "issuer_missing", "issuer_mismatch",
		"invalid_jwt", "expired_jwt", "revoked_jwt", "clock_skew",
	}
	have := []SignatureErrorCode{
		SigUnsupportedAlgorithm, SigUnsupportedScheme, SigCacheMiss, SigInvalidSignature, SigInvalidInput,
		SigInvalidRequest, SigInvalidKey, SigUnknownKey, SigIssuerMissing, SigIssuerMismatch,
		SigInvalidJWT, SigExpiredJWT, SigRevokedJWT, SigClockSkew,
	}
	for i := range want {
		if want[i] != have[i] {
			t.Errorf("%s != %s", want[i], have[i])
		}
	}
}

func TestSchemePolicyMerge(t *testing.T) {
	m := AgentPolicy().Merge(ServerPolicy(DwkPerson))
	if len(m.Schemes) != 2 || m.TokenTypes[0] != TypAgent || m.ServerDwks[0] != DwkPerson {
		t.Fatalf("%+v", m)
	}
	p := newParties(t)
	v, err := NewVerifier(VerifierConfig{Keys: p.disc, Scheme: m, Clock: p.clock.Now, Origin: testPS})
	if err != nil {
		t.Fatal(err)
	}
	// A pending URL polled by an agent and by a server.
	if _, err := v.Verify(ctx, p.signedAgentRequest(t, p.agentToken(t), testPS+"/pending/x", nil)); err != nil {
		t.Error(err)
	}
	if _, err := v.Verify(ctx, signedRequest(t, NewServerSigner(testPS, DwkPerson, p.psKey, p.clock.Now), testPS+"/pending/x", nil)); err != nil {
		t.Error(err)
	}
}
