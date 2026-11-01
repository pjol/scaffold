// Package app holds the dependency container handed to every module and the
// optional interfaces a module implements to plug into the API. The router
// discovers capabilities by type assertion; a module implements only what it
// needs.
package app

import (
	"context"
	"encoding/json"

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
// (spawn goroutines). Work that must run on one instance at a time takes
// db.TryAdvisoryLock; see jobs.Every.
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
}

// ToolProvider exposes a module's agent tools. Never expose a tool that
// deletes user data, sends anything outward, or fetches arbitrary URLs.
type ToolProvider interface {
	Tools() []Tool
}
