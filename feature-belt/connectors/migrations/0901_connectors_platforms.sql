-- Connected platforms (connectors 2.0): each person's accounts on outside
-- platforms (Google, Microsoft, GitHub, GitLab, Notion, HubSpot, Salesforce,
-- Zoom), made once in Settings → Connections and used by every feature
-- through app.Connections. Tokens are AES-256-GCM ciphertext bound to the row.

-- One person's account on one platform. Several per platform are allowed
-- (two Google accounts); the same account twice is not.
CREATE TABLE connector_platform_connections (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users(id),
  provider           text NOT NULL CHECK (provider ~ '^[a-z][a-z0-9]{1,29}$'),
  kind               text NOT NULL CHECK (kind IN ('oauth', 'token')),
  -- GitLab: the instance ('' = GITLAB_BASE_URL). Salesforce: the org's
  -- instance from the token answer.
  base_url           text NOT NULL DEFAULT '',
  instance_url       text NOT NULL DEFAULT '',
  account            text NOT NULL CHECK (length(account) BETWEEN 1 AND 200),
  account_id         text NOT NULL CHECK (length(account_id) BETWEEN 1 AND 200),
  status             text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'needs_reauth')),
  access_token_enc   bytea NOT NULL,
  refresh_token_enc  bytea,
  token_type         text NOT NULL DEFAULT 'Bearer',
  expires_at         timestamptz,
  -- Granted scopes as the platform reported them (space separated).
  scopes             text NOT NULL DEFAULT '',
  -- Capabilities the person turned on for this account (app.Capability keys).
  capabilities       text[] NOT NULL DEFAULT '{}' CHECK (cardinality(capabilities) <= 200),
  last_error         text NOT NULL DEFAULT '',
  last_error_at      timestamptz,
  last_used_at       timestamptz,
  connected_at       timestamptz NOT NULL DEFAULT now(),
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, provider, base_url, account_id)
);
CREATE INDEX connector_platform_connections_account ON connector_platform_connections (provider, account_id);

-- Platform sign-ins in progress (same three steps as MCP sign-ins: start,
-- callback, finish by the same signed-in person). connection_id is the
-- account being extended (incremental consent or reconnect), capability the
-- one to turn on when it finishes, return_to the app path to go back to.
CREATE TABLE connector_platform_flows (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid NOT NULL REFERENCES users(id),
  provider           text NOT NULL,
  connection_id      uuid REFERENCES connector_platform_connections(id) ON DELETE SET NULL,
  capability         text NOT NULL DEFAULT '',
  client             text NOT NULL CHECK (client IN ('web', 'mobile')),
  status             text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'authorized')),
  state_hash         text UNIQUE,
  finish_hash        text UNIQUE,
  code_verifier_enc  bytea,
  redirect_uri       text NOT NULL,
  base_url           text NOT NULL DEFAULT '',
  scopes             text NOT NULL DEFAULT '',
  return_to          text NOT NULL DEFAULT '',
  -- Filled by the callback, moved to the connection by finish.
  access_token_enc   bytea,
  refresh_token_enc  bytea,
  token_type         text NOT NULL DEFAULT 'Bearer',
  token_expires_at   timestamptz,
  granted_scopes     text NOT NULL DEFAULT '',
  instance_url       text NOT NULL DEFAULT '',
  account            text NOT NULL DEFAULT '',
  account_id         text NOT NULL DEFAULT '',
  expires_at         timestamptz NOT NULL,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connector_platform_flows_user ON connector_platform_flows (user_id);
CREATE INDEX connector_platform_flows_expiry ON connector_platform_flows (expires_at);

-- Workspace policy: which platforms and capabilities an org allows. A row
-- per platform the admins decided on; provider '*' is the default for the
-- rest. No row at all means everything is allowed.
CREATE TABLE connector_policies (
  org_id               uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
  provider             text NOT NULL CHECK (provider = '*' OR provider ~ '^[a-z][a-z0-9]{1,29}$'),
  allowed              boolean NOT NULL DEFAULT true,
  allow_writes         boolean NOT NULL DEFAULT true,
  denied_capabilities  text[] NOT NULL DEFAULT '{}' CHECK (cardinality(denied_capabilities) <= 200),
  updated_by           uuid REFERENCES users(id) ON DELETE SET NULL,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, provider)
);

-- How a queued revocation is sent: RFC 7009 (default), GitHub's grant
-- deletion, a JSON body with Basic auth (Notion), or DELETE with the token in
-- the path (HubSpot).
ALTER TABLE connector_revocations ADD COLUMN style text NOT NULL DEFAULT 'rfc7009'
  CHECK (style IN ('rfc7009', 'github_grant', 'json_basic', 'path_delete'));
