---
name: concrete
description: "The foundation every app is built on: Go 1.27 backend (chi, pgxpool, Postgres, S3, router-level auth, module contract), Next.js 16 web, Expo mobile, Clerk auth with a dev bypass, root-level design tokens (spacing system, type, colour, motion, OriginKit), the iterative 3-option design process, 3D figures and objects on the web (three/R3F: loading and fallbacks, characters as data, rigged kits, finishes, accessories, render recipes, generated characters, icon capture), showpiece scenes (beat sheets, reels, effects, synthesised sound), real logos recoloured by the surface, meaning-first writing, standardized env naming with ENV.md, the comprehensive dev-up script, the project doc set, and store/privacy compliance (account deletion, consent, UGC). Use when creating, reviewing or modifying any app on this stack, or before any backend, UI, copy, env, dev-tooling or compliance work in one."
---

# Concrete

The fixed foundation. `../template/` is the foundation as running code; these files are its rules. A project made with `scaffold new` already satisfies all of them. Keep it that way.

## Non-negotiables

1. **Stack**: Go backend (`chi` + `pgxpool` + embedded SQL migrations), Next.js web, Expo mobile, Postgres, S3-compatible storage, Clerk. No substitutions without the user's say-so.
2. **Module contract**: product code lives in modules (`New(d *app.Deps)` + `app` interfaces), features register from one `feature_<name>.go` file, routes are flat, auth is applied by route group.
3. **Zero-key development**: `scripts/dev-up/dev-up.sh` runs the whole stack with no third-party keys (dev auth, local storage, mail sink). Production refuses to boot without the real services.
4. **No stubs or fake data in runtime code.** Unconfigured features return an honest error; dev data lives in the database via `cmd/seed`.
5. **Users own their data**: in-app account deletion, data export and consent are wired through every module (`AccountDeleter`, `AccountExporter`). UGC requires moderation.
6. **Design**: tokens only, the 4 px spacing system and relationship ladder, no subtitles (tooltips), front pages only where necessary, motion with reduced-motion support, skeleton/empty/error on every async view, nothing that looks AI-generated.
7. **Copy is meaning-first**: factual, short, numerals, no banned words.
8. **Env**: standard names, declared in `scaffold.json`, documented in `ENV.md`, secrets never in clients, git or logs.
9. **Migrations**: forward-only, numbered by the ledger, never edited after applying.
10. **Never touch what you didn't start**: other running stacks, ports 3000–3010 and 8080, other projects' data. Never OTA a mobile app without a production build. Hours, if reported, are measured wall-clock (docs-set.md).

## Read by task

| Task | Read |
|---|---|
| Anything in a project | the project's `CLAUDE.md`, then this file |
| Backend code | `backend.md`, `database.md` |
| Sign-in, roles, dev auth | `auth.md` |
| Web screens | `web.md`, `design.md`, `writing.md` |
| Mobile screens, builds, store | `mobile.md`, `design.md`, `writing.md`, `compliance.md` |
| Visual identity / restyle | `design-process.md`, `design.md` |
| 3D figures, characters, objects (web) | `3d.md`, `web.md` |
| Showpiece sequences: openings, reels, reveals, effects, sound (web) | `scenes.md`, `3d.md` |
| Logos of real organisations and products | `design.md` (Logos and imagery) |
| Env vars | `env.md` |
| Local stack, scripts | `dev-up.md` |
| Project docs, branch scopes, hours | `docs-set.md` |
| Deletion, consent, UGC, payments rules, store review | `compliance.md` |
| Versions, layout, ports, commands | `stack.md` |

Read the whole file for the area you're touching; they're short on purpose.

## Changing the foundation

A change that should apply to every future app goes into `../template/` and the matching file here in the same edit, then gets verified by generating a fresh project (`scaffold new`) and running it. Patterns proven in a project are harvested back the same way.
