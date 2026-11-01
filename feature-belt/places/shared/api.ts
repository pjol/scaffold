import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminPlace,
  AdminPlaceListParams,
  BBox,
  FavoritePlace,
  GeoResult,
  LatLng,
  Place,
  PlaceClusterParams,
  PlaceClusterResult,
  PlaceInput,
  PlacesConfig,
  PlaceSearchParams,
  PlaceSummary,
  PlaceView,
} from "./types";

const fmt = (n: number) => String(Math.round(n * 1e6) / 1e6);
const bboxParam = (b: BBox | null | undefined) => (b ? b.map(fmt).join(",") : undefined);
const pointParam = (p: LatLng | null | undefined) => (p ? `${fmt(p.lat)},${fmt(p.lng)}` : undefined);
const categoryParam = (c: readonly string[] | undefined) => (c && c.length ? c.join(",") : undefined);

/** Places endpoints. Reach them with `useEndpoints(placesEndpoints)` or `endpointsFor(api.http, placesEndpoints)`. */
export function placesEndpoints(http: HttpClient) {
  const P = "/v1/places";
  const A = "/v1/admin/places";
  return {
    /** `GET /v1/places/config` (public). */
    config: (o?: CallOptions) => http.get<PlacesConfig>(`${P}/config`, { ...o, auth: false }),
    /** `GET /v1/places`: approved places, newest first; nearest first with `near`. Public. */
    search: (params: PlaceSearchParams = {}, o?: CallOptions) =>
      http.get<Page<PlaceSummary>>(P, {
        ...o,
        query: {
          bbox: bboxParam(params.bbox),
          category: categoryParam(params.category),
          q: params.q || undefined,
          near: pointParam(params.near),
          radius_m: params.near ? params.radius_m : undefined,
          limit: params.limit,
          cursor: params.cursor,
        },
      }),
    /** `GET /v1/places?cluster=true`: grid clusters and single places in a bbox. Public. */
    clusters: (params: PlaceClusterParams, o?: CallOptions) =>
      http.get<PlaceClusterResult>(P, {
        ...o,
        query: {
          cluster: true,
          zoom: Math.max(0, Math.min(22, Math.round(params.zoom))),
          bbox: bboxParam(params.bbox),
          category: categoryParam(params.category),
          q: params.q || undefined,
        },
      }),
    /** `GET /v1/places/{id}`: an approved place. Public. */
    get: (id: string, o?: CallOptions) => http.get<Place>(`${P}/${seg(id)}`, o),
    /** `GET /v1/places/mine`: the caller's submissions in every status. */
    mine: (params: { cursor?: string | null | undefined; limit?: number | undefined } = {}, o?: CallOptions) =>
      http.get<Page<Place>>(`${P}/mine`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** `GET /v1/places/{id}/viewer` (signed in): approved or the caller's own (any status), with `owned` and `favorite`. */
    view: (id: string, o?: CallOptions) => http.get<PlaceView>(`${P}/${seg(id)}/viewer`, o),
    /** `POST /v1/places`: submit (pending when the app requires approval). */
    create: (body: PlaceInput, o?: CallOptions) => http.post<Place>(P, body, o),
    /** `PATCH /v1/places/{id}` (owner). With approval on, an edit sends the place back to review. */
    update: (id: string, body: PlaceInput, o?: CallOptions) => http.patch<Place>(`${P}/${seg(id)}`, body, o),
    /** `DELETE /v1/places/{id}` (owner). */
    remove: (id: string, o?: CallOptions) => http.delete<void>(`${P}/${seg(id)}`, o),
    /** `POST /v1/places/{id}/photos` (owner): attach an uploaded `place_photo` key. */
    addPhoto: (id: string, key: string, o?: CallOptions) => http.post<Place>(`${P}/${seg(id)}/photos`, { key }, o),
    /** `DELETE /v1/places/{id}/photos/{photoId}` (owner). */
    removePhoto: (id: string, photoId: string, o?: CallOptions) => http.delete<void>(`${P}/${seg(id)}/photos/${seg(photoId)}`, o),
    /** `GET /v1/places/favorites`: newest first. */
    favorites: (params: { cursor?: string | null | undefined; limit?: number | undefined } = {}, o?: CallOptions) =>
      http.get<Page<FavoritePlace>>(`${P}/favorites`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** `GET /v1/places/favorites/ids`: up to 1000 ids, for marking pins. */
    favoriteIds: (o?: CallOptions) => http.get<{ ids: string[] }>(`${P}/favorites/ids`, o),
    /** `PUT /v1/places/{id}/favorite` (idempotent). */
    addFavorite: (id: string, o?: CallOptions) => http.put<void>(`${P}/${seg(id)}/favorite`, undefined, o),
    /** `DELETE /v1/places/{id}/favorite` (idempotent). */
    removeFavorite: (id: string, o?: CallOptions) => http.delete<void>(`${P}/${seg(id)}/favorite`, o),
    /** `GET /v1/places/geocode?q=&near=`: address search (signed in, 30/min). */
    geocode: (q: string, near?: LatLng | null, o?: CallOptions) =>
      http.get<{ items: GeoResult[] }>(`${P}/geocode`, { ...o, query: { q, near: pointParam(near) } }),
    /** `GET /v1/places/reverse?lat=&lng=`: the address at a point, or null. */
    reverse: (point: LatLng, o?: CallOptions) =>
      http.get<{ result: GeoResult | null }>(`${P}/reverse`, { ...o, query: { lat: fmt(point.lat), lng: fmt(point.lng) } }),

    admin: {
      /** `GET /v1/admin/places?status=&q=`: the pending queue lists oldest first. */
      list: (params: AdminPlaceListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminPlace>>(A, { ...o, query: { status: params.status, q: params.q || undefined, limit: params.limit, cursor: params.cursor } }),
      /** `GET /v1/admin/places/{id}`. */
      get: (id: string, o?: CallOptions) => http.get<AdminPlace>(`${A}/${seg(id)}`, o),
      /** `POST /v1/admin/places`: an approved listing with no owner. Audited. */
      create: (body: PlaceInput, o?: CallOptions) => http.post<AdminPlace>(A, body, o),
      /** `PATCH /v1/admin/places/{id}`. Audited. */
      update: (id: string, body: PlaceInput, o?: CallOptions) => http.patch<AdminPlace>(`${A}/${seg(id)}`, body, o),
      /** `POST /v1/admin/places/{id}/approve`. Audited; tells the owner. */
      approve: (id: string, o?: CallOptions) => http.post<AdminPlace>(`${A}/${seg(id)}/approve`, undefined, o),
      /** `POST /v1/admin/places/{id}/reject`: the owner sees `reason`. Audited. */
      reject: (id: string, reason: string, o?: CallOptions) => http.post<AdminPlace>(`${A}/${seg(id)}/reject`, { reason }, o),
      /** `POST /v1/admin/places/{id}/photos`: a `place_photo` key staff uploaded. Audited. */
      addPhoto: (id: string, key: string, o?: CallOptions) => http.post<AdminPlace>(`${A}/${seg(id)}/photos`, { key }, o),
      /** `DELETE /v1/admin/places/{id}/photos/{photoId}`. Audited. */
      removePhoto: (id: string, photoId: string, o?: CallOptions) => http.delete<void>(`${A}/${seg(id)}/photos/${seg(photoId)}`, o),
      /** `DELETE /v1/admin/places/{id}` (admin role). Audited. */
      remove: (id: string, o?: CallOptions) => http.delete<void>(`${A}/${seg(id)}`, o),
    },
  };
}

export type PlacesApi = ReturnType<typeof placesEndpoints>;
