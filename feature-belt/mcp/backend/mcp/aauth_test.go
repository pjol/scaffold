package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/aauth"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/legal"
)

// AAuth end to end, over real signed HTTP against a migrated database: an
// agent provider and a person server built on the same protocol library
// (internal/aauth) on loopback, an agent signing every request, and this
// resource deciding each one. Covers the challenges by token type, the
// resource token as a person server verifies it, linking (codes, email
// check, deny, wrong codes, one account per identity), the held call and its
// kept answer, step-up to write, a person token never standing in for an auth
// token, revocation, the authorization endpoint and unlinking.
func TestAAuthEndToEnd(t *testing.T) {
	w := newAAuthWorld(t)
	ctx := context.Background()

	// Metadata: a valid resource document whose issuer is this server.
	var meta aauth.ResourceMetadata
	w.getJSON("/.well-known/aauth-resource.json", &meta)
	if err := meta.Validate(w.policy); err != nil || meta.Issuer != w.chordID || meta.AccessMode != aauth.AccessModeAuthToken {
		t.Fatalf("metadata %+v: %v", meta, err)
	}
	var jwks aauth.JWKS
	w.getJSON("/aauth/jwks.json", &jwks)
	if len(jwks.Keys) == 0 || jwks.Keys[0].Alg != string(aauth.AlgEd25519) {
		t.Fatalf("jwks %+v", jwks)
	}

	// Nothing presented: a bearer challenge and a person-token requirement.
	resp := w.unsigned(initializeBody)
	w.expect(resp, http.StatusUnauthorized, aauth.RequirementPersonToken)
	if !strings.Contains(resp.Header.Get("WWW-Authenticate"), "Bearer") || !strings.Contains(resp.Header.Get("Link"), "aauth-resource") {
		t.Fatalf("unsigned headers %v", resp.Header)
	}

	// An agent token says which agent, not whom it acts for.
	w.expect(w.call(w.agentToken, initializeBody), http.StatusUnauthorized, aauth.RequirementPersonToken)

	// A person token earns a resource token, for read and (unlinked, and the
	// person server offers it) email, that the person server's own checks accept.
	person := w.personToken("person-1")
	rt := w.resourceToken(w.call(person, initializeBody))
	got := w.verifyAtPS(rt, person)
	if got.Claims.Scope != "read email" || got.Claims.Sub != "person-1" || got.Claims.Aud != w.ps.iss || got.Claims.Iss != w.chordID {
		t.Fatalf("resource token claims %+v", got.Claims)
	}

	// The auth token from an unlinked identity: the call is held, and the
	// person is sent to the link page with a code.
	authTok, authJti := w.authToken("person-1", "read email", "ada@chord.test")
	resp = w.call(authTok, initializeBody)
	req := w.expect(resp, http.StatusAccepted, aauth.RequirementInteraction)
	if req.URL != "http://localhost:3999/connect/aauth" || req.Code == "" {
		t.Fatalf("interaction %+v", req)
	}
	pending := resp.Header.Get("Location")
	if !strings.HasPrefix(pending, w.chordURL+"/aauth/pending/") || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("pending headers %v", resp.Header)
	}
	if status := w.pollStatus(pending, authTok); status != "pending" {
		t.Fatalf("poll before the person arrived: %s", status)
	}

	// The code is single use and binds the request to whoever claims it.
	var view AAuthInteraction
	w.user(w.ada, http.MethodPost, "/v1/mcp/aauth/interactions", map[string]string{"code": strings.ToLower(req.Code)}, http.StatusOK, &view)
	if view.EmailMatch != "match" || view.PersonServer.Name != "Test PS" || view.PersonServer.Issuer != w.ps.iss {
		t.Fatalf("view %+v", view)
	}
	w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions", map[string]string{"code": req.Code}, http.StatusGone, nil)
	w.user(w.bob, http.MethodGet, "/v1/mcp/aauth/interactions/"+view.ID, nil, http.StatusNotFound, nil)
	if status := w.pollStatus(pending, authTok); status != "interacting" {
		t.Fatalf("poll while the person decides: %s", status)
	}
	w.user(w.ada, http.MethodPost, "/v1/mcp/aauth/interactions/"+view.ID+"/decision", map[string]bool{"allow": true}, http.StatusOK, nil)

	// The first poll runs the held call; the same auth token gets the same
	// answer again, another gets 410.
	first := w.poll(pending, authTok)
	body1 := readBody(t, first)
	if first.StatusCode != http.StatusOK || !strings.Contains(body1, `"serverInfo"`) {
		t.Fatalf("held initialize: %d %s", first.StatusCode, body1)
	}
	again := w.poll(pending, authTok)
	if body2 := readBody(t, again); again.StatusCode != http.StatusOK || body2 != body1 {
		t.Fatalf("repeat poll: %d %s", again.StatusCode, body2)
	}
	other, _ := w.authToken("person-1", "read", "")
	if resp := w.poll(pending, other); resp.StatusCode != http.StatusGone {
		t.Fatalf("other token's poll: %d", resp.StatusCode)
	}

	// Linked: calls run as Ada, as "Agents via Test PS", with read tools only.
	list := readBody(t, w.callOK(authTok, toolsListBody))
	if !strings.Contains(list, "test_read") || strings.Contains(list, "test_write") {
		t.Fatalf("read-only tools/list: %s", list)
	}
	out := readBody(t, w.callOK(authTok, callBody("test_read")))
	if !strings.Contains(out, w.ada.String()) || !strings.Contains(out, "Agents via Test PS") {
		t.Fatalf("test_read ran as %s", out)
	}

	// Step-up: a write tool on a read token gets a resource token for read
	// and write, naming the auth token it carried. No email once linked.
	rt = w.resourceToken(w.call(authTok, callBody("test_write")))
	got = w.verifyAtPS(rt, authTok)
	if got.Claims.Scope != "read write" || got.Claims.PresentedJti != authJti {
		t.Fatalf("step-up resource token %+v", got.Claims)
	}
	if w.tools.writes() != 0 {
		t.Fatal("the write tool ran on a read token")
	}
	writeTok, _ := w.authToken("person-1", "read write", "")
	if out := readBody(t, w.callOK(writeTok, callBody("test_write"))); !strings.Contains(out, "written") || w.tools.writes() != 1 {
		t.Fatalf("test_write with write scope: %s", out)
	}

	// "A recipient MUST reject an aa-person+jwt wherever an auth token is
	// required": a linked person token still earns only a resource token.
	w.expect(w.call(person, callBody("test_read")), http.StatusUnauthorized, aauth.RequirementAuthToken)

	// The body is covered: a tampered body fails the signature.
	tampered := w.signed(http.MethodPost, "/mcp", authTok, []byte(callBody("test_read")))
	tampered.Body = io.NopCloser(strings.NewReader(callBody("test_write")))
	tampered.ContentLength = int64(len(callBody("test_write")))
	if resp := w.send(tampered); resp.StatusCode != http.StatusUnauthorized || resp.Header.Get(aauth.HeaderSignatureError) == "" {
		t.Fatalf("tampered body: %d %v", resp.StatusCode, resp.Header)
	}

	// An auth token from an access server: there is none here.
	asTok := w.accessServerToken("person-1")
	w.expect(w.call(asTok, toolsListBody), http.StatusUnauthorized, aauth.RequirementPersonToken)

	// Revocation by the person server that issued the token.
	if resp := w.revoke(w.ps, authJti); resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %d %s", resp.StatusCode, readBody(t, resp))
	}
	resp = w.call(authTok, toolsListBody)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get(aauth.HeaderSignatureError), "revoked_jwt") {
		t.Fatalf("revoked token: %d %v", resp.StatusCode, resp.Header)
	}
	w.expect(resp, http.StatusUnauthorized, aauth.RequirementPersonToken)
	if resp := w.revoke(w.stranger, "anything"); resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "unsupported_iss") {
		t.Fatalf("stranger's revocation: %d", resp.StatusCode)
	}

	// The authorization endpoint: a person token asks for scope up front.
	var tok aauth.ResourceTokenResponse
	resp = w.send(w.signed(http.MethodPost, "/aauth/authorize", person, []byte(`{"scope":"read write"}`)))
	if err := json.Unmarshal([]byte(readBody(t, resp)), &tok); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("authorize: %d %v", resp.StatusCode, err)
	}
	if got := w.verifyAtPS(tok.ResourceToken, person); got.Claims.Scope != "read write" {
		t.Fatalf("authorize scope %q", got.Claims.Scope)
	}
	if resp := w.send(w.signed(http.MethodPost, "/aauth/authorize", person, []byte(`{"scope":"admin"}`))); resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(readBody(t, resp), "invalid_scope") {
		t.Fatalf("authorize unknown scope: %d", resp.StatusCode)
	}
	if resp := w.send(w.signed(http.MethodPost, "/aauth/authorize", w.agentToken, []byte(`{"scope":"read"}`))); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("authorize with an agent token: %d", resp.StatusCode)
	}

	// Ada's list, and unlinking: the identity's calls are held again.
	var links struct{ Items []AAuthLink }
	w.user(w.ada, http.MethodGet, "/v1/mcp/aauth/links", nil, http.StatusOK, &links)
	if len(links.Items) != 1 || links.Items[0].Agents != 1 || links.Items[0].PersonServer.Name != "Test PS" ||
		links.Items[0].Email == nil || *links.Items[0].Email != "ada@chord.test" {
		t.Fatalf("links %+v", links.Items)
	}
	w.user(w.ada, http.MethodDelete, "/v1/mcp/aauth/links/"+links.Items[0].ID.String(), nil, http.StatusNoContent, nil)
	w.expect(w.call(writeTok, toolsListBody), http.StatusAccepted, aauth.RequirementInteraction)

	// Another identity: Bob claims it, the asserted email isn't his, and he
	// declines: the poll says denied, then 410, and the callback gets the error.
	carolTok, _ := w.authToken("person-2", "read", "carol@elsewhere.test")
	resp = w.call(carolTok, initializeBody)
	req = w.expect(resp, http.StatusAccepted, aauth.RequirementInteraction)
	pending = resp.Header.Get("Location")
	w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions",
		map[string]string{"code": req.Code, "callback": "https://agent.example/back?x=1"}, http.StatusOK, &view)
	if view.EmailMatch != "mismatch" || view.ReturnHost == nil || *view.ReturnHost != "agent.example" {
		t.Fatalf("mismatch view %+v", view)
	}
	var decision struct {
		Status      string  `json:"status"`
		RedirectURL *string `json:"redirect_url"`
	}
	w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions/"+view.ID+"/decision", map[string]bool{"allow": false}, http.StatusOK, &decision)
	if decision.Status != "denied" || decision.RedirectURL == nil || !strings.Contains(*decision.RedirectURL, "error=access_denied") {
		t.Fatalf("deny %+v", decision)
	}
	if resp := w.poll(pending, carolTok); resp.StatusCode != http.StatusForbidden || !strings.Contains(readBody(t, resp), "denied") {
		t.Fatalf("poll after deny: %d", resp.StatusCode)
	}
	if resp := w.poll(pending, carolTok); resp.StatusCode != http.StatusGone {
		t.Fatalf("poll after the terminal answer: %d", resp.StatusCode)
	}

	// Wrong codes: five end every open request sharing the code's prefix.
	resp = w.call(carolTok, initializeBody)
	req = w.expect(resp, http.StatusAccepted, aauth.RequirementInteraction)
	pending = resp.Header.Get("Location")
	code, _ := aauth.NormalizeInteractionCode(req.Code)
	for i := 0; i < aauthCodeFailure; i++ {
		wrong := code[:4] + strings.Repeat(string("0123456789"[i]), 4)
		if wrong == code {
			continue
		}
		w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions", map[string]string{"code": wrong}, http.StatusGone, nil)
	}
	if resp := w.poll(pending, carolTok); resp.StatusCode != http.StatusGone || !strings.Contains(readBody(t, resp), "invalid_code") {
		t.Fatalf("poll after wrong codes: %d", resp.StatusCode)
	}
	w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions", map[string]string{"code": req.Code}, http.StatusGone, nil)

	// One account per identity: person-1, linked again by Ada, can't be
	// linked by Bob from a second request.
	resp = w.call(writeTok, initializeBody)
	reqAda := w.expect(resp, http.StatusAccepted, aauth.RequirementInteraction)
	resp = w.call(writeTok, initializeBody)
	reqBob := w.expect(resp, http.StatusAccepted, aauth.RequirementInteraction)
	var viewAda, viewBob AAuthInteraction
	w.user(w.ada, http.MethodPost, "/v1/mcp/aauth/interactions", map[string]string{"code": reqAda.Code}, http.StatusOK, &viewAda)
	w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions", map[string]string{"code": reqBob.Code}, http.StatusOK, &viewBob)
	w.user(w.ada, http.MethodPost, "/v1/mcp/aauth/interactions/"+viewAda.ID+"/decision", map[string]bool{"allow": true}, http.StatusOK, nil)
	w.user(w.bob, http.MethodPost, "/v1/mcp/aauth/interactions/"+viewBob.ID+"/decision", map[string]bool{"allow": true}, http.StatusConflict, nil)

	// Staff see links across people; account deletion removes them.
	var all struct{ Items []AdminAAuthLink }
	w.user(w.ada, http.MethodGet, "/v1/admin/mcp/aauth/links", nil, http.StatusOK, &all)
	if len(all.Items) != 2 {
		t.Fatalf("admin links: %d", len(all.Items))
	}
	if err := db.WithTx(ctx, w.pool, func(tx pgx.Tx) error { return w.svc.DeleteAccount(ctx, tx, w.ada) }); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := w.pool.QueryRow(ctx, `SELECT count(*) FROM mcp_aauth_links WHERE user_id = $1`, w.ada).Scan(&left); err != nil || left != 0 {
		t.Fatalf("links after deletion: %d %v", left, err)
	}
	if err := w.svc.prune(ctx); err != nil {
		t.Fatal(err)
	}
}

// ---- the world -------------------------------------------------------------

const (
	initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"aauth-test","version":"1"}}}`
	toolsListBody  = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
)

func callBody(tool string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, tool)
}

type aauthWorld struct {
	t          *testing.T
	pool       *pgxpool.Pool
	policy     aauth.IdentifierPolicy
	svc        *Service
	tools      *testTools
	chordURL   string
	chordID    string
	ps, ap, as *party
	stranger   *party // a person server this resource has never dealt with
	agentKey   *aauth.PrivateKey
	agentToken string
	ada, bob   uuid.UUID
	identities map[uuid.UUID]*auth.Identity
}

func newAAuthWorld(t *testing.T) *aauthWorld {
	t.Helper()
	w := &aauthWorld{t: t, pool: aauthTestPool(t), policy: aauth.IdentifierPolicy{AllowInsecureLoopback: true},
		identities: map[uuid.UUID]*auth.Identity{}}
	w.ps = newParty(t, aauth.DwkPerson, func(iss string) any {
		return aauth.PersonServerMetadata{CommonMetadata: aauth.CommonMetadata{Issuer: iss, JWKSURI: iss + "/jwks", Name: "Test PS"},
			AuthTokenEndpoint: iss + "/token", PersonTokenEndpoint: iss + "/person", ScopesSupported: []string{"openid", "email"}}
	})
	w.stranger = newParty(t, aauth.DwkPerson, func(iss string) any {
		return aauth.PersonServerMetadata{CommonMetadata: aauth.CommonMetadata{Issuer: iss, JWKSURI: iss + "/jwks"},
			AuthTokenEndpoint: iss + "/token", PersonTokenEndpoint: iss + "/person"}
	})
	w.ap = newParty(t, aauth.DwkAgent, func(iss string) any {
		return aauth.AgentProviderMetadata{CommonMetadata: aauth.CommonMetadata{Issuer: iss, JWKSURI: iss + "/jwks"}}
	})
	w.as = newParty(t, aauth.DwkAccess, func(iss string) any {
		return aauth.AccessServerMetadata{CommonMetadata: aauth.CommonMetadata{Issuer: iss, JWKSURI: iss + "/jwks"}, AuthTokenEndpoint: iss + "/token"}
	})

	chord := httptest.NewUnstartedServer(nil)
	w.chordURL = "http://" + chord.Listener.Addr().String()
	w.chordID = w.chordURL
	d := &app.Deps{Cfg: &config.Config{Env: "development", PublicAPIURL: w.chordURL, PublicWebURL: "http://localhost:3999"}, Pool: w.pool}
	w.svc = New(d)
	if w.svc.aauth == nil {
		t.Fatal("aauth is off")
	}
	w.tools = &testTools{}
	d.Modules = []app.Module{w.svc, w.tools}
	r := chi.NewRouter()
	w.svc.MountRoot(r)
	r.Route("/v1", func(r chi.Router) {
		r.Use(w.testAuth)
		w.svc.MountUser(r)
		r.Route("/admin", w.svc.MountAdmin)
	})
	chord.Config.Handler = r
	chord.Start()
	t.Cleanup(chord.Close)

	ctx := context.Background()
	for _, u := range []struct {
		id    *uuid.UUID
		email string
	}{{&w.ada, "Ada@chord.test"}, {&w.bob, "bob@chord.test"}} {
		*u.id = uuid.New()
		if _, err := w.pool.Exec(ctx, `INSERT INTO users (id, auth_subject, email, display_name, admin_role, terms_version_accepted, privacy_version_accepted)
			VALUES ($1, $2, $3, $3, 'admin', $4, $5)`, *u.id, "dev_"+u.id.String(), u.email, legal.TermsVersion, legal.PrivacyVersion); err != nil {
			t.Fatal(err)
		}
		w.identities[*u.id] = &auth.Identity{UserID: *u.id, Email: u.email, DisplayName: u.email, Status: "active", AdminRole: "admin",
			TermsVersion: legal.TermsVersion, PrivacyVersion: legal.PrivacyVersion}
	}

	var err error
	if w.agentKey, err = aauth.GenerateKey(aauth.AlgEd25519, "agent-1"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	w.agentToken, err = aauth.TokenIssuer{Key: w.ap.key, Policy: w.policy}.Agent(aauth.AgentClaims{
		Claims: aauth.Claims{Iss: w.ap.iss, Jti: aauth.NewJTI(), Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()},
		Sub:    "aauth:tester@127.0.0.1", Cnf: aauth.ConfirmKey(w.agentKey.PublicJWK()), PS: w.ps.iss,
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// testAuth stands in for the core auth middleware: X-Test-User names the caller.
func (w *aauthWorld) testAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(r.Header.Get("X-Test-User"))
		if err != nil || w.identities[id] == nil {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(rw, r.WithContext(auth.WithIdentity(r.Context(), w.identities[id])))
	})
}

func (w *aauthWorld) personToken(sub string) string {
	w.t.Helper()
	now := time.Now()
	tok, err := aauth.TokenIssuer{Key: w.ps.key, Policy: w.policy}.Person(aauth.PersonClaims{
		Claims: aauth.Claims{Iss: w.ps.iss, Jti: aauth.NewJTI(), Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()},
		Aud:    w.chordID, Sub: sub, Cnf: aauth.ConfirmKey(w.agentKey.PublicJWK()),
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return tok
}

// authToken is what the person server issues for a resource token: email
// claims when it asserts a verified email.
func (w *aauthWorld) authToken(sub, scope, email string) (string, string) {
	w.t.Helper()
	now := time.Now()
	c := aauth.AuthClaims{
		Claims: aauth.Claims{Iss: w.ps.iss, Dwk: aauth.DwkPerson, Jti: aauth.NewJTI(), Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()},
		Aud:    w.chordID, PS: w.ps.iss, Sub: sub, Cnf: aauth.ConfirmKey(w.agentKey.PublicJWK()), Scope: scope,
	}
	if email != "" {
		c.Extra = map[string]json.RawMessage{"email": json.RawMessage(fmt.Sprintf("%q", email)), "email_verified": json.RawMessage("true")}
	}
	tok, err := aauth.TokenIssuer{Key: w.ps.key, Policy: w.policy}.Auth(c)
	if err != nil {
		w.t.Fatal(err)
	}
	return tok, c.Jti
}

func (w *aauthWorld) accessServerToken(sub string) string {
	w.t.Helper()
	now := time.Now()
	tok, err := aauth.TokenIssuer{Key: w.as.key, Policy: w.policy}.Auth(aauth.AuthClaims{
		Claims: aauth.Claims{Iss: w.as.iss, Dwk: aauth.DwkAccess, Jti: aauth.NewJTI(), Iat: now.Unix(), Exp: now.Add(time.Hour).Unix()},
		Aud:    w.chordID, PS: w.ps.iss, Sub: sub, Cnf: aauth.ConfirmKey(w.agentKey.PublicJWK()), Scope: "read write",
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return tok
}

// verifyAtPS runs the person server's checks on a resource token
// (Resource Token Verification steps 1 to 3), with the token presented.
func (w *aauthWorld) verifyAtPS(resourceToken, presented string) *aauth.ResourceToken {
	w.t.Helper()
	v := &aauth.TokenVerifier{Keys: aauth.NewDiscovery(aauth.DiscoveryConfig{Policy: w.policy}), Policy: w.policy}
	thumb, _ := w.agentKey.PublicJWK().Thumbprint()
	rt, _, err := v.VerifyTokenRequest(context.Background(), aauth.TokenRequestInput{
		ResourceToken: resourceToken, PresentedToken: presented, Audience: w.ps.iss, ExpectedPS: w.ps.iss, SigningThumbprint: thumb,
	})
	if err != nil {
		w.t.Fatalf("person server refused the resource token: %v", err)
	}
	return rt
}

func (w *aauthWorld) signed(method, path, token string, body []byte) *http.Request {
	w.t.Helper()
	r, err := http.NewRequest(method, w.chordURL+path, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	}
	if err := aauth.NewAgentSigner(token, w.agentKey, nil).SignRequest(r, body); err != nil {
		w.t.Fatal(err)
	}
	return r
}

func (w *aauthWorld) send(r *http.Request) *http.Response {
	w.t.Helper()
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (w *aauthWorld) call(token, body string) *http.Response {
	return w.send(w.signed(http.MethodPost, "/mcp", token, []byte(body)))
}

func (w *aauthWorld) callOK(token, body string) *http.Response {
	w.t.Helper()
	resp := w.call(token, body)
	if resp.StatusCode != http.StatusOK {
		w.t.Fatalf("%s: %d %v %s", body, resp.StatusCode, resp.Header, readBody(w.t, resp))
	}
	return resp
}

func (w *aauthWorld) unsigned(body string) *http.Response {
	r, _ := http.NewRequest(http.MethodPost, w.chordURL+"/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return w.send(r)
}

func (w *aauthWorld) poll(location, token string) *http.Response {
	return w.send(w.signed(http.MethodGet, strings.TrimPrefix(location, w.chordURL), token, nil))
}

func (w *aauthWorld) pollStatus(location, token string) string {
	w.t.Helper()
	resp := w.poll(location, token)
	var b struct{ Status string }
	if err := json.Unmarshal([]byte(readBody(w.t, resp)), &b); err != nil || resp.StatusCode != http.StatusAccepted {
		w.t.Fatalf("poll: %d %v", resp.StatusCode, err)
	}
	return b.Status
}

// expect checks the status and AAuth-Requirement value and returns the
// requirement.
func (w *aauthWorld) expect(resp *http.Response, status int, requirement string) aauth.Requirement {
	w.t.Helper()
	q, err := aauth.ParseRequirement(resp.Header.Values(aauth.HeaderAAuthRequirement))
	if resp.StatusCode != status || err != nil || q.Value != requirement {
		w.t.Fatalf("want %d %s, got %d %q (%v): %s", status, requirement, resp.StatusCode,
			resp.Header.Get(aauth.HeaderAAuthRequirement), err, readBody(w.t, resp))
	}
	return q
}

func (w *aauthWorld) resourceToken(resp *http.Response) string {
	w.t.Helper()
	q := w.expect(resp, http.StatusUnauthorized, aauth.RequirementAuthToken)
	if q.ResourceToken == "" {
		w.t.Fatal("no resource token")
	}
	return q.ResourceToken
}

func (w *aauthWorld) revoke(from *party, jti string) *http.Response {
	w.t.Helper()
	body, _ := json.Marshal(aauth.RevocationRequest{Jti: jti, Exp: time.Now().Add(time.Hour).Unix()})
	r, _ := http.NewRequest(http.MethodPost, w.chordURL+"/aauth/revoke", nil)
	r.Header.Set("Content-Type", "application/json")
	if err := aauth.NewServerSigner(from.iss, aauth.DwkPerson, from.key, nil).SignRequest(r, body); err != nil {
		w.t.Fatal(err)
	}
	return w.send(r)
}

func (w *aauthWorld) getJSON(path string, v any) {
	w.t.Helper()
	resp, err := http.Get(w.chordURL + path)
	if err != nil {
		w.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(v) != nil {
		w.t.Fatalf("GET %s: %d", path, resp.StatusCode)
	}
}

// user calls a /v1 route as a person and decodes the answer into out.
func (w *aauthWorld) user(as uuid.UUID, method, path string, in any, status int, out any) {
	w.t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, w.chordURL+path, body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Test-User", as.String())
	resp := w.send(r)
	b := readBody(w.t, resp)
	if resp.StatusCode != status {
		w.t.Fatalf("%s %s: want %d, got %d: %s", method, path, status, resp.StatusCode, b)
	}
	if out != nil {
		if err := json.Unmarshal([]byte(b), out); err != nil {
			w.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	return string(b)
}

// party is an agent provider, person server or access server on loopback:
// its metadata document, and a JWKS with its one key.
type party struct {
	iss string
	key *aauth.PrivateKey
}

func newParty(t *testing.T, dwk string, doc func(iss string) any) *party {
	t.Helper()
	key, err := aauth.GenerateKey(aauth.AlgEd25519, "k1")
	if err != nil {
		t.Fatal(err)
	}
	p := &party{key: key}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p.iss = srv.URL
	mux.HandleFunc("GET /.well-known/"+dwk, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc(p.iss))
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(aauth.JWKS{Keys: []aauth.JWK{key.PublicJWK()}})
	})
	return p
}

// testTools is a module with one read tool and one write tool, both saying
// whom they ran as.
type testTools struct {
	mu sync.Mutex
	n  int
}

func (*testTools) Name() string { return "testtools" }

func (m *testTools) writes() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n
}

func (m *testTools) Tools() []app.Tool {
	who := func(ctx context.Context, caller *auth.Identity) map[string]any {
		a, _ := auth.AgentFrom(ctx)
		return map[string]any{"user_id": caller.UserID, "client": a.Client}
	}
	schema := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	return []app.Tool{
		{Name: "test_read", Description: "Read.", InputSchema: schema, ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) { return who(ctx, caller), nil }},
		{Name: "test_write", Description: "Write.", InputSchema: schema,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				m.mu.Lock()
				m.n++
				m.mu.Unlock()
				out := who(ctx, caller)
				out["written"] = true
				return out, nil
			}},
	}
}

// aauthTestPool gives a test its own migrated database on TEST_DATABASE_URL
// (a role that may CREATE DATABASE), dropped on cleanup; unset, the test is
// skipped.
func aauthTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	ctx := context.Background()
	var b [6]byte
	_, _ = rand.Read(b[:])
	name := fmt.Sprintf("mcp_aauth_test_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	db.UseUTC(cfg)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	if err := db.Migrate(ctx, pool, false); err != nil {
		t.Fatal(err)
	}
	return pool
}
