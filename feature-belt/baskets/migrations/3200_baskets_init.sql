-- 3200: baskets. Composite markets people create from a list of exchange
-- markets. Each distinct basket (by canonical hash) is one exchange market
-- (exchange_markets, written through exchange.UpsertCatalog) priced by one
-- oracle basket feed (oracle_feeds kind 'basket', key = the symbol).

CREATE TABLE baskets (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol        text NOT NULL UNIQUE CHECK (symbol ~ '^[A-Z0-9][A-Z0-9-]{0,23}$'),
    hash          text NOT NULL UNIQUE CHECK (hash ~ '^[0-9a-f]{64}$'), -- sha256 of canonical
    canonical     text NOT NULL,                                       -- "basket-v1\n" + "SYMBOL\tQTY\n"…
    name          text NOT NULL CHECK (length(name) BETWEEN 3 AND 48),
    description   text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
    created_by    uuid REFERENCES users(id),                           -- NULL once the creator deleted their account
    source        text NOT NULL DEFAULT 'api',                         -- web | ios | android | api | agent | seed
    value_at_create numeric NOT NULL CHECK (value_at_create > 0),      -- Σ quantity × oracle price at creation
    max_share     numeric NOT NULL,                                    -- largest line's share of that value, 0–1
    ends_on       date,                                                -- optional end: reduce_only after the grace period
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reduce_only', 'archived')),
    status_at     timestamptz NOT NULL DEFAULT now(),
    hidden        boolean NOT NULL DEFAULT false,                      -- staff: out of discovery, winds down, never revives
    entries       int NOT NULL DEFAULT 0,                              -- people who registered it (basket_entries)
    open_reports  int NOT NULL DEFAULT 0,                              -- unresolved basket_reports
    -- Usage, refreshed by the hourly lifecycle job from exchange.UsageOf:
    volume_recent numeric NOT NULL DEFAULT 0,                          -- people's traded notional over BASKETS_IDLE_DAYS
    open_interest numeric NOT NULL DEFAULT 0,                          -- people's open notional
    holders       int NOT NULL DEFAULT 0,
    popularity    numeric NOT NULL DEFAULT 0,                          -- volume_recent + 25 × recent entries
    usage_at      timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX baskets_new ON baskets (created_at DESC, id DESC) WHERE NOT hidden;
CREATE INDEX baskets_popular ON baskets (popularity DESC, created_at DESC, id DESC) WHERE NOT hidden;
CREATE INDEX baskets_creator ON baskets (created_by, created_at DESC);
CREATE INDEX baskets_status ON baskets (status);

-- The canonical lines: one per market, integer quantities.
CREATE TABLE basket_lines (
    basket_id       uuid NOT NULL REFERENCES baskets(id) ON DELETE CASCADE,
    market          text NOT NULL,                                     -- exchange symbol
    feed            text NOT NULL,                                     -- oracle feed key that prices it
    name            text NOT NULL,                                     -- market name at creation
    quantity        int NOT NULL CHECK (quantity > 0),
    price_at_create numeric NOT NULL CHECK (price_at_create > 0),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (basket_id, market)
);

-- "This person registered this basket": every create-or-reuse writes one (once per person).
CREATE TABLE basket_entries (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    basket_id  uuid NOT NULL REFERENCES baskets(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id),
    source     text NOT NULL,                                          -- web | ios | android | api | agent | seed
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (basket_id, user_id)
);
CREATE INDEX basket_entries_user ON basket_entries (user_id, created_at DESC, id DESC);
CREATE INDEX basket_entries_recent ON basket_entries (basket_id, created_at DESC);

-- Reports of a basket's name or description. Staff resolve them in Admin → Baskets.
CREATE TABLE basket_reports (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    basket_id   uuid NOT NULL REFERENCES baskets(id) ON DELETE CASCADE,
    reporter_id uuid REFERENCES users(id),                             -- NULL once the reporter deleted their account
    reason      text NOT NULL CHECK (reason IN ('name', 'description', 'spam', 'other')),
    note        text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    UNIQUE (basket_id, reporter_id)
);
CREATE INDEX basket_reports_open ON basket_reports (basket_id) WHERE resolved_at IS NULL;
CREATE INDEX basket_reports_reporter ON basket_reports (reporter_id);
