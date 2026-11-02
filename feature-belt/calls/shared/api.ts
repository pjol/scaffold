import type { CallOptions, HttpClient } from "../api/http";
import type { PageParams } from "../api/params";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import { callsTimeZone } from "./format";
import type {
  CallsCallDetail,
  CallsCreateMeetingRequest,
  CallsGuestKnock,
  CallsHistoryItem,
  CallsJoinInfo,
  CallsJoinToken,
  CallsLobbyEntry,
  CallsMeeting,
  CallsMeetingDetail,
  CallsMeetingProviders,
  CallsPlatformMeetings,
  CallsPublicMeeting,
  CallsRing,
  CallsStartCallRequest,
  CallsUpdateMeetingRequest,
  CallsUser,
} from "./types";

/** Who to admit or deny: some lobby entries, or everyone waiting. */
export type CallsLobbyTarget = { ids: string[] } | { all: true };
/** Who to mute: one participant identity, or everyone but the hosts. */
export type CallsMuteTarget = { identity: string } | { all: true };

/** `/v1/calls` (signed in) and `/v1/calls/public` (guests, no token). */
export function callsEndpoints(http: HttpClient) {
  const C = "/v1/calls";
  const mtg = (id: string) => `${C}/meetings/${seg(id)}`;
  const pub = (id: string) => `${C}/public/lobby/${seg(id)}`;
  return {
    /** Calls you were in and calls you missed, newest first. */
    history: (params: PageParams = {}, o?: CallOptions) => http.get<Page<CallsHistoryItem>>(`${C}/history`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** One call (a session) with everyone's time in it. */
    call: (id: string, o?: CallOptions) => http.get<CallsCallDetail>(`${C}/history/${seg(id)}`, o),
    /** Scheduled meetings you host or are invited to, soonest first. */
    /** Meeting providers open to the caller: the app's own call, connected Zoom and Teams accounts. */
    providers: (orgId?: string | null, o?: CallOptions) => http.get<CallsMeetingProviders>(`${C}/providers`, { ...o, query: { org_id: orgId ?? undefined } }),
    /** Meetings with platform links on the caller's calendars: the next 14 days, or the last 14 ("recent"). */
    platformMeetings: (when: "upcoming" | "recent", orgId?: string | null, o?: CallOptions) =>
      http.get<CallsPlatformMeetings>(`${C}/external`, { ...o, query: { when, org_id: orgId ?? undefined }, timeoutMs: 30_000 }),
    upcoming: (params: PageParams = {}, o?: CallOptions) => http.get<Page<CallsMeeting>>(`${C}/upcoming`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** Unscheduled meeting links you made, newest first. */
    links: (params: PageParams = {}, o?: CallOptions) => http.get<Page<CallsMeeting>>(`${C}/links`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** Calls ringing for you now. */
    rings: (o?: CallOptions) => http.get<Page<CallsRing>>(`${C}/rings`, o),
    /** People you may call or invite (shared organizations by default). */
    people: (q: string, orgId?: string | null, o?: CallOptions) => http.get<Page<CallsUser>>(`${C}/people`, { ...o, query: { q: q || undefined, org_id: orgId ?? undefined } }),
    /** By code, link or id: can I join, and the meeting if I can. */
    joinInfo: (ref: string, o?: CallOptions) => http.get<CallsJoinInfo>(mtg(ref), o),
    /** Sends Calls' zone with the body (`setCallsTimeZone`, else the device's): invitations word times in it for people whose zone isn't known. */
    create: (body: CallsCreateMeetingRequest, o?: CallOptions) => http.post<CallsMeetingDetail>(`${C}/meetings`, { time_zone: callsTimeZone(), ...body }, o),
    update: (id: string, body: CallsUpdateMeetingRequest, o?: CallOptions) => http.patch<CallsMeetingDetail>(mtg(id), body, o),
    /** Cancel (the host): the link stops working. */
    cancel: (id: string, o?: CallOptions) => http.delete<void>(mtg(id), o),
    addInvitees: (id: string, userIds: string[], role: "invitee" | "cohost" = "invitee", o?: CallOptions) =>
      http.post<CallsMeetingDetail>(`${mtg(id)}/invitees`, { user_ids: userIds, role, time_zone: callsTimeZone() }, o),
    setInviteeRole: (id: string, userId: string, role: "invitee" | "cohost", o?: CallOptions) =>
      http.patch<CallsMeetingDetail>(`${mtg(id)}/invitees/${seg(userId)}`, { role }, o),
    removeInvitee: (id: string, userId: string, o?: CallOptions) => http.delete<void>(`${mtg(id)}/invitees/${seg(userId)}`, o),
    /** A room token (403 `calls_knock_required` when you must ask first). */
    join: (id: string, o?: CallOptions) => http.post<CallsJoinToken>(`${mtg(id)}/join`, undefined, o),
    knock: (id: string, o?: CallOptions) => http.post<CallsLobbyEntry>(`${mtg(id)}/knock`, undefined, o),
    knockStatus: (lobbyId: string, o?: CallOptions) => http.get<CallsLobbyEntry>(`${C}/lobby/${seg(lobbyId)}`, o),
    cancelKnock: (lobbyId: string, o?: CallOptions) => http.delete<void>(`${C}/lobby/${seg(lobbyId)}`, o),
    /** People waiting to join (hosts). */
    lobby: (id: string, o?: CallOptions) => http.get<Page<CallsLobbyEntry>>(`${mtg(id)}/lobby`, o),
    admit: (id: string, target: CallsLobbyTarget, o?: CallOptions) => http.post<Page<CallsLobbyEntry>>(`${mtg(id)}/lobby/admit`, target, o),
    deny: (id: string, target: CallsLobbyTarget, o?: CallOptions) => http.post<Page<CallsLobbyEntry>>(`${mtg(id)}/lobby/deny`, target, o),
    mute: (id: string, target: CallsMuteTarget, o?: CallOptions) => http.post<{ muted: number }>(`${mtg(id)}/mute`, target, o),
    remove: (id: string, identity: string, o?: CallOptions) => http.post<void>(`${mtg(id)}/remove`, { identity }, o),
    /** End the call for everyone (hosts). */
    end: (id: string, o?: CallOptions) => http.post<void>(`${mtg(id)}/end`, undefined, o),
    /** Raise or lower your hand; hosts lower someone's with `identity`. */
    hand: (id: string, raised: boolean, identity?: string, o?: CallOptions) => http.post<void>(`${mtg(id)}/hand`, identity ? { raised, identity } : { raised }, o),
    /** Ring invitees again (or, for hosts, new people). No ids = everyone invited who isn't here. */
    ring: (id: string, userIds: string[] = [], o?: CallOptions) => http.post<void>(`${mtg(id)}/ring`, { user_ids: userIds }, o),
    stopRinging: (id: string, o?: CallOptions) => http.delete<void>(`${mtg(id)}/ring`, o),
    /** Rings on a meeting in the last 30 minutes, oldest first (people in it). */
    meetingRings: (id: string, o?: CallOptions) => http.get<Page<CallsRing>>(`${mtg(id)}/rings`, o),
    /** An instant call that rings `user_ids`. */
    startCall: (body: CallsStartCallRequest, o?: CallOptions) => http.post<CallsMeetingDetail>(`${C}/calls`, body, o),
    declineRing: (ringId: string, o?: CallOptions) => http.post<CallsRing>(`${C}/rings/${seg(ringId)}/decline`, undefined, o),
    acceptRing: (ringId: string, o?: CallOptions) => http.post<CallsRing>(`${C}/rings/${seg(ringId)}/accept`, undefined, o),
    /** Guests without an account (no token sent). */
    guest: {
      meeting: (code: string, o?: CallOptions) => http.get<CallsPublicMeeting>(`${C}/public/meetings/${seg(code)}`, { ...o, auth: false }),
      knock: (code: string, name: string, o?: CallOptions) => http.post<CallsGuestKnock>(`${C}/public/meetings/${seg(code)}/knock`, { name }, { ...o, auth: false }),
      status: (lobbyId: string, key: string, o?: CallOptions) => http.post<CallsLobbyEntry>(`${pub(lobbyId)}/status`, { key }, { ...o, auth: false }),
      cancel: (lobbyId: string, key: string, o?: CallOptions) => http.post<void>(`${pub(lobbyId)}/cancel`, { key }, { ...o, auth: false }),
      /**
       * Where a closing waiting page sends `{ key }` as a beacon (any content type): hosts stop
       * seeing the guest at once, and a check-in from a reload brings the knock back.
       */
      leaveUrl: (lobbyId: string) => `${http.baseUrl}${pub(lobbyId)}/leave`,
      token: (lobbyId: string, key: string, o?: CallOptions) => http.post<CallsJoinToken>(`${pub(lobbyId)}/token`, { key }, { ...o, auth: false }),
      hand: (lobbyId: string, key: string, raised: boolean, o?: CallOptions) => http.post<void>(`${pub(lobbyId)}/hand`, { key, raised }, { ...o, auth: false }),
    },
  };
}

export type CallsEndpoints = ReturnType<typeof callsEndpoints>;
