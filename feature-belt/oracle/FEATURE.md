# Price oracle

## What it does

- Publishes one price per **feed** (a single, priced from its venues, or a **basket** = Σ quantity × part price) with a status: `live`, `stale` (no accepted print within the staleness window: stop relying on it), `seed` (no source has set a price yet) or `off`.
- Pulls prints from **sources**: pluggable pollers behind a scheduler with per-source budgets (spacing, cadence, quota), a lane per API host, per-row backoff and 429 `Retry-After` handling. The `http` source polls any public JSON endpoint; `manual` takes prices staff type in; app code pushes its own (`Service.Push`).
- **Screens** every print before it counts (stat-v1): distance from the recent median in MAD units, from the published price and from the other venues, same-direction bursts and walk-ups, oversized lots, future or old timestamps, listings. Score < 0.35 accepted, 0.35–0.70 downweighted (weight × (1 − score)), > 0.70 rejected; each observation keeps its score, weight and reasons. A venue whose prices run above or below the others is normalised first by its premium, fixed or learned. An optional **decision model** gives a second opinion on live prints; it can only raise risk and fails open.
- **Aggregates** a recency-weighted median (half-life per feed; no sale above 12% of the weight; aggregates count as several sales) and **publishes** every 3 s, moving toward the target at most `max_move_bps_per_min`, so one forced print can only nudge the price.
- Keeps **daily history** (its own closes, plus backfill from a source or a staff import, screened and checked against the live level) and the last day of published prices.
- People see **Prices** on web and mobile: every feed live, a chart (1D/30D/90D/1Y), venues with health and premium, basket parts, every screened print with its verdict and reasons. Staff get **Admin → Oracle**: feeds (create), source health with budgets and lanes, the decision model, the live observation stream; admins post prices and turn feeds off from a feed's ⋮.

## Data

| Table | Purpose |
|---|---|
| `oracle_feeds` | Key, name, category, quote, decimals, kind, optional seed, params (half-life, stale-after, max move, lookback, reference and burst windows), listed, enabled |
| `oracle_feed_sources` | A single's venues: registered source, venue label (unique per feed), ref (URL, product id), params, weight, fixed premium |
| `oracle_basket_parts` | Basket → single feeds with quantities |
| `oracle_observations` | Every screened print: raw and normalised price, premium, qty, times, verdict, score, reasons, model, weight, staff actor; dedupe key unique per feed |
| `oracle_inbox` | Pushed prints waiting for the pipeline (staff, app pushes, dev seeds), claimed with `SKIP LOCKED` once the leader has loaded their feed |
| `oracle_prices` | Published prices every 30 s on change, at least every 5 min |
| `oracle_feed_state` | Latest published price, target, status, last accepted print: what every instance reads and where a new leader resumes |
| `oracle_venue_premiums` | Learned premiums: recent log price ratios per (feed, venue) |
| `oracle_source_health`, `oracle_ref_health` | Per lane (API host): rate limited or not. Per source row: last success, last error, consecutive failures |
| `oracle_fetch_log` | Last fetch per (source, task), so a restart resumes cadences instead of re-bursting APIs |
| `oracle_history` | One close per feed per UTC day: `oracle` (its own) or the backfill's source; own days are never overwritten |
| `oracle_leader` | Lease naming the instance that runs the pipeline (renewed every 5 s, 20 s long) |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/oracle/feeds?q=&kind=&cursor=&limit=` | public (`ORACLE_PUBLIC_READS`) or user | Listed, enabled feeds by key with price, target, status, venues |
| GET | `/v1/oracle/feeds/{key}` | same | Any feed by key: params, venues (last price, health, premium), parts, last 24 h, latest 50 observations (a basket's include its parts') |
| GET | `/v1/oracle/feeds/{key}/history?days=` | same | Daily bars (≤ 365), oldest first; backfill smoothed and eased onto the first own close |
| GET | `/v1/oracle/observations?feed=&verdict=&cursor=&limit=` | same | Screened prints, newest first (keyset on received time) |
| GET | `/v1/admin/oracle/feeds`, `/feeds/{key}` | support | Every feed; detail plus `source_config` and `part_config` |
| GET | `/v1/admin/oracle/sources` | support | Sources (poll/push, budget, failing rows, lanes), decision model, leader |
| GET | `/v1/admin/oracle/observations` | support | Stream across every feed |
| POST | `/v1/admin/oracle/feeds` | admin | Create a single (with sources) or basket (with parts); profile `daily`, `intraday`, `realtime` or explicit params. Audited |
| PATCH | `/v1/admin/oracle/feeds/{key}` | admin | Name, category, quote, decimals, seed, params, listed, enabled. Audited |
| PUT | `/v1/admin/oracle/feeds/{key}/sources`, `/parts` | admin | Replace a single's sources (each validated by its source; URLs through safehttp) or a basket's parts. Audited |
| POST | `/v1/admin/oracle/feeds/{key}/prints` | admin, `ORACLE_PRINTS_PER_MINUTE` | Staff print → 202, screened like any other; needs a `manual` source (409 `no_manual_source`); `observed_at` (default now) within the feed's lookback (400 otherwise). Audited |
| PUT | `/v1/admin/oracle/feeds/{key}/history` | admin | Import daily closes for a single. Audited |

Realtime (any signed-in user): `oracle:<feed id>` → `oracle.price`, `oracle.observation`, `oracle.history`; `oracle:feeds` → `oracle.prices` (the feeds that changed this tick), `oracle.feeds` (catalog changed); `oracle:observations` → every `oracle.observation`. Agent tools (read-only): `oracle_list_feeds`, `oracle_get_feed`, `oracle_list_observations`, `oracle_get_history`.

**Pipeline.** One instance holds the lease and runs sources, screening, publishing and the batched writer; every instance serves reads from the database (state is written each tick it changes). Staff prints and pushes go through `oracle_inbox`, so they work on any instance; the leader claims a row only once it has loaded the row's feed (an unknown feed triggers a reload), so a print sent right after the feed was created waits instead of being lost. `Service.Push` rejects prints the pipeline would drop (outside the lookback, or in the future). A task's first poll after taking the lease replays the past and never goes to the decision model.

## Screens

- **Web** `/oracle` (nav and Home tile "Prices"): search, kind filter, table (feed, key, price, status, venues, updated), live; staff see Manage. `/oracle/[key]`: price, status ⓘ, target, max move, staleness, updated; chart with 1D/30D/90D/1Y in the URL; Venues or Parts `ListGroup`; Observations (table from 768 px, rows below) with a verdict filter; ⋮ `ResourceMenu` (feature `oracle`, kind `oracle.feed`) with Post a price… and Turn off… for admins. `/admin/oracle` (admin tab "Oracle"): Feeds (search, New feed), Sources (health, rows, budget, last success, lanes ⓘ; decision model; where the pipeline runs), Observations (verdict filter, live). Skeleton, empty and error states throughout.
- **Mobile** Home → Prices (`oracle/index`: search, list, pull to refresh) → `oracle/[key]` (price, status, chart, venues or parts, latest 30 observations with ⓘ reasons). No admin on mobile.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `ORACLE_PUBLISH_INTERVAL` | `3s` | Recompute, slew and publish (500 ms–1 m) |
| `ORACLE_SOURCES_ENABLED` | `true` | Poll sources; `false` for tests and offline machines (pushes still work) |
| `ORACLE_DISABLE_SOURCES` | — | Source names not to poll |
| `ORACLE_LEARN_PREMIUMS` | `true` | Learn venue premiums; a fixed premium always wins |
| `ORACLE_PUBLIC_READS` | `true` | Reads without sign-in (`false`: user routes) |
| `ORACLE_PRICE_RETENTION`, `ORACLE_OBSERVATION_RETENTION` | `720h`, `4320h` | Pruned every 6 h (`jobs.Every`); daily history is kept |
| `ORACLE_PRINTS_PER_MINUTE` | `30` | Staff prints per admin per minute |
| `ORACLE_HTTP_MIN_GAP`, `ORACLE_HTTP_CADENCE` | `1s`, `60s` | `http` source: spacing per API host; default poll interval |
| `ORACLE_DECISION_URL`, `ORACLE_DECISION_API_KEY`, `ORACLE_DECISION_MODEL` | — | Decision model speaking the decisions format (a state of short strings and yes/no questions answered with `noul` probabilities, as OpenRouter's `/api/alpha/decisions` serves for TypeSafe's Jev). Unset = stat-v1 only |
| `ORACLE_DECISION_MAX_RPS`, `ORACLE_DECISION_TIMEOUT` | `20`, `2s` | Its rate and per-call limit; 401/402/403 turn it off until restart, 429 pauses it |

## Compliance

- Personal data is only who created a feed and who posted a staff print: deletion clears both (prices other people rely on stay); export lists them. Staff writes are audited. No UGC.
- The decision model sees the feed's name and category, the venue label and the screen's features in words; never text a source supplied.
- The `http` source fetches through `safehttp` (no private or metadata addresses; https only outside development). Credentials never live in source rows.

## Stitching

1. Decide who reads prices: anyone (default) or signed-in people (`ORACLE_PUBLIC_READS=false`). Rename "Prices" in `web/src/features/oracle/nav.ts` and `mobile/src/features/oracle/registry.tsx` if the product calls them something else.
2. Create the feeds the product needs (Admin → Oracle → New feed, or the API) with a pace profile each; set a seed only when you trust it within ±30%.
3. Sources beyond JSON-HTTP: implement `oracle.Source` (and `HistorySource` for backfill) in your own package and call `oracle.RegisterSource("name", factory)` from an `init()`; give each API host its own `Task.Lane` and an honest `Budget` (published limits, with margin). Push-only sources call `oracle.RegisterPushSource` and `Service.Push`, with a `Print.ID` stable per feed, source and venue (a trade id) so retries are no-ops.
4. Where one market is the truth, give its venue a fixed premium of 0: learned premiums then measure the other venues against it (with no anchor they measure against the venues' median).
5. Consumers (an exchange, alerts) read `Service.Current(ctx, key)` (or `CurrentMany` for several keys) or follow `oracle.price`; on the leading instance `Service.OnPublish` gets every tick. Treat `stale` and `seed` as "don't act on this".
6. Optionally set `ORACLE_DECISION_*`, then watch Admin → Oracle → Sources for its calls, failures and cost.
7. Replace the dev seeds in `backend/internal/oracle/seed.go` (BTC-USD polls three public exchange APIs; GOLD-OZ, SILVER-OZ and METALS-BASKET take seeded staff prints).

## Extending

- Seams: `Source`, `HistorySource`, `RegisterSource`, `RegisterPushSource`, `Service.Push`, `UseDecisionModel`, `Service.OnPublish`, `Service.Current`, `Service.CurrentMany` (`PricePoint.Kind` says single or basket); pace presets in `Profiles`.
- Feeds from code, inside the caller's transaction (`ensure.go`, the baskets feature's path): `Service.EnsureBasketFeed(ctx, q, BasketFeed{Key, Name, Category, Quote, Decimals, Listed, Parts})` creates a basket feed or brings its name, listing and parts in line (no write when nothing changed; refuses a single's key with `ErrNotBasket`, and parts that are unknown or baskets); `Service.SetFeedEnabled(ctx, q, key, on)` turns any feed on or off (`ErrUnknownFeed`). Neither writes an audit row: audit staff actions yourself. The leader loads the change within 5 s of commit; call `Service.Reload()` after commit to make it sooner.
- Screening thresholds and coefficients live in `params.go` and `screen.go` (`stat-v1`); change them together with `oracle_test.go`, which pins the behaviour (outliers, walk-ups, slow trends, premiums, the decision floor).
- Don't publish from anywhere but the leader, and don't write observations directly: everything goes through `ingest`, so screening, dedupe and weights apply.

## Provenance

Distilled from MOX (`backend/internal/oracle`: `screen.go`, `params.go`, `oracle.go`, `history.go`, `sources.go`, `jev.go`) without its collectible sources and catalog. Generalised: feeds and sources live in the database and reload live; venues replace hard-coded source names; per-category params became per-feed params with profiles; the Jev client became a format-level decision model behind an interface. Fixed or added on the way: a broken source row no longer backs off every row of its source (per-task backoff, a lane per API host, per-row health); venue premium normalisation (fixed, or learned from raw prints against anchors, so it can't feed back on itself); a single-leader pipeline with database-backed reads, so more than one API instance works; pushed prints through an inbox; feeds without a seed; history backfill through any source, level-checked against the live price.

## Changes

- 1.0.0: first version.
- 1.1.0: basket feeds and feed switches from code (`EnsureBasketFeed`, `SetFeedEnabled`, `Reload`), `CurrentMany` and `PricePoint.Kind`; `Current` now reads a feed the leader hasn't published yet from the database (status `seed`, or `off` when disabled) instead of reporting it unknown. Tests: `ensure_test.go`.
