package exchange

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

// ---- collateral (the app's money in and out) ----

// Deposit credits collateral to a user's trading account (creating it). It
// only records the transfer: the engine settles it within a second. Run it in
// the same transaction as whatever paid for it, with an idempotency key that
// transaction owns (a payment id), so a retry never credits twice. Returns
// the transfer id.
func Deposit(ctx context.Context, q db.Querier, userID uuid.UUID, amount decimal.Decimal, idempotencyKey, note string) (string, error) {
	if !amount.IsPositive() {
		return "", fmt.Errorf("exchange: deposit amount must be positive")
	}
	return insertTransfer(ctx, q, userID, "deposit", amount, idempotencyKey, note)
}

// Withdraw asks for collateral out of a user's account. The engine applies it
// only if it leaves the account's free collateral at or above zero, else
// rejects it with a reason; read the outcome with TransferStatus before paying
// anything out.
func Withdraw(ctx context.Context, q db.Querier, userID uuid.UUID, amount decimal.Decimal, idempotencyKey, note string) (string, error) {
	if !amount.IsPositive() {
		return "", fmt.Errorf("exchange: withdrawal amount must be positive")
	}
	return insertTransfer(ctx, q, userID, "withdrawal", amount, idempotencyKey, note)
}

func insertTransfer(ctx context.Context, q db.Querier, userID uuid.UUID, kind string, amount decimal.Decimal, key, note string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `
		INSERT INTO exchange_transfers (user_id, kind, amount, idempotency_key, note) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING id::text`, userID, kind, amount.String(), key, note).Scan(&id)
	return id, err
}

// TransferStatus reports a transfer's state: pending, applied or rejected
// (with the reason).
func TransferStatus(ctx context.Context, q db.Querier, transferID string) (status, reason string, err error) {
	err = q.QueryRow(ctx, `SELECT status, reason FROM exchange_transfers WHERE id = $1`, transferID).Scan(&status, &reason)
	return status, reason, err
}

// ---- account deletion and export ----

// DeleteAccount unlinks the user's trading account and their sub-accounts
// and marks them closing: the engine then cancels their orders, hands their
// positions and balances to the backstop and closes them. Their orders,
// fills and ledger stay as anonymous financial records. Pending transfers are
// rejected.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE exchange_accounts SET status = 'closing', user_id = NULL, updated_at = now()
		WHERE user_id = $1 AND status = 'active'`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE exchange_accounts SET status = CASE WHEN status = 'closed' THEN status ELSE 'closing' END,
		  owner_id = NULL, updated_at = now()
		WHERE owner_id = $1 AND kind = 'sub'`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE exchange_transfers SET status = 'rejected', reason = 'The account was deleted.', settled_at = now()
		WHERE user_id = $1 AND status = 'pending'`, userID)
	return err
}

// AccountDeleted closes the accounts out right away when this instance runs
// the engine (otherwise the engine's inbox does it within a second).
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	eng := s.engine()
	if eng == nil {
		return nil
	}
	for _, sub := range eng.Subaccounts(userID.String()) {
		if err := eng.CloseAccount(sub.ID); err != nil {
			return err
		}
	}
	if id, ok := eng.AccountForUser(userID.String()); ok {
		return eng.CloseAccount(id)
	}
	return nil
}

// ExportAccount returns the user's trading account, positions, orders,
// fills, funding payments, ledger and transfers, and the same for each of
// their sub-accounts.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out, err := s.exportMain(ctx, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id::text, label, status, balance::text, deposits::text, created_at FROM exchange_accounts
		WHERE owner_id = $1 AND kind = 'sub' ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	type sub struct {
		ID        string    `json:"id"`
		Purpose   string    `json:"purpose"`
		Status    string    `json:"status"`
		Balance   string    `json:"balance"`
		Deposits  string    `json:"deposits"`
		CreatedAt time.Time `json:"created_at"`
	}
	subs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (sub, error) {
		var x sub
		return x, r.Scan(&x.ID, &x.Purpose, &x.Status, &x.Balance, &x.Deposits, &x.CreatedAt)
	})
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(subs))
	for _, x := range subs {
		m := map[string]any{"account": x}
		if err := s.exportHistory(ctx, x.ID, m); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	out["subaccounts"] = list
	return out, nil
}

func (s *Service) exportMain(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	var acct struct {
		ID        string    `json:"id"`
		Status    string    `json:"status"`
		Balance   string    `json:"balance"`
		Deposits  string    `json:"deposits"`
		CreatedAt time.Time `json:"created_at"`
	}
	err := s.d.Pool.QueryRow(ctx, `SELECT id::text, status, balance::text, deposits::text, created_at FROM exchange_accounts
		WHERE user_id = $1`, userID).Scan(&acct.ID, &acct.Status, &acct.Balance, &acct.Deposits, &acct.CreatedAt)
	if db.IsNoRows(err) {
		return map[string]any{"account": nil}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{"account": acct, "quote_currency": s.quoteCurrency}
	if err := s.exportHistory(ctx, acct.ID, out); err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT kind, amount::text, status, reason, note, created_at, settled_at FROM exchange_transfers
		WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("export transfers: %w", err)
	}
	transfers, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		return nil, fmt.Errorf("export transfers: %w", err)
	}
	if transfers == nil {
		transfers = []map[string]any{}
	}
	out["transfers"] = transfers
	return out, nil
}

// exportHistory adds one account's positions, orders, fills, funding and ledger to out.
func (s *Service) exportHistory(ctx context.Context, accountID string, out map[string]any) error {
	queries := map[string]string{
		"positions": `SELECT m.symbol, p.sz::text, p.entry_px::text, p.leverage, p.funding_since_open::text, p.updated_at
			FROM exchange_positions p JOIN exchange_markets m ON m.id = p.market_id WHERE p.account_id = $1 AND p.sz <> 0`,
		"orders": `SELECT o.id::text, m.symbol, o.side, o.type, o.px::text, o.sz::text, o.filled_sz::text, o.status, o.reduce_only,
			  o.post_only, o.leverage, o.liquidation, o.source, o.created_at, o.updated_at
			FROM exchange_orders o JOIN exchange_markets m ON m.id = o.market_id WHERE o.account_id = $1 ORDER BY o.created_at`,
		"fills": `SELECT f.id::text, f.order_id::text, m.symbol, f.side, f.px::text, f.sz::text, f.fee::text, f.realized_pnl::text,
			  f.liquidity, f.liquidation, f.created_at
			FROM exchange_fills f JOIN exchange_markets m ON m.id = f.market_id WHERE f.account_id = $1 ORDER BY f.created_at`,
		"funding": `SELECT m.symbol, f.rate::text, f.sz::text, f.payment::text, f.created_at
			FROM exchange_funding_payments f JOIN exchange_markets m ON m.id = f.market_id WHERE f.account_id = $1 ORDER BY f.created_at`,
		"ledger": `SELECT l.kind, l.amount::text, COALESCE(m.symbol, ''), l.note, l.created_at
			FROM exchange_ledger l LEFT JOIN exchange_markets m ON m.id = l.market_id WHERE l.account_id = $1 ORDER BY l.created_at`,
	}
	for name, q := range queries {
		rows, err := s.d.Pool.Query(ctx, q, accountID)
		if err != nil {
			return fmt.Errorf("export %s: %w", name, err)
		}
		list, err := pgx.CollectRows(rows, pgx.RowToMap)
		if err != nil {
			return fmt.Errorf("export %s: %w", name, err)
		}
		if list == nil {
			list = []map[string]any{}
		}
		out[name] = list
	}
	return nil
}

// ---- development data ----

// devCatalog is the development catalog: a few generic priced assets with
// seed prices (seed prices are used only in development). The metals' feed
// keys match the oracle feature's development feeds, so the two connect once
// stitched (FEATURE.md).
var devCatalog = []CatalogMarket{
	{Symbol: "GOLD", Name: "Gold", Category: "Metals", PriceFeed: "GOLD-OZ", Description: "One troy ounce of gold.", MaxLeverage: 20, SzDecimals: 3,
		TickSize: "0.1", MinNotional: "10", OICap: "2000000", SeedPrice: "2650", Sort: 1},
	{Symbol: "SILVER", Name: "Silver", Category: "Metals", PriceFeed: "SILVER-OZ", Description: "One troy ounce of silver.", MaxLeverage: 10, SzDecimals: 1,
		TickSize: "0.01", MinNotional: "10", OICap: "500000", SeedPrice: "31.2", Sort: 2},
	{Symbol: "OIL", Name: "Crude oil", Category: "Energy", Description: "One barrel of crude oil.", MaxLeverage: 10, SzDecimals: 2,
		TickSize: "0.01", MinNotional: "10", OICap: "1000000", SeedPrice: "72.4", Sort: 3},
	{Symbol: "WHEAT", Name: "Wheat", Category: "Agriculture", Description: "One bushel of wheat.", MaxLeverage: 5, SzDecimals: 1,
		TickSize: "0.0025", MinNotional: "10", OICap: "250000", SeedPrice: "5.8", Sort: 4},
}

// SeedDev lists the development catalog and deposits 10,000 into each dev
// identity's trading account (once; the running engine settles it).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if err := UpsertCatalog(ctx, s.d.Pool, devCatalog); err != nil {
		return err
	}
	for _, u := range users {
		if _, err := Deposit(ctx, s.d.Pool, u, decimal.NewFromInt(10_000), "seed:"+u.String(), "development deposit"); err != nil {
			return err
		}
	}
	return nil
}

// prune deletes public trades older than EXCHANGE_TRADE_RETENTION (accounts'
// fills, orders and ledger are kept; candles keep the price history).
func (s *Service) prune(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM exchange_trades WHERE id IN (
		SELECT id FROM exchange_trades WHERE created_at < $1 LIMIT 50000)`, time.Now().Add(-s.retention))
	return err
}
