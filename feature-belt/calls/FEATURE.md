# Calls

Video meetings with a shareable link and instant calls that ring, on LiveKit, for web and mobile.

## What it does

- Meetings with a stable Meet-style code and link (`PUBLIC_WEB_URL/meet/abc-defg-hij`). Product words: **Start call** (ring people now, or start alone and share the link), **New meeting** (a link to share, or a scheduled meeting with invitations). Access: **organization only** (members and invitees), **invited people** (others with an account ask to join), or **anyone with the link** (outsiders and guests without an account ask to join while the lobby is on; with it off, anyone joins).
- The lobby: guests type a name and knock; hosts get a toast (also on joining a room where people already wait, with a "N waiting to join" card and Admit) and admit, deny or admit all. Waiting pages check in with every poll; someone who stops for 45 s or closes the page (a beacon) drops out of the list and "Admit all", and comes back if their page checks in again. Hosts and co-hosts lock the room, switch the lobby, change access, mute one person or everyone, lower hands, remove someone (they can't rejoin that session), make co-hosts, end the call for everyone.
- Instant calls ring 1–16 people (a DM or a group): an incoming-call card with a muteable ringtone (web) or a full-screen incoming call with vibration (mobile); answered elsewhere, declined, or a missed-call notification after `CALLS_RING_TIMEOUT`. The caller sees who is ringing, declined or didn't answer, and rings again.
- In the call: pre-join with camera preview, mic level and device pickers; grid, speaker and auto layouts with the active speaker outlined; screen share; mic/camera toggles (⌘/Ctrl+D, ⌘/Ctrl+E), raise hand (⌘/Ctrl+Alt+H), six reactions, chat (data channel, not stored), people panel (⌘/Ctrl+Alt+P), connection warnings, a reconnect banner, blocked-device messages, closing screens with Rejoin.
- History: calls you were in (duration, who) and calls you missed; each call's detail with everyone's time in it.
- Times in invitations, agent previews and search are in the reader's zone with the zone named; on screen, in the app's zone (`setCallsTimeZone`, e.g. the calendar's), named when it isn't the device's. An app can send "Schedule a meeting" to its calendar instead of calls' own form; meetings another feature made are managed there ("Open in …").

## Data

| Table | Purpose |
|---|---|
| `calls_meetings` | code (unique), kind meeting/call, title, host, org, access, lobby, locked, schedule, `source_kind`/`source_id` (unique while active), `canceled_at` |
| `calls_invitees` | invited people; role `invitee` or `cohost` |
| `calls_sessions` | each time the room is live: SFU `room_sid`, start/end, end reason; one open per meeting; sessions nobody joined are deleted |
| `calls_participants` | attendance from webhooks: identity, user (guests: the typed name), joined/left |
| `calls_lobby` | knocks: a user, or a guest (sha256 of the guest's key); waiting/admitted/denied/canceled/expired/removed; `seen_at` (last check-in), `away_at` (stopped checking in) |
| `calls_rings` | instant-call rings: caller, callee, ringing/accepted/declined/missed, expiry |
| `calls_removals` | identities removed from a session |

`migrations/2000_calls_init.sql` is the schema; `2001_calls_lobby_presence.sql` adds the lobby check-ins.

## API

Under `/v1`, signed in unless public; `{id}` takes a meeting id (the GET also a code or link). Writes need an active account.

| Method | Path | Purpose |
|---|---|---|
| GET | `/calls/history?cursor=` · `/calls/history/{id}` | your calls and missed calls (keyset) · one call (attended, or a host) |
| GET | `/calls/upcoming` · `/calls/links` · `/calls/rings` · `/calls/people?q=&org_id=` | scheduled meetings · your unscheduled links · ringing now · people you may call |
| GET | `/calls/meetings/{id}` | `JoinInfo`: mode host/cohost/direct/knock/denied (+ reason); title for direct/knock, detail for direct only |
| POST (30/h) | `/calls/meetings` | create `{title?, org_id?, access?, lobby_enabled?, starts_at?, ends_at?, invitee_ids?, cohost_ids?, time_zone?}` (`time_zone`: the creator's IANA zone, sent by the shared client) |
| PATCH / DELETE | `/calls/meetings/{id}` | settings (hosts) / cancel (the host) |
| POST · PATCH · DELETE | `/calls/meetings/{id}/invitees` · `…/{userID}` | add `{user_ids, role?, time_zone?}` (co-hosts only by the host) · role · remove (or leave yourself) |

A meeting with a `source` (a calendar event's room, a channel's call) is managed there: its title, schedule, invitees (adding, removing others) and cancel answer 409 `calls_managed_by_source` with `details.source`; hosts still run the room (access, lobby, lock, co-hosts) and the seams still change it.
| POST (60/min) | `/calls/meetings/{id}/join` | room token for people who may join (403 `calls_knock_required`/`calls_not_allowed`, 503 `calls_unconfigured`/`calls_unavailable`) |
| POST · GET · DELETE | `/calls/meetings/{id}/knock` · `/calls/lobby/{id}` | ask to join (10/min) · your knock (polling it is your check-in) · cancel it |
| GET · POST | `/calls/meetings/{id}/lobby` · `…/lobby/admit`, `…/lobby/deny` | people still waiting · `{ids}` or `{all:true}` (hosts; only people still there) |
| POST | `/calls/meetings/{id}/mute`, `/remove`, `/end`, `/hand` | `{identity}` or `{all}` · `{identity}` · end for all (hosts) · `{raised, identity?}` |
| POST (30/h) · POST · DELETE · GET | `/calls/calls` · `/calls/meetings/{id}/ring` · same · `…/rings` | ring `{user_ids, org_id?, title?}` · ring again (hosts: new people) · stop ringing · ring states |
| POST | `/calls/rings/{id}/accept`, `/decline` | the callee's answer (allowed while suspended) |
| GET (60/min/IP) · POST (10/min/IP) | `/calls/public/meetings/{code}` · `…/knock {name}` | guests: preview (title only when open) · knock → entry + `key` |
| POST | `/calls/public/lobby/{id}/status`, `/cancel`, `/leave`, `/token`, `/hand` | guests, `{key}` in the body (120 or 30/min/IP); `status` is the check-in, `leave` the closing page's beacon (any content type) |
| POST | `/webhooks/livekit` | LiveKit webhooks (JWT + body hash, deduplicated) |

Realtime: `calls:<meeting id>` (people who may join; re-checked when that may shrink: access, lobby or lock changes, an invitee removed, a host's remove, cancel, the seams, leaving the org): `calls.meeting_updated`, `calls.ring_updated`. User topics: `calls.ring`, `calls.ring_ended` (callees), `calls.lobby` (hosts; reason knock, decided, canceled, left, expired: a 10-second job tells hosts who stopped checking in), `calls.lobby_decided` (the knocker). Notifications: `calls.invite` (push + email; the time in each invitee's zone from `SetUserZone`, else the inviter's `time_zone`, else UTC, always named), `calls.missed` (push).

Agent tools: `calls_list_recent`, `calls_list_upcoming`, `calls_get_meeting` (only meetings the user can join directly), `calls_create_meeting` (write; Preview “Create a meeting “Title” on Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST” in the reviewer's zone, else the agent's UTC offset, with title, time, organization and who can join in the after text; no invitations, no `Target`: it creates rather than changes one meeting). Search: titles of meetings the caller can join, with `OrgID` and `Context` ("Org · Calls"); a search scoped to an org (`app.SearchOrg`) keeps that org's meetings and ones in none. Every tool and hit goes through `accessFor` (the REST rule) and drops meetings the user's AI agent scope hides (`app.AgentAllows`, kind `calls.meeting`).

## Screens

- Web: `/calls` (nav + Home tile): Start call (`StartCallDialog`: ring people, or nobody picked = start alone), New meeting (a link to share / schedule: `ScheduleMeetingDialog`, or the app's `setCallsScheduleAction`), join by code, Upcoming, Recent, Your meeting links; ⋮ `ResourceMenu` on the header, rows and `/calls/[id]` (call detail). Meetings with a `source` show Copy link and the app's "Open in …" (`setCallsSourceLink`), never Edit or Cancel. `/meet/[code]` (public, full screen): pre-join, waiting, the call, a closing screen; guests get name + knock, or Sign in for non-open meetings. Session mount `CallsRinger`: the incoming-call card. LiveKit's React hooks under our components and tokens (no LiveKit stylesheet; we create the `Room`, not the prefab container).
- Mobile (dev builds only): Home row Calls (`calls/index`: join by code, Upcoming with ⋮ share link and the app's "Open in …" (`setCallsSourceLink`), Recent, New meeting sheet: a link to share, "Schedule a meeting" when the app sets `setCallsScheduleAction`), `calls/[id]`, `calls/new` (Start call: ring people, or start alone), `calls/incoming` (opened by the `callsSession` mount), `meet/[code]` (camera/mic choices, ask to join, the call with tiles, switch camera, hand, people waiting with Admit when alone, a people sheet with the lobby and host mute/remove, a chat sheet).
- Every async view has skeleton, empty and error states. Web verified at 375/1440, light/dark.

## Config

| Variable | Default | Meaning |
|---|---|---|
| `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` | — | all three or none (none = 503 `calls_unconfigured`); outside development a public `wss://` URL and a 32+ character secret, or the boot fails |
| `LIVEKIT_API_URL` | from `LIVEKIT_URL` | server API origin when it differs (private network) |
| `CALLS_TOKEN_TTL` · `CALLS_RING_TIMEOUT` · `CALLS_MAX_PARTICIPANTS` | 10m · 45s · 100 | join token life · ring time · room cap |
| `CALLS_LOCAL_LIVEKIT` · `CALLS_LIVEKIT_DOWNLOAD` (dev) | true · true | dev-up runs LiveKit · fetches the pinned release |

dev-up hook `scripts/dev-up/hooks/calls.sh`: LiveKit 1.13.11 on block ports **+7** (signalling, 127.0.0.1) and **+8** (media: UDP mux + TCP); the wallets hook has +6, +9 stays free. No `livekit-server` on PATH → Linux: the sha256-pinned release binary; macOS (LiveKit ships none): the sha256-pinned release source, built once with Go (~2 min) into `.data/bin`. Per-project key pair in `.data/livekit/keys`; webhooks go to the local API; menu `v`. Clients get the URL with each token: no client env. npm: web `livekit-client`, `@livekit/components-react`; mobile adds `@livekit/react-native`, `@livekit/react-native-webrtc`, `@livekit/react-native-expo-plugin`, `@config-plugins/react-native-webrtc` (plugin `mobile/plugins/calls.js`). No Go dependency: an HS256 signer/verifier (`jwt.go`, tested) instead of LiveKit's server SDK and the Pion stack it pulls in.

## Compliance

- Deletion: meetings the person hosted that nobody used are deleted; the rest are canceled (links stop) and keep their title for others' history; invitations, knocks and rings received are deleted; attendance and rings made stay, shown as "Deleted user". After commit: live rooms they hosted end and they're removed from rooms they're in.
- Export: meetings hosted, invitations, attendance, rings received and made.
- Nothing is recorded; chat and reactions use the data channel and aren't stored. Guests' names live in lobby and attendance rows.
- Mobile: the camera string covers shared photos and video calls, the microphone string calls; the plugin unblocks Android `RECORD_AUDIO` and adds iOS background audio (calls keep sound in the background). Privacy policy: name the SFU host.

## Stitching

1. LiveKit in production: a Cloud project or self-hosted server, `LIVEKIT_*`, and the webhook `<PUBLIC_API_URL>/webhooks/livekit` signed with that key (history stays empty without it).
2. Who may ring or invite whom: `calls.SetCallPolicy` (default: share an organization) with a matching `calls.SetDirectory`; add block checks if the app has moderation.
3. Calendars: `calls.CreateMeeting(ctx, tx, …Source: &calls.Source{Kind: "calendar.event", ID: id})` inside your transaction (a 409 means that source already has an active meeting: create without one); send your own invitations; `UpdateMeeting`, `CancelMeeting`, `MeetingBySource`. On the client, `setCallsScheduleAction` sends "Schedule a meeting" to the calendar and `setCallsSourceLink` maps a source to its page.
4. Channels and DMs: "Start call" → `POST /v1/calls/calls {user_ids}` (or `calls.StartCall`) → open `/meet/<code>?autojoin=1`. Use the same words everywhere: Start call (instant), New meeting (a link), Video call (a calendar location).
5. `PUBLIC_WEB_URL` builds join links; mobile universal links for `/meet/*` open `meet/[code]`.
6. A privacy-policy line for call media; rebuild the mobile dev client (native WebRTC).
7. Time zones: `calls.SetUserZone(fn)` with each person's zone (e.g. `calendar.UserZone`); without it, invitations use the inviter's device zone. On the client, `setCallsTimeZone(zone)` (shared, web and mobile) from a session mount whenever the person's zone is known or changes (e.g. the calendar settings' zone).

## Extending

```go
m, err := calls.CreateMeeting(ctx, tx, calls.MeetingInput{HostID: u, OrgID: &org, Title: "Intro", StartsAt: &t0, EndsAt: &t1,
    Access: calls.AccessOrg, InviteeIDs: ids, Source: &calls.Source{Kind: "calendar", ID: ev}}) // m.Code, m.JoinURL
calls.UpdateMeeting(ctx, tx, m.ID, calls.MeetingUpdate{StartsAt: &t2, InviteeIDs: &ids})
calls.CancelMeeting(ctx, tx, m.ID) // the worker ends a live room within a minute
calls.MeetingBySource(ctx, q, "calendar", ev); calls.GetMeeting(ctx, q, id); calls.CanJoin(ctx, q, id, user)
calls.StartCall(ctx, caller, []uuid.UUID{a, b}, calls.CallOptions{OrgID: &org, Source: &calls.Source{Kind: "channel", ID: ch}})
calls.SetCallPolicy(fn); calls.SetDirectory(fn); calls.SetMedia(m) // another SFU: implement calls.Media
calls.SetUserZone(func(ctx context.Context, id uuid.UUID) *time.Location { … }) // nil = unknown
```

- Shared: `callsEndpoints`, `callsKeys`, `useCallsJoinInfo`, `useJoinCallsMeeting`, `useStartCallsCall`, `useCallsRings`, `useCallsHistory`, `parseCallsCode`, `callsDeviceZone`, `setCallsTimeZone(zone | null)`, `callsTimeZone()`, `useCallsTimeZone()`, `formatCallsWhen(iso, zone?)`, `formatCallsTime(iso, zone?)`, `callsZoneLabel(zone?)`, … Web: `StartCallDialog`, `ScheduleMeetingDialog`, `copyMeetingLink` from `@/features/calls`; `setCallsScheduleAction(({ navigate, orgId }) => …)` and `setCallsSourceLink((source) => ({ href, label }) | null)` from `@/features/calls/seams` (web and mobile). Set them once at startup; the schedule action must not lead back to calls' Schedule.
- Out of scope also: a scheduling form on mobile (the sheet's Schedule row appears only with an app action).
- Out of scope: recording (LiveKit Egress: a host action starting a room-composite egress to S3, `egress_*` webhooks, a recordings table, a consent notice in the room), VoIP push with CallKit/ConnectionService (react-native-callkeep + PushKit), mobile screen share (broadcast extension), background blur (`@livekit/track-processors` on the camera track; it fetches MediaPipe models from a CDN), captions, breakout rooms, dial-in.
- Don't change: identities `u:`/`g:`, room name = meeting id, one open session per meeting, `accessFor` as the only access rule.

## Provenance

New module (no app on this machine had video calls); idioms from `chat` (realtime cache handlers, policy hook), `orgs` (guards, hashed tokens) and `payments` (dev-up hook). LiveKit behaviour checked against a running 1.13.11 server: Twirp JSON uses proto names; webhooks are camelCase protojson with `Authorization: <jwt>` (no Bearer) and a base64 SHA-256 body claim; the UDP mux listens on interface addresses, so the dev server advertises them, not `127.0.0.1`.

Verified: Go build/vet/tests, typecheck, `pnpm check`, doctor, web build, iOS and Android `expo export`, `expo prebuild` (permissions, purpose strings, background audio, LiveKit pod source); on dev-up with the hook building LiveKit: two-user video, guest knock/admit/deny, org refusal (REST and token), ringing (overlay, accept, decline, missed), host hand/mute/remove/end, chat, reactions, webhook attendance, tools and search under an agent policy. Not verified on a device: the mobile call screens.

## Changes

- 1.0.0: first release.
- 1.1.0: invitation notifications, the `calls_create_meeting` preview and search snippets word times in the reader's zone (`SetUserZone`, else the request's `time_zone` or the agent's offset), never a bare UTC; the shared client sends the device zone on create and invite; client seams `setCallsScheduleAction` (web and mobile) and `setCallsSourceLink` (web); meetings with a `source` are no longer edited or cancelled from Calls.
- 1.1.1: REST refuses changes a meeting's `source` owns (title, schedule, invitees, removing others, cancel: 409 `calls_managed_by_source`), matching the UI; live `calls:<id>` subscriptions are re-checked when access may shrink, including leaving the org (`orgs.OnMemberRemoved`, orgs ≥ 1.1.2), so people who can no longer join stop receiving its events; the `calls_create_meeting` suggestion preview shows who can join and the organization. Meeting notifications name their meeting (`AgentTarget`, notifications ≥ 1.1.1), so an agent reading the inbox doesn't see them for a meeting its scope hides.
- 1.2.0: the lobby shows only people still there: waiting pages check in with each poll (also from a background tab); someone who stops for 45 s, or a guest whose page closes (a beacon to the new `POST /calls/public/lobby/{id}/leave`), is set aside, hosts hear it at once (`calls.lobby` reasons `left`, `expired`; a 10-second `calls.lobby` job), "Admit all" never admits them, and a check-in brings them back (migration `2001_calls_lobby_presence.sql`). A host joining while people wait gets a toast and the room card shows "N waiting to join" with Admit (web and mobile). On-screen times read in the app's zone (`setCallsTimeZone`, `useCallsTimeZone`, `callsTimeZone`; new meetings send it) and name it when it isn't the device's; `formatCallsWhen(iso, zone?, now?)` (was `(iso, now?)`), new `formatCallsTime`, `callsZoneLabel`, and `toCallsLocalInput`/`fromCallsLocalInput` take a zone. Words: Start call (ring people, or start alone; replaces "Call" and "Start a meeting now"), New meeting (a link to share, or schedule). Mobile `setCallsSourceLink` and an Upcoming ⋮ (share link, "Open in …"). Search hits carry `OrgID` and `Context` and honour `app.SearchOrg` (template `SearchResult.Context`). Copy names organizations with orgs' noun (`orgsNoun`…, orgs with `configureOrgsNoun`; `CALLS_ACCESS_LABELS`/`CALLS_ACCESS_HELP` read it when used).
- 1.2.1: errors, the `calls_create_meeting` description and its preview name organizations with `orgs.Noun()` (orgs ≥ 1.3.0).
