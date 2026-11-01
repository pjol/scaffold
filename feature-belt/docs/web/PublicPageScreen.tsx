"use client";

import { docsTitle, useAuthSession, useDocsFileUrl, useDocsPublicPage } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Skeleton, cn } from "@scaffoldapp/ui-web";
import { FileText, Flag, Link2Off } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { ReportDialog } from "@/features/moderation";
import { signInHref } from "@/lib/redirect";
import { BlockView } from "./BlockView";
import { DatabaseView } from "./database/DatabaseView";
import { PropertyDisplay } from "./database/PropertyValue";
import { COVER_CLASS } from "./Dialogs";

/** /p/<token>[/<page id>]: a published page, read-only, without an account. */
export function PublicPageScreen({ token, pageId }: { token: string; pageId?: string }) {
  const q = useDocsPublicPage(token, pageId ?? null);
  const auth = useAuthSession();
  const pathname = usePathname() ?? "/";
  const [report, setReport] = useState(false);
  const cover = useDocsFileUrl(q.data?.page.cover?.file_id, token);
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
        {q.error.status === 404 ? (
          <EmptyState icon={<Link2Off />} title="This link doesn't work anymore" />
        ) : (
          <ErrorState title="Couldn't load this page." onRetry={() => void q.refetch()} />
        )}
      </div>
    );
  }
  const d = q.data;
  const page = d.page;
  const href = (id: string) => (d.refs.pages[id]?.title ? `/p/${token}/${id}` : null);
  return (
    <article className="flex flex-col">
      {page.cover ? (
        <div className={cn("h-36 w-full overflow-hidden sm:h-48", page.cover.color ? COVER_CLASS[page.cover.color] : "bg-sunken")}>
          {cover.data?.url ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={cover.data.url} alt="" className="size-full object-cover" style={{ objectPosition: `center ${page.cover.position}%` }} />
          ) : null}
        </div>
      ) : null}
      <div className={cn("mx-auto flex w-full flex-col gap-4 px-gutter py-section", page.full_width || page.kind === "database" ? "max-w-content" : "max-w-3xl")}>
        {d.path.length || pageId ? (
          <nav aria-label="Breadcrumb" className="flex flex-wrap items-center gap-1 type-label text-fg-muted">
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
        ) : null}
        {page.icon ? <span aria-hidden className="type-hero">{page.icon}</span> : null}
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
        {d.children.length ? (
          <section aria-label="Pages inside" className="flex flex-col gap-1 border-t border-border pt-4">
            {d.children.map((c) => (
              <Link key={c.id} href={`/p/${token}/${c.id}`} className="flex min-h-control items-center gap-2 rounded-sm px-1 type-body text-fg hover:bg-sunken">
                <span aria-hidden>{c.icon ?? <FileText className="size-4 text-fg-muted" />}</span>
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
      <ReportDialog open={report} onOpenChange={setReport} targetKind="docs.page" targetId={page.id} noun="page" />
    </article>
  );
}
