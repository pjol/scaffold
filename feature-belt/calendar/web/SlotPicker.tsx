"use client";

import {
  calendarAddDays,
  calendarDateKey,
  calendarDayStart,
  calendarMonthGrid,
  calendarStartOfMonth,
  calendarTodayKey,
  formatCalendarDayLong,
  formatCalendarTime,
  type CalendarSlot,
} from "@scaffoldapp/shared";
import { Button, ErrorState, Skeleton, SkeletonGroup, cn } from "@scaffoldapp/ui-web";
import { Globe } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { MiniMonth } from "./MonthView";
import { ZoneSelect } from "./parts";

/** The window of instants a month view of `month` covers in `zone` (6 weeks). */
export function monthWindow(month: string, zone: string, weekStart: number): { from: string; to: string } {
  const grid = calendarMonthGrid(month, weekStart);
  return { from: calendarDayStart(grid[0]!, zone), to: calendarDayStart(calendarAddDays(grid[41]!, 1), zone) };
}

/**
 * Month with available days and the free times of the chosen day, in the visitor's zone
 * (switchable). Only free slots are shown, never what fills the rest of the day.
 */
export function SlotPicker({
  zone,
  onZoneChange,
  month,
  onMonthChange,
  weekStart = 0,
  slots,
  loading,
  error,
  onRetry,
  selected,
  onSelect,
  actionLabel = "Next",
  onConfirm,
}: {
  zone: string;
  onZoneChange?: (z: string) => void;
  month: string;
  onMonthChange: (m: string) => void;
  weekStart?: number;
  slots: CalendarSlot[];
  loading: boolean;
  error?: string | null;
  onRetry?: () => void;
  selected: string | null;
  onSelect: (slot: CalendarSlot | null) => void;
  actionLabel?: string;
  onConfirm?: () => void;
}) {
  const byDay = useMemo(() => {
    const m = new Map<string, CalendarSlot[]>();
    for (const s of slots) {
      const k = calendarDateKey(s.starts_at, zone);
      m.set(k, [...(m.get(k) ?? []), s]);
    }
    return m;
  }, [slots, zone]);
  const [day, setDay] = useState<string | null>(null);

  // Pick the first available day in view (this month first, else the spill-over days) once slots arrive.
  useEffect(() => {
    if (day && byDay.has(day)) return;
    const days = [...byDay.keys()].sort();
    setDay(days.find((d) => d.slice(0, 7) === month.slice(0, 7) && d >= calendarTodayKey(zone)) ?? days[0] ?? null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [byDay, month]);

  const daySlots = day ? (byDay.get(day) ?? []) : [];
  const anyThisMonth = [...byDay.keys()].some((d) => d.slice(0, 7) === month.slice(0, 7));

  return (
    <div className="grid gap-6 md:grid-cols-[minmax(0,18rem)_minmax(0,1fr)]">
      <div className="flex flex-col gap-4">
        <MiniMonth
          value={day}
          zone={zone}
          weekStart={weekStart}
          month={month}
          onMonthChange={(m) => {
            onSelect(null);
            onMonthChange(m);
          }}
          onPick={(d) => {
            setDay(d);
            onSelect(null);
          }}
          isEnabled={(d) => byDay.has(d)}
          label="Available days"
        />
        {onZoneChange ? (
          <div className="flex flex-col gap-2">
            <span className="flex items-center gap-1.5 type-label text-fg-muted">
              <Globe className="size-4" aria-hidden />
              Times shown in
            </span>
            <ZoneSelect value={zone} onChange={onZoneChange} />
          </div>
        ) : null}
      </div>
      <div className="flex min-w-0 flex-col gap-3">
        {loading && slots.length === 0 ? (
          <SkeletonGroup label="Loading times" className="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {Array.from({ length: 9 }, (_, i) => (
              <Skeleton key={i} className="h-11" />
            ))}
          </SkeletonGroup>
        ) : error ? (
          <ErrorState compact title="Couldn't load free times" info={error} {...(onRetry ? { onRetry } : {})} />
        ) : !day ? (
          <p className="type-body text-fg-muted">
            {anyThisMonth || byDay.size > 0 ? "Pick a day." : calendarStartOfMonth(calendarTodayKey(zone)) > month ? "This month has passed." : "No free times this month. Try the next one."}
          </p>
        ) : (
          <>
            <h3 className="type-label font-semibold text-fg">{formatCalendarDayLong(day)}</h3>
            <ul className={cn("grid grid-cols-2 gap-2 sm:grid-cols-3", loading && "opacity-70")} aria-label={`Free times on ${formatCalendarDayLong(day)}`}>
              {daySlots.map((s) => {
                const on = selected === s.starts_at;
                return (
                  <li key={s.starts_at}>
                    <button
                      type="button"
                      aria-pressed={on}
                      onClick={() => onSelect(on ? null : s)}
                      className={cn(
                        "flex min-h-11 w-full items-center justify-center rounded-md border type-label tabular-nums focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap",
                        on ? "border-primary bg-primary text-primary-fg" : "border-border-strong bg-surface text-primary-strong hover:border-primary",
                      )}
                    >
                      {formatCalendarTime(s.starts_at, zone)}
                    </button>
                  </li>
                );
              })}
            </ul>
            {selected && onConfirm ? (
              <Button size="auto" className="self-start" onClick={onConfirm}>
                {actionLabel}
              </Button>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}
