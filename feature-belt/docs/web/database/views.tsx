"use client";

import {
  docsAggregations,
  docsComparePosition,
  docsGroupRows,
  docsGroupRowsBy,
  docsKeyBetween,
  docsTimeZone,
  docsTitle,
  docsValueType,
  useDocsRows,
  type DocsDateValue,
  type DocsGroupInfo,
  type DocsProperty,
  type DocsRollupFunction,
  type DocsRow,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Button, IconButton, Skeleton, cn, useMediaQuery } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronLeft, ChevronRight, CornerDownRight, Plus } from "lucide-react";
import { Fragment, useMemo, useRef, useState, type CSSProperties, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { Floating, floatingItemClass } from "../Floating";
import { PROPERTY_ICON } from "../format";
import { CardImage, CardProps, CardTitle, firstImage } from "./cards";
import { EditableValue, GroupHeader, GroupLabel, HiddenGroups, LoadMoreRow, NewRowButton, RowMenu, TitleCell, useGroupState } from "./cells";
import { calcLabel, formatCalc, groupValue, moveGroupValue, orderedColumns, reorderColumns, rowEditable, setShown, visibleProps } from "./model";
import { NewPropertyForm, PropertyMenu } from "./panels";
import type { ViewProps } from "./types";

export type { ViewProps } from "./types";

// ---- table ---------------------------------------------------------------------------

function HeaderCell({ p, props, index, width, sticky, frozenUpTo }: { p: DocsProperty; props: ViewProps; index: number; width: number; sticky: CSSProperties | undefined; frozenUpTo: boolean }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const Icon = PROPERTY_ICON[p.type];
  const resizing = useRef<{ x: number; w: number } | null>(null);
  const [live, setLive] = useState<number | null>(null);
  const w = live ?? width;
  const schemaEditable = props.schemaEditable ?? props.editable;
  const cols = orderedColumns(props.schema, props.config);
  const setCols = (columns: typeof props.config.columns) => props.onConfigChange({ ...props.config, columns });
  const onResize = (e: ReactPointerEvent<HTMLSpanElement>) => {
    e.preventDefault();
    e.stopPropagation();
    resizing.current = { x: e.clientX, w };
    const move = (ev: PointerEvent) => {
      if (resizing.current) setLive(Math.min(800, Math.max(80, resizing.current.w + ev.clientX - resizing.current.x)));
    };
    const up = (ev: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      if (!resizing.current) return;
      const nw = Math.min(800, Math.max(80, resizing.current.w + ev.clientX - resizing.current.x));
      resizing.current = null;
      setLive(null);
      setCols(cols.map((c) => (c.property === p.id ? { ...c, width: Math.round(nw) } : c)));
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const close = () => setAnchor(null);
  const pos = cols.findIndex((c) => c.property === p.id);
  return (
    <th scope="col" style={{ width: w, minWidth: w, maxWidth: w, ...sticky }} className={cn("relative border-b border-r border-border bg-surface p-0 text-left font-normal last:border-r-0", sticky && "sticky z-20")}>
      <button
        type="button"
        title={p.description || undefined}
        className="flex h-control w-full items-center gap-1.5 px-2 type-label text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        <Icon aria-hidden className="size-4 shrink-0" />
        <span className="truncate">{p.name}</span>
      </button>
      {props.editable ? (
        <span role="separator" aria-orientation="vertical" aria-label={`Resize ${p.name}`} className="absolute top-0 -right-1 z-10 h-full w-2 cursor-col-resize touch-none hover:bg-primary/40" onPointerDown={onResize} />
      ) : null}
      <Floating open={!!anchor} anchor={anchor} onClose={close} autoFocus label={p.name}>
        <PropertyMenu
          prop={p}
          onChange={(next) => (schemaEditable ? props.onSchemaChange(props.schema.map((x) => (x.id === next.id ? next : x))) : undefined)}
          onEdit={schemaEditable && props.onEditProperty ? () => (close(), props.onEditProperty!(p)) : undefined}
          onSort={p.type !== "button" && p.type !== "files" ? (dir) => (close(), props.onConfigChange({ ...props.config, sorts: [{ property: p.id, direction: dir }, ...props.config.sorts.filter((s) => s.property !== p.id)].slice(0, 5) })) : undefined}
          onFilter={props.onAddFilter ? () => (close(), props.onAddFilter!(p)) : undefined}
          onGroup={props.editable && p.type !== "button" && p.type !== "files" ? () => (close(), props.onConfigChange({ ...props.config, group_by: p.id })) : undefined}
          onMove={props.editable && p.type !== "title" ? (dir) => setCols(reorderColumns(props.schema, props.config, "table", pos, pos + dir)) : undefined}
          onFreeze={props.editable ? () => (close(), props.onConfigChange({ ...props.config, freeze_columns: frozenUpTo ? 0 : Math.min(3, index + 1) })) : undefined}
          frozen={frozenUpTo}
          onHide={props.editable && p.type !== "title" ? () => (close(), setCols(setShown(props.schema, props.config, "table", p.id, false))) : undefined}
          onDuplicate={schemaEditable && p.type !== "title" && p.type !== "relation" ? () => (close(), props.onSchemaChange([...props.schema, { ...structuredClone(p), id: newId(), name: `${p.name} (copy)` }])) : undefined}
          onDelete={schemaEditable && p.type !== "title" ? () => (close(), props.onDeleteProperty ? props.onDeleteProperty(p) : props.onSchemaChange(props.schema.filter((x) => x.id !== p.id))) : undefined}
        />
      </Floating>
    </th>
  );
}

function newId(): string {
  const a = "abcdefghijklmnopqrstuvwxyz0123456789";
  const b = new Uint8Array(8);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => a[x % a.length]).join("");
}

function AddPropertyHeader({ props }: { props: ViewProps }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  if (!(props.schemaEditable ?? props.editable)) return null;
  return (
    <th scope="col" className="w-10 border-b border-border bg-surface p-0">
      <IconButton label="Add a property" icon={<Plus />} size="sm" variant="ghost" onClick={(e) => setAnchor(e.currentTarget)} />
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus placement="bottom-end" label="New property">
        <NewPropertyForm
          onCreate={(p) => {
            setAnchor(null);
            props.onSchemaChange([...props.schema, p]);
          }}
          onSetup={
            props.onEditProperty
              ? (p) => {
                  setAnchor(null);
                  props.onEditProperty!(p, true);
                }
              : undefined
          }
        />
      </Floating>
    </th>
  );
}

/** A footer cell: the column's calculation, or a menu to pick one. */
function CalcCell({ p, props, sticky }: { p: DocsProperty; props: ViewProps; sticky: CSSProperties | undefined }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const fn = props.config.calculations?.[p.id];
  const value = fn ? props.meta?.calculations?.[p.id] : undefined;
  const type = p.type === "title" ? "text" : docsValueType(p);
  const fns = p.type === "button" ? [] : docsAggregations(type, true);
  const set = (f: DocsRollupFunction | null) => {
    const next = { ...(props.config.calculations ?? {}) };
    if (f) next[p.id] = f;
    else delete next[p.id];
    props.onConfigChange({ ...props.config, calculations: next });
    setAnchor(null);
  };
  return (
    <td style={sticky} className={cn("border-r border-border bg-surface p-0 last:border-r-0", sticky && "sticky z-10")}>
      <button
        type="button"
        disabled={!props.editable || !fns.length}
        onClick={(e) => setAnchor(e.currentTarget)}
        className={cn(
          "group/calc flex h-8 w-full items-center justify-end gap-1 px-2 type-caption hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent",
          fn ? "text-fg" : "text-fg-subtle",
        )}
      >
        {fn ? (
          <>
            <span className="text-fg-muted">{calcLabel(fn)}</span>
            <span className="truncate font-semibold tabular-nums">{formatCalc(fn, value, p) || "–"}</span>
          </>
        ) : props.editable && fns.length ? (
          <span className="sm:opacity-0 sm:group-hover/calc:opacity-100 sm:group-focus-visible/calc:opacity-100">Calculate</span>
        ) : null}
      </button>
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus placement="top-start" label={`Calculate ${p.name}`}>
        <div role="menu" className="flex max-h-80 w-56 flex-col overflow-y-auto">
          <button type="button" role="menuitemradio" aria-checked={!fn} className={floatingItemClass} onClick={() => set(null)}>
            None
          </button>
          {fns.map((f) => (
            <button key={f} type="button" role="menuitemradio" aria-checked={fn === f} className={cn(floatingItemClass, fn === f && "font-semibold")} onClick={() => set(f)}>
              {calcLabel(f)}
            </button>
          ))}
        </div>
      </Floating>
    </td>
  );
}

/** Nested sub-items of one row (fetched when expanded). */
function SubRows({ parent, depth, props, render }: { parent: DocsRow; depth: number; props: ViewProps; render: (row: DocsRow, depth: number) => ReactNode }) {
  const params = useMemo(() => ({ view: props.view.id, parent: parent.id, tz: docsTimeZone(), limit: 100 }), [props.view.id, parent.id]);
  const sub = useDocsRows(props.db.id, params, { token: props.token ?? null });
  const span = visibleProps(props.schema, props.config, "table").length + 1;
  if (sub.isPending) {
    return (
      <tr>
        <td colSpan={span} className="border-b border-border p-2" style={{ paddingLeft: 16 + depth * 20 }}>
          <Skeleton className="h-5 w-1/2" />
        </td>
      </tr>
    );
  }
  if (sub.isError) {
    return (
      <tr>
        <td colSpan={span} className="border-b border-border px-2 py-2 type-caption text-danger-strong" style={{ paddingLeft: 16 + depth * 20 }}>
          Couldn't load the sub-items.
        </td>
      </tr>
    );
  }
  return (
    <>
      {sub.data.rows.map((r) => render(r, depth))}
      {props.editable && props.onCreateSubRow && rowEditable(parent, props.editable) ? (
        <tr>
          <td colSpan={span} className="border-b border-border p-0">
            <button
              type="button"
              onClick={() => props.onCreateSubRow!(parent)}
              style={{ paddingLeft: 12 + depth * 20 }}
              className="flex min-h-8 w-full items-center gap-2 type-caption text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
            >
              <Plus aria-hidden className="size-3.5" />
              New sub-item
            </button>
          </td>
        </tr>
      ) : null}
    </>
  );
}

function ExpandToggle({ row, open, onToggle, depth }: { row: DocsRow; open: boolean; onToggle: () => void; depth: number }) {
  const n = row.sub_count ?? 0;
  return (
    <span className="flex shrink-0 items-center" style={{ paddingLeft: depth * 20 }}>
      {depth > 0 ? <CornerDownRight aria-hidden className="mr-1 size-3.5 text-fg-subtle" /> : null}
      <button
        type="button"
        aria-expanded={open}
        aria-label={open ? `Hide sub-items of ${docsTitle(row.title)}` : n ? `Show ${n} sub-items of ${docsTitle(row.title)}` : `Add sub-items to ${docsTitle(row.title)}`}
        onClick={(e) => {
          e.stopPropagation();
          onToggle();
        }}
        className={cn("flex size-6 items-center justify-center rounded-sm hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", n ? "text-fg-muted" : "text-fg-subtle")}
      >
        {open ? <ChevronDown aria-hidden className="size-4" /> : <ChevronRight aria-hidden className="size-4" />}
      </button>
    </span>
  );
}

export function TableView(props: ViewProps) {
  const cfg = props.config;
  const cols = orderedColumns(props.schema, cfg);
  const shown = visibleProps(props.schema, cfg, "table");
  const widthOf = (p: DocsProperty) => cols.find((c) => c.property === p.id)?.width ?? (p.type === "title" ? 280 : 180);
  const freeze = Math.min(cfg.freeze_columns ?? 0, shown.length);
  const lefts: number[] = [];
  shown.reduce((acc, p, i) => {
    lefts[i] = acc;
    return acc + widthOf(p);
  }, 0);
  // Frozen columns stay put only where there's room beside them: on a phone they'd cover every other column.
  const roomy = useMediaQuery("(min-width: 640px)", true);
  const stickyAt = (i: number): CSSProperties | undefined => (roomy && i < freeze ? { left: lefts[i] } : undefined);
  const wrap = !!cfg.wrap;
  const dense = cfg.density === "compact";
  const groupProp = props.schema.find((p) => p.id === cfg.group_by);
  const groups = groupProp ? props.meta?.groups : undefined;
  const nested = cfg.sub_items?.display === "nested" && !props.publicView;
  const [open, setOpen] = useState<Set<string>>(new Set());
  const gs = useGroupState(props);
  const toggle = (id: string) =>
    setOpen((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });
  const span = shown.length + 1;
  const showCalcs = props.editable || Object.keys(cfg.calculations ?? {}).length > 0;

  const renderRow = (row: DocsRow, depth = 0): ReactNode => (
    <Fragment key={`${row.id}-${depth}`}>
      <tr className="group/row hover:bg-sunken/50">
        {shown.map((p, i) => (
          <td key={p.id} style={stickyAt(i)} className={cn("relative border-b border-r border-border p-0 align-top last:border-r-0", roomy && i < freeze && "sticky z-10 bg-surface group-hover/row:bg-sunken")}>
            {p.type === "title" ? (
              <div className={cn("flex items-center", dense && "[&_.min-h-control]:min-h-8")}>
                <div className="min-w-0 flex-1">
                  <TitleCell row={row} props={props} wrap={wrap} lead={nested ? <ExpandToggle row={row} open={open.has(row.id)} onToggle={() => toggle(row.id)} depth={depth} /> : undefined} />
                </div>
                <span className="shrink-0 opacity-100 sm:opacity-0 sm:group-hover/row:opacity-100 sm:focus-within:opacity-100">
                  <RowMenu row={row} props={props} />
                </span>
              </div>
            ) : (
              <EditableValue p={p} row={row} props={props} wrap={wrap} className={dense ? "min-h-8" : undefined} />
            )}
          </td>
        ))}
        {props.schemaEditable ?? props.editable ? <td className="border-b border-border" /> : null}
      </tr>
      {nested && open.has(row.id) && depth < 5 ? <SubRows parent={row} depth={depth + 1} props={props} render={renderRow} /> : null}
    </Fragment>
  );

  const body = () => {
    if (props.loading && props.rows.length === 0) {
      return Array.from({ length: 3 }, (_, i) => (
        <tr key={i}>
          <td colSpan={span} className="border-b border-border p-2">
            <Skeleton className="h-6 w-full" />
          </td>
        </tr>
      ));
    }
    if (!groups) return props.rows.map((r) => renderRow(r));
    const sections = docsGroupRowsBy(props.rows, groups);
    return sections
      .filter((s) => !gs.isHidden(s.group) && !(s.group.key === "" && s.group.count === 0))
      .map(({ group, rows }) => {
        const collapsed = gs.isCollapsed(group.key);
        const preset = groupValue(groupProp, group.key);
        return (
          <Fragment key={`g-${group.key}`}>
            <tr>
              <td colSpan={span} className="border-b border-border bg-surface p-0">
                <div className="sticky left-0 w-fit">
                  <GroupHeader
                    group={group}
                    prop={groupProp}
                    props={props}
                    collapsed={collapsed}
                    onToggle={() => gs.toggle(group.key)}
                    onAdd={props.editable && preset !== undefined ? () => props.onCreateRow({ [groupProp!.id]: preset }) : undefined}
                    onHide={props.editable ? () => gs.hide(group.key) : undefined}
                  />
                </div>
              </td>
            </tr>
            {collapsed ? null : rows.map((r) => renderRow(r))}
          </Fragment>
        );
      });
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="overflow-x-auto rounded-md border border-border">
        <table className="w-max min-w-full table-fixed border-collapse">
          <thead>
            <tr>
              {shown.map((p, i) => (
                <HeaderCell key={p.id} p={p} props={props} index={i} width={widthOf(p)} sticky={stickyAt(i)} frozenUpTo={freeze > 0 && i === freeze - 1} />
              ))}
              <AddPropertyHeader props={props} />
            </tr>
          </thead>
          <tbody>{body()}</tbody>
          {showCalcs && props.rows.length > 0 ? (
            <tfoot>
              <tr>
                {shown.map((p, i) => (
                  <CalcCell key={p.id} p={p} props={props} sticky={stickyAt(i)} />
                ))}
                {props.schemaEditable ?? props.editable ? <td className="bg-surface" /> : null}
              </tr>
            </tfoot>
          ) : null}
        </table>
        {!props.loading && props.rows.length === 0 ? <p className="px-3 py-4 type-label text-fg-muted">No rows match this view.</p> : null}
      </div>
      {groups ? <HiddenGroups groups={groups.filter((g) => gs.isHidden(g))} prop={groupProp} props={props} onShow={props.editable ? gs.show : undefined} /> : null}
      <LoadMoreRow props={props} />
      <NewRowButton props={props} />
    </div>
  );
}

// ---- list ----------------------------------------------------------------------------

function ListRows({ rows, props, depth = 0 }: { rows: DocsRow[]; props: ViewProps; depth?: number }) {
  const shown = visibleProps(props.schema, props.config, "list").filter((p) => p.type !== "title");
  const nested = props.config.sub_items?.display === "nested" && !props.publicView;
  const [open, setOpen] = useState<Set<string>>(new Set());
  return (
    <>
      {rows.map((row) => (
        <Fragment key={row.id}>
          <li className={cn("flex flex-wrap items-center gap-x-3 gap-y-1 px-2 py-1", props.config.density === "compact" ? "min-h-9" : "min-h-12")}>
            <div className="min-w-40 flex-1">
              <TitleCell
                row={row}
                props={props}
                wrap={!!props.config.wrap}
                lead={
                  nested ? (
                    <ExpandToggle
                      row={row}
                      open={open.has(row.id)}
                      depth={depth}
                      onToggle={() =>
                        setOpen((s) => {
                          const n = new Set(s);
                          if (n.has(row.id)) n.delete(row.id);
                          else n.add(row.id);
                          return n;
                        })
                      }
                    />
                  ) : undefined
                }
              />
            </div>
            {shown.map((p) => (
              <div key={p.id} className="max-w-60 min-w-0">
                <EditableValue p={p} row={row} props={props} className="min-h-8" />
              </div>
            ))}
            <RowMenu row={row} props={props} />
          </li>
          {nested && open.has(row.id) && depth < 5 ? <ListSubRows parent={row} props={props} depth={depth + 1} /> : null}
        </Fragment>
      ))}
    </>
  );
}

function ListSubRows({ parent, props, depth }: { parent: DocsRow; props: ViewProps; depth: number }) {
  const params = useMemo(() => ({ view: props.view.id, parent: parent.id, tz: docsTimeZone(), limit: 100 }), [props.view.id, parent.id]);
  const sub = useDocsRows(props.db.id, params, { token: props.token ?? null });
  if (sub.isPending) {
    return (
      <li className="p-2" style={{ paddingLeft: 16 + depth * 20 }}>
        <Skeleton className="h-5 w-1/2" />
      </li>
    );
  }
  if (sub.isError) {
    return (
      <li className="px-2 py-2 type-caption text-danger-strong" style={{ paddingLeft: 16 + depth * 20 }}>
        Couldn't load the sub-items.
      </li>
    );
  }
  return (
    <>
      <ListRows rows={sub.data.rows} props={props} depth={depth} />
      {props.editable && props.onCreateSubRow && rowEditable(parent, props.editable) ? (
        <li>
          <button
            type="button"
            onClick={() => props.onCreateSubRow!(parent)}
            style={{ paddingLeft: 12 + depth * 20 }}
            className="flex min-h-8 w-full items-center gap-2 type-caption text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
          >
            <Plus aria-hidden className="size-3.5" />
            New sub-item
          </button>
        </li>
      ) : null}
    </>
  );
}

export function ListView(props: ViewProps) {
  const groupProp = props.schema.find((p) => p.id === props.config.group_by);
  const groups = groupProp ? props.meta?.groups : undefined;
  const gs = useGroupState(props);
  const skeleton = (
    <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
      {Array.from({ length: 3 }, (_, i) => (
        <li key={i} className="p-3">
          <Skeleton className="h-6 w-2/3" />
        </li>
      ))}
    </ul>
  );
  if (props.loading && props.rows.length === 0) return skeleton;
  return (
    <div className="flex flex-col gap-2">
      {groups ? (
        docsGroupRowsBy(props.rows, groups)
          .filter((s) => !gs.isHidden(s.group) && !(s.group.key === "" && s.group.count === 0))
          .map(({ group, rows }) => {
            const preset = groupValue(groupProp, group.key);
            return (
              <section key={group.key} aria-label={group.label} className="flex flex-col gap-1">
                <GroupHeader
                  group={group}
                  prop={groupProp}
                  props={props}
                  collapsed={gs.isCollapsed(group.key)}
                  onToggle={() => gs.toggle(group.key)}
                  onAdd={props.editable && preset !== undefined ? () => props.onCreateRow({ [groupProp!.id]: preset }) : undefined}
                  onHide={props.editable ? () => gs.hide(group.key) : undefined}
                />
                {gs.isCollapsed(group.key) ? null : rows.length ? (
                  <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
                    <ListRows rows={rows} props={props} />
                  </ul>
                ) : (
                  <p className="px-3 py-2 type-caption text-fg-muted">No rows.</p>
                )}
              </section>
            );
          })
      ) : (
        <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
          <ListRows rows={props.rows} props={props} />
        </ul>
      )}
      {!props.loading && props.rows.length === 0 ? <p className="px-3 py-4 type-label text-fg-muted">No rows match this view.</p> : null}
      {groups ? <HiddenGroups groups={groups.filter((g) => gs.isHidden(g))} prop={groupProp} props={props} onShow={props.editable ? gs.show : undefined} /> : null}
      <LoadMoreRow props={props} />
      <NewRowButton props={props} />
    </div>
  );
}

// ---- board ---------------------------------------------------------------------------

/** Server groups, or option groups from the rows while the first answer loads (and for connected databases). */
function boardGroups(props: ViewProps, group: DocsProperty): DocsGroupInfo[] {
  if (props.meta?.groups) return props.meta.groups;
  return docsGroupRows(props.rows, group).map((g) => ({ key: g.option?.id ?? "", label: g.option?.name ?? `No ${group.name.toLowerCase()}`, ...(g.option ? { color: g.option.color } : {}), count: g.rows.length }));
}

function rowGroups(row: DocsRow, group: DocsProperty, fromServer: boolean): string[] {
  if (fromServer && row.groups) return row.groups.length ? row.groups : [""];
  const v = row.props[group.id];
  if (typeof v === "string") return [v];
  return [""];
}

export function BoardView(props: ViewProps) {
  const cfg = props.config;
  const group = props.schema.find((p) => p.id === cfg.group_by);
  const sub = props.schema.find((p) => p.id === cfg.sub_group_by);
  const shown = visibleProps(props.schema, cfg, "board").filter((p) => p.type !== "title" && p.id !== group?.id && p.id !== sub?.id);
  const gs = useGroupState(props);
  const ss = useGroupState(props, "sub_group");
  const [over, setOver] = useState<string | null>(null);
  const dragging = useRef<{ row: DocsRow; from: string; lane: string | null } | null>(null);
  const sorted = useMemo(() => (cfg.sorts.length ? props.rows : [...props.rows].sort(docsComparePosition)), [props.rows, cfg.sorts.length]);
  if (!group) return <p className="px-3 py-4 type-label text-fg-muted">Choose a property to group this board by in the view's settings.</p>;
  const fromServer = !!props.meta?.groups;
  const groups = boardGroups(props, group);
  const columns = groups.filter((g) => !gs.isHidden(g));
  const lanes: (DocsGroupInfo | null)[] = sub && props.meta?.sub_groups ? props.meta.sub_groups.filter((g) => !ss.isHidden(g) && !(g.key === "" && g.count === 0)) : [null];
  const preview = cfg.card?.preview && cfg.card.preview !== "none" && cfg.card.preview !== "cover" && cfg.card.preview !== "content" ? cfg.card.preview : undefined;
  const draggable = props.editable && groupValue(group, "") !== undefined;
  const inCell = (row: DocsRow, col: string, lane: DocsGroupInfo | null) => rowGroups(row, group, fromServer).includes(col) && (!lane || (row.sub_groups?.length ? row.sub_groups : [""]).includes(lane.key));
  const dropInto = (col: string, lane: DocsGroupInfo | null, before: DocsRow | null, list: DocsRow[]) => {
    const d = dragging.current;
    dragging.current = null;
    setOver(null);
    if (!d) return;
    const values: Record<string, DocsValue> = {};
    if (d.from !== col) {
      const v = moveGroupValue(group, d.from, col, d.row.props[group.id]);
      if (v === undefined) return;
      values[group.id] = v;
    }
    if (lane && sub && d.lane !== lane.key) {
      const v = moveGroupValue(sub, d.lane ?? "", lane.key, d.row.props[sub.id]);
      if (v !== undefined) values[sub.id] = v;
    }
    let position: string | undefined;
    if (!cfg.sorts.length) {
      const rest = list.filter((r) => r.id !== d.row.id);
      const i = before ? rest.findIndex((r) => r.id === before.id) : rest.length;
      position = docsKeyBetween(i > 0 ? rest[i - 1]!.position : null, i < rest.length ? rest[i]!.position : null);
    }
    if (!Object.keys(values).length && !position) return;
    props.onMoveRow(d.row, { ...(Object.keys(values).length ? { values } : {}), ...(position ? { position } : {}) });
  };
  const column = (g: DocsGroupInfo, lane: DocsGroupInfo | null) => {
    const id = `${lane?.key ?? "-"}|${g.key}`;
    const list = sorted.filter((r) => inCell(r, g.key, lane));
    const collapsed = gs.isCollapsed(g.key);
    const preset = groupValue(group, g.key);
    const lanePreset = lane && sub ? groupValue(sub, lane.key) : undefined;
    if (collapsed && lane) return <div key={id} aria-hidden className="w-11 shrink-0 rounded-md bg-sunken" />;
    if (collapsed) {
      return (
        <section key={id} aria-label={g.label} className="flex w-11 shrink-0 flex-col items-center gap-2 rounded-md bg-sunken py-2">
          <IconButton label={`Expand ${g.label}`} icon={<ChevronRight />} size="sm" variant="ghost" onClick={() => gs.toggle(g.key)} />
          <span className="type-caption tabular-nums text-fg-muted">{lane ? list.length : g.count}</span>
          <span className="[writing-mode:vertical-rl]">
            <GroupLabel group={g} prop={group} refs={props.refs} />
          </span>
        </section>
      );
    }
    return (
      <section
        key={id}
        aria-label={g.label}
        className={cn("flex w-64 shrink-0 flex-col gap-2 rounded-md bg-sunken p-2", over === id && "ring-2 ring-primary")}
        onDragOver={(e) => {
          if (!draggable || !dragging.current) return;
          e.preventDefault();
          setOver(id);
        }}
        onDragLeave={() => setOver((o) => (o === id ? null : o))}
        onDrop={(e) => {
          e.preventDefault();
          dropInto(g.key, lane, null, list);
        }}
      >
        {lane ? null : (
          <GroupHeader group={g} prop={group} props={props} collapsed={false} onToggle={() => gs.toggle(g.key)} onHide={props.editable ? () => gs.hide(g.key) : undefined} />
        )}
        {props.loading && props.rows.length === 0 ? <Skeleton className="h-16 w-full rounded-md" /> : null}
        {list.map((row) => (
          <article
            key={row.id}
            draggable={draggable && rowEditable(row, props.editable)}
            onDragStart={(e) => {
              dragging.current = { row, from: g.key, lane: lane?.key ?? null };
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
              dropInto(g.key, lane, row, list);
            }}
            className="group/card flex flex-col gap-1.5 overflow-hidden rounded-md border border-border bg-surface"
          >
            {preview && firstImage(row, preview) ? <CardImage file={firstImage(row, preview)!} fit={cfg.card?.fit} className="h-28 border-b border-border" token={props.token} /> : null}
            <div className="flex flex-col gap-1 p-2">
              <CardTitle row={row} props={props} extra={draggable && rowEditable(row, props.editable) ? <MoveCardMenu row={row} group={group} groups={groups} from={g.key} props={props} /> : null} />
              <CardProps row={row} props={props} shown={shown} />
            </div>
          </article>
        ))}
        {props.editable && preset !== undefined ? <NewRowButton props={props} values={{ [group.id]: preset, ...(lanePreset !== undefined && sub ? { [sub.id]: lanePreset } : {}) }} /> : null}
      </section>
    );
  };
  const swim = !!lanes[0];
  return (
    <div className="flex flex-col gap-3">
      {swim ? (
        <div className="overflow-x-auto pb-2">
          <div className="flex w-max flex-col gap-3">
            <div className="sticky top-0 z-10 flex gap-3 bg-surface">
              {columns.map((g) =>
                gs.isCollapsed(g.key) ? (
                  <div key={g.key} className="flex w-11 shrink-0 justify-center">
                    <IconButton label={`Expand ${g.label}`} icon={<ChevronRight />} size="sm" variant="ghost" onClick={() => gs.toggle(g.key)} />
                  </div>
                ) : (
                  <div key={g.key} className="w-64 shrink-0">
                    <GroupHeader group={g} prop={group} props={props} collapsed={false} onToggle={() => gs.toggle(g.key)} onHide={props.editable ? () => gs.hide(g.key) : undefined} />
                  </div>
                ),
              )}
            </div>
            {lanes.map((lane) =>
              lane ? (
                <section key={lane.key} aria-label={lane.label} className="flex flex-col gap-2">
                  <div className="sticky left-0 w-fit">
                    <GroupHeader group={lane} prop={sub} props={props} collapsed={ss.isCollapsed(lane.key)} onToggle={() => ss.toggle(lane.key)} onHide={props.editable ? () => ss.hide(lane.key) : undefined} />
                  </div>
                  {ss.isCollapsed(lane.key) ? null : <div className="flex gap-3">{columns.map((g) => column(g, lane))}</div>}
                </section>
              ) : null,
            )}
          </div>
        </div>
      ) : (
        <div className="flex gap-3 overflow-x-auto pb-2">{columns.map((g) => column(g, null))}</div>
      )}
      <HiddenGroups groups={groups.filter((g) => gs.isHidden(g))} prop={group} props={props} onShow={props.editable ? gs.show : undefined} />
      {sub && props.meta?.sub_groups ? <HiddenGroups groups={props.meta.sub_groups.filter((g) => ss.isHidden(g))} prop={sub} props={props} onShow={props.editable ? ss.show : undefined} /> : null}
      <LoadMoreRow props={props} />
    </div>
  );
}

/** Keyboard and touch alternative to dragging a card. */
function MoveCardMenu({ row, group, groups, from, props }: { row: DocsRow; group: DocsProperty; groups: DocsGroupInfo[]; from: string; props: ViewProps }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  return (
    <>
      <button
        type="button"
        aria-label={`Move ${docsTitle(row.title)} to another group`}
        className="shrink-0 rounded-sm px-1 type-caption text-fg-subtle hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring sm:opacity-0 sm:group-hover/card:opacity-100 sm:focus-visible:opacity-100"
        onClick={(e) => setAnchor(e.currentTarget)}
      >
        Move
      </button>
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus label="Move to">
        <div role="menu" className="flex max-h-80 flex-col overflow-y-auto">
          {groups.map((g) => (
            <button
              key={g.key}
              type="button"
              role="menuitem"
              disabled={g.key === from}
              className={floatingItemClass}
              onClick={() => {
                setAnchor(null);
                const v = moveGroupValue(group, from, g.key, row.props[group.id]);
                if (v !== undefined) props.onMoveRow(row, { values: { [group.id]: v } });
              }}
            >
              <GroupLabel group={g} prop={group} refs={props.refs} />
            </button>
          ))}
        </div>
      </Floating>
    </>
  );
}

// ---- calendar ----------------------------------------------------------------------

function isoDay(d: Date): string {
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}-${String(d.getUTCDate()).padStart(2, "0")}`;
}

function monthDays(year: number, month: number): string[] {
  const first = new Date(Date.UTC(year, month, 1));
  const startDow = (first.getUTCDay() + 6) % 7; // Monday first
  const out: string[] = [];
  for (let i = -startDow; out.length < 42; i++) out.push(isoDay(new Date(Date.UTC(year, month, 1 + i))));
  return out;
}

function addDays(day: string, n: number): string {
  const [y, m, d] = day.split("-").map(Number);
  return isoDay(new Date(Date.UTC(y!, m! - 1, d! + n)));
}

function daysBetween(a: string, b: string): number {
  const [y1, m1, d1] = a.split("-").map(Number);
  const [y2, m2, d2] = b.split("-").map(Number);
  return Math.round((Date.UTC(y2!, m2! - 1, d2!) - Date.UTC(y1!, m1! - 1, d1!)) / 86_400_000);
}

/** The day a date-ish value starts and ends on (created/edited times in the viewer's zone). */
export function rowDates(row: DocsRow, p: DocsProperty | undefined, end?: DocsProperty): { start: string; end: string; value: DocsDateValue | null } | null {
  if (!p) return null;
  let v: DocsDateValue | null = null;
  if (p.type === "created_time" || p.type === "last_edited_time") {
    const d = new Date(p.type === "created_time" ? row.created_at : row.updated_at);
    const s = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
    return { start: s, end: s, value: null };
  }
  const raw = row.props[p.id] as DocsDateValue | null | undefined;
  if (raw && typeof raw === "object" && typeof raw.start === "string") v = raw;
  if (!v) return null;
  const s = v.start.slice(0, 10);
  let e = v.end?.slice(0, 10) ?? s;
  if (end) {
    const ev = row.props[end.id] as DocsDateValue | null | undefined;
    if (ev && typeof ev === "object" && typeof ev.start === "string") e = (ev.end ?? ev.start).slice(0, 10);
  }
  if (e < s) e = s;
  return { start: s, end: e, value: v };
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
    const first = days[0]!;
    const last = days[days.length - 1]!;
    for (const r of props.rows) {
      const d = rowDates(r, dateProp);
      if (!d) continue;
      // Ranges show on each of their days in the grid (up to 6 weeks).
      let day = d.start < first ? first : d.start;
      const stop = d.end > last ? last : d.end;
      for (let i = 0; day <= stop && i < 42; i++, day = addDays(day, 1)) m.set(day, [...(m.get(day) ?? []), r]);
    }
    return m;
  }, [props.rows, dateProp, days]);
  if (!dateProp) return <p className="px-3 py-4 type-label text-fg-muted">Choose a date property for this calendar in the view's settings.</p>;
  const movable = dateProp.type === "date";
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
                  if (!dragging.current || !movable) return;
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
                  if (!cur?.start) return;
                  const shiftBy = daysBetween(cur.start.slice(0, 10), d);
                  const time = cur.start.slice(10);
                  const next: DocsDateValue = { ...cur, start: d + time, ...(cur.end ? { end: addDays(cur.end.slice(0, 10), shiftBy) + cur.end.slice(10) } : {}) };
                  props.onMoveRow(row, { values: { [dateProp.id]: next } });
                }}
              >
                <div className="flex items-center justify-between">
                  <span className={cn("flex size-6 items-center justify-center rounded-pill type-caption tabular-nums", d === todayIso ? "bg-primary text-primary-fg" : inMonth ? "text-fg" : "text-fg-subtle")}>{Number(d.slice(8))}</span>
                  {props.editable && movable ? (
                    <button
                      type="button"
                      aria-label={`New row on ${d}`}
                      className="flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring sm:opacity-0 sm:group-hover/day:opacity-100"
                      onClick={() => props.onCreateRow({ [dateProp.id]: { start: d } })}
                    >
                      <Plus aria-hidden className="size-4" />
                    </button>
                  ) : null}
                </div>
                {list.map((row) => {
                  const v = row.props[dateProp.id] as DocsDateValue | undefined;
                  const time = v?.start && v.start.length > 10 && v.start.slice(0, 10) === d ? v.start.slice(11, 16) : "";
                  return (
                    <button
                      key={row.id}
                      type="button"
                      draggable={movable && rowEditable(row, props.editable)}
                      onDragStart={(e) => {
                        dragging.current = row;
                        e.dataTransfer.effectAllowed = "move";
                        e.dataTransfer.setData("text/plain", docsTitle(row.title));
                      }}
                      onClick={() => props.onOpenRow(row.id)}
                      className="flex flex-col rounded-sm border border-border bg-surface px-1.5 py-0.5 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                    >
                      <span className="truncate type-caption font-medium text-fg">
                        {time ? <span className="mr-1 tabular-nums text-fg-muted">{time}</span> : null}
                        {docsTitle(row.title)}
                      </span>
                    </button>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
      {props.hasMore ? <LoadMoreRow props={props} label="Load more rows" /> : null}
    </div>
  );
}
