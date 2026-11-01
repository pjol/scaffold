"use client";

import {
  docsEndpoints,
  docsKeyBetween,
  docsTitle,
  errorMessage,
  generateId,
  useDocsBacklinks,
  useDocsChildren,
  useDocsComments,
  useDocsCreatePage,
  useDocsDeletePermanently,
  useDocsDuplicatePage,
  useDocsExportMarkdown,
  useDocsFileUrl,
  useDocsMovePage,
  useDocsPage,
  useDocsPageSession,
  useDocsPresence,
  useDocsRestorePage,
  useDocsSetFavorite,
  useDocsTrashPage,
  useDocsUpdatePage,
  useDocsUploadFile,
  useEndpoints,
  useMe,
  type DocsPageDetail,
  type DocsSaveState,
} from "@scaffoldapp/shared";
import { Avatar, Banner, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Sheet, Skeleton, Tooltip, cn, toast, useMediaQuery } from "@scaffoldapp/ui-web";
import {
  ArrowLeft,
  Check,
  CloudOff,
  Copy,
  CornerUpRight,
  Download,
  FileText,
  Globe,
  History,
  ImagePlus,
  Link2,
  Loader2,
  Lock,
  MessageSquare,
  MoveHorizontal,
  Smile,
  Star,
  Trash2,
  LockOpen,
  Upload,
  TriangleAlert,
} from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { CommentsPanel, type CommentTarget } from "./Comments";
import { DatabaseView } from "./database/DatabaseView";
import { COVER_CLASS, CoverPicker, HistoryDialog, ImportDialog, PagePicker, PublishDialog } from "./Dialogs";
import { Editor } from "./editor/Editor";
import type { EditorSelection } from "./editor/context";
import { draftOps, toDrafts } from "./editor/ops";
import { EmojiPicker } from "./EmojiPicker";
import { Floating } from "./Floating";
import { RowProperties } from "./RowProperties";

function SaveState({ state, error }: { state: DocsSaveState; error: string | null }) {
  const map = {
    saved: { icon: <Check aria-hidden />, label: "Saved", tone: "text-fg-subtle" },
    saving: { icon: <Loader2 aria-hidden className="animate-spin motion-reduce:animate-none" />, label: "Saving…", tone: "text-fg-muted" },
    offline: { icon: <CloudOff aria-hidden />, label: "Offline", tone: "text-warning-strong" },
    error: { icon: <TriangleAlert aria-hidden />, label: "Not saved", tone: "text-danger-strong" },
  }[state];
  const tip = state === "offline" ? "Your changes will save when you're back online. Keep this tab open." : state === "error" ? (error ?? "Couldn't save the last change.") : null;
  const body = (
    <span role="status" aria-live="polite" className={cn("flex items-center gap-1 type-caption [&_svg]:size-3.5", map.tone)}>
      {map.icon}
      <span className="hidden sm:inline">{map.label}</span>
      <span className="sr-only sm:hidden">{map.label}</span>
    </span>
  );
  return tip ? <Tooltip content={tip}>{body}</Tooltip> : body;
}

function Cover({ detail, editable }: { detail: DocsPageDetail; editable: boolean }) {
  const cover = detail.page.cover;
  const file = useDocsFileUrl(cover?.file_id);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  if (!cover) return null;
  return (
    <div className={cn("group/cover relative h-36 w-full overflow-hidden sm:h-48", cover.color ? COVER_CLASS[cover.color] : "bg-sunken")}>
      {cover.file_id && file.data?.url ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={file.data.url} alt="" className="size-full object-cover" style={{ objectPosition: `center ${cover.position}%` }} />
      ) : null}
      {editable ? (
        <div className="absolute right-4 bottom-3 opacity-100 transition-opacity sm:opacity-0 sm:group-hover/cover:opacity-100 sm:focus-within:opacity-100 motion-reduce:transition-none">
          <Button size="sm" variant="secondary" onClick={(e) => setAnchor(e.currentTarget)}>
            Change cover
          </Button>
        </div>
      ) : null}
      <Floating open={!!anchor} anchor={anchor} onClose={() => setAnchor(null)} autoFocus placement="bottom-end" label="Cover">
        <CoverPicker page={detail.page} onDone={() => setAnchor(null)} />
      </Floating>
    </div>
  );
}

function TitleField({ detail, editable, onEnter }: { detail: DocsPageDetail; editable: boolean; onEnter: () => void }) {
  const update = useDocsUpdatePage(detail.page.id);
  const [value, setValue] = useState(detail.page.title);
  const ref = useRef<HTMLTextAreaElement>(null);
  const focused = useRef(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (!focused.current) setValue(detail.page.title);
  }, [detail.page.title]);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [value]);
  const save = (v: string) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    if (v !== detail.page.title) update.mutate({ title: v }, { onError: (e) => toast.error(errorMessage(e, "Couldn't rename the page.")) });
  };
  if (!editable) return <h1 className="type-display break-words text-fg [overflow-wrap:anywhere]">{docsTitle(detail.page.title)}</h1>;
  return (
    <textarea
      ref={ref}
      value={value}
      rows={1}
      maxLength={300}
      placeholder="Untitled"
      aria-label="Page title"
      className="w-full resize-none overflow-hidden bg-transparent type-display text-fg outline-none placeholder:text-fg-subtle"
      onFocus={() => {
        focused.current = true;
      }}
      onBlur={() => {
        focused.current = false;
        save(value);
      }}
      onChange={(e) => {
        const v = e.target.value.replace(/\n/g, " ");
        setValue(v);
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => save(v), 600);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter" || (e.key === "ArrowDown" && e.currentTarget.selectionStart === value.length)) {
          e.preventDefault();
          save(value);
          onEnter();
        }
      }}
    />
  );
}

function useVisible(): boolean {
  const [v, setV] = useState(true);
  useEffect(() => {
    const on = () => setV(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", on);
    return () => document.removeEventListener("visibilitychange", on);
  }, []);
  return v;
}

/** /docs/[id]: a page, database or database row. */
export function PageScreen({ id }: { id: string }) {
  const detail = useDocsPage(id);
  if (detail.isPending) {
    return (
      <div className="mx-auto flex w-full max-w-prose flex-col gap-4 px-gutter py-section" aria-busy aria-label="Loading page">
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
        <Skeleton className="h-4 w-3/4" />
      </div>
    );
  }
  if (detail.isError) {
    return (
      <div className="flex flex-1 items-center justify-center px-gutter py-section">
        {detail.error.status === 404 ? (
          <EmptyState
            icon={<Lock />}
            title="This page doesn't exist, or you can't open it"
            action={
              <Button asChild variant="secondary">
                <Link href="/docs">Go to docs</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this page." onRetry={() => void detail.refetch()} />
        )}
      </div>
    );
  }
  return <LoadedPage key={id} detail={detail.data} />;
}

function LoadedPage({ detail }: { detail: DocsPageDetail }) {
  const page = detail.page;
  const router = useRouter();
  const search = useSearchParams();
  const d = useEndpoints(docsEndpoints);
  const level = page.my_level;
  const trashed = !!page.deleted_at;
  const canEdit = level === "edit" || level === "manage";
  const editable = canEdit && !page.locked && !trashed;
  const canComment = (canEdit || level === "comment") && !trashed;
  const canManage = level === "manage";
  const { session, snapshot } = useDocsPageSession<EditorSelection>(page.id, detail, (message) => toast.error(message));
  const [focusedBlock, setFocusedBlock] = useState<string | null>(null);
  const visible = useVisible();
  const me = useMe();
  const allViewers = useDocsPresence(page.id, focusedBlock, visible && !trashed);
  const viewers = allViewers.filter((v) => v.user.id !== me.data?.id);
  const wide = useMediaQuery("(min-width: 1280px)");
  const [comments, setComments] = useState<{ open: boolean; block: string | null; thread: string | null; pending: CommentTarget | null }>({ open: false, block: null, thread: null, pending: null });
  const [dialog, setDialog] = useState<null | "history" | "publish" | "move" | "move-blocks" | "import" | "delete">(null);
  const [movingBlocks, setMovingBlocks] = useState<string[]>([]);
  const [iconAnchor, setIconAnchor] = useState<HTMLElement | null>(null);
  const [coverAnchor, setCoverAnchor] = useState<HTMLElement | null>(null);
  const update = useDocsUpdatePage(page.id);
  const favorite = useDocsSetFavorite(page.id);
  const createPage = useDocsCreatePage(page.org_id);
  const move = useDocsMovePage(page.org_id);
  const duplicate = useDocsDuplicatePage(page.org_id);
  const trash = useDocsTrashPage(page.org_id);
  const restore = useDocsRestorePage(page.org_id);
  const del = useDocsDeletePermanently(page.org_id);
  const upload = useDocsUploadFile(page.id);
  const exporter = useDocsExportMarkdown();
  const children = useDocsChildren(page.id, page.kind === "page");
  const backlinks = useDocsBacklinks(page.id);
  const openThreads = useDocsComments(page.id, "open");
  const editorFocus = useRef<(() => void) | null>(null);

  useEffect(() => {
    const c = search?.get("comment");
    if (c) setComments({ open: true, block: null, thread: c, pending: null });
  }, [search]);

  useEffect(() => {
    if (!session) return undefined;
    const onUnload = (e: BeforeUnloadEvent) => {
      session.flushNow();
      if (session.hasUnsaved()) e.preventDefault();
    };
    const onHide = () => {
      if (document.visibilityState === "hidden") session.flushNow();
    };
    window.addEventListener("beforeunload", onUnload);
    document.addEventListener("visibilitychange", onHide);
    return () => {
      window.removeEventListener("beforeunload", onUnload);
      document.removeEventListener("visibilitychange", onHide);
    };
  }, [session]);

  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const title = docsTitle(page.title);
  const people = Array.from(new Map(viewers.map((v) => [v.user.id, v.user])).values());

  const actions: ResourceMenuAction[] = [];
  if (!trashed) {
    actions.push({ id: "copy-link", label: "Copy link", icon: Link2, onSelect: () => void navigator.clipboard?.writeText(`${window.location.origin}/docs/${page.id}`).then(() => toast.success("Link copied."), () => toast.error("Couldn't copy the link.")) });
    if (canEdit && !page.is_row) actions.push({ id: "move", label: "Move to…", icon: CornerUpRight, onSelect: () => setDialog("move") });
    if (canEdit) actions.push({ id: "duplicate", label: "Duplicate", icon: Copy, onSelect: () => duplicate.mutate(page.id, { onSuccess: (p) => router.push(`/docs/${p.id}`), onError: onErr("Couldn't duplicate the page.") }) });
    if (canEdit) actions.push({ id: "lock", label: page.locked ? "Allow editing" : "Lock page", icon: page.locked ? LockOpen : Lock, onSelect: () => update.mutate({ locked: !page.locked }, { onError: onErr("Couldn't change the lock.") }) });
    if (canEdit && page.kind === "page") actions.push({ id: "width", label: page.full_width ? "Normal width" : "Full width", icon: MoveHorizontal, onSelect: () => update.mutate({ full_width: !page.full_width }) });
    if (canManage) actions.push({ id: "publish", label: detail.published ? "Published to the web…" : "Publish to the web…", icon: Globe, onSelect: () => setDialog("publish") });
    if (page.kind === "page") actions.push({ id: "history", label: "Page history", icon: History, onSelect: () => setDialog("history") });
    actions.push({
      id: "export",
      label: "Export as Markdown",
      icon: Download,
      onSelect: () =>
        exporter.mutate(page.id, {
          onSuccess: (md) => {
            const url = URL.createObjectURL(new Blob([md], { type: "text/markdown" }));
            const a = document.createElement("a");
            a.href = url;
            a.download = `${title.replace(/[^\w.-]+/g, "-") || "page"}.md`;
            a.click();
            setTimeout(() => URL.revokeObjectURL(url), 1000);
          },
          onError: onErr("Couldn't export the page."),
        }),
    });
    if (canEdit && page.kind === "page") actions.push({ id: "import", label: "Import Markdown here…", icon: Upload, onSelect: () => setDialog("import") });
    if (canEdit)
      actions.push({
        id: "trash",
        label: "Move to trash",
        icon: Trash2,
        destructive: true,
        onSelect: () =>
          trash.mutate(
            { pageId: page.id, databaseId: page.is_row ? page.parent_id : null },
            {
              onSuccess: () => {
                toast.success(`“${title}” is in the trash.`, { action: { label: "Undo", onClick: () => restore.mutate(page.id) } });
                router.push(page.parent_id ? `/docs/${page.parent_id}` : "/docs");
              },
              onError: onErr("Couldn't move it to the trash."),
            },
          ),
      });
  }

  const openComments = (block: string | null, thread?: string) => setComments({ open: true, block, thread: thread ?? null, pending: null });
  const commentsCount = Object.values(detail.comment_counts).reduce((a, b) => a + b, 0);
  const linkedChildren = new Set(snapshot.blocks.filter((b) => b.type === "page" || b.type === "database").map((b) => b.props.page_id ?? b.props.database_id));
  const loose = (children.data ?? []).filter((c) => !linkedChildren.has(c.id));

  const panelFor = (header: boolean) => (
    <CommentsPanel
      header={header}
      pageId={page.id}
      orgId={page.org_id}
      canComment={canComment}
      canManage={canManage}
      refs={detail.refs}
      blockFilter={comments.block}
      activeThread={comments.thread}
      pending={comments.pending}
      onClearFilter={() => setComments((c) => ({ ...c, block: null }))}
      onClearPending={() => setComments((c) => ({ ...c, pending: null }))}
      onActivate={(thread, block) => {
        setComments((c) => ({ ...c, thread }));
        if (block) document.querySelector(`[data-block-id="${block}"]`)?.scrollIntoView({ block: "center", behavior: "smooth" });
      }}
      onClose={() => setComments({ open: false, block: null, thread: null, pending: null })}
    />
  );

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto" id="docs-page-scroll">
        <header className="sticky top-0 z-20 flex min-h-12 items-center gap-2 border-b border-border bg-canvas px-gutter py-1">
          <Link href="/docs" aria-label="All pages" className="flex size-control-sm shrink-0 items-center justify-center rounded-md text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring lg:hidden [&_svg]:size-4">
            <ArrowLeft aria-hidden />
          </Link>
          <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden type-label text-fg-muted">
            {detail.path.map((c) => (
              <span key={c.id} className="hidden min-w-0 items-center gap-1 sm:flex">
                <Link href={`/docs/${c.id}`} className="truncate rounded-sm px-1 hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring">
                  {c.icon ? `${c.icon} ` : ""}
                  {c.title}
                </Link>
                <span aria-hidden>/</span>
              </span>
            ))}
            <span className="truncate px-1 text-fg" aria-current="page">
              {page.icon ? `${page.icon} ` : ""}
              {title}
            </span>
            {page.locked ? (
              <Tooltip content="Locked: choose Allow editing in ⋮ to change it.">
                <span className="flex shrink-0 items-center text-fg-muted">
                  <Lock aria-hidden className="size-3.5" />
                  <span className="sr-only">Locked</span>
                </span>
              </Tooltip>
            ) : null}
          </nav>
          {people.length ? (
            <div className="flex items-center -space-x-1.5" aria-label={`Also here: ${people.map((u) => u.name).join(", ")}`} role="group">
              {people.slice(0, 4).map((u) => (
                <span key={u.id} title={u.name} className="rounded-pill ring-2 ring-canvas">
                  <Avatar size="xs" name={u.name} {...(u.avatar_url ? { src: u.avatar_url } : {})} />
                </span>
              ))}
              {people.length > 4 ? <span className="pl-2 type-caption text-fg-muted">+{people.length - 4}</span> : null}
            </div>
          ) : null}
          {editable && page.kind === "page" ? <SaveState state={snapshot.saveState} error={snapshot.error} /> : null}
          {!canEdit && level !== "none" ? <span className="hidden type-caption text-fg-muted sm:inline">{level === "comment" ? "Can comment" : "View only"}</span> : null}
          <Button variant="ghost" size="sm" leftIcon={<MessageSquare />} aria-label={`Comments${commentsCount ? `, ${commentsCount} open` : ""}`} onClick={() => (comments.open ? setComments((c) => ({ ...c, open: false })) : openComments(null))}>
            {commentsCount ? commentsCount : <span className="hidden sm:inline">Comments</span>}
          </Button>
          <IconButton
            label={detail.favorite ? "Remove from favorites" : "Add to favorites"}
            icon={<Star className={detail.favorite ? "fill-current text-warning" : undefined} />}
            size="sm"
            variant="ghost"
            onClick={() => favorite.mutate(!detail.favorite, { onError: onErr("Couldn't update favorites.") })}
          />
          <ResourceMenu feature="docs" kind="docs.page" id={page.id} title={title} orgId={page.org_id} actions={actions} />
        </header>
        {trashed ? (
          <div className="px-gutter pt-4">
            <Banner tone="warning">
              <span className="flex flex-wrap items-center gap-2">
                This page is in the trash.
                {canEdit ? (
                  <Button size="sm" variant="secondary" onClick={() => restore.mutate(page.id, { onSuccess: () => toast.success("Restored."), onError: onErr("Couldn't restore the page.") })}>
                    Restore
                  </Button>
                ) : null}
                {canManage ? (
                  <Button size="sm" variant="danger-ghost" onClick={() => setDialog("delete")}>
                    Delete for good
                  </Button>
                ) : null}
              </span>
            </Banner>
          </div>
        ) : null}
        <Cover detail={detail} editable={editable} />
        <div className={cn("mx-auto flex w-full flex-col gap-4 px-gutter pt-8 pb-24 sm:px-16", page.full_width || page.kind === "database" ? "max-w-content" : "max-w-3xl")}>
          <div className="flex flex-col gap-2">
            {page.icon ? (
              <button
                type="button"
                disabled={!editable}
                aria-label="Change icon"
                className="w-fit rounded-md type-hero hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:hover:bg-transparent"
                onClick={(e) => setIconAnchor(e.currentTarget)}
              >
                {page.icon}
              </button>
            ) : null}
            {editable && (!page.icon || !page.cover) ? (
              <div className="flex gap-1">
                {!page.icon ? (
                  <Button variant="ghost" size="sm" leftIcon={<Smile />} onClick={(e) => setIconAnchor(e.currentTarget)}>
                    Add icon
                  </Button>
                ) : null}
                {!page.cover ? (
                  <Button variant="ghost" size="sm" leftIcon={<ImagePlus />} onClick={(e) => setCoverAnchor(e.currentTarget)}>
                    Add cover
                  </Button>
                ) : null}
              </div>
            ) : null}
            <TitleField detail={detail} editable={editable} onEnter={() => editorFocus.current?.()} />
          </div>
          {page.is_row && detail.database ? (
            <div className="flex flex-col gap-2 border-b border-border pb-4">
              {detail.database.title ? (
                <Link href={`/docs/${detail.database.id}`} className="w-fit type-caption text-fg-muted hover:text-fg hover:underline">
                  In {detail.database.icon ? `${detail.database.icon} ` : ""}
                  {detail.database.title}
                </Link>
              ) : null}
              <RowProperties row={page} database={detail.database} refs={detail.refs} editable={canEdit && !trashed} />
            </div>
          ) : null}
          {page.kind === "database" ? (
            <DatabaseView db={page} views={detail.views} orgId={page.org_id} editable={editable} />
          ) : session ? (
            <FocusBridge register={(fn) => (editorFocus.current = fn)} session={session} editable={editable}>
              <Editor
                pageId={page.id}
                orgId={page.org_id}
                session={session}
                snap={snapshot}
                refs={detail.refs}
                editable={editable}
                canComment={canComment}
                viewers={viewers}
                commentCounts={detail.comment_counts}
                threads={openThreads.data ?? []}
                activeThread={comments.thread}
                onFocusBlock={setFocusedBlock}
                onOpenComments={openComments}
                onComment={(target) => setComments({ open: true, block: null, thread: null, pending: target })}
                onOpenPage={(pid) => router.push(`/docs/${pid}`)}
                onCreateSubpage={() => createPage.mutateAsync({ parent_id: page.id }).then((p) => p.id, (e) => (toast.error(errorMessage(e, "Couldn't create the page.")), null))}
                onCreateDatabase={() =>
                  createPage.mutateAsync({ parent_id: page.id, kind: "database", inline: true, title: "Untitled database" }).then(
                    (p) => p.id,
                    (e) => (toast.error(errorMessage(e, "Couldn't create the database.")), null),
                  )
                }
                onUpload={(file) => upload.mutateAsync({ file, name: file.name })}
                onMoveToPage={(ids) => {
                  setMovingBlocks(ids);
                  setDialog("move-blocks");
                }}
              />
            </FocusBridge>
          ) : (
            <Skeleton className="h-24 w-full" />
          )}
          {loose.length ? (
            <section className="flex flex-col gap-1 border-t border-border pt-4" aria-label="Pages inside">
              <h2 className="type-label font-semibold text-fg-muted">Pages inside</h2>
              {loose.map((c) => (
                <Link key={c.id} href={`/docs/${c.id}`} className="flex min-h-control items-center gap-2 rounded-sm px-1 type-body text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                  <span aria-hidden className="flex size-6 items-center justify-center">
                    {c.icon ?? <FileText className="size-4 text-fg-muted" />}
                  </span>
                  <span className="truncate underline decoration-border-strong underline-offset-4">{docsTitle(c.title)}</span>
                </Link>
              ))}
            </section>
          ) : null}
          {backlinks.data?.length ? (
            <details className="border-t border-border pt-4">
              <summary className="cursor-pointer type-label text-fg-muted hover:text-fg">
                {backlinks.data.length} {backlinks.data.length === 1 ? "page links" : "pages link"} here
              </summary>
              <div className="flex flex-col gap-1 pt-2">
                {backlinks.data.map((b) => (
                  <Link key={b.id} href={`/docs/${b.id}`} className="flex min-h-control items-center gap-2 rounded-sm px-1 type-body text-fg hover:bg-sunken">
                    <span aria-hidden>{b.icon ?? <FileText className="size-4 text-fg-muted" />}</span>
                    {docsTitle(b.title)}
                  </Link>
                ))}
              </div>
            </details>
          ) : null}
        </div>
      </div>
      {comments.open && wide ? <aside className="flex w-80 shrink-0 flex-col border-l border-border bg-surface">{panelFor(true)}</aside> : null}
      {!wide ? (
        <Sheet open={comments.open} onOpenChange={(o) => !o && setComments({ open: false, block: null, thread: null, pending: null })} title="Comments">
          {comments.open ? panelFor(false) : null}
        </Sheet>
      ) : null}
      <Floating open={!!iconAnchor} anchor={iconAnchor} onClose={() => setIconAnchor(null)} autoFocus label="Page icon">
        <EmojiPicker
          onPick={(emoji) => {
            setIconAnchor(null);
            update.mutate({ icon: emoji }, { onError: onErr("Couldn't change the icon.") });
          }}
          {...(page.icon
            ? {
                onRemove: () => {
                  setIconAnchor(null);
                  update.mutate({ icon: null });
                },
              }
            : {})}
        />
      </Floating>
      <Floating open={!!coverAnchor} anchor={coverAnchor} onClose={() => setCoverAnchor(null)} autoFocus label="Cover">
        <CoverPicker page={page} onDone={() => setCoverAnchor(null)} />
      </Floating>
      <HistoryDialog open={dialog === "history"} onOpenChange={(o) => setDialog(o ? "history" : null)} page={page} canRestore={editable} />
      <PublishDialog open={dialog === "publish"} onOpenChange={(o) => setDialog(o ? "publish" : null)} page={page} />
      <ImportDialog open={dialog === "import"} onOpenChange={(o) => setDialog(o ? "import" : null)} orgId={page.org_id} parentId={page.id} />
      <PagePicker
        open={dialog === "move"}
        onOpenChange={(o) => setDialog(o ? "move" : null)}
        orgId={page.org_id}
        title={`Move “${title}”`}
        exclude={[page.id]}
        allowTop={canManage}
        onPick={(parent) => {
          setDialog(null);
          move.mutate({ pageId: page.id, parent_id: parent, last: true }, { onSuccess: () => toast.success("Moved."), onError: onErr("Couldn't move the page.") });
        }}
      />
      <PagePicker
        open={dialog === "move-blocks"}
        onOpenChange={(o) => setDialog(o ? "move-blocks" : null)}
        orgId={page.org_id}
        title={movingBlocks.length > 1 ? `Move ${movingBlocks.length} blocks to…` : "Move block to…"}
        exclude={[page.id]}
        onPick={async (target) => {
          setDialog(null);
          if (!target || !session) return;
          try {
            const dest = await d.page(target);
            if (dest.page.my_level !== "edit" && dest.page.my_level !== "manage") throw new Error("You can't edit that page.");
            const roots = dest.blocks.filter((b) => !b.parent_id);
            const drafts = toDrafts(session.getSnapshot(), movingBlocks);
            const { ops } = draftOps(drafts, null, roots[roots.length - 1]?.position ?? null, null);
            await d.applyOps(target, generateId(), ops);
            session.apply(movingBlocks.map((bid) => ({ op: "delete" as const, id: bid })));
            toast.success(`Moved to “${docsTitle(dest.page.title)}”.`, { action: { label: "Open", onClick: () => router.push(`/docs/${target}`) } });
          } catch (e) {
            toast.error(errorMessage(e, "Couldn't move the blocks. Images and files stay on their page."));
          }
        }}
      />
      <ConfirmDialog
        open={dialog === "delete"}
        onOpenChange={(o) => setDialog(o ? "delete" : null)}
        title={`Delete “${title}” for good?`}
        confirmLabel="Delete for good"
        tone="danger"
        onConfirm={() => del.mutateAsync(page.id).then(() => router.push("/docs"))}
        errorText={(e) => errorMessage(e, "Couldn't delete the page.")}
      >
        Its subpages, comments, history and files go too. It can't be undone.
      </ConfirmDialog>
    </div>
  );
}

/** Lets the title move focus into the editor (creating the first block if needed). */
function FocusBridge({ register, session, editable, children }: { register: (fn: () => void) => void; session: NonNullable<ReturnType<typeof useDocsPageSession<EditorSelection>>["session"]>; editable: boolean; children: React.ReactNode }) {
  useEffect(() => {
    register(() => {
      const snap = session.getSnapshot();
      const first = snap.children.get("")?.[0];
      if (first) {
        const el = document.querySelector<HTMLElement>(`[data-block-id="${first.id}"] [contenteditable="true"], [data-block-id="${first.id}"] textarea`);
        el?.focus();
        return;
      }
      if (!editable) return;
      const nid = generateId();
      session.apply([{ op: "create", id: nid, type: "paragraph", parent_id: null, position: docsKeyBetween(null, null), props: { text: [] } }]);
      requestAnimationFrame(() => document.querySelector<HTMLElement>(`[data-block-id="${nid}"] [contenteditable="true"]`)?.focus());
    });
  }, [register, session, editable]);
  return <>{children}</>;
}
