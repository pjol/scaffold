"use client";

import { docsTimeAgo, docsTitle, errorMessage, useActiveOrg, useDocsCreatePage, useDocsRecent } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { FileText, Plus, Table2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";

/** /docs beside the tree at ≥ 1024 px: the pages you opened lately. */
export function DocsHome() {
  const { org } = useActiveOrg();
  const recent = useDocsRecent(org?.id);
  const create = useDocsCreatePage(org?.id ?? "");
  const router = useRouter();
  const newPage = () => create.mutate({}, { onSuccess: (p) => router.push(`/docs/${p.id}`), onError: (e) => toast.error(errorMessage(e, "Couldn't create the page.")) });
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-section px-gutter py-section">
        <div className="flex items-center justify-between gap-3">
          <h1 className="type-headline text-fg">Recent</h1>
          <Button leftIcon={<Plus />} loading={create.isPending} onClick={newPage}>
            New page
          </Button>
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
              <Button variant="secondary" onClick={newPage}>
                Write a page
              </Button>
            }
          />
        ) : (
          <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
            {recent.data.map((p) => (
              <li key={p.id}>
                <Link href={`/docs/${p.id}`} className="flex min-h-14 items-center gap-3 px-4 hover:bg-sunken focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring">
                  <span aria-hidden className="flex size-6 items-center justify-center type-body">
                    {p.icon ?? (p.kind === "database" ? <Table2 className="size-4 text-fg-muted" /> : <FileText className="size-4 text-fg-muted" />)}
                  </span>
                  <span className="min-w-0 flex-1 truncate type-body text-fg">{docsTitle(p.title)}</span>
                  <span className="shrink-0 type-caption text-fg-muted">{docsTimeAgo(p.updated_at)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
