# Presence

Who is online, away or offline, custom statuses and Do Not Disturb, on web and mobile.

## What it does

- Online, away or offline for everyone you may see, live. Clients send heartbeats while the app is on screen; web reports idle (away) after `PRESENCE_IDLE_AFTER` without input or while the tab is hidden; a device that stops beating is offline after `PRESENCE_OFFLINE_TTL`. State lives in Postgres, so any number of API instances agree.
- A custom status: emoji + text + "clear after" (30 minutes, 1 hour, 4 hours, today, this week, don't clear), with presets. It clears on its own and the change is announced.
- Do Not Disturb until a time (30 minutes to next week, up to 31 days): pushes and emails are held through the notifications delivery filter (stitch 3); `presence.IsDND` answers for any feature.
- "Appear away" while connected.
- Visibility is the app's policy (default: any signed-in user; apps with orgs: people who share an org). Reads, the realtime authorizer and the agent tool all use it.

## Data

| Table | Purpose |
|---|---|
| `presence_devices` | one row per tab or app install: platform, `active` (not idle), `last_seen_at` (last heartbeat); rows silent for a day are deleted |
| `presence_status` | custom status (`emoji`, `text`, `clear_after`, `expires_at`), `dnd_until`, `away`, and `announced` (the state last published, so a transition is announced once) |

`migrations/0800_presence_init.sql` is the schema.

## API

Under `/v1`, signed in.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/presence?user_ids=a,b` | user | presence of the people you may see (≤ 100); others are left out |
| GET | `/presence/me` | user | yours + `away`, `audiences`, `heartbeat_seconds`, `idle_after_seconds` |
| POST | `/presence/heartbeat` | user, 60/min | `{device_id, platform, active}` → `{status, heartbeat_seconds, idle_after_seconds}` |
| POST | `/presence/offline` | user, 60/min | `{device_id}`: this device is leaving |
| PUT / DELETE | `/presence/me/status` | active, 30/min / user | `{emoji, text, clear_after, expires_at?}` (`expires_at` required for `today`/`week`, from the client's time zone) / clear |
| PUT / DELETE | `/presence/me/dnd` | user, 30/min | `{until}` / resume |
| PUT | `/presence/me/away` | user, 30/min | `{away}` |

Realtime: `presence:<audience>` (members of that audience, per the policy) and the person's own `user:` topic carry `presence.updated` (a `Presence`). A sweeper (`jobs.Every`, 15 s, one instance) announces devices going quiet and statuses or Do Not Disturb running out.

Agent tool (read-only): `presence_get_status {user_ids}` → status, status text, clear time and Do Not Disturb for the people the caller may see.

## Screens

- Shared (`packages/shared/src/presence`): `presenceEndpoints`, `usePresence(ids)` (batched across the screen into requests of 100; map of `Presence | null | undefined`), `useUserPresence`, `useMyPresence`, `useSetPresenceStatus`, `useClearPresenceStatus`, `useSetPresenceDND`, `useSetPresenceAway`, `usePresenceSession` (the heartbeat engine and topic subscriptions, platform-neutral), helpers (`presenceStatusRequest`, `presenceDndUntil`, `presenceLabel`, `presenceStatusText`, `presenceUntilLabel`, presets and options).
- Web: `PresenceDot` (online filled, away hollow, do not disturb filled with a bar, offline hidden or faint), `UserPresenceDot`, `PresenceAvatar`; account menu items (set a status, pause or resume notifications, appear away/active); the status dialog (emoji menu, text, clear after, presets) and the pause dialog; the session mount runs heartbeats once consent is accepted.
- Mobile: `PresenceDot`, `UserPresenceDot`, `PresenceAvatar`; Settings → Status (`presence/status`: status with emoji row, presets, clear after, pause notifications, appear away; the row's badge is your status emoji); the session mount beats while the app is in the foreground.

## Config

| Variable | Default | Meaning |
|---|---|---|
| `PRESENCE_OFFLINE_TTL` | 2m | no heartbeat for this long = offline (≥ 2 × interval) |
| `PRESENCE_HEARTBEAT_INTERVAL` | 45s | heartbeat cadence clients follow (≥ 10s) |
| `PRESENCE_IDLE_AFTER` | 10m | web: no input for this long = away (≥ 1m) |

Invalid values fail the boot (`config.Problem`). No dependencies; requires nothing.

## Compliance

- Deletion: devices and status rows are deleted. Export: status, Do Not Disturb, away, and each device's platform and heartbeat times (never device ids).
- A custom status is text other people see: wire `SetTextCheck` to the content filter (stitch 2). Status writes need an active account.
- Privacy policy: say that people you share the app with can see when you are online.

## Stitching

1. Visibility: `presence.SetVisibilityPolicy(presence.SharedGroupPolicy("org_members", "org_id", "user_id"))` for "people who share an org", or your own `VisibilityPolicy` (`Audiences` = topics a person's changes go to and they may subscribe to; `Visible` filters subjects). Default: any signed-in user.
2. `presence.SetTextCheck(moderation.Check)` when moderation is installed.
3. Do Not Disturb for every push and email (notifications ≥ 1.1.0), from any module's `New` or a `feature_*.go` file; the inbox row and realtime event still land. The channels feature registers exactly this when installed (the same name replaces):

```go
notifications.AddDeliveryFilter("presence.dnd", func(ctx context.Context, n notify.Notification) (notifications.Hold, error) {
	on, err := presence.IsDND(ctx, d.Pool, n.UserID)
	return notifications.Hold{Push: on, Email: on}, err
})
```
4. Place `PresenceAvatar` / `UserPresenceDot` wherever people appear (member lists, profiles, message headers).
5. Mobile: move the Status row from Settings to a profile or account sheet if the app has one.

## Extending

```go
on, _ := presence.IsDND(ctx, q, userID)            // Do Not Disturb right now?
until, _ := presence.DNDUntil(ctx, q, userID)      // *time.Time or nil
m, _ := presence.Lookup(ctx, q, viewerID, ids)     // map[uuid]Presence of the visible ones
ok, _ := presence.CanSee(ctx, q, viewerID, subjectID)
```

- Out of scope: Do Not Disturb schedules (nightly hours), status history, "last seen" times (not shown on purpose), OS-level idle detection on desktop.
- Don't change: presence is computed from `presence_devices` (never from one instance's sockets); every read goes through the policy.

## Provenance

New for the forum belt; heartbeat-and-TTL presence in the style of Slack (active / away, custom status with expiry, notification snooze). Built on the template's realtime broker and `jobs.Every`.

Verified on dev-up (installed with channels): heartbeats online → idle away → appear away → offline call, and the sweeper announcing offline about 2 minutes after the last heartbeat; status validation (emoji only, `expires_at` required for today/week); live `presence.updated` to another user; batched reads; dots and status in the web channel UI at 375 and 1440, light and dark; Do Not Disturb holding a push through the notifications filter while the inbox row lands. Not driven on a device: the mobile foreground/background lifecycle.

## Changes

- 1.0.0: first release (backend, shared hooks, web and mobile UI).
