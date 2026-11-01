"use client";

import {
  docsFormatDate,
  docsFormatNumber,
  docsOption,
  docsSafeLink,
  useAccessDirectory,
  type DocsDateValue,
  type DocsOption,
  type DocsProperty,
  type DocsRefs,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Avatar, Badge, Button, Input, cn } from "@scaffoldapp/ui-web";
import { Check, Plus, X } from "lucide-react";
import { useState } from "react";
import { floatingItemClass } from "../Floating";
import { newShortId, OPTION_COLORS, optionTone } from "../format";

/** A row value as it reads in a cell, card or the properties panel. */
export function PropertyDisplay({ prop, value, refs, compact }: { prop: DocsProperty; value: DocsValue | undefined; refs: DocsRefs; compact?: boolean }) {
  if (value === null || value === undefined || (Array.isArray(value) && value.length === 0)) {
    if (prop.type === "checkbox") return <CheckMark on={false} />;
    return compact ? null : <span className="type-label text-fg-subtle">Empty</span>;
  }
  switch (prop.type) {
    case "select":
    case "status": {
      const o = docsOption(prop, value);
      return o ? <OptionBadge option={o} /> : null;
    }
    case "multi_select":
      return (
        <span className="flex flex-wrap gap-1">
          {(value as string[]).map((id) => {
            const o = docsOption(prop, id);
            return o ? <OptionBadge key={id} option={o} /> : null;
          })}
        </span>
      );
    case "person":
      return (
        <span className="flex flex-wrap items-center gap-2">
          {(value as string[]).map((id) => {
            const u = refs.users[id];
            return (
              <span key={id} className="flex min-w-0 items-center gap-1.5">
                <Avatar size="xs" name={u?.name ?? "Deleted user"} {...(u?.avatar_url ? { src: u.avatar_url } : {})} />
                {compact ? null : <span className="truncate type-label text-fg">{u?.name ?? "Deleted user"}</span>}
              </span>
            );
          })}
        </span>
      );
    case "checkbox":
      return <CheckMark on={value === true} />;
    case "number":
      return <span className="type-label tabular-nums text-fg">{typeof value === "number" ? docsFormatNumber(value, prop.number_format) : ""}</span>;
    case "date": {
      const d = value as DocsDateValue;
      return <span className="type-label text-fg">{d.end ? `${docsFormatDate(d.start)} → ${docsFormatDate(d.end)}` : docsFormatDate(d.start)}</span>;
    }
    case "url":
      return (
        <a href={String(value)} target="_blank" rel="noopener noreferrer nofollow" className="truncate type-label text-primary-strong underline underline-offset-2" onClick={(e) => e.stopPropagation()}>
          {String(value).replace(/^https?:\/\//, "")}
        </a>
      );
    case "email":
      return (
        <a href={`mailto:${String(value)}`} className="truncate type-label text-primary-strong underline underline-offset-2" onClick={(e) => e.stopPropagation()}>
          {String(value)}
        </a>
      );
    default:
      return <span className={cn("type-label text-fg", compact ? "truncate" : "whitespace-pre-wrap break-words")}>{String(value)}</span>;
  }
}

export function OptionBadge({ option }: { option: DocsOption }) {
  return (
    <Badge tone={optionTone(option.color)} variant="soft" className="max-w-full">
      <span className="truncate">{option.name}</span>
    </Badge>
  );
}

function CheckMark({ on }: { on: boolean }) {
  return (
    <span aria-label={on ? "Yes" : "No"} className={cn("flex size-4 items-center justify-center rounded-xs border [&_svg]:size-3", on ? "border-primary bg-primary text-primary-fg" : "border-border-strong bg-surface")}>
      {on ? <Check aria-hidden strokeWidth={3} /> : null}
    </span>
  );
}

/**
 * Edits one value inside a Floating panel. `onSchema` adds an option typed into a
 * select (the property list changes first, then the value).
 */
export function PropertyEditor({
  prop,
  value,
  orgId,
  onChange,
  onSchema,
  onDone,
}: {
  prop: DocsProperty;
  value: DocsValue | undefined;
  orgId: string;
  onChange: (v: DocsValue) => void;
  onSchema?: ((next: DocsProperty) => void) | undefined;
  onDone: () => void;
}) {
  switch (prop.type) {
    case "select":
    case "status":
    case "multi_select":
      return <OptionsEditor prop={prop} value={value} onChange={onChange} onSchema={onSchema} onDone={onDone} />;
    case "person":
      return <PeopleEditor orgId={orgId} value={(value as string[] | null) ?? []} onChange={onChange} />;
    case "date":
      return <DateEditor value={(value as DocsDateValue | null) ?? null} onChange={onChange} onDone={onDone} />;
    default:
      return <TextEditor prop={prop} value={value} onChange={onChange} onDone={onDone} />;
  }
}

function TextEditor({ prop, value, onChange, onDone }: { prop: DocsProperty; value: DocsValue | undefined; onChange: (v: DocsValue) => void; onDone: () => void }) {
  const [v, setV] = useState(value === null || value === undefined ? "" : String(value));
  const [error, setError] = useState<string | null>(null);
  const save = () => {
    const t = v.trim();
    if (!t) {
      onChange(null);
      return onDone();
    }
    if (prop.type === "number") {
      const n = Number(t.replace(/[,\s$€£%]/g, ""));
      if (!Number.isFinite(n)) return setError("Enter a number.");
      onChange(n);
    } else if (prop.type === "url") {
      const href = docsSafeLink(t);
      if (!href || href.startsWith("mailto:")) return setError("Enter a web address.");
      onChange(href);
    } else if (prop.type === "email") {
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(t)) return setError("Enter an email address.");
      onChange(t);
    } else onChange(t);
    onDone();
  };
  return (
    <form
      className="flex w-72 flex-col gap-2 p-2"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Input
        value={v}
        onChange={(e) => setV(e.target.value)}
        aria-label={prop.name}
        aria-invalid={!!error}
        inputMode={prop.type === "number" ? "decimal" : prop.type === "email" ? "email" : prop.type === "url" ? "url" : "text"}
        data-autofocus
        onBlur={() => {
          if (!error) save();
        }}
      />
      {error ? <p className="type-caption text-danger-strong">{error}</p> : null}
    </form>
  );
}

function DateEditor({ value, onChange, onDone }: { value: DocsDateValue | null; onChange: (v: DocsValue) => void; onDone: () => void }) {
  const [start, setStart] = useState(value?.start.slice(0, 10) ?? "");
  const [end, setEnd] = useState(value?.end?.slice(0, 10) ?? "");
  const [range, setRange] = useState(!!value?.end);
  return (
    <form
      className="flex w-72 flex-col gap-3 p-2"
      onSubmit={(e) => {
        e.preventDefault();
        if (!start) onChange(null);
        else onChange(range && end && end >= start ? { start, end } : { start });
        onDone();
      }}
    >
      <label className="flex flex-col gap-1 type-label text-fg-muted">
        {range ? "Start" : "Date"}
        <Input type="date" value={start} onChange={(e) => setStart(e.target.value)} data-autofocus />
      </label>
      {range ? (
        <label className="flex flex-col gap-1 type-label text-fg-muted">
          End
          <Input type="date" value={end} min={start} onChange={(e) => setEnd(e.target.value)} />
        </label>
      ) : null}
      <div className="flex items-center justify-between gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={() => setRange((r) => !r)}>
          {range ? "Single date" : "Add an end date"}
        </Button>
        <div className="flex gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => {
              onChange(null);
              onDone();
            }}
          >
            Clear
          </Button>
          <Button type="submit" size="sm">
            Save
          </Button>
        </div>
      </div>
    </form>
  );
}

function OptionsEditor({ prop, value, onChange, onSchema, onDone }: { prop: DocsProperty; value: DocsValue | undefined; onChange: (v: DocsValue) => void; onSchema?: ((next: DocsProperty) => void) | undefined; onDone: () => void }) {
  const multi = prop.type === "multi_select";
  const [q, setQ] = useState("");
  const chosen = new Set(multi ? ((value as string[] | null) ?? []) : value ? [value as string] : []);
  const opts = (prop.options ?? []).filter((o) => o.name.toLowerCase().includes(q.trim().toLowerCase()));
  const exact = (prop.options ?? []).some((o) => o.name.toLowerCase() === q.trim().toLowerCase());
  const pick = (id: string) => {
    if (multi) {
      const next = chosen.has(id) ? [...chosen].filter((x) => x !== id) : [...chosen, id];
      onChange(next);
    } else {
      onChange(chosen.has(id) ? null : id);
      onDone();
    }
  };
  const create = () => {
    const name = q.trim();
    if (!name || !onSchema) return;
    const opt: DocsOption = { id: newShortId(), name, color: OPTION_COLORS[(prop.options?.length ?? 0) % OPTION_COLORS.length]!, ...(prop.type === "status" ? { group: "todo" as const } : {}) };
    onSchema({ ...prop, options: [...(prop.options ?? []), opt] });
    setQ("");
    if (multi) onChange([...chosen, opt.id]);
    else {
      onChange(opt.id);
      onDone();
    }
  };
  return (
    <div className="flex w-72 flex-col gap-1 p-1">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (opts.length === 1 && opts[0]) pick(opts[0].id);
          else if (!exact) create();
        }}
        className="p-1"
      >
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={onSchema ? "Search or add an option" : "Search options"} aria-label="Search options" data-autofocus />
      </form>
      <div role="listbox" aria-multiselectable={multi} className="flex max-h-64 flex-col overflow-y-auto">
        {opts.map((o) => (
          <button key={o.id} type="button" role="option" aria-selected={chosen.has(o.id)} className={floatingItemClass} onClick={() => pick(o.id)}>
            <span className="flex size-4 items-center justify-center text-primary-strong">{chosen.has(o.id) ? <Check aria-hidden /> : null}</span>
            <OptionBadge option={o} />
          </button>
        ))}
        {q.trim() && !exact && onSchema ? (
          <button type="button" className={floatingItemClass} onClick={create}>
            <Plus aria-hidden />
            <span className="truncate">Add “{q.trim()}”</span>
          </button>
        ) : null}
        {opts.length === 0 && !(q.trim() && onSchema) ? <p className="px-3 py-2 type-label text-fg-muted">No options yet.</p> : null}
      </div>
      {chosen.size ? (
        <button
          type="button"
          className={cn(floatingItemClass, "text-fg-muted")}
          onClick={() => {
            onChange(multi ? [] : null);
            onDone();
          }}
        >
          <X aria-hidden />
          Clear
        </button>
      ) : null}
    </div>
  );
}

function PeopleEditor({ orgId, value, onChange }: { orgId: string; value: string[]; onChange: (v: DocsValue) => void }) {
  const [q, setQ] = useState("");
  const people = useAccessDirectory(orgId, q, { types: "user", limit: 12 });
  const chosen = new Set(value);
  return (
    <div className="flex w-72 flex-col gap-1 p-1">
      <div className="p-1">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search people" aria-label="Search people" data-autofocus />
      </div>
      <div role="listbox" aria-multiselectable className="flex max-h-64 flex-col overflow-y-auto">
        {(people.data ?? []).map((p) => (
          <button key={p.id} type="button" role="option" aria-selected={chosen.has(p.id)} className={floatingItemClass} onClick={() => onChange(chosen.has(p.id) ? value.filter((x) => x !== p.id) : [...value, p.id])}>
            <span className="flex size-4 items-center justify-center text-primary-strong">{chosen.has(p.id) ? <Check aria-hidden /> : null}</span>
            <Avatar size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />
            <span className="min-w-0 flex-1 truncate">{p.name}</span>
          </button>
        ))}
        {people.isPending ? <p className="px-3 py-2 type-label text-fg-muted">Loading people…</p> : null}
        {!people.isPending && (people.data ?? []).length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No one matches.</p> : null}
      </div>
    </div>
  );
}
