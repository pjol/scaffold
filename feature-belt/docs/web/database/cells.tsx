"use client";

import { docsIsComputed, docsTitle, type DocsGroupInfo, type DocsProperty, type DocsRow, type DocsValue } from "@scaffoldapp/shared";
import { Avatar, Button, IconButton, Spinner, cn } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronRight, Copy, ExternalLink, FileText, Maximize2, MousePointerClick, Pencil, Plus, Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { Floating } from "../Floating";
import { rowEditable } from "./model";
import { hasLinks, isEmptyValue, OptionBadge, PropertyDisplay, PropertyEditor } from "./PropertyValue";
import type { ViewProps } from "./types";

/** Whether a property's value is set by hand in this view. */
export function valueEditable(p: DocsProperty, row: DocsRow, props: ViewProps): boolean {
  return rowEditable(row, props.editable) && !p.read_only && !docsIsComputed(p) && p.type !== "title";
}

/** A row's value for a property, with created/edited stamps. */
export function Value({ p, row, props, compact = true, links = false }: { p: DocsProperty; row: DocsRow; props: ViewProps; compact?: boolean; links?: boolean }) {
  return <PropertyDisplay prop={p} value={row.props[p.id]} refs={props.refs} compact={compact} row={row} links={links} token={props.token ?? null} />;
}

/** A button property's button. */
export function ButtonCell({ p, row, props, className }: { p: DocsProperty; row: DocsRow; props: ViewProps; className?: string }) {
  const [busy, setBusy] = useState(false);
  const can = rowEditable(row, props.editable) && !!props.onPressButton;
  return (
    <div className={cn("flex min-h-control items-center px-2", className)}>
      <button
        type="button"
        disabled={!can || busy}
        onClick={(e) => {
          e.stopPropagation();
          if (!props.onPressButton) return;
          setBusy(true);
          props.onPressButton(row, p.id);
          window.setTimeout(() => setBusy(false), 800);
        }}
        className="inline-flex min-h-7 max-w-full items-center gap-1.5 rounded-sm border border-border bg-surface px-2 type-caption font-medium text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default disabled:opacity-60 pointer-coarse:min-h-tap"
      >
        {busy ? <Spinner /> : <MousePointerClick aria-hidden className="size-3.5 shrink-0" />}
        <span className="truncate">{p.button?.label || p.name}</span>
      </button>
    </div>
  );
}

/** A cell that opens an editor for its value; links stay clickable beside an edit button. */
export function EditableValue({ p, row, props, children, className, wrap }: { p: DocsProperty; row: DocsRow; props: ViewProps; children?: ReactNode; className?: string; wrap?: boolean }) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const canEdit = valueEditable(p, row, props);
  const value = row.props[p.id];
  if (p.type === "button") return <ButtonCell p={p} row={row} props={props} {...(className ? { className } : {})} />;
  const content = children ?? <Value p={p} row={row} props={props} compact={!wrap} />;
  if (p.type === "checkbox") {
    return (
      <button
        type="button"
        role="checkbox"
        aria-checked={value === true}
        aria-label={p.name}
        disabled={!canEdit}
        className={cn("flex min-h-control w-full items-center px-2 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring disabled:cursor-default", className)}
        onClick={(e) => {
          e.stopPropagation();
          props.onSetValue(row, p.id, value !== true);
        }}
      >
        {content}
      </button>
    );
  }
  const editor = (
    <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus label={p.name}>
      <PropertyEditor
        prop={p}
        value={value}
        orgId={props.orgId}
        rowId={props.external ? undefined : row.id}
        refs={props.refs}
        onChange={(v: DocsValue) => props.onSetValue(row, p.id, v)}
        onSchema={(props.schemaEditable ?? props.editable) ? (next) => props.onSchemaChange(props.schema.map((x) => (x.id === next.id ? next : x))) : undefined}
        onDone={() => setAnchor(null)}
      />
    </Floating>
  );
  if (hasLinks(p) && !isEmptyValue(value)) {
    // Links can't sit inside a button: the value, then an edit button.
    return (
      <div className={cn("group/cell flex min-h-control w-full min-w-0 items-center gap-1 px-2", wrap && "py-1", className)}>
        <div className={cn("min-w-0 flex-1", wrap ? "whitespace-normal" : "overflow-hidden")}>{children ?? <Value p={p} row={row} props={props} compact={!wrap} links />}</div>
        {canEdit ? (
          <span className="shrink-0 sm:opacity-0 sm:group-hover/cell:opacity-100 sm:focus-within:opacity-100">
            <IconButton
              label={`Edit ${p.name}`}
              icon={<Pencil />}
              size="sm"
              variant="ghost"
              onClick={(e) => {
                e.stopPropagation();
                setAnchor(e.currentTarget);
              }}
            />
          </span>
        ) : null}
        {editor}
      </div>
    );
  }
  return (
    <>
      <button
        type="button"
        disabled={!canEdit}
        aria-label={canEdit ? `${p.name}, edit` : p.name}
        className={cn(
          "flex min-h-control w-full min-w-0 items-center px-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent",
          wrap && "py-1",
          className,
        )}
        onClick={(e) => {
          e.stopPropagation();
          setAnchor(e.currentTarget);
        }}
      >
        <span className={cn("min-w-0 flex-1", wrap ? "whitespace-normal break-words" : "overflow-hidden")}>{content}</span>
      </button>
      {editor}
    </>
  );
}

export function RowMenu({ row, props, extra = [] }: { row: DocsRow; props: ViewProps; extra?: { id: string; label: string; icon: typeof Plus; onSelect: () => void }[] }) {
  if (props.publicView) return null;
  if (props.external) {
    const ext = props.external;
    return (
      <ResourceMenu
        feature={`connectors:${ext.connectionId}`}
        kind="docs.external"
        id={`${ext.connectionId}:${row.ref ?? row.id}`}
        title={docsTitle(row.title)}
        label={`More actions for ${docsTitle(row.title)}`}
        actions={[
          { id: "open", label: "Open", icon: Maximize2, onSelect: () => props.onOpenRow(row.id) },
          ...(row.url ? [{ id: "platform", label: `Open in ${ext.platform}`, icon: ExternalLink, onSelect: () => void window.open(row.url, "_blank", "noopener,noreferrer") }] : []),
        ]}
      />
    );
  }
  const canEdit = rowEditable(row, props.editable);
  return (
    <ResourceMenu
      feature="docs"
      kind="docs.page"
      id={row.id}
      title={docsTitle(row.title)}
      label={`More actions for ${docsTitle(row.title)}`}
      actions={[
        { id: "open", label: "Open", icon: Maximize2, onSelect: () => props.onOpenRow(row.id) },
        ...extra,
        ...(canEdit ? [{ id: "dup", label: "Duplicate", icon: Copy, onSelect: () => props.onDuplicateRow(row) }] : []),
        ...(canEdit ? [{ id: "trash", label: "Move to trash", icon: Trash2, destructive: true, onSelect: () => props.onTrashRow(row) }] : []),
      ]}
    />
  );
}

export function TitleCell({ row, props, wrap, lead }: { row: DocsRow; props: ViewProps; wrap?: boolean; lead?: ReactNode }) {
  const canEdit = rowEditable(row, props.editable);
  const [editing, setEditing] = useState(canEdit && props.autoEditRow === row.id);
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
      {lead}
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-2 text-left focus-visible:outline-2 focus-visible:outline-ring"
        onClick={() => props.onOpenRow(row.id)}
        onDoubleClick={() => canEdit && setEditing(true)}
      >
        {row.icon ? <span aria-hidden>{row.icon}</span> : <FileText aria-hidden className="size-4 shrink-0 text-fg-muted" />}
        <span className={cn("type-label font-medium text-fg underline-offset-2 hover:underline", wrap ? "whitespace-normal break-words" : "truncate")}>{docsTitle(row.title)}</span>
      </button>
      {canEdit ? (
        <button
          type="button"
          className="shrink-0 rounded-sm px-1 type-caption text-fg-subtle opacity-0 hover:bg-sunken group-hover/title:opacity-100 focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring"
          onClick={() => setEditing(true)}
        >
          Rename
        </button>
      ) : null}
    </div>
  );
}

export function LoadMoreRow({ props, label = "Load more" }: { props: ViewProps; label?: string }) {
  if (!props.hasMore) return null;
  return (
    <div className="flex items-center justify-center gap-2 py-2">
      <Button variant="ghost" size="sm" onClick={props.loadMore}>
        {label}
      </Button>
      {props.meta?.total !== undefined ? (
        <span className="type-caption tabular-nums text-fg-muted">
          {props.rows.length} of {props.meta.total}
        </span>
      ) : null}
    </div>
  );
}

export function NewRowButton({ props, values, label = "New", className }: { props: ViewProps; values?: Record<string, DocsValue> | undefined; label?: string; className?: string }) {
  if (!props.editable) return null;
  return (
    <button
      type="button"
      onClick={() => props.onCreateRow(values)}
      className={cn(
        "flex min-h-control w-full items-center gap-2 rounded-sm px-2 type-label text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap",
        className,
      )}
    >
      <Plus aria-hidden className="size-4" />
      {label}
    </button>
  );
}

/** A group's name as its header shows it: an option chip, a person, or text. */
export function GroupLabel({ group, prop, refs }: { group: DocsGroupInfo; prop: DocsProperty | undefined; refs: ViewProps["refs"] }) {
  if (group.key === "") return <span className="truncate type-label text-fg-muted">{group.label}</span>;
  if (prop && (prop.type === "select" || prop.type === "status" || prop.type === "multi_select")) return <OptionBadge option={{ name: group.label, color: group.color ?? "default" }} status={prop.type === "status"} />;
  if (prop && (prop.type === "person" || prop.type === "created_by" || prop.type === "last_edited_by")) {
    const u = refs.users[group.key];
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <Avatar size="xs" name={group.label} {...(u?.avatar_url ? { src: u.avatar_url } : {})} />
        <span className="truncate type-label font-medium text-fg">{group.label}</span>
      </span>
    );
  }
  return <span className="truncate type-label font-medium text-fg">{group.label}</span>;
}

/** A collapsible group header row content. */
export function GroupHeader({ group, prop, props, collapsed, onToggle, onAdd, onHide }: { group: DocsGroupInfo; prop: DocsProperty | undefined; props: ViewProps; collapsed: boolean; onToggle: () => void; onAdd?: (() => void) | undefined; onHide?: (() => void) | undefined }) {
  return (
    <div className="group/gh flex min-h-control items-center gap-2 px-1">
      <button type="button" aria-expanded={!collapsed} onClick={onToggle} className="flex min-h-control min-w-0 items-center gap-2 rounded-sm px-1 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
        {collapsed ? <ChevronRight aria-hidden className="size-4 shrink-0 text-fg-muted" /> : <ChevronDown aria-hidden className="size-4 shrink-0 text-fg-muted" />}
        <GroupLabel group={group} prop={prop} refs={props.refs} />
        <span className="type-caption tabular-nums text-fg-muted">{group.count}</span>
      </button>
      <span className="flex items-center sm:opacity-0 sm:group-hover/gh:opacity-100 sm:focus-within:opacity-100">
        {onAdd ? <IconButton label={`New row in ${group.label}`} icon={<Plus />} size="sm" variant="ghost" onClick={onAdd} /> : null}
        {onHide ? (
          <Button size="sm" variant="ghost" onClick={onHide}>
            Hide
          </Button>
        ) : null}
      </span>
    </div>
  );
}

/** Hidden groups, listed at the end with a way to show each again. */
export function HiddenGroups({ groups, prop, props, onShow }: { groups: DocsGroupInfo[]; prop: DocsProperty | undefined; props: ViewProps; onShow?: ((key: string) => void) | undefined }) {
  const [open, setOpen] = useState(false);
  if (!groups.length) return null;
  return (
    <div className="flex flex-col gap-1">
      <button type="button" aria-expanded={open} onClick={() => setOpen((o) => !o)} className="flex min-h-control w-fit items-center gap-2 rounded-sm px-2 type-label text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
        {open ? <ChevronDown aria-hidden className="size-4" /> : <ChevronRight aria-hidden className="size-4" />}
        {groups.length === 1 ? "1 hidden group" : `${groups.length} hidden groups`}
      </button>
      {open ? (
        <ul className="flex flex-col gap-0.5 pl-6">
          {groups.map((g) => (
            <li key={g.key} className="flex min-h-control items-center gap-2">
              <GroupLabel group={g} prop={prop} refs={props.refs} />
              <span className="type-caption tabular-nums text-fg-muted">{g.count}</span>
              {onShow ? (
                <Button size="sm" variant="ghost" onClick={() => onShow(g.key)}>
                  Show
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** Collapsed and hidden group keys, saved in the view when the viewer can (else kept for this visit). */
export function useGroupState(props: ViewProps, which: "group" | "sub_group" = "group") {
  const cfg = props.config[which];
  const [local, setLocal] = useState<{ collapsed: string[]; hidden: string[] } | null>(null);
  const collapsed = local?.collapsed ?? cfg?.collapsed ?? [];
  const hiddenKeys = local?.hidden ?? cfg?.hidden ?? [];
  const save = (patch: { collapsed?: string[]; hidden?: string[] }) => {
    const next = { collapsed: patch.collapsed ?? collapsed, hidden: patch.hidden ?? hiddenKeys };
    if (props.configEditable) {
      setLocal(null);
      props.onConfigChange({ ...props.config, [which]: { ...(cfg ?? {}), collapsed: next.collapsed, hidden: next.hidden } });
    } else setLocal(next);
  };
  return {
    isCollapsed: (key: string) => collapsed.includes(key),
    toggle: (key: string) => save({ collapsed: collapsed.includes(key) ? collapsed.filter((k) => k !== key) : [...collapsed, key] }),
    isHidden: (g: DocsGroupInfo) => hiddenKeys.includes(g.key),
    hide: (key: string) => save({ hidden: [...hiddenKeys.filter((k) => k !== key), key] }),
    show: (key: string) => save({ hidden: hiddenKeys.filter((k) => k !== key) }),
  };
}
