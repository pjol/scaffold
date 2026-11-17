"use client";

/**
 * Hooks for channels 1.2: Activity, All unread, mark unread, drafts (server-side) and sent,
 * scheduled messages, reminders, preferences and sidebar sections, custom emoji, workspace
 * settings, profile cards, link cards, bookmarks, polls, forwards, group DM conversion and
 * incoming webhooks. Same conventions as hooks.ts (keys in `channelsKeys`).
 */
import { useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { channelsEndpoints } from "./api";
import { channelsTimeZone } from "./format";
import { channelsKeys, channelsPutMessage, channelsReplaceMessage, newChannelsClientId } from "./hooks";
import type {
  ChannelsActivityItem,
  ChannelsActivityKind,
  ChannelsBookmark,
  ChannelsCard,
  ChannelsCustomEmoji,
  ChannelsDetail,
  ChannelsDraft,
  ChannelsEmojiList,
  ChannelsForwardRequest,
  ChannelsMessage,
  ChannelsNewWebhook,
  ChannelsPollRequest,
  ChannelsProfileCard,
  ChannelsReminder,
  ChannelsReminderRequest,
  ChannelsScheduleRequest,
  ChannelsScheduled,
  ChannelsSentItem,
  ChannelsSidebarSection,
  ChannelsUnreadChannel,
  ChannelsUserPrefs,
  ChannelsUserPrefsRequest,
  ChannelsWebhook,
  ChannelsWorkspace,
  ChannelsWorkspaceSettings,
} from "./types";

function invalidateWorkspace(qc: QueryClient, orgId: string | null | undefined): void {
  if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.workspace(orgId) });
}

// ───────────────────────────────────────────────────────────── activity and unread ──

export function useChannelsActivity(orgId: string | null | undefined, filter: ChannelsActivityKind | "" = ""): PagedQueryResult<ChannelsActivityItem> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsActivityItem>(
    channelsKeys.activity(orgId ?? "", filter),
    (cursor, signal) => c.activity(orgId ?? "", { filter: filter || undefined, cursor, limit: 30 }, { signal }),
    { staleTime: 15_000 },
    !!orgId,
  );
}

/** Everything in Activity up to now counts as seen. */
export function useMarkChannelsActivityRead(orgId: string | null | undefined): () => void {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useCallback(() => {
    if (!orgId) return;
    qc.setQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId), (old) => (old ? { ...old, activity_unread: 0 } : old));
    void c.markActivityRead(orgId).catch(() => invalidateWorkspace(qc, orgId));
  }, [c, qc, orgId]);
}

export function useChannelsUnreads(orgId: string | null | undefined): UseQueryResult<{ items: ChannelsUnreadChannel[] }, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.unreads(orgId ?? ""), ({ signal }) => c.unreads(orgId ?? "", { signal }), { staleTime: 5_000 }, !!orgId);
}

/** Mark a message (and everything after it) unread. */
export function useChannelsMarkUnread(orgId: string | null | undefined): UseMutationResult<void, ApiError, ChannelsMessage> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, ChannelsMessage>({
    mutationFn: (m) => c.markUnread(m.channel_id, m.id),
    onSuccess: (_v, m) => {
      invalidateWorkspace(qc, orgId);
      void qc.invalidateQueries({ queryKey: channelsKeys.channel(m.channel_id) });
      void qc.invalidateQueries({ queryKey: channelsKeys.unread() });
      if (m.thread_root_id) void qc.invalidateQueries({ queryKey: channelsKeys.threadAll(m.thread_root_id) });
    },
  });
}

// ───────────────────────────────────────────────────────────── drafts and sent ──

export function useChannelsDrafts(orgId: string | null | undefined): PagedQueryResult<ChannelsDraft> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsDraft>(channelsKeys.drafts(orgId ?? ""), (cursor, signal) => c.drafts(orgId ?? "", { cursor, limit: 30 }, { signal }), undefined, !!orgId);
}

export function useChannelsSent(orgId: string | null | undefined): PagedQueryResult<ChannelsSentItem> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsSentItem>(channelsKeys.sent(orgId ?? ""), (cursor, signal) => c.sent(orgId ?? "", { cursor, limit: 30 }, { signal }), undefined, !!orgId);
}

/** The server draft for a conversation or thread (undefined data = none). */
export function useChannelsDraft(channelId: string | null | undefined, rootId?: string | null): UseQueryResult<ChannelsDraft | null, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(
    channelsKeys.draft(channelId ?? "", rootId),
    async ({ signal }) => (await c.draft(channelId ?? "", rootId, { signal })) ?? null,
    { staleTime: 60_000, refetchOnWindowFocus: false },
    !!channelId,
  );
}

export interface ChannelsDraftSaver {
  /** Save after a pause in typing (empty text deletes the draft). */
  save: (body: string) => void;
  /** Drop any pending save (the message was sent; the server cleared the draft). */
  cancel: () => void;
}

/** Saves a conversation's draft to the server, debounced, so it follows you to other devices. */
export function useChannelsDraftSaver(channelId: string, rootId: string | null | undefined, orgId: string | null | undefined, delayMs = 800): ChannelsDraftSaver {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pending = useRef<string | null>(null);
  const flush = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    const body = pending.current;
    pending.current = null;
    if (body === null) return;
    qc.setQueryData(channelsKeys.draft(channelId, rootId), body.trim() ? (old: ChannelsDraft | null | undefined) => (old ? { ...old, body } : old) : null);
    void c.saveDraft(channelId, body, rootId).then(
      () => {
        if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.drafts(orgId) });
      },
      () => undefined,
    );
  }, [c, qc, channelId, rootId, orgId]);
  // A different conversation, or leaving it: save what was typed.
  useEffect(() => flush, [flush]);
  const save = useCallback(
    (body: string) => {
      pending.current = body;
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(flush, delayMs);
    },
    [flush, delayMs],
  );
  const cancel = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    pending.current = null;
    qc.setQueryData(channelsKeys.draft(channelId, rootId), null);
  }, [qc, channelId, rootId]);
  return { save, cancel };
}

export function useDeleteChannelsDraft(orgId: string | null | undefined): UseMutationResult<void, ApiError, ChannelsDraft> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, ChannelsDraft>({
    mutationFn: (d) => c.deleteDraft(d.channel.id, d.thread_root_id),
    onSuccess: (_v, d) => {
      qc.setQueryData(channelsKeys.draft(d.channel.id, d.thread_root_id), null);
      if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.drafts(orgId) });
      invalidateWorkspace(qc, orgId);
    },
  });
}

// ───────────────────────────────────────────────────────────── scheduled ──

export function useChannelsScheduledList(orgId: string | null | undefined): PagedQueryResult<ChannelsScheduled> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsScheduled>(channelsKeys.scheduled(orgId ?? ""), (cursor, signal) => c.scheduledList(orgId ?? "", { cursor, limit: 30 }, { signal }), undefined, !!orgId);
}

function useScheduledMutation<V, R>(orgId: string | null | undefined, fn: (v: V) => Promise<R>): UseMutationResult<R, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<R, ApiError, V>({
    mutationFn: fn,
    onSuccess: (r) => {
      if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.scheduled(orgId) });
      invalidateWorkspace(qc, orgId);
      if (r && typeof r === "object" && "channel_id" in r && "kind" in r) channelsPutMessage(qc, r as unknown as ChannelsMessage);
    },
  });
}

/** Schedule a message in a conversation (`tz` is filled in). */
export function useChannelsSchedule(channelId: string, orgId: string | null | undefined): UseMutationResult<ChannelsScheduled, ApiError, ChannelsScheduleRequest> {
  const c = useEndpoints(channelsEndpoints);
  return useScheduledMutation(orgId, (body: ChannelsScheduleRequest) => c.schedule(channelId, { tz: channelsTimeZone(), ...body }));
}

export function useUpdateChannelsScheduled(orgId: string | null | undefined): UseMutationResult<ChannelsScheduled, ApiError, { id: string; body?: string; send_at?: string; when?: string }> {
  const c = useEndpoints(channelsEndpoints);
  return useScheduledMutation(orgId, ({ id, ...body }: { id: string; body?: string; send_at?: string; when?: string }) => c.updateScheduled(id, { tz: channelsTimeZone(), ...body }));
}

export function useDeleteChannelsScheduled(orgId: string | null | undefined): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  return useScheduledMutation(orgId, (id: string) => c.deleteScheduled(id));
}

export function useSendChannelsScheduledNow(orgId: string | null | undefined): UseMutationResult<ChannelsMessage, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  return useScheduledMutation(orgId, (id: string) => c.sendScheduledNow(id));
}

// ───────────────────────────────────────────────────────────── reminders ──

export function useChannelsReminders(orgId: string | null | undefined, state: "open" | "done" = "open"): PagedQueryResult<ChannelsReminder> {
  const c = useEndpoints(channelsEndpoints);
  return usePagedQuery<ChannelsReminder>(channelsKeys.reminders(orgId ?? "", state), (cursor, signal) => c.reminders(orgId ?? "", { state, cursor, limit: 30 }, { signal }), undefined, !!orgId);
}

function useReminderMutation<V, R>(orgId: string | null | undefined, fn: (v: V) => Promise<R>): UseMutationResult<R, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<R, ApiError, V>({
    mutationFn: fn,
    onSuccess: () => {
      if (!orgId) return;
      void qc.invalidateQueries({ queryKey: ["channels", "reminders", orgId] });
      void qc.invalidateQueries({ queryKey: ["channels", "later", orgId] });
      invalidateWorkspace(qc, orgId);
    },
  });
}

/** Remind me about a message, or about text (`command` takes `/remind` text). */
export function useCreateChannelsReminder(orgId: string | null | undefined): UseMutationResult<ChannelsReminder, ApiError, ChannelsReminderRequest> {
  const c = useEndpoints(channelsEndpoints);
  return useReminderMutation(orgId, (body: ChannelsReminderRequest) => c.createReminder(orgId ?? "", { tz: channelsTimeZone(), ...body }));
}

export function useUpdateChannelsReminder(orgId: string | null | undefined): UseMutationResult<ChannelsReminder, ApiError, { id: string; at?: string; when?: string; done?: boolean }> {
  const c = useEndpoints(channelsEndpoints);
  return useReminderMutation(orgId, ({ id, ...body }: { id: string; at?: string; when?: string; done?: boolean }) => c.updateReminder(id, { tz: channelsTimeZone(), ...body }));
}

export function useDeleteChannelsReminder(orgId: string | null | undefined): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  return useReminderMutation(orgId, (id: string) => c.deleteReminder(id));
}

// ───────────────────────────────────────────────────────────── preferences and sections ──

/** Change your preferences in a workspace; optimistic on the workspace cache. */
export function useUpdateChannelsUserPrefs(orgId: string | null | undefined): UseMutationResult<ChannelsUserPrefs, ApiError, ChannelsUserPrefsRequest> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsUserPrefs, ApiError, ChannelsUserPrefsRequest, { prev?: ChannelsWorkspace }>({
    mutationFn: (body) => c.updatePrefs(orgId ?? "", body),
    onMutate: (body) => {
      if (!orgId) return {};
      const prev = qc.getQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId));
      if (prev) qc.setQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId), { ...prev, prefs: { ...prev.prefs, ...body } });
      return { prev };
    },
    onError: (_e, _b, ctx) => {
      if (orgId && ctx?.prev) qc.setQueryData(channelsKeys.workspace(orgId), ctx.prev);
    },
    onSuccess: (prefs) => {
      if (orgId) qc.setQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId), (old) => (old ? { ...old, prefs } : old));
    },
  });
}

/** Tell the server this device's time zone once per session (profile cards, reminders). */
export function useChannelsReportZone(orgId: string | null | undefined, current: string | undefined): void {
  const update = useUpdateChannelsUserPrefs(orgId);
  const sent = useRef<string | null>(null);
  useEffect(() => {
    if (!orgId || current === undefined) return;
    const tz = channelsTimeZone();
    if (tz === current || sent.current === `${orgId}:${tz}`) return;
    sent.current = `${orgId}:${tz}`;
    update.mutate({ timezone: tz });
  }, [orgId, current, update]);
}

function useSectionMutation<V, R>(orgId: string | null | undefined, fn: (v: V) => Promise<R>, apply?: (ws: ChannelsWorkspace, v: V) => ChannelsWorkspace): UseMutationResult<R, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<R, ApiError, V, { prev?: ChannelsWorkspace }>({
    mutationFn: fn,
    onMutate: (v) => {
      if (!orgId || !apply) return {};
      const prev = qc.getQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId));
      if (prev) qc.setQueryData(channelsKeys.workspace(orgId), apply(prev, v));
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (orgId && ctx?.prev) qc.setQueryData(channelsKeys.workspace(orgId), ctx.prev);
    },
    onSettled: () => invalidateWorkspace(qc, orgId),
  });
}

export function useCreateChannelsSection(orgId: string | null | undefined): UseMutationResult<ChannelsSidebarSection, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  return useSectionMutation(orgId, (name: string) => c.createSection(orgId ?? "", name));
}

export function useUpdateChannelsSection(orgId: string | null | undefined): UseMutationResult<ChannelsSidebarSection, ApiError, { id: string; name?: string; position?: number; collapsed?: boolean }> {
  const c = useEndpoints(channelsEndpoints);
  return useSectionMutation(
    orgId,
    ({ id, ...body }: { id: string; name?: string; position?: number; collapsed?: boolean }) => c.updateSection(id, body),
    (ws, v) => ({ ...ws, sections: ws.sections.map((s) => (s.id === v.id ? { ...s, ...v } : s)) }),
  );
}

export function useDeleteChannelsSection(orgId: string | null | undefined): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  return useSectionMutation(
    orgId,
    (id: string) => c.deleteSection(id),
    (ws, id) => ({ ...ws, sections: ws.sections.filter((s) => s.id !== id), channels: ws.channels.map((ch) => (ch.section_id === id ? { ...ch, section_id: null } : ch)) }),
  );
}

/** Move a conversation into one of your sections (null: back to its usual place). */
export function useMoveChannelToSection(orgId: string | null | undefined): UseMutationResult<ChannelsDetail, ApiError, { channelId: string; sectionId: string | null }> {
  const c = useEndpoints(channelsEndpoints);
  return useSectionMutation(
    orgId,
    ({ channelId, sectionId }: { channelId: string; sectionId: string | null }) => c.setPrefs(channelId, { section_id: sectionId }),
    (ws, v) => ({ ...ws, channels: ws.channels.map((ch) => (ch.id === v.channelId ? { ...ch, section_id: v.sectionId } : ch)) }),
  );
}

// ───────────────────────────────────────────────────────────── custom emoji ──

/** The workspace's custom emoji (image links expire: refetched hourly) and your frequent ones. */
export function useChannelsEmoji(orgId: string | null | undefined): UseQueryResult<ChannelsEmojiList, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.emoji(orgId ?? ""), ({ signal }) => c.emoji(orgId ?? "", { signal }), { staleTime: 30 * 60_000, refetchInterval: 60 * 60_000 }, !!orgId);
}

export function useCreateChannelsEmoji(orgId: string | null | undefined): UseMutationResult<ChannelsCustomEmoji, ApiError, { name: string; key?: string; alias_of?: string }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsCustomEmoji, ApiError, { name: string; key?: string; alias_of?: string }>({
    mutationFn: (body) => c.createEmoji(orgId ?? "", body),
    onSuccess: () => {
      if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.emoji(orgId) });
    },
  });
}

export function useDeleteChannelsEmoji(orgId: string | null | undefined): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => c.deleteEmoji(id),
    onSuccess: () => {
      if (orgId) void qc.invalidateQueries({ queryKey: channelsKeys.emoji(orgId) });
    },
  });
}

// ───────────────────────────────────────────────────────────── settings, profiles, cards ──

export function useChannelsSettings(orgId: string | null | undefined): UseQueryResult<ChannelsWorkspaceSettings, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.settings(orgId ?? ""), ({ signal }) => c.settings(orgId ?? "", { signal }), undefined, !!orgId);
}

export function useUpdateChannelsSettings(orgId: string | null | undefined): UseMutationResult<ChannelsWorkspaceSettings, ApiError, { unfurl_links?: boolean; retention_days?: number }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsWorkspaceSettings, ApiError, { unfurl_links?: boolean; retention_days?: number }>({
    mutationFn: (body) => c.updateSettings(orgId ?? "", body),
    onSuccess: (settings) => {
      if (!orgId) return;
      qc.setQueryData(channelsKeys.settings(orgId), settings);
      qc.setQueryData<ChannelsWorkspace>(channelsKeys.workspace(orgId), (old) => (old ? { ...old, settings } : old));
    },
  });
}

export function useChannelsProfile(orgId: string | null | undefined, userId: string | null | undefined): UseQueryResult<ChannelsProfileCard, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.profile(orgId ?? "", userId ?? ""), ({ signal }) => c.profile(orgId ?? "", userId ?? "", { signal }), { staleTime: 5 * 60_000 }, !!orgId && !!userId);
}

/** Your card for a link to the app (null data: nothing to show, or not yours to see). */
export function useChannelsCard(url: string | null | undefined): UseQueryResult<ChannelsCard | null, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(
    channelsKeys.card(url ?? ""),
    async ({ signal }) => {
      try {
        return await c.card(url ?? "", { signal });
      } catch (e) {
        if ((e as ApiError).status === 404) return null;
        throw e;
      }
    },
    { staleTime: 5 * 60_000, retry: false },
    !!url,
  );
}

// ───────────────────────────────────────────────────────────── bookmarks ──

export function useChannelsBookmarks(channelId: string | null | undefined): UseQueryResult<{ items: ChannelsBookmark[] }, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.bookmarks(channelId ?? ""), ({ signal }) => c.bookmarks(channelId ?? "", { signal }), { staleTime: 60_000 }, !!channelId);
}

function useBookmarkMutation<V, R>(channelId: string, fn: (v: V) => Promise<R>): UseMutationResult<R, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<R, ApiError, V>({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: channelsKeys.bookmarks(channelId) });
      void qc.invalidateQueries({ queryKey: channelsKeys.channel(channelId) });
    },
  });
}

export function useCreateChannelsBookmark(channelId: string): UseMutationResult<ChannelsBookmark, ApiError, { url: string; title?: string }> {
  const c = useEndpoints(channelsEndpoints);
  return useBookmarkMutation(channelId, (body: { url: string; title?: string }) => c.createBookmark(channelId, body));
}

export function useUpdateChannelsBookmark(channelId: string): UseMutationResult<ChannelsBookmark, ApiError, { id: string; url?: string; title?: string; position?: number }> {
  const c = useEndpoints(channelsEndpoints);
  return useBookmarkMutation(channelId, ({ id, ...body }: { id: string; url?: string; title?: string; position?: number }) => c.updateBookmark(id, body));
}

export function useDeleteChannelsBookmark(channelId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  return useBookmarkMutation(channelId, (id: string) => c.deleteBookmark(id));
}

// ───────────────────────────────────────────────────────────── polls, forwards, previews ──

export function useChannelsCreatePoll(channelId: string): UseMutationResult<ChannelsMessage, ApiError, ChannelsPollRequest> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, ChannelsPollRequest>({
    mutationFn: (body) => c.createPoll(channelId, { client_id: newChannelsClientId(), ...body }),
    onSuccess: (msg) => channelsPutMessage(qc, msg),
  });
}

/** Replace your votes on a poll; optimistic. */
export function useChannelsVote(): UseMutationResult<ChannelsMessage, ApiError, { message: ChannelsMessage; optionIds: string[] }> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, { message: ChannelsMessage; optionIds: string[] }, { prev: ChannelsMessage }>({
    mutationFn: ({ message, optionIds }) => c.vote(message.id, optionIds),
    onMutate: ({ message, optionIds }) => {
      const poll = message.poll;
      if (poll) {
        const mine = new Set(poll.my_votes ?? []);
        const next = new Set(optionIds);
        const options = poll.options.map((o) => ({ ...o, count: o.count - (mine.has(o.id) ? 1 : 0) + (next.has(o.id) ? 1 : 0) }));
        const voters = poll.voters - (mine.size > 0 ? 1 : 0) + (next.size > 0 ? 1 : 0);
        channelsReplaceMessage(qc, { ...message, poll: { ...poll, options, voters, my_votes: optionIds } });
      }
      return { prev: message };
    },
    onError: (_e, _v, ctx) => {
      if (ctx) channelsReplaceMessage(qc, ctx.prev);
    },
    onSuccess: (msg) => channelsReplaceMessage(qc, msg),
  });
}

export function useChannelsClosePoll(): UseMutationResult<ChannelsMessage, ApiError, ChannelsMessage> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, ChannelsMessage>({ mutationFn: (m) => c.closePoll(m.id), onSuccess: (msg) => channelsReplaceMessage(qc, msg) });
}

export function useChannelsForward(): UseMutationResult<ChannelsMessage, ApiError, { message: ChannelsMessage } & ChannelsForwardRequest> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, { message: ChannelsMessage } & ChannelsForwardRequest>({
    mutationFn: ({ message, ...body }) => c.forward(message.id, { client_id: newChannelsClientId(), ...body }),
    onSuccess: (msg) => channelsPutMessage(qc, msg),
  });
}

export function useChannelsHidePreviews(): UseMutationResult<ChannelsMessage, ApiError, ChannelsMessage> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsMessage, ApiError, ChannelsMessage, { prev: ChannelsMessage }>({
    mutationFn: (m) => c.hidePreviews(m.id),
    onMutate: (m) => {
      channelsReplaceMessage(qc, { ...m, links: [], links_hidden: true });
      return { prev: m };
    },
    onError: (_e, _m, ctx) => {
      if (ctx) channelsReplaceMessage(qc, ctx.prev);
    },
    onSuccess: (msg) => channelsReplaceMessage(qc, msg),
  });
}

// ───────────────────────────────────────────────────────────── group DM → channel, webhooks ──

export function useConvertChannelsGroupDM(id: string, orgId: string | null | undefined): UseMutationResult<ChannelsDetail, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsDetail, ApiError, string>({
    mutationFn: (name) => c.convert(id, name),
    onSuccess: (d) => {
      qc.setQueryData(channelsKeys.channel(id), d);
      invalidateWorkspace(qc, orgId);
    },
  });
}

export function useChannelsWebhooks(channelId: string | null | undefined, enabled = true): UseQueryResult<{ items: ChannelsWebhook[] }, ApiError> {
  const c = useEndpoints(channelsEndpoints);
  return useApiQuery(channelsKeys.webhooks(channelId ?? ""), ({ signal }) => c.webhooks(channelId ?? "", { signal }), undefined, !!channelId && enabled);
}

export function useCreateChannelsWebhook(channelId: string): UseMutationResult<ChannelsNewWebhook, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<ChannelsNewWebhook, ApiError, string>({
    mutationFn: (name) => c.createWebhook(channelId, name),
    onSuccess: () => void qc.invalidateQueries({ queryKey: channelsKeys.webhooks(channelId) }),
  });
}

export function useDeleteChannelsWebhook(channelId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(channelsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => c.deleteWebhook(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: channelsKeys.webhooks(channelId) }),
  });
}
