"use client";

import {
  docsTimeAgo,
  docsTitle,
  errorMessage,
  orgsNoun,
  useAccessCreateLink,
  useAccessPermission,
  useAccessResource,
  useAccessRevokeLink,
  useDocsDeletePermanently,
  useDocsHistory,
  useDocsImport,
  useDocsRestorePage,
  useDocsRestoreSnapshot,
  useDocsSearch,
  useDocsSnapshot,
  useDocsTrash,
  useDocsUpdatePage,
  useDocsUploadFile,
  type DocsCoverColor,
  type DocsPage,
} from "@scaffoldapp/shared";
import { Avatar, Button, ConfirmDialog, Dialog, EmptyState, ErrorState, Input, Skeleton, SkeletonRow, Slider, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Copy, FileText, Globe, History, Lock, RotateCcw, Search, Trash2, Upload } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { BlockView } from "./BlockView";
import { floatingItemClass } from "./Floating";

/** Pick a page you can edit (moving a page or blocks). `allowTop` adds "Top level". */
export function PagePicker({
  open,
  onOpenChange,
  orgId,
  title,
  exclude,
  allowTop,
  onPick,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  orgId: string;
  title: string;
  exclude?: string[];
  allowTop?: boolean;
  onPick: (pageId: string | null) => void;
}) {
  const [q, setQ] = useState("");
  const results = useDocsSearch(orgId, q, { kind: "page", min_level: "edit", limit: 20, enabled: open });
  const list = (results.data ?? []).filter((p) => !exclude?.includes(p.id));
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={title}>
      <div className="flex flex-col gap-3">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search pages" aria-label="Search pages" autoFocus leadingIcon={<Search />} />
        <div role="listbox" aria-label="Pages" className="flex max-h-80 flex-col overflow-y-auto">
          {allowTop ? (
            <button type="button" role="option" aria-selected={false} className={floatingItemClass} onClick={() => onPick(null)}>
              <Lock aria-hidden />
              <span className="flex-1 truncate">Top level, only you and people it's shared with</span>
            </button>
          ) : null}
          {results.isPending ? <SkeletonRow /> : null}
          {list.map((p) => (
            <button key={p.id} type="button" role="option" aria-selected={false} className={floatingItemClass} onClick={() => onPick(p.id)}>
              <span aria-hidden className="flex size-5 items-center justify-center">
                {p.icon ?? <FileText className="size-4 text-fg-muted" />}
              </span>
              <span className="flex-1 truncate">{docsTitle(p.title)}</span>
            </button>
          ))}
          {!results.isPending && list.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No pages you can edit match.</p> : null}
        </div>
      </div>
    </Dialog>
  );
}

/** Saved versions: preview one, restore it (the current page is saved first). */
export function HistoryDialog({ open, onOpenChange, page, canRestore }: { open: boolean; onOpenChange: (open: boolean) => void; page: DocsPage; canRestore: boolean }) {
  const history = useDocsHistory(page.id, open);
  const [selected, setSelected] = useState<string | null>(null);
  const current = selected ?? history.data?.items[0]?.id ?? null;
  const snap = useDocsSnapshot(open ? current : null);
  const restore = useDocsRestoreSnapshot(page.id);
  const [confirm, setConfirm] = useState(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Page history" size="lg">
      <div className="flex min-h-80 flex-col gap-4 md:flex-row">
        <div className="flex max-h-48 flex-col gap-1 overflow-y-auto md:max-h-[28rem] md:w-56 md:shrink-0">
          {history.isPending ? (
            <>
              <SkeletonRow />
              <SkeletonRow />
            </>
          ) : history.isError ? (
            <ErrorState title="Couldn't load the history." onRetry={() => void history.refetch()} />
          ) : history.data.items.length === 0 ? (
            <EmptyState icon={<History />} title="No saved versions yet" />
          ) : (
            <>
              {history.data.items.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  aria-pressed={s.id === current}
                  onClick={() => setSelected(s.id)}
                  className={cn("flex flex-col items-start gap-1 rounded-sm px-3 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring", s.id === current && "bg-sunken")}
                >
                  <span className="type-label text-fg">{new Date(s.created_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}</span>
                  <span className="flex items-center gap-1">
                    {s.editors.slice(0, 3).map((e) => (
                      <Avatar key={e.id} size="xs" name={e.name} {...(e.avatar_url ? { src: e.avatar_url } : {})} />
                    ))}
                    <span className="type-caption text-fg-muted">{s.reason === "before_restore" ? "Before a restore" : docsTimeAgo(s.created_at)}</span>
                  </span>
                </button>
              ))}
              <LoadMore hasNextPage={!!history.hasNextPage} isFetchingNextPage={history.isFetchingNextPage} fetchNextPage={history.fetchNextPage} />
            </>
          )}
        </div>
        <div className="min-w-0 flex-1 overflow-y-auto rounded-md border border-border p-4 md:max-h-[28rem]">
          {snap.isPending && current ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-8 w-1/2" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4" />
            </div>
          ) : snap.data ? (
            <div className="flex flex-col gap-3">
              <h3 className="type-title text-fg">
                {snap.data.snapshot.icon ? `${snap.data.snapshot.icon} ` : ""}
                {snap.data.snapshot.title}
              </h3>
              <BlockView blocks={snap.data.snapshot.blocks} refs={snap.data.refs} />
            </div>
          ) : null}
        </div>
      </div>
      {canRestore && snap.data ? (
        <div className="flex justify-end pt-4">
          <Button leftIcon={<RotateCcw />} onClick={() => setConfirm(true)}>
            Restore this version
          </Button>
        </div>
      ) : null}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Restore this version?"
        confirmLabel="Restore"
        onConfirm={() =>
          restore.mutateAsync(current!).then(() => {
            toast.success("Version restored.");
            onOpenChange(false);
          })
        }
        errorText={(e) => errorMessage(e, "Couldn't restore that version.")}
      >
        The page goes back to how it was. Its current content is saved in the history first.
      </ConfirmDialog>
    </Dialog>
  );
}

/** Publish to the web: a public, read-only link (stops when revoked or when public sharing is turned off). */
export function PublishDialog({ open, onOpenChange, page }: { open: boolean; onOpenChange: (open: boolean) => void; page: DocsPage }) {
  const sharing = useAccessResource("docs.page", open ? page.id : null);
  const canPublic = useAccessPermission(page.org_id, "access.share_public");
  const canPublish = useAccessPermission(page.org_id, "docs.publish");
  const create = useAccessCreateLink("docs.page", page.id);
  const revoke = useAccessRevokeLink("docs.page", page.id);
  const [url, setUrl] = useState<string | null>(null);
  const links = sharing.data?.links ?? [];
  const allowed = canPublic !== false && canPublish !== false;
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Publish to the web">
      <div className="flex flex-col gap-4">
        {sharing.isPending ? (
          <SkeletonRow />
        ) : sharing.isError ? (
          <ErrorState title="Couldn't load the page's links." onRetry={() => void sharing.refetch()} />
        ) : !sharing.data.can_manage ? (
          <p className="type-body text-fg-muted">Only people who manage this page can publish it.</p>
        ) : (
          <>
            {!allowed ? <p className="type-body text-fg-muted">Your {orgsNoun()} doesn&apos;t let you publish pages. Ask an admin.</p> : null}
            {url ? (
              <div className="flex flex-col gap-2">
                <p className="type-label text-fg">Anyone with this link can read the page and its subpages. Copy it now: it isn't shown again.</p>
                <div className="flex gap-2">
                  <Input readOnly value={url} aria-label="Public link" onFocus={(e) => e.currentTarget.select()} />
                  <Button
                    variant="secondary"
                    leftIcon={<Copy />}
                    onClick={() =>
                      void navigator.clipboard?.writeText(url).then(
                        () => toast.success("Link copied."),
                        () => toast.error("Couldn't copy. Select the link and copy it."),
                      )
                    }
                  >
                    Copy
                  </Button>
                </div>
              </div>
            ) : null}
            {links.length > 0 ? (
              <div className="flex flex-col gap-2">
                <p className="type-label font-semibold text-fg">Published</p>
                {links.map((l) => (
                  <div key={l.id} className="flex items-center gap-3 rounded-md border border-border px-3 py-2">
                    <Globe aria-hidden className="size-4 shrink-0 text-fg-muted" />
                    <span className="min-w-0 flex-1 truncate type-label text-fg">
                      Link by {l.created_by?.name ?? "someone"}, {docsTimeAgo(l.created_at)}
                      {!l.working ? " · paused" : ""}
                    </span>
                    <Button variant="ghost" size="sm" loading={revoke.isPending} onClick={() => revoke.mutate(l.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't stop that link.")) })}>
                      Stop
                    </Button>
                  </div>
                ))}
              </div>
            ) : null}
            <div className="flex justify-end">
              <Button
                leftIcon={<Globe />}
                disabled={!allowed}
                loading={create.isPending}
                onClick={() =>
                  create.mutate(
                    { level: "view" },
                    {
                      onSuccess: (l) => setUrl(l.url ?? null),
                      onError: (e) => toast.error(errorMessage(e, "Couldn't publish the page.")),
                    },
                  )
                }
              >
                {links.length ? "Make another link" : "Publish"}
              </Button>
            </div>
          </>
        )}
      </div>
    </Dialog>
  );
}

/** Pages in the trash you can edit: restore, or delete for good (manage). */
export function TrashDialog({ open, onOpenChange, orgId }: { open: boolean; onOpenChange: (open: boolean) => void; orgId: string }) {
  const [q, setQ] = useState("");
  const trash = useDocsTrash(open ? orgId : null, q);
  const restore = useDocsRestorePage(orgId);
  const del = useDocsDeletePermanently(orgId);
  const [confirm, setConfirm] = useState<{ id: string; title: string } | null>(null);
  const router = useRouter();
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Trash" size="lg">
      <div className="flex flex-col gap-3">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search the trash" aria-label="Search the trash" leadingIcon={<Search />} />
        {trash.isPending ? (
          <>
            <SkeletonRow />
            <SkeletonRow />
          </>
        ) : trash.isError ? (
          <ErrorState title="Couldn't load the trash." onRetry={() => void trash.refetch()} />
        ) : trash.data.items.length === 0 ? (
          <EmptyState icon={<Trash2 />} title="The trash is empty" />
        ) : (
          <ul className="flex max-h-96 flex-col divide-y divide-border overflow-y-auto">
            {trash.data.items.map((p) => (
              <li key={p.id} className="flex flex-wrap items-center gap-2 py-2">
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-2 rounded-sm px-1 text-left hover:underline focus-visible:outline-2 focus-visible:outline-ring"
                  onClick={() => {
                    onOpenChange(false);
                    router.push(`/docs/${p.id}`);
                  }}
                >
                  <span aria-hidden>{p.icon ?? <FileText className="size-4 text-fg-muted" />}</span>
                  <span className="truncate type-body text-fg">{docsTitle(p.title)}</span>
                  <span className="shrink-0 type-caption text-fg-muted">{p.deleted_at ? docsTimeAgo(p.deleted_at) : ""}</span>
                </button>
                <Button variant="ghost" size="sm" leftIcon={<RotateCcw />} onClick={() => restore.mutate(p.id, { onSuccess: () => toast.success("Restored."), onError: (e) => toast.error(errorMessage(e, "Couldn't restore the page.")) })}>
                  Restore
                </Button>
                {p.my_level === "manage" ? (
                  <Button variant="danger-ghost" size="sm" leftIcon={<Trash2 />} onClick={() => setConfirm({ id: p.id, title: docsTitle(p.title) })}>
                    Delete
                  </Button>
                ) : null}
              </li>
            ))}
            <li className="py-2">
              <LoadMore hasNextPage={!!trash.hasNextPage} isFetchingNextPage={trash.isFetchingNextPage} fetchNextPage={trash.fetchNextPage} />
            </li>
          </ul>
        )}
      </div>
      <ConfirmDialog
        open={!!confirm}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Delete “${confirm?.title ?? ""}” for good?`}
        confirmLabel="Delete for good"
        tone="danger"
        onConfirm={() => del.mutateAsync(confirm!.id).then(() => setConfirm(null))}
        errorText={(e) => errorMessage(e, "Couldn't delete the page.")}
      >
        Its subpages, comments, history and files go too. It can't be undone.
      </ConfirmDialog>
    </Dialog>
  );
}

/** A new page from Markdown (pasted or from a .md file). */
export function ImportDialog({ open, onOpenChange, orgId, parentId }: { open: boolean; onOpenChange: (open: boolean) => void; orgId: string; parentId: string | null }) {
  const [md, setMd] = useState("");
  const [title, setTitle] = useState("");
  const importer = useDocsImport(orgId);
  const router = useRouter();
  const file = useRef<HTMLInputElement>(null);
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Import Markdown"
      size="lg"
      footer={
        <Button
          loading={importer.isPending}
          disabled={!md.trim()}
          onClick={() =>
            importer.mutate(
              { markdown: md, ...(title.trim() ? { title: title.trim() } : {}), parent_id: parentId },
              {
                onSuccess: (p) => {
                  setMd("");
                  setTitle("");
                  onOpenChange(false);
                  router.push(`/docs/${p.id}`);
                },
                onError: (e) => toast.error(errorMessage(e, "Couldn't import that.")),
              },
            )
          }
        >
          Import
        </Button>
      }
    >
      <div className="flex flex-col gap-3">
        <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title (or the first # heading)" aria-label="Title" maxLength={300} />
        <TextArea value={md} onChange={(e) => setMd(e.target.value)} rows={10} placeholder="Paste Markdown" aria-label="Markdown" className="font-mono" />
        <div>
          <Button variant="secondary" size="sm" leftIcon={<Upload />} onClick={() => file.current?.click()}>
            Choose a .md file
          </Button>
          <input
            ref={file}
            type="file"
            accept=".md,.markdown,.txt,text/markdown,text/plain"
            hidden
            onChange={async (e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              if (!f) return;
              if (f.size > 1024 * 1024) {
                toast.error("That file is over 1 MB. Split it into smaller pages.");
                return;
              }
              setMd(await f.text());
              if (!title) setTitle(f.name.replace(/\.(md|markdown|txt)$/i, ""));
            }}
          />
        </div>
      </div>
    </Dialog>
  );
}

const COVER_COLORS: DocsCoverColor[] = ["neutral", "primary", "accent", "info", "success", "warning", "danger"];
export const COVER_CLASS: Record<DocsCoverColor, string> = {
  neutral: "bg-ink-200",
  primary: "bg-primary-soft",
  accent: "bg-accent-soft",
  info: "bg-info-soft",
  success: "bg-success-soft",
  warning: "bg-warning-soft",
  danger: "bg-danger-soft",
};

/** Cover colour, an uploaded image, its vertical position, or none. */
export function CoverPicker({ page, onDone }: { page: DocsPage; onDone: () => void }) {
  const update = useDocsUpdatePage(page.id);
  const upload = useDocsUploadFile(page.id);
  const file = useRef<HTMLInputElement>(null);
  const [pos, setPos] = useState(page.cover?.position ?? 50);
  const set = (cover: DocsPage["cover"]) => update.mutate({ cover }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the cover.")) });
  return (
    <div className="flex w-72 flex-col gap-3 p-3">
      <p className="type-label font-semibold text-fg">Colour</p>
      <div className="flex flex-wrap gap-2">
        {COVER_COLORS.map((c) => (
          <button key={c} type="button" aria-label={`${c} cover`} className={cn("h-8 w-10 rounded-sm border border-border focus-visible:outline-2 focus-visible:outline-ring", COVER_CLASS[c])} onClick={() => set({ color: c, position: 50 })} />
        ))}
      </div>
      <Button
        variant="secondary"
        size="sm"
        leftIcon={<Upload />}
        loading={upload.isPending}
        onClick={() => file.current?.click()}
      >
        Upload an image
      </Button>
      <input
        ref={file}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          e.target.value = "";
          if (!f) return;
          upload.mutate(
            { file: f, name: f.name },
            { onSuccess: (up) => set({ file_id: up.id, position: 50 }), onError: (err) => toast.error(errorMessage(err, "Couldn't upload the image.")) },
          );
        }}
      />
      {page.cover?.file_id ? (
        <label className="flex flex-col gap-2 type-label text-fg-muted">
          Position
          <Slider value={[pos]} min={0} max={100} step={5} onValueChange={(v) => setPos(v[0] ?? 50)} onValueCommit={(v) => set({ ...page.cover!, position: v[0] ?? 50 })} aria-label="Cover position" />
        </label>
      ) : null}
      {page.cover ? (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            set(null);
            onDone();
          }}
        >
          Remove cover
        </Button>
      ) : null}
    </div>
  );
}
