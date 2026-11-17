"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, useTopic, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { forumEndpoints } from "./api";
import {
  forumPostTopic,
  type CreateForumCommentRequest,
  type CreateForumPostRequest,
  type ForumAdminCommunity,
  type ForumAuthor,
  type ForumComment,
  type ForumCommentModAction,
  type ForumCommentResult,
  type ForumCommentSort,
  type ForumCommunity,
  type ForumCommunityDetail,
  type ForumCommunityInput,
  type ForumKarma,
  type ForumPost,
  type ForumPostListParams,
  type ForumPostModAction,
  type ForumPostResult,
  type ForumPostSummary,
  type ForumVoteRequest,
  type ForumVoteResult,
  type ForumVoteValue,
  type UpdateForumCommentRequest,
  type UpdateForumPostRequest,
} from "./types";

/** Query keys. `forumKeys.all()` invalidates everything the forum caches. */
export const forumKeys = {
  all: () => ["forum"] as const,
  communities: () => ["forum", "communities"] as const,
  community: (slug: string) => ["forum", "community", slug] as const,
  posts: () => ["forum", "posts"] as const,
  postList: (params: Record<string, unknown> = {}) => ["forum", "posts", params] as const,
  post: (id: string) => ["forum", "post", id] as const,
  comments: (postId: string) => ["forum", "comments", postId] as const,
  commentList: (postId: string, sort: ForumCommentSort) => ["forum", "comments", postId, sort] as const,
  replies: (commentId: string) => ["forum", "replies", commentId] as const,
  people: (q: string) => ["forum", "people", q] as const,
  karma: (userId: string) => ["forum", "karma", userId] as const,
  admin: {
    all: () => ["forum", "admin"] as const,
    communities: () => ["forum", "admin", "communities"] as const,
  },
} as const;

// ───────────────────────────────────────────────────────── cache helpers ──

type Paged<T> = InfiniteData<Page<T>, string | undefined>;
type Voted = { score: number; viewer_vote: ForumVoteValue };

function mapPaged<T>(data: Paged<T> | undefined, fn: (item: T) => T): Paged<T> | undefined {
  if (!data) return data;
  return { ...data, pages: data.pages.map((page) => ({ ...page, items: page.items.map(fn) })) };
}

/** Apply `fn` to post `id` in every cached list, the communities' pinned lists and its detail. */
export function patchForumPost(qc: QueryClient, id: string, fn: <P extends ForumPostSummary>(post: P) => P): void {
  qc.setQueriesData<Paged<ForumPostSummary>>({ queryKey: forumKeys.posts() }, (d) => mapPaged(d, (p) => (p.id === id ? fn(p) : p)));
  qc.setQueriesData<ForumCommunityDetail>({ queryKey: ["forum", "community"] }, (d) => (d ? { ...d, pinned: d.pinned.map((p) => (p.id === id ? fn(p) : p)) } : d));
  qc.setQueryData<ForumPost>(forumKeys.post(id), (p) => (p ? fn(p) : p));
}

function mapComment(c: ForumComment, id: string, fn: (c: ForumComment) => ForumComment): ForumComment {
  const self = c.id === id ? fn(c) : c;
  return self.replies.length ? { ...self, replies: self.replies.map((r) => mapComment(r, id, fn)) } : self;
}

/** Apply `fn` to comment `id` wherever it is cached (every sort of its post's tree, reply pages). */
export function patchForumComment(qc: QueryClient, postId: string, id: string, fn: (c: ForumComment) => ForumComment): void {
  qc.setQueriesData<Paged<ForumComment>>({ queryKey: forumKeys.comments(postId) }, (d) => mapPaged(d, (c) => mapComment(c, id, fn)));
  qc.setQueriesData<Paged<ForumComment>>({ queryKey: ["forum", "replies"] }, (d) => mapPaged(d, (c) => mapComment(c, id, fn)));
}

function findComment(list: ForumComment[], id: string): ForumComment | null {
  for (const c of list) {
    if (c.id === id) return c;
    const hit = findComment(c.replies, id);
    if (hit) return hit;
  }
  return null;
}

/** The viewer's cached vote on a post or comment (0 when not cached). */
function cachedVote(qc: QueryClient, kind: "post" | "comment", id: string, postId: string): ForumVoteValue {
  if (kind === "post") {
    const detail = qc.getQueryData<ForumPost>(forumKeys.post(id));
    if (detail) return detail.viewer_vote;
    for (const [, d] of qc.getQueriesData<Paged<ForumPostSummary>>({ queryKey: forumKeys.posts() })) {
      const hit = d?.pages.flatMap((p) => p.items).find((p) => p.id === id);
      if (hit) return hit.viewer_vote;
    }
    return 0;
  }
  for (const [, d] of qc.getQueriesData<Paged<ForumComment>>({ queryKey: forumKeys.comments(postId) })) {
    const hit = d ? findComment(d.pages.flatMap((p) => p.items), id) : null;
    if (hit) return hit.viewer_vote;
  }
  return 0;
}

// ───────────────────────────────────────────────────────────── realtime ──

registerRealtimeHandler("forum.comment", (qc, e) => {
  patchForumPost(qc, e.data.post_id, (p) => ({ ...p, comment_count: e.data.comment_count }));
  void qc.invalidateQueries({ queryKey: forumKeys.comments(e.data.post_id) });
  if (e.data.parent_id) void qc.invalidateQueries({ queryKey: forumKeys.replies(e.data.parent_id) });
});

registerRealtimeHandler("forum.vote", (qc, e) => {
  const { post_id, target_kind, target_id, score } = e.data;
  if (target_kind === "post") patchForumPost(qc, target_id, (p) => ({ ...p, score }));
  else patchForumComment(qc, post_id, target_id, (c) => ({ ...c, score }));
});

registerRealtimeHandler("forum.status", (qc, e) => {
  void qc.invalidateQueries({ queryKey: forumKeys.post(e.data.post_id) });
  void qc.invalidateQueries({ queryKey: forumKeys.comments(e.data.post_id) });
  if (e.data.target_kind === "post") void qc.invalidateQueries({ queryKey: forumKeys.posts() });
});

registerRealtimeHandler("forum.post_updated", (qc, e) => {
  void qc.invalidateQueries({ queryKey: forumKeys.post(e.data.post_id) });
});

registerReconnectRefetch(forumKeys.all());

/** Subscribe to one post's realtime topic (new comments, votes, status) while mounted. */
export function useForumPostTopic(postId: string | null | undefined): void {
  useTopic(postId ? forumPostTopic(postId) : null);
}

// ──────────────────────────────────────────────────────────────── reads ──

/** `GET /v1/forum/communities`. */
export function useForumCommunities(options?: QueryOverrides<{ items: ForumCommunity[] }>): UseQueryResult<{ items: ForumCommunity[] }, ApiError> {
  const api = useEndpoints(forumEndpoints);
  return useApiQuery(forumKeys.communities(), ({ signal }) => api.communities({ signal }), { staleTime: 60_000, ...options });
}

/** `GET /v1/forum/communities/{slug}`. Pass null to disable. */
export function useForumCommunity(slug: string | null | undefined, options?: QueryOverrides<ForumCommunityDetail>): UseQueryResult<ForumCommunityDetail, ApiError> {
  const api = useEndpoints(forumEndpoints);
  return useApiQuery(forumKeys.community(slug ?? ""), ({ signal }) => api.community(slug ?? "", { signal }), options, !!slug);
}

/** `GET /v1/forum/posts` as an infinite list. */
export function useForumPosts(params: ForumPostListParams = {}, options?: InfiniteOverrides<ForumPostSummary>): PagedQueryResult<ForumPostSummary> {
  const api = useEndpoints(forumEndpoints);
  return usePagedQuery<ForumPostSummary>(forumKeys.postList(listKeyParams(params)), (cursor, signal) => api.posts({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** `GET /v1/forum/posts/{id}`. Pass null to disable. */
export function useForumPost(id: string | null | undefined, options?: QueryOverrides<ForumPost>): UseQueryResult<ForumPost, ApiError> {
  const api = useEndpoints(forumEndpoints);
  return useApiQuery(forumKeys.post(id ?? ""), ({ signal }) => api.post(id ?? "", { signal }), options, !!id);
}

/** `GET /v1/forum/posts/{id}/comments`: top-level comments with replies, as an infinite list. */
export function useForumComments(postId: string | null | undefined, sort: ForumCommentSort = "top", options?: InfiniteOverrides<ForumComment>): PagedQueryResult<ForumComment> {
  const api = useEndpoints(forumEndpoints);
  return usePagedQuery<ForumComment>(forumKeys.commentList(postId ?? "", sort), (cursor, signal) => api.comments(postId ?? "", { sort, cursor }, { signal }), options, !!postId);
}

/** `GET /v1/forum/comments/{id}/replies`: pass the id once "more replies" is opened. */
export function useForumReplies(commentId: string | null | undefined, options?: InfiniteOverrides<ForumComment>): PagedQueryResult<ForumComment> {
  const api = useEndpoints(forumEndpoints);
  return usePagedQuery<ForumComment>(forumKeys.replies(commentId ?? ""), (cursor, signal) => api.replies(commentId ?? "", { cursor }, { signal }), options, !!commentId);
}

/** Mention suggestions for what follows "@" (debounced 200 ms; an empty query asks nothing). */
export function useForumPeople(query: string): UseQueryResult<{ items: ForumAuthor[] }, ApiError> {
  const api = useEndpoints(forumEndpoints);
  const q = useDebouncedValue(query.trim(), 200);
  return useApiQuery(forumKeys.people(q), ({ signal }) => api.people(q, { signal }), { staleTime: 30_000 }, q.length > 0);
}

/** `GET /v1/forum/karma` (yours without a user id). */
export function useForumKarma(userId?: string, options?: QueryOverrides<ForumKarma>): UseQueryResult<ForumKarma, ApiError> {
  const api = useEndpoints(forumEndpoints);
  return useApiQuery(forumKeys.karma(userId ?? "me"), ({ signal }) => api.karma(userId, { signal }), options);
}

// ─────────────────────────────────────────────────────────────── writes ──

/** `POST /v1/forum/posts`. `result.moderation.status` "held" means it waits for review. */
export function useCreateForumPost(): UseMutationResult<ForumPostResult, ApiError, CreateForumPostRequest> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumPostResult, ApiError, CreateForumPostRequest>({
    mutationFn: (body) => api.createPost(body),
    onSuccess: (res) => qc.setQueryData(forumKeys.post(res.post.id), res.post),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: forumKeys.posts() }), qc.invalidateQueries({ queryKey: ["moderation", "content"] })]),
  });
}

/** `PATCH /v1/forum/posts/{id}`. */
export function useUpdateForumPost(): UseMutationResult<ForumPostResult, ApiError, { id: string } & UpdateForumPostRequest> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumPostResult, ApiError, { id: string } & UpdateForumPostRequest>({
    mutationFn: ({ id, ...body }) => api.updatePost(id, body),
    onSuccess: (res) => {
      patchForumPost(qc, res.post.id, (p) => ({ ...p, title: res.post.title, excerpt: res.post.excerpt, status: res.post.status, labels: res.post.labels, edited_at: res.post.edited_at }));
      qc.setQueryData(forumKeys.post(res.post.id), res.post);
    },
  });
}

/** `DELETE /v1/forum/posts/{id}`. */
export function useDeleteForumPost(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.deletePost(id),
    onSettled: (_r, _e, id) => Promise.all([qc.invalidateQueries({ queryKey: forumKeys.posts() }), qc.invalidateQueries({ queryKey: forumKeys.post(id) })]),
  });
}

/** `POST /v1/forum/posts/{id}/moderate`. */
export function useModerateForumPost(): UseMutationResult<ForumPost, ApiError, { id: string; action: ForumPostModAction; note?: string }> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumPost, ApiError, { id: string; action: ForumPostModAction; note?: string }>({
    mutationFn: ({ id, action, note }) => api.moderatePost(id, action, note),
    onSuccess: (post) => qc.setQueryData(forumKeys.post(post.id), post),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: forumKeys.posts() }), qc.invalidateQueries({ queryKey: ["forum", "community"] })]),
  });
}

/** `POST /v1/forum/posts/{id}/comments`. */
export function useCreateForumComment(): UseMutationResult<ForumCommentResult, ApiError, { postId: string } & CreateForumCommentRequest> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumCommentResult, ApiError, { postId: string } & CreateForumCommentRequest>({
    mutationFn: ({ postId, ...body }) => api.createComment(postId, body),
    onSettled: (_r, _e, v) =>
      Promise.all([
        qc.invalidateQueries({ queryKey: forumKeys.comments(v.postId) }),
        v.parent_id ? qc.invalidateQueries({ queryKey: forumKeys.replies(v.parent_id) }) : Promise.resolve(),
        qc.invalidateQueries({ queryKey: forumKeys.post(v.postId) }),
      ]),
  });
}

/** `PATCH /v1/forum/comments/{id}`. */
export function useUpdateForumComment(): UseMutationResult<ForumCommentResult, ApiError, { id: string; postId: string } & UpdateForumCommentRequest> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumCommentResult, ApiError, { id: string; postId: string } & UpdateForumCommentRequest>({
    mutationFn: ({ id, body, mentions }) => api.updateComment(id, { body, ...(mentions ? { mentions } : {}) }),
    onSuccess: (res, v) =>
      patchForumComment(qc, v.postId, v.id, (c) => ({
        ...c,
        body: res.comment.body,
        status: res.comment.status,
        labels: res.comment.labels,
        mentions: res.comment.mentions,
        moderation_reasons: res.comment.moderation_reasons,
        edited_at: res.comment.edited_at,
      })),
  });
}

/** `DELETE /v1/forum/comments/{id}`. */
export function useDeleteForumComment(): UseMutationResult<void, ApiError, { id: string; postId: string }> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; postId: string }>({
    mutationFn: ({ id }) => api.deleteComment(id),
    onSettled: (_r, _e, v) => Promise.all([qc.invalidateQueries({ queryKey: forumKeys.comments(v.postId) }), qc.invalidateQueries({ queryKey: forumKeys.post(v.postId) })]),
  });
}

/** `POST /v1/forum/comments/{id}/moderate`. */
export function useModerateForumComment(): UseMutationResult<ForumComment, ApiError, { id: string; postId: string; action: ForumCommentModAction; note?: string }> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumComment, ApiError, { id: string; postId: string; action: ForumCommentModAction; note?: string }>({
    mutationFn: ({ id, action, note }) => api.moderateComment(id, action, note),
    onSettled: (_r, _e, v) => Promise.all([qc.invalidateQueries({ queryKey: forumKeys.comments(v.postId) }), qc.invalidateQueries({ queryKey: forumKeys.post(v.postId) })]),
  });
}

/** Variables of `useForumVote`: the target, and the post it belongs to (the post itself for post votes). */
export interface ForumVoteVariables extends ForumVoteRequest {
  postId: string;
}

/** `PUT /v1/forum/votes`, optimistic on every cached copy. Value 0 clears your vote. */
export function useForumVote(): UseMutationResult<ForumVoteResult, ApiError, ForumVoteVariables, { before: ForumVoteValue }> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  const apply = (v: ForumVoteVariables, value: ForumVoteValue, score?: number) => {
    const set = <T extends Voted>(x: T): T => ({ ...x, score: score ?? x.score - x.viewer_vote + value, viewer_vote: value });
    if (v.target_kind === "post") patchForumPost(qc, v.target_id, set);
    else patchForumComment(qc, v.postId, v.target_id, set);
  };
  return useMutation<ForumVoteResult, ApiError, ForumVoteVariables, { before: ForumVoteValue }>({
    mutationFn: ({ target_kind, target_id, value }) => api.vote({ target_kind, target_id, value }),
    onMutate: (v) => {
      const before = cachedVote(qc, v.target_kind, v.target_id, v.postId);
      apply(v, v.value);
      return { before };
    },
    onError: (_e, v, ctx) => {
      if (ctx) apply(v, ctx.before);
    },
    onSuccess: (res, v) => apply(v, res.viewer_vote, res.score),
  });
}

// ──────────────────────────────────────────────────────────────── admin ──

/** `GET /v1/admin/forum/communities`. */
export function useForumAdminCommunities(options?: QueryOverrides<{ items: ForumAdminCommunity[] }>): UseQueryResult<{ items: ForumAdminCommunity[] }, ApiError> {
  const api = useEndpoints(forumEndpoints);
  return useApiQuery(forumKeys.admin.communities(), ({ signal }) => api.admin.communities({ signal }), options);
}

function invalidateCommunities(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: forumKeys.admin.communities() }),
    qc.invalidateQueries({ queryKey: forumKeys.communities() }),
    qc.invalidateQueries({ queryKey: ["forum", "community"] }),
    qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
  ]);
}

/** `POST /v1/admin/forum/communities`. */
export function useCreateForumCommunity(): UseMutationResult<ForumCommunity, ApiError, ForumCommunityInput> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumCommunity, ApiError, ForumCommunityInput>({ mutationFn: (body) => api.admin.createCommunity(body), onSettled: () => invalidateCommunities(qc) });
}

/** `PATCH /v1/admin/forum/communities/{id}`. */
export function useUpdateForumCommunity(): UseMutationResult<ForumCommunity, ApiError, { id: string } & ForumCommunityInput> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumCommunity, ApiError, { id: string } & ForumCommunityInput>({
    mutationFn: ({ id, ...body }) => api.admin.updateCommunity(id, body),
    onSettled: () => invalidateCommunities(qc),
  });
}

/** `POST /v1/admin/forum/communities/{id}/moderators` by email. */
export function useAddForumModerator(): UseMutationResult<ForumAuthor, ApiError, { id: string; email: string }> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<ForumAuthor, ApiError, { id: string; email: string }>({ mutationFn: ({ id, email }) => api.admin.addModerator(id, email), onSettled: () => invalidateCommunities(qc) });
}

/** `DELETE /v1/admin/forum/communities/{id}/moderators/{user_id}`. */
export function useRemoveForumModerator(): UseMutationResult<void, ApiError, { id: string; userId: string }> {
  const api = useEndpoints(forumEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; userId: string }>({ mutationFn: ({ id, userId }) => api.admin.removeModerator(id, userId), onSettled: () => invalidateCommunities(qc) });
}
