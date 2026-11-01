"use client";

import {
  errorMessage,
  formatDay,
  isNotFound,
  todayKey,
  useDeletePlace,
  usePlaceView,
  usePlacesConfig,
  WEEKDAYS,
  type Place,
  type PlaceHours,
} from "@scaffoldapp/shared";
import { Banner, Button, ConfirmDialog, EmptyState, ErrorState, PhotoGrid, Skeleton, SkeletonGroup, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronLeft, Globe, MapPinOff, Navigation, Pencil, Phone, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { MapView } from "./MapView";
import { FavoriteButton, OpenBadge, PlaceStatusBadge, directionsUrl, useCategoryLabel } from "./parts";

/** Weekly hours, today highlighted. */
export function HoursTable({ hours }: { hours: PlaceHours }) {
  const today = todayKey(hours);
  return (
    <div className="flex flex-col gap-2">
      <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 type-label">
        {WEEKDAYS.map((d) => (
          <div key={d.key} className="contents">
            <dt className={cn("text-fg-muted", d.key === today && "font-semibold text-fg")}>{d.label}</dt>
            <dd className={cn("tabular-nums text-fg", d.key === today && "font-semibold")}>{formatDay(hours.weekly[d.key])}</dd>
          </div>
        ))}
      </dl>
      {hours.note ? <p className="type-caption text-fg-muted">{hours.note}</p> : null}
    </div>
  );
}

function hostOf(url: string): string {
  return url.replace(/^https?:\/\//, "").replace(/\/$/, "");
}

function Body({ place, owned }: { place: Place; owned: boolean }) {
  const router = useRouter();
  const config = usePlacesConfig();
  const label = useCategoryLabel();
  const remove = useDeletePlace();
  const point = { lat: place.lat, lng: place.lng };

  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-3">
        <PageHeader
          title={place.name}
          action={
            <>
              {place.status === "approved" ? <FavoriteButton placeId={place.id} /> : null}
              {owned ? (
                <Button asChild variant="secondary" size="sm" leftIcon={<Pencil />}>
                  <Link href={`/places/${place.id}/edit`}>Edit</Link>
                </Button>
              ) : null}
            </>
          }
        />
        <div className="flex flex-wrap items-center gap-2">
          <span className="type-label text-fg-muted">{label(place.category)}</span>
          <OpenBadge hours={place.hours} />
          {place.status !== "approved" ? <PlaceStatusBadge status={place.status} /> : null}
        </div>
        {place.status === "rejected" && place.review_note ? <Banner tone="danger">{place.review_note}</Banner> : null}
      </div>

      {place.photos.length > 0 ? <PhotoGrid photos={place.photos.map((p) => ({ id: p.id, url: p.url }))} columns={3} /> : null}

      <div className="grid gap-section lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        <div className="flex flex-col gap-section">
          {place.description ? <p className="max-w-prose whitespace-pre-line type-body text-fg">{place.description}</p> : null}
          <Section title="Contact">
            <div className="flex flex-col gap-2">
              {place.address ? <p className="type-body text-fg">{place.address}</p> : null}
              <div className="flex flex-wrap gap-2">
                <Button asChild variant="secondary" size="auto" leftIcon={<Navigation />}>
                  <a href={directionsUrl(point)} target="_blank" rel="noopener noreferrer">
                    Directions
                  </a>
                </Button>
                {place.phone ? (
                  <Button asChild variant="secondary" size="auto" leftIcon={<Phone />}>
                    <a href={`tel:${place.phone.replace(/[^\d+]/g, "")}`}>{place.phone}</a>
                  </Button>
                ) : null}
                {place.website ? (
                  <Button asChild variant="secondary" size="auto" leftIcon={<Globe />}>
                    <a href={place.website} target="_blank" rel="noopener noreferrer nofollow ugc">
                      {hostOf(place.website)}
                    </a>
                  </Button>
                ) : null}
              </div>
            </div>
          </Section>
          {place.hours ? (
            <Section title="Hours" info={place.hours.timezone ? `Times are in ${place.hours.timezone.replace(/_/g, " ")}.` : undefined}>
              <HoursTable hours={place.hours} />
            </Section>
          ) : null}
        </div>
        {config.data ? (
          <MapView
            label={`Map showing ${place.name}`}
            styleUrl={config.data.map_style_url}
            center={point}
            zoom={15}
            interactive={false}
            pins={[{ id: place.id, lat: place.lat, lng: place.lng, label: place.name, selected: true }]}
            className="h-72 rounded-lg border border-border"
          />
        ) : null}
      </div>

      {owned ? (
        <div>
          <ConfirmDialog
            trigger={
              <Button variant="danger-ghost" size="auto" leftIcon={<Trash2 />}>
                Delete place
              </Button>
            }
            title={`Delete ${place.name}?`}
            confirmLabel="Delete place"
            tone="danger"
            onConfirm={async () => {
              await remove.mutateAsync(place.id);
              toast.success("Place deleted");
              router.replace("/places/saved?tab=mine");
            }}
            errorText={(e) => errorMessage(e, "Couldn't delete it. Try again.")}
          >
            Its photos go too. This can't be undone.
          </ConfirmDialog>
        </div>
      ) : null}
    </div>
  );
}

/** A place (`/places/[id]`): approved places, or your own in any status. */
export function PlaceDetailScreen({ id }: { id: string }) {
  const view = usePlaceView(id);
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-2 px-gutter py-section">
      <Link href="/places" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Places
      </Link>
      {view.isPending ? (
        <SkeletonGroup label="Loading place" className="flex flex-col gap-section">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-48" />
          <Skeleton className="h-32 max-w-prose" />
        </SkeletonGroup>
      ) : view.isError ? (
        isNotFound(view.error) ? (
          <EmptyState
            icon={<MapPinOff />}
            title="This place isn't available"
            action={
              <Button asChild variant="secondary" size="auto">
                <Link href="/places">Browse places</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState title="Couldn't load this place" info={errorMessage(view.error)} onRetry={() => void view.refetch()} retrying={view.isFetching} />
        )
      ) : (
        <Body place={view.data.place} owned={view.data.owned} />
      )}
    </div>
  );
}
