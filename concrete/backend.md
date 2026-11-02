# Backend

Go API in `backend/`, module path `<slug>/backend`. The template's code is the reference for every rule here; read the package before changing a pattern.

## Shape

One binary (`cmd/server`) runs the API and the background workers. Boot order: `config.Load` → logging → `db.Connect` → `db.Migrate` → realtime broker → storage → `app.Deps` → `api.Build` → `config.Check` → `Start` → `http.Server`. Shutdown (SIGTERM, 20 s in all): stop accepting → finish in-flight requests → `bg.Wait` → close broker and pool. `cmd/seed` fills a development database and also waits on `bg` before it exits.

```
backend/
  cmd/server/            boot + graceful shutdown
  cmd/seed/              idempotent dev data (development only)
  internal/app/          Deps + the module interfaces (the contract every feature implements)
  internal/api/          router, Build, feature registry (feature_<name>.go files)
  internal/config/       Config + env helpers (String/Int/Bool/Duration/List/Required)
  internal/db/           pool, Migrate, WithTx, error helpers
  internal/httpx/        Handler, Error, Decode, Page/cursor, middleware, Detach
  internal/auth/         Provider (Clerk), Identity, guards, dev tokens
  internal/legal/        versions + consent gate
  internal/accounts/     me, consent, uploads, export, deletion, Clerk webhook, /v1/config
  internal/admin/        users, roles, status, audit log
  internal/audit/ realtime/ storage/ mail/ notify/ jobs/ bg/ webhooks/
  internal/<feature>/    one package per feature-belt module or app domain
  migrations/            embedded NNNN_<track>_<what>.sql (database.md)
```

## Modules

Everything above the infrastructure is a module: `New(d *app.Deps) *Service` plus the optional interfaces in `internal/app` the router discovers by type assertion.

| Interface | Mounts / effect |
|---|---|
| `PublicRoutes` | `/v1`, no auth |
| `UserRoutes` | `/v1`, signed in; mutations behind the consent gate |
| `AdminRoutes` | `/v1/admin`, support or admin; wrap admin-only routes in `auth.RequireAdminRole("admin")` |
| `StreamRoutes` | `/v1`, signed in + consent gate, **no request timeout** (server-sent events) |
| `RootRoutes` | server root (`/mcp`, `/.well-known/…`), IP rate limit only; the handler authenticates |
| `WebhookRoutes` | `/webhooks`, raw body, signature-verified, no auth or timeout |
| `Worker` | `Start(ctx)` spawns loops; singleton work through `jobs.Every` |
| `TopicAuthorizer` | decides realtime subscriptions for its topic prefix |
| `AccountDeleter` | removes/anonymises its rows inside the deletion transaction |
| `AccountDeletedHook` | runs after the deletion commits (revoke sockets, provider calls); best-effort |
| `AccountExporter` | contributes its data to `GET /v1/me/export` |
| `DevSeeder` | adds realistic rows in `cmd/seed` (development only, idempotent) |
| `ToolProvider` | declares `app.Tool`s (name, JSON schema, handler run as the caller) that the `mcp` and `assistant` features expose to agents |
| `DynamicToolProvider` | `ToolsFor(ctx, caller)`: per-caller tools that can't be listed at boot (the `connectors` feature's remote MCP tools, with `Tool.Source` naming the service and `Tool.Feature` = `connectors:<id>` as the agent-scope key); the `assistant` merges them per turn and asks the user before each external write |
| `SearchProvider` | `Search(ctx, caller, q, limit)` → `[]app.SearchResult` for the `search` feature's global search; same permission checks as the module's list routes. Org-scoped hits set `OrgID` and `Context` (`app.SearchContext(orgName, container)`: "Riverside Climbing Club · #general"); when `app.SearchOrg(ctx)` names an org, filter to it in SQL if you can (search drops other orgs' hits either way; hits with no `OrgID` always pass). Hits read from a connected platform set `Source` (`app.ConnectedSource`) and `Context` "<Platform product> · <account>"; time-box each connection |
| `AgentPolicy` | one module (the `agents` feature) applies users' AI agent scopes: `FeatureLevel`, `ResourceLevel`, `GateTool`. The `mcp` and `assistant` features mark agent calls with `auth.WithAgent` and gate every tool through it; features drop rows an agent may not read with `app.AgentReadable` (a page at a time) or `app.AgentAllows` (one row); access resources get it from the access SQL helpers |
| `CapabilityProvider` | `Capabilities()` → `[]app.Capability`: what the module can do on an outside platform once a person connects it (`files.google.drive`, `crm.hubspot.records`), with the OAuth scopes each needs; the connections screen offers them and requests scopes incrementally |
| `Connections` | one module (the `connectors` feature) holds every person's platform connections and tokens: `For(user, org, capability)`, `Client(user, connection, capability)` (a signing, refreshing, host-locked HTTP client), `Offers(user, org, module)`. Features keep their own platform adapters and find it with `app.FindConnections(d.Modules)`; without it they offer no connected sources. Connected content is read live with the person's token (their platform permissions apply), rows carry a `source` (`app.ConnectedSource(conn, product, url)` → `*app.SourceRef`, JSON `{"type":"connected",provider,connection_id,account,product?,url?}`; nil for the app's own), and agents are also capped by the connection's scope (`app.ConnectionScope(id)`). Build URLs from `Connection.Endpoints` (self-managed GitLab, Salesforce instances); pass the workspace with `app.WithConnectionOrg(ctx, org)` before `Client` (else the strictest policy of the person's orgs applies). Failures are `*httpx.Error` with `app.Connection*` codes (`connection_needs_auth`, `connection_scope_missing`, `connection_blocked`, `connection_rate_limited`, `connection_host_refused`…; `app.ConnectionErrorCode(err)` reads one through `*url.Error`): pass them to clients, which link the fix |
| `ConnectionDirectory` | `All(ctx, user)`: every platform account a person connected (the connectors feature implements it; find it with `app.FindConnectionDirectory`). The agents feature lists each account with its own level; `app.ConnectionLabel(conn)` is "Google · sam@acme.com". Agent tools on connected items target kind `<module>.external`, id `app.ExternalTarget(conn, platformID)` (`app.TargetConnection` reads it back), so the account's level caps them too |
| `ConnectionWebhookHandler` | optional: `ConnectionWebhooks()` → `[]app.ConnectionWebhook{Provider, Handle}`; the connectors feature verifies `/webhooks/connectors/{provider}` and hands over `app.ConnectionEvent` (type, delivery id, body, matching connections). Treat it as a hint and re-read through `Client`; polling is the default |

- A feature registers from one file, `internal/api/feature_<name>.go` (`RegisterModule`; `RegisterDeps` when it replaces a shared service such as `d.Notify`). No other core file changes.
- Register **flat** routes inside `Mount*` (`r.Get("/chat/conversations", …)`). Never `r.Route`/`r.Mount` in a module: modules share prefixes and chi panics on a duplicate mount.
- Any module that stores user data implements `AccountDeleter` and `AccountExporter`. This is a store requirement, not a nicety.
- Modules talk to each other through `Deps` services (`Notify`, `Storage`, `Mail`, `Broker`). A feature may import the packages of features in its `requires` (they are always installed) and use their exported functions; it never imports anything else. Package-level registries (e.g. `moderation.RegisterTarget`) are filled in `New` and read at request time, so construction order never matters.
- Agent tools: read-only by default; never a tool that deletes user data, sends anything outward or fetches arbitrary URLs.
- Write tools set `Target` (the one resource they change, so per-resource agent scopes apply) and `Preview` (summary + before/after, what the user reviews when their agent scope is "suggest"). Agents never get sharing, permission or credential tools.

## Handlers

- `httpx.Handler(func(w, r) error)`. Return `*httpx.Error` for client errors (`BadRequest`, `NotFound`, `Conflict`, `Forbidden`, `NewError(status, code, msg)`); plain errors become logged 500s with a generic message. Error JSON is `{"error":{"code","message","details"}}`; `code` is stable snake_case clients branch on, `message` is user-facing copy (writing.md).
- Decode with `httpx.Decode` (1 MiB, unknown fields rejected, empty body = `{}`, so validate required fields). Respond with `httpx.OK/JSON/NoContent`.
- Identity: `auth.MustFromContext(r.Context())` inside user/admin routes only.
- Lists return `httpx.Page[T]{items, next_cursor}`: keyset pagination on `(created_at, id)` fetched with `limit+1` (`httpx.NewPage`). Never offset pagination; never a cursor without a tie-breaker.
- JSON is snake_case and must match `packages/shared` types exactly. Change both in the same commit.
- Money is integer minor units (`int64` cents) or `NUMERIC` + decimal strings on the wire; never float.
- Multi-statement writes go in `db.WithTx`. Admin mutations call `audit.Log(ctx, tx, …)` in the same transaction.
- Routes that call a payment provider inside a transaction use `httpx.Detach(timeout)` so a dropped client cannot roll back after the provider acted. Pair with an `Idempotency-Key`.
- Side effects that must not fail the request (notify, realtime, email) are best-effort: log and continue. Work that must outlive the request (fan-out to many recipients, push, provider calls) goes through `bg.Go(ctx, "<module>.<what>", fn)`, never a bare `go`: it keeps the request's values but not its cancellation, logs errors and panics, and shutdown and `cmd/seed` wait for it before the pool closes. `fn` bounds its own time. A plain `go` stays right for work the caller waits on (a parallel fan-out joined with a `sync.WaitGroup` before the handler returns) and for loops started from `Start` that end with its context.
- Expensive or abusable routes add `auth.RateLimit(n, window)` on top of the global limits.

## Auth

`auth.Provider` verifies tokens (Clerk by default; `auth.Clerk` checks session JWTs locally against cached JWKS and the `azp` origin). The users row is provisioned on first request from the provider's **verified** email and keyed on `auth_subject`. Roles: `admin_role` ∈ {support, admin}; `ADMIN_EMAILS` grants admin at sign-in. Status: active | suspended (reads only, `auth.RequireActive` on mutations) | banned | deleted. Development: `AUTH_DEV_BYPASS=true` (refused unless `APP_ENV=development`) accepts `Bearer dev:<subject>:<email>`. Full flow across tiers: auth.md.

## Shared services

| Service | Use |
|---|---|
| `realtime` | `d.Broker.Publish(ctx, topic, eventType, data)`. Topics `"<kind>:<uuid>"` via `realtime.Topic`; the hub owns `user:<id>` and `admin:live`. Delivery is at-most-once: clients refetch on reconnect. Memory broker in dev, Redis (`REDIS_URL`) for more than one instance. Revoke access with `realtime.Revoke` (one user) or `realtime.RevokeTopic` (re-check every subscriber of a topic, for changes that affect an unknown set of people). |
| `storage` | `storage.RegisterPurpose(Purpose{Name, Public, Types, MaxBytes, ConsentExempt, AnyType})` in `New` (`storage.ImageTypes`, `storage.DocumentTypes` for allow-lists; `AnyType` for a drive that takes any file). Clients get a presigned PUT from `POST /v1/uploads`, upload, then send the key; the owning endpoint checks `storage.KeyOwnedBy(key, purpose, userID)`. Private objects go out as short-lived `PresignGet`, or `PresignGetWith(…, GetOptions{ContentType, Disposition})` to force the type and `attachment` for anything that could run script (HTML, SVG, unknown). Server-side: `Stat` (check an upload exists and its real size before recording it), `ReadPrefix` (sniff a type), `Open`, `Put` (small objects), `Copy`, `NewKey`; a missing key is `ErrObjectNotFound` on S3 and local disk alike. Name downloads: a presigned URL is cross-origin, so the browser ignores `<a download>` and saves under the key unless the disposition carries `filename`. Bucket policy: `storage.BucketPolicy` (printed by `server -bucket-policy`) makes public purposes public-read and nothing else. Clean up with `Delete(key)` / `DeleteOwnerPurpose(owner, purpose)` (account deletion calls `DeleteOwner`; objects keyed by an org instead of the uploader survive it and are the module's to delete). Local disk in dev only. |
| `notify` | `d.Notify.Send(ctx, notify.Notification{UserID, Kind, Title, Body, Link})`. Core delivers in-app over realtime; the notifications feature adds inbox, push, email and preferences without callers changing. |
| `mail` | `d.Mail.Send(ctx, mail.Message{…})` with a plain-text body (HTML via `mail.Layout` optional) and optional small `Attachments` (an `.ics` invite). Resend in prod; logged or written to the sink in dev. |
| `jobs` | `go jobs.Every(ctx, d.Pool, "<module>.<job>", interval, fn)` from `Start`. Runs once per interval across all instances, never overlapping: a tick claims the job's row in `job_leases` with one statement (no connection held while `fn` runs); the finished run sets the next due time. `fn`'s context ends after 5 minutes or just under the interval, whichever is longer: batch bigger work. `job_leases` shows each job's last run, instance and error. |
| `bg` | `bg.Go(ctx, "<module>.<what>", fn)`: best-effort work that outlives the request (see Handlers). Only `cmd/server` and `cmd/seed` call `bg.Wait`, at exit. |
| `webhooks` | Verify (`VerifySvix`, `VerifyHexHMAC`, or the provider SDK), refuse when the secret is unset, then `webhooks.Once(ctx, pool, source, eventID, fn)`. Re-fetch state from the provider when money or permissions depend on it. |

## Config

- Read every variable through `config.String/Int/Bool/Duration/List/Required/RequiredOutsideDev`, core ones in `config.Load`, feature ones in the feature's `New`. A module that finds an invalid combination calls `config.Problem(...)`; never `log.Fatal` in a module. Bad values are collected and the boot fails once with all of them (`config.Check`).
- Unsafe combinations fail `Config.Validate` (dev bypass outside development, local URLs in production, missing S3/mail/webhook secrets outside development).
- Every variable is declared in `scaffold.json` and documented in `ENV.md` (env.md). `scaffold doctor` flags undeclared reads.
- Third-party services have an honest development fallback (local disk, logged mail, memory broker, dev auth) and are required outside development. No fake success paths in runtime code; tests may use fakes.

## Logging

`log/slog`: JSON to stdout outside development, text in development; `LOG_LEVEL=debug` for more. One line per request (`httpx.Logger`) with request id, status, duration and client IP. Log ids, never secrets, tokens, bodies, message content or prompts. A route that carries a secret in its path (calendar feed tokens, push tokens) registers `httpx.RedactPathPrefix("/v1/<module>/<route>/")` in `New`. Boot logs modules and which providers are live, as names and booleans.

## Security checklist

- Webhooks: signature required; an unset secret rejects everything.
- Validate ownership on every id a client sends (conversation membership, upload key owner, `reply_to` in the same conversation).
- Clients never set price, role, status or entitlement fields; the server computes them.
- Outbound fetches of user-supplied URLs go through an allow-list or a private-address guard (no SSRF).
- CORS lists exact origins in production. WebSocket origins follow CORS.
- Secrets live only in env and the host's secret store. Never in git, logs, error messages or `NEXT_PUBLIC_*`/`EXPO_PUBLIC_*`.
- Never commit exports of real user data (CSV, XLSX, PDF lists) to a repo.

## Verification

`cd backend && go build ./... && go vet ./...` must pass. Behaviour is verified on the dev-up stack (dev-up.md). Unit tests are for pure logic (money math, parsers, permission rules) where they are cheap; tests use a throwaway database or fakes that never ship in runtime paths.
