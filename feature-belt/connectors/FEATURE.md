# Connectors

## What it does

- **Connected platforms.** People connect their own accounts on Google Workspace, Microsoft 365, GitHub, GitLab (incl. self-managed), Notion, HubSpot, Salesforce and Zoom **once**, in Settings → Connections, and every feature that extends into that platform uses the same connection (`app.Connections`). Each feature asks only for the scopes its capability needs, **incrementally**, when the person first turns it on. Several accounts per platform are fine; one can be disconnected, or one capability turned off, without touching the rest.
- Content stays on the platform: features read and write it **live with the person's own token**, so the person and their agents reach exactly what that account can. GitHub, GitLab, Notion and HubSpot also take personal/private tokens.
- **Workspace policy (IT):** org owners and admins allow or deny platforms, deny single capabilities and decide whether capabilities that change data are allowed; it applies at once (connections show "Blocked", requests are refused). Staff see each platform's setup on the server.
- **Assistant tools:** people also connect remote MCP servers (Linear, Sentry… any MCP server) so the assistant uses their tools as that person; OAuth by MCP discovery/DCR or a pasted token, a switch per tool and **Allow changes**; staff curate the catalog.
- Every connected account and MCP service is its own AI agent scope key (`connectors:<id>`), in its ⋮ menu.

## Data

| Table | Purpose |
|---|---|
| `connector_platform_connections` | One per person × platform account (unique on user, platform, instance, account id): kind (oauth/token), account name and stable id, GitLab instance or Salesforce instance URL, status, sealed tokens, expiry, granted scopes, capabilities turned on, last error and use |
| `connector_platform_flows` | Platform sign-ins in progress (hashed single-use state, sealed PKCE verifier, the account being extended, the capability to turn on, `return_to`), then the issued tokens until finish; 10 minutes |
| `connector_policies` | Workspace policy per org × platform (`*` = default): allowed, allow writes, denied capabilities, who/when |
| `connectors`, `connector_connections`, `connector_oauth_clients`, `connector_oauth_flows` | MCP catalog, people's MCP connections, DCR registrations, MCP sign-ins |
| `connector_revocations` | Tokens queued for revocation (RFC 7009, GitHub grant deletion, Notion JSON, HubSpot DELETE), retried with backoff, 6 attempts |

Secrets are AES-256-GCM bound to their row and column (`crypto.go`); `CONNECTORS_ENCRYPTION_KEY_PREVIOUS` + the `connectors.reseal` job rotate keys (remove it after "…can be removed" is logged).

## Connected platforms

| Platform | Sign-in | Scopes combine | Refresh | Revoke | Account | Endpoints (`Connection.Endpoints`) | Tokens |
|---|---|---|---|---|---|---|---|
| google | PKCE, `access_type=offline` | incremental (`include_granted_scopes`) | yes | refresh token | userinfo email / sub | drive, drive_upload, docs, sheets, calendar, people, meet | — |
| microsoft | PKCE, `MICROSOFT_TENANT` | incremental (dynamic scopes + `offline_access`; `.default` works for admin-consented apps) | rotates, sends scope | none (remove at myapps) | Graph `/me` mail or UPN / id | graph | — |
| github | OAuth App + PKCE | re-auth with the union (`repo` is broad: shown) | no (OAuth Apps) | `DELETE /applications/{client}/grant` | `/user` login / id | api, web, graphql, uploads | PAT (classic scopes read from `X-OAuth-Scopes`) |
| gitlab | PKCE on `GITLAB_BASE_URL` | union | rotates | `/oauth/revoke` | `/api/v4/user` | api, web, graphql from the base URL | PAT on any instance (`base_url`) |
| notion | Basic + JSON token call, `owner=user` | no scopes (pages picked on consent) | if issued | `/v1/oauth/revoke` | token answer workspace name / id | api (+ `Notion-Version` default) | internal integration token |
| hubspot | client secret post | union, `oauth` always | yes (30 min access) | `DELETE /oauth/v1/refresh-tokens/{t}` | token-info hub domain / hub id | api | private app token |
| salesforce | PKCE on `SALESFORCE_LOGIN_URL` | union, `refresh_token id` always | on 401 (no `expires_in`) | `/services/oauth2/revoke` | identity URL username / org+user | instance (from `instance_url`, Salesforce domains only) | — |
| zoom | Basic + PKCE | union | rotates | `/oauth/revoke` | `/v2/users/me` | api | — (server-to-server acts as the whole account, so it's left out) |

- **Capabilities** come from installed features (`app.CapabilityProvider`), read per request; the screens group them by feature and show each scope (and what broad ones grant). Turning one on: granted already → on; otherwise consent for just the missing scopes (Google, Microsoft) or the union (others), then it turns on at finish (a declined scope leaves it off and says so). Reconnect asks again for everything the account's capabilities need. A sign-in that comes back with a different account than the one extended is added as its own account.
- **`Client`** (`platform_client.go`): bearer on the platform's API hosts only (exact host or suffix, with a path prefix for GitLab `/api/` and Salesforce `/services/`), content hosts (pre-signed downloads) without the token, every other host refused (`connection_host_refused`) before any network; refresh within a minute of expiry under a row lock, once more after a 401 (then `connection_needs_auth`); 429, 503 + Retry-After and GitHub's 403-with-no-requests-left wait up to 8 s twice, else `connection_rate_limited`; 64 MiB responses (1 GiB git packs); the SSRF guard on every dial; 10-minute timeout. Git smart-HTTP fetch (`<repo>.git/info/refs?service=git-upload-pack`, `git-upload-pack`) is allowed on the `web` host for capabilities whose scopes allow git, signed Basic `x-access-token`/`oauth2`; push and everything else there are refused.
- **Policy** applies to `For` (orgID, or every org the person is in when Nil: strictest wins), `Client` (`app.WithConnectionOrg(ctx, org)`, else strictest), connect and turning capabilities on.
- **Webhooks** (optional, polling is the default): `POST /webhooks/connectors/{provider}` verifies GitHub `X-Hub-Signature-256`, GitLab `X-Gitlab-Token`, HubSpot v3, Zoom `x-zm-signature` (+ URL validation), Notion `X-Notion-Signature`, Google channel token, Microsoft `clientState` (+ `validationToken`); unset secret → 503. Replays dropped (`webhooks.Once`); modules implementing `app.ConnectionWebhookHandler` get the event with the matching connections when the payload names the account.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/connectors/platforms?org_id=` | user | Every platform: sign-in set up, tokens, capabilities (policy), the caller's accounts with capability states |
| POST | `/v1/connectors/platforms/{provider}/connect` | active, 20/10 min | `{client, capability?, connection_id?, return_to?, org_id?}` → `{authorize_url}` or `{connection}` (already granted) |
| POST | `/v1/connectors/platforms/{provider}/token` | active, 10/10 min | `{token, base_url?, capability?, org_id?}`: checked with the platform first |
| POST | `/v1/connectors/platforms/finish` | active, 20/10 min | `{secret}` from `?platform_finish=` → `{connection, capability, capability_enabled, other_account, return_to}`; another account → 409 `connection_owner_mismatch` (revoked) |
| PUT | `/v1/connectors/platform-connections/{id}/capabilities/{key}` | owner, active, 60/10 min | `{enabled, client?, return_to?, org_id?}` → `{connection}` or `{authorize_url}` |
| DELETE | `/v1/connectors/platform-connections/{id}` | owner | → `{revocation: revoked\|pending\|unsupported\|manual}` |
| GET / PUT / DELETE | `/v1/connectors/orgs/{orgID}/policy[/{provider}]` | member / admin, audited | Workspace policy (`*` = default); reset |
| GET | `/v1/admin/connectors/platforms` | support | Setup per platform: missing env, redirect URL, scopes features ask for, connections, webhooks |
| GET | `/v1/connectors/oauth/callback` | public | Shared callback (platform and MCP sign-ins) |
| … | `/v1/connectors`, `/v1/connectors/{id}/connect`, `/v1/connectors/oauth/finish`, `/v1/connectors/connections/{id}[/tools…]`, `/v1/connectors/custom`, `/v1/admin/connectors[…]` | as 1.x | MCP servers (unchanged) |

Errors: `app.Connection*` codes (`connection_needs_auth`, `connection_scope_missing`, `connection_blocked`, `connection_not_found`, `connection_unavailable`, `connection_rate_limited`, `connection_host_refused`, `connection_response_too_large`), `platform_not_configured`, `tokens_unsupported`, `token_rejected`, `capability_not_found`, `connection_limit` (50 accounts), plus the MCP codes. Audit: `connectors.policy.update|reset`, `connectors.connector.*`.

## Screens

- **Web** `/settings/connections`: **Platforms** (a card each: status, accounts with Token badge, Reconnect / New token, ⋮ with Reconnect, Disconnect and AI agent access; capabilities grouped by feature with a switch, Grant access when consent is pending, a lock when the workspace blocks it; Connect per capability; Add another account; personal-token dialog with the instance field for GitLab) then **Assistant tools** (MCP servers). `?capability=<key>&return_to=<path>` shows that capability first ("Use sam@acme.com", "Another account", token) and returns after; sign-ins come back with `?platform_finish=`. `/settings/connection-policy`: default and per-platform switches, capability checkboxes (read-only for members). `/admin/connectors`: platform setup table with the redirect URL to copy, then the MCP catalog.
- **Mobile** Settings → Connections: platforms with accounts, status, ⋮ (Reconnect, Disconnect, AI agent access), a switch per capability, Connect through the system browser (auth session back to `scaffoldapp://settings/connections`); tokens on the web; the deep link puts the platform first and returns. MCP section as before.
- Every view has skeleton, empty and error states.

## Config

Platform client ids/secrets: `GOOGLE_`, `MICROSOFT_`, `GITHUB_`, `GITLAB_`, `NOTION_`, `HUBSPOT_`, `SALESFORCE_`, `ZOOM_` + `CLIENT_ID` / `CLIENT_SECRET` (set together; unset → "Not set up"), `MICROSOFT_TENANT` (`common`), `GITLAB_BASE_URL` (`https://gitlab.com`), `SALESFORCE_LOGIN_URL` (`https://login.salesforce.com`); webhooks: `GITHUB_WEBHOOK_SECRET`, `GITLAB_WEBHOOK_SECRET`, `NOTION_WEBHOOK_SECRET`, `ZOOM_WEBHOOK_SECRET`, `GOOGLE_WEBHOOK_TOKEN`, `MICROSOFT_WEBHOOK_CLIENT_STATE` (HubSpot signs with its client secret). `CONNECTORS_DEV_PROVIDER_URLS` (development only): `google=http://127.0.0.1:9000/g,…` rewrites each platform URL to `<mock>/<host><path>`. Unchanged: `CONNECTORS_ENCRYPTION_KEY` (required outside development), `CONNECTORS_ENCRYPTION_KEY_PREVIOUS`, `CONNECTORS_ALLOW_CUSTOM`, `CONNECTORS_ALLOW_PRIVATE_NETWORKS` (dev `true`), `CONNECTORS_TOOL_CACHE_TTL`, `CONNECTORS_TOOL_TIMEOUT`. `PUBLIC_API_URL` builds the one redirect URL every platform registers; `PUBLIC_WEB_URL` the return.

## Compliance

- Account deletion queues revocation of every OAuth grant (connections and unclaimed sign-ins) and deletes them in the deletion transaction; `AccountDeletedHook` revokes at once. Personal tokens are deleted, never revoked (people may use them elsewhere). Export lists connections metadata only (platform, account, host, scopes, capabilities, status, dates), never tokens.
- Nothing from a platform is stored by this feature; features read live and keep at most the references a person creates. Tokens, codes and payloads are never logged.
- Name the platforms people may connect in the privacy policy; Google requires verification for sensitive/restricted scopes (Drive) before launch.

## Stitching

1. Set `CONNECTORS_ENCRYPTION_KEY` everywhere but development; serve the API on public https as `PUBLIC_API_URL`.
2. Register an OAuth app per platform (the consent screen shows its name) with redirect `<PUBLIC_API_URL>/v1/connectors/oauth/callback`; set its client id/secret. Admin → Connectors → Platforms lists what's missing and the scopes installed features will ask for.
3. Decide each workspace's default policy (Settings → Connection policy) and the MCP catalog's Changes policy; decide `CONNECTORS_ALLOW_CUSTOM`.
4. Update the privacy policy (platforms and services people connect; assistant results go to the AI provider).
5. Mobile scheme change → update `mobileReturnURL` (backend) and `RETURN_URL` (mobile) together.

## Extending

- **Features** declare capabilities (`app.CapabilityProvider`: key `<module>.<provider>.<what>`, scopes, Write), list sources with `For` and `Offers`, call platforms with `Client` (build URLs from `Connection.Endpoints`, set `app.WithConnectionOrg`), badge rows with `app.ConnectedSource`, and show the fix by error code (`connectionFixPath` in shared). Never import this package.
- **Another platform**: add a `registry()` entry (endpoints, rules, user info, revoke style) and its env vars in `providerEnv` and feature.json.
- Extending, out of scope: GitHub Apps / GitHub Enterprise Server, org-wide (non-personal) connections and Zoom server-to-server, Salesforce outbound messages, partial revocation of single scopes (platforms can't), step-up auth on `insufficient_scope` for MCP tools.
- Don't hand tokens to features, follow redirects outside the rules, or log tokens or payloads.

## Provenance

New for the forum belt. SSRF guard from OLEARY/crm `internal/integration/httpsafe.go`; MCP OAuth client modelled on the Go MCP SDK's `auth`/`oauthex`. Platform endpoints, scope rules, revocation and token answer shapes from each platform's public OAuth docs; verified against local mocks only (no real platform credentials).

## Changes

- 2.1.0: implements `app.ConnectionDirectory` (`All`: every platform account a person connected) and fills `Connection.Platform` (the platform's name), for the agents feature's per-account levels.
- 2.0.0: connected platforms: provider registry (Google, Microsoft, GitHub, GitLab incl. self-managed, Notion, HubSpot, Salesforce, Zoom) with per-deployment OAuth apps, PKCE, incremental scopes, multiple accounts, personal tokens; `app.Connections` (`For`, `Client`, `Offers`) with a signing, refreshing, host-locked, rate-limit-aware client and `connection_*` errors; capability switches; workspace policy (audited) enforced live; staff setup status; webhook ingress with signature checks and `app.ConnectionWebhookHandler`; revocation styles per platform; Settings → Connections by platform with the capability deep link and `return_to`, Connection policy screen, mobile platforms; deletion/export cover platforms. Requires `orgs`. Migration `0901`.
- 1.0.2: Post-commit side effects run through `bg.Go`; gofmt.
- 1.0.1: security review: key rotation completes (`connectors.reseal`); remote tool titles/descriptions capped and stale lists evicted; SSRF guard refuses NAT64 local-use, 6to4 and IPv4-compatible IPv6 outside development.
- 1.0.0: catalog, OAuth and token connections, refresh and revocation, SSRF guard, dynamic tools with per-service agent scope keys, web and mobile screens.
