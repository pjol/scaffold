-- crm 1.3: tickets (a fourth record type on their own pipelines), products,
-- line items and quotes, email templates and drafts, logged email metadata,
-- reports and dashboards, sales goals, workflows, forms and lead scoring.
--
-- New access kinds: crm.ticket (a record, like deals), crm.quote (under its
-- deal: whoever can open the deal can open its quotes; public links through
-- access), crm.form (public lead forms, links through access) and
-- crm.dashboard (saved reports shared through access).

-- ---- tickets ----------------------------------------------------------------------

ALTER TABLE crm_pipelines ADD COLUMN object_type text NOT NULL DEFAULT 'deal' CHECK (object_type IN ('deal', 'ticket'));
DROP INDEX crm_pipelines_name_key;
CREATE UNIQUE INDEX crm_pipelines_name_key ON crm_pipelines (org_id, object_type, lower(name));
DROP INDEX crm_pipelines_org_idx;
CREATE INDEX crm_pipelines_org_idx ON crm_pipelines (org_id, object_type, position, id);

-- Ticket stages are open or closed; deal stages open, won or lost.
ALTER TABLE crm_stages DROP CONSTRAINT crm_stages_outcome_check;
ALTER TABLE crm_stages ADD CONSTRAINT crm_stages_outcome_check CHECK (outcome IN ('open', 'won', 'lost', 'closed'));

-- Whether the next ticket pipeline still has to be created for an org
-- (orgs bootstrapped before 1.3 get theirs on first use).
ALTER TABLE crm_settings
    ADD COLUMN tickets_ready boolean NOT NULL DEFAULT false,
    ADD COLUMN quote_seq     int NOT NULL DEFAULT 0;

CREATE TABLE crm_tickets (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name              text NOT NULL CHECK (name <> ''),         -- the subject
    description       text NOT NULL DEFAULT '',
    priority          text NOT NULL DEFAULT 'medium' CHECK (priority IN ('low', 'medium', 'high', 'urgent')),
    category          text NOT NULL DEFAULT '' ,                -- free text: billing, product question…
    channel           text NOT NULL DEFAULT 'email' CHECK (channel IN ('email', 'phone', 'chat', 'form', 'other')),
    pipeline_id       uuid NOT NULL REFERENCES crm_pipelines (id),
    stage_id          uuid NOT NULL REFERENCES crm_stages (id),
    company_id        uuid REFERENCES crm_companies (id) ON DELETE SET NULL,
    deal_id           uuid REFERENCES crm_deals (id) ON DELETE SET NULL,
    stage_entered_at  timestamptz NOT NULL DEFAULT now(),
    first_response_at timestamptz,                              -- first email, call or meeting logged after it opened
    closed_at         timestamptz,                              -- set while in a closed stage
    team_id           uuid REFERENCES access_teams (id) ON DELETE SET NULL,
    tag_ids           uuid[] NOT NULL DEFAULT '{}',
    props             jsonb NOT NULL DEFAULT '{}',
    created_by        uuid REFERENCES users (id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    last_activity_at  timestamptz
);
CREATE INDEX crm_tickets_org_idx ON crm_tickets (org_id, created_at DESC, id DESC);
CREATE INDEX crm_tickets_board_idx ON crm_tickets (pipeline_id, stage_id, created_at, id);
CREATE INDEX crm_tickets_stage_idx ON crm_tickets (stage_id);
CREATE INDEX crm_tickets_company_idx ON crm_tickets (company_id) WHERE company_id IS NOT NULL;
CREATE INDEX crm_tickets_deal_idx ON crm_tickets (deal_id) WHERE deal_id IS NOT NULL;
CREATE INDEX crm_tickets_name_trgm_idx ON crm_tickets USING gin (name gin_trgm_ops);
CREATE INDEX crm_tickets_tags_idx ON crm_tickets USING gin (tag_ids);
CREATE INDEX crm_tickets_props_idx ON crm_tickets USING gin (props jsonb_path_ops);
CREATE INDEX crm_tickets_team_idx ON crm_tickets (team_id) WHERE team_id IS NOT NULL;
CREATE INDEX crm_tickets_created_by_idx ON crm_tickets (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_ticket_contacts (
    ticket_id  uuid NOT NULL REFERENCES crm_tickets (id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES crm_contacts (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (ticket_id, contact_id)
);
CREATE INDEX crm_ticket_contacts_contact_idx ON crm_ticket_contacts (contact_id, ticket_id);
CREATE INDEX crm_ticket_contacts_created_by_idx ON crm_ticket_contacts (created_by) WHERE created_by IS NOT NULL;

-- Every table that names a record type learns 'ticket'.
ALTER TABLE crm_properties DROP CONSTRAINT crm_properties_object_type_check;
ALTER TABLE crm_properties ADD CONSTRAINT crm_properties_object_type_check CHECK (object_type IN ('company', 'contact', 'deal', 'ticket'));
ALTER TABLE crm_activities DROP CONSTRAINT crm_activities_record_type_check;
ALTER TABLE crm_activities ADD CONSTRAINT crm_activities_record_type_check CHECK (record_type IN ('company', 'contact', 'deal', 'ticket'));
ALTER TABLE crm_activity_records DROP CONSTRAINT crm_activity_records_record_type_check;
ALTER TABLE crm_activity_records ADD CONSTRAINT crm_activity_records_record_type_check CHECK (record_type IN ('company', 'contact', 'deal', 'ticket'));
ALTER TABLE crm_events DROP CONSTRAINT crm_events_record_type_check;
ALTER TABLE crm_events ADD CONSTRAINT crm_events_record_type_check CHECK (record_type IN ('company', 'contact', 'deal', 'ticket'));
ALTER TABLE crm_events DROP CONSTRAINT crm_events_kind_check;
ALTER TABLE crm_events ADD CONSTRAINT crm_events_kind_check CHECK (kind IN ('created', 'updated', 'stage_changed', 'associated', 'dissociated',
    'merged', 'imported', 'quote', 'form_submitted', 'workflow', 'email_sent'));
ALTER TABLE crm_views DROP CONSTRAINT crm_views_object_type_check;
ALTER TABLE crm_views ADD CONSTRAINT crm_views_object_type_check CHECK (object_type IN ('company', 'contact', 'deal', 'ticket'));

-- ---- contacts: lead status and score ---------------------------------------------

ALTER TABLE crm_contacts
    ADD COLUMN lead_status text CHECK (lead_status IN ('new', 'open', 'in_progress', 'open_deal', 'unqualified',
                                                      'attempted_to_contact', 'connected', 'bad_timing')),
    ADD COLUMN score       int NOT NULL DEFAULT 0;
CREATE INDEX crm_contacts_score_idx ON crm_contacts (org_id, score DESC, id);

-- Rules: a contact matching every filter gets the points (negative allowed).
CREATE TABLE crm_score_rules (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    label      text NOT NULL CHECK (label <> '' AND length(label) <= 120),
    filters    jsonb NOT NULL,                              -- [{key, op, value}] on contact properties
    points     int NOT NULL CHECK (points BETWEEN -1000 AND 1000 AND points <> 0),
    position   int NOT NULL DEFAULT 0,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_score_rules_org_idx ON crm_score_rules (org_id, position, id);
CREATE INDEX crm_score_rules_created_by_idx ON crm_score_rules (created_by) WHERE created_by IS NOT NULL;

-- ---- deals: forecast category and amount from line items ---------------------------

ALTER TABLE crm_deals
    ADD COLUMN forecast_category text CHECK (forecast_category IN ('omitted', 'pipeline', 'best_case', 'commit', 'closed')),
    ADD COLUMN amount_from_items boolean NOT NULL DEFAULT false;

-- ---- products and line items -------------------------------------------------------

CREATE TABLE crm_products (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name           text NOT NULL CHECK (name <> '' AND length(name) <= 200),
    sku            text CHECK (sku <> '' AND length(sku) <= 64),
    description    text NOT NULL DEFAULT '',
    unit_price     bigint NOT NULL CHECK (unit_price >= 0),            -- minor units of currency
    currency       text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    billing        text NOT NULL DEFAULT 'one_time' CHECK (billing IN ('one_time', 'recurring')),
    billing_period text CHECK (billing_period IN ('monthly', 'quarterly', 'semiannual', 'annual')),
    term_periods   int CHECK (term_periods BETWEEN 1 AND 120),          -- default term for new line items (recurring)
    tax_bp         int NOT NULL DEFAULT 0 CHECK (tax_bp BETWEEN 0 AND 10000),       -- basis points: 825 = 8.25 %
    discount_bp    int NOT NULL DEFAULT 0 CHECK (discount_bp BETWEEN 0 AND 10000),  -- default discount for new line items
    archived_at    timestamptz,
    created_by     uuid REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CHECK ((billing = 'one_time') = (billing_period IS NULL))
);
CREATE UNIQUE INDEX crm_products_sku_key ON crm_products (org_id, lower(sku)) WHERE sku IS NOT NULL;
CREATE INDEX crm_products_org_idx ON crm_products (org_id, lower(name), id);
CREATE INDEX crm_products_name_trgm_idx ON crm_products USING gin (name gin_trgm_ops);
CREATE INDEX crm_products_created_by_idx ON crm_products (created_by) WHERE created_by IS NOT NULL;

-- Line items on a deal, in the deal's currency. A quote copies them when it
-- is made (crm_quote_items) so later edits don't change a sent quote.
CREATE TABLE crm_line_items (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    deal_id         uuid NOT NULL REFERENCES crm_deals (id) ON DELETE CASCADE,
    product_id      uuid REFERENCES crm_products (id) ON DELETE SET NULL,
    position        int NOT NULL DEFAULT 0,
    name            text NOT NULL CHECK (name <> '' AND length(name) <= 200),
    sku             text NOT NULL DEFAULT '',
    description     text NOT NULL DEFAULT '',
    unit_price      bigint NOT NULL CHECK (unit_price >= 0),
    quantity_milli  bigint NOT NULL CHECK (quantity_milli > 0 AND quantity_milli <= 1000000000), -- thousandths: 1500 = 1.5
    discount_bp     int NOT NULL DEFAULT 0 CHECK (discount_bp BETWEEN 0 AND 10000),
    discount_amount bigint NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),  -- fixed discount per line, minor units
    tax_bp          int NOT NULL DEFAULT 0 CHECK (tax_bp BETWEEN 0 AND 10000),
    billing         text NOT NULL DEFAULT 'one_time' CHECK (billing IN ('one_time', 'recurring')),
    billing_period  text CHECK (billing_period IN ('monthly', 'quarterly', 'semiannual', 'annual')),
    term_periods    int NOT NULL DEFAULT 1 CHECK (term_periods BETWEEN 1 AND 120),
    created_by      uuid REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CHECK ((billing = 'one_time') = (billing_period IS NULL)),
    CHECK (discount_bp = 0 OR discount_amount = 0)
);
CREATE INDEX crm_line_items_deal_idx ON crm_line_items (deal_id, position, id);
CREATE INDEX crm_line_items_product_idx ON crm_line_items (product_id) WHERE product_id IS NOT NULL;
CREATE INDEX crm_line_items_created_by_idx ON crm_line_items (created_by) WHERE created_by IS NOT NULL;

-- ---- quotes ------------------------------------------------------------------------

-- A quote is an access resource (crm.quote) under its deal. Publishing makes
-- an access public link (access checks it on every open: revoked, the
-- creator's access.share_public and crm.publish_quotes); link_token keeps the
-- token so the sender can copy the link again. Totals and the sender are
-- frozen when it is published.
CREATE TABLE crm_quotes (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    deal_id         uuid NOT NULL REFERENCES crm_deals (id) ON DELETE CASCADE,
    number          text NOT NULL,                                     -- Q-0001, per org
    title           text NOT NULL CHECK (title <> '' AND length(title) <= 200),
    status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'accepted', 'declined')),
    currency        text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    expires_at      timestamptz NOT NULL,
    terms           text NOT NULL DEFAULT '',
    comments        text NOT NULL DEFAULT '',                          -- a note to the buyer
    contact_id      uuid REFERENCES crm_contacts (id) ON DELETE SET NULL,  -- who it is for
    sender_id       uuid REFERENCES users (id),
    sender_name     text NOT NULL DEFAULT '',
    sender_email    text NOT NULL DEFAULT '',
    buyer_name      text NOT NULL DEFAULT '',                          -- frozen at publish: contact and company names
    buyer_company   text NOT NULL DEFAULT '',
    seller_name     text NOT NULL DEFAULT '',                          -- the workspace's name at publish
    subtotal        bigint NOT NULL DEFAULT 0,
    discount_total  bigint NOT NULL DEFAULT 0,
    tax_total       bigint NOT NULL DEFAULT 0,
    total           bigint NOT NULL DEFAULT 0,
    link_id         uuid,
    link_token      text,
    published_at    timestamptz,
    published_by    uuid REFERENCES users (id),
    first_viewed_at timestamptz,
    last_viewed_at  timestamptz,
    view_count      int NOT NULL DEFAULT 0,
    signer_name     text,                                              -- typed name on accept or decline
    signer_email    text,
    signer_ip       text,
    signer_agent    text,
    decided_at      timestamptz,
    decline_reason  text,
    agent           text,                                              -- the AI agent that drafted it
    created_by      uuid REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, number),
    CHECK ((status = 'draft') = (link_token IS NULL))
);
CREATE INDEX crm_quotes_deal_idx ON crm_quotes (deal_id, created_at DESC, id DESC);
CREATE INDEX crm_quotes_contact_idx ON crm_quotes (contact_id) WHERE contact_id IS NOT NULL;
CREATE INDEX crm_quotes_sender_idx ON crm_quotes (sender_id) WHERE sender_id IS NOT NULL;
CREATE INDEX crm_quotes_published_by_idx ON crm_quotes (published_by) WHERE published_by IS NOT NULL;
CREATE INDEX crm_quotes_created_by_idx ON crm_quotes (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_quote_items (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    quote_id        uuid NOT NULL REFERENCES crm_quotes (id) ON DELETE CASCADE,
    product_id      uuid REFERENCES crm_products (id) ON DELETE SET NULL,
    position        int NOT NULL DEFAULT 0,
    name            text NOT NULL CHECK (name <> ''),
    sku             text NOT NULL DEFAULT '',
    description     text NOT NULL DEFAULT '',
    unit_price      bigint NOT NULL CHECK (unit_price >= 0),
    quantity_milli  bigint NOT NULL CHECK (quantity_milli > 0),
    discount_bp     int NOT NULL DEFAULT 0 CHECK (discount_bp BETWEEN 0 AND 10000),
    discount_amount bigint NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    tax_bp          int NOT NULL DEFAULT 0 CHECK (tax_bp BETWEEN 0 AND 10000),
    billing         text NOT NULL DEFAULT 'one_time' CHECK (billing IN ('one_time', 'recurring')),
    billing_period  text CHECK (billing_period IN ('monthly', 'quarterly', 'semiannual', 'annual')),
    term_periods    int NOT NULL DEFAULT 1 CHECK (term_periods BETWEEN 1 AND 120)
);
CREATE INDEX crm_quote_items_quote_idx ON crm_quote_items (quote_id, position, id);
CREATE INDEX crm_quote_items_product_idx ON crm_quote_items (product_id) WHERE product_id IS NOT NULL;

-- ---- email ---------------------------------------------------------------------------

-- Logged and sent emails are crm_activities rows (type email); this is the
-- mail metadata of the ones sent from or logged out of a connected mailbox
-- (never fetched content nobody chose to keep).
ALTER TABLE crm_activities
    ADD COLUMN email jsonb;  -- {provider, connection_id, message_id, thread_id, from, to[], cc[]}
CREATE UNIQUE INDEX crm_activities_email_message_key ON crm_activities (org_id, (email->>'message_id'))
    WHERE email IS NOT NULL AND email->>'message_id' IS NOT NULL;

CREATE TABLE crm_email_templates (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    owner_id   uuid REFERENCES users (id) ON DELETE CASCADE,
    shared     boolean NOT NULL DEFAULT false,                 -- everyone who uses the CRM may use it
    name       text NOT NULL CHECK (name <> '' AND length(name) <= 120),
    subject    text NOT NULL DEFAULT '' CHECK (length(subject) <= 300),
    body       text NOT NULL DEFAULT '' CHECK (length(body) <= 20000),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_email_templates_org_idx ON crm_email_templates (org_id, lower(name), id);
CREATE INDEX crm_email_templates_owner_idx ON crm_email_templates (owner_id) WHERE owner_id IS NOT NULL;

-- Drafts written in the app (by a person or their AI agent) for the person
-- to review and send from their own connected mailbox.
CREATE TABLE crm_email_drafts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,  -- whose draft it is (only they see and send it)
    contact_id uuid NOT NULL REFERENCES crm_contacts (id) ON DELETE CASCADE,
    deal_id    uuid REFERENCES crm_deals (id) ON DELETE SET NULL,
    ticket_id  uuid REFERENCES crm_tickets (id) ON DELETE SET NULL,
    to_emails  text[] NOT NULL DEFAULT '{}',
    cc_emails  text[] NOT NULL DEFAULT '{}',
    subject    text NOT NULL DEFAULT '' CHECK (length(subject) <= 300),
    body       text NOT NULL DEFAULT '' CHECK (length(body) <= 20000),
    agent      text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_email_drafts_user_idx ON crm_email_drafts (user_id, updated_at DESC, id DESC);
CREATE INDEX crm_email_drafts_contact_idx ON crm_email_drafts (contact_id);
CREATE INDEX crm_email_drafts_deal_idx ON crm_email_drafts (deal_id) WHERE deal_id IS NOT NULL;
CREATE INDEX crm_email_drafts_ticket_idx ON crm_email_drafts (ticket_id) WHERE ticket_id IS NOT NULL;

-- ---- reports, dashboards, goals -----------------------------------------------------------

-- A dashboard is an access resource (crm.dashboard): private to its owner
-- until shared. Each viewer sees every number computed over the records
-- they can open.
CREATE TABLE crm_dashboards (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (name <> '' AND length(name) <= 120),
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_dashboards_org_idx ON crm_dashboards (org_id, lower(name), id);
CREATE INDEX crm_dashboards_created_by_idx ON crm_dashboards (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_reports (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    dashboard_id uuid NOT NULL REFERENCES crm_dashboards (id) ON DELETE CASCADE,
    position     int NOT NULL DEFAULT 0,
    name         text NOT NULL CHECK (name <> '' AND length(name) <= 120),
    spec         jsonb NOT NULL,      -- {object, measure, property, group_by, bucket, filters, chart}
    created_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_reports_dashboard_idx ON crm_reports (dashboard_id, position, id);
CREATE INDEX crm_reports_created_by_idx ON crm_reports (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_goals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    metric       text NOT NULL CHECK (metric IN ('revenue_won', 'deals_won', 'deals_created')),
    user_id      uuid REFERENCES users (id) ON DELETE CASCADE,
    team_id      uuid REFERENCES access_teams (id) ON DELETE CASCADE,
    period       text NOT NULL CHECK (period IN ('month', 'quarter', 'year')),
    period_start date NOT NULL,
    target       bigint NOT NULL CHECK (target > 0),             -- minor units for revenue, a count otherwise
    currency     text CHECK (currency ~ '^[A-Z]{3}$'),
    created_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((user_id IS NULL) <> (team_id IS NULL)),
    CHECK ((metric = 'revenue_won') = (currency IS NOT NULL))
);
CREATE INDEX crm_goals_org_idx ON crm_goals (org_id, period_start DESC, id);
CREATE INDEX crm_goals_user_idx ON crm_goals (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX crm_goals_team_idx ON crm_goals (team_id) WHERE team_id IS NOT NULL;
CREATE INDEX crm_goals_created_by_idx ON crm_goals (created_by) WHERE created_by IS NOT NULL;

-- ---- workflows ---------------------------------------------------------------------------

-- A workflow runs as its owner (the person who last turned it on) with their
-- access. Runs: one per enrollment; delays park a run until resume_at.
CREATE TABLE crm_workflows (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (name <> '' AND length(name) <= 120),
    object_type text NOT NULL CHECK (object_type IN ('company', 'contact', 'deal', 'ticket')),
    enabled     boolean NOT NULL DEFAULT false,
    trigger     jsonb NOT NULL,     -- {type, property?, value?, stage_id?, date_property?, offset_days?}
    conditions  jsonb NOT NULL DEFAULT '[]',   -- list filters
    actions     jsonb NOT NULL DEFAULT '[]',   -- [{type, …}]
    reenroll    boolean NOT NULL DEFAULT false,
    owner_id    uuid REFERENCES users (id) ON DELETE SET NULL,
    secret      text NOT NULL,      -- signs webhook actions (X-CRM-Signature)
    state       jsonb NOT NULL DEFAULT '{}',   -- rotation positions by action index
    created_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_workflows_org_idx ON crm_workflows (org_id, object_type, enabled);
CREATE INDEX crm_workflows_owner_idx ON crm_workflows (owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX crm_workflows_created_by_idx ON crm_workflows (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_workflow_runs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id uuid NOT NULL REFERENCES crm_workflows (id) ON DELETE CASCADE,
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    record_type text NOT NULL CHECK (record_type IN ('company', 'contact', 'deal', 'ticket')),
    record_id   uuid NOT NULL,
    enroll_key  text NOT NULL,      -- dedupes enrollments: the record, or record + date for date triggers
    status      text NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'running', 'done', 'failed', 'stopped')),
    step        int NOT NULL DEFAULT 0,
    depth       int NOT NULL DEFAULT 0,
    resume_at   timestamptz NOT NULL DEFAULT now(),
    log         jsonb NOT NULL DEFAULT '[]',   -- [{step, type, status, message, at}]
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz
);
CREATE INDEX crm_workflow_runs_due_idx ON crm_workflow_runs (resume_at) WHERE status = 'waiting';
CREATE INDEX crm_workflow_runs_workflow_idx ON crm_workflow_runs (workflow_id, created_at DESC, id DESC);
CREATE INDEX crm_workflow_runs_enroll_idx ON crm_workflow_runs (workflow_id, enroll_key, created_at DESC);
CREATE INDEX crm_workflow_runs_record_idx ON crm_workflow_runs (record_type, record_id);

-- ---- forms -------------------------------------------------------------------------------

-- A public lead form (crm.form access resource; its public link comes from
-- access, link_token keeps it for the embed code). Submissions create or
-- update a contact by email, as the form's owner.
CREATE TABLE crm_forms (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name            text NOT NULL CHECK (name <> '' AND length(name) <= 120),
    fields          jsonb NOT NULL,     -- [{key, object, label, required}]
    submit_label    text NOT NULL DEFAULT 'Submit',
    success_message text NOT NULL DEFAULT '',
    consent_text    text NOT NULL DEFAULT '',         -- shown with a required checkbox when set
    owner_id        uuid REFERENCES users (id) ON DELETE SET NULL,  -- new contacts' owner; the form runs as them
    notify_ids      uuid[] NOT NULL DEFAULT '{}',
    lifecycle_stage text CHECK (lifecycle_stage IN ('subscriber', 'lead', 'mql', 'sql', 'opportunity', 'customer', 'evangelist', 'other')),
    tag_ids         uuid[] NOT NULL DEFAULT '{}',
    link_id         uuid,
    link_token      text,
    submission_count int NOT NULL DEFAULT 0,
    created_by      uuid REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_forms_org_idx ON crm_forms (org_id, lower(name), id);
CREATE INDEX crm_forms_owner_idx ON crm_forms (owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX crm_forms_created_by_idx ON crm_forms (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE crm_form_submissions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    form_id    uuid NOT NULL REFERENCES crm_forms (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    contact_id uuid REFERENCES crm_contacts (id) ON DELETE SET NULL,
    created    boolean NOT NULL,         -- a new contact (else an update)
    vals       jsonb NOT NULL,
    consent    boolean NOT NULL DEFAULT false,
    page_url   text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crm_form_submissions_form_idx ON crm_form_submissions (form_id, created_at DESC, id DESC);
CREATE INDEX crm_form_submissions_contact_idx ON crm_form_submissions (contact_id) WHERE contact_id IS NOT NULL;
