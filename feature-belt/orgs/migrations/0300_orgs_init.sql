-- orgs: organizations, memberships with roles, email invites and shareable
-- invite links. Members, invites and links are owned by their org and go
-- with it when the org is deleted.

CREATE TABLE orgs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    slug       text NOT NULL UNIQUE,                          -- lowercase a-z0-9 and single hyphens, 3–40
    avatar_key text,                                          -- storage key, purpose org_avatar (public)
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX orgs_created_idx ON orgs (created_at DESC, id DESC);

-- Every org has at least one owner (enforced in the API under a row lock on orgs).
CREATE TABLE org_members (
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    role       text NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),        -- joined
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_members_user_idx ON org_members (user_id, created_at, org_id);
CREATE INDEX org_members_org_idx ON org_members (org_id, created_at, user_id);

-- Email invites. Only the sha256 of the token is stored; resending issues a
-- new token. Accepting needs the signed-in account's verified email to match.
CREATE TABLE org_invites (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    email        text NOT NULL,                               -- lowercase
    role         text NOT NULL CHECK (role IN ('admin', 'member')),
    token_hash   bytea NOT NULL UNIQUE,
    status       text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'accepted', 'declined', 'revoked', 'expired')),
    invited_by   uuid REFERENCES users (id),
    accepted_by  uuid REFERENCES users (id),
    accepted_at  timestamptz,
    sent_count   int NOT NULL DEFAULT 1,
    last_sent_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX org_invites_one_pending_idx ON org_invites (org_id, email) WHERE status = 'pending';
CREATE INDEX org_invites_org_idx ON org_invites (org_id, created_at DESC, id DESC);
CREATE INDEX org_invites_email_idx ON org_invites (email, created_at DESC) WHERE status = 'pending';
CREATE INDEX org_invites_invited_by_idx ON org_invites (invited_by);

-- Shareable links: anyone signed in with the link joins as a member, until
-- it is revoked, expires or runs out of uses. Only the token hash is stored.
CREATE TABLE org_invite_links (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    role       text NOT NULL DEFAULT 'member' CHECK (role IN ('member')),
    token_hash bytea NOT NULL UNIQUE,
    max_uses   int CHECK (max_uses > 0),                     -- NULL = unlimited
    uses       int NOT NULL DEFAULT 0,
    expires_at timestamptz,                                  -- NULL = never
    revoked_at timestamptz,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX org_invite_links_org_idx ON org_invite_links (org_id, created_at DESC, id DESC);
CREATE INDEX org_invite_links_created_by_idx ON org_invite_links (created_by);
