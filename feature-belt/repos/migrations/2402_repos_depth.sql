-- repos 1.2: issues, labels and milestones (shared by issues and pull
-- requests), reactions and cross-references, pull request depth (drafts,
-- pending reviews, multi-line and suggestion comments, files viewed,
-- auto-merge, rebase merges, code owners), commit statuses posted by CI with
-- per-repository tokens, releases with assets, outbound webhooks with a
-- delivery log, and one-time archive download tokens.
--
-- Issues and pull requests share repos_repos.next_pull_number, so #12 names
-- exactly one of them, as on GitHub.

-- ---- labels and milestones -----------------------------------------------------

CREATE TABLE repos_labels (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id     uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    -- a tone from the design tokens (rendered like Badge tones)
    color       text NOT NULL DEFAULT 'neutral' CHECK (color IN ('neutral', 'primary', 'accent', 'success', 'warning', 'danger', 'info')),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 100),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX repos_labels_name_key ON repos_labels (repo_id, lower(name));

CREATE TABLE repos_milestones (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id     uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    title       text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 4000),
    due_on      date,
    state       text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed')),
    closed_at   timestamptz,
    created_by  uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX repos_milestones_title_key ON repos_milestones (repo_id, lower(title));
CREATE INDEX repos_milestones_list_idx ON repos_milestones (repo_id, state, due_on NULLS LAST, id);
CREATE INDEX repos_milestones_created_by_idx ON repos_milestones (created_by) WHERE created_by IS NOT NULL;

-- ---- issues --------------------------------------------------------------------

CREATE TABLE repos_issues (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id       uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    number        int NOT NULL,
    title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 256),
    body          text NOT NULL DEFAULT '' CHECK (char_length(body) <= 65536),
    author_id     uuid REFERENCES users (id),
    state         text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed')),
    state_reason  text CHECK (state_reason IN ('completed', 'not_planned')),
    closed_by     uuid REFERENCES users (id),
    closed_at     timestamptz,
    milestone_id  uuid REFERENCES repos_milestones (id) ON DELETE SET NULL,
    locked        boolean NOT NULL DEFAULT false,      -- only people who can edit comment
    pinned_at     timestamptz,                          -- up to 3 per repository
    comment_count int NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_issues_number_key UNIQUE (repo_id, number),
    CHECK ((state = 'closed') = (state_reason IS NOT NULL))
);
CREATE INDEX repos_issues_list_idx ON repos_issues (repo_id, state, updated_at DESC, id DESC);
CREATE INDEX repos_issues_created_idx ON repos_issues (repo_id, created_at DESC, id DESC);
CREATE INDEX repos_issues_pinned_idx ON repos_issues (repo_id, pinned_at) WHERE pinned_at IS NOT NULL;
CREATE INDEX repos_issues_milestone_idx ON repos_issues (milestone_id) WHERE milestone_id IS NOT NULL;
CREATE INDEX repos_issues_author_idx ON repos_issues (author_id) WHERE author_id IS NOT NULL;
CREATE INDEX repos_issues_closed_by_idx ON repos_issues (closed_by) WHERE closed_by IS NOT NULL;

CREATE TABLE repos_issue_labels (
    issue_id uuid NOT NULL REFERENCES repos_issues (id) ON DELETE CASCADE,
    label_id uuid NOT NULL REFERENCES repos_labels (id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);
CREATE INDEX repos_issue_labels_label_idx ON repos_issue_labels (label_id);

CREATE TABLE repos_issue_assignees (
    issue_id    uuid NOT NULL REFERENCES repos_issues (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id),
    assigned_by uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id)
);
CREATE INDEX repos_issue_assignees_user_idx ON repos_issue_assignees (user_id);
CREATE INDEX repos_issue_assignees_by_idx ON repos_issue_assignees (assigned_by) WHERE assigned_by IS NOT NULL;

CREATE TABLE repos_issue_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   uuid NOT NULL REFERENCES repos_issues (id) ON DELETE CASCADE,
    author_id  uuid REFERENCES users (id),
    body       text NOT NULL CHECK (char_length(body) <= 65536),
    edited_at  timestamptz,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_issue_comments_issue_idx ON repos_issue_comments (issue_id, created_at, id);
CREATE INDEX repos_issue_comments_author_idx ON repos_issue_comments (author_id) WHERE author_id IS NOT NULL;

CREATE TABLE repos_issue_events (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id   uuid NOT NULL REFERENCES repos_issues (id) ON DELETE CASCADE,
    actor_id   uuid REFERENCES users (id),
    kind       text NOT NULL CHECK (kind IN ('closed', 'reopened', 'renamed', 'labeled', 'unlabeled', 'assigned', 'unassigned',
                                             'milestoned', 'demilestoned', 'locked', 'unlocked', 'pinned', 'unpinned', 'referenced')),
    data       jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_issue_events_issue_idx ON repos_issue_events (issue_id, created_at, id);
CREATE INDEX repos_issue_events_actor_idx ON repos_issue_events (actor_id) WHERE actor_id IS NOT NULL;

-- ---- reactions and references --------------------------------------------------

CREATE TABLE repos_reactions (
    subject_type text NOT NULL CHECK (subject_type IN ('issue', 'issue_comment', 'pull', 'pull_comment')),
    subject_id   uuid NOT NULL,
    repo_id      uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id),
    emoji        text NOT NULL CHECK (emoji IN ('+1', '-1', 'laugh', 'hooray', 'confused', 'heart', 'rocket', 'eyes')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subject_type, subject_id, user_id, emoji)
);
CREATE INDEX repos_reactions_user_idx ON repos_reactions (user_id);
CREATE INDEX repos_reactions_repo_idx ON repos_reactions (repo_id);

-- "#12" written in an issue, a pull request, a comment or a commit message.
-- closes: a closing keyword ("Fixes #12") in a pull request's description or
-- in a commit that reached the default branch.
CREATE TABLE repos_references (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id       uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    target_number int NOT NULL,
    source_type   text NOT NULL CHECK (source_type IN ('issue', 'pull', 'commit')),
    source_key    text NOT NULL,                -- issue/pull number, or the commit id
    closes        boolean NOT NULL DEFAULT false,
    actor_id      uuid REFERENCES users (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_references_key UNIQUE (repo_id, target_number, source_type, source_key)
);
CREATE INDEX repos_references_source_idx ON repos_references (repo_id, source_type, source_key);
CREATE INDEX repos_references_actor_idx ON repos_references (actor_id) WHERE actor_id IS NOT NULL;

-- ---- pull request depth --------------------------------------------------------

ALTER TABLE repos_pulls
    ADD COLUMN draft             boolean NOT NULL DEFAULT false,
    ADD COLUMN milestone_id      uuid REFERENCES repos_milestones (id) ON DELETE SET NULL,
    ADD COLUMN auto_merge_method text CHECK (auto_merge_method IN ('merge', 'squash', 'rebase')),
    ADD COLUMN auto_merge_by     uuid REFERENCES users (id),
    ADD COLUMN auto_merge_at     timestamptz;
ALTER TABLE repos_pulls DROP CONSTRAINT IF EXISTS repos_pulls_merge_method_check;
ALTER TABLE repos_pulls ADD CONSTRAINT repos_pulls_merge_method_check CHECK (merge_method IN ('merge', 'squash', 'rebase'));
CREATE INDEX repos_pulls_milestone_idx ON repos_pulls (milestone_id) WHERE milestone_id IS NOT NULL;
CREATE INDEX repos_pulls_auto_merge_idx ON repos_pulls (repo_id) WHERE auto_merge_method IS NOT NULL AND state = 'open';
CREATE INDEX repos_pulls_auto_merge_by_idx ON repos_pulls (auto_merge_by) WHERE auto_merge_by IS NOT NULL;

ALTER TABLE repos_repos ADD COLUMN allow_rebase boolean NOT NULL DEFAULT true;
ALTER TABLE repos_repos ADD COLUMN allow_auto_merge boolean NOT NULL DEFAULT true;
ALTER TABLE repos_repos DROP CONSTRAINT IF EXISTS repos_repos_check;
ALTER TABLE repos_repos ADD CONSTRAINT repos_repos_merge_methods_check CHECK (allow_merge_commit OR allow_squash OR allow_rebase);

ALTER TABLE repos_protections
    ADD COLUMN require_code_owner_review boolean NOT NULL DEFAULT false,
    ADD COLUMN required_checks           text[] NOT NULL DEFAULT '{}' CHECK (cardinality(required_checks) <= 20);

ALTER TABLE repos_pull_events DROP CONSTRAINT IF EXISTS repos_pull_events_kind_check;
ALTER TABLE repos_pull_events ADD CONSTRAINT repos_pull_events_kind_check CHECK (kind IN (
    'opened', 'pushed', 'force_pushed', 'closed', 'reopened', 'merged', 'review_requested', 'review_request_removed', 'retitled',
    'base_changed', 'branch_deleted', 'approvals_dismissed', 'ready_for_review', 'converted_to_draft', 'labeled', 'unlabeled',
    'assigned', 'unassigned', 'milestoned', 'demilestoned', 'referenced', 'auto_merge_enabled', 'auto_merge_disabled',
    'branch_updated', 'suggestions_applied'));

CREATE TABLE repos_pull_labels (
    pull_id  uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    label_id uuid NOT NULL REFERENCES repos_labels (id) ON DELETE CASCADE,
    PRIMARY KEY (pull_id, label_id)
);
CREATE INDEX repos_pull_labels_label_idx ON repos_pull_labels (label_id);

CREATE TABLE repos_pull_assignees (
    pull_id     uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users (id),
    assigned_by uuid REFERENCES users (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pull_id, user_id)
);
CREATE INDEX repos_pull_assignees_user_idx ON repos_pull_assignees (user_id);
CREATE INDEX repos_pull_assignees_by_idx ON repos_pull_assignees (assigned_by) WHERE assigned_by IS NOT NULL;

-- A pending review holds its author's line comments until they submit it;
-- nobody else sees them meanwhile. One per person per pull request.
ALTER TABLE repos_reviews DROP CONSTRAINT IF EXISTS repos_reviews_state_check;
ALTER TABLE repos_reviews ADD CONSTRAINT repos_reviews_state_check CHECK (state IN ('approved', 'changes_requested', 'commented', 'pending'));
CREATE UNIQUE INDEX repos_reviews_pending_key ON repos_reviews (pull_id, author_id) WHERE state = 'pending';

-- Multi-line comments (start_line..line) and applied suggestions.
ALTER TABLE repos_comments
    ADD COLUMN start_line            int CHECK (start_line > 0),
    ADD COLUMN suggestion_applied_sha text;
ALTER TABLE repos_comments ADD CONSTRAINT repos_comments_start_line_check2 CHECK (start_line IS NULL OR (line IS NOT NULL AND start_line <= line));

-- "Viewed" checkboxes on changed files: the file's blob when marked, so a
-- new change to it clears the mark.
CREATE TABLE repos_viewed_files (
    pull_id    uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    path       text NOT NULL CHECK (char_length(path) BETWEEN 1 AND 4096),
    blob_oid   text NOT NULL,                  -- '' for a deleted file
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pull_id, user_id, path)
);
CREATE INDEX repos_viewed_files_user_idx ON repos_viewed_files (user_id);

-- ---- commit statuses -----------------------------------------------------------

-- Per-repository tokens CI uses to post statuses. Only the sha256 is kept.
CREATE TABLE repos_ci_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id      uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    prefix       text NOT NULL,
    secret_hash  text NOT NULL,
    created_by   uuid REFERENCES users (id),
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_ci_tokens_hash_key UNIQUE (secret_hash)
);
CREATE INDEX repos_ci_tokens_repo_idx ON repos_ci_tokens (repo_id, created_at DESC, id DESC);
CREATE INDEX repos_ci_tokens_created_by_idx ON repos_ci_tokens (created_by) WHERE created_by IS NOT NULL;

-- The latest status per commit and context (a CI job name).
CREATE TABLE repos_statuses (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id     uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    sha         text NOT NULL CHECK (sha ~ '^[0-9a-f]{40}([0-9a-f]{24})?$'),
    context     text NOT NULL CHECK (char_length(context) BETWEEN 1 AND 100),
    state       text NOT NULL CHECK (state IN ('pending', 'success', 'failure', 'error')),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 140),
    target_url  text CHECK (char_length(target_url) <= 2000),
    token_id    uuid REFERENCES repos_ci_tokens (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_statuses_key UNIQUE (repo_id, sha, context)
);
CREATE INDEX repos_statuses_token_idx ON repos_statuses (token_id) WHERE token_id IS NOT NULL;

-- ---- releases ------------------------------------------------------------------

CREATE TABLE repos_releases (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id      uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    tag          text NOT NULL CHECK (char_length(tag) BETWEEN 1 AND 200),
    name         text NOT NULL DEFAULT '' CHECK (char_length(name) <= 200),
    body         text NOT NULL DEFAULT '' CHECK (char_length(body) <= 65536),
    draft        boolean NOT NULL DEFAULT false,
    prerelease   boolean NOT NULL DEFAULT false,
    target_sha   text NOT NULL,
    author_id    uuid REFERENCES users (id),
    published_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_releases_tag_key UNIQUE (repo_id, tag)
);
CREATE INDEX repos_releases_list_idx ON repos_releases (repo_id, created_at DESC, id DESC);
CREATE INDEX repos_releases_author_idx ON repos_releases (author_id) WHERE author_id IS NOT NULL;

-- Asset files live in storage under an org-owned key (they outlive the uploader's account).
CREATE TABLE repos_release_assets (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id     uuid NOT NULL REFERENCES repos_releases (id) ON DELETE CASCADE,
    name           text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 255),
    storage_key    text NOT NULL,
    content_type   text NOT NULL,
    size_bytes     bigint NOT NULL CHECK (size_bytes >= 0),
    download_count int NOT NULL DEFAULT 0,
    uploaded_by    uuid REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_release_assets_name_key UNIQUE (release_id, name)
);
CREATE INDEX repos_release_assets_uploaded_by_idx ON repos_release_assets (uploaded_by) WHERE uploaded_by IS NOT NULL;

-- ---- outbound webhooks ---------------------------------------------------------

CREATE TABLE repos_webhooks (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id          uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    url              text NOT NULL CHECK (char_length(url) BETWEEN 8 AND 2000),
    secret           text NOT NULL,                 -- signs each delivery (HMAC-SHA256); never returned after creation
    events           text[] NOT NULL CHECK (cardinality(events) BETWEEN 1 AND 10),
    active           boolean NOT NULL DEFAULT true,
    created_by       uuid REFERENCES users (id),
    last_status      int,
    last_delivery_at timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_webhooks_repo_idx ON repos_webhooks (repo_id);
CREATE INDEX repos_webhooks_created_by_idx ON repos_webhooks (created_by) WHERE created_by IS NOT NULL;

CREATE TABLE repos_webhook_deliveries (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id    uuid NOT NULL REFERENCES repos_webhooks (id) ON DELETE CASCADE,
    event         text NOT NULL,
    action        text NOT NULL DEFAULT '',
    payload       text NOT NULL,                  -- the exact body sent (redeliveries resend it)
    status_code   int,
    response_body text NOT NULL DEFAULT '',       -- first 4 KB
    error         text NOT NULL DEFAULT '',
    duration_ms   int NOT NULL DEFAULT 0,
    redelivery_of uuid REFERENCES repos_webhook_deliveries (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_webhook_deliveries_hook_idx ON repos_webhook_deliveries (webhook_id, created_at DESC, id DESC);
CREATE INDEX repos_webhook_deliveries_redelivery_idx ON repos_webhook_deliveries (redelivery_of) WHERE redelivery_of IS NOT NULL;

-- ---- archive downloads ---------------------------------------------------------

-- A browser can't send the session header with a plain link, so an archive
-- download is a one-time token (sha256 kept) valid for two minutes.
CREATE TABLE repos_archive_tokens (
    token_hash text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id),
    repo_id    uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    sha        text NOT NULL,
    name       text NOT NULL,
    format     text NOT NULL CHECK (format IN ('zip', 'tar.gz')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_archive_tokens_user_idx ON repos_archive_tokens (user_id);
CREATE INDEX repos_archive_tokens_repo_idx ON repos_archive_tokens (repo_id);

-- Existing repositories get the labels new ones start with.
INSERT INTO repos_labels (repo_id, name, color, description)
SELECT r.id, l.name, l.color, l.description FROM repos_repos r CROSS JOIN (VALUES
    ('bug', 'danger', 'Something isn''t working'),
    ('documentation', 'info', 'Improvements or additions to documentation'),
    ('duplicate', 'neutral', 'This issue or pull request already exists'),
    ('enhancement', 'primary', 'New feature or request'),
    ('good first issue', 'accent', 'Good for newcomers'),
    ('help wanted', 'success', 'Extra attention is needed'),
    ('question', 'warning', 'Further information is requested'),
    ('wontfix', 'neutral', 'This will not be worked on')
) AS l (name, color, description)
ON CONFLICT DO NOTHING;
