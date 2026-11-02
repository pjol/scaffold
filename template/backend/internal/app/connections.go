package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

// Connected platforms: people link their own accounts on outside platforms
// (Google Workspace, Microsoft 365, GitHub, GitLab, Notion, HubSpot,
// Salesforce, Zoom…) once, and every feature that can extend into that
// platform uses the same connection. One module (the connectors feature)
// implements Connections and keeps every token; features hold platform
// adapters and ask it for an authenticated client, so a feature never depends
// on the connectors feature and works without it (no connected sources).
//
// Content from a connection is read and written live with the person's own
// token: they (and their agents) reach exactly what that account can reach on
// the platform. Agents are further narrowed by the connection's agent scope
// (feature key ConnectionScope(id)) and the owning feature's.

// Capability is one thing a feature can do with a platform once a person
// connects it, declared by the feature (CapabilityProvider) so the connections
// screen can offer it and request its scopes incrementally.
type Capability struct {
	Key      string   // "<module>.<provider>.<what>": files.google.drive, crm.hubspot.records
	Module   string   // owning module's Name()
	Provider string   // platform id in the connections registry: google, microsoft, github, gitlab, notion, hubspot, salesforce, zoom
	Label    string   // what people see: "Google Drive in Files"
	Scopes   []string // the platform's OAuth scopes this capability needs
	Write    bool     // it can change data on the platform
}

// CapabilityProvider declares a module's platform capabilities.
type CapabilityProvider interface {
	Capabilities() []Capability
}

// Connection is one person's link to one account on a platform.
type Connection struct {
	ID        string            `json:"id"`         // stable; the agent-scope key is ConnectionScope(ID)
	Provider  string            `json:"provider"`   // google, github, …
	Platform  string            `json:"platform"`   // the platform's name for people: Google, GitHub
	Account   string            `json:"account"`    // display: sam@acme.com, acme-org, Acme (Notion workspace)
	AccountID string            `json:"account_id"` // the platform's stable id for the account
	Scopes    []string          `json:"scopes"`     // granted
	Status    string            `json:"status"`     // active | needs_reauth | blocked (workspace policy)
	Endpoints map[string]string `json:"-"`          // API bases by name ("drive", "calendar", "api"); per connection for self-hosted GitLab or a Salesforce instance
}

// Connections is implemented by one module. Every method checks the
// connection belongs to userID and grants the capability; tokens never leave it.
type Connections interface {
	// For lists userID's connections that grant capability (active ones and
	// ones needing re-authorisation, so screens can say so), within the
	// workspace's policy for orgID (uuid.Nil = no workspace context).
	For(ctx context.Context, userID, orgID uuid.UUID, capability string) ([]Connection, error)
	// Client returns an HTTP client that signs requests with the connection's
	// token (refreshing it as needed), reaches only the platform's API hosts,
	// and backs off on rate limits. Failures are *httpx.Error values with a
	// Connection* code (ConnectionNeedsAuth when the person must reconnect
	// or grant the capability). Workspace policy: the org in
	// WithConnectionOrg(ctx, orgID) when set, else every org the person
	// belongs to.
	Client(ctx context.Context, userID uuid.UUID, connectionID, capability string) (*http.Client, Connection, error)
	// Offers lists capabilities of installed modules the person could still
	// connect or grant in orgID, with the app path that starts it.
	Offers(ctx context.Context, userID, orgID uuid.UUID, module string) ([]ConnectionOffer, error)
}

// ConnectionDirectory lists every platform account a person has connected,
// whatever its capabilities or status, for screens that manage accounts as a
// whole (the agents feature's settings list one AI agent level per account).
// The Connections module implements it too; it is a separate interface so
// Connections implementations without it keep compiling.
type ConnectionDirectory interface {
	All(ctx context.Context, userID uuid.UUID) ([]Connection, error)
}

// FindConnectionDirectory returns the installed ConnectionDirectory, or nil.
func FindConnectionDirectory(modules []Module) ConnectionDirectory {
	for _, m := range modules {
		if c, ok := m.(ConnectionDirectory); ok {
			return c
		}
	}
	return nil
}

// ConnectionLabel is how people see an account: "Google · sam@acme.com".
func ConnectionLabel(c Connection) string {
	name := c.Platform
	if name == "" {
		name = c.Provider
	}
	if c.Account == "" {
		return name
	}
	return name + " · " + c.Account
}

// ExternalTarget is the agent-scope target id of an item on a connected
// platform: "<connection id>:<platform id>", with kind "<module>.external"
// (Tool.Target), so the agent gate also applies the connection's own level.
func ExternalTarget(connectionID, platformID string) string { return connectionID + ":" + platformID }

// TargetConnection returns the connection an agent-scope target names: kind
// "<module>.external" and an id that starts with "<connection uuid>:".
func TargetConnection(kind, id string) (string, bool) {
	if !strings.HasSuffix(kind, ".external") {
		return "", false
	}
	conn, _, ok := strings.Cut(id, ":")
	if !ok {
		return "", false
	}
	u, err := uuid.Parse(conn)
	if err != nil || len(conn) != 36 {
		return "", false
	}
	return u.String(), true
}

// ConnectionOffer is a platform capability a person hasn't connected yet.
type ConnectionOffer struct {
	Capability string `json:"capability"`
	Provider   string `json:"provider"`
	Label      string `json:"label"`
	ConnectURL string `json:"connect_url"` // app path, e.g. /settings/connections?capability=files.google.drive
}

// ConnectionScope is the agent-scope feature key for one connection, so a
// person can set what agents may do with each connected account.
func ConnectionScope(connectionID string) string { return "connectors:" + connectionID }

// FindConnections returns the installed Connections, or nil (no connected
// sources: features offer none).
func FindConnections(modules []Module) Connections {
	for _, m := range modules {
		if c, ok := m.(Connections); ok {
			return c
		}
	}
	return nil
}

// ---- errors ------------------------------------------------------------------

// Error codes of the *httpx.Error values Connections returns (from For,
// Client, and requests made with a Client). Features pass them through to
// clients, which offer the fix (Settings → Connections) by code.
const (
	ConnectionNeedsAuth    = "connection_needs_auth"    // 409: reconnect (expired, revoked, or the token stopped working)
	ConnectionScopeMissing = "connection_scope_missing" // 409: the capability is off or its scopes aren't granted: turn it on
	ConnectionBlocked      = "connection_blocked"       // 403: the workspace's policy doesn't allow this platform or capability
	ConnectionNotFound     = "connection_not_found"     // 404: no such connection for this person
	ConnectionUnavailable  = "connection_unavailable"   // 502/503: the platform isn't set up here, or didn't answer
	ConnectionRateLimited  = "connection_rate_limited"  // 429: the platform is limiting requests even after backing off
	ConnectionHostRefused  = "connection_host_refused"  // 403: a request to a host outside the platform's API hosts
	ConnectionTooLarge     = "connection_response_too_large"
)

// ConnectionErrorCode returns the Connection* code of err (through any
// wrapping, such as the *url.Error an http.Client returns), or "".
func ConnectionErrorCode(err error) string {
	var e *httpx.Error
	if errors.As(err, &e) {
		switch e.Code {
		case ConnectionNeedsAuth, ConnectionScopeMissing, ConnectionBlocked, ConnectionNotFound, ConnectionUnavailable,
			ConnectionRateLimited, ConnectionHostRefused, ConnectionTooLarge:
			return e.Code
		}
	}
	return ""
}

// ---- workspace context -------------------------------------------------------

type connectionOrgKey struct{}

// WithConnectionOrg tells Client which workspace a request acts in, so that
// workspace's connection policy applies (features pass the active org).
func WithConnectionOrg(ctx context.Context, orgID uuid.UUID) context.Context {
	return context.WithValue(ctx, connectionOrgKey{}, orgID)
}

// ConnectionOrg is the workspace set by WithConnectionOrg, if any.
func ConnectionOrg(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(connectionOrgKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// ---- sources on rows -----------------------------------------------------------

// SourceRef says where a row came from when it isn't the app's own: one
// person's connected account on a platform. JSON matches the shared
// SourceRef ({"type":"connected",…}); app rows carry no source (nil).
type SourceRef struct {
	Type         string `json:"type"`              // always "connected"
	Provider     string `json:"provider"`          // google, github, …
	ConnectionID string `json:"connection_id"`     // Connection.ID
	Account      string `json:"account"`           // Connection.Account
	Product      string `json:"product,omitempty"` // drive, sharepoint, calendar… when it matters
	URL          string `json:"url,omitempty"`     // the item's page on the platform, when it has one
}

// ConnectedSource is the SourceRef for a row read through conn.
func ConnectedSource(conn Connection, product, url string) *SourceRef {
	return &SourceRef{Type: "connected", Provider: conn.Provider, ConnectionID: conn.ID, Account: conn.Account,
		Product: product, URL: url}
}

// ---- webhooks (optional) -------------------------------------------------------

// ConnectionWebhookHandler receives platform webhooks for modules that
// subscribe to them (polling with the person's Client is the default; use
// webhooks only to learn about changes sooner). The connectors feature
// serves POST /webhooks/connectors/{provider}, verifies the platform's
// signature (refusing when its secret is unset), drops replays, and hands
// the event to every module whose ConnectionWebhooks lists that provider.
// Treat the payload as a hint: re-read current state through Client.
type ConnectionWebhookHandler interface {
	ConnectionWebhooks() []ConnectionWebhook
}

// ConnectionWebhook subscribes one module to one platform's webhooks.
type ConnectionWebhook struct {
	Provider string
	Handle   func(ctx context.Context, e ConnectionEvent) error
}

// ConnectionEvent is one verified webhook delivery.
type ConnectionEvent struct {
	Provider   string
	Type       string // the platform's event name (X-GitHub-Event, object_kind, subscriptionType…)
	DeliveryID string // the platform's delivery id when it sends one
	Header     http.Header
	Body       json.RawMessage
	// Connections are the people's connections to the account the event is
	// about, when the platform says which (AccountID matched); empty when
	// it can't be told. Each still goes through Client as its own person.
	Connections []ConnectionOwner
}

// ConnectionOwner names one connection and the person it belongs to.
type ConnectionOwner struct {
	ConnectionID string
	UserID       uuid.UUID
}
