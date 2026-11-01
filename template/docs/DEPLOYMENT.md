# Deployment

Three environments: `development` (dev-up), `staging`, `production`. Staging mirrors production with test-mode provider keys.

## Backend (API + workers)

- One container from `backend/Dockerfile` (distroless, non-root). Any host that runs containers with health checks: Railway, Fly.io, Render, ECS.
- Health check `GET /readyz` (database + storage). Liveness `GET /healthz`.
- Migrations run on boot under an advisory lock; deploys are safe with several instances. Never edit an applied migration (the boot fails outside development if one changes).
- More than one instance needs `REDIS_URL` (realtime fan-out). `RUN_WORKERS=true` on every instance is fine (jobs take advisory locks).
- Behind a proxy or load balancer, set `TRUSTED_PROXY_HOPS` so rate limits key on the real client IP.
- Required outside development (the boot refuses otherwise): see `ENV.md` rows marked `prod`/`always`.

## Database

Managed Postgres 17 (Neon, Supabase, RDS, Railway). Daily backups with point-in-time recovery. `DB_MAX_CONNS` × instances below the server's connection limit; behind PgBouncer use transaction pooling (the migration and job locks are transaction-scoped and safe).

## Object storage

One bucket per environment. Bucket policy: public-read for purposes registered as public (`avatar/`, and features' public purposes), private for everything else. CORS on the bucket: `PUT` and `GET` from the web origin (presigned uploads). `S3_PUBLIC_URL` → a CDN in front of the bucket.

## Web (Next.js)

Vercel or any Node host (`next build && next start`). Env: the `web` rows of `ENV.md`. Add the production origin to `CORS_ALLOWED_ORIGINS` and `PUBLIC_WEB_URL` on the backend and to Clerk's allowed origins.

## Mobile (Expo)

- EAS Build with profiles `development` (dev client), `preview` (internal TestFlight / Play internal), `production`. Env via EAS secrets (`EXPO_PUBLIC_*` are baked into the binary).
- No over-the-air updates until a production build exists; changes ship by rebuilding the binary.
- Version: bump `version` (user-facing) and let EAS auto-increment build numbers. Raise `MOBILE_MIN_VERSION_IOS/ANDROID` on the backend to force updates when an old build breaks.
- Clerk: register the bundle id / package, the `scaffoldapp:///` and `scaffoldapp://` redirects, and the Android SHA-256 for passkeys.
- Before submitting, run the checklist in `~/scaffold/concrete/compliance.md` (privacy policy, labels/Data safety, manifest, deletion, UGC, payments, age rating, review notes with a demo account).

## Providers

| Provider | Production setup |
|---|---|
| Clerk | production instance, domain, webhook `https://<api>/webhooks/clerk` (`user.updated`, `user.deleted`) |
| Resend | verified sending domain (SPF, DKIM), API key |
| S3 | bucket + policy + CORS + CDN |
| Feature providers | see each `docs/features/<name>.md` → Config |

## Release checklist

1. Backend gate, `pnpm typecheck`, `pnpm check`, `scaffold doctor` clean on the release commit.
2. Staging deploy; smoke the core flows on web and a mobile build.
3. Production deploy: backend first (migrations), then web, then submit mobile builds.
4. Watch logs and `/readyz` for 30 minutes.
