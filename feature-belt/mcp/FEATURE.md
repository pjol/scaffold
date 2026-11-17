# MCP, connected apps, API keys and AAuth

## What it does

- People add the app to Claude.ai, ChatGPT, Claude Code, Cursor or any MCP client **by URL** (`<PUBLIC_API_URL>/mcp`) and sign in with their own account on a consent page that names the app, where it returns to and what it asks for (read; optionally changes). The agent then acts as that person: every tool runs as them and every feature applies its own permission checks.
- Settings → Connected apps lists each authorized app with its access, when it was connected and last used, and disconnects it (every token, every device). The server URL to add sits on top.
- Settings → API keys: personal keys (read only, or read and write) for scripts and clients without sign-in. The secret shows once. Up to 10 active keys.
- Read-only connections see only read-only tools; write tools also need current legal consent. Suspended, banned and deleted accounts are refused on every call, checked live.
- With the `agents` feature installed, every call also passes the person's AI agent scope (off / read / suggest / edit, per feature and resource): hidden features' tools aren't listed, and in suggest mode a write becomes a suggestion the person accepts or rejects instead of running. The key or token scope stays a ceiling on top.
- Staff list connected apps and keys across users and revoke them; revocations go to the audit log.
- **AAuth agents** (agents that sign every request, draft-hardt-oauth-aauth-protocol-11) use the same `/mcp` through their **person server** (Sayso, a company's own): the first call for a person sends them to `/connect/aauth` with a code, where they sign in and link that person server account to theirs, seeing the email it vouches for next to their own; from then on each agent needs an auth token from the person server (which asks the person) and acts as them, read only unless it also asked for write. Settings → Connected apps lists linked person servers with Unlink.
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
| `aauth_signing_keys` | The resource's Ed25519 keys for resource tokens (`internal/aauth/keys`, role `resource`): private JWK sealed with AES-256-GCM under `AAUTH_KEY_ENCRYPTION_SECRET`, published 2 min before signing, rotated monthly, kept published 2 h after |
| `mcp_aauth_links` | A person server's identifier for a person, `(ps_iss, sub)`, linked to the account that confirmed it: the server's name and the verified email it asserted at the time, first and last use, revocation (user or staff) |
| `mcp_aauth_agents` | Agents seen under a link, by signing-key thumbprint (no agent identifier reaches a resource), last seen at most once a minute |
| `mcp_aauth_pending` | A call from an identity nobody has linked, held 15 min while the person links it: code prefix and SHA-256, wrong-code count, who claimed it, the callback, the held JSON-RPC body; once run, the answer for the auth token that collected it until that token expires |
| `mcp_aauth_revocations` | `(iss, jti)` revoked by the person server that issued the token, until its `exp` |

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
| GET | `/.well-known/mcp.json` | public, CORS `*` | Server card: endpoint, every auth method (AAuth, OAuth, keys), scopes, tool summaries |
| GET | `/.well-known/aauth-resource.json` | public, CORS `*` | AAuth resource metadata: `issuer` (PUBLIC_API_URL's origin), `jwks_uri`, `access_mode: auth-token`, `authorization_endpoint`, `scope_descriptions` (`read`, `write`), `signature_window` 60, `revocation_endpoint`, `accept_signature_algs` (Ed25519, ES256) |
| GET | `/aauth/jwks.json` | public, CORS `*` | The keys resource tokens are signed with |
| POST | `/aauth/authorize` | signed, person token | Authorization endpoint: `{scope: "read" or "read write"}` → `{resource_token}`; `invalid_scope`, `invalid_account` (there are no accounts to choose), problem+json |
| GET | `/aauth/pending/{id}` | signed, same key | Poll a held call: 202 `pending`/`interacting`, then its answer, 403 `denied`, 408 `expired`, 410 `invalid_code`; after that 410, except the same auth token gets the same answer until it expires |
| POST | `/aauth/revoke` | signed by a person server (`jwks_uri` scheme, `aauth-person.json`) | `{jti, exp}` recorded by `(iss, jti)`; 200 at once; `unsupported_iss` for servers with no link or request here; at most 100,000 held per issuer (`rate_limited`) |
| POST | `/v1/mcp/aauth/interactions` | user, 20/min | `{code, callback?}` claims a link request (single use, bound to the caller); a wrong code counts against open requests sharing its first four symbols, five end them; 410 `invalid_code`, 400 `invalid_callback` |
| GET | `/v1/mcp/aauth/interactions/{id}` | user (the claimer) | The request: person server (issuer, host, its own name), asserted email and `email_match` (`match`, `mismatch`, `none`), return host, status; 410 `request_expired` |
| POST | `/v1/mcp/aauth/interactions/{id}/decision` | user (the claimer), 30/min; allow needs active + consent | `{allow}` → `{status, redirect_url}` (the callback, with `error=access_denied` on a refusal); 409 `already_linked` when the identity is another account's |
| GET, DELETE | `/v1/mcp/aauth/links`, `/v1/mcp/aauth/links/{id}` | user | Linked person servers; unlink stops their agents at once |
| GET, DELETE | `/v1/admin/mcp/aauth/links[/{id}]?user_id=&status=` | support | Links across users; unlink audited as `mcp.aauth.unlink` |

`/mcp` errors: 401 `unauthenticated` with `WWW-Authenticate: Bearer resource_metadata="…/oauth-protected-resource/mcp", scope="read write"` (plus `error="invalid_token"` for a bad token); 403 `account_suspended`; a read-only OAuth token calling a write tool gets 403 `insufficient_scope` with the scope challenge (clients step up), a read-only key 403 `scope_required`; 429 `rate_limited`. Tool failures are MCP results with `isError: true` and text `code: message` (`consent_required`, `invalid_input`, `timeout`, or the tool's `*httpx.Error` code); results carry JSON as text and `structuredContent`.

Agent scope (when an `app.AgentPolicy` is installed, found with `app.FindAgentPolicy` at request time): every request carries `auth.Agent{Via: "mcp", Client: <OAuth client or key name>, ClientID: <client_id or key id>}`; `tools/list` hides a module's tools when its `FeatureLevel` max is off and its write tools below suggest (feature = the providing module's `Name()`); each `tools/call` runs `GateTool` after the scope and consent checks: run, refuse (`isError`, `code: message`), or suggested (not an error: "Suggested to <name> for review (suggestion <id>)…" with `structuredContent {"status":"suggested","suggestion_id"}`). Without a policy, tools run as before.

**AAuth.** This server is an AAuth resource in PS authorization (three-party) access, with the protocol library in `internal/aauth` (shared with Sayso). A request carrying `Signature`, `Signature-Input` or `Signature-Key` takes this path; anything else is the bearer path, and an unsigned request with no bearer also gets `AAuth-Requirement: requirement=person-token`. Every `/mcp` response carries `Link: <…/.well-known/aauth-resource.json>; rel="aauth-resource"`. The seven verification steps run first (401 with `Signature-Error` on any failure, plus `requirement=person-token` for `revoked_jwt`); a request with a body must cover `content-type` and `content-digest`, since a JSON-RPC body is the whole of what a call does (`invalid_input` with `required_input` otherwise; not in `additional_signature_components`, which would apply to bodyless polls too). Then by token: an **agent token** gets 401 `requirement=person-token` (it says which agent, not whom it acts for); a **person token** gets 401 `requirement=auth-token` with a resource token and never data, linked or not; an **auth token** must be the person server's own (`dwk: aauth-person.json`, `ps = iss`; there is no access server, so anything else gets `person-token`) and carry `read`, else a fresh challenge. Resource tokens copy `ps`, `sub`, `presented_jti`, `mission_s256` and `tenant` from the token presented, set `aud` to the person server, `agent_jkt` to the signing key and live 5 minutes; they ask for `read`, `read write` when the batch calls a write tool, and while the identity is unlinked also `email` when the person server lists it in `scopes_supported`. A write tool on a read token is a step-up: 401 `auth-token` for `read write`, naming the auth token carried. An auth token from an identity nobody has linked is answered 202 `requirement=interaction` (`url` = `PUBLIC_WEB_URL/connect/aauth`, an 8-symbol code, `Location` on this origin, `Retry-After` 5, `Cache-Control: no-store`) and the call is held; at most 5 open per identity (429 `rate_limited`). After the person links, the first poll with an auth token for that identity runs the held call as them and answers with its response (Deferred Responses: a poll's 200 is the call's answer). Linked calls run as the account with `auth.Agent{Via: "mcp", Client: "Agents via <person server>", ClientID: "aauth:<link id>"}`, rate-limited per link and signing key; `(iss, sub)` is matched exactly and never across issuers. Not offered: four-party (an access server), two-party session tokens (`AAuth-Access`), access on a person token alone, R3 and Budgets.

**Agent credentials only.** Every credential an agent holds is minted with `auth.AgentCredentialPrefix`: access tokens `agt_scaffoldapp_at_…`, refresh tokens `agt_scaffoldapp_rt_…`, API keys `agt_scaffoldapp_sk_…`, client secrets `agt_scaffoldapp_cs_…` (opaque, stored as SHA-256). The core auth middleware refuses them on every user route, uploads and the WebSocket (401 `agent_credential`); `/mcp` accepts nothing else: not a person's session token, not a refresh token, not a token whose audience isn't exactly the MCP endpoint (RFC 8707; both metadata documents name it, and `resource` at authorize and token must equal it), not one carrying a scope other than `read`/`write`. `/mcp` takes POST only (405 otherwise) and runs only `initialize`, `ping`, `tools/list` and `tools/call` (notifications are accepted and ignored); any other method gets JSON-RPC -32601, so no resource or prompt can reach the REST API. Bodies are capped at 1 MiB (413).

## Screens

- **Web** `/connect/aauth?code=…[&callback=…]` (public route, CenteredScreen): signed out → sign-in with return; Terms not accepted → the consent screen first; a code in the address is claimed at once and the address switches to `?request=<id>`, so a reload doesn't present it again; with no code, a field to type the one the agent showed. Then "Link <person server>?": its name (ⓘ: its own claim) and verified address, the email its account vouches for with "Same as yours", or a warning banner when it names another email, the code to compare, where it returns to, the account with Switch account, Cancel and Link account. After: back to the callback, or "Linked … you can close this tab". Errors for a used or expired code, a callback that isn't https or loopback, and a request that isn't the caller's.
- **Web** `/connect` (public route, CenteredScreen): signed out → sign-in with return; Terms not accepted → the consent screen first; then the app's name (ⓘ: self-declared, or "published by <host>" for metadata documents), where it returns to, the account with Switch account, "See what you can see" and a "Make changes as you" switch when write is requested, a warning for loopback and app-scheme redirects, Allow and Deny; error states for missing, expired, unknown-client and redirect-mismatch requests. `/settings/connected-apps`: server URL with copy, app list (skeleton, empty, error, load more); each row's ⋮ is the core `ResourceMenu` (feature `mcp`, kind `mcp.client`, the client's id) holding Disconnect (confirmed) and whatever other features add there. `/settings/api-keys` as before. Below the apps, **Person servers**: linked ones with host, asserted email, linked and last-used dates and agents in the last 30 days; ⋮ is the `ResourceMenu` (kind `mcp.aauth_link`) with Unlink (confirmed). `/admin/connected-apps` and `/admin/api-keys`: tables with user and status filters and audited revoke; connected apps also lists person servers with audited unlink.
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
| `MCP_AAUTH_ENABLED` | `true` | Accept AAuth agents. Off by itself, with a log line, when `PUBLIC_API_URL` isn't an https origin with no path (loopback http in development) or `PUBLIC_WEB_URL` is unset |
| `AAUTH_KEY_ENCRYPTION_SECRET` | dev fallback | At least 32 bytes sealing the resource signing keys; required outside development while AAuth is on |

`PUBLIC_API_URL` (core) is the issuer and the resource; `PUBLIC_WEB_URL` (core) hosts `/connect`, and OAuth is off when it is unset. Go modules `github.com/modelcontextprotocol/go-sdk v1.8.0` and `github.com/dunglas/httpsfv v1.1.2` (structured fields, for AAuth); no npm packages. AAuth's outbound fetches (person server metadata and JWKS) go through `internal/safehttp` with `LongHeaders` (template since 2026-11-08), at most once a minute per issuer, cached up to 24 h.

## Compliance

- Account deletion deletes keys, grants (with their tokens) and pending codes. Export lists key and connected-app metadata, never secrets, tokens or hashes.
- Agents act with the person's permissions only, narrowed further by their agent scope when the `agents` feature is installed; write tools need the write scope and current consent. Status is checked on every exchange and call.
- Agent credentials can't act as the person anywhere but `/mcp` (the `agt_` prefix is refused by the core auth middleware), and minting keys, consenting to a client and disconnecting apps are user routes only a person's session reaches (concrete/auth.md → Agent credentials).
- AAuth: account deletion deletes links and the link requests the person claimed; export lists linked person servers (issuer, name, asserted email, tenant, dates). A held call's body is kept until it runs or its request ends (15 min), and its answer until the auth token that collected it expires (at most an hour), then wiped by the hourly prune. A person token alone never reaches data, and an agent's auth token is capped by its scope, the person's permissions and their agent scope, like every other way in.
- Upgrading from 1.0.0 revokes keys minted without the `agt_` prefix (migration 0601); owners create new ones.
- Secrets and tokens are never logged; request handles travel in query strings (request logs record paths only).

## Stitching

1. Give the features agents should use an `app.ToolProvider` with prescriptive descriptions; mark tools that change data `ReadOnly: false`.
2. Rewrite the server instructions, `resource_name` and card description in `backend/internal/mcp/{mcp,oauth}.go` and the consent copy in `web/src/features/mcp/ConnectScreen.tsx` for this app.
3. Serve the API at the origin `PUBLIC_API_URL` names (no path prefix): `/mcp`, `/.well-known/*` and `/oauth/*` live at its root.
4. Set `PUBLIC_WEB_URL`; `/connect` is in `proxy.ts` public paths so Clerk's middleware doesn't replace the relative return path.
5. Decide whether to allow client ID metadata documents (outbound fetches) and adjust TTLs to the app's risk.
6. AAuth: serve the API at an https origin with no path (it is the resource identifier), set `AAUTH_KEY_ENCRYPTION_SECRET` outside development, and rewrite the metadata `name`, `description` (`aauthDescription` in `mcp.go`) and scope descriptions (`aauth.go`) for this app. `"/connect/aauth"` goes in `proxy.ts` public paths. Or set `MCP_AAUTH_ENABLED=false`.

## Extending

- **Agent scope gate**: the policy lives in the `agents` feature (`app.AgentPolicy`); this package only marks calls (`auth.WithAgent`), filters `tools/list` by `FeatureLevel` and runs `GateTool` per call. Handlers and search providers read `auth.AgentFrom(ctx)` to filter rows; tools that act on one resource set `Tool.Target` (and `Tool.Preview` for suggestions).
- New tools belong in the owning feature's `Tools()`; this package only adapts them. Scopes are `read` and `write`; finer scopes mean a check per tool in `toolHandler` and `writeToolCalled`.
- AAuth extensions not built: an access server (four-party, `aauth-access.json`), two-party session tokens for agents with no person server, serving on a person token alone, R3 vocabularies and Budgets. Each is additive on `internal/aauth`, which already has their wire types. AAuth isn't offered on the REST API, uploads or git; agents reach the app only through `/mcp`.
- Out of scope: OIDC (no id_token or `/.well-known/openid-configuration`; MCP clients fall back to RFC 8414), `private_key_jwt`, token introspection, browser-based clients that need CORS on `/oauth/register` and `/mcp` (the root group follows the API's CORS list), and trust policies for metadata documents (any public https host is accepted; the consent page names it).
- Dynamically registered clients name themselves; a client that re-registers on each connect shows as a new row. Metadata-document clients (Claude Code, VS Code) are one row each.
- Don't accept keys or OAuth tokens on the REST API's user routes, and never let either create keys or approve connections.

## Provenance

Keys distilled from MOX (`internal/mcpserver`, `internal/access`): hashed keys with display prefixes, per-scope tool lists, `code: message` errors, the server card. OAuth distilled from OLEARY/crm (`handlers_mcp_oauth.go`, `store/mcpoauth.go`, `0004_mcp_oauth.sql`): this server as the authorization server with the app's sign-in bridged on a web page, hashed single-use values spent in the reading statement, live status checks. Generalised from operators-only to every user and brought to the 2025-11-25 spec: audience binding, step-up 403s, client ID metadata documents behind an SSRF guard, loopback any-port and native-app redirects, confidential clients, refresh replay revoking the grant, code replay revoking its tokens, RFC 7009, `iss` in responses, grants a person can see and disconnect. AAuth: the protocol library is Sayso's `internal/aauth` (PERSON_SERVER, pinned to github.com/dickhardt/AAuth at a3dce12, 2026-11-05), copied whole so fixes flow both ways, with its static interop fixtures against the published `@aauth` SDK; `keys` gained the `resource` role. `aauth_test.go` drives the resource end to end over real signed HTTP with an agent provider and person server built on the same library, and checks every resource token with the library's own person-server verification (`VerifyTokenRequest`). Verified with the official Go SDK's OAuth client (DCR and metadata documents, including Claude Code's published document) and Playwright on the consent page.

## Changes

- 1.2.0: AAuth resource (protocol -11, three-party): `/.well-known/aauth-resource.json`, signed `/mcp` calls with challenges by token type, resource tokens signed by rotating keys, step-up to write, account linking at `/connect/aauth` with the person server's verified email compared and held calls answered on the poll, an authorization endpoint, a revocation endpoint for person servers, linked person servers in Settings → Connected apps and admin; the protocol library (`internal/aauth`) and `internal/aauth/keys` copied from Sayso; needs `safehttp.Options.LongHeaders`.
- 1.1.0: OAuth 2.1 authorization server for remote connectors (discovery, DCR, metadata documents, consent page, PKCE, resource binding, rotating refresh tokens with replay detection, revocation); Settings → Connected apps (⋮ `ResourceMenu`); admin connected apps; every tool call marked `auth.Agent` and gated by the installed `app.AgentPolicy` (list filtering, refuse, suggest); read-only keys calling a write tool get 403 `scope_required` instead of "unknown tool"; agent credentials carry the `agt_` prefix and `/mcp` accepts only them, audience-bound, POST-only and tools-only (older keys are revoked).
- 1.0.0: endpoint and card served at the root (`app.RootRoutes`); settings and admin screens; shared client.
- 0.1.0: backend at `/v1/mcp`.
