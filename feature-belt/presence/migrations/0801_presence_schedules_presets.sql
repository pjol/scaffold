-- presence 1.1: Do Not Disturb schedules (quiet hours per weekday in the
-- person's zone) and each person's own status presets.

CREATE TABLE presence_dnd_schedules (
    user_id       uuid PRIMARY KEY REFERENCES users (id),
    enabled       boolean NOT NULL DEFAULT true,
    time_zone     text NOT NULL CHECK (char_length(time_zone) BETWEEN 1 AND 64),   -- IANA zone the hours are read in
    -- Seven entries, Sunday first: null (no quiet hours that day) or
    -- {"start":"22:00","end":"08:00"}; an end at or before the start runs
    -- into the next day, "24:00" is midnight at the end of the day.
    days          jsonb NOT NULL,
    -- "Resume notifications" during quiet hours skips them until this time
    -- (the end of that stretch); NULL = nothing skipped.
    skip_until    timestamptz,
    -- The end of the quiet hours last announced (NULL = outside them), so the
    -- sweeper publishes each start and end once, and when to look again.
    announced_end timestamptz,
    next_check_at timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX presence_dnd_schedules_due_idx ON presence_dnd_schedules (next_check_at) WHERE enabled;

CREATE TABLE presence_status_presets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id),
    emoji       text NOT NULL DEFAULT '' CHECK (char_length(emoji) <= 16),
    text        text NOT NULL DEFAULT '' CHECK (char_length(text) <= 100),
    clear_after text NOT NULL DEFAULT 'never'
                CHECK (clear_after IN ('30m', '1h', '4h', 'today', 'week', 'never')),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX presence_status_presets_user_idx ON presence_status_presets (user_id, created_at);
