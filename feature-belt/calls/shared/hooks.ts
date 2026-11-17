"use client";

import { useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useMemo, useSyncExternalStore } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, useTopic, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerRealtimeHandler, registerReconnectRefetch } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import type { Page } from "../types";
import { useActiveOrgId } from "../orgs/hooks";
import { callsEndpoints, type CallsLobbyTarget, type CallsMuteTarget, type CallsRoomTarget } from "./api";
import { callsTimeZone, subscribeCallsTimeZone } from "./format";
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
  CallsMeetingProviders,
  CallsPlatformMeetings,
  CallsCallDetail,
  CallsCreateMeetingRequest,
  CallsGuestKnock,
  CallsHistoryItem,
  CallsJoinInfo,
  CallsJoinToken,
  CallsLobbyEntry,
  CallsMeeting,
  CallsMeetingDetail,
  CallsPublicMeeting,
  CallsRing,
  CallsStartCallRequest,
  CallsUpdateMeetingRequest,
  CallsUser,
} from "./types";

// ───────────────────────────────────────────────────────────── keys ──

export const callsKeys = {
  all: () => ["calls"] as const,
  history: () => ["calls", "history"] as const,
  call: (id: string) => ["calls", "call", id] as const,
  upcoming: () => ["calls", "upcoming"] as const,
  providers: (orgId: string | null) => ["calls", "providers", orgId ?? ""] as const,
  platformMeetings: (when: string, orgId: string | null) => ["calls", "platform-meetings", when, orgId ?? ""] as const,
  links: () => ["calls", "links"] as const,
  rings: () => ["calls", "rings"] as const,
  people: (q: string, orgId?: string | null) => ["calls", "people", q, orgId ?? "all"] as const,
  joinInfoAll: () => ["calls", "join"] as const,
  joinInfo: (ref: string) => ["calls", "join", ref] as const,
  lobby: (meetingId: string) => ["calls", "lobby", meetingId] as const,
  meetingRings: (meetingId: string) => ["calls", "meeting-rings", meetingId] as const,
  knock: (lobbyId: string) => ["calls", "knock", lobbyId] as const,
  guestMeeting: (code: string) => ["calls", "guest", "meeting", code] as const,
  guestStatus: (lobbyId: string) => ["calls", "guest", "status", lobbyId] as const,
  room: (meetingId: string, as: string) => ["calls", "room", meetingId, as] as const,
  breakouts: (meetingId: string) => ["calls", "breakouts", meetingId] as const,
  recordingsAll: () => ["calls", "recordings"] as const,
  meetingRecordings: (meetingId: string) => ["calls", "recordings", "meeting", meetingId] as const,
  recording: (id: string) => ["calls", "recordings", "one", id] as const,
  sessions: (meetingId: string) => ["calls", "sessions", meetingId] as const,
  transcript: (sessionId: string) => ["calls", "transcript", sessionId] as const,
  savedChat: (sessionId: string) => ["calls", "saved-chat", sessionId] as const,
  orgSettings: (orgId: string) => ["calls", "org-settings", orgId] as const,
};

registerReconnectRefetch(callsKeys.all());

/** The zone Calls shows times in (`setCallsTimeZone`, else the device's); re-renders when the app sets another. */
export function useCallsTimeZone(): string | undefined {
  return useSyncExternalStore(subscribeCallsTimeZone, callsTimeZone, callsTimeZone);
}

/** How often a waiting knock checks for a decision (realtime also tells people with accounts). */
export const CALLS_LOBBY_POLL_MS = 2_500;

// ───────────────────────────────────────────────────────────── lists ──

export function useCallsHistory(): PagedQueryResult<CallsHistoryItem> {
  const c = useEndpoints(callsEndpoints);
  return usePagedQuery(callsKeys.history(), (cursor, signal) => c.history({ cursor }, { signal }));
}

export function useCallsCall(id: string | null | undefined, options?: QueryOverrides<CallsCallDetail>): UseQueryResult<CallsCallDetail, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.call(id ?? ""), ({ signal }) => c.call(id ?? "", { signal }), options, !!id);
}

/** Meeting providers open to the caller in the active workspace. */
export function useCallsProviders(options?: QueryOverrides<CallsMeetingProviders>): UseQueryResult<CallsMeetingProviders, ApiError> {
  const c = useEndpoints(callsEndpoints);
  const orgId = useActiveOrgId();
  return useApiQuery(callsKeys.providers(orgId), ({ signal }) => c.providers(orgId, { signal }), { staleTime: 60_000, ...options });
}

/** Meetings with Meet, Teams or Zoom links on the caller's calendars (connected ones included), polled each minute. */
export function useCallsPlatformMeetings(when: "upcoming" | "recent", options?: QueryOverrides<CallsPlatformMeetings>): UseQueryResult<CallsPlatformMeetings, ApiError> {
  const c = useEndpoints(callsEndpoints);
  const orgId = useActiveOrgId();
  return useApiQuery(callsKeys.platformMeetings(when, orgId), ({ signal }) => c.platformMeetings(when, orgId, { signal }), { refetchInterval: 60_000, staleTime: 30_000, retry: 1, ...options });
}

export function useCallsUpcoming(): PagedQueryResult<CallsMeeting> {
  const c = useEndpoints(callsEndpoints);
  return usePagedQuery(callsKeys.upcoming(), (cursor, signal) => c.upcoming({ cursor }, { signal }));
}

export function useCallsLinks(): PagedQueryResult<CallsMeeting> {
  const c = useEndpoints(callsEndpoints);
  return usePagedQuery(callsKeys.links(), (cursor, signal) => c.links({ cursor }, { signal }));
}

/** People you may call or invite; `q` matches names or an exact email. */
export function useCallsPeople(q: string, orgId?: string | null, enabled = true): UseQueryResult<Page<CallsUser>, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.people(q, orgId), ({ signal }) => c.people(q, orgId, { signal }), { staleTime: 30_000 }, enabled);
}

// ───────────────────────────────────────────────────────────── meetings ──

/** Can I join this meeting (code, link or id)? Refreshes on meeting and lobby events. */
export function useCallsJoinInfo(ref: string | null | undefined, options?: QueryOverrides<CallsJoinInfo>): UseQueryResult<CallsJoinInfo, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.joinInfo(ref ?? ""), ({ signal }) => c.joinInfo(ref ?? "", { signal }), { retry: false, ...options }, !!ref);
}

/** Subscribe to a meeting's live events (settings, ringing) while mounted. */
export function useCallsMeetingTopic(meetingId: string | null | undefined): void {
  useTopic(meetingId ? topic("calls", meetingId) : null);
}

function afterMeetingChange(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: callsKeys.joinInfoAll() }),
    qc.invalidateQueries({ queryKey: callsKeys.upcoming() }),
    qc.invalidateQueries({ queryKey: callsKeys.links() }),
  ]);
}

export function useCreateCallsMeeting(): UseMutationResult<CallsMeetingDetail, ApiError, CallsCreateMeetingRequest> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (body) => c.create(body), onSuccess: () => afterMeetingChange(qc) });
}

export function useUpdateCallsMeeting(meetingId: string): UseMutationResult<CallsMeetingDetail, ApiError, CallsUpdateMeetingRequest> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (body) => c.update(meetingId, body), onSuccess: () => afterMeetingChange(qc) });
}

export function useCancelCallsMeeting(): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (meetingId) => c.cancel(meetingId), onSuccess: () => afterMeetingChange(qc) });
}

export function useAddCallsInvitees(meetingId: string): UseMutationResult<CallsMeetingDetail, ApiError, { userIds: string[]; role?: "invitee" | "cohost" }> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: ({ userIds, role }) => c.addInvitees(meetingId, userIds, role), onSuccess: () => afterMeetingChange(qc) });
}

export function useSetCallsInviteeRole(meetingId: string): UseMutationResult<CallsMeetingDetail, ApiError, { userId: string; role: "invitee" | "cohost" }> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: ({ userId, role }) => c.setInviteeRole(meetingId, userId, role), onSuccess: () => afterMeetingChange(qc) });
}

export function useRemoveCallsInvitee(meetingId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (userId) => c.removeInvitee(meetingId, userId), onSuccess: () => afterMeetingChange(qc) });
}

/** A room token for a meeting you may join (or were admitted to). */
export function useJoinCallsMeeting(): UseMutationResult<CallsJoinToken, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (meetingId) => c.join(meetingId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: callsKeys.rings() });
    },
  });
}

// ───────────────────────────────────────────────────────────── lobby ──

export function useKnockCallsMeeting(): UseMutationResult<CallsLobbyEntry, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (meetingId) => c.knock(meetingId),
    onSuccess: (entry) => qc.setQueryData(callsKeys.knock(entry.id), entry),
  });
}

/**
 * Your knock's status; polls while waiting, also in a background tab (realtime answers
 * sooner). Each poll tells hosts you're still there: stop polling and they stop seeing you.
 */
export function useCallsKnockStatus(lobbyId: string | null | undefined): UseQueryResult<CallsLobbyEntry, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.knock(lobbyId ?? ""), ({ signal }) => c.knockStatus(lobbyId ?? "", { signal }), {
    refetchInterval: (q) => (q.state.data?.status === "waiting" ? CALLS_LOBBY_POLL_MS : false),
    refetchIntervalInBackground: true,
  }, !!lobbyId);
}

export function useCancelCallsKnock(): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (lobbyId) => c.cancelKnock(lobbyId), onSuccess: (_, id) => qc.removeQueries({ queryKey: callsKeys.knock(id) }) });
}

/** People waiting to join (hosts); kept live by `calls.lobby` events. */
export function useCallsLobby(meetingId: string | null | undefined, enabled = true): UseQueryResult<Page<CallsLobbyEntry>, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.lobby(meetingId ?? ""), ({ signal }) => c.lobby(meetingId ?? "", { signal }), { refetchInterval: 30_000 }, !!meetingId && enabled);
}

function dropFromLobby(qc: QueryClient, meetingId: string, ids: Set<string> | null) {
  qc.setQueryData<Page<CallsLobbyEntry>>(callsKeys.lobby(meetingId), (old) => (old ? { ...old, items: ids ? old.items.filter((e) => !ids.has(e.id)) : [] } : old));
}

function useDecideCallsLobby(meetingId: string, kind: "admit" | "deny"): UseMutationResult<Page<CallsLobbyEntry>, ApiError, CallsLobbyTarget> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (target) => (kind === "admit" ? c.admit(meetingId, target) : c.deny(meetingId, target)),
    onMutate: (target) => dropFromLobby(qc, meetingId, "ids" in target ? new Set(target.ids) : null),
    onSettled: () => qc.invalidateQueries({ queryKey: callsKeys.lobby(meetingId) }),
  });
}

export function useAdmitCallsLobby(meetingId: string) {
  return useDecideCallsLobby(meetingId, "admit");
}

export function useDenyCallsLobby(meetingId: string) {
  return useDecideCallsLobby(meetingId, "deny");
}

// ───────────────────────────────────────────────────────────── in the call ──

export function useMuteCallsParticipant(meetingId: string): UseMutationResult<{ muted: number }, ApiError, CallsMuteTarget> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (target) => c.mute(meetingId, target) });
}

export function useRemoveCallsParticipant(meetingId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (identity) => c.remove(meetingId, identity) });
}

export function useEndCallsMeeting(meetingId: string): UseMutationResult<void, ApiError, void> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: () => c.end(meetingId), onSuccess: () => qc.invalidateQueries({ queryKey: callsKeys.history() }) });
}

/** Raise or lower your hand (or, for hosts, lower someone's with `identity`). */
export function useSetCallsHand(meetingId: string): UseMutationResult<void, ApiError, { raised: boolean; identity?: string }> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: ({ raised, identity }) => c.hand(meetingId, raised, identity) });
}

// ───────────────────────────────────────────────────────────── ringing ──

export function useStartCallsCall(): UseMutationResult<CallsMeetingDetail, ApiError, CallsStartCallRequest> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (body) => c.startCall(body) });
}

export function useRingCallsAgain(meetingId: string): UseMutationResult<void, ApiError, string[] | undefined> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (userIds) => c.ring(meetingId, userIds ?? []) });
}

export function useStopCallsRinging(): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (meetingId) => c.stopRinging(meetingId) });
}

/** Calls ringing for you now; `calls.ring` / `calls.ring_ended` keep it live. */
export function useCallsRings(enabled = true): UseQueryResult<Page<CallsRing>, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.rings(), ({ signal }) => c.rings({ signal }), { staleTime: 60_000 }, enabled);
}

function dropRing(qc: QueryClient, ringId: string) {
  qc.setQueryData<Page<CallsRing>>(callsKeys.rings(), (old) => (old ? { ...old, items: old.items.filter((r) => r.id !== ringId) } : old));
}

export function useDeclineCallsRing(): UseMutationResult<CallsRing, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (ringId) => c.declineRing(ringId), onMutate: (id) => dropRing(qc, id), onSettled: () => qc.invalidateQueries({ queryKey: callsKeys.history() }) });
}

export function useAcceptCallsRing(): UseMutationResult<CallsRing, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (ringId) => c.acceptRing(ringId), onMutate: (id) => dropRing(qc, id) });
}

/** Keep only the latest ring per person. */
function latestPerCallee(items: CallsRing[]): CallsRing[] {
  const by = new Map<string, CallsRing>();
  for (const r of items) {
    const prev = by.get(r.callee.id);
    if (!prev || prev.created_at <= r.created_at) by.set(r.callee.id, r);
  }
  return [...by.values()].sort((a, b) => a.callee.display_name.localeCompare(b.callee.display_name));
}

/** Latest ring per person on a meeting (ringing, accepted, declined, missed), kept live by `calls.ring_updated`. */
export function useCallsMeetingRings(meetingId: string | null | undefined, enabled = true): CallsRing[] {
  const c = useEndpoints(callsEndpoints);
  const q = useApiQuery(callsKeys.meetingRings(meetingId ?? ""), ({ signal }) => c.meetingRings(meetingId ?? "", { signal }), { staleTime: 60_000, retry: false }, !!meetingId && enabled);
  return useMemo(() => latestPerCallee(q.data?.items ?? []), [q.data]);
}

// ───────────────────────────────────────────────────────────── guests ──

export function useCallsGuestMeeting(code: string | null | undefined): UseQueryResult<CallsPublicMeeting, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.guestMeeting(code ?? ""), ({ signal }) => c.guest.meeting(code ?? "", { signal }), { retry: false }, !!code);
}

export function useCallsGuestKnock(): UseMutationResult<CallsGuestKnock, ApiError, { code: string; name: string }> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: ({ code, name }) => c.guest.knock(code, name) });
}

/** A guest's knock status, polled while waiting (also in a background tab); each poll is the guest's check-in. */
export function useCallsGuestStatus(lobbyId: string | null | undefined, key: string | null | undefined): UseQueryResult<CallsLobbyEntry, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.guestStatus(lobbyId ?? ""), ({ signal }) => c.guest.status(lobbyId ?? "", key ?? "", { signal }), {
    retry: false,
    refetchInterval: (q) => (q.state.data?.status === "waiting" ? CALLS_LOBBY_POLL_MS : false),
    refetchIntervalInBackground: true,
  }, !!lobbyId && !!key);
}

export function useCallsGuestToken(): UseMutationResult<CallsJoinToken, ApiError, { lobbyId: string; key: string }> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: ({ lobbyId, key }) => c.guest.token(lobbyId, key) });
}

export function useCancelCallsGuestKnock(): UseMutationResult<void, ApiError, { lobbyId: string; key: string }> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: ({ lobbyId, key }) => c.guest.cancel(lobbyId, key) });
}

export function useSetCallsGuestHand(): UseMutationResult<void, ApiError, { lobbyId: string; key: string; raised: boolean }> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: ({ lobbyId, key, raised }) => c.guest.hand(lobbyId, key, raised) });
}

// ───────────────────────────────────────────────────────────── realtime ──

function putMeetingRing(qc: QueryClient, ring: CallsRing) {
  qc.setQueryData<Page<CallsRing>>(callsKeys.meetingRings(ring.meeting_id), (old) =>
    old ? { ...old, items: [...old.items.filter((r) => r.id !== ring.id), ring] } : old,
  );
}

registerRealtimeHandler("calls.ring", (qc, e) => {
  const ring = e.data;
  qc.setQueryData<Page<CallsRing>>(callsKeys.rings(), (old) => {
    const items = (old?.items ?? []).filter((r) => r.id !== ring.id);
    return { items: [ring, ...items], next_cursor: null };
  });
});

registerRealtimeHandler("calls.ring_ended", (qc, e) => {
  dropRing(qc, e.data.ring_id);
  if (e.data.status !== "accepted") void qc.invalidateQueries({ queryKey: callsKeys.history() });
});

registerRealtimeHandler("calls.ring_updated", (qc, e) => {
  putMeetingRing(qc, e.data);
});

registerRealtimeHandler("calls.lobby", (qc, e) => {
  const { meeting_id, entry, reason } = e.data;
  qc.setQueryData<Page<CallsLobbyEntry>>(callsKeys.lobby(meeting_id), (old) => {
    if (!old) return old;
    const items = old.items.filter((x) => x.id !== entry.id);
    return { ...old, items: reason === "knock" && entry.status === "waiting" ? [...items, entry] : items };
  });
});

registerRealtimeHandler("calls.lobby_decided", (qc, e) => {
  qc.setQueryData(callsKeys.knock(e.data.id), e.data);
  void qc.invalidateQueries({ queryKey: callsKeys.joinInfoAll() });
});

registerRealtimeHandler("calls.meeting_updated", (qc) => {
  void afterMeetingChange(qc);
});

registerRealtimeHandler("calls.recording_updated", (qc, e) => {
  void qc.invalidateQueries({ queryKey: callsKeys.recordingsAll() });
  void qc.invalidateQueries({ queryKey: callsKeys.sessions(e.data.meeting_id) });
  void qc.invalidateQueries({ queryKey: ["calls", "call"] });
});

// ───────────────────────────────────────────────────────────── room (polls, questions, breakouts) ──

function roomKey(t: CallsRoomTarget) {
  return callsKeys.room(t.meetingId, t.guest?.lobbyId ?? "me");
}

/**
 * What someone in the call needs beyond the room metadata: polls, questions and their
 * breakout room. Refetch it when a `polls`, `questions` or `breakout` signal arrives
 * (`useInvalidateCallsRoom`); it also refreshes every 30 s as a fallback.
 */
export function useCallsRoomState(t: CallsRoomTarget | null, enabled = true): UseQueryResult<CallsRoomState, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(t ? roomKey(t) : ["calls", "room", "none"], ({ signal }) => c.room(t!).state({ signal }), { refetchInterval: 30_000, retry: 1 }, !!t && enabled);
}

export function useInvalidateCallsRoom(t: CallsRoomTarget | null): () => void {
  const qc = useQueryClient();
  return () => {
    if (t) void qc.invalidateQueries({ queryKey: roomKey(t) });
  };
}

function useRoomMutation<V, R>(t: CallsRoomTarget, fn: (room: ReturnType<ReturnType<typeof callsEndpoints>["room"]>, v: V) => Promise<R>): UseMutationResult<R, ApiError, V> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (v: V) => fn(c.room(t), v), onSettled: () => qc.invalidateQueries({ queryKey: roomKey(t) }) });
}

export function useVoteCallsPoll(t: CallsRoomTarget): UseMutationResult<CallsPoll, ApiError, { pollId: string; choices: number[] }> {
  return useRoomMutation(t, (r, v: { pollId: string; choices: number[] }) => r.vote(v.pollId, v.choices));
}

export function useAskCallsQuestion(t: CallsRoomTarget): UseMutationResult<CallsQuestion, ApiError, { body: string; anonymous: boolean }> {
  return useRoomMutation(t, (r, v: { body: string; anonymous: boolean }) => r.ask(v.body, v.anonymous));
}

export function useVoteCallsQuestion(t: CallsRoomTarget): UseMutationResult<CallsQuestion, ApiError, { questionId: string; up: boolean }> {
  return useRoomMutation(t, (r, v: { questionId: string; up: boolean }) => r.voteQuestion(v.questionId, v.up));
}

export function useDeleteCallsQuestion(t: CallsRoomTarget): UseMutationResult<void, ApiError, string> {
  return useRoomMutation(t, (r, id: string) => r.deleteQuestion(id));
}

/** A token for a breakout room (yours, or `roomId`). */
export function useJoinCallsBreakout(t: CallsRoomTarget): UseMutationResult<CallsJoinToken, ApiError, string | null | undefined> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (roomId) => c.room(t).joinBreakout(roomId) });
}

export function useAskCallsHelp(t: CallsRoomTarget): UseMutationResult<void, ApiError, void> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: () => c.room(t).askForHelp() });
}

// Hosts.

function useHostMutation<V, R>(meetingId: string, fn: (c: ReturnType<typeof callsEndpoints>, v: V) => Promise<R>, also?: readonly unknown[]): UseMutationResult<R, ApiError, V> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: V) => fn(c, v),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["calls", "room", meetingId] });
      if (also) void qc.invalidateQueries({ queryKey: also });
    },
  });
}

export function useCreateCallsPoll(meetingId: string): UseMutationResult<CallsPoll, ApiError, CallsPollInput> {
  return useHostMutation(meetingId, (c, body: CallsPollInput) => c.createPoll(meetingId, body));
}

export function useUpdateCallsPoll(meetingId: string): UseMutationResult<CallsPoll, ApiError, { pollId: string } & CallsPollUpdate> {
  return useHostMutation(meetingId, (c, { pollId, ...body }: { pollId: string } & CallsPollUpdate) => c.updatePoll(meetingId, pollId, body));
}

export function useDeleteCallsPoll(meetingId: string): UseMutationResult<void, ApiError, string> {
  return useHostMutation(meetingId, (c, pollId: string) => c.deletePoll(meetingId, pollId));
}

export function useModerateCallsQuestion(meetingId: string): UseMutationResult<CallsQuestion, ApiError, { questionId: string; status: "open" | "answered" | "hidden" }> {
  return useHostMutation(meetingId, (c, v: { questionId: string; status: "open" | "answered" | "hidden" }) => c.moderateQuestion(meetingId, v.questionId, v.status));
}

/** The open breakout rooms with who is where (hosts); polled while shown. */
export function useCallsBreakouts(meetingId: string, enabled = true): UseQueryResult<CallsBreakout | null, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.breakouts(meetingId), ({ signal }) => c.breakouts(meetingId, { signal }), { refetchInterval: 5_000 }, enabled);
}

export function useCreateCallsBreakouts(meetingId: string): UseMutationResult<CallsBreakout, ApiError, CallsCreateBreakoutsRequest> {
  return useHostMutation(meetingId, (c, body: CallsCreateBreakoutsRequest) => c.createBreakouts(meetingId, body), callsKeys.breakouts(meetingId));
}

export function useUpdateCallsBreakouts(meetingId: string): UseMutationResult<CallsBreakout, ApiError, CallsUpdateBreakoutsRequest> {
  return useHostMutation(meetingId, (c, body: CallsUpdateBreakoutsRequest) => c.updateBreakouts(meetingId, body), callsKeys.breakouts(meetingId));
}

export function useBroadcastCallsBreakouts(meetingId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (text) => c.broadcastBreakouts(meetingId, text) });
}

export function useCloseCallsBreakouts(meetingId: string): UseMutationResult<void, ApiError, boolean | undefined> {
  return useHostMutation(meetingId, (c, now: boolean | undefined) => c.closeBreakouts(meetingId, !!now), callsKeys.breakouts(meetingId));
}

export function useStartCallsRecording(meetingId: string): UseMutationResult<CallsRecording, ApiError, void> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: () => c.startRecording(meetingId) });
}

export function useStopCallsRecording(meetingId: string): UseMutationResult<CallsRecording, ApiError, void> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: () => c.stopRecording(meetingId) });
}

export function useSetCallsSpotlight(meetingId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (identity) => c.spotlight(meetingId, identity) });
}

export function useAskCallsUnmute(meetingId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (identity) => c.askUnmute(meetingId, identity) });
}

export function useSetCallsTranscribing(meetingId: string): UseMutationResult<void, ApiError, boolean> {
  const c = useEndpoints(callsEndpoints);
  return useMutation({ mutationFn: (on) => c.setTranscribing(meetingId, on) });
}

// ───────────────────────────────────────────────────────────── records ──

/** A meeting's recordings you may open; kept live by `calls.recording_updated`. */
export function useCallsMeetingRecordings(meetingId: string | null | undefined): UseQueryResult<Page<CallsRecording>, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.meetingRecordings(meetingId ?? ""), ({ signal }) => c.meetingRecordings(meetingId ?? "", { signal }), undefined, !!meetingId);
}

/** One recording with playback links (valid for an hour, refreshed every 50 minutes). */
export function useCallsRecording(id: string | null | undefined): UseQueryResult<CallsRecording, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.recording(id ?? ""), ({ signal }) => c.recording(id ?? "", { signal }), { staleTime: 50 * 60_000, refetchInterval: 50 * 60_000, retry: false }, !!id);
}

export function useDeleteCallsRecording(): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id) => c.deleteRecording(id),
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: callsKeys.recording(id) });
      void qc.invalidateQueries({ queryKey: callsKeys.recordingsAll() });
      void qc.invalidateQueries({ queryKey: ["calls", "call"] });
    },
  });
}

export function useCallsMeetingSessions(meetingId: string | null | undefined): PagedQueryResult<CallsSessionSummary> {
  const c = useEndpoints(callsEndpoints);
  return usePagedQuery(callsKeys.sessions(meetingId ?? ""), (cursor, signal) => c.sessions(meetingId ?? "", { cursor }, { signal }), undefined, !!meetingId);
}

export function useCallsTranscript(sessionId: string | null | undefined, enabled = true): UseQueryResult<CallsTranscript, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.transcript(sessionId ?? ""), ({ signal }) => c.transcript(sessionId ?? "", { signal }), undefined, !!sessionId && enabled);
}

export function useCallsSavedChat(sessionId: string | null | undefined, enabled = true): UseQueryResult<Page<CallsChatMessage>, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.savedChat(sessionId ?? ""), ({ signal }) => c.savedChat(sessionId ?? "", { signal }), undefined, !!sessionId && enabled);
}

export function useCallsOrgSettings(orgId: string | null | undefined): UseQueryResult<CallsOrgSettings, ApiError> {
  const c = useEndpoints(callsEndpoints);
  return useApiQuery(callsKeys.orgSettings(orgId ?? ""), ({ signal }) => c.orgSettings(orgId ?? "", { signal }), undefined, !!orgId);
}

export function useUpdateCallsOrgSettings(orgId: string): UseMutationResult<CallsOrgSettings, ApiError, Partial<Pick<CallsOrgSettings, "captions" | "transcripts" | "recording">>> {
  const c = useEndpoints(callsEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (body) => c.updateOrgSettings(orgId, body), onSuccess: (d) => qc.setQueryData(callsKeys.orgSettings(orgId), d) });
}
