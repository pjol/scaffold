package connectors

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Platform connections: one row per person per platform account, with the
// capabilities (declared by features through app.CapabilityProvider) the
// person turned on for it. Turning on a capability whose scopes aren't
// granted yet sends the person through consent for just those (Google,
// Microsoft) or for the union (the rest). Disconnecting deletes our tokens
// at once and asks the platform to revoke them.

const maxPlatformConnectionsPerUser = 50

// ---- rows ------------------------------------------------------------------

type platformRow struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	Provider        string
	Kind            string // oauth | token
	BaseURL         string
	InstanceURL     string
	Account         string
	AccountID       string
	Status          string
	AccessTokenEnc  []byte
	RefreshTokenEnc []byte
	TokenType       string
	ExpiresAt       *time.Time
	Scopes          string
	Capabilities    []string
	LastError       string
	LastErrorAt     *time.Time
	LastUsedAt      *time.Time
	ConnectedAt     time.Time
}

const platformCols = `pc.id, pc.user_id, pc.provider, pc.kind, pc.base_url, pc.instance_url, pc.account, pc.account_id,
	pc.status, pc.access_token_enc, pc.refresh_token_enc, pc.token_type, pc.expires_at, pc.scopes, pc.capabilities,
	pc.last_error, pc.last_error_at, pc.last_used_at, pc.connected_at`

func scanPlatform(row pgx.Row) (*platformRow, error) {
	var r platformRow
	err := row.Scan(&r.ID, &r.UserID, &r.Provider, &r.Kind, &r.BaseURL, &r.InstanceURL, &r.Account, &r.AccountID,
		&r.Status, &r.AccessTokenEnc, &r.RefreshTokenEnc, &r.TokenType, &r.ExpiresAt, &r.Scopes, &r.Capabilities,
		&r.LastError, &r.LastErrorAt, &r.LastUsedAt, &r.ConnectedAt)
	if err != nil {
		return nil, err
	}
	if r.Capabilities == nil {
		r.Capabilities = []string{}
	}
	return &r, nil
}

func (r *platformRow) granted() []string { return splitGranted(r.Scopes) }

// ownPlatform loads the caller's connection (404 for anyone else's).
func (s *Service) ownPlatform(ctx context.Context, q db.Querier, userID, id uuid.UUID, lock bool) (*platformRow, error) {
	sql := `SELECT ` + platformCols + ` FROM connector_platform_connections pc WHERE pc.id = $1 AND pc.user_id = $2`
	if lock {
		sql += ` FOR UPDATE`
	}
	r, err := scanPlatform(q.QueryRow(ctx, sql, id, userID))
	if isNoRows(err) {
		return nil, httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "That connection doesn't exist.")
	}
	return r, err
}

func (s *Service) userPlatforms(ctx context.Context, q db.Querier, userID uuid.UUID, provider string) ([]*platformRow, error) {
	rows, err := q.Query(ctx, `SELECT `+platformCols+` FROM connector_platform_connections pc
		WHERE pc.user_id = $1 AND ($2 = '' OR pc.provider = $2) ORDER BY pc.connected_at, pc.id`, userID, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*platformRow
	for rows.Next() {
		r, err := scanPlatform(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) markPlatformNeedsReauth(ctx context.Context, id uuid.UUID, msg string) {
	_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `
		UPDATE connector_platform_connections SET status = 'needs_reauth', last_error = $2, last_error_at = now(), updated_at = now()
		WHERE id = $1`, id, msg)
}

// ---- capabilities ------------------------------------------------------------

// capabilities are every installed module's platform capabilities, read at
// request time, for platforms in the registry, in registry order.
func (s *Service) capabilities() []app.Capability {
	var out []app.Capability
	seen := map[string]bool{}
	for _, m := range s.d.Modules {
		cp, ok := m.(app.CapabilityProvider)
		if !ok {
			continue
		}
		for _, c := range cp.Capabilities() {
			if c.Key == "" || seen[c.Key] || s.providers[c.Provider] == nil {
				if c.Key != "" && s.providers[c.Provider] == nil {
					s.warnOnce("capability:"+c.Key, "connectors: capability names a platform that isn't in the registry", "capability", c.Key, "provider", c.Provider)
				}
				continue
			}
			if c.Module == "" {
				c.Module = m.Name()
			}
			seen[c.Key] = true
			out = append(out, c)
		}
	}
	rank := map[string]int{}
	for i, id := range s.providerOrder {
		rank[id] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return rank[out[i].Provider] < rank[out[j].Provider]
		}
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		return out[i].Write != out[j].Write && !out[i].Write
	})
	return out
}

func (s *Service) capability(key string) (app.Capability, bool) {
	for _, c := range s.capabilities() {
		if c.Key == key {
			return c, true
		}
	}
	return app.Capability{}, false
}

func (s *Service) warnOnce(key, msg string, args ...any) {
	if _, loaded := s.warned.LoadOrStore(key, true); !loaded {
		slog.Warn(msg, args...)
	}
}

// capabilityGranted: the connection's grant covers the capability's scopes
// (personal tokens: the platform decides, so always).
func (s *Service) capabilityGranted(p *provider, r *platformRow, c app.Capability) bool {
	if r.Kind == "token" {
		return true
	}
	return len(p.missingScopes(r.granted(), c.Scopes)) == 0
}

// ---- DTOs ----------------------------------------------------------------------

// ScopeNote explains a scope that grants more than its name suggests.
type ScopeNote struct {
	Scope string `json:"scope"`
	Note  string `json:"note"`
}

// PlatformCapability is one capability a platform offers here.
type PlatformCapability struct {
	Key        string      `json:"key"`
	Module     string      `json:"module"`
	Label      string      `json:"label"`
	Scopes     []string    `json:"scopes"`
	ScopeNotes []ScopeNote `json:"scope_notes"`
	Write      bool        `json:"write"`
	// Policy in the workspace asked about: allowed | blocked.
	Policy string `json:"policy"`
	// BlockedReason says why when blocked.
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// PlatformConnectionCapability is a capability's state on one account:
// on (turned on and granted), off, needs_grant (turned on, scopes missing:
// consent again), blocked (workspace policy).
type PlatformConnectionCapability struct {
	Key   string `json:"key"`
	State string `json:"state"`
}

// PlatformConnection is one connected account, as its owner sees it.
type PlatformConnection struct {
	ID           string                         `json:"id"`
	Provider     string                         `json:"provider"`
	Kind         string                         `json:"kind"` // oauth | token
	Account      string                         `json:"account"`
	AccountID    string                         `json:"account_id"`
	Host         string                         `json:"host,omitempty"` // GitLab instance or Salesforce org host
	Status       string                         `json:"status"`         // active | needs_reauth | blocked
	Scopes       []string                       `json:"scopes"`
	Capabilities []PlatformConnectionCapability `json:"capabilities"`
	AgentScope   string                         `json:"agent_scope"` // app.ConnectionScope(id): the AI agent access key
	LastError    string                         `json:"last_error"`
	LastErrorAt  *time.Time                     `json:"last_error_at"`
	LastUsedAt   *time.Time                     `json:"last_used_at"`
	ConnectedAt  time.Time                      `json:"connected_at"`
}

// PlatformToken says how a platform takes personal tokens.
type PlatformToken struct {
	Label           string `json:"label"`
	HelpURL         string `json:"help_url"`
	BaseURLEditable bool   `json:"base_url_editable"` // GitLab: any instance
}

// PlatformPolicy is the workspace's decision for a platform.
type PlatformPolicy struct {
	Allowed            bool     `json:"allowed"`
	AllowWrites        bool     `json:"allow_writes"`
	DeniedCapabilities []string `json:"denied_capabilities"`
	Explicit           bool     `json:"explicit"` // a row for this platform (else the default)
}

// Platform is one platform with the caller's accounts on it.
type Platform struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	OAuth        bool                 `json:"oauth"` // this server registered an app with the platform
	Token        *PlatformToken       `json:"token"`
	BaseURL      string               `json:"base_url,omitempty"`
	Revocable    bool                 `json:"revocable"`
	Policy       PlatformPolicy       `json:"policy"`
	Capabilities []PlatformCapability `json:"capabilities"`
	Connections  []PlatformConnection `json:"connections"`
}

// PlatformList is GET /v1/connectors/platforms.
type PlatformList struct {
	Items      []Platform `json:"items"`
	NextCursor *string    `json:"next_cursor"`
	OrgID      *string    `json:"org_id"`
}

func (s *Service) connectionDTO(p *provider, r *platformRow, caps []app.Capability, pol *policySet) PlatformConnection {
	out := PlatformConnection{ID: r.ID.String(), Provider: r.Provider, Kind: r.Kind, Account: r.Account, AccountID: r.AccountID,
		Status: r.Status, Scopes: r.granted(), Capabilities: []PlatformConnectionCapability{},
		AgentScope: app.ConnectionScope(r.ID.String()), LastError: r.LastError, LastErrorAt: r.LastErrorAt,
		LastUsedAt: r.LastUsedAt, ConnectedAt: r.ConnectedAt}
	switch {
	case r.InstanceURL != "":
		out.Host = hostOf(r.InstanceURL)
	case r.BaseURL != "":
		out.Host = hostOf(r.BaseURL)
	}
	if pol.blocks(p.ID, nil) != "" {
		out.Status = "blocked"
	}
	for _, c := range caps {
		if c.Provider != p.ID {
			continue
		}
		state := "off"
		on := slices.Contains(r.Capabilities, c.Key)
		switch {
		case pol.blocks(p.ID, &c) != "":
			state = "blocked"
		case on && s.capabilityGranted(p, r, c):
			state = "on"
		case on:
			state = "needs_grant"
		}
		out.Capabilities = append(out.Capabilities, PlatformConnectionCapability{Key: c.Key, State: state})
	}
	return out
}

func (s *Service) capabilityDTO(p *provider, c app.Capability, pol *policySet) PlatformCapability {
	out := PlatformCapability{Key: c.Key, Module: c.Module, Label: c.Label, Scopes: c.Scopes, ScopeNotes: []ScopeNote{}, Write: c.Write, Policy: "allowed"}
	if out.Scopes == nil {
		out.Scopes = []string{}
	}
	for _, sc := range c.Scopes {
		if n := p.scopeNote(sc); n != "" {
			out.ScopeNotes = append(out.ScopeNotes, ScopeNote{Scope: sc, Note: n})
		}
	}
	if reason := pol.blocks(p.ID, &c); reason != "" {
		out.Policy, out.BlockedReason = "blocked", reason
	}
	return out
}

// listPlatforms is GET /v1/connectors/platforms?org_id=: every platform,
// its capabilities from installed features, the workspace's policy and the
// caller's accounts.
func (s *Service) listPlatforms(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	orgID, err := optionalOrg(r)
	if err != nil {
		return err
	}
	pol, err := s.policiesFor(ctx, id.UserID, orgID, false)
	if err != nil {
		return err
	}
	rows, err := s.userPlatforms(ctx, s.d.Pool, id.UserID, "")
	if err != nil {
		return err
	}
	caps := s.capabilities()
	out := PlatformList{Items: make([]Platform, 0, len(s.providerOrder))}
	if orgID != uuid.Nil {
		v := orgID.String()
		out.OrgID = &v
	}
	for _, pid := range s.providerOrder {
		p := s.providers[pid]
		pl := Platform{ID: p.ID, Name: p.Name, OAuth: p.oauthReady(), Revocable: p.RevokeStyle != "",
			Policy: pol.platformPolicy(p.ID), Capabilities: []PlatformCapability{}, Connections: []PlatformConnection{}}
		if p.SelfManaged {
			pl.BaseURL = p.vars["base"]
		}
		if p.Token != nil {
			pl.Token = &PlatformToken{Label: p.Token.Label, HelpURL: p.page(p.Token.HelpURL), BaseURLEditable: p.SelfManaged}
		}
		for _, c := range caps {
			if c.Provider == p.ID {
				pl.Capabilities = append(pl.Capabilities, s.capabilityDTO(p, c, pol))
			}
		}
		for _, row := range rows {
			if row.Provider == p.ID {
				pl.Connections = append(pl.Connections, s.connectionDTO(p, row, caps, pol))
			}
		}
		out.Items = append(out.Items, pl)
	}
	httpx.OK(w, out)
	return nil
}

func optionalOrg(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("org_id"))
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.BadRequest("org_id must be a uuid.")
	}
	return id, nil
}

// ---- capability switches -----------------------------------------------------------

type capabilityReq struct {
	Enabled  *bool  `json:"enabled"`
	Client   string `json:"client"`    // web | mobile, when consent is needed
	ReturnTo string `json:"return_to"` // app path to come back to after consent
	OrgID    string `json:"org_id"`    // the workspace the person is in (policy)
}

// PlatformChange answers a connect or capability change: the updated
// connection, or an authorization URL to open when consent is needed.
type PlatformChange struct {
	Connection   *PlatformConnection `json:"connection,omitempty"`
	AuthorizeURL string              `json:"authorize_url,omitempty"`
}

// setCapability is PUT /v1/connectors/platform-connections/{id}/capabilities/{key}.
func (s *Service) setCapability(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	key, err := url.PathUnescape(chi.URLParam(r, "key"))
	if err != nil {
		return httpx.BadRequest("Invalid capability.")
	}
	var req capabilityReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Enabled == nil {
		return httpx.BadRequest("Send enabled.")
	}
	row, err := s.ownPlatform(ctx, s.d.Pool, id.UserID, connID, false)
	if err != nil {
		return err
	}
	p := s.providers[row.Provider]
	c, ok := s.capability(key)
	if !ok || p == nil || c.Provider != row.Provider {
		return httpx.NewError(http.StatusNotFound, "capability_not_found", "That capability isn't available for this account.")
	}
	if !*req.Enabled {
		if _, err := s.d.Pool.Exec(ctx, `UPDATE connector_platform_connections SET capabilities = array_remove(capabilities, $3),
			updated_at = now() WHERE id = $1 AND user_id = $2`, row.ID, id.UserID, c.Key); err != nil {
			return err
		}
		return s.writeConnection(w, r, id.UserID, row.ID, uuid.Nil)
	}
	orgID, err := parseOrgField(req.OrgID)
	if err != nil {
		return err
	}
	if err := s.checkPolicy(ctx, id.UserID, orgID, p, &c); err != nil {
		return err
	}
	if s.capabilityGranted(p, row, c) {
		if err := s.enableCapability(ctx, s.d.Pool, row.ID, c.Key); err != nil {
			return err
		}
		return s.writeConnection(w, r, id.UserID, row.ID, orgID)
	}
	if row.Kind == "token" || !p.oauthReady() {
		return httpx.NewError(http.StatusConflict, "platform_not_configured", p.Name+" sign-in isn't set up on this server, so more access can't be granted.")
	}
	u, err := s.startPlatformFlow(ctx, id.UserID, p, flowStart{Client: req.Client, Capability: &c, Target: row, ReturnTo: req.ReturnTo})
	if err != nil {
		return err
	}
	httpx.OK(w, PlatformChange{AuthorizeURL: u})
	return nil
}

func (s *Service) enableCapability(ctx context.Context, q db.Querier, connID uuid.UUID, key string) error {
	_, err := q.Exec(ctx, `UPDATE connector_platform_connections
		SET capabilities = array_append(array_remove(capabilities, $2), $2), updated_at = now()
		WHERE id = $1 AND cardinality(capabilities) < 200`, connID, key)
	return err
}

func parseOrgField(raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, httpx.BadRequest("org_id must be a uuid.")
	}
	return id, nil
}

// writeConnection answers with one connection as its owner sees it.
func (s *Service) writeConnection(w http.ResponseWriter, r *http.Request, userID, connID, orgID uuid.UUID) error {
	dto, err := s.platformConnectionDTO(r.Context(), userID, connID, orgID)
	if err != nil {
		return err
	}
	httpx.OK(w, PlatformChange{Connection: dto})
	return nil
}

func (s *Service) platformConnectionDTO(ctx context.Context, userID, connID, orgID uuid.UUID) (*PlatformConnection, error) {
	row, err := s.ownPlatform(ctx, s.d.Pool, userID, connID, false)
	if err != nil {
		return nil, err
	}
	pol, err := s.policiesFor(ctx, userID, orgID, false)
	if err != nil {
		return nil, err
	}
	dto := s.connectionDTO(s.providers[row.Provider], row, s.capabilities(), pol)
	return &dto, nil
}

// ---- disconnect ---------------------------------------------------------------

// disconnectPlatform is DELETE /v1/connectors/platform-connections/{id}:
// our tokens go at once; the platform is asked to revoke them. Revocation:
// revoked, pending (queued, retried), unsupported (the platform has no
// revocation: remove the app there), manual (a personal token: revoke it on
// the platform if nothing else uses it).
func (s *Service) disconnectPlatform(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	connID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var queued []uuid.UUID
	var kind, provider string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		row, err := s.ownPlatform(ctx, tx, id.UserID, connID, true)
		if err != nil {
			return err
		}
		kind, provider = row.Kind, row.Provider
		if queued, err = s.queuePlatformRevocations(ctx, tx, row); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM connector_platform_connections WHERE id = $1`, row.ID)
		return err
	})
	if err != nil {
		return err
	}
	out := Disconnected{Revocation: "unsupported"}
	p := s.providers[provider]
	switch {
	case kind == "token":
		out.Revocation = "manual"
	case p != nil && p.RevokeStyle != "" && len(queued) > 0:
		out.Revocation = "pending"
		if s.revokeNow(context.WithoutCancel(ctx), queued) == 0 {
			out.Revocation = "revoked"
		}
	}
	slog.InfoContext(ctx, "connectors: platform disconnected", "user_id", id.UserID, "provider", provider, "revocation", out.Revocation)
	httpx.OK(w, out)
	return nil
}

// queuePlatformRevocations queues an OAuth connection's tokens for
// revocation in the platform's style. Personal tokens are never revoked
// (people may use them elsewhere).
func (s *Service) queuePlatformRevocations(ctx context.Context, q db.Querier, row *platformRow) ([]uuid.UUID, error) {
	p := s.providers[row.Provider]
	if row.Kind != "oauth" || p == nil || p.RevokeStyle == "" || !p.oauthReady() {
		return nil, nil
	}
	access, _ := s.seal.open(row.AccessTokenEnc, aadPlatform(row.ID, "access"))
	refresh, _ := s.seal.open(row.RefreshTokenEnc, aadPlatform(row.ID, "refresh"))
	return s.queueTokens(ctx, q, p, row.UserID, row.BaseURL, access, refresh)
}

// queueTokens queues one grant's tokens (which ones: the platform's
// RevokeWhich) and returns the queued ids.
func (s *Service) queueTokens(ctx context.Context, q db.Querier, p *provider, userID uuid.UUID, base, access, refresh string) ([]uuid.UUID, error) {
	if p.RevokeStyle == "" || !p.oauthReady() {
		return nil, nil
	}
	endpoint := strings.ReplaceAll(p.expand(p.RevokeURL, base, "", nil), url.PathEscape("{token}"), "")
	endpoint = strings.ReplaceAll(endpoint, "{token}", "")
	method := "client_secret_post"
	if p.TokenAuth == "client_secret_basic" {
		method = "client_secret_basic"
	}
	client := oauthClient{ID: p.clientID, Secret: p.clientSecret, AuthMethod: method}
	var list []struct{ tok, hint string }
	switch p.RevokeWhich {
	case "access":
		list = append(list, struct{ tok, hint string }{access, "access_token"})
	case "both":
		list = append(list, struct{ tok, hint string }{refresh, "refresh_token"}, struct{ tok, hint string }{access, "access_token"})
	default: // refresh, falling back to access
		if refresh != "" {
			list = append(list, struct{ tok, hint string }{refresh, "refresh_token"})
		} else {
			list = append(list, struct{ tok, hint string }{access, "access_token"})
		}
	}
	uid := userID
	var ids []uuid.UUID
	for _, t := range list {
		id, err := s.enqueueStyledRevocation(ctx, q, &uid, endpoint, p.RevokeStyle, client, t.tok, t.hint)
		if err != nil {
			return nil, err
		}
		if id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// ---- personal tokens -------------------------------------------------------------

type platformTokenReq struct {
	Token      string `json:"token"`
	BaseURL    string `json:"base_url"`   // GitLab self-managed
	Capability string `json:"capability"` // turn this on right away
	OrgID      string `json:"org_id"`
}

// connectPlatformToken is POST /v1/connectors/platforms/{provider}/token:
// a personal or private token, checked against the platform (which names
// the account) before it is stored.
func (s *Service) connectPlatformToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	p := s.providers[chi.URLParam(r, "provider")]
	if p == nil {
		return httpx.NotFound("That platform isn't available.")
	}
	if p.Token == nil {
		return httpx.NewError(http.StatusUnprocessableEntity, "tokens_unsupported", p.Name+" doesn't offer personal tokens. Sign in instead.")
	}
	var req platformTokenReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return httpx.BadRequest("Paste the token first.")
	}
	if len(token) > 4096 || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return httpx.BadRequest("That doesn't look like a token: it has spaces or is too long.")
	}
	orgID, err := parseOrgField(req.OrgID)
	if err != nil {
		return err
	}
	var capPtr *app.Capability
	if req.Capability != "" {
		c, ok := s.capability(req.Capability)
		if !ok || c.Provider != p.ID {
			return httpx.NewError(http.StatusNotFound, "capability_not_found", "That capability isn't available for "+p.Name+".")
		}
		capPtr = &c
	}
	if err := s.checkPolicy(ctx, id.UserID, orgID, p, capPtr); err != nil {
		return err
	}
	base := ""
	if p.SelfManaged && strings.TrimSpace(req.BaseURL) != "" {
		u, err := s.guard.checkURL(strings.TrimSuffix(strings.TrimSpace(req.BaseURL), "/"))
		if err != nil || u.RawQuery != "" {
			return httpx.BadRequest("Enter the instance address, like https://gitlab.example.com.")
		}
		u.Path = strings.TrimSuffix(u.Path, "/")
		if b := u.String(); b != p.vars["base"] {
			base = b
		}
	}
	lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := s.fetchUserInfo(lctx, p, p.Token.UserInfo, token, base, "", nil)
	if err != nil {
		if err == errUnauthorized {
			return httpx.NewError(http.StatusUnprocessableEntity, "token_rejected", p.Name+" didn't accept that token. Check it and try again.")
		}
		if isBlocked(err) {
			return httpx.NewError(http.StatusBadGateway, app.ConnectionHostRefused, "That address isn't reachable from this server.")
		}
		slog.WarnContext(ctx, "connectors: token check failed", "provider", p.ID, "err", err)
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Couldn't reach "+p.Name+" to check the token. Try again shortly.")
	}
	scopes := s.tokenScopes(lctx, p, token, base, info)
	var connID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM connector_platform_connections WHERE user_id = $1`, id.UserID).Scan(&n); err != nil {
			return err
		}
		existing, err := scanPlatform(tx.QueryRow(ctx, `SELECT `+platformCols+` FROM connector_platform_connections pc
			WHERE pc.user_id = $1 AND pc.provider = $2 AND pc.base_url = $3 AND pc.account_id = $4 FOR UPDATE`,
			id.UserID, p.ID, base, info.AccountID))
		if err != nil && !isNoRows(err) {
			return err
		}
		if existing != nil {
			connID = existing.ID
			if _, err := s.queuePlatformRevocations(ctx, tx, existing); err != nil { // replacing an OAuth grant
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE connector_platform_connections SET kind = 'token', account = $2, status = 'active',
				access_token_enc = $3, refresh_token_enc = NULL, token_type = 'Bearer', expires_at = NULL, scopes = $4,
				last_error = '', last_error_at = NULL, connected_at = now(), updated_at = now() WHERE id = $1`,
				connID, info.Account, s.seal.seal(token, aadPlatform(connID, "access")), strings.Join(scopes, " "))
		} else {
			if n >= maxPlatformConnectionsPerUser {
				return httpx.NewError(http.StatusConflict, "connection_limit", "You can connect up to 50 accounts. Disconnect one first.")
			}
			connID = uuid.New()
			_, err = tx.Exec(ctx, `INSERT INTO connector_platform_connections (id, user_id, provider, kind, base_url, account, account_id,
				access_token_enc, scopes) VALUES ($1, $2, $3, 'token', $4, $5, $6, $7, $8)`,
				connID, id.UserID, p.ID, base, info.Account, info.AccountID, s.seal.seal(token, aadPlatform(connID, "access")),
				strings.Join(scopes, " "))
		}
		if err != nil {
			return err
		}
		if capPtr != nil {
			return s.enableCapability(ctx, tx, connID, capPtr.Key)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.kickUserRevocations(ctx, id.UserID)
	slog.InfoContext(ctx, "connectors: platform connected with a token", "user_id", id.UserID, "provider", p.ID)
	return s.writeConnection(w, r, id.UserID, connID, orgID)
}

// tokenScopes records what a personal token says it grants, when the
// platform tells (GitHub classic tokens, GitLab). Informational: personal
// tokens are checked by the platform on every call.
func (s *Service) tokenScopes(ctx context.Context, p *provider, token, base string, info *userInfoResult) []string {
	if p.Token.ScopeHeader != "" && info.Header != nil {
		if h := info.Header.Get(p.Token.ScopeHeader); h != "" {
			return splitGranted(h)
		}
	}
	if p.Token.ScopesURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.expand(p.Token.ScopesURL, base, "", nil), nil)
		if err != nil {
			return []string{}
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := s.tokenClient.Do(req)
		if err != nil {
			return []string{}
		}
		defer resp.Body.Close()
		var out struct {
			Scopes []string `json:"scopes"`
		}
		if resp.StatusCode == http.StatusOK && decodeJSON(resp.Body, &out) == nil {
			return out.Scopes
		}
	}
	return []string{}
}
