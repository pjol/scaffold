import type { OracleDailyBar, OraclePrintKind, OracleStatus, OracleVerdict } from "./types";

/** Status labels for badges. */
export const ORACLE_STATUS_LABEL: Record<OracleStatus, string> = { live: "Live", stale: "Stale", seed: "Seed", off: "Off" };

/** What each status means, for an info tip. */
export const ORACLE_STATUS_INFO: Record<OracleStatus, string> = {
  live: "An accepted price arrived within the feed's stale-after time.",
  stale: "No accepted price within the feed's stale-after time. Don't rely on it until a source reports again.",
  seed: "No source has set a price yet; this is the starting estimate, if any.",
  off: "Turned off by staff.",
};

export const ORACLE_VERDICT_LABEL: Record<OracleVerdict, string> = { accepted: "Accepted", downweighted: "Downweighted", rejected: "Rejected" };

export const ORACLE_KIND_LABEL: Record<OraclePrintKind, string> = { sale: "Sale", market: "Market", listing: "Listing" };

export type OracleTone = "success" | "warning" | "neutral" | "danger";

export function oracleStatusTone(s: OracleStatus): OracleTone {
  return s === "live" ? "success" : s === "stale" ? "warning" : s === "off" ? "danger" : "neutral";
}

export function oracleVerdictTone(v: OracleVerdict): OracleTone {
  return v === "accepted" ? "success" : v === "downweighted" ? "warning" : "danger";
}

/** Decimal places of a decimal string ("12.30" → 2). */
function placesOf(value: string): number {
  const i = value.indexOf(".");
  return i < 0 ? 0 : value.length - i - 1;
}

/** `"2816.35"` → `"2,816.35"`, keeping the string's own places. Null → "—". */
export function formatOraclePrice(value: string | null | undefined, maxPlaces = 10): string {
  if (value === null || value === undefined || value === "") return "—";
  const n = Number(value);
  if (!Number.isFinite(n)) return value;
  const places = Math.min(placesOf(value), maxPlaces);
  return n.toLocaleString("en-US", { minimumFractionDigits: places, maximumFractionDigits: places });
}

/** `0.0298` → `"+3.0%"`; 0 → "0%". */
export function formatOraclePremium(p: number): string {
  if (!p) return "0%";
  return `${p > 0 ? "+" : "−"}${Math.abs(p * 100).toFixed(1)}%`;
}

/** Seconds → "45 min", "2 h", "30 d". */
export function formatOracleDuration(seconds: number): string {
  if (seconds % 86_400 === 0) return `${seconds / 86_400} d`;
  if (seconds % 3_600 === 0) return `${seconds / 3_600} h`;
  if (seconds % 60 === 0) return `${seconds / 60} min`;
  return `${seconds} s`;
}

/** Change between two decimal strings as a signed percent ("+1.2%"), or null. */
export function oracleChangePct(from: string | null | undefined, to: string | null | undefined): number | null {
  const a = Number(from);
  const b = Number(to);
  if (!from || !to || !Number.isFinite(a) || !Number.isFinite(b) || a <= 0) return null;
  return ((b - a) / a) * 100;
}

export function formatOraclePct(pct: number | null): string {
  if (pct === null) return "—";
  const v = Math.abs(pct) < 0.005 ? 0 : pct;
  return `${v > 0 ? "+" : v < 0 ? "−" : ""}${Math.abs(v).toFixed(2)}%`;
}

/** A chart series: x in ms, y as numbers, in time order. */
export interface OracleSeriesPoint {
  t: number;
  v: number;
}

/** Daily bars → close series (a bar's day at UTC noon). */
export function oracleBarsSeries(bars: OracleDailyBar[]): OracleSeriesPoint[] {
  return bars.map((b) => ({ t: Date.parse(`${b.day}T12:00:00Z`), v: Number(b.close) })).filter((p) => Number.isFinite(p.v));
}

/** Points scaled into a width × height box (y grows down), with the min and max values. */
export function oracleScale(points: OracleSeriesPoint[], width: number, height: number, pad = 2): { xy: [number, number][]; min: number; max: number } {
  if (points.length === 0) return { xy: [], min: 0, max: 0 };
  let min = Infinity;
  let max = -Infinity;
  for (const p of points) {
    min = Math.min(min, p.v);
    max = Math.max(max, p.v);
  }
  const t0 = points[0]?.t ?? 0;
  const t1 = points[points.length - 1]?.t ?? t0;
  const span = Math.max(1, t1 - t0);
  const range = max - min || Math.abs(max) * 0.01 || 1;
  const xy = points.map((p): [number, number] => [
    points.length === 1 ? width / 2 : ((p.t - t0) / span) * width,
    pad + (1 - (p.v - min) / range) * (height - 2 * pad),
  ]);
  return { xy, min, max };
}

/** An SVG path through scaled points. */
export function oraclePath(xy: [number, number][]): string {
  return xy.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`).join(" ");
}

/** The area under a path down to the bottom of a height-tall box. */
export function oracleAreaPath(xy: [number, number][], height: number): string {
  const first = xy[0];
  const last = xy[xy.length - 1];
  if (!first || !last) return "";
  return `${oraclePath(xy)} L${last[0].toFixed(1)} ${height} L${first[0].toFixed(1)} ${height} Z`;
}
