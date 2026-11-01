// Package mcp serves the app as an MCP server for agents: personal API keys
// (read or read+write scopes) and a stateless streamable-HTTP endpoint that
// exposes every module's app.ToolProvider tools, run as the key's owner.
// Distilled from MOX (internal/mcpserver, internal/access) on the official Go
// SDK.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"scaffoldapp/backend/internal/legal"
)

// Version is reported to MCP clients and in the server card.
const Version = "0.1.0"

// toolTimeout bounds one tool call (the root group has no request timeout).
const toolTimeout = 15 * time.Second

// Service is the mcp module.
type Service struct {
	d          *app.Deps
	ratePerMin int

	once        sync.Once
	readServer  *sdk.Server
	writeServer *sdk.Server
	tools       []exposedTool
	handler     http.Handler
}

type exposedTool struct {
	tool   app.Tool
	schema *jsonschema.Resolved
}

func New(d *app.Deps) *Service {
	s := &Service{d: d, ratePerMin: max(1, config.Int("MCP_RATE_LIMIT_PER_MINUTE", 120))}
	return s
}

func (s *Service) Name() string { return "mcp" }

// MountRoot serves the MCP endpoint at /mcp and its server card at
// /.well-known/mcp.json, where agents look for them. The root group applies
// the per-IP rate limit only; the handler authenticates the API key itself
// and each tool call is bounded by toolTimeout.
func (s *Service) MountRoot(r chi.Router) {
	r.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.mcpHandler().ServeHTTP(w, r) }))
	r.Get("/.well-known/mcp.json", s.serverCard)
}

func (s *Service) MountUser(r chi.Router) {
	r.Get("/mcp/keys", httpx.Handler(s.listKeys))
	r.With(auth.RequireActive, auth.RateLimit(10, time.Hour)).Post("/mcp/keys", httpx.Handler(s.createKey))
	r.Delete("/mcp/keys/{id}", httpx.Handler(s.revokeKey))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/mcp/keys", httpx.Handler(s.adminListKeys))
	r.Delete("/mcp/keys/{id}", httpx.Handler(s.adminRevokeKey))
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
		var all []app.Tool
		for _, m := range s.d.Modules {
			if tp, ok := m.(app.ToolProvider); ok {
				all = append(all, tp.Tools()...)
			}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
		seen := map[string]bool{}
		for _, t := range all {
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
			s.tools = append(s.tools, exposedTool{tool: t, schema: resolved})
		}
		s.readServer = s.newServer(false)
		s.writeServer = s.newServer(true)
		h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
			if p, _ := r.Context().Value(principalKey{}).(*principal); p != nil && p.canWrite() {
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
				return "mcpkey:" + p.keyID.String(), nil
			}
			return httpx.KeyByClientIP(r)
		}, httprate.WithLimitHandler(httpx.RateLimitedHandler))
		s.handler = s.authenticate(limit(bearer(h)))
		slog.Info("mcp: tools exposed", "count", len(s.tools))
	})
}

const instructions = `Tools for Scaffold App. Every call acts as the account that owns the API key and sees only that account's data. Keys with the read scope see read-only tools; keys with read and write also see tools that change data. A failed call returns a tool result marked as an error whose text is "code: message". Calls are limited per key per minute.`

func (s *Service) newServer(write bool) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "scaffoldapp", Title: "Scaffold App", Version: Version},
		&sdk.ServerOptions{Instructions: instructions})
	for _, et := range s.tools {
		if !et.tool.ReadOnly && !write {
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

// authenticate resolves the API key before the SDK sees the request and
// answers failures in the API's JSON error envelope.
func (s *Service) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			httpx.Fail(w, r, errBadKey)
			return
		}
		p, err := s.resolveKey(r.Context(), strings.TrimSpace(token))
		if err != nil {
			var apiErr *httpx.Error
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
				w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", error="invalid_token"`)
			}
			httpx.Fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
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
			return errorResult("unauthenticated", "Send a valid API key."), nil
		}
		if !et.tool.ReadOnly {
			if !p.canWrite() {
				return errorResult("scope_required", fmt.Sprintf("This key needs the write scope for %s.", et.tool.Name)), nil
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
	base := strings.TrimRight(s.d.Cfg.PublicAPIURL, "/")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.OK(w, map[string]any{
		"name":        "Scaffold App",
		"description": "Tools for Scaffold App, acting as the account that owns the API key.",
		"version":     Version,
		"transport":   "streamable-http",
		"endpoints": []map[string]any{{
			"url":       base + "/mcp",
			"transport": "streamable-http",
			"auth": map[string]any{
				"type":   "bearer",
				"format": "Personal API key from Settings, starting with " + keyPrefix,
				"scopes": map[string]string{ScopeRead: "Read-only tools", ScopeWrite: "Also tools that change data"},
			},
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

// DeleteAccount deletes the user's keys.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM mcp_api_keys WHERE user_id = $1`, userID)
	return err
}

// ExportAccount returns key metadata (never secrets or hashes).
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+keyCols+` FROM mcp_api_keys WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToStructByPos[APIKey])
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []APIKey{}
	}
	return map[string]any{"api_keys": keys}, nil
}

// SeedDev gives the second dev identity one read-only key so the settings
// list has a row. Its secret is random and never printed.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 2 {
		return nil
	}
	id := uuid.NewSHA1(uuid.MustParse("2b8f0e7c-41d6-4a8e-9b3c-5d7a1f0c6e24"), []byte("mcp:key:"+users[1].String()))
	_, hash, prefix := newSecret()
	_, err := s.d.Pool.Exec(ctx, `
		INSERT INTO mcp_api_keys (id, user_id, name, prefix, secret_hash, scopes, created_at)
		VALUES ($1, $2, 'Laptop agent', $3, $4, '{read}', now() - interval '3 days')
		ON CONFLICT (id) DO NOTHING`, id, users[1], prefix, hash)
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
