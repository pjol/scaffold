-- mcp: personal API keys that let agents call the app's tools over MCP as
-- their owner. Only a SHA-256 of each key is stored.

CREATE TABLE mcp_api_keys (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    name         text NOT NULL,
    prefix       text NOT NULL,                          -- first characters of the key, shown to tell keys apart
    secret_hash  text NOT NULL UNIQUE,                   -- hex SHA-256 of the full key
    scopes       text[] NOT NULL DEFAULT '{read}'
                 CHECK (scopes <@ ARRAY['read', 'write']::text[] AND 'read' = ANY (scopes)),
    last_used_at timestamptz,                            -- updated at most once a minute
    revoked_at   timestamptz,
    revoked_by   uuid REFERENCES users (id),             -- staff member who revoked it; NULL when the owner did
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mcp_api_keys_user_idx ON mcp_api_keys (user_id, created_at DESC, id DESC);
CREATE INDEX mcp_api_keys_created_idx ON mcp_api_keys (created_at DESC, id DESC);
