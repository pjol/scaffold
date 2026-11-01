/**
 * Time-zone helpers for event pickers and labels, on `Intl.DateTimeFormat` only (works in
 * browsers and Hermes). Events store UTC instants plus an IANA zone; forms edit wall-clock
 * time in that zone.
 */

const partsCache = new Map<string, Intl.DateTimeFormat>();

function partsFormatter(timeZone: string): Intl.DateTimeFormat {
  let f = partsCache.get(timeZone);
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
    partsCache.set(timeZone, f);
  }
  return f;
}

/** Whether `timeZone` is an IANA zone this runtime knows. */
export function isValidTimeZone(timeZone: string): boolean {
  try {
    partsFormatter(timeZone);
    return true;
  } catch {
    return false;
  }
}

/** The device's zone, falling back to UTC. */
export function deviceTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function wallClock(ms: number, timeZone: string): { y: number; mo: number; d: number; h: number; mi: number; s: number } {
  const out = { y: 0, mo: 0, d: 0, h: 0, mi: 0, s: 0 };
  for (const p of partsFormatter(timeZone).formatToParts(new Date(ms))) {
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

/** Offset of `timeZone` from UTC at instant `ms`, in ms (UTC−8 → −28 800 000). */
export function zoneOffsetMs(ms: number, timeZone: string): number {
  const w = wallClock(ms, timeZone);
  const asUtc = Date.UTC(w.y, w.mo - 1, w.d, w.h, w.mi, w.s);
  return asUtc - (ms - (ms % 1000));
}

const pad = (n: number) => String(n).padStart(2, "0");

/** A UTC ISO instant as `YYYY-MM-DD` and `HH:mm` wall-clock values in `timeZone` (form inputs). */
export function toZonedInput(iso: string, timeZone: string): { date: string; time: string } {
  const w = wallClock(new Date(iso).getTime(), timeZone);
  return { date: `${w.y}-${pad(w.mo)}-${pad(w.d)}`, time: `${pad(w.h)}:${pad(w.mi)}` };
}

/**
 * Wall-clock `date` (`YYYY-MM-DD`) and `time` (`HH:mm`) in `timeZone` to a UTC ISO string,
 * or null when either is malformed. In a DST gap the time moves forward by the gap.
 */
export function fromZonedInput(date: string, time: string, timeZone: string): string | null {
  const dm = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const tm = /^(\d{2}):(\d{2})$/.exec(time);
  if (!dm || !tm) return null;
  const guess = Date.UTC(Number(dm[1]), Number(dm[2]) - 1, Number(dm[3]), Number(tm[1]), Number(tm[2]));
  if (Number.isNaN(guess)) return null;
  let ms = guess - zoneOffsetMs(guess, timeZone);
  const second = zoneOffsetMs(ms, timeZone);
  if (second !== zoneOffsetMs(guess, timeZone)) ms = guess - second;
  return new Date(ms).toISOString();
}

/** Short zone name at an instant: "PST", "GMT+1". */
export function zoneLabel(iso: string, timeZone: string): string {
  try {
    const parts = new Intl.DateTimeFormat("en-US", { timeZone, timeZoneName: "short" }).formatToParts(new Date(iso));
    return parts.find((p) => p.type === "timeZoneName")?.value ?? timeZone;
  } catch {
    return timeZone;
  }
}

function fmt(iso: string, timeZone: string, options: Intl.DateTimeFormatOptions): string {
  try {
    return new Intl.DateTimeFormat("en-US", { timeZone, ...options }).format(new Date(iso));
  } catch {
    return new Intl.DateTimeFormat("en-US", { timeZone: "UTC", ...options }).format(new Date(iso));
  }
}

/** "Sat, Nov 2". */
export function formatEventDay(iso: string, timeZone: string): string {
  return fmt(iso, timeZone, { weekday: "short", month: "short", day: "numeric" });
}

/** "10:00 AM". */
export function formatEventTime(iso: string, timeZone: string): string {
  return fmt(iso, timeZone, { hour: "numeric", minute: "2-digit" });
}

/**
 * "Sat, Nov 2 · 10:00 AM – 12:00 PM PST", or with both days when it spans midnight:
 * "Sat, Nov 2, 10:00 PM – Sun, Nov 3, 2:00 AM PST".
 */
export function formatEventRange(startsAt: string, endsAt: string, timeZone: string): string {
  const zone = zoneLabel(startsAt, timeZone);
  const d1 = formatEventDay(startsAt, timeZone);
  const d2 = formatEventDay(endsAt, timeZone);
  const t1 = formatEventTime(startsAt, timeZone);
  const t2 = formatEventTime(endsAt, timeZone);
  return d1 === d2 ? `${d1} · ${t1} – ${t2} ${zone}` : `${d1}, ${t1} – ${d2}, ${t2} ${zone}`;
}

/** Month and day parts for a date badge: { month: "NOV", day: "2" }. */
export function eventDateBadge(iso: string, timeZone: string): { month: string; day: string } {
  return { month: fmt(iso, timeZone, { month: "short" }).toUpperCase(), day: fmt(iso, timeZone, { day: "numeric" }) };
}

/** Zones offered in pickers when the runtime can't list its own. */
export const COMMON_TIME_ZONES: readonly string[] = [
  "UTC",
  "America/Los_Angeles",
  "America/Denver",
  "America/Phoenix",
  "America/Chicago",
  "America/New_York",
  "America/Anchorage",
  "Pacific/Honolulu",
  "America/Toronto",
  "America/Vancouver",
  "America/Mexico_City",
  "America/Bogota",
  "America/Sao_Paulo",
  "America/Argentina/Buenos_Aires",
  "Europe/London",
  "Europe/Dublin",
  "Europe/Lisbon",
  "Europe/Madrid",
  "Europe/Paris",
  "Europe/Berlin",
  "Europe/Amsterdam",
  "Europe/Rome",
  "Europe/Stockholm",
  "Europe/Warsaw",
  "Europe/Athens",
  "Europe/Istanbul",
  "Africa/Lagos",
  "Africa/Cairo",
  "Africa/Johannesburg",
  "Africa/Nairobi",
  "Asia/Dubai",
  "Asia/Karachi",
  "Asia/Kolkata",
  "Asia/Bangkok",
  "Asia/Singapore",
  "Asia/Hong_Kong",
  "Asia/Shanghai",
  "Asia/Seoul",
  "Asia/Tokyo",
  "Australia/Perth",
  "Australia/Sydney",
  "Pacific/Auckland",
];

/** Zones for a picker: the runtime's full list when available, else `COMMON_TIME_ZONES`, always including `include`. */
export function timeZoneOptions(include: readonly string[] = []): string[] {
  let zones: string[] = [];
  const supported = (Intl as unknown as { supportedValuesOf?: (key: string) => string[] }).supportedValuesOf;
  if (typeof supported === "function") {
    try {
      zones = supported("timeZone");
    } catch {
      zones = [];
    }
  }
  if (zones.length === 0) zones = [...COMMON_TIME_ZONES];
  for (const z of include) if (z && !zones.includes(z)) zones.unshift(z);
  if (!zones.includes("UTC")) zones.unshift("UTC");
  return zones;
}
