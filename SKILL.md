---
name: scaffold
description: "Root of the app-building skill tree and toolset: concrete (the Go/Next.js/Expo/Postgres/Clerk/S3 foundation, design system and process, 3D figures, animated scenes and real logos, env, dev-up, docs, compliance) plus feature-belt (copyable modules: chat, feed, places/maps, events, payments, wallets, AI assistant, notifications, moderation, orgs, support, identity, MCP/OAuth agent access, access/teams/sharing, presence, connectors, files, git repos, docs, CRM, calls, calendar/booking, channels, AI agent scopes, search), driven by the scaffold CLI (also available over MCP and HTTP). Use when starting a new app, adding a feature to an existing scaffold project, reviewing a project against the foundation, or improving the scaffold itself."
---

# Scaffold

Two layers and one toolset:

- **`concrete/`** — the foundation every app shares, as rules (`concrete/*.md`) and as running code (`template/`).
- **`feature-belt/`** — complete feature modules (migration + Go module + shared TS + web + mobile) that drop into any concrete app, plus the stitching each one needs.
- **`cli/`** — the `scaffold` toolset: one registry of operations exposed as a CLI, an MCP server and an HTTP API.

## Start a new app

1. Agree the brief with the user: name, one-line purpose, surfaces (web, mobile), features from `scaffold list`.
2. `bin/scaffold new <dir> --name "<Name>" [--slug x] [--features chat,feed]` (prints next steps; `--dry-run` first if unsure).
3. In the project: read `CLAUDE.md`, then `concrete/SKILL.md` (non-negotiables), then the files for your task.
4. `scripts/dev-up/dev-up.sh` — the stack runs with zero keys. Sign in with a dev identity.
5. Write `docs/ARCHITECTURE.md` for the product (domain, roles, API, decisions) before building domain code.
6. Run the design process (`concrete/design-process.md`) before building product screens; adopt the chosen design into `packages/tokens`.
7. Build domain modules on the module contract; stitch installed features (`FEATURE.md` → Stitching).
8. Gate every change: `go build ./... && go vet ./...`, `pnpm typecheck`, `pnpm check`, `scaffold doctor`, and use it in the running app on web and mobile.

## Add a feature to an existing app

`scaffold show <feature>` → `scaffold add <feature> --dry-run` → `scaffold add <feature>` → work the printed stitch list → restart dev-up → `scaffold doctor`. Details: `feature-belt/SKILL.md`.

## Toolset

| Command | Does |
|---|---|
| `scaffold list [--dir p]` | the feature catalog (✓ = installed in project p) |
| `scaffold show <feature>` | a feature's FEATURE.md |
| `scaffold spec [path]` | print any spec (`concrete/design`, `feature-belt/chat/FEATURE`); no path lists them |
| `scaffold new <dir> --name N` | a project from the template (+ `--features`, `--surfaces backend,web,mobile`, `--slug`, `--module`, `--bundle-id`) |
| `scaffold add <features…>` | install into the project in the current dir (`--dir`, `--dry-run`, `--force`) |
| `scaffold env [--write]` | the env spec; `--write` regenerates ENV.md and `.env.example` files; flags variables read in code but undeclared |
| `scaffold doctor` | checks manifest, migration ledger, anchors, root docs, env, house rules |
| `scaffold mcp` | every command as an MCP tool over stdio (`scaffold_<command>`) |
| `scaffold serve` | every command over HTTP: `GET /v1/ops`, `POST /v1/ops/{name}` (127.0.0.1:7420) |

`bin/scaffold` runs the CLI from source (`go run`); `--json` gives machine output. Register the MCP server with `claude mcp add scaffold -- <abs path>/bin/scaffold mcp`.

## Improving the scaffold

Read `CLAUDE.md` in this directory. In short: foundation changes go into `template/` and `concrete/` together; feature changes bump the feature's version; every change is verified by generating a fresh project and running it.
