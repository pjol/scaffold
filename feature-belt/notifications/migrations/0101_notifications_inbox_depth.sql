-- notifications 1.2: snooze, grouping by resource, mutes from the inbox and
-- an email digest of unread notifications.

-- sort_at orders the inbox: created_at, moved to the wake-up time when a
-- snoozed notification comes back (so it surfaces at the top).
ALTER TABLE notifications ADD COLUMN sort_at timestamptz;
UPDATE notifications SET sort_at = created_at;
ALTER TABLE notifications ALTER COLUMN sort_at SET NOT NULL, ALTER COLUMN sort_at SET DEFAULT now();
-- Hidden from the inbox (and the unread count) until then; NULL = not snoozed.
ALTER TABLE notifications ADD COLUMN snoozed_until timestamptz;
-- What it is about, "<kind>:<id>" (a channel, a thread, a page), for grouping
-- and muting; '' when the kind names no resource.
ALTER TABLE notifications ADD COLUMN resource text NOT NULL DEFAULT '';

CREATE INDEX notifications_inbox_idx ON notifications (user_id, sort_at DESC, id DESC);
CREATE INDEX notifications_snoozed_idx ON notifications (snoozed_until) WHERE snoozed_until IS NOT NULL;
CREATE INDEX notifications_resource_idx ON notifications (user_id, resource) WHERE resource <> '';

-- Resources a person muted from the inbox (for kinds whose feature allows
-- it): new notifications about them are dropped.
CREATE TABLE notification_mutes (
    user_id    uuid NOT NULL REFERENCES users (id),
    resource   text NOT NULL CHECK (char_length(resource) BETWEEN 3 AND 200),
    label      text NOT NULL DEFAULT '' CHECK (char_length(label) <= 200),  -- what the person saw ("#general")
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, resource)
);

-- Opt-in email digest of unread notifications.
CREATE TABLE notification_digests (
    user_id      uuid PRIMARY KEY REFERENCES users (id),
    cadence      text NOT NULL CHECK (cadence IN ('daily', 'weekly')),
    hour         smallint NOT NULL CHECK (hour BETWEEN 0 AND 23),          -- local hour it goes out
    weekday      smallint NOT NULL DEFAULT 1 CHECK (weekday BETWEEN 0 AND 6), -- weekly: 0 = Sunday
    time_zone    text NOT NULL CHECK (char_length(time_zone) BETWEEN 1 AND 64),
    last_sent_at timestamptz,
    next_at      timestamptz NOT NULL,                                     -- the next slot (or a retry while held)
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notification_digests_due_idx ON notification_digests (next_at);
