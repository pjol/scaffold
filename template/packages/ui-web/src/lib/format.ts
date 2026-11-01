/** Number and money formatting. Money is integer minor units on the wire. */

export type CentsDisplay = "auto" | "always" | "never";

const cache = new Map<string, Intl.NumberFormat>();
function nf(key: string, opts: Intl.NumberFormatOptions, locale?: string) {
  const k = `${locale ?? ""}|${key}`;
  let f = cache.get(k);
  if (!f) {
    f = new Intl.NumberFormat(locale, opts);
    cache.set(k, f);
  }
  return f;
}

/**
 * Integer cents as currency. `"auto"` (default): `$1,234` for whole amounts, `$12.50`
 * otherwise; `"always"`: `$1,234.00`; `"never"`: rounded to whole units.
 */
export function formatCents(
  cents: number,
  opts: { cents?: CentsDisplay; currency?: string; locale?: string; signDisplay?: "auto" | "always" | "exceptZero" } = {},
): string {
  const mode = opts.cents ?? "auto";
  const currency = opts.currency ?? "USD";
  const value = cents / 100;
  const whole = Math.round(cents) % 100 === 0;
  const digits = mode === "always" ? 2 : mode === "never" ? 0 : whole ? 0 : 2;
  const sign = opts.signDisplay ?? "auto";
  return nf(`cur-${currency}-${digits}-${sign}`, { style: "currency", currency, minimumFractionDigits: digits, maximumFractionDigits: digits, signDisplay: sign }, opts.locale).format(
    mode === "never" ? Math.round(value) : value,
  );
}

/** Grouped integer: 12345 → "12,345". */
export function formatInteger(n: number, locale?: string): string {
  return nf("int", { maximumFractionDigits: 0 }, locale).format(n);
}
