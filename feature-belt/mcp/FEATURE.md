# MCP, connected apps and API keys

## What it does

- People add the app to Claude.ai, ChatGPT, Claude Code, Cursor or any MCP client **by URL** (`<PUBLIC_API_URL>/mcp`) and sign in with their own account on a consent page that names the app, where it returns to and what it asks for (read; optionally changes). The agent then acts as that person: every tool runs as them and every feature applies its own permission checks.
- Settings → Connected apps lists each authorized app with its access, when it was connected and last used, and disconnects it (every token, every device). The server URL to add sits on top.
- Settings → API keys: personal keys (read only, or read and write) for scripts and clients without sign-in. The secret shows once. Up to 10 active keys.
- Read-only connections see only read-only tools; write tools also need current legal consent. Suspended, banned and deleted accounts are refused on every call, checked live.
- With the `agents` feature installed, every call also passes the person's AI agent scope (off / read / suggest / edit, per feature and resource): hidden features' tools aren't listed, and in suggest mode a write becomes a suggestion the person accepts or rejects instead of running. The key or token scope stays a ceiling on top.
- Staff list connected apps and keys across users and revoke them; revocations go to the audit log.
- Web only: mobile ships no UI (connecting agents is a desktop concern; the consent page works in a phone browser).

## Data

| Table | Purpose |
|---|---|
| `mcp_api_keys` | Owner, name, display prefix, SHA-256 of the secret, scopes, last use (once a minute), revocation and the staff member who revoked |
| `mcp_oauth_clients` | Registered (RFC 7591) or metadata-document clients: id, self-declared name, redirect URIs, auth method + secret hash (confidential only), cache expiry, last use |
| `mcp_oauth_requests` | Authorization requests parked during sign-in and consent (10 min): client, redirect URI, state, PKCE challenge, scopes, resource |
| `mcp_oauth_grants` | One per approval: person, client, scopes, audience, expiry, first/last use, revocation (who, why: user, staff, client, refresh or code replay) |
| `mcp_oauth_codes` | Single-use codes (5 min) bound to client, redirect URI and PKCE; keeps the grant it produced so a replay revokes it |
| `mcp_oauth_tokens` | Access/refresh pairs (hashes only); rotated refresh tokens stay until expiry so replay is detected |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST, GET, DELETE | `/mcp` | root (per-IP) + bearer in the handler, 120/min per connection | Streamable-HTTP MCP, stateless, JSON responses |
| GET | `/.well-known/oauth-protected-resource/mcp`, `/.well-known/oauth-protected-resource` | public, CORS `*` | RFC 9728 metadata; both name the MCP endpoint as the resource |
| GET | `/.well-known/oauth-authorization-server` | public, CORS `*` | RFC 8414 metadata: PKCE S256, `client_id_metadata_document_supported`, `authorization_response_iss_parameter_supported` |
| POST | `/oauth/register` | open, `MCP_OAUTH_REGISTRATIONS_PER_HOUR` per IP, unused-client cap | RFC 7591 DCR; `none` (default), `client_secret_post`, `client_secret_basic` |
| GET | `/oauth/authorize` | public | Validates client (an https `client_id` is fetched as a metadata document), redirect URI, PKCE, scope, `resource`; parks the request; 302 to `PUBLIC_WEB_URL/connect?request=…` |
| POST | `/oauth/token` | client auth, 120/min per client | `authorization_code` (PKCE S256 required, `resource` must match) and `refresh_token` (rotation; replay revokes the grant; may narrow scope) |
| POST | `/oauth/revoke` | client auth | RFC 7009: ends the grant behind an access or refresh token; always 200 |
| GET | `/v1/mcp/oauth/request?id=` | user | The consent page's view of a request (404 `request_expired`) |
| POST | `/v1/mcp/oauth/decision` | user, 30/min; allow needs active + consent | `{id, allow, scopes?}` → `{redirect_url}` with a code, or `access_denied` |
| GET, DELETE | `/v1/mcp/connections`, `/v1/mcp/connections/{id}` | user | Connected apps (one row per client); disconnect ends every grant to that client |
| GET, POST, DELETE | `/v1/mcp/keys`, `/v1/mcp/keys/{id}` | user (create: active, 10/hour) | API keys; 409 `key_limit` at 10 |
| GET, DELETE | `/v1/admin/mcp/connections[/{id}]?user_id=&status=` | support | Grants across users; revoke audited as `mcp.oauth.revoke` |
| GET, DELETE | `/v1/admin/mcp/keys[/{id}]?user_id=&status=` | support | Keys across users; revoke audited as `mcp.key.revoke` |
| GET | `/.well-known/mcp.json` | public, CORS `*` | Server card: endpoint, both auth methods, scopes, tool summaries |

`/mcp` errors: 401 `unauthenticated` with `WWW-Authenticate: Bearer resource_metadata="…/oauth-protected-resource/mcp", scope="read write"` (plus `error="invalid_token"` for a bad token); 403 `account_suspended`; a read-only OAuth token calling a write tool gets 403 `insufficient_scope` with the scope challenge (clients step up), a read-only key 403 `scope_required`; 429 `rate_limited`. Tool failures are MCP results with `isError: true` and text `code: message` (`consent_required`, `invalid_input`, `timeout`, or the tool's `*httpx.Error` code); results carry JSON as text and `structuredContent`.

Agent scope (when an `app.AgentPolicy` is installed, found with `app.FindAgentPolicy` at request time): every request carries `auth.Agent{Via: "mcp", Client: <OAuth client or key name>, ClientID: <client_id or key id>}`; `tools/list` hides a module's tools when its `FeatureLevel` max is off and its write tools below suggest (feature = the providing module's `Name()`); each `tools/call` runs `GateTool` after the scope and consent checks: run, refuse (`isError`, `code: message`), or suggested (not an error: "Suggested to <name> for review (suggestion <id>)…" with `structuredContent {"status":"suggested","suggestion_id"}`). Without a policy, tools run as before.

**Agent credentials only.** Every credential an agent holds is minted with `auth.AgentCredentialPrefix`: access tokens `agt_scaffoldapp_at_…`, refresh tokens `agt_scaffoldapp_rt_…`, API keys `agt_scaffoldapp_sk_…`, client secrets `agt_scaffoldapp_cs_…` (opaque, stored as SHA-256). The core auth middleware refuses them on every user route, uploads and the WebSocket (401 `agent_credential`); `/mcp` accepts nothing else: not a person's session token, not a refresh token, not a token whose audience isn't exactly the MCP endpoint (RFC 8707; both metadata documents name it, and `resource` at authorize and token must equal it), not one carrying a scope other than `read`/`write`. `/mcp` takes POST only (405 otherwise) and runs only `initialize`, `ping`, `tools/list` and `tools/call` (notifications are accepted and ignored); any other method gets JSON-RPC -32601, so no resource or prompt can reach the REST API. Bodies are capped at 1 MiB (413).

## Screens

- **Web** `/connect` (public route, CenteredScreen): signed out → sign-in with return; Terms not accepted → the consent screen first; then the app's name (ⓘ: self-declared, or "published by <host>" for metadata documents), where it returns to, the account with Switch account, "See what you can see" and a "Make changes as you" switch when write is requested, a warning for loopback and app-scheme redirects, Allow and Deny; error states for missing, expired, unknown-client and redirect-mismatch requests. `/settings/connected-apps`: server URL with copy, app list (skeleton, empty, error, load more); each row's ⋮ is the core `ResourceMenu` (feature `mcp`, kind `mcp.client`, the client's id) holding Disconnect (confirmed) and whatever other features add there. `/settings/api-keys` as before. `/admin/connected-apps` and `/admin/api-keys`: tables with user and status filters and audited revoke.
- **Mobile**: none.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `MCP_RATE_LIMIT_PER_MINUTE` | `120` | Requests per key or connection per minute |
| `MCP_OAUTH_ACCESS_TTL` | `1h` | Access token lifetime (1m–24h) |
| `MCP_OAUTH_REFRESH_TTL` | `720h` | Refresh token lifetime; each refresh issues a new one |
| `MCP_OAUTH_REGISTRATIONS_PER_HOUR` | `60` | DCR requests per IP per hour |
| `MCP_OAUTH_MAX_UNUSED_CLIENTS` | `5000` | Never-used registered clients allowed before registration pauses |
| `MCP_OAUTH_CLIENT_METADATA_ENABLED` | `true` | Accept https `client_id` URLs (fetched with a private-address guard, 5 KB, 5 s, no redirects) |

`PUBLIC_API_URL` (core) is the issuer and the resource; `PUBLIC_WEB_URL` (core) hosts `/connect`, and OAuth is off when it is unset. Go module `github.com/modelcontextprotocol/go-sdk v1.8.0`; no npm packages.

## Compliance

- Account deletion deletes keys, grants (with their tokens) and pending codes. Export lists key and connected-app metadata, never secrets, tokens or hashes.
- Agents act with the person's permissions only, narrowed further by their agent scope when the `agents` feature is installed; write tools need the write scope and current consent. Status is checked on every exchange and call.
- Agent credentials can't act as the person anywhere but `/mcp` (the `agt_` prefix is refused by the core auth middleware), and minting keys, consenting to a client and disconnecting apps are user routes only a person's session reaches (concrete/auth.md → Agent credentials).
- Upgrading from 1.0.0 revokes keys minted without the `agt_` prefix (migration 0601); owners create new ones.
- Secrets and tokens are never logged; request handles travel in query strings (request logs record paths only).

## Stitching

1. Give the features agents should use an `app.ToolProvider` with prescriptive descriptions; mark tools that change data `ReadOnly: false`.
2. Rewrite the server instructions, `resource_name` and card description in `backend/internal/mcp/{mcp,oauth}.go` and the consent copy in `web/src/features/mcp/ConnectScreen.tsx` for this app.
3. Serve the API at the origin `PUBLIC_API_URL` names (no path prefix): `/mcp`, `/.well-known/*` and `/oauth/*` live at its root.
4. Set `PUBLIC_WEB_URL`; `/connect` is in `proxy.ts` public paths so Clerk's middleware doesn't replace the relative return path.
5. Decide whether to allow client ID metadata documents (outbound fetches) and adjust TTLs to the app's risk.

## Extending

- **Agent scope gate**: the policy lives in the `agents` feature (`app.AgentPolicy`); this package only marks calls (`auth.WithAgent`), filters `tools/list` by `FeatureLevel` and runs `GateTool` per call. Handlers and search providers read `auth.AgentFrom(ctx)` to filter rows; tools that act on one resource set `Tool.Target` (and `Tool.Preview` for suggestions).
- New tools belong in the owning feature's `Tools()`; this package only adapts them. Scopes are `read` and `write`; finer scopes mean a check per tool in `toolHandler` and `writeToolCalled`.
- Out of scope: OIDC (no id_token or `/.well-known/openid-configuration`; MCP clients fall back to RFC 8414), `private_key_jwt`, token introspection, browser-based clients that need CORS on `/oauth/register` and `/mcp` (the root group follows the API's CORS list), and trust policies for metadata documents (any public https host is accepted; the consent page names it).
- Dynamically registered clients name themselves; a client that re-registers on each connect shows as a new row. Metadata-document clients (Claude Code, VS Code) are one row each.
- Don't accept keys or OAuth tokens on the REST API's user routes, and never let either create keys or approve connections.

## Provenance

Keys distilled from MOX (`internal/mcpserver`, `internal/access`): hashed keys with display prefixes, per-scope tool lists, `code: message` errors, the server card. OAuth distilled from OLEARY/crm (`handlers_mcp_oauth.go`, `store/mcpoauth.go`, `0004_mcp_oauth.sql`): this server as the authorization server with the app's sign-in bridged on a web page, hashed single-use values spent in the reading statement, live status checks. Generalised from operators-only to every user and brought to the 2025-11-25 spec: audience binding, step-up 403s, client ID metadata documents behind an SSRF guard, loopback any-port and native-app redirects, confidential clients, refresh replay revoking the grant, code replay revoking its tokens, RFC 7009, `iss` in responses, grants a person can see and disconnect. Verified with the official Go SDK's OAuth client (DCR and metadata documents, including Claude Code's published document) and Playwright on the consent page.

## Changes

- 1.1.0: OAuth 2.1 authorization server for remote connectors (discovery, DCR, metadata documents, consent page, PKCE, resource binding, rotating refresh tokens with replay detection, revocation); Settings → Connected apps (⋮ `ResourceMenu`); admin connected apps; every tool call marked `auth.Agent` and gated by the installed `app.AgentPolicy` (list filtering, refuse, suggest); read-only keys calling a write tool get 403 `scope_required` instead of "unknown tool"; agent credentials carry the `agt_` prefix and `/mcp` accepts only them, audience-bound, POST-only and tools-only (older keys are revoked).
- 1.0.0: endpoint and card served at the root (`app.RootRoutes`); settings and admin screens; shared client.
- 0.1.0: backend at `/v1/mcp`.
