import type { RealtimeEvent, RealtimeEventMap } from "../types";

/** Event types with a registered payload (`RealtimeEventMap`, extended by features). */
export type RealtimeEventType = keyof RealtimeEventMap & string;
/** Payload for one event type (`unknown` for types nobody registered). */
export type RealtimeDataOf<K extends string> = K extends keyof RealtimeEventMap ? RealtimeEventMap[K] : unknown;
/** The event object for one type, e.g. `RealtimeEventOf<"notification">`. */
export type RealtimeEventOf<K extends string> = RealtimeEvent<RealtimeDataOf<K>, K>;

/**
 * - `connecting`: opening, or waiting to reconnect after a drop
 * - `open`: connected and subscribed
 * - `closed`: not connected and not trying (closed, paused or signed out)
 */
export type ConnectionState = "connecting" | "open" | "closed";

/**
 * Minimal WebSocket surface the client uses: satisfied by the browser and React Native
 * `WebSocket`, and easy to fake in tests.
 */
export interface WebSocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(code?: number, reason?: string): void;
  // `any` params keep DOM/RN WebSocket (typed `this`/`MessageEvent`) assignable.
  onopen: ((event: any) => unknown) | null;
  onmessage: ((event: any) => unknown) | null;
  onclose: ((event: any) => unknown) | null;
  onerror: ((event: any) => unknown) | null;
}

/** WebSocket constructor (global `WebSocket` by default). */
export type WebSocketConstructor = new (url: string) => WebSocketLike;
