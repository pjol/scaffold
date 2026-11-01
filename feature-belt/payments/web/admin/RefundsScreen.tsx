"use client";

import { formatMinor, useAdminRefunds } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState, InfoTip } from "@scaffoldapp/ui-web";
import { Undo2 } from "lucide-react";
import Link from "next/link";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDateTime } from "@/lib/format";
import { REFUND_REASONS, REFUND_STATUS } from "../labels";
import { AdminError } from "./AdminUnavailable";
import { PaymentsAdminTabs } from "./PaymentsAdminTabs";

const reasonLabel = (r: string) => REFUND_REASONS.find((x) => x.value === r)?.label ?? "";

/** Admin → Payments → Refunds: staff refunds and ones made in the provider's dashboard. */
export function RefundsScreen() {
  const refunds = useAdminRefunds({ limit: 50 });
  const rows = refunds.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Payments" />
      <div className="flex flex-col gap-4">
        <PaymentsAdminTabs />
        <Card padding="none">
          {refunds.isPending ? (
            <TableSkeleton rows={6} cols={4} />
          ) : refunds.isError ? (
            <AdminError error={refunds.error} title="Couldn't load refunds" onRetry={() => void refunds.refetch()} retrying={refunds.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<Undo2 />} title="No refunds yet" />
          ) : (
            <>
              <Table label="Refunds" className="min-w-160">
                <THead>
                  <TH>Amount</TH>
                  <TH>Status</TH>
                  <TH className="hidden md:table-cell">Reason</TH>
                  <TH className="hidden sm:table-cell">By</TH>
                  <TH align="right">When</TH>
                </THead>
                <tbody>
                  {rows.map((r) => (
                    <TR key={r.id}>
                      <TD className="font-semibold tabular-nums">
                        <span className="flex items-center gap-1">
                          <Link href={`/admin/payments/customers/${r.user_id}`} className="underline-offset-2 hover:underline">
                            {formatMinor(r.amount_minor, r.currency)}
                          </Link>
                          {r.note && <InfoTip content={r.note} />}
                        </span>
                      </TD>
                      <TD>
                        <Badge tone={REFUND_STATUS[r.status].tone}>{REFUND_STATUS[r.status].label}</Badge>
                      </TD>
                      <TD className="hidden text-fg-muted md:table-cell">{reasonLabel(r.reason) || "—"}</TD>
                      <TD className="hidden max-w-48 truncate text-fg-muted sm:table-cell">{r.requested_by_email ?? "Provider dashboard"}</TD>
                      <TD align="right" className="whitespace-nowrap text-fg-muted">
                        {formatDateTime(r.created_at)}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={refunds.hasNextPage} isFetchingNextPage={refunds.isFetchingNextPage} fetchNextPage={refunds.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
