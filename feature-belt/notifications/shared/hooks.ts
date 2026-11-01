"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { notificationsEndpoints } from "./api";
import type {
  InboxPage,
  MarkReadRequest,
  NotificationEvent,
  NotificationItem,
  NotificationPreference,
  NotificationPreferences,
  NotificationStats,
  RegisterDeviceRequest,
  SendTestNotificationRequest,
  SendTestNotificationResult,
  UnreadCount,
} from "./types";

declare module "../types" {
  interface RealtimeEventMap {
    "notifications.unread": UnreadCount;
  }
}

/** Query keys. `notificationKeys.all()` invalidates everything this feature caches. */
export const notificationKeys = {
  all: () => ["notifications"] as const,
  inboxAll: () => ["notifications", "inbox"] as const,
  inbox: (unreadOnly: boolean) => ["notifications", "inbox", { unread: unreadOnly }] as const,
  unread: () => ["notifications", "unread"] as const,
  preferences: () => ["notifications", "preferences"] as const,
  admin: {
    stats: () => ["notifications", "admin", "stats"] as const,
  },
};

type InboxCache = InfiniteData<InboxPage, string | undefined>;

function setUnread(qc: QueryClient, count: number) {
  qc.setQueryData<UnreadCount>(notificationKeys.unread(), { unread_count: Math.max(0, count) });
}

/** Apply `fn` to every row of every cached inbox list. */
function mapInbox(qc: QueryClient, fn: (item: NotificationItem) => NotificationItem) {
  qc.setQueriesData<InboxCache>({ queryKey: notificationKeys.inboxAll() }, (data) =>
    data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map(fn) })) } : data,
  );
}

function isEvent(data: unknown): data is NotificationEvent {
  return typeof data === "object" && data !== null && typeof (data as { id?: unknown }).id === "string";
}

// A new notification: prepend it to cached inbox lists and take the new unread count.
registerRealtimeHandler("notification", (qc, e) => {
  const data: unknown = e.data;
  if (!isEvent(data)) return;
  const { user_id: _user, unread_count, ...item } = data;
  setUnread(qc, unread_count);
  qc.setQueriesData<InboxCache>({ queryKey: notificationKeys.inboxAll() }, (cache) => {
    const first = cache?.pages[0];
    if (!cache || !first || cache.pages.some((p) => p.items.some((i) => i.id === item.id))) return cache;
    return { ...cache, pages: [{ ...first, items: [item, ...first.items], unread_count }, ...cache.pages.slice(1)] };
  });
});
registerRealtimeHandler("notifications.unread", (qc, e) => setUnread(qc, e.data.unread_count));
registerReconnectRefetch(notificationKeys.all());

// ---- inbox -------------------------------------------------------------------

/** The inbox, newest first (`data.items`); `unreadOnly` lists unread rows. Keeps the unread count in step. */
export function useInbox(unreadOnly = false, options?: InfiniteOverrides<NotificationItem>): PagedQueryResult<NotificationItem> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  return usePagedQuery<NotificationItem>(
    notificationKeys.inbox(unreadOnly),
    async (cursor, signal) => {
      const page = await n.inbox({ unread: unreadOnly, cursor, limit: 30 }, { signal });
      if (!cursor) setUnread(qc, page.unread_count);
      return page;
    },
    options,
  );
}

/** `GET /v1/notifications/unread-count` (realtime keeps it current; refetched on reconnect). */
export function useUnreadNotifications(options?: QueryOverrides<UnreadCount>): UseQueryResult<UnreadCount, ApiError> {
  const n = useEndpoints(notificationsEndpoints);
  return useApiQuery(notificationKeys.unread(), ({ signal }) => n.unreadCount({ signal }), { staleTime: 60_000, ...options });
}

/** The unread count for badges: a number, or undefined while unknown. */
export function useUnreadNotificationCount(enabled = true): number | undefined {
  return useUnreadNotifications({ enabled }).data?.unread_count;
}

interface MarkReadSnapshot {
  inbox: [readonly unknown[], InboxCache | undefined][];
  unread: UnreadCount | undefined;
}

/** `POST /v1/notifications/read`: optimistic on the inbox and the count, rolled back on failure. */
export function useMarkNotificationsRead(): UseMutationResult<UnreadCount, ApiError, MarkReadRequest, MarkReadSnapshot> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  return useMutation<UnreadCount, ApiError, MarkReadRequest, MarkReadSnapshot>({
    mutationFn: (body) => n.markRead(body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: notificationKeys.inboxAll() });
      const snapshot: MarkReadSnapshot = {
        inbox: qc.getQueriesData<InboxCache>({ queryKey: notificationKeys.inboxAll() }),
        unread: qc.getQueryData<UnreadCount>(notificationKeys.unread()),
      };
      const now = new Date().toISOString();
      const ids = body.all ? null : new Set(body.ids);
      const marked = new Set<string>(); // the same row can sit in several cached lists
      mapInbox(qc, (item) => {
        if (item.read_at || (ids && !ids.has(item.id))) return item;
        marked.add(item.id);
        return { ...item, read_at: now };
      });
      if (snapshot.unread) setUnread(qc, body.all ? 0 : snapshot.unread.unread_count - marked.size);
      return snapshot;
    },
    onSuccess: (res) => setUnread(qc, res.unread_count),
    // The unread-only list drops what was just read.
    onSettled: () => qc.invalidateQueries({ queryKey: notificationKeys.inbox(true) }),
    onError: (_e, _body, snapshot) => {
      if (!snapshot) return;
      for (const [key, data] of snapshot.inbox) qc.setQueryData(key, data);
      if (snapshot.unread) qc.setQueryData(notificationKeys.unread(), snapshot.unread);
    },
  });
}

// ---- preferences ---------------------------------------------------------------

/** Every registered kind with the caller's push and email choice. */
export function useNotificationPreferences(options?: QueryOverrides<NotificationPreferences>): UseQueryResult<NotificationPreferences, ApiError> {
  const n = useEndpoints(notificationsEndpoints);
  return useApiQuery(notificationKeys.preferences(), ({ signal }) => n.preferences({ signal }), options);
}

/** Save one kind's channels, optimistically; rolled back on failure. */
export function useUpdateNotificationPreference(): UseMutationResult<NotificationPreferences, ApiError, Pick<NotificationPreference, "kind" | "push" | "email">, NotificationPreferences | undefined> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  const key = notificationKeys.preferences();
  return useMutation<NotificationPreferences, ApiError, Pick<NotificationPreference, "kind" | "push" | "email">, NotificationPreferences | undefined>({
    mutationFn: (pref) => n.updatePreferences({ items: [pref] }),
    onMutate: async (pref) => {
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<NotificationPreferences>(key);
      if (previous) qc.setQueryData<NotificationPreferences>(key, { items: previous.items.map((p) => (p.kind === pref.kind ? { ...p, push: pref.push, email: pref.email } : p)) });
      return previous;
    },
    onSuccess: (data) => qc.setQueryData(key, data),
    onError: (_e, _pref, previous) => {
      if (previous) qc.setQueryData(key, previous);
    },
  });
}

/** Preferences grouped by module prefix, in server order. */
export function groupPreferences(items: readonly NotificationPreference[]): { group: string; items: NotificationPreference[] }[] {
  const out: { group: string; items: NotificationPreference[] }[] = [];
  for (const item of items) {
    const last = out[out.length - 1];
    if (last && last.group === item.group) last.items.push(item);
    else out.push({ group: item.group, items: [item] });
  }
  return out;
}

/** "chat" → "Chat", "support_desk" → "Support desk". */
export function groupLabel(group: string): string {
  const words = group.replace(/[_-]+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : "Other";
}

// ---- devices -------------------------------------------------------------------

/** `POST /v1/devices`: register this device's Expo push token for the caller. */
export function useRegisterDevice(): UseMutationResult<void, ApiError, RegisterDeviceRequest> {
  const n = useEndpoints(notificationsEndpoints);
  return useMutation<void, ApiError, RegisterDeviceRequest>({ mutationFn: (body) => n.registerDevice(body) });
}

/** `DELETE /v1/devices/{token}`: stop pushes to this device (sign-out). */
export function useUnregisterDevice(): UseMutationResult<void, ApiError, string> {
  const n = useEndpoints(notificationsEndpoints);
  return useMutation<void, ApiError, string>({ mutationFn: (token) => n.unregisterDevice(token) });
}

// ---- admin ---------------------------------------------------------------------

/** `GET /v1/admin/notifications/stats` (staff). */
export function useNotificationStats(options?: QueryOverrides<NotificationStats>): UseQueryResult<NotificationStats, ApiError> {
  const n = useEndpoints(notificationsEndpoints);
  return useApiQuery(notificationKeys.admin.stats(), ({ signal }) => n.admin.stats({ signal }), options);
}

/** `POST /v1/admin/notifications/test` (staff, audited); refreshes the stats. */
export function useSendTestNotification(): UseMutationResult<SendTestNotificationResult, ApiError, SendTestNotificationRequest> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  return useMutation<SendTestNotificationResult, ApiError, SendTestNotificationRequest>({
    mutationFn: (body) => n.admin.sendTest(body),
    onSettled: () => qc.invalidateQueries({ queryKey: notificationKeys.admin.stats() }),
  });
}
