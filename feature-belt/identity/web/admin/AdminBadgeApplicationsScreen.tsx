"use client";

import { errorMessage, useIdentityAdminBadgeApplications, type IdentityApplicationStatus } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState, ErrorState, Select, type BadgeTone } from "@scaffoldapp/ui-web";
import { Inbox } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { IdentityAdminTabs } from "./IdentityAdminTabs";

const FILTERS: { value: IdentityApplicationStatus; label: string }[] = [
  { value: "submitted", label: "Waiting for review" },
  { value: "needs_info", label: "Waiting for the applicant" },
  { value: "approved", label: "Approved" },
  { value: "rejected", label: "Rejected" },
  { value: "withdrawn", label: "Withdrawn" },
];

export const APPLICATION_STATUS: Record<IdentityApplicationStatus, { label: string; tone: BadgeTone }> = {
  submitted: { label: "To review", tone: "warning" },
  needs_info: { label: "Asked for more", tone: "info" },
  approved: { label: "Approved", tone: "success" },
  rejected: { label: "Rejected", tone: "danger" },
  withdrawn: { label: "Withdrawn", tone: "neutral" },
};

/** Admin → Verification → Badge applications: the review queue (oldest first) and decided ones. */
export function AdminBadgeApplicationsScreen() {
  const router = useRouter();
  const [status, setStatus] = useUrlState<IdentityApplicationStatus>("status", "submitted");
  const list = useIdentityAdminBadgeApplications(status === "submitted" ? { limit: 50 } : { status, limit: 50 });
  const rows = list.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Verification" info="Applications for organization and notable badges. You can't review your own." />
      <div className="flex flex-col gap-4">
        <IdentityAdminTabs />
        <Select aria-label="Status" options={FILTERS} value={status} onValueChange={(v) => setStatus(v as IdentityApplicationStatus)} className="sm:w-64" />
        <Card padding="none">
          {list.isPending ? (
            <TableSkeleton rows={5} cols={4} />
          ) : list.isError ? (
            <ErrorState compact title="Couldn't load applications" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<Inbox />} title={status === "submitted" ? "Nothing to review" : "None"} />
          ) : (
            <>
              <Table label="Badge applications">
                <THead>
                  <TH>Applicant</TH>
                  <TH className="hidden sm:table-cell">Category</TH>
                  <TH>Status</TH>
                  <TH align="right">Sent</TH>
                </THead>
                <tbody>
                  {rows.map((a) => (
                    <TR key={a.id} onActivate={() => router.push(`/admin/identity/applications/${a.id}`)}>
                      <TD>
                        <Link href={`/admin/identity/applications/${a.id}`} className="font-semibold text-fg underline-offset-2 hover:underline">
                          {a.display_label}
                        </Link>
                        <span className="block truncate type-caption text-fg-muted">{a.user.display_name || a.user.email}</span>
                      </TD>
                      <TD className="hidden text-fg-muted sm:table-cell">{a.category}</TD>
                      <TD>
                        <Badge tone={APPLICATION_STATUS[a.status].tone}>{APPLICATION_STATUS[a.status].label}</Badge>
                      </TD>
                      <TD align="right" className="whitespace-nowrap text-fg-muted">
                        {formatDate(a.submitted_at)}
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
