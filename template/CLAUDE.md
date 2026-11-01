# CLAUDE.md

Guidance for anyone (human or agent) working in this repo. Read it in full before working.

## What this is

Scaffold App — <one line: what the product does and for whom; write it on day one>.

Built on the **concrete** foundation (`~/scaffold/concrete/`). Read `docs/ARCHITECTURE.md` (the product spec and source of truth) and `ENV.md` before changing anything; read the concrete file for the area you touch (`~/scaffold/concrete/SKILL.md` has the index).

## Layout

```
backend/            Go 1.27 · chi · pgxpool · embedded migrations (module scaffoldapp/backend)
web/                Next.js 16 · React 19 · Tailwind v4 · motion · Clerk
mobile/             Expo SDK 57 (dev builds) · expo-router · Reanimated 4 · Clerk
packages/tokens     design tokens, palettes, Tailwind theme, native theme
packages/ui-web     web components            packages/ui-native  native components
packages/shared     API types + client, auth session, realtime, react-query hooks, legal docs
design/             design rounds and the capture tool
scripts/dev-up/     the dev stack           docs/   ARCHITECTURE, DEPLOYMENT, legal, features
```

## Commands

```sh
scripts/dev-up/dev-up.sh                      # whole stack, zero keys; menu for admin/tokens/logs
cd backend && go build ./... && go vet ./...   # backend gate
pnpm typecheck && pnpm check                  # TS gate; tokens contrast, portability, legal versions
pnpm tokens                                   # after palette edits
scaffold doctor                               # manifest, migration ledger, anchors, env, house rules
```

Testing is human-in-the-loop on the dev-up stack (web + mobile, light + dark, 375 px + desktop).

## Backend conventions

- Modules: `New(d *app.Deps)` + the `app` interfaces; features register from `internal/api/feature_<name>.go`; flat routes; auth by route group (public / user / admin / webhooks).
- Handlers return errors (`*httpx.Error` for clients); JSON snake_case matching `packages/shared`; keyset pagination; `db.WithTx` for multi-statement writes; `audit.Log` in admin mutations; money as integer minor units.
- Every module storing user data implements `AccountDeleter` and `AccountExporter`.
- Migrations: `NNNN_<track>_<what>.sql`; this app writes the `app` track (5000–9999); never edit an applied file.
- Every env var read through `config.*`, declared in `scaffold.json`, documented by `scaffold env --write`.

## Frontend conventions

- Data through `@scaffoldapp/shared` hooks; never hand-roll fetches in screens.
- Tokens only (no raw colours, no default Tailwind palette); 4 px spacing system and relationship ladder; ui-package components first.
- **No subtitles** — context goes in `info` tooltips. Copy is meaning-first and factual (`concrete/writing.md`).
- Every async view has skeleton, empty and error states; motion on state changes with reduced motion respected.
- `/` is the product; front pages only where necessary.

## Installed features

<!-- scaffold add lists each installed feature above this anchor; specs are copied to docs/features/ -->
<!-- scaffold:features -->

## Project rules

<!-- Rules the user has set for this project. Add them as they come up. -->

## Never

- Touch other running stacks, their ports (3000–3010, 8080) or their data; stop processes you didn't start.
- OTA-publish the mobile app before a production build exists; mobile changes ship by rebuilding the binary.
- Edit an applied migration, commit secrets or real user data, or log tokens and message content.
- Report hours that weren't measured (`concrete/docs-set.md`).
