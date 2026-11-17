"use client";

import { BASKETS_STATUS_LABEL, compactExchangeNumber, formatBasketsWeight, type BasketsLine, type BasketsPriceStatus, type BasketsSummary } from "@scaffoldapp/shared";
import { Badge, cn } from "@scaffoldapp/ui-web";
import Link from "next/link";
import { basketsPaths } from "./registry";

/** A basket's status when it isn't simply trading; hidden and flagged for staff views. */
export function BasketStatusBadge({ basket, staff }: { basket: Pick<BasketsSummary, "status" | "hidden" | "flagged">; staff?: boolean }) {
  return (
    <>
      {basket.status !== "active" && (
        <Badge tone={basket.status === "archived" ? "neutral" : "warning"} size="sm">
          {BASKETS_STATUS_LABEL[basket.status]}
        </Badge>
      )}
      {staff && basket.hidden && (
        <Badge tone="danger" size="sm">
          Hidden
        </Badge>
      )}
      {staff && !basket.hidden && basket.flagged && (
        <Badge tone="warning" size="sm">
          Flagged
        </Badge>
      )}
    </>
  );
}

const priceLabels: Partial<Record<BasketsPriceStatus, string>> = { stale: "Stale", seed: "Not live", off: "Off" };

/** The oracle's status of a price when it isn't live. */
export function PriceStatusBadge({ status }: { status: BasketsPriceStatus | undefined }) {
  const label = status ? priceLabels[status] : undefined;
  if (!label) return null;
  return (
    <Badge tone={status === "stale" || status === "off" ? "warning" : "neutral"} size="sm">
      {label}
    </Badge>
  );
}

/** "Gold · Silver · Oil +2" from a summary's top lines. */
export function topLinesText(b: Pick<BasketsSummary, "top_lines" | "line_count">): string {
  const more = b.line_count - b.top_lines.length;
  return b.top_lines.join(" · ") + (more > 0 ? ` +${more}` : "");
}

/** One basket in a list: name, symbol and its largest lines; entries and recent volume. */
export function BasketRow({ b }: { b: BasketsSummary }) {
  return (
    <li>
      <Link
        href={basketsPaths.basket(b.symbol)}
        className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 border-b border-border py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring sm:grid-cols-[minmax(0,1fr)_5rem_6rem]"
      >
        <span className="flex min-w-0 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate type-label font-semibold text-fg">{b.name}</span>
            <BasketStatusBadge basket={b} />
          </span>
          <span className="truncate type-caption text-fg-muted">
            {b.symbol} · {topLinesText(b)}
          </span>
        </span>
        <span className="flex flex-col items-end">
          <span className="type-caption text-fg-muted">Entries</span>
          <span className="type-label tabular-nums text-fg">{b.entries}</span>
        </span>
        <span className="hidden flex-col items-end sm:flex">
          <span className="type-caption text-fg-muted">Volume</span>
          <span className="type-label tabular-nums text-fg">{compactExchangeNumber(b.volume_recent)}</span>
        </span>
      </Link>
    </li>
  );
}

const day = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeZone: "UTC" });

/** A calendar date ("2026-12-01" → "Dec 1, 2026"), without shifting it to the viewer's zone. */
export function formatBasketsDay(date: string): string {
  const d = new Date(`${date}T00:00:00Z`);
  return Number.isNaN(d.getTime()) ? date : day.format(d);
}

/** A thin bar for a line's share of the basket. */
export function WeightBar({ weight, className }: { weight: string | null; className?: string }) {
  const pct = weight ? Math.min(100, Math.max(0, Number(weight) * 100)) : 0;
  return (
    <span className={cn("relative block h-1.5 w-16 shrink-0 overflow-hidden rounded-pill bg-sunken", className)} aria-hidden>
      <span className="absolute inset-y-0 left-0 rounded-pill bg-primary" style={{ width: `${pct}%` }} />
    </span>
  );
}

/** A basket's lines: quantity × market, price, value and weight; markets link to their trade screens. */
export function BasketLines({ lines, money, label }: { lines: BasketsLine[]; money: (v: string | null) => string; label: string }) {
  return (
    <ul aria-label={label} className="flex flex-col">
      {lines.map((l) => (
        <li key={l.symbol} className="flex min-h-row items-center gap-3 border-b border-border py-2 last:border-b-0">
          <span className="w-12 shrink-0 text-right type-label tabular-nums text-fg-muted">{l.quantity}×</span>
          <span className="flex min-w-0 flex-1 flex-col">
            <Link href={basketsPaths.market(l.symbol)} className="truncate type-label text-fg underline-offset-2 hover:underline">
              {l.name}
            </Link>
            <span className="flex items-center gap-2 type-caption text-fg-muted">
              <span className="tabular-nums">
                {l.symbol} · {money(l.price)}
              </span>
              <PriceStatusBadge status={l.price_status} />
            </span>
          </span>
          <span className="hidden type-label tabular-nums text-fg sm:block">{money(l.value)}</span>
          <WeightBar weight={l.weight} className="hidden sm:block" />
          <span className="w-16 shrink-0 text-right type-label tabular-nums text-fg">{formatBasketsWeight(l.weight)}</span>
        </li>
      ))}
    </ul>
  );
}
