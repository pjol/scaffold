"use client";

import {
  errorMessage,
  isConnectionErrorCode,
  reposFormatBytes,
  reposRelativeTime,
  useActiveOrg,
  useCreateRepo,
  useImportReposFolder,
  useReposConnectedList,
  useReposImportPreview,
  useReposList,
  useReposSources,
  type ApiError,
  type ReposSummary,
} from "@scaffoldapp/shared";
import {
  Banner,
  Button,
  Card,
  Checkbox,
  ConnectedSection,
  ConnectPrompt,
  EmptyState,
  ErrorState,
  Field,
  Input,
  ListRow,
  ProviderIcon,
  Select,
  Skeleton,
  SkeletonGroup,
  SkeletonRow,
  SourceBadge,
  SourceFilter,
  TextArea,
  connectedSectionItems,
  sourceFilterOptions,
  toast,
} from "@scaffoldapp/ui-web";
import { Building2, FolderGit2, GitBranch, GitPullRequest, Lock, Plus, RefreshCw, Search } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";

/**
 * Repositories: the workspace's own, then each connected GitHub / GitLab account (the
 * Connected section and source chips pick one; "All" shows a few of each). Connected rows
 * carry a provider badge and open in the same screens, read live from the platform.
 */
export function ReposListScreen() {
  const { org, isPending } = useActiveOrg();
  const [q, setQ] = useState("");
  const [source, setSource] = useUrlState("source", "all");
  const sources = useReposSources(org?.id);
  const connected = (sources.data?.sources ?? []).filter((s) => s.type === "connected");
  const options = sourceFilterOptions({ appLabel: "Scaffold App", sources: sources.data?.sources ?? [] });
  const picked = connected.find((s) => s.id === source);
  const view = picked ? "connection" : source === "app" ? "app" : "all";
  if (!isPending && !org) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <Screen title="Repositories">
          <Card>
            <EmptyState icon={<Building2 />} title="Join or create a workspace first" />
          </Card>
        </Screen>
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <Screen
        title="Repositories"
        info="Git repositories live in the drive and share like folders. Clone them with a git credential from Settings. Connected GitHub and GitLab accounts show their repositories here too, read live with your own account."
        action={
          <Button asChild size="auto" leftIcon={<Plus />}>
            <Link href="/repos/new">New repository</Link>
          </Button>
        }
      >
        <div className="flex flex-col gap-6 lg:grid lg:grid-cols-[minmax(0,1fr)_15rem] lg:items-start lg:gap-8">
          <section className="flex min-w-0 flex-col gap-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
              <Input
                aria-label="Search repositories"
                placeholder={picked ? `Search ${picked.label} · ${picked.account ?? ""}` : "Search repositories"}
                leadingIcon={<Search aria-hidden />}
                value={q}
                onChange={(e) => setQ(e.target.value)}
                wrapperClassName="sm:w-72"
              />
              <SourceFilter options={options} value={picked ? picked.id : view} onChange={(id) => setSource(id === "all" ? "" : id)} className="min-w-0 flex-1" />
            </div>
            {view === "connection" && picked ? (
              <ConnectedRepoList source={picked} q={q} orgId={org?.id} />
            ) : (
              <>
                <AppRepoList orgId={org?.id} q={q} loading={isPending} titled={view === "all" && connected.length > 0} />
                {view === "all"
                  ? connected.map((c) => <ConnectedPreview key={c.id} source={c} q={q} orgId={org?.id} onMore={() => setSource(c.id)} />)
                  : null}
              </>
            )}
            {connected.length === 0 ? <ConnectPrompt offers={sources.data?.offers} returnTo="/repos" linkAs={Link} title="Bring in repositories from" /> : null}
          </section>
          <aside className="flex flex-col gap-4 lg:sticky lg:top-4">
            <ConnectedSection
              items={connectedSectionItems(sources.data?.sources, (id) => `/repos?source=${id}`)}
              activeId={picked?.id ?? null}
              linkAs={Link}
              addHref="/settings/connections?capability=repos.github.read&return_to=/repos"
              loading={sources.isPending && !!org}
            />
            {connected.length > 0 ? <ConnectPrompt offers={sources.data?.offers} returnTo="/repos" linkAs={Link} title="Connect more" /> : null}
          </aside>
        </div>
      </Screen>
    </div>
  );
}

function AppRepoList({ orgId, q, loading, titled }: { orgId: string | undefined; q: string; loading: boolean; titled: boolean }) {
  const repos = useReposList(orgId, q);
  const rows = repos.data?.items ?? [];
  return (
    <section className="flex flex-col gap-2" aria-label="Scaffold App">
      {titled ? <h2 className="px-1 type-label font-semibold text-fg-muted">Scaffold App</h2> : null}
      <Card padding="none" className="p-1">
        {repos.isPending || loading ? (
          <SkeletonGroup label="Loading repositories" className="flex flex-col">
            {Array.from({ length: 4 }, (_, i) => (
              <SkeletonRow key={i} />
            ))}
          </SkeletonGroup>
        ) : repos.isError ? (
          <ErrorState info={errorMessage(repos.error, "Try again.")} onRetry={() => void repos.refetch()} retrying={repos.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<FolderGit2 />}
            title={q ? "No repository matches" : "No repositories yet"}
            action={
              q ? undefined : (
                <Button asChild variant="outline">
                  <Link href="/repos/new">Create a repository</Link>
                </Button>
              )
            }
          />
        ) : (
          <ul className="flex flex-col">
            {rows.map((r) => (
              <li key={r.id}>
                <RepoRow repo={r} />
              </li>
            ))}
          </ul>
        )}
      </Card>
      <LoadMore hasNextPage={!!repos.hasNextPage} isFetchingNextPage={repos.isFetchingNextPage} fetchNextPage={repos.fetchNextPage} />
    </section>
  );
}

/** One repository row: the app's own and connected ones look the same, connected ones badged. */
function RepoRow({ repo: r }: { repo: ReposSummary }) {
  const connected = !!r.source && r.source.type === "connected";
  const changed = r.pushed_at ? `changed ${reposRelativeTime(r.pushed_at)}` : "no commits yet";
  return (
    <ListRow
      asChild
      leading={
        <span className="flex size-10 items-center justify-center rounded-md bg-sunken text-fg-muted">
          <GitBranch className="size-5" aria-hidden />
        </span>
      }
      title={
        <span className="flex min-w-0 flex-col">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate font-semibold">{connected ? (r.full_name ?? r.name) : r.name}</span>
            {connected && r.private ? <Lock className="size-3.5 shrink-0 text-fg-subtle" aria-label="Private" /> : null}
          </span>
          <span className="truncate type-caption text-fg-muted">
            {r.default_branch ? `${r.default_branch} · ` : ""}
            {changed}
          </span>
        </span>
      }
      trailing={
        <span className="flex items-center gap-3">
          {r.open_pulls > 0 ? (
            <span className="flex items-center gap-1 type-caption text-fg-muted">
              <GitPullRequest className="size-4" aria-hidden />
              {r.open_pulls}
              <span className="sr-only">open pull requests</span>
            </span>
          ) : null}
          <SourceBadge source={r.source} iconOnly className="sm:hidden" />
          <SourceBadge source={r.source} className="hidden sm:inline-flex" />
        </span>
      }
    >
      <Link href={`/repos/${r.id}`} />
    </ListRow>
  );
}

type ConnectedSourceRow = { id: string; label: string; provider?: string; account?: string; status?: string; fix_url?: string };

function connectedTitle(s: ConnectedSourceRow) {
  return `${s.label}${s.account ? ` · ${s.account}` : ""}`;
}

/** "All": a connected account's few most recently active repositories. */
function ConnectedPreview({ source, q, orgId, onMore }: { source: ConnectedSourceRow; q: string; orgId: string | undefined; onMore: () => void }) {
  const list = useReposConnectedList(source.status === "active" ? source.id : null, { q, orgId });
  const rows = (list.data?.items ?? []).slice(0, 5);
  return (
    <section className="flex flex-col gap-2" aria-label={connectedTitle(source)}>
      <div className="flex min-h-8 items-center gap-2 px-1">
        <h2 className="flex min-w-0 flex-1 items-center gap-2 type-label font-semibold text-fg-muted">
          {source.provider ? <ProviderIcon provider={source.provider} size={16} /> : null}
          <span className="truncate">{connectedTitle(source)}</span>
        </h2>
        {source.status === "active" ? (
          <Button size="sm" variant="ghost" onClick={onMore}>
            Show all
          </Button>
        ) : null}
      </div>
      <Card padding="none" className="p-1">
        {source.status !== "active" ? (
          <ReconnectState source={source} />
        ) : list.isPending ? (
          <SkeletonGroup label={`Loading ${connectedTitle(source)}`} className="flex flex-col">
            {Array.from({ length: 2 }, (_, i) => (
              <SkeletonRow key={i} />
            ))}
          </SkeletonGroup>
        ) : list.isError ? (
          <ConnectedError error={list.error} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<FolderGit2 />} title={q ? "No repository matches" : "No repositories on this account"} />
        ) : (
          <ul className="flex flex-col">
            {rows.map((r) => (
              <li key={r.id}>
                <RepoRow repo={r} />
              </li>
            ))}
          </ul>
        )}
      </Card>
    </section>
  );
}

/** One connected account's repositories: search, recent activity or name, and owner. */
function ConnectedRepoList({ source, q, orgId }: { source: ConnectedSourceRow; q: string; orgId: string | undefined }) {
  const [sort, setSort] = useState<"recent" | "name">("recent");
  const [owner, setOwner] = useState("");
  const list = useReposConnectedList(source.status === "active" ? source.id : null, { q, sort, owner, orgId });
  const rows = list.data?.items ?? [];
  const owners = list.meta?.owners ?? [];
  return (
    <section className="flex flex-col gap-3" aria-label={connectedTitle(source)}>
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="flex min-w-0 flex-1 items-center gap-2 type-heading text-fg">
          {source.provider ? <ProviderIcon provider={source.provider} size={20} /> : null}
          <span className="truncate">{connectedTitle(source)}</span>
        </h2>
        {owners.length > 1 ? (
          <Select
            aria-label="Owner"
            className="w-auto min-w-40"
            value={owner || "_all"}
            onValueChange={(v) => setOwner(v === "_all" ? "" : v)}
            options={[{ value: "_all", label: "Every owner" }, ...owners.map((o) => ({ value: o, label: o }))]}
          />
        ) : null}
        <Select
          aria-label="Sort"
          className="w-auto min-w-40"
          value={sort}
          onValueChange={(v) => setSort(v as "recent" | "name")}
          options={[
            { value: "recent", label: "Recent activity" },
            { value: "name", label: "Name" },
          ]}
        />
      </div>
      <Card padding="none" className="p-1">
        {source.status !== "active" ? (
          <ReconnectState source={source} />
        ) : list.isPending ? (
          <SkeletonGroup label="Loading repositories" className="flex flex-col">
            {Array.from({ length: 5 }, (_, i) => (
              <SkeletonRow key={i} />
            ))}
          </SkeletonGroup>
        ) : list.isError ? (
          <ConnectedError error={list.error} onRetry={() => void list.refetch()} retrying={list.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<FolderGit2 />} title={q || owner ? "No repository matches" : "No repositories on this account"} />
        ) : (
          <ul className="flex flex-col">
            {rows.map((r) => (
              <li key={r.id}>
                <RepoRow repo={r} />
              </li>
            ))}
          </ul>
        )}
      </Card>
      <LoadMore hasNextPage={!!list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
    </section>
  );
}

function ReconnectState({ source }: { source: ConnectedSourceRow }) {
  return (
    <EmptyState
      compact
      icon={<RefreshCw />}
      title={source.status === "blocked" ? "Your workspace doesn't allow this connection" : `Reconnect ${source.label} to see its repositories`}
      action={
        source.status !== "blocked" && source.fix_url ? (
          <Button asChild variant="outline">
            <Link href={source.fix_url}>Reconnect</Link>
          </Button>
        ) : undefined
      }
    />
  );
}

/** A platform error, with the fix when it's the connection's (reconnect, turn on). */
export function ConnectedError({ error, onRetry, retrying }: { error: unknown; onRetry: () => void; retrying?: boolean }) {
  const e = error as ApiError;
  const fix = typeof e?.details?.["fix_url"] === "string" ? (e.details["fix_url"] as string) : null;
  if (fix && isConnectionErrorCode(e.code)) {
    return (
      <EmptyState
        compact
        icon={<RefreshCw />}
        title={errorMessage(error, "Reconnect this account.")}
        action={
          <Button asChild variant="outline">
            <Link href={fix}>{e.code === "connection_needs_auth" ? "Reconnect" : "Open Connections"}</Link>
          </Button>
        }
      />
    );
  }
  return <ErrorState info={errorMessage(error, "Try again.")} onRetry={onRetry} retrying={retrying} />;
}

/**
 * New repository: in a folder (?folder=, from the drive's New menu) or at the top of your
 * drive; or ?from_folder= to turn an existing folder into a repository.
 */
export function NewRepoScreen() {
  const [fromFolder] = useUrlState("from_folder", "");
  return <div className="mx-auto w-full max-w-content px-gutter py-section">{fromFolder ? <ConvertFolder folderId={fromFolder} /> : <CreateRepo />}</div>;
}

const BRANCH_RE = /^[A-Za-z0-9._/-]{1,100}$/;

function CreateRepo() {
  const router = useRouter();
  const { org } = useActiveOrg();
  const [folder] = useUrlState("folder", "");
  const create = useCreateRepo();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [branch, setBranch] = useState("main");
  const [readme, setReadme] = useState(true);
  const submit = async () => {
    if (!org) return;
    try {
      const repo = await create.mutateAsync({ org_id: org.id, folder_id: folder || null, name: name.trim(), description: description.trim(), default_branch: branch.trim(), readme });
      toast.success(`Created ${repo.name}`);
      router.replace(`/repos/${repo.id}`);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't create the repository."));
    }
  };
  return (
    <Screen title="New repository" info={folder ? "It goes in the folder you came from and shares like it." : "It goes at the top of your drive, private to you until you share it."}>
      <form
        className="flex max-w-form flex-col gap-6"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Name" required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} autoFocus placeholder="route-board" />
        </Field>
        <Field label="Description">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={350} minRows={2} />
        </Field>
        <Field label="Default branch" info="Protected from the start: changes reach it through pull requests. Change the rule in the repository's settings." error={branch && !BRANCH_RE.test(branch) ? "Use letters, numbers, dots, dashes and slashes." : undefined}>
          <Input value={branch} onChange={(e) => setBranch(e.target.value)} className="font-mono" />
        </Field>
        <Checkbox checked={readme} onCheckedChange={setReadme} label="Start with a README" />
        <div className="flex gap-2">
          <Button type="submit" loading={create.isPending} disabled={!name.trim() || !org || !BRANCH_RE.test(branch)}>
            Create repository
          </Button>
          <Button type="button" variant="ghost" onClick={() => router.back()}>
            Cancel
          </Button>
        </div>
      </form>
    </Screen>
  );
}

function ConvertFolder({ folderId }: { folderId: string }) {
  const router = useRouter();
  const plan = useReposImportPreview(folderId);
  const convert = useImportReposFolder();
  const [branch, setBranch] = useState("main");
  const [message, setMessage] = useState("Import files from the drive");
  if (plan.isPending) {
    return (
      <SkeletonGroup label="Checking the folder" className="flex max-w-form flex-col gap-4">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="h-32 w-full" />
      </SkeletonGroup>
    );
  }
  if (plan.isError) {
    return (
      <Card>
        <ErrorState title="Couldn't check this folder" info={errorMessage(plan.error, "Try again.")} onRetry={() => void plan.refetch()} />
      </Card>
    );
  }
  const p = plan.data;
  const submit = async () => {
    try {
      const repo = await convert.mutateAsync({ folder_id: folderId, default_branch: branch.trim(), message: message.trim() });
      toast.success(`${repo.name} is now a repository`);
      router.replace(`/repos/${repo.id}`);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't turn the folder into a repository."));
    }
  };
  return (
    <Screen title="Turn into a repository">
      <div className="flex max-w-form flex-col gap-6">
        <Card className="flex flex-col gap-2 type-body">
          <p>
            <span className="font-semibold">{p.name}</span>: {p.files} {p.files === 1 ? "file" : "files"} ({reposFormatBytes(p.bytes)}) {p.files === 1 ? "becomes" : "become"} the first commit. The repository keeps the folder's name, place and sharing; the folder moves to the trash, where you can restore it.
          </p>
          {p.empty_folders > 0 ? <p className="type-label text-fg-muted">{p.empty_folders} empty {p.empty_folders === 1 ? "folder is" : "folders are"} left out (git stores files only).</p> : null}
          {p.skipped.length > 0 ? (
            <div className="flex flex-col gap-1">
              <p className="type-label text-fg-muted">Left out:</p>
              <ul className="flex flex-col gap-0.5">
                {p.skipped.map((s) => (
                  <li key={s.path} className="type-label">
                    <span className="font-mono">{s.path}</span> <span className="text-fg-muted">({s.reason})</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </Card>
        {p.blockers.map((b) => (
          <Banner key={b} tone="warning">
            {b}
          </Banner>
        ))}
        {p.can_convert ? (
          <form
            className="flex flex-col gap-6"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            <Field label="Default branch">
              <Input value={branch} onChange={(e) => setBranch(e.target.value)} className="font-mono" />
            </Field>
            <Field label="First commit message">
              <Input value={message} onChange={(e) => setMessage(e.target.value)} maxLength={200} />
            </Field>
            <div className="flex gap-2">
              <Button type="submit" loading={convert.isPending} disabled={!BRANCH_RE.test(branch) || !message.trim()}>
                Turn into repository
              </Button>
              <Button type="button" variant="ghost" onClick={() => router.back()}>
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <Button variant="outline" className="self-start" onClick={() => router.back()}>
            Back
          </Button>
        )}
      </div>
    </Screen>
  );
}
