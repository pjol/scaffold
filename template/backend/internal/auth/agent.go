package auth

import (
	"context"
	"net/http"
	"strings"

	"scaffoldapp/backend/internal/httpx"
)

// Agent marks a request made by an AI agent acting for the signed-in user:
// a client connected over MCP, or the in-app assistant. The mcp and
// assistant features put it on the context before running a tool, so every
// handler (and the access SQL helpers) can apply the user's agent scope on
// top of the user's own permissions. An agent never gets more than its user.
type Agent struct {
	Via      string // "mcp" | "assistant"
	Client   string // display name: "Claude", "Assistant", the API key's name
	ClientID string // stable id of the client or key, for audit; "" when none
}

const agentKey ctxKey = iota + 100

// WithAgent returns ctx marked as an agent call.
func WithAgent(ctx context.Context, a Agent) context.Context {
	return context.WithValue(ctx, agentKey, a)
}

// AgentFrom reports whether ctx is an agent call, and which.
func AgentFrom(ctx context.Context) (Agent, bool) {
	a, ok := ctx.Value(agentKey).(Agent)
	return a, ok
}

// AgentCredentialPrefix starts every credential minted for an AI agent (MCP
// OAuth access and refresh tokens, personal API keys): agt_<slug>_<kind>_….
// Authenticator.Middleware refuses any bearer token with it, so an agent's
// credential can only be used where the issuing feature accepts it (/mcp),
// never on the user routes a person's session reaches.
const AgentCredentialPrefix = "agt_"

// IsAgentCredential reports whether token was minted for an agent.
func IsAgentCredential(token string) bool { return strings.HasPrefix(token, AgentCredentialPrefix) }

var errAgentCredential = httpx.NewError(http.StatusUnauthorized, "agent_credential",
	"This credential is for AI agents. It works only at the agent endpoint.")
