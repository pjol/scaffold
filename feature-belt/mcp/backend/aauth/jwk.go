package aauth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Alg is a fully-specified JOSE signature algorithm identifier.
type Alg string

// The algorithms this package implements. "Every party MUST support Ed25519 and
// SHOULD support ES256" (protocol, Signature Algorithms).
const (
	AlgEd25519 Alg = "Ed25519"
	AlgES256   Alg = "ES256"
)

// SupportedAlgs returns the implemented algorithms in preference order. A new
// slice each call, so callers can't alter the package's state.
func SupportedAlgs() []Alg { return []Alg{AlgEd25519, AlgES256} }

func isSupportedAlg(a string) bool { return a == string(AlgEd25519) || a == string(AlgES256) }

var b64url = base64.RawURLEncoding.Strict()

// Base64URL encodes without padding, the only form JOSE uses.
func Base64URL(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// KeyError reports a problem with a JWK. Code is the Signature-Error code the
// protocol's Verification section assigns.
type KeyError struct {
	Code SignatureErrorCode
	// Incomplete marks a JWK missing kty or the members its key type requires.
	// The spec rejects that "as structurally incomplete before attempting key
	// decoding" (protocol, Request-Context Binding).
	Incomplete bool
	Detail     string
}

func (e *KeyError) Error() string { return string(e.Code) + ": " + e.Detail }

func keyErr(code SignatureErrorCode, format string, args ...any) *KeyError {
	return &KeyError{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// JWK is a public JSON Web Key. It holds no private material: parsing a JWK
// that carries any is an error reported by Validate, and marshalling never
// emits it. PrivateKey is the private counterpart.
type JWK struct {
	Kty string `json:"kty,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Alg string `json:"alg,omitempty"`
	Kid string `json:"kid,omitempty"`
	Use string `json:"use,omitempty"`

	hasPrivate bool
}

// UnmarshalJSON reads a JWK, ignoring members it does not understand ("a member
// a verifier does not understand is ignored").
func (k *JWK) UnmarshalJSON(b []byte) error {
	type plain JWK
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var private struct {
		D json.RawMessage `json:"d"`
		K json.RawMessage `json:"k"`
	}
	if err := json.Unmarshal(b, &private); err != nil {
		return err
	}
	*k = JWK(p)
	k.hasPrivate = private.D != nil || private.K != nil
	return nil
}

// ParseJWK decodes a JWK without judging it. Use Validate to judge it.
func ParseJWK(data []byte) (JWK, error) {
	var k JWK
	if err := json.Unmarshal(data, &k); err != nil {
		return JWK{}, fmt.Errorf("parse JWK: %w", err)
	}
	return k, nil
}

// CheckStructure applies the structural check on key members: kty, then the
// members that key type requires. A kty this package does not implement is not
// a structural problem: "Absence of support for a key type is a reason to
// decline, not a parsing failure."
func (k JWK) CheckStructure() error {
	missing := func(member string) error {
		return &KeyError{Code: SigInvalidKey, Incomplete: true, Detail: "JWK is missing " + member}
	}
	if k.Kty == "" {
		return missing("kty")
	}
	var required map[string]string
	switch k.Kty {
	case "OKP":
		required = map[string]string{"crv": k.Crv, "x": k.X}
	case "EC":
		required = map[string]string{"crv": k.Crv, "x": k.X, "y": k.Y}
	case "RSA":
		required = map[string]string{"n": k.N, "e": k.E}
	}
	for _, m := range []string{"crv", "x", "y", "n", "e"} {
		if v, ok := required[m]; ok && v == "" {
			return missing(m)
		}
	}
	return nil
}

// Algorithm applies Algorithm Determination (signature-key draft): the algorithm
// comes from `alg` and only from `alg`, and kty and crv must agree with it.
//
//   - unsupported_algorithm: `alg` is absent, polymorphic, `none`, symmetric, or
//     names an algorithm or key type this package does not implement.
//   - invalid_key: `alg` is one this package implements, and kty or crv disagree.
func (k JWK) Algorithm() (Alg, error) {
	switch {
	case k.Alg == "":
		return "", keyErr(SigUnsupportedAlgorithm, "JWK has no alg member")
	case k.Alg == "EdDSA":
		return "", keyErr(SigUnsupportedAlgorithm, "the polymorphic EdDSA identifier must not be used")
	case k.Alg == "none":
		return "", keyErr(SigUnsupportedAlgorithm, "alg none must not be used")
	case strings.HasPrefix(k.Alg, "HS"):
		return "", keyErr(SigUnsupportedAlgorithm, "symmetric algorithms must not be used")
	}
	switch Alg(k.Alg) {
	case AlgEd25519:
		if k.Kty != "OKP" {
			return "", keyErr(SigInvalidKey, "alg Ed25519 disagrees with kty %q", k.Kty)
		}
		if k.Crv != "" && k.Crv != "Ed25519" {
			return "", keyErr(SigInvalidKey, "alg Ed25519 disagrees with crv %q", k.Crv)
		}
		return AlgEd25519, nil
	case AlgES256:
		if k.Kty != "EC" {
			return "", keyErr(SigInvalidKey, "alg ES256 disagrees with kty %q", k.Kty)
		}
		if k.Crv != "" && k.Crv != "P-256" {
			return "", keyErr(SigInvalidKey, "alg ES256 disagrees with crv %q", k.Crv)
		}
		return AlgES256, nil
	}
	return "", keyErr(SigUnsupportedAlgorithm, "alg %q is not implemented", k.Alg)
}

// Validate runs every check in order: structure, algorithm, private material,
// key decoding. On success the key is usable for verification.
func (k JWK) Validate() error {
	if err := k.CheckStructure(); err != nil {
		return err
	}
	if _, err := k.Algorithm(); err != nil {
		return err
	}
	if k.hasPrivate {
		return keyErr(SigInvalidKey, "JWK carries private key material")
	}
	_, err := k.publicKey()
	return err
}

// publicKey decodes the key material. Callers run Algorithm first.
func (k JWK) publicKey() (any, error) {
	switch Alg(k.Alg) {
	case AlgEd25519:
		x, err := b64url.DecodeString(k.X)
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, keyErr(SigInvalidKey, "Ed25519 x is not 32 base64url octets")
		}
		return ed25519.PublicKey(x), nil
	case AlgES256:
		x, errX := b64url.DecodeString(k.X)
		y, errY := b64url.DecodeString(k.Y)
		if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
			return nil, keyErr(SigInvalidKey, "ES256 x and y are not 32 base64url octets")
		}
		point := append(append([]byte{4}, x...), y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
		if err != nil {
			return nil, keyErr(SigInvalidKey, "ES256 point is not on P-256")
		}
		return pub, nil
	}
	return nil, keyErr(SigUnsupportedAlgorithm, "alg %q is not implemented", k.Alg)
}

// ErrBadSignature reports a signature that does not verify.
var ErrBadSignature = errors.New("signature does not verify")

// Verify checks a JOSE-form signature (raw Ed25519, or r||s for ES256) over
// message, using the algorithm the key states.
func (k JWK) Verify(message, signature []byte) error {
	if _, err := k.Algorithm(); err != nil {
		return err
	}
	pub, err := k.publicKey()
	if err != nil {
		return err
	}
	switch p := pub.(type) {
	case ed25519.PublicKey:
		if len(signature) != ed25519.SignatureSize || !ed25519.Verify(p, message, signature) {
			return ErrBadSignature
		}
		return nil
	case *ecdsa.PublicKey:
		if len(signature) != 64 {
			return ErrBadSignature
		}
		digest := sha256.Sum256(message)
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		if !ecdsa.Verify(p, digest[:], r, s) {
			return ErrBadSignature
		}
		return nil
	}
	return ErrBadSignature
}

// Thumbprint computes the RFC 7638 JWK Thumbprint with SHA-256: the required
// members for the key type, in lexicographic order, with no whitespace.
func (k JWK) Thumbprint() (string, error) {
	var members [][2]string
	switch k.Kty {
	case "OKP":
		members = [][2]string{{"crv", k.Crv}, {"kty", k.Kty}, {"x", k.X}}
	case "EC":
		members = [][2]string{{"crv", k.Crv}, {"kty", k.Kty}, {"x", k.X}, {"y", k.Y}}
	case "RSA":
		members = [][2]string{{"e", k.E}, {"kty", k.Kty}, {"n", k.N}}
	default:
		return "", keyErr(SigUnsupportedAlgorithm, "no thumbprint for kty %q", k.Kty)
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range members {
		if m[1] == "" {
			return "", keyErr(SigInvalidKey, "JWK is missing %s", m[0])
		}
		if i > 0 {
			b.WriteByte(',')
		}
		name, _ := json.Marshal(m[0])
		value, _ := json.Marshal(m[1])
		b.Write(name)
		b.WriteByte(':')
		b.Write(value)
	}
	b.WriteByte('}')
	sum := sha256.Sum256([]byte(b.String()))
	return Base64URL(sum[:]), nil
}

// SameKey reports whether two JWKs are the same public key, by thumbprint.
func (k JWK) SameKey(other JWK) bool {
	a, errA := k.Thumbprint()
	b, errB := other.Thumbprint()
	return errA == nil && errB == nil && a == b
}

// JWKS is a JSON Web Key Set, as published at jwks_uri. Every key an AAuth
// server publishes MUST carry a fully-specified alg (protocol, JWKS Discovery).
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// SelectJWK returns the member matching kid from raw JWKS bytes.
//
// "A verifier MUST select the key matching kid without requiring any other
// member of the JWKS to be usable, and MUST NOT fail because an unselected
// member names a key type or algorithm it does not implement." So only `kid` is
// read from each member until the match, and a malformed member is passed over.
func SelectJWK(raw []byte, kid string) (JWK, bool, error) {
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		return JWK{}, false, fmt.Errorf("parse JWKS: %w", err)
	}
	if set.Keys == nil {
		return JWK{}, false, errors.New("parse JWKS: no keys member")
	}
	for _, m := range set.Keys {
		var id struct {
			Kid string `json:"kid"`
		}
		if json.Unmarshal(m, &id) != nil || id.Kid != kid {
			continue
		}
		k, err := ParseJWK(m)
		if err != nil {
			return JWK{}, true, keyErr(SigInvalidKey, "selected JWK is malformed")
		}
		return k, true, nil
	}
	return JWK{}, false, nil
}

// Signer produces JOSE-form signatures. *PrivateKey implements it; a KMS-backed
// key can too. The public JWK states the algorithm and kid.
type Signer interface {
	PublicJWK() JWK
	Sign(message []byte) ([]byte, error)
}

// PrivateKey is an Ed25519 or ES256 signing key.
type PrivateKey struct {
	pub JWK
	ed  ed25519.PrivateKey
	ec  *ecdsa.PrivateKey
}

// GenerateKey creates a key for alg. An empty kid uses the key's thumbprint.
func GenerateKey(alg Alg, kid string) (*PrivateKey, error) {
	k := &PrivateKey{}
	switch alg {
	case AlgEd25519:
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		k.ed = priv
		k.pub = JWK{Kty: "OKP", Crv: "Ed25519", X: Base64URL(pub), Alg: string(alg)}
	case AlgES256:
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		x, y, err := ecPoint(&priv.PublicKey)
		if err != nil {
			return nil, err
		}
		k.ec = priv
		k.pub = JWK{Kty: "EC", Crv: "P-256", X: x, Y: y, Alg: string(alg)}
	default:
		return nil, fmt.Errorf("aauth: cannot generate %q keys", alg)
	}
	if kid == "" {
		tp, err := k.pub.Thumbprint()
		if err != nil {
			return nil, err
		}
		kid = tp
	}
	k.pub.Kid = kid
	return k, nil
}

func ecPoint(pub *ecdsa.PublicKey) (x, y string, err error) {
	raw, err := pub.Bytes()
	if err != nil {
		return "", "", err
	}
	if len(raw) != 65 || raw[0] != 4 {
		return "", "", errors.New("aauth: unexpected P-256 point encoding")
	}
	return Base64URL(raw[1:33]), Base64URL(raw[33:]), nil
}

// ParsePrivateJWK reads a private JWK. It requires the same fully-specified alg
// as a public one, and checks the private part matches the public part.
func ParsePrivateJWK(data []byte) (*PrivateKey, error) {
	var raw struct {
		D string `json:"d"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse private JWK: %w", err)
	}
	pub, err := ParseJWK(data)
	if err != nil {
		return nil, err
	}
	if raw.D == "" {
		return nil, errors.New("parse private JWK: no d member")
	}
	pub.hasPrivate = false
	if err := pub.Validate(); err != nil {
		return nil, fmt.Errorf("parse private JWK: %w", err)
	}
	d, err := b64url.DecodeString(raw.D)
	if err != nil {
		return nil, errors.New("parse private JWK: d is not base64url")
	}
	k := &PrivateKey{pub: pub}
	switch Alg(pub.Alg) {
	case AlgEd25519:
		if len(d) != ed25519.SeedSize {
			return nil, errors.New("parse private JWK: Ed25519 d is not 32 octets")
		}
		k.ed = ed25519.NewKeyFromSeed(d)
		if Base64URL(k.ed.Public().(ed25519.PublicKey)) != pub.X {
			return nil, errors.New("parse private JWK: d does not match x")
		}
	case AlgES256:
		priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
		if err != nil {
			return nil, fmt.Errorf("parse private JWK: %w", err)
		}
		x, y, err := ecPoint(&priv.PublicKey)
		if err != nil {
			return nil, err
		}
		if x != pub.X || y != pub.Y {
			return nil, errors.New("parse private JWK: d does not match x and y")
		}
		k.ec = priv
	}
	return k, nil
}

// MarshalPrivateJWK serializes the key with its private part, for encrypted
// storage. Never log or publish the result.
func (k *PrivateKey) MarshalPrivateJWK() ([]byte, error) {
	var d []byte
	switch {
	case k.ed != nil:
		d = k.ed.Seed()
	case k.ec != nil:
		raw, err := k.ec.Bytes()
		if err != nil {
			return nil, err
		}
		d = raw
	default:
		return nil, errors.New("aauth: empty private key")
	}
	type private struct {
		JWK
		D string `json:"d"`
	}
	return json.Marshal(private{JWK: k.pub, D: Base64URL(d)})
}

// PublicJWK returns the public key with its alg and kid.
func (k *PrivateKey) PublicJWK() JWK { return k.pub }

// Sign returns a JOSE-form signature over message.
func (k *PrivateKey) Sign(message []byte) ([]byte, error) {
	switch {
	case k.ed != nil:
		return ed25519.Sign(k.ed, message), nil
	case k.ec != nil:
		digest := sha256.Sum256(message)
		r, s, err := ecdsa.Sign(rand.Reader, k.ec, digest[:])
		if err != nil {
			return nil, err
		}
		out := make([]byte, 64)
		r.FillBytes(out[:32])
		s.FillBytes(out[32:])
		return out, nil
	}
	return nil, errors.New("aauth: empty private key")
}
