-- feed: posts and announcements, media, one level of comments, reactions,
-- mentions, the per-user seen marker and the media cleanup queue.

CREATE TABLE feed_posts (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_id    uuid NOT NULL REFERENCES users (id),
    kind         text NOT NULL DEFAULT 'post' CHECK (kind IN ('post', 'announcement')),
    audience     text NOT NULL DEFAULT 'all',              -- "all" or "<kind>:<uuid>" (feed.Scope)
    body         text NOT NULL DEFAULT '',
    link_url     text,
    pinned_until timestamptz,                              -- announcements: shown on top until then
    edited_at    timestamptz,
    removed_at   timestamptz,                              -- staff removal; hidden from everyone
    removed_by   uuid REFERENCES users (id),
    search       tsvector GENERATED ALWAYS AS (to_tsvector('simple', body)) STORED,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (pinned_until IS NULL OR kind = 'announcement')
);
CREATE INDEX feed_posts_feed_idx ON feed_posts (audience, created_at DESC, id DESC) WHERE removed_at IS NULL;
CREATE INDEX feed_posts_author_idx ON feed_posts (author_id, created_at DESC, id DESC);
CREATE INDEX feed_posts_pinned_idx ON feed_posts (audience, created_at DESC, id DESC)
    WHERE kind = 'announcement' AND pinned_until IS NOT NULL AND removed_at IS NULL;
CREATE INDEX feed_posts_created_idx ON feed_posts (created_at DESC, id DESC);
CREATE INDEX feed_posts_search_idx ON feed_posts USING gin (search);

-- Images and video. Keys are unique so one upload can't back two posts
-- (deleting one would otherwise break the other).
CREATE TABLE feed_media (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id      uuid NOT NULL REFERENCES feed_posts (id) ON DELETE CASCADE,
    position     int NOT NULL,
    key          text NOT NULL UNIQUE,                     -- feed_media/<owner>/<uuid>.<ext>
    kind         text NOT NULL CHECK (kind IN ('image', 'video')),
    content_type text NOT NULL,
    width        int,
    height       int,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feed_media_post_idx ON feed_media (post_id, position);

-- Comments; parent_id makes a reply (one level: a parent never has a parent).
CREATE TABLE feed_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id    uuid NOT NULL REFERENCES feed_posts (id) ON DELETE CASCADE,
    parent_id  uuid REFERENCES feed_comments (id) ON DELETE CASCADE,
    author_id  uuid NOT NULL REFERENCES users (id),
    body       text NOT NULL,
    edited_at  timestamptz,
    deleted_at timestamptz,                                -- deleted with replies: placeholder, body cleared
    removed_at timestamptz,                                -- staff removal
    removed_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feed_comments_post_idx ON feed_comments (post_id, created_at, id);
CREATE INDEX feed_comments_parent_idx ON feed_comments (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX feed_comments_author_idx ON feed_comments (author_id, created_at);

-- One reaction per user per target; changing it updates the row.
CREATE TABLE feed_post_reactions (
    post_id    uuid NOT NULL REFERENCES feed_posts (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    reaction   text NOT NULL CHECK (reaction IN ('like', 'love', 'laugh', 'wow', 'sad')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);
CREATE INDEX feed_post_reactions_user_idx ON feed_post_reactions (user_id);

CREATE TABLE feed_comment_reactions (
    comment_id uuid NOT NULL REFERENCES feed_comments (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    reaction   text NOT NULL CHECK (reaction IN ('like', 'love', 'laugh', 'wow', 'sad')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id)
);
CREATE INDEX feed_comment_reactions_user_idx ON feed_comment_reactions (user_id);

-- @mentions in a post (comment_id NULL) or a comment. start_idx/length are
-- UTF-16 offsets of the "@Name" token, matching JavaScript string indexes.
CREATE TABLE feed_mentions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id    uuid NOT NULL REFERENCES feed_posts (id) ON DELETE CASCADE,
    comment_id uuid REFERENCES feed_comments (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    start_idx  int NOT NULL CHECK (start_idx >= 0),
    length     int NOT NULL CHECK (length > 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feed_mentions_post_idx ON feed_mentions (post_id) WHERE comment_id IS NULL;
CREATE INDEX feed_mentions_comment_idx ON feed_mentions (comment_id) WHERE comment_id IS NOT NULL;
CREATE INDEX feed_mentions_user_idx ON feed_mentions (user_id, created_at DESC);

-- Last time the user opened the feed; newer posts make the unread dot.
CREATE TABLE feed_seen (
    user_id    uuid PRIMARY KEY REFERENCES users (id),
    seen_at    timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Storage objects to delete (deleted posts, media dropped in an edit).
-- Filled inside the transaction, emptied by the feed.media_trash job.
CREATE TABLE feed_media_trash (
    key        text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);
