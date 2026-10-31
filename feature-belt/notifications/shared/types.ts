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

export interface InboxParams {
  /** Only unread rows. */
  unread?: boolean | undefined;
  limit?: number | undefined;
  cursor?: string | undefined;
}

/** `POST /v1/notifications/read`: ids (≤ 500) or `all`, never both. */
export type MarkReadRequest = { ids: string[]; all?: never } | { all: true; ids?: never };

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
