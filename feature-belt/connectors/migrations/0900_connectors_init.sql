-- Connectors: external MCP servers each user connects with their own
-- account (OAuth or a pasted token), so the in-app assistant can reach what
-- that person can reach there. Secrets are AES-256-GCM ciphertext (bytea),
-- never plain text.

-- The catalog. owner_id NULL = staff-managed; set = a user's own server
-- (CONNECTORS_ALLOW_CUSTOM), visible only to that user.
CREATE TABLE connectors (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id                 uuid REFERENCES users(id),
  slug                     text NOT NULL CHECK (slug ~ '^[a-z][a-z0-9]{1,19}$'),
  name                     text NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  description              text NOT NULL DEFAULT '',
  icon_url                 text NOT NULL DEFAULT '',
  server_url               text NOT NULL,
  auth_type                text NOT NULL CHECK (auth_type IN ('oauth', 'bearer')),
  oauth_client_id          text NOT NULL DEFAULT '',
  oauth_client_secret_enc  bytea,
  oauth_scopes             text NOT NULL DEFAULT '',
  write_policy             text NOT NULL DEFAULT 'off' CHECK (write_policy IN ('disabled', 'off', 'on')),
  enabled                  boolean NOT NULL DEFAULT true,
  created_by               uuid REFERENCES users(id),
  created_at               timestamptz NOT NULL DEFAULT now(),
  updated_at               timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX connectors_catalog_slug ON connectors (slug) WHERE owner_id IS NULL;
CREATE UNIQUE INDEX connectors_owner_slug ON connectors (owner_id, slug) WHERE owner_id IS NOT NULL;
CREATE INDEX connectors_catalog_order ON connectors (created_at DESC, id DESC) WHERE owner_id IS NULL;

-- A user's connection to a connector: their tokens and their choices.
CREATE TABLE connector_connections (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              uuid NOT NULL REFERENCES users(id),
  connector_id         uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'needs_reauth')),
  access_token_enc     bytea NOT NULL,
  refresh_token_enc    bytea,
  token_type           text NOT NULL DEFAULT 'Bearer',
  expires_at           timestamptz,
  scopes               text NOT NULL DEFAULT '',
  -- How to refresh and revoke (copied at connect time; empty for pasted tokens).
  resource             text NOT NULL DEFAULT '',
  issuer               text NOT NULL DEFAULT '',
  token_endpoint       text NOT NULL DEFAULT '',
  revocation_endpoint  text NOT NULL DEFAULT '',
  client_id            text NOT NULL DEFAULT '',
  client_secret_enc    bytea,
  token_auth_method    text NOT NULL DEFAULT 'none',
  writes_enabled       boolean NOT NULL DEFAULT false,
  disabled_tools       text[] NOT NULL DEFAULT '{}',
  last_error           text NOT NULL DEFAULT '',
  last_error_at        timestamptz,
  last_used_at         timestamptz,
  connected_at         timestamptz NOT NULL DEFAULT now(),
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, connector_id)
);
CREATE INDEX connector_connections_connector ON connector_connections (connector_id);

-- Client registrations obtained through Dynamic Client Registration
-- (RFC 7591), shared by every user of one authorization server.
CREATE TABLE connector_oauth_clients (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  issuer               text NOT NULL,
  redirect_uri         text NOT NULL,
  client_id            text NOT NULL,
  client_secret_enc    bytea,
  token_auth_method    text NOT NULL DEFAULT 'none',
  secret_expires_at    timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (issuer, redirect_uri)
);

-- Authorization flows in progress. state_hash (SHA-256 of the state sent to
-- the provider) is consumed once by the callback; finish_hash (SHA-256 of the
-- secret handed to the browser that came back) is consumed once by the
-- signed-in client, which must be the user who started the flow.
CREATE TABLE connector_oauth_flows (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              uuid NOT NULL REFERENCES users(id),
  connector_id         uuid NOT NULL REFERENCES connectors(id) ON DELETE CASCADE,
  client               text NOT NULL CHECK (client IN ('web', 'mobile')),
  status               text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'authorized')),
  state_hash           text UNIQUE,
  finish_hash          text UNIQUE,
  code_verifier_enc    bytea NOT NULL,
  redirect_uri         text NOT NULL,
  resource             text NOT NULL,
  issuer               text NOT NULL,
  iss_supported        boolean NOT NULL DEFAULT false,
  token_endpoint       text NOT NULL,
  revocation_endpoint  text NOT NULL DEFAULT '',
  client_id            text NOT NULL,
  client_secret_enc    bytea,
  token_auth_method    text NOT NULL DEFAULT 'none',
  scopes               text NOT NULL DEFAULT '',
  -- Filled by the callback, moved to connector_connections by finish.
  access_token_enc     bytea,
  refresh_token_enc    bytea,
  token_type           text NOT NULL DEFAULT 'Bearer',
  token_expires_at     timestamptz,
  granted_scopes       text NOT NULL DEFAULT '',
  expires_at           timestamptz NOT NULL,
  created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connector_oauth_flows_user ON connector_oauth_flows (user_id);
CREATE INDEX connector_oauth_flows_expiry ON connector_oauth_flows (expires_at);

-- Tokens waiting to be revoked at the provider (RFC 7009) after a
-- disconnect, a deleted connector or a deleted account. Rows go as soon as
-- the provider confirms, or after the last attempt.
CREATE TABLE connector_revocations (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              uuid REFERENCES users(id),
  revocation_endpoint  text NOT NULL,
  client_id            text NOT NULL,
  client_secret_enc    bytea,
  token_auth_method    text NOT NULL DEFAULT 'none',
  token_enc            bytea NOT NULL,
  token_type_hint      text NOT NULL CHECK (token_type_hint IN ('access_token', 'refresh_token')),
  attempts             int NOT NULL DEFAULT 0,
  next_attempt_at      timestamptz NOT NULL DEFAULT now(),
  created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connector_revocations_due ON connector_revocations (next_attempt_at);
CREATE INDEX connector_revocations_user ON connector_revocations (user_id);
