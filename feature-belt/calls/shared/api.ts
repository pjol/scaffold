import type { CallOptions, HttpClient } from "../api/http";
import type { PageParams } from "../api/params";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import { callsTimeZone } from "./format";
import type {
  CallsBreakout,
  CallsChatMessage,
  CallsCreateBreakoutsRequest,
  CallsOrgSettings,
  CallsPoll,
  CallsPollInput,
  CallsPollUpdate,
  CallsQuestion,
  CallsRecording,
  CallsRoomState,
  CallsSessionSummary,
  CallsTranscript,
  CallsUpdateBreakoutsRequest,
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

/** Who acts in a call: a signed-in person (by meeting), or an admitted guest (their lobby pass). */
export interface CallsRoomTarget {
  meetingId: string;
  guest?: { lobbyId: string; key: string } | null;
}

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
    /** Spotlight someone for everyone (hosts); "" clears. */
    spotlight: (id: string, identity: string, o?: CallOptions) => http.put<void>(`${mtg(id)}/spotlight`, { identity }, o),
    /** Ask someone to unmute (hosts); while unmuting is off, this lets them. */
    askUnmute: (id: string, identity: string, o?: CallOptions) => http.post<void>(`${mtg(id)}/ask-unmute`, { identity }, o),
    startRecording: (id: string, o?: CallOptions) => http.post<CallsRecording>(`${mtg(id)}/recording`, undefined, o),
    stopRecording: (id: string, o?: CallOptions) => http.delete<CallsRecording>(`${mtg(id)}/recording`, o),
    /** Recordings of a meeting you may open (no playback links: fetch one). */
    meetingRecordings: (id: string, o?: CallOptions) => http.get<Page<CallsRecording>>(`${mtg(id)}/recordings`, o),
    /** One recording with playback and download links (one hour). */
    recording: (recordingId: string, o?: CallOptions) => http.get<CallsRecording>(`${C}/recordings/${seg(recordingId)}`, o),
    deleteRecording: (recordingId: string, o?: CallOptions) => http.delete<void>(`${C}/recordings/${seg(recordingId)}`, o),
    /** A meeting's calls you were in or may read the records of. */
    sessions: (id: string, params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<CallsSessionSummary>>(`${mtg(id)}/sessions`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    setTranscribing: (id: string, on: boolean, o?: CallOptions) => http.put<void>(`${mtg(id)}/transcribing`, { on }, o),
    transcript: (sessionId: string, o?: CallOptions) => http.get<CallsTranscript>(`${C}/history/${seg(sessionId)}/transcript`, o),
    savedChat: (sessionId: string, o?: CallOptions) => http.get<Page<CallsChatMessage>>(`${C}/history/${seg(sessionId)}/chat`, o),
    /** Attendance as CSV (hosts): fetch it with the session's token, then save the text. */
    attendanceCsv: (sessionId: string, o?: CallOptions) => http.text("GET", `${C}/history/${seg(sessionId)}/attendance.csv`, o),
    createPoll: (id: string, body: CallsPollInput, o?: CallOptions) => http.post<CallsPoll>(`${mtg(id)}/polls`, body, o),
    updatePoll: (id: string, pollId: string, body: CallsPollUpdate, o?: CallOptions) => http.patch<CallsPoll>(`${mtg(id)}/polls/${seg(pollId)}`, body, o),
    deletePoll: (id: string, pollId: string, o?: CallOptions) => http.delete<void>(`${mtg(id)}/polls/${seg(pollId)}`, o),
    moderateQuestion: (id: string, questionId: string, status: "open" | "answered" | "hidden", o?: CallOptions) =>
      http.patch<CallsQuestion>(`${mtg(id)}/questions/${seg(questionId)}`, { status }, o),
    /** The open breakout rooms with who is where (hosts). */
    breakouts: (id: string, o?: CallOptions) => http.get<CallsBreakout | null>(`${mtg(id)}/breakouts`, o),
    createBreakouts: (id: string, body: CallsCreateBreakoutsRequest, o?: CallOptions) => http.post<CallsBreakout>(`${mtg(id)}/breakouts`, body, o),
    updateBreakouts: (id: string, body: CallsUpdateBreakoutsRequest, o?: CallOptions) => http.patch<CallsBreakout>(`${mtg(id)}/breakouts`, body, o),
    broadcastBreakouts: (id: string, text: string, o?: CallOptions) => http.post<void>(`${mtg(id)}/breakouts/broadcast`, { text }, o),
    /** Close the rooms after a 30-second countdown (`now`: at once). */
    closeBreakouts: (id: string, now = false, o?: CallOptions) => http.post<void>(`${mtg(id)}/breakouts/close`, { now }, o),
    orgSettings: (orgId: string, o?: CallOptions) => http.get<CallsOrgSettings>(`${C}/orgs/${seg(orgId)}/settings`, o),
    updateOrgSettings: (orgId: string, body: Partial<Pick<CallsOrgSettings, "captions" | "transcripts" | "recording">>, o?: CallOptions) =>
      http.put<CallsOrgSettings>(`${C}/orgs/${seg(orgId)}/settings`, body, o),
    /**
     * In-call actions for a signed-in person or an admitted guest: the same server code
     * behind `/meetings/{id}/…` and `/public/lobby/{id}/…` (the guest's key in the body).
     */
    room: (t: CallsRoomTarget) => {
      const g = t.guest;
      const post = <T>(path: string, body: Record<string, unknown> = {}, o?: CallOptions) =>
        g ? http.post<T>(`${pub(g.lobbyId)}${path}`, { ...body, key: g.key }, { ...o, auth: false }) : http.post<T>(`${mtg(t.meetingId)}${path}`, body, o);
      return {
        state: (o?: CallOptions) => (g ? http.post<CallsRoomState>(`${pub(g.lobbyId)}/room`, { key: g.key }, { ...o, auth: false }) : http.get<CallsRoomState>(`${mtg(t.meetingId)}/room`, o)),
        vote: (pollId: string, choices: number[], o?: CallOptions) => post<CallsPoll>(`/polls/${seg(pollId)}/vote`, { choices }, o),
        ask: (body: string, anonymous: boolean, o?: CallOptions) => post<CallsQuestion>(`/questions`, { body, anonymous }, o),
        voteQuestion: (questionId: string, up: boolean, o?: CallOptions) => post<CallsQuestion>(`/questions/${seg(questionId)}/vote`, { up }, o),
        deleteQuestion: (questionId: string, o?: CallOptions) =>
          g ? post<void>(`/questions/${seg(questionId)}/delete`, {}, o) : http.delete<void>(`${mtg(t.meetingId)}/questions/${seg(questionId)}`, o),
        /** Your own final caption lines, while the transcript is on. */
        transcript: (lines: { text: string; spoken_at: string }[], o?: CallOptions) => post<void>(`/transcript`, { lines }, o),
        /** A copy of a message you sent, while the meeting saves chat. */
        chat: (text: string, o?: CallOptions) => post<void>(`/chat`, { text }, o),
        /** A token for your breakout room (or `roomId`: hosts any, others when choosing is on). */
        joinBreakout: (roomId?: string | null, o?: CallOptions) => post<CallsJoinToken>(`/breakouts/join`, roomId ? { room_id: roomId } : {}, o),
        askForHelp: (o?: CallOptions) => post<void>(`/breakouts/help`, {}, o),
      };
    },
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
