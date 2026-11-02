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
import { callsEndpoints, type CallsLobbyTarget, type CallsMuteTarget } from "./api";
import { callsTimeZone, subscribeCallsTimeZone } from "./format";
import type {
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
