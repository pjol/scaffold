package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/aauth"
	"scaffoldapp/backend/internal/aauth/keys"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AAuth (draft-hardt-oauth-aauth-protocol-11): agents that sign every request
// (RFC 9421) with a token from their person server reach /mcp as the person
// they act for. This server is the protocol's resource, in PS authorization
// (three-party) access: an agent presents a person token and gets a resource
// token for read (and write when the call needs it); its person server asks
// the person and returns an auth token; every call is signed with that. The
// person server's identifier for the person, (iss, sub), is linked to an
// account once, by the person signing in at /connect/aauth and confirming.
// There is no access server (four-party), no session token (two-party) and
// no access on identity alone: every call needs an auth token. The protocol
// library is internal/aauth, shared with Sayso's person server.

const (
	aauthPendingTTL  = 15 * time.Minute // how long a person has to link, and then the agent to collect
	aauthCodeFailure = 5                // wrong codes that end the requests sharing a code prefix
	aauthMaxOpen     = 5                // open link requests per person server identity
	aauthKeyMaxAge   = 30 * 24 * time.Hour
	aauthRetryAfter  = 5
	// aauthMaxBody is read for the digest check: above the 1 MiB MCP cap, so an
	// oversize body gets 413 rather than a signature failure.
	aauthMaxBody = 2 << 20
)

// aauthResource is the AAuth side of the mcp module; nil when it is off.
type aauthResource struct {
	s         *Service
	id        string // this resource's identifier: PUBLIC_API_URL's scheme and host
	policy    aauth.IdentifierPolicy
	discovery *aauth.Discovery
	keys      *keys.RoleKeys
	agents    *aauth.Verifier // agents' signed requests: /mcp, /aauth/authorize, /aauth/pending
	servers   *aauth.Verifier // person servers signing as themselves: /aauth/revoke
}

// newAAuth sets AAuth up, or says in the log why it is off: PUBLIC_API_URL
// must be a server identifier (https, scheme and host only; loopback http in
// development), and the link page needs the web app.
func newAAuth(s *Service) *aauthResource {
	if !config.Bool("MCP_AAUTH_ENABLED", true) {
		slog.Info("mcp: aauth off (MCP_AAUTH_ENABLED=false)")
		return nil
	}
	policy := aauth.IdentifierPolicy{AllowInsecureLoopback: s.d.Cfg.Env == "development"}
	id, err := resourceIdentifier(s.d.Cfg.PublicAPIURL, policy)
	if err != nil {
		slog.Warn("mcp: aauth off: PUBLIC_API_URL is not an AAuth server identifier (https, scheme and host only)", "err", err)
		return nil
	}
	if s.webURL() == "" || s.d.Pool == nil {
		slog.Warn("mcp: aauth off: the link page needs PUBLIC_WEB_URL")
		return nil
	}
	secret := keys.SecretFromEnv()
	if secret == nil {
		return nil // config.Problem says why
	}
	store, err := keys.New(s.d.Pool, secret)
	if err != nil {
		config.Problem("mcp: aauth signing keys: %v", err)
		return nil
	}
	a := &aauthResource{s: s, id: id, policy: policy, keys: store.Role(keys.RoleResource)}
	a.discovery = aauth.NewDiscovery(aauth.DiscoveryConfig{Policy: policy})
	// Bodies must be covered by content-type and content-digest: a JSON-RPC
	// body is the whole of what a call does.
	if a.agents, err = aauth.NewVerifier(aauth.VerifierConfig{
		Keys: a.discovery, Scheme: aauth.ResourcePolicy(aauth.TypAgent, aauth.TypPerson, aauth.TypAuth),
		Origin: id, Audience: id, Identifiers: policy, MaxBodyBytes: aauthMaxBody,
		Revocations: aauth.RevocationCheckerFunc(a.revoked),
	}); err != nil {
		config.Problem("mcp: aauth verifier: %v", err)
		return nil
	}
	if a.servers, err = aauth.NewVerifier(aauth.VerifierConfig{
		Keys: a.discovery, Scheme: aauth.ServerPolicy(aauth.DwkPerson), Origin: id, Identifiers: policy,
	}); err != nil {
		config.Problem("mcp: aauth verifier: %v", err)
		return nil
	}
	slog.Info("mcp: aauth", "resource", id)
	return a
}

// resourceIdentifier is PUBLIC_API_URL as an AAuth server identifier.
func resourceIdentifier(publicAPIURL string, policy aauth.IdentifierPolicy) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(publicAPIURL), "/"))
	if err != nil {
		return "", err
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", errors.New("the API must be served at the root of its origin")
	}
	id := strings.ToLower(u.Scheme + "://" + u.Host)
	return id, policy.ValidateServerIdentifier(id)
}

func (a *aauthResource) mountRoot(r chi.Router) {
	r.Get("/.well-known/aauth-resource.json", a.metadata)
	r.Get("/aauth/jwks.json", a.jwks)
	r.Post("/aauth/authorize", a.authorize)
	r.Get("/aauth/pending/{id}", a.poll)
	r.Post("/aauth/revoke", a.revoke)
}

func (a *aauthResource) metadataURL() string { return a.id + "/.well-known/aauth-resource.json" }
func (a *aauthResource) linkPageURL() string { return a.s.webURL() + "/connect/aauth" }

// metadata is GET /.well-known/aauth-resource.json.
func (a *aauthResource) metadata(w http.ResponseWriter, _ *http.Request) {
	web := a.s.webURL()
	publicJSON(w, aauth.ResourceMetadata{
		CommonMetadata: aauth.CommonMetadata{
			Issuer:              a.id,
			JWKSURI:             a.id + "/aauth/jwks.json",
			AcceptSignatureAlgs: []string{string(aauth.AlgEd25519), string(aauth.AlgES256)},
			Name:                "Scaffold App",
			Description:         aauthDescription,
			TOSURI:              web + "/legal/terms",
			PolicyURI:           web + "/legal/privacy",
		},
		AccessMode:            aauth.AccessModeAuthToken,
		AuthorizationEndpoint: a.id + "/aauth/authorize",
		ScopeDescriptions: map[string]string{
			ScopeRead:  "Use read-only tools as you: see what you can see in Scaffold App.",
			ScopeWrite: "Also use tools that change data in Scaffold App, as you.",
		},
		SignatureWindow:    int(aauth.DefaultSignatureWindow / time.Second),
		RevocationEndpoint: a.id + "/aauth/revoke",
	})
}

// jwks is GET /aauth/jwks.json: the keys resource tokens are signed with,
// including one about to sign and ones still in their overlap.
func (a *aauthResource) jwks(w http.ResponseWriter, r *http.Request) {
	list, err := a.keys.PublicKeys(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	publicJSON(w, aauth.JWKS{Keys: list})
}

// advertise names the resource metadata on every /mcp response, by the
// aauth-resource link relation.
func (a *aauthResource) advertise(w http.ResponseWriter) {
	if a != nil {
		w.Header().Add("Link", fmt.Sprintf("<%s>; rel=%q", a.metadataURL(), "aauth-resource"))
	}
}

// askPersonToken tells an agent that presented nothing what to present. A
// bearer client ignores it; WWW-Authenticate is independent of it.
func (a *aauthResource) askPersonToken(w http.ResponseWriter) {
	if a != nil {
		a.setRequirement(w, aauth.Requirement{Value: aauth.RequirementPersonToken})
	}
}

func (a *aauthResource) setRequirement(w http.ResponseWriter, q aauth.Requirement) {
	if field, err := q.FieldWithPolicy(a.policy); err == nil {
		w.Header().Set(aauth.HeaderAAuthRequirement, field)
	}
}

// require answers a /mcp call with an AAuth requirement, in the API's error
// envelope: agents act on the header, people read the message.
func (a *aauthResource) require(w http.ResponseWriter, r *http.Request, status int, q aauth.Requirement, code, msg string) {
	a.setRequirement(w, q)
	httpx.Fail(w, r, httpx.NewError(status, code, msg))
}

func (a *aauthResource) requirePersonToken(w http.ResponseWriter, r *http.Request) {
	a.require(w, r, http.StatusUnauthorized, aauth.Requirement{Value: aauth.RequirementPersonToken}, "person_token_required",
		"Sign with a person token from your person server for this resource.")
}

// signed reports whether a request carries an HTTP message signature, and so
// takes the AAuth path rather than a bearer credential.
func signed(r *http.Request) bool {
	h := r.Header
	return h.Get(aauth.HeaderSignatureKey) != "" || h.Get(aauth.HeaderSignature) != "" || h.Get(aauth.HeaderSignatureInput) != ""
}

// verify runs the seven verification steps; a failure is 401 with
// Signature-Error. "A resource refusing a revoked auth token SHOULD also
// include AAuth-Requirement: requirement=person-token."
func (a *aauthResource) verify(w http.ResponseWriter, r *http.Request) (*aauth.VerifiedRequest, bool) {
	v, err := a.agents.Verify(r.Context(), r)
	if err != nil {
		var f *aauth.SignatureFailure
		if errors.As(err, &f) && f.Code == aauth.SigRevokedJWT {
			a.setRequirement(w, aauth.Requirement{Value: aauth.RequirementPersonToken})
		}
		aauth.WriteVerificationError(w, err)
		return nil, false
	}
	return v, true
}

// serveMCP authenticates a signed /mcp call and hands it to the checks every
// caller passes.
func (a *aauthResource) serveMCP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	v, ok := a.verify(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		httpx.Fail(w, r, errPostOnly)
		return
	}
	write := a.s.writeToolIn(parseRPC(v.Body)) != ""
	p, ok := a.principal(w, r, v, write)
	if !ok {
		return
	}
	a.s.serveAuthenticated(w, r, p, next)
}

// principal decides a verified call by the token it carried. An agent token
// says which agent, not whom it acts for; a person token says whom, and earns
// a resource token, never data ("A recipient MUST reject an aa-person+jwt
// wherever an auth token is required"); an auth token from the person's own
// server, for read, acts as the linked account, or holds the call while the
// person links it.
func (a *aauthResource) principal(w http.ResponseWriter, r *http.Request, v *aauth.VerifiedRequest, write bool) (*principal, bool) {
	ctx := r.Context()
	switch {
	case v.Person != nil:
		c := v.Person.Claims
		l, err := a.findLink(ctx, c.Iss, c.Sub)
		if err != nil {
			httpx.Fail(w, r, err)
			return nil, false
		}
		if l != nil && l.identity.Status != "active" {
			httpx.Fail(w, r, errInactive)
			return nil, false
		}
		a.challenge(w, r, v, a.scopeFor(ctx, write, l == nil, c.Iss))
		return nil, false
	case v.Auth != nil:
		c := v.Auth.Claims
		// No access server here, so the only auth tokens for this resource are
		// the person server's own (dwk aauth-person.json, ps = iss).
		if c.Dwk != aauth.DwkPerson || c.PS != c.Iss {
			a.requirePersonToken(w, r)
			return nil, false
		}
		l, err := a.findLink(ctx, c.PS, c.Sub)
		if err != nil {
			httpx.Fail(w, r, err)
			return nil, false
		}
		scopes := tokenScopes(c.Scope)
		if !slices.Contains(scopes, ScopeRead) {
			a.challenge(w, r, v, a.scopeFor(ctx, write, l == nil, c.PS))
			return nil, false
		}
		if l == nil {
			a.hold(w, r, v)
			return nil, false
		}
		if l.identity.Status != "active" {
			httpx.Fail(w, r, errInactive)
			return nil, false
		}
		if err := a.touch(ctx, l.id, v.Thumbprint); err != nil {
			httpx.Fail(w, r, err)
			return nil, false
		}
		return l.principal(scopes, v), true
	}
	a.requirePersonToken(w, r)
	return nil, false
}

// tokenScopes keeps the scopes this resource defines, out of an auth token's
// (identity scopes such as email ride along and grant nothing here).
func tokenScopes(scope string) []string {
	out := []string{}
	for _, s := range strings.Fields(scope) {
		if (s == ScopeRead || s == ScopeWrite) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// scopeFor is what a resource token asks for: read, write as well when the
// call needs it, and while the identity is unlinked the person's email, when
// their person server offers it, so the link page can show whose account it
// is. "A resource token MUST only include ... identity scopes that the PS has
// declared in its scopes_supported metadata."
func (a *aauthResource) scopeFor(ctx context.Context, write, unlinked bool, ps string) string {
	scope := ScopeRead
	if write {
		scope += " " + ScopeWrite
	}
	if unlinked {
		if m, err := a.discovery.PersonServer(ctx, ps); err == nil && slices.Contains(m.ScopesSupported, "email") {
			scope += " email"
		}
	}
	return scope
}

// challenge answers 401 requirement=auth-token with a resource token for scope.
func (a *aauthResource) challenge(w http.ResponseWriter, r *http.Request, v *aauth.VerifiedRequest, scope string) {
	token, err := a.resourceToken(r.Context(), v, scope)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	a.require(w, r, http.StatusUnauthorized, aauth.Requirement{Value: aauth.RequirementAuthToken, ResourceToken: token},
		"auth_token_required", "Take the resource token in AAuth-Requirement to your person server for an auth token, then sign this call with it.")
}

// resourceToken mints a resource token: "ps, sub, and presented_jti are
// copied from the token the request carried", and with no access server its
// aud is that person server. It carries no agent identifier; agent_jkt binds
// it to the signing key.
func (a *aauthResource) resourceToken(ctx context.Context, v *aauth.VerifiedRequest, scope string) (string, error) {
	now := time.Now()
	c := aauth.ResourceClaims{
		Claims:   aauth.Claims{Iss: a.id, Jti: aauth.NewJTI(), Iat: now.Unix(), Exp: now.Add(aauth.MaxResourceTokenLifetime).Unix()},
		AgentJKT: v.Thumbprint,
		Scope:    scope,
	}
	switch {
	case v.Person != nil:
		p := v.Person.Claims
		c.PS, c.Sub, c.PresentedJti, c.MissionS256, c.Tenant = p.Iss, p.Sub, p.Jti, p.MissionS256, p.Tenant
	case v.Auth != nil:
		t := v.Auth.Claims
		c.PS, c.Sub, c.PresentedJti, c.MissionS256, c.Tenant = t.PS, t.Sub, t.Jti, t.MissionS256, t.Tenant
	default:
		return "", errors.New("mcp: a resource token needs a verified person or auth token")
	}
	c.Aud = c.PS
	signer, err := a.keys.Signer(ctx)
	if err != nil {
		return "", err
	}
	return aauth.TokenIssuer{Key: signer, Policy: a.policy}.Resource(c)
}

// ---- linked identities -----------------------------------------------------

// aauthLink is an active link and the account behind it.
type aauthLink struct {
	id       uuid.UUID
	ps       string
	name     string // the person server's own name, when it gave one
	identity *auth.Identity
}

// findLink returns the account (iss, sub) is linked to, or nil. "A resource
// MUST treat (iss, sub) as the identifier ... and MUST NOT match a sub
// received from one issuer against a record established under another."
func (a *aauthResource) findLink(ctx context.Context, ps, sub string) (*aauthLink, error) {
	l := &aauthLink{ps: ps, identity: &auth.Identity{}}
	id := l.identity
	var role *string
	err := a.s.d.Pool.QueryRow(ctx, `
		SELECT l.id, l.ps_name, `+identityCols+`
		FROM mcp_aauth_links l JOIN users u ON u.id = l.user_id
		WHERE l.ps_iss = $1 AND l.sub = $2 AND l.revoked_at IS NULL`, ps, sub,
	).Scan(&l.id, &l.name, &id.UserID, &id.Subject, &id.Email, &id.DisplayName, &role, &id.Status,
		&id.TermsVersion, &id.PrivacyVersion)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if role != nil {
		id.AdminRole = *role
	}
	return l, nil
}

// principal is the caller an auth token makes of a link. The agent's own
// identifier never reaches a resource, so every agent of that person server
// account is one client: "Agents via <person server>".
func (l *aauthLink) principal(scopes []string, v *aauth.VerifiedRequest) *principal {
	name := l.name
	if name == "" {
		name = aauth.ServerHost(l.ps)
	}
	return &principal{identity: l.identity, via: "aauth", id: l.id, scopes: scopes,
		client: "Agents via " + name, clientID: "aauth:" + l.id.String(), signed: v}
}

// touch records the link's use and the agent's key, at most once a minute each.
func (a *aauthResource) touch(ctx context.Context, link uuid.UUID, jkt string) error {
	if _, err := a.s.d.Pool.Exec(ctx, `
		UPDATE mcp_aauth_links SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, link); err != nil {
		return err
	}
	_, err := a.s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_aauth_agents (link_id, jkt) VALUES ($1, $2)
		ON CONFLICT (link_id, jkt) DO UPDATE SET last_seen_at = now()
		WHERE mcp_aauth_agents.last_seen_at < now() - interval '1 minute'`, link, jkt)
	return err
}

// psName is a person server's name from its metadata, as plain text of at
// most 60 runes; "" when it gives none or can't be fetched. It is the
// server's own claim: pages show it next to the verified host.
func (a *aauthResource) psName(ctx context.Context, ps string) string {
	m, err := a.discovery.PersonServer(ctx, ps)
	if err != nil {
		return ""
	}
	name := strings.Join(strings.FieldsFunc(m.Name, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if rs := []rune(name); len(rs) > 60 {
		name = string(rs[:60])
	}
	return name
}

// ---- held calls ------------------------------------------------------------

// hold answers a call from an identity nobody has linked: 202 with
// requirement=interaction, the link page and a code. The call waits in the
// pending row and runs on the first poll after the person links.
func (a *aauthResource) hold(w http.ResponseWriter, r *http.Request, v *aauth.VerifiedRequest) {
	ctx := r.Context()
	c := v.Auth.Claims
	var open int
	if err := a.s.d.Pool.QueryRow(ctx, `
		SELECT count(*) FROM mcp_aauth_pending
		WHERE ps_iss = $1 AND sub = $2 AND status IN ('pending', 'interacting') AND expires_at > now()`, c.PS, c.Sub).Scan(&open); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if open >= aauthMaxOpen {
		w.Header().Set("Retry-After", "60")
		httpx.Fail(w, r, httpx.NewError(http.StatusTooManyRequests, "rate_limited",
			"Requests from this account are already waiting to be linked. Finish one on the link page, or wait for them to expire."))
		return
	}
	code, err := aauth.NewInteractionCode(aauth.MinInteractionCodeSymbols)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id := pendingID()
	headers, _ := json.Marshal(heldHeaders(r.Header))
	if _, err := a.s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_aauth_pending (id, ps_iss, sub, tenant, email, agent_jkt, code_prefix, code_hash, held_body, held_headers, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, c.PS, c.Sub, nullString(c.Tenant), verifiedEmail(c), v.Thumbprint, code[:4], codeHash(code), v.Body, headers,
		time.Now().Add(aauthPendingTTL)); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := aauth.WritePending(w, aauth.Pending{
		Location: a.id + "/aauth/pending/" + id, RetryAfter: aauthRetryAfter, Policy: a.policy,
		Requirement: &aauth.Requirement{Value: aauth.RequirementInteraction, URL: a.linkPageURL(), Code: aauth.FormatInteractionCode(code)},
	}); err != nil {
		httpx.Fail(w, r, err)
	}
}

// pendingID is an unguessable pending URL id: 128 random bits.
func pendingID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return aauth.Base64URL(b)
}

func codeHash(code string) []byte {
	h := sha256.Sum256([]byte(code))
	return h[:]
}

// heldHeaders keeps what the MCP transport reads from a call, to run it later.
func heldHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"Content-Type", "Accept", "Mcp-Protocol-Version"} {
		if v := h.Get(k); v != "" && len(v) <= 256 {
			out[k] = v
		}
	}
	return out
}

// verifiedEmail is the email an auth token asserts, when the person server
// says it verified it; nil otherwise.
func verifiedEmail(c aauth.AuthClaims) *string {
	var email string
	var verified bool
	if json.Unmarshal(c.Extra["email"], &email) != nil || json.Unmarshal(c.Extra["email_verified"], &verified) != nil || !verified {
		return nil
	}
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 320 || !strings.Contains(email, "@") || strings.ContainsFunc(email, unicode.IsControl) {
		return nil
	}
	return &email
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// pendingRow is what a poll reads.
type pendingRow struct {
	ps, sub, jkt, status  string
	expires               time.Time
	delivered             *time.Time
	resultJti, resultType *string
	resultStatus          *int
	resultBody            []byte
	resultUntil           *time.Time
	heldBody              []byte
	heldHeaders           map[string]string
}

// poll is GET /aauth/pending/{id}: the agent waiting on a held call, verified
// on every poll by the key it signed the call with. "A non-202 response is
// terminal": the held call's own answer, denied, expired or invalid_code;
// after that 410, except that the same auth token gets the same answer again
// until it expires, since "a response can be lost in transit".
func (a *aauthResource) poll(w http.ResponseWriter, r *http.Request) {
	v, ok := a.verify(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	var p pendingRow
	err := a.s.d.Pool.QueryRow(ctx, `
		SELECT ps_iss, sub, agent_jkt, status, expires_at, delivered_at, result_jti, result_status, result_type, result_body, result_until
		FROM mcp_aauth_pending WHERE id = $1`, id,
	).Scan(&p.ps, &p.sub, &p.jkt, &p.status, &p.expires, &p.delivered, &p.resultJti, &p.resultStatus, &p.resultType, &p.resultBody, &p.resultUntil)
	if db.IsNoRows(err) || (err == nil && p.jkt != v.Thumbprint) {
		w.Header().Set("Cache-Control", "no-store")
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeUnknownPending, "No request is waiting at this address for this agent."))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	now := time.Now()
	switch {
	case p.status == "linked" && p.delivered != nil:
		switch {
		case p.resultStatus == nil: // another poll is running it
			_ = aauth.WritePending(w, aauth.Pending{Location: a.id + "/aauth/pending/" + id, RetryAfter: 1})
		case v.Auth != nil && p.resultJti != nil && *p.resultJti == v.Auth.Claims.Jti && p.resultUntil != nil && now.Before(*p.resultUntil):
			writeResult(w, *p.resultStatus, p.resultType, p.resultBody)
		default:
			aauth.WriteGone(w)
		}
	case p.delivered != nil:
		aauth.WriteGone(w)
	case p.status == "failed":
		a.finish(ctx, id)
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidCode, "Too many wrong codes were entered for this request."))
	case p.status == "denied":
		a.finish(ctx, id)
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeDenied, "The person declined to link this account."))
	case !now.Before(p.expires):
		a.finish(ctx, id)
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeExpired, "Nobody linked this account in time."))
	case p.status == "linked":
		a.release(w, r, v, id, p)
	default:
		status := aauth.PendingStatusPending
		if p.status == "interacting" {
			status = aauth.PendingStatusInteracting
		}
		_ = aauth.WritePending(w, aauth.Pending{Location: a.id + "/aauth/pending/" + id, RetryAfter: aauthRetryAfter, Status: status})
	}
}

// finish marks a terminal answer given, so later polls get 410.
func (a *aauthResource) finish(ctx context.Context, id string) {
	if _, err := a.s.d.Pool.Exec(ctx, `UPDATE mcp_aauth_pending SET delivered_at = now(), held_body = '' WHERE id = $1 AND delivered_at IS NULL`, id); err != nil {
		slog.ErrorContext(ctx, "mcp: aauth pending", "err", err)
	}
}

// release runs a held call once its identity is linked, as the linked account
// with the poll's auth token, and keeps the answer for that token until it
// expires. A poll with a person token is told to bring an auth token.
func (a *aauthResource) release(w http.ResponseWriter, r *http.Request, v *aauth.VerifiedRequest, id string, p pendingRow) {
	ctx := r.Context()
	if v.Auth == nil || v.Auth.Claims.PS != p.ps || v.Auth.Claims.Sub != p.sub {
		if v.Person != nil && v.Person.Claims.Iss == p.ps && v.Person.Claims.Sub == p.sub {
			a.challenge(w, r, v, ScopeRead)
			return
		}
		a.requirePersonToken(w, r)
		return
	}
	c := v.Auth.Claims
	scopes := tokenScopes(c.Scope)
	if !slices.Contains(scopes, ScopeRead) {
		a.challenge(w, r, v, ScopeRead)
		return
	}
	// Claim the run, so two polls can't both run it.
	var held pendingRow
	var headers []byte
	err := a.s.d.Pool.QueryRow(ctx, `
		UPDATE mcp_aauth_pending SET delivered_at = now(), result_jti = $2, result_until = to_timestamp($3)
		WHERE id = $1 AND status = 'linked' AND delivered_at IS NULL
		RETURNING held_body, held_headers`, id, c.Jti, c.Exp).Scan(&held.heldBody, &headers)
	if db.IsNoRows(err) {
		_ = aauth.WritePending(w, aauth.Pending{Location: a.id + "/aauth/pending/" + id, RetryAfter: 1})
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	_ = json.Unmarshal(headers, &held.heldHeaders)
	rec := &recorder{header: http.Header{}}
	l, err := a.findLink(ctx, p.ps, p.sub)
	switch {
	case err != nil:
		httpx.Fail(rec, r, err)
	case l == nil:
		aauth.WriteProblem(rec, aauth.NewProblem(aauth.CodeDenied, "The link was removed before the call ran."))
	case l.identity.Status != "active":
		httpx.Fail(rec, r, errInactive)
	default:
		if err := a.touch(ctx, l.id, v.Thumbprint); err != nil {
			slog.ErrorContext(ctx, "mcp: aauth touch", "err", err)
		}
		call := r.Clone(ctx)
		call.Method = http.MethodPost
		call.URL = &url.URL{Path: "/mcp"}
		call.Header = http.Header{}
		for k, val := range held.heldHeaders {
			call.Header.Set(k, val)
		}
		call.Body = http.NoBody
		if len(held.heldBody) > 0 {
			call.Body = io.NopCloser(bytes.NewReader(held.heldBody))
		}
		call.ContentLength = int64(len(held.heldBody))
		a.s.serveAuthenticated(rec, call, l.principal(scopes, v), a.s.inner)
	}
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	ctype := rec.header.Get("Content-Type")
	if _, err := a.s.d.Pool.Exec(ctx, `
		UPDATE mcp_aauth_pending SET result_status = $2, result_type = $3, result_body = $4, held_body = '' WHERE id = $1`,
		id, status, ctype, rec.body.Bytes()); err != nil {
		slog.ErrorContext(ctx, "mcp: aauth keep result", "err", err)
	}
	for _, k := range []string{aauth.HeaderAAuthRequirement, "WWW-Authenticate", "Retry-After"} {
		if val := rec.header.Get(k); val != "" {
			w.Header().Set(k, val)
		}
	}
	writeResult(w, status, &ctype, rec.body.Bytes())
}

func writeResult(w http.ResponseWriter, status int, ctype *string, body []byte) {
	w.Header().Set("Cache-Control", "no-store")
	if ctype != nil && *ctype != "" {
		w.Header().Set("Content-Type", *ctype)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// recorder captures the answer to a held call.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(b)
}
func (r *recorder) Flush() {}

// ---- authorization and revocation endpoints --------------------------------

// authorize is POST /aauth/authorize: an agent asks for scope before calling,
// presenting a person token, and gets a resource token. "A request without
// one is answered per requirement=person-token."
func (a *aauthResource) authorize(w http.ResponseWriter, r *http.Request) {
	v, ok := a.verify(w, r)
	if !ok {
		return
	}
	if v.Person == nil {
		a.setRequirement(w, aauth.Requirement{Value: aauth.RequirementPersonToken})
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var q aauth.AuthorizationRequest
	if err := json.Unmarshal(v.Body, &q); err != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidRequest, "The body must be a JSON object."))
		return
	}
	if q.Account != "" {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidAccount, "There are no accounts to choose from here: the person token says whose account it is."))
		return
	}
	if q.R3Operations != nil || q.Budget != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidRequest, "Ask for scope: read, or read write."))
		return
	}
	fields := strings.Fields(q.Scope)
	if len(fields) == 0 {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidRequest, "scope is required: read, or read write."))
		return
	}
	write := false
	for _, f := range fields {
		switch f {
		case ScopeRead:
		case ScopeWrite:
			write = true
		default:
			aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidScope, "This resource defines read and write."))
			return
		}
	}
	ctx := r.Context()
	c := v.Person.Claims
	l, err := a.findLink(ctx, c.Iss, c.Sub)
	if err != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeServerError, ""))
		return
	}
	token, err := a.resourceToken(ctx, v, a.scopeFor(ctx, write, l == nil, c.Iss))
	if err != nil {
		slog.ErrorContext(ctx, "mcp: aauth resource token", "err", err)
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeServerError, ""))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, aauth.ResourceTokenResponse{ResourceToken: token})
}

// revoke is POST /aauth/revoke: a person server withdraws a person or auth
// token it issued for this resource, signing as itself. Kept by (iss, jti),
// "where iss is the verified identity of the caller", until the token's exp;
// nothing is downstream, so it answers at once. Only person servers this
// resource has dealt with may revoke, and each may hold a bounded number.
func (a *aauthResource) revoke(w http.ResponseWriter, r *http.Request) {
	v, err := a.servers.Verify(r.Context(), r)
	if err != nil {
		aauth.WriteVerificationError(w, err)
		return
	}
	ctx := r.Context()
	iss := v.Server.ID
	var known bool
	if err := a.s.d.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM mcp_aauth_links WHERE ps_iss = $1) OR EXISTS (SELECT 1 FROM mcp_aauth_pending WHERE ps_iss = $1)`,
		iss).Scan(&known); err != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeServerError, ""))
		return
	}
	if !known {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeUnsupportedIss, "This resource holds no tokens from you."))
		return
	}
	var q aauth.RevocationRequest
	if err := json.Unmarshal(v.Body, &q); err != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidRequest, "The body must be a JSON object."))
		return
	}
	if err := q.Validate(); err != nil {
		var p *aauth.Problem
		if errors.As(err, &p) {
			aauth.WriteProblem(w, p)
			return
		}
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidRequest, err.Error()))
		return
	}
	exp := time.Unix(q.Exp, 0)
	// "MAY reject a revocation whose exp is further in the future than the
	// longest lifetime it accepts for any token."
	if len(q.Jti) > 256 || exp.After(time.Now().Add(aauth.MaxAgentTokenLifetime+time.Hour)) {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeInvalidRequest, "jti is too long, or exp is later than any token here lives."))
		return
	}
	var held int
	if err := a.s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM mcp_aauth_revocations WHERE iss = $1 AND exp > now()`, iss).Scan(&held); err != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeServerError, ""))
		return
	}
	if held >= 100_000 {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeRateLimited, "Too many revocations held for you; try again once some expire.").WithRetryAfter(60))
		return
	}
	if _, err := a.s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_aauth_revocations (iss, jti, exp) VALUES ($1, $2, $3)
		ON CONFLICT (iss, jti) DO UPDATE SET exp = greatest(mcp_aauth_revocations.exp, EXCLUDED.exp)`, iss, q.Jti, exp); err != nil {
		aauth.WriteProblem(w, aauth.NewProblem(aauth.CodeServerError, ""))
		return
	}
	w.WriteHeader(http.StatusOK)
}

// revoked answers revoked_jwt for a person or auth token its issuer withdrew.
func (a *aauthResource) revoked(ctx context.Context, iss, jti string) (bool, error) {
	var yes bool
	err := a.s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM mcp_aauth_revocations WHERE iss = $1 AND jti = $2)`, iss, jti).Scan(&yes)
	return yes, err
}

// prune drops what nothing can use again: requests a day past their end,
// held calls and kept answers once done with, revocations past their exp,
// and the private parts of retired keys (rotating the key monthly).
func (a *aauthResource) prune(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM mcp_aauth_pending WHERE expires_at < now() - interval '1 day' AND (result_until IS NULL OR result_until < now())`,
		`UPDATE mcp_aauth_pending SET held_body = '' WHERE held_body <> '' AND expires_at < now()`,
		`UPDATE mcp_aauth_pending SET result_body = NULL WHERE result_body IS NOT NULL AND result_until < now()`,
		`DELETE FROM mcp_aauth_revocations WHERE exp < now() - interval '10 minutes'`,
	} {
		if _, err := a.s.d.Pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return a.keys.Maintain(ctx, aauthKeyMaxAge)
}
