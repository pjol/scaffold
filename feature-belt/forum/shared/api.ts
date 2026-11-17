import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  CreateForumCommentRequest,
  CreateForumPostRequest,
  ForumAdminCommunity,
  ForumAuthor,
  ForumComment,
  ForumCommentModAction,
  ForumCommentResult,
  ForumCommentSort,
  ForumCommunity,
  ForumCommunityDetail,
  ForumCommunityInput,
  ForumKarma,
  ForumPost,
  ForumPostListParams,
  ForumPostModAction,
  ForumPostResult,
  ForumPostSummary,
  ForumVoteRequest,
  ForumVoteResult,
  UpdateForumCommentRequest,
  UpdateForumPostRequest,
} from "./types";

/** Forum endpoints: communities, posts, comments, votes; communities and moderators for staff. */
export function forumEndpoints(http: HttpClient) {
  const F = "/v1/forum";
  const A = "/v1/admin/forum";
  return {
    /** `GET /v1/forum/communities`, the most active first. */
    communities: (o?: CallOptions) => http.get<{ items: ForumCommunity[] }>(`${F}/communities`, o),
    /** `GET /v1/forum/communities/{slug}` with moderators and pinned posts. */
    community: (slug: string, o?: CallOptions) => http.get<ForumCommunityDetail>(`${F}/communities/${seg(slug)}`, o),
    /** `GET /v1/forum/posts`: hot, new or top; keyset pages. */
    posts: (params: ForumPostListParams & { cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<Page<ForumPostSummary>>(`${F}/posts`, {
        ...o,
        query: { community: params.community, sort: params.sort, t: params.t, author_id: params.author_id, q: params.q, limit: params.limit, cursor: params.cursor },
      }),
    /** `GET /v1/forum/posts/{id}`. */
    post: (id: string, o?: CallOptions) => http.get<ForumPost>(`${F}/posts/${seg(id)}`, o),
    /** `POST /v1/forum/posts`: screened; a held post comes back with status "held". */
    createPost: (body: CreateForumPostRequest, o?: CallOptions) => http.post<ForumPostResult>(`${F}/posts`, body, o),
    /** `PATCH /v1/forum/posts/{id}` (author; re-screened). */
    updatePost: (id: string, body: UpdateForumPostRequest, o?: CallOptions) => http.patch<ForumPostResult>(`${F}/posts/${seg(id)}`, body, o),
    /** `DELETE /v1/forum/posts/{id}` (author). */
    deletePost: (id: string, o?: CallOptions) => http.delete<void>(`${F}/posts/${seg(id)}`, o),
    /** `POST /v1/forum/posts/{id}/moderate` (staff and community moderators; restore is staff only). */
    moderatePost: (id: string, action: ForumPostModAction, note?: string, o?: CallOptions) => http.post<ForumPost>(`${F}/posts/${seg(id)}/moderate`, { action, note }, o),
    /** `GET /v1/forum/posts/{id}/comments`: top-level comments with replies, keyset pages. */
    comments: (postId: string, params: { sort?: ForumCommentSort; cursor?: string | undefined; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<ForumComment>>(`${F}/posts/${seg(postId)}/comments`, { ...o, query: { sort: params.sort, cursor: params.cursor, limit: params.limit } }),
    /** `GET /v1/forum/comments/{id}/replies`: one comment's replies, oldest first. */
    replies: (commentId: string, params: { cursor?: string | undefined; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<ForumComment>>(`${F}/comments/${seg(commentId)}/replies`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    /** `POST /v1/forum/posts/{id}/comments`: screened. */
    createComment: (postId: string, body: CreateForumCommentRequest, o?: CallOptions) => http.post<ForumCommentResult>(`${F}/posts/${seg(postId)}/comments`, body, o),
    /** `PATCH /v1/forum/comments/{id}` (author; re-screened). */
    updateComment: (id: string, body: UpdateForumCommentRequest, o?: CallOptions) => http.patch<ForumCommentResult>(`${F}/comments/${seg(id)}`, body, o),
    /** `DELETE /v1/forum/comments/{id}` (author). */
    deleteComment: (id: string, o?: CallOptions) => http.delete<void>(`${F}/comments/${seg(id)}`, o),
    /** `POST /v1/forum/comments/{id}/moderate`. */
    moderateComment: (id: string, action: ForumCommentModAction, note?: string, o?: CallOptions) => http.post<ForumComment>(`${F}/comments/${seg(id)}/moderate`, { action, note }, o),
    /** `PUT /v1/forum/votes` (idempotent; 0 clears). */
    vote: (body: ForumVoteRequest, o?: CallOptions) => http.put<ForumVoteResult>(`${F}/votes`, body, o),
    /** `GET /v1/forum/people?q=`: mention suggestions. */
    people: (q: string, o?: CallOptions) => http.get<{ items: ForumAuthor[] }>(`${F}/people`, { ...o, query: { q } }),
    /** `GET /v1/forum/karma` (yours without a user id). */
    karma: (userId?: string, o?: CallOptions) => http.get<ForumKarma>(`${F}/karma`, { ...o, query: { user_id: userId } }),
    admin: {
      /** `GET /v1/admin/forum/communities`. */
      communities: (o?: CallOptions) => http.get<{ items: ForumAdminCommunity[] }>(`${A}/communities`, o),
      /** `POST /v1/admin/forum/communities`. Audited. */
      createCommunity: (body: ForumCommunityInput, o?: CallOptions) => http.post<ForumCommunity>(`${A}/communities`, body, o),
      /** `PATCH /v1/admin/forum/communities/{id}`. Audited. */
      updateCommunity: (id: string, body: ForumCommunityInput, o?: CallOptions) => http.patch<ForumCommunity>(`${A}/communities/${seg(id)}`, body, o),
      /** `POST /v1/admin/forum/communities/{id}/moderators` by email. Audited. */
      addModerator: (id: string, email: string, o?: CallOptions) => http.post<ForumAuthor>(`${A}/communities/${seg(id)}/moderators`, { email }, o),
      /** `DELETE /v1/admin/forum/communities/{id}/moderators/{user_id}`. Audited. */
      removeModerator: (id: string, userId: string, o?: CallOptions) => http.delete<void>(`${A}/communities/${seg(id)}/moderators/${seg(userId)}`, o),
    },
  };
}

export type ForumEndpoints = ReturnType<typeof forumEndpoints>;
