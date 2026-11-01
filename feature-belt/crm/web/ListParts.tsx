"use client";

import {
  crmFilterOps,
  crmFilterReady,
  crmMoneyInput,
  crmOpNeedsValue,
  crmParseMoney,
  useAccessDirectory,
  useCrmPipelines,
  useCrmTags,
  type CrmFilter,
  type CrmFilterOp,
  type CrmObjectType,
  type CrmPipeline,
  type CrmProperty,
  type CrmTag,
} from "@scaffoldapp/shared";
import { Button, Checkbox, Dialog, Field, IconButton, Input, Select, cn } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, Plus, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { MemberPicker, RecordPicker } from "./parts";

/** Columns shown when a view names none. */
export const DEFAULT_COLUMNS: Record<CrmObjectType, string[]> = {
  company: ["name", "domain", "owner", "industry", "city", "last_activity_at"],
  contact: ["name", "emails", "company", "phones", "owner", "lifecycle_stage", "last_activity_at"],
  deal: ["name", "amount", "stage", "close_date", "owner", "company", "last_activity_at"],
};

/** Readable text of one filter for its chip. */
export function filterText(f: CrmFilter, props: CrmProperty[], ctx: { tags: CrmTag[]; pipelines: CrmPipeline[]; currency: string }): string {
  const p = props.find((x) => x.key === f.key);
  if (!p) return f.key;
  const op = crmFilterOps(p.type).find((o) => o.op === f.op)?.label ?? f.op;
  if (!crmOpNeedsValue(f.op)) return `${p.label} ${op}`;
  let v = "";
  const list = Array.isArray(f.value) ? f.value : [];
  switch (p.type) {
    case "select":
    case "multi_select":
      v = list.map((x) => p.options.find((o) => o.value === x)?.label ?? x).join(", ");
      break;
    case "tags":
      v = list.map((x) => ctx.tags.find((t) => t.id === x)?.name ?? "a tag").join(", ");
      break;
    case "stage":
      v = list.map((x) => ctx.pipelines.flatMap((pl) => pl.stages).find((s) => s.id === x)?.name ?? "a stage").join(", ");
      break;
    case "pipeline":
      v = list.map((x) => ctx.pipelines.find((pl) => pl.id === x)?.name ?? "a pipeline").join(", ");
      break;
    case "user":
      v = list.length === 1 && list[0] === "me" ? "me" : `${list.includes("me") ? "me + " : ""}${list.filter((x) => x !== "me").length} people`.replace(" + 0 people", "");
      break;
    case "team":
      v = `${list.length} team${list.length === 1 ? "" : "s"}`;
      break;
    case "company":
      v = `${list.length} compan${list.length === 1 ? "y" : "ies"}`;
      break;
    case "currency":
      v = typeof f.value === "number" ? crmMoneyInput(f.value, ctx.currency) : String(f.value ?? "");
      break;
    default:
      v = String(f.value ?? "");
      if (f.op === "last_days" || f.op === "next_days") return `${p.label} in the ${f.op === "last_days" ? "last" : "next"} ${v} days`;
  }
  return `${p.label} ${op} ${v}`;
}

/** Edit the list's conditions (all must hold). */
export function FilterDialog({
  open,
  onOpenChange,
  orgId,
  props,
  filters,
  currency,
  onApply,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  orgId: string;
  props: CrmProperty[];
  filters: CrmFilter[];
  currency: string;
  onApply: (f: CrmFilter[]) => void;
}) {
  const [draft, setDraft] = useState<CrmFilter[]>(filters);
  useEffect(() => {
    if (open) setDraft(filters.length > 0 ? filters : []);
  }, [open, filters]);
  const filterable = props.filter((p) => p.filterable);
  const add = () => {
    const p = filterable[0];
    if (!p) return;
    setDraft([...draft, { key: p.key, op: crmFilterOps(p.type)[0]?.op ?? "eq" }]);
  };
  const ready = draft.filter(crmFilterReady);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Filters"
      info="Records must match every condition."
      size="lg"
      footer={
        <>
          <Button variant="ghost" onClick={() => onApply([])} className="sm:mr-auto">
            Clear all
          </Button>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={() => onApply(ready)}>Apply{ready.length ? ` ${ready.length}` : ""}</Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {draft.length === 0 ? <p className="type-body text-fg-muted">No conditions. Every record you can see is listed.</p> : null}
        {draft.map((f, i) => (
          <FilterRow
            key={i}
            orgId={orgId}
            props={filterable}
            filter={f}
            currency={currency}
            onChange={(nf) => setDraft(draft.map((x, j) => (j === i ? nf : x)))}
            onRemove={() => setDraft(draft.filter((_, j) => j !== i))}
          />
        ))}
        <Button variant="secondary" size="sm" leftIcon={<Plus />} onClick={add} className="self-start">
          Add condition
        </Button>
      </div>
    </Dialog>
  );
}

function FilterRow({
  orgId,
  props,
  filter,
  currency,
  onChange,
  onRemove,
}: {
  orgId: string;
  props: CrmProperty[];
  filter: CrmFilter;
  currency: string;
  onChange: (f: CrmFilter) => void;
  onRemove: () => void;
}) {
  const p = props.find((x) => x.key === filter.key) ?? props[0];
  if (!p) return null;
  const ops = crmFilterOps(p.type);
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <div className="flex flex-col gap-2 sm:flex-row">
        <Select
          aria-label="Field"
          className="sm:w-48"
          value={p.key}
          onValueChange={(k) => {
            const np = props.find((x) => x.key === k);
            if (np) onChange({ key: k, op: crmFilterOps(np.type)[0]?.op ?? "eq" });
          }}
          options={props.map((x) => ({ value: x.key, label: x.label }))}
        />
        <Select aria-label="Condition" className="sm:w-52" value={filter.op} onValueChange={(op) => onChange({ key: filter.key, op: op as CrmFilterOp })} options={ops.map((o) => ({ value: o.op, label: o.label }))} />
        <IconButton label="Remove condition" icon={<Trash2 />} variant="ghost" onClick={onRemove} className="self-end sm:self-auto" />
      </div>
      {crmOpNeedsValue(filter.op) ? <ValueEditor key={`${p.key}:${filter.op}`} orgId={orgId} prop={p} filter={filter} currency={currency} onChange={onChange} /> : null}
    </div>
  );
}

function ValueEditor({ orgId, prop, filter, currency, onChange }: { orgId: string; prop: CrmProperty; filter: CrmFilter; currency: string; onChange: (f: CrmFilter) => void }) {
  const tags = useCrmTags(orgId);
  const pipelines = useCrmPipelines(orgId);
  const list = Array.isArray(filter.value) ? filter.value : [];
  const set = (value: CrmFilter["value"]) => onChange({ ...filter, ...(value === undefined ? {} : { value }) });
  const toggle = (v: string) => set(list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  if (filter.op === "last_days" || filter.op === "next_days") {
    return <Input aria-label="Days" type="number" min={0} max={3650} value={typeof filter.value === "number" ? filter.value : ""} onChange={(e) => set(e.target.value === "" ? undefined : Number(e.target.value))} placeholder="30" />;
  }
  switch (prop.type) {
    case "date":
    case "datetime":
      return <Input aria-label="Date" type="date" value={typeof filter.value === "string" ? filter.value : ""} onChange={(e) => set(e.target.value)} />;
    case "number":
      return <Input aria-label="Number" type="number" value={typeof filter.value === "number" ? filter.value : ""} onChange={(e) => set(e.target.value === "" ? undefined : Number(e.target.value))} />;
    case "currency":
      return (
        <Input
          aria-label={`Amount in ${currency}`}
          inputMode="decimal"
          defaultValue={typeof filter.value === "number" ? crmMoneyInput(filter.value, currency) : ""}
          onChange={(e) => {
            const v = crmParseMoney(e.target.value, currency);
            set(v === null ? undefined : v);
          }}
          trailing={<span className="px-2 type-caption">{currency}</span>}
        />
      );
    case "select":
    case "multi_select":
      return <Choices options={prop.options.map((o) => ({ value: o.value, label: o.label }))} selected={list} onToggle={toggle} />;
    case "tags":
      return <Choices options={(tags.data ?? []).map((t) => ({ value: t.id, label: t.name }))} selected={list} onToggle={toggle} />;
    case "stage":
      return (
        <Choices
          options={(pipelines.data ?? []).flatMap((pl) => pl.stages.map((s) => ({ value: s.id, label: (pipelines.data?.length ?? 0) > 1 ? `${pl.name} · ${s.name}` : s.name })))}
          selected={list}
          onToggle={toggle}
        />
      );
    case "pipeline":
      return <Choices options={(pipelines.data ?? []).map((pl) => ({ value: pl.id, label: pl.name }))} selected={list} onToggle={toggle} />;
    case "team":
      return <TeamChoices orgId={orgId} selected={list} onToggle={toggle} />;
    case "user":
      return <PeopleChoices orgId={orgId} selected={list} onChange={set} />;
    case "company":
      return <CompanyChoices orgId={orgId} selected={list} onChange={set} />;
    default:
      return <Input aria-label="Value" value={typeof filter.value === "string" ? filter.value : ""} onChange={(e) => set(e.target.value)} />;
  }
}

function Choices({ options, selected, onToggle }: { options: { value: string; label: string }[]; selected: string[]; onToggle: (v: string) => void }) {
  if (options.length === 0) return <p className="type-label text-fg-muted">Nothing to choose from yet.</p>;
  return (
    <div className="grid max-h-56 gap-1 overflow-y-auto sm:grid-cols-2">
      {options.map((o) => (
        <Checkbox key={o.value} label={o.label} checked={selected.includes(o.value)} onCheckedChange={() => onToggle(o.value)} />
      ))}
    </div>
  );
}

function TeamChoices({ orgId, selected, onToggle }: { orgId: string; selected: string[]; onToggle: (v: string) => void }) {
  const dir = useAccessDirectory(orgId, "", { types: "team", limit: 50 });
  return <Choices options={(dir.data ?? []).map((t) => ({ value: t.id, label: t.name }))} selected={selected} onToggle={onToggle} />;
}

function PeopleChoices({ orgId, selected, onChange }: { orgId: string; selected: string[]; onChange: (v: string[]) => void }) {
  const [names, setNames] = useState<Record<string, string>>({ me: "Me" });
  const [picking, setPicking] = useState(false);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        <Checkbox label="Me" checked={selected.includes("me")} onCheckedChange={(c) => onChange(c ? [...selected, "me"] : selected.filter((x) => x !== "me"))} />
        {selected
          .filter((x) => x !== "me")
          .map((id) => (
            <span key={id} className="inline-flex h-8 items-center gap-1 rounded-md border border-border pl-3 pr-1 type-label">
              {names[id] ?? "Person"}
              <IconButton label="Remove" size="sm" icon={<X />} onClick={() => onChange(selected.filter((x) => x !== id))} />
            </span>
          ))}
        <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={() => setPicking(!picking)}>
          Person
        </Button>
      </div>
      {picking ? (
        <MemberPicker
          orgId={orgId}
          label="Add a person"
          autoFocus
          onPick={(u) => {
            setNames({ ...names, [u.id]: u.name });
            if (!selected.includes(u.id)) onChange([...selected, u.id]);
            setPicking(false);
          }}
        />
      ) : null}
    </div>
  );
}

function CompanyChoices({ orgId, selected, onChange }: { orgId: string; selected: string[]; onChange: (v: string[]) => void }) {
  const [names, setNames] = useState<Record<string, string>>({});
  const exclude = useMemo(() => new Set(selected), [selected]);
  return (
    <div className="flex flex-col gap-2">
      {selected.length > 0 ? (
        <div className="flex flex-wrap gap-2">
          {selected.map((id) => (
            <span key={id} className="inline-flex h-8 items-center gap-1 rounded-md border border-border pl-3 pr-1 type-label">
              {names[id] ?? "Company"}
              <IconButton label="Remove" size="sm" icon={<X />} onClick={() => onChange(selected.filter((x) => x !== id))} />
            </span>
          ))}
        </div>
      ) : null}
      <RecordPicker
        orgId={orgId}
        types={["company"]}
        exclude={exclude}
        label="Add a company"
        onPick={(h) => {
          setNames({ ...names, [h.id]: h.title });
          onChange([...selected, h.id]);
        }}
      />
    </div>
  );
}

/** Choose and order the table's columns. */
export function ColumnDialog({
  open,
  onOpenChange,
  props,
  columns,
  onApply,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  props: CrmProperty[];
  columns: string[];
  onApply: (cols: string[]) => void;
}) {
  const [draft, setDraft] = useState<string[]>(columns);
  useEffect(() => {
    if (open) setDraft(columns);
  }, [open, columns]);
  const move = (i: number, d: -1 | 1) => {
    const next = [...draft];
    const j = i + d;
    if (j < 0 || j >= next.length) return;
    [next[i], next[j]] = [next[j] as string, next[i] as string];
    setDraft(next);
  };
  const shown = draft.map((k) => props.find((p) => p.key === k)).filter((p): p is CrmProperty => !!p);
  const hidden = props.filter((p) => !draft.includes(p.key));
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Columns"
      size="md"
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={draft.length === 0} onClick={() => onApply(draft)}>
            Apply
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-section">
        <section className="flex flex-col gap-2" aria-label="Shown columns">
          <h3 className="type-label font-semibold text-fg">Shown, in order</h3>
          <ul className="flex flex-col gap-1">
            {shown.map((p, i) => (
              <li key={p.key} className="flex min-h-control items-center gap-2 rounded-md bg-sunken pl-3 pr-1">
                <Checkbox aria-label={`Hide ${p.label}`} checked onCheckedChange={() => setDraft(draft.filter((k) => k !== p.key))} />
                <span className="min-w-0 flex-1 truncate type-label text-fg">{p.label}</span>
                <IconButton label={`Move ${p.label} up`} size="sm" icon={<ArrowUp />} disabled={i === 0} onClick={() => move(i, -1)} />
                <IconButton label={`Move ${p.label} down`} size="sm" icon={<ArrowDown />} disabled={i === shown.length - 1} onClick={() => move(i, 1)} />
              </li>
            ))}
          </ul>
        </section>
        <section className="flex flex-col gap-2" aria-label="Other columns">
          <h3 className="type-label font-semibold text-fg">More fields</h3>
          <div className={cn("grid gap-1 sm:grid-cols-2")}>
            {hidden.map((p) => (
              <Checkbox key={p.key} label={p.label} checked={false} onCheckedChange={() => setDraft([...draft, p.key])} />
            ))}
          </div>
        </section>
      </div>
    </Dialog>
  );
}

/** Name a view and choose whether everyone sees it. */
export function SaveViewDialog({
  open,
  onOpenChange,
  initial,
  saving,
  onSave,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  initial?: { name: string; shared: boolean };
  saving: boolean;
  onSave: (v: { name: string; shared: boolean }) => void;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [shared, setShared] = useState(initial?.shared ?? false);
  useEffect(() => {
    if (open) {
      setName(initial?.name ?? "");
      setShared(initial?.shared ?? false);
    }
  }, [open, initial]);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={initial ? "Rename view" : "Save view"}
      size="sm"
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button loading={saving} disabled={!name.trim()} onClick={() => onSave({ name: name.trim(), shared })}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Name" required>
          <Input value={name} maxLength={60} autoFocus onChange={(e) => setName(e.target.value)} placeholder="Closing this quarter" />
        </Field>
        <Checkbox label="Everyone who uses the CRM can use it" checked={shared} onCheckedChange={setShared} info="Shared views list the same filters for everyone; each person still sees only the records they can open." />
      </div>
    </Dialog>
  );
}
