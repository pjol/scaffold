-- 3000: oracle. Feeds, their sources, screened observations, published
-- prices and pipeline state. Prices are numeric; times are timestamptz.

-- One priced thing: a single (priced from its sources) or a basket
-- (Σ quantity × part price). Params are seconds.
CREATE TABLE oracle_feeds (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key                  text NOT NULL UNIQUE CHECK (key ~ '^[A-Z0-9][A-Z0-9._-]{0,47}$'),
    name                 text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    category             text NOT NULL DEFAULT '' CHECK (length(category) <= 60),
    quote                text NOT NULL DEFAULT 'USD' CHECK (length(quote) BETWEEN 1 AND 12),
    decimals             int NOT NULL DEFAULT 2 CHECK (decimals BETWEEN 0 AND 10),
    kind                 text NOT NULL DEFAULT 'single' CHECK (kind IN ('single', 'basket')),
    seed_price           numeric CHECK (seed_price > 0),          -- loose anchor until a source sets a live price; NULL = none
    half_life_s          int NOT NULL CHECK (half_life_s > 0),
    stale_after_s        int NOT NULL CHECK (stale_after_s > 0),
    max_move_bps_per_min numeric NOT NULL CHECK (max_move_bps_per_min > 0),
    lookback_s           int NOT NULL CHECK (lookback_s > 0),
    ref_window_s         int NOT NULL CHECK (ref_window_s > 0),
    burst_window_s       int NOT NULL CHECK (burst_window_s > 0),
    listed               boolean NOT NULL DEFAULT true,           -- false: only a basket part, not shown on its own
    enabled              boolean NOT NULL DEFAULT true,
    created_by           uuid REFERENCES users(id),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- Where a single's prints come from. source is a registered source (http,
-- manual, the app's own); venue labels the market, compared across venues.
CREATE TABLE oracle_feed_sources (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    feed_id    uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    source     text NOT NULL,
    venue      text NOT NULL CHECK (venue ~ '^[a-z0-9][a-z0-9._-]{0,39}$'),
    ref        text NOT NULL DEFAULT '',                          -- the source's id for the feed: a URL, a product id
    params     jsonb NOT NULL DEFAULT '{}'::jsonb,
    weight     numeric CHECK (weight > 0),                        -- multiplies the kind weight; NULL = 1
    premium    numeric CHECK (premium > -0.5 AND premium <= 1),   -- fixed venue premium; NULL = learned
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (feed_id, venue)
);
CREATE INDEX oracle_feed_sources_source ON oracle_feed_sources (source);

CREATE TABLE oracle_basket_parts (
    basket_id uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    feed_id   uuid NOT NULL REFERENCES oracle_feeds(id),
    quantity  numeric NOT NULL CHECK (quantity > 0),
    position  int NOT NULL DEFAULT 0,
    PRIMARY KEY (basket_id, feed_id)
);
CREATE INDEX oracle_basket_parts_feed ON oracle_basket_parts (feed_id);

-- Every screened print, accepted or not. dedupe_key (source:venue:id, or a
-- key derived from the print) is unique per feed, so pollers can re-read
-- overlapping windows without double counting and two feeds can share a
-- venue's trade ids.
CREATE TABLE oracle_observations (
    id          uuid PRIMARY KEY,
    feed_id     uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    source      text NOT NULL,
    venue       text NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('sale', 'market', 'listing')),
    price       numeric NOT NULL,                                 -- normalised by the venue premium
    raw_price   numeric NOT NULL,
    premium     numeric NOT NULL DEFAULT 0,
    qty         numeric,
    observed_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    verdict     text NOT NULL CHECK (verdict IN ('accepted', 'downweighted', 'rejected')),
    score       numeric NOT NULL,
    reasons     jsonb NOT NULL DEFAULT '[]'::jsonb,
    model       text NOT NULL,
    weight      numeric NOT NULL,
    actor_id    uuid REFERENCES users(id),                        -- staff member who posted it (manual)
    dedupe_key  text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (feed_id, dedupe_key)
);
CREATE INDEX oracle_observations_feed_observed ON oracle_observations (feed_id, observed_at DESC);
CREATE INDEX oracle_observations_received ON oracle_observations (received_at DESC, id DESC);
CREATE INDEX oracle_observations_feed_received ON oracle_observations (feed_id, received_at DESC, id DESC);
CREATE INDEX oracle_observations_feed_venue ON oracle_observations (feed_id, venue, observed_at DESC);
CREATE INDEX oracle_observations_actor ON oracle_observations (actor_id) WHERE actor_id IS NOT NULL;

-- Pushed prints waiting for the leader (staff prints, app pushes, seeds).
-- The leader claims a row only once it has loaded the row's feed.
CREATE TABLE oracle_inbox (
    id          bigserial PRIMARY KEY,
    feed_id     uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    source      text NOT NULL,
    venue       text NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('sale', 'market', 'listing')),
    price       numeric NOT NULL CHECK (price > 0),
    qty         numeric,
    observed_at timestamptz NOT NULL,
    note        text,
    actor_id    uuid REFERENCES users(id),
    dedupe_key  text NOT NULL,                                     -- the observation's dedupe_key
    taken_at    timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (feed_id, dedupe_key)
);
CREATE INDEX oracle_inbox_pending ON oracle_inbox (id) WHERE taken_at IS NULL;
CREATE INDEX oracle_inbox_actor ON oracle_inbox (actor_id) WHERE actor_id IS NOT NULL;

-- Published prices, downsampled (every 30 s on change, at least every 5 min).
CREATE TABLE oracle_prices (
    feed_id uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    at      timestamptz NOT NULL,
    price   numeric NOT NULL,
    target  numeric,
    status  text NOT NULL,
    PRIMARY KEY (feed_id, at)
);
CREATE INDEX oracle_prices_at ON oracle_prices (at);

-- The latest published price per feed (what every instance reads) and the
-- pipeline state a new leader resumes from.
CREATE TABLE oracle_feed_state (
    feed_id          uuid PRIMARY KEY REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    price            numeric,
    target           numeric,
    status           text NOT NULL CHECK (status IN ('live', 'stale', 'seed', 'off')),
    live             boolean NOT NULL,
    last_accepted_at timestamptz,
    updated_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now()
);

-- Learned venue premiums: recent log price ratios against the other venues.
CREATE TABLE oracle_venue_premiums (
    feed_id    uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    venue      text NOT NULL,
    logs       jsonb NOT NULL DEFAULT '[]'::jsonb,
    premium    numeric NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (feed_id, venue)
);

-- Health of each lane (an API host) of a polled source: rate limited or not.
CREATE TABLE oracle_source_health (
    source        text NOT NULL,
    lane          text NOT NULL DEFAULT '',
    ok            boolean NOT NULL,
    note          text,
    last_ok_at    timestamptz,
    last_error_at timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, lane)
);

-- Outcome of the last fetch of each source row (what a feed's venue shows).
CREATE TABLE oracle_ref_health (
    ref_id        uuid PRIMARY KEY REFERENCES oracle_feed_sources(id) ON DELETE CASCADE,
    ok            boolean NOT NULL,
    last_ok_at    timestamptz,
    last_error    text,
    last_error_at timestamptz,
    fails         int NOT NULL DEFAULT 0,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- Last successful fetch per (source, task), so restarts resume each source's
-- cadence instead of re-bursting every API.
CREATE TABLE oracle_fetch_log (
    source    text NOT NULL,
    fetch_key text NOT NULL,
    last_at   timestamptz NOT NULL,
    value     text,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, fetch_key)
);

-- One close per feed per UTC day: the oracle's own (source 'oracle') or
-- backfilled reference history (charts only).
CREATE TABLE oracle_history (
    feed_id uuid NOT NULL REFERENCES oracle_feeds(id) ON DELETE CASCADE,
    day     date NOT NULL,
    close   numeric NOT NULL,
    low     numeric,
    high    numeric,
    source  text NOT NULL,
    PRIMARY KEY (feed_id, day)
);

-- Which instance runs the pipeline (a lease renewed every 5 s).
CREATE TABLE oracle_leader (
    id     smallint PRIMARY KEY CHECK (id = 1),
    holder     text NOT NULL,
    until      timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
