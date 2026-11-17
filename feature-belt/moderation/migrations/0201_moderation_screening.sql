-- moderation 1.1: screening decisions (rules, then an optional decision
-- model), cases screening opens, and strikes.

-- One row per screening of an item (new or edited), including items refused
-- outright by a blocked term (rejected, never stored by their feature).
CREATE TABLE moderation_decisions (
    id            uuid PRIMARY KEY,
    target_kind   text NOT NULL,                       -- registered kind: forum.post
    target_id     text NOT NULL,
    author_id     uuid REFERENCES users (id),          -- NULL once the author deleted their account
    text_hash     text,                                -- repeat rules; NULL for short texts and after deletion
    edit          boolean NOT NULL DEFAULT false,
    rejected      boolean NOT NULL DEFAULT false,
    rules_version text NOT NULL,
    action        text NOT NULL CHECK (action IN ('allow', 'label', 'hold', 'remove')),
    rules_action  text NOT NULL CHECK (rules_action IN ('allow', 'label', 'hold', 'remove')),
    labels        text[] NOT NULL DEFAULT '{}',
    categories    text[] NOT NULL DEFAULT '{}',
    hits          jsonb NOT NULL DEFAULT '[]',         -- [{rule, action, category, detail, reason}]
    model         text,                                -- the snapshot that answered
    model_status  text NOT NULL,                       -- ok | off | skipped | unavailable | rate_limited | timeout | cap_reached | error
    answers       jsonb,                               -- {question: P(yes)}
    distrusted    boolean NOT NULL DEFAULT false,      -- the text addressed the moderator
    latency_ms    bigint,
    cost_usd      numeric(14, 6),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_decisions_target_idx ON moderation_decisions (target_kind, target_id, created_at DESC);
CREATE INDEX moderation_decisions_hash_idx ON moderation_decisions (text_hash, created_at DESC) WHERE text_hash IS NOT NULL;
CREATE INDEX moderation_decisions_author_idx ON moderation_decisions (author_id, created_at DESC);
CREATE INDEX moderation_decisions_created_idx ON moderation_decisions (created_at DESC);

-- Cases screening opens ('auto') hold their content hidden until staff
-- approve (dismiss) or remove it.
ALTER TABLE moderation_cases
    ADD COLUMN source     text NOT NULL DEFAULT 'report' CHECK (source IN ('report', 'auto')),
    ADD COLUMN categories text[] NOT NULL DEFAULT '{}';
CREATE INDEX moderation_cases_author_content_idx ON moderation_cases (author_id, created_at DESC, id DESC)
    WHERE target_kind <> 'user';

-- A staff removal or ban adds one; they count for 90 days and restrict
-- posting at MODERATION_STRIKE_LIMITS.
CREATE TABLE moderation_strikes (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid NOT NULL REFERENCES users (id),
    case_id    uuid REFERENCES moderation_cases (id),
    action     text NOT NULL CHECK (action IN ('remove', 'ban')),
    category   text,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);
CREATE INDEX moderation_strikes_user_idx ON moderation_strikes (user_id, created_at DESC) WHERE revoked_at IS NULL;
