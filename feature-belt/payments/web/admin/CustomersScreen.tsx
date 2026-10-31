"use client";

import { useAdminPaymentCustomers } from "@scaffoldapp/shared";
import { Badge, Card, EmptyState } from "@scaffoldapp/ui-web";
import { SearchX, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { AdminError } from "./AdminUnavailable";
import { PaymentsAdminTabs } from "./PaymentsAdminTabs";

/** Admin → Payments → Customers: users with a provider customer; search by email, name, user id or provider id. */
export function CustomersScreen() {
  const router = useRouter();
  const [q, setQ] = useUrlState("q", "");
  const customers = useAdminPaymentCustomers({ q: q || undefined, limit: 50 });
  const rows = customers.data?.items ?? [];
  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Payments" />
      <div className="flex flex-col gap-4">
        <PaymentsAdminTabs />
        <SearchInput value={q} onSearch={setQ} placeholder="Email, name, user ID or customer ID" label="Search customers" className="sm:max-w-sm" />
        <Card padding="none">
          {customers.isPending ? (
            <TableSkeleton rows={6} cols={3} />
          ) : customers.isError ? (
            <AdminError error={customers.error} title="Couldn't load customers" onRetry={() => void customers.refetch()} retrying={customers.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={q ? <SearchX /> : <Users />} title={q ? "No matching customers" : "No customers yet"} />
          ) : (
            <>
              <Table label="Customers">
                <THead>
                  <TH>Customer</TH>
                  <TH className="hidden sm:table-cell">Email</TH>
                  <TH className="hidden md:table-cell" info="The provider's customer id (Stripe cus_…).">
                    Provider ID
                  </TH>
                  <TH align="right">Since</TH>
                </THead>
                <tbody>
                  {rows.map((c) => {
                    const deleted = c.user_status === "deleted";
                    const name = deleted ? "Deleted user" : c.display_name || c.email;
                    return (
                      <TR key={c.id} onActivate={() => router.push(`/admin/payments/customers/${c.user_id}`)}>
                        <TD className="max-w-48 truncate">
                          <Link href={`/admin/payments/customers/${c.user_id}`} className="font-semibold text-fg underline-offset-2 hover:underline">
                            {name}
                          </Link>
                        </TD>
                        <TD className="hidden max-w-64 truncate text-fg-muted sm:table-cell">{deleted ? "" : c.email}</TD>
                        <TD className="hidden font-mono text-fg-muted md:table-cell">
                          <span className="flex items-center gap-2">
                            {c.provider_customer_id}
                            {c.delete_after && <Badge tone="neutral">Retained until {formatDate(c.delete_after)}</Badge>}
                          </span>
                        </TD>
                        <TD align="right" className="whitespace-nowrap text-fg-muted">
                          {formatDate(c.created_at)}
                        </TD>
                      </TR>
                    );
                  })}
                </tbody>
              </Table>
              <LoadMore hasNextPage={customers.hasNextPage} isFetchingNextPage={customers.isFetchingNextPage} fetchNextPage={customers.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
    </div>
  );
}
