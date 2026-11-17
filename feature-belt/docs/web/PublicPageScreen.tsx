"use client";

import { docsTitle, errorMessage, orgsNoun, useActiveOrg, useAuthSession, useDocsDuplicatePublic, useDocsPublicPage, type DocsPublishSettings } from "@scaffoldapp/shared";
import { Button, Dialog, EmptyState, ErrorState, Select, Skeleton, cn, toast } from "@scaffoldapp/ui-web";
import { Copy, Flag, Link2Off } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ReportDialog } from "@/features/moderation";
import { signInHref } from "@/lib/redirect";
import { BlockView } from "./BlockView";
import { DatabaseView } from "./database/DatabaseView";
import { PropertyDisplay } from "./database/PropertyValue";
import { CoverArt } from "./Dialogs";
import { FONT_CLASS, SMALL_TEXT_CLASS } from "./pageStyle";
import { Outline } from "./Outline";
import { PageIcon } from "./PageIcon";

/** The publisher's theme, applied to the whole document while the page is open. */
function usePublishedTheme(theme: DocsPublishSettings["theme"] | undefined) {
  useEffect(() => {
    if (!theme || theme === "system") return undefined;
    const html = document.documentElement;
    const prev = html.getAttribute("data-theme");
    html.setAttribute("data-theme", theme);
    return () => {
      if (prev === null) html.removeAttribute("data-theme");
      else html.setAttribute("data-theme", prev);
    };
  }, [theme]);
}

/** Copy the published page into one of the visitor's workspaces (signed in, when the publisher allows it). */
function DuplicateButton({ token, pageId }: { token: string; pageId: string | null }) {
  const { org, orgs } = useActiveOrg();
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<string>("");
  const dup = useDocsDuplicatePublic(token);
  const router = useRouter();
  const chosen = target || org?.id || orgs[0]?.id || "";
  return (
    <>
      <Button variant="secondary" size="sm" leftIcon={<Copy />} onClick={() => setOpen(true)} disabled={!orgs.length}>
        Duplicate
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title="Duplicate into your workspace"
        info="A private copy of this page and its published subpages. You can edit and share it like any page."
        footer={
          <Button
            loading={dup.isPending}
            disabled={!chosen}
            onClick={() =>
              dup.mutate(
                { org_id: chosen, ...(pageId ? { page_id: pageId } : {}) },
                { onSuccess: (p) => router.push(`/docs/${p.id}`), onError: (e) => toast.error(errorMessage(e, "Couldn't copy the page.")) },
              )
            }
          >
            Duplicate
          </Button>
        }
      >
        {orgs.length > 1 ? (
          <Select value={chosen} onValueChange={setTarget} aria-label={`Choose a ${orgsNoun()}`} options={orgs.map((o) => ({ value: o.id, label: o.name }))} />
        ) : (
          <p className="type-body text-fg">Into {orgs[0]?.name}.</p>
        )}
      </Dialog>
    </>
  );
}

/** /p/<token>[/<page id>]: a published page, read-only, without an account. */
export function PublicPageScreen({ token, pageId }: { token: string; pageId?: string }) {
  const q = useDocsPublicPage(token, pageId ?? null);
  const auth = useAuthSession();
  const pathname = usePathname() ?? "/";
  const [report, setReport] = useState(false);
  usePublishedTheme(q.data?.settings?.theme);
  if (q.isPending) {
    return (
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 px-gutter py-section" aria-busy aria-label="Loading page">
        <Skeleton className="h-12 w-2/3" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
      </div>
    );
  }
  if (q.isError) {
    return (
      <div className="flex flex-1 items-center justify-center px-gutter py-section">
        {q.error.status === 404 ? <EmptyState icon={<Link2Off />} title="This link doesn't work anymore" /> : <ErrorState title="Couldn't load this page." onRetry={() => void q.refetch()} />}
      </div>
    );
  }
  const d = q.data;
  const page = d.page;
  const settings = d.settings;
  const href = (id: string) => (d.refs.pages[id]?.title ? `/p/${token}/${id}` : null);
  const outline = settings?.show_outline !== false && page.kind === "page";
  const linked = new Set(d.blocks.map((b) => b.props.page_id ?? b.props.database_id).filter(Boolean));
  const loose = d.children.filter((c) => !linked.has(c.id));
  return (
    <article className="flex flex-col">
      {page.cover ? <CoverArt cover={page.cover} token={token} className="h-36 shrink-0 sm:h-48" /> : null}
      <div className="flex justify-center gap-8">
        <div
          className={cn(
            "flex w-full min-w-0 flex-col gap-4 px-gutter py-section",
            page.full_width || page.kind === "database" ? "max-w-content" : "max-w-3xl",
            FONT_CLASS[page.font ?? "default"],
            page.small_text && SMALL_TEXT_CLASS,
          )}
        >
          <div className="flex flex-wrap items-center gap-2">
            {d.path.length || pageId ? (
              <nav aria-label="Breadcrumb" className="flex min-w-0 flex-1 flex-wrap items-center gap-1 type-label text-fg-muted">
                {pageId && d.root_id !== page.id ? (
                  <>
                    {[{ id: d.root_id, title: "", icon: null }, ...d.path.filter((c) => c.id !== d.root_id)].map((c, i) => (
                      <span key={c.id} className="flex items-center gap-1">
                        <Link href={i === 0 ? `/p/${token}` : `/p/${token}/${c.id}`} className="rounded-sm px-1 hover:bg-sunken hover:text-fg">
                          {i === 0 ? "Start" : `${c.icon ? `${c.icon} ` : ""}${c.title}`}
                        </Link>
                        <span aria-hidden>/</span>
                      </span>
                    ))}
                  </>
                ) : null}
              </nav>
            ) : (
              <span className="flex-1" />
            )}
            {settings?.allow_duplicate && !page.is_row ? (
              auth.isSignedIn ? (
                <DuplicateButton token={token} pageId={pageId ?? null} />
              ) : (
                <Button asChild variant="secondary" size="sm" leftIcon={<Copy />}>
                  <Link href={signInHref(pathname)}>Sign in to duplicate</Link>
                </Button>
              )
            ) : null}
          </div>
          <PageIcon icon={page.icon} custom={page.custom_icon} kind={page.kind} size="lg" token={token} fallback={false} />
          <h1 className="type-display break-words text-fg [overflow-wrap:anywhere]">{docsTitle(page.title)}</h1>
          {page.is_row && d.database ? (
            <dl className="flex flex-col gap-1 border-b border-border pb-4">
              {d.database.schema
                .filter((p) => p.type !== "title")
                .map((p) => (
                  <div key={p.id} className="grid grid-cols-[minmax(7rem,10rem)_1fr] items-center gap-2">
                    <dt className="type-label text-fg-muted">{p.name}</dt>
                    <dd className="min-h-8 py-1">
                      <PropertyDisplay prop={p} value={page.props[p.id]} refs={d.refs} />
                    </dd>
                  </div>
                ))}
            </dl>
          ) : null}
          {page.kind === "database" ? (
            <DatabaseView db={page} views={d.views} orgId={page.org_id} editable={false} token={token} />
          ) : (
            <BlockView blocks={d.blocks} refs={d.refs} token={token} pageHref={href} renderDatabase={() => <p className="type-label text-fg-muted">Open the database from the list below.</p>} />
          )}
          {loose.length ? (
            <section aria-label="Pages inside" className="flex flex-col gap-1 border-t border-border pt-4">
              {loose.map((c) => (
                <Link key={c.id} href={`/p/${token}/${c.id}`} className="flex min-h-control items-center gap-2 rounded-sm px-1 type-body text-fg hover:bg-sunken">
                  <PageIcon icon={c.icon} custom={c.custom_icon} kind={c.kind} size="sm" token={token} />
                  <span className="underline decoration-border-strong underline-offset-4">{docsTitle(c.title)}</span>
                </Link>
              ))}
            </section>
          ) : null}
          <div className="flex justify-end border-t border-border pt-4">
            {auth.isSignedIn ? (
              <Button variant="ghost" size="sm" leftIcon={<Flag />} onClick={() => setReport(true)}>
                Report this page
              </Button>
            ) : (
              <Button asChild variant="ghost" size="sm" leftIcon={<Flag />}>
                <Link href={signInHref(pathname)}>Sign in to report this page</Link>
              </Button>
            )}
          </div>
        </div>
        {outline ? (
          <aside className="sticky top-8 hidden h-fit shrink-0 pt-16 pr-4 xl:block">
            <Outline blocks={d.blocks} />
          </aside>
        ) : null}
      </div>
      <ReportDialog open={report} onOpenChange={setReport} targetKind="docs.page" targetId={page.id} noun="page" />
    </article>
  );
}
