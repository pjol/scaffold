# CRM

## What it does

- Companies, contacts (several emails and phones, companies with a primary one, lifecycle stage, lead status, lead score), deals (amount in minor units + currency, pipeline, stage, forecast category, close date, company, contacts) and support tickets (subject, status from a ticket pipeline, priority, channel, category, company, deal, contacts) per org, each with an owner, a team, tags and custom properties.
- Record pages: properties edited in place, a timeline (notes, calls, emails, meetings, tasks, field changes, stage moves, links, merges, quotes, form submissions, workflow steps) with a composer and @mentions, associations; tabs on narrow screens. Pipelines per object (deals: probabilities, won/lost; tickets: open/closed) keep stage history and time in stage. Tickets record first-response time (first outbound email, call or meeting) and resolution time (entering a closed status).
- Lists per type with saved views, columns, sort, AND filters, search, keyset paging, bulk assign/tag/move/delete, CSV export; deal and ticket boards with drag (or ⋮ move); my tasks with reminders; a dashboard. Duplicate warnings, merge (associations, timeline, tickets, line items and quotes move over), CSV import with mapping, preview and dedupe.
- **Products and quotes**: a product library (SKU, price, one-time or recurring billing with a term, tax); deal line items with quantity (thousandths), discount (% or amount) and tax, totals per billing period and contract value (integer minor units, half-away-from-zero rounding); the deal amount can follow the line items. Quotes copy the lines, number themselves per workspace (Q-0001), and are published as a public link (`/q/<token>`) where the buyer reads, prints (print stylesheet) and accepts or declines with a typed name (name, email, time and IP are kept); status draft → published → accepted · declined (expired once past its date), with view counts and a notification.
- **Email through connected accounts**: compose to a contact from the record page and send as the person with their own Gmail or Outlook (`app.Connections`), with shared or personal templates and merge fields (`{{contact.first_name | there}}`, `{{deal.amount}}`, `{{booking_link}}`…); sent mail is logged on the contact and on the deal/company/ticket chosen. "Log to CRM" lists recent mail with the contact read live from the mailbox (nothing stored but what's logged). Drafts are personal. No open or click tracking.
- **Meetings**: "Log meeting" picks the person's calendar events with the contact's addresses; "Insert booking link" adds their booking page — both through stitching seams, hidden when unwired.
- **Workflows**: triggers (created, a property changed, a stage entered, a date property reached ± days) with AND conditions; steps: wait, set a property, create a task, rotate the owner, notify people, add a tag, create a deal or ticket, call a webhook (HMAC-signed, `safehttp`). A workflow runs as the person who turned it on, with their access; runs are logged per record, audited, loop-protected (chains stop at depth 3, a record re-enrolls only when allowed and not within the hour).
- **Lead forms**: public page (`/forms/<token>`) or iframe embed; submissions create or update the contact by email (and the company), set the owner, tags and lifecycle stage, record consent, notify chosen people; honeypot and per-IP and per-form rate limits.
- **Lead scoring and status**: rules (any filter on contacts → points) score every contact on each write and every 6 hours; lead status is a built-in property.
- **Reports and dashboards**: count/sum/average of any object, grouped by a property (dates by day…year), filtered, as number, bar, line, donut or table; dashboards are access resources shared like records; every number is computed from the viewer's visible records. **Forecast** by owner and month in four categories (closed won, commit, best case, pipeline; omitted excluded) with weighted totals; **goals** (revenue won, deals won, deals created per person or team, month/quarter/year) with progress from visible deals.
- Every record is an access resource: sharing through access's ⋮ "Share…", and every list, total, board, report, forecast, export, search hit and agent tool comes from one access-visible query. Mobile: lists, records, tickets with status changes, deal line items and quotes (copy link), compose email, reports, tasks, board.
- Teams on HubSpot or Salesforce work their CRM here without migrating (see Connected platforms; tickets, products and quotes are app-only).

## Permission model

| Who | Can | Default |
|---|---|---|
| Everyone with `crm.access` | see the CRM; create records (they own them); add line items, draft quotes, send email, build personal dashboards | owners, admins, members |
| Record access `everyone_edits` / `everyone_views` / `owners_teams` | org grant edit · comment on every record · owner, team, sharees and admins only | `everyone_edits` |
| Org owners/admins | manage every record (`AdminsManage`) | |
| Levels on a record | view: read · comment: log activities, tasks · edit: fields, stage, links, line items, quotes, email logged to it · manage: share | |
| `crm.manage_settings` | pipelines, properties, tags, record access, currency, score rules, goals (audited) | owners, admins |
| `crm.export` / `crm.import` / `crm.delete` | export · import · delete and merge (plus edit on each record) | owners, admins |
| `crm.manage_products` | the product library | owners, admins |
| `crm.publish_quotes` | publish a quote link (plus edit on the deal and access's "Make public links") | owners, admins, members |
| `crm.manage_workflows` | create, change, turn on workflows; see runs, run counts and webhook secrets | owners, admins |
| `crm.manage_forms` | create and publish forms (plus "Make public links"); read submissions | owners, admins |

Public quote and form links are access links (`LinkPermission`): they stop working when the publisher loses the permission. AI agents get the person's level capped by `access_agent_cap` (never above edit) and never share, delete, merge, export, publish, unpublish, send email or change owners and teams.

## Data

| Table | Purpose |
|---|---|
| `crm_settings` | record access, currency, source mode, quote numbering, ticket bootstrap |
| `crm_pipelines`, `crm_stages` | per object type (`deal`, `ticket`); outcome open · won · lost · closed |
| `crm_properties`, `crm_tags`, `crm_views` | custom properties (values in `props` jsonb), tags, saved views |
| `crm_companies`, `crm_contacts`, `crm_deals`, `crm_tickets` | records (owner is the access owner); contacts carry `lead_status`, `score`; deals `forecast_category`, `amount_from_items`; tickets `first_response_at`, `closed_at` |
| `crm_contact_companies`, `crm_deal_contacts`, `crm_ticket_contacts` | associations |
| `crm_deal_stages`, `crm_activities`, `crm_activity_records`, `crm_events` | stage history; activities (emails carry provider, message id, from/to, unique per org); timeline |
| `crm_products`, `crm_line_items` | library; deal lines (snapshot of name and price) |
| `crm_quotes`, `crm_quote_items` | quotes with totals, link id/token, signer name, email, IP, time |
| `crm_email_templates`, `crm_email_drafts` | templates (personal or shared); personal drafts |
| `crm_workflows`, `crm_workflow_runs` | definitions (trigger, conditions, steps jsonb, owner, secret); runs with step log, resume time, depth |
| `crm_forms`, `crm_form_submissions` | forms (fields, consent text, owner, notify, link); submissions (values, consent, page, record created or updated) |
| `crm_dashboards`, `crm_reports`, `crm_goals`, `crm_score_rules` | dashboards (access resources) and their reports; goals; scoring rules |
| `crm_imports`, `crm_external_links` | import runs; company links to platform companies |

Access kinds: `crm.workspace`, `crm.team`, `crm.company|contact|deal|ticket`, `crm.quote` (under its deal; public path `/q/`), `crm.form` (`/forms/`), `crm.dashboard`.

## API

All under `/v1`; org routes need membership + `crm.access`; record routes check the record (404 without view, 403 below the level). Every CRM kind sets `Permission: crm.access`.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET / PUT | `/crm/orgs/{orgID}/settings` | crm.access / manage_settings | settings + `can` |
| CRUD | `/crm/orgs/{orgID}/{pipelines,properties,tags,views}` | crm.access / manage_settings | pipelines take `object_type` |
| GET / POST | `/crm/orgs/{orgID}/{companies,contacts,deals,tickets}` | crm.access | filters, sort, keyset → `{items, next_cursor, total}` |
| GET / PATCH / DELETE | `/crm/{type}/{id}`, `/timeline`, `/activities` | view / edit / comment | detail, timeline, log |
| POST | `/crm/deals/{id}/stage`, `/crm/tickets/{id}/stage` | edit | move (ticket stages only from ticket pipelines) |
| GET | `/crm/orgs/{orgID}/{tasks,board,dashboard,search,duplicates}` | crm.access | `board?object=ticket` |
| POST / GET | `/bulk`, `/merge`, `/imports…`, `/export` | per record + crm.delete/import/export | rate-limited, audited |
| CRUD | `/crm/orgs/{orgID}/products[/{id}]` | crm.access / manage_products | library |
| GET / POST / PUT / PATCH / DELETE | `/crm/deals/{id}/line-items[/order]`, `/amount-source`, `/crm/line-items/{id}` | view / edit | lines; amount follows items |
| GET / POST / PATCH / DELETE | `/crm/deals/{id}/quotes`, `/crm/quotes/{id}[/refresh]` | view / edit | drafts |
| POST | `/crm/quotes/{id}/publish`, `/unpublish` | edit + publish_quotes, 60/h | link (`url` hidden from agents) |
| GET / POST | `/crm/public/quotes/{token}[/accept|/decline]` | none; no agent credentials; 60/min, 20/h per IP | 410 `quote_expired` after expiry |
| GET / POST | `/crm/mail/accounts`, `/crm/mail/booking-links`, `/crm/mail/render` | signed in | mailboxes, booking links, merge preview |
| POST / GET | `/crm/contacts/{id}/mail` (200/h), `/mail/recent`, `/mail/log` (600/h) | edit on the contact | send as the person; read live; log (409 when logged) |
| CRUD | `/crm/contacts/{id}/mail/drafts`, `/crm/mail/drafts/{id}`, `/crm/orgs/{orgID}/mail/{drafts,templates}` | own drafts; templates: author | |
| GET / POST | `/crm/contacts/{id}/meetings[/log]` | view / edit | calendar seam |
| POST / CRUD | `/crm/orgs/{orgID}/reports/run`, `/dashboards`, `/crm/dashboards/{id}[/reports]` | crm.access; dashboard level | 1,200 runs/h |
| GET / CRUD | `/crm/orgs/{orgID}/forecast`, `/goals` | crm.access / manage_settings | |
| CRUD | `/crm/orgs/{orgID}/score-rules` | crm.access / manage_settings | rescoring follows |
| CRUD / GET | `/crm/orgs/{orgID}/workflows[/{id}[/runs]]` | crm.access (read) / manage_workflows | runs and counts: managers only |
| CRUD / POST / GET | `/crm/orgs/{orgID}/forms[/{id}[/publish|/unpublish|/submissions]]` | crm.access (read) / manage_forms | |
| GET / POST | `/crm/public/forms/{token}` | none; no agent credentials; 10/h per IP, 300/h per form | honeypot `website` |
| GET / POST / DELETE | `/crm/sources`, `/crm/orgs/{orgID}/sources/…`, `/crm/companies/{id}/links` | see Connected platforms | |

Webhooks: `POST` JSON `{event, workflow:{id,name}, record, sent_at}` with `X-CRM-Signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "t.body")>`; 10 s timeout, private addresses refused. Jobs: `crm.workflow_runs` (30 s, `SKIP LOCKED`), `crm.workflow_dates` (hourly), `crm.rescore` (6 h). Realtime: `crm:<record>`, `crm:<pipeline>`, `crm:<org>` (settings, products, scores). Notifications: `crm.mention`, `crm.task_assigned`, `crm.task_reminder`, `crm.assigned`, `crm.quote_decided`, `crm.form_submitted`, `crm.workflow`. Search: `crm.company|contact|deal|ticket`.

Agent tools (as the caller, `access.ViaAgent`): `crm_search`, `crm_get_contact|company|deal|ticket`, `crm_list_deals|tickets|activities|my_tasks|products|line_items|quotes`, `crm_get_quote`, `crm_run_report`, `crm_get_dashboard`, `crm_forecast`; writes with `Target` + exact `Preview`: `crm_update_field`, `crm_move_deal_stage`, `crm_move_ticket_status`, `crm_create_ticket`, `crm_add_note`, `crm_create_task`, `crm_log_call`, `crm_add_line_item`, `crm_update_line_item` (previews show the new deal amount), `crm_draft_quote` (a draft; publishing stays with the person), `crm_draft_mail` (a draft in the person's drafts; they send it).

## Connected platforms

A seam inside crm, `crm.Source` (the CrmSource interface in `sources.go`), is one person's connected account read and written live; `hubspot.go` and `salesforce.go` implement it and map platform objects onto the CRM's DTO shapes (`xmodel.go`: string ids, `source`). Routes resolve it once (`xroutes.go`): org routes take `?source=app|all|<connection id>`, record routes take an xref `<connection id>~<platform id>` where the app's records have UUIDs, so screens and tools never branch on the platform.

| | HubSpot (CRM v3/v4) | Salesforce (REST v62.0) |
|---|---|---|
| Capabilities | `crm.hubspot.read` (companies/contacts/deals/owners read, schemas read, sales-email-read) · `crm.hubspot.write` (+ objects write) | `crm.salesforce.read` · `crm.salesforce.write` (both `api refresh_token`; the org's profile and sharing decide) |
| Endpoints | `api` (default api.hubapi.com), `app` (UI; default from account-info `uiDomain`) | `instance` (or `api`): the org's instance URL, required |
| Read | objects search (filters, sort, `after` paging, `total`), associations v4, batch read, properties, deal pipelines, owners, engagements | SOQL with `nextRecordsUrl` batches and `totalSize`, describe, OpportunityStage, Task/Event/Note, User, parameterizedSearch |
| Board / dashboard | sums over the first 1,000 deals (`partial`), counts from search totals | aggregate SOQL (COUNT/SUM by stage), exact |
| Write | PATCH properties, dealstage, owner; POST notes/calls/tasks with v4 associations; new records | PATCH fields, StageName, OwnerId; Note, Task (Call subtype); new records (opportunities get a stage and close date) |

- **Modes** (CRM settings, `crm.manage_settings`, audited): app; connected (the app's records hidden); both (lists default to All: every source merged in one sort with a composite cursor, totals summed, sources that can't take the filters or fail listed in `skipped_sources`). Boards, dashboards and tasks show one source at a time.
- **Live, per person**: every request uses the viewer's own token through `app.Connections.Client` (after `app.WithConnectionOrg`), so owner visibility, teams, territories and sharing rules apply, totals included. A per-person cache keeps properties, pipelines, owners and account info for a minute; records are never stored. Platform validation errors come back as 422 `platform_validation` with `details.fields`; 401 → `connection_needs_auth`, 429 / Salesforce `REQUEST_LIMIT_EXCEEDED` → `connection_rate_limited`.
- **Write back**: edit fields with the CRM's property editors (custom properties from the platform's schema), board drag and the stage bar, notes/calls/tasks from the composer, owner from the platform's owners, new records, tasks marked done. Not here: deleting, merging, editing or deleting platform activities, changing associations, emails and meetings (log them on the platform).
- **Bridges**: "Import into <app>" (⋮ or bulk) copies records once, deduped by email/domain (update · skip · create), owner matched by email, company by domain; "Open in HubSpot/Salesforce" in ⋮. An app company can be linked to a platform company/account: the record page shows the platform's deals and latest activity through each viewer's own connection.
- **Search**: `SearchProvider` adds connected hits (time-boxed 2.5 s, `Context` "<org> · HubSpot · <account>", `Source`, `OrgID`); `/crm/orgs/{orgID}/search?source=` serves pickers.
- **Agents**: read tools take connected ids and `source` (`crm_search`, `crm_get_*`, `crm_list_deals`, `crm_list_activities`, new `crm_list_sources`), narrowed by the connection's scope (`app.ConnectionScope`) as well as the CRM's. Writes are separate tools per platform (`crm_hubspot_…`, `crm_salesforce_…`: `update_field`, `move_deal_stage`, `add_note`, `create_task`, `log_call`) with `Source` = the platform, Target kind `crm.external` id `<connection id>:<type>:<platform id>`, exact previews, and the connection's scope checked in Preview (suggest) and Handler (edit). Owners and team visibility (`hubspot_owner_id`, `hs_*team*`, `OwnerId`) are refused, as for the app's records.
- Out of scope: platform webhooks (reads are live), other objects (tickets, products, leads, cases), Salesforce multi-currency conversion and record types' separate sales processes (one pipeline from OpportunityStage), HubSpot multiple-object associations beyond companies/contacts/deals/engagements, agent tools that run raw SOQL.

## Screens

| Where | Web (`@/features/crm`) | Mobile |
|---|---|---|
| Nav | main nav + Home tile; tabs Dashboard · Contacts · Companies · Deals · Tickets · Tasks · Reports; ⋮ Products, Forecast and goals, Workflows, Forms, Email templates, Import, Settings | Home → CRM: search, tasks, board, lists, Tickets, Reports |
| Lists, boards | `/crm/{contacts,companies,deals,tickets}`, `/crm/deals/board`, `/crm/tickets/board`, `/crm/tasks` | lists, deals board, tickets list |
| Record | `/crm/{type}/[id]`: header (Email, Log meeting, Call; ⋮ Share…, Merge…, Delete), stage or status bar, ticket times, properties, timeline + composer, associations, line items, quotes, mail panel (drafts, recent mail to log) | Activity · About · Related; ticket status sheet; line items and quotes; compose sheet |
| Quotes | `/crm/quotes/[id]`: draft editor with live document preview; published: link, views, status | `crm/quotes/[id]`: document, share link |
| Public | `/q/[token]` quote with accept/decline and print; `/forms/[token]` (`?embed=1` for iframes) | — |
| Admin desk | `/crm/products`, `/crm/templates`, `/crm/workflows[/id|new]` with run log, `/crm/forms[/id]` with preview, embed code and submissions, `/crm/settings` (pipelines per object, properties, scoring, tags), `/crm/import` | web only |
| Reports | `/crm/reports` dashboards, `/crm/reports/[id]` with report builder, `/crm/forecast` with goals | `crm/reports`, `crm/reports/[id]` |

Every async view has skeleton, empty and error states; charts use token swatches (light and dark), keyboard focus and screen-reader text, and respect reduced motion.

## Config

No variables. No new npm or Go dependencies. Mail needs the connectors feature with Google (`gmail.readonly`, `gmail.send`) and Microsoft (`Mail.Read`, `Mail.Send`, `Mail.ReadWrite`) scopes offered through the capabilities `crm.google.mail_read|mail_send`, `crm.microsoft.mail_read|mail_send`. Imports: 5 MB / 5,000 rows; exports: 10,000 rows; 20 workflow steps, 200 workflows per workspace.

## Compliance

- Records, activities, quotes, forms and reports are org data and stay when someone deletes their account; their owner field is cleared, their name comes off authorship, history, assignments, mentions, person-valued properties, form notify lists and goals; their personal views, drafts, personal templates and goals go; workflows that ran as them turn off and forms they owned stop taking submissions; dashboards stay with their owner cleared (access). Export: activities they wrote or were assigned, records they own, views, drafts, templates, quotes they sent, workflows and forms they own.
- Mail: only messages someone logs are stored (sender, recipients, subject, body text, provider message id); recent mail is read live and never cached. Sending uses the person's own account; nothing tracks opens or clicks.
- Published quotes show the sender's name and email and the workspace name to anyone with the link; accepting keeps the buyer's typed name, email, IP and time as the record of acceptance. Unpublishing or deleting ends the link.
- Form submissions are personal data the workspace collects as controller: the consent text and tick are stored with each submission; deleting the form deletes its submissions (contacts it created stay as CRM records). Honeypot submissions are dropped unseen.
- Webhooks send record fields to a URL an admin chose; private and loopback addresses are refused.
- CSV export cells are escaped against formula injection; exports, imports, workflow and form changes are audited and rate-limited.

## Stitching

1. Choose defaults: record access (`everyone_edits`) and who holds `crm.import`/`crm.delete`/`crm.publish_quotes`/`crm.manage_workflows`/`crm.manage_forms` (Settings → Permissions).
2. Place the nav entry (web main nav + Home tile; mobile Home entry) or move CRM to a mobile tab.
3. Link to records with `/crm/{companies|contacts|deals|tickets}/<id>` and quotes with `/crm/quotes/<id>`.
4. Adjust the default pipelines (deals and the "Support pipeline" for tickets) and seeded properties to the product.
5. Calendar: `crm.SetMeetingSource(fn)` (events of the caller with any of the contact's addresses in a range → `[]crm.MeetingCandidate`) and `crm.SetBookingLinks(fn)` (the caller's booking pages); without them "Log meeting" and `{{booking_link}}` are hidden.
6. Link cards: `crm.Unfurl(ctx, viewer, path)` (or `(*crm.Service).Unfurl`) returns a `*crm.LinkCard` (kind, title, context, snippet, icon, link, time) for `/crm/<type>/<id>` and `/crm/quotes/<id>` with the record route's checks, nil when hidden; register it with a messaging feature (e.g. channels' `RegisterUnfurler("/crm/", …)`).
7. Mail: the module declares `crm.{google,microsoft}.mail_{read,send}` with HubSpot and Salesforce in `Capabilities()`, so connectors offers them on Google and Microsoft accounts; register the scopes on the OAuth apps. Google connections use a `gmail` endpoint when listed (else derived from `drive`). Gmail's read scope is a restricted scope: a production Google OAuth app needs verification and a security assessment before the "Log to CRM" list works for external users.
8. Public pages: the wire lines add `/q/(.*)` and `/forms/(.*)` to the web proxy's public paths and let `/forms/:token` be framed (`frame-ancestors *`); keep the public pages free of app chrome and secrets.
9. With the `agents` feature, per-record agent scopes inherit through `crm.team`/`crm.workspace`; nothing to wire.

## Extending

- Go: exported kinds, permission keys and notification kinds; add behaviour in this package. Every new list or aggregate must start from `visibleFrom(o)` (query.go) so totals can't include hidden rows; workflow conditions, report filters and score rules reuse the list filter builder.
- TS: `crmEndpoints`, `crmDepthEndpoints`, `useCrm…` hooks, `crmMoney`, `crmFieldText`; web `CrmRecordScreen`, `RecordPicker`, `CreateRecordDialog`, `ReportChart`, `QuoteDocument`.
- Out of scope: sequences (scheduled multi-step email with reply detection), agents sending email, open/click tracking, e-signature beyond a typed name, quote PDFs on the server (the page prints), exchange rates (totals are per currency), tickets/products/quotes on HubSpot or Salesforce, shared inboxes and email-to-ticket, per-field permissions.

## Provenance

New in the forum belt, modelled on HubSpot's CRM (objects, associations, pipelines, timeline, views, board, import). Permissions are built entirely on `access` (no parallel ACL): structural resources hold the org's record-access mode, so a mode change rewrites a handful of grants.

## Changes

- 1.3.1: package-level `Unfurl` for stitching without a Service; `Capabilities()` declares the mail capabilities, so connectors offers them.
- 1.3.0: HubSpot-depth objects and automation: support tickets (own pipelines, priority, channel, first-response and resolution times, board), products, deal line items and quotes buyers accept or decline on a public link (access `LinkPermission`), email sent and logged through each person's Gmail or Outlook (capabilities `crm.{google,microsoft}.mail_{read,send}`) with shared templates and merge fields, meeting logging and booking links through `crm.SetMeetingSource` / `crm.SetBookingLinks`, workflows (signed webhooks, run log, loop protection), lead forms with embeds, lead status and scoring, reports and dashboards (shared through access), forecast categories and goals, `(*Service).Unfurl` for link cards, 15 agent tools; migration 1902; permissions `crm.manage_products`, `crm.publish_quotes`, `crm.manage_workflows`, `crm.manage_forms`; web screens, mobile tickets/quotes/reports/compose. Needs `app.Connections` (connectors 2.1) for mail and the template's `web/next.config.ts` headers anchor for embeds.
- 1.2.0: Connected platforms: HubSpot and Salesforce through `app.Connections` (capabilities `crm.{hubspot,salesforce}.{read,write}`), the `crm.Source` seam, workspace source mode (app · connected · both) in settings, merged All lists, live per-person boards/dashboards/tasks/search, write-back, Import into the app, company links, connected agent tools, `GET /v1/crm/sources`; migration 1901; web and mobile source chips and badges from the ui kit. Needs the template's `app.Connections` contract and source UI kit (connectors 2.0 to connect accounts).

- 1.1.1: errors and agent tool descriptions name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
- 1.1.0: losing `crm.access` drops live CRM subscriptions right after the commit: `access.OnPermissionChange` (access ≥ the version that adds it) re-checks the org, pipeline and newest records' topics, and `orgs.OnMemberRemoved` the org and pipeline topics (access's own hook already covers records); before, `crm:<org>` and `crm:<pipeline>` kept delivering until the socket resubscribed. The CSV import runs on `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for it. The record page's activity column is a `<section>` (it nested a second `<main>` inside the shell's). The in-page org switcher follows `useOrgPickerVisible` (hidden when the shell has one) and “organization” in copy comes from the orgs noun helpers. Search sets `OrgID` and `Context` ("<org> · CRM") and keeps to the org a search is scoped to (`app.SearchOrg`).
- 1.0.2: `crm_update_field` refuses `owner` and `team` (403 `agent_forbidden`) in its preview, its run and an accepted suggestion: they move a record's access owner and parent, so an agent could change who can open it. Task and call previews list every field the write sets (assignee by name, reminder, priority, direction, duration, time), and a task for someone who can't see the record is refused before review. Mention, task and assignment notifications name their record (`AgentTarget`, notifications ≥ 1.1.1; task notifications now carry the record's type and id), so an agent reading the inbox doesn't see them for a record its scope hides.
- 1.0.1: structural grants (the workspace's general access, each team's edit on its crm.team) go through `access.SetGrant` / `RemoveGrant` instead of SQL on access tables, and skip deleted teams; every CRM kind sets access's `Permission: crm.access` (access ≥ 1.2.0), so someone without `crm.access` can no longer read a record's title or owner through access's sharing endpoint or `access_get_sharing`.
- 1.0.0: first release (backend, migration 1900, shared, web, mobile).
- 1.3.2: workflow editor: steps in "Then" are separated by hairlines instead of a bordered, padded box inside the card (design.md, house rule 8).
