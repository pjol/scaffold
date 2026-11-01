# CRM

## What it does

- Companies, contacts (several emails and phones, companies with a primary one, lifecycle stage) and deals (amount in minor units + currency, pipeline, stage, close date, company, contacts) per org, each with an owner, a team, tags and custom properties (text, number, date, dropdown, multiple choice, person, web address, email, phone, amount, checkbox).
- Record pages: properties edited in place on the left, a timeline in the centre (notes, logged calls, emails and meetings, tasks, field changes, stage moves, links, merges) with a composer and @mentions of people and teams, associations on the right; tabs on narrow screens. Pipelines with ordered stages, probabilities and won/lost; stage moves keep history and time in stage.
- Lists per type with saved views (personal or shared), a column picker, sort, AND filters on any property, search, keyset paging, bulk assign/tag/move/delete and CSV export; a deals board per pipeline with drag (or a ⋮ move), totals and weighted totals per stage, owner and close-date filters; my tasks (overdue, today, upcoming, done) with one-time reminders; a dashboard (open pipeline by stage, won and lost this month, activity this week, tasks due).
- Duplicate warnings while typing (email, domain), merge (associations and timeline move over), CSV import with column mapping, a per-row preview, dedupe by email/domain and background runs with an error report.
- Every record is an access resource: sharing through the access feature's ⋮ "Share…", and every list, total, board, dashboard number, export, search hit and agent tool comes from one access-visible query. Mobile: search, lists, record with timeline, log an activity, my tasks, the board with stage moves, call and email links.

## Permission model

| Who | Can | Default |
|---|---|---|
| Everyone with `crm.access` | see the CRM at all (REST, search, tools); create records (they own them) | owners, admins, members |
| Record access `everyone_edits` | edit every record (org grant 3 on `crm.workspace`) | the default |
| `everyone_views` | view and log activity (comment = 2) on every record; owners, sharees and admins edit | |
| `owners_teams` | owner (4), the record's team (edit 3 on `crm.team`), people/teams it's shared with, org admins | |
| Org owners/admins | manage every record (`AdminsManage`) | |
| Levels on a record | view: read · comment: log activities, add tasks · edit: change fields, stage, links, reassign, edit others' activities · manage: share | |
| `crm.manage_settings` | pipelines, properties, tag rename/delete, record access, currency (audited) | owners, admins |
| `crm.export` / `crm.import` / `crm.delete` | export a view · import CSV · delete and merge (plus edit on each record) | owners, admins |

AI agents get the person's level capped by `access_agent_cap` (never above edit) and never share, delete, merge or export; they can't change a record's owner or team either (both decide who can open it).

## Data

| Table | Purpose |
|---|---|
| `crm_settings` | per org: record access mode, default currency |
| `crm_pipelines`, `crm_stages` | ordered stages with probability and outcome (open/won/lost) |
| `crm_properties`, `crm_tags` | custom properties per object type (values in each record's `props` jsonb); org tags |
| `crm_companies`, `crm_contacts`, `crm_deals` | records; owner is `access_resources.owner_id`; `team_id` picks the access parent |
| `crm_contact_companies`, `crm_deal_contacts` | associations (one primary company per contact); deals carry `company_id` |
| `crm_deal_stages` | stage history (entered/exited) |
| `crm_activities`, `crm_activity_records` | notes, calls, emails, meetings, tasks; linked to every record they show on |
| `crm_events` | timeline entries: created, updated (changes), stage_changed, associated, dissociated, merged, imported |
| `crm_views`, `crm_imports` | saved views; import runs with counts and the first 200 row errors |

Access kinds: `crm.workspace` (id = org id, holds the org grant), `crm.team` (id = team id, holds the team grant in `owners_teams`), `crm.company|contact|deal` under their team's resource or the workspace.

## API

All under `/v1`; org routes need membership + `crm.access`; record routes check the record (404 without view, 403 below the level) and `crm.access` in its org. Every CRM access kind sets `Permission: crm.access`, so access's sharing endpoint, topics and tools (and `access.LevelOf`) treat CRM resources as missing without it.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET / PUT | `/crm/orgs/{orgID}/settings` | crm.access / manage_settings | settings + `can`; PUT rewrites workspace/team grants |
| GET / POST / PATCH / DELETE | `/crm/orgs/{orgID}/pipelines[/{id}]` | crm.access / manage_settings | stages replace in order; removed stages need `stage_moves` (409 `stage_has_deals`) |
| GET / POST / PATCH / DELETE | `/crm/orgs/{orgID}/properties[/{id}]` | crm.access / manage_settings | built-in + custom properties |
| GET / POST / PATCH / DELETE | `/crm/orgs/{orgID}/tags[/{id}]` | crm.access (create) / manage_settings | |
| GET / POST / PATCH / DELETE | `/crm/orgs/{orgID}/views[/{id}]` | owner; shared ones also manage_settings | |
| GET / POST | `/crm/orgs/{orgID}/{companies,contacts,deals}` | crm.access | `filters` (JSON), `q`, `sort`, `dir`, `cursor`, `tz` → `{items, next_cursor, total}`; create (409 `duplicate`) |
| GET / PATCH / DELETE | `/crm/{type}/{id}` | view / edit / edit + crm.delete | detail with visible associations |
| GET / POST | `/crm/{type}/{id}/timeline`, `/activities` | view / comment | `filter`, keyset; first page has `upcoming` |
| PATCH / DELETE | `/crm/activities/{id}` | author (comment) or edit; assignee for `done` | |
| POST / GET | `/crm/deals/{id}/stage`, `/stages` | edit / view | move; history and time per stage |
| POST / PUT / DELETE | `/crm/contacts/{id}/companies[/{companyID}[/primary]]`, `/crm/deals/{id}/contacts[/{contactID}]` | edit here, view there | |
| GET | `/crm/orgs/{orgID}/tasks`, `/tasks/summary`, `/board`, `/dashboard`, `/search`, `/duplicates` | crm.access | all visible-only |
| POST | `/crm/orgs/{orgID}/bulk` (≤200), `/merge` | edit per record; delete/merge need crm.delete, 60/h | audited bulk deletes and merges |
| POST / GET | `/crm/orgs/{orgID}/imports/inspect`, `/preview`, `/imports`, `/imports[/{id}]` | crm.import; 60/h, runs 10/h | background run, poll |
| GET | `/crm/orgs/{orgID}/export` | crm.export, 20/h | CSV (≤10,000 rows, formula-safe), audited |

Realtime: `crm:<record id>` (`crm.record_changed`, `crm.timeline_changed`, `crm.record_deleted`), `crm:<pipeline id>` (`crm.board_changed`), `crm:<org id>` (`crm.settings_changed`). Lost record access revokes through `access.OnChange`; losing `crm.access` (an admin changed it, a team or role change, `access.OnPermissionChange`) or leaving the org (`orgs.OnMemberRemoved`) re-checks the org and pipeline topics (and, for a permission change, the newest 1,000 records') right after the commit. Notifications: `crm.mention`, `crm.task_assigned`, `crm.task_reminder` (worker, each once), `crm.assigned`. Search: `crm.company|contact|deal`. Agent tools (as the caller, `access.ViaAgent`): `crm_search`, `crm_get_contact|company|deal`, `crm_list_deals`, `crm_list_activities`, `crm_list_my_tasks`; writes with `Target` + `Preview`: `crm_update_field` (exact before/after; refuses `owner` and `team` with 403 `agent_forbidden`), `crm_move_deal_stage`, `crm_add_note`, `crm_create_task`, `crm_log_call` (previews list every field set, assignee by name).

## Screens

| Where | Web (`@/features/crm`) | Mobile |
|---|---|---|
| Nav | main nav + Home tile "CRM" (badge: tasks overdue or today); `/crm` dashboard | Home → CRM: search, My tasks, Deals board, lists |
| Lists | `/crm/contacts`, `/companies`, `/deals`: views, filters dialog, columns dialog, bulk bar, export; table ≥640 px, rows on phones | `crm/{contacts,companies,deals}` with search and Mine |
| Record | `/crm/{type}/[id]`: header (call/email, ⋮ with Share…, Merge…, Delete), deal stage bar, properties, time in stage, timeline + composer, associations | `crm/{type}/[id]`: call/email/log/stage, Activity · About · Related |
| Board, tasks | `/crm/deals/board`, `/crm/tasks` | `crm/deals/board`, `crm/tasks` |
| Admin desk | `/crm/import` wizard + history, `/crm/settings` (record access, currency, pipelines, properties, tags) | web only |

Every async view has skeleton, empty and error states; no-org and no-`crm.access` states are explicit.

## Config

No variables. No new npm or Go dependencies (`pg_trgm` is a trusted extension). Imports: 5 MB / 5,000 rows; exports: 10,000 rows.

## Compliance

- Records, activities and history are org data and stay when someone deletes their account; their owner field is cleared (access), their name comes off authorship, history, assignments, mentions and person-valued properties, and their personal views go. Export: activities they wrote or were assigned, records they own, their views.
- Org-internal business records, visible only to org members with access; not public UGC. CSV export cells are escaped against formula injection; exports and imports are audited and rate-limited.

## Stitching

1. Choose defaults: record access (`everyone_edits`) and who holds `crm.import`/`crm.delete` (Settings → Permissions).
2. Place the nav entry (web main nav + Home tile; mobile Home entry) or move CRM to a mobile tab.
3. Link from other features to `/crm/{companies|contacts|deals}/<id>`.
4. Adjust the default pipeline and seeded custom properties to the product.
5. Review record-access and lifecycle copy (`shared/format.ts`).
6. With the `agents` feature, per-record agent scopes inherit through `crm.team`/`crm.workspace`; nothing to wire.

## Extending

- Go: the package exports kinds (`crm.KindCompany`, …), permission keys (`crm.PermExport`, …) and notification kinds; add behaviour in this package. Every new list or aggregate must start from `visibleFrom(o)` (query.go) so totals can't include hidden rows.
- TS: `crmEndpoints`, `useCrm…` hooks, `crmMoney`, `crmFieldText`, `crmBodySegments`, `crmFilterOps`; web `CrmRecordScreen`, `RecordPicker`, `CreateRecordDialog`.
- Out of scope: email sync/sending and calendar sync (log them instead), sequences, quotes/products/line items, exchange rates (totals are per currency), owner-based "teams" beyond one team per record, workflow automation, per-field permissions.

## Provenance

New in the forum belt, modelled on HubSpot's CRM (objects, associations, pipelines, timeline, views, board, import). Permissions are built entirely on `access` (no parallel ACL): structural resources hold the org's record-access mode, so a mode change rewrites a handful of grants.

## Changes

- 1.1.1: errors and agent tool descriptions name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
- 1.1.0: losing `crm.access` drops live CRM subscriptions right after the commit: `access.OnPermissionChange` (access ≥ the version that adds it) re-checks the org, pipeline and newest records' topics, and `orgs.OnMemberRemoved` the org and pipeline topics (access's own hook already covers records); before, `crm:<org>` and `crm:<pipeline>` kept delivering until the socket resubscribed. The CSV import runs on `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for it. The record page's activity column is a `<section>` (it nested a second `<main>` inside the shell's). The in-page org switcher follows `useOrgPickerVisible` (hidden when the shell has one) and “organization” in copy comes from the orgs noun helpers. Search sets `OrgID` and `Context` ("<org> · CRM") and keeps to the org a search is scoped to (`app.SearchOrg`).
- 1.0.2: `crm_update_field` refuses `owner` and `team` (403 `agent_forbidden`) in its preview, its run and an accepted suggestion: they move a record's access owner and parent, so an agent could change who can open it. Task and call previews list every field the write sets (assignee by name, reminder, priority, direction, duration, time), and a task for someone who can't see the record is refused before review. Mention, task and assignment notifications name their record (`AgentTarget`, notifications ≥ 1.1.1; task notifications now carry the record's type and id), so an agent reading the inbox doesn't see them for a record its scope hides.
- 1.0.1: structural grants (the workspace's general access, each team's edit on its crm.team) go through `access.SetGrant` / `RemoveGrant` instead of SQL on access tables, and skip deleted teams; every CRM kind sets access's `Permission: crm.access` (access ≥ 1.2.0), so someone without `crm.access` can no longer read a record's title or owner through access's sharing endpoint or `access_get_sharing`.
- 1.0.0: first release (backend, migration 1900, shared, web, mobile).
