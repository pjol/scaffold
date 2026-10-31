import type { CallOptions, HttpClient } from "../api/http";
import type { PageParams } from "../api/params";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  ChatAdminConversation,
  ChatAttachmentUrl,
  ChatConversation,
  ChatConversationDetail,
  ChatCreateConversationRequest,
  ChatMessage,
  ChatMessagePage,
  ChatMuteRequest,
  ChatRole,
  ChatSearchHit,
  ChatSendMessageRequest,
  ChatUnread,
  ChatUpdateConversationRequest,
  ChatUser,
  ChatWindowParams,
} from "./types";

export interface ChatInboxParams extends PageParams {
  archived?: boolean;
}

export interface ChatSearchParams extends PageParams {
  q: string;
  conversation_id?: string;
}

/** `/v1/chat` endpoints (signed in) and `/v1/admin/chat` (staff). */
export function chatEndpoints(http: HttpClient) {
  const C = "/v1/chat";
  const conv = (id: string) => `${C}/conversations/${seg(id)}`;
  const msg = (id: string) => `${C}/messages/${seg(id)}`;
  return {
    /** Inbox, newest activity first. */
    inbox: (params: ChatInboxParams = {}, o?: CallOptions) =>
      http.get<Page<ChatConversation>>(`${C}/conversations`, { ...o, query: { archived: params.archived ? "true" : undefined, limit: params.limit, cursor: params.cursor } }),
    /** Direct: 200 with the existing conversation or 201 new. Group: 201. */
    create: (body: ChatCreateConversationRequest, o?: CallOptions) => http.post<ChatConversationDetail>(`${C}/conversations`, body, o),
    conversation: (id: string, o?: CallOptions) => http.get<ChatConversationDetail>(conv(id), o),
    /** Group name and picture (owner or admin). */
    update: (id: string, body: ChatUpdateConversationRequest, o?: CallOptions) => http.patch<ChatConversationDetail>(conv(id), body, o),
    /** Leave a group; delete a direct conversation for me. */
    leave: (id: string, o?: CallOptions) => http.delete<void>(conv(id), o),
    addMembers: (id: string, userIds: string[], o?: CallOptions) => http.post<ChatConversationDetail>(`${conv(id)}/members`, { user_ids: userIds }, o),
    /** `owner` transfers ownership (owner only). */
    setRole: (id: string, userId: string, role: ChatRole, o?: CallOptions) => http.patch<ChatConversationDetail>(`${conv(id)}/members/${seg(userId)}`, { role }, o),
    /** Remove someone, or leave when `userId` is the caller. */
    removeMember: (id: string, userId: string, o?: CallOptions) => http.delete<void>(`${conv(id)}/members/${seg(userId)}`, o),
    messages: (id: string, params: ChatWindowParams = {}, o?: CallOptions) =>
      http.get<ChatMessagePage>(`${conv(id)}/messages`, { ...o, query: { before: params.before, after: params.after, around: params.around, limit: params.limit } }),
    /** 201 new; 200 with the original when `client_id` was already used here. */
    send: (id: string, body: ChatSendMessageRequest, o?: CallOptions) => http.post<ChatMessage>(`${conv(id)}/messages`, body, o),
    /** Moves the read pointer forward to `messageId` (default: the newest message). */
    markRead: (id: string, messageId?: string, o?: CallOptions) => http.post<void>(`${conv(id)}/read`, messageId ? { message_id: messageId } : {}, o),
    mute: (id: string, body: ChatMuteRequest, o?: CallOptions) => http.put<void>(`${conv(id)}/mute`, body, o),
    unmute: (id: string, o?: CallOptions) => http.delete<void>(`${conv(id)}/mute`, o),
    archive: (id: string, o?: CallOptions) => http.post<void>(`${conv(id)}/archive`, undefined, o),
    unarchive: (id: string, o?: CallOptions) => http.delete<void>(`${conv(id)}/archive`, o),
    typing: (id: string, o?: CallOptions) => http.post<void>(`${conv(id)}/typing`, undefined, o),
    edit: (messageId: string, body: string, o?: CallOptions) => http.patch<ChatMessage>(msg(messageId), { body }, o),
    remove: (messageId: string, o?: CallOptions) => http.delete<void>(msg(messageId), o),
    react: (messageId: string, emoji: string, o?: CallOptions) => http.post<ChatMessage>(`${msg(messageId)}/reactions`, { emoji }, o),
    unreact: (messageId: string, emoji: string, o?: CallOptions) => http.delete<ChatMessage>(`${msg(messageId)}/reactions`, { ...o, query: { emoji } }),
    attachmentUrl: (attachmentId: string, o?: CallOptions) => http.get<ChatAttachmentUrl>(`${C}/attachments/${seg(attachmentId)}`, o),
    unread: (o?: CallOptions) => http.get<ChatUnread>(`${C}/unread`, o),
    search: (params: ChatSearchParams, o?: CallOptions) =>
      http.get<Page<ChatSearchHit>>(`${C}/search`, { ...o, query: { q: params.q, conversation_id: params.conversation_id, limit: params.limit, cursor: params.cursor } }),
    /** People to message: by name or exact email; recent contacts when `q` is empty. */
    people: (q: string, o?: CallOptions) => http.get<Page<ChatUser>>(`${C}/people`, { ...o, query: { q: q || undefined } }),
    admin: {
      /** Staff view with hidden content; each call is audited. */
      conversation: (id: string, params: ChatWindowParams = {}, o?: CallOptions) =>
        http.get<ChatAdminConversation>(`/v1/admin/chat/conversations/${seg(id)}`, { ...o, query: { before: params.before, after: params.after, around: params.around, limit: params.limit } }),
      /** Hides a message for members; audited. */
      removeMessage: (messageId: string, reason: string, o?: CallOptions) => http.request<void>("DELETE", `/v1/admin/chat/messages/${seg(messageId)}`, { ...o, body: { reason } }),
    },
  };
}

export type ChatEndpoints = ReturnType<typeof chatEndpoints>;
