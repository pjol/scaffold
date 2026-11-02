"use client";

import { errorMessage, reposPullNoun, reposRelativeTime, useReposPulls, type ReposPull, type ReposPullFilter } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Input, SegmentedControl, Skeleton, SkeletonGroup, Tab, Tabs, TabsList } from "@scaffoldapp/ui-web";
import { Check, GitMerge, GitPullRequest, GitPullRequestClosed, MessageSquare, Plus, Search } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { repoHref, useRepoContext } from "./RepoLayout";

type Mine = "" | "author" | "review_requested";

/** Pull requests: open, closed or all; yours or waiting for your review; search by title or number. */
export function PullsScreen() {
  const repo = useRepoContext();
  const [state, setState] = useUrlState<ReposPullFilter>("state", "open");
  const [mine, setMine] = useUrlState<Mine | "all">("filter", "all");
  const [q, setQ] = useState("");
  const pulls = useReposPulls(repo.id, { state, q, mine: mine === "all" ? "" : mine });
  const rows = pulls.data?.items ?? [];
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <Tabs value={state} onValueChange={(v) => setState(v as ReposPullFilter)} variant="pill">
          <TabsList aria-label="State">
            <Tab value="open">Open</Tab>
            <Tab value="closed">Closed</Tab>
            <Tab value="all">All</Tab>
          </TabsList>
        </Tabs>
        <SegmentedControl<Mine | "all">
          aria-label="Whose"
          fullWidth={false}
          value={mine}
          onValueChange={setMine}
          options={[
            { value: "all", label: "Everyone's" },
            { value: "author", label: "Yours" },
            { value: "review_requested", label: "To review" },
          ]}
        />
        <Input aria-label={`Search ${reposPullNoun(repo, true).toLowerCase()}`} placeholder="Search by title or #" leadingIcon={<Search aria-hidden />} value={q} onChange={(e) => setQ(e.target.value)} wrapperClassName="md:ml-auto md:w-64" />
        {repo.can_push ? (
          <Button asChild size="auto" leftIcon={<Plus />}>
            <Link href={repoHref(repo.id, "compare", { base: repo.default_branch })}>New {reposPullNoun(repo).toLowerCase()}</Link>
          </Button>
        ) : null}
      </div>
      <Card padding="none">
        {pulls.isPending ? (
          <SkeletonGroup label="Loading pull requests" className="flex flex-col divide-y divide-border">
            {Array.from({ length: 4 }, (_, i) => (
              <div key={i} className="flex min-h-tap-lg items-center gap-3 px-4">
                <Skeleton className="size-5" shape="circle" />
                <Skeleton className="h-4" width={`${40 + i * 10}%`} />
              </div>
            ))}
          </SkeletonGroup>
        ) : pulls.isError ? (
          <ErrorState info={errorMessage(pulls.error, "Try again.")} onRetry={() => void pulls.refetch()} retrying={pulls.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<GitPullRequest />}
            title={q ? "Nothing matches" : state === "open" ? `No open ${reposPullNoun(repo, true).toLowerCase()}` : `No ${reposPullNoun(repo, true).toLowerCase()}`}
            action={
              repo.can_push && state === "open" && !q ? (
                <Button asChild variant="outline">
                  <Link href={repoHref(repo.id, "compare", { base: repo.default_branch })}>Compare branches</Link>
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ul className="divide-y divide-border">
            {rows.map((p) => (
              <PullRow key={p.id} repoId={repo.id} pull={p} />
            ))}
          </ul>
        )}
      </Card>
      <LoadMore hasNextPage={!!pulls.hasNextPage} isFetchingNextPage={pulls.isFetchingNextPage} fetchNextPage={pulls.fetchNextPage} />
    </div>
  );
}

export function PullStateIcon({ state, className = "size-5" }: { state: ReposPull["state"]; className?: string }) {
  if (state === "merged") return <GitMerge className={`${className} text-accent-strong`} aria-label="Merged" />;
  if (state === "closed") return <GitPullRequestClosed className={`${className} text-danger-strong`} aria-label="Closed" />;
  return <GitPullRequest className={`${className} text-success-strong`} aria-label="Open" />;
}

function PullRow({ repoId, pull: p }: { repoId: string; pull: ReposPull }) {
  const when = p.state === "merged" && p.merged_at ? `merged ${reposRelativeTime(p.merged_at)}` : p.state === "closed" && p.closed_at ? `closed ${reposRelativeTime(p.closed_at)}` : `opened ${reposRelativeTime(p.created_at)}`;
  return (
    <li>
      <Link href={repoHref(repoId, `pulls/${p.number}`)} className="flex min-h-tap-lg items-start gap-3 px-4 py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
        <PullStateIcon state={p.state} className="mt-0.5 size-5 shrink-0" />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="type-body font-semibold text-fg">{p.title}</span>
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1 type-caption text-fg-muted">
            <span>#{p.number}</span>
            <span>
              {when} by {p.author?.display_name || "Deleted user"}
            </span>
            <span className="font-mono">
              {p.head_branch} → {p.base_branch}
            </span>
          </span>
        </span>
        <span className="flex shrink-0 items-center gap-3 type-caption text-fg-muted">
          {p.changes_requests > 0 ? <Badge tone="warning">Changes requested</Badge> : p.approvals > 0 ? <Badge tone="success" icon={<Check />}>{p.approvals}</Badge> : null}
          {p.comment_count > 0 ? (
            <span className="flex items-center gap-1">
              <MessageSquare className="size-4" aria-hidden />
              <span className="tabular-nums">{p.comment_count}</span>
              <span className="sr-only">comments</span>
            </span>
          ) : null}
        </span>
      </Link>
    </li>
  );
}
