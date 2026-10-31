# Environment variables

One naming standard across every project, one machine-readable source (`scaffold.json` → `env`), one human doc (`ENV.md` at the project root, generated table + hand-written notes).

## Names

- `SCREAMING_SNAKE_CASE`, provider or subsystem first, then the thing: `CLERK_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `S3_BUCKET`, `CHAT_MESSAGE_MAX_CHARS`.
- Suffixes carry meaning:

| Suffix | Meaning | Example |
|---|---|---|
| `_URL` | absolute URL, no trailing slash | `PUBLIC_API_URL`, `DATABASE_URL` |
| `_SECRET`, `_SECRET_KEY` | secret: never in clients, logs or git | `STRIPE_SECRET_KEY` |
| `_KEY` | API key (secret unless `PUBLISHABLE`) | `RESEND_API_KEY` |
| `_PUBLISHABLE_KEY` | safe for clients | `CLERK_PUBLISHABLE_KEY` |
| `_ID` | identifier | `S3_ACCESS_KEY_ID` |
| `_ENABLED` | boolean feature switch | `CHAT_ATTACHMENTS_ENABLED` |
| `_TTL`, `_TIMEOUT`, `_INTERVAL` | Go duration string (`30s`, `15m`) | `REQUEST_TIMEOUT` |
| `_MAX_*`, `_MIN_*` | limits | `UPLOAD_MAX_MB` |
| `_PER_MIN` | rate limits | `RATE_LIMIT_IP_PER_MIN` |

- Feature settings are `<FEATURE>_<SETTING>` (`ASSISTANT_MODEL`, `PAYMENTS_PROVIDER`).
- Client copies keep the same name with the framework prefix, so one grep finds every tier: `CLERK_PUBLISHABLE_KEY` → `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY`, `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY`. Only `NEXT_PUBLIC_*`/`EXPO_PUBLIC_*` reach clients; nothing secret ever carries those prefixes.
- Booleans are `true`/`false`. Lists are comma-separated.

## Core variables

| Name | Tier | Notes |
|---|---|---|
| `APP_ENV` | backend | development \| staging \| production. Gates dev bypass, strict migrations, required secrets. |
| `PORT` | backend | dev-up assigns it |
| `DATABASE_URL` | backend | required |
| `PUBLIC_API_URL`, `PUBLIC_WEB_URL` | backend | https in production |
| `CORS_ALLOWED_ORIGINS` | backend | exact origins, required outside development |
| `CLERK_SECRET_KEY`, `CLERK_WEBHOOK_SECRET` | backend | Clerk dashboard |
| `AUTH_DEV_BYPASS` | backend | development only |
| `ADMIN_EMAILS` | backend | comma list, granted admin at sign-in |
| `S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, `S3_PUBLIC_URL`, `S3_FORCE_PATH_STYLE` | backend | required outside development |
| `RESEND_API_KEY`, `MAIL_FROM`, `SUPPORT_EMAIL` | backend | mail is logged when the key is unset (development) |
| `REDIS_URL` | backend | needed for more than one instance |
| `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY`, `NEXT_PUBLIC_AUTH_DEV`, `NEXT_PUBLIC_APP_ENV`, `NEXT_PUBLIC_PALETTE`, `NEXT_PUBLIC_S3_PUBLIC_URL` | web | `CLERK_SECRET_KEY` also on web (Clerk's Next middleware) |
| `EXPO_PUBLIC_API_URL`, `EXPO_PUBLIC_WEB_URL`, `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY`, `EXPO_PUBLIC_AUTH_DEV`, `EXPO_PUBLIC_APP_ENV`, `EXPO_PUBLIC_PALETTE` | mobile | `EXPO_PUBLIC_WEB_URL` = web origin for shared links |

The template's `scaffold.json` holds the complete list with defaults and docs.

## Files

| File | Read by | Git |
|---|---|---|
| `.env.dev` (+ `.env.dev.example`) | dev-up only: ports, secrets you paste once | ignored / committed |
| `backend/.env` (+ `.example`) | Go (`godotenv`, real env wins) | ignored / committed |
| `web/.env.local` (+ `web/.env.example`) | Next | ignored / committed |
| `mobile/.env` (+ `mobile/.env.example`) | Expo | ignored / committed |

dev-up generates the three runtime files from `.env.dev` plus computed values (ports, LAN IP, dev auth), so a developer edits one file. Production values live in the host's secret store, never in files.

## The workflow

1. Add the variable to `scaffold.json` → `env` (`name, tier, group, required: always|prod|optional, default, dev, secret, doc, source`).
2. Read it through `config.*` (backend) or `process.env.NEXT_PUBLIC_*`/`EXPO_PUBLIC_*` literals (clients; bundlers inline only literal names).
3. `scaffold env --write` regenerates `ENV.md`'s table and the `.example` files. Write the hand-written part of `ENV.md` (where to get the value, gotchas) outside the generated markers.
4. `scaffold doctor` fails the env check when code reads a variable `scaffold.json` doesn't declare.

Required-ness: `always` fails the boot everywhere, `prod` fails outside development, `optional` has a safe default or a disabled path.
