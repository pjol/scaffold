# Connectors

## What it does

- People connect outside services (a company's Linear, GitHub, Notion, Drive… any remote MCP server) with **their own account**, in Settings → Connections. The in-app assistant then uses those services' tools as that person: it reaches exactly what their account there can reach, nothing more.
- Connecting is the MCP authorization flow (spec 2025-11-25) run as an OAuth client: the service's own sign-in page, PKCE, scoped to that server. Services that use personal access tokens take a pasted token instead, checked against the server before it is saved. Tokens are encrypted at rest and refreshed before they expire; a lapsed sign-in shows "Sign in again" with Reconnect.
- Per connection: the server's tool list with a switch per tool, and **Allow changes** (off by default): tools that create, edit or delete are offered only when it is on, and the assistant asks the user to approve every such call. Disconnect deletes our tokens at once and asks the service to revoke them.
- Staff curate the catalog in Admin → Connectors (name, icon, description, server URL, sign-in type, optional pre-registered client, changes policy, on/off), with a server check that reports how connecting will register. With `CONNECTORS_ALLOW_CUSTOM=true` people may also add their own server by URL.
- Each service is its own AI agent scope key (`connectors:<connector id>`), and its ⋮ menu carries the agents feature's "AI agent access".
- Mobile lists connections with status, connects through the system browser (an auth session that returns to the app), and disconnects; tool switches, changes and token services are on the web.

## Data

| Table | Purpose |
|---|---|
| `connectors` | Catalog (owner null, staff) and people's own servers (owner set): slug (tool namespace), name, icon URL, server URL, sign-in type, pre-registered client id and sealed secret, scopes, changes policy, enabled |
| `connector_connections` | One per user × connector: status (`active`/`needs_reauth`), sealed access and refresh tokens, expiry, granted scopes, how to refresh and revoke (resource, issuer, token and revocation endpoints, client), changes switch, switched-off tools, last error, last use |
| `connector_oauth_clients` | Dynamic Client Registrations, one per authorization server (secret sealed) |
| `connector_oauth_flows` | Sign-ins in progress: hashed single-use state, sealed PKCE verifier, then the issued tokens until the user finishes; 10 minutes |
| `connector_revocations` | Tokens queued for RFC 7009 revocation after a disconnect, deleted connector or deleted account; retried with backoff, removed when confirmed or after 6 attempts |

Secrets are AES-256-GCM (`crypto.go`): key id + nonce + ciphertext, bound to their row and column, so a copied ciphertext won't open elsewhere. While `CONNECTORS_ENCRYPTION_KEY_PREVIOUS` is set, the `connectors.reseal` job (every minute, `reseal.go`) re-seals every stored secret still under the old key and logs once "…CONNECTORS_ENCRYPTION_KEY_PREVIOUS can be removed"; remove it only after that line (a value whose key is gone never opens again).

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/connectors` | user | Catalog + own servers with the caller's connection; `allow_custom` |
| POST | `/v1/connectors/{id}/connect` | active, 20/10 min | OAuth `{client: web\|mobile}` → `{authorize_url}`; token `{token}` → `{connector}` |
| GET | `/v1/connectors/oauth/callback` | public | Provider return: consumes state, exchanges the code, redirects to web `/settings/connections?finish=<secret>` (or `connect_error=`) or the app's scheme |
| POST | `/v1/connectors/oauth/finish` | active, 20/10 min | `{secret}`: only the user who started the flow finishes it; another account gets 409 `connection_owner_mismatch` and the grant is revoked |
| GET | `/v1/connectors/oauth/client-metadata.json` | public | Our Client ID Metadata Document (used when the API is on public https) |
| GET / PATCH / DELETE | `/v1/connectors/connections/{id}` | owner (PATCH active) | Read; `{writes_enabled}`; disconnect → `{revocation: revoked\|pending\|unsupported}` |
| GET | `/v1/connectors/connections/{id}/tools?refresh=true` | owner, 30/min | Remote tools with `read_only`, `enabled`, `offered` |
| PUT | `/v1/connectors/connections/{id}/tools/{tool}` | owner, active | `{enabled}` for one tool |
| POST / DELETE | `/v1/connectors/custom`, `/v1/connectors/custom/{id}` | active, 10/hour; owner | Add (discovery checked first) / remove your own server |
| GET | `/v1/admin/connectors`, `/v1/admin/connectors/{id}` | support | Catalog with connection counts |
| POST / PATCH / DELETE | `/v1/admin/connectors[/{id}]` | admin, audited | Create, edit (a new URL or sign-in type asks everyone to reconnect; slug fixed), delete (revokes every connection) |
| POST | `/v1/admin/connectors/check` | admin, 30/min | Discovery report: registration method, issuer, scopes, refresh, revocation |

Errors: `connector_<discovery code>` (502, e.g. `connector_registration_unsupported`, `connector_pkce_unsupported`, `connector_address_blocked`), `no_sign_in`, `token_rejected`, `connector_unavailable`, `connector_needs_reauth`, `flow_not_found`, `custom_disabled`, `custom_limit`, `slug_taken`, `writes_disabled`. Audit actions: `connectors.connector.create|update|delete`.

**Tools** (`app.DynamicToolProvider`, never `ToolProvider`, so the app's own MCP endpoint never serves them): `ToolsFor` lists each healthy, available connection's tools over MCP (official Go SDK client, streamable HTTP, cached per connection for `CONNECTORS_TOOL_CACHE_TTL`), named `<slug>__<tool>` (provider-safe, hash-suffixed when long; stable per user and connection), described `[Name] …`, `Source` = connector name, `Feature` = `connectors:<id>`, `Target` nil, `Preview` on writes. Read-only comes from `readOnlyHint`, believed only for catalog connectors (absent = change; own servers: every tool is a change). Handlers re-check ownership, status, availability and the switches as the caller (they work outside an assistant turn), then call `tools/call` with the user's token (refresh first, once more on 401), `CONNECTORS_TOOL_TIMEOUT`, 256 KiB of text. Listed titles and descriptions are capped (200 / 2,000 characters) and unused lists leave the cache after the TTL, so a server can't make the cache hold megabytes per connection.

**SSRF guard** (`httpsafe.go`) on every outbound request: resolved address checked at dial time (no loopback/private/CGNAT/ULA, NAT64, 6to4 or IPv4-compatible addresses unless development allows it; link-local and cloud metadata always refused), https only outside development, redirects re-checked (none followed for token, registration and MCP calls), no environment proxy, timeouts, body caps (1 MiB metadata, 256 KiB token, 4 MiB MCP).

## Screens

- **Web** `/settings/connections` (settings section): connected services (status, last use; each opens its page), available services with Connect (OAuth redirects to the service; token services open a token dialog), Add a server when allowed; handles the `finish` and `connect_error` return. `/settings/connections/<id>`: status facts, sign-in-again banner, Allow changes, tools with switches and refresh, ⋮ `ResourceMenu` (Reconnect, Disconnect or Remove server, plus feature rows such as AI agent access). `/admin/connectors` (admin tab): table and an add/edit dialog with server check and delete.
- **Mobile** Settings → Connections: connected rows with status and a ⋮ menu (Reconnect, Disconnect, AI agent access), available rows that connect through `expo-web-browser`'s auth session (returns to `scaffoldapp://settings/connections`); token services say "On the web".
- Every view has skeleton, empty and error states.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `CONNECTORS_ENCRYPTION_KEY` | — (required outside development) | 32 random bytes, base64 or hex. Development falls back to a public, logged development key |
| `CONNECTORS_ENCRYPTION_KEY_PREVIOUS` | — | Old key during a rotation: opens old values while the reseal job moves them to the current key; remove it after the "can be removed" log line |
| `CONNECTORS_ALLOW_CUSTOM` | `false` | People may add their own servers (10 each) |
| `CONNECTORS_ALLOW_PRIVATE_NETWORKS` | `false` (dev-up: `true`) | Loopback/private servers; refused outside development |
| `CONNECTORS_TOOL_CACHE_TTL` / `CONNECTORS_TOOL_TIMEOUT` | `5m` / `30s` | Tool list cache / longest remote call |

`PUBLIC_API_URL` builds the redirect URI and the metadata document URL; `PUBLIC_WEB_URL` the web return. Go: `github.com/modelcontextprotocol/go-sdk` (`go mod tidy` on add). npm: none (`expo-web-browser`, `expo-linking` ship with the template).

## Compliance

- Account deletion deletes connections, flows and own servers in the deletion transaction; tokens move to the revocation queue and `AccountDeletedHook` revokes them at once (the worker retries). Export lists connection metadata and own servers, never tokens or secrets.
- Tokens, codes, verifiers and tool payloads are never logged; logs carry ids and connector slugs.
- What a connected service returns is sent to the AI provider when the assistant uses it; the assistant's consent covers that send, and the privacy policy should say so.

## Stitching

1. Set `CONNECTORS_ENCRYPTION_KEY` everywhere but development.
2. Serve the API on public https as `PUBLIC_API_URL`; register `<PUBLIC_API_URL>/v1/connectors/oauth/callback` with services that need a pre-registered client.
3. Curate the catalog (Check each server; decide its Changes policy).
4. Decide `CONNECTORS_ALLOW_CUSTOM`.
5. Install `assistant` ≥ 1.1.0 (the consumer) and, for per-service scopes, the `agents` feature.
6. Update the privacy policy (services people connect; results go to the AI provider).
7. Mobile scheme change → update `mobileReturnURL` (backend) and `RETURN_URL` (mobile) together.

## Extending

- Another consumer of remote tools: call `ToolsFor` per request; honour `Source`, `Feature` and the approval rule for writes. Exported: `connectors.FeatureKey(id)`, shared `connectorFeatureKey(id)`.
- Out of scope: MCP resources and prompts (tools only), elicitation and sampling from servers, step-up re-authorization on `insufficient_scope` (shows as a tool error; Reconnect asks again), org-wide shared connections (every connection is one person's), the pre-2025-03-26 SSE-only transport.
- Don't proxy remote tools through the `mcp` feature, trust annotations from people's own servers, or log tokens or payloads.

## Provenance

New for the forum belt. SSRF guard distilled from OLEARY/crm `internal/integration/httpsafe.go` (dial-time check, IPv4-mapped handling, refused redirects); OAuth client modelled on the Go MCP SDK's `auth.AuthorizationCodeHandler` and `oauthex` (discovery order, client registration priority, PKCE check) but split into start / callback / finish for a multi-user server, with refreshes that carry `resource`. Added on the way: the finish secret against account-linking CSRF, a revocation queue instead of best-effort inline calls, ciphertexts bound to their row.

## Changes

- 1.0.0: catalog, OAuth and token connections, refresh and revocation, SSRF guard, dynamic tools with per-service agent scope keys, web and mobile screens.
- 1.0.1: security review: key rotation completes (`connectors.reseal` re-seals secrets still under the previous key; removing it before broke every older connection and pre-registered secret for good); remote tool titles/descriptions capped and stale tool lists evicted (a server could pin ~40 MiB per connection in memory); SSRF guard also refuses NAT64 local-use, 6to4 and IPv4-compatible IPv6 outside development.
- 1.0.2: Post-commit side effects run through `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for them and a panic is logged instead of crashing the process: revocations after a connect, disconnect or catalog delete (`connectors.revoke`, `connectors.revoke_user`) and the first tool list (`connectors.warm_tools`). gofmt.
