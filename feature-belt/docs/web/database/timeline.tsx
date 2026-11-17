"use client";

import { docsFormatDateValue, docsTitle, type DocsDateValue, type DocsProperty, type DocsRow, type DocsValue } from "@scaffoldapp/shared";
import { Button, InfoTip, Select, Skeleton, cn } from "@scaffoldapp/ui-web";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from "react";
import { EditableValue, LoadMoreRow, RowMenu } from "./cells";
import { rowEditable } from "./model";
import type { ViewProps } from "./types";

type Zoom = "hours" | "day" | "week" | "month" | "quarter" | "year";

const DAY = 86_400_000;
/** Pixels per day at each zoom, and the least span (days) shown around today. */
const SCALE: Record<Zoom, { px: number; span: number; label: string }> = {
  hours: { px: 960, span: 4, label: "Hours" },
  day: { px: 48, span: 60, label: "Day" },
  week: { px: 20, span: 180, label: "Week" },
  month: { px: 8, span: 400, label: "Month" },
  quarter: { px: 3, span: 900, label: "Quarter" },
  year: { px: 1.2, span: 2200, label: "Year" },
};

const LANE = 40;
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** A date value's text as a floating timestamp (UTC fields hold the local wall time). */
function toMs(s: string): number {
  const [d, t] = s.split("T");
  const [y, m, day] = (d ?? "").split("-").map(Number);
  const [hh = 0, mm = 0] = (t ?? "").split(":").map(Number);
  return Date.UTC(y ?? 1970, (m ?? 1) - 1, day ?? 1, hh, mm);
}

function fromMs(ms: number, withTime: boolean): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, "0");
  const day = `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())}`;
  return withTime ? `${day}T${p(d.getUTCHours())}:${p(d.getUTCMinutes())}` : day;
}

function todayMs(): number {
  const n = new Date();
  return Date.UTC(n.getFullYear(), n.getMonth(), n.getDate(), n.getHours(), n.getMinutes());
}

interface Span {
  row: DocsRow;
  start: number;
  /** Exclusive end. */
  end: number;
  timed: boolean;
}

function spanOf(row: DocsRow, startP: DocsProperty, endP: DocsProperty | undefined): Span | null {
  let v: DocsDateValue | null = null;
  if (startP.type === "created_time" || startP.type === "last_edited_time") {
    const d = new Date(startP.type === "created_time" ? row.created_at : row.updated_at);
    const ms = Date.UTC(d.getFullYear(), d.getMonth(), d.getDate());
    return { row, start: ms, end: ms + DAY, timed: false };
  }
  const raw = row.props[startP.id] as DocsDateValue | null | undefined;
  if (raw && typeof raw === "object" && typeof raw.start === "string") v = raw;
  if (!v) return null;
  const timed = v.start.length > 10;
  const start = toMs(v.start);
  let endText = v.end;
  if (endP) {
    const ev = endP.type === "date" ? (row.props[endP.id] as DocsDateValue | null | undefined) : null;
    endText = ev && typeof ev.start === "string" ? (ev.end ?? ev.start) : undefined;
  }
  let end = endText ? toMs(endText) : start;
  if (!endText || endText.length <= 10) end += DAY;
  else if (end <= start) end = start + 3_600_000;
  if (end <= start) end = start + DAY;
  return { row, start, end, timed };
}

/** Header ticks for a zoom over [from, to). */
function ticks(zoom: Zoom, from: number, to: number): { top: { at: number; label: string }[]; bottom: { at: number; label: string }[] } {
  const top: { at: number; label: string }[] = [];
  const bottom: { at: number; label: string }[] = [];
  const d0 = new Date(from);
  if (zoom === "hours") {
    for (let t = from; t < to; t += DAY) {
      const d = new Date(t);
      top.push({ at: t, label: `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}` });
      for (let h = 0; h < 24; h += 3) bottom.push({ at: t + h * 3_600_000, label: `${String(h).padStart(2, "0")}:00` });
    }
    return { top, bottom };
  }
  if (zoom === "day" || zoom === "week") {
    for (let y = d0.getUTCFullYear(), m = d0.getUTCMonth(); Date.UTC(y, m, 1) < to; m === 11 ? ((m = 0), y++) : m++) top.push({ at: Math.max(from, Date.UTC(y, m, 1)), label: `${MONTHS[m]} ${y}` });
    for (let t = from; t < to; t += DAY) {
      const d = new Date(t);
      if (zoom === "day") bottom.push({ at: t, label: String(d.getUTCDate()) });
      else if (d.getUTCDay() === 1) bottom.push({ at: t, label: String(d.getUTCDate()) });
    }
    return { top, bottom };
  }
  for (let y = d0.getUTCFullYear(); Date.UTC(y, 0, 1) < to; y++) {
    top.push({ at: Math.max(from, Date.UTC(y, 0, 1)), label: String(y) });
    if (zoom === "year") continue;
    for (let m = 0; m < 12; m += zoom === "quarter" ? 3 : 1) {
      const at = Date.UTC(y, m, 1);
      if (at >= from && at < to) bottom.push({ at, label: zoom === "quarter" ? `Q${m / 3 + 1}` : MONTHS[m]! });
    }
  }
  return { top, bottom };
}

export function TimelineView(props: ViewProps) {
  const cfg = props.config;
  const startP = props.schema.find((p) => p.id === cfg.date_property);
  const endP = props.schema.find((p) => p.id === cfg.end_property);
  const depP = props.schema.find((p) => p.id === cfg.timeline?.dependency);
  const [zoom, setZoomState] = useState<Zoom>((cfg.timeline?.zoom as Zoom | undefined) ?? "month");
  const scale = SCALE[zoom];
  const scroller = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<{ id: string; mode: "move" | "start" | "end"; dx: number } | null>(null);
  const spans = useMemo(() => (startP ? props.rows.map((r) => spanOf(r, startP, endP)) : []), [props.rows, startP, endP]);
  const dated = spans.filter((s): s is Span => !!s);
  const undated = props.rows.filter((_, i) => !spans[i]);
  const now = todayMs();
  const today0 = now - (now % DAY);
  const range = useMemo(() => {
    let lo = today0 - Math.floor(scale.span / 3) * DAY;
    let hi = today0 + Math.ceil((scale.span * 2) / 3) * DAY;
    for (const s of dated) {
      lo = Math.min(lo, s.start - 7 * DAY);
      hi = Math.max(hi, s.end + 7 * DAY);
    }
    lo -= lo % DAY;
    hi -= hi % DAY;
    // Keep the canvas under ~40,000 px.
    const maxDays = Math.floor(40_000 / scale.px);
    if ((hi - lo) / DAY > maxDays) {
      lo = Math.max(lo, today0 - Math.floor(maxDays / 2) * DAY);
      hi = lo + maxDays * DAY;
    }
    return { lo, hi };
  }, [dated, today0, scale.span, scale.px]);
  const x = (ms: number) => ((ms - range.lo) / DAY) * scale.px;
  const width = x(range.hi);
  useEffect(() => {
    const el = scroller.current;
    if (el) el.scrollLeft = Math.max(0, x(now) - el.clientWidth / 3);
    // Center on today when the zoom changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [zoom, startP?.id]);
  if (!startP) return <p className="px-3 py-4 type-label text-fg-muted">Choose a date property for this timeline in the view's settings.</p>;
  const movable = startP.type === "date" && (!endP || endP.type === "date");
  const step = zoom === "hours" ? 3_600_000 : DAY;
  const setZoom = (z: Zoom) => {
    setZoomState(z);
    props.onConfigChange({ ...cfg, timeline: { ...(cfg.timeline ?? {}), zoom: z } });
  };
  const write = (s: Span, start: number, end: number) => {
    const timed = s.timed;
    const st = fromMs(start, timed);
    // Exclusive end back to the stored, inclusive form.
    const en = timed ? fromMs(end, true) : fromMs(end - DAY, false);
    const cur = s.row.props[startP.id] as DocsDateValue;
    const values: Record<string, DocsValue> = {};
    if (endP) {
      const next: DocsDateValue = { ...cur, start: st };
      delete next.end;
      values[startP.id] = next;
      const ce = s.row.props[endP.id] as DocsDateValue | undefined;
      values[endP.id] = { ...(ce ?? {}), start: en } as DocsDateValue;
      if (ce?.end) delete (values[endP.id] as DocsDateValue).end;
    } else {
      const next: DocsDateValue = { ...cur, start: st };
      if (en !== st) next.end = en;
      else delete next.end;
      values[startP.id] = next;
    }
    props.onMoveRow(s.row, { values });
  };
  const shifted = (s: Span, mode: "move" | "start" | "end", by: number) => {
    let start = s.start;
    let end = s.end;
    if (mode === "move") {
      start += by;
      end += by;
    } else if (mode === "start") start = Math.min(start + by, end - step);
    else end = Math.max(end + by, start + step);
    return { start, end };
  };
  const onPointerDown = (e: ReactPointerEvent<HTMLElement>, s: Span, mode: "move" | "start" | "end") => {
    if (!movable || !rowEditable(s.row, props.editable)) return;
    e.preventDefault();
    e.stopPropagation();
    const x0 = e.clientX;
    let moved = false;
    const move = (ev: PointerEvent) => {
      const dx = ev.clientX - x0;
      if (Math.abs(dx) > 3) moved = true;
      setDrag({ id: s.row.id, mode, dx });
    };
    const up = (ev: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      setDrag(null);
      const units = Math.round((((ev.clientX - x0) / scale.px) * DAY) / step);
      if (moved && units !== 0) {
        const n = shifted(s, mode, units * step);
        write(s, n.start, n.end);
      } else if (!moved && mode === "move") props.onOpenRow(s.row.id);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  const onKey = (e: KeyboardEvent<HTMLElement>, s: Span) => {
    if (e.key === "Enter") return props.onOpenRow(s.row.id);
    if (!movable || !rowEditable(s.row, props.editable)) return;
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const by = (e.key === "ArrowLeft" ? -1 : 1) * step;
    const n = shifted(s, e.shiftKey ? "end" : "move", by);
    write(s, n.start, n.end);
  };
  const t = ticks(zoom, range.lo, range.hi);
  const index = new Map(dated.map((s, i) => [s.row.id, i]));
  const live = (s: Span) => {
    if (!drag || drag.id !== s.row.id) return s;
    const units = Math.round(((drag.dx / scale.px) * DAY) / step);
    return { ...s, ...shifted(s, drag.mode, units * step) };
  };
  const arrows: { from: Span; to: Span }[] = [];
  if (depP) {
    for (const s of dated) {
      const blockers = s.row.props[depP.id];
      if (!Array.isArray(blockers)) continue;
      for (const id of blockers as string[]) {
        const i = index.get(id);
        if (i !== undefined) arrows.push({ from: dated[i]!, to: s });
      }
    }
  }
  const tableProps = cfg.timeline?.show_table ? (cfg.timeline.table_properties ?? []).map((id) => props.schema.find((p) => p.id === id)).filter((p): p is DocsProperty => !!p && p.type !== "title") : [];
  const showTable = !!cfg.timeline?.show_table;
  const height = Math.max(dated.length, 1) * LANE;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Select aria-label="Zoom" className="w-32" value={zoom} options={(Object.keys(SCALE) as Zoom[]).map((z) => ({ value: z, label: SCALE[z].label }))} onValueChange={(v) => setZoom(v as Zoom)} />
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            const el = scroller.current;
            if (el) el.scrollTo({ left: Math.max(0, x(now) - el.clientWidth / 3), behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth" });
          }}
        >
          Today
        </Button>
        {!movable ? <InfoTip content={`Bars follow ${startP.name}, which is worked out, so they can't be dragged.`} /> : null}
      </div>
      {props.loading && props.rows.length === 0 ? (
        <Skeleton className="h-48 w-full rounded-md" />
      ) : (
        <div className="flex overflow-hidden rounded-md border border-border">
          {showTable ? (
            <div className="w-40 shrink-0 overflow-hidden border-r border-border sm:w-auto sm:max-w-[50%] sm:overflow-x-auto">
              <table className="w-full table-fixed border-collapse sm:table-auto">
                <thead>
                  <tr className="h-14">
                    <th scope="col" className="border-b border-border px-2 text-left align-bottom type-caption font-normal text-fg-muted sm:min-w-40">
                      Name
                    </th>
                    {tableProps.map((p) => (
                      <th key={p.id} scope="col" className="hidden min-w-32 border-b border-l border-border px-2 text-left align-bottom type-caption font-normal text-fg-muted sm:table-cell">
                        {p.name}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {dated.map((s) => (
                    <tr key={s.row.id} style={{ height: LANE }} className="border-b border-border last:border-b-0">
                      <td className="max-w-48 px-2">
                        <div className="flex items-center gap-1">
                          <button type="button" onClick={() => props.onOpenRow(s.row.id)} className="min-w-0 flex-1 truncate text-left type-label text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
                            {s.row.icon ? `${s.row.icon} ` : ""}
                            {docsTitle(s.row.title)}
                          </button>
                          <RowMenu row={s.row} props={props} />
                        </div>
                      </td>
                      {tableProps.map((p) => (
                        <td key={p.id} className="hidden max-w-48 border-l border-border p-0 sm:table-cell">
                          <EditableValue p={p} row={s.row} props={props} className="min-h-9" />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
          <div ref={scroller} className="relative min-w-0 flex-1 overflow-x-auto">
            <div style={{ width }} className="relative">
              <div className="relative h-14 border-b border-border bg-sunken/40">
                {t.top.map((k) => (
                  <span key={`t${k.at}`} style={{ left: x(k.at) }} className="absolute top-1 whitespace-nowrap border-l border-border pl-1 type-caption font-semibold text-fg-muted">
                    {k.label}
                  </span>
                ))}
                {t.bottom.map((k) => (
                  <span key={`b${k.at}`} style={{ left: x(k.at) }} className="absolute bottom-1 whitespace-nowrap pl-1 type-caption tabular-nums text-fg-subtle">
                    {k.label}
                  </span>
                ))}
              </div>
              <div className="relative" style={{ height }}>
                {t.bottom.map((k) => (
                  <span key={`g${k.at}`} aria-hidden style={{ left: x(k.at) }} className="absolute inset-y-0 w-px bg-border/60" />
                ))}
                <span aria-hidden style={{ left: x(now) }} className="absolute inset-y-0 w-0.5 bg-danger" />
                {arrows.length ? (
                  <svg aria-hidden className="pointer-events-none absolute inset-0" width={width} height={height}>
                    <defs>
                      <marker id={`dep-${props.view.id}`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="8" markerHeight="8" orient="auto">
                        <path d="M0,0 L8,4 L0,8 z" className="fill-fg-muted" />
                      </marker>
                    </defs>
                    {arrows.map(({ from, to }, i) => {
                      const a = live(from);
                      const b = live(to);
                      const x1 = x(a.end);
                      const y1 = index.get(from.row.id)! * LANE + LANE / 2;
                      const x2 = x(b.start) - 2;
                      const y2 = index.get(to.row.id)! * LANE + LANE / 2;
                      const mid = Math.max(x1 + 8, Math.min(x2 - 8, (x1 + x2) / 2));
                      return <path key={i} d={`M${x1},${y1} H${mid} V${y2} H${x2}`} fill="none" strokeWidth={1.5} className="stroke-fg-muted" markerEnd={`url(#dep-${props.view.id})`} />;
                    })}
                  </svg>
                ) : null}
                {dated.map((s0, i) => {
                  const s = live(s0);
                  const left = x(s.start);
                  const w = Math.max(x(s.end) - left, 10);
                  const can = movable && rowEditable(s.row, props.editable);
                  const label = `${docsTitle(s.row.title)}: ${docsFormatDateValue({ start: fromMs(s.start, s.timed), ...(s.end - s.start > (s.timed ? 0 : DAY) ? { end: s.timed ? fromMs(s.end, true) : fromMs(s.end - DAY, false) } : {}) }, startP)}`;
                  return (
                    <div key={s.row.id} className="absolute" style={{ top: i * LANE + 6, left, width: w, height: LANE - 12 }}>
                      <div
                        role="button"
                        tabIndex={0}
                        aria-label={can ? `${label}. Enter opens; arrow keys move it; Shift and arrow keys change the end.` : label}
                        title={label}
                        onPointerDown={(e) => onPointerDown(e, s0, "move")}
                        onKeyDown={(e) => onKey(e, s0)}
                        className={cn(
                          "group/bar relative flex size-full items-center overflow-visible rounded-sm border border-primary bg-primary-soft px-2 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring",
                          can ? "cursor-grab active:cursor-grabbing" : "cursor-pointer",
                          drag?.id === s.row.id && "shadow-md",
                        )}
                      >
                        <span className="pointer-events-none truncate type-caption font-medium text-fg">{docsTitle(s.row.title)}</span>
                        {can ? (
                          <>
                            <span aria-hidden onPointerDown={(e) => onPointerDown(e, s0, "start")} className="absolute inset-y-0 left-0 w-2 cursor-ew-resize rounded-l-sm hover:bg-primary/40" />
                            <span aria-hidden onPointerDown={(e) => onPointerDown(e, s0, "end")} className="absolute inset-y-0 right-0 w-2 cursor-ew-resize rounded-r-sm hover:bg-primary/40" />
                          </>
                        ) : null}
                      </div>
                    </div>
                  );
                })}
                {dated.length === 0 ? <p className="sticky left-0 px-3 py-3 type-label text-fg-muted">No rows with a {startP.name.toLowerCase()} in this view.</p> : null}
              </div>
            </div>
          </div>
        </div>
      )}
      {undated.length ? (
        <section aria-label="Rows without dates" className="flex flex-col gap-1">
          <h4 className="px-1 type-label font-semibold text-fg-muted">No {startP.name.toLowerCase()} · {undated.length}</h4>
          <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
            {undated.map((row) => (
              <li key={row.id} className="flex min-h-control items-center gap-2 px-2">
                <button type="button" onClick={() => props.onOpenRow(row.id)} className="min-w-0 flex-1 truncate text-left type-label text-fg hover:underline focus-visible:outline-2 focus-visible:outline-ring">
                  {docsTitle(row.title)}
                </button>
                {movable && rowEditable(row, props.editable) ? (
                  <Button size="sm" variant="ghost" onClick={() => props.onMoveRow(row, { values: { [startP.id]: { start: fromMs(today0, false) } } })}>
                    Start today
                  </Button>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
      <LoadMoreRow props={props} label="Load more rows" />
      {props.editable && movable ? (
        <div>
          <Button size="sm" variant="ghost" onClick={() => props.onCreateRow({ [startP.id]: { start: fromMs(today0, false) } })}>
            New
          </Button>
        </div>
      ) : null}
    </div>
  );
}
