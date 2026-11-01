# Notifications

## What it does

- Every `d.Notify.Send` lands in the user's inbox (always on) and arrives live with the new unread count: a bell with a badge in the top bar and an Inbox tile on Home (web), the Home row and the app icon (mobile), and a toast while the app is open.
- Push to the user's phones through the Expo push service. The app offers push from the inbox and notification settings ("Turn on"), never at launch; tapping a push opens its link.
- Email for kinds that allow it, when the user leaves email on.
- Settings → Notifications: push and email per kind, grouped by feature. Modules declare kinds and defaults with `RegisterKind`.
- Tokens are registered after sign-in, removed on sign-out, and pruned when Expo reports them dead.
- Admin → Notifications: delivery counts, busiest kinds, and a test send to one person (confirmed, audited).

## Data

| Table | Purpose |
|---|---|
| `notifications` | Inbox rows: kind, title, body, link, data, `read_at`. Read rows older than `NOTIFICATIONS_RETENTION` are pruned hourly. |
| `notification_devices` | Expo push tokens (PK), owner, platform ios/android/web, `last_seen_at`. 10 per user; re-registering from another account moves the token. |
| `notification_preferences` | Saved push/email per (user, kind). No row = the kind's registered defaults. |
| `notification_push_tickets` | Expo tickets awaiting the receipts check (15 min after send, dropped after 24 h). |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST | `/v1/devices` | user, consent-exempt, 30/h | `{token, platform}` register or move an Expo token → 204 |
| DELETE | `/v1/devices/{token}` | user, consent-exempt | Remove the caller's token (URL-encoded) → 204 |
| GET | `/v1/notifications` | user | `{items, next_cursor, unread_count}`; `?unread=true`, `?limit`, `?cursor` |
| GET | `/v1/notifications/unread-count` | user | `{unread_count}` |
| POST | `/v1/notifications/read` | user, consent-exempt | `{ids}` (≤ 500) or `{all: true}` → `{unread_count}` |
| GET / PUT | `/v1/notifications/preferences` | user (PUT 60/min) | `{items: [{kind, group, label, push, email}]}`; PUT saves the listed kinds |
| GET | `/v1/admin/notifications/stats` | support | push mode, totals, unread, devices by platform, pending receipts, top kinds (7 days) |
| POST | `/v1/admin/notifications/test` | support, audited | `{user_id, title?, body?}` → `{notification_id, devices, push_mode}`; ignores preferences, no email |

Realtime on `user:<id>`: `notification` (inbox item + `user_id` + `unread_count`, a superset of core's `InAppNotification`) and `notifications.unread` `{unread_count}` after every change. Push `data` = the notification's `data` + `notification_id`, `kind`, `link`; `badge` = unread count.

Shared (`@scaffoldapp/shared`): `notificationsEndpoints`, `notificationKeys`, `useInbox(unreadOnly)`, `useUnreadNotificationCount`, `useMarkNotificationsRead` (optimistic), `useNotificationPreferences`, `useUpdateNotificationPreference` (optimistic), `groupPreferences`, `groupLabel`, `useRegisterDevice`, `useUnregisterDevice`, `useNotificationStats`, `useSendTestNotification`. Realtime handlers update the inbox and count caches; reconnect refetches.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Inbox | `/notifications`: All/Unread (in the URL), Mark all read, rows open their link | `app/notifications.tsx` (Home row "Inbox" with badge): same, pull to refresh, push banner |
| Settings | `/settings/notifications` (registry section; emails link here): switches per kind and channel | `app/settings/notifications.tsx`: push banner, a group per kind with Push/Email switches |
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

- Deletion: inbox, devices, preferences and pending tickets are deleted. Export: preferences, devices (platform and dates, never tokens), latest 500 inbox rows.
- Push is optional and asked for at the moment of use; denial leaves the inbox working and shows a Settings link.
- Privacy policy: name Expo (push relay) and the mail provider.

## Stitching

1. Register each kind your modules send: `notifications.RegisterKind(notifications.Kind{ID: "chat.message", Label: "Messages", Push: true})` in the module's `New` (or in `internal/api/feature_<name>.go` for a feature that doesn't require notifications). Leave a kind unregistered when its module emails on its own (`support.reply`): unregistered kinds push and never email.
2. Give every notification a `Link` that is a route on both web and mobile. Titles and bodies show on lock screens: keep them factual.
3. Mobile push: run `eas init` (writes `extra.eas.projectId`; without it the banner stays hidden and no token is fetched), configure push credentials (APNs key in EAS, FCM v1 for Android) and rebuild the dev client. The config plugin is already wired; give it `icon`/`color` options for a branded Android icon. Real pushes need a device build.
4. Mobile placement: Inbox is a Home row. If notifications are a primary destination, add a tab (`<Tabs.Screen name="notifications" …>` with a route under `app/(tabs)/`) and put the badge there; the push lifecycle is a session mount and needs no change.
5. Ask for push at a moment that explains itself (after a first message, after opening a support request): render `PushBanner` from `src/features/notifications/PushBanner.tsx` there.

## Extending

- `Sender` is the `notify.Sender`; add channels in `sender.go` (web push for `platform = web`; the Expo sender skips web tokens).
- Kinds only through `RegisterKind`; put ids in `data`, not new columns. Don't call Expo from other modules.
- Web toasts: `web/src/features/notifications/toasts.ts`; mobile lifecycle: `mobile/src/features/notifications/lifecycle.ts`.

## Provenance

- DOGPATCH `internal/notify` (inbox + realtime + async push in one call), `accounts/devices.go` (token regex, move on re-register, per-user cap), `accounts/notifications.go` (keyset inbox with unread count).
- SFLUV tickets/receipts (`DeviceNotRegistered` deactivation), notification ledger, test-mode file sink, socket-first foreground suppression; THASSA `internal/push` (batches of 100).
- Fixed: sending continues past a failed batch or token (ASSEMBLY stopped at the first error); 429/5xx retried with backoff; receipts are actually checked; deleted users receive nothing; Expo messages that quote tokens are never logged; preferences per kind × channel added; push permission asked at use, not launch.
- Unverified here: delivery to a real device (no device build in this pass; the sink and a fake-Expo unit test cover the server side), Android channel behaviour on a device.

## Changes

- 1.0.0: shared hooks, web and mobile screens, admin page, retention validation.
- 1.0.1: web top-bar bell (`headerActions`) replaces the nav entry; toasts and the mobile push lifecycle move to `sessionMounts`; `expo-notifications` plugin wired through `scaffold:plugins`.
