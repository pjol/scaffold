"use client";

import { calendarAddDays, calendarTodayKey, formatCalendarDay, type CalendarDateOverride, type CalendarHoursInterval } from "@scaffoldapp/shared";
import { Button, Checkbox, IconButton, Input } from "@scaffoldapp/ui-web";
import { Plus, Trash2, X } from "lucide-react";

const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const pad = (n: number) => String(n).padStart(2, "0");
const toTime = (m: number) => (m >= 1440 ? "23:59" : `${pad(Math.floor(m / 60))}:${pad(m % 60)}`);
const fromTime = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  const v = (h ?? 0) * 60 + (m ?? 0);
  return v === 23 * 60 + 59 ? 1440 : v;
};

/** Weekly availability: each day on or off, with one or more time windows. */
export function HoursEditor({ value, onChange, weekStart = 1 }: { value: CalendarHoursInterval[]; onChange: (v: CalendarHoursInterval[]) => void; weekStart?: number }) {
  const order = Array.from({ length: 7 }, (_, i) => (weekStart + i) % 7);
  const setDay = (day: number, list: CalendarHoursInterval[]) => onChange([...value.filter((h) => h.day !== day), ...list].sort((a, b) => a.day - b.day || a.start - b.start));
  return (
    <div className="flex flex-col gap-2">
      {order.map((day) => {
        const list = value.filter((h) => h.day === day).sort((a, b) => a.start - b.start);
        const on = list.length > 0;
        return (
          <div key={day} className="flex flex-col gap-2 border-b border-border py-2 last:border-b-0 sm:flex-row sm:items-start">
            <div className="w-36 shrink-0 pt-2">
              <Checkbox checked={on} onCheckedChange={(v) => setDay(day, v ? [{ day, start: 540, end: 1020 }] : [])} label={DAYS[day]} />
            </div>
            {on ? (
              <div className="flex flex-1 flex-col gap-2">
                {list.map((h, i) => (
                  <div key={i} className="flex items-center gap-2">
                    <Input type="time" step={900} value={toTime(h.start)} aria-label={`${DAYS[day]} from`} onChange={(e) => e.target.value && setDay(day, list.map((x, j) => (j === i ? { ...x, start: fromTime(e.target.value) } : x)))} className="w-32" />
                    <span className="type-label text-fg-muted">to</span>
                    <Input type="time" step={900} value={toTime(h.end)} aria-label={`${DAYS[day]} until`} onChange={(e) => e.target.value && setDay(day, list.map((x, j) => (j === i ? { ...x, end: fromTime(e.target.value) } : x)))} className="w-32" />
                    <IconButton label={`Remove window on ${DAYS[day]}`} icon={<X />} size="sm" variant="ghost" onClick={() => setDay(day, list.filter((_, j) => j !== i))} />
                  </div>
                ))}
                {list.length < 4 ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    leftIcon={<Plus />}
                    className="self-start"
                    onClick={() => {
                      const last = list[list.length - 1]!;
                      const start = Math.min(last.end + 60, 1380);
                      setDay(day, [...list, { day, start, end: Math.min(start + 120, 1440) }]);
                    }}
                  >
                    Add hours
                  </Button>
                ) : null}
              </div>
            ) : (
              <span className="pt-2 type-label text-fg-muted">Unavailable</span>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** Dates that replace the weekly hours (a day off, or different hours). */
export function OverridesEditor({ value, onChange, zone }: { value: CalendarDateOverride[]; onChange: (v: CalendarDateOverride[]) => void; zone: string }) {
  const sorted = [...value].sort((a, b) => a.date.localeCompare(b.date));
  const add = () => {
    let d = calendarAddDays(calendarTodayKey(zone), 1);
    while (value.some((o) => o.date === d)) d = calendarAddDays(d, 1);
    onChange([...value, { date: d, intervals: [] }]);
  };
  return (
    <div className="flex flex-col gap-3">
      {sorted.length === 0 ? <p className="type-label text-fg-muted">No date overrides.</p> : null}
      {sorted.map((o) => {
        const update = (next: CalendarDateOverride) => onChange(value.map((x) => (x.date === o.date ? next : x)));
        return (
          <div key={o.date} className="flex flex-col gap-2 rounded-md bg-sunken p-3">
            <div className="flex items-center gap-2">
              <Input type="date" value={o.date} aria-label="Date" onChange={(e) => e.target.value && !value.some((x) => x.date === e.target.value) && update({ ...o, date: e.target.value })} className="w-44" />
              <span className="flex-1 truncate type-label text-fg-muted">{formatCalendarDay(o.date)}</span>
              <IconButton label={`Remove override for ${o.date}`} icon={<Trash2 />} size="sm" variant="ghost" onClick={() => onChange(value.filter((x) => x.date !== o.date))} />
            </div>
            <Checkbox
              checked={o.intervals.length === 0}
              onCheckedChange={(v) => update({ ...o, intervals: v ? [] : [{ start: 540, end: 1020 }] })}
              label="Unavailable all day"
            />
            {o.intervals.map((iv, i) => (
              <div key={i} className="flex items-center gap-2">
                <Input type="time" step={900} value={toTime(iv.start)} aria-label="From" onChange={(e) => e.target.value && update({ ...o, intervals: o.intervals.map((x, j) => (j === i ? { ...x, start: fromTime(e.target.value) } : x)) })} className="w-32" />
                <span className="type-label text-fg-muted">to</span>
                <Input type="time" step={900} value={toTime(iv.end)} aria-label="Until" onChange={(e) => e.target.value && update({ ...o, intervals: o.intervals.map((x, j) => (j === i ? { ...x, end: fromTime(e.target.value) } : x)) })} className="w-32" />
              </div>
            ))}
          </div>
        );
      })}
      <Button variant="secondary" size="sm" leftIcon={<Plus />} className="self-start" onClick={add}>
        Add a date
      </Button>
    </div>
  );
}
