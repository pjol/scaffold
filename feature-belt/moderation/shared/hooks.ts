"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { moderationEndpoints } from "./api";
import type {
  BlockedIds,
  BlockedUser,
  CaseActionRequest,
  CaseDetail,
  CreateReportRequest,
  ModerationCase,
  ModerationCaseListParams,
  ModerationSummary,
  MyReport,
} from "./types";

/** Query keys. `moderationKeys.all()` invalidates everything this feature caches. */
export const moderationKeys = {
  all: () => ["moderation"] as const,
  blocks: () => ["moderation", "blocks"] as const,
  blockList: () => ["moderation", "blocks", "list"] as const,
  blockedIds: () => ["moderation", "blocks", "ids"] as const,
  myReports: () => ["moderation", "reports"] as const,
  admin: {
    all: () => ["moderation", "admin"] as const,
    summary: () => ["moderation", "admin", "summary"] as const,
    cases: (params: Record<string, unknown> = {}) => ["moderation", "admin", "cases", params] as const,
    case: (id: string) => ["moderation", "admin", "case", id] as const,
  },
};

// ---- reports -----------------------------------------------------------------

/** `POST /v1/moderation/reports`. Reporting the same thing again updates your earlier report. */
export function useCreateReport(): UseMutationResult<MyReport, ApiError, CreateReportRequest> {
  const m = useEndpoints(moderationEndpoints);
  const qc = useQueryClient();
  return useMutation<MyReport, ApiError, CreateReportRequest>({
    mutationFn: (body) => m.report(body),
    onSettled: () => qc.invalidateQueries({ queryKey: moderationKeys.myReports() }),
  });
}

/** `GET /v1/moderation/reports` as an infinite list. */
export function useMyReports(options?: InfiniteOverrides<MyReport>): PagedQueryResult<MyReport> {
  const m = useEndpoints(moderationEndpoints);
  return usePagedQuery<MyReport>(moderationKeys.myReports(), (cursor, signal) => m.myReports({ cursor }, { signal }), options);
}

// ---- blocks ------------------------------------------------------------------

/** People the caller blocked, newest first (`data.items`). */
export function useBlockedUsers(options?: InfiniteOverrides<BlockedUser>): PagedQueryResult<BlockedUser> {
  const m = useEndpoints(moderationEndpoints);
  return usePagedQuery<BlockedUser>(moderationKeys.blockList(), (cursor, signal) => m.blocks({ cursor }, { signal }), options);
}

/** Ids of everyone the caller blocked (cached 5 min, refreshed on every block change). */
export function useBlockedIds(options?: QueryOverrides<BlockedIds>): UseQueryResult<BlockedIds, ApiError> {
  const m = useEndpoints(moderationEndpoints);
  return useApiQuery(moderationKeys.blockedIds(), ({ signal }) => m.blockedIds({ signal }), { staleTime: 5 * 60_000, ...options });
}

/** Whether the caller blocked `userId` (false while loading or for null). */
export function useIsBlocked(userId: string | null | undefined): boolean {
  const ids = useBlockedIds({ enabled: !!userId });
  return !!userId && (ids.data?.user_ids.includes(userId) ?? false);
}

function setBlocked(qc: QueryClient, userId: string, blocked: boolean): BlockedIds | undefined {
  const key = moderationKeys.blockedIds();
  const previous = qc.getQueryData<BlockedIds>(key);
  if (previous) {
    const rest = previous.user_ids.filter((id) => id !== userId);
    qc.setQueryData<BlockedIds>(key, { user_ids: blocked ? [userId, ...rest] : rest });
  }
  if (!blocked) {
    qc.setQueryData<InfiniteData<Page<BlockedUser>>>(moderationKeys.blockList(), (data) =>
      data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((b) => b.user_id !== userId) })) } : data,
    );
  }
  return previous;
}

interface BlockSnapshot {
  previous: BlockedIds | undefined;
}

/** Shared invalidation after any block change: block state shows in many features' lists. */
function afterBlockChange(qc: QueryClient) {
  return qc.invalidateQueries({ queryKey: moderationKeys.blocks() });
}

/** `POST /v1/moderation/blocks` (optimistic on `useBlockedIds`). Pass the user id. */
export function useBlockUser(): UseMutationResult<BlockedUser, ApiError, string, BlockSnapshot> {
  const m = useEndpoints(moderationEndpoints);
  const qc = useQueryClient();
  return useMutation<BlockedUser, ApiError, string, BlockSnapshot>({
    mutationFn: (userId) => m.block(userId),
    onMutate: async (userId) => {
      await qc.cancelQueries({ queryKey: moderationKeys.blockedIds() });
      return { previous: setBlocked(qc, userId, true) };
    },
    onError: (_e, _id, ctx) => {
      if (ctx?.previous) qc.setQueryData(moderationKeys.blockedIds(), ctx.previous);
    },
    onSettled: () => afterBlockChange(qc),
  });
}

/** `DELETE /v1/moderation/blocks/{user_id}` (optimistic on the id set and the list). */
export function useUnblockUser(): UseMutationResult<void, ApiError, string, BlockSnapshot> {
  const m = useEndpoints(moderationEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string, BlockSnapshot>({
    mutationFn: (userId) => m.unblock(userId),
    onMutate: async (userId) => {
      await qc.cancelQueries({ queryKey: moderationKeys.blocks() });
      return { previous: setBlocked(qc, userId, false) };
    },
    onError: (_e, _id, ctx) => {
      if (ctx?.previous) qc.setQueryData(moderationKeys.blockedIds(), ctx.previous);
    },
    onSettled: () => afterBlockChange(qc),
  });
}

// ---- staff queue -------------------------------------------------------------

/** `GET /v1/admin/moderation/summary`: open cases against the 24-hour target. */
export function useModerationSummary(options?: QueryOverrides<ModerationSummary>): UseQueryResult<ModerationSummary, ApiError> {
  const m = useEndpoints(moderationEndpoints);
  return useApiQuery(moderationKeys.admin.summary(), ({ signal }) => m.admin.summary({ signal }), options);
}

/** `GET /v1/admin/moderation/cases` as an infinite list. */
export function useModerationCases(params: ModerationCaseListParams = {}, options?: InfiniteOverrides<ModerationCase>): PagedQueryResult<ModerationCase> {
  const m = useEndpoints(moderationEndpoints);
  return usePagedQuery<ModerationCase>(
    moderationKeys.admin.cases(listKeyParams(params)),
    (cursor, signal) => m.admin.cases({ ...params, cursor }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** `GET /v1/admin/moderation/cases/{id}`. Pass null to disable. */
export function useModerationCase(id: string | null | undefined, options?: QueryOverrides<CaseDetail>): UseQueryResult<CaseDetail, ApiError> {
  const m = useEndpoints(moderationEndpoints);
  return useApiQuery(moderationKeys.admin.case(id ?? ""), ({ signal }) => m.admin.case(id ?? "", { signal }), options, !!id);
}

/** `POST /v1/admin/moderation/cases/{id}/actions`. */
export function useCaseAction(): UseMutationResult<CaseDetail, ApiError, { id: string } & CaseActionRequest> {
  const m = useEndpoints(moderationEndpoints);
  const qc = useQueryClient();
  return useMutation<CaseDetail, ApiError, { id: string } & CaseActionRequest>({
    mutationFn: ({ id, ...body }) => m.admin.act(id, body),
    onSuccess: (detail) => qc.setQueryData(moderationKeys.admin.case(detail.id), detail),
    onSettled: () =>
      Promise.all([qc.invalidateQueries({ queryKey: moderationKeys.admin.all() }), qc.invalidateQueries({ queryKey: ["admin", "audit"] }), qc.invalidateQueries({ queryKey: ["admin", "users"] })]),
  });
}

// ---- realtime ----------------------------------------------------------------

registerRealtimeHandler("moderation.blocks_changed", (qc) => void qc.invalidateQueries({ queryKey: moderationKeys.blocks() }));
registerRealtimeHandler("moderation.case_opened", (qc) => void qc.invalidateQueries({ queryKey: moderationKeys.admin.all() }));
registerRealtimeHandler("moderation.case_closed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: moderationKeys.admin.summary() });
  void qc.invalidateQueries({ queryKey: ["moderation", "admin", "cases"] });
  void qc.invalidateQueries({ queryKey: moderationKeys.admin.case(e.data.case_id) });
});
registerReconnectRefetch(moderationKeys.all());
