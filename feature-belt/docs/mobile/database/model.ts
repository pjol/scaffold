import {
  docsFormatComputed,
  docsValueType,
  type DocsColumn,
  type DocsComputedValue,
  type DocsGroupInfo,
  type DocsProperty,
  type DocsRefs,
  type DocsRow,
  type DocsRowPage,
  type DocsRollupFunction,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";
import { CalendarDays, ChartColumn, ChartGantt, ClipboardList, Columns3, LayoutGrid, LayoutList, Newspaper, Table2, type LucideIcon } from "lucide-react-native";

export const VIEW_ICON: Record<DocsViewType, LucideIcon> = {
  table: Table2,
  board: Columns3,
  list: LayoutList,
  calendar: CalendarDays,
  gallery: LayoutGrid,
  timeline: ChartGantt,
  chart: ChartColumn,
  form: ClipboardList,
  feed: Newspaper,
};

/** The first page's extras (groups, footer calculations, total) when the rows hook carries them. */
export interface RowsMeta {
  total?: number | undefined;
  groups?: DocsGroupInfo[] | undefined;
  sub_groups?: DocsGroupInfo[] | undefined;
  calculations?: Record<string, DocsComputedValue> | undefined;
}

export function rowsMeta(data: unknown): RowsMeta {
  if (!data || typeof data !== "object") return {};
  const d = data as Partial<DocsRowPage> & { pages?: DocsRowPage[] };
  const first = d.pages?.[0] ?? d;
  return { total: first.total, groups: first.groups, sub_groups: first.sub_groups, calculations: first.calculations };
}

/** Properties in a view's order (title first), hidden ones flagged. */
export function orderedColumns(schema: DocsProperty[], config: DocsViewConfig): DocsColumn[] {
  const cols: DocsColumn[] = [];
  for (const c of config.columns) if (schema.some((p) => p.id === c.property) && !cols.some((x) => x.property === c.property)) cols.push(c);
  for (const p of schema) if (!cols.some((c) => c.property === p.id)) cols.push({ property: p.id });
  const t = cols.findIndex((c) => c.property === "title");
  if (t > 0) cols.unshift(...cols.splice(t, 1));
  return cols;
}

export function visibleColumns(schema: DocsProperty[], config: DocsViewConfig): DocsProperty[] {
  return orderedColumns(schema, config)
    .filter((c) => !c.hidden || c.property === "title")
    .map((c) => schema.find((p) => p.id === c.property))
    .filter((p): p is DocsProperty => !!p);
}

/** Card, list and feed views show `card_properties` (title is always shown). */
export function cardProps(schema: DocsProperty[], config: DocsViewConfig, except?: string): DocsProperty[] {
  return config.card_properties.map((id) => schema.find((p) => p.id === id)).filter((p): p is DocsProperty => !!p && p.type !== "title" && p.id !== except);
}

/** Views that list properties as columns (table, timeline's table) vs cards. */
export function usesColumns(type: DocsViewType): boolean {
  return type === "table";
}

/** Properties a view can group by (mirrors Go groupable). */
export function groupable(p: DocsProperty): boolean {
  switch (p.type) {
    case "select":
    case "status":
    case "multi_select":
    case "person":
    case "created_by":
    case "last_edited_by":
    case "checkbox":
    case "date":
    case "created_time":
    case "last_edited_time":
    case "text":
    case "url":
    case "email":
    case "phone":
    case "number":
    case "relation":
    case "title":
      return true;
    case "formula":
    case "rollup": {
      const t = docsValueType(p);
      return t === "text" || t === "number" || t === "boolean" || t === "date";
    }
  }
  return false;
}

export function isDateish(p: DocsProperty): boolean {
  return p.type === "date" || p.type === "created_time" || p.type === "last_edited_time" || ((p.type === "formula" || p.type === "rollup") && docsValueType(p) === "date");
}

export function isNumeric(p: DocsProperty): boolean {
  return p.type === "number" || p.type === "unique_id" || ((p.type === "formula" || p.type === "rollup") && docsValueType(p) === "number");
}

export function sortable(p: DocsProperty): boolean {
  return p.type !== "button" && p.type !== "files";
}

/** The value type a footer calculation aggregates (title counts as text). */
export function calcValueType(p: DocsProperty): string | null {
  if (p.type === "button") return null;
  if (p.type === "title") return "text";
  return docsValueType(p);
}

export const CALC_SHORT: Partial<Record<DocsRollupFunction, string>> = {
  count_all: "Count",
  count_values: "Values",
  count_unique_values: "Unique",
  count_empty: "Empty",
  count_not_empty: "Not empty",
  percent_empty: "Empty",
  percent_not_empty: "Not empty",
  sum: "Sum",
  average: "Average",
  median: "Median",
  min: "Min",
  max: "Max",
  range: "Range",
  earliest_date: "Earliest",
  latest_date: "Latest",
  date_range: "Range",
  checked: "Checked",
  unchecked: "Unchecked",
  percent_checked: "Checked",
  percent_unchecked: "Unchecked",
};

/** A footer calculation's value as text (counts plain, percents with %, the rest as the property shows them). */
export function formatCalc(fn: DocsRollupFunction, v: DocsComputedValue | undefined, p: DocsProperty, refs?: DocsRefs): string {
  if (v === undefined || v === null) return "";
  if (typeof v === "number") {
    if (fn.startsWith("percent_")) return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(v)}%`;
    if (fn.startsWith("count") || fn === "checked" || fn === "unchecked") return new Intl.NumberFormat().format(v);
    if (fn === "date_range") return `${v} ${v === 1 ? "day" : "days"}`;
  }
  return docsFormatComputed(v, p.type === "title" ? undefined : p, refs);
}

/** Group sections in the server's order, hidden groups left out. */
export function groupSections(rows: DocsRow[], groups: DocsGroupInfo[] | undefined, sub = false): { group: DocsGroupInfo; rows: DocsRow[] }[] {
  if (!groups) return [];
  const out = groups.filter((g) => !g.hidden).map((g) => ({ group: g, rows: [] as DocsRow[] }));
  const byKey = new Map(out.map((x) => [x.group.key, x]));
  for (const r of rows) {
    const keys = (sub ? r.sub_groups : r.groups) ?? [];
    for (const k of keys.length ? keys : [""]) byKey.get(k)?.rows.push(r);
  }
  return out;
}

/** A short random property id (the server accepts client ids). */
export function newPropId(): string {
  const a = "abcdefghijklmnopqrstuvwxyz0123456789";
  let s = "";
  for (let i = 0; i < 8; i++) s += a[Math.floor(Math.random() * a.length)];
  return s;
}

/** A name not used by another property ("Parent item", "Parent item 2"…). */
export function freeName(schema: DocsProperty[], base: string): string {
  const taken = new Set(schema.map((p) => p.name.toLowerCase()));
  if (!taken.has(base.toLowerCase())) return base;
  for (let i = 2; ; i++) if (!taken.has(`${base} ${i}`.toLowerCase())) return `${base} ${i}`;
}
