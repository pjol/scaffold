/**
 * Calendar date and time-zone helpers on `Intl.DateTimeFormat` only (browsers and Hermes).
 * Grids work in date keys (`YYYY-MM-DD`) for a chosen zone; instants are UTC ISO strings.
 */
import type { CalendarOccurrence, CalendarRecurrence } from "./types";

const fmtCache = new Map<string, Intl.DateTimeFormat>();

function parts(timeZone: string): Intl.DateTimeFormat {
  let f = fmtCache.get(timeZone);
  if (!f) {
    f = new Intl.DateTimeFormat("en-US", {
      timeZone,
      hourCycle: "h23",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
    fmtCache.set(timeZone, f);
  }
  return f;
}

/** Whether `timeZone` is an IANA zone this runtime knows. */
export function calendarIsValidZone(timeZone: string): boolean {
  try {
    parts(timeZone);
    return true;
  } catch {
    return false;
  }
}

/** The device's zone, falling back to UTC. */
export function calendarDeviceZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

interface Wall {
  y: number;
  mo: number;
  d: number;
  h: number;
  mi: number;
  s: number;
}

function wall(ms: number, timeZone: string): Wall {
  const out: Wall = { y: 0, mo: 0, d: 0, h: 0, mi: 0, s: 0 };
  for (const p of parts(timeZone).formatToParts(new Date(ms))) {
    const v = Number(p.value);
    if (p.type === "year") out.y = v;
    else if (p.type === "month") out.mo = v;
    else if (p.type === "day") out.d = v;
    else if (p.type === "hour") out.h = v === 24 ? 0 : v;
    else if (p.type === "minute") out.mi = v;
    else if (p.type === "second") out.s = v;
  }
  return out;
}

function offsetMs(ms: number, timeZone: string): number {
  const w = wall(ms, timeZone);
  return Date.UTC(w.y, w.mo - 1, w.d, w.h, w.mi, w.s) - (ms - (ms % 1000));
}

const pad = (n: number) => String(n).padStart(2, "0");

function keyOf(y: number, mo: number, d: number): string {
  return `${y}-${pad(mo)}-${pad(d)}`;
}

function parseKey(key: string): { y: number; mo: number; d: number } {
  const [y, mo, d] = key.split("-").map(Number) as [number, number, number];
  return { y, mo, d };
}

/** The calendar date of an instant in `timeZone`: `YYYY-MM-DD`. */
export function calendarDateKey(iso: string | number, timeZone: string): string {
  const w = wall(typeof iso === "number" ? iso : new Date(iso).getTime(), timeZone);
  return keyOf(w.y, w.mo, w.d);
}

/** Today's date key in `timeZone`. */
export function calendarTodayKey(timeZone: string): string {
  return calendarDateKey(Date.now(), timeZone);
}

/** Minutes since local midnight of an instant in `timeZone`. */
export function calendarMinutesOfDay(iso: string | number, timeZone: string): number {
  const w = wall(typeof iso === "number" ? iso : new Date(iso).getTime(), timeZone);
  return w.h * 60 + w.mi;
}

/**
 * Wall-clock `minutes` after midnight on `dateKey` in `timeZone`, as a UTC ISO string. In a
 * daylight-saving gap the time moves forward by the gap. Minutes may exceed 1440.
 */
export function calendarFromWall(dateKey: string, minutes: number, timeZone: string): string {
  const { y, mo, d } = parseKey(dateKey);
  const guess = Date.UTC(y, mo - 1, d, 0, minutes);
  let ms = guess - offsetMs(guess, timeZone);
  const second = offsetMs(ms, timeZone);
  if (second !== offsetMs(guess, timeZone)) ms = guess - second;
  return new Date(ms).toISOString();
}

/** Local midnight of `dateKey` in `timeZone` (UTC ISO). */
export function calendarDayStart(dateKey: string, timeZone: string): string {
  return calendarFromWall(dateKey, 0, timeZone);
}

/** `dateKey` plus `n` days. */
export function calendarAddDays(dateKey: string, n: number): string {
  const { y, mo, d } = parseKey(dateKey);
  const t = new Date(Date.UTC(y, mo - 1, d + n));
  return keyOf(t.getUTCFullYear(), t.getUTCMonth() + 1, t.getUTCDate());
}

/** Days from `a` to `b` (both date keys). */
export function calendarDaysBetween(a: string, b: string): number {
  const pa = parseKey(a);
  const pb = parseKey(b);
  return Math.round((Date.UTC(pb.y, pb.mo - 1, pb.d) - Date.UTC(pa.y, pa.mo - 1, pa.d)) / 86_400_000);
}

/** Weekday of a date key: 0 = Sunday. */
export function calendarWeekday(dateKey: string): number {
  const { y, mo, d } = parseKey(dateKey);
  return new Date(Date.UTC(y, mo - 1, d)).getUTCDay();
}

/** First day of the week containing `dateKey` (`weekStart` 0 Sunday, 1 Monday, 6 Saturday). */
export function calendarStartOfWeek(dateKey: string, weekStart: number): string {
  return calendarAddDays(dateKey, -((calendarWeekday(dateKey) - weekStart + 7) % 7));
}

/** First day of the month of `dateKey`, moved by `n` months. */
export function calendarStartOfMonth(dateKey: string, n = 0): string {
  const { y, mo } = parseKey(dateKey);
  const t = new Date(Date.UTC(y, mo - 1 + n, 1));
  return keyOf(t.getUTCFullYear(), t.getUTCMonth() + 1, 1);
}

/** The 6 × 7 date keys a month view shows. */
export function calendarMonthGrid(dateKey: string, weekStart: number): string[] {
  const first = calendarStartOfWeek(calendarStartOfMonth(dateKey), weekStart);
  return Array.from({ length: 42 }, (_, i) => calendarAddDays(first, i));
}

/** Whether two date keys are in the same month. */
export function calendarSameMonth(a: string, b: string): boolean {
  return a.slice(0, 7) === b.slice(0, 7);
}

function fmt(date: Date, options: Intl.DateTimeFormatOptions, timeZone = "UTC"): string {
  try {
    return new Intl.DateTimeFormat("en-US", { timeZone, ...options }).format(date);
  } catch {
    return new Intl.DateTimeFormat("en-US", { timeZone: "UTC", ...options }).format(date);
  }
}

function keyDate(dateKey: string): Date {
  const { y, mo, d } = parseKey(dateKey);
  return new Date(Date.UTC(y, mo - 1, d, 12));
}

/** "10:00 AM" in `timeZone`. */
export function formatCalendarTime(iso: string, timeZone: string): string {
  return fmt(new Date(iso), { hour: "numeric", minute: "2-digit" }, timeZone);
}

/** "Mon, Nov 2". */
export function formatCalendarDay(dateKey: string): string {
  return fmt(keyDate(dateKey), { weekday: "short", month: "short", day: "numeric" });
}

/** "Monday, November 2". */
export function formatCalendarDayLong(dateKey: string): string {
  return fmt(keyDate(dateKey), { weekday: "long", month: "long", day: "numeric" });
}

/** "November 2026". */
export function formatCalendarMonth(dateKey: string): string {
  return fmt(keyDate(dateKey), { month: "long", year: "numeric" });
}

/** "Mon" / "M" style weekday names starting at `weekStart`. */
export function calendarWeekdayNames(weekStart: number, style: "short" | "narrow" = "short"): string[] {
  return Array.from({ length: 7 }, (_, i) => fmt(keyDate(calendarAddDays("2026-11-01", (weekStart + i) % 7)), { weekday: style }));
}

/** Short zone name at an instant: "PST", "GMT+1". */
export function calendarZoneLabel(iso: string, timeZone: string): string {
  try {
    const p = new Intl.DateTimeFormat("en-US", { timeZone, timeZoneName: "short" }).formatToParts(new Date(iso));
    return p.find((x) => x.type === "timeZoneName")?.value ?? timeZone;
  } catch {
    return timeZone;
  }
}

/**
 * "Mon, Nov 2 · 10:00 – 10:30 AM PST"; over midnight both days; all-day "Mon, Nov 2" or
 * "Mon, Nov 2 – Wed, Nov 4".
 */
export function formatCalendarRange(o: Pick<CalendarOccurrence, "starts_at" | "ends_at" | "all_day" | "start_date" | "end_date">, timeZone: string): string {
  if (o.all_day && o.start_date && o.end_date) {
    return o.start_date === o.end_date ? formatCalendarDay(o.start_date) : `${formatCalendarDay(o.start_date)} – ${formatCalendarDay(o.end_date)}`;
  }
  const d1 = calendarDateKey(o.starts_at, timeZone);
  const d2 = calendarDateKey(o.ends_at, timeZone);
  const zone = calendarZoneLabel(o.starts_at, timeZone);
  const t1 = formatCalendarTime(o.starts_at, timeZone);
  const t2 = formatCalendarTime(o.ends_at, timeZone);
  return d1 === d2 ? `${formatCalendarDay(d1)} · ${t1} – ${t2} ${zone}` : `${formatCalendarDay(d1)}, ${t1} – ${formatCalendarDay(d2)}, ${t2} ${zone}`;
}

/** "30 min", "1 hr", "1 hr 30 min". */
export function formatCalendarDuration(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m} min`;
  return m === 0 ? `${h} hr` : `${h} hr ${m} min`;
}

/** "10 minutes before", "1 day before", "At start". */
export function calendarReminderLabel(minutes: number): string {
  if (minutes === 0) return "At start";
  if (minutes % 10080 === 0) return `${minutes / 10080} week${minutes === 10080 ? "" : "s"} before`;
  if (minutes % 1440 === 0) return `${minutes / 1440} day${minutes === 1440 ? "" : "s"} before`;
  if (minutes % 60 === 0) return `${minutes / 60} hour${minutes === 60 ? "" : "s"} before`;
  return `${minutes} minutes before`;
}

const DAY_NAMES: Record<string, string> = { SU: "Sunday", MO: "Monday", TU: "Tuesday", WE: "Wednesday", TH: "Thursday", FR: "Friday", SA: "Saturday" };
const ORDINALS: Record<string, string> = { "1": "first", "2": "second", "3": "third", "4": "fourth", "5": "fifth", "-1": "last" };

/** "Weekly on Monday and Wednesday, until Dec 31, 2026". */
export function describeCalendarRecurrence(r: CalendarRecurrence | null | undefined): string {
  if (!r) return "Doesn't repeat";
  const unit = { daily: "day", weekly: "week", monthly: "month", yearly: "year" }[r.freq];
  let s = r.interval > 1 ? `Every ${r.interval} ${unit}s` : { daily: "Daily", weekly: "Weekly", monthly: "Monthly", yearly: "Yearly" }[r.freq];
  if (r.by_day.length > 0) {
    const names = r.by_day.map((d) => {
      const code = d.slice(-2);
      const n = d.slice(0, -2);
      return n ? `the ${ORDINALS[n] ?? n} ${DAY_NAMES[code]}` : (DAY_NAMES[code] ?? code);
    });
    s += ` on ${names.length > 1 ? `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}` : names[0]}`;
  }
  if (r.by_month_day.length > 0) s += ` on ${r.by_month_day.map((d) => (d === -1 ? "the last day" : `day ${d}`)).join(", ")}`;
  if (r.count) s += `, ${r.count} times`;
  else if (r.until) s += `, until ${fmt(keyDate(r.until), { month: "short", day: "numeric", year: "numeric" })}`;
  return s;
}

/** A stable key for one occurrence (list keys, selection). */
export function calendarOccurrenceKey(o: Pick<CalendarOccurrence, "event_id" | "occurrence_start">): string {
  return `${o.event_id}:${o.occurrence_start ?? ""}`;
}

/** Zones offered in pickers when the runtime can't list its own. */
export const CALENDAR_COMMON_ZONES: readonly string[] = [
  "UTC", "America/Los_Angeles", "America/Denver", "America/Phoenix", "America/Chicago", "America/New_York", "America/Anchorage",
  "Pacific/Honolulu", "America/Toronto", "America/Mexico_City", "America/Bogota", "America/Sao_Paulo", "Europe/London", "Europe/Dublin",
  "Europe/Lisbon", "Europe/Madrid", "Europe/Paris", "Europe/Berlin", "Europe/Amsterdam", "Europe/Rome", "Europe/Stockholm", "Europe/Warsaw",
  "Europe/Athens", "Europe/Istanbul", "Africa/Lagos", "Africa/Cairo", "Africa/Johannesburg", "Africa/Nairobi", "Asia/Dubai", "Asia/Karachi",
  "Asia/Kolkata", "Asia/Bangkok", "Asia/Singapore", "Asia/Hong_Kong", "Asia/Shanghai", "Asia/Seoul", "Asia/Tokyo", "Australia/Perth",
  "Australia/Sydney", "Pacific/Auckland",
];

/** Zones for a picker: the runtime's list when available, always including `include`. */
export function calendarZoneOptions(include: readonly string[] = []): string[] {
  let zones: string[] = [];
  const supported = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf;
  if (typeof supported === "function") {
    try {
      zones = supported("timeZone");
    } catch {
      zones = [];
    }
  }
  if (zones.length === 0) zones = [...CALENDAR_COMMON_ZONES];
  for (const z of include) if (z && !zones.includes(z)) zones.unshift(z);
  if (!zones.includes("UTC")) zones.unshift("UTC");
  return zones;
}

/** An occurrence placed in a day column: top/height in wall-clock minutes from local midnight, column within its overlap group. */
export interface CalendarPlaced<T> {
  item: T;
  top: number;
  height: number;
  column: number;
  columns: number;
}

/**
 * Lays timed occurrences out in one day column of `dateKey` in `timeZone`, by wall-clock
 * minutes (so 10:00 sits at the 10:00 line on daylight-saving days too). Overlapping events
 * share the width side by side; events crossing midnight are clipped to the day.
 */
export function calendarLayoutDay<T extends Pick<CalendarOccurrence, "starts_at" | "ends_at">>(items: readonly T[], dateKey: string, timeZone: string, minHeight = 15): CalendarPlaced<T>[] {
  const placed = items
    .map((item) => {
      const sk = calendarDateKey(item.starts_at, timeZone);
      const ek = calendarDateKey(item.ends_at, timeZone);
      const top = sk < dateKey ? 0 : sk > dateKey ? 1440 : calendarMinutesOfDay(item.starts_at, timeZone);
      const end = ek > dateKey ? 1440 : ek < dateKey ? 0 : calendarMinutesOfDay(item.ends_at, timeZone);
      return { item, top, end };
    })
    .filter((p) => p.end > p.top && p.top < 1440)
    .sort((a, b) => a.top - b.top || b.end - a.end);
  const out: CalendarPlaced<T>[] = [];
  let group: { item: T; top: number; end: number; column: number }[] = [];
  let groupEnd = -1;
  const flush = () => {
    const columns = Math.max(1, ...group.map((g) => g.column + 1));
    for (const g of group) out.push({ item: g.item, top: g.top, height: Math.max(g.end - g.top, minHeight), column: g.column, columns });
    group = [];
  };
  for (const p of placed) {
    if (group.length > 0 && p.top >= groupEnd) {
      flush();
      groupEnd = -1;
    }
    const used = new Set(group.filter((g) => Math.max(g.end, g.top + minHeight) > p.top).map((g) => g.column));
    let column = 0;
    while (used.has(column)) column++;
    group.push({ ...p, column });
    groupEnd = Math.max(groupEnd, Math.max(p.end, p.top + minHeight));
  }
  flush();
  return out;
}

/** Occurrences that touch `dateKey` in `timeZone` (all-day by their dates). */
export function calendarOnDay<T extends CalendarOccurrence>(items: readonly T[], dateKey: string, timeZone: string): T[] {
  const start = new Date(calendarDayStart(dateKey, timeZone)).getTime();
  const end = new Date(calendarDayStart(calendarAddDays(dateKey, 1), timeZone)).getTime();
  return items.filter((o) =>
    o.all_day && o.start_date && o.end_date ? o.start_date <= dateKey && dateKey <= o.end_date : new Date(o.starts_at).getTime() < end && new Date(o.ends_at).getTime() > start,
  );
}
