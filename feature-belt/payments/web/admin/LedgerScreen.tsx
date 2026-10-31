"use client";

import { useAdminLedger } from "@scaffoldapp/shared";
import { Card, EmptyState } from "@scaffoldapp/ui-web";
import { ScrollText } from "lucide-react";
import { TableSkeleton } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { AdminError } from "./AdminUnavailable";
import { LedgerTable } from "./LedgerTable";
import { PaymentsAdminTabs } from "./PaymentsAdminTabs";

/** Admin → Payments → Ledger: every money movement, newest first, with refunds. */
export function LedgerScreen() {
  const ledger = useAdminLedger({ limit: 50 });
  const rows = ledger.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Payments" info="The ledger is append-only: a refund is a new negative entry, never an edit." />
      <div className="flex flex-col gap-4">
        <PaymentsAdminTabs />
        <Card padding="none">
          {ledger.isPending ? (
            <TableSkeleton rows={8} cols={5} />
          ) : ledger.isError ? (
            <AdminError error={ledger.error} title="Couldn't load the ledger" onRetry={() => void ledger.refetch()} retrying={ledger.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<ScrollText />} title="No money movements yet" />
          ) : (
            <>
              <LedgerTable rows={rows} showUser />
              <LoadMore hasNextPage={ledger.hasNextPage} isFetchingNextPage={ledger.isFetchingNextPage} fetchNextPage={ledger.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
