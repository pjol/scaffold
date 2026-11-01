import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type {
  InboxPage,
  InboxParams,
  MarkReadRequest,
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
    inbox: (params: InboxParams = {}, o?: CallOptions) =>
      http.get<InboxPage>("/v1/notifications", { ...o, query: { unread: params.unread ? "true" : undefined, limit: params.limit, cursor: params.cursor } }),
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

export type NotificationsEndpoints = ReturnType<typeof notificationsEndpoints>;
