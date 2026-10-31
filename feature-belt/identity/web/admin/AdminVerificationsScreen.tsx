"use client";

import { errorMessage, useAdminVerifications, type IdentityState } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState, ErrorState, Select } from "@scaffoldapp/ui-web";
import { Inbox } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

type Filter = "queue" | Exclude<IdentityState, "none">;

const FILTERS: { value: Filter; label: string }[] = [
  { value: "queue", label: "Waiting for review" },
  { value: "verified", label: "Verified" },
  { value: "rejected", label: "Rejected" },
  { value: "pending", label: "Started" },
  { value: "expired", label: "Expired" },
];

export const STATE_LABEL: Record<Exclude<IdentityState, "none">, string> = { pending: "Pending", verified: "Verified", rejected: "Rejected", expired: "Expired" };

const STATE_TONE: Record<Exclude<IdentityState, "none">, "warning" | "success" | "danger" | "neutral"> = {
  pending: "warning",
  verified: "success",
  rejected: "danger",
  expired: "neutral",
};

/** Admin → Verification: submitted attempts awaiting review (oldest first) and the history by status. */
export function AdminVerificationsScreen() {
  const router = useRouter();
  const [filter, setFilter] = useUrlState<Filter>("status", "queue");
  const list = useAdminVerifications(filter === "queue" ? { limit: 50 } : { status: filter, limit: 50 });
  const rows = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Verification" info="Photos are deleted as soon as you approve or reject." />
      <div className="flex flex-col gap-4">
        <Select aria-label="Status" options={FILTERS} value={filter} onValueChange={(v) => setFilter(v as Filter)} className="sm:w-56" />
        <Card padding="none">
          {list.isPending ? (
            <TableSkeleton rows={5} cols={4} />
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load verifications" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<Inbox />} title={filter === "queue" ? "Nothing to review" : "None"} />
          ) : (
            <>
              <Table label="Verifications">
                <THead>
                  <TH>User</TH>
                  <TH className="hidden sm:table-cell">Method</TH>
                  <TH>Status</TH>
                  <TH align="right">{filter === "queue" ? "Submitted" : "Started"}</TH>
                </THead>
                <tbody>
                  {rows.map((v) => (
                    <TR key={v.id} onActivate={() => router.push(`/admin/identity/${v.id}`)}>
                      <TD>
                        <Link href={`/admin/identity/${v.id}`} className="font-semibold text-fg underline-offset-2 hover:underline">
                          {v.user.display_name || v.user.email}
                        </Link>
                      </TD>
                      <TD className="hidden text-fg-muted sm:table-cell">{v.provider === "persona" ? "Persona" : "Photos"}</TD>
                      <TD>
                        <Badge tone={STATE_TONE[v.status]}>{v.status === "pending" ? (v.submitted_at ? "To review" : "Started") : STATE_LABEL[v.status]}</Badge>
                      </TD>
                      <TD align="right" className="whitespace-nowrap text-fg-muted">
                        {formatDate((filter === "queue" ? v.submitted_at : null) ?? v.created_at)}
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
              <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
