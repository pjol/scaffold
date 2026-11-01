import type { TokenGetter } from "../api/http";
import type { RealtimeClientMessage, RealtimeEvent } from "../types";
import { computeBackoff, type BackoffOptions } from "../utils/backoff";
import { isServerManagedTopic } from "./topics";
import type { ConnectionState, RealtimeEventOf, RealtimeEventType, WebSocketConstructor, WebSocketLike } from "./types";

/** A registered event type, or any other string (its payload is `unknown`). */
export type RealtimeEventName = RealtimeEventType | (string & {});

export interface RealtimeClientOptions {
  /** WebSocket endpoint, e.g. `wss://api.example.com/v1/ws` (see `toWebSocketUrl`). */
  url: string;
  /**
   * Fresh session token. Called on **every** (re)connect — Clerk tokens are
   * short-lived. `forceRefresh` is set after an attempt failed before opening.
   * Returning null (signed out) keeps the client closed and retries with backoff.
   */
  getToken: TokenGetter;
  /** WebSocket constructor; defaults to the global `WebSocket` (browser / RN). */
  WebSocketImpl?: WebSocketConstructor;
  /** Ping cadence. Default 25 000 ms. */
  heartbeatIntervalMs?: number;
  /** A ping with no inbound traffic for this long marks the connection dead. Default 10 000 ms. */
  heartbeatTimeoutMs?: number;
  /** Reconnect backoff. Default 1 s → ×2 → capped at 30 s, equal jitter. */
  backoff?: Partial<BackoffOptions>;
  /** Randomness for jitter (tests). */
  random?: () => number;
  /** Clock (tests). */
  now?: () => number;
  /** Non-fatal problems (bad frames, handler exceptions, token failures). */
  onError?: (error: unknown, context: string) => void;
}

export interface OnOptions {
  /** Only deliver events for this topic (e.g. `chat:<id>`). */
  topic?: string;
}

/** Realtime connection with ref-counted topic subscriptions and typed events. */
export interface RealtimeClient {
  /** Start connecting (idempotent). Also call after `close()` to start again. */
  connect(): void;
  /** Disconnect and stop reconnecting. Subscriptions are kept for the next `connect()`. */
  close(): void;
  /** App backgrounded: drop the socket but remember intent (no reconnect attempts). */
  pause(): void;
  /** App foregrounded: reconnect immediately (fires `onReconnect` listeners once open). */
  resume(): void;
  /** Drop the current socket and reconnect now with a fresh token (e.g. after sign-in). */
  reconnect(): void;
  /**
   * Ref-counted topic subscription: the first subscriber sends `subscribe`, the
   * last unsubscribe sends `unsubscribe`. Survives reconnects. Returns an
   * idempotent unsubscribe function.
   */
  subscribe(topic: string): () => void;
  /** Typed handler for one event type. Returns an unsubscribe function. */
  on<K extends RealtimeEventName>(type: K, handler: (event: RealtimeEventOf<K>) => void, options?: OnOptions): () => void;
  /** Handler for every event. */
  onAny(handler: (event: RealtimeEvent) => void): () => void;
  getState(): ConnectionState;
  /** Observe connection state. Returns an unsubscribe function. */
  onStateChange(listener: (state: ConnectionState) => void): () => void;
  /**
   * Called each time a connection opens after an earlier one had opened
   * (drop/recover, pause/resume). Refetch anything realtime keeps fresh.
   */
  onReconnect(listener: () => void): () => void;
  /** Estimated `local clock − server clock` in ms (from ping/pong), 0 until measured. */
  getServerTimeOffset(): number;
  /** Topics with at least one subscriber. */
  getSubscribedTopics(): string[];
  isPaused(): boolean;
}

const OPEN = 1;
const DEFAULT_BACKOFF: BackoffOptions = { initialMs: 1_000, maxMs: 30_000, factor: 2, jitter: 0.5 };

type AnyHandler = (event: RealtimeEvent) => void;
interface HandlerEntry {
  handler: AnyHandler;
  topic: string | undefined;
}

function median(values: number[]): number {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[mid]! : (sorted[mid - 1]! + sorted[mid]!) / 2;
}

/**
 * Create the realtime client for `/v1/ws`. Works in browsers and React Native;
 * nothing connects until `connect()` (ApiProvider does this for you).
 */
export function createRealtimeClient(options: RealtimeClientOptions): RealtimeClient {
  const heartbeatIntervalMs = options.heartbeatIntervalMs ?? 25_000;
  const heartbeatTimeoutMs = options.heartbeatTimeoutMs ?? 10_000;
  const backoff: BackoffOptions = { ...DEFAULT_BACKOFF, ...options.backoff };
  const random = options.random ?? Math.random;
  const now = options.now ?? (() => Date.now());
  const reportError = (error: unknown, context: string) => {
    try {
      options.onError?.(error, context);
    } catch {
      // never let the error reporter break the client
    }
  };

  let state: ConnectionState = "closed";
  let wanted = false;
  let paused = false;
  let ws: WebSocketLike | null = null;
  let generation = 0;
  let attempt = 0;
  let forceRefreshNext = false;
  let hasOpenedBefore = false;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  let deadlineTimer: ReturnType<typeof setTimeout> | null = null;
  let pingSentAt = 0;
  const offsetSamples: number[] = [];

  const topicCounts = new Map<string, number>();
  const handlers = new Map<string, Set<HandlerEntry>>();
  const anyHandlers = new Set<AnyHandler>();
  const stateListeners = new Set<(s: ConnectionState) => void>();
  const reconnectListeners = new Set<() => void>();

  function setState(next: ConnectionState) {
    if (state === next) return;
    state = next;
    for (const l of [...stateListeners]) {
      try {
        l(next);
      } catch (err) {
        reportError(err, "stateListener");
      }
    }
  }

  function isOpen(): boolean {
    return ws !== null && ws.readyState === OPEN && state === "open";
  }

  function send(message: RealtimeClientMessage): boolean {
    if (!ws || ws.readyState !== OPEN) return false;
    try {
      ws.send(JSON.stringify(message));
      return true;
    } catch (err) {
      reportError(err, "send");
      return false;
    }
  }

  function clearReconnectTimer() {
    if (reconnectTimer !== null) clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }

  function clearDeadline() {
    if (deadlineTimer !== null) clearTimeout(deadlineTimer);
    deadlineTimer = null;
  }

  function stopHeartbeat() {
    if (heartbeatTimer !== null) clearInterval(heartbeatTimer);
    heartbeatTimer = null;
    clearDeadline();
  }

  function detach(socket: WebSocketLike | null) {
    if (!socket) return;
    socket.onopen = null;
    socket.onmessage = null;
    socket.onclose = null;
    socket.onerror = null;
  }

  function teardownSocket(code: number, reason: string) {
    const socket = ws;
    ws = null;
    stopHeartbeat();
    detach(socket);
    if (socket) {
      try {
        socket.close(code, reason);
      } catch {
        // already closed
      }
    }
  }

  function sendPing() {
    if (!send({ type: "ping" })) return;
    pingSentAt = now();
    if (deadlineTimer === null) {
      deadlineTimer = setTimeout(() => {
        deadlineTimer = null;
        // No traffic since the ping: the TCP connection is probably dead (NAT timeout, radio switch).
        handleDrop(true, "heartbeat timeout");
      }, heartbeatTimeoutMs);
    }
  }

  function startHeartbeat() {
    stopHeartbeat();
    heartbeatTimer = setInterval(sendPing, heartbeatIntervalMs);
  }

  function scheduleReconnect() {
    clearReconnectTimer();
    const delay = computeBackoff(attempt, backoff, random);
    attempt++;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      if (wanted && !paused) openSocket();
    }, delay);
  }

  function handleDrop(opened: boolean, reason: string) {
    teardownSocket(4000, reason);
    if (!opened) forceRefreshNext = true;
    if (wanted && !paused) {
      setState("connecting");
      scheduleReconnect();
    } else {
      setState("closed");
    }
  }

  function dispatch(event: RealtimeEvent) {
    const set = handlers.get(event.type);
    if (set) {
      for (const entry of [...set]) {
        if (entry.topic !== undefined && entry.topic !== event.topic) continue;
        try {
          entry.handler(event);
        } catch (err) {
          reportError(err, `handler:${event.type}`);
        }
      }
    }
    for (const h of [...anyHandlers]) {
      try {
        h(event);
      } catch (err) {
        reportError(err, "handler:*");
      }
    }
  }

  function handleMessage(data: unknown) {
    if (typeof data !== "string") return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(data);
    } catch (err) {
      reportError(err, "parse");
      return;
    }
    if (typeof parsed !== "object" || parsed === null || typeof (parsed as { type?: unknown }).type !== "string") return;
    const event = parsed as RealtimeEvent;
    if (event.type === "pong" && pingSentAt > 0 && typeof event.ts === "string") {
      const serverTs = Date.parse(event.ts);
      if (!Number.isNaN(serverTs)) {
        const midpoint = (pingSentAt + now()) / 2;
        offsetSamples.push(midpoint - serverTs);
        if (offsetSamples.length > 5) offsetSamples.shift();
      }
    }
    dispatch(event);
  }

  function openSocket() {
    clearReconnectTimer();
    teardownSocket(1000, "reconnect");
    const gen = ++generation;
    setState("connecting");
    const forceRefresh = forceRefreshNext;
    Promise.resolve()
      .then(() => options.getToken(forceRefresh ? { forceRefresh: true } : undefined))
      .then(
        (token) => {
          if (gen !== generation || !wanted || paused) return;
          if (!token) {
            // Signed out: stay closed, try again later (or on reconnect()).
            setState("closed");
            scheduleReconnect();
            return;
          }
          const Impl = options.WebSocketImpl ?? (globalThis as { WebSocket?: WebSocketConstructor }).WebSocket;
          if (!Impl) {
            reportError(new Error("No WebSocket implementation available"), "connect");
            setState("closed");
            return;
          }
          let socket: WebSocketLike;
          try {
            socket = new Impl(`${options.url}${options.url.includes("?") ? "&" : "?"}token=${encodeURIComponent(token)}`);
          } catch (err) {
            reportError(err, "connect");
            handleDrop(false, "constructor failed");
            return;
          }
          ws = socket;
          let opened = false;
          let receivedSinceOpen = false;
          socket.onopen = () => {
            if (ws !== socket) return;
            opened = true;
            forceRefreshNext = false;
            setState("open");
            for (const topic of topicCounts.keys()) {
              if (!isServerManagedTopic(topic)) send({ type: "subscribe", topic });
            }
            sendPing(); // immediate liveness check + clock-offset sample
            startHeartbeat();
            if (hasOpenedBefore) {
              for (const l of [...reconnectListeners]) {
                try {
                  l();
                } catch (err) {
                  reportError(err, "reconnectListener");
                }
              }
            }
            hasOpenedBefore = true;
          };
          socket.onmessage = (event: { data?: unknown }) => {
            if (ws !== socket) return;
            clearDeadline();
            if (!receivedSinceOpen) {
              receivedSinceOpen = true;
              attempt = 0; // healthy round trip → reset backoff
            }
            handleMessage(event?.data);
          };
          socket.onclose = () => {
            if (ws !== socket) return;
            handleDrop(opened, "closed");
          };
          socket.onerror = () => {
            if (ws !== socket) return;
            handleDrop(opened, "error");
          };
        },
        (err: unknown) => {
          if (gen !== generation) return;
          reportError(err, "getToken");
          if (wanted && !paused) {
            setState("connecting");
            scheduleReconnect();
          }
        },
      );
  }

  return {
    connect() {
      wanted = true;
      if (paused) return;
      if (ws !== null || reconnectTimer !== null || state === "connecting") return;
      attempt = 0;
      openSocket();
    },
    close() {
      wanted = false;
      generation++;
      clearReconnectTimer();
      teardownSocket(1000, "client close");
      attempt = 0;
      setState("closed");
    },
    pause() {
      if (paused) return;
      paused = true;
      generation++;
      clearReconnectTimer();
      teardownSocket(1000, "paused");
      setState("closed");
    },
    resume() {
      if (!paused) return;
      paused = false;
      attempt = 0;
      if (wanted) openSocket();
    },
    reconnect() {
      if (!wanted || paused) return;
      attempt = 0;
      openSocket();
    },
    subscribe(topic: string) {
      const count = (topicCounts.get(topic) ?? 0) + 1;
      topicCounts.set(topic, count);
      if (count === 1 && !isServerManagedTopic(topic) && isOpen()) send({ type: "subscribe", topic });
      let done = false;
      return () => {
        if (done) return;
        done = true;
        const current = topicCounts.get(topic) ?? 0;
        if (current <= 1) {
          topicCounts.delete(topic);
          if (!isServerManagedTopic(topic) && isOpen()) send({ type: "unsubscribe", topic });
        } else {
          topicCounts.set(topic, current - 1);
        }
      };
    },
    on(type, handler, onOptions) {
      const entry: HandlerEntry = { handler: handler as AnyHandler, topic: onOptions?.topic };
      let set = handlers.get(type);
      if (!set) {
        set = new Set();
        handlers.set(type, set);
      }
      set.add(entry);
      return () => {
        handlers.get(type)?.delete(entry);
      };
    },
    onAny(handler) {
      anyHandlers.add(handler);
      return () => {
        anyHandlers.delete(handler);
      };
    },
    getState: () => state,
    onStateChange(listener) {
      stateListeners.add(listener);
      return () => {
        stateListeners.delete(listener);
      };
    },
    onReconnect(listener) {
      reconnectListeners.add(listener);
      return () => {
        reconnectListeners.delete(listener);
      };
    },
    getServerTimeOffset: () => median(offsetSamples),
    getSubscribedTopics: () => [...topicCounts.keys()],
    isPaused: () => paused,
  };
}
