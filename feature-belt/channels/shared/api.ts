import type { CallOptions, HttpClient } from "../api/http";
import type { PageParams } from "../api/params";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  ChannelsAdminChannel,
  ChannelsAttachmentUrl,
  ChannelsBrowseChannel,
  ChannelsConfig,
  ChannelsCreateRequest,
  ChannelsDetail,
  ChannelsEditRequest,
  ChannelsFile,
  ChannelsGroup,
  ChannelsLaterItem,
  ChannelsMember,
  ChannelsMessage,
  ChannelsMessageContext,
  ChannelsMessagePage,
  ChannelsPerson,
  ChannelsPrefsRequest,
  ChannelsSearchHit,
  ChannelsSendRequest,
  ChannelsThread,
  ChannelsThreadSummary,
  ChannelsUnread,
  ChannelsUpdateRequest,
  ChannelsWindowParams,
  ChannelsWorkspace,
} from "./types";

const windowQuery = (p: ChannelsWindowParams = {}) => ({ before: p.before, after: p.after, around: p.around, limit: p.limit });

/** `/v1/channels` endpoints (signed in) and `/v1/admin/channels` (staff). */
export function channelsEndpoints(http: HttpClient) {
  const C = "/v1/channels";
  const org = (id: string) => `${C}/orgs/${seg(id)}`;
  const ch = (id: string) => `${C}/channels/${seg(id)}`;
  const msg = (id: string) => `${C}/messages/${seg(id)}`;
  return {
    config: (o?: CallOptions) => http.get<ChannelsConfig>(`${C}/config`, o),
    /** Badge totals across your orgs. */
    unread: (o?: CallOptions) => http.get<ChannelsUnread>(`${C}/unread`, o),
    /** The sidebar; the first load joins you to the org's default channels. */
    workspace: (orgId: string, o?: CallOptions) => http.get<ChannelsWorkspace>(`${org(orgId)}/workspace`, o),
    browse: (orgId: string, params: PageParams & { q?: string; archived?: boolean } = {}, o?: CallOptions) =>
      http.get<Page<ChannelsBrowseChannel>>(`${org(orgId)}/browse`, { ...o, query: { q: params.q || undefined, archived: params.archived ? "true" : undefined, cursor: params.cursor, limit: params.limit } }),
    create: (orgId: string, body: ChannelsCreateRequest, o?: CallOptions) => http.post<ChannelsDetail>(`${org(orgId)}/channels`, body, o),
    /** 200 with the existing conversation for these people, 201 new. Empty = a note to yourself. */
    openDM: (orgId: string, userIds: string[], o?: CallOptions) => http.post<ChannelsDetail>(`${org(orgId)}/dms`, { user_ids: userIds }, o),
    people: (orgId: string, q: string, channelId?: string, o?: CallOptions) =>
      http.get<Page<ChannelsPerson>>(`${org(orgId)}/people`, { ...o, query: { q: q || undefined, channel_id: channelId } }),
    groups: (orgId: string, q: string, o?: CallOptions) => http.get<Page<ChannelsGroup>>(`${org(orgId)}/groups`, { ...o, query: { q: q || undefined } }),
    threads: (orgId: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<ChannelsThreadSummary>>(`${org(orgId)}/threads`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    later: (orgId: string, params: PageParams & { state?: "open" | "done" } = {}, o?: CallOptions) =>
      http.get<Page<ChannelsLaterItem>>(`${org(orgId)}/later`, { ...o, query: { state: params.state, cursor: params.cursor, limit: params.limit } }),
    /** `q` may carry in:#channel, from:@person, from:me, has:file, is:thread. */
    search: (orgId: string, params: PageParams & { q: string; channel_id?: string }, o?: CallOptions) =>
      http.get<Page<ChannelsSearchHit>>(`${org(orgId)}/search`, { ...o, query: { q: params.q, channel_id: params.channel_id, cursor: params.cursor, limit: params.limit } }),

    channel: (id: string, o?: CallOptions) => http.get<ChannelsDetail>(ch(id), o),
    update: (id: string, body: ChannelsUpdateRequest, o?: CallOptions) => http.patch<ChannelsDetail>(ch(id), body, o),
    archive: (id: string, o?: CallOptions) => http.post<ChannelsDetail>(`${ch(id)}/archive`, undefined, o),
    unarchive: (id: string, o?: CallOptions) => http.delete<ChannelsDetail>(`${ch(id)}/archive`, o),
    join: (id: string, o?: CallOptions) => http.post<ChannelsDetail>(`${ch(id)}/join`, undefined, o),
    /** Leave a channel, or close a DM in your sidebar. */
    leave: (id: string, o?: CallOptions) => http.delete<void>(`${ch(id)}/membership`, o),
    members: (id: string, params: PageParams & { q?: string } = {}, o?: CallOptions) =>
      http.get<Page<ChannelsMember>>(`${ch(id)}/members`, { ...o, query: { q: params.q || undefined, cursor: params.cursor, limit: params.limit } }),
    addMembers: (id: string, userIds: string[], o?: CallOptions) => http.post<ChannelsDetail>(`${ch(id)}/members`, { user_ids: userIds }, o),
    removeMember: (id: string, userId: string, o?: CallOptions) => http.delete<void>(`${ch(id)}/members/${seg(userId)}`, o),
    setPrefs: (id: string, body: ChannelsPrefsRequest, o?: CallOptions) => http.put<ChannelsDetail>(`${ch(id)}/prefs`, body, o),
    markRead: (id: string, messageId?: string, o?: CallOptions) => http.post<void>(`${ch(id)}/read`, messageId ? { message_id: messageId } : {}, o),
    typing: (id: string, threadRootId?: string | null, o?: CallOptions) => http.post<void>(`${ch(id)}/typing`, threadRootId ? { thread_root_id: threadRootId } : {}, o),
    messages: (id: string, params: ChannelsWindowParams = {}, o?: CallOptions) => http.get<ChannelsMessagePage>(`${ch(id)}/messages`, { ...o, query: windowQuery(params) }),
    /** Forum posts, latest activity first. */
    topics: (id: string, params: PageParams & { tag?: string } = {}, o?: CallOptions) =>
      http.get<Page<ChannelsMessage>>(`${ch(id)}/topics`, { ...o, query: { tag: params.tag || undefined, cursor: params.cursor, limit: params.limit } }),
    /** 201 new; 200 with the original when `client_id` was already used. */
    send: (id: string, body: ChannelsSendRequest, o?: CallOptions) => http.post<ChannelsMessage>(`${ch(id)}/messages`, body, o),
    pins: (id: string, o?: CallOptions) => http.get<{ items: ChannelsMessage[] }>(`${ch(id)}/pins`, o),
    files: (id: string, params: PageParams = {}, o?: CallOptions) => http.get<Page<ChannelsFile>>(`${ch(id)}/files`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    /** Start or join the channel's call; resolves to the app path to open. */
    startCall: (id: string, o?: CallOptions) => http.post<{ path: string }>(`${ch(id)}/call`, undefined, o),

    message: (id: string, o?: CallOptions) => http.get<ChannelsMessageContext>(msg(id), o),
    thread: (id: string, params: ChannelsWindowParams = {}, o?: CallOptions) => http.get<ChannelsThread>(`${msg(id)}/thread`, { ...o, query: windowQuery(params) }),
    markThreadRead: (rootId: string, messageId?: string, o?: CallOptions) => http.post<void>(`${msg(rootId)}/thread/read`, messageId ? { message_id: messageId } : {}, o),
    follow: (rootId: string, o?: CallOptions) => http.put<{ following: boolean }>(`${msg(rootId)}/follow`, undefined, o),
    unfollow: (rootId: string, o?: CallOptions) => http.delete<{ following: boolean }>(`${msg(rootId)}/follow`, o),
    edit: (id: string, body: ChannelsEditRequest, o?: CallOptions) => http.patch<ChannelsMessage>(msg(id), body, o),
    remove: (id: string, o?: CallOptions) => http.delete<void>(msg(id), o),
    react: (id: string, emoji: string, o?: CallOptions) => http.post<ChannelsMessage>(`${msg(id)}/reactions`, { emoji }, o),
    unreact: (id: string, emoji: string, o?: CallOptions) => http.delete<ChannelsMessage>(`${msg(id)}/reactions`, { ...o, query: { emoji } }),
    pin: (id: string, o?: CallOptions) => http.put<ChannelsMessage>(`${msg(id)}/pin`, undefined, o),
    unpin: (id: string, o?: CallOptions) => http.delete<ChannelsMessage>(`${msg(id)}/pin`, o),
    /** Save for later, or mark a saved item done. */
    saveLater: (id: string, done = false, o?: CallOptions) => http.put<void>(`${msg(id)}/later`, { done }, o),
    unsaveLater: (id: string, o?: CallOptions) => http.delete<void>(`${msg(id)}/later`, o),
    attachmentUrl: (attachmentId: string, o?: CallOptions) => http.get<ChannelsAttachmentUrl>(`${C}/attachments/${seg(attachmentId)}`, o),

    admin: {
      /** Staff view with hidden content; each call is audited. */
      channel: (id: string, params: ChannelsWindowParams & { thread?: string } = {}, o?: CallOptions) =>
        http.get<ChannelsAdminChannel>(`/v1/admin/channels/channels/${seg(id)}`, { ...o, query: { ...windowQuery(params), thread: params.thread } }),
      /** Hides a message for members; audited. */
      removeMessage: (messageId: string, reason: string, o?: CallOptions) => http.request<void>("DELETE", `/v1/admin/channels/messages/${seg(messageId)}`, { ...o, body: { reason } }),
    },
  };
}

export type ChannelsEndpoints = ReturnType<typeof channelsEndpoints>;
