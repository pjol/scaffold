# Places

## What it does

- A map (MapLibre GL on web, Apple Maps / Google Maps through expo-maps on mobile) with the places in view listed beside it; dense areas come back from the server as clusters with counts.
- Search by name or address and filter by category; "Show my location" sorts the list nearest first (location asked at that tap, never at launch).
- Signed-in users add places with address search (Photon or Mapbox, through the API), hours and photos; staff approve or reject them when `PLACES_REQUIRE_APPROVAL=true`. Owners edit and delete their places; with approval on, an edit or a new photo sends the place back to review.
- Users save favourites; "Your places" lists saved places and their submissions with review status and the rejection reason.
- Staff work the review queue, edit any place, add listings of their own (no owner) and remove photos; admins delete.

## Data

| Table | Purpose |
|---|---|
| `places` | name, category, description, address, `lat`/`lng` (`double precision`), phone, website, `hours` (jsonb, validated), status `pending`/`approved`/`rejected`, review note, reviewer, owner (NULL for staff listings and deleted owners) |
| `place_photos` | storage keys (purpose `place_photo`, public-read), ordered, with uploader |
| `place_favorites` | (user, place) pairs |

Indexes: partial `(lat, lng)` on approved places for bbox/nearest prefilters, trigram GIN on `name || ' ' || address` for `?q=` (the migration creates `pg_trgm`, a trusted extension), keyset indexes for lists and the queue. No PostGIS.

Hours JSON: `{"timezone":"America/Los_Angeles","note":"","weekly":{"mon":[{"open":"09:00","close":"17:00"}],"tue":[],…}}`; an empty day is closed, a close before its open runs past midnight, up to 4 openings a day, `hours: null` = unknown.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/places/config` | public | categories, `map_style_url`, `default_center`, `default_zoom`, `require_approval`, `service_area`, `max_photos`, `geocoder` |
| GET | `/v1/places?bbox=minLng,minLat,maxLng,maxLat&category=a,b&q=&limit=&cursor=` | public | approved places, newest first (limit ≤ 500, keyset) |
| GET | `/v1/places?near=lat,lng&radius_m=` | public | nearest first with `distance_m`; bbox prefilter + haversine, keyset on (distance, id) |
| GET | `/v1/places?cluster=true&zoom=n&bbox=…` | public | `{clusters:[{id,lat,lng,count,bbox}], places, truncated}`, 64 px Web Mercator grid |
| GET | `/v1/places/{id}` | public | approved place with photos |
| GET | `/v1/places/{id}/viewer` | user | `{place, owned, favorite}`: approved or the caller's own in any status |
| GET | `/v1/places/geocode?q=&near=`, `/v1/places/reverse?lat=&lng=` | user, 30/min | address search; 30-minute cache, global upstream cap |
| GET | `/v1/places/mine`, `/v1/places/favorites`, `/v1/places/favorites/ids` | user | own submissions; favourites page; favourite ids |
| POST | `/v1/places` | active, 10/h | submit (`photo_keys` optional) |
| PATCH / DELETE | `/v1/places/{id}` | owner | edit (may re-queue) / delete with photos |
| POST / DELETE | `/v1/places/{id}/photos[/{photo_id}]` | owner | add `{key}` / remove |
| PUT / DELETE | `/v1/places/{id}/favorite` | active, 120/min | save / unsave (idempotent) |
| GET / POST | `/v1/admin/places?status=&q=` | support | queue (pending oldest first) / staff listing |
| GET / PATCH | `/v1/admin/places/{id}` | support | detail / edit (audited) |
| POST | `/v1/admin/places/{id}/approve`, `/reject {reason}` | support | decision (audited); notifies the owner (`places.review`) |
| POST / DELETE | `/v1/admin/places/{id}/photos[/{photo_id}]` | support | staff photos (audited) |
| DELETE | `/v1/admin/places/{id}` | admin | delete (audited) |

Realtime: `places.pending {id}` on `admin:live`. Agent tools: `places_search`, `places_get` (read-only; `places_get` targets the place, and both leave out places the caller's agent scope hides).

## Screens

| Surface | Route | Notes |
|---|---|---|
| Web | `/places` (nav + Home tile) | header · map · list on phones; 384 px list column beside the map ≥ 1024. Map pans refetch clusters (`usePlaceClusters`) and the list (`usePlaceSearch`, bbox) after 200 ms; pins are buttons; selecting one shows a preview card and highlights its row. Search, categories and the selected place live in the URL. |
| Web | `/places/[id]`, `/places/new`, `/places/[id]/edit`, `/places/saved?tab=saved\|mine` | detail (photos, contact, hours with today, small map, owner edit/delete); form (address search + drop a pin + reverse geocoding, hours editor, `PhotoUploader`); your places |
| Web admin | `/admin/places`, `/admin/places/new`, `/admin/places/[id]`, `/admin/places/[id]/edit` | queue with status tabs and search; review page (approve, reject with reason, photos, delete) |
| Web | `/maplibre-gl-worker.mjs` | route serving MapLibre's worker from node_modules (static at build) |
| Mobile | Places tab | search, category chips, Map/List switch; markers (`markers` prop), clusters zoom in, preview card, "Show my location" |
| Mobile | `/places/[id]`, `/places/new`, `/places/[id]/edit`, `/places/saved` (Home row "Saved places") | detail with directions (Apple Maps / `geo:`), form (address search, "Use my location", hours, photos), saved and added-by-you |

Every list and detail has skeleton, empty and error states. Map failures (no WebGL, style down) leave the list working on the area the map would show.

## Config

| Variable | Tier | Default | Notes |
|---|---|---|---|
| `PLACES_CATEGORIES` | backend | 9 general categories | `id:Label,…`; keep ids stable |
| `PLACES_REQUIRE_APPROVAL` | backend | `true` | staff review before a place is public |
| `PLACES_MAP_STYLE_URL` | backend | OpenFreeMap liberty | MapLibre style for the web map |
| `PLACES_DEFAULT_CENTER` / `PLACES_DEFAULT_ZOOM` | backend | San Francisco / 12 | |
| `PLACES_SERVICE_AREA` | backend | — | bbox; outside → 422 `outside_service_area` |
| `MAPBOX_TOKEN` | backend | — | set → Mapbox geocoding v6, else Photon |
| `PLACES_PHOTON_URL` | backend | `https://photon.komoot.io` | self-host for volume |
| `PLACES_GEOCODE_COUNTRIES` | backend | — | `us,ca` limits address search |
| `PLACES_GEOCODE_RATE_PER_MIN` | backend | `60` | upstream calls per minute, all users |
| `EXPO_PUBLIC_GOOGLE_MAPS_ANDROID_KEY` | mobile | — | Google Maps SDK key, injected by `plugins/withPlaces.js` (wired at the `scaffold:plugins` anchor) only when set |

npm: web `maplibre-gl ^6.16.0`; mobile `expo-maps ~57.0.4`, `expo-location ~57.0.22`, `expo-build-properties ~57.0.22`. No Go dependencies.

## Compliance

- Deletion: favourites deleted; every photo row the user uploaded deleted (core storage cleanup removes the objects under their prefix, so photos on approved places go too); pending and rejected submissions deleted; approved places stay as public listings with the owner cleared. Export: submitted places, favourites, uploaded photos.
- UGC: staff approval by default. With approval off, wire `places.SetContentFilter(moderation.Check)` and reports.
- Third parties: address searches go server-side to Photon (OpenStreetMap) or Mapbox with the app's user agent; the web map loads OpenFreeMap tiles; Android maps are Google's. Name them in the privacy policy. Websites are stored, never fetched.
- Location: when-in-use only, asked at the tap, with a purpose string; denial leaves the map and list working and offers Settings.

## Stitching

1. Set categories, default center/zoom and service area; decide approval mode and who may submit (default: any active signed-in user).
2. Mobile: `scaffold add` wires `"./plugins/withPlaces"` into `plugins` in `mobile/app.config.ts` (iOS 17.0 via expo-build-properties, the location purpose string, the Android key). If the app already uses `expo-build-properties`, move `ios.deploymentTarget: "17.0"` into that entry and drop it from the plugin. Rebuild the dev client.
3. Create the Android Maps key (package + SHA-1; add Play App Signing's SHA-1 after the first upload) and set it in each EAS environment.
4. Production geocoding: `MAPBOX_TOKEN` or a self-hosted Photon. Optionally a dark MapLibre style for dark mode (the basemap stays light by default).
5. Approval off → install moderation and call `places.SetContentFilter(moderation.Check)` from an app file.
6. With notifications installed, register kind `places.review` ("Place reviews").
7. Privacy policy: OpenStreetMap/Photon or Mapbox, OpenFreeMap, Google Maps (Android).

## Extending

- `Geocoder` (`Search`, `Reverse`) for another provider behind the cache; `SetContentFilter` for a pre-post check.
- `MapView` (web) and `PlacesMap` (mobile) take plain pins: reuse them for other map screens.
- App columns (price level, tags) go in an `app` migration plus app handlers; never edit `1200_places_init.sql`.

## Provenance

Distilled from SFLuv (merchant map, location schema, structured hours, service-area geofence), ASSEMBLY (expo-maps under the New Architecture after react-native-maps crashed on launch; `markers` not `annotations` on iOS; iOS 17; list beside the map; marker re-keying for MapKit's stuck selection) and DOGPATCH (`internal/geo` geocoding proxy with cache, Photon/Mapbox; MapLibre + OpenFreeMap).
Fixed on the way: SFLuv's `/locations` paged at 100 and clients never asked for more (now bbox + keyset + server clustering); no server-side spatial filtering (now indexed bbox and haversine); hard-coded city and US-only geocoding (now config); photos as BYTEA (now object storage); MapLibre's worker URL breaking under bundlers (served by a route).

Unverified: expo-maps `onMarkerClick` and `monogram` behaviour on device (exported for iOS and Android, not run on a simulator).

## Changes

- 1.0.3: agent scope in the tools: `places_get` has a `Target` (the place) and `places_search` drops places set to Off.
- 1.0.2: Global search: `app.SearchProvider` (kind `places.place`) over approved places by name or address, the rule of `GET /places?q=`; agent calls also need the place readable at the caller's agent scope.
- 1.0.0: first release.
- 1.0.1: the config plugin is wired at `mobile/app.config.ts` `scaffold:plugins` (no manual step).
