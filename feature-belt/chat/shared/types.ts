/**
 * Chat DTOs, snake_case, matching `backend/internal/chat` (types.go, conversations.go,
 * members.go, messages.go, fanout.go, admin.go) exactly. Change both sides together.
 */

/** A person as chat shows them. A deleted account reads "Deleted user". */
export interface ChatUser {
  id: string;
  display_name: string;
  avatar_url: string | null;
  deleted: boolean;
}

/** A read pointer: the newest message someone has seen and that message's time. */
export interface ChatReadMark {
  message_id: string;
  at: string;
}

export type ChatConversationKind = "direct" | "group";
export type ChatRole = "owner" | "admin" | "member";

/** One inbox row, from the caller's point of view. */
export interface ChatConversation {
  id: string;
  kind: ChatConversationKind;
  /** Group title, or the other person's name. */
  title: string;
  avatar_url: string | null;
  /** Direct only. */
  peer: ChatUser | null;
  /** Direct only: a block either way; sending is refused. */
  blocked: boolean;
  role: ChatRole;
  member_count: number;
  unread: number;
  muted: boolean;
  /** Null when not muted or muted until unmuted. */
  muted_until: string | null;
  archived: boolean;
  last_read: ChatReadMark | null;
  /** Every other member has read up to here ("Read" in the UI). */
  others_read: ChatReadMark | null;
  last_message: ChatMessage | null;
  last_message_at: string;
  created_at: string;
}

export interface ChatMember {
  user: ChatUser;
  role: ChatRole;
  joined_at: string;
  last_read: ChatReadMark | null;
}

/** `GET /v1/chat/conversations/{id}`. */
export interface ChatConversationDetail extends ChatConversation {
  members: ChatMember[];
  /** Members with a block either way with the caller: drop their live events. */
  hidden_user_ids: string[];
}

export type ChatMessageKind = "text" | "system";
export type ChatSystemEvent = "created" | "member_added" | "member_removed" | "member_left" | "title_changed" | "avatar_changed" | "owner_changed";
export type ChatAttachmentKind = "image" | "video" | "audio" | "file";

export interface ChatReplyPreview {
  id: string;
  sender: ChatUser;
  /** First 200 characters. */
  body: string;
  has_attachments: boolean;
  deleted: boolean;
}

/** A file on a message. `url` is signed and short-lived; refresh with `attachmentUrl`. */
export interface ChatAttachment {
  id: string;
  kind: ChatAttachmentKind;
  content_type: string;
  file_name: string;
  size_bytes: number | null;
  width: number | null;
  height: number | null;
  duration_ms: number | null;
  url: string;
  url_expires_at: string;
}

export interface ChatReaction {
  emoji: string;
  count: number;
  /** Oldest first. */
  user_ids: string[];
}

export interface ChatMessage {
  id: string;
  conversation_id: string;
  sender: ChatUser;
  kind: ChatMessageKind;
  /** Text messages; "" once deleted. */
  body: string;
  /** System messages. */
  event: ChatSystemEvent | null;
  /** System messages: the member the event is about. */
  subject: ChatUser | null;
  client_id: string | null;
  reply_to: ChatReplyPreview | null;
  attachments: ChatAttachment[];
  reactions: ChatReaction[];
  edited_at: string | null;
  deleted: boolean;
  created_at: string;
  /** Staff view only. */
  deleted_by?: "sender" | "staff" | "account_deleted" | null;
}

/** A message in the client cache: server messages, plus optimistic sends. */
export interface ChatThreadMessage extends ChatMessage {
  /** Set on optimistic sends until the server answers. */
  status?: "sending" | "failed";
  /** The request to resend after a failure. */
  pending?: ChatSendInput;
}

/** A window of a conversation, oldest first. */
export interface ChatMessagePage {
  items: ChatMessage[];
  has_older: boolean;
  has_newer: boolean;
}

/** `GET /v1/chat/unread`: muted and archived conversations excluded. */
export interface ChatUnread {
  conversations: number;
  messages: number;
}

export interface ChatConversationRef {
  id: string;
  kind: ChatConversationKind;
  title: string;
}

export interface ChatSearchHit {
  conversation: ChatConversationRef;
  message: ChatMessage;
}

export interface ChatAttachmentUrl {
  url: string;
  expires_at: string;
}

// ───────────────────────────────────────────────────────────── requests ──

export type ChatCreateConversationRequest =
  | { kind: "direct"; user_id: string }
  | { kind: "group"; title: string; member_ids: string[]; avatar_key?: string };

export interface ChatUpdateConversationRequest {
  title?: string;
  /** "" removes the picture. */
  avatar_key?: string;
}

export interface ChatAttachmentInput {
  /** From `useUpload` with purpose `chat_attachment`. */
  key: string;
  file_name?: string;
  size_bytes?: number;
  width?: number;
  height?: number;
  duration_ms?: number;
}

export interface ChatSendMessageRequest {
  client_id?: string;
  body?: string;
  reply_to_id?: string;
  attachments?: ChatAttachmentInput[];
}

export type ChatMuteRequest = { until: string } | { forever: true };

/** What `useSendMessage` takes: the request plus what the optimistic bubble shows. */
export interface ChatSendInput {
  body: string;
  reply_to?: ChatReplyPreview | null;
  attachments?: (ChatAttachmentInput & { preview_url?: string; content_type?: string })[];
  /** Reuse to resend a failed message; generated when omitted. */
  client_id?: string;
}

// ───────────────────────────────────────────────────────────── realtime ──

export interface ChatReadEvent {
  conversation_id: string;
  user_id: string;
  last_read: ChatReadMark;
}

export interface ChatTypingEvent {
  conversation_id: string;
  user: ChatUser;
}

export interface ChatMemberEvent {
  conversation_id: string;
  action: "added" | "removed" | "left" | "role";
  user_id: string;
  role: ChatRole;
  actor_id: string;
}

export interface ChatInboxEvent {
  conversation_id: string;
  reason: "message" | "message_updated" | "added" | "removed" | "read" | "updated";
  message?: ChatMessage;
}

// ───────────────────────────────────────────────────────────── staff ──

export interface ChatAdminMember {
  user: ChatUser;
  role: ChatRole;
  joined_at: string;
  left_at: string | null;
}

/** `GET /v1/admin/chat/conversations/{id}`: every member and message; audited. */
export interface ChatAdminConversation {
  id: string;
  kind: ChatConversationKind;
  title: string;
  avatar_url: string | null;
  created_by: ChatUser | null;
  created_at: string;
  last_message_at: string;
  members: ChatAdminMember[];
  messages: ChatMessagePage;
}

/** `before`/`after`/`around` take a message id (one at a time). */
export interface ChatWindowParams {
  before?: string;
  after?: string;
  around?: string;
  limit?: number;
}

declare module "../types" {
  interface RealtimeEventMap {
    "chat.message": ChatMessage;
    "chat.message_updated": ChatMessage;
    "chat.read": ChatReadEvent;
    "chat.typing": ChatTypingEvent;
    "chat.member": ChatMemberEvent;
    "chat.inbox": ChatInboxEvent;
  }
}
