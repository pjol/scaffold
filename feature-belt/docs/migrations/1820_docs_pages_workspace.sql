-- docs 1.2.0 (pages and the workspace around them): page chrome (custom
-- icons, fonts, small text, verification), templates, import runs,
-- following, page views, comment reactions, publish settings and teamspace
-- sidebar choices.

-- ---- page chrome and templates ---------------------------------------------------

ALTER TABLE docs_pages
    ADD COLUMN custom_icon    jsonb,             -- {"type":"lucide","name","color"} or {"type":"image","file_id"}; icon (emoji) is NULL then
    ADD COLUMN font           text NOT NULL DEFAULT 'default' CHECK (font IN ('default', 'serif', 'mono')),
    ADD COLUMN small_text     boolean NOT NULL DEFAULT false,
    ADD COLUMN is_template    boolean NOT NULL DEFAULT false, -- a workspace template (the whole tree), hidden from the tree and search
    ADD COLUMN verified_by    uuid REFERENCES users (id),
    ADD COLUMN verified_at    timestamptz,
    ADD COLUMN verified_until timestamptz;       -- NULL while verified: until someone changes it
CREATE INDEX docs_pages_templates_idx ON docs_pages (org_id, updated_at DESC) WHERE is_template AND parent_id IS NULL AND deleted_at IS NULL;
CREATE INDEX docs_pages_verified_by_idx ON docs_pages (verified_by) WHERE verified_by IS NOT NULL;

-- ---- import runs -------------------------------------------------------------------

-- One import of an uploaded file (Markdown, text, HTML, Word, CSV or a zip of
-- them), run in the background. key is the private upload (purpose
-- docs_import), deleted when the run ends. errors is the report shown to the
-- person: [{path, message}], at most 200.
CREATE TABLE docs_imports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id),
    parent_id    uuid REFERENCES docs_pages (id) ON DELETE SET NULL,
    shared       boolean NOT NULL DEFAULT false,
    key          text,
    name         text NOT NULL,
    format       text NOT NULL CHECK (format IN ('markdown', 'text', 'html', 'docx', 'csv', 'zip')),
    status       text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'failed')),
    pages_total  integer NOT NULL DEFAULT 0,
    pages_done   integer NOT NULL DEFAULT 0,
    files_done   integer NOT NULL DEFAULT 0,
    errors       jsonb NOT NULL DEFAULT '[]',
    message      text,                           -- why a failed run stopped
    root_page_id uuid REFERENCES docs_pages (id) ON DELETE SET NULL,
    heartbeat_at timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_imports_user_idx ON docs_imports (user_id, created_at DESC, id);
CREATE INDEX docs_imports_running_idx ON docs_imports (heartbeat_at) WHERE status = 'running';
CREATE INDEX docs_imports_org_idx ON docs_imports (org_id);

-- Finished exports waiting to be downloaded (private purpose docs_export,
-- written by the server); objects and rows go after an hour.
CREATE TABLE docs_exports (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    key        text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_exports_created_idx ON docs_exports (created_at);
CREATE INDEX docs_exports_user_idx ON docs_exports (user_id);

-- ---- following -------------------------------------------------------------------

-- Who follows a page (edits, batched, and comments notify them). Creators and
-- editors follow automatically unless they unfollowed (following = false
-- keeps that choice). notified_at batches edit notifications.
CREATE TABLE docs_follows (
    user_id     uuid NOT NULL REFERENCES users (id),
    page_id     uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    following   boolean NOT NULL DEFAULT true,
    notified_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, page_id)
);
CREATE INDEX docs_follows_page_idx ON docs_follows (page_id) WHERE following;

-- ---- page views ------------------------------------------------------------------

-- Views per page, day and member, for page analytics (kept a year).
CREATE TABLE docs_page_views (
    page_id        uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    day            date NOT NULL,
    user_id        uuid NOT NULL REFERENCES users (id),
    views          integer NOT NULL DEFAULT 1,
    last_viewed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (page_id, day, user_id)
);
CREATE INDEX docs_page_views_user_idx ON docs_page_views (user_id);
CREATE INDEX docs_page_views_day_idx ON docs_page_views (day);

-- ---- comment reactions -------------------------------------------------------------

CREATE TABLE docs_comment_reactions (
    comment_id uuid NOT NULL REFERENCES docs_comments (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    emoji      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id, emoji)
);
CREATE INDEX docs_comment_reactions_user_idx ON docs_comment_reactions (user_id);

-- ---- publishing --------------------------------------------------------------------

-- Options of a published page (the link itself, its level and expiry live in
-- access_links).
CREATE TABLE docs_publish_settings (
    page_id         uuid PRIMARY KEY REFERENCES docs_pages (id) ON DELETE CASCADE,
    search_indexing boolean NOT NULL DEFAULT false,
    allow_duplicate boolean NOT NULL DEFAULT false,
    theme           text NOT NULL DEFAULT 'system' CHECK (theme IN ('system', 'light', 'dark')),
    show_outline    boolean NOT NULL DEFAULT true,
    updated_by      uuid REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_publish_settings_updated_by_idx ON docs_publish_settings (updated_by) WHERE updated_by IS NOT NULL;

-- ---- teamspaces ----------------------------------------------------------------------

-- Teamspaces are access teams with pages shared to them; each person may
-- hide one from their sidebar (and show it again).
CREATE TABLE docs_teamspace_prefs (
    user_id    uuid NOT NULL REFERENCES users (id),
    team_id    uuid NOT NULL REFERENCES access_teams (id) ON DELETE CASCADE,
    hidden     boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, team_id)
);
CREATE INDEX docs_teamspace_prefs_team_idx ON docs_teamspace_prefs (team_id);
