"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { leaderboardEndpoints } from "./api";
import type { LeaderboardBoard, LeaderboardEntry, LeaderboardProfile, LeaderboardStanding, LeaderboardVisibility } from "./types";

/** Query keys. `leaderboardKeys.all()` invalidates everything. */
export const leaderboardKeys = {
  all: () => ["leaderboard"] as const,
  boards: () => ["leaderboard", "boards"] as const,
  entries: (board: string) => ["leaderboard", "entries", board] as const,
  standing: (board: string) => ["leaderboard", "standing", board] as const,
  profile: () => ["leaderboard", "profile"] as const,
} as const;

/** Boards refresh on the server every few seconds to a minute; poll at the same pace. */
const REFRESH_MS = 30_000;

export function useLeaderboardBoards(): UseQueryResult<{ items: LeaderboardBoard[] }, ApiError> {
  const api = useEndpoints(leaderboardEndpoints);
  return useApiQuery(leaderboardKeys.boards(), ({ signal }) => api.boards({ signal }), { refetchInterval: REFRESH_MS });
}

export function useLeaderboardEntries(board: string | null | undefined): PagedQueryResult<LeaderboardEntry> {
  const api = useEndpoints(leaderboardEndpoints);
  return usePagedQuery<LeaderboardEntry>(
    leaderboardKeys.entries(board ?? ""),
    (cursor, signal) => api.entries(board ?? "", { cursor, limit: 50 }, { signal }),
    { refetchInterval: REFRESH_MS },
    !!board,
  );
}

export function useLeaderboardStanding(board: string | null | undefined): UseQueryResult<LeaderboardStanding, ApiError> {
  const api = useEndpoints(leaderboardEndpoints);
  return useApiQuery(leaderboardKeys.standing(board ?? ""), ({ signal }) => api.standing(board ?? "", { signal }), { refetchInterval: REFRESH_MS }, !!board);
}

export function useLeaderboardProfile(): UseQueryResult<LeaderboardProfile, ApiError> {
  const api = useEndpoints(leaderboardEndpoints);
  return useApiQuery(leaderboardKeys.profile(), ({ signal }) => api.profile({ signal }));
}

/** Show the caller's name, their alias, or hide them from every board. */
export function useSetLeaderboardVisibility(): UseMutationResult<LeaderboardProfile, ApiError, LeaderboardVisibility, { previous: LeaderboardProfile | undefined }> {
  const api = useEndpoints(leaderboardEndpoints);
  const qc = useQueryClient();
  return useMutation<LeaderboardProfile, ApiError, LeaderboardVisibility, { previous: LeaderboardProfile | undefined }>({
    mutationFn: (v) => api.setVisibility(v),
    onMutate: async (v) => {
      await qc.cancelQueries({ queryKey: leaderboardKeys.profile() });
      const previous = qc.getQueryData<LeaderboardProfile>(leaderboardKeys.profile());
      qc.setQueryData<LeaderboardProfile>(leaderboardKeys.profile(), (p) => (p ? { ...p, visibility: v } : p));
      return { previous };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.previous) qc.setQueryData(leaderboardKeys.profile(), ctx.previous);
    },
    onSuccess: (p) => qc.setQueryData(leaderboardKeys.profile(), p),
    onSettled: () => void qc.invalidateQueries({ queryKey: leaderboardKeys.all() }),
  });
}

/** A score for display, by the board's format (money through `formatMoney` when given). */
export function formatLeaderboardScore(board: Pick<LeaderboardBoard, "format" | "currency">, score: string, formatMoney?: (amount: string, currency: string) => string): string {
  const grouped = (s: string) => {
    const neg = s.startsWith("-");
    const [int = "0", frac] = (neg ? s.slice(1) : s).split(".");
    return (neg ? "-" : "") + int.replace(/\B(?=(\d{3})+(?!\d))/g, ",") + (frac ? `.${frac}` : "");
  };
  if (board.format === "percent") return `${grouped(score)}%`;
  if (board.format === "money" && board.currency) return formatMoney ? formatMoney(score, board.currency) : `${grouped(score)} ${board.currency}`;
  return grouped(score);
}
