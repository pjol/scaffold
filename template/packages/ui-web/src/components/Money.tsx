"use client";

import { cn } from "../lib/cn";
import { formatCents, type CentsDisplay } from "../lib/format";
import { AnimatedNumber } from "./AnimatedNumber";

export interface MoneyProps {
  /** Integer minor units (the API's unit). */
  cents: number;
  /** `"auto"`: `$1,234` / `$12.50`. @default "auto" */
  decimals?: CentsDisplay;
  /** Roll digits when the amount changes. @default false */
  animate?: boolean;
  /** Show `+` / `−`. */
  signed?: boolean;
  /** Strike through (a replaced price). */
  strike?: boolean;
  /** ISO 4217. @default "USD" */
  currency?: string;
  className?: string;
}

/** Formats integer cents: `128500` → `$1,285`, `1250` → `$12.50`. */
export function Money({ cents, decimals = "auto", animate = false, signed, strike, currency, className }: MoneyProps) {
  const format = (n: number) => formatCents(n, { cents: decimals, ...(currency ? { currency } : {}), ...(signed ? { signDisplay: "exceptZero" as const } : {}) });
  return <AnimatedNumber value={cents} format={format} animate={animate} className={cn("font-semibold", strike && "line-through opacity-60", className)} />;
}
