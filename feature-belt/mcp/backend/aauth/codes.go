package aauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// MinInteractionCodeSymbols is the floor: "A code MUST carry at least 40 bits of
// entropy, at least 8 Crockford base32 symbols".
const MinInteractionCodeSymbols = 8

// NewInteractionCode returns a code of the given length in canonical form, drawn from
// crypto/rand: uppercase Crockford base32 with no hyphens. Fewer than
// MinInteractionCodeSymbols is an error.
func NewInteractionCode(symbols int) (string, error) {
	if symbols < MinInteractionCodeSymbols {
		return "", fmt.Errorf("aauth: an interaction code needs at least %d symbols", MinInteractionCodeSymbols)
	}
	// Five bits per symbol with no modulo, so every symbol is uniform.
	raw := make([]byte, (symbols*5+7)/8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(symbols)
	var acc, bits uint
	i := 0
	for n := 0; n < symbols; n++ {
		for bits < 5 {
			acc = acc<<8 | uint(raw[i])
			i++
			bits += 8
		}
		bits -= 5
		b.WriteByte(crockfordAlphabet[(acc>>bits)&31])
		acc &= (1 << bits) - 1
	}
	return b.String(), nil
}

// FormatInteractionCode inserts hyphens for display, every four symbols: A1B2-C3D4. "The
// hyphen carries no entropy and is not part of the code's value."
func FormatInteractionCode(code string) string {
	code = strings.ReplaceAll(code, "-", "")
	var b strings.Builder
	for i := 0; i < len(code); i++ {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(code[i])
	}
	return b.String()
}

// ErrInvalidInteractionCode reports input that can't be a code.
var ErrInvalidInteractionCode = errors.New("not a valid interaction code")

// NormalizeInteractionCode applies the comparison rules: strip all hyphens, fold case,
// and fold the Crockford decode aliases (I and L to 1, O to 0). Input with a symbol
// outside the alphabet after that, such as U, is an error.
func NormalizeInteractionCode(input string) (string, error) {
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c == '-' {
			continue
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		switch c {
		case 'I', 'L':
			c = '1'
		case 'O':
			c = '0'
		}
		if strings.IndexByte(crockfordAlphabet, c) < 0 {
			return "", ErrInvalidInteractionCode
		}
		b.WriteByte(c)
	}
	if b.Len() < MinInteractionCodeSymbols {
		return "", ErrInvalidInteractionCode
	}
	return b.String(), nil
}

// InteractionCodesEqual compares two codes in constant time after normalizing both. Bad
// input compares unequal. The code is a correlation identifier: "the code alone MUST NOT
// authorize the decision".
func InteractionCodesEqual(a, b string) bool {
	na, errA := NormalizeInteractionCode(a)
	nb, errB := NormalizeInteractionCode(b)
	if errA != nil || errB != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(na), []byte(nb)) == 1
}

// minDirectedKeyBytes is the shortest HMAC key DirectedSub accepts.
const minDirectedKeyBytes = 32

// DirectedSub derives the pairwise `sub` for a person at an audience: an HMAC-SHA-256
// keyed with a server secret, over a length-prefixed encoding of (person id, audience),
// so no two pairs can encode the same way. The value is the first 16 bytes in hex, the
// form of the spec's examples.
//
// It takes no agent or key, since a sub "MUST NOT vary with the agent or its key". The
// same function serves a resource and a supervision server: "the directed identifier the
// PS derives for this SS as audience, by the same rule".
func DirectedSub(key []byte, personID, audience string) (string, error) {
	if len(key) < minDirectedKeyBytes {
		return "", fmt.Errorf("aauth: the directed sub key needs at least %d bytes", minDirectedKeyBytes)
	}
	if personID == "" || audience == "" {
		return "", errors.New("aauth: a directed sub needs a person and an audience")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("aauth-directed-sub-v1\x00"))
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(personID)))
	mac.Write(n[:])
	mac.Write([]byte(personID))
	binary.BigEndian.PutUint64(n[:], uint64(len(audience)))
	mac.Write(n[:])
	mac.Write([]byte(audience))
	return hex.EncodeToString(mac.Sum(nil)[:16]), nil
}
