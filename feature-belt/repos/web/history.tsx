"use client";

import { errorMessage, reposRelativeTime, reposShortSha, useReposCodeSearch, useReposCommit, useReposCommits, type ApiError, type ReposCommit } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, IconButton, Input, Skeleton, SkeletonGroup, Tooltip } from "@scaffoldapp/ui-web";
import { Copy, FileCode, GitCommitHorizontal, Search } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { DiffModeToggle, DiffSkeleton, DiffView, useDiffMode } from "./DiffView";
import { PathCrumbs } from "./parts";
import { RefPicker } from "./RefPicker";
import { copyText, repoHref, useRepoContext } from "./RepoLayout";

function dayLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric", year: "numeric" });
}

/** Commit history of a branch (optionally of one path), grouped by day. */
export function CommitsScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [gitRef] = useUrlState("ref", repo.default_branch);
  const [path] = useUrlState("path", "");
  const commits = useReposCommits(repo.id, { ref: gitRef, path });
  const groups = useMemo(() => {
    const out: { day: string; items: ReposCommit[] }[] = [];
    for (const c of commits.data?.items ?? []) {
      const d = dayLabel(c.committer.at);
      const last = out[out.length - 1];
      if (last && last.day === d) last.items.push(c);
      else out.push({ day: d, items: [c] });
    }
    return out;
  }, [commits.data]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repoId={repo.id} value={gitRef} onChange={(r) => router.push(repoHref(repo.id, "commits", { ref: r, path }))} />
        {path ? (
          <>
            <span className="type-label text-fg-muted">History of</span>
            <PathCrumbs repoId={repo.id} repoName={repo.name} gitRef={gitRef} path={path} />
          </>
        ) : null}
      </div>
      {repo.empty ? (
        <Card>
          <EmptyState icon={<GitCommitHorizontal />} title="No commits yet" />
        </Card>
      ) : commits.isPending ? (
        <SkeletonGroup label="Loading commits" className="flex flex-col gap-2">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </SkeletonGroup>
      ) : commits.isError ? (
        <Card>
          <ErrorState info={errorMessage(commits.error, "Try again.")} onRetry={() => void commits.refetch()} retrying={commits.isFetching} />
        </Card>
      ) : (
        <div className="flex flex-col gap-6">
          {groups.map((g) => (
            <section key={g.day} className="flex flex-col gap-2">
              <h2 className="flex items-center gap-2 type-label text-fg-muted">
                <GitCommitHorizontal className="size-4" aria-hidden />
                Commits on {g.day}
              </h2>
              <Card padding="none" className="divide-y divide-border">
                {g.items.map((c) => (
                  <CommitRow key={c.sha} repoId={repo.id} commit={c} />
                ))}
              </Card>
            </section>
          ))}
          <LoadMore hasNextPage={!!commits.hasNextPage} isFetchingNextPage={commits.isFetchingNextPage} fetchNextPage={commits.fetchNextPage} />
        </div>
      )}
    </div>
  );
}

function CommitRow({ repoId, commit: c }: { repoId: string; commit: ReposCommit }) {
  return (
    <div className="flex min-h-row items-center gap-3 px-4 py-2">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <Link href={repoHref(repoId, `commits/${c.sha}`)} className="truncate type-label font-semibold text-fg hover:underline">
          {c.subject}
        </Link>
        <span className="truncate type-caption text-fg-muted">
          {c.author.name} committed {reposRelativeTime(c.committer.at)}
        </span>
      </div>
      <Link href={repoHref(repoId, `commits/${c.sha}`)} className="hidden font-mono type-caption text-fg-muted hover:underline sm:inline">
        {reposShortSha(c.sha)}
      </Link>
      <IconButton label="Copy commit id" icon={<Copy />} variant="ghost" size="sm" tooltip onClick={() => void copyText(c.sha, "Commit id copied")} />
      <Tooltip content="Browse files at this commit">
        <Button asChild variant="ghost" size="sm">
          <Link href={repoHref(repoId, "", { ref: c.sha })} aria-label="Browse files at this commit">
            <FileCode className="size-4" aria-hidden />
          </Link>
        </Button>
      </Tooltip>
    </div>
  );
}

/** One commit: message, author, parents and its diff. */
export function CommitScreen({ sha }: { sha: string }) {
  const repo = useRepoContext();
  const commit = useReposCommit(repo.id, sha);
  const [mode, setMode] = useDiffMode();
  if (commit.isPending) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-24 w-full" />
        <DiffSkeleton />
      </div>
    );
  }
  if (commit.isError) {
    return (
      <Card>
        <ErrorState title={(commit.error as ApiError).status === 404 ? "Commit not found" : "Couldn't load this commit"} info={errorMessage(commit.error, "Try again.")} onRetry={() => void commit.refetch()} />
      </Card>
    );
  }
  const c = commit.data;
  return (
    <div className="flex flex-col gap-6">
      <Card className="flex flex-col gap-3">
        <h2 className="type-title-sm text-fg">{c.subject}</h2>
        {c.body ? <pre className="whitespace-pre-wrap font-sans type-body text-fg-muted">{c.body}</pre> : null}
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 type-label text-fg-muted">
          <span>
            <span className="font-semibold text-fg">{c.author.name}</span> committed {reposRelativeTime(c.committer.at)}
          </span>
          {c.committer.name !== c.author.name ? <span>with {c.committer.name}</span> : null}
          <span className="flex items-center gap-1">
            {c.parents.length === 1 ? "1 parent" : `${c.parents.length} parents`}
            {c.parents.map((p) => (
              <Link key={p} href={repoHref(repo.id, `commits/${p}`)} className="font-mono text-primary-strong hover:underline">
                {reposShortSha(p)}
              </Link>
            ))}
          </span>
          <button type="button" className="flex items-center gap-1 font-mono hover:text-fg" onClick={() => void copyText(c.sha, "Commit id copied")}>
            {reposShortSha(c.sha)} <Copy className="size-3" aria-hidden />
          </button>
          <Button asChild variant="outline" size="sm" className="ml-auto">
            <Link href={repoHref(repo.id, "", { ref: c.sha })}>Browse files</Link>
          </Button>
        </div>
      </Card>
      <div className="flex flex-wrap items-center gap-3">
        <span className="type-label text-fg-muted">
          {c.diff.files.length} {c.diff.files.length === 1 ? "file" : "files"} changed, <span className="text-success-strong">+{c.diff.additions}</span>{" "}
          <span className="text-danger-strong">−{c.diff.deletions}</span>
        </span>
        <div className="ml-auto">
          <DiffModeToggle mode={mode} onChange={setMode} />
        </div>
      </div>
      <DiffView files={c.diff.files} mode={mode} repoId={repo.id} headRef={c.sha} filesTruncated={c.diff.files_truncated} />
    </div>
  );
}

/** Code search: lines containing the query at a ref. */
export function SearchScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [gitRef] = useUrlState("ref", repo.default_branch);
  const [urlQ, setUrlQ] = useUrlState("q", "");
  const [q, setQ] = useState(urlQ);
  const results = useReposCodeSearch(repo.id, urlQ, gitRef);
  const byFile = useMemo(() => {
    const m = new Map<string, { line: number; text: string }[]>();
    for (const r of results.data?.items ?? []) m.set(r.path, [...(m.get(r.path) ?? []), { line: r.line, text: r.text }]);
    return [...m.entries()];
  }, [results.data]);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repoId={repo.id} value={gitRef} onChange={(r) => router.push(repoHref(repo.id, "search", { q: urlQ, ref: r }))} />
        <form
          role="search"
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            setUrlQ(q.trim());
          }}
        >
          <Input autoFocus aria-label="Search code" placeholder="Search code" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="w-full max-w-form" />
        </form>
      </div>
      {urlQ.trim().length < 2 ? (
        <p className="type-body text-fg-muted">Search for at least 2 characters.</p>
      ) : results.isPending ? (
        <SkeletonGroup label="Searching" className="flex flex-col gap-2">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
          ))}
        </SkeletonGroup>
      ) : results.isError ? (
        <Card>
          <ErrorState info={errorMessage(results.error, "Try again.")} onRetry={() => void results.refetch()} />
        </Card>
      ) : byFile.length === 0 ? (
        <Card>
          <EmptyState icon={<Search />} title={`Nothing matches “${urlQ}”`} />
        </Card>
      ) : (
        <div className="flex flex-col gap-4">
          <p className="type-label text-fg-muted">
            {results.data.items.length} {results.data.items.length === 1 ? "line" : "lines"} in {byFile.length} {byFile.length === 1 ? "file" : "files"}
            {results.data.truncated ? " (showing the first ones)" : ""}
          </p>
          {byFile.map(([path, lines]) => (
            <Card key={path} padding="none" className="overflow-hidden">
              <Link href={repoHref(repo.id, "blob", { ref: gitRef, path })} className="flex min-h-10 items-center border-b border-border bg-sunken px-4 font-mono type-label font-semibold text-fg hover:underline">
                {path}
              </Link>
              <ul className="overflow-x-auto font-mono text-label font-normal">
                {lines.map((l) => (
                  <li key={l.line}>
                    <Link href={`${repoHref(repo.id, "blob", { ref: gitRef, path })}#L${l.line}`} className="flex gap-3 px-4 py-1 hover:bg-sunken">
                      <span className="w-10 shrink-0 text-right text-fg-subtle tabular-nums">{l.line}</span>
                      <span className="whitespace-pre text-fg">{l.text}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
