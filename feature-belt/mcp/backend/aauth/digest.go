package aauth

import (
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/dunglas/httpsfv"
)

// Errors from VerifyContentDigest.
var (
	// ErrDigestMismatch reports a digest that does not match the content.
	ErrDigestMismatch = errors.New("content digest does not match the content")
	// ErrDigestMalformed reports a field that is not an RFC 9530 Dictionary of
	// Byte Sequences.
	ErrDigestMalformed = errors.New("Content-Digest is malformed")
	// ErrDigestUnsupported reports a field naming no algorithm this package
	// implements, so nothing could be checked.
	ErrDigestUnsupported = errors.New("Content-Digest names no supported algorithm")
)

// ContentDigest returns the RFC 9530 Content-Digest field value for content,
// using sha-256.
func ContentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

// VerifyContentDigest checks every supported algorithm in the field against
// content. Unknown algorithms are ignored, but at least one supported algorithm
// must be present: a field that verifies nothing proves nothing.
func VerifyContentDigest(fieldValues []string, content []byte) error {
	dict, err := httpsfv.UnmarshalDictionary(fieldValues)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDigestMalformed, err)
	}
	checked := false
	for _, name := range dict.Names() {
		var sum []byte
		switch name {
		case "sha-256":
			s := sha256.Sum256(content)
			sum = s[:]
		case "sha-512":
			s := sha512.Sum512(content)
			sum = s[:]
		default:
			continue
		}
		member, _ := dict.Get(name)
		item, ok := member.(httpsfv.Item)
		if !ok {
			return fmt.Errorf("%w: %s is not an Item", ErrDigestMalformed, name)
		}
		got, ok := item.Value.([]byte)
		if !ok {
			return fmt.Errorf("%w: %s is not a Byte Sequence", ErrDigestMalformed, name)
		}
		if subtle.ConstantTimeCompare(got, sum) != 1 {
			return ErrDigestMismatch
		}
		checked = true
	}
	if !checked {
		return ErrDigestUnsupported
	}
	return nil
}
