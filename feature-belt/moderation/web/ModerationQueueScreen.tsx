"use client";

import { useModerationCases, useModerationSummary, type CaseStatusFilter, type ModerationCase, type ReportReason } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { Inbox, SearchX } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { ACTION_LABEL, formatAge, kindLabel, reasonLabel } from "./labels";

type Tab = "open" | "closed";

function Stat({ label, value, tone }: { label: string; value: string; tone?: "danger" }) {
  return (
    <div className="flex flex-col gap-1">
      <dt className="type-caption text-fg-muted">{label}</dt>
      <dd className={tone === "danger" ? "type-title-sm tabular-nums text-danger-strong" : "type-title-sm tabular-nums text-fg"}>{value}</dd>
    </div>
  );
}

function Summary() {
  const summary = useModerationSummary();
  if (summary.isPending) {
    return (
      <SkeletonGroup label="Loading queue health" className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-14" />
        ))}
      </SkeletonGroup>
    );
  }
  if (summary.isError) return null;
  const s = summary.data;
  return (
    <Card>
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Stat label="Open" value={String(s.open)} />
        <Stat label="Over 24 hours" value={String(s.over_sla)} {...(s.over_sla > 0 ? { tone: "danger" as const } : {})} />
        <Stat label="Oldest open" value={s.oldest_open_age_seconds === null ? "None" : formatAge(s.oldest_open_age_seconds)} />
        <Stat label="Median close, 7 days" value={s.median_close_seconds_7d === null ? "None" : formatAge(s.median_close_seconds_7d)} />
      </dl>
    </Card>
  );
}

function topReason(c: ModerationCase): ReportReason | null {
  let best: ReportReason | null = null;
  let n = 0;
  for (const [reason, count] of Object.entries(c.reasons) as [ReportReason, number][]) {
    if (count > n) {
      best = reason;
      n = count;
    }
  }
  return best;
}

function CaseRow({ c }: { c: ModerationCase }) {
  const router = useRouter();
  const href = `/admin/moderation/${c.id}`;
  const author = c.author;
  const authorName = author ? author.display_name || (author.status === "deleted" ? "Deleted user" : author.email) : "No author";
  const reason = topReason(c);
  const text = c.snapshot.text || (c.target_kind === "user" ? authorName : "No text");
  return (
    <TR onActivate={() => router.push(href)}>
      <TD className="max-w-96">
        <span className="flex min-w-0 flex-col gap-1">
          <span className="flex items-center gap-2">
            <Badge tone="neutral">{kindLabel(c.target_kind)}</Badge>
            {c.hidden && <Badge tone="warning">Hidden</Badge>}
          </span>
          <Link href={href} className="line-clamp-2 type-body text-fg underline-offset-2 hover:underline">
            {text}
          </Link>
        </span>
      </TD>
      <TD className="hidden md:table-cell">
        <span className="flex min-w-0 items-center gap-2">
          <AppAvatar name={authorName} src={author?.avatar_url ?? null} size="xs" />
          <span className="truncate text-fg-muted">{authorName}</span>
        </span>
      </TD>
      <TD className="hidden sm:table-cell">{reason ? reasonLabel(reason) : ""}</TD>
      <TD align="right">{c.report_count}</TD>
      <TD align="right">
        {c.status === "open" ? (
          <Badge tone={c.over_sla ? "danger" : "neutral"} {...(c.over_sla ? { dot: true } : {})}>
            {formatAge(c.age_seconds)}
          </Badge>
        ) : (
          <Badge tone={c.status === "dismissed" ? "neutral" : "success"}>{c.action ? ACTION_LABEL[c.action] : "Closed"}</Badge>
        )}
      </TD>
    </TR>
  );
}

/** Admin → Moderation: open cases oldest first with their age against the 24-hour target; closed cases newest first. */
export function ModerationQueueScreen() {
  const [tab, setTab] = useUrlState<Tab>("status", "open");
  const [authorId] = useUrlState("author_id", "");
  const status: CaseStatusFilter = tab === "open" ? "open" : "closed";
  const cases = useModerationCases({ status, ...(authorId ? { author_id: authorId } : {}), limit: 50 });
  const rows = cases.data?.items ?? [];

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Moderation" info="Reports from people and content are grouped by what was reported. Work open cases from the top: the target is under 24 hours." />
      <Summary />
      <div className="flex flex-col gap-4">
        <SegmentedControl<Tab>
          aria-label="Cases"
          className="self-start"
          value={tab}
          onValueChange={setTab}
          options={[
            { value: "open", label: "Open" },
            { value: "closed", label: "Closed" },
          ]}
        />
        <Card padding="none">
          {cases.isPending ? (
            <TableSkeleton rows={6} cols={4} />
          ) : cases.isError ? (
            <ErrorState compact title="Couldn't load cases" info={cases.error.message} onRetry={() => void cases.refetch()} retrying={cases.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={authorId ? <SearchX /> : <Inbox />} title={tab === "open" ? "No open cases" : "No closed cases"} />
          ) : (
            <>
              <Table label="Moderation cases">
                <THead>
                  <TH>Reported</TH>
                  <TH className="hidden md:table-cell">Author</TH>
                  <TH className="hidden sm:table-cell">Top reason</TH>
                  <TH align="right">Reports</TH>
                  <TH align="right" info={tab === "open" ? "Time since the first report. Red: over 24 hours." : undefined}>
                    {tab === "open" ? "Waiting" : "Outcome"}
                  </TH>
                </THead>
                <tbody>
                  {rows.map((c) => (
                    <CaseRow key={c.id} c={c} />
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={cases.hasNextPage} isFetchingNextPage={cases.isFetchingNextPage} fetchNextPage={cases.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
