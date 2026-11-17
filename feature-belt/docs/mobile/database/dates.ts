/** Calendar-day helpers on YYYY-MM-DD strings (local days, never shifted by time zone). */

export function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

export function ymd(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

export function todayYmd(): string {
  return ymd(new Date());
}

/** The day part of a date value's start or end ("2026-11-20T09:30" → "2026-11-20"). */
export function dayOf(s: string | undefined | null): string {
  return (s ?? "").slice(0, 10);
}

export function timeOf(s: string | undefined | null): string {
  const t = (s ?? "").split("T")[1];
  return t ? t.slice(0, 5) : "";
}

function toUTC(s: string): number {
  const [y, m, d] = s.split("-").map(Number);
  return Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1);
}

export function addDays(s: string, n: number): string {
  const d = new Date(toUTC(s) + n * 86_400_000);
  return `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}`;
}

/** Whole days from a to b. */
export function daysBetween(a: string, b: string): number {
  return Math.round((toUTC(b) - toUTC(a)) / 86_400_000);
}

export function addMonths(year: number, month: number, n: number): { year: number; month: number } {
  const t = year * 12 + month + n;
  return { year: Math.floor(t / 12), month: ((t % 12) + 12) % 12 };
}

/** Weeks (Sunday first) covering a month; month is 0-based. */
export function monthWeeks(year: number, month: number): string[][] {
  const first = `${year}-${pad2(month + 1)}-01`;
  const startDow = new Date(toUTC(first)).getUTCDay();
  let day = addDays(first, -startDow);
  const weeks: string[][] = [];
  for (let w = 0; w < 6; w++) {
    const week: string[] = [];
    for (let i = 0; i < 7; i++) {
      week.push(day);
      day = addDays(day, 1);
    }
    weeks.push(week);
    if (Number(day.slice(5, 7)) - 1 !== month && w >= 3) break;
  }
  return weeks;
}

export function monthLabel(year: number, month: number): string {
  return new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric", timeZone: "UTC" }).format(new Date(Date.UTC(year, month, 1)));
}

/** "Mon", "Tue"… Sunday first. */
export function weekdayLabels(style: "narrow" | "short" = "short"): string[] {
  const f = new Intl.DateTimeFormat(undefined, { weekday: style, timeZone: "UTC" });
  return Array.from({ length: 7 }, (_, i) => f.format(new Date(Date.UTC(2026, 0, 4 + i))));
}

export function longDayLabel(s: string): string {
  return new Intl.DateTimeFormat(undefined, { weekday: "long", month: "long", day: "numeric", timeZone: "UTC" }).format(new Date(toUTC(s)));
}

export function shortDayLabel(s: string): string {
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" }).format(new Date(toUTC(s)));
}

/** HH:MM (24 h) or null. Accepts "9:30", "0930", "9.30". */
export function normTime(t: string): string | null {
  const m = /^(\d{1,2})[:.]?(\d{2})$/.exec(t.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return `${pad2(h)}:${pad2(min)}`;
}

export const YMD_RE = /^\d{4}-\d{2}-\d{2}$/;
