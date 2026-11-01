-- mcp: an OAuth 2.1 authorization server so remote MCP clients (Claude,
-- ChatGPT, Cursor, Claude Code) connect by URL and the person signs in with
-- their own account. The app's sign-in (Clerk, or dev identities) only
-- authenticates the person on the consent page; everything an agent holds was
-- issued here.
--
-- Rules every table follows:
--  1. Only SHA-256 hashes of bearer values (request handles, codes, access and
--     refresh tokens, client secrets) are stored.
--  2. Single-use values are spent in the statement that reads them.
--  3. Nothing here decides access on its own: the user's status is checked
--     live on every token exchange and every MCP call.

-- Clients: dynamically registered (RFC 7591) or identified by the https URL of
-- a client ID metadata document. Registration grants nothing; it fixes which
-- redirect URIs a client id may use.
CREATE TABLE mcp_oauth_clients (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id           text NOT NULL UNIQUE,                 -- issued id, or the metadata document URL
    kind                text NOT NULL CHECK (kind IN ('registered', 'metadata_document')),
    client_name         text NOT NULL DEFAULT '',             -- self-declared; shown with that caveat
    redirect_uris       text[] NOT NULL CHECK (cardinality(redirect_uris) BETWEEN 1 AND 10),
    auth_method         text NOT NULL DEFAULT 'none'
                        CHECK (auth_method IN ('none', 'client_secret_post', 'client_secret_basic')),
    secret_hash         text,                                 -- confidential registered clients only
    metadata_expires_at timestamptz,                          -- metadata documents: refetch after this
    last_used_at        timestamptz,                          -- last code issued; NULL = never used (pruned)
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((auth_method = 'none') = (secret_hash IS NULL))
);
CREATE INDEX mcp_oauth_clients_unused_idx ON mcp_oauth_clients (created_at) WHERE last_used_at IS NULL;

-- Authorization requests parked while the person signs in and decides. The
-- browser carries only the handle; the PKCE challenge and redirect URI stay here.
CREATE TABLE mcp_oauth_requests (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    handle_hash    text NOT NULL UNIQUE,
    client_ref     uuid NOT NULL REFERENCES mcp_oauth_clients (id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    state          text NOT NULL DEFAULT '',                  -- the client's, returned untouched
    code_challenge text NOT NULL,                             -- S256
    scopes         text[] NOT NULL,
    resource       text NOT NULL,                             -- RFC 8707 audience
    expires_at     timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mcp_oauth_requests_expiry_idx ON mcp_oauth_requests (expires_at);

-- Grants: one per approved authorization, the family every token from that
-- code and its refreshes belongs to. Revoking a grant ends all of them.
CREATE TABLE mcp_oauth_grants (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id),
    client_ref    uuid NOT NULL REFERENCES mcp_oauth_clients (id),
    scopes        text[] NOT NULL CHECK (scopes <@ ARRAY['read', 'write']::text[] AND 'read' = ANY (scopes)),
    resource      text NOT NULL,
    expires_at    timestamptz NOT NULL,                       -- the newest refresh token's expiry
    first_used_at timestamptz,
    last_used_at  timestamptz,                                -- written at most once a minute
    revoked_at    timestamptz,
    revoked_by    uuid REFERENCES users (id),                 -- staff member; NULL otherwise
    revoke_reason text CHECK (revoke_reason IN ('user', 'staff', 'client', 'refresh_reuse', 'code_reuse')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mcp_oauth_grants_user_idx ON mcp_oauth_grants (user_id, client_ref) WHERE revoked_at IS NULL;
CREATE INDEX mcp_oauth_grants_client_idx ON mcp_oauth_grants (client_ref);
CREATE INDEX mcp_oauth_grants_created_idx ON mcp_oauth_grants (created_at DESC, id DESC);

-- Authorization codes: five minutes, single use, bound to client, redirect URI
-- and PKCE challenge. grant_id is set on exchange so a replay can revoke it.
CREATE TABLE mcp_oauth_codes (
    code_hash      text PRIMARY KEY,
    client_ref     uuid NOT NULL REFERENCES mcp_oauth_clients (id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users (id),
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    scopes         text[] NOT NULL,
    resource       text NOT NULL,
    grant_id       uuid REFERENCES mcp_oauth_grants (id) ON DELETE CASCADE,
    expires_at     timestamptz NOT NULL,
    consumed_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mcp_oauth_codes_user_idx ON mcp_oauth_codes (user_id);
CREATE INDEX mcp_oauth_codes_expiry_idx ON mcp_oauth_codes (expires_at);

-- Tokens: an access/refresh pair per issue. Refresh tokens rotate; a spent
-- one stays until it expires so presenting it again is detected.
CREATE TABLE mcp_oauth_tokens (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    grant_id           uuid NOT NULL REFERENCES mcp_oauth_grants (id) ON DELETE CASCADE,
    access_hash        text NOT NULL UNIQUE,
    refresh_hash       text NOT NULL UNIQUE,
    scopes             text[] NOT NULL,                       -- the access token's (may narrow the grant's)
    access_expires_at  timestamptz NOT NULL,
    refresh_expires_at timestamptz NOT NULL,
    refresh_used_at    timestamptz,
    revoked_at         timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mcp_oauth_tokens_grant_idx ON mcp_oauth_tokens (grant_id);
CREATE INDEX mcp_oauth_tokens_expiry_idx ON mcp_oauth_tokens (refresh_expires_at);

-- Agent credentials start with agt_ (auth.AgentCredentialPrefix) so the core
-- auth middleware refuses them on user routes, and /mcp accepts nothing else.
-- Keys minted before this release lack it: revoke them (owners create new ones).
UPDATE mcp_api_keys SET revoked_at = now(), updated_at = now()
WHERE revoked_at IS NULL AND prefix NOT LIKE 'agt\_%';
