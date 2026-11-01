"use client";

import {
  errorMessage,
  reposFormatBytes,
  reposRelativeTime,
  useActiveOrg,
  useCreateRepo,
  useImportReposFolder,
  useReposImportPreview,
  useReposList,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, Checkbox, EmptyState, ErrorState, Field, Input, ListRow, Skeleton, SkeletonGroup, SkeletonRow, TextArea, toast } from "@scaffoldapp/ui-web";
import { Building2, FolderGit2, GitBranch, GitPullRequest, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";

/** Repositories in the active workspace you can open, most recently changed first. */
export function ReposListScreen() {
  const { org, isPending } = useActiveOrg();
  const [q, setQ] = useState("");
  const repos = useReposList(org?.id, q);
  const rows = repos.data?.items ?? [];
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
        info="Git repositories live in the drive and share like folders. Clone them with a git credential from Settings."
        action={
          <Button asChild size="auto" leftIcon={<Plus />}>
            <Link href="/repos/new">New repository</Link>
          </Button>
        }
      >
        <section className="flex flex-col gap-4">
          <Input aria-label="Search repositories" placeholder="Search repositories" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="sm:w-72" />
          <Card padding="none" className="p-1">
            {repos.isPending || isPending ? (
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
                    <ListRow
                      asChild
                      leading={
                        <span className="flex size-10 items-center justify-center rounded-md bg-sunken text-fg-muted">
                          <GitBranch className="size-5" aria-hidden />
                        </span>
                      }
                      title={
                        <span className="flex min-w-0 flex-col">
                          <span className="truncate font-semibold">{r.name}</span>
                          <span className="truncate type-caption text-fg-muted">
                            {r.default_branch} · {r.pushed_at ? `changed ${reposRelativeTime(r.pushed_at)}` : "no commits yet"}
                          </span>
                        </span>
                      }
                      trailing={
                        r.open_pulls > 0 ? (
                          <span className="flex items-center gap-1 type-caption text-fg-muted">
                            <GitPullRequest className="size-4" aria-hidden />
                            {r.open_pulls}
                            <span className="sr-only">open pull requests</span>
                          </span>
                        ) : undefined
                      }
                    >
                      <Link href={`/repos/${r.id}`} />
                    </ListRow>
                  </li>
                ))}
              </ul>
            )}
          </Card>
          <LoadMore hasNextPage={!!repos.hasNextPage} isFetchingNextPage={repos.isFetchingNextPage} fetchNextPage={repos.fetchNextPage} />
        </section>
      </Screen>
    </div>
  );
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
