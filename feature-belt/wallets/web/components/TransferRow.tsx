"use client";

import { formatUnits, shortAddress, type WalletTransfer } from "@scaffoldapp/shared";
import { ListRow, cn } from "@scaffoldapp/ui-web";
import { ArrowDownLeft, ArrowLeftRight, ArrowUpRight } from "lucide-react";

const timeFormat = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" });

/** "4:05 PM" (rows sit under a day heading). */
function transferTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : timeFormat.format(d);
}

const KIND = {
  in: { icon: ArrowDownLeft, tile: "bg-success-soft text-success-strong", sign: "+", amount: "text-success-strong" },
  out: { icon: ArrowUpRight, tile: "bg-sunken text-fg", sign: "−", amount: "text-fg" },
  self: { icon: ArrowLeftRight, tile: "bg-sunken text-fg-muted", sign: "", amount: "text-fg-muted" },
} as const;

/** One transfer: direction, counterparty, time (≥ 640 px) and signed amount; opens the explorer when there is one. */
export function TransferRow({ transfer, chainName }: { transfer: WalletTransfer; chainName?: string | undefined }) {
  const k = KIND[transfer.direction];
  const Icon = k.icon;
  const counterparty = transfer.direction === "in" ? transfer.from : transfer.to;
  const lead = transfer.direction === "in" ? "From" : "To";
  // Below 640 px the arrow says the direction, so the title is the address alone.
  const title =
    transfer.direction === "self" ? (
      "Between your wallets"
    ) : (
      <>
        <span className="hidden sm:inline">{lead} </span>
        <span className="tabular-nums">{shortAddress(counterparty)}</span>
      </>
    );
  const spoken = transfer.direction === "self" ? "Between your wallets" : `${lead} ${shortAddress(counterparty)}`;
  const amount = `${k.sign}${formatUnits(transfer.amount, transfer.decimals)} ${transfer.symbol}`;
  const label = `${spoken}, ${amount}${chainName ? ` on ${chainName}` : ""}, at ${transferTime(transfer.occurred_at)}`;
  const row = {
    title,
    chevron: false,
    leading: (
      <span className={cn("flex size-10 items-center justify-center rounded-md", k.tile)}>
        <Icon className="size-5" aria-hidden />
      </span>
    ),
    trailing: (
      <>
        <span className="hidden type-caption whitespace-nowrap text-fg-muted sm:inline">{transferTime(transfer.occurred_at)}</span>
        <span className={cn("type-body font-semibold whitespace-nowrap tabular-nums", k.amount)}>{amount}</span>
      </>
    ),
  };
  if (!transfer.explorer_url) return <ListRow {...row} aria-label={label} />;
  return (
    <ListRow {...row} asChild>
      <a href={transfer.explorer_url} target="_blank" rel="noopener noreferrer" aria-label={`${label}. Opens the block explorer.`} />
    </ListRow>
  );
}
