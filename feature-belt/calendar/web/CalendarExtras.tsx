"use client";

import { calendarZoneLabel, errorMessage, formatCalendarRange, useCalendarSearch, useDebouncedValue, useNow, type CalendarSearchHit } from "@scaffoldapp/shared";
import { Dialog, EmptyState, Input, Skeleton } from "@scaffoldapp/ui-web";
import { CalendarSearch, Repeat } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";

type View = "day" | "3day" | "week" | "month" | "year" | "agenda";

/** Keyboard shortcuts, Google Calendar's: c, t, j/k (n/p), d x w m y a, /, e, Delete, ?. */
export const CALENDAR_SHORTCUTS: { keys: string; label: string }[] = [
  { keys: "c", label: "New event" },
  { keys: "t", label: "Today" },
  { keys: "j or n", label: "Next period" },
  { keys: "k or p", label: "Previous period" },
  { keys: "d · x · w · m · y · a", label: "Day, 3 days, week, month, year, agenda" },
  { keys: "/", label: "Search events" },
  { keys: "e", label: "Edit the open event" },
  { keys: "Delete", label: "Delete the open event" },
  { keys: "?", label: "These shortcuts" },
];

const VIEW_KEYS: Record<string, View> = { d: "day", x: "3day", w: "week", m: "month", y: "year", a: "agenda" };

/** Listens for the calendar's shortcuts while nothing is being typed and no dialog is open. */
export function useCalendarShortcuts(h: {
  enabled: boolean;
  onCreate: () => void;
  onToday: () => void;
  onStep: (dir: -1 | 1) => void;
  onView: (v: View) => void;
  onSearch: () => void;
  onHelp: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const ref = useRef(h);
  ref.current = h;
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const c = ref.current;
      if (!c.enabled || e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName))) return;
      // Inside a dialog only the event sheet takes shortcuts (e, Delete…).
      const dialog = t?.closest("[role=dialog]");
      if (dialog && !dialog.querySelector("[data-calendar-event]")) return;
      const k = e.key;
      const act = (fn: () => void) => {
        e.preventDefault();
        fn();
      };
      if (k === "c") act(c.onCreate);
      else if (k === "t") act(c.onToday);
      else if (k === "j" || k === "n") act(() => c.onStep(1));
      else if (k === "k" || k === "p") act(() => c.onStep(-1));
      else if (VIEW_KEYS[k]) act(() => c.onView(VIEW_KEYS[k]!));
      else if (k === "/") act(c.onSearch);
      else if (k === "?") act(c.onHelp);
      else if (k === "e") act(c.onEdit);
      else if (k === "Delete" || k === "Backspace") act(c.onDelete);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

/** `?add_calendar=<id>` (a share notification's link) adds that calendar to your list once, then drops the parameter. */
export function useAddCalendarParam(add: (id: string) => void) {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const id = params.get("add_calendar");
  const done = useRef<string | null>(null);
  useEffect(() => {
    if (!id || done.current === id) return;
    done.current = id;
    if (/^[0-9a-f-]{36}$/i.test(id)) add(id);
    const sp = new URLSearchParams(window.location.search);
    sp.delete("add_calendar");
    router.replace(`${pathname}${sp.size ? `?${sp.toString()}` : ""}`, { scroll: false });
  }, [id, add, router, pathname]);
}

export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Keyboard shortcuts" size="sm">
      <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-3">
        {CALENDAR_SHORTCUTS.map((s) => (
          <div key={s.keys} className="contents">
            <dt>
              <kbd className="rounded-xs border border-border bg-sunken px-1.5 py-0.5 font-mono type-caption text-fg">{s.keys}</kbd>
            </dt>
            <dd className="type-body text-fg">{s.label}</dd>
          </div>
        ))}
      </dl>
    </Dialog>
  );
}

/** Search your events (and calendars shared with you with details); pick one to open it on its day. */
export function EventSearchDialog({ open, onOpenChange, zone, onPick }: { open: boolean; onOpenChange: (o: boolean) => void; zone: string; onPick: (h: CalendarSearchHit) => void }) {
  const [q, setQ] = useState("");
  const term = useDebouncedValue(q, 250);
  const results = useCalendarSearch(open ? term : "");
  const items = results.data?.items ?? [];
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Search events" size="md">
      <div className="flex flex-col gap-3">
        <Input autoFocus value={q} onChange={(e) => setQ(e.target.value)} placeholder="Title, place or description" aria-label="Search events" />
        {term.trim().length < 2 ? (
          <p className="type-caption text-fg-muted">Type at least two letters.</p>
        ) : results.isPending ? (
          <div className="flex flex-col gap-2" aria-busy>
            <Skeleton className="h-12" />
            <Skeleton className="h-12" />
          </div>
        ) : results.isError ? (
          <p className="type-caption text-danger-strong">{errorMessage(results.error, "Couldn't search. Try again.")}</p>
        ) : items.length === 0 ? (
          <EmptyState compact icon={<CalendarSearch />} title={`No events match “${term.trim()}”`} />
        ) : (
          <ul className="flex flex-col divide-y divide-border" aria-label="Results">
            {items.map((h) => (
              <li key={`${h.event_id}${h.occurrence_start ?? ""}`}>
                <button type="button" onClick={() => onPick(h)} className="flex min-h-12 w-full flex-col items-start gap-0.5 rounded-md px-2 py-2 text-left hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                  <span className="flex w-full items-center gap-2 type-body text-fg">
                    <span className="min-w-0 flex-1 truncate">{h.title}</span>
                    {h.recurring ? <Repeat className="size-3.5 shrink-0 text-fg-muted" aria-label="Repeats" /> : null}
                  </span>
                  <span className="truncate type-caption text-fg-muted">
                    {formatCalendarRange({ starts_at: h.starts_at, ends_at: h.ends_at, all_day: h.all_day, start_date: h.all_day ? h.starts_at.slice(0, 10) : null, end_date: null }, h.all_day ? h.timezone : zone)}
                    {h.location ? ` · ${h.location}` : ""}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  );
}

/** The time now in each of the person's world clocks. */
export function WorldClocks({ zones }: { zones: string[] }) {
  const now = useNow(60_000);
  const iso = new Date(now).toISOString();
  return (
    <section aria-label="World clocks" className="flex flex-col gap-1">
      <h2 className="px-1 type-label font-semibold text-fg-muted">World clocks</h2>
      <ul className="flex flex-col">
        {zones.map((z) => (
          <li key={z} className="flex min-h-control items-center gap-2 px-1 type-label">
            <span className="min-w-0 flex-1 truncate text-fg">{z.split("/").pop()!.replace(/_/g, " ")}</span>
            <span className="tabular-nums text-fg">{new Date(now).toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit", timeZone: z })}</span>
            <span className="w-12 text-right type-caption text-fg-subtle">{calendarZoneLabel(iso, z)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
