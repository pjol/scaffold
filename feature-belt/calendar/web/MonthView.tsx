"use client";

import {
  calendarAddDays,
  calendarDateKey,
  calendarMonthGrid,
  calendarOccurrenceKey,
  calendarOnDay,
  calendarSameMonth,
  calendarStartOfMonth,
  calendarTodayKey,
  calendarWeekdayNames,
  formatCalendarDayLong,
  formatCalendarMonth,
  formatCalendarTime,
  type CalendarOccurrence,
} from "@scaffoldapp/shared";
import { IconButton, cn, useMediaQuery } from "@scaffoldapp/ui-web";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useState, type KeyboardEvent } from "react";
import { ColorDot, occurrenceClasses, SourceMark, sourceSuffix } from "./parts";

/** The month grid: up to 3 events per day, the rest behind "+N more" (opens the day). */
export function MonthView({
  date,
  zone,
  weekStart,
  items,
  onOpen,
  onDay,
  onCreate,
}: {
  date: string;
  zone: string;
  weekStart: number;
  items: CalendarOccurrence[];
  onOpen: (o: CalendarOccurrence) => void;
  onDay: (date: string) => void;
  onCreate: (date: string) => void;
}) {
  const grid = calendarMonthGrid(date, weekStart);
  const today = calendarTodayKey(zone);
  const wide = useMediaQuery("(min-width: 768px)");
  const names = calendarWeekdayNames(weekStart, wide ? "short" : "narrow");
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="grid grid-cols-7 border-b border-border">
        {names.map((n, i) => (
          <div key={i} className="py-2 text-center type-caption uppercase text-fg-muted">
            {n}
          </div>
        ))}
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-7 grid-rows-6">
        {grid.map((d) => {
          const dayItems = calendarOnDay(items, d, zone).sort((a, b) => Number(b.all_day) - Number(a.all_day) || a.starts_at.localeCompare(b.starts_at));
          const shown = dayItems.slice(0, wide ? 3 : 0);
          const more = dayItems.length - shown.length;
          const inMonth = calendarSameMonth(d, date);
          return (
            <div
              key={d}
              className={cn("flex min-h-20 min-w-0 flex-col gap-0.5 border-r border-b border-border p-1", !inMonth && "bg-sunken")}
              onDoubleClick={(e) => {
                if (e.target === e.currentTarget) onCreate(d);
              }}
            >
              <button
                type="button"
                onClick={() => onDay(d)}
                aria-label={`${formatCalendarDayLong(d)}, ${dayItems.length} events`}
                aria-current={d === today ? "date" : undefined}
                className={cn(
                  "flex size-7 items-center justify-center self-start rounded-pill type-label tabular-nums focus-visible:outline-2 focus-visible:outline-ring",
                  d === today ? "bg-primary text-primary-fg" : inMonth ? "text-fg hover:bg-sunken" : "text-fg-subtle hover:bg-surface",
                )}
              >
                {Number(d.slice(8))}
              </button>
              {shown.map((o) => (
                <button
                  key={calendarOccurrenceKey(o)}
                  type="button"
                  onClick={() => onOpen(o)}
                  aria-label={`${o.all_day ? "All day" : formatCalendarTime(o.starts_at, zone)}, ${o.title}${sourceSuffix(o)}`}
                  className={cn(
                    "flex min-w-0 items-center gap-1 rounded-sm px-1 text-left type-caption focus-visible:outline-2 focus-visible:outline-ring",
                    o.all_day ? occurrenceClasses(o) : "text-fg hover:bg-sunken",
                  )}
                >
                  {!o.all_day ? <ColorDot color={o.color} /> : null}
                  {!o.all_day ? <span className="shrink-0 text-fg-muted tabular-nums">{formatCalendarTime(o.starts_at, zone).replace(":00", "")}</span> : null}
                  <SourceMark o={o} />
                  <span className={cn("truncate", o.my_response === "declined" && "line-through")}>{o.title}</span>
                </button>
              ))}
              {more > 0 ? (
                wide ? (
                  <button type="button" onClick={() => onDay(d)} className="self-start rounded-sm px-1 type-caption font-semibold text-fg-muted hover:text-fg">
                    +{more} more
                  </button>
                ) : (
                  <span className="flex flex-wrap gap-0.5 px-1" aria-hidden>
                    {dayItems.slice(0, 4).map((o) => (
                      <ColorDot key={calendarOccurrenceKey(o)} color={o.color} />
                    ))}
                  </span>
                )
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/** A small month for picking a date: arrow keys move, Enter picks. */
export function MiniMonth({
  value,
  zone,
  weekStart,
  onPick,
  marked,
  isEnabled,
  month: controlledMonth,
  onMonthChange,
  label = "Pick a date",
}: {
  value: string | null;
  zone: string;
  weekStart: number;
  onPick: (date: string) => void;
  marked?: (date: string) => boolean;
  isEnabled?: (date: string) => boolean;
  month?: string;
  onMonthChange?: (month: string) => void;
  label?: string;
}) {
  const [ownMonth, setOwnMonth] = useState(() => calendarStartOfMonth(value ?? calendarTodayKey(zone)));
  const month = controlledMonth ?? ownMonth;
  const setMonth = (m: string) => (onMonthChange ? onMonthChange(m) : setOwnMonth(m));
  const [focus, setFocus] = useState<string>(value ?? calendarTodayKey(zone));
  const today = calendarTodayKey(zone);
  const grid = calendarMonthGrid(month, weekStart);
  const names = calendarWeekdayNames(weekStart, "narrow");

  const move = (e: KeyboardEvent, d: string) => {
    const delta = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }[e.key];
    if (delta === undefined) return;
    e.preventDefault();
    const next = calendarAddDays(d, delta);
    setFocus(next);
    if (!calendarSameMonth(next, month)) setMonth(calendarStartOfMonth(next));
    requestAnimationFrame(() => document.querySelector<HTMLButtonElement>(`[data-minimonth="${next}"]`)?.focus());
  };

  return (
    <div className="flex flex-col gap-2" role="group" aria-label={label}>
      <div className="flex items-center gap-1">
        <span className="flex-1 type-label font-semibold text-fg">{formatCalendarMonth(month)}</span>
        <IconButton label="Previous month" icon={<ChevronLeft />} size="sm" variant="ghost" onClick={() => setMonth(calendarStartOfMonth(month, -1))} />
        <IconButton label="Next month" icon={<ChevronRight />} size="sm" variant="ghost" onClick={() => setMonth(calendarStartOfMonth(month, 1))} />
      </div>
      <div className="grid grid-cols-7 gap-y-1 text-center">
        {names.map((n, i) => (
          <span key={i} className="type-caption text-fg-subtle" aria-hidden>
            {n}
          </span>
        ))}
        {grid.map((d) => {
          const enabled = isEnabled ? isEnabled(d) : true;
          const selected = d === value;
          return (
            <button
              key={d}
              type="button"
              data-minimonth={d}
              tabIndex={d === focus ? 0 : -1}
              disabled={!enabled}
              onClick={() => onPick(d)}
              onKeyDown={(e) => move(e, d)}
              onFocus={() => setFocus(d)}
              aria-pressed={selected}
              aria-label={formatCalendarDayLong(d)}
              className={cn(
                "relative mx-auto flex size-8 items-center justify-center rounded-pill type-label tabular-nums focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:size-10",
                selected ? "bg-primary text-primary-fg" : d === today ? "text-primary-strong font-semibold" : calendarSameMonth(d, month) ? "text-fg" : "text-fg-subtle",
                enabled && !selected && "hover:bg-sunken",
                !enabled && "cursor-default opacity-40",
                isEnabled && enabled && !selected && "bg-primary-soft text-primary-strong font-semibold",
              )}
            >
              {Number(d.slice(8))}
              {marked?.(d) && !selected ? <span className="absolute bottom-0.5 size-1 rounded-pill bg-primary" aria-hidden /> : null}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** Today's key in a zone, for callers outside a calendar view. */
export function todayIn(zone: string): string {
  return calendarDateKey(Date.now(), zone);
}
