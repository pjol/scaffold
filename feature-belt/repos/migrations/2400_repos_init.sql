-- repos: git repositories in the drive. A repository is a drive item of
-- type 'repo' (files): repos_repos.id is that item's id, so its name, folder,
-- sharing (access kind files.item), AI agent scopes and trash come from the
-- drive. The git data lives on disk under REPOS_DIR/<id>.git; these tables
-- hold what git doesn't: settings, branch protection, pull requests with
-- reviews and comments, and people's git credentials.

CREATE TABLE repos_repos (
    id                     uuid PRIMARY KEY REFERENCES files_items (id) ON DELETE CASCADE,
    org_id                 uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    slug                   text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9._-]{0,99}$' AND slug NOT LIKE '%.git'),
    default_branch         text NOT NULL DEFAULT 'main',
    description            text NOT NULL DEFAULT '' CHECK (char_length(description) <= 350),
    allow_merge_commit     boolean NOT NULL DEFAULT true,
    allow_squash           boolean NOT NULL DEFAULT true,
    delete_branch_on_merge boolean NOT NULL DEFAULT false,
    next_pull_number       int NOT NULL DEFAULT 1,
    size_bytes             bigint NOT NULL DEFAULT 0,
    created_by             uuid REFERENCES users (id),
    pushed_at              timestamptz,                  -- last change to any branch or tag
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CHECK (allow_merge_commit OR allow_squash)
);
-- The clone URL is /git/<org slug>/<repo slug>.git.
CREATE UNIQUE INDEX repos_repos_slug_key ON repos_repos (org_id, slug);
CREATE INDEX repos_repos_created_by_idx ON repos_repos (created_by) WHERE created_by IS NOT NULL;
CREATE INDEX repos_repos_pushed_idx ON repos_repos (org_id, pushed_at DESC NULLS LAST, id DESC);

-- Branch protection: a rule per branch name or glob (release/*). Enforced on
-- git push (pre-receive) and on in-app commits and merges.
CREATE TABLE repos_protections (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id                 uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    pattern                 text NOT NULL CHECK (char_length(pattern) BETWEEN 1 AND 200),
    require_pull_request    boolean NOT NULL DEFAULT true,  -- no direct pushes or commits
    required_approvals      int NOT NULL DEFAULT 0 CHECK (required_approvals BETWEEN 0 AND 10),
    dismiss_stale_approvals boolean NOT NULL DEFAULT true,  -- new commits reset approvals
    allow_force_push        boolean NOT NULL DEFAULT false,
    allow_deletion          boolean NOT NULL DEFAULT false,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_protections_pattern_key UNIQUE (repo_id, pattern)
);

CREATE TABLE repos_pulls (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id          uuid NOT NULL REFERENCES repos_repos (id) ON DELETE CASCADE,
    number           int NOT NULL,
    title            text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 256),
    body             text NOT NULL DEFAULT '' CHECK (char_length(body) <= 65536),
    author_id        uuid REFERENCES users (id),
    head_branch      text NOT NULL,
    base_branch      text NOT NULL,
    head_sha         text NOT NULL,                   -- the head branch's commit, kept current on push
    base_sha         text NOT NULL,                   -- the base branch's commit when opened, then at merge
    state            text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed', 'merged')),
    merge_method     text CHECK (merge_method IN ('merge', 'squash')),
    merge_commit_sha text,
    merged_by        uuid REFERENCES users (id),
    merged_at        timestamptz,
    closed_by        uuid REFERENCES users (id),
    closed_at        timestamptz,
    comment_count    int NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_pulls_number_key UNIQUE (repo_id, number)
);
CREATE INDEX repos_pulls_list_idx ON repos_pulls (repo_id, state, updated_at DESC, id DESC);
CREATE INDEX repos_pulls_branch_idx ON repos_pulls (repo_id, head_branch) WHERE state = 'open';
CREATE INDEX repos_pulls_base_idx ON repos_pulls (repo_id, base_branch) WHERE state = 'open';
CREATE UNIQUE INDEX repos_pulls_open_pair_key ON repos_pulls (repo_id, head_branch, base_branch) WHERE state = 'open';
CREATE INDEX repos_pulls_author_idx ON repos_pulls (author_id) WHERE author_id IS NOT NULL;
CREATE INDEX repos_pulls_merged_by_idx ON repos_pulls (merged_by) WHERE merged_by IS NOT NULL;
CREATE INDEX repos_pulls_closed_by_idx ON repos_pulls (closed_by) WHERE closed_by IS NOT NULL;

CREATE TABLE repos_reviews (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pull_id      uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    author_id    uuid REFERENCES users (id),
    state        text NOT NULL CHECK (state IN ('approved', 'changes_requested', 'commented')),
    body         text NOT NULL DEFAULT '' CHECK (char_length(body) <= 65536),
    commit_sha   text NOT NULL,                      -- the head reviewed
    dismissed_at timestamptz,                        -- approvals reset by new commits
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_reviews_pull_idx ON repos_reviews (pull_id, created_at, id);
CREATE INDEX repos_reviews_author_idx ON repos_reviews (author_id) WHERE author_id IS NOT NULL;

-- Conversation comments (no path) and line comments (path + line on one
-- side of the diff). Replies point at the thread's first comment.
CREATE TABLE repos_comments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pull_id     uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    review_id   uuid REFERENCES repos_reviews (id) ON DELETE SET NULL,
    author_id   uuid REFERENCES users (id),
    body        text NOT NULL CHECK (char_length(body) <= 65536),
    path        text,
    line        int CHECK (line > 0),
    side        text CHECK (side IN ('old', 'new')),
    commit_sha  text,
    reply_to    uuid REFERENCES repos_comments (id) ON DELETE CASCADE,
    resolved_at timestamptz,                         -- thread roots
    resolved_by uuid REFERENCES users (id),
    edited_at   timestamptz,
    deleted_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CHECK ((path IS NULL) = (line IS NULL) AND (path IS NULL) = (side IS NULL))
);
CREATE INDEX repos_comments_pull_idx ON repos_comments (pull_id, created_at, id);
CREATE INDEX repos_comments_reply_idx ON repos_comments (reply_to) WHERE reply_to IS NOT NULL;
CREATE INDEX repos_comments_review_idx ON repos_comments (review_id) WHERE review_id IS NOT NULL;
CREATE INDEX repos_comments_author_idx ON repos_comments (author_id) WHERE author_id IS NOT NULL;
CREATE INDEX repos_comments_resolved_by_idx ON repos_comments (resolved_by) WHERE resolved_by IS NOT NULL;

CREATE TABLE repos_review_requests (
    pull_id      uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES users (id),
    requested_by uuid REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pull_id, user_id)
);
CREATE INDEX repos_review_requests_user_idx ON repos_review_requests (user_id, created_at DESC);
CREATE INDEX repos_review_requests_by_idx ON repos_review_requests (requested_by) WHERE requested_by IS NOT NULL;

-- A pull request's timeline besides comments and reviews.
CREATE TABLE repos_pull_events (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pull_id    uuid NOT NULL REFERENCES repos_pulls (id) ON DELETE CASCADE,
    actor_id   uuid REFERENCES users (id),
    kind       text NOT NULL CHECK (kind IN ('opened', 'pushed', 'force_pushed', 'closed', 'reopened', 'merged',
                                             'review_requested', 'review_request_removed', 'retitled',
                                             'base_changed', 'branch_deleted', 'approvals_dismissed')),
    data       jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repos_pull_events_pull_idx ON repos_pull_events (pull_id, created_at, id);
CREATE INDEX repos_pull_events_actor_idx ON repos_pull_events (actor_id) WHERE actor_id IS NOT NULL;

-- Personal git credentials for clone, fetch and push over HTTPS. Only the
-- sha256 is stored; the secret is shown once. Never agent credentials.
CREATE TABLE repos_git_credentials (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    name         text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    prefix       text NOT NULL,
    secret_hash  text NOT NULL,
    access       text NOT NULL CHECK (access IN ('read', 'write')),
    expires_at   timestamptz,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repos_git_credentials_hash_key UNIQUE (secret_hash)
);
CREATE INDEX repos_git_credentials_user_idx ON repos_git_credentials (user_id, created_at DESC, id DESC);
