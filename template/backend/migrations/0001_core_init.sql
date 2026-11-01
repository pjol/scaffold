-- core: identity, consent history, admin audit, account deletions.

CREATE TABLE users (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    auth_subject             text NOT NULL UNIQUE,          -- identity provider user id (Clerk "user_…", dev "dev_…")
    email                    text NOT NULL,
    display_name             text NOT NULL DEFAULT '',
    avatar_url               text,
    admin_role               text CHECK (admin_role IN ('support', 'admin')),
    status                   text NOT NULL DEFAULT 'active'
                             CHECK (status IN ('active', 'suspended', 'banned', 'deleted')),
    terms_version_accepted   text,
    privacy_version_accepted text,
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    deleted_at               timestamptz
);
CREATE INDEX users_email_idx ON users (lower(email));
CREATE INDEX users_created_idx ON users (created_at DESC, id DESC);

-- Every acceptance, kept as evidence of consent (survives account deletion,
-- which anonymises the user row).
CREATE TABLE legal_acceptances (
    id          bigserial PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id),
    document    text NOT NULL CHECK (document IN ('terms', 'privacy')),
    version     text NOT NULL,
    platform    text NOT NULL DEFAULT '',                   -- web | ios | android
    ip          text NOT NULL DEFAULT '',
    accepted_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX legal_acceptances_user_idx ON legal_acceptances (user_id, accepted_at DESC);

-- Admin mutations, written in the same transaction as the change.
CREATE TABLE admin_audit (
    id          bigserial PRIMARY KEY,
    admin_id    uuid REFERENCES users (id),
    action      text NOT NULL,                              -- dotted: user.suspend, chat.message.remove
    target_type text NOT NULL,
    target_id   text NOT NULL,
    data        jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_created_idx ON admin_audit (created_at DESC, id DESC);
CREATE INDEX admin_audit_target_idx ON admin_audit (target_type, target_id, created_at DESC);

-- Deletion requests. The database part is done in the request; the identity
-- provider and object storage are finished (and retried) by a worker, which
-- clears auth_subject when both are done.
CREATE TABLE account_deletions (
    id               bigserial PRIMARY KEY,
    user_id          uuid NOT NULL REFERENCES users (id),
    auth_subject     text,
    source           text NOT NULL CHECK (source IN ('user', 'admin', 'provider')),
    provider_deleted boolean NOT NULL DEFAULT false,
    storage_deleted  boolean NOT NULL DEFAULT false,
    attempts         int NOT NULL DEFAULT 0,
    last_error       text NOT NULL DEFAULT '',
    requested_at     timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz
);
CREATE INDEX account_deletions_pending_idx ON account_deletions (requested_at) WHERE completed_at IS NULL;

-- Inbound webhook events already handled (webhooks.Once): "<source>:<event id>".
CREATE TABLE webhook_events (
    id          text PRIMARY KEY,
    source      text NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX webhook_events_received_idx ON webhook_events (received_at);
