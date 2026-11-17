"use client";

/** Calendar 1.3.0 hooks: several calendars and sharing, rooms, proposals, search, team-link reassignment. */

import { keepPreviousData, useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, type QueryOverrides } from "../hooks/internal";
import { useActiveOrgId } from "../orgs/hooks";
import { calendarEndpoints } from "./api";
import { calendarKeys, invalidateCalendarEvents } from "./hooks";
import type {
  CalendarBooking,
  CalendarCalendar,
  CalendarCalendarInput,
  CalendarCalendarPrefs,
  CalendarCalendars,
  CalendarEventDetail,
  CalendarRoom,
  CalendarRoomInput,
  CalendarRooms,
  CalendarSearchHit,
  CalendarShareLevel,
} from "./types";

// ─────────────────────────────────────────────────────────── calendars ──

/** Your calendars (primary first), your teams' and the ones you added; `shared` lists the rest shared with you. */
export function useCalendarCalendars(options?: QueryOverrides<CalendarCalendars>): UseQueryResult<CalendarCalendars, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  const orgId = useActiveOrgId();
  return useApiQuery(calendarKeys.calendars(), ({ signal }) => api.calendars(orgId, { signal }), options);
}

export function useCreateCalendarCalendar(): UseMutationResult<CalendarCalendar, ApiError, CalendarCalendarInput> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarCalendar, ApiError, CalendarCalendarInput>({
    mutationFn: (body) => api.createCalendar(body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: calendarKeys.calendars() }),
  });
}

export function useUpdateCalendarCalendar(): UseMutationResult<CalendarCalendar, ApiError, { id: string; body: CalendarCalendarInput }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarCalendar, ApiError, { id: string; body: CalendarCalendarInput }>({
    mutationFn: ({ id, body }) => api.updateCalendar(id, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.calendars() });
      void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

/** Your own view of a calendar: colour, hidden, in your list. Hiding is optimistic. */
export function useCalendarCalendarPrefs(): UseMutationResult<CalendarCalendar, ApiError, { id: string } & CalendarCalendarPrefs, { prev: CalendarCalendars | undefined }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarCalendar, ApiError, { id: string } & CalendarCalendarPrefs, { prev: CalendarCalendars | undefined }>({
    mutationFn: ({ id, ...body }) => api.calendarPrefs(id, body),
    onMutate: async ({ id, hidden, color }) => {
      await qc.cancelQueries({ queryKey: calendarKeys.calendars() });
      const prev = qc.getQueryData<CalendarCalendars>(calendarKeys.calendars());
      if (prev) {
        const patch = (c: CalendarCalendar): CalendarCalendar =>
          c.id !== id ? c : { ...c, ...(hidden !== undefined ? { hidden } : {}), ...(color !== undefined ? { color: color ?? c.own_color } : {}) };
        qc.setQueryData<CalendarCalendars>(calendarKeys.calendars(), { items: prev.items.map(patch), shared: prev.shared.map(patch) });
      }
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(calendarKeys.calendars(), ctx.prev);
    },
    onSettled: (_d, _e, v) => {
      void qc.invalidateQueries({ queryKey: calendarKeys.calendars() });
      if (v.added !== undefined) void qc.invalidateQueries({ queryKey: calendarKeys.ranges() });
    },
  });
}

export function useDeleteCalendarCalendar(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.deleteCalendar(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.calendars() });
      invalidateCalendarEvents(qc);
    },
  });
}

/** What each sharing level lets someone do with a calendar, in the product's words. */
export const CALENDAR_SHARE_LEVEL_LABEL: Record<CalendarShareLevel, string> = {
  view: "See only free/busy",
  comment: "See all event details",
  edit: "Make changes to events",
  manage: "Make changes and manage sharing",
};

// ─────────────────────────────────────────────────────────── rooms ──

export interface CalendarRoomQuery {
  org_id: string;
  from?: string;
  to?: string;
  capacity?: number;
  q?: string;
  exclude_event?: string;
}

/** A workspace's rooms; with from/to each carries its busy times and whether it's free throughout. */
export function useCalendarRooms(params: CalendarRoomQuery | null, options?: QueryOverrides<CalendarRooms>): UseQueryResult<CalendarRooms, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  return useApiQuery(calendarKeys.roomList(params), ({ signal }) => api.rooms(params as CalendarRoomQuery, { signal }), { placeholderData: keepPreviousData, ...options }, !!params?.org_id);
}

export function useSaveCalendarRoom(): UseMutationResult<CalendarRoom, ApiError, { id?: string; body: CalendarRoomInput }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarRoom, ApiError, { id?: string; body: CalendarRoomInput }>({
    mutationFn: ({ id, body }) => (id ? api.updateRoom(id, body) : api.createRoom(body)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: calendarKeys.rooms() }),
  });
}

export function useDeleteCalendarRoom(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.deleteRoom(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: calendarKeys.rooms() });
      invalidateCalendarEvents(qc);
    },
  });
}

// ─────────────────────────────────────────────────────────── proposals ──

export function useProposeCalendarTime(): UseMutationResult<CalendarEventDetail, ApiError, { id: string; starts_at: string; ends_at: string; note?: string }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarEventDetail, ApiError, { id: string; starts_at: string; ends_at: string; note?: string }>({
    mutationFn: ({ id, ...body }) => api.propose(id, body),
    onSuccess: () => invalidateCalendarEvents(qc),
  });
}

export function useWithdrawCalendarProposal(): UseMutationResult<CalendarEventDetail, ApiError, string> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarEventDetail, ApiError, string>({
    mutationFn: (id) => api.withdrawProposal(id),
    onSuccess: () => invalidateCalendarEvents(qc),
  });
}

export function useAnswerCalendarProposal(): UseMutationResult<CalendarEventDetail, ApiError, { id: string; attendeeId: string; accept: boolean }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarEventDetail, ApiError, { id: string; attendeeId: string; accept: boolean }>({
    mutationFn: ({ id, attendeeId, accept }) => api.answerProposal(id, attendeeId, accept),
    onSuccess: () => invalidateCalendarEvents(qc),
  });
}

// ─────────────────────────────────────────────────────────── search, bookings ──

/** Events matching q (two characters or more) on your calendars and calendars shared with you with details. */
export function useCalendarSearch(q: string, options?: QueryOverrides<{ items: CalendarSearchHit[] }>): UseQueryResult<{ items: CalendarSearchHit[] }, ApiError> {
  const api = useEndpoints(calendarEndpoints);
  const term = q.trim();
  return useApiQuery(calendarKeys.search(term), ({ signal }) => api.search(term, { signal }), { placeholderData: keepPreviousData, ...options }, term.length >= 2);
}

/** Hand a round-robin booking to another host of its link. */
export function useReassignCalendarBooking(): UseMutationResult<CalendarBooking, ApiError, { id: string; userId: string }> {
  const api = useEndpoints(calendarEndpoints);
  const qc = useQueryClient();
  return useMutation<CalendarBooking, ApiError, { id: string; userId: string }>({
    mutationFn: ({ id, userId }) => api.reassignBooking(id, userId),
    onSuccess: () => invalidateCalendarEvents(qc),
  });
}
