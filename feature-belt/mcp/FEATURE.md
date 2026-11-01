# MCP and API keys

## What it does

- Users create personal API keys in Settings → API keys (name; read only, or read and write). The secret is shown once with a copy button, next to the MCP endpoint URL. The list shows name, prefix, access, created and last used; revoke any time. Up to 10 active keys.
- Agents (Claude, ChatGPT, scripts) connect to the app's MCP endpoint with `Authorization: Bearer <key>` and call every installed feature's agent tools (`app.ToolProvider`) as the key's owner. Write tools appear only for read-and-write keys.
- Suspended, banned and deleted accounts can't use keys. Staff list keys across users (filter by user and status) and revoke them; revocations go to the audit log.
- `/.well-known/mcp.json` describes the endpoint, auth and tools.
- Web only: keys are a desktop-agent concern, so mobile ships no UI.

## Data

| Table | Purpose |
|---|---|
| `mcp_api_keys` | Owner, name, display prefix, SHA-256 of the secret, scopes, last use (written at most once a minute), revocation and the staff member who revoked |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST, GET, DELETE | `/mcp` | `app.RootRoutes` (per-IP limit) + API key in the handler, 120/min per key | Streamable-HTTP MCP endpoint, stateless, JSON responses |
| GET | `/.well-known/mcp.json` | public, CORS `*`, cached 5 min | Server card: endpoint URL (`PUBLIC_API_URL` + `/mcp`), auth format, scopes, tool summaries |
| GET | `/v1/mcp/keys?cursor=` | user | Active keys, newest first |
| POST | `/v1/mcp/keys` | user, active, 10/hour | `{name, scopes}` (`["read"]` or `["read","write"]`) → 201 `{key, secret}`; 409 `key_limit` at 10 |
| DELETE | `/v1/mcp/keys/{id}` | owner | Revoke |
| GET | `/v1/admin/mcp/keys?user_id=&status=active\|revoked&cursor=` | support | Keys across users with owner email |
| DELETE | `/v1/admin/mcp/keys/{id}` | support | Revoke, audited as `mcp.key.revoke` |

Endpoint errors before MCP: 401 `unauthenticated` (with `WWW-Authenticate: Bearer`), 403 `account_suspended`, 429 `rate_limited`. Tool failures come back as MCP tool results with `isError: true` and text `code: message` (`scope_required`, `consent_required` for write tools until the current Terms are accepted, `invalid_input` after JSON Schema validation, `timeout` after 15 s, or the tool's own `*httpx.Error` code). Results carry the tool's JSON as text and as `structuredContent` (non-objects wrapped as `{"result": …}`).

## Screens

- **Web** `/settings/api-keys` (Settings section): endpoint with copy, key list (skeleton, empty with Create key, error), create dialog (name, access) that turns into a one-time secret view, revoke confirmation. `/admin/api-keys` (admin tab): table with user ID and status filters, owner link to the user page, revoke confirmation naming the owner.
- **Mobile**: none.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `MCP_RATE_LIMIT_PER_MINUTE` | `120` | Requests per key per minute |

`PUBLIC_API_URL` (core) is the address the server card advertises. Go modules: `github.com/modelcontextprotocol/go-sdk v1.8.0` (brings `github.com/google/jsonschema-go`, used to validate tool arguments); `scaffold add` runs `go mod tidy`. No npm packages.

## Compliance

- Account deletion deletes the user's keys. Export lists key metadata, never secrets or hashes.
- Keys act with the owner's permissions only; write tools need the write scope and current legal consent.
- Secrets are never logged; the secret leaves the server once, in the create response, and the dialog forgets it on close.

## Stitching

1. Give the features agents should use an `app.ToolProvider` with prescriptive descriptions; mark tools that change data `ReadOnly: false`.
2. Rewrite the server instructions and card description in `backend/internal/mcp/mcp.go` for this app.
3. Make sure `PUBLIC_API_URL` is the public address agents reach (`/mcp` and `/.well-known/mcp.json` live at the API's root).

## Extending

- **OAuth for remote connectors (Claude.ai, ChatGPT):** not implemented. Clerk has no dynamic client registration, so the app runs its own small authorization server and bridges sign-in to Clerk: RFC 8414 metadata, open dynamic client registration (it grants nothing but redirect URIs), `GET /oauth/authorize` that sends the browser to a web page where the user signs in with Clerk and consents, a Clerk-authenticated `POST /oauth/complete` that issues the code, and `POST /oauth/token` with PKCE S256 and rotating refresh tokens; the `/mcp` 401 then carries `WWW-Authenticate: Bearer resource_metadata=…`. Reference: OLEARY/crm `backend/internal/api/handlers_mcp_oauth.go`, `backend/internal/store/mcpoauth.go`, `backend/migrations/0004_mcp_oauth.sql`. Resolve OAuth access tokens to the same `principal` in `authenticate` so tools stay unchanged; the root mount already serves the `.well-known` paths it needs.
- New tools belong in the owning feature's `Tools()`; this package only adapts them.
- Don't accept API keys on the REST API's user routes, and never let a key create or list keys.

## Provenance

Distilled from MOX (`docs/MCP.md`, `internal/mcpserver`, `internal/access`, `migrations/0005_ai_access.sql`): hashed personal keys with display prefixes, per-scope tool lists, `code: message` tool errors, the server card. Rebuilt on the official Go SDK (as in the scaffold CLI's MCP adapter) instead of `mcp-go`; one authenticated endpoint instead of a public and a trading one; admin listing and audited revocation added; key creation serialised per user so the 10-key limit holds under concurrency; last-use writes throttled.

## Changes

- 1.0.0: endpoint and card served at the root (`app.RootRoutes`); settings and admin screens; shared client.
- 0.1.0: backend at `/v1/mcp`.
