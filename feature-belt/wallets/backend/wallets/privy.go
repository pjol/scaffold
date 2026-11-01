package wallets

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// privyIssuer is the iss claim on Privy tokens (the bare name, not the API host).
const privyIssuer = "privy.io"

// privyVerifier checks Privy identity tokens locally: ES256 against the app's
// verification key (Privy dashboard → App settings → Verification key), issuer,
// audience (the app id) and expiry. No network call on the request path.
type privyVerifier struct {
	appID string
	key   *ecdsa.PublicKey
}

func newPrivyVerifier(appID, keyPEM string) (*privyVerifier, error) {
	key, err := parseP256PublicKey(keyPEM)
	if err != nil {
		return nil, err
	}
	return &privyVerifier{appID: appID, key: key}, nil
}

// parseP256PublicKey reads an SPKI public key, tolerating the ways a PEM gets
// mangled on one env line: literal "\n" escapes, lost newlines, or the bare
// base64 body without armor.
func parseP256PublicKey(s string) (*ecdsa.PublicKey, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), `\n`, "\n")
	s = strings.ReplaceAll(s, `\`, "") // doubled escapes; base64 never contains a backslash
	s = strings.ReplaceAll(s, "-----BEGIN PUBLIC KEY-----", "")
	s = strings.ReplaceAll(s, "-----END PUBLIC KEY-----", "")
	body := strings.Join(strings.Fields(s), "")
	der, err := base64.StdEncoding.DecodeString(body)
	if err != nil || body == "" {
		return nil, errors.New("expected the PEM verification key from the Privy dashboard (not the app secret)")
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse verification key: %w", err)
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok || ec.Curve != elliptic.P256() {
		return nil, errors.New("verification key must be an ES256 (P-256) public key")
	}
	return ec, nil
}

// privyIdentity is what a verified identity token tells us.
type privyIdentity struct {
	Subject string    // Privy user id (did:privy:…)
	Wallets []Address // embedded Ethereum wallets and Privy smart wallets
}

var errPrivyToken = errors.New("invalid Privy identity token")

// verify checks an identity token (from the Privy SDK's getIdentityToken /
// the privy-id-token cookie). Access tokens carry no linked accounts, so they
// verify but list no wallets.
func (p *privyVerifier) verify(token string, now time.Time) (*privyIdentity, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || len(token) > 32<<10 {
		return nil, errPrivyToken
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := decodeSegment(parts[0], &header); err != nil || header.Alg != "ES256" {
		return nil, errPrivyToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return nil, errPrivyToken
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(p.key, digest[:], r, s) {
		return nil, errPrivyToken
	}
	var claims struct {
		Iss            string          `json:"iss"`
		Sub            string          `json:"sub"`
		Aud            json.RawMessage `json:"aud"`
		Exp            *float64        `json:"exp"`
		Nbf            *float64        `json:"nbf"`
		LinkedAccounts json.RawMessage `json:"linked_accounts"`
	}
	if err := decodeSegment(parts[1], &claims); err != nil {
		return nil, errPrivyToken
	}
	if claims.Iss != privyIssuer || claims.Sub == "" || !audienceHas(claims.Aud, p.appID) {
		return nil, errPrivyToken
	}
	if claims.Exp == nil || !now.Before(time.Unix(int64(*claims.Exp), 0)) {
		return nil, errPrivyToken
	}
	if claims.Nbf != nil && now.Add(clockSkew).Before(time.Unix(int64(*claims.Nbf), 0)) {
		return nil, errPrivyToken
	}
	return &privyIdentity{Subject: claims.Sub, Wallets: embeddedWallets(claims.LinkedAccounts)}, nil
}

func decodeSegment(seg string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func audienceHas(raw json.RawMessage, appID string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == appID
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == appID {
				return true
			}
		}
	}
	return false
}

// embeddedWallets reads linked_accounts (a JSON string or an array) and keeps
// Privy-managed Ethereum wallets: embedded wallets (wallet_client_type
// "privy" / connector_type "embedded") and Privy smart wallets. External
// wallets the user connected to Privy are skipped; those link through SIWE.
func embeddedWallets(raw json.RawMessage) []Address {
	type account struct {
		Type             string `json:"type"`
		Address          string `json:"address"`
		ChainType        string `json:"chain_type"`
		WalletClientType string `json:"wallet_client_type"`
		ConnectorType    string `json:"connector_type"`
	}
	var accounts []account
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		raw = json.RawMessage(asString)
	}
	if json.Unmarshal(raw, &accounts) != nil {
		return nil
	}
	var out []Address
	seen := map[Address]bool{}
	for _, a := range accounts {
		embedded := a.Type == "wallet" && a.ChainType == "ethereum" &&
			(a.WalletClientType == "privy" || a.ConnectorType == "embedded")
		if !embedded && a.Type != "smart_wallet" {
			continue
		}
		addr, err := ParseAddress(a.Address)
		if err != nil || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}
