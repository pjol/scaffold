"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, useTopic, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { topic, topicId } from "../realtime/topics";
import type { Page } from "../types";
import { oracleEndpoints } from "./api";
import type {
  OracleAdminFeed,
  OracleCreateFeedRequest,
  OracleDailyBar,
  OracleFeed,
  OracleFeedDetail,
  OracleFeedListParams,
  OracleImportHistoryRequest,
  OracleObservation,
  OracleObservationListParams,
  OraclePartConfig,
  OraclePostPrintRequest,
  OraclePrice,
  OracleSourceInput,
  OracleSourcesStatus,
  OracleUpdateFeedRequest,
} from "./types";

declare module "../types" {
  interface RealtimeEventMap {
    "oracle.price": OraclePrice;
    "oracle.prices": { items: OraclePrice[] };
    "oracle.observation": OracleObservation;
    "oracle.history": { feed_id: string };
    "oracle.feeds": { changed: boolean };
  }
}

/** Query keys. `oracleKeys.all()` invalidates everything this feature caches. */
export const oracleKeys = {
  all: () => ["oracle"] as const,
  lists: () => ["oracle", "feeds"] as const,
  list: (params: Record<string, unknown> = {}) => ["oracle", "feeds", params] as const,
  details: () => ["oracle", "feed"] as const,
  feed: (key: string) => ["oracle", "feed", key] as const,
  histories: () => ["oracle", "history"] as const,
  history: (key: string, days: number) => ["oracle", "history", key, days] as const,
  observationLists: () => ["oracle", "observations"] as const,
  observations: (params: Record<string, unknown> = {}) => ["oracle", "observations", params] as const,
  admin: {
    all: () => ["oracle", "admin"] as const,
    lists: () => ["oracle", "admin", "feeds"] as const,
    list: (params: Record<string, unknown> = {}) => ["oracle", "admin", "feeds", params] as const,
    feed: (key: string) => ["oracle", "admin", "feed", key] as const,
    sources: () => ["oracle", "admin", "sources"] as const,
    observations: (params: Record<string, unknown> = {}) => ["oracle", "admin", "observations", params] as const,
  },
};

/** One feed's topic: prices, observations and history changes. */
export const oracleFeedTopic = (feedId: string): string => topic("oracle", feedId);
/** Price changes of every feed (one event per tick) and catalog changes. */
export const ORACLE_FEEDS_TOPIC = "oracle:feeds";
/** Every screened observation. */
export const ORACLE_OBSERVATIONS_TOPIC = "oracle:observations";

const MAX_LIVE_OBSERVATIONS = 50;

function applyPrice<T extends OracleFeed>(f: T, p: OraclePrice): T {
  if (f.id !== p.feed_id) return f;
  return { ...f, price: p.price, target: p.target, status: p.status, updated_at: p.updated_at };
}

function applyPricesToPages(qc: QueryClient, prefix: readonly unknown[], prices: OraclePrice[]) {
  const byId = new Map(prices.map((p) => [p.feed_id, p]));
  qc.setQueriesData<InfiniteData<Page<OracleFeed>>>({ queryKey: prefix }, (data) => {
    if (!data?.pages) return data;
    let changed = false;
    const pages = data.pages.map((page) => ({
      ...page,
      items: page.items.map((f) => {
        const p = byId.get(f.id);
        if (!p) return f;
        changed = true;
        return applyPrice(f, p);
      }),
    }));
    return changed ? { ...data, pages } : data;
  });
}

function applyPriceToDetails(qc: QueryClient, p: OraclePrice) {
  for (const prefix of [oracleKeys.details(), ["oracle", "admin", "feed"] as const]) {
    qc.setQueriesData<OracleFeedDetail>({ queryKey: prefix }, (d) => (d?.feed && d.feed.id === p.feed_id ? { ...d, feed: applyPrice(d.feed, p) } : d));
  }
}

function prependObservation(qc: QueryClient, feedTopicId: string | null, o: OracleObservation) {
  for (const prefix of [oracleKeys.details(), ["oracle", "admin", "feed"] as const]) {
    qc.setQueriesData<OracleFeedDetail>({ queryKey: prefix }, (d) => {
      if (!d?.feed || (d.feed.id !== feedTopicId && d.feed.id !== o.feed_id)) return d;
      if (d.observations.some((x) => x.id === o.id)) return d;
      return { ...d, observations: [o, ...d.observations].slice(0, MAX_LIVE_OBSERVATIONS) };
    });
  }
  // Observation lists without a feed filter take every new print that matches their verdict.
  for (const prefix of [oracleKeys.observationLists(), ["oracle", "admin", "observations"] as const]) {
    for (const [key, data] of qc.getQueriesData<InfiniteData<Page<OracleObservation>>>({ queryKey: prefix })) {
      const params = (key[key.length - 1] ?? {}) as OracleObservationListParams;
      const [first, ...rest] = data?.pages ?? [];
      if (!data || !first || params.feed || (params.verdict && params.verdict !== o.verdict)) continue;
      if (data.pages.some((p) => p.items.some((x) => x.id === o.id))) continue;
      qc.setQueryData(key, { ...data, pages: [{ ...first, items: [o, ...first.items] }, ...rest] });
    }
  }
}

registerRealtimeHandler("oracle.price", (qc, e) => {
  applyPriceToDetails(qc, e.data);
});
registerRealtimeHandler("oracle.prices", (qc, e) => {
  applyPricesToPages(qc, oracleKeys.lists(), e.data.items);
  applyPricesToPages(qc, oracleKeys.admin.lists(), e.data.items);
});
registerRealtimeHandler("oracle.observation", (qc, e) => {
  prependObservation(qc, topicId(e.topic, "oracle"), e.data);
});
registerRealtimeHandler("oracle.history", (qc) => {
  void qc.invalidateQueries({ queryKey: oracleKeys.histories() });
});
registerRealtimeHandler("oracle.feeds", (qc) => {
  void qc.invalidateQueries({ queryKey: oracleKeys.lists() });
  void qc.invalidateQueries({ queryKey: oracleKeys.admin.lists() });
});
registerReconnectRefetch(oracleKeys.all());

// ---- reads ---------------------------------------------------------------------------

/** Listed, enabled feeds by key (`data.items`), with live prices while mounted. */
export function useOracleFeeds(params: OracleFeedListParams = {}, options?: InfiniteOverrides<OracleFeed>): PagedQueryResult<OracleFeed> {
  const o = useEndpoints(oracleEndpoints);
  useTopic(ORACLE_FEEDS_TOPIC);
  return usePagedQuery<OracleFeed>(oracleKeys.list(listKeyParams(params)), (cursor, signal) => o.feeds({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/**
 * One feed with its venues, parts, last day and observations. Subscribes to the
 * feed's topic while mounted (pass `feedId` once known), so prices and prints arrive
 * live. Pass null to disable.
 */
export function useOracleFeed(key: string | null | undefined, options?: QueryOverrides<OracleFeedDetail>): UseQueryResult<OracleFeedDetail, ApiError> {
  const o = useEndpoints(oracleEndpoints);
  const q = useApiQuery(oracleKeys.feed(key ?? ""), ({ signal }) => o.feed(key ?? "", { signal }), options, !!key);
  useTopic(q.data?.feed.id ? oracleFeedTopic(q.data.feed.id) : null);
  return q;
}

/** Daily bars for the last `days` (≤ 365), oldest first. */
export function useOracleHistory(key: string | null | undefined, days = 90, options?: QueryOverrides<{ items: OracleDailyBar[] }>): UseQueryResult<{ items: OracleDailyBar[] }, ApiError> {
  const o = useEndpoints(oracleEndpoints);
  return useApiQuery(oracleKeys.history(key ?? "", days), ({ signal }) => o.history(key ?? "", days, { signal }), { staleTime: 5 * 60_000, ...options }, !!key);
}

/** Screened observations, newest first. Without a feed filter, new prints arrive live. */
export function useOracleObservations(params: OracleObservationListParams = {}, options?: InfiniteOverrides<OracleObservation>): PagedQueryResult<OracleObservation> {
  const o = useEndpoints(oracleEndpoints);
  useTopic(params.feed ? null : ORACLE_OBSERVATIONS_TOPIC);
  return usePagedQuery<OracleObservation>(
    oracleKeys.observations(listKeyParams(params)),
    (cursor, signal) => o.observations({ ...params, cursor }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

// ---- staff ---------------------------------------------------------------------------

/** Every feed, unlisted and disabled too (staff). */
export function useOracleAdminFeeds(params: OracleFeedListParams = {}, options?: InfiniteOverrides<OracleFeed>): PagedQueryResult<OracleFeed> {
  const o = useEndpoints(oracleEndpoints);
  useTopic(ORACLE_FEEDS_TOPIC);
  return usePagedQuery<OracleFeed>(oracleKeys.admin.list(listKeyParams(params)), (cursor, signal) => o.admin.feeds({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** One feed with its configuration (staff). Live like `useOracleFeed`. */
export function useOracleAdminFeed(key: string | null | undefined, options?: QueryOverrides<OracleAdminFeed>): UseQueryResult<OracleAdminFeed, ApiError> {
  const o = useEndpoints(oracleEndpoints);
  const q = useApiQuery(oracleKeys.admin.feed(key ?? ""), ({ signal }) => o.admin.feed(key ?? "", { signal }), options, !!key);
  useTopic(q.data?.feed.id ? oracleFeedTopic(q.data.feed.id) : null);
  return q;
}

/** Observations of every feed, unlisted ones too (staff). Live without a feed filter. */
export function useOracleAdminObservations(params: OracleObservationListParams = {}, options?: InfiniteOverrides<OracleObservation>): PagedQueryResult<OracleObservation> {
  const o = useEndpoints(oracleEndpoints);
  useTopic(params.feed ? null : ORACLE_OBSERVATIONS_TOPIC);
  return usePagedQuery<OracleObservation>(
    oracleKeys.admin.observations(listKeyParams(params)),
    (cursor, signal) => o.admin.observations({ ...params, cursor }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** Source health, budgets, the decision model and the leading instance (refreshes every 15 s). */
export function useOracleSources(options?: QueryOverrides<OracleSourcesStatus>): UseQueryResult<OracleSourcesStatus, ApiError> {
  const o = useEndpoints(oracleEndpoints);
  return useApiQuery(oracleKeys.admin.sources(), ({ signal }) => o.admin.sources({ signal }), { refetchInterval: 15_000, ...options });
}

function useOracleAdminMutation<V>(fn: (o: ReturnType<typeof oracleEndpoints>, v: V) => Promise<OracleAdminFeed>): UseMutationResult<OracleAdminFeed, ApiError, V> {
  const o = useEndpoints(oracleEndpoints);
  const qc = useQueryClient();
  return useMutation<OracleAdminFeed, ApiError, V>({
    mutationFn: (v) => fn(o, v),
    onSuccess: (d) => {
      qc.setQueryData(oracleKeys.admin.feed(d.feed.key), d);
    },
    onSettled: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: oracleKeys.lists() }),
        qc.invalidateQueries({ queryKey: oracleKeys.details() }),
        qc.invalidateQueries({ queryKey: oracleKeys.admin.lists() }),
        qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
      ]);
    },
  });
}

/** Create a feed (admin). */
export function useCreateOracleFeed(): UseMutationResult<OracleAdminFeed, ApiError, OracleCreateFeedRequest> {
  return useOracleAdminMutation<OracleCreateFeedRequest>((o, body) => o.admin.create(body));
}

/** Change a feed's name, params, listing or enabled state (admin). */
export function useUpdateOracleFeed(key: string): UseMutationResult<OracleAdminFeed, ApiError, OracleUpdateFeedRequest> {
  return useOracleAdminMutation<OracleUpdateFeedRequest>((o, body) => o.admin.update(key, body));
}

/** Replace a single's source rows (admin). */
export function usePutOracleSources(key: string): UseMutationResult<OracleAdminFeed, ApiError, OracleSourceInput[]> {
  return useOracleAdminMutation<OracleSourceInput[]>((o, sources) => o.admin.putSources(key, sources));
}

/** Replace a basket's parts (admin). */
export function usePutOracleParts(key: string): UseMutationResult<OracleAdminFeed, ApiError, OraclePartConfig[]> {
  return useOracleAdminMutation<OraclePartConfig[]>((o, parts) => o.admin.putParts(key, parts));
}

/** Post a price by hand (admin; the feed needs a manual source). The print arrives over realtime once screened. */
export function usePostOraclePrint(key: string): UseMutationResult<{ queued: boolean }, ApiError, OraclePostPrintRequest> {
  const o = useEndpoints(oracleEndpoints);
  const qc = useQueryClient();
  return useMutation<{ queued: boolean }, ApiError, OraclePostPrintRequest>({
    mutationFn: (body) => o.admin.postPrint(key, body),
    onSettled: () => qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
  });
}

/** Import daily closes for the chart (admin). */
export function useImportOracleHistory(key: string): UseMutationResult<{ stored: number }, ApiError, OracleImportHistoryRequest> {
  const o = useEndpoints(oracleEndpoints);
  const qc = useQueryClient();
  return useMutation<{ stored: number }, ApiError, OracleImportHistoryRequest>({
    mutationFn: (body) => o.admin.importHistory(key, body),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: oracleKeys.histories() }), qc.invalidateQueries({ queryKey: ["admin", "audit"] })]),
  });
}
