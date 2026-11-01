/**
 * Base-unit helpers. Token amounts travel as decimal strings of base units
 * ("1500000" with 6 decimals = 1.5); everything here is string arithmetic, so
 * no amount ever passes through a float or depends on BigInt support.
 */
import type { WalletChain, WalletsConfig } from "./types";

const DIGITS = /^\d+$/;

function stripZeros(s: string): string {
  const t = s.replace(/^0+/, "");
  return t === "" ? "0" : t;
}

/** True for a non-negative integer string ("0", "1500000"). */
export function isBaseUnits(value: string | null | undefined): value is string {
  return typeof value === "string" && DIGITS.test(value);
}

/** "0" or any run of zeros. */
export function isZeroUnits(amount: string): boolean {
  return /^0*$/.test(amount);
}

/** Sum of two base-unit strings. */
export function addUnits(a: string, b: string): string {
  let i = a.length - 1;
  let j = b.length - 1;
  let carry = 0;
  let out = "";
  while (i >= 0 || j >= 0 || carry) {
    const d = (i >= 0 ? a.charCodeAt(i) - 48 : 0) + (j >= 0 ? b.charCodeAt(j) - 48 : 0) + carry;
    out = String(d % 10) + out;
    carry = d >= 10 ? 1 : 0;
    i--;
    j--;
  }
  return stripZeros(out);
}

/** -1, 0 or 1. */
export function compareUnits(a: string, b: string): number {
  const x = stripZeros(a);
  const y = stripZeros(b);
  if (x.length !== y.length) return x.length < y.length ? -1 : 1;
  return x === y ? 0 : x < y ? -1 : 1;
}

/** Add 1 to the last digit of a digit string (rounding). */
function increment(digits: string): string {
  return addUnits(digits, "1").padStart(digits.length, "0");
}

function group(whole: string, separator: string): string {
  if (!separator) return whole;
  let out = "";
  for (let i = 0; i < whole.length; i++) {
    if (i > 0 && (whole.length - i) % 3 === 0) out += separator;
    out += whole[i];
  }
  return out;
}

export interface FormatUnitsOptions {
  /** Most digits after the point; the rest rounds half up. Default 6 (never more than `decimals`). */
  maxFractionDigits?: number;
  /** Fewest digits after the point (pads zeros). Default 0. */
  minFractionDigits?: number;
  /** Thousands separator. Default ",". Pass "" for none. */
  separator?: string;
}

/**
 * Base units → display string: `formatUnits("1234567890", 6)` = "1,234.56789".
 * Rounds half up at `maxFractionDigits`; a non-zero amount that rounds to zero
 * shows as "<0.000001" instead of a misleading "0".
 */
export function formatUnits(amount: string, decimals: number, options: FormatUnitsOptions = {}): string {
  if (!isBaseUnits(amount) || !Number.isInteger(decimals) || decimals < 0) return "—";
  const sep = options.separator ?? ",";
  const maxFrac = Math.min(options.maxFractionDigits ?? 6, decimals);
  const minFrac = Math.min(options.minFractionDigits ?? 0, maxFrac);
  const padded = stripZeros(amount).padStart(decimals + 1, "0");
  let whole = padded.slice(0, padded.length - decimals);
  let frac = padded.slice(padded.length - decimals);
  if (frac.length > maxFrac) {
    const roundUp = (frac.charCodeAt(maxFrac) - 48) >= 5;
    frac = frac.slice(0, maxFrac);
    if (roundUp) {
      const bumped = increment(whole + frac);
      whole = bumped.slice(0, bumped.length - maxFrac) || "0";
      frac = maxFrac > 0 ? bumped.slice(bumped.length - maxFrac) : "";
    }
  }
  frac = frac.replace(/0+$/, "");
  if (frac.length < minFrac) frac = frac.padEnd(minFrac, "0");
  const wholeOut = group(stripZeros(whole), sep);
  if (wholeOut === "0" && frac.replace(/0/g, "") === "" && !isZeroUnits(amount) && maxFrac > 0) {
    return `<0.${"0".repeat(maxFrac - 1)}1`;
  }
  return frac ? `${wholeOut}.${frac}` : wholeOut;
}

/**
 * Display string → base units, or null when it isn't a plain non-negative
 * number or has more fraction digits than `decimals`: `parseUnits("1.5", 6)` = "1500000".
 * Commas and spaces are ignored.
 */
export function parseUnits(value: string, decimals: number): string | null {
  const v = value.replace(/[,\s]/g, "");
  const m = /^(\d*)(?:\.(\d*))?$/.exec(v);
  if (!m || v === "" || v === "." || !Number.isInteger(decimals) || decimals < 0) return null;
  const whole = m[1] ?? "";
  const frac = m[2] ?? "";
  if (frac.length > decimals) return null;
  return stripZeros(whole + frac.padEnd(decimals, "0"));
}

/** "0x1234…abcd". */
export function shortAddress(address: string, chars = 4): string {
  if (address.length <= 2 + chars * 2 + 1) return address;
  return `${address.slice(0, 2 + chars)}…${address.slice(-chars)}`;
}

/** Explorer link for an address, or null when the chain has no explorer. */
export function explorerAddressUrl(chain: Pick<WalletChain, "explorer_url"> | undefined, address: string): string | null {
  return chain?.explorer_url ? `${chain.explorer_url}/address/${address}` : null;
}

/** The configured chain with this id. */
export function findChain(config: WalletsConfig | undefined, chainId: number): WalletChain | undefined {
  return config?.chains.find((c) => c.id === chainId);
}

/** True when the server has at least one chain configured. */
export function walletsConfigured(config: WalletsConfig | undefined): boolean {
  return (config?.chains.length ?? 0) > 0;
}

const dayFormat = new Intl.DateTimeFormat("en-US", { weekday: "short", month: "short", day: "numeric" });
const dayYearFormat = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

/** Heading for a day of activity: "Today", "Yesterday", "Wed, Oct 29", "Oct 29, 2025" (device time zone). */
export function activityDayLabel(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
  if (d.toDateString() === now.toDateString()) return "Today";
  if (d.toDateString() === yesterday.toDateString()) return "Yesterday";
  return d.getFullYear() === now.getFullYear() ? dayFormat.format(d) : dayYearFormat.format(d);
}

/** Consecutive items grouped under day headings (the list is already newest first). */
export function groupByDay<T extends { occurred_at: string }>(items: readonly T[], now: Date = new Date()): { label: string; items: T[] }[] {
  const out: { label: string; items: T[] }[] = [];
  for (const item of items) {
    const label = activityDayLabel(item.occurred_at, now);
    const last = out[out.length - 1];
    if (last && last.label === label) last.items.push(item);
    else out.push({ label, items: [item] });
  }
  return out;
}
