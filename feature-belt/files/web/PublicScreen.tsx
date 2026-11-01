"use client";

import { errorMessage, filesFormatSize, hasErrorCode, useAuthSession, useFilesPublic, useFilesPublicChildren, useFilesPublicUrl, type FilesPublicItem, type FilesPublicView } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Skeleton, SkeletonGroup, SkeletonRow, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronRight, Download, Flag, FolderOpen, Link2Off } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ReportDialog } from "@/features/moderation";
import { formatDate, formatRelative } from "@/lib/format";
import { signInHref } from "@/lib/redirect";
import { useUrlState } from "@/lib/url-state";
import { startDownload } from "./actions";
import { filesIconTone, filesItemIcon } from "./icons";
import { FilesPreview } from "./Preview";

/**
 * /f/[token]: what a public link opens, signed out. A file shows its preview and Download;
 * a folder can be browsed (`?item=` keeps the place). Visitors can report it (signed in).
 */
export function FilesPublicScreen({ token }: { token: string }) {
  const [itemId, setItemId] = useUrlState("item", "");
  const view = useFilesPublic(token, itemId || null);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      {view.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-96 w-full rounded-lg" />
        </div>
      ) : view.isError ? (
        hasErrorCode(view.error, "link_not_found") || hasErrorCode(view.error, "public_disabled") || hasErrorCode(view.error, "not_found") ? (
          <EmptyState icon={<Link2Off />} title={errorMessage(view.error, "This link doesn't work anymore. Ask for a new one.")} />
        ) : (
          <ErrorState title="Couldn't open this link" info={errorMessage(view.error, "")} onRetry={() => void view.refetch()} />
        )
      ) : (
        <PublicContent token={token} view={view.data} onOpen={(id) => setItemId(id === view.data.root_id ? null : id)} />
      )}
    </div>
  );
}

function PublicContent({ token, view, onOpen }: { token: string; view: FilesPublicView; onOpen: (id: string) => void }) {
  const item = view.item;
  const url = useFilesPublicUrl(token);
  const download = () =>
    url.mutate({ itemId: item.id, disposition: "attachment" }, { onSuccess: (u) => startDownload(u.url), onError: (e) => toast.error(errorMessage(e, "Couldn't start the download.")) });
  return (
    <>
      <header className="flex flex-col gap-2">
        {view.path.length > 0 ? (
          <nav aria-label="Breadcrumb">
            <ol className="flex flex-wrap items-center gap-1 type-label text-fg-muted">
              {view.path.map((c, i) => (
                <li key={c.id} className="flex items-center gap-1">
                  {i > 0 ? <ChevronRight className="size-3.5" aria-hidden /> : null}
                  <button type="button" className="max-w-48 truncate rounded-xs px-1 py-1 hover:text-fg hover:underline" onClick={() => onOpen(c.id)}>
                    {c.name}
                  </button>
                </li>
              ))}
            </ol>
          </nav>
        ) : null}
        <div className="flex min-h-control-lg flex-wrap items-center gap-3">
          <h1 className="w-full min-w-0 truncate type-title text-fg sm:w-auto sm:flex-1" title={item.name}>
            {item.name}
          </h1>
          {view.expires_at ? <Badge title={formatDate(view.expires_at)}>Link expires {formatRelative(view.expires_at)}</Badge> : null}
          {item.type === "file" ? (
            <Button leftIcon={<Download />} onClick={download} loading={url.isPending}>
              Download
            </Button>
          ) : null}
          <ReportItem itemId={item.id} name={item.name} />
        </div>
      </header>
      {item.type === "folder" ? <PublicFolder token={token} folder={item} onOpen={onOpen} /> : <FilesPreview item={item} source={{ kind: "public", token }} onDownload={download} />}
    </>
  );
}

function PublicFolder({ token, folder, onOpen }: { token: string; folder: FilesPublicItem; onOpen: (id: string) => void }) {
  const children = useFilesPublicChildren(token, folder.id);
  const rows = children.data?.items ?? [];
  if (children.isPending) {
    return (
      <SkeletonGroup label="Loading the folder" className="flex flex-col">
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    );
  }
  if (children.isError) return <ErrorState title="Couldn't load the folder" onRetry={() => void children.refetch()} />;
  if (rows.length === 0) return <EmptyState icon={<FolderOpen />} title="This folder is empty" />;
  return (
    <div className="flex flex-col">
      <ul className="flex flex-col rounded-lg border border-border bg-surface p-1">
        {rows.map((it) => {
          const Icon = filesItemIcon(it);
          return (
            <li key={it.id}>
              <button type="button" onClick={() => onOpen(it.id)} className="flex min-h-tap-lg w-full items-center gap-3 rounded-md px-3 text-left hover:bg-sunken">
                <span className="flex size-8 shrink-0 items-center justify-center">
                  {it.thumb_url ? (
                    // eslint-disable-next-line @next/next/no-img-element -- presigned, short-lived URL
                    <img src={it.thumb_url} alt="" className="size-8 rounded-sm object-cover" loading="lazy" />
                  ) : (
                    <Icon className={cn("size-6", filesIconTone(it))} aria-hidden />
                  )}
                </span>
                <span className="min-w-0 flex-1 truncate type-body font-medium text-fg">{it.name}</span>
                <span className="hidden shrink-0 type-label text-fg-muted sm:block">{formatRelative(it.updated_at)}</span>
                <span className="w-20 shrink-0 text-right type-label tabular-nums text-fg-muted">{it.type === "file" ? filesFormatSize(it.size) : ""}</span>
              </button>
            </li>
          );
        })}
      </ul>
      <LoadMore hasNextPage={!!children.hasNextPage} isFetchingNextPage={children.isFetchingNextPage} fetchNextPage={children.fetchNextPage} />
    </div>
  );
}

function ReportItem({ itemId, name }: { itemId: string; name: string }) {
  const auth = useAuthSession();
  const pathname = usePathname() ?? "/";
  const [open, setOpen] = useState(false);
  if (!auth.isLoaded) return null;
  if (!auth.isSignedIn) {
    return (
      <Button asChild variant="ghost" leftIcon={<Flag />}>
        <Link href={signInHref(`${pathname}${typeof window === "undefined" ? "" : window.location.search}`)}>Report</Link>
      </Button>
    );
  }
  return (
    <>
      <Button variant="ghost" leftIcon={<Flag />} onClick={() => setOpen(true)}>
        Report
      </Button>
      <ReportDialog open={open} onOpenChange={setOpen} targetKind="files.item" targetId={itemId} noun={name ? "file" : "item"} />
    </>
  );
}
