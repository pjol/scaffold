"use client";

import {
  DOCS_ADDABLE_TYPES,
  DOCS_FILTER_OP_LABEL,
  DOCS_PROPERTY_LABEL,
  DOCS_RELATIVE_DATES,
  DOCS_WITHIN_RANGES,
  docsDefaultFilter,
  docsFilterOpsFor,
  docsIsFilterGroup,
  docsOpIsUnary,
  docsTimeZone,
  docsTitle,
  docsValueType,
  useDebouncedValue,
  useDocsRows,
  type DocsFilter,
  type DocsFilterCondition,
  type DocsFilterGroup,
  type DocsProperty,
  type DocsPropertyType,
  type DocsRefs,
  type DocsSort,
  type DocsViewConfig,
} from "@scaffoldapp/shared";
import { Button, IconButton, Input, Select, cn } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Copy, EyeOff, Filter, Group, Info, Plus, Settings2, Snowflake, Trash2, X } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { Floating, floatingItemClass } from "../Floating";
import { newShortId, PROPERTY_ICON } from "../format";

const SORTABLE_EXTRA = [
  { value: "created_time", label: "Created time" },
  { value: "last_edited_time", label: "Last edited time" },
];

export const MAX_FILTER_DEPTH = 3;

/** Properties a filter can test (formulas and rollups with a filterable result). */
export function filterable(schema: DocsProperty[]): DocsProperty[] {
  return schema.filter((p) => docsFilterOpsFor(p).length > 0);
}

function PropertyOption({ p }: { p: DocsProperty }) {
  const Icon = PROPERTY_ICON[p.type];
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Icon aria-hidden className="size-4 shrink-0 text-fg-muted" />
      <span className="truncate">{p.name}</span>
    </span>
  );
}

function propertyOptions(schema: DocsProperty[]) {
  return schema.map((p) => ({ value: p.id, label: <PropertyOption p={p} /> }));
}

/** Picks a related row (relation filters). */
function RowPick({ dbId, value, refs, onChange }: { dbId: string | undefined; value: string; refs: DocsRefs; onChange: (id: string) => void }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const [q, setQ] = useState("");
  const query = useDebouncedValue(q.trim(), 200);
  const params = useMemo(() => ({ ...(query ? { q: query } : {}), limit: 20, tz: docsTimeZone() }), [query]);
  const rows = useDocsRows(dbId ?? null, params, { enabled: !!dbId && !!anchor });
  const [picked, setPicked] = useState<string | null>(null);
  const ref = value ? refs.pages[value] : undefined;
  const label = value ? (picked ?? (ref ? (ref.title === null ? "No access" : docsTitle(ref.title)) : "A row")) : "Choose a row";
  return (
    <>
      <button
        type="button"
        onClick={(e) => setAnchor(e.currentTarget)}
        className="flex h-control min-w-32 flex-1 items-center rounded-md border border-border bg-surface px-3 text-left type-label text-fg focus-visible:outline-2 focus-visible:outline-ring"
      >
        <span className="truncate">{label}</span>
      </button>
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus label="Choose a row">
        <div className="flex w-72 flex-col gap-1 p-1">
          <div className="p-1">
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search rows" aria-label="Search rows" data-autofocus />
          </div>
          <div role="listbox" aria-label="Rows" className="flex max-h-64 flex-col overflow-y-auto">
            {rows.isPending ? <p className="px-3 py-2 type-label text-fg-muted">Loading rows…</p> : null}
            {rows.isError ? <p className="px-3 py-2 type-label text-fg-muted">You can't open the related database.</p> : null}
            {(rows.data?.rows ?? []).map((r) => (
              <button
                key={r.id}
                type="button"
                role="option"
                aria-selected={r.id === value}
                className={floatingItemClass}
                onClick={() => {
                  setPicked(docsTitle(r.title));
                  onChange(r.id);
                  setAnchor(null);
                }}
              >
                {r.icon ? <span aria-hidden>{r.icon}</span> : null}
                <span className="min-w-0 flex-1 truncate">{docsTitle(r.title)}</span>
              </button>
            ))}
            {rows.data && rows.data.rows.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No row matches.</p> : null}
          </div>
        </div>
      </Floating>
    </>
  );
}

/** A date filter value: an exact day or a relative one; "is within" takes a range. */
function DateFilterValue({ filter, onChange }: { filter: DocsFilterCondition; onChange: (v: unknown) => void }) {
  if (filter.op === "is_within") {
    return (
      <Select
        aria-label="Range"
        className="min-w-36 flex-1"
        value={typeof filter.value === "string" ? filter.value : "this_week"}
        options={DOCS_WITHIN_RANGES.map((r) => ({ value: r.value, label: r.label }))}
        onValueChange={onChange}
      />
    );
  }
  const v = typeof filter.value === "string" ? filter.value : "";
  const relative = DOCS_RELATIVE_DATES.some((r) => r.value === v);
  return (
    <div className="flex min-w-36 flex-1 flex-wrap items-center gap-1">
      <Select
        aria-label="Date"
        className="min-w-32 flex-1"
        value={relative ? v : "exact"}
        options={[{ value: "exact", label: "Exact date" }, ...DOCS_RELATIVE_DATES.map((r) => ({ value: r.value, label: r.label }))]}
        onValueChange={(x) => onChange(x === "exact" ? "" : x)}
      />
      {!relative ? <Input type="date" aria-label="Exact date" className="min-w-32 flex-1" value={/^\d{4}-\d{2}-\d{2}$/.test(v) ? v : ""} onChange={(e) => onChange(e.target.value)} /> : null}
    </div>
  );
}

function FilterValue({ prop, filter, refs, onChange }: { prop: DocsProperty; filter: DocsFilterCondition; refs: DocsRefs; onChange: (v: unknown) => void }) {
  if (docsOpIsUnary(filter.op)) return null;
  const t = docsValueType(prop);
  switch (prop.type) {
    case "select":
    case "status":
    case "multi_select":
      return (
        <Select
          aria-label="Value"
          className="min-w-32 flex-1"
          value={typeof filter.value === "string" ? filter.value : ""}
          placeholder="Choose"
          options={(prop.options ?? []).map((o) => ({ value: o.id, label: o.name }))}
          onValueChange={onChange}
        />
      );
    case "person":
    case "created_by":
    case "last_edited_by":
      return (
        <Select
          aria-label="Person"
          className="min-w-32 flex-1"
          value={typeof filter.value === "string" ? filter.value : ""}
          placeholder="Choose"
          options={[{ value: "me", label: "Me" }, ...Object.values(refs.users).map((u) => ({ value: u.id, label: u.name }))]}
          onValueChange={onChange}
        />
      );
    case "relation":
      return <RowPick dbId={prop.relation?.database_id} value={typeof filter.value === "string" ? filter.value : ""} refs={refs} onChange={onChange} />;
    case "checkbox":
      return <CheckFilter value={filter.value} onChange={onChange} />;
    case "date":
    case "created_time":
    case "last_edited_time":
      return <DateFilterValue filter={filter} onChange={onChange} />;
    case "number":
    case "unique_id":
      return <NumberFilter key={`${filter.property}-${filter.op}`} value={filter.value} onChange={onChange} />;
  }
  if (t === "boolean") return <CheckFilter value={filter.value} onChange={onChange} />;
  if (t === "date") return <DateFilterValue filter={filter} onChange={onChange} />;
  if (t === "number") return <NumberFilter key={`${filter.property}-${filter.op}`} value={filter.value} onChange={onChange} />;
  return <Input aria-label="Text" className="min-w-32 flex-1" defaultValue={typeof filter.value === "string" ? filter.value : ""} onChange={(e) => onChange(e.target.value)} />;
}

function CheckFilter({ value, onChange }: { value: unknown; onChange: (v: unknown) => void }) {
  return (
    <Select
      aria-label="Value"
      className="min-w-28 flex-1"
      value={value === false ? "false" : "true"}
      options={[
        { value: "true", label: "Checked" },
        { value: "false", label: "Unchecked" },
      ]}
      onValueChange={(v) => onChange(v === "true")}
    />
  );
}

function NumberFilter({ value, onChange }: { value: unknown; onChange: (v: unknown) => void }) {
  return (
    <Input
      aria-label="Number"
      inputMode="decimal"
      className="min-w-24 flex-1"
      defaultValue={typeof value === "number" ? String(value) : ""}
      onBlur={(e) => {
        const t = e.target.value.trim().replace(/,/g, "");
        onChange(t === "" || !Number.isFinite(Number(t)) ? undefined : Number(t));
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur();
      }}
    />
  );
}

function ConjLabel({ index, conj, onConj }: { index: number; conj: "and" | "or"; onConj: (c: "and" | "or") => void }) {
  if (index === 0) return <span className="w-16 shrink-0 type-caption text-fg-muted">Where</span>;
  if (index === 1) {
    return (
      <Select
        aria-label="Match"
        className="w-20 shrink-0"
        value={conj}
        options={[
          { value: "and", label: "And" },
          { value: "or", label: "Or" },
        ]}
        onValueChange={(v) => onConj(v as "and" | "or")}
      />
    );
  }
  return <span className="w-16 shrink-0 type-caption text-fg-muted">{conj === "and" ? "And" : "Or"}</span>;
}

/** A list of conditions and groups joined by one conjunction (nested up to three deep). */
function FilterList({ schema, filters, conj, depth, refs, onChange }: { schema: DocsProperty[]; filters: DocsFilter[]; conj: "and" | "or"; depth: number; refs: DocsRefs; onChange: (filters: DocsFilter[], conj: "and" | "or") => void }) {
  const props = filterable(schema);
  const set = (i: number, f: DocsFilter) => onChange(filters.map((x, j) => (j === i ? f : x)), conj);
  const remove = (i: number) => onChange(filters.filter((_, j) => j !== i), conj);
  const first = props[0];
  return (
    <div className="flex flex-col gap-2">
      {filters.map((f, i) => {
        if (docsIsFilterGroup(f)) {
          return (
            <div key={i} className="flex items-start gap-2">
              <ConjLabel index={i} conj={conj} onConj={(c) => onChange(filters, c)} />
              <div className="flex min-w-0 flex-1 flex-col gap-2 rounded-md border border-border bg-sunken/40 p-2">
                <FilterList schema={schema} filters={f.filters} conj={f.conj} depth={depth + 1} refs={refs} onChange={(fs, c) => set(i, { conj: c, filters: fs } satisfies DocsFilterGroup)} />
                <div className="flex justify-end">
                  <Button size="sm" variant="danger-ghost" leftIcon={<Trash2 />} onClick={() => remove(i)}>
                    Remove group
                  </Button>
                </div>
              </div>
            </div>
          );
        }
        const prop = schema.find((p) => p.id === f.property);
        if (!prop) {
          return (
            <div key={i} className="flex items-center gap-2">
              <ConjLabel index={i} conj={conj} onConj={(c) => onChange(filters, c)} />
              <span className="flex-1 type-label text-fg-muted">A deleted property</span>
              <IconButton label="Remove filter" icon={<X />} size="sm" variant="ghost" onClick={() => remove(i)} />
            </div>
          );
        }
        const ops = docsFilterOpsFor(prop);
        return (
          <div key={i} className="flex flex-wrap items-center gap-2">
            <ConjLabel index={i} conj={conj} onConj={(c) => onChange(filters, c)} />
            <Select aria-label="Property" className="w-40" value={f.property} options={propertyOptions(props)} onValueChange={(id) => set(i, docsDefaultFilter(schema.find((p) => p.id === id)!))} />
            <Select
              aria-label="Condition"
              className="w-40"
              value={f.op}
              options={ops.map((op) => ({ value: op, label: DOCS_FILTER_OP_LABEL[op] ?? op }))}
              onValueChange={(op) => set(i, op === "is_within" ? { property: f.property, op, value: "this_week" } : f.op === "is_within" ? { property: f.property, op } : { ...f, op })}
            />
            <FilterValue key={`${f.property}`} prop={prop} filter={f} refs={refs} onChange={(v) => set(i, { ...f, value: v })} />
            <IconButton label="Remove filter" icon={<X />} size="sm" variant="ghost" onClick={() => remove(i)} />
          </div>
        );
      })}
      <div className="flex flex-wrap gap-2">
        {first ? (
          <AddProperty label="Add a filter" schema={props} onPick={(p) => onChange([...filters, docsDefaultFilter(p)], conj)} />
        ) : (
          <p className="type-label text-fg-muted">Add a property to filter by.</p>
        )}
        {first && depth < MAX_FILTER_DEPTH ? (
          <Button size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => onChange([...filters, { conj: conj === "and" ? "or" : "and", filters: [docsDefaultFilter(first)] }], conj)}>
            Add a filter group
          </Button>
        ) : null}
      </div>
    </div>
  );
}

/**
 * Filters: conditions and groups (up to three deep) joined by and/or. `onChange` receives the
 * filters and the top-level conjunction.
 */
export function FilterPanel({ schema, config, refs, onChange }: { schema: DocsProperty[]; config: DocsViewConfig; refs: DocsRefs; onChange: (filters: DocsFilter[], conj?: "and" | "or") => void }) {
  return (
    <div className="flex max-h-[min(36rem,80vh)] w-[min(44rem,calc(100vw-2rem))] flex-col gap-3 overflow-y-auto p-3">
      {config.filters.length === 0 ? <p className="type-label text-fg-muted">No filters. Every row shows.</p> : null}
      <FilterList schema={schema} filters={config.filters} conj={config.filter_conj ?? "and"} depth={1} refs={refs} onChange={(filters, conj) => onChange(filters, conj)} />
    </div>
  );
}

/** The conditions editor alone (automations reuse it). */
export function FilterEditor({ schema, filters, conj = "and", refs, onChange }: { schema: DocsProperty[]; filters: DocsFilter[]; conj?: "and" | "or"; refs: DocsRefs; onChange: (filters: DocsFilter[], conj: "and" | "or") => void }) {
  return <FilterList schema={schema} filters={filters} conj={conj} depth={1} refs={refs} onChange={onChange} />;
}

export function sortable(schema: DocsProperty[]): DocsProperty[] {
  return schema.filter((p) => p.type !== "button" && p.type !== "files");
}

export function SortPanel({ schema, config, onChange }: { schema: DocsProperty[]; config: DocsViewConfig; onChange: (sorts: DocsSort[]) => void }) {
  const props = sortable(schema);
  const options = [...propertyOptions(props), ...SORTABLE_EXTRA.filter((x) => !schema.some((p) => p.type === x.value))];
  const move = (i: number, d: -1 | 1) => {
    const j = i + d;
    if (j < 0 || j >= config.sorts.length) return;
    const next = [...config.sorts];
    [next[i], next[j]] = [next[j]!, next[i]!];
    onChange(next);
  };
  return (
    <div className="flex w-[min(30rem,calc(100vw-2rem))] flex-col gap-3 p-3">
      {config.sorts.length === 0 ? <p className="type-label text-fg-muted">No sorts. Rows keep their manual order.</p> : null}
      {config.sorts.map((s, i) => (
        <div key={`${s.property}-${i}`} className="flex flex-wrap items-center gap-2">
          <Select aria-label="Property" className="min-w-36 flex-1" value={s.property} options={options} onValueChange={(v) => onChange(config.sorts.map((x, j) => (j === i ? { ...x, property: v } : x)))} />
          <Select
            aria-label="Direction"
            className="w-36"
            value={s.direction}
            options={[
              { value: "asc", label: "Ascending" },
              { value: "desc", label: "Descending" },
            ]}
            onValueChange={(v) => onChange(config.sorts.map((x, j) => (j === i ? { ...x, direction: v as "asc" | "desc" } : x)))}
          />
          <div className="flex">
            <IconButton label="Move up" icon={<ArrowUp />} size="sm" variant="ghost" disabled={i === 0} onClick={() => move(i, -1)} />
            <IconButton label="Move down" icon={<ArrowDown />} size="sm" variant="ghost" disabled={i === config.sorts.length - 1} onClick={() => move(i, 1)} />
            <IconButton label="Remove sort" icon={<X />} size="sm" variant="ghost" onClick={() => onChange(config.sorts.filter((_, j) => j !== i))} />
          </div>
        </div>
      ))}
      {config.sorts.length < 5 ? (
        <AddProperty
          label="Add a sort"
          schema={props.filter((p) => !config.sorts.some((s) => s.property === p.id))}
          extra={SORTABLE_EXTRA.filter((x) => !config.sorts.some((s) => s.property === x.value))}
          onPick={(p) => onChange([...config.sorts, { property: p.id, direction: "asc" }])}
        />
      ) : null}
    </div>
  );
}

function AddProperty({ label, schema, extra = [], onPick }: { label: string; schema: DocsProperty[]; extra?: { value: string; label: string }[]; onPick: (p: DocsProperty) => void }) {
  return (
    <Select
      aria-label={label}
      className="w-48"
      placeholder={label}
      value=""
      options={[...propertyOptions(schema), ...extra]}
      onValueChange={(id) => {
        const p = schema.find((x) => x.id === id) ?? ({ id, name: id, type: "created_time" } as DocsProperty);
        onPick(p);
      }}
    />
  );
}

/** Types that need settings before they work (they open the property settings). */
export const SETUP_TYPES: readonly DocsPropertyType[] = ["relation", "rollup", "formula", "button"];

/** A new property of a type with what it needs to start (status options, number format, a button label). */
export function newProperty(type: DocsPropertyType, name: string): DocsProperty {
  const base: DocsProperty = { id: newShortId(), name: name.trim() || DOCS_PROPERTY_LABEL[type], type };
  if (type === "select" || type === "multi_select") base.options = [];
  if (type === "status")
    base.options = [
      { id: newShortId(), name: "Not started", color: "default", group: "todo" },
      { id: newShortId(), name: "In progress", color: "blue", group: "doing" },
      { id: newShortId(), name: "Done", color: "green", group: "done" },
    ];
  if (type === "number") base.number_format = "number";
  if (type === "unique_id") base.unique_id = { prefix: "" };
  if (type === "button") base.button = { label: base.name, actions: [] };
  if (type === "formula") base.formula = { expression: "" };
  return base;
}

/** A new property: name and type. Types that need settings go to `onSetup`. */
export function NewPropertyForm({ onCreate, onSetup }: { onCreate: (p: DocsProperty) => void; onSetup?: ((p: DocsProperty) => void) | undefined }) {
  const [name, setName] = useState("");
  const [q, setQ] = useState("");
  const types = DOCS_ADDABLE_TYPES.filter((t) => DOCS_PROPERTY_LABEL[t].toLowerCase().includes(q.trim().toLowerCase()));
  const pick = (t: DocsPropertyType) => {
    const p = newProperty(t, name);
    if (onSetup && SETUP_TYPES.includes(t)) onSetup(p);
    else onCreate(p);
  };
  return (
    <div className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-2 p-2">
      <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Property name" aria-label="Property name" maxLength={100} data-autofocus />
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search types" aria-label="Search property types" />
      <div role="listbox" aria-label="Type" className="grid max-h-80 grid-cols-1 gap-0.5 overflow-y-auto sm:grid-cols-2">
        {types.map((t) => {
          const Icon = PROPERTY_ICON[t];
          return (
            <button key={t} type="button" role="option" aria-selected={false} className={floatingItemClass} onClick={() => pick(t)}>
              <Icon aria-hidden />
              <span className="truncate">{DOCS_PROPERTY_LABEL[t]}</span>
            </button>
          );
        })}
        {types.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No type matches.</p> : null}
      </div>
    </div>
  );
}

export interface PropertyMenuProps {
  prop: DocsProperty;
  onChange: (p: DocsProperty) => void;
  onDelete?: (() => void) | undefined;
  onHide?: (() => void) | undefined;
  onMove?: ((dir: -1 | 1) => void) | undefined;
  /** Opens the full settings (formats, formulas, relations…). */
  onEdit?: (() => void) | undefined;
  onSort?: ((dir: "asc" | "desc") => void) | undefined;
  onFilter?: (() => void) | undefined;
  onGroup?: (() => void) | undefined;
  onDuplicate?: (() => void) | undefined;
  onFreeze?: (() => void) | undefined;
  frozen?: boolean;
}

/** A property's quick menu: rename, then actions. Delete asks first (the caller confirms). */
export function PropertyMenu({ prop, onChange, onDelete, onHide, onMove, onEdit, onSort, onFilter, onGroup, onDuplicate, onFreeze, frozen }: PropertyMenuProps) {
  const [name, setName] = useState(prop.name);
  const commitName = () => {
    const n = name.trim();
    if (n && n !== prop.name) onChange({ ...prop, name: n });
  };
  const Icon = PROPERTY_ICON[prop.type];
  const item = (key: string, icon: ReactNode, label: string, run: (() => void) | undefined, cls?: string) =>
    run ? (
      <button key={key} type="button" role="menuitem" className={cn(floatingItemClass, cls)} onClick={run}>
        {icon}
        {label}
      </button>
    ) : null;
  return (
    <div className="flex w-72 flex-col gap-2 p-2">
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          commitName();
        }}
      >
        <Icon aria-hidden className="size-4 shrink-0 text-fg-muted" />
        <Input value={name} onChange={(e) => setName(e.target.value)} onBlur={commitName} aria-label="Property name" maxLength={100} data-autofocus className="flex-1" />
      </form>
      {prop.description ? (
        <p className="flex items-start gap-2 px-1 type-caption text-fg-muted">
          <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
          {prop.description}
        </p>
      ) : null}
      <div role="menu" aria-label={prop.name} className="flex flex-col border-t border-border pt-1">
        {item("edit", <Settings2 aria-hidden />, "Edit property", onEdit)}
        {onSort ? (
          <>
            {item("asc", <ArrowUp aria-hidden />, "Sort ascending", () => onSort("asc"))}
            {item("desc", <ArrowDown aria-hidden />, "Sort descending", () => onSort("desc"))}
          </>
        ) : null}
        {item("filter", <Filter aria-hidden />, "Filter", onFilter)}
        {item("group", <Group aria-hidden />, "Group by", onGroup)}
        {onMove ? (
          <>
            {item("left", <ArrowLeft aria-hidden />, "Move left", () => onMove(-1))}
            {item("right", <ArrowRight aria-hidden />, "Move right", () => onMove(1))}
          </>
        ) : null}
        {item("freeze", <Snowflake aria-hidden />, frozen ? "Unfreeze columns" : "Freeze up to this column", onFreeze)}
        {item("hide", <EyeOff aria-hidden />, "Hide in this view", onHide)}
        {item("dup", <Copy aria-hidden />, "Duplicate property", onDuplicate)}
        {item("delete", <Trash2 aria-hidden />, "Delete property", onDelete, "text-danger-strong")}
      </div>
    </div>
  );
}

