-- docs: Notion-style pages and databases. A page is a tree of blocks (one row
-- per block, ordered by fractional index), nested under other pages. Every
-- page is an access resource ("docs.page") whose access parent is its parent
-- page, so sharing inherits down the tree; access_level() decides who can do
-- what (see the access feature). A database is a page with a property schema
-- whose rows are pages too (is_row), with saved views.
--
-- Edits are block operations applied server-side; docs_pages.version counts
-- them, and each block records the version that last touched it, so a client
-- that reconnects asks for "everything since version N" (blocks changed after
-- N plus tombstones of blocks deleted after N).

-- ---- pages -------------------------------------------------------------------

CREATE TABLE docs_pages (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    parent_id         uuid REFERENCES docs_pages (id) ON DELETE CASCADE, -- NULL: top level in the org
    kind              text NOT NULL DEFAULT 'page' CHECK (kind IN ('page', 'database')),
    is_row            boolean NOT NULL DEFAULT false,  -- a database row (parent is the database)
    inline            boolean NOT NULL DEFAULT false,  -- a database shown as a block on its parent page
    title             text NOT NULL DEFAULT '',
    icon              text,                            -- one emoji
    cover             jsonb,                           -- {"file_id"} or {"color"}, plus {"position": 0–100}
    position          text COLLATE "C" NOT NULL,       -- fractional index among siblings (byte order)
    full_width        boolean NOT NULL DEFAULT false,
    locked            boolean NOT NULL DEFAULT false,  -- block edits refused until unlocked
    props             jsonb NOT NULL DEFAULT '{}',     -- row values keyed by property id
    schema            jsonb,                           -- databases: [{id, name, type, options?, …}]
    version           bigint NOT NULL DEFAULT 0,       -- block operations applied so far
    tomb_floor        bigint NOT NULL DEFAULT 0,       -- tombstones before this version were trimmed
    public_blocked_at timestamptz,                     -- moderation took the published copy down
    created_by        uuid REFERENCES users (id),
    updated_by        uuid REFERENCES users (id),
    deleted_at        timestamptz,                     -- in the trash (with its whole subtree)
    deleted_by        uuid REFERENCES users (id),
    trash_root_id     uuid,                            -- the page whose trashing took this one
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (parent_id IS NULL OR parent_id <> id),
    CHECK (NOT is_row OR parent_id IS NOT NULL)
);
CREATE INDEX docs_pages_children_idx ON docs_pages (parent_id, position, id) WHERE deleted_at IS NULL;
CREATE INDEX docs_pages_roots_idx ON docs_pages (org_id, position, id) WHERE parent_id IS NULL AND deleted_at IS NULL;
CREATE INDEX docs_pages_trash_idx ON docs_pages (org_id, deleted_at DESC, id) WHERE deleted_at IS NOT NULL;
CREATE INDEX docs_pages_trash_root_idx ON docs_pages (trash_root_id) WHERE trash_root_id IS NOT NULL;
CREATE INDEX docs_pages_title_search_idx ON docs_pages USING gin (to_tsvector('simple', title));
CREATE INDEX docs_pages_created_by_idx ON docs_pages (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX docs_pages_updated_by_idx ON docs_pages (updated_by) WHERE updated_by IS NOT NULL;
CREATE INDEX docs_pages_deleted_by_idx ON docs_pages (deleted_by) WHERE deleted_by IS NOT NULL;

-- ---- blocks ------------------------------------------------------------------

-- Block ids are minted by the client (UUIDs), so a client can refer to a block
-- it created before the server acknowledged it. props holds the type's fields;
-- rich text is an array of spans {text, bold?, italic?, underline?, strike?,
-- code?, link?, mention?}, sanitised on write. plain is the block's text for
-- search and agent tools.
CREATE TABLE docs_blocks (
    id         uuid PRIMARY KEY,
    page_id    uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    parent_id  uuid REFERENCES docs_blocks (id) ON DELETE CASCADE, -- nesting (toggles, list items)
    type       text NOT NULL CHECK (type IN (
                   'paragraph', 'heading_1', 'heading_2', 'heading_3', 'bulleted_list', 'numbered_list',
                   'to_do', 'toggle', 'quote', 'callout', 'code', 'divider', 'image', 'file', 'bookmark',
                   'page', 'table', 'database')),
    props      jsonb NOT NULL DEFAULT '{}',
    position   text COLLATE "C" NOT NULL,
    plain      text NOT NULL DEFAULT '',
    version    bigint NOT NULL,                      -- the page version that last changed it
    created_by uuid REFERENCES users (id),
    updated_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_blocks_page_idx ON docs_blocks (page_id, parent_id, position, id);
CREATE INDEX docs_blocks_page_version_idx ON docs_blocks (page_id, version);
CREATE INDEX docs_blocks_parent_idx ON docs_blocks (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX docs_blocks_search_idx ON docs_blocks USING gin (to_tsvector('simple', plain));
CREATE INDEX docs_blocks_created_by_idx ON docs_blocks (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX docs_blocks_updated_by_idx ON docs_blocks (updated_by) WHERE updated_by IS NOT NULL;

-- Deleted blocks, so "since version N" can report deletions. Trimmed after a
-- week; a client further behind reloads the whole page.
CREATE TABLE docs_block_tombstones (
    page_id    uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    block_id   uuid NOT NULL,
    version    bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (page_id, block_id)
);
CREATE INDEX docs_block_tombstones_version_idx ON docs_block_tombstones (page_id, version);
CREATE INDEX docs_block_tombstones_created_idx ON docs_block_tombstones (created_at);

-- Page links: page mentions and sub-page blocks, for backlinks.
CREATE TABLE docs_links (
    source_block_id uuid NOT NULL REFERENCES docs_blocks (id) ON DELETE CASCADE,
    source_page_id  uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    target_page_id  uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_block_id, target_page_id)
);
CREATE INDEX docs_links_target_idx ON docs_links (target_page_id, source_page_id);
CREATE INDEX docs_links_source_page_idx ON docs_links (source_page_id);

-- ---- files -------------------------------------------------------------------

-- Images, files and covers uploaded to a page (private storage purpose
-- docs_file). Reading one needs view on its page; a duplicated page gets its
-- own rows pointing at the same object.
CREATE TABLE docs_files (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    page_id      uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    key          text NOT NULL,
    name         text NOT NULL,
    content_type text NOT NULL,
    size         bigint NOT NULL,
    uploaded_by  uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_files_page_idx ON docs_files (page_id);
CREATE INDEX docs_files_key_idx ON docs_files (key);
CREATE INDEX docs_files_uploaded_by_idx ON docs_files (uploaded_by) WHERE uploaded_by IS NOT NULL;

-- ---- people's own lists --------------------------------------------------------

CREATE TABLE docs_favorites (
    user_id    uuid NOT NULL REFERENCES users (id),
    page_id    uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, page_id)
);
CREATE INDEX docs_favorites_page_idx ON docs_favorites (page_id);

CREATE TABLE docs_visits (
    user_id    uuid NOT NULL REFERENCES users (id),
    page_id    uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    visited_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, page_id)
);
CREATE INDEX docs_visits_recent_idx ON docs_visits (user_id, visited_at DESC, page_id);
CREATE INDEX docs_visits_page_idx ON docs_visits (page_id);

-- ---- databases ---------------------------------------------------------------

-- Saved views of a database: type plus config {filters, sorts, group_by,
-- columns, card_properties, date_property}, validated against the schema.
CREATE TABLE docs_views (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    name        text NOT NULL,
    type        text NOT NULL CHECK (type IN ('table', 'board', 'list', 'calendar')),
    position    text COLLATE "C" NOT NULL,
    config      jsonb NOT NULL DEFAULT '{}',
    created_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_views_database_idx ON docs_views (database_id, position, id);
CREATE INDEX docs_views_created_by_idx ON docs_views (created_by) WHERE created_by IS NOT NULL;

-- ---- comments ----------------------------------------------------------------

-- A thread is a root comment (thread_id NULL) on the page or on one block,
-- optionally quoting a text range of it; replies point at the root. block_id
-- has no foreign key: a deleted block's threads stay on the page with their
-- quote.
CREATE TABLE docs_comments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id      uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    thread_id    uuid REFERENCES docs_comments (id) ON DELETE CASCADE,
    block_id     uuid,
    quote        text,                                 -- the selected text when it was made
    anchor_start integer,                              -- offsets into the block's plain text
    anchor_end   integer,
    body         jsonb NOT NULL DEFAULT '[]',          -- rich text spans (mentions notify)
    plain        text NOT NULL DEFAULT '',
    author_id    uuid REFERENCES users (id),
    resolved_at  timestamptz,                          -- roots only
    resolved_by  uuid REFERENCES users (id),
    edited_at    timestamptz,
    deleted_at   timestamptz,                          -- body cleared, shape kept
    hidden_at    timestamptz,                          -- removed by moderation
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((anchor_start IS NULL) = (anchor_end IS NULL))
);
CREATE INDEX docs_comments_page_idx ON docs_comments (page_id, created_at, id) WHERE thread_id IS NULL;
CREATE INDEX docs_comments_thread_idx ON docs_comments (thread_id, created_at, id) WHERE thread_id IS NOT NULL;
CREATE INDEX docs_comments_author_idx ON docs_comments (author_id) WHERE author_id IS NOT NULL;
CREATE INDEX docs_comments_resolved_by_idx ON docs_comments (resolved_by) WHERE resolved_by IS NOT NULL;

-- ---- history -----------------------------------------------------------------

-- Periodic copies of a page (title, icon and every block) to view and restore.
CREATE TABLE docs_snapshots (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id    uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    version    bigint NOT NULL,
    title      text NOT NULL,
    icon       text,
    blocks     jsonb NOT NULL,                         -- [{id, parent_id, type, props, position}]
    editors    uuid[] NOT NULL DEFAULT '{}',           -- who changed it since the previous copy
    reason     text NOT NULL DEFAULT 'auto' CHECK (reason IN ('auto', 'before_restore')),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_snapshots_page_idx ON docs_snapshots (page_id, created_at DESC, id DESC);
CREATE INDEX docs_snapshots_created_idx ON docs_snapshots (created_at);

-- ---- presence ----------------------------------------------------------------

-- Who has a page open and which block they are typing in. Ephemeral: rows
-- older than a minute are ignored and swept, so the table is unlogged.
CREATE UNLOGGED TABLE docs_presence (
    page_id    uuid NOT NULL,
    session_id text NOT NULL,
    user_id    uuid NOT NULL,
    block_id   uuid,
    seen_at    timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (page_id, session_id)
);
CREATE INDEX docs_presence_user_idx ON docs_presence (user_id);
CREATE INDEX docs_presence_seen_idx ON docs_presence (seen_at);
