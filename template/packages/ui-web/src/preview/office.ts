/**
 * Office documents read in the browser: OOXML (docx, xlsx, pptx) and OpenDocument (odt, ods,
 * odp) are zips of XML. fflate (MIT) unzips the parts we need and DOMParser reads them into a
 * small model the preview draws with components, so nothing from the file is ever injected as
 * HTML. Images inside come out as blob URLs of raster types only (never SVG).
 */
import { unzipSync } from "fflate";
import type { FilePreviewOfficeFormat } from "@scaffoldapp/shared/preview";

export interface Run {
  text: string;
  b?: boolean;
  i?: boolean;
  u?: boolean;
  s?: boolean;
  href?: string;
}

export type DocBlock =
  | { type: "heading"; level: number; runs: Run[] }
  | { type: "paragraph"; runs: Run[] }
  | { type: "list"; ordered: boolean; items: Run[][] }
  | { type: "table"; rows: Run[][][] }
  | { type: "image"; src: string; alt: string };

export interface Sheet {
  name: string;
  rows: string[][];
  truncated: boolean;
}

export interface Slide {
  title: string;
  paragraphs: { text: string; level: number }[];
  images: string[];
}

export type OfficeDoc =
  | { kind: "doc"; blocks: DocBlock[]; truncated: boolean }
  | { kind: "sheet"; sheets: Sheet[] }
  | { kind: "slides"; slides: Slide[] };

const MAX_BLOCKS = 5000;
const MAX_ROWS = 5000;
const MAX_COLS = 200;
const MAX_SLIDES = 300;
const MAX_IMAGE_BYTES = 8 * 1024 * 1024;

const RASTER: Record<string, string> = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", bmp: "image/bmp" };

type Parts = Record<string, Uint8Array>;

const decoder = new TextDecoder();

function xml(parts: Parts, path: string): Document | null {
  const bytes = parts[path];
  if (!bytes) return null;
  const doc = new DOMParser().parseFromString(decoder.decode(bytes), "application/xml");
  return doc.getElementsByTagName("parsererror").length ? null : doc;
}

/** Children (any depth) with a local name, in document order. */
function all(el: Element | Document, local: string): Element[] {
  return Array.from(el.getElementsByTagNameNS("*", local));
}

/** Direct children with a local name. */
function kids(el: Element, local?: string): Element[] {
  return Array.from(el.children).filter((c) => !local || c.localName === local);
}

/** True when an ancestor has this local name. */
function inside(el: Element, local: string): boolean {
  for (let p = el.parentElement; p; p = p.parentElement) if (p.localName === local) return true;
  return false;
}

/** An `r:id` / `r:embed` style attribute (relationship namespace), never a plain `id`. */
function rel(el: Element | null | undefined, local = "id"): string | null {
  if (!el) return null;
  for (const a of Array.from(el.attributes)) if (a.localName === local && (a.namespaceURI ?? "").includes("relationships")) return a.value;
  return null;
}

function attr(el: Element | null | undefined, local: string): string | null {
  if (!el) return null;
  for (const a of Array.from(el.attributes)) if (a.localName === local) return a.value;
  return null;
}

function safeHref(h: string | null): string | undefined {
  if (!h) return undefined;
  return /^(https?:|mailto:)/i.test(h.trim()) ? h.trim() : undefined;
}

/** Resolves "../media/a.png" against "ppt/slides/slide1.xml". */
function resolve(from: string, target: string): string {
  if (target.startsWith("/")) return target.slice(1);
  const base = from.split("/").slice(0, -1);
  for (const seg of target.split("/")) {
    if (seg === "..") base.pop();
    else if (seg !== ".") base.push(seg);
  }
  return base.join("/");
}

/** Relationship id → { target, external } for a part. */
function rels(parts: Parts, part: string): Map<string, { target: string; external: boolean }> {
  const dir = part.split("/").slice(0, -1).join("/");
  const file = part.split("/").pop();
  const doc = xml(parts, `${dir ? dir + "/" : ""}_rels/${file}.rels`);
  const map = new Map<string, { target: string; external: boolean }>();
  if (!doc) return map;
  for (const r of all(doc, "Relationship")) {
    const id = r.getAttribute("Id");
    const target = r.getAttribute("Target");
    if (id && target) map.set(id, { target, external: r.getAttribute("TargetMode") === "External" });
  }
  return map;
}

class Images {
  urls: string[] = [];
  private parts: Parts;
  constructor(parts: Parts) {
    this.parts = parts;
  }
  url(path: string): string | null {
    const ext = path.split(".").pop()?.toLowerCase() ?? "";
    const type = RASTER[ext];
    const bytes = this.parts[path];
    if (!type || !bytes || bytes.length > MAX_IMAGE_BYTES) return null;
    const u = URL.createObjectURL(new Blob([bytes.slice()], { type }));
    this.urls.push(u);
    return u;
  }
}

function wanted(format: FilePreviewOfficeFormat) {
  return (name: string, size: number) => {
    if (size > 64 * 1024 * 1024) return false;
    if (name.endsWith(".xml") || name.endsWith(".rels")) return true;
    if (/\.(png|jpe?g|gif|webp|bmp)$/i.test(name)) return format === "docx" || format === "pptx" || format === "odt" || format === "odp";
    return false;
  };
}

/** Parses an office file. Throws on a corrupt or unreadable file. `images` holds blob URLs to revoke. */
export function parseOffice(bytes: Uint8Array, format: FilePreviewOfficeFormat): { doc: OfficeDoc; images: string[] } {
  const want = wanted(format);
  const parts = unzipSync(bytes, { filter: (f) => want(f.name, f.originalSize) });
  const images = new Images(parts);
  let doc: OfficeDoc;
  switch (format) {
    case "docx":
      doc = docx(parts, images);
      break;
    case "xlsx":
      doc = xlsx(parts);
      break;
    case "pptx":
      doc = pptx(parts, images);
      break;
    case "odt":
      doc = odt(parts, images);
      break;
    case "ods":
      doc = ods(parts);
      break;
    case "odp":
      doc = odp(parts, images);
      break;
  }
  return { doc, images: images.urls };
}

// ---------- docx ----------

function docxRuns(p: Element, links: Map<string, { target: string; external: boolean }>): Run[] {
  const out: Run[] = [];
  const walk = (el: Element, href?: string) => {
    for (const c of kids(el)) {
      if (c.localName === "hyperlink") {
        const id = rel(c);
        const r = id ? links.get(id) : undefined;
        walk(c, r?.external ? safeHref(r.target) : undefined);
      } else if (c.localName === "r") {
        const pr = kids(c, "rPr")[0];
        const on = (name: string) => {
          const e = pr ? kids(pr, name)[0] : undefined;
          return !!e && attr(e, "val") !== "0" && attr(e, "val") !== "false" && attr(e, "val") !== "none";
        };
        let text = "";
        for (const t of kids(c)) {
          if (t.localName === "t") text += t.textContent ?? "";
          else if (t.localName === "tab") text += "\t";
          else if (t.localName === "br" || t.localName === "cr") text += "\n";
        }
        if (text) out.push({ text, b: on("b"), i: on("i"), u: on("u"), s: on("strike"), href });
      } else if (c.localName === "ins" || c.localName === "smartTag" || c.localName === "sdt" || c.localName === "sdtContent" || c.localName === "fldSimple") {
        walk(c, href);
      }
    }
  };
  walk(p);
  return out;
}

/** A short paragraph set all in bold at 14 pt or more reads as a heading (documents formatted by hand, or exported by tools without styles). */
function visualHeading(p: Element, runs: Run[]): [string, string] | null {
  const text = runs.map((r) => r.text).join("").trim();
  if (!text || text.length > 120 || !runs.every((r) => r.b || !r.text.trim())) return null;
  const sizes = all(p, "sz").map((e) => Number(attr(e, "val") ?? "0"));
  const size = sizes.length ? Math.min(...sizes) : 0;
  return size >= 40 ? ["", "1"] : size >= 32 ? ["", "2"] : size >= 28 ? ["", "3"] : null;
}

function docxNumbering(parts: Parts): (numId: string, ilvl: string) => boolean {
  const doc = xml(parts, "word/numbering.xml");
  const abstract = new Map<string, Map<string, string>>();
  const nums = new Map<string, string>();
  if (doc) {
    for (const a of all(doc, "abstractNum")) {
      const lv = new Map<string, string>();
      for (const l of kids(a, "lvl")) lv.set(attr(l, "ilvl") ?? "0", attr(kids(l, "numFmt")[0], "val") ?? "bullet");
      abstract.set(attr(a, "abstractNumId") ?? "", lv);
    }
    for (const n of all(doc, "num")) nums.set(attr(n, "numId") ?? "", attr(kids(n, "abstractNumId")[0], "val") ?? "");
  }
  return (numId, ilvl) => {
    const fmt = abstract.get(nums.get(numId) ?? "")?.get(ilvl) ?? "bullet";
    return fmt !== "bullet" && fmt !== "none";
  };
}

function docx(parts: Parts, images: Images): OfficeDoc {
  const doc = xml(parts, "word/document.xml");
  if (!doc) throw new Error("not a Word document");
  const links = rels(parts, "word/document.xml");
  const ordered = docxNumbering(parts);
  const body = all(doc, "body")[0];
  const blocks: DocBlock[] = [];
  let truncated = false;
  const push = (b: DocBlock) => {
    const last = blocks[blocks.length - 1];
    if (b.type === "list" && last?.type === "list" && last.ordered === b.ordered) last.items.push(...b.items);
    else blocks.push(b);
  };
  const paragraph = (p: Element) => {
    const pr = kids(p, "pPr")[0];
    const style = attr(pr ? kids(pr, "pStyle")[0] : undefined, "val") ?? "";
    const num = pr ? kids(pr, "numPr")[0] : undefined;
    for (const blip of all(p, "blip")) {
      const id = rel(blip, "embed");
      const r = id ? links.get(id) : undefined;
      const src = r && !r.external ? images.url(resolve("word/document.xml", r.target)) : null;
      if (src) push({ type: "image", src, alt: attr(all(p, "docPr")[0], "descr") ?? "" });
    }
    const runs = docxRuns(p, links);
    if (runs.length === 0) return;
    const heading = /^(?:heading|Heading)\s?(\d)$/.exec(style) ?? (style === "Title" ? ["", "1"] : style === "Subtitle" ? ["", "2"] : visualHeading(p, runs));
    if (heading) push({ type: "heading", level: Math.min(6, Number(heading[1]) || 1), runs });
    else if (num) push({ type: "list", ordered: ordered(attr(kids(num, "numId")[0], "val") ?? "", attr(kids(num, "ilvl")[0], "val") ?? "0"), items: [runs] });
    else push({ type: "paragraph", runs });
  };
  const table = (t: Element): DocBlock => ({
    type: "table",
    rows: kids(t, "tr").map((tr) => kids(tr, "tc").map((tc) => kids(tc, "p").flatMap((p, i) => (i > 0 ? [{ text: "\n" }, ...docxRuns(p, links)] : docxRuns(p, links))))),
  });
  for (const el of body ? kids(body) : []) {
    if (blocks.length >= MAX_BLOCKS) {
      truncated = true;
      break;
    }
    if (el.localName === "p") paragraph(el);
    else if (el.localName === "tbl") push(table(el));
    else if (el.localName === "sdt") for (const p of all(el, "p")) paragraph(p);
  }
  return { kind: "doc", blocks, truncated };
}

// ---------- xlsx ----------

const DATE_FORMATS = new Set([14, 15, 16, 17, 18, 19, 20, 21, 22, 45, 46, 47]);

function colIndex(ref: string): number {
  let n = 0;
  for (const ch of ref.replace(/\d+$/, "")) n = n * 26 + (ch.charCodeAt(0) - 64);
  return n - 1;
}

/** Excel serial date → "2024-03-01" (or with time). */
function excelDate(serial: number, withTime: boolean): string {
  const ms = Math.round((serial - 25569) * 86400 * 1000);
  const d = new Date(ms);
  if (Number.isNaN(d.getTime())) return String(serial);
  const iso = d.toISOString();
  return withTime && serial % 1 !== 0 ? `${iso.slice(0, 10)} ${iso.slice(11, 16)}` : iso.slice(0, 10);
}

function xlsx(parts: Parts): OfficeDoc {
  const wb = xml(parts, "xl/workbook.xml");
  if (!wb) throw new Error("not a spreadsheet");
  const wbRels = rels(parts, "xl/workbook.xml");
  const strings: string[] = [];
  const sst = xml(parts, "xl/sharedStrings.xml");
  if (sst) for (const si of all(sst, "si")) strings.push(all(si, "t").map((t) => t.textContent ?? "").join(""));
  // Which style indexes are dates.
  const dateStyles = new Set<number>();
  const styles = xml(parts, "xl/styles.xml");
  if (styles) {
    const custom = new Map<number, string>();
    for (const f of all(styles, "numFmt")) custom.set(Number(attr(f, "numFmtId")), attr(f, "formatCode") ?? "");
    const xfs = all(styles, "cellXfs")[0];
    kids(xfs ?? styles.documentElement, "xf").forEach((xf, i) => {
      const id = Number(attr(xf, "numFmtId") ?? "0");
      const code = (custom.get(id) ?? "").replace(/"[^"]*"|\[[^\]]*\]/g, "");
      if (DATE_FORMATS.has(id) || (/[dmy]/i.test(code) && !/^[#0.,%]+$/.test(code))) dateStyles.add(i);
    });
  }
  const sheets: Sheet[] = [];
  for (const s of all(wb, "sheet")) {
    const rid = rel(s);
    const target = rid ? wbRels.get(rid)?.target : undefined;
    if (!target) continue;
    const path = resolve("xl/workbook.xml", target);
    const doc = xml(parts, path);
    if (!doc) continue;
    const rows: string[][] = [];
    let truncated = false;
    for (const row of all(doc, "row")) {
      const r = Number(attr(row, "r") ?? rows.length + 1) - 1;
      if (r >= MAX_ROWS) {
        truncated = true;
        break;
      }
      while (rows.length <= r) rows.push([]);
      const out = rows[r]!;
      kids(row, "c").forEach((c, i) => {
        const ref = attr(c, "r");
        const col = ref ? colIndex(ref) : i;
        if (col >= MAX_COLS) {
          truncated = true;
          return;
        }
        const t = attr(c, "t");
        const v = kids(c, "v")[0]?.textContent ?? "";
        let val: string;
        if (t === "s") val = strings[Number(v)] ?? "";
        else if (t === "inlineStr") val = all(c, "t").map((x) => x.textContent ?? "").join("");
        else if (t === "b") val = v === "1" ? "TRUE" : "FALSE";
        else if (t === "str" || t === "e") val = v;
        else if (v !== "" && dateStyles.has(Number(attr(c, "s") ?? "-1"))) val = excelDate(Number(v), true);
        else val = v;
        while (out.length < col) out.push("");
        out[col] = val;
      });
    }
    while (rows.length && rows[rows.length - 1]!.every((x) => x === "")) rows.pop();
    sheets.push({ name: attr(s, "name") ?? `Sheet ${sheets.length + 1}`, rows, truncated });
  }
  return { kind: "sheet", sheets };
}

// ---------- pptx ----------

function pptx(parts: Parts, images: Images): OfficeDoc {
  const pres = xml(parts, "ppt/presentation.xml");
  if (!pres) throw new Error("not a presentation");
  const presRels = rels(parts, "ppt/presentation.xml");
  const slides: Slide[] = [];
  for (const id of all(pres, "sldId").slice(0, MAX_SLIDES)) {
    const rid = rel(id);
    const target = rid ? presRels.get(rid)?.target : undefined;
    if (!target) continue;
    const path = resolve("ppt/presentation.xml", target);
    const doc = xml(parts, path);
    if (!doc) continue;
    const slideRels = rels(parts, path);
    let title = "";
    const paragraphs: Slide["paragraphs"] = [];
    for (const sp of all(doc, "sp")) {
      const ph = all(sp, "ph")[0];
      const type = attr(ph, "type");
      const texts = all(sp, "p").map((p) => ({
        text: kids(p)
          .map((r) => (r.localName === "br" ? "\n" : all(r, "t").map((t) => t.textContent ?? "").join("")))
          .join(""),
        level: Number(attr(kids(p, "pPr")[0], "lvl") ?? "0"),
      }));
      const nonEmpty = texts.filter((t) => t.text.trim());
      if ((type === "title" || type === "ctrTitle") && !title) title = nonEmpty.map((t) => t.text).join(" ");
      else paragraphs.push(...nonEmpty);
    }
    const imgs: string[] = [];
    for (const blip of all(doc, "blip")) {
      const r = slideRels.get(rel(blip, "embed") ?? "");
      const src = r && !r.external ? images.url(resolve(path, r.target)) : null;
      if (src && imgs.length < 6) imgs.push(src);
    }
    slides.push({ title, paragraphs, images: imgs });
  }
  return { kind: "slides", slides };
}

// ---------- OpenDocument ----------

function odfText(el: Element): Run[] {
  const out: Run[] = [];
  const walk = (n: Node, style: Omit<Run, "text">) => {
    n.childNodes.forEach((c) => {
      if (c.nodeType === 3) {
        if (c.textContent) out.push({ text: c.textContent, ...style });
      } else if (c.nodeType === 1) {
        const e = c as Element;
        if (e.localName === "s") out.push({ text: " ".repeat(Number(attr(e, "c") ?? "1")), ...style });
        else if (e.localName === "tab") out.push({ text: "\t", ...style });
        else if (e.localName === "line-break") out.push({ text: "\n", ...style });
        else if (e.localName === "a") walk(e, { ...style, href: safeHref(attr(e, "href")) });
        else if (e.localName !== "note" && e.localName !== "annotation") walk(e, style);
      }
    });
  };
  walk(el, {});
  return out;
}

function odfBlocks(container: Element, images: Images, blocks: DocBlock[]) {
  for (const el of kids(container)) {
    if (blocks.length >= MAX_BLOCKS) return;
    if (el.localName === "h") blocks.push({ type: "heading", level: Math.min(6, Number(attr(el, "outline-level") ?? "1")), runs: odfText(el) });
    else if (el.localName === "p") {
      for (const img of all(el, "image")) {
        const src = images.url(attr(img, "href") ?? "");
        if (src) blocks.push({ type: "image", src, alt: "" });
      }
      const runs = odfText(el);
      if (runs.some((r) => r.text.trim())) blocks.push({ type: "paragraph", runs });
    } else if (el.localName === "list") {
      const items = all(el, "list-item").map((li) => kids(li, "p").flatMap((p) => odfText(p)));
      blocks.push({ type: "list", ordered: false, items: items.filter((i) => i.length) });
    } else if (el.localName === "table") {
      blocks.push({
        type: "table",
        rows: all(el, "table-row")
          .slice(0, MAX_ROWS)
          .map((tr) => kids(tr, "table-cell").map((tc) => kids(tc, "p").flatMap((p) => odfText(p)))),
      });
    } else if (el.localName === "section") odfBlocks(el, images, blocks);
  }
}

function odfBody(parts: Parts, local: string): Element {
  const doc = xml(parts, "content.xml");
  const body = doc ? all(doc, local)[0] : undefined;
  if (!body) throw new Error("not an OpenDocument file");
  return body;
}

function odt(parts: Parts, images: Images): OfficeDoc {
  const blocks: DocBlock[] = [];
  odfBlocks(odfBody(parts, "text"), images, blocks);
  return { kind: "doc", blocks, truncated: blocks.length >= MAX_BLOCKS };
}

function ods(parts: Parts): OfficeDoc {
  const body = odfBody(parts, "spreadsheet");
  const sheets: Sheet[] = [];
  for (const t of kids(body, "table")) {
    const rows: string[][] = [];
    let truncated = false;
    for (const tr of all(t, "table-row")) {
      const repeat = Math.min(Number(attr(tr, "number-rows-repeated") ?? "1"), MAX_ROWS);
      const row: string[] = [];
      for (const tc of kids(tr).filter((c) => c.localName === "table-cell" || c.localName === "covered-table-cell")) {
        const n = Math.min(Number(attr(tc, "number-columns-repeated") ?? "1"), MAX_COLS);
        const val = kids(tc, "p")
          .map((p) => odfText(p).map((r) => r.text).join(""))
          .join("\n");
        for (let i = 0; i < n && row.length < MAX_COLS; i++) row.push(val);
      }
      while (row.length && row[row.length - 1] === "") row.pop();
      for (let i = 0; i < repeat; i++) {
        if (rows.length >= MAX_ROWS) {
          truncated = true;
          break;
        }
        rows.push(row);
      }
      if (truncated) break;
    }
    while (rows.length && rows[rows.length - 1]!.length === 0) rows.pop();
    sheets.push({ name: attr(t, "name") ?? `Sheet ${sheets.length + 1}`, rows, truncated });
  }
  return { kind: "sheet", sheets };
}

function odp(parts: Parts, images: Images): OfficeDoc {
  const body = odfBody(parts, "presentation");
  const slides: Slide[] = [];
  for (const page of kids(body, "page").slice(0, MAX_SLIDES)) {
    let title = "";
    const paragraphs: Slide["paragraphs"] = [];
    const imgs: string[] = [];
    for (const frame of all(page, "frame").filter((f) => !inside(f, "notes"))) {
      const cls = attr(frame, "class");
      const texts = all(frame, "p")
        .map((p) => odfText(p).map((r) => r.text).join(""))
        .filter((t) => t.trim());
      if (cls === "title" && !title) title = texts.join(" ");
      else if (cls !== "notes") paragraphs.push(...texts.map((text) => ({ text, level: 0 })));
      for (const img of kids(frame, "image")) {
        const src = images.url(attr(img, "href") ?? "");
        if (src && imgs.length < 6) imgs.push(src);
      }
    }
    slides.push({ title: title || attr(page, "name") || "", paragraphs, images: imgs });
  }
  return { kind: "slides", slides };
}
