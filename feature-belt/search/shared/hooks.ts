"use client";

import { keepPreviousData, useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, type QueryOverrides } from "../hooks/internal";
import { searchEndpoints } from "./api";
import { searchDeviceZone, searchQueryReady } from "./operators";
import { type SearchParams, type SearchResponse, type SearchSaveRequest, type SearchSaved } from "./types";

export const searchKeys = {
  all: () => ["search"] as const,
  query: (q: string, kinds: readonly string[] = [], limit?: number, orgId?: string, source?: string) =>
    ["search", q, [...kinds].sort().join(","), limit ?? null, orgId ?? null, source ?? null] as const,
  saved: () => ["search-saved"] as const,
};

/**
 * Global search. Runs for queries with at least `SEARCH_MIN_QUERY_CHARS` of words or an
 * operator (`searchQueryReady`; debounce the input yourself, e.g. `useDebouncedValue`); dates
 * in operators are read in this device's zone. Keeps the previous results while the next
 * query loads, and never retries (a partial answer is already reported in `data.partial`).
 */
export function useSearch(params: SearchParams, options: { enabled?: boolean } = {}): UseQueryResult<SearchResponse, ApiError> {
  const ep = useEndpoints(searchEndpoints);
  const q = params.q.trim().replace(/\s+/g, " ");
  const enabled = (options.enabled ?? true) && searchQueryReady(q);
  return useApiQuery<SearchResponse>(
    searchKeys.query(q, params.kinds, params.limit, params.orgId || undefined, params.source || undefined),
    ({ signal }) => ep.search({ tz: searchDeviceZone(), ...params, q }, { signal }),
    { placeholderData: keepPreviousData, staleTime: 30_000, retry: false },
    enabled,
  );
}

/** Your saved searches, newest first. */
export function useSavedSearches(options?: QueryOverrides<{ items: SearchSaved[] }>): UseQueryResult<{ items: SearchSaved[] }, ApiError> {
  const ep = useEndpoints(searchEndpoints);
  return useApiQuery(searchKeys.saved(), ({ signal }) => ep.saved({ signal }), { staleTime: 5 * 60_000, ...options });
}

/** Save a search (name, query with its operators, optional kinds/source). */
export function useSaveSearch(): UseMutationResult<SearchSaved, ApiError, SearchSaveRequest> {
  const ep = useEndpoints(searchEndpoints);
  const qc = useQueryClient();
  return useMutation<SearchSaved, ApiError, SearchSaveRequest>({
    mutationFn: (body) => ep.save(body),
    onSuccess: (saved) => qc.setQueryData<{ items: SearchSaved[] }>(searchKeys.saved(), (old) => ({ items: [saved, ...(old?.items ?? [])] })),
  });
}

/** Remove a saved search (optimistic). */
export function useRemoveSavedSearch(): UseMutationResult<void, ApiError, string, { items: SearchSaved[] } | undefined> {
  const ep = useEndpoints(searchEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string, { items: SearchSaved[] } | undefined>({
    mutationFn: (id) => ep.removeSaved(id),
    onMutate: (id) => {
      const previous = qc.getQueryData<{ items: SearchSaved[] }>(searchKeys.saved());
      if (previous) qc.setQueryData(searchKeys.saved(), { items: previous.items.filter((x) => x.id !== id) });
      return previous;
    },
    onError: (_e, _id, previous) => {
      if (previous) qc.setQueryData(searchKeys.saved(), previous);
    },
  });
}
