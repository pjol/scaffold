"use client";

import {
  DOCS_OPTION_COLOR_LABEL,
  DOCS_STATUS_GROUP_LABEL,
  docsCurrencyCode,
  docsFormatComputed,
  docsFormatDateValue,
  docsFormatPropertyNumber,
  docsFormatUniqueID,
  docsFormatValue,
  docsNumberFill,
  docsNumberScale,
  docsOption,
  docsSafeLink,
  docsTimeZone,
  docsTitle,
  errorMessage,
  useAccessDirectory,
  useDebouncedValue,
  useDocsFileUrl,
  useDocsRows,
  useDocsUploadFile,
  type DocsComputedValue,
  type DocsDateValue,
  type DocsFileValue,
  type DocsOption,
  type DocsOptionColor,
  type DocsProperty,
  type DocsRefs,
  type DocsReminder,
  type DocsRow,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Avatar, Button, FileIcon, FilePreviewDialog, IconButton, Input, Select, Spinner, Switch, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Bell, Check, Ellipsis, ExternalLink, Link2, Lock, Plus, Trash2, Upload, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { floatingItemClass } from "../Floating";
import { newShortId, OPTION_COLORS, swatchFor } from "../format";

type RowStamp = Pick<DocsRow, "created_at" | "updated_at" | "created_by" | "updated_by">;

const EMPTY_REFS: DocsRefs = { users: {}, pages: {} };

/** Types whose display holds links (shown beside an edit button, never inside one). */
export function hasLinks(p: DocsProperty): boolean {
  return p.type === "url" || p.type === "email" || p.type === "phone" || p.type === "relation" || p.type === "files";
}

/** Whether a stored value is empty. */
export function isEmptyValue(v: DocsValue | undefined): boolean {
  return v === null || v === undefined || v === "" || (Array.isArray(v) && v.length === 0);
}

// ---- displays ---------------------------------------------------------------------------

/**
 * A row value as it reads in a cell, card or the properties panel. `links` renders URLs,
 * related rows and files as links (only where the value isn't inside a button). `row`
 * supplies created/edited time and people.
 */
export function PropertyDisplay({
  prop,
  value,
  refs = EMPTY_REFS,
  compact,
  row,
  links,
  token,
}: {
  prop: DocsProperty;
  value: DocsValue | undefined;
  refs?: DocsRefs;
  compact?: boolean;
  row?: RowStamp | undefined;
  links?: boolean;
  token?: string | null;
}) {
  switch (prop.type) {
    case "created_time":
    case "last_edited_time": {
      const s = row ? docsFormatValue(prop, null, refs, undefined, row) : "";
      return s ? <span className={cn("type-label text-fg", compact && "truncate")}>{s}</span> : <EmptyMark compact={compact} />;
    }
    case "created_by":
    case "last_edited_by": {
      const id = prop.type === "created_by" ? row?.created_by : row?.updated_by;
      return id ? <People ids={[id]} refs={refs} compact={false} /> : <EmptyMark compact={compact} />;
    }
    case "button":
      return (
        <span className="inline-flex min-h-6 items-center gap-1 rounded-sm border border-border px-2 type-caption text-fg">
          <span className="truncate">{prop.button?.label || prop.name}</span>
        </span>
      );
  }
  if (isEmptyValue(value)) {
    if (prop.type === "checkbox") return <CheckMark on={false} />;
    return <EmptyMark compact={compact} />;
  }
  switch (prop.type) {
    case "select":
    case "status": {
      const o = docsOption(prop, value);
      return o ? <OptionBadge option={o} status={prop.type === "status"} /> : null;
    }
    case "multi_select":
      return (
        <span className={cn("flex gap-1", compact ? "min-w-0 overflow-hidden" : "flex-wrap")}>
          {(value as string[]).map((id) => {
            const o = docsOption(prop, id);
            return o ? <OptionBadge key={id} option={o} /> : null;
          })}
        </span>
      );
    case "person":
      return <People ids={value as string[]} refs={refs} compact={!!compact} />;
    case "checkbox":
      return <CheckMark on={value === true} />;
    case "number":
      return typeof value === "number" ? <NumberValue prop={prop} value={value} stored /> : null;
    case "unique_id":
      return <span className="type-label tabular-nums text-fg">{docsFormatUniqueID(prop, value)}</span>;
    case "date":
      return <DateValue prop={prop} value={value as DocsDateValue} compact={compact} />;
    case "url": {
      const href = docsSafeLink(String(value));
      const text = String(value).replace(/^https?:\/\//, "").replace(/\/$/, "");
      return links && href ? (
        <a href={href} target="_blank" rel="noopener noreferrer nofollow" className="min-w-0 truncate type-label text-primary-strong underline underline-offset-2">
          {text}
        </a>
      ) : (
        <span className="min-w-0 truncate type-label text-fg underline decoration-border-strong underline-offset-2">{text}</span>
      );
    }
    case "email":
      return links ? (
        <a href={`mailto:${String(value)}`} className="min-w-0 truncate type-label text-primary-strong underline underline-offset-2">
          {String(value)}
        </a>
      ) : (
        <span className="min-w-0 truncate type-label text-fg">{String(value)}</span>
      );
    case "phone":
      return links ? (
        <a href={`tel:${String(value).replace(/[^0-9+]/g, "")}`} className="min-w-0 truncate type-label tabular-nums text-primary-strong underline underline-offset-2">
          {String(value)}
        </a>
      ) : (
        <span className="min-w-0 truncate type-label tabular-nums text-fg">{String(value)}</span>
      );
    case "relation":
      return <RelationChips ids={value as string[]} refs={refs} links={!!links} token={token} compact={compact} />;
    case "files":
      return <FileChips files={value as DocsFileValue[]} links={!!links} token={token} compact={compact} />;
    case "formula":
    case "rollup":
      return <ComputedValue prop={prop} value={value as DocsComputedValue} refs={refs} links={!!links} token={token} compact={compact} />;
    case "text":
    case "title":
    default:
      return <span className={cn("type-label text-fg", compact ? "truncate" : "whitespace-pre-wrap break-words")}>{String(value)}</span>;
  }
}

function EmptyMark({ compact }: { compact?: boolean | undefined }) {
  return compact ? null : <span className="type-label text-fg-subtle">Empty</span>;
}

/** An option chip in its swatch colour; status options lead with a dot. */
export function OptionBadge({ option, status }: { option: Pick<DocsOption, "name" | "color">; status?: boolean }) {
  const s = swatchFor(option.color);
  return (
    <span className={cn("inline-flex min-h-6 max-w-full items-center gap-1.5 rounded-sm px-1.5 type-caption font-medium", s.bg, s.fg)}>
      {status ? <span aria-hidden className={cn("size-2 shrink-0 rounded-pill", s.dot)} /> : null}
      <span className="truncate">{option.name}</span>
    </span>
  );
}

/** Picks one of the option colours (shown as swatches). */
export function SwatchPicker({ value, onChange, label = "Colour" }: { value: DocsOptionColor | undefined; onChange: (c: DocsOptionColor) => void; label?: string }) {
  const current = value === "neutral" ? "default" : (value ?? "default");
  return (
    <div role="radiogroup" aria-label={label} className="grid grid-cols-5 gap-1">
      {OPTION_COLORS.map((c) => {
        const s = swatchFor(c);
        const on = current === c;
        return (
          <button
            key={c}
            type="button"
            role="radio"
            aria-checked={on}
            aria-label={DOCS_OPTION_COLOR_LABEL[c] ?? c}
            title={DOCS_OPTION_COLOR_LABEL[c] ?? c}
            onClick={() => onChange(c)}
            className={cn("flex h-8 items-center justify-center rounded-sm border focus-visible:outline-2 focus-visible:outline-ring", s.bg, s.fg, on ? "border-fg" : "border-transparent")}
          >
            {on ? <Check aria-hidden className="size-4" /> : <span aria-hidden className="type-caption font-semibold">A</span>}
          </button>
        );
      })}
    </div>
  );
}

export function CheckMark({ on }: { on: boolean }) {
  return (
    <span role="img" aria-label={on ? "Checked" : "Not checked"} className={cn("flex size-4 shrink-0 items-center justify-center rounded-xs border [&_svg]:size-3", on ? "border-primary bg-primary text-primary-fg" : "border-border-strong bg-surface")}>
      {on ? <Check aria-hidden strokeWidth={3} /> : null}
    </span>
  );
}

function People({ ids, refs, compact }: { ids: string[]; refs: DocsRefs; compact: boolean }) {
  const many = ids.length > 1;
  return (
    <span className={cn("flex min-w-0 items-center gap-2", !compact && "flex-wrap")}>
      {ids.map((id) => {
        const u = refs.users[id];
        const name = u?.name ?? "Deleted user";
        return (
          <span key={id} className="flex min-w-0 items-center gap-1.5" title={name}>
            <Avatar size="xs" name={name} {...(u?.avatar_url ? { src: u.avatar_url } : {})} />
            {compact && many ? <span className="sr-only">{name}</span> : <span className="truncate type-label text-fg">{name}</span>}
          </span>
        );
      })}
    </span>
  );
}

/** A number with its format; bar and ring displays fill toward the property's maximum. */
export function NumberValue({ prop, value, stored }: { prop: DocsProperty; value: number; stored: boolean }) {
  const label = docsFormatPropertyNumber(prop, value, stored);
  if (prop.number_display === "bar" || prop.number_display === "ring") {
    const fill = docsNumberFill(prop, value, stored);
    if (prop.number_display === "ring") {
      const r = 7;
      const c = 2 * Math.PI * r;
      return (
        <span className="flex items-center gap-2">
          <svg aria-hidden viewBox="0 0 18 18" className="size-4 shrink-0 -rotate-90">
            <circle cx="9" cy="9" r={r} fill="none" strokeWidth="3" className="stroke-sunken" />
            <circle cx="9" cy="9" r={r} fill="none" strokeWidth="3" strokeLinecap="round" strokeDasharray={`${fill * c} ${c}`} className="stroke-primary" />
          </svg>
          <span className="type-label tabular-nums text-fg">{label}</span>
        </span>
      );
    }
    return (
      <span className="flex w-full min-w-16 items-center gap-2">
        <span aria-hidden className="h-1.5 min-w-8 flex-1 overflow-hidden rounded-pill bg-sunken">
          <span className="block h-full rounded-pill bg-primary" style={{ width: `${Math.round(fill * 100)}%` }} />
        </span>
        <span className="shrink-0 type-label tabular-nums text-fg">{label}</span>
      </span>
    );
  }
  return <span className="type-label tabular-nums text-fg">{label}</span>;
}

function DateValue({ prop, value, compact }: { prop: DocsProperty; value: DocsDateValue; compact?: boolean | undefined }) {
  const text = docsFormatDateValue(value, prop);
  const tz = value.tz && value.start.length > 10 && value.tz !== docsTimeZone() ? value.tz : null;
  return (
    <span className={cn("flex min-w-0 items-center gap-1.5 type-label text-fg", compact && "whitespace-nowrap")}>
      <span className={cn(compact && "truncate")}>{text}</span>
      {tz ? <span className="shrink-0 type-caption text-fg-muted">{tz.split("/").pop()?.replace(/_/g, " ")}</span> : null}
      {value.remind ? <Bell aria-label={`Reminder: ${REMINDER_LABEL[value.remind]}`} className="size-3.5 shrink-0 text-fg-muted" /> : null}
    </span>
  );
}

function pageHref(id: string, token?: string | null): string {
  return token ? `/p/${token}/${id}` : `/docs/${id}`;
}

/** Related rows: titles (links where allowed), "No access" for rows the viewer can't open; rows that are gone are left out. */
export function RelationChips({ ids, refs, links, token, compact }: { ids: string[]; refs: DocsRefs; links: boolean; token?: string | null | undefined; compact?: boolean | undefined }) {
  const shown = ids.filter((id) => refs.pages[id] && !refs.pages[id]!.deleted);
  if (!shown.length) return compact ? null : <span className="type-label text-fg-subtle">Empty</span>;
  return (
    <span className={cn("flex gap-1", compact ? "min-w-0 overflow-hidden" : "flex-wrap")}>
      {shown.map((id) => {
        const ref = refs.pages[id]!;
        if (ref.title === null) {
          return (
            <span key={id} className="inline-flex min-h-6 shrink-0 items-center gap-1 rounded-sm bg-sunken px-1.5 type-caption text-fg-muted">
              <Lock aria-hidden className="size-3" />
              No access
            </span>
          );
        }
        const inner = (
          <>
            {ref.icon ? <span aria-hidden>{ref.icon}</span> : null}
            <span className="truncate">{docsTitle(ref.title)}</span>
          </>
        );
        return links ? (
          <Link
            key={id}
            href={pageHref(id, token)}
            className="inline-flex min-h-6 max-w-60 shrink-0 items-center gap-1 rounded-sm px-1 type-label text-fg underline decoration-border-strong underline-offset-4 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
          >
            {inner}
          </Link>
        ) : (
          <span key={id} className="inline-flex min-h-6 max-w-60 shrink-0 items-center gap-1 type-label text-fg underline decoration-border-strong underline-offset-4">
            {inner}
          </span>
        );
      })}
    </span>
  );
}

/** One uploaded file: opens the preview (the link is fetched when it opens). */
function FileChip({ file, token }: { file: DocsFileValue; token?: string | null | undefined }) {
  const [open, setOpen] = useState(false);
  const url = useDocsFileUrl(open && file.file_id ? file.file_id : null, token);
  return (
    <>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setOpen(true);
        }}
        className="inline-flex min-h-6 max-w-48 shrink-0 items-center gap-1 rounded-sm bg-sunken px-1.5 type-caption text-fg hover:bg-border focus-visible:outline-2 focus-visible:outline-ring"
      >
        <FileIcon name={file.name} {...(file.mime ? { mime: file.mime } : {})} />
        <span className="truncate">{file.name}</span>
      </button>
      {open ? (
        url.data?.url ? (
          <FilePreviewDialog open onOpenChange={setOpen} url={url.data.url} name={file.name} mime={file.mime ?? url.data.content_type} size={url.data.size} downloadUrl={url.data.url} />
        ) : url.isError ? (
          <FileError onClose={() => setOpen(false)} />
        ) : null
      ) : null}
    </>
  );
}

function FileError({ onClose }: { onClose: () => void }) {
  useEffect(() => {
    toast.error("Couldn't open that file.");
    onClose();
    // Once, when the link fails.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
}

export function FileChips({ files, links, token, compact }: { files: DocsFileValue[]; links: boolean; token?: string | null | undefined; compact?: boolean | undefined }) {
  return (
    <span className={cn("flex gap-1", compact ? "min-w-0 overflow-hidden" : "flex-wrap")}>
      {files.map((f, i) =>
        f.url ? (
          links ? (
            <a
              key={i}
              href={docsSafeLink(f.url) ?? "#"}
              target="_blank"
              rel="noopener noreferrer nofollow"
              className="inline-flex min-h-6 max-w-48 shrink-0 items-center gap-1 rounded-sm bg-sunken px-1.5 type-caption text-fg hover:bg-border focus-visible:outline-2 focus-visible:outline-ring"
            >
              <Link2 aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{f.name}</span>
            </a>
          ) : (
            <span key={i} className="inline-flex min-h-6 max-w-48 shrink-0 items-center gap-1 rounded-sm bg-sunken px-1.5 type-caption text-fg">
              <Link2 aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{f.name}</span>
            </span>
          )
        ) : links && f.file_id ? (
          <FileChip key={f.file_id} file={f} token={token} />
        ) : (
          <span key={f.file_id ?? i} className="inline-flex min-h-6 max-w-48 shrink-0 items-center gap-1 rounded-sm bg-sunken px-1.5 type-caption text-fg">
            <FileIcon name={f.name} {...(f.mime ? { mime: f.mime } : {})} />
            <span className="truncate">{f.name}</span>
          </span>
        ),
      )}
    </span>
  );
}

function isPageRef(v: unknown): v is { page: string } {
  return !!v && typeof v === "object" && !Array.isArray(v) && typeof (v as { page?: unknown }).page === "string";
}

function isPersonRef(v: unknown): v is { person: string } {
  return !!v && typeof v === "object" && !Array.isArray(v) && typeof (v as { person?: unknown }).person === "string";
}

function flat(v: DocsComputedValue): DocsComputedValue[] {
  return Array.isArray(v) ? v.flatMap(flat) : [v];
}

/** A formula or rollup result: numbers with their format (bar/ring), booleans, dates, people and pages. */
export function ComputedValue({ prop, value, refs, links, token, compact }: { prop: DocsProperty; value: DocsComputedValue; refs: DocsRefs; links: boolean; token?: string | null | undefined; compact?: boolean | undefined }) {
  if (typeof value === "boolean") return <CheckMark on={value} />;
  if (typeof value === "number") {
    const fn = prop.type === "rollup" ? prop.rollup?.function : undefined;
    if (fn && /^(count|checked|unchecked|percent_|date_range)/.test(fn)) return <span className="type-label tabular-nums text-fg">{docsFormatComputed(value, prop, refs)}</span>;
    const fmt = prop.type === "rollup" && prop.rollup?.target && (!prop.number_format || prop.number_format === "number") && !prop.number_display ? prop.rollup.target : prop;
    return <NumberValue prop={{ ...fmt, number_display: prop.number_display ?? "", ...(prop.number_max !== undefined ? { number_max: prop.number_max } : {}) }} value={value} stored={false} />;
  }
  if (Array.isArray(value)) {
    const items = flat(value);
    if (items.length && items.every(isPageRef)) return <RelationChips ids={items.map((x) => (x as { page: string }).page)} refs={refs} links={links} token={token} compact={compact} />;
    if (items.length && items.every(isPersonRef)) return <People ids={items.map((x) => (x as { person: string }).person)} refs={refs} compact={!!compact} />;
    if (items.length && items.every((x) => typeof x === "boolean")) {
      return (
        <span className="flex gap-1">
          {items.map((x, i) => (
            <CheckMark key={i} on={x === true} />
          ))}
        </span>
      );
    }
  }
  if (isPageRef(value)) return <RelationChips ids={[value.page]} refs={refs} links={links} token={token} compact={compact} />;
  if (isPersonRef(value)) return <People ids={[value.person]} refs={refs} compact={!!compact} />;
  const text = docsFormatComputed(value, prop, refs);
  if (!text) return compact ? null : <span className="type-label text-fg-subtle">Empty</span>;
  return <span className={cn("type-label text-fg", compact ? "truncate" : "whitespace-pre-wrap break-words")}>{text}</span>;
}

// ---- editors ---------------------------------------------------------------------------

export interface PropertyEditorProps {
  prop: DocsProperty;
  value: DocsValue | undefined;
  orgId: string;
  onChange: (v: DocsValue) => void;
  /** Adds or edits options typed into a select (the property list changes first, then the value). */
  onSchema?: ((next: DocsProperty) => void) | undefined;
  onDone: () => void;
  /** The row being edited: files upload to its page; relations leave it out of their own picker. */
  rowId?: string | undefined;
  /** Names for the value's people and related rows. */
  refs?: DocsRefs | undefined;
}

/** Edits one value inside a Floating panel. Computed types aren't edited here. */
export function PropertyEditor(props: PropertyEditorProps) {
  const { prop, value, orgId, onChange, onSchema, onDone } = props;
  switch (prop.type) {
    case "select":
    case "status":
    case "multi_select":
      return <OptionsEditor prop={prop} value={value} onChange={onChange} onSchema={onSchema} onDone={onDone} />;
    case "person":
      return <PeopleEditor orgId={orgId} value={(value as string[] | null) ?? []} refs={props.refs ?? EMPTY_REFS} onChange={onChange} />;
    case "date":
      return <DateEditor prop={prop} value={(value as DocsDateValue | null) ?? null} onChange={onChange} onDone={onDone} />;
    case "number":
      return <NumberEditor prop={prop} value={typeof value === "number" ? value : null} onChange={onChange} onDone={onDone} />;
    case "files":
      return <FilesEditor prop={prop} value={(value as DocsFileValue[] | null) ?? []} rowId={props.rowId} onChange={onChange} />;
    case "relation":
      return <RelationEditor prop={prop} value={(value as string[] | null) ?? []} refs={props.refs ?? EMPTY_REFS} rowId={props.rowId} onChange={onChange} onDone={onDone} />;
    case "checkbox":
      return (
        <div className="p-2">
          <Switch checked={value === true} onCheckedChange={(on) => onChange(on)} label={prop.name} />
        </div>
      );
    default:
      return <TextEditor prop={prop} value={value} onChange={onChange} onDone={onDone} />;
  }
}

function TextEditor({ prop, value, onChange, onDone }: { prop: DocsProperty; value: DocsValue | undefined; onChange: (v: DocsValue) => void; onDone: () => void }) {
  const initial = value === null || value === undefined ? "" : String(value);
  const [v, setV] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  const saved = useRef(false);
  const save = () => {
    if (saved.current) return;
    const t = v.trim();
    if (t === initial.trim()) {
      saved.current = true;
      return onDone();
    }
    if (!t) {
      saved.current = true;
      onChange(null);
      return onDone();
    }
    if (prop.type === "url") {
      const href = docsSafeLink(t.includes("://") ? t : `https://${t}`);
      if (!href || href.startsWith("mailto:")) return setError("Enter a web address.");
      onChange(href);
    } else if (prop.type === "email") {
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(t)) return setError("Enter an email address.");
      onChange(t);
    } else if (prop.type === "phone") {
      if (!/^\+?[0-9 ().\-/]{3,30}(\s*(x|ext\.?)\s*[0-9]{1,6})?$/.test(t)) return setError("Enter a phone number.");
      onChange(t);
    } else onChange(t);
    saved.current = true;
    onDone();
  };
  const multiline = prop.type === "text";
  return (
    <form
      className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-2 p-2"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      {multiline ? (
        <TextArea
          value={v}
          onChange={(e) => setV(e.target.value)}
          aria-label={prop.name}
          minRows={2}
          maxRows={10}
          data-autofocus
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              save();
            }
          }}
          onBlur={save}
        />
      ) : (
        <Input
          value={v}
          onChange={(e) => {
            setV(e.target.value);
            setError(null);
          }}
          aria-label={prop.name}
          aria-invalid={!!error}
          type={prop.type === "email" ? "email" : prop.type === "phone" ? "tel" : prop.type === "url" ? "url" : "text"}
          inputMode={prop.type === "email" ? "email" : prop.type === "phone" ? "tel" : prop.type === "url" ? "url" : "text"}
          data-autofocus
          onBlur={() => {
            if (!error) save();
          }}
        />
      )}
      {error ? (
        <p role="alert" className="type-caption text-danger-strong">
          {error}
        </p>
      ) : null}
    </form>
  );
}

/** Reads a typed number: digits with an optional sign, decimal point or comma, grouping spaces, a currency sign or %. */
export function parseNumberInput(s: string): number | null {
  let t = s.trim().replace(/[\s$€£¥₹%]/g, "");
  if (!t) return null;
  // "1.234,5" → 1234.5; "1,234.5" → 1234.5; "12,5" → 12.5
  if (/,\d{1,2}$/.test(t) && !t.includes(".")) t = t.replace(/\./g, "").replace(",", ".");
  t = t.replace(/,/g, "");
  const n = Number(t);
  return Number.isFinite(n) ? n : null;
}

function NumberEditor({ prop, value, onChange, onDone }: { prop: DocsProperty; value: number | null; onChange: (v: DocsValue) => void; onDone: () => void }) {
  const scale = docsNumberScale(prop);
  const currency = docsCurrencyCode(prop.number_format);
  const major = value === null ? "" : String(value / scale);
  const [v, setV] = useState(major);
  const [error, setError] = useState<string | null>(null);
  const saved = useRef(false);
  const save = () => {
    if (saved.current) return;
    if (v.trim() === major) {
      saved.current = true;
      return onDone();
    }
    if (!v.trim()) {
      saved.current = true;
      onChange(null);
      return onDone();
    }
    const n = parseNumberInput(v);
    if (n === null) return setError("Enter a number.");
    saved.current = true;
    onChange(currency ? Math.round(n * scale) : n);
    onDone();
  };
  return (
    <form
      className="flex w-64 flex-col gap-2 p-2"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div className="flex items-center gap-2">
        {currency ? <span className="type-label font-semibold text-fg-muted">{currency.toUpperCase()}</span> : null}
        <Input
          value={v}
          onChange={(e) => {
            setV(e.target.value);
            setError(null);
          }}
          aria-label={prop.name}
          aria-invalid={!!error}
          inputMode="decimal"
          data-autofocus
          className="flex-1"
          onBlur={() => {
            if (!error) save();
          }}
        />
        {prop.number_format === "percent" ? <span className="type-label text-fg-muted">%</span> : null}
      </div>
      {error ? (
        <p role="alert" className="type-caption text-danger-strong">
          {error}
        </p>
      ) : null}
    </form>
  );
}

export const REMINDER_LABEL: Record<DocsReminder, string> = {
  at_time: "At the time",
  "5m": "5 minutes before",
  "10m": "10 minutes before",
  "15m": "15 minutes before",
  "30m": "30 minutes before",
  "1h": "1 hour before",
  "2h": "2 hours before",
  "1d": "1 day before",
  "2d": "2 days before",
  "1w": "1 week before",
};

const DAY_REMINDERS: DocsReminder[] = ["at_time", "1d", "2d", "1w"];
const TIME_REMINDERS: DocsReminder[] = ["at_time", "5m", "10m", "15m", "30m", "1h", "2h", "1d", "2d"];

let zonesCache: string[] | null = null;
export function timeZones(): string[] {
  if (zonesCache) return zonesCache;
  let list: string[] = [];
  try {
    list = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone") ?? [];
  } catch {
    list = [];
  }
  const here = docsTimeZone();
  zonesCache = Array.from(new Set([here, "UTC", ...list]));
  return zonesCache;
}

function DateEditor({ prop, value, onChange, onDone }: { prop: DocsProperty; value: DocsDateValue | null; onChange: (v: DocsValue) => void; onDone: () => void }) {
  const [start, setStart] = useState(value?.start.slice(0, 10) ?? "");
  const [startTime, setStartTime] = useState(value?.start.slice(11, 16) ?? "");
  const [end, setEnd] = useState(value?.end?.slice(0, 10) ?? "");
  const [endTime, setEndTime] = useState(value?.end?.slice(11, 16) ?? "");
  const [range, setRange] = useState(!!value?.end);
  const [withTime, setWithTime] = useState((value?.start.length ?? 0) > 10);
  const [tz, setTz] = useState(value?.tz ?? docsTimeZone());
  const [remind, setRemind] = useState<DocsReminder | "">(value?.remind ?? "");
  const [error, setError] = useState<string | null>(null);
  const reminders = withTime ? TIME_REMINDERS : DAY_REMINDERS;
  const submit = () => {
    if (!start) {
      onChange(null);
      return onDone();
    }
    const s = withTime ? `${start}T${startTime || "09:00"}` : start;
    let e: string | undefined;
    if (range && end) {
      e = withTime ? `${end}T${endTime || startTime || "09:00"}` : end;
      if (e < s) return setError("End after the start.");
    }
    const r = remind && reminders.includes(remind) ? remind : "";
    const next: DocsDateValue = { start: s, ...(e ? { end: e } : {}), ...(withTime || r ? { tz } : {}), ...(r ? { remind: r } : {}) };
    onChange(next);
    onDone();
  };
  const preview = start ? docsFormatDateValue({ start: withTime ? `${start}T${startTime || "09:00"}` : start, ...(range && end ? { end: withTime ? `${end}T${endTime || "09:00"}` : end } : {}) }, prop) : "";
  return (
    <form
      className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-3 p-2"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      {preview ? <p className="type-label font-medium text-fg">{preview}</p> : null}
      <div className="flex items-end gap-2">
        <label className="flex min-w-0 flex-1 flex-col gap-1 type-caption text-fg-muted">
          {range ? "Start" : "Date"}
          <Input type="date" value={start} onChange={(e) => setStart(e.target.value)} data-autofocus />
        </label>
        {withTime ? (
          <label className="flex w-28 flex-col gap-1 type-caption text-fg-muted">
            Time
            <Input type="time" value={startTime} onChange={(e) => setStartTime(e.target.value)} />
          </label>
        ) : null}
      </div>
      {range ? (
        <div className="flex items-end gap-2">
          <label className="flex min-w-0 flex-1 flex-col gap-1 type-caption text-fg-muted">
            End
            <Input type="date" value={end} min={start} onChange={(e) => setEnd(e.target.value)} />
          </label>
          {withTime ? (
            <label className="flex w-28 flex-col gap-1 type-caption text-fg-muted">
              Time
              <Input type="time" value={endTime} onChange={(e) => setEndTime(e.target.value)} />
            </label>
          ) : null}
        </div>
      ) : null}
      <div className="flex flex-col gap-2">
        <Switch checked={range} onCheckedChange={setRange} label="End date" />
        <Switch checked={withTime} onCheckedChange={setWithTime} label="Include time" />
      </div>
      {withTime ? <Select aria-label="Time zone" value={tz} onValueChange={setTz} options={timeZones().map((z) => ({ value: z, label: z.replace(/_/g, " ") }))} /> : null}
      <Select
        aria-label="Reminder"
        value={remind || "none"}
        onValueChange={(v) => setRemind(v === "none" ? "" : (v as DocsReminder))}
        options={[
          { value: "none", label: "No reminder" },
          ...reminders.map((r) => ({ value: r, label: r === "at_time" && !withTime ? "On the day (9:00)" : REMINDER_LABEL[r] })),
        ]}
      />
      {error ? (
        <p role="alert" className="type-caption text-danger-strong">
          {error}
        </p>
      ) : null}
      <div className="flex justify-end gap-2">
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
    </form>
  );
}

function OptionsEditor({ prop, value, onChange, onSchema, onDone }: { prop: DocsProperty; value: DocsValue | undefined; onChange: (v: DocsValue) => void; onSchema?: ((next: DocsProperty) => void) | undefined; onDone: () => void }) {
  const multi = prop.type === "multi_select";
  const [q, setQ] = useState("");
  const [editing, setEditing] = useState<string | null>(null);
  const chosen = new Set(multi ? ((value as string[] | null) ?? []) : value ? [value as string] : []);
  const all = prop.options ?? [];
  const opts = all.filter((o) => o.name.toLowerCase().includes(q.trim().toLowerCase()));
  const exact = all.some((o) => o.name.toLowerCase() === q.trim().toLowerCase());
  const pick = (id: string) => {
    if (multi) {
      onChange(chosen.has(id) ? [...chosen].filter((x) => x !== id) : [...chosen, id]);
    } else {
      onChange(chosen.has(id) ? null : id);
      onDone();
    }
  };
  const create = () => {
    const name = q.trim();
    if (!name || !onSchema) return;
    const opt: DocsOption = { id: newShortId(), name, color: OPTION_COLORS[(all.length % (OPTION_COLORS.length - 1)) + 1]!, ...(prop.type === "status" ? { group: "todo" as const } : {}) };
    onSchema({ ...prop, options: [...all, opt] });
    setQ("");
    if (multi) onChange([...chosen, opt.id]);
    else {
      onChange(opt.id);
      onDone();
    }
  };
  const setOption = (o: DocsOption) => onSchema?.({ ...prop, options: all.map((x) => (x.id === o.id ? o : x)) });
  const sections: { label: string | null; items: DocsOption[] }[] =
    prop.type === "status" ? (["todo", "doing", "done"] as const).map((g) => ({ label: DOCS_STATUS_GROUP_LABEL[g], items: opts.filter((o) => (o.group ?? "todo") === g) })) : [{ label: null, items: opts }];
  return (
    <div className="flex w-[min(18rem,calc(100vw-2rem))] flex-col gap-1 p-1">
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
      <div role="listbox" aria-multiselectable={multi} aria-label={prop.name} className="flex max-h-72 flex-col overflow-y-auto">
        {sections.map((s) =>
          s.items.length || !s.label ? (
            <div key={s.label ?? "all"} className="flex flex-col">
              {s.label ? <p className="px-3 pt-2 pb-1 type-caption font-semibold text-fg-muted">{s.label}</p> : null}
              {s.items.map((o) => (
                <div key={o.id} className="flex flex-col">
                  <div className="flex items-center">
                    <button type="button" role="option" aria-selected={chosen.has(o.id)} className={cn(floatingItemClass, "flex-1")} onClick={() => pick(o.id)}>
                      <span className="flex size-4 items-center justify-center text-primary-strong">{chosen.has(o.id) ? <Check aria-hidden /> : null}</span>
                      <OptionBadge option={o} status={prop.type === "status"} />
                    </button>
                    {onSchema ? (
                      <IconButton label={`Edit ${o.name}`} icon={<Ellipsis />} size="sm" variant="ghost" onClick={() => setEditing((x) => (x === o.id ? null : o.id))} aria-expanded={editing === o.id} />
                    ) : null}
                  </div>
                  {editing === o.id && onSchema ? (
                    <OptionEditor
                      option={o}
                      onChange={setOption}
                      onDelete={() => {
                        setEditing(null);
                        onSchema({ ...prop, options: all.filter((x) => x.id !== o.id) });
                        if (chosen.has(o.id)) onChange(multi ? [...chosen].filter((x) => x !== o.id) : null);
                      }}
                    />
                  ) : null}
                </div>
              ))}
            </div>
          ) : null,
        )}
        {q.trim() && !exact && onSchema ? (
          <button type="button" className={floatingItemClass} onClick={create}>
            <Plus aria-hidden />
            <span className="truncate">Add “{q.trim()}”</span>
          </button>
        ) : null}
        {opts.length === 0 && !(q.trim() && onSchema) ? <p className="px-3 py-2 type-label text-fg-muted">{all.length ? "No option matches." : "No options yet."}</p> : null}
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

/** Rename, recolour or delete one option. */
export function OptionEditor({ option, onChange, onDelete }: { option: DocsOption; onChange: (o: DocsOption) => void; onDelete: () => void }) {
  return (
    <div className="mx-2 mb-2 flex flex-col gap-2 rounded-md border border-border p-2">
      <Input
        defaultValue={option.name}
        aria-label="Option name"
        maxLength={100}
        onBlur={(e) => {
          const n = e.target.value.trim();
          if (n && n !== option.name) onChange({ ...option, name: n });
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            (e.target as HTMLInputElement).blur();
          }
        }}
      />
      <SwatchPicker value={option.color} onChange={(color) => onChange({ ...option, color })} />
      <Button type="button" size="sm" variant="danger-ghost" leftIcon={<Trash2 />} onClick={onDelete}>
        Delete option
      </Button>
    </div>
  );
}

function PeopleEditor({ orgId, value, refs, onChange }: { orgId: string; value: string[]; refs: DocsRefs; onChange: (v: DocsValue) => void }) {
  const [q, setQ] = useState("");
  const people = useAccessDirectory(orgId, q, { types: "user", limit: 12 });
  const chosen = new Set(value);
  const known = new Map((people.data ?? []).map((p) => [p.id, p]));
  return (
    <div className="flex w-[min(18rem,calc(100vw-2rem))] flex-col gap-1 p-1">
      {value.length ? (
        <div className="flex flex-wrap gap-1 p-1">
          {value.map((id) => {
            const name = refs.users[id]?.name ?? known.get(id)?.name ?? "Someone";
            return (
              <span key={id} className="inline-flex min-h-6 items-center gap-1 rounded-sm bg-sunken pl-1.5 type-caption text-fg">
                {name}
                <button type="button" aria-label={`Remove ${name}`} className="flex size-6 items-center justify-center rounded-sm hover:bg-border focus-visible:outline-2 focus-visible:outline-ring" onClick={() => onChange(value.filter((x) => x !== id))}>
                  <X aria-hidden className="size-3" />
                </button>
              </span>
            );
          })}
        </div>
      ) : null}
      <div className="p-1">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search people" aria-label="Search people" data-autofocus />
      </div>
      <div role="listbox" aria-multiselectable aria-label="People" className="flex max-h-64 flex-col overflow-y-auto">
        {(people.data ?? []).map((p) => (
          <button key={p.id} type="button" role="option" aria-selected={chosen.has(p.id)} className={floatingItemClass} onClick={() => onChange(chosen.has(p.id) ? value.filter((x) => x !== p.id) : [...value, p.id])}>
            <span className="flex size-4 items-center justify-center text-primary-strong">{chosen.has(p.id) ? <Check aria-hidden /> : null}</span>
            <Avatar size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />
            <span className="min-w-0 flex-1 truncate">{p.name}</span>
          </button>
        ))}
        {people.isPending ? <p className="px-3 py-2 type-label text-fg-muted">Loading people…</p> : null}
        {people.isError ? <p className="px-3 py-2 type-label text-danger-strong">Couldn't load people.</p> : null}
        {!people.isPending && !people.isError && (people.data ?? []).length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No one matches.</p> : null}
      </div>
    </div>
  );
}

function FilesEditor({ prop, value, rowId, onChange }: { prop: DocsProperty; value: DocsFileValue[]; rowId: string | undefined; onChange: (v: DocsValue) => void }) {
  const upload = useDocsUploadFile(rowId ?? "");
  const input = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(0);
  const [linkOpen, setLinkOpen] = useState(false);
  const [url, setUrl] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const files = useRef(value);
  files.current = value;
  const onPick = async (list: FileList | null) => {
    if (!list || !rowId) return;
    const picked = Array.from(list).slice(0, Math.max(0, 20 - value.length));
    setBusy((n) => n + picked.length);
    const added: DocsFileValue[] = [];
    for (const f of picked) {
      try {
        const up = await upload.mutateAsync({ file: f, name: f.name, ...(f.type ? { contentType: f.type } : {}) });
        added.push({ file_id: up.id, name: up.name, mime: up.content_type });
      } catch (e) {
        toast.error(errorMessage(e, `Couldn't upload ${f.name}.`));
      } finally {
        setBusy((n) => n - 1);
      }
    }
    if (added.length) onChange([...files.current, ...added]);
  };
  return (
    <div className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-2 p-2">
      {value.length ? (
        <ul className="flex flex-col gap-1" aria-label={prop.name}>
          {value.map((f, i) => (
            <li key={f.file_id ?? `${f.url}-${i}`} className="flex min-h-control items-center gap-2 rounded-sm px-1 hover:bg-sunken">
              {f.url ? <Link2 aria-hidden className="size-4 shrink-0 text-fg-muted" /> : <FileIcon name={f.name} {...(f.mime ? { mime: f.mime } : {})} />}
              <span className="min-w-0 flex-1 truncate type-label text-fg">{f.name}</span>
              {f.url ? (
                <a href={docsSafeLink(f.url) ?? "#"} target="_blank" rel="noopener noreferrer nofollow" aria-label={`Open ${f.name}`} className="flex size-8 items-center justify-center rounded-sm text-fg-muted hover:bg-border focus-visible:outline-2 focus-visible:outline-ring">
                  <ExternalLink aria-hidden className="size-4" />
                </a>
              ) : null}
              <IconButton label={`Remove ${f.name}`} icon={<X />} size="sm" variant="ghost" onClick={() => onChange(value.filter((_, j) => j !== i))} />
            </li>
          ))}
        </ul>
      ) : (
        <p className="px-1 type-label text-fg-muted">No files yet.</p>
      )}
      <input ref={input} type="file" multiple hidden onChange={(e) => void onPick(e.target.files).finally(() => (e.target.value = ""))} />
      <div className="flex flex-wrap gap-2">
        {rowId ? (
          <Button type="button" size="sm" variant="secondary" leftIcon={busy ? <Spinner /> : <Upload />} disabled={busy > 0 || value.length >= 20} onClick={() => input.current?.click()} data-autofocus>
            {busy ? "Uploading…" : "Upload"}
          </Button>
        ) : null}
        <Button type="button" size="sm" variant="ghost" leftIcon={<Link2 />} onClick={() => setLinkOpen((o) => !o)} aria-expanded={linkOpen}>
          Add a link
        </Button>
      </div>
      {linkOpen ? (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const raw = url.trim();
            const href = docsSafeLink(raw.includes("://") ? raw : `https://${raw}`);
            if (!raw || !href || href.startsWith("mailto:")) return setError("Enter a web address.");
            onChange([...value, { url: href, name: name.trim() || href.replace(/^https?:\/\//, "") }]);
            setUrl("");
            setName("");
            setError(null);
            setLinkOpen(false);
          }}
        >
          <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" aria-label="Link" type="url" aria-invalid={!!error} />
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Name (optional)" aria-label="Link name" maxLength={200} />
          {error ? (
            <p role="alert" className="type-caption text-danger-strong">
              {error}
            </p>
          ) : null}
          <Button type="submit" size="sm">
            Add link
          </Button>
        </form>
      ) : null}
    </div>
  );
}

function RelationEditor({ prop, value, refs, rowId, onChange, onDone }: { prop: DocsProperty; value: string[]; refs: DocsRefs; rowId: string | undefined; onChange: (v: DocsValue) => void; onDone: () => void }) {
  const target = prop.relation?.database_id ?? null;
  const single = prop.relation?.limit === 1;
  const [q, setQ] = useState("");
  const query = useDebouncedValue(q.trim(), 200);
  const params = useMemo(() => ({ ...(query ? { q: query } : {}), limit: 25, tz: docsTimeZone() }), [query]);
  const rows = useDocsRows(target, params, { enabled: !!target });
  const [titles, setTitles] = useState<Record<string, { title: string; icon: string | null }>>({});
  const chosen = new Set(value);
  const titleOf = (id: string): { title: string | null; icon: string | null } | null => {
    const r = refs.pages[id];
    if (r) return r.deleted ? null : { title: r.title, icon: r.icon };
    const t = titles[id];
    if (t) return t;
    const found = rows.data?.rows.find((x) => x.id === id);
    return found ? { title: found.title, icon: found.icon } : { title: null, icon: null };
  };
  const toggle = (row: DocsRow) => {
    setTitles((t) => ({ ...t, [row.id]: { title: row.title, icon: row.icon } }));
    if (single) {
      onChange(chosen.has(row.id) ? [] : [row.id]);
      onDone();
      return;
    }
    onChange(chosen.has(row.id) ? value.filter((x) => x !== row.id) : [...value, row.id]);
  };
  const results = (rows.data?.rows ?? []).filter((r) => r.id !== rowId);
  return (
    <div className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-1 p-1">
      {value.length ? (
        <div className="flex flex-col gap-0.5 border-b border-border p-1 pb-2">
          {value.map((id) => {
            const t = titleOf(id);
            if (!t) return null;
            const label = t.title === null ? "No access" : docsTitle(t.title);
            return (
              <div key={id} className="flex min-h-control items-center gap-2 rounded-sm px-2">
                {t.title === null ? <Lock aria-hidden className="size-4 shrink-0 text-fg-muted" /> : t.icon ? <span aria-hidden>{t.icon}</span> : null}
                {t.title === null ? (
                  <span className="min-w-0 flex-1 truncate type-label text-fg-muted">{label}</span>
                ) : (
                  <Link href={`/docs/${id}`} className="min-w-0 flex-1 truncate type-label text-fg underline decoration-border-strong underline-offset-4 hover:text-primary-strong">
                    {label}
                  </Link>
                )}
                <IconButton label={`Remove ${label}`} icon={<X />} size="sm" variant="ghost" onClick={() => onChange(value.filter((x) => x !== id))} />
              </div>
            );
          })}
        </div>
      ) : null}
      <div className="p-1">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search rows" aria-label="Search rows to link" data-autofocus />
      </div>
      <div role="listbox" aria-multiselectable={!single} aria-label={prop.name} className="flex max-h-64 flex-col overflow-y-auto">
        {!target ? <p className="px-3 py-2 type-label text-fg-muted">Choose the related database in the property's settings.</p> : null}
        {rows.isPending && target ? <p className="px-3 py-2 type-label text-fg-muted">Loading rows…</p> : null}
        {rows.isError ? (
          <p className="px-3 py-2 type-label text-fg-muted">{rows.error.status === 403 || rows.error.status === 404 ? "You can't open the related database." : "Couldn't load its rows."}</p>
        ) : null}
        {results.map((r) => (
          <button key={r.id} type="button" role="option" aria-selected={chosen.has(r.id)} className={floatingItemClass} onClick={() => toggle(r)}>
            <span className="flex size-4 items-center justify-center text-primary-strong">{chosen.has(r.id) ? <Check aria-hidden /> : null}</span>
            {r.icon ? <span aria-hidden>{r.icon}</span> : null}
            <span className="min-w-0 flex-1 truncate">{docsTitle(r.title)}</span>
          </button>
        ))}
        {rows.data && results.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">{query ? "No row matches." : "No rows yet."}</p> : null}
      </div>
    </div>
  );
}

/** A labelled value row (forms, settings). */
export function ValueRow({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[minmax(7rem,10rem)_1fr] items-start gap-2">
      <div className="flex min-h-control items-center px-2 type-label text-fg-muted">{label}</div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}
