package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/jobs"
)

// Start runs the reconcile job (hourly: missed webhooks, stuck checkouts,
// cancellations owed after account deletion) and the retention job (daily:
// provider customers of deleted accounts once the retention period ends).
func (s *Service) Start(ctx context.Context) {
	if s.gw == nil {
		return
	}
	go jobs.Every(ctx, s.d.Pool, "payments.reconcile", time.Hour, s.reconcile)
	go jobs.Every(ctx, s.d.Pool, "payments.retention", 24*time.Hour, s.retention)
}

func (s *Service) reconcile(ctx context.Context) error {
	var failures int
	n, err := s.reconcileCheckouts(ctx)
	failures += n
	if err != nil {
		return err
	}
	n, err = s.reconcileSubscriptions(ctx)
	failures += n
	if err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("payments reconcile: %d items failed (logged above); they retry next run", failures)
	}
	return nil
}

// reconcileCheckouts re-reads sessions still pending 10 minutes after they
// opened. A session whose provider call never returned is failed after an
// hour.
func (s *Service) reconcileCheckouts(ctx context.Context) (int, error) {
	if _, err := s.d.Pool.Exec(ctx, `UPDATE payment_checkout_sessions SET status = 'failed', updated_at = now()
		WHERE provider = $1 AND status = 'pending' AND provider_session_id IS NULL
		  AND created_at < now() - interval '1 hour'`, s.gw.Name()); err != nil {
		return 0, err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, provider_session_id FROM payment_checkout_sessions
		WHERE provider = $1 AND status = 'pending' AND provider_session_id IS NOT NULL
		  AND created_at < now() - interval '10 minutes'
		ORDER BY created_at LIMIT 100`, s.gw.Name())
	if err != nil {
		return 0, err
	}
	type pending struct {
		ID       uuid.UUID
		Provider string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pending])
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, p := range list {
		err := s.syncNow(ctx, &Event{Type: EventCheckout, ObjectID: p.Provider})
		if errors.Is(err, ErrNotFound) {
			_, err = s.d.Pool.Exec(ctx, `UPDATE payment_checkout_sessions SET status = 'expired', url = NULL,
				updated_at = now() WHERE id = $1 AND status = 'pending'`, p.ID)
		}
		if err != nil {
			failed++
			slog.WarnContext(ctx, "payments: reconcile checkout", "checkout_id", p.ID, "err", err)
		}
	}
	return failed, nil
}

// reconcileSubscriptions re-syncs every live subscription (oldest sync
// first) and performs cancellations owed after account deletion.
func (s *Service) reconcileSubscriptions(ctx context.Context) (int, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id, provider_subscription_id, cancel_pending FROM payment_subscriptions
		WHERE provider = $1 AND (status IN ('incomplete', 'trialing', 'active', 'past_due', 'unpaid', 'paused') OR cancel_pending)
		ORDER BY synced_at LIMIT 500`, s.gw.Name())
	if err != nil {
		return 0, err
	}
	type live struct {
		ID       uuid.UUID
		Provider string
		Cancel   bool
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[live])
	if err != nil {
		return 0, err
	}
	failed := 0
	for _, l := range list {
		var err error
		if l.Cancel {
			err = s.cancelAtProvider(ctx, l.ID, l.Provider)
		} else if err = s.syncNow(ctx, &Event{Type: EventSubscription, ObjectID: l.Provider}); err == nil {
			err = s.backfillInvoices(ctx, l.Provider)
		}
		if errors.Is(err, ErrNotFound) {
			err = s.markSubscriptionGone(ctx, l.ID)
		}
		if err != nil {
			failed++
			slog.WarnContext(ctx, "payments: reconcile subscription", "subscription_id", l.ID, "err", err)
		}
	}
	return failed, nil
}

// backfillInvoices records paid invoices of the last 40 days whose
// invoice.paid webhook never landed (the ledger key makes repeats no-ops).
func (s *Service) backfillInvoices(ctx context.Context, providerSubID string) error {
	invs, err := s.gw.ListPaidInvoices(ctx, providerSubID, time.Now().Add(-40*24*time.Hour))
	if err != nil || len(invs) == 0 {
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var fx effects
		for i := range invs {
			if err := s.syncInvoice(ctx, tx, EventInvoicePaid, &invs[i], nil, &fx); err != nil {
				return err
			}
		}
		return nil
	})
}

// cancelAtProvider cancels immediately and syncs the result.
func (s *Service) cancelAtProvider(ctx context.Context, id uuid.UUID, providerID string) error {
	sub, err := s.gw.CancelSubscription(ctx, providerID, "cancel:"+id.String())
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		_, err := s.syncSubscription(ctx, tx, sub, subHint{})
		return err
	})
}

// markSubscriptionGone ends a subscription the provider no longer knows.
func (s *Service) markSubscriptionGone(ctx context.Context, id uuid.UUID) error {
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE payment_subscriptions SET status = 'canceled', cancel_pending = false,
			ended_at = COALESCE(ended_at, now()), synced_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE payment_entitlements SET active_until = now(), updated_at = now()
			WHERE source = 'subscription' AND source_id = $1 AND (active_until IS NULL OR active_until > now())`, id)
		return err
	})
}

// retention deletes the provider customer of each deleted account whose
// retention period has passed, then purges that account's payment rows.
func (s *Service) retention(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT id, user_id, provider_customer_id FROM payment_customers
		WHERE provider = $1 AND delete_after IS NOT NULL AND delete_after <= now()
		ORDER BY delete_after LIMIT 50`, s.gw.Name())
	if err != nil {
		return err
	}
	type due struct {
		ID, UserID uuid.UUID
		Customer   string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[due])
	if err != nil {
		return err
	}
	var lastErr error
	for _, d := range list {
		if err := s.gw.DeleteCustomer(ctx, d.Customer); err != nil {
			lastErr = err
			slog.WarnContext(ctx, "payments: retention: provider customer delete failed", "user_id", d.UserID, "err", err)
			continue
		}
		if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return purgeUser(ctx, tx, d.UserID, d.ID) }); err != nil {
			lastErr = err
			continue
		}
		slog.InfoContext(ctx, "payments: retention period over; payment records purged", "user_id", d.UserID)
	}
	return lastErr
}

// purgeUser removes every payment row of a deleted account (retention over).
func purgeUser(ctx context.Context, tx pgx.Tx, userID, customerRow uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM payment_refunds WHERE user_id = $1`,
		`DELETE FROM payment_ledger WHERE user_id = $1`,
		`DELETE FROM payment_entitlements WHERE user_id = $1`,
		`DELETE FROM payment_orders WHERE user_id = $1`,
		`DELETE FROM payment_checkout_sessions WHERE user_id = $1`,
		`DELETE FROM payment_subscriptions WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM payment_customers WHERE id = $1`, customerRow)
	return err
}
