-- wallets: linked EVM addresses, SIWE challenges, ERC-20 transfers from the indexer.
-- Addresses and hashes are stored lowercase hex (COLLATE "C": byte order, so
-- prefix searches use the index); the API returns EIP-55 checksummed forms.

-- A user's linked wallets. An address belongs to at most one user. The
-- address is chain-agnostic (the same EOA on every EVM chain).
CREATE TABLE wallets (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users (id),
    address           text COLLATE "C" NOT NULL CHECK (address ~ '^0x[0-9a-f]{40}$'),
    kind              text NOT NULL CHECK (kind IN ('external', 'embedded')),
    proof             text NOT NULL CHECK (proof IN ('siwe', 'eip1271', 'privy')),
    verified_chain_id bigint,                                  -- chain named in the signed SIWE message
    provider_subject  text,                                    -- Privy user id (did:privy:…) for embedded wallets
    label             text NOT NULL DEFAULT '' CHECK (char_length(label) <= 40),
    is_primary        boolean NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX wallets_address_key ON wallets (address);
CREATE UNIQUE INDEX wallets_one_primary_key ON wallets (user_id) WHERE is_primary;
CREATE INDEX wallets_user_idx ON wallets (user_id, created_at, id);
CREATE INDEX wallets_created_idx ON wallets (created_at DESC, id DESC);
CREATE INDEX wallets_provider_subject_idx ON wallets (provider_subject) WHERE provider_subject IS NOT NULL;

-- Sign-In with Ethereum challenges: single use, valid 10 minutes, pruned a
-- day after expiry.
CREATE TABLE wallet_nonces (
    nonce      text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id),
    address    text COLLATE "C" NOT NULL CHECK (address ~ '^0x[0-9a-f]{40}$'),
    chain_id   bigint NOT NULL CHECK (chain_id > 0),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX wallet_nonces_user_idx ON wallet_nonces (user_id);
CREATE INDEX wallet_nonces_expires_idx ON wallet_nonces (expires_at);

-- ERC-20 transfers touching linked addresses, one row per (chain, tx, log).
-- from_user_id / to_user_id are the owners when recorded; unlinking clears
-- them, and a row nobody is linked to is deleted. Token symbol and decimals
-- are copied at ingest so history renders after the config changes.
-- amount is base units (decimals is a count of digits, never a multiplier).
CREATE TABLE wallet_transfers (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    chain_id       bigint NOT NULL CHECK (chain_id > 0),
    tx_hash        text COLLATE "C" NOT NULL CHECK (tx_hash ~ '^0x[0-9a-f]{64}$'),
    log_index      integer NOT NULL CHECK (log_index >= 0),
    block_number   bigint NOT NULL CHECK (block_number >= 0),
    token_address  text COLLATE "C" NOT NULL CHECK (token_address ~ '^0x[0-9a-f]{40}$'),
    token_symbol   text NOT NULL,
    token_decimals smallint NOT NULL CHECK (token_decimals BETWEEN 0 AND 36),
    from_address   text COLLATE "C" NOT NULL CHECK (from_address ~ '^0x[0-9a-f]{40}$'),
    to_address     text COLLATE "C" NOT NULL CHECK (to_address ~ '^0x[0-9a-f]{40}$'),
    amount         numeric(78, 0) NOT NULL CHECK (amount >= 0),
    from_user_id   uuid REFERENCES users (id),
    to_user_id     uuid REFERENCES users (id),
    occurred_at    timestamptz NOT NULL,                       -- block timestamp
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wallet_transfers_event_key UNIQUE (chain_id, tx_hash, log_index)
);
CREATE INDEX wallet_transfers_from_user_idx ON wallet_transfers (from_user_id, occurred_at DESC, id DESC)
    WHERE from_user_id IS NOT NULL;
CREATE INDEX wallet_transfers_to_user_idx ON wallet_transfers (to_user_id, occurred_at DESC, id DESC)
    WHERE to_user_id IS NOT NULL;
-- Rows left with no linked user are deleted in the same transaction; this
-- keeps finding them cheap.
CREATE INDEX wallet_transfers_orphan_idx ON wallet_transfers (id)
    WHERE from_user_id IS NULL AND to_user_id IS NULL;
