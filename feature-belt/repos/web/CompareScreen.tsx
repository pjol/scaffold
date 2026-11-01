"use client";

import { errorMessage, hasErrorCode, reposRelativeTime, reposShortSha, useOpenReposPull, useReposCompare, useReposPeople, type ApiError, type ReposComparison, type ReposPerson } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, Checkbox, EmptyState, ErrorState, Field, Input, TextArea, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Check, GitCompareArrows, GitPullRequest, Search, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useUrlState } from "@/lib/url-state";
import { DiffModeToggle, DiffSkeleton, DiffView, useDiffMode } from "./DiffView";
import { Person } from "./parts";
import { RefPicker } from "./RefPicker";
import { repoHref, useRepoContext } from "./RepoLayout";

/** base...head: what merging head into base brings, and the form to open a pull request. */
export function CompareScreen() {
  const repo = useRepoContext();
  const router = useRouter();
  const [base] = useUrlState("base", repo.default_branch);
  const [head] = useUrlState("head", "");
  const [expand] = useUrlState("expand", "");
  const cmp = useReposCompare(repo.id, base, head);
  const [mode, setMode] = useDiffMode();
  const go = (b: string, h: string) => router.replace(repoHref(repo.id, "compare", { base: b, head: h, expand }));

  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-3">
        <h2 className="type-title-sm text-fg">Compare changes</h2>
        <div className="flex flex-wrap items-center gap-2">
          <RefPicker repoId={repo.id} value={base} onChange={(b) => go(b, head)} label="Base" branchesOnly />
          <ArrowLeft className="size-4 text-fg-muted" aria-label="takes changes from" />
          <RefPicker repoId={repo.id} value={head || "Choose a branch"} onChange={(h) => go(base, h)} label="Compare" branchesOnly />
          {cmp.data ? <MergeHint cmp={cmp.data} /> : null}
        </div>
      </div>
      {!head ? (
        <Card>
          <EmptyState icon={<GitCompareArrows />} title="Choose a branch to compare" />
        </Card>
      ) : head === base ? (
        <Card>
          <EmptyState icon={<GitCompareArrows />} title="Choose two different branches" />
        </Card>
      ) : cmp.isPending ? (
        <DiffSkeleton />
      ) : cmp.isError ? (
        <Card>
          <ErrorState
            title={hasErrorCode(cmp.error, "unrelated_histories") ? "Nothing to compare" : (cmp.error as ApiError).status === 404 ? "Branch not found" : "Couldn't compare"}
            info={errorMessage(cmp.error, "Try again.")}
            onRetry={() => void cmp.refetch()}
          />
        </Card>
      ) : cmp.data.ahead === 0 ? (
        <Card>
          <EmptyState icon={<Check />} title={`${base} already has everything on ${head}`} />
        </Card>
      ) : (
        <>
          {cmp.data.open_pull ? (
            <Banner
              tone="info"
              action={
                <Button asChild size="sm" variant="outline">
                  <Link href={repoHref(repo.id, `pulls/${cmp.data.open_pull.number}`)}>View #{cmp.data.open_pull.number}</Link>
                </Button>
              }
            >
              {cmp.data.open_pull.title} already proposes these changes.
            </Banner>
          ) : repo.can_push ? (
            <OpenPullForm cmp={cmp.data} startOpen={expand === "1"} />
          ) : null}
          <section className="flex flex-col gap-3">
            <h2 className="type-title-sm text-fg">
              {cmp.data.ahead} {cmp.data.ahead === 1 ? "commit" : "commits"}
            </h2>
            <Card padding="none" className="divide-y divide-border">
              {cmp.data.commits.map((c) => (
                <div key={c.sha} className="flex min-h-row items-center gap-3 px-4 py-2 type-label">
                  <Link href={repoHref(repo.id, `commits/${c.sha}`)} className="min-w-0 flex-1 truncate font-semibold text-fg hover:underline">
                    {c.subject}
                  </Link>
                  <span className="hidden truncate text-fg-muted sm:inline">{c.author.name}</span>
                  <span className="type-caption text-fg-muted">{reposRelativeTime(c.committer.at)}</span>
                  <span className="font-mono type-caption text-fg-muted">{reposShortSha(c.sha)}</span>
                </div>
              ))}
            </Card>
          </section>
          <section className="flex flex-col gap-3">
            <div className="flex items-center gap-3">
              <h2 className="type-title-sm text-fg">
                {cmp.data.diff.files.length} {cmp.data.diff.files.length === 1 ? "file" : "files"} changed
              </h2>
              <span className="type-label">
                <span className="text-success-strong">+{cmp.data.diff.additions}</span> <span className="text-danger-strong">−{cmp.data.diff.deletions}</span>
              </span>
              <div className="ml-auto">
                <DiffModeToggle mode={mode} onChange={setMode} />
              </div>
            </div>
            <DiffView files={cmp.data.diff.files} mode={mode} repoId={repo.id} headRef={head} filesTruncated={cmp.data.diff.files_truncated} />
          </section>
        </>
      )}
    </div>
  );
}

function MergeHint({ cmp }: { cmp: ReposComparison }) {
  if (cmp.ahead === 0 || cmp.mergeable === null) return null;
  return cmp.mergeable ? (
    <Badge tone="success" icon={<Check />}>
      Can merge
    </Badge>
  ) : (
    <Badge tone="warning" icon={<X />}>
      Conflicts in {cmp.conflicts.length} {cmp.conflicts.length === 1 ? "file" : "files"}
    </Badge>
  );
}

function OpenPullForm({ cmp, startOpen }: { cmp: ReposComparison; startOpen: boolean }) {
  const repo = useRepoContext();
  const router = useRouter();
  const open = useOpenReposPull(repo.id);
  const [expanded, setExpanded] = useState(startOpen);
  const first = cmp.commits.length === 1 ? cmp.commits[0] : undefined;
  const [title, setTitle] = useState(first?.subject ?? cmp.head.replace(/[-_/]+/g, " ").replace(/^\w/, (c) => c.toUpperCase()));
  const [body, setBody] = useState(first?.body ?? "");
  const [reviewers, setReviewers] = useState<ReposPerson[]>([]);
  useEffect(() => setExpanded(startOpen), [startOpen]);

  if (!expanded) {
    return (
      <Card className="flex flex-wrap items-center gap-3">
        <GitPullRequest className="size-5 text-fg-muted" aria-hidden />
        <span className="min-w-0 flex-1 type-body text-fg">
          Propose <span className="font-mono">{cmp.head}</span> for <span className="font-mono">{cmp.base}</span>
        </span>
        <Button onClick={() => setExpanded(true)}>Open a pull request</Button>
      </Card>
    );
  }
  const submit = async () => {
    try {
      const p = await open.mutateAsync({ title: title.trim(), body, head: cmp.head, base: cmp.base, reviewers: reviewers.map((r) => r.id) });
      toast.success(`Opened #${p.number}`);
      router.push(repoHref(repo.id, `pulls/${p.number}`));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't open the pull request."));
    }
  };
  return (
    <Card>
      <form
        className="flex flex-col gap-4 lg:grid lg:grid-cols-[minmax(0,1fr)_16rem] lg:gap-8"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <div className="flex flex-col gap-4">
          <Field label="Title">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={256} autoFocus />
          </Field>
          <Field label="Description" info="Markdown works here. Say what changed and why, and how you checked it.">
            <TextArea value={body} onChange={(e) => setBody(e.target.value)} minRows={6} maxRows={20} />
          </Field>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => setExpanded(false)}>
              Cancel
            </Button>
            <Button type="submit" loading={open.isPending} disabled={!title.trim()}>
              Open pull request
            </Button>
          </div>
        </div>
        <ReviewerPicker repoId={repo.id} value={reviewers} onChange={setReviewers} />
      </form>
    </Card>
  );
}

/** Choose reviewers among people who can open the repository. */
export function ReviewerPicker({ repoId, value, onChange, exclude = [] }: { repoId: string; value: ReposPerson[]; onChange: (v: ReposPerson[]) => void; exclude?: string[] }) {
  const [q, setQ] = useState("");
  const people = useReposPeople(repoId, q);
  const chosen = new Set(value.map((p) => p.id));
  const options = (people.data?.items ?? []).filter((p) => !exclude.includes(p.id));
  return (
    <div className="flex flex-col gap-3">
      <h3 className="type-label font-semibold text-fg">Reviewers</h3>
      <Input aria-label="Find people" placeholder="Find people" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} />
      <ul className="flex max-h-56 flex-col overflow-y-auto">
        {options.map((p) => (
          <li key={p.id} className="flex min-h-control items-center gap-2 px-1">
            <Checkbox
              checked={chosen.has(p.id)}
              onCheckedChange={(on) => onChange(on ? [...value, p] : value.filter((v) => v.id !== p.id))}
              label={<Person user={{ id: p.id, display_name: p.display_name, avatar_url: p.avatar_url }} />}
            />
            {p.level === "view" ? <Badge tone="neutral">view only</Badge> : null}
          </li>
        ))}
        {options.length === 0 && !people.isPending ? <li className="px-1 py-2 type-label text-fg-muted">No one matches.</li> : null}
      </ul>
    </div>
  );
}
