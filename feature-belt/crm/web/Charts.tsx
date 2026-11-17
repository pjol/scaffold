"use client";

import { CRM_TYPE_LABEL, crmCurrencyExponent, crmMoney, type CrmReportChart, type CrmReportResult, type CrmReportRow } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";

/*
 * Report charts in plain SVG/HTML, following the dataviz method:
 * - The form follows the data: one total is a figure, nominal groups are ranked horizontal bars,
 *   dates are columns or a line, "share" is a thin segmented bar beside its figures (the
 *   part-to-whole form the method uses instead of a ring). A one-bar chart becomes the figure.
 * - Colour: a single series always wears slot 1. Parts take the swatch hues in the order
 *   blue · orange · purple · yellow · pink (validated: worst adjacent CVD ΔE 11.6, normal 18.8 in
 *   light; 14.7 / 19.3 in dark), with the rest folded into "Other" in gray. Past five named parts
 *   nothing gets a new hue. The swatch `fg` steps sit lighter than the dark-mode band
 *   (L 0.72–0.78 vs 0.48–0.67), so legends, labels and the table view always carry identity too.
 * - Marks: bars ≤ 24 px with a 4 px rounded data end, 2 px lines with round caps, ≥ 8 px end dots
 *   with a 2 px surface ring, hairline solid grid, text in text tokens (never the series colour).
 * - Every chart has a table twin (`view="table"`), hover and keyboard read-outs, and holds its
 *   frame at reduced opacity while it refetches.
 */

export type CrmSeriesHue = "blue" | "orange" | "purple" | "yellow" | "pink";

/** Categorical order for parts (validated adjacent pairs; never cycled). */
export const CRM_SERIES: readonly CrmSeriesHue[] = ["blue", "orange", "purple", "yellow", "pink"];

// Full class names so Tailwind sees them.
const BG: Record<CrmSeriesHue | "gray", string> = {
  blue: "bg-swatch-blue",
  orange: "bg-swatch-orange",
  purple: "bg-swatch-purple",
  yellow: "bg-swatch-yellow",
  pink: "bg-swatch-pink",
  gray: "bg-swatch-gray",
};

type Result = Pick<CrmReportResult, "rows" | "total" | "count" | "value_type" | "currency" | "truncated" | "spec">;

// ---- numbers -------------------------------------------------------------------------------

/** A report value as text: money in its currency, counts whole, numbers to 2 decimals; `short` compacts from 10,000. */
export function crmReportValue(v: number, r: { value_type: CrmReportResult["value_type"]; currency?: string | undefined }, short = false): string {
  if (r.value_type === "money") {
    const cur = r.currency || "USD";
    return crmMoney(Math.round(v), cur, { short: short && Math.abs(v) >= 1_000_000 });
  }
  const opts: Intl.NumberFormatOptions = { maximumFractionDigits: r.value_type === "count" ? 0 : 2 };
  if (short && Math.abs(v) >= 10_000) {
    opts.notation = "compact";
    opts.maximumFractionDigits = 1;
  }
  return new Intl.NumberFormat(undefined, opts).format(v);
}

/** "1 deal", "12 deals". */
export function crmRecordCount(n: number, object: CrmReportResult["spec"]["object"]): string {
  const l = CRM_TYPE_LABEL[object];
  return `${new Intl.NumberFormat().format(n)} ${n === 1 ? l.one : l.many}`;
}

function percent(part: number, whole: number): string {
  if (whole <= 0) return "—";
  const p = (part / whole) * 100;
  return `${p > 0 && p < 1 ? "<1" : Math.round(p)} %`;
}

/** Shares only add up for counts and sums of non-negative values in groups that don't overlap (tags and multiple choice do). */
function sharesAddUp(r: Result): boolean {
  if (r.spec.measure === "avg" || !r.spec.group_by) return false;
  if (r.rows.some((x) => x.value < 0)) return false;
  const sum = r.rows.reduce((a, x) => a + x.value, 0);
  return !r.truncated && Math.abs(sum - r.total) <= Math.max(1e-6, Math.abs(r.total) * 1e-9);
}

// ---- date buckets ---------------------------------------------------------------------------

const MON = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function parts(key: string): [number, number, number] | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(key);
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : null;
}

function keyOf(t: number): string {
  const d = new Date(t);
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}-${String(d.getUTCDate()).padStart(2, "0")}`;
}

function nextKey(key: string, bucket: string): string | null {
  const p = parts(key);
  if (!p) return null;
  const [y, m, d] = p;
  switch (bucket) {
    case "day":
      return keyOf(Date.UTC(y, m - 1, d + 1));
    case "week":
      return keyOf(Date.UTC(y, m - 1, d + 7));
    case "quarter":
      return keyOf(Date.UTC(y, m + 2, 1));
    case "year":
      return keyOf(Date.UTC(y + 1, 0, 1));
    default:
      return keyOf(Date.UTC(y, m, 1));
  }
}

/** The server's label for a bucket starting on key (kept in step with bucketLabel in reports.go). */
function bucketLabel(key: string, bucket: string): string {
  const p = parts(key);
  if (!p) return key;
  const [y, m, d] = p;
  switch (bucket) {
    case "year":
      return String(y);
    case "quarter":
      return `Q${Math.floor((m - 1) / 3) + 1} ${y}`;
    case "month":
      return `${MON[m - 1]} ${y}`;
    case "week":
      return `Week of ${d} ${MON[m - 1]} ${y}`;
    default:
      return `${d} ${MON[m - 1]} ${y}`;
  }
}

function axisLabel(key: string, bucket: string, fallback: string): string {
  const p = parts(key);
  if (!p) return fallback;
  if (bucket === "day" || bucket === "week") return `${p[2]} ${MON[p[1] - 1]}`;
  return bucketLabel(key, bucket);
}

interface Point {
  key: string;
  label: string;
  axis: string;
  value: number;
  count: number;
}

/** Points along x: date buckets with the empty ones filled in (up to 400), or the groups in the server's order. */
function seriesPoints(r: Result): { points: Point[]; undated: CrmReportRow | null } {
  const bucket = r.spec.bucket;
  if (!bucket) return { points: r.rows.map((x) => ({ key: x.key, label: x.label, axis: x.label, value: x.value, count: x.count })), undated: null };
  const undated = r.rows.find((x) => x.key === "") ?? null;
  const dated = r.rows.filter((x) => x.key !== "").sort((a, b) => a.key.localeCompare(b.key));
  const byKey = new Map(dated.map((x) => [x.key, x]));
  const out: Point[] = [];
  const first = dated[0];
  const last = dated[dated.length - 1];
  let filled = false;
  if (first && last) {
    let k: string | null = first.key;
    let guard = 0;
    while (k && k <= last.key && guard < 400) {
      const row = byKey.get(k);
      out.push({ key: k, label: row?.label ?? bucketLabel(k, bucket), axis: axisLabel(k, bucket, row?.label ?? k), value: row?.value ?? 0, count: row?.count ?? 0 });
      k = nextKey(k, bucket);
      guard++;
    }
    filled = !!k && k > last.key;
  }
  if (!filled) return { points: dated.map((x) => ({ key: x.key, label: x.label, axis: axisLabel(x.key, bucket, x.label), value: x.value, count: x.count })), undated };
  return { points: out, undated };
}

// ---- layout helpers -------------------------------------------------------------------------

function useWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T | null>(null);
  const [w, setW] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    setW(el.getBoundingClientRect().width);
    const ro = new ResizeObserver((entries) => {
      const e = entries[0];
      if (e) setW(e.contentRect.width);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

function niceTicks(lo: number, hi: number, integer: boolean, count = 4): number[] {
  if (hi === lo) hi = lo + (integer ? 4 : 1);
  const raw = (hi - lo) / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const norm = raw / mag;
  let step = (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 2.5 ? 2.5 : norm <= 5 ? 5 : 10) * mag;
  if (integer) step = Math.max(1, Math.round(step));
  const start = Math.floor(lo / step) * step;
  const end = Math.ceil(hi / step) * step;
  const out: number[] = [];
  for (let v = start; v <= end + step / 2 && out.length < 12; v += step) out.push(Number(v.toPrecision(12)));
  return out;
}

/** Ticks in full under 10,000 (major units for money), compact on an axis that reaches it. */
function tickFormatter(r: Result, ticks: number[]): (v: number) => string {
  const top = Math.max(...ticks.map(Math.abs));
  if (r.value_type === "money") {
    const cur = r.currency || "USD";
    const major = top / 10 ** crmCurrencyExponent(cur);
    return (v) => crmMoney(Math.round(v), cur, { short: major >= 10_000 });
  }
  const fmt = new Intl.NumberFormat(undefined, top >= 10_000 ? { notation: "compact", maximumFractionDigits: 1 } : { maximumFractionDigits: 2 });
  return (v) => fmt.format(v);
}

/** A column with a 4 px rounded data end and a square base on the baseline. */
function columnPath(x: number, w: number, y0: number, y1: number): string {
  const h = Math.abs(y1 - y0);
  if (h < 0.5) return "";
  const r = Math.min(4, w / 2, h);
  if (y1 < y0) {
    // grows up
    return `M${x},${y0} V${y1 + r} Q${x},${y1} ${x + r},${y1} H${x + w - r} Q${x + w},${y1} ${x + w},${y1 + r} V${y0} Z`;
  }
  return `M${x},${y0} V${y1 - r} Q${x},${y1} ${x + r},${y1} H${x + w - r} Q${x + w},${y1} ${x + w},${y1 - r} V${y0} Z`;
}

// ---- the dispatcher ------------------------------------------------------------------------

export interface ReportChartProps {
  result: Result;
  /** The chart to draw; the form adapts when the data can't take it (one total, shares that don't add up). */
  chart: CrmReportChart;
  /** The report's name, for accessible names. */
  name: string;
  /** Column headings for the table: the grouping property and the measure ("Sum of Amount"). */
  groupLabel?: string;
  measureLabel?: string;
  view?: "chart" | "table";
  /** Refetching: hold the frame at reduced opacity. */
  stale?: boolean;
  /** Bars shown before "Show all" (dashboard cards). */
  maxBars?: number;
  className?: string;
}

/** One report result as its chart (or its table twin). */
export function ReportChart({ result, chart, name, groupLabel, measureLabel, view = "chart", stale, maxBars = 10, className }: ReportChartProps) {
  let body: ReactNode;
  const grouped = !!result.spec.group_by;
  if (result.count === 0 && result.rows.length === 0) {
    body = <p className="flex min-h-24 items-center justify-center type-label text-fg-muted">No records match</p>;
  } else if (view === "table" || chart === "table") {
    body = <ReportTable result={result} name={name} groupLabel={groupLabel} measureLabel={measureLabel} />;
  } else if (chart === "number" || !grouped) {
    body = <ReportFigure result={result} />;
  } else if (chart === "line") {
    body = <XYChart result={result} name={name} kind="line" />;
  } else if (chart === "bar" && result.spec.bucket) {
    body = <XYChart result={result} name={name} kind="column" />;
  } else if (chart === "donut" && sharesAddUp(result)) {
    body = <ShareChart result={result} name={name} />;
  } else {
    body = <RankedBars result={result} name={name} max={maxBars} />;
  }
  return <div className={cn("min-w-0 transition-opacity duration-(--app-duration-fast) motion-reduce:transition-none", stale && "opacity-60", className)}>{body}</div>;
}

// ---- figure -----------------------------------------------------------------------------------

/** The report's one number (stat tile): the total, and for sums and averages how many records it covers. */
export function ReportFigure({ result }: { result: Result }) {
  return (
    <div className="flex min-h-24 flex-col justify-center gap-1">
      <span className="type-display text-fg">{crmReportValue(result.total, result, true)}</span>
      {result.value_type !== "count" ? (
        <span className="type-caption text-fg-muted">
          {result.spec.measure === "avg" ? "Average over " : "Across "}
          {crmRecordCount(result.count, result.spec.object)}
        </span>
      ) : null}
    </div>
  );
}

// ---- ranked horizontal bars ----------------------------------------------------------------------

function RankedBars({ result, name, max }: { result: Result; name: string; max: number }) {
  const [all, setAll] = useState(false);
  const [hover, setHover] = useState<string | null>(null);
  const rows = result.rows;
  const shown = all ? rows : rows.slice(0, max);
  const lo = Math.min(0, ...rows.map((r) => r.value));
  const hi = Math.max(0, ...rows.map((r) => r.value));
  const span = hi - lo || 1;
  const zero = ((0 - lo) / span) * 100;
  const shares = result.spec.measure !== "avg" && result.total > 0 && rows.every((r) => r.value >= 0);
  return (
    <div className="flex flex-col gap-2">
      <ul className="flex flex-col" aria-label={name}>
        {shown.map((r) => {
          const left = r.value < 0 ? ((r.value - lo) / span) * 100 : zero;
          const width = (Math.abs(r.value) / span) * 100;
          const value = crmReportValue(r.value, result);
          return (
            <li
              key={r.key || "∅"}
              className={cn("relative flex min-h-8 items-center gap-3 rounded-sm px-1", hover === r.key && "bg-sunken")}
              onPointerEnter={() => setHover(r.key)}
              onPointerLeave={() => setHover((h) => (h === r.key ? null : h))}
            >
              <span className="w-1/3 min-w-0 shrink-0 truncate type-label text-fg" title={r.label}>
                {r.label}
              </span>
              <span className="relative h-3 min-w-0 flex-1" aria-hidden>
                {width > 0 ? (
                  <span
                    className={cn(
                      "absolute inset-y-0 bg-swatch-blue transition-[width,left] duration-(--app-duration-base) motion-reduce:transition-none",
                      r.value < 0 ? "rounded-l-xs" : "rounded-r-xs",
                    )}
                    style={{ left: `${left}%`, width: `max(${width}%, 2px)` }}
                  />
                ) : null}
              </span>
              <span className="shrink-0 text-right type-label tabular-nums text-fg">{value}</span>
              <span className="sr-only">
                , {crmRecordCount(r.count, result.spec.object)}
                {shares ? `, ${percent(r.value, result.total)} of the total` : ""}
              </span>
              {hover === r.key ? (
                <span className="pointer-events-none absolute right-0 bottom-full z-(--app-z-raised) mb-1 flex flex-col rounded-md border border-border bg-surface-raised px-3 py-2 shadow-md" aria-hidden>
                  <span className="type-label font-semibold tabular-nums text-fg">{value}</span>
                  <span className="type-caption text-fg-muted">
                    {r.label} · {crmRecordCount(r.count, result.spec.object)}
                    {shares ? ` · ${percent(r.value, result.total)}` : ""}
                  </span>
                </span>
              ) : null}
            </li>
          );
        })}
      </ul>
      {rows.length > max ? (
        <button type="button" onClick={() => setAll(!all)} className="self-start rounded-sm px-1 type-label font-semibold text-primary-strong hover:underline focus-visible:underline">
          {all ? "Show fewer" : `Show all ${rows.length}`}
        </button>
      ) : null}
      {result.truncated ? <p className="px-1 type-caption text-fg-muted">The first {rows.length} groups</p> : null}
    </div>
  );
}

// ---- share: thin segmented bar beside its figures --------------------------------------------------

interface Part {
  key: string;
  label: string;
  value: number;
  count: number;
  hue: CrmSeriesHue | "gray";
}

function shareParts(r: Result): Part[] {
  const sorted = [...r.rows].sort((a, b) => b.value - a.value);
  if (sorted.length <= CRM_SERIES.length) return sorted.map((x, i) => ({ ...x, hue: CRM_SERIES[i] ?? "gray" }));
  const named = sorted.slice(0, CRM_SERIES.length).map((x, i) => ({ ...x, hue: CRM_SERIES[i] ?? ("gray" as const) }));
  const rest = sorted.slice(CRM_SERIES.length);
  return [
    ...named,
    {
      key: "__other",
      label: "Other",
      value: rest.reduce((a, x) => a + x.value, 0),
      count: rest.reduce((a, x) => a + x.count, 0),
      hue: "gray",
    },
  ];
}

function ShareChart({ result, name }: { result: Result; name: string }) {
  const [active, setActive] = useState<string | null>(null);
  const items = useMemo(() => shareParts(result), [result]);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex h-2 w-full gap-0.5 overflow-hidden rounded-pill" aria-hidden>
        {items
          .filter((p) => p.value > 0)
          .map((p) => (
            <span
              key={p.key || "∅"}
              className={cn("h-full min-w-0.5 basis-0 transition-[flex-grow,opacity] duration-(--app-duration-base) motion-reduce:transition-none", BG[p.hue], active && active !== p.key && "opacity-40")}
              style={{ flexGrow: p.value }}
              onPointerEnter={() => setActive(p.key)}
              onPointerLeave={() => setActive(null)}
            />
          ))}
      </div>
      <ul className="flex flex-col" aria-label={name}>
        {items.map((p) => (
          <li
            key={p.key || "∅"}
            className={cn("flex min-h-8 items-center gap-2 rounded-sm px-1 type-label", active === p.key && "bg-sunken")}
            onPointerEnter={() => setActive(p.key)}
            onPointerLeave={() => setActive(null)}
          >
            <span className={cn("size-2.5 shrink-0 rounded-xs", BG[p.hue])} aria-hidden />
            <span className="min-w-0 flex-1 truncate text-fg" title={p.label}>
              {p.label}
            </span>
            <span className="shrink-0 tabular-nums text-fg">{crmReportValue(p.value, result)}</span>
            <span className="w-12 shrink-0 text-right tabular-nums text-fg-muted">{percent(p.value, result.total)}</span>
            <span className="sr-only">, {crmRecordCount(p.count, result.spec.object)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// ---- columns and line over x -----------------------------------------------------------------

const PLOT_H = 184;
const AXIS_H = 28;

function XYChart({ result, name, kind }: { result: Result; name: string; kind: "line" | "column" }) {
  const [ref, width] = useWidth<HTMLDivElement>();
  const [active, setActive] = useState<number | null>(null);
  const [focused, setFocused] = useState(false);
  const { points, undated } = useMemo(() => seriesPoints(result), [result]);
  const n = points.length;

  const values = points.map((p) => p.value);
  const ticks = niceTicks(Math.min(0, ...values), Math.max(0, ...values), result.value_type === "count");
  const fmtTick = tickFormatter(result, ticks);
  const lo = ticks[0] ?? 0;
  const hi = ticks[ticks.length - 1] ?? 1;
  const gutter = Math.min(88, Math.max(32, Math.max(...ticks.map((t) => fmtTick(t).length)) * 7 + 12));
  const right = kind === "line" ? 8 : 0;
  const plotW = Math.max(0, width - gutter - right);
  const top = 8;
  const y = (v: number) => top + (1 - (v - lo) / (hi - lo || 1)) * PLOT_H;
  const band = n > 0 ? plotW / n : 0;
  const x = (i: number) => (kind === "column" ? gutter + band * (i + 0.5) : gutter + (n <= 1 ? plotW / 2 : (i * plotW) / (n - 1)));
  const colW = Math.max(1, Math.min(24, band - 2));
  const maxLabels = Math.max(1, Math.floor(plotW / 76));
  const stride = Math.max(1, Math.ceil(n / maxLabels));
  const height = top + PLOT_H + AXIS_H;

  const pick = (clientX: number, el: SVGSVGElement) => {
    const rect = el.getBoundingClientRect();
    const px = clientX - rect.left;
    if (n === 0) return;
    let i: number;
    if (kind === "column") i = Math.floor((px - gutter) / (band || 1));
    else i = n <= 1 ? 0 : Math.round(((px - gutter) / (plotW || 1)) * (n - 1));
    setActive(Math.max(0, Math.min(n - 1, i)));
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (n === 0) return;
    const cur = active ?? (e.key === "ArrowLeft" ? n : -1);
    let next: number | null = null;
    if (e.key === "ArrowRight") next = Math.min(n - 1, cur + 1);
    else if (e.key === "ArrowLeft") next = Math.max(0, cur - 1);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = n - 1;
    else if (e.key === "Escape") next = null;
    else return;
    e.preventDefault();
    setActive(next);
  };

  const linePath = points.map((p, i) => `${i === 0 ? "M" : "L"}${x(i)},${y(p.value)}`).join(" ");
  const areaPath = n > 1 ? `${linePath} L${x(n - 1)},${y(Math.max(lo, 0))} L${x(0)},${y(Math.max(lo, 0))} Z` : "";
  const last = n - 1;
  const ap = active !== null ? points[active] : undefined;
  const readout = ap ? `${ap.label}: ${crmReportValue(ap.value, result)}, ${crmRecordCount(ap.count, result.spec.object)}` : "";
  const tipX = active !== null ? x(active) : 0;
  const tipAlign = tipX < 90 ? "translate-x-0" : tipX > width - 90 ? "-translate-x-full" : "-translate-x-1/2";

  return (
    <div className="flex flex-col gap-2">
      <div
        ref={ref}
        role="group"
        tabIndex={0}
        aria-label={`${name}: ${kind === "line" ? "line" : "column"} chart of ${n} points. Arrow keys read each value.`}
        onKeyDown={onKeyDown}
        onFocus={() => setFocused(true)}
        onBlur={() => {
          setFocused(false);
          setActive(null);
        }}
        className="relative w-full rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        style={{ height }}
      >
        {width > 0 && n > 0 ? (
          <svg
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            aria-hidden
            className="block touch-pan-y select-none"
            onPointerMove={(e: PointerEvent<SVGSVGElement>) => pick(e.clientX, e.currentTarget)}
            onPointerDown={(e: PointerEvent<SVGSVGElement>) => pick(e.clientX, e.currentTarget)}
            onPointerLeave={() => {
              if (!focused) setActive(null);
            }}
          >
            {/* grid and y ticks */}
            {ticks.map((t) => (
              <g key={t}>
                <line x1={gutter} x2={width - right} y1={y(t)} y2={y(t)} className={t === 0 ? "stroke-border-strong" : "stroke-border"} strokeWidth={1} shapeRendering="crispEdges" />
                <text x={gutter - 8} y={y(t)} dy="0.32em" textAnchor="end" className="fill-fg-muted text-caption tabular-nums">
                  {fmtTick(t)}
                </text>
              </g>
            ))}
            {/* x labels */}
            {points.map((p, i) =>
              i % stride === 0 ? (
                <text
                  key={p.key || i}
                  x={x(i)}
                  y={top + PLOT_H + 18}
                  textAnchor={kind === "line" && n > 1 ? (i === 0 ? "start" : i === last ? "end" : "middle") : "middle"}
                  className="fill-fg-muted text-caption"
                >
                  {p.axis.length > 12 ? `${p.axis.slice(0, 11)}…` : p.axis}
                </text>
              ) : null,
            )}
            {kind === "column" ? (
              <>
                {active !== null ? <rect x={gutter + band * active} y={top} width={band} height={PLOT_H} className="fill-sunken" /> : null}
                {points.map((p, i) => {
                  const d = columnPath(x(i) - colW / 2, colW, y(0), y(p.value));
                  return d ? <path key={p.key || i} d={d} className="fill-swatch-blue" /> : null;
                })}
              </>
            ) : (
              <>
                {areaPath ? <path d={areaPath} className="fill-swatch-blue/10" /> : null}
                <path d={linePath} fill="none" className="stroke-swatch-blue" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
                {active !== null ? <line x1={x(active)} x2={x(active)} y1={top} y2={top + PLOT_H} className="stroke-border-strong" strokeWidth={1} shapeRendering="crispEdges" /> : null}
                {/* end dot, and the hovered point */}
                {[last, ...(active !== null && active !== last ? [active] : [])].map((i) => {
                  const p = points[i];
                  return p ? <circle key={i} cx={x(i)} cy={y(p.value)} r={4} className="fill-swatch-blue stroke-surface" strokeWidth={2} /> : null;
                })}
                {/* direct label: the latest value */}
                {points[last] && active === null ? (
                  <text x={x(last)} y={y(points[last].value) - 12} textAnchor={n > 1 ? "end" : "middle"} className="fill-fg text-caption font-semibold tabular-nums">
                    {crmReportValue(points[last].value, result, true)}
                  </text>
                ) : null}
              </>
            )}
          </svg>
        ) : null}
        {ap ? (
          <div className={cn("pointer-events-none absolute top-0 z-(--app-z-raised) flex flex-col rounded-md border border-border bg-surface-raised px-3 py-2 shadow-md", tipAlign)} style={{ left: tipX }} aria-hidden>
            <span className="flex items-center gap-2 type-label font-semibold tabular-nums text-fg">
              {kind === "line" ? <span className="h-0.5 w-3 rounded-pill bg-swatch-blue" /> : null}
              {crmReportValue(ap.value, result)}
            </span>
            <span className="type-caption whitespace-nowrap text-fg-muted">
              {ap.label} · {crmRecordCount(ap.count, result.spec.object)}
            </span>
          </div>
        ) : null}
        <span className="sr-only" aria-live="polite">
          {focused ? readout : ""}
        </span>
      </div>
      {undated && undated.count > 0 ? (
        <p className="type-caption text-fg-muted">
          {crmRecordCount(undated.count, result.spec.object)} without a date ({crmReportValue(undated.value, result)}) are in the table only
        </p>
      ) : null}
      {result.truncated ? <p className="type-caption text-fg-muted">The first {result.rows.length} groups</p> : null}
    </div>
  );
}

// ---- the table twin ---------------------------------------------------------------------------

/** Every value of a report as a table, with the total. */
export function ReportTable({ result, name, groupLabel, measureLabel }: { result: Result; name: string; groupLabel?: string | undefined; measureLabel?: string | undefined }) {
  const measured = result.spec.measure !== "count";
  const shares = result.spec.measure !== "avg" && result.total > 0 && result.rows.every((r) => r.value >= 0);
  const overlap = result.rows.reduce((a, x) => a + x.count, 0) > result.count;
  const typeLabel = CRM_TYPE_LABEL[result.spec.object].Many;
  return (
    <div className="-mx-4 flex flex-col">
      <Table label={name}>
        <THead>
          <TH>{result.spec.group_by ? (groupLabel ?? "Group") : "All"}</TH>
          <TH align="right">{typeLabel}</TH>
          {measured ? <TH align="right">{measureLabel ?? (result.spec.measure === "avg" ? "Average" : "Sum")}</TH> : null}
          {shares ? (
            <TH align="right" {...(overlap ? { info: "A record can be in more than one group, so shares add up to more than 100 %." } : {})}>
              Share
            </TH>
          ) : null}
        </THead>
        <tbody>
          {result.spec.group_by
            ? result.rows.map((r) => (
                <TR key={r.key || "∅"}>
                  <TD className="max-w-48 truncate">{r.label}</TD>
                  <TD align="right">{new Intl.NumberFormat().format(r.count)}</TD>
                  {measured ? <TD align="right">{crmReportValue(r.value, result)}</TD> : null}
                  {shares ? <TD align="right">{percent(r.value, result.total)}</TD> : null}
                </TR>
              ))
            : null}
          <TR className="[&>td]:font-semibold">
            <TD>Total</TD>
            <TD align="right">{new Intl.NumberFormat().format(result.count)}</TD>
            {measured ? <TD align="right">{crmReportValue(result.total, result)}</TD> : null}
            {shares ? <TD align="right">{result.spec.group_by ? "100 %" : ""}</TD> : null}
          </TR>
        </tbody>
      </Table>
      {result.truncated ? <p className="px-4 pt-2 type-caption text-fg-muted">The first {result.rows.length} groups; the total counts every match</p> : null}
    </div>
  );
}

/** Chart choices in the builder. */
export const CRM_CHART_LABEL: Record<CrmReportChart, string> = {
  bar: "Bars",
  line: "Line",
  donut: "Share of total",
  table: "Table",
  number: "Number",
};
