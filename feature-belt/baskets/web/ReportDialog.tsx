"use client";

import { errorMessage, useReportBasket, type BasketsReportReason } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { useState } from "react";

export const BASKETS_REPORT_REASONS: { value: BasketsReportReason; label: string }[] = [
  { value: "name", label: "The name is offensive or misleading" },
  { value: "description", label: "The description is offensive or misleading" },
  { value: "spam", label: "Spam" },
  { value: "other", label: "Something else" },
];

/** Report a basket's name or description to staff. */
export function BasketReportDialog({ symbol, name, open, onOpenChange }: { symbol: string; name: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const report = useReportBasket(symbol);
  const [reason, setReason] = useState<BasketsReportReason>("name");
  const [note, setNote] = useState("");
  const submit = () =>
    report.mutate(
      { reason, note: note.trim() || undefined },
      {
        onSuccess: () => {
          toast.success("Reported. Staff will review it.");
          onOpenChange(false);
          setNote("");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't send the report.")),
      },
    );
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Report “${name}”`}
      info="Staff review reports and can rename or remove a basket. People holding positions can still close them."
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} loading={report.isPending}>
            Report
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field label="What's wrong">
          <Select options={BASKETS_REPORT_REASONS} value={reason} onValueChange={(v) => setReason(v as BasketsReportReason)} />
        </Field>
        <Field label="Note">
          <TextArea value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} minRows={2} maxRows={6} />
        </Field>
      </form>
    </Dialog>
  );
}
