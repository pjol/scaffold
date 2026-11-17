"use client";

import { useInfiniteQuery, useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useMemo } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { REALTIME_FALLBACK_POLL_MS, useApiQuery, useConnectionState, usePagedQuery, useTopic, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import { exchangeEndpoints } from "./api";
import { flattenExchangeCandles, mergeExchangeLiveCandle } from "./format";
import type {
  ExchangeAccount,
  ExchangeAdminMarket,
  ExchangeBook,
  ExchangeCandle,
  ExchangeCandleInterval,
  ExchangeCandlePage,
  ExchangeCatalogMarket,
  ExchangeConfig,
  ExchangeFill,
  ExchangeFundingPayment,
  ExchangeLedgerEntry,
  ExchangeMarket,
  ExchangeMarketStats,
  ExchangeMarketStatus,
  ExchangeOrder,
  ExchangeOrderListParams,
  ExchangeOrderResult,
  ExchangeOverview,
  ExchangePlaceOrderRequest,
  ExchangeTrade,
} from "./types";

/** Query keys. `exchangeKeys.all()` invalidates everything the exchange caches. */
export const exchangeKeys = {
  all: () => ["exchange"] as const,
  config: () => ["exchange", "config"] as const,
  markets: () => ["exchange", "markets"] as const,
  market: (symbol: string) => ["exchange", "market", symbol] as const,
  book: (symbol: string) => ["exchange", "book", symbol] as const,
  trades: (symbol: string) => ["exchange", "trades", symbol] as const,
  candlesOf: (symbol: string) => ["exchange", "candles", symbol] as const,
  candles: (symbol: string, interval: ExchangeCandleInterval) => ["exchange", "candles", symbol, interval] as const,
  sparklines: (interval: ExchangeCandleInterval, points: number) => ["exchange", "sparklines", interval, points] as const,
  account: () => ["exchange", "account"] as const,
  history: () => ["exchange", "history"] as const,
  orders: (params: Record<string, unknown> = {}) => ["exchange", "history", "orders", params] as const,
  fills: () => ["exchange", "history", "fills"] as const,
  funding: () => ["exchange", "history", "funding"] as const,
  ledger: () => ["exchange", "history", "ledger"] as const,
  admin: {
    all: () => ["exchange", "admin"] as const,
    overview: () => ["exchange", "admin", "overview"] as const,
    markets: () => ["exchange", "admin", "markets"] as const,
  },
} as const;

/** The topic carrying every market's stats (the markets list). */
export const EXCHANGE_MARKETS_TOPIC = topic("exchange", "00000000-0000-0000-0000-000000000000");

/** One market's topic: book, trades and ticker. */
export function exchangeMarketTopic(marketId: string): string {
  return topic("exchange", marketId);
}

type CandleData = InfiniteData<ExchangeCandlePage, number | undefined>;

function patchStats(qc: QueryClient, stats: ExchangeMarketStats): void {
  qc.setQueryData<ExchangeMarket>(exchangeKeys.market(stats.symbol), (m) => (m ? { ...m, stats } : m));
  qc.setQueryData<{ items: ExchangeMarket[] }>(exchangeKeys.markets(), (d) =>
    d ? { items: d.items.map((m) => (m.symbol === stats.symbol ? { ...m, stats } : m)) } : d,
  );
}

// ───────────────────────────────────────────────────────────── realtime ──

registerRealtimeHandler("exchange.book", (qc, e) => {
  qc.setQueryData(exchangeKeys.book(e.data.symbol), e.data);
});

registerRealtimeHandler("exchange.trades", (qc, e) => {
  qc.setQueryData<{ items: ExchangeTrade[] }>(exchangeKeys.trades(e.data.symbol), (d) =>
    d ? { items: [...e.data.trades, ...d.items.filter((t) => !e.data.trades.some((n) => n.id === t.id))].slice(0, 100) } : d,
  );
});

registerRealtimeHandler("exchange.ticker", (qc, e) => {
  const { stats, candles } = e.data;
  patchStats(qc, stats);
  if (!candles) return;
  for (const q of qc.getQueryCache().findAll({ queryKey: exchangeKeys.candlesOf(stats.symbol) })) {
    const interval = q.queryKey[3] as ExchangeCandleInterval;
    const live = candles[interval];
    if (!live) continue;
    qc.setQueryData<CandleData>(q.queryKey, (d) => {
      const first = d?.pages[0];
      if (!d || !first) return d;
      return { ...d, pages: [mergeExchangeLiveCandle(first, live, interval), ...d.pages.slice(1)] };
    });
  }
});

registerRealtimeHandler("exchange.tickers", (qc, e) => {
  const bySymbol = new Map(e.data.stats.map((s) => [s.symbol, s]));
  qc.setQueryData<{ items: ExchangeMarket[] }>(exchangeKeys.markets(), (d) =>
    d ? { items: d.items.map((m) => ({ ...m, stats: bySymbol.get(m.symbol) ?? m.stats })) } : d,
  );
});

registerRealtimeHandler("exchange.summary", (qc, e) => {
  qc.setQueryData(exchangeKeys.account(), e.data);
});

registerRealtimeHandler("exchange.account", (qc, e) => {
  if (e.data.type === "order" || e.data.type === "fill" || e.data.type === "liquidation") {
    void qc.invalidateQueries({ queryKey: exchangeKeys.history(), refetchType: "active" });
  } else {
    void qc.invalidateQueries({ queryKey: exchangeKeys.history() });
    void qc.invalidateQueries({ queryKey: exchangeKeys.account() });
  }
});

registerReconnectRefetch(exchangeKeys.all());

/** Poll while the socket is down (signed out, or reconnecting). */
function useFallbackPoll(ms = 5_000): number | false {
  return useConnectionState() === "open" ? false : ms;
}

/** Subscribe to one market's book, trades and ticker while mounted. */
export function useExchangeMarketTopic(marketId: string | null | undefined): void {
  useTopic(marketId ? exchangeMarketTopic(marketId) : null);
}

/** Subscribe to every market's stats while mounted (the markets list). */
export function useExchangeMarketsTopic(enabled = true): void {
  useTopic(enabled ? EXCHANGE_MARKETS_TOPIC : null);
}

// ──────────────────────────────────────────────────────────────── reads ──

export function useExchangeConfig(options?: QueryOverrides<ExchangeConfig>): UseQueryResult<ExchangeConfig, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  return useApiQuery(exchangeKeys.config(), ({ signal }) => api.config({ signal }), { staleTime: 5 * 60_000, ...options });
}

/** Every listed market with live stats (kept fresh by `useExchangeMarketsTopic`). */
export function useExchangeMarkets(options?: QueryOverrides<{ items: ExchangeMarket[] }>): UseQueryResult<{ items: ExchangeMarket[] }, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  const poll = useFallbackPoll(REALTIME_FALLBACK_POLL_MS);
  return useApiQuery(exchangeKeys.markets(), ({ signal }) => api.markets({}, { signal }), { refetchInterval: poll, ...options });
}

export function useExchangeMarket(symbol: string | null | undefined, options?: QueryOverrides<ExchangeMarket>): UseQueryResult<ExchangeMarket, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  const poll = useFallbackPoll();
  return useApiQuery(exchangeKeys.market(symbol ?? ""), ({ signal }) => api.market(symbol ?? "", { signal }), { refetchInterval: poll, ...options }, !!symbol);
}

/** Top 20 levels a side, live over the market topic. */
export function useExchangeBook(symbol: string | null | undefined, options?: QueryOverrides<ExchangeBook>): UseQueryResult<ExchangeBook, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  const poll = useFallbackPoll(3_000);
  return useApiQuery(exchangeKeys.book(symbol ?? ""), ({ signal }) => api.book(symbol ?? "", 20, { signal }), { refetchInterval: poll, ...options }, !!symbol);
}

/** Latest trades, newest first, live over the market topic. */
export function useExchangeTrades(symbol: string | null | undefined, options?: QueryOverrides<{ items: ExchangeTrade[] }>): UseQueryResult<{ items: ExchangeTrade[] }, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  const poll = useFallbackPoll();
  return useApiQuery(exchangeKeys.trades(symbol ?? ""), ({ signal }) => api.trades(symbol ?? "", 50, { signal }), { refetchInterval: poll, ...options }, !!symbol);
}

export interface ExchangeCandlesResult {
  /** Oldest first; the last one is the live bucket. */
  candles: ExchangeCandle[];
  isPending: boolean;
  isError: boolean;
  error: ApiError | null;
  refetch: () => void;
  /** Load the page before the oldest loaded candle. */
  fetchOlder: () => void;
  hasOlder: boolean;
  isFetchingOlder: boolean;
}

/** Candles of one interval with history paging; the live bucket updates over the market topic. */
export function useExchangeCandles(symbol: string | null | undefined, interval: ExchangeCandleInterval, pageSize = 300): ExchangeCandlesResult {
  const api = useEndpoints(exchangeEndpoints);
  const poll = useFallbackPoll(10_000);
  const q = useInfiniteQuery<ExchangeCandlePage, ApiError, CandleData, readonly unknown[], number | undefined>({
    queryKey: exchangeKeys.candles(symbol ?? "", interval),
    queryFn: ({ pageParam, signal }) => api.candles(symbol ?? "", { interval, limit: pageSize, before: pageParam }, { signal }),
    initialPageParam: undefined,
    getNextPageParam: (last) => last.next_before ?? undefined,
    enabled: !!symbol,
    refetchInterval: poll,
    staleTime: 30_000,
  });
  const candles = useMemo(() => flattenExchangeCandles(q.data?.pages), [q.data]);
  return {
    candles,
    isPending: q.isPending,
    isError: q.isError,
    error: q.error,
    refetch: () => void q.refetch(),
    fetchOlder: () => {
      if (q.hasNextPage && !q.isFetchingNextPage) void q.fetchNextPage();
    },
    hasOlder: q.hasNextPage,
    isFetchingOlder: q.isFetchingNextPage,
  };
}

/** Every market's recent closes (oldest first), for list sparklines. */
export function useExchangeSparklines(interval: ExchangeCandleInterval = "1h", points = 24): UseQueryResult<Record<string, string[]>, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  return useApiQuery(exchangeKeys.sparklines(interval, points), async ({ signal }) => (await api.sparklines({ interval, points }, { signal })).sparklines, {
    staleTime: 30_000,
    refetchInterval: 60_000,
  });
}

/** The caller's trading account; live over their user topic (`exchange.summary`). */
export function useExchangeAccount(options?: QueryOverrides<ExchangeAccount>): UseQueryResult<ExchangeAccount, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  const poll = useFallbackPoll();
  return useApiQuery(exchangeKeys.account(), ({ signal }) => api.account({ signal }), { refetchInterval: poll, ...options });
}

export function useExchangeOrders(params: ExchangeOrderListParams = {}): PagedQueryResult<ExchangeOrder> {
  const api = useEndpoints(exchangeEndpoints);
  return usePagedQuery<ExchangeOrder>(exchangeKeys.orders(listKeyParams(params)), (cursor, signal) => api.orders({ ...params, cursor }, { signal }));
}

export function useExchangeFills(limit = 25): PagedQueryResult<ExchangeFill> {
  const api = useEndpoints(exchangeEndpoints);
  return usePagedQuery<ExchangeFill>(exchangeKeys.fills(), (cursor, signal) => api.fills({ limit, cursor }, { signal }));
}

export function useExchangeFunding(limit = 25): PagedQueryResult<ExchangeFundingPayment> {
  const api = useEndpoints(exchangeEndpoints);
  return usePagedQuery<ExchangeFundingPayment>(exchangeKeys.funding(), (cursor, signal) => api.funding({ limit, cursor }, { signal }));
}

export function useExchangeLedger(limit = 25): PagedQueryResult<ExchangeLedgerEntry> {
  const api = useEndpoints(exchangeEndpoints);
  return usePagedQuery<ExchangeLedgerEntry>(exchangeKeys.ledger(), (cursor, signal) => api.ledger({ limit, cursor }, { signal }));
}

// ──────────────────────────────────────────────────────────── mutations ──

function afterTrade(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: exchangeKeys.account() });
  void qc.invalidateQueries({ queryKey: exchangeKeys.history() });
}

export function usePlaceExchangeOrder(): UseMutationResult<ExchangeOrderResult, ApiError, ExchangePlaceOrderRequest> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeOrderResult, ApiError, ExchangePlaceOrderRequest>({
    mutationFn: (body) => api.place(body),
    onSettled: () => afterTrade(qc),
  });
}

/** Cancel one open order (removed from the account at once, restored on failure). */
export function useCancelExchangeOrder(): UseMutationResult<{ order: ExchangeOrder }, ApiError, string, { previous: ExchangeAccount | undefined }> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<{ order: ExchangeOrder }, ApiError, string, { previous: ExchangeAccount | undefined }>({
    mutationFn: (id) => api.cancel(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: exchangeKeys.account() });
      const previous = qc.getQueryData<ExchangeAccount>(exchangeKeys.account());
      qc.setQueryData<ExchangeAccount>(exchangeKeys.account(), (a) => (a ? { ...a, open_orders: a.open_orders.filter((o) => o.id !== id) } : a));
      return { previous };
    },
    onError: (_e, _id, ctx) => {
      if (ctx?.previous) qc.setQueryData(exchangeKeys.account(), ctx.previous);
    },
    onSettled: () => afterTrade(qc),
  });
}

export function useCancelAllExchangeOrders(): UseMutationResult<{ cancelled: number }, ApiError, string | undefined> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<{ cancelled: number }, ApiError, string | undefined>({
    mutationFn: (symbol) => api.cancelAll(symbol),
    onSettled: () => afterTrade(qc),
  });
}

export function useCloseExchangePosition(): UseMutationResult<ExchangeOrderResult, ApiError, { symbol: string; sz?: string }> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeOrderResult, ApiError, { symbol: string; sz?: string }>({
    mutationFn: ({ symbol, sz }) => api.close(symbol, sz),
    onSettled: () => afterTrade(qc),
  });
}

/** Claim test funds (when the exchange has a faucet). */
export function useExchangeFaucet(): UseMutationResult<ExchangeAccount, ApiError, void> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeAccount, ApiError, void>({
    mutationFn: () => api.faucet(),
    onSuccess: (account) => {
      qc.setQueryData(exchangeKeys.account(), account);
      void qc.invalidateQueries({ queryKey: exchangeKeys.ledger() });
    },
  });
}

// ──────────────────────────────────────────────────────────────── admin ──

export function useExchangeAdminOverview(): UseQueryResult<ExchangeOverview, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  return useApiQuery(exchangeKeys.admin.overview(), ({ signal }) => api.admin.overview({ signal }), { refetchInterval: 10_000 });
}

export function useExchangeAdminMarkets(): UseQueryResult<{ items: ExchangeAdminMarket[] }, ApiError> {
  const api = useEndpoints(exchangeEndpoints);
  return useApiQuery(exchangeKeys.admin.markets(), ({ signal }) => api.admin.markets({ signal }), { refetchInterval: 10_000 });
}

function afterCatalog(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: exchangeKeys.admin.all() });
  void qc.invalidateQueries({ queryKey: exchangeKeys.markets() });
}

export function useCreateExchangeMarket(): UseMutationResult<ExchangeAdminMarket, ApiError, ExchangeCatalogMarket> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeAdminMarket, ApiError, ExchangeCatalogMarket>({ mutationFn: (body) => api.admin.create(body), onSuccess: () => afterCatalog(qc) });
}

export function useUpdateExchangeMarket(): UseMutationResult<ExchangeAdminMarket, ApiError, ExchangeCatalogMarket> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeAdminMarket, ApiError, ExchangeCatalogMarket>({ mutationFn: (body) => api.admin.update(body.symbol, body), onSuccess: () => afterCatalog(qc) });
}

export function useSetExchangeMarketStatus(): UseMutationResult<ExchangeAdminMarket, ApiError, { symbol: string; status: ExchangeMarketStatus }> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeAdminMarket, ApiError, { symbol: string; status: ExchangeMarketStatus }>({
    mutationFn: ({ symbol, status }) => api.admin.setStatus(symbol, status),
    onSuccess: () => afterCatalog(qc),
  });
}

/** Set ("" clears) a market's manual index price. */
export function useSetExchangeIndexPrice(): UseMutationResult<ExchangeAdminMarket, ApiError, { symbol: string; price: string }> {
  const api = useEndpoints(exchangeEndpoints);
  const qc = useQueryClient();
  return useMutation<ExchangeAdminMarket, ApiError, { symbol: string; price: string }>({
    mutationFn: ({ symbol, price }) => api.admin.setPrice(symbol, price),
    onSuccess: () => afterCatalog(qc),
  });
}
