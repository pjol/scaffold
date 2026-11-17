"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { normalizeSocialHandle, SOCIAL_HANDLE_PATTERN, socialEndpoints } from "./api";
import type {
  SocialActivityItem,
  SocialFollowState,
  SocialHandleCheck,
  SocialMyProfile,
  SocialPerson,
  SocialProfile,
  UpdateSocialProfileRequest,
} from "./types";

/** Query keys. `socialKeys.all()` invalidates everything social caches. */
export const socialKeys = {
  all: () => ["social"] as const,
  me: () => ["social", "me"] as const,
  handle: (h: string) => ["social", "handle", h] as const,
  profiles: () => ["social", "profile"] as const,
  profile: (h: string) => ["social", "profile", h] as const,
  followers: (h: string) => ["social", "followers", h] as const,
  following: (h: string) => ["social", "following", h] as const,
  activity: (h: string) => ["social", "activity", h] as const,
  feed: () => ["social", "feed"] as const,
  people: (q: string) => ["social", "people", q] as const,
  suggestions: () => ["social", "suggestions"] as const,
} as const;

// ───────────────────────────────────────────────────────── cache helpers ──

type Paged<T> = InfiniteData<Page<T>, string | undefined>;

/** Apply `fn` to person `userId` wherever social caches people (lists, search, suggestions) and to their profile. */
export function patchSocialPerson(qc: QueryClient, userId: string, fn: (p: { is_following: boolean; followers: number }) => { is_following: boolean; followers: number }): void {
  const each = (p: SocialPerson): SocialPerson => (p.user_id === userId ? { ...p, ...fn(p) } : p);
  for (const key of [["social", "followers"], ["social", "following"]] as const) {
    qc.setQueriesData<Paged<SocialPerson>>({ queryKey: key }, (d) => (d ? { ...d, pages: d.pages.map((pg) => ({ ...pg, items: pg.items.map(each) })) } : d));
  }
  for (const key of [["social", "people"], socialKeys.suggestions()] as const) {
    qc.setQueriesData<{ items: SocialPerson[] }>({ queryKey: key }, (d) => (d ? { items: d.items.map(each) } : d));
  }
  qc.setQueriesData<SocialProfile>({ queryKey: socialKeys.profiles() }, (p) => {
    if (!p || p.user.user_id !== userId) return p;
    const next = fn({ is_following: p.viewer.following, followers: p.followers });
    return { ...p, followers: next.followers, viewer: { ...p.viewer, following: next.is_following } };
  });
}

// Someone followed the caller: their own follower count and list change.
registerRealtimeHandler("notification", (qc, e) => {
  if (e.data.kind !== "social.follow") return;
  void qc.invalidateQueries({ queryKey: socialKeys.profiles() });
  void qc.invalidateQueries({ queryKey: ["social", "followers"] });
});

// ──────────────────────────────────────────────────────────────── reads ──

/** `GET /v1/social/me`: the caller's handle, bio, links and privacy. */
export function useSocialMe(): UseQueryResult<SocialMyProfile, ApiError> {
  const api = useEndpoints(socialEndpoints);
  return useApiQuery(socialKeys.me(), ({ signal }) => api.me({ signal }));
}

/** A profile by handle (or "me"). Pass null to disable. */
export function useSocialProfile(handle: string | null | undefined): UseQueryResult<SocialProfile, ApiError> {
  const api = useEndpoints(socialEndpoints);
  const h = normalizeSocialHandle(handle ?? "");
  return useApiQuery(socialKeys.profile(h), ({ signal }) => api.profile(h, { signal }), { retry: (n, e) => e.status !== 404 && n < 2 }, !!h);
}

export function useSocialFollowers(handle: string | null | undefined, enabled = true): PagedQueryResult<SocialPerson> {
  const api = useEndpoints(socialEndpoints);
  const h = normalizeSocialHandle(handle ?? "");
  return usePagedQuery<SocialPerson>(socialKeys.followers(h), (cursor, signal) => api.followers(h, { cursor, limit: 30 }, { signal }), undefined, enabled && !!h);
}

export function useSocialFollowing(handle: string | null | undefined, enabled = true): PagedQueryResult<SocialPerson> {
  const api = useEndpoints(socialEndpoints);
  const h = normalizeSocialHandle(handle ?? "");
  return usePagedQuery<SocialPerson>(socialKeys.following(h), (cursor, signal) => api.following(h, { cursor, limit: 30 }, { signal }), undefined, enabled && !!h);
}

/** A profile's Activity tab, newest first. */
export function useSocialProfileActivity(handle: string | null | undefined, enabled = true): PagedQueryResult<SocialActivityItem> {
  const api = useEndpoints(socialEndpoints);
  const h = normalizeSocialHandle(handle ?? "");
  return usePagedQuery<SocialActivityItem>(socialKeys.activity(h), (cursor, signal) => api.activity(h, { cursor, limit: 20 }, { signal }), undefined, enabled && !!h);
}

/** The following feed: activity of the people the caller follows. Refreshes every minute while shown. */
export function useSocialFeed(): PagedQueryResult<SocialActivityItem> {
  const api = useEndpoints(socialEndpoints);
  return usePagedQuery<SocialActivityItem>(socialKeys.feed(), (cursor, signal) => api.feed({ cursor, limit: 20 }, { signal }), { refetchInterval: 60_000 });
}

/** People by handle or name prefix, debounced 250 ms; idle under 2 characters. */
export function useSocialPeople(query: string): UseQueryResult<{ items: SocialPerson[] }, ApiError> & { query: string } {
  const api = useEndpoints(socialEndpoints);
  const q = useDebouncedValue(query.trim(), 250);
  const result = useApiQuery(socialKeys.people(q), ({ signal }) => api.people(q, { limit: 20 }, { signal }), { placeholderData: (prev) => prev }, q.replace(/^@/, "").length >= 2);
  return Object.assign(result, { query: q });
}

/** People to follow: followed by people the caller follows, then the most followed. */
export function useSocialSuggestions(limit = 10): UseQueryResult<{ items: SocialPerson[] }, ApiError> {
  const api = useEndpoints(socialEndpoints);
  return useApiQuery(socialKeys.suggestions(), ({ signal }) => api.suggestions({ limit }, { signal }), { staleTime: 60_000 });
}

/**
 * Whether the caller can take `handle`, checked 300 ms after typing stops. Idle (no
 * request) while the input isn't a well-formed handle or equals `current`.
 */
export function useSocialHandleCheck(handle: string, current?: string): UseQueryResult<SocialHandleCheck, ApiError> & { handle: string } {
  const api = useEndpoints(socialEndpoints);
  const h = useDebouncedValue(normalizeSocialHandle(handle), 300);
  const result = useApiQuery(socialKeys.handle(h), ({ signal }) => api.checkHandle(h, { signal }), { staleTime: 10_000 }, SOCIAL_HANDLE_PATTERN.test(h) && h !== current);
  return Object.assign(result, { handle: h });
}

// ──────────────────────────────────────────────────────────────── writes ──

/** Save bio, links and privacy. */
export function useUpdateSocialProfile(): UseMutationResult<SocialMyProfile, ApiError, UpdateSocialProfileRequest> {
  const api = useEndpoints(socialEndpoints);
  const qc = useQueryClient();
  return useMutation<SocialMyProfile, ApiError, UpdateSocialProfileRequest>({
    mutationFn: (body) => api.updateMe(body),
    onSuccess: (me) => {
      qc.setQueryData(socialKeys.me(), me);
      void qc.invalidateQueries({ queryKey: socialKeys.profiles() });
      void qc.invalidateQueries({ queryKey: ["social", "activity"] });
    },
  });
}

/** Change the handle (cooldown and redirect apply on the server). */
export function useChangeSocialHandle(): UseMutationResult<SocialMyProfile, ApiError, string> {
  const api = useEndpoints(socialEndpoints);
  const qc = useQueryClient();
  return useMutation<SocialMyProfile, ApiError, string>({
    mutationFn: (handle) => api.changeHandle(normalizeSocialHandle(handle)),
    onSuccess: (me) => {
      qc.setQueryData(socialKeys.me(), me);
      // Handles show everywhere people appear.
      void qc.invalidateQueries({ queryKey: socialKeys.all(), predicate: (q) => q.queryKey[1] !== "me" });
    },
  });
}

export interface SocialFollowVars {
  userId: string;
  /** true follows, false unfollows. */
  follow: boolean;
}

/** Follow or unfollow, optimistically everywhere the person shows; the server's count wins. */
export function useSocialFollow(): UseMutationResult<SocialFollowState, ApiError, SocialFollowVars, { snapshot: [readonly unknown[], unknown][] }> {
  const api = useEndpoints(socialEndpoints);
  const qc = useQueryClient();
  return useMutation<SocialFollowState, ApiError, SocialFollowVars, { snapshot: [readonly unknown[], unknown][] }>({
    mutationFn: ({ userId, follow }) => (follow ? api.follow(userId) : api.unfollow(userId)),
    onMutate: async ({ userId, follow }) => {
      await qc.cancelQueries({ queryKey: socialKeys.all() });
      const snapshot = qc.getQueriesData({ queryKey: socialKeys.all() }) as [readonly unknown[], unknown][];
      patchSocialPerson(qc, userId, (p) =>
        p.is_following === follow ? p : { is_following: follow, followers: Math.max(0, p.followers + (follow ? 1 : -1)) },
      );
      return { snapshot };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.snapshot ?? []) qc.setQueryData(key, data);
    },
    onSuccess: (st) => patchSocialPerson(qc, st.user_id, () => ({ is_following: st.following, followers: st.followers })),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: socialKeys.feed() });
      void qc.invalidateQueries({ queryKey: socialKeys.suggestions() });
      // The caller's own following count and list.
      void qc.invalidateQueries({ queryKey: socialKeys.profile("me") });
      void qc.invalidateQueries({ queryKey: ["social", "following"] });
    },
  });
}
