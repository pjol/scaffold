"use client";

import { errorMessage, usePlace, usePlaceClusters, usePlaceSearch, usePlacesConfig, useDebouncedValue, type LatLng, type PlaceSummary } from "@scaffoldapp/shared";
import { Banner, Button, Card, EmptyState, ErrorState, IconButton, Skeleton, SkeletonGroup, SkeletonRow } from "@scaffoldapp/ui-web";
import { Bookmark, MapPin, Plus, SearchX, X } from "lucide-react";
import Link from "next/link";
import { useMemo, useRef, useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { LoadMore } from "@/components/shell/LoadMore";
import { useUrlState } from "@/lib/url-state";
import { MapView, type MapPin as Pin, type MapViewHandle, type MapViewport } from "./MapView";
import { browserLocation, CategoryChips, FavoriteButton, OpenBadge, PlaceRow, PlaceThumb, useCategoryLabel } from "./parts";

/** Selected place over the map: photo, name, open state, details link. */
function Preview({ id, onClose }: { id: string; onClose: () => void }) {
  const place = usePlace(id);
  const label = useCategoryLabel();
  return (
    <Card padding="compact" className="absolute inset-x-3 bottom-10 z-10 flex items-center gap-3 shadow-lg sm:right-auto sm:w-96">
      {place.data ? (
        <>
          <PlaceThumb url={place.data.photos[0]?.url ?? null} name={place.data.name} className="size-14" />
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <Link href={`/places/${id}`} className="truncate type-label font-semibold text-fg underline-offset-2 hover:underline">
              {place.data.name}
            </Link>
            <div className="flex flex-wrap items-center gap-2">
              <span className="type-caption text-fg-muted">{label(place.data.category)}</span>
              <OpenBadge hours={place.data.hours} />
            </div>
          </div>
          <FavoriteButton placeId={id} size="sm" />
        </>
      ) : place.isError ? (
        <p className="flex-1 type-label text-fg-muted">{errorMessage(place.error, "Couldn't load this place.")}</p>
      ) : (
        <SkeletonGroup className="flex flex-1 items-center gap-3">
          <Skeleton className="size-14" />
          <Skeleton shape="text" className="h-4 flex-1" />
        </SkeletonGroup>
      )}
      <IconButton label="Close" icon={<X />} size="sm" onClick={onClose} />
    </Card>
  );
}

/**
 * Places (`/places`): the map with server-side clustering and, always beside it, the list
 * of places in view (nearest first once the user shares their location). Search and
 * category filters apply to both and live in the URL.
 */
export function PlacesScreen() {
  const config = usePlacesConfig();
  const [q, setQ] = useUrlState("q", "");
  const [cat, setCat] = useUrlState("category", "");
  const [selected, setSelected] = useUrlState("place", "");
  const categories = useMemo(() => (cat ? cat.split(",").filter(Boolean) : []), [cat]);
  const [viewport, setViewport] = useState<MapViewport | null>(null);
  const view = useDebouncedValue(viewport, 200);
  const [near, setNear] = useState<LatLng | null>(null);
  const map = useRef<MapViewHandle>(null);

  const clusters = usePlaceClusters(view ? { bbox: view.bbox, zoom: view.zoom, category: categories, q } : null);
  const list = usePlaceSearch({ bbox: view?.bbox, category: categories, q, near, limit: 30 }, { enabled: !!view });
  const rows = list.data?.items ?? [];
  const filtered = !!q || categories.length > 0;

  const pins = useMemo<Pin[]>(() => {
    const data = clusters.data;
    if (!data) return [];
    return [
      ...data.clusters.map((c) => ({ id: `c:${c.id}`, lat: c.lat, lng: c.lng, count: c.count, label: `${c.count} places` })),
      ...data.places.map((p) => ({ id: p.id, lat: p.lat, lng: p.lng, label: p.name, selected: p.id === selected })),
    ];
  }, [clusters.data, selected]);

  const onPin = (pin: Pin) => {
    if (pin.id.startsWith("c:")) {
      const c = clusters.data?.clusters.find((x) => `c:${x.id}` === pin.id);
      if (!c) return;
      const [w, s, e, n] = c.bbox;
      if (e - w < 1e-5 && n - s < 1e-5) map.current?.flyTo({ lat: c.lat, lng: c.lng }, (view?.zoom ?? 14) + 3);
      else map.current?.fitBounds(c.bbox);
      return;
    }
    setSelected(pin.id);
    // Desktop: bring the row into view in the list column (phones keep the map in place).
    if (window.matchMedia("(min-width: 1024px)").matches) {
      document.querySelector(`[data-place-id="${pin.id}"]`)?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  };

  const selectRow = (p: PlaceSummary) => {
    setSelected(p.id);
    map.current?.flyTo({ lat: p.lat, lng: p.lng });
  };

  const locate = async () => {
    const p = await browserLocation();
    if (p) setNear(p);
    return p;
  };

  if (config.isPending) {
    return (
      <SkeletonGroup label="Loading places" className="flex flex-1 flex-col lg:flex-row">
        <div className="flex flex-col gap-3 px-gutter py-4 lg:w-96">
          <Skeleton className="h-control" />
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </div>
        <Skeleton className="h-[55dvh] flex-1 rounded-none lg:h-[calc(100dvh_-_var(--app-size-topbar))]" />
      </SkeletonGroup>
    );
  }
  if (config.isError || !config.data) {
    return (
      <div className="mx-auto w-full max-w-content px-gutter py-section">
        <ErrorState title="Couldn't load places" info={errorMessage(config.error)} onRetry={() => void config.refetch()} retrying={config.isFetching} />
      </div>
    );
  }

  const listBody = list.isPending ? (
    <SkeletonGroup label="Loading places" className="flex flex-col gap-1 px-3">
      {Array.from({ length: 6 }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </SkeletonGroup>
  ) : list.isError ? (
    <ErrorState compact title="Couldn't load places" info={errorMessage(list.error)} onRetry={() => void list.refetch()} retrying={list.isFetching} />
  ) : rows.length === 0 ? (
    <EmptyState
      compact
      icon={filtered ? <SearchX /> : <MapPin />}
      title={filtered ? "No matching places here" : "No places in this area"}
      action={
        filtered ? (
          <Button
            variant="secondary"
            size="auto"
            onClick={() => {
              setQ("");
              setCat("");
            }}
          >
            Clear filters
          </Button>
        ) : (
          <Button asChild variant="secondary" size="auto" leftIcon={<Plus />}>
            <Link href="/places/new">Add a place</Link>
          </Button>
        )
      }
    />
  ) : (
    <>
      <ul aria-label="Places in view" className="flex flex-col gap-1 px-1">
        {rows.map((p) => (
          <PlaceRow key={p.id} place={p} selected={p.id === selected} onSelect={selectRow} />
        ))}
      </ul>
      <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
    </>
  );

  // Phones: header · map · list. Desktop: header and list in a 384 px column beside the map.
  return (
    <div className="grid flex-1 grid-cols-1 [grid-template-areas:'header'_'map'_'list'] lg:h-[calc(100dvh_-_var(--app-size-topbar))] lg:flex-none lg:grid-cols-[24rem_minmax(0,1fr)] lg:grid-rows-[auto_minmax(0,1fr)] lg:[grid-template-areas:'header_map'_'list_map']">
      <div className="flex flex-col gap-3 px-gutter py-4 [grid-area:header] lg:border-r lg:border-border lg:px-4">
        <div className="flex items-center gap-2">
          <h1 className="min-w-0 flex-1 truncate type-title text-fg">Places</h1>
          <Button asChild variant="ghost" size="sm" leftIcon={<Bookmark />}>
            <Link href="/places/saved">Saved</Link>
          </Button>
          <Button asChild size="sm" leftIcon={<Plus />}>
            <Link href="/places/new">Add</Link>
          </Button>
        </div>
        <SearchInput value={q} onSearch={setQ} placeholder="Name or address" label="Search places" />
        <CategoryChips value={categories} onChange={(next) => setCat(next.join(","))} />
      </div>
      <MapView
        ref={map}
        label="Map of places"
        styleUrl={config.data.map_style_url}
        center={config.data.default_center}
        zoom={config.data.default_zoom}
        pins={pins}
        onViewportChange={setViewport}
        onPinClick={onPin}
        onLocate={locate}
        className="h-[55dvh] [grid-area:map] lg:h-full"
      >
        {clusters.data?.truncated ? (
          <div className="absolute top-3 left-3 max-w-72">
            <Banner tone="info">Zoom in to see every place.</Banner>
          </div>
        ) : null}
        {selected ? <Preview id={selected} onClose={() => setSelected("")} /> : null}
      </MapView>
      <section aria-label="Places list" className="flex min-h-0 flex-col gap-2 px-gutter py-4 [grid-area:list] lg:overflow-y-auto lg:border-r lg:border-border lg:px-3 lg:pt-0">
        {near ? (
          <div className="flex items-center gap-2 px-1 type-caption text-fg-muted">
            <span className="flex-1">Nearest first</span>
            <Button variant="ghost" size="sm" onClick={() => setNear(null)}>
              Newest first
            </Button>
          </div>
        ) : null}
        {listBody}
      </section>
    </div>
  );
}
