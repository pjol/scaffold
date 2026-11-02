"use client";

import {
  errorMessage,
  filesCanEdit,
  filesConnectedPath,
  filesKindLabel,
  hasErrorCode,
  isConnectionErrorCode,
  useActiveOrg,
  useDebouncedValue,
  useFilesConnectedChildren,
  useFilesConnectedDrives,
  useFilesConnectedItem,
  useFilesConnectedSearch,
  useFilesSources,
  type FilesConnectedDetail,
  type FilesConnectedDrive,
  type FilesConnectedItem,
  type FilesConnectedSource,
  type FilesItem,
} from "@scaffoldapp/shared";
import {
  Banner,
  Button,
  EmptyState,
  ErrorState,
  IconButton,
  Input,
  ListRow,
  Menu,
  MenuItem,
  MenuSeparator,
  ProviderIcon,
  SegmentedControl,
  Skeleton,
  SkeletonGroup,
  SkeletonRow,
  SourceBadge,
  toast,
} from "@scaffoldapp/ui-web";
import { ChevronRight, Copy, Download, ExternalLink, FileUp, FolderInput, FolderOpen, FolderPlus, HardDrive, LayoutGrid, List, Plus, Search, Trash, Unplug, Upload, Users, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { connectedProductName, openExternal, openOnPlatformLabel, useFilesConnectedActions, useFilesConnectedItemMenu } from "./connected";
import { FilesConnectedDetails } from "./DetailsPanel";
import { SortMenu, useFilesSortParam, useViewMode } from "./DriveListing";
import { ItemList } from "./ItemList";
import { FilesPreview } from "./Preview";
import { dragHasFiles, filesFromDrop, filesFromInput, useFilesUploads } from "./uploads";

const DRIVE_ICON: Record<FilesConnectedDrive["kind"], typeof HardDrive> = { my_drive: HardDrive, shared_with_me: Users, shared_drive: Users, onedrive: HardDrive, library: Users };

/** A connection error with its fix (reconnect, allow, blocked), or a plain error state. */
function ConnectedError({ error, title, onRetry }: { error: unknown; title: string; onRetry: () => void }) {
  const code = (error as { code?: string } | null)?.code ?? "";
  const fix = ((error as { details?: Record<string, unknown> } | null)?.details?.["fix_url"] as string | undefined) ?? undefined;
  if (isConnectionErrorCode(code) && fix) {
    return (
      <EmptyState
        icon={<Unplug />}
        title={errorMessage(error, title)}
        action={
          <Button asChild variant="secondary">
            <Link href={fix}>{code === "connection_scope_missing" ? "Allow it" : "Reconnect"}</Link>
          </Button>
        }
      />
    );
  }
  if (hasErrorCode(error, "not_found") || hasErrorCode(error, "connection_not_found")) {
    return (
      <EmptyState
        icon={<FolderOpen />}
        title={errorMessage(error, "This isn't available")}
        action={
          <Button asChild variant="secondary">
            <Link href="/files">Go to My drive</Link>
          </Button>
        }
      />
    );
  }
  return <ErrorState title={title} info={errorMessage(error, "")} onRetry={onRetry} />;
}

function ListingSkeleton() {
  return (
    <div className="flex flex-col gap-4">
      <Skeleton className="h-10 w-48" />
      <SkeletonGroup label="Loading files" className="flex flex-col">
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    </div>
  );
}

// ---- one account: its drives -----------------------------------------------------------------

/** /files/c/[connectionID]: the places to browse in one connected account. */
export function FilesConnectedAccountScreen({ connId }: { connId: string }) {
  const { org } = useActiveOrg();
  const sources = useFilesSources(org?.id);
  const drives = useFilesConnectedDrives(connId, org?.id);
  const source = sources.data?.sources.find((s) => s.id === connId);
  const list = drives.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4">
      <header className="flex flex-col gap-2">
        <nav aria-label="Breadcrumb" className="type-label text-fg-muted">
          <Link href="/files" className="rounded-xs px-1 py-1 hover:text-fg hover:underline">
            Files
          </Link>
        </nav>
        <div className="flex min-h-control-lg items-center gap-3">
          <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">
            {source?.provider ? <ProviderIcon provider={source.provider} product={source.product} size={24} /> : null}
            <span className="truncate">{source?.label ?? "Connected account"}</span>
          </h1>
          {source && !source.can_write && source.write_url ? (
            <Button asChild variant="ghost" size="auto">
              <Link href={source.write_url}>Allow changes</Link>
            </Button>
          ) : null}
        </div>
      </header>
      {drives.isPending ? (
        <SkeletonGroup label="Loading drives" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : drives.isError ? (
        <ConnectedError error={drives.error} title="Couldn't load the drives" onRetry={() => void drives.refetch()} />
      ) : list.length === 0 ? (
        <EmptyState icon={<HardDrive />} title="No drives in this account" />
      ) : (
        <ul className="flex flex-col" aria-label="Drives">
          {list.map((d) => {
            const Icon = DRIVE_ICON[d.kind] ?? HardDrive;
            return (
              <li key={d.ref}>
                <ListRow asChild leading={<Icon className="size-6 text-primary-strong" aria-hidden />} title={d.site ? `${d.site} · ${d.name}` : d.name} chevron>
                  <Link href={d.link} />
                </ListRow>
              </li>
            );
          })}
        </ul>
      )}
      <LoadMore hasNextPage={!!drives.hasNextPage} isFetchingNextPage={drives.isFetchingNextPage} fetchNextPage={drives.fetchNextPage} />
    </div>
  );
}

// ---- one item: a folder or a file --------------------------------------------------------------

/** /files/c/[connectionID]/[ref]: a connected folder's listing or a file's preview. */
export function FilesConnectedItemScreen({ connId, itemRef }: { connId: string; itemRef: string }) {
  const { org } = useActiveOrg();
  const detail = useFilesConnectedItem(connId, itemRef, org?.id);
  if (detail.isPending) return <ListingSkeleton />;
  if (detail.isError) return <ConnectedError error={detail.error} title="Couldn't load this" onRetry={() => void detail.refetch()} />;
  return detail.data.item.type === "folder" ? <ConnectedFolderView detail={detail.data} orgId={org?.id ?? null} key={itemRef} /> : <ConnectedFileView detail={detail.data} orgId={org?.id ?? null} />;
}

function ConnectedCrumbs({ detail }: { detail: FilesConnectedDetail }) {
  const src = detail.source;
  const crumbs: { key: string; name: string; href: string; icon?: ReactNode }[] = [
    { key: "account", name: src.label, href: filesConnectedPath(src.id), ...(src.provider ? { icon: <ProviderIcon provider={src.provider} product={src.product} size={14} /> } : {}) },
    ...detail.path.map((c) => ({ key: c.ref, name: c.name, href: c.link })),
  ];
  const shown = crumbs.length > 3 ? [crumbs[0], null, ...crumbs.slice(-2)] : crumbs;
  return (
    <nav aria-label="Breadcrumb">
      <ol className="flex min-w-0 flex-wrap items-center gap-1 type-label text-fg-muted">
        {shown.map((c, i) => (
          <li key={c ? c.key : "gap"} className="flex min-w-0 items-center gap-1">
            {i > 0 ? <ChevronRight className="size-3.5 shrink-0" aria-hidden /> : null}
            {c ? (
              <Link href={c.href} className="flex max-w-56 items-center gap-1.5 truncate rounded-xs px-1 py-1 hover:text-fg hover:underline">
                {c.icon}
                <span className="truncate">{c.name}</span>
              </Link>
            ) : (
              <span aria-label="More folders" className="px-1">
                …
              </span>
            )}
          </li>
        ))}
      </ol>
    </nav>
  );
}

function ConnectedFolderView({ detail, orgId }: { detail: FilesConnectedDetail; orgId: string | null }) {
  const folder = detail.item;
  const src = detail.source;
  const a = useFilesConnectedActions();
  const uploads = useFilesUploads();
  const sort = useFilesSortParam();
  const [view, setView] = useViewMode();
  const [q, setQ] = useUrlState("q", "");
  const [draft, setDraft] = useState(q);
  const debounced = useDebouncedValue(draft, 300);
  const searching = debounced.trim().length > 0;
  const query = useFilesConnectedChildren(folder.connection_id, folder.ref, { sort: sort.by, dir: sort.desc ? "desc" : "asc", thumbs: true }, orgId);
  const search = useFilesConnectedSearch(searching ? folder.connection_id : null, debounced, orgId);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dropping, setDropping] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const searchInput = useRef<HTMLInputElement>(null);
  const writable = src.can_write && folder.ref !== "shared-with-me" && folder.can_edit;
  const items = useMemo<FilesConnectedItem[]>(() => (searching ? (search.data?.items ?? []) : (query.data?.items ?? [])), [searching, search.data, query.data]);
  const chosen = items.filter((i) => selected.has(i.id));
  const product = connectedProductName(folder);
  const menu = useFilesConnectedItemMenu(folder, { showDetails: true });

  useEffect(() => {
    if (debounced !== q) setQ(debounced || null);
  }, [debounced, q, setQ]);
  useEffect(() => {
    setSelected((s) => {
      const ids = new Set(items.map((i) => i.id));
      const next = new Set([...s].filter((id) => ids.has(id)));
      return next.size === s.size ? s : next;
    });
  }, [items]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement | null;
      if (t && (t.closest("input, textarea, [contenteditable=true], [role=dialog], [role=menu]") || e.metaKey || e.ctrlKey || e.altKey)) return;
      if (e.key === "/") {
        e.preventDefault();
        searchInput.current?.focus();
      } else if ((e.key === "Delete" || e.key === "Backspace") && chosen.length > 0) {
        const editable = chosen.filter(filesCanEdit);
        if (editable.length && a) {
          e.preventDefault();
          a.trash(editable);
        }
      } else if (e.key === "F2" && chosen.length === 1 && chosen[0] && filesCanEdit(chosen[0]) && a) {
        e.preventDefault();
        a.rename(chosen[0]);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [chosen, a]);

  const upload = (picked: ReturnType<typeof filesFromInput>) => {
    if (picked.length === 0) return;
    if (!writable) {
      toast.error(src.can_write ? `This ${product} folder doesn't take new files.` : `Allow changes to ${product} first.`);
      return;
    }
    uploads.addConnected({ connectionId: folder.connection_id, parentRef: folder.ref, orgId }, picked);
  };
  const moveInto = (ids: string[], target: FilesItem) => {
    const list = items.filter((i) => ids.includes(i.id) && filesCanEdit(i));
    const dest = items.find((i) => i.id === target.id);
    if (!a || !dest || list.length === 0) return;
    a.move(list);
  };

  const pending = searching ? search.isPending : query.isPending;
  const error = searching ? search.error : query.error;
  const list = searching ? search : query;

  return (
    <div
      className="relative flex flex-col gap-4"
      onDragOver={(e) => {
        if (!dragHasFiles(e) || !writable) return;
        e.preventDefault();
        setDropping(true);
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDropping(false);
      }}
      onDrop={(e) => {
        if (!dragHasFiles(e) || !writable) return;
        e.preventDefault();
        setDropping(false);
        void filesFromDrop(e.dataTransfer).then(upload);
      }}
    >
      <input ref={fileInput} type="file" multiple hidden onChange={(e) => (upload(filesFromInput(e.target.files)), (e.target.value = ""))} />
      <header className="flex flex-col gap-2">
        <ConnectedCrumbs detail={detail} />
        <div className="flex min-h-control-lg items-center gap-3">
          <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">
            <span className="truncate">{folder.name}</span>
            <SourceBadge source={folder.source} iconOnly size="md" className="shrink-0" />
          </h1>
          {folder.web_url ? (
            <span className="hidden sm:flex">
              <Button variant="ghost" size="auto" rightIcon={<ExternalLink />} onClick={() => openExternal(folder.web_url)}>
                Open in {product}
              </Button>
            </span>
          ) : null}
          <ResourceMenu feature={`connectors:${folder.connection_id}`} actions={menu} label={`More actions for ${folder.name}`} />
          {!src.can_write && src.write_url ? (
            <Button asChild variant="secondary" size="auto">
              <Link href={src.write_url}>Allow changes</Link>
            </Button>
          ) : null}
          {writable && a ? (
            <Menu
              align="end"
              trigger={
                <Button size="auto" leftIcon={<Plus />}>
                  New
                </Button>
              }
            >
              <MenuItem icon={<FolderPlus />} onSelect={() => a.newFolder(folder.connection_id, folder.ref)}>
                Folder
              </MenuItem>
              <MenuSeparator />
              <MenuItem icon={<FileUp />} onSelect={() => fileInput.current?.click()}>
                Upload files
              </MenuItem>
            </Menu>
          ) : null}
        </div>
      </header>

      {chosen.length > 0 ? (
        <ConnectedSelectionBar items={chosen} onClear={() => setSelected(new Set())} />
      ) : (
        <div className="flex min-h-control-lg items-center gap-2">
          <Input
            ref={searchInput}
            aria-label={`Search ${src.label}`}
            placeholder={`Search ${product}`}
            leadingIcon={<Search aria-hidden />}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setDraft("");
            }}
            trailing={draft ? <IconButton label="Clear search" icon={<X />} variant="ghost" size="sm" onClick={() => setDraft("")} /> : undefined}
            wrapperClassName="min-w-0 flex-1 sm:max-w-sm"
          />
          <div className="ml-auto flex items-center gap-2">
            {!searching ? <SortMenu sort={sort} /> : null}
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
        <ConnectedError error={error} title="Couldn't load these files" onRetry={() => void list.refetch()} />
      ) : items.length === 0 ? (
        searching ? (
          <EmptyState icon={<Search />} title={`Nothing in ${product} matches “${debounced.trim()}”`} action={<Button variant="secondary" onClick={() => setDraft("")}>Clear search</Button>} />
        ) : (
          <EmptyState
            icon={<FolderOpen />}
            title="This folder is empty"
            action={
              writable ? (
                <Button variant="secondary" leftIcon={<Upload />} onClick={() => fileInput.current?.click()}>
                  Upload files
                </Button>
              ) : undefined
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
            onOpen={(it) => a?.open(it as FilesConnectedItem)}
            {...(!searching && writable ? { onDropOnFolder: moveInto } : {})}
            {...(!searching ? { sort: { by: sort.by, desc: sort.desc, onChange: sort.set } } : {})}
            label={folder.name}
          />
          <LoadMore hasNextPage={!!list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
        </>
      )}

      {dropping ? (
        <div className="pointer-events-none absolute inset-0 z-(--app-z-raised) flex items-center justify-center rounded-lg border-2 border-dashed border-primary bg-primary-soft/80">
          <p className="flex items-center gap-2 type-title-sm text-primary-strong">
            <Upload className="size-5" aria-hidden />
            Drop to upload to {product}
          </p>
        </div>
      ) : null}
    </div>
  );
}

function ConnectedSelectionBar({ items, onClear }: { items: FilesConnectedItem[]; onClear: () => void }) {
  const a = useFilesConnectedActions();
  if (!a) return null;
  const editable = items.filter(filesCanEdit);
  const files = items.filter((i) => i.type === "file" && (!i.native || i.preview === "pdf"));
  const sameDrive = new Set(items.map((i) => i.drive_ref)).size === 1;
  return (
    <div className="flex min-h-control-lg flex-wrap items-center gap-2 rounded-lg bg-primary-soft px-2" role="toolbar" aria-label="Selected items">
      <IconButton label="Clear selection" icon={<X />} variant="ghost" size="sm" onClick={onClear} />
      <span className="mr-auto type-label font-semibold text-primary-strong" aria-live="polite">
        {items.length} selected
      </span>
      {files.length === 1 && items.length === 1 && files[0] ? (
        <Button variant="ghost" size="sm" leftIcon={<Download />} onClick={() => a.download(files[0]!)}>
          <span className="hidden sm:inline">Download</span>
          <span className="sr-only sm:hidden">Download</span>
        </Button>
      ) : null}
      {files.length === 1 && items.length === 1 && files[0] ? (
        <Button variant="ghost" size="sm" leftIcon={<Copy />} onClick={() => a.copyToFiles(files[0]!)}>
          <span className="hidden sm:inline">Copy into Files</span>
          <span className="sr-only sm:hidden">Copy into Files</span>
        </Button>
      ) : null}
      {editable.length === items.length && sameDrive ? (
        <Button variant="ghost" size="sm" leftIcon={<FolderInput />} onClick={() => a.move(items)}>
          <span className="hidden sm:inline">Move</span>
          <span className="sr-only sm:hidden">Move</span>
        </Button>
      ) : null}
      {editable.length ? <IconButton label={`Move to ${connectedProductName(editable[0]!)} trash`} tooltip icon={<Trash />} variant="danger" size="sm" onClick={() => a.trash(editable)} /> : null}
    </div>
  );
}

function ConnectedFileView({ detail, orgId }: { detail: FilesConnectedDetail; orgId: string | null }) {
  const item = detail.item;
  const a = useFilesConnectedActions();
  const menu = useFilesConnectedItemMenu(item, { showDetails: false });
  const platformOnly = item.native && item.preview === "other";
  return (
    <div className="flex flex-col gap-6">
      <header className="flex flex-col gap-2">
        <ConnectedCrumbs detail={detail} />
        <div className="flex min-h-control-lg items-center gap-2">
          <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg" title={item.name}>
            <span className="truncate">{item.name}</span>
            <SourceBadge source={item.source} iconOnly size="md" className="shrink-0" />
          </h1>
          {item.web_url ? (
            <Button variant={item.native ? "primary" : "ghost"} size="auto" rightIcon={<ExternalLink />} onClick={() => openExternal(item.web_url)}>
              <span className="hidden sm:inline">{openOnPlatformLabel(item)}</span>
              <span className="sr-only sm:hidden">{openOnPlatformLabel(item)}</span>
            </Button>
          ) : null}
          {!platformOnly && a ? (
            <Button variant="secondary" size="auto" leftIcon={<Download />} onClick={() => a.download(item)}>
              <span className="hidden sm:inline">Download</span>
              <span className="sr-only sm:hidden">Download</span>
            </Button>
          ) : null}
          <ResourceMenu feature={`connectors:${item.connection_id}`} actions={menu} label={`More actions for ${item.name}`} />
        </div>
      </header>
      {item.native ? (
        <Banner tone="info" action={item.web_url ? <Button variant="ghost" size="sm" onClick={() => openExternal(item.web_url)}>{openOnPlatformLabel(item)}</Button> : undefined}>
          {`${filesKindLabel(item)} files are read-only here.`}
        </Banner>
      ) : null}
      <div className="flex flex-col gap-section xl:grid xl:grid-cols-[minmax(0,1fr)_20rem] xl:gap-8">
        <div className="min-w-0" aria-label={`${filesKindLabel(item)} preview`}>
          {platformOnly ? (
            <div className="flex flex-col items-center gap-4 rounded-lg border border-border bg-surface px-card py-12 text-center">
              <ProviderIcon provider={item.source.provider} product={item.source.product} size={40} />
              <p className="type-title-sm text-fg">{filesKindLabel(item)} open only in {connectedProductName(item)}</p>
              {item.web_url ? (
                <Button rightIcon={<ExternalLink />} onClick={() => openExternal(item.web_url)}>
                  {openOnPlatformLabel(item)}
                </Button>
              ) : null}
            </div>
          ) : (
            <FilesPreview item={item} source={{ kind: "connected", connectionId: item.connection_id, ref: item.ref, orgId }} onDownload={() => a?.download(item)} />
          )}
        </div>
        <aside className="min-w-0" aria-label="Details">
          <FilesConnectedDetails item={item} path={detail.path.map((c) => c.name)} orgId={orgId} />
        </aside>
      </div>
    </div>
  );
}

// ---- search across sources ---------------------------------------------------------------------

/** One connected account's hits for the drive search, under its badge (the "All" source filter). */
export function ConnectedSearchGroup({ source, q, orgId, limit = 8 }: { source: FilesConnectedSource; q: string; orgId: string | null; limit?: number }) {
  const search = useFilesConnectedSearch(source.status === "active" ? source.id : null, q, orgId);
  const a = useFilesConnectedActions();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [view] = useViewMode();
  const items = (search.data?.items ?? []).slice(0, limit === 0 ? undefined : limit);
  if (source.status !== "active") return null;
  return (
    <section className="flex flex-col gap-2" aria-label={`Results from ${source.label}`}>
      <h2 className="flex items-center gap-2 type-label font-semibold text-fg-muted">
        {source.provider ? <ProviderIcon provider={source.provider} product={source.product} size={16} /> : null}
        {source.label}
      </h2>
      {search.isPending ? (
        <SkeletonGroup label={`Searching ${source.label}`} className="flex flex-col">
          <SkeletonRow />
        </SkeletonGroup>
      ) : search.isError ? (
        <p className="type-caption text-fg-muted">{errorMessage(search.error, `Couldn't search ${source.label}.`)}</p>
      ) : items.length === 0 ? (
        <p className="type-caption text-fg-muted">No matches.</p>
      ) : (
        <>
          <ItemList items={items} view={view} selected={selected} onSelect={setSelected} onOpen={(it) => a?.open(it as FilesConnectedItem)} label={`Results from ${source.label}`} />
          {limit === 0 ? <LoadMore hasNextPage={!!search.hasNextPage} isFetchingNextPage={search.isFetchingNextPage} fetchNextPage={search.fetchNextPage} /> : null}
        </>
      )}
    </section>
  );
}

// ---- shortcuts ---------------------------------------------------------------------------------

export { ShortcutView } from "./ShortcutView";

/** Icons the sidebar uses for drives. */
export function connectedDriveIcon(kind: FilesConnectedDrive["kind"]) {
  return DRIVE_ICON[kind] ?? HardDrive;
}
