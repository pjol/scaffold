/**
 * Rich text as an array of spans (`DocsSpan[]`): pure functions the web editor, the mobile
 * editor and the page session share. Offsets are string indices (UTF-16 code units) into
 * the plain text; a mention span is one unit of `text.length` characters that edits never
 * split.
 */
import type { DocsMark, DocsSpan } from "./types";

const MARKS: DocsMark[] = ["bold", "italic", "underline", "strike", "code"];

export function docsPlain(spans: readonly DocsSpan[] | null | undefined): string {
  if (!spans) return "";
  let s = "";
  for (const sp of spans) s += sp.text;
  return s;
}

/** Same formatting (mentions never merge). */
export function docsSameFormat(a: DocsSpan, b: DocsSpan): boolean {
  if (a.mention || b.mention) return false;
  return (
    !!a.bold === !!b.bold &&
    !!a.italic === !!b.italic &&
    !!a.underline === !!b.underline &&
    !!a.strike === !!b.strike &&
    !!a.code === !!b.code &&
    (a.link ?? "") === (b.link ?? "")
  );
}

/** A span with only the fields that are set (stable JSON, compact payloads). */
function clean(sp: DocsSpan): DocsSpan {
  const out: DocsSpan = { text: sp.text };
  if (sp.mention) {
    out.mention = sp.mention;
    if (sp.bold) out.bold = true;
    if (sp.italic) out.italic = true;
    if (sp.underline) out.underline = true;
    if (sp.strike) out.strike = true;
    return out;
  }
  for (const m of MARKS) if (sp[m]) out[m] = true;
  if (sp.link) out.link = sp.link;
  return out;
}

/** Drops empty runs and merges neighbours with the same formatting. */
export function docsNormalize(spans: readonly DocsSpan[]): DocsSpan[] {
  const out: DocsSpan[] = [];
  for (const raw of spans) {
    if (!raw.text) continue;
    const sp = clean(raw);
    const last = out[out.length - 1];
    if (last && docsSameFormat(last, sp)) out[out.length - 1] = { ...last, text: last.text + sp.text };
    else out.push(sp);
  }
  return out;
}

export function docsSpansEqual(a: readonly DocsSpan[] | undefined, b: readonly DocsSpan[] | undefined): boolean {
  const x = docsNormalize(a ?? []);
  const y = docsNormalize(b ?? []);
  if (x.length !== y.length) return false;
  for (let i = 0; i < x.length; i++) {
    const p = x[i]!;
    const q = y[i]!;
    if (p.text !== q.text || !docsSameFormat(p, q) && !(p.mention && q.mention)) return false;
    if (p.mention || q.mention) {
      if (!p.mention || !q.mention || JSON.stringify(p.mention) !== JSON.stringify(q.mention)) return false;
      if (!!p.bold !== !!q.bold || !!p.italic !== !!q.italic) return false;
    }
  }
  return true;
}

/** Spans covering [start, end). A mention partly inside the range is kept whole. */
export function docsSlice(spans: readonly DocsSpan[], start: number, end: number = Number.POSITIVE_INFINITY): DocsSpan[] {
  const out: DocsSpan[] = [];
  let at = 0;
  for (const sp of spans) {
    const s = at;
    const e = at + sp.text.length;
    at = e;
    if (e <= start || s >= end) continue;
    if (sp.mention) {
      if (s >= start && e <= end) out.push(sp);
      continue;
    }
    out.push({ ...sp, text: sp.text.slice(Math.max(0, start - s), Math.min(sp.text.length, end - s)) });
  }
  return docsNormalize(out);
}

export function docsConcat(...parts: readonly (readonly DocsSpan[])[]): DocsSpan[] {
  return docsNormalize(parts.flat());
}

/** [before, after] at `at`. */
export function docsSplit(spans: readonly DocsSpan[], at: number): [DocsSpan[], DocsSpan[]] {
  return [docsSlice(spans, 0, at), docsSlice(spans, at)];
}

/** The formatting typed text takes at `at`: the character before it (never a mention or a link end). */
export function docsFormatAt(spans: readonly DocsSpan[], at: number): Omit<DocsSpan, "text" | "mention"> {
  let pos = 0;
  let found: DocsSpan | undefined;
  for (const sp of spans) {
    const e = pos + sp.text.length;
    if (at > pos && at <= e) {
      found = sp;
      break;
    }
    pos = e;
  }
  if (!found && at === 0) found = spans[0];
  if (!found || found.mention) return {};
  const { text: _t, mention: _m, ...fmt } = found;
  void _t;
  void _m;
  return fmt;
}

export function docsInsertText(spans: readonly DocsSpan[], at: number, text: string, format?: Omit<DocsSpan, "text" | "mention">): DocsSpan[] {
  if (!text) return docsNormalize(spans);
  const fmt = format ?? docsFormatAt(spans, at);
  const [a, b] = docsSplit(spans, at);
  return docsConcat(a, [{ ...fmt, text }], b);
}

export function docsInsertSpans(spans: readonly DocsSpan[], at: number, insert: readonly DocsSpan[]): DocsSpan[] {
  const [a, b] = docsSplit(spans, at);
  return docsConcat(a, insert, b);
}

export function docsDeleteRange(spans: readonly DocsSpan[], start: number, end: number): DocsSpan[] {
  if (end <= start) return docsNormalize(spans);
  return docsConcat(docsSlice(spans, 0, start), docsSlice(spans, end));
}

/** Whether every character in [start, end) has `mark` (false for an empty range). */
export function docsHasMark(spans: readonly DocsSpan[], start: number, end: number, mark: DocsMark | "link"): boolean {
  if (end <= start) return false;
  let at = 0;
  let any = false;
  for (const sp of spans) {
    const s = at;
    const e = at + sp.text.length;
    at = e;
    if (e <= start || s >= end) continue;
    any = true;
    if (mark === "link" ? !sp.link : !sp[mark]) return false;
  }
  return any;
}

function mapRange(spans: readonly DocsSpan[], start: number, end: number, fn: (sp: DocsSpan) => DocsSpan): DocsSpan[] {
  const out: DocsSpan[] = [];
  let at = 0;
  for (const sp of spans) {
    const s = at;
    const e = at + sp.text.length;
    at = e;
    if (e <= start || s >= end || sp.mention) {
      out.push(sp.mention && s >= start && e <= end ? fn(sp) : sp);
      continue;
    }
    const a = Math.max(start, s) - s;
    const b = Math.min(end, e) - s;
    if (a > 0) out.push({ ...sp, text: sp.text.slice(0, a) });
    out.push(fn({ ...sp, text: sp.text.slice(a, b) }));
    if (b < sp.text.length) out.push({ ...sp, text: sp.text.slice(b) });
  }
  return docsNormalize(out);
}

/** Adds `mark` to [start, end), or removes it when the whole range already has it. */
export function docsToggleMark(spans: readonly DocsSpan[], start: number, end: number, mark: DocsMark): DocsSpan[] {
  const on = !docsHasMark(spans, start, end, mark);
  return mapRange(spans, start, end, (sp) => {
    if (sp.mention && mark === "code") return sp;
    const next = { ...sp };
    if (on) next[mark] = true;
    else delete next[mark];
    return next;
  });
}

/** Sets (or with null, removes) a link on [start, end). */
export function docsSetLink(spans: readonly DocsSpan[], start: number, end: number, href: string | null): DocsSpan[] {
  return mapRange(spans, start, end, (sp) => {
    if (sp.mention) return sp;
    const next = { ...sp };
    if (href) next.link = href;
    else delete next.link;
    return next;
  });
}

/** The link covering `at` with its extent, for "edit link". */
export function docsLinkAt(spans: readonly DocsSpan[], at: number): { href: string; start: number; end: number } | null {
  let pos = 0;
  const runs: { s: number; e: number; link?: string }[] = [];
  for (const sp of spans) {
    runs.push({ s: pos, e: pos + sp.text.length, ...(sp.link ? { link: sp.link } : {}) });
    pos += sp.text.length;
  }
  const i = runs.findIndex((r) => at >= r.s && at <= r.e && r.link);
  if (i < 0) return null;
  const href = runs[i]!.link!;
  let a = i;
  let b = i;
  while (a > 0 && runs[a - 1]!.link === href) a--;
  while (b < runs.length - 1 && runs[b + 1]!.link === href) b++;
  return { href, start: runs[a]!.s, end: runs[b]!.e };
}

/** Accepts what a person types as a link: adds https:// to bare domains, refuses other schemes. */
export function docsSafeLink(raw: string): string | null {
  let v = raw.trim();
  if (!v) return null;
  if (/^[\w.+-]+@[\w-]+\.[\w.-]+$/.test(v)) v = `mailto:${v}`;
  else if (!/^[a-z][a-z0-9+.-]*:/i.test(v)) v = `https://${v}`;
  if (!/^(https?:\/\/[^\s/?#]+|mailto:\S+)/i.test(v) || /[\s<>"]/.test(v)) return null;
  return v;
}

// ---- diff and merge ------------------------------------------------------------

interface Region {
  start: number; // in base
  end: number; // in base
  insert: DocsSpan[];
  insertLen: number;
}

function region(base: string, next: string, nextSpans: readonly DocsSpan[]): Region {
  let p = 0;
  const max = Math.min(base.length, next.length);
  while (p < max && base[p] === next[p]) p++;
  let s = 0;
  while (s < max - p && base[base.length - 1 - s] === next[next.length - 1 - s]) s++;
  return { start: p, end: base.length - s, insert: docsSlice(nextSpans, p, next.length - s), insertLen: next.length - s - p };
}

/**
 * Spans with their plain text changed to `next`, keeping the formatting of what didn't
 * change (the typed text takes the formatting before it). For plain-text editors (mobile).
 */
export function docsReplaceText(spans: readonly DocsSpan[], next: string): DocsSpan[] {
  const prev = docsPlain(spans);
  if (prev === next) return docsNormalize(spans);
  let p = 0;
  const max = Math.min(prev.length, next.length);
  while (p < max && prev[p] === next[p]) p++;
  let s = 0;
  while (s < max - p && prev[prev.length - 1 - s] === next[next.length - 1 - s]) s++;
  const removed = docsDeleteRange(spans, p, prev.length - s);
  const inserted = next.slice(p, next.length - s);
  const fmt = docsFormatAt(spans, p);
  const plainFmt = { ...fmt };
  delete plainFmt.link;
  return docsInsertText(removed, p, inserted, inserted.includes("\n") ? plainFmt : fmt);
}

/**
 * Three-way merge of a block's text: `local` and `remote` both started from `base`. When
 * the two changes touch different places, both apply (with local's formatting); when they
 * overlap, local wins. The server runs the same merge on every text update.
 */
export function docsMergeText(base: readonly DocsSpan[], local: readonly DocsSpan[], remote: readonly DocsSpan[]): DocsSpan[] {
  if (docsSpansEqual(local, base)) return docsNormalize(remote);
  if (docsSpansEqual(remote, base) || docsSpansEqual(remote, local)) return docsNormalize(local);
  const bp = docsPlain(base);
  const lp = docsPlain(local);
  const rp = docsPlain(remote);
  const l = region(bp, lp, local);
  const r = region(bp, rp, remote);
  if (l.start < r.end && r.start < l.end) return docsNormalize(local);
  if (r.start === r.end && r.insertLen === 0) return docsNormalize(local);
  // Map the remote change into local's coordinates and splice it in.
  const shift = l.insertLen - (l.end - l.start);
  const rs = r.start >= l.end ? r.start + shift : r.start;
  const re = r.end >= l.end && r.start >= l.end ? r.end + shift : r.end;
  return docsConcat(docsSlice(local, 0, rs), r.insert, docsSlice(local, re));
}

/** Code-point length of the first `index` UTF-16 units (the server counts runes). */
export function docsCodePoints(text: string, index: number = text.length): number {
  return Array.from(text.slice(0, index)).length;
}
