"use client";

import {
  deviceTimeZone,
  errorMessage,
  EVENT_COVER_PURPOSE,
  fromZonedInput,
  MAX_UPLOAD_BYTES,
  timeZoneOptions,
  toZonedInput,
  useCreateEvent,
  useEvent,
  useUpdateEvent,
  useUpload,
  zoneLabel,
  type EventItem,
  type EventRequest,
  type EventVisibility,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Field, Input, SegmentedControl, Select, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { CalendarX, ImagePlus, Lock, Trash } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { CoverImage, VISIBILITY_INFO, VISIBILITY_LABEL } from "./parts";

const COVER_TYPES = ["image/jpeg", "image/png", "image/webp", "image/heic"];
const VISIBILITIES: EventVisibility[] = ["members", "public", "invite"];

interface FormState {
  title: string;
  details: string;
  startDate: string;
  startTime: string;
  endDate: string;
  endTime: string;
  timezone: string;
  location: string;
  onlineUrl: string;
  coverKey: string | null;
  coverUrl: string | null;
  capacity: string;
  visibility: EventVisibility;
}

type Errors = Partial<Record<"title" | "start" | "end" | "onlineUrl" | "capacity", string>>;

function initialState(event: EventItem | undefined): FormState {
  if (event) {
    const s = toZonedInput(event.starts_at, event.timezone);
    const e = toZonedInput(event.ends_at, event.timezone);
    return {
      title: event.title,
      details: event.details,
      startDate: s.date,
      startTime: s.time,
      endDate: e.date,
      endTime: e.time,
      timezone: event.timezone,
      location: event.location_text,
      onlineUrl: event.online_url ?? "",
      coverKey: event.cover_key,
      coverUrl: event.cover_url,
      capacity: event.capacity ? String(event.capacity) : "",
      visibility: event.visibility,
    };
  }
  const tz = deviceTimeZone();
  const tomorrow = toZonedInput(new Date(Date.now() + 24 * 3600_000).toISOString(), tz);
  return {
    title: "",
    details: "",
    startDate: tomorrow.date,
    startTime: "18:00",
    endDate: tomorrow.date,
    endTime: "20:00",
    timezone: tz,
    location: "",
    onlineUrl: "",
    coverKey: null,
    coverUrl: null,
    capacity: "",
    visibility: "members",
  };
}

function zoneName(z: string): string {
  return z.replace(/_/g, " ");
}

function validate(f: FormState): { errors: Errors; startsAt: string | null; endsAt: string | null } {
  const errors: Errors = {};
  if (!f.title.trim()) errors.title = "Enter a title.";
  const startsAt = fromZonedInput(f.startDate, f.startTime, f.timezone);
  const endsAt = fromZonedInput(f.endDate, f.endTime, f.timezone);
  if (!startsAt) errors.start = "Pick a start date and time.";
  if (!endsAt) errors.end = "Pick an end date and time.";
  else if (startsAt && new Date(endsAt) <= new Date(startsAt)) errors.end = "End time must be after the start time.";
  const url = f.onlineUrl.trim();
  if (url && !/^https?:\/\/\S+\.\S+/.test(url)) errors.onlineUrl = "Enter a full link starting with https://.";
  if (f.capacity.trim()) {
    const n = Number(f.capacity);
    if (!Number.isInteger(n) || n < 1 || n > 100000) errors.capacity = "Enter a whole number from 1 to 100000, or leave it empty.";
  }
  return { errors, startsAt, endsAt };
}

function CoverField({ form, setForm }: { form: FormState; setForm: (fn: (f: FormState) => FormState) => void }) {
  const upload = useUpload();
  const input = useRef<HTMLInputElement>(null);
  const onFile = async (file: File | undefined) => {
    if (!file) return;
    if (!COVER_TYPES.includes(file.type)) {
      toast.error("Choose a JPEG, PNG, WebP or HEIC image.");
      return;
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      toast.error("That image is over 10 MB. Choose a smaller one.");
      return;
    }
    try {
      const r = await upload.mutateAsync({ file, purpose: EVENT_COVER_PURPOSE, maxBytes: MAX_UPLOAD_BYTES });
      setForm((f) => ({ ...f, coverKey: r.key, coverUrl: r.public_url }));
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't upload the image. Try again."));
    }
  };
  return (
    <Field label="Cover image" info="Shown at the top of the event. 16:9 works best.">
      <div className="flex flex-col gap-3">
        {form.coverUrl ? <CoverImage src={form.coverUrl} alt="" /> : null}
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="secondary" size="auto" leftIcon={<ImagePlus />} loading={upload.isPending} onClick={() => input.current?.click()}>
            {form.coverUrl ? "Change image" : "Add image"}
          </Button>
          {form.coverUrl ? (
            <Button type="button" variant="ghost" size="auto" leftIcon={<Trash />} disabled={upload.isPending} onClick={() => setForm((f) => ({ ...f, coverKey: null, coverUrl: null }))}>
              Remove
            </Button>
          ) : null}
        </div>
        <input
          ref={input}
          type="file"
          accept={COVER_TYPES.join(",")}
          hidden
          onChange={(e) => {
            void onFile(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
      </div>
    </Field>
  );
}

function Form({ event }: { event?: EventItem }) {
  const router = useRouter();
  const create = useCreateEvent();
  const update = useUpdateEvent();
  const [form, setFormState] = useState<FormState>(() => initialState(event));
  const [errors, setErrors] = useState<Errors>({});
  const setForm = (fn: (f: FormState) => FormState) => setFormState((f) => fn(f));
  const zones = useMemo(() => timeZoneOptions([form.timezone]), [form.timezone]);
  const zoneOptions = useMemo(() => {
    const now = new Date().toISOString();
    return zones.map((z) => ({ value: z, label: `${zoneName(z)} (${zoneLabel(now, z)})` }));
  }, [zones]);
  const busy = create.isPending || update.isPending;

  // Moving the start keeps the event's length.
  const setStart = (patch: Partial<Pick<FormState, "startDate" | "startTime">>) =>
    setForm((f) => {
      const next = { ...f, ...patch };
      const oldStart = fromZonedInput(f.startDate, f.startTime, f.timezone);
      const oldEnd = fromZonedInput(f.endDate, f.endTime, f.timezone);
      const newStart = fromZonedInput(next.startDate, next.startTime, f.timezone);
      if (oldStart && oldEnd && newStart) {
        const length = Math.max(new Date(oldEnd).getTime() - new Date(oldStart).getTime(), 30 * 60_000);
        const end = toZonedInput(new Date(new Date(newStart).getTime() + length).toISOString(), f.timezone);
        next.endDate = end.date;
        next.endTime = end.time;
      }
      return next;
    });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const v = validate(form);
    setErrors(v.errors);
    if (Object.keys(v.errors).length > 0 || !v.startsAt || !v.endsAt) return;
    const locationChanged = !event || event.location_text !== form.location.trim();
    const body: EventRequest = {
      title: form.title.trim(),
      details: form.details.trim(),
      starts_at: v.startsAt,
      ends_at: v.endsAt,
      timezone: form.timezone,
      location_text: form.location.trim(),
      lat: locationChanged ? null : (event?.lat ?? null),
      lng: locationChanged ? null : (event?.lng ?? null),
      online_url: form.onlineUrl.trim() || null,
      cover_key: form.coverKey,
      capacity: form.capacity.trim() ? Number(form.capacity) : null,
      visibility: form.visibility,
    };
    const onError = (err: unknown) => toast.error(errorMessage(err, "Couldn't save the event. Try again."));
    if (event) {
      update.mutate(
        { id: event.id, body },
        {
          onSuccess: (saved) => {
            toast.success("Event saved");
            router.push(`/events/${saved.id}`);
          },
          onError,
        },
      );
    } else {
      create.mutate(body, {
        onSuccess: (saved) => {
          toast.success("Event created");
          router.push(`/events/${saved.id}`);
        },
        onError,
      });
    }
  };

  return (
    <form className="flex max-w-form flex-col gap-section" onSubmit={submit} noValidate>
      <div className="flex flex-col gap-4">
        <Field label="Title" error={errors.title} required>
          <Input value={form.title} maxLength={200} onChange={(e) => setForm((f) => ({ ...f, title: e.target.value }))} />
        </Field>
        <Field label="Details">
          <TextArea value={form.details} maxLength={5000} rows={5} onChange={(e) => setForm((f) => ({ ...f, details: e.target.value }))} />
        </Field>
      </div>

      <Section title="When">
        <div className="flex flex-col gap-4">
          <Field label="Time zone" info="Start and end are in this zone. Everyone sees the time in it, with its name.">
            <Select options={zoneOptions} value={form.timezone} onValueChange={(z) => setForm((f) => ({ ...f, timezone: z }))} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Start date" error={errors.start} required>
              <Input type="date" value={form.startDate} onChange={(e) => setStart({ startDate: e.target.value })} />
            </Field>
            <Field label="Start time" required>
              <Input type="time" value={form.startTime} onChange={(e) => setStart({ startTime: e.target.value })} />
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="End date" error={errors.end} required>
              <Input type="date" value={form.endDate} onChange={(e) => setForm((f) => ({ ...f, endDate: e.target.value }))} />
            </Field>
            <Field label="End time" required>
              <Input type="time" value={form.endTime} onChange={(e) => setForm((f) => ({ ...f, endTime: e.target.value }))} />
            </Field>
          </div>
        </div>
      </Section>

      <Section title="Where">
        <div className="flex flex-col gap-4">
          <Field label="Place">
            <Input value={form.location} maxLength={300} placeholder="Address or venue" onChange={(e) => setForm((f) => ({ ...f, location: e.target.value }))} />
          </Field>
          <Field label="Online link" error={errors.onlineUrl} info="Video call or stream. Shown to everyone who can see the event.">
            <Input type="url" inputMode="url" value={form.onlineUrl} placeholder="https://" onChange={(e) => setForm((f) => ({ ...f, onlineUrl: e.target.value }))} />
          </Field>
        </div>
      </Section>

      <Section title="Who can come">
        <div className="flex flex-col gap-4">
          <Field label="Visibility" info={VISIBILITY_INFO[form.visibility]}>
            <SegmentedControl<EventVisibility>
              options={VISIBILITIES.map((v) => ({ value: v, label: VISIBILITY_LABEL[v], ...(v === "invite" ? { icon: <Lock /> } : {}) }))}
              value={form.visibility}
              onValueChange={(v) => setForm((f) => ({ ...f, visibility: v }))}
              className="self-start"
            />
          </Field>
          <Field label="Capacity" error={errors.capacity} info="Leave empty for no limit. When it's full, new RSVPs join a waitlist that moves up automatically.">
            <Input type="number" inputMode="numeric" min={1} max={100000} value={form.capacity} placeholder="No limit" onChange={(e) => setForm((f) => ({ ...f, capacity: e.target.value }))} className="sm:max-w-48" />
          </Field>
        </div>
      </Section>

      <CoverField form={form} setForm={setForm} />

      <div className="flex flex-wrap gap-2">
        <Button type="submit" size="auto" loading={busy}>
          {event ? "Save" : "Create event"}
        </Button>
        <Button asChild variant="ghost" size="auto">
          <Link href={event ? `/events/${event.id}` : "/events"}>Cancel</Link>
        </Button>
      </div>
    </form>
  );
}

function FormSkeleton() {
  return (
    <SkeletonGroup label="Loading event" className="flex max-w-form flex-col gap-4">
      <Skeleton className="h-16" />
      <Skeleton className="h-32" />
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
    </SkeletonGroup>
  );
}

/** Create an event, or edit one (`id`; hosts only). Times are entered in the event's time zone. */
export function EventFormScreen({ id }: { id?: string }) {
  const event = useEvent(id ?? null);
  const shell = (body: ReactNode) => (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader title={id ? "Edit event" : "New event"} />
      {body}
    </div>
  );
  if (!id) return shell(<Form />);
  if (event.isPending) return shell(<FormSkeleton />);
  if (event.isError)
    return shell(
      <Card className="max-w-form">
        <ErrorState title="Couldn't load this event" info={event.error.message} onRetry={() => void event.refetch()} retrying={event.isFetching} />
      </Card>,
    );
  if (!event.data.host_role || event.data.status === "cancelled") {
    return shell(
      <Card className="max-w-form">
        <EmptyState
          icon={<CalendarX />}
          title={event.data.status === "cancelled" ? "This event was cancelled" : "Only hosts can edit this event"}
          action={
            <Button asChild variant="secondary" size="auto">
              <Link href={`/events/${event.data.id}`}>Back to the event</Link>
            </Button>
          }
        />
      </Card>,
    );
  }
  return shell(<Form event={event.data} />);
}
