import {
  emptyWeek,
  errorMessage,
  hasErrorCode,
  isClockTime,
  useAddPlacePhoto,
  useCreatePlace,
  useDebouncedValue,
  useGeocode,
  usePlaceView,
  usePlacesConfig,
  useRemovePlacePhoto,
  useReverseGeocode,
  useUpdatePlace,
  useUpload,
  WEEKDAYS,
  type GeoResult,
  type LatLng,
  type Place,
  type PlaceHours,
  type Weekday,
} from "@scaffoldapp/shared";
import {
  Banner,
  Body,
  Button,
  Caption,
  ErrorState,
  Field,
  Input,
  ListGroup,
  ListRow,
  PhotoUploader,
  Screen,
  Skeleton,
  Switch,
  TextArea,
  ToggleChip,
  haptic,
  toast,
  useTheme,
  type UploadedPhoto,
} from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { LocateFixed, MapPin, Save, Search } from "lucide-react-native";
import { useMemo, useState } from "react";
import { View } from "react-native";
import { deviceLocation } from "./location";

interface FormState {
  name: string;
  category: string;
  description: string;
  address: string;
  point: LatLng | null;
  phone: string;
  website: string;
  hours: PlaceHours | null;
}

function fromPlace(p?: Place): FormState {
  return {
    name: p?.name ?? "",
    category: p?.category ?? "",
    description: p?.description ?? "",
    address: p?.address ?? "",
    point: p ? { lat: p.lat, lng: p.lng } : null,
    phone: p?.phone ?? "",
    website: p?.website ?? "",
    hours: p?.hours ?? null,
  };
}

function weekdayHours(): PlaceHours {
  const weekly = emptyWeek();
  for (const d of ["mon", "tue", "wed", "thu", "fri"] as Weekday[]) weekly[d] = [{ open: "09:00", close: "17:00" }];
  let timezone = "";
  try {
    timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    timezone = "";
  }
  return { timezone, note: "", weekly };
}

/** One day: open switch and the first opening's times (further openings set on the web are kept). */
function DayRow({ day, label, hours, onChange }: { day: Weekday; label: string; hours: PlaceHours; onChange: (h: PlaceHours) => void }) {
  const theme = useTheme();
  const s = theme.space;
  const list = hours.weekly[day];
  const first = list[0];
  const set = (next: PlaceHours["weekly"][Weekday]) => onChange({ ...hours, weekly: { ...hours.weekly, [day]: next } });
  return (
    <View style={{ gap: s[2] }}>
      <Switch label={label} value={list.length > 0} onValueChange={(on: boolean) => set(on ? [{ open: "09:00", close: "17:00" }] : [])} />
      {first ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Input
            value={first.open}
            onChangeText={(v) => set([{ ...first, open: v }, ...list.slice(1)])}
            placeholder="09:00"
            keyboardType="numbers-and-punctuation"
            maxLength={5}
            accessibilityLabel={`${label} opens`}
            containerStyle={{ flex: 1 }}
          />
          <Caption tone="muted">to</Caption>
          <Input
            value={first.close}
            onChangeText={(v) => set([{ ...first, close: v }, ...list.slice(1)])}
            placeholder="17:00"
            keyboardType="numbers-and-punctuation"
            maxLength={5}
            accessibilityLabel={`${label} closes`}
            containerStyle={{ flex: 1 }}
          />
        </View>
      ) : null}
    </View>
  );
}

function Form({ initial }: { initial?: Place }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const config = usePlacesConfig();
  const create = useCreatePlace();
  const update = useUpdatePlace();
  const addPhoto = useAddPlacePhoto();
  const removePhoto = useRemovePlacePhoto();
  const upload = useUpload();
  const reverse = useReverseGeocode();
  const [f, setF] = useState<FormState>(() => fromPlace(initial));
  const [errors, setErrors] = useState<Partial<Record<"name" | "category" | "point" | "hours", string>>>({});
  const [search, setSearch] = useState("");
  const q = useDebouncedValue(search, 300);
  const results = useGeocode(q, f.point ?? config.data?.default_center);
  const [photos, setPhotos] = useState<UploadedPhoto[]>([]);
  const [uploading, setUploading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [locating, setLocating] = useState(false);
  const existing = useMemo<UploadedPhoto[]>(() => (initial?.photos ?? []).map((p) => ({ id: p.id, key: "", url: p.url })), [initial]);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((prev) => ({ ...prev, [k]: v }));
  const cfg = config.data;
  if (!cfg) return null;

  const pick = (r: GeoResult) => {
    setF((prev) => ({ ...prev, address: r.label, point: { lat: r.lat, lng: r.lng } }));
    setSearch("");
  };
  const fillFromLocation = async () => {
    setLocating(true);
    const p = await deviceLocation();
    setLocating(false);
    if (!p) return;
    set("point", p);
    reverse.mutate(p, { onSuccess: (r) => r && setF((prev) => ({ ...prev, address: r.label })) });
  };

  const save = async () => {
    const e: typeof errors = {};
    if (!f.name.trim()) e.name = "Enter the place's name.";
    if (!f.category) e.category = "Choose a category.";
    if (!f.point) e.point = "Find the address or use your location.";
    if (f.hours && Object.values(f.hours.weekly).some((l) => l.some((iv) => !isClockTime(iv.open) || !isClockTime(iv.close) || iv.open === iv.close))) e.hours = "Use times like 09:00 and 17:30.";
    setErrors(e);
    if (Object.keys(e).length > 0) return;
    setSaving(true);
    try {
      const body = {
        name: f.name.trim(),
        category: f.category,
        description: f.description.trim(),
        address: f.address.trim(),
        lat: f.point?.lat,
        lng: f.point?.lng,
        phone: f.phone.trim(),
        website: f.website.trim(),
        hours: f.hours,
      };
      const newKeys = photos.filter((p) => p.key).map((p) => p.key);
      let saved: Place;
      if (!initial) {
        saved = await create.mutateAsync({ ...body, photo_keys: newKeys });
      } else {
        saved = await update.mutateAsync({ id: initial.id, ...body });
        const kept = new Set(photos.map((p) => p.id));
        for (const old of existing) if (!kept.has(old.id)) await removePhoto.mutateAsync({ id: initial.id, photoId: old.id });
        for (const key of newKeys) await addPhoto.mutateAsync({ id: initial.id, key });
      }
      haptic.success();
      toast.success(cfg.require_approval ? (initial ? "Saved. Staff review the change before it shows." : "Sent for review") : "Saved");
      router.replace({ pathname: "/places/[id]", params: { id: saved.id } });
    } catch (err) {
      if (hasErrorCode(err, "outside_service_area")) setErrors({ point: err.message });
      toast.error(errorMessage(err, "Couldn't save. Try again."));
    } finally {
      setSaving(false);
    }
  };

  const items = q.trim().length >= 2 ? (results.data ?? []) : [];
  return (
    <Screen
      keyboard
      edges={["bottom"]}
      gap={s[8]}
      footer={
        <Button fullWidth leftIcon={Save} loading={saving} disabled={uploading} onPress={() => void save()}>
          {initial ? "Save" : cfg.require_approval ? "Send for review" : "Add place"}
        </Button>
      }
    >
      <Stack.Screen options={{ title: initial ? "Edit place" : "Add a place" }} />
      {cfg.require_approval ? <Banner tone="info" title="Staff review new places and edits before they show on the map." /> : null}
      <View style={{ gap: s[4] }}>
        <Field label="Name" required error={errors.name}>
          <Input value={f.name} onChangeText={(v) => set("name", v)} maxLength={120} />
        </Field>
        <Field label="Category" required error={errors.category}>
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
            {cfg.categories.map((cat) => (
              <ToggleChip key={cat.id} size="sm" pressed={f.category === cat.id} onPressedChange={() => set("category", cat.id)}>
                {cat.label}
              </ToggleChip>
            ))}
          </View>
        </Field>
        <Field label="Description">
          <TextArea value={f.description} onChangeText={(v) => set("description", v)} maxLength={2000} />
        </Field>
      </View>

      <View style={{ gap: s[3] }}>
        <Field label="Location" required error={errors.point}>
          <Input value={search} onChangeText={setSearch} placeholder="Search an address or place" leadingIcon={Search} autoCorrect={false} />
        </Field>
        {items.length > 0 ? (
          <ListGroup>
            {items.map((r) => (
              <ListRow key={`${r.lat},${r.lng},${r.label}`} title={r.label} leading={<MapPin size={20} color={c.textMuted} />} onPress={() => pick(r)} chevron={false} />
            ))}
          </ListGroup>
        ) : results.isError && q.trim().length >= 2 ? (
          <Caption tone="danger">{errorMessage(results.error, "Address search isn't available right now.")}</Caption>
        ) : null}
        <Button variant="secondary" leftIcon={LocateFixed} loading={locating} onPress={() => void fillFromLocation()}>
          Use my location
        </Button>
        {f.point ? (
          <Field label="Address as shown">
            <Input value={f.address} onChangeText={(v) => set("address", v)} maxLength={300} />
          </Field>
        ) : null}
      </View>

      <View style={{ gap: s[4] }}>
        <Field label="Phone">
          <Input value={f.phone} onChangeText={(v) => set("phone", v)} keyboardType="phone-pad" maxLength={40} />
        </Field>
        <Field label="Website">
          <Input value={f.website} onChangeText={(v) => set("website", v)} keyboardType="url" autoCapitalize="none" autoCorrect={false} placeholder="example.com" />
        </Field>
      </View>

      <View style={{ gap: s[3] }}>
        <Switch label="Opening hours" value={!!f.hours} onValueChange={(on: boolean) => set("hours", on ? (initial?.hours ?? weekdayHours()) : null)} />
        {errors.hours ? <Caption tone="danger">{errors.hours}</Caption> : null}
        {f.hours ? (
          <View style={{ gap: s[4] }}>
            {WEEKDAYS.map((d) => (
              <DayRow key={d.key} day={d.key} label={d.label} hours={f.hours as PlaceHours} onChange={(h) => set("hours", h)} />
            ))}
          </View>
        ) : (
          <Body tone="muted">No hours listed.</Body>
        )}
      </View>

      <Field label="Photos">
        <PhotoUploader
          max={cfg.max_photos}
          defaultValue={existing}
          onChange={setPhotos}
          onUploadingChange={setUploading}
          upload={async (file, { signal }) => {
            const r = await upload.mutateAsync({ file, purpose: "place_photo", signal });
            return { key: r.key, url: r.public_url };
          }}
        />
      </Field>
    </Screen>
  );
}

/** Add (`/places/new`) or edit (`/places/[id]/edit`) a place. */
export function PlaceFormScreen({ id }: { id?: string }) {
  const theme = useTheme();
  const config = usePlacesConfig();
  const place = usePlaceView(id);
  if (config.isPending || (id && place.isPending)) {
    return (
      <Screen edges={["bottom"]}>
        <Skeleton height={theme.tap.base} />
        <Skeleton height={theme.tap.base} />
        <Skeleton height={120} />
      </Screen>
    );
  }
  if (config.isError || (id && place.isError)) {
    const err = config.error ?? place.error;
    return (
      <Screen edges={["bottom"]} centered>
        <ErrorState title="Couldn't load the form" info={errorMessage(err)} onRetry={() => void (config.isError ? config.refetch() : place.refetch())} />
      </Screen>
    );
  }
  return <Form initial={id ? place.data?.place : undefined} />;
}
