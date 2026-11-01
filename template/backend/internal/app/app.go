// Package app holds the dependency container handed to every module and the
// optional interfaces a module implements to plug into the API. The router
// discovers capabilities by type assertion; a module implements only what it
// needs.
package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// Deps are the shared services. Modules receive *Deps in their New
// constructor. Only dependency providers (registered with api.RegisterDeps,
// e.g. the notifications feature replacing Notify) may assign fields, and
// only before modules are constructed.
type Deps struct {
	Cfg     *config.Config
	Pool    *pgxpool.Pool
	Auth    *auth.Authenticator
	Broker  realtime.Broker
	Storage *storage.Store
	Mail    mail.Mailer
	Notify  notify.Sender

	// Modules is every constructed module, filled by api.Build before any
	// request is served. Read it at request time, never in New.
	Modules []Module
}

// Module is a unit of product behaviour (a core package or a feature).
type Module interface {
	Name() string
}

// PublicRoutes mounts under /v1 without auth.
type PublicRoutes interface{ MountPublic(r chi.Router) }

// UserRoutes mounts under /v1 behind auth. Mutations are also behind the
// legal consent gate unless exempted with legal.Exempt.
type UserRoutes interface{ MountUser(r chi.Router) }

// AdminRoutes mounts under /v1/admin behind auth.RequireAdminRole("support").
// Wrap admin-only routes in auth.RequireAdminRole("admin"), and write
// audit.Log in the same transaction as every admin mutation.
type AdminRoutes interface{ MountAdmin(r chi.Router) }

// StreamRoutes mounts under /v1 behind auth, the user rate limit and the
// consent gate, but outside the request timeout: server-sent events and
// other long-lived responses. Detect a gone client from failed writes.
type StreamRoutes interface{ MountStream(r chi.Router) }

// RootRoutes mounts at the server root with the per-IP rate limit and no
// auth or timeout, for paths clients expect there (/mcp,
// /.well-known/...). The handler authenticates itself.
type RootRoutes interface{ MountRoot(r chi.Router) }

// WebhookRoutes mounts under /webhooks: raw bodies, no auth, no timeout.
// Each handler verifies its provider's signature.
type WebhookRoutes interface{ MountWebhooks(r chi.Router) }

// Worker runs background loops until ctx is done. Start must return quickly
// (spawn goroutines). Periodic work that one instance should do at a time
// goes through jobs.Every.
type Worker interface{ Start(ctx context.Context) }

// TopicAuthorizer decides realtime subscriptions for topics it owns. It
// returns handled=false for topics it does not recognise.
type TopicAuthorizer interface {
	AuthorizeTopic(ctx context.Context, userID uuid.UUID, isAdmin bool, topic string) (handled, allowed bool)
}

// AccountDeleter removes or anonymises the module's rows for a user inside
// the account-deletion transaction. Delete personal content; anonymise rows
// other users or the law still need (financial records, other people's
// conversations). See concrete/compliance.md.
type AccountDeleter interface {
	DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error
}

// AccountDeletedHook runs after the deletion transaction commits, for work
// that must not happen inside it: revoking realtime subscriptions, calling
// providers, deleting objects outside the user's storage prefix. Best-effort:
// errors are logged.
type AccountDeletedHook interface {
	AccountDeleted(ctx context.Context, userID uuid.UUID) error
}

// AccountExporter returns the module's data about a user for GET
// /v1/me/export. The value is JSON-encoded under the module's Name().
type AccountExporter interface {
	ExportAccount(ctx context.Context, userID uuid.UUID) (any, error)
}

// DevSeeder adds realistic development rows (cmd/seed only, never in
// production). users are the dev identities in auth.DevIdentities order.
// It must be safe to run repeatedly.
type DevSeeder interface {
	SeedDev(ctx context.Context, users []uuid.UUID) error
}

// Tool is one capability an agent may call on the user's behalf. A module
// declares its tools once; the mcp feature serves them over MCP and the
// assistant feature hands them to the model, so both stay in step with the
// API. The handler gets the caller from the Identity, never from input.
type Tool struct {
	Name        string          // snake_case, prefixed with the module: chat_list_conversations
	Description string          // one or two sentences for the model
	InputSchema json.RawMessage // JSON Schema (draft 2020-12) object for the input
	ReadOnly    bool            // true when the tool changes nothing
	Handler     func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error)
	// Source names the outside service that serves the tool ("Linear") when
	// it isn't this app (DynamicToolProvider tools from a connected server).
	// Agents show it, treat the output as third-party content, and ask the
	// user before each call when ReadOnly is false. Empty for the app's own.
	Source string
	// Feature is the agent-scope key (AgentPolicy) when it isn't the
	// providing module's Name(): the connectors feature sets
	// "connectors:<connector id>" so each external service is scoped on its
	// own. Agents use Feature, else the module name.
	Feature string

	// Target names the one resource a call acts on, read from its input
	// ("docs.page", "<uuid>"), so a per-resource agent scope applies to it.
	// Leave nil for tools that span many resources (searches, lists); their
	// queries filter by agent scope themselves (AgentPolicy, access helpers).
	Target func(input json.RawMessage) (kind, id string)
	// Preview describes what a write call would change, for the suggestion a
	// user reviews when their agent scope is "suggest": a one-line summary and
	// optional before/after text. Nil falls back to the tool name and input.
	Preview func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (ToolPreview, error)
}

// ToolPreview is the human-readable form of a pending write.
type ToolPreview struct {
	Summary string `json:"summary"`          // "Rename “Q3 plan” to “Q3 plan (final)”"
	Before  string `json:"before,omitempty"` // plain text; the reviewer sees a diff
	After   string `json:"after,omitempty"`
	Link    string `json:"link,omitempty"` // app path of the target
}

// ToolProvider exposes a module's agent tools. Never expose a tool that
// deletes user data, sends anything outward, or fetches arbitrary URLs.
type ToolProvider interface {
	Tools() []Tool
}

// DynamicToolProvider exposes tools that differ per caller and can't be
// listed once at boot (a user's connected external MCP servers). The
// assistant feature asks it per conversation turn and merges the result with
// every ToolProvider's tools. The same rules apply: run as the caller, never
// widen what the caller could reach themselves.
type DynamicToolProvider interface {
	ToolsFor(ctx context.Context, caller *auth.Identity) ([]Tool, error)
}

// SearchResult is one hit a module returns to global search. Link is an
// app-relative path the clients open (/docs/<id>). Score orders hits across
// modules (0..1, higher first); Snippet is plain text, never HTML.
type SearchResult struct {
	Kind    string    `json:"kind"` // module-prefixed: docs.page, crm.contact
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Snippet string    `json:"snippet,omitempty"`
	Link    string    `json:"link"`
	Icon    string    `json:"icon,omitempty"` // lucide icon name
	Score   float64   `json:"score"`
	At      time.Time `json:"at"` // last activity, for tie-breaks and display
	// OrgID is the organization the hit belongs to, when it belongs to one.
	// Search scoped to an org (SearchOrg) drops hits whose OrgID names another.
	OrgID string `json:"org_id,omitempty"`
	// Context says where the hit lives, for people: the org's name first when
	// OrgID is set, then its container, joined by SearchContext
	// ("Riverside Climbing Club · #general"). Clients hide the org part when
	// the search is scoped to that org. Optional.
	Context string `json:"context,omitempty"`
}

// SearchContext joins the non-empty parts of a SearchResult.Context with
// " · ": SearchContext(orgName, "#general").
func SearchContext(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// SearchProvider contributes hits to global search (the search feature's
// GET /v1/search). Return only what caller may open: the same permission
// checks as the module's own list routes. limit is a per-module cap. When
// SearchOrg(ctx) names an org, return that org's hits (and ones that belong
// to none); a provider that can't filter by org returns everything and
// search drops hits whose OrgID names another org.
type SearchProvider interface {
	Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]SearchResult, error)
}

type searchOrgKey struct{}

// WithSearchOrg scopes a search to one org (GET /v1/search?org_id=).
func WithSearchOrg(ctx context.Context, orgID uuid.UUID) context.Context {
	return context.WithValue(ctx, searchOrgKey{}, orgID)
}

// SearchOrg is the org a search is scoped to, if any.
func SearchOrg(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(searchOrgKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// AgentLevel is how far a user lets AI agents acting for them go, per
// feature or per resource. It only ever narrows the user's own permissions.
type AgentLevel int

const (
	AgentOff     AgentLevel = iota // agents can't see it
	AgentRead                      // read tools only (the default)
	AgentSuggest                   // writes become suggestions the user accepts or rejects
	AgentEdit                      // writes run (still never sharing or permission changes)
)

// ToolAction is the outcome of AgentPolicy.GateTool.
type ToolAction string

const (
	ToolRun       ToolAction = "run"       // call the handler
	ToolRefuse    ToolAction = "refuse"    // return Code/Message as a tool error
	ToolSuggested ToolAction = "suggested" // recorded for the user; tell the agent SuggestionID
)

type ToolDecision struct {
	Action       ToolAction
	Code         string // refuse: agent_scope_off | agent_scope_read_only | …
	Message      string
	SuggestionID string
}

// AgentPolicy applies users' agent scopes. One module implements it (the
// agents feature); the mcp and assistant features find it in Deps.Modules
// (FindAgentPolicy) and call it around every tool. Without one, tools run
// as before.
type AgentPolicy interface {
	// FeatureLevel is the user's level for a feature (module name, or
	// "connectors:<id>" for one external service) as a whole, and the highest
	// level any resource override in it reaches: hide every tool when max is
	// AgentOff, write tools when max < AgentSuggest. max is only an upper
	// bound: a resource override may be lower than base (one channel Off in a
	// Read feature), so base == max never means "every resource is at base".
	// Only max < need lets a caller skip per-resource checks.
	FeatureLevel(ctx context.Context, caller *auth.Identity, feature string) (base, max AgentLevel, err error)
	// ResourceLevel resolves one resource: its override, else its parent's
	// (access resources inherit), else the feature's, else the default.
	ResourceLevel(ctx context.Context, caller *auth.Identity, feature, kind, id string) (AgentLevel, error)
	// ResourceLevels resolves many resources of one kind in one go (a page
	// of list rows or search hits), with the same rules as ResourceLevel.
	// The map has an entry for every id.
	ResourceLevels(ctx context.Context, caller *auth.Identity, feature, kind string, ids []string) (map[string]AgentLevel, error)
	// GateTool runs before every agent tool call (ctx carries auth.Agent).
	GateTool(ctx context.Context, caller *auth.Identity, feature string, t Tool, input json.RawMessage) (ToolDecision, error)
}

// FindAgentPolicy returns the installed AgentPolicy, or nil.
func FindAgentPolicy(modules []Module) AgentPolicy {
	for _, m := range modules {
		if p, ok := m.(AgentPolicy); ok {
			return p
		}
	}
	return nil
}

// AgentAllows reports whether the request may touch (kind, id) of feature
// at level need. It is true for requests that aren't agent calls and when
// no policy is installed. Features whose rows aren't access resources use it
// to drop rows from agent reads (need = AgentRead).
func AgentAllows(ctx context.Context, modules []Module, caller *auth.Identity, feature, kind, id string, need AgentLevel) (bool, error) {
	if _, ok := auth.AgentFrom(ctx); !ok {
		return true, nil
	}
	p := FindAgentPolicy(modules)
	if p == nil {
		return true, nil
	}
	lvl, err := p.ResourceLevel(ctx, caller, feature, kind, id)
	if err != nil {
		return false, err
	}
	return lvl >= need, nil
}

// AgentReadable returns which of ids (one kind) the request may read: all of
// them when it isn't an agent call or no policy is installed, otherwise
// those whose agent level is at least AgentRead. Use it to filter a page of
// rows that aren't access resources (access-backed queries filter in SQL).
func AgentReadable(ctx context.Context, modules []Module, caller *auth.Identity, feature, kind string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	_, agent := auth.AgentFrom(ctx)
	p := FindAgentPolicy(modules)
	if !agent || p == nil {
		for _, id := range ids {
			out[id] = true
		}
		return out, nil
	}
	levels, err := p.ResourceLevels(ctx, caller, feature, kind, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		out[id] = levels[id] >= AgentRead
	}
	return out, nil
}
