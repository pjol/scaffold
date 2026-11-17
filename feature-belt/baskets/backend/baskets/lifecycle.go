package baskets

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
)

// Lifecycle, hourly and once across instances (jobs.Every):
//
//   - usage: people's volume over BASKETS_IDLE_DAYS, open interest and
//     holders (exchange.UsageOf), and popularity = that volume + 25 × the
//     entries recorded over the same window;
//   - active, no holders and no volume, at least BASKETS_IDLE_DAYS after it
//     became active (and not before its end date) → archived: market halted,
//     oracle feed off; its page and history stay;
//   - active with an end date, BASKETS_END_GRACE_DAYS after it → reduce_only
//     while people hold positions, archived otherwise;
//   - hidden by staff and still active → the same wind-down;
//   - reduce_only with no holders left → archived.
//
// Creating the same basket again revives an archived one (Create), unless
// staff hid it.

// nextStatus is where a basket should be now ("" = unchanged).
func nextStatus(status string, statusAt time.Time, endsOn *time.Time, hidden bool, holders int, volume decimal.Decimal,
	now time.Time, idle, grace time.Duration) string {
	windDown := func() string {
		if holders > 0 {
			return StatusReduceOnly
		}
		return StatusArchived
	}
	switch status {
	case StatusReduceOnly:
		if holders == 0 {
			return StatusArchived
		}
		return ""
	case StatusActive:
	default:
		return ""
	}
	if hidden {
		return windDown()
	}
	idleFrom := statusAt.Add(idle)
	if endsOn != nil {
		if !now.Before(endsOn.Add(grace)) {
			return windDown()
		}
		if endsOn.After(idleFrom) {
			idleFrom = *endsOn
		}
	}
	if holders == 0 && !volume.IsPositive() && !now.Before(idleFrom) {
		return StatusArchived
	}
	return ""
}

func (s *Service) lifecycle(ctx context.Context) error {
	now := time.Now()
	since := now.Add(-s.cfg.idle)
	rows, err := s.d.Pool.Query(ctx, `SELECT `+basketColumns+basketFrom+` WHERE b.status <> 'archived' ORDER BY b.created_at`)
	if err != nil {
		return err
	}
	var live []basketRow
	for rows.Next() {
		b, err := scanBasket(rows)
		if err != nil {
			rows.Close()
			return err
		}
		live = append(live, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	recent, err := recentEntries(ctx, s.d.Pool, since)
	if err != nil {
		return err
	}
	var changed, failed int
	for _, b := range live {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		moved, err := s.lifecycleOne(ctx, b, recent[b.ID], since, now)
		switch {
		case err != nil:
			failed++
			s.log.Warn("baskets: lifecycle", "symbol", b.Symbol, "err", err)
		case moved:
			changed++
		}
	}
	if changed+failed > 0 {
		s.log.Info("baskets: lifecycle", "checked", len(live), "changed", changed, "failed", failed)
	}
	if changed > 0 {
		s.markets.clear()
		s.oracle().Reload()
	}
	return nil
}

func recentEntries(ctx context.Context, q db.Querier, since time.Time) (map[uuid.UUID]int, error) {
	rows, err := q.Query(ctx, `SELECT basket_id, count(*) FROM basket_entries WHERE created_at >= $1 GROUP BY basket_id`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// lifecycleOne refreshes one basket's usage and moves it if due.
func (s *Service) lifecycleOne(ctx context.Context, b basketRow, entries int, since, now time.Time) (bool, error) {
	u, err := exchange.UsageOf(ctx, s.d.Pool, b.Symbol, since)
	if errors.Is(err, engine.ErrUnknownMarket) {
		return false, errors.New("its exchange market is gone")
	}
	if err != nil {
		return false, err
	}
	pop := u.Volume.Add(decimal.NewFromInt(int64(entries * entryWeight)))
	to := nextStatus(b.Status, b.StatusAt, b.EndsOn, b.Hidden, u.Holders, u.Volume, now, s.cfg.idle, s.cfg.endGrace)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := basketByLock(ctx, tx, "id", b.ID.String(), true)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE baskets SET volume_recent = $2, open_interest = $3, holders = $4, popularity = $5, usage_at = now() WHERE id = $1`,
			b.ID, u.Volume.String(), u.OpenInterest.String(), u.Holders, pop.String()); err != nil {
			return err
		}
		if to == "" || cur.Status != b.Status { // revived or moved meanwhile: next run decides
			to = ""
			return nil
		}
		return s.setStatus(ctx, tx, cur, to)
	})
	if err != nil || to == "" {
		return false, err
	}
	b.Status = to
	s.publish(ctx, b)
	return true, nil
}
