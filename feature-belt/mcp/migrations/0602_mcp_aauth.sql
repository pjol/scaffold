-- mcp 1.2.0: AAuth. Agents that sign their requests (draft-hardt-oauth-aauth-protocol)
-- reach /mcp through their person server (PS): the PS's identifier for the person,
-- (ps_iss, sub), is linked once to the account of whoever confirms it at /connect/aauth,
-- and every later call carries an auth token from that PS.

-- Signing keys for the resource tokens this server issues (internal/aauth/keys). The
-- private JWK is sealed with AES-256-GCM under AAUTH_KEY_ENCRYPTION_SECRET, with the role
-- and kid as additional data. Status follows from the timestamps: not_before in the
-- future is published but not signing; the newest past not_before signs; retire_at in
-- the future is published, past it the private part is wiped.
CREATE TABLE aauth_signing_keys (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    role        text NOT NULL CHECK (role IN ('ps', 'ss', 'resource')),
    kid         text NOT NULL UNIQUE,
    alg         text NOT NULL,
    public_jwk  jsonb NOT NULL,
    private_enc bytea,
    not_before  timestamptz NOT NULL DEFAULT now(),
    retire_at   timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX aauth_signing_keys_role_idx ON aauth_signing_keys (role, not_before DESC);

-- A person server's identifier for a person, linked to the account that confirmed it.
-- (ps_iss, sub) is the identifier: sub is unique only within its issuer. ps_name and
-- email are what the PS said when it was linked (its metadata name, and the verified
-- email in the auth token), kept for the person's list.
CREATE TABLE mcp_aauth_links (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ps_iss        text NOT NULL,
    sub           text NOT NULL,
    ps_name       text NOT NULL DEFAULT '',
    email         text,
    tenant        text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz,
    revoked_at    timestamptz,
    revoked_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    revoke_reason text CHECK (revoke_reason IN ('user', 'staff'))
);
CREATE UNIQUE INDEX mcp_aauth_links_identity_idx ON mcp_aauth_links (ps_iss, sub) WHERE revoked_at IS NULL;
CREATE INDEX mcp_aauth_links_user_idx ON mcp_aauth_links (user_id, created_at DESC);
CREATE INDEX mcp_aauth_links_created_idx ON mcp_aauth_links (created_at DESC, id DESC);

-- Agents seen under a link, by the thumbprint of their signing key (no agent
-- identifier reaches a resource). Written at most once a minute per agent.
CREATE TABLE mcp_aauth_agents (
    link_id       uuid NOT NULL REFERENCES mcp_aauth_links (id) ON DELETE CASCADE,
    jkt           text NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (link_id, jkt)
);

-- A call from an identity no one has linked yet, held while the person links it. The
-- agent polls /aauth/pending/{id}; the person arrives at /connect/aauth with the code.
-- A wrong code counts against every open request sharing its first four symbols, and
-- five end them. Once linked, the first poll runs held_body and keeps the answer for
-- result_jti until result_until, so a lost response can be fetched again.
CREATE TABLE mcp_aauth_pending (
    id            text PRIMARY KEY,
    ps_iss        text NOT NULL,
    sub           text NOT NULL,
    tenant        text,
    email         text,
    agent_jkt     text NOT NULL,
    code_prefix   text NOT NULL,
    code_hash     bytea NOT NULL,
    failures      int NOT NULL DEFAULT 0,
    status        text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'interacting', 'linked', 'denied', 'failed')),
    viewer_id     uuid REFERENCES users (id) ON DELETE CASCADE,
    callback      text,
    held_body     bytea NOT NULL,
    held_headers  jsonb NOT NULL DEFAULT '{}',
    delivered_at  timestamptz,
    result_jti    text,
    result_status int,
    result_type   text,
    result_body   bytea,
    result_until  timestamptz,
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mcp_aauth_pending_code_idx ON mcp_aauth_pending (code_prefix) WHERE status = 'pending';
CREATE INDEX mcp_aauth_pending_identity_idx ON mcp_aauth_pending (ps_iss, sub, created_at DESC);

-- Revocations person servers sent: (iss, jti) refused until exp.
CREATE TABLE mcp_aauth_revocations (
    iss        text NOT NULL,
    jti        text NOT NULL,
    exp        timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (iss, jti)
);
CREATE INDEX mcp_aauth_revocations_exp_idx ON mcp_aauth_revocations (exp);
