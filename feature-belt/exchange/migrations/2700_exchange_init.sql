-- Exchange: perpetual futures on any priced asset (feature-belt/exchange).
-- Money and sizes are numeric; candle times are unix milliseconds.

-- The catalog: one row per listed market. Admins own status and manual_price.
CREATE TABLE exchange_markets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  symbol text NOT NULL UNIQUE CHECK (symbol ~ '^[A-Z0-9][A-Z0-9-]{0,23}$'),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  category text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  image_url text NOT NULL DEFAULT '',
  price_feed text NOT NULL DEFAULT '',          -- the price feed's key for the asset ('' = symbol)
  max_leverage int NOT NULL CHECK (max_leverage BETWEEN 1 AND 100),
  sz_decimals int NOT NULL CHECK (sz_decimals BETWEEN 0 AND 8),
  tick_size numeric NOT NULL CHECK (tick_size > 0),
  min_notional numeric NOT NULL CHECK (min_notional >= 0),
  oi_cap numeric NOT NULL CHECK (oi_cap > 0),
  seed_price numeric CHECK (seed_price > 0),    -- reference price before the feed reports (development only)
  manual_price numeric CHECK (manual_price > 0), -- admin override of the index price
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reduce_only', 'halted')),
  sort int NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- Trading accounts. user_id is null for system and simulated accounts, and
-- once a user deletes their account (it is closed out and kept anonymous).
CREATE TABLE exchange_accounts (
  id uuid PRIMARY KEY,
  user_id uuid UNIQUE REFERENCES users(id),
  kind text NOT NULL CHECK (kind IN ('user', 'bot', 'mm', 'backstop', 'fee')),
  label text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closing', 'closed')),
  balance numeric NOT NULL DEFAULT 0,
  deposits numeric NOT NULL DEFAULT 0,          -- net collateral put in (PnL = account value - deposits)
  last_faucet_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX exchange_accounts_system ON exchange_accounts (kind, label) WHERE kind <> 'user';
CREATE INDEX exchange_accounts_closing ON exchange_accounts (id) WHERE status = 'closing';

CREATE TABLE exchange_positions (
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  sz numeric NOT NULL,                          -- signed: long > 0
  entry_px numeric NOT NULL,
  leverage int NOT NULL CHECK (leverage >= 1),
  funding_since_open numeric NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, market_id)
);
CREATE INDEX exchange_positions_market ON exchange_positions (market_id) WHERE sz <> 0;

CREATE TABLE exchange_orders (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  side text NOT NULL CHECK (side IN ('buy', 'sell')),
  type text NOT NULL CHECK (type IN ('market', 'limit')),
  px numeric,                                   -- limit price (null for market orders)
  sz numeric NOT NULL CHECK (sz > 0),
  filled_sz numeric NOT NULL DEFAULT 0,
  status text NOT NULL CHECK (status IN ('open', 'filled', 'cancelled', 'rejected')),
  reduce_only boolean NOT NULL DEFAULT false,
  post_only boolean NOT NULL DEFAULT false,
  leverage int NOT NULL,
  liquidation boolean NOT NULL DEFAULT false,
  seq bigint NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);
CREATE INDEX exchange_orders_account ON exchange_orders (account_id, created_at DESC, id DESC);
CREATE INDEX exchange_orders_open ON exchange_orders (seq) WHERE status = 'open';

-- The public tape (pruned after EXCHANGE_TRADE_RETENTION).
CREATE TABLE exchange_trades (
  id uuid PRIMARY KEY,
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  px numeric NOT NULL,
  sz numeric NOT NULL,
  side text NOT NULL CHECK (side IN ('buy', 'sell')), -- taker side
  buyer_id uuid NOT NULL,
  seller_id uuid NOT NULL,
  buyer_sim boolean NOT NULL DEFAULT false,
  seller_sim boolean NOT NULL DEFAULT false,
  liquidation boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL
);
CREATE INDEX exchange_trades_market ON exchange_trades (market_id, created_at DESC, id DESC);
CREATE INDEX exchange_trades_created ON exchange_trades (created_at);

-- One side of a trade, private to the account (users only).
CREATE TABLE exchange_fills (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  order_id uuid NOT NULL,
  trade_id uuid NOT NULL,
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  side text NOT NULL CHECK (side IN ('buy', 'sell')),
  px numeric NOT NULL,
  sz numeric NOT NULL,
  fee numeric NOT NULL,
  realized_pnl numeric NOT NULL,
  liquidity text NOT NULL CHECK (liquidity IN ('maker', 'taker')),
  liquidation boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL
);
CREATE INDEX exchange_fills_account ON exchange_fills (account_id, created_at DESC, id DESC);

-- Every balance change of a user account.
CREATE TABLE exchange_ledger (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  kind text NOT NULL CHECK (kind IN ('deposit', 'withdrawal', 'faucet', 'fee', 'realized_pnl', 'funding', 'liquidation', 'adjustment')),
  amount numeric NOT NULL,
  market_id uuid REFERENCES exchange_markets(id),
  note text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL
);
CREATE INDEX exchange_ledger_account ON exchange_ledger (account_id, created_at DESC, id DESC);

CREATE TABLE exchange_funding_payments (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  rate numeric NOT NULL,                        -- per hour
  sz numeric NOT NULL,
  payment numeric NOT NULL,
  created_at timestamptz NOT NULL
);
CREATE INDEX exchange_funding_payments_account ON exchange_funding_payments (account_id, created_at DESC, id DESC);

CREATE TABLE exchange_funding_rates (
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  at timestamptz NOT NULL,
  rate numeric NOT NULL,
  premium numeric NOT NULL,
  index_px numeric NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (market_id, at)
);

CREATE TABLE exchange_liquidations (
  id uuid PRIMARY KEY,
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  sz numeric NOT NULL,                          -- signed position closed
  px numeric NOT NULL,
  book_sz numeric NOT NULL,
  backstop_sz numeric NOT NULL,
  bad_debt numeric NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL
);
CREATE INDEX exchange_liquidations_account ON exchange_liquidations (account_id, created_at DESC);

-- 1m candles; longer intervals aggregate them.
CREATE TABLE exchange_candles (
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  t bigint NOT NULL,
  o numeric NOT NULL,
  h numeric NOT NULL,
  l numeric NOT NULL,
  c numeric NOT NULL,
  v numeric NOT NULL DEFAULT 0,
  v_quote numeric NOT NULL DEFAULT 0,
  v_sim numeric NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (market_id, t)
);

-- Daily history from the price feed, shown on 1d charts before a market's first live day.
CREATE TABLE exchange_candle_backfill (
  market_id uuid NOT NULL REFERENCES exchange_markets(id),
  t bigint NOT NULL,
  o numeric NOT NULL,
  h numeric NOT NULL,
  l numeric NOT NULL,
  c numeric NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (market_id, t)
);

-- Collateral in and out (exchange.Deposit / exchange.Withdraw); the engine settles pending rows.
CREATE TABLE exchange_transfers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  account_id uuid REFERENCES exchange_accounts(id),
  kind text NOT NULL CHECK (kind IN ('deposit', 'withdrawal', 'adjustment')),
  amount numeric NOT NULL CHECK (amount <> 0),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'rejected')),
  reason text NOT NULL DEFAULT '',
  idempotency_key text NOT NULL UNIQUE,
  note text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  settled_at timestamptz
);
CREATE INDEX exchange_transfers_pending ON exchange_transfers (created_at, id) WHERE status = 'pending';
CREATE INDEX exchange_transfers_user ON exchange_transfers (user_id, created_at DESC);
