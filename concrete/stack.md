# Stack

Versions verified 2026-10-30. Upgrade deliberately (one layer at a time, after reading the release notes); record the change here and in the template.

| Layer | Choice | Version |
|---|---|---|
| Backend | Go, `net/http` + chi v5, `log/slog` | Go 1.27 toolchain (`go 1.26` in go.mod), chi 5.5 |
| Database | Postgres via pgx/pgxpool, embedded SQL migrations | Postgres 17, pgx 5.11 |
| Realtime | gorilla/websocket hub + broker (memory / Redis) | websocket 1.5.3, go-redis 9 |
| Storage | S3-compatible (AWS, R2, Tigris, MinIO) via aws-sdk-go-v2 | s3 1.114 |
| Auth | Clerk: clerk-sdk-go v2, `@clerk/nextjs` 7, `@clerk/expo` 4 | |
| Mail | Resend over HTTP (swappable `mail.Mailer`) | |
| Payments | Stripe (stripe-go v87, Stripe.js, stripe-react-native) behind `payments.Gateway` | |
| Web | Next.js App Router, React, Tailwind v4, `motion`, Radix, lucide, sonner | Next 16.4.3, React 19.2, Tailwind 4.3, motion 14 |
| Mobile | Expo (dev builds), expo-router, Reanimated 4, expo-maps | SDK 57 (move to 58 once Expo Go 58 ships), RN 0.86, Reanimated 4.5 |
| Data layer | TanStack Query 5 in `packages/shared`, shared by web and mobile | 5.104+ |
| Tooling | pnpm 10 workspace (`node-linker=hoisted` for Metro), TypeScript 6, Node 22 | |
| AI | Anthropic (default) or OpenAI over HTTP behind the assistant's provider interface | anthropic-sdk-go v2 / openai-go v3 when an SDK is wanted |
| MCP | official Go SDK `github.com/modelcontextprotocol/go-sdk` | 1.8 |

## Repository layout

```
<app>/
  scaffold.json            project manifest: name, slug, surfaces, installed features, env spec
  README.md CLAUDE.md AGENTS.md ENV.md   root docs (docs-set.md)
  backend/                 Go module <slug>/backend (backend.md)
  web/                     Next.js app (web.md)
  mobile/                  Expo app (mobile.md)
  packages/tokens/         design tokens, palettes, Tailwind theme, native theme (design.md)
  packages/ui-web/         web components
  packages/ui-native/      native components
  packages/shared/         API types + client, auth session, realtime, react-query hooks, legal docs
  design/                  design-process rounds and the capture tool (design-process.md)
  docs/                    ARCHITECTURE.md, DEPLOYMENT.md, legal/ exports
  branch-scopes/           one file per branch (docs-set.md)
  scripts/dev-up/          the dev stack (dev-up.md)
  .data/ logs/             local Postgres data and process logs (gitignored)
```

TS packages are consumed as source (no build step). Web and mobile import `@<slug>/shared`, `@<slug>/tokens` and their ui package; they never import each other.

## Ports

Never use 3000–3010 or 8080 locally; other stacks on the developer machine own them. dev-up gives each project a stable block derived from its slug (`DEV_PORT_BASE`, in 20000–29990 steps of 10):

| Offset | Service |
|---|---|
| +0 | API |
| +1 | web |
| +2 | Postgres (private cluster) |
| +3 | Expo Metro |
| +4 | MinIO (optional S3) |
| +5 | Redis (optional) |
| +6…+9 | feature hooks (`hook_port`) |

## Commands

```sh
scripts/dev-up/dev-up.sh                     # the whole stack (dev-up.md)
cd backend && go build ./... && go vet ./...  # backend gate
pnpm typecheck                               # all TS packages and apps
pnpm check                                   # tokens contrast + generated files, shared portability, legal versions
pnpm tokens                                  # regenerate token CSS/JSON after palette edits
scaffold doctor                              # manifest, migrations ledger, anchors, env, house rules
```

## Verification

Before calling work done: the backend gate, `pnpm typecheck`, `pnpm check` and `scaffold doctor` pass, and the change was exercised on the dev-up stack on web and mobile (light + dark, 375 px + desktop). Testing is human-in-the-loop on the running stack; add unit tests only for pure logic where cheap. Never add an e2e harness unless asked.
