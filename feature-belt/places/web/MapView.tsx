"use client";

import "maplibre-gl/dist/maplibre-gl.css";
import "./map.css";

import type { BBox, LatLng } from "@scaffoldapp/shared";
import { IconButton, cn } from "@scaffoldapp/ui-web";
import { LocateFixed, Minus, Plus } from "lucide-react";
import type { Map as MapLibreMap, Marker as MapLibreMarker } from "maplibre-gl";
import { forwardRef, useEffect, useImperativeHandle, useRef, useState, type ReactNode } from "react";

/** A pin: one place, or a cluster of `count` places. */
export interface MapPin {
  id: string;
  lat: number;
  lng: number;
  /** Accessible name ("Tartine Bakery", "12 places"). */
  label: string;
  count?: number;
  selected?: boolean;
}

export interface MapViewport {
  bbox: BBox;
  zoom: number;
  center: LatLng;
}

export interface MapViewHandle {
  flyTo: (point: LatLng, zoom?: number) => void;
  fitBounds: (bbox: BBox) => void;
}

export interface MapViewProps {
  styleUrl: string;
  center: LatLng;
  zoom: number;
  pins: readonly MapPin[];
  /** Accessible name of the map region. */
  label: string;
  /** After every pan or zoom settles (and once on load). */
  onViewportChange?: (view: MapViewport) => void;
  onPinClick?: (pin: MapPin) => void;
  /** Clicks on the map itself (dropping a pin). */
  onMapClick?: (point: LatLng) => void;
  /** Shows a locate button; resolve to the user's position or null when unavailable. */
  onLocate?: () => Promise<LatLng | null>;
  /** Pan and zoom by the user (off for the small map on a detail page). */
  interactive?: boolean;
  /** Overlays inside the map frame (a preview card). */
  children?: ReactNode;
  className?: string;
}

const wrapLng = (lng: number) => ((((lng + 180) % 360) + 360) % 360) - 180;

/** The box a map of `width`×`height` px would show at `center`/`zoom` (when the map can't load). */
function approxViewport(center: LatLng, zoom: number, width: number, height: number): MapViewport {
  const lngSpan = Math.min(360, (360 / 2 ** zoom) * (Math.max(width, 256) / 256));
  const latSpan = Math.min(180, lngSpan * (Math.max(height, 256) / Math.max(width, 256)) * Math.cos((center.lat * Math.PI) / 180));
  return {
    center,
    zoom,
    bbox: [Math.max(-180, center.lng - lngSpan / 2), Math.max(-90, center.lat - latSpan / 2), Math.min(180, center.lng + lngSpan / 2), Math.min(90, center.lat + latSpan / 2)],
  };
}

function viewportOf(map: MapLibreMap): MapViewport {
  const b = map.getBounds();
  let west = b.getWest();
  let east = b.getEast();
  if (east - west >= 360) {
    west = -180;
    east = 180;
  } else {
    west = wrapLng(west);
    east = wrapLng(east);
  }
  const c = map.getCenter();
  return {
    bbox: [west, Math.max(-90, b.getSouth()), east, Math.min(90, b.getNorth())],
    zoom: map.getZoom(),
    center: { lat: c.lat, lng: wrapLng(c.lng) },
  };
}

// The button keeps MapLibre's own classes (positioning); visuals live on an inner span.
const BUTTON_CLASSES = ["group", "flex", "min-h-8", "min-w-8", "items-center", "justify-center", "rounded-pill", "outline-none", "cursor-pointer", "focus-visible:ring-2", "focus-visible:ring-ring"];
const DOT_CLASS = "block rounded-pill border-2 border-surface shadow-sm transition-transform duration-(--app-duration-fast) group-hover:scale-110";
const CLUSTER_CLASS =
  "flex h-8 min-w-8 items-center justify-center rounded-pill border-2 border-surface bg-primary px-2 type-caption font-semibold text-primary-fg tabular-nums shadow-sm";

function renderPin(el: HTMLButtonElement, pin: MapPin) {
  el.setAttribute("aria-label", pin.label);
  el.title = pin.label;
  let inner = el.firstElementChild as HTMLSpanElement | null;
  if (!inner) {
    inner = document.createElement("span");
    el.replaceChildren(inner);
  }
  if (pin.count && pin.count > 1) {
    el.removeAttribute("aria-pressed");
    inner.className = CLUSTER_CLASS;
    inner.textContent = pin.count > 999 ? "999+" : String(pin.count);
    return;
  }
  el.setAttribute("aria-pressed", pin.selected ? "true" : "false");
  inner.textContent = "";
  inner.className = cn(DOT_CLASS, pin.selected ? "size-5 bg-accent" : "size-4 bg-primary");
}

/**
 * MapLibre GL map (style from `GET /v1/places/config`) with token-styled pins and
 * controls. Pins are buttons, so the keyboard reaches them; the page always lists the same
 * places beside the map.
 */
export const MapView = forwardRef<MapViewHandle, MapViewProps>(function MapView(
  { styleUrl, center, zoom, pins, label, onViewportChange, onPinClick, onMapClick, onLocate, interactive = true, children, className },
  ref,
) {
  const container = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const markers = useRef(new Map<string, { marker: MapLibreMarker; el: HTMLButtonElement; pin: MapPin }>());
  const markerCtor = useRef<typeof MapLibreMarker | null>(null);
  const [ready, setReady] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [locating, setLocating] = useState(false);
  const handlers = useRef({ onViewportChange, onPinClick, onMapClick });
  handlers.current = { onViewportChange, onPinClick, onMapClick };
  const initial = useRef({ center, zoom, styleUrl, interactive });

  // Without a map the list still needs an area: the one the map would have shown.
  useEffect(() => {
    if (!failure) return;
    const el = container.current;
    handlers.current.onViewportChange?.(approxViewport(initial.current.center, initial.current.zoom, el?.clientWidth ?? 800, el?.clientHeight ?? 600));
  }, [failure]);

  useEffect(() => {
    let cancelled = false;
    let map: MapLibreMap | null = null;
    void (async () => {
      try {
        const lib = await import("maplibre-gl");
        if (cancelled || !container.current) return;
        // Served by app/maplibre-gl-worker.mjs/route.ts (bundlers break MapLibre's own worker URL).
        if (!lib.getWorkerUrl().includes("/maplibre-gl-worker.mjs?v=")) lib.setWorkerUrl(`/maplibre-gl-worker.mjs?v=${lib.getVersion()}`);
        const init = initial.current;
        map = new lib.Map({
          container: container.current,
          style: init.styleUrl,
          center: [init.center.lng, init.center.lat],
          zoom: init.zoom,
          attributionControl: { compact: true },
          dragRotate: false,
          pitchWithRotate: false,
          touchPitch: false,
          interactive: init.interactive,
          cooperativeGestures: false,
          // Third-party styles (OpenFreeMap) carry harmless filter warnings; skip client-side validation.
          validateStyle: false,
        });
        map.touchZoomRotate.disableRotation();
        map.keyboard.disableRotation();
        markerCtor.current = lib.Marker;
        mapRef.current = map;
        const emit = () => map && handlers.current.onViewportChange?.(viewportOf(map));
        // The camera is known at construction: report it and place pins before tiles finish loading.
        setReady(true);
        emit();
        map.on("moveend", emit);
        map.on("click", (e) => handlers.current.onMapClick?.({ lat: e.lngLat.lat, lng: wrapLng(e.lngLat.lng) }));
        map.on("error", (e) => {
          // Tile hiccups are transient; a style that never loads is not.
          if (!map?.isStyleLoaded() && !map?.loaded()) setFailure(e.error?.message ?? "style");
        });
      } catch {
        if (!cancelled) setFailure("webgl");
      }
    })();
    const markerMap = markers.current;
    return () => {
      cancelled = true;
      markerMap.clear();
      map?.remove();
      mapRef.current = null;
    };
  }, []);

  // Pins: add, update and remove by id.
  useEffect(() => {
    const map = mapRef.current;
    const Marker = markerCtor.current;
    if (!map || !Marker || !ready) return;
    const next = new Set(pins.map((p) => p.id));
    for (const [id, m] of markers.current) {
      if (!next.has(id)) {
        m.marker.remove();
        markers.current.delete(id);
      }
    }
    for (const pin of pins) {
      const existing = markers.current.get(pin.id);
      if (existing) {
        existing.pin = pin;
        renderPin(existing.el, pin);
        existing.marker.setLngLat([pin.lng, pin.lat]);
        continue;
      }
      const el = document.createElement("button");
      el.type = "button";
      el.classList.add(...BUTTON_CLASSES);
      const entry = { marker: new Marker({ element: el, anchor: "center" }).setLngLat([pin.lng, pin.lat]).addTo(map), el, pin };
      el.addEventListener("click", (e) => {
        e.stopPropagation();
        handlers.current.onPinClick?.(entry.pin);
      });
      renderPin(el, pin);
      markers.current.set(pin.id, entry);
    }
  }, [pins, ready]);

  useImperativeHandle(
    ref,
    () => ({
      flyTo: (p, z) => mapRef.current?.easeTo({ center: [p.lng, p.lat], zoom: z ?? Math.max(mapRef.current.getZoom(), 14), duration: 600 }),
      fitBounds: (b) => mapRef.current?.fitBounds([b[0], b[1], b[2], b[3]], { padding: 64, maxZoom: 17, duration: 600 }),
    }),
    [],
  );

  const zoomBy = (d: number) => mapRef.current?.easeTo({ zoom: mapRef.current.getZoom() + d, duration: 250 });
  const locate = async () => {
    if (!onLocate) return;
    setLocating(true);
    try {
      const p = await onLocate();
      if (p) mapRef.current?.easeTo({ center: [p.lng, p.lat], zoom: Math.max(mapRef.current.getZoom(), 14), duration: 600 });
    } finally {
      setLocating(false);
    }
  };

  return (
    <div className={cn("places-map relative isolate overflow-hidden bg-sunken", className)}>
      {/* maplibre-gl.css makes the container position: relative, so size it with h-full. */}
      <div ref={container} role="region" aria-label={label} className="h-full w-full" />
      {failure ? (
        <div className="absolute inset-0 flex items-center justify-center p-card text-center type-label text-fg-muted">
          {failure === "webgl" ? "This browser can't show the map. The list has every place." : "Couldn't load the map. The list has every place."}
        </div>
      ) : null}
      {interactive && !failure ? (
        <div className="absolute top-3 right-3 flex flex-col gap-2">
          <div className="flex flex-col overflow-hidden rounded-md border border-border bg-surface">
            <IconButton label="Zoom in" icon={<Plus />} variant="ghost" shape="square" onClick={() => zoomBy(1)} />
            <IconButton label="Zoom out" icon={<Minus />} variant="ghost" shape="square" onClick={() => zoomBy(-1)} />
          </div>
          {onLocate ? (
            <div className="overflow-hidden rounded-md border border-border bg-surface">
              <IconButton label="Show my location" tooltip icon={<LocateFixed />} variant="ghost" shape="square" loading={locating} onClick={() => void locate()} />
            </div>
          ) : null}
        </div>
      ) : null}
      {children}
    </div>
  );
});
