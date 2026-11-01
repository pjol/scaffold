/**
 * A small Markdown parser for assistant replies, shared by web and mobile. It returns a
 * tree the apps render as plain elements and text nodes, so model output can never inject
 * HTML: raw tags stay literal text, and links keep only http(s) and mailto targets.
 * Supported: paragraphs, headings, bullet and numbered lists, block quotes, fenced code,
 * tables (rendered as preformatted text), rules, **bold**, *italic*, `code`, [links](…)
 * and bare URLs. Partial Markdown (a reply still streaming) parses without errors.
 */

export type MdInline =
  | { type: "text"; text: string }
  | { type: "strong"; children: MdInline[] }
  | { type: "em"; children: MdInline[] }
  | { type: "code"; text: string }
  | { type: "link"; href: string; children: MdInline[] };

export type MdBlock =
  | { type: "paragraph"; children: MdInline[] }
  | { type: "heading"; level: 1 | 2 | 3; children: MdInline[] }
  | { type: "list"; ordered: boolean; start: number; items: MdInline[][] }
  | { type: "quote"; children: MdInline[] }
  | { type: "code"; lang: string; text: string }
  | { type: "rule" };

const FENCE = /^\s*(```|~~~)\s*([\w+-]*)\s*$/;
const HEADING = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const BULLET = /^\s*[-*+]\s+(.*)$/;
const ORDERED = /^\s*(\d{1,9})[.)]\s+(.*)$/;
const RULE = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
const QUOTE = /^\s{0,3}>\s?(.*)$/;
const TABLE = /^\s*\|.*\|\s*$/;

export function parseMarkdown(source: string): MdBlock[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: MdBlock[] = [];
  let para: string[] = [];
  const flushPara = () => {
    if (para.length) blocks.push({ type: "paragraph", children: parseInline(para.join("\n")) });
    para = [];
  };
  let i = 0;
  while (i < lines.length) {
    const line = lines[i] ?? "";
    const fence = FENCE.exec(line);
    if (fence) {
      flushPara();
      const marker = fence[1] ?? "```";
      const body: string[] = [];
      i++;
      while (i < lines.length && !(lines[i] ?? "").trim().startsWith(marker)) body.push(lines[i++] ?? "");
      i++; // closing fence (or end of a reply still streaming)
      blocks.push({ type: "code", lang: fence[2] ?? "", text: body.join("\n") });
      continue;
    }
    if (line.trim() === "") {
      flushPara();
      i++;
      continue;
    }
    if (RULE.test(line)) {
      flushPara();
      blocks.push({ type: "rule" });
      i++;
      continue;
    }
    const heading = HEADING.exec(line);
    if (heading) {
      flushPara();
      const level = Math.min(3, (heading[1] ?? "#").length) as 1 | 2 | 3;
      blocks.push({ type: "heading", level, children: parseInline(heading[2] ?? "") });
      i++;
      continue;
    }
    if (TABLE.test(line)) {
      flushPara();
      const rows: string[] = [];
      while (i < lines.length && TABLE.test(lines[i] ?? "")) rows.push((lines[i++] ?? "").trim());
      blocks.push({ type: "code", lang: "table", text: rows.join("\n") });
      continue;
    }
    if (QUOTE.test(line)) {
      flushPara();
      const body: string[] = [];
      while (i < lines.length && QUOTE.test(lines[i] ?? "")) body.push(QUOTE.exec(lines[i++] ?? "")?.[1] ?? "");
      blocks.push({ type: "quote", children: parseInline(body.join("\n")) });
      continue;
    }
    const bullet = BULLET.exec(line);
    const ordered = ORDERED.exec(line);
    if (bullet || ordered) {
      flushPara();
      const isOrdered = !bullet;
      const items: string[] = [];
      while (i < lines.length) {
        const l = lines[i] ?? "";
        const m = isOrdered ? ORDERED.exec(l) : BULLET.exec(l);
        if (m) {
          items.push((isOrdered ? m[2] : m[1]) ?? "");
        } else if (l.trim() !== "" && /^\s{2,}/.test(l) && items.length) {
          items[items.length - 1] += `\n${l.trim()}`; // continuation or nested item, kept flat
        } else {
          break;
        }
        i++;
      }
      blocks.push({ type: "list", ordered: isOrdered, start: isOrdered ? Number(ordered?.[1] ?? 1) : 1, items: items.map((t) => parseInline(t)) });
      continue;
    }
    para.push(line);
    i++;
  }
  flushPara();
  return blocks;
}

/** Only these link targets become links; anything else renders as its text. */
export function safeHref(raw: string): string | null {
  const href = raw.trim();
  return /^(https?:\/\/|mailto:)[^\s<>"']+$/i.test(href) ? href : null;
}

const BARE_URL = /^https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"]/;

export function parseInline(src: string): MdInline[] {
  const out: MdInline[] = [];
  let text = "";
  const pushText = () => {
    if (text) out.push({ type: "text", text });
    text = "";
  };
  let i = 0;
  while (i < src.length) {
    const ch = src[i] ?? "";
    const rest = src.slice(i);
    if (ch === "\\" && i + 1 < src.length && /[\\`*_[\]()#+\-.!>|]/.test(src[i + 1] ?? "")) {
      text += src[i + 1];
      i += 2;
      continue;
    }
    if (ch === "`") {
      const end = src.indexOf("`", i + 1);
      if (end > i) {
        pushText();
        out.push({ type: "code", text: src.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }
    if (rest.startsWith("**") || rest.startsWith("__")) {
      const marker = rest.slice(0, 2);
      const end = src.indexOf(marker, i + 2);
      if (end > i + 2) {
        pushText();
        out.push({ type: "strong", children: parseInline(src.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
    }
    if ((ch === "*" || ch === "_") && src[i + 1] !== " " && src[i + 1] !== ch) {
      const end = src.indexOf(ch, i + 1);
      const wordInside = ch === "_" && /\w/.test(src[i - 1] ?? "");
      if (end > i + 1 && !wordInside && src[end - 1] !== " ") {
        pushText();
        out.push({ type: "em", children: parseInline(src.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }
    if (ch === "[") {
      const m = /^\[([^\]\n]+)\]\(([^()\s]+)\)/.exec(rest);
      if (m) {
        const href = safeHref(m[2] ?? "");
        pushText();
        const children = parseInline(m[1] ?? "");
        if (href) out.push({ type: "link", href, children });
        else out.push(...children);
        i += m[0].length;
        continue;
      }
    }
    if (ch === "h" && (i === 0 || /[\s(]/.test(src[i - 1] ?? ""))) {
      const m = BARE_URL.exec(rest);
      if (m) {
        pushText();
        out.push({ type: "link", href: m[0], children: [{ type: "text", text: m[0] }] });
        i += m[0].length;
        continue;
      }
    }
    text += ch;
    i++;
  }
  pushText();
  return out;
}

/** Plain text of inline nodes (accessibility labels, copy). */
export function inlineText(nodes: MdInline[]): string {
  return nodes.map((n) => (n.type === "text" || n.type === "code" ? n.text : inlineText(n.children))).join("");
}
