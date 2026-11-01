"use client";

import {
  useInfiniteQuery,
  useQuery,
  type InfiniteData,
  type QueryKey,
  type UseInfiniteQueryOptions,
  type UseInfiniteQueryResult,
  type UseQueryOptions,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import type { ApiError } from "../api/errors";
import { flattenPages, nextPageCursor } from "../api/params";
import type { RealtimeEventName } from "../realtime/client";
import type { ConnectionState, RealtimeEventOf } from "../realtime/types";
import type { Page } from "../types";
import { useRealtime } from "./context";

/** Options you can pass to any query hook (everything except key and fn). */
export type QueryOverrides<T> = Omit<UseQueryOptions<T, ApiError, T, QueryKey>, "queryKey" | "queryFn" | "enabled"> & {
  enabled?: boolean;
};

/** Infinite list data with a flattened `items` array. */
export interface PagedData<T> extends InfiniteData<Page<T>, string | undefined> {
  items: T[];
}

/** Options for infinite list hooks. */
export type InfiniteOverrides<T> = Omit<
  UseInfiniteQueryOptions<Page<T>, ApiError, PagedData<T>, QueryKey, string | undefined>,
  "queryKey" | "queryFn" | "initialPageParam" | "getNextPageParam" | "select" | "enabled"
> & { enabled?: boolean };

/** Result of an infinite list hook: `data.items` is every loaded row. */
export type PagedQueryResult<T> = UseInfiniteQueryResult<PagedData<T>, ApiError>;

function selectPaged<T>(data: InfiniteData<Page<T>, string | undefined>): PagedData<T> {
  return { ...data, items: flattenPages(data.pages) };
}

/** `useQuery` typed with `ApiError`. Feature hooks build on this. */
export function useApiQuery<T>(
  queryKey: QueryKey,
  queryFn: (ctx: { signal: AbortSignal }) => Promise<T>,
  overrides?: QueryOverrides<T>,
  enabled = true,
): UseQueryResult<T, ApiError> {
  return useQuery<T, ApiError, T, QueryKey>({
    ...overrides,
    queryKey,
    queryFn: ({ signal }) => queryFn({ signal }),
    enabled: enabled && (overrides?.enabled ?? true),
  });
}

/** Cursor-paginated list (`Page<T>`) as an infinite query; `data.items` flattens every page. */
export function usePagedQuery<T>(
  queryKey: QueryKey,
  fetchPage: (cursor: string | undefined, signal: AbortSignal) => Promise<Page<T>>,
  overrides?: InfiniteOverrides<T>,
  enabled = true,
): PagedQueryResult<T> {
  return useInfiniteQuery<Page<T>, ApiError, PagedData<T>, QueryKey, string | undefined>({
    ...overrides,
    queryKey,
    queryFn: ({ pageParam, signal }) => fetchPage(pageParam, signal),
    initialPageParam: undefined,
    getNextPageParam: nextPageCursor,
    select: selectPaged,
    enabled: enabled && (overrides?.enabled ?? true),
  });
}

/** `value`, updated only after it stops changing for `delayMs` (search boxes). */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    if (delayMs <= 0) {
      setDebounced(value);
      return undefined;
    }
    const t = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(t);
  }, [value, delayMs]);
  return delayMs <= 0 ? value : debounced;
}

/** Subscribe to a realtime topic while mounted (ref-counted; no-op without realtime). */
export function useTopic(topic: string | null | undefined): void {
  const realtime = useRealtime();
  useEffect(() => {
    if (!realtime || !topic) return undefined;
    return realtime.subscribe(topic);
  }, [realtime, topic]);
}

/**
 * Handle one realtime event type while mounted (component-local reactions such as a
 * toast). Cache updates belong in `registerRealtimeHandler`. The latest `handler` is
 * always used; it need not be memoized.
 */
export function useRealtimeEvent<K extends RealtimeEventName>(type: K, handler: (event: RealtimeEventOf<K>) => void, options: { topic?: string } = {}): void {
  const realtime = useRealtime();
  const latest = useRef(handler);
  latest.current = handler;
  const { topic } = options;
  useEffect(() => {
    if (!realtime) return undefined;
    return realtime.on(type, (event) => latest.current(event), topic === undefined ? undefined : { topic });
  }, [realtime, type, topic]);
}

const noopSubscribe = () => () => {};
const closedSnapshot = (): ConnectionState => "closed";

/** Live realtime connection state (`closed` when there is no realtime client). */
export function useConnectionState(): ConnectionState {
  const realtime = useRealtime();
  return useSyncExternalStore(realtime ? realtime.onStateChange : noopSubscribe, realtime ? realtime.getState : closedSnapshot, closedSnapshot);
}

/** `Date.now()`, ticking every `intervalMs` while `active` (countdowns, "2 min ago"). */
export function useNow(intervalMs: number, active = true): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return undefined;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs, active]);
  return now;
}

/** Polling interval for realtime-backed views while the socket is down. */
export const REALTIME_FALLBACK_POLL_MS = 15_000;
