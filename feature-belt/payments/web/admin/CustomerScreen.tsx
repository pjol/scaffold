"use client";

import { formatMinor, useAdminLedger, useAdminPaymentCustomer, type PaymentOrder } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, InfoTip, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { ChevronLeft, ReceiptText } from "lucide-react";
import Link from "next/link";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate, formatDateTime } from "@/lib/format";
import { ORDER_STATUS, REFUND_STATUS, SUBSCRIPTION_STATUS, subscriptionDateLine } from "../labels";
import { AdminError } from "./AdminUnavailable";
import { LedgerTable } from "./LedgerTable";

function shippingText(o: PaymentOrder): string | null {
  const a = o.shipping;
  if (!a) return null;
  return [a.name, a.line1, a.line2, [a.postal_code, a.city].filter(Boolean).join(" "), a.state, a.country].filter(Boolean).join(", ");
}

function CustomerLedger({ userId }: { userId: string }) {
  const ledger = useAdminLedger({ user_id: userId, limit: 50 });
  const rows = ledger.data?.items ?? [];
  return (
    <Section title="Ledger">
      <Card padding="none">
        {ledger.isPending ? (
          <TableSkeleton rows={4} cols={4} />
        ) : ledger.isError ? (
          <AdminError error={ledger.error} title="Couldn't load the ledger" onRetry={() => void ledger.refetch()} retrying={ledger.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={<ReceiptText />} title="No money movements yet" />
        ) : (
          <>
            <LedgerTable rows={rows} />
            <LoadMore hasNextPage={ledger.hasNextPage} isFetchingNextPage={ledger.isFetchingNextPage} fetchNextPage={ledger.fetchNextPage} />
          </>
        )}
      </Card>
    </Section>
  );
}

/** Admin → Payments → a customer: plan, access, orders, ledger (with refunds) and refunds. */
export function CustomerScreen({ userId }: { userId: string }) {
  const detail = useAdminPaymentCustomer(userId);
  const back = (
    <Link href="/admin/payments" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
      <ChevronLeft className="size-4" aria-hidden />
      Customers
    </Link>
  );
  if (detail.isPending) {
    return (
      <div className="flex flex-col gap-section">
        {back}
        <SkeletonGroup className="flex flex-col gap-4">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-32" />
          <Skeleton className="h-48" />
        </SkeletonGroup>
      </div>
    );
  }
  if (detail.isError) {
    return (
      <div className="flex flex-col gap-section">
        {back}
        <AdminError error={detail.error} title="Couldn't load this customer" onRetry={() => void detail.refetch()} retrying={detail.isFetching} />
      </div>
    );
  }
  const d = detail.data;
  const c = d.customer;
  const deleted = c?.user_status === "deleted";
  const title = deleted ? "Deleted user" : c?.display_name || c?.email || "Customer";
  const net = Object.entries(d.net_paid);
  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-2">
        {back}
        <PageHeader
          title={title}
          info={c ? `Provider customer ${c.provider_customer_id}${c.delete_after ? `; kept pseudonymised until ${formatDate(c.delete_after)}` : ""}.` : "No provider customer yet."}
          action={
            <Button asChild variant="secondary" size="sm">
              <Link href={`/admin/users/${d.user_id}`}>Open user</Link>
            </Button>
          }
        />
      </div>

      <Section title="Summary">
        <Card className="grid gap-4 sm:grid-cols-3">
          <div className="flex flex-col gap-1">
            <span className="type-caption text-fg-muted">Email</span>
            <span className="truncate type-body text-fg">{deleted ? "Deleted" : (c?.email ?? "—")}</span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="flex items-center gap-1 type-caption text-fg-muted">
              Net paid
              <InfoTip content="Payments minus refunds, per currency." />
            </span>
            <span className="type-body font-semibold text-fg tabular-nums">{net.length ? net.map(([cur, v]) => formatMinor(v, cur)).join(" · ") : "—"}</span>
          </div>
          <div className="flex flex-col gap-1">
            <span className="type-caption text-fg-muted">Access</span>
            <span className="flex flex-wrap gap-1">
              {d.entitlements.length ? (
                d.entitlements.map((e) => (
                  <Badge key={e.key} tone="primary">
                    {e.active_until ? `${e.key} until ${formatDate(e.active_until)}` : e.key}
                  </Badge>
                ))
              ) : (
                <span className="type-body text-fg-muted">None</span>
              )}
            </span>
          </div>
        </Card>
      </Section>

      <Section title="Subscriptions">
        <Card padding="none">
          {d.subscriptions.length === 0 ? (
            <EmptyState compact icon={<ReceiptText />} title="No subscriptions" />
          ) : (
            <Table label="Subscriptions">
              <THead>
                <TH>Plan</TH>
                <TH>Status</TH>
                <TH className="hidden sm:table-cell" align="right">
                  Period
                </TH>
              </THead>
              <tbody>
                {d.subscriptions.map((s) => (
                  <TR key={s.id}>
                    <TD className="max-w-48 truncate font-semibold">{s.label}</TD>
                    <TD>
                      <Badge tone={SUBSCRIPTION_STATUS[s.status].tone}>{SUBSCRIPTION_STATUS[s.status].label}</Badge>
                    </TD>
                    <TD align="right" className="hidden whitespace-nowrap text-fg-muted sm:table-cell">
                      {subscriptionDateLine(s) ?? "—"}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </Section>

      <Section title="Orders">
        <Card padding="none">
          {d.orders.length === 0 ? (
            <EmptyState compact icon={<ReceiptText />} title="No orders" />
          ) : (
            <Table label="Orders" className="min-w-160">
              <THead>
                <TH>Price</TH>
                <TH>Status</TH>
                <TH align="right">Amount</TH>
                <TH className="hidden sm:table-cell" align="right">
                  When
                </TH>
              </THead>
              <tbody>
                {d.orders.map((o) => {
                  const ship = shippingText(o);
                  return (
                    <TR key={o.id}>
                      <TD className="max-w-64">
                        <span className="flex items-center gap-1 truncate">
                          {o.quantity > 1 ? `${o.price_key} × ${o.quantity}` : o.price_key}
                          {ship && <InfoTip content={`Ship to ${ship}`} />}
                          {o.ref && <InfoTip content={`Reference ${o.ref}`} />}
                        </span>
                      </TD>
                      <TD>
                        <Badge tone={ORDER_STATUS[o.status].tone}>{ORDER_STATUS[o.status].label}</Badge>
                      </TD>
                      <TD align="right" className="font-semibold">
                        {formatMinor(o.amount_minor, o.currency)}
                      </TD>
                      <TD align="right" className="hidden whitespace-nowrap text-fg-muted sm:table-cell">
                        {formatDateTime(o.created_at)}
                      </TD>
                    </TR>
                  );
                })}
              </tbody>
            </Table>
          )}
        </Card>
      </Section>

      <CustomerLedger userId={d.user_id} />

      {d.refunds.length > 0 && (
        <Section title="Refunds">
          <Card padding="none">
            <Table label="Refunds">
              <THead>
                <TH>Amount</TH>
                <TH>Status</TH>
                <TH className="hidden sm:table-cell">By</TH>
                <TH align="right">When</TH>
              </THead>
              <tbody>
                {d.refunds.map((r) => (
                  <TR key={r.id}>
                    <TD className="font-semibold tabular-nums">
                      <span className="flex items-center gap-1">
                        {formatMinor(r.amount_minor, r.currency)}
                        {r.note && <InfoTip content={r.note} />}
                      </span>
                    </TD>
                    <TD>
                      <Badge tone={REFUND_STATUS[r.status].tone}>{REFUND_STATUS[r.status].label}</Badge>
                    </TD>
                    <TD className="hidden max-w-48 truncate text-fg-muted sm:table-cell">{r.requested_by_email ?? "Provider dashboard"}</TD>
                    <TD align="right" className="whitespace-nowrap text-fg-muted">
                      {formatDateTime(r.created_at)}
                    </TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          </Card>
        </Section>
      )}
    </div>
  );
}
