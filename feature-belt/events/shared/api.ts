import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminEvent,
  AdminEventListParams,
  CalendarFeedStatus,
  CalendarFeedUrl,
  EventAttendee,
  EventInviteLink,
  EventInvitee,
  EventItem,
  EventListParams,
  EventRequest,
  PublicEvent,
  RsvpChoice,
  RsvpStatus,
} from "./types";

/** Upload purpose for cover images (`POST /v1/uploads`, public, jpeg/png/heic/webp, 10 MB). */
export const EVENT_COVER_PURPOSE = "event_cover";

/** Events endpoints (`/v1/events`, `/v1/admin/events`). */
export function eventsEndpoints(http: HttpClient) {
  const E = "/v1/events";
  return {
    /** Upcoming (soonest first), past (latest first) or a from/to window; events the caller can see. */
    list: (params: EventListParams = {}, o?: CallOptions) =>
      http.get<Page<EventItem>>(E, {
        ...o,
        query: { when: params.when, filter: params.filter, from: params.from, to: params.to, limit: params.limit, cursor: params.cursor },
      }),
    get: (id: string, o?: CallOptions) => http.get<EventItem>(`${E}/${seg(id)}`, o),
    create: (body: EventRequest, o?: CallOptions) => http.post<EventItem>(E, body, o),
    update: (id: string, body: EventRequest, o?: CallOptions) => http.put<EventItem>(`${E}/${seg(id)}`, body, o),
    cancel: (id: string, o?: CallOptions) => http.post<EventItem>(`${E}/${seg(id)}/cancel`, undefined, o),
    rsvp: (id: string, status: RsvpChoice, o?: CallOptions) => http.put<EventItem>(`${E}/${seg(id)}/rsvp`, { status }, o),
    clearRsvp: (id: string, o?: CallOptions) => http.delete<void>(`${E}/${seg(id)}/rsvp`, o),
    /** Hosts see every status; attendees all but declined. */
    attendees: (id: string, params: { status?: RsvpStatus; limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<EventAttendee>>(`${E}/${seg(id)}/attendees`, { ...o, query: { status: params.status, limit: params.limit, cursor: params.cursor } }),
    addCoHost: (id: string, userId: string, o?: CallOptions) => http.post<EventItem>(`${E}/${seg(id)}/cohosts`, { user_id: userId }, o),
    removeCoHost: (id: string, userId: string, o?: CallOptions) => http.delete<void>(`${E}/${seg(id)}/cohosts/${seg(userId)}`, o),
    invites: (id: string, params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
      http.get<Page<EventInvitee>>(`${E}/${seg(id)}/invites`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    invite: (id: string, userIds: string[], o?: CallOptions) => http.post<{ invited: number }>(`${E}/${seg(id)}/invites`, { user_ids: userIds }, o),
    uninvite: (id: string, userId: string, o?: CallOptions) => http.delete<void>(`${E}/${seg(id)}/invites/${seg(userId)}`, o),
    rotateInviteLink: (id: string, o?: CallOptions) => http.post<EventInviteLink>(`${E}/${seg(id)}/invite-link`, undefined, o),
    disableInviteLink: (id: string, o?: CallOptions) => http.delete<void>(`${E}/${seg(id)}/invite-link`, o),
    join: (code: string, o?: CallOptions) => http.post<EventItem>(`${E}/join`, { code }, o),
    /** The event as iCalendar text (signed in; save it as `event.ics`). */
    ics: (id: string, o?: CallOptions) => http.text("GET", `${E}/${seg(id)}.ics`, { ...o, accept: "text/calendar" }),

    calendarFeed: (o?: CallOptions) => http.get<CalendarFeedStatus>(`${E}/calendar-feed`, o),
    /** New secret feed URL; any earlier one stops working. */
    rotateCalendarFeed: (o?: CallOptions) => http.post<CalendarFeedUrl>(`${E}/calendar-feed`, undefined, o),
    deleteCalendarFeed: (o?: CallOptions) => http.delete<void>(`${E}/calendar-feed`, o),

    public: {
      list: (params: { limit?: number; cursor?: string } = {}, o?: CallOptions) =>
        http.get<Page<PublicEvent>>(`${E}/public`, { ...o, auth: false, query: { limit: params.limit, cursor: params.cursor } }),
      get: (id: string, o?: CallOptions) => http.get<PublicEvent>(`${E}/public/${seg(id)}`, { ...o, auth: false }),
      /** Direct link calendar apps and browsers can open (no auth). */
      icsUrl: (id: string) => `${http.baseUrl}${E}/public/${seg(id)}.ics`,
    },

    admin: {
      list: (params: AdminEventListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminEvent>>("/v1/admin/events", {
          ...o,
          query: { q: params.q, host_id: params.host_id, status: params.status, limit: params.limit, cursor: params.cursor },
        }),
      /** Cancel any scheduled event; attendees are told. Audited. */
      cancel: (id: string, reason?: string, o?: CallOptions) => http.post<void>(`/v1/admin/events/${seg(id)}/cancel`, { reason: reason ?? "" }, o),
    },
  };
}

export type EventsEndpoints = ReturnType<typeof eventsEndpoints>;
