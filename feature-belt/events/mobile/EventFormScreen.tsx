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
import { Button, EmptyState, ErrorState, Field, Input, ListRow, Screen, SegmentedControl, Sheet, Skeleton, Text, TextArea, haptic, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { router } from "expo-router";
import { CalendarX, Check, Globe, ImagePlus, Search, Trash } from "lucide-react-native";
import { useMemo, useState } from "react";
import { FlatList, Pressable, View } from "react-native";
import { DateTimeRow, VISIBILITY_INFO, VISIBILITY_LABEL } from "./parts";

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

const zoneName = (z: string) => z.replace(/_/g, " ");

function TimeZoneSheet({ open, value, onClose, onPick }: { open: boolean; value: string; onClose: () => void; onPick: (z: string) => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [q, setQ] = useState("");
  const all = useMemo(() => timeZoneOptions([value]), [value]);
  const now = useMemo(() => new Date().toISOString(), []);
  const needle = q.trim().toLowerCase().replace(/\s+/g, "_");
  const zones = needle ? all.filter((z) => z.toLowerCase().includes(needle)) : all;
  return (
    <Sheet open={open} onClose={onClose} onDismissed={() => setQ("")} title="Time zone" snapPoints={["85%"]}>
      <View style={{ flex: 1, gap: s[3] }}>
        <Input value={q} onChangeText={setQ} placeholder="Search" leadingIcon={Search} autoCorrect={false} autoCapitalize="none" accessibilityLabel="Search time zones" />
        <FlatList
          data={zones}
          keyExtractor={(z) => z}
          keyboardShouldPersistTaps="handled"
          initialNumToRender={20}
          renderItem={({ item }) => (
            <Pressable
              accessibilityRole="button"
              accessibilityState={{ selected: item === value }}
              onPress={() => {
                onPick(item);
                onClose();
              }}
              style={({ pressed }) => ({ minHeight: theme.tap.base, flexDirection: "row", alignItems: "center", gap: s[3], paddingHorizontal: s[2], borderRadius: theme.radius.md, backgroundColor: pressed ? c.sunken : "transparent" })}
            >
              <Text role="body" style={{ flex: 1 }}>
                {zoneName(item)}
              </Text>
              <Text role="label" tone="muted">
                {zoneLabel(now, item)}
              </Text>
              {item === value ? <Check size={18} color={c.primary} strokeWidth={2.25} /> : null}
            </Pressable>
          )}
        />
      </View>
    </Sheet>
  );
}

function Form({ event }: { event?: EventItem }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const create = useCreateEvent();
  const update = useUpdateEvent();
  const upload = useUpload();
  const [form, setForm] = useState<FormState>(() => initialState(event));
  const [errors, setErrors] = useState<Errors>({});
  const [zoneOpen, setZoneOpen] = useState(false);
  const busy = create.isPending || update.isPending;
  const patch = (p: Partial<FormState>) => setForm((f) => ({ ...f, ...p }));

  // Moving the start keeps the event's length.
  const setStart = (date: string, time: string) =>
    setForm((f) => {
      const next = { ...f, startDate: date, startTime: time };
      const oldStart = fromZonedInput(f.startDate, f.startTime, f.timezone);
      const oldEnd = fromZonedInput(f.endDate, f.endTime, f.timezone);
      const newStart = fromZonedInput(date, time, f.timezone);
      if (oldStart && oldEnd && newStart) {
        const length = Math.max(new Date(oldEnd).getTime() - new Date(oldStart).getTime(), 30 * 60_000);
        const end = toZonedInput(new Date(new Date(newStart).getTime() + length).toISOString(), f.timezone);
        next.endDate = end.date;
        next.endTime = end.time;
      }
      return next;
    });

  const pickCover = async () => {
    try {
      const [photo] = await pickPhotos("library", 1, 0.8);
      if (!photo) return;
      const r = await upload.mutateAsync({ file: photo, purpose: EVENT_COVER_PURPOSE, maxBytes: MAX_UPLOAD_BYTES });
      patch({ coverKey: r.key, coverUrl: r.public_url });
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't upload the image. Try again."));
    }
  };

  const save = () => {
    const v = validate(form);
    setErrors(v.errors);
    if (Object.keys(v.errors).length > 0 || !v.startsAt || !v.endsAt) {
      haptic.error();
      return;
    }
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
          onSuccess: () => {
            haptic.success();
            toast.success("Event saved");
            router.back();
          },
          onError,
        },
      );
    } else {
      create.mutate(body, {
        onSuccess: (saved) => {
          haptic.success();
          toast.success("Event created");
          router.replace({ pathname: "/events/[id]", params: { id: saved.id } });
        },
        onError,
      });
    }
  };

  return (
    <Screen
      keyboard
      edges={["bottom"]}
      gap={s[8]}
      footer={
        <Button fullWidth loading={busy} disabled={upload.isPending} onPress={save}>
          {event ? "Save" : "Create event"}
        </Button>
      }
    >
      <View style={{ gap: s[4] }}>
        <Field label="Title" error={errors.title} required>
          <Input value={form.title} maxLength={200} onChangeText={(t) => patch({ title: t })} returnKeyType="next" />
        </Field>
        <Field label="Details">
          <TextArea value={form.details} maxLength={5000} onChangeText={(t) => patch({ details: t })} />
        </Field>
      </View>

      <View style={{ gap: s[4] }}>
        <Field label="Time zone" info="Start and end are in this zone. Everyone sees the time in it, with its name.">
          <ListRow title={`${zoneName(form.timezone)} (${zoneLabel(new Date().toISOString(), form.timezone)})`} leading={<Globe size={20} color={c.textMuted} strokeWidth={2} />} chevron onPress={() => setZoneOpen(true)} />
        </Field>
        <Field label="Starts" error={errors.start} required>
          <DateTimeRow label="Starts" date={form.startDate} time={form.startTime} onChange={setStart} />
        </Field>
        <Field label="Ends" error={errors.end} required>
          <DateTimeRow label="Ends" date={form.endDate} time={form.endTime} onChange={(date, time) => patch({ endDate: date, endTime: time })} />
        </Field>
      </View>

      <View style={{ gap: s[4] }}>
        <Field label="Place">
          <Input value={form.location} maxLength={300} placeholder="Address or venue" onChangeText={(t) => patch({ location: t })} />
        </Field>
        <Field label="Online link" error={errors.onlineUrl} info="Video call or stream. Shown to everyone who can see the event.">
          <Input value={form.onlineUrl} placeholder="https://" keyboardType="url" autoCapitalize="none" autoCorrect={false} onChangeText={(t) => patch({ onlineUrl: t })} />
        </Field>
      </View>

      <View style={{ gap: s[4] }}>
        <Field label="Visibility" info={VISIBILITY_INFO[form.visibility]}>
          <SegmentedControl<EventVisibility>
            accessibilityLabel="Visibility"
            options={VISIBILITIES.map((v) => ({ value: v, label: VISIBILITY_LABEL[v] }))}
            value={form.visibility}
            onValueChange={(v) => patch({ visibility: v })}
          />
        </Field>
        <Field label="Capacity" error={errors.capacity} info="Leave empty for no limit. When it's full, new RSVPs join a waitlist that moves up automatically.">
          <Input value={form.capacity} placeholder="No limit" keyboardType="number-pad" maxLength={6} onChangeText={(t) => patch({ capacity: t.replace(/[^0-9]/g, "") })} />
        </Field>
      </View>

      <Field label="Cover image" info="Shown at the top of the event. 16:9 works best.">
        <View style={{ gap: s[3] }}>
          {form.coverUrl ? (
            <Image source={{ uri: form.coverUrl }} style={{ width: "100%", aspectRatio: 16 / 9, borderRadius: theme.radius.lg, backgroundColor: c.sunken }} contentFit="cover" accessibilityIgnoresInvertColors />
          ) : null}
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
            <Button variant="secondary" leftIcon={ImagePlus} loading={upload.isPending} onPress={() => void pickCover()}>
              {form.coverUrl ? "Change image" : "Add image"}
            </Button>
            {form.coverUrl ? (
              <Button variant="ghost" leftIcon={Trash} disabled={upload.isPending} onPress={() => patch({ coverKey: null, coverUrl: null })}>
                Remove
              </Button>
            ) : null}
          </View>
        </View>
      </Field>

      <TimeZoneSheet open={zoneOpen} value={form.timezone} onClose={() => setZoneOpen(false)} onPick={(z) => patch({ timezone: z })} />
    </Screen>
  );
}

/** Create an event, or edit one (`id`; hosts only). Times are entered in the event's time zone. */
export function EventFormScreen({ id }: { id?: string | undefined }) {
  const theme = useTheme();
  const event = useEvent(id ?? null);
  if (!id) return <Form />;
  if (event.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <View style={{ gap: theme.space[4] }} accessibilityLabel="Loading event" accessibilityRole="progressbar">
          <Skeleton height={theme.tap.base} />
          <Skeleton height={120} />
          <Skeleton height={theme.tap.base} />
        </View>
      </Screen>
    );
  }
  if (event.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <ErrorState title="Couldn't load this event" info={errorMessage(event.error)} onRetry={() => void event.refetch()} retrying={event.isRefetching} />
      </Screen>
    );
  }
  if (!event.data.host_role || event.data.status === "cancelled") {
    return (
      <Screen edges={["bottom"]} centered>
        <EmptyState
          icon={CalendarX}
          title={event.data.status === "cancelled" ? "This event was cancelled" : "Only hosts can edit this event"}
          action={
            <Button variant="secondary" onPress={() => router.back()}>
              Back
            </Button>
          }
        />
      </Screen>
    );
  }
  return <Form event={event.data} />;
}
