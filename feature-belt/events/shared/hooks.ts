"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, useTopic, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import { topic } from "../realtime/topics";
import type { Page } from "../types";
import { eventsEndpoints } from "./api";
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

/** Query keys. `eventsKeys.all()` invalidates everything events caches. */
export const eventsKeys = {
  all: () => ["events"] as const,
  lists: () => ["events", "list"] as const,
  list: (params: Record<string, unknown> = {}) => ["events", "list", params] as const,
  event: (id: string) => ["events", "event", id] as const,
  attendees: (id: string, status: RsvpStatus) => ["events", "attendees", id, status] as const,
  invites: (id: string) => ["events", "invites", id] as const,
  calendarFeed: () => ["events", "calendar-feed"] as const,
  public: {
    all: () => ["events", "public"] as const,
    list: () => ["events", "public", "list"] as const,
    event: (id: string) => ["events", "public", "event", id] as const,
  },
  admin: {
    all: () => ["events", "admin"] as const,
    list: (params: Record<string, unknown> = {}) => ["events", "admin", "list", params] as const,
  },
} as const;

type Paged<T> = InfiniteData<Page<T>, string | undefined>;

/** Apply `fn` to event `id` in its detail and in every cached list. */
export function patchEvent(qc: QueryClient, id: string, fn: (e: EventItem) => EventItem): void {
  qc.setQueryData<EventItem>(eventsKeys.event(id), (e) => (e ? fn(e) : e));
  qc.setQueriesData<Paged<EventItem>>({ queryKey: eventsKeys.lists() }, (d) =>
    d ? { ...d, pages: d.pages.map((p) => ({ ...p, items: p.items.map((e) => (e.id === id ? fn(e) : e)) })) } : d,
  );
}

function storeEvent(qc: QueryClient, event: EventItem): void {
  qc.setQueryData(eventsKeys.event(event.id), event);
  patchEvent(qc, event.id, () => event);
}

// ───────────────────────────────────────────────────────────── realtime ──

registerRealtimeHandler("events.rsvp", (qc, e) => {
  const { event_id, going_count, maybe_count, waitlist_count, is_full } = e.data;
  patchEvent(qc, event_id, (ev) => ({ ...ev, going_count, maybe_count, waitlist_count, is_full }));
  void qc.invalidateQueries({ queryKey: ["events", "attendees", event_id] });
  // A waitlisted viewer may just have been promoted (or moved up): refetch their RSVP.
  if (qc.getQueryData<EventItem>(eventsKeys.event(event_id))?.my_rsvp?.status === "waitlisted") {
    void qc.invalidateQueries({ queryKey: eventsKeys.event(event_id) });
    void qc.invalidateQueries({ queryKey: eventsKeys.lists(), refetchType: "none" });
  }
});

registerRealtimeHandler("events.updated", (qc, e) => {
  void qc.invalidateQueries({ queryKey: eventsKeys.event(e.data.event_id) });
  void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
  void qc.invalidateQueries({ queryKey: eventsKeys.public.all() });
});

registerReconnectRefetch(eventsKeys.all());

/** Subscribe to one event's realtime topic while mounted (counts and changes). */
export function useEventTopic(id: string | null | undefined): void {
  useTopic(id ? topic("events", id) : null);
}

// ──────────────────────────────────────────────────────────────── reads ──

/** `GET /v1/events` as an infinite list. Default: upcoming, soonest first. */
export function useEvents(params: EventListParams = {}, options?: InfiniteOverrides<EventItem>): PagedQueryResult<EventItem> {
  const api = useEndpoints(eventsEndpoints);
  return usePagedQuery<EventItem>(eventsKeys.list(listKeyParams(params)), (cursor, signal) => api.list({ ...params, cursor }, { signal }), options);
}

/** `GET /v1/events/{id}`. Pass null to disable. */
export function useEvent(id: string | null | undefined, options?: QueryOverrides<EventItem>): UseQueryResult<EventItem, ApiError> {
  const api = useEndpoints(eventsEndpoints);
  return useApiQuery(eventsKeys.event(id ?? ""), ({ signal }) => api.get(id ?? "", { signal }), options, !!id);
}

/** Attendee list by status (hosts: every status; attendees: all but declined). */
export function useEventAttendees(id: string | null | undefined, status: RsvpStatus, options?: InfiniteOverrides<EventAttendee>): PagedQueryResult<EventAttendee> {
  const api = useEndpoints(eventsEndpoints);
  return usePagedQuery<EventAttendee>(eventsKeys.attendees(id ?? "", status), (cursor, signal) => api.attendees(id ?? "", { status, cursor, limit: 50 }, { signal }), options, !!id);
}

/** Hosts: who is invited. */
export function useEventInvites(id: string | null | undefined, options?: InfiniteOverrides<EventInvitee>): PagedQueryResult<EventInvitee> {
  const api = useEndpoints(eventsEndpoints);
  return usePagedQuery<EventInvitee>(eventsKeys.invites(id ?? ""), (cursor, signal) => api.invites(id ?? "", { cursor, limit: 50 }, { signal }), options, !!id);
}

/** Upcoming public events (works signed out). */
export function usePublicEvents(options?: InfiniteOverrides<PublicEvent>): PagedQueryResult<PublicEvent> {
  const api = useEndpoints(eventsEndpoints);
  return usePagedQuery<PublicEvent>(eventsKeys.public.list(), (cursor, signal) => api.public.list({ cursor }, { signal }), options);
}

/** One public event (works signed out; 404 for anything not public). */
export function usePublicEvent(id: string | null | undefined, options?: QueryOverrides<PublicEvent>): UseQueryResult<PublicEvent, ApiError> {
  const api = useEndpoints(eventsEndpoints);
  return useApiQuery(eventsKeys.public.event(id ?? ""), ({ signal }) => api.public.get(id ?? "", { signal }), options, !!id);
}

/** Whether the caller has a calendar feed URL. */
export function useCalendarFeed(options?: QueryOverrides<CalendarFeedStatus>): UseQueryResult<CalendarFeedStatus, ApiError> {
  const api = useEndpoints(eventsEndpoints);
  return useApiQuery(eventsKeys.calendarFeed(), ({ signal }) => api.calendarFeed({ signal }), options);
}

// ──────────────────────────────────────────────────────────── mutations ──

export function useCreateEvent(): UseMutationResult<EventItem, ApiError, EventRequest> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventItem, ApiError, EventRequest>({
    mutationFn: (body) => api.create(body),
    onSuccess: (event) => {
      qc.setQueryData(eventsKeys.event(event.id), event);
      void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
    },
  });
}

/** `PUT /v1/events/{id}` (hosts): replaces every editable field. */
export function useUpdateEvent(): UseMutationResult<EventItem, ApiError, { id: string; body: EventRequest }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventItem, ApiError, { id: string; body: EventRequest }>({
    mutationFn: ({ id, body }) => api.update(id, body),
    onSuccess: (event) => {
      storeEvent(qc, event);
      void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
    },
  });
}

/** Creator cancels; attendees are notified. */
export function useCancelEvent(): UseMutationResult<EventItem, ApiError, string> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventItem, ApiError, string>({
    mutationFn: (id) => api.cancel(id),
    onSuccess: (event) => {
      storeEvent(qc, event);
      void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
    },
  });
}

/** Set the caller's RSVP. Going on a full event comes back `waitlisted`. */
export function useRsvp(): UseMutationResult<EventItem, ApiError, { id: string; status: RsvpChoice }, { previous: EventItem | undefined }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventItem, ApiError, { id: string; status: RsvpChoice }, { previous: EventItem | undefined }>({
    mutationFn: ({ id, status }) => api.rsvp(id, status),
    onMutate: async ({ id, status }) => {
      await qc.cancelQueries({ queryKey: eventsKeys.event(id) });
      const previous = qc.getQueryData<EventItem>(eventsKeys.event(id));
      // Going may become waitlisted on the server; keep a waitlisted place until it answers.
      patchEvent(qc, id, (e) => (e.my_rsvp?.status === "waitlisted" && status === "going" ? e : { ...e, my_rsvp: { status, waitlist_position: null } }));
      return { previous };
    },
    onSuccess: (event) => storeEvent(qc, event),
    onError: (_e, { id }, ctx) => {
      if (ctx?.previous) storeEvent(qc, ctx.previous);
      else void qc.invalidateQueries({ queryKey: eventsKeys.event(id) });
    },
    onSettled: (_d, _e, { id }) => {
      void qc.invalidateQueries({ queryKey: ["events", "attendees", id] });
      void qc.invalidateQueries({ queryKey: eventsKeys.lists(), refetchType: "none" });
    },
  });
}

/** Withdraw the caller's RSVP. */
export function useClearRsvp(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.clearRsvp(id),
    onSuccess: (_d, id) => {
      patchEvent(qc, id, (e) => ({ ...e, my_rsvp: null }));
      void qc.invalidateQueries({ queryKey: eventsKeys.event(id) });
      void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
    },
  });
}

export function useAddCoHost(): UseMutationResult<EventItem, ApiError, { id: string; userId: string }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventItem, ApiError, { id: string; userId: string }>({
    mutationFn: ({ id, userId }) => api.addCoHost(id, userId),
    onSuccess: (event) => storeEvent(qc, event),
  });
}

/** The creator removes a co-host, or a co-host steps down. */
export function useRemoveCoHost(): UseMutationResult<void, ApiError, { id: string; userId: string }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; userId: string }>({
    mutationFn: ({ id, userId }) => api.removeCoHost(id, userId),
    onSuccess: (_d, { id }) => void qc.invalidateQueries({ queryKey: eventsKeys.event(id) }),
  });
}

export function useInviteToEvent(): UseMutationResult<{ invited: number }, ApiError, { id: string; userIds: string[] }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<{ invited: number }, ApiError, { id: string; userIds: string[] }>({
    mutationFn: ({ id, userIds }) => api.invite(id, userIds),
    onSuccess: (_d, { id }) => void qc.invalidateQueries({ queryKey: eventsKeys.invites(id) }),
  });
}

/** Withdraw an invite (on invite-only events this drops their RSVP too). */
export function useUninvite(): UseMutationResult<void, ApiError, { id: string; userId: string }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; userId: string }>({
    mutationFn: ({ id, userId }) => api.uninvite(id, userId),
    onSuccess: (_d, { id }) => {
      void qc.invalidateQueries({ queryKey: eventsKeys.invites(id) });
      void qc.invalidateQueries({ queryKey: eventsKeys.event(id) });
    },
  });
}

/** New invite link code (the old one stops working). */
export function useRotateInviteLink(): UseMutationResult<EventInviteLink, ApiError, string> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventInviteLink, ApiError, string>({
    mutationFn: (id) => api.rotateInviteLink(id),
    onSuccess: (link, id) => patchEvent(qc, id, (e) => ({ ...e, invite_code: link.code })),
  });
}

export function useDisableInviteLink(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.disableInviteLink(id),
    onSuccess: (_d, id) => patchEvent(qc, id, (e) => ({ ...e, invite_code: null })),
  });
}

/** Join an invite-only event with a link code; resolves to the event. */
export function useJoinEvent(): UseMutationResult<EventItem, ApiError, string> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<EventItem, ApiError, string>({
    mutationFn: (code) => api.join(code),
    onSuccess: (event) => {
      qc.setQueryData(eventsKeys.event(event.id), event);
      void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
    },
  });
}

/** The event as iCalendar text; the app saves or shares it. */
export function useEventIcs(): UseMutationResult<string, ApiError, string> {
  const api = useEndpoints(eventsEndpoints);
  return useMutation<string, ApiError, string>({ mutationFn: (id) => api.ics(id) });
}

/** Create (or replace) the caller's calendar feed URL. The URL is shown once. */
export function useRotateCalendarFeed(): UseMutationResult<CalendarFeedUrl, ApiError, void> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarFeedUrl, ApiError, void>({
    mutationFn: () => api.rotateCalendarFeed(),
    onSuccess: (feed) => qc.setQueryData<CalendarFeedStatus>(eventsKeys.calendarFeed(), { active: true, created_at: feed.created_at }),
  });
}

export function useDeleteCalendarFeed(): UseMutationResult<void, ApiError, void> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: () => api.deleteCalendarFeed(),
    onSuccess: () => qc.setQueryData<CalendarFeedStatus>(eventsKeys.calendarFeed(), { active: false, created_at: null }),
  });
}

// ──────────────────────────────────────────────────────────────── admin ──

export function useAdminEvents(params: AdminEventListParams = {}, options?: InfiniteOverrides<AdminEvent>): PagedQueryResult<AdminEvent> {
  const api = useEndpoints(eventsEndpoints);
  return usePagedQuery<AdminEvent>(eventsKeys.admin.list(listKeyParams(params)), (cursor, signal) => api.admin.list({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** Staff cancel (audited); attendees are notified. */
export function useAdminCancelEvent(): UseMutationResult<void, ApiError, { id: string; reason?: string }> {
  const api = useEndpoints(eventsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; reason?: string }>({
    mutationFn: ({ id, reason }) => api.admin.cancel(id, reason),
    onSuccess: (_d, { id }) => {
      void qc.invalidateQueries({ queryKey: eventsKeys.admin.all() });
      void qc.invalidateQueries({ queryKey: eventsKeys.event(id) });
      void qc.invalidateQueries({ queryKey: eventsKeys.lists() });
    },
  });
}
