import type { ModerationOutcome } from "../moderation";

/** A person as the forum shows them. */
export interface ForumAuthor {
  id: string;
  display_name: string;
  avatar_url: string | null;
}

/** `GET /v1/forum/communities` item. */
export interface ForumCommunity {
  id: string;
  slug: string;
  title: string;
  description: string;
  rules: string[];
  /** Only moderators post. */
  locked: boolean;
  /** The app object it belongs to (`forum.EnsureCommunity`), if any. */
  ref_kind: string | null;
  ref_id: string | null;
  posts_7d: number;
  created_at: string;
}

/** `GET /v1/forum/communities/{slug}`. */
export interface ForumCommunityDetail extends ForumCommunity {
  moderators: ForumAuthor[];
  pinned: ForumPostSummary[];
  /** Staff or a moderator of this community. */
  can_moderate: boolean;
}

export type ForumContentStatus = "visible" | "held" | "removed" | "deleted";
export type ForumPostSort = "hot" | "new" | "top";
export type ForumTopPeriod = "day" | "week" | "month" | "year" | "all";
export type ForumCommentSort = "top" | "new" | "old";
export type ForumVoteValue = -1 | 0 | 1;

/** A post in a list. */
export interface ForumPostSummary {
  id: string;
  community: { id: string; slug: string; title: string };
  /** null once deleted. */
  author: ForumAuthor | null;
  title: string;
  kind: "text" | "link";
  url: string | null;
  domain: string | null;
  excerpt: string;
  score: number;
  comment_count: number;
  viewer_vote: ForumVoteValue;
  /** Moderation's (`off_topic`) and the app's own (see FORUM_LABELS). */
  labels: string[];
  status: ForumContentStatus;
  pinned: boolean;
  locked: boolean;
  created_at: string;
  edited_at: string | null;
}

/** `GET /v1/forum/posts/{id}`. */
export interface ForumPost extends ForumPostSummary {
  /** Markdown. */
  body: string;
  mentions: ForumAuthor[];
  /** Why a held or removed post isn't public (author and staff only). */
  moderation_reasons: string[];
  can_moderate: boolean;
}

/** A comment with its replies. Placeholders (deleted, removed, blocked) have no author or body. */
export interface ForumComment {
  id: string;
  post_id: string;
  parent_id: string | null;
  depth: number;
  author: ForumAuthor | null;
  body: string;
  score: number;
  viewer_vote: ForumVoteValue;
  labels: string[];
  status: ForumContentStatus;
  moderation_reasons: string[];
  mentions: ForumAuthor[];
  created_at: string;
  edited_at: string | null;
  replies: ForumComment[];
  /** Replies not included: `GET /v1/forum/comments/{id}/replies` pages them. */
  more_replies: number;
}

export interface ForumPostListParams {
  /** Community slug; omit for every community. */
  community?: string | undefined;
  sort?: ForumPostSort | undefined;
  t?: ForumTopPeriod | undefined;
  author_id?: string | undefined;
  q?: string | undefined;
  limit?: number;
}

/** `POST /v1/forum/posts`. A link post sets `url`. */
export interface CreateForumPostRequest {
  community: string;
  title: string;
  body?: string;
  url?: string;
  /** People named as "@Name" in the text (ids), at most 10. */
  mentions?: string[];
}

export interface UpdateForumPostRequest {
  title?: string;
  body?: string;
  mentions?: string[];
}

/** `POST /v1/forum/posts/{id}/comments`. */
export interface CreateForumCommentRequest {
  body: string;
  parent_id?: string | null;
  mentions?: string[];
}

export interface UpdateForumCommentRequest {
  body: string;
  mentions?: string[];
}

/** What a write returns: the item and what screening decided. */
export interface ForumPostResult {
  post: ForumPost;
  moderation: ModerationOutcome;
}

export interface ForumCommentResult {
  comment: ForumComment;
  moderation: ModerationOutcome;
}

/** `PUT /v1/forum/votes`. */
export interface ForumVoteRequest {
  target_kind: "post" | "comment";
  target_id: string;
  value: ForumVoteValue;
}

export interface ForumVoteResult {
  score: number;
  viewer_vote: ForumVoteValue;
}

export type ForumPostModAction = "pin" | "unpin" | "lock" | "reopen" | "remove" | "restore";
export type ForumCommentModAction = "remove" | "restore";

/** `GET /v1/forum/karma`. */
export interface ForumKarma {
  user_id: string;
  karma: number;
  karma_90d: number;
}

/** A community as staff manage it (`GET /v1/admin/forum/communities`). */
export interface ForumAdminCommunity extends ForumCommunity {
  moderators: ForumAuthor[];
  post_count: number;
}

/** `POST /v1/admin/forum/communities` and `PATCH .../{id}`. */
export interface ForumCommunityInput {
  slug?: string;
  title?: string;
  description?: string;
  rules?: string[];
  locked?: boolean;
}

/** Labels clients show as badges; unknown labels read as words. Add the app's own (forum.SetLabeler) here. */
export const FORUM_LABELS: Record<string, string> = {
  off_topic: "Off-topic",
};

/** Limits the server enforces (FORUM_POST_MAX_CHARS, FORUM_COMMENT_MAX_CHARS defaults). */
export const FORUM_LIMITS = { titleMin: 3, titleMax: 300, postChars: 10000, commentChars: 5000, mentions: 10, maxDepth: 8 } as const;

/** Realtime topic of one post. */
export function forumPostTopic(postId: string): string {
  return `forum:${postId}`;
}

declare module "../types" {
  interface RealtimeEventMap {
    "forum.comment": { post_id: string; comment_id: string; parent_id: string | null; comment_count: number };
    "forum.vote": { post_id: string; target_kind: "post" | "comment"; target_id: string; score: number };
    "forum.status": { post_id: string; target_kind: "post" | "comment"; target_id: string; status: ForumContentStatus };
    "forum.post_updated": { post_id: string };
  }
}
