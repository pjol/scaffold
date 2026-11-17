-- docs 1.2.0 (databases): view types, linked views, repeating row templates
-- (a row template is a row with docs_pages.is_template, added by 1820, under
-- its database; nothing here may use that column), unique ID counters, date reminders on rows,
-- automations with a run log, form submissions, and currencies in minor units.
--
-- Formulas, rollups and created/edited values are never stored: they are
-- evaluated for each viewer when read (a rollup counts only related rows the
-- viewer can open).

-- ---- views -----------------------------------------------------------------------

ALTER TABLE docs_views DROP CONSTRAINT IF EXISTS docs_views_type_check;
ALTER TABLE docs_views ADD CONSTRAINT docs_views_type_check CHECK (type IN (
    'table', 'board', 'list', 'calendar', 'gallery', 'timeline', 'chart', 'form', 'feed'));

-- A linked view belongs to a database block on another page (host): editors
-- of the host page change it; its rows are the source database's, checked
-- against the viewer live. block_id has no foreign key (blocks come and go
-- through operations); views whose block is gone are swept.
ALTER TABLE docs_views
    ADD COLUMN block_id     uuid,
    ADD COLUMN host_page_id uuid REFERENCES docs_pages (id) ON DELETE CASCADE;
CREATE INDEX docs_views_block_idx ON docs_views (block_id, position, id) WHERE block_id IS NOT NULL;
CREATE INDEX docs_views_host_idx ON docs_views (host_page_id) WHERE host_page_id IS NOT NULL;

-- ---- row templates -----------------------------------------------------------------

-- Repeating templates make a row on a schedule: {every: day|week|month,
-- interval, weekdays, time, tz}; repeat_next_at is the next run.
ALTER TABLE docs_pages
    ADD COLUMN repeat         jsonb,
    ADD COLUMN repeat_next_at timestamptz;
CREATE INDEX docs_pages_repeat_idx ON docs_pages (repeat_next_at) WHERE repeat_next_at IS NOT NULL;

-- ---- unique IDs ------------------------------------------------------------------

-- The last number handed out per database and unique ID property.
CREATE TABLE docs_counters (
    database_id uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    property_id text NOT NULL,
    last        bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (database_id, property_id)
);

-- ---- date reminders on rows --------------------------------------------------------

-- A date property value with a reminder notifies the person who set it at
-- fire_at, if they can still open the row. One per row and property.
CREATE TABLE docs_row_reminders (
    row_id      uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    property_id text NOT NULL,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    fire_at     timestamptz NOT NULL,
    label       text NOT NULL DEFAULT '',  -- the date as set, for the notification
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (row_id, property_id)
);
CREATE INDEX docs_row_reminders_due_idx ON docs_row_reminders (fire_at);
CREATE INDEX docs_row_reminders_user_idx ON docs_row_reminders (user_id);

-- ---- automations -----------------------------------------------------------------

-- trigger: {type: row_added|property_edited|schedule, property?, values?,
-- every?: day|week, weekdays?, time?, tz?}; conditions: filters a row must
-- match; actions: [{type: set_property|add_row|notify|webhook, …}]. They run
-- as created_by with that person's access at run time. secret signs webhooks.
CREATE TABLE docs_automations (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    database_id   uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    name          text NOT NULL,
    enabled       boolean NOT NULL DEFAULT true,
    trigger       jsonb NOT NULL,
    conditions    jsonb NOT NULL DEFAULT '[]',
    actions       jsonb NOT NULL DEFAULT '[]',
    secret        text NOT NULL,
    next_run_at   timestamptz,               -- schedules
    paused_reason text,                      -- why the server turned it off
    created_by    uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    last_run_at   timestamptz
);
CREATE INDEX docs_automations_db_idx ON docs_automations (database_id, created_at, id);
CREATE INDEX docs_automations_due_idx ON docs_automations (next_run_at) WHERE enabled AND next_run_at IS NOT NULL;
CREATE INDEX docs_automations_created_by_idx ON docs_automations (created_by) WHERE created_by IS NOT NULL;

-- Each run, for the run log (kept 30 days). row_id has no foreign key: the
-- log outlives the row.
CREATE TABLE docs_automation_runs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id uuid NOT NULL REFERENCES docs_automations (id) ON DELETE CASCADE,
    row_id        uuid,
    status        text NOT NULL CHECK (status IN ('ok', 'failed', 'skipped')),
    detail        text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_automation_runs_idx ON docs_automation_runs (automation_id, created_at DESC, id DESC);
CREATE INDEX docs_automation_runs_created_idx ON docs_automation_runs (created_at);

-- ---- forms -----------------------------------------------------------------------

-- A form view is also the access resource docs.form (parent: its database),
-- so it can be shared inside the workspace or by public link. Each
-- submission is logged for rate limits and the form's response count.
CREATE TABLE docs_form_submissions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    view_id    uuid NOT NULL REFERENCES docs_views (id) ON DELETE CASCADE,
    row_id     uuid REFERENCES docs_pages (id) ON DELETE SET NULL,
    user_id    uuid REFERENCES users (id),   -- NULL: anonymous or public
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX docs_form_submissions_view_idx ON docs_form_submissions (view_id, created_at DESC);
CREATE INDEX docs_form_submissions_user_idx ON docs_form_submissions (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX docs_form_submissions_row_idx ON docs_form_submissions (row_id) WHERE row_id IS NOT NULL;

-- ---- currencies in minor units -----------------------------------------------------

-- docs 1.1 stored currency numbers in major units with the formats dollar,
-- euro and pound; they become usd, eur and gbp in integer minor units.
DO $$
DECLARE
    d record;
    p jsonb;
BEGIN
    FOR d IN SELECT id, schema FROM docs_pages WHERE kind = 'database' AND jsonb_typeof(schema) = 'array' LOOP
        FOR p IN SELECT e FROM jsonb_array_elements(d.schema) AS e LOOP
            IF p->>'type' = 'number' AND p->>'number_format' IN ('dollar', 'euro', 'pound') THEN
                UPDATE docs_pages
                SET props = jsonb_set(props, ARRAY[p->>'id'], to_jsonb(round((props->>(p->>'id'))::numeric * 100)))
                WHERE parent_id = d.id AND is_row AND jsonb_typeof(props->(p->>'id')) = 'number';
            END IF;
        END LOOP;
        UPDATE docs_pages SET schema = (
            SELECT jsonb_agg(CASE e->>'number_format'
                    WHEN 'dollar' THEN jsonb_set(e, '{number_format}', '"usd"')
                    WHEN 'euro' THEN jsonb_set(e, '{number_format}', '"eur"')
                    WHEN 'pound' THEN jsonb_set(e, '{number_format}', '"gbp"')
                    ELSE e END ORDER BY ord)
            FROM jsonb_array_elements(d.schema) WITH ORDINALITY AS t(e, ord))
        WHERE id = d.id;
    END LOOP;
END $$;
