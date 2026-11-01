-- access: teams inside orgs; one registry of shareable resources that every
-- content feature writes to, with grants to people, teams and the whole org
-- (inherited along parent links), public links, and org permissions with role
-- defaults and team/person overrides.
--
-- access_level() is the single source of truth for what someone may do with a
-- resource. Go (access.LevelOf / access.Require) and feature list queries
-- (WHERE access_level($1, 'docs.page', p.id, $2) >= 1) call the same function.
-- Levels: 0 none, 1 view, 2 comment, 3 edit, 4 manage.

-- ---- teams -------------------------------------------------------------------

CREATE TABLE access_teams (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name        text NOT NULL,
    handle      text NOT NULL,                 -- lowercase a-z0-9 and single hyphens, 2–32; "@design"
    description text NOT NULL DEFAULT '',
    avatar_key  text,                          -- storage key, purpose access_team_avatar (public)
    created_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT access_teams_handle_key UNIQUE (org_id, handle)
);
CREATE INDEX access_teams_org_name_idx ON access_teams (org_id, lower(name), id);
CREATE INDEX access_teams_created_by_idx ON access_teams (created_by) WHERE created_by IS NOT NULL;

-- Only org members belong to a team (checked on add; leaving the org removes
-- you, see access_org_member_removed below).
CREATE TABLE access_team_members (
    team_id    uuid NOT NULL REFERENCES access_teams (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    role       text NOT NULL DEFAULT 'member' CHECK (role IN ('lead', 'member')),
    added_by   uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);
CREATE INDEX access_team_members_user_idx ON access_team_members (user_id, team_id);
CREATE INDEX access_team_members_list_idx ON access_team_members (team_id, role, created_at, user_id);
CREATE INDEX access_team_members_added_by_idx ON access_team_members (added_by) WHERE added_by IS NOT NULL;

-- ---- resources and grants ----------------------------------------------------

-- One row per shareable thing, written by the owning feature in the same
-- transaction as its own row (access.CreateResource / MoveResource /
-- DeleteResource). Deleting a row removes its grants, links and every
-- descendant's row (the parent key cascades): access fails closed.
CREATE TABLE access_resources (
    kind          text NOT NULL,                     -- registered in Go by the feature: docs.page, files.item
    id            uuid NOT NULL,                     -- the feature's own row id
    org_id        uuid REFERENCES orgs (id) ON DELETE CASCADE, -- NULL: personal (owner and people grants only)
    owner_id      uuid REFERENCES users (id),        -- NULL once the owner's account is deleted (org admins manage)
    parent_kind   text,
    parent_id     uuid,
    inherits      boolean NOT NULL DEFAULT true,     -- false ("restricted"): the parent's access stops here
    admins_manage boolean NOT NULL DEFAULT false,    -- org owners/admins manage it (the kind's option at creation)
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, id),
    CONSTRAINT access_resources_parent_fkey FOREIGN KEY (parent_kind, parent_id)
        REFERENCES access_resources (kind, id) ON DELETE CASCADE,
    CHECK ((parent_kind IS NULL) = (parent_id IS NULL)),
    CHECK (parent_id IS NULL OR parent_id <> id OR parent_kind <> kind)
);
CREATE INDEX access_resources_parent_idx ON access_resources (parent_kind, parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX access_resources_org_idx ON access_resources (org_id, kind);
CREATE INDEX access_resources_owner_idx ON access_resources (owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX access_resources_id_idx ON access_resources (id);

-- Grants add to what is inherited. subject_id is a users.id, an
-- access_teams.id in the resource's org, or the resource's org id (general
-- access "everyone in the org", at most edit).
CREATE TABLE access_grants (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_kind text NOT NULL,
    resource_id   uuid NOT NULL,
    subject_type  text NOT NULL CHECK (subject_type IN ('user', 'team', 'org')),
    subject_id    uuid NOT NULL,
    level         smallint NOT NULL CHECK (level BETWEEN 1 AND 4),
    granted_by    uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT access_grants_resource_fkey FOREIGN KEY (resource_kind, resource_id)
        REFERENCES access_resources (kind, id) ON DELETE CASCADE,
    CONSTRAINT access_grants_subject_key UNIQUE (resource_kind, resource_id, subject_type, subject_id),
    CHECK (subject_type <> 'org' OR level <= 3)
);
CREATE INDEX access_grants_subject_idx ON access_grants (subject_type, subject_id);
CREATE INDEX access_grants_granted_by_idx ON access_grants (granted_by) WHERE granted_by IS NOT NULL;

-- Public links: anyone with the URL, no account. Only the sha256 of the token
-- is stored; the URL is shown once. A link works while it is unrevoked,
-- unexpired, and its creator still holds access.share_public in the org.
CREATE TABLE access_links (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_kind text NOT NULL,
    resource_id   uuid NOT NULL,
    token_hash    bytea NOT NULL UNIQUE,
    level         smallint NOT NULL CHECK (level IN (1, 2)),  -- view or comment
    expires_at    timestamptz,                                 -- NULL = until revoked
    revoked_at    timestamptz,
    created_by    uuid REFERENCES users (id),
    revoked_by    uuid REFERENCES users (id),
    last_used_at  timestamptz,                                 -- written at most hourly
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT access_links_resource_fkey FOREIGN KEY (resource_kind, resource_id)
        REFERENCES access_resources (kind, id) ON DELETE CASCADE
);
CREATE INDEX access_links_resource_idx ON access_links (resource_kind, resource_id, created_at DESC, id DESC);
CREATE INDEX access_links_created_by_idx ON access_links (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX access_links_revoked_by_idx ON access_links (revoked_by) WHERE revoked_by IS NOT NULL;

-- ---- org permissions ---------------------------------------------------------

-- No row = the defaults the feature registered (access.RegisterPermission).
-- A row replaces the roles and may add teams and people.
CREATE TABLE access_permission_policies (
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    key        text NOT NULL,                     -- crm.export
    roles      text[] NOT NULL,                   -- org roles that hold it
    updated_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key)
);
CREATE INDEX access_permission_policies_updated_by_idx ON access_permission_policies (updated_by) WHERE updated_by IS NOT NULL;

CREATE TABLE access_permission_subjects (
    org_id       uuid NOT NULL,
    key          text NOT NULL,
    subject_type text NOT NULL CHECK (subject_type IN ('team', 'user')),
    subject_id   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key, subject_type, subject_id),
    FOREIGN KEY (org_id, key) REFERENCES access_permission_policies (org_id, key) ON DELETE CASCADE
);
CREATE INDEX access_permission_subjects_subject_idx ON access_permission_subjects (subject_type, subject_id);

-- ---- leaving an org ----------------------------------------------------------

-- Someone who leaves (or is removed from) an org leaves its teams and loses
-- their own grants and permission overrides there, so rejoining starts clean.
-- access_level() also requires current membership, so nothing leaks between
-- the delete and this trigger.
CREATE FUNCTION access_org_member_removed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM access_team_members tm USING access_teams t
     WHERE t.id = tm.team_id AND t.org_id = OLD.org_id AND tm.user_id = OLD.user_id;
    DELETE FROM access_grants g USING access_resources r
     WHERE g.subject_type = 'user' AND g.subject_id = OLD.user_id
       AND r.kind = g.resource_kind AND r.id = g.resource_id AND r.org_id = OLD.org_id;
    DELETE FROM access_permission_subjects
     WHERE org_id = OLD.org_id AND subject_type = 'user' AND subject_id = OLD.user_id;
    RETURN OLD;
END $$;

CREATE TRIGGER access_org_member_removed AFTER DELETE ON org_members
    FOR EACH ROW EXECUTE FUNCTION access_org_member_removed();

-- ---- agent scope (extension point) ------------------------------------------

-- access_agent_cap(user, kind, id): the most an AI agent acting for user may
-- do with the resource (0 none … 3 edit; agents never manage). This stub
-- allows edit everywhere. The agents feature replaces it in its own
-- migration with CREATE OR REPLACE FUNCTION, keeping this exact signature
-- and parameter names (off → 0, read → 1, suggest → 1, edit → 3; suggested
-- writes run later as the human, without the agent marker). access never
-- redefines it after this migration.
CREATE FUNCTION access_agent_cap(p_user uuid, p_kind text, p_id uuid)
RETURNS int LANGUAGE sql STABLE AS $$ SELECT 3 $$;

-- ---- the effective level -----------------------------------------------------

-- access_level(user, kind, id, via_agent): the level user has on the resource.
--   * No registry row, or the resource's org doesn't count user as a member: 0.
--   * Org owners/admins: 4 when the resource is admin-managed, has no owner,
--     or its owner has left the org (p_admin_rule = false skips this, to tell
--     an admin override apart from real access).
--   * Otherwise the highest of: owning the resource or an ancestor it inherits
--     from (4), and every grant to user, user's teams or the org on the
--     resource and on ancestors up the inheriting chain (at most 32 rows).
--   * p_via_agent (an AI agent acting for user): capped at
--     least(level, access_agent_cap(user, kind, id), 3).
-- Feature list queries pass access.ViaAgent(ctx) as the fourth argument:
--   WHERE access_level($1, 'docs.page', p.id, $2) >= 1
CREATE FUNCTION access_level(p_user uuid, p_kind text, p_id uuid, p_via_agent boolean DEFAULT false, p_admin_rule boolean DEFAULT true)
RETURNS int LANGUAGE plpgsql STABLE AS $$
DECLARE
    r       record;
    v_org   uuid;
    v_best  int := 0;
    v_lvl   int;
    v_depth int := 0;
BEGIN
    IF p_user IS NULL OR p_id IS NULL THEN
        RETURN 0;
    END IF;
    -- One statement for the resource, the caller's role in its org and, for
    -- org owners/admins only, whether the resource is admin-managed.
    SELECT a.kind, a.id, a.org_id, a.owner_id, a.parent_kind, a.parent_id, a.inherits, m.role,
           CASE WHEN m.role IN ('owner', 'admin') THEN
               a.admins_manage OR a.owner_id IS NULL
               OR NOT EXISTS (SELECT 1 FROM org_members o WHERE o.org_id = a.org_id AND o.user_id = a.owner_id)
           END AS admin_managed
      INTO r
      FROM access_resources a
      LEFT JOIN org_members m ON m.org_id = a.org_id AND m.user_id = p_user
     WHERE a.kind = p_kind AND a.id = p_id;
    IF NOT FOUND OR (r.org_id IS NOT NULL AND r.role IS NULL) THEN
        RETURN 0;
    END IF;
    v_org := r.org_id;
    IF p_admin_rule AND r.admin_managed THEN
        v_best := 4;
    ELSE
        LOOP
            IF r.owner_id = p_user THEN
                v_best := 4;
                EXIT;
            END IF;
            SELECT max(g.level) INTO v_lvl FROM access_grants g
             WHERE g.resource_kind = r.kind AND g.resource_id = r.id
               AND (   (g.subject_type = 'user' AND g.subject_id = p_user)
                    OR (g.subject_type = 'org' AND g.subject_id = v_org)
                    OR (g.subject_type = 'team' AND EXISTS (
                            SELECT 1 FROM access_team_members tm WHERE tm.team_id = g.subject_id AND tm.user_id = p_user)));
            IF v_lvl IS NOT NULL AND v_lvl > v_best THEN
                v_best := v_lvl;
            END IF;
            EXIT WHEN v_best >= 4 OR NOT r.inherits OR r.parent_id IS NULL;
            v_depth := v_depth + 1;
            EXIT WHEN v_depth >= 32;
            SELECT kind, id, owner_id, parent_kind, parent_id, inherits INTO r
              FROM access_resources WHERE kind = r.parent_kind AND id = r.parent_id;
            EXIT WHEN NOT FOUND;
        END LOOP;
    END IF;
    IF p_via_agent AND v_best > 0 THEN
        v_best := least(v_best, COALESCE(access_agent_cap(p_user, p_kind, p_id), 0), 3);
    END IF;
    RETURN v_best;
END $$;

-- access_visible(user, kind, via_agent): every resource of kind user can open,
-- with the level, computed top-down from user's grants, teams, orgs and
-- ownership. Same answer as access_level for each row (agent cap included);
-- join it for "everything I can see" lists and search instead of filtering a
-- large table row by row:
--   SELECT p.* FROM docs_pages p JOIN access_visible($1, 'docs.page', $2) v ON v.id = p.id
CREATE FUNCTION access_visible(p_user uuid, p_kind text, p_via_agent boolean DEFAULT false)
RETURNS TABLE (id uuid, level int) LANGUAGE sql STABLE AS $$
    WITH RECURSIVE
    member_of AS (
        SELECT org_id, role FROM org_members WHERE user_id = p_user
    ),
    seeds AS (
        SELECT g.resource_kind AS kind, g.resource_id AS id, g.level::int AS level
          FROM access_grants g
         WHERE (g.subject_type = 'user' AND g.subject_id = p_user)
            OR (g.subject_type = 'team' AND g.subject_id IN (SELECT team_id FROM access_team_members WHERE user_id = p_user))
            OR (g.subject_type = 'org' AND g.subject_id IN (SELECT org_id FROM member_of))
        UNION ALL
        SELECT r.kind, r.id, 4 FROM access_resources r WHERE r.owner_id = p_user
    ),
    tree (kind, id, level, depth) AS (
        SELECT kind, id, level, 0 FROM seeds
        UNION
        SELECT c.kind, c.id, t.level, t.depth + 1
          FROM tree t JOIN access_resources c ON c.parent_kind = t.kind AND c.parent_id = t.id
         WHERE c.inherits AND t.depth < 31
    ),
    best AS (
        SELECT x.id, max(x.level)::int AS level FROM (
            SELECT t.id, t.level
              FROM tree t JOIN access_resources r ON r.kind = t.kind AND r.id = t.id
             WHERE t.kind = p_kind AND (r.org_id IS NULL OR r.org_id IN (SELECT org_id FROM member_of))
            UNION ALL
            SELECT r.id, 4
              FROM access_resources r JOIN member_of m ON m.org_id = r.org_id AND m.role IN ('owner', 'admin')
             WHERE r.kind = p_kind AND (r.admins_manage OR r.owner_id IS NULL
                   OR NOT EXISTS (SELECT 1 FROM org_members o WHERE o.org_id = r.org_id AND o.user_id = r.owner_id))
        ) x
        GROUP BY x.id
    )
    SELECT b.id, b.level FROM best b WHERE NOT p_via_agent
    UNION ALL
    SELECT b.id, c.level
      FROM best b CROSS JOIN LATERAL (
          SELECT least(b.level, COALESCE(access_agent_cap(p_user, p_kind, b.id), 0), 3) AS level) c
     WHERE p_via_agent AND c.level > 0
$$;

-- access_permission_allowed(org, user, key, default roles): whether a member
-- holds an org permission. Go passes the registered defaults (they live in
-- code), so access.Allowed and the public-link check agree.
CREATE FUNCTION access_permission_allowed(p_org uuid, p_user uuid, p_key text, p_default_roles text[])
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1
          FROM org_members m
          LEFT JOIN access_permission_policies p ON p.org_id = m.org_id AND p.key = p_key
         WHERE m.org_id = p_org AND m.user_id = p_user
           AND (   m.role = ANY (COALESCE(p.roles, p_default_roles))
                OR EXISTS (
                    SELECT 1 FROM access_permission_subjects s
                     WHERE s.org_id = p_org AND s.key = p_key
                       AND (   (s.subject_type = 'user' AND s.subject_id = p_user)
                            OR (s.subject_type = 'team' AND EXISTS (
                                    SELECT 1 FROM access_team_members tm
                                     WHERE tm.team_id = s.subject_id AND tm.user_id = p_user)))))
    )
$$;

-- access_link_level(token hash, kind, id, share_public default roles): the
-- level a public link grants on a resource: the link's own resource, or one
-- that inherits from it. 0 when the link is revoked or expired, or its creator
-- no longer holds access.share_public in the org (checked live, so switching
-- public sharing off stops existing links at once).
CREATE FUNCTION access_link_level(p_hash bytea, p_kind text, p_id uuid, p_share_roles text[])
RETURNS int LANGUAGE plpgsql STABLE AS $$
DECLARE
    l       record;
    r       record;
    v_org   uuid;
    v_depth int := 0;
BEGIN
    SELECT k.resource_kind, k.resource_id, k.level, k.created_by INTO l
      FROM access_links k
     WHERE k.token_hash = p_hash AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now());
    IF NOT FOUND THEN
        RETURN 0;
    END IF;
    SELECT org_id INTO v_org FROM access_resources WHERE kind = l.resource_kind AND id = l.resource_id;
    IF NOT FOUND THEN
        RETURN 0;
    END IF;
    IF v_org IS NOT NULL AND (l.created_by IS NULL
       OR NOT access_permission_allowed(v_org, l.created_by, 'access.share_public', p_share_roles)) THEN
        RETURN 0;
    END IF;
    SELECT kind, id, parent_kind, parent_id, inherits INTO r FROM access_resources WHERE kind = p_kind AND id = p_id;
    LOOP
        EXIT WHEN NOT FOUND;
        IF r.kind = l.resource_kind AND r.id = l.resource_id THEN
            RETURN l.level;
        END IF;
        EXIT WHEN NOT r.inherits OR r.parent_id IS NULL;
        v_depth := v_depth + 1;
        EXIT WHEN v_depth >= 32;
        SELECT kind, id, parent_kind, parent_id, inherits INTO r
          FROM access_resources WHERE kind = r.parent_kind AND id = r.parent_id;
    END LOOP;
    RETURN 0;
END $$;
