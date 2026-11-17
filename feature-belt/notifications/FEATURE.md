# Notifications

## What it does

- Every `d.Notify.Send` lands in the user's inbox (always on) and arrives live with the new unread count: a bell with a badge in the top bar and an Inbox tile on Home (web), the Home row and the app icon (mobile), and a toast while the app is open.
- Push to the user's phones through the Expo push service. The app offers push from the inbox and notification settings ("Turn on"), never at launch; tapping a push opens its link.
- Email for kinds that allow it, when the user leaves email on.
- Settings → Notifications: push and email per kind, grouped by feature. Modules declare kinds and defaults with `RegisterKind`.
- The inbox works like Slack's Activity and the GitHub/Linear inboxes: All / Unread / Snoozed, a filter by feature with counts, read or unread per item and Mark all read, snooze until a time (it comes back unread at the top and pushes again), grouping by thread or resource, and Mute on items whose feature allows it (nothing new arrives about that resource; Settings lists and unmutes).
- An opt-in email digest of unread notifications, daily or weekly at a local hour. It waits while Do Not Disturb or quiet hours hold email (delivery filters) and retries for up to 12 hours.
- Tokens are registered after sign-in, removed on sign-out, and pruned when Expo reports them dead.
- Admin → Notifications: delivery counts, busiest kinds, and a test send to one person (confirmed, audited).

## Data

| Table | Purpose |
|---|---|
| `notifications` | Inbox rows: kind, title, body, link, data, `read_at`. Read rows older than `NOTIFICATIONS_RETENTION` are pruned hourly. |
| `notification_devices` | Expo push tokens (PK), owner, platform ios/android/web, `last_seen_at`. 10 per user; re-registering from another account moves the token. |
| `notification_preferences` | Saved push/email per (user, kind). No row = the kind's registered defaults. |
| `notification_push_tickets` | Expo tickets awaiting the receipts check (15 min after send, dropped after 24 h). |
| `notifications` (1.2 columns) | `sort_at` (inbox order; the wake-up time after a snooze), `snoozed_until`, `resource` (`<kind>:<id>[/thread:<id>]`, for groups and mutes) |
| `notification_mutes` | Resources a person muted from the inbox (with the label they saw). |
| `notification_digests` | Digest cadence (daily/weekly), local hour, weekday, zone, `next_at` (slot or retry), `last_sent_at`. |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST | `/v1/devices` | user, consent-exempt, 30/h | `{token, platform}` register or move an Expo token → 204 |
| DELETE | `/v1/devices/{token}` | user, consent-exempt | Remove the caller's token (URL-encoded) → 204 |
| GET | `/v1/notifications` | user | `{items, next_cursor, unread_count}`; `?unread=true`, `?snoozed=true` (only snoozed; others hide them), `?kind=` (kind or feature), `?limit`, `?cursor` (keyset on `sort_at, id`) |
| GET | `/v1/notifications/groups` | user | same filters; `{items: [{key, count, unread, latest}]}` grouped by resource (a group of one is keyed by its id), newest group first |
| GET | `/v1/notifications/kinds` | user | `{items: [{group, count, unread}], snoozed}` for filter chips (`?snoozed=true` counts the snoozed view) |
| GET | `/v1/notifications/unread-count` | user | `{unread_count}` (snoozed ones don't count) |
| POST | `/v1/notifications/read` | user, consent-exempt | `{ids}` (≤ 500), `{resources}` (group keys, ≤ 50) or `{all: true}` (not the snoozed) → `{unread_count}` |
| POST | `/v1/notifications/unread` | user, 120/min | `{ids}` or `{resources}` → `{unread_count}` |
| POST | `/v1/notifications/snooze` / `unsnooze` | user, 60/min | `{ids \| resources, until}` (1 min to 30 days) / `{ids \| resources}` → `{unread_count}`; a job wakes them every 30 s (unread, `sort_at` now, `notification` event, push again per preferences and filters) |
| GET / POST / DELETE | `/v1/notifications/mutes` | user (writes 60/min) | list / `{resource, label}` (a resource you were notified about whose kind allows muting; its unread turn read) / `?resource=` |
| GET / PUT | `/v1/notifications/digest` | user (PUT 30/min) | `{cadence: off\|daily\|weekly, hour, weekday, time_zone}` (+ `next_at`, `last_sent_at`) |
| GET / PUT | `/v1/notifications/preferences` | user (PUT 60/min) | `{items: [{kind, group, label, push, email}]}`; PUT saves the listed kinds |
| GET | `/v1/admin/notifications/stats` | support | push mode, totals, unread, devices by platform, pending receipts, top kinds (7 days) |
| POST | `/v1/admin/notifications/test` | support, audited | `{user_id, title?, body?}` → `{notification_id, devices, push_mode}`; ignores preferences, no email |

Realtime on `user:<id>`: `notification` (inbox item + `user_id` + `unread_count`, a superset of core's `InAppNotification`) and `notifications.unread` `{unread_count}` after every change. Push `data` = the notification's `data` + `notification_id`, `kind`, `link`; `badge` = unread count.

Agent tools: `notifications_list_inbox` (`unread_only`, `kind`; snoozed ones left out), `notifications_get_preferences` (read) and `notifications_mark_read` (write, previewed with the titles it marks). Snooze, mute and the digest are the person's own and have no tools. The inbox quotes every feature, so an AI agent reading it for the person (`auth.WithAgent`) sees only notifications about what their agent scope lets agents read, counts only those, and marks only those read.

Shared (`@scaffoldapp/shared`): `notificationsEndpoints`, `notificationKeys`, `useInbox(filter)` (boolean = unread only, or `{unread, snoozed, kind}`), `useNotificationGroups`, `useNotificationKinds`, `useUnreadNotificationCount`, `useMarkNotificationsRead` (optimistic; ids, resources or all), `useMarkNotificationsUnread`, `useSnoozeNotifications`, `useUnsnoozeNotifications`, `notificationSnoozeOptions`, `useMuteNotificationResource`, `useNotificationMutes`, `useUnmuteNotificationResource`, `useNotificationDigest`, `useUpdateNotificationDigest`, `notificationDeviceZone`, `useNotificationPreferences`, `useUpdateNotificationPreference` (optimistic), `groupPreferences`, `groupLabel`, `useRegisterDevice`, `useUnregisterDevice`, `useNotificationStats`, `useSendTestNotification`. Realtime handlers update the inbox (filter-aware; a woken snooze moves to the top) and count caches; reconnect refetches.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Inbox | `/notifications`: All/Unread/Snoozed, feature chips, Group by thread (all in the URL), Mark all read; rows open their link and have ⋮ (Mark read/unread, Snooze ▸ In 3 hours · This evening · Tomorrow · Next week, Unsnooze, Mute); groups show their count | `app/notifications.tsx` (Home row "Inbox" with badge): the same views, chips and group switch, ⋮ or long press opens an action sheet, pull to refresh, push banner |
| Settings | `/settings/notifications` (registry section; emails link here): Email digest (Never / Every day / Every week, day, hour), switches per kind and channel, Muted with Unmute | `app/settings/notifications.tsx`: push banner, Email digest (pickers in a sheet), a group per kind with Push/Email switches, Muted |
| Admin | `/admin/notifications` (admin tab): counts, kinds table, test send with user search; `?user=<id>` preselects | — |

Registry wiring. Web: `headerActions` bell (opens the inbox, unread badge), `homeTiles` Inbox, `sessionMounts` foreground toasts, `settingsSections`, `adminNav`. Mobile: `homeEntries` Inbox (badge), `settingsSections`, `sessionMounts` push lifecycle (token sync on launch when permission is granted, unregister on sign-out, push taps, icon badge, toasts), and the `expo-notifications` config plugin in `app.config.ts`. Every list has skeleton, empty (one action) and error states. Verified at 375/1440, light/dark.

## Config

| Variable | Default | Doc |
|---|---|---|
| `EXPO_ACCESS_TOKEN` | — | Expo push access token; needed only when push security is on. Unset in development = sink or log. |
| `PUSH_SINK_DIR` | dev `.data/push` | Development without a token: each push batch is written here as JSON. |
| `NOTIFICATIONS_RETENTION` | `2160h` | Read inbox rows older than this are deleted (≥ 1h). |

Push modes: `expo` outside development or with a token; in development without one, `sink` with `PUSH_SINK_DIR`, else `log`. npm: `expo-notifications ~57.0.23` (mobile). No new Go dependencies.

## Compliance

- Deletion: inbox, devices, preferences, mutes, the digest setting and pending tickets are deleted. Export: preferences, devices (platform and dates, never tokens), latest 500 inbox rows and up to 500 snoozed, mutes, digest setting.
- The digest is opt-in and says how to change or stop it in every email.
- Push is optional and asked for at the moment of use; denial leaves the inbox working and shows a Settings link.
- Privacy policy: name Expo (push relay) and the mail provider.

## Stitching

1. Register each kind your modules send: `notifications.RegisterKind(notifications.Kind{ID: "chat.message", Label: "Messages", Push: true})` in the module's `New` (or in `internal/api/feature_<name>.go` for a feature that doesn't require notifications). Leave a kind unregistered when its module emails on its own (`support.reply`): unregistered kinds push and never email.
2. Give every notification a `Link` that is a route on both web and mobile. Titles and bodies show on lock screens: keep them factual.
3. Mobile push: run `eas init` (writes `extra.eas.projectId`; without it the banner stays hidden and no token is fetched), configure push credentials (APNs key in EAS, FCM v1 for Android) and rebuild the dev client. The config plugin is already wired; give it `icon`/`color` options for a branded Android icon. Real pushes need a device build.
4. Mobile placement: Inbox is a Home row. If notifications are a primary destination, add a tab (`<Tabs.Screen name="notifications" …>` with a route under `app/(tabs)/`) and put the badge there; the push lifecycle is a session mount and needs no change.
5. Ask for push at a moment that explains itself (after a first message, after opening a support request): render `PushBanner` from `src/features/notifications/PushBanner.tsx` there.
6. Mute from the inbox is opt-in per resource kind: `notifications.AllowMute("docs.page")` (stored here, new notifications about it dropped, a muted resource mutes its threads) or `notifications.RegisterMuteHandler("channels.channel", h)` when the feature keeps the mute itself (`h.SetMuted(ctx, userID, id, muted)` checks access; the feature then stops sending).
7. Threads: notifications with data `thread_id`, or a link with `?thread=<id>`, group by that thread; `Kind.Group` names another resource than `AgentTarget`.

## Extending

- Delivery filters hold back push and email without this feature importing yours (Do Not Disturb, muted channels). The inbox row and realtime event always go out; filters only remove channels the kind and preferences turned on. They run in the background after `Send` returns, 2 s each; an error, panic or timeout is logged and the preferences stand. Staff test sends skip them. Register from your `New` (or `feature_<name>.go` if you don't require notifications); the same name replaces:

```go
notifications.AddDeliveryFilter("presence.dnd", func(ctx context.Context, n notify.Notification) (notifications.Hold, error) {
	on, err := presence.IsDND(ctx, d.Pool, n.UserID)
	return notifications.Hold{Push: on, Email: on}, err
})
```

  Keep filters to an indexed lookup; read ids from `n.Data` (`channel_id`). Held pushes are not sent later; the inbox has them.
- `Sender` is the `notify.Sender`; add channels in `sender.go` (web push for `platform = web`; the Expo sender skips web tokens).
- Kinds only through `RegisterKind`; put ids in `data`, not new columns. Don't call Expo from other modules.
- Agent scope: give a kind `AgentTarget` naming the item its notifications are about, `notifications.DataTarget("channels.channel", "channel_id")`, so an agent sees them only while that item is readable at the person's agent scope (item → ancestors → feature → default). Without it, data `{"kind", "id"}` is used when present (access shares), else the kind's feature (its prefix) decides as a whole; a kind about another feature's item also needs its own feature readable.
- Web toasts: `web/src/features/notifications/toasts.ts`; mobile lifecycle: `mobile/src/features/notifications/lifecycle.ts`.

## Provenance

- DOGPATCH `internal/notify` (inbox + realtime + async push in one call), `accounts/devices.go` (token regex, move on re-register, per-user cap), `accounts/notifications.go` (keyset inbox with unread count).
- SFLUV tickets/receipts (`DeviceNotRegistered` deactivation), notification ledger, test-mode file sink, socket-first foreground suppression; THASSA `internal/push` (batches of 100).
- Fixed: sending continues past a failed batch or token (ASSEMBLY stopped at the first error); 429/5xx retried with backoff; receipts are actually checked; deleted users receive nothing; Expo messages that quote tokens are never logged; preferences per kind × channel added; push permission asked at use, not launch.
- Unverified here: delivery to a real device (no device build in this pass; the sink and a fake-Expo unit test cover the server side), Android channel behaviour on a device.

## Changes

- 1.0.0: shared hooks, web and mobile screens, admin page, retention validation.
- 1.0.1: web top-bar bell (`headerActions`) replaces the nav entry; toasts and the mobile push lifecycle move to `sessionMounts`; `expo-notifications` plugin wired through `scaffold:plugins`.
- 1.1.0: `AddDeliveryFilter` (`DeliveryFilter`, `Hold`): other features hold back push and email per notification; fails open.
- 1.1.1: agent scope for the inbox tools: an AI agent sees, counts and marks read only notifications about what the person lets agents read (`Kind.AgentTarget`, `DataTarget`, else data `{kind, id}`, else the kind's feature); before, the inbox quoted messages and titles from features and items set to Off. `notifications_mark_read` has a preview.
- 1.1.2: push, email and delivery filters run through core `bg.Go` instead of bare goroutines, so server shutdown and `cmd/seed` wait for them (needs the template's `internal/bg`).
- 1.2.0: inbox depth: mark unread, snooze/unsnooze (wake job, push again), groups by thread or resource (`/notifications/groups`, `Kind.Group`, `thread_id`), filter by kind or feature (`?kind=`, `/notifications/kinds`), mute from the inbox through `AllowMute` / `RegisterMuteHandler`, email digest (daily/weekly at a local hour, held by delivery filters); web and mobile inbox and settings (migration `0101_notifications_inbox_depth.sql`).
