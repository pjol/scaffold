/**
 * Channels DTOs, snake_case, matching `backend/internal/channels` (types.go, workspace.go,
 * channel.go, messages.go, threads.go, search.go, fanout.go, admin.go) exactly. Change both
 * sides together.
 */

/** A person as channels show them. A deleted account reads "Deleted user". */
export interface ChannelsUser {
  id: string;
  display_name: string;
  avatar_url: string | null;
  deleted: boolean;
}

export type ChannelsKind = "public" | "private" | "dm" | "group_dm";
export type ChannelsLayout = "chat" | "forum";
export type ChannelsNotify = "default" | "all" | "mentions" | "nothing";
export type ChannelsRole = "admin" | "member";
export type ChannelsMessageKind = "text" | "system" | "bot";
export type ChannelsSystemEvent = "created" | "joined" | "left" | "added" | "removed" | "renamed" | "archived" | "unarchived" | "topic" | "converted";
export type ChannelsAttachmentKind = "image" | "video" | "audio" | "file";

/** A file on a message. `url` is signed and short-lived; refresh with `attachmentUrl`. */
export interface ChannelsAttachment {
  id: string;
  kind: ChannelsAttachmentKind;
  content_type: string;
  file_name: string;
  size_bytes: number | null;
  width: number | null;
  height: number | null;
  url: string;
  url_expires_at: string;
}

export interface ChannelsReaction {
  emoji: string;
  count: number;
  /** Oldest first. */
  user_ids: string[];
}

export interface ChannelsMessage {
  id: string;
  channel_id: string;
  kind: ChannelsMessageKind;
  /** Null for bot messages. */
  sender: ChannelsUser | null;
  bot_name: string | null;
  /** Markdown with mention tokens; "" once deleted. Render with `parseChannelsMarkdown`. */
  body: string;
  event: ChannelsSystemEvent | null;
  subject: ChannelsUser | null;
  thread_root_id: string | null;
  also_in_channel: boolean;
  /** Forum posts. */
  title: string | null;
  tags: string[];
  /** People the body's `<@id>` tokens name, with current names. */
  mentions: ChannelsUser[];
  reply_count: number;
  last_reply_at: string | null;
  /** Up to 4 most recent repliers. */
  reply_users: ChannelsUser[];
  attachments: ChannelsAttachment[];
  reactions: ChannelsReaction[];
  pinned: boolean;
  /** In your Later list. */
  saved: boolean;
  client_id: string | null;
  edited_at: string | null;
  deleted: boolean;
  created_at: string;
  /** Staff view only. */
  deleted_by?: "sender" | "moderator" | "staff" | "account_deleted" | null;
}

/** A message in the client cache: server messages plus optimistic sends. */
export interface ChannelsCachedMessage extends ChannelsMessage {
  status?: "sending" | "failed";
  pending?: ChannelsSendInput;
}

/** A window of a timeline or a thread, oldest first. */
export interface ChannelsMessagePage {
  items: ChannelsMessage[];
  has_older: boolean;
  has_newer: boolean;
}

export interface ChannelsReadMark {
  message_id: string;
  at: string;
}

/** One channel or DM in your sidebar. */
export interface ChannelsSidebarChannel {
  id: string;
  org_id: string;
  kind: ChannelsKind;
  layout: ChannelsLayout;
  /** Channel handle, or the DM's people ("You" for a note to yourself). */
  name: string;
  topic: string;
  is_default: boolean;
  archived: boolean;
  /** DMs: the other people (up to 8). */
  people: ChannelsUser[];
  member_count: number;
  /** Timeline messages after your read pointer (capped at 100). */
  unread: number;
  /** Of those, ones that mention you (every message in a DM). */
  mentions: number;
  muted: boolean;
  notify: ChannelsNotify;
  starred: boolean;
  last_read_at: string | null;
  last_message_at: string;
}

export interface ChannelsConfig {
  max_chars: number;
  /** 0 = no limit. */
  edit_window_seconds: number;
  attachment_max_bytes: number;
  max_attachments: number;
  max_group_dm: number;
  /** The app wired SetCallStarter: show "Start call". */
  calls_enabled: boolean;
  /** The app wired SetGroupResolver: offer @groups. */
  groups_enabled: boolean;
}

/** `GET /v1/channels/orgs/{orgID}/workspace`: everything the sidebar shows. */
export interface ChannelsWorkspace {
  org_id: string;
  role: string;
  channels: ChannelsSidebarChannel[];
  permissions: { create_public: boolean; create_private: boolean; set_default: boolean };
  threads_unread: number;
  later_count: number;
  config: ChannelsConfig;
}

/** `GET /v1/channels/unread`: badge totals across your orgs. */
export interface ChannelsUnread {
  mentions: number;
  unread_channels: number;
  orgs: { org_id: string; unread_channels: number; mentions: number }[];
}

export interface ChannelsBrowseChannel {
  id: string;
  kind: ChannelsKind;
  layout: ChannelsLayout;
  name: string;
  topic: string;
  description: string;
  is_default: boolean;
  archived: boolean;
  member_count: number;
  joined: boolean;
}

export interface ChannelsPermissions {
  post: boolean;
  edit: boolean;
  archive: boolean;
  remove_members: boolean;
  mention_all: boolean;
  set_default: boolean;
}

/** `GET /v1/channels/channels/{id}`. */
export interface ChannelsDetail extends ChannelsSidebarChannel {
  description: string;
  forum_tags: string[];
  created_by: ChannelsUser | null;
  created_at: string;
  /** False: previewing a public channel. */
  member: boolean;
  role: ChannelsRole | "";
  pinned_count: number;
  permissions: ChannelsPermissions;
  /** A block either way with you: drop their live events. */
  hidden_user_ids: string[];
}

export interface ChannelsMember {
  user: ChannelsUser;
  role: ChannelsRole;
  joined_at: string;
}

/** An org member in pickers and mention autocomplete. */
export interface ChannelsPerson extends ChannelsUser {
  /** With `channel_id`: already in that channel. */
  in_channel: boolean;
}

export interface ChannelsGroup {
  id: string;
  handle: string;
  name: string;
  member_count: number;
}

export interface ChannelsFile extends ChannelsAttachment {
  message_id: string;
  thread_root_id: string | null;
  sender: ChannelsUser | null;
  created_at: string;
}

/** Names a channel in links, search hits and Later items. */
export interface ChannelsRef {
  id: string;
  org_id: string;
  kind: ChannelsKind;
  layout: ChannelsLayout;
  name: string;
}

export interface ChannelsMessageContext {
  message: ChannelsMessage;
  channel: ChannelsRef;
}

/** `GET /v1/channels/messages/{id}/thread`. */
export interface ChannelsThread {
  root: ChannelsMessage;
  replies: ChannelsMessagePage;
  channel: ChannelsRef;
  following: boolean;
  last_read: ChannelsReadMark | null;
}

export interface ChannelsThreadSummary {
  root: ChannelsMessage;
  channel: ChannelsRef;
  unread: number;
  last_read: ChannelsReadMark | null;
  /** Up to 2, oldest first. */
  latest_replies: ChannelsMessage[];
}

export interface ChannelsLaterItem {
  message: ChannelsMessage;
  channel: ChannelsRef;
  saved_at: string;
  completed_at: string | null;
}

export interface ChannelsSearchHit {
  message: ChannelsMessage;
  channel: ChannelsRef;
}

export interface ChannelsAttachmentUrl {
  url: string;
  expires_at: string;
}

// ───────────────────────────────────────────────────────────── requests ──

export interface ChannelsCreateRequest {
  name: string;
  kind: "public" | "private";
  layout?: ChannelsLayout;
  topic?: string;
  description?: string;
  forum_tags?: string[];
  member_ids?: string[];
  is_default?: boolean;
}

export interface ChannelsUpdateRequest {
  name?: string;
  topic?: string;
  description?: string;
  forum_tags?: string[];
  /** "private" converts a public channel (one way). */
  kind?: "private";
  is_default?: boolean;
}

export interface ChannelsPrefsRequest {
  notify?: ChannelsNotify;
  muted?: boolean;
  starred?: boolean;
}

export interface ChannelsAttachmentInput {
  /** From `useUpload` with purpose `channel_attachment`. */
  key: string;
  file_name?: string;
  size_bytes?: number;
  width?: number;
  height?: number;
}

export interface ChannelsSendRequest {
  client_id?: string;
  body?: string;
  thread_root_id?: string;
  also_send_to_channel?: boolean;
  /** Forum channels: required for a new post. */
  title?: string;
  tags?: string[];
  attachments?: ChannelsAttachmentInput[];
}

/** What `useChannelsSend` takes: the request plus what the optimistic message shows. */
export interface ChannelsSendInput {
  body: string;
  thread_root_id?: string | null;
  also_send_to_channel?: boolean;
  title?: string;
  tags?: string[];
  attachments?: (ChannelsAttachmentInput & { preview_url?: string; content_type?: string })[];
  /** Reuse to resend a failed message; generated when omitted. */
  client_id?: string;
}

export interface ChannelsEditRequest {
  body?: string;
  title?: string;
  tags?: string[];
}

/** `before`/`after`/`around` take a message id (one at a time). */
export interface ChannelsWindowParams {
  before?: string;
  after?: string;
  around?: string;
  limit?: number;
}

// ───────────────────────────────────────────────────────────── realtime ──

export interface ChannelsChannelEvent {
  channel_id: string;
  reason: "updated" | "members" | "pins";
  user_id?: string;
}

export interface ChannelsSidebarMessage {
  id: string;
  sender_id: string | null;
  created_at: string;
  thread_root_id: string | null;
  also_in_channel: boolean;
}

export interface ChannelsSidebarEvent {
  org_id: string;
  channel_id: string;
  reason: "message" | "added" | "removed" | "updated" | "read" | "later";
  message?: ChannelsSidebarMessage;
  mention: boolean;
}

export interface ChannelsThreadEvent {
  org_id: string;
  channel_id: string;
  root_id: string;
  reason: "reply" | "read" | "follow";
  message?: ChannelsSidebarMessage;
  mention: boolean;
}

export interface ChannelsTypingEvent {
  channel_id: string;
  thread_root_id: string | null;
  user: ChannelsUser;
}

// ───────────────────────────────────────────────────────────── staff ──

/** `GET /v1/admin/channels/channels/{id}`: members and messages, hidden ones readable; audited. */
export interface ChannelsAdminChannel {
  id: string;
  org_id: string;
  org_name: string;
  kind: ChannelsKind;
  layout: ChannelsLayout;
  name: string;
  archived: boolean;
  created_at: string;
  members: ChannelsUser[];
  messages: ChannelsMessagePage;
  thread: ChannelsMessagePage | null;
}

declare module "../types" {
  interface RealtimeEventMap {
    "channels.message": ChannelsMessage;
    "channels.message_updated": ChannelsMessage;
    "channels.typing": ChannelsTypingEvent;
    "channels.channel": ChannelsChannelEvent;
    "channels.sidebar": ChannelsSidebarEvent;
    "channels.thread": ChannelsThreadEvent;
  }
}
