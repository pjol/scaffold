# Calendar

## What it does

- A personal calendar: events with time zone, all-day, location, video call, colour, "show as" busy/free and what colleagues see (busy or the title). Day, week, month and agenda views on web (drag to create, move and resize; Alt+arrows on a focused event); agenda and day on mobile.
- Repeating events (daily, weekly on chosen days, monthly by date or nth weekday, yearly; every N; until a date or N times) expanded in the event's zone, so 10:00 stays 10:00 across daylight-saving changes. Edit or delete one occurrence, this and following (the series splits), or all.
- Guests: colleagues who share an organization (people picker, in-app invitation and RSVP) and anyone by email (an `.ics` invite: REQUEST, updates bump SEQUENCE, CANCEL on delete or removal, plus a tokenized reply page). Colleagues' free/busy while scheduling and "everyone's free" suggestions. Reminders per event (defaults in settings), once per person per occurrence.
- Busy time from other calendars added by their secret iCal address (fetched by the server through an SSRF guard, refreshed every 30 minutes, times only), and a private iCal feed of your calendar for other apps.
- Booking links: lengths, location (video, phone, in person, other), weekly hours plus date overrides in your zone, buffers, minimum notice, days ahead, a daily cap, start-time spacing, custom questions, confirmation message, pause. **Internal** links are bookable only by signed-in members of the link's organization; **external** links by anyone, guests included.
- Booking pages (`/book/<slug>`): free days and times only (never events) in the visitor's zone (switchable), a short form, confirmation with the video call link and add-to-calendar. Booking is race-safe; host and booker get emails with `.ics`, the event lands on both calendars, and the booker's manage link reschedules or cancels within the host's rules.
- Times people read (notifications, reminders, emails to colleagues, agent previews, search) are in the reader's zone with the zone named; the device zone is saved the first time someone is signed in (once the account is active and the Terms are accepted). Other features hand scheduling over with a new-event link that opens the event dialog prefilled.

## Data

| Table | Purpose |
|---|---|
| `calendar_settings` | zone (the device's, saved on first sign-in, or chosen; NULL until then), week start, working hours, default reminders |
| `calendar_events` | single events, series masters (`rrule`, `series_until`) and exceptions (`recurring_event_id` + `original_start`; `status = cancelled` is a deleted occurrence); `sequence` for iTIP |
| `calendar_attendees` | guests of a single event or series: `user_id`, or external `email` + sha256 of its RSVP token; response |
| `calendar_reminders_sent` | (event, occurrence, user, offset): at-most-once reminders |
| `calendar_feeds` | sha256 of each user's feed token |
| `calendar_subscriptions` / `calendar_busy_blocks` | external calendars (URL AES-GCM encrypted, host kept for display) and their busy ranges |
| `calendar_booking_links` | link settings; `org_id` required when `audience = internal`; soft-deleted |
| `calendar_bookings` | booker details, answers, buffered `block`; an exclusion constraint forbids overlapping confirmed bookings per host |
| `calendar_outbox` | invites, cancellations, notifications and room cancellations queued in the change's transaction, sent after commit with retries: a short statement claims a batch (`claim`, `next_attempt_at` as its lease), sending holds no transaction, results are recorded by claim (`2101_calendar_outbox_claims.sql`) |

Extensions `btree_gist` and `pg_trgm` (both trusted).

## API

All under `/v1/calendar`. Signed-in routes check ownership or attendance on every id; events you can't see are 404.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET / PUT | `/settings` | user / active, 60/h | zone, week start, working hours, default reminders; `video_available`, `subscriptions_available` |
| GET | `/events?from&to` | user | occurrences (≤ 62 days, ≤ 2000) + subscribed `busy` |
| POST | `/events` | active, 120/h | create (guests, `recurrence`, `conference: "add"`) |
| GET | `/events/{id}?occurrence=` | owner or guest | one occurrence with guests and replies |
| PATCH / DELETE | `/events/{id}` | organizer, 300/h | `scope` this/following/all + `occurrence_start`, `send_updates`; deleting a booked event cancels the booking (`?reason=`) |
| PUT | `/events/{id}/rsvp` | guest, 60/min | accepted / tentative / declined (whole series) |
| GET | `/events/{id}.ics` | owner or guest | download |
| GET | `/people?q=` or `?ids=` (≤ 50), `/freebusy?user_ids&from&to`, `/find-time?user_ids&from&to&duration` | user | co-members only (other ids are left out); free/busy titles only for "details" events |
| GET POST PATCH DELETE | `/subscriptions`, `/subscriptions/{id}`, POST `/subscriptions/{id}/refresh` | owner (add 20/h, refresh 12/h) | external calendars; the URL is never returned |
| GET / POST / DELETE | `/feed` | user (rotate 10/h) | status / new secret URL (shown once) / off |
| GET POST · GET PATCH DELETE | `/links` · `/links/{id}` | owner (create 30/h) | booking links (keyset list) |
| POST | `/links/preview` | user, 240/h | slots a draft link would offer (live preview) |
| GET · GET · POST | `/bookings?role=host\|booker&when=upcoming\|past\|cancelled` · `/bookings/{id}` · `/bookings/{id}/cancel` | host or booker | keyset lists; cancel (host any time, booker within rules) |
| GET | `/book/{slug}` | public + optional token, 60/min/IP | page info; internal links answer `access: "members_only"` with the org's name only |
| GET / POST | `/book/{slug}/slots`, `/book/{slug}` | public + optional token; book 10/h/IP, 60/h/link | slots (≤ 42 days); book (`members_only` 403 for non-members) |
| GET / POST | `/manage/{token}` (+ `.ics`, `/slots`, `/cancel`, `/reschedule`) | token; writes 20/h/IP | the booker's manage link |
| GET / POST | `/rsvp/{token}` | token | an external guest's invitation and reply |
| GET | `/feed/{token}.ics` | token | personal feed; token paths are redacted in logs |

Booking abuse controls: per-IP and per-link limits, 5 bookings per email per day, 3 upcoming per email per link, no self-booking, `slot_taken` (409) on races. Every public route answers an AI agent's credential (`agt_…`) with 401 `agent_credential`.

Realtime: `calendar.changed {scope}` on `user:<id>` (events, bookings, links, subscriptions). Notifications: `calendar.invite`, `.updated`, `.cancelled`, `.response`, `.reminder`, `.booking`.

Agent tools, run as the caller: `calendar_list_events`, `calendar_get_event` (Target `calendar.event`), `calendar_find_free_time`, `calendar_search_people`, `calendar_list_booking_links`, `calendar_list_bookings` (read-only) and `calendar_create_event` (write; Target kind `calendar.event`; Preview in the reviewer's zone, "Add “Title” Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST", with the whole event as the after text: when, repeat, place, guests by name, description; colleagues only, never emails anyone). Agent calls drop rows the user's agent scope hides (`calendar.event`, `calendar.booking_link`, `calendar.subscription`); `Search` (events and booking links) does the same, with `Context` "Calendar" (members-only links: `OrgID` and "Org · Calendar", left out of a search scoped to another org).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Calendar | `/calendar?view=day\|week\|month\|agenda&date=` (nav, home tile): mini month, other calendars, now line, working-hours shading, busy blocks, event sheet with RSVP and ⋮ `ResourceMenu` (edit, .ics, copy video link, delete), event dialog (repeat, guests + availability, video, reminders, colour, show as) | Home → `/calendar`: Agenda (2 weeks) and Day timeline (tap to add), ⋮ to booking links |
| New-event link | `/calendar?new=1&title=&details=&start=&end=&duration=&video=1&guests=<ids, emails>` opens the dialog prefilled on its day, then drops the parameters | the same link opens `/calendar/form` with the same parameters |
| Event | `/calendar/event/[id]?occurrence=` (notification links) | `/calendar/event/[id]`: details, RSVP, ⋮ (edit, add to another calendar, delete); `/calendar/form` modal create/edit with a scope sheet |
| Booking links | `/calendar/links` (⋮ per row: copy, open, edit, pause, delete), `/calendar/links/new`, `/calendar/links/[id]`: editor beside a live preview | `/calendar/links`: share (system sheet incl. Copy), pause |
| Bookings | `/calendar/bookings?when&role` with answers and cancel | — |
| Settings | `/settings/calendars`: zone, week start, working hours, default reminders, other calendars, feed link | — |
| Public | `/book/[slug]`, `/book/manage/[token]`, `/calendar/rsvp/[token]` (a reply from an email link needs a tap, so link scanners never answer) | shares web URLs |

Session mount `calendarZoneSync` (web and mobile) saves the device zone when there is none, once the account is active and the Terms are accepted. Every async view has skeleton, empty and error states.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `CALENDAR_SECRET_KEY` | — (required outside dev) | 32 bytes, base64: encrypts subscription URLs, signs RSVP and manage links; the published dev value is refused elsewhere |
| `CALENDAR_ALLOW_PRIVATE_URLS` | `false` (dev `true`) | subscription URLs may reach this machine; refused outside development |
| `CALENDAR_SUBSCRIPTION_INTERVAL` | `30m` | refresh interval (5m–24h); failures back off to a day |
| `CALENDAR_SUBSCRIPTION_MAX_MB` | `5` | largest subscribed file (1–50) |

Mobile adds `@react-native-community/datetimepicker` 9.1.0 (rebuild the dev client). No new Go dependencies.

## Compliance

- Deletion: future bookings they host are cancelled (bookers emailed "The host's account was closed"); future bookings they made are cancelled (host told); guests of their upcoming events get cancellations; then their events, links, bookings as host, subscriptions, feed, settings and invitations are deleted. Bookings they made on others' links stay with the host, anonymised ("Deleted user").
- A host who cancels (or deletes a booked event): the booker gets a CANCEL email with the host's note and "Book another time" while the link is open.
- Export: settings, events with guests, invitations, links, bookings (both sides), subscriptions (host only, never the secret URL), feed date.
- Booking emails go to addresses visitors type (limits above). Privacy policy: subscribed URLs are fetched by the server; name the mail provider.

## Stitching

1. Video: implement `calendar.Conferencing` over the meetings feature and call `calendar.SetConferencing` from stitching; honour `ConferenceRequest.Audience` (external: guests may join; internal: members of `OrgID`, or the host and `ParticipantIDs`). Without it, video calls aren't offered.
2. Set `PUBLIC_WEB_URL` (booking, manage and RSVP links in email). `/book/(.*)` and `/calendar/rsvp/[^/]+` are wired into `web/proxy.ts`.
3. Generate `CALENDAR_SECRET_KEY` per environment; rotating it breaks emailed links and makes stored subscriptions unreadable (people re-add them).
4. Decide who appears in the guest picker and free/busy (default: people sharing an organization) and who may create internal links (default: any member).
5. Name booking links in product copy; keep Calendar in the main nav (default) or Home only.
6. Mobile: add a Calendar tab if scheduling is a primary destination (default: a Home entry).
7. Scheduling started elsewhere (a meetings feature's Schedule, a channel's "Schedule a meeting") can open `calendarNewEventLink({ title, guests, video: true })` so it lands on calendars with invitations; give other features people's zones with `calendar.UserZone` (e.g. `calls.SetUserZone`) and, on the client, the settings' zone (`useCalendarSettings`: `timezone` when `timezone_set`, e.g. into `setCallsTimeZone`) so they show times on the calendar's clock. Product words: "Video call" is the location type; other features say Start call (instant) and New meeting (a link).

## Extending

- `calendar.SetConferencing(c Conferencing)` with `Create(ctx, ConferenceRequest) (Conference, error)`, `Update(ctx, id, ConferenceRequest) (Conference, error)`, `Cancel(ctx, id) error`; `calendar.VideoAvailable() bool`. `ConferenceRequest.EventID` names the event a room is for (link back to `/calendar/event/<id>`; a series' occurrences may share it, so it isn't a unique key).
- `calendar.UserZone(ctx, q, userID) (*time.Location, bool)`: the person's zone (false until they have one).
- Shared: `calendarNewEventLink(draft)`, `parseCalendarNewEventLink(params)`, `calendarDraftTimes`, `calendarDraftGuests`, `useCalendarPeopleByIds(ids)`, `useCalendarDeviceZoneSave()`.
- Out of scope: Google/Microsoft OAuth sync, iTIP REPLY by mail (replies use the RSVP link), round-robin or collective booking links, paid bookings, RRULE parts beyond the subset (BYSETPOS, hourly), per-occurrence guest lists (guests are series-wide), Windows zone names in subscriptions beyond the mapped set (others fall back to the file's standard offset).
- Don't store subscription URLs or tokens in plain text; never put event details on booking pages; never save from new-event link parameters (anyone can craft one): they only prefill the dialog the person confirms.

## Provenance

New module. Recurrence follows Google Calendar's master/exception model; the SSRF guard follows OLEARY/crm `integration/httpsafe.go` (dial-time resolved-IP checks, no proxy); iCal writing extends `events` (folding, escaping) with VTIMEZONE and iTIP methods. Verified on dev-up: curl and a Python client for every flow, a Go client for agent scope (fake policy) and cross-user tools/search, Playwright at 375/1440 in light and dark; mobile by typecheck and `expo export`.

## Changes

- 1.0.0: first release.
- 1.1.0: times in notifications, reminders, colleague emails, search and the `calendar_create_event` preview use the reader's zone (else the event's), never a silent UTC; all-day dates no longer shift with the reader's zone; the device zone is saved on first sign-in (`calendarZoneSync` session mount: new wire lines on web and mobile); new-event links (`calendarNewEventLink`, web dialog and mobile form); `GET /people?ids=`; `calendar.UserZone`; `ConferenceRequest.EventID`.
- 1.1.1: the `calendar_create_event` suggestion shows the whole event the person accepts (when, repeat, place, guests by name, description) and refuses guests who aren't colleagues before review; its `Target` names `calendar.event`; feed, manage and RSVP token routes (and booking routes) refuse AI agent credentials. Event notifications name their event (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for an event its scope hides.
- 1.1.2: the outbox claims rows in one short statement (`FOR UPDATE SKIP LOCKED`, a claim id, `next_attempt_at` as a lease), sends with no transaction or connection held, and records each result by claim, so a rollback never re-sends what went out and a crashed sender's rows come due when the lease ends (migration `2101_calendar_outbox_claims.sql`). The public booking confirmation links the video call ("Join video call", the manage page's words). The session zone save waits for an active account with the Terms accepted (no 403 for new accounts). Search hits carry `Context` (and `OrgID` for members-only links) and honour `app.SearchOrg` (template `SearchResult.Context`). Copy names organizations with orgs' noun (`orgsNoun`…, orgs with `configureOrgsNoun`). A new link's live preview no longer fails (400) before a title is typed.
- 1.1.3: errors and agent tool descriptions name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
