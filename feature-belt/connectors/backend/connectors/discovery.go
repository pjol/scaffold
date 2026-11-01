package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Discovery follows the MCP authorization spec (2025-11-25):
//
//  1. Probe the MCP endpoint without a token. A 401 carries
//     WWW-Authenticate: Bearer resource_metadata="…", scope="…".
//  2. Protected Resource Metadata (RFC 9728): the URL from the challenge,
//     then /.well-known/oauth-protected-resource/<path>, then the root.
//     Its resource must name this server; its first authorization server is
//     used.
//  3. Authorization Server Metadata (RFC 8414) with OpenID Connect discovery
//     as the fallback, path insertion when the issuer has a path. The issuer
//     must match and S256 PKCE must be advertised, or we stop.
//  4. Scopes: the challenge's scope, else the resource's scopes_supported.

// serverAuth is what discovery learned about a server's sign-in.
type serverAuth struct {
	// NoAuth: the server answered the probe without asking for a token.
	NoAuth bool
	// Resource is the RFC 8707 resource indicator sent with authorization
	// and token requests.
	Resource string
	Scopes   []string
	AS       authServerMeta
}

type authServerMeta struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
	IssParameterSupported             bool     `json:"authorization_response_iss_parameter_supported"`
}

type protectedResourceMeta struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// discoveryError is a discovery failure with a stable code and a sentence
// for the person who typed the URL.
type discoveryError struct {
	Code    string
	Message string
	cause   error
}

func (e *discoveryError) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *discoveryError) Unwrap() error { return e.cause }

func discoveryFail(code, msg string, cause error) *discoveryError {
	return &discoveryError{Code: code, Message: msg, cause: cause}
}

// discover learns how serverURL wants users to sign in.
func (s *Service) discover(ctx context.Context, serverURL string) (*serverAuth, error) {
	u, err := s.guard.checkURL(serverURL)
	if err != nil {
		return nil, unreachable(err)
	}
	challengeMeta, challengeScopes, open, err := s.probe(ctx, u.String())
	if err != nil {
		return nil, err
	}
	if open {
		return &serverAuth{NoAuth: true, Resource: canonicalResource(u)}, nil
	}

	prm, err := s.protectedResource(ctx, u, challengeMeta)
	if err != nil {
		return nil, err
	}
	out := &serverAuth{Resource: canonicalResource(u)}
	issuer := ""
	if prm != nil {
		out.Resource = prm.Resource
		issuer = prm.AuthorizationServers[0]
		out.Scopes = prm.ScopesSupported
	} else {
		// 2025-03-26 fallback: the server's origin is its authorization server.
		issuer = u.Scheme + "://" + u.Host
	}
	if len(challengeScopes) > 0 {
		out.Scopes = challengeScopes
	}
	as, err := s.authServer(ctx, issuer)
	if err != nil {
		return nil, err
	}
	out.AS = *as
	return out, nil
}

// probe sends an unauthenticated MCP initialize. It returns the challenge's
// resource_metadata URL and scopes, or open=true when the server answered
// without asking for a token.
func (s *Service) probe(ctx context.Context, serverURL string) (metaURL string, scopes []string, open bool, err error) {
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"scaffoldapp","version":"1.0.0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(body))
	if err != nil {
		return "", nil, false, discoveryFail("invalid_server_url", "That server URL isn't valid.", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := s.metaClient.Do(req)
	if err != nil {
		return "", nil, false, unreachable(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		challenges, _ := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
		for _, c := range challenges {
			if c.Scheme != "bearer" {
				continue
			}
			if v := c.Params["resource_metadata"]; v != "" && metaURL == "" {
				metaURL = v
			}
			if v := c.Params["scope"]; v != "" && scopes == nil {
				scopes = strings.Fields(v)
			}
		}
		return metaURL, scopes, false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return "", nil, true, nil
	default:
		// Some servers refuse a bare initialize (405, 406, 400); the
		// well-known documents may still describe them.
		return "", nil, false, nil
	}
}

// protectedResource finds the server's Protected Resource Metadata (nil when
// it publishes none).
func (s *Service) protectedResource(ctx context.Context, u *url.URL, challengeURL string) (*protectedResourceMeta, error) {
	origin := u.Scheme + "://" + u.Host
	var candidates []string
	if challengeURL != "" {
		candidates = append(candidates, challengeURL)
	}
	if p := strings.Trim(u.EscapedPath(), "/"); p != "" {
		candidates = append(candidates, origin+"/.well-known/oauth-protected-resource/"+p)
	}
	candidates = append(candidates, origin+"/.well-known/oauth-protected-resource")
	for _, c := range candidates {
		var prm protectedResourceMeta
		status, err := s.getJSON(ctx, c, &prm)
		if err != nil {
			if isBlocked(err) {
				return nil, unreachable(err)
			}
			continue
		}
		if status != http.StatusOK {
			continue
		}
		// RFC 9728 §3.3: the document must describe this server (the full
		// URL, or its origin for the root document).
		if !sameResource(prm.Resource, u.String()) && !sameResource(prm.Resource, origin) {
			continue
		}
		if len(prm.AuthorizationServers) == 0 {
			return nil, discoveryFail("no_authorization_server", "The server doesn't name an authorization server.", nil)
		}
		if _, err := s.guard.checkURL(prm.AuthorizationServers[0]); err != nil {
			return nil, unreachable(err)
		}
		return &prm, nil
	}
	return nil, nil
}

// authServer fetches RFC 8414 metadata (OIDC discovery as fallback).
func (s *Service) authServer(ctx context.Context, issuer string) (*authServerMeta, error) {
	iu, err := s.guard.checkURL(issuer)
	if err != nil {
		return nil, unreachable(err)
	}
	origin := iu.Scheme + "://" + iu.Host
	var candidates []string
	if p := strings.Trim(iu.EscapedPath(), "/"); p == "" {
		candidates = []string{origin + "/.well-known/oauth-authorization-server", origin + "/.well-known/openid-configuration"}
	} else {
		candidates = []string{
			origin + "/.well-known/oauth-authorization-server/" + p,
			origin + "/.well-known/openid-configuration/" + p,
			origin + "/" + p + "/.well-known/openid-configuration",
		}
	}
	for _, c := range candidates {
		var m authServerMeta
		status, err := s.getJSON(ctx, c, &m)
		if err != nil {
			if isBlocked(err) {
				return nil, unreachable(err)
			}
			continue
		}
		if status != http.StatusOK {
			continue
		}
		if strings.TrimSuffix(m.Issuer, "/") != strings.TrimSuffix(issuer, "/") {
			return nil, discoveryFail("issuer_mismatch", "The authorization server's metadata names a different issuer.", nil)
		}
		if !slices.Contains(m.CodeChallengeMethodsSupported, "S256") {
			return nil, discoveryFail("pkce_unsupported", "The authorization server doesn't support PKCE (S256), so connecting isn't safe.", nil)
		}
		if m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" {
			return nil, discoveryFail("authorization_server_incomplete", "The authorization server's metadata is missing its endpoints.", nil)
		}
		for _, e := range []string{m.AuthorizationEndpoint, m.TokenEndpoint, m.RegistrationEndpoint, m.RevocationEndpoint} {
			if e == "" {
				continue
			}
			if _, err := s.guard.checkURL(e); err != nil {
				return nil, unreachable(err)
			}
		}
		return &m, nil
	}
	return nil, discoveryFail("no_authorization_metadata", "Couldn't find the server's sign-in settings (no authorization server metadata).", nil)
}

// getJSON GETs u into dst. Non-200 answers return their status and no error.
func (s *Service) getJSON(ctx context.Context, u string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.metaClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.StatusCode, nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(dst); err != nil {
		return 0, fmt.Errorf("connectors: %s is not JSON: %w", u, err)
	}
	return http.StatusOK, nil
}

// canonicalResource is the server URL as a resource indicator: lowercase
// scheme and host, no fragment or query, no trailing slash.
func canonicalResource(u *url.URL) string {
	c := url.URL{Scheme: strings.ToLower(u.Scheme), Host: strings.ToLower(u.Host), Path: u.Path, RawPath: u.RawPath}
	return strings.TrimSuffix(c.String(), "/")
}

// sameResource compares resource identifiers, ignoring case in scheme and
// host and a trailing slash.
func sameResource(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil || ua.Host == "" || ub.Host == "" {
		return false
	}
	return canonicalResource(ua) == canonicalResource(ub)
}

// unreachable turns a transport or guard error into a discovery error.
func unreachable(err error) error {
	var de *discoveryError
	if errors.As(err, &de) {
		return de
	}
	if isBlocked(err) {
		return discoveryFail("address_blocked", "That server's address isn't allowed: it must be a public https address.", err)
	}
	if errors.Is(err, errTooLarge) {
		return discoveryFail("response_too_large", "The server sent a response that is too large.", err)
	}
	return discoveryFail("server_unreachable", "Couldn't reach that server. Check the URL and try again.", err)
}
