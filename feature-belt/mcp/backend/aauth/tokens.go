package aauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultSignatureWindow is the signature validity window: "The default window
// is 60 seconds" (protocol, Verification).
const DefaultSignatureWindow = 60 * time.Second

// KeyResolver finds an issuer's published signing keys. *Discovery implements
// it over egress-admitted HTTP; LocalKeys answers for this server's own keys.
type KeyResolver interface {
	// Key returns the JWK with kid from {iss}/.well-known/{dwk}. It may serve a
	// cached copy and refresh for an unknown kid, within the fetch floor. A
	// failure is a *DiscoveryError.
	Key(ctx context.Context, iss, dwk, kid string) (JWK, error)
	// RefreshKey re-reads the issuer's keys after a signature failed against a
	// cached key. fetched reports whether a fetch actually happened; when the
	// once-per-minute floor forbids one it is false and the caller keeps its
	// failure. A key gone from the refreshed set is a *DiscoveryError with
	// unknown_key.
	RefreshKey(ctx context.Context, iss, dwk, kid string) (key JWK, fetched bool, err error)
}

// RevocationChecker answers whether a token has been revoked. "Recipients
// maintaining revocation state MUST key it by (iss, jti)."
type RevocationChecker interface {
	IsRevoked(ctx context.Context, iss, jti string) (bool, error)
}

// RevocationCheckerFunc adapts a function to RevocationChecker.
type RevocationCheckerFunc func(ctx context.Context, iss, jti string) (bool, error)

// IsRevoked implements RevocationChecker.
func (f RevocationCheckerFunc) IsRevoked(ctx context.Context, iss, jti string) (bool, error) {
	return f(ctx, iss, jti)
}

// TokenParam names a token carried as a request parameter, which fails with
// codes of the form <invalid|expired|revoked>_<parameter>_token.
type TokenParam string

// The token parameters of the token endpoints.
const (
	ParamResource  TokenParam = "resource"
	ParamPresented TokenParam = "presented"
	ParamUpstream  TokenParam = "upstream"
	ParamSubagent  TokenParam = "subagent"
)

// TokenError is a token verification failure.
//
// Sig is the Signature-Error code if this token was in the Signature-Key header.
// Code is the problem+json code if it was a request parameter, set by the
// functions that verify parameters. Internal marks a failure of ours, such as a
// revocation store outage, which answers 500 rather than blaming the token.
type TokenError struct {
	Sig      SignatureErrorCode
	Code     ErrorCode
	Internal bool
	// Tampered marks a mismatch between a resource token and a presented token
	// that both verified: "evidence of tampering and SHOULD be surfaced to
	// operators" (protocol, Resource Token Verification).
	Tampered bool
	Detail   string
	Cause    error
}

func (e *TokenError) Error() string {
	code := string(e.Code)
	if code == "" {
		code = string(e.Sig)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", code, e.Detail, e.Cause)
	}
	return code + ": " + e.Detail
}

// Unwrap returns the underlying cause.
func (e *TokenError) Unwrap() error { return e.Cause }

// Problem returns the response for a token carried as a request parameter.
func (e *TokenError) Problem() *Problem {
	code := e.Code
	if code == "" || e.Internal {
		code = CodeServerError
	}
	detail := e.Detail
	if e.Internal {
		detail = ""
	}
	return NewProblem(code, detail)
}

func tokenFail(sig SignatureErrorCode, detail string) *TokenError {
	return &TokenError{Sig: sig, Detail: detail}
}

// paramCode maps a Signature-Error code to the code for a token carried as the
// parameter p. Token Endpoint Error Codes: invalid_presented_token covers a
// malformed token or failed signature, expired_ only "when only exp fails",
// revoked_ when revoked, and clock_skew keeps its own name for a parameter too.
func paramCode(sig SignatureErrorCode, p TokenParam) ErrorCode {
	switch sig {
	case SigExpiredJWT:
		return ErrorCode("expired_" + string(p) + "_token")
	case SigRevokedJWT:
		return ErrorCode("revoked_" + string(p) + "_token")
	case SigClockSkew:
		return CodeClockSkew
	default:
		return ErrorCode("invalid_" + string(p) + "_token")
	}
}

func (e *TokenError) forParam(p TokenParam) *TokenError {
	if e.Internal {
		e.Code = CodeServerError
	} else {
		e.Code = paramCode(e.Sig, p)
	}
	return e
}

// AgentToken is a verified agent token. Key and Thumbprint are its cnf.jwk.
type AgentToken struct {
	Raw        string
	Header     JWTHeader
	Claims     AgentClaims
	Key        JWK
	Thumbprint string
}

// PersonToken is a verified person token.
type PersonToken struct {
	Raw        string
	Header     JWTHeader
	Claims     PersonClaims
	Key        JWK
	Thumbprint string
}

// ResourceToken is a verified resource token. It has no cnf; agent_jkt binds it.
type ResourceToken struct {
	Raw    string
	Header JWTHeader
	Claims ResourceClaims
}

// AuthToken is a verified auth token.
type AuthToken struct {
	Raw        string
	Header     JWTHeader
	Claims     AuthClaims
	Key        JWK
	Thumbprint string
}

// ChainToken is the person token or auth token a recipient verified as a
// presented_token or upstream_token. Exactly one of Person and Auth is set.
type ChainToken struct {
	Person *PersonToken
	Auth   *AuthToken
}

// Raw returns the compact token.
func (t *ChainToken) Raw() string {
	if t.Person != nil {
		return t.Person.Raw
	}
	return t.Auth.Raw
}

// Typ returns the token's JWT type.
func (t *ChainToken) Typ() string {
	if t.Person != nil {
		return TypPerson
	}
	return TypAuth
}

// Iss returns the `iss` claim.
func (t *ChainToken) Iss() string {
	if t.Person != nil {
		return t.Person.Claims.Iss
	}
	return t.Auth.Claims.Iss
}

// PS returns the person server the token names: `iss` of a person token, `ps`
// of an auth token.
func (t *ChainToken) PS() string {
	if t.Person != nil {
		return t.Person.Claims.Iss
	}
	return t.Auth.Claims.PS
}

// Jti returns the `jti` claim.
func (t *ChainToken) Jti() string {
	if t.Person != nil {
		return t.Person.Claims.Jti
	}
	return t.Auth.Claims.Jti
}

// Aud returns the `aud` claim.
func (t *ChainToken) Aud() string {
	if t.Person != nil {
		return t.Person.Claims.Aud
	}
	return t.Auth.Claims.Aud
}

// Sub returns the `sub` claim.
func (t *ChainToken) Sub() string {
	if t.Person != nil {
		return t.Person.Claims.Sub
	}
	return t.Auth.Claims.Sub
}

// Exp returns the `exp` claim.
func (t *ChainToken) Exp() int64 {
	if t.Person != nil {
		return t.Person.Claims.Exp
	}
	return t.Auth.Claims.Exp
}

// MissionS256 returns the `mission_s256` claim.
func (t *ChainToken) MissionS256() string {
	if t.Person != nil {
		return t.Person.Claims.MissionS256
	}
	return t.Auth.Claims.MissionS256
}

// Tenant returns the `tenant` claim.
func (t *ChainToken) Tenant() string {
	if t.Person != nil {
		return t.Person.Claims.Tenant
	}
	return t.Auth.Claims.Tenant
}

// Key returns the token's cnf.jwk.
func (t *ChainToken) Key() JWK {
	if t.Person != nil {
		return t.Person.Key
	}
	return t.Auth.Key
}

// Thumbprint returns the JWK Thumbprint of cnf.jwk.
func (t *ChainToken) Thumbprint() string {
	if t.Person != nil {
		return t.Person.Thumbprint
	}
	return t.Auth.Thumbprint
}

// TokenVerifier implements Common JWT Verification and each token's own section.
type TokenVerifier struct {
	Keys   KeyResolver
	Clock  func() time.Time
	Policy IdentifierPolicy
	// Revocations answers revoked_jwt and revoked_<parameter>_token. Nil means
	// this verifier holds no revocations.
	Revocations RevocationChecker
	// IatWindow bounds how far `iat` may be ahead of this clock. Zero uses
	// DefaultSignatureWindow; negative turns the OPTIONAL bound off.
	IatWindow time.Duration
}

func (v *TokenVerifier) now() time.Time {
	if v.Clock != nil {
		return v.Clock()
	}
	return time.Now()
}

// verifyJWT runs the common pipeline. check holds the token's own checks.
//
// Order matters: the protocol's Common JWT Verification checks the signature
// before exp, and "expired_presented_token when only exp fails" requires every
// other check to have passed first, so a forgery with a past exp is reported as
// invalid, never expired. iss is validated before any fetch, since it decides
// where the fetch goes.
func (v *TokenVerifier) verifyJWT(ctx context.Context, raw, typ string, dwks []string, dst claimsHolder, maxLife time.Duration, check func() *TokenError) (*JWT, *TokenError) {
	token, err := ParseJWT(raw)
	if err != nil {
		return nil, &TokenError{Sig: SigInvalidJWT, Detail: "token is malformed", Cause: err}
	}
	if token.Header.Typ != typ {
		return nil, tokenFail(SigInvalidJWT, "token typ is not "+typ)
	}
	if !isSupportedAlg(token.Header.Alg) {
		return nil, tokenFail(SigInvalidJWT, "token alg is not accepted")
	}
	if token.Header.Kid == "" {
		return nil, tokenFail(SigInvalidJWT, "token header has no kid")
	}
	if err := json.Unmarshal(token.Payload, dst); err != nil {
		return nil, &TokenError{Sig: SigInvalidJWT, Detail: "token payload is malformed", Cause: err}
	}
	c := dst.common()
	dwkOK := false
	for _, d := range dwks {
		dwkOK = dwkOK || c.Dwk == d
	}
	if !dwkOK {
		return nil, tokenFail(SigInvalidJWT, "token dwk is not the expected metadata document")
	}
	if err := v.Policy.ValidateServerIdentifier(c.Iss); err != nil {
		return nil, &TokenError{Sig: SigInvalidJWT, Detail: "token iss is not a server identifier", Cause: err}
	}
	if c.Jti == "" || c.Iat == 0 || c.Exp == 0 {
		return nil, tokenFail(SigInvalidJWT, "token is missing jti, iat or exp")
	}
	if te := v.verifySignature(ctx, token, c); te != nil {
		return nil, te
	}
	if maxLife > 0 && c.Exp-c.Iat > int64(maxLife/time.Second) {
		return nil, tokenFail(SigInvalidJWT, "token lifetime exceeds its ceiling")
	}
	if check != nil {
		if te := check(); te != nil {
			return nil, te
		}
	}
	now := v.now()
	// "Verify exp is in the future... This document defines no tolerance for
	// clock skew on exp."
	if now.Unix() >= c.Exp {
		return nil, tokenFail(SigExpiredJWT, "token has expired")
	}
	window := v.IatWindow
	if window == 0 {
		window = DefaultSignatureWindow
	}
	if window > 0 && c.Iat-now.Unix() > int64(window/time.Second) {
		return nil, tokenFail(SigClockSkew, "token iat is ahead of this server's clock")
	}
	if v.Revocations != nil {
		revoked, err := v.Revocations.IsRevoked(ctx, c.Iss, c.Jti)
		if err != nil {
			return nil, &TokenError{Sig: SigInvalidJWT, Internal: true, Detail: "revocation lookup failed", Cause: err}
		}
		if revoked {
			return nil, tokenFail(SigRevokedJWT, "token has been revoked")
		}
	}
	return token, nil
}

// verifySignature resolves the issuer's key and checks the JWT signature. When a
// cached key fails, it refreshes once, within the floor, and retries: "SHOULD
// refresh the issuer's JWKS once and retry before returning unknown_key (if the
// key is then absent) or invalid_jwt (if verification still fails)".
func (v *TokenVerifier) verifySignature(ctx context.Context, token *JWT, c *Claims) *TokenError {
	if v.Keys == nil {
		return &TokenError{Sig: SigInvalidJWT, Internal: true, Detail: "verifier has no key resolver"}
	}
	key, err := v.Keys.Key(ctx, c.Iss, c.Dwk, token.Header.Kid)
	if err != nil {
		return discoveryTokenError(err)
	}
	err = token.Verify(key)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrBadSignature) {
		return &TokenError{Sig: SigInvalidJWT, Detail: "issuer key is not usable", Cause: err}
	}
	fresh, fetched, rerr := v.Keys.RefreshKey(ctx, c.Iss, c.Dwk, token.Header.Kid)
	if rerr != nil {
		return discoveryTokenError(rerr)
	}
	if fetched && token.Verify(fresh) == nil {
		return nil
	}
	return tokenFail(SigInvalidJWT, "token signature does not verify")
}

// discoveryTokenError turns a key lookup failure into a token failure, keeping
// the issuer_missing, issuer_mismatch and unknown_key distinctions.
func discoveryTokenError(err error) *TokenError {
	var de *DiscoveryError
	if errors.As(err, &de) {
		return &TokenError{Sig: de.Code, Detail: "issuer keys could not be resolved", Cause: err}
	}
	return &TokenError{Sig: SigUnknownKey, Detail: "issuer keys could not be resolved", Cause: err}
}

// confirmationKey applies the checks of protocol section Request-Context
// Binding to cnf.jwk: "If it is absent, or if its JWK is missing kty or the
// members required for that key type, reject the token as structurally
// incomplete before attempting key decoding. If present but not parseable as a
// supported public key, reject it as invalid key material."
//
// Absent and incomplete answer invalid_jwt: the token is what is wrong. An
// unparseable key answers invalid_key, a disagreement between alg and kty or crv
// answers invalid_key, and an absent or unimplemented alg answers
// unsupported_algorithm (protocol, Verification, steps 5 and 6).
func confirmationKey(c *Confirmation) (JWK, string, *TokenError) {
	if c == nil {
		return JWK{}, "", tokenFail(SigInvalidJWT, "token has no cnf.jwk")
	}
	if c.parseErr != nil {
		return JWK{}, "", &TokenError{Sig: SigInvalidKey, Detail: "cnf.jwk cannot be parsed", Cause: c.parseErr}
	}
	if err := c.JWK.CheckStructure(); err != nil {
		return JWK{}, "", &TokenError{Sig: SigInvalidJWT, Detail: "cnf.jwk is structurally incomplete", Cause: err}
	}
	if _, err := c.JWK.Algorithm(); err != nil {
		var ke *KeyError
		errors.As(err, &ke)
		return JWK{}, "", &TokenError{Sig: ke.Code, Detail: "cnf.jwk has no usable algorithm", Cause: err}
	}
	if err := c.JWK.Validate(); err != nil {
		return JWK{}, "", &TokenError{Sig: SigInvalidKey, Detail: "cnf.jwk is not a usable public key", Cause: err}
	}
	tp, err := c.JWK.Thumbprint()
	if err != nil {
		return JWK{}, "", &TokenError{Sig: SigInvalidKey, Detail: "cnf.jwk has no thumbprint", Cause: err}
	}
	return c.JWK, tp, nil
}

// keyBinding is what a token's cnf.jwk must match: the key that signed the
// request, or a thumbprint standing in for it. The zero value compares nothing,
// which is right when cnf.jwk is itself the key verifying the signature, and for
// an upstream token, whose cnf.jwk "is not compared with the key that signed
// this request".
type keyBinding struct {
	key        *JWK
	thumbprint string
}

func (b keyBinding) check(tp string) *TokenError {
	want := b.thumbprint
	if b.key != nil {
		t, err := b.key.Thumbprint()
		if err != nil {
			return &TokenError{Sig: SigInvalidJWT, Detail: "signing key has no thumbprint", Cause: err}
		}
		want = t
	}
	if want != "" && want != tp {
		return tokenFail(SigInvalidJWT, "cnf.jwk does not match the signing key")
	}
	return nil
}

// AgentTokenOptions configures VerifyAgentToken.
type AgentTokenOptions struct {
	// SigningKey, when set, must be the token's cnf.jwk. The request verifier
	// leaves it unset because it takes the signing key from cnf.jwk itself.
	SigningKey *JWK
}

// VerifyAgentToken applies Common JWT Verification with typ aa-agent+jwt and dwk
// aauth-agent.json, then Agent Token Verification. A failure's Sig is the
// Signature-Error code; an agent token at the PS always arrives in Signature-Key.
func (v *TokenVerifier) VerifyAgentToken(ctx context.Context, raw string, opts AgentTokenOptions) (*AgentToken, error) {
	t, te := v.verifyAgent(ctx, raw, opts, nil)
	if te != nil {
		return nil, te
	}
	return t, nil
}

func (v *TokenVerifier) verifyAgent(ctx context.Context, raw string, opts AgentTokenOptions, extra func(*AgentClaims) *TokenError) (*AgentToken, *TokenError) {
	var claims AgentClaims
	var key JWK
	var tp string
	jwt, te := v.verifyJWT(ctx, raw, TypAgent, []string{DwkAgent}, &claims, 0, func() *TokenError {
		var te *TokenError
		if key, tp, te = confirmationKey(claims.Cnf); te != nil {
			return te
		}
		if te := (keyBinding{key: opts.SigningKey}).check(tp); te != nil {
			return te
		}
		if claims.ParentAgent != "" {
			if err := v.Policy.ValidateAgentIdentifier(claims.ParentAgent); err != nil {
				return &TokenError{Sig: SigInvalidJWT, Detail: "parent_agent is not an agent identifier", Cause: err}
			}
		}
		// "For protocol decisions, verifiers rely on parent_agent, not on parsing the local part; the naming is for operational readability". What a `+` means is the issuer's rule to keep (ValidateTopLevelAgentIdentifier,
		// ValidateSubAgentIdentifier), and it decides nothing here. What's checked is that this is an identifier at all.
		if err := v.Policy.ValidateAgentIdentifier(claims.Sub); err != nil {
			return &TokenError{Sig: SigInvalidJWT, Detail: "sub is not an agent identifier", Cause: err}
		}
		if !v.Policy.AgentBelongsToIssuer(claims.Sub, claims.Iss) {
			return tokenFail(SigInvalidJWT, "sub is not under the token's issuer")
		}
		if claims.PS != "" {
			if err := v.Policy.ValidateServerIdentifier(claims.PS); err != nil {
				return &TokenError{Sig: SigInvalidJWT, Detail: "ps is not a server identifier", Cause: err}
			}
		}
		if extra != nil {
			return extra(&claims)
		}
		return nil
	})
	if te != nil {
		return nil, te
	}
	return &AgentToken{Raw: raw, Header: jwt.Header, Claims: claims, Key: key, Thumbprint: tp}, nil
}

// PersonTokenOptions configures VerifyPersonToken.
type PersonTokenOptions struct {
	// Audience is the identifier `aud` must equal. Required.
	Audience string
	// SigningKey, when set, must be the token's cnf.jwk.
	SigningKey *JWK
}

// VerifyPersonToken applies Common JWT Verification with typ aa-person+jwt and
// dwk aauth-person.json, then Person Token Verification.
func (v *TokenVerifier) VerifyPersonToken(ctx context.Context, raw string, opts PersonTokenOptions) (*PersonToken, error) {
	t, te := v.verifyPerson(ctx, raw, opts.Audience, keyBinding{key: opts.SigningKey})
	if te != nil {
		return nil, te
	}
	return t, nil
}

func (v *TokenVerifier) verifyPerson(ctx context.Context, raw, audience string, binding keyBinding) (*PersonToken, *TokenError) {
	var claims PersonClaims
	var key JWK
	var tp string
	jwt, te := v.verifyJWT(ctx, raw, TypPerson, []string{DwkPerson}, &claims, MaxPersonTokenLifetime, func() *TokenError {
		if audience == "" || claims.Aud != audience {
			return tokenFail(SigInvalidJWT, "aud is not the expected identifier")
		}
		if claims.Sub == "" {
			return tokenFail(SigInvalidJWT, "token has no sub")
		}
		for _, forbidden := range []string{"scope", "account"} {
			if _, ok := claims.Extra[forbidden]; ok {
				return tokenFail(SigInvalidJWT, "a person token must not contain "+forbidden)
			}
		}
		if claims.MissionS256 != "" && !validMissionS256(claims.MissionS256) {
			return tokenFail(SigInvalidJWT, "mission_s256 is malformed")
		}
		var te *TokenError
		if key, tp, te = confirmationKey(claims.Cnf); te != nil {
			return te
		}
		return binding.check(tp)
	})
	if te != nil {
		return nil, te
	}
	return &PersonToken{Raw: raw, Header: jwt.Header, Claims: claims, Key: key, Thumbprint: tp}, nil
}

// AuthTokenOptions configures VerifyAuthToken.
type AuthTokenOptions struct {
	// Audience is the identifier `aud` must equal. Required.
	Audience string
	// SigningKey, when set, must be the token's cnf.jwk.
	SigningKey *JWK
}

// VerifyAuthToken applies Common JWT Verification with typ aa-auth+jwt and dwk
// aauth-access.json or aauth-person.json, then Auth Token Verification. The
// resource's record check on (iss, sub) is the caller's, since it needs state.
func (v *TokenVerifier) VerifyAuthToken(ctx context.Context, raw string, opts AuthTokenOptions) (*AuthToken, error) {
	t, te := v.verifyAuth(ctx, raw, opts.Audience, keyBinding{key: opts.SigningKey})
	if te != nil {
		return nil, te
	}
	return t, nil
}

func (v *TokenVerifier) verifyAuth(ctx context.Context, raw, audience string, binding keyBinding) (*AuthToken, *TokenError) {
	var claims AuthClaims
	var key JWK
	var tp string
	jwt, te := v.verifyJWT(ctx, raw, TypAuth, []string{DwkAccess, DwkPerson}, &claims, MaxAuthTokenLifetime, func() *TokenError {
		if audience == "" || claims.Aud != audience {
			return tokenFail(SigInvalidJWT, "aud is not the expected identifier")
		}
		if claims.Sub == "" {
			return tokenFail(SigInvalidJWT, "token has no sub")
		}
		if err := v.Policy.ValidateServerIdentifier(claims.PS); err != nil {
			return &TokenError{Sig: SigInvalidJWT, Detail: "ps is not a server identifier", Cause: err}
		}
		// "ps: Equal to iss when a PS issued the token."
		if claims.Dwk == DwkPerson && claims.PS != claims.Iss {
			return tokenFail(SigInvalidJWT, "a PS-issued auth token has ps equal to iss")
		}
		if err := checkAuthExtensions(&claims); err != nil {
			return &TokenError{Sig: SigInvalidJWT, Detail: "token extension claims are malformed", Cause: err}
		}
		var te *TokenError
		if key, tp, te = confirmationKey(claims.Cnf); te != nil {
			return te
		}
		return binding.check(tp)
	})
	if te != nil {
		return nil, te
	}
	return &AuthToken{Raw: raw, Header: jwt.Header, Claims: claims, Key: key, Thumbprint: tp}, nil
}

// ResourceTokenOptions configures VerifyResourceToken.
type ResourceTokenOptions struct {
	// Audience is the recipient's own identifier: "the PS in three-party, or the
	// AS in four-party". Required.
	Audience string
	// AgentThumbprint is what agent_jkt must equal: the thumbprint of the key
	// that signed the request, or for a parent-mediated authorization the
	// subagent_token's cnf.jwk. Required.
	AgentThumbprint string
	// ExpectedPS is what `ps` must equal: "A PS MUST verify that ps names itself;
	// an AS MUST verify that ps names the PS that sent the token request." Required.
	ExpectedPS string
}

// VerifyResourceToken applies Common JWT Verification with typ aa-resource+jwt
// and dwk aauth-resource.json, then steps 1 and 2 of Resource Token Verification.
// Use VerifyTokenRequest to run step 3 with it. Errors carry Code: one of
// invalid_, expired_ or revoked_resource_token, or clock_skew.
func (v *TokenVerifier) VerifyResourceToken(ctx context.Context, raw string, opts ResourceTokenOptions) (*ResourceToken, error) {
	var claims ResourceClaims
	jwt, te := v.verifyJWT(ctx, raw, TypResource, []string{DwkResource}, &claims, 0, func() *TokenError {
		if opts.Audience == "" || claims.Aud != opts.Audience {
			return tokenFail(SigInvalidJWT, "aud is not this recipient")
		}
		if err := v.Policy.ValidateServerIdentifier(claims.PS); err != nil {
			return &TokenError{Sig: SigInvalidJWT, Detail: "ps is not a server identifier", Cause: err}
		}
		if opts.ExpectedPS == "" || claims.PS != opts.ExpectedPS {
			return tokenFail(SigInvalidJWT, "ps is not the expected person server")
		}
		if claims.Sub == "" || claims.PresentedJti == "" || claims.AgentJKT == "" {
			return tokenFail(SigInvalidJWT, "token needs sub, presented_jti and agent_jkt")
		}
		if opts.AgentThumbprint == "" || claims.AgentJKT != opts.AgentThumbprint {
			return tokenFail(SigInvalidJWT, "agent_jkt does not match the signing key")
		}
		if err := checkResourceExtensions(&claims); err != nil {
			return &TokenError{Sig: SigInvalidJWT, Detail: "token extension claims are malformed", Cause: err}
		}
		return nil
	})
	if te != nil {
		te = te.forParam(ParamResource)
		// "invalid_budget... or the object is otherwise malformed" (budgets, Errors).
		if errors.Is(te.Cause, errMalformedBudget) {
			te.Code = CodeInvalidBudget
		}
		return nil, te
	}
	return &ResourceToken{Raw: raw, Header: jwt.Header, Claims: claims}, nil
}

// peekTyp reads the JWT type without verifying anything, for dispatch.
func peekTyp(raw string) string {
	header, _, ok := strings.Cut(raw, ".")
	if !ok {
		return ""
	}
	b, err := b64url.DecodeString(header)
	if err != nil {
		return ""
	}
	var h JWTHeader
	if json.Unmarshal(b, &h) != nil {
		return ""
	}
	return h.Typ
}

// verifyChainToken verifies a person token or auth token by its typ.
func (v *TokenVerifier) verifyChainToken(ctx context.Context, raw, audience string, binding keyBinding) (*ChainToken, *TokenError) {
	switch peekTyp(raw) {
	case TypPerson:
		t, te := v.verifyPerson(ctx, raw, audience, binding)
		if te != nil {
			return nil, te
		}
		return &ChainToken{Person: t}, nil
	case TypAuth:
		t, te := v.verifyAuth(ctx, raw, audience, binding)
		if te != nil {
			return nil, te
		}
		return &ChainToken{Auth: t}, nil
	}
	return nil, tokenFail(SigInvalidJWT, "token typ is neither a person token nor an auth token")
}

// VerifyPresentedToken applies step 3 of Resource Token Verification to a
// presented_token.
//
// The token is verified as a person token or auth token by its typ, with the two
// substitutions the spec makes: "aud MUST equal the resource token's iss rather
// than the verifier's own identifier, and cnf.jwk MUST match the resource token's
// agent_jkt rather than the key that signed the request". The resource's record
// check on sub does not apply.
//
// A token failure carries invalid_presented_token, expired_presented_token (only
// when exp alone fails), revoked_presented_token or clock_skew. Then the token is
// compared with the resource token: jti against presented_jti, iss or ps against
// ps, and sub, mission_s256 and tenant exactly. Any mismatch rejects the resource
// token with invalid_resource_token and sets Tampered.
func (v *TokenVerifier) VerifyPresentedToken(ctx context.Context, raw string, rt *ResourceToken) (*ChainToken, error) {
	t, te := v.verifyChainToken(ctx, raw, rt.Claims.Iss, keyBinding{thumbprint: rt.Claims.AgentJKT})
	if te != nil {
		return nil, te.forParam(ParamPresented)
	}
	c := &rt.Claims
	mismatch := ""
	switch {
	case t.Jti() != c.PresentedJti:
		mismatch = "presented_jti"
	case t.PS() != c.PS:
		mismatch = "ps"
	case t.Sub() != c.Sub:
		mismatch = "sub"
	case t.MissionS256() != c.MissionS256:
		mismatch = "mission_s256"
	case t.Tenant() != c.Tenant:
		mismatch = "tenant"
	}
	if mismatch != "" {
		return nil, &TokenError{
			Sig:      SigInvalidJWT,
			Code:     CodeInvalidResourceToken,
			Tampered: true,
			Detail:   "resource token and presented token disagree on " + mismatch,
		}
	}
	return t, nil
}

// TokenRequestInput is what an auth token request carries for verification.
type TokenRequestInput struct {
	ResourceToken  string
	PresentedToken string
	// Audience is the recipient's identifier, for the resource token's aud.
	Audience string
	// ExpectedPS is what `ps` must name: this PS, or at an AS the PS that signed.
	ExpectedPS string
	// SigningThumbprint is the thumbprint of the key that signed the request.
	SigningThumbprint string
	// Subagent is the verified subagent_token of a parent-mediated request. The
	// resource token then binds to its key: "verify it against the
	// subagent_token's cnf.jwk instead, since the parent signs the request."
	Subagent *AgentToken
}

// VerifyTokenRequest runs steps 1 to 3 of Resource Token Verification. Step 4, on
// the mission's state, needs the PS's records and is the caller's.
func (v *TokenVerifier) VerifyTokenRequest(ctx context.Context, in TokenRequestInput) (*ResourceToken, *ChainToken, error) {
	thumbprint := in.SigningThumbprint
	if in.Subagent != nil {
		thumbprint = in.Subagent.Thumbprint
	}
	rt, err := v.VerifyResourceToken(ctx, in.ResourceToken, ResourceTokenOptions{
		Audience:        in.Audience,
		AgentThumbprint: thumbprint,
		ExpectedPS:      in.ExpectedPS,
	})
	if err != nil {
		return nil, nil, err
	}
	presented, err := v.VerifyPresentedToken(ctx, in.PresentedToken, rt)
	if err != nil {
		return rt, nil, err
	}
	return rt, presented, nil
}

// CheckUpdatedRequest applies the updated_request invariants: "The new resource
// token MUST have the same iss, ps, sub, agent_jkt, mission_s256, and tenant as
// the original; its presented_jti MAY differ, and MUST equal the jti of the
// presented_token sent with it." Both tokens have already been verified.
func CheckUpdatedRequest(original, updated *ResourceToken, presented *ChainToken) error {
	o, u := &original.Claims, &updated.Claims
	mismatch := ""
	switch {
	case u.Iss != o.Iss:
		mismatch = "iss"
	case u.PS != o.PS:
		mismatch = "ps"
	case u.Sub != o.Sub:
		mismatch = "sub"
	case u.AgentJKT != o.AgentJKT:
		mismatch = "agent_jkt"
	case u.MissionS256 != o.MissionS256:
		mismatch = "mission_s256"
	case u.Tenant != o.Tenant:
		mismatch = "tenant"
	case u.PresentedJti != presented.Jti():
		mismatch = "presented_jti"
	}
	if mismatch != "" {
		return &TokenError{Sig: SigInvalidJWT, Code: CodeInvalidResourceToken, Tampered: true, Detail: "updated resource token differs on " + mismatch}
	}
	return nil
}

// UpstreamTokenOptions configures VerifyUpstreamToken.
type UpstreamTokenOptions struct {
	// Intermediary is the `iss` of the intermediary's agent token: the upstream
	// token's aud must equal it. Required.
	Intermediary string
	// PS is this person server's identifier, set when verifying at a PS.
	PS string
	// RequestingPS is the PS that signed the request, set when verifying at an AS.
	RequestingPS string
	// ASPresented answers, at a PS, whether this PS presented a person token to
	// the access server asIss for that aud and sub. Nil answers no.
	ASPresented func(ctx context.Context, asIss, aud, sub string) (bool, error)
}

// VerifyUpstreamToken applies steps 1 to 3 of Upstream Token Verification.
//
// The substitutions: aud must equal the intermediary's identifier rather than the
// verifier's own, cnf.jwk "is not compared with the key that signed this request",
// and the record check on sub does not apply. Step 4 (identifying the calling
// agent from the PS's records) and step 5 (mission and policy) are the PS's.
//
// Errors carry invalid_upstream_token, expired_upstream_token when only exp
// fails, revoked_upstream_token, or clock_skew. Any typ other than a person token
// or auth token is invalid_upstream_token.
func (v *TokenVerifier) VerifyUpstreamToken(ctx context.Context, raw string, opts UpstreamTokenOptions) (*ChainToken, error) {
	if opts.Intermediary == "" {
		return nil, (&TokenError{Sig: SigInvalidJWT, Detail: "no intermediary to compare aud with"}).forParam(ParamUpstream)
	}
	t, te := v.verifyChainToken(ctx, raw, opts.Intermediary, keyBinding{})
	if te != nil {
		return nil, te.forParam(ParamUpstream)
	}
	switch {
	case opts.PS != "":
		if t.Person != nil {
			if t.Iss() != opts.PS {
				return nil, tokenFail(SigInvalidJWT, "upstream person token's iss is not this PS").forParam(ParamUpstream)
			}
			break
		}
		if t.PS() != opts.PS {
			return nil, tokenFail(SigInvalidJWT, "upstream auth token's ps is not this PS").forParam(ParamUpstream)
		}
		if t.Iss() != opts.PS {
			presented := false
			if opts.ASPresented != nil {
				var err error
				presented, err = opts.ASPresented(ctx, t.Iss(), t.Aud(), t.Sub())
				if err != nil {
					return nil, (&TokenError{Sig: SigInvalidJWT, Internal: true, Detail: "record lookup failed", Cause: err}).forParam(ParamUpstream)
				}
			}
			if !presented {
				return nil, tokenFail(SigInvalidJWT, "upstream auth token's iss is neither this PS nor an AS it presented to").forParam(ParamUpstream)
			}
		}
	case opts.RequestingPS != "":
		if t.PS() != opts.RequestingPS {
			return nil, tokenFail(SigInvalidJWT, "upstream token does not name the requesting PS").forParam(ParamUpstream)
		}
	default:
		return nil, tokenFail(SigInvalidJWT, "no person server to compare the upstream token with").forParam(ParamUpstream)
	}
	return t, nil
}

// VerifySubagentToken verifies a subagent_token against the agent that signed the
// request: Agent Token Verification, then "its parent_agent names the parent" and
// "The iss of a sub-agent's agent token MUST equal the iss of its parent's". Its
// cnf.jwk is the sub-agent's key, not the signer's, so it is not compared. Errors
// carry invalid_subagent_token, expired_subagent_token, revoked_subagent_token or
// clock_skew.
func (v *TokenVerifier) VerifySubagentToken(ctx context.Context, raw string, parent *AgentToken) (*AgentToken, error) {
	t, te := v.verifyAgent(ctx, raw, AgentTokenOptions{}, func(c *AgentClaims) *TokenError {
		if c.ParentAgent == "" {
			return tokenFail(SigInvalidJWT, "subagent_token has no parent_agent")
		}
		if c.ParentAgent != parent.Claims.Sub {
			return tokenFail(SigInvalidJWT, "parent_agent does not name the signing agent")
		}
		if c.Iss != parent.Claims.Iss {
			return tokenFail(SigInvalidJWT, "subagent_token's iss is not the signing agent's")
		}
		return nil
	})
	if te != nil {
		return nil, te.forParam(ParamSubagent)
	}
	return t, nil
}

// DeliveryChecks are the expected values for an auth token an AS returned.
type DeliveryChecks struct {
	// AS is the access server the PS sent the token request to.
	AS string
	// Resource is the resource token's iss.
	Resource string
	// AgentKey is the agent's signing key, or the sub-agent's for a
	// parent-mediated request.
	AgentKey JWK
	// Sub is the directed identifier the PS issues for this person at this resource.
	Sub string
	// ResourceTokenScope is the resource token's scope.
	ResourceTokenScope string
	// PresentedExp is the exp of the presented_token the PS presented.
	PresentedExp int64
	// BudgetSent is the budget parameter the PS sent, nil when it sent none.
	BudgetSent *Budget
	// ResourceToken carries the R3 claims the delivered token must repeat.
	ResourceToken *ResourceToken
	// R3Document is the verified R3 document, required when the token carries R3
	// claims: every operation it grants must appear there.
	R3Document *R3Document
}

// VerifyDeliveredAuthToken applies Auth Token Delivery, with the Budgets and R3
// additions. On failure the PS answers as_unreachable, which the error's Code
// states: "or an auth token that fails delivery verification".
func (v *TokenVerifier) VerifyDeliveredAuthToken(ctx context.Context, raw string, want DeliveryChecks) (*AuthToken, error) {
	fail := func(detail string, cause error) (*AuthToken, error) {
		return nil, &TokenError{Sig: SigInvalidJWT, Code: CodeASUnreachable, Detail: detail, Cause: cause}
	}
	thumbprint, err := want.AgentKey.Thumbprint()
	if err != nil {
		return fail("agent key has no thumbprint", err)
	}
	t, te := v.verifyAuth(ctx, raw, want.Resource, keyBinding{thumbprint: thumbprint})
	if te != nil {
		return fail("auth token failed verification", te)
	}
	c := &t.Claims
	switch {
	case c.Iss != want.AS:
		return fail("iss is not the access server", nil)
	case c.Sub != want.Sub:
		return fail("sub is not this person's directed identifier", nil)
	case !ScopeWithin(c.Scope, want.ResourceTokenScope):
		return fail("scope is broader than the resource token's", nil)
	case want.PresentedExp != 0 && c.Exp > want.PresentedExp:
		return fail("exp is later than the presented token's", nil)
	}
	if want.BudgetSent == nil && c.Budget != nil {
		return fail("token carries a budget the PS did not send", nil)
	}
	if want.BudgetSent != nil {
		switch {
		case c.Budget == nil:
			return fail("token has no budget though the PS sent one", nil)
		case c.Budget.Unit != want.BudgetSent.Unit || c.Budget.Decimals != want.BudgetSent.Decimals:
			return fail("budget unit or decimals differ from what the PS sent", nil)
		case c.Budget.Amount > want.BudgetSent.Amount:
			return fail("budget amount is higher than what the PS sent", nil)
		}
	}
	if c.R3URI != "" {
		if want.ResourceToken == nil || c.R3URI != want.ResourceToken.Claims.R3URI || c.R3S256 != want.ResourceToken.Claims.R3S256 {
			return fail("R3 claims differ from the resource token's", nil)
		}
		if want.R3Document == nil {
			return fail("token carries R3 claims but no R3 document was checked", nil)
		}
		for _, ops := range []*R3Operations{c.R3Granted, c.R3PerCall} {
			if ops != nil && !want.R3Document.CoversOperations(ops) {
				return fail("token grants an operation the R3 document does not list", nil)
			}
		}
	}
	return t, nil
}

// ScopeWithin reports whether every value in the space-separated granted scope
// appears in allowed: "MUST NOT be broader than the resource token's scope".
func ScopeWithin(granted, allowed string) bool {
	have := map[string]bool{}
	for _, s := range strings.Fields(allowed) {
		have[s] = true
	}
	for _, s := range strings.Fields(granted) {
		if !have[s] {
			return false
		}
	}
	return true
}
