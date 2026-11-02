"use client";

import {
  errorMessage,
  isConnectionErrorCode,
  reposPlatformAccessUrl,
  reposPlatformName,
  reposPullNoun,
  reposSupports,
  reposTopic,
  useActiveOrg,
  useCreateReposLink,
  useImportConnectedRepo,
  useRepo,
  useTopic,
  type ApiError,
  type ReposRepo,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Field, Input, InfoTip, Menu, Skeleton, SkeletonGroup, SourceBadge, cn, motion, toast, useMotionTokens } from "@scaffoldapp/ui-web";
import { Check, Code2, Copy, ExternalLink, FolderInput, FolderOpen, GitBranch, GitCommitHorizontal, GitPullRequest, KeyRound, Link2, RefreshCw, Settings, Users } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useId, useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { FolderPickerDialog } from "@/features/files/actions";

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
  useTopic(repo.data && reposSupports(repo.data, "realtime") ? reposTopic(repo.data.id) : null);

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
    const err = repo.error as ApiError;
    const notFound = err.status === 404;
    const fix = typeof err.details?.["fix_url"] === "string" ? (err.details["fix_url"] as string) : null;
    if (fix && isConnectionErrorCode(err.code)) {
      return (
        <div className="mx-auto w-full max-w-content px-gutter py-section">
          <Card>
            <EmptyState
              icon={<RefreshCw />}
              title={err.code === "connection_rate_limited" ? "The platform is busy" : "Reconnect to open this repository"}
              info={errorMessage(err, "Reconnect the account in Settings → Connections.")}
              action={
                <Button asChild variant="outline">
                  <Link href={`${fix}${fix.includes("?") ? "&" : "?"}return_to=${encodeURIComponent(`/repos/${repoId}`)}`}>Open Connections</Link>
                </Button>
              }
            />
          </Card>
        </div>
      );
    }
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
  const { org } = useActiveOrg();
  const [dialog, setDialog] = useState<"import" | "link" | null>(null);
  const base = `/repos/${repo.id}`;
  const platform = reposPlatformName(repo);
  const accessUrl = reposPlatformAccessUrl(repo);
  const tabs = [
    { href: base, label: "Code", icon: Code2, active: (p: string) => p === base || /\/(blob|edit|upload|search)$/.test(p) },
    { href: `${base}/pulls`, label: reposPullNoun(repo, true), icon: GitPullRequest, count: repo.open_pulls, active: (p: string) => p.startsWith(`${base}/pulls`) || p.startsWith(`${base}/compare`) },
    { href: `${base}/commits`, label: "Commits", icon: GitCommitHorizontal, active: (p: string) => p.startsWith(`${base}/commits`) },
    { href: `${base}/branches`, label: "Branches", icon: GitBranch, active: (p: string) => p.startsWith(`${base}/branches`) },
    ...(repo.can_manage && reposSupports(repo, "settings")
      ? [{ href: `${base}/settings`, label: "Settings", icon: Settings, active: (p: string) => p.startsWith(`${base}/settings`) }]
      : []),
  ];
  const connectedActions = platform
    ? [
        ...(repo.web_url ? [{ id: "repos-open-platform", label: `Open in ${platform}`, icon: ExternalLink, onSelect: () => window.open(repo.web_url, "_blank", "noopener,noreferrer") }] : []),
        ...(accessUrl ? [{ id: "repos-platform-access", label: `Manage access in ${platform}`, icon: Users, onSelect: () => window.open(accessUrl, "_blank", "noopener,noreferrer") }] : []),
        { id: "repos-copy-url", label: "Copy clone URL", icon: Copy, onSelect: () => void copyText(repo.clone_url, "Clone URL copied") },
        ...(reposSupports(repo, "import") && org ? [{ id: "repos-import", label: "Import into Scaffold App", icon: FolderInput, onSelect: () => setDialog("import") }] : []),
        ...(org ? [{ id: "repos-link", label: "Add to a Scaffold App folder", icon: Link2, onSelect: () => setDialog("link") }] : []),
      ]
    : [
        { id: "repos-copy-url", label: "Copy clone URL", icon: Copy, onSelect: () => void copyText(repo.clone_url, "Clone URL copied") },
        ...(repo.folder_id ? [{ id: "repos-open-folder", label: "Open folder", icon: FolderOpen, onSelect: () => router.push(`/files/${repo.folder_id}`) }] : []),
        { id: "repos-credentials", label: "Git credentials", icon: KeyRound, onSelect: () => router.push("/settings/git-credentials") },
        ...(repo.can_manage ? [{ id: "repos-settings", label: "Settings", icon: Settings, onSelect: () => router.push(`${base}/settings`) }] : []),
      ];
  return (
    <header className="flex flex-col gap-4">
      <div className="flex min-h-control-lg flex-wrap items-center gap-3">
        <h1 className="flex min-w-0 flex-1 items-center gap-2 type-title text-fg">
          <GitBranch className="size-6 shrink-0 text-fg-muted" aria-hidden />
          <Link href={base} className="truncate hover:underline focus-visible:outline-2 focus-visible:outline-ring">
            {platform ? (repo.full_name ?? repo.name) : repo.name}
          </Link>
          {repo.description ? <InfoTip content={repo.description} size="md" /> : null}
          {!repo.can_push ? (
            <Badge tone="neutral" className="shrink-0">
              {repo.my_level === "comment" ? "Can comment" : "View only"}
            </Badge>
          ) : null}
        </h1>
        <div className="flex shrink-0 items-center gap-2">
          <SourceBadge source={repo.source} iconOnly size="md" className="sm:hidden" />
          <SourceBadge source={repo.source} showAccount size="md" className="hidden max-w-56 sm:inline-flex" />
          <CloneMenu repo={repo} />
          <ResourceMenu
            feature="repos"
            kind={platform ? "repos.external" : "repos.repository"}
            id={platform && repo.source?.type === "connected" ? `${repo.source.connection_id}:${repo.full_name ?? repo.name}` : repo.id}
            title={repo.full_name ?? repo.name}
            orgId={repo.org_id || org?.id}
            actions={connectedActions}
          />
        </div>
      </div>
      {repo.grant_write_url ? (
        <p className="flex flex-wrap items-center gap-2 type-label text-fg-muted">
          You can read {repo.full_name ?? repo.name} here. To commit, review and merge from Scaffold App, turn on changes for {platform}.
          <Link href={repo.grant_write_url} className="font-semibold text-primary-strong underline underline-offset-2 hover:no-underline">
            Turn on changes
          </Link>
        </p>
      ) : null}
      <RepoTabs tabs={tabs} pathname={pathname} />
      {dialog && org ? <ConnectedBridgeDialog repo={repo} orgId={org.id} kind={dialog} onClose={() => setDialog(null)} /> : null}
    </header>
  );
}

/** "Import into the app" (a copy with branches and tags) or "Add to a folder" (a link colleagues open with their own account). */
function ConnectedBridgeDialog({ repo, orgId, kind, onClose }: { repo: ReposRepo; orgId: string; kind: "import" | "link"; onClose: () => void }) {
  const router = useRouter();
  const importRepo = useImportConnectedRepo();
  const link = useCreateReposLink();
  const name = repo.full_name ?? repo.name;
  return (
    <FolderPickerDialog
      orgId={orgId}
      mode="copy"
      items={[]}
      title={kind === "import" ? `Import ${name} into` : `Add a link to ${name} in`}
      actionVerb={kind === "import" ? "Import" : "Add link"}
      onClose={onClose}
      onPick={async (folderId) => {
        try {
          if (kind === "import") {
            const created = await importRepo.mutateAsync({ repo_id: repo.id, org_id: orgId, folder_id: folderId });
            toast.success(`Imported ${name}`);
            router.push(`/repos/${created.id}`);
          } else {
            await link.mutateAsync({ repo_id: repo.id, org_id: orgId, folder_id: folderId });
            toast.success(`Added a link to ${name}. Colleagues open it with their own ${reposPlatformName(repo) ?? ""} account.`);
          }
          onClose();
        } catch (e) {
          toast.error(errorMessage(e, kind === "import" ? "Couldn't import it." : "Couldn't add the link."));
        }
      }}
    />
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
        <Field
          label="Clone over HTTPS"
          info={
            reposSupports(repo, "credentials")
              ? "Sign in with a git credential as the password. Make one in Settings → Git credentials."
              : `Clone from ${reposPlatformName(repo) ?? "the platform"} with your account there (a personal access token works as the password).`
          }
        >
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
        {reposSupports(repo, "credentials") ? (
          <Link href="/settings/git-credentials" className="type-label text-primary-strong underline underline-offset-2 hover:no-underline">
            Git credentials
          </Link>
        ) : null}
      </div>
    </Menu>
  );
}
