-- chat: direct and group conversations, members with roles and read state,
-- messages (text, system events, tombstones), attachments, reactions, and the
-- queue of storage objects left behind by deleted rows.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE chat_conversations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            text NOT NULL CHECK (kind IN ('direct', 'group')),
    -- direct: both people, ordered so a pair has one conversation. They stay
    -- after an account deletion so the partner still sees "Deleted user".
    user_low        uuid REFERENCES users (id),
    user_high       uuid REFERENCES users (id),
    title           text NOT NULL DEFAULT '' CHECK (char_length(title) <= 80),
    avatar_key      text,                                   -- groups: storage key, purpose chat_avatar
    created_by      uuid REFERENCES users (id),
    last_message_at timestamptz NOT NULL DEFAULT now(),     -- inbox order; only moves forward
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chat_conversations_shape_check CHECK (
        (kind = 'direct' AND user_low IS NOT NULL AND user_high IS NOT NULL AND user_low < user_high
            AND title = '' AND avatar_key IS NULL)
        OR (kind = 'group' AND user_low IS NULL AND user_high IS NULL))
);
CREATE UNIQUE INDEX chat_conversations_pair_uidx ON chat_conversations (user_low, user_high) WHERE kind = 'direct';
CREATE INDEX chat_conversations_created_by_idx ON chat_conversations (created_by, created_at DESC);

CREATE TABLE chat_members (
    conversation_id      uuid NOT NULL REFERENCES chat_conversations (id) ON DELETE CASCADE,
    user_id              uuid NOT NULL REFERENCES users (id),
    role                 text NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'admin', 'member')),
    added_by             uuid REFERENCES users (id),
    -- Read pointer: the newest message this member has seen, with its
    -- created_at, so unread counts compare (created_at, id) without a join.
    last_read_message_id uuid,
    last_read_at         timestamptz,
    muted_until          timestamptz,                        -- NULL = not muted; 9999-12-31 = until unmuted
    archived_at          timestamptz,                        -- hidden for this member until the next message
    cleared_at           timestamptz,                        -- "delete for me": earlier history is hidden
    left_at              timestamptz,                        -- groups: left or removed; NULL = member
    joined_at            timestamptz NOT NULL DEFAULT now(),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, user_id)
);
CREATE INDEX chat_members_user_idx ON chat_members (user_id) WHERE left_at IS NULL;
CREATE UNIQUE INDEX chat_members_owner_uidx ON chat_members (conversation_id) WHERE role = 'owner' AND left_at IS NULL;

CREATE TABLE chat_messages (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id uuid NOT NULL REFERENCES chat_conversations (id) ON DELETE CASCADE,
    sender_id       uuid NOT NULL REFERENCES users (id),   -- system messages: who acted
    kind            text NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'system')),
    -- text: what was said ('' once deleted). system: the event (created,
    -- member_added, member_removed, member_left, title_changed,
    -- avatar_changed, owner_changed); clients word it with current names.
    body            text NOT NULL DEFAULT '',
    subject_user_id uuid REFERENCES users (id),            -- system: the member the event is about
    reply_to_id     uuid REFERENCES chat_messages (id) ON DELETE SET NULL,
    client_id       text CHECK (char_length(client_id) BETWEEN 1 AND 64), -- client-generated; makes sends idempotent
    edited_at       timestamptz,
    deleted_at      timestamptz,                           -- tombstone: body and attachments are gone
    deleted_by      text CHECK (deleted_by IN ('sender', 'staff', 'account_deleted')),
    -- clock_timestamp: rows written in one transaction (system events) keep their order.
    created_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chat_messages_deleted_check CHECK ((deleted_at IS NULL) = (deleted_by IS NULL))
);
CREATE INDEX chat_messages_conversation_idx ON chat_messages (conversation_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX chat_messages_client_uidx ON chat_messages (sender_id, client_id) WHERE client_id IS NOT NULL;
CREATE INDEX chat_messages_sender_idx ON chat_messages (sender_id, created_at DESC);
CREATE INDEX chat_messages_reply_idx ON chat_messages (reply_to_id) WHERE reply_to_id IS NOT NULL;
CREATE INDEX chat_messages_body_trgm_idx ON chat_messages USING gin (body gin_trgm_ops)
    WHERE kind = 'text' AND deleted_at IS NULL;

CREATE TABLE chat_attachments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id   uuid NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    position     smallint NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 9),
    kind         text NOT NULL CHECK (kind IN ('image', 'video', 'audio', 'file')),
    storage_key  text NOT NULL UNIQUE,                     -- purpose chat_attachment (private)
    content_type text NOT NULL,
    file_name    text NOT NULL DEFAULT '' CHECK (char_length(file_name) <= 160),
    size_bytes   bigint CHECK (size_bytes IS NULL OR size_bytes > 0),
    width        int CHECK (width IS NULL OR width > 0),   -- client-reported, for layout
    height       int CHECK (height IS NULL OR height > 0),
    duration_ms  int CHECK (duration_ms IS NULL OR duration_ms >= 0),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_attachments_message_idx ON chat_attachments (message_id, position);

-- Free-form emoji, one row per person and emoji.
CREATE TABLE chat_reactions (
    message_id uuid NOT NULL REFERENCES chat_messages (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    emoji      text NOT NULL CHECK (char_length(emoji) BETWEEN 1 AND 16),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, user_id, emoji)
);
CREATE INDEX chat_reactions_user_idx ON chat_reactions (user_id);

-- Storage objects whose rows are gone (deleted messages, replaced group
-- pictures, deleted conversations). Written in the same transaction as the
-- delete; a worker removes each object and stamps deleted_at. The row stays
-- so a deleted key can't be attached again.
CREATE TABLE chat_deleted_objects (
    storage_key text PRIMARY KEY,
    attempts    int NOT NULL DEFAULT 0,
    last_error  text NOT NULL DEFAULT '',
    deleted_at  timestamptz,                               -- NULL = still to delete
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX chat_deleted_objects_pending_idx ON chat_deleted_objects (created_at) WHERE deleted_at IS NULL;
