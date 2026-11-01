"use client";

import { errorMessage, reposTopic, useRepo, useTopic, type ApiError, type ReposRepo } from "@scaffoldapp/shared";
import { Badge, Button, Card, ErrorState, Field, Input, InfoTip, Menu, Skeleton, SkeletonGroup, cn, motion, toast, useMotionTokens } from "@scaffoldapp/ui-web";
import { Check, Code2, Copy, FolderOpen, GitBranch, GitCommitHorizontal, GitPullRequest, KeyRound, Settings } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useId, useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";

const RepoContext = createContext<ReposRepo | null>(null);

/** The repository the current screen is inside (from RepoLayout). */
export function useRepoContext(): ReposRepo {
  const repo = useContext(RepoContext);
  if (!repo) throw new Error("useRepoContext outside RepoLayout");
  return repo;
}

/** A repository path for the app: /repos/<id>/<sub>?ref=&path=. */
export function repoHref(repoId: string, sub: string, params: Record<string, string | number | undefined | null> = {}): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== "") q.set(k, String(v));
  const s = q.toString();
  return `/repos/${repoId}${sub ? `/${sub}` : ""}${s ? `?${s}` : ""}`;
}

/** Copy text with a toast. */
export async function copyText(text: string, what = "Copied") {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(what);
  } catch {
    toast.error("Couldn't copy. Select the text and copy it yourself.");
  }
}

/**
 * Every repository screen's frame: name, clone, the ⋮ menu (Share… and AI agent access
 * come from other features for kind repos.repository) and the section tabs. Live updates
 * arrive on repos:<id> while it's open.
 */
export function RepoLayout({ repoId, children }: { repoId: string; children: ReactNode }) {
  const repo = useRepo(repoId);
  useTopic(repo.data ? reposTopic(repo.data.id) : null);

  if (repo.isPending) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <SkeletonGroup label="Loading repository" className="flex flex-col gap-6">
          <Skeleton className="h-10 w-64" />
          <Skeleton className="h-10 w-full max-w-md" />
          <Skeleton className="h-80 w-full" />
        </SkeletonGroup>
      </div>
    );
  }
  if (repo.isError) {
    const notFound = (repo.error as ApiError).status === 404;
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <Card>
          <ErrorState
            title={notFound ? "Repository not found" : "Couldn't load this repository"}
            info={notFound ? "It may have been deleted, moved to the trash, or not shared with you." : errorMessage(repo.error, "Check your connection and try again.")}
            {...(notFound ? {} : { onRetry: () => void repo.refetch(), retrying: repo.isFetching })}
          />
        </Card>
      </div>
    );
  }
  return (
    <RepoContext.Provider value={repo.data}>
      <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
        <RepoHeader repo={repo.data} />
        {children}
      </div>
    </RepoContext.Provider>
  );
}

function RepoHeader({ repo }: { repo: ReposRepo }) {
  const pathname = usePathname();
  const router = useRouter();
  const base = `/repos/${repo.id}`;
  const tabs = [
    { href: base, label: "Code", icon: Code2, active: (p: string) => p === base || /\/(blob|edit|upload|search)$/.test(p) },
    { href: `${base}/pulls`, label: "Pull requests", icon: GitPullRequest, count: repo.open_pulls, active: (p: string) => p.startsWith(`${base}/pulls`) || p.startsWith(`${base}/compare`) },
    { href: `${base}/commits`, label: "Commits", icon: GitCommitHorizontal, active: (p: string) => p.startsWith(`${base}/commits`) },
    { href: `${base}/branches`, label: "Branches", icon: GitBranch, active: (p: string) => p.startsWith(`${base}/branches`) },
    ...(repo.can_manage ? [{ href: `${base}/settings`, label: "Settings", icon: Settings, active: (p: string) => p.startsWith(`${base}/settings`) }] : []),
  ];
  return (
    <header className="flex flex-col gap-4">
      <div className="flex min-h-control-lg flex-wrap items-center gap-3">
        <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">
          <GitBranch className="size-6 shrink-0 text-fg-muted" aria-hidden />
          <Link href={base} className="truncate hover:underline focus-visible:outline-2 focus-visible:outline-ring">
            {repo.name}
          </Link>
          {repo.description ? <InfoTip content={repo.description} size="md" /> : null}
          {!repo.can_push ? (
            <Badge tone="neutral" className="shrink-0">
              {repo.my_level === "comment" ? "Can comment" : "View only"}
            </Badge>
          ) : null}
        </h1>
        <div className="flex shrink-0 items-center gap-2">
          <CloneMenu repo={repo} />
          <ResourceMenu
            feature="repos"
            kind="repos.repository"
            id={repo.id}
            title={repo.name}
            orgId={repo.org_id}
            actions={[
              { id: "repos-copy-url", label: "Copy clone URL", icon: Copy, onSelect: () => void copyText(repo.clone_url, "Clone URL copied") },
              ...(repo.folder_id ? [{ id: "repos-open-folder", label: "Open folder", icon: FolderOpen, onSelect: () => router.push(`/files/${repo.folder_id}`) }] : []),
              { id: "repos-credentials", label: "Git credentials", icon: KeyRound, onSelect: () => router.push("/settings/git-credentials") },
              ...(repo.can_manage ? [{ id: "repos-settings", label: "Settings", icon: Settings, onSelect: () => router.push(`${base}/settings`) }] : []),
            ]}
          />
        </div>
      </div>
      <RepoTabs tabs={tabs} pathname={pathname} />
    </header>
  );
}

function RepoTabs({ tabs, pathname }: { tabs: { href: string; label: string; icon: typeof Code2; count?: number; active: (p: string) => boolean }[]; pathname: string }) {
  const t = useMotionTokens();
  const layoutId = useId();
  return (
    <nav aria-label="Repository" className="-mx-gutter overflow-x-auto px-gutter">
      <ul className="flex min-w-max gap-1 border-b border-border">
        {tabs.map((tab) => {
          const active = tab.active(pathname);
          const Icon = tab.icon;
          return (
            <li key={tab.href} className="relative">
              <Link
                href={tab.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "flex min-h-tap-sm items-center gap-2 rounded-t-md px-3 type-label font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-ring",
                  active ? "text-fg" : "text-fg-muted hover:bg-sunken hover:text-fg",
                )}
              >
                <Icon className="size-4" aria-hidden />
                {tab.label}
                {tab.count ? <span className="rounded-sm bg-sunken px-1.5 type-caption tabular-nums text-fg-muted">{tab.count}</span> : null}
              </Link>
              {active ? <motion.span layoutId={layoutId} className="absolute inset-x-0 -bottom-px h-0.5 rounded-pill bg-primary" transition={t.spring.snappy} /> : null}
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

function CloneMenu({ repo }: { repo: ReposRepo }) {
  const [copied, setCopied] = useState(false);
  return (
    <Menu
      align="end"
      trigger={
        <Button size="sm" variant="outline" leftIcon={<Code2 />}>
          Clone
        </Button>
      }
    >
      <div className="flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-3 p-3" onKeyDown={(e) => e.stopPropagation()}>
        <Field label="Clone over HTTPS" info="Sign in with a git credential as the password. Make one in Settings → Git credentials.">
          <Input
            readOnly
            value={repo.clone_url}
            onFocus={(e) => e.currentTarget.select()}
            className="font-mono"
            trailing={
              <button
                type="button"
                aria-label="Copy clone URL"
                className="flex size-8 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken hover:text-fg [&_svg]:size-4"
                onClick={async () => {
                  await copyText(repo.clone_url, "Clone URL copied");
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                }}
              >
                {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
              </button>
            }
          />
        </Field>
        <Link href="/settings/git-credentials" className="type-label text-primary-strong underline underline-offset-2 hover:no-underline">
          Git credentials
        </Link>
      </div>
    </Menu>
  );
}
