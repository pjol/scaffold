/**
 * The message format: a safe markdown subset parsed into a small tree that web and mobile
 * render with their own elements. Nothing is ever rendered as HTML, so a message can't
 * inject markup; links are only http(s) and mailto.
 *
 * Blocks: paragraphs (single newlines kept), ``` code blocks ```, > quotes, - / * / 1. lists.
 * Inline: **bold** or *bold*, _italic_, ~~strike~~ or ~strike~, `code`, [text](https://…),
 * bare URLs, mention tokens (<@uuid>, <@here>, <@channel>, <@group:id|handle>) and
 * :shortcode: emoji.
 */
import { CHANNELS_EMOJI } from "./emoji";

export type ChannelsMention = { kind: "user"; id: string } | { kind: "here" } | { kind: "channel" } | { kind: "group"; id: string; handle: string };

export type ChannelsInline =
  | { t: "text"; text: string }
  | { t: "br" }
  | { t: "bold" | "italic" | "strike"; children: ChannelsInline[] }
  | { t: "code"; text: string }
  | { t: "link"; href: string; children: ChannelsInline[] }
  | { t: "mention"; mention: ChannelsMention }
  | { t: "emoji"; text: string; name: string };

export type ChannelsBlock =
  | { t: "p"; children: ChannelsInline[] }
  | { t: "code"; text: string; lang: string }
  | { t: "quote"; children: ChannelsBlock[] }
  | { t: "list"; ordered: boolean; start: number; items: ChannelsInline[][] };

const LIST_RE = /^\s{0,3}([-*•]|(\d{1,3})[.)])\s+(.*)$/;
const FENCE_RE = /^\s{0,3}```(.*)$/;
const QUOTE_RE = /^\s{0,3}>\s?(.*)$/;

/** Parse a message body into blocks (depth-limited: quotes don't nest). */
export function parseChannelsMarkdown(body: string, depth = 0): ChannelsBlock[] {
  const lines = body.replace(/\r\n?/g, "\n").split("\n");
  const out: ChannelsBlock[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    const fence = FENCE_RE.exec(line);
    if (fence) {
      const lang = (fence[1] ?? "").trim().slice(0, 20);
      const code: string[] = [];
      i++;
      while (i < lines.length && !FENCE_RE.test(lines[i] ?? "")) code.push(lines[i++] ?? "");
      i++; // closing fence (or the end)
      out.push({ t: "code", text: code.join("\n"), lang: /^[\w+-]*$/.test(lang) ? lang : "" });
      continue;
    }
    if (depth === 0 && QUOTE_RE.test(line)) {
      const quoted: string[] = [];
      while (i < lines.length && QUOTE_RE.test(lines[i] ?? "")) quoted.push(QUOTE_RE.exec(lines[i++] ?? "")?.[1] ?? "");
      out.push({ t: "quote", children: parseChannelsMarkdown(quoted.join("\n"), depth + 1) });
      continue;
    }
    const item = LIST_RE.exec(line);
    if (item) {
      const ordered = !!item[2];
      const start = ordered ? Number(item[2]) : 1;
      const items: ChannelsInline[][] = [];
      while (i < lines.length) {
        const m = LIST_RE.exec(lines[i] ?? "");
        if (!m || !!m[2] !== ordered) break;
        items.push(parseChannelsInline(m[3] ?? ""));
        i++;
      }
      out.push({ t: "list", ordered, start, items });
      continue;
    }
    if (line.trim() === "") {
      i++;
      continue;
    }
    const para: string[] = [];
    while (i < lines.length) {
      const l = lines[i] ?? "";
      if (l.trim() === "" || FENCE_RE.test(l) || LIST_RE.test(l) || (depth === 0 && QUOTE_RE.test(l))) break;
      para.push(l);
      i++;
    }
    out.push({ t: "p", children: parseChannelsInline(para.join("\n")) });
  }
  return out;
}

// One pass over the text: the earliest match of any inline form wins.
const INLINE_RE = new RegExp(
  [
    "(`[^`\\n]+`)", // 1 code
    "(<@(?:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|here|channel|group:[A-Za-z0-9_.:-]{1,64}\\|[A-Za-z0-9_.-]{1,64})>)", // 2 mention
    "(\\[[^\\]\\n]{1,200}\\]\\((?:https?:\\/\\/|mailto:)[^\\s)]{1,2000}\\))", // 3 [text](url)
    "((?:https?:\\/\\/|mailto:)[^\\s<>]{2,2000})", // 4 bare URL
    "(\\*\\*(?=\\S)[^\\n]+?\\*\\*)", // 5 **bold**
    "(~~(?=\\S)[^\\n]+?~~)", // 6 ~~strike~~
    "((?<![\\w*])\\*(?=\\S)[^*\\n]+?\\*(?![\\w*]))", // 7 *bold*
    "((?<![\\w_])_(?=\\S)[^_\\n]+?_(?![\\w_]))", // 8 _italic_
    "((?<![\\w~])~(?=\\S)[^~\\n]+?~(?![\\w~]))", // 9 ~strike~
    "(:[a-z0-9_+-]{1,40}:)", // 10 :emoji:
  ].join("|"),
  "g",
);

function pushText(out: ChannelsInline[], text: string): void {
  const parts = text.split("\n");
  parts.forEach((part, i) => {
    if (i > 0) out.push({ t: "br" });
    if (!part) return;
    const last = out[out.length - 1];
    if (last && last.t === "text") last.text += part;
    else out.push({ t: "text", text: part });
  });
}

function mentionOf(token: string): ChannelsMention {
  const inner = token.slice(2, -1);
  if (inner === "here" || inner === "channel") return { kind: inner };
  if (inner.startsWith("group:")) {
    const [id = "", handle = ""] = inner.slice(6).split("|");
    return { kind: "group", id, handle };
  }
  return { kind: "user", id: inner };
}

/** Trailing punctuation that ends a sentence rather than a URL. */
function trimUrl(url: string): [string, string] {
  let end = url.length;
  while (end > 0 && /[.,;:!?'")\]]/.test(url[end - 1] ?? "")) {
    if (url[end - 1] === ")" && (url.slice(0, end).match(/\(/g)?.length ?? 0) >= (url.slice(0, end).match(/\)/g)?.length ?? 0)) break;
    end--;
  }
  return [url.slice(0, end), url.slice(end)];
}

/** Parse inline markup (also used for one-line previews). */
export function parseChannelsInline(text: string, depth = 0): ChannelsInline[] {
  const out: ChannelsInline[] = [];
  let last = 0;
  INLINE_RE.lastIndex = 0;
  const re = new RegExp(INLINE_RE.source, "g");
  let m: RegExpExecArray | null;
  while ((m = re.exec(text))) {
    const [all] = m;
    if (m.index > last) pushText(out, text.slice(last, m.index));
    last = m.index + all.length;
    const inner = (n: number) => (depth < 3 ? parseChannelsInline(all.slice(n, all.length - n), depth + 1) : [{ t: "text" as const, text: all.slice(n, all.length - n) }]);
    if (m[1]) out.push({ t: "code", text: all.slice(1, -1) });
    else if (m[2]) out.push({ t: "mention", mention: mentionOf(all) });
    else if (m[3]) {
      const close = all.indexOf("](");
      const label = all.slice(1, close);
      out.push({ t: "link", href: all.slice(close + 2, -1), children: depth < 3 ? parseChannelsInline(label, 3) : [{ t: "text", text: label }] });
    } else if (m[4]) {
      const [url, rest] = trimUrl(all);
      out.push({ t: "link", href: url, children: [{ t: "text", text: url.replace(/^mailto:/, "") }] });
      if (rest) pushText(out, rest);
    } else if (m[5]) out.push({ t: "bold", children: inner(2) });
    else if (m[6]) out.push({ t: "strike", children: inner(2) });
    else if (m[7]) out.push({ t: "bold", children: inner(1) });
    else if (m[8]) out.push({ t: "italic", children: inner(1) });
    else if (m[9]) out.push({ t: "strike", children: inner(1) });
    else if (m[10]) {
      const name = all.slice(1, -1);
      const emoji = CHANNELS_EMOJI[name];
      if (emoji) out.push({ t: "emoji", text: emoji, name });
      else pushText(out, all);
    }
  }
  if (last < text.length) pushText(out, text.slice(last));
  return out;
}

/** True when a body is only 1–3 emoji (rendered large, like Slack). */
export function isChannelsEmojiOnly(body: string): boolean {
  const blocks = parseChannelsMarkdown(body.trim());
  if (blocks.length !== 1 || blocks[0]?.t !== "p") return false;
  const parts = blocks[0].children.filter((c) => !(c.t === "text" && c.text.trim() === ""));
  if (parts.length === 0 || parts.length > 3) return false;
  return parts.every((c) => c.t === "emoji" || (c.t === "text" && /^(\p{Extended_Pictographic}|\p{Regional_Indicator}|‍|️|\s)+$/u.test(c.text)));
}

// ───────────────────────────────────────────────────────────── composer ──

/** Something the composer inserted from autocomplete: shown as `@label`, sent as `token`. */
export interface ChannelsPickedMention {
  label: string;
  token: string;
}

export const channelsUserToken = (id: string) => `<@${id}>`;
export const channelsGroupToken = (id: string, handle: string) => `<@group:${id}|${handle}>`;

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * Turn the composer's text into a body: every `@label` the person picked becomes its token
 * (longest labels first, so "@Sam Rivera" wins over "@Sam"); `@here` and `@channel` always do.
 */
export function encodeChannelsMentions(text: string, picked: readonly ChannelsPickedMention[]): string {
  let out = text;
  const sorted = [...picked].sort((a, b) => b.label.length - a.label.length);
  for (const p of sorted) out = out.replace(new RegExp(`(^|[^\\w<])@${escapeRe(p.label)}(?![\\w])`, "g"), (_, pre: string) => `${pre}${p.token}`);
  return out.replace(/(^|[^\w<])@(here|channel)\b/g, (_, pre: string, k: string) => `${pre}<@${k}>`);
}

/**
 * Turn a stored body back into editable text: tokens become `@Name`, and the returned
 * `picked` list lets `encodeChannelsMentions` restore them on save.
 */
export function decodeChannelsMentions(body: string, mentioned: readonly { id: string; display_name: string }[]): { text: string; picked: ChannelsPickedMention[] } {
  const names = new Map(mentioned.map((u) => [u.id, u.display_name]));
  const picked: ChannelsPickedMention[] = [];
  const text = body.replace(/<@([^>]+)>/g, (tok, inner: string) => {
    if (inner === "here" || inner === "channel") return `@${inner}`;
    if (inner.startsWith("group:")) {
      const handle = inner.split("|")[1] ?? "group";
      picked.push({ label: handle, token: tok });
      return `@${handle}`;
    }
    const name = names.get(inner);
    if (!name) return tok;
    picked.push({ label: name, token: tok });
    return `@${name}`;
  });
  return { text, picked };
}

/** A body as one line of plain text (previews, notifications on the client). */
export function channelsPlainText(body: string, mentioned: readonly { id: string; display_name: string }[] = []): string {
  const names = new Map(mentioned.map((u) => [u.id, u.display_name]));
  const flat = (nodes: ChannelsInline[]): string =>
    nodes
      .map((n) => {
        switch (n.t) {
          case "text":
          case "code":
          case "emoji":
            return n.text;
          case "br":
            return " ";
          case "mention":
            return n.mention.kind === "user" ? `@${names.get(n.mention.id) ?? "someone"}` : n.mention.kind === "group" ? `@${n.mention.handle}` : `@${n.mention.kind}`;
          default:
            return flat(n.children);
        }
      })
      .join("");
  return parseChannelsMarkdown(body)
    .map((b) => (b.t === "p" ? flat(b.children) : b.t === "code" ? b.text : b.t === "list" ? b.items.map(flat).join(", ") : b.children.map((c) => (c.t === "p" ? flat(c.children) : "")).join(" ")))
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
}
