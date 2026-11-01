/** A value that can appear in a query string. `undefined`/`null` entries are skipped. */
export type QueryValue = string | number | boolean | Date | null | undefined;
export type QueryParams = Record<string, QueryValue | readonly QueryValue[]>;

function encodeValue(value: Exclude<QueryValue, null | undefined>): string {
  if (value instanceof Date) return encodeURIComponent(value.toISOString());
  return encodeURIComponent(String(value));
}

/**
 * Build `?a=1&b=x` from an object, skipping `undefined`/`null` (arrays repeat the
 * key). Hand-rolled because React Native's `URLSearchParams` is incomplete.
 * Returns `""` when nothing remains.
 *
 * @example buildQueryString({ page: 2, q: "hello world", cursor: undefined }) === "?page=2&q=hello%20world"
 */
export function buildQueryString(params: QueryParams | undefined | null): string {
  if (!params) return "";
  const parts: string[] = [];
  for (const key of Object.keys(params)) {
    const raw = params[key];
    const values = Array.isArray(raw) ? (raw as readonly QueryValue[]) : [raw as QueryValue];
    for (const v of values) {
      if (v === undefined || v === null) continue;
      if (typeof v === "number" && !Number.isFinite(v)) continue;
      parts.push(`${encodeURIComponent(key)}=${encodeValue(v)}`);
    }
  }
  return parts.length ? `?${parts.join("&")}` : "";
}

/** Encode one path segment (ids, slugs, tokens). */
export function seg(value: string): string {
  return encodeURIComponent(value);
}
