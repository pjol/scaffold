-- Social: public profiles (handle, bio, links, privacy per section), the
-- follow graph and the activity feed. Display name and photo stay on users
-- (core); blocks are moderation's (moderation_blocks), never copied here.

-- One per person, created on first use (or by the profiles job). Handles are
-- stored lowercase, so uniqueness is case-insensitive.
CREATE TABLE social_profiles (
    user_id           uuid PRIMARY KEY REFERENCES users (id),
    handle            text NOT NULL CHECK (handle ~ '^[a-z][a-z0-9_]{2,19}$'),
    bio               text NOT NULL DEFAULT '',
    links             jsonb NOT NULL DEFAULT '[]',   -- [{"url", "label"}], at most 5
    privacy           jsonb NOT NULL DEFAULT '{}',   -- section key → public | followers | private; a missing key is the section's default
    followers         integer NOT NULL DEFAULT 0 CHECK (followers >= 0),
    following         integer NOT NULL DEFAULT 0 CHECK (following >= 0),
    handle_changed_at timestamptz,                   -- last change by the person (the cooldown starts here)
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
-- text_pattern_ops serves equality and the prefix search (handle LIKE 'sa%').
CREATE UNIQUE INDEX social_profiles_handle_key ON social_profiles (handle text_pattern_ops);
CREATE INDEX social_profiles_followers_idx ON social_profiles (followers DESC, user_id);

-- Handles given up. Each redirects to its owner's current handle and nobody
-- else can claim it until expires_at. user_id NULL: released by a deleted or
-- moderated account, held without redirecting anywhere.
CREATE TABLE social_handle_history (
    handle     text PRIMARY KEY,
    user_id    uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX social_handle_history_user_idx ON social_handle_history (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX social_handle_history_expires_idx ON social_handle_history (expires_at);

-- follower follows followee. Counters on social_profiles move in the same
-- transaction; recount() repairs them after bulk removals.
CREATE TABLE social_follows (
    follower_id uuid NOT NULL REFERENCES users (id),
    followee_id uuid NOT NULL REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followee_id),
    CHECK (follower_id <> followee_id)
);
CREATE INDEX social_follows_following_idx ON social_follows (follower_id, created_at DESC, followee_id DESC);
CREATE INDEX social_follows_followers_idx ON social_follows (followee_id, created_at DESC, follower_id DESC);

-- When a follow last notified the followee: at most one notification per
-- follower per hour, however often they unfollow and follow again.
CREATE TABLE social_follow_notices (
    follower_id uuid NOT NULL REFERENCES users (id),
    followee_id uuid NOT NULL REFERENCES users (id),
    sent_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (follower_id, followee_id)
);
CREATE INDEX social_follow_notices_sent_idx ON social_follow_notices (sent_at);

-- What people did, recorded by modules (social.Record). section is the
-- privacy section that gates it; subject_id is another person it is about.
-- Recording the same (actor, kind, ref_id) again updates the row.
CREATE TABLE social_activity (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id      uuid NOT NULL REFERENCES users (id),
    kind          text NOT NULL,
    section       text NOT NULL,
    ref_id        text NOT NULL DEFAULT '',
    subject_id    uuid REFERENCES users (id),
    data          jsonb NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    visible_after timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX social_activity_actor_idx ON social_activity (actor_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX social_activity_ref_key ON social_activity (actor_id, kind, ref_id) WHERE ref_id <> '';
CREATE INDEX social_activity_kind_ref_idx ON social_activity (kind, ref_id) WHERE ref_id <> '';
CREATE INDEX social_activity_subject_idx ON social_activity (subject_id) WHERE subject_id IS NOT NULL;
CREATE INDEX social_activity_created_idx ON social_activity (created_at);
