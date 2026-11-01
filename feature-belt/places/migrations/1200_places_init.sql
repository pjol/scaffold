-- places: listings with coordinates, photos, per-user favourites and staff
-- review. Geo is plain double precision lat/lng with a btree on (lat, lng)
-- for bounding-box and nearest queries; text search uses pg_trgm (a trusted
-- extension: the database owner can create it without superuser).

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE places (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id    uuid REFERENCES users (id),                 -- submitter; NULL for staff-created listings and after the owner deletes their account
    name        text NOT NULL,
    category    text NOT NULL,                              -- an id from PLACES_CATEGORIES
    description text NOT NULL DEFAULT '',
    address     text NOT NULL DEFAULT '',
    lat         double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng         double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
    phone       text NOT NULL DEFAULT '',
    website     text NOT NULL DEFAULT '',
    hours       jsonb,                                      -- {"timezone","note","weekly":{"mon":[{"open":"09:00","close":"17:00"}],…}}; NULL = unknown
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    review_note text NOT NULL DEFAULT '',                   -- rejection reason shown to the owner
    reviewed_by uuid REFERENCES users (id),
    reviewed_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- Bounding-box prefilter for the map, clustering and nearest-first lists.
CREATE INDEX places_approved_lat_lng_idx ON places (lat, lng) WHERE status = 'approved';
-- Default list order (keyset) of the public search.
CREATE INDEX places_approved_created_idx ON places (created_at DESC, id DESC) WHERE status = 'approved';
-- Staff queue by status, oldest first.
CREATE INDEX places_status_created_idx ON places (status, created_at, id);
CREATE INDEX places_owner_idx ON places (owner_id, created_at DESC, id DESC) WHERE owner_id IS NOT NULL;
-- ?q= text search over name and address (the query uses this exact expression).
CREATE INDEX places_search_trgm_idx ON places USING gin ((name || ' ' || address) gin_trgm_ops);

CREATE TABLE place_photos (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    place_id    uuid NOT NULL REFERENCES places (id) ON DELETE CASCADE,
    key         text NOT NULL UNIQUE,                       -- storage key, purpose place_photo (public-read)
    uploaded_by uuid REFERENCES users (id),
    position    int NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX place_photos_place_idx ON place_photos (place_id, position, created_at);
CREATE INDEX place_photos_uploader_idx ON place_photos (uploaded_by) WHERE uploaded_by IS NOT NULL;

CREATE TABLE place_favorites (
    user_id    uuid NOT NULL REFERENCES users (id),
    place_id   uuid NOT NULL REFERENCES places (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, place_id)
);
CREATE INDEX place_favorites_user_idx ON place_favorites (user_id, created_at DESC, place_id DESC);
CREATE INDEX place_favorites_place_idx ON place_favorites (place_id);
