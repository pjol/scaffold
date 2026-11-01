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
import { chatEndpoints } from "./api";
import { attachmentKindOf, compareMessages } from "./format";
import type {
  ChatAdminConversation,
  ChatAttachment,
  ChatAttachmentUrl,
  ChatConversation,
  ChatConversationDetail,
  ChatCreateConversationRequest,
  ChatMessage,
  ChatMessagePage,
  ChatReadMark,
  ChatRole,
  ChatSearchHit,
  ChatSendInput,
  ChatThreadMessage,
  ChatUnread,
  ChatUpdateConversationRequest,
  ChatUser,
  ChatWindowParams,
} from "./types";

// ───────────────────────────────────────────────────────────── keys ──

export const chatKeys = {
  all: () => ["chat"] as const,
  inboxAll: () => ["chat", "inbox"] as const,
  inbox: (archived = false) => ["chat", "inbox", archived ? "archived" : "active"] as const,
  unread: () => ["chat", "unread"] as const,
  conversation: (id: string) => ["chat", "conversation", id] as const,
  messagesAll: (id: string) => ["chat", "messages", id] as const,
  /** `around` = a message id the window is centred on (jump to message); null = newest. */
  messages: (id: string, around?: string | null) => ["chat", "messages", id, around ?? "latest"] as const,
  people: (q: string) => ["chat", "people", q] as const,
  search: (q: string, conversationId?: string | null) => ["chat", "search", q, conversationId ?? "all"] as const,
  attachment: (id: string) => ["chat", "attachment", id] as const,
  admin: (id: string, window: ChatWindowParams = {}) => ["chat", "admin", id, window] as const,
};

registerReconnectRefetch(chatKeys.all());

// ───────────────────────────────────────────────────────────── cache shapes ──

export interface ChatThreadPage extends Omit<ChatMessagePage, "items"> {
  items: ChatThreadMessage[];
}
export type ChatThreadPageParam = { before?: string; after?: string; around?: string } | undefined;
export type ChatThreadData = InfiniteData<ChatThreadPage, ChatThreadPageParam>;
type InboxData = InfiniteData<Page<ChatConversation>, string | undefined>;

/** Every loaded message of a thread, oldest first (optimistic sends last). */
export function flattenThread(data: ChatThreadData | undefined): ChatThreadMessage[] {
  if (!data) return [];
  const out: ChatThreadMessage[] = [];
  const seen = new Set<string>();
  for (let i = data.pages.length - 1; i >= 0; i--) {
    const page = data.pages[i];
    if (!page) continue;
    for (const m of page.items) {
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

function isHidden(qc: QueryClient, conversationId: string, userId: string): boolean {
  return qc.getQueryData<ChatConversationDetail>(chatKeys.conversation(conversationId))?.hidden_user_ids.includes(userId) ?? false;
}

function sameMessage(a: ChatThreadMessage, b: ChatMessage): boolean {
  return a.id === b.id || (!!b.client_id && a.client_id === b.client_id && a.sender.id === b.sender.id);
}

/** Replace a message wherever it is (by id or client_id), or insert it into the newest window. */
function upsertMessage(data: ChatThreadData, msg: ChatThreadMessage): ChatThreadData {
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
  if (!first || first.has_newer) return data; // this window doesn't show the newest messages
  const items = first.items.slice();
  let at = items.length;
  while (at > 0) {
    const prev = items[at - 1];
    if (!prev || (!prev.status && compareMessages(prev, msg) <= 0) || msg.status) break;
    at--;
  }
  items.splice(at, 0, msg);
  return { ...data, pages: [{ ...first, items }, ...pages.slice(1)] };
}

/** Apply `fn` to one message (by id) and keep quoted previews of it in step. */
function mapMessage(data: ChatThreadData, id: string, fn: (m: ChatThreadMessage) => ChatThreadMessage): ChatThreadData {
  return {
    ...data,
    pages: data.pages.map((p) => ({
      ...p,
      items: p.items.map((m) => {
        let next = m.id === id ? fn(m) : m;
        if (next.reply_to?.id === id) {
          const target = data.pages.flatMap((x) => x.items).find((x) => x.id === id);
          const updated = target ? fn(target) : null;
          if (updated) {
            next = {
              ...next,
              reply_to: {
                ...next.reply_to,
                deleted: updated.deleted,
                body: updated.deleted ? "" : updated.body.slice(0, 200),
                has_attachments: !updated.deleted && updated.attachments.length > 0,
              },
            };
          }
        }
        return next;
      }),
    })),
  };
}

function updateThreads(qc: QueryClient, conversationId: string, fn: (data: ChatThreadData) => ChatThreadData): void {
  qc.setQueriesData<ChatThreadData>({ queryKey: chatKeys.messagesAll(conversationId) }, (old) => (old ? fn(old) : old));
}

function updateInboxRow(qc: QueryClient, conversationId: string, fn: (c: ChatConversation) => ChatConversation): void {
  qc.setQueriesData<InboxData>({ queryKey: chatKeys.inboxAll() }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.map((c) => (c.id === conversationId ? fn(c) : c)) })) } : old,
  );
  qc.setQueryData<ChatConversationDetail>(chatKeys.conversation(conversationId), (old) => (old ? { ...old, ...fn(old) } : old));
}

function removeInboxRow(qc: QueryClient, conversationId: string): void {
  qc.setQueriesData<InboxData>({ queryKey: chatKeys.inboxAll() }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, items: p.items.filter((c) => c.id !== conversationId) })) } : old,
  );
}

/** Move a conversation to the top of the inbox with `msg` as its latest message. False when it isn't loaded. */
function bumpInbox(qc: QueryClient, msg: ChatMessage, countsUnread: boolean): boolean {
  let found = false;
  qc.setQueryData<InboxData>(chatKeys.inbox(false), (old) => {
    if (!old) return old;
    let row: ChatConversation | undefined;
    const pages = old.pages.map((p) => ({
      ...p,
      items: p.items.filter((c) => {
        if (c.id !== msg.conversation_id) return true;
        row = c;
        return false;
      }),
    }));
    const first = pages[0];
    if (!row || !first) return old;
    found = true;
    const updated: ChatConversation = {
      ...row,
      last_message: msg,
      last_message_at: msg.created_at > row.last_message_at ? msg.created_at : row.last_message_at,
      unread: row.unread + (countsUnread ? 1 : 0),
    };
    pages[0] = { ...first, items: [updated, ...first.items] };
    return { ...old, pages };
  });
  return found;
}

function earlier(a: ChatReadMark | null, b: ChatReadMark | null): boolean {
  if (!a) return b !== null;
  if (!b) return false;
  return a.at < b.at || (a.at === b.at && a.message_id < b.message_id);
}

// ───────────────────────────────────────────────────────────── typing store ──

/** A typing indicator lasts this long without a new signal. */
export const CHAT_TYPING_TTL_MS = 6_000;
/** Senders repeat the signal at most this often while typing. */
export const CHAT_TYPING_HEARTBEAT_MS = 3_000;

const typing = new Map<string, Map<string, { user: ChatUser; timer: ReturnType<typeof setTimeout> }>>();
const typingSnapshots = new Map<string, ChatUser[]>();
const typingListeners = new Set<() => void>();
const NO_ONE: ChatUser[] = [];

function setTyping(conversationId: string, user: ChatUser, on: boolean): void {
  let users = typing.get(conversationId);
  const existing = users?.get(user.id);
  if (existing) clearTimeout(existing.timer);
  if (!on && !existing) return;
  if (on) {
    if (!users) {
      users = new Map();
      typing.set(conversationId, users);
    }
    users.set(user.id, { user, timer: setTimeout(() => setTyping(conversationId, user, false), CHAT_TYPING_TTL_MS) });
  } else {
    users?.delete(user.id);
  }
  typingSnapshots.set(conversationId, users ? [...users.values()].map((e) => e.user) : NO_ONE);
  for (const l of typingListeners) l();
}

function subscribeTyping(listener: () => void): () => void {
  typingListeners.add(listener);
  return () => typingListeners.delete(listener);
}

/** People typing in a conversation right now (never the caller). */
export function useChatTypingUsers(conversationId: string | null | undefined): ChatUser[] {
  return useSyncExternalStore(
    subscribeTyping,
    () => (conversationId ? (typingSnapshots.get(conversationId) ?? NO_ONE) : NO_ONE),
    () => NO_ONE,
  );
}

// ───────────────────────────────────────────────────────────── realtime ──

let activeConversation: string | null = null;

registerRealtimeHandler("chat.message", (qc, e) => {
  const msg = e.data;
  if (isHidden(qc, msg.conversation_id, msg.sender.id)) return;
  setTyping(msg.conversation_id, msg.sender, false);
  updateThreads(qc, msg.conversation_id, (d) => upsertMessage(d, msg));
});

registerRealtimeHandler("chat.message_updated", (qc, e) => {
  const msg = e.data;
  if (isHidden(qc, msg.conversation_id, msg.sender.id)) return;
  updateThreads(qc, msg.conversation_id, (d) => mapMessage(d, msg.id, () => msg));
});

registerRealtimeHandler("chat.typing", (qc, e) => {
  const { conversation_id, user } = e.data;
  if (user.id === meId(qc) || isHidden(qc, conversation_id, user.id)) return;
  setTyping(conversation_id, user, true);
});

registerRealtimeHandler("chat.read", (qc, e) => {
  const { conversation_id, user_id, last_read } = e.data;
  const me = meId(qc);
  qc.setQueryData<ChatConversationDetail>(chatKeys.conversation(conversation_id), (old) => {
    if (!old) return old;
    const members = old.members.map((m) => (m.user.id === user_id && earlier(m.last_read, last_read) ? { ...m, last_read } : m));
    let others: ChatReadMark | null | undefined;
    for (const m of members) {
      if (m.user.id === me) continue;
      if (others === undefined || earlier(m.last_read, others)) others = m.last_read;
    }
    const next = { ...old, members, others_read: others ?? null };
    if (user_id === me && earlier(old.last_read, last_read)) next.last_read = last_read;
    return next;
  });
  const detail = qc.getQueryData<ChatConversationDetail>(chatKeys.conversation(conversation_id));
  if (detail) updateInboxRow(qc, conversation_id, (c) => ({ ...c, others_read: detail.others_read }));
});

registerRealtimeHandler("chat.member", (qc, e) => {
  void qc.invalidateQueries({ queryKey: chatKeys.conversation(e.data.conversation_id) });
  void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
});

registerRealtimeHandler("chat.inbox", (qc, e) => {
  const { conversation_id: id, reason, message } = e.data;
  switch (reason) {
    case "message": {
      if (!message) break;
      const fromOther = message.sender.id !== meId(qc) && message.kind === "text";
      if (!bumpInbox(qc, message, fromOther && activeConversation !== id)) void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
      void qc.invalidateQueries({ queryKey: chatKeys.inbox(true) });
      if (fromOther) void qc.invalidateQueries({ queryKey: chatKeys.unread() });
      return;
    }
    case "message_updated":
      if (message) updateInboxRow(qc, id, (c) => (c.last_message?.id === message.id ? { ...c, last_message: message } : c));
      return;
    case "removed":
      removeInboxRow(qc, id);
      void qc.invalidateQueries({ queryKey: chatKeys.conversation(id) });
      void qc.invalidateQueries({ queryKey: chatKeys.messagesAll(id) });
      break;
    default:
      void qc.invalidateQueries({ queryKey: chatKeys.conversation(id) });
  }
  void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
  void qc.invalidateQueries({ queryKey: chatKeys.unread() });
});

/**
 * Subscribe to a conversation's topic while mounted, and mark it as the one on screen
 * (its new messages don't raise the inbox count while the thread is open).
 */
export function useChatConversationTopic(conversationId: string | null | undefined): void {
  useTopic(conversationId ? topic("chat", conversationId) : null);
  useEffect(() => {
    if (!conversationId) return undefined;
    activeConversation = conversationId;
    return () => {
      if (activeConversation === conversationId) activeConversation = null;
    };
  }, [conversationId]);
}

function useFallbackPoll(): number | false {
  return useConnectionState() === "open" ? false : REALTIME_FALLBACK_POLL_MS;
}

// ───────────────────────────────────────────────────────────── queries ──

/** Inbox (or the archive), newest activity first, as an infinite list. */
export function useChatInbox(archived = false): PagedQueryResult<ChatConversation> {
  const chat = useEndpoints(chatEndpoints);
  const poll = useFallbackPoll();
  return usePagedQuery<ChatConversation>(chatKeys.inbox(archived), (cursor, signal) => chat.inbox({ archived, cursor, limit: 30 }, { signal }), {
    refetchInterval: poll,
  });
}

/** Unread totals for badges (muted and archived conversations excluded). */
export function useChatUnread(options?: QueryOverrides<ChatUnread>): UseQueryResult<ChatUnread, ApiError> {
  const chat = useEndpoints(chatEndpoints);
  const poll = useFallbackPoll();
  return useApiQuery(chatKeys.unread(), ({ signal }) => chat.unread({ signal }), { staleTime: 30_000, refetchInterval: poll === false ? 120_000 : poll, ...options });
}

/** Unread message count for a nav badge (undefined while loading). */
export function useChatUnreadCount(): number | undefined {
  return useChatUnread().data?.messages;
}

/** A conversation with its members. Pass null to disable. */
export function useChatConversation(id: string | null | undefined): UseQueryResult<ChatConversationDetail, ApiError> {
  const chat = useEndpoints(chatEndpoints);
  return useApiQuery(chatKeys.conversation(id ?? ""), ({ signal }) => chat.conversation(id ?? "", { signal }), { retry: (n, err) => err.status !== 404 && n < 2 }, !!id);
}

/**
 * A thread's messages as an infinite query in both directions: `fetchNextPage` loads
 * older messages, `fetchPreviousPage` newer ones (after a jump). `around` centres the
 * first window on a message id. Flatten with `flattenThread(data)`.
 */
export function useChatMessages(id: string | null | undefined, around?: string | null): UseInfiniteQueryResult<ChatThreadData, ApiError> {
  const chat = useEndpoints(chatEndpoints);
  return useInfiniteQuery<ChatThreadPage, ApiError, ChatThreadData, QueryKey, ChatThreadPageParam>({
    queryKey: chatKeys.messages(id ?? "", around),
    queryFn: ({ pageParam, signal }) => chat.messages(id ?? "", { ...pageParam, limit: 50 }, { signal }),
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

/** People to start a conversation with (`q` ≥ 2 characters, or "" for recent contacts). */
export function useChatPeople(q: string): UseQueryResult<Page<ChatUser>, ApiError> {
  const chat = useEndpoints(chatEndpoints);
  const query = q.trim();
  return useApiQuery(chatKeys.people(query), ({ signal }) => chat.people(query, { signal }), { placeholderData: (prev) => prev, staleTime: 30_000 }, query === "" || query.length >= 2);
}

/** Message search in the caller's conversations (optionally one). Enabled at 2+ characters. */
export function useChatSearch(q: string, conversationId?: string | null): PagedQueryResult<ChatSearchHit> {
  const chat = useEndpoints(chatEndpoints);
  const query = q.trim();
  return usePagedQuery<ChatSearchHit>(
    chatKeys.search(query, conversationId),
    (cursor, signal) => chat.search({ q: query, ...(conversationId ? { conversation_id: conversationId } : {}), cursor }, { signal }),
    { placeholderData: (prev) => prev },
    query.length >= 2,
  );
}

/** A fresh signed URL for an attachment whose link expired (enable on a load error). */
export function useChatAttachmentUrl(attachment: Pick<ChatAttachment, "id"> | null, enabled: boolean): UseQueryResult<ChatAttachmentUrl, ApiError> {
  const chat = useEndpoints(chatEndpoints);
  return useApiQuery(chatKeys.attachment(attachment?.id ?? ""), ({ signal }) => chat.attachmentUrl(attachment?.id ?? "", { signal }), { staleTime: 50 * 60_000 }, enabled && !!attachment);
}

// ───────────────────────────────────────────────────────────── sending ──

/** A client id for an idempotent send. */
export function newChatClientId(): string {
  return generateId().replace(/[^A-Za-z0-9_-]/g, "");
}

function optimisticMessage(qc: QueryClient, conversationId: string, input: ChatSendInput & { client_id: string }): ChatThreadMessage | null {
  const me = qc.getQueryData<Me>(queryKeys.me());
  if (!me) return null;
  return {
    id: `local-${input.client_id}`,
    conversation_id: conversationId,
    sender: { id: me.id, display_name: me.display_name, avatar_url: me.avatar_url, deleted: false },
    kind: "text",
    body: input.body.trim(),
    event: null,
    subject: null,
    client_id: input.client_id,
    reply_to: input.reply_to ?? null,
    attachments: (input.attachments ?? []).map((a) => ({
      id: `local-${a.key}`,
      kind: attachmentKindOf(a.content_type ?? "", a.key),
      content_type: a.content_type ?? "",
      file_name: a.file_name ?? "",
      size_bytes: a.size_bytes ?? null,
      width: a.width ?? null,
      height: a.height ?? null,
      duration_ms: a.duration_ms ?? null,
      url: a.preview_url ?? "",
      url_expires_at: "",
    })),
    reactions: [],
    edited_at: null,
    deleted: false,
    created_at: new Date().toISOString(),
    status: "sending",
    pending: input,
  };
}

export interface ChatSender {
  /** Send a message: it shows at once and is confirmed (or marked failed) when the server answers. */
  send: (input: ChatSendInput) => void;
  /** Resend a failed message with its original client id (the server never doubles it). */
  retry: (message: ChatThreadMessage) => void;
  /** Drop a failed message from the thread. */
  discard: (message: ChatThreadMessage) => void;
}

/** Optimistic, idempotent sends keyed by `client_id`. */
export function useSendMessage(conversationId: string): ChatSender {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  const mutation = useMutation<ChatMessage, ApiError, ChatSendInput & { client_id: string }>({
    mutationFn: (input) =>
      chat.send(conversationId, {
        client_id: input.client_id,
        ...(input.body.trim() ? { body: input.body.trim() } : {}),
        ...(input.reply_to ? { reply_to_id: input.reply_to.id } : {}),
        ...(input.attachments?.length ? { attachments: input.attachments.map(({ preview_url: _p, content_type: _c, ...a }) => a) } : {}),
      }),
    onMutate: (input) => {
      const msg = optimisticMessage(qc, conversationId, input);
      if (!msg) return;
      qc.setQueryData<ChatThreadData>(chatKeys.messages(conversationId), (old) => (old ? upsertMessage(old, msg) : old));
    },
    onSuccess: (msg) => {
      updateThreads(qc, conversationId, (d) => upsertMessage(d, msg));
      bumpInbox(qc, msg, false);
    },
    onError: (_err, input) => {
      updateThreads(qc, conversationId, (d) => ({
        ...d,
        pages: d.pages.map((p) => ({ ...p, items: p.items.map((m) => (m.client_id === input.client_id && m.status ? { ...m, status: "failed" as const } : m)) })),
      }));
    },
  });
  const { mutate } = mutation;
  const send = useCallback((input: ChatSendInput) => mutate({ ...input, client_id: input.client_id ?? newChatClientId() }), [mutate]);
  const retry = useCallback(
    (message: ChatThreadMessage) => {
      if (message.pending && message.client_id) mutate({ ...message.pending, client_id: message.client_id });
    },
    [mutate],
  );
  const discard = useCallback(
    (message: ChatThreadMessage) =>
      updateThreads(qc, conversationId, (d) => ({ ...d, pages: d.pages.map((p) => ({ ...p, items: p.items.filter((m) => m.id !== message.id) })) })),
    [qc, conversationId],
  );
  return { send, retry, discard };
}

/** Edit your own message (within the edit window). Optimistic; rolled back on failure. */
export function useEditMessage(conversationId: string): UseMutationResult<ChatMessage, ApiError, { id: string; body: string }> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<ChatMessage, ApiError, { id: string; body: string }>({
    mutationFn: ({ id, body }) => chat.edit(id, body),
    onMutate: ({ id, body }) => updateThreads(qc, conversationId, (d) => mapMessage(d, id, (m) => ({ ...m, body: body.trim(), edited_at: new Date().toISOString() }))),
    onSuccess: (msg) => updateThreads(qc, conversationId, (d) => mapMessage(d, msg.id, () => msg)),
    onError: () => void qc.invalidateQueries({ queryKey: chatKeys.messagesAll(conversationId) }),
  });
}

/** Delete your own message for everyone (a "Message deleted" tombstone). Optimistic. */
export function useDeleteMessage(conversationId: string): UseMutationResult<void, ApiError, string> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => chat.remove(id),
    onMutate: (id) => updateThreads(qc, conversationId, (d) => mapMessage(d, id, (m) => ({ ...m, deleted: true, body: "", attachments: [], reactions: [], edited_at: null }))),
    onError: () => void qc.invalidateQueries({ queryKey: chatKeys.messagesAll(conversationId) }),
    onSettled: () => void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() }),
  });
}

/** Add or remove your reaction (`emoji`) on a message. Optimistic. */
export function useToggleReaction(conversationId: string): UseMutationResult<ChatMessage, ApiError, { message: ChatMessage; emoji: string }> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<ChatMessage, ApiError, { message: ChatMessage; emoji: string }>({
    mutationFn: ({ message, emoji }) => {
      const me = meId(qc);
      const mine = message.reactions.some((r) => r.emoji === emoji && !!me && r.user_ids.includes(me));
      return mine ? chat.unreact(message.id, emoji) : chat.react(message.id, emoji);
    },
    onMutate: ({ message, emoji }) => {
      const me = meId(qc);
      if (!me) return;
      updateThreads(qc, conversationId, (d) =>
        mapMessage(d, message.id, (m) => {
          const existing = m.reactions.find((r) => r.emoji === emoji);
          const mine = !!existing?.user_ids.includes(me);
          const reactions = existing
            ? m.reactions
                .map((r) => (r.emoji !== emoji ? r : mine ? { ...r, count: r.count - 1, user_ids: r.user_ids.filter((u) => u !== me) } : { ...r, count: r.count + 1, user_ids: [...r.user_ids, me] }))
                .filter((r) => r.count > 0)
            : [...m.reactions, { emoji, count: 1, user_ids: [me] }];
          return { ...m, reactions };
        }),
      );
    },
    onSuccess: (msg) => updateThreads(qc, conversationId, (d) => mapMessage(d, msg.id, () => msg)),
    onError: () => void qc.invalidateQueries({ queryKey: chatKeys.messagesAll(conversationId) }),
  });
}

/**
 * Returns `markRead(message)`: moves the read pointer to that message once it has been
 * shown. Repeated or older calls are skipped; the inbox row clears at once.
 */
export function useMarkRead(conversationId: string | null | undefined): (message: Pick<ChatMessage, "id" | "created_at"> | null | undefined) => void {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  const sent = useRef<ChatReadMark | null>(null);
  useEffect(() => {
    sent.current = null;
  }, [conversationId]);
  return useCallback(
    (message) => {
      if (!conversationId || !message || message.id.startsWith("local-")) return;
      const mark: ChatReadMark = { message_id: message.id, at: message.created_at };
      const detail = qc.getQueryData<ChatConversationDetail>(chatKeys.conversation(conversationId));
      const current = sent.current ?? detail?.last_read ?? null;
      if (current && !earlier(current, mark)) return;
      sent.current = mark;
      updateInboxRow(qc, conversationId, (c) => ({ ...c, last_read: mark, unread: c.last_message?.id === message.id ? 0 : c.unread }));
      chat.markRead(conversationId, message.id).then(
        () => void qc.invalidateQueries({ queryKey: chatKeys.unread() }),
        () => {
          sent.current = null;
        },
      );
    },
    [chat, qc, conversationId],
  );
}

/** Returns `notifyTyping()`: tells the conversation you're typing, at most every 3 seconds. */
export function useTypingSignal(conversationId: string | null | undefined): () => void {
  const chat = useEndpoints(chatEndpoints);
  const last = useRef(0);
  return useCallback(() => {
    const now = Date.now();
    if (!conversationId || now - last.current < CHAT_TYPING_HEARTBEAT_MS) return;
    last.current = now;
    chat.typing(conversationId).catch(() => undefined);
  }, [chat, conversationId]);
}

// ───────────────────────────────────────────────────────────── conversations ──

function useDetailMutation<V>(fn: (v: V) => Promise<ChatConversationDetail>): UseMutationResult<ChatConversationDetail, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<ChatConversationDetail, ApiError, V>({
    mutationFn: fn,
    onSuccess: (detail) => {
      qc.setQueryData(chatKeys.conversation(detail.id), detail);
      void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
    },
  });
}

/** Start (or reopen) a direct conversation, or create a group. Resolves to the conversation. */
export function useCreateConversation(): UseMutationResult<ChatConversationDetail, ApiError, ChatCreateConversationRequest> {
  const chat = useEndpoints(chatEndpoints);
  return useDetailMutation((body: ChatCreateConversationRequest) => chat.create(body));
}

/** Rename a group or change its picture (owner or admin). */
export function useUpdateConversation(id: string): UseMutationResult<ChatConversationDetail, ApiError, ChatUpdateConversationRequest> {
  const chat = useEndpoints(chatEndpoints);
  return useDetailMutation((body: ChatUpdateConversationRequest) => chat.update(id, body));
}

export function useAddMembers(id: string): UseMutationResult<ChatConversationDetail, ApiError, string[]> {
  const chat = useEndpoints(chatEndpoints);
  return useDetailMutation((userIds: string[]) => chat.addMembers(id, userIds));
}

/** Change a member's role; `owner` hands the group over (owner only). */
export function useSetMemberRole(id: string): UseMutationResult<ChatConversationDetail, ApiError, { userId: string; role: ChatRole }> {
  const chat = useEndpoints(chatEndpoints);
  return useDetailMutation(({ userId, role }: { userId: string; role: ChatRole }) => chat.setRole(id, userId, role));
}

/** Remove someone from a group (owner; admins remove members). */
export function useRemoveChatMember(id: string): UseMutationResult<void, ApiError, string> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (userId) => chat.removeMember(id, userId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: chatKeys.conversation(id) }),
  });
}

/** Leave a group, or delete a direct conversation for yourself. */
export function useLeaveConversation(): UseMutationResult<void, ApiError, string> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => chat.leave(id),
    onSuccess: (_v, id) => {
      removeInboxRow(qc, id);
      qc.removeQueries({ queryKey: chatKeys.messagesAll(id) });
      qc.removeQueries({ queryKey: chatKeys.conversation(id) });
      void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
      void qc.invalidateQueries({ queryKey: chatKeys.unread() });
    },
  });
}

/** Mute until a time, forever (`{ forever: true }`), or unmute (`null`). Optimistic. */
export function useMuteConversation(id: string): UseMutationResult<void, ApiError, { until: string } | { forever: true } | null> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { until: string } | { forever: true } | null>({
    mutationFn: (v) => (v ? chat.mute(id, v) : chat.unmute(id)),
    onMutate: (v) => updateInboxRow(qc, id, (c) => ({ ...c, muted: !!v, muted_until: v && "until" in v ? v.until : null })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: chatKeys.conversation(id) });
      void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
      void qc.invalidateQueries({ queryKey: chatKeys.unread() });
    },
  });
}

/** Archive (`true`: hidden until the next message) or unarchive (`false`). */
export function useArchiveConversation(id: string): UseMutationResult<void, ApiError, boolean> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, boolean>({
    mutationFn: (archived) => (archived ? chat.archive(id) : chat.unarchive(id)),
    onMutate: (archived) => {
      if (archived) removeInboxRow(qc, id);
      updateInboxRow(qc, id, (c) => ({ ...c, archived }));
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: chatKeys.inboxAll() });
      void qc.invalidateQueries({ queryKey: chatKeys.unread() });
    },
  });
}

/** Refetch everything chat shows after the user blocks or unblocks someone. */
export function refreshChatAfterBlock(qc: QueryClient): Promise<void> {
  return qc.invalidateQueries({ queryKey: chatKeys.all() });
}

// ───────────────────────────────────────────────────────────── staff ──

/** Staff view of a conversation (every member and message, hidden content readable). Audited per load. */
export function useChatAdminConversation(id: string | null | undefined, window: ChatWindowParams = {}): UseQueryResult<ChatAdminConversation, ApiError> {
  const chat = useEndpoints(chatEndpoints);
  return useApiQuery(chatKeys.admin(id ?? "", window), ({ signal }) => chat.admin.conversation(id ?? "", window, { signal }), { placeholderData: (prev) => prev, retry: (n, err) => err.status !== 404 && n < 2 }, !!id);
}

/** Hide a message for members (staff). Audited. */
export function useAdminRemoveChatMessage(): UseMutationResult<void, ApiError, { messageId: string; reason: string }> {
  const chat = useEndpoints(chatEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { messageId: string; reason: string }>({
    mutationFn: ({ messageId, reason }) => chat.admin.removeMessage(messageId, reason),
    onSettled: () => void qc.invalidateQueries({ queryKey: ["chat", "admin"] }),
  });
}
