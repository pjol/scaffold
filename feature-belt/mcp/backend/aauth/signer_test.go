package aauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// protocol, Keying Material, and the Revocation Request example.
func TestServerSignerWireFormat(t *testing.T) {
	p := newParties(t)
	signer := NewServerSigner(testPS, DwkPerson, p.psKey, p.clock.Now)
	body := []byte(`{"jti":"unique-token-identifier","exp":1788727813}`)
	out, _ := http.NewRequest("POST", testResource+"/revoke", nil)
	out.Header.Set("Content-Type", ContentTypeJSON)
	if err := signer.SignRequest(out, body); err != nil {
		t.Fatal(err)
	}
	if got := out.Header.Get(HeaderSignatureKey); got != `sig=jwks_uri;id="https://ps.example";dwk="aauth-person.json";kid="ps-1"` {
		t.Errorf("Signature-Key %s", got)
	}
	want := `sig=("@method" "@authority" "@path" "content-type" "content-digest" "signature-key");created=` + itoa(p.clock.Now().Unix())
	if got := out.Header.Get(HeaderSignatureInput); got != want {
		t.Errorf("Signature-Input %s", got)
	}
	if got := out.Header.Get(HeaderContentDigest); got != ContentDigest(body) {
		t.Errorf("Content-Digest %s", got)
	}
	if out.ContentLength != int64(len(body)) || out.GetBody == nil {
		t.Errorf("body not attached: %d", out.ContentLength)
	}
	if !strings.HasPrefix(out.Header.Get(HeaderSignature), "sig=:") {
		t.Errorf("Signature %s", out.Header.Get(HeaderSignature))
	}
	// A body must have a covered Content-Type.
	bare, _ := http.NewRequest("POST", testResource+"/revoke", nil)
	if err := signer.SignRequest(bare, body); err == nil {
		t.Error("a body with no Content-Type was signed")
	}
	// Extra covered fields.
	g, _ := http.NewRequest("GET", testResource+"/api", nil)
	g.Header.Set("Authorization", "AAuth abc")
	if err := NewAgentSigner("a.b.c", p.agentKey, p.clock.Now).SignRequest(g, nil, "authorization"); err != nil {
		t.Fatal(err)
	}
	if got := g.Header.Get(HeaderSignatureInput); !strings.HasPrefix(got, `sig=("@method" "@authority" "@path" "authorization" "signature-key")`) {
		t.Errorf("Signature-Input %s", got)
	}
	// "agents MUST NOT include the alg signature parameter", and they SHOULD NOT include keyid.
	if strings.Contains(g.Header.Get(HeaderSignatureInput), "alg") || strings.Contains(g.Header.Get(HeaderSignatureInput), "keyid") {
		t.Errorf("Signature-Input %s", g.Header.Get(HeaderSignatureInput))
	}
}

func decisionRequest(t *testing.T, p *parties, body []byte) (*RequestMessage, *http.Request) {
	t.Helper()
	signer := NewServerSigner(testPS, DwkPerson, p.psKey, p.clock.Now)
	out, _ := http.NewRequest("POST", testSS+"/decision", nil)
	out.Header.Set("Content-Type", ContentTypeJSON)
	out.Header.Set(HeaderAcceptSignature, AcceptSignatureForDecision(true))
	if err := signer.SignRequest(out, body); err != nil {
		t.Fatal(err)
	}
	in := toServer(out, body)
	msg := NewRequestMessage(in)
	return &msg, out
}

// supervision, Signed Decisions, in both directions.
func TestSignedDecisionRoundTrip(t *testing.T) {
	p := newParties(t)
	reqBody := []byte(`{"sdi":"d-4d2a91"}`)
	serverMsg, clientReq := decisionRequest(t, p, reqBody)
	respBody := []byte(`{"sdi":"d-4d2a91","decision":"allow","scope":"calendar.read"}`)
	ss := NewServerSigner(testSS, DwkSupervisor, p.ssKey, p.clock.Now)
	signed, err := ss.SignResponse(serverMsg, 200, http.Header{"Content-Type": {ContentTypeJSON}}, respBody)
	if err != nil {
		t.Fatal(err)
	}
	// The wire: the exact fields of the draft's example.
	if got := signed.Header.Get(HeaderSignatureKey); got != `sig=jwks_uri;id="https://ss.example";dwk="aauth-supervisor.json";kid="ss-1"` {
		t.Errorf("Signature-Key %s", got)
	}
	want := `sig=("@status" "content-type" "content-digest" "content-digest";req "@authority";req "@path";req);created=` + itoa(p.clock.Now().Unix()) + `;keyid="ss-1"`
	if got := signed.Header.Get(HeaderSignatureInput); got != want {
		t.Errorf("Signature-Input:\n got %s\nwant %s", got, want)
	}
	if got := signed.Header.Get(HeaderContentDigest); got != ContentDigest(respBody) {
		t.Errorf("Content-Digest %s", got)
	}

	// The PS verifies against the request as it signed it. Its own view of the request is the client
	// request; the authority and path agree with what the server saw.
	clientMsg := NewRequestMessage(clientReq)
	resp := &ResponseMessage{Status: signed.Status, Header: signed.Header}
	opts := ResponseVerifyOptions{Keys: p.disc, Issuer: testSS, Dwk: DwkSupervisor}
	got, err := VerifySignedResponse(ctx, &clientMsg, resp, signed.Body, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != testSS || got.Kid != "ss-1" || got.Alg != AlgEd25519 || got.Created != p.clock.Now().Unix() {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.SignatureBase, `"content-digest";req: `+ContentDigest(reqBody)) || !strings.Contains(got.SignatureBase, `"@authority";req: ss.example`) || !strings.Contains(got.SignatureBase, `"@path";req: /decision`) {
		t.Errorf("base:\n%s", got.SignatureBase)
	}
	// The stored response replays through a real ResponseWriter byte for byte.
	w := httptest.NewRecorder()
	signed.WriteTo(w)
	if w.Code != 200 || w.Body.String() != string(respBody) || w.Header().Get(HeaderSignature) != signed.Header.Get(HeaderSignature) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	// It survives storage as JSON: "the same signed response".
	stored, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	var back SignedResponse
	if err := json.Unmarshal(stored, &back); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySignedResponse(ctx, &clientMsg, &ResponseMessage{Status: back.Status, Header: back.Header}, back.Body, opts); err != nil {
		t.Fatalf("after storage: %v", err)
	}
	// "A repeated supervision request ... the same signed response": an honest old `created` passes,
	// long after the window.
	p.clock.Advance(1000 * 3600 * 1e9)
	if _, err := VerifySignedResponse(ctx, &clientMsg, resp, signed.Body, opts); err != nil {
		t.Fatalf("after time passed: %v", err)
	}
	strict := opts
	strict.Clock, strict.MaxAge = p.clock.Now, 60*1e9
	_, err = VerifySignedResponse(ctx, &clientMsg, resp, signed.Body, strict)
	wantFailure(t, err, SigInvalidSignature)
}

func TestSignedDecisionNegatives(t *testing.T) {
	p := newParties(t)
	reqBody := []byte(`{"sdi":"d-1"}`)
	serverMsg, clientReq := decisionRequest(t, p, reqBody)
	clientMsg := NewRequestMessage(clientReq)
	respBody := []byte(`{"sdi":"d-1","decision":"allow"}`)
	ss := NewServerSigner(testSS, DwkSupervisor, p.ssKey, p.clock.Now)
	signed, err := ss.SignResponse(serverMsg, 200, nil, respBody)
	if err != nil {
		t.Fatal(err)
	}
	if signed.Header.Get("Content-Type") != ContentTypeJSON {
		t.Errorf("default Content-Type %q", signed.Header.Get("Content-Type"))
	}
	opts := ResponseVerifyOptions{Keys: p.disc, Issuer: testSS, Dwk: DwkSupervisor}
	verify := func(status int, h http.Header, body []byte, o ResponseVerifyOptions) error {
		_, err := VerifySignedResponse(ctx, &clientMsg, &ResponseMessage{Status: status, Header: h}, body, o)
		return err
	}
	if err := verify(200, signed.Header, respBody, opts); err != nil {
		t.Fatal(err)
	}
	// Body tampered: the digest no longer matches.
	wantFailure(t, verify(200, signed.Header, []byte(`{"sdi":"d-1","decision":"deny"}`), opts), SigInvalidSignature)
	// Body and digest both replaced: the signature covers the digest.
	h := signed.Header.Clone()
	evil := []byte(`{"sdi":"d-1","decision":"allow","scope":"everything"}`)
	h.Set(HeaderContentDigest, ContentDigest(evil))
	wantFailure(t, verify(200, h, evil, opts), SigInvalidSignature)
	// Status changed.
	wantFailure(t, verify(201, signed.Header, respBody, opts), SigInvalidSignature)
	// "MUST verify that the id in the response's Signature-Key is the issuer of the SS it called."
	other := opts
	other.Issuer = "https://other.example"
	wantFailure(t, verify(200, signed.Header, respBody, other), SigInvalidSignature)
	// A wrong metadata document.
	dwk := opts
	dwk.Dwk = DwkResource
	wantFailure(t, verify(200, signed.Header, respBody, dwk), SigInvalidKey)
	// Signed by someone else with the SS's kid.
	imp := NewServerSigner(testSS, DwkSupervisor, mustKey(t, AlgEd25519, "ss-1"), p.clock.Now)
	forged, err := imp.SignResponse(serverMsg, 200, nil, respBody)
	if err != nil {
		t.Fatal(err)
	}
	wantFailure(t, verify(200, forged.Header, respBody, opts), SigInvalidSignature)
	// Bound to the request: the same response doesn't verify against another one. "which bind a
	// synchronous decision to the request it answers".
	_, otherReq := decisionRequest(t, p, []byte(`{"sdi":"d-2"}`))
	otherMsg := NewRequestMessage(otherReq)
	if _, err := VerifySignedResponse(ctx, &otherMsg, &ResponseMessage{Status: 200, Header: signed.Header}, respBody, opts); err == nil {
		t.Error("a decision verified against another request")
	}
	// Unsigned.
	if HasResponseSignature(http.Header{"Content-Type": {ContentTypeJSON}}) {
		t.Error("unsigned read as signed")
	}
	if !HasResponseSignature(signed.Header) {
		t.Error("signed read as unsigned")
	}
	wantFailure(t, verify(200, http.Header{}, respBody, opts), SigInvalidSignature)
	// Coverage: the MUST set is enforced, whatever the signer chose.
	thin, err := ss.SignResponse(serverMsg, 200, nil, respBody, Comp("content-digest"))
	if err != nil {
		t.Fatal(err)
	}
	f := wantFailure(t, verify(200, thin.Header, respBody, opts), SigInvalidInput)
	if !containsString(f.RequiredInput, "@status") {
		t.Errorf("required_input %v", f.RequiredInput)
	}
	// An extra requirement.
	req := opts
	req.Required = []Component{ReqComp("@method")}
	wantFailure(t, verify(200, signed.Header, respBody, req), SigInvalidInput)
	// keyid must agree with kid.
	kh := signed.Header.Clone()
	kh.Set(HeaderSignatureInput, strings.Replace(kh.Get(HeaderSignatureInput), `keyid="ss-1"`, `keyid="other"`, 1))
	wantFailure(t, verify(200, kh, respBody, opts), SigInvalidSignature)
	// Unknown kid at the issuer.
	uk := NewServerSigner(testSS, DwkSupervisor, mustKey(t, AlgEd25519, "gone"), p.clock.Now)
	gone, err := uk.SignResponse(serverMsg, 200, nil, respBody)
	if err != nil {
		t.Fatal(err)
	}
	wantFailure(t, verify(200, gone.Header, respBody, opts), SigUnknownKey)
	// An agent can't sign a response under jwks_uri.
	if _, err := NewAgentSigner("a.b.c", p.agentKey, p.clock.Now).SignResponse(serverMsg, 200, nil, respBody); err == nil {
		t.Error("an agent signed a response")
	}
	// Options are checked.
	if _, err := VerifySignedResponse(ctx, &clientMsg, &ResponseMessage{Status: 200, Header: signed.Header}, respBody, ResponseVerifyOptions{}); err == nil {
		t.Error("empty options accepted")
	}
}

// A decision at a pending URL answers the PS's GET, with no request digest to bind.
func TestSignedDecisionOnPoll(t *testing.T) {
	p := newParties(t)
	signer := NewServerSigner(testPS, DwkPerson, p.psKey, p.clock.Now)
	out, _ := http.NewRequest("GET", testSS+"/pending/abc", nil)
	if err := signer.SignRequest(out, nil); err != nil {
		t.Fatal(err)
	}
	in := toServer(out, nil)
	serverMsg := NewRequestMessage(in)
	ss := NewServerSigner(testSS, DwkSupervisor, p.ssKey, p.clock.Now)
	body := []byte(`{"sdi":"d-1","decision":"deny"}`)
	signed, err := ss.SignResponse(&serverMsg, 200, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(signed.Header.Get(HeaderSignatureInput), `"content-digest";req`) {
		t.Errorf("no request digest to bind: %s", signed.Header.Get(HeaderSignatureInput))
	}
	clientMsg := NewRequestMessage(out)
	v, err := VerifySignedResponse(ctx, &clientMsg, &ResponseMessage{Status: 200, Header: signed.Header}, body, ResponseVerifyOptions{Keys: p.disc, Issuer: testSS, Dwk: DwkSupervisor})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.SignatureBase, `"@path";req: /pending/abc`) {
		t.Errorf("base:\n%s", v.SignatureBase)
	}
}

// RFC 9421 section 5.2: "MUST include the same set of covered components".
func TestSignResponseHonoursAcceptSignature(t *testing.T) {
	p := newParties(t)
	ss := NewServerSigner(testSS, DwkSupervisor, p.ssKey, p.clock.Now)
	mk := func(accept string) *RequestMessage {
		r := httptest.NewRequest("POST", "/decision", nil)
		r.Host = "ss.example"
		r.Header.Set(HeaderAcceptSignature, accept)
		r.Header.Set(HeaderContentDigest, ContentDigest([]byte("x")))
		m := NewRequestMessage(r)
		return &m
	}
	signed, err := ss.SignResponse(mk(`sig=("@status" "content-type" "content-digest" "@method";req)`), 200, nil, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := signed.Header.Get(HeaderSignatureInput); !strings.HasPrefix(got, `sig=("@status" "content-type" "content-digest" "@method";req)`) {
		t.Errorf("Signature-Input %s", got)
	}
	// A request that omits the MUST set, or can't be applied, falls back to the decision set.
	for _, accept := range []string{
		`sig=("content-digest")`,
		`sig=("@status" "content-type" "content-digest" "x-absent";req)`,
		`other=("@status" "content-type" "content-digest")`,
		`not a ( dictionary`,
	} {
		signed, err := ss.SignResponse(mk(accept), 200, nil, []byte(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if got := signed.Header.Get(HeaderSignatureInput); !strings.Contains(got, `"content-digest";req "@authority";req "@path";req`) {
			t.Errorf("%s: %s", accept, got)
		}
	}
}

func TestLocalKeys(t *testing.T) {
	p := newParties(t)
	calls := 0
	local := &LocalKeys{
		Issuer: testPS,
		Keys: func(context.Context) ([]JWK, error) {
			calls++
			return []JWK{p.psKey.PublicJWK()}, nil
		},
		Next: p.disc,
	}
	if k, err := local.Key(ctx, testPS, DwkPerson, "ps-1"); err != nil || !k.SameKey(p.psKey.PublicJWK()) {
		t.Fatalf("%v %v", k, err)
	}
	if p.net.count(MetadataURL(testPS, DwkPerson)) != 0 {
		t.Error("fetched its own metadata")
	}
	if _, err := local.Key(ctx, testPS, DwkPerson, "absent"); err == nil {
		t.Error("absent kid found")
	}
	if _, fetched, err := local.RefreshKey(ctx, testPS, DwkPerson, "ps-1"); fetched || err != nil {
		t.Errorf("fetched=%v err=%v", fetched, err)
	}
	// Other issuers go on to discovery.
	if k, err := local.Key(ctx, testAP, DwkAgent, "ap-1"); err != nil || !k.SameKey(p.apKey.PublicJWK()) {
		t.Fatalf("%v %v", k, err)
	}
	// A store failure is an unknown key with a cause, never a pass.
	bad := &LocalKeys{Issuer: testPS, Keys: func(context.Context) ([]JWK, error) { return nil, errors.New("db") }}
	var de *DiscoveryError
	if _, err := bad.Key(ctx, testPS, DwkPerson, "ps-1"); !errors.As(err, &de) || de.Code != SigUnknownKey {
		t.Fatalf("%v", err)
	}
	// A verifier built on it verifies this PS's own token with no fetch.
	v := &TokenVerifier{Keys: local, Clock: p.clock.Now}
	raw, _ := p.personToken(t)
	if _, err := v.VerifyPersonToken(ctx, raw, PersonTokenOptions{Audience: testResource}); err != nil {
		t.Fatal(err)
	}
}

// "roles hosted under a shared origin share one identifier and are distinguished by the
// well-known document": a role's local keys answer only for its own document.
func TestLocalKeysLimitedToOneDocument(t *testing.T) {
	p := newParties(t)
	other := map[string]bool{}
	next := keyResolverFunc(func(iss, dwk string) (JWK, error) {
		other[iss+" "+dwk] = true
		return p.apKey.PublicJWK(), nil
	})
	local := &LocalKeys{
		Issuer: testPS,
		Dwk:    DwkPerson,
		Keys:   func(context.Context) ([]JWK, error) { return []JWK{p.psKey.PublicJWK()}, nil },
		Next:   next,
	}
	if k, err := local.Key(ctx, testPS, DwkPerson, "ps-1"); err != nil || !k.SameKey(p.psKey.PublicJWK()) {
		t.Fatalf("own document: %v %v", k, err)
	}
	if len(other) != 0 {
		t.Errorf("its own document went on: %v", other)
	}
	// A role co-hosted under the same identifier goes on to discovery, for both calls.
	if k, err := local.Key(ctx, testPS, DwkSupervisor, "ss-1"); err != nil || !k.SameKey(p.apKey.PublicJWK()) {
		t.Fatalf("co-hosted role: %v %v", k, err)
	}
	if !other[testPS+" "+DwkSupervisor] {
		t.Errorf("Key didn't go on: %v", other)
	}
	if _, fetched, err := local.RefreshKey(ctx, testPS, DwkSupervisor, "ss-1"); err != nil || !fetched {
		t.Errorf("RefreshKey didn't go on: fetched=%v err=%v", fetched, err)
	}
}

type keyResolverFunc func(iss, dwk string) (JWK, error)

func (f keyResolverFunc) Key(_ context.Context, iss, dwk, _ string) (JWK, error) { return f(iss, dwk) }

func (f keyResolverFunc) RefreshKey(_ context.Context, iss, dwk, _ string) (JWK, bool, error) {
	k, err := f(iss, dwk)
	return k, true, err
}

func TestMarshalJWKS(t *testing.T) {
	a, b := mustKey(t, AlgEd25519, "a"), mustKey(t, AlgES256, "b")
	raw, err := MarshalJWKS(a.PublicJWK(), b.PublicJWK())
	if err != nil {
		t.Fatal(err)
	}
	if k, found, err := SelectJWK(raw, "b"); err != nil || !found || k.Alg != "ES256" {
		t.Fatalf("%+v %v %v", k, found, err)
	}
	if strings.Contains(string(raw), `"d"`) {
		t.Fatal("private material published")
	}
	noAlg := a.PublicJWK()
	noAlg.Alg = ""
	if _, err := MarshalJWKS(noAlg); err == nil {
		t.Error("a key with no alg was published")
	}
	noKid := a.PublicJWK()
	noKid.Kid = ""
	if _, err := MarshalJWKS(noKid); err == nil {
		t.Error("a key with no kid was published")
	}
	if raw, err := MarshalJWKS(); err != nil || string(raw) != `{"keys":[]}` {
		t.Errorf("%s %v", raw, err)
	}
}
