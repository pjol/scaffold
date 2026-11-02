"use client";

import { keepPreviousData, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery } from "../hooks/internal";
import { searchEndpoints } from "./api";
import { SEARCH_MIN_QUERY_CHARS, type SearchParams, type SearchResponse } from "./types";

export const searchKeys = {
  all: () => ["search"] as const,
  query: (q: string, kinds: readonly string[] = [], limit?: number, orgId?: string, source?: string) =>
    ["search", q, [...kinds].sort().join(","), limit ?? null, orgId ?? null, source ?? null] as const,
};

/**
 * Global search. Runs only for queries of at least `SEARCH_MIN_QUERY_CHARS` (debounce the
 * input yourself, e.g. `useDebouncedValue`); keeps the previous results while the next query
 * loads, and never retries (a partial answer is already reported in `data.partial`).
 */
export function useSearch(params: SearchParams, options: { enabled?: boolean } = {}): UseQueryResult<SearchResponse, ApiError> {
  const ep = useEndpoints(searchEndpoints);
  const q = params.q.trim().replace(/\s+/g, " ");
  const enabled = (options.enabled ?? true) && q.length >= SEARCH_MIN_QUERY_CHARS;
  return useApiQuery<SearchResponse>(
    searchKeys.query(q, params.kinds, params.limit, params.orgId || undefined, params.source || undefined),
    ({ signal }) => ep.search({ ...params, q }, { signal }),
    { placeholderData: keepPreviousData, staleTime: 30_000, retry: false },
    enabled,
  );
}
