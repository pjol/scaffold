/**
 * The search query grammar on the client (mirrors backend `app.ParseSearchQuery`): operators
 * become chips, and the words left are highlighted in results.
 *
 *   from:@sam  from:me   in:#general  in:"Q3 plan"   type:page   is:open   has:link
 *   with:@sam  to:me     before:/after:/on:2026-11-01 (or today, yesterday)
 *   during:today|yesterday|week|month|year|2026|2026-11|november
 */
import { SEARCH_MIN_QUERY_CHARS } from "./types";

export type SearchOperatorKey = "from" | "in" | "type" | "is" | "has" | "with" | "to" | "before" | "after" | "on" | "during";

/** One operator token in the query. */
export interface SearchOperatorChip {
  key: SearchOperatorKey;
  /** The value as typed, without quotes. */
  value: string;
  /** The exact token in the query (for removing it). */
  token: string;
}

const OP_RE = /(^|\s)(from|in|type|is|has|with|to|before|after|on|during):("[^"]{1,100}"|\S+)/gi;
const DAY_RE = /^(\d{4}-\d{2}-\d{2}|today|yesterday)$/i;
const SPAN_RE = /^(today|yesterday|week|month|year|\d{4}|\d{4}-\d{2}|\d{4}-\d{2}-\d{2}|jan(uary)?|feb(ruary)?|mar(ch)?|apr(il)?|may|june?|july?|aug(ust)?|sept?(ember)?|oct(ober)?|nov(ember)?|dec(ember)?)$/i;

function valid(key: SearchOperatorKey, value: string): boolean {
  if (!value) return false;
  if (key === "before" || key === "after" || key === "on") return DAY_RE.test(value);
  if (key === "during") return SPAN_RE.test(value);
  return true;
}

/** Splits a query into its words and operator chips (operators with unreadable values stay words). */
export function parseSearchOperators(raw: string): { text: string; chips: SearchOperatorChip[] } {
  const chips: SearchOperatorChip[] = [];
  const text = raw.replace(OP_RE, (tok: string, lead: string, key: string, val: string) => {
    const k = key.toLowerCase() as SearchOperatorKey;
    const value = val.replace(/^"|"$/g, "");
    if (!valid(k, value)) return tok;
    chips.push({ key: k, value, token: tok.trim() });
    return lead;
  });
  return { text: text.replace(/\s+/g, " ").trim(), chips };
}

/** The query without one chip's token. */
export function removeSearchOperator(raw: string, chip: SearchOperatorChip): string {
  const at = raw.indexOf(chip.token);
  if (at < 0) return raw;
  return `${raw.slice(0, at)} ${raw.slice(at + chip.token.length)}`.replace(/\s+/g, " ").trim();
}

/** Whether the API will search it: enough words, or an operator alone. */
export function searchQueryReady(raw: string): boolean {
  const { text, chips } = parseSearchOperators(raw);
  return chips.length > 0 || text.length >= SEARCH_MIN_QUERY_CHARS;
}

const LABELS: Record<SearchOperatorKey, string> = {
  from: "From",
  in: "In",
  type: "Type",
  is: "Is",
  has: "Has",
  with: "With",
  to: "To",
  before: "Before",
  after: "After",
  on: "On",
  during: "During",
};

/** "From Sam Lee", "In #general", "Type page", "During week". `names` resolves from: values. */
export function searchOperatorLabel(chip: SearchOperatorChip, names?: Record<string, string[]>): string {
  let v = chip.value;
  if (chip.key === "from" || chip.key === "with" || chip.key === "to") {
    const bare = v.replace(/^@/, "").toLowerCase();
    if (bare === "me") v = "you";
    else {
      const n = names?.[bare];
      v = n && n.length > 0 ? n.slice(0, 2).join(", ") + (n.length > 2 ? "…" : "") : v.startsWith("@") ? v : `@${v}`;
    }
  }
  return `${LABELS[chip.key]} ${v}`;
}

/** Operators the palette and results page offer as you type, with what each does. */
export const SEARCH_OPERATOR_HINTS: readonly { key: SearchOperatorKey; example: string; label: string }[] = [
  { key: "from", example: "from:@", label: "Written or created by someone" },
  { key: "in", example: "in:#", label: "In a channel, folder or page" },
  { key: "type", example: "type:", label: "Messages, pages, files, events…" },
  { key: "after", example: "after:", label: "After a date (2026-11-01, yesterday)" },
  { key: "before", example: "before:", label: "Before a date" },
  { key: "during", example: "during:", label: "This week, month or year" },
  { key: "is", example: "is:", label: "A state the feature knows" },
];

/** This device's IANA time zone ("UTC" when the runtime doesn't say). */
export function searchDeviceZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** A piece of a title or snippet, marked when it matches a word of the query. */
export interface SearchHighlightPart {
  text: string;
  match: boolean;
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Splits text into parts, marking every occurrence of the query's words (2+ letters, any case). */
export function searchHighlight(text: string, query: string): SearchHighlightPart[] {
  const words = [...new Set(parseSearchOperators(query).text.toLowerCase().split(/\s+/).filter((w) => w.length >= 2))];
  if (!text || words.length === 0) return [{ text, match: false }];
  const re = new RegExp(`(${words.sort((a, b) => b.length - a.length).map(escapeRe).join("|")})`, "giu");
  const out: SearchHighlightPart[] = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    const at = m.index ?? 0;
    if (at > last) out.push({ text: text.slice(last, at), match: false });
    out.push({ text: m[0], match: true });
    last = at + m[0].length;
  }
  if (last < text.length) out.push({ text: text.slice(last), match: false });
  return out;
}
