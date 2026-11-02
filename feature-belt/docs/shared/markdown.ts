/**
 * Markdown ⇄ blocks for paste and copy (the server has the same rules for import, export
 * and agent tools). Portable: no DOM.
 */
import { docsNormalize, docsPlain, docsSafeLink } from "./rich";
import type { DocsBlock, DocsBlockProps, DocsBlockType, DocsSpan } from "./types";

/** A block not yet on a page: what paste and import produce. */
export interface DocsDraftBlock {
  type: DocsBlockType;
  props: DocsBlockProps;
  children: DocsDraftBlock[];
}

const NESTABLE = new Set<DocsBlockType>(["paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"]);

export function docsIsNestable(type: DocsBlockType): boolean {
  return NESTABLE.has(type);
}

// ---- inline ----------------------------------------------------------------------

interface InlineState {
  bold?: boolean;
  italic?: boolean;
  strike?: boolean;
  underline?: boolean;
  link?: string;
}

function findAhead(s: string, from: number, delim: string): number {
  for (let i = from; i + delim.length <= s.length; i++) {
    if (s[i] === "\\") {
      i++;
      continue;
    }
    if (s.startsWith(delim, i)) return i;
  }
  return -1;
}

function linkAt(s: string, i: number): { label: string; target: string; next: number } | null {
  let depth = 0;
  for (let j = i; j < s.length; j++) {
    const c = s[j];
    if (c === "\\") {
      j++;
      continue;
    }
    if (c === "[") depth++;
    if (c === "]") {
      depth--;
      if (depth === 0) {
        if (s[j + 1] !== "(") return null;
        const end = findAhead(s, j + 2, ")");
        if (end < 0) return null;
        return { label: s.slice(i + 1, j), target: s.slice(j + 2, end).trim(), next: end + 1 };
      }
    }
  }
  return null;
}

const UUID = "[0-9a-fA-F-]{36}";
const PAGE_TARGET = new RegExp(`^(?:page:|/docs/)(${UUID})$`);

function parseInline(s: string, st: InlineState, out: DocsSpan[]): void {
  let buf = "";
  const emit = (text: string, code = false, state: InlineState = st) => {
    if (!text) return;
    const sp: DocsSpan = { text };
    if (state.bold) sp.bold = true;
    if (state.italic) sp.italic = true;
    if (state.strike) sp.strike = true;
    if (state.underline) sp.underline = true;
    if (state.link) sp.link = state.link;
    if (code) sp.code = true;
    out.push(sp);
  };
  const flush = () => {
    emit(buf);
    buf = "";
  };
  for (let i = 0; i < s.length; i++) {
    const c = s[i]!;
    if (c === "\\" && i + 1 < s.length) {
      buf += s[i + 1];
      i++;
      continue;
    }
    if (c === "`") {
      const fence = s[i + 1] === "`" ? "``" : "`";
      const end = findAhead(s, i + fence.length, fence);
      if (end > i + fence.length - 1) {
        flush();
        emit(s.slice(i + fence.length, end), true);
        i = end + fence.length - 1;
        continue;
      }
    }
    const two = s.slice(i, i + 2);
    if (two === "**" || two === "__" || two === "~~") {
      const end = findAhead(s, i + 2, two);
      if (end > i + 2) {
        flush();
        parseInline(s.slice(i + 2, end), two === "~~" ? { ...st, strike: true } : { ...st, bold: true }, out);
        i = end + 1;
        continue;
      }
    }
    if (s.startsWith("<u>", i)) {
      const end = findAhead(s, i + 3, "</u>");
      if (end >= i + 3) {
        flush();
        parseInline(s.slice(i + 3, end), { ...st, underline: true }, out);
        i = end + 3;
        continue;
      }
    }
    if ((c === "*" || c === "_") && i + 1 < s.length && !/\s/.test(s[i + 1]!)) {
      const wordStart = i === 0 || !/[\p{L}\p{N}]/u.test(s[i - 1]!);
      const end = findAhead(s, i + 1, c);
      if (end > i + 1 && (c === "*" || wordStart)) {
        flush();
        parseInline(s.slice(i + 1, end), { ...st, italic: true }, out);
        i = end;
        continue;
      }
    }
    if (c === "@" && s[i + 1] === "[") {
      const l = linkAt(s, i + 1);
      if (l && l.target.startsWith("user:")) {
        flush();
        out.push({ text: `@${l.label}`, mention: { type: "user", id: l.target.slice(5) } });
        i = l.next - 1;
        continue;
      }
    }
    if (c === "[") {
      const l = linkAt(s, i);
      if (l) {
        flush();
        const page = PAGE_TARGET.exec(l.target);
        if (page) out.push({ text: l.label, mention: { type: "page", id: page[1]!.toLowerCase() } });
        else {
          const href = docsSafeLink(l.target);
          parseInline(l.label, href && /^(https?:|mailto:)/i.test(l.target) ? { ...st, link: href } : st, out);
        }
        i = l.next - 1;
        continue;
      }
    }
    if (c === "<") {
      const end = s.indexOf(">", i);
      const inner = end > i ? s.slice(i + 1, end) : "";
      if (/^(https?:\/\/|mailto:)/i.test(inner)) {
        const href = docsSafeLink(inner);
        if (href) {
          flush();
          emit(inner, false, { ...st, link: href });
          i = end;
          continue;
        }
      }
    }
    buf += c;
  }
  flush();
}

/** Inline Markdown → spans. Bare URLs become links. */
export function docsInlineMarkdown(text: string): DocsSpan[] {
  const out: DocsSpan[] = [];
  parseInline(text, {}, out);
  // Autolink bare URLs in plain runs.
  const linked: DocsSpan[] = [];
  for (const sp of out) {
    if (sp.link || sp.code || sp.mention) {
      linked.push(sp);
      continue;
    }
    let last = 0;
    for (const m of sp.text.matchAll(/https?:\/\/[^\s<>"')\]]+/g)) {
      const at = m.index ?? 0;
      if (at > last) linked.push({ ...sp, text: sp.text.slice(last, at) });
      linked.push({ ...sp, text: m[0], link: m[0] });
      last = at + m[0].length;
    }
    if (last < sp.text.length) linked.push({ ...sp, text: sp.text.slice(last) });
  }
  return docsNormalize(linked);
}

const ESCAPE = /([\\`*_[\]~<])/g;

/** Spans → inline Markdown (people as @Name, pages as links to /docs/<id>). */
export function docsSpansToMarkdown(spans: readonly DocsSpan[], names: { user?: (id: string) => string | undefined; page?: (id: string) => string | undefined } = {}): string {
  let out = "";
  for (const sp of spans) {
    if (sp.mention) {
      if (sp.mention.type === "user") out += `@${(names.user?.(sp.mention.id) ?? sp.text.replace(/^@/, "")).replace(ESCAPE, "\\$1")}`;
      else if (sp.mention.type === "page") out += `[${(names.page?.(sp.mention.id) ?? sp.text).replace(ESCAPE, "\\$1")}](/docs/${sp.mention.id})`;
      else out += sp.text.replace(ESCAPE, "\\$1");
      continue;
    }
    const lead = /^[ \t]*/.exec(sp.text)![0];
    const trail = /[ \t]*$/.exec(sp.text.slice(lead.length))![0];
    const core = sp.text.slice(lead.length, sp.text.length - trail.length);
    if (!core) {
      out += sp.text;
      continue;
    }
    let t = sp.code ? (core.includes("`") ? `\`\`${core}\`\`` : `\`${core}\``) : core.replace(ESCAPE, "\\$1").replace(/\n/g, "  \n");
    if (sp.strike) t = `~~${t}~~`;
    if (sp.italic) t = `_${t}_`;
    if (sp.bold) t = `**${t}**`;
    if (sp.underline) t = `<u>${t}</u>`;
    if (sp.link) t = `[${t}](${sp.link})`;
    out += lead + t + trail;
  }
  return out;
}

// ---- blocks ----------------------------------------------------------------------

const HEADING = /^(#{1,6})\s+(.*)$/;
const LIST = /^(\s*)([-*+]|\d{1,9}[.)])\s+(\[( |x|X)\]\s+)?(.*)$/;
const FENCE = /^(\s*)(`{3,}|~{3,})\s*([A-Za-z0-9+#.-]*)\s*$/;
const DIVIDER = /^\s*([-*_])(\s*\1){2,}\s*$/;
const TABLE_SEP = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;
const IMAGE = /^!\[([^\]]*)\]\(([^)\s]+)\)$/;

function leadingEmoji(s: string): { icon: string; rest: string } | null {
  const m = /^(\p{Extended_Pictographic}(?:\uFE0F|\u200D\p{Extended_Pictographic}|[\u{1F3FB}-\u{1F3FF}])*)\s*(.*)$/su.exec(s);
  return m ? { icon: m[1]!, rest: m[2]! } : null;
}

function text(type: DocsBlockType, md: string): DocsDraftBlock {
  return { type, props: { text: docsInlineMarkdown(md) }, children: [] };
}

function cells(line: string): DocsSpan[][] {
  const t = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  const out: DocsSpan[][] = [];
  let cur = "";
  for (let i = 0; i < t.length; i++) {
    if (t[i] === "\\" && t[i + 1] === "|") {
      cur += "|";
      i++;
    } else if (t[i] === "|") {
      out.push(docsInlineMarkdown(cur.trim()));
      cur = "";
    } else cur += t[i];
  }
  out.push(docsInlineMarkdown(cur.trim()));
  return out.slice(0, 20);
}

function startsBlock(lines: string[], i: number): boolean {
  const l = lines[i]!;
  const t = l.trim();
  return HEADING.test(t) || LIST.test(l) || FENCE.test(l) || t.startsWith(">") || DIVIDER.test(l) || t.startsWith("<details") || (t.startsWith("|") && TABLE_SEP.test(lines[i + 1] ?? ""));
}

function parse(lines: string[], start: number, depth: number): [DocsDraftBlock[], number] {
  const out: DocsDraftBlock[] = [];
  const stack: { indent: number; block: DocsDraftBlock }[] = [];
  const add = (b: DocsDraftBlock, indent: number, listy: boolean) => {
    if (!listy) {
      stack.length = 0;
      out.push(b);
      return;
    }
    while (stack.length && stack[stack.length - 1]!.indent >= indent) stack.pop();
    const parent = stack[stack.length - 1];
    if (parent && stack.length < 10) parent.block.children.push(b);
    else out.push(b);
    stack.push({ indent, block: b });
  };
  let i = start;
  while (i < lines.length) {
    const line = lines[i]!.replace(/[ \t]+$/, "");
    const t = line.trim();
    if (t === "") {
      i++;
      continue;
    }
    if (t === "</details>") return [out, i + 1];
    if (t.startsWith("<details")) {
      i++;
      let summary = "";
      const m = /^\s*<summary>(.*)<\/summary>\s*$/.exec(lines[i] ?? "");
      if (m) {
        summary = m[1]!;
        i++;
      }
      const b = text("toggle", summary);
      if (depth < 10) [b.children, i] = parse(lines, i, depth + 1);
      add(b, 0, false);
      continue;
    }
    const fence = FENCE.exec(line);
    if (fence) {
      const code: string[] = [];
      i++;
      while (i < lines.length && lines[i]!.trim() !== fence[2]) code.push(lines[i++]!);
      i++;
      add({ type: "code", props: { code: code.join("\n"), language: (fence[3] || "plain").toLowerCase() }, children: [] }, 0, false);
      continue;
    }
    if (DIVIDER.test(line)) {
      add({ type: "divider", props: {}, children: [] }, 0, false);
      i++;
      continue;
    }
    const h = HEADING.exec(t);
    if (h) {
      add(text(`heading_${Math.min(h[1]!.length, 3)}` as DocsBlockType, h[2]!.replace(/[#\s]+$/, "")), 0, false);
      i++;
      continue;
    }
    const l = LIST.exec(line);
    if (l) {
      const indent = l[1]!.replace(/\t/g, "    ").length;
      const b = l[3] ? text("to_do", l[5]!) : text(/^\d/.test(l[2]!) ? "numbered_list" : "bulleted_list", l[5]!);
      if (l[3]) b.props.checked = /x/i.test(l[4]!);
      add(b, indent, true);
      i++;
      continue;
    }
    if (t.startsWith(">")) {
      const parts: string[] = [];
      while (i < lines.length && lines[i]!.trim().startsWith(">")) parts.push(lines[i++]!.trim().replace(/^>\s?/, ""));
      const joined = parts.join("\n");
      const e = leadingEmoji(joined);
      if (e) {
        const b = text("callout", e.rest);
        b.props.icon = e.icon;
        add(b, 0, false);
      } else add(text("quote", joined), 0, false);
      continue;
    }
    if (t.startsWith("|") && TABLE_SEP.test(lines[i + 1] ?? "")) {
      const rows: DocsSpan[][][] = [];
      while (i < lines.length && lines[i]!.trim().startsWith("|")) {
        if (!TABLE_SEP.test(lines[i]!) && rows.length < 100) rows.push(cells(lines[i]!));
        i++;
      }
      add({ type: "table", props: { rows, header_row: true }, children: [] }, 0, false);
      continue;
    }
    const img = IMAGE.exec(t);
    if (img) {
      const href = docsSafeLink(img[2]!);
      if (href && /^https?:/i.test(href)) add({ type: "bookmark", props: { url: href, title: img[1]! }, children: [] }, 0, false);
      else if (img[1]) add(text("paragraph", img[1]), 0, false);
      i++;
      continue;
    }
    const para: string[] = [];
    while (i < lines.length) {
      const pl = lines[i]!.replace(/[ \t]+$/, "");
      if (pl.trim() === "" || (para.length > 0 && startsBlock(lines, i))) break;
      para.push(pl.trimStart().replace(/\\$/, ""));
      i++;
    }
    add(text("paragraph", para.join("\n")), 0, false);
  }
  return [out, i];
}

/** Markdown → draft blocks: headings, nested lists, to-dos, quotes, callouts (a quote opening with an emoji), code fences, dividers, tables, `<details>` toggles, paragraphs. */
export function docsMarkdownToBlocks(md: string): DocsDraftBlock[] {
  return parse(md.replace(/\r\n?/g, "\n").split("\n"), 0, 0)[0];
}

/** Plain text pasted as blocks: one paragraph per line (Markdown if it looks like it). */
export function docsTextToBlocks(plain: string): DocsDraftBlock[] {
  const looksMarkdown = /(^|\n)\s*(#{1,3}\s|[-*+]\s|\d+[.)]\s|>\s|```|\|.*\|)/.test(plain) || /\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\)/.test(plain);
  if (looksMarkdown) return docsMarkdownToBlocks(plain);
  return plain
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => ({ type: "paragraph" as const, props: { text: docsInlineMarkdown(line) }, children: [] }));
}

/** Blocks (document order) → Markdown, for copy. */
export function docsBlocksToMarkdown(blocks: readonly DocsBlock[], names: Parameters<typeof docsSpansToMarkdown>[1] = {}): string {
  const ids = new Set(blocks.map((b) => b.id));
  const kids = new Map<string, DocsBlock[]>();
  const roots: DocsBlock[] = [];
  for (const b of blocks) {
    if (b.parent_id && ids.has(b.parent_id)) kids.set(b.parent_id, [...(kids.get(b.parent_id) ?? []), b]);
    else roots.push(b);
  }
  const lines: string[] = [];
  const render = (list: DocsBlock[], indent: string) => {
    let n = 0;
    for (const b of list) {
      n = b.type === "numbered_list" ? n + 1 : 0;
      const t = docsSpansToMarkdown(b.props.text ?? [], names).replace(/\n/g, `\n${indent}  `);
      let childIndent = `${indent}  `;
      switch (b.type) {
        case "heading_1":
          lines.push(`${indent}# ${t}`);
          break;
        case "heading_2":
          lines.push(`${indent}## ${t}`);
          break;
        case "heading_3":
          lines.push(`${indent}### ${t}`);
          break;
        case "bulleted_list":
          lines.push(`${indent}- ${t}`);
          break;
        case "numbered_list":
          lines.push(`${indent}${n}. ${t}`);
          childIndent = indent + " ".repeat(String(n).length + 2);
          break;
        case "to_do":
          lines.push(`${indent}- [${b.props.checked ? "x" : " "}] ${t}`);
          break;
        case "toggle":
          lines.push(`${indent}<details>`, `${indent}<summary>${t}</summary>`, "");
          render(kids.get(b.id) ?? [], indent);
          lines.push(`${indent}</details>`);
          continue;
        case "quote":
          lines.push(`${indent}> ${t.replace(/\n/g, "\n> ")}`);
          break;
        case "callout":
          lines.push(`${indent}> ${b.props.icon ?? "💡"} ${t.replace(/\n/g, "\n> ")}`);
          break;
        case "code":
          lines.push(`${indent}\`\`\`${b.props.language && b.props.language !== "plain" ? b.props.language : ""}`, b.props.code ?? "", `${indent}\`\`\``);
          break;
        case "divider":
          lines.push(`${indent}---`);
          break;
        case "bookmark":
          lines.push(`${indent}[${b.props.title || b.props.url}](${b.props.url})`);
          break;
        case "image":
        case "file":
          lines.push(`${indent}${b.props.name ?? "File"}`);
          break;
        case "page":
          lines.push(`${indent}[${names.page?.(b.props.page_id ?? "") ?? "Untitled"}](/docs/${b.props.page_id})`);
          break;
        case "database":
          lines.push(`${indent}[${names.page?.(b.props.database_id ?? "") ?? "Untitled"}](/docs/${b.props.database_id})`);
          break;
        case "connected_page":
          lines.push(`${indent}[${b.props.title || "Untitled"}](${b.props.url ?? ""})`);
          break;
        case "table":
          (b.props.rows ?? []).forEach((row, ri) => {
            lines.push(`${indent}| ${row.map((c) => docsSpansToMarkdown(c, names).replace(/\|/g, "\\|")).join(" | ")} |`);
            if (ri === 0) lines.push(`${indent}| ${row.map(() => "---").join(" | ")} |`);
          });
          break;
        default:
          lines.push(`${indent}${t}`);
      }
      render(kids.get(b.id) ?? [], childIndent);
    }
  };
  render(roots, "");
  return lines.join("\n");
}

/** Plain text of blocks (document order), one line per block. */
export function docsBlocksToText(blocks: readonly DocsBlock[]): string {
  return blocks
    .map((b) => (b.type === "code" ? (b.props.code ?? "") : b.type === "table" ? (b.props.rows ?? []).map((r) => r.map(docsPlain).join("\t")).join("\n") : docsPlain(b.props.text ?? b.props.caption ?? [])))
    .join("\n");
}
