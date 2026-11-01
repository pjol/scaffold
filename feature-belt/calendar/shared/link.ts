/**
 * "New event" links: `/calendar?new=1&title=…&start=…&video=1&guests=…` opens the new-event
 * dialog (web) or form (mobile) prefilled, so other features (and the app's stitching) can
 * hand scheduling to the calendar. Nothing is saved until the person presses Add event.
 */
import { buildQueryString } from "../api/query-string";

/** What a new-event link prefills. Every field is optional. */
export interface CalendarNewEventDraft {
  title?: string;
  description?: string;
  /** ISO instant. Without it the event starts at the next half hour. */
  startsAt?: string;
  /** ISO instant after `startsAt`. Without it the event lasts `durationMinutes`. */
  endsAt?: string;
  /** Length when there's no `endsAt`: 5 to 1440 minutes, default 30. */
  durationMinutes?: number;
  /** Turn on "Add video call" (ignored when the app has no video calls). */
  video?: boolean;
  /** Guests: colleagues' user ids and email addresses (≤ 50). */
  guests?: readonly string[];
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const EMAIL = /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"]+$/;
const MAX_GUESTS = 50;

/** The app path that opens the calendar's new-event dialog with `draft` filled in. */
export function calendarNewEventLink(draft: CalendarNewEventDraft = {}): string {
  const guests = (draft.guests ?? []).filter((g) => UUID.test(g) || EMAIL.test(g)).slice(0, MAX_GUESTS);
  return (
    "/calendar" +
    buildQueryString({
      new: 1,
      title: draft.title?.trim() || undefined,
      details: draft.description?.trim() || undefined,
      start: draft.startsAt,
      end: draft.endsAt,
      duration: draft.durationMinutes,
      video: draft.video ? 1 : undefined,
      guests: guests.length ? guests.join(",") : undefined,
    })
  );
}

type LinkParam = string | readonly string[] | null | undefined;
/** Query parameters as routers hand them over (Next's searchParams, expo-router's params). */
type RawParams = { readonly [key: string]: LinkParam } & { new?: LinkParam; title?: LinkParam; details?: LinkParam; start?: LinkParam; end?: LinkParam; duration?: LinkParam; video?: LinkParam; guests?: LinkParam };

const one = (v: LinkParam): string => (typeof v === "string" ? v : (v?.[0] ?? "")).trim();
const instant = (v: string): string | undefined => {
  const t = v ? Date.parse(v) : NaN;
  return Number.isFinite(t) ? new Date(t).toISOString() : undefined;
};

/** What a link prefills, from its query parameters; null unless it asks for a new event (`new=1`). Bad values are dropped, not trusted. */
export function parseCalendarNewEventLink(params: RawParams): CalendarNewEventDraft | null {
  if (one(params.new) !== "1") return null;
  const draft: CalendarNewEventDraft = {};
  const title = one(params.title).slice(0, 200);
  if (title) draft.title = title;
  const details = one(params.details).slice(0, 8000);
  if (details) draft.description = details;
  const start = instant(one(params.start));
  const end = instant(one(params.end));
  if (start) {
    draft.startsAt = start;
    if (end && end > start) draft.endsAt = end;
  }
  const duration = Number(one(params.duration));
  if (Number.isInteger(duration) && duration >= 5 && duration <= 1440) draft.durationMinutes = duration;
  if (one(params.video) === "1") draft.video = true;
  const guests = [
    ...new Set(
      one(params.guests)
        .split(",")
        .map((g) => g.trim())
        .map((g) => (UUID.test(g) ? g.toLowerCase() : EMAIL.test(g) ? g.toLowerCase() : ""))
        .filter(Boolean),
    ),
  ].slice(0, MAX_GUESTS);
  if (guests.length) draft.guests = guests;
  return draft;
}

/** Splits a draft's guests into colleague ids and email addresses. */
export function calendarDraftGuests(draft: Pick<CalendarNewEventDraft, "guests">): { userIds: string[]; emails: string[] } {
  const userIds: string[] = [];
  const emails: string[] = [];
  for (const g of draft.guests ?? []) (UUID.test(g) ? userIds : emails).push(g);
  return { userIds, emails };
}

/** A draft's start and end: its own, else the next half hour from `now`, lasting its duration (default 30 minutes). */
export function calendarDraftTimes(draft: Pick<CalendarNewEventDraft, "startsAt" | "endsAt" | "durationMinutes">, now = Date.now()): { startsAt: string; endsAt: string } {
  const start = draft.startsAt ? Date.parse(draft.startsAt) : Math.ceil((now + 60_000) / 1_800_000) * 1_800_000;
  const end = draft.endsAt ? Date.parse(draft.endsAt) : start + (draft.durationMinutes ?? 30) * 60_000;
  return { startsAt: new Date(start).toISOString(), endsAt: new Date(end).toISOString() };
}
