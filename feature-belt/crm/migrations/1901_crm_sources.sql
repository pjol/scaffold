-- crm: connected CRMs (HubSpot, Salesforce) per workspace, and links from
-- the app's companies to a platform company or account.
--
-- source_mode: app (the app's own CRM), connected (people work in their
-- connected CRM; the app's records are hidden), both (side by side).
-- Platform records are never copied here: they are read live with each
-- person's own connection. A link stores only which platform record a
-- company points at; each viewer sees it through their own connection.

ALTER TABLE crm_settings
    ADD COLUMN source_mode      text NOT NULL DEFAULT 'app' CHECK (source_mode IN ('app', 'connected', 'both')),
    ADD COLUMN source_providers text[] NOT NULL DEFAULT '{hubspot,salesforce}'
        CHECK (source_providers <@ ARRAY['hubspot', 'salesforce']::text[]);

CREATE TABLE crm_external_links (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    company_id  uuid NOT NULL REFERENCES crm_companies (id) ON DELETE CASCADE,
    provider    text NOT NULL CHECK (provider IN ('hubspot', 'salesforce')),
    -- the platform account: HubSpot portal id, Salesforce org id
    account_id  text NOT NULL CHECK (account_id <> '' AND length(account_id) <= 200),
    -- HubSpot company id, Salesforce Account id
    external_id text NOT NULL CHECK (external_id ~ '^[A-Za-z0-9]{1,64}$'),
    created_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, provider, account_id)
);
CREATE INDEX crm_external_links_org_idx ON crm_external_links (org_id);
CREATE INDEX crm_external_links_created_by_idx ON crm_external_links (created_by) WHERE created_by IS NOT NULL;
