import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminFeedListParams,
  CreateFeedCommentRequest,
  CreateFeedPostRequest,
  FeedAuthor,
  FeedComment,
  FeedListParams,
  FeedPost,
  FeedReaction,
  FeedReactionSummary,
  FeedUnread,
  UpdateFeedCommentRequest,
  UpdateFeedPostRequest,
} from "./types";

/** Upload purposes for post attachments (`POST /v1/uploads`). */
export const FEED_MEDIA_PURPOSE = "feed_media";
export const FEED_FILE_PURPOSE = "feed_file";

/** Content types each purpose accepts (mirrors the backend). */
export const FEED_MEDIA_TYPES = ["image/jpeg", "image/png", "image/heic", "image/webp", "image/gif", "video/mp4", "video/quicktime"] as const;
export const FEED_FILE_TYPES = [
  "application/pdf",
  "text/plain",
  "text/csv",
  "application/zip",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
] as const;

/** Defaults of FEED_MEDIA_MAX_MB / FEED_FILE_MAX_MB / FEED_POST_MAX_CHARS / FEED_COMMENT_MAX_CHARS. */
export const FEED_LIMITS = { mediaBytes: 50 * 1024 * 1024, fileBytes: 25 * 1024 * 1024, postChars: 5000, commentChars: 2000, attachments: 10 } as const;

/** Feed endpoints (`/v1/feed`, `/v1/admin/feed`). */
export function feedEndpoints(http: HttpClient) {
  const F = "/v1/feed";
  const A = "/v1/admin/feed";
  return {
    /** `GET /v1/feed/posts`: newest first; without q/author it leaves out pinned announcements. */
    posts: (params: FeedListParams = {}, o?: CallOptions) =>
      http.get<Page<FeedPost>>(`${F}/posts`, { ...o, query: { q: params.q, author_id: params.author_id, limit: params.limit, cursor: params.cursor } }),
    /** `GET /v1/feed/pinned`: up to 3 active announcements. */
    pinned: (o?: CallOptions) => http.get<{ items: FeedPost[] }>(`${F}/pinned`, o),
    post: (id: string, o?: CallOptions) => http.get<FeedPost>(`${F}/posts/${seg(id)}`, o),
    createPost: (body: CreateFeedPostRequest, o?: CallOptions) => http.post<FeedPost>(`${F}/posts`, body, o),
    updatePost: (id: string, body: UpdateFeedPostRequest, o?: CallOptions) => http.patch<FeedPost>(`${F}/posts/${seg(id)}`, body, o),
    deletePost: (id: string, o?: CallOptions) => http.delete<void>(`${F}/posts/${seg(id)}`, o),
    /** `GET /v1/feed/posts/{id}/comments`: flat, oldest first; thread by `parent_id`. */
    comments: (postId: string, params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<FeedComment>>(`${F}/posts/${seg(postId)}/comments`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    createComment: (postId: string, body: CreateFeedCommentRequest, o?: CallOptions) => http.post<FeedComment>(`${F}/posts/${seg(postId)}/comments`, body, o),
    updateComment: (id: string, body: UpdateFeedCommentRequest, o?: CallOptions) => http.patch<FeedComment>(`${F}/comments/${seg(id)}`, body, o),
    deleteComment: (id: string, o?: CallOptions) => http.delete<void>(`${F}/comments/${seg(id)}`, o),
    reactPost: (id: string, reaction: FeedReaction, o?: CallOptions) => http.put<FeedReactionSummary>(`${F}/posts/${seg(id)}/reaction`, { reaction }, o),
    unreactPost: (id: string, o?: CallOptions) => http.delete<FeedReactionSummary>(`${F}/posts/${seg(id)}/reaction`, o),
    reactComment: (id: string, reaction: FeedReaction, o?: CallOptions) => http.put<FeedReactionSummary>(`${F}/comments/${seg(id)}/reaction`, { reaction }, o),
    unreactComment: (id: string, o?: CallOptions) => http.delete<FeedReactionSummary>(`${F}/comments/${seg(id)}/reaction`, o),
    unread: (o?: CallOptions) => http.get<FeedUnread>(`${F}/unread`, o),
    markSeen: (o?: CallOptions) => http.post<void>(`${F}/seen`, undefined, o),
    /** `GET /v1/feed/people?q=`: up to 8 mention suggestions. */
    people: (q: string, o?: CallOptions) => http.get<{ items: FeedAuthor[] }>(`${F}/people`, { ...o, query: { q } }),

    admin: {
      posts: (params: AdminFeedListParams = {}, o?: CallOptions) =>
        http.get<Page<FeedPost>>(`${A}/posts`, {
          ...o,
          query: { q: params.q, author_id: params.author_id, kind: params.kind, status: params.status, limit: params.limit, cursor: params.cursor },
        }),
      post: (id: string, o?: CallOptions) => http.get<FeedPost>(`${A}/posts/${seg(id)}`, o),
      /** Change or end an announcement's pin (`null` unpins). Audited. */
      pin: (id: string, pinned_until: string | null, o?: CallOptions) => http.patch<FeedPost>(`${A}/posts/${seg(id)}`, { pinned_until }, o),
      /** Soft-remove (hidden from everyone, kept for the record). Audited. */
      removePost: (id: string, reason?: string, o?: CallOptions) => http.request<void>("DELETE", `${A}/posts/${seg(id)}`, { ...o, body: { reason: reason ?? "" } }),
      removeComment: (id: string, reason?: string, o?: CallOptions) => http.request<void>("DELETE", `${A}/comments/${seg(id)}`, { ...o, body: { reason: reason ?? "" } }),
      createAnnouncement: (body: Omit<CreateFeedPostRequest, "kind">, o?: CallOptions) => http.post<FeedPost>(`${A}/announcements`, body, o),
    },
  };
}

export type FeedEndpoints = ReturnType<typeof feedEndpoints>;
