"use client";

import {
  calendarAddDays,
  calendarOccurrenceKey,
  calendarOnDay,
  calendarTodayKey,
  formatCalendarDayLong,
  formatCalendarTime,
  type CalendarOccurrence,
} from "@scaffoldapp/shared";
import { Badge, SourceBadge, cn } from "@scaffoldapp/ui-web";
import { Repeat, Video } from "lucide-react";
import type { ReactNode } from "react";
import { ColorDot, responseLabel } from "./parts";

/** Upcoming days with events, grouped by day, from `date` for `days` days. */
export function AgendaView({ date, days, zone, items, onOpen, header }: { date: string; days: number; zone: string; items: CalendarOccurrence[]; onOpen: (o: CalendarOccurrence) => void; header?: ReactNode }) {
  const today = calendarTodayKey(zone);
  const groups = Array.from({ length: days }, (_, i) => calendarAddDays(date, i))
    .map((d) => ({ d, list: calendarOnDay(items, d, zone).sort((a, b) => Number(b.all_day) - Number(a.all_day) || a.starts_at.localeCompare(b.starts_at)) }))
    .filter((g) => g.list.length > 0);
  if (groups.length === 0) {
    return (
      <div className="flex flex-col">
        {header ? <div className="px-gutter pt-3">{header}</div> : null}
        <p className="px-gutter py-section text-center type-body text-fg-muted">Nothing scheduled in the next {days} days.</p>
      </div>
    );
  }
  return (
    <ol className="flex flex-col gap-6 overflow-y-auto px-gutter py-4">
      {header ? <li>{header}</li> : null}
      {groups.map((g) => (
        <li key={g.d} className="flex flex-col gap-2">
          <h3 className={cn("type-label font-semibold", g.d === today ? "text-primary-strong" : "text-fg-muted")}>
            {g.d === today ? "Today · " : ""}
            {formatCalendarDayLong(g.d)}
          </h3>
          <ul className="flex flex-col">
            {g.list.map((o) => (
              <li key={calendarOccurrenceKey(o)}>
                <button
                  type="button"
                  onClick={() => onOpen(o)}
                  className="flex min-h-tap w-full items-center gap-3 rounded-md px-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                >
                  <span className="w-24 shrink-0 type-label text-fg-muted tabular-nums">{o.all_day ? "All day" : formatCalendarTime(o.starts_at, zone)}</span>
                  <ColorDot color={o.color} />
                  <span className={cn("min-w-0 flex-1 truncate type-body text-fg", o.my_response === "declined" && "text-fg-muted line-through")}>{o.title}</span>
                  <SourceBadge source={o.source} className="hidden sm:inline-flex" />
                  {o.recurring ? <Repeat className="size-4 shrink-0 text-fg-subtle" aria-label="Repeats" /> : null}
                  {o.conference_url ? <Video className="size-4 shrink-0 text-fg-subtle" aria-label="Video call" /> : null}
                  {o.my_response === "needs_action" || o.my_response === "tentative" ? <Badge tone="neutral">{responseLabel(o.my_response)}</Badge> : null}
                </button>
              </li>
            ))}
          </ul>
        </li>
      ))}
    </ol>
  );
}
