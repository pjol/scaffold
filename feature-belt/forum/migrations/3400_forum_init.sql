-- forum: communities, posts, threaded comments, votes, mentions.
-- Content status: visible | held (screening; author and staff only) |
-- removed | deleted (by the author; a placeholder in threads).

CREATE TABLE forum_communities (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$'),
    title       text NOT NULL,
    description text NOT NULL DEFAULT '',
    rules       text[] NOT NULL DEFAULT '{}',
    locked      boolean NOT NULL DEFAULT false,        -- only moderators post
    ref_kind    text,                                  -- the app object it belongs to (forum.EnsureCommunity)
    ref_id      text,
    created_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((ref_kind IS NULL) = (ref_id IS NULL))
);
CREATE UNIQUE INDEX forum_communities_ref_idx ON forum_communities (ref_kind, ref_id) WHERE ref_kind IS NOT NULL;

-- Community moderators pin, lock and remove in their community.
CREATE TABLE forum_moderators (
    community_id uuid NOT NULL REFERENCES forum_communities (id),
    user_id      uuid NOT NULL REFERENCES users (id),
    added_by     uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (community_id, user_id)
);
CREATE INDEX forum_moderators_user_idx ON forum_moderators (user_id);

CREATE TABLE forum_posts (
    id            uuid PRIMARY KEY,
    community_id  uuid NOT NULL REFERENCES forum_communities (id),
    author_id     uuid NOT NULL REFERENCES users (id),
    title         text NOT NULL,
    body          text NOT NULL DEFAULT '',             -- Markdown
    url           text,                                 -- link posts
    domain        text,
    labels        text[] NOT NULL DEFAULT '{}',         -- moderation's (off_topic) and the app's (forum.SetLabeler)
    mod_reasons   text[] NOT NULL DEFAULT '{}',         -- why screening held or removed it (author and staff)
    status        text NOT NULL CHECK (status IN ('visible', 'held', 'removed', 'deleted')),
    score         int NOT NULL DEFAULT 0,
    ups           int NOT NULL DEFAULT 0,
    downs         int NOT NULL DEFAULT 0,
    hot           double precision NOT NULL,            -- forum.Hot
    comment_count int NOT NULL DEFAULT 0,               -- visible comments
    pinned        boolean NOT NULL DEFAULT false,
    locked        boolean NOT NULL DEFAULT false,       -- no new comments
    notified      boolean NOT NULL DEFAULT false,       -- mentions were told (held posts tell on approval)
    search        tsvector GENERATED ALWAYS AS (to_tsvector('simple', title || ' ' || body)) STORED,
    created_at    timestamptz NOT NULL DEFAULT now(),
    edited_at     timestamptz,
    removed_at    timestamptz
);
CREATE INDEX forum_posts_hot_idx ON forum_posts (hot DESC, id DESC) WHERE status = 'visible';
CREATE INDEX forum_posts_new_idx ON forum_posts (created_at DESC, id DESC) WHERE status = 'visible';
CREATE INDEX forum_posts_top_idx ON forum_posts (score DESC, created_at DESC, id DESC) WHERE status = 'visible';
CREATE INDEX forum_posts_community_hot_idx ON forum_posts (community_id, hot DESC, id DESC) WHERE status = 'visible';
CREATE INDEX forum_posts_community_new_idx ON forum_posts (community_id, created_at DESC, id DESC) WHERE status = 'visible';
CREATE INDEX forum_posts_community_top_idx ON forum_posts (community_id, score DESC, created_at DESC, id DESC) WHERE status = 'visible';
CREATE INDEX forum_posts_pinned_idx ON forum_posts (community_id) WHERE pinned AND status = 'visible';
CREATE INDEX forum_posts_author_idx ON forum_posts (author_id, created_at DESC);
CREATE INDEX forum_posts_search_idx ON forum_posts USING gin (search);

CREATE TABLE forum_comments (
    id          uuid PRIMARY KEY,
    post_id     uuid NOT NULL REFERENCES forum_posts (id),
    parent_id   uuid REFERENCES forum_comments (id),
    depth       int NOT NULL CHECK (depth BETWEEN 0 AND 8),
    author_id   uuid NOT NULL REFERENCES users (id),
    body        text NOT NULL,
    labels      text[] NOT NULL DEFAULT '{}',
    mod_reasons text[] NOT NULL DEFAULT '{}',
    status      text NOT NULL CHECK (status IN ('visible', 'held', 'removed', 'deleted')),
    score       int NOT NULL DEFAULT 0,
    ups         int NOT NULL DEFAULT 0,
    downs       int NOT NULL DEFAULT 0,
    notified    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    edited_at   timestamptz,
    removed_at  timestamptz
);
CREATE INDEX forum_comments_post_idx ON forum_comments (post_id, created_at, id);
CREATE INDEX forum_comments_author_idx ON forum_comments (author_id, created_at DESC);

-- One vote per person per post or comment. author_id is who received it
-- (karma); counts is false for votes by accounts younger than
-- FORUM_KARMA_MIN_AGE.
CREATE TABLE forum_votes (
    user_id     uuid NOT NULL REFERENCES users (id),
    target_kind text NOT NULL CHECK (target_kind IN ('post', 'comment')),
    target_id   uuid NOT NULL,
    author_id   uuid NOT NULL REFERENCES users (id),
    value       smallint NOT NULL CHECK (value IN (-1, 1)),
    counts      boolean NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_kind, target_id)
);
CREATE INDEX forum_votes_author_idx ON forum_votes (author_id, created_at) WHERE counts;

CREATE TABLE forum_mentions (
    target_kind text NOT NULL CHECK (target_kind IN ('post', 'comment')),
    target_id   uuid NOT NULL,
    user_id     uuid NOT NULL REFERENCES users (id),
    PRIMARY KEY (target_kind, target_id, user_id)
);
CREATE INDEX forum_mentions_user_idx ON forum_mentions (user_id);
