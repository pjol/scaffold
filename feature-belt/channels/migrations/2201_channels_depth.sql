-- channels 1.2: server drafts, scheduled messages, reminders, link previews,
-- custom emoji, polls, sidebar sections and per-person workspace preferences
-- (sort, collapsed sections, keywords, skin tone, profile title and zone),
-- bookmarks, announcement-only posting, forwards, incoming webhooks and
-- retention. Everything still belongs to an org and goes with it.

-- One row per org once anyone changes a setting; no row = the defaults.
CREATE TABLE channel_workspace_settings (
    org_id         uuid PRIMARY KEY REFERENCES orgs (id) ON DELETE CASCADE,
    unfurl_links   boolean NOT NULL DEFAULT true,                 -- previews of outside links
    retention_days int CHECK (retention_days IS NULL OR retention_days BETWEEN 1 AND 36500), -- NULL = keep forever
    updated_by     uuid REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE channels
    ADD COLUMN posting        text NOT NULL DEFAULT 'everyone' CHECK (posting IN ('everyone', 'admins')),
    ADD COLUMN replies_open   boolean NOT NULL DEFAULT true,      -- posting = admins: members may still reply in threads
    ADD COLUMN retention_days int CHECK (retention_days IS NULL OR retention_days BETWEEN 1 AND 36500); -- overrides the workspace's

-- Incoming webhooks: an integration posts into one channel through a secret
-- URL. Only the token's hash is stored.
CREATE TABLE channel_webhooks (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id   uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    token_hash   bytea NOT NULL UNIQUE,
    created_by   uuid REFERENCES users (id),
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_webhooks_channel_idx ON channel_webhooks (channel_id, created_at);

ALTER TABLE channel_messages
    -- A forward shows the original live, checked against each viewer.
    -- is_forward stays true when the original is gone (retention, deletion).
    ADD COLUMN forward_of   uuid REFERENCES channel_messages (id) ON DELETE SET NULL,
    ADD COLUMN is_forward   boolean NOT NULL DEFAULT false,
    ADD COLUMN webhook_id   uuid REFERENCES channel_webhooks (id) ON DELETE SET NULL,
    ADD COLUMN links_hidden boolean NOT NULL DEFAULT false;       -- the author removed the link previews
CREATE INDEX channel_messages_forward_idx ON channel_messages (forward_of) WHERE forward_of IS NOT NULL;
CREATE INDEX channel_messages_retention_idx ON channel_messages (channel_id, created_at);

-- Unsent text per conversation and thread, kept on the server so it follows
-- the person between devices. Body carries mention tokens.
CREATE TABLE channel_drafts (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id),
    channel_id     uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    thread_root_id uuid REFERENCES channel_messages (id) ON DELETE CASCADE,
    body           text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 40000),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX channel_drafts_uidx ON channel_drafts (user_id, channel_id, COALESCE(thread_root_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX channel_drafts_user_idx ON channel_drafts (user_id, updated_at DESC, id DESC);

-- Messages to send later. A sent one is deleted (the message exists); a
-- failed one stays with its error until the person edits or deletes it.
CREATE TABLE channel_scheduled (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id),
    channel_id      uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    thread_root_id  uuid REFERENCES channel_messages (id) ON DELETE CASCADE,
    also_in_channel boolean NOT NULL DEFAULT false,
    body            text NOT NULL DEFAULT '',
    attachments     jsonb NOT NULL DEFAULT '[]',                  -- uploads to attach when it sends
    send_at         timestamptz NOT NULL,
    status          text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'failed')),
    error           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_scheduled_due_idx ON channel_scheduled (send_at) WHERE status = 'scheduled';
CREATE INDEX channel_scheduled_user_idx ON channel_scheduled (user_id, send_at, id);

-- Reminders: about a message (it also goes to Later) or free text (/remind).
CREATE TABLE channel_reminders (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    message_id   uuid REFERENCES channel_messages (id) ON DELETE CASCADE,
    text         text NOT NULL DEFAULT '' CHECK (char_length(text) <= 1000),
    remind_at    timestamptz NOT NULL,
    fired_at     timestamptz,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT channel_reminders_what_check CHECK (message_id IS NOT NULL OR text <> '')
);
CREATE INDEX channel_reminders_due_idx ON channel_reminders (remind_at) WHERE fired_at IS NULL;
CREATE INDEX channel_reminders_user_idx ON channel_reminders (user_id, org_id, remind_at, id);
CREATE INDEX channel_reminders_message_idx ON channel_reminders (message_id) WHERE message_id IS NOT NULL;

-- Link previews of outside URLs, shared by every message that links them.
-- Images and icons are fetched once and stored (readers never contact the
-- outside host). status none = the page had nothing to show.
CREATE TABLE channel_link_previews (
    url_hash     text PRIMARY KEY,                                -- sha256 of the normalised URL
    url          text NOT NULL,
    status       text NOT NULL CHECK (status IN ('ok', 'none', 'error')),
    title        text NOT NULL DEFAULT '',
    description  text NOT NULL DEFAULT '',
    site_name    text NOT NULL DEFAULT '',
    image_key    text,
    image_width  int,
    image_height int,
    icon_key     text,
    fetched_at   timestamptz NOT NULL DEFAULT now(),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_link_previews_fetched_idx ON channel_link_previews (fetched_at);

CREATE TABLE channel_message_links (
    message_id uuid NOT NULL REFERENCES channel_messages (id) ON DELETE CASCADE,
    position   smallint NOT NULL CHECK (position BETWEEN 0 AND 9),
    url        text NOT NULL,
    url_hash   text NOT NULL,
    PRIMARY KEY (message_id, position)
);
CREATE INDEX channel_message_links_hash_idx ON channel_message_links (url_hash);

-- Reactions may be custom emoji, stored as :name:.
ALTER TABLE channel_reactions DROP CONSTRAINT channel_reactions_emoji_check,
    ADD CONSTRAINT channel_reactions_emoji_check CHECK (char_length(emoji) BETWEEN 1 AND 44);

-- Custom emoji per org; an alias points at another emoji and has no image.
CREATE TABLE channel_emoji (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (name ~ '^[a-z0-9_+-]{2,40}$'),
    alias_of     uuid REFERENCES channel_emoji (id) ON DELETE CASCADE,
    storage_key  text UNIQUE,                                     -- purpose channel_emoji (private)
    content_type text,
    created_by   uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT channel_emoji_shape_check CHECK ((alias_of IS NULL) = (storage_key IS NOT NULL))
);
CREATE UNIQUE INDEX channel_emoji_name_uidx ON channel_emoji (org_id, name);
CREATE INDEX channel_emoji_created_by_idx ON channel_emoji (created_by);

-- Polls: a message with a question and options.
CREATE TABLE channel_polls (
    message_id uuid PRIMARY KEY REFERENCES channel_messages (id) ON DELETE CASCADE,
    question   text NOT NULL CHECK (char_length(question) BETWEEN 1 AND 300),
    multiple   boolean NOT NULL DEFAULT false,
    anonymous  boolean NOT NULL DEFAULT false,               -- votes counted, voters never shown
    closed_at  timestamptz,
    closed_by  uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE channel_poll_options (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    message_id uuid NOT NULL REFERENCES channel_polls (message_id) ON DELETE CASCADE,
    position   smallint NOT NULL CHECK (position BETWEEN 0 AND 9),
    text       text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 200),
    UNIQUE (message_id, position)
);
CREATE TABLE channel_poll_votes (
    option_id  uuid NOT NULL REFERENCES channel_poll_options (id) ON DELETE CASCADE,
    message_id uuid NOT NULL REFERENCES channel_polls (message_id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (option_id, user_id)
);
CREATE INDEX channel_poll_votes_message_idx ON channel_poll_votes (message_id, user_id);
CREATE INDEX channel_poll_votes_user_idx ON channel_poll_votes (user_id);

-- Custom sidebar sections, per person per org.
CREATE TABLE channel_sidebar_sections (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name       text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
    position   int NOT NULL DEFAULT 0,
    collapsed  boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_sidebar_sections_user_idx ON channel_sidebar_sections (user_id, org_id, position);

ALTER TABLE channel_members ADD COLUMN section_id uuid REFERENCES channel_sidebar_sections (id) ON DELETE SET NULL;
CREATE INDEX channel_members_section_idx ON channel_members (section_id) WHERE section_id IS NOT NULL;

-- A person's preferences in one workspace.
CREATE TABLE channel_user_prefs (
    user_id          uuid NOT NULL REFERENCES users (id),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    sidebar_sort     text NOT NULL DEFAULT 'alpha' CHECK (sidebar_sort IN ('alpha', 'recent')),
    collapsed        text[] NOT NULL DEFAULT '{}',               -- built-in sections: starred, channels, direct
    keywords         text[] NOT NULL DEFAULT '{}',               -- words that notify like a mention
    skin_tone        smallint NOT NULL DEFAULT 0 CHECK (skin_tone BETWEEN 0 AND 5),
    activity_read_at timestamptz,
    title            text NOT NULL DEFAULT '' CHECK (char_length(title) <= 80), -- profile card
    timezone         text NOT NULL DEFAULT '' CHECK (char_length(timezone) <= 64), -- IANA, reported by the person's client
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, org_id)
);
CREATE INDEX channel_user_prefs_org_idx ON channel_user_prefs (org_id) WHERE keywords <> '{}';

-- Links pinned above a channel's timeline.
CREATE TABLE channel_bookmarks (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id uuid NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    title      text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    url        text NOT NULL CHECK (char_length(url) BETWEEN 1 AND 2000),
    position   int NOT NULL DEFAULT 0,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX channel_bookmarks_channel_idx ON channel_bookmarks (channel_id, position, created_at);

-- Stored objects of deleted emoji and replaced previews go through the same
-- clean-up queue as attachments.
CREATE FUNCTION channel_emoji_queue_object() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.storage_key IS NOT NULL THEN
        INSERT INTO channel_deleted_objects (storage_key) VALUES (OLD.storage_key) ON CONFLICT (storage_key) DO NOTHING;
    END IF;
    RETURN OLD;
END;
$$;
CREATE TRIGGER channel_emoji_queue_object_trg
    AFTER DELETE ON channel_emoji FOR EACH ROW EXECUTE FUNCTION channel_emoji_queue_object();

CREATE FUNCTION channel_link_previews_queue_objects() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' OR OLD.image_key IS DISTINCT FROM NEW.image_key THEN
        IF OLD.image_key IS NOT NULL THEN
            INSERT INTO channel_deleted_objects (storage_key) VALUES (OLD.image_key) ON CONFLICT (storage_key) DO NOTHING;
        END IF;
    END IF;
    IF TG_OP = 'DELETE' OR OLD.icon_key IS DISTINCT FROM NEW.icon_key THEN
        IF OLD.icon_key IS NOT NULL THEN
            INSERT INTO channel_deleted_objects (storage_key) VALUES (OLD.icon_key) ON CONFLICT (storage_key) DO NOTHING;
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER channel_link_previews_queue_objects_trg
    AFTER UPDATE OR DELETE ON channel_link_previews FOR EACH ROW EXECUTE FUNCTION channel_link_previews_queue_objects();
