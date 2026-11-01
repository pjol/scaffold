package calls

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A minimal HS256 JWT signer and verifier: LiveKit access tokens, server API
// tokens and webhook signatures are all HS256 JWTs keyed by the API secret.
// Hand-rolled instead of importing LiveKit's server SDK, which pulls the
// whole Pion WebRTC stack into every app's build for ~80 lines of HMAC.
// The verifier pins the algorithm (no "none", no RS/ES confusion), compares
// signatures in constant time and checks exp/nbf with a small leeway.

var (
	errTokenMalformed = errors.New("calls: malformed token")
	errTokenAlg       = errors.New("calls: token algorithm must be HS256")
	errTokenSignature = errors.New("calls: token signature mismatch")
	errTokenExpired   = errors.New("calls: token expired or not yet valid")
)

// jwtHeader is the only header we sign and the only algorithm we accept.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// tokenLeeway absorbs clock skew between us and the SFU.
const tokenLeeway = 30 * time.Second

func signHS256(secret []byte, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// registeredClaims are the time and issuer claims every verified token has.
type registeredClaims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub,omitempty"`
	ExpiresAt int64  `json:"exp"`
	NotBefore int64  `json:"nbf,omitempty"`
}

// verifyHS256 checks token against secret and decodes its payload into dst
// (which should embed registeredClaims). exp is required.
func verifyHS256(secret []byte, token string, now time.Time, dst any) error {
	if len(secret) == 0 {
		return errTokenSignature
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return errTokenMalformed
	}
	rawHeader, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errTokenMalformed
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		return errTokenMalformed
	}
	if header.Alg != "HS256" {
		return errTokenAlg
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errTokenMalformed
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return errTokenSignature
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errTokenMalformed
	}
	var reg registeredClaims
	if err := json.Unmarshal(payload, &reg); err != nil {
		return errTokenMalformed
	}
	if reg.ExpiresAt == 0 || now.After(time.Unix(reg.ExpiresAt, 0).Add(tokenLeeway)) {
		return errTokenExpired
	}
	if reg.NotBefore != 0 && now.Add(tokenLeeway).Before(time.Unix(reg.NotBefore, 0)) {
		return errTokenExpired
	}
	if dst != nil {
		if err := json.Unmarshal(payload, dst); err != nil {
			return errTokenMalformed
		}
	}
	return nil
}
