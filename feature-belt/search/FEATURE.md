# Search

## What it does

- One search box finds anything the person can see across every installed feature (messages, posts, events, places, documents, records…): ⌘K / Ctrl+K anywhere on web, a top-bar button (from 640 px), a Home tile, a full results page, and a Search screen on mobile.
- Results are grouped by kind with icons, best match first and newest first among equals; the results page filters to one kind. Each row says where the hit lives ("Riverside Climbing Club · #general").
- With an org scope wired (stitching step 3), search covers the org the person is working in, with a toggle for every org; hits that belong to no org always show.
- Content from connected platforms (a person's Google Drive, GitHub, Notion… through the `connectors` feature) appears beside the app's own: each such hit carries a `SourceBadge` ("Google Drive · sam@acme.com"), and source chips filter by **All · <App> · each connected platform with hits**, with counts. The app's own hits carry no marker.
- If a feature fails or is slow, the rest still answer and the screen says which part is missing, with Retry.
- Recent searches stay on the device only (per account), and can be removed or cleared.
- Agents (the `mcp` and `assistant` features) get the same search as the read-only tool `search_everything`, run as the person with the same permission filtering.

## Data

None. Search reads each feature's own tables through its `app.SearchProvider`; nothing is indexed or stored here.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/search?q=&kinds=&limit=&org_id=&source=` | user, `SEARCH_RATE_LIMIT_PER_MINUTE` | Fans out to every `app.SearchProvider` in `Deps.Modules` concurrently, each with `SEARCH_PROVIDER_TIMEOUT`; merges by score, then recency; `{query, items, providers: [{name, status: ok\|timeout\|error, count, took_ms}], partial, sources: [{id: app\|provider:<id>, provider?, count}]}` |

- `q`: trimmed, whitespace collapsed, at most 200 characters; under 2 → 400 `query_too_short`. `kinds`: comma list of features (`chat`) or kinds (`chat.message`); providers whose module isn't named are not called. `limit`: default 20, at most 50 (also each provider's cap). `org_id` (optional uuid): providers see it as `app.SearchOrg(ctx)` and filter in SQL if they can; search then drops hits whose `org_id` names another org. Hits with no `org_id` (DMs, personal items, providers that don't set it) pass through. Scoping to an org you're not in returns only those.
- Hits that break the contract are dropped: kind not prefixed with the provider's module, missing id or title, a link that isn't an app path (`/…`, never `//` or a URL), or a malformed `source` (connected content is never shown as the app's own). Titles, snippets, `context` and the source's account/product are trimmed; a source `url` that isn't https is cleared; scores clamped to 0..1; duplicates (kind + id) removed.
- `source` (optional): `app` (hits without a source), `connected`, `provider:<id>` or a connection id; anything else → 400. `sources` counts every merged hit per source before this filter (and after `kinds`), so chips keep their counts while one is selected.
- A provider that ignores its context can't hold the request (it runs in its own goroutine); a panic is reported as `error`.

Agent tool: `search_everything {query, kinds?, limit?, org_id?, source?}` → `{query, items (with `context`, `source` on connected hits, and an absolute url when PUBLIC_WEB_URL is set), partial, unanswered}`. The description tells the model that hits with a source are third-party content from the person's connected account.

## Screens

- **Web** palette (registry `sessionMounts`, opened by ⌘K / Ctrl+K or the top-bar button from `headerActions`, shown from 640 px so the phone top bar keeps its room; phones use the `homeTiles` entry): combobox input, scope chips (the org / "All …", when a scope is registered; each open starts on the org), source chips (`SourceFilter`, shown once a connected platform has hits), results grouped by kind with icon, title, where it lives · snippet, a `SourceBadge` on connected hits (icon only below 640 px), and time (the org's name is left out while scoped to it); ↑/↓ move, Enter opens, ⌘/Ctrl+Enter opens in a new tab, Esc closes; recent searches when empty; skeleton, no-results (with "Search all …" while scoped), error with Retry and a partial notice; last row opens the results page. `/search?q=&kind=&scope=all&source=` (in the app shell): input, scope chips, source chips, kind chips, groups with counts, `SourceBadge` with the account on connected hits (the row's context is dropped when it only repeats the badge), the same states; query and filters live in the URL.
- **Mobile** `/search` (Home → Search): input, scope chips, source chips, `SourceBadge` on connected hits, recent searches (per-user storage, wiped on sign-out), grouped results with where each lives, skeleton/empty/error/partial states; a result opens its app path.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `SEARCH_PROVIDER_TIMEOUT` | `2s` | Time each feature gets (100ms–10s); slower ones are reported, not waited on |
| `SEARCH_RATE_LIMIT_PER_MINUTE` | `60` | Searches per user per minute |

npm: `@radix-ui/react-dialog` (web; already in ui-web's tree).

## Compliance

- Stores nothing on the server, so no deletion or export duties. Recent searches never leave the device; on web they are keyed by user id, on mobile they live in per-user storage cleared on sign-out.
- Search can only surface what each feature's provider returns for the caller; the feature is responsible for applying exactly its list routes' permissions (and the agent scope, below).

## Stitching

1. Implement `app.SearchProvider` in each feature with searchable content: the same permission checks as its list routes, kinds prefixed with the module (`docs.page`), links that are routes on web and mobile, a score from match quality (exact or prefix title matches high), `At` = last activity. Org-scoped content sets `OrgID` and `Context` (`app.SearchContext(orgName, container)`) and filters to `app.SearchOrg(ctx)` when set.
2. Kind and feature names and icons for every belt feature ship here (`kinds.ts`, `icons.tsx`, `icons.ts`); unknown kinds read "Companies"-style plurals of their last segment. Rename or add with `registerSearchKindLabel` / `registerSearchProviderLabel` (shared) and `registerSearchIcon` (`@/features/search/icons` on web and mobile) at module load.
3. If the app has orgs, wire the scope once in stitching (web and mobile): `registerSearchScope(() => { const { org } = useActiveOrg(); return org ? { id: org.id, name: org.name, allLabel: \`All ${orgsNoun("many")}\` } : null; })`. Without it, search covers every org unlabelled by scope (rows still show where hits live).
4. Decide placement: the palette and top-bar button are on every signed-in web screen; on mobile search is a Home row (make it a tab if search is a primary destination).
5. Set `SEARCH_PROVIDER_TIMEOUT` to the slowest provider you accept; providers that search connected platforms should time-box each connection inside it.
6. Connected sources: providers that search a person's connected platforms (through `app.FindConnections(d.Modules)`) set `Source: app.ConnectedSource(conn, product, url)` and `Context` = "<Platform product> · <account>"; nothing to wire here.

## Extending

- **Agent scope**: when an agent searches (`search_everything` from MCP or the assistant), the context carries `auth.Agent` and is passed to providers unchanged. Search skips a module whose feature the person's agent scope has entirely off (`AgentPolicy.FeatureLevel` max below read, or a failed lookup). Items off inside a readable feature are the provider's to drop: access-backed features filter in SQL; others drop rows with `app.AgentAllows(ctx, d.Modules, caller, "<feature>", "<kind>", id, app.AgentRead)`. A provider that ignores it leaks rows the person hid from agents.
- `search.Service.Run(ctx, caller, search.Query{Text, Kinds, Limit, OrgID})` is the fan-out other server code can call; `search.NormalizeQuery` is the query rule.
- **Org scope**: a provider that can filter by org reads `app.SearchOrg(ctx)` (files, docs and access teams do); one that can't returns everything and search drops hits whose `OrgID` names another org, so fill `OrgID` either way. Hits without `OrgID` always pass. `Context` starts with the org's name when `OrgID` is set: clients strip that part while scoped to the org (`searchResultContext`).
- **Connected sources**: `app.SearchResult.Source` (template, `*app.SourceRef`) marks a hit read live from a person's platform account; search validates it, counts it in `sources` and filters by `source`. Providers check the connection's agent scope too (`app.AgentAllows(…, app.ConnectionScope(id), …)`) for agent searches.
- Out of scope: a shared index (ranking across features is by each provider's score), typo tolerance beyond what providers do, server-side search history.

## Provenance

New for the scaffold (no source project had cross-feature search). The provider contract (`app.SearchProvider`, `app.SearchResult`) is core; the fan-out, partial reporting and palette follow the patterns of the command palettes in Linear, Slack and Notion: grouped results, keyboard-first, honest about missing sources.

## Changes

- 1.2.0: connected sources: `source` on hits (template `app.SourceRef`) validated (malformed → dropped, non-https URL cleared), `sources` facets and the `source` filter on `/v1/search` and `search_everything`; source chips (`SourceFilter`) and `SourceBadge` on the palette, results page (`source=` in the URL) and mobile.
- 1.1.1: the `search_everything` description and the `org_id` error use neutral wording (search doesn't require orgs, so it can't read its noun).
- 1.1.0: org scope: `org_id` on `/v1/search` and `search_everything` (`app.SearchOrg` for providers, other orgs' hits dropped, org-less hits kept), `registerSearchScope` seam with scope chips on the palette, results page (`scope=all`) and mobile; rows show `SearchResult.context` (template field), minus the org while scoped; names for every belt kind and feature (no more "Repositorys") and a proper plural fallback; icons for every name belt providers send (git-branch, file-code, handshake, user-round, table, lock…) and `registerSearchIcon` on web and mobile.

- 1.0.1: agent searches skip every module whose feature the person's agent scope has off (fail closed if the scope can't be read), so a provider without its own feature-level check can't leak it (access's teams did).
- 1.0.0: global search API with concurrent fan-out, per-provider timeouts and partial results; `search_everything` agent tool; web ⌘K palette and results page; mobile search screen.
