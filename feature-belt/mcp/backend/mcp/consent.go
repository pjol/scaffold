package mcp

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/legal"
)

// The consent page's API. The web app's /connect page reads the parked
// request with the signed-in person's session, shows who is asking and for
// what, and posts the decision; the answer is the URL to send the browser
// back to the client.

// OAuthClientInfo is what the consent page says about the client.
type OAuthClientInfo struct {
	Name string `json:"name"` // self-declared by the client; "" when it gave none
	Kind string `json:"kind"` // registered | metadata_document
	// Host publishing the client's metadata document (metadata_document
	// clients only): the one name on the page the client can't make up.
	Host *string `json:"host"`
}

// OAuthRequest is a pending authorization request.
type OAuthRequest struct {
	Client       OAuthClientInfo `json:"client"`
	RedirectURI  string          `json:"redirect_uri"`
	RedirectHost string          `json:"redirect_host"`
	RedirectKind string          `json:"redirect_kind"` // web | loopback | app
	Scopes       []string        `json:"scopes"`
	ExpiresAt    time.Time       `json:"expires_at"`
}

var errRequestExpired = httpx.NewError(http.StatusNotFound, "request_expired",
	"This request expired or was already used. Start again from the app you're connecting.")

func clientInfo(clientID, kind, name string) OAuthClientInfo {
	info := OAuthClientInfo{Name: name, Kind: kind}
	if kind == "metadata_document" {
		if u, err := url.Parse(clientID); err == nil {
			h := u.Host
			info.Host = &h
		}
	}
	return info
}

// describeRequest is GET /v1/mcp/oauth/request?id=<handle>. The handle is in
// the query so request logs (path only) never record it.
func (s *Service) describeRequest(w http.ResponseWriter, r *http.Request) error {
	handle := strings.TrimSpace(r.URL.Query().Get("id"))
	if handle == "" {
		return errRequestExpired
	}
	var (
		out                  OAuthRequest
		clientID, kind, name string
	)
	err := s.d.Pool.QueryRow(r.Context(), `
		SELECT c.client_id, c.kind, c.client_name, q.redirect_uri, q.scopes, q.expires_at
		FROM mcp_oauth_requests q JOIN mcp_oauth_clients c ON c.id = q.client_ref
		WHERE q.handle_hash = $1 AND q.expires_at > now()`, hashSecret(handle),
	).Scan(&clientID, &kind, &name, &out.RedirectURI, &out.Scopes, &out.ExpiresAt)
	if db.IsNoRows(err) {
		return errRequestExpired
	}
	if err != nil {
		return err
	}
	out.Client = clientInfo(clientID, kind, name)
	out.RedirectHost = redirectHost(out.RedirectURI)
	out.RedirectKind, _ = redirectKind(out.RedirectURI)
	httpx.OK(w, out)
	return nil
}

type decisionReq struct {
	ID     string   `json:"id"`
	Allow  bool     `json:"allow"`
	Scopes []string `json:"scopes"` // optional: narrow what was requested (drop write)
}

// OAuthDecision is where to send the browser next.
type OAuthDecision struct {
	RedirectURL string `json:"redirect_url"`
}

// decide is POST /v1/mcp/oauth/decision. The request is spent whatever the
// answer. Allowing needs an active account and current legal consent and
// mints a single-use code; denying sends access_denied back to the client
// and works for anyone (it is exempt from the consent gate for that reason).
func (s *Service) decide(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req decisionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Allow {
		if !id.Active() {
			return httpx.ErrSuspended
		}
		if !legal.Current(id.TermsVersion, id.PrivacyVersion) {
			return legal.ErrConsentRequired()
		}
	}
	handle := strings.TrimSpace(req.ID)
	if handle == "" {
		return errRequestExpired
	}
	var (
		clientRef                    uuid.UUID
		clientID, redirectURI, state string
		challenge, resource          string
		scopes                       []string
	)
	err := s.d.Pool.QueryRow(r.Context(), `
		DELETE FROM mcp_oauth_requests q USING mcp_oauth_clients c
		WHERE q.handle_hash = $1 AND q.expires_at > now() AND c.id = q.client_ref
		RETURNING q.client_ref, c.client_id, q.redirect_uri, q.state, q.code_challenge, q.scopes, q.resource`,
		hashSecret(handle)).Scan(&clientRef, &clientID, &redirectURI, &state, &challenge, &scopes, &resource)
	if db.IsNoRows(err) {
		return errRequestExpired
	}
	if err != nil {
		return err
	}
	if !req.Allow {
		httpx.OK(w, OAuthDecision{RedirectURL: withParams(redirectURI, map[string]string{
			"error": "access_denied", "error_description": "The person declined.", "state": state, "iss": s.issuer(),
		})})
		return nil
	}
	granted := scopes
	if len(req.Scopes) > 0 {
		granted = []string{ScopeRead}
		if slices.Contains(req.Scopes, ScopeWrite) && slices.Contains(scopes, ScopeWrite) {
			granted = append(granted, ScopeWrite)
		}
	}
	code := newOpaque("")
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO mcp_oauth_codes (code_hash, client_ref, user_id, redirect_uri, code_challenge, scopes, resource, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now() + $8::interval)`,
		hashSecret(code), clientRef, id.UserID, redirectURI, challenge, granted, resource, codeTTL.String()); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE mcp_oauth_clients SET last_used_at = now() WHERE id = $1`, clientRef); err != nil {
		return err
	}
	httpx.OK(w, OAuthDecision{RedirectURL: withParams(redirectURI, map[string]string{
		"code": code, "state": state, "iss": s.issuer(),
	})})
	return nil
}
