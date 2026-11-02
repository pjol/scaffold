"use client";

import { filesFormatSize, filesIsConnected, filesKindLabel, sourceProductLabel, type FilesItem, type FilesSort } from "@scaffoldapp/shared";
import { Checkbox, ProviderIcon, SourceBadge, Tooltip, cn } from "@scaffoldapp/ui-web";
import { ArrowDown, ArrowUp, Globe, Star, Users } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type DragEvent, type KeyboardEvent, type MouseEvent } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { formatDate, formatRelative } from "@/lib/format";
import { useFilesItemMenu } from "./actions";
import { useFilesConnectedItemMenu } from "./connected";
import { filesIconTone, filesItemIcon } from "./icons";

/** The drag payload for items moved inside the drive. */
export const FILES_DRAG_TYPE = "application/x-files-items";

export interface ItemListProps {
  items: FilesItem[];
  view: "list" | "grid";
  selected: ReadonlySet<string>;
  onSelect: (next: Set<string>) => void;
  onOpen: (item: FilesItem) => void;
  /** Dropping items on a folder row moves them there. */
  onDropOnFolder?: (ids: string[], folder: FilesItem) => void;
  /** Column sort (list view); omit for lists with a fixed order (recent, trash). */
  sort?: { by: FilesSort; desc: boolean; onChange: (by: FilesSort, desc: boolean) => void };
  /** Which date column to show. */
  dateColumn?: "updated" | "trashed";
  /** Excerpts for search hits by id. */
  snippets?: Record<string, string>;
  label: string;
}

/**
 * Items as a list or a grid. Click a name to open it; click elsewhere on a row (or its
 * checkbox) to select, with Shift for a range and ⌘/Ctrl to add. Keyboard: arrows move,
 * Space selects, Enter opens, ⌘/Ctrl+A selects all, Esc clears. Rows drag onto folders.
 */
export function ItemList({ items, view, selected, onSelect, onOpen, onDropOnFolder, sort, dateColumn = "updated", snippets, label }: ItemListProps) {
  const [focus, setFocus] = useState(0);
  const [anchor, setAnchor] = useState<number | null>(null);
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  const focusIndex = Math.min(focus, Math.max(items.length - 1, 0));

  const focusRow = useCallback((i: number) => {
    setFocus(i);
    root.current?.querySelector<HTMLElement>(`[data-row="${i}"] [data-primary]`)?.focus();
  }, []);

  useEffect(() => {
    if (focus >= items.length && items.length > 0) setFocus(items.length - 1);
  }, [items.length, focus]);

  const select = (i: number, e: { shiftKey: boolean; metaKey: boolean; ctrlKey: boolean }, toggle = false) => {
    const it = items[i];
    if (!it) return;
    const next = new Set(toggle || e.metaKey || e.ctrlKey ? selected : []);
    if (e.shiftKey && anchor !== null) {
      const [a, b] = anchor < i ? [anchor, i] : [i, anchor];
      for (let k = a; k <= b; k++) {
        const id = items[k]?.id;
        if (id) next.add(id);
      }
    } else if ((toggle || e.metaKey || e.ctrlKey) && selected.has(it.id)) next.delete(it.id);
    else next.add(it.id);
    if (!e.shiftKey) setAnchor(i);
    setFocus(i);
    onSelect(next);
  };

  const columns = () => {
    if (view !== "grid" || !root.current) return 1;
    const tpl = getComputedStyle(root.current).gridTemplateColumns;
    return Math.max(1, tpl.split(" ").filter(Boolean).length);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (items.length === 0) return;
    const step = { ArrowDown: columns(), ArrowUp: -columns(), ArrowRight: view === "grid" ? 1 : 0, ArrowLeft: view === "grid" ? -1 : 0 }[e.key];
    if (step) {
      e.preventDefault();
      const next = Math.max(0, Math.min(items.length - 1, focusIndex + step));
      if (e.shiftKey) select(next, { shiftKey: true, metaKey: false, ctrlKey: false });
      focusRow(next);
      return;
    }
    if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      focusRow(e.key === "Home" ? 0 : items.length - 1);
      return;
    }
    if (e.key === " " && !(e.target instanceof HTMLInputElement)) {
      e.preventDefault();
      select(focusIndex, { shiftKey: e.shiftKey, metaKey: true, ctrlKey: false }, true);
      return;
    }
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "a") {
      e.preventDefault();
      onSelect(new Set(items.map((i) => i.id)));
      return;
    }
    if (e.key === "Escape" && selected.size > 0) {
      e.preventDefault();
      onSelect(new Set());
    }
  };

  const dragStart = (e: DragEvent, it: FilesItem) => {
    const ids = selected.has(it.id) ? [...selected] : [it.id];
    e.dataTransfer.setData(FILES_DRAG_TYPE, JSON.stringify(ids));
    e.dataTransfer.setData("text/plain", ids.length === 1 ? it.name : `${ids.length} items`);
    e.dataTransfer.effectAllowed = "move";
  };
  const dropProps = (it: FilesItem) =>
    it.type === "folder" && onDropOnFolder
      ? {
          onDragOver: (e: DragEvent) => {
            if (!Array.from(e.dataTransfer.types).includes(FILES_DRAG_TYPE)) return;
            e.preventDefault();
            e.dataTransfer.dropEffect = "move";
            setDropTarget(it.id);
          },
          onDragLeave: () => setDropTarget((t) => (t === it.id ? null : t)),
          onDrop: (e: DragEvent) => {
            const raw = e.dataTransfer.getData(FILES_DRAG_TYPE);
            setDropTarget(null);
            if (!raw) return;
            e.preventDefault();
            e.stopPropagation();
            const ids = (JSON.parse(raw) as string[]).filter((id) => id !== it.id);
            if (ids.length) onDropOnFolder(ids, it);
          },
        }
      : {};

  const rowClick = (e: MouseEvent, i: number) => {
    if (!isRowBackground(e)) return;
    select(i, e);
  };

  if (view === "grid") {
    return (
      <div ref={root} role="grid" aria-label={label} aria-multiselectable onKeyDown={onKeyDown} className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
        {items.map((it, i) => (
          <GridCard
            key={it.id}
            item={it}
            index={i}
            focused={i === focusIndex}
            selected={selected.has(it.id)}
            dropping={dropTarget === it.id}
            onOpen={() => onOpen(it)}
            onToggle={(e) => select(i, e, true)}
            onClick={(e) => rowClick(e, i)}
            onFocus={() => setFocus(i)}
            onDragStart={(e) => dragStart(e, it)}
            {...dropProps(it)}
          />
        ))}
      </div>
    );
  }

  const header = (by: FilesSort, text: string, className?: string) => {
    if (!sort) return <span className={cn("type-caption font-semibold text-fg-muted", className)}>{text}</span>;
    const active = sort.by === by;
    const Icon = sort.desc ? ArrowDown : ArrowUp;
    return (
      <button
        type="button"
        className={cn("flex min-h-control items-center gap-1 type-caption font-semibold", active ? "text-fg" : "text-fg-muted hover:text-fg", className)}
        aria-sort={active ? (sort.desc ? "descending" : "ascending") : "none"}
        onClick={() => sort.onChange(by, active ? !sort.desc : by !== "name")}
      >
        {text}
        {active ? <Icon className="size-3.5" aria-hidden /> : null}
      </button>
    );
  };

  return (
    <div className="flex flex-col">
      <div className="hidden min-h-control items-center gap-3 border-b border-border px-2 sm:flex" aria-hidden={!sort}>
        <span className="w-6 shrink-0" />
        <span className="flex min-w-0 flex-1 items-center">{header("name", "Name")}</span>
        <span className="hidden w-36 shrink-0 items-center md:flex">
          <span className="type-caption font-semibold text-fg-muted">{dateColumn === "trashed" ? "Trashed by" : "Modified by"}</span>
        </span>
        <span className="flex w-28 shrink-0 items-center">{dateColumn === "trashed" ? <span className="type-caption font-semibold text-fg-muted">Trashed</span> : header("updated", "Modified")}</span>
        <span className="hidden w-20 shrink-0 items-center justify-end lg:flex">{header("size", "Size")}</span>
        <span className="w-8 shrink-0" />
      </div>
      <div ref={root} role="grid" aria-label={label} aria-multiselectable onKeyDown={onKeyDown} className="flex flex-col py-1">
        {items.map((it, i) => (
          <ListRowItem
            key={it.id}
            item={it}
            index={i}
            focused={i === focusIndex}
            selected={selected.has(it.id)}
            dropping={dropTarget === it.id}
            dateColumn={dateColumn}
            snippet={snippets?.[it.id]}
            onOpen={() => onOpen(it)}
            onToggle={(e) => select(i, e, true)}
            onClick={(e) => rowClick(e, i)}
            onFocus={() => setFocus(i)}
            onDragStart={(e) => dragStart(e, it)}
            {...dropProps(it)}
          />
        ))}
      </div>
    </div>
  );
}

/** A click on the row itself, not on its controls or on a menu or dialog portalled out of it. */
function isRowBackground(e: MouseEvent): boolean {
  const target = e.target as HTMLElement;
  return (e.currentTarget as HTMLElement).contains(target) && !target.closest("button, a, input, label, [role=menuitem]");
}

interface RowProps {
  item: FilesItem;
  index: number;
  focused: boolean;
  selected: boolean;
  dropping: boolean;
  onOpen: () => void;
  onToggle: (e: { shiftKey: boolean; metaKey: boolean; ctrlKey: boolean }) => void;
  onClick: (e: MouseEvent) => void;
  onFocus: () => void;
  onDragStart: (e: DragEvent) => void;
  onDragOver?: (e: DragEvent) => void;
  onDragLeave?: () => void;
  onDrop?: (e: DragEvent) => void;
}

function Markers({ item }: { item: FilesItem }) {
  return (
    <>
      <SourceBadge source={item.source} iconOnly className="shrink-0" />
      {item.shortcut ? (
        <Tooltip content={`Shortcut to ${sourceProductLabel(item.shortcut.provider, item.shortcut.product)}`}>
          <span className="inline-flex shrink-0 text-fg-muted" aria-label={`Shortcut to ${sourceProductLabel(item.shortcut.provider, item.shortcut.product)}`}>
            <ProviderIcon provider={item.shortcut.provider} product={item.shortcut.product} size={14} />
          </span>
        </Tooltip>
      ) : null}
      {item.starred ? <Star className="size-3.5 shrink-0 fill-current text-warning-strong" aria-label="Starred" /> : null}
      {item.public_link ? (
        <Tooltip content={item.public_disabled ? "Public link turned off by staff" : "Anyone with the link can view"}>
          <Globe className={cn("size-3.5 shrink-0", item.public_disabled ? "text-danger-strong" : "text-fg-muted")} aria-label="Public link" />
        </Tooltip>
      ) : item.shared ? (
        <Tooltip content="Shared">
          <Users className="size-3.5 shrink-0 text-fg-muted" aria-label="Shared" />
        </Tooltip>
      ) : null}
    </>
  );
}

function ItemMenu({ item }: { item: FilesItem }) {
  return filesIsConnected(item) ? <ConnectedRowMenu item={item} /> : <AppRowMenu item={item} />;
}

function AppRowMenu({ item }: { item: FilesItem }) {
  const actions = useFilesItemMenu(item, { showOpen: true });
  return <ResourceMenu feature="files" kind="files.item" id={item.id} title={item.name} orgId={item.org_id} actions={actions} label={`More actions for ${item.name}`} />;
}

/** Connected rows get the platform's actions (Open in…, Manage sharing in…) and no app Share…. */
function ConnectedRowMenu({ item }: { item: FilesItem }) {
  const actions = useFilesConnectedItemMenu(filesIsConnected(item) ? item : null, { showOpen: true });
  // kind without an id: no other feature adds rows (no app Share…; agent access lives on the account's own menus).
  return <ResourceMenu feature="files" kind="files.external" actions={actions} label={`More actions for ${item.name}`} />;
}

/** A thumbnail that falls back to the kind's icon when the link fails or expires. */
function Thumb({ item, className, iconClass }: { item: FilesItem; className: string; iconClass: string }) {
  const [failed, setFailed] = useState(false);
  const Icon = filesItemIcon(item);
  if (!item.thumb_url || failed) return <Icon className={cn(iconClass, filesIconTone(item))} aria-hidden />;
  // eslint-disable-next-line @next/next/no-img-element -- short-lived provider or presigned URLs
  return <img src={item.thumb_url} alt="" className={className} loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)} />;
}

function personName(p: FilesItem["updated_by"]): string {
  if (!p) return "—";
  return p.name || "Deleted user";
}

function ListRowItem({ item, index, focused, selected, dropping, dateColumn, snippet, onOpen, onToggle, onClick, onFocus, onDragStart, onDragOver, onDragLeave, onDrop }: RowProps & { dateColumn: "updated" | "trashed"; snippet?: string | undefined }) {
  const date = dateColumn === "trashed" ? (item.trashed_at ?? item.updated_at) : item.updated_at;
  return (
    <div
      role="row"
      aria-selected={selected}
      data-row={index}
      draggable={!item.trashed_at}
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
      onClick={onClick}
      onDoubleClick={(e) => {
        if (isRowBackground(e)) onOpen();
      }}
      className={cn(
        "group flex min-h-tap-lg items-center gap-3 rounded-md px-2 transition-colors",
        selected ? "bg-primary-soft" : "hover:bg-sunken",
        dropping && "ring-2 ring-ring",
      )}
    >
      <span role="gridcell" className="flex w-6 shrink-0 items-center justify-center">
        <Checkbox
          checked={selected}
          onCheckedChange={() => onToggle({ shiftKey: false, metaKey: true, ctrlKey: false })}
          aria-label={`Select ${item.name}`}
          className={cn(!selected && "sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100")}
        />
      </span>
      <span role="gridcell" className="flex min-w-0 flex-1 items-center gap-3">
        <span className="flex size-8 shrink-0 items-center justify-center">
          <Thumb item={item} className="size-8 rounded-sm object-cover" iconClass="size-6" />
        </span>
        <span className="flex min-w-0 flex-col">
          <span className="flex min-w-0 items-center gap-2">
            <button
              type="button"
              data-primary
              tabIndex={focused ? 0 : -1}
              onFocus={onFocus}
              onClick={onOpen}
              className="min-w-0 truncate rounded-xs text-left type-body font-medium text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring"
              title={item.name}
            >
              {item.name}
            </button>
            <Markers item={item} />
          </span>
          {snippet ? <span className="truncate type-caption text-fg-muted">{snippet}</span> : null}
        </span>
      </span>
      <span role="gridcell" className="hidden w-36 shrink-0 truncate type-label text-fg-muted md:block">
        {dateColumn === "trashed" ? (item.trashed_by ? personName(item.trashed_by) : "Deleted account") : personName(item.updated_by)}
      </span>
      <span role="gridcell" className="hidden w-28 shrink-0 truncate type-label text-fg-muted sm:block" title={formatDate(date)}>
        {formatRelative(date)}
      </span>
      <span role="gridcell" className="hidden w-20 shrink-0 text-right type-label tabular-nums text-fg-muted lg:block">
        {item.type === "file" && (item.size > 0 || !filesIsConnected(item)) ? filesFormatSize(item.size) : "—"}
      </span>
      <span role="gridcell" className="flex w-8 shrink-0 justify-end">
        <ItemMenu item={item} />
      </span>
    </div>
  );
}

function GridCard({ item, index, focused, selected, dropping, onOpen, onToggle, onClick, onFocus, onDragStart, onDragOver, onDragLeave, onDrop }: RowProps) {
  return (
    <div
      role="row"
      aria-selected={selected}
      data-row={index}
      draggable={!item.trashed_at}
      onDragStart={onDragStart}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
      onClick={onClick}
      onDoubleClick={(e) => {
        if (isRowBackground(e)) onOpen();
      }}
      className={cn(
        "group relative flex flex-col overflow-hidden rounded-lg border transition-colors",
        selected ? "border-primary bg-primary-soft" : "border-border bg-surface hover:bg-sunken",
        dropping && "ring-2 ring-ring",
      )}
    >
      <span role="gridcell" className="flex aspect-4/3 items-center justify-center bg-sunken">
        <Thumb item={item} className="size-full object-cover" iconClass="size-10" />
      </span>
      <span className="absolute top-2 left-2">
        <Checkbox
          checked={selected}
          onCheckedChange={() => onToggle({ shiftKey: false, metaKey: true, ctrlKey: false })}
          aria-label={`Select ${item.name}`}
          className={cn("rounded-sm bg-surface", !selected && "sm:opacity-0 sm:group-hover:opacity-100 sm:group-focus-within:opacity-100")}
        />
      </span>
      <span role="gridcell" className="flex min-h-row items-center gap-2 py-1 pr-1 pl-3">
        <button
          type="button"
          data-primary
          tabIndex={focused ? 0 : -1}
          onFocus={onFocus}
          onClick={onOpen}
          title={`${item.name} · ${filesKindLabel(item)}`}
          className="min-w-0 flex-1 truncate rounded-xs text-left type-label font-medium text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring"
        >
          {item.name}
        </button>
        <Markers item={item} />
        <ItemMenu item={item} />
      </span>
    </div>
  );
}
