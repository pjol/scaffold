import type { PlaceHours, PlaceInterval, PlaceWeek, Weekday } from "./types";

/** Monday first, with display labels. */
export const WEEKDAYS: readonly { key: Weekday; label: string; short: string }[] = [
  { key: "mon", label: "Monday", short: "Mon" },
  { key: "tue", label: "Tuesday", short: "Tue" },
  { key: "wed", label: "Wednesday", short: "Wed" },
  { key: "thu", label: "Thursday", short: "Thu" },
  { key: "fri", label: "Friday", short: "Fri" },
  { key: "sat", label: "Saturday", short: "Sat" },
  { key: "sun", label: "Sunday", short: "Sun" },
];

const JS_DAY: readonly Weekday[] = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

/** An empty week (every day closed). */
export function emptyWeek(): PlaceWeek {
  return { mon: [], tue: [], wed: [], thu: [], fri: [], sat: [], sun: [] };
}

function minutes(hhmm: string): number {
  const [h, m] = hhmm.split(":");
  return Number(h) * 60 + Number(m);
}

/** "HH:MM" is a valid opening or closing time (00:00 to 24:00). */
export function isClockTime(value: string): boolean {
  if (!/^\d{2}:\d{2}$/.test(value)) return false;
  const [h, m] = value.split(":").map(Number);
  return h !== undefined && m !== undefined && h <= 24 && m <= 59 && !(h === 24 && m !== 0);
}

/** "09:00–17:30" (24-hour, as stored). */
export function formatInterval(iv: PlaceInterval): string {
  return `${iv.open}–${iv.close}`;
}

/** "Closed" or "09:00–12:00, 13:00–17:00". */
export function formatDay(intervals: readonly PlaceInterval[]): string {
  return intervals.length ? intervals.map(formatInterval).join(", ") : "Closed";
}

/** Weekday and minutes after midnight at `date` in `timezone` ("" = the device's zone). */
function localClock(date: Date, timezone: string): { day: Weekday; minute: number } {
  if (timezone) {
    try {
      const parts = new Intl.DateTimeFormat("en-US", { timeZone: timezone, weekday: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(date);
      const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
      const day = get("weekday").slice(0, 3).toLowerCase() as Weekday;
      const hour = Number(get("hour")) % 24;
      if (JS_DAY.includes(day)) return { day, minute: hour * 60 + Number(get("minute")) };
    } catch {
      // Unknown zone on this runtime: fall back to the device clock.
    }
  }
  return { day: JS_DAY[date.getDay()] ?? "mon", minute: date.getHours() * 60 + date.getMinutes() };
}

const prevDay = (d: Weekday): Weekday => JS_DAY[(JS_DAY.indexOf(d) + 6) % 7] ?? d;

/**
 * Whether the place is open at `date`: `"unknown"` without hours. Openings that close
 * earlier than they open run past midnight into the next day.
 */
export function openState(hours: PlaceHours | null | undefined, date: Date = new Date()): "open" | "closed" | "unknown" {
  if (!hours) return "unknown";
  const { day, minute } = localClock(date, hours.timezone);
  for (const iv of hours.weekly[day] ?? []) {
    const o = minutes(iv.open);
    const c = minutes(iv.close);
    if (c > o ? minute >= o && minute < c : minute >= o) return "open";
  }
  for (const iv of hours.weekly[prevDay(day)] ?? []) {
    const o = minutes(iv.open);
    const c = minutes(iv.close);
    if (c < o && minute < c) return "open";
  }
  return "closed";
}

/** Today's weekday key in the place's zone (highlight it in an hours table). */
export function todayKey(hours: PlaceHours | null | undefined, date: Date = new Date()): Weekday {
  return localClock(date, hours?.timezone ?? "").day;
}

/** "1.2 km" / "350 m" from metres. */
export function formatDistance(metres: number | null | undefined): string {
  if (metres === null || metres === undefined || !Number.isFinite(metres)) return "";
  if (metres < 1000) return `${Math.max(10, Math.round(metres / 10) * 10)} m`;
  return `${(metres / 1000).toFixed(metres < 10_000 ? 1 : 0)} km`;
}
