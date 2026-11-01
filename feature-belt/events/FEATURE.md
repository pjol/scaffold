# Events

## What it does

- People create events: title, details, start and end (UTC plus an IANA timezone for display), place with optional coordinates, online link, cover image, optional capacity, visibility public, members or invite.
- The creator hosts and can add up to 10 co-hosts; hosts edit everything, including visibility.
- RSVP going, maybe or declined. Going on a full event joins the waitlist; when a spot frees, the first person waiting moves to going and is notified.
- Attendee lists are visible to hosts and attendees only, never on public pages.
- Reminders before the start (default 24 hours and 1 hour), cancellation and change notices to attendees.
- Calendar export: one event as `.ics`, and a secret per-user feed URL that calendar apps subscribe to.

## Data

| Table | Purpose |
|---|---|
| `events` | the event; `host_id` is the creator; `status` scheduled/cancelled; `invite_code` for the invite link; `sequence` for iCalendar updates |
| `event_cohosts` | co-hosts |
| `event_invites` | who may see an invite-only event |
| `event_rsvps` | one per user per event; `waitlisted_at` orders the queue |
| `event_reminders_sent` | one row per user per event per offset (at-most-once reminders) |
| `event_calendar_feeds` | sha256 of each user's calendar feed token |
| `event_outbox` | notifications queued inside account deletion, sent after commit |

## API

All under `/v1`. Lists are `{items, next_cursor}`, keyset on `(starts_at, id)`.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/events?when=upcoming\|past&filter=all\|hosting\|attending&from&to&cursor` | user | events the caller can see; from/to (≤100 days) for calendars |
| POST | `/events` | active, 20/h | create |
| GET | `/events/{id}` | can see | event with counts, co-hosts, `my_rsvp`, `host_role` |
| PUT | `/events/{id}` | host, 60/h | replace editable fields; attendees told when time or place changes |
| POST | `/events/{id}/cancel` | creator | cancel; attendees told |
| PUT / DELETE | `/events/{id}/rsvp` | can see, 60/min | `{status: going\|maybe\|declined}` / withdraw |
| GET | `/events/{id}/attendees?status=going\|maybe\|waitlisted\|declined` | hosts; attendees (not declined) | attendee list |
| POST / DELETE | `/events/{id}/cohosts`, `/events/{id}/cohosts/{user_id}` | creator (a co-host may remove themselves) | co-hosts |
| GET / POST | `/events/{id}/invites` | host | list / invite `{user_ids}` (≤100) |
| DELETE | `/events/{id}/invites/{user_id}` | host | withdraw (invite-only: drops their RSVP) |
| POST / DELETE | `/events/{id}/invite-link` | host | new code `{code, path}` / disable |
| POST | `/events/join` | active | `{code}` adds the caller to the invite list |
| GET | `/events/{id}.ics` | can see | download one event |
| GET / POST / DELETE | `/events/calendar-feed` | user | status / new secret URL (shown once) / disable |
| GET | `/events/feed/{token}.ics` | token | calendar feed: hosting, going, maybe, waitlisted; ended up to 30 days |
| GET | `/events/public`, `/events/public/{id}`, `/events/public/{id}.ics` | none | upcoming public events, no attendee data |
| GET | `/admin/events?q&host_id&status` | support | all events |
| POST | `/admin/events/{id}/cancel` | support, audited | cancel `{reason}` |

Realtime: topic `events:<event id>`, allowed for anyone who can see the event. `events.updated {event_id, status}`, `events.rsvp {event_id, going_count, maybe_count, waitlist_count, is_full}`.

Notifications: `events.invite`, `events.reminder`, `events.cancelled`, `events.updated`, `events.promoted` (push on), `events.rsvp` (to the creator, push off).

Agent tools: `events_list_upcoming`, `events_get` (read-only).

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/events` (nav, home tile) | Upcoming/Past tabs (`?when=`), All/Hosting/Going filter, cards with date block, time in the event's zone, place, counts and badges, New event |
| Web | `/events/[id]` | cover, when/where/online link, details, RSVP (going/maybe/can't go, waitlist place, Leave waitlist), who's coming sheet with status tabs, hosts, Add to calendar (.ics), host menu (edit, invite link copy/rotate/disable, cancel) |
| Web | `/events/new`, `/events/[id]/edit` | title, details, start/end edited in the chosen time zone, place, online link, visibility, capacity, cover upload |
| Web | `/events/join?code=` | joins an invite-only event and opens it |
| Web | `/settings/calendar` | calendar feed: create (URL shown once, copy), replace, turn off |
| Web | `/e/[id]` (public) | public events only: details, host name, .ics link, Sign in to RSVP; no attendee data |
| Web | `/admin/events` | all events: search, status filter, cancel with reason (audited) |
| Mobile | Home entry → `/events`, `/events/[id]`, `/events/[id]/attendees`, `/events/form` (modal, `?id=` edits), `/events/join`, Settings → `/settings/calendar-feed` | the same flows; native date/time pickers; Add to calendar writes the .ics and opens the share sheet |

Every view has skeleton, empty and error states; `events:<id>` realtime keeps counts and the viewer's RSVP current while a detail screen is open.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `EVENTS_REMINDER_OFFSETS` | `24h,1h` | reminder times before the start (1m to 720h); a bad entry fails the boot |

Storage purpose `event_cover` (public-read images). Time zones: embedded `time/tzdata` on the server, `Intl` on clients (`timeZoneOptions`, `toZonedInput`, `fromZonedInput`). Mobile adds `@react-native-community/datetimepicker` 9.1.0 (native: rebuild the dev client). No new Go dependencies. The calendar feed path is redacted in request logs (`httpx.RedactPathPrefix`).

## Compliance

- Account deletion removes the user's RSVPs (waitlists promote), co-host roles, invites, reminders and calendar feed. Events they created that haven't ended are cancelled and attendees told; all their events keep title and times but lose details, place, links and cover.
- Export: events created, RSVPs, co-host roles, invites.
- Public routes never expose attendees or counts.

## Stitching

1. Who may create events: any active user by default; restrict it in `createHandler` (staff, org admins) if needed.
2. Default visibility for new events (members) and where public events are linked from (`/e/<id>` is wired into the `proxy.ts` public paths).
3. Hosts add co-hosts and invitees by user id (API) or share the invite link; give hosts a people picker if the app has a directory.
4. Mobile invite links use `EXPO_PUBLIC_WEB_URL` (`<web>/events/join?code=`) and fall back to the `<scheme>://` app link when it is unset.
5. Bucket policy: `event_cover/` public-read.

## Extending

- Recurrence is not modelled; create one event per occurrence (ASSEMBLY's read-time expansion is the reference if needed).
- Paid tickets belong to the payments feature: gate `PUT /rsvp` through it rather than adding price columns here.

## Provenance

Distilled from ASSEMBLY's events/RSVP and SFLuv's volunteer events (capacity enforced with the event row locked, idempotent cancellation). Fixed: only hosts and staff change visibility (any ASSEMBLY member could make an event public, exposing its attendee list); attendee lists are never public; reminders are at-most-once per user per offset across instances (ASSEMBLY's unconditional update double-sent with two replicas); waitlist with automatic promotion; keyset pagination with an id tie-breaker.

Verified: backend build/vet and curl of every route; web flows in Chromium at 375 and 1440, light and dark (list, detail, waitlist and promotion over realtime, create, .ics download, cancel, calendar feed, public page signed out, admin), console clean; mobile typecheck and `expo export --platform ios`. Not verified on a device: the mobile screens at runtime, the native date/time pickers, modal presentation of `/events/form`.

## Changes

- 1.0.1: mobile invite links use `EXPO_PUBLIC_WEB_URL` when set, else the app link.
- 1.0.0: web and mobile UI, shared client and time-zone helpers, feed path redacted in logs, `config.Problem` for bad offsets, public `/e/<id>` page.
- 0.1.0: backend.
