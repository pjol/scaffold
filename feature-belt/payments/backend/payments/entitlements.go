package payments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Entitlement changes. Every write that moves a payment_entitlements row
// (an order paid or refunded, a subscription started, renewed, cancelled or
// lost, a term that ran out, an account deleted) is announced to the hooks
// registered with OnEntitlementChange, inside the transaction that made it,
// with the user's resulting hold on the key across every source. Other
// modules follow subscriptions this way without polling. A write that
// changes nothing a reader can see (a re-sync of the same period) announces
// nothing.

// Causes of an EntitlementChange.
const (
	EntitlementGranted   = "granted"   // a source began granting the key (paid order, new or reactivated subscription)
	EntitlementExtended  = "extended"  // its end moved later (a renewal)
	EntitlementShortened = "shortened" // its end moved earlier but is still ahead (cancel at period end)
	EntitlementRevoked   = "revoked"   // ended now: refund, cancellation, plan change, the provider lost the subscription
	EntitlementLapsed    = "lapsed"    // its end passed with no sync (a term ran out, a renewal never arrived)
	EntitlementDeleted   = "deleted"   // the account was deleted
	EntitlementReplayed  = "replayed"  // ReplayEntitlements re-announcing a current holder
)

// EntitlementChange is one announced change.
type EntitlementChange struct {
	UserID uuid.UUID
	Key    string
	Cause  string // Entitlement* constant
	// The row that moved: "order" or "subscription" and its id. Empty and
	// uuid.Nil for Cause deleted and replayed (every source).
	Source   string
	SourceID uuid.UUID
	// Active and Until are the user's hold on Key after the change, across
	// every source: Until is the latest end, nil when a source grants it
	// with no end (or when not Active).
	Active bool
	Until  *time.Time
}

// EntitlementHook runs inside the transaction that changed the entitlement,
// so its writes commit or roll back with it; an error fails the sync (the
// webhook answers 500 and the provider retries). Hooks must be idempotent
// and quick: set state to what the change says, never call out. Queue
// notifications and provider calls with AfterCommit.
type EntitlementHook func(ctx context.Context, tx pgx.Tx, ch EntitlementChange) error

var entitlementHooks struct {
	sync.RWMutex
	list []EntitlementHook
}

// OnEntitlementChange registers a hook for entitlement changes (stitching
// code, or a feature that follows a paid plan). Call it from a module's New
// or an init().
func OnEntitlementChange(h EntitlementHook) {
	entitlementHooks.Lock()
	defer entitlementHooks.Unlock()
	entitlementHooks.list = append(entitlementHooks.list, h)
}

func runEntitlementHooks(ctx context.Context, tx pgx.Tx, ch EntitlementChange) error {
	entitlementHooks.RLock()
	hooks := append([]EntitlementHook(nil), entitlementHooks.list...)
	entitlementHooks.RUnlock()
	for _, h := range hooks {
		if err := h(ctx, tx, ch); err != nil {
			return fmt.Errorf("entitlement hook (%s %s %s): %w", ch.Cause, ch.Key, ch.UserID, err)
		}
	}
	return nil
}

// ---- after commit ----------------------------------------------------------------

type effectsKey struct{}

func withEffects(ctx context.Context, fx *effects) context.Context {
	return context.WithValue(ctx, effectsKey{}, fx)
}

// AfterCommit queues fn to run in the background (bg.Go) once the
// transaction a payments hook (OnOrder, OnEntitlementChange) runs in has
// committed; if it rolls back, fn never runs. Use it for the notifications
// and provider calls a hook triggers. Work queued while an account is being
// deleted (Cause deleted) is dropped: the person is gone and payments
// cancels their subscriptions itself. Called outside a hook, fn starts in the
// background right away.
func AfterCommit(ctx context.Context, name string, fn func(ctx context.Context) error) {
	if fx, ok := ctx.Value(effectsKey{}).(*effects); ok && fx != nil {
		if !fx.drop {
			fx.after = append(fx.after, afterWork{name: name, fn: fn})
		}
		return
	}
	bg.Go(ctx, name, fn)
}

type afterWork struct {
	name string
	fn   func(ctx context.Context) error
}

// inTx runs fn in a transaction whose hooks may queue notifications and
// AfterCommit work, and sends them once it commits.
func (s *Service) inTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	var fx effects
	hctx := withEffects(ctx, &fx)
	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return fn(hctx, tx) }); err != nil {
		return err
	}
	s.flush(ctx, &fx)
	return nil
}

// ---- change detection --------------------------------------------------------------

// rowState is one entitlement row's end before or after a write.
type rowState struct {
	exists bool
	until  *time.Time // nil = no end
}

func (r rowState) activeAt(now time.Time) bool {
	return r.exists && (r.until == nil || r.until.After(now))
}

// lockEntitlement reads one row's state and locks it for the write.
func lockEntitlement(ctx context.Context, tx pgx.Tx, source string, sourceID uuid.UUID, key string) (rowState, error) {
	var st rowState
	err := tx.QueryRow(ctx, `SELECT active_until FROM payment_entitlements
		WHERE source = $1 AND source_id = $2 AND key = $3 FOR UPDATE`, source, sourceID, key).Scan(&st.until)
	switch {
	case db.IsNoRows(err):
		return rowState{}, nil
	case err != nil:
		return rowState{}, err
	}
	st.exists = true
	return st, nil
}

// changeCause names what a row's write did to a reader at now ("" = nothing).
func changeCause(before, after rowState, now time.Time) string {
	was, is := before.activeAt(now), after.activeAt(now)
	switch {
	case !was && is:
		return EntitlementGranted
	case was && !is:
		return EntitlementRevoked
	case !was:
		return ""
	}
	switch {
	case before.until == nil && after.until == nil:
		return ""
	case after.until == nil:
		return EntitlementExtended
	case before.until == nil:
		return EntitlementShortened
	case after.until.Equal(*before.until):
		return ""
	case after.until.After(*before.until):
		return EntitlementExtended
	default:
		return EntitlementShortened
	}
}

// holdingOf is the user's hold on key across every source at now.
func holdingOf(ctx context.Context, q db.Querier, userID uuid.UUID, key string, now time.Time) (bool, *time.Time, error) {
	rows, err := q.Query(ctx, `SELECT active_until FROM payment_entitlements
		WHERE user_id = $1 AND key = $2 AND (active_until IS NULL OR active_until > $3)`, userID, key, now)
	if err != nil {
		return false, nil, err
	}
	ends, err := pgx.CollectRows(rows, pgx.RowTo[*time.Time])
	if err != nil || len(ends) == 0 {
		return false, nil, err
	}
	var latest *time.Time
	for _, e := range ends {
		if e == nil {
			return true, nil, nil
		}
		if latest == nil || e.After(*latest) {
			latest = e
		}
	}
	return true, latest, nil
}

// announce fills the user's resulting hold and runs the hooks.
func announce(ctx context.Context, tx pgx.Tx, ch EntitlementChange, now time.Time) error {
	var err error
	if ch.Active, ch.Until, err = holdingOf(ctx, tx, ch.UserID, ch.Key, now); err != nil {
		return err
	}
	return runEntitlementHooks(ctx, tx, ch)
}

// entitlementMoved announces one row's write when a reader would see it.
func entitlementMoved(ctx context.Context, tx pgx.Tx, userID uuid.UUID, key, source string, sourceID uuid.UUID,
	before, after rowState, now time.Time) error {
	cause := changeCause(before, after, now)
	if cause == "" {
		return nil
	}
	return announce(ctx, tx, EntitlementChange{UserID: userID, Key: key, Cause: cause, Source: source, SourceID: sourceID}, now)
}

// revokeSource ends every live entitlement of one order or subscription
// (except keep) at now and announces each.
func revokeSource(ctx context.Context, tx pgx.Tx, source string, sourceID uuid.UUID, keep string, now time.Time) error {
	rows, err := tx.Query(ctx, `
		UPDATE payment_entitlements SET active_until = $3, end_reported_at = $3, updated_at = now()
		WHERE source = $1 AND source_id = $2 AND key <> $4 AND (active_until IS NULL OR active_until > $3)
		RETURNING user_id, key`, source, sourceID, now, keep)
	if err != nil {
		return err
	}
	type ended struct {
		UserID uuid.UUID
		Key    string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ended])
	if err != nil {
		return err
	}
	for _, e := range list {
		if err := announce(ctx, tx, EntitlementChange{UserID: e.UserID, Key: e.Key, Cause: EntitlementRevoked,
			Source: source, SourceID: sourceID}, now); err != nil {
			return err
		}
	}
	return nil
}

// ---- lapses ------------------------------------------------------------------------

// sweepLapses announces entitlements whose end passed with no sync (a term
// bought with an order ran out, a renewal never arrived), once each: the row
// is marked end_reported_at in the same transaction. One transaction per row,
// so a failing hook holds back only its own row (retried next run).
func (s *Service) sweepLapses(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM payment_entitlements
		WHERE end_reported_at IS NULL AND active_until IS NOT NULL AND active_until <= now()
		ORDER BY active_until LIMIT 500`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			now := time.Now()
			var ch EntitlementChange
			err := tx.QueryRow(ctx, `UPDATE payment_entitlements SET end_reported_at = $2
				WHERE id = $1 AND end_reported_at IS NULL AND active_until IS NOT NULL AND active_until <= $2
				RETURNING user_id, key, source, source_id`, id, now).Scan(&ch.UserID, &ch.Key, &ch.Source, &ch.SourceID)
			if db.IsNoRows(err) {
				return nil // renewed or reported meanwhile
			}
			if err != nil {
				return err
			}
			ch.Cause = EntitlementLapsed
			return announce(ctx, tx, ch, now)
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("entitlement %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// ---- replay ------------------------------------------------------------------------

// ReplayEntitlements announces every current holder of key to the hooks
// (Cause replayed, one transaction each), for a listener added after sales
// began. Hooks are idempotent, so running it again is harmless. Run it once,
// e.g. from a one-off command or an admin action; it returns how many
// holders it announced.
func (s *Service) ReplayEntitlements(ctx context.Context, key string) (int, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT user_id FROM payment_entitlements
		WHERE key = $1 AND (active_until IS NULL OR active_until > now())`, key)
	if err != nil {
		return 0, err
	}
	users, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, err
	}
	n := 0
	var errs []error
	for _, u := range users {
		err := s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return announce(ctx, tx, EntitlementChange{UserID: u, Key: key, Cause: EntitlementReplayed}, time.Now())
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("replay %s for %s: %w", key, u, err))
			continue
		}
		n++
	}
	return n, errors.Join(errs...)
}

// ---- stop renewal ------------------------------------------------------------------

// StopRenewal makes the user's live subscriptions that grant key end at the
// close of their paid period instead of renewing (no refund: access runs to
// that date), and syncs them, which announces the shortened entitlement. For
// a perk staff took away. Idempotent; a no-op without a provider. The user
// can resume renewal in the billing portal.
func (s *Service) StopRenewal(ctx context.Context, userID uuid.UUID, key string) error {
	if s.gw == nil {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT sub.provider_subscription_id FROM payment_subscriptions sub
		JOIN payment_prices p ON p.key = sub.price_key
		WHERE sub.user_id = $1 AND sub.provider = $2 AND p.entitlement = $3 AND sub.status = ANY($4)
		  AND NOT sub.cancel_at_period_end AND NOT sub.cancel_pending`,
		userID, s.gw.Name(), key, []string{SubTrialing, SubActive, SubPastDue, SubUnpaid, SubPaused})
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		sub, err := s.gw.CancelAtPeriodEnd(ctx, id)
		if err == nil {
			err = s.inTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err := s.syncSubscription(ctx, tx, sub, subHint{})
				return err
			})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("stop renewal of %s: %w", id, err))
		}
	}
	if len(errs) > 0 {
		slog.WarnContext(ctx, "payments: stop renewal failed", "user_id", userID, "key", key, "failures", len(errs))
	}
	return errors.Join(errs...)
}

// ---- checkout guards ---------------------------------------------------------------

// GuardedPrice is the price a checkout is about to sell.
type GuardedPrice struct {
	Key         string
	Entitlement string // "" when the price grants none
	Recurring   bool
}

// CheckoutGuard may refuse a checkout before the provider sees it. Return an
// *httpx.Error (e.g. 422 not_eligible) and the client shows its message; any
// other error is a 500. It runs for every checkout: return nil for prices it
// doesn't care about.
type CheckoutGuard func(ctx context.Context, q db.Querier, userID uuid.UUID, price GuardedPrice) error

var checkoutGuards struct {
	sync.RWMutex
	list []CheckoutGuard
}

// GuardCheckout registers a guard (stitching: only people who can hold a
// perk may buy it). Call it from a module's New or an init().
func GuardCheckout(g CheckoutGuard) {
	checkoutGuards.Lock()
	defer checkoutGuards.Unlock()
	checkoutGuards.list = append(checkoutGuards.list, g)
}

func runCheckoutGuards(ctx context.Context, q db.Querier, userID uuid.UUID, p *AdminPrice) error {
	checkoutGuards.RLock()
	guards := append([]CheckoutGuard(nil), checkoutGuards.list...)
	checkoutGuards.RUnlock()
	gp := GuardedPrice{Key: p.Key, Recurring: p.Kind == "recurring"}
	if p.Entitlement != nil {
		gp.Entitlement = *p.Entitlement
	}
	for _, g := range guards {
		if err := g(ctx, q, userID, gp); err != nil {
			var he *httpx.Error
			if errors.As(err, &he) {
				return he
			}
			return fmt.Errorf("checkout guard: %w", err)
		}
	}
	return nil
}
