-- events: events, co-hosts, invites, RSVPs with a waitlist, reminder
-- bookkeeping, personal calendar feeds and an outbox for notifications
-- queued inside other modules' transactions.

CREATE TABLE events (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id       uuid NOT NULL REFERENCES users (id),       -- the creator
    title         text NOT NULL,
    details       text NOT NULL DEFAULT '',
    starts_at     timestamptz NOT NULL,
    ends_at       timestamptz NOT NULL,
    timezone      text NOT NULL DEFAULT 'UTC',               -- IANA zone for display
    location_text text NOT NULL DEFAULT '',
    lat           double precision,
    lng           double precision,
    online_url    text,
    cover_key     text,                                      -- storage key, purpose event_cover
    capacity      int CHECK (capacity IS NULL OR capacity > 0),
    visibility    text NOT NULL DEFAULT 'members' CHECK (visibility IN ('public', 'members', 'invite')),
    status        text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'cancelled')),
    invite_code   text UNIQUE,                               -- invite link; hosts rotate or clear it
    sequence      int NOT NULL DEFAULT 0,                    -- iCalendar SEQUENCE, bumped on every change
    cancelled_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    CHECK ((lat IS NULL) = (lng IS NULL))
);
CREATE INDEX events_starts_idx ON events (starts_at, id);
CREATE INDEX events_public_idx ON events (starts_at, id) WHERE visibility = 'public' AND status = 'scheduled';
CREATE INDEX events_host_idx ON events (host_id, starts_at);
CREATE INDEX events_reminder_idx ON events (starts_at) WHERE status = 'scheduled';
CREATE INDEX events_geo_idx ON events (lat, lng) WHERE lat IS NOT NULL;

CREATE TABLE event_cohosts (
    event_id   uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    added_by   uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id)
);
CREATE INDEX event_cohosts_user_idx ON event_cohosts (user_id);

-- Who may see an invite-only event (besides hosts and anyone with an RSVP).
CREATE TABLE event_invites (
    event_id   uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    invited_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id)
);
CREATE INDEX event_invites_user_idx ON event_invites (user_id);
CREATE INDEX event_invites_list_idx ON event_invites (event_id, created_at, user_id);

-- One RSVP per user per event. waitlisted is set by the server when going
-- finds the event full; waitlisted_at orders the queue.
CREATE TABLE event_rsvps (
    event_id      uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id       uuid NOT NULL REFERENCES users (id),
    status        text NOT NULL CHECK (status IN ('going', 'maybe', 'declined', 'waitlisted')),
    waitlisted_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id),
    CHECK ((status = 'waitlisted') = (waitlisted_at IS NOT NULL))
);
CREATE INDEX event_rsvps_user_idx ON event_rsvps (user_id, status);
CREATE INDEX event_rsvps_list_idx ON event_rsvps (event_id, status, created_at, user_id);
CREATE INDEX event_rsvps_waitlist_idx ON event_rsvps (event_id, waitlisted_at, user_id) WHERE status = 'waitlisted';

-- Reminders already sent: one row per user per event per offset.
CREATE TABLE event_reminders_sent (
    event_id       uuid NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    user_id        uuid NOT NULL REFERENCES users (id),
    offset_minutes int NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, user_id, offset_minutes)
);
CREATE INDEX event_reminders_sent_user_idx ON event_reminders_sent (user_id);

-- Personal calendar feed: only the sha256 of the secret URL token is kept.
CREATE TABLE event_calendar_feeds (
    user_id    uuid PRIMARY KEY REFERENCES users (id),
    token_hash text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Notifications queued inside a transaction (account deletion), sent by the
-- events.outbox job once it has committed.
CREATE TABLE event_outbox (
    id         bigserial PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id),
    kind       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL DEFAULT '',
    link       text NOT NULL DEFAULT '',
    data       jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_outbox_user_idx ON event_outbox (user_id);
