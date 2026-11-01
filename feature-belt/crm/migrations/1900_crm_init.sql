-- crm: companies, contacts and deals per org, with pipelines and stages,
-- custom properties, tags, activities (notes, calls, emails, meetings, tasks),
-- a timeline of changes, saved views and import history.
--
-- Every company, contact and deal is an access resource (crm.company,
-- crm.contact, crm.deal) under its team's crm.team resource (id = the team
-- id) or, without a team, the org's crm.workspace resource (id = the org id).
-- The org's record access setting lives as grants on those two structural
-- resources, so access_level() / access_visible() decide every read. The
-- record owner is access_resources.owner_id; this schema has no owner column.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- ---- settings, pipelines, properties, tags -------------------------------------

CREATE TABLE crm_settings (
    org_id        uuid PRIMARY KEY REFERENCES orgs (id) ON DELETE CASCADE,
    -- everyone_edits: the org edits every record (org grant 3 on the workspace)
    -- everyone_views: the org views and logs activity (grant 2); owners edit
    -- owners_teams:   owners and the record's team edit (team grant 3); no org grant
    record_access text NOT NULL DEFAULT 'everyone_edits'
                  CHECK (record_access IN ('everyone_edits', 'everyone_views', 'owners_teams')),
    currency      text NOT NULL DEFAULT 'USD' CHECK (currency ~ '^[A-Z]{3}$'),
    updated_by    uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_settings_updated_by_idx ON crm_settings (updated_by) WHERE updated_by IS NOT NULL;

CREATE TABLE crm_pipelines (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name       text NOT NULL,
    position   int NOT NULL DEFAULT 0,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX crm_pipelines_name_key ON crm_pipelines (org_id, lower(name));
CREATE INDEX crm_pipelines_org_idx ON crm_pipelines (org_id, position, id);
CREATE INDEX crm_pipelines_created_by_idx ON crm_pipelines (created_by) WHERE created_by IS NOT NULL;

-- Stages in order. outcome won/lost closes a deal (probability 100 / 0).
CREATE TABLE crm_stages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id uuid NOT NULL REFERENCES crm_pipelines (id) ON DELETE CASCADE,
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name        text NOT NULL,
    position    int NOT NULL,
    probability smallint NOT NULL DEFAULT 0 CHECK (probability BETWEEN 0 AND 100),
    outcome     text NOT NULL DEFAULT 'open' CHECK (outcome IN ('open', 'won', 'lost')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (outcome = 'open' OR probability = CASE outcome WHEN 'won' THEN 100 ELSE 0 END)
);
CREATE INDEX crm_stages_pipeline_idx ON crm_stages (pipeline_id, position, id);

-- Custom properties an org adds per object type. Values live in the record's
-- props jsonb under key, validated on write by type.
CREATE TABLE crm_properties (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    object_type text NOT NULL CHECK (object_type IN ('company', 'contact', 'deal')),
    key         text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{0,39}$'),
    label       text NOT NULL,
    type        text NOT NULL CHECK (type IN ('text', 'number', 'date', 'select', 'multi_select', 'user',
                                              'url', 'email', 'phone', 'currency', 'checkbox')),
    options     jsonb NOT NULL DEFAULT '[]',   -- select / multi_select: [{"value","label"}]
    position    int NOT NULL DEFAULT 0,
    created_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT crm_properties_key_key UNIQUE (org_id, object_type, key)
);
CREATE INDEX crm_properties_list_idx ON crm_properties (org_id, object_type, position, id);
CREATE INDEX crm_properties_created_by_idx ON crm_properties (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_tags (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name       text NOT NULL,
    tone       text NOT NULL DEFAULT 'neutral'
               CHECK (tone IN ('neutral', 'primary', 'accent', 'success', 'warning', 'danger', 'info')),
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX crm_tags_name_key ON crm_tags (org_id, lower(name));
CREATE INDEX crm_tags_created_by_idx ON crm_tags (created_by) WHERE created_by IS NOT NULL;

-- ---- records ------------------------------------------------------------------

CREATE TABLE crm_companies (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (name <> ''),
    domain           text CHECK (domain = lower(domain) AND domain <> ''), -- example.com, no scheme or www
    industry         text NOT NULL DEFAULT '',
    employees        int CHECK (employees >= 0),
    phone            text NOT NULL DEFAULT '',
    city             text NOT NULL DEFAULT '',
    country          text NOT NULL DEFAULT '',
    description      text NOT NULL DEFAULT '',
    team_id          uuid REFERENCES access_teams (id) ON DELETE SET NULL,
    tag_ids          uuid[] NOT NULL DEFAULT '{}',
    props            jsonb NOT NULL DEFAULT '{}',
    created_by       uuid REFERENCES users (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    last_activity_at timestamptz
);
CREATE INDEX crm_companies_org_idx ON crm_companies (org_id, created_at DESC, id DESC);
CREATE INDEX crm_companies_name_idx ON crm_companies (org_id, lower(name), id);
CREATE INDEX crm_companies_domain_idx ON crm_companies (org_id, domain) WHERE domain IS NOT NULL;
CREATE INDEX crm_companies_name_trgm_idx ON crm_companies USING gin (name gin_trgm_ops);
CREATE INDEX crm_companies_tags_idx ON crm_companies USING gin (tag_ids);
CREATE INDEX crm_companies_props_idx ON crm_companies USING gin (props jsonb_path_ops);
CREATE INDEX crm_companies_team_idx ON crm_companies (team_id) WHERE team_id IS NOT NULL;
CREATE INDEX crm_companies_created_by_idx ON crm_companies (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_contacts (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    first_name       text NOT NULL DEFAULT '',
    last_name        text NOT NULL DEFAULT '',
    emails           text[] NOT NULL DEFAULT '{}',   -- lowercase; the first is primary
    phones           text[] NOT NULL DEFAULT '{}',   -- the first is primary
    job_title        text NOT NULL DEFAULT '',
    lifecycle_stage  text NOT NULL DEFAULT 'lead'
                     CHECK (lifecycle_stage IN ('subscriber', 'lead', 'mql', 'sql', 'opportunity', 'customer', 'evangelist', 'other')),
    city             text NOT NULL DEFAULT '',
    country          text NOT NULL DEFAULT '',
    team_id          uuid REFERENCES access_teams (id) ON DELETE SET NULL,
    tag_ids          uuid[] NOT NULL DEFAULT '{}',
    props            jsonb NOT NULL DEFAULT '{}',
    created_by       uuid REFERENCES users (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    last_activity_at timestamptz,
    CHECK (first_name <> '' OR last_name <> '' OR cardinality(emails) > 0)
);
CREATE INDEX crm_contacts_org_idx ON crm_contacts (org_id, created_at DESC, id DESC);
CREATE INDEX crm_contacts_name_idx ON crm_contacts (org_id, lower(first_name || ' ' || last_name), id);
CREATE INDEX crm_contacts_emails_idx ON crm_contacts USING gin (emails);
CREATE INDEX crm_contacts_name_trgm_idx ON crm_contacts USING gin ((first_name || ' ' || last_name) gin_trgm_ops);
CREATE INDEX crm_contacts_tags_idx ON crm_contacts USING gin (tag_ids);
CREATE INDEX crm_contacts_props_idx ON crm_contacts USING gin (props jsonb_path_ops);
CREATE INDEX crm_contacts_team_idx ON crm_contacts (team_id) WHERE team_id IS NOT NULL;
CREATE INDEX crm_contacts_created_by_idx ON crm_contacts (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_deals (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (name <> ''),
    amount           bigint CHECK (amount >= 0),           -- minor units of currency
    currency         text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    pipeline_id      uuid NOT NULL REFERENCES crm_pipelines (id),
    stage_id         uuid NOT NULL REFERENCES crm_stages (id),
    close_date       date,
    company_id       uuid REFERENCES crm_companies (id) ON DELETE SET NULL,
    stage_entered_at timestamptz NOT NULL DEFAULT now(),
    closed_at        timestamptz,                           -- set while in a won/lost stage
    team_id          uuid REFERENCES access_teams (id) ON DELETE SET NULL,
    tag_ids          uuid[] NOT NULL DEFAULT '{}',
    props            jsonb NOT NULL DEFAULT '{}',
    created_by       uuid REFERENCES users (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    last_activity_at timestamptz
);
CREATE INDEX crm_deals_org_idx ON crm_deals (org_id, created_at DESC, id DESC);
CREATE INDEX crm_deals_board_idx ON crm_deals (pipeline_id, stage_id, close_date, id);
CREATE INDEX crm_deals_stage_idx ON crm_deals (stage_id);
CREATE INDEX crm_deals_close_idx ON crm_deals (org_id, close_date);
CREATE INDEX crm_deals_closed_idx ON crm_deals (org_id, closed_at) WHERE closed_at IS NOT NULL;
CREATE INDEX crm_deals_company_idx ON crm_deals (company_id) WHERE company_id IS NOT NULL;
CREATE INDEX crm_deals_name_trgm_idx ON crm_deals USING gin (name gin_trgm_ops);
CREATE INDEX crm_deals_tags_idx ON crm_deals USING gin (tag_ids);
CREATE INDEX crm_deals_props_idx ON crm_deals USING gin (props jsonb_path_ops);
CREATE INDEX crm_deals_team_idx ON crm_deals (team_id) WHERE team_id IS NOT NULL;
CREATE INDEX crm_deals_created_by_idx ON crm_deals (created_by) WHERE created_by IS NOT NULL;

-- Stage history: one row per stay in a stage (time in stage = exited - entered).
CREATE TABLE crm_deal_stages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id     uuid NOT NULL REFERENCES crm_deals (id) ON DELETE CASCADE,
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    pipeline_id uuid NOT NULL,
    stage_id    uuid NOT NULL,         -- no FK: history outlives a deleted stage
    stage_name  text NOT NULL,
    entered_at  timestamptz NOT NULL DEFAULT now(),
    exited_at   timestamptz,
    entered_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_deal_stages_deal_idx ON crm_deal_stages (deal_id, entered_at DESC, id DESC);
CREATE INDEX crm_deal_stages_entered_by_idx ON crm_deal_stages (entered_by) WHERE entered_by IS NOT NULL;

-- ---- associations ---------------------------------------------------------------

CREATE TABLE crm_contact_companies (
    contact_id uuid NOT NULL REFERENCES crm_contacts (id) ON DELETE CASCADE,
    company_id uuid NOT NULL REFERENCES crm_companies (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    is_primary boolean NOT NULL DEFAULT false,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (contact_id, company_id)
);
CREATE UNIQUE INDEX crm_contact_companies_primary_idx ON crm_contact_companies (contact_id) WHERE is_primary;
CREATE INDEX crm_contact_companies_company_idx ON crm_contact_companies (company_id, contact_id);
CREATE INDEX crm_contact_companies_created_by_idx ON crm_contact_companies (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_deal_contacts (
    deal_id    uuid NOT NULL REFERENCES crm_deals (id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES crm_contacts (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (deal_id, contact_id)
);
CREATE INDEX crm_deal_contacts_contact_idx ON crm_deal_contacts (contact_id, deal_id);
CREATE INDEX crm_deal_contacts_created_by_idx ON crm_deal_contacts (created_by) WHERE created_by IS NOT NULL;

-- ---- activities and the timeline --------------------------------------------------

-- Notes, logged calls/emails/meetings and tasks. (record_type, record_id) is
-- the record it was logged on; crm_activity_records links it to every record
-- it shows on (that one included). Someone sees an activity when they can
-- view at least one linked record.
CREATE TABLE crm_activities (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    type             text NOT NULL CHECK (type IN ('note', 'call', 'email', 'meeting', 'task')),
    record_type      text NOT NULL CHECK (record_type IN ('company', 'contact', 'deal')),
    record_id        uuid NOT NULL,
    subject          text NOT NULL DEFAULT '',
    body             text NOT NULL DEFAULT '',             -- plain text with <@uuid> / <@team:uuid> mentions
    occurred_at      timestamptz NOT NULL DEFAULT now(),   -- when it happened (tasks: when created)
    duration_minutes int CHECK (duration_minutes BETWEEN 0 AND 1440),
    outcome          text,                                 -- call/meeting outcome (activities.go)
    direction        text CHECK (direction IN ('inbound', 'outbound')),
    due_at           timestamptz,                          -- tasks
    assignee_id      uuid REFERENCES users (id),
    reminder_at      timestamptz,
    reminded_at      timestamptz,                          -- the reminder went out (once)
    done_at          timestamptz,
    priority         text NOT NULL DEFAULT 'none' CHECK (priority IN ('none', 'low', 'medium', 'high')),
    mentions         uuid[] NOT NULL DEFAULT '{}',          -- people and teams mentioned in body
    agent            text,                                 -- the AI agent that wrote it for created_by
    created_by       uuid REFERENCES users (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (type = 'task' OR (due_at IS NULL AND assignee_id IS NULL AND reminder_at IS NULL AND done_at IS NULL))
);
CREATE INDEX crm_activities_org_idx ON crm_activities (org_id, occurred_at DESC, id DESC);
CREATE INDEX crm_activities_tasks_idx ON crm_activities (assignee_id, due_at, id) WHERE type = 'task';
CREATE INDEX crm_activities_reminders_idx ON crm_activities (reminder_at)
    WHERE type = 'task' AND reminder_at IS NOT NULL AND reminded_at IS NULL AND done_at IS NULL;
CREATE INDEX crm_activities_created_by_idx ON crm_activities (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_activity_records (
    activity_id uuid NOT NULL REFERENCES crm_activities (id) ON DELETE CASCADE,
    record_type text NOT NULL CHECK (record_type IN ('company', 'contact', 'deal')),
    record_id   uuid NOT NULL,
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (activity_id, record_type, record_id)
);
CREATE INDEX crm_activity_records_record_idx ON crm_activity_records (record_type, record_id, activity_id);

-- System entries on a record's timeline: created, field changes (one row per
-- save, data.changes = [{key, label, before, after}]), stage moves,
-- associations, merges, imports.
CREATE TABLE crm_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    record_type text NOT NULL CHECK (record_type IN ('company', 'contact', 'deal')),
    record_id   uuid NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('created', 'updated', 'stage_changed', 'associated', 'dissociated', 'merged', 'imported')),
    actor_id    uuid REFERENCES users (id),
    agent       text,
    data        jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_events_record_idx ON crm_events (record_type, record_id, created_at DESC, id DESC);
CREATE INDEX crm_events_actor_idx ON crm_events (actor_id) WHERE actor_id IS NOT NULL;

-- ---- views and imports --------------------------------------------------------------

-- Saved list views: personal (owner only) or shared with everyone who uses
-- the CRM in the org.
CREATE TABLE crm_views (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    object_type text NOT NULL CHECK (object_type IN ('company', 'contact', 'deal')),
    name        text NOT NULL CHECK (name <> ''),
    owner_id    uuid REFERENCES users (id),
    shared      boolean NOT NULL DEFAULT false,
    columns     text[] NOT NULL DEFAULT '{}',
    sort_key    text NOT NULL DEFAULT 'created_at',
    sort_dir    text NOT NULL DEFAULT 'desc' CHECK (sort_dir IN ('asc', 'desc')),
    filters     jsonb NOT NULL DEFAULT '[]',
    q           text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_views_list_idx ON crm_views (org_id, object_type, lower(name), id);
CREATE INDEX crm_views_owner_idx ON crm_views (owner_id) WHERE owner_id IS NOT NULL;

CREATE TABLE crm_imports (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    object_type   text NOT NULL CHECK (object_type IN ('company', 'contact', 'deal')),
    filename      text NOT NULL DEFAULT '',
    dedupe        text NOT NULL CHECK (dedupe IN ('update', 'skip', 'create')),
    status        text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'failed')),
    total_rows    int NOT NULL DEFAULT 0,
    processed     int NOT NULL DEFAULT 0,
    created_count int NOT NULL DEFAULT 0,
    updated_count int NOT NULL DEFAULT 0,
    skipped_count int NOT NULL DEFAULT 0,
    error_count   int NOT NULL DEFAULT 0,
    errors        jsonb NOT NULL DEFAULT '[]',  -- first 200: [{"row", "message"}]
    created_by    uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    finished_at   timestamptz
);
CREATE INDEX crm_imports_org_idx ON crm_imports (org_id, created_at DESC, id DESC);
CREATE INDEX crm_imports_created_by_idx ON crm_imports (created_by) WHERE created_by IS NOT NULL;
