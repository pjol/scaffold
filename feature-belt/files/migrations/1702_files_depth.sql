-- Files 1.3: thumbnails and extracted text per version, a weighted search index, comments,
-- public link options, file requests, locks, folder colours, named and kept versions.

-- Versions: a server-made thumbnail, the background pass that makes it (and extracts text),
-- a name, and keep-forever (never pruned).
ALTER TABLE files_versions
    ADD COLUMN thumb_key    text,
    ADD COLUMN processed_at timestamptz,
    ADD COLUMN keep_forever boolean NOT NULL DEFAULT false,
    ADD COLUMN name         text NOT NULL DEFAULT '' CHECK (char_length(name) <= 100);
CREATE INDEX files_versions_pending_idx ON files_versions (created_at) WHERE processed_at IS NULL;

CREATE FUNCTION files_queue_thumb_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.thumb_key IS NOT NULL THEN
        INSERT INTO files_object_deletions (storage_key) VALUES (OLD.thumb_key) ON CONFLICT DO NOTHING;
    END IF;
    RETURN OLD;
END $$;
CREATE TRIGGER files_versions_thumb_deleted AFTER DELETE ON files_versions
    FOR EACH ROW EXECUTE FUNCTION files_queue_thumb_deletion();

-- Items: where content_text came from, a lock, a folder colour, and one search vector
-- (name weighted over text) replacing the text-only index.
ALTER TABLE files_items
    ADD COLUMN content_source text NOT NULL DEFAULT '' CHECK (content_source IN ('', 'text', 'extracted')),
    ADD COLUMN locked_by uuid REFERENCES users (id),
    ADD COLUMN locked_at timestamptz,
    ADD COLUMN lock_note text NOT NULL DEFAULT '' CHECK (char_length(lock_note) <= 200),
    ADD COLUMN color text NOT NULL DEFAULT '' CHECK (color IN ('', 'primary', 'accent', 'success', 'warning', 'danger', 'info')),
    ADD COLUMN search_vector tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', name), 'A') ||
        setweight(to_tsvector('simple', left(COALESCE(content_text, ''), 200000)), 'B')) STORED,
    ADD CONSTRAINT files_items_lock_check CHECK ((locked_by IS NULL) = (locked_at IS NULL));
DROP INDEX files_items_content_idx;
CREATE INDEX files_items_search_idx ON files_items USING gin (search_vector);
CREATE INDEX files_items_locked_by_idx ON files_items (locked_by) WHERE locked_by IS NOT NULL;
CREATE INDEX files_items_modified_idx ON files_items (org_id, updated_at DESC, id DESC) WHERE trashed_at IS NULL;

ALTER TABLE files_activity DROP CONSTRAINT files_activity_action_check;
ALTER TABLE files_activity ADD CONSTRAINT files_activity_action_check CHECK (action IN (
    'created', 'uploaded', 'version', 'version_restored', 'edited', 'renamed', 'moved', 'copied', 'trashed', 'restored',
    'commented', 'locked', 'unlocked', 'owner_changed', 'requested_upload', 'version_named'));

-- Comments: threads on a file or folder; replies point at their thread. Anchors pin a thread
-- to a PDF page or a moment in a video or recording.
CREATE TABLE files_comments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    item_id     uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    thread_id   uuid REFERENCES files_comments (id) ON DELETE CASCADE, -- NULL: starts a thread
    author_id   uuid REFERENCES users (id),                            -- NULL: a deleted account
    body        text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 10000),
    mentions    uuid[] NOT NULL DEFAULT '{}',
    anchor      jsonb,                                                 -- {"page": 3} | {"seconds": 42.5}
    resolved_at timestamptz,
    resolved_by uuid REFERENCES users (id),
    edited_at   timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_comments_threads_idx ON files_comments (item_id, created_at, id) WHERE thread_id IS NULL;
CREATE INDEX files_comments_replies_idx ON files_comments (thread_id, created_at, id) WHERE thread_id IS NOT NULL;
CREATE INDEX files_comments_author_idx ON files_comments (author_id) WHERE author_id IS NOT NULL;
CREATE INDEX files_comments_resolved_by_idx ON files_comments (resolved_by) WHERE resolved_by IS NOT NULL;

-- Public link options on access's links to files: a password, downloads off, and how often
-- the link was opened.
CREATE TABLE files_link_settings (
    link_id        uuid PRIMARY KEY REFERENCES access_links (id) ON DELETE CASCADE,
    item_id        uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    password_hash  text,                                 -- bcrypt; NULL: no password
    allow_download boolean NOT NULL DEFAULT true,
    opens          bigint NOT NULL DEFAULT 0,
    last_opened_at timestamptz,
    updated_by     uuid REFERENCES users (id),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_link_settings_item_idx ON files_link_settings (item_id);
CREATE INDEX files_link_settings_updated_by_idx ON files_link_settings (updated_by) WHERE updated_by IS NOT NULL;

-- Proof a visitor typed a link's password (12 hours; sha256 of the token).
CREATE TABLE files_link_unlocks (
    token_hash bytea PRIMARY KEY,
    link_id    uuid NOT NULL REFERENCES access_links (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL
);
CREATE INDEX files_link_unlocks_link_idx ON files_link_unlocks (link_id);
CREATE INDEX files_link_unlocks_expiry_idx ON files_link_unlocks (expires_at);

-- File requests: an upload-only link into a folder for people without an account.
CREATE TABLE files_requests (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    folder_id      uuid NOT NULL REFERENCES files_items (id) ON DELETE CASCADE,
    created_by     uuid REFERENCES users (id),          -- NULL after account deletion: the link stops working
    title          text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    instructions   text NOT NULL DEFAULT '' CHECK (char_length(instructions) <= 2000),
    token_hash     bytea NOT NULL UNIQUE,
    expires_at     timestamptz,
    max_files      int CHECK (max_files IS NULL OR max_files BETWEEN 1 AND 10000),
    max_file_bytes bigint CHECK (max_file_bytes IS NULL OR max_file_bytes > 0),
    require_email  boolean NOT NULL DEFAULT true,
    files_received int NOT NULL DEFAULT 0,
    closed_at      timestamptz,
    closed_by      uuid REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_requests_folder_idx ON files_requests (folder_id, created_at DESC);
CREATE INDEX files_requests_org_idx ON files_requests (org_id, created_at DESC, id DESC);
CREATE INDEX files_requests_created_by_idx ON files_requests (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX files_requests_closed_by_idx ON files_requests (closed_by) WHERE closed_by IS NOT NULL;

CREATE TABLE files_request_uploads (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id     uuid NOT NULL REFERENCES files_requests (id) ON DELETE CASCADE,
    storage_key    text NOT NULL UNIQUE,
    name           text NOT NULL,
    content_type   text NOT NULL,
    size           bigint NOT NULL,
    uploader_name  text NOT NULL CHECK (char_length(uploader_name) BETWEEN 1 AND 100),
    uploader_email text NOT NULL DEFAULT '' CHECK (char_length(uploader_email) <= 254),
    item_id        uuid REFERENCES files_items (id) ON DELETE SET NULL,
    completed_at   timestamptz,
    expires_at     timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_request_uploads_request_idx ON files_request_uploads (request_id, created_at DESC);
CREATE INDEX files_request_uploads_item_idx ON files_request_uploads (item_id) WHERE item_id IS NOT NULL;
CREATE INDEX files_request_uploads_expiry_idx ON files_request_uploads (expires_at) WHERE completed_at IS NULL;
CREATE TRIGGER files_request_uploads_deleted AFTER DELETE ON files_request_uploads
    FOR EACH ROW WHEN (OLD.completed_at IS NULL) EXECUTE FUNCTION files_queue_object_deletion();

-- Last: an UPDATE queues files_items' deferred foreign-key checks, and no index can be built
-- on the table in the same transaction after that.
UPDATE files_items SET content_source = 'text' WHERE content_text IS NOT NULL;
