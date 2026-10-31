"use client";

import { keepPreviousData, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { hasErrorCode, type ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerRealtimeHandler } from "../hooks/realtime-handlers";
import { placesEndpoints } from "./api";
import type {
  AdminPlace,
  AdminPlaceListParams,
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

/** Query keys (`placesKeys.all()` invalidates everything). */
export const placesKeys = {
  all: () => ["places"] as const,
  config: () => ["places", "config"] as const,
  search: (params: Record<string, unknown>) => ["places", "search", params] as const,
  clusters: (params: Record<string, unknown>) => ["places", "clusters", params] as const,
  detail: (id: string) => ["places", "detail", id] as const,
  view: (id: string) => ["places", "view", id] as const,
  mine: () => ["places", "mine"] as const,
  favorites: () => ["places", "favorites"] as const,
  favoriteIds: () => ["places", "favorites", "ids"] as const,
  geocode: (q: string, near: string) => ["places", "geocode", q, near] as const,
  admin: {
    all: () => ["places", "admin"] as const,
    list: (params: Record<string, unknown>) => ["places", "admin", "list", params] as const,
    detail: (id: string) => ["places", "admin", "detail", id] as const,
  },
};

// A place entered the review queue: refresh the staff lists.
registerRealtimeHandler("places.pending", (qc) => {
  void qc.invalidateQueries({ queryKey: placesKeys.admin.all() });
});

const searchKey = (p: PlaceSearchParams) =>
  listKeyParams({ ...p, bbox: p.bbox?.map((n) => Math.round(n * 1e5) / 1e5).join(","), category: p.category?.join(","), near: p.near ? `${p.near.lat},${p.near.lng}` : undefined });

/** `GET /v1/places/config` (cached 10 min; works signed out). */
export function usePlacesConfig(options?: QueryOverrides<PlacesConfig>): UseQueryResult<PlacesConfig, ApiError> {
  const api = useEndpoints(placesEndpoints);
  return useApiQuery(placesKeys.config(), ({ signal }) => api.config({ signal }), { staleTime: 10 * 60_000, ...options });
}

/** Approved places (bbox, category, text; nearest first with `near`) as an infinite list. */
export function usePlaceSearch(params: PlaceSearchParams, options?: InfiniteOverrides<PlaceSummary>): PagedQueryResult<PlaceSummary> {
  const api = useEndpoints(placesEndpoints);
  return usePagedQuery<PlaceSummary>(placesKeys.search(searchKey(params)), (cursor, signal) => api.search({ ...params, cursor }, { signal }), {
    placeholderData: keepPreviousData,
    ...options,
  });
}

/** Map pins for a bbox and zoom: clusters for dense cells, single places otherwise. Keeps the last result while moving. */
export function usePlaceClusters(params: PlaceClusterParams | null, options?: QueryOverrides<PlaceClusterResult>): UseQueryResult<PlaceClusterResult, ApiError> {
  const api = useEndpoints(placesEndpoints);
  const key = params ? searchKey({ bbox: params.bbox, category: params.category, q: params.q }) : {};
  return useApiQuery(
    placesKeys.clusters({ ...key, zoom: params ? Math.round(params.zoom) : null }),
    ({ signal }) => api.clusters(params as PlaceClusterParams, { signal }),
    { placeholderData: keepPreviousData, staleTime: 30_000, ...options },
    !!params,
  );
}

/** An approved place (public route, works signed out). Pass null to disable. */
export function usePlace(id: string | null | undefined, options?: QueryOverrides<Place>): UseQueryResult<Place, ApiError> {
  const api = useEndpoints(placesEndpoints);
  return useApiQuery(placesKeys.detail(id ?? ""), ({ signal }) => api.get(id ?? "", { signal }), { retry: (n, e) => !hasErrorCode(e, "not_found") && n < 2, ...options }, !!id);
}

/** A place as the signed-in caller sees it: approved or their own in any status, with `owned` and `favorite`. */
export function usePlaceView(id: string | null | undefined, options?: QueryOverrides<PlaceView>): UseQueryResult<PlaceView, ApiError> {
  const api = useEndpoints(placesEndpoints);
  return useApiQuery(placesKeys.view(id ?? ""), ({ signal }) => api.view(id ?? "", { signal }), { retry: (n, e) => !hasErrorCode(e, "not_found") && n < 2, ...options }, !!id);
}

/** The caller's submissions in every status (with `review_note`). */
export function useMyPlaces(options?: InfiniteOverrides<Place>): PagedQueryResult<Place> {
  const api = useEndpoints(placesEndpoints);
  return usePagedQuery<Place>(placesKeys.mine(), (cursor, signal) => api.mine({ cursor }, { signal }), options);
}

function invalidateLists(qc: QueryClient) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: ["places", "search"] }),
    qc.invalidateQueries({ queryKey: ["places", "clusters"] }),
    qc.invalidateQueries({ queryKey: placesKeys.mine() }),
    qc.invalidateQueries({ queryKey: placesKeys.favorites() }),
    qc.invalidateQueries({ queryKey: ["places", "view"] }),
  ]);
}

/** `POST /v1/places`. */
export function useCreatePlace(): UseMutationResult<Place, ApiError, PlaceInput> {
  const api = useEndpoints(placesEndpoints);
  const qc = useQueryClient();
  return useMutation<Place, ApiError, PlaceInput>({
    mutationFn: (body) => api.create(body),
    onSuccess: (place) => {
      if (place.status === "approved") qc.setQueryData(placesKeys.detail(place.id), place);
      else qc.removeQueries({ queryKey: placesKeys.detail(place.id) });
    },
    onSettled: () => invalidateLists(qc),
  });
}

/** `PATCH /v1/places/{id}` (owner). */
export function useUpdatePlace(): UseMutationResult<Place, ApiError, { id: string } & PlaceInput> {
  const api = useEndpoints(placesEndpoints);
  const qc = useQueryClient();
  return useMutation<Place, ApiError, { id: string } & PlaceInput>({
    mutationFn: ({ id, ...body }) => api.update(id, body),
    onSuccess: (place) => {
      if (place.status === "approved") qc.setQueryData(placesKeys.detail(place.id), place);
      else qc.removeQueries({ queryKey: placesKeys.detail(place.id) });
    },
    onSettled: () => invalidateLists(qc),
  });
}

/** `DELETE /v1/places/{id}` (owner). */
export function useDeletePlace(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(placesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.remove(id),
    onSuccess: (_d, id) => qc.removeQueries({ queryKey: placesKeys.detail(id) }),
    onSettled: () => invalidateLists(qc),
  });
}

/** `POST /v1/places/{id}/photos` (owner). */
export function useAddPlacePhoto(): UseMutationResult<Place, ApiError, { id: string; key: string }> {
  const api = useEndpoints(placesEndpoints);
  const qc = useQueryClient();
  return useMutation<Place, ApiError, { id: string; key: string }>({
    mutationFn: ({ id, key }) => api.addPhoto(id, key),
    onSettled: (_d, _e, { id }) => Promise.all([invalidateLists(qc), qc.invalidateQueries({ queryKey: placesKeys.detail(id) })]),
  });
}

/** `DELETE /v1/places/{id}/photos/{photoId}` (owner). */
export function useRemovePlacePhoto(): UseMutationResult<void, ApiError, { id: string; photoId: string }> {
  const api = useEndpoints(placesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; photoId: string }>({
    mutationFn: ({ id, photoId }) => api.removePhoto(id, photoId),
    onSettled: (_d, _e, { id }) => Promise.all([invalidateLists(qc), qc.invalidateQueries({ queryKey: placesKeys.detail(id) })]),
  });
}

/** The caller's favourites, newest first. */
export function useFavoritePlaces(options?: InfiniteOverrides<FavoritePlace>): PagedQueryResult<FavoritePlace> {
  const api = useEndpoints(placesEndpoints);
  return usePagedQuery<FavoritePlace>(placesKeys.favorites(), (cursor, signal) => api.favorites({ cursor }, { signal }), options);
}

/** Ids of the caller's favourites (pin and button state). Disable while signed out. */
export function useFavoriteIds(options?: QueryOverrides<string[]>): UseQueryResult<string[], ApiError> {
  const api = useEndpoints(placesEndpoints);
  return useApiQuery(placesKeys.favoriteIds(), async ({ signal }) => (await api.favoriteIds({ signal })).ids, { staleTime: 60_000, ...options });
}

/** Add or remove a favourite, optimistic on the ids cache. */
export function useToggleFavorite(): UseMutationResult<void, ApiError, { id: string; favorite: boolean }, { previous: string[] | undefined }> {
  const api = useEndpoints(placesEndpoints);
  const qc = useQueryClient();
  const key = placesKeys.favoriteIds();
  return useMutation<void, ApiError, { id: string; favorite: boolean }, { previous: string[] | undefined }>({
    mutationFn: ({ id, favorite }) => (favorite ? api.addFavorite(id) : api.removeFavorite(id)),
    onMutate: async ({ id, favorite }) => {
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<string[]>(key);
      const rest = (previous ?? []).filter((x) => x !== id);
      qc.setQueryData<string[]>(key, favorite ? [id, ...rest] : rest);
      return { previous };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.previous) qc.setQueryData(key, ctx.previous);
    },
    onSettled: (_d, _e, { id }) =>
      Promise.all([qc.invalidateQueries({ queryKey: key }), qc.invalidateQueries({ queryKey: placesKeys.favorites(), exact: true }), qc.invalidateQueries({ queryKey: placesKeys.view(id) })]),
  });
}

/** Address search; enabled from 2 characters (debounce the text first). */
export function useGeocode(q: string, near?: LatLng | null, options?: QueryOverrides<GeoResult[]>): UseQueryResult<GeoResult[], ApiError> {
  const api = useEndpoints(placesEndpoints);
  const text = q.trim();
  const bias = near ? `${near.lat.toFixed(2)},${near.lng.toFixed(2)}` : "";
  return useApiQuery(placesKeys.geocode(text.toLowerCase(), bias), async ({ signal }) => (await api.geocode(text, near, { signal })).items, {
    staleTime: 30 * 60_000,
    placeholderData: keepPreviousData,
    retry: false,
    ...options,
  }, text.length >= 2);
}

/** The address at a point (dropping a pin). */
export function useReverseGeocode(): UseMutationResult<GeoResult | null, ApiError, LatLng> {
  const api = useEndpoints(placesEndpoints);
  return useMutation<GeoResult | null, ApiError, LatLng>({ mutationFn: async (p) => (await api.reverse(p)).result });
}

// ---- staff ------------------------------------------------------------------------

/** `GET /v1/admin/places?status=&q=` (pending oldest first). */
export function useAdminPlaces(params: AdminPlaceListParams = {}, options?: InfiniteOverrides<AdminPlace>): PagedQueryResult<AdminPlace> {
  const api = useEndpoints(placesEndpoints);
  return usePagedQuery<AdminPlace>(placesKeys.admin.list(listKeyParams(params)), (cursor, signal) => api.admin.list({ ...params, cursor }, { signal }), {
    placeholderData: keepPreviousData,
    ...options,
  });
}

/** `GET /v1/admin/places/{id}`. */
export function useAdminPlace(id: string | null | undefined, options?: QueryOverrides<AdminPlace>): UseQueryResult<AdminPlace, ApiError> {
  const api = useEndpoints(placesEndpoints);
  return useApiQuery(placesKeys.admin.detail(id ?? ""), ({ signal }) => api.admin.get(id ?? "", { signal }), options, !!id);
}

function adminMutation<V>(fn: (api: ReturnType<typeof placesEndpoints>, v: V) => Promise<AdminPlace | void>) {
  return function useAdminPlaceMutation(): UseMutationResult<AdminPlace | void, ApiError, V> {
    const api = useEndpoints(placesEndpoints);
    const qc = useQueryClient();
    return useMutation<AdminPlace | void, ApiError, V>({
      mutationFn: (v) => fn(api, v),
      onSuccess: (place) => {
        if (place) qc.setQueryData(placesKeys.admin.detail(place.id), place);
      },
      onSettled: () =>
        Promise.all([
          qc.invalidateQueries({ queryKey: placesKeys.admin.all() }),
          invalidateLists(qc),
          qc.invalidateQueries({ queryKey: ["places", "detail"] }),
          qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
        ]),
    });
  };
}

/** `POST /v1/admin/places` (approved, no owner). */
export const useAdminCreatePlace = adminMutation<PlaceInput>((api, body) => api.admin.create(body));
/** `PATCH /v1/admin/places/{id}`. */
export const useAdminUpdatePlace = adminMutation<{ id: string } & PlaceInput>((api, { id, ...body }) => api.admin.update(id, body));
/** `POST /v1/admin/places/{id}/approve`. */
export const useApprovePlace = adminMutation<string>((api, id) => api.admin.approve(id));
/** `POST /v1/admin/places/{id}/reject`. */
export const useRejectPlace = adminMutation<{ id: string; reason: string }>((api, { id, reason }) => api.admin.reject(id, reason));
/** `POST /v1/admin/places/{id}/photos`. */
export const useAdminAddPlacePhoto = adminMutation<{ id: string; key: string }>((api, { id, key }) => api.admin.addPhoto(id, key));
/** `DELETE /v1/admin/places/{id}/photos/{photoId}`. */
export const useAdminRemovePlacePhoto = adminMutation<{ id: string; photoId: string }>((api, { id, photoId }) => api.admin.removePhoto(id, photoId));
/** `DELETE /v1/admin/places/{id}` (admin role). */
export const useAdminDeletePlace = adminMutation<string>((api, id) => api.admin.remove(id));
