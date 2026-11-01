"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
  type QueryKey,
  type UseInfiniteQueryResult,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { REALTIME_FALLBACK_POLL_MS, useApiQuery, useConnectionState, usePagedQuery, useTopic, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { queryKeys } from "../hooks/keys";
import { registerRealtimeHandler, registerReconnectRefetch } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import type { Me, Page } from "../types";
import { generateId } from "../utils/id";
import { channelsEndpoints } from "./api";
import { channelsAttachmentKind, compareChannelsMessages } from "./format";
import type {
  ChannelsAdminChannel,
  ChannelsAttachmentUrl,
  ChannelsBrowseChannel,
  ChannelsCachedMessage,
  ChannelsCreateRequest,
  ChannelsDetail,
  ChannelsEditRequest,
  ChannelsFile,
  ChannelsGroup,
  ChannelsLaterItem,
  ChannelsMember,
  ChannelsMessage,
  ChannelsMessageContext,
  ChannelsMessagePage,
  ChannelsPerson,
  ChannelsPrefsRequest,
  ChannelsReadMark,
  ChannelsRef,
  ChannelsSearchHit,
  ChannelsSendInput,
  ChannelsThreadSummary,
  ChannelsUnread,
  ChannelsUpdateRequest,
  ChannelsUser,
  ChannelsWindowParams,
  ChannelsWorkspace,
} from "./types";

// ───────────────────────────────────────────────────────────── keys ──

export const channelsKeys = {
  all: () => ["channels"] as const,
  unread: () => ["channels", "unread"] as const,
  workspace: (orgId: string) => ["channels", "workspace", orgId] as const,
  browse: (orgId: string, q: string, archived: boolean) => ["channels", "browse", orgId, q, archived] as const,
  channel: (id: string) => ["channels", "channel", id] as const,
  messagesAll: (id: string) => ["channels", "messages", id] as const,
  /** `around` = a message id the window is centred on (jump to message); null = newest. */
  messages: (id: string, around?: string | null) => ["channels", "messages", id, around ?? "latest"] as const,
  topicsAll: (id: string) => ["channels", "topics", id] as const,
  topics: (id: string, tag: string) => ["channels", "topics", id, tag] as const,
  threadAll: (rootId: string) => ["channels", "thread", rootId] as const,
  thread: (rootId: string, around?: string | null) => ["channels", "thread", rootId, around ?? "latest"] as const,
  members: (id: string, q: string) => ["channels", "members", id, q] as const,
  pins: (id: string) => ["channels", "pins", id] as const,
  files: (id: string) => ["channels", "files", id] as const,
  threads: (orgId: string) => ["channels", "threads", orgId] as const,
  later: (orgId: string, state: "open" | "done") => ["channels", "later", orgId, state] as const,
  search: (orgId: string, q: string, channelId?: string | null) => ["channels", "search", orgId, q, channelId ?? "all"] as const,
  people: (orgId: string, q: string, channelId?: string | null) => ["channels", "people", orgId, q, channelId ?? "none"] as const,
  groups: (orgId: string, q: string) => ["channels", "groups", orgId, q] as const,
  message: (id: string) => ["channels", "message", id] as const,
  attachment: (id: string) => ["channels", "attachment", id] as const,
  admin: (id: string, params: object = {}) => ["channels", "admin", id, params] as const,
};

registerReconnectRefetch(channelsKeys.all());

// ───────────────────────────────────────────────────────────── cache shapes ──

export interface ChannelsCachePage extends Omit<ChannelsMessagePage, "items"> {
  items: ChannelsCachedMessage[];
}
/** A thread page: replies plus the thread's root and your state in it. */
export interface ChannelsThreadPage extends ChannelsCachePage {
  root: ChannelsMessage;
  channel: ChannelsRef;
  following: boolean;
  last_read: ChannelsReadMark | null;
}
export type ChannelsPageParam = { before?: string; after?: string; around?: string } | undefined;
export type ChannelsTimelineData = InfiniteData<ChannelsCachePage, ChannelsPageParam>;
export type ChannelsThreadData = InfiniteData<ChannelsThreadPage, ChannelsPageParam>;
type AnyPagesData = InfiniteData<ChannelsCachePage, ChannelsPageParam>;
type TopicsData = InfiniteData<Page<ChannelsMessage>, string | undefined>;

/** Every loaded message of a timeline or thread, oldest first (optimistic sends last). */
export function flattenChannelsPages(data: { pages: ChannelsCachePage[] } | undefined): ChannelsCachedMessage[] {
  if (!data) return [];
  const out: ChannelsCachedMessage[] = [];
  const seen = new Set<string>();
  for (let i = data.pages.length - 1; i >= 0; i--) {
    for (const m of data.pages[i]?.items ?? []) {
      if (seen.has(m.id)) continue;
      seen.add(m.id);
      out.push(m);
    }
  }
  return out;
}

function meId(qc: QueryClient): string | undefined {
  return qc.getQueryData<Me>(queryKeys.me())?.id;
}

function isHidden(qc: QueryClient, channelId: string, userId: string | null | undefined): boolean {
  if (!userId) return false;
  return qc.getQueryData<ChannelsDetail>(channelsKeys.channel(channelId))?.hidden_user_ids.includes(userId) ?? false;
}

function sameMessage(a: ChannelsCachedMessage, b: ChannelsMessage): boolean {
  return a.id === b.id || (!!b.client_id && a.client_id === b.client_id && a.sender?.id === b.sender?.id);
}

/** Replace a message wherever it is (by id or client_id), or insert it into the newest window. */
function upsert<T extends AnyPagesData>(data: T, msg: ChannelsCachedMessage): T {
  let replaced = false;
  const pages = data.pages.map((p) => {
    const idx = p.items.findIndex((m) => sameMessage(m, msg));
    if (idx < 0) return p;
    replaced = true;
    const items = p.items.slice();
    items[idx] = msg;
    return { ...p, items };
  });
  if (replaced) return { ...data, pages };
  const first = pages[0];
  if (!first || first.has_newer) return data;
  const items = first.items.slice();
  let at = items.length;
  while (at > 0) {
    const prev = items[at - 1];
    if (!prev || (!prev.status && compareChannelsMessages(prev, msg) <= 0) || msg.status) break;
    at--;
  }
  items.splice(at, 0, msg);
  return { ...data, pages: [{ ...first, items }, ...pages.slice(1)] };
}

function mapMessage<T extends AnyPagesData>(data: T, id: string, fn: (m: ChannelsCachedMessage) => ChannelsCachedMessage): T {
  return { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map((m) => (m.id === id ? fn(m) : m)) })) };
}

function updateTimelines(qc: QueryClient, channelId: string, fn: (d: ChannelsTimelineData) => ChannelsTimelineData): void {
  qc.setQueriesData<ChannelsTimelineData>({ queryKey: channelsKeys.messagesAll(channelId) }, (old) => (old ? fn(old) : old));
}

function updateThreads(qc: QueryClient, rootId: string, fn: (d: ChannelsThreadData) => ChannelsThreadData): void {
  qc.setQueriesData<ChannelsThreadData>({ queryKey: channelsKeys.threadAll(rootId) }, (old) => (old ? fn(old) : old));
}

/** Apply `fn` to a message in every cache that holds it (timeline, thread, thread root, forum list). */
function updateEverywhere(qc: QueryClient, m: Pick<ChannelsMessage, "id" | "channel_id" | "thread_root_id">, fn: (x: ChannelsCachedMessage) => ChannelsCachedMessage): void {
  updateTimelines(qc, m.channel_id, (d) => mapMessage(d, m.id, fn));
  if (m.thread_root_id) updateThreads(qc, m.thread_root_id, (d) => mapMessage(d, m.id, fn));
  else updateThreads(qc, m.id, (d) => ({ ...d, pages: d.pages.map((p) => ({ ...p, root: fn(p.root) })) }));
  qc.setQueriesData<TopicsData>({ queryKey: channelsKeys.topicsAll(m.channel_id) }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.map((x) => (x.id === m.id ? (fn(x) as ChannelsMessage) : x)) })) } : old,
  );
}

function updateSidebarRow(qc: QueryClient, orgId: string, channelId: string, fn: (c: ChannelsWorkspace["channels"][number]) => ChannelsWorkspace["channels"][number]): boolean {
  let found = false;
  qc.setQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId), (old) => {
    if (!old) return old;
    return {
      ...old,
      channels: old.channels.map((c) => {
        if (c.id !== channelId) return c;
        found = true;
        return fn(c);
      }),
    };
  });
  return found;
}

// ───────────────────────────────────────────────────────────── typing store ──

/** A typing indicator lasts this long without a new signal. */
export const CHANNELS_TYPING_TTL_MS = 6_000;
/** Senders repeat the signal at most this often while typing. */
export const CHANNELS_TYPING_HEARTBEAT_MS = 3_000;

const typing = new Map<string, Map<string, { user: ChannelsUser; timer: ReturnType<typeof setTimeout> }>>();
const typingSnapshots = new Map<string, ChannelsUser[]>();
const typingListeners = new Set<() => void>();
const NO_ONE: ChannelsUser[] = [];
const typingKey = (channelId: string, rootId?: string | null) => `${channelId}:${rootId ?? ""}`;

function setTyping(key: string, user: ChannelsUser, on: boolean): void {
  let users = typing.get(key);
  const existing = users?.get(user.id);
  if (existing) clearTimeout(existing.timer);
  if (!on && !existing) return;
  if (on) {
    if (!users) {
      users = new Map();
      typing.set(key, users);
    }
    users.set(user.id, { user, timer: setTimeout(() => setTyping(key, user, false), CHANNELS_TYPING_TTL_MS) });
  } else {
    users?.delete(user.id);
  }
  typingSnapshots.set(key, users ? [...users.values()].map((e) => e.user) : NO_ONE);
  for (const l of typingListeners) l();
}

function subscribeTyping(listener: () => void): () => void {
  typingListeners.add(listener);
  return () => typingListeners.delete(listener);
}

/** People typing in a channel (or one of its threads) right now, never you. */
export function useChannelsTypingUsers(channelId: string | null | undefined, rootId?: string | null): ChannelsUser[] {
  return useSyncExternalStore(
    subscribeTyping,
    () => (channelId ? (typingSnapshots.get(typingKey(channelId, rootId)) ?? NO_ONE) : NO_ONE),
    () => NO_ONE,
  );
}

// ───────────────────────────────────────────────────────────── realtime ──

let activeChannel: string | null = null;
let activeThread: string | null = null;

registerRealtimeHandler("channels.message", (qc, e) => {
  const msg = e.data;
  if (isHidden(qc, msg.channel_id, msg.sender?.id)) return;
  if (msg.sender) setTyping(typingKey(msg.channel_id, msg.thread_root_id), msg.sender, false);
  if (!msg.thread_root_id || msg.also_in_channel) updateTimelines(qc, msg.channel_id, (d) => upsert(d, msg));
  if (msg.thread_root_id) updateThreads(qc, msg.thread_root_id, (d) => upsert(d, msg));
  else if (msg.title !== null) void qc.invalidateQueries({ queryKey: channelsKeys.topicsAll(msg.channel_id) });
});

registerRealtimeHandler("channels.message_updated", (qc, e) => {
  const msg = e.data;
  if (isHidden(qc, msg.channel_id, msg.sender?.id)) return;
  // Viewer-specific fields (saved) aren't in broadcast events: keep the cached value.
  updateEverywhere(qc, msg, (old) => ({ ...msg, saved: old.saved }));
  if (!msg.thread_root_id && msg.reply_count > 0) void qc.invalidateQueries({ queryKey: channelsKeys.topicsAll(msg.channel_id) });
});

registerRealtimeHandler("channels.typing", (qc, e) => {
  const { channel_id, thread_root_id, user } = e.data;
  if (user.id === meId(qc) || isHidden(qc, channel_id, user.id)) return;
  setTyping(typingKey(channel_id, thread_root_id), user, true);
});

registerRealtimeHandler("channels.channel", (qc, e) => {
  const { channel_id, reason } = e.data;
  void qc.invalidateQueries({ queryKey: channelsKeys.channel(channel_id) });
  if (reason === "members") void qc.invalidateQueries({ queryKey: ["channels", "members", channel_id] });
  if (reason === "pins") void qc.invalidateQueries({ queryKey: channelsKeys.pins(channel_id) });
});

registerRealtimeHandler("channels.sidebar", (qc, e) => {
  const { org_id, channel_id, reason, message, mention } = e.data;
  if (reason === "message" && message) {
    const me = meId(qc);
    const own = !!me && message.sender_id === me;
    const timeline = !message.thread_root_id || message.also_in_channel;
    const watching = activeChannel === channel_id;
    let bumpedMention = false;
    const found = updateSidebarRow(qc, org_id, channel_id, (c) => {
      const next = { ...c, last_message_at: message.created_at > c.last_message_at ? message.created_at : c.last_message_at };
      if (own || !timeline || watching) return next;
      bumpedMention = mention;
      return { ...next, unread: Math.min(100, c.unread + 1), mentions: c.mentions + (mention ? 1 : 0) };
    });
    if (!found) void qc.invalidateQueries({ queryKey: channelsKeys.workspace(org_id) });
    if (bumpedMention || (!found && mention)) qc.setQueryData<ChannelsUnread>(channelsKeys.unread(), (old) => (old ? { ...old, mentions: old.mentions + 1 } : old));
    if (!own && timeline && !watching) void qc.invalidateQueries({ queryKey: channelsKeys.unread() });
    return;
  }
  void qc.invalidateQueries({ queryKey: channelsKeys.workspace(org_id) });
  void qc.invalidateQueries({ queryKey: channelsKeys.unread() });
  if (reason === "later") void qc.invalidateQueries({ queryKey: ["channels", "later", org_id] });
  if (reason === "removed" || reason === "added" || reason === "updated") void qc.invalidateQueries({ queryKey: channelsKeys.channel(channel_id) });
});

registerRealtimeHandler("channels.thread", (qc, e) => {
  const { org_id, root_id, reason, message } = e.data;
  const me = meId(qc);
  const viewing = activeThread === root_id;
  if (reason === "reply" && message && (message.sender_id === me || viewing)) {
    // Your own reply, or one arriving in the thread you're reading: no unread to show.
    return;
  }
  void qc.invalidateQueries({ queryKey: channelsKeys.threads(org_id) });
  void qc.invalidateQueries({ queryKey: channelsKeys.workspace(org_id) });
  if (reason === "follow") void qc.invalidateQueries({ queryKey: channelsKeys.threadAll(root_id) });
});

/**
 * Subscribe to a channel's live events while mounted and mark it as the one on screen
 * (its new messages don't raise its unread count while open).
 */
export function useChannelsTopic(channelId: string | null | undefined): void {
  useTopic(channelId ? topic("channels", channelId) : null);
  useEffect(() => {
    if (!channelId) return undefined;
    activeChannel = channelId;
    return () => {
      if (activeChannel === channelId) activeChannel = null;
    };
  }, [channelId]);
}

/** Mark a thread as the one on screen while mounted. */
export function useChannelsActiveThread(rootId: string | null | undefined): void {
  useEffect(() => {
    if (!rootId) return undefined;
    activeThread = rootId;
    return () => {
      if (activeThread === rootId) activeThread = null;
    };
  }, [rootId]);
}

function useFallbackPoll(): number | false {
  return useConnectionState() === "open" ? false : REALTIME_FALLBACK_POLL_MS;
}

// ───────────────────────────────────────────────────────────── queries ──

/** Badge totals across your orgs (mentions include every DM message). */
export function useChannelsUnread(options?: QueryOverrides<ChannelsUnread>): UseQueryResult<ChannelsUnread, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  const poll = useFallbackPoll();
  return useApiQuery(channelsKeys.unread(), ({ signal }) => c.unread({ signal }), { staleTime: 30_000, refetchInterval: poll === false ? 120_000 : poll, ...options });
}

/** Mentions and DM messages waiting, for a nav badge (undefined while loading or zero). */
export function useChannelsMentionCount(): number | undefined {
  const n = useChannelsUnread().data?.mentions;
  return n ? n : undefined;
}

/** The sidebar for an org (joins default channels on first load). */
export function useChannelsWorkspace(orgId: string | null | undefined): UseQueryResult<ChannelsWorkspace, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  const poll = useFallbackPoll();
  return useApiQuery(channelsKeys.workspace(orgId ?? ""), ({ signal }) => c.workspace(orgId ?? "", { signal }), { staleTime: 30_000, refetchInterval: poll }, !!orgId);
}

export function useChannelsBrowse(orgId: string | null | undefined, q: string, archived = false): PagedQueryResult<ChannelsBrowseChannel> {
  const c = useEndpoints(channelsEndpoints);
  const query = q.trim();
  return usePagedQuery<ChannelsBrowseChannel>(
    channelsKeys.browse(orgId ?? "", query, archived),
    (cursor, signal) => c.browse(orgId ?? "", { q: query, archived, cursor, limit: 50 }, { signal }),
    { placeholderData: (prev) => prev },
    !!orgId,
  );
}

/** A channel's details and your permissions in it. Pass null to disable. */
export function useChannel(id: string | null | undefined): UseQueryResult<ChannelsDetail, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.channel(id ?? ""), ({ signal }) => c.channel(id ?? "", { signal }), { retry: (n, err) => err.status !== 404 && n < 2 }, !!id);
}

/**
 * A channel's timeline as an infinite query in both directions: `fetchNextPage` loads older
 * messages, `fetchPreviousPage` newer ones (after a jump). Flatten with `flattenChannelsPages`.
 */
export function useChannelsMessages(id: string | null | undefined, around?: string | null): UseInfiniteQueryResult<ChannelsTimelineData, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useInfiniteQuery<ChannelsCachePage, ApiError, ChannelsTimelineData, QueryKey, ChannelsPageParam>({
    queryKey: channelsKeys.messages(id ?? "", around),
    queryFn: ({ pageParam, signal }) => c.messages(id ?? "", { ...pageParam, limit: 50 }, { signal }),
    initialPageParam: around ? { around } : undefined,
    getNextPageParam: (last) => {
      const oldest = last.items.find((m) => !m.status);
      return last.has_older && oldest ? { before: oldest.id } : undefined;
    },
    getPreviousPageParam: (first) => {
      const newest = [...first.items].reverse().find((m) => !m.status);
      return first.has_newer && newest ? { after: newest.id } : undefined;
    },
    enabled: !!id,
    retry: (n, err) => err.status !== 404 && n < 2,
  });
}

/** A forum channel's posts, latest activity first (`tag` filters). */
export function useChannelsTopics(id: string | null | undefined, tag = ""): PagedQueryResult<ChannelsMessage> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsMessage>(channelsKeys.topics(id ?? "", tag), (cursor, signal) => c.topics(id ?? "", { tag, cursor, limit: 30 }, { signal }), { placeholderData: (prev) => prev }, !!id);
}

/** A thread (root, replies in both directions, your follow state). */
export function useChannelsThread(rootId: string | null | undefined, around?: string | null): UseInfiniteQueryResult<ChannelsThreadData, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useInfiniteQuery<ChannelsThreadPage, ApiError, ChannelsThreadData, QueryKey, ChannelsPageParam>({
    queryKey: channelsKeys.thread(rootId ?? "", around),
    queryFn: async ({ pageParam, signal }) => {
      const t = await c.thread(rootId ?? "", { ...pageParam, limit: 50 }, { signal });
      return { ...t.replies, root: t.root, channel: t.channel, following: t.following, last_read: t.last_read };
    },
    initialPageParam: around ? { around } : undefined,
    getNextPageParam: (last) => {
      const oldest = last.items.find((m) => !m.status);
      return last.has_older && oldest ? { before: oldest.id } : undefined;
    },
    getPreviousPageParam: (first) => {
      const newest = [...first.items].reverse().find((m) => !m.status);
      return first.has_newer && newest ? { after: newest.id } : undefined;
    },
    enabled: !!rootId,
    retry: (n, err) => err.status !== 404 && n < 2,
  });
}

export function useChannelsMembers(id: string | null | undefined, q = ""): PagedQueryResult<ChannelsMember> {
  const c = useEndpoints(channelsEndpoints);
  const query = q.trim();
  return usePagedQuery<ChannelsMember>(channelsKeys.members(id ?? "", query), (cursor, signal) => c.members(id ?? "", { q: query, cursor, limit: 50 }, { signal }), { placeholderData: (prev) => prev }, !!id);
}

export function useChannelsPins(id: string | null | undefined): UseQueryResult<{ items: ChannelsMessage[] }, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.pins(id ?? ""), ({ signal }) => c.pins(id ?? "", { signal }), undefined, !!id);
}

export function useChannelsFiles(id: string | null | undefined): PagedQueryResult<ChannelsFile> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsFile>(channelsKeys.files(id ?? ""), (cursor, signal) => c.files(id ?? "", { cursor, limit: 30 }, { signal }), undefined, !!id);
}

/** Threads you follow, latest reply first, with unread counts. */
export function useChannelsThreads(orgId: string | null | undefined): PagedQueryResult<ChannelsThreadSummary> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsThreadSummary>(channelsKeys.threads(orgId ?? ""), (cursor, signal) => c.threads(orgId ?? "", { cursor, limit: 20 }, { signal }), undefined, !!orgId);
}

/** Saved items ("Later"), open or done. */
export function useChannelsLater(orgId: string | null | undefined, state: "open" | "done" = "open"): PagedQueryResult<ChannelsLaterItem> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsLaterItem>(channelsKeys.later(orgId ?? "", state), (cursor, signal) => c.later(orgId ?? "", { state, cursor, limit: 30 }, { signal }), undefined, !!orgId);
}

/** Message search (enabled at 2+ characters or with a filter). */
export function useChannelsSearch(orgId: string | null | undefined, q: string, channelId?: string | null): PagedQueryResult<ChannelsSearchHit> {
  const c = useEndpoints(channelsEndpoints);
  const query = q.trim();
  const ready = query.length >= 2;
  return usePagedQuery<ChannelsSearchHit>(
    channelsKeys.search(orgId ?? "", query, channelId),
    (cursor, signal) => c.search(orgId ?? "", { q: query, ...(channelId ? { channel_id: channelId } : {}), cursor, limit: 30 }, { signal }),
    { placeholderData: (prev) => prev },
    !!orgId && ready,
  );
}

/** Org members by name or email (pickers, @ autocomplete); `channelId` marks who's in it. */
export function useChannelsPeople(orgId: string | null | undefined, q: string, channelId?: string | null, enabled = true): UseQueryResult<Page<ChannelsPerson>, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  const query = q.trim();
  return useApiQuery(channelsKeys.people(orgId ?? "", query, channelId), ({ signal }) => c.people(orgId ?? "", query, channelId ?? undefined, { signal }), { placeholderData: (prev) => prev, staleTime: 30_000 }, !!orgId && enabled);
}

/** Groups to @mention (empty when the app wired none). */
export function useChannelsGroups(orgId: string | null | undefined, q: string, enabled = true): UseQueryResult<Page<ChannelsGroup>, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  const query = q.trim();
  return useApiQuery(channelsKeys.groups(orgId ?? "", query), ({ signal }) => c.groups(orgId ?? "", query, { signal }), { placeholderData: (prev) => prev, staleTime: 60_000 }, !!orgId && enabled);
}

/** A message and its channel (message links). */
export function useChannelsMessage(id: string | null | undefined): UseQueryResult<ChannelsMessageContext, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.message(id ?? ""), ({ signal }) => c.message(id ?? "", { signal }), { retry: false }, !!id);
}

/** A fresh signed URL for an attachment whose link expired (enable on a load error). */
export function useChannelsAttachmentUrl(attachmentId: string | null | undefined, enabled: boolean): UseQueryResult<ChannelsAttachmentUrl, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.attachment(attachmentId ?? ""), ({ signal }) => c.attachmentUrl(attachmentId ?? "", { signal }), { staleTime: 50 * 60_000 }, enabled && !!attachmentId);
}

// ───────────────────────────────────────────────────────────── sending ──

/** A client id for an idempotent send. */
export function newChannelsClientId(): string {
  return generateId().replace(/[^A-Za-z0-9_-]/g, "");
}

function optimistic(qc: QueryClient, channelId: string, input: ChannelsSendInput & { client_id: string }): ChannelsCachedMessage | null {
  const me = qc.getQueryData<Me>(queryKeys.me());
  if (!me) return null;
  return {
    id: `local-${input.client_id}`,
    channel_id: channelId,
    kind: "text",
    sender: { id: me.id, display_name: me.display_name, avatar_url: me.avatar_url, deleted: false },
    bot_name: null,
    body: input.body.trim(),
    event: null,
    subject: null,
    thread_root_id: input.thread_root_id ?? null,
    also_in_channel: !!input.also_send_to_channel,
    title: input.title?.trim() || null,
    tags: input.tags ?? [],
    mentions: [],
    reply_count: 0,
    last_reply_at: null,
    reply_users: [],
    attachments: (input.attachments ?? []).map((a) => ({
      id: `local-${a.key}`,
      kind: channelsAttachmentKind(a.content_type ?? ""),
      content_type: a.content_type ?? "",
      file_name: a.file_name ?? "",
      size_bytes: a.size_bytes ?? null,
      width: a.width ?? null,
      height: a.height ?? null,
      url: a.preview_url ?? "",
      url_expires_at: "",
    })),
    reactions: [],
    pinned: false,
    saved: false,
    client_id: input.client_id,
    edited_at: null,
    deleted: false,
    created_at: new Date().toISOString(),
    status: "sending",
    pending: input,
  };
}

export interface ChannelsSender {
  /** Send: it shows at once and is confirmed (or marked failed) when the server answers. */
  send: (input: ChannelsSendInput) => Promise<ChannelsMessage | null>;
  /** Resend a failed message with its original client id (never doubled). */
  retry: (message: ChannelsCachedMessage) => void;
  /** Drop a failed message. */
  discard: (message: ChannelsCachedMessage) => void;
}

function place(qc: QueryClient, channelId: string, msg: ChannelsCachedMessage): void {
  if (!msg.thread_root_id || msg.also_in_channel) qc.setQueryData<ChannelsTimelineData>(channelsKeys.messages(channelId), (old) => (old ? upsert(old, msg) : old));
  if (msg.thread_root_id) qc.setQueriesData<ChannelsThreadData>({ queryKey: channelsKeys.threadAll(msg.thread_root_id) }, (old) => (old ? upsert(old, msg) : old));
}

function markFailed(qc: QueryClient, channelId: string, clientId: string, rootId?: string | null): void {
  const fail = <T extends AnyPagesData>(d: T): T => ({ ...d, pages: d.pages.map((p) => ({ ...p, items: p.items.map((m) => (m.client_id === clientId && m.status ? { ...m, status: "failed" as const } : m)) })) });
  updateTimelines(qc, channelId, fail);
  if (rootId) updateThreads(qc, rootId, fail);
}

/** Optimistic, idempotent sends (timeline, thread replies, forum posts) keyed by `client_id`. */
export function useChannelsSend(channelId: string): ChannelsSender {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  const mutation = useMutation<ChannelsMessage, ApiError, ChannelsSendInput & { client_id: string }>({
    mutationFn: (input) =>
      c.send(channelId, {
        client_id: input.client_id,
        ...(input.body.trim() ? { body: input.body.trim() } : {}),
        ...(input.thread_root_id ? { thread_root_id: input.thread_root_id } : {}),
        ...(input.also_send_to_channel ? { also_send_to_channel: true } : {}),
        ...(input.title?.trim() ? { title: input.title.trim() } : {}),
        ...(input.tags?.length ? { tags: input.tags } : {}),
        ...(input.attachments?.length ? { attachments: input.attachments.map(({ preview_url: _p, content_type: _c, ...a }) => a) } : {}),
      }),
    onMutate: (input) => {
      if (input.title) return; // forum posts show in the list once they exist
      const msg = optimistic(qc, channelId, input);
      if (msg) place(qc, channelId, msg);
    },
    onSuccess: (msg) => {
      place(qc, channelId, msg);
      if (msg.title !== null) void qc.invalidateQueries({ queryKey: channelsKeys.topicsAll(channelId) });
      // Replying follows the thread (the server did the same).
      if (msg.thread_root_id) updateThreads(qc, msg.thread_root_id, (d) => ({ ...d, pages: d.pages.map((p) => ({ ...p, following: true })) }));
    },
    onError: (_e, input) => markFailed(qc, channelId, input.client_id, input.thread_root_id),
  });
  const { mutateAsync, mutate } = mutation;
  const send = useCallback(
    (input: ChannelsSendInput) => mutateAsync({ ...input, client_id: input.client_id ?? newChannelsClientId() }).catch(() => null),
    [mutateAsync],
  );
  const retry = useCallback(
    (m: ChannelsCachedMessage) => {
      if (m.pending && m.client_id) mutate({ ...m.pending, client_id: m.client_id });
    },
    [mutate],
  );
  const discard = useCallback(
    (m: ChannelsCachedMessage) => {
      const drop = <T extends AnyPagesData>(d: T): T => ({ ...d, pages: d.pages.map((p) => ({ ...p, items: p.items.filter((x) => x.id !== m.id) })) });
      updateTimelines(qc, channelId, drop);
      if (m.thread_root_id) updateThreads(qc, m.thread_root_id, drop);
    },
    [qc, channelId],
  );
  return { send, retry, discard };
}

/** Edit your message. Optimistic; rolled back on failure. */
export function useChannelsEdit(): UseMutationResult<ChannelsMessage, ApiError, { message: ChannelsMessage; body: string; title?: string; tags?: string[] }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, { message: ChannelsMessage; body: string; title?: string; tags?: string[] }>({
    mutationFn: ({ message, body, title, tags }) => {
      const req: ChannelsEditRequest = { body };
      if (title !== undefined) req.title = title;
      if (tags !== undefined) req.tags = tags;
      return c.edit(message.id, req);
    },
    onMutate: ({ message, body }) => updateEverywhere(qc, message, (m) => ({ ...m, body: body.trim(), edited_at: new Date().toISOString() })),
    onSuccess: (msg) => updateEverywhere(qc, msg, (old) => ({ ...msg, saved: old.saved })),
    onError: (_e, { message }) => {
      void qc.invalidateQueries({ queryKey: channelsKeys.messagesAll(message.channel_id) });
      if (message.thread_root_id) void qc.invalidateQueries({ queryKey: channelsKeys.threadAll(message.thread_root_id) });
    },
  });
}

/** Delete a message for everyone (yours, or as a channel or org admin). Optimistic. */
export function useChannelsDelete(): UseMutationResult<void, ApiError, ChannelsMessage> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, ChannelsMessage>({
    mutationFn: (m) => c.remove(m.id),
    onMutate: (message) => updateEverywhere(qc, message, (m) => ({ ...m, deleted: true, body: "", attachments: [], reactions: [], edited_at: null, pinned: false })),
    onError: (_e, m) => void qc.invalidateQueries({ queryKey: channelsKeys.messagesAll(m.channel_id) }),
    onSettled: (_d, _e, m) => {
      void qc.invalidateQueries({ queryKey: channelsKeys.topicsAll(m.channel_id) });
      void qc.invalidateQueries({ queryKey: channelsKeys.pins(m.channel_id) });
    },
  });
}

/** Add or remove your reaction. Optimistic. */
export function useChannelsReact(): UseMutationResult<ChannelsMessage, ApiError, { message: ChannelsMessage; emoji: string }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, { message: ChannelsMessage; emoji: string }>({
    mutationFn: ({ message, emoji }) => {
      const me = meId(qc);
      const mine = message.reactions.some((r) => r.emoji === emoji && !!me && r.user_ids.includes(me));
      return mine ? c.unreact(message.id, emoji) : c.react(message.id, emoji);
    },
    onMutate: ({ message, emoji }) => {
      const me = meId(qc);
      if (!me) return;
      updateEverywhere(qc, message, (m) => {
        const existing = m.reactions.find((r) => r.emoji === emoji);
        const mine = !!existing?.user_ids.includes(me);
        const reactions = existing
          ? m.reactions
              .map((r) => (r.emoji !== emoji ? r : mine ? { ...r, count: r.count - 1, user_ids: r.user_ids.filter((u) => u !== me) } : { ...r, count: r.count + 1, user_ids: [...r.user_ids, me] }))
              .filter((r) => r.count > 0)
          : [...m.reactions, { emoji, count: 1, user_ids: [me] }];
        return { ...m, reactions };
      });
    },
    onSuccess: (msg) => updateEverywhere(qc, msg, (old) => ({ ...msg, saved: old.saved })),
    onError: (_e, { message }) => void qc.invalidateQueries({ queryKey: channelsKeys.messagesAll(message.channel_id) }),
  });
}

/** Pin (`true`) or unpin a message. */
export function useChannelsPin(): UseMutationResult<ChannelsMessage, ApiError, { message: ChannelsMessage; pinned: boolean }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, { message: ChannelsMessage; pinned: boolean }>({
    mutationFn: ({ message, pinned }) => (pinned ? c.pin(message.id) : c.unpin(message.id)),
    onMutate: ({ message, pinned }) => updateEverywhere(qc, message, (m) => ({ ...m, pinned })),
    onSettled: (_d, _e, { message }) => {
      void qc.invalidateQueries({ queryKey: channelsKeys.pins(message.channel_id) });
      void qc.invalidateQueries({ queryKey: channelsKeys.channel(message.channel_id) });
    },
  });
}

/** Save for later (`saved: true`), mark done (`done: true`), or remove (`saved: false`). */
export function useChannelsSaveLater(orgId: string | null | undefined): UseMutationResult<void, ApiError, { message: ChannelsMessage; saved: boolean; done?: boolean }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { message: ChannelsMessage; saved: boolean; done?: boolean }>({
    mutationFn: ({ message, saved, done }) => (saved ? c.saveLater(message.id, !!done) : c.unsaveLater(message.id)),
    onMutate: ({ message, saved, done }) => updateEverywhere(qc, message, (m) => ({ ...m, saved: saved && !done })),
    onSettled: () => {
      if (!orgId) return;
      void qc.invalidateQueries({ queryKey: ["channels", "later", orgId] });
      void qc.invalidateQueries({ queryKey: channelsKeys.workspace(orgId) });
    },
  });
}

/**
 * Returns `markRead(message)`: moves your read pointer to that message once shown. Older
 * or repeated calls are skipped; the sidebar row clears at once.
 */
export function useChannelsMarkRead(channelId: string | null | undefined, orgId: string | null | undefined): (m: Pick<ChannelsMessage, "id" | "created_at"> | null | undefined) => void {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  const sent = useRef<Pick<ChannelsMessage, "id" | "created_at"> | null>(null);
  useEffect(() => {
    sent.current = null;
  }, [channelId]);
  return useCallback(
    (m) => {
      if (!channelId || !m || m.id.startsWith("local-")) return;
      if (sent.current && compareChannelsMessages(sent.current, m) >= 0) return;
      sent.current = m;
      if (orgId) {
        let cleared = 0;
        updateSidebarRow(qc, orgId, channelId, (row) => {
          cleared = row.mentions;
          return { ...row, unread: 0, mentions: 0, last_read_at: m.created_at };
        });
        if (cleared) qc.setQueryData<ChannelsUnread>(channelsKeys.unread(), (old) => (old ? { ...old, mentions: Math.max(0, old.mentions - cleared) } : old));
      }
      c.markRead(channelId, m.id).then(
        () => void qc.invalidateQueries({ queryKey: channelsKeys.unread() }),
        () => {
          sent.current = null;
        },
      );
    },
    [c, qc, channelId, orgId],
  );
}

/** Returns `markRead(reply)` for a thread you follow. */
export function useChannelsMarkThreadRead(rootId: string | null | undefined, orgId: string | null | undefined): (m: Pick<ChannelsMessage, "id" | "created_at"> | null | undefined) => void {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  const sent = useRef<Pick<ChannelsMessage, "id" | "created_at"> | null>(null);
  useEffect(() => {
    sent.current = null;
  }, [rootId]);
  return useCallback(
    (m) => {
      if (!rootId || !m || m.id.startsWith("local-")) return;
      if (sent.current && compareChannelsMessages(sent.current, m) >= 0) return;
      sent.current = m;
      c.markThreadRead(rootId, m.id).then(
        () => {
          if (!orgId) return;
          void qc.invalidateQueries({ queryKey: channelsKeys.threads(orgId) });
          void qc.invalidateQueries({ queryKey: channelsKeys.workspace(orgId) });
        },
        () => {
          sent.current = null;
        },
      );
    },
    [c, qc, rootId, orgId],
  );
}

/** Returns `notifyTyping()`: at most every 3 seconds. */
export function useChannelsTypingSignal(channelId: string | null | undefined, rootId?: string | null): () => void {
  const c = useEndpoints(channelsEndpoints);
  const last = useRef(0);
  return useCallback(() => {
    const now = Date.now();
    if (!channelId || now - last.current < CHANNELS_TYPING_HEARTBEAT_MS) return;
    last.current = now;
    c.typing(channelId, rootId).catch(() => undefined);
  }, [c, channelId, rootId]);
}

/** Follow (`true`) or unfollow a thread. */
export function useChannelsFollow(rootId: string): UseMutationResult<{ following: boolean }, ApiError, boolean> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<{ following: boolean }, ApiError, boolean>({
    mutationFn: (follow) => (follow ? c.follow(rootId) : c.unfollow(rootId)),
    onMutate: (following) => updateThreads(qc, rootId, (d) => ({ ...d, pages: d.pages.map((p) => ({ ...p, following })) })),
    onSettled: () => void qc.invalidateQueries({ queryKey: ["channels", "threads"] }),
  });
}

// ───────────────────────────────────────────────────────────── channels ──

function useDetailMutation<V>(orgId: string | null | undefined, fn: (v: V) => Promise<ChannelsDetail>): UseMutationResult<ChannelsDetail, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<ChannelsDetail, ApiError, V>({
    mutationFn: fn,
    onSuccess: (d) => {
      qc.setQueryData(channelsKeys.channel(d.id), d);
      void qc.invalidateQueries({ queryKey: channelsKeys.workspace(orgId ?? d.org_id) });
      void qc.invalidateQueries({ queryKey: ["channels", "browse", orgId ?? d.org_id] });
    },
  });
}

export function useCreateChannel(orgId: string): UseMutationResult<ChannelsDetail, ApiError, ChannelsCreateRequest> {
  const c = useEndpoints(channelsEndpoints);
  return useDetailMutation(orgId, (body: ChannelsCreateRequest) => c.create(orgId, body));
}

/** Open (or reopen) a DM or group DM with these people. Resolves to the conversation. */
export function useOpenChannelsDM(orgId: string): UseMutationResult<ChannelsDetail, ApiError, string[]> {
  const c = useEndpoints(channelsEndpoints);
  return useDetailMutation(orgId, (ids: string[]) => c.openDM(orgId, ids));
}

export function useUpdateChannel(id: string): UseMutationResult<ChannelsDetail, ApiError, ChannelsUpdateRequest> {
  const c = useEndpoints(channelsEndpoints);
  return useDetailMutation(null, (body: ChannelsUpdateRequest) => c.update(id, body));
}

/** Archive (`true`) or unarchive. */
export function useArchiveChannel(id: string): UseMutationResult<ChannelsDetail, ApiError, boolean> {
  const c = useEndpoints(channelsEndpoints);
  return useDetailMutation(null, (archived: boolean) => (archived ? c.archive(id) : c.unarchive(id)));
}

export function useJoinChannel(id: string): UseMutationResult<ChannelsDetail, ApiError, void> {
  const c = useEndpoints(channelsEndpoints);
  return useDetailMutation(null, () => c.join(id));
}

export function useAddChannelMembers(id: string): UseMutationResult<ChannelsDetail, ApiError, string[]> {
  const c = useEndpoints(channelsEndpoints);
  return useDetailMutation(null, (ids: string[]) => c.addMembers(id, ids));
}

/** Notification level, mute and star. Optimistic on the sidebar row. */
export function useChannelPrefs(id: string, orgId: string | null | undefined): UseMutationResult<ChannelsDetail, ApiError, ChannelsPrefsRequest> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsDetail, ApiError, ChannelsPrefsRequest>({
    mutationFn: (body) => c.setPrefs(id, body),
    onMutate: (body) => {
      const patch = { ...(body.notify ? { notify: body.notify } : {}), ...(body.muted !== undefined ? { muted: body.muted } : {}), ...(body.starred !== undefined ? { starred: body.starred } : {}) };
      if (orgId) updateSidebarRow(qc, orgId, id, (row) => ({ ...row, ...patch }));
      qc.setQueryData<ChannelsDetail>(channelsKeys.channel(id), (old) => (old ? { ...old, ...patch } : old));
    },
    onSuccess: (d) => qc.setQueryData(channelsKeys.channel(id), d),
    onSettled: () => {
      if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.workspace(orgId) });
    },
  });
}

/** Leave a channel, or close a DM in your sidebar. */
export function useLeaveChannel(orgId: string | null | undefined): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => c.leave(id),
    onSuccess: (_v, id) => {
      if (orgId) qc.setQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId), (old) => (old ? { ...old, channels: old.channels.filter((x) => x.id !== id) } : old));
      void qc.invalidateQueries({ queryKey: channelsKeys.channel(id) });
      void qc.invalidateQueries({ queryKey: channelsKeys.unread() });
      if (orgId) void qc.invalidateQueries({ queryKey: ["channels", "browse", orgId] });
    },
  });
}

export function useRemoveChannelMember(id: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (userId) => c.removeMember(id, userId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["channels", "members", id] });
      void qc.invalidateQueries({ queryKey: channelsKeys.channel(id) });
    },
  });
}

/** Start or join the channel's call (when the app wired calls); resolves to the path to open. */
export function useStartChannelCall(id: string): UseMutationResult<{ path: string }, ApiError, void> {
  const c = useEndpoints(channelsEndpoints);
  return useMutation<{ path: string }, ApiError, void>({ mutationFn: () => c.startCall(id) });
}

// ───────────────────────────────────────────────────────────── staff ──

/** Staff view of a channel (hidden content readable). Audited per load. */
export function useChannelsAdminChannel(id: string | null | undefined, params: ChannelsWindowParams & { thread?: string } = {}): UseQueryResult<ChannelsAdminChannel, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.admin(id ?? "", params), ({ signal }) => c.admin.channel(id ?? "", params, { signal }), { placeholderData: (prev) => prev, retry: (n, err) => err.status !== 404 && n < 2 }, !!id);
}

/** Hide a message for members (staff). Audited. */
export function useAdminRemoveChannelsMessage(): UseMutationResult<void, ApiError, { messageId: string; reason: string }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { messageId: string; reason: string }>({
    mutationFn: ({ messageId, reason }) => c.admin.removeMessage(messageId, reason),
    onSettled: () => void qc.invalidateQueries({ queryKey: ["channels", "admin"] }),
  });
}

/** Refetch everything channels show after the user blocks or unblocks someone. */
export function refreshChannelsAfterBlock(qc: QueryClient): Promise<void> {
  return qc.invalidateQueries({ queryKey: channelsKeys.all() });
}
