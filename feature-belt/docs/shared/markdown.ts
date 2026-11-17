/**
 * Markdown ⇄ blocks for paste and copy (the server has the same rules for import, export
 * and agent tools). Portable: no DOM.
 */
import { docsEmbedFor, docsHoldsChildren, docsIsColor } from "./blocks";
import { docsNormalize, docsPlain, docsSafeLink } from "./rich";
import type { DocsBlock, DocsBlockProps, DocsBlockType, DocsColor, DocsSpan } from "./types";

/** A block not yet on a page: what paste and import produce. */
export interface DocsDraftBlock {
  type: DocsBlockType;
  props: DocsBlockProps;
  children: DocsDraftBlock[];
}

/** Whether a block type may hold children (see `docsHoldsChildren` for props-aware rules: toggle headings, synced copies). */
export function docsIsNestable(type: DocsBlockType, props?: DocsBlockProps): boolean {
  return docsHoldsChildren(type, props);
}

// ---- inline ----------------------------------------------------------------------

interface InlineState {
  bold?: boolean;
  italic?: boolean;
  strike?: boolean;
  underline?: boolean;
  link?: string;
  color?: DocsColor;
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
    if (state.color) sp.color = state.color;
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
    if (c === "$" && i + 1 < s.length && !/[\s\d$]/.test(s[i + 1]!)) {
      // $TeX$ (an opening $ before a digit is money): the closing $ follows a non-space and isn't before a digit.
      let end = -1;
      for (let j = i + 1; j < s.length; j++) {
        if (s[j] === "\\") {
          j++;
          continue;
        }
        if (s[j] === "\n") break;
        if (s[j] === "$" && !/\s/.test(s[j - 1]!) && !/\d/.test(s[j + 1] ?? "")) {
          end = j;
          break;
        }
      }
      if (end > i + 1) {
        flush();
        out.push({ text: s.slice(i + 1, end).replace(/\\\$/g, "$"), equation: true });
        i = end;
        continue;
      }
    }
    if (s.startsWith('<span color="', i)) {
      const m = /^<span color="([a-z_]{1,20})">/.exec(s.slice(i, i + 40));
      const end = m ? findAhead(s, i + m[0].length, "</span>") : -1;
      if (m && end >= 0) {
        flush();
        parseInline(s.slice(i + m[0].length, end), docsIsColor(m[1]) ? { ...st, color: m[1] } : st, out);
        i = end + "</span>".length - 1;
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
    if (sp.link || sp.code || sp.mention || sp.equation) {
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
const escape = (t: string) => t.replace(ESCAPE, "\\$1").replace(/\$(?=[^\s\d$])/g, "\\$");

/** Spans → inline Markdown (people as @Name, pages as links to /docs/<id>). */
export function docsSpansToMarkdown(spans: readonly DocsSpan[], names: { user?: (id: string) => string | undefined; page?: (id: string) => string | undefined } = {}): string {
  let out = "";
  for (const sp of spans) {
    if (sp.equation) {
      const tex = sp.text.replace(/\$/g, "\\$");
      out += `$${/^\d/.test(tex) ? `{}${tex}` : tex}$`;
      continue;
    }
    if (sp.mention) {
      if (sp.mention.type === "user") out += `@${escape(names.user?.(sp.mention.id) ?? sp.text.replace(/^@/, ""))}`;
      else if (sp.mention.type === "page") out += `[${escape(names.page?.(sp.mention.id) ?? sp.text)}](/docs/${sp.mention.id})`;
      else out += escape(sp.text);
      continue;
    }
    const lead = /^[ \t]*/.exec(sp.text)![0];
    const trail = /[ \t]*$/.exec(sp.text.slice(lead.length))![0];
    const core = sp.text.slice(lead.length, sp.text.length - trail.length);
    if (!core) {
      out += sp.text;
      continue;
    }
    let t = sp.code ? (core.includes("`") ? `\`\`${core}\`\`` : `\`${core}\``) : escape(core).replace(/\n/g, "  \n");
    if (sp.strike) t = `~~${t}~~`;
    if (sp.italic) t = `_${t}_`;
    if (sp.bold) t = `**${t}**`;
    if (sp.underline) t = `<u>${t}</u>`;
    if (sp.color) t = `<span color="${sp.color}">${t}</span>`;
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
const SUMMARY = /^\s*<summary>(.*)<\/summary>\s*$/;
const SUMMARY_HEAD = /^(#{1,4})\s+(.*)$/;
const COLUMN = /^\s*<column(?:\s+width="([0-9.]{1,6})")?\s*>\s*$/;
const TOC = /^(\[toc\]|\[\[_toc_\]\]|<!--\s*toc\s*-->)$/i;
const BREADCRUMB = /^<!--\s*breadcrumb\s*-->$/i;
const BARE_URL = /^<?(https?:\/\/[^\s<>]+)>?$/;
const CLOSERS: Record<string, string> = { "</details>": "details", "</column>": "column", "</columns>": "columns" };

function leadingEmoji(s: string): { icon: string; rest: string } | null {
  const m = /^(\p{Extended_Pictographic}(?:️|‍\p{Extended_Pictographic}|[\u{1F3FB}-\u{1F3FF}])*)\s*(.*)$/su.exec(s);
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
  return (
    HEADING.test(t) ||
    LIST.test(l) ||
    FENCE.test(l) ||
    t.startsWith(">") ||
    DIVIDER.test(l) ||
    t.startsWith("<details") ||
    t in CLOSERS ||
    t === "<columns>" ||
    t === "$$" ||
    TOC.test(t) ||
    (t.startsWith("|") && TABLE_SEP.test(lines[i + 1] ?? ""))
  );
}

/** Parses blocks from `start` until a closing tag (left unconsumed) or the end: [blocks, next line, closer]. */
function parse(lines: string[], start: number, depth: number): [DocsDraftBlock[], number, string] {
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
  const nested = (from: number, want: string): [DocsDraftBlock[], number] => {
    if (depth >= 10) return [[], from];
    const [kids, j, closer] = parse(lines, from, depth + 1);
    return [kids, closer === want ? j + 1 : j];
  };
  let i = start;
  while (i < lines.length) {
    const line = lines[i]!.replace(/[ \t]+$/, "");
    const t = line.trim();
    if (t === "") {
      i++;
      continue;
    }
    if (t in CLOSERS) return [out, i, CLOSERS[t]!];
    if (t.startsWith("<details")) {
      i++;
      let summary = "";
      const m = SUMMARY.exec(lines[i] ?? "");
      if (m) {
        summary = m[1]!;
        i++;
      }
      const h = SUMMARY_HEAD.exec(summary.trim());
      const b = h ? text(`heading_${h[1]!.length}` as DocsBlockType, h[2]!) : text("toggle", summary);
      if (h) b.props.toggleable = true;
      [b.children, i] = nested(i, "details");
      add(b, 0, false);
      continue;
    }
    if (t === "<columns>") {
      const list: DocsDraftBlock = { type: "column_list", props: {}, children: [] };
      i++;
      while (i < lines.length) {
        const ct = lines[i]!.trim();
        if (ct === "") {
          i++;
          continue;
        }
        if (ct === "</columns>") {
          i++;
          break;
        }
        const cm = COLUMN.exec(ct);
        if (!cm || list.children.length >= 5) break;
        const col: DocsDraftBlock = { type: "column", props: {}, children: [] };
        const w = Number(cm[1]);
        if (cm[1] && w > 0 && w <= 1) col.props.width = w;
        [col.children, i] = nested(i + 1, "column");
        list.children.push(col);
      }
      if (list.children.length === 1) for (const k of list.children[0]!.children) add(k, 0, false);
      else if (list.children.length > 1) add(list, 0, false);
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
    if (t === "$$") {
      const expr: string[] = [];
      i++;
      while (i < lines.length && lines[i]!.trim() !== "$$") expr.push(lines[i++]!.trim());
      i++;
      add({ type: "equation", props: { expression: expr.join("\n") }, children: [] }, 0, false);
      continue;
    }
    if (t.length > 4 && t.startsWith("$$") && t.endsWith("$$")) {
      add({ type: "equation", props: { expression: t.slice(2, -2).trim() }, children: [] }, 0, false);
      i++;
      continue;
    }
    if (TOC.test(t) || BREADCRUMB.test(t)) {
      add({ type: TOC.test(t) ? "table_of_contents" : "breadcrumb", props: {}, children: [] }, 0, false);
      i++;
      continue;
    }
    if (DIVIDER.test(line)) {
      add({ type: "divider", props: {}, children: [] }, 0, false);
      i++;
      continue;
    }
    const h = HEADING.exec(t);
    if (h) {
      add(text(`heading_${Math.min(h[1]!.length, 4)}` as DocsBlockType, h[2]!.replace(/[#\s]+$/, "")), 0, false);
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
    const bare = BARE_URL.exec(t);
    if (bare && docsEmbedFor(bare[1]!)) {
      add({ type: "embed", props: { url: bare[1]! }, children: [] }, 0, false);
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
  return [out, i, ""];
}

/**
 * Markdown → draft blocks: headings 1–4, nested lists, to-dos, quotes, callouts (a quote opening with an emoji), code
 * fences, $$ equations, dividers, tables, `<details>` toggles (a "# …" summary: toggle heading), `<columns>`, [TOC],
 * `<!-- breadcrumb -->`, a lone embeddable link as an embed, paragraphs. The server parses the same way.
 */
export function docsMarkdownToBlocks(md: string): DocsDraftBlock[] {
  const lines = md.replace(/\r\n?/g, "\n").split("\n");
  const out: DocsDraftBlock[] = [];
  let i = 0;
  while (i < lines.length) {
    const [part, next] = parse(lines, i, 0);
    out.push(...part);
    i = Math.max(next, i + 1);
    if (i < lines.length && lines[i]!.trim() in CLOSERS) i++;
  }
  return out;
}

/** Plain text pasted as blocks: one paragraph per line (Markdown if it looks like it). */
export function docsTextToBlocks(plain: string): DocsDraftBlock[] {
  const looksMarkdown = /(^|\n)\s*(#{1,4}\s|[-*+]\s|\d+[.)]\s|>\s|```|\$\$|\|.*\|)/.test(plain) || /\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\)/.test(plain);
  if (looksMarkdown) return docsMarkdownToBlocks(plain);
  return plain
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => ({ type: "paragraph" as const, props: { text: docsInlineMarkdown(line) }, children: [] }));
}

/** Blocks (document order) → Markdown, for copy. The server's export renders the same way. */
export function docsBlocksToMarkdown(blocks: readonly DocsBlock[], names: Parameters<typeof docsSpansToMarkdown>[1] = {}): string {
  const ids = new Set(blocks.map((b) => b.id));
  const kids = new Map<string, DocsBlock[]>();
  const roots: DocsBlock[] = [];
  for (const b of blocks) {
    if (b.parent_id && ids.has(b.parent_id)) kids.set(b.parent_id, [...(kids.get(b.parent_id) ?? []), b]);
    else roots.push(b);
  }
  const lines: string[] = [];
  const render = (list: DocsBlock[], indent: string, depth: number) => {
    let n = 0;
    for (const b of list) {
      n = b.type === "numbered_list" ? n + 1 : 0;
      const t = docsSpansToMarkdown(b.props.text ?? [], names).replace(/\n/g, `\n${indent}  `);
      let childIndent = `${indent}  `;
      if (depth > 30) return;
      switch (b.type) {
        case "heading_1":
        case "heading_2":
        case "heading_3":
        case "heading_4": {
          const hashes = "#".repeat(Number(b.type.slice(-1)));
          if (b.props.toggleable) {
            lines.push(`${indent}<details>`, `${indent}<summary>${hashes} ${t.replace(/\n/g, " ")}</summary>`, "");
            render(kids.get(b.id) ?? [], indent, depth + 1);
            lines.push(`${indent}</details>`);
            continue;
          }
          lines.push(`${indent}${hashes} ${t}`);
          break;
        }
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
          render(kids.get(b.id) ?? [], indent, depth + 1);
          lines.push(`${indent}</details>`);
          continue;
        case "quote":
          lines.push(`${indent}> ${t.replace(/\n/g, "\n> ")}`);
          break;
        case "callout":
          lines.push(`${indent}> ${b.props.icon ?? "💡"} ${t.replace(/\n/g, "\n> ")}`);
          break;
        case "code": {
          let fence = "```";
          while ((b.props.code ?? "").includes(fence)) fence += "`";
          lines.push(`${indent}${fence}${b.props.language && b.props.language !== "plain" ? b.props.language : ""}`, b.props.code ?? "", `${indent}${fence}`);
          break;
        }
        case "equation":
          lines.push(`${indent}$$`, `${indent}${(b.props.expression ?? "").replace(/\n/g, `\n${indent}`)}`, `${indent}$$`);
          break;
        case "divider":
          lines.push(`${indent}---`);
          break;
        case "table_of_contents":
          lines.push(`${indent}[TOC]`);
          break;
        case "breadcrumb":
          lines.push(`${indent}<!-- breadcrumb -->`);
          break;
        case "bookmark":
          lines.push(`${indent}[${b.props.title || b.props.url}](${b.props.url})`);
          break;
        case "embed":
          lines.push(`${indent}${b.props.url ?? ""}`);
          break;
        case "image":
        case "file":
        case "video":
        case "audio":
        case "pdf":
          lines.push(`${indent}${b.props.name ?? "File"}`);
          break;
        case "page":
        case "link_to_page":
          lines.push(`${indent}[${names.page?.(b.props.page_id ?? "") ?? "Untitled"}](/docs/${b.props.page_id})`);
          break;
        case "database":
          lines.push(`${indent}[${names.page?.(b.props.database_id ?? "") ?? "Untitled"}](/docs/${b.props.database_id})`);
          break;
        case "connected_page":
          lines.push(`${indent}[${b.props.title || "Untitled"}](${b.props.url ?? ""})`);
          break;
        case "button":
          lines.push(`${indent}[${b.props.label ?? "Button"}] (button)`);
          break;
        case "column_list":
          lines.push(`${indent}<columns>`);
          for (const col of kids.get(b.id) ?? []) {
            lines.push(`${indent}<column${col.props.width ? ` width="${col.props.width}"` : ""}>`, "");
            render(kids.get(col.id) ?? [], indent, depth + 2);
            lines.push("", `${indent}</column>`);
          }
          lines.push(`${indent}</columns>`);
          continue;
        case "column":
        case "synced_block":
          render(kids.get(b.id) ?? [], indent, depth + 1);
          continue;
        case "table":
          (b.props.rows ?? []).forEach((row, ri) => {
            lines.push(`${indent}| ${row.map((c) => docsSpansToMarkdown(c, names).replace(/\|/g, "\\|")).join(" | ")} |`);
            if (ri === 0) lines.push(`${indent}| ${row.map(() => "---").join(" | ")} |`);
          });
          break;
        default:
          lines.push(`${indent}${t}`);
      }
      render(kids.get(b.id) ?? [], childIndent, depth + 1);
    }
  };
  render(roots, "", 0);
  return lines.join("\n");
}

/** Plain text of blocks (document order), one line per block. */
export function docsBlocksToText(blocks: readonly DocsBlock[]): string {
  return blocks
    .map((b) =>
      b.type === "code"
        ? (b.props.code ?? "")
        : b.type === "equation"
          ? (b.props.expression ?? "")
          : b.type === "table"
            ? (b.props.rows ?? []).map((r) => r.map(docsPlain).join("\t")).join("\n")
            : b.type === "embed" || b.type === "bookmark"
              ? (b.props.url ?? "")
              : docsPlain(b.props.text ?? b.props.caption ?? []),
    )
    .join("\n");
}
