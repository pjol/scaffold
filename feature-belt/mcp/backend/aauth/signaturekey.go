package aauth

import (
	"errors"
	"fmt"

	"github.com/dunglas/httpsfv"
)

// Scheme is a Signature-Key scheme token. AAuth uses jwt and jwks_uri; the rest
// are named so policy and error paths can refer to them.
type Scheme string

// The schemes of the Signature-Key scheme registry.
const (
	SchemeJWT     Scheme = "jwt"
	SchemeJWKSURI Scheme = "jwks_uri"
	SchemeHWK     Scheme = "hwk"
	SchemeJKTJWT  Scheme = "jkt-jwt"
	SchemeJWKS    Scheme = "jwks"
	SchemeSelfJWT Scheme = "self-jwt"
	SchemeX509    Scheme = "x509"
	SchemeCached  Scheme = "cached"
)

// DefaultSignatureLabel is the label AAuth's examples use. Any label is valid;
// labels are correlated by equality across the three fields.
const DefaultSignatureLabel = "sig"

// SignatureKeyMember is one member of a Signature-Key Dictionary.
//
// Params are only read for the scheme named, and a parameter the scheme does not
// define is ignored. Err is set when the member's value is not a scheme token at
// all, and a required parameter is checked by the verifier for the member it
// selects: "A verifier MUST NOT reject a request because a member it did not
// select names a scheme the verifier does not implement".
type SignatureKeyMember struct {
	Label  string
	Scheme Scheme
	// JWT is the compact token, for the jwt scheme.
	JWT string
	// Cache is the jwt scheme's request for a cache identifier.
	Cache bool
	// ID, Dwk and Kid are the jwks_uri scheme's parameters.
	ID  string
	Dwk string
	Kid string
	Err error
}

// JWTKeyMember builds a jwt scheme member: `sig=jwt;jwt="eyJ..."`.
func JWTKeyMember(label, token string) SignatureKeyMember {
	return SignatureKeyMember{Label: label, Scheme: SchemeJWT, JWT: token}
}

// JWKSURIKeyMember builds a jwks_uri scheme member:
// `sig=jwks_uri;id="https://ps.example";dwk="aauth-person.json";kid="key-1"`.
func JWKSURIKeyMember(label, id, dwk, kid string) SignatureKeyMember {
	return SignatureKeyMember{Label: label, Scheme: SchemeJWKSURI, ID: id, Dwk: dwk, Kid: kid}
}

// SignatureKeyField is a parsed Signature-Key field, in field order.
type SignatureKeyField struct {
	Members []SignatureKeyMember
}

// Member returns the member for label.
func (f *SignatureKeyField) Member(label string) (SignatureKeyMember, bool) {
	for _, m := range f.Members {
		if m.Label == label {
			return m, true
		}
	}
	return SignatureKeyMember{}, false
}

// ParseSignatureKey parses a Signature-Key field. An error means the field is not
// a Dictionary. A member that is unusable reports it in its own Err.
func ParseSignatureKey(fieldValues []string) (*SignatureKeyField, error) {
	dict, err := httpsfv.UnmarshalDictionary(fieldValues)
	if err != nil {
		return nil, fmt.Errorf("parse Signature-Key: %w", err)
	}
	f := &SignatureKeyField{}
	for _, label := range dict.Names() {
		member, _ := dict.Get(label)
		f.Members = append(f.Members, parseKeyMember(label, member))
	}
	return f, nil
}

func parseKeyMember(label string, member httpsfv.Member) SignatureKeyMember {
	m := SignatureKeyMember{Label: label}
	item, ok := member.(httpsfv.Item)
	if !ok {
		m.Err = errors.New("member is an Inner List, not a scheme")
		return m
	}
	token, ok := item.Value.(httpsfv.Token)
	if !ok {
		m.Err = errors.New("member's value is not a scheme token")
		return m
	}
	m.Scheme = Scheme(token)
	readString := func(name string, dst *string) {
		if v, ok := item.Params.Get(name); ok {
			if s, ok := v.(string); ok {
				*dst = s
			} else if m.Err == nil {
				m.Err = fmt.Errorf("parameter %s is not a String", name)
			}
		}
	}
	switch m.Scheme {
	case SchemeJWT:
		readString("jwt", &m.JWT)
		if v, ok := item.Params.Get("cache"); ok && v == true {
			m.Cache = true
		}
	case SchemeJWKSURI:
		readString("id", &m.ID)
		readString("dwk", &m.Dwk)
		readString("kid", &m.Kid)
	}
	return m
}

func (m SignatureKeyMember) item() (httpsfv.Item, error) {
	params := httpsfv.NewParams()
	switch m.Scheme {
	case SchemeJWT:
		if m.JWT == "" {
			return httpsfv.Item{}, errors.New("jwt scheme needs a jwt")
		}
		params.Add("jwt", m.JWT)
		if m.Cache {
			params.Add("cache", true)
		}
	case SchemeJWKSURI:
		if m.ID == "" || m.Dwk == "" || m.Kid == "" {
			return httpsfv.Item{}, errors.New("jwks_uri scheme needs id, dwk and kid")
		}
		params.Add("id", m.ID)
		params.Add("dwk", m.Dwk)
		params.Add("kid", m.Kid)
	default:
		return httpsfv.Item{}, fmt.Errorf("cannot serialize scheme %q", m.Scheme)
	}
	return httpsfv.Item{Value: httpsfv.Token(m.Scheme), Params: params}, nil
}

// SerializeSignatureKey serializes members as a Signature-Key Dictionary. "the
// complete Signature-Key header (containing all keys) MUST be populated before any
// signature is created".
func SerializeSignatureKey(members ...SignatureKeyMember) (string, error) {
	dict := httpsfv.NewDictionary()
	for _, m := range members {
		if m.Label == "" {
			return "", errors.New("Signature-Key member has no label")
		}
		item, err := m.item()
		if err != nil {
			return "", err
		}
		dict.Add(m.Label, item)
	}
	return httpsfv.Marshal(dict)
}
