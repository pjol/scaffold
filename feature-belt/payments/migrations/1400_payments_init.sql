-- payments: provider customers, the price catalog, hosted checkout sessions,
-- one-time orders, subscriptions, entitlements, an append-only money ledger,
-- refunds and Connect accounts. Money is bigint minor units beside a
-- lowercase currency. Every provider id sits beside a provider column, so
-- another gateway can take over without a schema change.

-- One provider customer per user and provider.
CREATE TABLE payment_customers (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              uuid NOT NULL REFERENCES users (id),
    provider             text NOT NULL,
    provider_customer_id text NOT NULL,
    -- Account deleted: remove the provider customer and purge this user's
    -- payment rows after this time (PAYMENTS_RETENTION_YEARS).
    delete_after         timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider),
    UNIQUE (provider, provider_customer_id)
);
CREATE INDEX payment_customers_created_idx ON payment_customers (created_at DESC, id DESC);
CREATE INDEX payment_customers_retention_idx ON payment_customers (delete_after) WHERE delete_after IS NOT NULL;

-- Connect: a user's payout account.
CREATE TABLE payment_connected_accounts (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid NOT NULL REFERENCES users (id),
    provider            text NOT NULL,
    provider_account_id text NOT NULL,
    charges_enabled     boolean NOT NULL DEFAULT false,
    payouts_enabled     boolean NOT NULL DEFAULT false,
    details_submitted   boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, provider),
    UNIQUE (provider, provider_account_id)
);

-- The catalog: stable app keys mapped to provider prices. Amount, currency
-- and interval are copied from the provider when an entry is saved.
CREATE TABLE payment_prices (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key                 text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_]{1,62}$'),
    provider            text NOT NULL,
    provider_price_id   text NOT NULL,
    label               text NOT NULL DEFAULT '',
    kind                text NOT NULL CHECK (kind IN ('one_time', 'recurring')),
    amount_minor        bigint CHECK (amount_minor >= 0),      -- null for tiered or pay-what-you-want prices
    currency            text NOT NULL,
    billing_interval    text CHECK (billing_interval IN ('day', 'week', 'month', 'year')),
    interval_count      int,
    entitlement         text CHECK (entitlement ~ '^[a-z][a-z0-9_]{1,62}$'), -- what buying it grants; null for none
    entitlement_days    int CHECK (entitlement_days > 0),     -- one-time prices: access length; null = no expiry
    destination_user_id uuid REFERENCES users (id),           -- Connect: who receives the money
    active              boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_prices_provider_idx ON payment_prices (provider, provider_price_id);
CREATE INDEX payment_prices_entitlement_idx ON payment_prices (entitlement) WHERE active;

-- Hosted checkout pages. The id is generated before the provider call (it
-- is in the return URLs). pending → completed | expired | failed.
CREATE TABLE payment_checkout_sessions (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid NOT NULL REFERENCES users (id),
    provider            text NOT NULL,
    provider_session_id text,
    price_key           text NOT NULL,
    mode                text NOT NULL CHECK (mode IN ('payment', 'subscription')),
    quantity            int NOT NULL CHECK (quantity BETWEEN 1 AND 100),
    amount_minor        bigint,                               -- expected total (unit price × quantity) when fixed
    currency            text NOT NULL,
    destination_user_id uuid REFERENCES users (id),
    platform_fee_minor  bigint NOT NULL DEFAULT 0,
    ref                 text NOT NULL DEFAULT '',             -- app reference from stitching code, copied to the order
    status              text NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'completed', 'expired', 'failed')),
    url                 text,
    expires_at          timestamptz,
    completed_at        timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_session_id)
);
CREATE INDEX payment_checkout_sessions_user_idx ON payment_checkout_sessions (user_id, created_at DESC, id DESC);
CREATE INDEX payment_checkout_sessions_pending_idx ON payment_checkout_sessions (created_at) WHERE status = 'pending';
CREATE INDEX payment_checkout_sessions_price_idx ON payment_checkout_sessions (price_key);

-- One-time purchases. Written only from provider state (webhook or
-- reconcile), never from a client.
CREATE TABLE payment_orders (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid NOT NULL REFERENCES users (id),
    checkout_session_id uuid NOT NULL UNIQUE REFERENCES payment_checkout_sessions (id),
    provider            text NOT NULL,
    provider_payment_id text NOT NULL,
    price_key           text NOT NULL,
    quantity            int NOT NULL,
    amount_minor        bigint NOT NULL,                      -- what the provider took
    currency            text NOT NULL,
    refunded_minor      bigint NOT NULL DEFAULT 0,
    platform_fee_minor  bigint NOT NULL DEFAULT 0,
    destination_user_id uuid REFERENCES users (id),
    ref                 text NOT NULL DEFAULT '',
    shipping            jsonb,                                -- address Checkout collected (PAYMENTS_SHIPPING_COUNTRIES)
    status              text NOT NULL DEFAULT 'paid' CHECK (status IN ('paid', 'partially_refunded', 'refunded')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payment_orders_user_idx ON payment_orders (user_id, created_at DESC, id DESC);
CREATE INDEX payment_orders_payment_idx ON payment_orders (provider, provider_payment_id);
CREATE INDEX payment_orders_ref_idx ON payment_orders (user_id, ref) WHERE ref <> '';
CREATE INDEX payment_orders_destination_idx ON payment_orders (destination_user_id, created_at DESC)
    WHERE destination_user_id IS NOT NULL;

-- Subscriptions, mirrored from the provider (one price each).
CREATE TABLE payment_subscriptions (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                  uuid NOT NULL REFERENCES users (id),
    provider                 text NOT NULL,
    provider_subscription_id text NOT NULL,
    provider_customer_id     text NOT NULL,
    provider_price_id        text NOT NULL,
    price_key                text NOT NULL DEFAULT '',
    quantity                 int NOT NULL DEFAULT 1,
    destination_user_id      uuid REFERENCES users (id),      -- Connect: who receives each invoice
    status                   text NOT NULL CHECK (status IN ('incomplete', 'incomplete_expired', 'trialing', 'active',
                                                             'past_due', 'canceled', 'unpaid', 'paused')),
    current_period_start     timestamptz,
    current_period_end       timestamptz,
    cancel_at_period_end     boolean NOT NULL DEFAULT false,
    cancel_at                timestamptz,
    canceled_at              timestamptz,
    ended_at                 timestamptz,
    cancel_pending           boolean NOT NULL DEFAULT false,  -- account deleted, provider cancel failed: reconcile retries
    synced_at                timestamptz NOT NULL DEFAULT now(),
    created_at               timestamptz NOT NULL DEFAULT now(),
    updated_at               timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_subscription_id)
);
CREATE INDEX payment_subscriptions_user_idx ON payment_subscriptions (user_id, created_at DESC, id DESC);
CREATE INDEX payment_subscriptions_live_idx ON payment_subscriptions (synced_at)
    WHERE status IN ('incomplete', 'trialing', 'active', 'past_due', 'unpaid', 'paused') OR cancel_pending;
CREATE INDEX payment_subscriptions_price_idx ON payment_subscriptions (price_key);

-- What a user holds, derived from orders and subscriptions.
CREATE TABLE payment_entitlements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    key          text NOT NULL,
    source       text NOT NULL CHECK (source IN ('order', 'subscription')),
    source_id    uuid NOT NULL,                             -- payment_orders.id or payment_subscriptions.id
    active_until timestamptz,                               -- null: no expiry
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source, source_id, key)
);
CREATE INDEX payment_entitlements_user_idx ON payment_entitlements (user_id, key);

-- Append-only money ledger: payments and invoices in (positive), refunds
-- out (negative). The idempotency key makes every provider fact land once.
CREATE TABLE payment_ledger (
    id                  bigserial PRIMARY KEY,
    user_id             uuid NOT NULL REFERENCES users (id),
    kind                text NOT NULL CHECK (kind IN ('payment', 'invoice', 'refund')),
    amount_minor        bigint NOT NULL,
    currency            text NOT NULL,
    provider            text NOT NULL,
    provider_ref        text NOT NULL,                      -- payment, invoice or refund id
    provider_payment_id text,                               -- the payment refunds target
    order_id            uuid REFERENCES payment_orders (id),
    subscription_id     uuid REFERENCES payment_subscriptions (id),
    description         text NOT NULL DEFAULT '',
    idempotency_key     text NOT NULL UNIQUE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'refund' AND amount_minor < 0) OR (kind <> 'refund' AND amount_minor > 0))
);
CREATE INDEX payment_ledger_user_idx ON payment_ledger (user_id, created_at DESC, id DESC);
CREATE INDEX payment_ledger_created_idx ON payment_ledger (created_at DESC, id DESC);
CREATE INDEX payment_ledger_payment_idx ON payment_ledger (provider, provider_payment_id)
    WHERE provider_payment_id IS NOT NULL;

CREATE FUNCTION payment_ledger_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'payment_ledger is append-only: record a correcting entry instead';
END
$$;
CREATE TRIGGER payment_ledger_no_update BEFORE UPDATE ON payment_ledger
    FOR EACH ROW EXECUTE FUNCTION payment_ledger_append_only();

-- Refunds, whether staff made them here or in the provider's dashboard.
CREATE TABLE payment_refunds (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid NOT NULL REFERENCES users (id),
    ledger_id           bigint NOT NULL REFERENCES payment_ledger (id), -- the payment being refunded
    order_id            uuid REFERENCES payment_orders (id),
    provider            text NOT NULL,
    provider_refund_id  text,                               -- null until the provider answers
    provider_payment_id text NOT NULL,
    amount_minor        bigint NOT NULL CHECK (amount_minor > 0),
    currency            text NOT NULL,
    reason              text NOT NULL DEFAULT '',
    note                text NOT NULL DEFAULT '',
    status              text NOT NULL CHECK (status IN ('pending', 'succeeded', 'failed', 'canceled')),
    requested_by        uuid REFERENCES users (id),         -- staff; null when made at the provider
    idempotency_key     text NOT NULL UNIQUE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_refund_id)
);
CREATE INDEX payment_refunds_user_idx ON payment_refunds (user_id, created_at DESC, id DESC);
CREATE INDEX payment_refunds_created_idx ON payment_refunds (created_at DESC, id DESC);
CREATE INDEX payment_refunds_ledger_idx ON payment_refunds (ledger_id);
CREATE INDEX payment_refunds_payment_idx ON payment_refunds (provider, provider_payment_id);
