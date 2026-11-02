---
name: feature-belt
description: "Catalog of copyable feature modules for apps built on the concrete foundation (Go/chi/pgxpool backend, Next.js web, Expo mobile): chat, social feed, places/maps, events, payments (Stripe, swappable), wallets/blockchain, AI assistant, notifications/push, moderation, orgs/teams, support, identity verification, MCP/OAuth access for agents, teams and sharing (access), presence, connected platforms and external MCP connectors, file drive, git repositories, Notion-style docs, CRM, video calls, calendars with booking links, Slack-style channels, AI agent scopes and suggestions, global search. Use when adding a feature to a scaffold project, choosing features for a new app, or building/updating a feature module. Install with the scaffold CLI, then stitch."
---

# Feature belt

Features are complete vertical slices — migration, Go module, shared TS types/hooks, web screens, mobile screens — that drop into any project made from `template/`. Installing copies code; stitching (a little custom code per app) connects it to the product. Don't try to make features configure themselves into every possible app: ship sensible defaults and a clear stitch list.

Read `../concrete/SKILL.md` first. Every feature obeys it.

## Catalog

| Feature | Block | Requires | What it adds |
|---|---|---|---|
| `notifications` | 0100 | — | device tokens, Expo push, in-app inbox + unread badge, per-kind preferences, email channel; replaces `Deps.Notify` |
| `moderation` | 0200 | — | reports on any content or user, blocks, pre-post content filter, admin review queue; required by every UGC feature |
| `orgs` | 0300 | notifications | organizations/teams, memberships with roles, email invites, org-scoped guards |
| `support` | 0400 | — | feedback/tickets with attachments, staff replies by email, admin queue |
| `identity` | 0500 | — | identity verification (Persona; manual review fallback), verified badge |
| `mcp` | 0600 | — | MCP endpoint exposing every feature's tools to agents as the user: OAuth 2.1 authorization server (DCR, PKCE, resource-bound rotating tokens, connected apps) and personal API keys; agent-only `agt_` credentials; agent-scope gate |
| `access` | 0700 | orgs, notifications | teams; resource sharing to people, teams or the whole org with inheritance, general access and public links; org permission matrix; `access_level()`/`access_visible()` as the single source of truth; Share dialog |
| `presence` | 0800 | — | online/away from heartbeats, custom status, Do Not Disturb, visibility policy |
| `connectors` | 0900 | orgs | connected platforms (Google, Microsoft, GitHub, GitLab, Notion, HubSpot, Salesforce, Zoom) each person connects once for every feature: `app.Connections` (signing, refreshing, host-locked client), incremental scopes, multiple accounts, personal tokens, workspace policy, webhook ingress; plus the assistant's external MCP servers (discovery, DCR/CIMD, PKCE, sealed tokens, SSRF guard) |
| `chat` | 1000 | notifications, moderation | DMs and groups, realtime delivery, typing, read state, unread counts, attachments, replies, report/block |
| `feed` | 1100 | notifications, moderation | posts with media, comments (one level), reactions, mentions, pins/announcements, keyset feed |
| `places` | 1200 | — | places with coordinates, map + list views (web MapLibre, mobile expo-maps), geocoding, bounding-box search |
| `events` | 1300 | notifications | events with time/place, RSVP, capacity, reminders, calendar export |
| `payments` | 1400 | notifications | Stripe behind a `Gateway` interface: customers, checkout, subscriptions/entitlements, webhooks, ledger, refunds; Connect payouts optional |
| `wallets` | 1500 | — | EVM wallets (embedded via Privy or linked), chain config, token balances and transfers, indexer webhook |
| `assistant` | 1600 | moderation | AI chat assistant: provider-neutral (Anthropic/OpenAI), tool registry scoped to the user, guardrails, streaming, quotas, consent |
| `files` | 1700 | access, moderation | company drive: My drive and shared folders, uploads with versions, previews, trash, quotas, private / team / org / public-link sharing, item types other features register |
| `docs` | 1800 | access, notifications, moderation | Notion-style pages (block model, realtime block ops), databases with table/board views, comments, history, publish to web |
| `crm` | 1900 | access, notifications | companies, contacts, deals on pipelines, activities and tasks, custom properties, saved views, import/export, record access modes on access |
| `calls` | 2000 | orgs, notifications | LiveKit video meetings with Meet-style links, lobby for guests, host controls, instant calls that ring; dev hook runs a local LiveKit |
| `calendar` | 2100 | orgs, notifications | personal calendars with recurrence and `.ics` invites, iCal busy calendars, booking links (internal = org members, external = anyone), conferencing seam |
| `channels` | 2200 | orgs, presence, notifications, moderation | Slack-style workspace messaging: public/private channels, DMs, threads, forum channels, mentions incl. groups, pins, saved, search; call and group seams |
| `agents` | 2300 | access, notifications | per-person AI agent scope (Off / Read / Suggest / Edit) by default, feature, service and resource; suggestions inbox with diffs; implements `app.AgentPolicy` and `access_agent_cap()` |
| `repos` | 2400 | files | git repositories as drive items: smart-HTTP clone/push with git credentials, protected branches, in-browser commits, pull requests with reviews and merges; agent tools for branch → commit → PR |
| `search` | — | — | ⌘K global search across every `app.SearchProvider`, as the caller, with source chips and badges for connected-platform hits; `search_everything` agent tool |

Core (always present, not in the belt): auth (Clerk + dev bypass), users/profile, legal consent, account deletion and export, uploads, admin console (users, roles, audit), realtime, mail, jobs (leased, once per tick across instances), bg (tracked best-effort background work). See `../concrete/backend.md`.

`feature-belt/agents/openai.yaml` is this skill's Codex metadata (skill convention), not part of the `agents` feature.

`scaffold list` prints the live catalog; `scaffold show <feature>` prints a feature's FEATURE.md.

## Install

```sh
scaffold add chat            # resolves requirements (notifications, moderation) first
scaffold add chat --dry-run  # shows files, wiring and env without writing
```

`add` copies each feature's files into the project with token replacement, inserts wiring lines above `scaffold:<anchor>` comments, merges its env vars into `scaffold.json`, regenerates `ENV.md` and the `.env.example` files, records the version in `scaffold.json`, and prints the stitch list. Then:

1. Read the feature's `FEATURE.md` in full.
2. Work the stitch list (navigation placement, which screens link to it, app-specific rules, copy).
3. Restart dev-up (migrations apply on boot), run `scaffold doctor`, and check the feature in the running app on web and mobile.

Removing a feature: delete `backend/internal/api/feature_<name>.go` (unplugs it), its package and UI folders, its wiring lines and env entries. Its tables stay; drop them with an `app` migration if wanted.

## Module contract

Every feature directory has this shape:

```
feature-belt/<name>/
  FEATURE.md            the spec (format below) — the source of truth
  feature.json          manifest the CLI reads
  backend/<name>/       Go package → backend/internal/<name>/
  backend/feature_<name>.go → backend/internal/api/feature_<name>.go
  migrations/           NNNN_<name>_<what>.sql → backend/migrations/
  shared/               → packages/shared/src/<name>/ (types.ts, api.ts, hooks.ts, index.ts)
  web/                  → web/src/features/<name>/ (components, screens)
  web-app/              → web/app/… (route files, thin: import screens from web/src/features)
  mobile/               → mobile/src/features/<name>/
  mobile-app/           → mobile/app/… (route files, thin)
  mobile-plugin/        → mobile/plugins/ (Expo config plugins, wired at scaffold:plugins)
  scripts/dev-up/hooks/<name>.sh → same path (local services; concrete/dev-up.md)
```

**Backend**
- Package `<name>` with `New(d *app.Deps) *Service`, `Name() string`, and only the `app` interfaces it needs (route groups incl. `StreamRoutes`/`RootRoutes`, `Worker`, `TopicAuthorizer`, `AccountDeleter`, `AccountDeletedHook`, `AccountExporter`, `DevSeeder`, `ToolProvider`). Storing user data ⇒ `AccountDeleter` + `AccountExporter` are mandatory; provider calls and socket revocation on deletion go in `AccountDeletedHook`.
- Invalid settings call `config.Problem`; secrets in URL paths register `httpx.RedactPathPrefix`; uploads register a `storage.Purpose` (with `ConsentExempt` when needed before consent).
- Registers from `feature_<name>.go` via `api.RegisterModule` (and `api.RegisterDeps` only to replace a shared service).
- Reads settings with `config.*` in `New`; every variable appears in `feature.json` → `env`.
- Flat routes under a feature prefix (`/chat/...`, `/admin/chat/...`); JSON snake_case matching `shared/types.ts`; keyset pagination; ownership checks on every client-supplied id; rate limits on writes users can spam.
- Cross-feature calls go through `Deps` services, or through the exported functions of a feature listed in `requires` (always installed). Never import a feature you don't require; optional integrations go through an interface the stitching code wires.
- Agent tools: implement `app.ToolProvider` for the useful read operations (and safe writes) so the `mcp` and `assistant` features can offer them.
- Realtime topics `"<name>:<uuid>"` + a `TopicAuthorizer`; events `"<name>.<event>"`.
- Development data through `DevSeeder` (realistic, small, idempotent).

**Migrations**: numbered inside the feature's block (`1000_chat_init.sql`, later `1001_chat_…`). Reference only core and lower blocks. Never edit a shipped file; ship the next number.

**Shared TS**: `types.ts` (DTOs mirroring Go), `api.ts` (`<name>Endpoints(http)`), `hooks.ts` (react-query hooks + query keys + `registerRealtimeHandler` cache updates), `index.ts`. Portable (no DOM/Node APIs). Every feature is re-exported from one index, so **exported names carry the feature name** (`useChatInbox`, `useRemoveOrgMember`, `ChatMessage`), never generic ones (`useRemoveMember`, `Message`).

**UI**: tokens and ui-package components only; no subtitles (`info` tooltips); skeleton/empty/error states; works at 375 px and with the largest text; web screens in `web/src/features/<name>/` with thin route files; mobile screens likewise. Navigation entries go through anchors, not hand edits to core files.

**Anchors in the template** (the CLI inserts above them; an anchor is a comment that *starts* with `scaffold:<name>`, so prose mentioning one never matches). Inserting is idempotent: a line identical to one already in the file is skipped, so every inserted line must be unique in its file (give each registry entry its own constant: `chatNav,`, `chatHomeTile,`):

| File | Anchor | Inserts |
|---|---|---|
| `packages/shared/src/index.ts` | `scaffold:exports` | `export * from "./<name>";` |
| `web/src/features/registry.tsx` | `scaffold:imports`, `scaffold:nav`, `scaffold:home`, `scaffold:admin-nav`, `scaffold:settings`, `scaffold:account-menu`, `scaffold:session`, `scaffold:header-actions`, `scaffold:consent-routes`, `scaffold:support-links`, `scaffold:resource-menu` | nav items, home tiles, admin tabs, settings sections, account menu entries, signed-in session mounts, top-bar actions, routes open during consent, support links, ⋮ menu providers |
| `web/proxy.ts` | `scaffold:public-paths` | public path patterns (`"/invites/(.*)",`) |
| `mobile/src/features/registry.tsx` | `scaffold:imports`, `scaffold:home`, `scaffold:settings`, `scaffold:public-routes`, `scaffold:consent-routes`, `scaffold:support-links`, `scaffold:session`, `scaffold:resource-menu` | home entries, settings sections, routes reachable signed out (`"invites/[token]",`), routes open during consent or while blocked, support links, signed-in session mounts, ⋮ menu providers |
| `mobile/app/(tabs)/_layout.tsx` | `scaffold:imports`, `scaffold:tabs` | tab icon import, `<Tabs.Screen …>` |
| `mobile/app.config.ts` | `scaffold:plugins` | config plugin entries (`"./plugins/withPlaces",`; the file ships as `mobile/plugins/<name>.js`) |

## feature.json

```json
{
  "name": "chat", "title": "Chat", "version": "1.0.0",
  "summary": "One line: what the user can do.",
  "requires": ["notifications", "moderation"],
  "surfaces": ["backend", "web", "mobile"],
  "migration_block": 1000,
  "env": [{ "name": "CHAT_MESSAGE_MAX_CHARS", "tier": "backend", "group": "chat", "required": "optional",
            "default": "4000", "doc": "Longest message accepted, in characters." }],
  "files": [
    { "src": "backend/chat", "dst": "backend/internal/chat" },
    { "src": "backend/feature_chat.go", "dst": "backend/internal/api/feature_chat.go" },
    { "src": "migrations", "dst": "backend/migrations" },
    { "src": "shared", "dst": "packages/shared/src/chat" },
    { "src": "web", "dst": "web/src/features/chat" },
    { "src": "web-app", "dst": "web/app" },
    { "src": "mobile", "dst": "mobile/src/features/chat" },
    { "src": "mobile-app", "dst": "mobile/app" }
  ],
  "wire": [
    { "file": "packages/shared/src/index.ts", "anchor": "exports", "insert": "export * from \"./chat\";" }
  ],
  "deps": { "web": { "maplibre-gl": "^6.16.0" }, "mobile": { "expo-maps": "~57.0.0" } },
  "stitch": ["Decide who may start a conversation with whom (default: any signed-in user who hasn't blocked them)."]
}
```

`deps` lists npm packages the feature's client code needs per workspace package (`web`, `mobile`, `shared`, `ui-web`, `ui-native`); `add` inserts the missing ones into that `package.json` and tells you to run `pnpm install`. Go dependencies need no manifest entry: `add` runs `go mod tidy`. `dst` paths decide surfaces: `backend/`, `web/`, `mobile/`, `packages/` (web or mobile). Files for a surface the project lacks are skipped. Use the template tokens (`scaffoldapp`, `Scaffold App`) wherever the project name belongs.

## FEATURE.md format

Keep it under ~150 lines. Sections, in order:
1. **What it does** — 3–6 bullets of user-visible behaviour.
2. **Data** — tables with purpose (the migration is the exact schema).
3. **API** — route table: method, path, guard, purpose. Realtime topics/events.
4. **Screens** — web and mobile, where they mount, states.
5. **Config** — env vars (mirrors feature.json).
6. **Compliance** — deletion/export behaviour, UGC duties, store notes.
7. **Stitching** — the decisions and edits each app makes, numbered.
8. **Extending** — the supported seams (interfaces, hooks) and what not to change.
9. **Provenance** — which project it was distilled from and what was fixed on the way.

## Building or updating a feature

- Start from the best source on this machine (listed in the catalog's provenance), keep its proven mechanics, strip the domain, fix known bugs.
- Verify by installing into a fresh project: `scaffold new /tmp/x --name X --features <name>` (0 anchor warnings) then `go build ./... && go vet ./...`, `pnpm install && pnpm typecheck && pnpm check`, `scaffold doctor`, the web build, `expo export`, boot dev-up, and exercise the flows on web (Playwright: 375 + 1440, light + dark, clean console) and mobile.
- Bump `version` in feature.json on any change; describe the change at the bottom of FEATURE.md. Projects compare versions with `scaffold doctor` and port changes by hand.
