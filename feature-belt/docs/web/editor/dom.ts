/**
 * Rich text ⇄ DOM for contenteditable blocks. Each span renders as one element whose data
 * attributes carry its marks; mentions are non-editable chips; comment highlights are
 * <mark> wrappers that reading ignores. Offsets count UTF-16 units of the plain text
 * (a chip counts its label's length), matching shared/rich.ts.
 */
import { docsFormatDate, docsNormalize, type DocsRefs, type DocsSpan } from "@scaffoldapp/shared";

export interface Decoration {
  start: number;
  end: number;
  /** Thread id, for clicks. */
  id: string;
  active?: boolean;
}

const MARK_CLASS = {
  bold: "font-semibold",
  italic: "italic",
  underline: "underline underline-offset-2",
  strike: "line-through",
  code: "rounded-xs bg-sunken px-1 font-mono",
} as const;

/** The label a mention shows now (live names, "No access" for pages you can't open). */
export function mentionLabel(sp: DocsSpan, refs?: DocsRefs): string {
  const m = sp.mention!;
  if (m.type === "user") {
    const u = refs?.users[m.id];
    return u ? `@${u.name}` : sp.text.startsWith("@") ? sp.text : `@${sp.text}`;
  }
  if (m.type === "page") {
    const p = refs?.pages[m.id];
    if (p && p.title === null) return "No access";
    return p?.title ? `${p.icon ? `${p.icon} ` : ""}${p.title}` : sp.text;
  }
  return docsFormatDate(m.date) || sp.text;
}

function splitAt(spans: DocsSpan[], cuts: number[]): DocsSpan[] {
  const out: DocsSpan[] = [];
  let at = 0;
  for (const sp of spans) {
    const s = at;
    const e = at + sp.text.length;
    at = e;
    if (sp.mention) {
      out.push(sp);
      continue;
    }
    let from = 0;
    for (const c of cuts) {
      if (c > s && c < e) {
        out.push({ ...sp, text: sp.text.slice(from, c - s) });
        from = c - s;
      }
    }
    out.push({ ...sp, text: sp.text.slice(from) });
  }
  return out.filter((x) => x.text);
}

/** Replaces el's children with spans (mention chips show live labels from refs). */
export function renderSpans(el: HTMLElement, spans: readonly DocsSpan[], refs?: DocsRefs, decorations: readonly Decoration[] = []): void {
  const cuts = decorations.flatMap((d) => [d.start, d.end]).sort((a, b) => a - b);
  const parts = cuts.length ? splitAt([...spans], cuts) : spans;
  const frag = document.createDocumentFragment();
  let at = 0;
  for (const sp of parts) {
    const s = at;
    at += sp.text.length;
    let node: HTMLElement;
    if (sp.mention) {
      node = document.createElement("span");
      node.contentEditable = "false";
      node.dataset["mention"] = JSON.stringify(sp.mention);
      node.dataset["text"] = sp.text;
      node.className =
        sp.mention.type === "user"
          ? "mx-px rounded-xs bg-primary-soft px-1 text-primary-strong"
          : sp.mention.type === "page"
            ? "mx-px cursor-pointer rounded-xs underline decoration-border-strong underline-offset-2 hover:bg-sunken"
            : "mx-px rounded-xs bg-sunken px-1 text-fg-muted";
      node.textContent = mentionLabel(sp, refs);
      if (sp.mention.type === "page") node.dataset["pageId"] = sp.mention.id;
    } else if (sp.link) {
      const a = document.createElement("a");
      a.href = sp.link;
      a.target = "_blank";
      a.rel = "noopener noreferrer nofollow";
      a.dataset["link"] = sp.link;
      a.className = "text-primary-strong underline decoration-primary/40 underline-offset-2";
      node = a;
      node.textContent = sp.text;
    } else {
      node = document.createElement(sp.code ? "code" : "span");
      node.textContent = sp.text;
    }
    const classes: string[] = [];
    for (const m of ["bold", "italic", "underline", "strike", "code"] as const) {
      if (sp[m]) {
        node.dataset[m] = "1";
        if (!(m === "code" && sp.mention)) classes.push(MARK_CLASS[m]);
      }
    }
    if (classes.length) node.className = `${node.className} ${classes.join(" ")}`.trim();
    const deco = decorations.find((d) => s >= d.start && s < d.end);
    if (deco) {
      const mark = document.createElement("mark");
      mark.dataset["thread"] = deco.id;
      mark.className = deco.active ? "rounded-xs bg-warning-soft text-fg ring-1 ring-warning" : "rounded-xs bg-warning-soft text-fg";
      mark.appendChild(node);
      frag.appendChild(mark);
    } else frag.appendChild(node);
  }
  if (spans.length && spans[spans.length - 1]!.text.endsWith("\n")) {
    const br = document.createElement("br");
    br.dataset["trailing"] = "1";
    frag.appendChild(br);
  }
  el.replaceChildren(frag);
}

interface Fmt {
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
}

function fmtOf(el: HTMLElement, parent: Fmt): Fmt {
  const f: Fmt = { ...parent };
  const tag = el.tagName;
  const d = el.dataset;
  if (d["bold"] || tag === "B" || tag === "STRONG") f.bold = true;
  if (d["italic"] || tag === "I" || tag === "EM") f.italic = true;
  if (d["underline"] || tag === "U") f.underline = true;
  if (d["strike"] || tag === "S" || tag === "DEL" || tag === "STRIKE") f.strike = true;
  if (d["code"] || tag === "CODE") f.code = true;
  if (tag === "A") {
    const href = el.getAttribute("href");
    if (href && /^(https?:|mailto:)/i.test(href)) f.link = href;
  }
  return f;
}

/** Reads the spans back from el (what the person typed). */
export function readSpans(el: HTMLElement): DocsSpan[] {
  const out: DocsSpan[] = [];
  const walk = (node: Node, fmt: Fmt) => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === Node.TEXT_NODE) {
        const text = (child.textContent ?? "").replace(/​/g, "");
        if (text) out.push({ ...fmt, text });
        continue;
      }
      if (!(child instanceof HTMLElement)) continue;
      if (child.dataset["mention"]) {
        try {
          out.push({ text: child.dataset["text"] || child.textContent || "", mention: JSON.parse(child.dataset["mention"]), ...(fmt.bold ? { bold: true } : {}), ...(fmt.italic ? { italic: true } : {}) });
        } catch {
          out.push({ ...fmt, text: child.textContent ?? "" });
        }
        continue;
      }
      if (child.tagName === "BR") {
        if (!child.dataset["trailing"]) out.push({ ...fmt, text: "\n" });
        continue;
      }
      if (child.tagName === "DIV" || child.tagName === "P") {
        // Some browsers wrap new lines in blocks; keep them as line breaks.
        if (out.length && !out[out.length - 1]!.text.endsWith("\n")) out.push({ ...fmt, text: "\n" });
      }
      walk(child, child.tagName === "MARK" ? fmt : fmtOf(child, fmt));
    }
  };
  walk(el, {});
  return docsNormalize(out);
}

/** The length a node contributes to the plain text. */
function nodeLength(node: Node): number {
  if (node.nodeType === Node.TEXT_NODE) return (node.textContent ?? "").replace(/​/g, "").length;
  if (node instanceof HTMLElement) {
    if (node.dataset["mention"]) return (node.dataset["text"] || node.textContent || "").length;
    if (node.tagName === "BR") return node.dataset["trailing"] ? 0 : 1;
  }
  let n = 0;
  for (const c of Array.from(node.childNodes)) n += nodeLength(c);
  return n;
}

/** Plain-text offset of a DOM position inside el. */
export function offsetOf(el: HTMLElement, node: Node, offset: number): number {
  if (!el.contains(node)) return node.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING ? 0 : nodeLength(el);
  let total = 0;
  // A position inside a chip counts as after it.
  let chip: Node | null = node;
  while (chip && chip !== el) {
    if (chip instanceof HTMLElement && chip.dataset["mention"]) {
      node = chip.parentNode!;
      offset = Array.prototype.indexOf.call(node.childNodes, chip) + 1;
      break;
    }
    chip = chip.parentNode;
  }
  const walk = (n: Node): boolean => {
    if (n === node) {
      if (n.nodeType === Node.TEXT_NODE) {
        total += (n.textContent ?? "").slice(0, offset).replace(/​/g, "").length;
      } else {
        const kids = Array.from(n.childNodes);
        for (let i = 0; i < offset && i < kids.length; i++) total += nodeLength(kids[i]!);
      }
      return true;
    }
    if (n.nodeType === Node.TEXT_NODE || (n instanceof HTMLElement && (n.dataset["mention"] || n.tagName === "BR"))) {
      total += nodeLength(n);
      return false;
    }
    for (const c of Array.from(n.childNodes)) if (walk(c)) return true;
    return false;
  };
  walk(el);
  return total;
}

/** The selection inside el as plain-text offsets, or null when it isn't in el. */
export function getCaret(el: HTMLElement): { start: number; end: number } | null {
  const sel = window.getSelection();
  if (!sel || sel.rangeCount === 0) return null;
  const r = sel.getRangeAt(0);
  if (!el.contains(r.startContainer) && r.startContainer !== el) return null;
  const a = offsetOf(el, r.startContainer, r.startOffset);
  const b = el.contains(r.endContainer) || r.endContainer === el ? offsetOf(el, r.endContainer, r.endOffset) : nodeLength(el);
  return { start: Math.min(a, b), end: Math.max(a, b) };
}

/** The DOM position for a plain-text offset (after a chip, never inside). */
function positionAt(el: HTMLElement, target: number): { node: Node; offset: number } {
  let remaining = target;
  const walk = (n: Node): { node: Node; offset: number } | null => {
    for (let i = 0; i < n.childNodes.length; i++) {
      const c = n.childNodes[i]!;
      const len = nodeLength(c);
      if (c.nodeType === Node.TEXT_NODE) {
        if (remaining <= len) return { node: c, offset: remaining };
        remaining -= len;
        continue;
      }
      if (c instanceof HTMLElement && (c.dataset["mention"] || c.tagName === "BR")) {
        if (remaining < len || (remaining === len && c.tagName !== "BR")) {
          if (remaining === 0) return { node: n, offset: i };
          remaining -= len;
          if (remaining <= 0) return { node: n, offset: i + 1 };
          continue;
        }
        remaining -= len;
        if (remaining === 0) return { node: n, offset: i + 1 };
        continue;
      }
      if (remaining <= len) {
        const inner = walk(c);
        if (inner) return inner;
      }
      remaining -= len;
    }
    return null;
  };
  return walk(el) ?? { node: el, offset: el.childNodes.length };
}

export function setCaret(el: HTMLElement, start: number, end: number = start): void {
  const sel = window.getSelection();
  if (!sel) return;
  const a = positionAt(el, Math.max(0, start));
  const b = end === start ? a : positionAt(el, Math.max(0, end));
  const r = document.createRange();
  try {
    r.setStart(a.node, a.offset);
    r.setEnd(b.node, b.offset);
  } catch {
    r.selectNodeContents(el);
    r.collapse(false);
  }
  sel.removeAllRanges();
  sel.addRange(r);
}

/** Where the caret is on screen (for menus). */
export function caretRect(el: HTMLElement): DOMRect {
  const sel = window.getSelection();
  if (sel && sel.rangeCount > 0) {
    const r = sel.getRangeAt(0).cloneRange();
    const rects = r.getClientRects();
    const rect = rects[rects.length - 1] ?? r.getBoundingClientRect();
    if (rect && (rect.width || rect.height)) return rect;
  }
  return el.getBoundingClientRect();
}

function lineHeight(el: HTMLElement): number {
  const lh = parseFloat(getComputedStyle(el).lineHeight);
  return Number.isFinite(lh) ? lh : 24;
}

/** Whether the caret sits on el's first visual line. */
export function caretOnFirstLine(el: HTMLElement): boolean {
  const caret = getCaret(el);
  if (!caret || caret.start === 0) return true;
  const r = caretRect(el);
  return r.top - el.getBoundingClientRect().top < lineHeight(el) * 0.8;
}

export function caretOnLastLine(el: HTMLElement): boolean {
  const caret = getCaret(el);
  if (!caret) return true;
  if (caret.end >= nodeLength(el)) return true;
  const r = caretRect(el);
  return el.getBoundingClientRect().bottom - r.bottom < lineHeight(el) * 0.8;
}

/** Puts the caret in el at screen x on its first (or last) line, else at the start (end). */
export function placeCaretAtX(el: HTMLElement, x: number, edge: "first" | "last"): void {
  const rect = el.getBoundingClientRect();
  const y = edge === "first" ? rect.top + Math.min(8, rect.height / 2) : rect.bottom - Math.min(8, rect.height / 2);
  const doc = document as Document & { caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null };
  let pos: { node: Node; offset: number } | null = null;
  if (doc.caretPositionFromPoint) {
    const p = doc.caretPositionFromPoint(x, y);
    if (p) pos = { node: p.offsetNode, offset: p.offset };
  } else if (document.caretRangeFromPoint) {
    const r = document.caretRangeFromPoint(x, y);
    if (r) pos = { node: r.startContainer, offset: r.startOffset };
  }
  if (pos && el.contains(pos.node)) {
    const off = offsetOf(el, pos.node, pos.offset);
    setCaret(el, off);
    return;
  }
  const len = nodeLength(el);
  setCaret(el, edge === "first" ? 0 : len);
}

export function textLength(el: HTMLElement): number {
  return nodeLength(el);
}
