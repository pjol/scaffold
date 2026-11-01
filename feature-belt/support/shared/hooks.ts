"use client";

import { useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, useTopic, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import { supportEndpoints } from "./api";
import type {
  AdminReplyRequest,
  AdminSupportTicket,
  AdminSupportTicketDetail,
  AdminTicketListParams,
  AdminUpdateTicketRequest,
  CreateTicketRequest,
  ReplyTicketRequest,
  SupportMessage,
  SupportStaffRef,
  SupportStats,
  SupportStatus,
  SupportTicket,
  SupportTicketDetail,
} from "./types";

declare module "../types" {
  interface RealtimeEventMap {
    "support.message": SupportMessage;
    "support.ticket": SupportTicket;
  }
}

/** Query keys. `supportKeys.all()` invalidates everything this feature caches. */
export const supportKeys = {
  all: () => ["support"] as const,
  mineAll: () => ["support", "mine"] as const,
  mine: (status: SupportStatus | undefined) => ["support", "mine", { status: status ?? null }] as const,
  ticket: (id: string) => ["support", "ticket", id] as const,
  admin: {
    all: () => ["support", "admin"] as const,
    lists: () => ["support", "admin", "list"] as const,
    list: (params: Record<string, unknown> = {}) => ["support", "admin", "list", params] as const,
    ticket: (id: string) => ["support", "admin", "ticket", id] as const,
    stats: () => ["support", "admin", "stats"] as const,
    assignees: () => ["support", "admin", "assignees"] as const,
  },
};

/** Realtime topic of one ticket (its owner and staff). */
export const supportTicketTopic = (id: string): string => topic("support", id);

function appendMessage<T extends { messages: SupportMessage[] }>(data: T | undefined, m: SupportMessage): T | undefined {
  if (!data || data.messages.some((x) => x.id === m.id)) return data;
  return { ...data, messages: [...data.messages, m] };
}

function refreshStaffViews(qc: QueryClient, ticketId: string) {
  void qc.invalidateQueries({ queryKey: supportKeys.admin.lists() });
  void qc.invalidateQueries({ queryKey: supportKeys.admin.stats() });
  void qc.invalidateQueries({ queryKey: supportKeys.admin.ticket(ticketId) });
}

registerRealtimeHandler("support.message", (qc, e) => {
  qc.setQueryData<SupportTicketDetail>(supportKeys.ticket(e.data.ticket_id), (d) => appendMessage(d, e.data));
  qc.setQueryData<AdminSupportTicketDetail>(supportKeys.admin.ticket(e.data.ticket_id), (d) => appendMessage(d, e.data));
});
registerRealtimeHandler("support.ticket", (qc, e) => {
  qc.setQueryData<SupportTicketDetail>(supportKeys.ticket(e.data.id), (d) => (d ? { ...d, ...e.data } : d));
  void qc.invalidateQueries({ queryKey: supportKeys.mineAll() });
  refreshStaffViews(qc, e.data.id);
});
registerReconnectRefetch(supportKeys.all());

// ---- the caller's tickets ----------------------------------------------------------

/** The caller's tickets, newest first (`data.items`); `status` filters. */
export function useMyTickets(status?: SupportStatus, options?: InfiniteOverrides<SupportTicket>): PagedQueryResult<SupportTicket> {
  const s = useEndpoints(supportEndpoints);
  return usePagedQuery<SupportTicket>(supportKeys.mine(status), (cursor, signal) => s.tickets({ status, cursor }, { signal }), options);
}

/**
 * One of the caller's tickets with its thread. Subscribes to the ticket's topic while
 * mounted, so staff replies and status changes arrive live. Pass null to disable.
 */
export function useTicket(id: string | null | undefined, options?: QueryOverrides<SupportTicketDetail>): UseQueryResult<SupportTicketDetail, ApiError> {
  const s = useEndpoints(supportEndpoints);
  useTopic(id ? supportTicketTopic(id) : null);
  return useApiQuery(supportKeys.ticket(id ?? ""), ({ signal }) => s.ticket(id ?? "", { signal }), options, !!id);
}

function useTicketMutation<V>(fn: (s: ReturnType<typeof supportEndpoints>, v: V) => Promise<SupportTicketDetail>): UseMutationResult<SupportTicketDetail, ApiError, V> {
  const s = useEndpoints(supportEndpoints);
  const qc = useQueryClient();
  return useMutation<SupportTicketDetail, ApiError, V>({
    mutationFn: (v) => fn(s, v),
    onSuccess: (detail) => {
      qc.setQueryData(supportKeys.ticket(detail.id), detail);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: supportKeys.mineAll() }),
  });
}

/** `POST /v1/support/tickets`: resolves to the new ticket with its thread. */
export function useCreateTicket(): UseMutationResult<SupportTicketDetail, ApiError, CreateTicketRequest> {
  return useTicketMutation<CreateTicketRequest>((s, body) => s.create(body));
}

/** Reply on one of the caller's tickets (reopens it). */
export function useReplyTicket(id: string): UseMutationResult<SupportTicketDetail, ApiError, ReplyTicketRequest> {
  return useTicketMutation<ReplyTicketRequest>((s, body) => s.reply(id, body));
}

/** Close one of the caller's tickets. */
export function useCloseTicket(): UseMutationResult<SupportTicketDetail, ApiError, string> {
  return useTicketMutation<string>((s, id) => s.close(id));
}

/** Reopen one of the caller's closed tickets. */
export function useReopenTicket(): UseMutationResult<SupportTicketDetail, ApiError, string> {
  return useTicketMutation<string>((s, id) => s.reopen(id));
}

// ---- staff ---------------------------------------------------------------------------

/** The staff queue as an infinite list (`data.items`). */
export function useAdminTickets(params: AdminTicketListParams = {}, options?: InfiniteOverrides<AdminSupportTicket>): PagedQueryResult<AdminSupportTicket> {
  const s = useEndpoints(supportEndpoints);
  return usePagedQuery<AdminSupportTicket>(
    supportKeys.admin.list(listKeyParams(params)),
    (cursor, signal) => s.admin.tickets({ ...params, cursor }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** One ticket for staff: owner, assignee and the full thread with notes. Subscribes to its topic while mounted. */
export function useAdminTicket(id: string | null | undefined, options?: QueryOverrides<AdminSupportTicketDetail>): UseQueryResult<AdminSupportTicketDetail, ApiError> {
  const s = useEndpoints(supportEndpoints);
  useTopic(id ? supportTicketTopic(id) : null);
  return useApiQuery(supportKeys.admin.ticket(id ?? ""), ({ signal }) => s.admin.ticket(id ?? "", { signal }), options, !!id);
}

function useAdminTicketMutation<V>(fn: (s: ReturnType<typeof supportEndpoints>, v: V) => Promise<AdminSupportTicketDetail>): UseMutationResult<AdminSupportTicketDetail, ApiError, V> {
  const s = useEndpoints(supportEndpoints);
  const qc = useQueryClient();
  return useMutation<AdminSupportTicketDetail, ApiError, V>({
    mutationFn: (v) => fn(s, v),
    onSuccess: (detail) => {
      qc.setQueryData(supportKeys.admin.ticket(detail.id), detail);
    },
    onSettled: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: supportKeys.admin.lists() }),
        qc.invalidateQueries({ queryKey: supportKeys.admin.stats() }),
        qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
      ]);
    },
  });
}

/** Staff reply (emails and notifies the user) or internal note. */
export function useAdminReply(id: string): UseMutationResult<AdminSupportTicketDetail, ApiError, AdminReplyRequest> {
  return useAdminTicketMutation<AdminReplyRequest>((s, body) => s.admin.reply(id, body));
}

/** Change status and/or assignee. */
export function useAdminUpdateTicket(id: string): UseMutationResult<AdminSupportTicketDetail, ApiError, AdminUpdateTicketRequest> {
  return useAdminTicketMutation<AdminUpdateTicketRequest>((s, body) => s.admin.update(id, body));
}

/** `GET /v1/admin/support/stats`: queue counts for badges and the queue header. */
export function useSupportStats(options?: QueryOverrides<SupportStats>): UseQueryResult<SupportStats, ApiError> {
  const s = useEndpoints(supportEndpoints);
  return useApiQuery(supportKeys.admin.stats(), ({ signal }) => s.admin.stats({ signal }), { staleTime: 30_000, ...options });
}

/** Active staff for the assignee picker (cached 5 min). */
export function useSupportAssignees(options?: QueryOverrides<{ items: SupportStaffRef[] }>): UseQueryResult<{ items: SupportStaffRef[] }, ApiError> {
  const s = useEndpoints(supportEndpoints);
  return useApiQuery(supportKeys.admin.assignees(), ({ signal }) => s.admin.assignees({ signal }), { staleTime: 5 * 60_000, ...options });
}
