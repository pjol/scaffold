-- docs 1.2.0: date reminders. A date mention with remind_at notifies the
-- person who set it at that time (jobs + notifications), if they can still
-- open the page. Rows follow the block: editing the reminder away or
-- deleting the block removes them.

CREATE TABLE docs_reminders (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id    uuid NOT NULL REFERENCES docs_pages (id) ON DELETE CASCADE,
    block_id   uuid NOT NULL REFERENCES docs_blocks (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    remind_at  timestamptz NOT NULL,
    date_label text NOT NULL DEFAULT '',           -- the date as written (YYYY-MM-DD[ HH:MM])
    sent_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (block_id, remind_at)
);
CREATE INDEX docs_reminders_due_idx ON docs_reminders (remind_at) WHERE sent_at IS NULL;
CREATE INDEX docs_reminders_user_idx ON docs_reminders (user_id);
CREATE INDEX docs_reminders_page_idx ON docs_reminders (page_id);
