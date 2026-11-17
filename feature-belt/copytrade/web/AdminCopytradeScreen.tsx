"use client";

import { copytradeStatusLabel, copytradeStopReasonLabel, useCopytradeAdminCopies, useCopytradeAdminOverview, useMe, useStopCopytradeAsStaff, type CopytradeStatus } from "@scaffoldapp/shared";
import { Banner, Button, Card, ConfirmDialog, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { Copy as CopyIcon } from "lucide-react";
import { useState } from "react";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { CopyStatusBadge, Stat, useExchangeMoney, when } from "./parts";

type Filter = "" | CopytradeStatus;

/** Admin → Copy trading: totals, the kill switch's state and every copy (staff may stop one). */
export function AdminCopytradeScreen() {
  const overview = useCopytradeAdminOverview();
  const [status, setStatus] = useUrlState<Filter>("status", "");
  const copies = useCopytradeAdminCopies(status);
  const stop = useStopCopytradeAsStaff();
  const me = useMe();
  const isAdmin = me.data?.admin_role === "admin";
  const { money } = useExchangeMoney();
  const [stopping, setStopping] = useState<string | null>(null);
  const o = overview.data;

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader title="Copy trading" info="Copies run where the exchange engine runs. Amounts are the last recorded values." />
      {o?.paused && <Banner tone="warning">The kill switch is on (COPYTRADE_PAUSED): no new copies and no copied orders. Stops and withdrawals still work.</Banner>}
      <Section title="Totals">
        {overview.isPending ? (
          <SkeletonGroup label="Loading totals" className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            {Array.from({ length: 8 }, (_, i) => (
              <Skeleton key={i} className="h-11" />
            ))}
          </SkeletonGroup>
        ) : overview.isError || !o ? (
          <ErrorState compact title="Couldn't load the totals" info={overview.error?.message} onRetry={() => void overview.refetch()} />
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <Stat label="Kill switch">{o.paused ? "On" : "Off"}</Stat>
            <Stat label="Copying">{o.active}</Stat>
            <Stat label="Paused">{o.paused_copies}</Stat>
            <Stat label="Stopping">{o.stopping}</Stat>
            <Stat label="Leaders">{o.leaders}</Stat>
            <Stat label="Followers">{o.followers}</Stat>
            <Stat label="Assets copying">{money(o.aum)}</Stat>
            <Stat label="Profit share paid">{money(o.profit_share_paid)}</Stat>
            <Stat label="Copied orders, 24h">{o.orders_24h}</Stat>
            <Stat label="Skips, 24h">{o.skips_24h}</Stat>
            <Stat label="Fills dropped">{o.running ? o.fills_dropped : "On another instance"}</Stat>
          </div>
        )}
      </Section>
      <Section title="Copies">
        <SegmentedControl
          options={[
            { value: "", label: "All" },
            { value: "active", label: copytradeStatusLabel("active") },
            { value: "paused", label: "Paused" },
            { value: "stopping", label: "Stopping" },
            { value: "stopped", label: "Stopped" },
          ]}
          value={status}
          onValueChange={(v) => setStatus(v || null)}
          className="self-start"
        />
        <Card padding="none">
          {copies.isPending ? (
            <TableSkeleton rows={5} cols={5} />
          ) : copies.isError ? (
            <ErrorState compact title="Couldn't load copies" info={copies.error.message} onRetry={() => void copies.refetch()} />
          ) : copies.data.items.length === 0 ? (
            <EmptyState compact icon={<CopyIcon />} title="No copies" />
          ) : (
            <Table label="Copies">
              <THead>
                <TH>Follower → leader</TH>
                <TH className="hidden sm:table-cell" align="right">
                  Moved in
                </TH>
                <TH className="hidden sm:table-cell" align="right">
                  Value
                </TH>
                <TH>Status</TH>
                <TH className="hidden md:table-cell">Started</TH>
                <TH align="right">
                  <span className="sr-only">Actions</span>
                </TH>
              </THead>
              <tbody>
                {copies.data.items.map((c) => (
                  <TR key={c.id}>
                    <TD>
                      <span className="block truncate font-semibold text-fg">{c.follower.display_name}</span>
                      <span className="block truncate text-fg-muted">→ {c.leader.display_name}</span>
                    </TD>
                    <TD className="hidden tabular-nums sm:table-cell" align="right">
                      {money(c.allocation)}
                    </TD>
                    <TD className="hidden tabular-nums sm:table-cell" align="right">
                      {money(c.equity)}
                    </TD>
                    <TD>
                      <span className="flex flex-col items-start gap-1">
                        <CopyStatusBadge status={c.status} />
                        {c.stop_reason && <span className="type-caption text-fg-muted">{copytradeStopReasonLabel(c.stop_reason)}</span>}
                      </span>
                    </TD>
                    <TD className="hidden md:table-cell">{when(c.created_at)}</TD>
                    <TD align="right">
                      {isAdmin && (c.status === "active" || c.status === "paused") && (
                        <Button variant="ghost" size="sm" onClick={() => setStopping(c.id)}>
                          Stop
                        </Button>
                      )}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
        {copies.data && <LoadMore hasNextPage={copies.hasNextPage} isFetchingNextPage={copies.isFetchingNextPage} fetchNextPage={copies.fetchNextPage} />}
      </Section>
      <ConfirmDialog
        open={!!stopping}
        onOpenChange={(open) => !open && setStopping(null)}
        title="Stop this copy?"
        info="Its positions close at market and its funds return to the follower. The follower is told; this is recorded in the audit log."
        confirmLabel="Stop copy"
        tone="danger"
        onConfirm={async () => {
          if (!stopping) return;
          await stop.mutateAsync(stopping);
          toast.success("Copy stopped");
          setStopping(null);
        }}
      />
    </div>
  );
}
