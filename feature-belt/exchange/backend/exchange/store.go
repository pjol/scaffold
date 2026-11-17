package exchange

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange/engine"
)

// pgStore is the Postgres engine.Store plus the history queries the API
// serves straight from the database (any instance can answer those).
type pgStore struct{ pool *pgxpool.Pool }

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ms(t int64) time.Time { return time.UnixMilli(t).UTC() }

// Commit writes one engine command's changes in a single transaction (one round trip).
func (s *pgStore) Commit(ctx context.Context, b *engine.Batch) error {
	q := &pgx.Batch{}
	queueAccount := func(a *engine.Account) {
		var faucet any
		if a.LastFaucetMs > 0 {
			faucet = ms(a.LastFaucetMs)
		}
		q.Queue(`INSERT INTO exchange_accounts (id, user_id, owner_id, kind, label, status, balance, deposits, last_faucet_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance, deposits = EXCLUDED.deposits,
			  last_faucet_at = EXCLUDED.last_faucet_at, updated_at = now()`,
			a.ID, nullable(a.UserID), nullable(a.OwnerID), a.Kind, a.Label, a.Status, a.Balance.String(), a.Deposits.String(), faucet,
			ms(a.CreatedMs))
	}
	seen := map[string]bool{}
	for _, a := range b.NewAccounts {
		queueAccount(a)
		seen[a.ID] = true
	}
	for id, a := range b.Accounts {
		if !seen[id] {
			queueAccount(a)
		}
	}
	accounts, positions := b.PositionsList()
	for i, p := range positions {
		q.Queue(`INSERT INTO exchange_positions (account_id, market_id, sz, entry_px, leverage, funding_since_open)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (account_id, market_id) DO UPDATE SET sz = EXCLUDED.sz, entry_px = EXCLUDED.entry_px,
			  leverage = EXCLUDED.leverage, funding_since_open = EXCLUDED.funding_since_open, updated_at = now()`,
			accounts[i], p.MarketID, p.Sz.String(), p.EntryPx.String(), p.Leverage, p.FundingSinceOpen.String())
	}
	for _, o := range b.Orders {
		var px any
		if o.Type == engine.TypeLimit {
			px = o.Px.String()
		}
		src := o.Source
		if src == "" {
			src = engine.SourceUser
		}
		q.Queue(`INSERT INTO exchange_orders (id, account_id, market_id, side, type, px, sz, filled_sz, status, reduce_only,
			  post_only, leverage, liquidation, seq, created_at, updated_at, source, client_ref)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
			ON CONFLICT (id) DO UPDATE SET filled_sz = EXCLUDED.filled_sz, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			o.ID, o.AccountID, o.MarketID, o.Side, o.Type, px, o.Sz.String(), o.Filled.String(), o.Status, o.ReduceOnly,
			o.PostOnly, o.Leverage, o.Liquidation, o.Seq, ms(o.CreatedMs), ms(o.UpdatedMs), src, nullable(o.ClientRef))
	}
	for _, t := range b.Trades {
		q.Queue(`INSERT INTO exchange_trades (id, market_id, px, sz, side, buyer_id, seller_id, buyer_sim, seller_sim, liquidation, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			t.ID, t.MarketID, t.Px.String(), t.Sz.String(), t.Side, t.BuyerID, t.SellerID, t.BuyerSim, t.SellerSim, t.Liquidation, ms(t.Ts))
	}
	for _, f := range b.Fills {
		q.Queue(`INSERT INTO exchange_fills (id, account_id, order_id, trade_id, market_id, side, px, sz, fee, realized_pnl,
			  liquidity, liquidation, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			f.ID, f.AccountID, f.OrderID, f.TradeID, f.MarketID, f.Side, f.Px.String(), f.Sz.String(), f.Fee.String(),
			f.RealizedPnl.String(), f.Liquidity, f.Liquidation, ms(f.Ts))
	}
	for _, l := range b.Ledger {
		q.Queue(`INSERT INTO exchange_ledger (id, account_id, kind, amount, market_id, note, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			l.ID, l.AccountID, l.Kind, l.Amount.String(), nullable(l.MarketID), l.Note, ms(l.Ts))
	}
	for _, f := range b.Funding {
		q.Queue(`INSERT INTO exchange_funding_payments (id, account_id, market_id, rate, sz, payment, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			f.ID, f.AccountID, f.MarketID, f.Rate.String(), f.Sz.String(), f.Payment.String(), ms(f.Ts))
	}
	for _, f := range b.FundingRates {
		q.Queue(`INSERT INTO exchange_funding_rates (market_id, at, rate, premium, index_px) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT DO NOTHING`, f.MarketID, ms(f.Ts), f.Rate.String(), f.Premium.String(), f.Index.String())
	}
	for _, l := range b.Liquidations {
		q.Queue(`INSERT INTO exchange_liquidations (id, account_id, market_id, sz, px, book_sz, backstop_sz, bad_debt, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			l.ID, l.AccountID, l.MarketID, l.Sz.String(), l.Px.String(), l.BookSz.String(), l.BackstopSz.String(), l.BadDebt.String(), ms(l.Ts))
	}
	for _, c := range b.Candles {
		q.Queue(`INSERT INTO exchange_candles (market_id, t, o, h, l, c, v, v_quote, v_sim) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (market_id, t) DO UPDATE SET o = EXCLUDED.o, h = EXCLUDED.h, l = EXCLUDED.l, c = EXCLUDED.c,
			  v = EXCLUDED.v, v_quote = EXCLUDED.v_quote, v_sim = EXCLUDED.v_sim`,
			c.MarketID, c.T, c.O.String(), c.H.String(), c.L.String(), c.C.String(), c.V.String(), c.VQuote.String(), c.VSim.String())
	}
	for _, t := range b.Transfers {
		q.Queue(`UPDATE exchange_transfers SET status = $2, reason = $3, account_id = $4, settled_at = $5
			WHERE id = $1 AND status = 'pending'`, t.ID, t.Status, t.Reason, nullable(t.AccountID), ms(t.Ts))
	}
	for _, id := range b.Closed {
		q.Queue(`UPDATE exchange_accounts SET status = 'closed', user_id = NULL, updated_at = now() WHERE id = $1`, id)
	}
	if q.Len() == 0 {
		return nil
	}
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.SendBatch(ctx, q).Close(); err != nil {
			return fmt.Errorf("exchange commit: %w", err)
		}
		return nil
	})
}

// Load rebuilds the engine snapshot.
func (s *pgStore) Load(ctx context.Context) (*engine.Snapshot, error) {
	snap := &engine.Snapshot{}
	byID := map[string]*engine.Account{}
	rows, err := s.pool.Query(ctx, `SELECT id::text, COALESCE(user_id::text, ''), COALESCE(owner_id::text, ''), kind, label, status,
		  balance::text, deposits::text,
		  COALESCE((extract(epoch FROM last_faucet_at) * 1000)::bigint, 0), (extract(epoch FROM created_at) * 1000)::bigint
		FROM exchange_accounts WHERE status <> 'closed'`)
	if err != nil {
		return nil, fmt.Errorf("load accounts: %w", err)
	}
	for rows.Next() {
		a := &engine.Account{Positions: map[string]*engine.Position{}}
		var bal, dep string
		if err := rows.Scan(&a.ID, &a.UserID, &a.OwnerID, &a.Kind, &a.Label, &a.Status, &bal, &dep, &a.LastFaucetMs, &a.CreatedMs); err != nil {
			rows.Close()
			return nil, err
		}
		if a.Status == engine.AccountClosing {
			a.Status = engine.AccountActive // the close-out runs once the engine is up
		}
		a.Balance, a.Deposits = dec(bal), dec(dep)
		byID[a.ID] = a
		snap.Accounts = append(snap.Accounts, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.pool.Query(ctx, `SELECT p.account_id::text, m.symbol, p.market_id::text, p.sz::text, p.entry_px::text, p.leverage,
		  p.funding_since_open::text
		FROM exchange_positions p JOIN exchange_markets m ON m.id = p.market_id`)
	if err != nil {
		return nil, fmt.Errorf("load positions: %w", err)
	}
	for rows.Next() {
		var aid, sz, entry, fso string
		p := &engine.Position{}
		if err := rows.Scan(&aid, &p.Symbol, &p.MarketID, &sz, &entry, &p.Leverage, &fso); err != nil {
			rows.Close()
			return nil, err
		}
		p.Sz, p.EntryPx, p.FundingSinceOpen = dec(sz), dec(entry), dec(fso)
		if a := byID[aid]; a != nil {
			a.Positions[p.Symbol] = p
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// latest fill per account in the last 24h (24h trader counts)
	rows, err = s.pool.Query(ctx, `SELECT id::text, (extract(epoch FROM max(at)) * 1000)::bigint FROM (
			SELECT buyer_id AS id, created_at AS at FROM exchange_trades WHERE created_at > now() - interval '24 hours'
			UNION ALL SELECT seller_id, created_at FROM exchange_trades WHERE created_at > now() - interval '24 hours') x
		GROUP BY id`)
	if err != nil {
		return nil, fmt.Errorf("load last trades: %w", err)
	}
	for rows.Next() {
		var id string
		var t int64
		if err := rows.Scan(&id, &t); err != nil {
			rows.Close()
			return nil, err
		}
		if a := byID[id]; a != nil {
			a.LastTradeMs = t
		}
	}
	rows.Close()

	orders, err := s.queryOrders(ctx, `WHERE o.status = 'open' ORDER BY o.seq`)
	if err != nil {
		return nil, fmt.Errorf("load orders: %w", err)
	}
	snap.OpenOrders = orders
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM exchange_orders`).Scan(&snap.MaxSeq); err != nil {
		return nil, err
	}

	rows, err = s.pool.Query(ctx, `SELECT t.id::text, m.symbol, t.market_id::text, t.px::text, t.sz::text, t.side, t.buyer_id::text,
		  t.seller_id::text, t.buyer_sim, t.seller_sim, t.liquidation, (extract(epoch FROM t.created_at) * 1000)::bigint
		FROM exchange_markets m CROSS JOIN LATERAL (
		  SELECT * FROM exchange_trades WHERE market_id = m.id ORDER BY created_at DESC, id DESC LIMIT 200) t
		ORDER BY t.created_at, t.id`)
	if err != nil {
		return nil, fmt.Errorf("load trades: %w", err)
	}
	for rows.Next() {
		var t engine.TradeRec
		var px, sz string
		if err := rows.Scan(&t.ID, &t.Symbol, &t.MarketID, &px, &sz, &t.Side, &t.BuyerID, &t.SellerID, &t.BuyerSim, &t.SellerSim,
			&t.Liquidation, &t.Ts); err != nil {
			rows.Close()
			return nil, err
		}
		t.Px, t.Sz = dec(px), dec(sz)
		snap.Trades = append(snap.Trades, t)
	}
	rows.Close()

	rows, err = s.pool.Query(ctx, `SELECT m.symbol, c.market_id::text, c.t, c.o::text, c.h::text, c.l::text, c.c::text, c.v::text,
		  c.v_quote::text, c.v_sim::text
		FROM exchange_markets m CROSS JOIN LATERAL (
		  SELECT * FROM exchange_candles WHERE market_id = m.id ORDER BY t DESC LIMIT 1500) c
		ORDER BY m.symbol, c.t`)
	if err != nil {
		return nil, fmt.Errorf("load candles: %w", err)
	}
	for rows.Next() {
		c, err := scanCandle(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		snap.Candles = append(snap.Candles, c)
	}
	rows.Close()
	return snap, rows.Err()
}

func scanCandle(rows pgx.Rows) (engine.CandleRec, error) {
	var c engine.CandleRec
	var o, h, l, cl, v, vq, vs string
	if err := rows.Scan(&c.Symbol, &c.MarketID, &c.T, &o, &h, &l, &cl, &v, &vq, &vs); err != nil {
		return c, err
	}
	c.O, c.H, c.L, c.C, c.V, c.VQuote, c.VSim = dec(o), dec(h), dec(l), dec(cl), dec(v), dec(vq), dec(vs)
	return c, nil
}

func (s *pgStore) queryOrders(ctx context.Context, where string, args ...any) ([]*engine.Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id::text, o.account_id::text, m.symbol, o.market_id::text, o.side, o.type,
		  COALESCE(o.px, 0)::text, o.sz::text, o.filled_sz::text, o.status, o.reduce_only, o.post_only, o.leverage, o.liquidation,
		  o.seq, (extract(epoch FROM o.created_at) * 1000)::bigint, (extract(epoch FROM o.updated_at) * 1000)::bigint,
		  o.source, COALESCE(o.client_ref, '')
		FROM exchange_orders o JOIN exchange_markets m ON m.id = o.market_id `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*engine.Order
	for rows.Next() {
		o := &engine.Order{}
		var px, sz, filled string
		if err := rows.Scan(&o.ID, &o.AccountID, &o.Symbol, &o.MarketID, &o.Side, &o.Type, &px, &sz, &filled, &o.Status, &o.ReduceOnly,
			&o.PostOnly, &o.Leverage, &o.Liquidation, &o.Seq, &o.CreatedMs, &o.UpdatedMs, &o.Source, &o.ClientRef); err != nil {
			return nil, err
		}
		o.Px, o.Sz, o.Filled = dec(px), dec(sz), dec(filled)
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- history (keyset pages, newest first) ----

// histCursor is the keyset position of a history page.
type histCursor struct {
	T  time.Time `json:"t"`
	ID string    `json:"id"`
}

func (s *pgStore) Orders(ctx context.Context, accountID, status string, c histCursor, limit int) ([]*engine.Order, error) {
	where := `WHERE o.account_id = $1`
	args := []any{accountID}
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(` AND o.status = $%d`, len(args))
	}
	if c.ID != "" {
		args = append(args, c.T, c.ID)
		where += fmt.Sprintf(` AND (o.created_at, o.id) < ($%d, $%d::uuid)`, len(args)-1, len(args))
	}
	args = append(args, limit)
	return s.queryOrders(ctx, where+fmt.Sprintf(` ORDER BY o.created_at DESC, o.id DESC LIMIT $%d`, len(args)), args...)
}

func (s *pgStore) Fills(ctx context.Context, accountID string, c histCursor, limit int) ([]engine.FillRec, error) {
	args := []any{accountID}
	where := `f.account_id = $1`
	if c.ID != "" {
		args = append(args, c.T, c.ID)
		where += ` AND (f.created_at, f.id) < ($2, $3::uuid)`
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT f.id::text, f.order_id::text, f.trade_id::text, m.symbol, f.market_id::text, f.side,
		  f.px::text, f.sz::text, f.fee::text, f.realized_pnl::text, f.liquidity, f.liquidation, (extract(epoch FROM f.created_at) * 1000)::bigint
		FROM exchange_fills f JOIN exchange_markets m ON m.id = f.market_id
		WHERE %s ORDER BY f.created_at DESC, f.id DESC LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.FillRec{}
	for rows.Next() {
		var f engine.FillRec
		var px, sz, fee, rp string
		if err := rows.Scan(&f.ID, &f.OrderID, &f.TradeID, &f.Symbol, &f.MarketID, &f.Side, &px, &sz, &fee, &rp, &f.Liquidity,
			&f.Liquidation, &f.Ts); err != nil {
			return nil, err
		}
		f.AccountID = accountID
		f.Px, f.Sz, f.Fee, f.RealizedPnl = dec(px), dec(sz), dec(fee), dec(rp)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *pgStore) Funding(ctx context.Context, accountID string, c histCursor, limit int) ([]engine.FundingRec, error) {
	args := []any{accountID}
	where := `f.account_id = $1`
	if c.ID != "" {
		args = append(args, c.T, c.ID)
		where += ` AND (f.created_at, f.id) < ($2, $3::uuid)`
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT f.id::text, m.symbol, f.market_id::text, (extract(epoch FROM f.created_at) * 1000)::bigint,
		  f.rate::text, f.sz::text, f.payment::text
		FROM exchange_funding_payments f JOIN exchange_markets m ON m.id = f.market_id
		WHERE %s ORDER BY f.created_at DESC, f.id DESC LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.FundingRec{}
	for rows.Next() {
		var f engine.FundingRec
		var rate, sz, pay string
		if err := rows.Scan(&f.ID, &f.Symbol, &f.MarketID, &f.Ts, &rate, &sz, &pay); err != nil {
			return nil, err
		}
		f.AccountID = accountID
		f.Rate, f.Sz, f.Payment = dec(rate), dec(sz), dec(pay)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *pgStore) Ledger(ctx context.Context, accountID string, c histCursor, limit int, quoteDecimals int32) ([]engine.LedgerEntry, error) {
	args := []any{accountID}
	where := `l.account_id = $1`
	if c.ID != "" {
		args = append(args, c.T, c.ID)
		where += ` AND (l.created_at, l.id) < ($2, $3::uuid)`
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT l.id::text, l.kind, l.amount::text, COALESCE(m.symbol, ''), l.note, l.created_at
		FROM exchange_ledger l LEFT JOIN exchange_markets m ON m.id = l.market_id
		WHERE %s ORDER BY l.created_at DESC, l.id DESC LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.LedgerEntry{}
	for rows.Next() {
		var l engine.LedgerEntry
		var amt string
		if err := rows.Scan(&l.ID, &l.Kind, &amt, &l.Symbol, &l.Note, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Amount = dec(amt).StringFixed(quoteDecimals + 2)
		out = append(out, l)
	}
	return out, rows.Err()
}

// Candles aggregates persisted 1m candles into interval buckets that open
// before beforeMs, newest limit buckets, returned oldest first.
func (s *pgStore) Candles(ctx context.Context, marketID, symbol string, ivMs, beforeMs int64, limit int) ([]engine.CandleRec, error) {
	from := (beforeMs/ivMs - int64(limit)) * ivMs
	rows, err := s.pool.Query(ctx, `SELECT $5::text, market_id::text, (t / $2) * $2 AS b,
		  ((array_agg(o ORDER BY t))[1])::text, max(h)::text, min(l)::text, ((array_agg(c ORDER BY t DESC))[1])::text,
		  sum(v)::text, sum(v_quote)::text, sum(v_sim)::text
		FROM exchange_candles WHERE market_id = $1 AND t >= $3 AND t < $4
		GROUP BY market_id, b ORDER BY b DESC LIMIT $6`, marketID, ivMs, from, beforeMs, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.CandleRec
	for rows.Next() {
		c, err := scanCandle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	slices.Reverse(out)
	return out, rows.Err()
}

// EarliestCandle is the open time of a market's first 1m candle (ok=false if none).
func (s *pgStore) EarliestCandle(ctx context.Context, marketID string) (int64, bool, error) {
	var t *int64
	if err := s.pool.QueryRow(ctx, `SELECT min(t) FROM exchange_candles WHERE market_id = $1`, marketID).Scan(&t); err != nil {
		return 0, false, err
	}
	if t == nil {
		return 0, false, nil
	}
	return *t, true, nil
}

// Backfill returns up to limit daily backfill bars strictly before beforeMs, oldest first.
func (s *pgStore) Backfill(ctx context.Context, marketID string, beforeMs int64, limit int) ([]engine.CandleRec, error) {
	rows, err := s.pool.Query(ctx, `SELECT '', market_id::text, t, o::text, h::text, l::text, c::text, '0', '0', '0'
		FROM exchange_candle_backfill WHERE market_id = $1 AND t < $2 ORDER BY t DESC LIMIT $3`, marketID, beforeMs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.CandleRec
	for rows.Next() {
		c, err := scanCandle(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	slices.Reverse(out)
	return out, rows.Err()
}

// ReplaceBackfill makes bars a market's entire daily backfill.
func (s *pgStore) ReplaceBackfill(ctx context.Context, marketID string, bars []DailyBar) error {
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM exchange_candle_backfill WHERE market_id = $1`, marketID); err != nil {
			return err
		}
		q := &pgx.Batch{}
		for _, b := range bars {
			q.Queue(`INSERT INTO exchange_candle_backfill (market_id, t, o, h, l, c) VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (market_id, t) DO UPDATE SET o = EXCLUDED.o, h = EXCLUDED.h, l = EXCLUDED.l, c = EXCLUDED.c`,
				marketID, b.Day.UnixMilli(), b.O.String(), b.H.String(), b.L.String(), b.C.String())
		}
		if q.Len() == 0 {
			return nil
		}
		return tx.SendBatch(ctx, q).Close()
	})
}
