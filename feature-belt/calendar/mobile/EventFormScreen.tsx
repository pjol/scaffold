import {
  calendarAddDays,
  calendarConferenceOptions,
  calendarDateKey,
  calendarFindCalendar,
  calendarWritableCalendars,
  calendarDaysBetween,
  calendarDraftGuests,
  calendarDraftTimes,
  calendarFromWall,
  calendarMinutesOfDay,
  calendarReminderLabel,
  calendarWeekday,
  describeCalendarRecurrence,
  errorMessage,
  orgsNoun,
  sourceProductLabel,
  useCalendarEvent,
  useCalendarSources,
  useCreateCalendarConnectedEvent,
  useCalendarPeople,
  useCalendarPeopleByIds,
  useCalendarSettings,
  useCreateCalendarEvent,
  useDebouncedValue,
  useUpdateCalendarEvent,
  type CalendarCalendarRef,
  type CalendarConnectedEventInput,
  type CalendarEventDetail,
  type CalendarEventInput,
  type CalendarNewEventDraft,
  type CalendarPerson,
  type CalendarScope,
  type CalendarAutoDecline,
  type CalendarEventRoom,
  type CalendarEventType,
  calendarDayStart,
  formatCalendarDay,
  formatCalendarTime,
  useActiveOrg,
  useCalendarCalendars,
  useCalendarFindTime,
  useCalendarRooms,
} from "@scaffoldapp/shared";
import { Avatar, Button, Chip, EmptyState, ErrorState, Field, IconButton, Input, ListGroup, ListRow, ProviderIcon, Screen, SegmentedControl, Sheet, Skeleton, Switch, Text, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { CalendarX, Check, DoorOpen, Mail, Plus, Search, X } from "lucide-react-native";
import { useMemo, useState } from "react";
import { View } from "react-native";
import { CalendarDateTimeRow, useCalendarZone } from "./parts";

const CODES = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"] as const;
const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
type Repeat = "none" | "daily" | "weekdays" | "weekly" | "monthly" | "yearly" | "custom";
type Guest = { kind: "user"; person: CalendarPerson } | { kind: "email"; email: string };
const REMINDERS = [0, 5, 10, 15, 30, 60, 1440];
const EMAIL = /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"]+$/;

const pad = (n: number) => String(n).padStart(2, "0");
const hhmm = (m: number) => `${pad(Math.floor(m / 60) % 24)}:${pad(m % 60)}`;
const toMin = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
};

interface Form {
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
  guests: Guest[];
  conference: "keep" | "add" | "remove";
  reminders: number[];
  /** "app", or a connected calendar as "<connection>|<calendar>". */
  calendar: string;
  /** Meeting provider for "add": "<option id>|<connection>". */
  provider: string;
  eventType: CalendarEventType;
  autoDecline: CalendarAutoDecline;
  declineMessage: string;
  rooms: CalendarEventRoom[];
}

const calKey = (r: CalendarCalendarRef) => `${r.connection_id}|${r.calendar_id}`;
const calRef = (k: string): CalendarCalendarRef | null => {
  if (k.startsWith("cal:")) return null;
  const i = k.indexOf("|");
  return i > 0 ? { connection_id: k.slice(0, i), calendar_id: k.slice(i + 1) } : null;
};

function repeatOf(e: CalendarEventDetail | null, startDate: string): Repeat {
  const r = e?.recurrence;
  if (!r) return "none";
  if (r.interval !== 1 || r.count || r.until) return "custom";
  if (r.freq === "daily" && r.by_day.length === 0) return "daily";
  if (r.freq === "weekly" && r.by_day.join() === "MO,TU,WE,TH,FR") return "weekdays";
  if (r.freq === "weekly" && (r.by_day.length === 0 || (r.by_day.length === 1 && r.by_day[0] === CODES[calendarWeekday(startDate)]))) return "weekly";
  if (r.freq === "monthly" && r.by_day.length === 0 && (r.by_month_day.length === 0 || r.by_month_day[0] === Number(startDate.slice(8)))) return "monthly";
  if (r.freq === "yearly") return "yearly";
  return "custom";
}

/** A new event's prefill: the draft from a new-event link plus its guests by id, once loaded. */
interface Prefill {
  draft: CalendarNewEventDraft;
  people: CalendarPerson[];
  videoAvailable: boolean;
}

function initial(e: CalendarEventDetail | null, zone: string, prefill: Prefill, reminders: number[], eventType: CalendarEventType = "default"): Form {
  if (e) {
    const tz = e.timezone || zone;
    const startDate = e.all_day && e.start_date ? e.start_date : calendarDateKey(e.starts_at, tz);
    return {
      title: e.title,
      description: e.description,
      location: e.location,
      allDay: e.all_day,
      startDate,
      startTime: hhmm(calendarMinutesOfDay(e.starts_at, tz)),
      endDate: e.all_day && e.end_date ? e.end_date : calendarDateKey(e.ends_at, tz),
      endTime: hhmm(calendarMinutesOfDay(e.ends_at, tz)),
      timezone: tz,
      repeat: repeatOf(e, startDate),
      guests: e.attendees.map((a): Guest => (a.user_id ? { kind: "user", person: { id: a.user_id, display_name: a.name, avatar_url: a.avatar_url } } : { kind: "email", email: a.email })),
      conference: "keep",
      reminders: e.reminders,
      calendar: e.calendar ? `cal:${e.calendar.id}` : "app",
      provider: "",
      eventType: e.event_type ?? "default",
      autoDecline: e.auto_decline ?? "none",
      declineMessage: e.decline_message ?? "",
      rooms: e.rooms ?? [],
    };
  }
  const { draft } = prefill;
  const { startsAt: start, endsAt: end } = calendarDraftTimes(draft);
  const allDay = eventType === "out_of_office" && calendarMinutesOfDay(start, zone) === 0 && calendarMinutesOfDay(end, zone) === 0;
  return {
    title: draft.title ?? (eventType === "out_of_office" ? "Out of office" : eventType === "focus" ? "Focus time" : ""),
    description: draft.description ?? "",
    location: "",
    allDay,
    startDate: calendarDateKey(start, zone),
    startTime: hhmm(calendarMinutesOfDay(start, zone)),
    endDate: allDay ? calendarDateKey(start, zone) : calendarDateKey(end, zone),
    endTime: hhmm(calendarMinutesOfDay(end, zone)),
    timezone: zone,
    repeat: "none",
    guests: [...prefill.people.map((person): Guest => ({ kind: "user", person })), ...calendarDraftGuests(draft).emails.map((email): Guest => ({ kind: "email", email }))],
    conference: draft.video && prefill.videoAvailable ? "add" : "keep",
    reminders: eventType === "default" ? reminders : [],
    calendar: "app",
    provider: "",
    eventType,
    autoDecline: eventType === "default" ? "none" : "new",
    declineMessage: "",
    rooms: [],
  };
}

/**
 * Create or edit an event (modal). Edit with `id` (and `occurrence` for one occurrence of a
 * series); create prefilled from `draft` (a new-event link: title, times, guests, video).
 */
export function EventFormScreen({ id, occurrence, draft = {}, eventType }: { id?: string; occurrence?: string; draft?: CalendarNewEventDraft; eventType?: CalendarEventType }) {
  const theme = useTheme();
  const event = useCalendarEvent(id, occurrence ?? null);
  const settings = useCalendarSettings();
  const { zone, ready } = useCalendarZone();
  const guestIds = useMemo(() => (id ? [] : calendarDraftGuests(draft).userIds), [id, draft]);
  const people = useCalendarPeopleByIds(guestIds);
  if ((id && event.isPending) || settings.isPending || !ready || (guestIds.length > 0 && people.isPending)) {
    return (
      <Screen edges={["bottom"]} gap={theme.space[4]}>
        <Skeleton height={theme.tap.base} />
        <Skeleton height={theme.tap.base} />
        <Skeleton height={theme.tap.lg * 2} />
      </Screen>
    );
  }
  if (id && event.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        {event.error.status === 404 ? <EmptyState icon={CalendarX} title="This event isn't on your calendar" /> : <ErrorState title="Couldn't load the event" info={errorMessage(event.error)} onRetry={() => void event.refetch()} />}
      </Screen>
    );
  }
  if (event.data && !event.data.can_edit) {
    return (
      <Screen edges={["bottom"]} centered>
        <EmptyState icon={CalendarX} title="Only the organizer can edit this event" />
      </Screen>
    );
  }
  const videoAvailable = settings.data?.video_available ?? false;
  return (
    <Form
      event={event.data ?? null}
      zone={zone}
      prefill={{ draft, people: people.data?.items ?? [], videoAvailable }}
      defaults={settings.data?.default_reminders ?? [10]}
      videoAvailable={videoAvailable}
      {...(eventType ? { eventType } : {})}
    />
  );
}

function Form({ event, zone, prefill, defaults, videoAvailable, eventType }: { event: CalendarEventDetail | null; zone: string; prefill: Prefill; defaults: number[]; videoAvailable: boolean; eventType?: CalendarEventType }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const [f, setF] = useState<Form>(() => initial(event, zone, prefill, defaults, eventType));
  const calendars = useCalendarCalendars();
  const appCalendars = (calendars.data?.items ?? []).filter((x) => x.level === "edit" || x.level === "manage");
  const block = f.eventType !== "default";
  const [error, setError] = useState<{ title?: string; time?: string }>({});
  const [sheet, setSheet] = useState<"repeat" | "guests" | "scope" | "calendar" | "provider" | "rooms" | null>(null);
  const create = useCreateCalendarEvent();
  const createConnected = useCreateCalendarConnectedEvent();
  // New events can go to a connected calendar (Google, Outlook), whose platform invites guests.
  const sources = useCalendarSources();
  const writable = calendarWritableCalendars(sources.data);
  const target = calRef(f.calendar);
  const targetCal = calendarFindCalendar(sources.data, target);
  const providers = calendarConferenceOptions(sources.data, target);
  const provider = providers.find((o) => `${o.id}|${o.connection_id ?? ""}` === f.provider) ?? providers[0];
  const calendarLabel = (key: string) => {
    if (key.startsWith("cal:")) {
      const cal = appCalendars.find((x) => x.id === key.slice(4));
      return cal ? (cal.mine ? cal.name : `${cal.name} · ${cal.team_name ? "team" : (cal.owner?.display_name ?? "")}`) : "Calendar";
    }
    const found = calendarFindCalendar(sources.data, calRef(key));
    if (!found) return appCalendars.find((x) => x.is_primary && x.mine)?.name ?? "This app's calendar";
    const name = found.calendar.primary && found.calendar.name === found.source.account ? sourceProductLabel(found.source.provider ?? "", found.source.product) : found.calendar.name;
    return `${name} · ${found.source.account}`;
  };
  const update = useUpdateCalendarEvent();
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setF((x) => ({ ...x, [k]: v }));
  const custom = f.repeat === "custom";

  const repeatLabel: Record<Repeat, string> = {
    none: "Doesn't repeat",
    daily: "Every day",
    weekdays: "Every weekday",
    weekly: `Every week on ${DAYS[calendarWeekday(f.startDate)]}`,
    monthly: `Every month on day ${Number(f.startDate.slice(8))}`,
    yearly: "Every year",
    custom: event?.recurrence ? describeCalendarRecurrence(event.recurrence) : "Custom",
  };

  const body = (scope: CalendarScope | null): CalendarEventInput | null => {
    const next: typeof error = {};
    if (!f.title.trim()) next.title = "Add a title.";
    const s1 = calendarFromWall(f.startDate, toMin(f.startTime), f.timezone);
    const e1 = calendarFromWall(f.endDate, toMin(f.endTime), f.timezone);
    if (f.allDay ? f.endDate < f.startDate : e1 <= s1) next.time = "The end must be after the start.";
    setError(next);
    if (Object.keys(next).length > 0) return null;
    const b: CalendarEventInput = { title: f.title.trim(), description: f.description, location: f.location, all_day: f.allDay, timezone: f.timezone, reminders: f.reminders };
    if (f.allDay) Object.assign(b, { start_date: f.startDate, end_date: f.endDate });
    else Object.assign(b, { starts_at: s1, ends_at: e1 });
    if (f.conference !== "keep") b.conference = f.conference;
    if (f.conference === "add" && provider) {
      b.conference_provider = provider.id;
      if (provider.connection_id && !provider.native) b.conference_connection_id = provider.connection_id;
    }
    if (scope !== "this") {
      b.attendees = f.guests.map((g) => (g.kind === "user" ? { user_id: g.person.id } : { email: g.email }));
      if (!custom) {
        const base = { interval: 1, by_day: [] as string[], by_month_day: [] as number[], count: null, until: null };
        b.recurrence =
          f.repeat === "none"
            ? null
            : f.repeat === "daily"
              ? { ...base, freq: "daily" }
              : f.repeat === "weekdays"
                ? { ...base, freq: "weekly", by_day: ["MO", "TU", "WE", "TH", "FR"] }
                : f.repeat === "weekly"
                  ? { ...base, freq: "weekly", by_day: [CODES[calendarWeekday(f.startDate)]!] }
                  : f.repeat === "monthly"
                    ? { ...base, freq: "monthly", by_month_day: [Number(f.startDate.slice(8))] }
                    : { ...base, freq: "yearly" };
      }
    }
    if (event) {
      if (scope) b.scope = scope;
      b.occurrence_start = event.occurrence_start;
    }
    const calId = f.calendar.startsWith("cal:") ? f.calendar.slice(4) : null;
    if (calId && scope !== "this" && (!event || event.calendar?.id !== calId)) b.calendar_id = calId;
    if (!event && block) b.event_type = f.eventType;
    if (block) {
      b.auto_decline = f.autoDecline;
      b.decline_message = f.declineMessage.trim();
      delete b.attendees;
      delete b.conference;
    } else if (scope !== "this") {
      b.room_ids = f.rooms.map((r) => r.id);
    }
    return b;
  };

  const save = async (scope: CalendarScope | null) => {
    const b = body(scope);
    if (!b) return;
    if (target && !event) {
      const product = targetCal ? sourceProductLabel(targetCal.source.provider ?? "", targetCal.source.product) : "the calendar";
      const cb: CalendarConnectedEventInput = {
        title: b.title!,
        description: b.description ?? "",
        location: b.location ?? "",
        all_day: b.all_day!,
        timezone: b.timezone!,
        ...(b.start_date ? { start_date: b.start_date, end_date: b.end_date! } : { starts_at: b.starts_at!, ends_at: b.ends_at! }),
        ...(b.recurrence ? { recurrence: b.recurrence } : {}),
        attendees: b.attendees ?? [],
        ...(b.conference ? { conference: b.conference } : {}),
        ...(b.conference_provider ? { conference_provider: b.conference_provider } : {}),
        ...(b.conference_connection_id ? { conference_connection_id: b.conference_connection_id } : {}),
      };
      try {
        await createConnected.mutateAsync({ calendar: target, body: cb });
        toast.success(`Added to ${product}`);
        router.back();
      } catch (e) {
        toast.error(errorMessage(e, "Couldn't save the event. Try again."));
      }
      return;
    }
    try {
      if (event) await update.mutateAsync({ id: event.event_id, body: b });
      else await create.mutateAsync(b);
      toast.success(event ? "Event saved" : "Event added");
      router.back();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the event. Try again."));
    }
  };

  return (
    <Screen
      edges={["bottom"]}
      keyboard
      gap={s[6]}
      footer={
        <Button
          fullWidth
          loading={create.isPending || update.isPending || createConnected.isPending}
          onPress={() => {
            if (!body(null)) return;
            if (event?.recurrence) setSheet("scope");
            else void save(null);
          }}
        >
          {event ? "Save" : "Add event"}
        </Button>
      }
    >
      <Field label="Title" error={error.title} required>
        <Input value={f.title} onChangeText={(v) => set("title", v)} placeholder="Add a title" maxLength={200} autoFocus={!event} />
      </Field>
      {!event && !target ? (
        <SegmentedControl<CalendarEventType>
          accessibilityLabel="Kind of event"
          value={f.eventType}
          onValueChange={(v) =>
            setF((x) => ({
              ...x,
              eventType: v,
              autoDecline: v === "default" ? "none" : "new",
              title: !x.title || ["Out of office", "Focus time"].includes(x.title) ? (v === "out_of_office" ? "Out of office" : v === "focus" ? "Focus time" : "") : x.title,
            }))
          }
          options={[
            { value: "default", label: "Event" },
            { value: "out_of_office", label: "Out of office" },
            { value: "focus", label: "Focus" },
          ]}
        />
      ) : null}
      {block ? (
        <View style={{ gap: s[3] }}>
          <ListGroup title="Invitations during it">
            {(
              [
                ["new", "Decline new invitations"],
                ["all", "Decline new and existing"],
                ["none", "Don't decline"],
              ] as const
            ).map(([v, label]) => (
              <ListRow key={v} title={label} chevron={false} trailing={f.autoDecline === v ? <Check size={20} color={c.primary} accessibilityLabel="Selected" /> : undefined} onPress={() => set("autoDecline", v)} />
            ))}
          </ListGroup>
          {f.autoDecline !== "none" ? (
            <Field label="Message" info="Sent to organizers with the decline.">
              <Input value={f.declineMessage} onChangeText={(v) => set("declineMessage", v)} maxLength={500} placeholder={f.eventType === "focus" ? "In focus time; ask me after" : "Away until Monday"} />
            </Field>
          ) : null}
        </View>
      ) : null}
      <View style={{ gap: s[3] }}>
        <Switch value={f.allDay} onValueChange={(v) => set("allDay", v)} label="All day" />
        <Field label="Starts" error={error.time}>
          <CalendarDateTimeRow
            label="Starts"
            date={f.startDate}
            time={f.startTime}
            dateOnly={f.allDay}
            onChange={(d, t) => {
              const len = toMin(f.endTime) - toMin(f.startTime) + calendarDaysBetween(f.startDate, f.endDate) * 1440;
              const endTotal = toMin(t) + Math.max(len, 15);
              setF((x) => ({ ...x, startDate: d, startTime: t, endDate: calendarAddDays(d, Math.floor(endTotal / 1440)), endTime: hhmm(endTotal % 1440) }));
            }}
          />
        </Field>
        <Field label="Ends">
          <CalendarDateTimeRow label="Ends" date={f.endDate} time={f.endTime} dateOnly={f.allDay} onChange={(d, t) => setF((x) => ({ ...x, endDate: d, endTime: t }))} />
        </Field>
        {f.timezone !== zone ? (
          <Text role="label" tone="muted">
            Times in {f.timezone.replace(/_/g, " ")}
          </Text>
        ) : null}
      </View>
      {(!event || event.role === "owner") && (writable.length > 0 || appCalendars.length > 1) ? (
        <ListGroup>
          <ListRow
            title="Calendar"
            trailing={
              <Text role="label" tone="muted" numberOfLines={1} style={{ maxWidth: 200 }}>
                {calendarLabel(f.calendar)}
              </Text>
            }
            leading={targetCal ? <ProviderIcon provider={targetCal.source.provider ?? ""} product={targetCal.source.product ?? null} size={20} /> : undefined}
            onPress={() => setSheet("calendar")}
          />
        </ListGroup>
      ) : null}
      <ListGroup>
        <ListRow title={repeatLabel[f.repeat]} info={custom ? "Change custom repeats on the web." : undefined} onPress={custom ? undefined : () => setSheet("repeat")} />
        {block ? null : <ListRow title={f.guests.length === 0 ? "Add guests" : `${f.guests.length} ${f.guests.length === 1 ? "guest" : "guests"}`} onPress={() => setSheet("guests")} />}
        {block || target ? null : (
          <ListRow title={f.rooms.length === 0 ? "Add a room" : f.rooms.map((r) => r.name).join(", ")} leading={<DoorOpen size={20} color={c.textMuted} />} onPress={() => setSheet("rooms")} />
        )}
      </ListGroup>
      {!block && !f.allDay && f.guests.some((g) => g.kind === "user") ? (
        <Suggestions
          guests={f.guests.flatMap((g) => (g.kind === "user" ? [g.person.id] : []))}
          zone={f.timezone}
          date={f.startDate}
          minutes={Math.max(15, toMin(f.endTime) - toMin(f.startTime) + calendarDaysBetween(f.startDate, f.endDate) * 1440)}
          onPick={(st, en) => setF((x) => ({ ...x, startDate: calendarDateKey(st, x.timezone), startTime: hhmm(calendarMinutesOfDay(st, x.timezone)), endDate: calendarDateKey(en, x.timezone), endTime: hhmm(calendarMinutesOfDay(en, x.timezone)) }))}
        />
      ) : null}
      {block ? null : (
        <Field label="Location">
          <Input value={f.location} onChangeText={(v) => set("location", v)} maxLength={500} placeholder="Room, address or link" />
        </Field>
      )}
      {!block && (videoAvailable || providers.length > 0 || event?.conference_url) ? (
        <View style={{ gap: s[2] }}>
          <Switch
            value={f.conference === "add" || (!!event?.conference_url && f.conference !== "remove")}
            onValueChange={(v) => set("conference", v ? (event?.conference_url ? "keep" : "add") : event?.conference_url ? "remove" : "keep")}
            label="Video call"
            info="A meeting link is added to the event and the invitations. Google Meet, Teams and Zoom open in their own apps."
          />
          {f.conference === "add" && providers.length > 1 && provider ? (
            <ListGroup>
              <ListRow
                title="Video call with"
                trailing={
                  <Text role="label" tone="muted" numberOfLines={1} style={{ maxWidth: 200 }}>
                    {provider.account && !provider.native ? `${provider.label} · ${provider.account}` : provider.label}
                  </Text>
                }
                onPress={() => setSheet("provider")}
              />
            </ListGroup>
          ) : null}
        </View>
      ) : null}
      <Field label="Description">
        <TextArea value={f.description} onChangeText={(v) => set("description", v)} maxLength={8000} />
      </Field>
      {target ? null : (
      <Field label="Reminders">
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {REMINDERS.map((m) => {
            const on = f.reminders.includes(m);
            return (
              <Chip key={m} selected={on} onPress={() => set("reminders", on ? f.reminders.filter((x) => x !== m) : [...f.reminders, m].sort((a, b) => a - b).slice(0, 5))}>
                {calendarReminderLabel(m)}
              </Chip>
            );
          })}
        </View>
      </Field>
      )}

      <Sheet open={sheet === "calendar"} onClose={() => setSheet(null)} title="Calendar">
        <ListGroup>
          {[
            ...(appCalendars.length > 0 ? appCalendars.filter((x) => !event || x.mine || x.team_id).map((x) => `cal:${x.id}`) : ["app"]),
            ...(event ? [] : writable.map((w) => calKey({ connection_id: w.source.id, calendar_id: w.calendar.id }))),
          ].map((k) => (
            <ListRow
              key={k}
              title={calendarLabel(k)}
              chevron={false}
              trailing={f.calendar === k ? <Check size={20} color={c.primary} accessibilityLabel="Selected" /> : undefined}
              onPress={() => {
                setF((x) => ({ ...x, calendar: k, provider: "" }));
                setSheet(null);
              }}
            />
          ))}
        </ListGroup>
      </Sheet>
      <Sheet open={sheet === "provider"} onClose={() => setSheet(null)} title="Video call with">
        <ListGroup>
          {providers.map((o) => {
            const k = `${o.id}|${o.connection_id ?? ""}`;
            return (
              <ListRow
                key={k}
                title={o.account && !o.native ? `${o.label} · ${o.account}` : o.label}
                chevron={false}
                trailing={provider && `${provider.id}|${provider.connection_id ?? ""}` === k ? <Check size={20} color={c.primary} accessibilityLabel="Selected" /> : undefined}
                onPress={() => {
                  set("provider", k);
                  setSheet(null);
                }}
              />
            );
          })}
        </ListGroup>
      </Sheet>

      <Sheet open={sheet === "repeat"} onClose={() => setSheet(null)} title="Repeat">
        <ListGroup>
          {(["none", "daily", "weekdays", "weekly", "monthly", "yearly"] as const).map((r) => (
            <ListRow
              key={r}
              title={repeatLabel[r]}
              chevron={false}
              trailing={f.repeat === r ? <Check size={20} color={c.primary} accessibilityLabel="Selected" /> : undefined}
              onPress={() => {
                set("repeat", r);
                setSheet(null);
              }}
            />
          ))}
        </ListGroup>
      </Sheet>
      <GuestsSheet open={sheet === "guests"} onClose={() => setSheet(null)} guests={f.guests} onChange={(g) => set("guests", g)} />
      <RoomsSheet
        open={sheet === "rooms"}
        onClose={() => setSheet(null)}
        rooms={f.rooms}
        onChange={(r) => set("rooms", r)}
        from={f.allDay ? null : calendarFromWall(f.startDate, toMin(f.startTime), f.timezone)}
        to={f.allDay ? null : calendarFromWall(f.endDate, toMin(f.endTime), f.timezone)}
        {...(event ? { eventId: event.event_id } : {})}
      />
      <Sheet open={sheet === "scope"} onClose={() => setSheet(null)} title="Save recurring event">
        <ListGroup>
          {(
            [
              ["this", "This event"],
              ["following", "This and following events"],
              ["all", "All events"],
            ] as const
          )
            .filter(([scope]) => scope !== "this" || (f.guests.length === (event?.attendees.length ?? 0) && f.repeat === repeatOf(event, f.startDate)))
            .map(([scope, label]) => (
              <ListRow
                key={scope}
                title={label}
                chevron={false}
                onPress={() => {
                  setSheet(null);
                  void save(scope);
                }}
              />
            ))}
        </ListGroup>
      </Sheet>
    </Screen>
  );
}

function GuestsSheet({ open, onClose, guests, onChange }: { open: boolean; onClose: () => void; guests: Guest[]; onChange: (g: Guest[]) => void }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const [q, setQ] = useState("");
  const debounced = useDebouncedValue(q.trim(), 200);
  const people = useCalendarPeople(debounced, { enabled: open });
  const chosen = useMemo(() => new Set(guests.map((g) => (g.kind === "user" ? g.person.id : g.email))), [guests]);
  const results = (people.data?.items ?? []).filter((p) => !chosen.has(p.id)).slice(0, 8);
  const email = q.trim().toLowerCase();
  return (
    <Sheet open={open} onClose={onClose} onDismissed={() => setQ("")} title="Guests" snapPoints={["85%"]}>
      <View style={{ gap: s[4] }}>
        <Input value={q} onChangeText={setQ} placeholder="Search people or type an email" leadingIcon={Search} autoCapitalize="none" autoCorrect={false} keyboardType="email-address" accessibilityLabel="Search guests" />
        {EMAIL.test(email) && !chosen.has(email) ? (
          <Button
            variant="secondary"
            leftIcon={Mail}
            onPress={() => {
              onChange([...guests, { kind: "email", email }]);
              setQ("");
            }}
          >
            {`Invite ${email}`}
          </Button>
        ) : null}
        {results.length > 0 ? (
          <ListGroup title={`People in your ${orgsNoun("many")}`}>
            {results.map((p) => (
              <ListRow
                key={p.id}
                title={p.display_name}
                leading={<Avatar name={p.display_name} src={p.avatar_url} size="sm" />}
                trailing={<Plus size={20} color={c.primary} />}
                chevron={false}
                onPress={() => {
                  onChange([...guests, { kind: "user", person: p }]);
                  setQ("");
                }}
              />
            ))}
          </ListGroup>
        ) : null}
        {guests.length > 0 ? (
          <ListGroup title="Invited">
            {guests.map((g) => (
              <ListRow
                key={g.kind === "user" ? g.person.id : g.email}
                title={g.kind === "user" ? g.person.display_name : g.email}
                leading={g.kind === "user" ? <Avatar name={g.person.display_name} src={g.person.avatar_url} size="sm" /> : <Mail size={20} color={c.textMuted} />}
                trailing={<IconButton label="Remove" icon={X} onPress={() => onChange(guests.filter((x) => x !== g))} />}
                chevron={false}
              />
            ))}
          </ListGroup>
        ) : null}
      </View>
    </Sheet>
  );
}

/** Simplified find a time: when the guests (and you) are all free in everyone's working hours. */
function Suggestions({ guests, zone, date, minutes, onPick }: { guests: string[]; zone: string; date: string; minutes: number; onPick: (s: string, e: string) => void }) {
  const theme = useTheme();
  const s = theme.space;
  const from = useMemo(() => new Date(Math.max(Math.ceil(Date.now() / 900_000) * 900_000, new Date(calendarDayStart(date, zone)).getTime())).toISOString(), [date, zone]);
  const find = useCalendarFindTime({ user_ids: guests, from, to: calendarDayStart(calendarAddDays(date, 7), zone), duration: minutes });
  if (find.isPending) return <Skeleton height={theme.tap.sm} />;
  if (find.isError) return <Text role="caption" tone="muted">{errorMessage(find.error, "Couldn't find free times.")}</Text>;
  const slots = find.data.slots.slice(0, 6);
  return (
    <View style={{ gap: s[2] }}>
      <Text role="label" tone="muted">
        {slots.length > 0 ? "Everyone's free" : "No time this week when everyone's free"}
      </Text>
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
        {slots.map((sl) => (
          <Chip key={sl.starts_at} size="sm" icon={Plus} onPress={() => onPick(sl.starts_at, sl.ends_at)}>
            {`${formatCalendarDay(calendarDateKey(sl.starts_at, zone))} ${formatCalendarTime(sl.starts_at, zone)}`}
          </Chip>
        ))}
      </View>
    </View>
  );
}

/** Rooms and resources of the workspace, free or busy at the event's time; tap to book or drop one. */
function RoomsSheet({ open, onClose, rooms, onChange, from, to, eventId }: { open: boolean; onClose: () => void; rooms: CalendarEventRoom[]; onChange: (r: CalendarEventRoom[]) => void; from: string | null; to: string | null; eventId?: string }) {
  const theme = useTheme();
  const c = theme.colors;
  const { org } = useActiveOrg();
  const list = useCalendarRooms(open && org && from && to ? { org_id: org.id, from, to, ...(eventId ? { exclude_event: eventId } : {}) } : null);
  const picked = new Set(rooms.map((r) => r.id));
  return (
    <Sheet open={open} onClose={onClose} title="Rooms and resources" snapPoints={["75%"]}>
      {!org ? (
        <EmptyState icon={DoorOpen} title={`Rooms belong to a ${orgsNoun()}`} />
      ) : !from || !to ? (
        <EmptyState icon={DoorOpen} title="Pick a start and end time first" />
      ) : list.isPending ? (
        <View style={{ gap: theme.space[2] }}>
          <Skeleton height={theme.tap.lg} />
          <Skeleton height={theme.tap.lg} />
        </View>
      ) : list.isError ? (
        <ErrorState title="Couldn't load rooms" info={errorMessage(list.error)} onRetry={() => void list.refetch()} />
      ) : list.data.items.length === 0 ? (
        <EmptyState icon={DoorOpen} title="No rooms yet" />
      ) : (
        <ListGroup>
          {list.data.items.map((r) => {
            const on = picked.has(r.id);
            const usable = on || (r.can_book && r.free !== false);
            return (
              <ListRow
                key={r.id}
                title={r.name}
                {...([r.building, r.capacity ? `${r.capacity} people` : ""].filter(Boolean).length ? { info: [r.building, r.capacity ? `${r.capacity} people` : ""].filter(Boolean).join(" · ") } : {})}
                chevron={false}
                disabled={!usable}
                trailing={
                  on ? (
                    <Check size={20} color={c.primary} accessibilityLabel="Booked" />
                  ) : (
                    <Text role="label" tone={usable ? "muted" : "subtle"}>
                      {!r.can_book ? "Can't book" : r.free === false ? "Busy then" : "Free"}
                    </Text>
                  )
                }
                onPress={() => onChange(on ? rooms.filter((x) => x.id !== r.id) : [...rooms, { id: r.id, name: r.name, kind: r.kind, building: r.building, capacity: r.capacity }])}
              />
            );
          })}
        </ListGroup>
      )}
    </Sheet>
  );
}
