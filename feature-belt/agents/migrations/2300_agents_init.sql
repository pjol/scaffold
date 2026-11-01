-- agents: how far AI agents acting for a user may go (the user's "agent
-- scope"), and the suggestions agents leave when a write needs the user's OK.
--
-- Levels, stored as text and ranked 0–3 in SQL: off (agents can't see it),
-- read (read tools only; the default), suggest (writes wait for the user),
-- edit (writes run). A scope only ever narrows the user's own permissions.
--
-- Resolution for one resource (agents_resolve): the resource's own override →
-- the nearest ancestor's override up the access registry's parent chain →
-- the feature's override → the user's default. "Turn off all agent access"
-- (paused) makes everything off and keeps every setting.

-- ---- settings ----------------------------------------------------------------

-- No row = default read, not paused. Access is off while either the user
-- (paused) or staff (staff_paused_by) turned it off; only staff lift theirs.
CREATE TABLE agents_settings (
    user_id         uuid PRIMARY KEY REFERENCES users (id),
    default_level   text NOT NULL DEFAULT 'read' CHECK (default_level IN ('off', 'read', 'suggest', 'edit')),
    paused          boolean NOT NULL DEFAULT false,
    paused_at       timestamptz,
    staff_paused_by uuid REFERENCES users (id),
    staff_paused_at timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agents_settings_paused_idx ON agents_settings (paused_at DESC) WHERE paused;
CREATE INDEX agents_settings_staff_paused_idx ON agents_settings (staff_paused_by) WHERE staff_paused_by IS NOT NULL;

-- A level for a whole feature: a module name (docs, calendar) or one external
-- service (connectors:<connector id>).
CREATE TABLE agents_feature_scopes (
    user_id    uuid NOT NULL REFERENCES users (id),
    feature    text NOT NULL,
    level      text NOT NULL CHECK (level IN ('off', 'read', 'suggest', 'edit')),
    label      text NOT NULL DEFAULT '',  -- display name when it was set ("Linear"), for services no longer connected
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, feature)
);

-- A level for one resource. Access kinds (rows in access_resources) pass it
-- down to everything under them; other kinds (channels.channel,
-- calendar.event) apply to that resource only. feature is the kind's prefix.
CREATE TABLE agents_resource_scopes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id),
    feature     text NOT NULL,
    kind        text NOT NULL,
    resource_id text NOT NULL,
    level       text NOT NULL CHECK (level IN ('off', 'read', 'suggest', 'edit')),
    title       text NOT NULL DEFAULT '',  -- kinds outside access: the title the client showed (only this user sees it)
    link        text NOT NULL DEFAULT '',  -- kinds outside access: app path
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT agents_resource_scopes_key UNIQUE (user_id, kind, resource_id)
);
CREATE INDEX agents_resource_scopes_feature_idx ON agents_resource_scopes (user_id, feature);
CREATE INDEX agents_resource_scopes_list_idx ON agents_resource_scopes (user_id, created_at DESC, id DESC);

-- ---- agents and suggestions ----------------------------------------------------

-- Every agent that has called a tool for the user (seen by the policy), for
-- Settings → AI agents and "accept all from this agent".
CREATE TABLE agents_clients (
    user_id        uuid NOT NULL REFERENCES users (id),
    via            text NOT NULL,             -- mcp | assistant
    client_key     text NOT NULL,             -- the key or OAuth client id; the name when there is none
    client         text NOT NULL,             -- display name ("Claude", the key's name, "Assistant")
    last_seen_at   timestamptz NOT NULL DEFAULT now(),  -- written at most once a minute
    last_pushed_at timestamptz,               -- last suggestion push; pushes within 2 minutes after it are held
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, via, client_key)
);
CREATE INDEX agents_clients_seen_idx ON agents_clients (user_id, last_seen_at DESC);

-- A write an agent wanted to make while the scope said suggest: the deferred
-- tool call, what the user reviews, and the outcome. Accepting re-runs the
-- tool as the user (no agent marker), so their permissions at that moment
-- apply. 'accepting' is held while the tool runs; a stuck row turns failed.
CREATE TABLE agents_suggestions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id),
    via           text NOT NULL,
    client_key    text NOT NULL,
    client        text NOT NULL,
    tool          text NOT NULL,
    feature       text NOT NULL,
    source        text NOT NULL DEFAULT '',   -- outside service serving the tool ("Linear"); '' for the app's own
    input         jsonb NOT NULL,
    target_kind   text NOT NULL DEFAULT '',
    target_id     text NOT NULL DEFAULT '',
    summary       text NOT NULL,
    before_text   text NOT NULL DEFAULT '',
    after_text    text NOT NULL DEFAULT '',
    link          text NOT NULL DEFAULT '',
    status        text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'accepting', 'accepted', 'rejected', 'failed', 'expired')),
    result        jsonb,                      -- the tool's output on accept (dropped when large)
    error_code    text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    expires_at    timestamptz NOT NULL,
    decided_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agents_suggestions_status_idx ON agents_suggestions (user_id, status, created_at DESC, id DESC);
CREATE INDEX agents_suggestions_user_idx ON agents_suggestions (user_id, created_at DESC, id DESC);
CREATE INDEX agents_suggestions_client_idx ON agents_suggestions (user_id, via, client_key, status, created_at);
CREATE INDEX agents_suggestions_expiry_idx ON agents_suggestions (expires_at) WHERE status = 'pending';
CREATE INDEX agents_suggestions_stuck_idx ON agents_suggestions (updated_at) WHERE status = 'accepting';
CREATE INDEX agents_suggestions_decided_idx ON agents_suggestions (decided_at) WHERE decided_at IS NOT NULL;

-- ---- resolution ----------------------------------------------------------------

CREATE FUNCTION agents_level_rank(p_level text) RETURNS int LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE p_level WHEN 'off' THEN 0 WHEN 'read' THEN 1 WHEN 'suggest' THEN 2 WHEN 'edit' THEN 3 ELSE 0 END
$$;

-- agents_resolve(user, feature, kind, id, skip_self, ignore_pause): the
-- level (0–3) and where it comes from: paused | resource (the item's own) |
-- parent (an ancestor's, from_kind/from_id) | feature | default. skip_self
-- ignores the item's own override, or for a whole feature its feature
-- override (what it would inherit); ignore_pause
-- answers as if access were on (settings screens). kind and id may be ''
-- for a whole feature. Ancestors are walked in the access registry (up to
-- 32) whether or not the item inherits sharing: a scope follows the tree.
CREATE FUNCTION agents_resolve(p_user uuid, p_feature text, p_kind text, p_id text,
                               p_skip_self boolean DEFAULT false, p_ignore_pause boolean DEFAULT false)
RETURNS TABLE (agent_level int, from_type text, from_kind text, from_id text)
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v_default text;
    v_paused  boolean;
    v_level   text;
    v_kind    text;
    v_id      uuid;
    v_depth   int := 0;
BEGIN
    SELECT s.default_level, s.paused OR s.staff_paused_by IS NOT NULL INTO v_default, v_paused
      FROM agents_settings s WHERE s.user_id = p_user;
    IF COALESCE(v_paused, false) AND NOT p_ignore_pause THEN
        RETURN QUERY SELECT 0, 'paused'::text, ''::text, ''::text;
        RETURN;
    END IF;
    IF COALESCE(p_kind, '') <> '' AND COALESCE(p_id, '') <> '' THEN
        IF NOT p_skip_self THEN
            SELECT r.level INTO v_level FROM agents_resource_scopes r
             WHERE r.user_id = p_user AND r.kind = p_kind AND r.resource_id = p_id;
            IF v_level IS NOT NULL THEN
                RETURN QUERY SELECT agents_level_rank(v_level), 'resource'::text, p_kind, p_id;
                RETURN;
            END IF;
        END IF;
        IF p_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
           AND EXISTS (SELECT 1 FROM agents_resource_scopes r WHERE r.user_id = p_user) THEN
            SELECT a.parent_kind, a.parent_id INTO v_kind, v_id
              FROM access_resources a WHERE a.kind = p_kind AND a.id = p_id::uuid;
            WHILE v_id IS NOT NULL AND v_depth < 32 LOOP
                SELECT r.level INTO v_level FROM agents_resource_scopes r
                 WHERE r.user_id = p_user AND r.kind = v_kind AND r.resource_id = v_id::text;
                IF v_level IS NOT NULL THEN
                    RETURN QUERY SELECT agents_level_rank(v_level), 'parent'::text, v_kind, v_id::text;
                    RETURN;
                END IF;
                v_depth := v_depth + 1;
                SELECT a.parent_kind, a.parent_id INTO v_kind, v_id
                  FROM access_resources a WHERE a.kind = v_kind AND a.id = v_id;
            END LOOP;
        END IF;
    END IF;
    IF NOT (p_skip_self AND COALESCE(p_kind, '') = '') THEN
        SELECT f.level INTO v_level FROM agents_feature_scopes f WHERE f.user_id = p_user AND f.feature = p_feature;
    END IF;
    IF v_level IS NOT NULL THEN
        RETURN QUERY SELECT agents_level_rank(v_level), 'feature'::text, ''::text, ''::text;
        RETURN;
    END IF;
    RETURN QUERY SELECT agents_level_rank(COALESCE(v_default, 'read')), 'default'::text, ''::text, ''::text;
END $$;

-- agents_level(user, feature, kind, id): the level alone.
CREATE FUNCTION agents_level(p_user uuid, p_feature text, p_kind text, p_id text)
RETURNS int LANGUAGE sql STABLE AS $$
    SELECT agent_level FROM agents_resolve(p_user, p_feature, p_kind, p_id)
$$;

-- The access feature's extension point (stubbed in 0700): the most an agent
-- acting for the user may do with an access resource, as an access level.
-- off → 0 (none), read and suggest → 1 (view; a suggested write runs later
-- as the user, without the agent marker), edit → 3. access_level() also caps
-- it at the user's own level, so an agent never exceeds its user.
CREATE OR REPLACE FUNCTION access_agent_cap(p_user uuid, p_kind text, p_id uuid)
RETURNS int LANGUAGE sql STABLE AS $$
    SELECT CASE agents_level(p_user, split_part(p_kind, '.', 1), p_kind, p_id::text)
               WHEN 0 THEN 0
               WHEN 3 THEN 3
               ELSE 1
           END
$$;
