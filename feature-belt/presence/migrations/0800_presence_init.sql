-- presence: each device's heartbeat (online / away / offline, computed from
-- the database so every API instance agrees), each person's custom status,
-- Do Not Disturb and "appear away", and the state last announced so a change
-- or an expiry is published once.

CREATE TABLE presence_devices (
    user_id      uuid NOT NULL REFERENCES users (id),
    -- Client-generated per browser tab or app install; never shown.
    device_id    text NOT NULL CHECK (char_length(device_id) BETWEEN 8 AND 64),
    platform     text NOT NULL DEFAULT 'web' CHECK (platform IN ('web', 'ios', 'android')),
    active       boolean NOT NULL DEFAULT true,          -- false: the device reports its person idle
    last_seen_at timestamptz NOT NULL DEFAULT now(),     -- last heartbeat
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, device_id)
);
CREATE INDEX presence_devices_seen_idx ON presence_devices (last_seen_at);

CREATE TABLE presence_status (
    user_id      uuid PRIMARY KEY REFERENCES users (id),
    emoji        text NOT NULL DEFAULT '' CHECK (char_length(emoji) <= 16),
    text         text NOT NULL DEFAULT '' CHECK (char_length(text) <= 100),
    clear_after  text NOT NULL DEFAULT 'never'
                 CHECK (clear_after IN ('30m', '1h', '4h', 'today', 'week', 'never')),
    expires_at   timestamptz,                            -- the custom status clears then; NULL = never
    dnd_until    timestamptz,                            -- Do Not Disturb until; NULL = off
    away         boolean NOT NULL DEFAULT false,         -- "appear away" while connected
    -- What was last published for this person, so the sweeper announces a
    -- change (going offline, a status expiring) exactly once.
    announced    text NOT NULL DEFAULT 'offline' CHECK (announced IN ('online', 'away', 'offline')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX presence_status_expires_idx ON presence_status (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX presence_status_dnd_idx ON presence_status (dnd_until) WHERE dnd_until IS NOT NULL;
CREATE INDEX presence_status_live_idx ON presence_status (user_id) WHERE announced <> 'offline';
