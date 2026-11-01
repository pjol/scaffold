// Package mcp serves the app as an MCP server for agents. Two ways in, both
// acting as one person with that person's permissions: personal API keys
// (read or read+write) and OAuth 2.1, so remote connectors (Claude, ChatGPT,
// Cursor, Claude Code) are added by URL and the person signs in. The
// stateless streamable-HTTP endpoint exposes every module's app.ToolProvider
// tools, run as the caller. Distilled from MOX (internal/mcpserver,
// internal/access) on the official Go SDK; OAuth from OLEARY/crm.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/legal"
)

// Version is reported to MCP clients and in the server card.
const Version = "1.1.0"

// toolTimeout bounds one tool call (the root group has no request timeout).
const toolTimeout = 15 * time.Second

// Service is the mcp module.
type Service struct {
	d            *app.Deps
	ratePerMin   int
	oauth        oauthConfig
	metadataHTTP *http.Client

	once        sync.Once
	readServer  *sdk.Server
	writeServer *sdk.Server
	tools       []exposedTool
	writeTools  map[string]bool
	handler     http.Handler
}

type exposedTool struct {
	tool    app.Tool
	schema  *jsonschema.Resolved
	feature string // the providing module's Name(): the agent-scope key
}

func New(d *app.Deps) *Service {
	s := &Service{
		d:          d,
		ratePerMin: max(1, config.Int("MCP_RATE_LIMIT_PER_MINUTE", 120)),
		oauth: oauthConfig{
			accessTTL:            config.Duration("MCP_OAUTH_ACCESS_TTL", time.Hour),
			refreshTTL:           config.Duration("MCP_OAUTH_REFRESH_TTL", 30*24*time.Hour),
			registrationsPerHour: max(1, config.Int("MCP_OAUTH_REGISTRATIONS_PER_HOUR", 60)),
			maxUnusedClients:     max(1, config.Int("MCP_OAUTH_MAX_UNUSED_CLIENTS", 5000)),
			metadataDocuments:    config.Bool("MCP_OAUTH_CLIENT_METADATA_ENABLED", true),
		},
		metadataHTTP: newMetadataClient(),
	}
	if s.oauth.accessTTL < time.Minute || s.oauth.accessTTL > 24*time.Hour {
		config.Problem("MCP_OAUTH_ACCESS_TTL must be between 1m and 24h (got %s)", s.oauth.accessTTL)
	}
	if s.oauth.refreshTTL < s.oauth.accessTTL || s.oauth.refreshTTL > 365*24*time.Hour {
		config.Problem("MCP_OAUTH_REFRESH_TTL must be at least MCP_OAUTH_ACCESS_TTL and at most 8760h (got %s)", s.oauth.refreshTTL)
	}
	// Denying a connection must work before the Terms are accepted; allowing
	// checks consent in the handler.
	legal.Exempt("/v1/mcp/oauth/decision")
	slog.Info("mcp: oauth", "enabled", s.oauthEnabled(), "client_metadata_documents", s.oauth.metadataDocuments)
	return s
}

func (s *Service) Name() string { return "mcp" }

// MountRoot serves, at the server root where clients look for them: the MCP
// endpoint, its server card, the OAuth discovery documents and the
// authorization server's endpoints. The root group applies the per-IP rate
// limit only; each handler authenticates what it needs.
func (s *Service) MountRoot(r chi.Router) {
	r.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.mcpHandler().ServeHTTP(w, r) }))
	r.Get("/.well-known/mcp.json", s.serverCard)
	r.Get("/.well-known/oauth-protected-resource", s.protectedResource)
	r.Get("/.well-known/oauth-protected-resource/mcp", s.protectedResource)
	r.Get("/.well-known/oauth-authorization-server", s.authorizationServer)
	r.With(httprate.LimitBy(s.oauth.registrationsPerHour, time.Hour, httpx.KeyByClientIP,
		httprate.WithLimitHandler(oauthRateLimited))).Post("/oauth/register", s.register)
	r.Get("/oauth/authorize", s.authorize)
	r.With(httprate.LimitBy(120, time.Minute, func(r *http.Request) (string, error) {
		// Per client: hosted connectors refresh for many people from shared IPs.
		r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
		if err := r.ParseForm(); err == nil {
			if id, _, ok := r.BasicAuth(); ok {
				return "oauth-token:" + id, nil
			}
			if id := r.PostForm.Get("client_id"); id != "" {
				return "oauth-token:" + id, nil
			}
		}
		return httpx.KeyByClientIP(r)
	}, httprate.WithLimitHandler(oauthRateLimited))).Post("/oauth/token", s.token)
	r.Post("/oauth/revoke", s.revoke)
}

func oauthRateLimited(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Retry-After", "60")
	writeOAuthError(w, oerr(http.StatusTooManyRequests, "slow_down", "Too many requests. Try again shortly."))
}

func (s *Service) MountUser(r chi.Router) {
	r.Get("/mcp/keys", httpx.Handler(s.listKeys))
	r.With(auth.RequireActive, auth.RateLimit(10, time.Hour)).Post("/mcp/keys", httpx.Handler(s.createKey))
	r.Delete("/mcp/keys/{id}", httpx.Handler(s.revokeKey))
	r.Get("/mcp/oauth/request", httpx.Handler(s.describeRequest))
	r.With(auth.RateLimit(30, time.Minute)).Post("/mcp/oauth/decision", httpx.Handler(s.decide))
	r.Get("/mcp/connections", httpx.Handler(s.listConnections))
	r.Delete("/mcp/connections/{id}", httpx.Handler(s.revokeConnection))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/mcp/keys", httpx.Handler(s.adminListKeys))
	r.Delete("/mcp/keys/{id}", httpx.Handler(s.adminRevokeKey))
	r.Get("/mcp/connections", httpx.Handler(s.adminListConnections))
	r.Delete("/mcp/connections/{id}", httpx.Handler(s.adminRevokeConnection))
}

// Start prunes OAuth rows nothing can use again, hourly.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "mcp.oauth_prune", time.Hour, s.prune)
}

// ---- endpoint --------------------------------------------------------------

type principalKey struct{}

func (s *Service) mcpHandler() http.Handler {
	s.build()
	return s.handler
}

// build assembles the servers on first use: Deps.Modules is filled after
// every module is constructed.
func (s *Service) build() {
	s.once.Do(func() {
		type provided struct {
			app.Tool
			feature string
		}
		var all []provided
		for _, m := range s.d.Modules {
			if tp, ok := m.(app.ToolProvider); ok {
				for _, t := range tp.Tools() {
					all = append(all, provided{t, m.Name()})
				}
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		seen := map[string]bool{}
		s.writeTools = map[string]bool{}
		for _, pt := range all {
			t := pt.Tool
			if t.Handler == nil || seen[t.Name] {
				slog.Error("mcp: tool refused (no handler or duplicate name)", "tool", t.Name)
				continue
			}
			var sch jsonschema.Schema
			if err := json.Unmarshal(t.InputSchema, &sch); err != nil || sch.Type != "object" {
				slog.Error("mcp: tool refused (input schema must be a JSON Schema object)", "tool", t.Name, "err", err)
				continue
			}
			resolved, err := sch.Resolve(nil)
			if err != nil {
				slog.Error("mcp: tool refused (schema does not resolve)", "tool", t.Name, "err", err)
				continue
			}
			seen[t.Name] = true
			if !t.ReadOnly {
				s.writeTools[t.Name] = true
			}
			s.tools = append(s.tools, exposedTool{tool: t, schema: resolved, feature: pt.feature})
		}
		s.readServer = s.newServer(false, nil)
		s.writeServer = s.newServer(true, nil)
		h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
			p, _ := r.Context().Value(principalKey{}).(*principal)
			write := p != nil && p.canWrite()
			// tools/list follows the person's agent scope; calls are gated per
			// call in toolHandler, so the shared servers serve everything else.
			if policy := app.FindAgentPolicy(s.d.Modules); policy != nil && p != nil && listsTools(r.Context()) {
				return s.newServer(write, s.hiddenTools(r.Context(), policy, p))
			}
			if write {
				return s.writeServer
			}
			return s.readServer
		}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
		bearer := sdkauth.RequireBearerToken(func(ctx context.Context, _ string, r *http.Request) (*sdkauth.TokenInfo, error) {
			p, _ := r.Context().Value(principalKey{}).(*principal)
			if p == nil {
				return nil, sdkauth.ErrInvalidToken
			}
			return &sdkauth.TokenInfo{UserID: p.identity.UserID.String(), Scopes: p.scopes,
				Extra: map[string]any{"principal": p}}, nil
		}, &sdkauth.RequireBearerTokenOptions{AllowMissingExpiration: true})
		limit := httprate.LimitBy(s.ratePerMin, time.Minute, func(r *http.Request) (string, error) {
			if p, _ := r.Context().Value(principalKey{}).(*principal); p != nil {
				return "mcp:" + p.via + ":" + p.id.String(), nil
			}
			return httpx.KeyByClientIP(r)
		}, httprate.WithLimitHandler(httpx.RateLimitedHandler))
		s.handler = s.authenticate(limit(bearer(h)))
		slog.Info("mcp: tools exposed", "count", len(s.tools), "write", len(s.writeTools))
	})
}

const instructions = `Tools for Scaffold App. Every call acts as the person who connected (by signing in over OAuth, or with their API key) and sees only what that person can see in the app. Read access lists read-only tools; read and write access also lists tools that change data. The person can narrow this per feature in their agent settings: tools they turned off are not listed, and a change may come back as a suggestion they review instead of running (the result says so; it is not an error). A failed call returns a tool result marked as an error whose text is "code: message". Calls are limited per connection per minute.`

// newServer builds an MCP server with the tools a scope allows: read-only
// ones, plus write ones when write is true, minus hidden.
func (s *Service) newServer(write bool, hidden map[string]bool) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "scaffoldapp", Title: "Scaffold App", Version: Version},
		&sdk.ServerOptions{Instructions: instructions})
	for _, et := range s.tools {
		if (!et.tool.ReadOnly && !write) || hidden[et.tool.Name] {
			continue
		}
		srv.AddTool(&sdk.Tool{
			Name:        et.tool.Name,
			Description: et.tool.Description,
			InputSchema: et.tool.InputSchema,
			Annotations: annotations(et.tool),
		}, s.toolHandler(et))
	}
	return srv
}

func annotations(t app.Tool) *sdk.ToolAnnotations {
	closed := false
	if t.ReadOnly {
		return &sdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &closed, IdempotentHint: true, OpenWorldHint: &closed}
	}
	return &sdk.ToolAnnotations{OpenWorldHint: &closed}
}

// hiddenTools applies the person's agent scope to tools/list: a feature whose
// highest level is off hides every tool, one below suggest hides its write
// tools. A policy error hides the feature (fail closed).
func (s *Service) hiddenTools(ctx context.Context, policy app.AgentPolicy, p *principal) map[string]bool {
	ctx = auth.WithAgent(ctx, p.agent())
	levels := map[string]app.AgentLevel{}
	hidden := map[string]bool{}
	for _, et := range s.tools {
		maxLevel, ok := levels[et.feature]
		if !ok {
			_, m, err := policy.FeatureLevel(ctx, p.identity, et.feature)
			if err != nil {
				slog.ErrorContext(ctx, "mcp: agent scope lookup failed; hiding feature", "feature", et.feature, "err", err)
				m = app.AgentOff
			}
			levels[et.feature], maxLevel = m, m
		}
		if maxLevel == app.AgentOff || (!et.tool.ReadOnly && maxLevel < app.AgentSuggest) {
			hidden[et.tool.Name] = true
		}
	}
	return hidden
}

// challenge is the WWW-Authenticate value that starts OAuth discovery
// (RFC 9728 §5.1): where the resource metadata is and which scopes to ask for.
func (s *Service) challenge(extra string) string {
	if !s.oauthEnabled() {
		return `Bearer realm="mcp"` + extra
	}
	return fmt.Sprintf(`Bearer resource_metadata=%q, scope="read write"%s`, s.resourceMetadataURL(), extra)
}

// authenticate resolves the bearer (API key or OAuth access token) before the
// SDK sees the request and answers failures in the API's JSON error envelope,
// with the headers OAuth clients act on.
func (s *Service) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			w.Header().Set("WWW-Authenticate", s.challenge(""))
			httpx.Fail(w, r, errBadKey)
			return
		}
		p, err := s.resolve(r.Context(), strings.TrimSpace(token))
		if err != nil {
			var apiErr *httpx.Error
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
				w.Header().Set("WWW-Authenticate", s.challenge(`, error="invalid_token"`))
			}
			httpx.Fail(w, r, err)
			return
		}
		// Stateless with JSON responses: no stream to open (GET) and no session
		// to end (DELETE). Every request is a POST the endpoint can inspect.
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			httpx.Fail(w, r, httpx.NewError(http.StatusMethodNotAllowed, "method_not_allowed", "Send JSON-RPC requests with POST."))
			return
		}
		calls, body, ok := peekRPC(r)
		if !ok {
			httpx.Fail(w, r, httpx.NewError(http.StatusRequestEntityTooLarge, "payload_too_large", "Requests are limited to 1 MiB."))
			return
		}
		r.Body = body
		// Tools only: every other method (resources, prompts, completion,
		// logging…) is refused here, so nothing but a gated tool call runs.
		for _, m := range calls {
			if !allowedMethod(m.Method) {
				methodNotFound(w, calls, m.Method)
				return
			}
		}
		ctx := context.WithValue(r.Context(), rpcKey{}, calls)
		if tool := s.writeToolIn(calls); tool != "" && !p.canWrite() {
			if p.via == "oauth" {
				// Step-up (MCP spec, scope challenge): a 403 the client can
				// answer by asking the person for write access.
				w.Header().Set("WWW-Authenticate", s.challenge(
					`, error="insufficient_scope", error_description="`+tool+` changes data and needs the write scope"`))
				httpx.Fail(w, r, httpx.NewError(http.StatusForbidden, "insufficient_scope",
					"This tool changes data. Reconnect the app with read and write access."))
				return
			}
			httpx.Fail(w, r, httpx.NewError(http.StatusForbidden, "scope_required",
				"This key is read only. Create a key with read and write access to use "+tool+"."))
			return
		}
		ctx = auth.WithAgent(context.WithValue(ctx, principalKey{}, p), p.agent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type rpcKey struct{}

// rpcCall is the part of a JSON-RPC message the endpoint routes on.
type rpcCall struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name string `json:"name"`
	} `json:"params"`
}

// peekRPC reads the JSON-RPC message(s) in a POST body and returns a reader
// that replays it; ok is false for bodies over 1 MiB. A body that isn't JSON
// yields no calls (the SDK answers with a parse error and runs nothing).
func peekRPC(r *http.Request) ([]rpcCall, io.ReadCloser, bool) {
	const peek = 1 << 20
	buf, err := io.ReadAll(io.LimitReader(r.Body, peek+1))
	if err != nil || len(buf) > peek {
		return nil, nil, false
	}
	body := io.NopCloser(bytes.NewReader(buf))
	var msgs []rpcCall
	if t := bytes.TrimSpace(buf); len(t) > 0 && t[0] == '[' {
		_ = json.Unmarshal(t, &msgs)
	} else {
		var one rpcCall
		if json.Unmarshal(t, &one) == nil {
			msgs = []rpcCall{one}
		}
	}
	return msgs, body, true
}

// allowedMethod: the lifecycle, ping and tools. Messages without a method
// are responses, and notifications carry nothing to run.
func allowedMethod(m string) bool {
	switch m {
	case "", "initialize", "ping", "tools/list", "tools/call":
		return true
	}
	return strings.HasPrefix(m, "notifications/")
}

// methodNotFound answers JSON-RPC -32601 for a method the server doesn't offer.
func methodNotFound(w http.ResponseWriter, calls []rpcCall, method string) {
	type rpcErr struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   map[string]any  `json:"error"`
	}
	mk := func(id json.RawMessage) rpcErr {
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		return rpcErr{"2.0", id, map[string]any{"code": -32601, "message": "Method not found: " + method + ". This server offers tools only."}}
	}
	if len(calls) == 1 {
		httpx.OK(w, mk(calls[0].ID))
		return
	}
	out := make([]rpcErr, 0, len(calls))
	for _, c := range calls {
		if len(c.ID) > 0 {
			out = append(out, mk(c.ID))
		}
	}
	httpx.OK(w, out)
}

// writeToolIn names a tool that changes data among tools/call messages.
func (s *Service) writeToolIn(calls []rpcCall) string {
	for _, m := range calls {
		if m.Method == "tools/call" && s.writeTools[m.Params.Name] {
			return m.Params.Name
		}
	}
	return ""
}

// listsTools reports whether the request asks for tools/list.
func listsTools(ctx context.Context) bool {
	calls, _ := ctx.Value(rpcKey{}).([]rpcCall)
	for _, m := range calls {
		if m.Method == "tools/list" {
			return true
		}
	}
	return false
}

// toolHandler adapts an app.Tool: scope and consent checks, input
// validation, a timeout, and errors as tool results ("code: message").
func (s *Service) toolHandler(et exposedTool) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var p *principal
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			p, _ = req.Extra.TokenInfo.Extra["principal"].(*principal)
		}
		if p == nil {
			return errorResult("unauthenticated", "Send a valid API key or access token."), nil
		}
		if !et.tool.ReadOnly {
			if !p.canWrite() {
				return errorResult("scope_required", fmt.Sprintf("This connection needs the write scope for %s.", et.tool.Name)), nil
			}
			if !legal.Current(p.identity.TermsVersion, p.identity.PrivacyVersion) {
				e := legal.ErrConsentRequired()
				return errorResult(e.Code, e.Message), nil
			}
		}
		raw := json.RawMessage(`{}`)
		if req.Params != nil && len(req.Params.Arguments) > 0 && string(req.Params.Arguments) != "null" {
			raw = req.Params.Arguments
		}
		var input any
		if err := json.Unmarshal(raw, &input); err != nil {
			return errorResult("invalid_input", "Arguments must be a JSON object."), nil
		}
		if err := et.schema.Validate(input); err != nil {
			return errorResult("invalid_input", "Invalid arguments: "+err.Error()), nil
		}
		// The person's agent scope, on top of the key or token scope above
		// (a ceiling: a read-only connection never writes, and a write one in
		// suggest mode only suggests).
		ctx = auth.WithAgent(ctx, p.agent())
		if policy := app.FindAgentPolicy(s.d.Modules); policy != nil {
			d, err := policy.GateTool(ctx, p.identity, et.feature, et.tool, raw)
			if err != nil {
				return errorResult(failure(ctx, et.tool.Name, err)), nil
			}
			switch d.Action {
			case app.ToolRun:
			case app.ToolSuggested:
				return suggestedResult(p.identity, d.SuggestionID), nil
			default:
				return errorResult(d.Code, d.Message), nil
			}
		}
		out, err := s.run(ctx, et.tool, p.identity, raw)
		if err != nil {
			return errorResult(failure(ctx, et.tool.Name, err)), nil
		}
		return successResult(out), nil
	}
}

func (s *Service) run(ctx context.Context, t app.Tool, caller *auth.Identity, input json.RawMessage) (out any, err error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "mcp: tool panicked", "tool", t.Name, "panic", fmt.Sprint(rec))
			err = errors.New("tool panicked")
		}
	}()
	return t.Handler(ctx, caller, input)
}

func failure(ctx context.Context, name string, err error) (string, string) {
	var apiErr *httpx.Error
	switch {
	case errors.As(err, &apiErr):
		return apiErr.Code, apiErr.Message
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout", "The tool took too long to answer."
	}
	slog.ErrorContext(ctx, "mcp: tool failed", "tool", name, "err", err)
	return "internal", "Something went wrong. Try again."
}

// suggestedResult tells the agent its write became a suggestion: not an
// error, nothing ran, and the person decides.
func suggestedResult(caller *auth.Identity, id string) *sdk.CallToolResult {
	who := caller.DisplayName
	if who == "" {
		who = caller.Email
	}
	return &sdk.CallToolResult{
		Content:           []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("Suggested to %s for review (suggestion %s). It runs only if they accept.", who, id)}},
		StructuredContent: map[string]any{"status": "suggested", "suggestion_id": id},
	}
}

func errorResult(code, msg string) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: code + ": " + msg}}}
}

// successResult returns the tool's JSON both as text and as structured
// content (an object; other values are wrapped as {"result": …}).
func successResult(out any) *sdk.CallToolResult {
	b, err := json.Marshal(out)
	if err != nil {
		return errorResult("internal", "The result could not be encoded.")
	}
	var structured any = json.RawMessage(b)
	if len(b) == 0 || b[0] != '{' {
		structured = map[string]json.RawMessage{"result": b}
		b, _ = json.Marshal(structured)
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}, StructuredContent: structured}
}

// ---- server card -----------------------------------------------------------

type cardTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"read_only"`
}

// serverCard is GET /.well-known/mcp.json: what an agent needs to connect.
func (s *Service) serverCard(w http.ResponseWriter, r *http.Request) {
	s.build()
	tools := make([]cardTool, 0, len(s.tools))
	for _, et := range s.tools {
		tools = append(tools, cardTool{Name: et.tool.Name, Description: firstSentence(et.tool.Description), ReadOnly: et.tool.ReadOnly})
	}
	scopes := map[string]string{ScopeRead: "Read-only tools", ScopeWrite: "Also tools that change data"}
	auths := []map[string]any{{
		"type":   "bearer",
		"format": "Personal API key from Settings, starting with " + keyPrefix,
		"scopes": scopes,
	}}
	if s.oauthEnabled() {
		auths = append([]map[string]any{{
			"type":                 "oauth2",
			"authorization_server": s.issuer(),
			"resource":             s.resourceURL(),
			"resource_metadata":    s.resourceMetadataURL(),
			"client_registration":  []string{"client_id_metadata_document", "dynamic"},
			"scopes":               scopes,
		}}, auths...)
	}
	publicJSON(w, map[string]any{
		"name":        "Scaffold App",
		"description": "Tools for Scaffold App, acting as the person who connected: sign in over OAuth, or use a personal API key.",
		"version":     Version,
		"transport":   "streamable-http",
		"endpoints": []map[string]any{{
			"url":       s.resourceURL(),
			"transport": "streamable-http",
			"auth":      auths,
		}},
		"errors": "Failed calls return an error tool result with text \"code: message\".",
		"tools":  tools,
	})
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

// ---- account deletion, export, seed, tools ----------------------------------

// DeleteAccount deletes the user's keys, OAuth grants (with their tokens)
// and unexchanged codes.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM mcp_api_keys WHERE user_id = $1`,
		`DELETE FROM mcp_oauth_codes WHERE user_id = $1`,
		`DELETE FROM mcp_oauth_grants WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// exportedGrant is a connected app in the data export.
type exportedGrant struct {
	ClientName   string     `json:"client_name"`
	ClientID     string     `json:"client_id"`
	Scopes       []string   `json:"scopes"`
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
}

// ExportAccount returns key and connected-app metadata (never secrets,
// tokens or hashes).
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+keyCols+` FROM mcp_api_keys WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToStructByPos[APIKey])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT c.client_name, c.client_id, g.scopes, g.created_at, g.last_used_at, g.revoked_at, g.revoke_reason
		FROM mcp_oauth_grants g JOIN mcp_oauth_clients c ON c.id = g.client_ref
		WHERE g.user_id = $1 ORDER BY g.created_at, g.id`, userID)
	if err != nil {
		return nil, err
	}
	grants, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportedGrant])
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []APIKey{}
	}
	if grants == nil {
		grants = []exportedGrant{}
	}
	return map[string]any{"api_keys": keys, "connected_apps": grants}, nil
}

// prune deletes OAuth rows nothing can use again: lapsed requests, codes a
// day after expiry (kept that long so a replay still revokes its grant),
// tokens whose refresh expired (spent ones stay until then so a replay is
// detected), grants revoked or lapsed 90 days ago, and clients never used
// within a day of registering.
func (s *Service) prune(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM mcp_oauth_requests WHERE expires_at < now()`,
		`DELETE FROM mcp_oauth_codes WHERE expires_at < now() - interval '1 day'`,
		`DELETE FROM mcp_oauth_tokens WHERE refresh_expires_at < now()`,
		`DELETE FROM mcp_oauth_grants WHERE revoked_at < now() - interval '90 days' OR expires_at < now() - interval '90 days'`,
		`DELETE FROM mcp_oauth_clients c WHERE c.last_used_at IS NULL AND c.created_at < now() - interval '1 day'
		   AND NOT EXISTS (SELECT 1 FROM mcp_oauth_grants g WHERE g.client_ref = c.id)`,
	} {
		if _, err := s.d.Pool.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// SeedDev gives the second dev identity a read-only API key and a connected
// app, so both settings lists have a row. Secrets are random and never
// printed; the seeded grant has no usable token.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 2 {
		return nil
	}
	ns := uuid.MustParse("2b8f0e7c-41d6-4a8e-9b3c-5d7a1f0c6e24")
	id := uuid.NewSHA1(ns, []byte("mcp:key:"+users[1].String()))
	_, hash, prefix := newSecret()
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_api_keys (id, user_id, name, prefix, secret_hash, scopes, created_at)
		VALUES ($1, $2, 'Laptop agent', $3, $4, '{read}', now() - interval '3 days')
		ON CONFLICT (id) DO NOTHING`, id, users[1], prefix, hash); err != nil {
		return err
	}
	clientRef := uuid.NewSHA1(ns, []byte("mcp:client:desktop"))
	grantID := uuid.NewSHA1(ns, []byte("mcp:grant:"+users[1].String()))
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_oauth_clients (id, client_id, kind, client_name, redirect_uris, last_used_at, created_at)
		VALUES ($1, $2, 'registered', 'Desktop agent', '{http://127.0.0.1/callback}', now() - interval '2 hours', now() - interval '6 days')
		ON CONFLICT (id) DO NOTHING`, clientRef, clientIDPrefix+"devseed"); err != nil {
		return err
	}
	_, err := s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_oauth_grants (id, user_id, client_ref, scopes, resource, expires_at, first_used_at, last_used_at, created_at)
		VALUES ($1, $2, $3, '{read}', $4, now() + interval '30 days', now() - interval '6 days', now() - interval '2 hours', now() - interval '6 days')
		ON CONFLICT (id) DO UPDATE SET expires_at = EXCLUDED.expires_at`, grantID, users[1], clientRef, s.resourceURL())
	return err
}

// Tools: one read tool so an agent can confirm whose account it acts for.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{{
		Name:        "mcp_whoami",
		Description: "Get the account these tools act for: user id, display name and email. Call this first when you need to know whose data you are working with.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		ReadOnly:    true,
		Handler: func(_ context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
			return map[string]any{"user_id": caller.UserID, "display_name": caller.DisplayName, "email": caller.Email}, nil
		},
	}}
}
