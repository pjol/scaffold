"use client";

import {
  errorMessage,
  filesCanEdit,
  filesCanManage,
  filesFormatSize,
  orgsNoun,
  useAccessDirectory,
  useFilesAddComment,
  useFilesCloseRequest,
  useFilesComments,
  useFilesCreateRequest,
  useFilesDeleteComment,
  useFilesFolderRequests,
  useFilesLock,
  useFilesPatchComment,
  useFilesPublicLinks,
  useFilesRequestUploads,
  useFilesSetColor,
  useFilesSetLinkOptions,
  useFilesStorage,
  useFilesTransferOwner,
  FILES_FOLDER_COLORS,
  FILES_SEARCH_KINDS,
  type FilesComment,
  type FilesItem,
  type FilesPublicLinkInfo,
  type FilesRequest,
  type FilesSearchFilters,
  type FilesSearchKind,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Badge,
  Banner,
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  ListRow,
  SegmentedControl,
  Select,
  Sheet,
  SkeletonGroup,
  SkeletonRow,
  Switch,
  TextArea,
  ToggleChip,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import { AtSign, Check, Copy, FileLock2, Inbox, Link2, Lock, LockOpen, MessageSquare, Palette, Reply, Star, Trash, UserRoundCog, Users } from "lucide-react";
import Link from "next/link";
import { createContext, useContext, useMemo, useState, type ReactNode } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import type { ResourceMenuAction } from "@/features/registry";
import { formatDate, formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

// Drive depth on the web: comments, check out / in, folder colours, file
// requests, owner hand-over, public link options, storage per person, search
// filters and video posters.

const personName = (p: { name: string } | null | undefined) => (p ? p.name || "Deleted user" : "Deleted user");

// ---- dialogs context ------------------------------------------------------------------

interface FilesExtras {
  requestFiles: (folder: FilesItem) => void;
  showRequests: (folder: FilesItem) => void;
  transferOwner: (item: FilesItem) => void;
  checkOut: (item: FilesItem) => void;
}

const ExtrasCtx = createContext<FilesExtras | null>(null);

export function useFilesExtras(): FilesExtras {
  const ctx = useContext(ExtrasCtx);
  if (!ctx) throw new Error("useFilesExtras must be used inside FilesExtrasProvider");
  return ctx;
}

/** The depth dialogs, once per drive (inside FilesActionsProvider). */
export function FilesExtrasProvider({ children }: { children: ReactNode }) {
  const [requesting, setRequesting] = useState<FilesItem | null>(null);
  const [listing, setListing] = useState<FilesItem | null>(null);
  const [transferring, setTransferring] = useState<FilesItem | null>(null);
  const [checking, setChecking] = useState<FilesItem | null>(null);
  const value = useMemo<FilesExtras>(() => ({ requestFiles: setRequesting, showRequests: setListing, transferOwner: setTransferring, checkOut: setChecking }), []);
  return (
    <ExtrasCtx.Provider value={value}>
      {children}
      <RequestFilesDialog folder={requesting} onClose={() => setRequesting(null)} />
      <FolderRequestsSheet folder={listing} onClose={() => setListing(null)} onNew={(f) => setRequesting(f)} />
      <TransferOwnerDialog item={transferring} onClose={() => setTransferring(null)} />
      <CheckOutDialog item={checking} onClose={() => setChecking(null)} />
    </ExtrasCtx.Provider>
  );
}

/** ⋮ rows the depth features add to an item. */
export function useFilesExtraRows(item: FilesItem | null | undefined, myId: string | null | undefined): ResourceMenuAction[] {
  const x = useFilesExtras();
  const lock = useFilesLock();
  const color = useFilesSetColor();
  if (!item || item.trashed_at) return [];
  const rows: ResourceMenuAction[] = [];
  const edit = filesCanEdit(item);
  if (item.type === "file" && edit) {
    if (!item.lock) rows.push({ id: "check-out", label: "Check out", icon: Lock, onSelect: () => x.checkOut(item) });
    else if (item.lock.by?.id === myId)
      rows.push({
        id: "check-in",
        label: "Check in",
        icon: LockOpen,
        onSelect: () => lock.mutate({ id: item.id, locked: false }, { onSuccess: () => toast.success("Checked in"), onError: (e) => toast.error(errorMessage(e, "Couldn't check it in.")) }),
      });
  }
  if (item.type === "file" && item.lock && item.lock.by?.id !== myId && filesCanManage(item)) {
    rows.push({
      id: "break-lock",
      label: "Break the lock",
      icon: LockOpen,
      destructive: true,
      onSelect: () => lock.mutate({ id: item.id, locked: false }, { onSuccess: () => toast.success("Lock broken"), onError: (e) => toast.error(errorMessage(e, "Couldn't break the lock.")) }),
    });
  }
  if (item.type === "folder" && edit) {
    rows.push({
      id: "color",
      label: "Folder colour",
      icon: Palette,
      hint: FILES_FOLDER_COLORS.find((c) => c.id === (item.color ?? ""))?.label,
      choices: FILES_FOLDER_COLORS.map((c) => ({
        id: c.id || "default",
        label: c.label,
        checked: (item.color ?? "") === c.id,
        onSelect: () => color.mutate({ id: item.id, color: c.id }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the colour.")) }),
      })),
    });
    rows.push({ id: "request-files", label: "Request files…", icon: Inbox, onSelect: () => x.requestFiles(item) });
    rows.push({ id: "file-requests", label: "File requests", icon: Link2, onSelect: () => x.showRequests(item) });
  }
  if (filesCanManage(item) && item.drive === "my" && item.owner_id === myId && item.parent_id === null && (item.type === "file" || item.type === "folder")) {
    rows.push({ id: "owner", label: "Transfer ownership…", icon: UserRoundCog, onSelect: () => x.transferOwner(item) });
  }
  return rows;
}

// ---- check out ---------------------------------------------------------------------------

function CheckOutDialog({ item, onClose }: { item: FilesItem | null; onClose: () => void }) {
  const lock = useFilesLock();
  const [note, setNote] = useState("");
  const submit = () => {
    if (!item) return;
    lock.mutate(
      { id: item.id, locked: true, note: note.trim() },
      {
        onSuccess: () => {
          toast.success("Checked out. Others can't change it until you check it in.");
          setNote("");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't check it out.")),
      },
    );
  };
  return (
    <Dialog
      open={item !== null}
      onOpenChange={(o) => !o && onClose()}
      title={`Check out “${item?.name ?? ""}”`}
      info="While it's checked out, only you can rename, move, edit or upload new versions. People who manage it can break the lock."
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit} loading={lock.isPending} leftIcon={<Lock />}>
            Check out
          </Button>
        </>
      }
    >
      <Field label="Note">
        <Input value={note} maxLength={200} placeholder="Updating the figures for Q3" onChange={(e) => setNote(e.target.value)} onKeyDown={(e) => e.key === "Enter" && submit()} />
      </Field>
    </Dialog>
  );
}

/** The lock banner on a file's page. */
export function LockBanner({ item, myId }: { item: FilesItem; myId: string | null | undefined }) {
  const lock = useFilesLock();
  if (!item.lock) return null;
  const mine = item.lock.by?.id === myId;
  const who = mine ? "You" : personName(item.lock.by);
  return (
    <Banner
      tone={mine ? "info" : "warning"}
      icon={<FileLock2 aria-hidden />}
      action={
        mine || filesCanManage(item) ? (
          <Button variant="ghost" size="sm" loading={lock.isPending} onClick={() => lock.mutate({ id: item.id, locked: false }, { onError: (e) => toast.error(errorMessage(e, item.lock?.by?.id === myId ? "Couldn't check it in." : "Couldn't break the lock.")) })}>
            {mine ? "Check in" : "Break the lock"}
          </Button>
        ) : undefined
      }
    >
      {who} checked this out {formatRelative(item.lock.at)}
      {item.lock.note ? `: “${item.lock.note}”` : "."}
    </Banner>
  );
}

// ---- owner hand-over ---------------------------------------------------------------------

function TransferOwnerDialog({ item, onClose }: { item: FilesItem | null; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [pick, setPick] = useState<{ id: string; name: string } | null>(null);
  const dir = useAccessDirectory(item?.org_id, q, { types: "user", enabled: item !== null });
  const transfer = useFilesTransferOwner();
  const close = () => {
    setQ("");
    setPick(null);
    onClose();
  };
  return (
    <Dialog
      open={item !== null}
      onOpenChange={(o) => !o && close()}
      title={`Transfer “${item?.name ?? ""}”`}
      info={`It moves to the top of their My drive with everything inside it. You keep edit access.`}
      footer={
        <>
          <Button variant="ghost" onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={!pick}
            loading={transfer.isPending}
            onClick={() =>
              item &&
              pick &&
              transfer.mutate(
                { id: item.id, userId: pick.id },
                {
                  onSuccess: () => {
                    toast.success(`${pick.name} owns it now`);
                    close();
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't transfer it.")),
                },
              )
            }
          >
            Transfer
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field label="New owner">
          <Input value={q} placeholder={`Search people in this ${orgsNoun()}`} onChange={(e) => setQ(e.target.value)} />
        </Field>
        <ul className="flex flex-col" aria-label="People">
          {(dir.data ?? [])
            .filter((d) => d.type === "user")
            .map((d) => (
              <li key={d.id}>
                <ListRow
                  title={d.name}
                  leading={<Avatar name={d.name} src={d.avatar_url} size="sm" />}
                  trailing={pick?.id === d.id ? <Check className="size-5 text-primary-strong" aria-label="Chosen" /> : undefined}
                  onClick={() => setPick({ id: d.id, name: d.name })}
                />
              </li>
            ))}
        </ul>
      </div>
    </Dialog>
  );
}

// ---- file requests -----------------------------------------------------------------------

function RequestFilesDialog({ folder, onClose }: { folder: FilesItem | null; onClose: () => void }) {
  const create = useFilesCreateRequest(folder?.id ?? "");
  const [title, setTitle] = useState("");
  const [instructions, setInstructions] = useState("");
  const [until, setUntil] = useState("");
  const [maxFiles, setMaxFiles] = useState("");
  const [maxMB, setMaxMB] = useState("");
  const [requireEmail, setRequireEmail] = useState(true);
  const [made, setMade] = useState<FilesRequest | null>(null);
  const [error, setError] = useState<string | null>(null);
  const close = () => {
    setTitle("");
    setInstructions("");
    setUntil("");
    setMaxFiles("");
    setMaxMB("");
    setRequireEmail(true);
    setMade(null);
    setError(null);
    onClose();
  };
  const submit = () => {
    setError(null);
    create.mutate(
      {
        title: title.trim(),
        instructions: instructions.trim(),
        expires_at: until ? new Date(`${until}T23:59:59`).toISOString() : null,
        max_files: maxFiles ? Number(maxFiles) : null,
        max_file_mb: maxMB ? Number(maxMB) : null,
        require_email: requireEmail,
      },
      { onSuccess: setMade, onError: (e) => setError(errorMessage(e, "Couldn't make the request.")) },
    );
  };
  return (
    <Dialog
      open={folder !== null}
      onOpenChange={(o) => !o && close()}
      title={made ? "Send this link" : `Request files into “${folder?.name ?? ""}”`}
      info="People with the link can upload files here without an account. They can't see the folder or what's in it."
      footer={
        made ? (
          <Button onClick={close}>Done</Button>
        ) : (
          <>
            <Button variant="ghost" onClick={close}>
              Cancel
            </Button>
            <Button onClick={submit} loading={create.isPending} disabled={!title.trim()}>
              Create link
            </Button>
          </>
        )
      }
    >
      {made ? (
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <Input readOnly value={made.url ?? ""} aria-label="Request link" onFocus={(e) => e.currentTarget.select()} wrapperClassName="min-w-0 flex-1" />
            <IconButton
              label="Copy link"
              icon={<Copy />}
              variant="secondary"
              onClick={() => {
                void navigator.clipboard.writeText(made.url ?? "").then(() => toast.success("Link copied"));
              }}
            />
          </div>
          <p className="type-caption text-fg-muted">The link is shown once. You can close the request from the folder's ⋮ menu → File requests.</p>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {error ? <Banner tone="danger">{error}</Banner> : null}
          <Field label="Title" required>
            <Input value={title} maxLength={200} placeholder="Receipts for March" onChange={(e) => setTitle(e.target.value)} />
          </Field>
          <Field label="Instructions">
            <TextArea value={instructions} maxLength={2000} minRows={2} placeholder="PDF or photos, one per receipt." onChange={(e) => setInstructions(e.target.value)} />
          </Field>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
            <Field label="Open until">
              <Input type="date" value={until} min={new Date().toISOString().slice(0, 10)} onChange={(e) => setUntil(e.target.value)} />
            </Field>
            <Field label="Most files">
              <Input type="number" inputMode="numeric" min={1} max={10000} value={maxFiles} placeholder="No limit" onChange={(e) => setMaxFiles(e.target.value)} />
            </Field>
            <Field label="Largest file (MB)">
              <Input type="number" inputMode="numeric" min={1} value={maxMB} placeholder="Drive limit" onChange={(e) => setMaxMB(e.target.value)} />
            </Field>
          </div>
          <Switch checked={requireEmail} onCheckedChange={setRequireEmail} label="Ask for their email" />
        </div>
      )}
    </Dialog>
  );
}

function FolderRequestsSheet({ folder, onClose, onNew }: { folder: FilesItem | null; onClose: () => void; onNew: (f: FilesItem) => void }) {
  const list = useFilesFolderRequests(folder?.id, folder !== null);
  const close = useFilesCloseRequest();
  const [open, setOpen] = useState<string | null>(null);
  const rows = list.data?.items ?? [];
  return (
    <Sheet
      open={folder !== null}
      onOpenChange={(o) => !o && onClose()}
      title="File requests"
      footer={
        folder ? (
          <Button leftIcon={<Inbox />} onClick={() => onNew(folder)}>
            New request
          </Button>
        ) : undefined
      }
    >
      {list.isPending ? (
        <SkeletonGroup label="Loading requests" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : list.isError ? (
        <ErrorState compact title="Couldn't load requests" onRetry={() => void list.refetch()} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<Inbox />} title="No file requests for this folder" />
      ) : (
        <ul className="flex flex-col gap-3">
          {rows.map((r) => (
            <li key={r.id} className="flex flex-col gap-2 rounded-lg border border-border p-card">
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate type-body font-semibold text-fg">{r.title}</span>
                <Badge tone={r.open ? "success" : "neutral"}>{r.open ? "Open" : r.closed_at ? "Closed" : "Ended"}</Badge>
              </div>
              <p className="type-caption text-fg-muted">
                {[
                  `${r.files_received} ${r.files_received === 1 ? "file" : "files"}${r.max_files ? ` of ${r.max_files}` : ""}`,
                  r.expires_at ? `until ${formatDate(r.expires_at)}` : null,
                  `by ${personName(r.created_by)}`,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="secondary" onClick={() => setOpen(open === r.id ? null : r.id)}>
                  {open === r.id ? "Hide files" : "Who sent what"}
                </Button>
                {r.open ? (
                  <Button size="sm" variant="ghost" loading={close.isPending && close.variables === r.id} onClick={() => close.mutate(r.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't close it.")) })}>
                    Close request
                  </Button>
                ) : null}
              </div>
              {open === r.id ? <RequestUploads requestId={r.id} /> : null}
            </li>
          ))}
        </ul>
      )}
      <LoadMore hasNextPage={!!list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
    </Sheet>
  );
}

function RequestUploads({ requestId }: { requestId: string }) {
  const up = useFilesRequestUploads(requestId);
  if (up.isPending) return <SkeletonRow />;
  if (up.isError) return <ErrorState compact title="Couldn't load the files" onRetry={() => void up.refetch()} />;
  if (up.data.items.length === 0) return <p className="type-caption text-fg-muted">Nothing yet.</p>;
  return (
    <ul className="flex flex-col gap-2">
      {up.data.items.map((u) => (
        <li key={u.id} className="flex flex-col">
          {u.item_id ? (
            <Link href={`/files/${u.item_id}`} className="truncate type-label font-semibold text-fg hover:underline">
              {u.name}
            </Link>
          ) : (
            <span className="truncate type-label text-fg-muted">{u.name} (deleted)</span>
          )}
          <span className="truncate type-caption text-fg-muted">
            {u.uploader_name}
            {u.uploader_email ? ` <${u.uploader_email}>` : ""} · {filesFormatSize(u.size)} · {formatRelative(u.completed_at)}
          </span>
        </li>
      ))}
    </ul>
  );
}

// ---- comments ----------------------------------------------------------------------------

function anchorLabel(c: FilesComment): string | null {
  if (c.anchor?.page) return `Page ${c.anchor.page}`;
  if (c.anchor?.seconds !== undefined) {
    const s = Math.floor(c.anchor.seconds);
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
  }
  return null;
}

/** Comment threads on an item: write, reply, mention, resolve, edit and delete your own. */
export function FilesComments({ item }: { item: FilesItem }) {
  const [status, setStatus] = useState<"open" | "resolved">("open");
  const threads = useFilesComments(item.id, status);
  const canComment = item.my_level !== "view" && item.my_level !== "none" && !item.trashed_at;
  const rows = threads.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4">
      {canComment ? <Composer item={item} /> : <p className="type-caption text-fg-muted">You can read comments here, not add them.</p>}
      <SegmentedControl
        aria-label="Show"
        fullWidth={false}
        className="self-start"
        value={status}
        onValueChange={setStatus}
        options={[
          { value: "open", label: "Open" },
          { value: "resolved", label: "Resolved" },
        ]}
      />
      {threads.isPending ? (
        <SkeletonGroup label="Loading comments" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : threads.isError ? (
        <ErrorState compact title="Couldn't load comments" onRetry={() => void threads.refetch()} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<MessageSquare />} title={status === "open" ? "No open comments" : "No resolved comments"} />
      ) : (
        <ol className="flex flex-col gap-3">
          {rows.map((t) => (
            <Thread key={t.id} item={item} thread={t} canComment={canComment} />
          ))}
        </ol>
      )}
      <LoadMore hasNextPage={!!threads.hasNextPage} isFetchingNextPage={threads.isFetchingNextPage} fetchNextPage={threads.fetchNextPage} />
    </div>
  );
}

function Composer({ item, threadId, onDone }: { item: FilesItem; threadId?: string; onDone?: () => void }) {
  const add = useFilesAddComment(item.id);
  const [body, setBody] = useState("");
  const [mentions, setMentions] = useState<{ id: string; name: string }[]>([]);
  const [picking, setPicking] = useState(false);
  const [q, setQ] = useState("");
  const [page, setPage] = useState("");
  const pdf = item.preview === "pdf";
  const dir = useAccessDirectory(item.org_id, q, { types: "user", enabled: picking });
  const submit = () => {
    const text = body.trim();
    if (!text) return;
    add.mutate(
      { body: text, thread_id: threadId ?? null, mentions: mentions.map((m) => m.id), anchor: !threadId && pdf && page ? { page: Number(page) } : null },
      {
        onSuccess: (res) => {
          setBody("");
          setMentions([]);
          setPage("");
          if (res.not_notified.length > 0) toast.warning(`${res.not_notified.length === 1 ? "One person" : `${res.not_notified.length} people`} you mentioned can't open this, so they weren't told. Share it with them first.`);
          onDone?.();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't post the comment.")),
      },
    );
  };
  return (
    <div className="flex flex-col gap-2">
      <TextArea
        value={body}
        minRows={threadId ? 1 : 2}
        maxLength={10000}
        aria-label={threadId ? "Reply" : "Comment"}
        placeholder={threadId ? "Reply" : "Add a comment"}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit();
        }}
      />
      {mentions.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {mentions.map((m) => (
            <ToggleChip key={m.id} size="sm" pressed onPressedChange={() => setMentions((ms) => ms.filter((x) => x.id !== m.id))}>
              @{m.name}
            </ToggleChip>
          ))}
        </div>
      ) : null}
      {picking ? (
        <div className="flex flex-col gap-1 rounded-md border border-border p-2">
          <Input autoFocus value={q} placeholder="Mention someone" aria-label="Mention someone" onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === "Escape" && setPicking(false)} />
          {(dir.data ?? [])
            .filter((d) => d.type === "user" && !mentions.some((m) => m.id === d.id))
            .slice(0, 5)
            .map((d) => (
              <ListRow
                key={d.id}
                title={d.name}
                leading={<Avatar name={d.name} src={d.avatar_url} size="sm" />}
                onClick={() => {
                  setMentions((ms) => [...ms, { id: d.id, name: d.name }]);
                  setPicking(false);
                  setQ("");
                }}
              />
            ))}
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        <IconButton label="Mention someone" icon={<AtSign />} variant="ghost" size="sm" tooltip onClick={() => setPicking((p) => !p)} />
        {!threadId && pdf ? (
          <Input type="number" inputMode="numeric" min={1} value={page} placeholder="Page" aria-label="On page" wrapperClassName="w-24" onChange={(e) => setPage(e.target.value)} />
        ) : null}
        <span className="flex-1" />
        {onDone ? (
          <Button variant="ghost" size="sm" onClick={onDone}>
            Cancel
          </Button>
        ) : null}
        <Button size="sm" onClick={submit} loading={add.isPending} disabled={!body.trim()}>
          {threadId ? "Reply" : "Comment"}
        </Button>
      </div>
    </div>
  );
}

function Thread({ item, thread, canComment }: { item: FilesItem; thread: FilesComment; canComment: boolean }) {
  const patch = useFilesPatchComment(item.id);
  const [replying, setReplying] = useState(false);
  const canResolve = canComment && (thread.mine || filesCanEdit(item));
  const anchor = anchorLabel(thread);
  return (
    <li className={cn("flex flex-col gap-3 rounded-lg border border-border bg-surface p-card", thread.resolved_at && "opacity-80")} id={`comment-${thread.id}`}>
      <CommentBody item={item} c={thread} anchor={anchor} />
      {(thread.replies ?? []).map((r) => (
        <div key={r.id} className="border-l-2 border-border pl-3">
          <CommentBody item={item} c={r} anchor={null} />
        </div>
      ))}
      {replying ? <Composer item={item} threadId={thread.id} onDone={() => setReplying(false)} /> : null}
      {!replying && canComment ? (
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="ghost" leftIcon={<Reply />} onClick={() => setReplying(true)}>
            Reply
          </Button>
          {canResolve ? (
            <Button
              size="sm"
              variant="ghost"
              leftIcon={<Check />}
              loading={patch.isPending}
              onClick={() => patch.mutate({ commentId: thread.id, resolved: !thread.resolved_at }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
            >
              {thread.resolved_at ? "Reopen" : "Resolve"}
            </Button>
          ) : null}
        </div>
      ) : null}
      {thread.resolved_at ? <p className="type-caption text-fg-muted">Resolved by {personName(thread.resolved_by)} {formatRelative(thread.resolved_at)}</p> : null}
    </li>
  );
}

function CommentBody({ item, c, anchor }: { item: FilesItem; c: FilesComment; anchor: string | null }) {
  const patch = useFilesPatchComment(item.id);
  const del = useFilesDeleteComment(item.id);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(c.body);
  const [confirm, setConfirm] = useState(false);
  const canDelete = c.mine || filesCanManage(item);
  return (
    <div className="flex gap-3">
      <Avatar name={personName(c.author)} src={c.author?.avatar_url} size="sm" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-baseline gap-x-2">
          <span className="type-label font-semibold text-fg">{personName(c.author)}</span>
          <time className="type-caption text-fg-muted" dateTime={c.created_at} title={formatDateTime(c.created_at)}>
            {formatRelative(c.created_at)}
            {c.edited_at ? " · edited" : ""}
          </time>
          {anchor ? <Badge>{anchor}</Badge> : null}
        </div>
        {editing ? (
          <div className="flex flex-col gap-2">
            <TextArea value={draft} minRows={1} maxLength={10000} aria-label="Edit comment" onChange={(e) => setDraft(e.target.value)} />
            <div className="flex gap-2">
              <Button
                size="sm"
                loading={patch.isPending}
                onClick={() => patch.mutate({ commentId: c.id, body: draft }, { onSuccess: () => setEditing(false), onError: (e) => toast.error(errorMessage(e, "Couldn't save.")) })}
              >
                Save
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <p className="type-body break-words whitespace-pre-wrap text-fg">{c.body}</p>
        )}
        {!editing && (c.mine || canDelete) ? (
          <div className="flex gap-1">
            {c.mine ? (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setDraft(c.body);
                  setEditing(true);
                }}
              >
                Edit
              </Button>
            ) : null}
            {canDelete ? (
              <Button size="sm" variant="ghost" onClick={() => setConfirm(true)}>
                Delete
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={c.thread_id ? "Delete this reply?" : "Delete this thread?"}
        confirmLabel="Delete"
        tone="danger"
        onConfirm={async () => {
          await del.mutateAsync(c.id);
          setConfirm(false);
        }}
      >
        <p className="type-body text-fg-muted">{c.thread_id ? "The reply is removed for everyone." : "The comment and its replies are removed for everyone."}</p>
      </ConfirmDialog>
    </div>
  );
}

// ---- public link options -----------------------------------------------------------------

/** Password, downloads and opens for the item's public links (managers). */
export function FilesLinkOptions({ item }: { item: FilesItem }) {
  const links = useFilesPublicLinks(item.id, filesCanManage(item) && item.public_link);
  if (!filesCanManage(item) || !item.public_link) return null;
  if (links.isPending) return <SkeletonRow />;
  if (links.isError) return <ErrorState compact title="Couldn't load the public links" onRetry={() => void links.refetch()} />;
  if (links.data.items.length === 0) return null;
  return (
    <section className="flex flex-col gap-3">
      <h3 className="type-title-sm text-fg">Public link</h3>
      {links.data.items.map((l) => (
        <LinkOptionsRow key={l.id} item={item} link={l} />
      ))}
    </section>
  );
}

function LinkOptionsRow({ item, link }: { item: FilesItem; link: FilesPublicLinkInfo }) {
  const set = useFilesSetLinkOptions(item.id);
  const [pw, setPw] = useState("");
  const [editing, setEditing] = useState(false);
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border p-card">
      <p className="type-caption text-fg-muted">
        Opened {link.opens} {link.opens === 1 ? "time" : "times"}
        {link.last_opened_at ? `, last ${formatRelative(link.last_opened_at)}` : ""}
        {link.expires_at ? ` · ends ${formatDate(link.expires_at)}` : ""}
      </p>
      <Switch
        checked={link.allow_download}
        onCheckedChange={(v) => set.mutate({ linkId: link.id, allow_download: v }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
        label="Allow downloads"
        info="Off hides Download on the public page and refuses download links. People can still see the preview, and nothing stops a screenshot."
      />
      {editing ? (
        <div className="flex flex-col gap-2">
          <Field label="Password">
            <Input type="password" autoComplete="new-password" value={pw} minLength={4} maxLength={128} onChange={(e) => setPw(e.target.value)} />
          </Field>
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={pw.length < 4}
              loading={set.isPending}
              onClick={() =>
                set.mutate(
                  { linkId: link.id, password: pw },
                  {
                    onSuccess: () => {
                      toast.success("Password set. People who opened it before need it now.");
                      setPw("");
                      setEditing(false);
                    },
                    onError: (e) => toast.error(errorMessage(e, "Couldn't set the password.")),
                  },
                )
              }
            >
              Save password
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
              Cancel
            </Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <span className="flex-1 type-body text-fg">{link.has_password ? "Password protected" : "No password"}</span>
          <Button size="sm" variant="secondary" onClick={() => setEditing(true)}>
            {link.has_password ? "Change password" : "Add a password"}
          </Button>
          {link.has_password ? (
            <Button size="sm" variant="ghost" onClick={() => set.mutate({ linkId: link.id, password: "" }, { onSuccess: () => toast.success("Password removed") })}>
              Remove
            </Button>
          ) : null}
        </div>
      )}
    </div>
  );
}

// ---- storage per person --------------------------------------------------------------------

/** "Storage" dialog: your usage, and everyone's for owners and admins. */
export function FilesStorageDialog({ orgId, open, onOpenChange }: { orgId: string | null; open: boolean; onOpenChange: (o: boolean) => void }) {
  const st = useFilesStorage(orgId, open);
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Storage" info="Every version and the trash count. Each file counts toward the person who uploaded it.">
      {st.isPending ? (
        <SkeletonGroup label="Loading storage" className="flex flex-col">
          <SkeletonRow />
          <SkeletonRow />
        </SkeletonGroup>
      ) : st.isError ? (
        <ErrorState compact title="Couldn't load storage" onRetry={() => void st.refetch()} />
      ) : (
        <div className="flex flex-col gap-4">
          <p className="type-body text-fg">
            {filesFormatSize(st.data.used_bytes)}
            {st.data.quota_bytes > 0 ? ` of ${filesFormatSize(st.data.quota_bytes)}` : ""} used · you: {filesFormatSize(st.data.mine.bytes)} in {st.data.mine.files}{" "}
            {st.data.mine.files === 1 ? "file" : "files"}
          </p>
          {st.data.people ? (
            <ol className="flex flex-col">
              {st.data.people.map((p, i) => (
                <li key={p.person?.id ?? `gone-${i}`} className="flex min-h-tap items-center gap-3 border-b border-border py-2 last:border-b-0">
                  <Avatar name={personName(p.person)} src={p.person?.avatar_url} size="sm" />
                  <span className="min-w-0 flex-1 truncate type-body text-fg">{personName(p.person)}</span>
                  <span className="type-label text-fg-muted tabular-nums">{filesFormatSize(p.bytes)}</span>
                </li>
              ))}
            </ol>
          ) : null}
        </div>
      )}
    </Dialog>
  );
}

// ---- search filters ----------------------------------------------------------------------

const MODIFIED = [
  { value: "any", label: "Any time" },
  { value: "1", label: "Today" },
  { value: "7", label: "Last 7 days" },
  { value: "30", label: "Last 30 days" },
  { value: "365", label: "Last year" },
];

/** Search filters kept in the URL (kind, owner, modified, location, starred, shared with me). */
interface FilterParams {
  kind: string;
  owner: string;
  modified: string;
  in: string;
  starred: string;
  shared: string;
}

export function useFilesSearchFilters(): { filters: FilesSearchFilters; active: boolean; set: (k: keyof FilterParams, v: string | null) => void; clear: () => void; raw: FilterParams } {
  const [kind, setKind] = useUrlState("kind", "");
  const [owner, setOwner] = useUrlState("owner", "");
  const [modified, setModified] = useUrlState("modified", "");
  const [location, setLocation] = useUrlState("in", "");
  const [starred, setStarred] = useUrlState("starred", "");
  const [shared, setShared] = useUrlState("shared", "");
  const filters = useMemo<FilesSearchFilters>(() => {
    const days = Number(modified);
    const after = days > 0 ? new Date(Date.now() - days * 86_400_000).toISOString().slice(0, 10) : undefined;
    return {
      kind: (kind || undefined) as FilesSearchKind | undefined,
      owner: owner || undefined,
      modified_after: after,
      location: location || undefined,
      starred: starred === "1" || undefined,
      shared_with_me: shared === "1" || undefined,
    };
  }, [kind, owner, modified, location, starred, shared]);
  const setters: Record<keyof FilterParams, (v: string | null) => void> = { kind: setKind, owner: setOwner, modified: setModified, in: setLocation, starred: setStarred, shared: setShared };
  return {
    filters,
    active: !!(kind || owner || modified || location || starred || shared),
    set: (k, v) => setters[k](v),
    clear: () => Object.values(setters).forEach((s) => s(null)),
    raw: { kind, owner, modified, in: location, starred, shared },
  };
}

/** The filter row under the search box. */
export function FilesSearchFiltersBar({ state, folderId }: { state: ReturnType<typeof useFilesSearchFilters>; folderId?: string | null }) {
  const { raw, set, active, clear } = state;
  return (
    <div className="flex flex-wrap items-center gap-2" role="group" aria-label="Search filters">
      <Select
        aria-label="Type"
        className="w-auto min-w-36"
        value={raw.kind || "any"}
        onValueChange={(v) => set("kind", v === "any" ? null : v)}
        options={[{ value: "any", label: "Any type" }, ...FILES_SEARCH_KINDS.map((k) => ({ value: k.id, label: k.label }))]}
      />
      <Select
        aria-label="Owner"
        className="w-auto min-w-36"
        value={raw.owner || "any"}
        onValueChange={(v) => set("owner", v === "any" ? null : v)}
        options={[
          { value: "any", label: "Anyone" },
          { value: "me", label: "Owned by me" },
          { value: "others", label: "Not owned by me" },
        ]}
      />
      <Select aria-label="Modified" className="w-auto min-w-36" value={raw.modified || "any"} onValueChange={(v) => set("modified", v === "any" ? null : v)} options={MODIFIED} />
      <Select
        aria-label="Location"
        className="w-auto min-w-36"
        value={raw.in || "any"}
        onValueChange={(v) => set("in", v === "any" ? null : v)}
        options={[
          { value: "any", label: "Anywhere" },
          { value: "my", label: "My drive" },
          { value: "shared", label: "Shared folders" },
          ...(folderId ? [{ value: folderId, label: "In this folder" }] : []),
        ]}
      />
      <ToggleChip icon={<Star />} pressed={raw.starred === "1"} onPressedChange={(p) => set("starred", p ? "1" : null)}>
        Starred
      </ToggleChip>
      <ToggleChip icon={<Users />} pressed={raw.shared === "1"} onPressedChange={(p) => set("shared", p ? "1" : null)}>
        Shared with me
      </ToggleChip>
      {active ? (
        <Button variant="ghost" size="sm" leftIcon={<Trash />} onClick={clear}>
          Clear filters
        </Button>
      ) : null}
    </div>
  );
}

// ---- video posters -----------------------------------------------------------------------

/** Draws a frame about a second into a video file (scaled to 640 px) as a JPEG data URL, or null. */
export function filesCapturePoster(file: File): Promise<string | null> {
  if (!file.type.startsWith("video/") || typeof document === "undefined") return Promise.resolve(null);
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file);
    const video = document.createElement("video");
    let done = false;
    const finish = (v: string | null) => {
      if (done) return;
      done = true;
      URL.revokeObjectURL(url);
      video.removeAttribute("src");
      resolve(v);
    };
    const timer = setTimeout(() => finish(null), 8000);
    video.muted = true;
    video.playsInline = true;
    video.preload = "metadata";
    video.onloadedmetadata = () => {
      video.currentTime = Math.min(1, (video.duration || 2) / 2);
    };
    video.onseeked = () => {
      clearTimeout(timer);
      const w = video.videoWidth;
      const h = video.videoHeight;
      if (!w || !h) return finish(null);
      const scale = Math.min(1, 640 / Math.max(w, h));
      const canvas = document.createElement("canvas");
      canvas.width = Math.round(w * scale);
      canvas.height = Math.round(h * scale);
      const ctx = canvas.getContext("2d");
      if (!ctx) return finish(null);
      ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
      try {
        finish(canvas.toDataURL("image/jpeg", 0.8));
      } catch {
        finish(null);
      }
    };
    video.onerror = () => finish(null);
    video.src = url;
  });
}
