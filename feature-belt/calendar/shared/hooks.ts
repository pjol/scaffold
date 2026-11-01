"use client";

import { keepPreviousData, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useMe } from "../hooks/common";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { useEffect } from "react";
import { calendarEndpoints } from "./api";
import { calendarDeviceZone } from "./time";
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

/** Query keys. `calendarKeys.all()` covers everything this feature caches. */
export const calendarKeys = {
  all: () => ["calendar"] as const,
  settings: () => ["calendar", "settings"] as const,
  ranges: () => ["calendar", "range"] as const,
  range: (from: string, to: string) => ["calendar", "range", from, to] as const,
  events: () => ["calendar", "event"] as const,
  event: (id: string, occurrence: string | null | undefined) => ["calendar", "event", id, occurrence ?? ""] as const,
  people: (q: string) => ["calendar", "people", q] as const,
  peopleByIds: (ids: readonly string[]) => ["calendar", "people", "ids", [...ids].sort().join(",")] as const,
  freeBusy: (ids: readonly string[], from: string, to: string) => ["calendar", "freebusy", [...ids].sort().join(","), from, to] as const,
  findTime: (ids: readonly string[], from: string, to: string, duration: number) => ["calendar", "find-time", [...ids].sort().join(","), from, to, duration] as const,
  subscriptions: () => ["calendar", "subscriptions"] as const,
  feed: () => ["calendar", "feed"] as const,
  links: () => ["calendar", "links"] as const,
  link: (id: string) => ["calendar", "link", id] as const,
  preview: (body: unknown) => ["calendar", "preview", JSON.stringify(body)] as const,
  bookingLists: () => ["calendar", "bookings"] as const,
  bookings: (role: string, when: string) => ["calendar", "bookings", role, when] as const,
  publicLink: (slug: string) => ["calendar", "book", slug] as const,
  publicSlots: (slug: string, from: string, to: string, duration: number) => ["calendar", "book", slug, "slots", from, to, duration] as const,
  manage: (token: string) => ["calendar", "manage", token] as const,
  manageSlots: (token: string, from: string, to: string) => ["calendar", "manage", token, "slots", from, to] as const,
  guest: (token: string) => ["calendar", "guest", token] as const,
} as const;

// ───────────────────────────────────────────────────────────── realtime ──

registerRealtimeHandler("calendar.changed", (qc, e) => {
  switch (e.data.scope) {
    case "events":
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
      void qc.invalidateQueries({ queryKey: calendarKeys.events() });
      break;
    case "bookings":
      void qc.invalidateQueries({ queryKey: calendarKeys.bookingLists() });
      void qc.invalidateQueries({ queryKey: calendarKeys.links() });
      break;
    case "links":
      void qc.invalidateQueries({ queryKey: calendarKeys.links() });
      break;
    case "subscriptions":
      void qc.invalidateQueries({ queryKey: calendarKeys.subscriptions() });
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
      break;
  }
});

registerReconnectRefetch(calendarKeys.ranges());
registerReconnectRefetch(calendarKeys.bookingLists());

function invalidateEvents(qc: QueryClient): void {
  void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
  void qc.invalidateQueries({ queryKey: calendarKeys.events() });
  void qc.invalidateQueries({ queryKey: calendarKeys.bookingLists() });
}

// ──────────────────────────────────────────────────────────────── reads ──

export function useCalendarSettings(options?: QueryOverrides<CalendarSettings>): UseQueryResult<CalendarSettings, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.settings(), ({ signal }) => api.settings({ signal }), options);
}

/** Occurrences in [from, to); keeps the previous window on screen while the next loads. */
export function useCalendarRange(from: string | null, to: string | null, options?: QueryOverrides<CalendarRange>): UseQueryResult<CalendarRange, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.range(from ?? "", to ?? ""), ({ signal }) => api.range(from ?? "", to ?? "", { signal }), { placeholderData: keepPreviousData, ...options }, !!from && !!to);
}

/** One event (or one occurrence of a series). */
export function useCalendarEvent(id: string | null | undefined, occurrence?: string | null, options?: QueryOverrides<CalendarEventDetail>): UseQueryResult<CalendarEventDetail, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.event(id ?? "", occurrence), ({ signal }) => api.event(id ?? "", occurrence, { signal }), options, !!id);
}

/** People from the caller's organizations (pass a debounced query). */
export function useCalendarPeople(q: string, options?: QueryOverrides<{ items: CalendarPerson[] }>): UseQueryResult<{ items: CalendarPerson[] }, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.people(q), ({ signal }) => api.people(q, { signal }), options);
}

/** Colleagues by id (a prefilled guest list); ids that aren't colleagues are left out. */
export function useCalendarPeopleByIds(ids: readonly string[], options?: QueryOverrides<{ items: CalendarPerson[] }>): UseQueryResult<{ items: CalendarPerson[] }, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.peopleByIds(ids), ({ signal }) => api.peopleByIds(ids, { signal }), options, ids.length > 0);
}

/** Colleagues' busy time (free/busy only). */
export function useCalendarFreeBusy(userIds: readonly string[], from: string, to: string, options?: QueryOverrides<{ items: CalendarFreeBusy[] }>): UseQueryResult<{ items: CalendarFreeBusy[] }, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.freeBusy(userIds, from, to), ({ signal }) => api.freeBusy([...userIds], from, to, { signal }), options, userIds.length > 0);
}

/** Times when the caller and the given colleagues are all free, in the caller's working hours. */
export function useCalendarFindTime(params: { user_ids: readonly string[]; from: string; to: string; duration: number } | null, options?: QueryOverrides<CalendarFreeTime>): UseQueryResult<CalendarFreeTime, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  const p = params ?? { user_ids: [], from: "", to: "", duration: 0 };
  return useApiQuery(calendarKeys.findTime(p.user_ids, p.from, p.to, p.duration), ({ signal }) => api.findTime({ ...p, user_ids: [...p.user_ids] }, { signal }), options, !!params);
}

export function useCalendarSubscriptions(options?: QueryOverrides<{ items: CalendarSubscription[] }>): UseQueryResult<{ items: CalendarSubscription[] }, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.subscriptions(), ({ signal }) => api.subscriptions({ signal }), options);
}

export function useCalendarPersonalFeed(options?: QueryOverrides<CalendarFeedState>): UseQueryResult<CalendarFeedState, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.feed(), ({ signal }) => api.feed({ signal }), options);
}

export function useCalendarLinks(options?: InfiniteOverrides<CalendarBookingLink>): PagedQueryResult<CalendarBookingLink> {
  const api = useEndpoints(calendarEndpoints);
  return usePagedQuery<CalendarBookingLink>(calendarKeys.links(), (cursor, signal) => api.links({ cursor, limit: 50 }, { signal }), options);
}

export function useCalendarLink(id: string | null | undefined, options?: QueryOverrides<CalendarBookingLink>): UseQueryResult<CalendarBookingLink, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.link(id ?? ""), ({ signal }) => api.link(id ?? "", { signal }), options, !!id);
}

/** Live preview: the slots a draft link would offer in [from, to). */
export function useCalendarLinkPreview(
  body: { link: CalendarBookingLinkInput; link_id?: string | null; from: string; to: string; duration?: number } | null,
  options?: QueryOverrides<{ slots: CalendarSlot[]; duration: number; timezone: string }>,
): UseQueryResult<{ slots: CalendarSlot[]; duration: number; timezone: string }, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.preview(body), ({ signal }) => api.previewLink(body ?? { link: {}, from: "", to: "" }, { signal }), { placeholderData: keepPreviousData, retry: false, ...options }, !!body);
}

export function useCalendarBookings(role: "host" | "booker", when: CalendarBookingWhen, options?: InfiniteOverrides<CalendarBooking>): PagedQueryResult<CalendarBooking> {
  const api = useEndpoints(calendarEndpoints);
  return usePagedQuery<CalendarBooking>(calendarKeys.bookings(role, when), (cursor, signal) => api.bookings({ role, when, cursor, limit: 25 }, { signal }), options);
}

/** A booking page (signed out, or signed in for internal links). */
export function useCalendarPublicLink(slug: string | null | undefined, options?: QueryOverrides<CalendarPublicLink>): UseQueryResult<CalendarPublicLink, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.publicLink(slug ?? ""), ({ signal }) => api.book.link(slug ?? "", { signal }), { retry: false, ...options }, !!slug);
}

export function useCalendarPublicSlots(slug: string | null | undefined, from: string, to: string, duration: number, options?: QueryOverrides<CalendarSlots>): UseQueryResult<CalendarSlots, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.publicSlots(slug ?? "", from, to, duration), ({ signal }) => api.book.slots(slug ?? "", { from, to, duration }, { signal }), { placeholderData: keepPreviousData, ...options }, !!slug && duration > 0);
}

export function useCalendarManagedBooking(token: string | null | undefined, options?: QueryOverrides<CalendarPublicBooking>): UseQueryResult<CalendarPublicBooking, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.manage(token ?? ""), ({ signal }) => api.manage.get(token ?? "", { signal }), { retry: false, ...options }, !!token);
}

export function useCalendarManageSlots(token: string | null | undefined, from: string, to: string, enabled: boolean, options?: QueryOverrides<CalendarSlots>): UseQueryResult<CalendarSlots, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.manageSlots(token ?? "", from, to), ({ signal }) => api.manage.slots(token ?? "", { from, to }, { signal }), { placeholderData: keepPreviousData, ...options }, !!token && enabled);
}

export function useCalendarGuestInvitation(token: string | null | undefined, options?: QueryOverrides<CalendarGuestInvitation>): UseQueryResult<CalendarGuestInvitation, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.guest(token ?? ""), ({ signal }) => api.guest.get(token ?? "", { signal }), { retry: false, ...options }, !!token);
}

// ──────────────────────────────────────────────────────────── mutations ──

export function useUpdateCalendarSettings(): UseMutationResult<CalendarSettings, ApiError, CalendarSettingsInput> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarSettings, ApiError, CalendarSettingsInput>({
    mutationFn: (body) => api.updateSettings(body),
    onSuccess: (s) => {
      qc.setQueryData(calendarKeys.settings(), s);
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

// The settings response a save was last tried for, shared by every caller (the session
// mount and the calendar): one attempt per signed-in person, no retry loop on failure.
let deviceZoneAskedFor: CalendarSettings | undefined;

/**
 * Saves the device's zone as the calendar zone when the person has none yet, so
 * invitations, reminders and agent previews are worded in their zone even before they
 * open the calendar. Waits until the account is active and the Terms are accepted (a
 * save before then is refused). Mounted once per session (`calendarZoneSync`); returns
 * the settings query.
 */
export function useCalendarDeviceZoneSave(): UseQueryResult<CalendarSettings, ApiError> {
  const settings = useCalendarSettings();
  const me = useMe();
  const { mutate } = useUpdateCalendarSettings();
  const data = settings.data;
  const ready = me.data?.status === "active" && !me.data.consent.required;
  useEffect(() => {
    if (!ready || !data || data.timezone_set || deviceZoneAskedFor === data) return;
    deviceZoneAskedFor = data;
    mutate({ timezone: calendarDeviceZone() });
  }, [ready, data, mutate]);
  return settings;
}

export function useCreateCalendarEvent(): UseMutationResult<CalendarEventDetail, ApiError, CalendarEventInput> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarEventDetail, ApiError, CalendarEventInput>({
    mutationFn: (body) => api.createEvent(body),
    onSuccess: () => invalidateEvents(qc),
  });
}

type RangeSnapshot = [readonly unknown[], CalendarRange | undefined][];

/**
 * Update an event. A time change on one occurrence (or a single event) moves it in every
 * cached window at once (drag to move/resize), rolled back if the server refuses.
 */
export function useUpdateCalendarEvent(): UseMutationResult<CalendarEventDetail, ApiError, { id: string; body: CalendarEventInput }, { snapshot: RangeSnapshot }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarEventDetail, ApiError, { id: string; body: CalendarEventInput }, { snapshot: RangeSnapshot }>({
    mutationFn: ({ id, body }) => api.updateEvent(id, body),
    onMutate: async ({ id, body }) => {
      await qc.cancelQueries({ queryKey: calendarKeys.ranges() });
      const snapshot = qc.getQueriesData<CalendarRange>({ queryKey: calendarKeys.ranges() }) as RangeSnapshot;
      const single = !body.scope || body.scope === "this";
      if (single && body.starts_at && body.ends_at && !body.all_day) {
        const starts = body.starts_at;
        const ends = body.ends_at;
        qc.setQueriesData<CalendarRange>({ queryKey: calendarKeys.ranges() }, (d) =>
          d
            ? {
                ...d,
                items: d.items.map((o) =>
                  o.event_id === id && (o.occurrence_start ?? null) === (body.occurrence_start ?? null) ? { ...o, starts_at: starts, ends_at: ends } : o,
                ),
              }
            : d,
        );
      }
      return { snapshot };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.snapshot ?? []) qc.setQueryData(key, data);
    },
    onSettled: () => invalidateEvents(qc),
  });
}

export function useDeleteCalendarEvent(): UseMutationResult<void, ApiError, { id: string; scope?: CalendarScope; occurrence?: string | null; reason?: string; send_updates?: boolean }, { snapshot: RangeSnapshot }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; scope?: CalendarScope; occurrence?: string | null; reason?: string; send_updates?: boolean }, { snapshot: RangeSnapshot }>({
    mutationFn: ({ id, ...params }) => api.deleteEvent(id, params),
    onMutate: async ({ id, scope, occurrence }) => {
      await qc.cancelQueries({ queryKey: calendarKeys.ranges() });
      const snapshot = qc.getQueriesData<CalendarRange>({ queryKey: calendarKeys.ranges() }) as RangeSnapshot;
      qc.setQueriesData<CalendarRange>({ queryKey: calendarKeys.ranges() }, (d) =>
        d
          ? {
              ...d,
              items: d.items.filter((o) => {
                if (o.event_id !== id) return true;
                if (!scope || scope === "all" || !o.occurrence_start || !occurrence) return false;
                if (scope === "this") return o.occurrence_start !== occurrence;
                return o.occurrence_start < occurrence;
              }),
            }
          : d,
      );
      return { snapshot };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.snapshot ?? []) qc.setQueryData(key, data);
    },
    onSettled: () => invalidateEvents(qc),
  });
}

/** Reply to an invitation (applies to the whole series). */
export function useRespondCalendarEvent(): UseMutationResult<CalendarEventDetail, ApiError, { id: string; response: Exclude<CalendarResponse, "needs_action"> }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarEventDetail, ApiError, { id: string; response: Exclude<CalendarResponse, "needs_action"> }>({
    mutationFn: ({ id, response }) => api.respond(id, response),
    onSuccess: () => invalidateEvents(qc),
  });
}

export function useAddCalendarSubscription(): UseMutationResult<CalendarSubscription, ApiError, CalendarSubscriptionInput> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarSubscription, ApiError, CalendarSubscriptionInput>({
    mutationFn: (body) => api.addSubscription(body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.subscriptions() });
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

export function useUpdateCalendarSubscription(): UseMutationResult<CalendarSubscription, ApiError, { id: string; name?: string; color?: string; enabled?: boolean }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarSubscription, ApiError, { id: string; name?: string; color?: string; enabled?: boolean }>({
    mutationFn: ({ id, ...body }) => api.updateSubscription(id, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.subscriptions() });
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

export function useRemoveCalendarSubscription(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.removeSubscription(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.subscriptions() });
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

export function useRefreshCalendarSubscription(): UseMutationResult<CalendarSubscription, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarSubscription, ApiError, string>({
    mutationFn: (id) => api.refreshSubscription(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.subscriptions() });
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

export function useRotateCalendarPersonalFeed(): UseMutationResult<CalendarFeedSecret, ApiError, void> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarFeedSecret, ApiError, void>({
    mutationFn: () => api.rotateFeed(),
    onSuccess: (f) => qc.setQueryData<CalendarFeedState>(calendarKeys.feed(), { active: true, created_at: f.created_at }),
  });
}

export function useDisableCalendarPersonalFeed(): UseMutationResult<void, ApiError, void> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: () => api.disableFeed(),
    onSuccess: () => qc.setQueryData<CalendarFeedState>(calendarKeys.feed(), { active: false, created_at: null }),
  });
}

export function useCreateCalendarLink(): UseMutationResult<CalendarBookingLink, ApiError, CalendarBookingLinkInput> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarBookingLink, ApiError, CalendarBookingLinkInput>({
    mutationFn: (body) => api.createLink(body),
    onSuccess: (l) => {
      qc.setQueryData(calendarKeys.link(l.id), l);
      void qc.invalidateQueries({ queryKey: calendarKeys.links() });
    },
  });
}

export function useUpdateCalendarLink(): UseMutationResult<CalendarBookingLink, ApiError, { id: string; body: CalendarBookingLinkInput }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarBookingLink, ApiError, { id: string; body: CalendarBookingLinkInput }>({
    mutationFn: ({ id, body }) => api.updateLink(id, body),
    onSuccess: (l) => {
      qc.setQueryData(calendarKeys.link(l.id), l);
      void qc.invalidateQueries({ queryKey: calendarKeys.links() });
    },
  });
}

export function useDeleteCalendarLink(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.deleteLink(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: calendarKeys.links() }),
  });
}

/** Host or booker cancels a booking from the app. */
export function useCancelCalendarBooking(): UseMutationResult<CalendarBooking, ApiError, { id: string; reason: string }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarBooking, ApiError, { id: string; reason: string }>({
    mutationFn: ({ id, reason }) => api.cancelBooking(id, reason),
    onSuccess: () => invalidateEvents(qc),
  });
}

export function useCreateCalendarBooking(slug: string): UseMutationResult<CalendarBookingConfirmation, ApiError, CalendarBookingRequest> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarBookingConfirmation, ApiError, CalendarBookingRequest>({
    mutationFn: (body) => api.book.create(slug, body),
    onSettled: () => void qc.invalidateQueries({ queryKey: calendarKeys.publicLink(slug) }),
  });
}

export function useCancelManagedCalendarBooking(token: string): UseMutationResult<CalendarPublicBooking, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarPublicBooking, ApiError, string>({
    mutationFn: (reason) => api.manage.cancel(token, reason),
    onSuccess: (b) => qc.setQueryData(calendarKeys.manage(token), b),
  });
}

export function useRescheduleCalendarBooking(token: string): UseMutationResult<CalendarPublicBooking, ApiError, { starts_at: string; timezone: string }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarPublicBooking, ApiError, { starts_at: string; timezone: string }>({
    mutationFn: (body) => api.manage.reschedule(token, body),
    onSuccess: (b) => {
      qc.setQueryData(calendarKeys.manage(token), b);
      void qc.invalidateQueries({ queryKey: ["calendar", "manage", token, "slots"] });
    },
  });
}

export function useRespondCalendarGuest(token: string): UseMutationResult<CalendarGuestInvitation, ApiError, Exclude<CalendarResponse, "needs_action">> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarGuestInvitation, ApiError, Exclude<CalendarResponse, "needs_action">>({
    mutationFn: (response) => api.guest.respond(token, response),
    onSuccess: (g) => qc.setQueryData(calendarKeys.guest(token), g),
  });
}
