"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, useTopic, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { feedEndpoints } from "./api";
import {
  FEED_ALL_TOPIC,
  type AdminFeedListParams,
  type CreateFeedCommentRequest,
  type CreateFeedPostRequest,
  type FeedAuthor,
  type FeedComment,
  type FeedListParams,
  type FeedPost,
  type FeedReaction,
  type FeedReactionCounts,
  type FeedReactionSummary,
  type FeedUnread,
  type UpdateFeedCommentRequest,
  type UpdateFeedPostRequest,
} from "./types";

/** Query keys. `feedKeys.all()` invalidates everything the feed caches. */
export const feedKeys = {
  all: () => ["feed"] as const,
  posts: () => ["feed", "posts"] as const,
  postList: (params: Record<string, unknown> = {}) => ["feed", "posts", params] as const,
  pinned: () => ["feed", "pinned"] as const,
  post: (id: string) => ["feed", "post", id] as const,
  comments: (postId: string) => ["feed", "comments", postId] as const,
  unread: () => ["feed", "unread"] as const,
  people: (q: string) => ["feed", "people", q] as const,
  admin: {
    all: () => ["feed", "admin"] as const,
    posts: (params: Record<string, unknown> = {}) => ["feed", "admin", "posts", params] as const,
    post: (id: string) => ["feed", "admin", "post", id] as const,
  },
} as const;

// ───────────────────────────────────────────────────────── cache helpers ──

type Paged<T> = InfiniteData<Page<T>, string | undefined>;

function mapPaged<T>(data: Paged<T> | undefined, fn: (item: T) => T | null): Paged<T> | undefined {
  if (!data) return data;
  return {
    ...data,
    pages: data.pages.map((page) => ({ ...page, items: page.items.map(fn).filter((x): x is T => x !== null) })),
  };
}

/** Apply `fn` to post `id` in every cached list, the pinned list and its detail (`null` removes it from lists). */
export function patchFeedPost(qc: QueryClient, id: string, fn: (post: FeedPost) => FeedPost | null): void {
  const each = (p: FeedPost) => (p.id === id ? fn(p) : p);
  qc.setQueriesData<Paged<FeedPost>>({ queryKey: feedKeys.posts() }, (d) => mapPaged(d, each));
  qc.setQueriesData<Paged<FeedPost>>({ queryKey: feedKeys.admin.posts() }, (d) => mapPaged(d, each));
  qc.setQueryData<{ items: FeedPost[] }>(feedKeys.pinned(), (d) => (d ? { items: d.items.map(each).filter((x): x is FeedPost => x !== null) } : d));
  qc.setQueryData<FeedPost>(feedKeys.post(id), (p) => (p ? (fn(p) ?? p) : p));
}

/** Apply `fn` to one comment in its post's cached comment list. */
export function patchFeedComment(qc: QueryClient, postId: string, commentId: string, fn: (comment: FeedComment) => FeedComment | null): void {
  qc.setQueryData<Paged<FeedComment>>(feedKeys.comments(postId), (d) => mapPaged(d, (c) => (c.id === commentId ? fn(c) : c)));
}

/** Counts after the viewer switches from `from` to `to` (either may be null). */
export function nextReactionCounts(counts: FeedReactionCounts, total: number, from: FeedReaction | null, to: FeedReaction | null): { reactions: FeedReactionCounts; reaction_count: number } {
  const next: FeedReactionCounts = { ...counts };
  let sum = total;
  if (from) {
    const n = (next[from] ?? 0) - 1;
    if (n > 0) next[from] = n;
    else delete next[from];
    sum -= 1;
  }
  if (to) {
    next[to] = (next[to] ?? 0) + 1;
    sum += 1;
  }
  return { reactions: next, reaction_count: Math.max(0, sum) };
}

// ───────────────────────────────────────────────────────────── realtime ──

registerRealtimeHandler("feed.post", (qc, e) => {
  void qc.invalidateQueries({ queryKey: feedKeys.unread() });
  if (e.data.kind === "announcement") void qc.invalidateQueries({ queryKey: feedKeys.pinned() });
});

registerRealtimeHandler("feed.post_updated", (qc, e) => {
  const { post_id, deleted } = e.data;
  if (deleted) patchFeedPost(qc, post_id, () => null);
  void qc.invalidateQueries({ queryKey: feedKeys.post(post_id) });
  void qc.invalidateQueries({ queryKey: feedKeys.pinned() });
  void qc.invalidateQueries({ queryKey: feedKeys.unread() });
  if (!deleted) {
    // Refetch the lists that hold the post (an edit, a pin change).
    const holders = qc.getQueriesData<Paged<FeedPost>>({ queryKey: feedKeys.posts() }).filter(([, d]) => d?.pages.some((p) => p.items.some((x) => x.id === post_id)));
    for (const [key] of holders) void qc.invalidateQueries({ queryKey: key, exact: true });
  }
});

registerRealtimeHandler("feed.comment", (qc, e) => {
  patchFeedPost(qc, e.data.post_id, (p) => ({ ...p, comment_count: e.data.comment_count }));
  void qc.invalidateQueries({ queryKey: feedKeys.comments(e.data.post_id) });
});

registerRealtimeHandler("feed.reaction", (qc, e) => {
  const { target_kind, target_id, post_id, reactions, reaction_count } = e.data;
  if (target_kind === "post") patchFeedPost(qc, target_id, (p) => ({ ...p, reactions, reaction_count }));
  else patchFeedComment(qc, post_id, target_id, (c) => ({ ...c, reactions, reaction_count }));
});

registerReconnectRefetch(feedKeys.all());

/** Subscribe to the feed's realtime topic while mounted (the "all" audience by default). */
export function useFeedTopic(topic: string | null = FEED_ALL_TOPIC): void {
  useTopic(topic);
}

// ──────────────────────────────────────────────────────────────── reads ──

/** `GET /v1/feed/posts` as an infinite list, newest first. */
export function useFeedPosts(params: FeedListParams = {}, options?: InfiniteOverrides<FeedPost>): PagedQueryResult<FeedPost> {
  const api = useEndpoints(feedEndpoints);
  return usePagedQuery<FeedPost>(feedKeys.postList(listKeyParams(params)), (cursor, signal) => api.posts({ ...params, cursor }, { signal }), options);
}

/** `GET /v1/feed/pinned`: up to 3 active announcements, shown above the feed. */
export function useFeedPinned(options?: QueryOverrides<{ items: FeedPost[] }>): UseQueryResult<{ items: FeedPost[] }, ApiError> {
  const api = useEndpoints(feedEndpoints);
  return useApiQuery(feedKeys.pinned(), ({ signal }) => api.pinned({ signal }), options);
}

/** `GET /v1/feed/posts/{id}`. Pass null to disable. */
export function useFeedPost(id: string | null | undefined, options?: QueryOverrides<FeedPost>): UseQueryResult<FeedPost, ApiError> {
  const api = useEndpoints(feedEndpoints);
  return useApiQuery(feedKeys.post(id ?? ""), ({ signal }) => api.post(id ?? "", { signal }), options, !!id);
}

/** A post's comments and replies, oldest first, 50 per page. Thread them with `threadComments`. */
export function useFeedComments(postId: string | null | undefined, options?: InfiniteOverrides<FeedComment>): PagedQueryResult<FeedComment> {
  const api = useEndpoints(feedEndpoints);
  return usePagedQuery<FeedComment>(feedKeys.comments(postId ?? ""), (cursor, signal) => api.comments(postId ?? "", { cursor, limit: 50 }, { signal }), options, !!postId);
}

/** `GET /v1/feed/unread`, refreshed by new posts over realtime. */
export function useFeedUnread(options?: QueryOverrides<FeedUnread>): UseQueryResult<FeedUnread, ApiError> {
  const api = useEndpoints(feedEndpoints);
  return useApiQuery(feedKeys.unread(), ({ signal }) => api.unread({ signal }), { staleTime: 30_000, ...options });
}

/** Unread count for a nav badge: reads the cache and keeps the feed topic subscribed. */
export function useFeedUnreadCount(): number | undefined {
  useFeedTopic();
  const unread = useFeedUnread();
  return unread.data?.unread_count || undefined;
}

/** Mention suggestions for the text after "@" (debounced; empty query = no request). */
export function useFeedPeople(query: string, options?: QueryOverrides<{ items: FeedAuthor[] }>): UseQueryResult<{ items: FeedAuthor[] }, ApiError> {
  const api = useEndpoints(feedEndpoints);
  const q = useDebouncedValue(query.trim(), 200);
  return useApiQuery(feedKeys.people(q), ({ signal }) => api.people(q, { signal }), { staleTime: 60_000, placeholderData: (prev) => prev, ...options }, q.length > 0);
}

/** Comments grouped into threads: top-level comments in order, each with its replies. */
export function threadComments(comments: readonly FeedComment[]): Array<{ comment: FeedComment; replies: FeedComment[] }> {
  const threads: Array<{ comment: FeedComment; replies: FeedComment[] }> = [];
  const byId = new Map<string, { comment: FeedComment; replies: FeedComment[] }>();
  for (const c of comments) {
    if (!c.parent_id) {
      const t = { comment: c, replies: [] as FeedComment[] };
      threads.push(t);
      byId.set(c.id, t);
    }
  }
  for (const c of comments) {
    if (c.parent_id) byId.get(c.parent_id)?.replies.push(c);
  }
  return threads;
}

// ──────────────────────────────────────────────────────────── mutations ──

/** `POST /v1/feed/seen`: clears the unread dot. */
export function useMarkFeedSeen(): UseMutationResult<void, ApiError, void> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: () => api.markSeen(),
    onMutate: () => {
      qc.setQueryData<FeedUnread>(feedKeys.unread(), { unread_count: 0, has_unread: false });
    },
  });
}

/** `POST /v1/feed/posts`: the new post goes to the top of the feed (or the pinned list). */
export function useCreateFeedPost(): UseMutationResult<FeedPost, ApiError, CreateFeedPostRequest> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedPost, ApiError, CreateFeedPostRequest>({
    mutationFn: (body) => api.createPost(body),
    onSuccess: (post) => insertCreated(qc, post),
  });
}

function insertCreated(qc: QueryClient, post: FeedPost): void {
  qc.setQueryData(feedKeys.post(post.id), post);
  if (post.kind === "announcement" && post.pinned_until) {
    void qc.invalidateQueries({ queryKey: feedKeys.pinned() });
    void qc.invalidateQueries({ queryKey: feedKeys.posts() });
  } else {
    qc.setQueryData<Paged<FeedPost>>(feedKeys.postList({}), (d) => {
      if (!d || d.pages.length === 0) return d;
      const [first, ...rest] = d.pages;
      if (!first || first.items.some((p) => p.id === post.id)) return d;
      return { ...d, pages: [{ ...first, items: [post, ...first.items] }, ...rest] };
    });
    void qc.invalidateQueries({ queryKey: feedKeys.posts(), refetchType: "none" });
  }
  void qc.invalidateQueries({ queryKey: feedKeys.admin.all(), refetchType: "none" });
}

/** `PATCH /v1/feed/posts/{id}` (author only). */
export function useUpdateFeedPost(): UseMutationResult<FeedPost, ApiError, { id: string } & UpdateFeedPostRequest> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedPost, ApiError, { id: string } & UpdateFeedPostRequest>({
    mutationFn: ({ id, ...body }) => api.updatePost(id, body),
    onSuccess: (post) => patchFeedPost(qc, post.id, () => post),
  });
}

/** `DELETE /v1/feed/posts/{id}` (author only): removed from every list. */
export function useDeleteFeedPost(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.deletePost(id),
    onSuccess: (_d, id) => {
      patchFeedPost(qc, id, () => null);
      qc.removeQueries({ queryKey: feedKeys.post(id) });
    },
  });
}

interface ReactionTarget {
  id: string;
  post_id: string;
  reactions: FeedReactionCounts;
  reaction_count: number;
  my_reaction: FeedReaction | null;
}

/** Set (`reaction`) or clear (`null`) the viewer's reaction on a post. Optimistic. */
export function useReactToFeedPost(): UseMutationResult<FeedReactionSummary, ApiError, { post: Omit<ReactionTarget, "post_id">; reaction: FeedReaction | null }, ReactionTarget> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedReactionSummary, ApiError, { post: Omit<ReactionTarget, "post_id">; reaction: FeedReaction | null }, ReactionTarget>({
    mutationFn: ({ post, reaction }) => (reaction ? api.reactPost(post.id, reaction) : api.unreactPost(post.id)),
    onMutate: ({ post, reaction }) => {
      const next = nextReactionCounts(post.reactions, post.reaction_count, post.my_reaction, reaction);
      patchFeedPost(qc, post.id, (p) => ({ ...p, ...next, my_reaction: reaction }));
      return { ...post, post_id: post.id };
    },
    onSuccess: (s) => patchFeedPost(qc, s.target_id, (p) => ({ ...p, reactions: s.reactions, reaction_count: s.reaction_count, my_reaction: s.my_reaction ?? null })),
    onError: (_e, _v, prev) => {
      if (prev) patchFeedPost(qc, prev.id, (p) => ({ ...p, reactions: prev.reactions, reaction_count: prev.reaction_count, my_reaction: prev.my_reaction }));
    },
  });
}

/** Set or clear the viewer's reaction on a comment. Optimistic. */
export function useReactToFeedComment(): UseMutationResult<FeedReactionSummary, ApiError, { comment: ReactionTarget; reaction: FeedReaction | null }, ReactionTarget> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedReactionSummary, ApiError, { comment: ReactionTarget; reaction: FeedReaction | null }, ReactionTarget>({
    mutationFn: ({ comment, reaction }) => (reaction ? api.reactComment(comment.id, reaction) : api.unreactComment(comment.id)),
    onMutate: ({ comment, reaction }) => {
      const next = nextReactionCounts(comment.reactions, comment.reaction_count, comment.my_reaction, reaction);
      patchFeedComment(qc, comment.post_id, comment.id, (c) => ({ ...c, ...next, my_reaction: reaction }));
      return comment;
    },
    onSuccess: (s) => patchFeedComment(qc, s.post_id, s.target_id, (c) => ({ ...c, reactions: s.reactions, reaction_count: s.reaction_count, my_reaction: s.my_reaction ?? null })),
    onError: (_e, _v, prev) => {
      if (prev) patchFeedComment(qc, prev.post_id, prev.id, (c) => ({ ...c, reactions: prev.reactions, reaction_count: prev.reaction_count, my_reaction: prev.my_reaction }));
    },
  });
}

/** `POST /v1/feed/posts/{id}/comments`. */
export function useCreateFeedComment(): UseMutationResult<FeedComment, ApiError, { postId: string } & CreateFeedCommentRequest> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedComment, ApiError, { postId: string } & CreateFeedCommentRequest>({
    mutationFn: ({ postId, ...body }) => api.createComment(postId, body),
    onSuccess: (comment) => {
      qc.setQueryData<Paged<FeedComment>>(feedKeys.comments(comment.post_id), (d) => {
        if (!d || d.pages.length === 0) return d;
        const last = d.pages[d.pages.length - 1];
        // Only when every page is loaded: otherwise the refetch puts it in place.
        if (!last || last.next_cursor || last.items.some((c) => c.id === comment.id)) return d;
        return { ...d, pages: [...d.pages.slice(0, -1), { ...last, items: [...last.items, comment] }] };
      });
      patchFeedPost(qc, comment.post_id, (p) => ({ ...p, comment_count: p.comment_count + 1 }));
    },
  });
}

/** `PATCH /v1/feed/comments/{id}` (author only). */
export function useUpdateFeedComment(): UseMutationResult<FeedComment, ApiError, { id: string } & UpdateFeedCommentRequest> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedComment, ApiError, { id: string } & UpdateFeedCommentRequest>({
    mutationFn: ({ id, ...body }) => api.updateComment(id, body),
    onSuccess: (comment) => patchFeedComment(qc, comment.post_id, comment.id, () => comment),
  });
}

/** `DELETE /v1/feed/comments/{id}` (its author, or the post's author). */
export function useDeleteFeedComment(): UseMutationResult<void, ApiError, { id: string; postId: string }> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; postId: string }>({
    mutationFn: ({ id }) => api.deleteComment(id),
    onSuccess: (_d, { postId }) => {
      void qc.invalidateQueries({ queryKey: feedKeys.comments(postId) });
      patchFeedPost(qc, postId, (p) => ({ ...p, comment_count: Math.max(0, p.comment_count - 1) }));
    },
  });
}

// ──────────────────────────────────────────────────────────────── admin ──

/** `GET /v1/admin/feed/posts`: every post, removed ones included. */
export function useAdminFeedPosts(params: AdminFeedListParams = {}, options?: InfiniteOverrides<FeedPost>): PagedQueryResult<FeedPost> {
  const api = useEndpoints(feedEndpoints);
  return usePagedQuery<FeedPost>(feedKeys.admin.posts(listKeyParams(params)), (cursor, signal) => api.admin.posts({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** Staff removal of a post (soft, audited). */
export function useAdminRemoveFeedPost(): UseMutationResult<void, ApiError, { id: string; reason?: string }> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; reason?: string }>({
    mutationFn: ({ id, reason }) => api.admin.removePost(id, reason),
    onSuccess: (_d, { id }) => {
      qc.setQueriesData<Paged<FeedPost>>({ queryKey: feedKeys.admin.posts() }, (d) => mapPaged(d, (p) => (p.id === id ? { ...p, removed_at: new Date().toISOString() } : p)));
      qc.setQueriesData<Paged<FeedPost>>({ queryKey: feedKeys.posts() }, (d) => mapPaged(d, (p) => (p.id === id ? null : p)));
      void qc.invalidateQueries({ queryKey: feedKeys.pinned() });
      void qc.invalidateQueries({ queryKey: feedKeys.admin.all() });
    },
  });
}

/** Staff removal of a comment (soft, audited). */
export function useAdminRemoveFeedComment(): UseMutationResult<void, ApiError, { id: string; postId: string; reason?: string }> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; postId: string; reason?: string }>({
    mutationFn: ({ id, reason }) => api.admin.removeComment(id, reason),
    onSuccess: (_d, { postId }) => {
      void qc.invalidateQueries({ queryKey: feedKeys.comments(postId) });
    },
  });
}

/** Change or end an announcement's pin (`pinned_until: null`). */
export function useAdminPinFeedPost(): UseMutationResult<FeedPost, ApiError, { id: string; pinned_until: string | null }> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedPost, ApiError, { id: string; pinned_until: string | null }>({
    mutationFn: ({ id, pinned_until }) => api.admin.pin(id, pinned_until),
    onSuccess: (post) => {
      patchFeedPost(qc, post.id, () => post);
      void qc.invalidateQueries({ queryKey: feedKeys.pinned() });
      void qc.invalidateQueries({ queryKey: feedKeys.posts() });
    },
  });
}

/** `POST /v1/admin/feed/announcements` (audited). */
export function useCreateAnnouncement(): UseMutationResult<FeedPost, ApiError, Omit<CreateFeedPostRequest, "kind">> {
  const api = useEndpoints(feedEndpoints);
  const qc = useQueryClient();
  return useMutation<FeedPost, ApiError, Omit<CreateFeedPostRequest, "kind">>({
    mutationFn: (body) => api.admin.createAnnouncement(body),
    onSuccess: (post) => {
      insertCreated(qc, post);
      void qc.invalidateQueries({ queryKey: feedKeys.admin.all() });
    },
  });
}
