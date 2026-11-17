"use client";

import {
  calendarAddDays,
  calendarDateKey,
  calendarDayStart,
  calendarMonthGrid,
  calendarSameMonth,
  calendarStartOfMonth,
  calendarTodayKey,
  calendarWeekdayNames,
  formatCalendarMonth,
  useCalendarRange,
  type CalendarOccurrence,
} from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { useMemo } from "react";

/** Days with something on them in [from, to), from two-month ranges (the API serves ≤ 62 days). */
function useYearMarks(year: string, zone: string, keep: (o: CalendarOccurrence) => boolean): { marks: Map<string, number>; loading: boolean } {
  const starts = Array.from({ length: 6 }, (_, i) => `${year}-${String(i * 2 + 1).padStart(2, "0")}-01`);
  const ranges = [
    useCalendarRange(calendarDayStart(starts[0]!, zone), calendarDayStart(starts[1]!, zone)),
    useCalendarRange(calendarDayStart(starts[1]!, zone), calendarDayStart(starts[2]!, zone)),
    useCalendarRange(calendarDayStart(starts[2]!, zone), calendarDayStart(starts[3]!, zone)),
    useCalendarRange(calendarDayStart(starts[3]!, zone), calendarDayStart(starts[4]!, zone)),
    useCalendarRange(calendarDayStart(starts[4]!, zone), calendarDayStart(starts[5]!, zone)),
    useCalendarRange(calendarDayStart(starts[5]!, zone), calendarDayStart(`${Number(year) + 1}-01-01`, zone)),
  ];
  const datas = ranges.map((r) => r.data);
  const marks = useMemo(() => {
    const m = new Map<string, number>();
    for (const d of datas) {
      for (const o of d?.items ?? []) {
        if (!keep(o)) continue;
        let day = o.all_day && o.start_date ? o.start_date : calendarDateKey(o.starts_at, zone);
        const last = o.all_day && o.end_date ? o.end_date : calendarDateKey(new Date(new Date(o.ends_at).getTime() - 1).toISOString(), zone);
        for (let i = 0; i < 31 && day <= last; i++) {
          m.set(day, (m.get(day) ?? 0) + 1);
          day = calendarAddDays(day, 1);
        }
      }
    }
    return m;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...datas, zone, keep]);
  return { marks, loading: ranges.some((r) => r.isPending) };
}

/** The year: twelve months, days with events marked (more events, stronger); a day opens the day view. */
export function YearView({ date, zone, weekStart, keep, onDay }: { date: string; zone: string; weekStart: number; keep: (o: CalendarOccurrence) => boolean; onDay: (d: string) => void }) {
  const year = date.slice(0, 4);
  const { marks, loading } = useYearMarks(year, zone, keep);
  const today = calendarTodayKey(zone);
  const names = calendarWeekdayNames(weekStart, "narrow");
  return (
    <div className={cn("min-h-0 flex-1 overflow-y-auto p-3 sm:p-4", loading && "opacity-80")} aria-busy={loading}>
      <div className="grid grid-cols-1 gap-6 min-[480px]:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {Array.from({ length: 12 }, (_, i) => {
          const month = `${year}-${String(i + 1).padStart(2, "0")}-01`;
          const grid = calendarMonthGrid(month, weekStart);
          return (
            <section key={month} aria-label={formatCalendarMonth(month)} className="flex flex-col gap-2">
              <h2 className="px-1 type-label font-semibold text-fg">{formatCalendarMonth(month)}</h2>
              <div className="grid grid-cols-7 gap-y-1 text-center">
                {names.map((n, k) => (
                  <span key={k} className="type-caption text-fg-subtle" aria-hidden>
                    {n}
                  </span>
                ))}
                {grid.map((d) => {
                  if (!calendarSameMonth(d, month)) return <span key={d} aria-hidden />;
                  const n = marks.get(d) ?? 0;
                  const isToday = d === today;
                  return (
                    <button
                      key={d}
                      type="button"
                      onClick={() => onDay(d)}
                      aria-label={`${d}${n ? `, ${n} ${n === 1 ? "event" : "events"}` : ""}`}
                      aria-current={isToday ? "date" : undefined}
                      className={cn(
                        "mx-auto flex size-8 items-center justify-center rounded-pill type-caption tabular-nums focus-visible:outline-2 focus-visible:outline-ring",
                        isToday ? "bg-primary text-primary-fg" : n >= 4 ? "bg-primary-soft font-semibold text-primary-strong" : n > 0 ? "bg-sunken font-semibold text-fg" : "text-fg-muted hover:bg-sunken",
                      )}
                    >
                      {Number(d.slice(8))}
                    </button>
                  );
                })}
              </div>
            </section>
          );
        })}
      </div>
    </div>
  );
}

/** First day of the year of dateKey. */
export const calendarStartOfYear = (dateKey: string) => calendarStartOfMonth(`${dateKey.slice(0, 4)}-01-01`);
