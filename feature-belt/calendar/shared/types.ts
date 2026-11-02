/** Calendar DTOs. Field names match the Go JSON exactly (snake_case). Times are UTC ISO strings. */

import type { ConnectionOffer, SourceProvider, SourceRef } from "../sources";

export type CalendarColor = "primary" | "accent" | "success" | "warning" | "danger" | "info" | "neutral";
export const CALENDAR_COLORS: readonly CalendarColor[] = ["primary", "accent", "success", "warning", "danger", "info", "neutral"];

export type CalendarResponse = "needs_action" | "accepted" | "tentative" | "declined";
/** Which occurrences of a recurring event an edit or delete touches. */
export type CalendarScope = "this" | "following" | "all";
export type CalendarVisibility = "busy" | "details";
export type CalendarShowAs = "busy" | "free";

/** Another user as people see them: name and picture, never an email. */
export interface CalendarPerson {
  id: string;
  display_name: string;
  avatar_url: string | null;
}

export interface CalendarAttendee {
  id: string;
  user_id: string | null;
  /** External guests only; platform users show by name. */
  email: string;
  name: string;
  avatar_url: string | null;
  response: CalendarResponse;
  responded_at: string | null;
}

/** One instance of an event in a calendar window. */
export interface CalendarOccurrence {
  /** The single event or the series master. */
  event_id: string;
  /** Series only: the instance's original start, its key for edits. */
  occurrence_start: string | null;
  recurring: boolean;
  /** An edited occurrence of a series. */
  modified: boolean;
  title: string;
  location: string;
  conference_url: string | null;
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  /** All-day: first day, YYYY-MM-DD. */
  start_date: string | null;
  /** All-day: last day (inclusive), YYYY-MM-DD. */
  end_date: string | null;
  timezone: string;
  color: CalendarColor;
  show_as: CalendarShowAs;
  visibility: CalendarVisibility;
  role: "owner" | "attendee";
  my_response: CalendarResponse | null;
  owner: CalendarPerson;
  attendee_count: number;
  booking_id: string | null;
  /** Set on events from a connected calendar (`CalendarConnectedOccurrence`); the app's own carry none. */
  source?: SourceRef | null;
  /** Connected events: the meeting link's provider (google_meet, teams, zoom, app). */
  conference_provider?: string;
}

export interface CalendarRecurrence {
  freq: "daily" | "weekly" | "monthly" | "yearly";
  interval: number;
  /** Weekly: MO…SU; monthly: 1MO…-1SU. */
  by_day: string[];
  /** Monthly: 1…31, -1 = last day. */
  by_month_day: number[];
  count: number | null;
  /** Last day (YYYY-MM-DD, inclusive) in the event's zone. */
  until: string | null;
}

export interface CalendarEventDetail extends CalendarOccurrence {
  description: string;
  recurrence: CalendarRecurrence | null;
  rrule: string;
  attendees: CalendarAttendee[];
  reminders: number[];
  can_edit: boolean;
  created_at: string;
  updated_at: string;
}

/** A guest: a colleague by user_id, or anyone by email. */
export interface CalendarAttendeeInput {
  user_id?: string;
  email?: string;
  name?: string;
}

/**
 * Create/update body. On update send only what changes; a time change sends the full set
 * (all_day, timezone, and starts_at/ends_at or start_date/end_date).
 */
export interface CalendarEventInput {
  title?: string;
  description?: string;
  location?: string;
  starts_at?: string;
  ends_at?: string;
  start_date?: string;
  end_date?: string;
  timezone?: string;
  all_day?: boolean;
  /** null stops repeating. */
  recurrence?: Omit<CalendarRecurrence, "count" | "until"> & { count?: number | null; until?: string | null } | null;
  visibility?: CalendarVisibility;
  show_as?: CalendarShowAs;
  color?: CalendarColor;
  reminders?: number[];
  attendees?: CalendarAttendeeInput[];
  conference?: "add" | "remove";
  /** With conference "add": a `CalendarConferenceOption` id ("app" = the app's own call). */
  conference_provider?: string;
  conference_connection_id?: string;
  /** Default true: guests hear about the change. */
  send_updates?: boolean;
  scope?: CalendarScope;
  occurrence_start?: string | null;
}

export interface CalendarExternalBusy {
  subscription_id: string;
  name: string;
  color: CalendarColor;
  starts_at: string;
  ends_at: string;
}

export interface CalendarRange {
  items: CalendarOccurrence[];
  busy: CalendarExternalBusy[];
  truncated: boolean;
}

/** Weekly window: day 0 = Sunday; start/end are minutes from local midnight. */
export interface CalendarHoursInterval {
  day: number;
  start: number;
  end: number;
}

export interface CalendarTimeInterval {
  start: number;
  end: number;
}

/** Replaces a date's weekly hours; no intervals = unavailable that day. */
export interface CalendarDateOverride {
  date: string;
  intervals: CalendarTimeInterval[];
}

export interface CalendarSettings {
  timezone: string;
  timezone_set: boolean;
  /** 0 Sunday, 1 Monday, 6 Saturday. */
  week_start: number;
  working_hours: CalendarHoursInterval[];
  default_reminders: number[];
  video_available: boolean;
  subscriptions_available: boolean;
}

export interface CalendarSettingsInput {
  timezone?: string;
  week_start?: number;
  working_hours?: CalendarHoursInterval[];
  default_reminders?: number[];
}

export interface CalendarBusyBlock {
  starts_at: string;
  ends_at: string;
  /** Only when the owner shares details for that event. */
  title: string | null;
}

export interface CalendarFreeBusy {
  user: CalendarPerson;
  busy: CalendarBusyBlock[];
}

export interface CalendarSlot {
  starts_at: string;
  ends_at: string;
}

export interface CalendarFreeTime {
  timezone: string;
  slots: CalendarSlot[];
}

/** An external calendar added by its secret iCal address (never returned, only its host). */
export interface CalendarSubscription {
  id: string;
  name: string;
  host: string;
  color: CalendarColor;
  enabled: boolean;
  last_fetched_at: string | null;
  last_success_at: string | null;
  last_error: string;
  busy_count: number;
  created_at: string;
}

export interface CalendarSubscriptionInput {
  name: string;
  url: string;
  color?: CalendarColor;
}

export interface CalendarFeedState {
  active: boolean;
  created_at: string | null;
}

/** Shown once, when created: only a hash is stored. */
export interface CalendarFeedSecret {
  url: string;
  created_at: string;
}

// ───────────────────────────────────────────────────────────── booking links ──

export type CalendarLocationType = "video" | "phone" | "in_person" | "custom";
export type CalendarLinkAudience = "internal" | "external";
export type CalendarQuestionType = "short_text" | "long_text" | "phone" | "choice";

export interface CalendarQuestion {
  id: string;
  label: string;
  type: CalendarQuestionType;
  required: boolean;
  options: string[];
}

export interface CalendarBookingLink {
  id: string;
  org_id: string | null;
  org_name: string | null;
  audience: CalendarLinkAudience;
  slug: string;
  /** The public booking page (PUBLIC_WEB_URL/book/<slug>). */
  url: string;
  title: string;
  description: string;
  durations: number[];
  location_type: CalendarLocationType;
  location_value: string;
  timezone: string;
  weekly_hours: CalendarHoursInterval[];
  date_overrides: CalendarDateOverride[];
  buffer_before: number;
  buffer_after: number;
  /** Minutes. */
  min_notice: number;
  max_days_ahead: number;
  max_per_day: number | null;
  slot_interval: number;
  questions: CalendarQuestion[];
  confirmation_message: string;
  color: CalendarColor;
  active: boolean;
  allow_reschedule: boolean;
  allow_cancel: boolean;
  /** Minutes before the start when booker changes stop. */
  manage_cutoff: number;
  upcoming_count: number;
  created_at: string;
  updated_at: string;
  /** The connected calendar bookings are also written to (null = only this app's calendar). */
  add_to: CalendarCalendarRef | null;
  /** Meeting provider for video bookings ("app" default, google_meet, teams, zoom). */
  conference_provider: string;
  conference_connection_id: string | null;
}

export type CalendarBookingLinkInput = Partial<
  Omit<CalendarBookingLink, "id" | "org_name" | "url" | "upcoming_count" | "created_at" | "updated_at">
>;

/** What a booking page shows: the link and host, never events. */
export interface CalendarPublicLink {
  /** "members_only": an internal link this visitor can't book; only slug, org_name and signed_in are filled. */
  access: "open" | "members_only";
  signed_in: boolean;
  slug: string;
  title: string;
  description: string;
  durations: number[];
  location_type: CalendarLocationType;
  location_label: string;
  needs_phone: boolean;
  timezone: string;
  audience: CalendarLinkAudience;
  org_name: string | null;
  host: CalendarPerson;
  questions: CalendarQuestion[];
  max_days_ahead: number;
  color: CalendarColor;
  /** The signed-in visitor, prefilled. */
  booker: { name: string; email: string } | null;
}

export interface CalendarSlots {
  slots: CalendarSlot[];
  duration: number;
}

export interface CalendarBookingRequest {
  starts_at: string;
  duration: number;
  /** Ignored when signed in (the account's name and email are used). */
  name?: string;
  email?: string;
  phone?: string;
  timezone: string;
  answers: { question_id: string; value: string }[];
}

export interface CalendarAnswer {
  question_id: string;
  label: string;
  value: string;
}

/** A booking as the booker sees it (manage page). */
export interface CalendarPublicBooking {
  id: string;
  title: string;
  host: CalendarPerson;
  booker_name: string;
  booker_email: string;
  booker_timezone: string;
  starts_at: string;
  ends_at: string;
  location_type: CalendarLocationType;
  location_label: string;
  conference_url: string | null;
  status: "confirmed" | "cancelled";
  cancelled_by: "host" | "booker" | "system" | null;
  cancel_reason: string;
  can_cancel: boolean;
  can_reschedule: boolean;
  manage_cutoff: number;
  /** Set while the link is open: book another time. */
  link_slug: string | null;
  durations: number[];
  timezone: string;
}

export interface CalendarBookingConfirmation {
  booking: CalendarPublicBooking;
  /** The booker's secret link to reschedule or cancel (also emailed). */
  manage_url: string;
  confirmation_message: string;
}

/** A booking in the signed-in lists. */
export interface CalendarBooking {
  id: string;
  role: "host" | "booker";
  link_id: string | null;
  event_id: string | null;
  title: string;
  host: CalendarPerson;
  booker_user_id: string | null;
  booker_name: string;
  booker_email: string;
  booker_timezone: string;
  starts_at: string;
  ends_at: string;
  location_type: CalendarLocationType;
  location_value: string;
  conference_url: string | null;
  answers: CalendarAnswer[];
  status: "confirmed" | "cancelled";
  cancelled_at: string | null;
  cancelled_by: "host" | "booker" | "system" | null;
  cancel_reason: string;
  created_at: string;
}

export type CalendarBookingWhen = "upcoming" | "past" | "cancelled";

/** An external guest's invitation (tokenized RSVP page). */
export interface CalendarGuestInvitation {
  title: string;
  organizer: string;
  starts_at: string;
  ends_at: string;
  all_day: boolean;
  start_date: string | null;
  end_date: string | null;
  timezone: string;
  repeats: string;
  location: string;
  conference_url: string | null;
  description: string;
  name: string;
  response: CalendarResponse;
  responded_at: string | null;
}

/** `members_only` error details on internal booking links. */
export interface CalendarMembersOnly {
  org_name: string;
  signed_in: boolean;
}

// ───────────────────────────────────────────────────────────── realtime ──

/** `calendar.changed` on `user:<id>`: refetch that part of the calendar. */
export interface CalendarChangedEvent {
  scope: "events" | "bookings" | "links" | "subscriptions" | "connected";
}

declare module "../types" {
  interface RealtimeEventMap {
    "calendar.changed": CalendarChangedEvent;
  }
}

// ─────────────────────────────────────────────────────── connected calendars ──

/** Meeting providers: "app" is the app's own call; the others open in their own apps. */
export type CalendarConferenceProvider = "app" | "google_meet" | "teams" | "zoom" | (string & {});

/** One meeting provider a person can pick for "Video call" on a target calendar. */
export interface CalendarConferenceOption {
  id: CalendarConferenceProvider;
  label: string;
  /** Connections registry id (google, microsoft, zoom); absent for the app's own. */
  platform?: SourceProvider;
  connection_id?: string;
  account?: string;
  status?: "active" | "needs_reauth" | "blocked";
  fix_url?: string;
  /** Made by the target calendar's platform (Meet on Google events, Teams on Outlook events). */
  native?: boolean;
}

/** One of the person's connected calendars ("<connection>/<calendar>"). */
export interface CalendarCalendarRef {
  connection_id: string;
  calendar_id: string;
}

export interface CalendarConnectedCalendar {
  id: string;
  name: string;
  color: CalendarColor;
  hidden: boolean;
  /** Its busy time counts for booking links and find-time. */
  busy: boolean;
  primary: boolean;
  /** The account may add events and the connection allows changes. */
  can_write: boolean;
  access_role: string;
  timezone: string;
  /** Meeting links the platform makes on this calendar's events. */
  conferencing: CalendarConferenceOption[];
}

export interface CalendarSourceError {
  source_id: string;
  calendar_id?: string;
  code: string;
  message: string;
  fix_url?: string;
}

/** A place Calendar shows events from (shared `FeatureSource` plus calendars). */
export interface CalendarSource {
  id: string;
  type: "app" | "connected";
  provider?: SourceProvider;
  product?: string;
  account?: string;
  label: string;
  status?: "active" | "needs_reauth" | "blocked";
  capability?: string;
  fix_url?: string;
  can_write: boolean;
  /** Grants changes when can_write is false. */
  write_url?: string;
  calendars: CalendarConnectedCalendar[];
  error?: CalendarSourceError | null;
}

/** `GET /v1/calendar/sources` (shared `FeatureSources` plus meeting providers for the app's own events). */
export interface CalendarSources {
  sources: CalendarSource[];
  offers: ConnectionOffer[];
  conferencing: CalendarConferenceOption[];
}

/** An event from a connected calendar, drawn by the same views. `event_id` is "<connection>:<calendar>:<event>". */
export interface CalendarConnectedOccurrence extends CalendarOccurrence {
  source: Extract<SourceRef, { type: "connected" }>;
  conference_provider: string;
  connection_id: string;
  calendar_id: string;
  calendar_name: string;
  external_id: string;
  series_id: string;
  web_url: string;
  can_edit: boolean;
  can_respond: boolean;
}

export interface CalendarConnectedRange {
  items: CalendarConnectedOccurrence[];
  errors: CalendarSourceError[];
}

export interface CalendarConnectedAttendee {
  email: string;
  name: string;
  response: CalendarResponse;
  organizer: boolean;
  self: boolean;
  optional: boolean;
}

export interface CalendarConnectedEventDetail extends CalendarConnectedOccurrence {
  description: string;
  attendees: CalendarConnectedAttendee[];
  organizer: CalendarConnectedAttendee;
}

/** Create/update body for a connected event; the platform invites guests as it normally does. */
export interface CalendarConnectedEventInput {
  title?: string;
  description?: string;
  location?: string;
  starts_at?: string;
  ends_at?: string;
  start_date?: string;
  end_date?: string;
  timezone?: string;
  all_day?: boolean;
  recurrence?: CalendarEventInput["recurrence"];
  show_as?: CalendarShowAs;
  attendees?: CalendarAttendeeInput[];
  conference?: "add" | "remove";
  conference_provider?: string;
  conference_connection_id?: string;
  send_updates?: boolean;
  /** A series: "this" occurrence (default) or "all". */
  scope?: "this" | "all";
}

/** A connected event's place: connection, calendar and the platform's event id. */
export interface CalendarConnectedRef {
  connection_id: string;
  calendar_id: string;
  event_id: string;
}
