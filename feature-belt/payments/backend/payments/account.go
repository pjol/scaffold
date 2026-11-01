package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- account deletion ----------------------------------------------------------

// DeleteAccount runs inside the account-deletion transaction and touches only
// the database:
//   - live subscriptions are flagged cancel_pending (AccountDeleted cancels
//     them at the provider right after commit; the reconcile job retries),
//   - open checkouts are marked expired, entitlements deleted,
//   - prices that paid this user (Connect) are deactivated,
//   - orders, subscriptions, ledger entries and refunds stay, pseudonymised
//     (the users row no longer identifies anyone) for PAYMENTS_RETENTION_YEARS,
//   - the provider customer is scheduled for deletion when that period ends
//     (the retention job then purges these rows too); a customer with no
//     money movement is removed on the job's next run.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`UPDATE payment_subscriptions SET cancel_pending = true, updated_at = now()
		 WHERE user_id = $1 AND status IN ('incomplete', 'trialing', 'active', 'past_due', 'unpaid', 'paused')`,
		`UPDATE payment_checkout_sessions SET status = 'expired', url = NULL, updated_at = now()
		 WHERE user_id = $1 AND status = 'pending'`,
		`DELETE FROM payment_entitlements WHERE user_id = $1`,
		`UPDATE payment_prices SET active = false, updated_at = now() WHERE destination_user_id = $1 AND active`,
		`UPDATE payment_connected_accounts SET charges_enabled = false, updated_at = now() WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	// A customer that never paid holds nothing the law needs: the retention
	// job removes it on its next run.
	_, err := tx.Exec(ctx, `UPDATE payment_customers SET delete_after = CASE
		  WHEN EXISTS (SELECT 1 FROM payment_ledger WHERE user_id = $1) THEN now() + make_interval(years => $2)
		  ELSE now() END
		WHERE user_id = $1 AND delete_after IS NULL`, userID, s.cfg.retentionYears)
	return err
}

// AccountDeleted runs after the deletion commits: cancel the user's live
// subscriptions at the provider now and close checkout pages that are still
// open there. Best-effort; cancel_pending stays set on failure and the
// reconcile job retries within the hour.
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	if s.gw == nil {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, provider_subscription_id FROM payment_subscriptions
		WHERE user_id = $1 AND provider = $2 AND cancel_pending`, userID, s.gw.Name())
	if err != nil {
		return err
	}
	type owed struct {
		ID         uuid.UUID
		ProviderID string
	}
	subs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[owed])
	if err != nil {
		return err
	}
	var errs []error
	for _, o := range subs {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := s.cancelAtProvider(cctx, o.ID, o.ProviderID)
		cancel()
		if errors.Is(err, ErrNotFound) {
			err = s.markSubscriptionGone(ctx, o.ID)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("cancel subscription %s: %w", o.ID, err))
		}
	}
	srows, err := s.d.Pool.Query(ctx, `SELECT provider_session_id FROM payment_checkout_sessions
		WHERE user_id = $1 AND provider = $2 AND status = 'expired' AND provider_session_id IS NOT NULL
		  AND expires_at > now()`, userID, s.gw.Name())
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	open, err := pgx.CollectRows(srows, pgx.RowTo[string])
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, id := range open {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := s.gw.ExpireCheckout(cctx, id); err != nil {
			errs = append(errs, fmt.Errorf("expire checkout %s: %w", id, err))
		}
		cancel()
	}
	if len(errs) > 0 {
		slog.WarnContext(ctx, "payments: provider cleanup after account deletion will be retried", "user_id", userID, "failures", len(errs))
	}
	return errors.Join(errs...)
}

// ---- export ----------------------------------------------------------------

// ExportAccount returns the user's orders, subscriptions, ledger entries and
// refunds.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	orders, err := queryOrders(ctx, s.d.Pool, `WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	subs, err := querySubscriptions(ctx, s.d.Pool, `WHERE s.user_id = $1 ORDER BY s.created_at`, userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, kind, amount_minor, currency, description, order_id, subscription_id, created_at
		FROM payment_ledger WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	ledger, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LedgerEntry])
	if err != nil {
		return nil, err
	}
	type refund struct {
		ID          uuid.UUID  `json:"id"`
		OrderID     *uuid.UUID `json:"order_id"`
		AmountMinor int64      `json:"amount_minor"`
		Currency    string     `json:"currency"`
		Status      string     `json:"status"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	rrows, err := s.d.Pool.Query(ctx, `SELECT id, order_id, amount_minor, currency, status, created_at
		FROM payment_refunds WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	refunds, err := pgx.CollectRows(rrows, pgx.RowToStructByPos[refund])
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"orders":        orders,
		"subscriptions": nonNil(subs),
		"ledger":        nonNil(ledger),
		"refunds":       nonNil(refunds),
	}, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ---- development seed --------------------------------------------------------

// SeedDev seeds the catalog from PAYMENTS_PRICES (with the provider's real
// amounts) and gives the second dev user one past purchase. Seed rows carry
// provider "seed", so the reconcile job and refunds never send them to the
// real provider.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if err := s.seedCatalog(ctx); err != nil {
		return err
	}
	if len(users) < 2 {
		return nil
	}
	buyer := users[1]
	checkoutID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("scaffold:payments:seed:"+buyer.String()))
	tag := checkoutID.String()[:8]
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO payment_checkout_sessions (id, user_id, provider, provider_session_id, price_key, mode, quantity,
			  amount_minor, currency, status, completed_at, created_at, updated_at)
			VALUES ($1, $2, 'seed', 'seed_cs_' || $3, 'supporter_pack', 'payment', 1, 1500, 'usd', 'completed',
			  now() - interval '12 days', now() - interval '12 days', now() - interval '12 days')
			ON CONFLICT (id) DO NOTHING`, checkoutID, buyer, tag); err != nil {
			return err
		}
		var orderID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO payment_orders (user_id, checkout_session_id, provider, provider_payment_id, price_key, quantity,
			  amount_minor, currency, created_at, updated_at)
			VALUES ($1, $2, 'seed', 'seed_pi_' || $3, 'supporter_pack', 1, 1500, 'usd',
			  now() - interval '12 days', now() - interval '12 days')
			ON CONFLICT (checkout_session_id) DO UPDATE SET updated_at = payment_orders.updated_at
			RETURNING id`, buyer, checkoutID, tag).Scan(&orderID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO payment_ledger (user_id, kind, amount_minor, currency, provider, provider_ref, provider_payment_id,
			  order_id, description, idempotency_key, created_at)
			VALUES ($1, 'payment', 1500, 'usd', 'seed', 'seed_pi_' || $3, 'seed_pi_' || $3, $2, 'Supporter pack',
			  'seed:payment:' || $3, now() - interval '12 days')
			ON CONFLICT (idempotency_key) DO NOTHING`, buyer, orderID, tag)
		return err
	})
}

// ---- agent tools -------------------------------------------------------------

func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "payments_get_subscription",
			Description: "Get the caller's current subscription (plan, status, renewal or end date) and the entitlements they hold.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				sub, err := CurrentSubscription(ctx, s.d.Pool, caller.UserID)
				if err != nil {
					return nil, err
				}
				ents, err := ActiveEntitlements(ctx, s.d.Pool, caller.UserID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"subscription": sub, "entitlements": ents}, nil
			},
		},
		{
			Name: "payments_list_history",
			Description: "List the caller's payments and refunds, newest first. Amounts are integer minor units " +
				"(cents for USD) with a currency; refunds are negative. Pass next_cursor back as cursor for more.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` +
				`"limit":{"type":"integer","minimum":1,"maximum":100},` +
				`"cursor":{"type":"string"}},"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Limit  int    `json:"limit"`
					Cursor string `json:"cursor"`
				}
				if len(input) > 0 {
					if err := json.Unmarshal(input, &in); err != nil {
						return nil, httpx.BadRequest("Invalid input.")
					}
				}
				p := httpx.PageParams{Limit: 20, Cursor: in.Cursor}
				if in.Limit > 0 {
					p.Limit = min(in.Limit, 100)
				}
				return ListLedger(ctx, s.d.Pool, caller.UserID, p)
			},
		},
	}
}
