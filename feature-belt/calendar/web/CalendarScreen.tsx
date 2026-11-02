"use client";

import {
  calendarAddDays,
  calendarDateKey,
  calendarDayStart,
  calendarDraftGuests,
  calendarDraftTimes,
  calendarFromWall,
  calendarMonthGrid,
  calendarStartOfMonth,
  calendarStartOfWeek,
  calendarTodayKey,
  errorMessage,
  formatCalendarDayLong,
  formatCalendarMonth,
  calendarConnectedRef,
  isCalendarConnected,
  parseCalendarConnectedRef,
  parseCalendarNewEventLink,
  sourceProductLabel,
  useCalendarConnectedRange,
  useCalendarPeopleByIds,
  useCalendarRange,
  useCalendarSettings,
  useCalendarSources,
  useCalendarSubscriptions,
  useUpdateCalendarConnectedEvent,
  useUpdateCalendarEvent,
  type CalendarConnectedEventDetail,
  type CalendarConnectedOccurrence,
  type CalendarConnectedRef,
  type CalendarEventDetail,
  type CalendarOccurrence,
  type CalendarScope,
} from "@scaffoldapp/shared";
import { Button, ErrorState, IconButton, SegmentedControl, Select, Sheet, Skeleton, SourceFilter, cn, sourceFilterOptions, toast, useMediaQuery } from "@scaffoldapp/ui-web";
import { CalendarCheck, CalendarCog, ChevronLeft, ChevronRight, Link2, ListChecks, Plus, Rss } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { useUrlState } from "@/lib/url-state";
import { AgendaView } from "./AgendaView";
import { ConnectedCalendars } from "./ConnectedCalendars";
import { ConnectedEventPanel } from "./ConnectedEvent";
import { EventDetailPanel } from "./EventDetail";
import { EventDialog, ScopeDialog, type EventDialogTarget } from "./EventDialog";
import { MiniMonth, MonthView } from "./MonthView";
import { CALENDAR_COLOR_CLASSES, useCalendarZone } from "./parts";
import { TimeGrid } from "./TimeGrid";

type View = "day" | "week" | "month" | "agenda";
const AGENDA_DAYS = 30;

const isDateKey = (v: string) => /^\d{4}-\d{2}-\d{2}$/.test(v);

/** "November 2026", or "Oct – Nov 2026" / "Dec 2026 – Jan 2027" across months. */
function weekTitle(first: string, last: string): string {
  if (first.slice(0, 7) === last.slice(0, 7)) return formatCalendarMonth(first);
  const short = (d: string, year: boolean) => new Date(`${d}T12:00:00Z`).toLocaleDateString("en-US", { month: "short", ...(year ? { year: "numeric" } : {}), timeZone: "UTC" });
  return first.slice(0, 4) === last.slice(0, 4) ? `${short(first, false)} – ${short(last, true)}` : `${short(first, true)} – ${short(last, true)}`;
}

type OpenEvent = { kind: "app"; id: string; occurrence: string | null } | { kind: "connected"; ref: CalendarConnectedRef };

const openOf = (o: CalendarOccurrence): OpenEvent =>
  isCalendarConnected(o) ? { kind: "connected", ref: calendarConnectedRef(o) } : { kind: "app", id: o.event_id, occurrence: o.occurrence_start };

/**
 * Opens a connected event named by `?open=<connection>:<calendar>:<event>` (search results
 * link to the day with it) once, then drops the parameter.
 */
function useOpenParam(ready: boolean, open: (o: OpenEvent) => void) {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const raw = params.get("open");
  useEffect(() => {
    const ref = parseCalendarConnectedRef(raw);
    if (!raw || !ready) return;
    if (ref) open({ kind: "connected", ref });
    const sp = new URLSearchParams(window.location.search);
    sp.delete("open");
    router.replace(`${pathname}?${sp.toString()}`, { scroll: false });
  }, [raw, ready, open, router, pathname]);
}

/** Query parameters of a new-event link (`calendarNewEventLink`), dropped once it opens. */
const LINK_PARAMS = ["new", "title", "details", "start", "end", "duration", "video", "guests"];

/**
 * Opens the new-event dialog once for a new-event link (`/calendar?new=1&…`), after the
 * zone, settings and any guests by id have loaded, on the day it starts; then drops the
 * link's parameters so Back or a refresh doesn't open it again.
 */
function useNewEventLink(ready: boolean, zone: string, open: (t: EventDialogTarget) => void) {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const draft = useMemo(() => parseCalendarNewEventLink(Object.fromEntries(params.entries())), [params]);
  const { userIds, emails } = useMemo(() => (draft ? calendarDraftGuests(draft) : { userIds: [], emails: [] }), [draft]);
  const people = useCalendarPeopleByIds(userIds);
  const done = useRef(false);
  const waiting = userIds.length > 0 && people.isPending;
  useEffect(() => {
    if (!draft || done.current || !ready || waiting) return;
    done.current = true;
    const times = calendarDraftTimes(draft);
    open({
      mode: "new",
      ...times,
      ...(draft.title ? { title: draft.title } : {}),
      ...(draft.description ? { description: draft.description } : {}),
      people: people.data?.items ?? [],
      emails,
      video: !!draft.video,
    });
    const sp = new URLSearchParams(window.location.search);
    for (const k of LINK_PARAMS) sp.delete(k);
    sp.set("date", calendarDateKey(times.startsAt, zone));
    router.replace(`${pathname}?${sp.toString()}`, { scroll: false });
  }, [draft, ready, waiting, people.data, emails, zone, open, router, pathname]);
}

/** The calendar: day, week, month and agenda views; drag to create, move and resize. View and date live in the URL. */
export function CalendarScreen() {
  const router = useRouter();
  const { zone, weekStart, ready } = useCalendarZone();
  const settings = useCalendarSettings();
  const subs = useCalendarSubscriptions();
  const wide = useMediaQuery("(min-width: 1024px)");
  const phone = !useMediaQuery("(min-width: 640px)");
  const [viewParam, setView] = useUrlState<View | "">("view", "");
  const [dateParam, setDate] = useUrlState<string>("date", "");
  const view: View = viewParam || (phone ? "day" : "week");
  const today = calendarTodayKey(zone);
  const date = isDateKey(dateParam) ? dateParam : today;

  const days = useMemo(() => {
    if (view === "day") return [date];
    if (view === "week") {
      const s = calendarStartOfWeek(date, weekStart);
      return Array.from({ length: 7 }, (_, i) => calendarAddDays(s, i));
    }
    if (view === "month") return calendarMonthGrid(date, weekStart);
    return Array.from({ length: AGENDA_DAYS }, (_, i) => calendarAddDays(date, i));
  }, [view, date, weekStart]);
  const from = ready ? calendarDayStart(days[0]!, zone) : null;
  const to = ready ? calendarDayStart(calendarAddDays(days[days.length - 1]!, 1), zone) : null;
  const range = useCalendarRange(from, to);
  const sources = useCalendarSources();
  const connected = useCalendarConnectedRange(from, to);
  const [sourceFilter, setSourceFilter] = useState("all");

  const [open, setOpen] = useState<OpenEvent | null>(null);
  const [dialog, setDialog] = useState<EventDialogTarget | null>(null);
  const [pendingMove, setPendingMove] = useState<{ o: CalendarOccurrence; startsAt: string; endsAt: string } | null>(null);
  const [connectedOpen, setConnectedOpen] = useState(false);
  const update = useUpdateCalendarEvent();
  const updateConnected = useUpdateCalendarConnectedEvent();
  useOpenParam(ready && !settings.isPending, setOpen);

  const step = (dir: -1 | 1) => {
    if (view === "day") setDate(calendarAddDays(date, dir));
    else if (view === "week") setDate(calendarAddDays(date, dir * 7));
    else if (view === "month") setDate(calendarStartOfMonth(date, dir));
    else setDate(calendarAddDays(date, dir * AGENDA_DAYS));
  };

  const title =
    view === "day"
      ? formatCalendarDayLong(date)
      : view === "week"
        ? weekTitle(days[0]!, days[6]!)
        : view === "month"
          ? formatCalendarMonth(date)
          : `From ${formatCalendarDayLong(date)}`;

  const newAt = (startsAt?: string, endsAt?: string) => {
    if (startsAt && endsAt) {
      setDialog({ mode: "new", startsAt, endsAt });
      return;
    }
    // Next half hour on the shown day (today when it's in view).
    const base = days.includes(today) ? today : days[0]!;
    const now = new Date();
    const mins = base === today ? Math.ceil((now.getHours() * 60 + now.getMinutes() + 1) / 30) * 30 : 9 * 60;
    const s = calendarFromWall(base, Math.min(mins, 23 * 60), zone);
    setDialog({ mode: "new", startsAt: s, endsAt: new Date(new Date(s).getTime() + 30 * 60_000).toISOString() });
  };

  const move = (o: CalendarOccurrence, startsAt: string, endsAt: string) => {
    if (o.recurring) setPendingMove({ o, startsAt, endsAt });
    else void commitMove(o, startsAt, endsAt, null);
  };

  // A connected event moves on its platform (one occurrence, or the whole series).
  const commitConnectedMove = async (o: CalendarConnectedOccurrence, startsAt: string, endsAt: string, scope: CalendarScope | null) => {
    const product = sourceProductLabel(o.source.provider, o.source.product);
    try {
      await updateConnected.mutateAsync({
        ref: calendarConnectedRef(o),
        body: { starts_at: startsAt, ends_at: endsAt, all_day: false, timezone: o.timezone || zone, ...(scope === "all" ? { scope: "all" as const } : {}) },
      });
      toast.success(o.attendee_count > 0 ? `Moved in ${product}. It tells the guests.` : `Moved in ${product}`);
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't move the event."));
    }
  };
  useNewEventLink(ready && !settings.isPending, zone, setDialog);

  const commitMove = async (o: CalendarOccurrence, startsAt: string, endsAt: string, scope: CalendarScope | null) => {
    if (isCalendarConnected(o)) return commitConnectedMove(o, startsAt, endsAt, scope);
    try {
      await update.mutateAsync({
        id: o.event_id,
        body: { starts_at: startsAt, ends_at: endsAt, all_day: false, timezone: o.timezone, occurrence_start: o.occurrence_start, ...(scope ? { scope } : {}) },
      });
      toast.success(o.attendee_count > 0 ? "Moved. Guests were told." : "Moved");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't move the event."));
    }
  };

  const onEdit = (e: CalendarEventDetail) => {
    setOpen(null);
    setDialog({ mode: "edit", event: e });
  };
  const onEditConnected = (e: CalendarConnectedEventDetail) => {
    setOpen(null);
    setDialog({ mode: "edit-connected", event: e });
  };

  // The app's own events and the visible connected calendars' (hidden ones drop at once).
  const hidden = useMemo(() => {
    const out = new Set<string>();
    for (const src of sources.data?.sources ?? []) for (const c of src.calendars) if (c.hidden) out.add(`${src.id}|${c.id}`);
    return out;
  }, [sources.data]);
  const connectedItems = useMemo(
    () => (connected.data?.items ?? []).filter((o) => !hidden.has(`${o.connection_id}|${o.calendar_id}`)),
    [connected.data, hidden],
  );
  const allItems = useMemo(() => [...(range.data?.items ?? []), ...connectedItems], [range.data, connectedItems]);
  const filterOptions = useMemo(
    () => sourceFilterOptions({ appLabel: "Calendar", sources: (sources.data?.sources ?? []).filter((x) => x.type === "app" || x.status === "active") }),
    [sources.data],
  );
  const items =
    sourceFilter === "all" ? allItems : sourceFilter === "app" ? (range.data?.items ?? []) : connectedItems.filter((o) => o.connection_id === sourceFilter);
  const connectedErrors = connected.data?.errors ?? [];
  const busy = range.data?.busy ?? [];
  const workingHours = settings.data?.working_hours ?? [];

  const headerMenu = [
    ...(!wide ? [{ id: "connected", label: "Connected calendars", icon: CalendarCheck, onSelect: () => setConnectedOpen(true) }] : []),
    { id: "links", label: "Booking links", icon: Link2, onSelect: () => router.push("/calendar/links") },
    { id: "bookings", label: "Bookings", icon: ListChecks, onSelect: () => router.push("/calendar/bookings") },
    { id: "settings", label: "Calendar settings", icon: CalendarCog, onSelect: () => router.push("/settings/calendars") },
  ];

  const body = !ready ? (
    <Skeleton className="m-4 flex-1" />
  ) : range.isError && !range.data ? (
    <ErrorState title="Couldn't load your calendar" info={range.error.message} onRetry={() => void range.refetch()} retrying={range.isFetching} />
  ) : view === "month" ? (
    <MonthView
      date={date}
      zone={zone}
      weekStart={weekStart}
      items={items}
      onOpen={(o) => setOpen(openOf(o))}
      onDay={(d) => {
        setDate(d);
        setView("day");
      }}
      onCreate={(d) => newAt(calendarFromWall(d, 9 * 60, zone), calendarFromWall(d, 9 * 60 + 30, zone))}
    />
  ) : view === "agenda" ? (
    <AgendaView
      date={date}
      days={AGENDA_DAYS}
      zone={zone}
      items={items}
      onOpen={(o) => setOpen(openOf(o))}
      header={<SourceFilter options={filterOptions} value={sourceFilter} onChange={setSourceFilter} label="Show events from" />}
    />
  ) : (
    <TimeGrid
      days={days}
      zone={zone}
      items={items}
      busy={busy}
      workingHours={workingHours}
      onCreate={(s, e) => newAt(s, e)}
      onCreateAllDay={(d) => setDialog({ mode: "new", startsAt: calendarDayStart(d, zone), endsAt: calendarDayStart(calendarAddDays(d, 1), zone), allDay: true })}
      onOpen={(o) => setOpen(openOf(o))}
      onMove={move}
    />
  );

  return (
    <div
      className="mx-auto flex min-h-0 w-full max-w-content gap-6 px-gutter py-4"
      // Fill the viewport under the top bar (and above the phone tab bar) so the grid scrolls, not the page.
      style={{ height: wide ? "calc(100dvh - var(--spacing-topbar))" : "calc(100dvh - var(--spacing-topbar) - 4rem - env(safe-area-inset-bottom))", minHeight: 520 }}
    >
      {wide ? (
        <aside className="flex w-64 shrink-0 flex-col gap-6 overflow-y-auto" aria-label="Calendar navigation">
          <Button leftIcon={<Plus />} onClick={() => newAt()}>
            New event
          </Button>
          <MiniMonth
            value={date}
            zone={zone}
            weekStart={weekStart}
            onPick={(d) => setDate(d)}
            marked={(d) => items.some((o) => o.start_date === d || o.starts_at.startsWith(d))}
          />
          <ConnectedCalendars sources={sources.data} loading={sources.isPending} />
          {(subs.data?.items.length ?? 0) > 0 ? (
            <section className="flex flex-col gap-2" aria-label="Other calendars">
              <h2 className="type-label font-semibold text-fg-muted">Other calendars</h2>
              <ul className="flex flex-col gap-1">
                {subs.data?.items.map((s) => (
                  <li key={s.id} className="flex min-h-control items-center gap-2 type-label text-fg">
                    <span className={cn("size-3 rounded-sm border border-dashed", CALENDAR_COLOR_CLASSES[s.color].chip)} aria-hidden />
                    <span className={cn("truncate", !s.enabled && "text-fg-subtle line-through")}>{s.name}</span>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
          <nav className="flex flex-col gap-1" aria-label="Scheduling">
            <Link href="/calendar/links" className="flex min-h-control items-center gap-2 rounded-md px-2 type-label text-fg hover:bg-sunken">
              <Link2 className="size-4 text-fg-muted" aria-hidden />
              Booking links
            </Link>
            <Link href="/calendar/bookings" className="flex min-h-control items-center gap-2 rounded-md px-2 type-label text-fg hover:bg-sunken">
              <ListChecks className="size-4 text-fg-muted" aria-hidden />
              Bookings
            </Link>
            <Link href="/settings/calendars" className="flex min-h-control items-center gap-2 rounded-md px-2 type-label text-fg hover:bg-sunken">
              <Rss className="size-4 text-fg-muted" aria-hidden />
              Other calendars and feed
            </Link>
          </nav>
        </aside>
      ) : null}

      <section className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-border bg-surface" aria-label="Calendar">
        <header className="flex flex-col gap-2 border-b border-border p-2 sm:flex-row sm:items-center sm:p-3">
          <div className="flex min-w-0 items-center gap-2 sm:order-2 sm:flex-1">
            <h1 className="min-w-0 flex-1 truncate type-title-sm text-fg" aria-live="polite">
              {title}
            </h1>
            {phone ? (
              <>
                <IconButton label="New event" icon={<Plus />} size="sm" variant="primary" onClick={() => newAt()} />
                <ResourceMenu feature="calendar" title="Calendar" actions={headerMenu} />
              </>
            ) : null}
          </div>
          <div className="flex items-center gap-2 sm:order-1">
            <Button variant="secondary" size="sm" onClick={() => setDate(null)} disabled={date === today && view !== "agenda"}>
              Today
            </Button>
            <IconButton label="Previous" icon={<ChevronLeft />} size="sm" variant="ghost" onClick={() => step(-1)} />
            <IconButton label="Next" icon={<ChevronRight />} size="sm" variant="ghost" onClick={() => step(1)} />
            {phone ? (
              <Select
                value={view}
                onValueChange={(v) => setView(v as View)}
                options={[
                  { value: "day", label: "Day" },
                  { value: "week", label: "Week" },
                  { value: "month", label: "Month" },
                  { value: "agenda", label: "Agenda" },
                ]}
                className="ml-auto w-32"
              />
            ) : null}
          </div>
          {!phone ? (
            <div className="flex items-center gap-2 sm:order-3">
              <SegmentedControl<View>
                fullWidth={false}
                aria-label="View"
                value={view}
                onValueChange={(v) => setView(v)}
                options={[
                  { value: "day", label: "Day" },
                  { value: "week", label: "Week" },
                  { value: "month", label: "Month" },
                  { value: "agenda", label: "Agenda" },
                ]}
              />
              {!wide ? <IconButton label="New event" icon={<Plus />} size="sm" variant="primary" onClick={() => newAt()} /> : null}
              <ResourceMenu feature="calendar" title="Calendar" actions={headerMenu} />
            </div>
          ) : null}
        </header>
        {range.data?.truncated ? <p className="border-b border-border px-3 py-2 type-caption text-fg-muted">Showing the first 2000 events in this range.</p> : null}
        {connectedErrors.length > 0 ? (
          <div role="status" className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-warning-soft px-3 py-2 type-caption text-warning-strong">
            <span className="min-w-0 flex-1">
              {(() => {
                const src = sources.data?.sources.find((x) => x.id === connectedErrors[0]!.source_id);
                const name = src ? `${sourceProductLabel(src.provider ?? "", src.product)} · ${src.account}` : "A connected calendar";
                return `${name}: ${connectedErrors[0]!.message}`;
              })()}
            </span>
            {connectedErrors[0]!.fix_url ? (
              <Link href={connectedErrors[0]!.fix_url} className="font-semibold underline underline-offset-2">
                Reconnect
              </Link>
            ) : null}
          </div>
        ) : null}
        <div className={cn("flex min-h-0 flex-1 flex-col", range.isFetching && range.isPlaceholderData && "opacity-70 transition-opacity motion-reduce:transition-none")}>{body}</div>
      </section>

      <Sheet open={!!open} onOpenChange={(o) => !o && setOpen(null)} title="Event" size="md">
        {open?.kind === "app" ? <EventDetailPanel id={open.id} occurrence={open.occurrence} onEdit={onEdit} onDeleted={() => setOpen(null)} /> : null}
        {open?.kind === "connected" ? <ConnectedEventPanel eventRef={open.ref} onEdit={onEditConnected} onDeleted={() => setOpen(null)} /> : null}
      </Sheet>
      <Sheet open={connectedOpen} onOpenChange={setConnectedOpen} title="Connected calendars" size="sm">
        <ConnectedCalendars sources={sources.data} loading={sources.isPending} />
      </Sheet>
      {dialog ? <EventDialog target={dialog} zone={zone} onClose={() => setDialog(null)} /> : null}
      <ScopeDialog
        key={pendingMove ? `${pendingMove.o.event_id}${isCalendarConnected(pendingMove.o)}` : "none"}
        open={!!pendingMove}
        onOpenChange={(o) => !o && setPendingMove(null)}
        title="Move recurring event"
        confirmLabel="Move"
        allowFollowing={!pendingMove || !isCalendarConnected(pendingMove.o)}
        onConfirm={async (scope) => {
          if (pendingMove) await commitMove(pendingMove.o, pendingMove.startsAt, pendingMove.endsAt, scope);
          setPendingMove(null);
        }}
      />
    </div>
  );
}
