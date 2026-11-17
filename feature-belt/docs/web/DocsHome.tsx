"use client";

import { docsTimeAgo, docsTitle, useActiveOrg, useDocsRecent, useDocsTemplates } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Skeleton, SkeletonRow } from "@scaffoldapp/ui-web";
import { FileText, LayoutTemplate, Plus, Upload } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { NewPageDialog } from "./NewPageDialog";
import { PageIcon } from "./PageIcon";

/** /docs beside the tree at ≥ 1024 px: the pages you opened lately, and a quick start from templates. */
export function DocsHome() {
  const { org } = useActiveOrg();
  const recent = useDocsRecent(org?.id);
  const templates = useDocsTemplates(org?.id);
  const [dialog, setDialog] = useState<null | "empty" | "templates" | "import">(null);
  const quick = [...(templates.data?.workspace ?? []), ...(templates.data?.builtin ?? [])].slice(0, 6);
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-section px-gutter py-section">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="type-headline text-fg">Recent</h1>
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" leftIcon={<Upload />} onClick={() => setDialog("import")}>
              Import
            </Button>
            <Button leftIcon={<Plus />} onClick={() => setDialog("empty")}>
              New page
            </Button>
          </div>
        </div>
        {recent.isPending ? (
          <div className="flex flex-col gap-2">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </div>
        ) : recent.isError ? (
          <ErrorState title="Couldn't load recent pages." onRetry={() => void recent.refetch()} />
        ) : recent.data.length === 0 ? (
          <EmptyState
            icon={<FileText />}
            title="No pages opened yet"
            action={
              <Button variant="secondary" onClick={() => setDialog("empty")}>
                Write a page
              </Button>
            }
          />
        ) : (
          <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
            {recent.data.map((p) => (
              <li key={p.id}>
                <Link href={`/docs/${p.id}`} className="flex min-h-14 items-center gap-3 px-4 hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
                  <PageIcon icon={p.icon} custom={p.custom_icon} kind={p.kind} size="sm" />
                  <span className="min-w-0 flex-1 truncate type-body text-fg">{docsTitle(p.title)}</span>
                  <span className="shrink-0 type-caption text-fg-muted">{docsTimeAgo(p.updated_at)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
        <section className="flex flex-col gap-3" aria-label="Start from a template">
          <div className="flex items-center justify-between gap-3">
            <h2 className="type-title text-fg">Start from a template</h2>
            <Button variant="ghost" size="sm" leftIcon={<LayoutTemplate />} onClick={() => setDialog("templates")}>
              All templates
            </Button>
          </div>
          {templates.isPending ? (
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
              {Array.from({ length: 3 }, (_, i) => (
                <Skeleton key={i} className="h-16" />
              ))}
            </div>
          ) : templates.isError ? (
            <ErrorState title="Couldn't load templates." onRetry={() => void templates.refetch()} />
          ) : (
            <ul className="grid grid-cols-1 gap-2 sm:grid-cols-3">
              {quick.map((t) => (
                <li key={t.id}>
                  <button type="button" onClick={() => setDialog("templates")} className="flex w-full items-center gap-3 rounded-md border border-border p-3 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                    <PageIcon icon={t.icon} custom={t.custom_icon} kind={t.kind} size="md" />
                    <span className="min-w-0 truncate type-label font-semibold text-fg">{t.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
      {org && dialog ? <NewPageDialog key={dialog} open onOpenChange={(o) => !o && setDialog(null)} orgId={org.id} defaultTab={dialog} /> : null}
    </div>
  );
}
