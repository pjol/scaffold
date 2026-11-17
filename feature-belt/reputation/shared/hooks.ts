"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useEffect } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { reputationEndpoints } from "./api";
import type { Reputation, ReputationDecideRequest, ReputationFlag, ReputationFlagStatus, ReputationSummary } from "./types";

/** Query keys. `reputationKeys.all()` invalidates everything. */
export const reputationKeys = {
  all: () => ["reputation"] as const,
  me: () => ["reputation", "me"] as const,
  person: (userId: string) => ["reputation", "person", userId] as const,
  people: (userIds: readonly string[]) => ["reputation", "people", [...userIds].sort().join(",")] as const,
  admin: {
    all: () => ["reputation", "admin"] as const,
    flags: (status: ReputationFlagStatus) => ["reputation", "admin", "flags", status] as const,
    person: (userId: string) => ["reputation", "admin", "person", userId] as const,
  },
} as const;

/** Scores move when the server rescores (every snapshot interval); refetch at a calm pace. */
const STALE_MS = 60_000;
const REFRESH_MS = 5 * 60_000;

/** `GET /v1/reputation/me`: the caller's score and everything it is made of. */
export function useMyReputation(): UseQueryResult<Reputation, ApiError> {
  const api = useEndpoints(reputationEndpoints);
  return useApiQuery(reputationKeys.me(), ({ signal }) => api.me({ signal }), { staleTime: STALE_MS, refetchInterval: REFRESH_MS });
}

/** One person's score, tier and status. Pass null to disable. Lists prime it with `useReputationSummaries`. */
export function useReputationSummary(userId: string | null | undefined): UseQueryResult<ReputationSummary, ApiError> {
  const api = useEndpoints(reputationEndpoints);
  return useApiQuery(reputationKeys.person(userId ?? ""), ({ signal }) => api.person(userId ?? "", { signal }), { staleTime: STALE_MS }, !!userId);
}

/**
 * Many people's summaries in one request (≤ 100 ids), for lists. Each result also fills
 * that person's `useReputationSummary` cache, so badges in the rows don't fetch again.
 */
export function useReputationSummaries(userIds: readonly string[]): UseQueryResult<{ items: ReputationSummary[] }, ApiError> {
  const api = useEndpoints(reputationEndpoints);
  const qc = useQueryClient();
  const ids = [...new Set(userIds)].slice(0, 100);
  const q = useApiQuery(reputationKeys.people(ids), ({ signal }) => api.people(ids, { signal }), { staleTime: STALE_MS }, ids.length > 0);
  useEffect(() => {
    for (const s of q.data?.items ?? []) qc.setQueryData(reputationKeys.person(s.user_id), s);
  }, [q.data, qc]);
  return q;
}

/** Admin → Reputation: flags by status (open oldest first; decided newest first). */
export function useReputationFlags(status: ReputationFlagStatus): PagedQueryResult<ReputationFlag> {
  const api = useEndpoints(reputationEndpoints);
  return usePagedQuery<ReputationFlag>(reputationKeys.admin.flags(status), (cursor, signal) => api.admin.flags({ status, cursor, limit: 50 }, { signal }));
}

/** Anyone's full reputation, for staff. Pass null to disable. */
export function useAdminReputation(userId: string | null | undefined): UseQueryResult<Reputation, ApiError> {
  const api = useEndpoints(reputationEndpoints);
  return useApiQuery(reputationKeys.admin.person(userId ?? ""), ({ signal }) => api.admin.person(userId ?? "", { signal }), undefined, !!userId);
}

/** Clear or confirm an open flag (audited). Refreshes the queue, the person and the audit log. */
export function useDecideReputationFlag(): UseMutationResult<ReputationFlag, ApiError, { id: string } & ReputationDecideRequest> {
  const api = useEndpoints(reputationEndpoints);
  const qc = useQueryClient();
  return useMutation<ReputationFlag, ApiError, { id: string } & ReputationDecideRequest>({
    mutationFn: ({ id, ...body }) => api.admin.decide(id, body),
    onSettled: (flag) =>
      Promise.all([
        qc.invalidateQueries({ queryKey: reputationKeys.admin.all() }),
        flag ? qc.invalidateQueries({ queryKey: reputationKeys.person(flag.user.id) }) : undefined,
        qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
      ]),
  });
}
