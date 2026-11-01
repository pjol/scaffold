# Docs

## What it does

- Notion-style pages inside an org: a tree of nested pages (icon, cover, full width, lock) made of blocks — text, headings 1–3, bulleted / numbered / to-do / toggle lists, quote, callout, code, divider, image, file, bookmark, sub-page link, simple table and inline database. Sidebar with Favorites, Shared and Private; drag to reorder and nest; recent pages; duplicate, move, trash with restore; page links and backlinks; Markdown import and export.
- A web editor with one contenteditable per text block over a span model (bold, italic, underline, strike, code, links, mentions of people, pages and dates): Enter splits, Backspace merges, arrows cross blocks, Tab nests, `/` menu, Markdown shortcuts (`#`, `-`, `1.`, `[]`, `>`, ```` ``` ````, `---`, `**x**`), a formatting toolbar, `@` mentions, drag handles, block selection (Escape, shift-click, ⌘A) with delete, move, turn into, copy and paste (text, Markdown, HTML, sanitised), undo and redo, autosave with an honest saved / offline state.
- Live collaboration: the server applies block operations and broadcasts them; people viewing the page show in the header and on the block they're typing in; edits to the same block merge (three-way, server-side); clients catch up after a reconnect by page version.
- Databases: a page with properties (title, text, number, select, multi-select, status, date, person, checkbox, URL, email) whose rows are pages; table (resize and reorder columns, inline edit), board (drag between columns), list and calendar views, each with saved filters (all must match), sorts and grouping. Inline database blocks.
- Comments on the page, a block or a text range: threads, resolve / reopen, `@` mentions (notified). History: versions saved while editing, preview and restore. Publish to the web as a read-only public link (`/p/<token>`), reportable through moderation.
- Agents acting for a person read and change pages with exactly that person's access, capped by their AI agent scope; writes are single reviewable changes.

## Permission model

Every page (databases and rows too) is the access resource `docs.page` whose access parent is its parent page: sharing inherits down the tree and a subpage can be restricted. Levels come from `access_level()`.

| Who | Level |
|---|---|
| The page's owner (its creator) | manage |
| People, teams, the whole org (Share dialog), on the page or a parent it inherits from | the granted level |
| Org owners and admins | manage, only on pages whose owner was deleted or left the org |
| Anyone with a public link | view: the page and subpages that inherit from it |

**View**: read, export, history, backlinks, presence. **Comment**: + comment, resolve. **Edit**: + blocks, title, icon, cover, lock, move inside pages you can edit, duplicate, trash and restore, rows, properties, views, uploads, restore a version. **Manage**: + sharing and public links, move to the top level, delete from the trash for good. New top-level pages are private unless made for everyone. Org permissions: `docs.create_shared` (top-level pages everyone can edit; all roles by default) and `docs.publish` (all roles by default; the page kind's access `LinkPermission`: access requires it, with `access.share_public`, to make a link and checks both live for each link's creator, so turning either off stops published pages and the Share dialog says which is missing). Agents get at most edit, capped by `access_agent_cap`; there are no sharing, publishing or deleting tools.

## Data

| Table | Purpose |
|---|---|
| `docs_pages` | pages, databases (`kind`, `schema`) and rows (`is_row`, `props`); `position` (fractional, `COLLATE "C"`), `version`, `locked`, `cover`, trash (`deleted_at`, `trash_root_id`), `public_blocked_at` |
| `docs_blocks` | one row per block: type, `props` (spans…), parent block, position, `plain` (search), `version` |
| `docs_block_tombstones` | deleted block ids by version, for `sync` (a week; older clients reload via `tomb_floor`) |
| `docs_links` | page mentions and sub-page blocks (backlinks) |
| `docs_files` | uploads per page (private purpose `docs_file`) |
| `docs_views` | views: type, position, config `{filters, sorts, group_by, columns, card_properties, date_property}` |
| `docs_comments` | threads and replies; block, quote and range anchors; resolve; soft delete; moderation hide |
| `docs_snapshots` | saved versions (title, icon, blocks, editors) |
| `docs_favorites`, `docs_visits`, `docs_presence` (unlogged) | favorites, recent pages, who has a page open |

## API

Under `/v1/docs`, signed in unless noted. Every id goes through `access.Require` (404 without view, 403 below the level).

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/orgs/{orgID}/sidebar` · `/recent` · `/trash` · `/search?q=&kind=&rows=&min_level=` | member | tree roots by section · recent · trashed (edit) · title + text search (no q: recent) |
| POST | `/orgs/{orgID}/pages` · `/orgs/{orgID}/import` | member, 300/h · 30/h | `{parent_id?, kind, title, icon, shared, after_id, inline}` · `{markdown, title?, parent_id?}` |
| GET | `/pages/{id}` · `/children` · `/sync?since=` · `/backlinks` · `/export` | view | detail (blocks, refs, path, views, row's database) · subpages · changes since a version · links here · Markdown |
| POST | `/pages/{id}/ops` | edit, unlocked, 900/min | `{client_id, ops:[create\|update\|move\|delete]}` → `{version, blocks, deleted, rejected}` |
| PATCH · POST | `/pages/{id}` · `/move` · `/duplicate` | edit (top level: manage) | title, icon, cover, width, lock · `{parent_id, after_id\|last}` · copy with subpages |
| DELETE · POST · DELETE | `/pages/{id}` · `/restore` · `/permanent` | edit · edit · manage | trash · restore · delete for good |
| PUT / DELETE | `/pages/{id}/favorite` | view | |
| POST · GET | `/pages/{id}/files` · `/files/{fileID}` | edit · view | attach an upload · 1-hour download link |
| GET · POST · DELETE | `/pages/{id}/presence[/{session}]` | view, 240/min | viewers · heartbeat `{session_id, block_id}` · leave |
| GET · POST | `/pages/{id}/comments` | view · comment, 60/min | `?status=open\|resolved\|all` · `{body, thread_id? \| block_id, anchor_start, anchor_end}` |
| PATCH, DELETE · POST, DELETE | `/comments/{id}` · `/comments/{id}/resolve` | author (delete: or manage) · comment | edit, delete · resolve, reopen |
| GET · GET · POST | `/pages/{id}/history` · `/history/{id}` · `/history/{id}/restore` | view · view · edit | the current state is saved first |
| GET · POST | `/databases/{id}/rows?view=&filters=&sorts=&q=&cursor=&limit=` | view · edit | keyset rows · new row |
| PATCH | `/pages/{rowID}/properties` | edit | `{values?, title?, after_id\|first\|position}` |
| PUT · POST, PATCH, DELETE | `/databases/{id}/schema` · `/databases/{id}/views`, `/views/{id}` | edit | properties (values migrate) · views |
| GET | `/public/{token}[/pages/{id}]` · `/files/{id}` · `/databases/{id}/rows` | public, 120/min/IP | read-only, no author ids; refuses agent credentials |

Realtime `docs:<page id>` (viewers): `docs.ops`, `docs.page`, `docs.presence`, `docs.comments`, `docs.reload`, `docs.rows`, `docs.schema`; `docs:<org id>` (members): `docs.tree`. Lost access drops subscriptions (`access.OnChange`). Notifications `docs.mention` (pages, comments) and `docs.comment` (replies; new threads on your page), never to someone who can't open the page or blocks the sender. Moderation targets `docs.page` (removal stops the public copy) and `docs.comment` (hidden). `app.SearchProvider`: `docs.page` (`Context` = org · parent page when you can open it; scoped by `app.SearchOrg`).

Agent tools, run as the caller with `access.ViaAgent` in every query: `docs_search_pages`, `docs_get_page` (Markdown with `<!-- block:<id> -->`), `docs_list_children`, `docs_query_database` (properties and options by name). Writes set `Target` (`docs.page`) and an exact `Preview`: `docs_create_page`, `docs_update_block` (before/after text; `expected_text` refuses stale edits), `docs_insert_blocks`, `docs_set_property`, `docs_add_comment`.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Workspace | `/docs`: tree beside Recent at ≥ 1024 px; the tree alone below. Search (⇧⌘P), import, trash, new page (private or for everyone) | `docs` (home entry): Recent, Favorites, Shared, Private, expandable; new page sheet |
| Page | `/docs/[id]`: breadcrumbs, presence, save state, comments, favorite, ⋮ (copy link, move, duplicate, lock, width, publish, history, export, import here, trash; Share… from access); cover, icon, title, row properties, editor or database, pages inside, backlinks; trash banner | `docs/[id]`: native rendering of every block type; tap a text block to edit it as plain text (marks outside the change are kept; a newline at the end starts a block; Backspace on an empty block removes it; `- `, `# `, `[] ` shortcuts); to-do and toggle taps; long press: add below, move, turn into, delete; title; row properties; ⋮ sheet |
| Database | view tabs, filters and sorts (saved by editors, local for others), search, new row; table, board, list, calendar | view chips; table and board with property edits; long press moves a card; list and calendar as lists |
| Comments | side panel ≥ 1280 px, sheet below; quoted text highlighted | `docs/[id]/comments`: threads, reply, resolve, reopen, new (plain text) |
| Public | `/p/<token>[/<page id>]`: read-only, subpages, report (signed in) | `p/[token]?page=` |

## Config

| Variable | Default | Meaning |
|---|---|---|
| `DOCS_FILE_MAX_MB` | 25 | largest image or file |
| `DOCS_TRASH_DAYS` | 30 | days before the trash is emptied |
| `DOCS_HISTORY_DAYS` | 90 | days versions are kept (the newest always) |
| `DOCS_SNAPSHOT_INTERVAL` | 10m | version cadence while someone keeps editing (≥ 1m); idle pages after 2 minutes |

No new npm or Go dependencies.

## Compliance

- Deletion: private pages (top level, owned, nothing in the tree shared) go; their comments become "Comment deleted"; favorites, visits, presence and uploads go; names leave attribution columns. Shared pages stay with the org (admins manage them).
- Export: pages they created (with Markdown), comments, favorites.
- UGC: comments pass `moderation.Check`, can be reported and hidden; blocked people's comments are hidden both ways and never notify. Published pages are public: reportable, and staff removal stops the public copy. Page text is internal collaboration and isn't filtered keystroke by keystroke.

## Stitching

1. Place `docsNav` (and promote mobile `docsHomeEntry` to a tab) where docs belongs.
2. Choose defaults for `docs.create_shared` and `docs.publish` per role.
3. Install `agents` (per-page AI scopes) and `search` (global search); both plug into the seams above.
4. Say in the privacy policy that published pages are readable by anyone with the link.

## Extending

- A block type: app migration for the type check, `normalizeProps`, Markdown both ways, web `BlockBody` and `BlockView`, mobile `Blocks`.
- `DocsPageSession` (shared) drives any editor; keep `docsMergeText` (TS) and `mergeSpans` (Go) identical.
- Out of scope: relations and rollups, formulas, text colours, synced blocks, templates, embeds that fetch URLs, cursors inside a block, mobile rich formatting and comment mentions, comment edit/delete on mobile.

## Provenance

New in the forum belt: Notion's block model, page tree and databases on the `access` sharing model; positions follow rocicorp's fractional-indexing.

## Changes

- 1.0.4: registering an upload checks the object exists and records its stored size (a key never uploaded, or a size the client misstated, no longer makes a broken file). File links carry the file's name and type (`inline; filename=…`), so downloads save under it. Errors, the shared-pages permission and agent tools name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
- 1.0.3: copy names organizations with orgs' noun (`orgsNoun`…, orgs ≥ 1.2.0). Search results carry `OrgID` and `Context` (org · parent page) and honour the `org_id` scope (search ≥ 1.1.0). Mention and comment notifications run under `bg.Go`.

- 1.0.2: the dev seed shares through `access.AddGrant` (no direct writes to access tables; a grantee the share dialog would refuse is skipped); the public routes refuse agent credentials; the `docs_create_page` preview shows the title and icon exactly as stored, and the `docs_add_comment` preview names the block it attaches to. Mention and comment notifications name their page (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for a page its scope hides.
- 1.0.1: `docs.publish` is the page kind's access `LinkPermission` (access ≥ 1.2.0): the Share dialog no longer makes links that can't open for people without it, and docs' own resolve-time check is gone (access does it). Behaviour is otherwise unchanged.
- 1.0.0: first release.
