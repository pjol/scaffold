import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  ExchangeAccount,
  ExchangeAdminMarket,
  ExchangeBook,
  ExchangeCandleInterval,
  ExchangeCandlePage,
  ExchangeCatalogMarket,
  ExchangeConfig,
  ExchangeFill,
  ExchangeFundingPayment,
  ExchangeLedgerEntry,
  ExchangeMarket,
  ExchangeMarketStatus,
  ExchangeOrder,
  ExchangeOrderListParams,
  ExchangeOrderResult,
  ExchangeOverview,
  ExchangePlaceOrderRequest,
  ExchangeTrade,
} from "./types";

type HistoryParams = { limit?: number; cursor?: string };

/** Exchange endpoints (`/v1/exchange`, `/v1/admin/exchange`). Market data works signed out. */
export function exchangeEndpoints(http: HttpClient) {
  const X = "/v1/exchange";
  const M = (symbol: string) => `${X}/markets/${seg(symbol)}`;
  return {
    config: (o?: CallOptions) => http.get<ExchangeConfig>(`${X}/config`, { ...o, auth: false }),
    markets: (params: { q?: string; category?: string } = {}, o?: CallOptions) =>
      http.get<{ items: ExchangeMarket[] }>(`${X}/markets`, { ...o, auth: false, query: { q: params.q, category: params.category } }),
    market: (symbol: string, o?: CallOptions) => http.get<ExchangeMarket>(M(symbol), { ...o, auth: false }),
    book: (symbol: string, depth = 20, o?: CallOptions) => http.get<ExchangeBook>(`${M(symbol)}/book`, { ...o, auth: false, query: { depth } }),
    trades: (symbol: string, limit = 50, o?: CallOptions) =>
      http.get<{ items: ExchangeTrade[] }>(`${M(symbol)}/trades`, { ...o, auth: false, query: { limit } }),
    /** Oldest first; `before` (unix ms) pages back from `next_before`. */
    candles: (symbol: string, params: { interval: ExchangeCandleInterval; limit?: number; before?: number }, o?: CallOptions) =>
      http.get<ExchangeCandlePage>(`${M(symbol)}/candles`, {
        ...o,
        auth: false,
        query: { interval: params.interval, limit: params.limit, before: params.before },
      }),
    /** Every market's recent closes, oldest first (cached 30 s). */
    sparklines: (params: { interval?: ExchangeCandleInterval; points?: number } = {}, o?: CallOptions) =>
      http.get<{ sparklines: Record<string, string[]> }>(`${X}/sparklines`, {
        ...o,
        auth: false,
        query: { interval: params.interval, points: params.points },
      }),

    account: (o?: CallOptions) => http.get<ExchangeAccount>(`${X}/account`, o),
    orders: (params: ExchangeOrderListParams = {}, o?: CallOptions) =>
      http.get<Page<ExchangeOrder>>(`${X}/orders`, { ...o, query: { status: params.status, limit: params.limit, cursor: params.cursor } }),
    fills: (params: HistoryParams = {}, o?: CallOptions) =>
      http.get<Page<ExchangeFill>>(`${X}/fills`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    funding: (params: HistoryParams = {}, o?: CallOptions) =>
      http.get<Page<ExchangeFundingPayment>>(`${X}/funding`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    ledger: (params: HistoryParams = {}, o?: CallOptions) =>
      http.get<Page<ExchangeLedgerEntry>>(`${X}/ledger`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),

    place: (body: ExchangePlaceOrderRequest, o?: CallOptions) => http.post<ExchangeOrderResult>(`${X}/orders`, body, o),
    cancel: (orderId: string, o?: CallOptions) => http.delete<{ order: ExchangeOrder }>(`${X}/orders/${seg(orderId)}`, o),
    /** Cancel every open order (in one market when `symbol` is given). */
    cancelAll: (symbol?: string, o?: CallOptions) => http.delete<{ cancelled: number }>(`${X}/orders`, { ...o, query: { symbol } }),
    /** Market-close a position (all of it, or `sz` base units). */
    close: (symbol: string, sz?: string, o?: CallOptions) =>
      http.post<ExchangeOrderResult>(`${X}/positions/${seg(symbol)}/close`, sz ? { sz } : {}, o),
    faucet: (o?: CallOptions) => http.post<ExchangeAccount>(`${X}/faucet`, undefined, o),

    admin: {
      overview: (o?: CallOptions) => http.get<ExchangeOverview>("/v1/admin/exchange/overview", o),
      markets: (o?: CallOptions) => http.get<{ items: ExchangeAdminMarket[] }>("/v1/admin/exchange/markets", o),
      create: (body: ExchangeCatalogMarket, o?: CallOptions) => http.post<ExchangeAdminMarket>("/v1/admin/exchange/markets", body, o),
      update: (symbol: string, body: ExchangeCatalogMarket, o?: CallOptions) =>
        http.put<ExchangeAdminMarket>(`/v1/admin/exchange/markets/${seg(symbol)}`, body, o),
      setStatus: (symbol: string, status: ExchangeMarketStatus, o?: CallOptions) =>
        http.put<ExchangeAdminMarket>(`/v1/admin/exchange/markets/${seg(symbol)}/status`, { status }, o),
      /** Set a manual index price, or clear it with "" (back to the price feed). */
      setPrice: (symbol: string, price: string, o?: CallOptions) =>
        http.put<ExchangeAdminMarket>(`/v1/admin/exchange/markets/${seg(symbol)}/price`, { price }, o),
    },
  };
}

export type ExchangeEndpoints = ReturnType<typeof exchangeEndpoints>;
