// Package agentweb makes the app's tools reachable from a browser agent on
// the open page (WebMCP) and tells the web app what to advertise in
// /llms.txt. Tools are every module's app.ToolProvider tools, the same ones
// the mcp feature serves at /mcp; this package adds no tools of its own.
// WebMCP gets the read-only ones only, run as the signed-in person, marked
// as an agent call (auth.Agent{Via: "webmcp"}) and gated by the installed
// app.AgentPolicy exactly like an MCP call. Distilled from MOX
// (web/components/live/WebMcp.tsx and the llms routes).
package agentweb

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
	"github.com/google/jsonschema-go/jsonschema"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/legal"
)

// toolTimeout bounds one tool call.
const toolTimeout = 15 * time.Second

// Service is the agent-web module.
type Service struct {
	d           *app.Deps
	callsPerMin int
	enabled     bool

	once   sync.Once
	tools  []exposed // sorted by name
	byName map[string]exposed
}

type exposed struct {
	tool    app.Tool
	schema  *jsonschema.Resolved
	feature string // agent-scope key: Tool.Feature, else the providing module's Name()
}

func New(d *app.Deps) *Service {
	s := &Service{
		d:           d,
		callsPerMin: config.Int("AGENT_WEB_TOOL_CALLS_PER_MINUTE", 60),
		enabled:     config.Bool("AGENT_WEB_WEBMCP_ENABLED", true),
	}
	if s.callsPerMin < 1 || s.callsPerMin > 6000 {
		config.Problem("AGENT_WEB_TOOL_CALLS_PER_MINUTE must be between 1 and 6000 (got %d)", s.callsPerMin)
	}
	// Tool calls only read, so they work before the Terms are re-accepted,
	// like any other GET.
	legal.Exempt("/v1/agent-web/tools/")
	return s
}

func (s *Service) Name() string { return "agent-web" }

// build collects the tools once every module exists (Deps.Modules is filled
// after construction).
func (s *Service) build() {
	s.once.Do(func() {
		s.byName = map[string]exposed{}
		for _, m := range s.d.Modules {
			tp, ok := m.(app.ToolProvider)
			if !ok {
				continue
			}
			for _, t := range tp.Tools() {
				if t.Handler == nil || t.Source != "" {
					continue
				}
				if _, dup := s.byName[t.Name]; dup {
					continue
				}
				var sch jsonschema.Schema
				if err := json.Unmarshal(t.InputSchema, &sch); err != nil || sch.Type != "object" {
					slog.Error("agent-web: tool skipped (input schema must be a JSON Schema object)", "tool", t.Name, "err", err)
					continue
				}
				resolved, err := sch.Resolve(nil)
				if err != nil {
					slog.Error("agent-web: tool skipped (schema does not resolve)", "tool", t.Name, "err", err)
					continue
				}
				feature := t.Feature
				if feature == "" {
					feature = m.Name()
				}
				e := exposed{tool: t, schema: resolved, feature: feature}
				s.byName[t.Name] = e
				s.tools = append(s.tools, e)
			}
		}
		sort.Slice(s.tools, func(i, j int) bool { return s.tools[i].tool.Name < s.tools[j].tool.Name })
		slog.Info("agent-web: tools", "count", len(s.tools))
	})
}

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/agent-web/manifest", httpx.Handler(s.manifest))
}

func (s *Service) MountUser(r chi.Router) {
	r.Get("/agent-web/tools", httpx.Handler(s.listTools))
	r.With(auth.RateLimit(s.callsPerMin, time.Minute)).Post("/agent-web/tools/{name}", httpx.Handler(s.callTool))
}

// ---- manifest (for /llms.txt) ----

// ManifestTool is one tool as /llms.txt lists it.
type ManifestTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"read_only"`
	Feature     string `json:"feature"`
}

// Manifest is GET /v1/agent-web/manifest: what the web app advertises to agents.
type Manifest struct {
	APIURL string `json:"api_url"`
	// MCP is set when the mcp feature is installed: where agents connect.
	MCP    *ManifestMCP   `json:"mcp"`
	WebMCP bool           `json:"webmcp"`
	Tools  []ManifestTool `json:"tools"`
}

type ManifestMCP struct {
	Endpoint   string `json:"endpoint"`
	ServerCard string `json:"server_card"`
}

func (s *Service) manifest(w http.ResponseWriter, _ *http.Request) error {
	s.build()
	base := strings.TrimRight(s.d.Cfg.PublicAPIURL, "/")
	m := Manifest{APIURL: base, WebMCP: s.enabled, Tools: make([]ManifestTool, 0, len(s.tools))}
	for _, mod := range s.d.Modules {
		if mod.Name() == "mcp" && base != "" {
			m.MCP = &ManifestMCP{Endpoint: base + "/mcp", ServerCard: base + "/.well-known/mcp.json"}
		}
	}
	for _, e := range s.tools {
		m.Tools = append(m.Tools, ManifestTool{Name: e.tool.Name, Description: firstSentence(e.tool.Description), ReadOnly: e.tool.ReadOnly, Feature: e.feature})
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.OK(w, m)
	return nil
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

// ---- WebMCP ----

// Tool is one WebMCP tool for the signed-in person (GET /v1/agent-web/tools).
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Feature     string          `json:"feature"`
}

func webAgent() auth.Agent {
	return auth.Agent{Via: "webmcp", Client: "Browser agent", ClientID: "webmcp"}
}

// visible applies the person's agent scope: a feature whose highest level is
// off hides its tools. A policy error hides the feature (fail closed).
func (s *Service) visible(ctx context.Context, caller *auth.Identity) []exposed {
	out := make([]exposed, 0, len(s.tools))
	policy := app.FindAgentPolicy(s.d.Modules)
	levels := map[string]app.AgentLevel{}
	ctx = auth.WithAgent(ctx, webAgent())
	for _, e := range s.tools {
		if !e.tool.ReadOnly {
			continue
		}
		if policy != nil {
			lvl, ok := levels[e.feature]
			if !ok {
				_, m, err := policy.FeatureLevel(ctx, caller, e.feature)
				if err != nil {
					slog.ErrorContext(ctx, "agent-web: agent scope lookup failed; hiding feature", "feature", e.feature, "err", err)
					m = app.AgentOff
				}
				levels[e.feature], lvl = m, m
			}
			if lvl == app.AgentOff {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

func (s *Service) listTools(w http.ResponseWriter, r *http.Request) error {
	if !s.enabled {
		httpx.OK(w, map[string]any{"items": []Tool{}})
		return nil
	}
	s.build()
	me := auth.MustFromContext(r.Context())
	items := []Tool{}
	for _, e := range s.visible(r.Context(), me) {
		items = append(items, Tool{Name: e.tool.Name, Description: e.tool.Description, InputSchema: e.tool.InputSchema, Feature: e.feature})
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

// CallRequest is POST /v1/agent-web/tools/{name}.
type CallRequest struct {
	Arguments json.RawMessage `json:"arguments"`
}

// CallResult is a tool's answer: its JSON, or a suggestion id when the
// person's agent scope turned the call into a suggestion.
type CallResult struct {
	Result       json.RawMessage `json:"result,omitempty"`
	Status       string          `json:"status"` // ok | suggested
	SuggestionID string          `json:"suggestion_id,omitempty"`
}

func (s *Service) callTool(w http.ResponseWriter, r *http.Request) error {
	if !s.enabled {
		return httpx.NewError(http.StatusNotFound, "webmcp_off", "Browser agent tools are turned off.")
	}
	s.build()
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	name := chi.URLParam(r, "name")
	e, ok := s.byName[name]
	if !ok || !e.tool.ReadOnly {
		// Write tools are never offered here: a browser agent reaches them only
		// through the page's own controls, where the person acts.
		return httpx.NewError(http.StatusNotFound, "unknown_tool", "No read tool named "+name+".")
	}
	var req CallRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	raw := req.Arguments
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return httpx.NewError(http.StatusBadRequest, "invalid_input", "Arguments must be a JSON object.")
	}
	if err := e.schema.Validate(input); err != nil {
		return httpx.NewError(http.StatusBadRequest, "invalid_input", "Invalid arguments: "+err.Error())
	}
	ctx = auth.WithAgent(ctx, webAgent())
	if policy := app.FindAgentPolicy(s.d.Modules); policy != nil {
		d, err := policy.GateTool(ctx, me, e.feature, e.tool, raw)
		if err != nil {
			return err
		}
		switch d.Action {
		case app.ToolRun:
		case app.ToolSuggested:
			httpx.OK(w, CallResult{Status: "suggested", SuggestionID: d.SuggestionID})
			return nil
		default:
			return httpx.NewError(http.StatusForbidden, d.Code, d.Message)
		}
	}
	out, err := run(ctx, e.tool, me, raw)
	if err != nil {
		var apiErr *httpx.Error
		switch {
		case errors.As(err, &apiErr):
			return apiErr
		case errors.Is(err, context.DeadlineExceeded):
			return httpx.NewError(http.StatusGatewayTimeout, "timeout", "The tool took too long to answer.")
		}
		return err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	httpx.OK(w, CallResult{Status: "ok", Result: b})
	return nil
}

func run(ctx context.Context, t app.Tool, caller *auth.Identity, input json.RawMessage) (out any, err error) {
	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("tool %s panicked: %v", t.Name, rec)
		}
	}()
	return t.Handler(ctx, caller, input)
}
