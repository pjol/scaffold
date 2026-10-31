"use client";

import { formatMinor, useMe, type AdminLedgerEntry } from "@scaffoldapp/shared";
import { Badge, Button } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { useState } from "react";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { formatDateTime } from "@/lib/format";
import { RefundDialog } from "./RefundDialog";

const KIND: Record<AdminLedgerEntry["kind"], string> = { payment: "Payment", invoice: "Invoice", refund: "Refund" };

/** Ledger rows with a Refund action on payments and invoices (admins only; support sees it disabled). */
export function LedgerTable({ rows, showUser }: { rows: AdminLedgerEntry[]; showUser?: boolean }) {
  const me = useMe();
  const isAdmin = me.data?.admin_role === "admin";
  const [target, setTarget] = useState<AdminLedgerEntry | null>(null);
  return (
    <>
      <Table label="Ledger" className="min-w-160">
        <THead>
          <TH>Description</TH>
          <TH>Kind</TH>
          {showUser && <TH className="hidden md:table-cell">Customer</TH>}
          <TH className="hidden lg:table-cell" info="Payment, invoice or refund id at the provider.">
            Reference
          </TH>
          <TH align="right">Amount</TH>
          <TH className="hidden sm:table-cell" align="right">
            When
          </TH>
          <TH align="right" className="relative">
            <span className="sr-only">Actions</span>
          </TH>
        </THead>
        <tbody>
          {rows.map((e) => {
            const refundable = e.kind !== "refund" && e.provider_payment_id !== null && e.refunded_minor < e.amount_minor;
            return (
              <TR key={e.id}>
                <TD className="max-w-64 truncate">{e.description || KIND[e.kind]}</TD>
                <TD>
                  <span className="flex items-center gap-2">
                    <Badge tone={e.kind === "refund" ? "success" : "neutral"}>{KIND[e.kind]}</Badge>
                    {e.kind !== "refund" && e.refunded_minor > 0 && <Badge tone="warning">{formatMinor(e.refunded_minor, e.currency)} refunded</Badge>}
                  </span>
                </TD>
                {showUser && (
                  <TD className="hidden md:table-cell">
                    <Link href={`/admin/payments/customers/${e.user_id}`} className="font-mono text-fg-muted underline-offset-2 hover:underline">
                      {e.user_id.slice(0, 8)}
                    </Link>
                  </TD>
                )}
                <TD className="hidden max-w-48 truncate font-mono text-fg-muted lg:table-cell">{e.provider_ref}</TD>
                <TD align="right" className="font-semibold">
                  {formatMinor(e.amount_minor, e.currency, { signed: e.kind === "refund" })}
                </TD>
                <TD align="right" className="hidden whitespace-nowrap text-fg-muted sm:table-cell">
                  {formatDateTime(e.created_at)}
                </TD>
                <TD align="right">
                  {refundable && (
                    <Button size="sm" variant="ghost" disabled={!isAdmin} title={isAdmin ? undefined : "Admins only"} onClick={() => setTarget(e)}>
                      Refund
                    </Button>
                  )}
                </TD>
              </TR>
            );
          })}
        </tbody>
      </Table>
      <RefundDialog entry={target} open={target !== null} onOpenChange={(o) => !o && setTarget(null)} />
    </>
  );
}
