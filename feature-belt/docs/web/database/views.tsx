"use client";

import {
  docsComparePosition,
  docsGroupRows,
  docsKeyBetween,
  docsTitle,
  type DocsColumn,
  type DocsDateValue,
  type DocsPage,
  type DocsProperty,
  type DocsRefs,
  type DocsRow,
  type DocsValue,
  type DocsView,
  type DocsViewConfig,
} from "@scaffoldapp/shared";
import { Button, IconButton, Skeleton, cn } from "@scaffoldapp/ui-web";
import { ChevronLeft, ChevronRight, Copy, FileText, Maximize2, Plus, Trash2 } from "lucide-react";
import { useMemo, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Floating } from "../Floating";
import { PROPERTY_ICON } from "../format";
import { NewPropertyForm, PropertyMenu } from "./panels";
import { OptionBadge, PropertyDisplay, PropertyEditor } from "./PropertyValue";

export interface ViewProps {
  db: DocsPage;
  schema: DocsProperty[];
  view: DocsView;
  config: DocsViewConfig;
  rows: DocsRow[];
  refs: DocsRefs;
  loading: boolean;
  hasMore: boolean;
  loadMore: () => void;
  editable: boolean;
  orgId: string;
  publicView: boolean;
  onOpenRow: (id: string) => void;
  onSetValue: (row: DocsRow, propId: string, value: DocsValue) => void;
  onSetTitle: (row: DocsRow, title: string) => void;
  onCreateRow: (values?: Record<string, DocsValue>, title?: string) => void;
  onMoveRow: (row: DocsRow, change: { values?: Record<string, DocsValue>; position?: string }) => void;
  onSchemaChange: (props: DocsProperty[]) => void;
  onConfigChange: (cfg: DocsViewConfig) => void;
  onTrashRow: (row: DocsRow) => void;
  onDuplicateRow: (row: DocsRow) => void;
}

/** A cell that opens an editor for its value. */
function EditableValue({ p, row, props, children, className }: { p: DocsProperty; row: DocsRow; props: ViewProps; children: ReactNode; className?: string }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const canEdit = props.editable && row.my_level !== "view" && row.my_level !== "comment" && row.my_level !== "none";
  if (p.type === "checkbox") {
    return (
      <button
        type="button"
        role="checkbox"
        aria-checked={row.props[p.id] === true}
        aria-label={`${p.name}: ${row.props[p.id] === true ? "checked" : "not checked"}`}
        disabled={!canEdit}
        className={cn("flex min-h-control w-full items-center px-2 focus-visible:outline-2 focus-visible:outline-ring", className)}
        onClick={(e) => {
          e.stopPropagation();
          props.onSetValue(row, p.id, row.props[p.id] !== true);
        }}
      >
        {children}
      </button>
    );
  }
  return (
    <>
      <button
        type="button"
        disabled={!canEdit}
        aria-label={`${p.name}, edit`}
        className={cn("flex min-h-control w-full min-w-0 items-center px-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent", className)}
        onClick={(e) => {
          e.stopPropagation();
          setAnchor(e.currentTarget);
        }}
      >
        {children}
      </button>
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus label={p.name}>
        <PropertyEditor
          prop={p}
          value={row.props[p.id]}
          orgId={props.orgId}
          onChange={(v) => props.onSetValue(row, p.id, v)}
          onSchema={props.editable ? (next) => props.onSchemaChange(props.schema.map((x) => (x.id === next.id ? next : x))) : undefined}
          onDone={() => setAnchor(null)}
        />
      </Floating>
    </>
  );
}

function RowMenu({ row, props }: { row: DocsRow; props: ViewProps }) {
  if (props.publicView) return null;
  const canEdit = props.editable && (row.my_level === "edit" || row.my_level === "manage");
  return (
    <ResourceMenu
      feature="docs"
      kind="docs.page"
      id={row.id}
      title={docsTitle(row.title)}
      label={`More actions for ${docsTitle(row.title)}`}
      actions={[
        { id: "open", label: "Open", icon: Maximize2, onSelect: () => props.onOpenRow(row.id) },
        ...(canEdit ? [{ id: "dup", label: "Duplicate", icon: Copy, onSelect: () => props.onDuplicateRow(row) }] : []),
        ...(canEdit ? [{ id: "trash", label: "Move to trash", icon: Trash2, destructive: true, onSelect: () => props.onTrashRow(row) }] : []),
      ]}
    />
  );
}

function TitleCell({ row, props }: { row: DocsRow; props: ViewProps }) {
  const [editing, setEditing] = useState(false);
  const canEdit = props.editable && (row.my_level === "edit" || row.my_level === "manage");
  if (editing) {
    return (
      <input
        autoFocus
        defaultValue={row.title}
        aria-label="Title"
        maxLength={300}
        className="h-control w-full min-w-0 bg-surface px-2 type-label text-fg outline-2 outline-ring"
        onBlur={(e) => {
          setEditing(false);
          if (e.target.value !== row.title) props.onSetTitle(row, e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") (e.target as HTMLInputElement).blur();
          if (e.key === "Escape") setEditing(false);
        }}
      />
    );
  }
  return (
    <div className="group/title flex min-h-control min-w-0 items-center gap-1 px-2">
      <button type="button" className="flex min-w-0 flex-1 items-center gap-2 text-left focus-visible:outline-2 focus-visible:outline-ring" onClick={() => props.onOpenRow(row.id)} onDoubleClick={() => canEdit && setEditing(true)}>
        {row.icon ? <span aria-hidden>{row.icon}</span> : <FileText aria-hidden className="size-4 shrink-0 text-fg-muted" />}
        <span className="truncate type-label font-medium text-fg underline-offset-2 hover:underline">{docsTitle(row.title)}</span>
      </button>
      {canEdit ? (
        <button type="button" className="shrink-0 rounded-sm px-1 type-caption text-fg-subtle opacity-0 group-hover/title:opacity-100 hover:bg-sunken focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring" onClick={() => setEditing(true)}>
          Rename
        </button>
      ) : null}
    </div>
  );
}

function orderedColumns(schema: DocsProperty[], config: DocsViewConfig): DocsColumn[] {
  const cols: DocsColumn[] = [];
  for (const c of config.columns) if (schema.some((p) => p.id === c.property)) cols.push(c);
  for (const p of schema) if (!cols.some((c) => c.property === p.id)) cols.push({ property: p.id });
  const title = cols.findIndex((c) => c.property === "title");
  if (title > 0) cols.unshift(...cols.splice(title, 1));
  return cols;
}

function HeaderCell({ p, col, props, index, cols }: { p: DocsProperty; col: DocsColumn; props: ViewProps; index: number; cols: DocsColumn[] }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const Icon = PROPERTY_ICON[p.type];
  const resizing = useRef<{ x: number; w: number } | null>(null);
  const [width, setWidth] = useState<number | null>(null);
  const w = width ?? col.width ?? (p.type === "title" ? 240 : 160);
  const setCols = (next: DocsColumn[]) => props.onConfigChange({ ...props.config, columns: next });
  const onResize = (e: ReactPointerEvent<HTMLSpanElement>) => {
    e.preventDefault();
    e.stopPropagation();
    resizing.current = { x: e.clientX, w };
    const move = (ev: PointerEvent) => {
      if (resizing.current) setWidth(Math.min(800, Math.max(80, resizing.current.w + ev.clientX - resizing.current.x)));
    };
    const up = (ev: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      if (!resizing.current) return;
      const nw = Math.min(800, Math.max(80, resizing.current.w + ev.clientX - resizing.current.x));
      resizing.current = null;
      setWidth(null);
      if (props.editable) setCols(cols.map((c) => (c.property === p.id ? { ...c, width: Math.round(nw) } : c)));
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return (
    <th scope="col" style={{ width: w, minWidth: w, maxWidth: w }} className="relative border-b border-r border-border p-0 text-left font-normal last:border-r-0">
      <button
        type="button"
        disabled={!props.editable}
        className="flex h-control w-full items-center gap-1.5 px-2 type-label text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:hover:bg-transparent"
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        <Icon aria-hidden className="size-4 shrink-0" />
        <span className="truncate">{p.name}</span>
      </button>
      {props.editable ? (
        <span
          role="separator"
          aria-orientation="vertical"
          aria-label={`Resize ${p.name}`}
          className="absolute top-0 -right-1 z-10 h-full w-2 cursor-col-resize touch-none hover:bg-primary/40"
          onPointerDown={onResize}
        />
      ) : null}
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus label={p.name}>
        <PropertyMenu
          prop={p}
          onChange={(next) => props.onSchemaChange(props.schema.map((x) => (x.id === next.id ? next : x)))}
          onDelete={
            p.type === "title"
              ? undefined
              : () => {
                  setAnchor(null);
                  props.onSchemaChange(props.schema.filter((x) => x.id !== p.id));
                }
          }
          onHide={
            p.type === "title"
              ? undefined
              : () => {
                  setAnchor(null);
                  setCols(cols.map((c) => (c.property === p.id ? { ...c, hidden: true } : c)));
                }
          }
          onMove={(dir) => {
            const j = index + dir;
            if (j < 1 || j >= cols.length || index === 0) return;
            const next = [...cols];
            [next[index], next[j]] = [next[j]!, next[index]!];
            setCols(next);
          }}
        />
      </Floating>
    </th>
  );
}

function AddPropertyHeader({ props }: { props: ViewProps }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  if (!props.editable) return null;
  return (
    <th scope="col" className="w-10 border-b border-border p-0">
      <IconButton label="Add a property" icon={<Plus />} size="sm" variant="ghost" onClick={(e) => setAnchor(e.currentTarget)} />
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus placement="bottom-end" label="New property">
        <NewPropertyForm
          onCreate={(p) => {
            setAnchor(null);
            props.onSchemaChange([...props.schema, p]);
          }}
        />
      </Floating>
    </th>
  );
}

function LoadMoreRow({ props }: { props: ViewProps }) {
  if (!props.hasMore) return null;
  return (
    <div className="flex justify-center py-2">
      <Button variant="ghost" size="sm" onClick={props.loadMore}>
        Load more
      </Button>
    </div>
  );
}

function NewRowButton({ props, values, label = "New" }: { props: ViewProps; values?: Record<string, DocsValue>; label?: string }) {
  if (!props.editable) return null;
  return (
    <button
      type="button"
      onClick={() => props.onCreateRow(values)}
      className="flex min-h-control w-full items-center gap-2 rounded-sm px-2 type-label text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap"
    >
      <Plus aria-hidden className="size-4" />
      {label}
    </button>
  );
}

export function TableView(props: ViewProps) {
  const cols = orderedColumns(props.schema, props.config);
  const visible = cols.filter((c) => !c.hidden);
  const byId = new Map(props.schema.map((p) => [p.id, p]));
  return (
    <div className="flex flex-col">
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-max min-w-full table-fixed border-collapse">
          <thead>
            <tr>
              {visible.map((c) => {
                const p = byId.get(c.property)!;
                return <HeaderCell key={c.property} p={p} col={c} props={props} index={cols.indexOf(c)} cols={cols} />;
              })}
              <AddPropertyHeader props={props} />
            </tr>
          </thead>
          <tbody>
            {props.loading && props.rows.length === 0
              ? Array.from({ length: 3 }, (_, i) => (
                  <tr key={i}>
                    <td colSpan={visible.length + 1} className="border-b border-border p-2">
                      <Skeleton className="h-6 w-full" />
                    </td>
                  </tr>
                ))
              : props.rows.map((row) => (
                  <tr key={row.id} className="group/row hover:bg-sunken/50">
                    {visible.map((c, i) => {
                      const p = byId.get(c.property)!;
                      return (
                        <td key={c.property} className="relative border-b border-r border-border p-0 align-top last:border-r-0">
                          {p.type === "title" ? (
                            <div className="flex items-center">
                              <div className="min-w-0 flex-1">
                                <TitleCell row={row} props={props} />
                              </div>
                              {i === 0 ? (
                                <span className="shrink-0 opacity-100 sm:opacity-0 sm:group-hover/row:opacity-100 sm:focus-within:opacity-100">
                                  <RowMenu row={row} props={props} />
                                </span>
                              ) : null}
                            </div>
                          ) : (
                            <EditableValue p={p} row={row} props={props}>
                              <PropertyDisplay prop={p} value={row.props[p.id]} refs={props.refs} compact />
                            </EditableValue>
                          )}
                        </td>
                      );
                    })}
                    {props.editable ? <td className="border-b border-border" /> : null}
                  </tr>
                ))}
          </tbody>
        </table>
        {!props.loading && props.rows.length === 0 ? <p className="px-3 py-4 type-label text-fg-muted">No rows match this view.</p> : null}
      </div>
      <LoadMoreRow props={props} />
      <NewRowButton props={props} />
    </div>
  );
}

export function ListView(props: ViewProps) {
  const shown = props.config.card_properties.map((id) => props.schema.find((p) => p.id === id)).filter((p): p is DocsProperty => !!p && p.type !== "title");
  return (
    <div className="flex flex-col">
      <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
        {props.loading && props.rows.length === 0
          ? Array.from({ length: 3 }, (_, i) => (
              <li key={i} className="p-3">
                <Skeleton className="h-6 w-2/3" />
              </li>
            ))
          : props.rows.map((row) => (
              <li key={row.id} className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-1 px-2 py-1">
                <div className="min-w-40 flex-1">
                  <TitleCell row={row} props={props} />
                </div>
                {shown.map((p) => (
                  <div key={p.id} className="max-w-60">
                    <EditableValue p={p} row={row} props={props} className="min-h-8">
                      <PropertyDisplay prop={p} value={row.props[p.id]} refs={props.refs} compact />
                    </EditableValue>
                  </div>
                ))}
                <RowMenu row={row} props={props} />
              </li>
            ))}
      </ul>
      {!props.loading && props.rows.length === 0 ? <p className="px-3 py-4 type-label text-fg-muted">No rows match this view.</p> : null}
      <LoadMoreRow props={props} />
      <NewRowButton props={props} />
    </div>
  );
}

export function BoardView(props: ViewProps) {
  const group = props.schema.find((p) => p.id === props.config.group_by);
  const shown = props.config.card_properties.map((id) => props.schema.find((p) => p.id === id)).filter((p): p is DocsProperty => !!p && p.type !== "title" && p.id !== group?.id);
  const columns = useMemo(() => docsGroupRows([...props.rows].sort(docsComparePosition), group), [props.rows, group]);
  const [over, setOver] = useState<string | null>(null);
  const dragging = useRef<DocsRow | null>(null);
  if (!group) return <p className="px-3 py-4 type-label text-fg-muted">Add a select or status property to group this board by.</p>;
  const dropInto = (optionId: string | null, beforeRow: DocsRow | null, list: DocsRow[]) => {
    const row = dragging.current;
    dragging.current = null;
    setOver(null);
    if (!row) return;
    const rest = list.filter((r) => r.id !== row.id);
    const i = beforeRow ? rest.findIndex((r) => r.id === beforeRow.id) : rest.length;
    const lo = i > 0 ? rest[i - 1]!.position : null;
    const hi = i < rest.length ? rest[i]!.position : null;
    const values = (row.props[group.id] ?? null) !== optionId ? { [group.id]: optionId } : undefined;
    props.onMoveRow(row, { ...(values ? { values } : {}), position: docsKeyBetween(lo, hi) });
  };
  return (
    <div className="flex gap-3 overflow-x-auto pb-2">
      {columns.map((col) => {
        const key = col.option?.id ?? "none";
        return (
          <section
            key={key}
            aria-label={col.option?.name ?? `No ${group.name}`}
            className={cn("flex w-64 shrink-0 flex-col gap-2 rounded-md bg-sunken p-2", over === key && "ring-2 ring-primary")}
            onDragOver={(e) => {
              if (!props.editable || !dragging.current) return;
              e.preventDefault();
              setOver(key);
            }}
            onDragLeave={() => setOver((o) => (o === key ? null : o))}
            onDrop={(e) => {
              e.preventDefault();
              dropInto(col.option?.id ?? null, null, col.rows);
            }}
          >
            <header className="flex items-center gap-2 px-1">
              {col.option ? <OptionBadge option={col.option} /> : <span className="type-label text-fg-muted">No {group.name.toLowerCase()}</span>}
              <span className="type-caption tabular-nums text-fg-muted">{col.rows.length}</span>
            </header>
            {props.loading && props.rows.length === 0 ? <Skeleton className="h-16 w-full rounded-md" /> : null}
            {col.rows.map((row) => (
              <article
                key={row.id}
                draggable={props.editable && (row.my_level === "edit" || row.my_level === "manage")}
                onDragStart={(e) => {
                  dragging.current = row;
                  e.dataTransfer.effectAllowed = "move";
                  e.dataTransfer.setData("text/plain", docsTitle(row.title));
                }}
                onDragEnd={() => {
                  dragging.current = null;
                  setOver(null);
                }}
                onDragOver={(e) => {
                  if (dragging.current) e.preventDefault();
                }}
                onDrop={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  dropInto(col.option?.id ?? null, row, col.rows);
                }}
                className="group/card flex flex-col gap-2 rounded-md border border-border bg-surface p-3"
              >
                <div className="flex items-start gap-1">
                  <button type="button" className="min-w-0 flex-1 text-left type-label font-medium text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring" onClick={() => props.onOpenRow(row.id)}>
                    {row.icon ? `${row.icon} ` : ""}
                    {docsTitle(row.title)}
                  </button>
                  {props.editable && (row.my_level === "edit" || row.my_level === "manage") ? <MoveCardMenu row={row} group={group} props={props} /> : null}
                  <RowMenu row={row} props={props} />
                </div>
                {shown.map((p) =>
                  row.props[p.id] !== undefined && row.props[p.id] !== null ? (
                    <div key={p.id} className="flex min-w-0 items-center">
                      <PropertyDisplay prop={p} value={row.props[p.id]} refs={props.refs} compact />
                    </div>
                  ) : null,
                )}
              </article>
            ))}
            <NewRowButton props={props} values={col.option ? { [group.id]: col.option.id } : {}} />
          </section>
        );
      })}
    </div>
  );
}

/** Keyboard and touch alternative to dragging a card. */
function MoveCardMenu({ row, group, props }: { row: DocsRow; group: DocsProperty; props: ViewProps }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  return (
    <>
      <button
        type="button"
        aria-label={`Move ${docsTitle(row.title)} to another column`}
        className="shrink-0 rounded-sm px-1 type-caption text-fg-subtle hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring sm:opacity-0 sm:group-hover/card:opacity-100 sm:focus-visible:opacity-100"
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        Move
      </button>
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus label="Move to">
        <div className="flex flex-col">
          {[...(group.options ?? []), null].map((o) => (
            <button
              key={o?.id ?? "none"}
              type="button"
              disabled={(row.props[group.id] ?? null) === (o?.id ?? null)}
              className="flex min-h-control items-center gap-2 rounded-sm px-3 text-left hover:bg-sunken focus-visible:bg-sunken focus-visible:outline-none disabled:opacity-50"
              onClick={() => {
                setAnchor(null);
                props.onMoveRow(row, { values: { [group.id]: o?.id ?? null } });
              }}
            >
              {o ? <OptionBadge option={o} /> : <span className="type-label text-fg-muted">No {group.name.toLowerCase()}</span>}
            </button>
          ))}
        </div>
      </Floating>
    </>
  );
}

function monthDays(year: number, month: number): string[] {
  const first = new Date(Date.UTC(year, month, 1));
  const startDow = (first.getUTCDay() + 6) % 7; // Monday first
  const out: string[] = [];
  for (let i = -startDow; out.length < 42; i++) {
    const d = new Date(Date.UTC(year, month, 1 + i));
    out.push(d.toISOString().slice(0, 10));
  }
  return out;
}

export function CalendarView(props: ViewProps) {
  const dateProp = props.schema.find((p) => p.id === props.config.date_property);
  const today = new Date();
  const [ym, setYm] = useState({ y: today.getFullYear(), m: today.getMonth() });
  const dragging = useRef<DocsRow | null>(null);
  const [over, setOver] = useState<string | null>(null);
  const days = monthDays(ym.y, ym.m);
  const byDay = useMemo(() => {
    const m = new Map<string, DocsRow[]>();
    if (!dateProp) return m;
    for (const r of props.rows) {
      const v = r.props[dateProp.id] as DocsDateValue | undefined;
      if (!v?.start) continue;
      const d = v.start.slice(0, 10);
      m.set(d, [...(m.get(d) ?? []), r]);
    }
    return m;
  }, [props.rows, dateProp]);
  if (!dateProp) return <p className="px-3 py-4 type-label text-fg-muted">Add a date property to show rows on a calendar.</p>;
  const label = new Intl.DateTimeFormat(undefined, { month: "long", year: "numeric" }).format(new Date(ym.y, ym.m, 1));
  const shift = (d: number) => setYm(({ y, m }) => ({ y: m + d < 0 ? y - 1 : m + d > 11 ? y + 1 : y, m: (m + d + 12) % 12 }));
  const todayIso = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, "0")}-${String(today.getDate()).padStart(2, "0")}`;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <IconButton label="Previous month" icon={<ChevronLeft />} size="sm" variant="ghost" onClick={() => shift(-1)} />
        <h3 className="min-w-40 text-center type-title-sm text-fg">{label}</h3>
        <IconButton label="Next month" icon={<ChevronRight />} size="sm" variant="ghost" onClick={() => shift(1)} />
        <Button variant="ghost" size="sm" onClick={() => setYm({ y: today.getFullYear(), m: today.getMonth() })}>
          Today
        </Button>
      </div>
      <div className="overflow-x-auto">
        <div role="grid" aria-label={label} className="grid min-w-[42rem] grid-cols-7 overflow-hidden rounded-md border border-border">
          {["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d) => (
            <div key={d} role="columnheader" className="border-b border-border bg-sunken px-2 py-1 type-caption text-fg-muted">
              {d}
            </div>
          ))}
          {days.map((d) => {
            const inMonth = Number(d.slice(5, 7)) - 1 === ym.m;
            const list = byDay.get(d) ?? [];
            return (
              <div
                key={d}
                role="gridcell"
                aria-label={d}
                className={cn("group/day flex min-h-24 flex-col gap-1 border-b border-r border-border p-1", !inMonth && "bg-sunken/60", over === d && "ring-2 ring-inset ring-primary")}
                onDragOver={(e) => {
                  if (!dragging.current) return;
                  e.preventDefault();
                  setOver(d);
                }}
                onDragLeave={() => setOver((o) => (o === d ? null : o))}
                onDrop={(e) => {
                  e.preventDefault();
                  setOver(null);
                  const row = dragging.current;
                  dragging.current = null;
                  if (!row) return;
                  const cur = row.props[dateProp.id] as DocsDateValue | undefined;
                  const time = cur?.start.slice(10) ?? "";
                  props.onMoveRow(row, { values: { [dateProp.id]: { start: d + time, ...(cur?.end ? { end: cur.end } : {}) } } });
                }}
              >
                <div className="flex items-center justify-between">
                  <span className={cn("flex size-6 items-center justify-center rounded-pill type-caption tabular-nums", d === todayIso ? "bg-primary text-primary-fg" : inMonth ? "text-fg" : "text-fg-subtle")}>{Number(d.slice(8))}</span>
                  {props.editable ? (
                    <button
                      type="button"
                      aria-label={`New row on ${d}`}
                      className="flex size-6 items-center justify-center rounded-sm text-fg-subtle opacity-0 hover:bg-sunken group-hover/day:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring"
                      onClick={() => props.onCreateRow({ [dateProp.id]: { start: d } })}
                    >
                      <Plus aria-hidden className="size-4" />
                    </button>
                  ) : null}
                </div>
                {list.map((row) => (
                  <button
                    key={row.id}
                    type="button"
                    draggable={props.editable && (row.my_level === "edit" || row.my_level === "manage")}
                    onDragStart={(e) => {
                      dragging.current = row;
                      e.dataTransfer.effectAllowed = "move";
                      e.dataTransfer.setData("text/plain", docsTitle(row.title));
                    }}
                    onClick={() => props.onOpenRow(row.id)}
                    className="truncate rounded-sm border border-border bg-surface px-1.5 py-0.5 text-left type-caption text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                  >
                    {docsTitle(row.title)}
                  </button>
                ))}
              </div>
            );
          })}
        </div>
      </div>
      {props.hasMore ? <p className="type-caption text-fg-muted">Showing the first 1,000 rows.</p> : null}
    </div>
  );
}
