"use client";

import {
  errorMessage,
  formatDistance,
  openState,
  useFavoriteIds,
  usePlacesConfig,
  useToggleFavorite,
  type LatLng,
  type PlaceHours,
  type PlaceStatus,
  type PlaceSummary,
} from "@scaffoldapp/shared";
import { Badge, IconButton, ToggleChip, cn, toast } from "@scaffoldapp/ui-web";
import { Heart, MapPin } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { useCallback } from "react";
import { isOptimisable } from "@/lib/images";

/** Category id → label from the config (falls back to the id). */
export function useCategoryLabel(): (id: string) => string {
  const config = usePlacesConfig();
  const cats = config.data?.categories;
  return useCallback((id: string) => cats?.find((c) => c.id === id)?.label ?? id, [cats]);
}

/** "Open now" / "Closed now"; nothing when the hours are unknown. */
export function OpenBadge({ hours, className }: { hours: PlaceHours | null; className?: string }) {
  const state = openState(hours);
  if (state === "unknown") return null;
  return (
    <Badge tone={state === "open" ? "success" : "neutral"} dot className={className}>
      {state === "open" ? "Open now" : "Closed now"}
    </Badge>
  );
}

const STATUS: Record<PlaceStatus, { label: string; tone: "warning" | "success" | "danger" }> = {
  pending: { label: "In review", tone: "warning" },
  approved: { label: "On the map", tone: "success" },
  rejected: { label: "Not approved", tone: "danger" },
};

export function PlaceStatusBadge({ status }: { status: PlaceStatus }) {
  const s = STATUS[status];
  return <Badge tone={s.tone}>{s.label}</Badge>;
}

/** Heart toggle for a place (optimistic). */
export function FavoriteButton({ placeId, size = "md" }: { placeId: string; size?: "sm" | "md" }) {
  const ids = useFavoriteIds();
  const toggle = useToggleFavorite();
  const on = ids.data?.includes(placeId) ?? false;
  return (
    <IconButton
      label={on ? "Remove from saved" : "Save place"}
      tooltip
      size={size}
      variant="ghost"
      aria-pressed={on}
      className={on ? "text-danger-strong" : undefined}
      icon={<Heart className={on ? "fill-current" : undefined} />}
      onClick={() =>
        toggle.mutate(
          { id: placeId, favorite: !on },
          { onError: (e) => toast.error(errorMessage(e, on ? "Couldn't remove it. Try again." : "Couldn't save it. Try again.")) },
        )
      }
    />
  );
}

/** Square photo or a pin glyph. */
export function PlaceThumb({ url, name, className }: { url: string | null; name: string; className?: string }) {
  return (
    <span className={cn("relative flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-md bg-sunken text-fg-muted", className)}>
      {url ? (
        <Image src={url} alt={name} fill sizes="48px" className="object-cover" unoptimized={!isOptimisable(url)} />
      ) : (
        <MapPin className="size-5" aria-hidden />
      )}
    </span>
  );
}

/** One place in a list: thumb · name · category and distance · save. */
export function PlaceRow({ place, selected, onSelect }: { place: PlaceSummary; selected?: boolean; onSelect?: (p: PlaceSummary) => void }) {
  const label = useCategoryLabel();
  const distance = formatDistance(place.distance_m);
  return (
    <li
      data-place-id={place.id}
      className={cn("flex items-center gap-3 rounded-md px-3 py-2 transition-colors", selected ? "bg-primary-soft" : "hover:bg-sunken")}
    >
      <PlaceThumb url={place.photo_url} name={place.name} />
      <Link
        href={`/places/${place.id}`}
        onClick={(e) => {
          if (onSelect && !e.metaKey && !e.ctrlKey && !selected) {
            e.preventDefault();
            onSelect(place);
          }
        }}
        className="flex min-h-tap min-w-0 flex-1 flex-col justify-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="truncate type-label font-semibold text-fg">{place.name}</span>
        <span className="truncate type-caption text-fg-muted">
          {[label(place.category), distance, place.address].filter(Boolean).join(" · ")}
        </span>
      </Link>
      <FavoriteButton placeId={place.id} size="sm" />
    </li>
  );
}

/** Category filter chips (any of the selected). */
export function CategoryChips({ value, onChange, className }: { value: readonly string[]; onChange: (next: string[]) => void; className?: string }) {
  const config = usePlacesConfig();
  const cats = config.data?.categories ?? [];
  if (cats.length === 0) return null;
  return (
    <div role="group" aria-label="Categories" className={cn("flex gap-2 overflow-x-auto scrollbar-none", className)}>
      {cats.map((c) => {
        const on = value.includes(c.id);
        return (
          <ToggleChip key={c.id} size="sm" pressed={on} onPressedChange={(p) => onChange(p ? [...value, c.id] : value.filter((x) => x !== c.id))} className="shrink-0">
            {c.label}
          </ToggleChip>
        );
      })}
    </div>
  );
}

/** The browser's position, asked when the user taps a locate control. */
export async function browserLocation(): Promise<LatLng | null> {
  if (typeof navigator === "undefined" || !navigator.geolocation) {
    toast.error("This browser can't share your location.");
    return null;
  }
  return new Promise((resolve) => {
    navigator.geolocation.getCurrentPosition(
      (pos) => resolve({ lat: pos.coords.latitude, lng: pos.coords.longitude }),
      (err) => {
        toast.error(err.code === err.PERMISSION_DENIED ? "Location is off for this site. Allow it in the browser to see places near you." : "Couldn't find your location. Try again.");
        resolve(null);
      },
      { enableHighAccuracy: false, timeout: 10_000, maximumAge: 60_000 },
    );
  });
}

/** Directions in the user's maps app (web: OpenStreetMap routing). */
export function directionsUrl(p: LatLng): string {
  return `https://www.openstreetmap.org/directions?to=${p.lat.toFixed(6)}%2C${p.lng.toFixed(6)}`;
}
