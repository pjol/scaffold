# Environment

Every variable the app reads. The table is generated from `scaffold.json` by `scaffold env --write`; edit `scaffold.json`, not the table. Naming rules: `~/scaffold/concrete/env.md`.

## Local development

`scripts/dev-up/dev-up.sh` writes `backend/.env`, `web/.env.local` and `mobile/.env` from `scaffold.json` and `.env.dev`. Everything works with no keys. To use real providers locally, paste their test keys into `.env.dev` once (a key named without a prefix, like `CLERK_PUBLISHABLE_KEY`, also fills its `NEXT_PUBLIC_`/`EXPO_PUBLIC_` copies).

## Production

Values live in each host's secret store (backend host, Vercel, EAS secrets), never in files. `APP_ENV=production` makes the backend refuse to boot without the required ones.

## Getting values

- **Clerk**: dashboard → API keys (publishable + secret); Webhooks → add `https://<api>/webhooks/clerk` with `user.updated`, `user.deleted` → signing secret. Production: add the web origin and the mobile redirect URLs (`scaffoldapp:///` and `scaffoldapp://`).
- **S3**: any S3-compatible bucket (AWS, Cloudflare R2, Tigris). Public prefixes public-read, everything else private (`server -bucket-policy` prints the policy); `S3_PUBLIC_URL` is the CDN or bucket URL for public objects.
- **Resend**: verify the sending domain, then create an API key; `MAIL_FROM` must use that domain.

## Variables

<!-- scaffold:env:begin -->

### Backend — `backend/.env`

| Variable | Group | Required | Default | Dev value | What it is |
|---|---|---|---|---|---|
| `APP_ENV` | core | optional | `development` | `development` | development, staging or production. Gates the dev auth bypass, strict migrations and required secrets. |
| `CORS_ALLOWED_ORIGINS` | core | prod | — | — | Comma-separated browser origins allowed to call the API and open sockets. |
| `MOBILE_MIN_VERSION_ANDROID` | core | optional | `1.0.0` | — | Oldest Android build allowed; older builds show the update screen. |
| `MOBILE_MIN_VERSION_IOS` | core | optional | `1.0.0` | — | Oldest iOS build allowed; older builds show the update screen. |
| `PORT` | core | optional | `8080` | — | HTTP port. dev-up assigns it from the project's port block. |
| `PUBLIC_API_URL` | core | prod | `http://localhost:8080` | — | Public origin of this API (upload URLs, links). https in production. |
| `PUBLIC_WEB_URL` | core | prod | — | — | Public origin of the web app (email links, redirects, Clerk authorized party). |
| `RATE_LIMIT_IP_PER_MIN` | core | optional | `600` | — | Requests per minute per client IP on /v1. |
| `RATE_LIMIT_USER_PER_MIN` | core | optional | `300` | — | Requests per minute per signed-in user. |
| `RATE_LIMIT_WS_PER_MIN` | core | optional | `60` | — | New WebSocket connections per minute per IP. |
| `REDIS_URL` | core | optional | — | secret | Realtime broker across instances. Unset = in-memory (one instance). Source: Redis host. |
| `REQUEST_TIMEOUT` | core | optional | `20s` | — | Per-request timeout for /v1 routes (Go duration). |
| `RUN_WORKERS` | core | optional | `true` | — | Run module background workers on this instance. |
| `SUPPORT_EMAIL` | core | optional | `support@example.com` | — | Published support address (settings, footer, store listings). |
| `TRUSTED_PROXY_HOPS` | core | optional | `0` | — | Proxies in front of the API that append X-Forwarded-For (0 = use the TCP peer). |
| `DATABASE_URL` | database | always | — | secret | Postgres connection URL. Source: dev-up writes the local one; production from the database host. |
| `DB_MAX_CONNS` | database | optional | `20` | — | Pool size per instance. |
| `ADMIN_EMAILS` | auth | optional | — | `admin@scaffoldapp.test` | Comma-separated emails granted admin at sign-in. |
| `AUTH_DEV_BYPASS` | auth | optional | `false` | `true` | Accept dev:<subject>:<email> tokens. Refused unless APP_ENV=development. |
| `CLERK_SECRET_KEY` | auth | prod | — | secret | Clerk backend key. Optional in development when AUTH_DEV_BYPASS is on. Source: Clerk dashboard → API keys. |
| `CLERK_WEBHOOK_SECRET` | auth | prod | — | secret | Svix signing secret for /webhooks/clerk (user.updated, user.deleted). Source: Clerk dashboard → Webhooks → endpoint. |
| `LOCAL_UPLOAD_DIR` | storage | optional | `uploads` | `.data/uploads` | Upload directory when S3_BUCKET is unset (development). |
| `S3_ACCESS_KEY_ID` | storage | optional | — | secret | Access key id (unset = default AWS credential chain). |
| `S3_BUCKET` | storage | prod | — | — | Bucket for uploads. Unset = local disk (development only). Source: S3/R2/Tigris console. |
| `S3_ENDPOINT` | storage | optional | — | — | Endpoint for non-AWS providers (R2, Tigris, MinIO). |
| `S3_FORCE_PATH_STYLE` | storage | optional | `false` | — | Path-style addressing (MinIO). |
| `S3_PUBLIC_URL` | storage | prod | — | — | Public base URL for public-read objects (CDN or bucket URL). |
| `S3_REGION` | storage | optional | `us-east-1` | — | Bucket region. |
| `S3_SECRET_ACCESS_KEY` | storage | optional | — | secret | Secret access key. |
| `MAIL_FROM` | email | optional | `Scaffold App <no-reply@example.com>` | — | From header for transactional mail; the domain must be verified in Resend. |
| `MAIL_SINK_DIR` | email | optional | — | `.data/mail` | Development: write each email as a Markdown file here instead of logging it. |
| `RESEND_API_KEY` | email | prod | — | secret | Resend key. Unset = mail is logged, not sent (development). Source: resend.com → API keys. |
| `LOG_LEVEL` | observability | optional | `info` | — | info or debug. |

### Web — `web/.env.local`

| Variable | Group | Required | Default | Dev value | What it is |
|---|---|---|---|---|---|
| `NEXT_PUBLIC_API_URL` | core | always | — | — | Backend origin the browser calls. Source: dev-up writes it. |
| `NEXT_PUBLIC_APP_ENV` | core | optional | `production` | `development` | development, staging or production (gates dev auth in the browser). |
| `NEXT_PUBLIC_PALETTE` | core | optional | `base` | — | Default palette id from packages/tokens. |
| `CLERK_SECRET_KEY` | auth | prod | — | secret | Clerk secret for Next's Clerk middleware (server only). Source: Clerk dashboard → API keys. |
| `NEXT_PUBLIC_AUTH_DEV` | auth | optional | `false` | `true` | Show the dev identity picker instead of Clerk (non-production only). |
| `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY` | auth | prod | — | — | Clerk publishable key. Unset in development = dev sign-in only. Source: Clerk dashboard → API keys. |
| `NEXT_PUBLIC_S3_PUBLIC_URL` | storage | optional | — | — | Public bucket origin, the same value as the backend's S3_PUBLIC_URL. Lets next/image optimise avatars and uploads; unset = they load unoptimised. Source: Same as S3_PUBLIC_URL. |

### Mobile — `mobile/.env`

| Variable | Group | Required | Default | Dev value | What it is |
|---|---|---|---|---|---|
| `EXPO_PUBLIC_API_URL` | core | always | — | — | Backend origin the app calls. dev-up uses the machine's LAN IP so devices can reach it. Source: dev-up writes it. |
| `EXPO_PUBLIC_APP_ENV` | core | optional | `production` | `development` | development, staging or production. |
| `EXPO_PUBLIC_PALETTE` | core | optional | `base` | — | Default palette id from packages/tokens. |
| `EXPO_PUBLIC_WEB_URL` | core | prod | — | — | Public web origin for links the app shares (invites, events, posts) and opens (legal pages, wallet linking). Source: dev-up writes it; production = PUBLIC_WEB_URL. |
| `EXPO_PUBLIC_AUTH_DEV` | auth | optional | `false` | `true` | Show the dev identity picker instead of Clerk (non-production only). |
| `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` | auth | prod | — | — | Clerk publishable key. Unset in development = dev sign-in only. Source: Clerk dashboard → API keys. |

### Dev tooling — `.env.dev`

| Variable | Group | Required | Default | Dev value | What it is |
|---|---|---|---|---|---|
| `CLERK_PUBLISHABLE_KEY` | auth | optional | — | — | Clerk publishable key, pasted once: dev-up copies it to NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY and EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY. Source: Clerk dashboard → API keys. |
| `DEV_LAN_IP` | dev | optional | — | — | LAN IP that --lan/--tunnel write into EXPO_PUBLIC_API_URL and PUBLIC_API_URL. Unset = detected from the default network interface. |
| `DEV_PG_BIN` | dev | optional | — | — | Directory holding Postgres 17 initdb/pg_ctl/psql. Unset = /Library/PostgreSQL/17/bin, Homebrew, Postgres.app, then PATH; none found = docker compose. |
| `DEV_PORT_BASE` | dev | optional | — | — | First port of this project's local block (API +0, web +1, Postgres +2, Metro +3, MinIO +4, Redis +5, hooks +6…+9). Unset = derived from the slug; never 3000-3010 or 8080. |

<!-- scaffold:env:end -->
