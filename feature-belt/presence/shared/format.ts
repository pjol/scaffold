import type { Presence, PresenceClearAfter, PresenceCustomStatus, PresenceSetStatusRequest } from "./types";

/** "Clear after" choices for a custom status, in menu order. */
export const PRESENCE_CLEAR_AFTER_OPTIONS: readonly { value: PresenceClearAfter; label: string }[] = [
  { value: "30m", label: "30 minutes" },
  { value: "1h", label: "1 hour" },
  { value: "4h", label: "4 hours" },
  { value: "today", label: "Today" },
  { value: "week", label: "This week" },
  { value: "never", label: "Don't clear" },
];

/** Quick statuses offered above the custom one. */
export const PRESENCE_STATUS_PRESETS: readonly { emoji: string; text: string; clear_after: PresenceClearAfter }[] = [
  { emoji: "📅", text: "In a meeting", clear_after: "1h" },
  { emoji: "🚌", text: "Commuting", clear_after: "30m" },
  { emoji: "🎧", text: "Focusing", clear_after: "1h" },
  { emoji: "🤒", text: "Out sick", clear_after: "today" },
  { emoji: "🏡", text: "Working remotely", clear_after: "today" },
  { emoji: "🌴", text: "On vacation", clear_after: "never" },
];

/** Emoji offered in the status picker (any emoji can be typed). */
export const PRESENCE_STATUS_EMOJI = ["💬", "📅", "🚌", "🎧", "🤒", "🏡", "🌴", "✈️", "🍽️", "☕", "🎉", "🛠️", "📞", "🧘", "🚲", "👶"] as const;

/** "Pause notifications" choices. */
export type PresenceDndChoice = { label: string; minutes: number } | { label: string; until: "tomorrow" | "next_week" };

export const PRESENCE_DND_OPTIONS: readonly PresenceDndChoice[] = [
  { label: "30 minutes", minutes: 30 },
  { label: "1 hour", minutes: 60 },
  { label: "2 hours", minutes: 120 },
  { label: "Until tomorrow", until: "tomorrow" },
  { label: "Until next week", until: "next_week" },
];

/** When a Do Not Disturb choice ends, in the viewer's time zone ("tomorrow" = 9:00 tomorrow, "next week" = 9:00 next Monday). */
export function presenceDndUntil(choice: PresenceDndChoice, now: Date = new Date()): string {
  if ("minutes" in choice) return new Date(now.getTime() + choice.minutes * 60_000).toISOString();
  const d = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 9, 0, 0, 0);
  if (choice.until === "tomorrow") d.setDate(d.getDate() + 1);
  else d.setDate(d.getDate() + (((8 - d.getDay()) % 7) || 7));
  return d.toISOString();
}

/**
 * When a custom status clears, in the viewer's time zone: `today` ends at midnight,
 * `week` at midnight after Sunday. Null for `never`. The server computes 30m/1h/4h itself.
 */
export function presenceClearAfterExpiry(value: PresenceClearAfter, now: Date = new Date()): string | null {
  const minutes: Partial<Record<PresenceClearAfter, number>> = { "30m": 30, "1h": 60, "4h": 240 };
  const m = minutes[value];
  if (m) return new Date(now.getTime() + m * 60_000).toISOString();
  if (value === "never") return null;
  const end = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 0, 0, 0, 0);
  if (value === "week") end.setDate(end.getDate() + ((7 - now.getDay()) % 7));
  return end.toISOString();
}

/** The request for a status, with `expires_at` filled for `today` and `week`. */
export function presenceStatusRequest(emoji: string, text: string, clearAfter: PresenceClearAfter, now: Date = new Date()): PresenceSetStatusRequest {
  return { emoji, text, clear_after: clearAfter, expires_at: presenceClearAfterExpiry(clearAfter, now) };
}

/** The custom status if it hasn't run out yet. */
export function activeCustomStatus(p: Pick<Presence, "custom_status"> | null | undefined, now = Date.now()): PresenceCustomStatus | null {
  const c = p?.custom_status;
  if (!c || (!c.emoji && !c.text)) return null;
  return c.expires_at && Date.parse(c.expires_at) <= now ? null : c;
}

/** Whether Do Not Disturb is on right now. */
export function isPresenceDND(p: Pick<Presence, "dnd_until"> | null | undefined, now = Date.now()): boolean {
  return !!p?.dnd_until && Date.parse(p.dnd_until) > now;
}

/** "🌴 On vacation" (either part may be missing). */
export function presenceStatusText(c: PresenceCustomStatus | null | undefined): string {
  if (!c) return "";
  return [c.emoji, c.text].filter(Boolean).join(" ");
}

/** "Online", "Away", "Offline", or "Do not disturb" (it wins while on). */
export function presenceLabel(p: Pick<Presence, "status" | "dnd_until"> | null | undefined, now = Date.now()): string {
  if (!p) return "Offline";
  if (isPresenceDND(p, now)) return p.status === "offline" ? "Offline, notifications paused" : "Do not disturb";
  return p.status === "online" ? "Online" : p.status === "away" ? "Away" : "Offline";
}

const timeFmt = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
const dayFmt = new Intl.DateTimeFormat(undefined, { weekday: "short", hour: "numeric", minute: "2-digit" });
const dateFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

/** "until 3:00 PM", "until Mon 9:00 AM", "until Nov 12". */
export function presenceUntilLabel(iso: string | null | undefined, now: Date = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  const days = (d.getTime() - now.getTime()) / 86_400_000;
  const sameDay = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate();
  if (sameDay) return `until ${timeFmt.format(d)}`;
  if (days < 6.5) return `until ${dayFmt.format(d)}`;
  return `until ${dateFmt.format(d)}`;
}

/** Contains a pictograph and no letters: what the backend accepts as a status emoji. */
export function isPresenceEmoji(value: string): boolean {
  return value.length > 0 && value.length <= 32 && /\p{Extended_Pictographic}|\p{Regional_Indicator}/u.test(value) && !/\p{L}|\s/u.test(value);
}
