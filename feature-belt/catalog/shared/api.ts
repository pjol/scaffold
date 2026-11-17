import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  CatalogAdminItemListParams,
  CatalogAdminStatus,
  CatalogIndex,
  CatalogItem,
  CatalogItemListParams,
  CatalogResolution,
  CatalogSearchParams,
  CatalogSummary,
  CatalogSyncRun,
} from "./types";

/** Catalog endpoints: reads (public or signed-in, per `CATALOG_PUBLIC_READS`), resolve, and staff sync and items. */
export function catalogEndpoints(http: HttpClient) {
  const C = "/v1/catalog";
  const A = "/v1/admin/catalog";
  return {
    /** `GET /v1/catalog`: counts, categories, and whether the client index is served. 503 `catalog_loading` right after a boot. */
    summary: (o?: CallOptions) => http.get<CatalogSummary>(C, o),
    /** `GET /v1/catalog/search`: ranked, at most 50; `q` needs 2 characters (400 `query_too_short`). */
    search: (p: CatalogSearchParams, o?: CallOptions) =>
      http.get<{ items: CatalogItem[] }>(`${C}/search`, { ...o, query: { q: p.q, category: p.category, retired: p.retired, limit: p.limit } }),
    /** `GET /v1/catalog/items`: by name, keyset pages. */
    items: (p: CatalogItemListParams = {}, o?: CallOptions) =>
      http.get<Page<CatalogItem>>(`${C}/items`, { ...o, query: { category: p.category, retired: p.retired, limit: p.limit, cursor: p.cursor } }),
    /** `GET /v1/catalog/items/{key}`: retired items too. */
    item: (key: string, o?: CallOptions) => http.get<CatalogItem>(`${C}/items/${seg(key)}`, o),
    /** `POST /v1/catalog/resolve`: 1 to 500 names, in order. Rate limited. */
    resolve: (names: string[], o?: CallOptions) => http.post<{ results: CatalogResolution[] }>(`${C}/resolve`, { names }, o),
    /** `GET /v1/catalog/index`: every item, compact, for search on the device (ETag; 404 `client_index_off` for large catalogs). */
    index: (o?: CallOptions) => http.get<CatalogIndex>(`${C}/index`, o),
    admin: {
      status: (o?: CallOptions) => http.get<CatalogAdminStatus>(`${A}/status`, o),
      /** Sync runs, newest first. */
      runs: (p: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
        http.get<Page<CatalogSyncRun>>(`${A}/runs`, { ...o, query: { limit: p.limit, cursor: p.cursor } }),
      /** Admin: start a sync (202). `force` skips the safety valve. 409 `sync_running` / `no_source`. Audited. */
      sync: (force = false, o?: CallOptions) => http.post<CatalogSyncRun>(`${A}/sync`, { force }, o),
      /** Stored items by name; `q` matches the name, key or an alias. */
      items: (p: CatalogAdminItemListParams = {}, o?: CallOptions) =>
        http.get<Page<CatalogItem>>(`${A}/items`, { ...o, query: { q: p.q, status: p.status, limit: p.limit, cursor: p.cursor } }),
      item: (key: string, o?: CallOptions) => http.get<CatalogItem>(`${A}/items/${seg(key)}`, o),
      /** Admin: retire by hand (holds until restored, whatever the source says). Audited. */
      retire: (key: string, o?: CallOptions) => http.post<CatalogItem>(`${A}/items/${seg(key)}/retire`, {}, o),
      /** Admin: make active again (the next sync retires it again if the source no longer lists it). Audited. */
      restore: (key: string, o?: CallOptions) => http.post<CatalogItem>(`${A}/items/${seg(key)}/restore`, {}, o),
    },
  };
}

export type CatalogEndpoints = ReturnType<typeof catalogEndpoints>;
