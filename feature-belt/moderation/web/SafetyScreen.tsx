"use client";

import { errorMessage, useBlockedUsers, useModerationContent, useModerationStanding, useMyReports, useUnblockUser } from "@scaffoldapp/shared";
import { Badge, Banner, Button, EmptyState, ErrorState, ListGroup, ListRow, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Flag, ShieldCheck, Sparkles } from "lucide-react";
import Link from "next/link";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate, formatDateTime } from "@/lib/format";
import { CONTENT_STATE, categoryLabel, kindLabel, OUTCOME, reasonLabel } from "./labels";

function RowsSkeleton({ label }: { label: string }) {
  return (
    <SkeletonGroup label={label} className="flex flex-col">
      <SkeletonRow />
      <SkeletonRow />
      <SkeletonRow />
    </SkeletonGroup>
  );
}

function Standing() {
  const standing = useModerationStanding();
  const s = standing.data;
  const ladder = s?.ladder.map((l) => `${l.strikes} strikes: ${l.hours >= 48 ? `${Math.round(l.hours / 24)} days` : `${l.hours} hours`}`).join(" · ");
  return (
    <Section title="Strikes" info={`A removal or ban by a moderator adds a strike. Strikes count for ${s?.window_days ?? 90} days.${ladder ? ` Posting stops for a while at ${ladder}.` : ""}`}>
      {standing.isPending ? (
        <RowsSkeleton label="Loading strikes" />
      ) : standing.isError ? (
        <ErrorState compact title="Couldn't load your strikes" info={standing.error.message} onRetry={() => void standing.refetch()} retrying={standing.isFetching} />
      ) : (
        <div className="flex flex-col gap-3">
          {s?.restricted_until ? <Banner tone="warning">{`You can post again on ${formatDateTime(s.restricted_until)}.`}</Banner> : null}
          {s && s.strikes.length > 0 ? (
            <ListGroup>
              <ul>
                {s.strikes.map((k) => (
                  <li key={k.id}>
                    <ListRow
                      title={k.action === "ban" ? "Account banned" : k.category ? `Removed: ${categoryLabel(k.category)}` : "Content removed"}
                      trailing={<span className="type-caption">{`Counts until ${formatDate(k.expires_at)}`}</span>}
                    />
                  </li>
                ))}
              </ul>
            </ListGroup>
          ) : (
            <EmptyState compact icon={<ShieldCheck />} title="No strikes" />
          )}
        </div>
      )}
    </Section>
  );
}

function YourContent() {
  const content = useModerationContent();
  const rows = content.data?.items ?? [];
  return (
    <Section title="Held and removed" info="What you posted that screening held for review or a moderator removed, with the reason. Held items are visible only to you until a moderator approves them.">
      {content.isPending ? (
        <RowsSkeleton label="Loading held and removed content" />
      ) : content.isError ? (
        <ErrorState compact title="Couldn't load your content" info={content.error.message} onRetry={() => void content.refetch()} retrying={content.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<Sparkles />} title="Nothing held or removed" />
      ) : (
        <ListGroup>
          <ul>
            {rows.map((c) => (
              <li key={c.case_id} className="flex flex-col gap-1 py-3">
                <span className="flex flex-wrap items-center gap-2">
                  <span className="type-label font-semibold text-fg">{kindLabel(c.target_kind)}</span>
                  <Badge tone={CONTENT_STATE[c.state].tone}>{CONTENT_STATE[c.state].label}</Badge>
                  <span className="ml-auto type-caption text-fg-muted">{formatDate(c.created_at)}</span>
                </span>
                {c.text ? <p className="line-clamp-3 max-w-prose break-words type-body text-fg">{c.text}</p> : null}
                {c.reasons.length > 0 ? <p className="type-label text-fg-muted">{c.reasons.join(". ")}</p> : null}
                {c.link && c.state !== "removed" ? (
                  <Link href={c.link} className="self-start rounded-sm type-label font-semibold underline underline-offset-2">
                    Open
                  </Link>
                ) : null}
              </li>
            ))}
          </ul>
        </ListGroup>
      )}
      <LoadMore hasNextPage={content.hasNextPage} isFetchingNextPage={content.isFetchingNextPage} fetchNextPage={content.fetchNextPage} />
    </Section>
  );
}

function BlockedPeople() {
  const blocks = useBlockedUsers();
  const unblock = useUnblockUser();
  const rows = blocks.data?.items ?? [];
  return (
    <Section title="Blocked people" info="Blocked people can't message you, and you don't see each other's content. They aren't told.">
      {blocks.isPending ? (
        <RowsSkeleton label="Loading blocked people" />
      ) : blocks.isError ? (
        <ErrorState compact title="Couldn't load blocked people" info={blocks.error.message} onRetry={() => void blocks.refetch()} retrying={blocks.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<ShieldCheck />} title="No one blocked" />
      ) : (
        <ListGroup>
          <ul>
            {rows.map((b) => (
              <li key={b.user_id}>
                <ListRow
                  title={b.display_name || "Unnamed"}
                  leading={<AppAvatar name={b.display_name || "?"} src={b.avatar_url} size="sm" />}
                  trailing={
                    <Button
                      variant="secondary"
                      size="sm"
                      loading={unblock.isPending && unblock.variables === b.user_id}
                      onClick={() =>
                        unblock.mutate(b.user_id, {
                          onSuccess: () => toast.success(`${b.display_name || "They"} unblocked`),
                          onError: (e) => toast.error(errorMessage(e, "Couldn't unblock. Try again.")),
                        })
                      }
                    >
                      Unblock
                    </Button>
                  }
                />
              </li>
            ))}
          </ul>
        </ListGroup>
      )}
      <LoadMore hasNextPage={blocks.hasNextPage} isFetchingNextPage={blocks.isFetchingNextPage} fetchNextPage={blocks.fetchNextPage} />
    </Section>
  );
}

function YourReports() {
  const reports = useMyReports();
  const rows = reports.data?.items ?? [];
  return (
    <Section title="Your reports" info="Moderators review every report. You hear when one is closed, but not what action was taken.">
      {reports.isPending ? (
        <RowsSkeleton label="Loading your reports" />
      ) : reports.isError ? (
        <ErrorState compact title="Couldn't load your reports" info={reports.error.message} onRetry={() => void reports.refetch()} retrying={reports.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<Flag />} title="No reports yet" />
      ) : (
        <ListGroup>
          <ul>
            {rows.map((r) => (
              <li key={r.id}>
                <ListRow
                  title={`${kindLabel(r.target_kind)} · ${reasonLabel(r.reason)}`}
                  trailing={
                    <span className="flex items-center gap-3">
                      <span className="hidden type-caption text-fg-muted sm:inline">{formatDate(r.created_at)}</span>
                      <Badge tone={OUTCOME[r.outcome].tone}>{OUTCOME[r.outcome].label}</Badge>
                    </span>
                  }
                />
              </li>
            ))}
          </ul>
        </ListGroup>
      )}
      <LoadMore hasNextPage={reports.hasNextPage} isFetchingNextPage={reports.isFetchingNextPage} fetchNextPage={reports.fetchNextPage} />
    </Section>
  );
}

/** Settings → Safety: your strikes, what screening held or moderators removed, people you blocked, and the reports you sent. */
export function SafetyScreen() {
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Safety" />
      <Standing />
      <YourContent />
      <BlockedPeople />
      <YourReports />
    </div>
  );
}
