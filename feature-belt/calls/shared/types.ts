/** Calls DTOs. JSON matches backend/internal/calls exactly (snake_case). */

import type { ConnectionOffer, SourceRef } from "../sources";

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
  /** What people who aren't hosts may do in the room (hosts always may). */
  allow_screen_share: boolean;
  allow_chat: boolean;
  allow_reactions: boolean;
  /** false: people unmute only when a host asks them to. */
  allow_unmute: boolean;
  mute_on_entry: boolean;
  /** Chat messages are kept with the meeting. */
  save_chat: boolean;
  /** New questions wait for a host's approval. */
  moderate_questions: boolean;
  /** Who opens recordings, transcripts and saved chat besides hosts. */
  records_access: CallsRecordsAccess;
}

/** participants: people who were in that call · invited: also invitees · org: also the workspace's members. */
export type CallsRecordsAccess = "participants" | "invited" | "org";

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
  /** The room is being recorded right now (say so before joining). */
  recording: boolean;
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
  recording: boolean;
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
  /** Host or co-host: may download attendance. */
  is_host: boolean;
  /** What the call kept; null when the meeting's records rule leaves you out. */
  records: CallsCallRecords | null;
}

export interface CallsCallRecords {
  recordings: CallsRecording[];
  transcript_lines: number;
  chat_messages: number;
  polls: number;
  questions: number;
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
  allow_screen_share?: boolean;
  allow_chat?: boolean;
  allow_reactions?: boolean;
  allow_unmute?: boolean;
  mute_on_entry?: boolean;
  save_chat?: boolean;
  moderate_questions?: boolean;
  records_access?: CallsRecordsAccess;
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

/** A way to hold a video meeting: the app's own call (embedded) or a connected platform (opens its own app). */
export interface CallsMeetingProvider {
  id: "app" | "zoom" | "teams" | "google_meet" | (string & {});
  label: string;
  platform?: string;
  connection_id?: string;
  account?: string;
  status?: "active" | "needs_reauth" | "blocked";
  fix_url?: string;
  /** Runs inside the app (the app's own); the others open Zoom, Teams or Meet. */
  embedded: boolean;
  via?: string;
}

/** `GET /v1/calls/providers`. */
export interface CallsMeetingProviders {
  providers: CallsMeetingProvider[];
  offers: ConnectionOffer[];
}

/** A meeting with a platform link (Meet, Teams, Zoom) on the person's calendars, beside the app's own. */
export interface CallsPlatformMeeting {
  /** The calendar's reference for the event. */
  id: string;
  title: string;
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  provider: "google_meet" | "teams" | "zoom" | (string & {});
  join_url: string;
  organizer: string;
  my_response?: string;
  /** App path that opens it in the calendar. */
  link?: string;
  /** Set when it comes from a connected calendar; null for the app's own calendar. */
  source: SourceRef | null;
}

/** `GET /v1/calls/external?when=upcoming|recent`. */
export interface CallsPlatformMeetings {
  items: CallsPlatformMeeting[];
  /** A calendar is wired (the app's stitching). */
  available: boolean;
}

declare module "../types" {
  interface RealtimeEventMap {
    "calls.meeting_updated": CallsMeetingUpdatedEvent;
    "calls.ring_updated": CallsRing;
    "calls.ring": CallsRing;
    "calls.ring_ended": CallsRingEnded;
    "calls.lobby": CallsLobbyEvent;
    "calls.lobby_decided": CallsLobbyEntry;
    "calls.recording_updated": { meeting_id: string };
  }
}

// ───────────────────────────────────────────────────────────── room state ──

/** The host's rules as the room sees them (room metadata). */
export interface CallsRoomRules {
  screen_share: boolean;
  chat: boolean;
  reactions: boolean;
  unmute: boolean;
  mute_on_entry: boolean;
  save_chat: boolean;
}

export interface CallsBreakoutRef {
  id: string;
  name: string;
}

/** Server-owned room metadata every participant reads (guests too). */
export interface CallsRoomMeta {
  v: number;
  meeting_id: string;
  recording: { id: string; status: "starting" | "recording"; started_at: string } | null;
  /** The transcript is being saved. */
  transcribing: boolean;
  /** The workspace allows browser captions. */
  captions: boolean;
  /** Hosts may save the transcript. */
  transcripts: boolean;
  /** The server and workspace allow recording. */
  can_record: boolean;
  /** Identity everyone sees large ("" = none). */
  spotlight: string;
  rules: CallsRoomRules;
  breakout: {
    id: string;
    status: "open" | "closing";
    ends_at: string | null;
    closing_at: string | null;
    allow_choose: boolean;
    rooms: CallsBreakoutRef[];
    /** Set in a breakout room's own metadata. */
    room?: CallsBreakoutRef;
  } | null;
}

/** Server-sent data messages on `CALLS_SIGNAL_TOPIC`. */
export interface CallsSignal {
  type: "polls" | "questions" | "broadcast" | "ask_unmute" | "help" | "breakout";
  text?: string;
  from?: string;
  room_id?: string;
}

export interface CallsPoll {
  id: string;
  meeting_id: string;
  question: string;
  options: string[];
  multiple: boolean;
  /** Hosts see totals, not who voted for what. */
  anonymous: boolean;
  status: "draft" | "open" | "closed";
  results_shared: boolean;
  /** Per option: hosts always, others once results are shared. */
  counts: number[] | null;
  voters: number;
  my_choices: number[];
  /** Hosts, polls that aren't anonymous. */
  votes?: { name: string; choices: number[] }[];
  opened_at: string | null;
  closed_at: string | null;
  created_at: string;
}

export interface CallsPollInput {
  question: string;
  options: string[];
  multiple?: boolean;
  anonymous?: boolean;
  /** Launch now (the call must be live). */
  open?: boolean;
}

export interface CallsPollUpdate {
  question?: string;
  options?: string[];
  multiple?: boolean;
  anonymous?: boolean;
  status?: "open" | "closed";
  results_shared?: boolean;
}

export interface CallsQuestion {
  id: string;
  meeting_id: string;
  body: string;
  /** "Anonymous" for anonymous questions. */
  name: string;
  anonymous: boolean;
  status: "pending" | "open" | "answered" | "hidden";
  votes: number;
  voted: boolean;
  mine: boolean;
  created_at: string;
}

export interface CallsMyBreakout {
  id: string;
  status: "open" | "closing";
  allow_choose: boolean;
  ends_at: string | null;
  closing_at: string | null;
  /** Your room (null: not assigned). */
  room: CallsBreakoutRef | null;
  rooms: CallsBreakoutRef[];
}

/** `GET /v1/calls/meetings/{id}/room` (guests: `POST …/public/lobby/{id}/room`). */
export interface CallsRoomState {
  identity: string;
  host: boolean;
  polls: CallsPoll[];
  questions: CallsQuestion[];
  breakout: CallsMyBreakout | null;
}

export interface CallsBreakoutPerson {
  identity: string;
  name: string;
  user_id: string | null;
  present: boolean;
}

export interface CallsBreakoutRoom extends CallsBreakoutRef {
  assigned: CallsBreakoutPerson[];
  present: CallsBreakoutPerson[];
}

/** The open breakout rooms, for hosts. */
export interface CallsBreakout {
  id: string;
  status: "open" | "closing";
  allow_choose: boolean;
  ends_at: string | null;
  closing_at: string | null;
  rooms: CallsBreakoutRoom[];
  main_room: CallsBreakoutPerson[];
}

export interface CallsCreateBreakoutsRequest {
  /** Room names; or `count` rooms named Room 1…N. */
  rooms?: string[];
  count?: number;
  assign: "auto" | "manual" | "choose";
  /** manual: identity → room index. */
  assignments?: Record<string, number>;
  /** 0 = no timer. */
  minutes?: number;
}

export interface CallsUpdateBreakoutsRequest {
  /** identity → room id (null: unassign). */
  assignments?: Record<string, string | null>;
  /** Restart the timer from now (0 removes it). */
  minutes?: number;
  allow_choose?: boolean;
}

export type CallsRecordingStatus = "starting" | "recording" | "processing" | "ready" | "failed";

export interface CallsRecording {
  id: string;
  meeting_id: string;
  meeting_title: string;
  session_id: string | null;
  status: CallsRecordingStatus;
  started_by: CallsUser | null;
  started_at: string;
  ended_at: string | null;
  duration_seconds: number;
  size_bytes: number | null;
  expires_at: string | null;
  error: string | null;
  can_delete: boolean;
  /** Playback, for one hour (ready recordings fetched one at a time). */
  url: string | null;
  download_url: string | null;
}

export interface CallsTranscriptLine {
  name: string;
  user_id: string | null;
  text: string;
  spoken_at: string;
}

export interface CallsTranscript {
  session_id: string;
  meeting_id: string;
  title: string;
  started_at: string;
  ended_at: string | null;
  lines: CallsTranscriptLine[];
}

export interface CallsChatMessage {
  id: string;
  name: string;
  user_id: string | null;
  text: string;
  sent_at: string;
}

/** One call of a meeting, for the meeting page. */
export interface CallsSessionSummary {
  id: string;
  started_at: string;
  ended_at: string | null;
  duration_seconds: number;
  live: boolean;
  participant_count: number;
  recordings: number;
  transcript_lines: number;
  chat_messages: number;
}

/** What a workspace allows in its calls. */
export interface CallsOrgSettings {
  org_id: string;
  /** Browser speech recognition: each speaker's audio goes to their browser's vendor. */
  captions: boolean;
  transcripts: boolean;
  recording: boolean;
  /** You are a workspace owner or admin. */
  can_manage: boolean;
  /** This server can record at all. */
  recording_available: boolean;
}
