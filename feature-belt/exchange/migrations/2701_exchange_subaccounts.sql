-- Exchange 1.1.0: people's sub-accounts (copy trading and other modules),
-- internal transfers, order sources and client references.

-- A sub-account belongs to a person (owner_id) but is never their main
-- account: user_id stays the main account's unique link, null for kind 'sub'.
ALTER TABLE exchange_accounts DROP CONSTRAINT exchange_accounts_kind_check;
ALTER TABLE exchange_accounts ADD CONSTRAINT exchange_accounts_kind_check
  CHECK (kind IN ('user', 'sub', 'bot', 'mm', 'backstop', 'fee'));
ALTER TABLE exchange_accounts ADD COLUMN owner_id uuid REFERENCES users(id); -- kind 'sub': the person (null once they delete their account)
ALTER TABLE exchange_accounts ADD CONSTRAINT exchange_accounts_sub_unlinked CHECK (kind <> 'sub' OR user_id IS NULL);
DROP INDEX exchange_accounts_system;
CREATE UNIQUE INDEX exchange_accounts_system ON exchange_accounts (kind, label) WHERE kind NOT IN ('user', 'sub');
CREATE INDEX exchange_accounts_owner ON exchange_accounts (owner_id, created_at) WHERE owner_id IS NOT NULL;

-- Who placed an order (user, agent, copy, liquidation, system or a module's
-- name) and the caller's reference, so a module finds its order after a crash.
ALTER TABLE exchange_orders ADD COLUMN source text NOT NULL DEFAULT 'user';
ALTER TABLE exchange_orders ADD COLUMN client_ref text;
CREATE INDEX exchange_orders_client_ref ON exchange_orders (account_id, client_ref) WHERE client_ref IS NOT NULL;

-- Internal transfers (main ↔ own sub-account) and profit shares (sub-account → another person's main account).
ALTER TABLE exchange_ledger DROP CONSTRAINT exchange_ledger_kind_check;
ALTER TABLE exchange_ledger ADD CONSTRAINT exchange_ledger_kind_check
  CHECK (kind IN ('deposit', 'withdrawal', 'faucet', 'fee', 'realized_pnl', 'funding', 'liquidation', 'adjustment',
                  'transfer', 'profit_share'));
