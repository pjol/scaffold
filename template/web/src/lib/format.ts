const dateTime = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeStyle: "short" });
const date = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });
const relative = new Intl.RelativeTimeFormat("en-US", { numeric: "auto" });

/** "Oct 30, 2026, 4:05 PM". */
export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : dateTime.format(d);
}

/** "Oct 30, 2026". */
export function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : date.format(d);
}

/** "5 minutes ago", "yesterday"; falls back to the date after 30 days. */
export function formatRelative(iso: string, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return iso;
  const s = Math.round((t - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return "just now";
  if (abs < 3600) return relative.format(Math.round(s / 60), "minute");
  if (abs < 86_400) return relative.format(Math.round(s / 3600), "hour");
  if (abs < 30 * 86_400) return relative.format(Math.round(s / 86_400), "day");
  return formatDate(iso);
}
