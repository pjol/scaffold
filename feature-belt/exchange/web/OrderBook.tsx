"use client";

import { exchangeDecimals, formatExchangePrice, formatExchangeSize, useExchangeBook, useExchangeTrades, type ExchangeBookLevel, type ExchangeMarket } from "@scaffoldapp/shared";
import { ErrorState, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import { useMemo } from "react";

const DEPTH = 10;

function Level({
  level,
  side,
  max,
  market,
  onPick,
}: {
  level: ExchangeBookLevel;
  side: "bid" | "ask";
  max: number;
  market: ExchangeMarket;
  onPick?: (px: string) => void;
}) {
  const width = max > 0 ? Math.min(100, (Number(level.sz) / max) * 100) : 0;
  return (
    <button
      type="button"
      onClick={() => onPick?.(level.px)}
      className="relative grid h-7 w-full grid-cols-2 items-center px-2 text-left type-caption tabular-nums hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
      aria-label={`${side === "bid" ? "Bid" : "Ask"} ${level.px}, size ${level.sz}`}
    >
      <span aria-hidden className={cn("absolute inset-y-0.5 right-0 rounded-xs", side === "bid" ? "bg-success-soft" : "bg-danger-soft")} style={{ width: `${width}%` }} />
      <span className={cn("relative", side === "bid" ? "text-success-strong" : "text-danger-strong")}>{formatExchangePrice(level.px, market.tick_size)}</span>
      <span className="relative text-right text-fg">{formatExchangeSize(level.sz, market.sz_decimals)}</span>
    </button>
  );
}

function BookSkeleton() {
  return (
    <SkeletonGroup label="Loading the order book" className="flex flex-col gap-1">
      {Array.from({ length: 12 }, (_, i) => (
        <Skeleton key={i} shape="text" className="h-5 w-full" />
      ))}
    </SkeletonGroup>
  );
}

/** Asks above, bids below, the spread and mark between; a price click hands it to the ticket. */
export function OrderBook({ market, onPick }: { market: ExchangeMarket; onPick?: (px: string) => void }) {
  const book = useExchangeBook(market.symbol);
  const { asks, bids, max } = useMemo(() => {
    const a = (book.data?.asks ?? []).slice(0, DEPTH);
    const b = (book.data?.bids ?? []).slice(0, DEPTH);
    const m = Math.max(0, ...a.map((l) => Number(l.sz)), ...b.map((l) => Number(l.sz)));
    return { asks: a.slice().reverse(), bids: b, max: m };
  }, [book.data]);
  const spread = asks.length && bids.length ? Number(asks[asks.length - 1]!.px) - Number(bids[0]!.px) : null;

  if (book.isPending) return <BookSkeleton />;
  if (book.isError) return <ErrorState compact title="Couldn't load the order book" info={book.error.message} onRetry={() => void book.refetch()} />;
  return (
    <div className="flex flex-col" aria-label="Order book">
      <div className="grid grid-cols-2 px-2 pb-1 type-caption text-fg-muted">
        <span>Price</span>
        <span className="text-right">Size</span>
      </div>
      {asks.length === 0 && <p className="px-2 py-3 type-caption text-fg-muted">No asks</p>}
      {asks.map((l) => (
        <Level key={`a${l.px}`} level={l} side="ask" max={max} market={market} onPick={onPick} />
      ))}
      <div className="flex items-center justify-between border-y border-border px-2 py-1.5 type-label tabular-nums">
        <span className="text-fg">{formatExchangePrice(market.stats.mark_px, market.tick_size)}</span>
        <span className="type-caption text-fg-muted">{spread === null ? "No spread" : `Spread ${spread.toFixed(exchangeDecimals(market.tick_size))}`}</span>
      </div>
      {bids.map((l) => (
        <Level key={`b${l.px}`} level={l} side="bid" max={max} market={market} onPick={onPick} />
      ))}
      {bids.length === 0 && <p className="px-2 py-3 type-caption text-fg-muted">No bids</p>}
    </div>
  );
}

/** The latest trades, newest first. Simulated trades carry a dot. */
export function TradesTape({ market }: { market: ExchangeMarket }) {
  const trades = useExchangeTrades(market.symbol);
  if (trades.isPending) return <BookSkeleton />;
  if (trades.isError) return <ErrorState compact title="Couldn't load trades" info={trades.error.message} onRetry={() => void trades.refetch()} />;
  const items = trades.data.items.slice(0, 30);
  if (items.length === 0) return <p className="px-2 py-3 type-label text-fg-muted">No trades yet</p>;
  return (
    <div className="flex flex-col" aria-label="Recent trades">
      <div className="grid grid-cols-3 px-2 pb-1 type-caption text-fg-muted">
        <span>Price</span>
        <span className="text-right">Size</span>
        <span className="text-right">Time</span>
      </div>
      {items.map((t) => (
        <div key={t.id} className="grid h-7 grid-cols-3 items-center px-2 type-caption tabular-nums">
          <span className={cn("flex items-center gap-1", t.side === "buy" ? "text-success-strong" : "text-danger-strong")}>
            {formatExchangePrice(t.px, market.tick_size)}
            {t.liquidation && <span className="text-warning-strong">L</span>}
          </span>
          <span className="text-right text-fg">{formatExchangeSize(t.sz, market.sz_decimals)}</span>
          <span className="flex items-center justify-end gap-1 text-fg-muted">
            {t.sim && <span title="Simulated trader" className="size-1.5 rounded-pill bg-fg-subtle" />}
            {new Date(t.at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" })}
          </span>
        </div>
      ))}
    </div>
  );
}
