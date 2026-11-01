"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, isApiError } from "../api/errors";
import { useAuthSession } from "../auth/session";
import { useApi, useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import type { Page } from "../types";
import { assistantEndpoints } from "./api";
import { streamAssistant, type StreamFetch } from "./stream";
import type {
  AssistantConsent,
  AssistantConversation,
  AssistantConversationListParams,
  AssistantDailyTotals,
  AssistantMessage,
  AssistantStatus,
  AssistantStreamEvent,
} from "./types";

/** Query keys; `all()` invalidates everything the assistant caches. */
export const assistantKeys = {
  all: () => ["assistant"] as const,
  status: () => ["assistant", "status"] as const,
  conversations: () => ["assistant", "conversations"] as const,
  conversationList: (params: Record<string, unknown> = {}) => ["assistant", "conversations", "list", params] as const,
  conversation: (id: string) => ["assistant", "conversations", "detail", id] as const,
  messages: (id: string) => ["assistant", "messages", id] as const,
  adminUsage: (days: number) => ["assistant", "admin", "usage", days] as const,
};

/** `GET /v1/assistant/status`: provider, consent, today's usage. */
export function useAssistantStatus(options?: QueryOverrides<AssistantStatus>): UseQueryResult<AssistantStatus, ApiError> {
  const ep = useEndpoints(assistantEndpoints);
  return useApiQuery(assistantKeys.status(), ({ signal }) => ep.status({ signal }), options);
}

/** Record consent for the provider the dialog named. */
export function useAcceptAssistantConsent(): UseMutationResult<AssistantConsent, ApiError, string> {
  const ep = useEndpoints(assistantEndpoints);
  const qc = useQueryClient();
  return useMutation<AssistantConsent, ApiError, string>({
    mutationFn: (provider) => ep.acceptConsent(provider),
    onSuccess: (consent) => {
      qc.setQueryData<AssistantStatus>(assistantKeys.status(), (s) => (s ? { ...s, consent } : s));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: assistantKeys.status() }),
  });
}

/** Withdraw consent: the next message asks again. */
export function useWithdrawAssistantConsent(): UseMutationResult<void, ApiError, void> {
  const ep = useEndpoints(assistantEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: () => ep.withdrawConsent(),
    onSettled: () => qc.invalidateQueries({ queryKey: assistantKeys.status() }),
  });
}

/** The user's conversations, most recently active first (`data.items`). */
export function useAssistantConversations(params: Omit<AssistantConversationListParams, "cursor"> = {}, options?: InfiniteOverrides<AssistantConversation>): PagedQueryResult<AssistantConversation> {
  const ep = useEndpoints(assistantEndpoints);
  return usePagedQuery<AssistantConversation>(
    assistantKeys.conversationList(listKeyParams(params)),
    (cursor, signal) => ep.conversations({ ...params, ...(cursor ? { cursor } : {}) }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** One conversation. Pass null to disable. */
export function useAssistantConversation(id: string | null | undefined, options?: QueryOverrides<AssistantConversation>): UseQueryResult<AssistantConversation, ApiError> {
  const ep = useEndpoints(assistantEndpoints);
  return useApiQuery(assistantKeys.conversation(id ?? ""), ({ signal }) => ep.conversation(id ?? "", { signal }), options, !!id);
}

export function useCreateAssistantConversation(): UseMutationResult<AssistantConversation, ApiError, { title?: string } | void> {
  const ep = useEndpoints(assistantEndpoints);
  const qc = useQueryClient();
  return useMutation<AssistantConversation, ApiError, { title?: string } | void>({
    mutationFn: (body) => ep.createConversation(body ?? {}),
    onSuccess: (c) => {
      qc.setQueryData(assistantKeys.conversation(c.id), c);
      qc.setQueryData<MessagePages>(assistantKeys.messages(c.id), { pages: [{ items: [], next_cursor: null }], pageParams: [undefined] });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: assistantKeys.conversations() }),
  });
}

export function useRenameAssistantConversation(): UseMutationResult<AssistantConversation, ApiError, { id: string; title: string }> {
  const ep = useEndpoints(assistantEndpoints);
  const qc = useQueryClient();
  return useMutation<AssistantConversation, ApiError, { id: string; title: string }>({
    mutationFn: ({ id, title }) => ep.renameConversation(id, title),
    onSuccess: (c) => qc.setQueryData(assistantKeys.conversation(c.id), c),
    onSettled: () => qc.invalidateQueries({ queryKey: assistantKeys.conversations() }),
  });
}

export function useDeleteAssistantConversation(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(assistantEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.deleteConversation(id),
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: assistantKeys.conversation(id) });
      qc.removeQueries({ queryKey: assistantKeys.messages(id) });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: assistantKeys.conversations() }),
  });
}

/** A conversation's messages, newest first (`data.items`); render them reversed. */
export function useAssistantMessages(conversationId: string | null | undefined, options?: InfiniteOverrides<AssistantMessage>): PagedQueryResult<AssistantMessage> {
  const ep = useEndpoints(assistantEndpoints);
  return usePagedQuery<AssistantMessage>(
    assistantKeys.messages(conversationId ?? ""),
    (cursor, signal) => ep.messages(conversationId ?? "", { limit: 30, ...(cursor ? { cursor } : {}) }, { signal }),
    { staleTime: 30_000, ...options },
    !!conversationId,
  );
}

/** Daily totals across users (admin). */
export function useAdminAssistantUsage(days = 30, options?: QueryOverrides<Page<AssistantDailyTotals>>): UseQueryResult<Page<AssistantDailyTotals>, ApiError> {
  const ep = useEndpoints(assistantEndpoints);
  return useApiQuery(assistantKeys.adminUsage(days), ({ signal }) => ep.admin.usage(days, { signal }), options);
}

// ---- streaming ---------------------------------------------------------------

type MessagePages = InfiniteData<Page<AssistantMessage>, string | undefined>;

/** Map every cached message of a conversation. */
function mapMessages(qc: QueryClient, conversationId: string, fn: (m: AssistantMessage) => AssistantMessage) {
  qc.setQueryData<MessagePages>(assistantKeys.messages(conversationId), (data) =>
    data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map(fn) })) } : data,
  );
}

/** Insert or replace a message (newest first: new ones go to the front of the first page). */
function upsertMessage(qc: QueryClient, conversationId: string, msg: AssistantMessage) {
  qc.setQueryData<MessagePages>(assistantKeys.messages(conversationId), (data) => {
    if (!data) return data;
    if (data.pages.some((p) => p.items.some((m) => m.id === msg.id))) {
      return { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map((m) => (m.id === msg.id ? msg : m)) })) };
    }
    const [first, ...rest] = data.pages;
    return { ...data, pages: [{ items: [msg, ...(first?.items ?? [])], next_cursor: first?.next_cursor ?? null }, ...rest] };
  });
}

function patchMessage(qc: QueryClient, conversationId: string, id: string, fn: (m: AssistantMessage) => AssistantMessage) {
  mapMessages(qc, conversationId, (m) => (m.id === id ? fn(m) : m));
}

/** Apply one stream event to the react-query cache (messages, conversation). */
export function applyAssistantEvent(qc: QueryClient, conversationId: string, e: AssistantStreamEvent): void {
  switch (e.event) {
    case "message": {
      upsertMessage(qc, conversationId, e.data.message);
      return;
    }
    case "delta": {
      const { message_id, text } = e.data;
      patchMessage(qc, conversationId, message_id, (m) => ({ ...m, parts: [{ type: "text", text: (m.parts[0]?.text ?? "") + text }] }));
      return;
    }
    case "tool_start": {
      const d = e.data;
      patchMessage(qc, conversationId, d.message_id, (m) =>
        m.activity.some((a) => a.call_id === d.call_id)
          ? m
          : { ...m, activity: [...m.activity, { call_id: d.call_id, name: d.name, status: "running", summary: "", input: d.input, started_at: d.started_at, duration_ms: 0 }] },
      );
      return;
    }
    case "tool_end": {
      const d = e.data;
      patchMessage(qc, conversationId, d.message_id, (m) => ({
        ...m,
        activity: m.activity.map((a) => (a.call_id === d.call_id ? { ...a, name: d.name, status: d.status, summary: d.summary, duration_ms: d.duration_ms } : a)),
      }));
      return;
    }
    case "done": {
      if (e.data.conversation) qc.setQueryData(assistantKeys.conversation(conversationId), e.data.conversation);
      return;
    }
    case "error":
      return; // the final `message` event carries the error
  }
}

export interface UseAssistantChatOptions {
  /** Streaming fetch. Web: omit (browser fetch). Expo: `fetch` from `expo/fetch`. */
  fetch?: StreamFetch;
}

export interface AssistantChat {
  /** Send a message (to `conversationId`, or the id given). Resolves false when refused (see `error`). */
  send: (text: string, conversationId?: string) => Promise<boolean>;
  /** Ask the server to stop the reply (any instance); the final events still arrive. */
  stop: () => Promise<void>;
  /** Re-attach to a reply running on the server. */
  resume: () => Promise<boolean>;
  streaming: boolean;
  /** Why the last send or resume failed: `ai_consent_required`, `quota_exceeded`, `assistant_unconfigured`… */
  error: ApiError | null;
  clearError: () => void;
}

function defaultFetch(): StreamFetch {
  const f = (globalThis as { fetch?: StreamFetch }).fetch;
  if (typeof f !== "function") throw new Error("No global fetch; pass `fetch` to useAssistantChat.");
  return (url, init) => f(url, init);
}

/**
 * Live chat state for one conversation: sends and resumes over the event stream, writes
 * every event into the messages cache (so `useAssistantMessages` renders it), resumes a
 * reply that was already running when the screen mounted, and stops reading on unmount
 * (the reply keeps running on the server).
 */
export function useAssistantChat(conversationId: string | null | undefined, options: UseAssistantChatOptions = {}): AssistantChat {
  const api = useApi();
  const session = useAuthSession();
  const ep = useEndpoints(assistantEndpoints);
  const qc = useQueryClient();
  const [streaming, setStreaming] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const streamFor = useRef<string | null>(null);
  const fetchRef = useRef(options.fetch);
  fetchRef.current = options.fetch;
  const getToken = useRef(session.getToken);
  getToken.current = session.getToken;

  const run = useCallback(
    async (text: string | undefined, id: string | null | undefined): Promise<boolean> => {
      if (!id) return false;
      abortRef.current?.abort();
      const ctrl = new AbortController();
      abortRef.current = ctrl;
      streamFor.current = id;
      setStreaming(true);
      setError(null);
      try {
        const token = await getToken.current();
        await streamAssistant({
          fetch: fetchRef.current ?? defaultFetch(),
          baseUrl: api.http.baseUrl,
          platform: api.http.platform,
          token,
          conversationId: id,
          ...(text !== undefined ? { text } : {}),
          signal: ctrl.signal,
          onEvent: (e) => applyAssistantEvent(qc, id, e),
        });
        return true;
      } catch (e) {
        if (isApiError(e) && e.code === "aborted") return false;
        setError(isApiError(e) ? e : new ApiError({ status: 0, code: "network_error", message: "Couldn't reach the assistant. Check your connection and try again.", cause: e }));
        return false;
      } finally {
        if (abortRef.current === ctrl) {
          abortRef.current = null;
          setStreaming(false);
        }
        void qc.invalidateQueries({ queryKey: assistantKeys.status() });
        void qc.invalidateQueries({ queryKey: assistantKeys.conversations() });
      }
    },
    [api, qc],
  );

  const conversation = useAssistantConversation(conversationId);
  const resumedFor = useRef<string | null>(null);
  useEffect(() => {
    if (!conversationId || !conversation.data?.running || streaming || resumedFor.current === conversationId) return;
    resumedFor.current = conversationId;
    void run(undefined, conversationId);
  }, [conversationId, conversation.data?.running, streaming, run]);

  // Switching to another conversation stops reading the old one's stream. Getting an id
  // for the conversation just created (undefined → id) keeps it.
  useEffect(() => {
    if (abortRef.current && conversationId && streamFor.current !== conversationId) abortRef.current.abort();
  }, [conversationId]);
  useEffect(() => () => abortRef.current?.abort(), []);

  return {
    send: (text, id) => run(text, id ?? conversationId),
    stop: async () => {
      if (conversationId) await ep.stop(conversationId);
    },
    resume: () => run(undefined, conversationId),
    streaming,
    error,
    clearError: () => setError(null),
  };
}
