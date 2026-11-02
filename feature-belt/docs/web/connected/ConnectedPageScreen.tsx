"use client";

import {
  connectionFixPath,
  docsConnectedHref,
  docsEndpoints,
  docsKeyBetween,
  docsPlatformName,
  docsTitle,
  errorMessage,
  generateId,
  isConnectionErrorCode,
  useActiveOrg,
  useDocsConnectedCopy,
  useDocsConnectedPage,
  useDocsConnectedPageSession,
  useDocsConnectedRename,
  useDocsConnectedSetRow,
  useEndpoints,
  type DocsBlockType,
  type DocsConnectedPageDetail,
  type DocsValue,
} from "@scaffoldapp/shared";
import { Banner, Button, EmptyState, ErrorState, Sheet, Skeleton, SourceBadge, cn, toast, useMediaQuery, withReturnTo } from "@scaffoldapp/ui-web";
import { ArrowLeft, Copy, ExternalLink, FilePlus2, Link2, Lock, MessageSquare, RefreshCw, Users } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { PropertyDisplay, PropertyEditor } from "../database/PropertyValue";
import { PagePicker } from "../Dialogs";
import { Editor, type EditorConnected } from "../editor/Editor";
import type { EditorSelection } from "../editor/context";
import { Floating } from "../Floating";
import { PROPERTY_ICON } from "../format";
import { SaveState } from "../PageScreen";
import { ConnectedComments } from "./ConnectedComments";
import { ConnectedDatabaseView } from "./ConnectedDatabaseView";

/** Block types each platform takes from the editor. */
const NOTION_TYPES: DocsBlockType[] = ["paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout", "code", "divider"];
const GOOGLE_TYPES: DocsBlockType[] = ["paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list"];

type Problem = { kind: "changed" | "expired" | "connection"; message: string; fixUrl?: string } | null;

/** /docs/c/[connection]/[ref]: a Notion page or database, or a Google Doc, rendered and edited in Docs. */
export function ConnectedPageScreen({ connectionId, pageRef }: { connectionId: string; pageRef: string }) {
  const { org } = useActiveOrg();
  const pathname = usePathname() ?? "/docs";
  const detail = useDocsConnectedPage(connectionId, pageRef, org?.id);
  if (detail.isPending) {
    return (
      <div className="mx-auto flex w-full max-w-prose flex-col gap-4 px-gutter py-section" aria-busy aria-label="Loading page">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
      </div>
    );
  }
  if (detail.isError) {
    const e = detail.error;
    const fix = (e.details?.["fix_url"] as string | undefined) ?? (isConnectionErrorCode(e.code) ? connectionFixPath(e.details?.["capability"] as string | undefined) : undefined);
    return (
      <div className="flex flex-1 items-center justify-center px-gutter py-section">
        {e.status === 404 ? (
          <EmptyState
            icon={<Lock />}
            title="This page isn't available to this account"
            info={errorMessage(e)}
            action={
              <Button asChild variant="secondary">
                <Link href="/docs">Go to docs</Link>
              </Button>
            }
          />
        ) : fix ? (
          <EmptyState
            icon={<Lock />}
            title={errorMessage(e)}
            action={
              e.code === "connection_blocked" ? undefined : (
                <Button asChild>
                  <Link href={withReturnTo(fix, pathname)}>{e.code === "connection_scope_missing" ? "Allow access" : "Reconnect"}</Link>
                </Button>
              )
            }
          />
        ) : (
          <ErrorState title={errorMessage(e, "Couldn't load this page.")} onRetry={() => void detail.refetch()} />
        )}
      </div>
    );
  }
  return <Loaded key={detail.data.session_id} detail={detail.data} orgId={org?.id ?? detail.data.page.org_id} onReload={() => void detail.refetch()} reloading={detail.isFetching} />;
}

function Title({ detail, editable }: { detail: DocsConnectedPageDetail; editable: boolean }) {
  const rename = useDocsConnectedRename(detail.connection_id, detail.ref);
  const [value, setValue] = useState(detail.page.title);
  const ref = useRef<HTMLTextAreaElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [value]);
  const platform = docsPlatformName(detail.source.provider);
  const save = (v: string) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    if (v.trim() && v !== detail.page.title) rename.mutate(v, { onError: (e) => toast.error(errorMessage(e, `Couldn't rename it in ${platform}.`)) });
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
      onBlur={() => save(value)}
      onChange={(e) => {
        const v = e.target.value.replace(/\n/g, " ");
        setValue(v);
        if (timer.current) clearTimeout(timer.current);
        timer.current = setTimeout(() => save(v), 900);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          save(value);
        }
      }}
    />
  );
}

/** A connected database row's values, edited on the platform. */
function RowValues({ detail, editable }: { detail: DocsConnectedPageDetail; editable: boolean }) {
  const db = detail.database!;
  const setRow = useDocsConnectedSetRow(detail.connection_id, detail.database_ref ?? "");
  const [values, setValues] = useState<Record<string, DocsValue>>(detail.page.props);
  const [editing, setEditing] = useState<{ id: string; anchor: HTMLElement } | null>(null);
  const platform = docsPlatformName(detail.source.provider);
  const save = (propId: string, v: DocsValue) => {
    const before = values[propId];
    setValues((s) => ({ ...s, [propId]: v }));
    setRow.mutate(
      { rowRef: detail.ref, rowId: detail.page.id, values: { [propId]: v } },
      {
        onError: (e) => {
          setValues((s) => ({ ...s, [propId]: before ?? null }));
          toast.error(errorMessage(e, `Couldn't save that value in ${platform}.`));
        },
      },
    );
  };
  return (
    <div className="flex flex-col gap-2 border-b border-border pb-4">
      {detail.database_ref ? (
        <Link href={docsConnectedHref(detail.connection_id, detail.database_ref)} className="w-fit type-caption text-fg-muted hover:text-fg hover:underline">
          In {db.icon ? `${db.icon} ` : ""}
          {docsTitle(db.title)}
        </Link>
      ) : null}
      <dl className="flex flex-col gap-0.5">
        {db.schema
          .filter((p) => p.type !== "title")
          .map((p) => {
            const Icon = PROPERTY_ICON[p.type];
            const can = editable && !p.read_only;
            return (
              <div key={p.id} className="grid grid-cols-[minmax(7rem,10rem)_1fr] items-start gap-2">
                <dt className="flex min-h-control items-center gap-2 px-2 type-label text-fg-muted">
                  <Icon aria-hidden className="size-4 shrink-0" />
                  <span className="truncate">{p.name}</span>
                </dt>
                <dd>
                  <button
                    type="button"
                    disabled={!can}
                    aria-label={`${p.name}${can ? ", edit" : ""}`}
                    {...(p.type === "checkbox" ? { role: "checkbox", "aria-checked": values[p.id] === true } : {})}
                    className="flex min-h-control w-full min-w-0 items-center rounded-sm px-2 py-1 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-default disabled:hover:bg-transparent"
                    onClick={(e) => (p.type === "checkbox" ? save(p.id, values[p.id] !== true) : setEditing({ id: p.id, anchor: e.currentTarget }))}
                  >
                    <PropertyDisplay prop={p} value={values[p.id]} refs={detail.refs} />
                  </button>
                </dd>
              </div>
            );
          })}
      </dl>
      {editing ? (
        <Floating open anchor={editing.anchor} onClose={() => setEditing(null)} autoFocus label="Edit value">
          <PropertyEditor prop={db.schema.find((p) => p.id === editing.id)!} value={values[editing.id]} orgId={detail.page.org_id} onChange={(v) => save(editing.id, v)} onDone={() => setEditing(null)} />
        </Floating>
      ) : null}
    </div>
  );
}

function Loaded({ detail, orgId, onReload, reloading }: { detail: DocsConnectedPageDetail; orgId: string; onReload: () => void; reloading: boolean }) {
  const router = useRouter();
  const pathname = usePathname() ?? "/docs";
  const d = useEndpoints(docsEndpoints);
  const page = detail.page;
  const provider = detail.source.provider;
  const platform = docsPlatformName(provider);
  const editable = detail.can_edit;
  const [problem, setProblem] = useState<Problem>(null);
  const { session, snapshot } = useDocsConnectedPageSession<EditorSelection>(page.kind === "page" ? detail : undefined, orgId, (message, code) => {
    if (code === "changed_on_platform") {
      setProblem({ kind: "changed", message: `Changed in ${platform} while you were editing. Your last change wasn't saved; the page now shows the latest version.` });
      toast.error(`Changed in ${platform} while you were editing. Your last change wasn't saved.`);
    }
    else if (code === "connected_session_expired") setProblem({ kind: "expired", message });
    else if (isConnectionErrorCode(code)) setProblem({ kind: "connection", message, fixUrl: connectionFixPath(provider === "google" ? "docs.google.read" : "docs.notion.read", pathname) });
    else toast.error(message);
  });
  const wide = useMediaQuery("(min-width: 1280px)");
  const [comments, setComments] = useState<{ open: boolean; quote: string | null }>({ open: false, quote: null });
  const [linking, setLinking] = useState(false);
  const copy = useDocsConnectedCopy(detail.connection_id, detail.ref);
  const readOnly = useMemo(() => new Set(detail.read_only_blocks), [detail.read_only_blocks]);
  const connected: EditorConnected = useMemo(
    () => ({ types: provider === "google" ? GOOGLE_TYPES : NOTION_TYPES, readOnly, platform, url: detail.url, dates: provider === "notion" }),
    [provider, readOnly, platform, detail.url],
  );

  useEffect(() => {
    if (!session) return undefined;
    const onUnload = (e: BeforeUnloadEvent) => {
      session.flushNow();
      if (session.hasUnsaved()) e.preventDefault();
    };
    window.addEventListener("beforeunload", onUnload);
    return () => window.removeEventListener("beforeunload", onUnload);
  }, [session]);

  const onErr = (fallback: string) => (e: unknown) => toast.error(errorMessage(e, fallback));
  const title = docsTitle(page.title);
  const openPlatform = () => void window.open(detail.url, "_blank", "noopener,noreferrer");
  const actions: ResourceMenuAction[] = [
    { id: "platform", label: `Open in ${platform}`, icon: ExternalLink, onSelect: openPlatform },
    // The app's Share and Publish don't apply here: sharing is the platform's.
    { id: "sharing", label: `Manage sharing in ${platform}`, icon: Users, onSelect: openPlatform },
    ...(page.kind === "page"
      ? [
          {
            id: "copy",
            label: "Copy into Docs",
            icon: Copy,
            onSelect: () =>
              copy.mutate(
                { org_id: orgId },
                {
                  onSuccess: (p) => toast.success(`Copied into Docs as “${docsTitle(p.title)}”.`, { action: { label: "Open", onClick: () => router.push(`/docs/${p.id}`) } }),
                  onError: onErr("Couldn't copy it into Docs."),
                },
              ),
          },
          { id: "link", label: "Add to a Docs page…", icon: FilePlus2, onSelect: () => setLinking(true) },
        ]
      : []),
    {
      id: "copy-link",
      label: "Copy link",
      icon: Link2,
      onSelect: () => void navigator.clipboard?.writeText(`${window.location.origin}${docsConnectedHref(detail.connection_id, detail.ref)}`).then(() => toast.success("Link copied."), () => toast.error("Couldn't copy the link.")),
    },
    { id: "reload", label: "Reload", icon: RefreshCw, onSelect: onReload },
  ];

  const panel = (header: boolean) => (
    <ConnectedComments
      header={header}
      connectionId={detail.connection_id}
      pageRef={detail.ref}
      platform={platform}
      canComment={detail.connection.can_write}
      pendingQuote={comments.quote}
      onClearQuote={() => setComments((c) => ({ ...c, quote: null }))}
      onClose={() => setComments({ open: false, quote: null })}
    />
  );

  return (
    <div className="flex min-h-0 flex-1">
      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto">
        <header className="sticky top-0 z-20 flex min-h-12 items-center gap-2 border-b border-border bg-canvas px-gutter py-1">
          <Link href="/docs" aria-label="All pages" className="flex size-control-sm shrink-0 items-center justify-center rounded-md text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring lg:hidden [&_svg]:size-4">
            <ArrowLeft aria-hidden />
          </Link>
          <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden type-label text-fg-muted">
            <SourceBadge source={detail.source} showAccount className="hidden shrink-0 sm:inline-flex" />
            <SourceBadge source={detail.source} iconOnly className="shrink-0 sm:hidden" />
            <span aria-hidden className="px-0.5">
              /
            </span>
            <span className="truncate px-1 text-fg" aria-current="page">
              {page.icon ? `${page.icon} ` : ""}
              {title}
            </span>
          </nav>
          {editable && page.kind === "page" ? <SaveState state={snapshot.saveState} error={snapshot.error} /> : null}
          {!editable ? <span className="hidden type-caption text-fg-muted sm:inline">View only</span> : null}
          {page.kind === "page" ? (
            <Button variant="ghost" size="sm" leftIcon={<MessageSquare />} aria-label="Comments" onClick={() => setComments((c) => ({ ...c, open: !c.open }))}>
              <span className="hidden sm:inline">Comments</span>
            </Button>
          ) : null}
          <ResourceMenu feature={`connectors:${detail.connection_id}`} kind="docs.external" id={`${detail.connection_id}:${detail.ref}`} title={title} actions={actions} />
        </header>
        <div className={cn("mx-auto flex w-full flex-col gap-4 px-gutter pt-8 pb-24 sm:px-16", page.kind === "database" ? "max-w-content" : "max-w-3xl")}>
          {problem ? (
            <Banner
              tone={problem.kind === "changed" ? "warning" : "danger"}
              action={
                problem.kind === "connection" && problem.fixUrl ? (
                  <Button asChild size="sm" variant="secondary">
                    <Link href={problem.fixUrl}>Reconnect</Link>
                  </Button>
                ) : (
                  <Button size="sm" variant="secondary" loading={reloading} onClick={onReload}>
                    Reload
                  </Button>
                )
              }
            >
              {problem.message}
            </Banner>
          ) : null}
          {!detail.connection.can_write ? (
            <Banner
              tone="info"
              action={
                detail.connection.write_url ? (
                  <Button asChild size="sm" variant="secondary">
                    <Link href={withReturnTo(detail.connection.write_url, pathname)}>Allow changes</Link>
                  </Button>
                ) : undefined
              }
            >
              You can read this here. To edit it from Docs, allow changes to {platform}.
            </Banner>
          ) : !editable ? (
            <Banner tone="info">This {platform} account can view this page but not edit it.</Banner>
          ) : null}
          {detail.partial ? (
            <Banner tone="info" action={<Button size="sm" variant="secondary" onClick={openPlatform}>Open in {platform}</Button>}>
              This page is long: Docs shows the first part.
            </Banner>
          ) : null}
          <div className="flex flex-col gap-2">
            {page.icon ? <span className="w-fit type-hero">{page.icon}</span> : null}
            <Title detail={detail} editable={editable && page.kind === "page"} />
          </div>
          {page.is_row && detail.database ? <RowValues detail={detail} editable={editable} /> : null}
          {page.kind === "database" ? (
            <ConnectedDatabaseView connectionId={detail.connection_id} dbRef={detail.ref} db={page} views={detail.views} orgId={orgId} editable={detail.connection.can_write} platform={platform} />
          ) : session ? (
            <Editor
              pageId={page.id}
              orgId={orgId}
              session={session}
              snap={snapshot}
              refs={detail.refs}
              editable={editable}
              canComment={detail.connection.can_write}
              viewers={[]}
              commentCounts={{}}
              threads={[]}
              activeThread={null}
              connected={connected}
              onFocusBlock={() => {}}
              onOpenComments={() => setComments((c) => ({ ...c, open: true }))}
              onComment={(target) => setComments({ open: true, quote: target.quote ?? null })}
              onOpenPage={(id) => router.push(docsConnectedHref(detail.connection_id, id))}
              onCreateSubpage={async () => {
                toast.error(`Add subpages in ${platform}.`);
                return null;
              }}
              onCreateDatabase={async () => {
                toast.error(`Add databases in ${platform}.`);
                return null;
              }}
              onUpload={() => Promise.reject(new Error(`Add images and files in ${platform}.`))}
              onMoveToPage={() => toast.error(`Move blocks in ${platform}.`)}
            />
          ) : (
            <Skeleton className="h-24 w-full" />
          )}
        </div>
      </div>
      {comments.open && wide ? <aside className="flex w-80 shrink-0 flex-col border-l border-border bg-surface">{panel(true)}</aside> : null}
      {!wide ? (
        <Sheet open={comments.open} onOpenChange={(o) => !o && setComments({ open: false, quote: null })} title="Comments">
          {comments.open ? panel(false) : null}
        </Sheet>
      ) : null}
      <PagePicker
        open={linking}
        onOpenChange={setLinking}
        orgId={orgId}
        title={`Add “${title}” to a page`}
        exclude={[]}
        onPick={async (target) => {
          setLinking(false);
          if (!target) return;
          try {
            const dest = await d.page(target);
            if (dest.page.my_level !== "edit" && dest.page.my_level !== "manage") throw new Error("You can't edit that page.");
            const roots = dest.blocks.filter((b) => !b.parent_id).sort((a, b) => (a.position < b.position ? -1 : 1));
            await d.applyOps(target, generateId(), [
              {
                op: "create",
                id: generateId(),
                type: "connected_page",
                parent_id: null,
                position: docsKeyBetween(roots[roots.length - 1]?.position ?? null, null),
                props: { provider, account_id: detail.source.type === "connected" ? accountIdOf(detail) : "", ref: detail.ref, title: page.title, url: detail.url },
              },
            ]);
            toast.success(`Added to “${docsTitle(dest.page.title)}”. Colleagues see it with their own ${platform} access.`, { action: { label: "Open", onClick: () => router.push(`/docs/${target}`) } });
          } catch (e) {
            toast.error(errorMessage(e, "Couldn't add it to that page."));
          }
        }}
      />
    </div>
  );
}

/** The platform account (a Notion workspace) the page belongs to, so viewers' own connections to it can open it. */
function accountIdOf(detail: DocsConnectedPageDetail): string {
  return detail.connection.account_id ?? "";
}
