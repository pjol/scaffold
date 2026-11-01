# Moderation

## What it does

- Anyone signed in reports a person or any registered content (chat message, post, comment, AI reply) with a reason (spam, harassment, hate, sexual, violence, self_harm, illegal, other) and an optional note. What was reported is captured at report time. The report dialog can block the author in the same step.
- Reporting the same thing twice updates the first report; reports from different people on the same target collect into one case.
- Block anyone: their content disappears for both people and messages stop in both directions. Settings → Safety lists blocked people (unblock there) and your reports with a generic outcome.
- Text is checked before it is posted: a blocked-terms list (ignores case, accents and swaps like `h3ll0`) plus an optional OpenAI moderation call.
- Staff work a queue of open cases, oldest first with their age against a 24-hour target: dismiss, remove the content, warn, suspend or ban the author, or resolve with a note. Reporters hear that their report was reviewed, never which action.
- Optional auto-hide: content disappears once N different people report it, until staff decide.

## Data

| Table | Purpose |
|---|---|
| `moderation_blocks` | blocker → blocked, PK on the pair, reverse index for either-direction checks |
| `moderation_cases` | one per reported target while open: snapshot from the first report, distinct-reporter count, hidden state, staff action and note |
| `moderation_reports` | one per reporter per case: reason, note, the snapshot that reporter saw, whether they were told the outcome |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST | `/v1/moderation/reports` | user, 20/h, consent-exempt | `{target_kind, target_id, reason, note}`; `"user"` reports a person. 201 new, 200 updated your earlier report |
| GET | `/v1/moderation/reports` | user | your reports, `outcome` open, actioned or closed |
| GET | `/v1/moderation/blocks` | user | people you blocked |
| GET | `/v1/moderation/blocks/ids` | user | `{user_ids}` you blocked (never who blocked you), up to 5000 |
| POST | `/v1/moderation/blocks` | user, 60/h, consent-exempt | `{user_id}`; idempotent |
| DELETE | `/v1/moderation/blocks/{user_id}` | user | unblock; idempotent |
| GET | `/v1/admin/moderation/summary` | support | open, over SLA, oldest open age, closed and median close time over 7 days |
| GET | `/v1/admin/moderation/cases` | support | `?status=open\|resolved\|dismissed\|closed&kind=&author_id=`; open oldest first |
| GET | `/v1/admin/moderation/cases/{id}` | support | case, every report with reporter and snapshot, author history |
| POST | `/v1/admin/moderation/cases/{id}/actions` | support | `{action, note, remove_content, notify_reporters}`; audited |

Actions: `dismiss` (restores auto-hidden content when the target supports it), `remove` (`Target.Remove` in the staff transaction), `warn`, `suspend`, `ban` (author's `users.status`; only admins act on staff; nobody on themselves; `remove_content: true` also takes content down), `resolve`. Each writes `moderation.case.<action>` to the audit log, plus `user.suspend` or `user.ban`.

Realtime: `moderation.case_opened` / `moderation.case_closed` on `admin:live`; `moderation.blocks_changed` on the blocker's `user:` topic. Notifications: `moderation.outcome` to reporters (link `/settings/safety`), `moderation.notice` to authors. Not registered with notifications (moderation doesn't require it): they deliver with push on, email off.

Agent tools: `moderation_list_blocks`, `moderation_list_reports`, `moderation_block_user` (write), `moderation_queue_summary` (staff).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Settings → Safety | `/settings/safety`: blocked people (Unblock), your reports with outcome badge | `settings/safety`: same, pull to refresh |
| Admin → Moderation | `/admin/moderation`: queue health, Open/Closed (`?status=`), table with kind, author, top reason, reports, waiting time (red over 24 h); `/admin/moderation/[id]`: snapshot as captured, media, every report, author history, action dialog with note, "Also remove the content", "Tell reporters" | none (admin is web only) |
| Anywhere a feature shows people or content | `ReportDialog`, `ReportButton`, `BlockDialog`, `BlockButton` | same names |

Every list has skeleton, empty and error states. Shared hooks in `packages/shared/src/moderation` (`useCreateReport`, `useBlockedIds`, `useIsBlocked`, `useBlockUser`, `useUnblockUser`, admin hooks); block changes are optimistic.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `MODERATION_BLOCKED_TERMS` | empty | comma-separated terms refused before posting |
| `MODERATION_PROVIDER` | `none` | `none` or `openai` (anything else fails the boot) |
| `OPENAI_API_KEY` | — | required when the provider is `openai` |
| `MODERATION_AUTOHIDE_REPORTS` | `0` | distinct reporters that hide content; 0 = off |
| `MODERATION_NOTIFY_REPORTER` | `true` | tell reporters their report was reviewed |

No web or mobile variables, no npm dependencies. Go: `golang.org/x/text` (already in the template's graph) becomes direct.

## Compliance

- Covers Apple 1.2 and Play UGC: filter before posting, report content and people, block both ways, a queue with a 24-hour target, AI output reportable (`assistant.message`).
- Deletion: the user's reports stay with `reporter_id` cleared; cases and reports about their content keep their shape with snapshot text and media scrubbed, and open ones close; every block involving them is deleted.
- Export: the user's reports (without the reported content) and the people they blocked.
- Reports and blocks skip the consent gate and work for suspended accounts. The privacy policy names OpenAI when the provider is on.

## Stitching

1. Write the community rules and link them from the Terms; review the reason labels in `shared/types.ts`.
2. Choose `MODERATION_AUTOHIDE_REPORTS` (3 for a public feed, 0 for small private groups).
3. Name who works the queue and how they hear of new cases (`admin:live` is realtime; add an email or chat alert if nobody watches the console).
4. Fill `MODERATION_BLOCKED_TERMS`; set `MODERATION_PROVIDER=openai` for public UGC.
5. Add report and block entry points on profiles and anywhere no feature covers (avatars, member lists).

## Extending

**Go** (features that require moderation):

```go
moderation.IsBlocked(ctx, q, a, b)        // either direction
moderation.BlockedIDs(ctx, q, viewer)     // filter: author_id <> ALL($n)
moderation.Check(ctx, text)               // *httpx.Error 422 content_rejected
moderation.RegisterTarget("feed.post", t) // in New; t: Snapshot + Remove (+ optional Restorer)
```

`Snapshot` returns `{author_id, text, media_keys, link}`, or an error wrapping `pgx.ErrNoRows`/`httpx.ErrNotFound`. `Remove` soft-deletes in the given transaction. `Restorer.Restore` lets a dismissal undo an auto-hide.

**Web** (`import { … } from "@/features/moderation"`):

| Component | Props |
|---|---|
| `ReportDialog` | `open`, `onOpenChange(open)`, `targetKind` (`"user"` or a registered kind), `targetId`, `noun?` ("message" → "Report this message"), `author?: { id, name }` (adds "Also block"), `onReported?({ blocked })` |
| `ReportButton` | the dialog's props minus `open`/`onOpenChange`, plus `variant?: "icon" \| "button"`, `size?` |
| `BlockDialog` | `open`, `onOpenChange(open)`, `userId`, `name`, `onBlocked?` (confirm, then block) |
| `BlockButton` | `userId`, `name`, `size?`, `variant?: "secondary" \| "outline" \| "ghost"`, `className?`, `onBlocked?`, `onUnblocked?`; shows Unblock when blocked, nothing for yourself |

**Mobile** (`import { … } from "@/features/moderation"`): the same four with native conventions: `open` + `onClose()` instead of `onOpenChange`, `ReportDialog` is a bottom sheet with `onDismissed?` (start the next modal from there), `ReportButton` is a flag icon button, `BlockButton` takes `fullWidth?` instead of `size`/`className`.

Hide blocked people client-side with `useBlockedIds()` (e.g. drop realtime events). Don't change `contract.go`; other features compile against it. Another classifier: implement `moderation.Provider` and add it to the switch in `New`.

## Provenance

- DOGPATCH `internal/accounts/reports.go`, `internal/admin/{reports,moderation}.go`: dedupe per reporter, rate limit counted in the database, staff resolution with suspend/ban rules, generic reporter outcome.
- ASSEMBLY `0021_blocks_reports.sql` and its block/report UI: fixed reports that were written and never read (now a queue with actions), only people being reportable (now any registered content), blocks not enforced everywhere (one `IsBlocked`/`BlockedIDs` contract), no per-post report entry (shared `ReportDialog`).
- SFLuv bulletin safety: auto-hide after N reports, recorded moderation actions. Fixed: its admin routes had no client; the queue is first-class on web.

Unverified: mobile screens were typechecked and bundled (`expo export`), not driven on a device.

## Changes

- 1.0.0: first release (backend, shared hooks, web and mobile UI).
