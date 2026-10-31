/** DTOs for the places feature (backend/internal/places). snake_case, same shapes as the Go structs. */
import type { PageParams } from "../api/params";

export type PlaceStatus = "pending" | "approved" | "rejected";

/** `minLng, minLat, maxLng, maxLat` (GeoJSON order). minLng > maxLng crosses the antimeridian. */
export type BBox = [number, number, number, number];

export interface LatLng {
  lat: number;
  lng: number;
}

export interface PlaceCategory {
  id: string;
  label: string;
}

/** `GET /v1/places/config` (public). */
export interface PlacesConfig {
  categories: PlaceCategory[];
  /** MapLibre style JSON (web). */
  map_style_url: string;
  default_center: LatLng;
  default_zoom: number;
  /** Submissions wait for staff review. */
  require_approval: boolean;
  /** Places outside are refused; null = anywhere. */
  service_area: BBox | null;
  max_photos: number;
  /** Address search provider, for attribution. */
  geocoder: "photon" | "mapbox";
}

/** One opening, "HH:MM" 24-hour local time; close may be "24:00" or earlier than open (past midnight). */
export interface PlaceInterval {
  open: string;
  close: string;
}

export type Weekday = "mon" | "tue" | "wed" | "thu" | "fri" | "sat" | "sun";

/** Monday first; an empty day is closed. */
export type PlaceWeek = Record<Weekday, PlaceInterval[]>;

export interface PlaceHours {
  /** IANA zone; "" = the viewer's local time. */
  timezone: string;
  note: string;
  weekly: PlaceWeek;
}

export interface PlacePhoto {
  id: string;
  url: string;
}

/** A full listing (`GET /v1/places/{id}`, `/mine`). */
export interface Place {
  id: string;
  name: string;
  category: string;
  description: string;
  address: string;
  lat: number;
  lng: number;
  phone: string;
  website: string;
  /** null = unknown. */
  hours: PlaceHours | null;
  photos: PlacePhoto[];
  status: PlaceStatus;
  /** Rejection reason (owner and staff views). */
  review_note: string;
  created_at: string;
  updated_at: string;
}

/** `GET /v1/places/{id}/viewer`: the place with the caller's relation to it. */
export interface PlaceView {
  place: Place;
  owned: boolean;
  favorite: boolean;
}

/** A map pin or list row. */
export interface PlaceSummary {
  id: string;
  name: string;
  category: string;
  address: string;
  lat: number;
  lng: number;
  photo_url: string | null;
  /** Metres from `near` (nearest-first searches only). */
  distance_m: number | null;
}

/** A grid cell holding two or more places. */
export interface PlaceCluster {
  /** `<zoom>:<x>:<y>`, stable for a zoom level. */
  id: string;
  lat: number;
  lng: number;
  count: number;
  /** The members' extent. */
  bbox: BBox;
}

/** `GET /v1/places?cluster=true&zoom=&bbox=`. */
export interface PlaceClusterResult {
  clusters: PlaceCluster[];
  places: PlaceSummary[];
  /** Over 1000 cells: zoom in. */
  truncated: boolean;
}

export interface FavoritePlace extends PlaceSummary {
  favorited_at: string;
}

/** One address search match (`GET /v1/places/geocode`, `/reverse`). */
export interface GeoResult {
  label: string;
  /** Venue or area name when the match is not a street address. */
  name: string;
  street: string;
  city: string;
  region: string;
  postcode: string;
  /** ISO 3166-1 alpha-2, upper case. */
  country_code: string;
  lat: number;
  lng: number;
}

/** Create/update body. Absent fields keep their value on update; `hours: null` clears them. */
export interface PlaceInput {
  name?: string | undefined;
  category?: string | undefined;
  description?: string | undefined;
  address?: string | undefined;
  lat?: number | undefined;
  lng?: number | undefined;
  phone?: string | undefined;
  website?: string | undefined;
  hours?: PlaceHours | null | undefined;
  /** Create only: keys from `POST /v1/uploads` with purpose `place_photo`. */
  photo_keys?: string[] | undefined;
}

export interface PlaceOwner {
  id: string;
  email: string;
  display_name: string;
}

/** A place as staff see it. */
export interface AdminPlace extends Place {
  /** null for staff-made listings and deleted owners. */
  owner: PlaceOwner | null;
  reviewed_by: string | null;
  reviewed_at: string | null;
}

/** `GET /v1/places` list and nearest modes. */
export interface PlaceSearchParams extends PageParams {
  bbox?: BBox | null | undefined;
  /** Category ids (any of). */
  category?: readonly string[] | undefined;
  /** Text matched against name and address (≤ 100 characters). */
  q?: string | undefined;
  /** Nearest first, with `distance_m`. */
  near?: LatLng | null | undefined;
  /** With `near`; default 50 km. */
  radius_m?: number | undefined;
}

/** `GET /v1/places?cluster=true`. */
export interface PlaceClusterParams {
  bbox: BBox;
  /** Map zoom, 0 to 22 (rounded). */
  zoom: number;
  category?: readonly string[] | undefined;
  q?: string | undefined;
}

export interface AdminPlaceListParams extends PageParams {
  status?: PlaceStatus | undefined;
  q?: string | undefined;
}

declare module "../types" {
  interface RealtimeEventMap {
    /** On `admin:live` when a place enters the review queue. */
    "places.pending": { id: string };
  }
}
