"use client";

import { useAdminAudit, useAdminOverview } from "@scaffoldapp/shared";
import { AnimatedNumber, Button, Card, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, Stagger, StaggerItem } from "@scaffoldapp/ui-web";
import { ScrollText } from "lucide-react";
import Link from "next/link";
import { AuditTable } from "@/components/admin/AuditTable";
import { TableSkeleton } from "@/components/admin/Table";
import { Screen, Section } from "@/components/shell/PageHeader";

function Stat({ label, value, info }: { label: string; value: number; info: string }) {
  return (
    <Card className="flex h-full flex-col gap-2">
      <span className="flex items-center gap-2 type-label text-fg-muted">
        {label}
        <InfoTip content={info} />
      </span>
      <AnimatedNumber value={value} className="type-headline text-fg" />
    </Card>
  );
}

/** Admin → Overview: account counts and the latest admin actions. */
export function OverviewScreen() {
  const overview = useAdminOverview();
  const audit = useAdminAudit({ limit: 5 });
  const recent = audit.data?.items ?? [];

  return (
    <Screen title="Overview">
      {overview.isPending ? (
        <SkeletonGroup label="Loading counts" className="grid gap-4 sm:grid-cols-3">
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
        </SkeletonGroup>
      ) : overview.isError ? (
        <Card padding="compact">
          <ErrorState compact title="Couldn't load the counts" info={overview.error.message} onRetry={() => void overview.refetch()} retrying={overview.isFetching} />
        </Card>
      ) : (
        <Stagger as="ul" className="grid gap-4 sm:grid-cols-3" aria-label="Accounts">
          <StaggerItem as="li">
            <Stat label="Accounts" value={overview.data.users} info="Every account that isn't deleted." />
          </StaggerItem>
          <StaggerItem as="li">
            <Stat label="New in 7 days" value={overview.data.new_7d} info="Accounts created in the last 7 days." />
          </StaggerItem>
          <StaggerItem as="li">
            <Stat label="Suspended or banned" value={overview.data.suspended} info="Accounts that can't make changes (suspended) or sign in (banned)." />
          </StaggerItem>
        </Stagger>
      )}
      <Section
        title="Recent admin actions"
        action={
          <Button asChild variant="ghost" size="sm">
            <Link href="/admin/audit">Audit log</Link>
          </Button>
        }
      >
        <Card padding="none">
          {audit.isPending ? (
            <TableSkeleton rows={3} cols={4} />
          ) : audit.isError ? (
            <ErrorState compact title="Couldn't load recent actions" info={audit.error.message} onRetry={() => void audit.refetch()} retrying={audit.isFetching} />
          ) : recent.length === 0 ? (
            <EmptyState compact icon={<ScrollText />} title="No admin actions yet" />
          ) : (
            <AuditTable entries={recent} />
          )}
        </Card>
      </Section>
    </Screen>
  );
}
