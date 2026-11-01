"use client";

import {
  accessLevelAtLeast,
  crmFieldText,
  crmMoneyInput,
  crmParseMoney,
  errorMessage,
  useAccessDirectory,
  useCrmCreateTag,
  useCrmPipelines,
  useCrmProperties,
  useCrmTags,
  useCrmUpdateRecord,
  type CrmObjectType,
  type CrmProperty,
  type CrmRecord,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Checkbox, Dialog, IconButton, Input, Select, Skeleton, SkeletonGroup, TextArea, ToggleChip, toast } from "@scaffoldapp/ui-web";
import { Pencil, Plus, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { formatDateTime } from "@/lib/format";
import { useCrm } from "./CrmFrame";
import { MemberPicker, MoneyText, PersonChip, RecordLink, RecordPicker, StageBadge, TagList, dayLabel } from "./parts";

/** Read-only fields shown at the bottom. */
const SYSTEM = new Set(["created_at", "updated_at", "last_activity_at", "stage_entered_at", "closed_at", "probability", "weighted_amount", "name"]);

/** Which built-in fields a record page lists, in order (the rest are in the header or associations). */
const ORDER: Record<CrmObjectType, string[]> = {
  company: ["domain", "industry", "employees", "phone", "city", "country", "owner", "team", "tags", "description"],
  contact: ["first_name", "last_name", "emails", "phones", "job_title", "lifecycle_stage", "city", "country", "owner", "team", "tags"],
  deal: ["amount", "currency", "stage", "close_date", "company", "owner", "team", "tags"],
};

/** Properties on the left of a record page: every field, edited in place when you can edit. */
export function PropertiesPanel({ record }: { record: CrmRecord }) {
  const { orgId } = useCrm();
  const props = useCrmProperties(orgId, record.type);
  const editable = accessLevelAtLeast(record.my_level === "none" ? null : record.my_level, "edit");
  if (props.isPending)
    return (
      <Card padding="compact">
        <SkeletonGroup label="Loading fields" className="flex flex-col gap-4">
          {[0, 1, 2, 3, 4].map((i) => (
            <div key={i} className="flex flex-col gap-1">
              <Skeleton shape="text" className="h-3 w-20" />
              <Skeleton shape="text" className="h-4 w-40" />
            </div>
          ))}
        </SkeletonGroup>
      </Card>
    );
  const list = props.data ?? [];
  const builtin = ORDER[record.type].map((k) => list.find((p) => p.key === k)).filter((p): p is CrmProperty => !!p);
  const custom = list.filter((p) => !p.builtin);
  const system = list.filter((p) => SYSTEM.has(p.key) && p.key !== "name");
  return (
    <Card padding="compact" className="flex flex-col gap-4">
      <h2 className="type-title-sm text-fg">About this {record.type}</h2>
      <dl className="flex flex-col gap-3">
        {[...builtin, ...custom].map((p) => (
          <PropertyRow key={p.key} record={record} prop={p} editable={editable && p.editable} />
        ))}
      </dl>
      <dl className="flex flex-col gap-2 border-t border-border pt-3">
        {system.map((p) => {
          const v = (record as unknown as Record<string, unknown>)[p.key];
          const text = p.type === "datetime" ? (typeof v === "string" ? formatDateTime(v) : "") : crmFieldText(record, p);
          if (!text) return null;
          return (
            <div key={p.key} className="flex items-baseline justify-between gap-3">
              <dt className="type-caption text-fg-muted">{p.label}</dt>
              <dd className="text-right type-caption text-fg">{text}</dd>
            </div>
          );
        })}
      </dl>
    </Card>
  );
}

function PropertyRow({ record, prop, editable }: { record: CrmRecord; prop: CrmProperty; editable: boolean }) {
  const [editing, setEditing] = useState(false);
  const update = useCrmUpdateRecord(record.type, record.id);
  const save = (value: unknown) => {
    setEditing(false);
    update.mutate(
      { [prop.key]: value },
      { onError: (e) => toast.error(errorMessage(e, `Couldn't save ${prop.label.toLowerCase()}.`)) },
    );
  };
  const value = <PropertyValue record={record} prop={prop} />;
  const inlineToggle = prop.type === "checkbox";
  return (
    <div className="group flex flex-col gap-1">
      <dt className="type-caption text-fg-muted">{prop.label}</dt>
      <dd className="min-w-0">
        {editing ? (
          <PropertyEditor record={record} prop={prop} onSave={save} onCancel={() => setEditing(false)} />
        ) : inlineToggle ? (
          <Checkbox aria-label={prop.label} checked={record.props[prop.key] === true} disabled={!editable} onCheckedChange={(c) => save(c)} label={record.props[prop.key] ? "Yes" : "No"} />
        ) : editable ? (
          <button
            type="button"
            onClick={() => setEditing(true)}
            aria-label={`Edit ${prop.label}`}
            className="flex min-h-control w-full items-center gap-2 rounded-md px-2 -mx-2 text-left type-label text-fg hover:bg-sunken focus-visible:bg-sunken"
          >
            <span className="min-w-0 flex-1">{value}</span>
            <Pencil className="size-3.5 shrink-0 text-fg-subtle opacity-0 transition-opacity group-hover:opacity-100" aria-hidden />
          </button>
        ) : (
          <div className="flex min-h-control items-center type-label text-fg">{value}</div>
        )}
      </dd>
    </div>
  );
}

/** A field's value for display. */
export function PropertyValue({ record, prop }: { record: CrmRecord; prop: CrmProperty }): ReactNode {
  const { settings } = useCrm();
  const empty = <span className="text-fg-subtle">—</span>;
  switch (prop.key) {
    case "owner":
      return <PersonChip user={record.owner} />;
    case "team":
      return record.team ? <span className="truncate">{record.team.name}</span> : empty;
    case "tags":
      return record.tags.length ? <TagList tags={record.tags} max={8} /> : empty;
    case "stage":
      return record.type === "deal" ? <StageBadge name={record.stage.name} outcome={record.stage.outcome} /> : empty;
    case "amount":
      return record.type === "deal" ? <MoneyText minor={record.amount} currency={record.currency} className="font-semibold" /> : empty;
    case "company":
      return record.type === "deal" && record.company ? <RecordLink type="company" id={record.company.id} name={record.company.name} /> : empty;
    case "emails":
      return record.type === "contact" && record.emails.length ? (
        <span className="flex min-w-0 flex-col">
          {record.emails.map((e) => (
            <span key={e} className="truncate">
              {e}
            </span>
          ))}
        </span>
      ) : (
        empty
      );
    case "phones":
      return record.type === "contact" && record.phones.length ? (
        <span className="flex flex-col">
          {record.phones.map((p) => (
            <span key={p} className="tabular-nums">
              {p}
            </span>
          ))}
        </span>
      ) : (
        empty
      );
    case "description":
      return record.type === "company" && record.description ? <span className="whitespace-pre-wrap">{record.description}</span> : empty;
  }
  if (prop.type === "date") {
    const v = prop.builtin ? (record as unknown as Record<string, string | null>)[prop.key] : (record.props[prop.key] as string | undefined);
    return v ? <span className="tabular-nums">{dayLabel(v)}</span> : empty;
  }
  if (!prop.builtin && prop.type === "currency") {
    const v = record.props[prop.key];
    return typeof v === "number" ? <MoneyText minor={v} currency={settings.currency} /> : empty;
  }
  if (!prop.builtin && prop.type === "url") {
    const v = record.props[prop.key];
    return typeof v === "string" ? (
      <a href={v} target="_blank" rel="noreferrer noopener" className="truncate text-primary-strong underline-offset-2 hover:underline" onClick={(e) => e.stopPropagation()}>
        {v.replace(/^https?:\/\//, "")}
      </a>
    ) : (
      empty
    );
  }
  if (!prop.builtin && prop.type === "multi_select") {
    const v = record.props[prop.key];
    return Array.isArray(v) && v.length ? (
      <span className="flex flex-wrap gap-1">
        {v.map((x) => (
          <Badge key={x}>{prop.options.find((o) => o.value === x)?.label ?? x}</Badge>
        ))}
      </span>
    ) : (
      empty
    );
  }
  const text = crmFieldText(record, prop, { currency: settings.currency });
  return text ? <span className="truncate">{text}</span> : empty;
}

/** In-place editor for one field; Enter or blur saves, Escape cancels. */
function PropertyEditor({ record, prop, onSave, onCancel }: { record: CrmRecord; prop: CrmProperty; onSave: (v: unknown) => void; onCancel: () => void }) {
  const { orgId, settings } = useCrm();
  const r = record as unknown as Record<string, unknown>;
  const current = prop.builtin ? r[prop.key] : record.props[prop.key];
  const key = (e: React.KeyboardEvent, commit: () => void) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    } else if (e.key === "Enter" && !(e.target instanceof HTMLTextAreaElement)) {
      e.preventDefault();
      commit();
    }
  };
  switch (prop.type) {
    case "user":
      return <UserEditor orgId={orgId} label={prop.label} onSave={onSave} onCancel={onCancel} />;
    case "team":
      return <TeamEditor orgId={orgId} value={record.team?.id ?? ""} onSave={onSave} onCancel={onCancel} />;
    case "tags":
      return <TagEditor orgId={orgId} record={record} onSave={onSave} onCancel={onCancel} />;
    case "stage":
      return <StageEditor orgId={orgId} record={record} onSave={onSave} onCancel={onCancel} />;
    case "company":
      return <CompanyEditor orgId={orgId} onSave={onSave} onCancel={onCancel} />;
    case "select":
      return (
        <div className="flex items-center gap-2">
          <Select
            aria-label={prop.label}
            className="flex-1"
            value={typeof current === "string" ? current : undefined}
            onValueChange={(v) => onSave(v)}
            options={prop.options.map((o) => ({ value: o.value, label: o.label }))}
          />
          {!prop.builtin ? <IconButton label={`Clear ${prop.label}`} icon={<X />} size="sm" onClick={() => onSave(null)} /> : null}
          <Button variant="ghost" size="sm" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      );
    case "multi_select":
      return <MultiEditor prop={prop} value={Array.isArray(current) ? (current as string[]) : []} onSave={onSave} onCancel={onCancel} />;
    case "emails":
    case "phones":
      return <ListEditor prop={prop} value={Array.isArray(current) ? (current as string[]) : []} onSave={onSave} onCancel={onCancel} />;
    case "currency": {
      const cur = record.type === "deal" && prop.builtin ? record.currency : settings.currency;
      return <TextEditor initial={crmMoneyInput(typeof current === "number" ? current : null, cur)} label={prop.label} inputMode="decimal" suffix={cur} onKey={key} onCancel={onCancel} onSave={(t) => (t.trim() === "" ? onSave(null) : (() => {
        const v = crmParseMoney(t, cur);
        if (v === null) toast.error("Enter the amount as a number, like 12500.");
        else onSave(v);
      })())} />;
    }
    case "number":
      return <TextEditor initial={current === null || current === undefined ? "" : String(current)} label={prop.label} type="number" onKey={key} onCancel={onCancel} onSave={(t) => onSave(t.trim() === "" ? null : Number(t))} />;
    case "date":
      return <TextEditor initial={typeof current === "string" ? current : ""} label={prop.label} type="date" onKey={key} onCancel={onCancel} onSave={(t) => onSave(t || null)} />;
    case "long_text":
      return <TextEditor initial={typeof current === "string" ? current : ""} label={prop.label} multiline onKey={key} onCancel={onCancel} onSave={(t) => onSave(t)} />;
    default:
      return (
        <TextEditor
          initial={typeof current === "string" ? current : current === null || current === undefined ? "" : String(current)}
          label={prop.label}
          type={prop.type === "email" ? "email" : prop.type === "url" ? "url" : "text"}
          onKey={key}
          onCancel={onCancel}
          onSave={(t) => onSave(prop.builtin ? t : t.trim() === "" ? null : t)}
        />
      );
  }
}

function TextEditor({
  initial,
  label,
  type = "text",
  inputMode,
  multiline,
  suffix,
  onKey,
  onSave,
  onCancel,
}: {
  initial: string;
  label: string;
  type?: string;
  inputMode?: "decimal";
  multiline?: boolean;
  suffix?: string;
  onKey: (e: React.KeyboardEvent, commit: () => void) => void;
  onSave: (t: string) => void;
  onCancel: () => void;
}) {
  const [v, setV] = useState(initial);
  const done = useRef(false);
  const commit = () => {
    if (done.current) return;
    done.current = true;
    if (v === initial) onCancel();
    else onSave(v);
  };
  if (multiline) {
    return (
      <div className="flex flex-col gap-2">
        <TextArea aria-label={label} autoFocus value={v} onChange={(e) => setV(e.target.value)} onKeyDown={(e) => onKey(e, commit)} />
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onCancel}>
            Cancel
          </Button>
          <Button size="sm" onClick={commit}>
            Save
          </Button>
        </div>
      </div>
    );
  }
  return (
    <Input
      aria-label={label}
      autoFocus
      type={type}
      {...(inputMode ? { inputMode } : {})}
      value={v}
      onChange={(e) => setV(e.target.value)}
      onKeyDown={(e) => onKey(e, commit)}
      onBlur={commit}
      {...(suffix ? { trailing: <span className="px-2 type-caption">{suffix}</span> } : {})}
    />
  );
}

function UserEditor({ orgId, label, onSave, onCancel }: { orgId: string; label: string; onSave: (v: unknown) => void; onCancel: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onCancel()} title={label} size="sm">
      <div className="flex flex-col gap-4">
        <MemberPicker orgId={orgId} label={`Choose ${label.toLowerCase()}`} autoFocus onPick={(u) => onSave(u.id)} />
        <Button variant="ghost" onClick={() => onSave(null)}>
          Clear
        </Button>
      </div>
    </Dialog>
  );
}

function TeamEditor({ orgId, value, onSave, onCancel }: { orgId: string; value: string; onSave: (v: unknown) => void; onCancel: () => void }) {
  const dir = useAccessDirectory(orgId, "", { types: "team", limit: 50 });
  return (
    <div className="flex items-center gap-2">
      <Select
        aria-label="Team"
        className="flex-1"
        value={value || "none"}
        onValueChange={(v) => onSave(v === "none" ? null : v)}
        options={[{ value: "none", label: "No team" }, ...(dir.data ?? []).map((t) => ({ value: t.id, label: t.name }))]}
      />
      <Button variant="ghost" size="sm" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  );
}

function StageEditor({ orgId, record, onSave, onCancel }: { orgId: string; record: CrmRecord; onSave: (v: unknown) => void; onCancel: () => void }) {
  const pipelines = useCrmPipelines(orgId);
  if (record.type !== "deal") return null;
  return (
    <div className="flex items-center gap-2">
      <Select
        aria-label="Stage"
        className="flex-1"
        value={record.stage.id}
        onValueChange={(v) => onSave(v)}
        options={(pipelines.data ?? []).flatMap((p) => p.stages.map((s) => ({ value: s.id, label: (pipelines.data?.length ?? 0) > 1 ? `${p.name} · ${s.name}` : s.name })))}
      />
      <Button variant="ghost" size="sm" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  );
}

function CompanyEditor({ orgId, onSave, onCancel }: { orgId: string; onSave: (v: unknown) => void; onCancel: () => void }) {
  return (
    <Dialog open onOpenChange={(o) => !o && onCancel()} title="Company" size="md">
      <div className="flex flex-col gap-4">
        <RecordPicker orgId={orgId} types={["company"]} label="Find a company" autoFocus onPick={(h) => onSave(h.id)} />
        <Button variant="ghost" onClick={() => onSave(null)}>
          Remove the company
        </Button>
      </div>
    </Dialog>
  );
}

function MultiEditor({ prop, value, onSave, onCancel }: { prop: CrmProperty; value: string[]; onSave: (v: unknown) => void; onCancel: () => void }) {
  const [sel, setSel] = useState<string[]>(value);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        {prop.options.map((o) => (
          <ToggleChip key={o.value} size="sm" pressed={sel.includes(o.value)} onPressedChange={(p) => setSel(p ? [...sel, o.value] : sel.filter((x) => x !== o.value))}>
            {o.label}
          </ToggleChip>
        ))}
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => onSave(sel.length ? sel : null)}>
          Save
        </Button>
      </div>
    </div>
  );
}

function ListEditor({ prop, value, onSave, onCancel }: { prop: CrmProperty; value: string[]; onSave: (v: unknown) => void; onCancel: () => void }) {
  const [items, setItems] = useState<string[]>(value.length ? value : [""]);
  return (
    <div className="flex flex-col gap-2">
      {items.map((v, i) => (
        <div key={i} className="flex items-center gap-2">
          <Input
            aria-label={`${prop.label} ${i + 1}`}
            autoFocus={i === items.length - 1}
            type={prop.type === "emails" ? "email" : "tel"}
            value={v}
            onChange={(e) => setItems(items.map((x, j) => (j === i ? e.target.value : x)))}
            onKeyDown={(e) => {
              if (e.key === "Escape") onCancel();
              if (e.key === "Enter") onSave(items.map((x) => x.trim()).filter(Boolean));
            }}
            wrapperClassName="flex-1"
          />
          <IconButton label="Remove" size="sm" icon={<X />} onClick={() => setItems(items.filter((_, j) => j !== i))} />
        </div>
      ))}
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={() => setItems([...items, ""])}>
          Add
        </Button>
        <span className="ml-auto" />
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => onSave(items.map((x) => x.trim()).filter(Boolean))}>
          Save
        </Button>
      </div>
    </div>
  );
}

function TagEditor({ orgId, record, onSave, onCancel }: { orgId: string; record: CrmRecord; onSave: (v: unknown) => void; onCancel: () => void }) {
  const tags = useCrmTags(orgId);
  const create = useCrmCreateTag(orgId);
  const [sel, setSel] = useState<string[]>(record.tags.map((t) => t.id));
  const [name, setName] = useState("");
  useEffect(() => setSel(record.tags.map((t) => t.id)), [record.tags]);
  const add = () => {
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim() },
      {
        onSuccess: (t) => {
          setSel((s) => (s.includes(t.id) ? s : [...s, t.id]));
          setName("");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't add the tag.")),
      },
    );
  };
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap gap-2">
        {(tags.data ?? []).map((t) => (
          <ToggleChip key={t.id} size="sm" pressed={sel.includes(t.id)} onPressedChange={(p) => setSel(p ? [...sel, t.id] : sel.filter((x) => x !== t.id))}>
            {t.name}
          </ToggleChip>
        ))}
      </div>
      <div className="flex items-center gap-2">
        <Input
          aria-label="New tag"
          placeholder="New tag"
          value={name}
          maxLength={40}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add();
            }
          }}
          wrapperClassName="flex-1"
        />
        <Button variant="secondary" size="sm" loading={create.isPending} onClick={add}>
          Add
        </Button>
      </div>
      <div className="flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => onSave(sel)}>
          Save
        </Button>
      </div>
    </div>
  );
}
