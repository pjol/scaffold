/**
 * Display helpers for exchange decimal strings. They work on the strings themselves (no
 * float conversion), so what the server computed is what people see; only sorting and
 * charts read numbers.
 */
import type { ExchangeCandle, ExchangeCandleInterval, ExchangeCandlePage } from "./types";
import { EXCHANGE_INTERVAL_MS } from "./types";

/** Decimals of a tick or step size ("0.01" → 2, "5" → 0). */
export function exchangeDecimals(step: string): number {
  const i = step.indexOf(".");
  return i < 0 ? 0 : step.length - i - 1;
}

/** "1234567.5" → "1,234,567.5" (sign kept, decimals untouched). */
export function groupExchangeDigits(value: string): string {
  const neg = value.startsWith("-");
  const s = neg || value.startsWith("+") ? value.slice(1) : value;
  const [int = "0", frac] = s.split(".");
  const grouped = int.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return (neg ? "-" : "") + grouped + (frac !== undefined ? `.${frac}` : "");
}

/** Round a decimal string to `decimals` places (half away from zero), as a string. */
export function roundExchangeDecimal(value: string, decimals: number): string {
  const neg = value.startsWith("-");
  const s = neg || value.startsWith("+") ? value.slice(1) : value;
  const [rawInt = "0", rawFrac = ""] = s.split(".");
  const int = rawInt === "" ? "0" : rawInt;
  if (rawFrac.length <= decimals) {
    const frac = rawFrac.padEnd(decimals, "0");
    const out = decimals > 0 ? `${int}.${frac}` : int;
    return neg && !/^[0.]*$/.test(out) ? `-${out}` : out;
  }
  const keep = (int + rawFrac.slice(0, decimals)).split("").map(Number);
  if (Number(rawFrac[decimals]) >= 5) {
    let i = keep.length - 1;
    while (i >= 0) {
      if (keep[i] === 9) {
        keep[i] = 0;
        i -= 1;
      } else {
        keep[i] = (keep[i] ?? 0) + 1;
        break;
      }
    }
    if (i < 0) keep.unshift(1);
  }
  const digits = keep.join("");
  const intLen = digits.length - decimals;
  const out = decimals > 0 ? `${digits.slice(0, intLen) || "0"}.${digits.slice(intLen)}` : digits;
  return neg && !/^[0.]*$/.test(out) ? `-${out}` : out;
}

/** Shift the decimal point (×10^places): "0.0000125", 2 → "0.00125". */
export function shiftExchangeDecimal(value: string, places: number): string {
  const neg = value.startsWith("-");
  const s = neg ? value.slice(1) : value;
  const [int = "0", frac = ""] = s.split(".");
  let digits = int + frac;
  let point = int.length + places;
  if (point < 0) {
    digits = "0".repeat(-point) + digits;
    point = 0;
  }
  if (point > digits.length) digits = digits.padEnd(point, "0");
  const outInt = digits.slice(0, point).replace(/^0+(?=\d)/, "") || "0";
  const outFrac = digits.slice(point).replace(/0+$/, "");
  return (neg ? "-" : "") + outInt + (outFrac ? `.${outFrac}` : "");
}

/** -1, 0 or 1 for a decimal string. */
export function exchangeSign(value: string | null | undefined): -1 | 0 | 1 {
  if (!value) return 0;
  if (/^[-+]?[0.]*$/.test(value)) return 0;
  return value.startsWith("-") ? -1 : 1;
}

/** A price at the market's tick precision, grouped: "2,651.4". */
export function formatExchangePrice(px: string | null | undefined, tickSize?: string): string {
  if (!px) return "–";
  return groupExchangeDigits(tickSize ? roundExchangeDecimal(px, exchangeDecimals(tickSize)) : px);
}

/** A size at the market's size decimals. */
export function formatExchangeSize(sz: string | null | undefined, szDecimals?: number): string {
  if (!sz) return "–";
  return groupExchangeDigits(szDecimals === undefined ? sz : roundExchangeDecimal(sz, szDecimals));
}

const currencySymbols: Record<string, string> = { USD: "$", EUR: "€", GBP: "£", JPY: "¥" };

/** An amount in the quote currency: "$1,234.56", "1,234.56 USDC"; `signed` adds "+" to gains. */
export function formatExchangeMoney(amount: string | null | undefined, currency: string, decimals = 2, signed = false): string {
  if (amount === null || amount === undefined || amount === "") return "–";
  const rounded = roundExchangeDecimal(amount, decimals);
  const sign = exchangeSign(rounded);
  const abs = groupExchangeDigits(rounded.replace(/^-/, ""));
  const prefix = sign < 0 ? "-" : signed && sign > 0 ? "+" : "";
  const sym = currencySymbols[currency];
  return sym ? `${prefix}${sym}${abs}` : `${prefix}${abs} ${currency}`;
}

/** "1.234" → "+1.23%" (signed). */
export function formatExchangePct(pct: string | null | undefined, decimals = 2): string {
  if (!pct) return "–";
  const r = roundExchangeDecimal(pct, decimals);
  return `${exchangeSign(r) > 0 ? "+" : ""}${r}%`;
}

/** An hourly funding rate as a percentage: "0.0000125" → "0.0013%". */
export function formatExchangeFunding(rate: string | null | undefined): string {
  if (!rate) return "–";
  const pct = roundExchangeDecimal(shiftExchangeDecimal(rate, 2), 4);
  return `${exchangeSign(pct) > 0 ? "+" : ""}${pct}%`;
}

/** Large amounts shortened for tables (display only): 1_250_000 → "1.25M". */
export function compactExchangeNumber(value: string | number | null | undefined): string {
  const n = typeof value === "string" ? Number(value) : (value ?? 0);
  if (!Number.isFinite(n)) return "–";
  const abs = Math.abs(n);
  const fmt = (x: number, unit: string) => `${n < 0 ? "-" : ""}${x >= 100 ? x.toFixed(0) : x >= 10 ? x.toFixed(1) : x.toFixed(2)}${unit}`;
  if (abs >= 1e9) return fmt(abs / 1e9, "B");
  if (abs >= 1e6) return fmt(abs / 1e6, "M");
  if (abs >= 1e3) return fmt(abs / 1e3, "K");
  return `${n < 0 ? "-" : ""}${abs.toFixed(abs >= 100 ? 0 : 2)}`;
}

/** "12:34" until `iso` (funding countdowns); "0:00" once passed. */
export function exchangeCountdown(iso: string | null | undefined, now: number): string {
  if (!iso) return "–";
  const left = Math.max(0, Math.floor((Date.parse(iso) - now) / 1000));
  const h = Math.floor(left / 3600);
  const m = Math.floor((left % 3600) / 60);
  const s = left % 60;
  const mm = String(m).padStart(h > 0 ? 2 : 1, "0");
  return h > 0 ? `${h}:${mm}:${String(s).padStart(2, "0")}` : `${mm}:${String(s).padStart(2, "0")}`;
}

/** Candle pages (newest page first, each oldest-first) as one oldest-first list without duplicates. */
export function flattenExchangeCandles(pages: readonly ExchangeCandlePage[] | undefined): ExchangeCandle[] {
  if (!pages) return [];
  const out: ExchangeCandle[] = [];
  let last = -Infinity;
  for (let i = pages.length - 1; i >= 0; i -= 1) {
    for (const c of pages[i]?.items ?? []) {
      if (c.t > last) {
        out.push(c);
        last = c.t;
      } else if (c.t === last) {
        out[out.length - 1] = c;
      }
    }
  }
  return out;
}

/** Put the live bucket of `interval` into the newest page (replacing or appending). */
export function mergeExchangeLiveCandle(page: ExchangeCandlePage, live: ExchangeCandle, interval: ExchangeCandleInterval): ExchangeCandlePage {
  const items = page.items.slice();
  const t = Math.floor(live.t / EXCHANGE_INTERVAL_MS[interval]) * EXCHANGE_INTERVAL_MS[interval];
  const last = items[items.length - 1];
  if (last && last.t === t) items[items.length - 1] = { ...live, t };
  else if (!last || last.t < t) items.push({ ...live, t });
  return { ...page, items };
}

/** Free collateral needed to open `notional` at `leverage` (estimate for tickets; the server decides). */
export function exchangeMarginFor(notional: number, leverage: number): number {
  return leverage > 0 ? notional / leverage : notional;
}
