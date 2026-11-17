import { FORUM_LABELS } from "./types";

/** The "@query" being typed just before `caret` (after a space or at the start), or null. */
export function forumMentionQuery(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0) return null;
  if (at > 0 && !/[\s(]/.test(before.charAt(at - 1))) return null;
  const query = before.slice(at + 1);
  if (query.length > 40 || /[\n@]/.test(query) || /\s\s/.test(query) || query.startsWith(" ")) return null;
  return { start: at, query };
}

/** Replaces the "@query" from `start` to `caret` with "@Name " and returns the text and the caret after it. */
export function insertForumMention(text: string, start: number, caret: number, name: string): { text: string; caret: number } {
  const insert = `@${name} `;
  return { text: text.slice(0, start) + insert + text.slice(caret), caret: start + insert.length };
}

/** Ids of picked people whose "@Name" is still in the text: what to send as `mentions` (at most 10). */
export function forumMentionIds(text: string, people: readonly { id: string; display_name: string }[]): string[] {
  const lower = text.toLowerCase();
  const out: string[] = [];
  for (const p of people) {
    if (p.display_name && lower.includes(`@${p.display_name.toLowerCase()}`) && !out.includes(p.id)) out.push(p.id);
  }
  return out.slice(0, 10);
}

/** A run of plain text; `mention` marks "@Name" of someone the item mentions. */
export interface ForumTextPart {
  text: string;
  mention?: boolean;
}

/** Splits plain text into runs, marking the "@Name" of each mentioned person. */
export function forumMentionParts(text: string, mentions: readonly { display_name: string }[]): ForumTextPart[] {
  const names = mentions
    .map((m) => m.display_name)
    .filter(Boolean)
    .sort((a, b) => b.length - a.length);
  if (names.length === 0 || !text.includes("@")) return [{ text }];
  const re = new RegExp(`@(${names.map((n) => n.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})`, "gi");
  const parts: ForumTextPart[] = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    const i = m.index ?? 0;
    if (i > last) parts.push({ text: text.slice(last, i) });
    parts.push({ text: m[0], mention: true });
    last = i + m[0].length;
  }
  if (last < text.length) parts.push({ text: text.slice(last) });
  return parts;
}

/** A label's display text: FORUM_LABELS, else the words of the label ("verified_seller" → "Verified seller"). */
export function forumLabelText(label: string): string {
  const known = FORUM_LABELS[label];
  if (known) return known;
  const words = label.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** "now", "5 min", "3 h", "2 d", then a date. */
export function forumAge(iso: string, now: number = Date.now()): string {
  const s = Math.max(0, (now - new Date(iso).getTime()) / 1000);
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)} min`;
  if (s < 86_400) return `${Math.floor(s / 3600)} h`;
  if (s < 86_400 * 30) return `${Math.floor(s / 86_400)} d`;
  return new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

/** "1 comment", "12 comments". */
export function forumCount(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** Top periods in display order. */
export const FORUM_TOP_PERIODS = [
  { value: "day", label: "Today" },
  { value: "week", label: "This week" },
  { value: "month", label: "This month" },
  { value: "year", label: "This year" },
  { value: "all", label: "All time" },
] as const;
