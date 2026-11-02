-- Links to connected repositories (GitHub, GitLab) placed in a drive folder.
-- The row is only the reference someone made: whoever opens it is checked
-- live against the platform with their own connection; nothing from the
-- repository is stored. The drive item (type repo_link) carries the name,
-- folder and sharing.
CREATE TABLE repos_links (
    id         uuid PRIMARY KEY REFERENCES files_items (id) ON DELETE CASCADE,
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    provider   text NOT NULL CHECK (provider IN ('github', 'gitlab')),
    web_base   text NOT NULL CHECK (length(web_base) BETWEEN 8 AND 300),   -- https://github.com, https://git.example.com/gitlab
    path       text NOT NULL CHECK (length(path) BETWEEN 3 AND 400),       -- owner/repo, group/subgroup/project
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX repos_links_org ON repos_links (org_id);
CREATE INDEX repos_links_created_by ON repos_links (created_by);
