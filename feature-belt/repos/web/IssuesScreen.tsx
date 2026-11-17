"use client";

import { errorMessage, reposRelativeTime, useReposIssues, useReposLabels, useReposMilestones, type ReposIssue } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Input, Menu, MenuChoice, MenuSeparator, Skeleton, SkeletonGroup, Tooltip, cn } from "@scaffoldapp/ui-web";
import { ChevronDown, CircleDot, GitPullRequest, Milestone, MessageSquare, Pin, Plus, Search, Tag, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { IssueStateIcon, LabelList } from "./issueParts";
import { repoHref, useRepoContext } from "./RepoLayout";

const DEFAULT_QUERY = "is:open";

/** Split a query into tokens, keeping quoted qualifier values together. */
function tokens(q: string): string[] {
  return q.match(/(?:[^\s"]+|"[^"]*")+/g) ?? [];
}

/** Set (or remove with null) one qualifier in a query; `multi` keeps other values and toggles this one (label:). */
export function withQualifier(q: string, key: string, value: string | null, multi = false): string {
  const quoted = value && /\s/.test(value) ? `"${value}"` : value;
  const kept = tokens(q).filter((t) => {
    const k = t.split(":")[0]?.toLowerCase();
    if (k !== key) return true;
    if (!multi) return false;
    return value === null ? true : t.toLowerCase() !== `${key}:${quoted}`.toLowerCase();
  });
  const has = tokens(q).some((t) => t.toLowerCase() === `${key}:${quoted}`.toLowerCase());
  if (value !== null && !(multi && has)) kept.push(`${key}:${quoted}`);
  return kept.join(" ");
}

function hasQualifier(q: string, key: string, value: string): boolean {
  const quoted = /\s/.test(value) ? `"${value}"` : value;
  return tokens(q).some((t) => t.toLowerCase() === `${key}:${quoted}`.toLowerCase());
}

/** Issues: the search syntax in the box, Open/Closed totals, filters that write qualifiers, pinned issues on top. */
export function IssuesScreen() {
  const repo = useRepoContext();
  const [urlQ, setUrlQ] = useUrlState("q", DEFAULT_QUERY);
  const [q, setQ] = useState(urlQ);
  useEffect(() => setQ(urlQ), [urlQ]);
  const issues = useReposIssues(repo.id, q);
  const pinned = useReposIssues(repo.id, "is:pinned is:open");
  const labels = useReposLabels(repo.id);
  const milestones = useReposMilestones(repo.id, "open");
  const rows = issues.data?.items ?? [];
  const counts = issues.meta?.counts;
  const apply = (next: string) => {
    setQ(next);
    setUrlQ(next === DEFAULT_QUERY ? null : next);
  };
  const state = hasQualifier(q, "is", "closed") ? "closed" : hasQualifier(q, "is", "open") ? "open" : "all";
  const showPinned = q.trim() === DEFAULT_QUERY && (pinned.data?.items.length ?? 0) > 0;
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <form
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            apply(q.trim());
          }}
        >
          <Input
            aria-label="Search issues"
            placeholder='is:open label:bug assignee:@me milestone:"v1.0"'
            leadingIcon={<Search aria-hidden />}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onBlur={() => apply(q.trim())}
            trailing={
              q.trim() !== DEFAULT_QUERY ? (
                <button type="button" aria-label="Clear filters" className="flex size-8 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken hover:text-fg [&_svg]:size-4" onClick={() => apply(DEFAULT_QUERY)}>
                  <X aria-hidden />
                </button>
              ) : undefined
            }
          />
        </form>
        <div className="flex flex-wrap items-center gap-2">
          <Button asChild variant="outline" size="sm" leftIcon={<Tag />}>
            <Link href={repoHref(repo.id, "labels")}>Labels</Link>
          </Button>
          <Button asChild variant="outline" size="sm" leftIcon={<Milestone />}>
            <Link href={repoHref(repo.id, "milestones")}>Milestones</Link>
          </Button>
          {repo.my_level !== "view" ? (
            <Button asChild size="sm" leftIcon={<Plus />}>
              <Link href={repoHref(repo.id, "issues/new")}>New issue</Link>
            </Button>
          ) : null}
        </div>
      </div>

      {showPinned ? (
        <ul aria-label="Pinned issues" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {pinned.data!.items.map((is) => (
            <li key={is.id}>
              <Link href={repoHref(repo.id, `issues/${is.number}`)} className="flex h-full flex-col gap-2 rounded-lg border border-border bg-surface p-3 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                <span className="flex items-center gap-2 type-caption text-fg-muted">
                  <Pin className="size-3.5" aria-hidden />
                  Pinned · #{is.number}
                </span>
                <span className="flex items-start gap-2 type-body font-semibold text-fg">
                  <IssueStateIcon state={is.state} reason={is.state_reason} className="mt-0.5 size-4 shrink-0" />
                  {is.title}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : null}

      <Card padding="none">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-border px-4 py-2">
          <button
            type="button"
            aria-pressed={state === "open"}
            className={cn("flex min-h-tap-sm items-center gap-2 type-label", state === "open" ? "font-semibold text-fg" : "text-fg-muted hover:text-fg")}
            onClick={() => apply(withQualifier(q, "is", "open").replace(/\bis:closed\b/g, "").trim())}
          >
            <CircleDot className="size-4" aria-hidden />
            <span className="tabular-nums">{counts?.open ?? "…"}</span> Open
          </button>
          <button
            type="button"
            aria-pressed={state === "closed"}
            className={cn("flex min-h-tap-sm items-center gap-2 type-label", state === "closed" ? "font-semibold text-fg" : "text-fg-muted hover:text-fg")}
            onClick={() => apply(withQualifier(q.replace(/\bis:open\b/g, ""), "is", "closed").trim())}
          >
            <span className="tabular-nums">{counts?.closed ?? "…"}</span> Closed
          </button>
          <div className="ml-auto flex flex-wrap items-center gap-1">
            <Menu
              trigger={
                <Button variant="ghost" size="sm" rightIcon={<ChevronDown />}>
                  Author
                </Button>
              }
            >
              <MenuChoice checked={hasQualifier(q, "author", "@me")} onSelect={() => apply(withQualifier(q, "author", hasQualifier(q, "author", "@me") ? null : "@me"))}>
                Opened by you
              </MenuChoice>
              <MenuChoice checked={hasQualifier(q, "mentions", "@me")} onSelect={() => apply(withQualifier(q, "mentions", hasQualifier(q, "mentions", "@me") ? null : "@me"))}>
                Mentioning you
              </MenuChoice>
            </Menu>
            <Menu
              trigger={
                <Button variant="ghost" size="sm" rightIcon={<ChevronDown />}>
                  Label
                </Button>
              }
            >
              {(labels.data?.items ?? []).map((l) => (
                <MenuChoice key={l.id} checked={hasQualifier(q, "label", l.name)} onSelect={() => apply(withQualifier(q, "label", l.name, true))}>
                  {l.name}
                </MenuChoice>
              ))}
              {(labels.data?.items ?? []).length ? <MenuSeparator /> : null}
              <MenuChoice checked={hasQualifier(q, "no", "label")} onSelect={() => apply(withQualifier(q, "no", hasQualifier(q, "no", "label") ? null : "label"))}>
                No label
              </MenuChoice>
            </Menu>
            <Menu
              trigger={
                <Button variant="ghost" size="sm" rightIcon={<ChevronDown />}>
                  Milestone
                </Button>
              }
            >
              {(milestones.data?.items ?? []).map((m) => (
                <MenuChoice key={m.id} checked={hasQualifier(q, "milestone", m.title)} onSelect={() => apply(withQualifier(q, "milestone", hasQualifier(q, "milestone", m.title) ? null : m.title))}>
                  {m.title}
                </MenuChoice>
              ))}
              {(milestones.data?.items ?? []).length ? <MenuSeparator /> : null}
              <MenuChoice checked={hasQualifier(q, "no", "milestone")} onSelect={() => apply(withQualifier(q, "no", hasQualifier(q, "no", "milestone") ? null : "milestone"))}>
                No milestone
              </MenuChoice>
            </Menu>
            <Menu
              trigger={
                <Button variant="ghost" size="sm" rightIcon={<ChevronDown />}>
                  Assignee
                </Button>
              }
            >
              <MenuChoice checked={hasQualifier(q, "assignee", "@me")} onSelect={() => apply(withQualifier(q, "assignee", hasQualifier(q, "assignee", "@me") ? null : "@me"))}>
                Assigned to you
              </MenuChoice>
              <MenuChoice checked={hasQualifier(q, "no", "assignee")} onSelect={() => apply(withQualifier(q, "no", hasQualifier(q, "no", "assignee") ? null : "assignee"))}>
                Nobody assigned
              </MenuChoice>
            </Menu>
            <Menu
              trigger={
                <Button variant="ghost" size="sm" rightIcon={<ChevronDown />}>
                  Sort
                </Button>
              }
            >
              {(
                [
                  ["created", "Newest"],
                  ["created-asc", "Oldest"],
                  ["updated", "Recently updated"],
                  ["comments", "Most commented"],
                ] as const
              ).map(([v, label]) => (
                <MenuChoice key={v} checked={hasQualifier(q, "sort", v) || (v === "created" && !/\bsort:/.test(q))} onSelect={() => apply(withQualifier(q, "sort", v === "created" ? null : v))}>
                  {label}
                </MenuChoice>
              ))}
            </Menu>
          </div>
        </div>
        {issues.meta?.ignored.length ? (
          <p className="border-b border-border px-4 py-2 type-caption text-fg-muted">Not understood, so ignored: {issues.meta.ignored.join(", ")}</p>
        ) : null}
        {issues.isPending ? (
          <SkeletonGroup label="Loading issues" className="flex flex-col divide-y divide-border">
            {Array.from({ length: 4 }, (_, i) => (
              <div key={i} className="flex min-h-tap-lg items-center gap-3 px-4">
                <Skeleton className="size-5" shape="circle" />
                <Skeleton className="h-4" width={`${40 + i * 10}%`} />
              </div>
            ))}
          </SkeletonGroup>
        ) : issues.isError ? (
          <ErrorState info={errorMessage(issues.error, "Try again.")} onRetry={() => void issues.refetch()} retrying={issues.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<CircleDot />}
            title={q.trim() === DEFAULT_QUERY ? "No open issues" : "Nothing matches"}
            action={
              repo.my_level !== "view" && q.trim() === DEFAULT_QUERY ? (
                <Button asChild variant="outline">
                  <Link href={repoHref(repo.id, "issues/new")}>New issue</Link>
                </Button>
              ) : q.trim() !== DEFAULT_QUERY ? (
                <Button variant="outline" onClick={() => apply(DEFAULT_QUERY)}>
                  Clear filters
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ul className="divide-y divide-border">
            {rows.map((is) => (
              <IssueRow key={is.id} repoId={repo.id} issue={is} onFilter={(next) => apply(next)} q={q} />
            ))}
          </ul>
        )}
      </Card>
      <LoadMore hasNextPage={!!issues.hasNextPage} isFetchingNextPage={issues.isFetchingNextPage} fetchNextPage={issues.fetchNextPage} />
    </div>
  );
}

function IssueRow({ repoId, issue: is, q, onFilter }: { repoId: string; issue: ReposIssue; q: string; onFilter: (q: string) => void }) {
  const when = is.state === "closed" && is.closed_at ? `closed ${reposRelativeTime(is.closed_at)}` : `opened ${reposRelativeTime(is.created_at)} by ${is.author?.display_name || "Deleted user"}`;
  return (
    <li className="flex items-start gap-3 px-4 py-3 hover:bg-sunken">
      <IssueStateIcon state={is.state} reason={is.state_reason} className="mt-0.5 size-5 shrink-0" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <Link href={repoHref(repoId, `issues/${is.number}`)} className="type-body font-semibold text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
            {is.title}
          </Link>
          {is.labels.map((l) => (
            <button key={l.id} type="button" className="rounded-pill focus-visible:outline-2 focus-visible:outline-ring" aria-label={`Filter by ${l.name}`} onClick={() => onFilter(withQualifier(q, "label", l.name, true))}>
              <LabelList labels={[l]} />
            </button>
          ))}
        </div>
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1 type-caption text-fg-muted">
          <span>#{is.number}</span>
          <span>{when}</span>
          {is.milestone ? (
            <span className="flex items-center gap-1">
              <Milestone className="size-3.5" aria-hidden />
              {is.milestone.title}
            </span>
          ) : null}
          {is.locked ? <span>Locked</span> : null}
        </span>
      </div>
      <span className="flex shrink-0 items-center gap-3 type-caption text-fg-muted">
        {is.linked_pulls > 0 ? (
          <Tooltip content={`${is.linked_pulls} linked pull ${is.linked_pulls === 1 ? "request" : "requests"}`}>
            <span className="flex items-center gap-1">
              <GitPullRequest className="size-4" aria-hidden />
              <span className="tabular-nums">{is.linked_pulls}</span>
            </span>
          </Tooltip>
        ) : null}
        {is.assignees.length ? (
          <span className="hidden -space-x-1.5 sm:flex" aria-label={`Assigned to ${is.assignees.map((a) => a.display_name).join(", ")}`}>
            {is.assignees.slice(0, 3).map((a) => (
              <AppAvatar key={a.id} name={a.display_name} src={a.avatar_url ?? null} size="xs" />
            ))}
          </span>
        ) : null}
        {is.comment_count > 0 ? (
          <span className="flex items-center gap-1">
            <MessageSquare className="size-4" aria-hidden />
            <span className="tabular-nums">{is.comment_count}</span>
            <span className="sr-only">comments</span>
          </span>
        ) : null}
      </span>
    </li>
  );
}
