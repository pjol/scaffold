package aauth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var ctx = context.Background()

func TestIssuerRefusesInvalidTokens(t *testing.T) {
	p := newParties(t)
	issue := TokenIssuer{Key: p.apKey}
	cases := map[string]func(c *AgentClaims){
		"bad iss":       func(c *AgentClaims) { c.Iss = "http://agent.example" },
		"no jti":        func(c *AgentClaims) { c.Jti = "" },
		"no iat":        func(c *AgentClaims) { c.Iat = 0 },
		"exp not later": func(c *AgentClaims) { c.Exp = c.Iat },
		"over 24h":      func(c *AgentClaims) { c.Exp = c.Iat + 24*3600 + 1 },
		"bad sub":       func(c *AgentClaims) { c.Sub = "aauth:bad agent@agent.example" },
		"plus sub":      func(c *AgentClaims) { c.Sub = "aauth:a+b@agent.example" },
		"foreign sub":   func(c *AgentClaims) { c.Sub = "aauth:a@other.example" },
		"no cnf":        func(c *AgentClaims) { c.Cnf = nil },
		"bad cnf":       func(c *AgentClaims) { c.Cnf = &Confirmation{JWK: JWK{Kty: "OKP", Crv: "Ed25519", X: "abc"}} },
		"bad ps":        func(c *AgentClaims) { c.PS = "https://ps.example/path" },
		"bad parent":    func(c *AgentClaims) { c.ParentAgent = "aauth:x@other.example" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := p.agentClaims()
			mutate(&c)
			if _, err := issue.Agent(c); err == nil {
				t.Fatal("issued")
			}
		})
	}
	c := p.personClaims()
	c.Exp = c.Iat + 3601
	if _, err := (TokenIssuer{Key: p.psKey}).Person(c); err == nil {
		t.Error("a person token over one hour was issued")
	}
	c = p.personClaims()
	c.Extra = map[string]json.RawMessage{"scope": json.RawMessage(`"x"`)}
	if _, err := (TokenIssuer{Key: p.psKey}).Person(c); err == nil {
		t.Error("a person token with scope was issued")
	}
	a := p.authClaims()
	a.Exp = a.Iat + 3601
	if _, err := (TokenIssuer{Key: p.psKey}).Auth(a); err == nil {
		t.Error("an auth token over one hour was issued")
	}
	a = p.authClaims()
	a.PS = testAS
	if _, err := (TokenIssuer{Key: p.psKey}).Auth(a); err == nil {
		t.Error("a PS-issued auth token with ps other than iss was issued")
	}
	a = p.authClaims()
	a.Dwk = ""
	if _, err := (TokenIssuer{Key: p.psKey}).Auth(a); err == nil {
		t.Error("an auth token with no dwk was issued")
	}
	_, pc := p.personToken(t)
	rc := p.resourceClaims(pc)
	rc.Exp = rc.Iat + 301
	if _, err := (TokenIssuer{Key: p.resourceKey}).Resource(rc); err == nil {
		t.Error("a resource token over five minutes was issued")
	}
	rc = p.resourceClaims(pc)
	rc.BudgetConsumed = &BudgetConsumed{Jti: "x", Consumed: 1}
	if _, err := (TokenIssuer{Key: p.resourceKey}).Resource(rc); err == nil {
		t.Error("budget_consumed with no budget was issued")
	}
	rc = p.resourceClaims(pc)
	rc.R3URI = "https://resource.example/r3"
	if _, err := (TokenIssuer{Key: p.resourceKey}).Resource(rc); err == nil {
		t.Error("r3_uri with no r3_s256 was issued")
	}
}

func TestAgentTokenRoundTrip(t *testing.T) {
	p := newParties(t)
	c := p.agentClaims()
	c.Extra = map[string]json.RawMessage{"software_attestation": json.RawMessage(`{"level":"high"}`)}
	raw, err := TokenIssuer{Key: p.apKey}.Agent(c)
	if err != nil {
		t.Fatal(err)
	}
	// The wire: exact header and member names.
	jwt, _ := ParseJWT(raw)
	if jwt.Header.Typ != "aa-agent+jwt" || jwt.Header.Alg != "Ed25519" || jwt.Header.Kid != "ap-1" {
		t.Fatalf("%+v", jwt.Header)
	}
	var payload map[string]any
	_ = json.Unmarshal(jwt.Payload, &payload)
	for _, m := range []string{"iss", "dwk", "jti", "iat", "exp", "sub", "cnf", "ps", "software_attestation"} {
		if _, ok := payload[m]; !ok {
			t.Errorf("payload lacks %s: %v", m, payload)
		}
	}
	if payload["dwk"] != "aauth-agent.json" {
		t.Errorf("dwk %v", payload["dwk"])
	}
	got, err := p.tokens().VerifyAgentToken(ctx, raw, AgentTokenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims.Sub != testAgentSub || got.Claims.PS != testPS {
		t.Fatalf("%+v", got.Claims)
	}
	// Unknown claims are ignored for verification and preserved for reading.
	if string(got.Claims.Extra["software_attestation"]) != `{"level":"high"}` {
		t.Errorf("extra claim: %s", got.Claims.Extra)
	}
	tp, _ := p.agentKey.PublicJWK().Thumbprint()
	if got.Thumbprint != tp || !got.Key.SameKey(p.agentKey.PublicJWK()) {
		t.Errorf("key %+v", got)
	}
	// SigningKey, when given, must match.
	other := mustKey(t, AlgEd25519, "x").PublicJWK()
	_, err = p.tokens().VerifyAgentToken(ctx, raw, AgentTokenOptions{SigningKey: &other})
	wantTokenSig(t, err, SigInvalidJWT)
	mine := p.agentKey.PublicJWK()
	if _, err := p.tokens().VerifyAgentToken(ctx, raw, AgentTokenOptions{SigningKey: &mine}); err != nil {
		t.Error(err)
	}
}

// The negative cases of Common JWT Verification, one mutation at a time.
func TestCommonVerificationNegatives(t *testing.T) {
	p := newParties(t)
	v := p.tokens()
	now := p.clock.Now().Unix()
	goodHeader := func() map[string]any { return map[string]any{"alg": "Ed25519", "typ": TypAgent, "kid": "ap-1"} }
	goodPayload := func() map[string]any {
		return map[string]any{
			"iss": testAP, "dwk": DwkAgent, "sub": testAgentSub, "jti": "j1",
			"iat": now, "exp": now + 3600, "cnf": cnfMap(p.agentKey.PublicJWK()),
		}
	}
	if _, err := v.VerifyAgentToken(ctx, signRawJWT(t, p.apKey, goodHeader(), goodPayload()), AgentTokenOptions{}); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	cases := []struct {
		name    string
		header  func(h map[string]any)
		payload func(c map[string]any)
		code    SignatureErrorCode
	}{
		{"wrong typ", func(h map[string]any) { h["typ"] = TypPerson }, nil, SigInvalidJWT},
		{"no typ", func(h map[string]any) { delete(h, "typ") }, nil, SigInvalidJWT},
		{"EdDSA alg", func(h map[string]any) { h["alg"] = "EdDSA" }, nil, SigInvalidJWT},
		{"none alg", func(h map[string]any) { h["alg"] = "none" }, nil, SigInvalidJWT},
		{"HS256 alg", func(h map[string]any) { h["alg"] = "HS256" }, nil, SigInvalidJWT},
		{"missing alg", func(h map[string]any) { delete(h, "alg") }, nil, SigInvalidJWT},
		{"alg disagrees with key", func(h map[string]any) { h["alg"] = "ES256" }, nil, SigInvalidJWT},
		{"no kid", func(h map[string]any) { delete(h, "kid") }, nil, SigInvalidJWT},
		{"unknown kid", func(h map[string]any) { h["kid"] = "nope" }, nil, SigUnknownKey},
		{"wrong dwk", nil, func(c map[string]any) { c["dwk"] = DwkPerson }, SigInvalidJWT},
		{"no dwk", nil, func(c map[string]any) { delete(c, "dwk") }, SigInvalidJWT},
		{"dwk traversal", nil, func(c map[string]any) { c["dwk"] = "../evil.json" }, SigInvalidJWT},
		{"bad iss", nil, func(c map[string]any) { c["iss"] = "http://agent.example" }, SigInvalidJWT},
		{"iss with port", nil, func(c map[string]any) { c["iss"] = "https://agent.example:8443" }, SigInvalidJWT},
		{"no iss", nil, func(c map[string]any) { delete(c, "iss") }, SigInvalidJWT},
		{"no jti", nil, func(c map[string]any) { delete(c, "jti") }, SigInvalidJWT},
		{"no iat", nil, func(c map[string]any) { delete(c, "iat") }, SigInvalidJWT},
		{"no exp", nil, func(c map[string]any) { delete(c, "exp") }, SigInvalidJWT},
		{"string exp", nil, func(c map[string]any) { c["exp"] = "soon" }, SigInvalidJWT},
		{"expired", nil, func(c map[string]any) { c["exp"] = now }, SigExpiredJWT},
		{"expired one second ago", nil, func(c map[string]any) { c["exp"] = now - 1 }, SigExpiredJWT},
		{"future iat", nil, func(c map[string]any) { c["iat"] = now + 61 }, SigClockSkew},
		{"no sub", nil, func(c map[string]any) { delete(c, "sub") }, SigInvalidJWT},
		{"foreign sub", nil, func(c map[string]any) { c["sub"] = "aauth:a@other.example" }, SigInvalidJWT},
		{"bad ps", nil, func(c map[string]any) { c["ps"] = "http://ps.example" }, SigInvalidJWT},
		{"bad parent_agent", nil, func(c map[string]any) { c["parent_agent"] = "nope" }, SigInvalidJWT},
		{"no cnf", nil, func(c map[string]any) { delete(c, "cnf") }, SigInvalidJWT},
		{"empty cnf", nil, func(c map[string]any) { c["cnf"] = map[string]any{} }, SigInvalidJWT},
		{"cnf not an object", nil, func(c map[string]any) { c["cnf"] = "x" }, SigInvalidKey},
		{"cnf.jwk not an object", nil, func(c map[string]any) { c["cnf"] = map[string]any{"jwk": "x"} }, SigInvalidKey},
		{"cnf.jwk x wrong type", nil, func(c map[string]any) {
			c["cnf"] = map[string]any{"jwk": map[string]any{"kty": "OKP", "crv": "Ed25519", "x": 5, "alg": "Ed25519"}}
		}, SigInvalidKey},
		{"cnf.jwk no kty", nil, func(c map[string]any) { c["cnf"] = cnfMap(JWK{Crv: "Ed25519", X: "a", Alg: "Ed25519"}) }, SigInvalidJWT},
		{"cnf.jwk no x", nil, func(c map[string]any) { c["cnf"] = cnfMap(JWK{Kty: "OKP", Crv: "Ed25519", Alg: "Ed25519"}) }, SigInvalidJWT},
		{"cnf.jwk EC no y", nil, func(c map[string]any) { c["cnf"] = cnfMap(JWK{Kty: "EC", Crv: "P-256", X: "a", Alg: "ES256"}) }, SigInvalidJWT},
		{"cnf.jwk no alg", nil, func(c map[string]any) {
			k := p.agentKey.PublicJWK()
			k.Alg = ""
			c["cnf"] = cnfMap(k)
		}, SigUnsupportedAlgorithm},
		{"cnf.jwk EdDSA", nil, func(c map[string]any) {
			k := p.agentKey.PublicJWK()
			k.Alg = "EdDSA"
			c["cnf"] = cnfMap(k)
		}, SigUnsupportedAlgorithm},
		{"cnf.jwk kty/alg mismatch", nil, func(c map[string]any) {
			k := p.agentKey.PublicJWK()
			k.Alg = "ES256"
			c["cnf"] = cnfMap(k)
		}, SigInvalidKey},
		{"cnf.jwk crv mismatch", nil, func(c map[string]any) {
			k := p.agentKey.PublicJWK()
			k.Crv = "X25519"
			c["cnf"] = cnfMap(k)
		}, SigInvalidKey},
		{"cnf.jwk unusable x", nil, func(c map[string]any) { c["cnf"] = cnfMap(JWK{Kty: "OKP", Crv: "Ed25519", X: "AAAA", Alg: "Ed25519"}) }, SigInvalidKey},
		{"cnf.jwk symmetric", nil, func(c map[string]any) {
			c["cnf"] = map[string]any{"jwk": map[string]any{"kty": "oct", "k": "c2VjcmV0", "alg": "HS256"}}
		}, SigUnsupportedAlgorithm},
		{"cnf.jwk RSA", nil, func(c map[string]any) { c["cnf"] = cnfMap(JWK{Kty: "RSA", N: "a", E: "AQAB", Alg: "RS256"}) }, SigUnsupportedAlgorithm},
		{"cnf.jwk private material", nil, func(c map[string]any) {
			c["cnf"] = map[string]any{"jwk": map[string]any{"kty": "OKP", "crv": "Ed25519", "x": p.agentKey.PublicJWK().X, "alg": "Ed25519", "d": "x"}}
		}, SigInvalidKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, pl := goodHeader(), goodPayload()
			if c.header != nil {
				c.header(h)
			}
			if c.payload != nil {
				c.payload(pl)
			}
			_, err := v.VerifyAgentToken(ctx, signRawJWT(t, p.apKey, h, pl), AgentTokenOptions{})
			wantTokenSig(t, err, c.code)
		})
		// A clock tick so refreshes triggered by unknown kids don't interfere.
		p.clock.Advance(0)
	}
}

func TestVerificationBoundaries(t *testing.T) {
	p := newParties(t)
	v := p.tokens()
	// "This document defines no tolerance for clock skew on exp": valid one second before, not at.
	c := p.agentClaims()
	raw, _ := TokenIssuer{Key: p.apKey}.Agent(c)
	p.clock.Advance(time.Duration(c.Exp-c.Iat-1) * time.Second)
	if _, err := v.VerifyAgentToken(ctx, raw, AgentTokenOptions{}); err != nil {
		t.Fatalf("one second before exp: %v", err)
	}
	p.clock.Advance(time.Second)
	_, err := v.VerifyAgentToken(ctx, raw, AgentTokenOptions{})
	wantTokenSig(t, err, SigExpiredJWT)

	// iat exactly at the window is fine, one past is clock_skew, and the bound is optional.
	p2 := newParties(t)
	now := p2.clock.Now().Unix()
	c2 := p2.agentClaims()
	c2.Iat, c2.Exp = now+60, now+3600
	raw, _ = TokenIssuer{Key: p2.apKey}.Agent(c2)
	if _, err := p2.tokens().VerifyAgentToken(ctx, raw, AgentTokenOptions{}); err != nil {
		t.Fatalf("iat at the window: %v", err)
	}
	c2.Iat = now + 61
	raw, _ = TokenIssuer{Key: p2.apKey}.Agent(c2)
	_, err = p2.tokens().VerifyAgentToken(ctx, raw, AgentTokenOptions{})
	wantTokenSig(t, err, SigClockSkew)
	off := p2.tokens()
	off.IatWindow = -1
	if _, err := off.VerifyAgentToken(ctx, raw, AgentTokenOptions{}); err != nil {
		t.Fatalf("bound off: %v", err)
	}
}

// "expired_presented_token when only exp fails": a forged, expired token is invalid, not expired.
func TestSignatureCheckedBeforeExpiry(t *testing.T) {
	p := newParties(t)
	c := p.agentClaims()
	c.Exp = c.Iat - 1
	c.Iat = c.Exp - 10
	forged, err := SignJWT(mustKey(t, AlgEd25519, "ap-1"), TypAgent, c)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.tokens().VerifyAgentToken(ctx, forged, AgentTokenOptions{})
	wantTokenSig(t, err, SigInvalidJWT)
}

// Fractional NumericDate is legal in RFC 7519.
func TestFractionalNumericDate(t *testing.T) {
	p := newParties(t)
	now := float64(p.clock.Now().Unix())
	raw := signRawJWT(t, p.apKey, map[string]any{"alg": "Ed25519", "typ": TypAgent, "kid": "ap-1"}, map[string]any{
		"iss": testAP, "dwk": DwkAgent, "sub": testAgentSub, "jti": "j", "iat": now + 0.25, "exp": now + 100.5,
		"cnf": cnfMap(p.agentKey.PublicJWK()),
	})
	got, err := p.tokens().VerifyAgentToken(ctx, raw, AgentTokenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims.Exp != int64(now)+100 {
		t.Errorf("exp truncated to %d", got.Claims.Exp)
	}
}

func TestRevokedAgentToken(t *testing.T) {
	p := newParties(t)
	c := p.agentClaims()
	raw, _ := TokenIssuer{Key: p.apKey}.Agent(c)
	v := p.tokens()
	var asked [2]string
	v.Revocations = RevocationCheckerFunc(func(_ context.Context, iss, jti string) (bool, error) {
		asked = [2]string{iss, jti}
		return true, nil
	})
	_, err := v.VerifyAgentToken(ctx, raw, AgentTokenOptions{})
	wantTokenSig(t, err, SigRevokedJWT)
	if asked != [2]string{testAP, c.Jti} {
		t.Errorf("keyed by %v", asked)
	}
	// Not asked about until the token verified and is unexpired: an expired token stays expired.
	p.clock.Advance(2 * time.Hour)
	_, err = v.VerifyAgentToken(ctx, raw, AgentTokenOptions{})
	wantTokenSig(t, err, SigExpiredJWT)
	// A store outage is ours, not the token's.
	p2 := newParties(t)
	raw2 := p2.agentToken(t)
	v2 := p2.tokens()
	v2.Revocations = RevocationCheckerFunc(func(context.Context, string, string) (bool, error) {
		return false, errors.New("db down")
	})
	_, err = v2.VerifyAgentToken(ctx, raw2, AgentTokenOptions{})
	var te *TokenError
	if !errors.As(err, &te) || !te.Internal {
		t.Fatalf("want internal, got %v", err)
	}
}

func TestPersonTokenVerification(t *testing.T) {
	p := newParties(t)
	raw, c := p.personToken(t, func(c *PersonClaims) {
		c.MissionS256 = S256([]byte("blob"))
		c.Tenant = "acme.example"
	})
	v := p.tokens()
	key := p.agentKey.PublicJWK()
	got, err := v.VerifyPersonToken(ctx, raw, PersonTokenOptions{Audience: testResource, SigningKey: &key})
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims.Sub != c.Sub || got.Claims.MissionS256 != c.MissionS256 || got.Claims.Tenant != "acme.example" {
		t.Fatalf("%+v", got.Claims)
	}
	_, err = v.VerifyPersonToken(ctx, raw, PersonTokenOptions{Audience: "https://other.example"})
	wantTokenSig(t, err, SigInvalidJWT)
	_, err = v.VerifyPersonToken(ctx, raw, PersonTokenOptions{})
	wantTokenSig(t, err, SigInvalidJWT)
	other := mustKey(t, AlgEd25519, "o").PublicJWK()
	_, err = v.VerifyPersonToken(ctx, raw, PersonTokenOptions{Audience: testResource, SigningKey: &other})
	wantTokenSig(t, err, SigInvalidJWT)

	// "A recipient MUST reject an aa-person+jwt wherever an auth token is required."
	_, err = v.VerifyAuthToken(ctx, raw, AuthTokenOptions{Audience: testResource})
	wantTokenSig(t, err, SigInvalidJWT)

	// One-hour ceiling on exp minus iat, and no scope or account.
	now := p.clock.Now().Unix()
	base := map[string]any{"iss": testPS, "dwk": DwkPerson, "aud": testResource, "sub": "s", "jti": "j", "iat": now, "exp": now + 3600, "cnf": cnfMap(key)}
	header := map[string]any{"alg": "Ed25519", "typ": TypPerson, "kid": "ps-1"}
	mutate := func(k string, val any) string {
		m := map[string]any{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = val
		return signRawJWT(t, p.psKey, header, m)
	}
	if _, err := v.VerifyPersonToken(ctx, signRawJWT(t, p.psKey, header, base), PersonTokenOptions{Audience: testResource}); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	for name, tok := range map[string]string{
		"over one hour": mutate("exp", now+3601),
		"has scope":     mutate("scope", "a.b"),
		"has account":   mutate("account", "x"),
		"bad mission":   mutate("mission_s256", "short"),
		"no sub":        mutate("sub", ""),
	} {
		_, err := v.VerifyPersonToken(ctx, tok, PersonTokenOptions{Audience: testResource})
		if te := wantTokenSig(t, err, SigInvalidJWT); te == nil {
			t.Error(name)
		}
	}
}

func TestAuthTokenVerification(t *testing.T) {
	p := newParties(t)
	c := p.authClaims()
	raw := p.authToken(t, c)
	v := p.tokens()
	got, err := v.VerifyAuthToken(ctx, raw, AuthTokenOptions{Audience: testResource})
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims.PS != testPS || got.Claims.Scope != "data.read" {
		t.Fatalf("%+v", got.Claims)
	}
	// Issued by an AS: iss and ps differ, dwk is aauth-access.json.
	asc := p.authClaims()
	asc.Iss, asc.Dwk = testAS, DwkAccess
	as := p.authToken(t, asc)
	if _, err := v.VerifyAuthToken(ctx, as, AuthTokenOptions{Audience: testResource}); err != nil {
		t.Fatal(err)
	}
	// R3 and Budgets claims round-trip.
	r3 := p.authClaims()
	r3.R3URI, r3.R3S256 = "https://resource.example/r3/a", S256([]byte("doc"))
	r3.R3Granted = &R3Operations{Vocabulary: "urn:aauth:vocabulary:mcp", Operations: []json.RawMessage{json.RawMessage(`{"tool":"list"}`)}}
	r3.R3PerCall = &R3Operations{Vocabulary: "urn:aauth:vocabulary:mcp", Operations: []json.RawMessage{json.RawMessage(`{"tool":"create"}`)}}
	r3.Budget = &Budget{Amount: 2000000, Unit: "USD", Decimals: 6}
	r3.Extra = map[string]json.RawMessage{"email": json.RawMessage(`"a@b.example"`)}
	gotR3, err := v.VerifyAuthToken(ctx, p.authToken(t, r3), AuthTokenOptions{Audience: testResource})
	if err != nil {
		t.Fatal(err)
	}
	if gotR3.Claims.R3URI != r3.R3URI || gotR3.Claims.R3Granted == nil || string(gotR3.Claims.R3PerCall.Operations[0]) != `{"tool":"create"}` ||
		gotR3.Claims.Budget.Amount != 2000000 || string(gotR3.Claims.Extra["email"]) != `"a@b.example"` {
		t.Fatalf("%+v", gotR3.Claims)
	}
	// PS-103: "exp minus iat MUST NOT exceed" one hour, whoever signed it.
	now := p.clock.Now().Unix()
	tooLong := signRawJWT(t, p.psKey, map[string]any{"alg": "Ed25519", "typ": TypAuth, "kid": "ps-1"}, map[string]any{
		"iss": testPS, "dwk": DwkPerson, "aud": testResource, "ps": testPS, "sub": "s", "jti": "j", "iat": now, "exp": now + 3601,
		"cnf": cnfMap(p.agentKey.PublicJWK()),
	})
	_, err = v.VerifyAuthToken(ctx, tooLong, AuthTokenOptions{Audience: testResource})
	wantTokenSig(t, err, SigInvalidJWT)
	// A PS-issued auth token's ps must be its iss, even if its signature is good.
	badPS := signRawJWT(t, p.psKey, map[string]any{"alg": "Ed25519", "typ": TypAuth, "kid": "ps-1"}, map[string]any{
		"iss": testPS, "dwk": DwkPerson, "aud": testResource, "ps": testAS, "sub": "s", "jti": "j", "iat": now, "exp": now + 3600,
		"cnf": cnfMap(p.agentKey.PublicJWK()),
	})
	_, err = v.VerifyAuthToken(ctx, badPS, AuthTokenOptions{Audience: testResource})
	wantTokenSig(t, err, SigInvalidJWT)
}

// presentedSetup returns a resource token bound to a person token.
func presentedSetup(t *testing.T, mutate ...func(*PersonClaims)) (p *parties, personRaw string, pc PersonClaims) {
	t.Helper()
	p = newParties(t)
	personRaw, pc = p.personToken(t, mutate...)
	return p, personRaw, pc
}

// Resource Token Verification, step 3, including the two substitutions.
func TestVerifyTokenRequestHappyPath(t *testing.T) {
	p, personRaw, pc := presentedSetup(t, func(c *PersonClaims) {
		c.MissionS256 = S256([]byte("blob"))
		c.Tenant = "acme.example"
	})
	rawRT := p.resourceToken(t, p.resourceClaims(pc))
	tp, _ := p.agentKey.PublicJWK().Thumbprint()
	rt, presented, err := p.tokens().VerifyTokenRequest(ctx, TokenRequestInput{
		ResourceToken: rawRT, PresentedToken: personRaw,
		Audience: testPS, ExpectedPS: testPS, SigningThumbprint: tp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt.Claims.Sub != pc.Sub || presented.Typ() != TypPerson || presented.Jti() != pc.Jti || presented.PS() != testPS ||
		presented.Exp() != pc.Exp || presented.Tenant() != "acme.example" || presented.Aud() != testResource || presented.Iss() != testPS {
		t.Fatalf("%+v %+v", rt.Claims, presented)
	}

	// A presented auth token on a step-up, issued by the same PS: `ps` is compared, not `iss`.
	ac := p.authClaims()
	ac.MissionS256, ac.Tenant = pc.MissionS256, pc.Tenant
	rawAuth := p.authToken(t, ac)
	rc := p.resourceClaims(pc)
	rc.PresentedJti = ac.Jti
	_, presented, err = p.tokens().VerifyTokenRequest(ctx, TokenRequestInput{
		ResourceToken: p.resourceToken(t, rc), PresentedToken: rawAuth,
		Audience: testPS, ExpectedPS: testPS, SigningThumbprint: tp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if presented.Typ() != TypAuth || presented.Auth == nil {
		t.Fatalf("%+v", presented)
	}

	// Issued by an AS for four-party: iss differs from ps, and the compare is on ps.
	asc := p.authClaims()
	asc.Iss, asc.Dwk = testAS, DwkAccess
	asc.MissionS256, asc.Tenant = pc.MissionS256, pc.Tenant
	rc = p.resourceClaims(pc)
	rc.PresentedJti = asc.Jti
	if _, _, err := p.tokens().VerifyTokenRequest(ctx, TokenRequestInput{
		ResourceToken: p.resourceToken(t, rc), PresentedToken: p.authToken(t, asc),
		Audience: testPS, ExpectedPS: testPS, SigningThumbprint: tp,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestResourceTokenNegatives(t *testing.T) {
	p, personRaw, pc := presentedSetup(t)
	tp, _ := p.agentKey.PublicJWK().Thumbprint()
	input := func(rawRT string) TokenRequestInput {
		return TokenRequestInput{ResourceToken: rawRT, PresentedToken: personRaw, Audience: testPS, ExpectedPS: testPS, SigningThumbprint: tp}
	}
	cases := []struct {
		name   string
		mutate func(*ResourceClaims)
		code   ErrorCode
	}{
		{"aud is another party", func(c *ResourceClaims) { c.Aud = testAS }, CodeInvalidResourceToken},
		{"ps is another party", func(c *ResourceClaims) { c.PS = "https://other.example" }, CodeInvalidResourceToken},
		{"agent_jkt is another key", func(c *ResourceClaims) { c.AgentJKT = "other" }, CodeInvalidResourceToken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc := p.resourceClaims(pc)
			c.mutate(&rc)
			_, _, err := p.tokens().VerifyTokenRequest(ctx, input(p.resourceToken(t, rc)))
			wantTokenCode(t, err, c.code)
		})
	}
	// "invalid_budget... or the object is otherwise malformed" (budgets, Errors). TokenIssuer refuses to sign these.
	for name, b := range map[string]*Budget{
		"out of range": {Amount: MaxBudgetAmount + 1, Unit: "USD", Decimals: 6},
		"no unit":      {Amount: 1, Decimals: 6},
		"negative":     {Amount: -1, Unit: "USD", Decimals: 6},
	} {
		t.Run("a budget "+name, func(t *testing.T) {
			rc := p.resourceClaims(pc)
			rc.Dwk, rc.Budget = DwkResource, b
			raw, err := SignJWT(p.resourceKey, TypResource, rc)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = p.tokens().VerifyTokenRequest(ctx, input(raw))
			wantTokenCode(t, err, CodeInvalidBudget)
		})
	}
	// Wrong type altogether.
	_, _, err := p.tokens().VerifyTokenRequest(ctx, input(personRaw))
	wantTokenCode(t, err, CodeInvalidResourceToken)
	_, _, err = p.tokens().VerifyTokenRequest(ctx, input("not a jwt"))
	wantTokenCode(t, err, CodeInvalidResourceToken)

	// Expired resource token.
	rc := p.resourceClaims(pc)
	raw := p.resourceToken(t, rc)
	p.clock.Advance(6 * time.Minute)
	_, _, err = p.tokens().VerifyTokenRequest(ctx, input(raw))
	wantTokenCode(t, err, CodeExpiredResourceToken)

	// Revoked resource token.
	p2, personRaw2, pc2 := presentedSetup(t)
	tp2, _ := p2.agentKey.PublicJWK().Thumbprint()
	rc2 := p2.resourceClaims(pc2)
	v := p2.tokens()
	v.Revocations = RevocationCheckerFunc(func(_ context.Context, iss, jti string) (bool, error) {
		return iss == testResource && jti == rc2.Jti, nil
	})
	_, _, err = v.VerifyTokenRequest(ctx, TokenRequestInput{
		ResourceToken: p2.resourceToken(t, rc2), PresentedToken: personRaw2,
		Audience: testPS, ExpectedPS: testPS, SigningThumbprint: tp2,
	})
	wantTokenCode(t, err, CodeRevokedResourceToken)
}

func TestPresentedTokenNegatives(t *testing.T) {
	type setup struct {
		p   *parties
		in  TokenRequestInput
		pc  PersonClaims
		rc  ResourceClaims
		raw string
	}
	build := func(t *testing.T, mutatePerson func(*PersonClaims), mutateRes func(*ResourceClaims)) setup {
		p := newParties(t)
		mp := func(c *PersonClaims) {}
		if mutatePerson != nil {
			mp = mutatePerson
		}
		personRaw, pc := p.personToken(t, mp)
		rc := p.resourceClaims(pc)
		if mutateRes != nil {
			mutateRes(&rc)
		}
		tp, _ := p.agentKey.PublicJWK().Thumbprint()
		return setup{p: p, pc: pc, rc: rc, raw: personRaw, in: TokenRequestInput{
			ResourceToken: p.resourceToken(t, rc), PresentedToken: personRaw,
			Audience: testPS, ExpectedPS: testPS, SigningThumbprint: tp,
		}}
	}
	// A mismatch against a token that verifies: invalid_resource_token, and flagged as tampering.
	mismatches := map[string]func(*ResourceClaims){
		"presented_jti": func(c *ResourceClaims) { c.PresentedJti = "other" },
		"sub":           func(c *ResourceClaims) { c.Sub = "other" },
		"mission_s256":  func(c *ResourceClaims) { c.MissionS256 = S256([]byte("stripped")) },
		"tenant":        func(c *ResourceClaims) { c.Tenant = "other" },
	}
	for name, mutate := range mismatches {
		t.Run("mismatch "+name, func(t *testing.T) {
			s := build(t, nil, mutate)
			_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
			te := wantTokenCode(t, err, CodeInvalidResourceToken)
			if !te.Tampered {
				t.Error("not flagged as tampering")
			}
		})
	}
	// "any mismatch or omission": a mission on the presented token dropped from the resource token.
	t.Run("mission stripped", func(t *testing.T) {
		s := build(t, func(c *PersonClaims) { c.MissionS256 = S256([]byte("blob")) }, nil)
		s.rc.MissionS256 = ""
		s.in.ResourceToken = s.p.resourceToken(t, s.rc)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		te := wantTokenCode(t, err, CodeInvalidResourceToken)
		if !te.Tampered {
			t.Error("not flagged as tampering")
		}
	})
	t.Run("tenant stripped", func(t *testing.T) {
		s := build(t, func(c *PersonClaims) { c.Tenant = "acme.example" }, nil)
		s.rc.Tenant = ""
		s.in.ResourceToken = s.p.resourceToken(t, s.rc)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		te := wantTokenCode(t, err, CodeInvalidResourceToken)
		if !te.Tampered {
			t.Error("not flagged as tampering")
		}
	})
	// The substitutions: aud is compared with the resource token's iss, cnf with agent_jkt.
	t.Run("aud is not the resource token's iss", func(t *testing.T) {
		s := build(t, func(c *PersonClaims) { c.Aud = "https://other.example" }, nil)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidPresentedToken)
	})
	t.Run("cnf is not agent_jkt", func(t *testing.T) {
		other := mustKey(t, AlgEd25519, "other").PublicJWK()
		s := build(t, func(c *PersonClaims) { c.Cnf = ConfirmKey(other) }, nil)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidPresentedToken)
	})
	t.Run("wrong typ", func(t *testing.T) {
		s := build(t, nil, nil)
		s.in.PresentedToken = s.p.agentToken(t)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidPresentedToken)
	})
	t.Run("malformed", func(t *testing.T) {
		s := build(t, nil, nil)
		s.in.PresentedToken = "garbage"
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidPresentedToken)
	})
	t.Run("bad signature", func(t *testing.T) {
		s := build(t, nil, nil)
		forged, err := TokenIssuer{Key: mustKey(t, AlgEd25519, "ps-1")}.Person(s.pc)
		if err != nil {
			t.Fatal(err)
		}
		s.in.PresentedToken = forged
		_, _, err = s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidPresentedToken)
	})
	t.Run("expired", func(t *testing.T) {
		s := build(t, func(c *PersonClaims) { c.Exp = c.Iat + 120 }, nil)
		s.p.clock.Advance(3 * time.Minute)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeExpiredPresentedToken)
	})
	t.Run("expired and wrong aud is invalid", func(t *testing.T) {
		s := build(t, func(c *PersonClaims) {
			c.Exp = c.Iat + 120
			c.Aud = "https://other.example"
		}, nil)
		s.p.clock.Advance(3 * time.Minute)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidPresentedToken)
	})
	t.Run("revoked", func(t *testing.T) {
		s := build(t, nil, nil)
		v := s.p.tokens()
		v.Revocations = RevocationCheckerFunc(func(_ context.Context, iss, jti string) (bool, error) {
			return jti == s.pc.Jti, nil
		})
		_, _, err := v.VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeRevokedPresentedToken)
	})
	t.Run("clock skew", func(t *testing.T) {
		s := build(t, func(c *PersonClaims) {
			c.Iat += 120
			c.Exp = c.Iat + 600
		}, nil)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeClockSkew)
	})
	// An auth token on a step-up is held to its `ps`, as a person token is to its `iss`.
	t.Run("presented auth token from another ps", func(t *testing.T) {
		s := build(t, nil, nil)
		ac := s.p.authClaims()
		ac.Iss, ac.Dwk, ac.PS = testAS, DwkAccess, "https://other-ps.example"
		ac.MissionS256, ac.Tenant = s.pc.MissionS256, s.pc.Tenant
		s.rc.PresentedJti = ac.Jti
		s.in.ResourceToken = s.p.resourceToken(t, s.rc)
		s.in.PresentedToken = s.p.authToken(t, ac)
		_, _, err := s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidResourceToken)
	})
	t.Run("presented token from another ps", func(t *testing.T) {
		s := build(t, nil, nil)
		// Same claims signed by the AS's key under its own identity: iss differs from `ps`.
		s.p.publish("https://rogue.example", DwkPerson, s.p.asKey.PublicJWK())
		pc := s.pc
		pc.Iss = "https://rogue.example"
		raw, err := TokenIssuer{Key: s.p.asKey}.Person(pc)
		if err != nil {
			t.Fatal(err)
		}
		s.in.PresentedToken = raw
		_, _, err = s.p.tokens().VerifyTokenRequest(ctx, s.in)
		wantTokenCode(t, err, CodeInvalidResourceToken)
	})
}

// The parent-mediated rule: "verify it against the subagent_token's cnf.jwk instead".
func TestSubagentBinding(t *testing.T) {
	p := newParties(t)
	subKey := mustKey(t, AlgEd25519, "sub-eph")
	parentRaw := p.agentToken(t)
	parent, err := p.tokens().VerifyAgentToken(ctx, parentRaw, AgentTokenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	subClaims := func() AgentClaims {
		c := p.agentClaims()
		c.Sub = "aauth:assistant+worker1@agent.example"
		c.ParentAgent = testAgentSub
		c.Cnf = ConfirmKey(subKey.PublicJWK())
		return c
	}
	raw, err := TokenIssuer{Key: p.apKey}.Agent(subClaims())
	if err != nil {
		t.Fatal(err)
	}
	sub, err := p.tokens().VerifySubagentToken(ctx, raw, parent)
	if err != nil {
		t.Fatal(err)
	}
	if !sub.Key.SameKey(subKey.PublicJWK()) || sub.Claims.ParentAgent != testAgentSub {
		t.Fatalf("%+v", sub.Claims)
	}

	// The token request binds to the sub-agent's key, and the person token confirms that key.
	personRaw, pc := p.personToken(t, func(c *PersonClaims) { c.Cnf = ConfirmKey(subKey.PublicJWK()) })
	rc := p.resourceClaims(pc)
	rc.AgentJKT = sub.Thumbprint
	parentTP, _ := p.agentKey.PublicJWK().Thumbprint()
	in := TokenRequestInput{
		ResourceToken: p.resourceToken(t, rc), PresentedToken: personRaw,
		Audience: testPS, ExpectedPS: testPS, SigningThumbprint: parentTP, Subagent: sub,
	}
	if _, _, err := p.tokens().VerifyTokenRequest(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.Subagent = nil
	_, _, err = p.tokens().VerifyTokenRequest(ctx, in)
	wantTokenCode(t, err, CodeInvalidResourceToken)

	// Negatives.
	mk := func(mutate func(*AgentClaims), key *PrivateKey) string {
		c := subClaims()
		mutate(&c)
		tok, err := SignJWT(key, TypAgent, c)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	if _, err := p.tokens().VerifySubagentToken(ctx, mk(func(c *AgentClaims) { c.ParentAgent = "" }, p.apKey), parent); err == nil {
		t.Error("no parent_agent accepted")
	} else {
		wantTokenCode(t, err, CodeInvalidSubagentToken)
	}
	_, err = p.tokens().VerifySubagentToken(ctx, mk(func(c *AgentClaims) { c.ParentAgent = "aauth:other@agent.example" }, p.apKey), parent)
	wantTokenCode(t, err, CodeInvalidSubagentToken)
	// PS-71: another issuer's sub-agent.
	otherAP := mustKey(t, AlgEd25519, "ap-2")
	p.publish("https://other.example", DwkAgent, otherAP.PublicJWK())
	_, err = p.tokens().VerifySubagentToken(ctx, mk(func(c *AgentClaims) {
		c.Iss = "https://other.example"
		c.Sub = "aauth:assistant+worker1@other.example"
	}, otherAP), parent)
	wantTokenCode(t, err, CodeInvalidSubagentToken)
	expiring := subClaims()
	expiring.Exp = expiring.Iat + 30
	shortLived, _ := TokenIssuer{Key: p.apKey}.Agent(expiring)
	p.clock.Advance(time.Minute)
	_, err = p.tokens().VerifySubagentToken(ctx, shortLived, parent)
	wantTokenCode(t, err, CodeExpiredSubagentToken)
}

const testIntermediary = "https://flights.example"

func upstreamSetup(t *testing.T) (*parties, PersonClaims) {
	p := newParties(t)
	pc := p.personClaims()
	pc.Aud = testIntermediary
	return p, pc
}

// Upstream Token Verification, steps 1 to 3.
func TestUpstreamToken(t *testing.T) {
	p, pc := upstreamSetup(t)
	v := p.tokens()
	raw, err := TokenIssuer{Key: p.psKey}.Person(pc)
	if err != nil {
		t.Fatal(err)
	}
	opts := UpstreamTokenOptions{Intermediary: testIntermediary, PS: testPS}
	got, err := v.VerifyUpstreamToken(ctx, raw, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.Person == nil || got.Sub() != pc.Sub {
		t.Fatalf("%+v", got)
	}
	// Substitution: cnf.jwk is the calling agent's and isn't compared, so the token passes even
	// though nothing here knows the intermediary's signing key.
	if !got.Key().SameKey(p.agentKey.PublicJWK()) {
		t.Error("cnf should be the calling agent's key")
	}
	// Step 3: aud must be the intermediary.
	_, err = v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: "https://other.example", PS: testPS})
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	_, err = v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{PS: testPS})
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	// Step 2 at the PS: a person token's iss must be this PS.
	_, err = v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: testIntermediary, PS: "https://other.example"})
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	// Step 2 at the AS: it must be the PS that signed.
	if _, err := v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: testIntermediary, RequestingPS: testPS}); err != nil {
		t.Error(err)
	}
	_, err = v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: testIntermediary, RequestingPS: "https://other.example"})
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	_, err = v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: testIntermediary})
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	// "rejects any other typ with invalid_upstream_token".
	_, err = v.VerifyUpstreamToken(ctx, p.agentToken(t), opts)
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	// Revoked.
	rev := p.tokens()
	rev.Revocations = RevocationCheckerFunc(func(context.Context, string, string) (bool, error) { return true, nil })
	_, err = rev.VerifyUpstreamToken(ctx, raw, opts)
	wantTokenCode(t, err, CodeRevokedUpstreamToken)
	// Expired, when only exp fails.
	short := pc
	short.Exp = short.Iat + 30
	rawShort, _ := TokenIssuer{Key: p.psKey}.Person(short)
	p.clock.Advance(time.Minute)
	_, err = v.VerifyUpstreamToken(ctx, rawShort, opts)
	wantTokenCode(t, err, CodeExpiredUpstreamToken)
}

func TestUpstreamAuthToken(t *testing.T) {
	p := newParties(t)
	v := p.tokens()
	// Issued by this PS.
	ac := p.authClaims()
	ac.Aud = testIntermediary
	if _, err := v.VerifyUpstreamToken(ctx, p.authToken(t, ac), UpstreamTokenOptions{Intermediary: testIntermediary, PS: testPS}); err != nil {
		t.Fatal(err)
	}
	// Issued by an AS: "its iss MUST be this PS or an AS this PS presented a person token to".
	asc := p.authClaims()
	asc.Aud = testIntermediary
	asc.Iss, asc.Dwk = testAS, DwkAccess
	raw := p.authToken(t, asc)
	var asked [3]string
	opts := UpstreamTokenOptions{Intermediary: testIntermediary, PS: testPS, ASPresented: func(_ context.Context, as, aud, sub string) (bool, error) {
		asked = [3]string{as, aud, sub}
		return true, nil
	}}
	if _, err := v.VerifyUpstreamToken(ctx, raw, opts); err != nil {
		t.Fatal(err)
	}
	if asked != [3]string{testAS, testIntermediary, asc.Sub} {
		t.Errorf("asked %v", asked)
	}
	opts.ASPresented = func(context.Context, string, string, string) (bool, error) { return false, nil }
	_, err := v.VerifyUpstreamToken(ctx, raw, opts)
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	opts.ASPresented = nil
	_, err = v.VerifyUpstreamToken(ctx, raw, opts)
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	opts.ASPresented = func(context.Context, string, string, string) (bool, error) { return false, errors.New("db") }
	_, err = v.VerifyUpstreamToken(ctx, raw, opts)
	te := wantTokenCode(t, err, CodeServerError)
	if !te.Internal {
		t.Error("a lookup failure is internal")
	}
	// At the PS, ps must name this PS even if iss would pass.
	_, err = v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: testIntermediary, PS: testAS})
	wantTokenCode(t, err, CodeInvalidUpstreamToken)
	// At the AS, only ps is checked: "The AS does not check an auth token's iss beyond verifying its
	// signature".
	if _, err := v.VerifyUpstreamToken(ctx, raw, UpstreamTokenOptions{Intermediary: testIntermediary, RequestingPS: testPS}); err != nil {
		t.Error(err)
	}
}

func TestCheckUpdatedRequest(t *testing.T) {
	p, _, pc := presentedSetup(t)
	base := p.resourceClaims(pc)
	orig := &ResourceToken{Claims: base}
	presented := &ChainToken{Person: &PersonToken{Claims: pc}}
	if err := CheckUpdatedRequest(orig, &ResourceToken{Claims: base}, presented); err != nil {
		t.Fatal(err)
	}
	mut := func(f func(*ResourceClaims)) error {
		c := base
		f(&c)
		return CheckUpdatedRequest(orig, &ResourceToken{Claims: c}, presented)
	}
	if err := mut(func(c *ResourceClaims) { c.Scope = "narrower" }); err != nil {
		t.Errorf("scope may change: %v", err)
	}
	for name, f := range map[string]func(*ResourceClaims){
		"iss":           func(c *ResourceClaims) { c.Iss = "x" },
		"ps":            func(c *ResourceClaims) { c.PS = "x" },
		"sub":           func(c *ResourceClaims) { c.Sub = "x" },
		"agent_jkt":     func(c *ResourceClaims) { c.AgentJKT = "x" },
		"mission_s256":  func(c *ResourceClaims) { c.MissionS256 = "x" },
		"tenant":        func(c *ResourceClaims) { c.Tenant = "x" },
		"presented_jti": func(c *ResourceClaims) { c.PresentedJti = "x" },
	} {
		err := mut(f)
		te := wantTokenCode(t, err, CodeInvalidResourceToken)
		if !strings.Contains(te.Detail, name) {
			t.Errorf("%s: %s", name, te.Detail)
		}
	}
}

func TestVerifyDeliveredAuthToken(t *testing.T) {
	p := newParties(t)
	key := p.agentKey.PublicJWK()
	claims := func() AuthClaims {
		c := p.authClaims()
		c.Iss, c.Dwk = testAS, DwkAccess
		c.Scope = "data.read"
		return c
	}
	want := func(c AuthClaims) DeliveryChecks {
		return DeliveryChecks{AS: testAS, Resource: testResource, AgentKey: key, Sub: c.Sub, ResourceTokenScope: "data.read data.write", PresentedExp: c.Exp + 10}
	}
	c := claims()
	if _, err := p.tokens().VerifyDeliveredAuthToken(ctx, p.authToken(t, c), want(c)); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		mutate func(*AuthClaims)
		checks func(*DeliveryChecks)
	}{
		"iss":             {checks: func(d *DeliveryChecks) { d.AS = "https://other.example" }},
		"aud":             {checks: func(d *DeliveryChecks) { d.Resource = "https://other.example" }},
		"cnf":             {checks: func(d *DeliveryChecks) { d.AgentKey = mustKey(t, AlgEd25519, "o").PublicJWK() }},
		"sub":             {checks: func(d *DeliveryChecks) { d.Sub = "other" }},
		"scope broader":   {mutate: func(c *AuthClaims) { c.Scope = "data.read admin" }},
		"exp too late":    {checks: func(d *DeliveryChecks) { d.PresentedExp = 5 }},
		"unasked budget":  {mutate: func(c *AuthClaims) { c.Budget = &Budget{Amount: 1, Unit: "USD", Decimals: 2} }},
		"budget missing":  {checks: func(d *DeliveryChecks) { d.BudgetSent = &Budget{Amount: 1, Unit: "USD", Decimals: 2} }},
		"budget too high": {mutate: func(c *AuthClaims) { c.Budget = &Budget{Amount: 9, Unit: "USD", Decimals: 2} }, checks: func(d *DeliveryChecks) { d.BudgetSent = &Budget{Amount: 1, Unit: "USD", Decimals: 2} }},
		"budget unit":     {mutate: func(c *AuthClaims) { c.Budget = &Budget{Amount: 1, Unit: "EUR", Decimals: 2} }, checks: func(d *DeliveryChecks) { d.BudgetSent = &Budget{Amount: 1, Unit: "USD", Decimals: 2} }},
		"budget decimals": {mutate: func(c *AuthClaims) { c.Budget = &Budget{Amount: 1, Unit: "USD", Decimals: 6} }, checks: func(d *DeliveryChecks) { d.BudgetSent = &Budget{Amount: 1, Unit: "USD", Decimals: 2} }},
	} {
		t.Run(name, func(t *testing.T) {
			c := claims()
			if tc.mutate != nil {
				tc.mutate(&c)
			}
			d := want(c)
			if tc.checks != nil {
				tc.checks(&d)
			}
			_, err := p.tokens().VerifyDeliveredAuthToken(ctx, p.authToken(t, c), d)
			wantTokenCode(t, err, CodeASUnreachable)
		})
	}
	// Budget within what was sent passes.
	c = claims()
	c.Budget = &Budget{Amount: 1, Unit: "USD", Decimals: 2}
	d := want(c)
	d.BudgetSent = &Budget{Amount: 5, Unit: "USD", Decimals: 2}
	if _, err := p.tokens().VerifyDeliveredAuthToken(ctx, p.authToken(t, c), d); err != nil {
		t.Fatal(err)
	}
	// R3 delivery checks.
	doc := []byte(`{"vocabulary":"urn:aauth:vocabulary:mcp","operations":[{"tool":"a"},{"tool":"b","x":1}]}`)
	s := S256(doc)
	r3doc, err := ParseR3Document(doc, s)
	if err != nil {
		t.Fatal(err)
	}
	c = claims()
	c.R3URI, c.R3S256 = "https://resource.example/r3", s
	c.R3Granted = &R3Operations{Vocabulary: "urn:aauth:vocabulary:mcp", Operations: []json.RawMessage{json.RawMessage(`{"x":1,"tool":"b"}`)}}
	d = want(c)
	d.ResourceToken = &ResourceToken{Claims: ResourceClaims{R3URI: c.R3URI, R3S256: s}}
	d.R3Document = r3doc
	if _, err := p.tokens().VerifyDeliveredAuthToken(ctx, p.authToken(t, c), d); err != nil {
		t.Fatal(err)
	}
	c.R3Granted = &R3Operations{Vocabulary: "urn:aauth:vocabulary:mcp", Operations: []json.RawMessage{json.RawMessage(`{"tool":"c"}`)}}
	_, err = p.tokens().VerifyDeliveredAuthToken(ctx, p.authToken(t, c), d)
	wantTokenCode(t, err, CodeASUnreachable)
	c.R3Granted = &R3Operations{Vocabulary: "urn:aauth:vocabulary:mcp", Operations: []json.RawMessage{json.RawMessage(`{"tool":"a"}`)}}
	d.ResourceToken = &ResourceToken{Claims: ResourceClaims{R3URI: "https://resource.example/other", R3S256: s}}
	_, err = p.tokens().VerifyDeliveredAuthToken(ctx, p.authToken(t, c), d)
	wantTokenCode(t, err, CodeASUnreachable)
}

func TestScopeWithin(t *testing.T) {
	for _, c := range []struct {
		granted, allowed string
		ok               bool
	}{
		{"a", "a b", true},
		{"a b", "a b", true},
		{"", "a", true},
		{"c", "a b", false},
		{"a c", "a b", false},
		{"a", "", false},
	} {
		if ScopeWithin(c.granted, c.allowed) != c.ok {
			t.Errorf("%+v", c)
		}
	}
}

func TestTokenErrorProblem(t *testing.T) {
	e := tokenFail(SigExpiredJWT, "gone").forParam(ParamPresented)
	if p := e.Problem(); p.Code != CodeExpiredPresentedToken || p.HTTPStatus() != 400 {
		t.Fatalf("%+v", p)
	}
	internal := &TokenError{Sig: SigInvalidJWT, Internal: true, Detail: "secret detail"}
	internal.forParam(ParamUpstream)
	if p := internal.Problem(); p.Code != CodeServerError || p.Detail != "" {
		t.Fatalf("%+v", p)
	}
	for _, c := range []struct {
		sig   SignatureErrorCode
		param TokenParam
		want  ErrorCode
	}{
		{SigInvalidJWT, ParamResource, "invalid_resource_token"},
		{SigExpiredJWT, ParamResource, "expired_resource_token"},
		{SigRevokedJWT, ParamResource, "revoked_resource_token"},
		{SigInvalidJWT, ParamUpstream, "invalid_upstream_token"},
		{SigUnknownKey, ParamUpstream, "invalid_upstream_token"},
		{SigIssuerMismatch, ParamPresented, "invalid_presented_token"},
		{SigUnsupportedAlgorithm, ParamSubagent, "invalid_subagent_token"},
		{SigClockSkew, ParamSubagent, "clock_skew"},
	} {
		if got := paramCode(c.sig, c.param); got != c.want {
			t.Errorf("%s/%s: %s", c.sig, c.param, got)
		}
	}
}

// The check this replaced asserted a violation: "verifiers rely on parent_agent, not on parsing the local part". A token whose sub has a `+` and no parent_agent verifies as a top-level agent, and one with parent_agent verifies
// without its local part being tied to the parent's.
func TestAgentTokenLocalPartDecidesNothing(t *testing.T) {
	p := newParties(t)
	v := p.tokens()
	now := p.clock.Now().Unix()
	token := func(edit func(map[string]any)) string {
		c := map[string]any{
			"iss": testAP, "dwk": DwkAgent, "sub": testAgentSub, "jti": "j1",
			"iat": now, "exp": now + 3600, "cnf": cnfMap(p.agentKey.PublicJWK()),
		}
		edit(c)
		return signRawJWT(t, p.apKey, map[string]any{"alg": "Ed25519", "typ": TypAgent, "kid": "ap-1"}, c)
	}
	for _, sub := range []string{"aauth:a+b@agent.example", "aauth:a+b+c@agent.example"} {
		if _, err := v.VerifyAgentToken(ctx, token(func(c map[string]any) { c["sub"] = sub }), AgentTokenOptions{}); err != nil {
			t.Errorf("%s: %v", sub, err)
		}
	}
	raw := token(func(c map[string]any) { c["sub"], c["parent_agent"] = "aauth:other+a+b@agent.example", testAgentSub })
	if _, err := v.VerifyAgentToken(ctx, raw, AgentTokenOptions{}); err != nil {
		t.Errorf("with parent_agent: %v", err)
	}
	// What isn't an identifier still isn't.
	if _, err := v.VerifyAgentToken(ctx, token(func(c map[string]any) { c["sub"] = "aauth:a b@agent.example" }), AgentTokenOptions{}); err == nil {
		t.Error("a space in the local part was accepted")
	}
}
