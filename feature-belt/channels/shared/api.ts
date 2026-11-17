import type { CallOptions, HttpClient } from "../api/http";
import type { PageParams } from "../api/params";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  ChannelsActivityItem,
  ChannelsActivityKind,
  ChannelsAdminChannel,
  ChannelsBookmark,
  ChannelsCard,
  ChannelsCustomEmoji,
  ChannelsDraft,
  ChannelsEmojiList,
  ChannelsForwardRequest,
  ChannelsNewWebhook,
  ChannelsPollRequest,
  ChannelsProfileCard,
  ChannelsReminder,
  ChannelsReminderRequest,
  ChannelsScheduleRequest,
  ChannelsScheduled,
  ChannelsSentItem,
  ChannelsSidebarSection,
  ChannelsUnreadChannel,
  ChannelsUserPrefs,
  ChannelsUserPrefsRequest,
  ChannelsWebhook,
  ChannelsWorkspaceSettings,
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
    /** `q` may carry in:, from:, with:, to:me, has:file|link|pin|reaction, is:thread|saved, before:, after:, on:, during: (dates in `tz`). */
    search: (orgId: string, params: PageParams & { q: string; channel_id?: string; tz?: string }, o?: CallOptions) =>
      http.get<Page<ChannelsSearchHit>>(`${org(orgId)}/search`, { ...o, query: { q: params.q, channel_id: params.channel_id, tz: params.tz, cursor: params.cursor, limit: params.limit } }),
    activity: (orgId: string, params: PageParams & { filter?: ChannelsActivityKind } = {}, o?: CallOptions) =>
      http.get<Page<ChannelsActivityItem>>(`${org(orgId)}/activity`, { ...o, query: { filter: params.filter, cursor: params.cursor, limit: params.limit } }),
    markActivityRead: (orgId: string, o?: CallOptions) => http.post<void>(`${org(orgId)}/activity/read`, undefined, o),
    unreads: (orgId: string, o?: CallOptions) => http.get<{ items: ChannelsUnreadChannel[] }>(`${org(orgId)}/unreads`, o),
    drafts: (orgId: string, params: PageParams = {}, o?: CallOptions) => http.get<Page<ChannelsDraft>>(`${org(orgId)}/drafts`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    sent: (orgId: string, params: PageParams = {}, o?: CallOptions) => http.get<Page<ChannelsSentItem>>(`${org(orgId)}/sent`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    scheduledList: (orgId: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<ChannelsScheduled>>(`${org(orgId)}/scheduled`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    reminders: (orgId: string, params: PageParams & { state?: "open" | "done" } = {}, o?: CallOptions) =>
      http.get<Page<ChannelsReminder>>(`${org(orgId)}/reminders`, { ...o, query: { state: params.state, cursor: params.cursor, limit: params.limit } }),
    createReminder: (orgId: string, body: ChannelsReminderRequest, o?: CallOptions) => http.post<ChannelsReminder>(`${org(orgId)}/reminders`, body, o),
    updateReminder: (id: string, body: { at?: string; when?: string; tz?: string; done?: boolean }, o?: CallOptions) => http.patch<ChannelsReminder>(`${C}/reminders/${seg(id)}`, body, o),
    deleteReminder: (id: string, o?: CallOptions) => http.delete<void>(`${C}/reminders/${seg(id)}`, o),
    updatePrefs: (orgId: string, body: ChannelsUserPrefsRequest, o?: CallOptions) => http.put<ChannelsUserPrefs>(`${org(orgId)}/prefs`, body, o),
    createSection: (orgId: string, name: string, o?: CallOptions) => http.post<ChannelsSidebarSection>(`${org(orgId)}/sections`, { name }, o),
    updateSection: (id: string, body: { name?: string; position?: number; collapsed?: boolean }, o?: CallOptions) => http.patch<ChannelsSidebarSection>(`${C}/sections/${seg(id)}`, body, o),
    deleteSection: (id: string, o?: CallOptions) => http.delete<void>(`${C}/sections/${seg(id)}`, o),
    emoji: (orgId: string, o?: CallOptions) => http.get<ChannelsEmojiList>(`${org(orgId)}/emoji`, o),
    createEmoji: (orgId: string, body: { name: string; key?: string; alias_of?: string }, o?: CallOptions) => http.post<ChannelsCustomEmoji>(`${org(orgId)}/emoji`, body, o),
    deleteEmoji: (id: string, o?: CallOptions) => http.delete<void>(`${C}/emoji/${seg(id)}`, o),
    settings: (orgId: string, o?: CallOptions) => http.get<ChannelsWorkspaceSettings>(`${org(orgId)}/settings`, o),
    /** Owners and admins; `retention_days` 0 = keep forever. */
    updateSettings: (orgId: string, body: { unfurl_links?: boolean; retention_days?: number }, o?: CallOptions) => http.patch<ChannelsWorkspaceSettings>(`${org(orgId)}/settings`, body, o),
    profile: (orgId: string, userId: string, o?: CallOptions) => http.get<ChannelsProfileCard>(`${org(orgId)}/people/${seg(userId)}`, o),
    /** Your card for a link to the app; 404 when there is none or it isn't yours to see. */
    card: (url: string, o?: CallOptions) => http.get<ChannelsCard>(`${C}/card`, { ...o, query: { url } }),

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
    bookmarks: (id: string, o?: CallOptions) => http.get<{ items: ChannelsBookmark[] }>(`${ch(id)}/bookmarks`, o),
    createBookmark: (id: string, body: { url: string; title?: string }, o?: CallOptions) => http.post<ChannelsBookmark>(`${ch(id)}/bookmarks`, body, o),
    updateBookmark: (bookmarkId: string, body: { url?: string; title?: string; position?: number }, o?: CallOptions) => http.patch<ChannelsBookmark>(`${C}/bookmarks/${seg(bookmarkId)}`, body, o),
    deleteBookmark: (bookmarkId: string, o?: CallOptions) => http.delete<void>(`${C}/bookmarks/${seg(bookmarkId)}`, o),
    /** 204 (undefined) when there is none. */
    draft: (id: string, threadRootId?: string | null, o?: CallOptions) => http.get<ChannelsDraft | undefined>(`${ch(id)}/draft`, { ...o, query: { thread_root_id: threadRootId ?? undefined } }),
    /** An empty body deletes the draft. */
    saveDraft: (id: string, body: string, threadRootId?: string | null, o?: CallOptions) =>
      http.put<void>(`${ch(id)}/draft`, { body, ...(threadRootId ? { thread_root_id: threadRootId } : {}) }, o),
    deleteDraft: (id: string, threadRootId?: string | null, o?: CallOptions) => http.delete<void>(`${ch(id)}/draft`, { ...o, query: { thread_root_id: threadRootId ?? undefined } }),
    /** This message and everything after it become unread (a thread reply: the thread). */
    markUnread: (id: string, messageId: string, o?: CallOptions) => http.post<void>(`${ch(id)}/unread`, { message_id: messageId }, o),
    schedule: (id: string, body: ChannelsScheduleRequest, o?: CallOptions) => http.post<ChannelsScheduled>(`${ch(id)}/scheduled`, body, o),
    updateScheduled: (scheduledId: string, body: { body?: string; send_at?: string; when?: string; tz?: string }, o?: CallOptions) =>
      http.patch<ChannelsScheduled>(`${C}/scheduled/${seg(scheduledId)}`, body, o),
    deleteScheduled: (scheduledId: string, o?: CallOptions) => http.delete<void>(`${C}/scheduled/${seg(scheduledId)}`, o),
    sendScheduledNow: (scheduledId: string, o?: CallOptions) => http.post<ChannelsMessage>(`${C}/scheduled/${seg(scheduledId)}/send`, undefined, o),
    createPoll: (id: string, body: ChannelsPollRequest, o?: CallOptions) => http.post<ChannelsMessage>(`${ch(id)}/polls`, body, o),
    /** A group DM becomes a private channel. */
    convert: (id: string, name: string, o?: CallOptions) => http.post<ChannelsDetail>(`${ch(id)}/convert`, { name }, o),
    webhooks: (id: string, o?: CallOptions) => http.get<{ items: ChannelsWebhook[] }>(`${ch(id)}/webhooks`, o),
    createWebhook: (id: string, name: string, o?: CallOptions) => http.post<ChannelsNewWebhook>(`${ch(id)}/webhooks`, { name }, o),
    deleteWebhook: (webhookId: string, o?: CallOptions) => http.delete<void>(`${C}/webhooks/${seg(webhookId)}`, o),

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
    forward: (id: string, body: ChannelsForwardRequest, o?: CallOptions) => http.post<ChannelsMessage>(`${msg(id)}/forward`, body, o),
    /** Replaces your votes; [] clears them. */
    vote: (id: string, optionIds: string[], o?: CallOptions) => http.put<ChannelsMessage>(`${msg(id)}/vote`, { option_ids: optionIds }, o),
    closePoll: (id: string, o?: CallOptions) => http.post<ChannelsMessage>(`${msg(id)}/poll/close`, undefined, o),
    /** The author removes link previews. */
    hidePreviews: (id: string, o?: CallOptions) => http.delete<ChannelsMessage>(`${msg(id)}/previews`, o),

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
