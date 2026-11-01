"use client";

import { currencyDigits, errorMessage, formatMinor, parseMinor, useCreateRefund, type AdminLedgerEntry, type RefundReason } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { useEffect, useState } from "react";
import { REFUND_REASONS } from "../labels";

/**
 * Refund a payment or invoice (admin role). Defaults to everything not yet refunded.
 * The request carries an Idempotency-Key, so a retry after a dropped connection
 * returns the first result instead of refunding twice. Audited.
 */
export function RefundDialog({ entry, open, onOpenChange }: { entry: AdminLedgerEntry | null; open: boolean; onOpenChange: (open: boolean) => void }) {
  const refund = useCreateRefund();
  const remaining = entry ? entry.amount_minor - entry.refunded_minor : 0;
  const currency = entry?.currency ?? "usd";
  const [amount, setAmount] = useState("");
  const [reason, setReason] = useState<string>("requested_by_customer");
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !entry) return;
    const digits = currencyDigits(currency);
    setAmount((remaining / 10 ** digits).toFixed(digits));
    setReason("requested_by_customer");
    setNote("");
    setError(null);
  }, [open, entry, remaining, currency]);

  const minor = parseMinor(amount, currency);
  const invalid = minor === null || minor > remaining;

  const submit = () => {
    if (!entry || invalid || minor === null) return;
    setError(null);
    refund.mutate(
      { ledger_id: entry.id, amount_minor: minor, reason: (reason === "none" ? "" : reason) as RefundReason, ...(note.trim() ? { note: note.trim() } : {}) },
      {
        onSuccess: (r) => {
          toast.success(r.status === "succeeded" ? `Refunded ${formatMinor(r.amount_minor, r.currency)}` : `Refund of ${formatMinor(r.amount_minor, r.currency)} is ${r.status}`);
          onOpenChange(false);
        },
        onError: (e) => setError(errorMessage(e, "Couldn't refund. Try again.")),
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Refund payment"
      info="The money goes back to the original payment method. On a seller's sale, the seller's share and the platform fee are pulled back too."
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button variant="danger" loading={refund.isPending} disabled={invalid} onClick={submit}>
            {minor !== null && !invalid ? `Refund ${formatMinor(minor, currency)}` : "Refund"}
          </Button>
        </>
      }
    >
      {entry && (
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Field label="Amount" info={`Up to ${formatMinor(remaining, currency)} is left to refund on "${entry.description}".`} error={amount && invalid ? `Enter an amount up to ${formatMinor(remaining, currency)}.` : undefined}>
            <Input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} trailing={<span className="type-label text-fg-muted">{currency.toUpperCase()}</span>} />
          </Field>
          <Field label="Reason">
            <Select options={[...REFUND_REASONS]} value={reason} onValueChange={setReason} />
          </Field>
          <Field label="Note" info="Kept with the refund for staff. The customer doesn't see it.">
            <TextArea value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} rows={3} />
          </Field>
          {error && (
            <p role="alert" className="type-label text-danger-strong">
              {error}
            </p>
          )}
        </form>
      )}
    </Dialog>
  );
}
