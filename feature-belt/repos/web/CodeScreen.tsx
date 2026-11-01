"use client";

import { errorMessage, reposRelativeTime, useReposTree, useReposTreeCommits, type ApiError, type ReposRepo } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Input, Menu, MenuItem, Skeleton, SkeletonGroup, Tooltip } from "@scaffoldapp/ui-web";
import { FileCode, FilePlus, Folder, FolderGit2, GitCommitHorizontal, Link2, Plus, Search, Upload } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useState } from "react";
import { useUrlState } from "@/lib/url-state";
import { RepoMarkdown } from "./Markdown";
import { FileFinder, PathCrumbs, CommitLine, Panel, useFinderShortcut } from "./parts";
import { RefPicker } from "./RefPicker";
import { copyText, repoHref, useRepoContext } from "./RepoLayout";

/** A folder of the repository at a branch, tag or commit, with its README. */
export function CodeScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [gitRef] = useUrlState("ref", repo.default_branch);
  const [path] = useUrlState("path", "");
  const [finder, setFinder] = useState(false);
  const [q, setQ] = useState("");
  const tree = useReposTree(repo.id, { ref: gitRef, path }, !repo.empty);
  const commits = useReposTreeCommits(repo.id, { ref: gitRef, path }, !!tree.data);
  const openFinder = useCallback(() => setFinder(true), []);
  useFinderShortcut(openFinder);

  if (repo.empty) return <EmptyRepo repo={repo} />;

  const dir = tree.data?.path ?? path;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <RefPicker repoId={repo.id} value={gitRef} onChange={(r) => router.push(repoHref(repo.id, "", { ref: r, path }))} />
        <PathCrumbs repoId={repo.id} repoName={repo.name} gitRef={gitRef} path={dir} />
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <form
            role="search"
            onSubmit={(e) => {
              e.preventDefault();
              if (q.trim().length >= 2) router.push(repoHref(repo.id, "search", { q: q.trim(), ref: gitRef }));
            }}
          >
            <Input aria-label="Search code" placeholder="Search code" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="w-full sm:w-56" />
          </form>
          <Tooltip content="Go to file (T)">
            <Button variant="outline" size="md" onClick={openFinder}>
              Go to file
            </Button>
          </Tooltip>
          {repo.can_push ? (
            <Menu
              trigger={
                <Button size="md" leftIcon={<Plus />}>
                  Add file
                </Button>
              }
            >
              <MenuItem icon={<FilePlus />} onSelect={() => router.push(repoHref(repo.id, "edit", { ref: gitRef, path: dir, new: 1 }))}>
                Create a file
              </MenuItem>
              <MenuItem icon={<Upload />} onSelect={() => router.push(repoHref(repo.id, "upload", { ref: gitRef, path: dir }))}>
                Upload files
              </MenuItem>
            </Menu>
          ) : null}
        </div>
      </div>

      {tree.isPending ? (
        <TreeSkeleton />
      ) : tree.isError ? (
        <Card>
          <ErrorState
            title={(tree.error as ApiError).status === 404 ? "Not found" : "Couldn't load this folder"}
            info={errorMessage(tree.error, "Try again.")}
            onRetry={() => void tree.refetch()}
            retrying={tree.isFetching}
          />
        </Card>
      ) : (
        <>
          <Panel
            header={
              tree.data.last_commit ? (
                <>
                  <CommitLine repoId={repo.id} commit={tree.data.last_commit} className="min-w-0 flex-1" />
                  <Button asChild variant="ghost" size="sm" leftIcon={<GitCommitHorizontal />}>
                    <Link href={repoHref(repo.id, "commits", { ref: gitRef, path: dir })}>History</Link>
                  </Button>
                </>
              ) : (
                <span className="type-label text-fg-muted">No commits touch this folder.</span>
              )
            }
          >
            <table className="w-full border-collapse type-label">
              <tbody>
                {dir ? (
                  <tr className="border-b border-border">
                    <td colSpan={3}>
                      <Link href={repoHref(repo.id, "", { ref: gitRef, path: dir.split("/").slice(0, -1).join("/") })} className="flex min-h-control items-center px-4 font-mono text-fg-muted hover:bg-sunken">
                        ..
                      </Link>
                    </td>
                  </tr>
                ) : null}
                {tree.data.entries.map((e) => {
                  const c = commits.data?.commits[e.name];
                  const href = e.type === "tree" ? repoHref(repo.id, "", { ref: gitRef, path: e.path }) : repoHref(repo.id, "blob", { ref: gitRef, path: e.path });
                  const Icon = e.type === "tree" ? Folder : e.type === "commit" ? FolderGit2 : e.type === "symlink" ? Link2 : FileCode;
                  return (
                    <tr key={e.path} className="border-b border-border last:border-0 hover:bg-sunken">
                      <td className="w-full max-w-0 sm:w-auto sm:max-w-none">
                        <Link href={href} className="flex min-h-control items-center gap-3 px-4 text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
                          <Icon className={e.type === "tree" ? "size-4 shrink-0 text-primary-strong" : "size-4 shrink-0 text-fg-muted"} aria-hidden />
                          <span className="truncate">{e.name}</span>
                        </Link>
                      </td>
                      <td className="hidden max-w-0 px-4 text-fg-muted md:table-cell md:w-1/2">
                        {c ? (
                          <Link href={repoHref(repo.id, `commits/${c.sha}`)} className="block truncate hover:text-fg hover:underline">
                            {c.subject}
                          </Link>
                        ) : commits.isPending ? (
                          <Skeleton className="h-3 w-40" />
                        ) : null}
                      </td>
                      <td className="hidden whitespace-nowrap px-4 text-right type-caption text-fg-muted sm:table-cell">{c ? reposRelativeTime(c.author.at) : null}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </Panel>
          {tree.data.readme?.encoding === "utf-8" ? (
            <Panel
              header={
                <Link href={repoHref(repo.id, "blob", { ref: gitRef, path: tree.data.readme.path })} className="type-label font-semibold text-fg hover:underline">
                  {tree.data.readme.name}
                </Link>
              }
            >
              <div className="p-card">
                {/\.(md|markdown)$/i.test(tree.data.readme.name) || !tree.data.readme.name.includes(".") ? (
                  <RepoMarkdown source={tree.data.readme.content} repoId={repo.id} gitRef={gitRef} dir={dir} />
                ) : (
                  <pre className="whitespace-pre-wrap font-mono type-label text-fg">{tree.data.readme.content}</pre>
                )}
              </div>
            </Panel>
          ) : null}
        </>
      )}
      <FileFinder repoId={repo.id} gitRef={gitRef} open={finder} onOpenChange={setFinder} />
    </div>
  );
}

function TreeSkeleton() {
  return (
    <SkeletonGroup label="Loading files" className="flex flex-col overflow-hidden rounded-lg border border-border">
      <div className="h-12 border-b border-border bg-sunken" />
      {Array.from({ length: 6 }, (_, i) => (
        <div key={i} className="flex h-10 items-center gap-3 border-b border-border px-4 last:border-0">
          <Skeleton className="size-4" />
          <Skeleton className="h-3" width={`${20 + ((i * 23) % 30)}%`} />
        </div>
      ))}
    </SkeletonGroup>
  );
}

function EmptyRepo({ repo }: { repo: ReposRepo }) {
  const router = useRouter();
  const steps = [`git clone ${repo.clone_url}`, `cd ${repo.slug}`, "# add files, then", "git add .", `git commit -m "First commit"`, `git push origin ${repo.default_branch}`].join("\n");
  return (
    <Card className="flex flex-col gap-6">
      <EmptyState
        icon={<FolderGit2 />}
        title="This repository is empty"
        info="Push from your computer, or make the first file here."
        action={
          repo.can_push ? (
            <Button leftIcon={<FilePlus />} onClick={() => router.push(repoHref(repo.id, "edit", { name: "README.md", new: 1 }))}>
              Create a README
            </Button>
          ) : undefined
        }
      />
      <div className="mx-auto flex w-full max-w-form flex-col gap-2">
        <div className="flex items-center justify-between">
          <h2 className="type-label font-semibold text-fg">Push an existing project</h2>
          <Button variant="ghost" size="sm" onClick={() => void copyText(steps, "Commands copied")}>
            Copy
          </Button>
        </div>
        <pre className="overflow-x-auto rounded-md bg-sunken p-4 font-mono type-label text-fg">{steps}</pre>
      </div>
    </Card>
  );
}
