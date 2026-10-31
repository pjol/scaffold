import type { BBox, LatLng } from "@scaffoldapp/shared";
import { useTheme } from "@scaffoldapp/ui-native";
import { AppleMaps, GoogleMaps, type CameraMoveEvent } from "expo-maps";
import { forwardRef, useImperativeHandle, useMemo, useRef } from "react";
import { Platform, type StyleProp, type ViewStyle } from "react-native";

export interface NativePin {
  /** Place id, or `c:<cluster id>`. */
  id: string;
  lat: number;
  lng: number;
  title: string;
  count?: number;
}

export interface NativeViewport {
  bbox: BBox;
  zoom: number;
  center: LatLng;
}

export interface PlacesMapHandle {
  moveTo: (p: LatLng, zoom?: number) => void;
}

// Only this app's pins are tappable: platform points of interest stay hidden.
const GOOGLE_POI_OFF = JSON.stringify([{ featureType: "poi", stylers: [{ visibility: "off" }] }]);

const wrap = (lng: number) => ((((lng + 180) % 360) + 360) % 360) - 180;

function viewportOf(e: CameraMoveEvent): NativeViewport {
  const lat = e.coordinates.latitude ?? 0;
  const lng = e.coordinates.longitude ?? 0;
  const dLat = e.latitudeDelta / 2;
  const dLng = e.longitudeDelta / 2;
  const span = e.longitudeDelta >= 360;
  return {
    bbox: [span ? -180 : wrap(lng - dLng), Math.max(-90, lat - dLat), span ? 180 : wrap(lng + dLng), Math.min(90, lat + dLat)],
    zoom: e.zoom,
    center: { lat, lng: wrap(lng) },
  };
}

/**
 * expo-maps: Apple Maps on iOS (17+, no key), Google Maps on Android (key from
 * EXPO_PUBLIC_GOOGLE_MAPS_ANDROID_KEY via plugins/withPlaces.js). Pins use the `markers`
 * prop: on iOS only markers (not annotations) are tappable, and only on iOS 18+, so the
 * screen always offers the list too. `epoch` re-keys markers to clear MapKit's stuck
 * selection.
 */
export const PlacesMap = forwardRef<
  PlacesMapHandle,
  { pins: readonly NativePin[]; initial: { center: LatLng; zoom: number }; onViewport?: (v: NativeViewport) => void; onPinPress?: (id: string) => void; showsUser?: boolean; epoch?: number; style?: StyleProp<ViewStyle> }
>(function PlacesMap({ pins, initial, onViewport, onPinPress, showsUser, epoch = 0, style }, ref) {
  const theme = useTheme();
  const c = theme.colors;
  const apple = useRef<AppleMaps.MapView>(null);
  const google = useRef<GoogleMaps.MapView>(null);
  // The camera prop is the starting point only; later moves go through the ref.
  const camera = useMemo(() => ({ coordinates: { latitude: initial.center.lat, longitude: initial.center.lng }, zoom: initial.zoom }), []); // eslint-disable-line react-hooks/exhaustive-deps
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const onMove = (e: CameraMoveEvent) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => onViewport?.(viewportOf(e)), 250);
  };
  const press = (id: string | undefined) => {
    if (id) onPinPress?.(id.split("#")[0] ?? id);
  };

  useImperativeHandle(ref, () => ({
    moveTo: (p, zoom) => {
      const cam = { coordinates: { latitude: p.lat, longitude: p.lng }, zoom: zoom ?? 15 };
      if (Platform.OS === "ios") apple.current?.setCameraPosition(cam);
      else google.current?.setCameraPosition(cam);
    },
  }));

  if (Platform.OS === "ios") {
    return (
      <AppleMaps.View
        ref={apple}
        style={[{ flex: 1 }, style]}
        cameraPosition={camera}
        colorScheme={theme.mode === "dark" ? AppleMaps.MapColorScheme.DARK : AppleMaps.MapColorScheme.LIGHT}
        markers={pins.map((p) => ({
          id: `${p.id}#${epoch}`,
          coordinates: { latitude: p.lat, longitude: p.lng },
          title: p.title,
          tintColor: c.primary,
          ...(p.count && p.count > 1 ? { monogram: p.count > 99 ? "99+" : String(p.count) } : { systemImage: "mappin" }),
        }))}
        uiSettings={{ compassEnabled: false, myLocationButtonEnabled: false, togglePitchEnabled: false, scaleBarEnabled: false }}
        properties={{ pointsOfInterest: { including: [] }, isMyLocationEnabled: !!showsUser }}
        onMarkerClick={(m) => press(m.id)}
        onCameraMove={onMove}
      />
    );
  }
  return (
    <GoogleMaps.View
      ref={google}
      style={[{ flex: 1 }, style]}
      cameraPosition={camera}
      colorScheme={theme.mode === "dark" ? GoogleMaps.MapColorScheme.DARK : GoogleMaps.MapColorScheme.LIGHT}
      markers={pins.map((p) => ({
        id: `${p.id}#${epoch}`,
        coordinates: { latitude: p.lat, longitude: p.lng },
        title: p.title,
        ...(p.count && p.count > 1 ? { snippet: `${p.count} places` } : {}),
      }))}
      uiSettings={{ compassEnabled: false, myLocationButtonEnabled: false, mapToolbarEnabled: false, zoomControlsEnabled: false, tiltGesturesEnabled: false }}
      properties={{ isMyLocationEnabled: !!showsUser, mapStyleOptions: { json: GOOGLE_POI_OFF } }}
      onMarkerClick={(m) => press(m.id)}
      onCameraMove={onMove}
    />
  );
});
