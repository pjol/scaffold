"use client";

import {
  errorMessage,
  filesFormatSize,
  hasErrorCode,
  useAuthSession,
  useFilesPublic,
  useFilesPublicChildren,
  useFilesPublicPassword,
  useFilesPublicUrl,
  type FilesPublicItem,
  type FilesPublicView,
} from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Field, Input, Skeleton, SkeletonGroup, SkeletonRow, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronRight, Download, Flag, FolderOpen, KeyRound, Link2Off } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState, type FormEvent } from "react";
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
  const [proof, setProof] = useState(() => readProof(token));
  const view = useFilesPublic(token, itemId || null, undefined, proof || undefined);

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      {view.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-96 w-full rounded-lg" />
        </div>
      ) : view.isError && hasErrorCode(view.error, "link_password_required") ? (
        <PasswordGate
          token={token}
          retry={!!proof}
          onProof={(u) => {
            saveProof(token, u);
            setProof(u);
          }}
        />
      ) : view.isError ? (
        hasErrorCode(view.error, "link_not_found") || hasErrorCode(view.error, "public_disabled") || hasErrorCode(view.error, "not_found") ? (
          <EmptyState icon={<Link2Off />} title={errorMessage(view.error, "This link doesn't work anymore. Ask for a new one.")} />
        ) : (
          <ErrorState title="Couldn't open this link" info={errorMessage(view.error, "")} onRetry={() => void view.refetch()} />
        )
      ) : (
        <PublicContent token={token} proof={proof} view={view.data} onOpen={(id) => setItemId(id === view.data.root_id ? null : id)} />
      )}
    </div>
  );
}

const proofKey = (token: string) => `files.link-proof.${token.slice(-12)}`;

/** The password proof for this tab (sessionStorage; gone when the tab closes). */
function readProof(token: string): string {
  try {
    return typeof window === "undefined" ? "" : (window.sessionStorage.getItem(proofKey(token)) ?? "");
  } catch {
    return "";
  }
}

function saveProof(token: string, proof: string) {
  try {
    window.sessionStorage.setItem(proofKey(token), proof);
  } catch {
    // Private mode: it lasts for this page only.
  }
}

/** The password form a protected link shows first. */
function PasswordGate({ token, retry, onProof }: { token: string; retry: boolean; onProof: (proof: string) => void }) {
  const m = useFilesPublicPassword(token);
  const [pw, setPw] = useState("");
  const [error, setError] = useState<string | null>(retry ? "The password changed. Enter the new one." : null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    m.mutate(pw, {
      onSuccess: (r) => onProof(r.proof),
      onError: (err) => setError(hasErrorCode(err, "rate_limited") ? "Too many tries. Wait a minute and try again." : errorMessage(err, "That password isn't right.")),
    });
  };
  return (
    <form onSubmit={submit} className="mx-auto flex w-full max-w-form flex-col gap-4 rounded-xl border border-border bg-surface p-card">
      <div className="flex items-center gap-3">
        <KeyRound className="size-6 text-fg-muted" aria-hidden />
        <h1 className="type-title-sm text-fg">This link needs a password</h1>
      </div>
      <Field label="Password" error={error ?? undefined}>
        <Input type="password" autoComplete="current-password" autoFocus value={pw} onChange={(e) => setPw(e.target.value)} />
      </Field>
      <Button type="submit" loading={m.isPending} disabled={!pw}>
        Open
      </Button>
    </form>
  );
}

function PublicContent({ token, proof, view, onOpen }: { token: string; proof: string; view: FilesPublicView; onOpen: (id: string) => void }) {
  const item = view.item;
  const url = useFilesPublicUrl(token, proof || undefined);
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
          {item.type === "file" && view.allow_download ? (
            <Button leftIcon={<Download />} onClick={download} loading={url.isPending}>
              Download
            </Button>
          ) : null}
          <ReportItem itemId={item.id} name={item.name} />
        </div>
      </header>
      {item.type === "folder" ? (
        <PublicFolder token={token} proof={proof} folder={item} onOpen={onOpen} />
      ) : (
        <FilesPreview item={item} source={{ kind: "public", token, proof: proof || undefined }} onDownload={view.allow_download ? download : undefined} />
      )}
    </>
  );
}

function PublicFolder({ token, proof, folder, onOpen }: { token: string; proof: string; folder: FilesPublicItem; onOpen: (id: string) => void }) {
  const children = useFilesPublicChildren(token, folder.id, {}, proof || undefined);
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
