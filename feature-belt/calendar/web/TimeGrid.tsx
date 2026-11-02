"use client";

import {
  calendarDateKey,
  calendarFromWall,
  calendarLayoutDay,
  calendarMinutesOfDay,
  calendarOccurrenceKey,
  calendarOnDay,
  calendarTodayKey,
  calendarWeekday,
  formatCalendarTime,
  useNow,
  type CalendarExternalBusy,
  type CalendarHoursInterval,
  type CalendarOccurrence,
} from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { Repeat, Video } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { occurrenceClasses, SourceMark, sourceSuffix } from "./parts";

/** Pixels per hour in the time grid (48 = the touch target height). */
export const HOUR_PX = 48;
const SNAP = 15;
const DAY = 1440;

const snap = (m: number) => Math.round(m / SNAP) * SNAP;
const clampMin = (m: number) => Math.max(0, Math.min(DAY, m));

interface TimeGridProps {
  days: string[];
  zone: string;
  items: CalendarOccurrence[];
  busy: CalendarExternalBusy[];
  workingHours: CalendarHoursInterval[];
  onCreate: (startsAt: string, endsAt: string) => void;
  onCreateAllDay: (date: string) => void;
  onOpen: (o: CalendarOccurrence) => void;
  onMove: (o: CalendarOccurrence, startsAt: string, endsAt: string) => void;
}

type Drag =
  | { kind: "create"; day: number; anchor: number; start: number; end: number }
  | { kind: "move" | "resize"; o: CalendarOccurrence; x0: number; y0: number; day0: number; start0: number; dur: number; day: number; start: number; end: number; moved: boolean };

const hourLabel = (h: number) => (h === 0 ? "" : `${h % 12 === 0 ? 12 : h % 12} ${h < 12 ? "AM" : "PM"}`);

/**
 * Day or week time grid. Drag on empty time to create, drag an event to move it (across
 * days too), drag its bottom edge to resize. Keyboard: focus an event and press Alt+↑/↓ to
 * move 15 minutes, Alt+Shift+↑/↓ to change the end, Alt+←/→ to move a day; Enter opens it.
 */
export function TimeGrid({ days, zone, items, busy, workingHours, onCreate, onCreateAllDay, onOpen, onMove }: TimeGridProps) {
  const now = useNow(60_000);
  const today = calendarTodayKey(zone);
  const scrollRef = useRef<HTMLDivElement>(null);
  const colsRef = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<Drag | null>(null);

  const timed = useMemo(() => items.filter((o) => !o.all_day), [items]);
  const allDay = useMemo(() => items.filter((o) => o.all_day), [items]);

  // Start the view at 7:00 (or an hour before now when today is shown).
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const nowMin = calendarMinutesOfDay(Date.now(), zone);
    const visible = (el.clientHeight / HOUR_PX) * 60;
    // Morning by default; when today is shown and now would be below the fold, keep now in view.
    const target = days.includes(today) && nowMin > 7.5 * 60 + visible - 60 ? nowMin - visible / 2 : 7.5 * 60;
    el.scrollTop = (target / 60) * HOUR_PX;
    // Only when the visible days change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [days.join(",")]);

  const minuteAt = (clientY: number) => {
    const rect = colsRef.current?.getBoundingClientRect();
    return rect ? clampMin(((clientY - rect.top) / HOUR_PX) * 60) : 0;
  };
  const dayAt = (clientX: number) => {
    const rect = colsRef.current?.getBoundingClientRect();
    if (!rect) return 0;
    return Math.max(0, Math.min(days.length - 1, Math.floor(((clientX - rect.left) / rect.width) * days.length)));
  };

  const startCreate = (e: PointerEvent<HTMLDivElement>, day: number) => {
    if (e.button !== 0 || e.target !== e.currentTarget) return;
    const m = Math.floor(minuteAt(e.clientY) / SNAP) * SNAP;
    colsRef.current?.setPointerCapture(e.pointerId);
    setDrag({ kind: "create", day, anchor: m, start: m, end: Math.min(DAY, m + 30) });
  };

  const startMove = (e: PointerEvent<HTMLElement>, o: CalendarOccurrence, mode: "move" | "resize", day: number) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    if (o.role !== "owner") return; // guests open the event; only organizers move it
    colsRef.current?.setPointerCapture(e.pointerId);
    const start0 = calendarDateKey(o.starts_at, zone) < days[day]! ? 0 : calendarMinutesOfDay(o.starts_at, zone);
    const dur = Math.round((new Date(o.ends_at).getTime() - new Date(o.starts_at).getTime()) / 60_000);
    setDrag({ kind: mode, o, x0: e.clientX, y0: e.clientY, day0: day, start0, dur, day, start: start0, end: start0 + dur, moved: false });
  };

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!drag) return;
    if (drag.kind === "create") {
      const m = snap(minuteAt(e.clientY));
      setDrag({ ...drag, start: Math.min(drag.anchor, m), end: Math.max(drag.anchor + SNAP, m) });
      return;
    }
    const dy = ((e.clientY - drag.y0) / HOUR_PX) * 60;
    const moved = drag.moved || Math.abs(e.clientY - drag.y0) > 4 || Math.abs(e.clientX - drag.x0) > 4;
    if (drag.kind === "move") {
      const start = clampMin(snap(drag.start0 + dy));
      setDrag({ ...drag, moved, day: dayAt(e.clientX), start: Math.min(start, DAY - SNAP), end: Math.min(start, DAY - SNAP) + drag.dur });
    } else {
      setDrag({ ...drag, moved, end: Math.max(drag.start0 + SNAP, snap(drag.start0 + drag.dur + dy)) });
    }
  };

  const onPointerUp = () => {
    if (!drag) return;
    const d = drag;
    setDrag(null);
    if (d.kind === "create") {
      onCreate(calendarFromWall(days[d.day]!, d.start, zone), calendarFromWall(days[d.day]!, d.end, zone));
      return;
    }
    if (!d.moved) {
      onOpen(d.o);
      return;
    }
    const startsAt = calendarFromWall(days[d.day]!, d.start, zone);
    const endsAt = d.kind === "move" ? new Date(new Date(startsAt).getTime() + d.dur * 60_000).toISOString() : calendarFromWall(days[d.day]!, d.end, zone);
    if (startsAt !== d.o.starts_at || endsAt !== d.o.ends_at) onMove(d.o, startsAt, endsAt);
  };

  const onEventKey = (e: KeyboardEvent<HTMLButtonElement>, o: CalendarOccurrence) => {
    if (!e.altKey || o.role !== "owner" || !["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(e.key)) return;
    e.preventDefault();
    const s = new Date(o.starts_at).getTime();
    const en = new Date(o.ends_at).getTime();
    const step = SNAP * 60_000;
    const dayMs = 86_400_000;
    let ns = s;
    let ne = en;
    if (e.key === "ArrowUp" || e.key === "ArrowDown") {
      const dir = e.key === "ArrowUp" ? -1 : 1;
      if (e.shiftKey) ne = Math.max(s + step, en + dir * step);
      else {
        ns += dir * step;
        ne += dir * step;
      }
    } else {
      const dir = e.key === "ArrowLeft" ? -1 : 1;
      ns += dir * dayMs;
      ne += dir * dayMs;
    }
    onMove(o, new Date(ns).toISOString(), new Date(ne).toISOString());
  };

  const cols = `repeat(${days.length}, minmax(0, 1fr))`;
  const nowMin = calendarMinutesOfDay(now, zone);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Day headers and all-day events */}
      <div className="flex border-b border-border">
        <div className="w-14 shrink-0" />
        <div className="grid min-w-0 flex-1" style={{ gridTemplateColumns: cols }}>
          {days.map((d) => {
            const isToday = d === today;
            const dayItems = calendarOnDay(allDay, d, zone);
            const date = new Date(`${d}T12:00:00Z`);
            return (
              <div key={d} className="flex min-w-0 flex-col gap-1 border-l border-border px-1 pb-1">
                <div className="flex flex-col items-center gap-0.5 py-1">
                  <span className={cn("type-caption uppercase", isToday ? "text-primary-strong" : "text-fg-muted")}>
                    {date.toLocaleDateString("en-US", { weekday: "short", timeZone: "UTC" })}
                  </span>
                  <span
                    className={cn("flex size-8 items-center justify-center rounded-pill type-title-sm tabular-nums", isToday ? "bg-primary text-primary-fg" : "text-fg")}
                    aria-current={isToday ? "date" : undefined}
                  >
                    {date.getUTCDate()}
                  </span>
                </div>
                <div className="flex min-h-6 flex-col gap-0.5" onDoubleClick={() => onCreateAllDay(d)}>
                  {dayItems.map((o) => (
                    <button
                      key={calendarOccurrenceKey(o)}
                      type="button"
                      onClick={() => onOpen(o)}
                      aria-label={`${o.title}${sourceSuffix(o)}`}
                      className={cn("flex items-center gap-1 truncate rounded-sm px-1.5 text-left type-caption focus-visible:outline-2 focus-visible:outline-ring", occurrenceClasses(o))}
                    >
                      <SourceMark o={o} />
                      <span className="truncate">{o.title}</span>
                    </button>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* Hours */}
      <div ref={scrollRef} className="relative min-h-0 flex-1 overflow-y-auto overscroll-contain">
        <div className="flex">
          <div className="relative w-14 shrink-0" style={{ height: 24 * HOUR_PX }} aria-hidden>
            {Array.from({ length: 24 }, (_, h) => (
              <span key={h} className="absolute right-2 -translate-y-1/2 type-caption text-fg-subtle tabular-nums" style={{ top: h * HOUR_PX }}>
                {hourLabel(h)}
              </span>
            ))}
          </div>
          <div
            ref={colsRef}
            className={cn("relative grid min-w-0 flex-1 touch-pan-y select-none", drag && "cursor-grabbing")}
            style={{ gridTemplateColumns: cols, height: 24 * HOUR_PX }}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={() => setDrag(null)}
            onKeyDown={(e) => {
              if (e.key === "Escape") setDrag(null);
            }}
          >
            {days.map((d, di) => {
              const weekday = calendarWeekday(d);
              const work = workingHours.filter((h) => h.day === weekday);
              const placed = calendarLayoutDay(timed.filter((o) => !(drag && drag.kind !== "create" && drag.moved && o === drag.o)), d, zone, 20);
              const dayStart = calendarFromWall(d, 0, zone);
              const dayEnd = calendarFromWall(d, DAY, zone);
              const dayBusy = busy.filter((b) => b.starts_at < dayEnd && b.ends_at > dayStart);
              return (
                <div
                  key={d}
                  className="relative border-l border-border"
                  onPointerDown={(e) => startCreate(e, di)}
                  aria-label={`${d}: drag to add an event`}
                >
                  {/* Hour lines and non-working time */}
                  {Array.from({ length: 24 }, (_, h) => {
                    const inWork = work.some((w) => w.start <= h * 60 && w.end >= (h + 1) * 60);
                    return <div key={h} className={cn("pointer-events-none border-t border-border", !inWork && "bg-sunken")} style={{ height: HOUR_PX }} />;
                  })}
                  {/* Busy time from other calendars */}
                  {calendarLayoutDay(dayBusy, d, zone, 15).map((p, i) => (
                    <div
                      key={`busy-${i}`}
                      className="pointer-events-none absolute right-0.5 left-0.5 overflow-hidden rounded-sm border border-dashed border-border-strong bg-surface px-1 type-caption text-fg-muted"
                      style={{ top: (p.top / 60) * HOUR_PX, height: (p.height / 60) * HOUR_PX }}
                    >
                      Busy · {p.item.name}
                    </div>
                  ))}
                  {/* Events */}
                  {placed.map((p) => {
                    const o = p.item;
                    const short = p.height < 40;
                    return (
                      <button
                        key={calendarOccurrenceKey(o)}
                        type="button"
                        aria-keyshortcuts={o.role === "owner" ? "Alt+ArrowUp Alt+ArrowDown Alt+ArrowLeft Alt+ArrowRight" : undefined}
                        aria-label={`${o.title}, ${formatCalendarTime(o.starts_at, zone)} to ${formatCalendarTime(o.ends_at, zone)}${sourceSuffix(o)}`}
                        onPointerDown={(e) => startMove(e, o, "move", di)}
                        onClick={(e) => {
                          // Organizers' pointer clicks open on pointerup (after a possible drag);
                          // guests, keyboards and assistive tech open here.
                          if (e.detail === 0 || o.role !== "owner") onOpen(o);
                        }}
                        onKeyDown={(e) => onEventKey(e, o)}
                        className={cn(
                          "group absolute flex flex-col overflow-hidden rounded-sm px-1.5 text-left shadow-none ring-1 ring-surface focus-visible:z-20 focus-visible:outline-2 focus-visible:outline-ring",
                          short ? "justify-center" : "py-0.5",
                          occurrenceClasses(o),
                          o.role === "owner" ? "cursor-grab" : "cursor-pointer",
                        )}
                        style={{
                          top: (p.top / 60) * HOUR_PX,
                          height: Math.max((p.height / 60) * HOUR_PX - 1, 18),
                          left: `calc(${(p.column / p.columns) * 100}% + 1px)`,
                          width: `calc(${100 / p.columns}% - 3px)`,
                        }}
                      >
                        <span className="flex min-w-0 items-center gap-1 type-caption font-semibold">
                          <SourceMark o={o} />
                          <span className="truncate">{o.title}</span>
                          {o.recurring ? <Repeat className="size-3 shrink-0 opacity-70" aria-hidden /> : null}
                          {o.conference_url ? <Video className="size-3 shrink-0 opacity-70" aria-hidden /> : null}
                        </span>
                        {!short ? (
                          <span className="truncate type-caption opacity-80">
                            {formatCalendarTime(o.starts_at, zone)} – {formatCalendarTime(o.ends_at, zone)}
                          </span>
                        ) : null}
                        {o.role === "owner" ? (
                          <span
                            aria-hidden
                            className="absolute inset-x-0 bottom-0 h-2 cursor-ns-resize"
                            onPointerDown={(e) => startMove(e, o, "resize", di)}
                          />
                        ) : null}
                      </button>
                    );
                  })}
                  {/* Drag preview */}
                  {drag && drag.day === di && (drag.kind === "create" || drag.moved) ? (
                    <div
                      className="pointer-events-none absolute right-0.5 left-0.5 z-10 rounded-sm border border-primary bg-primary-soft px-1.5 type-caption font-semibold text-primary-strong"
                      style={{ top: (drag.start / 60) * HOUR_PX, height: Math.max(((drag.end - drag.start) / 60) * HOUR_PX, 12) }}
                    >
                      {drag.kind === "create" ? "New event" : drag.o.title} · {formatCalendarTime(calendarFromWall(d, drag.start, zone), zone)}
                    </div>
                  ) : null}
                  {/* Now */}
                  {d === today ? (
                    <div className="pointer-events-none absolute inset-x-0 z-10 flex items-center" style={{ top: (nowMin / 60) * HOUR_PX }} aria-hidden>
                      <span className="-ml-1 size-2 rounded-pill bg-danger" />
                      <span className="h-px flex-1 bg-danger" />
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
