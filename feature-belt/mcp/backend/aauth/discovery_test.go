package aauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"scaffoldapp/backend/internal/safehttp"
)

func wantDiscovery(t *testing.T, err error, code SignatureErrorCode) *DiscoveryError {
	t.Helper()
	var de *DiscoveryError
	if !errors.As(err, &de) {
		t.Fatalf("want discovery error %s, got %v", code, err)
	}
	if de.Code != code {
		t.Fatalf("want %s, got %s (%v)", code, de.Code, de)
	}
	return de
}

const (
	testMetaURL = "https://issuer.example/.well-known/aauth-agent.json"
	testJWKSURL = "https://issuer.example/.well-known/jwks.json"
	testIssuer  = "https://issuer.example"
)

func discoveryWorld(t *testing.T, keys ...JWK) (*world, *PrivateKey) {
	t.Helper()
	w := newWorld(t)
	k := mustKey(t, AlgEd25519, "k1")
	w.publish(testIssuer, DwkAgent, append([]JWK{k.PublicJWK()}, keys...)...)
	return w, k
}

// protocol, Metadata Documents: "A document with no issuer MUST be rejected with issuer_missing; one
// whose issuer does not match MUST be rejected with issuer_mismatch".
func TestMetadataIssuerChecks(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		code SignatureErrorCode
	}{
		"missing":      {`{"jwks_uri":"https://issuer.example/.well-known/jwks.json"}`, SigIssuerMissing},
		"null":         {`{"issuer":null,"jwks_uri":"https://issuer.example/j"}`, SigIssuerMissing},
		"other":        {`{"issuer":"https://other.example","jwks_uri":"https://issuer.example/j"}`, SigIssuerMismatch},
		"case differs": {`{"issuer":"https://Issuer.example","jwks_uri":"https://issuer.example/j"}`, SigIssuerMismatch},
		"slash added":  {`{"issuer":"https://issuer.example/","jwks_uri":"https://issuer.example/j"}`, SigIssuerMismatch},
		"not a string": {`{"issuer":7,"jwks_uri":"https://issuer.example/j"}`, SigIssuerMismatch},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.net.set(testMetaURL, fakeResponse{body: []byte(tc.body)})
			_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
			wantDiscovery(t, err, tc.code)
			// Metadata reads answer the same.
			w2 := newWorld(t)
			w2.net.set(testMetaURL, fakeResponse{body: []byte(tc.body)})
			_, err = w2.disc.Metadata(ctx, testIssuer, DwkAgent)
			wantDiscovery(t, err, tc.code)
			// Keys were never fetched from a document that failed.
			if w.net.count(testJWKSURL) != 0 {
				t.Error("JWKS fetched after a failed issuer check")
			}
		})
	}
	// Byte equality holds when it matches.
	w, k := discoveryWorld(t)
	got, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
	if err != nil || !got.SameKey(k.PublicJWK()) {
		t.Fatalf("%+v %v", got, err)
	}
	doc, err := w.disc.Metadata(ctx, testIssuer, DwkAgent)
	if err != nil || doc.Issuer != testIssuer || doc.JWKSURI != testJWKSURL {
		t.Fatalf("%+v %v", doc, err)
	}
	var m map[string]any
	if err := doc.Decode(&m); err != nil || m["issuer"] != testIssuer {
		t.Fatalf("%v %v", m, err)
	}
}

func TestFetchFailures(t *testing.T) {
	cases := map[string]func(*world){
		"metadata 500":      func(w *world) { w.net.set(testMetaURL, fakeResponse{status: 500}) },
		"metadata 404":      func(*world) {},
		"metadata not JSON": func(w *world) { w.net.set(testMetaURL, fakeResponse{body: []byte("<html>")}) },
		"metadata network":  func(w *world) { w.net.set(testMetaURL, fakeResponse{err: errors.New("boom")}) },
		"jwks 500":          func(w *world) { w.publish(testIssuer, DwkAgent); w.net.set(testJWKSURL, fakeResponse{status: 500}) },
		"jwks not JSON": func(w *world) {
			w.publish(testIssuer, DwkAgent)
			w.net.set(testJWKSURL, fakeResponse{body: []byte("x")})
		},
		"jwks has no keys": func(w *world) {
			w.publish(testIssuer, DwkAgent)
			w.net.set(testJWKSURL, fakeResponse{body: []byte(`{}`)})
		},
		"no jwks_uri": func(w *world) {
			w.net.set(testMetaURL, fakeResponse{body: []byte(`{"issuer":"https://issuer.example"}`)})
		},
		"insecure jwks_uri": func(w *world) {
			w.net.set(testMetaURL, fakeResponse{body: []byte(`{"issuer":"https://issuer.example","jwks_uri":"http://issuer.example/j"}`)})
		},
		"key absent": func(w *world) { w.publish(testIssuer, DwkAgent, mustKey(w.t, AlgEd25519, "other").PublicJWK()) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			setup(w)
			_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
			wantDiscovery(t, err, SigUnknownKey)
		})
	}
	// The selected member being unparseable is invalid_key.
	w := newWorld(t)
	w.publish(testIssuer, DwkAgent)
	w.net.set(testJWKSURL, fakeResponse{body: []byte(`{"keys":[{"kid":"k1","kty":5}]}`)})
	_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
	wantDiscovery(t, err, SigInvalidKey)
	// A document over the limit fails, rather than being truncated.
	w2 := newWorld(t)
	w2.publish(testIssuer, DwkAgent, mustKey(t, AlgEd25519, "k1").PublicJWK())
	big := NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: w2.net}, Clock: w2.clock.Now, MaxBodyBytes: 8})
	var de *DiscoveryError
	if _, err := big.Key(ctx, testIssuer, DwkAgent, "k1"); !errors.As(err, &de) || !errors.Is(err, safehttp.ErrTooLarge) {
		t.Fatalf("%v", err)
	}
}

// "MUST NOT fetch a given issuer's JWKS more frequently than once per minute."
func TestUnknownKidRefreshFloor(t *testing.T) {
	w, _ := discoveryWorld(t)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	if n := w.net.count(testJWKSURL); n != 1 {
		t.Fatalf("fetches %d", n)
	}
	// The issuer rotates. Inside the minute an unknown kid doesn't fetch, however often it is asked.
	k2 := mustKey(t, AlgEd25519, "k2")
	w.publish(testIssuer, DwkAgent, k2.PublicJWK())
	for i := 0; i < 5; i++ {
		_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k2")
		wantDiscovery(t, err, SigUnknownKey)
		w.clock.Advance(10 * time.Second)
	}
	if n := w.net.count(testJWKSURL); n != 1 {
		t.Fatalf("fetched inside the floor: %d", n)
	}
	// Past the minute, an unknown kid refreshes, and finds it.
	w.clock.Advance(10 * time.Second)
	got, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k2")
	if err != nil {
		t.Fatal(err)
	}
	if !got.SameKey(k2.PublicJWK()) {
		t.Fatal("wrong key")
	}
	if n := w.net.count(testJWKSURL); n != 2 {
		t.Fatalf("fetches %d", n)
	}
	// A kid that stays unknown is not refreshed again inside the floor either.
	_, err = w.disc.Key(ctx, testIssuer, DwkAgent, "k3")
	wantDiscovery(t, err, SigUnknownKey)
	if n := w.net.count(testJWKSURL); n != 2 {
		t.Fatalf("fetches %d", n)
	}
}

// The floor is on the issuer's JWKS, not on the metadata document naming it: a PS and an AS on one host
// that publish one key set are one fetch a minute.
func TestFloorAcrossMetadataDocuments(t *testing.T) {
	w := newWorld(t)
	k := mustKey(t, AlgEd25519, "k1")
	w.publish(testIssuer, DwkPerson, k.PublicJWK())
	w.publish(testIssuer, DwkAccess, k.PublicJWK())
	if _, err := w.disc.Key(ctx, testIssuer, DwkPerson, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.disc.Key(ctx, testIssuer, DwkAccess, "k1"); err != nil {
		t.Fatalf("served from the same fetch: %v", err)
	}
	if n := w.net.count(testJWKSURL); n != 1 {
		t.Fatalf("one key set fetched %d times inside the floor", n)
	}
	// Past the floor, a refresh under either name fetches once more, and the other shares it.
	w.clock.Advance(time.Minute)
	if _, _, err := w.disc.RefreshKey(ctx, testIssuer, DwkPerson, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.disc.RefreshKey(ctx, testIssuer, DwkAccess, "k1"); err != nil {
		t.Fatal(err)
	}
	if n := w.net.count(testJWKSURL); n != 2 {
		t.Fatalf("fetches %d", n)
	}
	// A failed fetch counts toward the floor too.
	w2 := newWorld(t)
	w2.publish(testIssuer, DwkPerson, k.PublicJWK())
	w2.publish(testIssuer, DwkAccess, k.PublicJWK())
	w2.net.set(testJWKSURL, fakeResponse{status: 500})
	_, err := w2.disc.Key(ctx, testIssuer, DwkPerson, "k1")
	wantDiscovery(t, err, SigUnknownKey)
	_, err = w2.disc.Key(ctx, testIssuer, DwkAccess, "k1")
	wantDiscovery(t, err, SigUnknownKey)
	if n := w2.net.count(testJWKSURL); n != 1 {
		t.Fatalf("a failing key set fetched %d times", n)
	}
	// Two documents naming two key sets are two resources, each with its own floor.
	w3 := newWorld(t)
	w3.net.set(MetadataURL(testIssuer, DwkPerson), fakeResponse{body: jsonBytes(t, map[string]string{"issuer": testIssuer, "jwks_uri": testIssuer + "/a"})})
	w3.net.set(MetadataURL(testIssuer, DwkAccess), fakeResponse{body: jsonBytes(t, map[string]string{"issuer": testIssuer, "jwks_uri": testIssuer + "/b"})})
	w3.net.set(testIssuer+"/a", fakeResponse{body: jsonBytes(t, JWKS{Keys: []JWK{k.PublicJWK()}})})
	w3.net.set(testIssuer+"/b", fakeResponse{body: jsonBytes(t, JWKS{Keys: []JWK{k.PublicJWK()}})})
	if _, err := w3.disc.Key(ctx, testIssuer, DwkPerson, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := w3.disc.Key(ctx, testIssuer, DwkAccess, "k1"); err != nil {
		t.Fatal(err)
	}
	// The record of fetches is bounded like the cache.
	d := NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: newFakeNet()}, MaxIssuers: 2, Clock: w.clock.Now})
	for _, h := range []string{"a", "b", "c", "d"} {
		_, _, _ = d.getJWKS(ctx, "https://"+h+".example", "https://"+h+".example/jwks")
	}
	if len(d.jwks) > 2 {
		t.Fatalf("record size %d", len(d.jwks))
	}
}

// "If a JWKS fetch fails, implementations SHOULD use the cached JWKS"; "SHOULD be discarded after a
// maximum of 24 hours".
func TestStaleWhenOnlyTheJWKSFails(t *testing.T) {
	w, k := discoveryWorld(t)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	// The metadata still answers. The key set is what's down.
	w.net.set(testJWKSURL, fakeResponse{status: 500})
	w.clock.Advance(defaultJWKSTTL + time.Second)
	got, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	if !got.SameKey(k.PublicJWK()) {
		t.Fatal("wrong key")
	}
}

func TestStaleOnFailureUntil24Hours(t *testing.T) {
	w, k := discoveryWorld(t)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	w.net.set(testMetaURL, fakeResponse{status: 503})
	// Past the default TTL, the refresh fails, and the stale copy serves.
	w.clock.Advance(defaultJWKSTTL + time.Second)
	got, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	if !got.SameKey(k.PublicJWK()) {
		t.Fatal("wrong key")
	}
	// Still serving just before 24 hours from the last success.
	w.clock.Advance(maxJWKSCacheAge - defaultJWKSTTL - 2*time.Second)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatalf("just under 24h: %v", err)
	}
	// At 24 hours it is discarded.
	w.clock.Advance(2 * time.Second)
	_, err = w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
	wantDiscovery(t, err, SigUnknownKey)
	// And it recovers once the issuer does, within the floor.
	w.net.set(testMetaURL, fakeResponse{body: jsonBytes(t, map[string]string{"issuer": testIssuer, "jwks_uri": testJWKSURL})})
	w.clock.Advance(2 * defaultMaxBackoff)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatalf("recovered: %v", err)
	}
}

// A fresh success doesn't outlive 24 hours either, "regardless of cache headers".
func TestCacheHeadersClamped(t *testing.T) {
	w := newWorld(t)
	k := mustKey(t, AlgEd25519, "k1")
	long := http.Header{"Cache-Control": {"max-age=31536000"}}
	w.net.set(testMetaURL, fakeResponse{header: long, body: jsonBytes(t, map[string]string{"issuer": testIssuer, "jwks_uri": testJWKSURL})})
	w.net.set(testJWKSURL, fakeResponse{header: long, body: jsonBytes(t, JWKS{Keys: []JWK{k.PublicJWK()}})})
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	// Honoured up to the cap: no fetch for a day.
	w.clock.Advance(23 * time.Hour)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	if n := w.net.count(testJWKSURL); n != 1 {
		t.Fatalf("fetches %d", n)
	}
	w.clock.Advance(time.Hour)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	if n := w.net.count(testJWKSURL); n != 2 {
		t.Fatalf("no refresh past 24h: %d", n)
	}
}

func TestFreshnessFromHeaders(t *testing.T) {
	clock := newTestClock()
	d := NewDiscovery(DiscoveryConfig{Clock: clock.Now, Client: &http.Client{}})
	for name, c := range map[string]struct {
		h    http.Header
		want time.Duration
	}{
		"no headers":     {http.Header{}, defaultJWKSTTL},
		"max-age":        {http.Header{"Cache-Control": {"public, max-age=600"}}, 10 * time.Minute},
		"max-age floor":  {http.Header{"Cache-Control": {"max-age=5"}}, time.Minute},
		"no-store":       {http.Header{"Cache-Control": {"no-store"}}, time.Minute},
		"no-cache wins":  {http.Header{"Cache-Control": {"max-age=600, no-cache"}}, time.Minute},
		"max-age cap":    {http.Header{"Cache-Control": {"max-age=99999999"}}, 24 * time.Hour},
		"expires":        {http.Header{"Expires": {clock.Now().Add(30 * time.Minute).UTC().Format(http.TimeFormat)}}, 30 * time.Minute},
		"expires past":   {http.Header{"Expires": {clock.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)}}, time.Minute},
		"max-age beats":  {http.Header{"Cache-Control": {"max-age=120"}, "Expires": {clock.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}}, 2 * time.Minute},
		"age subtracted": {http.Header{"Cache-Control": {"max-age=600"}, "Age": {"300"}}, 5 * time.Minute},
	} {
		if got := d.freshness(&http.Response{Header: c.h}); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

// "SHOULD retry with exponential backoff", no faster than the floor.
func TestBackoffAfterFailure(t *testing.T) {
	w := newWorld(t)
	w.net.set(testMetaURL, fakeResponse{status: 500})
	attempt := func() {
		_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
		wantDiscovery(t, err, SigUnknownKey)
	}
	attempt()
	if n := w.net.count(testMetaURL); n != 1 {
		t.Fatalf("fetches %d", n)
	}
	attempt()
	if n := w.net.count(testMetaURL); n != 1 {
		t.Fatalf("inside the floor: %d", n)
	}
	w.clock.Advance(time.Minute)
	attempt() // second failure: next attempt in 2 minutes
	if n := w.net.count(testMetaURL); n != 2 {
		t.Fatalf("after the floor: %d", n)
	}
	w.clock.Advance(time.Minute)
	attempt()
	if n := w.net.count(testMetaURL); n != 2 {
		t.Fatalf("inside the backoff: %d", n)
	}
	w.clock.Advance(time.Minute)
	attempt() // third failure: next in 4 minutes
	if n := w.net.count(testMetaURL); n != 3 {
		t.Fatalf("after the backoff: %d", n)
	}
	w.clock.Advance(3 * time.Minute)
	attempt()
	if n := w.net.count(testMetaURL); n != 3 {
		t.Fatalf("doubled: %d", n)
	}
	// The backoff never exceeds the cap.
	for i := 0; i < 12; i++ {
		w.clock.Advance(2 * defaultMaxBackoff)
		attempt()
	}
	before := w.net.count(testMetaURL)
	w.clock.Advance(defaultMaxBackoff)
	attempt()
	if n := w.net.count(testMetaURL); n != before+1 {
		t.Fatalf("capped: %d -> %d", before, n)
	}
}

// "If a cached key matching the JWT kid fails signature verification, the verifier SHOULD refresh the
// issuer's JWKS once and retry", for a token.
func TestRefreshOnVerifyFailure(t *testing.T) {
	p := newParties(t)
	// Warm the cache with the AP's first key.
	first := p.agentToken(t)
	if _, err := p.tokens().VerifyAgentToken(ctx, first, AgentTokenOptions{}); err != nil {
		t.Fatal(err)
	}
	jwksURL := testAP + "/.well-known/jwks.json"
	if n := p.net.count(jwksURL); n != 1 {
		t.Fatalf("fetches %d", n)
	}
	// Silent re-key under the same kid.
	newKey := mustKey(t, AlgEd25519, "ap-1")
	p.publish(testAP, DwkAgent, newKey.PublicJWK())
	p.apKey = newKey
	rekeyed := p.agentToken(t)
	// Inside the floor the refresh can't happen: invalid_jwt, no fetch.
	_, err := p.tokens().VerifyAgentToken(ctx, rekeyed, AgentTokenOptions{})
	wantTokenSig(t, err, SigInvalidJWT)
	if n := p.net.count(jwksURL); n != 1 {
		t.Fatalf("fetched inside the floor: %d", n)
	}
	// After the floor it refreshes once and the retry passes.
	p.clock.Advance(time.Minute)
	rekeyed = p.agentToken(t)
	if _, err := p.tokens().VerifyAgentToken(ctx, rekeyed, AgentTokenOptions{}); err != nil {
		t.Fatalf("after refresh: %v", err)
	}
	if n := p.net.count(jwksURL); n != 2 {
		t.Fatalf("fetches %d", n)
	}
	// A forgery after the floor: one refresh, then invalid_jwt.
	p.clock.Advance(time.Minute)
	forged, _ := SignJWT(mustKey(t, AlgEd25519, "ap-1"), TypAgent, p.agentClaims())
	_, err = p.tokens().VerifyAgentToken(ctx, forged, AgentTokenOptions{})
	wantTokenSig(t, err, SigInvalidJWT)
	if n := p.net.count(jwksURL); n != 3 {
		t.Fatalf("fetches %d", n)
	}
	// The key removed after the failure: "unknown_key (if the key is then absent)".
	p.clock.Advance(time.Minute)
	p.publish(testAP, DwkAgent, mustKey(t, AlgEd25519, "other").PublicJWK())
	forged, _ = SignJWT(mustKey(t, AlgEd25519, "ap-1"), TypAgent, p.agentClaims())
	_, err = p.tokens().VerifyAgentToken(ctx, forged, AgentTokenOptions{})
	wantTokenSig(t, err, SigUnknownKey)
}

// The same refresh, for a server's key under the jwks_uri scheme.
func TestRefreshOnVerifyFailureServerScheme(t *testing.T) {
	p := newParties(t)
	v := p.serverVerifier(t, DwkAgent)
	sign := func(key *PrivateKey) *http.Request {
		return signedRequest(t, NewServerSigner(testAP, DwkAgent, key, p.clock.Now), testPS+"/revoke", []byte(`{}`))
	}
	if _, err := v.Verify(ctx, sign(p.apKey)); err != nil {
		t.Fatal(err)
	}
	newKey := mustKey(t, AlgEd25519, "ap-1")
	p.publish(testAP, DwkAgent, newKey.PublicJWK())
	_, err := v.Verify(ctx, sign(newKey))
	wantFailure(t, err, SigInvalidSignature)
	p.clock.Advance(time.Minute)
	res, err := v.Verify(ctx, sign(newKey))
	if err != nil {
		t.Fatalf("after refresh: %v", err)
	}
	if !res.Key.SameKey(newKey.PublicJWK()) {
		t.Error("result must carry the refreshed key")
	}
}

// A document that stops binding to its identity isn't served from cache.
func TestIssuerFailureDropsCache(t *testing.T) {
	w, _ := discoveryWorld(t)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	w.net.set(testMetaURL, fakeResponse{body: []byte(`{"issuer":"https://takeover.example","jwks_uri":"https://issuer.example/.well-known/jwks.json"}`)})
	w.clock.Advance(defaultJWKSTTL + time.Second)
	_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1")
	wantDiscovery(t, err, SigIssuerMismatch)
	// RefreshKey reports the same verdict, once the floor allows another look.
	w.clock.Advance(defaultMaxBackoff)
	_, _, err = w.disc.RefreshKey(ctx, testIssuer, DwkAgent, "k1")
	wantDiscovery(t, err, SigIssuerMismatch)
}

// RefreshKey during a transport failure leaves the caller's failure standing.
func TestRefreshKeyTransportFailure(t *testing.T) {
	w, _ := discoveryWorld(t)
	if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
	w.clock.Advance(time.Minute)
	w.net.set(testMetaURL, fakeResponse{status: 500})
	if _, fetched, err := w.disc.RefreshKey(ctx, testIssuer, DwkAgent, "k1"); fetched || err != nil {
		t.Fatalf("fetched=%v err=%v", fetched, err)
	}
}

// signature-key draft, Scheme-Specific Risks.
func TestEgressAdmission(t *testing.T) {
	// The default client: real egress admission, no fake transport. Nothing here reaches the network.
	strict := NewDiscovery(DiscoveryConfig{})
	dev := IdentifierPolicy{AllowInsecureLoopback: true}
	blocked := func(t *testing.T, err error) {
		t.Helper()
		var de *DiscoveryError
		if !errors.As(err, &de) || !de.Blocked || de.Code != SigUnknownKey {
			t.Fatalf("want blocked, got %v", err)
		}
	}
	for _, iss := range []string{
		"http://agent.example",       // HTTPS is required
		"https://127.0.0.1",          // loopback
		"https://10.0.0.1",           // private
		"https://192.168.1.1",        // private
		"https://169.254.169.254",    // link-local, cloud metadata
		"https://[::1]",              // loopback
		"https://[fd00:ec2::254]",    // metadata over IPv6
		"https://2130706433",         // decimal loopback
		"https://0x7f000001",         // hex loopback
		"https://agent.example:8443", // not a server identifier
		"https://agent.example/path", // not a server identifier
		"https://agent.example#@evil.example",
	} {
		t.Run(iss, func(t *testing.T) {
			_, err := strict.Key(ctx, iss, DwkAgent, "k")
			blocked(t, err)
		})
	}
	// Names that resolve locally are refused before any dial.
	for _, iss := range []string{"https://localhost", "https://metadata.google.internal", "https://printer.local"} {
		t.Run(iss, func(t *testing.T) {
			_, err := strict.Key(ctx, iss, DwkAgent, "k")
			blocked(t, err)
		})
	}
	// A dwk is one well-known name, so it can't walk off the path.
	for _, dwk := range []string{"../../admin", "a/b", "", ".hidden", "x y", "a?b=c"} {
		_, err := strict.Key(ctx, "https://agent.example", dwk, "k")
		blocked(t, err)
	}

	// jwks_uri is checked as hard as the metadata URL, and a private address named in a document is
	// refused even though the document itself was fine.
	for _, jwks := range []string{
		"http://issuer.example/jwks",
		"https://127.0.0.1/jwks",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/jwks",
		"https://user:pw@issuer.example/jwks",
	} {
		t.Run("jwks_uri "+jwks, func(t *testing.T) {
			w := newWorld(t)
			w.net.set(testMetaURL, fakeResponse{body: jsonBytes(t, map[string]string{"issuer": testIssuer, "jwks_uri": jwks})})
			_, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k")
			blocked(t, err)
		})
	}
	// The dev policy opens loopback only, not a public host over http.
	if _, err := CheckEgressURL("http://localhost:22260/x", dev); err != nil {
		t.Error(err)
	}
	if _, err := CheckEgressURL("http://example.com/x", dev); !safehttp.IsBlocked(err) {
		t.Errorf("http to a public host under dev: %v", err)
	}
	if _, err := CheckEgressURL("http://localhost:22260/x", IdentifierPolicy{}); !safehttp.IsBlocked(err) {
		t.Errorf("production: %v", err)
	}
	if _, err := CheckEgressURL("https://resource.example/r3/a", IdentifierPolicy{}); err != nil {
		t.Error(err)
	}
}

// "Treat cross-origin jwks_uri URLs as requiring explicit deployment admission."
func TestCrossOriginJWKS(t *testing.T) {
	k := mustKey(t, AlgEd25519, "k1")
	setup := func(net *fakeNet) {
		net.set(testMetaURL, fakeResponse{body: jsonBytes(t, map[string]string{"issuer": testIssuer, "jwks_uri": "https://keys.cdn.example/jwks"})})
		net.set("https://keys.cdn.example/jwks", fakeResponse{body: jsonBytes(t, JWKS{Keys: []JWK{k.PublicJWK()}})})
	}
	net := newFakeNet()
	setup(net)
	refuse := NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: net}})
	_, err := refuse.Key(ctx, testIssuer, DwkAgent, "k1")
	var de *DiscoveryError
	if !errors.As(err, &de) || !de.Blocked {
		t.Fatalf("%v", err)
	}
	if net.count("https://keys.cdn.example/jwks") != 0 {
		t.Fatal("a refused cross-origin jwks_uri was fetched")
	}
	net2 := newFakeNet()
	setup(net2)
	admit := NewDiscovery(DiscoveryConfig{
		Client: &http.Client{Transport: net2},
		AdmitCrossOriginJWKS: func(issuer, jwksURI string) bool {
			return issuer == testIssuer && strings.HasPrefix(jwksURI, "https://keys.cdn.example/")
		},
	})
	if _, err := admit.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
		t.Fatal(err)
	}
}

// "Refuse or constrain redirects": they are refused, on the default client and on an injected one.
func TestRedirectsRefused(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"issuer":"x"}`))
	}))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/.well-known/aauth-agent.json", http.StatusFound)
	}))
	defer redir.Close()
	dev := IdentifierPolicy{AllowInsecureLoopback: true}
	d := NewDiscovery(DiscoveryConfig{Policy: dev}) // the real egress client under the dev policy
	_, err := d.Metadata(ctx, redir.URL, DwkAgent)
	wantDiscovery(t, err, SigUnknownKey)
	if hits != 0 {
		t.Fatal("a redirect was followed")
	}
	injected := NewDiscovery(DiscoveryConfig{Policy: dev, Client: &http.Client{}})
	_, err = injected.Metadata(ctx, redir.URL, DwkAgent)
	wantDiscovery(t, err, SigUnknownKey)
	if hits != 0 {
		t.Fatal("a redirect was followed by an injected client")
	}
}

// The real egress client over a real loopback server, end to end, under the dev policy; and the same
// server refused at the private-address check with the production policy.
func TestRealClientLoopback(t *testing.T) {
	k := mustKey(t, AlgEd25519, "k1")
	mux := http.NewServeMux()
	var issuer string
	mux.HandleFunc("/.well-known/aauth-agent.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write(jsonBytes(t, map[string]string{"issuer": issuer, "jwks_uri": issuer + "/.well-known/jwks.json"}))
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write(jsonBytes(t, JWKS{Keys: []JWK{k.PublicJWK()}}))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	issuer = srv.URL
	d := NewDiscovery(DiscoveryConfig{Policy: IdentifierPolicy{AllowInsecureLoopback: true}})
	got, err := d.Key(ctx, issuer, DwkAgent, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.SameKey(k.PublicJWK()) {
		t.Fatal("wrong key")
	}
	_, err = NewDiscovery(DiscoveryConfig{}).Key(ctx, issuer, DwkAgent, "k1")
	var de *DiscoveryError
	if !errors.As(err, &de) || !de.Blocked {
		t.Fatalf("production reached loopback: %v", err)
	}
}

// The client this package builds has the guard in place in production mode. The check on a hostname's
// resolved address happens at dial time inside safehttp, and is proven there by
// TestDialRefusesResolvedPrivate, with a resolver this package can't inject.
func TestEgressClientRefusesPrivateAddress(t *testing.T) {
	c := NewEgressClient(IdentifierPolicy{}, EgressOptions{Timeout: 2 * time.Second})
	if c.CheckRedirect == nil {
		t.Fatal("redirects unguarded")
	}
	req, _ := http.NewRequest("GET", "https://127.0.0.1:1/x", nil)
	_, err := c.Do(req)
	if !safehttp.IsBlocked(err) {
		t.Fatalf("want blocked, got %v", err)
	}
}

// "Verifiers MUST implement cache limits."
func TestCacheIsBounded(t *testing.T) {
	net := newFakeNet()
	d := NewDiscovery(DiscoveryConfig{Client: &http.Client{Transport: net}, MaxIssuers: 3})
	for _, h := range []string{"a", "b", "c", "d", "e"} {
		_, _ = d.Key(ctx, "https://"+h+".example", DwkAgent, "k")
	}
	if n := d.lru.Len(); n != 3 {
		t.Fatalf("entries %d", n)
	}
	if len(d.entries) != 3 {
		t.Fatalf("map %d", len(d.entries))
	}
}

// Concurrent callers on a cold entry make one fetch.
func TestConcurrentFetchesCollapse(t *testing.T) {
	w, _ := discoveryWorld(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.disc.Key(ctx, testIssuer, DwkAgent, "k1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := w.net.count(testJWKSURL); n != 1 {
		t.Fatalf("fetches %d", n)
	}
}

// The typed reads validate what they fetch.
func TestTypedMetadata(t *testing.T) {
	w := newWorld(t)
	pm := PersonServerMetadata{
		CommonMetadata:      CommonMetadata{Issuer: testPS, JWKSURI: testPS + "/.well-known/jwks.json", Name: "Example"},
		AuthTokenEndpoint:   testPS + "/token",
		PersonTokenEndpoint: testPS + "/person",
		MissionEndpoint:     testPS + "/mission",
		RevocationEndpoint:  testPS + "/revoke",
	}
	w.net.set(MetadataURL(testPS, DwkPerson), fakeResponse{body: jsonBytes(t, pm)})
	w.net.set(testPS+"/.well-known/jwks.json", fakeResponse{body: []byte(`{"keys":[]}`)})
	got, err := w.disc.PersonServer(ctx, testPS)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthTokenEndpoint != pm.AuthTokenEndpoint || got.Name != "Example" {
		t.Fatalf("%+v", got)
	}
	// A document missing a required member is refused by the typed read.
	w.net.set(MetadataURL(testAS, DwkAccess), fakeResponse{body: []byte(`{"issuer":"https://as.example","jwks_uri":"https://as.example/j"}`)})
	w.net.set("https://as.example/j", fakeResponse{body: []byte(`{"keys":[]}`)})
	if _, err := w.disc.AccessServer(ctx, testAS); err == nil {
		t.Error("a missing auth_token_endpoint passed")
	}
	// An SS with no jwks_uri is a plain JSON responder and is valid.
	ss := SupervisorMetadata{
		CommonMetadata:   CommonMetadata{Issuer: testSS},
		ConnectEndpoint:  testSS + "/connect",
		MissionEndpoint:  testSS + "/mission",
		DecisionEndpoint: testSS + "/decision",
		EventEndpoint:    testSS + "/event",
	}
	w.net.set(MetadataURL(testSS, DwkSupervisor), fakeResponse{body: jsonBytes(t, ss)})
	sm, err := w.disc.Supervisor(ctx, testSS)
	if err != nil {
		t.Fatal(err)
	}
	if sm.JWKSURI != "" || sm.DecisionEndpoint != testSS+"/decision" {
		t.Fatalf("%+v", sm)
	}
	w.net.set(MetadataURL(testResource, DwkResource), fakeResponse{body: []byte(`{"issuer":"https://resource.example","access_mode":"future-mode","budget_units":[{"unit":"USD","decimals":6,"max":10000000}],"usage_endpoint":"https://resource.example/usage"}`)})
	rm, err := w.disc.Resource(ctx, testResource)
	if err != nil {
		t.Fatal(err)
	}
	if rm.AccessMode != "future-mode" || *rm.BudgetUnits[0].Max != 10000000 {
		t.Fatalf("%+v", rm)
	}
	w.net.set(MetadataURL(testAP, DwkAgent), fakeResponse{body: []byte(`{"issuer":"https://agent.example","jwks_uri":"https://agent.example/j","callback_endpoint":"https://agent.example/cb","future":{"a":1}}`)})
	w.net.set("https://agent.example/j", fakeResponse{body: []byte(`{"keys":[]}`)})
	am, err := w.disc.AgentProvider(ctx, testAP)
	if err != nil {
		t.Fatal(err)
	}
	if am.CallbackEndpoint != "https://agent.example/cb" {
		t.Fatalf("%+v", am)
	}
}
