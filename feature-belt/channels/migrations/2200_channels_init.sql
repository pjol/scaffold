-- channels: org workspaces. Public, private and forum channels, direct and
-- group messages, threads, mentions, reactions, pins, saved items, per-person
-- read pointers and notification preferences, attachments, and the queue of
-- storage objects left behind by deleted rows. Everything belongs to an org
-- and goes with it.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE channels (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN ('public', 'private', 'dm', 'group_dm')),
    layout          text NOT NULL DEFAULT 'chat' CHECK (layout IN ('chat', 'forum')),
    -- public/private: the handle people type after # (lowercase letters,
    -- digits, - and _). Unique per org, archived ones included.
    name            text CHECK (name ~ '^[a-z0-9][a-z0-9_-]{0,79}$'),
    topic           text NOT NULL DEFAULT '' CHECK (char_length(topic) <= 250),
    description     text NOT NULL DEFAULT '' CHECK (char_length(description) <= 1000),
    forum_tags      text[] NOT NULL DEFAULT '{}',                -- forum: tags posts can carry
    is_default      boolean NOT NULL DEFAULT false,              -- every org member joins it once
    -- dm/group_dm: the members' ids sorted and joined, so a set of people has
    -- one conversation per org.
    dm_key          text,
    created_by      uuid REFERENCES users (id),
    archived_at     timestamptz,                                 -- read-only while set
    archived_by     uuid REFERENCES users (id),
    last_message_at timestamptz NOT NULL DEFAULT now(),          -- only moves forward
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT channels_shape_check CHECK (
        (kind IN ('public', 'private') AND name IS NOT NULL AND dm_key IS NULL)
        OR (kind IN ('dm', 'group_dm') AND name IS NULL AND dm_key IS NOT NULL AND layout = 'chat' AND NOT is_default)),
    CONSTRAINT channels_default_check CHECK (NOT is_default OR kind = 'public')
);
CREATE UNIQUE INDEX channels_name_uidx ON channels (org_id, name) WHERE name IS NOT NULL;
CREATE UNIQUE INDEX channels_dm_uidx ON channels (org_id, dm_key) WHERE dm_key IS NOT NULL;
CREATE INDEX channels_org_idx ON channels (org_id, kind, name);
CREATE INDEX channels_default_idx ON channels (org_id) WHERE is_default AND archived_at IS NULL;
CREATE INDEX channels_name_trgm_idx ON channels USING gin (name gin_trgm_ops) WHERE name IS NOT NULL;
CREATE INDEX channels_created_by_idx ON channels (created_by);

CREATE TABLE channel_members (
    channel_id           uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id              uuid NOT NULL REFERENCES users (id),
    role                 text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')), -- admin: manages this channel
    added_by             uuid REFERENCES users (id),
    -- Read pointer on the channel timeline (thread replies have their own,
    -- in channel_thread_follows), with its created_at for keyset compares.
    last_read_message_id uuid,
    last_read_at         timestamptz,
    notify               text NOT NULL DEFAULT 'default' CHECK (notify IN ('default', 'all', 'mentions', 'nothing')),
    muted                boolean NOT NULL DEFAULT false,
    starred_at           timestamptz,
    hidden_at            timestamptz,                            -- a DM closed from the sidebar; back with the next message
    joined_at            timestamptz NOT NULL DEFAULT now(),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX channel_members_user_idx ON channel_members (user_id, channel_id);

-- Default channels are joined once per person: leaving one sticks.
CREATE TABLE channel_autojoins (
    channel_id uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX channel_autojoins_user_idx ON channel_autojoins (user_id);

CREATE TABLE channel_messages (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id       uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    sender_id        uuid REFERENCES users (id),                 -- NULL only for bot messages (posted by the app)
    kind             text NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'system', 'bot')),
    -- text/bot: markdown with mention tokens (<@uuid>, <@here>, <@channel>,
    -- <@group:id|handle>); '' once deleted. system: the event (created,
    -- joined, left, added, removed, renamed, archived, unarchived, topic,
    -- converted); clients word it with current names.
    body             text NOT NULL DEFAULT '',
    bot_name         text CHECK (char_length(bot_name) BETWEEN 1 AND 80),
    subject_user_id  uuid REFERENCES users (id),                 -- system: who the event is about
    thread_root_id   uuid REFERENCES channel_messages (id) ON DELETE CASCADE,
    also_in_channel  boolean NOT NULL DEFAULT false,             -- a thread reply also shown in the channel
    title            text CHECK (char_length(title) BETWEEN 1 AND 200), -- forum posts
    tags             text[] NOT NULL DEFAULT '{}',               -- forum posts, from the channel's forum_tags
    mention_channel  boolean NOT NULL DEFAULT false,             -- @channel: every member is mentioned
    -- Thread roots: replies and the latest one; activity_at orders forum
    -- topic lists (latest reply, else the post itself).
    reply_count      int NOT NULL DEFAULT 0,
    last_reply_at    timestamptz,
    activity_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    pinned_at        timestamptz,
    pinned_by        uuid REFERENCES users (id),
    client_id        text CHECK (char_length(client_id) BETWEEN 1 AND 64), -- makes sends idempotent
    edited_at        timestamptz,
    deleted_at       timestamptz,                                -- tombstone: body and attachments are gone
    deleted_by       text CHECK (deleted_by IN ('sender', 'moderator', 'staff', 'account_deleted')), -- moderator: an org or channel admin
    -- clock_timestamp: rows written in one transaction keep their order.
    created_at       timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT channel_messages_deleted_check CHECK ((deleted_at IS NULL) = (deleted_by IS NULL)),
    CONSTRAINT channel_messages_sender_check CHECK ((kind = 'bot') = (sender_id IS NULL) AND (kind = 'bot') = (bot_name IS NOT NULL)),
    CONSTRAINT channel_messages_thread_check CHECK (thread_root_id IS NULL OR (title IS NULL AND kind <> 'system'))
);
CREATE INDEX channel_messages_timeline_idx ON channel_messages (channel_id, created_at DESC, id DESC)
    WHERE thread_root_id IS NULL OR also_in_channel;
CREATE INDEX channel_messages_thread_idx ON channel_messages (thread_root_id, created_at, id) WHERE thread_root_id IS NOT NULL;
CREATE INDEX channel_messages_forum_idx ON channel_messages (channel_id, activity_at DESC, id DESC) WHERE thread_root_id IS NULL;
CREATE INDEX channel_messages_pins_idx ON channel_messages (channel_id, pinned_at DESC) WHERE pinned_at IS NOT NULL;
CREATE UNIQUE INDEX channel_messages_client_uidx ON channel_messages (sender_id, client_id) WHERE client_id IS NOT NULL;
CREATE INDEX channel_messages_sender_idx ON channel_messages (sender_id, created_at DESC);
CREATE INDEX channel_messages_body_trgm_idx ON channel_messages USING gin (body gin_trgm_ops)
    WHERE kind <> 'system' AND deleted_at IS NULL;
CREATE INDEX channel_messages_title_trgm_idx ON channel_messages USING gin (title gin_trgm_ops)
    WHERE title IS NOT NULL AND deleted_at IS NULL;

-- Who a message mentions by name, by @here (members online when it was
-- sent) or through a group. @channel is a flag on the message instead.
CREATE TABLE channel_mentions (
    message_id uuid NOT NULL REFERENCES channel_messages (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    kind       text NOT NULL CHECK (kind IN ('user', 'here', 'group')),
    PRIMARY KEY (message_id, user_id)
);
CREATE INDEX channel_mentions_user_idx ON channel_mentions (user_id, message_id);

CREATE TABLE channel_reactions (
    message_id uuid NOT NULL REFERENCES channel_messages (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    emoji      text NOT NULL CHECK (char_length(emoji) BETWEEN 1 AND 16),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, user_id, emoji)
);
CREATE INDEX channel_reactions_user_idx ON channel_reactions (user_id);

CREATE TABLE channel_attachments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id   uuid NOT NULL REFERENCES channel_messages (id) ON DELETE CASCADE,
    position     smallint NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 9),
    kind         text NOT NULL CHECK (kind IN ('image', 'video', 'audio', 'file')),
    storage_key  text NOT NULL UNIQUE,                           -- purpose channel_attachment (private)
    content_type text NOT NULL,
    file_name    text NOT NULL DEFAULT '' CHECK (char_length(file_name) <= 160),
    size_bytes   bigint CHECK (size_bytes IS NULL OR size_bytes > 0),
    width        int CHECK (width IS NULL OR width > 0),
    height       int CHECK (height IS NULL OR height > 0),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_attachments_message_idx ON channel_attachments (message_id, position);

-- "Later": messages a person saved, open or done.
CREATE TABLE channel_saved (
    user_id      uuid NOT NULL REFERENCES users (id),
    message_id   uuid NOT NULL REFERENCES channel_messages (id) ON DELETE CASCADE,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, message_id)
);
CREATE INDEX channel_saved_user_idx ON channel_saved (user_id, created_at DESC, message_id DESC);

-- Threads a person follows (they posted, were mentioned, or chose to) with
-- their read pointer inside the thread. following = false is an explicit
-- unfollow that later replies don't undo.
CREATE TABLE channel_thread_follows (
    root_id            uuid NOT NULL REFERENCES channel_messages (id) ON DELETE CASCADE,
    user_id            uuid NOT NULL REFERENCES users (id),
    following          boolean NOT NULL DEFAULT true,
    last_read_reply_id uuid,
    last_read_at       timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (root_id, user_id)
);
CREATE INDEX channel_thread_follows_user_idx ON channel_thread_follows (user_id) WHERE following;

-- Storage objects whose rows are gone. Every attachment delete (a message
-- erased, a channel or org deleted by cascade) queues its object through the
-- trigger below; a worker deletes each one and stamps deleted_at. The row
-- stays so a deleted key can't be attached again.
CREATE TABLE channel_deleted_objects (
    storage_key text PRIMARY KEY,
    attempts    int NOT NULL DEFAULT 0,
    last_error  text NOT NULL DEFAULT '',
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_deleted_objects_pending_idx ON channel_deleted_objects (created_at) WHERE deleted_at IS NULL;

CREATE FUNCTION channel_attachments_queue_object() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO channel_deleted_objects (storage_key) VALUES (OLD.storage_key) ON CONFLICT (storage_key) DO NOTHING;
    RETURN OLD;
END;
$$;

CREATE TRIGGER channel_attachments_queue_object_trg
    AFTER DELETE ON channel_attachments FOR EACH ROW EXECUTE FUNCTION channel_attachments_queue_object();
