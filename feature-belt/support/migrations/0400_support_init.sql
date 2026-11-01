-- support: tickets users open with staff, the message thread (with internal
-- staff notes), and attachments.

CREATE TABLE support_tickets (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    number          bigserial NOT NULL UNIQUE,                  -- human reference: #1042
    user_id         uuid NOT NULL REFERENCES users (id),
    category        text NOT NULL CHECK (category IN ('question', 'bug', 'account', 'billing', 'other')),
    subject         text NOT NULL,
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'closed')),
    assignee_id     uuid REFERENCES users (id),                 -- staff member working it
    client_platform text NOT NULL DEFAULT '',                   -- X-Client-Platform at creation: web | ios | android
    client_version  text NOT NULL DEFAULT '',                   -- X-Client-Version at creation
    last_message_at timestamptz NOT NULL DEFAULT now(),
    closed_at       timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_tickets_user_idx ON support_tickets (user_id, created_at DESC, id DESC);
CREATE INDEX support_tickets_queue_idx ON support_tickets (status, created_at DESC, id DESC);
CREATE INDEX support_tickets_created_idx ON support_tickets (created_at DESC, id DESC);
CREATE INDEX support_tickets_assignee_idx ON support_tickets (assignee_id, status) WHERE assignee_id IS NOT NULL;

-- The thread. The first message is the user's request. internal = a staff
-- note the user never sees.
CREATE TABLE support_messages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   uuid NOT NULL REFERENCES support_tickets (id) ON DELETE CASCADE,
    author_id   uuid NOT NULL REFERENCES users (id),
    author_role text NOT NULL CHECK (author_role IN ('user', 'staff')),
    internal    boolean NOT NULL DEFAULT false,
    body        text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (NOT internal OR author_role = 'staff')
);
CREATE INDEX support_messages_ticket_idx ON support_messages (ticket_id, created_at, id);

-- Files the user attached (storage purpose support_attachment, private).
CREATE TABLE support_attachments (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id    uuid NOT NULL REFERENCES support_tickets (id) ON DELETE CASCADE,
    message_id   uuid NOT NULL REFERENCES support_messages (id) ON DELETE CASCADE,
    storage_key  text NOT NULL UNIQUE,
    file_name    text NOT NULL,
    content_type text NOT NULL,
    size_bytes   bigint NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_attachments_message_idx ON support_attachments (message_id);
CREATE INDEX support_attachments_ticket_idx ON support_attachments (ticket_id);
