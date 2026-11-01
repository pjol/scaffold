/** Calls DTOs. JSON matches backend/internal/calls exactly (snake_case). */

/** org: members of the meeting's org and invitees · invited: invitees, others may ask · open: anyone with the link may ask (guests too). */
export type CallsAccess = "org" | "invited" | "open";
export type CallsKind = "meeting" | "call";
/** A person's role in a meeting (also in each participant's room metadata). */
export type CallsRole = "host" | "cohost" | "invitee" | "member" | "guest";
/** What the caller may do: join now, ask to join (`knock`), or nothing. */
export type CallsJoinMode = "host" | "cohost" | "direct" | "knock" | "denied";
export type CallsDeniedReason = "org_only" | "invited_only" | "locked" | "removed" | "canceled" | "sign_in";
export type CallsLobbyStatus = "waiting" | "admitted" | "denied" | "canceled" | "expired" | "removed";
export type CallsRingStatus = "ringing" | "accepted" | "declined" | "missed";

export interface CallsUser {
  id: string;
  display_name: string;
  avatar_url: string | null;
  deleted: boolean;
}

/** The record another feature created the meeting for (a calendar event). */
export interface CallsSource {
  kind: string;
  id: string;
}

export interface CallsMeeting {
  id: string;
  /** `abc-defg-hij` */
  code: string;
  /** `PUBLIC_WEB_URL/meet/<code>` */
  join_url: string;
  kind: CallsKind;
  title: string;
  host: CallsUser;
  org_id: string | null;
  org_name: string | null;
  access: CallsAccess;
  lobby_enabled: boolean;
  locked: boolean;
  starts_at: string | null;
  ends_at: string | null;
  source: CallsSource | null;
  canceled_at: string | null;
  /** Set while the room is live. */
  live_since: string | null;
  created_at: string;
}

export interface CallsInvitee {
  user: CallsUser;
  role: "invitee" | "cohost";
}

export interface CallsMeetingDetail extends CallsMeeting {
  /** The caller's role. */
  role: CallsRole;
  /** Co-hosts first. */
  invitees: CallsInvitee[];
}

/** `GET /v1/calls/meetings/{code}`: what the pre-join screen needs. */
export interface CallsJoinInfo {
  meeting_id: string;
  code: string;
  kind: CallsKind;
  mode: CallsJoinMode;
  reason: CallsDeniedReason | null;
  /** Shown to people who may join or ask. */
  title: string | null;
  host_name: string | null;
  live: boolean;
  /** Only for people who may join without asking. */
  meeting: CallsMeetingDetail | null;
  /** The caller's latest request to join (12 hours). */
  lobby: CallsLobbyEntry | null;
}

/** What a guest without an account sees. Title and host only for open meetings. */
export interface CallsPublicMeeting {
  code: string;
  title: string | null;
  host_name: string | null;
  guests_allowed: boolean;
  lobby_enabled: boolean;
  live: boolean;
  canceled: boolean;
  locked: boolean;
}

/** A short-lived SFU credential for one room: connect to `url` with `token`. */
export interface CallsJoinToken {
  token: string;
  url: string;
  room: string;
  /** `u:<user id>` or `g:<lobby id>` */
  identity: string;
  expires_at: string;
  role: CallsRole;
  meeting_id: string;
}

export interface CallsLobbyEntry {
  id: string;
  meeting_id: string;
  user: CallsUser | null;
  name: string;
  guest: boolean;
  status: CallsLobbyStatus;
  created_at: string;
  decided_at: string | null;
}

/** A guest's knock; keep `key` (only shown once) to poll and to join once admitted. */
export interface CallsGuestKnock extends CallsLobbyEntry {
  key: string;
}

export interface CallsRing {
  id: string;
  meeting_id: string;
  code: string;
  title: string;
  caller: CallsUser;
  callee: CallsUser;
  /** Everyone else rung with this person. */
  others: CallsUser[];
  status: CallsRingStatus;
  expires_at: string;
  created_at: string;
}

export interface CallsRingEnded {
  ring_id: string;
  meeting_id: string;
  status: CallsRingStatus;
}

export interface CallsMeetingRef {
  id: string;
  code: string;
  title: string;
  kind: CallsKind;
}

/** Someone who was in a call: an account, or a guest by name. */
export interface CallsPerson {
  user: CallsUser | null;
  name: string;
  guest: boolean;
}

export interface CallsHistoryItem {
  /** Session id (`call`) or ring id (`missed`). */
  id: string;
  type: "call" | "missed";
  meeting: CallsMeetingRef;
  started_at: string;
  ended_at: string | null;
  duration_seconds: number;
  live: boolean;
  /** Up to 6, first to join first. */
  participants: CallsPerson[];
  participant_count: number;
  caller: CallsUser | null;
  ring_status: "missed" | "declined" | null;
}

export interface CallsAttendance extends CallsPerson {
  identity: string;
  first_joined_at: string;
  /** null while still in the call. */
  last_left_at: string | null;
  seconds: number;
  joins: number;
}

export interface CallsCallDetail {
  id: string;
  meeting: CallsMeetingRef;
  host: CallsUser;
  started_at: string;
  ended_at: string | null;
  duration_seconds: number;
  live: boolean;
  participants: CallsAttendance[];
  can_join: boolean;
}

export interface CallsCreateMeetingRequest {
  title?: string;
  org_id?: string | null;
  access?: CallsAccess;
  lobby_enabled?: boolean;
  starts_at?: string | null;
  ends_at?: string | null;
  invitee_ids?: string[];
  cohost_ids?: string[];
  /** The creator's IANA zone; the endpoints send the device's. Invitations use it for people whose zone isn't known. */
  time_zone?: string;
}

export interface CallsUpdateMeetingRequest {
  title?: string;
  access?: CallsAccess;
  lobby_enabled?: boolean;
  locked?: boolean;
  starts_at?: string;
  ends_at?: string;
  /** Make it an unscheduled link. */
  clear_times?: boolean;
}

export interface CallsStartCallRequest {
  user_ids: string[];
  org_id?: string | null;
  title?: string;
}

/** `calls.lobby` on hosts' user topics. */
export interface CallsLobbyEvent {
  meeting_id: string;
  code: string;
  title: string;
  entry: CallsLobbyEntry;
  /** knock (asked, or back after stepping away), decided, canceled, left (stopped checking in), expired (nobody answered). */
  reason: "knock" | "decided" | "canceled" | "left" | "expired";
}

/** `calls.meeting_updated`: the whole meeting after a settings change, or a state flag. */
export type CallsMeetingUpdatedEvent = Partial<CallsMeeting> & { id: string; live?: boolean; ended?: boolean; canceled?: boolean };

/** Server-owned JSON in each participant's room metadata. */
export interface CallsParticipantMeta {
  role: CallsRole;
  user_id?: string;
  avatar_url?: string | null;
}

declare module "../types" {
  interface RealtimeEventMap {
    "calls.meeting_updated": CallsMeetingUpdatedEvent;
    "calls.ring_updated": CallsRing;
    "calls.ring": CallsRing;
    "calls.ring_ended": CallsRingEnded;
    "calls.lobby": CallsLobbyEvent;
    "calls.lobby_decided": CallsLobbyEntry;
  }
}
