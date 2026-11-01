/**
 * Pasted HTML → draft blocks. Only structure and the inline marks the span model has
 * survive; links keep http(s) and mailto only; everything else becomes text.
 */
import { docsNormalize, docsSafeLink, type DocsBlockType, type DocsDraftBlock, type DocsSpan } from "@scaffoldapp/shared";

interface Fmt {
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
}

const BLOCK_TAGS = new Set(["P", "DIV", "H1", "H2", "H3", "H4", "H5", "H6", "UL", "OL", "LI", "BLOCKQUOTE", "PRE", "HR", "TABLE", "SECTION", "ARTICLE", "HEADER", "FOOTER", "MAIN", "DETAILS", "FIGURE"]);

function inlineFmt(el: HTMLElement, f: Fmt): Fmt {
  const next = { ...f };
  const tag = el.tagName;
  const style = el.getAttribute("style") ?? "";
  if (tag === "B" || tag === "STRONG" || /font-weight:\s*(bold|[6-9]00)/.test(style)) next.bold = true;
  if (tag === "I" || tag === "EM" || /font-style:\s*italic/.test(style)) next.italic = true;
  if (tag === "U" || /text-decoration[^;]*underline/.test(style)) next.underline = true;
  if (tag === "S" || tag === "DEL" || tag === "STRIKE" || /line-through/.test(style)) next.strike = true;
  if (tag === "CODE" || tag === "KBD" || tag === "SAMP") next.code = true;
  if (tag === "A") {
    const href = el.getAttribute("href") ?? "";
    const safe = /^(https?:|mailto:)/i.test(href) ? docsSafeLink(href) : null;
    if (safe) next.link = safe;
  }
  return next;
}

function spansOf(node: Node, f: Fmt = {}): DocsSpan[] {
  const out: DocsSpan[] = [];
  const walk = (n: Node, fmt: Fmt) => {
    for (const c of Array.from(n.childNodes)) {
      if (c.nodeType === Node.TEXT_NODE) {
        const t = (c.textContent ?? "").replace(/\s+/g, " ");
        if (t) out.push({ ...fmt, text: t });
      } else if (c instanceof HTMLElement) {
        if (c.tagName === "BR") out.push({ ...fmt, text: "\n" });
        else if (c.tagName !== "STYLE" && c.tagName !== "SCRIPT" && !(c instanceof HTMLInputElement)) walk(c, inlineFmt(c, fmt));
      }
    }
  };
  walk(node, f);
  const spans = docsNormalize(out);
  if (spans.length) {
    spans[0] = { ...spans[0]!, text: spans[0]!.text.replace(/^\s+/, "") };
    const last = spans.length - 1;
    spans[last] = { ...spans[last]!, text: spans[last]!.text.replace(/\s+$/, "") };
  }
  return docsNormalize(spans);
}

function hasBlockChild(el: Element): boolean {
  return Array.from(el.children).some((c) => BLOCK_TAGS.has(c.tagName));
}

function text(type: DocsBlockType, spans: DocsSpan[]): DocsDraftBlock {
  return { type, props: { text: spans }, children: [] };
}

function convert(el: Element, out: DocsDraftBlock[], depth: number): void {
  if (depth > 12) return;
  const tag = el.tagName;
  switch (tag) {
    case "H1":
      out.push(text("heading_1", spansOf(el)));
      return;
    case "H2":
      out.push(text("heading_2", spansOf(el)));
      return;
    case "H3":
    case "H4":
    case "H5":
    case "H6":
      out.push(text("heading_3", spansOf(el)));
      return;
    case "HR":
      out.push({ type: "divider", props: {}, children: [] });
      return;
    case "PRE":
      out.push({ type: "code", props: { code: el.textContent ?? "", language: "plain" }, children: [] });
      return;
    case "BLOCKQUOTE":
      out.push(text("quote", spansOf(el)));
      return;
    case "UL":
    case "OL":
      for (const li of Array.from(el.children)) {
        if (li.tagName !== "LI") continue;
        const box = li.querySelector(":scope > input[type=checkbox], :scope > p > input[type=checkbox]") as HTMLInputElement | null;
        const own = li.cloneNode(true) as HTMLElement;
        own.querySelectorAll(":scope > ul, :scope > ol").forEach((n) => n.remove());
        const block = box
          ? { type: "to_do" as const, props: { text: spansOf(own), checked: box.checked || box.hasAttribute("checked") }, children: [] as DocsDraftBlock[] }
          : text(tag === "OL" ? "numbered_list" : "bulleted_list", spansOf(own));
        for (const sub of Array.from(li.children)) if (sub.tagName === "UL" || sub.tagName === "OL") convert(sub, block.children, depth + 1);
        out.push(block);
      }
      return;
    case "TABLE": {
      const rows = Array.from(el.querySelectorAll("tr"))
        .slice(0, 100)
        .map((tr) =>
          Array.from(tr.children)
            .slice(0, 20)
            .map((td) => spansOf(td)),
        );
      if (rows.length) out.push({ type: "table", props: { rows, header_row: !!el.querySelector("th") }, children: [] });
      return;
    }
    case "DETAILS": {
      const summary = el.querySelector(":scope > summary");
      const block = text("toggle", summary ? spansOf(summary) : []);
      for (const c of Array.from(el.children)) if (c !== summary) convert(c, block.children, depth + 1);
      out.push(block);
      return;
    }
    case "IMG":
      return;
  }
  if (hasBlockChild(el)) {
    let inline: Node[] = [];
    const flushInline = () => {
      const holder = document.createElement("div");
      inline.forEach((n) => holder.appendChild(n.cloneNode(true)));
      const spans = spansOf(holder);
      if (spans.length) out.push(text("paragraph", spans));
      inline = [];
    };
    for (const c of Array.from(el.childNodes)) {
      if (c instanceof Element && BLOCK_TAGS.has(c.tagName)) {
        flushInline();
        convert(c, out, depth + 1);
      } else inline.push(c);
    }
    flushInline();
    return;
  }
  const spans = spansOf(el);
  if (spans.length) out.push(text("paragraph", spans));
}

/** Clipboard HTML → blocks (null when it holds nothing usable). */
export function htmlToBlocks(html: string): DocsDraftBlock[] | null {
  const doc = new DOMParser().parseFromString(html, "text/html");
  doc.querySelectorAll("script, style, meta, link, title, template, iframe, object, embed").forEach((n) => n.remove());
  const out: DocsDraftBlock[] = [];
  convert(doc.body, out, 0);
  return out.length ? out : null;
}

/** Inline-only HTML (a single paragraph) → spans, for pasting inside a line. */
export function htmlToSpans(html: string): DocsSpan[] {
  const doc = new DOMParser().parseFromString(html, "text/html");
  doc.querySelectorAll("script, style").forEach((n) => n.remove());
  return spansOf(doc.body);
}
