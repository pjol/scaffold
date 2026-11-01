"use client";

import {
  errorMessage,
  filesCanEdit,
  filesCanManage,
  filesEndpoints,
  filesTypePath,
  orgsNoun,
  useApi,
  useFilesBatch,
  useFilesChildren,
  useFilesCopy,
  useFilesCreateFolder,
  useFilesDrive,
  useFilesGetUrl,
  useFilesRename,
  useFilesSection,
  useFilesStar,
  type FilesItem,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, ListRow, Select, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
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
  Pencil,
  RotateCcw,
  Star,
  StarOff,
  Trash,
  Upload,
  Users,
  X,
  type LucideIcon,
} from "lucide-react";
import { useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { ResourceMenuAction } from "@/features/registry";
import { ShareDialog } from "@/features/access";
import { filesTypeIcon } from "./icons";
import { useFilesUploads } from "./uploads";

/** What a screen can do with items; dialogs live once in FilesActionsProvider. */
export interface FilesActions {
  orgId: string | null;
  open: (item: FilesItem) => void;
  download: (items: FilesItem[]) => void;
  rename: (item: FilesItem) => void;
  move: (items: FilesItem[]) => void;
  copy: (item: FilesItem) => void;
  star: (item: FilesItem, on: boolean) => void;
  trash: (items: FilesItem[]) => void;
  restore: (items: FilesItem[]) => void;
  remove: (items: FilesItem[]) => void;
  newVersion: (item: FilesItem) => void;
  newFolder: (parentId: string | null, shared?: boolean) => void;
  details: (item: FilesItem) => void;
  /** The item whose details panel is open. */
  detailsId: string | null;
  closeDetails: () => void;
}

const Ctx = createContext<FilesActions | null>(null);

export function useFilesActions(): FilesActions {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useFilesActions must be used inside FilesActionsProvider");
  return ctx;
}

/** Downloads through a link: attachments never navigate away. */
export function startDownload(url: string) {
  const a = document.createElement("a");
  a.href = url;
  a.rel = "noopener";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function summarize(results: { ok: boolean; error?: { message: string } }[], done: string): void {
  const failed = results.filter((r) => !r.ok);
  if (failed.length === 0) toast.success(done);
  else if (failed.length === results.length) toast.error(failed[0]?.error?.message ?? "That didn't work. Try again.");
  else toast.warning(`${results.length - failed.length} done, ${failed.length} failed: ${failed[0]?.error?.message ?? ""}`);
}

const plural = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"}`;

/** Holds the drive's dialogs (rename, new folder, move or copy, delete forever, share after create). */
export function FilesActionsProvider({ orgId, children }: { orgId: string | null; children: ReactNode }) {
  const router = useRouter();
  const uploads = useFilesUploads();
  const getUrl = useFilesGetUrl();
  const api = useApi();
  const batch = useFilesBatch();
  const star = useFilesStar();
  const copyM = useFilesCopy();
  const [renaming, setRenaming] = useState<FilesItem | null>(null);
  const [moving, setMoving] = useState<{ items: FilesItem[]; mode: "move" | "copy" } | null>(null);
  const [removing, setRemoving] = useState<FilesItem[] | null>(null);
  const [creating, setCreating] = useState<{ parentId: string | null; shared: boolean } | null>(null);
  const [sharing, setSharing] = useState<FilesItem | null>(null);
  const [detailsId, setDetailsId] = useState<string | null>(null);
  const versionInput = useRef<HTMLInputElement>(null);
  const versionTarget = useRef<FilesItem | null>(null);

  const actions = useMemo<FilesActions>(
    () => ({
      orgId,
      open: (item) => router.push(item.link),
      download: (items) => {
        const one = items.length === 1 ? items[0] : undefined;
        if (one && one.type === "file") {
          getUrl.mutate({ id: one.id, disposition: "attachment" }, { onSuccess: (u) => startDownload(u.url), onError: (e) => toast.error(errorMessage(e, "Couldn't start the download.")) });
          return;
        }
        const ids = items.filter((i) => i.type === "file" || i.type === "folder").map((i) => i.id);
        const org = items[0]?.org_id;
        if (ids.length === 0 || !org) return;
        filesEndpoints(api.http).archive(org, ids).then(
          (a) => {
            toast.success("Preparing your zip");
            startDownload(a.url);
          },
          (e: unknown) => toast.error(errorMessage(e, "Couldn't start the download.")),
        );
      },
      rename: setRenaming,
      move: (items) => setMoving({ items, mode: "move" }),
      copy: (item) => setMoving({ items: [item], mode: "copy" }),
      star: (item, on) => star.mutate({ id: item.id, on }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the star.")) }),
      trash: (items) =>
        batch.mutate(
          { action: "trash", ids: items.map((i) => i.id) },
          {
            onSuccess: ({ results }) => {
              const ok = results.filter((r) => r.ok).map((r) => r.id);
              if (ok.length === results.length) {
                toast.success(items.length === 1 ? `“${items[0]?.name}” moved to the trash` : `${plural(ok.length, "item")} moved to the trash`, {
                  action: { label: "Undo", onClick: () => batch.mutate({ action: "restore", ids: ok }) },
                });
              } else summarize(results, "");
            },
            onError: (e) => toast.error(errorMessage(e, "Couldn't move that to the trash.")),
          },
        ),
      restore: (items) =>
        batch.mutate(
          { action: "restore", ids: items.map((i) => i.id) },
          { onSuccess: ({ results }) => summarize(results, items.length === 1 ? `“${items[0]?.name}” restored` : `${plural(items.length, "item")} restored`), onError: (e) => toast.error(errorMessage(e, "Couldn't restore.")) },
        ),
      remove: setRemoving,
      newVersion: (item) => {
        versionTarget.current = item;
        versionInput.current?.click();
      },
      newFolder: (parentId, shared = false) => setCreating({ parentId, shared }),
      details: (item) => setDetailsId(item.id),
      detailsId,
      closeDetails: () => setDetailsId(null),
    }),
    [orgId, router, getUrl, api, batch, star, detailsId],
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
          if (file && item) uploads.addVersion(item, file);
          e.target.value = "";
        }}
      />
      <RenameDialog item={renaming} onClose={() => setRenaming(null)} />
      {orgId ? (
        <NewFolderDialog
          orgId={orgId}
          target={creating}
          onClose={() => setCreating(null)}
          onCreated={(it, shared) => {
            if (shared) setSharing(it);
          }}
        />
      ) : null}
      {orgId && moving ? (
        <FolderPickerDialog
          orgId={orgId}
          mode={moving.mode}
          items={moving.items}
          onClose={() => setMoving(null)}
          onPick={(parentId) => {
            const items = moving.items;
            if (moving.mode === "copy") {
              const src = items[0];
              if (!src) return Promise.resolve();
              return copyM.mutateAsync({ id: src.id, parentId }).then((it) => {
                toast.success(`Copied as “${it.name}”`);
              });
            }
            return batch.mutateAsync({ action: "move", ids: items.map((i) => i.id), parentId }).then(({ results }) => summarize(results, items.length === 1 ? `“${items[0]?.name}” moved` : `${plural(items.length, "item")} moved`));
          }}
        />
      ) : null}
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={removing?.length === 1 ? `Delete “${removing[0]?.name}” forever?` : `Delete ${plural(removing?.length ?? 0, "item")} forever?`}
        tone="danger"
        confirmLabel="Delete forever"
        onConfirm={async () => {
          const items = removing ?? [];
          const { results } = await batch.mutateAsync({ action: "delete", ids: items.map((i) => i.id) });
          summarize(results, items.length === 1 ? "Deleted" : `${plural(items.length, "item")} deleted`);
          setRemoving(null);
        }}
      >
        <p className="type-body text-fg-muted">Everything inside goes too, with every version. This can't be undone.</p>
      </ConfirmDialog>
      {sharing ? <ShareDialog kind="files.item" id={sharing.id} title={sharing.name} open onOpenChange={(o) => !o && setSharing(null)} /> : null}
    </Ctx.Provider>
  );
}

// ---- the ⋮ menu rows ---------------------------------------------------------------------

/** The screen's own rows for an item's ⋮ menu (Share… and AI agent access come from other features). */
export function useFilesItemMenu(item: FilesItem | null | undefined, opts: { showOpen?: boolean; showDetails?: boolean } = {}): ResourceMenuAction[] {
  const a = useFilesActions();
  const drive = useFilesDrive(a.orgId);
  if (!item) return [];
  const edit = filesCanEdit(item);
  const rows: ResourceMenuAction[] = [];
  const add = (id: string, label: string, icon: LucideIcon, onSelect: () => void, destructive = false) => rows.push({ id, label, icon, onSelect, destructive });
  if (item.trashed_at) {
    if (edit) add("restore", "Restore", RotateCcw, () => a.restore([item]));
    if (filesCanManage(item)) add("delete", "Delete forever", X, () => a.remove([item]), true);
    return rows;
  }
  if (opts.showOpen) add("open", item.type === "folder" ? "Open" : item.type === "file" ? "Open preview" : `Open ${item.type_info?.title.toLowerCase() ?? ""}`.trim(), item.type === "folder" ? FolderOpen : ExternalLink, () => a.open(item));
  if (item.type === "file" || item.type === "folder") add("download", item.type === "folder" ? "Download as zip" : "Download", Download, () => a.download([item]));
  if (item.type === "file" && edit) add("version", "Upload new version", Upload, () => a.newVersion(item));
  if (edit) add("rename", "Rename", Pencil, () => a.rename(item));
  if (edit) add("move", "Move", FolderInput, () => a.move([item]));
  if (item.type === "file" || item.type === "folder") add("copy", "Make a copy", Copy, () => a.copy(item));
  add("star", item.starred ? "Remove star" : "Star", item.starred ? StarOff : Star, () => a.star(item, !item.starred));
  if (opts.showDetails !== false) add("details", "Details and activity", Info, () => a.details(item));
  if (item.type === "folder" && edit) {
    for (const t of drive.data?.types ?? []) {
      if (t.convert_path) rows.push({ id: `convert-${t.type}`, label: t.convert_label, icon: filesTypeIcon(t.icon), onSelect: () => (window.location.href = filesTypePath(t.convert_path, item.id)) });
    }
  }
  if (edit) add("trash", "Move to trash", Trash, () => a.trash([item]), true);
  return rows;
}

// ---- dialogs -------------------------------------------------------------------------------

function RenameDialog({ item, onClose }: { item: FilesItem | null; onClose: () => void }) {
  const rename = useFilesRename();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!item) return;
    setName(item.name);
    setError(null);
    // Select the name without its extension, like a desktop file manager.
    const t = setTimeout(() => {
      const el = input.current;
      if (!el) return;
      const dot = item.type === "file" ? item.name.lastIndexOf(".") : -1;
      el.setSelectionRange(0, dot > 0 ? dot : item.name.length);
    }, 50);
    return () => clearTimeout(t);
  }, [item]);
  const submit = () => {
    if (!item) return;
    const next = name.trim();
    if (!next || next === item.name) return onClose();
    rename.mutate(
      { id: item.id, name: next },
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

// A function so the labels read the app's noun (configureOrgsNoun) when the dialog renders.
const generalOptions = () => [
  { value: "none", label: "Only people added" },
  { value: "view", label: `Everyone in the ${orgsNoun()} can view` },
  { value: "edit", label: `Everyone in the ${orgsNoun()} can edit` },
];

function NewFolderDialog({ orgId, target, onClose, onCreated }: { orgId: string; target: { parentId: string | null; shared: boolean } | null; onClose: () => void; onCreated: (it: FilesItem, shared: boolean) => void }) {
  const create = useFilesCreateFolder(orgId);
  const [name, setName] = useState("Untitled folder");
  const [general, setGeneral] = useState("none");
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!target) return;
    setName(target.shared ? "" : "Untitled folder");
    setGeneral("none");
    setError(null);
  }, [target]);
  const submit = () => {
    if (!target) return;
    create.mutate(
      {
        parent_id: target.parentId,
        name: name.trim(),
        ...(target.shared ? { shared: true, ...(general !== "none" ? { general: general as "view" | "edit" } : {}) } : {}),
      },
      {
        onSuccess: (it) => {
          onClose();
          onCreated(it, target.shared);
        },
        onError: (e) => setError(errorMessage(e, "Couldn't create the folder. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={target !== null}
      onOpenChange={(o) => !o && onClose()}
      title={target?.shared ? "New shared folder" : "New folder"}
      info={target?.shared ? `The ${orgsNoun()} owns shared folders, so they stay when people leave. Add teams and people next.` : undefined}
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
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="Name" error={error}>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={255} autoFocus onFocus={(e) => e.currentTarget.select()} placeholder={target?.shared ? "Marketing" : undefined} />
        </Field>
        {target?.shared ? (
          <Field label="General access">
            <Select options={generalOptions()} value={general} onValueChange={setGeneral} />
          </Field>
        ) : null}
      </form>
    </Dialog>
  );
}

type PickerPlace = { kind: "root" } | { kind: "my" } | { kind: "shared" } | { kind: "folder"; folder: FilesItem; trail: FilesItem[] };

/** Choose a destination folder: My drive, or any folder you can edit (shared folders included). */
function FolderPickerDialog({ orgId, mode, items, onClose, onPick }: { orgId: string; mode: "move" | "copy"; items: FilesItem[]; onClose: () => void; onPick: (parentId: string | null) => Promise<unknown> }) {
  const [place, setPlace] = useState<PickerPlace>({ kind: "root" });
  const [busy, setBusy] = useState(false);
  const exclude = useMemo(() => new Set(items.map((i) => i.id)), [items]);
  const my = useFilesSection(place.kind === "my" ? orgId : null, "my", { sort: "name" });
  const shared = useFilesSection(place.kind === "shared" ? orgId : null, "shared", { sort: "name" });
  const children = useFilesChildren(place.kind === "folder" ? place.folder.id : null, { sort: "name" });
  const list = place.kind === "my" ? my : place.kind === "shared" ? shared : children;
  const folders = (list.data?.items ?? []).filter((i) => i.type === "folder");
  const title = mode === "copy" ? `Copy “${items[0]?.name ?? ""}” to` : items.length === 1 ? `Move “${items[0]?.name ?? ""}” to` : `Move ${plural(items.length, "item")} to`;
  const here: { id: string | null; label: string; allowed: boolean } =
    place.kind === "my"
      ? { id: null, label: "My drive", allowed: true }
      : place.kind === "folder"
        ? { id: place.folder.id, label: place.folder.name, allowed: filesCanEdit(place.folder) && !exclude.has(place.folder.id) }
        : { id: null, label: "", allowed: false };
  const back = () => {
    if (place.kind === "folder") {
      const trail = place.trail.slice(0, -1);
      const parent = place.trail[place.trail.length - 1];
      setPlace(parent ? { kind: "folder", folder: parent, trail } : place.folder.drive === "shared" ? { kind: "shared" } : { kind: "my" });
    } else setPlace({ kind: "root" });
  };
  const pick = async () => {
    setBusy(true);
    try {
      await onPick(here.id);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, mode === "copy" ? "Couldn't copy. Try again." : "Couldn't move. Try again."));
    } finally {
      setBusy(false);
    }
  };
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
          <Button onClick={() => void pick()} loading={busy} disabled={!here.allowed}>
            {here.allowed ? `${mode === "copy" ? "Copy" : "Move"} to ${here.label}` : mode === "copy" ? "Copy here" : "Move here"}
          </Button>
        </>
      }
    >
      <div className="flex min-h-72 flex-col gap-2">
        {place.kind !== "root" ? (
          <button type="button" onClick={back} className="flex min-h-control items-center gap-2 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
            <ArrowLeft className="size-4" aria-hidden />
            {place.kind === "folder" ? (place.trail[place.trail.length - 1]?.name ?? (place.folder.drive === "shared" ? "Shared folders" : "My drive")) : "All drives"}
          </button>
        ) : null}
        {place.kind === "root" ? (
          <ul className="flex flex-col">
            <PickerRow icon={HardDrive} label="My drive" onClick={() => setPlace({ kind: "my" })} />
            <PickerRow icon={Users} label="Shared folders" onClick={() => setPlace({ kind: "shared" })} />
          </ul>
        ) : list.isPending ? (
          <SkeletonGroup label="Loading folders" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : list.isError ? (
          <ErrorState compact title="Couldn't load folders" onRetry={() => void list.refetch()} />
        ) : folders.length === 0 ? (
          <EmptyState compact icon={<Folder />} title="No folders here" />
        ) : (
          <ul className="flex flex-col">
            {folders.map((f) => (
              <PickerRow
                key={f.id}
                icon={Folder}
                label={f.name}
                disabled={exclude.has(f.id)}
                onClick={() => setPlace({ kind: "folder", folder: f, trail: place.kind === "folder" ? [...place.trail, place.folder] : [] })}
              />
            ))}
          </ul>
        )}
        {list.hasNextPage ? (
          <Button variant="ghost" size="sm" className="self-center" onClick={() => void list.fetchNextPage()} loading={list.isFetchingNextPage}>
            Show more folders
          </Button>
        ) : null}
      </div>
    </Dialog>
  );
}

function PickerRow({ icon: Icon, label, onClick, disabled }: { icon: LucideIcon; label: string; onClick: () => void; disabled?: boolean }) {
  return (
    <li>
      <ListRow asChild leading={<Icon className="size-5 text-fg-muted" aria-hidden />} title={label}>
        <button type="button" onClick={onClick} disabled={disabled} className="w-full text-left disabled:opacity-50" />
      </ListRow>
    </li>
  );
}
