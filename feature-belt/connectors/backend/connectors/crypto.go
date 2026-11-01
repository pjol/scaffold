package connectors

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Secrets at rest (access and refresh tokens, PKCE verifiers, client
// secrets) are sealed with AES-256-GCM. A sealed value is
//
//	0x01 | key id (4 bytes) | nonce (12) | ciphertext+tag
//
// The key id is the first 4 bytes of SHA-256(key), so a value sealed with
// CONNECTORS_ENCRYPTION_KEY_PREVIOUS still opens after a rotation. Every
// value is bound to where it is stored (additional data such as
// "connection:<id>:access"), so a ciphertext copied into another row or
// column fails to open.

const sealVersion = 0x01

// devKeyPhrase derives the development fallback key. It is public on
// purpose: the name says it, and New refuses it outside development.
const devKeyPhrase = "scaffoldapp connectors DEVELOPMENT key - not secret, never use outside development"

var errSealed = errors.New("connectors: sealed value can't be opened (wrong key or tampered)")

type sealer struct {
	keys    map[[4]byte]cipher.AEAD
	current [4]byte
}

// newSealer builds the key ring: current first, then optional previous keys
// (decrypt only).
func newSealer(current []byte, previous ...[]byte) (*sealer, error) {
	s := &sealer{keys: map[[4]byte]cipher.AEAD{}}
	for i, k := range append([][]byte{current}, previous...) {
		if len(k) != 32 {
			return nil, fmt.Errorf("encryption key must be 32 bytes (got %d)", len(k))
		}
		block, err := aes.NewCipher(k)
		if err != nil {
			return nil, err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		id := keyID(k)
		s.keys[id] = gcm
		if i == 0 {
			s.current = id
		}
	}
	return s, nil
}

func keyID(k []byte) [4]byte {
	sum := sha256.Sum256(k)
	return [4]byte(sum[:4])
}

// devKey is the development fallback key (see devKeyPhrase).
func devKey() []byte {
	sum := sha256.Sum256([]byte(devKeyPhrase))
	return sum[:]
}

// parseKey accepts 32 bytes as standard or URL-safe base64, or 64 hex
// characters.
func parseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 64 {
		if b, err := hex.DecodeString(raw); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(raw); err == nil {
			if len(b) != 32 {
				return nil, fmt.Errorf("must decode to 32 bytes (got %d)", len(b))
			}
			return b, nil
		}
	}
	return nil, errors.New("must be 32 random bytes as base64 or 64 hex characters (openssl rand -base64 32)")
}

// seal encrypts plaintext bound to aad. An empty plaintext seals to nil (a
// NULL column).
func (s *sealer) seal(plaintext, aad string) []byte {
	if plaintext == "" {
		return nil
	}
	gcm := s.keys[s.current]
	out := make([]byte, 0, 1+4+gcm.NonceSize()+len(plaintext)+gcm.Overhead())
	out = append(out, sealVersion)
	out = append(out, s.current[:]...)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("connectors: no randomness for a nonce: " + err.Error())
	}
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, []byte(plaintext), []byte(aad))
}

// open decrypts a sealed value. nil opens to "".
func (s *sealer) open(sealed []byte, aad string) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	if len(sealed) < 1+4+12+16 || sealed[0] != sealVersion {
		return "", errSealed
	}
	gcm, ok := s.keys[[4]byte(sealed[1:5])]
	if !ok {
		return "", errSealed
	}
	nonce := sealed[5 : 5+gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, sealed[5+gcm.NonceSize():], []byte(aad))
	if err != nil {
		return "", errSealed
	}
	return string(plain), nil
}

// Additional data for each sealed column.
func aadConnection(id fmt.Stringer, field string) string {
	return "connection:" + id.String() + ":" + field
}

func aadFlow(id fmt.Stringer, field string) string {
	return "flow:" + id.String() + ":" + field
}

func aadConnector(id fmt.Stringer) string {
	return "connector:" + id.String() + ":client_secret"
}

func aadClient(id fmt.Stringer) string {
	return "oauth_client:" + id.String() + ":client_secret"
}

func aadRevocation(id fmt.Stringer, field string) string {
	return "revocation:" + id.String() + ":" + field
}
