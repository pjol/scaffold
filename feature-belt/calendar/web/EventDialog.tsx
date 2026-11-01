"use client";

import {
  CALENDAR_COLORS,
  calendarAddDays,
  calendarDateKey,
  calendarDaysBetween,
  calendarDayStart,
  calendarFromWall,
  calendarMinutesOfDay,
  calendarReminderLabel,
  calendarWeekday,
  calendarZoneLabel,
  errorMessage,
  formatCalendarDay,
  formatCalendarTime,
  orgsNoun,
  useCalendarFindTime,
  useCalendarFreeBusy,
  useCalendarPeople,
  useCalendarSettings,
  useCreateCalendarEvent,
  useDebouncedValue,
  useUpdateCalendarEvent,
  type CalendarColor,
  type CalendarEventDetail,
  type CalendarEventInput,
  type CalendarPerson,
  type CalendarRecurrence,
  type CalendarScope,
  type CalendarShowAs,
  type CalendarVisibility,
} from "@scaffoldapp/shared";
import { Button, Checkbox, Chip, Dialog, Field, IconButton, Input, SegmentedControl, Select, Switch, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { Check, Globe, Mail, Plus, Video, X } from "lucide-react";
import { useMemo, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { CALENDAR_COLOR_CLASSES, CALENDAR_COLOR_NAMES, ZoneSelect } from "./parts";

const CODES = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"] as const;
const DAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const ORD = ["first", "second", "third", "fourth", "fifth"];

type Repeat = "none" | "daily" | "weekdays" | "weekly" | "monthly_date" | "monthly_weekday" | "yearly" | "custom";
type Guest = { kind: "user"; person: CalendarPerson } | { kind: "email"; email: string; name: string };

interface FormState {
  title: string;
  description: string;
  location: string;
  allDay: boolean;
  startDate: string;
  startTime: string;
  endDate: string;
  endTime: string;
  timezone: string;
  repeat: Repeat;
  customFreq: CalendarRecurrence["freq"];
  interval: string;
  byDay: string[];
  ends: "never" | "on" | "after";
  until: string;
  count: string;
  guests: Guest[];
  conference: "keep" | "add" | "remove";
  showAs: CalendarShowAs;
  visibility: CalendarVisibility;
  color: CalendarColor;
  reminders: number[];
  sendUpdates: boolean;
}

/**
 * What opens the dialog: a new event over a time range (optionally prefilled, from a
 * new-event link), or an existing event's occurrence.
 */
export type EventDialogTarget =
  | { mode: "new"; startsAt: string; endsAt: string; allDay?: boolean; title?: string; description?: string; people?: CalendarPerson[]; emails?: string[]; video?: boolean }
  | { mode: "edit"; event: CalendarEventDetail };

const pad = (n: number) => String(n).padStart(2, "0");
const hhmm = (m: number) => `${pad(Math.floor(m / 60) % 24)}:${pad(m % 60)}`;
const toMin = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
};

function nthOfMonth(dateKey: string): string {
  const day = Number(dateKey.slice(8));
  const n = Math.ceil(day / 7);
  const last = Number(calendarAddDays(dateKey, 7).slice(8)) < day;
  return `${last && n >= 4 ? -1 : n}${CODES[calendarWeekday(dateKey)]}`;
}

function repeatOf(r: CalendarRecurrence | null, startDate: string): Repeat {
  if (!r) return "none";
  const simple = r.interval === 1;
  if (simple && r.freq === "daily" && r.by_day.length === 0) return "daily";
  if (simple && r.freq === "weekly" && r.by_day.join() === "MO,TU,WE,TH,FR") return "weekdays";
  if (simple && r.freq === "weekly" && (r.by_day.length === 0 || (r.by_day.length === 1 && r.by_day[0] === CODES[calendarWeekday(startDate)]))) return "weekly";
  if (simple && r.freq === "monthly" && r.by_day.length === 0 && (r.by_month_day.length === 0 || r.by_month_day.join() === String(Number(startDate.slice(8))))) return "monthly_date";
  if (simple && r.freq === "monthly" && r.by_day.length === 1 && r.by_day[0] === nthOfMonth(startDate)) return "monthly_weekday";
  if (simple && r.freq === "yearly") return "yearly";
  return "custom";
}

function initialState(target: EventDialogTarget, zone: string, defaults: number[], videoAvailable: boolean): FormState {
  if (target.mode === "edit") {
    const e = target.event;
    const tz = e.timezone || zone;
    const startDate = e.all_day && e.start_date ? e.start_date : calendarDateKey(e.starts_at, tz);
    const endDate = e.all_day && e.end_date ? e.end_date : calendarDateKey(e.ends_at, tz);
    const r = e.recurrence;
    return {
      title: e.title,
      description: e.description,
      location: e.location,
      allDay: e.all_day,
      startDate,
      startTime: hhmm(calendarMinutesOfDay(e.starts_at, tz)),
      endDate,
      endTime: hhmm(calendarMinutesOfDay(e.ends_at, tz)),
      timezone: tz,
      repeat: repeatOf(r, startDate),
      customFreq: r?.freq ?? "weekly",
      interval: String(r?.interval ?? 1),
      byDay: r?.by_day.filter((d) => d.length === 2) ?? [],
      ends: r?.count ? "after" : r?.until ? "on" : "never",
      until: r?.until ?? calendarAddDays(startDate, 90),
      count: String(r?.count ?? 10),
      guests: e.attendees.map((a): Guest => (a.user_id ? { kind: "user", person: { id: a.user_id, display_name: a.name, avatar_url: a.avatar_url } } : { kind: "email", email: a.email, name: a.name })),
      conference: "keep",
      showAs: e.show_as,
      visibility: e.visibility,
      color: e.color,
      reminders: e.reminders,
      sendUpdates: true,
    };
  }
  const startDate = calendarDateKey(target.startsAt, zone);
  const endDate = target.allDay ? startDate : calendarDateKey(target.endsAt, zone);
  const guests: Guest[] = [
    ...(target.people ?? []).map((person): Guest => ({ kind: "user", person })),
    ...(target.emails ?? []).map((email): Guest => ({ kind: "email", email, name: "" })),
  ];
  return {
    title: target.title ?? "",
    description: target.description ?? "",
    location: "",
    allDay: !!target.allDay,
    startDate,
    startTime: hhmm(calendarMinutesOfDay(target.startsAt, zone)),
    endDate,
    endTime: hhmm(calendarMinutesOfDay(target.endsAt, zone)),
    timezone: zone,
    repeat: "none",
    customFreq: "weekly",
    interval: "1",
    byDay: [CODES[calendarWeekday(startDate)]!],
    ends: "never",
    until: calendarAddDays(startDate, 90),
    count: "10",
    guests,
    conference: target.video && videoAvailable ? "add" : "keep",
    showAs: target.allDay ? "free" : "busy",
    visibility: "busy",
    color: "primary",
    reminders: target.allDay ? [] : defaults,
    sendUpdates: true,
  };
}

function recurrenceOf(f: FormState): CalendarEventInput["recurrence"] {
  const ends = f.ends === "after" ? { count: Number(f.count), until: null } : f.ends === "on" ? { count: null, until: f.until } : { count: null, until: null };
  const base = { interval: 1, by_day: [] as string[], by_month_day: [] as number[], ...ends };
  switch (f.repeat) {
    case "none":
      return null;
    case "daily":
      return { ...base, freq: "daily" };
    case "weekdays":
      return { ...base, freq: "weekly", by_day: ["MO", "TU", "WE", "TH", "FR"] };
    case "weekly":
      return { ...base, freq: "weekly", by_day: [CODES[calendarWeekday(f.startDate)]!] };
    case "monthly_date":
      return { ...base, freq: "monthly", by_month_day: [Number(f.startDate.slice(8))] };
    case "monthly_weekday":
      return { ...base, freq: "monthly", by_day: [nthOfMonth(f.startDate)] };
    case "yearly":
      return { ...base, freq: "yearly" };
    case "custom":
      return { ...base, freq: f.customFreq, interval: Math.max(1, Number(f.interval) || 1), by_day: f.customFreq === "weekly" ? f.byDay : [] };
  }
}

const REMINDER_CHOICES = [0, 5, 10, 15, 30, 60, 120, 1440, 2880, 10080];

/** Create or edit an event. Saving a change to a recurring event asks which occurrences change. */
export function EventDialog({ target, zone, onClose, onSaved }: { target: EventDialogTarget; zone: string; onClose: () => void; onSaved?: (e: CalendarEventDetail) => void }) {
  const settings = useCalendarSettings();
  const [f, setF] = useState<FormState>(() => initialState(target, zone, settings.data?.default_reminders ?? [10], settings.data?.video_available ?? false));
  const [errors, setErrors] = useState<Partial<Record<"title" | "time" | "guests" | "repeat", string>>>({});
  const [scopeOpen, setScopeOpen] = useState(false);
  const [showZone, setShowZone] = useState(target.mode === "edit" && target.event.timezone !== zone);
  const [more, setMore] = useState(false);
  const create = useCreateCalendarEvent();
  const update = useUpdateCalendarEvent();
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((s) => ({ ...s, [k]: v }));
  const editing = target.mode === "edit" ? target.event : null;
  const recurring = !!editing?.recurrence;
  const hasExternal = f.guests.some((g) => g.kind === "email");
  const dayNum = Number(f.startDate.slice(8));
  const nth = nthOfMonth(f.startDate);
  const nthLabel = `${nth.startsWith("-1") ? "last" : ORD[Number(nth.slice(0, -2)) - 1]} ${DAY_NAMES[calendarWeekday(f.startDate)]}`;

  const times = (): { startsAt: string; endsAt: string } | null => {
    if (f.allDay) return f.endDate >= f.startDate ? { startsAt: f.startDate, endsAt: f.endDate } : null;
    const s = calendarFromWall(f.startDate, toMin(f.startTime), f.timezone);
    const e = calendarFromWall(f.endDate, toMin(f.endTime), f.timezone);
    return e > s ? { startsAt: s, endsAt: e } : null;
  };

  const body = (scope: CalendarScope | null): CalendarEventInput | null => {
    const next: typeof errors = {};
    if (!f.title.trim()) next.title = "Add a title.";
    const t = times();
    if (!t) next.time = "The end must be after the start.";
    if (f.repeat !== "none" && f.ends === "after" && !(Number(f.count) >= 1 && Number(f.count) <= 999)) next.repeat = "Repeat 1 to 999 times.";
    if (f.repeat === "custom" && f.customFreq === "weekly" && f.byDay.length === 0) next.repeat = "Pick at least one day.";
    setErrors(next);
    if (Object.keys(next).length > 0 || !t) return null;
    const b: CalendarEventInput = {
      title: f.title.trim(),
      description: f.description,
      location: f.location,
      all_day: f.allDay,
      timezone: f.timezone,
      show_as: f.showAs,
      visibility: f.visibility,
      color: f.color,
      reminders: f.reminders,
    };
    if (f.allDay) {
      b.start_date = t.startsAt;
      b.end_date = t.endsAt;
    } else {
      b.starts_at = t.startsAt;
      b.ends_at = t.endsAt;
    }
    if (f.conference !== "keep") b.conference = f.conference;
    if (scope !== "this") {
      b.recurrence = recurrenceOf(f);
      b.attendees = f.guests.map((g) => (g.kind === "user" ? { user_id: g.person.id } : { email: g.email, name: g.name }));
    }
    if (editing) {
      b.send_updates = f.sendUpdates;
      if (scope) b.scope = scope;
      b.occurrence_start = editing.occurrence_start;
    }
    return b;
  };

  const save = async (scope: CalendarScope | null) => {
    const b = body(scope);
    if (!b) return;
    try {
      const saved = editing ? await update.mutateAsync({ id: editing.event_id, body: b }) : await create.mutateAsync(b);
      toast.success(editing ? "Event saved" : "Event added");
      onSaved?.(saved);
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the event. Try again."));
    }
  };

  const submit = () => {
    if (!body(null)) return;
    if (recurring) setScopeOpen(true);
    else void save(null);
  };

  const userGuests = f.guests.filter((g): g is Extract<Guest, { kind: "user" }> => g.kind === "user");

  return (
    <>
      <Dialog
        open
        onOpenChange={(o) => !o && onClose()}
        size="lg"
        title={editing ? "Edit event" : "New event"}
        footer={
          <div className="flex w-full flex-col-reverse gap-2 sm:flex-row sm:items-center sm:justify-end">
            {editing && f.guests.length > 0 ? (
              <Checkbox className="sm:mr-auto" checked={f.sendUpdates} onCheckedChange={(v) => set("sendUpdates", v)} label="Tell guests about changes" />
            ) : null}
            <Button variant="ghost" size="auto" onClick={onClose}>
              Cancel
            </Button>
            <Button size="auto" loading={create.isPending || update.isPending} onClick={submit}>
              {editing ? "Save" : "Add event"}
            </Button>
          </div>
        }
      >
        <form
          className="flex flex-col gap-6"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Field label="Title" error={errors.title} required>
            <Input value={f.title} onChange={(e) => set("title", e.target.value)} autoFocus maxLength={200} placeholder="Add a title" />
          </Field>

          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between gap-3">
              <Switch checked={f.allDay} onCheckedChange={(v) => setF((s) => ({ ...s, allDay: v, showAs: v ? "free" : "busy", endDate: v ? s.startDate : s.endDate }))} label="All day" />
              <button type="button" onClick={() => setShowZone((v) => !v)} className="flex min-h-control items-center gap-1.5 rounded-sm type-label text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
                <Globe className="size-4" aria-hidden />
                {calendarZoneLabel(calendarDayStart(f.startDate, f.timezone), f.timezone)}
              </button>
            </div>
            <div className="flex flex-col gap-3">
              <Field label="Starts" error={errors.time}>
                <div className="flex gap-2">
                  <Input
                    type="date"
                    value={f.startDate}
                    onChange={(e) => {
                      const v = e.target.value;
                      if (!v) return;
                      const shift = calendarDaysBetween(f.startDate, v);
                      setF((s) => ({ ...s, startDate: v, endDate: calendarAddDays(s.endDate, shift) }));
                    }}
                    aria-label="Start date"
                  />
                  {!f.allDay ? (
                    <Input
                      type="time"
                      step={900}
                      value={f.startTime}
                      onChange={(e) => {
                        const v = e.target.value;
                        if (!v) return;
                        const len = toMin(f.endTime) - toMin(f.startTime) + calendarDaysBetween(f.startDate, f.endDate) * 1440;
                        const endTotal = toMin(v) + Math.max(len, 15);
                        setF((s) => ({ ...s, startTime: v, endTime: hhmm(endTotal % 1440), endDate: calendarAddDays(s.startDate, Math.floor(endTotal / 1440)) }));
                      }}
                      aria-label="Start time"
                    />
                  ) : null}
                </div>
              </Field>
              <Field label="Ends">
                <div className="flex gap-2">
                  <Input type="date" value={f.endDate} min={f.startDate} onChange={(e) => e.target.value && set("endDate", e.target.value)} aria-label="End date" />
                  {!f.allDay ? <Input type="time" step={900} value={f.endTime} onChange={(e) => e.target.value && set("endTime", e.target.value)} aria-label="End time" /> : null}
                </div>
              </Field>
            </div>
            {showZone ? (
              <Field label="Time zone" info="Times are entered in this zone. Repeating events keep their local time in it across daylight-saving changes.">
                <ZoneSelect value={f.timezone} onChange={(z) => set("timezone", z)} />
              </Field>
            ) : null}
          </div>

          <div className="flex flex-col gap-3">
            <Field label="Repeat" error={errors.repeat}>
              <Select
                value={f.repeat}
                onValueChange={(v) => set("repeat", v as Repeat)}
                options={[
                  { value: "none", label: "Doesn't repeat" },
                  { value: "daily", label: "Every day" },
                  { value: "weekdays", label: "Every weekday (Monday to Friday)" },
                  { value: "weekly", label: `Every week on ${DAY_NAMES[calendarWeekday(f.startDate)]}` },
                  { value: "monthly_date", label: `Every month on day ${dayNum}` },
                  { value: "monthly_weekday", label: `Every month on the ${nthLabel}` },
                  { value: "yearly", label: `Every year on ${formatCalendarDay(f.startDate).split(", ")[1] ?? ""}` },
                  { value: "custom", label: "Custom" },
                ]}
              />
            </Field>
            {f.repeat === "custom" ? (
              <div className="flex flex-col gap-3 rounded-md bg-sunken p-4">
                <div className="flex items-center gap-2">
                  <span className="type-label text-fg">Every</span>
                  <Input type="number" min={1} max={99} value={f.interval} onChange={(e) => set("interval", e.target.value)} className="w-20" aria-label="Interval" />
                  <Select
                    value={f.customFreq}
                    onValueChange={(v) => set("customFreq", v as CalendarRecurrence["freq"])}
                    options={[
                      { value: "daily", label: "days" },
                      { value: "weekly", label: "weeks" },
                      { value: "monthly", label: "months" },
                      { value: "yearly", label: "years" },
                    ]}
                  />
                </div>
                {f.customFreq === "weekly" ? (
                  <div className="flex flex-wrap gap-2" role="group" aria-label="On days">
                    {CODES.map((c, i) => (
                      <button
                        key={c}
                        type="button"
                        aria-pressed={f.byDay.includes(c)}
                        aria-label={DAY_NAMES[i]}
                        onClick={() => set("byDay", f.byDay.includes(c) ? f.byDay.filter((d) => d !== c) : [...f.byDay, c])}
                        className={cn(
                          "flex size-10 items-center justify-center rounded-pill type-label focus-visible:outline-2 focus-visible:outline-ring",
                          f.byDay.includes(c) ? "bg-primary text-primary-fg" : "bg-surface text-fg hover:bg-canvas",
                        )}
                      >
                        {DAY_NAMES[i]!.slice(0, 1)}
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>
            ) : null}
            {f.repeat !== "none" ? (
              <div className="flex flex-wrap items-center gap-2">
                <span className="type-label text-fg-muted">Ends</span>
                <SegmentedControl
                  fullWidth={false}
                  value={f.ends}
                  onValueChange={(v) => set("ends", v)}
                  options={[
                    { value: "never", label: "Never" },
                    { value: "on", label: "On" },
                    { value: "after", label: "After" },
                  ]}
                  aria-label="Ends"
                />
                {f.ends === "on" ? <Input type="date" value={f.until} min={f.startDate} onChange={(e) => e.target.value && set("until", e.target.value)} className="w-44" aria-label="Last day" /> : null}
                {f.ends === "after" ? (
                  <span className="flex items-center gap-2">
                    <Input type="number" min={1} max={999} value={f.count} onChange={(e) => set("count", e.target.value)} className="w-20" aria-label="Times" />
                    <span className="type-label text-fg-muted">times</span>
                  </span>
                ) : null}
              </div>
            ) : null}
          </div>

          <GuestPicker guests={f.guests} onChange={(g) => set("guests", g)} />
          {userGuests.length > 0 && !f.allDay ? (
            <GuestAvailability
              people={userGuests.map((g) => g.person)}
              date={f.startDate}
              zone={f.timezone}
              startMin={toMin(f.startTime)}
              endMin={toMin(f.endTime)}
              onPick={(s, e) => setF((st) => ({ ...st, startDate: calendarDateKey(s, st.timezone), startTime: hhmm(calendarMinutesOfDay(s, st.timezone)), endDate: calendarDateKey(e, st.timezone), endTime: hhmm(calendarMinutesOfDay(e, st.timezone)) }))}
            />
          ) : null}

          <div className="flex flex-col gap-3">
            <Field label="Location">
              <Input value={f.location} onChange={(e) => set("location", e.target.value)} maxLength={500} placeholder="Room, address or link" />
            </Field>
            <VideoRow
              available={settings.data?.video_available ?? false}
              current={editing?.conference_url ?? null}
              action={f.conference}
              onChange={(a) => set("conference", a)}
              external={hasExternal}
            />
          </div>

          <Field label="Description">
            <TextArea value={f.description} onChange={(e) => set("description", e.target.value)} maxLength={8000} minRows={3} />
          </Field>

          {more ? (
            <div className="flex flex-col gap-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="Show as" info="Free time stays bookable on your booking links.">
                  <SegmentedControl value={f.showAs} onValueChange={(v) => set("showAs", v)} options={[{ value: "busy", label: "Busy" }, { value: "free", label: "Free" }]} />
                </Field>
                <Field label="Colleagues see" info={`People in your ${orgsNoun("many")} checking your availability see either just “Busy” or this event's title.`}>
                  <SegmentedControl value={f.visibility} onValueChange={(v) => set("visibility", v)} options={[{ value: "busy", label: "Busy" }, { value: "details", label: "Title" }]} />
                </Field>
              </div>
              <Field label="Color">
                <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Color">
                  {CALENDAR_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      role="radio"
                      aria-checked={f.color === c}
                      aria-label={CALENDAR_COLOR_NAMES[c]}
                      onClick={() => set("color", c)}
                      className={cn(
                        "flex size-10 items-center justify-center rounded-pill focus-visible:outline-2 focus-visible:outline-ring",
                        CALENDAR_COLOR_CLASSES[c].chip,
                        f.color === c && "ring-2 ring-offset-2 ring-offset-surface " + CALENDAR_COLOR_CLASSES[c].ring,
                      )}
                    >
                      {f.color === c ? <Check className="size-4" aria-hidden /> : null}
                    </button>
                  ))}
                </div>
              </Field>
              <Field label="Reminders" info="Reminders go to you and to colleagues who haven't declined, in the app and as push notifications.">
                <div className="flex flex-wrap items-center gap-2">
                  {f.reminders.map((m) => (
                    <Chip key={m} onClick={() => set("reminders", f.reminders.filter((x) => x !== m))} icon={<X />} aria-label={`Remove reminder ${calendarReminderLabel(m)}`}>
                      {calendarReminderLabel(m)}
                    </Chip>
                  ))}
                  {f.reminders.length < 5 ? (
                    <Select
                      value=""
                      placeholder="Add reminder"
                      onValueChange={(v) => set("reminders", [...f.reminders, Number(v)].sort((a, b) => a - b))}
                      options={REMINDER_CHOICES.filter((m) => !f.reminders.includes(m)).map((m) => ({ value: String(m), label: calendarReminderLabel(m) }))}
                    />
                  ) : null}
                </div>
              </Field>
            </div>
          ) : (
            <Button variant="ghost" size="auto" className="self-start" onClick={() => setMore(true)}>
              More options
            </Button>
          )}
        </form>
      </Dialog>
      <ScopeDialog
        open={scopeOpen}
        onOpenChange={setScopeOpen}
        title="Save recurring event"
        confirmLabel="Save"
        allowThis={!(f.repeat !== repeatOf(editing?.recurrence ?? null, f.startDate)) && f.guests.length === (editing?.attendees.length ?? 0)}
        onConfirm={(scope) => save(scope)}
      />
    </>
  );
}

function VideoRow({ available, current, action, onChange, external }: { available: boolean; current: string | null; action: "keep" | "add" | "remove"; onChange: (a: "keep" | "add" | "remove") => void; external: boolean }) {
  const has = (current && action !== "remove") || action === "add";
  if (!available && !current) return null;
  return (
    <div className="flex min-h-control flex-wrap items-center gap-2">
      <Video className="size-5 text-fg-muted" aria-hidden />
      {has ? (
        <>
          <span className="min-w-0 flex-1 truncate type-label text-fg">{action === "add" ? "A video call link is added when you save" : current}</span>
          <Button variant="ghost" size="sm" onClick={() => onChange(current ? "remove" : "keep")}>
            Remove
          </Button>
        </>
      ) : (
        <Button variant="secondary" size="sm" onClick={() => onChange(current ? "keep" : "add")} disabled={!available}>
          Add video call
        </Button>
      )}
      {has && external ? <span className="w-full type-caption text-fg-muted">{`Guests outside your ${orgsNoun("many")} can join without an account.`}</span> : null}
    </div>
  );
}

const EMAIL = /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"]+$/;

/** Colleagues from your organizations (search) and anyone by email. */
function GuestPicker({ guests, onChange }: { guests: Guest[]; onChange: (g: Guest[]) => void }) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const debounced = useDebouncedValue(q.trim(), 200);
  const people = useCalendarPeople(debounced, { enabled: open });
  const chosen = new Set(guests.map((g) => (g.kind === "user" ? g.person.id : g.email)));
  const results = (people.data?.items ?? []).filter((p) => !chosen.has(p.id)).slice(0, 6);
  const isEmail = EMAIL.test(q.trim());

  const addEmail = () => {
    const email = q.trim().toLowerCase();
    if (!EMAIL.test(email) || chosen.has(email)) return;
    onChange([...guests, { kind: "email", email, name: "" }]);
    setQ("");
  };

  return (
    <Field label="Guests" info="Colleagues get an invitation in the app. Anyone else gets an email with a calendar invite and a link to reply.">
      <div className="flex flex-col gap-2">
        <div className="relative">
          <Input
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setOpen(true);
            }}
            onFocus={() => setOpen(true)}
            onBlur={() => setTimeout(() => setOpen(false), 150)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                if (isEmail) addEmail();
                else if (results[0]) {
                  onChange([...guests, { kind: "user", person: results[0] }]);
                  setQ("");
                }
              }
            }}
            placeholder="Add people or emails"
            role="combobox"
            aria-expanded={open && (results.length > 0 || isEmail)}
            aria-autocomplete="list"
          />
          {open && (results.length > 0 || isEmail) ? (
            <ul role="listbox" className="absolute inset-x-0 top-full z-30 mt-1 flex flex-col rounded-md border border-border bg-surface-raised p-1 shadow-lg">
              {results.map((p) => (
                <li key={p.id} role="option" aria-selected={false}>
                  <button
                    type="button"
                    onMouseDown={(e) => e.preventDefault()}
                    onClick={() => {
                      onChange([...guests, { kind: "user", person: p }]);
                      setQ("");
                    }}
                    className="flex min-h-tap w-full items-center gap-3 rounded-sm px-2 text-left hover:bg-sunken"
                  >
                    <AppAvatar name={p.display_name} src={p.avatar_url} size="sm" />
                    <span className="truncate type-body text-fg">{p.display_name}</span>
                  </button>
                </li>
              ))}
              {isEmail ? (
                <li role="option" aria-selected={false}>
                  <button type="button" onMouseDown={(e) => e.preventDefault()} onClick={addEmail} className="flex min-h-tap w-full items-center gap-3 rounded-sm px-2 text-left hover:bg-sunken">
                    <Mail className="size-5 text-fg-muted" aria-hidden />
                    <span className="truncate type-body text-fg">Invite {q.trim()}</span>
                  </button>
                </li>
              ) : null}
            </ul>
          ) : null}
        </div>
        {guests.length > 0 ? (
          <ul className="flex flex-wrap gap-2">
            {guests.map((g) => (
              <li key={g.kind === "user" ? g.person.id : g.email} className="flex items-center gap-2 rounded-pill bg-sunken py-1 pr-1 pl-1">
                {g.kind === "user" ? <AppAvatar name={g.person.display_name} src={g.person.avatar_url} size="xs" /> : <Mail className="ml-1 size-4 text-fg-muted" aria-hidden />}
                <span className="max-w-48 truncate type-label text-fg">{g.kind === "user" ? g.person.display_name : g.email}</span>
                <IconButton
                  label={`Remove ${g.kind === "user" ? g.person.display_name : g.email}`}
                  icon={<X />}
                  size="sm"
                  variant="ghost"
                  onClick={() => onChange(guests.filter((x) => x !== g))}
                />
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </Field>
  );
}

/** Colleagues' busy time on the event's day (free/busy only) and the next times everyone is free. */
function GuestAvailability({ people, date, zone, startMin, endMin, onPick }: { people: CalendarPerson[]; date: string; zone: string; startMin: number; endMin: number; onPick: (s: string, e: string) => void }) {
  const from = calendarDayStart(date, zone);
  const to = calendarDayStart(calendarAddDays(date, 1), zone);
  const ids = people.map((p) => p.id);
  const fb = useCalendarFreeBusy(ids, from, to);
  const duration = Math.max(15, endMin - startMin);
  // Rounded to the quarter hour so the query key stays stable between renders.
  const findFrom = useMemo(() => new Date(Math.max(Math.ceil(Date.now() / 900_000) * 900_000, new Date(from).getTime())).toISOString(), [from]);
  const suggestions = useCalendarFindTime({ user_ids: ids, from: findFrom, to: calendarDayStart(calendarAddDays(date, 7), zone), duration });
  const lo = 7 * 60;
  const hi = 20 * 60;
  const pct = (m: number) => `${((Math.min(hi, Math.max(lo, m)) - lo) / (hi - lo)) * 100}%`;
  const rows = useMemo(() => fb.data?.items ?? [], [fb.data]);
  return (
    <div className="flex flex-col gap-3" aria-label="Guests' availability">
      <span className="type-label font-semibold text-fg">Availability on {formatCalendarDay(date)}</span>
      <div className="flex flex-col gap-2">
        {fb.isPending ? (
          <div className="h-8 animate-pulse rounded-sm bg-sunken motion-reduce:animate-none" />
        ) : fb.isError ? (
          <p className="type-caption text-fg-muted">{errorMessage(fb.error, "Couldn't load availability.")}</p>
        ) : (
          rows.map((r) => (
            <div key={r.user.id} className="flex items-center gap-3">
              <span className="w-28 shrink-0 truncate type-caption text-fg">{r.user.display_name}</span>
              <div className="relative h-6 flex-1 rounded-sm bg-sunken">
                <div className="absolute inset-y-0 rounded-sm border border-primary" style={{ left: pct(startMin), width: `calc(${pct(endMin)} - ${pct(startMin)})` }} aria-hidden />
                {r.busy.map((b, i) => {
                  const s = calendarDateKey(b.starts_at, zone) < date ? 0 : calendarMinutesOfDay(b.starts_at, zone);
                  const e = calendarDateKey(b.ends_at, zone) > date ? 1440 : calendarMinutesOfDay(b.ends_at, zone);
                  return (
                    <div
                      key={i}
                      title={b.title ?? "Busy"}
                      className="absolute inset-y-1 rounded-sm bg-fg-muted"
                      style={{ left: pct(s), width: `calc(${pct(e)} - ${pct(s)})` }}
                    />
                  );
                })}
              </div>
            </div>
          ))
        )}
        <div className="flex items-center gap-3 type-caption text-fg-subtle" aria-hidden>
          <span className="w-28 shrink-0" />
          <span className="flex flex-1 justify-between">
            <span>7 AM</span>
            <span>1 PM</span>
            <span>8 PM</span>
          </span>
        </div>
      </div>
      {suggestions.data && suggestions.data.slots.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          <span className="type-caption text-fg-muted">Everyone's free</span>
          {suggestions.data.slots.slice(0, 4).map((s) => (
            <Chip key={s.starts_at} size="sm" icon={<Plus />} onClick={() => onPick(s.starts_at, s.ends_at)}>
              {formatCalendarDay(calendarDateKey(s.starts_at, zone))} {formatCalendarTime(s.starts_at, zone)}
            </Chip>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/** Asks which occurrences of a recurring event a save or delete applies to. */
export function ScopeDialog({
  open,
  onOpenChange,
  title,
  confirmLabel,
  tone = "primary",
  allowThis = true,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  title: string;
  confirmLabel: string;
  tone?: "primary" | "danger";
  allowThis?: boolean;
  onConfirm: (scope: CalendarScope) => Promise<void> | void;
}) {
  const [scope, setScope] = useState<CalendarScope>(allowThis ? "this" : "following");
  const [busy, setBusy] = useState(false);
  const options: { value: CalendarScope; label: string }[] = [
    ...(allowThis ? [{ value: "this" as const, label: "This event" }] : []),
    { value: "following", label: "This and following events" },
    { value: "all", label: "All events" },
  ];
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="sm"
      title={title}
      footer={
        <div className="flex w-full justify-end gap-2">
          <Button variant="ghost" size="auto" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant={tone}
            size="auto"
            loading={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await onConfirm(scope);
                onOpenChange(false);
              } finally {
                setBusy(false);
              }
            }}
          >
            {confirmLabel}
          </Button>
        </div>
      }
    >
      <div role="radiogroup" aria-label="Which events" className="flex flex-col gap-1">
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={scope === o.value}
            onClick={() => setScope(o.value)}
            className="flex min-h-tap items-center gap-3 rounded-md px-2 text-left type-body text-fg hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
          >
            <span className={cn("flex size-5 items-center justify-center rounded-pill border-2", scope === o.value ? "border-primary" : "border-border-strong")} aria-hidden>
              {scope === o.value ? <span className="size-2 rounded-pill bg-primary" /> : null}
            </span>
            {o.label}
          </button>
        ))}
      </div>
    </Dialog>
  );
}
