import type {
  CalendarCalendarRef,
  CalendarConferenceOption,
  CalendarConnectedCalendar,
  CalendarConnectedOccurrence,
  CalendarConnectedRef,
  CalendarOccurrence,
  CalendarSource,
  CalendarSources,
} from "./types";

/** True for an event from a connected calendar (it carries `source`). */
export function isCalendarConnected(o: CalendarOccurrence | null | undefined): o is CalendarConnectedOccurrence {
  return !!o && o.source?.type === "connected";
}

/** A connected event's place from its occurrence. */
export function calendarConnectedRef(o: Pick<CalendarConnectedOccurrence, "connection_id" | "calendar_id" | "external_id">): CalendarConnectedRef {
  return { connection_id: o.connection_id, calendar_id: o.calendar_id, event_id: o.external_id };
}

/** "<connection>:<calendar>:<event>" (an occurrence's `event_id`, search links' `open=`) back into its parts. */
export function parseCalendarConnectedRef(ref: string | null | undefined): CalendarConnectedRef | null {
  if (!ref) return null;
  const a = ref.indexOf(":");
  const b = a < 0 ? -1 : ref.indexOf(":", a + 1);
  if (a <= 0 || b <= a + 1 || b === ref.length - 1) return null;
  return { connection_id: ref.slice(0, a), calendar_id: ref.slice(a + 1, b), event_id: ref.slice(b + 1) };
}

/** Writable connected calendars, for a "Calendar" picker (each with its source). */
export function calendarWritableCalendars(sources: CalendarSources | undefined): Array<{ source: CalendarSource; calendar: CalendarConnectedCalendar }> {
  const out: Array<{ source: CalendarSource; calendar: CalendarConnectedCalendar }> = [];
  for (const s of sources?.sources ?? []) {
    if (s.type !== "connected" || s.status !== "active" || !s.can_write) continue;
    for (const c of s.calendars) if (c.can_write) out.push({ source: s, calendar: c });
  }
  return out;
}

/** The connected calendar a ref names, with its source. */
export function calendarFindCalendar(sources: CalendarSources | undefined, ref: CalendarCalendarRef | null | undefined): { source: CalendarSource; calendar: CalendarConnectedCalendar } | null {
  if (!ref) return null;
  const source = sources?.sources.find((s) => s.id === ref.connection_id);
  const calendar = source?.calendars.find((c) => c.id === ref.calendar_id);
  return source && calendar ? { source, calendar } : null;
}

/**
 * Meeting providers for a target calendar (null: the app's own): the app's options (its own
 * call, connected Zoom/Teams), then the platform's own link on a connected calendar, which
 * replaces the same provider made another way (the server applies the same rule).
 */
export function calendarConferenceOptions(sources: CalendarSources | undefined, target: CalendarCalendarRef | null | undefined): CalendarConferenceOption[] {
  const base = (sources?.conferencing ?? []).filter((o) => o.status !== "blocked");
  const found = calendarFindCalendar(sources, target);
  if (!found) return base;
  const native = found.calendar.conferencing;
  return [...base.filter((o) => !native.some((n) => n.id === o.id)), ...native];
}

const PROVIDER_LABELS: Record<string, string> = { google_meet: "Google Meet", teams: "Microsoft Teams", zoom: "Zoom" };
const PROVIDER_PLATFORMS: Record<string, string> = { google_meet: "google", teams: "microsoft", zoom: "zoom" };

/** What people call a meeting provider ("Video call" for the app's own or an unknown one). */
export function calendarProviderLabel(id: string | null | undefined): string {
  return (id && PROVIDER_LABELS[id]) || "Video call";
}

/** The platform id (for `ProviderIcon`) of a meeting provider, or null for the app's own. */
export function calendarProviderPlatform(id: string | null | undefined): string | null {
  return (id && PROVIDER_PLATFORMS[id]) || null;
}

/** The product (for `ProviderIcon` / `SourceBadge`) of a meeting provider: meet, teams, meetings. */
export function calendarProviderProduct(id: string | null | undefined): string | null {
  return id === "google_meet" ? "meet" : id === "teams" ? "teams" : id === "zoom" ? "meetings" : null;
}

/** The provider a meeting link belongs to, from its address (app events store only the link). */
export function calendarConferenceProvider(url: string | null | undefined, given?: string | null): string {
  if (given) return given;
  if (!url) return "";
  // The host by hand: URL is incomplete in React Native.
  const host = (/^https?:\/\/([^/?#:]+)/i.exec(url)?.[1] ?? "").toLowerCase();
  if (!host) return "";
  if (host === "meet.google.com") return "google_meet";
  if (host === "zoom.us" || host.endsWith(".zoom.us")) return "zoom";
  if (host === "teams.microsoft.com" || host === "teams.live.com") return "teams";
  return "app";
}

/** "Join on Zoom" for platform meetings, "Join video call" for the app's own. */
export function calendarJoinLabel(provider: string | null | undefined): string {
  return provider && PROVIDER_LABELS[provider] ? `Join on ${PROVIDER_LABELS[provider]}` : "Join video call";
}
