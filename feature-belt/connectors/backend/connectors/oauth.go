package connectors

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The OAuth 2.1 client half of MCP authorization: client registration
// (pre-registered → Client ID Metadata Document → Dynamic Client
// Registration), the authorization URL with PKCE S256, state and the RFC
// 8707 resource indicator, and the token endpoint (code exchange, refresh,
// RFC 7009 revocation). Token requests are written by hand rather than with
// golang.org/x/oauth2 because refreshes must carry `resource` too.

// oauthClient is how we identify ourselves to one authorization server.
type oauthClient struct {
	ID         string
	Secret     string
	AuthMethod string // client_secret_basic | client_secret_post | none
	Source     string // preregistered | metadata_document | dynamic
}

// redirectURI is where every authorization server sends the browser back.
func (s *Service) redirectURI() string {
	return strings.TrimSuffix(s.d.Cfg.PublicAPIURL, "/") + "/v1/connectors/oauth/callback"
}

// clientMetadataURL is our Client ID Metadata Document (its URL is the
// client_id). Usable only when the API is on public https.
func (s *Service) clientMetadataURL() string {
	return strings.TrimSuffix(s.d.Cfg.PublicAPIURL, "/") + "/v1/connectors/oauth/client-metadata.json"
}

func (s *Service) canUseMetadataDocument() bool {
	u, err := url.Parse(s.d.Cfg.PublicAPIURL)
	return err == nil && u.Scheme == "https" && !strings.EqualFold(u.Hostname(), "localhost") && !isIPLiteral(u.Hostname())
}

func isIPLiteral(h string) bool {
	_, err := netip.ParseAddr(h)
	return err == nil
}

// resolveClient picks the client registration for c at as, registering
// dynamically (and caching the result) when needed.
func (s *Service) resolveClient(ctx context.Context, c *connectorRow, as *authServerMeta) (*oauthClient, error) {
	if c.OAuthClientID != "" {
		secret, err := s.seal.open(c.OAuthClientSecretEnc, aadConnector(c.ID))
		if err != nil {
			return nil, err
		}
		method := "none"
		if secret != "" {
			method = pickAuthMethod(as.TokenEndpointAuthMethodsSupported, true)
		}
		return &oauthClient{ID: c.OAuthClientID, Secret: secret, AuthMethod: method, Source: "preregistered"}, nil
	}
	if as.ClientIDMetadataDocumentSupported && s.canUseMetadataDocument() {
		return &oauthClient{ID: s.clientMetadataURL(), AuthMethod: "none", Source: "metadata_document"}, nil
	}
	if as.RegistrationEndpoint == "" {
		return nil, discoveryFail("registration_unsupported",
			"This server needs a client ID registered in advance. Ask an admin to add one to the connector.", nil)
	}
	return s.dynamicClient(ctx, as)
}

// pickAuthMethod chooses a token endpoint auth method. RFC 8414: absent
// means client_secret_basic.
func pickAuthMethod(supported []string, haveSecret bool) string {
	if !haveSecret {
		return "none"
	}
	if len(supported) == 0 {
		return "client_secret_basic"
	}
	for _, m := range []string{"client_secret_basic", "client_secret_post"} {
		if slices.Contains(supported, m) {
			return m
		}
	}
	return "client_secret_basic"
}

// dynamicClient returns the cached DCR registration for as, registering
// when there is none or its secret expired.
func (s *Service) dynamicClient(ctx context.Context, as *authServerMeta) (*oauthClient, error) {
	redirect := s.redirectURI()
	var id uuid.UUID
	var cl oauthClient
	var secretEnc []byte
	var expires *time.Time
	err := s.d.Pool.QueryRow(ctx, `
		SELECT id, client_id, client_secret_enc, token_auth_method, secret_expires_at
		FROM connector_oauth_clients WHERE issuer = $1 AND redirect_uri = $2`, as.Issuer, redirect,
	).Scan(&id, &cl.ID, &secretEnc, &cl.AuthMethod, &expires)
	if err == nil && (expires == nil || expires.After(time.Now().Add(time.Hour))) {
		secret, err := s.seal.open(secretEnc, aadClient(id))
		if err == nil {
			cl.Secret, cl.Source = secret, "dynamic"
			return &cl, nil
		}
	} else if err != nil && !isNoRows(err) {
		return nil, err
	}

	method := "none"
	if len(as.TokenEndpointAuthMethodsSupported) == 0 || slices.ContainsFunc(as.TokenEndpointAuthMethodsSupported, func(m string) bool {
		return m == "client_secret_basic" || m == "client_secret_post"
	}) {
		method = pickAuthMethod(as.TokenEndpointAuthMethodsSupported, true)
	}
	reg := map[string]any{
		"client_name":                appName,
		"redirect_uris":              []string{redirect},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
		"application_type":           "web",
	}
	if strings.HasPrefix(s.d.Cfg.PublicWebURL, "https://") {
		reg["client_uri"] = s.d.Cfg.PublicWebURL
	}
	body, _ := json.Marshal(reg)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, as.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.tokenClient.Do(req)
	if err != nil {
		return nil, unreachable(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, unreachable(err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, discoveryFail("registration_failed",
			fmt.Sprintf("The authorization server refused to register this app (HTTP %d).", resp.StatusCode), errors.New(oauthErrorText(raw)))
	}
	var out struct {
		ClientID              string `json:"client_id"`
		ClientSecret          string `json:"client_secret"`
		TokenEndpointAuth     string `json:"token_endpoint_auth_method"`
		ClientSecretExpiresAt int64  `json:"client_secret_expires_at"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ClientID == "" {
		return nil, discoveryFail("registration_failed", "The authorization server's registration answer is missing a client ID.", err)
	}
	cl = oauthClient{ID: out.ClientID, Secret: out.ClientSecret, AuthMethod: out.TokenEndpointAuth, Source: "dynamic"}
	if cl.AuthMethod == "" {
		cl.AuthMethod = method
	}
	if cl.Secret == "" {
		cl.AuthMethod = "none"
	}
	var exp *time.Time
	if out.ClientSecretExpiresAt > 0 {
		t := time.Unix(out.ClientSecretExpiresAt, 0).UTC()
		exp = &t
	}
	newID := uuid.New()
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO connector_oauth_clients (id, issuer, redirect_uri, client_id, client_secret_enc, token_auth_method, secret_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (issuer, redirect_uri) DO UPDATE SET id = EXCLUDED.id, client_id = EXCLUDED.client_id,
		  client_secret_enc = EXCLUDED.client_secret_enc, token_auth_method = EXCLUDED.token_auth_method,
		  secret_expires_at = EXCLUDED.secret_expires_at, created_at = now()`,
		newID, as.Issuer, redirect, cl.ID, s.seal.seal(cl.Secret, aadClient(newID)), cl.AuthMethod, exp); err != nil {
		return nil, err
	}
	return &cl, nil
}

// forgetDynamicClient drops a cached registration the server no longer
// accepts (invalid_client), so the next connect registers again.
func (s *Service) forgetDynamicClient(ctx context.Context, issuer, clientID string) {
	_, _ = s.d.Pool.Exec(ctx, `DELETE FROM connector_oauth_clients WHERE issuer = $1 AND client_id = $2`, issuer, clientID)
}

// ---- authorization request ---------------------------------------------------

// randomToken is 32 random bytes, base64url.
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("connectors: no randomness: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeURL builds the authorization request.
func authorizeURL(endpoint, clientID, redirect, state, verifier, resource string, scopes []string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge(verifier))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", resource)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// requestedScopes: the catalog's override, else what discovery found, plus
// offline_access when the server offers it (so we get a refresh token).
func requestedScopes(override string, discovered []string, as *authServerMeta) []string {
	scopes := discovered
	if o := strings.Fields(override); len(o) > 0 {
		scopes = o
	}
	scopes = slices.Clone(scopes)
	if slices.Contains(as.ScopesSupported, "offline_access") && !slices.Contains(scopes, "offline_access") {
		scopes = append(scopes, "offline_access")
	}
	return scopes
}

// ---- token endpoint ----------------------------------------------------------

// tokenSet is a token endpoint answer.
type tokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    *time.Time
	Scope        string
}

// tokenError is an OAuth error answer. Code is the RFC 6749 error code;
// invalid_grant on a refresh means the user must sign in again.
type tokenError struct {
	Status int
	Code   string
	Desc   string
}

func (e *tokenError) Error() string {
	return fmt.Sprintf("token endpoint: %d %s %s", e.Status, e.Code, e.Desc)
}

// reauthNeeded reports whether err means the grant is gone (sign in again)
// rather than a passing failure.
func reauthNeeded(err error) bool {
	var te *tokenError
	if !errors.As(err, &te) {
		return false
	}
	switch te.Code {
	case "invalid_grant", "invalid_client", "unauthorized_client", "invalid_scope":
		return true
	}
	return te.Status == http.StatusBadRequest || te.Status == http.StatusUnauthorized
}

type tokenRequest struct {
	Endpoint   string
	Client     oauthClient
	Form       url.Values
	Resource   string
	PrevScopes string
}

// tokenCall posts a token request with the client's authentication.
func (s *Service) tokenCall(ctx context.Context, tr tokenRequest) (*tokenSet, error) {
	form := tr.Form
	if tr.Resource != "" {
		form.Set("resource", tr.Resource)
	}
	switch tr.Client.AuthMethod {
	case "client_secret_post":
		form.Set("client_id", tr.Client.ID)
		form.Set("client_secret", tr.Client.Secret)
	case "client_secret_basic":
	default:
		form.Set("client_id", tr.Client.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tr.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if tr.Client.AuthMethod == "client_secret_basic" {
		req.SetBasicAuth(url.QueryEscape(tr.Client.ID), url.QueryEscape(tr.Client.Secret))
	}
	resp, err := s.tokenClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		te := &tokenError{Status: resp.StatusCode}
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(raw, &e) == nil {
			te.Code, te.Desc = e.Error, truncate(e.Description, 200)
		}
		return nil, te
	}
	var out struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		TokenType    string          `json:"token_type"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
		Scope        string          `json:"scope"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.AccessToken == "" {
		return nil, errors.New("connectors: token endpoint answered without an access token")
	}
	ts := &tokenSet{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, TokenType: out.TokenType, Scope: out.Scope}
	if ts.TokenType == "" || strings.EqualFold(ts.TokenType, "bearer") {
		ts.TokenType = "Bearer"
	}
	if secs := parseExpiresIn(out.ExpiresIn); secs > 0 {
		t := time.Now().Add(time.Duration(secs) * time.Second).UTC()
		ts.ExpiresAt = &t
	}
	if ts.Scope == "" {
		ts.Scope = tr.PrevScopes
	}
	return ts, nil
}

// parseExpiresIn accepts a number or a numeric string (some servers send one).
func parseExpiresIn(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(bytes.Trim(raw, `"`)))
	dec.UseNumber()
	if dec.Decode(&n) != nil {
		return 0
	}
	if i, err := n.Int64(); err == nil {
		return i
	}
	if f, err := n.Float64(); err == nil {
		return int64(f)
	}
	return 0
}

// revokeToken calls an RFC 7009 revocation endpoint. 200 means revoked (or
// already invalid, which RFC 7009 also answers with 200).
func (s *Service) revokeToken(ctx context.Context, endpoint string, client oauthClient, token, hint string) error {
	form := url.Values{"token": {token}, "token_type_hint": {hint}}
	switch client.AuthMethod {
	case "client_secret_post":
		form.Set("client_id", client.ID)
		form.Set("client_secret", client.Secret)
	case "client_secret_basic":
	default:
		form.Set("client_id", client.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if client.AuthMethod == "client_secret_basic" {
		req.SetBasicAuth(url.QueryEscape(client.ID), url.QueryEscape(client.Secret))
	}
	resp, err := s.tokenClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("connectors: revocation endpoint answered %d", resp.StatusCode)
	}
	return nil
}

// oauthErrorText is the error code and description from an OAuth error
// body, for logs (never the full body).
func oauthErrorText(raw []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return "unreadable error body"
	}
	return truncate(strings.TrimSpace(e.Error+" "+e.Description), 200)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
