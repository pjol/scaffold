-- Copy trading (feature-belt/copytrade): leaders, copies, their activity and
-- daily account snapshots for leader statistics. Amounts are numeric in the
-- exchange's quote currency.

-- What a leader offers. eligible is refreshed every few minutes.
CREATE TABLE copytrade_leaders (
  user_id uuid PRIMARY KEY REFERENCES users(id),
  enabled boolean NOT NULL DEFAULT false,
  profit_share_pct numeric NOT NULL DEFAULT 0 CHECK (profit_share_pct >= 0 AND profit_share_pct <= 50),
  min_allocation numeric NOT NULL DEFAULT 50 CHECK (min_allocation > 0),
  max_copiers int NOT NULL DEFAULT 100 CHECK (max_copiers >= 1),
  show_copiers boolean NOT NULL DEFAULT false,
  eligible boolean NOT NULL DEFAULT false,
  checked_at timestamptz,
  enabled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX copytrade_leaders_enabled ON copytrade_leaders (user_id) WHERE enabled;

-- One follower copying one leader through a sub-account of the follower's
-- (exchange_accounts kind 'sub', purpose 'copy:<id>').
CREATE TABLE copytrade_copies (
  id uuid PRIMARY KEY,
  follower_id uuid NOT NULL REFERENCES users(id),
  leader_id uuid NOT NULL REFERENCES users(id),
  account_id uuid NOT NULL REFERENCES exchange_accounts(id),
  status text NOT NULL CHECK (status IN ('active', 'paused', 'stopping', 'stopped')),
  stop_reason text NOT NULL DEFAULT '',
  pause_reason text NOT NULL DEFAULT '',
  settings jsonb NOT NULL,
  net_deposits numeric NOT NULL,        -- moved in minus moved out
  gross_deposits numeric NOT NULL,      -- everything moved in (the base of pnl_pct)
  peak_equity numeric NOT NULL,         -- for the drawdown stop
  equity numeric NOT NULL,              -- last recorded (leader AUM); live values come from the engine
  profit_share numeric NOT NULL DEFAULT 0, -- fraction fixed when the copy started
  hwm_gain numeric NOT NULL DEFAULT 0,
  profit_share_paid numeric NOT NULL DEFAULT 0,
  last_settled_at timestamptz NOT NULL,
  ignored jsonb NOT NULL DEFAULT '{}',  -- the leader's positions at the start, not mirrored until flat or flipped: symbol → sign
  close_on_stop boolean NOT NULL DEFAULT true,
  identity_visible boolean NOT NULL DEFAULT false, -- the leader showed copiers when this copy started
  disclosure_version text NOT NULL,
  disclosure_accepted_at timestamptz NOT NULL,
  account_closed_at timestamptz,        -- the sub-account was swept and closed
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  stopped_at timestamptz
);
CREATE UNIQUE INDEX copytrade_copies_active_pair ON copytrade_copies (follower_id, leader_id) WHERE status <> 'stopped';
CREATE INDEX copytrade_copies_follower ON copytrade_copies (follower_id, created_at DESC, id DESC);
CREATE INDEX copytrade_copies_leader ON copytrade_copies (leader_id, created_at DESC, id DESC) WHERE status <> 'stopped';
CREATE INDEX copytrade_copies_all ON copytrade_copies (created_at DESC, id DESC);
CREATE INDEX copytrade_copies_unswept ON copytrade_copies (stopped_at) WHERE status = 'stopped' AND account_closed_at IS NULL;

-- A copy's activity: orders, skips, transfers, stops (pruned after COPYTRADE_EVENT_RETENTION).
CREATE TABLE copytrade_events (
  id uuid PRIMARY KEY,
  copy_id uuid NOT NULL REFERENCES copytrade_copies(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK (kind IN ('start', 'order', 'skip', 'stop', 'transfer', 'profit_share', 'reconcile', 'pause', 'resume', 'settings')),
  symbol text NOT NULL DEFAULT '',
  side text NOT NULL DEFAULT '',
  sz numeric,
  notional numeric,
  order_id uuid,
  leader_fill_id uuid,
  skip_class text NOT NULL DEFAULT '' CHECK (skip_class IN ('', 'too_small', 'risk_limit', 'rejected', 'slippage')),
  reason text NOT NULL,
  latency_ms bigint,
  created_at timestamptz NOT NULL
);
CREATE INDEX copytrade_events_copy ON copytrade_events (copy_id, created_at DESC, id DESC);
CREATE INDEX copytrade_events_created ON copytrade_events (created_at);

-- One row per trader per day: main-account value and net deposits (leader
-- returns and drawdown; pruned after 120 days).
CREATE TABLE copytrade_snapshots (
  user_id uuid NOT NULL REFERENCES users(id),
  day date NOT NULL,
  account_value numeric NOT NULL,
  deposits numeric NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, day)
);
