"use client";

import { useMutation, useQuery, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useMemo } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { catalogEndpoints } from "./api";
import { buildCatalogClientIndex, catalogNameKey, searchCatalogClientIndex, type CatalogClientIndex } from "./search";
import type {
  CatalogAdminItemListParams,
  CatalogAdminStatus,
  CatalogIndex,
  CatalogItem,
  CatalogItemListParams,
  CatalogListItem,
  CatalogResolution,
  CatalogSearchParams,
  CatalogSummary,
  CatalogSyncRun,
} from "./types";

/** Query keys. `catalogKeys.all()` invalidates everything this feature caches. */
export const catalogKeys = {
  all: () => ["catalog"] as const,
  summary: () => ["catalog", "summary"] as const,
  searches: () => ["catalog", "search"] as const,
  search: (params: Record<string, unknown>) => ["catalog", "search", params] as const,
  lists: () => ["catalog", "items"] as const,
  list: (params: Record<string, unknown> = {}) => ["catalog", "items", params] as const,
  item: (key: string) => ["catalog", "item", key] as const,
  index: () => ["catalog", "index"] as const,
  admin: {
    all: () => ["catalog", "admin"] as const,
    status: () => ["catalog", "admin", "status"] as const,
    runs: () => ["catalog", "admin", "runs"] as const,
    lists: () => ["catalog", "admin", "items"] as const,
    list: (params: Record<string, unknown> = {}) => ["catalog", "admin", "items", params] as const,
  },
};

const MINUTE = 60_000;

/** True when `q` is long enough to search (2 characters once folded). */
export function catalogQueryReady(q: string): boolean {
  return [...catalogNameKey(q)].length >= 2;
}

/** Counts, categories (largest first) and whether the client index is served. */
export function useCatalogSummary(options?: QueryOverrides<CatalogSummary>): UseQueryResult<CatalogSummary, ApiError> {
  const c = useEndpoints(catalogEndpoints);
  return useApiQuery(catalogKeys.summary(), ({ signal }) => c.summary({ signal }), { staleTime: 5 * MINUTE, ...options });
}

/** Server search; pass null (or a query under 2 characters) to stay idle. Keeps the last results while the next load. */
export function useCatalogSearch(params: CatalogSearchParams | null, options?: QueryOverrides<{ items: CatalogItem[] }>): UseQueryResult<{ items: CatalogItem[] }, ApiError> {
  const c = useEndpoints(catalogEndpoints);
  const ready = !!params && catalogQueryReady(params.q);
  return useApiQuery(
    catalogKeys.search(listKeyParams({ ...params, q: params ? catalogNameKey(params.q) : "" })),
    ({ signal }) => c.search(params as CatalogSearchParams, { signal }),
    { staleTime: MINUTE, placeholderData: (prev) => prev, ...options },
    ready,
  );
}

/** Items by name (`data.items`), a category or all; keyset pages. */
export function useCatalogItems(params: CatalogItemListParams = {}, options?: InfiniteOverrides<CatalogItem>): PagedQueryResult<CatalogItem> {
  const c = useEndpoints(catalogEndpoints);
  return usePagedQuery<CatalogItem>(catalogKeys.list(listKeyParams(params)), (cursor, signal) => c.items({ ...params, cursor }, { signal }), {
    staleTime: MINUTE,
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** One item by key (retired ones too). Pass null to disable. */
export function useCatalogItem(key: string | null | undefined, options?: QueryOverrides<CatalogItem>): UseQueryResult<CatalogItem, ApiError> {
  const c = useEndpoints(catalogEndpoints);
  return useApiQuery(catalogKeys.item(key ?? ""), ({ signal }) => c.item(key ?? "", { signal }), { staleTime: MINUTE, ...options }, !!key);
}

/** Resolve up to 500 names (a pasted list) to items, in order. */
export function useResolveCatalogNames(): UseMutationResult<{ results: CatalogResolution[] }, ApiError, string[]> {
  const c = useEndpoints(catalogEndpoints);
  return useMutation<{ results: CatalogResolution[] }, ApiError, string[]>({ mutationFn: (names) => c.resolve(names) });
}

const selectIndex = (ix: CatalogIndex): CatalogClientIndex => buildCatalogClientIndex(ix);

/** The client index, built for searching (only when the summary says it is served). */
export function useCatalogClientIndex(enabled: boolean): UseQueryResult<CatalogClientIndex, ApiError> {
  const c = useEndpoints(catalogEndpoints);
  return useQuery<CatalogIndex, ApiError, CatalogClientIndex>({
    queryKey: catalogKeys.index(),
    queryFn: ({ signal }) => c.index({ signal }),
    select: selectIndex,
    staleTime: 10 * MINUTE,
    gcTime: 30 * MINUTE,
    enabled,
  });
}

export interface CatalogFindResult {
  /** Ranked hits; undefined until the first answer. */
  items: CatalogListItem[] | undefined;
  /** Searching on the device (the client index), not on the server. */
  local: boolean;
  isError: boolean;
  error: ApiError | null;
  isFetching: boolean;
  retry: () => void;
}

/**
 * Search as you type: on the device over the client index when the catalog serves one (no
 * request per keystroke), else on the server (debounce `q` first). Idle under 2 characters.
 */
export function useCatalogFind(params: CatalogSearchParams): CatalogFindResult {
  const summary = useCatalogSummary();
  const wantsLocal = summary.data?.client_index === true;
  const index = useCatalogClientIndex(wantsLocal);
  const local = wantsLocal && !index.isError;
  const ready = catalogQueryReady(params.q);
  const server = useCatalogSearch(ready && !local && !summary.isPending ? params : null);
  const { q, category, retired, limit } = params;
  const localItems = useMemo(
    () => (local && ready && index.data ? searchCatalogClientIndex(index.data, q, { category, retired, limit }) : undefined),
    [local, ready, index.data, q, category, retired, limit],
  );
  if (local) {
    return {
      items: localItems,
      local,
      isError: false,
      error: null,
      isFetching: index.isFetching,
      retry: () => void index.refetch(),
    };
  }
  const failed = summary.isError ? summary.error : server.error;
  return {
    items: server.data?.items,
    local,
    isError: summary.isError || server.isError,
    error: failed ?? null,
    isFetching: summary.isFetching || server.isFetching,
    retry: () => void (summary.isError ? summary.refetch() : server.refetch()),
  };
}

// ---- staff ----

/** Source, schedule, loaded copy and the running and last sync; every 3 s while one runs, else every 30 s. */
export function useCatalogAdminStatus(options?: QueryOverrides<CatalogAdminStatus>): UseQueryResult<CatalogAdminStatus, ApiError> {
  const c = useEndpoints(catalogEndpoints);
  return useApiQuery(catalogKeys.admin.status(), ({ signal }) => c.admin.status({ signal }), {
    refetchInterval: (q) => (q.state.data?.running ? 3_000 : 30_000),
    ...options,
  });
}

/** Sync runs, newest first. */
export function useCatalogSyncRuns(options?: InfiniteOverrides<CatalogSyncRun>): PagedQueryResult<CatalogSyncRun> {
  const c = useEndpoints(catalogEndpoints);
  return usePagedQuery<CatalogSyncRun>(catalogKeys.admin.runs(), (cursor, signal) => c.admin.runs({ cursor, limit: 20 }, { signal }), options);
}

/** Stored items for staff (`data.items`): q matches the name, key or an alias. */
export function useCatalogAdminItems(params: CatalogAdminItemListParams = {}, options?: InfiniteOverrides<CatalogItem>): PagedQueryResult<CatalogItem> {
  const c = useEndpoints(catalogEndpoints);
  return usePagedQuery<CatalogItem>(catalogKeys.admin.list(listKeyParams(params)), (cursor, signal) => c.admin.items({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** Start a sync now (admin). `force` skips the safety valve. */
export function useRunCatalogSync(): UseMutationResult<CatalogSyncRun, ApiError, { force?: boolean } | void> {
  const c = useEndpoints(catalogEndpoints);
  const qc = useQueryClient();
  return useMutation<CatalogSyncRun, ApiError, { force?: boolean } | void>({
    mutationFn: (v) => c.admin.sync(!!(v && v.force)),
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: catalogKeys.admin.status() }),
        qc.invalidateQueries({ queryKey: catalogKeys.admin.runs() }),
        qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
      ]),
  });
}

function useCatalogItemMutation(fn: (c: ReturnType<typeof catalogEndpoints>, key: string) => Promise<CatalogItem>): UseMutationResult<CatalogItem, ApiError, string> {
  const c = useEndpoints(catalogEndpoints);
  const qc = useQueryClient();
  return useMutation<CatalogItem, ApiError, string>({
    mutationFn: (key) => fn(c, key),
    onSuccess: (item) => qc.setQueryData(catalogKeys.item(item.key), item),
    onSettled: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: catalogKeys.admin.all() }),
        qc.invalidateQueries({ queryKey: catalogKeys.lists() }),
        qc.invalidateQueries({ queryKey: catalogKeys.searches() }),
        qc.invalidateQueries({ queryKey: catalogKeys.summary() }),
        qc.invalidateQueries({ queryKey: catalogKeys.index() }),
        qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
      ]),
  });
}

/** Retire an item by hand (admin): it stays retired until restored. */
export function useRetireCatalogItem(): UseMutationResult<CatalogItem, ApiError, string> {
  return useCatalogItemMutation((c, key) => c.admin.retire(key));
}

/** Make a retired item active again (admin). */
export function useRestoreCatalogItem(): UseMutationResult<CatalogItem, ApiError, string> {
  return useCatalogItemMutation((c, key) => c.admin.restore(key));
}
