-- moderation: blocks, reports on content and people, and the staff cases
-- that group them.

-- A block in either direction hides content and stops messages both ways.
CREATE TABLE moderation_blocks (
    blocker_id uuid NOT NULL REFERENCES users (id),
    blocked_id uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);
CREATE INDEX moderation_blocks_blocked_idx ON moderation_blocks (blocked_id, blocker_id);
CREATE INDEX moderation_blocks_list_idx ON moderation_blocks (blocker_id, created_at DESC, blocked_id DESC);

-- One case per reported target while it is open; every report on that target
-- joins it. Closed cases stay as history; a later report opens a new case.
CREATE TABLE moderation_cases (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    target_kind      text NOT NULL,                          -- "user" or a registered kind: chat.message, feed.post
    target_id        text NOT NULL,
    author_id        uuid REFERENCES users (id),             -- whose content (the person, for kind "user")
    snapshot         jsonb NOT NULL,                         -- moderation.Snapshot from the first report
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    report_count     int NOT NULL DEFAULT 0,                 -- distinct reporters
    hidden_at        timestamptz,                            -- content taken down (auto-hide or staff removal)
    auto_hidden      boolean NOT NULL DEFAULT false,
    action           text CHECK (action IN ('dismiss', 'remove', 'warn', 'suspend', 'ban', 'resolve')),
    resolution_note  text NOT NULL DEFAULT '',               -- staff only
    resolved_by      uuid REFERENCES users (id),
    resolved_at      timestamptz,
    last_reported_at timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX moderation_cases_one_open_idx ON moderation_cases (target_kind, target_id) WHERE status = 'open';
CREATE INDEX moderation_cases_queue_idx ON moderation_cases (created_at, id) WHERE status = 'open';
CREATE INDEX moderation_cases_closed_idx ON moderation_cases (created_at DESC, id DESC) WHERE status <> 'open';
CREATE INDEX moderation_cases_author_idx ON moderation_cases (author_id, created_at DESC);

-- Individual reports. A reporter has one report per case: reporting again
-- updates it instead of adding a row.
CREATE TABLE moderation_reports (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id             uuid NOT NULL REFERENCES moderation_cases (id),
    reporter_id         uuid REFERENCES users (id),          -- NULL once the reporter deletes their account
    target_kind         text NOT NULL,
    target_id           text NOT NULL,
    reason              text NOT NULL CHECK (reason IN ('spam', 'harassment', 'hate', 'sexual', 'violence',
                                                        'self_harm', 'illegal', 'other')),
    note                text NOT NULL DEFAULT '',
    snapshot            jsonb NOT NULL,                      -- the content as this reporter saw it
    outcome_notified_at timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX moderation_reports_one_per_reporter_idx ON moderation_reports (case_id, reporter_id);
CREATE INDEX moderation_reports_reporter_idx ON moderation_reports (reporter_id, created_at DESC, id DESC);
CREATE INDEX moderation_reports_case_idx ON moderation_reports (case_id, created_at, id);
