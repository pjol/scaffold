"use client";

import {
  CALENDAR_COLORS,
  calendarDeviceZone,
  calendarStartOfMonth,
  calendarTodayKey,
  errorMessage,
  formatCalendarDuration,
  isApiError,
  orgsNounA,
  orgsNounTitle,
  useCalendarLink,
  useCalendarLinkPreview,
  useCalendarSettings,
  useCreateCalendarLink,
  useDebouncedValue,
  useOrgs,
  useUpdateCalendarLink,
  type CalendarBookingLink,
  type CalendarBookingLinkInput,
  type CalendarQuestion,
  type CalendarSlot,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, Checkbox, EmptyState, ErrorState, Field, IconButton, Input, SegmentedControl, Select, Skeleton, SkeletonGroup, Switch, Tab, Tabs, TabsList, TextArea, ToggleChip, cn, toast, useMediaQuery } from "@scaffoldapp/ui-web";
import { Check, ChevronLeft, Copy, Link2, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { HoursEditor, OverridesEditor } from "./HoursEditor";
import { CALENDAR_COLOR_CLASSES, CALENDAR_COLOR_NAMES, ZoneSelect, copyText } from "./parts";
import { LinkHeader } from "./PublicBookingScreen";
import { SlotPicker, monthWindow } from "./SlotPicker";

type Draft = Required<Omit<CalendarBookingLinkInput, "org_id" | "max_per_day">> & { org_id: string | null; max_per_day: number | null };

const DURATIONS = [15, 20, 30, 45, 60, 90, 120];
const BUFFERS = [0, 5, 10, 15, 30, 45, 60];
const NOTICE = [0, 30, 60, 120, 240, 720, 1440, 2880, 10080];
const INTERVALS = [10, 15, 20, 30, 45, 60, 90, 120];
const CUTOFFS = [0, 30, 60, 120, 240, 720, 1440, 2880];

const minutesText = (m: number) => (m === 0 ? "None" : m % 1440 === 0 ? `${m / 1440} day${m === 1440 ? "" : "s"}` : m % 60 === 0 ? `${m / 60} hour${m === 60 ? "" : "s"}` : `${m} minutes`);

function draftFrom(l: CalendarBookingLink | null, zone: string, hours: Draft["weekly_hours"]): Draft {
  return {
    title: l?.title ?? "",
    description: l?.description ?? "",
    slug: l?.slug ?? "",
    audience: l?.audience ?? "external",
    org_id: l?.org_id ?? null,
    durations: l?.durations ?? [30],
    location_type: l?.location_type ?? "phone",
    location_value: l?.location_value ?? "",
    timezone: l?.timezone ?? zone,
    weekly_hours: l?.weekly_hours ?? hours,
    date_overrides: l?.date_overrides ?? [],
    buffer_before: l?.buffer_before ?? 0,
    buffer_after: l?.buffer_after ?? 0,
    min_notice: l?.min_notice ?? 240,
    max_days_ahead: l?.max_days_ahead ?? 60,
    max_per_day: l?.max_per_day ?? null,
    slot_interval: l?.slot_interval ?? 30,
    questions: l?.questions ?? [],
    confirmation_message: l?.confirmation_message ?? "",
    color: l?.color ?? "primary",
    active: l?.active ?? true,
    allow_reschedule: l?.allow_reschedule ?? true,
    allow_cancel: l?.allow_cancel ?? true,
    manage_cutoff: l?.manage_cutoff ?? 60,
  };
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-4">
      <h2 className="type-title-sm text-fg">{title}</h2>
      {children}
    </section>
  );
}

/** /calendar/links/new and /calendar/links/<id>: the link's settings beside a live preview of its booking page. */
export function LinkEditorScreen({ id }: { id?: string }) {
  const existing = useCalendarLink(id);
  const settings = useCalendarSettings();
  if (id && existing.isPending) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <SkeletonGroup label="Loading booking link" className="flex flex-col gap-4">
          <Skeleton shape="text" className="h-8 w-1/3" />
          <Skeleton className="h-96" />
        </SkeletonGroup>
      </div>
    );
  }
  if (id && existing.isError) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <Card>
          {isApiError(existing.error) && existing.error.status === 404 ? (
            <EmptyState icon={<Link2 />} title="This booking link doesn't exist" action={<Button asChild variant="secondary" size="auto"><Link href="/calendar/links">All booking links</Link></Button>} />
          ) : (
            <ErrorState title="Couldn't load the link" info={existing.error.message} onRetry={() => void existing.refetch()} retrying={existing.isFetching} />
          )}
        </Card>
      </div>
    );
  }
  if (settings.isPending) return <Skeleton className="m-gutter h-96" />;
  const zone = settings.data?.timezone_set ? settings.data.timezone : calendarDeviceZone();
  return <LinkEditor key={id ?? "new"} link={existing.data ?? null} zone={zone} hours={settings.data?.working_hours ?? []} videoAvailable={settings.data?.video_available ?? false} />;
}

function LinkEditor({ link, zone, hours, videoAvailable }: { link: CalendarBookingLink | null; zone: string; hours: Draft["weekly_hours"]; videoAvailable: boolean }) {
  const router = useRouter();
  const [d, setD] = useState<Draft>(() => draftFrom(link, zone, hours));
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<"settings" | "preview">("settings");
  const wide = useMediaQuery("(min-width: 1024px)");
  const create = useCreateCalendarLink();
  const update = useUpdateCalendarLink();
  const orgs = useOrgs();
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((s) => ({ ...s, [k]: v }));
  const orgList = orgs.data?.items ?? [];

  useEffect(() => {
    if (d.audience === "internal" && !d.org_id && orgList[0]) set("org_id", orgList[0].id);
  }, [d.audience, d.org_id, orgList]);

  const body = (): CalendarBookingLinkInput => ({ ...d, slug: d.slug.trim() || undefined, org_id: d.audience === "internal" ? d.org_id : null }) as CalendarBookingLinkInput;

  const save = async () => {
    setError(null);
    if (!d.title.trim()) {
      setError("Add a title.");
      return;
    }
    try {
      if (link) {
        await update.mutateAsync({ id: link.id, body: body() });
        toast.success("Link saved");
      } else {
        const created = await create.mutateAsync(body());
        toast.success("Link created");
        router.replace(`/calendar/links/${created.id}`);
      }
    } catch (e) {
      setError(errorMessage(e, "Couldn't save the link. Try again."));
    }
  };

  const form = (
    <div className="flex flex-col gap-section">
      {error ? <Banner tone="danger">{error}</Banner> : null}
      <Group title="Basics">
        <Field label="Title" required>
          <Input value={d.title} onChange={(e) => set("title", e.target.value)} maxLength={200} placeholder="30-minute intro" />
        </Field>
        <Field label="Link name" info="The end of the booking page address. Lowercase letters, numbers and dashes. Leave empty to make one from the title.">
          <div className="flex items-center gap-2">
            <span className="shrink-0 type-label text-fg-muted" aria-hidden>
              /book/
            </span>
            <Input value={d.slug} onChange={(e) => set("slug", e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))} maxLength={60} placeholder="made from the title" />
          </div>
        </Field>
        <Field label="Description">
          <TextArea value={d.description} onChange={(e) => set("description", e.target.value)} maxLength={2000} minRows={2} />
        </Field>
        <Field label="Meeting lengths" info="Offer more than one and the booker picks.">
          <div className="flex flex-wrap gap-2" role="group" aria-label="Meeting lengths">
            {DURATIONS.map((m) => (
              <ToggleChip
                key={m}
                pressed={d.durations.includes(m)}
                onPressedChange={(on) => set("durations", on ? [...d.durations, m].sort((a, b) => a - b) : d.durations.length > 1 ? d.durations.filter((x) => x !== m) : d.durations)}
              >
                {formatCalendarDuration(m)}
              </ToggleChip>
            ))}
          </div>
        </Field>
        <Field label="Where">
          <Select
            value={d.location_type}
            onValueChange={(v) => set("location_type", v as Draft["location_type"])}
            options={[
              ...(videoAvailable || d.location_type === "video" ? [{ value: "video", label: "Video call (link made per booking)" }] : []),
              { value: "phone", label: "Phone call" },
              { value: "in_person", label: "In person" },
              { value: "custom", label: "Other" },
            ]}
          />
        </Field>
        {d.location_type === "phone" ? (
          <Field label="Your phone number" info="Leave empty and the booker gives a number for you to call.">
            <Input type="tel" value={d.location_value} onChange={(e) => set("location_value", e.target.value)} maxLength={40} />
          </Field>
        ) : d.location_type === "in_person" || d.location_type === "custom" ? (
          <Field label={d.location_type === "in_person" ? "Address" : "Details"} required>
            <Input value={d.location_value} onChange={(e) => set("location_value", e.target.value)} maxLength={500} />
          </Field>
        ) : null}
      </Group>

      <Group title="Who can book">
        <SegmentedControl
          value={d.audience}
          onValueChange={(v) => set("audience", v)}
          options={[
            { value: "external", label: "Anyone with the link" },
            { value: "internal", label: `Members of ${orgsNounA()}` },
          ]}
          aria-label="Who can book"
        />
        {d.audience === "internal" ? (
          orgList.length === 0 ? (
            <Banner tone="warning">{`You aren't in ${orgsNounA()} yet. Join or create one in Settings → ${orgsNounTitle("many")}.`}</Banner>
          ) : (
            <Field label={orgsNounTitle()} info="Only signed-in members can see times and book. Everyone else sees that the link is for members.">
              <Select value={d.org_id ?? ""} onValueChange={(v) => set("org_id", v)} options={orgList.map((o) => ({ value: o.id, label: o.name }))} />
            </Field>
          )
        ) : null}
      </Group>

      <Group title="Availability">
        <Field label="Time zone" info="Your hours below are in this zone; bookers see times in their own.">
          <ZoneSelect value={d.timezone} onChange={(z) => set("timezone", z)} />
        </Field>
        <HoursEditor value={d.weekly_hours} onChange={(v) => set("weekly_hours", v)} />
        <Field label="Date overrides" info="A day off or different hours on a date. Overrides replace the weekly hours for that date.">
          <OverridesEditor value={d.date_overrides} onChange={(v) => set("date_overrides", v)} zone={d.timezone} />
        </Field>
      </Group>

      <Group title="Limits">
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Buffer before" info="Free time kept before each booking.">
            <Select value={String(d.buffer_before)} onValueChange={(v) => set("buffer_before", Number(v))} options={BUFFERS.map((m) => ({ value: String(m), label: minutesText(m) }))} />
          </Field>
          <Field label="Buffer after">
            <Select value={String(d.buffer_after)} onValueChange={(v) => set("buffer_after", Number(v))} options={BUFFERS.map((m) => ({ value: String(m), label: minutesText(m) }))} />
          </Field>
          <Field label="Minimum notice" info="How soon before a start people can book.">
            <Select value={String(d.min_notice)} onValueChange={(v) => set("min_notice", Number(v))} options={NOTICE.map((m) => ({ value: String(m), label: minutesText(m) }))} />
          </Field>
          <Field label="Days ahead" info="How far into the future people can book.">
            <Input type="number" min={1} max={365} value={d.max_days_ahead} onChange={(e) => set("max_days_ahead", Math.max(1, Math.min(365, Number(e.target.value) || 1)))} />
          </Field>
          <Field label="Bookings per day" info="Leave empty for no limit.">
            <Input type="number" min={1} max={50} value={d.max_per_day ?? ""} onChange={(e) => set("max_per_day", e.target.value ? Math.max(1, Math.min(50, Number(e.target.value))) : null)} />
          </Field>
          <Field label="Start times every">
            <Select value={String(d.slot_interval)} onValueChange={(v) => set("slot_interval", Number(v))} options={INTERVALS.map((m) => ({ value: String(m), label: minutesText(m) }))} />
          </Field>
        </div>
      </Group>

      <Group title="Booking form">
        <p className="type-label text-fg-muted">Everyone gives a name and email. Add up to 10 questions.</p>
        <QuestionsEditor value={d.questions} onChange={(v) => set("questions", v)} />
      </Group>

      <Group title="After booking">
        <Field label="Confirmation message" info="Shown on the confirmation page and in the confirmation email.">
          <TextArea value={d.confirmation_message} onChange={(e) => set("confirmation_message", e.target.value)} maxLength={1000} minRows={2} />
        </Field>
        <div className="flex flex-col gap-3">
          <Switch checked={d.allow_reschedule} onCheckedChange={(v) => set("allow_reschedule", v)} label="Bookers can reschedule" />
          <Switch checked={d.allow_cancel} onCheckedChange={(v) => set("allow_cancel", v)} label="Bookers can cancel" />
          {d.allow_reschedule || d.allow_cancel ? (
            <Field label="Changes close">
              <Select value={String(d.manage_cutoff)} onValueChange={(v) => set("manage_cutoff", Number(v))} options={CUTOFFS.map((m) => ({ value: String(m), label: m === 0 ? "At the start" : `${minutesText(m)} before` }))} />
            </Field>
          ) : null}
        </div>
        <Field label="Color on your calendar">
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Color">
            {CALENDAR_COLORS.map((c) => (
              <button
                key={c}
                type="button"
                role="radio"
                aria-checked={d.color === c}
                aria-label={CALENDAR_COLOR_NAMES[c]}
                onClick={() => set("color", c)}
                className={cn("flex size-10 items-center justify-center rounded-pill focus-visible:outline-2 focus-visible:outline-ring", CALENDAR_COLOR_CLASSES[c].chip, d.color === c && "ring-2 ring-offset-2 ring-offset-surface " + CALENDAR_COLOR_CLASSES[c].ring)}
              >
                {d.color === c ? <Check className="size-4" aria-hidden /> : null}
              </button>
            ))}
          </div>
        </Field>
        <Switch checked={d.active} onCheckedChange={(v) => set("active", v)} label="Accept bookings" />
      </Group>
    </div>
  );

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <Link href="/calendar/links" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Booking links
      </Link>
      <div className="flex min-h-control-lg items-center gap-3">
        <h1 className="min-w-0 flex-1 truncate type-title text-fg">{link ? link.title : "New booking link"}</h1>
        {link ? <IconButton label="Copy link" icon={<Copy />} variant="ghost" onClick={() => void copyText(link.url, toast)} /> : null}
        {link ? (
          <ResourceMenu
            feature="calendar"
            kind="calendar.booking_link"
            id={link.id}
            title={link.title}
            actions={[{ id: "open", label: "Open booking page", icon: Link2, onSelect: () => window.open(link.url, "_blank", "noopener") }]}
          />
        ) : null}
        <Button size="auto" loading={create.isPending || update.isPending} onClick={() => void save()}>
          {link ? "Save" : "Create link"}
        </Button>
      </div>
      {!wide ? (
        <Tabs value={tab} onValueChange={(v) => setTab(v as "settings" | "preview")}>
          <TabsList aria-label="Editor">
            <Tab value="settings">Settings</Tab>
            <Tab value="preview">Preview</Tab>
          </TabsList>
        </Tabs>
      ) : null}
      <div className="grid gap-section lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        {wide || tab === "settings" ? form : null}
        {wide || tab === "preview" ? (
          <div className="lg:sticky lg:top-24 lg:self-start">
            <Preview draft={d} linkId={link?.id ?? null} zone={zone} />
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** The booking page as visitors will see it, with slots computed from your real calendar. */
function Preview({ draft, linkId, zone }: { draft: Draft; linkId: string | null; zone: string }) {
  const [month, setMonth] = useState(() => calendarStartOfMonth(calendarTodayKey(zone)));
  const [duration, setDuration] = useState(draft.durations[0] ?? 30);
  const [slot, setSlot] = useState<CalendarSlot | null>(null);
  const dur = draft.durations.includes(duration) ? duration : (draft.durations[0] ?? 30);
  const win = useMemo(() => monthWindow(month, zone, 0), [month, zone]);
  const debounced = useDebouncedValue(draft, 400);
  const body = useMemo(
    () => ({ link: { ...debounced, slug: debounced.slug || undefined, org_id: debounced.audience === "internal" ? debounced.org_id : null } as CalendarBookingLinkInput, link_id: linkId, from: win.from, to: win.to, duration: dur }),
    [debounced, linkId, win, dur],
  );
  const preview = useCalendarLinkPreview(body);
  const label = { video: "Video call", phone: draft.location_value ? `Phone call: you call ${draft.location_value}` : "Phone call: you'll be called", in_person: "In person", custom: draft.location_value }[draft.location_type];
  return (
    <Card className="flex flex-col gap-6" aria-label="Preview">
      <span className="type-caption uppercase text-fg-subtle">Preview</span>
      <LinkHeader
        link={{ host: { id: "", display_name: "You", avatar_url: null }, title: draft.title || "Untitled link", durations: draft.durations, location_type: draft.location_type, location_label: label, description: draft.description, org_name: null, audience: draft.audience }}
        duration={dur}
        onDuration={setDuration}
      />
      <SlotPicker
        zone={zone}
        month={month}
        onMonthChange={setMonth}
        slots={preview.data?.slots ?? []}
        loading={preview.isFetching}
        error={preview.isError ? errorMessage(preview.error) : null}
        selected={slot?.starts_at ?? null}
        onSelect={setSlot}
      />
    </Card>
  );
}

function QuestionsEditor({ value, onChange }: { value: CalendarQuestion[]; onChange: (v: CalendarQuestion[]) => void }) {
  const update = (i: number, q: Partial<CalendarQuestion>) => onChange(value.map((x, j) => (j === i ? { ...x, ...q } : x)));
  return (
    <div className="flex flex-col gap-3">
      {value.map((q, i) => (
        <div key={q.id} className="flex flex-col gap-3 rounded-md bg-sunken p-4">
          <div className="flex items-end gap-2">
            <Field label={`Question ${i + 1}`} className="flex-1">
              <Input value={q.label} onChange={(e) => update(i, { label: e.target.value })} maxLength={200} />
            </Field>
            <IconButton label={`Remove question ${i + 1}`} icon={<Trash2 />} variant="ghost" size="sm" onClick={() => onChange(value.filter((_, j) => j !== i))} />
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Select
              value={q.type}
              onValueChange={(v) => update(i, { type: v as CalendarQuestion["type"], options: v === "choice" ? (q.options.length >= 2 ? q.options : ["", ""]) : [] })}
              options={[
                { value: "short_text", label: "Short answer" },
                { value: "long_text", label: "Paragraph" },
                { value: "phone", label: "Phone number" },
                { value: "choice", label: "One choice" },
              ]}
              className="w-44"
            />
            <Checkbox checked={q.required} onCheckedChange={(v) => update(i, { required: v })} label="Required" />
          </div>
          {q.type === "choice" ? (
            <div className="flex flex-col gap-2">
              {q.options.map((o, k) => (
                <div key={k} className="flex items-center gap-2">
                  <Input value={o} aria-label={`Option ${k + 1}`} onChange={(e) => update(i, { options: q.options.map((x, j) => (j === k ? e.target.value : x)) })} maxLength={100} />
                  {q.options.length > 2 ? <IconButton label={`Remove option ${k + 1}`} icon={<Trash2 />} size="sm" variant="ghost" onClick={() => update(i, { options: q.options.filter((_, j) => j !== k) })} /> : null}
                </div>
              ))}
              {q.options.length < 20 ? (
                <Button variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => update(i, { options: [...q.options, ""] })}>
                  Add option
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      ))}
      {value.length < 10 ? (
        <Button
          variant="secondary"
          size="sm"
          leftIcon={<Plus />}
          className="self-start"
          onClick={() => onChange([...value, { id: Math.random().toString(36).slice(2, 10), label: "", type: "short_text", required: false, options: [] }])}
        >
          Add question
        </Button>
      ) : null}
    </div>
  );
}
