/** Feed DTOs. snake_case, mirroring backend/internal/feed (change both together). */

export type FeedPostKind = "post" | "announcement";

/** The fixed reaction set: one per person per post or comment, changeable. */
export type FeedReaction = "like" | "love" | "laugh" | "wow" | "sad";
export const FEED_REACTIONS: readonly FeedReaction[] = ["like", "love", "laugh", "wow", "sad"];

/** Counts per reaction; only non-zero entries are present. */
export type FeedReactionCounts = Partial<Record<FeedReaction, number>>;

export interface FeedAuthor {
  id: string;
  display_name: string;
  avatar_url: string | null;
}

/** An attachment: a photo, a video (purpose `feed_media`) or a document (purpose `feed_file`). */
export interface FeedMedia {
  id: string;
  kind: "image" | "video" | "file";
  /** Storage key; send it back when editing to keep the attachment. */
  key: string;
  url: string;
  content_type: string;
  width: number | null;
  height: number | null;
  /** Documents: the file name to show. */
  name: string | null;
}

/** A resolved @mention. `start`/`length` are UTF-16 offsets (JavaScript string indexes) of the "@Name" token. */
export interface FeedMention {
  user_id: string;
  display_name: string;
  start: number;
  length: number;
}

export interface FeedPost {
  id: string;
  kind: FeedPostKind;
  /** "all" or "<kind>:<uuid>" (see feed.Scope). */
  audience: string;
  author: FeedAuthor;
  body: string;
  link_url: string | null;
  media: FeedMedia[];
  mentions: FeedMention[];
  reactions: FeedReactionCounts;
  reaction_count: number;
  my_reaction: FeedReaction | null;
  comment_count: number;
  /** Announcements: shown above the feed until then. */
  pinned_until: string | null;
  edited_at: string | null;
  created_at: string;
  /** Staff views only. */
  removed_at?: string | null;
}

/** Anything but `visible` is a placeholder that keeps replies in place (blank body and author). */
export type FeedCommentStatus = "visible" | "deleted" | "removed" | "hidden";

export interface FeedComment {
  id: string;
  post_id: string;
  /** Set on replies (one level). */
  parent_id: string | null;
  author: FeedAuthor | null;
  body: string;
  mentions: FeedMention[];
  reactions: FeedReactionCounts;
  reaction_count: number;
  my_reaction: FeedReaction | null;
  status: FeedCommentStatus;
  edited_at: string | null;
  created_at: string;
}

/** Response to a reaction change, and the `feed.reaction` realtime payload (without `my_reaction`). */
export interface FeedReactionSummary {
  target_kind: "post" | "comment";
  target_id: string;
  post_id: string;
  reactions: FeedReactionCounts;
  reaction_count: number;
  my_reaction?: FeedReaction | null;
}

export interface FeedUnread {
  /** Capped at 100. */
  unread_count: number;
  has_unread: boolean;
}

// ───────────────────────────────────────────────────────────── requests ──

export interface FeedMediaInput {
  key: string;
  width?: number | null;
  height?: number | null;
  /** Documents: the original file name. */
  name?: string | null;
}

export interface FeedMentionInput {
  user_id: string;
  start: number;
  length: number;
}

export interface CreateFeedPostRequest {
  /** `announcement` is staff only. */
  kind?: FeedPostKind;
  audience?: string;
  body: string;
  link_url?: string | null;
  media?: FeedMediaInput[];
  mentions?: FeedMentionInput[];
  /** Announcements only; at most 90 days ahead. */
  pinned_until?: string | null;
}

/** `PATCH /v1/feed/posts/{id}`: mentions are replaced whenever `body` is sent; `media` replaces the whole list. */
export interface UpdateFeedPostRequest {
  body?: string;
  /** "" clears the link. */
  link_url?: string;
  media?: FeedMediaInput[];
  mentions?: FeedMentionInput[];
}

export interface CreateFeedCommentRequest {
  body: string;
  /** Reply to this comment; a reply to a reply attaches to its parent. */
  parent_id?: string | null;
  mentions?: FeedMentionInput[];
}

export interface UpdateFeedCommentRequest {
  body: string;
  mentions?: FeedMentionInput[];
}

export interface FeedListParams {
  q?: string;
  author_id?: string;
  limit?: number;
  cursor?: string;
}

export interface AdminFeedListParams {
  /** Words in the text, or a post id. */
  q?: string;
  author_id?: string;
  kind?: FeedPostKind;
  status?: "visible" | "removed";
  limit?: number;
  cursor?: string;
}

// ───────────────────────────────────────────────────────────── realtime ──

/** Topic for the "all" audience. Other audiences use `feedAudienceTopic`. */
export const FEED_ALL_TOPIC = "feed:00000000-0000-0000-0000-000000000000";

/** `feed.post`: a new post. Payloads carry ids and counts; fetch the post through the API. */
export interface FeedPostEvent {
  post_id: string;
  author_id: string;
  kind: FeedPostKind;
  audience: string;
  created_at: string;
}

/** `feed.post_updated`: edited, pinned, removed or deleted. */
export interface FeedPostUpdatedEvent {
  post_id: string;
  deleted: boolean;
}

/** `feed.comment`: a comment was added, edited or deleted. */
export interface FeedCommentEvent {
  post_id: string;
  comment_id: string;
  parent_id?: string | null;
  author_id?: string;
  comment_count: number;
  deleted: boolean;
}

declare module "../types" {
  interface RealtimeEventMap {
    "feed.post": FeedPostEvent;
    "feed.post_updated": FeedPostUpdatedEvent;
    "feed.comment": FeedCommentEvent;
    "feed.reaction": FeedReactionSummary;
  }
}
