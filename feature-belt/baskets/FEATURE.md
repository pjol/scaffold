# Baskets

## What it does

- People turn a list of the exchange's markets into one composite market: paste lines ("2 Gold", "Silver x3", "BTC-USD, 1", CSV with a header; quantity defaults to 1) or pick markets from search, and see it resolve and price live before listing it.
- Lines resolve by symbol, name or alias after normalisation (case, accents, punctuation, spacing); a name of 6+ characters within 2 edits of exactly one market resolves with a warning; unmatched or ambiguous lines offer up to 5 candidates. The app can resolve against a larger reference catalog instead (`baskets.SetResolver`).
- The preview shows each line, warnings in words, the basket's value (Σ quantity × oracle price), its largest line's share, blockers in words, the symbol and market spec it would get, and whether the same basket exists.
- One basket = one market, whoever lists it: lines merge by market, sort and hash, and the symbol comes from the hash (`BKT-7KQ2M9`). Listing writes the basket, an oracle basket feed and the exchange market in one transaction; listing the same lines again adds the person's entry, or reopens an archived basket.
- Idle baskets (no positions, no trading for 14 days) archive themselves: market halted, feed off, page kept. A basket with an end date winds down after it (reduce-only while held, then archived).
- Discovery (popular, new, mine; trading or archived), a page per basket (lines with weights at current prices, value, published price, market spec, entries, who listed it), reports, and Admin → Baskets (rename, hide, archive, handle reports; audited).

## Data

| Table | Purpose |
|---|---|
| `baskets` | One per distinct basket: symbol, hash and canonical string, name, description, creator, value and largest share at listing, optional `ends_on`, status (active / reduce_only / archived), `hidden`, entry and open-report counts, usage refreshed hourly (people's recent volume, open interest, holders, popularity) |
| `basket_lines` | Canonical lines: market symbol, the oracle feed that prices it, quantity, name and price at listing |
| `basket_entries` | Who registered a basket (listed or joined it), from where (web, ios, android, api, agent, seed); once per person |
| `basket_reports` | Reports of a name or description: reason, note, resolved_at |

Canonical form: merge by symbol, integer quantities, sort by symbol, `"basket-v1\n" + "SYMBOL\tQTY\n"…`, SHA-256. Symbol: `BASKETS_SYMBOL_PREFIX` + `-` + the first 6 Crockford base32 characters of the hash, 8/10/12 when another basket, market or oracle feed holds the shorter one (always within the exchange's `^[A-Z0-9][A-Z0-9-]{0,23}$`). The market's `price_feed` and the oracle basket feed's key are the symbol; the feed is unlisted (reachable by key, not on the Prices screen).

Market spec from value V and the largest share: tick = largest {1, 2, 5}×10^k ≤ V/10,000 (floor 0.001); 2 size decimals below 1,000, else 3; max leverage `BASKETS_MAX_LEVERAGE`, about half when one line is over half of V; minimum order `BASKETS_MIN_ORDER`; OI cap 100·V clamped to `BASKETS_OI_CAP_MIN`–`MAX`, 2 significant figures, halved when concentrated. Category `Baskets`; seed price = V (development only, as the exchange uses seeds).

## API

All under `/v1`, signed in. Lists are `{items, next_cursor}`, keyset on (created_at, id), popular on (popularity, created_at, id).

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST | `/baskets/preview` | user, 60/min | `{text?, picks?: [{symbol, quantity}]}` → lines, warnings, basket, value, max_share, blockers, creatable, symbol, spec, existing. No writes |
| GET | `/baskets/search?q&limit` | user | markets a basket may hold (the app's Resolver when set) |
| GET | `/baskets?sort=new\|popular&status=live\|archived\|all&q&cursor&limit` | user | discovery (hidden and flagged baskets left out) |
| GET | `/baskets/mine` | user | baskets the caller listed or joined, newest entry first |
| GET | `/baskets/{symbol}` | user | detail: lines at current prices with weights, value, the feed's published price, market status and spec, entries, creator, `entered`, `reported` |
| POST | `/baskets` | active, 20/h | `{text?, picks?, name?, description?, ends_on?}` → `{basket, created, revived, joined, warnings}`; 201 when listed |
| POST | `/baskets/{symbol}/report` | active, 10/h | `{reason: name\|description\|spam\|other, note?}` → 204 |
| GET | `/admin/baskets?filter=all\|reported\|hidden&status&q&cursor` | support | every basket with open reports and their reasons |
| PATCH | `/admin/baskets/{symbol}` | support | rename `{name?, description?}` (market and feed follow). Audited |
| POST | `/admin/baskets/{symbol}/hide` | support | `{hidden}`: hiding takes it out of discovery and winds it down; it never reopens. Audited |
| POST | `/admin/baskets/{symbol}/reports/resolve` | support | mark open reports handled. Audited |
| POST | `/admin/baskets/{symbol}/archive` | admin | wind down: reduce-only while held, else archived. Audited |

Error codes: `unresolved_lines`, `basket_too_small`, `basket_too_large`, `basket_unpriced`, `basket_hidden` (409), `baskets_rate_limited` (429, `details.retry_after_seconds`), `baskets_capacity` (503), `name_rejected`, `description_rejected`, `list_too_long`.

Realtime: `baskets:<basket id>` → `baskets.basket` (status, name, entries, visibility changed); any signed-in person may follow an existing basket. Prices come from the exchange's and oracle's own topics. Agent tools: `baskets_preview`, `baskets_list`, `baskets_get` (read); `baskets_create` (write: `Target` kind `baskets.basket`, `Preview` lists the lines, value and blockers, so Suggest shows it first). Lifecycle job `baskets.lifecycle`: hourly, once across instances (`jobs.Every`).

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/baskets` (nav, home tile) | Popular · New · Mine tabs (`?tab`), search and Trading/Archived (`?q`, `?status`), rows with top lines, entries, volume; New basket |
| Web | `/baskets/new` (`?from=SYMBOL` starts from a basket's lines) | paste box, market search with Add and picked rows with quantity steppers; live preview (lines with fixes: candidates replace a line, unmatched lines drop), warnings, blockers, one basket unit with weights, value, spec, symbol, the existing basket; name, description, end date; Create / Join / Reopen |
| Web | `/baskets/[symbol]` | name, price, status; banners for hidden, archived (Reopen), winding down; facts; lines linking to `/markets/[symbol]`; market spec; Trade; ⋮ `ResourceMenu` (Start a basket from this one, Report…) |
| Web | `/admin/baskets` (admin tab) | filter, search, table (status, entries, reports, creator), row menu: Rename…, Mark reports handled, Hide… / Show, Archive… (admins) |
| Mobile | Home → `/baskets`, `/baskets/new` (paste), `/baskets/[symbol]` | the same without admin and search-picking; report through the ⋮ menu's reasons; Trade opens `/markets/[symbol]` |

Every async view has skeleton, empty and error states.

## Config

| Variable | Default | |
|---|---|---|
| `BASKETS_SYMBOL_PREFIX` | `BKT` | 1–11 of A–Z, 0–9 |
| `BASKETS_MIN_VALUE` | `20` | smallest listable value, quote currency |
| `BASKETS_MIN_LINES`, `BASKETS_MAX_LINES` | `2`, `100` | different markets per basket |
| `BASKETS_MAX_LEVERAGE`, `BASKETS_MIN_ORDER` | `3`, `10` | new market spec |
| `BASKETS_OI_CAP_MIN`, `BASKETS_OI_CAP_MAX` | `5000`, `50000` | OI cap clamp |
| `BASKETS_PER_DAY`, `BASKETS_PER_MONTH` | `5`, `20` | new listings per person (joins don't count) |
| `BASKETS_CAPACITY` | `5000` | listed (non-archived) baskets at once; creating or reopening past it is refused |
| `BASKETS_IDLE_DAYS`, `BASKETS_END_GRACE_DAYS` | `14`, `30` | lifecycle |
| `BASKETS_REPORTS_TO_FLAG` | `3` | open reports that take a basket out of discovery until staff review |
| `BASKETS_BLOCKED_TERMS` | — | words names and descriptions may not use |

## Compliance

- Deletion: baskets the person listed stay listed (others hold positions) with the creator removed ("Deleted user"); their entries are deleted; their reports stay as anonymous moderation records. Export: baskets listed, entries, reports.
- Names and descriptions are user content. Before storing: charset, no links or @, reserved claims (official, verified, guaranteed, risk-free, airdrop), `BASKETS_BLOCKED_TERMS` with digit swaps undone, and the app's check (`SetTextCheck`). People report from a basket's page; staff rename or hide in Admin → Baskets (target under 24 hours). The moderation feature isn't required: a basket has no free-form posts, so a filter, reports and staff tools cover it without another dependency; with moderation installed, plug its filter in (Stitching 4).
- The creator's display name is shown to signed-in people on baskets they list (the create screen says so).
- Listing a basket creates a market; it isn't a trade. The exchange's compliance notes (real-money derivatives) apply to trading it.

## Stitching

1. Stitch the exchange to the oracle (exchange FEATURE.md → Stitching 2): basket markets get their index price from their oracle basket feed through it. Without it they trade only on development seed prices.
2. Decide what a line may name. Default: the exchange's markets. A larger reference catalog plugs in as the Resolver; with the `catalog` feature (an item's `Attrs["market"]` names its exchange symbol), add `backend/internal/api/stitch_baskets_catalog.go`:
   ```go
   func init() { RegisterModule(func(d *app.Deps) app.Module { return &basketsCatalog{d: d} }) }
   type basketsCatalog struct{ d *app.Deps }
   func (*basketsCatalog) Name() string { return "baskets_catalog" }
   func (b *basketsCatalog) Start(context.Context) {
   	for _, m := range b.d.Modules { if c, ok := m.(*catalog.Service); ok { baskets.SetResolver(catalogResolver{c}) } }
   }
   type catalogResolver struct{ c *catalog.Service }
   func cand(it catalog.Item) baskets.Candidate {
   	return baskets.Candidate{Symbol: it.Attrs["market"], Name: it.Name, Category: it.Category, ImageURL: it.ImageURL}
   }
   func (r catalogResolver) Resolve(_ context.Context, text string) (baskets.Resolution, error) {
   	res := r.c.Lookup(catalog.Query{Name: text})
   	if !res.OK || res.Item.Attrs["market"] == "" {
   		out := baskets.Resolution{Reason: baskets.ReasonUnknown}
   		if res.OK { out.Reason = baskets.ReasonNotListed } else if res.Reason == baskets.ReasonAmbiguous { out.Reason = res.Reason }
   		for _, it := range res.Candidates { if it.Attrs["market"] != "" { out.Candidates = append(out.Candidates, cand(it)) } }
   		return out, nil
   	}
   	return baskets.Resolution{OK: true, Match: res.Match, Market: cand(res.Item)}, nil
   }
   func (r catalogResolver) Search(_ context.Context, q string, limit int) ([]baskets.Candidate, error) {
   	var out []baskets.Candidate
   	for _, it := range r.c.Search(q, catalog.Filter{}, limit) { if it.Attrs["market"] != "" { out = append(out, cand(it)) } }
   	return out, nil
   }
   ```
   Exact market symbols and picked markets still resolve directly; whatever the Resolver returns must be a listed, non-halted, non-basket market whose price feed is an oracle single.
3. Set the limits for your audience and the spec knobs in the exchange's quote currency (Config).
4. Fill `BASKETS_BLOCKED_TERMS` (include the app's own name). With the moderation feature: `func init() { baskets.SetTextCheck(moderation.Check) }` in `backend/internal/api/stitch_baskets_moderation.go`. Staff the Admin → Baskets queue.
5. Basket markets show on the exchange's Markets screen under the Baskets category. Rename "Baskets" in `web/src/features/baskets/registry.ts` and `mobile/src/features/baskets/registry.tsx` if the product calls them something else.

## Extending

- Seams: `Resolver` + `SetResolver`, `SetTextCheck`, `Service.Preview`, `Service.Create` (the API's path, for app code that lists baskets), `Parse`, `Canonical`/`Hash`/`SymbolFor`.
- Pure logic is tested in `baskets_test.go` (parsing, canonical form, symbols, spec, resolution, text filter, limits, lifecycle transitions); the create/reuse/archive/revive/hidden path, limits and deletion in `baskets_db_test.go` (`TEST_DATABASE_URL`). Change behaviour together with them.
- A canonical basket never changes: keep `"basket-v1"` and the hash rules, or existing baskets stop matching their lines. Write markets through `exchange.UpsertCatalog`/`SetMarketStatus` and feeds through `oracle.EnsureBasketFeed`/`SetFeedEnabled`, never their tables.

## Provenance

Distilled from MOX's deck indexes: `internal/decks` (`basket.go` canonical form and symbols, `spec.go` market spec, `service.go` Preview/EnsureIndex/create/reuse/blockers, `lifecycle.go`, `store.go` limits, `names.go` text rules, `parse.go`, `resolve.go`, `indexes.go` discovery), `internal/deckindex` (contract and errors), `internal/universe` (name keys, OSA distance, lookup order), migration `0014_decks.sql`, web `components/decks` (ImportPanel, BasketPanel, CreateIndexDialog, IndexCard, DiscoveryView). Generalised: decks, cards, sideboards, basics, formats, events and MTG list grammars became markets and a generic line/CSV grammar; handles became display names; the universe became the exchange's catalog or an app `Resolver`; MOX's listing pipeline became `exchange.UpsertCatalog` + an oracle basket feed in one transaction; the event date became `ends_on` with one grace period; reports and moderation became a light report table plus staff tools (no moderation dependency); saved deck drafts were left out (the create screen starts from `?from=` instead). Fixed or changed: offset cursors became keyset; trending (scored per request from engine stats) became an hourly popularity column; the lifecycle runs once across instances through `jobs.Every` instead of an advisory lock per process; a basket line can't be another basket or an unpriced market; symbols also avoid existing markets and feeds; the denylist file became `BASKETS_BLOCKED_TERMS` plus `SetTextCheck`.

Verified: a generated project builds, vets and passes `go test ./internal/baskets/... ./internal/oracle/...` (database tests against a private Postgres 17), `pnpm typecheck`, `pnpm check` and `scaffold doctor`; on a backend-only dev-up the seed lists two baskets, and over the API preview, create, join, report, admin archive (market halted, feed off), reopen, hide (409 on re-create) and rename (market and feed follow) behave as described, with the basket feed publishing live from its parts. Not verified: `next build`, `expo export`, Playwright and device runs (screens unseen at 375/1440, light/dark), trading a basket market with the exchange stitched to the oracle, the lifecycle job against real positions (its rules are unit-tested), oracle load with thousands of basket feeds, and the `catalog` stitch above (written against that feature's announced API, not compiled).

## Changes

- 1.0.0: first version.
