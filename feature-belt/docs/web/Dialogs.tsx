"use client";

import {
  DOCS_COVER_GALLERY_NAMES,
  DOCS_IMPORT_ACCEPT,
  docsTimeAgo,
  docsTitle,
  errorMessage,
  orgsNoun,
  useAccessCreateLink,
  useAccessPermission,
  useAccessResource,
  useAccessRevokeLink,
  useDocsDeletePermanently,
  useDocsFileUrl,
  useDocsHistory,
  useDocsImport,
  useDocsImportRun,
  useDocsPublishSettings,
  useDocsRestorePage,
  useDocsRestoreSnapshot,
  useDocsSearch,
  useDocsSnapshot,
  useDocsStartImport,
  useDocsTrashFiltered,
  useDocsUpdatePage,
  useDocsUpdatePublishSettings,
  useDocsUpdates,
  useDocsUploadFile,
  useDocsVersionDiff,
  type DocsCover,
  type DocsCoverColor,
  type DocsCoverGallery,
  type DocsDiffEntry,
  type DocsPage,
  type DocsPublishSettings,
} from "@scaffoldapp/shared";
import {
  Avatar,
  Banner,
  Button,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  InfoTip,
  Input,
  SegmentedControl,
  Select,
  Skeleton,
  SkeletonRow,
  Slider,
  Switch,
  Tab,
  TabPanel,
  Tabs,
  TabsList,
  TextArea,
  cn,
  toast,
} from "@scaffoldapp/ui-web";
import { CircleAlert, CircleCheck, Copy, FileUp, Globe, History, Lock, MessageSquare, RotateCcw, Search, Trash2, Upload } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState, type ReactNode } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { BlockView } from "./BlockView";
import { floatingItemClass } from "./Floating";
import { PageIcon } from "./PageIcon";

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
              <span className="flex-1 truncate">Top level, only you and people it&apos;s shared with</span>
            </button>
          ) : null}
          {results.isPending ? <SkeletonRow /> : null}
          {list.map((p) => (
            <button key={p.id} type="button" role="option" aria-selected={false} className={floatingItemClass} onClick={() => onPick(p.id)}>
              <PageIcon icon={p.icon} custom={p.custom_icon} kind={p.kind} size="xs" />
              <span className="flex-1 truncate">{docsTitle(p.title)}</span>
            </button>
          ))}
          {!results.isPending && list.length === 0 ? <p className="px-3 py-2 type-label text-fg-muted">No pages you can edit match.</p> : null}
        </div>
      </div>
    </Dialog>
  );
}

// ---- history -------------------------------------------------------------------

const CHANGE_STYLE: Record<DocsDiffEntry["change"], string> = {
  "": "border-transparent opacity-60",
  added: "border-success bg-success-soft",
  removed: "border-danger bg-danger-soft line-through opacity-80",
  changed: "border-warning bg-warning-soft",
};
const CHANGE_LABEL: Record<DocsDiffEntry["change"], string> = { "": "", added: "Added", removed: "Removed", changed: "Edited" };

/** A version comparison, block by block: added, removed and edited blocks are marked. */
export function DiffView({ snapshotId, against }: { snapshotId: string; against: "previous" | "current" }) {
  const diff = useDocsVersionDiff(snapshotId, against);
  if (diff.isPending) {
    return (
      <div className="flex flex-col gap-2">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-3/4" />
      </div>
    );
  }
  if (diff.isError) return <ErrorState title="Couldn't compare the versions." onRetry={() => void diff.refetch()} />;
  const d = diff.data;
  const s = d.summary;
  const changes = s.added + s.removed + s.changed;
  return (
    <div className="flex flex-col gap-3">
      <p className="type-label text-fg-muted" role="status">
        {s.title_changed ? `Renamed from “${d.from_title}” to “${d.to_title}”. ` : ""}
        {changes === 0 && !s.title_changed
          ? against === "current"
            ? "Nothing has changed since this version."
            : "No changes to the text in this version."
          : changes > 0
            ? `${s.added} added, ${s.changed} edited, ${s.removed} removed`
            : ""}
      </p>
      {changes > 0 ? (
        <ol className="flex flex-col gap-1" aria-label="Changes">
          {d.entries.map((e) => (
            <li
              key={`${e.block.id}-${e.change}`}
              className={cn("rounded-sm border-l-4 px-2 py-1", CHANGE_STYLE[e.change])}
              style={{ marginLeft: `calc(var(--spacing) * ${Math.min(e.depth, 6) * 4})` }}
            >
              {e.change ? <span className="sr-only">{CHANGE_LABEL[e.change]}: </span> : null}
              <BlockView blocks={[{ ...e.block, parent_id: null }]} refs={d.refs} />
              {e.change === "changed" && e.before_text ? (
                <p className="mt-1 line-clamp-3 type-caption text-fg-muted no-underline">
                  <span className="font-semibold">Before:</span> {e.before_text}
                </p>
              ) : null}
            </li>
          ))}
        </ol>
      ) : null}
    </div>
  );
}

/** Saved versions (what changed, what changed since, the whole page; restore) and the page's updates feed. */
export function HistoryDialog({ open, onOpenChange, page, canRestore }: { open: boolean; onOpenChange: (open: boolean) => void; page: DocsPage; canRestore: boolean }) {
  const history = useDocsHistory(page.id, open);
  const updates = useDocsUpdates(page.id, open);
  const [tab, setTab] = useState("versions");
  const [selected, setSelected] = useState<string | null>(null);
  const [mode, setMode] = useState<"version" | "previous" | "current">("previous");
  const current = selected ?? history.data?.items[0]?.id ?? null;
  const snap = useDocsSnapshot(open && mode === "version" ? current : null);
  const restore = useDocsRestoreSnapshot(page.id);
  const [confirm, setConfirm] = useState(false);
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Page history" size="lg">
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList aria-label="History">
          <Tab value="versions">Versions</Tab>
          <Tab value="updates">Updates</Tab>
        </TabsList>
        <TabPanel value="versions">
          <div className="flex min-h-80 flex-col gap-4 pt-3 md:flex-row">
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
            <div className="flex min-w-0 flex-1 flex-col gap-3">
              {current ? (
                <SegmentedControl<"version" | "previous" | "current">
                  value={mode}
                  onValueChange={setMode}
                  aria-label="Show"
                  options={[
                    { value: "previous", label: "Changes" },
                    { value: "current", label: "Since then" },
                    { value: "version", label: "Whole page" },
                  ]}
                />
              ) : null}
              <div className="min-w-0 flex-1 overflow-y-auto rounded-md border border-border p-4 md:max-h-[24rem]">
                {!current ? null : mode !== "version" ? (
                  <DiffView snapshotId={current} against={mode} />
                ) : snap.isPending ? (
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
          </div>
          {canRestore && current ? (
            <div className="flex justify-end pt-4">
              <Button leftIcon={<RotateCcw />} onClick={() => setConfirm(true)}>
                Restore this version
              </Button>
            </div>
          ) : null}
        </TabPanel>
        <TabPanel value="updates">
          <div className="flex max-h-[28rem] min-h-60 flex-col gap-1 overflow-y-auto pt-3">
            {updates.isPending ? (
              <>
                <SkeletonRow />
                <SkeletonRow />
              </>
            ) : updates.isError ? (
              <ErrorState title="Couldn't load the updates." onRetry={() => void updates.refetch()} />
            ) : updates.data.items.length === 0 ? (
              <EmptyState icon={<History />} title="No updates yet" />
            ) : (
              <>
                <ul className="flex flex-col divide-y divide-border">
                  {updates.data.items.map((u) => (
                    <li key={`${u.type}-${u.snapshot_id ?? u.thread_id}-${u.at}`}>
                      <button
                        type="button"
                        className="flex w-full items-start gap-3 rounded-sm px-2 py-3 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:hover:bg-transparent"
                        onClick={() => {
                          if (!u.snapshot_id) return;
                          setSelected(u.snapshot_id);
                          setMode("previous");
                          setTab("versions");
                        }}
                        disabled={!u.snapshot_id}
                      >
                        <span className="flex -space-x-1.5 pt-0.5">
                          {u.people.length ? (
                            u.people.slice(0, 3).map((p) => <Avatar key={p.id} size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />)
                          ) : (
                            <History aria-hidden className="size-4 text-fg-muted" />
                          )}
                        </span>
                        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                          <span className="type-label text-fg">
                            {u.people.map((p) => p.name).join(", ") || "Someone"}
                            <span className="text-fg-muted">{u.type === "comment" ? " commented" : ""}</span>
                          </span>
                          <span className="line-clamp-2 type-caption text-fg-muted">
                            {u.type === "comment" ? <MessageSquare aria-hidden className="mr-1 inline size-3" /> : null}
                            {u.text}
                          </span>
                        </span>
                        <time dateTime={u.at} className="shrink-0 type-caption text-fg-muted">
                          {docsTimeAgo(u.at)}
                        </time>
                      </button>
                    </li>
                  ))}
                </ul>
                <LoadMore hasNextPage={!!updates.hasNextPage} isFetchingNextPage={updates.isFetchingNextPage} fetchNextPage={updates.fetchNextPage} />
              </>
            )}
          </div>
        </TabPanel>
      </Tabs>
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

// ---- publishing ----------------------------------------------------------------

const EXPIRY: { value: string; label: string; days: number }[] = [
  { value: "never", label: "Until I stop it", days: 0 },
  { value: "1", label: "1 day", days: 1 },
  { value: "7", label: "7 days", days: 7 },
  { value: "30", label: "30 days", days: 30 },
  { value: "90", label: "90 days", days: 90 },
];

/**
 * Publish to the web: a public, read-only link (stops when revoked, expired, or when public sharing is
 * turned off) and its options: search engines, copying into a visitor's workspace, theme, outline.
 */
export function PublishDialog({ open, onOpenChange, page }: { open: boolean; onOpenChange: (open: boolean) => void; page: DocsPage }) {
  const sharing = useAccessResource("docs.page", open ? page.id : null);
  const canPublic = useAccessPermission(page.org_id, "access.share_public");
  const canPublish = useAccessPermission(page.org_id, "docs.publish");
  const create = useAccessCreateLink("docs.page", page.id);
  const revoke = useAccessRevokeLink("docs.page", page.id);
  const settings = useDocsPublishSettings(page.id, open);
  const save = useDocsUpdatePublishSettings(page.id);
  const [url, setUrl] = useState<string | null>(null);
  const [expiry, setExpiry] = useState("never");
  const links = sharing.data?.links ?? [];
  const allowed = canPublic !== false && canPublish !== false;
  const set = (patch: Partial<DocsPublishSettings>) => save.mutate(patch, { onError: (e) => toast.error(errorMessage(e, "Couldn't save that option.")) });
  const s = settings.data;
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Publish to the web" size="lg">
      <div className="flex flex-col gap-4">
        {sharing.isPending ? (
          <SkeletonRow />
        ) : sharing.isError ? (
          <ErrorState title="Couldn't load the page's links." onRetry={() => void sharing.refetch()} />
        ) : !sharing.data.can_manage ? (
          <p className="type-body text-fg-muted">Only people who manage this page can publish it.</p>
        ) : (
          <>
            {!allowed ? <Banner tone="warning">Your {orgsNoun()} doesn&apos;t let you publish pages. Ask an admin.</Banner> : null}
            {url ? (
              <div className="flex flex-col gap-2">
                <p className="type-label text-fg">Anyone with this link can read the page and its subpages. Copy it now: it isn&apos;t shown again.</p>
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
                      {l.expires_at ? ` · ends ${new Date(l.expires_at).toLocaleDateString()}` : ""}
                      {!l.working ? " · paused" : ""}
                    </span>
                    <Button variant="ghost" size="sm" loading={revoke.isPending} onClick={() => revoke.mutate(l.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't stop that link.")) })}>
                      Stop
                    </Button>
                  </div>
                ))}
              </div>
            ) : null}
            <section className="flex flex-col gap-3 rounded-md border border-border p-3" aria-label="Options">
              <p className="type-label font-semibold text-fg">Options</p>
              {settings.isPending ? (
                <SkeletonRow />
              ) : settings.isError ? (
                <ErrorState title="Couldn't load the options." onRetry={() => void settings.refetch()} />
              ) : s ? (
                <>
                  <div className="flex items-center justify-between gap-3 type-body text-fg">
                    <span className="flex items-center gap-1">
                      Search engines can list it
                      <InfoTip content="Off: published pages tell search engines not to index them." />
                    </span>
                    <Switch checked={s.search_indexing} onCheckedChange={(v) => set({ search_indexing: v })} aria-label="Search engines can list it" />
                  </div>
                  <div className="flex items-center justify-between gap-3 type-body text-fg">
                    <span className="flex items-center gap-1">
                      Visitors can copy it as a template
                      <InfoTip content="Signed-in visitors get a Duplicate button that copies the page and its published subpages into their own workspace." />
                    </span>
                    <Switch checked={s.allow_duplicate} onCheckedChange={(v) => set({ allow_duplicate: v })} aria-label="Visitors can copy it as a template" />
                  </div>
                  <div className="flex items-center justify-between gap-3 type-body text-fg">
                    Outline of headings
                    <Switch checked={s.show_outline} onCheckedChange={(v) => set({ show_outline: v })} aria-label="Outline of headings" />
                  </div>
                  <div className="flex flex-wrap items-center justify-between gap-3 type-body text-fg">
                    Theme
                    <SegmentedControl<DocsPublishSettings["theme"]>
                      value={s.theme}
                      onValueChange={(v) => set({ theme: v })}
                      aria-label="Theme"
                      options={[
                        { value: "system", label: "Reader's" },
                        { value: "light", label: "Light" },
                        { value: "dark", label: "Dark" },
                      ]}
                    />
                  </div>
                  <p className="flex items-center gap-1 type-caption text-fg-muted">
                    Comments stay private
                    <InfoTip content="Published pages never show comments: people wrote them for colleagues." />
                  </p>
                </>
              ) : null}
            </section>
            <div className="flex flex-wrap items-center justify-end gap-2">
              <span className="type-label text-fg-muted">New link lasts</span>
              <div className="w-40">
                <Select value={expiry} onValueChange={setExpiry} aria-label="New link lasts" options={EXPIRY.map((e) => ({ value: e.value, label: e.label }))} />
              </div>
              <Button
                leftIcon={<Globe />}
                disabled={!allowed}
                loading={create.isPending}
                onClick={() => {
                  const days = EXPIRY.find((e) => e.value === expiry)?.days ?? 0;
                  create.mutate(
                    { level: "view", ...(days ? { expires_at: new Date(Date.now() + days * 86_400_000).toISOString() } : {}) },
                    { onSuccess: (l) => setUrl(l.url ?? null), onError: (e) => toast.error(errorMessage(e, "Couldn't publish the page.")) },
                  );
                }}
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

// ---- trash ---------------------------------------------------------------------

/** Pages in the trash you can edit: search and filter, restore, or delete for good (manage). */
export function TrashDialog({ open, onOpenChange, orgId }: { open: boolean; onOpenChange: (open: boolean) => void; orgId: string }) {
  const [q, setQ] = useState("");
  const [by, setBy] = useState<"anyone" | "me">("anyone");
  const [kind, setKind] = useState<"all" | "page" | "database">("all");
  const trash = useDocsTrashFiltered(orgId, { q, ...(by === "me" ? { by: "me" as const } : {}), ...(kind !== "all" ? { kind } : {}) }, open);
  const restore = useDocsRestorePage(orgId);
  const del = useDocsDeletePermanently(orgId);
  const [confirm, setConfirm] = useState<{ id: string; title: string } | null>(null);
  const router = useRouter();
  const filtered = !!q.trim() || by === "me" || kind !== "all";
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Trash" size="lg">
      <div className="flex flex-col gap-3">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search the trash" aria-label="Search the trash" leadingIcon={<Search />} />
        <div className="flex flex-wrap items-center gap-2">
          <SegmentedControl<"anyone" | "me">
            value={by}
            onValueChange={setBy}
            aria-label="Trashed by"
            options={[
              { value: "anyone", label: "Anyone" },
              { value: "me", label: "By me" },
            ]}
          />
          <SegmentedControl<"all" | "page" | "database">
            value={kind}
            onValueChange={setKind}
            aria-label="Kind"
            options={[
              { value: "all", label: "All" },
              { value: "page", label: "Pages" },
              { value: "database", label: "Databases" },
            ]}
          />
        </div>
        {trash.isPending ? (
          <>
            <SkeletonRow />
            <SkeletonRow />
          </>
        ) : trash.isError ? (
          <ErrorState title="Couldn't load the trash." onRetry={() => void trash.refetch()} />
        ) : trash.data.items.length === 0 ? (
          <EmptyState icon={<Trash2 />} title={filtered ? "Nothing in the trash matches" : "The trash is empty"} />
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
                  <PageIcon icon={p.icon} custom={p.custom_icon} kind={p.kind} size="xs" />
                  <span className="min-w-0 truncate type-body text-fg">{docsTitle(p.title)}</span>
                  <span className="min-w-0 shrink truncate type-caption text-fg-muted">
                    {[p.deleted_at ? docsTimeAgo(p.deleted_at) : "", p.deleted_by ? `by ${p.deleted_by.name}` : "", p.parent_title ? `from ${p.parent_title}` : ""].filter(Boolean).join(" · ")}
                  </span>
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
        Its subpages, comments, history and files go too. It can&apos;t be undone.
      </ConfirmDialog>
    </Dialog>
  );
}

// ---- import --------------------------------------------------------------------

/** One import's progress and report; Open when it's done. */
function ImportProgress({ runId, orgId, onOpen }: { runId: string; orgId: string; onOpen: (pageId: string) => void }) {
  const run = useDocsImportRun(runId, orgId);
  if (run.isPending) return <SkeletonRow />;
  if (run.isError) return <ErrorState title="Couldn't check the import." onRetry={() => void run.refetch()} />;
  const r = run.data;
  const pct = r.pages_total ? Math.round((r.pages_done / r.pages_total) * 100) : r.status === "done" ? 100 : 5;
  return (
    <div className="flex flex-col gap-3" aria-live="polite">
      <div className="flex items-center gap-2">
        {r.status === "done" ? (
          <CircleCheck aria-hidden className="size-5 text-success" />
        ) : r.status === "failed" ? (
          <CircleAlert aria-hidden className="size-5 text-danger" />
        ) : (
          <FileUp aria-hidden className="size-5 text-fg-muted" />
        )}
        <span className="min-w-0 flex-1 truncate type-body text-fg">{r.name}</span>
        <span className="type-caption text-fg-muted">
          {r.status === "running"
            ? `${r.pages_done} of ${r.pages_total || "…"} pages`
            : r.status === "done"
              ? `${r.pages_done} ${r.pages_done === 1 ? "page" : "pages"}, ${r.files_done} ${r.files_done === 1 ? "file" : "files"}`
              : "Stopped"}
        </span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-pill bg-sunken" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label="Import progress">
        <div className={cn("h-full rounded-pill transition-[width] motion-reduce:transition-none", r.status === "failed" ? "bg-danger" : "bg-primary")} style={{ width: `${pct}%` }} />
      </div>
      {r.message ? <Banner tone="danger">{r.message}</Banner> : null}
      {r.errors.length > 0 ? (
        <details className="rounded-md border border-border p-3">
          <summary className="cursor-pointer type-label text-fg">
            {r.errors.length} {r.errors.length === 1 ? "thing" : "things"} couldn&apos;t be imported
          </summary>
          <ul className="flex max-h-40 flex-col gap-1 overflow-y-auto pt-2">
            {r.errors.map((e, i) => (
              <li key={i} className="type-caption text-fg-muted">
                <span className="font-semibold text-fg">{e.path}</span>: {e.message}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
      {r.root_page_id && r.status !== "running" ? (
        <div className="flex justify-end">
          <Button onClick={() => onOpen(r.root_page_id!)}>Open</Button>
        </div>
      ) : null}
    </div>
  );
}

/**
 * Import a file (Markdown, text, HTML, Word, CSV, or a .zip such as a Notion or Confluence export) in
 * the background, or paste Markdown. Used by the import dialog and the New page dialog.
 */
export function ImportPanel({ orgId, parentId, shared = false, onDone }: { orgId: string; parentId: string | null; shared?: boolean; onDone: () => void }) {
  const start = useDocsStartImport(orgId);
  const importer = useDocsImport(orgId);
  const router = useRouter();
  const file = useRef<HTMLInputElement>(null);
  const [runId, setRunId] = useState<string | null>(null);
  const [md, setMd] = useState("");
  const [title, setTitle] = useState("");
  const [dragging, setDragging] = useState(false);
  const begin = (f: File) =>
    start.mutate(
      { file: f, name: f.name, parent_id: parentId, ...(shared && !parentId ? { shared } : {}) },
      { onSuccess: (r) => setRunId(r.id), onError: (e) => toast.error(errorMessage(e, "Couldn't import that file.")) },
    );
  const open = (id: string) => {
    onDone();
    router.push(`/docs/${id}`);
  };
  return (
    <Tabs defaultValue="file">
      <TabsList aria-label="Import from">
        <Tab value="file">File</Tab>
        <Tab value="paste">Paste Markdown</Tab>
      </TabsList>
      <TabPanel value="file">
        <div className="flex flex-col gap-3 pt-3">
          {runId ? (
            <ImportProgress runId={runId} orgId={orgId} onOpen={open} />
          ) : (
            <button
              type="button"
              onClick={() => file.current?.click()}
              onDragOver={(e) => {
                e.preventDefault();
                setDragging(true);
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault();
                setDragging(false);
                const f = e.dataTransfer.files?.[0];
                if (f) begin(f);
              }}
              disabled={start.isPending}
              className={cn(
                "flex min-h-40 flex-col items-center justify-center gap-2 rounded-md border-2 border-dashed px-4 py-6 text-center focus-visible:outline-2 focus-visible:outline-ring",
                dragging ? "border-primary bg-primary-soft" : "border-border-strong hover:bg-sunken",
              )}
            >
              <Upload aria-hidden className="size-6 text-fg-muted" />
              <span className="type-body text-fg">{start.isPending ? "Uploading…" : "Choose a file or drop it here"}</span>
              <span className="type-caption text-fg-muted">Markdown, text, HTML, Word (.docx), CSV, or a .zip of them: a Notion, Confluence or Markdown export</span>
            </button>
          )}
          <input
            ref={file}
            type="file"
            accept={DOCS_IMPORT_ACCEPT}
            hidden
            onChange={(e) => {
              const f = e.target.files?.[0];
              e.target.value = "";
              if (f) begin(f);
            }}
          />
          {runId ? (
            <div>
              <Button variant="ghost" size="sm" onClick={() => setRunId(null)}>
                Import another file
              </Button>
            </div>
          ) : null}
        </div>
      </TabPanel>
      <TabPanel value="paste">
        <div className="flex flex-col gap-3 pt-3">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Title (or the first # heading)" aria-label="Title" maxLength={300} />
          <TextArea value={md} onChange={(e) => setMd(e.target.value)} rows={8} placeholder="Paste Markdown" aria-label="Markdown" className="font-mono" />
          <div className="flex justify-end">
            <Button
              loading={importer.isPending}
              disabled={!md.trim()}
              onClick={() =>
                importer.mutate(
                  { markdown: md, ...(title.trim() ? { title: title.trim() } : {}), parent_id: parentId, ...(shared && !parentId ? { shared } : {}) },
                  {
                    onSuccess: (p) => {
                      setMd("");
                      setTitle("");
                      open(p.id);
                    },
                    onError: (e) => toast.error(errorMessage(e, "Couldn't import that.")),
                  },
                )
              }
            >
              Import
            </Button>
          </div>
        </div>
      </TabPanel>
    </Tabs>
  );
}

/** Import into the workspace (or inside a page). */
export function ImportDialog({ open, onOpenChange, orgId, parentId }: { open: boolean; onOpenChange: (open: boolean) => void; orgId: string; parentId: string | null }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={parentId ? "Import into this page" : "Import"} size="lg">
      {open ? <ImportPanel orgId={orgId} parentId={parentId} onDone={() => onOpenChange(false)} /> : null}
    </Dialog>
  );
}

// ---- covers --------------------------------------------------------------------

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

/** Built-in covers (shared DOCS_COVER_GALLERY): gradients between two palette roles. */
export const GALLERY_CLASS: Record<DocsCoverGallery, string> = {
  dawn: "bg-linear-to-br from-warning-soft to-danger-soft",
  lagoon: "bg-linear-to-br from-info-soft to-success-soft",
  dusk: "bg-linear-to-br from-primary to-accent",
  meadow: "bg-linear-to-br from-success-soft to-warning-soft",
  ember: "bg-linear-to-br from-danger to-warning",
  glacier: "bg-linear-to-br from-info-soft to-primary-soft",
  orchid: "bg-linear-to-br from-accent-soft to-primary-soft",
  slate: "bg-linear-to-br from-border-strong to-sunken",
  citrus: "bg-linear-to-br from-warning to-success",
  tide: "bg-linear-to-br from-primary to-info",
  aurora: "bg-linear-to-br from-success to-accent",
  sand: "bg-linear-to-br from-sunken to-warning-soft",
};

/** The background class of a cover (colour or gallery), or the sunken fill behind an image. */
export function coverClass(cover: DocsCover | null | undefined): string {
  if (!cover) return "";
  if (cover.gallery) return GALLERY_CLASS[cover.gallery] ?? "bg-sunken";
  if (cover.color) return COVER_CLASS[cover.color];
  return "bg-sunken";
}

/** A page's cover: image (with its vertical focus), gallery gradient or colour. */
export function CoverArt({ cover, token, className, children }: { cover: DocsCover; token?: string | null; className?: string; children?: ReactNode }) {
  const file = useDocsFileUrl(cover.file_id, token);
  return (
    <div className={cn("relative w-full overflow-hidden", coverClass(cover), className)}>
      {cover.file_id && file.data?.url ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={file.data.url} alt="" className="size-full object-cover" style={{ objectPosition: `center ${cover.position}%` }} />
      ) : null}
      {children}
    </div>
  );
}

/** Cover gallery, colours, an uploaded image and its vertical position, or none. */
export function CoverPicker({ page, onDone }: { page: DocsPage; onDone: () => void }) {
  const update = useDocsUpdatePage(page.id);
  const upload = useDocsUploadFile(page.id);
  const file = useRef<HTMLInputElement>(null);
  const [pos, setPos] = useState(page.cover?.position ?? 50);
  const set = (cover: DocsPage["cover"]) => update.mutate({ cover }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change the cover.")) });
  return (
    <div className="flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-3 p-3">
      <p className="type-label font-semibold text-fg">Gallery</p>
      <div className="grid grid-cols-4 gap-2">
        {DOCS_COVER_GALLERY_NAMES.map((g) => (
          <button
            key={g}
            type="button"
            aria-label={`${g} cover`}
            aria-pressed={page.cover?.gallery === g}
            className={cn("h-10 rounded-sm border focus-visible:outline-2 focus-visible:outline-ring", GALLERY_CLASS[g], page.cover?.gallery === g ? "border-fg" : "border-border")}
            onClick={() => set({ gallery: g, position: 50 })}
          />
        ))}
      </div>
      <p className="type-label font-semibold text-fg">Colour</p>
      <div className="flex flex-wrap gap-2">
        {COVER_COLORS.map((c) => (
          <button
            key={c}
            type="button"
            aria-label={`${c} cover`}
            aria-pressed={page.cover?.color === c && !page.cover?.gallery}
            className={cn("h-8 w-10 rounded-sm border focus-visible:outline-2 focus-visible:outline-ring", COVER_CLASS[c], page.cover?.color === c ? "border-fg" : "border-border")}
            onClick={() => set({ color: c, position: 50 })}
          />
        ))}
      </div>
      <Button variant="secondary" size="sm" leftIcon={<Upload />} loading={upload.isPending} onClick={() => file.current?.click()}>
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
          upload.mutate({ file: f, name: f.name }, { onSuccess: (up) => set({ file_id: up.id, position: 50 }), onError: (err) => toast.error(errorMessage(err, "Couldn't upload the image.")) });
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
