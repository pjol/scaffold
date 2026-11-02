-- files: connected drives (Google Drive, OneDrive, SharePoint). Platform
-- content is read live with each person's own connection and never copied
-- here; the only references kept are the shortcuts people pin into a folder
-- and short-lived download links.

-- A shortcut: a drive item of type 'link' pointing at an item on a connected
-- platform. It stores the reference the pinner chose (the platform's id, its
-- name, kind and web address), never content or tokens; whoever opens it is
-- checked live with their own connection.
CREATE TABLE files_links (
    item_id    uuid PRIMARY KEY REFERENCES files_items (id) ON DELETE CASCADE,
    provider   text NOT NULL CHECK (provider ~ '^[a-z][a-z0-9_]{0,31}$'), -- google | microsoft
    product    text NOT NULL CHECK (product ~ '^[a-z][a-z0-9_]{0,31}$'),  -- drive | onedrive | sharepoint
    ref        text NOT NULL CHECK (char_length(ref) BETWEEN 1 AND 400),   -- the platform's id (Graph: <drive id>~<item id>)
    mime_type  text NOT NULL DEFAULT '',
    folder     boolean NOT NULL DEFAULT false,
    web_url    text NOT NULL DEFAULT '' CHECK (char_length(web_url) <= 2000),
    kind_label text NOT NULL DEFAULT '',                                   -- "Google Docs" for native documents
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 15-minute links that stream a connected file through the API (previews
-- and downloads), so the person's token never reaches the browser. Only a
-- hash of the token is stored.
CREATE TABLE files_connected_downloads (
    token_hash    bytea PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    org_id        uuid REFERENCES orgs (id) ON DELETE CASCADE,
    connection_id text NOT NULL,
    ref           text NOT NULL,
    format        text NOT NULL DEFAULT '',   -- '' the file | pdf a rendering
    content_type  text NOT NULL,
    disposition   text NOT NULL,
    expires_at    timestamptz NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX files_connected_downloads_expiry_idx ON files_connected_downloads (expires_at);
CREATE INDEX files_connected_downloads_user_idx ON files_connected_downloads (user_id);
