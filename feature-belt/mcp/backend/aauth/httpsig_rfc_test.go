package aauth

import (
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Vectors below are transcribed from RFC 9421 with their section cited. RFC 8792 line wrapping
// is undone: a backslash-newline and the following indent are joined.

const (
	rfcTestRequestDigest  = "sha-512=:WZDPaVn/7XgHaAy8pmojAkGWoRx2UFChF41A2svX+TaPm+AbwAgBWnrIiYllu7BNNyealdVLvRwEmTHWXvJwew==:"
	rfcTestResponseDigest = "sha-512=:mEWXIS7MaLRuGgxOBdODa3xqM1XdEvxoYhvlCFJ41QJgJc4GTsPp29l5oGX69wWdXymyU0rjJuahq4l5aGgfLQ==:"
	rfcBusyDigest         = "sha-512=:0Y6iCBzGg5rZtoXS95Ijz03mslf6KAMCloESHObfwnHJDbkkWWQz6PhhU9kxsTbARtY2PTBOzq24uJFpHsMuAg==:"

	// RFC 9421 appendix B.1.2, test-key-rsa-pss.
	rfcRSAPublicPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAr4tmm3r20Wd/PbqvP1s2
+QEtvpuRaV8Yq40gjUR8y2Rjxa6dpG2GXHbPfvMs8ct+Lh1GH45x28Rw3Ry53mm+
oAXjyQ86OnDkZ5N8lYbggD4O3w6M6pAvLkhk95AndTrifbIFPNU8PPMO7OyrFAHq
gDsznjPFmTOtCEcN2Z1FpWgchwuYLPL+Wokqltd11nqqzi+bJ9cvSKADYdUAAN5W
Utzdpiy6LbTgSxP7ociU4Tn0g5I6aDZJ7A8Lzo0KSyZYoA485mqcO0GVAdVw9lq4
aOT9v6d+nb4bnNkQVklLQ3fVAvJm+xdDOp9LCNCN48V2pnDOkFV6+U9nV5oyc6XI
2wIDAQAB
-----END PUBLIC KEY-----`

	// RFC 9421 appendix B.1.5, test-shared-secret.
	rfcSharedSecret = "uzvJfB4u3N0Jy4T7NZ75MDVcr8zSTInedJtkgcu46YW4XByzNJjxBdtjUkdJPBtbmHhIDi6pcl8jsasjlTMtDQ=="
)

// RFC 9421 appendix B.1.3, test-key-ecc-p256, public part with the alg AAuth requires.
func rfcP256Key(t *testing.T) JWK {
	t.Helper()
	k, err := ParseJWK([]byte(`{"kty":"EC","crv":"P-256","alg":"ES256","kid":"test-key-ecc-p256",
		"x":"qIVYZVLCrPZHGHjP17CTW0_-D9Lfw0EkjqF7xB4FivA",
		"y":"Mc4nN9LTDOBhfoUeg8Ye9WedFRhnZXZJA12Qp0zZ6F0"}`))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// RFC 9421 appendix B.1.4, test-key-ed25519, with the alg AAuth requires.
func rfcEd25519Key(t *testing.T) *PrivateKey {
	t.Helper()
	k, err := ParsePrivateJWK([]byte(`{"kty":"OKP","crv":"Ed25519","alg":"Ed25519","kid":"test-key-ed25519",
		"d":"n4Ni-HpISpVObnQMW0wOhCKROaIKqKtW_2ZYb2p9KcU",
		"x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs"}`))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// The test-request message of RFC 9421 appendix B.2, as a server would receive it.
func rfcTestRequest() RequestMessage {
	r := httptest.NewRequest(http.MethodPost, "/foo?param=Value&Pet=dog", strings.NewReader(`{"hello": "world"}`))
	r.Host = "example.com"
	r.Header.Set("Date", "Tue, 20 Apr 2021 02:07:55 GMT")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Content-Digest", rfcTestRequestDigest)
	r.Header.Set("Content-Length", "18")
	m := NewRequestMessage(r)
	m.Scheme = "https"
	return m
}

// The test-response message of RFC 9421 appendix B.2.
func rfcTestResponse() ResponseMessage {
	return ResponseMessage{Status: 200, Header: http.Header{
		"Date":           {"Tue, 20 Apr 2021 02:07:56 GMT"},
		"Content-Type":   {"application/json"},
		"Content-Digest": {rfcTestResponseDigest},
		"Content-Length": {"23"},
	}}
}

// The unsigned response of RFC 9421 section 2.4.
func rfcBusyResponse() ResponseMessage {
	return ResponseMessage{Status: 503, Header: http.Header{
		"Date":           {"Tue, 20 Apr 2021 02:07:56 GMT"},
		"Content-Type":   {"application/json"},
		"Content-Length": {"62"},
		"Content-Digest": {rfcBusyDigest},
	}}
}

// inputMember parses a Signature-Input field value and returns its only member's parameters.
func inputMember(t *testing.T, field string) SignatureParams {
	t.Helper()
	members, err := ParseSignatureInput([]string{field})
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].Err != nil {
		t.Fatalf("%+v", members)
	}
	return members[0].Params
}

func signatureBytes(t *testing.T, field, label string) []byte {
	t.Helper()
	sigs, err := ParseSignatures([]string{field})
	if err != nil {
		t.Fatal(err)
	}
	b, ok := sigs[label]
	if !ok {
		t.Fatalf("no signature for %s", label)
	}
	return b
}

func verifyRSAPSS(t *testing.T, base string, sig []byte) {
	t.Helper()
	block, _ := pem.Decode([]byte(rfcRSAPublicPEM))
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha512.Sum512([]byte(base))
	if err := rsa.VerifyPSS(pub.(*rsa.PublicKey), crypto.SHA512, digest[:], sig, &rsa.PSSOptions{SaltLength: 64}); err != nil {
		t.Fatalf("published signature does not verify over our base: %v\n%s", err, base)
	}
}

// RFC 9421 appendix B.2.1, minimal signature with no covered components.
func TestRFC9421_B21_Minimal(t *testing.T) {
	const input = `sig-b21=();created=1618884473;keyid="test-key-rsa-pss";nonce="b3k2pp5k7z-50gnwp.yemd"`
	const sig = `sig-b21=:d2pmTvmbncD3xQm8E9ZV2828BjQWGgiwAaw5bAkgibUopemLJcWDy/lkbbHAve4cRAtx31Iq786U7it++wgGxbtRxf8Udx7zFZsckzXaJMkA7ChG52eSkFxykJeNqsrWH5S+oxNFlD4dzVuwe8DhTSja8xxbR/Z2cOGdCbzR72rgFWhzx2VjBqJzsPLMIQKhO4DGezXehhWwE56YCE+O6c0mKZsfxVrogUvA4HELjVKWmAvtl6UnCh8jYzuVG5WSb/QEVPnP5TmcAnLH1g+s++v6d4s8m0gCw1fV5/SITLq9mhho8K3+7EPYTU8IU1bLhdxO5Nyt8C8ssinQ98Xw9Q==:`
	const wantBase = `"@signature-params": ();created=1618884473;keyid="test-key-rsa-pss";nonce="b3k2pp5k7z-50gnwp.yemd"`
	req := rfcTestRequest()
	base, err := SignatureBase(&req, nil, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	verifyRSAPSS(t, base, signatureBytes(t, sig, "sig-b21"))
}

// RFC 9421 appendix B.2.2, selective components including a @query-param and a tag parameter.
func TestRFC9421_B22_Selective(t *testing.T) {
	const input = `sig-b22=("@authority" "content-digest" "@query-param";name="Pet");created=1618884473;keyid="test-key-rsa-pss";tag="header-example"`
	const sig = `sig-b22=:LjbtqUbfmvjj5C5kr1Ugj4PmLYvx9wVjZvD9GsTT4F7GrcQEdJzgI9qHxICagShLRiLMlAJjtq6N4CDfKtjvuJyE5qH7KT8UCMkSowOB4+ECxCmT8rtAmj/0PIXxi0A0nxKyB09RNrCQibbUjsLS/2YyFYXEu4TRJQzRw1rLEuEfY17SARYhpTlaqwZVtR8NV7+4UKkjqpcAoFqWFQh62s7Cl+H2fjBSpqfZUJcsIk4N6wiKYd4je2U/lankenQ99PZfB4jY3I5rSV2DSBVkSFsURIjYErOs0tFTQosMTAoxk//0RoKUqiYY8Bh0aaUEb0rQl3/XaVe4bXTugEjHSw==:`
	wantBase := strings.Join([]string{
		`"@authority": example.com`,
		`"content-digest": ` + rfcTestRequestDigest,
		`"@query-param";name="Pet": dog`,
		`"@signature-params": ("@authority" "content-digest" "@query-param";name="Pet");created=1618884473;keyid="test-key-rsa-pss";tag="header-example"`,
	}, "\n")
	req := rfcTestRequest()
	base, err := SignatureBase(&req, nil, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	verifyRSAPSS(t, base, signatureBytes(t, sig, "sig-b22"))
}

// RFC 9421 appendix B.2.3, full coverage including @query.
func TestRFC9421_B23_Full(t *testing.T) {
	const input = `sig-b23=("date" "@method" "@path" "@query" "@authority" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-rsa-pss"`
	const sig = `sig-b23=:bbN8oArOxYoyylQQUU6QYwrTuaxLwjAC9fbY2F6SVWvh0yBiMIRGOnMYwZ/5MR6fb0Kh1rIRASVxFkeGt683+qRpRRU5p2voTp768ZrCUb38K0fUxN0O0iC59DzYx8DFll5GmydPxSmme9v6ULbMFkl+V5B1TP/yPViV7KsLNmvKiLJH1pFkh/aYA2HXXZzNBXmIkoQoLd7YfW91kE9o/CCoC1xMy7JA1ipwvKvfrs65ldmlu9bpG6A9BmzhuzF8Eim5f8ui9eH8LZH896+QIF61ka39VBrohr9iyMUJpvRX2Zbhl5ZJzSRxpJyoEZAFL2FUo5fTIztsDZKEgM4cUA==:`
	wantBase := strings.Join([]string{
		`"date": Tue, 20 Apr 2021 02:07:55 GMT`,
		`"@method": POST`,
		`"@path": /foo`,
		`"@query": ?param=Value&Pet=dog`,
		`"@authority": example.com`,
		`"content-type": application/json`,
		`"content-digest": ` + rfcTestRequestDigest,
		`"content-length": 18`,
		`"@signature-params": ("date" "@method" "@path" "@query" "@authority" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-rsa-pss"`,
	}, "\n")
	req := rfcTestRequest()
	base, err := SignatureBase(&req, nil, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	verifyRSAPSS(t, base, signatureBytes(t, sig, "sig-b23"))
}

// RFC 9421 appendix B.2.4, signing a response with ecdsa-p256-sha256: the JOSE ES256 form.
func TestRFC9421_B24_ResponseECDSA(t *testing.T) {
	const input = `sig-b24=("@status" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-ecc-p256"`
	const sig = `sig-b24=:wNmSUAhwb5LxtOtOpNa6W5xj067m5hFrj0XQ4fvpaCLx0NKocgPquLgyahnzDnDAUy5eCdlYUEkLIj+32oiasw==:`
	wantBase := strings.Join([]string{
		`"@status": 200`,
		`"content-type": application/json`,
		`"content-digest": ` + rfcTestResponseDigest,
		`"content-length": 23`,
		`"@signature-params": ("@status" "content-type" "content-digest" "content-length");created=1618884473;keyid="test-key-ecc-p256"`,
	}, "\n")
	resp := rfcTestResponse()
	base, err := SignatureBase(nil, &resp, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	if err := rfcP256Key(t).Verify([]byte(base), signatureBytes(t, sig, "sig-b24")); err != nil {
		t.Fatal(err)
	}
}

// RFC 9421 appendix B.2.5. AAuth forbids HMAC, but the base is shared, and the published MAC
// proves ours is the RFC's.
func TestRFC9421_B25_BaseUnderHMAC(t *testing.T) {
	const input = `sig-b25=("date" "@authority" "content-type");created=1618884473;keyid="test-shared-secret"`
	const sig = `sig-b25=:pxcQw6G3AjtMBQjwo8XzkZf/bws5LelbaMk5rGIGtE8=:`
	wantBase := strings.Join([]string{
		`"date": Tue, 20 Apr 2021 02:07:55 GMT`,
		`"@authority": example.com`,
		`"content-type": application/json`,
		`"@signature-params": ("date" "@authority" "content-type");created=1618884473;keyid="test-shared-secret"`,
	}, "\n")
	req := rfcTestRequest()
	base, err := SignatureBase(&req, nil, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	secret, err := base64.StdEncoding.DecodeString(rfcSharedSecret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(base))
	if !hmac.Equal(mac.Sum(nil), signatureBytes(t, sig, "sig-b25")) {
		t.Fatal("published MAC does not match our base")
	}
}

// RFC 9421 appendix B.2.6, signing a request with ed25519. Deterministic, so the signature we make
// must equal the published one byte for byte.
func TestRFC9421_B26_Ed25519(t *testing.T) {
	const input = `sig-b26=("date" "@method" "@path" "@authority" "content-type" "content-length");created=1618884473;keyid="test-key-ed25519"`
	const sig = `sig-b26=:wqcAqbmYJ2ji2glfAMaRy4gruYYnx2nEFN2HN6jrnDnQCK1u02Gb04v9EDgwUPiu4A0w6vuQv5lIp5WPpBKRCw==:`
	wantBase := strings.Join([]string{
		`"date": Tue, 20 Apr 2021 02:07:55 GMT`,
		`"@method": POST`,
		`"@path": /foo`,
		`"@authority": example.com`,
		`"content-type": application/json`,
		`"content-length": 18`,
		`"@signature-params": ("date" "@method" "@path" "@authority" "content-type" "content-length");created=1618884473;keyid="test-key-ed25519"`,
	}, "\n")
	req := rfcTestRequest()
	params := inputMember(t, input)
	base, err := SignatureBase(&req, nil, params)
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	key := rfcEd25519Key(t)
	published := signatureBytes(t, sig, "sig-b26")
	if err := key.PublicJWK().Verify([]byte(base), published); err != nil {
		t.Fatal(err)
	}
	made, err := key.Sign([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	field, err := SignatureField("sig-b26", made)
	if err != nil {
		t.Fatal(err)
	}
	if field != sig {
		t.Fatalf("signature field:\n%s", field)
	}
	// The Signature-Input we serialize from the same parameters is the RFC's too.
	inputField, err := SignatureInputField("sig-b26", params)
	if err != nil {
		t.Fatal(err)
	}
	if inputField != input {
		t.Fatalf("input field:\n%s", inputField)
	}
}

// RFC 9421 section 2.5, Figure 1.
func TestRFC9421_Figure1(t *testing.T) {
	req := rfcTestRequest()
	base, err := SignatureBase(&req, nil, inputMember(t,
		`sig=("@method" "@authority" "@path" "content-digest" "content-length" "content-type");created=1618884473;keyid="test-key-rsa-pss"`))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		`"@method": POST`,
		`"@authority": example.com`,
		`"@path": /foo`,
		`"content-digest": ` + rfcTestRequestDigest,
		`"content-length": 18`,
		`"content-type": application/json`,
		`"@signature-params": ("@method" "@authority" "@path" "content-digest" "content-length" "content-type");created=1618884473;keyid="test-key-rsa-pss"`,
	}, "\n")
	if base != want {
		t.Fatalf("base:\n%s", base)
	}
}

// RFC 9421 section 3.2, the worked verification.
func TestRFC9421_Section32_Verify(t *testing.T) {
	const input = `sig1=("@method" "@authority" "@path" "content-digest" "content-length" "content-type");created=1618884473;keyid="test-key-rsa-pss"`
	const sig = `sig1=:HIbjHC5rS0BYaa9v4QfD4193TORw7u9edguPh0AW3dMq9WImrlFrCGUDih47vAxi4L2YRZ3XMJc1uOKk/J0ZmZ+wcta4nKIgBkKq0rM9hs3CQyxXGxHLMCy8uqK488o+9jrptQ+xFPHK7a9sRL1IXNaagCNN3ZxJsYapFj+JXbmaI5rtAdSfSvzPuBCh+ARHBmWuNo1UzVVdHXrl8ePL4cccqlazIJdC4QEjrF+Sn4IxBQzTZsL9y9TP5FsZYzHvDqbInkTNigBcE9cKOYNFCn4D/WM7F6TNuZO9EgtzepLWcjTymlHzK7aXq6Am6sfOrpIC49yXjj3ae6HRalVc/g==:`
	req := rfcTestRequest()
	base, err := SignatureBase(&req, nil, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	verifyRSAPSS(t, base, signatureBytes(t, sig, "sig1"))
}

// RFC 9421 section 2.4, the first signed response: `req` components on a response.
func TestRFC9421_Section24_RequestComponentsInResponse(t *testing.T) {
	const input = `reqres=("@status" "content-digest" "content-type" "@authority";req "@method";req "@path";req "content-digest";req);created=1618884479;keyid="test-key-ecc-p256"`
	const sig = `reqres=:dMT/A/76ehrdBTD/2Xx8QuKV6FoyzEP/I9hdzKN8LQJLNgzU4W767HK05rx1i8meNQQgQPgQp8wq2ive3tV5Ag==:`
	wantBase := strings.Join([]string{
		`"@status": 503`,
		`"content-digest": ` + rfcBusyDigest,
		`"content-type": application/json`,
		`"@authority";req: example.com`,
		`"@method";req: POST`,
		`"@path";req: /foo`,
		`"content-digest";req: ` + rfcTestRequestDigest,
		`"@signature-params": ("@status" "content-digest" "content-type" "@authority";req "@method";req "@path";req "content-digest";req);created=1618884479;keyid="test-key-ecc-p256"`,
	}, "\n")
	req, resp := rfcTestRequest(), rfcBusyResponse()
	base, err := SignatureBase(&req, &resp, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	if err := rfcP256Key(t).Verify([]byte(base), signatureBytes(t, sig, "reqres")); err != nil {
		t.Fatal(err)
	}
}

// RFC 9421 section 2.4, the second signed response, with @query, content-type and content-length
// from the request.
func TestRFC9421_Section24_SignedRequestSignedResponse(t *testing.T) {
	const input = `reqres=("@status" "content-digest" "content-type" "@authority";req "@method";req "@path";req "@query";req "content-digest";req "content-type";req "content-length";req);created=1618884479;keyid="test-key-ecc-p256"`
	const sig = `reqres=:C73J41GVKc+TYXbSobvZf0CmNcptRiWN+NY1Or0A36ISg6ymdRN6ZgR2QfrtopFNzqAyv+CeWrMsNbcV2Ojsgg==:`
	wantBase := strings.Join([]string{
		`"@status": 503`,
		`"content-digest": ` + rfcBusyDigest,
		`"content-type": application/json`,
		`"@authority";req: example.com`,
		`"@method";req: POST`,
		`"@path";req: /foo`,
		`"@query";req: ?param=Value&Pet=dog`,
		`"content-digest";req: ` + rfcTestRequestDigest,
		`"content-type";req: application/json`,
		`"content-length";req: 18`,
		`"@signature-params": ("@status" "content-digest" "content-type" "@authority";req "@method";req "@path";req "@query";req "content-digest";req "content-type";req "content-length";req);created=1618884479;keyid="test-key-ecc-p256"`,
	}, "\n")
	req, resp := rfcTestRequest(), rfcBusyResponse()
	base, err := SignatureBase(&req, &resp, inputMember(t, input))
	if err != nil {
		t.Fatal(err)
	}
	if base != wantBase {
		t.Fatalf("base:\n%s", base)
	}
	if err := rfcP256Key(t).Verify([]byte(base), signatureBytes(t, sig, "reqres")); err != nil {
		t.Fatal(err)
	}
}

// componentLine resolves one identifier against a header set and returns its base line.
func componentLine(t *testing.T, msg *RequestMessage, id string, sf map[string]string) (string, error) {
	t.Helper()
	c, err := ParseComponent(id)
	if err != nil {
		t.Fatal(err)
	}
	base, err := signatureBase(msg, nil, SignatureParams{Components: []Component{c}}, sf)
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(base, "\n")
	return line, nil
}

// RFC 9421 section 2.1, the field canonicalization examples.
func TestRFC9421_Section21_Fields(t *testing.T) {
	msg := &RequestMessage{Method: "GET", Authority: "www.example.com", Path: "/", Header: http.Header{
		"Date":              {"Tue, 20 Apr 2021 02:07:56 GMT"},
		"X-Ows-Header":      {"   Leading and trailing whitespace.   "},
		"X-Obs-Fold-Header": {"Obsolete\r\n    line folding."},
		"Cache-Control":     {"max-age=60", "   must-revalidate"},
		"Example-Dict":      {"  a=1,    b=2;x=1;y=2,   c=(a   b   c)"},
		"X-Empty-Header":    {""},
	}}
	for id, want := range map[string]string{
		`"host"`:              `"host": www.example.com`,
		`"date"`:              `"date": Tue, 20 Apr 2021 02:07:56 GMT`,
		`"x-ows-header"`:      `"x-ows-header": Leading and trailing whitespace.`,
		`"x-obs-fold-header"`: `"x-obs-fold-header": Obsolete line folding.`,
		`"cache-control"`:     `"cache-control": max-age=60, must-revalidate`,
		`"example-dict"`:      `"example-dict": a=1,    b=2;x=1;y=2,   c=(a   b   c)`,
		`"x-empty-header"`:    `"x-empty-header": `,
	} {
		got, err := componentLine(t, msg, id, nil)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if got != want {
			t.Errorf("%s:\n got %q\nwant %q", id, got, want)
		}
	}
}

// RFC 9421 sections 2.1.1, 2.1.2 and 2.1.3: the sf, key and bs parameters.
func TestRFC9421_Section21_Parameters(t *testing.T) {
	sf := map[string]string{"example-dict": "dictionary"}
	dictMsg := &RequestMessage{Header: http.Header{"Example-Dict": {"  a=1,    b=2;x=1;y=2,   c=(a   b   c)"}}}
	got, err := componentLine(t, dictMsg, `"example-dict";sf`, sf)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"example-dict";sf: a=1, b=2;x=1;y=2, c=(a b c)`; got != want {
		t.Errorf("sf: %q", got)
	}
	// Section 2.1.1: "the use of this flag will produce an error" for an unknown type.
	if _, err := componentLine(t, dictMsg, `"example-dict";sf`, nil); err == nil {
		t.Error("sf on a field of unknown type must fail")
	}

	keyMsg := &RequestMessage{Header: http.Header{"Example-Dict": {"  a=1, b=2;x=1;y=2, c=(a   b    c), d"}}}
	for id, want := range map[string]string{
		`"example-dict";key="a"`: `"example-dict";key="a": 1`,
		`"example-dict";key="d"`: `"example-dict";key="d": ?1`,
		`"example-dict";key="b"`: `"example-dict";key="b": 2;x=1;y=2`,
		`"example-dict";key="c"`: `"example-dict";key="c": (a b c)`,
	} {
		got, err := componentLine(t, keyMsg, id, nil)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if got != want {
			t.Errorf("%s: %q", id, got)
		}
	}
	if _, err := componentLine(t, keyMsg, `"example-dict";key="zz"`, nil); err == nil {
		t.Error("a missing Dictionary key must fail")
	}

	bsMulti := &RequestMessage{Header: http.Header{"Example-Header": {"value, with, lots", "of, commas"}}}
	if got, _ := componentLine(t, bsMulti, `"example-header"`, nil); got != `"example-header": value, with, lots, of, commas` {
		t.Errorf("multi: %q", got)
	}
	if got, _ := componentLine(t, bsMulti, `"example-header";bs`, nil); got != `"example-header";bs: :dmFsdWUsIHdpdGgsIGxvdHM=:, :b2YsIGNvbW1hcw==:` {
		t.Errorf("bs multi: %q", got)
	}
	bsOne := &RequestMessage{Header: http.Header{"Example-Header": {"value, with, lots, of, commas"}}}
	if got, _ := componentLine(t, bsOne, `"example-header";bs`, nil); got != `"example-header";bs: :dmFsdWUsIHdpdGgsIGxvdHMsIG9mLCBjb21tYXM=:` {
		t.Errorf("bs one: %q", got)
	}

	// RFC 9421 section 2.1.4, the tr parameter reads trailers, not headers.
	trMsg := &RequestMessage{Header: http.Header{"Expires": {"header"}}, Trailer: http.Header{"Expires": {"Wed, 9 Nov 2022 07:28:00 GMT"}}}
	if got, _ := componentLine(t, trMsg, `"expires";tr`, nil); got != `"expires";tr: Wed, 9 Nov 2022 07:28:00 GMT` {
		t.Errorf("tr: %q", got)
	}
}

// RFC 9421 section 2.2, the derived component examples.
func TestRFC9421_Section22_Derived(t *testing.T) {
	line := func(m *RequestMessage, id string) string {
		t.Helper()
		got, err := componentLine(t, m, id, nil)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return got
	}
	server := func(method, target, host string) *RequestMessage {
		r := httptest.NewRequest(method, target, nil)
		r.Host = host
		m := NewRequestMessage(r)
		return &m
	}
	m := server("POST", "/path?param=value", "www.example.com")
	m.Scheme = "https"
	for id, want := range map[string]string{
		`"@method"`:         `"@method": POST`,
		`"@target-uri"`:     `"@target-uri": https://www.example.com/path?param=value`,
		`"@authority"`:      `"@authority": www.example.com`,
		`"@scheme"`:         `"@scheme": https`,
		`"@request-target"`: `"@request-target": /path?param=value`,
		`"@path"`:           `"@path": /path`,
		`"@query"`:          `"@query": ?param=value`,
	} {
		if got := line(m, id); got != want {
			t.Errorf("%s: %q", id, got)
		}
	}
	// Section 2.2.7.
	if got := line(server("GET", "/path?param=value&foo=bar&baz=bat%2Dman", "www.example.com"), `"@query"`); got != `"@query": ?param=value&foo=bar&baz=bat%2Dman` {
		t.Errorf("query: %q", got)
	}
	if got := line(server("POST", "/path?queryString", "www.example.com"), `"@query"`); got != `"@query": ?queryString` {
		t.Errorf("query: %q", got)
	}
	if got := line(server("GET", "/path", "www.example.com"), `"@query"`); got != `"@query": ?` {
		t.Errorf("no query: %q", got)
	}
	// Section 2.2.6: an empty path is a single slash, and percent-encoding is left alone.
	if got := line(&RequestMessage{Method: "GET", Authority: "a"}, `"@path"`); got != `"@path": /` {
		t.Errorf("empty path: %q", got)
	}
	if got := line(server("GET", "/a%2Fb/c%20d?x=1", "a"), `"@path"`); got != `"@path": /a%2Fb/c%20d` {
		t.Errorf("encoded path: %q", got)
	}
	// Section 2.2.8.
	q := server("GET", "/path?param=value&foo=bar&baz=batman&qux=", "www.example.com")
	if got := line(q, `"@query-param";name="baz"`); got != `"@query-param";name="baz": batman` {
		t.Errorf("%q", got)
	}
	if got := line(q, `"@query-param";name="qux"`); got != `"@query-param";name="qux": ` {
		t.Errorf("%q", got)
	}
	if got := line(q, `"@query-param";name="param"`); got != `"@query-param";name="param": value` {
		t.Errorf("%q", got)
	}
	enc := server("GET", "/parameters?var=this%20is%20a%20big%0Amultiline%20value&bar=with+plus+whitespace&fa%C3%A7ade%22%3A%20=something", "www.example.com")
	if got := line(enc, `"@query-param";name="var"`); got != `"@query-param";name="var": this%20is%20a%20big%0Amultiline%20value` {
		t.Errorf("%q", got)
	}
	if got := line(enc, `"@query-param";name="bar"`); got != `"@query-param";name="bar": with%20plus%20whitespace` {
		t.Errorf("%q", got)
	}
	if got := line(enc, `"@query-param";name="fa%C3%A7ade%22%3A%20"`); got != `"@query-param";name="fa%C3%A7ade%22%3A%20": something` {
		t.Errorf("%q", got)
	}
	if _, err := componentLine(t, q, `"@query-param";name="absent"`, nil); err == nil {
		t.Error("an absent query parameter must fail")
	}
	dup := server("GET", "/p?a=1&a=2", "h")
	if _, err := componentLine(t, dup, `"@query-param";name="a"`, nil); err == nil {
		t.Error("a repeated query parameter must fail")
	}
	// Section 2.2.1: the method is not case-folded.
	if got := line(&RequestMessage{Method: "get", Authority: "a", Path: "/"}, `"@method"`); got != `"@method": get` {
		t.Errorf("method: %q", got)
	}
}

// RFC 9421 section 2.2.3: "the hostname is normalized to lowercase, and the default port is
// omitted".
func TestAuthorityNormalization(t *testing.T) {
	cases := []struct{ host, scheme, want string }{
		{"WWW.Example.COM", "https", "www.example.com"},
		{"www.example.com:443", "https", "www.example.com"},
		{"www.example.com:80", "http", "www.example.com"},
		{"www.example.com:80", "https", "www.example.com:80"},
		{"www.example.com:443", "http", "www.example.com:443"},
		{"www.example.com:8443", "https", "www.example.com:8443"},
		{"EXAMPLE.com:8443", "", "example.com:8443"},
		{"www.example.com:443", "", "www.example.com"},
		{"www.example.com:80", "", "www.example.com"},
		{"[::1]:443", "https", "[::1]"},
		{"[::1]:8443", "https", "[::1]:8443"},
		{"127.0.0.1:22260", "http", "127.0.0.1:22260"},
		{"localhost", "http", "localhost"},
	}
	for _, c := range cases {
		if got := NormalizeAuthority(c.host, c.scheme); got != c.want {
			t.Errorf("NormalizeAuthority(%q, %q) = %q, want %q", c.host, c.scheme, got, c.want)
		}
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Host = "PS.Example:443"
	if m := NewRequestMessage(r); m.Authority != "ps.example" {
		t.Errorf("server request authority %q", m.Authority)
	}
	client, _ := http.NewRequest("GET", "https://PS.Example:443/x", nil)
	if m := NewRequestMessage(client); m.Authority != "ps.example" || m.Scheme != "https" {
		t.Errorf("client request %+v", m)
	}
	viaProxy, _ := http.NewRequest("GET", "http://localhost:22260/x", nil)
	if m := NewRequestMessage(viaProxy); m.Authority != "localhost:22260" {
		t.Errorf("port kept: %+v", m)
	}
}

// RFC 9421 section 2.5: every listed error fails the algorithm.
func TestSignatureBaseErrors(t *testing.T) {
	req := rfcTestRequest()
	resp := rfcTestResponse()
	comps := func(ids ...string) SignatureParams {
		t.Helper()
		p := SignatureParams{HasCreated: true, Created: 1}
		for _, id := range ids {
			c, err := ParseComponent(id)
			if err != nil {
				t.Fatal(err)
			}
			p.Components = append(p.Components, c)
		}
		return p
	}
	cases := []struct {
		name string
		resp *ResponseMessage
		ids  []string
	}{
		{"duplicate", nil, []string{`"@method"`, `"@method"`}},
		{"signature-params listed", nil, []string{`"@signature-params"`}},
		{"unknown derived", nil, []string{`"@nonsense"`}},
		{"unknown parameter", nil, []string{`"date";zz`}},
		{"missing field", nil, []string{`"x-absent"`}},
		{"req on a request", nil, []string{`"@method";req`}},
		{"req field on a request", nil, []string{`"date";req`}},
		{"status on a request", nil, []string{`"@status"`}},
		{"bs with sf", nil, []string{`"date";bs;sf`}},
		{"bs with key", nil, []string{`"date";bs;key="a"`}},
		{"sf on derived", nil, []string{`"@method";sf`}},
		{"name on a field", nil, []string{`"date";name="a"`}},
		{"name on other derived", nil, []string{`"@method";name="a"`}},
		{"query-param without name", nil, []string{`"@query-param"`}},
		{"non-boolean req", nil, []string{`"date";req=1`}},
		{"request component on a response without req", &resp, []string{`"@method"`}},
		{"status with req", &resp, []string{`"@status";req`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := SignatureBase(&req, c.resp, comps(c.ids...)); err == nil {
				t.Fatal("want an error")
			}
		})
	}
	// Non-ASCII in the base is an error.
	bad := RequestMessage{Header: http.Header{"X-A": {"caf\xc3\xa9"}}}
	if _, err := SignatureBase(&bad, nil, comps(`"x-a"`)); err == nil {
		t.Error("non-ASCII base must fail")
	}
	// A newline in a value must not forge a base line.
	inject := RequestMessage{Header: http.Header{"X-A": {"a\n\"@method\": GET"}}}
	if _, err := SignatureBase(&inject, nil, comps(`"x-a"`)); err == nil {
		t.Error("a value with a newline must fail")
	}
	// An HTTP request's base can't read a response.
	if _, err := SignatureBase(nil, nil, comps(`"@method"`)); err == nil {
		t.Error("no request must fail")
	}
}

// RFC 9421 section 2.3, the serialized example with its order kept.
func TestSignatureParamsKeepOrder(t *testing.T) {
	const value = `("@target-uri" "@authority" "date" "cache-control");keyid="test-key-rsa-pss";alg="rsa-pss-sha512";created=1618884475;expires=1618884775`
	p := inputMember(t, "sig="+value)
	got, err := p.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if got != value {
		t.Fatalf("got %s", got)
	}
	if !p.HasCreated || p.Created != 1618884475 || !p.HasExpires || p.Expires != 1618884775 || p.KeyID != "test-key-rsa-pss" {
		t.Fatalf("%+v", p)
	}
	// Optional whitespace in the field is normalized by strict serialization.
	spaced := inputMember(t, `sig=( "@method"   "@path" );created=5`)
	got, _ = spaced.Serialize()
	if got != `("@method" "@path");created=5` {
		t.Fatalf("got %s", got)
	}
	// Component parameters are preserved in order.
	ordered := inputMember(t, `sig=("a";bs;tr "b";req);created=5`)
	got, _ = ordered.Serialize()
	if got != `("a";bs;tr "b";req);created=5` {
		t.Fatalf("got %s", got)
	}
}

func TestParseSignatureInputMalformed(t *testing.T) {
	if _, err := ParseSignatureInput([]string{"not a (dictionary"}); err == nil {
		t.Fatal("want an error")
	}
	m, err := ParseSignatureInput([]string{`a="not a list", b=("@method");created=1, c=(1 2);created=1`})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 || m[0].Err == nil || m[1].Err != nil || m[2].Err == nil {
		t.Fatalf("%+v", m)
	}
	if m, _ := ParseSignatureInput([]string{`a=("x");created="1"`}); m[0].Params.HasCreated {
		t.Error("a String created is not an Integer")
	}
	if m, _ := ParseSignatureInput([]string{`a=("x");expires="1"`}); m[0].Err == nil {
		t.Error("a String expires must be an error")
	}
}

func TestAcceptSignature(t *testing.T) {
	// The field in the supervision draft's Mission Endpoint example.
	const want = `sig=("@status" "content-type" "content-digest" "content-digest";req "@authority";req "@path";req)`
	if got := AcceptSignatureForDecision(true); got != want {
		t.Fatalf("got %s", got)
	}
	parsed, err := ParseAcceptSignature([]string{want})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Label != "sig" || len(parsed[0].Components) != 6 || !parsed[0].Components[3].HasReq() {
		t.Fatalf("%+v", parsed)
	}
	// RFC 9421 section 5.1's example carries metadata parameters and still parses.
	r, err := ParseAcceptSignature([]string{`sig1=("@method" "@target-uri" "@authority" "content-digest" "cache-control");keyid="test-key-rsa-pss";created;tag="app-123"`})
	if err != nil || len(r) != 1 || len(r[0].Components) != 5 {
		t.Fatalf("%+v %v", r, err)
	}
	if got := AcceptSignatureForDecision(false); strings.Contains(got, `"content-digest";req`) {
		t.Errorf("a poll has no request digest to bind: %s", got)
	}
}
