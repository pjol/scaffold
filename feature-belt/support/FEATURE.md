# Support

## What it does

- Users open a request: category, subject, message and up to 5 attachments (web: photos, videos, PDFs, text files, 25 MB each; mobile: photos). A "Support requests" link sits on the consent screen, the suspended banner and Settings → Support, and those pages open before the Terms are accepted (consent routes, consent-exempt API and uploads); suspended accounts can use them too.
- Users see their requests and the thread live, reply, close and reopen.
- Staff work a queue (status, category, assignee, search, oldest first), reply (the user gets an email and a `support.reply` notification), keep internal notes the user never sees, assign and change status.
- Statuses: open (waiting on staff) → answered (waiting on the user) → closed. A user reply reopens; a staff reply marks answered and assigns the replier if nobody has it.
- New requests alert `SUPPORT_EMAIL`. App platform and version come from `X-Client-Platform` / `X-Client-Version`.

## Data

| Table | Purpose |
|---|---|
| `support_tickets` | One request: `number` (#1042), owner, category, subject, status, `assignee_id`, client platform/version, `last_message_at`, `closed_at`. |
| `support_messages` | The thread; `author_role` user/staff; `internal` = staff note. The first message is the request. |
| `support_attachments` | Files on a user message: storage key (purpose `support_attachment`, private, consent-exempt), name, type, size. |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/support/tickets` | user | Caller's requests, newest first; `?status`, `?cursor` |
| POST | `/v1/support/tickets` | user, consent-exempt, 5/h | `{category, subject, body, attachments: [{key, name, size}]}` → 201 detail |
| GET | `/v1/support/tickets/{id}` | owner | Thread without notes; attachment `url`s last 15 minutes |
| POST | `/v1/support/tickets/{id}/messages` | owner, consent-exempt, 30/h shared with close/reopen | `{body, attachments}` reply; reopens → 201 detail |
| POST | `/v1/support/tickets/{id}/close` · `/reopen` | owner, consent-exempt | Idempotent → detail |
| GET | `/v1/admin/support/tickets` | support | Queue: `?status`, `?category`, `?assignee=me\|none\|<uuid>`, `?user_id`, `?q` (subject, email, #number), `?sort=oldest` |
| GET | `/v1/admin/support/tickets/{id}` | support | Owner, assignee, full thread with notes |
| POST | `/v1/admin/support/tickets/{id}/messages` | support, audited | `{body, internal, close}` |
| PATCH | `/v1/admin/support/tickets/{id}` | support, audited | `{status?, assignee_id?}` (`""` unassigns; active staff only) |
| GET | `/v1/admin/support/stats` · `/assignees` | support | Queue counts · active staff for the picker |

Attachments: `POST /v1/uploads {purpose: "support_attachment", …}`, PUT the file, send the key (must be the caller's). Realtime: `support:<ticket id>` (owner and staff) carries `support.message` and `support.ticket`; `admin:live` carries `support.ticket` for new requests, replies, notes and updates. Notes are never published to the ticket topic.

Shared: `supportEndpoints`, `supportKeys`, `useMyTickets`, `useTicket` (subscribes to the topic), `useCreateTicket`, `useReplyTicket`, `useCloseTicket`, `useReopenTicket`, `useAdminTickets`, `useAdminTicket`, `useAdminReply`, `useAdminUpdateTicket`, `useSupportStats`, `useSupportAssignees`, plus `SUPPORT_*` constants and labels.

## Screens

| Where | Web | Mobile |
|---|---|---|
| List | `/settings/support-requests` (Settings section "Support requests") | `app/settings/support-requests/index.tsx` (Settings row) |
| New | `/settings/support-requests/new`: category, subject, message, files (upload on pick) | `…/new.tsx` modal: category chips, subject, message, photos (system picker) |
| Thread | `/settings/support-requests/[id]`: live thread, reply with files, close/reopen | `…/[id].tsx`: live thread, reply (text), close/reopen |
| Admin | `/admin/support` (admin tab): counts, filters in the URL, table · `/admin/support/[id]`: facts, status, assignee, thread with notes, Reply/Internal note composer, "Send and close" | — |

Registry wiring. Web: `settingsSections`, `adminNav` (badge = open requests), `consentRoutes` (`/settings/support-requests(/.*)?`), `supportLinks`. Mobile: `settingsSections`, `consentRoutes` (`settings/support-requests`), `supportLinks`. Email and notification links use `/settings/support-requests/<id>` (both apps) and `/admin/support/<id>`. Skeleton, empty and error states everywhere; checked at 375/1440, light/dark.

## Config

No new variables: core `SUPPORT_EMAIL` (alert inbox, Reply-To on reply emails) and `PUBLIC_WEB_URL` (email links). No new Go or npm dependencies.

## Compliance

- Deletion: the user's requests, messages and attachment rows are deleted; files go with their storage prefix. Requests they were assigned as staff are unassigned; their staff replies stay under the anonymised account.
- Export: requests with the user-visible thread and attachment names/types/sizes; notes excluded.
- `SUPPORT_EMAIL` is also the published contact (Apple 1.2, 5.1.1). Request text reaches the mail provider: say so in the privacy policy.

## Stitching

1. Set `SUPPORT_EMAIL` to a monitored inbox.
2. Leave `support.reply` unregistered with `notifications` (support's own email is the only one; a registered kind would add a duplicating email toggle).
3. Banned accounts can't use the API: the link on the blocked screen can't help them, so the support email next to it is their path. Point a public page there if your app needs more.
4. Change categories only with an app migration (CHECK) plus `categories` in `support.go` and `SUPPORT_CATEGORIES` in shared.

## Extending

- More staff alerts (chat, paging): next to `alertNewTicket`. Auto-close answered requests: an app `jobs.Every`.
- Never expose internal notes in a user route, event or export.

## Provenance

- STIQQ feedback (categories, statuses, admin response, attachments, alert email with a review link) and the SKILLS backend-setup rule (status, attachments, response, responder, timestamp), generalised into a thread with an author per message; DOGPATCH `accounts/feedback.go` (platform capture, validation).
- Fixed: attachments in object storage, not bytea; ownership check on every key; creation rate-limited and consent-exempt (uploads too); plain-text reply email; notes separate from replies; live threads.
- Unverified here: mobile screens on a device (typecheck with typed routes and `expo export` pass).

## Changes

- 1.0.0: consent-exempt attachments, text files, app paths under Settings; shared hooks, web and mobile screens, admin queue.
- 1.0.1: `supportLinks` and `consentRoutes` on web and mobile (requests open from the consent screen and suspended banner); open-request badge on the admin tab.
