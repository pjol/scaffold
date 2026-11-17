package aauth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// RFC 7638 section 3.1, the published thumbprint.
func TestThumbprintRFC7638Example(t *testing.T) {
	k, err := ParseJWK([]byte(`{
		"kty": "RSA",
		"n": "0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw",
		"e": "AQAB",
		"alg": "RS256",
		"kid": "2011-04-29"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := k.Thumbprint()
	if err != nil {
		t.Fatal(err)
	}
	if got != "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs" {
		t.Fatalf("thumbprint %s", got)
	}
}

// RFC 8037 appendices A.1 to A.4: the key, its thumbprint, and the signature over the JWS
// signing input. The signature is a raw Ed25519 one; the `alg: EdDSA` in that example's header
// is what AAuth refuses, which the next test shows.
func TestEd25519RFC8037Vectors(t *testing.T) {
	k, err := ParsePrivateJWK([]byte(`{"kty":"OKP","crv":"Ed25519","alg":"Ed25519","kid":"a1",
		"d":"nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A",
		"x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`))
	if err != nil {
		t.Fatal(err)
	}
	tp, err := k.PublicJWK().Thumbprint()
	if err != nil {
		t.Fatal(err)
	}
	if tp != "kPrK_qmxVWaYVA9wwBF6Iuo3vVzz7TxHCTwXBygrS4k" {
		t.Fatalf("thumbprint %s", tp)
	}
	input := "eyJhbGciOiJFZERTQSJ9.RXhhbXBsZSBvZiBFZDI1NTE5IHNpZ25pbmc"
	const want = "hgyY0il_MGCjP0JzlnLWG1PPOt7-09PGcvMg3AIbQR6dWbhijcNR4ki4iylGjg5BhVsPt9g7sVvpAr_MuM0KAg"
	sig, err := k.Sign([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if Base64URL(sig) != want {
		t.Fatalf("signature %s", Base64URL(sig))
	}
	if err := k.PublicJWK().Verify([]byte(input), sig); err != nil {
		t.Fatal(err)
	}
}

// The same RFC 8037 JWS, whose header says EdDSA, is refused though its signature is good.
func TestJWTVerifyRefusesEdDSAHeader(t *testing.T) {
	k, _ := ParsePrivateJWK([]byte(`{"kty":"OKP","crv":"Ed25519","alg":"Ed25519","kid":"a1",
		"d":"nWGxne_9WmC6hEr0kuwsxERJxWl7MmkZcDusAxyuf2A",
		"x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`))
	jwt, err := ParseJWT("eyJhbGciOiJFZERTQSJ9.RXhhbXBsZSBvZiBFZDI1NTE5IHNpZ25pbmc.hgyY0il_MGCjP0JzlnLWG1PPOt7-09PGcvMg3AIbQR6dWbhijcNR4ki4iylGjg5BhVsPt9g7sVvpAr_MuM0KAg")
	if err != nil {
		t.Fatal(err)
	}
	if err := jwt.Verify(k.PublicJWK()); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("want refusal, got %v", err)
	}
}

// protocol, Signature Algorithms; signature-key draft, Algorithm Determination.
func TestAlgorithmDetermination(t *testing.T) {
	okp := JWK{Kty: "OKP", Crv: "Ed25519", X: "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}
	ec := JWK{Kty: "EC", Crv: "P-256", X: "qIVYZVLCrPZHGHjP17CTW0_-D9Lfw0EkjqF7xB4FivA", Y: "Mc4nN9LTDOBhfoUeg8Ye9WedFRhnZXZJA12Qp0zZ6F0"}
	with := func(k JWK, alg string) JWK { k.Alg = alg; return k }
	cases := []struct {
		name string
		key  JWK
		code SignatureErrorCode // empty means valid
	}{
		{"Ed25519", with(okp, "Ed25519"), ""},
		{"ES256", with(ec, "ES256"), ""},
		{"alg absent", okp, SigUnsupportedAlgorithm},
		{"polymorphic EdDSA", with(okp, "EdDSA"), SigUnsupportedAlgorithm},
		{"none", with(okp, "none"), SigUnsupportedAlgorithm},
		{"HS256", JWK{Kty: "oct", Alg: "HS256"}, SigUnsupportedAlgorithm},
		{"oct with no alg", JWK{Kty: "oct"}, SigUnsupportedAlgorithm},
		{"Ed448 not implemented", JWK{Kty: "OKP", Crv: "Ed448", X: "abc", Alg: "Ed448"}, SigUnsupportedAlgorithm},
		{"ES384 not implemented", JWK{Kty: "EC", Crv: "P-384", X: "a", Y: "b", Alg: "ES384"}, SigUnsupportedAlgorithm},
		{"RS256 not implemented", JWK{Kty: "RSA", N: "abc", E: "AQAB", Alg: "RS256"}, SigUnsupportedAlgorithm},
		{"post-quantum key type", JWK{Kty: "AKP", Alg: "ML-DSA-65"}, SigUnsupportedAlgorithm},
		// The spec's two named examples of disagreement.
		{"ES256 with kty RSA", JWK{Kty: "RSA", N: "abc", E: "AQAB", Alg: "ES256"}, SigInvalidKey},
		{"ES256 with crv P-384", JWK{Kty: "EC", Crv: "P-384", X: "a", Y: "b", Alg: "ES256"}, SigInvalidKey},
		{"Ed25519 with kty EC", with(ec, "Ed25519"), SigInvalidKey},
		{"Ed25519 with crv X25519", JWK{Kty: "OKP", Crv: "X25519", X: "a", Alg: "Ed25519"}, SigInvalidKey},
		{"ES256 with kty OKP", with(okp, "ES256"), SigInvalidKey},
		{"ES256 with kty oct", JWK{Kty: "oct", Alg: "ES256"}, SigInvalidKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.key.Algorithm()
			if c.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				if err := c.key.Validate(); err != nil {
					t.Fatal(err)
				}
				return
			}
			var ke *KeyError
			if !errors.As(err, &ke) || ke.Code != c.code {
				t.Fatalf("want %s, got %v", c.code, err)
			}
		})
	}
}

// protocol, Request-Context Binding: incomplete before decoding, then invalid key material.
func TestJWKStructureAndDecoding(t *testing.T) {
	incomplete := []JWK{
		{},
		{Alg: "Ed25519", X: "abc"},
		{Kty: "OKP", Alg: "Ed25519"},
		{Kty: "OKP", Crv: "Ed25519", Alg: "Ed25519"},
		{Kty: "EC", Crv: "P-256", X: "a", Alg: "ES256"},
		{Kty: "EC", X: "a", Y: "b", Alg: "ES256"},
		{Kty: "RSA", E: "AQAB", Alg: "RS256"},
		{Kty: "RSA", N: "a", Alg: "RS256"},
	}
	for _, k := range incomplete {
		var ke *KeyError
		if err := k.Validate(); !errors.As(err, &ke) || !ke.Incomplete {
			t.Errorf("%+v: want incomplete, got %v", k, err)
		}
	}
	bad := []JWK{
		{Kty: "OKP", Crv: "Ed25519", X: "!!!", Alg: "Ed25519"},
		{Kty: "OKP", Crv: "Ed25519", X: "AAAA", Alg: "Ed25519"}, // too short
		{Kty: "EC", Crv: "P-256", X: "AAAA", Y: "AAAA", Alg: "ES256"},
		// 32 bytes each, but not a point on P-256.
		{Kty: "EC", Crv: "P-256", X: strings.Repeat("A", 43), Y: strings.Repeat("A", 43), Alg: "ES256"},
	}
	for _, k := range bad {
		var ke *KeyError
		if err := k.Validate(); !errors.As(err, &ke) || ke.Code != SigInvalidKey || ke.Incomplete {
			t.Errorf("%+v: want invalid_key, got %v", k, err)
		}
	}
	// A public key must not carry its private part.
	withD, err := ParseJWK([]byte(`{"kty":"OKP","crv":"Ed25519","alg":"Ed25519","d":"x","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}`))
	if err != nil {
		t.Fatal(err)
	}
	var ke *KeyError
	if err := withD.Validate(); !errors.As(err, &ke) || ke.Code != SigInvalidKey {
		t.Errorf("private material: %v", err)
	}
}

// RFC 7517: unknown members are ignored. Marshalling never emits private material.
func TestJWKRoundTrip(t *testing.T) {
	k, err := ParseJWK([]byte(`{"kty":"OKP","crv":"Ed25519","alg":"Ed25519","kid":"a","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo","future":{"a":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Validate(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(k)
	if want := `{"kty":"OKP","crv":"Ed25519","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo","alg":"Ed25519","kid":"a"}`; string(b) != want {
		t.Fatalf("got %s", b)
	}
	for _, alg := range []Alg{AlgEd25519, AlgES256} {
		priv := mustKey(t, alg, "")
		b, err := priv.MarshalPrivateJWK()
		if err != nil {
			t.Fatal(err)
		}
		back, err := ParsePrivateJWK(b)
		if err != nil {
			t.Fatal(err)
		}
		pubJSON, _ := json.Marshal(back.PublicJWK())
		if strings.Contains(string(pubJSON), `"d"`) {
			t.Fatalf("public JWK leaks d: %s", pubJSON)
		}
		tp, _ := priv.PublicJWK().Thumbprint()
		if priv.PublicJWK().Kid != tp {
			t.Errorf("default kid is the thumbprint: %s", priv.PublicJWK().Kid)
		}
		sig, err := back.Sign([]byte("m"))
		if err != nil {
			t.Fatal(err)
		}
		if err := priv.PublicJWK().Verify([]byte("m"), sig); err != nil {
			t.Fatalf("%s: %v", alg, err)
		}
		if len(sig) != 64 {
			t.Errorf("%s: JOSE signature is 64 octets, got %d", alg, len(sig))
		}
		if err := priv.PublicJWK().Verify([]byte("other"), sig); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: wrong message: %v", alg, err)
		}
		if err := priv.PublicJWK().Verify([]byte("m"), sig[:63]); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: short signature: %v", alg, err)
		}
	}
	// d must match x.
	other := mustKey(t, AlgEd25519, "o")
	b2, _ := other.MarshalPrivateJWK()
	var m map[string]any
	_ = json.Unmarshal(b2, &m)
	m["x"] = "11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"
	mismatched, _ := json.Marshal(m)
	if _, err := ParsePrivateJWK(mismatched); err == nil {
		t.Error("a private JWK whose d doesn't match x was accepted")
	}
}

// "A verifier MUST select the key matching kid without requiring any other member of the JWKS to
// be usable".
func TestSelectJWKIgnoresOtherMembers(t *testing.T) {
	raw := []byte(`{"keys":[
		{"kty":"AKP","alg":"ML-DSA-65","kid":"pq","pub":"xyz"},
		{"kty":"RSA","kid":"rsa-no-alg","n":"a","e":"AQAB"},
		"not an object",
		42,
		{"kid":7},
		{"kty":"OKP","crv":"Ed25519","alg":"Ed25519","kid":"mine","x":"11qYAYKxCrfVS_7TyWQHOg7hcvPapiMlrwIaaPcHURo"}
	]}`)
	k, found, err := SelectJWK(raw, "mine")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if err := k.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SelectJWK(raw, "absent"); found || err != nil {
		t.Fatalf("absent: found=%v err=%v", found, err)
	}
	// The selected member is the one judged.
	k, found, err = SelectJWK(raw, "rsa-no-alg")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if _, err := k.Algorithm(); err == nil {
		t.Fatal("selected key with no alg passed")
	}
	if _, _, err := SelectJWK([]byte(`{"nokeys":[]}`), "a"); err == nil {
		t.Fatal("a document with no keys member parsed")
	}
}

func TestParseJWTIsStrict(t *testing.T) {
	k := mustKey(t, AlgEd25519, "k")
	good, err := SignJWT(k, TypAgent, map[string]any{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := ParseJWT(good)
	if err != nil {
		t.Fatal(err)
	}
	if jwt.Header.Typ != TypAgent || jwt.Header.Alg != "Ed25519" || jwt.Header.Kid != "k" {
		t.Fatalf("%+v", jwt.Header)
	}
	if err := jwt.Verify(k.PublicJWK()); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, ".")
	critHeader := Base64URL([]byte(`{"alg":"Ed25519","typ":"aa-agent+jwt","kid":"k","crit":["x"]}`))
	for _, bad := range []string{
		"",
		"a.b",
		"a.b.c.d",
		parts[0] + "=." + parts[1] + "." + parts[2], // padding
		"###." + parts[1] + "." + parts[2],
		parts[0] + ".###." + parts[2],
		parts[0] + "." + parts[1] + ".###",
		Base64URL([]byte("not json")) + "." + parts[1] + "." + parts[2],
		critHeader + "." + parts[1] + "." + parts[2],
		strings.Repeat("a", maxJWTSize+1),
	} {
		if _, err := ParseJWT(bad); !errors.Is(err, ErrMalformedJWT) {
			t.Errorf("%.40q: want ErrMalformedJWT, got %v", bad, err)
		}
	}
	// A key whose alg differs from the header's can't verify the token.
	es := mustKey(t, AlgES256, "k")
	if err := jwt.Verify(es.PublicJWK()); !errors.Is(err, ErrBadSignature) {
		t.Errorf("alg mismatch: %v", err)
	}
}
