-- Reference catalog (feature-belt/catalog): items imported from an outside source by the sync,
-- loaded into memory on every instance. Not the exchange feature's catalog of markets.

-- One row per item. key is the source's stable id. Rows are never deleted: an item the source
-- stops listing is retired (retired_by 'source') and restored when it comes back; staff
-- retirements ('staff') hold until staff restore the item.
CREATE TABLE catalog_items (
    key           text PRIMARY KEY,
    name          text NOT NULL,
    aliases       text[] NOT NULL DEFAULT '{}',          -- only grow: a renamed item keeps its old name
    category      text NOT NULL DEFAULT '',
    subtitle      text NOT NULL DEFAULT '',
    image_url     text NOT NULL DEFAULT '',
    attrs         jsonb NOT NULL DEFAULT '{}'::jsonb,    -- string → string
    ref_price     numeric CHECK (ref_price >= 0),        -- a reference price from the source; null = none
    currency      text NOT NULL DEFAULT '',              -- ISO 4217, with ref_price
    retired_at    timestamptz,
    retired_by    text CHECK (retired_by IN ('source', 'staff')),
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK ((retired_at IS NULL) = (retired_by IS NULL)),
    CHECK (ref_price IS NULL OR currency <> '')
);
CREATE INDEX catalog_items_name_idx ON catalog_items (lower(name), key);  -- the admin list

-- The catalog's version: bumped in the same transaction as every change, so each instance
-- reloads its in-memory index when it moves.
CREATE TABLE catalog_state (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    version    bigint NOT NULL DEFAULT 0,
    changed_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO catalog_state (id) VALUES (true) ON CONFLICT DO NOTHING;

-- The sync log, and its lock: one 'running' row at a time across instances. A running sync
-- beats every 30 s; one silent for 2 minutes died with its instance and is failed by the next.
-- A refused run (the safety valve) records what it would have done.
CREATE TABLE catalog_sync_runs (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trigger      text NOT NULL CHECK (trigger IN ('schedule', 'staff', 'app', 'seed')),
    source       text NOT NULL,
    forced       boolean NOT NULL DEFAULT false,         -- the safety valve was skipped
    status       text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'ok', 'failed', 'refused')),
    started_at   timestamptz NOT NULL DEFAULT now(),
    heartbeat_at timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    fetched      integer NOT NULL DEFAULT 0,
    inserted     integer NOT NULL DEFAULT 0,
    updated      integer NOT NULL DEFAULT 0,
    retired      integer NOT NULL DEFAULT 0,
    restored     integer NOT NULL DEFAULT 0,
    unchanged    integer NOT NULL DEFAULT 0,
    skipped      integer NOT NULL DEFAULT 0,
    error        text NOT NULL DEFAULT '',
    notes        text[] NOT NULL DEFAULT '{}'            -- the first few skip reasons
);
CREATE UNIQUE INDEX catalog_sync_runs_one_running ON catalog_sync_runs (status) WHERE status = 'running';
CREATE INDEX catalog_sync_runs_started_idx ON catalog_sync_runs (started_at DESC, id DESC);
