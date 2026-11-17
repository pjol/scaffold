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
	"time"
)

// RequestSigner signs outbound requests under one Signature-Key member. Use
// NewServerSigner for a server signing in its own right and NewAgentSigner for an
// agent presenting a token.
type RequestSigner struct {
	key    Signer
	member SignatureKeyMember
	clock  func() time.Time
	label  string
}

// NewServerSigner returns a signer using the jwks_uri scheme. "The id parameter MUST
// be the server's issuer as published in its metadata, and dwk MUST be that metadata
// document's well-known name". The kid comes from the key.
func NewServerSigner(issuer, dwk string, key Signer, clock func() time.Time) *RequestSigner {
	if clock == nil {
		clock = time.Now
	}
	return &RequestSigner{
		key:    key,
		member: JWKSURIKeyMember(DefaultSignatureLabel, issuer, dwk, key.PublicJWK().Kid),
		clock:  clock,
		label:  DefaultSignatureLabel,
	}
}

// NewAgentSigner returns a signer using the jwt scheme, presenting token. key is the
// key token's cnf.jwk confirms. Sayso is not an agent, so this serves the testbed and
// the conformance suite.
func NewAgentSigner(token string, key Signer, clock func() time.Time) *RequestSigner {
	if clock == nil {
		clock = time.Now
	}
	return &RequestSigner{
		key:    key,
		member: JWTKeyMember(DefaultSignatureLabel, token),
		clock:  clock,
		label:  DefaultSignatureLabel,
	}
}

// SignRequest signs r, covering @method, @authority, @path and signature-key, and on
// a body also content-type and content-digest, in the order of the spec's examples.
// A non-nil body becomes r's body and its Content-Digest is set; a Content-Type must
// already be set, since it is covered. Extra adds covered fields present on r, such as
// authorization. It must be the last change to the headers it covers.
func (s *RequestSigner) SignRequest(r *http.Request, body []byte, extra ...string) error {
	components := []Component{Comp("@method"), Comp("@authority"), Comp("@path")}
	if len(body) > 0 {
		if r.Header.Get("Content-Type") == "" {
			return errors.New("aauth: a signed request with a body needs a Content-Type")
		}
		r.Header.Set(HeaderContentDigest, ContentDigest(body))
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		components = append(components, Comp("content-type"), Comp(HeaderContentDigest))
	}
	for _, name := range extra {
		components = append(components, Comp(name))
	}
	components = append(components, Comp(HeaderSignatureKey))

	skField, err := SerializeSignatureKey(s.member)
	if err != nil {
		return err
	}
	r.Header.Set(HeaderSignatureKey, skField)

	params := NewSignatureParams(components, s.clock())
	msg := NewRequestMessage(r)
	base, err := SignatureBase(&msg, nil, params)
	if err != nil {
		return fmt.Errorf("aauth: signature base: %w", err)
	}
	return s.attach(r.Header, params, base)
}

func (s *RequestSigner) attach(h http.Header, params SignatureParams, base string) error {
	sig, err := s.key.Sign([]byte(base))
	if err != nil {
		return fmt.Errorf("aauth: sign: %w", err)
	}
	input, err := SignatureInputField(s.label, params)
	if err != nil {
		return err
	}
	field, err := SignatureField(s.label, sig)
	if err != nil {
		return err
	}
	h.Set(HeaderSignatureInput, input)
	h.Set(HeaderSignature, field)
	return nil
}

// SignedResponse is a signed response ready to send. It is plain data, so an SS can
// store it and answer a repeated request with "the same signed response" (supervision,
// Idempotence).
type SignedResponse struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

// WriteTo sends the response exactly as signed.
func (r *SignedResponse) WriteTo(w http.ResponseWriter) {
	h := w.Header()
	for k, v := range r.Header {
		h[k] = append([]string(nil), v...)
	}
	w.WriteHeader(r.Status)
	if len(r.Body) > 0 {
		_, _ = w.Write(r.Body)
	}
}

// DecisionComponents are the components a signed decision covers, as in the
// supervision draft's example. "The signature MUST cover @status, content-type, and
// content-digest, and SHOULD cover the request's content-digest, @authority, and @path
// as ;req components". A request with no Content-Digest, such as a poll, has no
// content-digest to bind.
func DecisionComponents(hasRequestDigest bool) []Component {
	out := []Component{Comp("@status"), Comp("content-type"), Comp(HeaderContentDigest)}
	if hasRequestDigest {
		out = append(out, ReqComp(HeaderContentDigest))
	}
	return append(out, ReqComp("@authority"), ReqComp("@path"))
}

// AcceptSignatureForDecision is the Accept-Signature field a PS sends on a supervision
// request (supervision, Signed Decisions).
func AcceptSignatureForDecision(hasRequestDigest bool) string {
	s, err := BuildAcceptSignature(DefaultSignatureLabel, DecisionComponents(hasRequestDigest))
	if err != nil {
		return ""
	}
	return s
}

// SignResponse signs a response to req, covering components, and returns it with the
// Content-Digest, Signature-Key, Signature-Input and Signature headers set. header must
// carry the Content-Type when there is a body. With components nil it covers what the
// request's Accept-Signature names when that is applicable, otherwise
// DecisionComponents. The identifying Signature-Key member is added under the signer's
// jwks_uri scheme, and keyid repeats its kid, as the supervision draft's example does.
//
// signature-key isn't covered unless asked for. RFC 9421 section 5.2 says a signature
// fulfilling Accept-Signature "MUST include the same set of covered components", and
// the draft's example request doesn't name it. The verifier checks the id it carries
// against the issuer it called, so substituting it gains nothing.
func (s *RequestSigner) SignResponse(req *RequestMessage, status int, header http.Header, body []byte, components ...Component) (*SignedResponse, error) {
	if s.member.Scheme != SchemeJWKSURI {
		return nil, errors.New("aauth: a signed response identifies a server, under jwks_uri")
	}
	h := http.Header{}
	for k, v := range header {
		h[k] = append([]string(nil), v...)
	}
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", ContentTypeJSON)
	}
	h.Set(HeaderContentDigest, ContentDigest(body))
	skField, err := SerializeSignatureKey(s.member)
	if err != nil {
		return nil, err
	}
	h.Set(HeaderSignatureKey, skField)

	resp := &ResponseMessage{Status: status, Header: h}
	if len(components) == 0 {
		components = responseComponents(req, resp)
	}
	params := NewSignatureParams(components, s.clock())
	params.KeyID = s.member.Kid
	base, err := SignatureBase(req, resp, params)
	if err != nil {
		return nil, fmt.Errorf("aauth: signature base: %w", err)
	}
	if err := s.attach(h, params, base); err != nil {
		return nil, err
	}
	return &SignedResponse{Status: status, Header: h, Body: body}, nil
}

// responseComponents reads Accept-Signature, keeping the components it requests if
// they can be applied to this message and include the set a decision must cover.
func responseComponents(req *RequestMessage, resp *ResponseMessage) []Component {
	fallback := func() []Component {
		return DecisionComponents(req != nil && req.Header.Get(HeaderContentDigest) != "")
	}
	if req == nil {
		return []Component{Comp("@status"), Comp("content-type"), Comp(HeaderContentDigest)}
	}
	values := req.Header.Values(HeaderAcceptSignature)
	if len(values) == 0 {
		return fallback()
	}
	requested, err := ParseAcceptSignature(values)
	if err != nil {
		return fallback()
	}
	for _, r := range requested {
		if r.Label != DefaultSignatureLabel || len(r.Components) == 0 {
			continue
		}
		if !coversAll(r.Components, []Component{Comp("@status"), Comp("content-type"), Comp(HeaderContentDigest)}) {
			continue
		}
		probe := SignatureParams{Components: r.Components}
		if _, err := SignatureBase(req, resp, probe); err != nil {
			continue
		}
		return r.Components
	}
	return fallback()
}

func coversAll(have, want []Component) bool {
	set := map[string]bool{}
	for _, c := range have {
		set[c.String()] = true
	}
	for _, c := range want {
		if !set[c.String()] {
			return false
		}
	}
	return true
}

// ResponseVerifyOptions configures VerifySignedResponse.
type ResponseVerifyOptions struct {
	// Keys resolves the signer's keys. Required.
	Keys KeyResolver
	// Issuer is the server called: "MUST verify that the id in the response's
	// Signature-Key is the issuer of the SS it called". Required.
	Issuer string
	// Dwk is the metadata document the signer must name. Required: DwkSupervisor for a
	// decision, DwkResource for a usage response.
	Dwk string
	// Required lists further components that must be covered. @status, content-type and
	// content-digest always are.
	Required []Component
	// Clock and MaxAge bound created when MaxAge is positive. Leave MaxAge zero for a
	// decision: an SS answers a repeated request with "the same signed response", so
	// an honest created can be old, and a log entry is verified long after the fact.
	Clock  func() time.Time
	MaxAge time.Duration
}

// VerifiedResponse is the result of VerifySignedResponse.
type VerifiedResponse struct {
	Label      string
	ID         string
	Dwk        string
	Kid        string
	Key        JWK
	Thumbprint string
	Alg        Alg
	Created    int64
	Covered    []string
	// SignatureBase is what the signature covers, for the log entry.
	SignatureBase string
}

// VerifySignedResponse checks a signed response (supervision, Signed Decisions; RFC
// 9421 section 2.4). req is the request as signed, which `;req` components read. A
// failure is a *SignatureFailure, and the caller treats it as server_error: "A signed
// response that fails either check is treated as server_error". Use
// HasResponseSignature to tell an unsigned response, which is treated the same way when
// the server publishes a jwks_uri.
func VerifySignedResponse(ctx context.Context, req *RequestMessage, resp *ResponseMessage, body []byte, opts ResponseVerifyOptions) (*VerifiedResponse, error) {
	if opts.Keys == nil || opts.Issuer == "" || opts.Dwk == "" {
		return nil, errors.New("aauth: VerifySignedResponse needs Keys, Issuer and Dwk")
	}
	h := resp.Header
	inputs, err := ParseSignatureInput(h.Values(HeaderSignatureInput))
	if err != nil {
		return nil, fail(SigInvalidSignature, "Signature-Input is malformed", err)
	}
	signatures, err := ParseSignatures(h.Values(HeaderSignature))
	if err != nil {
		return nil, fail(SigInvalidSignature, "Signature is malformed", err)
	}
	keys, err := ParseSignatureKey(h.Values(HeaderSignatureKey))
	if err != nil {
		return nil, fail(SigInvalidKey, "Signature-Key cannot be parsed", err)
	}
	// The label is the member naming the server called. Others are ignored.
	var input SignatureInputMember
	var member SignatureKeyMember
	found := false
	for _, in := range inputs {
		m, ok := keys.Member(in.Label)
		_, signed := signatures[in.Label]
		if ok && signed && in.Err == nil && m.Scheme == SchemeJWKSURI && m.ID == opts.Issuer {
			input, member, found = in, m, true
			break
		}
	}
	if !found {
		return nil, fail(SigInvalidSignature, "response has no signature from the server called", nil)
	}
	if member.Dwk != opts.Dwk {
		return nil, fail(SigInvalidKey, "response names the wrong metadata document", nil)
	}
	if member.Kid == "" {
		return nil, fail(SigInvalidKey, "response Signature-Key has no kid", nil)
	}
	params := input.Params
	if params.KeyID != "" && params.KeyID != member.Kid {
		return nil, fail(SigInvalidSignature, "keyid and kid identify different keys", nil)
	}
	if !params.HasCreated {
		return nil, fail(SigInvalidSignature, "signature has no created parameter", nil)
	}
	if opts.MaxAge > 0 {
		clock := opts.Clock
		if clock == nil {
			clock = time.Now
		}
		if clock().Unix()-params.Created > int64(opts.MaxAge/time.Second) {
			return nil, fail(SigInvalidSignature, "signature is older than allowed", nil)
		}
	}
	required := append([]Component{Comp("@status"), Comp("content-type"), Comp(HeaderContentDigest)}, opts.Required...)
	if !coversAll(params.Components, required) {
		names := make([]string, len(required))
		for i, c := range required {
			names[i] = strings.Trim(c.String(), `"`)
		}
		return nil, &SignatureFailure{Code: SigInvalidInput, RequiredInput: names, Detail: "response signature does not cover a required component"}
	}
	key, err := opts.Keys.Key(ctx, member.ID, member.Dwk, member.Kid)
	if err != nil {
		var de *DiscoveryError
		if errors.As(err, &de) {
			return nil, fail(de.Code, "signer keys could not be resolved", err)
		}
		return nil, fail(SigUnknownKey, "signer keys could not be resolved", err)
	}
	alg, err := key.Algorithm()
	if err != nil {
		var ke *KeyError
		errors.As(err, &ke)
		return nil, fail(ke.Code, "signer key has no usable algorithm", err)
	}
	if err := key.Validate(); err != nil {
		return nil, fail(SigInvalidKey, "signer key cannot be parsed", err)
	}
	base, err := SignatureBase(req, resp, params)
	if err != nil {
		return nil, fail(SigInvalidSignature, "signature base cannot be created", err)
	}
	if err := key.Verify([]byte(base), signatures[input.Label]); err != nil {
		fresh, fetched, rerr := opts.Keys.RefreshKey(ctx, member.ID, member.Dwk, member.Kid)
		if rerr != nil || !fetched || fresh.Verify([]byte(base), signatures[input.Label]) != nil {
			return nil, fail(SigInvalidSignature, "response signature does not verify", err)
		}
		key = fresh
	}
	if err := VerifyContentDigest(h.Values(HeaderContentDigest), body); err != nil {
		return nil, fail(SigInvalidSignature, "Content-Digest does not match the body", err)
	}
	tp, err := key.Thumbprint()
	if err != nil {
		return nil, fail(SigInvalidKey, "signer key has no thumbprint", err)
	}
	covered := make([]string, len(params.Components))
	for i, c := range params.Components {
		covered[i] = c.String()
	}
	return &VerifiedResponse{
		Label: input.Label, ID: member.ID, Dwk: member.Dwk, Kid: member.Kid,
		Key: key, Thumbprint: tp, Alg: alg, Created: params.Created,
		Covered: covered, SignatureBase: base,
	}, nil
}

// HasResponseSignature reports whether a response carries a signature at all.
func HasResponseSignature(h http.Header) bool {
	return h.Get(HeaderSignature) != "" && h.Get(HeaderSignatureInput) != "" && h.Get(HeaderSignatureKey) != ""
}

// LocalKeys answers for one issuer's own keys and sends everything else to Next, so a
// PS verifying a person token it issued doesn't fetch its own JWKS over the network.
type LocalKeys struct {
	Issuer string
	// Dwk, when set, limits this to one metadata document. "Roles hosted under a shared
	// origin share one identifier and are distinguished by the well-known document": a
	// PS co-hosted with an SS answers for aauth-person.json and fetches the SS's keys.
	Dwk string
	// Keys returns the issuer's published keys, including retiring ones still valid.
	Keys func(ctx context.Context) ([]JWK, error)
	Next KeyResolver
}

func (l *LocalKeys) answers(iss, dwk string) bool {
	return iss == l.Issuer && (l.Dwk == "" || dwk == l.Dwk)
}

// Key implements KeyResolver.
func (l *LocalKeys) Key(ctx context.Context, iss, dwk, kid string) (JWK, error) {
	if !l.answers(iss, dwk) {
		if l.Next == nil {
			return JWK{}, &DiscoveryError{Code: SigUnknownKey, Issuer: iss}
		}
		return l.Next.Key(ctx, iss, dwk, kid)
	}
	keys, err := l.Keys(ctx)
	if err != nil {
		return JWK{}, &DiscoveryError{Code: SigUnknownKey, Issuer: iss, Cause: err}
	}
	for _, k := range keys {
		if k.Kid == kid {
			return k, nil
		}
	}
	return JWK{}, &DiscoveryError{Code: SigUnknownKey, Issuer: iss}
}

// RefreshKey implements KeyResolver. Local keys are always current, so there is never
// anything further to fetch.
func (l *LocalKeys) RefreshKey(ctx context.Context, iss, dwk, kid string) (JWK, bool, error) {
	if !l.answers(iss, dwk) {
		if l.Next == nil {
			return JWK{}, false, &DiscoveryError{Code: SigUnknownKey, Issuer: iss}
		}
		return l.Next.RefreshKey(ctx, iss, dwk, kid)
	}
	return JWK{}, false, nil
}

// MarshalJWKS serializes keys for /.well-known/jwks.json, refusing a key without a
// fully-specified alg: "Every key an AAuth server publishes at its jwks_uri MUST carry
// a fully-specified alg member".
func MarshalJWKS(keys ...JWK) ([]byte, error) {
	set := JWKS{Keys: []JWK{}}
	for _, k := range keys {
		if err := k.Validate(); err != nil {
			return nil, fmt.Errorf("aauth: key %q: %w", k.Kid, err)
		}
		if k.Kid == "" {
			return nil, errors.New("aauth: a published key needs a kid")
		}
		set.Keys = append(set.Keys, k)
	}
	return json.Marshal(set)
}
