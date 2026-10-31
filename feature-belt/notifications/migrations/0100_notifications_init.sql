-- notifications: in-app inbox, push devices, per-kind channel preferences,
-- and Expo push tickets awaiting a receipt check.

-- One row per notification a user received. Inbox delivery is always on;
-- read rows older than NOTIFICATIONS_RETENTION are pruned.
CREATE TABLE notifications (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    kind       text NOT NULL,                               -- dotted id: chat.message, support.reply
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    link       text NOT NULL DEFAULT '',                    -- app path, e.g. /chat/<id>
    data       jsonb NOT NULL DEFAULT '{}',
    read_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC, id DESC);
CREATE INDEX notifications_unread_idx ON notifications (user_id) WHERE read_at IS NULL;
CREATE INDEX notifications_prune_idx ON notifications (created_at) WHERE read_at IS NOT NULL;
CREATE INDEX notifications_kind_idx ON notifications (kind, created_at DESC);

-- Expo push tokens. A token belongs to one user at a time: registering it
-- again from another account moves it. Removed on sign-out, on account
-- deletion and when Expo reports DeviceNotRegistered.
CREATE TABLE notification_devices (
    token        text PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id),
    platform     text NOT NULL CHECK (platform IN ('ios', 'android', 'web')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_devices_user_idx ON notification_devices (user_id, last_seen_at DESC);

-- A user's saved choice per kind; kinds without a row use the defaults the
-- owning module registered (notifications.RegisterKind).
CREATE TABLE notification_preferences (
    user_id    uuid NOT NULL REFERENCES users (id),
    kind       text NOT NULL,
    push       boolean NOT NULL,
    email      boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind)
);

-- Expo accepted these messages; the receipts job asks Expo how each ended
-- (after ~15 minutes) and prunes tokens reported as DeviceNotRegistered.
CREATE TABLE notification_push_tickets (
    ticket_id  text PRIMARY KEY,
    token      text NOT NULL,
    user_id    uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_push_tickets_created_idx ON notification_push_tickets (created_at);
CREATE INDEX notification_push_tickets_user_idx ON notification_push_tickets (user_id);
