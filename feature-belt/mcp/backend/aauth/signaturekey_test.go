package aauth

import "testing"

// signature-key draft, Signature-Key HTTP Request Header, and protocol, Keying Material.
func TestSignatureKeySerialization(t *testing.T) {
	got, err := SerializeSignatureKey(JWKSURIKeyMember("sig", "https://ps.example", "aauth-person.json", "key-1"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `sig=jwks_uri;id="https://ps.example";dwk="aauth-person.json";kid="key-1"`; got != want {
		t.Fatalf("got %s", got)
	}
	got, err = SerializeSignatureKey(JWTKeyMember("sig", "eyJhbGc.e30.sig"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `sig=jwt;jwt="eyJhbGc.e30.sig"`; got != want {
		t.Fatalf("got %s", got)
	}
	// "the parameter is serialized as cache rather than cache=?1".
	m := JWTKeyMember("sig1", "eyJ")
	m.Cache = true
	got, err = SerializeSignatureKey(m)
	if err != nil {
		t.Fatal(err)
	}
	if want := `sig1=jwt;jwt="eyJ";cache`; got != want {
		t.Fatalf("got %s", got)
	}
	got, err = SerializeSignatureKey(JWTKeyMember("sig1", "eyJ"), JWKSURIKeyMember("sig2", "https://example.com", "eg-config", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `sig1=jwt;jwt="eyJ", sig2=jwks_uri;id="https://example.com";dwk="eg-config";kid="k1"`; got != want {
		t.Fatalf("got %s", got)
	}
	for _, bad := range []SignatureKeyMember{
		{Label: "sig", Scheme: SchemeJWT},
		{Label: "sig", Scheme: SchemeJWKSURI, ID: "https://a.example"},
		{Label: "sig", Scheme: SchemeHWK},
		{Scheme: SchemeJWT, JWT: "x"},
	} {
		if _, err := SerializeSignatureKey(bad); err == nil {
			t.Errorf("%+v serialized", bad)
		}
	}
}

// The spec shows these with spaces and line breaks; RFC 9651 parsing takes them as the same field.
func TestSignatureKeyParse(t *testing.T) {
	f, err := ParseSignatureKey([]string{`sig=jwks_uri;id="https://ps.example"; dwk="aauth-person.json";kid="key-1", other=jwt;jwt="a.b.c";cache`})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Members) != 2 {
		t.Fatalf("%+v", f.Members)
	}
	m, ok := f.Member("sig")
	if !ok || m.Scheme != SchemeJWKSURI || m.ID != "https://ps.example" || m.Dwk != "aauth-person.json" || m.Kid != "key-1" {
		t.Fatalf("%+v", m)
	}
	m, ok = f.Member("other")
	if !ok || m.Scheme != SchemeJWT || m.JWT != "a.b.c" || !m.Cache {
		t.Fatalf("%+v", m)
	}
	if _, ok := f.Member("absent"); ok {
		t.Fatal("absent label found")
	}
	// A scheme the verifier doesn't implement parses: the verifier decides what it means.
	f, err = ParseSignatureKey([]string{`sig=hwk;kty="OKP";crv="Ed25519";x="abc";alg="Ed25519"`})
	if err != nil {
		t.Fatal(err)
	}
	if f.Members[0].Scheme != SchemeHWK || f.Members[0].Err != nil {
		t.Fatalf("%+v", f.Members[0])
	}
	f, _ = ParseSignatureKey([]string{`sig=future-scheme;anything=1`})
	if f.Members[0].Scheme != "future-scheme" || f.Members[0].Err != nil {
		t.Fatalf("%+v", f.Members[0])
	}
	// Not a scheme token.
	for _, value := range []string{`sig="jwt"`, `sig=1`, `sig=(a b)`, `sig`} {
		f, err := ParseSignatureKey([]string{value})
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if f.Members[0].Err == nil {
			t.Errorf("%s: want a member error", value)
		}
	}
	// A parameter of the wrong type.
	f, _ = ParseSignatureKey([]string{`sig=jwt;jwt=abc`})
	if f.Members[0].Err == nil {
		t.Error("a Token jwt parameter is not a String")
	}
	f, _ = ParseSignatureKey([]string{`sig=jwks_uri;id=:AAAA:;dwk="d";kid="k"`})
	if f.Members[0].Err == nil {
		t.Error("a Byte Sequence id is not a String")
	}
	if _, err := ParseSignatureKey([]string{`not a dictionary(`}); err == nil {
		t.Error("want an error for a malformed field")
	}
}
