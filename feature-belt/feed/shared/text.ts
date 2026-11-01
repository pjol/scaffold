import type { FeedAuthor, FeedMention, FeedMentionInput, FeedPost } from "./types";

/**
 * Mention and text helpers shared by the web and mobile composers and renderers.
 * Offsets are JavaScript string indexes (UTF-16 code units), which is what the API
 * expects for `mentions[].start` / `length`.
 */

/** The @query being typed at `caret`, or null. "@" must start the text or follow whitespace. */
export function mentionQueryAt(text: string, caret: number): { query: string; start: number } | null {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf("@");
  if (at < 0) return null;
  if (at > 0 && !/\s/.test(before.charAt(at - 1))) return null;
  const query = before.slice(at + 1);
  if (query.length > 30 || query.includes("\n") || query.includes("@") || /\s\s$/.test(query) || query.startsWith(" ")) return null;
  return { query, start: at };
}

/** Replace the @query that starts at `start` (and runs to `caret`) with "@Display Name ". */
export function insertMention(text: string, start: number, caret: number, person: Pick<FeedAuthor, "display_name">): { text: string; caret: number } {
  const token = `@${person.display_name} `;
  const next = text.slice(0, start) + token + text.slice(caret);
  return { text: next, caret: start + token.length };
}

/** Letters and digits (Latin-1 and beyond, without Unicode property escapes for older engines). */
const WORD_CHAR = /[A-Za-z0-9\u00C0-\uFFFF]/;

/**
 * The mentions to send with `text`: for each person picked in the composer, the first
 * "@Display Name" in the text. People whose token was edited away are dropped.
 */
export function buildMentions(text: string, people: readonly Pick<FeedAuthor, "id" | "display_name">[]): FeedMentionInput[] {
  const out: FeedMentionInput[] = [];
  const taken: Array<[number, number]> = [];
  const seen = new Set<string>();
  // Longer names first, so "@Sam Rivera" wins over "@Sam".
  const sorted = [...people].sort((a, b) => b.display_name.length - a.display_name.length);
  for (const p of sorted) {
    if (seen.has(p.id) || !p.display_name) continue;
    const token = `@${p.display_name}`;
    let from = 0;
    while (from <= text.length) {
      const i = text.indexOf(token, from);
      if (i < 0) break;
      const end = i + token.length;
      const boundary = (i === 0 || /\s/.test(text.charAt(i - 1))) && (end === text.length || !WORD_CHAR.test(text.charAt(end)));
      const overlaps = taken.some(([s, e]) => i < e && end > s);
      if (boundary && !overlaps) {
        out.push({ user_id: p.id, start: i, length: token.length });
        taken.push([i, end]);
        seen.add(p.id);
        break;
      }
      from = i + 1;
    }
  }
  return out.sort((a, b) => a.start - b.start);
}

/** People already mentioned in a post or comment (seed the composer when editing). */
export function mentionedPeople(mentions: readonly FeedMention[]): Pick<FeedAuthor, "id" | "display_name">[] {
  const seen = new Set<string>();
  const out: Pick<FeedAuthor, "id" | "display_name">[] = [];
  for (const m of mentions) {
    if (seen.has(m.user_id)) continue;
    seen.add(m.user_id);
    out.push({ id: m.user_id, display_name: m.display_name });
  }
  return out;
}

export type TextSegment = { type: "text"; text: string } | { type: "mention"; text: string; user_id: string } | { type: "link"; text: string; href: string };

function linkify(text: string): TextSegment[] {
  const out: TextSegment[] = [];
  const re = /https?:\/\/[^\s<>"']+/g;
  let last = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    const start = m.index;
    // Trailing punctuation belongs to the sentence, not the link.
    const href = m[0].replace(/[.,;:!?)\]]+$/, "");
    if (start > last) out.push({ type: "text", text: text.slice(last, start) });
    out.push({ type: "link", text: href, href });
    last = start + href.length;
    re.lastIndex = last;
  }
  if (last < text.length) out.push({ type: "text", text: text.slice(last) });
  return out;
}

/** Split text into plain, mention and link segments for rendering. Invalid or overlapping mentions are ignored. */
export function splitText(text: string, mentions: readonly FeedMention[] = []): TextSegment[] {
  const valid = [...mentions].filter((m) => m.start >= 0 && m.length > 0 && m.start + m.length <= text.length).sort((a, b) => a.start - b.start);
  const out: TextSegment[] = [];
  let pos = 0;
  for (const m of valid) {
    if (m.start < pos) continue;
    if (m.start > pos) out.push(...linkify(text.slice(pos, m.start)));
    out.push({ type: "mention", text: text.slice(m.start, m.start + m.length), user_id: m.user_id });
    pos = m.start + m.length;
  }
  if (pos < text.length) out.push(...linkify(text.slice(pos)));
  return out;
}

/** Whether a post is a pinned announcement right now. */
export function isPinned(post: Pick<FeedPost, "kind" | "pinned_until">, now: number = Date.now()): boolean {
  return post.kind === "announcement" && !!post.pinned_until && new Date(post.pinned_until).getTime() > now;
}

/** A document's display name: the stored name, else the last segment of its key. */
export function attachmentName(media: { name: string | null; key: string }): string {
  return media.name || media.key.split("/").pop() || "Attachment";
}

/** "PDF", "DOCX"… from a file name or key. */
export function attachmentExtension(nameOrKey: string): string {
  const m = /\.([a-z0-9]+)$/i.exec(nameOrKey);
  return m?.[1] ? m[1].toUpperCase() : "FILE";
}

/** Realtime topic for an audience key: "all" → the nil-uuid topic, "<kind>:<uuid>" → "feed:<uuid>". */
export function feedAudienceTopic(audience: string): string | null {
  if (audience === "all") return "feed:00000000-0000-0000-0000-000000000000";
  const i = audience.indexOf(":");
  return i > 0 && audience.length > i + 1 ? `feed:${audience.slice(i + 1)}` : null;
}
