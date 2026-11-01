-- files: an org-scoped drive. Folders, files with versions, and item types
-- other features register (a repository). Every item is an access resource
-- (kind files.item) whose parent is its folder, so sharing and AI agent
-- scopes inherit down the tree; every list filters with access_level /
-- access_visible.
--
-- Trees: an item lives in one person's My drive (drive = 'my', owner_id =
-- that person) or in an org-owned shared folder (drive = 'shared', owner_id
-- NULL). Objects are stored under the org (files/<org id>/…), never under the
-- uploader, so org content survives the uploader's account deletion.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE files_items (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    parent_id        uuid REFERENCES files_items (id) ON DELETE CASCADE, -- NULL: top level of a My drive or a shared folder
    type             text NOT NULL CHECK (type ~ '^[a-z][a-z0-9_]{0,31}$'), -- folder | file | a registered type (repo)
    name             text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
    drive            text NOT NULL CHECK (drive IN ('my', 'shared')),
    owner_id         uuid REFERENCES users (id),          -- the My drive's person; NULL for org-owned items
    created_by       uuid REFERENCES users (id),
    updated_by       uuid REFERENCES users (id),
    -- files: the current version, copied here for listing and search
    version_id       uuid,
    version_count    int NOT NULL DEFAULT 0,
    mime_type        text NOT NULL DEFAULT '',
    size             bigint NOT NULL DEFAULT 0,
    text_like        boolean NOT NULL DEFAULT false,      -- valid UTF-8 without NUL bytes
    content_text     text,                                -- first 100 KB of small text files, for search
    -- trash: trashing marks the item and its subtree with the item as trash_root
    trashed_at       timestamptz,
    trashed_by       uuid REFERENCES users (id),
    trash_root       uuid,
    purge_after      timestamptz,
    -- staff: public links stop working for this item and everything under it
    public_disabled_at     timestamptz,
    public_disabled_by     uuid REFERENCES users (id),
    public_disabled_reason text NOT NULL DEFAULT '',
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK ((drive = 'my') = (owner_id IS NOT NULL)),
    CHECK ((trashed_at IS NULL) = (trash_root IS NULL))
);
-- Names are unique (ignoring case) among an item's live siblings, so paths
-- work for downloads and git; top-level items are siblings per My drive, or
-- among an org's shared folders.
CREATE UNIQUE INDEX files_items_name_key ON files_items
    (org_id, drive, COALESCE(parent_id, owner_id, org_id), lower(name)) WHERE trashed_at IS NULL;
CREATE INDEX files_items_parent_idx ON files_items (parent_id, (type <> 'folder'), lower(name), id) WHERE trashed_at IS NULL;
CREATE INDEX files_items_roots_idx ON files_items (org_id, drive, owner_id) WHERE parent_id IS NULL AND trashed_at IS NULL;
CREATE INDEX files_items_owner_idx ON files_items (owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX files_items_created_by_idx ON files_items (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX files_items_updated_by_idx ON files_items (updated_by) WHERE updated_by IS NOT NULL;
CREATE INDEX files_items_trashed_by_idx ON files_items (trashed_by) WHERE trashed_by IS NOT NULL;
CREATE INDEX files_items_trash_idx ON files_items (org_id, trashed_at DESC, id DESC) WHERE trashed_at IS NOT NULL AND trash_root = id;
CREATE INDEX files_items_trash_root_idx ON files_items (trash_root) WHERE trash_root IS NOT NULL;
CREATE INDEX files_items_purge_idx ON files_items (purge_after) WHERE trash_root = id;
CREATE INDEX files_items_disabled_idx ON files_items (public_disabled_at DESC, id DESC) WHERE public_disabled_at IS NOT NULL;
CREATE INDEX files_items_disabled_by_idx ON files_items (public_disabled_by) WHERE public_disabled_by IS NOT NULL;
CREATE INDEX files_items_name_trgm_idx ON files_items USING gin (lower(name) gin_trgm_ops);
CREATE INDEX files_items_content_idx ON files_items USING gin (to_tsvector('simple', COALESCE(content_text, '')));

-- Every version of a file. Objects are immutable: a new version, a restore
-- or a copy writes a new object.
CREATE TABLE files_versions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id     uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    org_id      uuid NOT NULL,                        -- quota sums by org
    number      int NOT NULL,
    storage_key text NOT NULL UNIQUE,                 -- files/<org id>/<uuid>.<ext>
    size        bigint NOT NULL,
    mime_type   text NOT NULL,
    text_like   boolean NOT NULL DEFAULT false,
    uploaded_by uuid REFERENCES users (id),
    note        text NOT NULL DEFAULT '',             -- "Restored from version 2", "Edited by Claude"
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT files_versions_number_key UNIQUE (item_id, number)
);
CREATE INDEX files_versions_org_idx ON files_versions (org_id) INCLUDE (size);
CREATE INDEX files_versions_uploaded_by_idx ON files_versions (uploaded_by) WHERE uploaded_by IS NOT NULL;
ALTER TABLE files_items ADD CONSTRAINT files_items_version_fkey
    FOREIGN KEY (version_id) REFERENCES files_versions (id) DEFERRABLE INITIALLY DEFERRED;

-- Presigned uploads in flight: what the key was issued for, so completion
-- trusts nothing the client sends back but the upload id.
CREATE TABLE files_uploads (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id),
    parent_id    uuid REFERENCES files_items (id) ON DELETE CASCADE, -- new file: its folder (NULL: My drive)
    item_id      uuid REFERENCES files_items (id) ON DELETE CASCADE, -- new version of this file
    name         text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    storage_key  text NOT NULL UNIQUE,
    completed_at timestamptz,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_uploads_user_idx ON files_uploads (user_id, created_at DESC);
CREATE INDEX files_uploads_expiry_idx ON files_uploads (expires_at);

-- Per-person markers.
CREATE TABLE files_stars (
    user_id    uuid NOT NULL REFERENCES users (id),
    item_id    uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX files_stars_list_idx ON files_stars (user_id, created_at DESC, item_id DESC);
CREATE INDEX files_stars_item_idx ON files_stars (item_id);

CREATE TABLE files_recents (
    user_id   uuid NOT NULL REFERENCES users (id),
    item_id   uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    org_id    uuid NOT NULL,
    action    text NOT NULL DEFAULT 'viewed' CHECK (action IN ('viewed', 'edited')),
    at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX files_recents_list_idx ON files_recents (user_id, org_id, at DESC, item_id DESC);
CREATE INDEX files_recents_item_idx ON files_recents (item_id);

-- What happened to an item. folder_id is the parent at the time, so a
-- folder's activity includes its direct children's.
CREATE TABLE files_activity (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    item_id    uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    folder_id  uuid,
    actor_id   uuid REFERENCES users (id),
    action     text NOT NULL CHECK (action IN ('created', 'uploaded', 'version', 'version_restored', 'edited',
                                               'renamed', 'moved', 'copied', 'trashed', 'restored')),
    data       jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_activity_item_idx ON files_activity (item_id, created_at DESC, id DESC);
CREATE INDEX files_activity_folder_idx ON files_activity (folder_id, created_at DESC, id DESC) WHERE folder_id IS NOT NULL;
CREATE INDEX files_activity_actor_idx ON files_activity (actor_id) WHERE actor_id IS NOT NULL;

-- Public link traffic per item and day (views = inline previews).
CREATE TABLE files_public_stats (
    item_id   uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    day       date NOT NULL,
    views     int NOT NULL DEFAULT 0,
    downloads int NOT NULL DEFAULT 0,
    PRIMARY KEY (item_id, day)
);

-- Short-lived tokens for zip downloads (a browser download can't carry the
-- session header). Only the sha256 is stored.
CREATE TABLE files_archives (
    token_hash bytea PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    item_ids   uuid[] NOT NULL,
    name       text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_archives_expiry_idx ON files_archives (expires_at);

-- Objects to delete from storage, filled by triggers whenever a version or
-- an unfinished upload row goes (purge, account deletion, org deletion,
-- pruning), drained by the files.delete_objects job.
CREATE TABLE files_object_deletions (
    storage_key text PRIMARY KEY,
    attempts    int NOT NULL DEFAULT 0,
    last_error  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION files_queue_object_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO files_object_deletions (storage_key) VALUES (OLD.storage_key) ON CONFLICT DO NOTHING;
    RETURN OLD;
END $$;

CREATE TRIGGER files_versions_deleted AFTER DELETE ON files_versions
    FOR EACH ROW EXECUTE FUNCTION files_queue_object_deletion();
CREATE TRIGGER files_uploads_deleted AFTER DELETE ON files_uploads
    FOR EACH ROW WHEN (OLD.completed_at IS NULL) EXECUTE FUNCTION files_queue_object_deletion();
