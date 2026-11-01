const day = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });
const dayYear = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

/** "just now", "5 minutes ago", "3 hours ago", "2 days ago", then "Oct 30" (Hermes has no Intl.RelativeTimeFormat). */
export function timeAgo(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? "" : "s"} ago`;
  if (s < 45) return "just now";
  if (s < 3600) return plural(Math.max(1, Math.round(s / 60)), "minute");
  if (s < 86_400) return plural(Math.round(s / 3600), "hour");
  if (s < 7 * 86_400) return plural(Math.round(s / 86_400), "day");
  const d = new Date(t);
  return d.getFullYear() === new Date(now).getFullYear() ? day.format(d) : dayYear.format(d);
}
