package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

/*
 * The authorization server for remote MCP clients (MCP authorization spec,
 * 2025-11-25 revision: OAuth 2.1 + RFC 8414, 7591, 7009, 8707, 9728, 9207 and
 * client ID metadata documents).
 *
 *	client ──► GET  /mcp                                    401 + resource_metadata
 *	       ──► GET  /.well-known/oauth-protected-resource/mcp
 *	       ──► GET  /.well-known/oauth-authorization-server
 *	       ──► POST /oauth/register      (or an https client_id: metadata document)
 *	       ──► GET  /oauth/authorize     parks the request, 302 to the web app
 *	                   │
 *	                   ▼  PUBLIC_WEB_URL/connect?request=…   the person signs in and decides
 *	                   ▼  POST /v1/mcp/oauth/decision        (their session) → redirect_url
 *	       ◄── redirect_uri?code=…&state=…&iss=…
 *	       ──► POST /oauth/token         code + PKCE verifier + resource → tokens
 *	       ──► POST /mcp                 Bearer access token, as that person
 *
 * Clerk has no dynamic client registration, so this server is the
 * authorization server and the app's own sign-in authenticates the person on
 * the consent page; nothing of Clerk's is stored. Distilled from OLEARY/crm
 * (handlers_mcp_oauth.go), generalised from "operators only" to every user.
 */

// Every credential an agent holds starts with auth.AgentCredentialPrefix, so
// the core auth middleware refuses it on user routes (concrete/auth.md →
// Agent credentials). The client id is an identifier, not a credential.
const (
	accessPrefix       = auth.AgentCredentialPrefix + "scaffoldapp_at_"
	refreshPrefix      = auth.AgentCredentialPrefix + "scaffoldapp_rt_"
	clientSecretPrefix = auth.AgentCredentialPrefix + "scaffoldapp_cs_"
	clientIDPrefix     = "scaffoldapp_client_"

	requestTTL = 10 * time.Minute // a person walking through sign-in and consent
	codeTTL    = 5 * time.Minute

	maxRedirectURIs = 10
	maxURILength    = 2000
)

// oauthConfig holds the settings read in New.
type oauthConfig struct {
	accessTTL            time.Duration
	refreshTTL           time.Duration
	registrationsPerHour int
	maxUnusedClients     int
	metadataDocuments    bool
}

// ---- small helpers ---------------------------------------------------------

// newOpaque returns prefix + 256 random bits, base64url. A crypto/rand
// failure panics: every value this makes is a credential.
func newOpaque(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("mcp: crypto/rand failed: " + err.Error())
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) issuer() string      { return strings.TrimRight(s.d.Cfg.PublicAPIURL, "/") }
func (s *Service) resourceURL() string { return s.issuer() + "/mcp" }
func (s *Service) resourceMetadataURL() string {
	return s.issuer() + "/.well-known/oauth-protected-resource/mcp"
}
func (s *Service) webURL() string { return strings.TrimRight(s.d.Cfg.PublicWebURL, "/") }

// oauthEnabled: the consent page lives on the web app, so OAuth needs its URL.
func (s *Service) oauthEnabled() bool { return s.webURL() != "" }

// canonicalResource normalises a resource indicator (RFC 8707): absolute,
// no fragment, lowercase scheme and host, no trailing slash.
func canonicalResource(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" || u.User != nil || u.RawQuery != "" {
		return "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String(), true
}

// acceptedResource reports whether a canonical resource is this server's
// one protected resource, the MCP endpoint. Tokens are only ever minted for
// it, so a token can't be aimed anywhere else.
func (s *Service) acceptedResource(res string) bool {
	mcp, _ := canonicalResource(s.resourceURL())
	return res != "" && res == mcp
}

// verifyPKCE checks an S256 verifier (RFC 7636: 43–128 unreserved chars).
func verifyPKCE(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || !pkceChars(verifier) || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func pkceChars(v string) bool {
	for _, r := range v {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_' || r == '~') {
			return false
		}
	}
	return true
}

// parseScopes reads a space-delimited scope string. read is implied (and the
// default); write adds tools that change data. OIDC and offline_access
// values clients add by habit are ignored: refresh tokens are always issued.
func parseScopes(raw string) ([]string, error) {
	write := false
	for _, sc := range strings.Fields(raw) {
		switch sc {
		case ScopeRead, "openid", "profile", "email", "offline_access":
		case ScopeWrite:
			write = true
		default:
			return nil, fmt.Errorf("unknown scope %q", sc)
		}
	}
	if write {
		return []string{ScopeRead, ScopeWrite}, nil
	}
	return []string{ScopeRead}, nil
}

// cleanName makes a self-declared client name safe to show: no control or
// format characters, single spaces, at most 80 characters.
func cleanName(in string) string {
	in = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, in)
	name := strings.Join(strings.Fields(in), " ")
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	return name
}

// ---- redirect URIs ---------------------------------------------------------

var blockedSchemes = map[string]bool{
	"javascript": true, "data": true, "file": true, "vbscript": true, "about": true, "blob": true,
	"ftp": true, "ws": true, "wss": true, "mailto": true, "tel": true, "sms": true,
}

func isLoopbackHost(h string) bool { return h == "localhost" || h == "127.0.0.1" || h == "::1" }

// redirectKind classifies a redirect URI the way OAuth 2.1 allows them:
// "web" (https), "loopback" (http on this machine, for desktop clients) or
// "app" (a private-use scheme a native app claims, cursor://…). Anything
// else is refused: an http redirect to another host hands the code over in
// clear, and script/data schemes are injection.
func redirectKind(raw string) (string, bool) {
	if raw == "" || len(raw) > maxURILength || strings.ContainsAny(raw, " \t\r\n") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil || u.Scheme == "" {
		return "", false
	}
	switch scheme := strings.ToLower(u.Scheme); {
	case scheme == "https":
		return "web", u.Host != ""
	case scheme == "http":
		return "loopback", isLoopbackHost(u.Hostname())
	case blockedSchemes[scheme] || !validScheme(scheme):
		return "", false
	default:
		return "app", u.Host != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/")
	}
}

func validScheme(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

// matchRedirect finds the registered URI a request's redirect_uri satisfies:
// an exact string match, or for loopback URIs the same scheme, host, path and
// query on any port (RFC 8252 §7.3: native clients bind a free port). An
// empty redirect_uri is allowed when exactly one is registered.
func matchRedirect(registered []string, got string) (string, bool) {
	if got == "" {
		if len(registered) == 1 {
			return registered[0], true
		}
		return "", false
	}
	if slices.Contains(registered, got) {
		return got, true
	}
	g, err := url.Parse(got)
	if err != nil || g.Scheme != "http" || !isLoopbackHost(g.Hostname()) || g.Fragment != "" || g.User != nil {
		return "", false
	}
	for _, reg := range registered {
		r, err := url.Parse(reg)
		if err != nil || r.Scheme != "http" || !isLoopbackHost(r.Hostname()) {
			continue
		}
		if r.Hostname() == g.Hostname() && r.EscapedPath() == g.EscapedPath() && r.RawQuery == g.RawQuery {
			return got, true
		}
	}
	return "", false
}

// redirectHost is what the consent page names as the destination.
func redirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return u.Scheme + "://" + u.Host
	}
	return u.Host
}

// ---- responses -------------------------------------------------------------

// oauthErr is an RFC 6749 error, answered in the shape clients parse.
type oauthErr struct {
	status int
	code   string
	desc   string
}

func (e *oauthErr) Error() string { return e.code + ": " + e.desc }

func oerr(status int, code, desc string) *oauthErr { return &oauthErr{status, code, desc} }

func writeOAuthError(w http.ResponseWriter, e *oauthErr) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if e.status == http.StatusUnauthorized && e.code == "invalid_client" {
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
	}
	w.WriteHeader(e.status)
	body := map[string]string{"error": e.code}
	if e.desc != "" {
		body["error_description"] = e.desc
	}
	_ = json.NewEncoder(w).Encode(body)
}

func writeOAuthJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	httpx.JSON(w, status, v)
}

func publicJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.OK(w, v)
}

// withParams adds query parameters to a validated redirect URI.
func withParams(redirectURI string, params map[string]string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return redirectURI
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ---- discovery -------------------------------------------------------------

// protectedResource serves RFC 9728 metadata at the path-suffixed location
// and at the root. Both name the MCP endpoint, the only protected resource
// (and the only audience tokens are minted for): clients that probe the root
// get the same answer instead of an origin no token can be used at.
func (s *Service) protectedResource(w http.ResponseWriter, r *http.Request) {
	if !s.oauthEnabled() {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	publicJSON(w, map[string]any{
		"resource":                 s.resourceURL(),
		"authorization_servers":    []string{s.issuer()},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{ScopeRead, ScopeWrite},
		"resource_name":            "Scaffold App",
		"resource_documentation":   s.webURL() + "/settings/connected-apps",
	})
}

// authorizationServer serves RFC 8414 metadata.
func (s *Service) authorizationServer(w http.ResponseWriter, r *http.Request) {
	if !s.oauthEnabled() {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	base := s.issuer()
	methods := []string{"none", "client_secret_post", "client_secret_basic"}
	publicJSON(w, map[string]any{
		"issuer":                                         base,
		"authorization_endpoint":                         base + "/oauth/authorize",
		"token_endpoint":                                 base + "/oauth/token",
		"registration_endpoint":                          base + "/oauth/register",
		"revocation_endpoint":                            base + "/oauth/revoke",
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          methods,
		"revocation_endpoint_auth_methods_supported":     methods,
		"scopes_supported":                               []string{ScopeRead, ScopeWrite},
		"client_id_metadata_document_supported":          s.oauth.metadataDocuments,
		"authorization_response_iss_parameter_supported": true,
		"service_documentation":                          s.webURL() + "/settings/connected-apps",
	})
}

// ---- registration (RFC 7591) -----------------------------------------------

type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// register is open: anybody may register and it grants nothing but a client
// id bound to its redirect URIs. Rate limited per IP; never-used clients are
// pruned after a day and capped in number.
func (s *Service) register(w http.ResponseWriter, r *http.Request) {
	if !s.oauthEnabled() {
		writeOAuthError(w, oerr(http.StatusNotFound, "invalid_request", "OAuth is not enabled on this server."))
		return
	}
	var in registrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_client_metadata", "The registration request must be a JSON object."))
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > maxRedirectURIs {
		writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_redirect_uri", "Send 1 to 10 redirect_uris."))
		return
	}
	for _, u := range in.RedirectURIs {
		if _, ok := redirectKind(u); !ok {
			writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_redirect_uri",
				"Redirect URIs must be https, http on localhost, or an app's own scheme, without a fragment."))
			return
		}
	}
	for _, g := range in.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_client_metadata", "Only authorization_code and refresh_token grants are supported."))
			return
		}
	}
	for _, rt := range in.ResponseTypes {
		if rt != "code" {
			writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_client_metadata", "Only the code response type is supported."))
			return
		}
	}
	method := in.TokenEndpointAuthMethod
	switch method {
	case "":
		method = "none"
	case "none", "client_secret_post", "client_secret_basic":
	default:
		writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_client_metadata",
			"token_endpoint_auth_method must be none, client_secret_post or client_secret_basic."))
		return
	}

	var unused int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM mcp_oauth_clients WHERE last_used_at IS NULL`).Scan(&unused); err != nil {
		slog.ErrorContext(r.Context(), "mcp: count clients", "err", err)
		writeOAuthError(w, oerr(http.StatusInternalServerError, "server_error", ""))
		return
	}
	if unused >= s.oauth.maxUnusedClients {
		w.Header().Set("Retry-After", "3600")
		writeOAuthError(w, oerr(http.StatusServiceUnavailable, "temporarily_unavailable", "Too many pending registrations. Try again later."))
		return
	}

	clientID := newOpaque(clientIDPrefix)[:len(clientIDPrefix)+24]
	var secret string
	var secretHash *string
	if method != "none" {
		secret = newOpaque(clientSecretPrefix)
		h := hashSecret(secret)
		secretHash = &h
	}
	name := cleanName(in.ClientName)
	var issued time.Time
	if err := s.d.Pool.QueryRow(r.Context(), `
		INSERT INTO mcp_oauth_clients (client_id, kind, client_name, redirect_uris, auth_method, secret_hash)
		VALUES ($1, 'registered', $2, $3, $4, $5) RETURNING created_at`,
		clientID, name, in.RedirectURIs, method, secretHash).Scan(&issued); err != nil {
		slog.ErrorContext(r.Context(), "mcp: register client", "err", err)
		writeOAuthError(w, oerr(http.StatusInternalServerError, "server_error", ""))
		return
	}
	slog.InfoContext(r.Context(), "mcp: oauth client registered", "client_id", clientID, "auth_method", method)
	out := map[string]any{
		"client_id":                  clientID,
		"client_id_issued_at":        issued.Unix(),
		"client_name":                name,
		"redirect_uris":              in.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
	}
	if secret != "" {
		out["client_secret"] = secret
		out["client_secret_expires_at"] = 0
	}
	writeOAuthJSON(w, http.StatusCreated, out)
}

// ---- clients ---------------------------------------------------------------

type oauthClient struct {
	ref          uuid.UUID
	clientID     string
	kind         string
	name         string
	redirectURIs []string
	authMethod   string
	secretHash   *string
}

var errUnknownClient = errors.New("unknown client")

const clientCols = `id, client_id, kind, client_name, redirect_uris, auth_method, secret_hash`

func scanClient(row pgx.Row) (*oauthClient, error) {
	c := &oauthClient{}
	err := row.Scan(&c.ref, &c.clientID, &c.kind, &c.name, &c.redirectURIs, &c.authMethod, &c.secretHash)
	if db.IsNoRows(err) {
		return nil, errUnknownClient
	}
	return c, err
}

// findClient loads a client by its client_id. With refresh, an https
// client_id is a metadata document: fetched (or refetched once its cache
// expires) and stored, so the token endpoint later finds the same row.
func (s *Service) findClient(ctx context.Context, clientID string, refresh bool) (*oauthClient, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" || len(clientID) > maxURILength {
		return nil, errUnknownClient
	}
	if !strings.HasPrefix(clientID, "https://") {
		return scanClient(s.d.Pool.QueryRow(ctx, `SELECT `+clientCols+` FROM mcp_oauth_clients
			WHERE client_id = $1 AND kind = 'registered'`, clientID))
	}
	if !s.oauth.metadataDocuments {
		return nil, errUnknownClient
	}
	var fresh bool
	c, err := scanClient(s.d.Pool.QueryRow(ctx, `SELECT `+clientCols+` FROM mcp_oauth_clients
		WHERE client_id = $1 AND kind = 'metadata_document'`, clientID))
	if err == nil {
		err = s.d.Pool.QueryRow(ctx, `SELECT coalesce(metadata_expires_at > now(), false) FROM mcp_oauth_clients WHERE id = $1`, c.ref).Scan(&fresh)
		if err != nil {
			return nil, err
		}
	} else if !errors.Is(err, errUnknownClient) {
		return nil, err
	}
	if fresh || !refresh {
		if c == nil {
			return nil, errUnknownClient
		}
		return c, nil
	}
	doc, ttl, ferr := s.fetchClientMetadata(ctx, clientID)
	if ferr != nil {
		if c != nil {
			// Keep serving the last good copy while the client's host is down.
			slog.WarnContext(ctx, "mcp: client metadata refetch failed; using cached copy", "client_id", clientID, "err", ferr)
			return c, nil
		}
		slog.InfoContext(ctx, "mcp: client metadata refused", "client_id", clientID, "err", ferr)
		return nil, errUnknownClient
	}
	return scanClient(s.d.Pool.QueryRow(ctx, `
		INSERT INTO mcp_oauth_clients (client_id, kind, client_name, redirect_uris, metadata_expires_at)
		VALUES ($1, 'metadata_document', $2, $3, now() + $4::interval)
		ON CONFLICT (client_id) DO UPDATE SET client_name = EXCLUDED.client_name, redirect_uris = EXCLUDED.redirect_uris,
			metadata_expires_at = EXCLUDED.metadata_expires_at, updated_at = now()
		RETURNING `+clientCols, clientID, doc.name, doc.redirectURIs, ttl.String()))
}

// authenticateClient identifies the client at the token and revocation
// endpoints: client_secret_basic, client_secret_post, or a public client's
// bare client_id. A confidential client must present its secret.
func (s *Service) authenticateClient(r *http.Request) (*oauthClient, *oauthErr) {
	id, secret, basic := r.BasicAuth()
	if basic {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	c, err := s.findClient(r.Context(), id, false)
	if err != nil {
		if !errors.Is(err, errUnknownClient) {
			slog.ErrorContext(r.Context(), "mcp: find client", "err", err)
			return nil, oerr(http.StatusInternalServerError, "server_error", "")
		}
		return nil, oerr(http.StatusUnauthorized, "invalid_client", "Unknown client. Register again.")
	}
	if c.authMethod != "none" {
		if secret == "" || c.secretHash == nil ||
			subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(*c.secretHash)) != 1 {
			return nil, oerr(http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		}
	}
	return c, nil
}

// ---- authorization ---------------------------------------------------------

// browserError handles a request whose client or redirect URI can't be
// trusted: sending it back to an unverified URI is the open redirect this
// endpoint exists to avoid, so the person sees the web app's error page.
func (s *Service) browserError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, s.webURL()+"/connect?error="+url.QueryEscape(code), http.StatusFound)
}

// authorize validates the request, parks it, and sends the browser to the
// consent page. It authenticates nobody.
func (s *Service) authorize(w http.ResponseWriter, r *http.Request) {
	if !s.oauthEnabled() {
		writeOAuthError(w, oerr(http.StatusNotFound, "invalid_request", "OAuth is not enabled on this server."))
		return
	}
	q := r.URL.Query()
	client, err := s.findClient(r.Context(), q.Get("client_id"), true)
	if err != nil {
		if !errors.Is(err, errUnknownClient) {
			slog.ErrorContext(r.Context(), "mcp: authorize client lookup", "err", err)
			s.browserError(w, r, "server_error")
			return
		}
		s.browserError(w, r, "unknown_client")
		return
	}
	redirectURI, ok := matchRedirect(client.redirectURIs, q.Get("redirect_uri"))
	if !ok {
		s.browserError(w, r, "redirect_mismatch")
		return
	}

	// From here the redirect URI is trusted: errors go back to the client.
	state := q.Get("state")
	fail := func(code, desc string) {
		http.Redirect(w, r, withParams(redirectURI, map[string]string{
			"error": code, "error_description": desc, "state": state, "iss": s.issuer(),
		}), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "Only response_type=code is supported.")
		return
	}
	challenge := q.Get("code_challenge")
	if q.Get("code_challenge_method") != "S256" || len(challenge) != 43 || !pkceChars(challenge) {
		fail("invalid_request", "PKCE with code_challenge_method=S256 is required.")
		return
	}
	scopes, err := parseScopes(q.Get("scope"))
	if err != nil {
		fail("invalid_scope", "Supported scopes are read and write.")
		return
	}
	resource := s.resourceURL()
	if rs := q["resource"]; len(rs) > 1 {
		fail("invalid_target", "Send one resource.")
		return
	} else if len(rs) == 1 {
		res, ok := canonicalResource(rs[0])
		if !ok || !s.acceptedResource(res) {
			fail("invalid_target", "Unknown resource. Use "+s.resourceURL()+".")
			return
		}
		resource = res
	}

	handle := newOpaque("")
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO mcp_oauth_requests (handle_hash, client_ref, redirect_uri, state, code_challenge, scopes, resource, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now() + $8::interval)`,
		hashSecret(handle), client.ref, redirectURI, state, challenge, scopes, resource, requestTTL.String()); err != nil {
		slog.ErrorContext(r.Context(), "mcp: park authorization request", "err", err)
		fail("server_error", "")
		return
	}
	http.Redirect(w, r, s.webURL()+"/connect?request="+url.QueryEscape(handle), http.StatusFound)
}

// ---- token -----------------------------------------------------------------

// token is POST /oauth/token: authorization_code (PKCE S256 mandatory) and
// refresh_token (rotating; a replayed refresh token revokes its grant).
func (s *Service) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_request", "Send a form-encoded body."))
		return
	}
	client, e := s.authenticateClient(r)
	if e != nil {
		writeOAuthError(w, e)
		return
	}
	var out *tokenPair
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		out, e = s.exchangeCode(r.Context(), client, r.PostForm)
	case "refresh_token":
		out, e = s.refresh(r.Context(), client, r.PostForm)
	default:
		e = oerr(http.StatusBadRequest, "unsupported_grant_type", "Use authorization_code or refresh_token.")
	}
	if e != nil {
		writeOAuthError(w, e)
		return
	}
	writeOAuthJSON(w, http.StatusOK, map[string]any{
		"access_token":  out.access,
		"token_type":    "Bearer",
		"expires_in":    int(s.oauth.accessTTL.Seconds()),
		"refresh_token": out.refresh,
		"scope":         strings.Join(out.scopes, " "),
	})
}

type tokenPair struct {
	access, refresh string
	scopes          []string
}

var errGrantInvalid = oerr(http.StatusBadRequest, "invalid_grant", "That grant is invalid, expired or revoked. Connect again.")
var errAccountInactive = oerr(http.StatusBadRequest, "invalid_grant", "This account can't connect apps. Contact support.")

func serverErr(ctx context.Context, what string, err error) *oauthErr {
	slog.ErrorContext(ctx, "mcp: "+what, "err", err)
	return oerr(http.StatusInternalServerError, "server_error", "")
}

func (s *Service) exchangeCode(ctx context.Context, client *oauthClient, form url.Values) (*tokenPair, *oauthErr) {
	code := form.Get("code")
	if code == "" {
		return nil, oerr(http.StatusBadRequest, "invalid_request", "code is required.")
	}
	var (
		clientRef, userID      uuid.UUID
		redirectURI, challenge string
		resource, status       string
		scopes                 []string
	)
	err := s.d.Pool.QueryRow(ctx, `
		UPDATE mcp_oauth_codes c SET consumed_at = now()
		FROM users u
		WHERE c.code_hash = $1 AND c.consumed_at IS NULL AND c.expires_at > now() AND u.id = c.user_id
		RETURNING c.client_ref, c.user_id, c.redirect_uri, c.code_challenge, c.scopes, c.resource, u.status`,
		hashSecret(code)).Scan(&clientRef, &userID, &redirectURI, &challenge, &scopes, &resource, &status)
	if db.IsNoRows(err) {
		// Spent already? OAuth 2.1 §4.1.3: revoke what the first exchange issued.
		tag, rerr := s.d.Pool.Exec(ctx, `
			UPDATE mcp_oauth_grants SET revoked_at = now(), revoke_reason = 'code_reuse', updated_at = now()
			WHERE revoked_at IS NULL AND id = (SELECT grant_id FROM mcp_oauth_codes WHERE code_hash = $1 AND consumed_at IS NOT NULL)`,
			hashSecret(code))
		if rerr == nil && tag.RowsAffected() > 0 {
			slog.WarnContext(ctx, "mcp: authorization code replayed; grant revoked", "client_id", client.clientID)
		}
		return nil, errGrantInvalid
	}
	if err != nil {
		return nil, serverErr(ctx, "consume code", err)
	}
	// Bound to the client, the redirect URI and the PKCE challenge it was minted for.
	if clientRef != client.ref || form.Get("redirect_uri") != redirectURI || !verifyPKCE(form.Get("code_verifier"), challenge) {
		return nil, errGrantInvalid
	}
	if rs := form["resource"]; len(rs) > 0 {
		res, ok := canonicalResource(rs[0])
		if len(rs) > 1 || !ok || res != resource {
			return nil, oerr(http.StatusBadRequest, "invalid_target", "The resource must match the authorization request.")
		}
	}
	// Asked again: minting and redeeming are separated by a browser round trip.
	if status != "active" {
		return nil, errAccountInactive
	}
	pair := &tokenPair{access: newOpaque(accessPrefix), refresh: newOpaque(refreshPrefix), scopes: scopes}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var grantID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO mcp_oauth_grants (user_id, client_ref, scopes, resource, expires_at)
			VALUES ($1, $2, $3, $4, now() + $5::interval) RETURNING id`,
			userID, clientRef, scopes, resource, s.oauth.refreshTTL.String()).Scan(&grantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mcp_oauth_codes SET grant_id = $2 WHERE code_hash = $1`, hashSecret(code), grantID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mcp_oauth_clients SET last_used_at = now() WHERE id = $1`, clientRef); err != nil {
			return err
		}
		return s.insertTokens(ctx, tx, grantID, pair)
	})
	if err != nil {
		return nil, serverErr(ctx, "issue tokens", err)
	}
	slog.InfoContext(ctx, "mcp: app connected", "user_id", userID, "client_id", client.clientID, "scopes", strings.Join(scopes, " "))
	return pair, nil
}

func (s *Service) insertTokens(ctx context.Context, tx pgx.Tx, grantID uuid.UUID, p *tokenPair) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO mcp_oauth_tokens (grant_id, access_hash, refresh_hash, scopes, access_expires_at, refresh_expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval, now() + $6::interval)`,
		grantID, hashSecret(p.access), hashSecret(p.refresh), p.scopes, s.oauth.accessTTL.String(), s.oauth.refreshTTL.String())
	return err
}

// refresh rotates a refresh token. The row is locked, so of two requests
// presenting the same token one wins and the other is a replay: a replay
// (a stolen token, or a client that kept an old one) revokes the grant, and
// the legitimate client's next refresh fails visibly instead of silently.
func (s *Service) refresh(ctx context.Context, client *oauthClient, form url.Values) (*tokenPair, *oauthErr) {
	token := form.Get("refresh_token")
	if !strings.HasPrefix(token, refreshPrefix) {
		return nil, errGrantInvalid
	}
	var pair *tokenPair
	var reused bool
	var e *oauthErr
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var (
			tokenID, grantID, clientRef uuid.UUID
			usedAt, tokRevoked          *time.Time
			grantRevoked                *time.Time
			expired                     bool
			scopes                      []string
			resource, status            string
		)
		err := tx.QueryRow(ctx, `
			SELECT t.id, t.grant_id, t.refresh_used_at, t.revoked_at, t.refresh_expires_at <= now(),
			       g.client_ref, g.revoked_at, g.scopes, g.resource, u.status
			FROM mcp_oauth_tokens t
			JOIN mcp_oauth_grants g ON g.id = t.grant_id
			JOIN users u ON u.id = g.user_id
			WHERE t.refresh_hash = $1 FOR UPDATE OF t`, hashSecret(token),
		).Scan(&tokenID, &grantID, &usedAt, &tokRevoked, &expired, &clientRef, &grantRevoked, &scopes, &resource, &status)
		if db.IsNoRows(err) {
			e = errGrantInvalid
			return nil
		}
		if err != nil {
			return err
		}
		if clientRef != client.ref {
			e = errGrantInvalid
			return nil
		}
		if usedAt != nil && grantRevoked == nil {
			reused = true
			e = errGrantInvalid
			return revokeGrant(ctx, tx, grantID, "refresh_reuse", nil)
		}
		if usedAt != nil || tokRevoked != nil || grantRevoked != nil || expired {
			e = errGrantInvalid
			return nil
		}
		if status != "active" {
			e = errAccountInactive
			return nil
		}
		if rs := form["resource"]; len(rs) > 0 {
			res, ok := canonicalResource(rs[0])
			if len(rs) > 1 || !ok || res != resource {
				e = oerr(http.StatusBadRequest, "invalid_target", "The resource must match the original grant.")
				return nil
			}
		}
		issued := scopes
		if raw := strings.TrimSpace(form.Get("scope")); raw != "" {
			want, err := parseScopes(raw)
			if err != nil || (slices.Contains(want, ScopeWrite) && !slices.Contains(scopes, ScopeWrite)) {
				e = oerr(http.StatusBadRequest, "invalid_scope", "A refresh can't add scopes. Connect again for more access.")
				return nil
			}
			issued = want
		}
		if _, err := tx.Exec(ctx, `UPDATE mcp_oauth_tokens SET refresh_used_at = now(), revoked_at = now() WHERE id = $1`, tokenID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE mcp_oauth_grants SET expires_at = now() + $2::interval, updated_at = now() WHERE id = $1`,
			grantID, s.oauth.refreshTTL.String()); err != nil {
			return err
		}
		pair = &tokenPair{access: newOpaque(accessPrefix), refresh: newOpaque(refreshPrefix), scopes: issued}
		return s.insertTokens(ctx, tx, grantID, pair)
	})
	if err != nil {
		return nil, serverErr(ctx, "refresh", err)
	}
	if reused {
		slog.WarnContext(ctx, "mcp: refresh token replayed; grant revoked", "client_id", client.clientID)
	}
	if e != nil {
		return nil, e
	}
	return pair, nil
}

// revokeGrant ends a grant and every token in it.
func revokeGrant(ctx context.Context, q db.Querier, grantID uuid.UUID, reason string, by *uuid.UUID) error {
	if _, err := q.Exec(ctx, `
		UPDATE mcp_oauth_grants SET revoked_at = now(), revoke_reason = $2, revoked_by = $3, updated_at = now()
		WHERE id = $1 AND revoked_at IS NULL`, grantID, reason, by); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE mcp_oauth_tokens SET revoked_at = now() WHERE grant_id = $1 AND revoked_at IS NULL`, grantID)
	return err
}

// ---- revocation (RFC 7009) -------------------------------------------------

// revoke ends the grant behind an access or refresh token. It answers 200
// whether or not the token existed, so it can't be used to test tokens; a
// token issued to another client is left alone.
func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_request", "Send a form-encoded body."))
		return
	}
	client, e := s.authenticateClient(r)
	if e != nil {
		writeOAuthError(w, e)
		return
	}
	token := r.PostForm.Get("token")
	if token == "" {
		writeOAuthError(w, oerr(http.StatusBadRequest, "invalid_request", "token is required."))
		return
	}
	col := "access_hash"
	if strings.HasPrefix(token, refreshPrefix) {
		col = "refresh_hash"
	} else if !strings.HasPrefix(token, accessPrefix) {
		writeOAuthJSON(w, http.StatusOK, map[string]any{})
		return
	}
	err := db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var grantID uuid.UUID
		err := tx.QueryRow(r.Context(), `
			SELECT t.grant_id FROM mcp_oauth_tokens t JOIN mcp_oauth_grants g ON g.id = t.grant_id
			WHERE t.`+col+` = $1 AND g.client_ref = $2`, hashSecret(token), client.ref).Scan(&grantID)
		if db.IsNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return revokeGrant(r.Context(), tx, grantID, "client", nil)
	})
	if err != nil {
		writeOAuthError(w, serverErr(r.Context(), "revoke", err))
		return
	}
	writeOAuthJSON(w, http.StatusOK, map[string]any{})
}
