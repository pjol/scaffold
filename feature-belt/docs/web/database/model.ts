import {
  DOCS_ROLLUP_LABEL,
  docsFormatComputed,
  type DocsColumn,
  type DocsComputedValue,
  type DocsProperty,
  type DocsRollupFunction,
  type DocsRow,
  type DocsValue,
  type DocsViewConfig,
  type DocsViewType,
} from "@scaffoldapp/shared";

export const EMPTY_CONFIG: DocsViewConfig = { filters: [], sorts: [], columns: [], card_properties: [] };

/** Every property in the view's order (title first), with its column settings. */
export function orderedColumns(schema: DocsProperty[], config: DocsViewConfig): DocsColumn[] {
  const cols: DocsColumn[] = [];
  for (const c of config.columns) if (schema.some((p) => p.id === c.property) && !cols.some((x) => x.property === c.property)) cols.push(c);
  for (const p of schema) if (!cols.some((c) => c.property === p.id)) cols.push({ property: p.id });
  const title = cols.findIndex((c) => c.property === "title");
  if (title > 0) cols.unshift(...cols.splice(title, 1));
  return cols;
}

/** Card-like views start with no properties shown; tables show every property. */
export function showsAllByDefault(type: DocsViewType): boolean {
  return type === "table";
}

/** Whether a column is shown in a view type (unlisted properties follow the type's default). */
export function columnShown(config: DocsViewConfig, type: DocsViewType, id: string): boolean {
  if (id === "title") return true;
  const listed = config.columns.find((c) => c.property === id);
  if (listed) return !listed.hidden;
  if (!config.columns.length && !showsAllByDefault(type)) return config.card_properties.includes(id);
  return showsAllByDefault(type);
}

/** The properties a view shows, in order (title first). */
export function visibleProps(schema: DocsProperty[], config: DocsViewConfig, type: DocsViewType): DocsProperty[] {
  const byId = new Map(schema.map((p) => [p.id, p]));
  let order = orderedColumns(schema, config).map((c) => c.property);
  if (!config.columns.length && !showsAllByDefault(type)) {
    // docs 1.1 card properties keep their order.
    order = ["title", ...config.card_properties, ...order.filter((id) => id !== "title" && !config.card_properties.includes(id))];
  }
  return order.filter((id) => columnShown(config, type, id)).map((id) => byId.get(id)!).filter(Boolean);
}

/** Columns with every property listed and `id` shown or hidden (so new properties keep the type's default). */
export function setShown(schema: DocsProperty[], config: DocsViewConfig, type: DocsViewType, id: string, shown: boolean): DocsColumn[] {
  const cols = orderedColumns(schema, config).map((c) => ({ ...c, hidden: !columnShown(config, type, c.property) }));
  return cols.map((c) => (c.property === id ? { ...c, hidden: !shown } : c)).map(({ hidden, ...rest }) => (hidden ? { ...rest, hidden: true } : rest));
}

/** Columns in a new order, keeping each one's shown state. */
export function reorderColumns(schema: DocsProperty[], config: DocsViewConfig, type: DocsViewType, from: number, to: number): DocsColumn[] {
  const cols = orderedColumns(schema, config).map((c) => ({ ...c, hidden: !columnShown(config, type, c.property) }));
  if (from < 1 || to < 1 || from >= cols.length || to >= cols.length) return config.columns;
  const [moved] = cols.splice(from, 1);
  cols.splice(to, 0, moved!);
  return cols.map(({ hidden, ...rest }) => (hidden ? { ...rest, hidden: true } : rest));
}

/** Whether the viewer may change this row. */
export function rowEditable(row: Pick<DocsRow, "my_level">, editable: boolean): boolean {
  return editable && (row.my_level === "edit" || row.my_level === "manage");
}

/**
 * The value that puts a row in a group (adding to or dropping into it), or undefined when
 * the group can't be set by hand (date buckets, numbers, computed values).
 */
export function groupValue(p: DocsProperty | undefined, key: string, current?: DocsValue): DocsValue | undefined {
  if (!p) return undefined;
  switch (p.type) {
    case "select":
    case "status":
      return key === "" ? null : key;
    case "multi_select":
    case "person":
    case "relation": {
      if (key === "") return [];
      const list = Array.isArray(current) ? (current as string[]) : [];
      return list.includes(key) ? list : [key];
    }
    case "checkbox":
      return key === "true";
    case "text":
    case "url":
    case "email":
    case "phone":
      return key === "" ? null : key.startsWith("t:") ? key.slice(2) : undefined;
  }
  return undefined;
}

/** Moving a row from one group to another (multi-value properties swap the one key). */
export function moveGroupValue(p: DocsProperty | undefined, from: string, to: string, current: DocsValue | undefined): DocsValue | undefined {
  if (!p) return undefined;
  if (p.type === "multi_select" || p.type === "person" || p.type === "relation") {
    const list = (Array.isArray(current) ? (current as string[]) : []).filter((x) => x !== from);
    if (to === "") return list.length ? list : [];
    return list.includes(to) ? list : [...list, to];
  }
  return groupValue(p, to, current);
}

/** Text for a footer calculation. */
export function formatCalc(fn: DocsRollupFunction, v: DocsComputedValue | undefined, p: DocsProperty): string {
  if (v === undefined || v === null) return "";
  if (typeof v === "number") {
    if (/^percent_/.test(fn)) return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(v)}%`;
    if (/^count|^checked$|^unchecked$/.test(fn)) return new Intl.NumberFormat().format(v);
    if (fn === "date_range") return `${v} ${v === 1 ? "day" : "days"}`;
  }
  const fmt: DocsProperty = p.type === "title" ? { ...p, type: "text" } : p;
  return docsFormatComputed(v, fmt) || String(v);
}

/** Short label for a calculation in the footer ("Sum", "Count"). */
export function calcLabel(fn: DocsRollupFunction): string {
  return DOCS_ROLLUP_LABEL[fn];
}
