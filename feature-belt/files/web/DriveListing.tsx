"use client";

import {
  errorMessage,
  filesCanEdit,
  filesCanManage,
  filesTypePath,
  topic,
  useDebouncedValue,
  useFilesBatch,
  useFilesDrive,
  useFilesSearch,
  useTopic,
  type FilesItem,
  type FilesSort,
  type PagedQueryResult,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, Input, Menu, MenuItem, MenuSeparator, SegmentedControl, SkeletonGroup, SkeletonRow, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowDownUp, Check, Download, FileUp, FolderInput, FolderPlus, FolderUp, LayoutGrid, List, Plus, RotateCcw, Search, Star, Trash, Upload, Users, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { useFilesActions } from "./actions";
import { filesTypeIcon } from "./icons";
import { ItemList } from "./ItemList";
import { dragHasFiles, filesFromDrop, filesFromInput, useFilesUploads } from "./uploads";

const VIEW_KEY = "scaffoldapp.files.view";

function useViewMode(): ["list" | "grid", (v: "list" | "grid") => void] {
  const [view, setView] = useState<"list" | "grid">("list");
  useEffect(() => {
    try {
      const v = window.localStorage.getItem(VIEW_KEY);
      if (v === "grid" || v === "list") setView(v);
    } catch {
      /* storage unavailable: keep the default */
    }
  }, []);
  return [
    view,
    (v) => {
      setView(v);
      try {
        window.localStorage.setItem(VIEW_KEY, v);
      } catch {
        /* storage unavailable */
      }
    },
  ];
}

/** Sort from the URL (?sort=updated&dir=desc), folders always first. */
export function useFilesSortParam(): { by: FilesSort; desc: boolean; set: (by: FilesSort, desc: boolean) => void } {
  const [by, setBy] = useUrlState<FilesSort>("sort", "name");
  const [dir, setDir] = useUrlState<"asc" | "desc">("dir", "asc");
  return {
    by,
    desc: dir === "desc",
    set: (b, d) => {
      setBy(b);
      setDir(d ? "desc" : "asc");
    },
  };
}

export interface DriveListingProps {
  orgId: string;
  title: ReactNode;
  /** Breadcrumbs or the section switcher above the title. */
  above?: ReactNode;
  query: PagedQueryResult<FilesItem>;
  /** Where uploads and new folders go; null: you can't add here. undefined parentId: My drive. */
  target: { parentId: string | null; canAdd: boolean } | null;
  /** Shared folders section: "New" makes an org-owned shared folder. */
  sharedRoot?: boolean;
  sortable?: boolean;
  trash?: boolean;
  empty: { title: string; icon: ReactNode; action?: ReactNode };
  /** Realtime topic to follow (a folder, or the org for shared folders). */
  topicId?: string | null;
  headerActions?: ReactNode;
}

/** A section or folder: header with New, search, sort and view; the list; selection actions; drop to upload. */
export function DriveListing({ orgId, title, above, query, target, sharedRoot, sortable, trash, empty, topicId, headerActions }: DriveListingProps) {
  const router = useRouter();
  const a = useFilesActions();
  const uploads = useFilesUploads();
  const drive = useFilesDrive(orgId);
  const batch = useFilesBatch();
  const sort = useFilesSortParam();
  const [view, setView] = useViewMode();
  const [q, setQ] = useUrlState("q", "");
  const [draft, setDraft] = useState(q);
  const debounced = useDebouncedValue(draft, 250);
  const search = useFilesSearch(orgId, debounced);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dropping, setDropping] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  useTopic(topicId ? topic("files", topicId) : null);

  useEffect(() => {
    if (debounced !== q) setQ(debounced || null);
  }, [debounced, q, setQ]);
  useEffect(() => {
    if (drive.data) uploads.setMaxBytes(drive.data.max_upload_bytes);
  }, [drive.data, uploads]);
  useEffect(() => {
    folderInput.current?.setAttribute("webkitdirectory", "");
  }, []);

  const searching = debounced.trim().length > 0;
  const list = searching ? null : query;
  const items = useMemo(() => (searching ? (search.data?.items.map((h) => h.item) ?? []) : (query.data?.items ?? [])), [searching, search.data, query.data]);
  const snippets = useMemo(() => Object.fromEntries((search.data?.items ?? []).filter((h) => h.snippet).map((h) => [h.item.id, h.snippet])), [search.data]);
  const chosen = items.filter((i) => selected.has(i.id));
  // Drop selections that left the list (moved, trashed, another page).
  useEffect(() => {
    setSelected((s) => {
      const ids = new Set(items.map((i) => i.id));
      const next = new Set([...s].filter((id) => ids.has(id)));
      return next.size === s.size ? s : next;
    });
  }, [items]);

  // Screen shortcuts: / searches, Delete trashes the selection, F2 renames.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.closest("input, textarea, [contenteditable=true], [role=dialog], [role=menu]") || e.metaKey || e.ctrlKey || e.altKey)) return;
      if (e.key === "/") {
        e.preventDefault();
        searchInput.current?.focus();
      } else if ((e.key === "Delete" || e.key === "Backspace") && chosen.length > 0 && !trash) {
        const editable = chosen.filter(filesCanEdit);
        if (editable.length) {
          e.preventDefault();
          a.trash(editable);
        }
      } else if (e.key === "F2" && chosen.length === 1 && chosen[0] && filesCanEdit(chosen[0])) {
        e.preventDefault();
        a.rename(chosen[0]);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [chosen, trash, a]);

  const upload = (picked: ReturnType<typeof filesFromInput>) => {
    if (!target?.canAdd || picked.length === 0) return;
    uploads.add(orgId, target.parentId, picked);
  };

  const moveInto = (ids: string[], folder: FilesItem) => {
    batch.mutate(
      { action: "move", ids, parentId: folder.id },
      {
        onSuccess: ({ results }) => {
          const failed = results.filter((r) => !r.ok);
          if (failed.length === 0) toast.success(ids.length === 1 ? `Moved to “${folder.name}”` : `${ids.length} items moved to “${folder.name}”`);
          else toast.error(failed[0]?.error?.message ?? "Couldn't move that.");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't move that.")),
      },
    );
  };

  const types = drive.data?.types ?? [];
  const newMenu =
    target?.canAdd || sharedRoot ? (
      <Menu
        align="end"
        trigger={
          <Button size="auto" leftIcon={<Plus />}>
            New
          </Button>
        }
      >
        {sharedRoot ? (
          <MenuItem icon={<Users />} onSelect={() => a.newFolder(null, true)} disabled={!drive.data?.can_create_shared}>
            Shared folder
          </MenuItem>
        ) : (
          <>
            <MenuItem icon={<FolderPlus />} onSelect={() => a.newFolder(target?.parentId ?? null)}>
              Folder
            </MenuItem>
            <MenuSeparator />
            <MenuItem icon={<FileUp />} onSelect={() => fileInput.current?.click()}>
              Upload files
            </MenuItem>
            <MenuItem icon={<FolderUp />} onSelect={() => folderInput.current?.click()}>
              Upload a folder
            </MenuItem>
            {types.some((t) => t.create_path) ? <MenuSeparator /> : null}
            {types
              .filter((t) => t.create_path)
              .map((t) => {
                const Icon = filesTypeIcon(t.icon);
                return (
                  <MenuItem key={t.type} icon={<Icon />} onSelect={() => router.push(filesTypePath(t.create_path, target?.parentId))}>
                    {t.title}
                  </MenuItem>
                );
              })}
          </>
        )}
      </Menu>
    ) : null;

  const pending = searching ? search.isPending : query.isPending;
  const error = searching ? search.error : query.error;

  return (
    <div
      className="relative flex flex-col gap-4"
      onDragOver={(e) => {
        if (!dragHasFiles(e) || !target?.canAdd) return;
        e.preventDefault();
        setDropping(true);
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDropping(false);
      }}
      onDrop={(e) => {
        if (!dragHasFiles(e) || !target?.canAdd) return;
        e.preventDefault();
        setDropping(false);
        void filesFromDrop(e.dataTransfer).then(upload);
      }}
    >
      <input ref={fileInput} type="file" multiple hidden onChange={(e) => (upload(filesFromInput(e.target.files)), (e.target.value = ""))} />
      <input ref={folderInput} type="file" multiple hidden onChange={(e) => (upload(filesFromInput(e.target.files)), (e.target.value = ""))} />

      <header className="flex flex-col gap-2">
        {above}
        <div className="flex min-h-control-lg items-center gap-3">
          <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">{title}</h1>
          {headerActions}
          {newMenu}
        </div>
      </header>

      {chosen.length > 0 ? (
        <SelectionBar items={chosen} trash={!!trash} onClear={() => setSelected(new Set())} />
      ) : (
        <div className="flex min-h-control-lg items-center gap-2">
          {trash ? null : (
          <Input
            ref={searchInput}
            aria-label="Search files"
            placeholder="Search files"
            leadingIcon={<Search aria-hidden />}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setDraft("");
            }}
            trailing={draft ? <IconButton label="Clear search" icon={<X />} variant="ghost" size="sm" onClick={() => setDraft("")} /> : undefined}
            wrapperClassName="min-w-0 flex-1 sm:max-w-sm"
          />
          )}
          <div className="ml-auto flex items-center gap-2">
            {sortable && !searching ? <SortMenu sort={sort} /> : null}
            <SegmentedControl
              aria-label="View"
              fullWidth={false}
              value={view}
              onValueChange={setView}
              options={[
                { value: "list", label: <List className="size-4" aria-label="List" /> },
                { value: "grid", label: <LayoutGrid className="size-4" aria-label="Grid" /> },
              ]}
            />
          </div>
        </div>
      )}

      {pending ? (
        <SkeletonGroup label="Loading files" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : error && items.length === 0 ? (
        <ErrorState title="Couldn't load these files" info={errorMessage(error, "")} onRetry={() => void (searching ? search.refetch() : query.refetch())} />
      ) : items.length === 0 ? (
        searching ? (
          <EmptyState icon={<Search />} title={`Nothing matches “${debounced.trim()}”`} action={<Button variant="secondary" onClick={() => setDraft("")}>Clear search</Button>} />
        ) : (
          <EmptyState
            icon={empty.icon}
            title={empty.title}
            action={
              empty.action ??
              (target?.canAdd ? (
                <Button variant="secondary" leftIcon={<Upload />} onClick={() => fileInput.current?.click()}>
                  Upload files
                </Button>
              ) : undefined)
            }
          />
        )
      ) : (
        <>
          {error ? (
            <p className="type-caption text-danger-strong" role="status">
              Couldn&apos;t refresh. Showing what was loaded.
            </p>
          ) : null}
          <ItemList
            items={items}
            view={view}
            selected={selected}
            onSelect={setSelected}
            onOpen={a.open}
            {...(!searching && !trash ? { onDropOnFolder: moveInto } : {})}
            {...(sortable && !searching ? { sort: { by: sort.by, desc: sort.desc, onChange: sort.set } } : {})}
            dateColumn={trash ? "trashed" : "updated"}
            snippets={snippets}
            label={typeof title === "string" ? title : "Files"}
          />
          {searching ? (
            <LoadMore hasNextPage={!!search.hasNextPage} isFetchingNextPage={search.isFetchingNextPage} fetchNextPage={search.fetchNextPage} />
          ) : list ? (
            <LoadMore hasNextPage={!!list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
          ) : null}
        </>
      )}

      {dropping ? (
        <div className="pointer-events-none absolute inset-0 z-(--app-z-raised) flex items-center justify-center rounded-lg border-2 border-dashed border-primary bg-primary-soft/80">
          <p className="flex items-center gap-2 type-title-sm text-primary-strong">
            <Upload className="size-5" aria-hidden />
            Drop to upload
          </p>
        </div>
      ) : null}
    </div>
  );
}

function SortMenu({ sort }: { sort: ReturnType<typeof useFilesSortParam> }) {
  const opts: { by: FilesSort; label: string }[] = [
    { by: "name", label: "Name" },
    { by: "updated", label: "Last modified" },
    { by: "size", label: "Size" },
  ];
  return (
    <Menu align="end" trigger={<IconButton label="Sort" tooltip icon={<ArrowDownUp />} variant="ghost" />}>
      {opts.map((o) => (
        <MenuItem key={o.by} icon={sort.by === o.by ? <Check /> : <span className="size-4" />} onSelect={() => sort.set(o.by, o.by === sort.by ? sort.desc : o.by !== "name")}>
          {o.label}
        </MenuItem>
      ))}
      <MenuSeparator />
      <MenuItem icon={<ArrowDownUp />} onSelect={() => sort.set(sort.by, !sort.desc)}>
        {sort.desc ? "Ascending" : "Descending"}
      </MenuItem>
    </Menu>
  );
}

/** Actions for the selected items. */
function SelectionBar({ items, trash, onClear }: { items: FilesItem[]; trash: boolean; onClear: () => void }) {
  const a = useFilesActions();
  const editable = items.filter(filesCanEdit);
  const manageable = items.filter(filesCanManage);
  const downloadable = items.filter((i) => i.type === "file" || i.type === "folder");
  const allStarred = items.every((i) => i.starred);
  return (
    <div className="flex min-h-control-lg flex-wrap items-center gap-2 rounded-lg bg-primary-soft px-2" role="toolbar" aria-label="Selected items">
      <IconButton label="Clear selection" icon={<X />} variant="ghost" size="sm" onClick={onClear} />
      <span className="mr-auto type-label font-semibold text-primary-strong" aria-live="polite">
        {items.length} selected
      </span>
      {trash ? (
        <>
          {editable.length ? (
            <Button variant="ghost" size="sm" leftIcon={<RotateCcw />} onClick={() => a.restore(editable)}>
              Restore
            </Button>
          ) : null}
          {manageable.length ? (
            <Button variant="danger-ghost" size="sm" leftIcon={<X />} onClick={() => a.remove(manageable)}>
              Delete forever
            </Button>
          ) : null}
        </>
      ) : (
        <>
          {downloadable.length ? (
            <Button variant="ghost" size="sm" leftIcon={<Download />} onClick={() => a.download(downloadable)}>
              <span className="hidden sm:inline">Download</span>
              <span className="sr-only sm:hidden">Download</span>
            </Button>
          ) : null}
          {editable.length === items.length ? (
            <Button variant="ghost" size="sm" leftIcon={<FolderInput />} onClick={() => a.move(items)}>
              <span className="hidden sm:inline">Move</span>
              <span className="sr-only sm:hidden">Move</span>
            </Button>
          ) : null}
          <IconButton
            label={allStarred ? "Remove stars" : "Star"}
            tooltip
            icon={<Star className={cn(allStarred && "fill-current")} />}
            variant="ghost"
            size="sm"
            onClick={() => items.forEach((i) => a.star(i, !allStarred))}
          />
          {editable.length ? <IconButton label="Move to trash" tooltip icon={<Trash />} variant="danger" size="sm" onClick={() => a.trash(editable)} /> : null}
        </>
      )}
    </div>
  );
}
