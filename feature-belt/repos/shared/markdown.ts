/**
 * README rendering: `marked` (MIT) lexes Markdown into tokens and this reduces them to
 * a small tree each platform renders with its own components. Raw HTML in the source
 * is shown as text, never parsed; links keep only http(s), mailto and in-repository
 * paths, so a README can't run script or point at javascript: URLs.
 */
import { Lexer, type Token, type Tokens } from "marked";

export type ReposMdInline =
  | { t: "text"; text: string }
  | { t: "strong" | "em" | "del"; c: ReposMdInline[] }
  | { t: "code"; text: string }
  | { t: "link"; href: string; c: ReposMdInline[] }
  | { t: "image"; src: string; alt: string }
  | { t: "br" };

export type ReposMdBlock =
  | { t: "heading"; depth: number; c: ReposMdInline[] }
  | { t: "paragraph"; c: ReposMdInline[] }
  | { t: "code"; lang: string; text: string }
  | { t: "list"; ordered: boolean; start: number; items: { task: boolean; checked: boolean; c: ReposMdBlock[] }[] }
  | { t: "quote"; c: ReposMdBlock[] }
  | { t: "hr" }
  | { t: "table"; align: ("left" | "center" | "right" | null)[]; header: ReposMdInline[][]; rows: ReposMdInline[][][] }
  | { t: "text"; text: string };

const MAX_MARKDOWN = 400_000;

/** A link target that is safe to follow: http(s), mailto, an anchor, or a relative path. */
export function reposSafeHref(href: string): string | null {
  const h = href.trim();
  if (!h) return null;
  if (/^(https?:|mailto:)/i.test(h)) return h;
  if (/^[a-z][a-z0-9+.-]*:/i.test(h) || h.startsWith("//")) return null; // javascript:, data:, protocol-relative…
  return h;
}

/** Resolve a relative path from a README in `dir` ("docs") to a repository path. */
export function reposResolvePath(dir: string, rel: string): string {
  const [pathPart] = rel.split(/[?#]/);
  const parts = (rel.startsWith("/") ? [] : dir.split("/").filter(Boolean)).concat((pathPart ?? "").split("/"));
  const out: string[] = [];
  for (const p of parts) {
    if (!p || p === ".") continue;
    if (p === "..") out.pop();
    else out.push(p);
  }
  return out.join("/");
}

function inlines(tokens: Token[] | undefined): ReposMdInline[] {
  const out: ReposMdInline[] = [];
  for (const tok of tokens ?? []) {
    switch (tok.type) {
      case "text": {
        const t = tok as Tokens.Text;
        if (t.tokens && t.tokens.length) out.push(...inlines(t.tokens));
        else out.push({ t: "text", text: t.text });
        break;
      }
      case "escape":
      case "html":
        out.push({ t: "text", text: (tok as Tokens.Escape).text });
        break;
      case "strong":
      case "em":
      case "del":
        out.push({ t: tok.type, c: inlines((tok as Tokens.Strong).tokens) });
        break;
      case "codespan":
        out.push({ t: "code", text: (tok as Tokens.Codespan).text });
        break;
      case "br":
        out.push({ t: "br" });
        break;
      case "link": {
        const l = tok as Tokens.Link;
        const href = reposSafeHref(l.href);
        const c = inlines(l.tokens);
        if (href) out.push({ t: "link", href, c });
        else out.push(...c);
        break;
      }
      case "image": {
        const i = tok as Tokens.Image;
        const src = reposSafeHref(i.href);
        if (src) out.push({ t: "image", src, alt: i.text });
        else out.push({ t: "text", text: i.text });
        break;
      }
      default:
        if ("text" in tok && typeof tok.text === "string") out.push({ t: "text", text: tok.text });
    }
  }
  return out;
}

function blocks(tokens: Token[]): ReposMdBlock[] {
  const out: ReposMdBlock[] = [];
  for (const tok of tokens) {
    switch (tok.type) {
      case "space":
      case "def":
        break;
      case "heading": {
        const h = tok as Tokens.Heading;
        out.push({ t: "heading", depth: Math.min(Math.max(h.depth, 1), 6), c: inlines(h.tokens) });
        break;
      }
      case "paragraph":
        out.push({ t: "paragraph", c: inlines((tok as Tokens.Paragraph).tokens) });
        break;
      case "code": {
        const c = tok as Tokens.Code;
        out.push({ t: "code", lang: (c.lang ?? "").split(/\s/)[0] ?? "", text: c.text });
        break;
      }
      case "list": {
        const l = tok as Tokens.List;
        out.push({
          t: "list",
          ordered: l.ordered,
          start: typeof l.start === "number" ? l.start : 1,
          items: l.items.map((it) => ({ task: it.task, checked: !!it.checked, c: blocks(it.tokens) })),
        });
        break;
      }
      case "blockquote":
        out.push({ t: "quote", c: blocks((tok as Tokens.Blockquote).tokens) });
        break;
      case "hr":
        out.push({ t: "hr" });
        break;
      case "table": {
        const t = tok as Tokens.Table;
        out.push({
          t: "table",
          align: t.align.map((a) => a ?? null),
          header: t.header.map((cell) => inlines(cell.tokens)),
          rows: t.rows.map((row) => row.map((cell) => inlines(cell.tokens))),
        });
        break;
      }
      case "text": {
        const t = tok as Tokens.Text;
        out.push({ t: "paragraph", c: t.tokens ? inlines(t.tokens) : [{ t: "text", text: t.text }] });
        break;
      }
      case "html":
        out.push({ t: "text", text: (tok as Tokens.HTML).text });
        break;
      default:
        if ("raw" in tok && typeof tok.raw === "string" && tok.raw.trim()) out.push({ t: "text", text: tok.raw });
    }
  }
  return out;
}

/** Parse Markdown into blocks (GitHub-flavoured: tables, task lists, strikethrough). */
export function reposParseMarkdown(source: string): ReposMdBlock[] {
  const text = source.length > MAX_MARKDOWN ? source.slice(0, MAX_MARKDOWN) : source;
  try {
    return blocks(new Lexer({ gfm: true }).lex(text));
  } catch {
    return [{ t: "text", text }];
  }
}

/** Plain text of inline nodes (headings' anchor ids, alt text). */
export function reposInlineText(nodes: ReposMdInline[]): string {
  return nodes
    .map((n) => {
      switch (n.t) {
        case "text":
        case "code":
          return n.text;
        case "image":
          return n.alt;
        case "br":
          return " ";
        default:
          return reposInlineText(n.c);
      }
    })
    .join("");
}
