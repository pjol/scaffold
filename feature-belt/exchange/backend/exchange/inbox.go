package exchange

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/exchange/engine"
)

// inbox applies work other instances and the app leave in the database for
// the engine: collateral transfers, account close-outs and catalog changes.
// It runs after every engine second, on the engine's goroutine.
type inbox struct {
	s        *Service
	eng      *engine.Engine
	pool     *pgxpool.Pool
	synced   time.Time // newest exchange_markets.updated_at applied
	lastSync time.Time
}

func (in *inbox) poll(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := in.transfers(qctx); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "exchange: transfers", "err", err)
	}
	if err := in.closures(qctx); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "exchange: account close-out", "err", err)
	}
	if time.Since(in.lastSync) >= 3*time.Second {
		in.lastSync = time.Now()
		if err := in.markets(qctx); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "exchange: catalog sync", "err", err)
		}
	}
}

func (in *inbox) transfers(ctx context.Context) error {
	rows, err := in.pool.Query(ctx, `SELECT id::text, user_id::text, kind, amount::text, note FROM exchange_transfers
		WHERE status = 'pending' ORDER BY created_at, id LIMIT 200`)
	if err != nil {
		return err
	}
	ts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (engine.Transfer, error) {
		var t engine.Transfer
		var amt string
		err := row.Scan(&t.ID, &t.UserID, &t.Kind, &amt, &t.Note)
		t.Amount = dec(amt)
		return t, err
	})
	if err != nil || len(ts) == 0 {
		return err
	}
	_, err = in.eng.ApplyTransfers(ts)
	return err
}

func (in *inbox) closures(ctx context.Context) error {
	rows, err := in.pool.Query(ctx, `SELECT id::text FROM exchange_accounts WHERE status = 'closing' LIMIT 50`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := in.eng.CloseAccount(id); err != nil {
			if err == engine.ErrNotFound { // not loaded (closed before this engine started)
				_, err = in.pool.Exec(ctx, `UPDATE exchange_accounts SET status = 'closed', user_id = NULL, updated_at = now() WHERE id = $1`, id)
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (in *inbox) markets(ctx context.Context) error {
	var latest *time.Time
	if err := in.pool.QueryRow(ctx, `SELECT max(updated_at) FROM exchange_markets`).Scan(&latest); err != nil {
		return err
	}
	if latest == nil || !latest.After(in.synced) {
		return nil
	}
	specs, synced, err := loadSpecs(ctx, in.pool)
	if err != nil {
		return err
	}
	in.eng.SyncMarkets(specs)
	in.synced = synced
	return nil
}
