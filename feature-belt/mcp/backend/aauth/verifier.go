package aauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dunglas/httpsfv"
)

// SignatureErrorCode is the `error` member of a Signature-Error field.
type SignatureErrorCode string

// The Signature-Error codes (signature-key draft, Error Codes).
const (
	SigUnsupportedAlgorithm SignatureErrorCode = "unsupported_algorithm"
	SigUnsupportedScheme    SignatureErrorCode = "unsupported_scheme"
	SigCacheMiss            SignatureErrorCode = "cache_miss"
	SigInvalidSignature     SignatureErrorCode = "invalid_signature"
	SigInvalidInput         SignatureErrorCode = "invalid_input"
	SigInvalidRequest       SignatureErrorCode = "invalid_request"
	SigInvalidKey           SignatureErrorCode = "invalid_key"
	SigUnknownKey           SignatureErrorCode = "unknown_key"
	SigIssuerMissing        SignatureErrorCode = "issuer_missing"
	SigIssuerMismatch       SignatureErrorCode = "issuer_mismatch"
	SigInvalidJWT           SignatureErrorCode = "invalid_jwt"
	SigExpiredJWT           SignatureErrorCode = "expired_jwt"
	SigRevokedJWT           SignatureErrorCode = "revoked_jwt"
	SigClockSkew            SignatureErrorCode = "clock_skew"
)

// SignatureFailure is a failed verification. The protocol answers every signature
// failure with 401 and this code in Signature-Error; WriteTo does that. It
// implements error.
type SignatureFailure struct {
	Code SignatureErrorCode
	// RequiredInput lists the required components, for invalid_input.
	RequiredInput []string
	// AcceptSchemes is sent as Accept-Signature-Scheme, for unsupported_scheme.
	AcceptSchemes []Scheme
	// AcceptAlgs is sent as Accept-Signature-Alg, for unsupported_algorithm.
	AcceptAlgs []Alg
	// Detail is a short human-readable explanation, safe to send.
	Detail string
	// Cause is for the server's logs. It is never sent.
	Cause error
	// Internal marks a failure on our side, such as a revocation store outage.
	// It is written as 500 server_error, with no Signature-Error, because nothing
	// about the signature was judged.
	Internal bool
}

func (f *SignatureFailure) Error() string {
	if f.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", f.Code, f.Detail, f.Cause)
	}
	return string(f.Code) + ": " + f.Detail
}

// Unwrap returns the underlying cause.
func (f *SignatureFailure) Unwrap() error { return f.Cause }

// SignatureErrorField serializes the Signature-Error field value.
func (f *SignatureFailure) SignatureErrorField() string {
	dict := httpsfv.NewDictionary()
	dict.Add("error", httpsfv.NewItem(httpsfv.Token(f.Code)))
	if f.Code == SigInvalidInput && len(f.RequiredInput) > 0 {
		il := httpsfv.InnerList{Params: httpsfv.NewParams()}
		for _, c := range f.RequiredInput {
			il.Items = append(il.Items, httpsfv.NewItem(c))
		}
		dict.Add("required_input", il)
	}
	out, err := httpsfv.Marshal(dict)
	if err != nil {
		return "error=" + string(f.Code)
	}
	return out
}

// WriteTo writes the failure response: 401, the Signature-Error header and any
// Accept-Signature-* header, and a problem+json body. "The header, not the
// response body, is the machine-readable carrier". The body's `type` is the URN the
// signature-key draft requires of a Problem Details body under Signature-Error.
func (f *SignatureFailure) WriteTo(w http.ResponseWriter) {
	if f.Internal {
		WriteProblem(w, NewProblem(CodeServerError, ""))
		return
	}
	h := w.Header()
	h.Set(HeaderSignatureError, f.SignatureErrorField())
	if f.Code == SigUnsupportedScheme && len(f.AcceptSchemes) > 0 {
		h.Set(HeaderAcceptSignatureScheme, schemesField(f.AcceptSchemes))
	}
	if f.Code == SigUnsupportedAlgorithm && len(f.AcceptAlgs) > 0 {
		h.Set(HeaderAcceptSignatureAlg, algsField(f.AcceptAlgs))
	}
	h.Set("Content-Type", ContentTypeProblem)
	body := map[string]any{
		"type":   "urn:ietf:params:sig-error:" + string(f.Code),
		"status": http.StatusUnauthorized,
		"error":  f.Code,
	}
	if f.Detail != "" {
		body["detail"] = f.Detail
	}
	b, err := json.Marshal(body)
	if err != nil {
		b = []byte(`{"error":"` + string(f.Code) + `"}`)
	}
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(b)
}

func schemesField(schemes []Scheme) string {
	parts := make([]string, len(schemes))
	for i, s := range schemes {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}

func algsField(algs []Alg) string {
	parts := make([]string, len(algs))
	for i, a := range algs {
		parts[i] = string(a)
	}
	return strings.Join(parts, ", ")
}

// WriteVerificationError writes the response for an error from Verifier.Verify. A
// non-failure error is written as server_error, so a caller can pass any error.
func WriteVerificationError(w http.ResponseWriter, err error) {
	var f *SignatureFailure
	if errors.As(err, &f) {
		f.WriteTo(w)
		return
	}
	WriteProblem(w, NewProblem(CodeServerError, ""))
}

// SchemePolicy says which Signature-Key schemes an endpoint accepts and what may
// arrive under them. "Agents MUST use the jwt scheme"; servers "MUST use the
// jwks_uri scheme".
type SchemePolicy struct {
	// Schemes are the accepted schemes in preference order. They are what
	// Accept-Signature-Scheme names, and any other scheme is unsupported_scheme.
	Schemes []Scheme
	// TokenTypes are the JWT types accepted under the jwt scheme.
	TokenTypes []string
	// ServerDwks are the metadata documents accepted under the jwks_uri scheme.
	ServerDwks []string
}

// AgentPolicy accepts what a PS endpoint takes from an agent: the jwt scheme, with an
// agent token. "Agent token: presented to the PS and the AP always".
func AgentPolicy() SchemePolicy {
	return SchemePolicy{Schemes: []Scheme{SchemeJWT}, TokenTypes: []string{TypAgent}}
}

// ServerPolicy accepts servers signing in their own right, from the named metadata
// documents: the jwks_uri scheme.
func ServerPolicy(dwks ...string) SchemePolicy {
	return SchemePolicy{Schemes: []Scheme{SchemeJWKSURI}, ServerDwks: dwks}
}

// ResourcePolicy accepts what a resource takes: the jwt scheme, with the named
// token types. Used by the testbed resource.
func ResourcePolicy(tokenTypes ...string) SchemePolicy {
	return SchemePolicy{Schemes: []Scheme{SchemeJWT}, TokenTypes: tokenTypes}
}

// Merge returns a policy accepting what either accepts, for an endpoint served to
// both kinds of caller such as a pending URL.
func (p SchemePolicy) Merge(other SchemePolicy) SchemePolicy {
	out := SchemePolicy{}
	out.Schemes = append(append(out.Schemes, p.Schemes...), other.Schemes...)
	out.TokenTypes = append(append(out.TokenTypes, p.TokenTypes...), other.TokenTypes...)
	out.ServerDwks = append(append(out.ServerDwks, p.ServerDwks...), other.ServerDwks...)
	return out
}

func (p SchemePolicy) acceptsScheme(s Scheme) bool {
	for _, a := range p.Schemes {
		if a == s {
			return true
		}
	}
	return false
}

// ReplayKey is the tuple the protocol names for a replay cache: "(signing-key-
// thumbprint, created, @method, @authority, @path)".
type ReplayKey struct {
	Thumbprint string
	Created    int64
	Method     string
	Authority  string
	Path       string
}

// ReplayCache rejects duplicate tuples within the window. Optional: "a verifier MAY
// maintain a short-lived cache".
type ReplayCache interface {
	// SeenOrAdd records the key for ttl and reports whether it was already there.
	SeenOrAdd(ctx context.Context, key ReplayKey, ttl time.Duration) (bool, error)
}

// MemoryReplayCache is an in-process ReplayCache for a single instance. Note the
// tuple has one-second resolution, so a client making two identical signed calls in
// one second is refused on the second; use it only on endpoints where that is right.
type MemoryReplayCache struct {
	mu    sync.Mutex
	clock func() time.Time
	seen  map[ReplayKey]time.Time
}

// NewMemoryReplayCache returns an empty cache. A nil clock uses time.Now.
func NewMemoryReplayCache(clock func() time.Time) *MemoryReplayCache {
	if clock == nil {
		clock = time.Now
	}
	return &MemoryReplayCache{clock: clock, seen: map[ReplayKey]time.Time{}}
}

// SeenOrAdd implements ReplayCache.
func (c *MemoryReplayCache) SeenOrAdd(_ context.Context, key ReplayKey, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	for k, exp := range c.seen {
		if !now.Before(exp) {
			delete(c.seen, k)
		}
	}
	if _, ok := c.seen[key]; ok {
		return true, nil
	}
	c.seen[key] = now.Add(ttl)
	return false, nil
}

// VerifierConfig configures a request Verifier.
type VerifierConfig struct {
	// Keys resolves issuers' published keys. Required.
	Keys KeyResolver
	// Scheme says which schemes this endpoint accepts. Required.
	Scheme SchemePolicy
	// Origin is this server's public origin, such as its issuer. When set,
	// @authority and @scheme come from it rather than from the Host header, so a
	// signature made for another host fails even if the Host header is forged to
	// match. Set it in production; a request's own Host is used when empty.
	Origin string
	// Audience is this server's identifier, for the aud of person and auth tokens.
	// Required when Scheme accepts those types.
	Audience string
	// Identifiers is the identifier policy.
	Identifiers IdentifierPolicy
	// Clock is the verifier's time. Nil uses time.Now. Tests and static fixtures
	// pin it.
	Clock func() time.Time
	// SignatureWindow is how far `created` may be from now. Zero uses
	// DefaultSignatureWindow.
	SignatureWindow time.Duration
	// RequiredComponents names further components a request must cover, beyond the
	// base components, such as a resource's additional_signature_components.
	RequiredComponents func(*http.Request) []string
	// AllowUncoveredBody drops the content-digest and content-type requirement.
	// "On a request carrying a body to a PS or AS endpoint, or to any revocation
	// endpoint, the signature MUST additionally cover" them, so only a resource sets
	// this.
	AllowUncoveredBody bool
	// Revocations answers revoked_jwt.
	Revocations RevocationChecker
	// ReplayCache is the optional replay check.
	ReplayCache ReplayCache
	// MaxBodyBytes bounds the body read for digest checking. Zero uses 1 MiB.
	MaxBodyBytes int64
	// Algorithms are the accepted algorithms in preference order. They are exactly
	// what Accept-Signature-Alg names. Nil uses SupportedAlgs.
	Algorithms []Alg
	// DisableIatBound turns off the OPTIONAL bound on a token's iat.
	DisableIatBound bool
}

// Verifier implements the seven ordered steps of protocol section "Verification
// (Server)" for one endpoint's policy. It is safe for concurrent use.
type Verifier struct {
	cfg       VerifierConfig
	tokens    *TokenVerifier
	authority string
	scheme    string
}

// NewVerifier checks the configuration and returns a Verifier.
func NewVerifier(cfg VerifierConfig) (*Verifier, error) {
	if cfg.Keys == nil {
		return nil, errors.New("aauth: Verifier needs a KeyResolver")
	}
	if len(cfg.Scheme.Schemes) == 0 {
		return nil, errors.New("aauth: Verifier needs at least one accepted scheme")
	}
	for _, s := range cfg.Scheme.Schemes {
		if s != SchemeJWT && s != SchemeJWKSURI {
			return nil, fmt.Errorf("aauth: scheme %q is not implemented", s)
		}
	}
	if cfg.Scheme.acceptsScheme(SchemeJWT) && len(cfg.Scheme.TokenTypes) == 0 {
		return nil, errors.New("aauth: the jwt scheme needs accepted token types")
	}
	for _, t := range cfg.Scheme.TokenTypes {
		switch t {
		case TypAgent:
		case TypPerson, TypAuth:
			if cfg.Audience == "" {
				return nil, fmt.Errorf("aauth: accepting %s needs Audience", t)
			}
		default:
			return nil, fmt.Errorf("aauth: %s is not a token that signs requests", t)
		}
	}
	if cfg.Scheme.acceptsScheme(SchemeJWKSURI) && len(cfg.Scheme.ServerDwks) == 0 {
		return nil, errors.New("aauth: the jwks_uri scheme needs accepted metadata documents")
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.SignatureWindow == 0 {
		cfg.SignatureWindow = DefaultSignatureWindow
	}
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = 1 << 20
	}
	if cfg.Algorithms == nil {
		cfg.Algorithms = SupportedAlgs()
	}
	v := &Verifier{cfg: cfg}
	if cfg.Origin != "" {
		if err := cfg.Identifiers.ValidateServerIdentifier(cfg.Origin); err != nil {
			return nil, fmt.Errorf("aauth: Origin: %w", err)
		}
		scheme, host, _ := strings.Cut(cfg.Origin, "://")
		v.scheme = scheme
		v.authority = NormalizeAuthority(host, scheme)
	}
	iat := cfg.SignatureWindow
	if cfg.DisableIatBound {
		iat = -1
	}
	v.tokens = &TokenVerifier{
		Keys:        cfg.Keys,
		Clock:       cfg.Clock,
		Policy:      cfg.Identifiers,
		Revocations: cfg.Revocations,
		IatWindow:   iat,
	}
	return v, nil
}

// ServerIdentity is who signed under the jwks_uri scheme. ID is the identity the
// protocol takes from the signature: "The recipient resolves id to the caller's
// identity". The signature-key draft warns a verifier with no prior relationship
// holds "an origin-bound pseudonym rather than an authorized identity"; checking ID
// against who is allowed is the caller's job.
type ServerIdentity struct {
	ID  string
	Dwk string
	Kid string
}

// VerifiedRequest is the result of a successful verification.
type VerifiedRequest struct {
	Label  string
	Scheme Scheme
	// Key is the public key that verified the HTTP signature. Thumbprint is its
	// RFC 7638 thumbprint and Alg the algorithm from its alg member.
	Key        JWK
	Thumbprint string
	Alg        Alg
	// Created is the signature's created parameter, and Expires its optional
	// expires (zero when absent).
	Created int64
	Expires int64
	// Covered lists the serialized covered component identifiers, in order.
	Covered []string
	// Exactly one of the next four is set: the token presented under the jwt scheme
	// by its typ, or the server identity under jwks_uri.
	Agent  *AgentToken
	Person *PersonToken
	Auth   *AuthToken
	Server *ServerIdentity
	// Body is the request body, which has been read and put back on the request.
	Body []byte
}

func fail(code SignatureErrorCode, detail string, cause error) *SignatureFailure {
	return &SignatureFailure{Code: code, Detail: detail, Cause: cause}
}

// Verify runs the verification steps in order and returns a *VerifiedRequest, or an
// error that is always a *SignatureFailure. The body is read, bounded by
// MaxBodyBytes, and restored on r so a handler can read it, or take it from the
// result. Only after a nil error is anything in the request authenticated.
func (v *Verifier) Verify(ctx context.Context, r *http.Request) (*VerifiedRequest, error) {
	res, f := v.verify(ctx, r)
	if f != nil {
		return nil, f
	}
	return res, nil
}

func (v *Verifier) verify(ctx context.Context, r *http.Request) (*VerifiedRequest, *SignatureFailure) {
	body, f := v.readBody(r)
	if f != nil {
		return nil, f
	}

	// Step 1: "Extract the Signature, Signature-Input, and Signature-Key headers.
	// If any are missing, return invalid_signature."
	sigValues := r.Header.Values(HeaderSignature)
	inputValues := r.Header.Values(HeaderSignatureInput)
	keyValues := r.Header.Values(HeaderSignatureKey)
	if len(sigValues) == 0 || len(inputValues) == 0 || len(keyValues) == 0 {
		return nil, fail(SigInvalidSignature, "request is missing a signature header", nil)
	}
	inputs, err := ParseSignatureInput(inputValues)
	if err != nil {
		return nil, fail(SigInvalidSignature, "Signature-Input is malformed", err)
	}
	signatures, err := ParseSignatures(sigValues)
	if err != nil {
		return nil, fail(SigInvalidSignature, "Signature is malformed", err)
	}
	// A malformed Signature-Key is reported at step 5, as a key that can't be parsed,
	// so steps 2 to 4 still apply to the request first.
	keyField, keyFieldErr := ParseSignatureKey(keyValues)

	input, f := v.selectLabel(inputs, signatures, keyField)
	if f != nil {
		return nil, f
	}
	signature, ok := signatures[input.Label]
	if !ok {
		return nil, fail(SigInvalidSignature, "Signature has no member for the label", nil)
	}
	if input.Err != nil {
		return nil, fail(SigInvalidSignature, "Signature-Input is malformed", input.Err)
	}
	params := input.Params

	// Step 2: "Verify that the Signature-Input covers the required components and any
	// additional components the server requires."
	required := v.requiredComponents(r, body)
	covered := make([]string, len(params.Components))
	coveredSet := make(map[string]bool, len(params.Components))
	for i, c := range params.Components {
		covered[i] = c.String()
		coveredSet[covered[i]] = true
	}
	for _, name := range required {
		if !coveredSet[Comp(name).String()] {
			return nil, &SignatureFailure{Code: SigInvalidInput, RequiredInput: required, Detail: "signature does not cover a required component"}
		}
	}

	// Step 3: created is present and within the window. "Return invalid_signature if
	// created is older than the window, and clock_skew if it is further ahead of the
	// server's clock than the window." expires: "servers MUST honor it when present
	// and MUST reject requests where expires is in the past."
	now := v.cfg.Clock()
	if f := checkFreshness(params, now, v.cfg.SignatureWindow); f != nil {
		return nil, f
	}

	// Step 4: select the member for the label and read its scheme. A scheme this
	// endpoint does not accept takes the defined path whether it is a real scheme or
	// any unregistered value.
	if keyFieldErr != nil {
		return nil, fail(SigInvalidKey, "Signature-Key cannot be parsed", keyFieldErr)
	}
	member, ok := keyField.Member(input.Label)
	if !ok {
		// "the corresponding dictionary member is missing, verification for that
		// signature MUST fail"
		return nil, fail(SigInvalidSignature, "Signature-Key has no member for the label", nil)
	}
	if member.Err != nil && member.Scheme == "" {
		return nil, fail(SigInvalidKey, "Signature-Key member cannot be parsed", member.Err)
	}
	if !v.cfg.Scheme.acceptsScheme(member.Scheme) {
		return nil, &SignatureFailure{
			Code:          SigUnsupportedScheme,
			AcceptSchemes: append([]Scheme(nil), v.cfg.Scheme.Schemes...),
			Detail:        "this endpoint does not accept that Signature-Key scheme",
		}
	}
	if member.Err != nil {
		return nil, fail(SigInvalidKey, "Signature-Key member cannot be parsed", member.Err)
	}

	res := &VerifiedRequest{
		Label:   input.Label,
		Scheme:  member.Scheme,
		Created: params.Created,
		Expires: params.Expires,
		Covered: covered,
		Body:    body,
	}

	// Step 5: obtain the key according to the scheme.
	var key JWK
	switch member.Scheme {
	case SchemeJWT:
		key, f = v.keyFromToken(ctx, member, res)
		if f == nil && !keyIDNames(params.KeyID, key) {
			// "If keyid is present for a label that also appears in Signature-Key, the two MUST identify the same key, and the verifier MUST take the key from Signature-Key."
			return nil, fail(SigInvalidSignature, "keyid and the token's cnf.jwk identify different keys", nil)
		}
	case SchemeJWKSURI:
		key, f = v.keyFromServer(ctx, member, params, res)
	}
	if f != nil {
		return nil, f
	}

	// Step 6: determine the algorithm from the key's alg.
	alg, f := v.determineAlgorithm(key)
	if f != nil {
		return nil, f
	}
	res.Key, res.Alg = key, alg
	if res.Thumbprint, err = key.Thumbprint(); err != nil {
		return nil, fail(SigInvalidKey, "key has no thumbprint", err)
	}

	// Step 7: verify the HTTP Message Signature.
	msg := NewRequestMessage(r)
	if v.authority != "" {
		msg.Authority = v.authority
		msg.Scheme = v.scheme
	}
	base, err := SignatureBase(&msg, nil, params)
	if err != nil {
		return nil, fail(SigInvalidSignature, "signature base cannot be created", err)
	}
	if err := key.Verify([]byte(base), signature); err != nil {
		key2, refreshed := v.refreshServerKey(ctx, member)
		if !refreshed || key2.Verify([]byte(base), signature) != nil {
			return nil, fail(SigInvalidSignature, "signature does not verify", err)
		}
		res.Key = key2
		if res.Thumbprint, err = key2.Thumbprint(); err != nil {
			return nil, fail(SigInvalidKey, "key has no thumbprint", err)
		}
	}

	// A signature over content-digest proves nothing about the body until the digest
	// is checked against it.
	if digest := r.Header.Values(HeaderContentDigest); len(digest) > 0 {
		if err := VerifyContentDigest(digest, body); err != nil {
			return nil, fail(SigInvalidSignature, "Content-Digest does not match the body", err)
		}
	}

	if v.cfg.ReplayCache != nil {
		// A verifier "rejecting duplicate tuples": a replay within the window is a
		// stale signature, which the draft answers with invalid_signature.
		seen, err := v.cfg.ReplayCache.SeenOrAdd(ctx, ReplayKey{
			Thumbprint: res.Thumbprint,
			Created:    params.Created,
			Method:     msg.Method,
			Authority:  msg.Authority,
			Path:       msg.Path,
		}, 2*v.cfg.SignatureWindow)
		if err != nil {
			return nil, &SignatureFailure{Code: SigInvalidSignature, Internal: true, Detail: "replay cache failed", Cause: err}
		}
		if seen {
			return nil, fail(SigInvalidSignature, "signature was already used", nil)
		}
	}
	return res, nil
}

func (v *Verifier) readBody(r *http.Request) ([]byte, *SignatureFailure) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, v.cfg.MaxBodyBytes+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, fail(SigInvalidRequest, "request body could not be read", err)
	}
	if int64(len(body)) > v.cfg.MaxBodyBytes {
		return nil, fail(SigInvalidRequest, "request body is too large", nil)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// selectLabel decides which signature to verify. "The verifier determines which
// labels it is attempting to verify", and members it did not select are ignored, so
// a request offering one signature under a scheme this endpoint doesn't accept and
// another under one it does verifies the second. With no acceptable member, the
// first label is verified, which yields the defined failure for that member.
func (v *Verifier) selectLabel(inputs []SignatureInputMember, signatures map[string][]byte, keys *SignatureKeyField) (SignatureInputMember, *SignatureFailure) {
	if len(inputs) == 0 {
		return SignatureInputMember{}, fail(SigInvalidSignature, "Signature-Input has no members", nil)
	}
	if keys != nil {
		for _, in := range inputs {
			if _, hasSig := signatures[in.Label]; !hasSig || in.Err != nil {
				continue
			}
			if m, ok := keys.Member(in.Label); ok && m.Err == nil && v.cfg.Scheme.acceptsScheme(m.Scheme) {
				return in, nil
			}
		}
	}
	return inputs[0], nil
}

// requiredComponents lists what the signature must cover, in the order sent back in
// required_input.
func (v *Verifier) requiredComponents(r *http.Request, body []byte) []string {
	required := []string{"@method", "@authority", "@path", "signature-key"}
	if len(body) > 0 && !v.cfg.AllowUncoveredBody {
		required = append(required, "content-digest", "content-type")
	}
	if v.cfg.RequiredComponents != nil {
		for _, extra := range v.cfg.RequiredComponents(r) {
			dup := false
			for _, have := range required {
				dup = dup || have == extra
			}
			if !dup {
				required = append(required, extra)
			}
		}
	}
	return required
}

func checkFreshness(params SignatureParams, now time.Time, window time.Duration) *SignatureFailure {
	if !params.HasCreated {
		return fail(SigInvalidSignature, "signature has no created parameter", nil)
	}
	// Compared in seconds: created is attacker-chosen, and a large value converted to a
	// Duration would overflow into the window.
	age, limit := now.Unix()-params.Created, int64(window/time.Second)
	if age > limit {
		return fail(SigInvalidSignature, "signature is older than the validity window", nil)
	}
	if -age > limit {
		return fail(SigClockSkew, "signature created is ahead of this server's clock", nil)
	}
	if params.HasExpires && params.Expires <= now.Unix() {
		return fail(SigInvalidSignature, "signature has expired", nil)
	}
	return nil
}

// keyFromToken handles the jwt scheme: it verifies the token by its typ and returns
// its cnf.jwk, the key "the same key that signed the request".
func (v *Verifier) keyFromToken(ctx context.Context, member SignatureKeyMember, res *VerifiedRequest) (JWK, *SignatureFailure) {
	if member.JWT == "" {
		return JWK{}, fail(SigInvalidKey, "jwt scheme has no jwt parameter", nil)
	}
	typ := peekTyp(member.JWT)
	accepted := false
	for _, t := range v.cfg.Scheme.TokenTypes {
		accepted = accepted || t == typ
	}
	if !accepted {
		return JWK{}, fail(SigInvalidJWT, "this endpoint does not accept that token type", nil)
	}
	switch typ {
	case TypAgent:
		t, te := v.tokens.verifyAgent(ctx, member.JWT, AgentTokenOptions{}, nil)
		if te != nil {
			return JWK{}, v.tokenFailure(te)
		}
		res.Agent = t
		return t.Key, nil
	case TypPerson:
		t, te := v.tokens.verifyPerson(ctx, member.JWT, v.cfg.Audience, keyBinding{})
		if te != nil {
			return JWK{}, v.tokenFailure(te)
		}
		res.Person = t
		return t.Key, nil
	case TypAuth:
		t, te := v.tokens.verifyAuth(ctx, member.JWT, v.cfg.Audience, keyBinding{})
		if te != nil {
			return JWK{}, v.tokenFailure(te)
		}
		res.Auth = t
		return t.Key, nil
	}
	return JWK{}, fail(SigInvalidJWT, "this endpoint does not accept that token type", nil)
}

// keyIDNames says a keyid doesn't contradict the key a token confirms. A cnf.jwk needn't carry a kid, and a keyid that names some key by a local name can't then be told from one naming this key, so that passes: the signature is
// checked against the key from Signature-Key either way, as the spec asks, and a key's thumbprint names it whatever its kid says.
func keyIDNames(keyID string, key JWK) bool {
	if keyID == "" || key.Kid == "" || keyID == key.Kid {
		return true
	}
	thumbprint, err := key.Thumbprint()
	return err == nil && keyID == thumbprint
}

// tokenFailure turns a token error into the verifier's failure, adding the
// Accept-Signature-Alg list when it is an algorithm problem.
func (v *Verifier) tokenFailure(te *TokenError) *SignatureFailure {
	f := &SignatureFailure{Code: te.Sig, Detail: te.Detail, Cause: te.Cause, Internal: te.Internal}
	if te.Sig == SigUnsupportedAlgorithm {
		f.AcceptAlgs = append([]Alg(nil), v.cfg.Algorithms...)
	}
	return f
}

// keyFromServer handles the jwks_uri scheme.
func (v *Verifier) keyFromServer(ctx context.Context, member SignatureKeyMember, params SignatureParams, res *VerifiedRequest) (JWK, *SignatureFailure) {
	if member.ID == "" || member.Dwk == "" || member.Kid == "" {
		return JWK{}, fail(SigInvalidKey, "jwks_uri scheme needs id, dwk and kid", nil)
	}
	if err := v.cfg.Identifiers.ValidateServerIdentifier(member.ID); err != nil {
		return JWK{}, fail(SigInvalidKey, "id is not a server identifier", err)
	}
	dwkOK := false
	for _, d := range v.cfg.Scheme.ServerDwks {
		dwkOK = dwkOK || d == member.Dwk
	}
	if !dwkOK {
		return JWK{}, fail(SigInvalidKey, "this endpoint does not accept that metadata document", nil)
	}
	// "If keyid is present for a label that also appears in Signature-Key, the two
	// MUST identify the same key, and the verifier MUST take the key from
	// Signature-Key."
	if params.KeyID != "" && params.KeyID != member.Kid {
		return JWK{}, fail(SigInvalidSignature, "keyid and kid identify different keys", nil)
	}
	key, err := v.cfg.Keys.Key(ctx, member.ID, member.Dwk, member.Kid)
	if err != nil {
		var de *DiscoveryError
		if errors.As(err, &de) {
			return JWK{}, fail(de.Code, "signer keys could not be resolved", err)
		}
		return JWK{}, fail(SigUnknownKey, "signer keys could not be resolved", err)
	}
	res.Server = &ServerIdentity{ID: member.ID, Dwk: member.Dwk, Kid: member.Kid}
	return key, nil
}

// determineAlgorithm is step 6: "Return unsupported_algorithm if alg is absent, is a
// polymorphic identifier, or names an algorithm or key type the server does not
// implement, and include an Accept-Signature-Alg header... Return invalid_key if the
// key's kty or crv disagrees with its alg."
func (v *Verifier) determineAlgorithm(key JWK) (Alg, *SignatureFailure) {
	unsupported := func(detail string, cause error) *SignatureFailure {
		return &SignatureFailure{
			Code:       SigUnsupportedAlgorithm,
			AcceptAlgs: append([]Alg(nil), v.cfg.Algorithms...),
			Detail:     detail,
			Cause:      cause,
		}
	}
	alg, err := key.Algorithm()
	if err != nil {
		var ke *KeyError
		if errors.As(err, &ke) && ke.Code == SigUnsupportedAlgorithm {
			return "", unsupported("key has no algorithm this server accepts", err)
		}
		return "", fail(SigInvalidKey, "key's kty or crv disagrees with its alg", err)
	}
	accepted := false
	for _, a := range v.cfg.Algorithms {
		accepted = accepted || a == alg
	}
	if !accepted {
		return "", unsupported("key has no algorithm this server accepts", nil)
	}
	if err := key.Validate(); err != nil {
		return "", fail(SigInvalidKey, "key cannot be parsed", err)
	}
	return alg, nil
}

// refreshServerKey applies the refresh on a failed signature to the jwks_uri scheme:
// "SHOULD refresh the issuer's JWKS once and retry", within the floor. Under the jwt
// scheme the failed key is cnf.jwk, which came from the token, so there is nothing
// to refresh.
func (v *Verifier) refreshServerKey(ctx context.Context, member SignatureKeyMember) (JWK, bool) {
	if member.Scheme != SchemeJWKSURI {
		return JWK{}, false
	}
	key, fetched, err := v.cfg.Keys.RefreshKey(ctx, member.ID, member.Dwk, member.Kid)
	if err != nil || !fetched {
		return JWK{}, false
	}
	return key, true
}
