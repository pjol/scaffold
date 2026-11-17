"use client";

import { ORACLE_KIND_LABEL, errorMessage, usePostOraclePrint, type OraclePrintKind } from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, Select, toast } from "@scaffoldapp/ui-web";
import { useState } from "react";

const KINDS: OraclePrintKind[] = ["sale", "market", "listing"];

/**
 * Admin: type a price in by hand for a feed with a manual source. The print is
 * queued and screened like any other, so an outlier is still rejected.
 */
export function OraclePostPriceDialog({
  feedKey,
  quote,
  venues,
  open,
  onOpenChange,
}: {
  feedKey: string;
  quote: string;
  venues: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const post = usePostOraclePrint(feedKey);
  const [price, setPrice] = useState("");
  const [kind, setKind] = useState<OraclePrintKind>("sale");
  const [venue, setVenue] = useState(venues[0] ?? "");
  const [note, setNote] = useState("");
  const valid = /^\d+(\.\d+)?$/.test(price.trim()) && Number(price) > 0;

  function submit() {
    if (!valid) return;
    post.mutate(
      { price: price.trim(), kind, venue: venue || undefined, note: note.trim() || undefined },
      {
        onSuccess: () => {
          toast.success("Price sent for screening");
          setPrice("");
          setNote("");
          onOpenChange(false);
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't post the price.")),
      },
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Post a price"
      info="Screened like every other print: one far from the others is downweighted or rejected, and the published price still moves at its capped rate."
      footer={
        <>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={submit} loading={post.isPending} disabled={!valid}>
            Post price
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
        <Field label={`Price (${quote})`} required>
          <Input inputMode="decimal" autoComplete="off" value={price} onChange={(e) => setPrice(e.target.value)} placeholder="0.00" />
        </Field>
        <Field label="Kind" info="A sale is one trade; a market price summarises many; a listing is only an ask and counts least.">
          <Select options={KINDS.map((k) => ({ value: k, label: ORACLE_KIND_LABEL[k] }))} value={kind} onValueChange={(v) => setKind(v as OraclePrintKind)} />
        </Field>
        {venues.length > 1 && (
          <Field label="Venue">
            <Select options={venues.map((v) => ({ value: v, label: v }))} value={venue} onValueChange={setVenue} />
          </Field>
        )}
        <Field label="Note" info="Shown as the first reason on the observation.">
          <Input value={note} maxLength={120} onChange={(e) => setNote(e.target.value)} placeholder="Desk quote" />
        </Field>
      </form>
    </Dialog>
  );
}
