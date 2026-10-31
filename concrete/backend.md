# Backend

Go API in `backend/`, module path `<slug>/backend`. The template's code is the reference for every rule here; read the package before changing a pattern.

## Shape

One binary (`cmd/server`) runs the API and the background workers. Boot order: `config.Load` → logging → `db.Connect` → `db.Migrate` → realtime broker → storage → `app.Deps` → `api.Build` → `config.Check` → `Start` → `http.Server`. `cmd/seed` fills a development database.

```
backend/
  cmd/server/            boot + graceful shutdown
  cmd/seed/              idempotent dev data (development only)
  internal/app/          Deps + the module interfaces (the contract every feature implements)
  internal/api/          router, Build, feature registry (feature_<name>.go files)
  internal/config/       Config + env helpers (String/Int/Bool/Duration/List/Required)
  internal/db/           pool, Migrate, WithTx, TryAdvisoryLock, error helpers
  internal/httpx/        Handler, Error, Decode, Page/cursor, middleware, Detach
  internal/auth/         Provider (Clerk), Identity, guards, dev tokens
  internal/legal/        versions + consent gate
  internal/accounts/     me, consent, uploads, export, deletion, Clerk webhook, /v1/config
  internal/admin/        users, roles, status, audit log
  internal/audit/ realtime/ storage/ mail/ notify/ jobs/ webhooks/
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

- A feature registers from one file, `internal/api/feature_<name>.go` (`RegisterModule`; `RegisterDeps` when it replaces a shared service such as `d.Notify`). No other core file changes.
- Register **flat** routes inside `Mount*` (`r.Get("/chat/conversations", …)`). Never `r.Route`/`r.Mount` in a module: modules share prefixes and chi panics on a duplicate mount.
- Any module that stores user data implements `AccountDeleter` and `AccountExporter`. This is a store requirement, not a nicety.
- Modules talk to each other through `Deps` services (`Notify`, `Storage`, `Mail`, `Broker`). A feature may import the packages of features in its `requires` (they are always installed) and use their exported functions; it never imports anything else. Package-level registries (e.g. `moderation.RegisterTarget`) are filled in `New` and read at request time, so construction order never matters.
- Agent tools: read-only by default; never a tool that deletes user data, sends anything outward or fetches arbitrary URLs.

## Handlers

- `httpx.Handler(func(w, r) error)`. Return `*httpx.Error` for client errors (`BadRequest`, `NotFound`, `Conflict`, `Forbidden`, `NewError(status, code, msg)`); plain errors become logged 500s with a generic message. Error JSON is `{"error":{"code","message","details"}}`; `code` is stable snake_case clients branch on, `message` is user-facing copy (writing.md).
- Decode with `httpx.Decode` (1 MiB, unknown fields rejected, empty body = `{}`, so validate required fields). Respond with `httpx.OK/JSON/NoContent`.
- Identity: `auth.MustFromContext(r.Context())` inside user/admin routes only.
- Lists return `httpx.Page[T]{items, next_cursor}`: keyset pagination on `(created_at, id)` fetched with `limit+1` (`httpx.NewPage`). Never offset pagination; never a cursor without a tie-breaker.
- JSON is snake_case and must match `packages/shared` types exactly. Change both in the same commit.
- Money is integer minor units (`int64` cents) or `NUMERIC` + decimal strings on the wire; never float.
- Multi-statement writes go in `db.WithTx`. Admin mutations call `audit.Log(ctx, tx, …)` in the same transaction.
- Routes that call a payment provider inside a transaction use `httpx.Detach(timeout)` so a dropped client cannot roll back after the provider acted. Pair with an `Idempotency-Key`.
- Side effects that must not fail the request (notify, realtime, email) are best-effort: log and continue.
- Expensive or abusable routes add `auth.RateLimit(n, window)` on top of the global limits.

## Auth

`auth.Provider` verifies tokens (Clerk by default; `auth.Clerk` checks session JWTs locally against cached JWKS and the `azp` origin). The users row is provisioned on first request from the provider's **verified** email and keyed on `auth_subject`. Roles: `admin_role` ∈ {support, admin}; `ADMIN_EMAILS` grants admin at sign-in. Status: active | suspended (reads only, `auth.RequireActive` on mutations) | banned | deleted. Development: `AUTH_DEV_BYPASS=true` (refused unless `APP_ENV=development`) accepts `Bearer dev:<subject>:<email>`. Full flow across tiers: auth.md.

## Shared services

| Service | Use |
|---|---|
| `realtime` | `d.Broker.Publish(ctx, topic, eventType, data)`. Topics `"<kind>:<uuid>"` via `realtime.Topic`; the hub owns `user:<id>` and `admin:live`. Delivery is at-most-once: clients refetch on reconnect. Memory broker in dev, Redis (`REDIS_URL`) for more than one instance. Revoke access with `realtime.Revoke`. |
| `storage` | `storage.RegisterPurpose(Purpose{Name, Public, Types, MaxBytes, ConsentExempt})` in `New` (`storage.ImageTypes`, `storage.DocumentTypes` for allow-lists). Clients get a presigned PUT from `POST /v1/uploads`, upload, then send the key; the owning endpoint checks `storage.KeyOwnedBy(key, purpose, userID)`. Private objects go out as short-lived `PresignGet`. Clean up with `Delete(key)` / `DeleteOwnerPurpose(owner, purpose)` (account deletion calls `DeleteOwner`). Local disk in dev only. |
| `notify` | `d.Notify.Send(ctx, notify.Notification{UserID, Kind, Title, Body, Link})`. Core delivers in-app over realtime; the notifications feature adds inbox, push, email and preferences without callers changing. |
| `mail` | `d.Mail.Send(ctx, mail.Message{…})` with a plain-text body (HTML via `mail.Layout` optional). Resend in prod; logged in dev. |
| `jobs` | `go jobs.Every(ctx, d.Pool, "<module>.<job>", interval, fn)` from `Start`. |
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
