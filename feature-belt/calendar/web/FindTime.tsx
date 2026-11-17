"use client";

import {
  calendarAddDays,
  calendarDateKey,
  calendarDayStart,
  calendarFromWall,
  calendarMinutesOfDay,
  calendarWeekday,
  calendarZoneLabel,
  errorMessage,
  formatCalendarDay,
  formatCalendarTime,
  useCalendarFindTime,
  useCalendarFreeBusy,
  useMe,
  type CalendarFreeBusy,
  type CalendarHoursInterval,
  type CalendarPerson,
} from "@scaffoldapp/shared";
import { Chip, IconButton, cn } from "@scaffoldapp/ui-web";
import { Building2, ChevronLeft, ChevronRight, Home, MapPin, Plus } from "lucide-react";
import { useMemo } from "react";

const LO = 7 * 60;
const HI = 20 * 60;
const pct = (m: number) => `${((Math.min(HI, Math.max(LO, m)) - LO) / (HI - LO)) * 100}%`;
const span = (a: number, b: number) => ({ left: pct(a), width: `calc(${pct(b)} - ${pct(a)})` });

/** A person's working windows on the viewer's date, in the viewer's minutes (0–1440). */
export function calendarHoursInViewer(hours: CalendarHoursInterval[], theirZone: string, date: string, viewerZone: string): [number, number][] {
  const out: [number, number][] = [];
  for (const offset of [-1, 0, 1]) {
    const theirDate = calendarAddDays(date, offset);
    const weekday = calendarWeekday(theirDate);
    for (const h of hours.filter((x) => x.day === weekday)) {
      const s = calendarFromWall(theirDate, h.start, theirZone);
      const e = calendarFromWall(theirDate, h.end, theirZone);
      const sm = calendarDateKey(s, viewerZone) < date ? 0 : calendarDateKey(s, viewerZone) > date ? 1440 : calendarMinutesOfDay(s, viewerZone);
      const em = calendarDateKey(e, viewerZone) > date ? 1440 : calendarDateKey(e, viewerZone) < date ? 0 : calendarMinutesOfDay(e, viewerZone);
      if (em > sm) out.push([sm, em]);
    }
  }
  return out;
}

const LOCATION_ICON = { office: Building2, home: Home, other: MapPin } as const;

/**
 * Find a time: you and each colleague on one timeline (your zone, 7 AM–8 PM) with their busy
 * time, time outside their working hours shaded, out of office and focus time marked, their
 * zone and where they work that day; and suggested times inside everyone's working hours.
 */
export function FindTime({
  people,
  date,
  zone,
  startMin,
  endMin,
  onPick,
  onDate,
}: {
  people: CalendarPerson[];
  date: string;
  zone: string;
  startMin: number;
  endMin: number;
  onPick: (s: string, e: string) => void;
  onDate: (d: string) => void;
}) {
  const me = useMe();
  const from = calendarDayStart(date, zone);
  const to = calendarDayStart(calendarAddDays(date, 1), zone);
  const ids = useMemo(() => [...(me.data?.id ? [me.data.id] : []), ...people.map((p) => p.id)], [me.data?.id, people]);
  const fb = useCalendarFreeBusy(ids, from, to);
  const duration = Math.max(15, endMin - startMin);
  // Rounded to the quarter hour so the query key stays stable between renders.
  const findFrom = useMemo(() => new Date(Math.max(Math.ceil(Date.now() / 900_000) * 900_000, new Date(from).getTime())).toISOString(), [from]);
  const suggestions = useCalendarFindTime({ user_ids: people.map((p) => p.id), from: findFrom, to: calendarDayStart(calendarAddDays(date, 7), zone), duration });
  const rows = fb.data?.items ?? [];
  const selectedAt = calendarFromWall(date, startMin, zone);

  return (
    <div className="flex flex-col gap-3 rounded-md border border-border p-3" aria-label="Find a time">
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 type-label font-semibold text-fg">Find a time</span>
        <IconButton label="Previous day" icon={<ChevronLeft />} size="sm" variant="ghost" onClick={() => onDate(calendarAddDays(date, -1))} />
        <span className="type-caption text-fg tabular-nums">{formatCalendarDay(date)}</span>
        <IconButton label="Next day" icon={<ChevronRight />} size="sm" variant="ghost" onClick={() => onDate(calendarAddDays(date, 1))} />
      </div>
      <div className="flex flex-col gap-2">
        {fb.isPending ? (
          <div className="flex flex-col gap-2" aria-busy>
            {ids.map((id) => (
              <div key={id} className="h-10 animate-pulse rounded-sm bg-sunken motion-reduce:animate-none" />
            ))}
          </div>
        ) : fb.isError ? (
          <p className="type-caption text-fg-muted">{errorMessage(fb.error, "Couldn't load availability.")}</p>
        ) : (
          rows.map((r) => <PersonRow key={r.user.id} row={r} self={r.user.id === me.data?.id} date={date} zone={zone} startMin={startMin} endMin={endMin} selectedAt={selectedAt} />)
        )}
        <div className="flex items-center gap-3 type-caption text-fg-subtle" aria-hidden>
          <span className="w-32 shrink-0" />
          <span className="flex flex-1 justify-between">
            <span>7 AM</span>
            <span>1 PM</span>
            <span>8 PM</span>
          </span>
        </div>
      </div>
      {suggestions.isPending ? null : suggestions.data && suggestions.data.slots.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="type-caption text-fg-muted">Everyone's free</span>
          {suggestions.data.slots.slice(0, 6).map((s) => (
            <Chip key={s.starts_at} size="sm" icon={<Plus />} onClick={() => onPick(s.starts_at, s.ends_at)}>
              {formatCalendarDay(calendarDateKey(s.starts_at, zone))} {formatCalendarTime(s.starts_at, zone)}
            </Chip>
          ))}
        </div>
      ) : (
        <p className="type-caption text-fg-muted">No time this week when everyone's free in their working hours.</p>
      )}
    </div>
  );
}

function PersonRow({ row: r, self, date, zone, startMin, endMin, selectedAt }: { row: CalendarFreeBusy; self: boolean; date: string; zone: string; startMin: number; endMin: number; selectedAt: string }) {
  const theirZone = r.timezone ?? zone;
  const work = useMemo(() => calendarHoursInViewer(r.working_hours, theirZone, date, zone), [r.working_hours, theirZone, date, zone]);
  const theirDate = calendarDateKey(selectedAt, theirZone);
  const loc = r.work_locations.find((l) => l.day === calendarWeekday(theirDate));
  const LocIcon = loc ? LOCATION_ICON[loc.type] : null;
  const locLabel = loc ? (loc.label || { office: "Office", home: "Home", other: "Elsewhere" }[loc.type]) : "";
  const otherZone = theirZone !== zone;
  // Off hours: the gaps between working windows across the visible day.
  const off: [number, number][] = [];
  let cursor = 0;
  for (const [a, b] of [...work].sort((x, y) => x[0] - y[0])) {
    if (a > cursor) off.push([cursor, a]);
    cursor = Math.max(cursor, b);
  }
  if (cursor < 1440) off.push([cursor, 1440]);
  return (
    <div className="flex items-center gap-3">
      <span className="flex w-32 shrink-0 flex-col">
        <span className="truncate type-caption font-semibold text-fg">{self ? "You" : r.user.display_name}</span>
        <span className="flex items-center gap-1 truncate type-caption text-fg-muted">
          {LocIcon ? <LocIcon className="size-3 shrink-0" aria-hidden /> : null}
          <span className="truncate">
            {otherZone ? `${formatCalendarTime(selectedAt, theirZone)} ${calendarZoneLabel(selectedAt, theirZone)}` : ""}
            {otherZone && locLabel ? " · " : ""}
            {locLabel}
          </span>
        </span>
      </span>
      <div className="relative h-7 flex-1 overflow-hidden rounded-sm bg-surface ring-1 ring-border" role="img" aria-label={`${self ? "Your" : `${r.user.display_name}'s`} day: ${r.busy.length} busy ${r.busy.length === 1 ? "block" : "blocks"}`}>
        {off.map(([a, b], i) => (
          <div key={`off-${i}`} className="absolute inset-y-0 bg-sunken" style={span(a, b)} aria-hidden />
        ))}
        {r.busy.map((b, i) => {
          const s = calendarDateKey(b.starts_at, zone) < date ? 0 : calendarMinutesOfDay(b.starts_at, zone);
          const e = calendarDateKey(b.ends_at, zone) > date ? 1440 : calendarMinutesOfDay(b.ends_at, zone);
          return (
            <div
              key={i}
              title={b.kind === "out_of_office" ? (b.title ?? "Out of office") : b.kind === "focus" ? (b.title ?? "Focus time") : (b.title ?? "Busy")}
              className={cn(
                "absolute inset-y-1 rounded-xs",
                b.kind === "out_of_office" ? "bg-warning" : b.kind === "focus" ? "bg-info" : "bg-fg-muted",
              )}
              style={span(s, e)}
            />
          );
        })}
        <div className="absolute inset-y-0 rounded-xs border-2 border-primary" style={span(startMin, endMin)} aria-hidden />
      </div>
    </div>
  );
}
