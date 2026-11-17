# Reference catalog

A large, read-mostly catalog of items (products, parts, species, titles: thousands to hundreds of thousands) imported from an outside source, kept in memory on every instance, resolved by name and searched fast. Not the `exchange` feature's catalog of markets.

## What it does

- **Imports** every item from one source (JSON array or NDJSON over HTTPS, a file, or any API through a registered `catalog.Source`) nightly at a set UTC hour and when staff press **Sync now**, one sync at a time across instances. A sync inserts new keys, updates changed items, retires items the source stopped listing and restores ones it lists again; nothing is ever deleted, and staff retirements hold.
- **Safety valve**: a sync that would retire more than `CATALOG_MAX_RETIRE_PERCENT` of the active items (catalogs of 100 or more), or a pull with no usable items, is refused and changes nothing; staff can force it once.
- **Resolves names** as people type or paste them: exact name, then alias, then a close spelling (≤ 2 edits on names of ≥ 6 characters, only when one item is closest), else unresolved (`unknown` or `ambiguous`) with up to 5 candidates. Accents, case, curly quotes, dashes, odd spaces and `A/B` · `A // B` · `A | B` don't matter. A renamed item keeps its old name as an alias.
- **Searches** by name (exact, starts with, a later word, contains), then aliases; filtered by category; on the device over a compact index when the catalog is small enough, else on the server.
- People get **Catalog** (search with category chips, browse, item pages) on web and mobile; ⌘K global search and agents find items too. Staff get **Admin → Catalog**: sync status and log, Sync now, look up, retire and restore items (audited).

## Data

| Table | Purpose |
|---|---|
| `catalog_items` | One row per source key: name, aliases (only grow), category, subtitle, image URL, attributes (string map), reference price + currency, retired at/by (`source` or `staff`), first seen, updated |
| `catalog_state` | The catalog version, bumped in the same transaction as every change; each instance reloads its index when it moves |
| `catalog_sync_runs` | The sync log (trigger, source, status `running`/`ok`/`failed`/`refused`, counts, error, first skip reasons) and its lock: one `running` row at a time, heartbeat every 30 s, a silent one failed after 2 minutes |

In memory: an immutable index (items in name order, name and alias maps, a length-bucketed fuzzy table with character masks, joined key strings for substring scans) behind an `atomic.Pointer`, swapped after each load. Memory is about the size of the data (about twice during a sync); `CATALOG_MAX_ITEMS` caps it.

## API

Reads are public with `CATALOG_PUBLIC_READS=true` (default), signed-in otherwise; until an instance has loaded the catalog they answer 503 `catalog_loading`.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/catalog` | reads | `{version, active, retired, categories: [{name, count}], client_index}` |
| GET | `/v1/catalog/search?q=&category=&retired=&limit=` | reads | `{items}` ranked, ≤ 50; `q` under 2 characters → 400 `query_too_short` |
| GET | `/v1/catalog/items?category=&retired=&cursor=&limit=` | reads | Browse by name, keyset pages |
| GET | `/v1/catalog/items/{key}` | reads | One item, retired too |
| POST | `/v1/catalog/resolve` | reads, `CATALOG_RESOLVE_PER_MINUTE` | `{names: [≤ 500]}` → `{results: [{name, ok, match?, reason?, item, candidates}]}` in order |
| GET | `/v1/catalog/index` | reads | Every item compact (key, name, aliases, category, subtitle, image, price, retired) for search on the device; ETag + gzip, 304 on match; 404 `client_index_off` above `CATALOG_CLIENT_INDEX_MAX` |
| GET | `/v1/admin/catalog/status` | support | Source, nightly hour, next sync, this instance's loaded copy, running and last run, valve |
| GET | `/v1/admin/catalog/runs?cursor=` | support | Sync log, newest first |
| POST | `/v1/admin/catalog/sync` | admin, 10/min | `{force?}` → 202 run; 409 `sync_running`, 409 `no_source`. Audited (`catalog.sync`) |
| GET | `/v1/admin/catalog/items?q=&status=all\|active\|retired&cursor=` | support | Stored items by name; `q` matches name, key or an alias |
| GET | `/v1/admin/catalog/items/{key}` | support | One stored item |
| POST | `/v1/admin/catalog/items/{key}/retire`, `/restore` | admin | Retire by hand (holds through syncs) or restore. Audited (`catalog.item.retire` / `.restore`) |

No realtime: clients refetch (the status polls every 3 s while a sync runs). Global search: kind `catalog.item`, link `/catalog/<key>`, active items only. Agent tools (read-only): `catalog_search`, `catalog_resolve` (≤ 100 names), `catalog_get_item`.

**Source format** (http and file): a JSON array or NDJSON of `{"key", "name", "aliases"?, "category"?, "subtitle"?, "image_url"? (https), "attrs"? (string, number or boolean values), "ref_price"? (string or number), "currency"? (ISO 4217, required with a price)}`; gzip and a byte order mark are fine. Keys are 1–200 letters, digits or `. _ : ~ -` (URL-safe, stable). Invalid items and repeated keys are skipped and counted; the run keeps the first 5 reasons.

## Screens

- **Web** `/catalog` (nav and Home tile "Catalog"): search (`q` in the URL), category chips (the 12 largest, a picker for the rest, `category` in the URL), rows with thumbnail or initials, subtitle (from 640 px), reference price and a Retired badge; browse with Load more when not searching; Manage for staff. `/catalog/[key]`: picture, name, category, subtitle, price ⓘ, Details (key, retired ⓘ, first seen, updated), Also known as, Attributes; ⋮ `ResourceMenu` (feature `catalog`, kind `catalog.item`) with Retire… / Restore… for admins. `/admin/catalog` (admin tab "Catalog"): **Sync** (running or refused banner with Sync anyway, status facts, Sync now, the log with counts and ⓘ reasons) and **Items** (look up by name, key or alias, status filter, Retire / Restore). Every change confirms in a `ConfirmDialog`. Skeleton, empty and error states throughout.
- **Mobile** Home → Catalog (`catalog/index`: search, category chips, list, pull to refresh, Load more) → `catalog/[key]` (the item page without admin actions). No admin on mobile.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `CATALOG_SOURCE` | — | Registered source; unset = `http` when `CATALOG_SOURCE_URL` is set, else `file` when `CATALOG_SOURCE_FILE` is set, else none (no syncs) |
| `CATALOG_SOURCE_URL`, `CATALOG_SOURCE_TOKEN` | — | The http source (through safehttp; https outside development) and its optional bearer token (secret) |
| `CATALOG_SOURCE_FILE` | — | The file source (`internal/catalog/devdata/elements.ndjson` to try syncs in development) |
| `CATALOG_SOURCE_MAX_MB`, `CATALOG_SOURCE_TIMEOUT` | `256`, `10m` | Largest http response; time a source gets |
| `CATALOG_SYNC_HOUR` | `3` | UTC hour of the nightly sync (caught up later that day if missed); `-1` = off |
| `CATALOG_MAX_RETIRE_PERCENT` | `40` | The safety valve |
| `CATALOG_MAX_ITEMS` | `500000` | Most items a source may send |
| `CATALOG_PUBLIC_READS` | `true` | Reads without sign-in |
| `CATALOG_RELOAD_INTERVAL` | `30s` | Version check per instance |
| `CATALOG_RESOLVE_PER_MINUTE` | `30` | Resolve calls per person (or IP) per minute |
| `CATALOG_CLIENT_INDEX_MAX` | `20000` | Largest catalog searched on the device; `0` = never |

## Compliance

- Stores no user data: no `AccountDeleter`/`AccountExporter`. Staff actions are in the audit log only (actor id there, never on catalog rows).
- No UGC: items come from the operator's source. Image URLs must be https (http only in development) and render as images only; source text is shown as text.
- The http source fetches through `safehttp` (no private or metadata addresses); the token lives in env, never in rows or logs.

## Stitching

1. Decide who reads the catalog: anyone (default) or signed-in people (`CATALOG_PUBLIC_READS=false`). Rename "Catalog" in `web/src/features/catalog/nav.ts` and `mobile/src/features/catalog/registry.tsx`.
2. Choose the source: `CATALOG_SOURCE_URL` for a JSON array or NDJSON feed (`CATALOG_SOURCE_TOKEN` if needed), or implement `catalog.Source` and call `catalog.RegisterSource("name", factory)` from an `init()` in `backend/internal/api/stitch_catalog.go`, then `CATALOG_SOURCE=name`. Map the outside fields to `Item` with a key that never changes.
3. Set `CATALOG_SYNC_HOUR` after the source's own daily refresh and `CATALOG_MAX_RETIRE_PERCENT` to the largest real drop; run Admin → Catalog → Sync now once and read the log.
4. Consumers (a feature that requires `catalog`, or stitching code that finds the `*catalog.Service` in `d.Modules`) call `Lookup(Query{Name})`, `Search(q, Filter{Category}, limit)` and `Item(key)`. Treat `Match == "fuzzy"` as a guess to confirm and `Item.Retired` as "no longer listed".
5. Image hosts you trust: add them to `next.config.ts` `images.remotePatterns` and `web/src/lib/images.ts` so web optimises them (others render unoptimised).
6. Replace the development elements (`backend/internal/catalog/devdata/elements.ndjson`: 50 chemical elements with symbols and Latin names as aliases, approximate reference prices for 5 metals), which `cmd/seed` loads into an empty catalog through a sync run.

## Extending

- Seams: `Source` (+ optional `SourceInfo`), `RegisterSource`, `ReportSkipped`, `DecodeItems` (reuse the JSON/NDJSON reader), `Service.SyncNow` (the app learned the source changed), `Lookup`, `Search`, `Item`, `Categories`, `Version`, `Loaded`. `GlobalSearch` is the `app.SearchProvider`, registered as a second module in `feature_catalog.go` because `Service.Search` is the Go API above.
- Matching rules live in `normalize.go` (`NameKey`) and `index.go`, mirrored on the device in `shared/search.ts`; change them together with `index_test.go`, `search_test.go` and `sync_test.go`, which pin them (ported from MOX). `catalog_db_test.go` runs the whole sync against `TEST_DATABASE_URL`.
- Don't write `catalog_items` outside a sync or the admin routes: every change must bump `catalog_state.version` in its transaction, or other instances keep serving the old index.

## Provenance

Distilled from MOX (`backend/internal/universe`: `contract.go` resolution rules, `normalize.go`, `index.go`, `sync.go`, `service.go`, `http.go`, their tests, migration `0013_universe.sql`, `web/components/decks/CardSearch.tsx`). Kept: the name key, the exact → alias → fuzzy (OSA ≤ 2, ≥ 6 characters, unique closest) → candidates order, the search ranking, the immutable index swapped atomically, a nightly sync that diffs and never deletes, renamed items keeping their old names, aliases only growing, the partial-pull guard (MOX refused a pull under 60% of the stored set; here a configurable retire share), the ETag'd gzip client index and the local-then-server search. Generalised: cards became items with category, subtitle, attributes and an optional reference price; Scryfall/tcgcsv became pluggable streaming sources (http through safehttp, file); faces, finish markers, digital-only names, printings, symbols and market listing were dropped. Fixed or added: the sync lock and log live in the database instead of a session advisory lock (visible, survives crashes), the valve also refuses empty or all-invalid pulls and staff can force it, staff retire/restore that syncs respect, a retired item no longer makes a name ambiguous, other instances reload on a stored version, substring scans and fuzzy masks so search and resolve hold up at 100,000+ items (benchmarked in memory: about 2 ms per fuzzy lookup, 7 µs per search).

Not verified: `next build`, `expo export`, Playwright and screen review at 375 / 1440 px (light, dark), mobile on a device, the http source against a real feed, and syncs above 100,000 items against Postgres.

## Changes

- 1.0.0: first version.
