"use client";

import {
  errorMessage,
  filesCanEdit,
  filesConnectedPath,
  filesIsConnected,
  sourceProductLabel,
  useFilesConnectedChildren,
  useFilesConnectedCreateFolder,
  useFilesConnectedDrives,
  useFilesConnectedGetUrl,
  useFilesConnectedMove,
  useFilesConnectedRename,
  useFilesConnectedTrash,
  useFilesCopyToFiles,
  useFilesCreateShortcut,
  useFilesSaveTo,
  useFilesSources,
  type FilesConnectedItem,
  type FilesConnectedSource,
  type FilesItem,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, ListRow, ProviderIcon, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import {
  ArrowLeft,
  Copy,
  Download,
  ExternalLink,
  Folder,
  FolderInput,
  FolderOpen,
  HardDrive,
  Info,
  Link2,
  Pencil,
  Share2,
  Trash,
  Upload,
  Users,
  type LucideIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { ResourceMenuAction } from "@/features/registry";
import { FolderPickerDialog, startDownload } from "./actions";
import { useFilesUploads } from "./uploads";

/** What screens can do with connected items; the dialogs live once in FilesConnectedProvider. */
export interface FilesConnectedActions {
  orgId: string | null;
  sources: FilesConnectedSource[];
  sourceOf: (connId: string) => FilesConnectedSource | undefined;
  open: (item: FilesConnectedItem) => void;
  download: (item: FilesConnectedItem) => void;
  rename: (item: FilesConnectedItem) => void;
  move: (items: FilesConnectedItem[]) => void;
  trash: (items: FilesConnectedItem[]) => void;
  copyToFiles: (item: FilesConnectedItem) => void;
  addShortcut: (item: FilesConnectedItem) => void;
  newVersion: (item: FilesConnectedItem) => void;
  newFolder: (connId: string, parentRef: string) => void;
  details: (item: FilesConnectedItem) => void;
  detailsItem: FilesConnectedItem | null;
  closeDetails: () => void;
  /** Save a Files file to a connected drive (picks the folder). */
  saveTo: (item: FilesItem, source: FilesConnectedSource) => void;
}

const Ctx = createContext<FilesConnectedActions | null>(null);

/** The connected actions, or null outside the drive's frame. */
export function useFilesConnectedActions(): FilesConnectedActions | null {
  return useContext(Ctx);
}

/** "Google Drive", "OneDrive", "SharePoint", or "Google Docs" for a native document. */
export function connectedProductName(item: Pick<FilesConnectedItem, "source">): string {
  return sourceProductLabel(item.source.provider, item.source.product);
}

/** "Open in Google Docs" for native documents, else "Open in Google Drive". */
export function openOnPlatformLabel(item: FilesConnectedItem): string {
  return `Open in ${item.native && item.kind_label ? item.kind_label : connectedProductName(item)}`;
}

/** Opens a platform page in a new tab (never inside the app). */
export function openExternal(url: string) {
  window.open(url, "_blank", "noopener,noreferrer");
}

/** Holds the connected dialogs: rename, move within a drive, new folder, trash, Copy into Files, shortcuts, Save to. */
export function FilesConnectedProvider({ orgId, children }: { orgId: string | null; children: ReactNode }) {
  const router = useRouter();
  const uploads = useFilesUploads();
  const sources = useFilesSources(orgId);
  const getUrl = useFilesConnectedGetUrl();
  const copy = useFilesCopyToFiles();
  const shortcut = useFilesCreateShortcut(orgId ?? "");
  const move = useFilesConnectedMove(orgId);
  const save = useFilesSaveTo();
  const [renaming, setRenaming] = useState<FilesConnectedItem | null>(null);
  const [moving, setMoving] = useState<FilesConnectedItem[] | null>(null);
  const [trashing, setTrashing] = useState<FilesConnectedItem[] | null>(null);
  const [creating, setCreating] = useState<{ connId: string; parentRef: string } | null>(null);
  const [picking, setPicking] = useState<{ item: FilesConnectedItem; mode: "copy" | "shortcut" } | null>(null);
  const [saving, setSaving] = useState<{ item: FilesItem; source: FilesConnectedSource } | null>(null);
  const [detailsItem, setDetailsItem] = useState<FilesConnectedItem | null>(null);
  const versionInput = useRef<HTMLInputElement>(null);
  const versionTarget = useRef<FilesConnectedItem | null>(null);
  const list = useMemo(() => sources.data?.sources ?? [], [sources.data]);

  const actions = useMemo<FilesConnectedActions>(
    () => ({
      orgId,
      sources: list,
      sourceOf: (id) => list.find((s) => s.id === id),
      open: (item) => router.push(item.link),
      download: (item) =>
        getUrl.mutate(
          { connId: item.connection_id, ref: item.ref, orgId, disposition: "attachment" },
          { onSuccess: (u) => startDownload(u.url), onError: (e) => toast.error(errorMessage(e, "Couldn't start the download.")) },
        ),
      rename: setRenaming,
      move: setMoving,
      trash: setTrashing,
      copyToFiles: (item) => setPicking({ item, mode: "copy" }),
      addShortcut: (item) => setPicking({ item, mode: "shortcut" }),
      newVersion: (item) => {
        versionTarget.current = item;
        versionInput.current?.click();
      },
      newFolder: (connId, parentRef) => setCreating({ connId, parentRef }),
      details: setDetailsItem,
      detailsItem,
      closeDetails: () => setDetailsItem(null),
      saveTo: (item, source) => setSaving({ item, source }),
    }),
    [orgId, list, router, getUrl, detailsItem],
  );

  return (
    <Ctx.Provider value={actions}>
      {children}
      <input
        ref={versionInput}
        type="file"
        hidden
        aria-hidden
        tabIndex={-1}
        onChange={(e) => {
          const file = e.target.files?.[0];
          const item = versionTarget.current;
          if (file && item) uploads.addConnectedVersion(item, file, orgId);
          e.target.value = "";
        }}
      />
      <ConnectedRenameDialog item={renaming} orgId={orgId} onClose={() => setRenaming(null)} />
      {moving && moving[0] ? (
        <ConnectedFolderPicker
          connId={moving[0].connection_id}
          orgId={orgId}
          driveRef={moving[0].drive_ref}
          exclude={new Set(moving.map((m) => m.ref))}
          title={moving.length === 1 ? `Move “${moving[0].name}” to` : `Move ${moving.length} items to`}
          actionLabel="Move here"
          onClose={() => setMoving(null)}
          onPick={(ref) => moveAll(moving, ref)}
        />
      ) : null}
      {saving ? (
        <ConnectedFolderPicker
          connId={saving.source.id}
          orgId={orgId}
          title={`Save “${saving.item.name}” to ${saving.source.label}`}
          actionLabel="Save here"
          onClose={() => setSaving(null)}
          onPick={(ref) => saveItem(saving.item, saving.source, ref)}
        />
      ) : null}
      {creating ? <ConnectedNewFolderDialog target={creating} orgId={orgId} onClose={() => setCreating(null)} /> : null}
      <ConnectedTrashDialog items={trashing} orgId={orgId} onClose={() => setTrashing(null)} />
      {picking && orgId ? (
        <FolderPickerDialog
          orgId={orgId}
          mode="copy"
          items={[picking.item]}
          title={picking.mode === "copy" ? `Copy “${picking.item.name}” into` : `Add a shortcut to “${picking.item.name}” in`}
          actionVerb={picking.mode === "copy" ? "Copy" : "Add"}
          onClose={() => setPicking(null)}
          onPick={(parentId) =>
            picking.mode === "copy"
              ? copy.mutateAsync({ connId: picking.item.connection_id, ref: picking.item.ref, orgId, parentId }).then((it) =>
                  toast.success(`Copied as “${it.name}”`, { action: { label: "Open", onClick: () => router.push(it.link) } }),
                )
              : shortcut.mutateAsync({ connId: picking.item.connection_id, ref: picking.item.ref, parentId }).then((it) =>
                  toast.success(`Shortcut “${it.name}” added`, { action: { label: "Open folder", onClick: () => router.push(it.parent_id ? `/files/${it.parent_id}` : "/files") } }),
                )
          }
        />
      ) : null}
    </Ctx.Provider>
  );

  async function moveAll(items: FilesConnectedItem[], parentRef: string): Promise<void> {
    const results = await Promise.allSettled(items.map((i) => move.mutateAsync({ connId: i.connection_id, ref: i.ref, parentRef })));
    const failed = results.filter((r): r is PromiseRejectedResult => r.status === "rejected");
    if (failed.length === 0) toast.success(items.length === 1 ? `“${items[0]?.name}” moved` : `${items.length} items moved`);
    else toast.error(errorMessage(failed[0]?.reason, "Couldn't move that."));
  }
  async function saveItem(item: FilesItem, source: FilesConnectedSource, parentRef: string): Promise<void> {
    const it = await save.mutateAsync({ id: item.id, connId: source.id, parentRef });
    toast.success(`Saved to ${source.label}`, { action: { label: "Open", onClick: () => router.push(it.link) } });
  }
}

// ---- the ⋮ menu rows -----------------------------------------------------------------------

/** Rows for a connected item's ⋮ menu: platform actions in place of the app's Share…. */
export function useFilesConnectedItemMenu(item: FilesConnectedItem | null | undefined, opts: { showOpen?: boolean; showDetails?: boolean } = {}): ResourceMenuAction[] {
  const a = useFilesConnectedActions();
  if (!item || !a) return [];
  const edit = filesCanEdit(item);
  const file = item.type === "file";
  const rows: ResourceMenuAction[] = [];
  const add = (id: string, label: string, icon: LucideIcon, onSelect: () => void, destructive = false) => rows.push({ id, label, icon, onSelect, destructive });
  if (opts.showOpen) add("open", item.type === "folder" ? "Open" : "Open preview", item.type === "folder" ? FolderOpen : ExternalLink, () => a.open(item));
  if (item.web_url) add("open-platform", openOnPlatformLabel(item), ExternalLink, () => openExternal(item.web_url));
  if (file && (item.preview !== "other" || !item.native)) add("download", "Download", Download, () => a.download(item));
  if (file && edit) add("version", "Upload new version", Upload, () => a.newVersion(item));
  if (edit) add("rename", "Rename", Pencil, () => a.rename(item));
  if (edit) add("move", "Move", FolderInput, () => a.move([item]));
  if (file && (!item.native || item.preview === "pdf")) add("copy-to-files", "Copy into Files", Copy, () => a.copyToFiles(item));
  add("shortcut", "Add shortcut to Files", Link2, () => a.addShortcut(item));
  if (item.web_url) add("sharing", `Manage sharing in ${connectedProductName(item)}`, Share2, () => openExternal(item.web_url));
  if (opts.showDetails !== false) add("details", "Details", Info, () => a.details(item));
  if (edit) add("trash", `Move to ${connectedProductName(item)} trash`, Trash, () => a.trash([item]), true);
  return rows;
}

/** "Save to Google Drive" rows for an app file, one per connected account that allows changes. */
export function useFilesSaveToRows(item: FilesItem | null | undefined): ResourceMenuAction[] {
  const a = useFilesConnectedActions();
  if (!item || !a || item.type !== "file" || filesIsConnected(item) || item.trashed_at) return [];
  const writable = a.sources.filter((s) => s.type === "connected" && s.status === "active" && s.can_write);
  const counts = new Map<string, number>();
  for (const s of writable) counts.set(`${s.provider}.${s.product}`, (counts.get(`${s.provider}.${s.product}`) ?? 0) + 1);
  return writable.map((s) => {
    const product = sourceProductLabel(s.provider ?? "", s.product);
    const label = (counts.get(`${s.provider}.${s.product}`) ?? 0) > 1 ? `Save to ${product} · ${s.account}` : `Save to ${product}`;
    return { id: `save-to-${s.id}`, label, icon: Upload, onSelect: () => a.saveTo(item, s) };
  });
}

// ---- dialogs ---------------------------------------------------------------------------------

function ConnectedRenameDialog({ item, orgId, onClose }: { item: FilesConnectedItem | null; orgId: string | null; onClose: () => void }) {
  const rename = useFilesConnectedRename(orgId);
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!item) return;
    setName(item.name);
    setError(null);
    const t = setTimeout(() => {
      const dot = item.type === "file" ? item.name.lastIndexOf(".") : -1;
      input.current?.setSelectionRange(0, dot > 0 ? dot : item.name.length);
    }, 50);
    return () => clearTimeout(t);
  }, [item]);
  const submit = () => {
    if (!item) return;
    const next = name.trim();
    if (!next || next === item.name) return onClose();
    rename.mutate(
      { connId: item.connection_id, ref: item.ref, name: next },
      {
        onSuccess: () => {
          toast.success("Renamed");
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't rename. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={item !== null}
      onOpenChange={(o) => !o && onClose()}
      title="Rename"
      info={item ? `Renames it in ${connectedProductName(item)} too.` : undefined}
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit} loading={rename.isPending}>
            Save
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" error={error}>
          <Input ref={input} value={name} onChange={(e) => setName(e.target.value)} maxLength={255} autoFocus />
        </Field>
      </form>
    </Dialog>
  );
}

function ConnectedNewFolderDialog({ target, orgId, onClose }: { target: { connId: string; parentRef: string }; orgId: string | null; onClose: () => void }) {
  const create = useFilesConnectedCreateFolder(target.connId, orgId);
  const [name, setName] = useState("Untitled folder");
  const [error, setError] = useState<string | null>(null);
  const submit = () =>
    create.mutate(
      { parentRef: target.parentRef, name: name.trim() },
      {
        onSuccess: (it) => {
          toast.success(`“${it.name}” created`);
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't create the folder. Try again.")),
      },
    );
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="New folder"
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit} loading={create.isPending} disabled={!name.trim()}>
            Create
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" error={error}>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={255} autoFocus onFocus={(e) => e.currentTarget.select()} />
        </Field>
      </form>
    </Dialog>
  );
}

function ConnectedTrashDialog({ items, orgId, onClose }: { items: FilesConnectedItem[] | null; orgId: string | null; onClose: () => void }) {
  const trash = useFilesConnectedTrash(orgId);
  const first = items?.[0];
  const product = first ? connectedProductName(first) : "";
  return (
    <ConfirmDialog
      open={!!first}
      onOpenChange={(o) => !o && onClose()}
      title={items?.length === 1 ? `Move “${first?.name}” to the ${product} trash?` : `Move ${items?.length ?? 0} items to the ${product} trash?`}
      tone="danger"
      confirmLabel="Move to trash"
      onConfirm={async () => {
        const list = items ?? [];
        const results = await Promise.allSettled(list.map((i) => trash.mutateAsync({ connId: i.connection_id, ref: i.ref })));
        const failed = results.filter((r): r is PromiseRejectedResult => r.status === "rejected");
        if (failed.length === 0) toast.success(list.length === 1 ? `Moved to the ${product} trash` : `${list.length} items moved to the ${product} trash`);
        else toast.error(errorMessage(failed[0]?.reason, "Couldn't move that to the trash."));
        onClose();
      }}
    >
      <p className="type-body text-fg-muted">You can restore it from the trash in {product}.</p>
    </ConfirmDialog>
  );
}

/** Choose a folder in one connection: its drives, then folders (or only inside driveRef, for moves). */
export function ConnectedFolderPicker({
  connId,
  orgId,
  driveRef,
  exclude,
  title,
  actionLabel,
  onClose,
  onPick,
}: {
  connId: string;
  orgId: string | null;
  driveRef?: string;
  exclude?: Set<string>;
  title: string;
  actionLabel: string;
  onClose: () => void;
  onPick: (ref: string) => Promise<unknown>;
}) {
  const [trail, setTrail] = useState<{ ref: string; name: string }[]>(driveRef ? [{ ref: driveRef, name: "" }] : []);
  const [busy, setBusy] = useState(false);
  const here = trail[trail.length - 1];
  const drives = useFilesConnectedDrives(!here ? connId : null, orgId);
  const children = useFilesConnectedChildren(here ? connId : null, here?.ref ?? null, { sort: "name" }, orgId);
  const folders = (children.data?.items ?? []).filter((i) => i.type === "folder");
  const pickable = !!here && here.ref !== "shared-with-me" && !exclude?.has(here.ref);
  const pick = async () => {
    if (!here) return;
    setBusy(true);
    try {
      await onPick(here.ref);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "That didn't work. Try again."));
    } finally {
      setBusy(false);
    }
  };
  const list = here ? children : drives;
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={title}
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={() => void pick()} loading={busy} disabled={!pickable}>
            {actionLabel}
          </Button>
        </>
      }
    >
      <div className="flex min-h-72 flex-col gap-2">
        {trail.length > (driveRef ? 1 : 0) ? (
          <button
            type="button"
            onClick={() => setTrail((t) => t.slice(0, -1))}
            className="flex min-h-control items-center gap-2 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap"
          >
            <ArrowLeft className="size-4" aria-hidden />
            {trail[trail.length - 2]?.name || "Drives"}
          </button>
        ) : null}
        {list.isPending ? (
          <SkeletonGroup label="Loading folders" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load folders" info={errorMessage(list.error, "")} onRetry={() => void list.refetch()} />
        ) : !here ? (
          <ul className="flex flex-col">
            {(drives.data?.items ?? [])
              .filter((d) => d.kind !== "shared_with_me")
              .map((d) => (
                <PickerRow key={d.ref} icon={d.kind === "shared_drive" || d.kind === "library" ? Users : HardDrive} label={d.site ? `${d.site} · ${d.name}` : d.name} onClick={() => setTrail([{ ref: d.ref, name: d.name }])} />
              ))}
          </ul>
        ) : folders.length === 0 ? (
          <EmptyState compact icon={<Folder />} title="No folders here" />
        ) : (
          <ul className="flex flex-col">
            {folders.map((f) => (
              <PickerRow key={f.ref} icon={Folder} label={f.name} disabled={exclude?.has(f.ref)} onClick={() => setTrail((t) => [...t, { ref: f.ref, name: f.name }])} />
            ))}
          </ul>
        )}
        {list.hasNextPage ? (
          <Button variant="ghost" size="sm" className="self-center" onClick={() => void list.fetchNextPage()} loading={list.isFetchingNextPage}>
            Show more
          </Button>
        ) : null}
      </div>
    </Dialog>
  );
}

function PickerRow({ icon: Icon, label, onClick, disabled }: { icon: LucideIcon; label: string; onClick: () => void; disabled?: boolean | undefined }) {
  return (
    <li>
      <ListRow asChild leading={<Icon className="size-5 text-fg-muted" aria-hidden />} title={label}>
        <button type="button" onClick={onClick} disabled={disabled} className="w-full text-left disabled:opacity-50" />
      </ListRow>
    </li>
  );
}

/** A provider mark sized for rows and headers. */
export function SourceMark({ item, size = 16 }: { item: Pick<FilesConnectedItem, "source">; size?: number }) {
  return <ProviderIcon provider={item.source.provider} product={item.source.product} size={size} />;
}

/** The connected item's app path (for links built outside the item). */
export const connectedItemPath = filesConnectedPath;
