"use client";

import {
  errorMessage,
  hasErrorCode,
  isClockTime,
  useAddPlacePhoto,
  useAdminAddPlacePhoto,
  useAdminCreatePlace,
  useAdminPlace,
  useAdminRemovePlacePhoto,
  useAdminUpdatePlace,
  useCreatePlace,
  usePlaceView,
  usePlacesConfig,
  useRemovePlacePhoto,
  useReverseGeocode,
  useUpdatePlace,
  useUpload,
  type LatLng,
  type Place,
  type PlaceHours,
  type PlaceInput,
} from "@scaffoldapp/shared";
import { Button, ErrorState, Field, PhotoUploader, Select, Skeleton, SkeletonGroup, Switch, TextArea, Input, toast, type UploadedPhoto } from "@scaffoldapp/ui-web";
import { ChevronLeft } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState, type FormEvent } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { AddressSearch } from "./AddressSearch";
import { HoursEditor, defaultHours } from "./HoursEditor";
import { MapView } from "./MapView";
import { browserLocation } from "./parts";

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

type Errors = Partial<Record<"name" | "category" | "point" | "website" | "hours", string>>;

function fromPlace(p: Place | undefined): FormState {
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

function validate(f: FormState): Errors {
  const e: Errors = {};
  if (!f.name.trim()) e.name = "Enter the place's name.";
  else if (f.name.trim().length > 120) e.name = "Use 120 characters or fewer.";
  if (!f.category) e.category = "Choose a category.";
  if (!f.point) e.point = "Choose an address or drop a pin on the map.";
  if (f.website && !/^(https?:\/\/)?[^\s/$.?#].[^\s]*$/i.test(f.website.trim())) e.website = "Enter a web address like example.com.";
  if (f.hours) {
    for (const list of Object.values(f.hours.weekly)) {
      for (const iv of list) {
        if (!isClockTime(iv.open) || !isClockTime(iv.close)) e.hours = "Fill in every opening and closing time.";
        else if (iv.open === iv.close) e.hours = "An opening can't start and end at the same time.";
      }
    }
  }
  return e;
}

function toInput(f: FormState): PlaceInput {
  return {
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
}

function Form({ initial, mode, admin }: { initial?: Place; mode: "create" | "edit"; admin: boolean }) {
  const router = useRouter();
  const config = usePlacesConfig();
  const upload = useUpload();
  const reverse = useReverseGeocode();
  const create = useCreatePlace();
  const update = useUpdatePlace();
  const adminCreate = useAdminCreatePlace();
  const adminUpdate = useAdminUpdatePlace();
  const addPhoto = useAddPlacePhoto();
  const removePhoto = useRemovePlacePhoto();
  const adminAddPhoto = useAdminAddPlacePhoto();
  const adminRemovePhoto = useAdminRemovePlacePhoto();
  const [f, setF] = useState<FormState>(() => fromPlace(initial));
  const [errors, setErrors] = useState<Errors>({});
  const [photos, setPhotos] = useState<UploadedPhoto[]>([]);
  const [uploading, setUploading] = useState(false);
  const [saving, setSaving] = useState(false);
  const existing = useMemo<UploadedPhoto[]>(() => (initial?.photos ?? []).map((p) => ({ id: p.id, key: "", url: p.url })), [initial]);
  const set = <K extends keyof FormState>(k: K, v: FormState[K]) => setF((prev) => ({ ...prev, [k]: v }));
  const cfg = config.data;
  if (!cfg) return null;

  const dropPin = (p: LatLng) => {
    set("point", p);
    reverse.mutate(p, { onSuccess: (r) => r && setF((prev) => ({ ...prev, address: r.label })) });
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const errs = validate(f);
    setErrors(errs);
    if (Object.keys(errs).length > 0) {
      document.querySelector<HTMLElement>("[aria-invalid=true]")?.focus();
      return;
    }
    setSaving(true);
    try {
      const body = toInput(f);
      const newKeys = photos.filter((p) => p.key).map((p) => p.key);
      let saved: Place;
      if (mode === "create") {
        saved = admin ? ((await adminCreate.mutateAsync({ ...body, photo_keys: newKeys })) as Place) : await create.mutateAsync({ ...body, photo_keys: newKeys });
      } else {
        const id = initial?.id ?? "";
        saved = admin ? ((await adminUpdate.mutateAsync({ id, ...body })) as Place) : await update.mutateAsync({ id, ...body });
        const kept = new Set(photos.map((p) => p.id));
        for (const old of existing) {
          if (!kept.has(old.id)) await (admin ? adminRemovePhoto.mutateAsync({ id, photoId: old.id }) : removePhoto.mutateAsync({ id, photoId: old.id }));
        }
        for (const key of newKeys) await (admin ? adminAddPhoto.mutateAsync({ id, key }) : addPhoto.mutateAsync({ id, key }));
      }
      const review = !admin && cfg.require_approval;
      toast.success(review ? (mode === "create" ? "Sent for review" : "Saved. Staff review the change before it shows.") : "Saved");
      router.push(admin ? `/admin/places/${saved.id}` : `/places/${saved.id}`);
    } catch (err) {
      if (hasErrorCode(err, "outside_service_area")) setErrors({ point: err.message });
      toast.error(errorMessage(err, "Couldn't save. Try again."));
    } finally {
      setSaving(false);
    }
  };

  const back = admin ? (initial ? `/admin/places/${initial.id}` : "/admin/places") : initial ? `/places/${initial.id}` : "/places";
  const title = mode === "create" ? "Add a place" : "Edit place";
  const info = !admin && cfg.require_approval ? "Staff review new places and edits before they show on the map." : undefined;

  return (
    <form className="flex flex-col gap-section" onSubmit={submit} noValidate>
      <div className="flex flex-col gap-2">
        <Link href={back} className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
          <ChevronLeft className="size-4" aria-hidden />
          Back
        </Link>
        <PageHeader title={title} info={info} />
      </div>

      <Section title="Basics">
        <div className="flex max-w-form flex-col gap-4">
          <Field label="Name" required error={errors.name}>
            <Input value={f.name} maxLength={120} onChange={(e) => set("name", e.target.value)} autoComplete="off" />
          </Field>
          <Field label="Category" required error={errors.category}>
            <Select options={cfg.categories.map((c) => ({ value: c.id, label: c.label }))} value={f.category} placeholder="Choose a category" onValueChange={(v) => set("category", v)} />
          </Field>
          <Field label="Description">
            <TextArea value={f.description} maxLength={2000} autoGrow minRows={3} onChange={(e) => set("description", e.target.value)} />
          </Field>
        </div>
      </Section>

      <Section title="Location">
        <div className="grid gap-4 lg:grid-cols-2">
          <div className="flex flex-col gap-4">
            <Field label="Find the address" error={errors.point} info="Or click the map to drop the pin.">
              <AddressSearch
                near={f.point ?? cfg.default_center}
                invalid={!!errors.point}
                onPick={(r) => setF((prev) => ({ ...prev, address: r.label, point: { lat: r.lat, lng: r.lng } }))}
              />
            </Field>
            <Field label="Address as shown">
              <Input value={f.address} maxLength={300} onChange={(e) => set("address", e.target.value)} />
            </Field>
          </div>
          <MapView
            key={f.point ? "set" : "unset"}
            label="Place location; click to move the pin"
            styleUrl={cfg.map_style_url}
            center={f.point ?? cfg.default_center}
            zoom={f.point ? 16 : cfg.default_zoom}
            pins={f.point ? [{ id: "pin", lat: f.point.lat, lng: f.point.lng, label: f.name || "New place", selected: true }] : []}
            onMapClick={dropPin}
            onLocate={async () => {
              const p = await browserLocation();
              if (p) dropPin(p);
              return p;
            }}
            className="h-72 rounded-lg border border-border"
          />
        </div>
      </Section>

      <Section title="Contact">
        <div className="grid max-w-form gap-4 sm:grid-cols-2">
          <Field label="Phone">
            <Input type="tel" value={f.phone} maxLength={40} onChange={(e) => set("phone", e.target.value)} autoComplete="off" />
          </Field>
          <Field label="Website" error={errors.website}>
            <Input type="url" inputMode="url" value={f.website} onChange={(e) => set("website", e.target.value)} placeholder="example.com" autoComplete="off" />
          </Field>
        </div>
      </Section>

      <Section title="Hours" action={<Switch aria-label="Add opening hours" checked={!!f.hours} onCheckedChange={(on) => set("hours", on ? (initial?.hours ?? defaultHours()) : null)} />}>
        {errors.hours ? (
          <p role="alert" className="type-label text-danger-strong">
            {errors.hours}
          </p>
        ) : null}
        {f.hours ? <HoursEditor value={f.hours} onChange={(h) => set("hours", h)} /> : <p className="type-label text-fg-muted">No hours listed.</p>}
      </Section>

      <Section title="Photos">
        <PhotoUploader
          max={cfg.max_photos}
          capture={false}
          defaultValue={existing}
          onChange={setPhotos}
          onUploadingChange={setUploading}
          upload={async (file, { signal }) => {
            const r = await upload.mutateAsync({ file, purpose: "place_photo", signal });
            return { key: r.key, url: r.public_url };
          }}
        />
      </Section>

      <div className="flex flex-wrap gap-2">
        <Button type="submit" size="auto" loading={saving} disabled={uploading}>
          {mode === "create" ? (!admin && cfg.require_approval ? "Send for review" : "Add place") : "Save"}
        </Button>
        <Button asChild variant="ghost" size="auto">
          <Link href={back}>Cancel</Link>
        </Button>
      </div>
    </form>
  );
}

function Loading() {
  return (
    <SkeletonGroup label="Loading form" className="flex max-w-form flex-col gap-section">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-16" />
      <Skeleton className="h-16" />
      <Skeleton className="h-40" />
    </SkeletonGroup>
  );
}

function EditUser({ id }: { id: string }) {
  const view = usePlaceView(id);
  if (view.isPending) return <Loading />;
  if (view.isError) return <ErrorState title="Couldn't load this place" info={errorMessage(view.error)} onRetry={() => void view.refetch()} retrying={view.isFetching} />;
  return <Form initial={view.data.place} mode="edit" admin={false} />;
}

function EditAdmin({ id }: { id: string }) {
  const place = useAdminPlace(id);
  if (place.isPending) return <Loading />;
  if (place.isError) return <ErrorState title="Couldn't load this place" info={errorMessage(place.error)} onRetry={() => void place.refetch()} retrying={place.isFetching} />;
  return <Form initial={place.data} mode="edit" admin />;
}

/** Add or edit a place (`/places/new`, `/places/[id]/edit`, and the admin equivalents with `admin`). */
export function PlaceFormScreen({ id, admin = false }: { id?: string; admin?: boolean }) {
  const config = usePlacesConfig();
  const content = config.isPending ? (
    <Loading />
  ) : config.isError ? (
    <ErrorState title="Couldn't load the form" info={errorMessage(config.error)} onRetry={() => void config.refetch()} retrying={config.isFetching} />
  ) : !id ? (
    <Form mode="create" admin={admin} />
  ) : admin ? (
    <EditAdmin id={id} />
  ) : (
    <EditUser id={id} />
  );
  return admin ? content : <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">{content}</div>;
}
