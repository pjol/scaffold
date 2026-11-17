-- docs 1.2.0: bookmark previews. The server fetches a link's title,
-- description, site name, favicon and preview image once (through safehttp,
-- capped) and keeps them here for a week; the two images are copied into
-- private storage (purpose docs_preview) so readers load them from the app,
-- never from the third-party host. A preview is public web metadata, not
-- anyone's content: it is shared by every bookmark of the same URL.

CREATE TABLE docs_link_previews (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    url_hash    text NOT NULL UNIQUE,              -- sha256 of the normalised URL
    url         text NOT NULL,
    title       text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    site_name   text NOT NULL DEFAULT '',
    icon_key    text,                              -- storage keys (purpose docs_preview)
    icon_type   text,
    image_key   text,
    image_type  text,
    status      text NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'failed')),
    fetched_at  timestamptz NOT NULL DEFAULT now(),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_link_previews_fetched_idx ON docs_link_previews (fetched_at);

-- Bookmarks by preview, so unused previews can be cleaned up.
CREATE INDEX docs_blocks_preview_idx ON docs_blocks ((props->>'preview_id')) WHERE type = 'bookmark' AND props ? 'preview_id';
