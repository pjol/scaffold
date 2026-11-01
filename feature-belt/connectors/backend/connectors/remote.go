package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Talking to a connected server: the official Go SDK client over
// streamable HTTP, one short session per operation (initialize, then
// tools/list or tools/call, then close), with the user's token added by a
// round tripper on top of the guarded transport. Access tokens are
// refreshed shortly before they expire, and once more when the server
// answers 401; a refresh the authorization server refuses marks the
// connection needs_reauth.

const (
	maxRemoteTools   = 128      // per connection
	maxRemoteSchema  = 16 << 10 // bytes of input schema per tool
	maxResultText    = 256 << 10
	listTimeout      = 15 * time.Second
	refreshLeeway    = 60 * time.Second
	clientVersionTag = "1.0.0"
)

var (
	errUnauthorized = errors.New("connectors: the server refused the token")
	errNeedsReauth  = errors.New("connectors: the connection needs a new sign-in")
)

// remoteTool is a tool as the server lists it.
type remoteTool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	ReadOnlyHint bool            `json:"read_only_hint"`
}

// ---- token handling ----------------------------------------------------------

// accessToken returns a usable access token for cc, refreshing it when it
// expires within a minute (or always, with force, after a 401 for stale).
func (s *Service) accessToken(ctx context.Context, cc *connectionRow, force bool, stale string) (string, error) {
	tok, err := s.seal.open(cc.AccessTokenEnc, aadConnection(cc.ID, "access"))
	if err != nil {
		return "", err
	}
	near := cc.ExpiresAt != nil && time.Until(*cc.ExpiresAt) < refreshLeeway
	if !near && !force {
		return tok, nil
	}
	if cc.TokenEndpoint == "" || len(cc.RefreshTokenEnc) == 0 {
		if near {
			s.markNeedsReauth(ctx, cc.ID, "The sign-in expired. Connect again.")
			return "", errNeedsReauth
		}
		return "", errUnauthorized
	}
	return s.refresh(ctx, cc.ID, cc.UserID, force, stale)
}

// refresh exchanges the refresh token under a row lock, so concurrent calls
// (on any instance) refresh once and share the result.
func (s *Service) refresh(ctx context.Context, connID, userID uuid.UUID, force bool, stale string) (string, error) {
	unlock := s.lockConnection(connID)
	defer unlock()
	var out string
	var reauth bool
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cc, _, err := s.ownConnection(ctx, tx, userID, connID, true)
		if err != nil {
			return err
		}
		if cc.Status != "active" {
			reauth = true
			return nil
		}
		current, err := s.seal.open(cc.AccessTokenEnc, aadConnection(cc.ID, "access"))
		if err != nil {
			return err
		}
		fresh := cc.ExpiresAt == nil || time.Until(*cc.ExpiresAt) >= refreshLeeway
		if fresh && (!force || current != stale) {
			out = current // someone refreshed while we waited
			return nil
		}
		refreshTok, err := s.seal.open(cc.RefreshTokenEnc, aadConnection(cc.ID, "refresh"))
		if err != nil {
			return err
		}
		client, err := cc.client(s)
		if err != nil {
			return err
		}
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		ts, err := s.tokenCall(rctx, tokenRequest{Endpoint: cc.TokenEndpoint, Client: client, Resource: cc.Resource,
			PrevScopes: cc.Scopes, Form: url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshTok}}})
		if err != nil {
			if reauthNeeded(err) {
				slog.InfoContext(ctx, "connectors: refresh refused; needs a new sign-in", "connection_id", cc.ID, "err", err)
				reauth = true
				_, err := tx.Exec(ctx, `UPDATE connector_connections SET status = 'needs_reauth',
					last_error = 'The sign-in expired or was revoked. Connect again.', last_error_at = now(), updated_at = now()
					WHERE id = $1`, cc.ID)
				return err
			}
			return fmt.Errorf("connectors: refresh failed: %w", err)
		}
		newRefresh := cc.RefreshTokenEnc
		if ts.RefreshToken != "" { // rotated
			newRefresh = s.seal.seal(ts.RefreshToken, aadConnection(cc.ID, "refresh"))
		}
		if _, err := tx.Exec(ctx, `UPDATE connector_connections SET access_token_enc = $2, refresh_token_enc = $3,
			token_type = $4, expires_at = $5, scopes = $6, updated_at = now() WHERE id = $1`,
			cc.ID, s.seal.seal(ts.AccessToken, aadConnection(cc.ID, "access")), newRefresh, ts.TokenType, ts.ExpiresAt, ts.Scope); err != nil {
			return err
		}
		out = ts.AccessToken
		return nil
	})
	if err != nil {
		return "", err
	}
	if reauth {
		s.tools.forget(connID)
		return "", errNeedsReauth
	}
	return out, nil
}

// ---- sessions ----------------------------------------------------------------

// bearerTransport adds the token and notices a 401 from the server.
type bearerTransport struct {
	base         http.RoundTripper
	token        string
	unauthorized atomic.Bool
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+t.token)
	resp, err := t.base.RoundTrip(r)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		t.unauthorized.Store(true)
	}
	return resp, err
}

// session runs fn in an MCP session authenticated with token.
func (s *Service) session(ctx context.Context, serverURL, token string, fn func(*sdk.ClientSession) error) error {
	bt := &bearerTransport{base: s.mcpClient.Transport, token: token}
	hc := &http.Client{Transport: bt, Timeout: s.mcpClient.Timeout, CheckRedirect: s.mcpClient.CheckRedirect}
	client := sdk.NewClient(&sdk.Implementation{Name: appName, Version: clientVersionTag}, nil)
	cs, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint: serverURL, HTTPClient: hc, MaxRetries: -1, DisableStandaloneSSE: true, MaxEventSize: 4 << 20,
	}, nil)
	if err != nil {
		if bt.unauthorized.Load() {
			return errUnauthorized
		}
		return err
	}
	defer cs.Close()
	err = fn(cs)
	if err != nil && bt.unauthorized.Load() {
		return errUnauthorized
	}
	return err
}

// withConnection runs fn with a valid token for cc, refreshing once on 401.
func (s *Service) withConnection(ctx context.Context, c *connectorRow, cc *connectionRow, fn func(*sdk.ClientSession) error) error {
	tok, err := s.accessToken(ctx, cc, false, "")
	if err != nil {
		return err
	}
	err = s.session(ctx, c.ServerURL, tok, fn)
	if !errors.Is(err, errUnauthorized) {
		return err
	}
	tok2, err := s.accessToken(ctx, cc, true, tok)
	if errors.Is(err, errUnauthorized) {
		// A pasted token with nothing to refresh: the server stopped accepting it.
		s.markNeedsReauth(ctx, cc.ID, c.Name+" stopped accepting the token. Connect again with a new one.")
		return errNeedsReauth
	}
	if err != nil {
		return err
	}
	err = s.session(ctx, c.ServerURL, tok2, fn)
	if errors.Is(err, errUnauthorized) {
		s.markNeedsReauth(ctx, cc.ID, c.Name+" refused the sign-in. Connect again.")
		return errNeedsReauth
	}
	return err
}

// ---- tools/list --------------------------------------------------------------

func listAll(ctx context.Context, cs *sdk.ClientSession) ([]remoteTool, error) {
	var out []remoteTool
	cursor := ""
	for page := 0; page < 10 && len(out) < maxRemoteTools; page++ {
		res, err := cs.ListTools(ctx, &sdk.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, t := range res.Tools {
			if t == nil || t.Name == "" || len(out) >= maxRemoteTools {
				continue
			}
			// Capped as listed: the list is cached per connection for the life
			// of the process, and nothing downstream shows more (the assistant
			// takes 1000 runes, settings 400).
			rt := remoteTool{Name: truncate(t.Name, 128), Title: truncate(t.Title, 200), Description: truncate(t.Description, 2000)}
			if rt.Title == "" && t.Annotations != nil {
				rt.Title = truncate(t.Annotations.Title, 200)
			}
			if t.Annotations != nil {
				rt.ReadOnlyHint = t.Annotations.ReadOnlyHint
			}
			schema, err := json.Marshal(t.InputSchema)
			if err != nil || len(schema) > maxRemoteSchema {
				slog.WarnContext(ctx, "connectors: remote tool skipped (schema missing or too large)", "tool", rt.Name)
				continue
			}
			rt.InputSchema = schema
			out = append(out, rt)
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

// listWithToken lists a server's tools with a token that isn't stored yet
// (checking a pasted token).
func (s *Service) listWithToken(ctx context.Context, c *connectorRow, token string) ([]remoteTool, error) {
	var tools []remoteTool
	err := s.session(ctx, c.ServerURL, token, func(cs *sdk.ClientSession) error {
		var err error
		tools, err = listAll(ctx, cs)
		return err
	})
	return tools, err
}

// remoteTools returns a connection's tools from the cache, listing them
// from the server when stale (or refresh is set). Concurrent misses share
// one request.
func (s *Service) remoteTools(ctx context.Context, c *connectorRow, cc *connectionRow, refresh bool) ([]remoteTool, time.Time, error) {
	if !refresh {
		if tools, at, ok := s.tools.get(cc.ID, s.toolTTL); ok {
			return tools, at, nil
		}
	}
	s.tools.sweep(s.toolTTL)
	return s.tools.load(cc.ID, func() ([]remoteTool, error) {
		lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), listTimeout)
		defer cancel()
		var tools []remoteTool
		err := s.withConnection(lctx, c, cc, func(cs *sdk.ClientSession) error {
			var err error
			tools, err = listAll(lctx, cs)
			return err
		})
		if err != nil {
			if !errors.Is(err, errNeedsReauth) {
				s.noteError(ctx, cc.ID, "Couldn't reach "+c.Name+" at "+time.Now().UTC().Format("15:04")+" UTC.")
			}
			return nil, err
		}
		if cc.LastError != "" {
			s.noteError(ctx, cc.ID, "")
		}
		return tools, nil
	})
}

// warmTools fills the cache after a connect.
func (s *Service) warmTools(ctx context.Context, userID, connID uuid.UUID) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cc, c, err := s.ownConnection(ctx, s.d.Pool, userID, connID, false)
	if err != nil {
		return
	}
	if _, _, err := s.remoteTools(ctx, c, cc, true); err != nil {
		slog.InfoContext(ctx, "connectors: first tool list failed", "connection_id", connID, "err", err)
	}
}

// ---- tools/call --------------------------------------------------------------

// callRemote runs one remote tool and turns the result into data for the
// assistant. Failures are *httpx.Error values with honest codes.
func (s *Service) callRemote(ctx context.Context, c *connectorRow, cc *connectionRow, name string, input json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, s.toolTimeout)
	defer cancel()
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage(`{}`)
	}
	var res *sdk.CallToolResult
	err := s.withConnection(ctx, c, cc, func(cs *sdk.ClientSession) error {
		var err error
		res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: input})
		return err
	})
	if err != nil {
		return nil, remoteHTTPError(c, err)
	}
	s.touch(ctx, cc)
	return resultData(c, res)
}

// touch records last use, at most once a minute.
func (s *Service) touch(ctx context.Context, cc *connectionRow) {
	if cc.LastUsedAt != nil && time.Since(*cc.LastUsedAt) < time.Minute {
		return
	}
	_, _ = s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE connector_connections SET last_used_at = now() WHERE id = $1`, cc.ID)
}

// resultData shapes a CallToolResult: structured content when the server
// sent it, the text blocks joined, and a note for anything left out.
func resultData(c *connectorRow, res *sdk.CallToolResult) (any, error) {
	var text strings.Builder
	omitted := 0
	for _, block := range res.Content {
		switch b := block.(type) {
		case *sdk.TextContent:
			if text.Len() < maxResultText {
				if text.Len() > 0 {
					text.WriteString("\n\n")
				}
				text.WriteString(b.Text)
			}
		case *sdk.EmbeddedResource:
			if b.Resource != nil && b.Resource.Text != "" && text.Len() < maxResultText {
				if text.Len() > 0 {
					text.WriteString("\n\n")
				}
				text.WriteString(b.Resource.Text)
			} else {
				omitted++
			}
		default:
			omitted++
		}
	}
	t := text.String()
	if len(t) > maxResultText {
		t = t[:maxResultText]
	}
	if res.IsError {
		msg := strings.TrimSpace(t)
		if msg == "" {
			msg = "The tool reported an error without details."
		}
		return nil, httpx.NewError(http.StatusBadGateway, "remote_tool_error", c.Name+" returned an error: "+truncate(msg, 500))
	}
	out := map[string]any{}
	if res.StructuredContent != nil {
		out["structured"] = res.StructuredContent
	}
	if t != "" {
		out["text"] = t
	}
	if omitted > 0 {
		out["omitted"] = fmt.Sprintf("%d non-text items (images, audio, files) were left out.", omitted)
	}
	if len(out) == 0 {
		out["text"] = "(no content)"
	}
	return out, nil
}

// remoteHTTPError maps a remote failure to an API/tool error with a code
// clients and the model can act on.
func remoteHTTPError(c *connectorRow, err error) error {
	var apiErr *httpx.Error
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.Is(err, errNeedsReauth), errors.Is(err, errUnauthorized):
		return httpx.NewError(http.StatusConflict, "connector_needs_reauth",
			c.Name+" needs you to sign in again. Reconnect it in Settings → Connections.")
	case isBlocked(err):
		return httpx.NewError(http.StatusBadGateway, "connector_address_blocked",
			c.Name+" is at an address this server isn't allowed to reach.")
	case errors.Is(err, context.DeadlineExceeded):
		return httpx.NewError(http.StatusGatewayTimeout, "connector_timeout", c.Name+" took too long to answer.")
	case errors.Is(err, errTooLarge):
		return httpx.NewError(http.StatusBadGateway, "connector_response_too_large", c.Name+" sent a response that is too large.")
	case strings.Contains(err.Error(), "unknown tool") || strings.Contains(err.Error(), "not found"):
		return httpx.NewError(http.StatusNotFound, "remote_tool_not_found", c.Name+" no longer offers that tool.")
	}
	return httpx.NewError(http.StatusBadGateway, "connector_unavailable", "Couldn't reach "+c.Name+". Try again shortly.")
}

// ---- cache -------------------------------------------------------------------

type cachedTools struct {
	tools []remoteTool
	at    time.Time
}

type inflight struct {
	done  chan struct{}
	tools []remoteTool
	err   error
}

// toolCache keeps each connection's tool list for CONNECTORS_TOOL_CACHE_TTL
// on this instance.
type toolCache struct {
	mu      sync.Mutex
	entries map[uuid.UUID]cachedTools
	loading map[uuid.UUID]*inflight
	swept   time.Time
}

// sweep drops entries older than maxAge (at most once a minute), so lists
// of connections nobody uses any more don't stay in memory.
func (tc *toolCache) sweep(maxAge time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if time.Since(tc.swept) < time.Minute {
		return
	}
	tc.swept = time.Now()
	for id, e := range tc.entries {
		if time.Since(e.at) > maxAge {
			delete(tc.entries, id)
		}
	}
}

func newToolCache() *toolCache {
	return &toolCache{entries: map[uuid.UUID]cachedTools{}, loading: map[uuid.UUID]*inflight{}}
}

func (tc *toolCache) get(id uuid.UUID, ttl time.Duration) ([]remoteTool, time.Time, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	e, ok := tc.entries[id]
	if !ok || time.Since(e.at) > ttl {
		return nil, time.Time{}, false
	}
	return e.tools, e.at, true
}

func (tc *toolCache) put(id uuid.UUID, tools []remoteTool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.entries[id] = cachedTools{tools: tools, at: time.Now()}
}

func (tc *toolCache) forget(id uuid.UUID) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	delete(tc.entries, id)
}

// load runs fn once for concurrent callers of the same id and caches a
// success.
func (tc *toolCache) load(id uuid.UUID, fn func() ([]remoteTool, error)) ([]remoteTool, time.Time, error) {
	tc.mu.Lock()
	if f, ok := tc.loading[id]; ok {
		tc.mu.Unlock()
		<-f.done
		return f.tools, time.Now(), f.err
	}
	f := &inflight{done: make(chan struct{})}
	tc.loading[id] = f
	tc.mu.Unlock()

	f.tools, f.err = fn()
	tc.mu.Lock()
	delete(tc.loading, id)
	if f.err == nil {
		tc.entries[id] = cachedTools{tools: f.tools, at: time.Now()}
	}
	tc.mu.Unlock()
	close(f.done)
	return f.tools, time.Now(), f.err
}
