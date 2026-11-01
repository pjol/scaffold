package connectors

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Connecting with OAuth takes three requests:
//
//  1. POST /v1/connectors/{id}/connect (signed in) discovers the server,
//     resolves the client registration, stores a flow (PKCE verifier sealed,
//     state hashed, 10 minutes) and returns the authorization URL.
//  2. The provider sends the browser to GET /v1/connectors/oauth/callback.
//     The state is consumed once, the code is exchanged (PKCE, resource),
//     the tokens are sealed onto the flow, and the browser goes to the
//     settings screen with a one-time finish secret.
//  3. POST /v1/connectors/oauth/finish (signed in) with that secret moves
//     the tokens into the user's connection, but only when the signed-in
//     user is the one who started the flow. The secret exists only in the
//     browser that came back from the provider, and the session only in the
//     right account, so neither someone who started a flow and lured another
//     person through the provider's consent, nor someone who got hold of a
//     callback URL, can attach another person's account. A mismatch cancels
//     the flow and revokes what was issued.
//
// Pasted tokens (auth_type bearer) are checked against the server and
// stored in one request.

type connectReq struct {
	Client string `json:"client"` // web | mobile (OAuth connectors)
	Token  string `json:"token"`  // bearer connectors
}

// ConnectStart is the answer to POST /v1/connectors/{id}/connect: an
// authorization URL to open (OAuth), or the finished connection (pasted
// token).
type ConnectStart struct {
	AuthorizeURL string     `json:"authorize_url,omitempty"`
	Connector    *Connector `json:"connector,omitempty"`
}

func (s *Service) connect(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	connectorID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req connectReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	c, err := s.visibleConnector(ctx, s.d.Pool, id.UserID, connectorID)
	if err != nil {
		return err
	}
	if !s.available(c) {
		return httpx.NewError(http.StatusConflict, "connector_unavailable", c.Name+" is turned off on this server.")
	}
	if c.AuthType == "bearer" {
		dto, err := s.connectBearer(ctx, id.UserID, c, req.Token)
		if err != nil {
			return err
		}
		httpx.OK(w, ConnectStart{Connector: dto})
		return nil
	}

	client := req.Client
	if client == "" {
		client = "web"
	}
	if client != "web" && client != "mobile" {
		return httpx.BadRequest("client must be web or mobile.")
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sa, err := s.discover(dctx, c.ServerURL)
	if err != nil {
		return discoveryHTTPError(err)
	}
	if sa.NoAuth {
		return httpx.NewError(http.StatusUnprocessableEntity, "no_sign_in",
			c.Name+" doesn't ask for sign-in, so it can't act as you. Ask an admin to check the connector.")
	}
	cl, err := s.resolveClient(dctx, c, &sa.AS)
	if err != nil {
		return discoveryHTTPError(err)
	}
	scopes := requestedScopes(c.OAuthScopes, sa.Scopes, &sa.AS)
	state, verifier := randomToken(), randomToken()
	flowID := uuid.New()
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// One flow per user and connector: a new attempt replaces the last.
		if _, err := tx.Exec(ctx, `DELETE FROM connector_oauth_flows WHERE user_id = $1 AND connector_id = $2 AND status = 'pending'`,
			id.UserID, c.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO connector_oauth_flows (id, user_id, connector_id, client, state_hash, code_verifier_enc, redirect_uri,
			  resource, issuer, iss_supported, token_endpoint, revocation_endpoint, client_id, client_secret_enc,
			  token_auth_method, scopes, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
			flowID, id.UserID, c.ID, client, hashToken(state), s.seal.seal(verifier, aadFlow(flowID, "verifier")), s.redirectURI(),
			sa.Resource, sa.AS.Issuer, sa.AS.IssParameterSupported, sa.AS.TokenEndpoint, sa.AS.RevocationEndpoint,
			cl.ID, s.seal.seal(cl.Secret, aadFlow(flowID, "client_secret")), cl.AuthMethod, strings.Join(scopes, " "),
			time.Now().Add(flowTTL))
		return err
	})
	if err != nil {
		return err
	}
	u, err := authorizeURL(sa.AS.AuthorizationEndpoint, cl.ID, s.redirectURI(), state, verifier, sa.Resource, scopes)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "connectors: authorization started", "user_id", id.UserID, "connector", c.Slug,
		"registration", cl.Source, "client", client)
	httpx.OK(w, ConnectStart{AuthorizeURL: u})
	return nil
}

// discoveryHTTPError maps a discovery failure to an API error (502: the
// remote side is at fault; the message says what to do).
func discoveryHTTPError(err error) error {
	var de *discoveryError
	if errors.As(err, &de) {
		e := httpx.NewError(http.StatusBadGateway, "connector_"+de.Code, de.Message)
		return e
	}
	return err
}

// connectBearer checks a pasted token against the server and stores it.
func (s *Service) connectBearer(ctx context.Context, userID uuid.UUID, c *connectorRow, raw string) (*Connector, error) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return nil, httpx.BadRequest("Paste the token first.")
	}
	if len(token) > 4096 || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return nil, httpx.BadRequest("That doesn't look like a token: it has spaces or is too long.")
	}
	lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	tools, err := s.listWithToken(lctx, c, token)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			return nil, httpx.NewError(http.StatusUnprocessableEntity, "token_rejected", c.Name+" didn't accept that token. Check it and try again.")
		}
		return nil, remoteHTTPError(c, err)
	}
	var connID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		existing, err := s.connectionFor(ctx, tx, userID, c.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			connID = existing.ID
			if err := s.enqueueConnectionRevocations(ctx, tx, existing); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				UPDATE connector_connections SET status = 'active', access_token_enc = $2, refresh_token_enc = NULL,
				  token_type = 'Bearer', expires_at = NULL, scopes = '', resource = '', issuer = '', token_endpoint = '',
				  revocation_endpoint = '', client_id = '', client_secret_enc = NULL, token_auth_method = 'none',
				  last_error = '', last_error_at = NULL, connected_at = now(), updated_at = now()
				WHERE id = $1`, connID, s.seal.seal(token, aadConnection(connID, "access")))
			return err
		}
		connID = uuid.New()
		_, err = tx.Exec(ctx, `
			INSERT INTO connector_connections (id, user_id, connector_id, access_token_enc, writes_enabled)
			VALUES ($1, $2, $3, $4, $5)`,
			connID, userID, c.ID, s.seal.seal(token, aadConnection(connID, "access")), c.WritePolicy == "on")
		return err
	})
	if err != nil {
		return nil, err
	}
	s.tools.put(connID, tools)
	slog.InfoContext(ctx, "connectors: connected with a token", "user_id", userID, "connector", c.Slug, "tools", len(tools))
	return s.connectorWithConnection(ctx, userID, connID)
}

// connectionFor returns the user's connection to a connector (locked), or nil.
func (s *Service) connectionFor(ctx context.Context, q db.Querier, userID, connectorID uuid.UUID) (*connectionRow, error) {
	cc, err := scanConnection(q.QueryRow(ctx, `SELECT `+connectionCols+` FROM connector_connections cc
		WHERE cc.user_id = $1 AND cc.connector_id = $2 FOR UPDATE`, userID, connectorID))
	if isNoRows(err) {
		return nil, nil
	}
	return cc, err
}

func (s *Service) connectorWithConnection(ctx context.Context, userID, connID uuid.UUID) (*Connector, error) {
	cc, c, err := s.ownConnection(ctx, s.d.Pool, userID, connID, false)
	if err != nil {
		return nil, err
	}
	dto := s.connectorDTO(c, cc)
	return &dto, nil
}

// ---- callback ----------------------------------------------------------------

type flowRow struct {
	ID                 uuid.UUID
	UserID             uuid.UUID
	ConnectorID        uuid.UUID
	Client             string
	ExpiresAt          time.Time
	CodeVerifierEnc    []byte
	RedirectURI        string
	Resource           string
	Issuer             string
	IssSupported       bool
	TokenEndpoint      string
	RevocationEndpoint string
	ClientID           string
	ClientSecretEnc    []byte
	TokenAuthMethod    string
	Scopes             string
}

const flowCols = `id, user_id, connector_id, client, expires_at, code_verifier_enc, redirect_uri, resource, issuer,
	iss_supported, token_endpoint, revocation_endpoint, client_id, client_secret_enc, token_auth_method, scopes`

func scanFlow(row pgx.Row) (*flowRow, error) {
	var f flowRow
	err := row.Scan(&f.ID, &f.UserID, &f.ConnectorID, &f.Client, &f.ExpiresAt, &f.CodeVerifierEnc, &f.RedirectURI,
		&f.Resource, &f.Issuer, &f.IssSupported, &f.TokenEndpoint, &f.RevocationEndpoint, &f.ClientID,
		&f.ClientSecretEnc, &f.TokenAuthMethod, &f.Scopes)
	return &f, err
}

func (f *flowRow) client(s *Service) (oauthClient, error) {
	secret, err := s.seal.open(f.ClientSecretEnc, aadFlow(f.ID, "client_secret"))
	return oauthClient{ID: f.ClientID, Secret: secret, AuthMethod: f.TokenAuthMethod}, err
}

// callback is GET /v1/connectors/oauth/callback, where the provider sends
// the browser. It always answers with a redirect to the client that started
// the flow (web settings, or the app's scheme for the mobile auth session).
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 25*time.Second)
	defer cancel()
	q := r.URL.Query()
	state := q.Get("state")
	if state == "" || len(state) > 512 {
		s.finishRedirect(w, r, "web", "", "invalid_callback")
		return
	}
	f, err := scanFlow(s.d.Pool.QueryRow(ctx, `
		UPDATE connector_oauth_flows SET state_hash = NULL
		WHERE state_hash = $1 AND status = 'pending'
		RETURNING `+flowCols, hashToken(state)))
	if err != nil {
		if !isNoRows(err) {
			slog.ErrorContext(ctx, "connectors: loading flow failed", "err", err)
		}
		s.finishRedirect(w, r, "web", "", "expired")
		return
	}
	fail := func(code string) {
		_, _ = s.d.Pool.Exec(ctx, `DELETE FROM connector_oauth_flows WHERE id = $1`, f.ID)
		s.finishRedirect(w, r, f.Client, "", code)
	}
	if time.Now().After(f.ExpiresAt) {
		fail("expired")
		return
	}
	if e := q.Get("error"); e != "" {
		slog.InfoContext(ctx, "connectors: provider declined authorization", "user_id", f.UserID, "error", truncate(e, 60))
		if e == "access_denied" {
			fail("cancelled")
		} else {
			fail("provider_error")
		}
		return
	}
	code := q.Get("code")
	if code == "" {
		fail("invalid_callback")
		return
	}
	// RFC 9207: the issuer identifier, when the server sends it, must match.
	if iss := q.Get("iss"); (f.IssSupported && iss == "") || (iss != "" && strings.TrimSuffix(iss, "/") != strings.TrimSuffix(f.Issuer, "/")) {
		slog.WarnContext(ctx, "connectors: issuer mismatch in callback", "user_id", f.UserID, "issuer", f.Issuer)
		fail("issuer_mismatch")
		return
	}
	verifier, err := s.seal.open(f.CodeVerifierEnc, aadFlow(f.ID, "verifier"))
	if err != nil {
		slog.ErrorContext(ctx, "connectors: flow verifier unreadable", "flow_id", f.ID, "err", err)
		fail("exchange_failed")
		return
	}
	client, err := f.client(s)
	if err != nil {
		fail("exchange_failed")
		return
	}
	ts, err := s.tokenCall(ctx, tokenRequest{
		Endpoint: f.TokenEndpoint, Client: client, Resource: f.Resource, PrevScopes: f.Scopes,
		Form: url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {f.RedirectURI}, "code_verifier": {verifier}},
	})
	if err != nil {
		var te *tokenError
		if errors.As(err, &te) && te.Code == "invalid_client" {
			s.forgetDynamicClient(ctx, f.Issuer, f.ClientID)
		}
		slog.WarnContext(ctx, "connectors: code exchange failed", "user_id", f.UserID, "connector_id", f.ConnectorID, "err", err)
		fail("exchange_failed")
		return
	}
	secret := randomToken()
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE connector_oauth_flows SET status = 'authorized', finish_hash = $2, access_token_enc = $3, refresh_token_enc = $4,
		  token_type = $5, token_expires_at = $6, granted_scopes = $7, expires_at = $8
		WHERE id = $1`,
		f.ID, hashToken(secret), s.seal.seal(ts.AccessToken, aadFlow(f.ID, "access")), s.seal.seal(ts.RefreshToken, aadFlow(f.ID, "refresh")),
		ts.TokenType, ts.ExpiresAt, ts.Scope, time.Now().Add(flowTTL)); err != nil {
		slog.ErrorContext(ctx, "connectors: saving tokens failed", "flow_id", f.ID, "err", err)
		_ = s.revokeIssued(ctx, f.RevocationEndpoint, client, ts)
		fail("exchange_failed")
		return
	}
	s.finishRedirect(w, r, f.Client, secret, "")
}

// finishRedirect sends the browser back to the client: ?finish=<secret> on
// success, ?connect_error=<code> otherwise.
func (s *Service) finishRedirect(w http.ResponseWriter, r *http.Request, client, secret, errCode string) {
	target := mobileReturnURL
	if client != "mobile" {
		base := strings.TrimSuffix(s.d.Cfg.PublicWebURL, "/")
		if base == "" {
			// No web app configured (development without web): say it plainly.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if errCode != "" {
				http.Error(w, "Connecting failed ("+errCode+"). Close this window and try again.", http.StatusBadRequest)
				return
			}
			http.Error(w, "PUBLIC_WEB_URL is not set, so this window can't return to the app.", http.StatusInternalServerError)
			return
		}
		target = base + "/settings/connections"
	}
	v := url.Values{}
	if errCode != "" {
		v.Set("connect_error", errCode)
	} else {
		v.Set("finish", secret)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target+"?"+v.Encode(), http.StatusSeeOther)
}

// ---- finish ------------------------------------------------------------------

type finishReq struct {
	Secret string `json:"secret"`
}

func (s *Service) finish(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	var req finishReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Secret == "" || len(req.Secret) > 512 {
		return httpx.BadRequest("Send the finish secret from the return link.")
	}
	var connID uuid.UUID
	var mismatch bool
	var outcome error // a refusal decided inside the transaction, returned after it commits
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var accessEnc, refreshEnc []byte
		var tokenType, granted string
		var tokenExpires *time.Time
		f, err := scanFlowWithTokens(tx.QueryRow(ctx, `SELECT `+flowCols+`, access_token_enc, refresh_token_enc, token_type,
			token_expires_at, granted_scopes FROM connector_oauth_flows
			WHERE finish_hash = $1 AND status = 'authorized' FOR UPDATE`, hashToken(req.Secret)),
			&accessEnc, &refreshEnc, &tokenType, &tokenExpires, &granted)
		if isNoRows(err) {
			return httpx.NewError(http.StatusNotFound, "flow_not_found", "This sign-in link expired or was already used. Connect again.")
		}
		if err != nil {
			return err
		}
		access, err := s.seal.open(accessEnc, aadFlow(f.ID, "access"))
		if err != nil {
			return err
		}
		refresh, err := s.seal.open(refreshEnc, aadFlow(f.ID, "refresh"))
		if err != nil {
			return err
		}
		client, err := f.client(s)
		if err != nil {
			return err
		}
		drop := func() error {
			if err := s.enqueueRevocation(ctx, tx, &f.UserID, f.RevocationEndpoint, client, refresh, "refresh_token"); err != nil {
				return err
			}
			if err := s.enqueueRevocation(ctx, tx, &f.UserID, f.RevocationEndpoint, client, access, "access_token"); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `DELETE FROM connector_oauth_flows WHERE id = $1`, f.ID)
			return err
		}
		if f.UserID != id.UserID {
			slog.WarnContext(ctx, "connectors: finish by a different user; flow cancelled", "flow_id", f.ID,
				"started_by", f.UserID, "finished_by", id.UserID)
			mismatch = true
			return drop()
		}
		if time.Now().After(f.ExpiresAt) {
			outcome = errFlowGone
			return drop()
		}
		c, err := s.visibleConnector(ctx, tx, id.UserID, f.ConnectorID)
		if err != nil {
			return err
		}
		if !s.available(c) {
			outcome = httpx.NewError(http.StatusConflict, "connector_unavailable", c.Name+" is turned off on this server.")
			return drop()
		}

		existing, err := s.connectionFor(ctx, tx, id.UserID, c.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			connID = existing.ID
			if err := s.enqueueConnectionRevocations(ctx, tx, existing); err != nil {
				return err
			}
		} else {
			connID = uuid.New()
		}
		args := []any{connID, s.seal.seal(access, aadConnection(connID, "access")), s.seal.seal(refresh, aadConnection(connID, "refresh")),
			tokenType, tokenExpires, granted, f.Resource, f.Issuer, f.TokenEndpoint, f.RevocationEndpoint, f.ClientID,
			s.seal.seal(client.Secret, aadConnection(connID, "client_secret")), f.TokenAuthMethod}
		if existing != nil {
			_, err = tx.Exec(ctx, `
				UPDATE connector_connections SET status = 'active', access_token_enc = $2, refresh_token_enc = $3, token_type = $4,
				  expires_at = $5, scopes = $6, resource = $7, issuer = $8, token_endpoint = $9, revocation_endpoint = $10,
				  client_id = $11, client_secret_enc = $12, token_auth_method = $13, last_error = '', last_error_at = NULL,
				  connected_at = now(), updated_at = now()
				WHERE id = $1`, args...)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO connector_connections (id, access_token_enc, refresh_token_enc, token_type, expires_at, scopes, resource,
				  issuer, token_endpoint, revocation_endpoint, client_id, client_secret_enc, token_auth_method,
				  user_id, connector_id, writes_enabled)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
				append(args, id.UserID, c.ID, c.WritePolicy == "on")...)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM connector_oauth_flows WHERE id = $1`, f.ID)
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
	s.tools.forget(connID)
	s.kickUserRevocations(ctx, id.UserID)
	dto, err := s.connectorWithConnection(ctx, id.UserID, connID)
	if err != nil {
		return err
	}
	// Warm the tool list so the first assistant turn doesn't wait for it.
	bg.Go(ctx, "connectors.warm_tools", func(ctx context.Context) error {
		s.warmTools(ctx, id.UserID, connID)
		return nil
	})
	slog.InfoContext(ctx, "connectors: connected", "user_id", id.UserID, "connector", dto.Slug)
	httpx.OK(w, dto)
	return nil
}

var errFlowGone = httpx.NewError(http.StatusNotFound, "flow_not_found", "This sign-in link expired or was already used. Connect again.")

func scanFlowWithTokens(row pgx.Row, access, refresh *[]byte, tokenType *string, expires **time.Time, granted *string) (*flowRow, error) {
	var f flowRow
	err := row.Scan(&f.ID, &f.UserID, &f.ConnectorID, &f.Client, &f.ExpiresAt, &f.CodeVerifierEnc, &f.RedirectURI,
		&f.Resource, &f.Issuer, &f.IssSupported, &f.TokenEndpoint, &f.RevocationEndpoint, &f.ClientID,
		&f.ClientSecretEnc, &f.TokenAuthMethod, &f.Scopes, access, refresh, tokenType, expires, granted)
	return &f, err
}

// revokeIssued revokes tokens that were issued but couldn't be kept.
func (s *Service) revokeIssued(ctx context.Context, endpoint string, client oauthClient, ts *tokenSet) error {
	if endpoint == "" || ts == nil {
		return nil
	}
	if ts.RefreshToken != "" {
		_ = s.revokeToken(ctx, endpoint, client, ts.RefreshToken, "refresh_token")
	}
	return s.revokeToken(ctx, endpoint, client, ts.AccessToken, "access_token")
}

// clientMetadata serves our Client ID Metadata Document: authorization
// servers that support it fetch this URL instead of asking us to register.
func (s *Service) clientMetadata(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"client_id":                  s.clientMetadataURL(),
		"client_name":                appName,
		"redirect_uris":              []string{s.redirectURI()},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	if strings.HasPrefix(s.d.Cfg.PublicWebURL, "https://") {
		doc["client_uri"] = s.d.Cfg.PublicWebURL
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httpx.OK(w, doc)
}

// sweepFlows removes expired flows. Tokens issued to a flow nobody finished
// are queued for revocation first.
func (s *Service) sweepFlows(ctx context.Context) error {
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+flowCols+`, access_token_enc, refresh_token_enc FROM connector_oauth_flows
			WHERE expires_at < now() AND status = 'authorized' LIMIT 200 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		type issued struct {
			f               *flowRow
			access, refresh []byte
		}
		var list []issued
		for rows.Next() {
			var f flowRow
			var a, rf []byte
			if err := rows.Scan(&f.ID, &f.UserID, &f.ConnectorID, &f.Client, &f.ExpiresAt, &f.CodeVerifierEnc, &f.RedirectURI,
				&f.Resource, &f.Issuer, &f.IssSupported, &f.TokenEndpoint, &f.RevocationEndpoint, &f.ClientID,
				&f.ClientSecretEnc, &f.TokenAuthMethod, &f.Scopes, &a, &rf); err != nil {
				rows.Close()
				return err
			}
			list = append(list, issued{&f, a, rf})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, it := range list {
			client, err := it.f.client(s)
			if err == nil {
				access, _ := s.seal.open(it.access, aadFlow(it.f.ID, "access"))
				refresh, _ := s.seal.open(it.refresh, aadFlow(it.f.ID, "refresh"))
				if err := s.enqueueRevocation(ctx, tx, &it.f.UserID, it.f.RevocationEndpoint, client, refresh, "refresh_token"); err != nil {
					return err
				}
				if err := s.enqueueRevocation(ctx, tx, &it.f.UserID, it.f.RevocationEndpoint, client, access, "access_token"); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM connector_oauth_flows WHERE id = $1`, it.f.ID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM connector_oauth_flows WHERE expires_at < now() AND status = 'pending'`)
		return err
	})
}
