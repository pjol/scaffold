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
  NotificationDigest,
  NotificationGroup,
  NotificationInboxFilter,
  NotificationKinds,
  NotificationMute,
  NotificationTargets,
  SnoozeNotificationsRequest,
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
  inbox: (filter: boolean | NotificationInboxFilter) => ["notifications", "inbox", normalizeFilter(filter)] as const,
  groupsAll: () => ["notifications", "groups"] as const,
  groups: (filter: NotificationInboxFilter) => ["notifications", "groups", normalizeFilter(filter)] as const,
  kinds: (snoozed: boolean) => ["notifications", "kinds", { snoozed }] as const,
  mutes: () => ["notifications", "mutes"] as const,
  digest: () => ["notifications", "digest"] as const,
  unread: () => ["notifications", "unread"] as const,
  preferences: () => ["notifications", "preferences"] as const,
  admin: {
    stats: () => ["notifications", "admin", "stats"] as const,
  },
};

type InboxCache = InfiniteData<InboxPage, string | undefined>;

function normalizeFilter(f: boolean | NotificationInboxFilter): { unread: boolean; snoozed: boolean; kind: string } {
  if (typeof f === "boolean") return { unread: f, snoozed: false, kind: "" };
  return { unread: !!f.unread, snoozed: !!f.snoozed, kind: f.kind ?? "" };
}

/** Whether a new (unread, not snoozed) notification belongs in a cached list with this key. */
function fitsList(key: readonly unknown[], item: NotificationItem): boolean {
  const f = key[2] as ReturnType<typeof normalizeFilter> | undefined;
  if (!f) return true;
  if (f.snoozed) return false;
  return !f.kind || item.kind === f.kind || item.kind.startsWith(`${f.kind}.`);
}

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
  for (const [key, cache] of qc.getQueriesData<InboxCache>({ queryKey: notificationKeys.inboxAll() })) {
    const first = cache?.pages[0];
    if (!cache || !first || !fitsList(key, item)) continue;
    // A snoozed one coming back moves to the top.
    const pages = cache.pages.map((p) => ({ ...p, items: p.items.filter((i) => i.id !== item.id) }));
    qc.setQueryData<InboxCache>(key, { ...cache, pages: [{ ...pages[0]!, items: [item, ...pages[0]!.items], unread_count }, ...pages.slice(1)] });
  }
  // Groups and counts are cheaper to refetch than to patch.
  void qc.invalidateQueries({ queryKey: notificationKeys.groupsAll() });
  void qc.invalidateQueries({ queryKey: ["notifications", "kinds"] });
});
registerRealtimeHandler("notifications.unread", (qc, e) => setUnread(qc, e.data.unread_count));
registerReconnectRefetch(notificationKeys.all());

// ---- inbox -------------------------------------------------------------------

/**
 * The inbox, newest first (`data.items`): `true` (or `{unread: true}`) lists unread rows,
 * `{snoozed: true}` the snoozed ones, `{kind}` one kind or feature. Keeps the unread count in step.
 */
export function useInbox(filter: boolean | NotificationInboxFilter = false, options?: InfiniteOverrides<NotificationItem>): PagedQueryResult<NotificationItem> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  const f = normalizeFilter(filter);
  return usePagedQuery<NotificationItem>(
    notificationKeys.inbox(f),
    async (cursor, signal) => {
      const page = await n.inbox({ ...f, cursor, limit: 30 }, { signal });
      if (!cursor) setUnread(qc, page.unread_count);
      return page;
    },
    options,
  );
}

/** The inbox grouped by thread or resource (GitHub-style), newest group first. */
export function useNotificationGroups(filter: NotificationInboxFilter = {}, options?: InfiniteOverrides<NotificationGroup>): PagedQueryResult<NotificationGroup> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  const f = normalizeFilter(filter);
  return usePagedQuery<NotificationGroup>(
    notificationKeys.groups(f),
    async (cursor, signal) => {
      const page = await n.groups({ ...f, cursor, limit: 30 }, { signal });
      if (!cursor) setUnread(qc, page.unread_count);
      return page;
    },
    options,
  );
}

/** Per-feature counts for the inbox's filter chips, and how many are snoozed. */
export function useNotificationKinds(snoozed = false, options?: QueryOverrides<NotificationKinds>): UseQueryResult<NotificationKinds, ApiError> {
  const n = useEndpoints(notificationsEndpoints);
  return useApiQuery(notificationKeys.kinds(snoozed), ({ signal }) => n.kinds(snoozed, { signal }), { staleTime: 30_000, ...options });
}

/** After a change that moves rows between lists: refetch lists, groups and chips; take the new count. */
function useInboxChange<V>(fn: (v: V) => Promise<UnreadCount>, hide?: (v: V) => (item: NotificationItem) => boolean): UseMutationResult<UnreadCount, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<UnreadCount, ApiError, V>({
    mutationFn: fn,
    onMutate: (v) => {
      if (!hide) return;
      const gone = hide(v);
      qc.setQueriesData<InboxCache>({ queryKey: notificationKeys.inboxAll() }, (data) =>
        data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((i) => !gone(i)) })) } : data,
      );
    },
    onSuccess: (res) => setUnread(qc, res.unread_count),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: notificationKeys.inboxAll() });
      void qc.invalidateQueries({ queryKey: notificationKeys.groupsAll() });
      void qc.invalidateQueries({ queryKey: ["notifications", "kinds"] });
    },
  });
}

function targets(t: NotificationTargets): (item: NotificationItem) => boolean {
  if (t.ids) {
    const ids = new Set(t.ids);
    return (i) => ids.has(i.id);
  }
  const keys = t.resources ?? [];
  return (i) => keys.some((k) => i.id === k || i.resource === k || i.resource.startsWith(`${k}/thread:`));
}

/** `POST /v1/notifications/unread`: read ones turn unread. */
export function useMarkNotificationsUnread(): UseMutationResult<UnreadCount, ApiError, NotificationTargets> {
  const n = useEndpoints(notificationsEndpoints);
  return useInboxChange((t: NotificationTargets) => n.markUnread(t));
}

/** Snooze until a time (≤ 30 days): they leave the inbox now and come back unread at the top. */
export function useSnoozeNotifications(): UseMutationResult<UnreadCount, ApiError, SnoozeNotificationsRequest> {
  const n = useEndpoints(notificationsEndpoints);
  return useInboxChange((b: SnoozeNotificationsRequest) => n.snooze(b), (b) => targets(b));
}

/** Bring snoozed notifications back now. */
export function useUnsnoozeNotifications(): UseMutationResult<UnreadCount, ApiError, NotificationTargets> {
  const n = useEndpoints(notificationsEndpoints);
  return useInboxChange((t: NotificationTargets) => n.unsnooze(t), (t) => targets(t));
}

/** Mute a resource from the inbox (rows with `mutable`): its unread ones turn read and new ones stop. */
export function useMuteNotificationResource(): UseMutationResult<UnreadCount, ApiError, { resource: string; label: string }> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  return useMutation<UnreadCount, ApiError, { resource: string; label: string }>({
    mutationFn: (b) => n.mute(b),
    onSuccess: (res) => setUnread(qc, res.unread_count),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: notificationKeys.inboxAll() });
      void qc.invalidateQueries({ queryKey: notificationKeys.groupsAll() });
      void qc.invalidateQueries({ queryKey: notificationKeys.mutes() });
    },
  });
}

/** Resources you muted from the inbox. */
export function useNotificationMutes(options?: QueryOverrides<{ items: NotificationMute[] }>): UseQueryResult<{ items: NotificationMute[] }, ApiError> {
  const n = useEndpoints(notificationsEndpoints);
  return useApiQuery(notificationKeys.mutes(), ({ signal }) => n.mutes({ signal }), options);
}

/** Unmute (optimistic on the mutes list). */
export function useUnmuteNotificationResource(): UseMutationResult<void, ApiError, string, { items: NotificationMute[] } | undefined> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string, { items: NotificationMute[] } | undefined>({
    mutationFn: (resource) => n.unmute(resource),
    onMutate: (resource) => {
      const previous = qc.getQueryData<{ items: NotificationMute[] }>(notificationKeys.mutes());
      if (previous) qc.setQueryData(notificationKeys.mutes(), { items: previous.items.filter((m) => m.resource !== resource) });
      return previous;
    },
    onError: (_e, _r, previous) => {
      if (previous) qc.setQueryData(notificationKeys.mutes(), previous);
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: notificationKeys.inboxAll() }),
  });
}

/** Your email digest setting (`cadence: "off"` until you choose one). */
export function useNotificationDigest(options?: QueryOverrides<NotificationDigest>): UseQueryResult<NotificationDigest, ApiError> {
  const n = useEndpoints(notificationsEndpoints);
  return useApiQuery(notificationKeys.digest(), ({ signal }) => n.digest({ signal }), options);
}

export function useUpdateNotificationDigest(): UseMutationResult<NotificationDigest, ApiError, NotificationDigest> {
  const n = useEndpoints(notificationsEndpoints);
  const qc = useQueryClient();
  return useMutation<NotificationDigest, ApiError, NotificationDigest>({
    mutationFn: (body) => n.updateDigest(body),
    onSuccess: (d) => qc.setQueryData(notificationKeys.digest(), d),
  });
}

/** Snooze choices: later today (+3 h), this evening (18:00), tomorrow (9:00), next week (Monday 9:00), in the viewer's zone. */
export function notificationSnoozeOptions(now: Date = new Date()): { label: string; until: string }[] {
  const at = (days: number, hour: number) => {
    const d = new Date(now.getFullYear(), now.getMonth(), now.getDate() + days, hour, 0, 0, 0);
    return d;
  };
  const out: { label: string; until: Date }[] = [{ label: "In 3 hours", until: new Date(now.getTime() + 3 * 3_600_000) }];
  if (now.getHours() < 17) out.push({ label: "This evening", until: at(0, 18) });
  out.push({ label: "Tomorrow", until: at(1, 9) });
  const toMonday = ((8 - now.getDay()) % 7) || 7;
  out.push({ label: "Next week", until: at(toMonday, 9) });
  return out.map((o) => ({ label: o.label, until: o.until.toISOString() }));
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
      const match = body.all ? null : targets(body.resources ? { resources: body.resources } : { ids: body.ids ?? [] });
      const marked = new Set<string>(); // the same row can sit in several cached lists
      mapInbox(qc, (item) => {
        if (item.read_at || (match && !match(item))) return item;
        marked.add(item.id);
        return { ...item, read_at: now };
      });
      if (snapshot.unread) setUnread(qc, body.all ? 0 : snapshot.unread.unread_count - marked.size);
      return snapshot;
    },
    onSuccess: (res) => setUnread(qc, res.unread_count),
    // The unread-only lists drop what was just read; groups recount.
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: notificationKeys.inboxAll(), predicate: (q) => (q.queryKey[2] as { unread?: boolean } | undefined)?.unread === true });
      void qc.invalidateQueries({ queryKey: notificationKeys.groupsAll() });
      void qc.invalidateQueries({ queryKey: ["notifications", "kinds"] });
    },
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

/** This device's IANA time zone, for the digest ("UTC" when the runtime doesn't say). */
export function notificationDeviceZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}
