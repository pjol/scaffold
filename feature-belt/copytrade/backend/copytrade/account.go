package copytrade

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// adminStop stops a copy as staff (positions closed, funds returned), audited
// in the same transaction as the status change.
func (s *Service) adminStop(w http.ResponseWriter, r *http.Request) error {
	rt, err := s.running()
	if err != nil {
		return err
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return errNotFound
	}
	cs, ok := rt.state(id)
	if !ok {
		return httpx.Conflict("That copy isn't running.")
	}
	staff := auth.MustFromContext(r.Context())
	ctx := context.WithoutCancel(r.Context())
	cs.mu.Lock()
	defer cs.mu.Unlock()
	rec := &cs.rec
	if rec.Status != StatusActive && rec.Status != StatusPaused {
		return httpx.Conflict("That copy is already stopping.")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE copytrade_copies SET status = 'stopping', stop_reason = $2, pause_reason = '',
			close_on_stop = true, updated_at = now() WHERE id = $1 AND status IN ('active', 'paused')`, id, StopStaff)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.Conflict("That copy is already stopping.")
		}
		return audit.Log(ctx, tx, staff.UserID, "copytrade.copy.stop", "copytrade_copy", id.String(),
			map[string]any{"follower_id": rec.FollowerID, "leader_id": rec.LeaderID})
	})
	if err != nil {
		return err
	}
	rec.Status, rec.StopReason, rec.PauseReason, rec.CloseOnStop = StatusStopping, StopStaff, "", true
	e := rt.newEvent(cs, EventStop, "")
	e.Reason = stopText(StopStaff, true)
	rt.record(ctx, cs, []eventRec{e})
	if err := rt.stopLocked(ctx, cs, StopStaff, true); err != nil {
		return apiErr(err)
	}
	httpx.NoContent(w)
	return nil
}

// ---- account deletion and export ----

// DeleteAccount removes the person's leader settings, snapshots and copies
// (with their events); the exchange closes their copy accounts with the rest
// of their trading accounts. Copies of the person as a leader stop within a
// minute (or at once on the engine's instance).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM copytrade_leaders WHERE user_id = $1`,
		`DELETE FROM copytrade_snapshots WHERE user_id = $1`,
		`DELETE FROM copytrade_copies WHERE follower_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// AccountDeleted updates the running pipeline: the person's own copies are
// forgotten and copies of them as a leader stop.
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	rt := s.rt.Load()
	if rt == nil {
		return nil
	}
	rt.forgetFollower(userID)
	if len(rt.copiesOf(userID)) > 0 {
		bg.Go(ctx, "copytrade.leader_left", func(ctx context.Context) error {
			rt.stopAllOf(ctx, userID, StopLeaderLeft)
			return nil
		})
	}
	s.invalidateLeaders()
	return nil
}

// ExportAccount returns the person's leader settings, their copies with
// every event, and their daily account snapshots.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	out := map[string]any{}
	lr, ok, err := getLeader(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	if ok {
		out["leader"] = lr.settings()
	} else {
		out["leader"] = nil
	}
	recs, err := listCopies(ctx, s.d.Pool, `WHERE follower_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	copies := make([]map[string]any, 0, len(recs))
	for _, rec := range recs {
		rows, err := s.d.Pool.Query(ctx, `SELECT kind, symbol, side, sz::text, notional::text, skip_class, reason, created_at
			FROM copytrade_events WHERE copy_id = $1 ORDER BY created_at`, rec.ID)
		if err != nil {
			return nil, err
		}
		events, err := pgx.CollectRows(rows, pgx.RowToMap)
		if err != nil {
			return nil, fmt.Errorf("export copy events: %w", err)
		}
		copies = append(copies, map[string]any{
			"id": rec.ID, "leader_id": rec.LeaderID, "status": rec.Status, "stop_reason": rec.StopReason, "settings": rec.Settings,
			"net_deposits": rec.NetDeposits.String(), "profit_share_paid": rec.ProfitSharePaid.String(),
			"disclosure_version": rec.DisclosureVersion, "disclosure_accepted_at": rec.DisclosureAt,
			"created_at": rec.CreatedAt, "stopped_at": rec.StoppedAt, "events": events,
		})
	}
	out["copies"] = copies
	rows, err := s.d.Pool.Query(ctx, `SELECT day, account_value::text, deposits::text FROM copytrade_snapshots WHERE user_id = $1 ORDER BY day`, userID)
	if err != nil {
		return nil, err
	}
	snaps, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		return nil, err
	}
	out["snapshots"] = snaps
	return out, nil
}

// SeedDev lets every development identity but the first lead (so the first
// can browse leaders and copy them once they trade). Idempotent.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	for i, u := range users {
		if i == 0 {
			continue
		}
		if _, err := s.d.Pool.Exec(ctx, `INSERT INTO copytrade_leaders (user_id, enabled, profit_share_pct, min_allocation, max_copiers,
			  show_copiers, eligible, checked_at, enabled_at)
			VALUES ($1, true, $2, $3, 100, $4, true, now(), now()) ON CONFLICT (user_id) DO NOTHING`,
			u, []string{"0", "10", "5"}[i%3], s.minAllocation.Mul(decimal.NewFromInt(int64(i))).String(), i%2 == 0); err != nil {
			return err
		}
	}
	return nil
}
