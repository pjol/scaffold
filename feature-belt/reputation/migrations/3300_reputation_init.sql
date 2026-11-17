-- Reputation: a transparent 0–100 score per person from a series the app
-- supplies. Snapshots of each person's cumulative counters (14-day
-- retention) roll up into day rows; apps may also push day rows. Scores,
-- components, requirements and stats are stored per person; integrity flags
-- put a person under review until staff decide.

CREATE TABLE reputation_snapshots (
    user_id   uuid NOT NULL REFERENCES users (id),
    at        timestamptz NOT NULL,
    value     numeric NOT NULL,              -- what the person holds (account value)
    net_flows numeric NOT NULL,              -- cumulative money in minus out: never a return
    activity  bigint NOT NULL,               -- cumulative
    volume    numeric NOT NULL,              -- cumulative
    incidents bigint NOT NULL,               -- cumulative (liquidations)
    exposed   boolean NOT NULL,              -- open exposure at that moment
    PRIMARY KEY (user_id, at)
);
CREATE INDEX reputation_snapshots_at_idx ON reputation_snapshots (at);

-- One row per person per UTC day. The day's return is
-- (close_value − open_value − flows) / max(open_value, REPUTATION_RETURN_FLOOR),
-- computed when scoring.
CREATE TABLE reputation_days (
    user_id     uuid NOT NULL REFERENCES users (id),
    day         date NOT NULL,
    open_value  numeric NOT NULL,
    close_value numeric NOT NULL,
    flows       numeric NOT NULL,
    active      boolean NOT NULL,            -- activity, or open exposure, that day
    activity    integer NOT NULL CHECK (activity >= 0),
    volume      numeric NOT NULL CHECK (volume >= 0),
    incidents   integer NOT NULL CHECK (incidents >= 0),
    source      text NOT NULL CHECK (source IN ('snapshots', 'pushed')),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day)
);
CREATE INDEX reputation_days_day_idx ON reputation_days (day);
CREATE INDEX reputation_days_updated_idx ON reputation_days (updated_at);

CREATE TABLE reputation_scores (
    user_id           uuid PRIMARY KEY REFERENCES users (id),
    version           text NOT NULL DEFAULT '',
    status            text NOT NULL DEFAULT 'unrated' CHECK (status IN ('rated', 'unrated', 'under_review')),
    score             integer CHECK (score BETWEEN 0 AND 100),
    tier              text,
    components        jsonb NOT NULL DEFAULT '[]',
    requirements      jsonb NOT NULL DEFAULT '[]',
    stats             jsonb NOT NULL DEFAULT '{}',
    first_activity_at timestamptz,
    computed_at       timestamptz              -- null: not computed yet
);
CREATE INDEX reputation_scores_rated_idx ON reputation_scores (score DESC) WHERE status = 'rated';
CREATE INDEX reputation_scores_computed_idx ON reputation_scores (computed_at);

CREATE TABLE reputation_flags (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id),
    kind            text NOT NULL,           -- wash_pair | vote_ring | a module's own
    counterparty_id uuid REFERENCES users (id),
    metrics         jsonb NOT NULL DEFAULT '{}',
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'cleared', 'confirmed')),
    note            text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    decided_at      timestamptz,
    decided_by      uuid REFERENCES users (id)
);
CREATE UNIQUE INDEX reputation_flags_open_uidx ON reputation_flags
    (user_id, kind, COALESCE(counterparty_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE status = 'open';
CREATE INDEX reputation_flags_open_idx ON reputation_flags (created_at, id) WHERE status = 'open';
CREATE INDEX reputation_flags_decided_idx ON reputation_flags (status, decided_at DESC, id DESC);
CREATE INDEX reputation_flags_user_idx ON reputation_flags (user_id, status);
