import type { QueryClient, QueryKey } from "@tanstack/react-query";
import type { RealtimeClient, RealtimeEventName } from "../realtime/client";
import type { RealtimeEventOf } from "../realtime/types";
import type { RealtimeEvent } from "../types";
import { queryKeys } from "./keys";

/** Applies one realtime event to the query cache. */
export type RealtimeHandler<K extends string = string> = (queryClient: QueryClient, event: RealtimeEventOf<K>) => void;

const handlers = new Map<string, Set<RealtimeHandler>>();
const reconnectKeys = new Map<string, QueryKey>();

/**
 * Plug a cache update into every `ApiProvider` without editing core. Call it at module
 * level in the feature file that exports the hooks reading that cache:
 *
 * ```ts
 * registerRealtimeHandler("chat.message", (qc, e) => qc.setQueryData(chatKeys.thread(e.data.conversation_id), …));
 * ```
 *
 * Handlers run for events on any topic the connection is subscribed to. Returns an
 * unregister function.
 */
export function registerRealtimeHandler<K extends RealtimeEventName>(type: K, handler: RealtimeHandler<K>): () => void {
  let set = handlers.get(type);
  if (!set) {
    set = new Set();
    handlers.set(type, set);
  }
  const entry = handler as RealtimeHandler;
  set.add(entry);
  return () => {
    handlers.get(type)?.delete(entry);
  };
}

/**
 * Refetch (active queries under) `key` whenever the socket reconnects: events are
 * at-most-once, so anything realtime keeps fresh must be reloaded after a gap.
 */
export function registerReconnectRefetch(key: QueryKey): () => void {
  const id = JSON.stringify(key);
  reconnectKeys.set(id, key);
  return () => {
    reconnectKeys.delete(id);
  };
}

registerReconnectRefetch(queryKeys.me());

/** Run every registered handler for `event` (exported for tests and custom setups). */
export function dispatchRealtimeEvent(queryClient: QueryClient, event: RealtimeEvent, onError?: (error: unknown) => void): void {
  const set = handlers.get(event.type);
  if (!set) return;
  for (const handler of [...set]) {
    try {
      handler(queryClient, event);
    } catch (error) {
      onError?.(error);
    }
  }
}

/** Wire the registry to a realtime client. Installed by `ApiProvider`; returns an uninstall function. */
export function installRealtimeHandlers(queryClient: QueryClient, realtime: Pick<RealtimeClient, "onAny" | "onReconnect">): () => void {
  const offEvents = realtime.onAny((event) => dispatchRealtimeEvent(queryClient, event));
  const offReconnect = realtime.onReconnect(() => {
    for (const key of reconnectKeys.values()) void queryClient.invalidateQueries({ queryKey: key, refetchType: "active" });
  });
  return () => {
    offEvents();
    offReconnect();
  };
}
