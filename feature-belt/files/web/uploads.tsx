"use client";

import {
  ApiError,
  errorMessage,
  filesConnectedUpload,
  filesEndpoints,
  filesFormatSize,
  filesSplitPath,
  filesUpload,
  useApi,
  type FilesChunkPutFn,
  type FilesConnectedItem,
  type FilesItem,
  type FilesPutFn,
} from "@scaffoldapp/shared";
import { useQueryClient } from "@tanstack/react-query";
import { Button, IconButton, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronDown, ChevronUp, CircleAlert, CircleCheck, RotateCcw, X } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

/** A file chosen or dropped, with its path inside a dropped folder ("" for loose files). */
export interface FilesPicked {
  file: File;
  /** "Photos/2026/a.jpg" for folder uploads; "" otherwise. */
  path: string;
}

type TaskState = "queued" | "uploading" | "done" | "failed" | "canceled";

interface Task {
  id: string;
  batch: string;
  file: File;
  name: string;
  folders: string[];
  orgId: string;
  parentId: string | null;
  itemId?: string;
  /** A connected drive instead of the app's: bytes go straight to the platform in resumable chunks. */
  connected?: { connectionId: string; parentRef?: string; itemRef?: string; orgId?: string | null };
  state: TaskState;
  sent: number;
  error?: string;
  result?: FilesItem;
}

/** Where a connected upload goes. */
export interface FilesConnectedTarget {
  connectionId: string;
  parentRef: string;
  orgId?: string | null;
}

interface UploadsContext {
  tasks: Task[];
  /** Upload files (and folder trees) into a folder (null: the top of My drive). */
  add: (orgId: string, parentId: string | null, picked: FilesPicked[]) => void;
  /** Upload a file as the new version of an existing one. */
  addVersion: (item: FilesItem, file: File) => void;
  /** Upload loose files into a connected folder. */
  addConnected: (target: FilesConnectedTarget, picked: FilesPicked[]) => void;
  /** Replace a connected file's contents. */
  addConnectedVersion: (item: FilesConnectedItem, file: File, orgId?: string | null) => void;
  maxBytes: number | null;
  setMaxBytes: (n: number | null) => void;
}

const Ctx = createContext<UploadsContext | null>(null);

const CONCURRENCY = 3;

/** Sends bytes with XMLHttpRequest so the tray can show progress. */
const xhrPut: FilesPutFn = (url, headers, body, { signal, onProgress }) =>
  new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => onProgress?.(e.loaded, e.total);
    xhr.onload = () =>
      xhr.status >= 200 && xhr.status < 300
        ? resolve()
        : reject(new ApiError({ status: xhr.status, code: xhr.status >= 500 ? "internal" : "invalid_request", message: "Upload failed. Try again." }));
    xhr.onerror = () => reject(new ApiError({ status: 0, code: "network_error", message: "Upload failed. Check your connection and try again." }));
    xhr.onabort = () => reject(new ApiError({ status: 0, code: "aborted", message: "Upload canceled." }));
    if (signal) {
      if (signal.aborted) xhr.abort();
      signal.addEventListener("abort", () => xhr.abort(), { once: true });
    }
    xhr.send(body);
  });

/** Sends one chunk with XMLHttpRequest (progress) and returns the platform's answer, whatever its status. */
const xhrChunk: FilesChunkPutFn = (url, headers, body, { signal, onProgress }) =>
  new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => onProgress?.(e.loaded, e.total);
    xhr.onload = () => resolve({ status: xhr.status, text: xhr.responseText });
    xhr.onerror = () => reject(new ApiError({ status: 0, code: "network_error", message: "Upload failed. Check your connection and try again." }));
    xhr.onabort = () => reject(new ApiError({ status: 0, code: "aborted", message: "Upload canceled." }));
    if (signal) {
      if (signal.aborted) xhr.abort();
      signal.addEventListener("abort", () => xhr.abort(), { once: true });
    }
    xhr.send(body);
  });

let seq = 0;
const nextId = () => `u${Date.now().toString(36)}${(seq++).toString(36)}`;

/**
 * The drive's upload queue (mounted by the files layout, so uploads keep going while you
 * browse): 3 at a time, folder trees created on the way, progress, cancel and retry.
 */
export function FilesUploadsProvider({ children }: { children: ReactNode }) {
  const api = useApi();
  const qc = useQueryClient();
  const [tasks, setTasks] = useState<Task[]>([]);
  const [maxBytes, setMaxBytes] = useState<number | null>(null);
  const controllers = useRef(new Map<string, AbortController>());
  const folders = useRef(new Map<string, Promise<string>>());
  const running = useRef(new Set<string>());
  const tasksRef = useRef(tasks);
  tasksRef.current = tasks;

  const patch = useCallback((id: string, p: Partial<Task>) => setTasks((list) => list.map((t) => (t.id === id ? { ...t, ...p } : t))), []);

  /** The folder a task's path needs, created once per batch (the top one is numbered if the name is taken). */
  const ensureFolder = useCallback(
    (t: Task): Promise<string | null> => {
      if (t.folders.length === 0) return Promise.resolve(t.parentId);
      const f = filesEndpoints(api.http);
      let chain: Promise<string | null> = Promise.resolve(t.parentId);
      for (let i = 0; i < t.folders.length; i++) {
        const key = `${t.batch}/${t.folders.slice(0, i + 1).join("/")}`;
        const name = t.folders[i] ?? "";
        let p = folders.current.get(key);
        if (!p) {
          const parent = chain;
          p = parent.then((parentId) => f.createFolder(t.orgId, { parent_id: parentId, name, on_conflict: i === 0 ? "rename" : "fail" }).then((it) => it.id));
          folders.current.set(key, p);
          p.catch(() => folders.current.delete(key));
        }
        chain = p;
      }
      return chain;
    },
    [api],
  );

  const run = useCallback(
    async (t: Task) => {
      running.current.add(t.id);
      const controller = new AbortController();
      controllers.current.set(t.id, controller);
      patch(t.id, { state: "uploading", sent: 0, error: undefined });
      try {
        if (t.connected) {
          const c = t.connected;
          await filesConnectedUpload(api.http, {
            connectionId: c.connectionId,
            ...(c.orgId ? { orgId: c.orgId } : {}),
            ...(c.parentRef ? { parentRef: c.parentRef } : {}),
            ...(c.itemRef ? { itemRef: c.itemRef } : {}),
            name: t.name,
            body: t.file,
            signal: controller.signal,
            put: xhrChunk,
            onProgress: (sent) => patch(t.id, { sent }),
          });
          patch(t.id, { state: "done", sent: t.file.size });
          void qc.invalidateQueries({ queryKey: ["files", "connected", c.connectionId] });
          return;
        }
        const parentId = await ensureFolder(t);
        const result = await filesUpload(api.http, {
          orgId: t.orgId,
          parentId,
          ...(t.itemId ? { itemId: t.itemId } : {}),
          name: t.name,
          body: t.file,
          signal: controller.signal,
          put: xhrPut,
          onProgress: (sent) => patch(t.id, { sent }),
        });
        patch(t.id, { state: "done", sent: t.file.size, result });
        void qc.invalidateQueries({ queryKey: ["files", "list"] });
        void qc.invalidateQueries({ queryKey: ["files", "drive", t.orgId] });
        if (t.itemId) void qc.invalidateQueries({ queryKey: ["files", "item", t.itemId] });
      } catch (e) {
        if (controller.signal.aborted) patch(t.id, { state: "canceled" });
        else patch(t.id, { state: "failed", error: errorMessage(e, "Upload failed. Try again.") });
      } finally {
        running.current.delete(t.id);
        controllers.current.delete(t.id);
      }
    },
    [api, ensureFolder, patch, qc],
  );

  // Start queued tasks while fewer than CONCURRENCY run.
  useEffect(() => {
    const free = CONCURRENCY - running.current.size;
    if (free <= 0) return;
    tasks.filter((t) => t.state === "queued" && !running.current.has(t.id)).slice(0, free).forEach((t) => void run(t));
  }, [tasks, run]);

  const add = useCallback(
    (orgId: string, parentId: string | null, picked: FilesPicked[]) => {
      const batch = nextId();
      const fresh: Task[] = [];
      const tooBig: string[] = [];
      for (const p of picked) {
        const { folders: dirs, name } = p.path ? filesSplitPath(p.path) : { folders: [], name: p.file.name };
        if (!name || name === ".DS_Store" || name === "Thumbs.db") continue;
        if (maxBytes !== null && p.file.size > maxBytes) {
          tooBig.push(name);
          continue;
        }
        fresh.push({ id: nextId(), batch, file: p.file, name, folders: dirs, orgId, parentId, state: "queued", sent: 0 });
      }
      if (tooBig.length > 0 && maxBytes !== null) {
        toast.error(`${tooBig.length === 1 ? `“${tooBig[0]}” is` : `${tooBig.length} files are`} over ${filesFormatSize(maxBytes)}. Choose smaller files.`);
      }
      if (fresh.length) setTasks((list) => [...list, ...fresh]);
    },
    [maxBytes],
  );

  const addVersion = useCallback(
    (item: FilesItem, file: File) => {
      if (maxBytes !== null && file.size > maxBytes) {
        toast.error(`That file is over ${filesFormatSize(maxBytes)}. Choose a smaller one.`);
        return;
      }
      setTasks((list) => [...list, { id: nextId(), batch: nextId(), file, name: item.name, folders: [], orgId: item.org_id, parentId: item.parent_id, itemId: item.id, state: "queued", sent: 0 }]);
    },
    [maxBytes],
  );

  const addConnected = useCallback((target: FilesConnectedTarget, picked: FilesPicked[]) => {
    const batch = nextId();
    const fresh: Task[] = [];
    for (const p of picked) {
      const name = p.file.name;
      if (!name || name === ".DS_Store" || name === "Thumbs.db" || p.path.includes("/")) continue;
      fresh.push({
        id: nextId(),
        batch,
        file: p.file,
        name,
        folders: [],
        orgId: target.orgId ?? "",
        parentId: null,
        connected: { connectionId: target.connectionId, parentRef: target.parentRef, orgId: target.orgId ?? null },
        state: "queued",
        sent: 0,
      });
    }
    if (picked.some((p) => p.path.includes("/"))) toast.info("Folders upload to connected drives one file at a time: choose the files instead.");
    if (fresh.length) setTasks((list) => [...list, ...fresh]);
  }, []);

  const addConnectedVersion = useCallback((item: FilesConnectedItem, file: File, orgId?: string | null) => {
    setTasks((list) => [
      ...list,
      {
        id: nextId(),
        batch: nextId(),
        file,
        name: item.name,
        folders: [],
        orgId: orgId ?? "",
        parentId: null,
        connected: { connectionId: item.connection_id, itemRef: item.ref, orgId: orgId ?? null },
        state: "queued",
        sent: 0,
      },
    ]);
  }, []);

  const value = useMemo<UploadsContext>(
    () => ({ tasks, add, addVersion, addConnected, addConnectedVersion, maxBytes, setMaxBytes }),
    [tasks, add, addVersion, addConnected, addConnectedVersion, maxBytes],
  );
  const cancel = (id: string) => {
    const c = controllers.current.get(id);
    if (c) c.abort();
    else patch(id, { state: "canceled" });
  };
  const retry = (id: string) => patch(id, { state: "queued", sent: 0, error: undefined });
  const clear = () => setTasks((list) => list.filter((t) => t.state === "queued" || t.state === "uploading"));
  const cancelAll = () => {
    for (const t of tasksRef.current) if (t.state === "queued" || t.state === "uploading") cancel(t.id);
  };

  return (
    <Ctx.Provider value={value}>
      {children}
      <UploadTray tasks={tasks} onCancel={cancel} onRetry={retry} onClear={clear} onCancelAll={cancelAll} />
    </Ctx.Provider>
  );
}

/** The upload queue (inside the files layout). */
export function useFilesUploads(): UploadsContext {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useFilesUploads must be used inside FilesUploadsProvider");
  return ctx;
}

// ---- picking and dropping ------------------------------------------------------------

type Entry = { isFile: boolean; isDirectory: boolean; name: string; fullPath: string };
type FileEntry = Entry & { file: (ok: (f: File) => void, fail: (e: unknown) => void) => void };
type DirEntry = Entry & { createReader: () => { readEntries: (ok: (e: Entry[]) => void, fail: (e: unknown) => void) => void } };

async function walkEntry(entry: Entry, out: FilesPicked[]): Promise<void> {
  if (entry.isFile) {
    const file = await new Promise<File>((ok, fail) => (entry as FileEntry).file(ok, fail));
    out.push({ file, path: entry.fullPath.replace(/^\//, "") });
    return;
  }
  if (!entry.isDirectory) return;
  const reader = (entry as DirEntry).createReader();
  for (;;) {
    const batch = await new Promise<Entry[]>((ok, fail) => reader.readEntries(ok, fail));
    if (batch.length === 0) break;
    for (const e of batch) await walkEntry(e, out);
  }
}

/** Files from a drop, keeping folder structure (Chrome, Edge, Safari, Firefox). */
export async function filesFromDrop(dt: DataTransfer): Promise<FilesPicked[]> {
  const items = Array.from(dt.items ?? []);
  const entries = items
    .filter((i) => i.kind === "file")
    .map((i) => (i as DataTransferItem & { webkitGetAsEntry?: () => Entry | null }).webkitGetAsEntry?.() ?? null);
  if (entries.length > 0 && entries.every((e) => e !== null)) {
    const out: FilesPicked[] = [];
    for (const e of entries) if (e) await walkEntry(e, out);
    // Loose files keep no path; files inside a dropped folder keep "Folder/…".
    return out.map((p) => (p.path.includes("/") ? p : { file: p.file, path: "" }));
  }
  return Array.from(dt.files).map((file) => ({ file, path: "" }));
}

/** Files from an <input type=file> (webkitRelativePath is set for folder picks). */
export function filesFromInput(list: FileList | null): FilesPicked[] {
  return Array.from(list ?? []).map((file) => ({ file, path: (file as File & { webkitRelativePath?: string }).webkitRelativePath ?? "" }));
}

/** Whether a drag carries files from the computer (not items dragged inside the drive). */
export function dragHasFiles(e: { dataTransfer: DataTransfer | null }): boolean {
  return Array.from(e.dataTransfer?.types ?? []).includes("Files");
}

// ---- tray -----------------------------------------------------------------------------

function UploadTray({ tasks, onCancel, onRetry, onClear, onCancelAll }: { tasks: Task[]; onCancel: (id: string) => void; onRetry: (id: string) => void; onClear: () => void; onCancelAll: () => void }) {
  const [collapsed, setCollapsed] = useState(false);
  const visible = tasks.filter((t) => t.state !== "canceled");
  if (visible.length === 0) return null;
  const active = visible.filter((t) => t.state === "queued" || t.state === "uploading");
  const failed = visible.filter((t) => t.state === "failed");
  const total = active.reduce((n, t) => n + t.file.size, 0);
  const sent = active.reduce((n, t) => n + t.sent, 0);
  const title =
    active.length > 0
      ? `Uploading ${active.length} ${active.length === 1 ? "file" : "files"}`
      : failed.length > 0
        ? `${failed.length} ${failed.length === 1 ? "upload" : "uploads"} failed`
        : `${visible.length} ${visible.length === 1 ? "upload" : "uploads"} done`;
  return (
    <section
      aria-label="Uploads"
      className="fixed inset-x-4 bottom-24 z-(--app-z-dropdown) flex max-h-96 flex-col overflow-hidden rounded-lg border border-border bg-surface-raised shadow-lg lg:inset-x-auto lg:right-6 lg:bottom-6 lg:w-96"
    >
      <header className="flex min-h-row items-center gap-2 border-b border-border px-4">
        <h2 className="min-w-0 flex-1 truncate type-label font-semibold text-fg" aria-live="polite">
          {title}
          {active.length > 0 && total > 0 ? <span className="ml-2 font-normal text-fg-muted tabular-nums">{Math.round((sent / total) * 100)}%</span> : null}
        </h2>
        {active.length > 1 ? (
          <Button variant="ghost" size="sm" onClick={onCancelAll}>
            Cancel all
          </Button>
        ) : null}
        <IconButton label={collapsed ? "Show uploads" : "Hide uploads"} icon={collapsed ? <ChevronUp /> : <ChevronDown />} variant="ghost" size="sm" onClick={() => setCollapsed((c) => !c)} />
        {active.length === 0 ? <IconButton label="Close uploads" icon={<X />} variant="ghost" size="sm" onClick={onClear} /> : null}
      </header>
      {collapsed ? null : (
        <ul className="flex flex-col overflow-y-auto py-1">
          {visible.map((t) => (
            <li key={t.id} className="flex min-h-row items-center gap-3 px-4 py-2">
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="truncate type-label text-fg" title={[...t.folders, t.name].join("/")}>
                  {t.itemId ? `New version of ${t.name}` : t.name}
                </span>
                {t.state === "uploading" || t.state === "queued" ? (
                  <div className="h-1 overflow-hidden rounded-pill bg-sunken" role="progressbar" aria-label={`Uploading ${t.name}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round((t.sent / Math.max(t.file.size, 1)) * 100)}>
                    <div className="h-full rounded-pill bg-primary transition-[width] duration-150 motion-reduce:transition-none" style={{ width: `${Math.round((t.sent / Math.max(t.file.size, 1)) * 100)}%` }} />
                  </div>
                ) : t.state === "failed" ? (
                  <span className="type-caption text-danger-strong">{t.error}</span>
                ) : (
                  <span className="type-caption text-fg-muted">
                    {filesFormatSize(t.file.size)}
                    {t.result && t.result.name !== t.name ? ` · saved as ${t.result.name}` : ""}
                  </span>
                )}
              </div>
              {t.state === "done" ? <CircleCheck className="size-5 shrink-0 text-success-strong" aria-label="Uploaded" /> : null}
              {t.state === "failed" ? (
                <>
                  <CircleAlert className="size-5 shrink-0 text-danger-strong" aria-hidden />
                  <IconButton label={`Retry ${t.name}`} icon={<RotateCcw />} variant="ghost" size="sm" onClick={() => onRetry(t.id)} />
                </>
              ) : null}
              {t.state === "queued" || t.state === "uploading" ? (
                <IconButton label={`Cancel ${t.name}`} icon={<X />} variant="ghost" size="sm" onClick={() => onCancel(t.id)} className={cn(t.state === "queued" && "opacity-80")} />
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
