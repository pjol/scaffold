package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// app.Connections: features find this module with app.FindConnections and
// never see a token. For lists a person's accounts that power a capability
// (with status, so sidebars can say "Reconnect" or "Blocked"); Client hands
// back an *http.Client whose transport signs requests to the platform's API
// hosts only, refreshes before expiry (row-locked) and once more on a 401,
// backs off on 429/Retry-After, refuses every other host, and caps response
// sizes. Errors are *httpx.Error values with app.Connection* codes.

var _ app.Connections = (*Service)(nil)

const (
	apiMaxBody      = 64 << 20 // API and content responses
	gitMaxBody      = 1 << 30  // a git pack (repository import)
	rateLimitWait   = 8 * time.Second
	rateLimitTries  = 2
	platformTimeout = 10 * time.Minute
)

// For is app.Connections.For.
func (s *Service) For(ctx context.Context, userID, orgID uuid.UUID, capability string) ([]app.Connection, error) {
	c, ok := s.capability(capability)
	if !ok {
		return nil, fmt.Errorf("connectors: no installed module declares capability %q", capability)
	}
	p := s.providers[c.Provider]
	rows, err := s.userPlatforms(ctx, s.d.Pool, userID, c.Provider)
	if err != nil {
		return nil, err
	}
	pol, err := s.policiesFor(ctx, userID, orgID, true)
	if err != nil {
		return nil, err
	}
	out := []app.Connection{}
	for _, r := range rows {
		if !slices.Contains(r.Capabilities, c.Key) {
			continue
		}
		conn := s.appConnection(p, r)
		switch {
		case pol.blocks(p.ID, &c) != "":
			conn.Status = "blocked"
		case !s.capabilityGranted(p, r, c):
			conn.Status = "needs_reauth"
		}
		out = append(out, conn)
	}
	return out, nil
}

func (s *Service) appConnection(p *provider, r *platformRow) app.Connection {
	return app.Connection{ID: r.ID.String(), Provider: r.Provider, Platform: p.Name, Account: r.Account, AccountID: r.AccountID,
		Scopes: r.granted(), Status: r.Status, Endpoints: p.endpoints(r.BaseURL, r.InstanceURL)}
}

var _ app.ConnectionDirectory = (*Service)(nil)

// All is app.ConnectionDirectory: every platform account userID connected
// (status active or needs_reauth; workspace policy isn't applied here), in
// the order they were connected.
func (s *Service) All(ctx context.Context, userID uuid.UUID) ([]app.Connection, error) {
	rows, err := s.userPlatforms(ctx, s.d.Pool, userID, "")
	if err != nil {
		return nil, err
	}
	out := make([]app.Connection, 0, len(rows))
	for _, r := range rows {
		if p := s.providers[r.Provider]; p != nil {
			out = append(out, s.appConnection(p, r))
		}
	}
	return out, nil
}

// Offers is app.Connections.Offers: capabilities of module ("" = every
// module) the person could still connect or turn on, within orgID's policy.
func (s *Service) Offers(ctx context.Context, userID, orgID uuid.UUID, module string) ([]app.ConnectionOffer, error) {
	rows, err := s.userPlatforms(ctx, s.d.Pool, userID, "")
	if err != nil {
		return nil, err
	}
	pol, err := s.policiesFor(ctx, userID, orgID, true)
	if err != nil {
		return nil, err
	}
	out := []app.ConnectionOffer{}
	for _, c := range s.capabilities() {
		if module != "" && c.Module != module {
			continue
		}
		p := s.providers[c.Provider]
		if !p.available() || pol.blocks(p.ID, &c) != "" {
			continue
		}
		on := false
		for _, r := range rows {
			if r.Provider == c.Provider && r.Status == "active" && slices.Contains(r.Capabilities, c.Key) && s.capabilityGranted(p, r, c) {
				on = true
				break
			}
		}
		if !on {
			out = append(out, app.ConnectionOffer{Capability: c.Key, Provider: c.Provider, Label: c.Label,
				ConnectURL: "/settings/connections?capability=" + url.QueryEscape(c.Key)})
		}
	}
	return out, nil
}

// Client is app.Connections.Client.
func (s *Service) Client(ctx context.Context, userID uuid.UUID, connectionID, capability string) (*http.Client, app.Connection, error) {
	notFound := httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "That connection doesn't exist.")
	connID, err := uuid.Parse(connectionID)
	if err != nil {
		return nil, app.Connection{}, notFound
	}
	r, err := s.ownPlatform(ctx, s.d.Pool, userID, connID, false)
	if err != nil {
		return nil, app.Connection{}, err
	}
	p := s.providers[r.Provider]
	c, ok := s.capability(capability)
	if !ok || p == nil || c.Provider != r.Provider {
		return nil, app.Connection{}, httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing, "That account can't be used for this.")
	}
	conn := s.appConnection(p, r)
	orgID, _ := app.ConnectionOrg(ctx)
	pol, err := s.policiesFor(ctx, userID, orgID, true)
	if err != nil {
		return nil, conn, err
	}
	if reason := pol.blocks(p.ID, &c); reason != "" {
		conn.Status = "blocked"
		return nil, conn, httpx.NewError(http.StatusForbidden, app.ConnectionBlocked, reason)
	}
	if r.Status != "active" {
		return nil, conn, needsAuth(p, r)
	}
	if !slices.Contains(r.Capabilities, c.Key) || !s.capabilityGranted(p, r, c) {
		return nil, conn, httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing,
			"Turn on "+c.Label+" for "+r.Account+" in Settings → Connections.")
	}
	t := &platformTransport{s: s, p: p, row: r, base: s.platformBase,
		api: p.rules(p.API, r.BaseURL, r.InstanceURL), content: p.rules(p.Content, r.BaseURL, r.InstanceURL)}
	if p.Git != nil && hasAny(p, c.Scopes, p.Git.Scopes) {
		if web, err := url.Parse(p.expand(p.Endpoints[p.Git.Web], r.BaseURL, r.InstanceURL, nil)); err == nil && web.Host != "" {
			t.git = &hostRule{Host: strings.ToLower(web.Host), Prefix: strings.TrimSuffix(web.Path, "/")}
		}
	}
	s.touchPlatform(ctx, r)
	hc := &http.Client{
		Transport: t,
		Timeout:   platformTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("connectors: too many redirects")
			}
			return nil // every hop goes through the transport's host rules
		},
	}
	return hc, conn, nil
}

func hasAny(p *provider, have, want []string) bool {
	set := p.grantedSet(have)
	for _, w := range want {
		if set[p.fold(w)] {
			return true
		}
	}
	return false
}

func needsAuth(p *provider, r *platformRow) error {
	msg := p.Name + " needs you to sign in again for " + r.Account + ". Reconnect it in Settings → Connections."
	if r.Kind == "token" {
		msg = p.Name + " stopped accepting the token for " + r.Account + ". Add a new one in Settings → Connections."
	}
	return httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, msg)
}

func (s *Service) touchPlatform(ctx context.Context, r *platformRow) {
	if r.LastUsedAt != nil && time.Since(*r.LastUsedAt) < time.Minute {
		return
	}
	_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE connector_platform_connections SET last_used_at = now() WHERE id = $1`, r.ID)
}

// ---- transport ---------------------------------------------------------------

// gitPath matches the two smart-HTTP fetch endpoints under a repository.
var gitPath = regexp.MustCompile(`^/.+\.git/(info/refs|git-upload-pack)$`)

type platformTransport struct {
	s       *Service
	p       *provider
	base    http.RoundTripper
	api     []hostRule
	content []hostRule
	git     *hostRule

	mu  sync.Mutex
	row *platformRow
}

type requestMode int

const (
	modeRefused requestMode = iota
	modeAPI
	modeContent
	modeGit
)

func (t *platformTransport) classify(req *http.Request) requestMode {
	u := req.URL
	if u.Scheme != "https" && !(u.Scheme == "http" && t.s.d.Cfg.IsDevelopment()) {
		return modeRefused
	}
	if t.git != nil && strings.EqualFold(u.Host, t.git.Host) && strings.HasPrefix(u.Path, t.git.Prefix+"/") {
		rest := strings.TrimPrefix(u.Path, t.git.Prefix)
		if m := gitPath.FindStringSubmatch(rest); m != nil {
			if m[1] == "info/refs" && req.Method == http.MethodGet && u.Query().Get("service") == "git-upload-pack" {
				return modeGit
			}
			if m[1] == "git-upload-pack" && req.Method == http.MethodPost {
				return modeGit
			}
			return modeRefused // never git-receive-pack or anything else on the web host
		}
	}
	for _, r := range t.api {
		if r.match(u.Host, u.Path) {
			return modeAPI
		}
	}
	for _, r := range t.content {
		if r.match(u.Host, u.Path) {
			return modeContent
		}
	}
	return modeRefused
}

func (t *platformTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	mode := t.classify(req)
	if mode == modeRefused {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, httpx.NewError(http.StatusForbidden, app.ConnectionHostRefused,
			fmt.Sprintf("Requests through %s may only go to %s's API (refused %s).", t.p.Name, t.p.Name, req.URL.Host))
	}
	ctx := req.Context()
	req = req.Clone(ctx) // RoundTrip must not modify the caller's request
	if err := makeReplayable(req); err != nil {
		return nil, err
	}
	token := ""
	if mode != modeContent {
		var err error
		if token, err = t.token(ctx, false, ""); err != nil {
			return nil, err
		}
	}
	retried401, waits := false, 0
	for {
		r := req.Clone(ctx)
		if req.Body != nil && req.Body != http.NoBody && req.GetBody != nil && (retried401 || waits > 0) {
			b, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			r.Body = b
		}
		switch mode {
		case modeAPI:
			r.Header.Set("Authorization", "Bearer "+token)
			for k, v := range t.p.DefaultHeaders {
				if r.Header.Get(k) == "" {
					r.Header.Set(k, v)
				}
			}
		case modeGit:
			r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(t.p.Git.User+":"+token)))
		}
		if r.Header.Get("User-Agent") == "" {
			r.Header.Set("User-Agent", appName)
		}
		resp, err := t.base.RoundTrip(r)
		if err != nil {
			if isBlocked(err) {
				return nil, httpx.NewError(http.StatusForbidden, app.ConnectionHostRefused, t.p.Name+" is at an address this server isn't allowed to reach.")
			}
			return nil, err
		}
		replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
		if resp.StatusCode == http.StatusUnauthorized && mode != modeContent {
			drain(resp)
			if retried401 {
				t.s.markPlatformNeedsReauth(ctx, t.row.ID, t.p.Name+" refused the sign-in. Reconnect it.")
				return nil, needsAuth(t.p, t.row)
			}
			if !replayable {
				// A streamed body can't be sent twice: renew the sign-in for the next try.
				if _, err := t.token(ctx, true, token); err != nil {
					return nil, err
				}
				return nil, httpx.NewError(http.StatusServiceUnavailable, app.ConnectionUnavailable, t.p.Name+" asked for a fresh sign-in, which is done. Try again.")
			}
			retried401 = true
			if token, err = t.token(ctx, true, token); err != nil {
				return nil, err
			}
			continue
		}
		if wait, limited := rateLimited(resp); limited {
			if waits < rateLimitTries && wait <= rateLimitWait && replayable {
				drain(resp)
				waits++
				if wait <= 0 {
					wait = time.Duration(waits) * 500 * time.Millisecond
				}
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				continue
			}
			drain(resp)
			msg := t.p.Name + " is limiting requests. Try again shortly."
			if wait > 0 {
				msg = fmt.Sprintf("%s is limiting requests. Try again in %d seconds.", t.p.Name, int(wait.Round(time.Second).Seconds())+0)
			}
			return nil, httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, msg)
		}
		limit := int64(apiMaxBody)
		if mode == modeGit {
			limit = gitMaxBody
		}
		if resp.ContentLength > limit {
			drain(resp)
			return nil, tooLarge(t.p)
		}
		resp.Body = &platformBody{rc: resp.Body, left: limit, p: t.p}
		return resp, nil
	}
}

// replayBuffer is the largest request body kept in memory so a request can
// be sent again after a refresh or a rate-limit wait.
const replayBuffer = 8 << 20

// makeReplayable gives a request with a body but no GetBody one, when the
// body fits replayBuffer; a larger body streams once (no retries).
func makeReplayable(req *http.Request) error {
	if req.Body == nil || req.Body == http.NoBody || req.GetBody != nil {
		return nil
	}
	if req.ContentLength > replayBuffer {
		return nil
	}
	buf, err := io.ReadAll(io.LimitReader(req.Body, replayBuffer+1))
	if err != nil {
		return err
	}
	if len(buf) > replayBuffer {
		req.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(buf), req.Body), req.Body}
		return nil
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(buf))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf)), nil }
	req.ContentLength = int64(len(buf))
	return nil
}

// rateLimited reads 429s, 503s with Retry-After, and GitHub's 403 with no
// requests left, and how long to wait (0 = unknown).
func rateLimited(resp *http.Response) (time.Duration, bool) {
	h := resp.Header
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
	case resp.StatusCode == http.StatusServiceUnavailable && h.Get("Retry-After") != "":
	case resp.StatusCode == http.StatusForbidden && h.Get("X-RateLimit-Remaining") == "0":
	default:
		return 0, false
	}
	if ra := strings.TrimSpace(h.Get("Retry-After")); ra != "" {
		if secs, err := strconv.Atoi(ra); err == nil {
			return time.Duration(secs) * time.Second, true
		}
		if t, err := http.ParseTime(ra); err == nil {
			return max(0, time.Until(t)), true
		}
	}
	if reset := h.Get("X-RateLimit-Reset"); reset != "" {
		if epoch, err := strconv.ParseInt(reset, 10, 64); err == nil && epoch > 1e9 {
			return max(0, time.Until(time.Unix(epoch, 0))), true
		}
	}
	return 0, true
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

func tooLarge(p *provider) error {
	return httpx.NewError(http.StatusBadGateway, app.ConnectionTooLarge, p.Name+" sent a response that is too large.")
}

// platformBody fails with ConnectionTooLarge past the cap.
type platformBody struct {
	rc   io.ReadCloser
	left int64
	p    *provider
}

func (b *platformBody) Read(buf []byte) (int, error) {
	if b.left <= 0 {
		var one [1]byte
		if n, _ := b.rc.Read(one[:]); n > 0 {
			return 0, tooLarge(b.p)
		}
		return 0, io.EOF
	}
	if int64(len(buf)) > b.left {
		buf = buf[:b.left]
	}
	n, err := b.rc.Read(buf)
	b.left -= int64(n)
	return n, err
}

func (b *platformBody) Close() error { return b.rc.Close() }

// token returns a usable access token, refreshing within a minute of expiry
// (or, with force, after a 401 for stale).
func (t *platformTransport) token(ctx context.Context, force bool, stale string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.row
	tok, err := t.s.seal.open(r.AccessTokenEnc, aadPlatform(r.ID, "access"))
	if err != nil {
		return "", err
	}
	near := r.ExpiresAt != nil && time.Until(*r.ExpiresAt) < refreshLeeway
	if !near && !force {
		return tok, nil
	}
	if r.Kind == "token" || len(r.RefreshTokenEnc) == 0 || !t.p.oauthReady() {
		msg := "The sign-in expired. Reconnect it."
		if r.Kind == "token" {
			msg = t.p.Name + " stopped accepting the token. Add a new one."
		}
		t.s.markPlatformNeedsReauth(ctx, r.ID, msg)
		return "", needsAuth(t.p, r)
	}
	fresh, err := t.s.refreshPlatform(ctx, t.p, r.ID, r.UserID, force, stale)
	if err != nil {
		if errors.Is(err, errNeedsReauth) {
			return "", needsAuth(t.p, r)
		}
		slog.WarnContext(ctx, "connectors: platform refresh failed", "connection_id", r.ID, "provider", t.p.ID, "err", err)
		return "", httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Couldn't reach "+t.p.Name+" to renew the sign-in. Try again shortly.")
	}
	t.row = fresh
	return t.s.seal.open(fresh.AccessTokenEnc, aadPlatform(fresh.ID, "access"))
}

// refreshPlatform exchanges the refresh token under a row lock, so
// concurrent requests (on any instance) refresh once and share the result.
func (s *Service) refreshPlatform(ctx context.Context, p *provider, connID, userID uuid.UUID, force bool, stale string) (*platformRow, error) {
	unlock := s.lockConnection(connID)
	defer unlock()
	var out *platformRow
	var reauth bool
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		r, err := s.ownPlatform(ctx, tx, userID, connID, true)
		if err != nil {
			return err
		}
		if r.Status != "active" {
			reauth = true
			return nil
		}
		current, err := s.seal.open(r.AccessTokenEnc, aadPlatform(r.ID, "access"))
		if err != nil {
			return err
		}
		fresh := r.ExpiresAt == nil || time.Until(*r.ExpiresAt) >= refreshLeeway
		if fresh && (!force || current != stale) {
			out = r // someone refreshed while we waited
			return nil
		}
		refreshTok, err := s.seal.open(r.RefreshTokenEnc, aadPlatform(r.ID, "refresh"))
		if err != nil {
			return err
		}
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshTok}}
		if p.RefreshScope && r.Scopes != "" {
			form.Set("scope", r.Scopes)
		}
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		ts, err := s.platformTokenCall(rctx, p, r.BaseURL, form)
		if err != nil {
			if reauthNeeded(err) {
				slog.InfoContext(ctx, "connectors: platform refresh refused; needs a new sign-in", "connection_id", r.ID, "provider", p.ID, "err", err)
				reauth = true
				_, err := tx.Exec(ctx, `UPDATE connector_platform_connections SET status = 'needs_reauth',
					last_error = 'The sign-in expired or was revoked. Reconnect it.', last_error_at = now(), updated_at = now()
					WHERE id = $1`, r.ID)
				return err
			}
			return err
		}
		newRefresh := r.RefreshTokenEnc
		if ts.RefreshToken != "" { // rotated
			newRefresh = s.seal.seal(ts.RefreshToken, aadPlatform(r.ID, "refresh"))
		}
		scopes := r.Scopes
		if ts.Scope != "" {
			scopes = ts.Scope
		}
		instance := r.InstanceURL
		if p.Instance {
			if iu := strings.TrimSuffix(jsonString(ts.Extra, "instance_url"), "/"); iu != "" && p.checkInstanceURL(iu) == nil {
				instance = iu
			}
		}
		accessEnc := s.seal.seal(ts.AccessToken, aadPlatform(r.ID, "access"))
		if _, err := tx.Exec(ctx, `UPDATE connector_platform_connections SET access_token_enc = $2, refresh_token_enc = $3,
			token_type = $4, expires_at = $5, scopes = $6, instance_url = $7, last_error = '', last_error_at = NULL, updated_at = now()
			WHERE id = $1`, r.ID, accessEnc, newRefresh, ts.TokenType, ts.ExpiresAt, scopes, instance); err != nil {
			return err
		}
		r.AccessTokenEnc, r.RefreshTokenEnc, r.TokenType, r.ExpiresAt, r.Scopes, r.InstanceURL = accessEnc, newRefresh, ts.TokenType, ts.ExpiresAt, scopes, instance
		out = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	if reauth {
		return nil, errNeedsReauth
	}
	return out, nil
}
