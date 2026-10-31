-- assistant: AI conversations and messages, per-user daily usage, consent to
-- send content to the AI provider, and deployment settings (prompt canary).

CREATE TABLE assistant_conversations (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid NOT NULL REFERENCES users (id),
    title              text NOT NULL DEFAULT '',
    summary            text NOT NULL DEFAULT '',            -- compacted earlier turns; replaces them in the prompt
    summary_through_at timestamptz,                         -- (created_at, id) of the last message the summary covers
    summary_through_id uuid,
    context_tokens     int NOT NULL DEFAULT 0,              -- prompt + output of the latest request; drives compaction
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    last_message_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assistant_conversations_user_idx ON assistant_conversations (user_id, last_message_at DESC, id DESC);

CREATE TABLE assistant_messages (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id   uuid NOT NULL REFERENCES assistant_conversations (id) ON DELETE CASCADE,
    user_id           uuid NOT NULL REFERENCES users (id),   -- conversation owner (deletion, export)
    role              text NOT NULL CHECK (role IN ('user', 'assistant')),
    parts             jsonb NOT NULL DEFAULT '[]',           -- display parts: [{"type":"text","text":…}]
    activity          jsonb NOT NULL DEFAULT '[]',           -- assistant: tool calls as shown in the UI
    items             json NOT NULL DEFAULT '[]',            -- neutral transcript with raw provider blocks, replayed verbatim (json, not jsonb: bytes and key order must survive)
    provider          text NOT NULL DEFAULT '',
    model             text NOT NULL DEFAULT '',
    status            text NOT NULL DEFAULT 'complete'
                      CHECK (status IN ('streaming', 'complete', 'cancelled', 'error')),
    stop_reason       text NOT NULL DEFAULT '',
    error             jsonb,                                 -- {code, message, retryable}
    usage             jsonb NOT NULL DEFAULT '{}',           -- {input_tokens, cache_read_tokens, cache_write_tokens, output_tokens, rounds, estimated}
    cost_usd          numeric(12, 6),                        -- estimate from list prices; NULL for unpriced models
    stop_requested_at timestamptz,                           -- set by POST …/stop on any instance
    removed_at        timestamptz,                           -- taken down by staff after a report
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz
);
CREATE INDEX assistant_messages_conversation_idx ON assistant_messages (conversation_id, created_at DESC, id DESC);
CREATE INDEX assistant_messages_user_idx ON assistant_messages (user_id);
CREATE INDEX assistant_messages_streaming_idx ON assistant_messages (created_at) WHERE status = 'streaming';

-- Daily (UTC) usage per user for quotas and the admin report.
CREATE TABLE assistant_usage_daily (
    user_id            uuid NOT NULL REFERENCES users (id),
    day                date NOT NULL,
    messages           int NOT NULL DEFAULT 0,
    input_tokens       bigint NOT NULL DEFAULT 0,
    cache_read_tokens  bigint NOT NULL DEFAULT 0,
    cache_write_tokens bigint NOT NULL DEFAULT 0,
    output_tokens      bigint NOT NULL DEFAULT 0,
    cost_usd           numeric(14, 6) NOT NULL DEFAULT 0,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day)
);
CREATE INDEX assistant_usage_daily_day_idx ON assistant_usage_daily (day);

-- Explicit permission to send the user's content to a named AI provider
-- (Apple 5.1.2(i)). created_at is when the user accepted.
CREATE TABLE assistant_consents (
    user_id    uuid NOT NULL REFERENCES users (id),
    provider   text NOT NULL,                               -- anthropic | openai | mock
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, provider)
);

-- Deployment-wide values every instance shares (the prompt canary).
CREATE TABLE assistant_settings (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
