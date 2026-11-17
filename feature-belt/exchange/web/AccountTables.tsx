"use client";

import {
  errorMessage,
  exchangeSign,
  formatExchangeFunding,
  formatExchangePrice,
  formatExchangeSize,
  useCancelAllExchangeOrders,
  useCancelExchangeOrder,
  useCloseExchangePosition,
  useExchangeFills,
  useExchangeFunding,
  useExchangeLedger,
  type ExchangeLedgerKind,
  type ExchangeMarket,
  type ExchangeOrder,
  type ExchangePosition,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Skeleton, SkeletonGroup, cn, toast } from "@scaffoldapp/ui-web";
import { History, Layers, ListOrdered, X } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { signTone, useExchangeMoney } from "./parts";

type MarketLookup = (symbol: string) => ExchangeMarket | undefined;

function when(iso: string) {
  return new Date(iso).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

/** A data row: a title cell, then label/value cells that wrap on narrow screens. */
function Row({ title, cells, action }: { title: ReactNode; cells: { label: string; value: ReactNode; className?: string }[]; action?: ReactNode }) {
  return (
    <li className="flex flex-col gap-2 border-b border-border py-3 last:border-b-0 sm:flex-row sm:items-center sm:gap-4">
      <div className="flex min-w-0 flex-wrap items-center gap-2 sm:w-36 sm:shrink-0">{title}</div>
      <dl className="grid flex-1 grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-4">
        {cells.map((c) => (
          <div key={c.label} className="flex min-w-0 flex-col">
            <dt className="type-caption text-fg-muted">{c.label}</dt>
            <dd className={cn("type-label tabular-nums break-words text-fg", c.className)}>{c.value}</dd>
          </div>
        ))}
      </dl>
      {action && <div className="flex shrink-0 justify-end">{action}</div>}
    </li>
  );
}

function SideBadge({ side, sz }: { side: "buy" | "sell" | "long" | "short"; sz?: string }) {
  const long = side === "buy" || side === "long";
  return (
    <Badge tone={long ? "success" : "danger"} size="sm">
      {side === "buy" ? "Buy" : side === "sell" ? "Sell" : long ? "Long" : "Short"}
      {sz ? ` ${sz}` : ""}
    </Badge>
  );
}

function ListSkeleton({ label }: { label: string }) {
  return (
    <SkeletonGroup label={label} className="flex flex-col gap-3">
      {[0, 1, 2].map((i) => (
        <Skeleton key={i} className="h-12 w-full" />
      ))}
    </SkeletonGroup>
  );
}

function PositionRow({ p, market }: { p: ExchangePosition; market: ExchangeMarket | undefined }) {
  const { money } = useExchangeMoney();
  const close = useCloseExchangePosition();
  const long = exchangeSign(p.sz) > 0;
  const abs = p.sz.replace(/^-/, "");
  return (
    <Row
      title={
        <>
          <Link href={`/markets/${p.symbol}`} className="type-label font-semibold text-fg hover:underline">
            {p.symbol}
          </Link>
          <SideBadge side={long ? "long" : "short"} />
          <span className="type-caption text-fg-muted">{p.leverage}×</span>
        </>
      }
      cells={[
        { label: "Size", value: formatExchangeSize(abs, market?.sz_decimals) },
        { label: "Entry / mark", value: `${formatExchangePrice(p.entry_px, market?.tick_size)} / ${formatExchangePrice(p.mark_px, market?.tick_size)}` },
        { label: "PnL", value: `${money(p.unrealized_pnl, true)} (${p.roe_pct}%)`, className: signTone(p.unrealized_pnl) },
        { label: "Liq. price", value: p.liquidation_px ? formatExchangePrice(p.liquidation_px, market?.tick_size) : "–" },
      ]}
      action={
        <ConfirmDialog
          trigger={
            <Button variant="outline" size="sm" loading={close.isPending}>
              Close
            </Button>
          }
          title={`Close ${p.symbol} at market?`}
          confirmLabel="Close position"
          tone="danger"
          onConfirm={() =>
            close.mutateAsync({ symbol: p.symbol }).then(
              () => toast.success(`${p.symbol} position closed`),
              (e: unknown) => {
                toast.error(errorMessage(e, "Couldn't close the position. Try again."));
              },
            )
          }
        >
          <p className="type-body text-fg-muted">
            {long ? "Sells" : "Buys"} {formatExchangeSize(abs, market?.sz_decimals)} {p.symbol} against the book now.
          </p>
        </ConfirmDialog>
      }
    />
  );
}

/** Open positions with PnL, liquidation price and a market close. */
export function PositionsList({ positions, lookup }: { positions: ExchangePosition[]; lookup: MarketLookup }) {
  if (positions.length === 0) return <EmptyState compact icon={<Layers />} title="No open positions" />;
  return (
    <ul aria-label="Open positions">
      {positions.map((p) => (
        <PositionRow key={p.symbol} p={p} market={lookup(p.symbol)} />
      ))}
    </ul>
  );
}

/** Resting orders with cancel, and cancel-all. */
export function OpenOrdersList({ orders, lookup, symbol }: { orders: ExchangeOrder[]; lookup: MarketLookup; symbol?: string }) {
  const cancel = useCancelExchangeOrder();
  const cancelAll = useCancelAllExchangeOrders();
  if (orders.length === 0) return <EmptyState compact icon={<ListOrdered />} title="No open orders" />;
  return (
    <div className="flex flex-col gap-2">
      <ul aria-label="Open orders">
        {orders.map((o) => {
          const m = lookup(o.symbol);
          return (
            <Row
              key={o.id}
              title={
                <>
                  <span className="type-label font-semibold text-fg">{o.symbol}</span>
                  <SideBadge side={o.side} />
                  {o.reduce_only && <span className="type-caption text-fg-muted">Reduce only</span>}
                </>
              }
              cells={[
                { label: "Price", value: formatExchangePrice(o.px, m?.tick_size) },
                { label: "Filled", value: `${formatExchangeSize(o.filled_sz, m?.sz_decimals)} / ${formatExchangeSize(o.sz, m?.sz_decimals)}` },
                { label: "Leverage", value: `${o.leverage}×` },
                { label: "Placed", value: when(o.created_at) },
              ]}
              action={
                <IconButton
                  label={`Cancel ${o.side} ${o.symbol} order`}
                  icon={<X />}
                  variant="ghost"
                  size="sm"
                  onClick={() => cancel.mutate(o.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't cancel the order.")) })}
                />
              }
            />
          );
        })}
      </ul>
      {orders.length > 1 && (
        <Button
          variant="ghost"
          size="sm"
          className="self-end"
          loading={cancelAll.isPending}
          onClick={() =>
            cancelAll.mutate(symbol, {
              onSuccess: (r) => toast.success(`${r.cancelled} order${r.cancelled === 1 ? "" : "s"} cancelled`),
              onError: (e) => toast.error(errorMessage(e, "Couldn't cancel the orders.")),
            })
          }
        >
          Cancel all
        </Button>
      )}
    </div>
  );
}

/** Executed trades, newest first. */
export function FillsList({ lookup, symbol }: { lookup: MarketLookup; symbol?: string }) {
  const { money } = useExchangeMoney();
  const fills = useExchangeFills();
  if (fills.isPending) return <ListSkeleton label="Loading fills" />;
  if (fills.isError) return <ErrorState compact title="Couldn't load fills" info={fills.error.message} onRetry={() => void fills.refetch()} />;
  const rows = fills.data.items.filter((f) => !symbol || f.symbol === symbol);
  if (rows.length === 0) return <EmptyState compact icon={<History />} title="No trades yet" />;
  return (
    <div className="flex flex-col">
      <ul aria-label="Fills">
        {rows.map((f) => {
          const m = lookup(f.symbol);
          return (
            <Row
              key={f.id}
              title={
                <>
                  <span className="type-label font-semibold text-fg">{f.symbol}</span>
                  <SideBadge side={f.side} />
                  {f.liquidation && <Badge tone="warning" size="sm">Liquidation</Badge>}
                </>
              }
              cells={[
                { label: "Price", value: formatExchangePrice(f.px, m?.tick_size) },
                { label: "Size", value: formatExchangeSize(f.sz, m?.sz_decimals) },
                { label: "Realized", value: money(f.realized_pnl, true), className: signTone(f.realized_pnl) },
                { label: "When", value: when(f.created_at) },
              ]}
            />
          );
        })}
      </ul>
      <LoadMore hasNextPage={fills.hasNextPage} isFetchingNextPage={fills.isFetchingNextPage} fetchNextPage={fills.fetchNextPage} />
    </div>
  );
}

/** Funding paid and received. */
export function FundingList() {
  const { money } = useExchangeMoney();
  const funding = useExchangeFunding();
  if (funding.isPending) return <ListSkeleton label="Loading funding" />;
  if (funding.isError) return <ErrorState compact title="Couldn't load funding" info={funding.error.message} onRetry={() => void funding.refetch()} />;
  if (funding.data.items.length === 0) return <EmptyState compact icon={<History />} title="No funding payments yet" />;
  return (
    <div className="flex flex-col">
      <ul aria-label="Funding payments">
        {funding.data.items.map((f) => (
          <Row
            key={f.id}
            title={<span className="type-label font-semibold text-fg">{f.symbol}</span>}
            cells={[
              { label: "Payment", value: money(f.payment, true), className: signTone(f.payment) },
              { label: "Rate / h", value: formatExchangeFunding(f.rate) },
              { label: "Position", value: f.sz },
              { label: "When", value: when(f.created_at) },
            ]}
          />
        ))}
      </ul>
      <LoadMore hasNextPage={funding.hasNextPage} isFetchingNextPage={funding.isFetchingNextPage} fetchNextPage={funding.fetchNextPage} />
    </div>
  );
}

const ledgerLabels: Record<ExchangeLedgerKind, string> = {
  deposit: "Deposit",
  withdrawal: "Withdrawal",
  faucet: "Test funds",
  fee: "Fee",
  realized_pnl: "Realized PnL",
  funding: "Funding",
  liquidation: "Liquidation",
  adjustment: "Adjustment",
  transfer: "Copy trading transfer",
  profit_share: "Profit share",
};

/** Every balance change. */
export function LedgerList() {
  const { money } = useExchangeMoney();
  const ledger = useExchangeLedger();
  if (ledger.isPending) return <ListSkeleton label="Loading the ledger" />;
  if (ledger.isError) return <ErrorState compact title="Couldn't load the ledger" info={ledger.error.message} onRetry={() => void ledger.refetch()} />;
  if (ledger.data.items.length === 0) return <EmptyState compact icon={<History />} title="No balance changes yet" />;
  return (
    <div className="flex flex-col">
      <ul aria-label="Ledger">
        {ledger.data.items.map((l) => (
          <Row
            key={l.id}
            title={<span className="type-label font-semibold text-fg">{ledgerLabels[l.kind]}</span>}
            cells={[
              { label: "Amount", value: money(l.amount, true), className: signTone(l.amount) },
              { label: "Market", value: l.symbol || "–" },
              { label: "Note", value: l.note || "–" },
              { label: "When", value: when(l.created_at) },
            ]}
          />
        ))}
      </ul>
      <LoadMore hasNextPage={ledger.hasNextPage} isFetchingNextPage={ledger.isFetchingNextPage} fetchNextPage={ledger.fetchNextPage} />
    </div>
  );
}
