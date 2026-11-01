"use client";

import {
  DOCS_ADDABLE_TYPES,
  DOCS_FILTER_OP_LABEL,
  DOCS_PROPERTY_LABEL,
  docsDefaultFilter,
  docsFilterOps,
  docsOpIsUnary,
  docsToday,
  type DocsFilter,
  type DocsOption,
  type DocsProperty,
  type DocsPropertyType,
  type DocsRefs,
  type DocsSort,
  type DocsViewConfig,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, IconButton, Input, Select } from "@scaffoldapp/ui-web";
import { ArrowLeft, ArrowRight, EyeOff, Plus, Trash2, X } from "lucide-react";
import { useState } from "react";
import { floatingItemClass } from "../Floating";
import { newShortId, OPTION_COLOR_LABEL, OPTION_COLORS, PROPERTY_ICON } from "../format";

const SORTABLE_EXTRA = [
  { value: "created_time", label: "Created" },
  { value: "last_edited_time", label: "Last edited" },
];

function FilterValue({ prop, filter, refs, onChange }: { prop: DocsProperty; filter: DocsFilter; refs: DocsRefs; onChange: (v: unknown) => void }) {
  if (docsOpIsUnary(filter.op)) return null;
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
    case "checkbox":
      return (
        <Select
          aria-label="Value"
          className="min-w-24 flex-1"
          value={filter.value === false ? "false" : "true"}
          options={[
            { value: "true", label: "Checked" },
            { value: "false", label: "Unchecked" },
          ]}
          onValueChange={(v) => onChange(v === "true")}
        />
      );
    case "date":
      return (
        <div className="flex min-w-32 flex-1 items-center gap-1">
          <Input type="date" aria-label="Date" value={filter.value === "today" ? docsToday() : typeof filter.value === "string" ? filter.value : ""} onChange={(e) => onChange(e.target.value)} />
          <Button type="button" size="sm" variant="ghost" onClick={() => onChange("today")}>
            Today
          </Button>
        </div>
      );
    case "number":
      return <Input aria-label="Number" inputMode="decimal" className="min-w-24 flex-1" defaultValue={typeof filter.value === "number" ? String(filter.value) : ""} onBlur={(e) => onChange(e.target.value.trim() === "" ? undefined : Number(e.target.value))} />;
    default:
      return <Input aria-label="Text" className="min-w-32 flex-1" defaultValue={typeof filter.value === "string" ? filter.value : ""} onChange={(e) => onChange(e.target.value)} />;
  }
}

/** Every filter must match (AND). */
export function FilterPanel({ schema, config, refs, onChange }: { schema: DocsProperty[]; config: DocsViewConfig; refs: DocsRefs; onChange: (filters: DocsFilter[]) => void }) {
  const set = (i: number, f: DocsFilter) => onChange(config.filters.map((x, j) => (j === i ? f : x)));
  return (
    <div className="flex w-[min(36rem,calc(100vw-2rem))] flex-col gap-3 p-3">
      {config.filters.length === 0 ? <p className="type-label text-fg-muted">No filters. Rows that match all filters show.</p> : null}
      {config.filters.map((f, i) => {
        const prop = schema.find((p) => p.id === f.property);
        if (!prop) return null;
        return (
          <div key={i} className="flex flex-wrap items-center gap-2">
            <span className="w-12 type-caption text-fg-muted">{i === 0 ? "Where" : "and"}</span>
            <Select aria-label="Property" className="w-36" value={f.property} options={schema.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(id) => set(i, docsDefaultFilter(schema.find((p) => p.id === id)!))} />
            <Select aria-label="Condition" className="w-40" value={f.op} options={docsFilterOps(prop.type).map((op) => ({ value: op, label: DOCS_FILTER_OP_LABEL[op] ?? op }))} onValueChange={(op) => set(i, { ...f, op })} />
            <FilterValue prop={prop} filter={f} refs={refs} onChange={(v) => set(i, { ...f, value: v })} />
            <IconButton label="Remove filter" icon={<X />} size="sm" variant="ghost" onClick={() => onChange(config.filters.filter((_, j) => j !== i))} />
          </div>
        );
      })}
      <AddProperty label="Add a filter" schema={schema} onPick={(p) => onChange([...config.filters, docsDefaultFilter(p)])} />
    </div>
  );
}

export function SortPanel({ schema, config, onChange }: { schema: DocsProperty[]; config: DocsViewConfig; onChange: (sorts: DocsSort[]) => void }) {
  const options = [...schema.map((p) => ({ value: p.id, label: p.name })), ...SORTABLE_EXTRA];
  return (
    <div className="flex w-[min(28rem,calc(100vw-2rem))] flex-col gap-3 p-3">
      {config.sorts.length === 0 ? <p className="type-label text-fg-muted">No sorts. Rows keep their manual order.</p> : null}
      {config.sorts.map((s, i) => (
        <div key={i} className="flex items-center gap-2">
          <Select aria-label="Property" className="flex-1" value={s.property} options={options} onValueChange={(v) => onChange(config.sorts.map((x, j) => (j === i ? { ...x, property: v } : x)))} />
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
          <IconButton label="Remove sort" icon={<X />} size="sm" variant="ghost" onClick={() => onChange(config.sorts.filter((_, j) => j !== i))} />
        </div>
      ))}
      {config.sorts.length < 5 ? (
        <AddProperty label="Add a sort" schema={schema} extra={SORTABLE_EXTRA} onPick={(p) => onChange([...config.sorts, { property: p.id, direction: "asc" }])} />
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
      options={[...schema.map((p) => ({ value: p.id, label: p.name })), ...extra]}
      onValueChange={(id) => {
        const p = schema.find((x) => x.id === id) ?? ({ id, name: id, type: "text" } as DocsProperty);
        onPick(p);
      }}
    />
  );
}

/** A new property: name and type. */
export function NewPropertyForm({ onCreate }: { onCreate: (p: DocsProperty) => void }) {
  const [name, setName] = useState("");
  const [type, setType] = useState<DocsPropertyType>("text");
  return (
    <form
      className="flex w-72 flex-col gap-3 p-3"
      onSubmit={(e) => {
        e.preventDefault();
        const n = name.trim() || DOCS_PROPERTY_LABEL[type];
        const base: DocsProperty = { id: newShortId(), name: n, type };
        if (type === "select" || type === "multi_select") base.options = [];
        if (type === "status")
          base.options = [
            { id: newShortId(), name: "Not started", color: "neutral", group: "todo" },
            { id: newShortId(), name: "In progress", color: "info", group: "doing" },
            { id: newShortId(), name: "Done", color: "success", group: "done" },
          ];
        if (type === "number") base.number_format = "number";
        onCreate(base);
      }}
    >
      <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Property name" aria-label="Property name" maxLength={100} data-autofocus />
      <div role="listbox" aria-label="Type" className="grid grid-cols-2 gap-1">
        {DOCS_ADDABLE_TYPES.map((t) => {
          const Icon = PROPERTY_ICON[t];
          return (
            <button key={t} type="button" role="option" aria-selected={type === t} className={floatingItemClass} onClick={() => setType(t)}>
              <Icon aria-hidden />
              <span className="truncate">{DOCS_PROPERTY_LABEL[t]}</span>
            </button>
          );
        })}
      </div>
      <Button type="submit" size="sm">
        Add property
      </Button>
    </form>
  );
}

/** Rename, change type, edit options, hide, move and delete one property. */
export function PropertyMenu({
  prop,
  onChange,
  onDelete,
  onHide,
  onMove,
}: {
  prop: DocsProperty;
  onChange: (p: DocsProperty) => void;
  onDelete?: (() => void) | undefined;
  onHide?: (() => void) | undefined;
  onMove?: ((dir: -1 | 1) => void) | undefined;
}) {
  const [name, setName] = useState(prop.name);
  const [confirm, setConfirm] = useState(false);
  const commitName = () => {
    const n = name.trim();
    if (n && n !== prop.name) onChange({ ...prop, name: n });
  };
  const setOption = (i: number, o: DocsOption) => onChange({ ...prop, options: (prop.options ?? []).map((x, j) => (j === i ? o : x)) });
  return (
    <div className="flex w-80 flex-col gap-3 p-3">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          commitName();
        }}
      >
        <Input value={name} onChange={(e) => setName(e.target.value)} onBlur={commitName} aria-label="Property name" maxLength={100} data-autofocus />
      </form>
      {prop.type !== "title" ? (
        <Select
          aria-label="Type"
          value={prop.type}
          options={DOCS_ADDABLE_TYPES.map((t) => ({ value: t, label: DOCS_PROPERTY_LABEL[t] }))}
          onValueChange={(t) => {
            const next: DocsProperty = { id: prop.id, name: prop.name, type: t as DocsPropertyType };
            if (t === "select" || t === "multi_select" || t === "status") next.options = (prop.options ?? []).map((o) => ({ ...o, ...(t === "status" ? { group: o.group ?? "todo" } : {}) }));
            if (t === "number") next.number_format = "number";
            onChange(next);
          }}
        />
      ) : null}
      {prop.type === "number" ? (
        <Select
          aria-label="Number format"
          value={prop.number_format ?? "number"}
          options={[
            { value: "number", label: "Number" },
            { value: "percent", label: "Percent" },
            { value: "dollar", label: "US dollar" },
            { value: "euro", label: "Euro" },
            { value: "pound", label: "Pound" },
          ]}
          onValueChange={(v) => onChange({ ...prop, number_format: v as DocsProperty["number_format"] })}
        />
      ) : null}
      {prop.type === "select" || prop.type === "multi_select" || prop.type === "status" ? (
        <div className="flex flex-col gap-2">
          <p className="type-caption font-semibold text-fg-muted">Options</p>
          {(prop.options ?? []).map((o, i) => (
            <div key={o.id} className="flex items-center gap-1">
              <Input
                defaultValue={o.name}
                aria-label="Option name"
                className="flex-1"
                maxLength={100}
                onBlur={(e) => {
                  const n = e.target.value.trim();
                  if (n && n !== o.name) setOption(i, { ...o, name: n });
                }}
              />
              <Select aria-label="Colour" className="w-24" value={o.color} options={OPTION_COLORS.map((c) => ({ value: c, label: OPTION_COLOR_LABEL[c] }))} onValueChange={(c) => setOption(i, { ...o, color: c as DocsOption["color"] })} />
              {prop.type === "status" ? (
                <Select
                  aria-label="Group"
                  className="w-28"
                  value={o.group ?? "todo"}
                  options={[
                    { value: "todo", label: "To-do" },
                    { value: "doing", label: "In progress" },
                    { value: "done", label: "Complete" },
                  ]}
                  onValueChange={(g) => setOption(i, { ...o, group: g as DocsOption["group"] })}
                />
              ) : null}
              <IconButton label={`Delete ${o.name}`} icon={<Trash2 />} size="sm" variant="ghost" onClick={() => onChange({ ...prop, options: (prop.options ?? []).filter((x) => x.id !== o.id) })} />
            </div>
          ))}
          <Button
            type="button"
            size="sm"
            variant="ghost"
            leftIcon={<Plus />}
            onClick={() => onChange({ ...prop, options: [...(prop.options ?? []), { id: newShortId(), name: `Option ${(prop.options?.length ?? 0) + 1}`, color: OPTION_COLORS[(prop.options?.length ?? 0) % OPTION_COLORS.length]!, ...(prop.type === "status" ? { group: "todo" as const } : {}) }] })}
          >
            Add an option
          </Button>
        </div>
      ) : null}
      <div className="flex flex-col border-t border-border pt-2">
        {onMove ? (
          <>
            <button type="button" className={floatingItemClass} onClick={() => onMove(-1)}>
              <ArrowLeft aria-hidden />
              Move left
            </button>
            <button type="button" className={floatingItemClass} onClick={() => onMove(1)}>
              <ArrowRight aria-hidden />
              Move right
            </button>
          </>
        ) : null}
        {onHide ? (
          <button type="button" className={floatingItemClass} onClick={onHide}>
            <EyeOff aria-hidden />
            Hide in this view
          </button>
        ) : null}
        {onDelete ? (
          <button type="button" className={`${floatingItemClass} text-danger-strong`} onClick={() => setConfirm(true)}>
            <Trash2 aria-hidden />
            Delete property
          </button>
        ) : null}
      </div>
      {onDelete ? (
        <ConfirmDialog
          open={confirm}
          onOpenChange={setConfirm}
          title={`Delete “${prop.name}”?`}
          confirmLabel="Delete property"
          tone="danger"
          onConfirm={() => onDelete()}
        >
          Every row loses its {prop.name} value. It can't be undone.
        </ConfirmDialog>
      ) : null}
    </div>
  );
}
