# Calendar

## What it does

- Calendars: a primary calendar per person plus more of their own (Work, Personal) and team calendars, each shared through access at **free/busy · all details · make changes · manage** (`calendar.calendar`); colleagues' and teams' calendars are added beside yours with their own colour and visibility. Events have time zone, all-day, location, video call, colour, "show as" busy/free and what colleagues see (busy or the title). Day, 3-day, week, month, year and agenda views on web (drag to create, move and resize; Alt+arrows; Google's keyboard shortcuts `c t j k d x w m y a / e Delete ?`), a second time zone and world clocks, event search, duplicate; agenda, day, 3-day and month on mobile.
- Out of office and focus time: busy blocks that decline invitations to one-off events (new ones, or also existing ones) with the person's message; focus time tells the stitching (Do Not Disturb). Working hours and where people work each weekday show in find a time. An opt-in morning agenda email (6 AM local) lists the day.
- Find a time: you and each guest on one timeline with their zone, working hours, location, out of office and focus, and suggestions inside everyone's working day. Rooms and resources per workspace (capacity, features, free/busy) booked with events, race-safe, who may book set through access (`calendar.room`). Guests reply with a note and propose a new time the organizer accepts.
- Repeating events (daily, weekly on chosen days, monthly by date or nth weekday, yearly; every N; until a date or N times) expanded in the event's zone, so 10:00 stays 10:00 across daylight-saving changes. Edit or delete one occurrence, this and following (the series splits), or all.
- Guests: colleagues who share an organization (people picker, in-app invitation and RSVP) and anyone by email (an `.ics` invite: REQUEST, updates bump SEQUENCE, CANCEL on delete or removal, plus a tokenized reply page). Colleagues' free/busy while scheduling and "everyone's free" suggestions. Reminders per event (defaults in settings), once per person per occurrence.
- Busy time from other calendars added by their secret iCal address (fetched by the server through an SSRF guard, refreshed every 30 minutes, times only), and a private iCal feed of your calendar for other apps.
- Booking links: lengths, location (video, phone, in person, other), weekly hours plus date overrides in your zone, buffers, minimum notice, days ahead, a daily cap, start-time spacing, questions (short/long text, email, phone, one or several choices, checkbox), confirmation message or redirect, reminder emails to bookers, guests the booker adds, one-booking links, pause, embed code. **Internal** links are bookable only by signed-in members of the link's organization; **external** links by anyone. **Team links** belong to an access team: **round robin** (one free host per booking: priority, then fewest recent bookings for their weight, then least recently booked; reassignable) or **collective** (only times every host is free; the event is on every host's calendar), with per-host hours.
- Booking pages (`/book/<slug>`): free days and times only (never events) in the visitor's zone (switchable), a short form, confirmation with the video call link and add-to-calendar. Booking is race-safe; host and booker get emails with `.ics`, the event lands on both calendars, and the booker's manage link reschedules or cancels within the host's rules.
- Times people read (notifications, reminders, emails to colleagues, agent previews, search) are in the reader's zone with the zone named; the device zone is saved the first time someone is signed in (once the account is active and the Terms are accepted). Other features hand scheduling over with a new-event link that opens the event dialog prefilled.
- Connected calendars (Google Calendar, Outlook) appear natively in every view, under "Connected" in the sidebar per account, with their own colour, visibility and "counts for availability"; their events can be created, moved, resized, edited, deleted and answered from here, and "Video call" offers the app's own call, Google Meet, Teams or Zoom (see Connected platforms).

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
| `calendar_outbox` | invites, cancellations, notifications, room cancellations and booking-copy moves/deletes queued in the change's transaction, sent after commit with retries: a short statement claims a batch (`claim`, `next_attempt_at` as its lease), sending holds no transaction, results are recorded by claim (`2101_calendar_outbox_claims.sql`) |
| `calendar_connected_calendars` | per person and connected calendar: colour, hidden, busy (NULL = default) |
| `calendar_calendars` / `calendar_calendar_prefs` | calendars (owner, workspace, team, colour, primary, `counts_busy`; access kind `calendar.calendar`) · each person's colour, hidden, added |
| `calendar_rooms` / `calendar_event_rooms` | workspace rooms and resources (access kind `calendar.room`, admins manage) · rooms an event (whole series) books |
| `calendar_link_hosts` / `calendar_booking_hosts` | team links' hosts (priority 0–4, weight, own hours, active) · collective bookings' other hosts |
| `calendar_booking_reminders_sent` / `calendar_focus_signals` | at-most-once booker reminders · focus starts handed to the hook |
| `calendar_connected_conferences` | rooms the wired conferencing made for connected events (to move or close them) |

`2102_calendar_connected.sql` adds those, `write_connection_id`/`write_calendar_id`/`conference_provider`/`conference_connection_id` on links and `ext_*`/`conference_provider` on bookings. Platform events are never stored. `2103_calendar_depth.sql` adds the rest: events' `calendar_id` (NULL = the owner's primary, filed when it's made), `event_type`, `auto_decline`, `decline_message`; attendees' `note`, `proposed_start/end`; links' `kind`, `team_id`, `single_use`/`used_at`, `redirect_url`, `booker_reminders`, `allow_guests`; bookings' `guests`, `assignment`; settings' `work_locations`, `secondary_timezone`, `world_clocks`, `agenda_email`/`agenda_sent_on`.

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
| GET | `/people?q=` or `?ids=` (≤ 50), `/freebusy?user_ids&from&to`, `/find-time?user_ids&from&to&duration` | user | co-members only (other ids are left out); free/busy titles only for "details" events, block `kind` (busy, out_of_office, focus), each person's zone, working hours, work locations; find-time stays inside every attendee's hours |
| POST / DELETE · POST | `/events/{id}/propose` · `/events/{id}/proposals/{attendee}` | guest, 30/h · can edit | propose a time for a one-off event (`note`) / withdraw · `{accept}` moves the event (guests told) or keeps it |
| GET | `/search?q=` | user, 120/min | events (own, invited, calendars shared with details), next occurrence |
| GET POST · PATCH DELETE · PUT | `/calendars` (`?org_id=` for a new primary) · `/calendars/{id}` · `…/prefs` | user · manage · viewer | yours + added + `shared`; create in a workspace (`team_id`: the team may edit) · rename, colour, `counts_busy` / delete (guests told) · your colour, hidden, added |
| GET POST · PATCH DELETE | `/rooms?org_id&from&to&capacity&q&exclude_event` · `/rooms/{id}` | member · `calendar.manage_rooms` or manage (audited) | rooms you can see with busy times and `free`, `can_create` · change, retire |
| POST | `/bookings/{id}/reassign` | link maker, team lead or host, 60/h | hand a round-robin booking to another free host |
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
| GET | `/sources?org_id=` | user | `FeatureSources` + each account's calendars and `conferencing` (meeting providers) |
| GET | `/connected/events?from&to&source=` | user, 240/min | events of visible connected calendars (≤ 62 days) + per-source `errors` |
| PATCH | `/connected/{conn}/calendars/{cal}` | active | `{color, hidden, busy}` |
| POST · GET PATCH DELETE · PUT | `/connected/{conn}/calendars/{cal}/events` · `…/events/{event}` · `…/rsvp` | active (write capability) | create (guests, `recurrence`, `conference: "add"` + `conference_provider`) · read / change (`scope` this\|all) / delete (`scope`) · reply |

Events take `calendar_id` (yours, a team's, or one shared with you at "make changes": the calendar's owner organizes), `event_type`, `auto_decline`, `decline_message` and `room_ids` (409 `room_busy` with the clash); RSVP takes `note`. Sharing levels: view = free/busy (events come back `redacted`: "Busy" unless shown to colleagues, no place or link; 404 on open), comment = details, edit = create/change/delete, manage = also sharing (access's grant routes; agents capped at edit). Links take `kind`, `team_id`, `hosts` (team members), `single_use`, `redirect_url`, `booker_reminders`, `allow_guests`; team links use the app's own video calls and no connected copy; a team's leads and the maker edit them, members see them. Booking abuse controls: per-IP and per-link limits, 5 bookings per email per day, 3 upcoming per email per link, no self-booking, ≤ 10 guests, `slot_taken` (409) on races (each host's lock is taken, a team link locks its whole pool and re-picks inside the transaction), 410 `link_used`. Every public route answers an AI agent's credential (`agt_…`) with 401 `agent_credential`.

Realtime: `calendar.changed {scope}` on `user:<id>` (events, bookings, links, subscriptions, calendars; a shared calendar's watchers and a team's members too). Notifications: `calendar.invite`, `.updated`, `.cancelled`, `.response`, `.reminder`, `.booking`.

Agent tools, run as the caller: `calendar_list_events` (`source`: all, app or a connection id; connected events under `connected`), `calendar_list_sources`, `calendar_get_connected_event`, `calendar_get_event` (Target `calendar.event`), `calendar_find_free_time`, `calendar_search_people`, `calendar_list_booking_links`, `calendar_list_bookings`, `calendar_list_calendars`, `calendar_list_rooms` (read-only; free/busy only for what the person may see), `calendar_book_room` (Target `calendar.event` id; "Book “Board room” for “Design review” …", rooms before/after), `calendar_create_time_block` (out of office or focus; Target kind `calendar.event`; "Add out of office Fri, Nov 13 (all day): declines new invitations"), `calendar_propose_time` (Target the event; proposed vs current time) and `calendar_create_event` (write; Target kind `calendar.event`; Preview in the reviewer's zone, "Add “Title” Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST", with the whole event as the after text: when, repeat, place, guests by name, description; colleagues only, never emails anyone). Agent calls drop rows the user's agent scope hides (`calendar.event`, `calendar.booking_link`, `calendar.subscription`); `Search` (events and booking links) does the same, with `Context` "Calendar" (members-only links: `OrgID` and "Org · Calendar", left out of a search scoped to another org).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Calendar | `/calendar?view=day\|3day\|week\|month\|year\|agenda&date=` (nav, home tile): mini month, My calendars (show/hide in colour, ⋮ Share… with calendar levels, Settings, Color, Remove/Delete; + New calendar, Shared with you), world clocks, now line, second-zone gutter, working-hours shading, busy blocks, search (`/`), shortcuts (`?`), event sheet with RSVP + note, Propose a new time, guests' notes and proposals (Accept / Keep time), rooms, ⋮ (edit, duplicate, .ics, copy video link, delete); event dialog (Event / Out of office / Focus time, calendar, repeat, guests + Find a time, rooms, video, reminders, colour, show as); `?add_calendar=<id>` adds a shared calendar | Home → `/calendar`: Agenda, Day, 3 days, Month (dots; tap a day), hidden calendars and colours applied, ⋮ Out of office, Focus time, booking links |
| New-event link | `/calendar?new=1&title=&details=&start=&end=&duration=&video=1&guests=<ids, emails>` opens the dialog prefilled on its day, then drops the parameters | the same link opens `/calendar/form` with the same parameters |
| Event | `/calendar/event/[id]?occurrence=` (notification links) | `/calendar/event/[id]`: details, rooms, calendar, RSVP, Add a note, Propose a new time, proposals (Accept / Keep time), ⋮ (edit, add to another calendar, delete); `/calendar/form` modal create/edit (`event_type=` starts a block): kind, decline options, calendar, rooms sheet, "Everyone's free" suggestions, scope sheet |
| Rooms | `/calendar/rooms`: rooms with today's free/busy, New room and ⋮ Edit, Who can book…, Delete for managers | booked from the form |
| Booking links | `/calendar/links` (yours and your teams'; team and one-booking badges; ⋮ copy, open, copy embed code, edit, pause, delete), `/calendar/links/new`, `/calendar/links/[id]`: editor (Hosts: Just me / Round robin / Everyone, team, hosts with priority, weight, taking bookings) beside a live preview | `/calendar/links`: share, pause, team badge and hosts |
| Bookings | `/calendar/bookings?when&role` with answers, guests, assignment, Hand over (round robin) and cancel | — |
| Settings | `/settings/calendars`: zone, week start, second zone, world clocks, working hours, where you work, default reminders, morning agenda, other calendars, feed link | — |
| Connected | sidebar "Connected" (`ConnectPrompt` when none): accounts with status/Reconnect and ⋮ AI agent access, calendars with visibility checkbox in their colour and ⋮ colour / counts for availability (a sheet from the header ⋮ below 1024 px); `SourceBadge` in agenda rows and the event sheet, provider marks on grid and month chips, `SourceFilter` over the agenda; event sheet with Join on Meet/Teams/Zoom, RSVP, ⋮ Open in Google Calendar/Outlook, Edit, Delete; event dialog "Calendar" picker and "Video call" provider; link editor "Add bookings to" and "Video call with" | agenda and day show connected events with badges; `/calendar/connected`: details, Join, RSVP, Open in …, delete; the form's Calendar and Video call with rows |
| Public | `/book/[slug]` (team hosts, guests, every question type, redirect), `/book/[slug]?embed=1` → `/book/[slug]/embed` (no frame; posts `{type: "scaffoldapp:booking", height \| booked}` to the parent; the only page with `frame-ancestors *`), `/book/manage/[token]`, `/calendar/rsvp/[token]` (a reply from an email link needs a tap, so link scanners never answer) | shares web URLs |

Session mount `calendarZoneSync` (web and mobile) saves the device zone when there is none, once the account is active and the Terms are accepted. Every async view has skeleton, empty and error states.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `CALENDAR_SECRET_KEY` | — (required outside dev) | 32 bytes, base64: encrypts subscription URLs, signs RSVP and manage links; the published dev value is refused elsewhere |
| `CALENDAR_ALLOW_PRIVATE_URLS` | `false` (dev `true`) | subscription URLs may reach this machine; refused outside development |
| `CALENDAR_SUBSCRIPTION_INTERVAL` | `30m` | refresh interval (5m–24h); failures back off to a day |
| `CALENDAR_SUBSCRIPTION_MAX_MB` | `5` | largest subscribed file (1–50) |

Mobile adds `@react-native-community/datetimepicker` 9.1.0 (rebuild the dev client). No new Go dependencies.

## Connected platforms

| Capability | Scopes | Gives |
|---|---|---|
| `calendar.google.read` · `calendar.google.write` | `calendar.readonly` · + `calendar.events` | calendars, events, free/busy · create/change/delete, RSVP, Meet links |
| `calendar.microsoft.read` · `calendar.microsoft.write` | `Calendars.Read` · `Calendars.ReadWrite` | calendars, events, getSchedule · create/change/delete, RSVP, Teams via `isOnlineMeeting` |

- **Live, per person.** Adapters build URLs from `Connection.Endpoints` (`calendar`/`api`; `graph`/`api`). Google: singles, masters and exceptions since 120 days ago with `events.list` + `nextSyncToken`, then changes only; a 410 starts over; series expand with `events.instances` per window (cached until the series changes). Outlook: the default calendar per month with `calendarView/delta` (`@removed`, 410 starts the month over), other calendars live with `calendarView`. Times in UTC (`Prefer: outlook.timezone`), all-day dates kept, Windows zone names mapped. Memory caches per person (sync ≤ every 30 s, lists 2 min, busy 1 min); clients poll each minute. Webhook push is optional (Extending).
- **Write back.** Edits need the write capability (`connection_scope_missing` + `fix_url` otherwise); guests are invited by the platform; `scope: "all"` shifts a series by the instance's change; Outlook keeps a Teams link once added.
- **Availability.** `busyFor` adds free/busy (Google `freeBusy`, Graph `getSchedule`) of calendars marked "counts for availability" (primary by default), for booking links, find-time and colleagues' free/busy (times only). A platform that doesn't answer is skipped and logged.
- **Bookings into a connected calendar.** A link's `add_to` writes each booking there too (no guests on the copy: the app emails the booker), with a native Meet/Teams link when `conference_provider` asks (else the room's link in its notes). The app's event stays the record; the copy follows reschedules and cancels through the outbox and is left out of connected views. A lapsed connection books without the copy; a native meeting then falls back to the app's own call.
- **Meetings.** "Video call" options = the wired conferencing's (`ConferenceOptions`: its own call, Zoom, Teams) + the target calendar's native link (Meet on Google, Teams on Outlook), which replaces the same provider. Seam rooms on connected events are written into the event and recorded to follow its time.
- **Agents.** Reads drop what the connection's scope hides (`app.ConnectionScope`, kind `calendar.external`, id `<connection>:<calendar>:<event>` sanitized, hashed when long). Writes: `calendar_google_create_event`/`_move_event`/`_rsvp` and `calendar_outlook_*` (`Source` "Google Calendar"/"Outlook Calendar", Target `calendar.external`), exact preview in the reviewer's zone, the connection checked at suggest in Preview and edit in Handler; colleagues only as guests and the platform never emails them (`sendUpdates=none`).
- **Search.** Connected events 30 days back to 60 ahead (3 s per search), one hit per series (the next), `Source` and `Context` "Google Calendar · account", linking to `/calendar?view=day&date=&open=<ref>`.
- Out of scope: Outlook non-default calendars' delta, editing a series' repeat rule on a connected calendar, attachments, conference add-ons other than Meet, push channels (Google `events.watch`, Graph subscriptions).

## Compliance

- Deletion: their calendars go (team calendars stay with the team, ownerless; team links pass to another member), as do link-host rows, choices and room attributions; (bookers emailed "The host's account was closed"); future bookings they made are cancelled (host told); guests of their upcoming events get cancellations; then their events, links, bookings as host, subscriptions, feed, settings and invitations are deleted. Bookings they made on others' links stay with the host, anonymised ("Deleted user").
- A host who cancels (or deletes a booked event): the booker gets a CANCEL email with the host's note and "Book another time" while the link is open.
- Export: settings, calendars, calendar choices, team links hosted, events with guests, invitations, links, bookings (both sides), subscriptions (host only, never the secret URL), feed date, connected-calendar choices and rooms made for connected events (references).
- Leaving a workspace re-homes a person's own calendars there as personal (its admins never gain them); team calendars stay with the workspace. Booker reminder emails go only to addresses that booked; guests a booker adds get the invite. Deletion also drops those and the person's caches; booking copies stay on the person's own platform calendar.
- Booking emails go to addresses visitors type (limits above). Privacy policy: subscribed URLs are fetched by the server; name the mail provider.

## Stitching

1. Video: implement `calendar.Conferencing` over the meetings feature and call `calendar.SetConferencing` from stitching; honour `ConferenceRequest.Audience` (external: guests may join; internal: members of `OrgID`, or the host and `ParticipantIDs`). Without it, video calls aren't offered.
2. Set `PUBLIC_WEB_URL` (booking, manage and RSVP links in email). `/book/(.*)` and `/calendar/rsvp/[^/]+` are wired into `web/proxy.ts`.
3. Generate `CALENDAR_SECRET_KEY` per environment; rotating it breaks emailed links and makes stored subscriptions unreadable (people re-add them).
4. Decide who appears in the guest picker and free/busy (default: people sharing an organization) and who may create internal links (default: any member).
5. Name booking links in product copy; keep Calendar in the main nav (default) or Home only.
6. Mobile: add a Calendar tab if scheduling is a primary destination (default: a Home entry).
7. Connected platforms: install the connectors feature (2.0+) and register the Google and Microsoft OAuth apps there; wire Zoom/Teams meeting options by implementing `calendar.ConferenceOptions` on your Conferencing (e.g. over `calls.Providers`) and routing `ConferenceRequest.Provider` (`calls.CreateProviderMeeting`…). Pass the active workspace (`?org_id=`, the shared hooks do) so its connection policy applies.
8. Focus time: `calendar.SetFocusHook(func(ctx, userID, until))` from stitching (e.g. presence's Do Not Disturb until `until`); `calendar.FocusUntil` answers "in focus now?". Without it, focus time still declines invitations.
9. Rooms: decide who holds `calendar.manage_rooms` (default owners, admins); restrict booking per room in Who can book….
10. `web/next.config.ts` gets `frame-ancestors 'self'` for the app (template) and `*` for `/book/:slug/embed` (wire line); keep embeds there only.
11. Scheduling started elsewhere (a meetings feature's Schedule, a channel's "Schedule a meeting") can open `calendarNewEventLink({ title, guests, video: true })` so it lands on calendars with invitations; give other features people's zones with `calendar.UserZone` (e.g. `calls.SetUserZone`) and, on the client, the settings' zone (`useCalendarSettings`: `timezone` when `timezone_set`, e.g. into `setCallsTimeZone`) so they show times on the calendar's clock. Product words: "Video call" is the location type; other features say Start call (instant) and New meeting (a link).
12. Read seam for link cards: `calendar.EventRef(ctx, q, viewer *auth.Identity, eventID, occurrence *time.Time) (*calendar.LinkedEvent, error)` reads an event (one occurrence of a series) with the checks of `GET /calendar/events/{id}`: owner, guest or a calendar level of all details or more sees title, `When` (in the viewer's zone; all-day in the event's), repeat rule, location, organizer, a first-line `Snippet` and `Path` (`/calendar/event/<id>`, `?occurrence=` for a series); free/busy on its calendar gets `Busy` with Title "Busy", the times and a `Path` to that day in their calendar, nothing else; agent calls are capped like `calendar_get_event` (calendar level by `access.ViaAgent`, the event by the `calendar.event` scope). Cancelled events and occurrences, occurrences outside the series and everyone else get `calendar.ErrEventNotFound`; nothing is recorded. `calendar.EventFromURL(u)` reads the id and `?occurrence=`. Use it for `channels.RegisterUnfurler("/calendar/event/", …)`, returning nil on not found.
13. Meetings with guests and public booking pages (e.g. a CRM's `crm.SetMeetingSource` / `crm.SetBookingLinks`): `calendar.MeetingsWith(ctx, caller, orgID, emails, from, to) ([]calendar.GuestMeeting, error)` lists the caller's own events (organized or invited, declined left out) and their visible connected Google/Outlook events in orgID's context (attendees from the same cached range read) with any of the addresses as guest or organizer, ≤ 62 days, agent-scoped, never events on calendars merely shared with them; `calendar.PublicBookingLinks(ctx, caller, orgID) ([]calendar.BookingPage, error)` lists their active external links (own, and orgID's team links they made or host; used single-use links and members-only links never) with absolute URLs on `PUBLIC_WEB_URL`.

## Extending

- `calendar.SetConferencing(c Conferencing)` with `Create(ctx, ConferenceRequest) (Conference, error)`, `Update(ctx, id, ConferenceRequest) (Conference, error)`, `Cancel(ctx, id) error`; `calendar.VideoAvailable() bool`. `ConferenceRequest.EventID` names the event a room is for (link back to `/calendar/event/<id>`; a series' occurrences may share it, so it isn't a unique key).
- `calendar.UserZone(ctx, q, userID) (*time.Location, bool)`: the person's zone (false until they have one).
- `calendar.SetFocusHook(fn)`, `calendar.FocusUntil(ctx, q, userID) (time.Time, bool)`; access kinds `calendar.KindCalendar`, `calendar.KindRoom`, permission `calendar.PermManageRooms`.
- Additive conferencing: `ConferenceRequest.Provider`/`ConnectionID`/`GuestEmails`, `Conference.Provider`, `ConferenceOption`, optional `ConferenceOptions` on the wired Conferencing; provider ids `ProviderApp`, `ProviderGoogleMeet`, `ProviderTeams`, `ProviderZoom`.
- `calendar.PlatformMeetings(ctx, caller, orgID, from, to)`: events with Meet/Teams/Zoom links on the person's own and connected calendars (for a meetings feature's lists, e.g. `calls.SetCalendarMeetings`).
- Webhooks: implement `app.ConnectionWebhookHandler` and mark caches stale (`cache.stale`) once the connectors feature registers Google `events.watch` / Graph subscriptions.
- Shared: `useCalendarCalendars`, `useCalendarCalendarPrefs`, `useCalendarRooms`, `useProposeCalendarTime`, `useAnswerCalendarProposal`, `useCalendarSearch`, `useReassignCalendarBooking`, `CALENDAR_SHARE_LEVEL_LABEL`, `CALENDAR_EMBED_MESSAGE` (web: `calendarEmbedSnippet`), `calendarNewEventLink(draft)`, `parseCalendarNewEventLink(params)`, `calendarDraftTimes`, `calendarDraftGuests`, `useCalendarPeopleByIds(ids)`, `useCalendarDeviceZoneSave()`.
- Out of scope: iTIP REPLY by mail (replies use the RSVP link), seats (group bookings), routing forms, event attachments, per-guest permissions (modify, invite, see guests), proposals for series, auto-decline of series invitations, a mobile rooms list and calendar list, paid bookings, RRULE parts beyond the subset (BYSETPOS, hourly), per-occurrence guest lists (guests are series-wide), Windows zone names in subscriptions beyond the mapped set (others fall back to the file's standard offset).
- Don't store subscription URLs or tokens in plain text; never put event details on booking pages; never save from new-event link parameters (anyone can craft one): they only prefill the dialog the person confirms.

## Provenance

New module. Recurrence follows Google Calendar's master/exception model; the SSRF guard follows OLEARY/crm `integration/httpsafe.go` (dial-time resolved-IP checks, no proxy); iCal writing extends `events` (folding, escaping) with VTIMEZONE and iTIP methods. Verified on dev-up: curl and a Python client for every flow, a Go client for agent scope (fake policy) and cross-user tools/search, Playwright at 375/1440 in light and dark; mobile by typecheck and `expo export`.

## Changes

- 1.0.0: first release.
- 1.1.0: times in notifications, reminders, colleague emails, search and the `calendar_create_event` preview use the reader's zone (else the event's), never a silent UTC; all-day dates no longer shift with the reader's zone; the device zone is saved on first sign-in (`calendarZoneSync` session mount: new wire lines on web and mobile); new-event links (`calendarNewEventLink`, web dialog and mobile form); `GET /people?ids=`; `calendar.UserZone`; `ConferenceRequest.EventID`.
- 1.1.1: the `calendar_create_event` suggestion shows the whole event the person accepts (when, repeat, place, guests by name, description) and refuses guests who aren't colleagues before review; its `Target` names `calendar.event`; feed, manage and RSVP token routes (and booking routes) refuse AI agent credentials. Event notifications name their event (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for an event its scope hides.
- 1.1.2: the outbox claims rows in one short statement (`FOR UPDATE SKIP LOCKED`, a claim id, `next_attempt_at` as a lease), sends with no transaction or connection held, and records each result by claim, so a rollback never re-sends what went out and a crashed sender's rows come due when the lease ends (migration `2101_calendar_outbox_claims.sql`). The public booking confirmation links the video call ("Join video call", the manage page's words). The session zone save waits for an active account with the Terms accepted (no 403 for new accounts). Search hits carry `Context` (and `OrgID` for members-only links) and honour `app.SearchOrg` (template `SearchResult.Context`). Copy names organizations with orgs' noun (`orgsNoun`…, orgs with `configureOrgsNoun`). A new link's live preview no longer fails (400) before a title is typed.
- 1.1.3: errors and agent tool descriptions name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
- 1.2.0: connected calendars (Google Calendar, Outlook through `app.Connections`): capabilities, `/sources`, `/connected/*`, incremental sync with per-person caches, write-back and RSVP, connected busy time in availability, booking links' `add_to` and `conference_provider` (Meet/Teams native, Zoom/Teams/app through the seam), additive conferencing options, `PlatformMeetings`, connected agent tools and search hits; web sidebar, badges, event sheet, dialog and link editor; mobile agenda/day, detail and form (migration `2102_calendar_connected.sql`; template `SourceRef`, UI kit).
- 1.3.0: several calendars and team calendars shared through access (`calendar.calendar`: free/busy, details, make changes, manage; delegates edit and create), shared calendars beside yours, re-homed when the owner leaves a workspace; out of office and focus time with auto-decline and `SetFocusHook`/`FocusUntil`; find a time with everyone's zone, hours and work location; rooms and resources (`calendar.room`, `calendar.manage_rooms`, race-safe); RSVP notes and proposed times; event search, duplicate, 3-day and year views, second zone, world clocks, shortcuts, morning agenda email; team booking links (round robin with priority/weight, collective, per-host hours, reassignment), single-use links, redirect, booker reminders, booker guests, email/multi-choice/checkbox questions, embed (`/book/<slug>/embed`, template `scaffold:headers` anchor); mobile 3-day/month, blocks, rooms, notes, proposals, team links; agent tools `calendar_list_calendars`, `_list_rooms`, `_book_room`, `_create_time_block`, `_propose_time` (migration `2103_calendar_depth.sql`; requires access).
- 1.3.1: read seam for link cards (`EventRef`, `EventFromURL`, `ErrEventNotFound`).
- 1.3.2: `MeetingsWith` and `PublicBookingLinks` for other features (e.g. CRM meeting logging and booking links).
