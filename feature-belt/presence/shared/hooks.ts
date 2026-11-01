"use client";

import { useMutation, useQueries, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints, useRealtime } from "../hooks/context";
import { useApiQuery, type QueryOverrides } from "../hooks/internal";
import { registerRealtimeHandler, registerReconnectRefetch } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import { presenceEndpoints, type PresenceEndpoints } from "./api";
import type { Presence, PresenceMine, PresencePlatform, PresenceSetStatusRequest } from "./types";

// ───────────────────────────────────────────────────────────── keys ──

export const presenceKeys = {
  all: () => ["presence"] as const,
  me: () => ["presence", "me"] as const,
  user: (id: string) => ["presence", "user", id] as const,
};

registerReconnectRefetch(presenceKeys.all());

function putPresence(qc: QueryClient, p: Presence): void {
  qc.setQueryData<Presence | null>(presenceKeys.user(p.user_id), p);
  qc.setQueryData<PresenceMine>(presenceKeys.me(), (old) => (old && old.user_id === p.user_id ? { ...old, ...p } : old));
}

registerRealtimeHandler("presence.updated", (qc, e) => putPresence(qc, e.data));

// ───────────────────────────────────────────────────────────── batching ──

/**
 * Collects the ids every mounted `usePresence` asks for within one tick and fetches
 * them in requests of up to 100, so a member list of 60 avatars costs one request.
 */
class PresenceBatcher {
  private queue = new Map<string, { resolve: (p: Presence | null) => void; reject: (e: unknown) => void }[]>();
  private timer: ReturnType<typeof setTimeout> | null = null;

  private readonly endpoints: PresenceEndpoints;

  constructor(endpoints: PresenceEndpoints) {
    this.endpoints = endpoints;
  }

  load(id: string): Promise<Presence | null> {
    return new Promise((resolve, reject) => {
      const waiting = this.queue.get(id) ?? [];
      waiting.push({ resolve, reject });
      this.queue.set(id, waiting);
      this.timer ??= setTimeout(() => this.flush(), 10);
    });
  }

  private flush(): void {
    this.timer = null;
    const batch = this.queue;
    this.queue = new Map();
    const ids = [...batch.keys()];
    for (let i = 0; i < ids.length; i += 100) {
      const chunk = ids.slice(i, i + 100);
      this.endpoints.list(chunk).then(
        (res) => {
          const byId = new Map(res.items.map((p) => [p.user_id, p]));
          for (const id of chunk) for (const w of batch.get(id) ?? []) w.resolve(byId.get(id) ?? null);
        },
        (err: unknown) => {
          for (const id of chunk) for (const w of batch.get(id) ?? []) w.reject(err);
        },
      );
    }
  }
}

const batchers = new WeakMap<PresenceEndpoints, PresenceBatcher>();

function batcherFor(endpoints: PresenceEndpoints): PresenceBatcher {
  let b = batchers.get(endpoints);
  if (!b) {
    b = new PresenceBatcher(endpoints);
    batchers.set(endpoints, b);
  }
  return b;
}

// ───────────────────────────────────────────────────────────── queries ──

/**
 * Presence for several people at once, batched across every component on screen and kept
 * live by `presence.updated`. Returns a map by user id: a `Presence`, `null` when you may
 * not see that person, or `undefined` while loading.
 */
export function usePresence(userIds: readonly (string | null | undefined)[]): Record<string, Presence | null | undefined> {
  const endpoints = useEndpoints(presenceEndpoints);
  const key = userIds.filter((id): id is string => !!id).join(",");
  const ids = useMemo(() => [...new Set(key ? key.split(",") : [])].sort(), [key]);
  return useQueries({
    queries: ids.map((id) => ({
      queryKey: presenceKeys.user(id),
      queryFn: () => batcherFor(endpoints).load(id),
      staleTime: 60_000,
      gcTime: 10 * 60_000,
    })),
    combine: (results) => {
      const out: Record<string, Presence | null | undefined> = {};
      results.forEach((r, i) => {
        const id = ids[i];
        if (id) out[id] = r.data;
      });
      return out;
    },
  });
}

/** One person's presence (`null` when hidden from you, `undefined` while loading). */
export function useUserPresence(userId: string | null | undefined): Presence | null | undefined {
  const map = usePresence([userId]);
  return userId ? map[userId] : undefined;
}

/** Your own presence, status settings and the audiences to subscribe to. */
export function useMyPresence(options?: QueryOverrides<PresenceMine>): UseQueryResult<PresenceMine, ApiError> {
  const p = useEndpoints(presenceEndpoints);
  return useApiQuery(presenceKeys.me(), ({ signal }) => p.me({ signal }), { staleTime: 60_000, ...options });
}

// ───────────────────────────────────────────────────────────── mutations ──

function useMineMutation<V>(fn: (v: V) => Promise<PresenceMine>): UseMutationResult<PresenceMine, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<PresenceMine, ApiError, V>({
    mutationFn: fn,
    onSuccess: (mine) => {
      qc.setQueryData(presenceKeys.me(), mine);
      qc.setQueryData<Presence>(presenceKeys.user(mine.user_id), { user_id: mine.user_id, status: mine.status, custom_status: mine.custom_status, dnd_until: mine.dnd_until });
    },
  });
}

/** Set your custom status (build the body with `presenceStatusRequest`). */
export function useSetPresenceStatus(): UseMutationResult<PresenceMine, ApiError, PresenceSetStatusRequest> {
  const p = useEndpoints(presenceEndpoints);
  return useMineMutation((body: PresenceSetStatusRequest) => p.setStatus(body));
}

export function useClearPresenceStatus(): UseMutationResult<PresenceMine, ApiError, void> {
  const p = useEndpoints(presenceEndpoints);
  return useMineMutation(() => p.clearStatus());
}

/** Pause notifications until a time (`presenceDndUntil`), or resume with `null`. */
export function useSetPresenceDND(): UseMutationResult<PresenceMine, ApiError, string | null> {
  const p = useEndpoints(presenceEndpoints);
  return useMineMutation((until: string | null) => (until ? p.setDND(until) : p.clearDND()));
}

/** "Appear away" on or off. */
export function useSetPresenceAway(): UseMutationResult<PresenceMine, ApiError, boolean> {
  const p = useEndpoints(presenceEndpoints);
  return useMineMutation((away: boolean) => p.setAway(away));
}

// ───────────────────────────────────────────────────────────── session ──

/** Something the heartbeat can observe: returns an unsubscribe function. */
export type PresenceSubscribe = (listener: () => void) => () => void;

export interface PresenceSessionOptions {
  /** False while signed out or before consent: no heartbeats, no topics. */
  enabled: boolean;
  platform: PresencePlatform;
  /** Stable for this tab or app launch (`generateId()` once). */
  deviceId: string;
  /** Whether the app is on screen (web: tab visible; native: AppState active). */
  isVisible: () => boolean;
  onVisibilityChange: PresenceSubscribe;
  /** Input from the person (web: pointer, keys, scroll). Without it, visible = active. */
  onActivity?: PresenceSubscribe;
  /** False on native: backgrounded apps can't beat, so stop instead of reporting idle. */
  beatWhileHidden?: boolean;
}

/**
 * Keeps you online: a heartbeat every `heartbeat_seconds` while the app is on screen,
 * `active: false` once you've been idle for `idle_after_seconds` (or the tab is hidden,
 * on web), at once on every change, and `offline` when the session ends. Also
 * subscribes to your presence audiences so others' changes arrive live. Mount once
 * per signed-in session.
 */
export function usePresenceSession(options: PresenceSessionOptions): void {
  const endpoints = useEndpoints(presenceEndpoints);
  const realtime = useRealtime();
  const qc = useQueryClient();
  const mine = useMyPresence({ enabled: options.enabled });
  const opts = useRef(options);
  opts.current = options;

  const audiences = mine.data?.audiences.join(",") ?? "";
  useEffect(() => {
    if (!options.enabled || !realtime || !audiences) return undefined;
    const offs = audiences.split(",").map((a) => realtime.subscribe(topic("presence", a)));
    return () => offs.forEach((off) => off());
  }, [options.enabled, realtime, audiences]);

  const { enabled, deviceId, platform } = options;
  useEffect(() => {
    if (!enabled) return undefined;
    let intervalMs = 45_000;
    let idleAfterMs = 10 * 60_000;
    let lastInput = Date.now();
    let sentActive: boolean | null = null;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;

    const activeNow = () => opts.current.isVisible() && Date.now() - lastInput < idleAfterMs;
    const schedule = () => {
      if (timer) clearTimeout(timer);
      timer = null;
      if (stopped) return;
      if (!opts.current.isVisible() && opts.current.beatWhileHidden === false) return;
      timer = setTimeout(beat, intervalMs);
    };
    const beat = () => {
      if (stopped) return;
      const active = activeNow();
      sentActive = active;
      endpoints.heartbeat({ device_id: deviceId, platform, active }).then(
        (res) => {
          intervalMs = Math.max(10, res.heartbeat_seconds) * 1000;
          idleAfterMs = Math.max(60, res.idle_after_seconds) * 1000;
          qc.setQueryData<PresenceMine>(presenceKeys.me(), (old) => (old ? { ...old, status: res.status } : old));
        },
        () => undefined,
      );
      schedule();
    };
    const onChange = () => {
      if (activeNow() !== sentActive || (opts.current.isVisible() && !timer)) beat();
    };
    const onInput = () => {
      const wasIdle = Date.now() - lastInput >= idleAfterMs;
      lastInput = Date.now();
      if (wasIdle || sentActive === false) onChange();
    };
    const idleCheck = setInterval(() => {
      if (sentActive && !activeNow()) beat();
    }, 30_000);

    const offVisibility = opts.current.onVisibilityChange(onChange);
    const offActivity = opts.current.onActivity?.(onInput) ?? (() => undefined);
    if (opts.current.isVisible() || opts.current.beatWhileHidden !== false) beat();
    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
      clearInterval(idleCheck);
      offVisibility();
      offActivity();
      endpoints.offline(deviceId).catch(() => undefined);
    };
  }, [enabled, deviceId, platform, endpoints, qc]);
}
