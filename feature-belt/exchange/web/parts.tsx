"use client";

import {
  exchangeSign,
  formatExchangeMoney,
  formatExchangePct,
  useExchangeConfig,
  type ExchangeIndexStatus,
  type ExchangeMarket,
} from "@scaffoldapp/shared";
import { Badge, cn } from "@scaffoldapp/ui-web";
import { useMemo, type ReactNode } from "react";

/** The exchange's quote currency and a money formatter for it. */
export function useExchangeMoney() {
  const config = useExchangeConfig();
  const currency = config.data?.quote_currency ?? "USD";
  const decimals = config.data?.quote_decimals ?? 2;
  return useMemo(
    () => ({
      currency,
      decimals,
      config: config.data,
      money: (amount: string | null | undefined, signed = false) => formatExchangeMoney(amount, currency, decimals, signed),
    }),
    [currency, decimals, config.data],
  );
}

/** Text tone for a signed decimal: gains success, losses danger. */
export function signTone(value: string | null | undefined): string {
  const s = exchangeSign(value);
  return s > 0 ? "text-success-strong" : s < 0 ? "text-danger-strong" : "text-fg-muted";
}

/** A signed percentage, coloured by direction. */
export function ChangeText({ pct, className }: { pct: string | null | undefined; className?: string }) {
  return <span className={cn("tabular-nums", signTone(pct), className)}>{formatExchangePct(pct)}</span>;
}

const indexLabels: Record<ExchangeIndexStatus, string> = {
  none: "No price",
  seed: "Seed price",
  live: "Live",
  manual: "Manual price",
  stale: "Stale",
};

/** The index price's status when it isn't simply live. */
export function IndexStatusBadge({ status }: { status: ExchangeIndexStatus }) {
  if (status === "live") return null;
  const tone = status === "stale" || status === "none" ? "warning" : "neutral";
  return (
    <Badge tone={tone} size="sm">
      {indexLabels[status]}
    </Badge>
  );
}

/** Market status when trading is limited. */
export function MarketStatusBadge({ market }: { market: Pick<ExchangeMarket, "status"> }) {
  if (market.status === "active") return null;
  return (
    <Badge tone={market.status === "halted" ? "danger" : "warning"} size="sm">
      {market.status === "halted" ? "Paused" : "Reduce only"}
    </Badge>
  );
}

/** A tiny line of recent closes; colour follows the change over the line. */
export function Sparkline({ closes, className, label }: { closes: readonly string[] | undefined; className?: string; label: string }) {
  const points = useMemo(() => {
    const ys = (closes ?? []).map(Number).filter((n) => Number.isFinite(n));
    if (ys.length < 2) return null;
    const lo = Math.min(...ys);
    const hi = Math.max(...ys);
    const span = hi - lo || 1;
    return {
      up: (ys[ys.length - 1] ?? 0) >= (ys[0] ?? 0),
      d: ys.map((y, i) => `${((i / (ys.length - 1)) * 100).toFixed(2)},${(30 - ((y - lo) / span) * 28 - 1).toFixed(2)}`).join(" "),
    };
  }, [closes]);
  if (!points) return <span className={cn("block h-8 w-24", className)} aria-hidden />;
  return (
    <svg viewBox="0 0 100 30" preserveAspectRatio="none" role="img" aria-label={label} className={cn("block h-8 w-24", points.up ? "text-success" : "text-danger", className)}>
      <polyline points={points.d} fill="none" stroke="currentColor" strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/** The market's image when the catalog has one (never a stand-in). */
export function MarketImage({ market, size = 32 }: { market: Pick<ExchangeMarket, "image_url" | "name">; size?: number }) {
  if (!market.image_url) return null;
  // eslint-disable-next-line @next/next/no-img-element -- catalog images come from any host the app lists
  return <img src={market.image_url} alt="" width={size} height={size} className="shrink-0 rounded-md bg-sunken object-cover" style={{ width: size, height: size }} />;
}

/** A label above a value, for stat rows. */
export function Stat({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-0.5", className)}>
      <span className="type-caption text-fg-muted">{label}</span>
      <span className="truncate type-label tabular-nums text-fg">{children}</span>
    </div>
  );
}
