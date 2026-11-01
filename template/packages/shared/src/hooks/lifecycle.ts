import { focusManager } from "@tanstack/react-query";
import type { RealtimeClient } from "../realtime/client";

/** The parts of React Native's `AppState` we use (pass `AppState` from "react-native"). */
export interface AppStateLike {
  currentState?: string | null;
  addEventListener(type: "change", listener: (state: string) => void): { remove(): void };
}

export interface AppStateLifecycleOptions {
  realtime?: RealtimeClient | null;
  /** Pause the socket while backgrounded (default true; pushes cover the gap). */
  pauseInBackground?: boolean;
  /** Drive react-query's focus refetching from AppState (default true). */
  manageFocus?: boolean;
}

/**
 * Expo apps: wire AppState to the realtime client (pause in background,
 * resume + refetch on foreground) and to react-query's focus manager.
 * Call once at the root; returns a cleanup function.
 *
 * ```ts
 * useEffect(() => bindAppStateLifecycle(AppState, { realtime }), [realtime]);
 * ```
 */
export function bindAppStateLifecycle(appState: AppStateLike, options: AppStateLifecycleOptions = {}): () => void {
  const { realtime, pauseInBackground = true, manageFocus = true } = options;
  const onChange = (state: string) => {
    if (state === "active") realtime?.resume();
    else if (state === "background" && pauseInBackground) realtime?.pause();
    // "inactive" (iOS control center, app switcher peek) is ignored to avoid churn.
  };
  const sub = appState.addEventListener("change", onChange);
  if (manageFocus) {
    focusManager.setEventListener((handleFocus) => {
      const focusSub = appState.addEventListener("change", (state) => handleFocus(state === "active"));
      return () => focusSub.remove();
    });
  }
  return () => {
    sub.remove();
    if (manageFocus) focusManager.setEventListener(() => () => undefined);
  };
}

/** The parts of `document` we use (pass `document`; never accessed globally). */
export interface VisibilityTarget {
  visibilityState: string;
  addEventListener(type: "visibilitychange", listener: () => void): void;
  removeEventListener(type: "visibilitychange", listener: () => void): void;
}

export interface VisibilityLifecycleOptions {
  realtime?: RealtimeClient | null;
  /**
   * Pause after the tab has been hidden this long (ms). Default `null` = never
   * (background tabs keep receiving events).
   */
  pauseAfterHiddenMs?: number | null;
}

/**
 * Web apps: when the tab becomes visible again, resume/reconnect the socket if
 * it isn't open (background tabs throttle timers, so heartbeats can lapse).
 * Optionally pause after a long hidden period. Returns a cleanup function.
 */
export function bindVisibilityLifecycle(doc: VisibilityTarget, options: VisibilityLifecycleOptions = {}): () => void {
  const { realtime, pauseAfterHiddenMs = null } = options;
  let hiddenTimer: ReturnType<typeof setTimeout> | null = null;
  const onChange = () => {
    if (!realtime) return;
    if (doc.visibilityState === "visible") {
      if (hiddenTimer !== null) clearTimeout(hiddenTimer);
      hiddenTimer = null;
      if (realtime.isPaused()) realtime.resume();
      else if (realtime.getState() !== "open") realtime.reconnect();
    } else if (pauseAfterHiddenMs !== null && hiddenTimer === null) {
      hiddenTimer = setTimeout(() => {
        hiddenTimer = null;
        realtime.pause();
      }, pauseAfterHiddenMs);
    }
  };
  doc.addEventListener("visibilitychange", onChange);
  return () => {
    doc.removeEventListener("visibilitychange", onChange);
    if (hiddenTimer !== null) clearTimeout(hiddenTimer);
  };
}
