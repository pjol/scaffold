"use client";

import {
  CALENDAR_COLORS,
  calendarReminderLabel,
  errorMessage,
  useAddCalendarSubscription,
  useCalendarPersonalFeed,
  useCalendarSettings,
  useCalendarSubscriptions,
  useDisableCalendarPersonalFeed,
  useRefreshCalendarSubscription,
  useRemoveCalendarSubscription,
  useRotateCalendarPersonalFeed,
  useUpdateCalendarSettings,
  useUpdateCalendarSubscription,
  type CalendarColor,
  type CalendarHoursInterval,
  type CalendarSubscription,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, Chip, ConfirmDialog, ErrorState, Field, IconButton, Input, ListRow, Select, Skeleton, SkeletonGroup, Switch, toast } from "@scaffoldapp/ui-web";
import { CalendarSync, Copy, RefreshCw, Trash2, X } from "lucide-react";
import { useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate, formatRelative } from "@/lib/format";
import { HoursEditor } from "./HoursEditor";
import { CALENDAR_COLOR_NAMES, ColorDot, ZoneSelect, copyText } from "./parts";

const REMINDER_CHOICES = [0, 5, 10, 15, 30, 60, 120, 1440];

/** Settings → Calendars: zone, week, working hours, default reminders, other calendars, and the iCal feed. */
export function CalendarSettingsScreen() {
  const settings = useCalendarSettings();
  const update = useUpdateCalendarSettings();
  const [hours, setHours] = useState<CalendarHoursInterval[] | null>(null);

  const save = (body: Parameters<typeof update.mutate>[0], done = "Saved") =>
    update.mutate(body, { onSuccess: () => toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) });

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Calendars" />
      {settings.isPending ? (
        <SkeletonGroup label="Loading calendar settings" className="flex flex-col gap-4">
          <Skeleton className="h-10 max-w-form" />
          <Skeleton className="h-64 max-w-form" />
        </SkeletonGroup>
      ) : settings.isError ? (
        <Card>
          <ErrorState compact title="Couldn't load calendar settings" info={settings.error.message} onRetry={() => void settings.refetch()} retrying={settings.isFetching} />
        </Card>
      ) : (
        <>
          <Section title="Time zone" info="Your calendar, reminders and working hours use this zone. Repeating events keep their local time across daylight-saving changes.">
            <div className="grid max-w-form gap-4 sm:grid-cols-2">
              <Field label="Zone">
                <ZoneSelect value={settings.data.timezone} onChange={(z) => save({ timezone: z }, "Time zone saved")} />
              </Field>
              <Field label="Week starts on">
                <Select
                  value={String(settings.data.week_start)}
                  onValueChange={(v) => save({ week_start: Number(v) })}
                  options={[
                    { value: "1", label: "Monday" },
                    { value: "0", label: "Sunday" },
                    { value: "6", label: "Saturday" },
                  ]}
                />
              </Field>
            </div>
          </Section>

          <Section title="Working hours" info="Used when finding times with colleagues, and as the starting hours of new booking links.">
            <div className="flex max-w-form flex-col gap-4">
              <HoursEditor value={hours ?? settings.data.working_hours} onChange={setHours} weekStart={settings.data.week_start} />
              {hours ? (
                <div className="flex gap-2">
                  <Button size="auto" loading={update.isPending} onClick={() => update.mutate({ working_hours: hours }, { onSuccess: () => { setHours(null); toast.success("Working hours saved"); }, onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) })}>
                    Save hours
                  </Button>
                  <Button variant="ghost" size="auto" onClick={() => setHours(null)}>
                    Discard
                  </Button>
                </div>
              ) : null}
            </div>
          </Section>

          <Section title="Default reminders" info="Added to new timed events. Change them per event.">
            <div className="flex max-w-form flex-wrap items-center gap-2">
              {settings.data.default_reminders.length === 0 ? <span className="type-label text-fg-muted">No reminders</span> : null}
              {settings.data.default_reminders.map((m) => (
                <Chip key={m} icon={<X />} aria-label={`Remove ${calendarReminderLabel(m)}`} onClick={() => save({ default_reminders: settings.data.default_reminders.filter((x) => x !== m) })}>
                  {calendarReminderLabel(m)}
                </Chip>
              ))}
              {settings.data.default_reminders.length < 5 ? (
                <Select
                  value=""
                  placeholder="Add reminder"
                  onValueChange={(v) => save({ default_reminders: [...settings.data.default_reminders, Number(v)] })}
                  options={REMINDER_CHOICES.filter((m) => !settings.data.default_reminders.includes(m)).map((m) => ({ value: String(m), label: calendarReminderLabel(m) }))}
                />
              ) : null}
            </div>
          </Section>

          <OtherCalendars available={settings.data.subscriptions_available} />
          <PersonalFeed />
        </>
      )}
    </div>
  );
}

function SubscriptionRow({ sub }: { sub: CalendarSubscription }) {
  const update = useUpdateCalendarSubscription();
  const refresh = useRefreshCalendarSubscription();
  const remove = useRemoveCalendarSubscription();
  const [confirm, setConfirm] = useState(false);
  const status = sub.last_error
    ? sub.last_error
    : sub.last_success_at
      ? `Updated ${formatRelative(sub.last_success_at)} · ${sub.busy_count} busy ${sub.busy_count === 1 ? "block" : "blocks"} ahead`
      : "Not read yet";
  return (
    <li className="flex flex-col gap-2 border-b border-border py-3 last:border-b-0 sm:flex-row sm:items-center">
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <ColorDot color={sub.color} className="mt-2" />
        <div className="flex min-w-0 flex-col">
          <span className="truncate type-body font-semibold text-fg">{sub.name}</span>
          <span className="truncate type-caption text-fg-muted">{sub.host}</span>
          <span className={sub.last_error ? "type-caption text-danger-strong" : "type-caption text-fg-muted"}>{status}</span>
        </div>
      </div>
      <div className="flex items-center gap-2 self-end sm:self-center">
        <Switch
          checked={sub.enabled}
          onCheckedChange={(v) => update.mutate({ id: sub.id, enabled: v }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) })}
          label={<span className="sr-only">Use {sub.name} for busy time</span>}
        />
        <IconButton
          label={`Refresh ${sub.name}`}
          icon={<RefreshCw />}
          size="sm"
          variant="ghost"
          loading={refresh.isPending}
          onClick={() => refresh.mutate(sub.id, { onSuccess: (s) => (s.last_error ? toast.error(s.last_error) : toast.success("Calendar refreshed")), onError: (e) => toast.error(errorMessage(e, "Couldn't refresh.")) })}
        />
        <IconButton label={`Remove ${sub.name}`} icon={<Trash2 />} size="sm" variant="ghost" onClick={() => setConfirm(true)} />
      </div>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={`Remove “${sub.name}”?`}
        tone="danger"
        confirmLabel="Remove"
        onConfirm={async () => {
          await remove.mutateAsync(sub.id);
          toast.success("Calendar removed");
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove it. Try again.")}
      >
        <p className="type-body text-fg">Its busy time stops counting. Nothing changes in the other calendar.</p>
      </ConfirmDialog>
    </li>
  );
}

function OtherCalendars({ available }: { available: boolean }) {
  const subs = useCalendarSubscriptions();
  const add = useAddCalendarSubscription();
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [color, setColor] = useState<CalendarColor>("neutral");
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    setError(null);
    if (!name.trim() || !url.trim()) {
      setError("Enter a name and the calendar's secret address.");
      return;
    }
    try {
      await add.mutateAsync({ name: name.trim(), url: url.trim(), color });
      setName("");
      setUrl("");
      toast.success("Calendar added");
    } catch (e) {
      setError(errorMessage(e, "Couldn't add that calendar."));
    }
  };
  return (
    <Section
      title="Other calendars"
      info="Add a Google, Outlook or Apple calendar by its secret iCal address so its busy time blocks your booking links and shows on your calendar. Only times are kept, never titles. It refreshes every 30 minutes."
    >
      <div className="flex max-w-form flex-col gap-4">
        {!available ? <Banner tone="warning">Adding other calendars isn't set up in this app.</Banner> : null}
        {subs.isPending ? (
          <Skeleton className="h-16" />
        ) : subs.isError ? (
          <ErrorState compact title="Couldn't load your calendars" info={subs.error.message} onRetry={() => void subs.refetch()} />
        ) : subs.data.items.length > 0 ? (
          <ul className="flex flex-col">{subs.data.items.map((s) => <SubscriptionRow key={s.id} sub={s} />)}</ul>
        ) : null}
        {available ? (
          <form
            className="flex flex-col gap-4 rounded-md bg-sunken p-4"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            {error ? <Banner tone="danger">{error}</Banner> : null}
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder="Work" />
            </Field>
            <Field label="Secret iCal address" info="Google Calendar: Settings → your calendar → Secret address in iCal format. Outlook: Settings → Shared calendars → Publish a calendar → ICS.">
              <Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://…/basic.ics" inputMode="url" autoComplete="off" />
            </Field>
            <Field label="Color">
              <Select value={color} onValueChange={(v) => setColor(v as CalendarColor)} options={CALENDAR_COLORS.map((c) => ({ value: c, label: CALENDAR_COLOR_NAMES[c] }))} />
            </Field>
            <Button type="submit" size="auto" variant="secondary" loading={add.isPending} className="self-start">
              Add calendar
            </Button>
          </form>
        ) : null}
      </div>
    </Section>
  );
}

function PersonalFeed() {
  const feed = useCalendarPersonalFeed();
  const rotate = useRotateCalendarPersonalFeed();
  const disable = useDisableCalendarPersonalFeed();
  const [url, setUrl] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"replace" | "off" | null>(null);
  const create = async () => setUrl((await rotate.mutateAsync()).url);
  return (
    <Section
      title="Subscribe from another app"
      info="A private iCal link to your calendar for Google Calendar, Apple Calendar or Outlook. Anyone with the link can read your events, so keep it to yourself; replace it if it leaks."
    >
      {feed.isPending ? (
        <Skeleton className="h-14 max-w-form" />
      ) : feed.isError ? (
        <ErrorState compact title="Couldn't load your feed" info={feed.error.message} onRetry={() => void feed.refetch()} />
      ) : (
        <div className="flex max-w-form flex-col gap-4">
          {url ? (
            <div className="flex flex-col gap-3">
              <Banner tone="info">Copy this link now. It's shown once; replace it to get a new one.</Banner>
              <Input readOnly value={url} aria-label="Calendar feed link" onFocus={(e) => e.currentTarget.select()} trailing={<IconButton label="Copy link" icon={<Copy />} size="sm" onClick={() => void copyText(url, toast)} />} />
            </div>
          ) : (
            <Card padding="none" className="p-1">
              <ListRow title={feed.data.active ? `Active since ${formatDate(feed.data.created_at ?? "")}` : "No feed link yet"} leading={<CalendarSync className="size-5 text-fg-muted" aria-hidden />} />
            </Card>
          )}
          <div className="flex flex-wrap gap-2">
            {feed.data.active ? (
              <>
                <Button variant="secondary" size="auto" onClick={() => setConfirm("replace")}>
                  Replace link
                </Button>
                <Button variant="ghost" size="auto" onClick={() => setConfirm("off")}>
                  Turn off
                </Button>
              </>
            ) : (
              <Button size="auto" loading={rotate.isPending} onClick={() => void create().catch((e: unknown) => toast.error(errorMessage(e, "Couldn't create the link. Try again.")))}>
                Create feed link
              </Button>
            )}
          </div>
        </div>
      )}
      <ConfirmDialog open={confirm === "replace"} onOpenChange={(o) => setConfirm(o ? "replace" : null)} title="Replace your feed link?" confirmLabel="Replace link" onConfirm={create} errorText={(e) => errorMessage(e, "Couldn't replace the link.")}>
        <p className="type-body text-fg">The old link stops working. Calendars subscribed to it stop updating.</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "off"}
        onOpenChange={(o) => setConfirm(o ? "off" : null)}
        title="Turn off your feed?"
        tone="danger"
        confirmLabel="Turn off"
        onConfirm={async () => {
          await disable.mutateAsync();
          setUrl(null);
          toast.success("Feed turned off");
        }}
        errorText={(e) => errorMessage(e, "Couldn't turn it off.")}
      >
        <p className="type-body text-fg">The link stops working. Calendars subscribed to it stop updating.</p>
      </ConfirmDialog>
    </Section>
  );
}
