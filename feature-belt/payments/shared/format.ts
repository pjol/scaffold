/**
 * Money formatting for payments. Amounts are integer minor units in the
 * currency's own exponent: 1250 USD = $12.50, 1250 JPY = ¥1,250. Portable
 * (Intl only), so web and mobile format identically.
 */

const digitsCache = new Map<string, number>();
const formatCache = new Map<string, Intl.NumberFormat>();

/** Fraction digits of a currency (USD 2, JPY 0, BHD 3). */
export function currencyDigits(currency: string): number {
  const code = currency.toUpperCase();
  let d = digitsCache.get(code);
  if (d === undefined) {
    try {
      d = new Intl.NumberFormat("en-US", { style: "currency", currency: code }).resolvedOptions().maximumFractionDigits ?? 2;
    } catch {
      d = 2;
    }
    digitsCache.set(code, d);
  }
  return d;
}

/**
 * `formatMinor(1250, "usd")` → "$12.50"; whole amounts drop the decimals ("$9",
 * "$1,200"). `signed` shows "+" / "−".
 */
export function formatMinor(amountMinor: number, currency: string, options: { signed?: boolean; locale?: string } = {}): string {
  const code = currency.toUpperCase();
  const digits = currencyDigits(code);
  const value = amountMinor / 10 ** digits;
  const whole = Number.isInteger(value);
  const shown = whole ? 0 : digits;
  const key = `${options.locale ?? ""}|${code}|${shown}|${options.signed ? 1 : 0}`;
  let f = formatCache.get(key);
  if (!f) {
    try {
      f = new Intl.NumberFormat(options.locale ?? "en-US", {
        style: "currency",
        currency: code,
        minimumFractionDigits: shown,
        maximumFractionDigits: shown,
        signDisplay: options.signed ? "exceptZero" : "auto",
      });
    } catch {
      return `${value.toFixed(shown)} ${code}`;
    }
    formatCache.set(key, f);
  }
  return f.format(value);
}

/**
 * Parse a typed amount in major units ("12.5", "1,200") into minor units for
 * `currency`; null when it isn't a positive amount with at most the currency's
 * decimals.
 */
export function parseMinor(input: string, currency: string): number | null {
  const digits = currencyDigits(currency);
  const s = input.trim().replace(/,/g, "");
  const re = digits > 0 ? new RegExp(`^\\d+(\\.\\d{1,${digits}})?$`) : /^\d+$/;
  if (!re.test(s)) return null;
  const [whole = "0", frac = ""] = s.split(".");
  const minor = Number(whole) * 10 ** digits + Number(frac.padEnd(digits, "0") || "0");
  return Number.isSafeInteger(minor) && minor > 0 ? minor : null;
}
