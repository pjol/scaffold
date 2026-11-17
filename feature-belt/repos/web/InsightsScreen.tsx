"use client";

import { errorMessage, reposRelativeTime, useReposInsights, type ReposContributor, type ReposInsights } from "@scaffoldapp/shared";
import { Card, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import { BarChart3 } from "lucide-react";
import { useState } from "react";
import { Person } from "./parts";
import { useRepoContext } from "./RepoLayout";

/** Insights: issue and pull request totals, weekly commits on the default branch, and contributors. */
export function InsightsScreen() {
  const repo = useRepoContext();
  const insights = useReposInsights(repo.id);
  if (insights.isPending) {
    return (
      <SkeletonGroup label="Loading insights" className="flex flex-col gap-section">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-5">
          {Array.from({ length: 5 }, (_, i) => (
            <Skeleton key={i} className="h-20" />
          ))}
        </div>
        <Skeleton className="h-56 w-full" />
        <Skeleton className="h-64 w-full" />
      </SkeletonGroup>
    );
  }
  if (insights.isError) {
    return (
      <Card>
        <ErrorState info={errorMessage(insights.error, "Try again.")} onRetry={() => void insights.refetch()} retrying={insights.isFetching} />
      </Card>
    );
  }
  const d = insights.data;
  return (
    <div className="flex flex-col gap-section">
      <section aria-label="Totals" className="grid grid-cols-2 gap-3 sm:grid-cols-5">
        <Stat label="Open issues" value={d.open_issues} />
        <Stat label="Closed issues" value={d.closed_issues} />
        <Stat label="Open pull requests" value={d.open_pulls} />
        <Stat label="Merged pull requests" value={d.merged_pulls} />
        <Stat label={`Commits on ${d.branch}`} value={d.total_commits} suffix={d.truncated ? "+" : ""} />
      </section>
      {d.total_commits === 0 ? (
        <Card>
          <EmptyState icon={<BarChart3 />} title="No commits yet" info={`Activity shows once ${d.branch} has commits.`} />
        </Card>
      ) : (
        <>
          <WeeklyCommits insights={d} />
          <Contributors list={d.contributors} branch={d.branch} truncated={d.truncated} />
        </>
      )}
    </div>
  );
}

function Stat({ label, value, suffix = "" }: { label: string; value: number; suffix?: string }) {
  return (
    <Card padding="compact" className="flex flex-col gap-1">
      <span className="type-caption text-fg-muted">{label}</span>
      <span className="type-title font-semibold text-fg tabular-nums">
        {value.toLocaleString()}
        {suffix}
      </span>
    </Card>
  );
}

function weekLabel(iso: string): string {
  return new Date(`${iso}T00:00:00Z`).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" });
}

function niceMax(n: number): number {
  if (n <= 4) return Math.max(n, 1);
  const step = Math.pow(10, Math.floor(Math.log10(n)));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * step >= n) return m * step;
  return 10 * step;
}

/** 52 weekly columns, one series (no legend), a hover/focus readout and a table for screen readers. */
function WeeklyCommits({ insights: d }: { insights: ReposInsights }) {
  const weeks = d.weeks;
  const max = niceMax(Math.max(...weeks.map((w) => w.commits)));
  const [active, setActive] = useState<number | null>(null);
  const total = weeks.reduce((a, w) => a + w.commits, 0);
  const shown = active;
  return (
    <section className="flex flex-col gap-3" aria-labelledby="repos-insights-weekly">
      <div className="flex min-h-control flex-wrap items-center gap-2">
        <h2 id="repos-insights-weekly" className="flex flex-1 items-center gap-2 type-title-sm text-fg">
          Commits per week
          <InfoTip content={`On ${d.branch} over the last 52 weeks, merge commits left out. Hover or use the arrow keys for a week's count.`} />
        </h2>
        <span className="type-label text-fg-muted tabular-nums">{total.toLocaleString()} in the last year</span>
      </div>
      <Card className="flex flex-col gap-2">
        <p aria-live="polite" className="min-h-6 type-label text-fg-muted">
          {shown !== null && weeks[shown] ? (
            <>
              Week of {weekLabel(weeks[shown].week)}: <span className="font-semibold text-fg tabular-nums">{weeks[shown].commits}</span> {weeks[shown].commits === 1 ? "commit" : "commits"}
            </>
          ) : (
            " "
          )}
        </p>
        <div className="flex gap-2">
          <div className="flex h-40 w-8 shrink-0 flex-col justify-between text-right type-caption text-fg-muted tabular-nums" aria-hidden>
            <span className="-translate-y-1/2">{max}</span>
            <span>{Number.isInteger(max / 2) ? max / 2 : ""}</span>
            <span className="translate-y-1/2">0</span>
          </div>
          <div
            role="group"
            aria-label="Commits per week, last 52 weeks"
            tabIndex={0}
            onKeyDown={(e) => {
              if (e.key === "ArrowRight") {
                e.preventDefault();
                setActive((a) => Math.min((a ?? weeks.length - 1) + (a === null ? 0 : 1), weeks.length - 1));
              } else if (e.key === "ArrowLeft") {
                e.preventDefault();
                setActive((a) => Math.max((a ?? weeks.length - 1) - (a === null ? 0 : 1), 0));
              } else if (e.key === "Escape") {
                setActive(null);
              }
            }}
            onBlur={() => setActive(null)}
            onMouseLeave={() => setActive(null)}
            className="relative flex h-40 min-w-0 flex-1 items-end gap-0.5 rounded-xs border-b border-border focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
          >
            <span className="pointer-events-none absolute inset-x-0 top-0 border-t border-border" aria-hidden />
            <span className="pointer-events-none absolute inset-x-0 top-1/2 border-t border-border" aria-hidden />
            {weeks.map((w, i) => {
              const h = w.commits === 0 ? 0 : Math.max(4, (w.commits / max) * 100);
              return (
                <div key={w.week} className="relative flex h-full min-w-0 flex-1 items-end justify-center" onMouseEnter={() => setActive(i)} aria-hidden>
                  <div
                    className={cn("w-full max-w-6 rounded-t-xs transition-colors motion-reduce:transition-none", active === i ? "bg-primary-strong" : "bg-primary")}
                    style={{ height: `${h}%` }}
                  />
                </div>
              );
            })}
          </div>
        </div>
        <div className="flex justify-between pl-10 type-caption text-fg-muted" aria-hidden>
          <span>{weekLabel(weeks[0]?.week ?? "")}</span>
          <span>{weekLabel(weeks[weeks.length - 1]?.week ?? "")}</span>
        </div>
        <table className="sr-only">
          <caption>Commits per week</caption>
          <thead>
            <tr>
              <th scope="col">Week of</th>
              <th scope="col">Commits</th>
            </tr>
          </thead>
          <tbody>
            {weeks.map((w) => (
              <tr key={w.week}>
                <td>{weekLabel(w.week)}</td>
                <td>{w.commits}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </section>
  );
}

function Contributors({ list, branch, truncated }: { list: ReposContributor[]; branch: string; truncated: boolean }) {
  return (
    <section className="flex flex-col gap-3" aria-labelledby="repos-insights-people">
      <h2 id="repos-insights-people" className="flex items-center gap-2 type-title-sm text-fg">
        Contributors
        <InfoTip content={`Authors of commits on ${branch}, most commits first.${truncated ? " Counted over the latest 20,000 commits." : ""} Commits made in the app show the person.`} />
      </h2>
      <Card padding="none" className="overflow-x-auto">
        <table className="w-full border-collapse type-label">
          <thead className="bg-sunken text-left text-fg-muted">
            <tr>
              <th scope="col" className="px-4 py-2 font-semibold">
                Who
              </th>
              <th scope="col" className="px-4 py-2 text-right font-semibold">
                Commits
              </th>
              <th scope="col" className="px-4 py-2 text-right font-semibold">
                Lines
              </th>
              <th scope="col" className="hidden px-4 py-2 font-semibold sm:table-cell">
                First
              </th>
              <th scope="col" className="hidden px-4 py-2 font-semibold min-[480px]:table-cell">
                Latest
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {list.map((c, i) => (
              <tr key={`${c.name}-${i}`}>
                <td className="max-w-56 px-4 py-2">
                  {c.user ? <Person user={c.user} /> : <span className="block truncate font-semibold text-fg">{c.name}</span>}
                </td>
                <td className="px-4 py-2 text-right tabular-nums text-fg">{c.commits.toLocaleString()}</td>
                <td className="whitespace-nowrap px-4 py-2 text-right tabular-nums">
                  <span className="text-success-strong">+{c.additions.toLocaleString()}</span> <span className="text-danger-strong">−{c.deletions.toLocaleString()}</span>
                </td>
                <td className="hidden whitespace-nowrap px-4 py-2 text-fg-muted sm:table-cell">{new Date(c.first).toLocaleDateString()}</td>
                <td className="hidden whitespace-nowrap px-4 py-2 text-fg-muted min-[480px]:table-cell">{reposRelativeTime(c.last)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
    </section>
  );
}
