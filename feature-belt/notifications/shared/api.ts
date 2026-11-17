import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type {
  InboxPage,
  InboxParams,
  MarkReadRequest,
  NotificationDigest,
  NotificationGroupsPage,
  NotificationKinds,
  NotificationMute,
  NotificationTargets,
  SnoozeNotificationsRequest,
  NotificationPreferences,
  NotificationStats,
  RegisterDeviceRequest,
  SendTestNotificationRequest,
  SendTestNotificationResult,
  UnreadCount,
  UpdatePreferencesRequest,
} from "./types";

/** Notifications endpoints: devices, inbox, preferences; stats and test sends for staff. */
export function notificationsEndpoints(http: HttpClient) {
  const A = "/v1/admin/notifications";
  return {
    /** `POST /v1/devices` (consent-exempt): register or move this device's Expo push token. */
    registerDevice: (body: RegisterDeviceRequest, o?: CallOptions) => http.post<void>("/v1/devices", body, o),
    /** `DELETE /v1/devices/{token}` (consent-exempt, idempotent): call before signing out. */
    unregisterDevice: (token: string, o?: CallOptions) => http.delete<void>(`/v1/devices/${seg(token)}`, o),
    /** `GET /v1/notifications`, newest first, with the unread count. */
    inbox: (params: InboxParams = {}, o?: CallOptions) => http.get<InboxPage>("/v1/notifications", { ...o, query: inboxQuery(params) }),
    /** `GET /v1/notifications/groups`: the inbox grouped by thread or resource, newest group first. */
    groups: (params: InboxParams = {}, o?: CallOptions) => http.get<NotificationGroupsPage>("/v1/notifications/groups", { ...o, query: inboxQuery(params) }),
    /** `GET /v1/notifications/kinds`: per-feature counts for filter chips, and how many are snoozed. */
    kinds: (snoozed = false, o?: CallOptions) => http.get<NotificationKinds>("/v1/notifications/kinds", { ...o, query: { snoozed: snoozed ? "true" : undefined } }),
    /** `POST /v1/notifications/unread`: read ones turn unread → the new unread count. */
    markUnread: (body: NotificationTargets, o?: CallOptions) => http.post<UnreadCount>("/v1/notifications/unread", body, o),
    /** `POST /v1/notifications/snooze`: hidden until `until` (≤ 30 days), then back unread at the top. */
    snooze: (body: SnoozeNotificationsRequest, o?: CallOptions) => http.post<UnreadCount>("/v1/notifications/snooze", body, o),
    unsnooze: (body: NotificationTargets, o?: CallOptions) => http.post<UnreadCount>("/v1/notifications/unsnooze", body, o),
    /** `GET /v1/notifications/mutes`: resources muted from the inbox. */
    mutes: (o?: CallOptions) => http.get<{ items: NotificationMute[] }>("/v1/notifications/mutes", o),
    /** Mute a resource you were notified about (when `mutable`): its unread ones turn read, new ones stop. */
    mute: (body: { resource: string; label: string }, o?: CallOptions) => http.post<UnreadCount>("/v1/notifications/mutes", body, o),
    unmute: (resource: string, o?: CallOptions) => http.delete<void>("/v1/notifications/mutes", { ...o, query: { resource } }),
    digest: (o?: CallOptions) => http.get<NotificationDigest>("/v1/notifications/digest", o),
    updateDigest: (body: NotificationDigest, o?: CallOptions) => http.put<NotificationDigest>("/v1/notifications/digest", body, o),
    /** `GET /v1/notifications/unread-count`. */
    unreadCount: (o?: CallOptions) => http.get<UnreadCount>("/v1/notifications/unread-count", o),
    /** `POST /v1/notifications/read` (consent-exempt): ids or all → the new unread count. */
    markRead: (body: MarkReadRequest, o?: CallOptions) => http.post<UnreadCount>("/v1/notifications/read", body, o),
    /** `GET /v1/notifications/preferences`: every registered kind with the caller's channels. */
    preferences: (o?: CallOptions) => http.get<NotificationPreferences>("/v1/notifications/preferences", o),
    /** `PUT /v1/notifications/preferences` → the full list. */
    updatePreferences: (body: UpdatePreferencesRequest, o?: CallOptions) => http.put<NotificationPreferences>("/v1/notifications/preferences", body, o),
    admin: {
      /** `GET /v1/admin/notifications/stats`. */
      stats: (o?: CallOptions) => http.get<NotificationStats>(`${A}/stats`, o),
      /** `POST /v1/admin/notifications/test`: inbox + push to every device, no email. Audited. */
      sendTest: (body: SendTestNotificationRequest, o?: CallOptions) => http.post<SendTestNotificationResult>(`${A}/test`, body, o),
    },
  };
}

function inboxQuery(p: InboxParams) {
  return { unread: p.unread ? "true" : undefined, snoozed: p.snoozed ? "true" : undefined, kind: p.kind || undefined, limit: p.limit, cursor: p.cursor };
}

export type NotificationsEndpoints = ReturnType<typeof notificationsEndpoints>;
