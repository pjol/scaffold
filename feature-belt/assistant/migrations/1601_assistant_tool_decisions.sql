-- The user's approve/decline of a tool call that changes something in a
-- connected service. The instance running the turn waits on this table, so
-- a decision taken through any instance reaches it. Kept with the message as
-- the record of what the user allowed.
CREATE TABLE assistant_tool_decisions (
    message_id uuid NOT NULL REFERENCES assistant_messages(id) ON DELETE CASCADE,
    call_id    text NOT NULL CHECK (length(call_id) BETWEEN 1 AND 200),
    user_id    uuid NOT NULL REFERENCES users(id),
    approved   boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (message_id, call_id)
);
CREATE INDEX assistant_tool_decisions_user ON assistant_tool_decisions (user_id);
