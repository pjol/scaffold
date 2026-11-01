# Architecture

The product and technical spec, and the source of truth. Write the Product sections before building domain code; update them in the same change as the code.

## Product

<one line: what the app does and for whom.>

### Roles
<Who uses it (signed-in user, staff support/admin, plus any app roles owned by a feature such as org admin) and what each can do.>

### Core flows
<The 3–6 journeys that matter, step by step, naming the screens on web and mobile.>

### Domain model
<Entities, their states and transitions, ownership, and which module owns each table.>

### Decisions
<Each non-obvious choice with its reason and date. Delete nothing; mark superseded entries.>

## Foundation (from the concrete template)

- **Backend** `backend/`: one Go binary (API + workers). Boot: config → db → migrate → realtime broker → storage → `app.Deps` → modules → router. Modules: `accounts` (me, consent, uploads, export, deletion, `/v1/config`, Clerk webhook), `admin` (users, roles, audit), plus installed features and app modules.
- **API surface**: `/healthz`, `/readyz`; `/v1/*` public; `/v1/*` signed-in (consent gate on writes); `/v1/admin/*` (support/admin); `/v1/ws` realtime; `/webhooks/*` (signed). Errors `{"error":{"code","message","details"}}`; lists `{items, next_cursor}`.
- **Realtime**: WebSocket `/v1/ws`; every connection gets `user:<id>`; modules own `<module>:<uuid>` topics. At-most-once; clients refetch on reconnect.
- **Data**: Postgres, migrations by ledger (core 0001–0099, features by block, app 5000–9999). Users are anonymised on deletion, never hard-deleted.
- **Auth**: Clerk session JWTs verified locally; users provisioned on first request; roles `support`/`admin`; dev bypass in development only.
- **Clients**: Next.js web and Expo mobile share `packages/shared` (types, client, hooks), `packages/tokens` (design), and their ui packages.

## API

<Route table for app modules: method, path, guard, purpose. Feature routes are in docs/features/*.md.>

## Realtime topics and events

<Topic → events → who may subscribe.>

## Background jobs

<Job name, interval, what it does. Core: accounts.finish_deletions every 10 minutes.>
