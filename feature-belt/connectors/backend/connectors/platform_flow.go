package connectors

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Platform sign-in takes the same three steps as an MCP sign-in (flow.go):
// start (signed in) stores a flow with a hashed single-use state and a
// sealed PKCE verifier; the platform sends the browser to the shared
// callback, which exchanges the code and names the account; finish (signed
// in, the same person, with the one-time secret only the returning browser
// holds) moves the tokens into the connection. The callback URL is the one
// MCP sign-ins use, so a deployment registers one redirect URL everywhere.

type flowStart struct {
	Client     string
	Capability *app.Capability
	Target     *platformRow // the account to extend (incremental consent, reconnect)
	ReturnTo   string
	OrgID      uuid.UUID
}

type platformConnectReq struct {
	Client       string `json:"client"`
	Capability   string `json:"capability"`
	ConnectionID string `json:"connection_id"` // extend or reconnect this account
	ReturnTo     string `json:"return_to"`
	OrgID        string `json:"org_id"`
}

// connectPlatform is POST /v1/connectors/platforms/{provider}/connect:
// sign in to a new account, or (connection_id) reconnect or grant more on
// an existing one. A capability already granted on that account is just
// turned on.
func (s *Service) connectPlatform(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	p := s.providers[chi.URLParam(r, "provider")]
	if p == nil {
		return httpx.NotFound("That platform isn't available.")
	}
	var req platformConnectReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !p.oauthReady() {
		msg := p.Name + " sign-in isn't set up on this server yet."
		if p.Token != nil {
			msg += " Use a " + strings.ToLower(p.Token.Label) + " instead."
		}
		return httpx.NewError(http.StatusConflict, "platform_not_configured", msg)
	}
	orgID, err := parseOrgField(req.OrgID)
	if err != nil {
		return err
	}
	fs := flowStart{Client: req.Client, ReturnTo: req.ReturnTo, OrgID: orgID}
	if req.Capability != "" {
		c, ok := s.capability(req.Capability)
		if !ok || c.Provider != p.ID {
			return httpx.NewError(http.StatusNotFound, "capability_not_found", "That capability isn't available for "+p.Name+".")
		}
		fs.Capability = &c
	}
	if err := s.checkPolicy(ctx, id.UserID, orgID, p, fs.Capability); err != nil {
		return err
	}
	if req.ConnectionID != "" {
		cid, err := uuid.Parse(req.ConnectionID)
		if err != nil {
			return httpx.BadRequest("connection_id must be a uuid.")
		}
		row, err := s.ownPlatform(ctx, s.d.Pool, id.UserID, cid, false)
		if err != nil {
			return err
		}
		if row.Provider != p.ID {
			return httpx.BadRequest("That connection is on another platform.")
		}
		if row.Kind == "token" && row.BaseURL != "" {
			return httpx.BadRequest("Sign-in works with " + hostOf(p.vars["base"]) + " only; replace the token for " + hostOf(row.BaseURL) + " instead.")
		}
		fs.Target = row
		if fs.Capability != nil && row.Status == "active" && row.Kind == "oauth" && s.capabilityGranted(p, row, *fs.Capability) {
			if err := s.enableCapability(ctx, s.d.Pool, row.ID, fs.Capability.Key); err != nil {
				return err
			}
			return s.writeConnection(w, r, id.UserID, row.ID, orgID)
		}
	}
	u, err := s.startPlatformFlow(ctx, id.UserID, p, fs)
	if err != nil {
		return err
	}
	httpx.OK(w, PlatformChange{AuthorizeURL: u})
	return nil
}

// safeReturnTo keeps an app-relative path ("/files?x=1"), or "".
func safeReturnTo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 512 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	return raw
}

// startPlatformFlow stores a flow and returns the authorization URL.
func (s *Service) startPlatformFlow(ctx context.Context, userID uuid.UUID, p *provider, fs flowStart) (string, error) {
	client := fs.Client
	if client == "" {
		client = "web"
	}
	if client != "web" && client != "mobile" {
		return "", httpx.BadRequest("client must be web or mobile.")
	}
	var granted, need []string
	var capKey string
	if fs.Target != nil && fs.Target.Kind == "oauth" && fs.Target.Status == "active" {
		granted = fs.Target.granted()
	}
	if fs.Target != nil {
		// Reconnecting keeps every capability the account had working.
		for _, c := range s.capabilities() {
			if c.Provider == p.ID && slices.Contains(fs.Target.Capabilities, c.Key) {
				need = unionScopes(need, c.Scopes)
			}
		}
		if p.Incremental && fs.Target.Status == "active" {
			need = p.missingScopes(granted, need)
		}
	}
	if fs.Capability != nil {
		capKey = fs.Capability.Key
		need = unionScopes(need, p.missingScopes(granted, fs.Capability.Scopes))
	}
	scopes := p.requestScopes(granted, need)
	state, verifier := randomToken(), ""
	if p.PKCE {
		verifier = randomToken()
	}
	flowID := uuid.New()
	var target *uuid.UUID
	base := ""
	if fs.Target != nil {
		target = &fs.Target.ID
		base = fs.Target.BaseURL
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// One pending sign-in per person and platform: a new attempt replaces the last.
		if _, err := tx.Exec(ctx, `DELETE FROM connector_platform_flows WHERE user_id = $1 AND provider = $2 AND status = 'pending'`,
			userID, p.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO connector_platform_flows (id, user_id, provider, connection_id, capability, client,
			state_hash, code_verifier_enc, redirect_uri, base_url, scopes, return_to, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			flowID, userID, p.ID, target, capKey, client, hashToken(state), s.seal.seal(verifier, aadPlatformFlow(flowID, "verifier")),
			s.redirectURI(), base, strings.Join(scopes, " "), safeReturnTo(fs.ReturnTo), time.Now().Add(flowTTL))
		return err
	})
	if err != nil {
		return "", err
	}
	u, err := url.Parse(p.expand(p.AuthorizeURL, base, "", nil))
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.clientID)
	q.Set("redirect_uri", s.redirectURI())
	q.Set("state", state)
	if p.ScopeParam && len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	if verifier != "" {
		q.Set("code_challenge", pkceChallenge(verifier))
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range p.AuthParams {
		q.Set(k, v)
	}
	// Which account: the one being extended, or a choice for a new one.
	switch p.ID {
	case "google":
		if fs.Target != nil {
			q.Set("login_hint", fs.Target.Account)
			q.Set("prompt", "consent")
		} else {
			q.Set("prompt", "select_account consent")
		}
	case "microsoft":
		if fs.Target != nil {
			q.Set("login_hint", fs.Target.Account)
		} else {
			q.Set("prompt", "select_account")
		}
	case "github":
		if fs.Target != nil {
			q.Set("login", fs.Target.Account)
		}
	}
	u.RawQuery = q.Encode()
	slog.InfoContext(ctx, "connectors: platform sign-in started", "user_id", userID, "provider", p.ID,
		"capability", capKey, "extend", target != nil, "client", client)
	return u.String(), nil
}

// ---- callback ----------------------------------------------------------------

type platformFlowRow struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Provider     string
	ConnectionID *uuid.UUID
	Capability   string
	Client       string
	VerifierEnc  []byte
	RedirectURI  string
	BaseURL      string
	Scopes       string
	ReturnTo     string
	ExpiresAt    time.Time
}

const platformFlowCols = `id, user_id, provider, connection_id, capability, client, code_verifier_enc, redirect_uri, base_url,
	scopes, return_to, expires_at`

func scanPlatformFlow(row pgx.Row, extra ...any) (*platformFlowRow, error) {
	var f platformFlowRow
	dst := append([]any{&f.ID, &f.UserID, &f.Provider, &f.ConnectionID, &f.Capability, &f.Client, &f.VerifierEnc,
		&f.RedirectURI, &f.BaseURL, &f.Scopes, &f.ReturnTo, &f.ExpiresAt}, extra...)
	return &f, row.Scan(dst...)
}

// platformCallback handles the callback when state belongs to a platform
// flow. handled=false: not ours (an MCP sign-in).
func (s *Service) platformCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, state string) bool {
	f, err := scanPlatformFlow(s.d.Pool.QueryRow(ctx, `UPDATE connector_platform_flows SET state_hash = NULL
		WHERE state_hash = $1 AND status = 'pending' RETURNING `+platformFlowCols, hashToken(state)))
	if err != nil {
		if !isNoRows(err) {
			slog.ErrorContext(ctx, "connectors: loading platform flow failed", "err", err)
		}
		return false
	}
	fail := func(code string) {
		_, _ = s.d.Pool.Exec(ctx, `DELETE FROM connector_platform_flows WHERE id = $1`, f.ID)
		s.platformRedirect(w, r, f.Client, "", code)
	}
	p := s.providers[f.Provider]
	q := r.URL.Query()
	switch {
	case p == nil || !p.oauthReady():
		fail("provider_error")
		return true
	case time.Now().After(f.ExpiresAt):
		fail("expired")
		return true
	case q.Get("error") != "":
		slog.InfoContext(ctx, "connectors: platform declined authorization", "user_id", f.UserID, "provider", f.Provider, "error", truncate(q.Get("error"), 60))
		if q.Get("error") == "access_denied" {
			fail("cancelled")
		} else {
			fail("provider_error")
		}
		return true
	case q.Get("code") == "":
		fail("invalid_callback")
		return true
	}
	verifier, err := s.seal.open(f.VerifierEnc, aadPlatformFlow(f.ID, "verifier"))
	if err != nil {
		fail("exchange_failed")
		return true
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {q.Get("code")}, "redirect_uri": {f.RedirectURI}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	ts, err := s.platformTokenCall(ctx, p, f.BaseURL, form)
	if err != nil {
		slog.WarnContext(ctx, "connectors: platform code exchange failed", "user_id", f.UserID, "provider", f.Provider, "err", err)
		fail("exchange_failed")
		return true
	}
	revoke := func() {
		if ids, err := s.queueTokens(ctx, s.d.Pool, p, f.UserID, f.BaseURL, ts.AccessToken, ts.RefreshToken); err == nil {
			s.revokeNow(ctx, ids)
		}
	}
	instance := ""
	if p.Instance {
		instance = strings.TrimSuffix(jsonString(ts.Extra, "instance_url"), "/")
		if err := p.checkInstanceURL(instance); err != nil {
			slog.WarnContext(ctx, "connectors: platform instance refused", "provider", f.Provider, "err", err)
			revoke()
			fail("provider_error")
			return true
		}
	}
	var account, accountID string
	if p.FromToken != nil {
		account, accountID = p.FromToken(ts.Extra)
	} else {
		info, err := s.fetchUserInfo(ctx, p, p.UserInfo, ts.AccessToken, f.BaseURL, instance, ts.Extra)
		if err != nil {
			slog.WarnContext(ctx, "connectors: platform user info failed", "provider", f.Provider, "err", err)
			revoke()
			fail("exchange_failed")
			return true
		}
		account, accountID = info.Account, info.AccountID
	}
	if accountID == "" {
		revoke()
		fail("exchange_failed")
		return true
	}
	if account == "" {
		account = accountID
	}
	granted := ts.Scope
	if granted == "" && p.ScopeParam {
		granted = f.Scopes // the platform didn't say: what was asked for
	}
	secret := randomToken()
	if _, err := s.d.Pool.Exec(ctx, `UPDATE connector_platform_flows SET status = 'authorized', finish_hash = $2,
		access_token_enc = $3, refresh_token_enc = $4, token_type = $5, token_expires_at = $6, granted_scopes = $7,
		instance_url = $8, account = $9, account_id = $10, expires_at = $11 WHERE id = $1`,
		f.ID, hashToken(secret), s.seal.seal(ts.AccessToken, aadPlatformFlow(f.ID, "access")),
		s.seal.seal(ts.RefreshToken, aadPlatformFlow(f.ID, "refresh")), ts.TokenType, ts.ExpiresAt, granted, instance,
		truncate(account, 200), truncate(accountID, 200), time.Now().Add(flowTTL)); err != nil {
		slog.ErrorContext(ctx, "connectors: saving platform tokens failed", "flow_id", f.ID, "err", err)
		revoke()
		fail("exchange_failed")
		return true
	}
	s.platformRedirect(w, r, f.Client, secret, "")
	return true
}

// platformRedirect sends the browser back: ?platform_finish=<secret> or
// ?connect_error=<code>.
func (s *Service) platformRedirect(w http.ResponseWriter, r *http.Request, client, secret, errCode string) {
	if secret == "" {
		s.finishRedirect(w, r, client, "", errCode)
		return
	}
	target := mobileReturnURL
	if client != "mobile" {
		base := strings.TrimSuffix(s.d.Cfg.PublicWebURL, "/")
		if base == "" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			http.Error(w, "PUBLIC_WEB_URL is not set, so this window can't return to the app.", http.StatusInternalServerError)
			return
		}
		target = base + "/settings/connections"
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target+"?"+url.Values{"platform_finish": {secret}}.Encode(), http.StatusSeeOther)
}

// ---- finish ------------------------------------------------------------------

// PlatformFinished answers POST /v1/connectors/platforms/finish.
type PlatformFinished struct {
	Connection *PlatformConnection `json:"connection"`
	// Capability turned on by this sign-in ("" when none was asked for).
	Capability string `json:"capability"`
	// CapabilityEnabled is false when the person declined part of the
	// consent, so the capability's scopes still aren't granted.
	CapabilityEnabled bool `json:"capability_enabled"`
	// OtherAccount: the person signed in to a different account than the
	// one being extended; it was connected as its own account.
	OtherAccount bool   `json:"other_account"`
	ReturnTo     string `json:"return_to"`
}

type platformFinishReq struct {
	Secret string `json:"secret"`
	OrgID  string `json:"org_id"`
}

func (s *Service) finishPlatform(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	var req platformFinishReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Secret == "" || len(req.Secret) > 512 {
		return httpx.BadRequest("Send the finish secret from the return link.")
	}
	orgID, err := parseOrgField(req.OrgID)
	if err != nil {
		return err
	}
	var out PlatformFinished
	var connID uuid.UUID
	var mismatch bool
	var outcome error
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var accessEnc, refreshEnc []byte
		var tokenType, granted, instance, account, accountID string
		var tokenExpires *time.Time
		f, err := scanPlatformFlow(tx.QueryRow(ctx, `SELECT `+platformFlowCols+`, access_token_enc, refresh_token_enc, token_type,
			token_expires_at, granted_scopes, instance_url, account, account_id FROM connector_platform_flows
			WHERE finish_hash = $1 AND status = 'authorized' FOR UPDATE`, hashToken(req.Secret)),
			&accessEnc, &refreshEnc, &tokenType, &tokenExpires, &granted, &instance, &account, &accountID)
		if isNoRows(err) {
			return errFlowGone
		}
		if err != nil {
			return err
		}
		access, err := s.seal.open(accessEnc, aadPlatformFlow(f.ID, "access"))
		if err != nil {
			return err
		}
		refresh, err := s.seal.open(refreshEnc, aadPlatformFlow(f.ID, "refresh"))
		if err != nil {
			return err
		}
		p := s.providers[f.Provider]
		drop := func() error {
			if p != nil {
				if _, err := s.queueTokens(ctx, tx, p, f.UserID, f.BaseURL, access, refresh); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `DELETE FROM connector_platform_flows WHERE id = $1`, f.ID)
			return err
		}
		if f.UserID != id.UserID {
			slog.WarnContext(ctx, "connectors: platform finish by a different user; flow cancelled", "flow_id", f.ID,
				"started_by", f.UserID, "finished_by", id.UserID)
			mismatch = true
			return drop()
		}
		if p == nil || time.Now().After(f.ExpiresAt) {
			outcome = errFlowGone
			return drop()
		}
		out.ReturnTo, out.Capability = f.ReturnTo, f.Capability
		existing, err := scanPlatform(tx.QueryRow(ctx, `SELECT `+platformCols+` FROM connector_platform_connections pc
			WHERE pc.user_id = $1 AND pc.provider = $2 AND pc.base_url = $3 AND pc.account_id = $4 FOR UPDATE`,
			id.UserID, p.ID, f.BaseURL, accountID))
		if err != nil && !isNoRows(err) {
			return err
		}
		out.OtherAccount = f.ConnectionID != nil && (existing == nil || existing.ID != *f.ConnectionID)
		grantedList := splitGranted(granted)
		if existing != nil {
			connID = existing.ID
			if existing.Kind == "oauth" && p.Incremental {
				// Incremental consent keeps earlier grants; some platforms list only the new ones.
				grantedList = unionScopes(existing.granted(), grantedList)
			}
			refreshEnc := s.seal.seal(refresh, aadPlatform(connID, "refresh"))
			if refresh == "" && existing.Kind == "oauth" {
				refreshEnc = existing.RefreshTokenEnc // the platform kept the earlier refresh token
			}
			_, err = tx.Exec(ctx, `UPDATE connector_platform_connections SET kind = 'oauth', account = $2, status = 'active',
				access_token_enc = $3, refresh_token_enc = $4, token_type = $5, expires_at = $6, scopes = $7, instance_url = $8,
				last_error = '', last_error_at = NULL, connected_at = now(), updated_at = now() WHERE id = $1`,
				connID, account, s.seal.seal(access, aadPlatform(connID, "access")), refreshEnc, tokenType, tokenExpires,
				strings.Join(grantedList, " "), instance)
		} else {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM connector_platform_connections WHERE user_id = $1`, id.UserID).Scan(&n); err != nil {
				return err
			}
			if n >= maxPlatformConnectionsPerUser {
				outcome = httpx.NewError(http.StatusConflict, "connection_limit", "You can connect up to 50 accounts. Disconnect one first.")
				return drop()
			}
			connID = uuid.New()
			_, err = tx.Exec(ctx, `INSERT INTO connector_platform_connections (id, user_id, provider, kind, base_url, instance_url,
				account, account_id, access_token_enc, refresh_token_enc, token_type, expires_at, scopes)
				VALUES ($1, $2, $3, 'oauth', $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				connID, id.UserID, p.ID, f.BaseURL, instance, account, accountID, s.seal.seal(access, aadPlatform(connID, "access")),
				s.seal.seal(refresh, aadPlatform(connID, "refresh")), tokenType, tokenExpires, strings.Join(grantedList, " "))
		}
		if err != nil {
			return err
		}
		if f.Capability != "" {
			if c, ok := s.capability(f.Capability); ok && (!p.ScopeParam || len(p.missingScopes(grantedList, c.Scopes)) == 0) {
				if err := s.enableCapability(ctx, tx, connID, c.Key); err != nil {
					return err
				}
				out.CapabilityEnabled = true
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM connector_platform_flows WHERE id = $1`, f.ID)
		return err
	})
	if err != nil {
		return err
	}
	if mismatch {
		s.kickRevocations(ctx)
		return httpx.NewError(http.StatusConflict, "connection_owner_mismatch",
			"This sign-in was started from another account, so it was cancelled. Connect again from your account.")
	}
	if outcome != nil {
		s.kickUserRevocations(ctx, id.UserID)
		return outcome
	}
	dto, err := s.platformConnectionDTO(ctx, id.UserID, connID, orgID)
	if err != nil {
		return err
	}
	out.Connection = dto
	slog.InfoContext(ctx, "connectors: platform connected", "user_id", id.UserID, "provider", dto.Provider,
		"capability", out.Capability, "enabled", out.CapabilityEnabled)
	httpx.OK(w, out)
	return nil
}

// sweepPlatformFlows removes expired platform sign-ins; tokens issued to
// one nobody finished are revoked.
func (s *Service) sweepPlatformFlows(ctx context.Context) error {
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+platformFlowCols+`, access_token_enc, refresh_token_enc FROM connector_platform_flows
			WHERE expires_at < now() AND status = 'authorized' LIMIT 200 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		type issued struct {
			f               *platformFlowRow
			access, refresh []byte
		}
		var list []issued
		for rows.Next() {
			var a, rf []byte
			f, err := scanPlatformFlow(rows, &a, &rf)
			if err != nil {
				rows.Close()
				return err
			}
			list = append(list, issued{f, a, rf})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, it := range list {
			if p := s.providers[it.f.Provider]; p != nil {
				access, _ := s.seal.open(it.access, aadPlatformFlow(it.f.ID, "access"))
				refresh, _ := s.seal.open(it.refresh, aadPlatformFlow(it.f.ID, "refresh"))
				if _, err := s.queueTokens(ctx, tx, p, it.f.UserID, it.f.BaseURL, access, refresh); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM connector_platform_flows WHERE id = $1`, it.f.ID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM connector_platform_flows WHERE expires_at < now() AND status = 'pending'`)
		return err
	})
}
