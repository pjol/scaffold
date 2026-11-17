"use client";

import {
  errorMessage,
  reposDirname,
  reposFormatBytes,
  reposRelativeTime,
  reposShortSha,
  reposSupports,
  useReposBlame,
  useReposBlob,
  useReposCommits,
  useReposDownload,
  type ApiError,
  type ReposBlameRange,
} from "@scaffoldapp/shared";
import { Button, Card, ErrorState, IconButton, Menu, MenuItem, SegmentedControl, Skeleton, SkeletonGroup, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { Download, History, Link2, Pencil, Pin, Trash2, WrapText } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { useUrlState } from "@/lib/url-state";
import { CodeView } from "./code";
import { CommitDialog } from "./CommitDialog";
import { RepoMarkdown } from "./Markdown";
import { CommitLine, PathCrumbs, Panel } from "./parts";
import { RefPicker } from "./RefPicker";
import { copyText, repoHref, useRepoContext } from "./RepoLayout";

type View = "preview" | "code" | "blame";

function parseLines(hash: string): { from: number; to: number } | null {
  const m = /^#L(\d+)(?:-L?(\d+))?$/.exec(hash);
  if (!m) return null;
  const a = Number(m[1]);
  const b = m[2] ? Number(m[2]) : a;
  return { from: Math.min(a, b), to: Math.max(a, b) };
}

/** One file at a ref: highlighted code (or rendered Markdown, an image), blame, history, edit and delete. */
export function FileScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [gitRef] = useUrlState("ref", repo.default_branch);
  const [path] = useUrlState("path", "");
  const blob = useReposBlob(repo.id, { ref: gitRef, path });
  const isMarkdown = /\.(md|markdown)$/i.test(path);
  const [view, setView] = useState<View>(isMarkdown ? "preview" : "code");
  const [wrap, setWrap] = useState(false);
  const [selected, setSelected] = useState<{ from: number; to: number } | null>(null);
  const [deleting, setDeleting] = useState(false);
  const blame = useReposBlame(repo.id, { ref: gitRef, path }, view === "blame");
  const last = useReposCommits(repo.id, { ref: gitRef, path });
  const download = useReposDownload(repo.id);

  // #L12 or #L10-L20 selects lines on load and when the hash changes (links on this page, back/forward).
  useEffect(() => {
    const read = () => {
      const s = parseLines(window.location.hash);
      setSelected(s);
      if (s) setView((v) => (v === "preview" ? "code" : v));
    };
    read();
    window.addEventListener("hashchange", read);
    return () => window.removeEventListener("hashchange", read);
  }, []);
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    if (!selected || !blob.data || scrolled) return;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    document.getElementById(`L${selected.from}`)?.scrollIntoView({ block: "center", behavior: reduce ? "auto" : "smooth" });
    setScrolled(true);
  }, [blob.data, selected, scrolled]);

  const blameByLine = useMemo(() => {
    const m = new Map<number, ReposBlameRange>();
    for (const r of blame.data?.ranges ?? []) m.set(r.start, r);
    return m;
  }, [blame.data]);

  const selectLine = (n: number, extend: boolean) => {
    const next = extend && selected ? { from: Math.min(selected.from, n), to: Math.max(selected.from, n) } : { from: n, to: n };
    setSelected(next);
    const hash = next.from === next.to ? `#L${next.from}` : `#L${next.from}-L${next.to}`;
    window.history.replaceState(null, "", window.location.pathname + window.location.search + hash);
  };

  const save = async () => {
    try {
      const d = await download.mutateAsync({ ref: gitRef, path });
      const bytes = Uint8Array.from(atob(d.content_base64), (c) => c.charCodeAt(0));
      const url = URL.createObjectURL(new Blob([bytes], { type: "application/octet-stream" }));
      const a = document.createElement("a");
      a.href = url;
      a.download = d.name;
      document.body.appendChild(a);
      a.click();
      a.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't download this file."));
    }
  };

  const b = blob.data;
  const isBranch = !/^[0-9a-f]{40}$/.test(gitRef);
  const lineHash = selected ? (selected.from === selected.to ? `#L${selected.from}` : `#L${selected.from}-L${selected.to}`) : "";
  const lineWords = selected ? (selected.from === selected.to ? `line ${selected.from}` : `lines ${selected.from}–${selected.to}`) : "";
  const copyPermalink = () => {
    if (!b) return;
    void copyText(window.location.origin + repoHref(repo.id, "blob", { ref: b.sha, path }) + lineHash, selected ? `Permalink to ${lineWords} copied` : "Permalink copied");
  };
  const copyLineLink = () => void copyText(window.location.origin + repoHref(repo.id, "blob", { ref: gitRef, path }) + lineHash, `Link to ${lineWords} copied`);
  const lastCommit = last.data?.items[0];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repoId={repo.id} value={gitRef} onChange={(r) => router.push(repoHref(repo.id, "blob", { ref: r, path }))} />
        <PathCrumbs repoId={repo.id} repoName={repo.name} gitRef={gitRef} path={path} leaf="file" />
      </div>
      {lastCommit ? (
        <Card padding="compact" className="flex flex-wrap items-center gap-3">
          <CommitLine repoId={repo.id} commit={lastCommit} className="min-w-0 flex-1" />
          <Button asChild variant="ghost" size="sm" leftIcon={<History />}>
            <Link href={repoHref(repo.id, "commits", { ref: gitRef, path })}>History</Link>
          </Button>
        </Card>
      ) : null}
      {blob.isPending ? (
        <SkeletonGroup label="Loading file" className="flex flex-col gap-2 rounded-lg border border-border p-4">
          {Array.from({ length: 12 }, (_, i) => (
            <Skeleton key={i} className="h-4" width={`${30 + ((i * 41) % 60)}%`} />
          ))}
        </SkeletonGroup>
      ) : blob.isError ? (
        <Card>
          <ErrorState
            title={(blob.error as ApiError).status === 404 ? "File not found" : "Couldn't load this file"}
            info={errorMessage(blob.error, "Try again.")}
            onRetry={() => void blob.refetch()}
            retrying={blob.isFetching}
          />
        </Card>
      ) : b ? (
        <Panel
          header={
            <>
              <span className="type-caption text-fg-muted">
                {b.encoding === "utf-8" ? `${b.lines} lines · ` : ""}
                {reposFormatBytes(b.size)}
                {b.mode === "100755" ? " · executable" : ""}
              </span>
              {b.encoding === "utf-8" ? (
                <SegmentedControl<View>
                  aria-label="View"
                  fullWidth={false}
                  value={view}
                  onValueChange={setView}
                  options={[...(isMarkdown ? [{ value: "preview" as View, label: "Preview" }] : []), { value: "code", label: "Code" }, ...(reposSupports(repo, "blame") ? [{ value: "blame" as View, label: "Blame" }] : [])]}
                />
              ) : null}
              <div className="ml-auto flex items-center gap-1">
                {b.encoding === "utf-8" && view !== "preview" ? (
                  <IconButton label={wrap ? "Don't wrap lines" : "Wrap lines"} icon={<WrapText />} variant={wrap ? "soft" : "ghost"} size="sm" tooltip onClick={() => setWrap(!wrap)} />
                ) : null}
                <Menu trigger={<IconButton label={selected ? `Copy a link to ${lineWords}` : "Copy a link"} icon={<Link2 />} variant="ghost" size="sm" />}>
                  <MenuItem icon={<Pin />} onSelect={copyPermalink}>
                    {selected ? `Copy permalink to ${lineWords}` : "Copy permalink"}
                  </MenuItem>
                  {selected ? (
                    <MenuItem icon={<Link2 />} onSelect={copyLineLink}>
                      Copy link to {lineWords} on {isBranch ? gitRef : "this commit"}
                    </MenuItem>
                  ) : null}
                </Menu>
                <IconButton label="Download" icon={<Download />} variant="ghost" size="sm" tooltip loading={download.isPending} onClick={() => void save()} />
                {repo.can_push && isBranch && b.editable ? (
                  <Tooltip content="Edit this file">
                    <Button asChild variant="ghost" size="sm" leftIcon={<Pencil />}>
                      <Link href={repoHref(repo.id, "edit", { ref: gitRef, path })}>Edit</Link>
                    </Button>
                  </Tooltip>
                ) : null}
                {repo.can_push && isBranch ? (
                  <IconButton label="Delete file" icon={<Trash2 />} variant="danger" size="sm" tooltip onClick={() => setDeleting(true)} />
                ) : null}
              </div>
            </>
          }
        >
          {b.submodule ? (
            <p className="p-4 type-label text-fg-muted">A submodule at commit {reposShortSha(b.oid)}.</p>
          ) : b.image && b.encoding === "base64" ? (
            <div className="flex justify-center bg-sunken p-4">
              {/* eslint-disable-next-line @next/next/no-img-element -- a data URL from the API, not an optimisable remote image */}
              <img src={`data:${imageType(b.name)};base64,${b.content}`} alt={b.name} className="max-h-[70vh] max-w-full object-contain" />
            </div>
          ) : b.too_large || b.binary ? (
            <div className="flex flex-col items-start gap-3 p-4">
              <p className="type-label text-fg-muted">{b.too_large ? `This file is too large to show here (${reposFormatBytes(b.size)}).` : "This is a binary file."}</p>
              <Button size="sm" variant="outline" leftIcon={<Download />} loading={download.isPending} onClick={() => void save()}>
                Download
              </Button>
            </div>
          ) : view === "preview" ? (
            <div className="p-card">
              <RepoMarkdown source={b.content} repoId={repo.id} gitRef={gitRef} dir={reposDirname(path)} />
            </div>
          ) : (
            <CodeView
              code={b.content}
              language={b.language}
              selected={selected}
              onSelectLine={selectLine}
              wrap={wrap}
              {...(view === "blame"
                ? {
                    gutter: (n: number) => {
                      const r = blameByLine.get(n);
                      if (!r) return blame.isPending && n === 1 ? <Skeleton className="h-3 w-40" /> : null;
                      return (
                        <Link href={repoHref(repo.id, `commits/${r.sha}`)} className={cn("flex w-56 items-center gap-2 font-sans type-caption text-fg-muted hover:text-fg")} title={r.subject}>
                          <span className="truncate">{r.subject}</span>
                          <span className="ml-auto shrink-0">{reposRelativeTime(new Date(r.at * 1000).toISOString())}</span>
                        </Link>
                      );
                    },
                  }
                : {})}
            />
          )}
        </Panel>
      ) : null}
      {selected && b?.encoding === "utf-8" ? (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <span className="type-caption text-fg-muted">Selected {lineWords}. Shift-click a line number to extend.</span>
          <Button variant="ghost" size="sm" leftIcon={<Pin />} onClick={copyPermalink}>
            Copy permalink
          </Button>
        </div>
      ) : null}
      {b ? (
        <CommitDialog
          open={deleting}
          onOpenChange={setDeleting}
          title={`Delete ${b.name}`}
          branch={gitRef}
          expectedHead={b.sha}
          defaultMessage={`Delete ${path}`}
          changes={[{ op: "delete", path }]}
          confirmLabel="Delete file"
          tone="danger"
          onDone={(res) => router.push(repoHref(repo.id, "", { ref: res.branch, path: reposDirname(path) }))}
        />
      ) : null}
    </div>
  );
}

function imageType(name: string): string {
  const ext = name.split(".").pop()?.toLowerCase();
  return ext === "jpg" || ext === "jpeg" ? "image/jpeg" : ext === "gif" ? "image/gif" : ext === "webp" ? "image/webp" : ext === "ico" ? "image/x-icon" : ext === "bmp" ? "image/bmp" : "image/png";
}
