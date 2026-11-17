"use client";

import { docsFormatNumber, docsOptionHue, docsTimeZone, useDocsChart, type DocsChartData, type DocsChartPoint, type DocsGroupInfo } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, Skeleton, cn } from "@scaffoldapp/ui-web";
import { ChartColumn } from "lucide-react";
import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { NEUTRAL_SWATCH, SERIES_HUES, SWATCH_CLASS } from "../format";
import type { ViewProps } from "./types";

type Mark = { fill: string; stroke: string; dot: string };

const H = 280;
const PAD = { top: 12, right: 12, bottom: 40, left: 48 };

/** Width of an element that may mount later (a callback ref). */
function useWidth(): [(el: HTMLDivElement | null) => void, number] {
  const [w, setW] = useState(0);
  const ro = useRef<ResizeObserver | null>(null);
  const ref = useCallback((el: HTMLDivElement | null) => {
    ro.current?.disconnect();
    ro.current = null;
    if (!el) return;
    ro.current = new ResizeObserver(() => setW(el.clientWidth));
    ro.current.observe(el);
    setW(el.clientWidth);
  }, []);
  return [ref, w];
}

function markFor(color: string | undefined, i: number): Mark {
  const hue = color ? docsOptionHue(color as never) : null;
  const h = hue ?? (color ? null : SERIES_HUES[i]);
  const s = h ? SWATCH_CLASS[h] : NEUTRAL_SWATCH;
  return { fill: s.fill, stroke: s.stroke, dot: s.dot };
}

/** Clean axis ticks from 0 to past max. */
function niceTicks(max: number, min = 0): number[] {
  const lo = Math.min(0, min);
  const span = Math.max(max - lo, 1);
  const raw = span / 4;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = ([1, 2, 2.5, 5, 10].find((m) => m * mag >= raw) ?? 10) * mag;
  const out: number[] = [];
  for (let v = Math.floor(lo / step) * step; v <= max + step * 0.001; v += step) out.push(+v.toFixed(10));
  if (out[out.length - 1]! < max) out.push(out[out.length - 1]! + step);
  return out;
}

function compact(v: number, top: number): string {
  return new Intl.NumberFormat(undefined, top >= 10_000 ? { notation: "compact", maximumFractionDigits: 1 } : { maximumFractionDigits: 2 }).format(v);
}

interface Series {
  key: string;
  label: string;
  mark: Mark;
}

/** Series to draw: option colours where they exist, else a fixed hue order; past five, the rest fold into "Other" when values add up. */
function seriesOf(data: DocsChartData, additive: boolean): { series: Series[]; note: string | null; fold: Set<string> } {
  // Series with no rows are left out; "no value" draws grey and takes no hue.
  const all = data.series.filter((g) => g.count > 0);
  const limit = SERIES_HUES.length;
  const fold = new Set<string>();
  let shown: DocsGroupInfo[] = all;
  let note: string | null = null;
  if (all.length > limit) {
    const ranked = [...all].sort((a, b) => b.count - a.count);
    const keep = new Set(ranked.slice(0, additive ? limit - 1 : limit).map((g) => g.key));
    shown = all.filter((g) => keep.has(g.key));
    if (additive) for (const g of all) if (!keep.has(g.key)) fold.add(g.key);
    else note = `Showing ${limit} of ${all.length} series.`;
  }
  let plain = 0;
  const neutral: Mark = { fill: NEUTRAL_SWATCH.fill, stroke: NEUTRAL_SWATCH.stroke, dot: NEUTRAL_SWATCH.dot };
  const series = shown.map((g) => ({ key: g.key, label: g.label, mark: g.key === "" ? neutral : g.color && docsOptionHue(g.color) ? markFor(g.color, 0) : markFor(undefined, plain++) }));
  if (fold.size) series.push({ key: "__other", label: "Other", mark: { fill: NEUTRAL_SWATCH.fill, stroke: NEUTRAL_SWATCH.stroke, dot: NEUTRAL_SWATCH.dot } });
  return { series, note, fold };
}

function valuesOf(p: DocsChartPoint, series: Series[], fold: Set<string>): Record<string, number> {
  const out: Record<string, number> = {};
  for (const s of series) out[s.key] = 0;
  for (const [k, v] of Object.entries(p.series ?? {})) {
    if (fold.has(k)) out["__other"] = (out["__other"] ?? 0) + v;
    else if (k in out) out[k] = v;
  }
  return out;
}

interface Tip {
  x: number;
  y: number;
  title: string;
  rows: { label: string; value: string; dot: string }[];
}

function Tooltip({ tip }: { tip: Tip | null }) {
  if (!tip) return null;
  return (
    <div role="status" style={{ left: tip.x, top: tip.y }} className="pointer-events-none absolute z-10 min-w-36 -translate-x-1/2 -translate-y-full rounded-md border border-border bg-surface-raised px-3 py-2 shadow-md">
      <p className="type-caption text-fg-muted">{tip.title}</p>
      {tip.rows.map((r) => (
        <p key={r.label} className="flex items-center gap-2 type-caption">
          <span aria-hidden className={cn("h-0.5 w-3 shrink-0 rounded-pill", r.dot)} />
          <span className="font-semibold tabular-nums text-fg">{r.value}</span>
          <span className="truncate text-fg-muted">{r.label}</span>
        </p>
      ))}
    </div>
  );
}

function Legend({ series }: { series: Series[] }) {
  if (series.length < 2) return null;
  return (
    <ul aria-label="Legend" className="flex flex-wrap gap-x-4 gap-y-1">
      {series.map((s) => (
        <li key={s.key} className="flex items-center gap-1.5 type-caption text-fg-muted">
          <span aria-hidden className={cn("size-2.5 rounded-xs", s.mark.dot)} />
          {s.label}
        </li>
      ))}
    </ul>
  );
}

/** A bar with a 4px rounded data end and a square base (vertical: up; horizontal: right). */
function barPath(x: number, y: number, w: number, h: number, horizontal: boolean, round: boolean): string {
  const r = round ? Math.min(4, horizontal ? h / 2 : w / 2, horizontal ? w : h) : 0;
  if (horizontal) return `M${x},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${y + h - r} Q${x + w},${y + h} ${x + w - r},${y + h} H${x} Z`;
  return `M${x},${y + h} V${y + r} Q${x},${y} ${x + r},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${y + h} Z`;
}

export function ChartView(props: ViewProps) {
  const chart = props.config.chart ?? { type: "bar" as const };
  const params = useMemo(() => ({ view: props.view.id, tz: docsTimeZone() }), [props.view.id]);
  const q = useDocsChart(props.token ? null : props.db.id, params);
  const [ref, width] = useWidth();
  const [tip, setTip] = useState<Tip | null>(null);
  const yProp = props.schema.find((p) => p.id === chart.y_property);
  const xProp = props.schema.find((p) => p.id === chart.x);
  const fmt = (v: number) => (chart.y && chart.y !== "count" && yProp ? docsFormatNumber(v, yProp.number_format, { stored: false, ...(yProp.decimals !== undefined ? { decimals: yProp.decimals } : {}) }) : new Intl.NumberFormat().format(v));
  if (props.token) return <p className="px-3 py-4 type-label text-fg-muted">Charts show to people in the workspace.</p>;
  if (!chart.x || !xProp) {
    return (
      <EmptyState
        icon={<ChartColumn />}
        title="Choose what to chart"
        action={props.onOpenSettings ? <Button onClick={() => props.onOpenSettings!("chart")}>Open chart settings</Button> : undefined}
      />
    );
  }
  if (q.isPending) return <Skeleton className="h-72 w-full rounded-md" />;
  if (q.isError) return <ErrorState title="Couldn't load the chart." onRetry={() => void q.refetch()} />;
  const data = q.data;
  if (!data.points.length) return <p className="px-3 py-4 type-label text-fg-muted">No rows to chart in this view.</p>;
  const additive = !chart.y || chart.y === "count" || chart.y === "sum";
  const hasSeries = !!chart.series && data.series.length > 0;
  const { series, note, fold } = hasSeries ? seriesOf(data, additive) : { series: [] as Series[], note: null, fold: new Set<string>() };
  // One series: points keep their option colours (the axis names them), else one hue.
  const pointMark = (p: DocsChartPoint, i: number): Mark => (p.color && docsOptionHue(p.color) ? markFor(p.color, i) : chart.type === "donut" ? markFor(undefined, i % SERIES_HUES.length) : markFor(undefined, 0));
  let body: ReactNode = null;

  if (chart.type === "donut") {
    const pts = data.points.filter((p) => p.value > 0);
    let plain = 0;
    const marks = pts.map((p, i) => (p.color && docsOptionHue(p.color) ? markFor(p.color, i) : plain < SERIES_HUES.length ? markFor(undefined, plain++) : { fill: NEUTRAL_SWATCH.fill, stroke: NEUTRAL_SWATCH.stroke, dot: NEUTRAL_SWATCH.dot }));
    const total = pts.reduce((a, p) => a + p.value, 0) || 1;
    const R = 100;
    const r = 64;
    let a0 = -Math.PI / 2;
    body = (
      <div className="flex flex-col items-center gap-4 sm:flex-row sm:items-start">
        <svg viewBox="-110 -110 220 220" className="size-56 shrink-0" role="img" aria-label={`${xProp.name}: ${pts.map((p) => `${p.label} ${fmt(p.value)}`).join(", ")}`}>
          {pts.map((p, i) => {
            const a1 = a0 + (p.value / total) * Math.PI * 2;
            const large = a1 - a0 > Math.PI ? 1 : 0;
            const path =
              pts.length === 1
                ? `M0,${-R} A${R},${R} 0 1 1 -0.01,${-R} L-0.01,${-r} A${r},${r} 0 1 0 0,${-r} Z`
                : `M${R * Math.cos(a0)},${R * Math.sin(a0)} A${R},${R} 0 ${large} 1 ${R * Math.cos(a1)},${R * Math.sin(a1)} L${r * Math.cos(a1)},${r * Math.sin(a1)} A${r},${r} 0 ${large} 0 ${r * Math.cos(a0)},${r * Math.sin(a0)} Z`;
            const mid = (a0 + a1) / 2;
            a0 = a1;
            return (
              <path
                key={p.key}
                d={path}
                className={cn(marks[i]!.fill, "stroke-surface outline-none hover:opacity-80 focus-visible:opacity-80")}
                strokeWidth={2}
                tabIndex={0}
                aria-label={`${p.label}: ${fmt(p.value)}`}
                onPointerEnter={() => setTip({ x: 112 + Math.cos(mid) * 90, y: 112 + Math.sin(mid) * 90, title: p.label, rows: [{ label: `${Math.round((p.value / total) * 100)}%`, value: fmt(p.value), dot: marks[i]!.dot }] })}
                onPointerLeave={() => setTip(null)}
                onFocus={() => setTip({ x: 112 + Math.cos(mid) * 90, y: 112 + Math.sin(mid) * 90, title: p.label, rows: [{ label: `${Math.round((p.value / total) * 100)}%`, value: fmt(p.value), dot: marks[i]!.dot }] })}
                onBlur={() => setTip(null)}
              />
            );
          })}
          <text textAnchor="middle" dy="-0.1em" className="fill-fg type-title-sm">
            {fmt(pts.reduce((a, p) => a + p.value, 0))}
          </text>
          <text textAnchor="middle" dy="1.4em" className="fill-fg-muted type-caption">
            {chart.y && chart.y !== "count" && yProp ? yProp.name : "Rows"}
          </text>
        </svg>
        <ul className="flex min-w-0 flex-col gap-1">
          {pts.map((p, i) => (
            <li key={p.key} className="flex items-center gap-2 type-label">
              <span aria-hidden className={cn("size-2.5 shrink-0 rounded-xs", marks[i]!.dot)} />
              <span className="min-w-0 truncate text-fg">{p.label}</span>
              <span className="tabular-nums text-fg-muted">{fmt(p.value)}</span>
              <span className="tabular-nums text-fg-subtle">{Math.round((p.value / total) * 100)}%</span>
            </li>
          ))}
        </ul>
      </div>
    );
  } else if (width > 0) {
    const horizontal = chart.type === "horizontal_bar";
    const line = chart.type === "line";
    const totals = data.points.map((p) => (hasSeries ? (line ? Math.max(0, ...Object.values(valuesOf(p, series, fold))) : Object.values(valuesOf(p, series, fold)).reduce((a, b) => a + b, 0)) : p.value));
    const minVal = Math.min(0, ...data.points.map((p) => (hasSeries ? Math.min(0, ...Object.values(valuesOf(p, series, fold))) : p.value)));
    const yt = niceTicks(Math.max(...totals, 0), minVal);
    const top = yt[yt.length - 1]!;
    const bottom = yt[0]!;
    if (horizontal) {
      const labelW = Math.min(160, Math.max(72, width * 0.3));
      const band = 32;
      const h = data.points.length * band + 28;
      const plotW = width - labelW - 56;
      const sx = (v: number) => labelW + ((v - bottom) / (top - bottom || 1)) * plotW;
      body = (
        <svg width={width} height={h} role="img" aria-label={`${xProp.name} by ${chart.y ?? "count"}`}>
          {yt.map((t) => (
            <g key={t}>
              <line x1={sx(t)} x2={sx(t)} y1={0} y2={h - 24} className="stroke-border" strokeWidth={1} />
              <text x={sx(t)} y={h - 8} textAnchor="middle" className="fill-fg-muted type-caption tabular-nums">
                {compact(t, top)}
              </text>
            </g>
          ))}
          {data.points.map((p, i) => {
            const y = i * band + 6;
            const bh = Math.min(20, band - 8);
            const vals = hasSeries ? valuesOf(p, series, fold) : null;
            let acc = 0;
            const tipRows = hasSeries ? series.map((s) => ({ label: s.label, value: fmt(vals![s.key] ?? 0), dot: s.mark.dot })) : [{ label: chart.y && chart.y !== "count" && yProp ? yProp.name : "Rows", value: fmt(p.value), dot: pointMark(p, i).dot }];
            const show = () => setTip({ x: Math.min(width - 80, sx(totals[i]!) + 40), y: y + 4, title: p.label, rows: tipRows });
            return (
              <g key={p.key} tabIndex={0} role="img" aria-label={`${p.label}: ${tipRows.map((r) => `${r.label} ${r.value}`).join(", ")}`} className="outline-none focus-visible:[&_text]:underline" onPointerEnter={show} onPointerLeave={() => setTip(null)} onFocus={show} onBlur={() => setTip(null)}>
                <rect x={0} y={y - 4} width={width} height={band} className="fill-transparent" />
                <text x={labelW - 8} y={y + bh / 2 + 4} textAnchor="end" className="fill-fg type-caption">
                  {p.label.length > 22 ? `${p.label.slice(0, 21)}…` : p.label}
                </text>
                {hasSeries ? (
                  series.map((s, si) => {
                    const v = vals![s.key] ?? 0;
                    if (v <= 0) return null;
                    const x0 = sx(acc);
                    acc += v;
                    const w = Math.max(sx(acc) - x0 - (si < series.length - 1 ? 2 : 0), 1);
                    const last = series.slice(si + 1).every((t) => (vals![t.key] ?? 0) <= 0);
                    return <path key={s.key} d={barPath(x0, y, w, bh, true, last)} className={s.mark.fill} />;
                  })
                ) : (
                  <path d={barPath(sx(Math.min(0, p.value)), y, Math.max(Math.abs(sx(p.value) - sx(0)), 1), bh, true, true)} className={pointMark(p, i).fill} />
                )}
                {chart.show_values ? (
                  <text x={sx(totals[i]!) + 6} y={y + bh / 2 + 4} className="fill-fg-muted type-caption tabular-nums">
                    {fmt(totals[i]!)}
                  </text>
                ) : null}
              </g>
            );
          })}
        </svg>
      );
    } else {
      const plotW = width - PAD.left - PAD.right;
      const plotH = H - PAD.top - PAD.bottom;
      const n = data.points.length;
      const bandW = plotW / n;
      const sy = (v: number) => PAD.top + plotH - ((v - bottom) / (top - bottom || 1)) * plotH;
      const cx = (i: number) => PAD.left + bandW * i + bandW / 2;
      const everyNth = Math.max(1, Math.ceil(n / Math.max(1, Math.floor(plotW / 64))));
      const lineSeries = hasSeries ? series : [{ key: "__v", label: chart.y && chart.y !== "count" && yProp ? yProp.name : "Rows", mark: markFor(undefined, 0) }];
      const valueAt = (p: DocsChartPoint, key: string) => (hasSeries ? (valuesOf(p, series, fold)[key] ?? 0) : p.value);
      body = (
        <svg width={width} height={H} role="img" aria-label={`${xProp.name} by ${chart.y ?? "count"}`}>
          {yt.map((t) => (
            <g key={t}>
              <line x1={PAD.left} x2={width - PAD.right} y1={sy(t)} y2={sy(t)} className="stroke-border" strokeWidth={1} />
              <text x={PAD.left - 6} y={sy(t) + 4} textAnchor="end" className="fill-fg-muted type-caption tabular-nums">
                {compact(t, top)}
              </text>
            </g>
          ))}
          {data.points.map((p, i) =>
            i % everyNth === 0 ? (
              <text key={p.key} x={cx(i)} y={H - PAD.bottom + 16} textAnchor="middle" className="fill-fg-muted type-caption">
                {p.label.length > 12 ? `${p.label.slice(0, 11)}…` : p.label}
              </text>
            ) : null,
          )}
          {line
            ? lineSeries.map((s) => (
                <polyline key={s.key} fill="none" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" className={s.mark.stroke} points={data.points.map((p, i) => `${cx(i)},${sy(valueAt(p, s.key))}`).join(" ")} />
              ))
            : null}
          {data.points.map((p, i) => {
            const vals = hasSeries ? valuesOf(p, series, fold) : null;
            const tipRows = lineSeries.map((s) => ({ label: s.label, value: fmt(valueAt(p, s.key)), dot: hasSeries ? s.mark.dot : pointMark(p, i).dot }));
            const show = () => setTip({ x: cx(i), y: Math.max(16, sy(totals[i]!) - 8), title: p.label, rows: tipRows });
            const bw = Math.min(24, bandW * 0.6);
            let acc = 0;
            return (
              <g key={p.key} tabIndex={0} role="img" aria-label={`${p.label}: ${tipRows.map((r) => `${r.label} ${r.value}`).join(", ")}`} className="group/pt outline-none" onPointerEnter={show} onPointerLeave={() => setTip(null)} onFocus={show} onBlur={() => setTip(null)}>
                <rect x={PAD.left + bandW * i} y={PAD.top} width={bandW} height={plotH} className="fill-transparent" />
                {line ? (
                  <>
                    <line x1={cx(i)} x2={cx(i)} y1={PAD.top} y2={PAD.top + plotH} className="stroke-border-strong opacity-0 group-hover/pt:opacity-100 group-focus-visible/pt:opacity-100" strokeWidth={1} />
                    {lineSeries.map((s) => (
                      <circle key={s.key} cx={cx(i)} cy={sy(valueAt(p, s.key))} r={4} strokeWidth={2} className={cn(hasSeries ? s.mark.fill : markFor(undefined, 0).fill, "stroke-surface")} />
                    ))}
                  </>
                ) : hasSeries ? (
                  series.map((s, si) => {
                    const v = vals![s.key] ?? 0;
                    if (v <= 0) return null;
                    const y0 = sy(acc);
                    acc += v;
                    const y1 = sy(acc);
                    const last = series.slice(si + 1).every((t) => (vals![t.key] ?? 0) <= 0);
                    const gap = si > 0 ? 2 : 0;
                    return <path key={s.key} d={barPath(cx(i) - bw / 2, y1, bw, Math.max(y0 - y1 - gap, 1), false, last)} className={s.mark.fill} />;
                  })
                ) : (
                  <path d={barPath(cx(i) - bw / 2, sy(Math.max(0, p.value)), bw, Math.max(Math.abs(sy(p.value) - sy(0)), 1), false, p.value >= 0)} className={cn(pointMark(p, i).fill, "group-hover/pt:opacity-80")} />
                )}
                {chart.show_values ? (
                  <text x={cx(i)} y={sy(totals[i]!) - 6} textAnchor="middle" className="fill-fg-muted type-caption tabular-nums">
                    {fmt(totals[i]!)}
                  </text>
                ) : null}
              </g>
            );
          })}
        </svg>
      );
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Legend series={series} />
        <span className="type-caption text-fg-muted">
          {data.total} {data.total === 1 ? "row" : "rows"}
          {note ? ` · ${note}` : ""}
        </span>
      </div>
      <div ref={ref} className="relative w-full" onPointerLeave={() => setTip(null)}>
        {body}
        <Tooltip tip={tip} />
      </div>
      <details className="rounded-md border border-border">
        <summary className="cursor-pointer select-none px-3 py-2 type-label text-fg-muted">Show values as a table</summary>
        <div className="overflow-x-auto border-t border-border">
          <table className="w-full border-collapse type-label">
            <thead>
              <tr>
                <th scope="col" className="px-3 py-1.5 text-left font-semibold text-fg-muted">
                  {xProp.name}
                </th>
                {hasSeries ? (
                  series.map((s) => (
                    <th key={s.key} scope="col" className="px-3 py-1.5 text-right font-semibold text-fg-muted">
                      {s.label}
                    </th>
                  ))
                ) : (
                  <th scope="col" className="px-3 py-1.5 text-right font-semibold text-fg-muted">
                    {chart.y && chart.y !== "count" && yProp ? `${chart.y} of ${yProp.name}` : "Rows"}
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {data.points.map((p) => {
                const vals = hasSeries ? valuesOf(p, series, fold) : null;
                return (
                  <tr key={p.key} className="border-t border-border">
                    <th scope="row" className="px-3 py-1.5 text-left font-normal text-fg">
                      {p.label}
                    </th>
                    {hasSeries ? (
                      series.map((s) => (
                        <td key={s.key} className="px-3 py-1.5 text-right tabular-nums text-fg">
                          {fmt(vals![s.key] ?? 0)}
                        </td>
                      ))
                    ) : (
                      <td className="px-3 py-1.5 text-right tabular-nums text-fg">{fmt(p.value)}</td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  );
}

