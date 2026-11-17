package aauth

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// JWT `typ` values (protocol, AAuth Tokens).
const (
	TypAgent    = "aa-agent+jwt"
	TypPerson   = "aa-person+jwt"
	TypResource = "aa-resource+jwt"
	TypAuth     = "aa-auth+jwt"
)

// Well-known metadata document names, the `dwk` claim and Signature-Key
// parameter (protocol, Keying Material; supervision, Signed Decisions).
const (
	DwkAgent      = "aauth-agent.json"
	DwkPerson     = "aauth-person.json"
	DwkAccess     = "aauth-access.json"
	DwkResource   = "aauth-resource.json"
	DwkSupervisor = "aauth-supervisor.json"
)

// maxJWTSize bounds what is parsed from an unauthenticated caller. Far above any
// token this protocol produces, including post-quantum signatures and attestation
// claims in an agent token.
const maxJWTSize = 64 << 10

// JWTHeader is the protected header of an AAuth JWT.
type JWTHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid,omitempty"`
}

// JWT is a parsed compact JWS. Nothing in it is authenticated until Verify
// returns nil.
type JWT struct {
	Raw       string
	Header    JWTHeader
	Payload   []byte
	Signature []byte

	signingInput string
}

// ErrMalformedJWT is wrapped by every parse failure.
var ErrMalformedJWT = errors.New("malformed JWT")

// ParseJWT parses a compact JWS strictly: three parts, unpadded base64url, a JSON
// object header. A `crit` header is refused, since RFC 7515 requires a recipient
// that does not understand a listed extension to reject the token and this
// protocol defines none.
func ParseJWT(raw string) (*JWT, error) {
	if len(raw) > maxJWTSize {
		return nil, fmt.Errorf("%w: too large", ErrMalformedJWT)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: expected three parts", ErrMalformedJWT)
	}
	headerJSON, err := b64url.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header is not base64url", ErrMalformedJWT)
	}
	payload, err := b64url.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload is not base64url", ErrMalformedJWT)
	}
	sig, err := b64url.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature is not base64url", ErrMalformedJWT)
	}
	var header struct {
		JWTHeader
		Crit json.RawMessage `json:"crit"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: header is not a JSON object", ErrMalformedJWT)
	}
	if header.Crit != nil {
		return nil, fmt.Errorf("%w: crit is not supported", ErrMalformedJWT)
	}
	return &JWT{
		Raw:          raw,
		Header:       header.JWTHeader,
		Payload:      payload,
		Signature:    sig,
		signingInput: parts[0] + "." + parts[1],
	}, nil
}

// Verify checks the token's signature against key. The header alg must be one
// this package implements and must be the alg the key states, so a token can't
// choose an operation its key does not name.
func (t *JWT) Verify(key JWK) error {
	if !isSupportedAlg(t.Header.Alg) {
		return fmt.Errorf("%w: alg %q is not accepted", ErrBadSignature, t.Header.Alg)
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if t.Header.Alg != key.Alg {
		return fmt.Errorf("%w: JWT alg %q is not the key's alg %q", ErrBadSignature, t.Header.Alg, key.Alg)
	}
	return key.Verify([]byte(t.signingInput), t.Signature)
}

// SignJWT creates a compact JWS with the header `{"typ","alg","kid"}` from the
// signer's public JWK.
func SignJWT(key Signer, typ string, claims any) (string, error) {
	pub := key.PublicJWK()
	if !isSupportedAlg(pub.Alg) {
		return "", fmt.Errorf("aauth: cannot sign with alg %q", pub.Alg)
	}
	if pub.Kid == "" {
		return "", errors.New("aauth: signing key has no kid")
	}
	header, err := json.Marshal(JWTHeader{Typ: typ, Alg: pub.Alg, Kid: pub.Kid})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := Base64URL(header) + "." + Base64URL(payload)
	sig, err := key.Sign([]byte(input))
	if err != nil {
		return "", err
	}
	return input + "." + Base64URL(sig), nil
}

// NewJTI returns a random token identifier with 128 bits of entropy.
func NewJTI() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return Base64URL(b[:])
}
