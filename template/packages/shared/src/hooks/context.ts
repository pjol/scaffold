"use client";

import { QueryClientProvider, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { createContext, createElement, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { endpointsFor, type ApiClient, type EndpointFactory } from "../api/client";
import type { RealtimeClient } from "../realtime/client";
import { installConsentRecovery } from "./consent-recovery";
import { createQueryClient } from "./query-client";
import { installRealtimeHandlers } from "./realtime-handlers";

interface ApiContextValue {
  api: ApiClient;
  realtime: RealtimeClient | null;
}

const ApiContext = createContext<ApiContextValue | null>(null);

export interface ApiProviderProps {
  /** From `createApiClient`; create once (e.g. `useState(() => createApiClient(…))`). */
  api: ApiClient;
  /**
   * From `createRealtimeClient`. Pass `null` while signed out: the provider connects when
   * given a client and closes it when it's removed or unmounted.
   */
  realtime?: RealtimeClient | null;
  /** Your QueryClient; one is created with `createQueryClient()` if omitted. */
  queryClient?: QueryClient;
  /** Call `realtime.connect()` on mount (default true). */
  autoConnect?: boolean;
  children?: ReactNode;
}

// Deferred closes survive React StrictMode's mount → unmount → mount cycle without
// tearing down and re-opening the socket.
const pendingClose = new WeakMap<RealtimeClient, ReturnType<typeof setTimeout>>();

/**
 * Provides the API client, realtime client and QueryClient to every hook; routes realtime
 * events to the handlers features registered (`registerRealtimeHandler`) and refetches
 * their keys after a reconnect; refetches `me` whenever the API answers
 * `consent_required`, so `useConsentGate()` shows the consent screen.
 */
export function ApiProvider({ api, realtime = null, queryClient, autoConnect = true, children }: ApiProviderProps) {
  const [fallbackClient] = useState<QueryClient>(() => queryClient ?? createQueryClient());
  const client = queryClient ?? fallbackClient;
  const value = useMemo<ApiContextValue>(() => ({ api, realtime }), [api, realtime]);

  useEffect(() => installConsentRecovery(client), [client]);

  useEffect(() => {
    if (!realtime) return undefined;
    return installRealtimeHandlers(client, realtime);
  }, [client, realtime]);

  useEffect(() => {
    if (!realtime || !autoConnect) return undefined;
    const pending = pendingClose.get(realtime);
    if (pending !== undefined) {
      clearTimeout(pending);
      pendingClose.delete(realtime);
    }
    realtime.connect();
    return () => {
      pendingClose.set(
        realtime,
        setTimeout(() => {
          pendingClose.delete(realtime);
          realtime.close();
        }, 0),
      );
    };
  }, [realtime, autoConnect]);

  return createElement(QueryClientProvider, { client }, createElement(ApiContext.Provider, { value }, children));
}

function useApiContext(): ApiContextValue {
  const ctx = useContext(ApiContext);
  if (!ctx) throw new Error("@scaffoldapp/shared hooks must be used inside <ApiProvider>.");
  return ctx;
}

/** The typed API client from the nearest `ApiProvider`. */
export function useApi(): ApiClient {
  return useApiContext().api;
}

/**
 * A feature's endpoint group, built once per API client:
 * `const chat = useEndpoints(chatEndpoints); chat.send(…)`. Pass a module-level factory.
 */
export function useEndpoints<T>(factory: EndpointFactory<T>): T {
  return endpointsFor(useApiContext().api.http, factory);
}

/** The realtime client, or null when none was provided (signed out, SSR). */
export function useRealtime(): RealtimeClient | null {
  return useApiContext().realtime;
}

/** Re-exported so apps don't need a second import for the client. */
export { useQueryClient };
