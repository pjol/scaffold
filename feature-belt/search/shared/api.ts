import type { CallOptions, HttpClient } from "../api/http";
import type { SearchParams, SearchResponse } from "./types";

/** Global search across every installed feature, as the caller. */
export const searchEndpoints = (http: HttpClient) => ({
  search: (params: SearchParams, o?: CallOptions) =>
    http.get<SearchResponse>("/v1/search", {
      ...o,
      query: { q: params.q, kinds: params.kinds?.length ? params.kinds.join(",") : undefined, limit: params.limit, org_id: params.orgId || undefined },
    }),
});

export type SearchEndpoints = ReturnType<typeof searchEndpoints>;
