package wallets

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestAddressChecksum(t *testing.T) {
	// EIP-55 test vectors.
	for _, want := range []string{
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
	} {
		a, err := ParseAddress(strings.ToLower(want))
		if err != nil {
			t.Fatal(err)
		}
		if a.Hex() != want {
			t.Errorf("Hex() = %s, want %s", a.Hex(), want)
		}
		if _, err := ParseAddress(want); err != nil {
			t.Errorf("valid checksum rejected: %s", want)
		}
	}
	if _, err := ParseAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAeD"); err == nil {
		t.Error("bad checksum accepted")
	}
	if _, err := ParseAddress("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beae"); err == nil {
		t.Error("short address accepted")
	}
}

func TestRecoverPersonalSign(t *testing.T) {
	// cast wallet sign "hello" with Anvil account 0.
	sig, _ := decodeHex("0xf16ea9a3478698f695fd1401bfe27e9e4a7e8e3da94aa72b021125e31fa899cc573c48ea3fe1d4ab61a9db10c19032026e3ed2dbccba5a178235ac27f94504311c")
	got, err := recoverAddress(personalHash([]byte("hello")), sig)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hex() != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" {
		t.Errorf("recovered %s", got.Hex())
	}
	sig[64] -= 27 // v as 0/1
	if got2, err := recoverAddress(personalHash([]byte("hello")), sig); err != nil || got2 != got {
		t.Errorf("v=0/1 form: %v %s", err, got2.Hex())
	}
	if got3, _ := recoverAddress(personalHash([]byte("hellO")), sig); got3 == got {
		t.Error("different message recovered the same signer")
	}
}

func TestSIWERoundTrip(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	exp := now.Add(10 * time.Minute)
	addr, _ := ParseAddress("0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266")
	m := siweMessage{Domain: "app.example.com", Address: addr.Hex(), Statement: "Link this wallet.",
		URI: "https://app.example.com", Version: "1", ChainID: 8453, Nonce: "abc123def456", IssuedAt: now, ExpirationTime: &exp}
	parsed, err := parseSIWE(m.String())
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, m.String())
	}
	if parsed.String() != m.String() {
		t.Errorf("round trip changed the message:\n%s\n---\n%s", parsed.String(), m.String())
	}
	want := siweExpect{Domain: "app.example.com", URI: "https://app.example.com", Address: addr, ChainID: 8453, Nonce: "abc123def456"}
	if err := parsed.verify(want, now.Add(time.Minute)); err != nil {
		t.Errorf("verify: %v", err)
	}
	if err := parsed.verify(want, exp.Add(time.Second)); err == nil {
		t.Error("expired message verified")
	}
	bad := want
	bad.Domain = "evil.example"
	if err := parsed.verify(bad, now); err == nil {
		t.Error("wrong domain verified")
	}
	bad = want
	bad.ChainID = 1
	if err := parsed.verify(bad, now); err == nil {
		t.Error("wrong chain verified")
	}

	// No statement, viem-style single blank line, CRLF.
	raw := "app.example.com wants you to sign in with your Ethereum account:\r\n" + addr.Hex() + "\r\n\r\nURI: https://app.example.com\r\nVersion: 1\r\nChain ID: 8453\r\nNonce: abc123def456\r\nIssued At: 2026-10-30T12:00:00.000Z"
	if p, err := parseSIWE(raw); err != nil || p.Statement != "" || p.Nonce != "abc123def456" {
		t.Errorf("library layout: %v %+v", err, p)
	}
	if _, err := parseSIWE("hello"); err == nil {
		t.Error("garbage parsed")
	}
}

func TestFormatUnits(t *testing.T) {
	for _, c := range []struct {
		amount string
		dec    int
		want   string
	}{
		{"1500000", 6, "1.5"}, {"1000000", 6, "1"}, {"1", 6, "0.000001"}, {"0", 18, "0"},
		{"123", 0, "123"}, {"1000000000000000000", 18, "1"}, {"25000000000000000000", 18, "25"},
	} {
		if got := formatUnits(c.amount, c.dec); got != c.want {
			t.Errorf("formatUnits(%s, %d) = %s, want %s", c.amount, c.dec, got, c.want)
		}
	}
}

func TestParseChains(t *testing.T) {
	ok := `[{"id":8453,"name":"Base","rpc_url":"https://rpc.example/KEY","explorer_url":"https://basescan.org/",
	  "tokens":[{"address":"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913","symbol":"USDC","decimals":6}]}]`
	r, err := ParseChains(ok, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.DefaultChainID() != 8453 || *r.Chains()[0].NativeDecimals != 18 || r.Chains()[0].Tokens[0].Address != "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913" {
		t.Errorf("unexpected registry: %+v", r.Chains()[0])
	}
	if u := r.TxURL(8453, "0xabc"); u == nil || *u != "https://basescan.org/tx/0xabc" {
		t.Errorf("TxURL = %v", u)
	}
	pub, _ := json.Marshal(r.public())
	if strings.Contains(string(pub), "KEY") {
		t.Error("public config leaks the RPC URL")
	}
	for name, raw := range map[string]string{
		"multiplier":   strings.Replace(ok, `"decimals":6`, `"decimals":1000000`, 1),
		"no decimals":  strings.Replace(ok, `,"decimals":6`, ``, 1),
		"unknown":      strings.Replace(ok, `"decimals":6`, `"token_decimals":6`, 1),
		"local in prd": strings.Replace(ok, "https://rpc.example/KEY", "http://127.0.0.1:8545", 1),
		"duplicate":    `[{"id":1,"name":"A","rpc_url":"https://a"},{"id":1,"name":"B","rpc_url":"https://b"}]`,
	} {
		if _, err := ParseChains(raw, 0, true); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ParseChains(ok, 1, false); err == nil {
		t.Error("default chain not in list accepted")
	}
}

func TestPrivyVerify(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pemKey := strings.ReplaceAll(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "\n", `\n`)
	v, err := newPrivyVerifier("app-123", pemKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sign := func(claims map[string]any) string {
		h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT"}`))
		c, _ := json.Marshal(claims)
		input := h + "." + base64.RawURLEncoding.EncodeToString(c)
		d := sha256.Sum256([]byte(input))
		r, s, _ := ecdsa.Sign(rand.Reader, key, d[:])
		sig := make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
		return input + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	linked := `[{"type":"email","address":"a@b.c"},
	  {"type":"wallet","address":"0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266","chain_type":"ethereum","wallet_client_type":"privy"},
	  {"type":"wallet","address":"0x70997970c51812dc3a010c7d01b50e0d17dc79c8","chain_type":"ethereum","wallet_client_type":"metamask"}]`
	claims := map[string]any{"iss": "privy.io", "aud": "app-123", "sub": "did:privy:abc", "exp": now.Add(time.Hour).Unix(), "linked_accounts": linked}
	id, err := v.verify(sign(claims), now)
	if err != nil {
		t.Fatal(err)
	}
	if id.Subject != "did:privy:abc" || len(id.Wallets) != 1 || id.Wallets[0].Hex() != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" {
		t.Errorf("identity = %+v", id)
	}
	for name, mutate := range map[string]func(map[string]any){
		"wrong aud": func(c map[string]any) { c["aud"] = "other" },
		"wrong iss": func(c map[string]any) { c["iss"] = "auth.privy.io" },
		"expired":   func(c map[string]any) { c["exp"] = now.Add(-time.Minute).Unix() },
		"no exp":    func(c map[string]any) { delete(c, "exp") },
	} {
		c := map[string]any{}
		for k, val := range claims {
			c[k] = val
		}
		mutate(c)
		if _, err := v.verify(sign(c), now); err == nil {
			t.Errorf("%s: verified", name)
		}
	}
	tok := sign(claims)
	if _, err := v.verify(tok[:len(tok)-4]+"AAAA", now); err == nil {
		t.Error("tampered signature verified")
	}
}

func TestIsValidSignatureData(t *testing.T) {
	hash := make([]byte, 32)
	sig := make([]byte, 65)
	data := isValidSignatureData(hash, sig)
	// selector + hash + offset + length + 65 bytes padded to 96
	if len(data) != 2+2*(4+32+32+32+96) || !strings.HasPrefix(data, "0x1626ba7e") {
		t.Errorf("calldata length %d: %s", len(data), data[:20])
	}
}
