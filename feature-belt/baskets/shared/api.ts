import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
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
  BasketsSummary,
} from "./types";

/** Baskets endpoints (`/v1/baskets`, `/v1/admin/baskets`). Everything needs sign-in. */
export function basketsEndpoints(http: HttpClient) {
  const B = "/v1/baskets";
  const A = "/v1/admin/baskets";
  return {
    /** Resolve, price and check a list without writing. */
    preview: (input: BasketsInput, o?: CallOptions) => http.post<BasketsPreview>(`${B}/preview`, input, o),
    /** Markets a basket may hold, for a picker (`q` may be empty). */
    search: (q: string, limit = 20, o?: CallOptions) => http.get<{ items: BasketsCandidate[] }>(`${B}/search`, { ...o, query: { q, limit } }),
    list: (params: BasketsListParams = {}, o?: CallOptions) =>
      http.get<Page<BasketsSummary>>(B, { ...o, query: { sort: params.sort, status: params.status, q: params.q, limit: params.limit, cursor: params.cursor } }),
    /** Baskets the caller registered (created or joined), newest first. */
    mine: (params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<BasketsSummary>>(`${B}/mine`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    get: (symbol: string, o?: CallOptions) => http.get<BasketsDetail>(`${B}/${seg(symbol)}`, o),
    /** Create, or join the basket with the same lines (reopening it if archived). */
    create: (body: BasketsCreateRequest, o?: CallOptions) => http.post<BasketsCreateResult>(B, body, o),
    report: (symbol: string, body: BasketsReportRequest, o?: CallOptions) => http.post<void>(`${B}/${seg(symbol)}/report`, body, o),

    admin: {
      list: (params: BasketsAdminListParams = {}, o?: CallOptions) =>
        http.get<Page<BasketsAdminBasket>>(A, {
          ...o,
          query: { filter: params.filter, status: params.status, q: params.q, limit: params.limit, cursor: params.cursor },
        }),
      rename: (symbol: string, body: { name?: string; description?: string }, o?: CallOptions) =>
        http.patch<BasketsAdminBasket>(`${A}/${seg(symbol)}`, body, o),
      /** Hiding takes it out of discovery and winds it down; it never reopens. */
      setHidden: (symbol: string, hidden: boolean, o?: CallOptions) => http.post<BasketsAdminBasket>(`${A}/${seg(symbol)}/hide`, { hidden }, o),
      /** Reduce-only while people hold positions, then archived (admins). */
      archive: (symbol: string, o?: CallOptions) => http.post<BasketsAdminBasket>(`${A}/${seg(symbol)}/archive`, undefined, o),
      resolveReports: (symbol: string, o?: CallOptions) => http.post<BasketsAdminBasket>(`${A}/${seg(symbol)}/reports/resolve`, undefined, o),
    },
  };
}

export type BasketsEndpoints = ReturnType<typeof basketsEndpoints>;
