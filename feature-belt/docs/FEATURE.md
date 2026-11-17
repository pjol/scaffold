# Docs

## What it does

- Notion-style pages inside an org: a tree of nested pages made of blocks. Sidebar with Recent, Favorites, Teamspaces (teams you're on, with the pages shared with them; hide per person), Shared, Private and Connected, Templates · Import · Trash in the footer; drag to reorder and nest; duplicate (only the subpages you can open), move, trash with filters and restore; page links, hover previews and backlinks; quick find (⌘P) by title and text with filters.
- Blocks: text, headings 1–4 (1–3 toggleable), bulleted / numbered / to-do / toggle lists, quote, callout, divider, code (language, highlighting, wrap, caption; `mermaid` draws the diagram), equation (TeX via KaTeX), simple table (header row/column, rows and columns anywhere), columns (2–5, resizable, stacked under 640 px), synced block (an original, or a copy that shows and edits it live), table of contents, breadcrumb, button (insert blocks, open a link, add a row), sub-page, link to page, inline database, image (width, alignment, alt, caption, zoom), video, audio, PDF, file (core `FilePreview` card), bookmark (title, description, icon and image fetched by the server), embed (allowlisted providers in a sandboxed frame), connected page. Any block takes one of Notion's nine colours as text or background. Rich text: bold, italic, underline, strike, code, links, colour and highlight, inline equations, `:` emoji, mentions of people, pages and dates (with a time; a date can remind whoever set it).
- Web editor: one contenteditable per text block over a span model; `/` menu with every block, view, embed and action (grouped, fuzzy with aliases like `/h1` `/todo` `/db`, recent first); Markdown shortcuts (`#`–`####`, `-` `*` `+`, `1.` `a.` `i.`, `[]`, `"` quote, `>` toggle, ```` ``` ````, `---`, `$$`, `**x**` `*x*` `~~x~~` `` `x` `` `$x$`, `[[` / `+` link or create a page, `@`, `:`); Notion's keys (⌘⌥0–9 turn into, ⌘D, ⌘⇧↑↓, ⌘/, ⌘↵, ⌘B/I/U/E/K, ⌘⇧S/E/H/M, Tab/⇧Tab, Esc) and a shortcuts sheet (⌘?); block menu ⋮⋮ (turn into, colour, duplicate, move, move to page, copy link to block `#block-<id>`, comment, replace file, sync / unsync, delete); files dropped, pasted or picked become the right block; a pasted link offers mention, bookmark or embed; block selection, copy and paste (text, Markdown, HTML, sanitised), undo/redo, autosave with an honest saved / offline state.
- Live collaboration: the server applies block operations and broadcasts them; viewers show in the header and on the block they're typing in; edits to one block merge three-way server-side; clients catch up after a reconnect by page version.
- Page chrome: emoji, an icon from the set (90 lucide icons tinted with a palette role) or an uploaded image; cover from a 12-gradient gallery (palette tokens, so it follows theme), a colour or an upload with its position; font (default, serif, mono), small text, full width, lock; **Verified** (until changed or for 7/30/90 days); Page info (words, reading time, created / edited by; for editors, views per day and who viewed); a floating outline beside wide pages.
- Templates: New page offers Empty, Templates (18 built-in: meeting notes, 1:1, retro, project brief, roadmap, task and bug trackers, RFC, design spec, wiki, onboarding, OKRs, decision log, PRD, content calendar, reading list, journal…) and Import; Save as template keeps a workspace template (private or for everyone; out of the tree, recents and search). Imports run in the background with progress and an error report: Markdown, text, HTML (Confluence exports too), Word .docx, CSV/TSV as a database, zips (Notion/Markdown exports keep the tree, databases, files and links). Exports: Markdown, Markdown + CSV zip, HTML, HTML zip, Word .docx, print to PDF, optionally with the subpages the exporter can open; links last an hour.
- Databases: a page with properties whose rows are pages. Types: title, text, number (formats incl. 40 currencies in minor units; bar or ring), select, multi-select, status, date (end, time, zone, reminder, formats), person, checkbox, URL, email, phone, files & media, formula (Notion formulas 2.0), relation (two-way, limit), rollup, created/edited time and by, unique ID, button. Nine views: table, board, list, calendar, gallery, timeline (drag, dependencies), chart (bar, line, donut), form (workspace or public link, anonymous option), feed; each saves nested AND/OR filters, sorts, group and sub-group, footer calculations, shown properties, layout, per-view search, sub-items and a default template. Linked views show a database on another page. Row templates (repeating), automations (on add, change or schedule: set, add, notify, signed webhook), CSV import and export.
- Computed values (formulas, rollups, related titles) are worked out per viewer when read: a related row the viewer can't open shows as "No access" and is never counted (agents the same, at their capped level).
- Comments on the page, a block or a text range: threads, resolve / reopen, `@` mentions, emoji reactions, Open / Resolved / All. History: versions saved while editing, what each changed (diff), restore; an Updates feed. Following: creators and editors follow; one batched "X edited" notification per saved version, at most hourly. Analytics for editors (views and viewers per day, a year). Publish to the web (`/p/<token>`): search indexing (off by default), visitors may copy it as a template, theme, outline, link expiry; reportable.
- Agents acting for a person read and change pages with exactly that person's access, capped by their AI agent scope; writes are single reviewable changes. Connected Notion workspaces and Google Docs (with connectors) appear under **Connected** and open in the same editor and database views, live with the person's own account.

## Permission model

Every page (databases and rows too) is the access resource `docs.page` whose access parent is its parent page: sharing inherits down the tree and a subpage can be restricted. Levels come from `access_level()`.

| Who | Level |
|---|---|
| The page's owner (its creator) | manage |
| People, teams, the whole org (Share dialog), on the page or a parent it inherits from | the granted level |
| Org owners and admins | manage, only on pages whose owner was deleted or left the org |
| Anyone with a public link | view: the page and subpages that inherit from it |

**View**: read, export, history, backlinks, presence, follow. **Comment**: + comment, resolve, react. **Edit**: + blocks, title, chrome, lock, verify, analytics, move inside pages you can edit, duplicate, trash and restore, rows, properties, views, templates, automations, uploads, restore a version. **Manage**: + sharing, public links and publish options, move to the top level, delete for good. New top-level pages are private unless made for everyone or placed in a teamspace (shared with the team at edit). Org permissions: `docs.create_shared` (all roles by default) and `docs.publish` (all roles by default; the `LinkPermission` of `docs.page` and `docs.form`, checked live with `access.share_public` for each link's creator).

- Forms: a form view is the access resource `docs.form` (parent: its database). View on it lets a person submit (a row is added without edit on the database); public links refuse agent credentials.
- Relations: linking a row needs view on it; ids already linked that the caller can't open are kept on save. A two-way relation needs edit on the other database; deleting one side removes the other where the caller can edit it, else makes it one-way.
- Automations run as the person who last saved them, with that person's access at run time (losing edit pauses them); buttons run each step with the clicker's access.
- Agents get at most edit (`access_agent_cap`); they never share, publish, set publish options, export, import, submit forms, follow for someone, or create or change automations and repeating templates.

## Data

| Table | Purpose |
|---|---|
| `docs_pages` | pages, databases (`kind`, `schema`) and rows (`is_row`, `props`); `position` (fractional, `COLLATE "C"`), `version`, `locked`, `cover` (`gallery`), trash, `public_blocked_at`; `custom_icon`, `font`, `small_text`, `is_template` (workspace templates; on rows, row templates), `verified_by/at/until`, `repeat` + `repeat_next_at` (repeating row templates) |
| `docs_blocks` | one row per block: type, `props` (spans, `color`…), parent block, position, `plain` (search), `version` |
| `docs_block_tombstones` · `docs_links` | deleted block ids for `sync` (a week) · page mentions and sub-page blocks (backlinks) |
| `docs_files` · `docs_link_previews` | uploads per page (`docs_file`) · a link's title, description, site and copies of its icon and image (`docs_preview`), per URL, a week (failures a day) |
| `docs_views` | type (table, board, list, calendar, gallery, timeline, chart, form, feed), position, `block_id` + `host_page_id` (linked views), config (filter groups, sorts, groups, columns, calculations, layout, chart, form, template) |
| `docs_counters` · `docs_row_reminders` · `docs_reminders` | last unique ID per database and property · date-property reminders · date-mention reminders (`remind_at`, `sent_at`) |
| `docs_automations` · `docs_automation_runs` · `docs_form_submissions` | trigger, conditions, actions, signing secret, next run, pause reason · run log (30 days) · responses per form (who, unless anonymous) |
| `docs_comments` · `docs_comment_reactions` | threads and replies; block, quote and range anchors; resolve; soft delete; moderation hide · emoji per comment and person |
| `docs_snapshots` · `docs_follows` · `docs_page_views` | saved versions · follow / unfollow per person and page (`notified_at` batching) · views per page, day and member (a year) |
| `docs_imports` · `docs_exports` | import runs (status, progress, error report, root page) · finished exports (objects deleted after an hour) |
| `docs_publish_settings` · `docs_teamspace_prefs` | indexing, duplicate, theme, outline per published page · teamspaces hidden from a sidebar |
| `docs_favorites`, `docs_visits`, `docs_presence` (unlogged) | favorites, recent pages, who has a page open |

Computed types (formula, rollup, created/edited, button) store nothing; relation values are row ids on both sides; currencies are integer minor units (1810 converted 1.1's dollar/euro/pound values). Migrations: 1800 init, 1801 connected page, 1802 block types (+ synced-source index), 1803 link previews, 1804 reminders, 1810 databases, 1820 pages and workspace.

## API

Under `/v1/docs`, signed in unless noted. Every id goes through `access.Require` (404 without view, 403 below the level).

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/orgs/{orgID}/sidebar` · `/recent` · `/trash?by=me&kind=&in=` · `/search?q=&kind=&rows=&min_level=` · `/find?q=&kind=&created_by=&edited=&in=&sort=` | member | sections incl. `teamspaces` · recent · trashed (who, where) · title + text search · quick find |
| POST | `/orgs/{orgID}/pages` · `/import` · `/databases` | member, 300/h · 30/h · 300/h | `{parent_id?, kind, title, icon, shared, team_id?, after_id, inline}` · `{markdown}` · `{…, view_type}` → `{page, views}` |
| GET | `/pages/{id}[?preview=1]` · `/children` · `/sync?since=` · `/backlinks` · `/export` | view | detail (blocks, refs, path, views, `following`; preview counts no visit) · subpages · changes · links here · Markdown |
| POST | `/pages/{id}/ops` | edit, unlocked, 900/min | `{client_id, ops:[create\|update\|move\|delete]}` → `{version, blocks, deleted, rejected}`; block rules below |
| PATCH · POST | `/pages/{id}` · `/move` · `/duplicate` | edit (top level: manage) | title, icon, `custom_icon`, cover, `font`, `small_text`, width, lock · move · copy |
| DELETE · POST · DELETE | `/pages/{id}` · `/restore` · `/permanent` | edit · edit · manage | trash · restore · delete for good |
| PUT/DELETE | `/pages/{id}/favorite` · `/follow` · `/verification` `{days}` | view · view · edit | |
| POST · GET | `/pages/{id}/files` · `/files/{fileID}[?download=1]` | edit · view | attach an upload (any type, `content_type` checked) · 1-hour link |
| POST · GET | `/link-preview` `{url}` · `/link-previews/{id}` | signed in, 30/min | preview (safehttp or the week's cache; `embed` when allowlisted) · with fresh image links |
| GET · POST · DELETE | `/pages/{id}/presence[/{session}]` | view, 240/min | viewers · heartbeat · leave |
| GET · POST | `/pages/{id}/comments` | view · comment, 60/min | `?status=` · `{body, thread_id? \| block_id, anchor_start, anchor_end}` |
| PATCH, DELETE · POST, DELETE · PUT, DELETE | `/comments/{id}` · `/resolve` · `/reactions` | author (delete: or manage) · comment · comment / own | edit, delete · resolve, reopen · `{emoji}` |
| GET · GET · POST · GET | `/pages/{id}/history` · `/history/{id}[/diff?against=previous\|current]` · `/history/{id}/restore` · `/pages/{id}/updates` | view · view · edit · view | versions · version or block diff · restore (current saved first) · updates feed |
| GET | `/pages/{id}/analytics?days=` · `/pages/{id}/export/{format}?subpages=` | edit · view, 20/min | views and viewers · markdown, zip, html, html_zip, docx → `{url, expires_at}` |
| GET · GET · POST · POST, DELETE | `/orgs/{orgID}/templates` · `/templates/{id}/preview` · `/templates/{id}/use` · `/pages/{id}/template` | member · member · 120/h · view 30/h, edit | gallery · blocks · new page · save as / stop being a template |
| POST · GET · GET | `/orgs/{orgID}/imports` · `/orgs/{orgID}/imports` · `/imports/{id}` | member 30/h, 3 running · member · owner | start `{key (docs_import), name, parent_id?}` → 202 · my last 20 · progress, report |
| PUT/DELETE · GET, PATCH | `/orgs/{orgID}/teamspaces/{teamID}/hidden` · `/pages/{id}/publish` | on the team · view, manage | hide / show · publish options |
| GET · POST · PATCH | `/databases/{id}/rows?view=&filters=&filter_conj=&sorts=&q=&tz=&parent=&cursor=` · `/rows` · `/pages/{rowID}/properties` | view · edit · edit | rows with values per viewer (first page: total, groups, calculations) · new row · values, title, position |
| GET · POST · POST | `/databases/{id}/chart` · `/formula` · `/pages/{rowID}/buttons/{propID}` | view, 120/min · view · edit | points and series · type check + preview (`{pos, message}`) · run a button |
| PUT · POST, PATCH, DELETE | `/databases/{id}/schema` · `/databases/{id}/views`, `/views/{id}` | edit | properties (values migrate, mirrors, formula checks 400 `formula_invalid`, rescaling) · views |
| POST · GET | `/pages/{id}/linked-views` · `/pages/{id}/blocks/{blockID}/database` | edit · view on the host | linked view · the block's database (null without view) |
| GET, POST · PATCH | `/databases/{id}/templates` · `/templates/{id}` `{repeat}` | view, edit · edit (agents refused) | row templates |
| GET · POST | `/forms/{viewID}` · `/forms/{viewID}/submit` | view on the form, 30/min (agents refused) | form · response |
| GET, POST, PATCH, DELETE · GET | `/databases/{id}/automations`, `/automations/{id}` · `/automations/{id}/runs` | edit, audited (agents refused) | automations · run log |
| GET · POST · POST | `/databases/{id}/csv?view=&all=1` · `/csv/preview` · `/csv/import` | view · edit · edit 20/h | CSV · columns and inferred types · background import |
| GET | `/public/{token}[/pages/{id}]` · `/files/{id}` · `/databases/{id}/rows` · `/meta` · `/previews/{id}` | public, 120/min/IP | read-only, no author ids; `meta` gives title, indexing, theme for `<head>` |
| GET · POST | `/public/forms/{token}` | public, 120/min · 10/min/IP, 600/h/form | public form · anonymous response |
| POST | `/public/{token}/duplicate` `{org_id, page_id?}` | signed in, 20/h, allow_duplicate | a private copy of what the link opens |

Every `/public/…` route refuses agent credentials. Block rules (ops): columns only in a column list (2–5), no columns in columns or synced blocks in synced blocks, headings hold children only when toggleable, structural blocks never turn into others, a synced copy's original must be an original in the same org the copier can open, embeds only from the allowlist, buttons insert ≤ 50 blocks and link to http(s)/mailto. Files: any type up to `DOCS_FILE_MAX_MB`; images, video, audio, PDF and SVG whose bytes disagree with the type are refused (`422 content_type_mismatch`); raster, media, PDF and text serve inline, SVG as an image with `attachment`, HTML/XML/script/unknown as downloads.

Realtime `docs:<page id>` (viewers): `docs.ops`, `docs.page` (or `{row_id, reload}`), `docs.presence`, `docs.comments`, `docs.reload`, `docs.rows` (also to databases related to a changed one), `docs.schema`; `docs:<org id>` (members): `docs.tree`. Lost access drops subscriptions (`access.OnChange`). Notifications (never to someone who can't open the page or blocks the sender; agents see them only for pages their scope reads): `docs.mention`, `docs.comment`, `docs.reminder` (push, email), `docs.page_update` (email), `docs.automation`. Jobs: `docs.snapshots`, `docs.reminders`, `docs.row_reminders`, `docs.repeating_templates`, `docs.automation_schedules` (each minute), `docs.imports` (5 min; dead runs fail within 30 min), `docs.cleanup` (15 min), `docs.databases_cleanup` (hourly), `docs.previews_cleanup` (daily; previews unused 90 days). Moderation targets `docs.page` and `docs.comment`. `app.SearchProvider`: `docs.page` (templates excluded; `Context` = org · parent page when you can open it).

Agent tools, run as the caller with `access.ViaAgent` in every query: `docs_search_pages`, `docs_get_page` (Markdown with `<!-- block:<id> -->`; equations `$$`, toggles `<details>`, `<columns>`, `[TOC]`, synced copies as their original when readable), `docs_list_children`, `docs_query_database` (nested filters, names, totals; computed values honour access), `docs_list_templates`, `docs_page_history`. Writes set `Target` (`docs.page`) and an exact `Preview`: `docs_create_page`, `docs_create_from_template` (re-checks edit on the parent; no teamspaces or top level), `docs_update_block` (`expected_text`; equation TeX), `docs_insert_blocks` (same Markdown), `docs_set_property` (every settable type; relations among rows the person can open), `docs_add_row`, `docs_add_comment`.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Workspace | `/docs`: sidebar sections, footer Templates · Import · Trash, ⌘P quick find, New page dialog (Empty / Templates / Import; private, everyone or a teamspace); home: recent + template quick start | `docs` (home entry): Recent, Favorites, teamspaces, Shared, Private; New page sheet (Empty / Template / Import via the document picker) |
| Page | `/docs/[id]`: breadcrumbs, presence, save state, comments, favorite; cover and icon pickers, Verified chip, template banner, outline ≥ 1280 px, hover cards; ⋮ (copy link, move, duplicate, lock, width, text style, follow, verify, page info, save as template, publish, history, export, import here, trash; Share… from access); row properties, editor or database, backlinks | `docs/[id]`: every block rendered natively (columns stack, equations as TeX text, media and embeds in the in-app browser); tap to edit text as plain text with `- ` `# ` `[] ` shortcuts; long press: add below ("+" `BlockInsertSheet`), move, turn into, delete; cover and icon sheets; ⋮ sheet adds follow, history, export (share sheet), save as template, text style |
| Database | view tabs (nine types), nested filters, sorts, group, properties, calculations, layout; templates by New; ⋮ Automations, Import / Export CSV, Share form…; property menus for every type (formula editor with live check) | view chips; filter / sort / group / properties / calculations / layout sheets; every view (timeline as bars, chart as SVG with a values table); templates on New; automations read-only; schema edits stay on web |
| Forms | `/docs/forms/[viewId]` (people it's shared with); public `/p/form/[token]` | `docs/forms/[viewId]`, `p/form/[token]` |
| History · Comments | Versions (What changed · Since then · Whole page, restore) and Updates; side panel ≥ 1280 px with reactions, filter, jump to block | `docs/[id]/history` (diff sheet, restore, updates); `docs/[id]/comments` (reply, edit, delete, report, reactions, mentions, filter) |
| Public | `/p/<token>[/<page id>]`: read-only, outline, publisher's theme, duplicate (signed in, when allowed), report; robots from publish options | `p/[token]?page=` |

Web only: editing columns, synced blocks and buttons; Mermaid and KaTeX drawing; drag and drop; inline formatting.

## Config

| Variable | Default | Meaning |
|---|---|---|
| `DOCS_FILE_MAX_MB` | 25 | largest file of any type a page accepts |
| `DOCS_IMPORT_MAX_MB` | 50 | largest import upload (files inside still follow `DOCS_FILE_MAX_MB`) |
| `DOCS_TRASH_DAYS` | 30 | days before the trash is emptied |
| `DOCS_HISTORY_DAYS` | 90 | days versions are kept (the newest always) |
| `DOCS_SNAPSHOT_INTERVAL` | 10m | version cadence while someone keeps editing (≥ 1m); idle pages after 2 minutes |

Upload purposes: `docs_file`, `docs_preview` (server copies), `docs_import` (text, CSV, zip, docx), `docs_export` (server-written). Dependencies: web `katex` and `mermaid` (MIT, lazy-loaded; KaTeX's stylesheet and fonts bundled); mobile `expo-document-picker` (native: rebuild the binary); Go `golang.org/x/net/html` (BSD-3, HTML import; `go mod tidy` resolves it, no manifest entry). Code highlighting uses the shared preview kit's highlight.js; block colours are the tokens' swatches.

## Compliance

- Deletion: private pages (top level, owned, nothing in the tree shared) go; their comments become "Comment deleted"; favorites, visits, presence, uploads, follows, page views, reactions, import runs, exports, teamspace choices and row reminders go; their automations stop and lose their creator, repeating templates stop, form responses lose who sent them; names leave attribution, verification and publish-settings columns. Shared pages stay with the org (admins manage them).
- Export: pages they created (with Markdown), comments, favorites, follows, page views, reactions, import runs, hidden teamspaces, automations they set up, forms they answered.
- UGC: comments pass `moderation.Check`, can be reported and hidden; blocked people's comments and reactions are hidden both ways and never notify. Published pages are public and reportable; staff removal stops the public copy. Page text is internal collaboration and isn't filtered keystroke by keystroke.
- Privacy: link previews are fetched by the server and their images copied, so sites never learn who reads a page; embeds frame only the provider's src with `referrerpolicy="strict-origin"` and no camera, microphone or location. Anyone with a public form link can add a row (rate-limited; the form can be closed).

## Connected platforms

Needs the connectors feature (`app.Connections`); without it Docs offers no connected sources. Docs never stores platform content: only pages people copy in and connected-page blocks (a reference).

| Capability | Provider scopes | What it gives |
|---|---|---|
| `docs.notion.read` · `docs.notion.write` | none (Notion's consent screen picks the pages; more are shared from a page's ••• → Connections) | pages, blocks, databases, comments · edits |
| `docs.google.read` · `docs.google.write` | `documents.readonly` + `drive.readonly` · `documents` + `drive` | Google Docs (Drive lists them; comments via Drive) · edits |

- **Browse**: sidebar Connected section (kit `ConnectedSection`) per account with status and Reconnect; Notion: shared pages and databases as a lazy tree; Google: Recent and Shared with me (documents only); search within each. Mobile: `docs` home → `docs/c/[conn]`.
- **Render**: `/docs/c/[conn]/[ref]` (mobile `docs/c/[conn]/[ref]`) with a `SourceBadge`. Notion text blocks, lists, to-dos, toggles and toggle headings, quotes, callouts, code, dividers, column lists, synced blocks (read-only), equations, tables of contents, link-to-page, video/audio/PDF, allowlisted embeds and colours (text, highlights, block colours; written back) map to Docs blocks with rich text and mentions; images, files, bookmarks, sub-pages and tables show read-only. Notion databases render in Docs' views (viewer's own views and filters, translated to Notion queries); simple types edit; formulas, rollups, created/edited, unique IDs, files and people are read-only; relations stay read-only text ("3 linked"); currencies scale to minor units. Google Docs: named styles → headings, bullets and numbering → lists, tables, inline images (read-only), rules, links and inline marks.
- **Write back**: `DocsPageSession` sends block operations to `POST /docs/connected/{conn}/pages/{ref}/ops`; an in-memory per-person session (2 h) maps block ids. Notion: update, append after, archive, turn into, to-dos, rename, row values, new rows and pages, comments. Google: one atomic `batchUpdate` per batch with UTF-16 index math, guarded by `requiredRevisionId`. Moves and unsupported blocks are refused ("Edit in Notion"). A block changed on the platform since loading, or a moved Google revision, is refused as `changed_on_platform` and the page reloads.
- **Bridges**: ⋮ Open in / Manage sharing in the platform, Copy into Docs (media become links, sub-pages connected-page blocks), Add to a Docs page (a `connected_page` block each viewer opens with their own connection, or is offered Connect / Reconnect / Ask for access).
- **Search and agents**: `SearchProvider` adds each active connection's hits (3 s each, `Kind` `docs.external`, `Source`). Read tools take `source` (from `docs_list_sources`). Writes are `docs_notion_{update_block,insert_blocks,set_property,create_page,add_comment}` and `docs_google_{update_block,insert_blocks,add_comment}` with `Source`, `Target` `docs.external` / `<connection>:<platform id>`, exact Preview, and the connection's agent scope checked in Preview (suggest) and Handler (edit); Google edits require `expected_text`.
- Routes (signed in, the caller's own connection): `GET /docs/sources`, `/docs/connected/resolve`, `/docs/connected/{conn}/tree` · `/search` · `/pages/{ref}` · `/sync` · `/comments` · `/databases/{ref}/rows`; `POST` `…/pages/{ref}/ops` · `/comments` · `/copy` · `/databases/{ref}/rows` · `/pages`; `PATCH` `…/pages/{ref}` · `/rows/{ref}`. Errors carry `app.Connection*` codes with `fix_url`, never a 401.
- Out of scope: realtime from the platforms, moving blocks, editing media/tables/synced blocks, Notion block comments, Google suggestions mode, checklists' checked state and table edits, Confluence (add it as `docs.atlassian.*` beside these two).

## Stitching

1. Place `docsNav` (and promote mobile `docsHomeEntry` to a tab) where docs belongs; optionally link Templates from the home screen.
2. Choose defaults for `docs.create_shared` and `docs.publish` per role.
3. Install `agents` (per-page AI scopes) and `search` (global search); both plug into the seams above.
4. Privacy policy: published pages are readable by anyone with the link, publishers may let search engines index them or let visitors copy them (off by default); anyone with a public form link can add a row; people who can edit a page see who viewed it. Community rules: published pages and comments can be reported.
5. Public paths: `/p/(.*)` (web; covers forms at `/p/form/<token>`) and `p/[token]`, `p/form/[token]` (mobile) are wired; configure universal links for `/p/*` if published pages and forms should open in the app.
6. With connectors 2.0: register the Notion integration (read, update and insert content, comments, user names) and the Google OAuth client, and say in the privacy policy that connected content is read live with each person's own account and isn't stored.
7. Read seam for other features: `docs.PageRef(ctx, q, viewer *auth.Identity, pageID) (*docs.PageLink, error)` reads a page as the viewer with the same check as `GET /pages/{id}` (`access_level` on `docs.page`, capped by `access.ViaAgent(ctx)`), refuses trashed pages, records no visit, and returns title, emoji / custom icon, `Context` (org · parent when the viewer can open it), a first-line `Snippet`, `Path` (`/docs/<id>`), level and `UpdatedAt`; else `docs.ErrPageNotFound`. `docs.PageIDFromPath(path)` reads the id from `/docs/<id>`. Use it for link cards (`channels.RegisterUnfurler("/docs/", …)`, returning nil on not found) and assistant context.

## Extending

- A block type: migration for the type check, `normalizeProps` (+ `holdsChildren` if it nests), Markdown both ways (Go `markdown.go`, `shared/markdown.ts`), the catalogue in `shared/blocks.ts`, web `BlockBody` and `BlockView`, mobile `Blocks`. An embed: a rule in `embeds.go` and its mirror in `shared/blocks.ts`.
- A property type: `properties.go` (normalise, migrate values), filters and sorts (`rows.go`, `compute.go` when computed), shared `database.ts`, web `PropertyValue` and `PropertySheet`, mobile `values.tsx` / `editors.tsx`. Formula functions: `formula.go` (types) and `formula_eval.go`.
- `DocsPageSession` (shared) drives any editor; keep `docsMergeText` (TS) and `mergeSpans` (Go) identical.
- Computed values are per viewer, so filtering, sorting, grouping or calculating on formulas and rollups reads every row the viewer can open (≤ 10,000) in Go; plain views stay keyset SQL.
- Out of scope: place property and map view (no keyless, permissive tile source that keeps viewers' addresses private), cursors inside a block, mobile rich formatting, equations and Mermaid drawn natively on mobile, editing synced copies or columns on mobile, oEmbed beyond the allowlist, preview images on bookmarks made before 1.2.0 (until edited), "unsync all copies".

## Provenance

New in the forum belt: Notion's block model, page tree and databases on the `access` sharing model; positions follow rocicorp's fractional-indexing; formulas follow Notion formulas 2.0.

## Changes

- 1.2.0: blocks and commands: every Notion block type (heading 4 and toggle headings, columns, synced blocks, TOC, breadcrumb, equations, buttons, link to page, video, audio, PDF, allowlisted embeds, bookmark previews, colours), inline colours, equations, date reminders and emoji, the full `/` menu, Notion's shortcuts and keys, any file type with type-checked serving (1802–1804); pages and workspace: icon set and image icons, cover gallery, fonts, verification, page info, outline, hover previews, 18 built-in and workspace templates, background imports (Markdown, HTML/Confluence, Word, CSV, Notion zips) and exports (Markdown/CSV zip, HTML, Word, PDF) with subpages, version diffs and an Updates feed, following, analytics, teamspaces, quick find, trash filters, comment reactions, publish options and duplicating published pages; duplicate copies only subpages you can open (1820); databases: phone, files, formula, relation, rollup, created/edited, unique ID and button properties, currencies in minor units, date formats, zones and reminders, gallery, timeline, chart, form (public at `/p/form/<token>`) and feed views, nested filters, sub-groups, calculations, sub-items, linked views, repeating row templates, automations, CSV import/export, `docs_add_row`, computed values per viewer (1810, currency values converted). `docs.PageRef` read seam. New env `DOCS_IMPORT_MAX_MB`; deps katex, mermaid (web), expo-document-picker (mobile), golang.org/x/net (Go).
- 1.1.0: connected platforms. Notion pages and databases and Google Docs, browsed under Connected, rendered and edited in the docs editor and database views, written back live (`docs.notion.*`, `docs.google.*` capabilities); copy into Docs; `connected_page` blocks (migration 1801); connected search results; `source` on the read tools and per-platform write tools. Properties gain `read_only`, rows `source`/`ref`/`url`; images and files of connected pages carry `url`. The editor takes `connected` (types, read-only blocks, no moves); `ViewProps` takes `schemaEditable` and `external`.
- 1.0.4: registering an upload checks the object exists and records its stored size (a key never uploaded, or a size the client misstated, no longer makes a broken file). File links carry the file's name and type (`inline; filename=…`), so downloads save under it. Errors, the shared-pages permission and agent tools name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
- 1.0.3: copy names organizations with orgs' noun (`orgsNoun`…, orgs ≥ 1.2.0). Search results carry `OrgID` and `Context` (org · parent page) and honour the `org_id` scope (search ≥ 1.1.0). Mention and comment notifications run under `bg.Go`.
- 1.0.2: the dev seed shares through `access.AddGrant` (no direct writes to access tables; a grantee the share dialog would refuse is skipped); the public routes refuse agent credentials; the `docs_create_page` preview shows the title and icon exactly as stored, and the `docs_add_comment` preview names the block it attaches to. Mention and comment notifications name their page (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for a page its scope hides.
- 1.0.1: `docs.publish` is the page kind's access `LinkPermission` (access ≥ 1.2.0): the Share dialog no longer makes links that can't open for people without it, and docs' own resolve-time check is gone (access does it). Behaviour is otherwise unchanged.
- 1.0.0: first release.
