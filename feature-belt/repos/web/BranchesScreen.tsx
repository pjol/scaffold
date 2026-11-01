"use client";

import { errorMessage, reposRelativeTime, useCreateReposBranch, useDeleteReposBranch, useReposRefs, type ReposRef } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Skeleton, SkeletonGroup, Tooltip, toast } from "@scaffoldapp/ui-web";
import { GitBranch, GitCompareArrows, GitPullRequest, Plus, Search, ShieldCheck, Tag, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { RefPicker } from "./RefPicker";
import { repoHref, useRepoContext } from "./RepoLayout";

/** Branches with how far each is from the default branch; create, compare and delete them. Tags below. */
export function BranchesScreen() {
  const repo = useRepoContext();
  const refs = useReposRefs(repo.id, true);
  const [q, setQ] = useState("");
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<ReposRef | null>(null);
  const del = useDeleteReposBranch(repo.id);
  const all = refs.data?.items ?? [];
  const match = (r: ReposRef) => r.name.toLowerCase().includes(q.toLowerCase());
  const branches = all.filter((r) => r.kind === "branch" && match(r)).sort((a, b) => Number(b.default) - Number(a.default));
  const tags = all.filter((r) => r.kind === "tag" && match(r));

  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-wrap items-center gap-3">
        <Input aria-label="Find a branch" placeholder="Find a branch" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="w-full sm:w-72" />
        {repo.can_push && !repo.empty ? (
          <Button size="auto" leftIcon={<Plus />} className="sm:ml-auto" onClick={() => setCreating(true)}>
            New branch
          </Button>
        ) : null}
      </div>
      {refs.isPending ? (
        <SkeletonGroup label="Loading branches" className="flex flex-col gap-2">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </SkeletonGroup>
      ) : refs.isError ? (
        <Card>
          <ErrorState info={errorMessage(refs.error, "Try again.")} onRetry={() => void refs.refetch()} retrying={refs.isFetching} />
        </Card>
      ) : branches.length === 0 && tags.length === 0 ? (
        <Card>
          <EmptyState icon={<GitBranch />} title={q ? "No branch matches" : "No branches yet"} />
        </Card>
      ) : (
        <>
          <section className="flex flex-col gap-3">
            <h2 className="type-title-sm text-fg">Branches</h2>
            <Card padding="none" className="divide-y divide-border">
              {branches.map((b) => (
                <div key={b.name} className="flex min-h-tap-lg flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2">
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="flex min-w-0 items-center gap-2">
                      <Link href={repoHref(repo.id, "", { ref: b.name })} className="truncate font-mono type-label font-semibold text-fg hover:underline">
                        {b.name}
                      </Link>
                      {b.default ? <Badge tone="primary">default</Badge> : null}
                      {b.protected ? (
                        <Tooltip content="Protected: changes land through pull requests">
                          <span className="text-fg-muted">
                            <ShieldCheck className="size-4" aria-label="Protected" />
                          </span>
                        </Tooltip>
                      ) : null}
                    </span>
                    <span className="truncate type-caption text-fg-muted">
                      {b.author} · {b.subject} · {reposRelativeTime(b.updated_at)}
                    </span>
                  </div>
                  {!b.default && b.ahead !== null && b.behind !== null ? (
                    <Tooltip content={`${b.behind} behind and ${b.ahead} ahead of ${repo.default_branch}`}>
                      <span className="type-caption tabular-nums text-fg-muted">
                        {b.behind} behind · {b.ahead} ahead
                      </span>
                    </Tooltip>
                  ) : null}
                  <div className="flex items-center gap-1">
                    {b.open_pull ? (
                      <Button asChild variant="ghost" size="sm" leftIcon={<GitPullRequest />}>
                        <Link href={repoHref(repo.id, `pulls/${b.open_pull}`)}>#{b.open_pull}</Link>
                      </Button>
                    ) : !b.default ? (
                      <Button asChild variant="ghost" size="sm" leftIcon={<GitCompareArrows />}>
                        <Link href={repoHref(repo.id, "compare", { base: repo.default_branch, head: b.name })}>Compare</Link>
                      </Button>
                    ) : null}
                    {repo.can_push && !b.default && !b.protected ? (
                      <IconButton label={`Delete ${b.name}`} icon={<Trash2 />} variant="danger" size="sm" tooltip onClick={() => setDeleting(b)} />
                    ) : null}
                  </div>
                </div>
              ))}
            </Card>
          </section>
          {tags.length > 0 ? (
            <section className="flex flex-col gap-3">
              <h2 className="type-title-sm text-fg">Tags</h2>
              <Card padding="none" className="divide-y divide-border">
                {tags.map((t) => (
                  <Link key={t.name} href={repoHref(repo.id, "", { ref: t.name })} className="flex min-h-row items-center gap-3 px-4 hover:bg-sunken">
                    <Tag className="size-4 text-fg-muted" aria-hidden />
                    <span className="font-mono type-label text-fg">{t.name}</span>
                    <span className="ml-auto truncate type-caption text-fg-muted">{reposRelativeTime(t.updated_at)}</span>
                  </Link>
                ))}
              </Card>
            </section>
          ) : null}
        </>
      )}
      <NewBranchDialog open={creating} onOpenChange={setCreating} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete ${deleting?.name ?? "branch"}?`}
        tone="danger"
        confirmLabel="Delete branch"
        onConfirm={async () => {
          if (!deleting) return;
          await del.mutateAsync(deleting.name);
          toast.success(`Deleted ${deleting.name}`);
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the branch.")}
      >
        Its commits stay reachable from any pull request that used it. Open pull requests from it close.
      </ConfirmDialog>
    </div>
  );
}

function NewBranchDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const repo = useRepoContext();
  const create = useCreateReposBranch(repo.id);
  const [name, setName] = useState("");
  const [from, setFrom] = useState(repo.default_branch);
  const submit = async () => {
    try {
      const b = await create.mutateAsync({ name: name.trim(), from });
      toast.success(`Created ${b.name}`);
      setName("");
      onOpenChange(false);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't create the branch."));
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New branch"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button loading={create.isPending} disabled={!name.trim()} onClick={() => void submit()}>
            Create branch
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Name">
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="feature/new-board" className="font-mono" />
        </Field>
        <Field label="Start from">
          <RefPicker repoId={repo.id} value={from} onChange={setFrom} label="Start from" />
        </Field>
      </form>
    </Dialog>
  );
}
