"use client";

import {
  DOCS_ADDABLE_TYPES,
  DOCS_CURRENCIES,
  DOCS_PROPERTY_LABEL,
  DOCS_ROLLUP_LABEL,
  DOCS_STATUS_GROUP_LABEL,
  docsAggregations,
  docsFormatComputed,
  docsFormatDate,
  docsFormatUniqueID,
  docsTitle,
  docsValueType,
  errorMessage,
  isApiError,
  useDebouncedValue,
  useDocsCheckFormula,
  useDocsPage,
  useDocsPutSchema,
  useDocsSearch,
  type DocsButtonAction,
  type DocsFormulaCheck,
  type DocsOption,
  type DocsProperty,
  type DocsPropertyType,
  type DocsRollupFunction,
} from "@scaffoldapp/shared";
import { Button, Field, IconButton, Input, Select, Sheet, Spinner, Switch, TextArea, cn } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { newShortId, OPTION_COLORS, PROPERTY_ICON } from "../format";
import { newProperty } from "./panels";
import { OptionBadge, OptionEditor } from "./PropertyValue";

// ---- formats -------------------------------------------------------------------------

function currencyName(code: string): string {
  try {
    return new Intl.DisplayNames(undefined, { type: "currency" }).of(code.toUpperCase()) ?? code.toUpperCase();
  } catch {
    return code.toUpperCase();
  }
}

export function numberFormatOptions() {
  return [
    { value: "number", label: "Number" },
    { value: "number_commas", label: "Number with commas" },
    { value: "percent", label: "Percent" },
    ...Object.keys(DOCS_CURRENCIES)
      .map((c) => ({ value: c, label: `${currencyName(c)} (${c.toUpperCase()})` }))
      .sort((a, b) => a.label.localeCompare(b.label)),
  ];
}

const SAMPLE_DATE = `${new Date().getFullYear()}-11-20`;

function dateFormatOptions() {
  return (["", "full", "mdy", "dmy", "ymd", "relative"] as const).map((f) => ({
    value: f || "default",
    label: f === "relative" ? "Relative (Today, Tomorrow)" : docsFormatDate(SAMPLE_DATE, f),
  }));
}

function NumberFormatFields({ prop, onChange }: { prop: DocsProperty; onChange: (p: DocsProperty) => void }) {
  const display = prop.number_display ?? "";
  return (
    <div className="flex flex-col gap-3">
      <Field label="Number format" info="Currencies are stored in their smallest unit (cents), so sums stay exact.">
        <Select value={prop.number_format || "number"} options={numberFormatOptions()} onValueChange={(v) => onChange({ ...prop, number_format: v })} />
      </Field>
      <Field label="Decimal places">
        <Select
          value={prop.decimals === undefined ? "default" : String(prop.decimals)}
          options={[{ value: "default", label: "Default" }, ...Array.from({ length: 9 }, (_, i) => ({ value: String(i), label: String(i) }))]}
          onValueChange={(v) => {
            const next = { ...prop };
            if (v === "default") delete next.decimals;
            else next.decimals = Number(v);
            onChange(next);
          }}
        />
      </Field>
      <Field label="Show as">
        <Select
          value={display || "number"}
          options={[
            { value: "number", label: "Number" },
            { value: "bar", label: "Bar" },
            { value: "ring", label: "Ring" },
          ]}
          onValueChange={(v) => onChange({ ...prop, number_display: v === "number" ? "" : (v as "bar" | "ring"), ...(v !== "number" && !prop.number_max ? { number_max: 100 } : {}) })}
        />
      </Field>
      {display ? (
        <Field label="Full at" info="The value that fills the bar or ring.">
          <Input
            inputMode="decimal"
            defaultValue={String(prop.number_max ?? 100)}
            onBlur={(e) => {
              const n = Number(e.target.value.replace(/,/g, ""));
              if (Number.isFinite(n) && n > 0) onChange({ ...prop, number_max: n });
            }}
          />
        </Field>
      ) : null}
    </div>
  );
}

function DateFormatFields({ prop, onChange }: { prop: DocsProperty; onChange: (p: DocsProperty) => void }) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row">
      <Field label="Date format" className="flex-1">
        <Select value={prop.date_format || "default"} options={dateFormatOptions()} onValueChange={(v) => onChange({ ...prop, date_format: (v === "default" ? "" : v) as DocsProperty["date_format"] })} />
      </Field>
      <Field label="Time format" className="flex-1">
        <Select
          value={prop.time_format || "12h"}
          options={[
            { value: "12h", label: "12 hour (2:30 PM)" },
            { value: "24h", label: "24 hour (14:30)" },
          ]}
          onValueChange={(v) => onChange({ ...prop, time_format: v as "12h" | "24h" })}
        />
      </Field>
    </div>
  );
}

// ---- options ---------------------------------------------------------------------------

function OptionsFields({ prop, onChange }: { prop: DocsProperty; onChange: (p: DocsProperty) => void }) {
  const [open, setOpen] = useState<string | null>(null);
  const options = prop.options ?? [];
  const status = prop.type === "status";
  const setOption = (o: DocsOption) => onChange({ ...prop, options: options.map((x) => (x.id === o.id ? o : x)) });
  const add = (group?: "todo" | "doing" | "done") => {
    const o: DocsOption = { id: newShortId(), name: `Option ${options.length + 1}`, color: OPTION_COLORS[(options.length % (OPTION_COLORS.length - 1)) + 1]!, ...(group ? { group } : {}) };
    onChange({ ...prop, options: [...options, o] });
    setOpen(o.id);
  };
  const move = (id: string, d: -1 | 1) => {
    const i = options.findIndex((o) => o.id === id);
    const j = i + d;
    if (i < 0 || j < 0 || j >= options.length) return;
    const next = [...options];
    [next[i], next[j]] = [next[j]!, next[i]!];
    onChange({ ...prop, options: next });
  };
  const row = (o: DocsOption) => (
    <li key={o.id} className="flex flex-col">
      <div className="flex items-center gap-1">
        <button type="button" aria-expanded={open === o.id} onClick={() => setOpen((x) => (x === o.id ? null : o.id))} className="flex min-h-control min-w-0 flex-1 items-center gap-2 rounded-sm px-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
          {open === o.id ? <ChevronDown aria-hidden className="size-4 shrink-0 text-fg-muted" /> : <ChevronRight aria-hidden className="size-4 shrink-0 text-fg-muted" />}
          <OptionBadge option={o} status={status} />
        </button>
        {status ? (
          <Select
            aria-label={`Group of ${o.name}`}
            className="w-32"
            value={o.group ?? "todo"}
            options={(["todo", "doing", "done"] as const).map((g) => ({ value: g, label: DOCS_STATUS_GROUP_LABEL[g] }))}
            onValueChange={(g) => setOption({ ...o, group: g as DocsOption["group"] })}
          />
        ) : null}
        <IconButton label={`Move ${o.name} up`} icon={<ArrowUp />} size="sm" variant="ghost" onClick={() => move(o.id, -1)} />
        <IconButton label={`Move ${o.name} down`} icon={<ArrowDown />} size="sm" variant="ghost" onClick={() => move(o.id, 1)} />
      </div>
      {open === o.id ? <OptionEditor option={o} onChange={setOption} onDelete={() => onChange({ ...prop, options: options.filter((x) => x.id !== o.id) })} /> : null}
    </li>
  );
  if (status) {
    return (
      <div className="flex flex-col gap-3">
        {(["todo", "doing", "done"] as const).map((g) => (
          <section key={g} aria-label={DOCS_STATUS_GROUP_LABEL[g]} className="flex flex-col gap-1">
            <div className="flex items-center justify-between">
              <h4 className="type-label font-semibold text-fg-muted">{DOCS_STATUS_GROUP_LABEL[g]}</h4>
              <IconButton label={`Add an option to ${DOCS_STATUS_GROUP_LABEL[g]}`} icon={<Plus />} size="sm" variant="ghost" onClick={() => add(g)} />
            </div>
            <ul className="flex flex-col gap-0.5">{options.filter((o) => (o.group ?? "todo") === g).map(row)}</ul>
          </section>
        ))}
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      <h4 className="type-label font-semibold text-fg-muted">Options</h4>
      {options.length ? <ul className="flex flex-col gap-0.5">{options.map(row)}</ul> : <p className="type-label text-fg-muted">No options yet.</p>}
      <div>
        <Button type="button" size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => add()}>
          Add an option
        </Button>
      </div>
    </div>
  );
}

// ---- relation ---------------------------------------------------------------------------

function DatabasePicker({ orgId, value, selfId, onChange }: { orgId: string; value: string; selfId: string; onChange: (id: string, title: string) => void }) {
  const [q, setQ] = useState("");
  const hits = useDocsSearch(orgId, q, { kind: "database", limit: 12 });
  return (
    <div className="flex flex-col gap-2">
      <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search databases" aria-label="Search databases" />
      <div role="listbox" aria-label="Databases" className="flex max-h-56 flex-col overflow-y-auto rounded-md border border-border p-1">
        <button type="button" role="option" aria-selected={value === selfId} onClick={() => onChange(selfId, "This database")} className={cn("flex min-h-control items-center gap-2 rounded-sm px-2 text-left type-label hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", value === selfId && "bg-sunken font-semibold")}>
          This database
        </button>
        {(hits.data ?? [])
          .filter((h) => h.id !== selfId)
          .map((h) => (
            <button
              key={h.id}
              type="button"
              role="option"
              aria-selected={value === h.id}
              onClick={() => onChange(h.id, docsTitle(h.title))}
              className={cn("flex min-h-control items-center gap-2 rounded-sm px-2 text-left type-label hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", value === h.id && "bg-sunken font-semibold")}
            >
              {h.icon ? <span aria-hidden>{h.icon}</span> : null}
              <span className="min-w-0 flex-1 truncate">{docsTitle(h.title)}</span>
            </button>
          ))}
        {hits.isPending ? <p className="px-2 py-1 type-caption text-fg-muted">Searching…</p> : null}
        {hits.isError ? <p className="px-2 py-1 type-caption text-danger-strong">Couldn't search databases.</p> : null}
      </div>
    </div>
  );
}

function RelationFields({ prop, isNew, orgId, dbId, onChange }: { prop: DocsProperty; isNew: boolean; orgId: string; dbId: string; onChange: (p: DocsProperty) => void }) {
  const rel = prop.relation ?? { database_id: "" };
  const target = useDocsPage(rel.database_id && rel.database_id !== dbId ? rel.database_id : null);
  const [pickedTitle, setPickedTitle] = useState<string | null>(null);
  const targetTitle = rel.database_id === dbId ? "This database" : (pickedTitle ?? (target.data ? docsTitle(target.data.page.title) : target.isError ? "A database you can't open" : "…"));
  const set = (patch: Partial<NonNullable<DocsProperty["relation"]>>) => onChange({ ...prop, relation: { ...rel, ...patch } });
  return (
    <div className="flex flex-col gap-3">
      {isNew ? (
        <Field label="Related to">
          <DatabasePicker
            orgId={orgId}
            value={rel.database_id}
            selfId={dbId}
            onChange={(id, title) => {
              setPickedTitle(title);
              set({ database_id: id });
            }}
          />
        </Field>
      ) : (
        <Field label="Related to" info="The related database can't change; make a new relation instead.">
          <p className="flex min-h-control items-center type-body text-fg">{targetTitle}</p>
        </Field>
      )}
      <Field label="Limit">
        <Select
          value={rel.limit === 1 ? "1" : "0"}
          options={[
            { value: "0", label: "No limit" },
            { value: "1", label: "1 row" },
          ]}
          onValueChange={(v) => set({ limit: v === "1" ? 1 : 0 })}
        />
      </Field>
      {isNew ? (
        <>
          <Switch
            checked={!!rel.two_way}
            onCheckedChange={(on) => set({ two_way: on, ...(on && !rel.mirror_name ? { mirror_name: "Related" } : {}) })}
            label="Show on the related database"
            info="Adds a matching relation there, kept in sync both ways. You need edit access to that database."
          />
          {rel.two_way ? (
            <Field label="Name there">
              <Input value={rel.mirror_name ?? ""} maxLength={100} onChange={(e) => set({ mirror_name: e.target.value })} />
            </Field>
          ) : null}
        </>
      ) : rel.two_way || rel.mirror_property ? (
        <p className="type-label text-fg-muted">Shown on the related database too.</p>
      ) : null}
    </div>
  );
}

// ---- rollup ---------------------------------------------------------------------------

function RollupFields({ prop, schema, dbId, onChange }: { prop: DocsProperty; schema: DocsProperty[]; dbId: string; onChange: (p: DocsProperty) => void }) {
  const r = prop.rollup ?? { relation: "", property: "", function: "show_original" as DocsRollupFunction };
  const relations = schema.filter((p) => p.type === "relation" && p.id !== prop.id);
  const relProp = relations.find((p) => p.id === r.relation);
  const targetId = relProp?.relation?.database_id;
  const remote = useDocsPage(targetId && targetId !== dbId ? targetId : null);
  const targetSchema = targetId === dbId ? schema : (remote.data?.page.schema ?? []);
  const targetProp = targetSchema.find((p) => p.id === r.property);
  const fns = targetProp ? docsAggregations(docsValueType(targetProp)) : (["show_original"] as DocsRollupFunction[]);
  const set = (patch: Partial<NonNullable<DocsProperty["rollup"]>>) => onChange({ ...prop, rollup: { ...r, ...patch } });
  if (!relations.length) return <p className="type-label text-fg-muted">Add a relation first: a rollup reads the rows it links to.</p>;
  return (
    <div className="flex flex-col gap-3">
      <Field label="Relation">
        <Select value={r.relation} placeholder="Choose a relation" options={relations.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => set({ relation: v, property: "", function: "show_original" })} />
      </Field>
      <Field label="Property">
        {remote.isPending && targetId && targetId !== dbId ? (
          <p className="flex min-h-control items-center gap-2 type-label text-fg-muted">
            <Spinner /> Loading its properties…
          </p>
        ) : remote.isError ? (
          <p className="type-label text-fg-muted">You can't open the related database.</p>
        ) : (
          <Select
            value={r.property}
            placeholder="Choose a property"
            disabled={!r.relation}
            options={targetSchema.filter((p) => p.type !== "button").map((p) => ({ value: p.id, label: p.name }))}
            onValueChange={(v) => set({ property: v, function: "show_original" })}
          />
        )}
      </Field>
      <Field label="Calculate">
        <Select value={fns.includes(r.function) ? r.function : "show_original"} options={fns.map((f) => ({ value: f, label: DOCS_ROLLUP_LABEL[f] }))} onValueChange={(v) => set({ function: v as DocsRollupFunction })} />
      </Field>
      {["sum", "average", "median", "min", "max", "range"].includes(r.function) ? <NumberFormatFields prop={prop} onChange={onChange} /> : null}
      {["earliest_date", "latest_date", "show_original"].includes(r.function) && targetProp && docsValueType(targetProp) === "date" ? <DateFormatFields prop={prop} onChange={onChange} /> : null}
    </div>
  );
}

// ---- formula --------------------------------------------------------------------------

const FORMULA_FUNCTIONS: { group: string; items: [string, string][] }[] = [
  {
    group: "Logic",
    items: [
      ["if(condition, then, else)", "Returns then when the condition is true, else otherwise."],
      ["ifs(condition1, value1, …, else)", "The value of the first true condition."],
      ["and / or / not", "Combines or flips true and false."],
      ["empty(value)", "Whether a value is empty."],
      ["let(name, value, result)", "Names a value to use in the result."],
      ["lets(name1, value1, …, result)", "Names several values."],
    ],
  },
  {
    group: "Text",
    items: [
      ["concat(a, b, …)", "Joins text and lists (+ joins text too)."],
      ["length(text)", "Characters in text, or items in a list."],
      ["substring(text, start, end)", "Part of the text."],
      ["contains(text, search)", "Whether the text includes the search."],
      ["test(text, pattern)", "Whether the text matches a regular expression."],
      ["match(text, pattern)", "The parts that match."],
      ["replace(text, pattern, with)", "Replaces the first match."],
      ["replaceAll(text, pattern, with)", "Replaces every match."],
      ["lower(text) / upper(text) / trim(text)", "Changes case, trims spaces."],
      ["repeat(text, times)", "The text repeated."],
      ["padStart(text, length, pad) / padEnd", "Pads the text to a length."],
      ["split(text, separator)", "A list of the parts."],
      ["join(list, separator)", "A list as text."],
      ["format(value)", "Any value as text."],
      ["toNumber(text)", "Text as a number."],
      ["link(label, url)", "A link."],
      ["style(text, \"b\", \"i\", \"red\", …)", "Bold, italic, underline, code or a colour."],
      ["unstyle(text)", "Removes styles."],
    ],
  },
  {
    group: "Math",
    items: [
      ["round(number, places)", "Rounds to the places given (0 by default)."],
      ["ceil / floor / abs / sign", "Rounds up or down, absolute value, sign."],
      ["sqrt / cbrt / exp / ln / pow(a, b)", "Roots, powers and logarithms."],
      ["mod(a, b)", "The remainder of a ÷ b."],
      ["add / subtract / multiply / divide", "The same as + − × ÷."],
      ["min(…) / max(…)", "The smallest or largest number."],
      ["sum(…) / mean(…) / median(…)", "Totals and averages of numbers and lists."],
      ["pi() / e()", "The constants."],
    ],
  },
  {
    group: "Dates",
    items: [
      ["now() / today()", "The current date and time, or today."],
      ["dateAdd(date, amount, \"days\")", "Adds years, quarters, months, weeks, days, hours or minutes."],
      ["dateSubtract(date, amount, \"days\")", "Subtracts them."],
      ["dateBetween(a, b, \"days\")", "The time from b to a in a unit."],
      ["dateRange(start, end)", "A date range."],
      ["dateStart(range) / dateEnd(range)", "The start or end of a range."],
      ["minute / hour / day / date / week / month / year", "Parts of a date (day is the weekday)."],
      ["formatDate(date, \"MMMM D, YYYY\")", "A date as text."],
      ["parseDate(\"2026-11-20\")", "Text as a date."],
      ["timestamp(date) / fromTimestamp(ms)", "Milliseconds since 1970 and back."],
    ],
  },
  {
    group: "Lists",
    items: [
      ["at(list, index) / first(list) / last(list)", "One item."],
      ["slice(list, start, end)", "Part of the list."],
      ["sort(list) / reverse(list) / unique(list) / flat(list)", "Reorders, removes repeats, flattens."],
      ["includes(list, value)", "Whether the list holds the value."],
      ["filter(list, current > 1)", "Items for which the condition is true (current is each item)."],
      ["map(list, current * 2)", "Each item changed."],
      ["find / findIndex / some / every", "The first match, its place, any or all matching."],
    ],
  },
  {
    group: "People and pages",
    items: [
      ["name(person) / email(person)", "A person's name or email."],
      ["id(page)", "A page's or person's id."],
      ["prop(\"Name\")", "A property of this row."],
      ["current.prop(\"Name\")", "A property of each related row, inside map() or filter()."],
    ],
  },
];

/** Byte offset (the server's) → character index in the text. */
function charIndex(text: string, byte: number): number {
  let b = 0;
  let i = 0;
  for (const ch of text) {
    if (b >= byte) return i;
    b += new TextEncoder().encode(ch).length;
    i += ch.length;
  }
  return text.length;
}

function formulaTypeLabel(t: string | undefined): string {
  if (!t) return "";
  if (t.startsWith("list:")) return `List of ${formulaTypeLabel(t.slice(5)).toLowerCase()}`;
  return { number: "Number", text: "Text", boolean: "True or false", date: "Date", person: "Person", page: "Page", empty: "Empty" }[t] ?? t;
}

function FormulaEditor({ dbId, prop, schema, onChange, saveError }: { dbId: string; prop: DocsProperty; schema: DocsProperty[]; onChange: (p: DocsProperty) => void; saveError: { message: string; pos?: number } | null }) {
  const expr = prop.formula?.expression ?? "";
  const ref = useRef<HTMLTextAreaElement>(null);
  const check = useDocsCheckFormula(dbId);
  const debounced = useDebouncedValue(expr, 350);
  const [result, setResult] = useState<DocsFormulaCheck | null>(null);
  const [checkFailed, setCheckFailed] = useState(false);
  const [refOpen, setRefOpen] = useState<string | null>(null);
  const mutate = check.mutate;
  useEffect(() => {
    if (!debounced.trim()) {
      setResult(null);
      return;
    }
    mutate(
      { expression: debounced, property_id: prop.id },
      {
        onSuccess: (r) => {
          setResult(r);
          setCheckFailed(false);
        },
        onError: () => setCheckFailed(true),
      },
    );
  }, [debounced, prop.id, mutate]);
  const insert = (text: string) => {
    const el = ref.current;
    const at = el ? el.selectionStart : expr.length;
    const end = el ? el.selectionEnd : expr.length;
    const next = expr.slice(0, at) + text + expr.slice(end);
    onChange({ ...prop, formula: { expression: next } });
    requestAnimationFrame(() => {
      el?.focus();
      const caret = at + text.length - (text.endsWith(")") && text.includes("(") && !text.startsWith("prop(") ? 1 : 0);
      el?.setSelectionRange(caret, caret);
    });
  };
  const err = result?.error ?? (saveError?.pos !== undefined ? { pos: saveError.pos, message: saveError.message } : null);
  const errAt = err ? charIndex(expr, err.pos) : -1;
  const props = schema.filter((p) => p.id !== prop.id && p.type !== "button");
  const resultType = result?.type ?? prop.formula?.type;
  return (
    <div className="flex flex-col gap-3">
      <Field label="Formula" info={'Read properties with prop("Name"). Text goes in quotes; + − × ÷ and comparisons work as usual.'}>
        <TextArea
          ref={ref}
          value={expr}
          onChange={(e) => onChange({ ...prop, formula: { expression: e.target.value } })}
          minRows={3}
          maxRows={12}
          spellCheck={false}
          aria-invalid={!!err}
          className="font-mono type-label"
          placeholder={'if(prop("Done"), "✓", "")'}
        />
      </Field>
      <div aria-live="polite" className="flex min-h-6 flex-col gap-1">
        {check.isPending && expr.trim() ? (
          <p className="flex items-center gap-2 type-caption text-fg-muted">
            <Spinner /> Checking…
          </p>
        ) : err ? (
          <div className="flex flex-col gap-1">
            <p className="type-label text-danger-strong">{err.message}</p>
            {errAt >= 0 ? (
              <p className="overflow-x-auto whitespace-pre font-mono type-caption text-fg-muted">
                {expr.slice(Math.max(0, errAt - 30), errAt)}
                <mark className="rounded-xs bg-danger-soft px-0.5 text-danger-strong">{expr.slice(errAt, errAt + 1) || " "}</mark>
                {expr.slice(errAt + 1, errAt + 30)}
              </p>
            ) : null}
          </div>
        ) : checkFailed ? (
          <p className="type-caption text-fg-muted">Couldn't check the formula. It's checked again when you save.</p>
        ) : result && expr.trim() ? (
          <p className="flex flex-wrap items-center gap-2 type-label text-fg">
            <span className="rounded-sm bg-sunken px-1.5 type-caption text-fg-muted">{formulaTypeLabel(result.type)}</span>
            {result.preview !== null && result.preview !== undefined ? <span className="min-w-0 truncate">{docsFormatComputed(result.preview, { ...prop, formula: { expression: expr, ...(result.type ? { type: result.type } : {}) } }) || "Empty"}</span> : null}
          </p>
        ) : null}
      </div>
      <details className="rounded-md border border-border">
        <summary className="cursor-pointer select-none px-3 py-2 type-label font-medium text-fg">Properties and functions</summary>
        <div className="flex flex-col gap-3 border-t border-border p-3">
          <div className="flex flex-col gap-1">
            <h4 className="type-caption font-semibold text-fg-muted">Properties</h4>
            <div className="flex flex-wrap gap-1">
              {props.map((p) => {
                const Icon = PROPERTY_ICON[p.type];
                return (
                  <button key={p.id} type="button" onClick={() => insert(`prop("${p.name.replace(/"/g, '\\"')}")`)} className="inline-flex min-h-8 items-center gap-1.5 rounded-sm bg-sunken px-2 type-caption text-fg hover:bg-border focus-visible:outline-2 focus-visible:outline-ring">
                    <Icon aria-hidden className="size-3.5" />
                    {p.name}
                  </button>
                );
              })}
            </div>
          </div>
          {FORMULA_FUNCTIONS.map((g) => (
            <div key={g.group} className="flex flex-col">
              <button type="button" aria-expanded={refOpen === g.group} onClick={() => setRefOpen((x) => (x === g.group ? null : g.group))} className="flex min-h-8 items-center gap-1 rounded-sm text-left type-caption font-semibold text-fg-muted hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
                {refOpen === g.group ? <ChevronDown aria-hidden className="size-3.5" /> : <ChevronRight aria-hidden className="size-3.5" />}
                {g.group}
              </button>
              {refOpen === g.group ? (
                <dl className="flex flex-col gap-1 pl-5">
                  {g.items.map(([sig, desc]) => (
                    <div key={sig} className="flex flex-col">
                      <dt>
                        <button type="button" onClick={() => insert(sig.split(" ")[0]!.replace(/\(.*$/, "()"))} className="rounded-xs text-left font-mono type-caption text-primary-strong hover:underline focus-visible:outline-2 focus-visible:outline-ring">
                          {sig}
                        </button>
                      </dt>
                      <dd className="type-caption text-fg-muted">{desc}</dd>
                    </div>
                  ))}
                </dl>
              ) : null}
            </div>
          ))}
        </div>
      </details>
      {resultType === "number" ? <NumberFormatFields prop={prop} onChange={onChange} /> : null}
      {resultType === "date" ? <DateFormatFields prop={prop} onChange={onChange} /> : null}
    </div>
  );
}

// ---- button --------------------------------------------------------------------------

const DYNAMIC = [
  { value: "@me", label: "Me (who clicks)" },
  { value: "@today", label: "Today" },
  { value: "@now", label: "Now" },
];

/** A value a button or automation step sets: a fixed value, or "@me" / "@today" / "@now". */
export function StepValue({ prop, value, onChange }: { prop: DocsProperty | undefined; value: unknown; onChange: (v: unknown) => void }) {
  if (!prop) return null;
  switch (prop.type) {
    case "select":
    case "status":
      return <Select aria-label="Value" value={typeof value === "string" ? value : ""} placeholder="Choose an option" options={(prop.options ?? []).map((o) => ({ value: o.id, label: o.name }))} onValueChange={onChange} />;
    case "multi_select":
      return (
        <Select
          aria-label="Value"
          value={Array.isArray(value) && typeof value[0] === "string" ? value[0] : ""}
          placeholder="Choose an option"
          options={(prop.options ?? []).map((o) => ({ value: o.id, label: o.name }))}
          onValueChange={(v) => onChange([v])}
        />
      );
    case "checkbox":
      return (
        <Select
          aria-label="Value"
          value={value === false ? "false" : "true"}
          options={[
            { value: "true", label: "Checked" },
            { value: "false", label: "Unchecked" },
          ]}
          onValueChange={(v) => onChange(v === "true")}
        />
      );
    case "person":
      return <Select aria-label="Value" value={Array.isArray(value) && value[0] === "@me" ? "@me" : "@me"} options={[DYNAMIC[0]!]} onValueChange={(v) => onChange([v])} />;
    case "date": {
      const dyn = typeof value === "string" && value.startsWith("@") ? value : "";
      const exact = value && typeof value === "object" && "start" in (value as object) ? (value as { start: string }).start.slice(0, 10) : "";
      return (
        <div className="flex flex-wrap gap-2">
          <Select aria-label="Date" className="min-w-32 flex-1" value={dyn || "exact"} options={[...DYNAMIC.slice(1), { value: "exact", label: "Exact date" }]} onValueChange={(v) => onChange(v === "exact" ? { start: exact || new Date().toISOString().slice(0, 10) } : v)} />
          {!dyn ? <Input type="date" aria-label="Exact date" className="min-w-32 flex-1" value={exact} onChange={(e) => onChange(e.target.value ? { start: e.target.value } : null)} /> : null}
        </div>
      );
    }
    case "number":
      return <Input aria-label="Value" inputMode="decimal" defaultValue={typeof value === "number" ? String(value) : ""} onBlur={(e) => onChange(e.target.value.trim() === "" ? null : Number(e.target.value))} />;
  }
  return <Input aria-label="Value" defaultValue={typeof value === "string" ? value : ""} onBlur={(e) => onChange(e.target.value)} maxLength={2000} />;
}

export function settable(schema: DocsProperty[]): DocsProperty[] {
  return schema.filter((p) => ["title", "text", "number", "select", "multi_select", "status", "date", "person", "checkbox", "url", "email", "phone"].includes(p.type));
}

function ButtonFields({ prop, schema, orgId, dbId, onChange }: { prop: DocsProperty; schema: DocsProperty[]; orgId: string; dbId: string; onChange: (p: DocsProperty) => void }) {
  const b = prop.button ?? { label: prop.name, actions: [] };
  const set = (actions: DocsButtonAction[]) => onChange({ ...prop, button: { ...b, actions } });
  const setAt = (i: number, a: DocsButtonAction) => set(b.actions.map((x, j) => (j === i ? a : x)));
  const targets = settable(schema);
  return (
    <div className="flex flex-col gap-3">
      <Field label="Label">
        <Input value={b.label} maxLength={60} onChange={(e) => onChange({ ...prop, button: { ...b, label: e.target.value } })} />
      </Field>
      <div className="flex flex-col gap-2">
        <h4 className="type-label font-semibold text-fg-muted">When clicked</h4>
        {b.actions.length === 0 ? <p className="type-label text-fg-muted">No steps yet.</p> : null}
        <ol className="flex flex-col gap-2">
          {b.actions.map((a, i) => (
            <li key={i} className="flex flex-col gap-2 rounded-md border border-border p-2">
              <div className="flex items-center gap-2">
                <span className="type-caption tabular-nums text-fg-muted">{i + 1}</span>
                <Select
                  aria-label="Step"
                  className="flex-1"
                  value={a.type}
                  options={[
                    { value: "set_property", label: "Edit a property" },
                    { value: "add_row", label: "Add a row" },
                    { value: "open_url", label: "Open a link" },
                  ]}
                  onValueChange={(t) => setAt(i, { type: t as DocsButtonAction["type"] })}
                />
                <IconButton label="Remove step" icon={<Trash2 />} size="sm" variant="ghost" onClick={() => set(b.actions.filter((_, j) => j !== i))} />
              </div>
              {a.type === "set_property" ? (
                <div className="flex flex-col gap-2 sm:flex-row">
                  <Select aria-label="Property" className="sm:w-44" value={a.property ?? ""} placeholder="Property" options={targets.map((p) => ({ value: p.id, label: p.name }))} onValueChange={(v) => setAt(i, { type: "set_property", property: v })} />
                  <div className="min-w-0 flex-1">
                    <StepValue key={a.property} prop={schema.find((p) => p.id === a.property)} value={a.value} onChange={(v) => setAt(i, { ...a, value: v })} />
                  </div>
                </div>
              ) : null}
              {a.type === "add_row" ? (
                <AddRowTarget orgId={orgId} dbId={dbId} value={a.database_id ?? ""} onChange={(id) => setAt(i, { type: "add_row", database_id: id })} />
              ) : null}
              {a.type === "open_url" ? <Input aria-label="Link" type="url" placeholder="https://" defaultValue={a.url ?? ""} onBlur={(e) => setAt(i, { type: "open_url", url: e.target.value.trim() })} /> : null}
            </li>
          ))}
        </ol>
        {b.actions.length < 10 ? (
          <div>
            <Button type="button" size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => set([...b.actions, { type: "set_property" }])}>
              Add a step
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** Picks the database a step adds rows to. */
export function AddRowTarget({ orgId, dbId, value, onChange }: { orgId: string; dbId: string; value: string; onChange: (id: string) => void }) {
  const [open, setOpen] = useState(!value);
  const target = useDocsPage(value && value !== dbId ? value : null);
  const title = value === dbId ? "This database" : target.data ? docsTitle(target.data.page.title) : value ? "A database" : "";
  return open ? (
    <DatabasePicker
      orgId={orgId}
      value={value}
      selfId={dbId}
      onChange={(id) => {
        onChange(id);
        setOpen(false);
      }}
    />
  ) : (
    <div className="flex items-center gap-2">
      <span className="min-w-0 flex-1 truncate type-label text-fg">Adds a row to {title}</span>
      <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(true)}>
        Change
      </Button>
    </div>
  );
}

// ---- the sheet -------------------------------------------------------------------------

/** Settings a type keeps when another type is chosen (the rest start fresh). */
function retype(prop: DocsProperty, t: DocsPropertyType): DocsProperty {
  const fresh = newProperty(t, prop.name);
  const next: DocsProperty = { ...fresh, id: prop.id, name: prop.name, ...(prop.description ? { description: prop.description } : {}) };
  if ((t === "select" || t === "multi_select" || t === "status") && prop.options?.length) next.options = prop.options.map((o) => ({ ...o, ...(t === "status" ? { group: o.group ?? "todo" } : {}) }));
  return next;
}

export interface PropertySheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  dbId: string;
  orgId: string;
  schema: DocsProperty[];
  prop: DocsProperty | null;
  /** A property not in the schema yet: saving adds it. */
  isNew?: boolean;
  onSaved?: (p: DocsProperty) => void;
}

/** Every setting of one property: name, description, type and what the type needs. Saving replaces the schema. */
export function PropertySheet({ open, onOpenChange, dbId, orgId, schema, prop, isNew = false, onSaved }: PropertySheetProps) {
  const putSchema = useDocsPutSchema(dbId);
  const [draft, setDraft] = useState<DocsProperty | null>(prop);
  const [error, setError] = useState<{ message: string; pos?: number } | null>(null);
  useEffect(() => {
    if (open) {
      setDraft(prop);
      setError(null);
    }
  }, [open, prop]);
  const valid = useMemo(() => {
    if (!draft || !draft.name.trim()) return false;
    if (draft.type === "relation") return !!draft.relation?.database_id;
    if (draft.type === "rollup") return !!draft.rollup?.relation && !!draft.rollup.property;
    return true;
  }, [draft]);
  if (!draft) return null;
  const save = () => {
    const clean = { ...draft, name: draft.name.trim() };
    if (!clean.description?.trim()) delete clean.description;
    const next = isNew ? [...schema, clean] : schema.map((x) => (x.id === clean.id ? clean : x));
    setError(null);
    putSchema.mutate(next, {
      onSuccess: () => {
        onOpenChange(false);
        onSaved?.(clean);
      },
      onError: (e) => {
        const pos = isApiError(e) && typeof e.details?.["pos"] === "number" ? (e.details["pos"] as number) : undefined;
        setError({ message: errorMessage(e, "Couldn't save the property."), ...(pos !== undefined ? { pos } : {}) });
      },
    });
  };
  const t = draft.type;
  let body: ReactNode = null;
  if (t === "number") body = <NumberFormatFields prop={draft} onChange={setDraft} />;
  else if (t === "select" || t === "multi_select" || t === "status") body = <OptionsFields prop={draft} onChange={setDraft} />;
  else if (t === "date" || t === "created_time" || t === "last_edited_time") body = <DateFormatFields prop={draft} onChange={setDraft} />;
  else if (t === "formula") body = <FormulaEditor dbId={dbId} prop={draft} schema={schema} onChange={setDraft} saveError={error} />;
  else if (t === "relation") body = <RelationFields prop={draft} isNew={isNew || !schema.some((p) => p.id === draft.id && p.type === "relation")} orgId={orgId} dbId={dbId} onChange={setDraft} />;
  else if (t === "rollup") body = <RollupFields prop={draft} schema={schema} dbId={dbId} onChange={setDraft} />;
  else if (t === "button") body = <ButtonFields prop={draft} schema={schema} orgId={orgId} dbId={dbId} onChange={setDraft} />;
  else if (t === "unique_id")
    body = (
      <Field label="Prefix" info="Up to 10 letters or digits. Each new row gets the next number.">
        <Input
          value={draft.unique_id?.prefix ?? ""}
          maxLength={10}
          onChange={(e) => setDraft({ ...draft, unique_id: { prefix: e.target.value.replace(/[^A-Za-z0-9]/g, "").toUpperCase() } })}
          placeholder="TASK"
        />
        <p className="pt-1 type-caption text-fg-muted">Reads {docsFormatUniqueID(draft, 12)}</p>
      </Field>
    );
  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title={isNew ? "New property" : "Edit property"}
      size="md"
      footer={
        <div className="flex w-full flex-col gap-2">
          {error && !(t === "formula" && error.pos !== undefined) ? (
            <p role="alert" className="type-label text-danger-strong">
              {error.message}
            </p>
          ) : null}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button onClick={save} loading={putSchema.isPending} disabled={!valid}>
              {isNew ? "Add property" : "Save"}
            </Button>
          </div>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name">
          <Input value={draft.name} maxLength={100} onChange={(e) => setDraft({ ...draft, name: e.target.value })} data-autofocus />
        </Field>
        {draft.type !== "title" ? (
          <Field label="Type">
            <Select
              value={draft.type}
              options={DOCS_ADDABLE_TYPES.map((x) => {
                const Icon = PROPERTY_ICON[x];
                return {
                  value: x,
                  label: (
                    <span className="flex items-center gap-2">
                      <Icon aria-hidden className="size-4 text-fg-muted" />
                      {DOCS_PROPERTY_LABEL[x]}
                    </span>
                  ),
                };
              })}
              onValueChange={(x) => setDraft(retype(draft, x as DocsPropertyType))}
            />
          </Field>
        ) : null}
        <Field label="Description" info="Shown when people point at the property's name.">
          <TextArea value={draft.description ?? ""} maxLength={500} minRows={2} maxRows={5} onChange={(e) => setDraft({ ...draft, description: e.target.value })} />
        </Field>
        {body}
        {!isNew && schema.find((p) => p.id === draft.id)?.type !== draft.type ? (
          <p className="type-label text-fg-muted">Changing the type clears its values, except between text, URL, email and phone.</p>
        ) : null}
      </div>
    </Sheet>
  );
}
