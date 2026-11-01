/**
 * A small Markdown reader for previews: headings, paragraphs, lists, block quotes, fenced
 * code, rules and tables, with inline code, bold, italic and links. It produces a tree the
 * web and mobile previews render with their own components (never HTML), so a file can't
 * inject markup. Links keep only http(s), mailto and relative targets.
 */

export type FilesMdInline =
  | { type: "text"; text: string }
  | { type: "code"; text: string }
  | { type: "strong"; children: FilesMdInline[] }
  | { type: "em"; children: FilesMdInline[] }
  | { type: "link"; href: string; children: FilesMdInline[] };

export type FilesMdBlock =
  | { type: "heading"; level: 1 | 2 | 3 | 4 | 5 | 6; children: FilesMdInline[] }
  | { type: "paragraph"; children: FilesMdInline[] }
  | { type: "list"; ordered: boolean; start: number; items: FilesMdInline[][] }
  | { type: "quote"; children: FilesMdInline[] }
  | { type: "code"; lang: string; text: string }
  | { type: "table"; header: FilesMdInline[][]; rows: FilesMdInline[][][] }
  | { type: "rule" };

/** The link target if it is http(s), mailto or relative; null for anything else (javascript:, data:, //host). */
export function filesSafeHref(href: string): string | null {
  const h = href.trim();
  if (!h || h.startsWith("//")) return null;
  const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(h);
  if (scheme) return /^(https?|mailto)$/i.test(scheme[1] ?? "") ? h : null;
  return h;
}

/** Inline Markdown → nodes. */
export function filesParseInline(src: string): FilesMdInline[] {
  const out: FilesMdInline[] = [];
  let text = "";
  const flush = () => {
    if (text) out.push({ type: "text", text });
    text = "";
  };
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === "\\" && i + 1 < src.length && /[\\`*_[\]()#+\-.!>|]/.test(src[i + 1] ?? "")) {
      text += src[i + 1];
      i += 2;
      continue;
    }
    if (c === "`") {
      const end = src.indexOf("`", i + 1);
      if (end > i) {
        flush();
        out.push({ type: "code", text: src.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    }
    if ((c === "*" || c === "_") && src[i + 1] === c) {
      const end = src.indexOf(c + c, i + 2);
      if (end > i + 2) {
        flush();
        out.push({ type: "strong", children: filesParseInline(src.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
    }
    if (c === "*" || (c === "_" && (i === 0 || /\W/.test(src[i - 1] ?? "")))) {
      const end = src.indexOf(c, i + 1);
      if (end > i + 1 && src[i + 1] !== " ") {
        flush();
        out.push({ type: "em", children: filesParseInline(src.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }
    if (c === "[") {
      const close = src.indexOf("]", i + 1);
      if (close > i && src[close + 1] === "(") {
        const end = src.indexOf(")", close + 2);
        if (end > close) {
          const href = filesSafeHref(src.slice(close + 2, end).split(/\s+/)[0] ?? "");
          const label = filesParseInline(src.slice(i + 1, close));
          flush();
          if (href) out.push({ type: "link", href, children: label });
          else out.push(...label);
          i = end + 1;
          continue;
        }
      }
    }
    text += c;
    i++;
  }
  flush();
  return out;
}

function tableCells(line: string): string[] {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim());
}

/** Block Markdown → nodes (at most `maxBlocks`). */
export function filesParseMarkdown(src: string, maxBlocks = 2000): FilesMdBlock[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const blocks: FilesMdBlock[] = [];
  let para: string[] = [];
  const endPara = () => {
    if (para.length) blocks.push({ type: "paragraph", children: filesParseInline(para.join(" ")) });
    para = [];
  };
  for (let i = 0; i < lines.length && blocks.length < maxBlocks; i++) {
    const line = lines[i] ?? "";
    const fence = /^\s*(```|~~~)\s*([\w+-]*)/.exec(line);
    if (fence) {
      endPara();
      const body: string[] = [];
      for (i++; i < lines.length && !(lines[i] ?? "").trim().startsWith(fence[1] ?? "```"); i++) body.push(lines[i] ?? "");
      blocks.push({ type: "code", lang: fence[2] ?? "", text: body.join("\n") });
      continue;
    }
    if (!line.trim()) {
      endPara();
      continue;
    }
    const heading = /^(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
    if (heading) {
      endPara();
      blocks.push({ type: "heading", level: (heading[1]?.length ?? 1) as 1 | 2 | 3 | 4 | 5 | 6, children: filesParseInline(heading[2] ?? "") });
      continue;
    }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) {
      endPara();
      blocks.push({ type: "rule" });
      continue;
    }
    if (/^\s*>/.test(line)) {
      endPara();
      const body: string[] = [];
      for (; i < lines.length && /^\s*>/.test(lines[i] ?? ""); i++) body.push((lines[i] ?? "").replace(/^\s*>\s?/, ""));
      i--;
      blocks.push({ type: "quote", children: filesParseInline(body.join(" ")) });
      continue;
    }
    const item = /^\s*(?:([-*+])|(\d+)[.)])\s+(.*)$/.exec(line);
    if (item) {
      endPara();
      const ordered = item[2] !== undefined;
      const items: FilesMdInline[][] = [];
      for (; i < lines.length; i++) {
        const m = /^\s*(?:([-*+])|(\d+)[.)])\s+(.*)$/.exec(lines[i] ?? "");
        if (m && (m[2] !== undefined) === ordered) items.push(filesParseInline(m[3] ?? ""));
        else if (m || !(lines[i] ?? "").trim() || !/^\s{2,}/.test(lines[i] ?? "")) break;
        else {
          const last = items[items.length - 1];
          if (last) last.push({ type: "text", text: " " }, ...filesParseInline((lines[i] ?? "").trim()));
        }
      }
      i--;
      blocks.push({ type: "list", ordered, start: ordered ? Number(item[2]) : 1, items });
      continue;
    }
    if (line.includes("|") && /^\s*\|?\s*:?-{3,}/.test(lines[i + 1] ?? "")) {
      endPara();
      const header = tableCells(line).map(filesParseInline);
      const rows: FilesMdInline[][][] = [];
      for (i += 2; i < lines.length && (lines[i] ?? "").includes("|") && (lines[i] ?? "").trim(); i++) rows.push(tableCells(lines[i] ?? "").map(filesParseInline));
      i--;
      blocks.push({ type: "table", header, rows });
      continue;
    }
    para.push(line.trim());
  }
  endPara();
  return blocks;
}

/** Plain text of inline nodes (accessible labels, search). */
export function filesInlineText(nodes: FilesMdInline[]): string {
  return nodes.map((n) => (n.type === "text" || n.type === "code" ? n.text : filesInlineText(n.children))).join("");
}
