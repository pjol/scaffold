import type { Page } from "../types";

/** Where a push token lives. The Expo sender delivers to `ios` and `android`. */
export type DevicePlatform = "ios" | "android" | "web";

/** One inbox row (`notifications.Item`). */
export interface NotificationItem {
  id: string;
  /** Stable dotted id, e.g. `chat.message`. */
  kind: string;
  title: string;
  body: string;
  /** App path the notification opens (`/chat/<id>`), or "". */
  link: string;
  data: Record<string, unknown>;
  read_at: string | null;
  created_at: string;
  /** Inbox order: `created_at`, or when a snooze ended. */
  sort_at: string;
  /** Hidden until then (the snoozed view lists it). */
  snoozed_until: string | null;
  /** What it is about, `<kind>:<id>` (`/thread:<id>` for a thread); "" for nothing in particular. */
  resource: string;
  /** The person can mute `resource` from the inbox. */
  mutable: boolean;
}

/**
 * The `notification` realtime event as this feature sends it: the inbox item plus
 * `user_id` and the new `unread_count` (a superset of core's `InAppNotification`).
 */
export interface NotificationEvent extends NotificationItem {
  user_id: string;
  unread_count: number;
}

/** `{unread_count}`: the `notifications.unread` event and the mark-read response. */
export interface UnreadCount {
  unread_count: number;
}

/** `GET /v1/notifications`. */
export interface InboxPage extends Page<NotificationItem> {
  unread_count: number;
}

/** Which notifications an inbox list shows. */
export interface NotificationInboxFilter {
  /** Only unread rows. */
  unread?: boolean | undefined;
  /** Only snoozed rows (hidden elsewhere until they wake). */
  snoozed?: boolean | undefined;
  /** One kind (`channels.mention`) or one feature (`channels`). */
  kind?: string | undefined;
}

export interface InboxParams extends NotificationInboxFilter {
  limit?: number | undefined;
  cursor?: string | undefined;
}

/** Notifications about one thread or resource (`GET /v1/notifications/groups`). */
export interface NotificationGroup {
  /** The resource, or the notification id for a group of one. */
  key: string;
  count: number;
  unread: number;
  latest: NotificationItem;
}

export interface NotificationGroupsPage extends Page<NotificationGroup> {
  unread_count: number;
}

/** A filter chip: one feature's notifications in the inbox. */
export interface NotificationKindCount {
  /** Feature prefix (`channels`). */
  group: string;
  count: number;
  unread: number;
}

export interface NotificationKinds {
  items: NotificationKindCount[];
  /** How many are snoozed. */
  snoozed: number;
}

/** Ids, or every notification about these resources (group keys). */
export type NotificationTargets = { ids: string[]; resources?: never } | { resources: string[]; ids?: never };

export type SnoozeNotificationsRequest = NotificationTargets & { until: string };

/** A resource muted from the inbox (settings list). */
export interface NotificationMute {
  resource: string;
  label: string;
  created_at: string;
}

export type NotificationDigestCadence = "off" | "daily" | "weekly";

/** `GET/PUT /v1/notifications/digest`: an email of unread notifications at a local hour. */
export interface NotificationDigest {
  cadence: NotificationDigestCadence;
  /** Local hour, 0–23. */
  hour: number;
  /** Weekly: 0 = Sunday. */
  weekday: number;
  time_zone: string;
  next_at?: string | null;
  last_sent_at?: string | null;
}

/** `POST /v1/notifications/read`: ids (≤ 500), group keys (`resources`) or `all`, one of them. */
export type MarkReadRequest =
  | { ids: string[]; all?: never; resources?: never }
  | { all: true; ids?: never; resources?: never }
  | { resources: string[]; ids?: never; all?: never };

/** One kind's channels for the caller. The inbox is always on. */
export interface NotificationPreference {
  kind: string;
  /** Module prefix of `kind` (`chat` for `chat.message`), for grouping rows. */
  group: string;
  label: string;
  push: boolean;
  email: boolean;
}

export interface NotificationPreferences {
  items: NotificationPreference[];
}

/** `PUT /v1/notifications/preferences`: the listed kinds are saved, others keep their value. */
export interface UpdatePreferencesRequest {
  items: { kind: string; push: boolean; email: boolean }[];
}

/** `POST /v1/devices`. */
export interface RegisterDeviceRequest {
  /** Expo push token (`ExponentPushToken[…]`). */
  token: string;
  platform: DevicePlatform;
}

/** How the backend delivers pushes: Expo, a development file sink, or the log only. */
export type PushMode = "expo" | "sink" | "log";

/** `GET /v1/admin/notifications/stats`. */
export interface NotificationStats {
  push_mode: PushMode;
  notifications: number;
  notifications_7d: number;
  unread: number;
  devices: Record<DevicePlatform, number>;
  users_with_devices: number;
  pending_receipts: number;
  kinds_7d: { kind: string; count: number }[];
}

/** `POST /v1/admin/notifications/test`. */
export interface SendTestNotificationRequest {
  user_id: string;
  title?: string;
  body?: string;
}

export interface SendTestNotificationResult {
  notification_id: string;
  /** iOS and Android devices the push went to (0 = inbox only). */
  devices: number;
  push_mode: PushMode;
}
