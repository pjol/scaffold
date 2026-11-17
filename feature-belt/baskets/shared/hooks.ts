"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, useTopic, type PagedQueryResult } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import { basketsEndpoints } from "./api";
import type {
  BasketsAdminBasket,
  BasketsAdminListParams,
  BasketsCandidate,
  BasketsCreateRequest,
  BasketsCreateResult,
  BasketsDetail,
  BasketsInput,
  BasketsListParams,
  BasketsPreview,
  BasketsReportRequest,
  BasketsStatus,
  BasketsSummary,
} from "./types";

declare module "../types" {
  interface RealtimeEventMap {
    "baskets.basket": BasketsSummary;
  }
}

/** Query keys. `basketsKeys.all()` invalidates everything baskets cache. */
export const basketsKeys = {
  all: () => ["baskets"] as const,
  lists: () => ["baskets", "list"] as const,
  list: (params: BasketsListParams = {}) => ["baskets", "list", listKeyParams(params)] as const,
  mine: () => ["baskets", "list", "mine"] as const,
  detail: (symbol: string) => ["baskets", "detail", symbol] as const,
  search: (q: string) => ["baskets", "search", q] as const,
  preview: (input: BasketsInput) => ["baskets", "preview", input] as const,
  admin: {
    all: () => ["baskets", "admin"] as const,
    list: (params: BasketsAdminListParams = {}) => ["baskets", "admin", "list", listKeyParams(params)] as const,
  },
} as const;

/** Labels for a basket's status. */
export const BASKETS_STATUS_LABEL: Record<BasketsStatus, string> = {
  active: "Trading",
  reduce_only: "Winding down",
  archived: "Archived",
};

/** A weight ("0.4213") as a percentage ("42.1%"). */
export function formatBasketsWeight(weight: string | null | undefined): string {
  if (!weight) return "–";
  const n = Number(weight) * 100;
  return Number.isFinite(n) ? `${n.toFixed(1)}%` : "–";
}

/** The basket's realtime topic. */
export function basketsTopic(id: string): string {
  return topic("baskets", id);
}

// ───────────────────────────────────────────────────────────── realtime ──

registerRealtimeHandler("baskets.basket", (qc, e) => {
  const s = e.data;
  qc.setQueryData<BasketsDetail>(basketsKeys.detail(s.symbol), (d) =>
    d ? { ...d, name: s.name, description: s.description, status: s.status, hidden: s.hidden, entries: s.entries, flagged: s.flagged } : d,
  );
  void qc.invalidateQueries({ queryKey: basketsKeys.lists() });
});
registerReconnectRefetch(basketsKeys.all());

// ───────────────────────────────────────────────────────────────── reads ──

/** Discovery: new or popular, live (default) or archived, matching `q`. */
export function useBasketsList(params: BasketsListParams = {}): PagedQueryResult<BasketsSummary> {
  const api = useEndpoints(basketsEndpoints);
  return usePagedQuery<BasketsSummary>(basketsKeys.list(params), (cursor, signal) => api.list({ ...params, cursor, limit: params.limit ?? 24 }, { signal }));
}

/** Baskets the caller created or joined. */
export function useMyBaskets(): PagedQueryResult<BasketsSummary> {
  const api = useEndpoints(basketsEndpoints);
  return usePagedQuery<BasketsSummary>(basketsKeys.mine(), (cursor, signal) => api.mine({ cursor, limit: 24 }, { signal }));
}

/** One basket with live line prices (refreshes every 30 s; status changes arrive over realtime). */
export function useBasket(symbol: string | null | undefined): UseQueryResult<BasketsDetail, ApiError> {
  const api = useEndpoints(basketsEndpoints);
  const q = useApiQuery(basketsKeys.detail(symbol ?? ""), ({ signal }) => api.get(symbol ?? "", { signal }), { refetchInterval: 30_000 }, !!symbol);
  useTopic(q.data ? basketsTopic(q.data.id) : null);
  return q;
}

/** Markets a basket may hold, for a picker. Debounce `q` in the screen. */
export function useBasketsSearch(q: string, enabled = true): UseQueryResult<{ items: BasketsCandidate[] }, ApiError> {
  const api = useEndpoints(basketsEndpoints);
  return useApiQuery(basketsKeys.search(q), ({ signal }) => api.search(q, 20, { signal }), { staleTime: 30_000 }, enabled);
}

/** Live preview of a list (no writes). Debounce the input in the screen; the last result stays while the next loads. */
export function useBasketsPreview(input: BasketsInput, enabled = true): UseQueryResult<BasketsPreview, ApiError> {
  const api = useEndpoints(basketsEndpoints);
  const empty = !input.text?.trim() && !(input.picks?.length ?? 0);
  return useApiQuery(basketsKeys.preview(input), ({ signal }) => api.preview(input, { signal }), { placeholderData: (prev) => prev, staleTime: 15_000 }, enabled && !empty);
}

// ──────────────────────────────────────────────────────────────── writes ──

/** Create a basket, or join the one with the same lines. */
export function useCreateBasket(): UseMutationResult<BasketsCreateResult, ApiError, BasketsCreateRequest> {
  const api = useEndpoints(basketsEndpoints);
  const qc = useQueryClient();
  return useMutation<BasketsCreateResult, ApiError, BasketsCreateRequest>({
    mutationFn: (body) => api.create(body),
    onSuccess: (res) => {
      qc.setQueryData(basketsKeys.detail(res.basket.symbol), res.basket);
      void qc.invalidateQueries({ queryKey: basketsKeys.lists() });
      void qc.invalidateQueries({ queryKey: ["baskets", "preview"] });
    },
  });
}

/** Report a basket's name or description to staff. */
export function useReportBasket(symbol: string): UseMutationResult<void, ApiError, BasketsReportRequest> {
  const api = useEndpoints(basketsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, BasketsReportRequest>({
    mutationFn: (body) => api.report(symbol, body),
    onSuccess: () => qc.setQueryData<BasketsDetail>(basketsKeys.detail(symbol), (d) => (d ? { ...d, reported: true } : d)),
  });
}

// ───────────────────────────────────────────────────────────────── admin ──

export function useBasketsAdminList(params: BasketsAdminListParams = {}): PagedQueryResult<BasketsAdminBasket> {
  const api = useEndpoints(basketsEndpoints);
  return usePagedQuery<BasketsAdminBasket>(basketsKeys.admin.list(params), (cursor, signal) => api.admin.list({ ...params, cursor, limit: params.limit ?? 50 }, { signal }));
}

function useBasketsAdminMutation<V>(fn: (api: ReturnType<typeof basketsEndpoints>, v: V) => Promise<BasketsAdminBasket>): UseMutationResult<BasketsAdminBasket, ApiError, V> {
  const api = useEndpoints(basketsEndpoints);
  const qc = useQueryClient();
  return useMutation<BasketsAdminBasket, ApiError, V>({
    mutationFn: (v) => fn(api, v),
    onSuccess: (b) => {
      void qc.invalidateQueries({ queryKey: basketsKeys.admin.all() });
      void qc.invalidateQueries({ queryKey: basketsKeys.lists() });
      void qc.invalidateQueries({ queryKey: basketsKeys.detail(b.symbol) });
    },
  });
}

/** Staff: rename a basket (name and/or description). Audited. */
export function useRenameBasket(): UseMutationResult<BasketsAdminBasket, ApiError, { symbol: string; name?: string; description?: string }> {
  return useBasketsAdminMutation<{ symbol: string; name?: string; description?: string }>((api, { symbol, ...body }) => api.admin.rename(symbol, body));
}

/** Staff: hide (out of discovery, winds down, never reopens) or show again. Audited. */
export function useSetBasketHidden(): UseMutationResult<BasketsAdminBasket, ApiError, { symbol: string; hidden: boolean }> {
  return useBasketsAdminMutation<{ symbol: string; hidden: boolean }>((api, v) => api.admin.setHidden(v.symbol, v.hidden));
}

/** Admins: wind a basket down (reduce-only while held, then archived). Audited. */
export function useArchiveBasket(): UseMutationResult<BasketsAdminBasket, ApiError, string> {
  return useBasketsAdminMutation<string>((api, symbol) => api.admin.archive(symbol));
}

/** Staff: mark a basket's open reports handled. Audited. */
export function useResolveBasketReports(): UseMutationResult<BasketsAdminBasket, ApiError, string> {
  return useBasketsAdminMutation<string>((api, symbol) => api.admin.resolveReports(symbol));
}
