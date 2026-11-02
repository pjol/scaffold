/** Labels, slash-menu catalogue, Markdown shortcuts and database value formatting. Portable. */
import { docsPlain } from "./rich";
import type { DocsBlock, DocsBlockProps, DocsBlockType, DocsFilter, DocsOption, DocsProperty, DocsPropertyType, DocsRefs, DocsRow, DocsTextBlockType, DocsValue } from "./types";

export const DOCS_TEXT_TYPES: readonly DocsTextBlockType[] = [
  "paragraph",
  "heading_1",
  "heading_2",
  "heading_3",
  "bulleted_list",
  "numbered_list",
  "to_do",
  "toggle",
  "quote",
  "callout",
];

export function docsIsTextBlock(type: DocsBlockType): type is DocsTextBlockType {
  return (DOCS_TEXT_TYPES as readonly string[]).includes(type);
}

/** "Untitled" for an empty title. */
export function docsTitle(title: string | null | undefined): string {
  return title && title.trim() ? title : "Untitled";
}

export const DOCS_BLOCK_LABEL: Record<DocsBlockType, string> = {
  paragraph: "Text",
  heading_1: "Heading 1",
  heading_2: "Heading 2",
  heading_3: "Heading 3",
  bulleted_list: "Bulleted list",
  numbered_list: "Numbered list",
  to_do: "To-do list",
  toggle: "Toggle list",
  quote: "Quote",
  callout: "Callout",
  code: "Code",
  divider: "Divider",
  image: "Image",
  file: "File",
  bookmark: "Bookmark",
  page: "Page",
  table: "Table",
  database: "Database",
  connected_page: "Connected page",
};

/** What the slash menu offers. `action` items need more than a block (an upload, a new page). */
export interface DocsSlashItem {
  id: string;
  label: string;
  group: "Basic blocks" | "Media" | "Pages" | "Advanced" | "Inline";
  keywords: string[];
  type?: DocsBlockType;
  props?: DocsBlockProps;
  action?: "image" | "file" | "bookmark" | "page" | "database" | "mention-page" | "mention-person" | "date";
  /** Shortcut hint shown on the right ("#"). */
  hint?: string;
}

export const DOCS_SLASH_ITEMS: readonly DocsSlashItem[] = [
  { id: "paragraph", label: "Text", group: "Basic blocks", keywords: ["text", "plain", "paragraph"], type: "paragraph" },
  { id: "heading_1", label: "Heading 1", group: "Basic blocks", keywords: ["h1", "title", "heading"], type: "heading_1", hint: "#" },
  { id: "heading_2", label: "Heading 2", group: "Basic blocks", keywords: ["h2", "subtitle", "heading"], type: "heading_2", hint: "##" },
  { id: "heading_3", label: "Heading 3", group: "Basic blocks", keywords: ["h3", "heading"], type: "heading_3", hint: "###" },
  { id: "bulleted_list", label: "Bulleted list", group: "Basic blocks", keywords: ["bullet", "ul", "list"], type: "bulleted_list", hint: "-" },
  { id: "numbered_list", label: "Numbered list", group: "Basic blocks", keywords: ["number", "ol", "ordered", "list"], type: "numbered_list", hint: "1." },
  { id: "to_do", label: "To-do list", group: "Basic blocks", keywords: ["todo", "task", "checkbox", "check"], type: "to_do", props: { checked: false }, hint: "[]" },
  { id: "toggle", label: "Toggle list", group: "Basic blocks", keywords: ["toggle", "collapse", "details", "expand"], type: "toggle" },
  { id: "quote", label: "Quote", group: "Basic blocks", keywords: ["quote", "blockquote", "citation"], type: "quote", hint: ">" },
  { id: "callout", label: "Callout", group: "Basic blocks", keywords: ["callout", "note", "tip", "warning", "info"], type: "callout", props: { icon: "💡" } },
  { id: "divider", label: "Divider", group: "Basic blocks", keywords: ["divider", "line", "separator", "hr", "rule"], type: "divider", hint: "---" },
  { id: "page", label: "Page", group: "Pages", keywords: ["page", "subpage", "new", "child"], action: "page" },
  { id: "mention-page", label: "Link to page", group: "Pages", keywords: ["link", "page", "mention", "reference"], action: "mention-page" },
  { id: "database", label: "Database", group: "Pages", keywords: ["database", "table", "board", "tracker", "inline"], action: "database" },
  { id: "image", label: "Image", group: "Media", keywords: ["image", "photo", "picture", "upload"], action: "image" },
  { id: "file", label: "File", group: "Media", keywords: ["file", "attachment", "pdf", "upload"], action: "file" },
  { id: "bookmark", label: "Bookmark", group: "Media", keywords: ["bookmark", "link", "url", "web"], action: "bookmark" },
  { id: "code", label: "Code", group: "Advanced", keywords: ["code", "snippet", "pre"], type: "code", props: { code: "", language: "plain" }, hint: "```" },
  {
    id: "table",
    label: "Table",
    group: "Advanced",
    keywords: ["table", "grid", "cells"],
    type: "table",
    props: {
      rows: [
        [[], [], []],
        [[], [], []],
      ],
      header_row: true,
    },
  },
  { id: "mention-person", label: "Mention a person", group: "Inline", keywords: ["mention", "person", "people", "user", "@"], action: "mention-person" },
  { id: "date", label: "Date", group: "Inline", keywords: ["date", "today", "tomorrow", "day"], action: "date" },
];

/** Slash-menu items matching a query (label first, then keywords). */
export function docsFilterSlash(query: string, items: readonly DocsSlashItem[] = DOCS_SLASH_ITEMS): DocsSlashItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...items];
  const starts = items.filter((i) => i.label.toLowerCase().startsWith(q));
  const rest = items.filter((i) => !starts.includes(i) && (i.label.toLowerCase().includes(q) || i.keywords.some((k) => k.startsWith(q))));
  return [...starts, ...rest];
}

/**
 * The block a Markdown shortcut typed at the start of a block turns it into: "#", "##",
 * "###", "-", "*", "+", "1.", "[]", "[x]", ">", "```", "---". null when it isn't one.
 */
export function docsShortcut(prefix: string): { type: DocsBlockType; props?: DocsBlockProps } | null {
  switch (prefix) {
    case "#":
      return { type: "heading_1" };
    case "##":
      return { type: "heading_2" };
    case "###":
      return { type: "heading_3" };
    case "-":
    case "*":
    case "+":
      return { type: "bulleted_list" };
    case "[]":
    case "[ ]":
      return { type: "to_do", props: { checked: false } };
    case "[x]":
      return { type: "to_do", props: { checked: true } };
    case ">":
      return { type: "quote" };
    case '"':
      return { type: "quote" };
    case "```":
      return { type: "code", props: { code: "", language: "plain" } };
    case "---":
      return { type: "divider" };
  }
  if (/^\d{1,3}[.)]$/.test(prefix)) return { type: "numbered_list" };
  return null;
}

/** Numbered-list numbers per block id (runs restart after any other block). */
export function docsListNumbers(siblings: readonly DocsBlock[]): Map<string, number> {
  const out = new Map<string, number>();
  let n = 0;
  for (const b of siblings) {
    n = b.type === "numbered_list" ? n + 1 : 0;
    if (n > 0) out.set(b.id, n);
  }
  return out;
}

export const DOCS_CODE_LANGUAGES: readonly string[] = [
  "plain",
  "bash",
  "c",
  "cpp",
  "csharp",
  "css",
  "go",
  "html",
  "java",
  "javascript",
  "json",
  "kotlin",
  "markdown",
  "php",
  "python",
  "ruby",
  "rust",
  "sql",
  "swift",
  "typescript",
  "yaml",
];

/** Text a block shows in lists and previews. */
export function docsBlockText(b: Pick<DocsBlock, "type" | "props">): string {
  if (b.type === "code") return b.props.code ?? "";
  if (b.type === "bookmark") return b.props.title || b.props.url || "";
  if (b.type === "image" || b.type === "file") return docsPlain(b.props.caption) || b.props.name || "";
  return docsPlain(b.props.text);
}

// ---- databases ---------------------------------------------------------------------

export const DOCS_PROPERTY_LABEL: Record<DocsPropertyType, string> = {
  title: "Title",
  text: "Text",
  number: "Number",
  select: "Select",
  multi_select: "Multi-select",
  status: "Status",
  date: "Date",
  person: "Person",
  checkbox: "Checkbox",
  url: "URL",
  email: "Email",
};

/** Types a person can add (title comes with every database). */
export const DOCS_ADDABLE_TYPES: readonly DocsPropertyType[] = ["text", "number", "select", "multi_select", "status", "date", "person", "checkbox", "url", "email"];

const OPS: Record<DocsPropertyType, string[]> = {
  title: ["contains", "not_contains", "is", "is_not", "starts_with", "is_empty", "is_not_empty"],
  text: ["contains", "not_contains", "is", "is_not", "starts_with", "is_empty", "is_not_empty"],
  url: ["contains", "not_contains", "is", "is_not", "is_empty", "is_not_empty"],
  email: ["contains", "not_contains", "is", "is_not", "is_empty", "is_not_empty"],
  number: ["eq", "neq", "gt", "gte", "lt", "lte", "is_empty", "is_not_empty"],
  select: ["is", "is_not", "is_empty", "is_not_empty"],
  status: ["is", "is_not", "is_empty", "is_not_empty"],
  multi_select: ["contains", "not_contains", "is_empty", "is_not_empty"],
  person: ["contains", "not_contains", "is_empty", "is_not_empty"],
  date: ["is", "before", "after", "on_or_before", "on_or_after", "is_empty", "is_not_empty"],
  checkbox: ["is"],
};

export const DOCS_FILTER_OP_LABEL: Record<string, string> = {
  contains: "contains",
  not_contains: "doesn't contain",
  is: "is",
  is_not: "is not",
  starts_with: "starts with",
  is_empty: "is empty",
  is_not_empty: "is not empty",
  eq: "=",
  neq: "≠",
  gt: ">",
  gte: "≥",
  lt: "<",
  lte: "≤",
  before: "is before",
  after: "is after",
  on_or_before: "is on or before",
  on_or_after: "is on or after",
};

export function docsFilterOps(type: DocsPropertyType): string[] {
  return OPS[type];
}

/** Whether a filter op takes no value. */
export function docsOpIsUnary(op: string): boolean {
  return op === "is_empty" || op === "is_not_empty";
}

export function docsDefaultFilter(p: DocsProperty): DocsFilter {
  if (p.type === "checkbox") return { property: p.id, op: "is", value: true };
  return { property: p.id, op: OPS[p.type][0]! };
}

export function docsOption(p: DocsProperty | undefined, id: unknown): DocsOption | undefined {
  return typeof id === "string" ? p?.options?.find((o) => o.id === id) : undefined;
}

const CURRENCY: Record<string, string> = { dollar: "USD", euro: "EUR", pound: "GBP" };

export function docsFormatNumber(v: number, format: DocsProperty["number_format"] = "number"): string {
  if (format === "percent") return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(v)}%`;
  const currency = format ? CURRENCY[format] : undefined;
  if (currency) return new Intl.NumberFormat(undefined, { style: "currency", currency, maximumFractionDigits: 2 }).format(v);
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 6 }).format(v);
}

/** "Nov 20, 2026" (+ time when set). Dates are calendar days, never shifted by time zone. */
export function docsFormatDate(value: string | undefined | null): string {
  if (!value) return "";
  const [d, t] = value.split("T");
  const [y, m, day] = (d ?? "").split("-").map(Number);
  if (!y || !m || !day) return value;
  const date = new Date(Date.UTC(y, m - 1, day));
  const s = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" }).format(date);
  return t ? `${s} ${t}` : s;
}

/** A local calendar day as YYYY-MM-DD. */
export function docsToday(offsetDays = 0): string {
  const d = new Date();
  d.setDate(d.getDate() + offsetDays);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** A row value as text (option names, people's names, formatted numbers and dates). */
export function docsFormatValue(p: DocsProperty, value: DocsValue | undefined, refs?: DocsRefs, title?: string): string {
  if (p.type === "title") return docsTitle(title);
  if (value === null || value === undefined) return "";
  switch (p.type) {
    case "select":
    case "status":
      return docsOption(p, value)?.name ?? "";
    case "multi_select":
      return (Array.isArray(value) ? value : [])
        .map((id) => docsOption(p, id)?.name)
        .filter(Boolean)
        .join(", ");
    case "person":
      return (Array.isArray(value) ? value : [])
        .map((id) => refs?.users[id]?.name)
        .filter(Boolean)
        .join(", ");
    case "number":
      return typeof value === "number" ? docsFormatNumber(value, p.number_format) : "";
    case "checkbox":
      return value === true ? "Yes" : "No";
    case "date": {
      const v = value as { start?: string; end?: string };
      return v.end ? `${docsFormatDate(v.start)} → ${docsFormatDate(v.end)}` : docsFormatDate(v.start);
    }
    default:
      return typeof value === "string" ? value : "";
  }
}

/** Rows grouped for a board, in option order, then "No value". */
export function docsGroupRows(rows: readonly DocsRow[], p: DocsProperty | undefined): { option: DocsOption | null; rows: DocsRow[] }[] {
  if (!p) return [{ option: null, rows: [...rows] }];
  const groups = (p.options ?? []).map((o) => ({ option: o as DocsOption | null, rows: [] as DocsRow[] }));
  const none = { option: null as DocsOption | null, rows: [] as DocsRow[] };
  for (const r of rows) {
    const g = groups.find((x) => x.option && x.option.id === r.props[p.id]);
    (g ?? none).rows.push(r);
  }
  return [...groups, none];
}

export const DOCS_STATUS_GROUP_LABEL: Record<"todo" | "doing" | "done", string> = { todo: "To-do", doing: "In progress", done: "Complete" };

/** Short "2 min ago" style label for list rows. */
export function docsTimeAgo(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime();
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 60) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  if (d < 7) return `${d} d ago`;
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(new Date(t));
}
