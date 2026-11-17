import type { CallOptions, HttpClient } from "../api/http";
import type { SearchParams, SearchResponse, SearchSaveRequest, SearchSaved } from "./types";

/** Global search across every installed feature, as the caller. */
export const searchEndpoints = (http: HttpClient) => ({
  search: (params: SearchParams, o?: CallOptions) =>
    http.get<SearchResponse>("/v1/search", {
      ...o,
      query: {
        q: params.q,
        kinds: params.kinds?.length ? params.kinds.join(",") : undefined,
        limit: params.limit,
        org_id: params.orgId || undefined,
        source: params.source || undefined,
        tz: params.tz || undefined,
      },
    }),
  /** Your saved searches, newest first. */
  saved: (o?: CallOptions) => http.get<{ items: SearchSaved[] }>("/v1/search/saved", o),
  /** Save a search (at most 50). */
  save: (body: SearchSaveRequest, o?: CallOptions) => http.post<SearchSaved>("/v1/search/saved", body, o),
  removeSaved: (id: string, o?: CallOptions) => http.delete<void>(`/v1/search/saved/${encodeURIComponent(id)}`, o),
});

export type SearchEndpoints = ReturnType<typeof searchEndpoints>;
