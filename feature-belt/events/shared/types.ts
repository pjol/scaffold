/** Events DTOs. snake_case, mirroring backend/internal/events (change both together). */

export type EventVisibility = "public" | "members" | "invite";
export type EventStatus = "scheduled" | "cancelled";
/** `waitlisted` is set by the server when `going` finds the event full; clients send going, maybe or declined. */
export type RsvpStatus = "going" | "maybe" | "declined" | "waitlisted";
export type RsvpChoice = Exclude<RsvpStatus, "waitlisted">;
export type EventHostRole = "creator" | "cohost";

export interface EventPerson {
  id: string;
  display_name: string;
  avatar_url: string | null;
}

export interface MyRsvp {
  status: RsvpStatus;
  /** 1-based place in the queue while waitlisted. */
  waitlist_position: number | null;
}

/** An event as a signed-in user sees it. Times are UTC; `timezone` (IANA) is for display. */
export interface EventItem {
  id: string;
  title: string;
  details: string;
  starts_at: string;
  ends_at: string;
  timezone: string;
  location_text: string;
  lat: number | null;
  lng: number | null;
  online_url: string | null;
  cover_key: string | null;
  cover_url: string | null;
  capacity: number | null;
  visibility: EventVisibility;
  status: EventStatus;
  host: EventPerson;
  co_hosts: EventPerson[];
  going_count: number;
  maybe_count: number;
  waitlist_count: number;
  is_full: boolean;
  my_rsvp: MyRsvp | null;
  host_role: EventHostRole | null;
  /** Hosts only: the current invite link code. */
  invite_code?: string | null;
  cancelled_at: string | null;
  created_at: string;
  updated_at: string;
}

/** A public event for signed-out readers: no attendee data, no host account id. */
export interface PublicEvent {
  id: string;
  title: string;
  details: string;
  starts_at: string;
  ends_at: string;
  timezone: string;
  location_text: string;
  lat: number | null;
  lng: number | null;
  online_url: string | null;
  cover_url: string | null;
  status: EventStatus;
  is_full: boolean;
  host: { display_name: string; avatar_url: string | null };
  cancelled_at: string | null;
  updated_at: string;
}

export interface EventAttendee {
  user: EventPerson;
  status: RsvpStatus;
  waitlist_position: number | null;
  responded_at: string;
}

export interface EventInvitee {
  user: EventPerson;
  invited_at: string;
}

export interface AdminEvent {
  id: string;
  title: string;
  starts_at: string;
  ends_at: string;
  timezone: string;
  visibility: EventVisibility;
  status: EventStatus;
  capacity: number | null;
  host: EventPerson;
  going_count: number;
  waitlist_count: number;
  cancelled_at: string | null;
  created_at: string;
}

/** `POST /v1/events` and `PUT /v1/events/{id}` (PUT replaces every field). */
export interface EventRequest {
  title: string;
  details: string;
  starts_at: string;
  ends_at: string;
  timezone: string;
  location_text: string;
  lat: number | null;
  lng: number | null;
  online_url: string | null;
  /** A key from `POST /v1/uploads` with purpose `event_cover` (or the current one). */
  cover_key: string | null;
  /** null = no limit. */
  capacity: number | null;
  visibility: EventVisibility;
}

export interface EventListParams {
  when?: "upcoming" | "past";
  filter?: "all" | "hosting" | "attending";
  /** RFC 3339; with `to` (≤ 100 days apart) lists events overlapping the window. */
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
}

export interface AdminEventListParams {
  q?: string;
  host_id?: string;
  status?: EventStatus;
  limit?: number;
  cursor?: string;
}

export interface EventInviteLink {
  code: string;
  /** App path that joins with the code: `/events/join?code=…`. */
  path: string;
}

export interface CalendarFeedStatus {
  active: boolean;
  created_at: string | null;
}

/** Shown once, when created: only a hash is stored. */
export interface CalendarFeedUrl {
  url: string;
  created_at: string;
}

// ───────────────────────────────────────────────────────────── realtime ──

/** `events.updated` on `events:<id>`: edited or cancelled; refetch the event. */
export interface EventUpdatedEvent {
  event_id: string;
  status: EventStatus;
}

/** `events.rsvp` on `events:<id>`: counts only, never who. */
export interface EventRsvpEvent {
  event_id: string;
  going_count: number;
  maybe_count: number;
  waitlist_count: number;
  is_full: boolean;
}

declare module "../types" {
  interface RealtimeEventMap {
    "events.updated": EventUpdatedEvent;
    "events.rsvp": EventRsvpEvent;
  }
}
