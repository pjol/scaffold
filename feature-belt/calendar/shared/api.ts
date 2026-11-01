import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  CalendarBooking,
  CalendarBookingConfirmation,
  CalendarBookingLink,
  CalendarBookingLinkInput,
  CalendarBookingRequest,
  CalendarBookingWhen,
  CalendarEventDetail,
  CalendarEventInput,
  CalendarFeedSecret,
  CalendarFeedState,
  CalendarFreeBusy,
  CalendarFreeTime,
  CalendarGuestInvitation,
  CalendarPerson,
  CalendarPublicBooking,
  CalendarPublicLink,
  CalendarRange,
  CalendarResponse,
  CalendarScope,
  CalendarSettings,
  CalendarSettingsInput,
  CalendarSlot,
  CalendarSlots,
  CalendarSubscription,
  CalendarSubscriptionInput,
} from "./types";

/** Calendar endpoints (`/v1/calendar`). Public booking, manage and RSVP calls work signed out. */
export function calendarEndpoints(http: HttpClient) {
  const C = "/v1/calendar";
  return {
    settings: (o?: CallOptions) => http.get<CalendarSettings>(`${C}/settings`, o),
    updateSettings: (body: CalendarSettingsInput, o?: CallOptions) => http.put<CalendarSettings>(`${C}/settings`, body, o),

    /** Occurrences on the caller's calendar in [from, to) (≤ 62 days) plus subscribed busy time. */
    range: (from: string, to: string, o?: CallOptions) => http.get<CalendarRange>(`${C}/events`, { ...o, query: { from, to } }),
    event: (id: string, occurrence?: string | null, o?: CallOptions) =>
      http.get<CalendarEventDetail>(`${C}/events/${seg(id)}`, { ...o, query: { occurrence: occurrence ?? undefined } }),
    createEvent: (body: CalendarEventInput, o?: CallOptions) => http.post<CalendarEventDetail>(`${C}/events`, body, o),
    updateEvent: (id: string, body: CalendarEventInput, o?: CallOptions) => http.patch<CalendarEventDetail>(`${C}/events/${seg(id)}`, body, o),
    deleteEvent: (id: string, params: { scope?: CalendarScope; occurrence?: string | null; reason?: string; send_updates?: boolean } = {}, o?: CallOptions) =>
      http.delete<void>(`${C}/events/${seg(id)}`, {
        ...o,
        query: {
          scope: params.scope,
          occurrence: params.occurrence ?? undefined,
          reason: params.reason || undefined,
          send_updates: params.send_updates === false ? "false" : undefined,
        },
      }),
    respond: (id: string, response: Exclude<CalendarResponse, "needs_action">, o?: CallOptions) =>
      http.put<CalendarEventDetail>(`${C}/events/${seg(id)}/rsvp`, { response }, o),
    /** The event (whole series) as iCalendar text. */
    eventIcs: (id: string, o?: CallOptions) => http.text("GET", `${C}/events/${seg(id)}.ics`, { ...o, accept: "text/calendar" }),

    /** People who share an organization with the caller. */
    people: (q: string, o?: CallOptions) => http.get<{ items: CalendarPerson[] }>(`${C}/people`, { ...o, query: { q } }),
    /** Those of `ids` (≤ 50) who share an organization with the caller; others are left out. */
    peopleByIds: (ids: readonly string[], o?: CallOptions) => http.get<{ items: CalendarPerson[] }>(`${C}/people`, { ...o, query: { ids: ids.join(",") } }),
    freeBusy: (userIds: string[], from: string, to: string, o?: CallOptions) =>
      http.get<{ items: CalendarFreeBusy[] }>(`${C}/freebusy`, { ...o, query: { user_ids: userIds.join(","), from, to } }),
    findTime: (params: { user_ids: string[]; from: string; to: string; duration: number }, o?: CallOptions) =>
      http.get<CalendarFreeTime>(`${C}/find-time`, { ...o, query: { user_ids: params.user_ids.join(","), from: params.from, to: params.to, duration: params.duration } }),

    subscriptions: (o?: CallOptions) => http.get<{ items: CalendarSubscription[] }>(`${C}/subscriptions`, o),
    addSubscription: (body: CalendarSubscriptionInput, o?: CallOptions) => http.post<CalendarSubscription>(`${C}/subscriptions`, body, { ...o, timeoutMs: 30_000 }),
    updateSubscription: (id: string, body: { name?: string; color?: string; enabled?: boolean }, o?: CallOptions) =>
      http.patch<CalendarSubscription>(`${C}/subscriptions/${seg(id)}`, body, o),
    removeSubscription: (id: string, o?: CallOptions) => http.delete<void>(`${C}/subscriptions/${seg(id)}`, o),
    refreshSubscription: (id: string, o?: CallOptions) => http.post<CalendarSubscription>(`${C}/subscriptions/${seg(id)}/refresh`, undefined, { ...o, timeoutMs: 30_000 }),

    feed: (o?: CallOptions) => http.get<CalendarFeedState>(`${C}/feed`, o),
    /** A new secret feed URL; any earlier one stops working. */
    rotateFeed: (o?: CallOptions) => http.post<CalendarFeedSecret>(`${C}/feed`, undefined, o),
    disableFeed: (o?: CallOptions) => http.delete<void>(`${C}/feed`, o),

    links: (params: { cursor?: string; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<CalendarBookingLink>>(`${C}/links`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    link: (id: string, o?: CallOptions) => http.get<CalendarBookingLink>(`${C}/links/${seg(id)}`, o),
    createLink: (body: CalendarBookingLinkInput, o?: CallOptions) => http.post<CalendarBookingLink>(`${C}/links`, body, o),
    updateLink: (id: string, body: CalendarBookingLinkInput, o?: CallOptions) => http.patch<CalendarBookingLink>(`${C}/links/${seg(id)}`, body, o),
    deleteLink: (id: string, o?: CallOptions) => http.delete<void>(`${C}/links/${seg(id)}`, o),
    /** Slots a draft link would offer, from the owner's real calendar (the editor's live preview). */
    previewLink: (body: { link: CalendarBookingLinkInput; link_id?: string | null; from: string; to: string; duration?: number }, o?: CallOptions) =>
      http.post<{ slots: CalendarSlot[]; duration: number; timezone: string }>(`${C}/links/preview`, body, o),

    bookings: (params: { role?: "host" | "booker"; when?: CalendarBookingWhen; cursor?: string; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<CalendarBooking>>(`${C}/bookings`, { ...o, query: { role: params.role, when: params.when, cursor: params.cursor, limit: params.limit } }),
    booking: (id: string, o?: CallOptions) => http.get<CalendarBooking>(`${C}/bookings/${seg(id)}`, o),
    cancelBooking: (id: string, reason: string, o?: CallOptions) => http.post<CalendarBooking>(`${C}/bookings/${seg(id)}/cancel`, { reason }, o),

    /** Booking pages. Signed-in callers send their token (internal links need it). */
    book: {
      link: (slug: string, o?: CallOptions) => http.get<CalendarPublicLink>(`${C}/book/${seg(slug)}`, o),
      slots: (slug: string, params: { from: string; to: string; duration: number }, o?: CallOptions) =>
        http.get<CalendarSlots>(`${C}/book/${seg(slug)}/slots`, { ...o, query: params }),
      create: (slug: string, body: CalendarBookingRequest, o?: CallOptions) => http.post<CalendarBookingConfirmation>(`${C}/book/${seg(slug)}`, body, o),
    },

    /** The booker's manage link (the token is the credential). */
    manage: {
      get: (token: string, o?: CallOptions) => http.get<CalendarPublicBooking>(`${C}/manage/${seg(token)}`, { ...o, auth: false }),
      slots: (token: string, params: { from: string; to: string }, o?: CallOptions) =>
        http.get<CalendarSlots>(`${C}/manage/${seg(token)}/slots`, { ...o, auth: false, query: params }),
      cancel: (token: string, reason: string, o?: CallOptions) => http.post<CalendarPublicBooking>(`${C}/manage/${seg(token)}/cancel`, { reason }, { ...o, auth: false }),
      reschedule: (token: string, body: { starts_at: string; timezone: string }, o?: CallOptions) =>
        http.post<CalendarPublicBooking>(`${C}/manage/${seg(token)}/reschedule`, body, { ...o, auth: false }),
      /** Direct link browsers and calendar apps can open. */
      icsUrl: (token: string) => `${http.baseUrl}${C}/manage/${seg(token)}.ics`,
    },

    /** An external guest's RSVP link. */
    guest: {
      get: (token: string, o?: CallOptions) => http.get<CalendarGuestInvitation>(`${C}/rsvp/${seg(token)}`, { ...o, auth: false }),
      respond: (token: string, response: Exclude<CalendarResponse, "needs_action">, o?: CallOptions) =>
        http.post<CalendarGuestInvitation>(`${C}/rsvp/${seg(token)}`, { response }, { ...o, auth: false }),
    },
  };
}

export type CalendarEndpoints = ReturnType<typeof calendarEndpoints>;
