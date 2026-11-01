"use client";

import { errorMessage, useBlockedUsers, useMyReports, useUnblockUser } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, ListRow, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Flag, ShieldCheck } from "lucide-react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { kindLabel, OUTCOME, reasonLabel } from "./labels";

function RowsSkeleton({ label }: { label: string }) {
  return (
    <SkeletonGroup label={label} className="flex flex-col">
      <SkeletonRow />
      <SkeletonRow />
      <SkeletonRow />
    </SkeletonGroup>
  );
}

function BlockedPeople() {
  const blocks = useBlockedUsers();
  const unblock = useUnblockUser();
  const rows = blocks.data?.items ?? [];
  return (
    <Section title="Blocked people" info="Blocked people can't message you, and you don't see each other's content. They aren't told.">
      <Card padding="none" className="p-1">
        {blocks.isPending ? (
          <RowsSkeleton label="Loading blocked people" />
        ) : blocks.isError ? (
          <ErrorState compact title="Couldn't load blocked people" info={blocks.error.message} onRetry={() => void blocks.refetch()} retrying={blocks.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<ShieldCheck />} title="No one blocked" />
        ) : (
          <ul className="flex flex-col">
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
        )}
        <LoadMore hasNextPage={blocks.hasNextPage} isFetchingNextPage={blocks.isFetchingNextPage} fetchNextPage={blocks.fetchNextPage} />
      </Card>
    </Section>
  );
}

function YourReports() {
  const reports = useMyReports();
  const rows = reports.data?.items ?? [];
  return (
    <Section title="Your reports" info="Moderators review every report. You hear when one is closed, but not what action was taken.">
      <Card padding="none" className="p-1">
        {reports.isPending ? (
          <RowsSkeleton label="Loading your reports" />
        ) : reports.isError ? (
          <ErrorState compact title="Couldn't load your reports" info={reports.error.message} onRetry={() => void reports.refetch()} retrying={reports.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<Flag />} title="No reports yet" />
        ) : (
          <ul className="flex flex-col">
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
        )}
        <LoadMore hasNextPage={reports.hasNextPage} isFetchingNextPage={reports.isFetchingNextPage} fetchNextPage={reports.fetchNextPage} />
      </Card>
    </Section>
  );
}

/** Settings → Safety: people you blocked (unblock here) and the reports you sent. */
export function SafetyScreen() {
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Safety" />
      <BlockedPeople />
      <YourReports />
    </div>
  );
}
