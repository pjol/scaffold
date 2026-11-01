/** Money and number formatting (integer minor units on the wire). */
export type CentsDisplay = "auto" | "always" | "never";

const cache = new Map<string, Intl.NumberFormat>();

function nf(key: string, opts: Intl.NumberFormatOptions, locale?: string): Intl.NumberFormat {
  const k = `${locale ?? ""}|${key}`;
  let f = cache.get(k);
  if (!f) {
    f = new Intl.NumberFormat(locale, opts);
    cache.set(k, f);
  }
  return f;
}

/** `128500` → `$1,285`; `1250` → `$12.50` (`auto` hides `.00`). */
export function formatCents(
  cents: number,
  opts: { cents?: CentsDisplay; currency?: string; locale?: string; signDisplay?: "auto" | "always" | "exceptZero" } = {},
): string {
  const mode = opts.cents ?? "auto";
  const whole = Math.round(cents) % 100 === 0;
  const digits = mode === "always" ? 2 : mode === "never" ? 0 : whole ? 0 : 2;
  const currency = opts.currency ?? "USD";
  const sign = opts.signDisplay ?? "auto";
  return nf(`cur-${currency}-${digits}-${sign}`, { style: "currency", currency, minimumFractionDigits: digits, maximumFractionDigits: digits, signDisplay: sign }, opts.locale).format(
    mode === "never" ? Math.round(cents / 100) : cents / 100,
  );
}

/** `12345` → `12,345`. */
export function formatInteger(n: number, locale?: string): string {
  return nf("int", { maximumFractionDigits: 0 }, locale).format(n);
}
