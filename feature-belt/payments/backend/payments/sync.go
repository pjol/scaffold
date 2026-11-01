package payments

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/notify"
)

// Everything that changes payment state runs through this file: the webhook
// and the reconcile job both fetch the provider's current object (never the
// event's snapshot), then apply it here inside one transaction. Applying the
// same state twice changes nothing, so replays, out-of-order events and the
// reconcile job can't double-grant or double-record.

// snapshot is the provider's current state for one event. It is fetched
// before the transaction opens, so no row is locked during a network call.
type snapshot struct {
	checkout *Checkout
	sub      *Subscription
	invoice  *Invoice
	payment  *Payment
	account  *Account
}

func (s *Service) fetch(ctx context.Context, ev *Event) (*snapshot, error) {
	snap := &snapshot{}
	var err error
	switch ev.Type {
	case EventCheckout:
		if snap.checkout, err = s.gw.GetCheckout(ctx, ev.ObjectID); err != nil {
			return nil, err
		}
		if id := snap.checkout.SubscriptionID; id != "" {
			snap.sub, err = s.gw.GetSubscription(ctx, id)
		}
	case EventInvoicePaid, EventInvoiceFailed:
		if snap.invoice, err = s.gw.GetInvoice(ctx, ev.ObjectID); err != nil {
			return nil, err
		}
		if id := snap.invoice.SubscriptionID; id != "" {
			snap.sub, err = s.gw.GetSubscription(ctx, id)
		}
	case EventSubscription:
		snap.sub, err = s.gw.GetSubscription(ctx, ev.ObjectID)
	case EventRefund:
		snap.payment, err = s.gw.GetPayment(ctx, ev.ObjectID)
	case EventAccount:
		if s.cfg.connect {
			snap.account, err = s.gw.GetAccount(ctx, ev.ObjectID)
		}
	}
	return snap, err
}

func (s *Service) apply(ctx context.Context, tx pgx.Tx, ev *Event, snap *snapshot, fx *effects) error {
	switch ev.Type {
	case EventCheckout:
		return s.syncCheckout(ctx, tx, snap.checkout, snap.sub)
	case EventInvoicePaid, EventInvoiceFailed:
		return s.syncInvoice(ctx, tx, ev.Type, snap.invoice, snap.sub, fx)
	case EventSubscription:
		_, err := s.syncSubscription(ctx, tx, snap.sub, subHint{})
		return err
	case EventRefund:
		return s.syncRefunds(ctx, tx, snap.payment)
	case EventAccount:
		return s.syncAccount(ctx, tx, snap.account)
	}
	return nil
}

// effects are side effects collected inside a transaction and sent after it
// commits (best-effort).
type effects struct {
	notes []notify.Notification
}

func (s *Service) flush(ctx context.Context, fx *effects) {
	for _, n := range fx.notes {
		if err := s.d.Notify.Send(ctx, n); err != nil {
			slog.WarnContext(ctx, "payments: notify failed", "kind", n.Kind, "user_id", n.UserID, "err", err)
		}
	}
}

// ---- orders ------------------------------------------------------------------

// Order is a one-time purchase. Only provider state creates one.
type Order struct {
	ID                uuid.UUID        `json:"id"`
	UserID            uuid.UUID        `json:"user_id"`
	CheckoutID        uuid.UUID        `json:"checkout_id"`
	PriceKey          string           `json:"price_key"`
	Quantity          int              `json:"quantity"`
	AmountMinor       int64            `json:"amount_minor"`
	Currency          string           `json:"currency"`
	RefundedMinor     int64            `json:"refunded_minor"`
	PlatformFeeMinor  int64            `json:"platform_fee_minor"`
	DestinationUserID *uuid.UUID       `json:"destination_user_id"`
	Ref               string           `json:"ref"`
	Shipping          *ShippingAddress `json:"shipping"`
	Status            string           `json:"status"` // paid | partially_refunded | refunded
	CreatedAt         time.Time        `json:"created_at"`
}

const orderCols = `id, user_id, checkout_session_id, price_key, quantity, amount_minor, currency, refunded_minor,
	platform_fee_minor, destination_user_id, ref, shipping, status, created_at`

// OrderEvent is handed to order hooks. Type is "paid" or "refunded" (fully).
type OrderEvent struct {
	Type  string
	Order Order
}

// OrderHook runs inside the transaction that records the order change, so
// its writes commit or roll back with it. Returning an error fails the
// webhook and the provider retries; hooks must be idempotent.
type OrderHook func(ctx context.Context, tx pgx.Tx, ev OrderEvent) error

var orderHooks struct {
	sync.RWMutex
	list []OrderHook
}

// OnOrder registers a hook for paid and refunded orders (stitching code and
// features that sell things: mark a ticket paid, queue a shipment). Call it
// from a module's New.
func OnOrder(h OrderHook) {
	orderHooks.Lock()
	defer orderHooks.Unlock()
	orderHooks.list = append(orderHooks.list, h)
}

func runOrderHooks(ctx context.Context, tx pgx.Tx, ev OrderEvent) error {
	orderHooks.RLock()
	hooks := append([]OrderHook(nil), orderHooks.list...)
	orderHooks.RUnlock()
	for _, h := range hooks {
		if err := h(ctx, tx, ev); err != nil {
			return fmt.Errorf("order hook (%s %s): %w", ev.Type, ev.Order.ID, err)
		}
	}
	return nil
}

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.UserID, &o.CheckoutID, &o.PriceKey, &o.Quantity, &o.AmountMinor, &o.Currency,
		&o.RefundedMinor, &o.PlatformFeeMinor, &o.DestinationUserID, &o.Ref, &o.Shipping, &o.Status, &o.CreatedAt)
	return o, err
}

// ---- ledger writes -------------------------------------------------------------

type ledgerInsert struct {
	UserID         uuid.UUID
	Kind           string // payment | invoice | refund
	Amount         int64  // positive in, negative out
	Currency       string
	ProviderRef    string
	PaymentID      string
	OrderID        *uuid.UUID
	SubscriptionID *uuid.UUID
	Description    string
	Key            string // idempotency key; a repeat is a no-op
}

func (s *Service) addLedger(ctx context.Context, tx pgx.Tx, e ledgerInsert) error {
	if e.Amount == 0 {
		return nil
	}
	var payment *string
	if e.PaymentID != "" {
		payment = &e.PaymentID
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO payment_ledger (user_id, kind, amount_minor, currency, provider, provider_ref, provider_payment_id,
		  order_id, subscription_id, description, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		e.UserID, e.Kind, e.Amount, strings.ToLower(e.Currency), s.gw.Name(), e.ProviderRef, payment,
		e.OrderID, e.SubscriptionID, e.Description, e.Key)
	return err
}

func userDeleted(ctx context.Context, q db.Querier, userID uuid.UUID) (bool, error) {
	var status string
	if err := q.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&status); err != nil {
		return false, err
	}
	return status == "deleted", nil
}

func labelOf(p *AdminPrice, fallback string) string {
	if p != nil && p.Label != "" {
		return p.Label
	}
	if p != nil {
		return p.Key
	}
	return fallback
}

// ---- checkout ------------------------------------------------------------------

func (s *Service) syncCheckout(ctx context.Context, tx pgx.Tx, cs *Checkout, sub *Subscription) error {
	if cs == nil {
		return nil
	}
	var (
		id, userID       uuid.UUID
		priceKey, mode   string
		qty              int
		amount           *int64
		currency, status string
		ref              string
		dest             *uuid.UUID
		fee              int64
		providerID       *string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, price_key, mode, quantity, amount_minor, currency, status, ref, destination_user_id,
		       platform_fee_minor, provider_session_id
		FROM payment_checkout_sessions
		WHERE provider = $1 AND (provider_session_id = $2 OR (provider_session_id IS NULL AND id::text = $3))
		FOR UPDATE`, s.gw.Name(), cs.ID, cs.Reference).
		Scan(&id, &userID, &priceKey, &mode, &qty, &amount, &currency, &status, &ref, &dest, &fee, &providerID)
	if db.IsNoRows(err) {
		slog.InfoContext(ctx, "payments: checkout session is not ours", "session", cs.ID)
		return nil
	}
	if err != nil {
		return err
	}
	if providerID == nil {
		if _, err := tx.Exec(ctx, `UPDATE payment_checkout_sessions SET provider_session_id = $2, updated_at = now()
			WHERE id = $1`, id, cs.ID); err != nil {
			return err
		}
	}
	switch cs.Status {
	case CheckoutExpired:
		_, err := tx.Exec(ctx, `UPDATE payment_checkout_sessions SET status = 'expired', url = NULL, updated_at = now()
			WHERE id = $1 AND status = 'pending'`, id)
		return err
	case CheckoutComplete:
	default:
		return nil // still open
	}
	complete := func(newStatus string) error {
		_, err := tx.Exec(ctx, `UPDATE payment_checkout_sessions SET status = $2, url = NULL,
			completed_at = COALESCE(completed_at, now()), updated_at = now()
			WHERE id = $1 AND status <> $2`, id, newStatus)
		return err
	}

	if mode == string(ModeSubscription) {
		if err := complete("completed"); err != nil {
			return err
		}
		_, err := s.syncSubscription(ctx, tx, sub, subHint{userID: &userID, priceKey: priceKey, dest: dest})
		return err
	}

	// A completed session can still be unpaid: delayed methods (bank debits)
	// settle later and arrive as async_payment_succeeded, or the reconcile
	// job sees them paid.
	if cs.PaymentStatus != PaymentPaid {
		return nil
	}
	if cs.PaymentID == "" {
		return fmt.Errorf("paid checkout %s carries no payment id", cs.ID)
	}
	price, err := priceByKey(ctx, tx, priceKey)
	if err != nil {
		return err
	}
	desc := labelOf(price, priceKey)
	if qty > 1 {
		desc = fmt.Sprintf("%s × %d", desc, qty)
	}

	// What the provider took must match what was priced when the payer left.
	// If not, record the money (so staff can refund it) and grant nothing.
	if amount != nil && (*amount != cs.AmountTotal || !strings.EqualFold(currency, cs.Currency)) {
		slog.ErrorContext(ctx, "payments: provider charged an amount the checkout did not price; no order written",
			"checkout_id", id, "session", cs.ID, "expected", *amount, "expected_currency", currency,
			"charged", cs.AmountTotal, "charged_currency", cs.Currency)
		if err := s.addLedger(ctx, tx, ledgerInsert{
			UserID: userID, Kind: "payment", Amount: cs.AmountTotal, Currency: cs.Currency,
			ProviderRef: cs.PaymentID, PaymentID: cs.PaymentID, Description: desc, Key: "payment:" + cs.PaymentID,
		}); err != nil {
			return err
		}
		return complete("failed")
	}

	o, err := scanOrder(tx.QueryRow(ctx, `
		INSERT INTO payment_orders (user_id, checkout_session_id, provider, provider_payment_id, price_key, quantity,
		  amount_minor, currency, platform_fee_minor, destination_user_id, ref, shipping)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (checkout_session_id) DO NOTHING
		RETURNING `+orderCols,
		userID, id, s.gw.Name(), cs.PaymentID, priceKey, qty, cs.AmountTotal, strings.ToLower(cs.Currency), fee, dest, ref, cs.Shipping))
	switch {
	case db.IsNoRows(err):
		// Already recorded (completed and async_payment_succeeded both arrive).
		return complete("completed")
	case err != nil:
		return err
	}
	if err := s.addLedger(ctx, tx, ledgerInsert{
		UserID: userID, Kind: "payment", Amount: o.AmountMinor, Currency: o.Currency, ProviderRef: cs.PaymentID,
		PaymentID: cs.PaymentID, OrderID: &o.ID, Description: desc, Key: "payment:" + cs.PaymentID,
	}); err != nil {
		return err
	}
	if err := s.grantOrder(ctx, tx, o, price); err != nil {
		return err
	}
	if err := runOrderHooks(ctx, tx, OrderEvent{Type: "paid", Order: o}); err != nil {
		return err
	}
	slog.InfoContext(ctx, "payments: order paid", "order_id", o.ID, "amount_minor", o.AmountMinor, "currency", o.Currency)
	return complete("completed")
}

func (s *Service) grantOrder(ctx context.Context, tx pgx.Tx, o Order, price *AdminPrice) error {
	if price == nil || price.Entitlement == nil {
		return nil
	}
	if gone, err := userDeleted(ctx, tx, o.UserID); err != nil || gone {
		return err
	}
	var until *time.Time
	if price.EntitlementDays != nil {
		t := time.Now().UTC().AddDate(0, 0, *price.EntitlementDays)
		until = &t
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO payment_entitlements (user_id, key, source, source_id, active_until)
		VALUES ($1, $2, 'order', $3, $4) ON CONFLICT (source, source_id, key) DO NOTHING`,
		o.UserID, *price.Entitlement, o.ID, until)
	return err
}

// ---- subscriptions -------------------------------------------------------------

type subHint struct {
	userID   *uuid.UUID
	priceKey string
	dest     *uuid.UUID
}

type subRef struct {
	id, userID uuid.UUID
	priceKey   string
}

var knownSubStatuses = map[string]bool{
	SubIncomplete: true, SubIncompleteExpired: true, SubTrialing: true, SubActive: true,
	SubPastDue: true, SubCanceled: true, SubUnpaid: true, SubPaused: true,
}

func tp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// accessUntil is when a subscription's entitlement ends. Paid-up states keep
// access to the end of the period plus a renewal grace (renewal webhooks can
// lag); a scheduled cancellation ends access exactly at its date; every other
// state has already ended.
func accessUntil(sub *Subscription, grace time.Duration, now time.Time) time.Time {
	switch sub.Status {
	case SubActive, SubTrialing, SubPastDue:
		end := sub.CurrentPeriodEnd
		if end.IsZero() {
			end = now
		}
		if !sub.CancelAt.IsZero() && !sub.CancelAt.After(end) {
			return sub.CancelAt
		}
		if sub.CancelAtPeriodEnd {
			return end
		}
		return end.Add(grace)
	}
	if !sub.EndedAt.IsZero() && sub.EndedAt.Before(now) {
		return sub.EndedAt
	}
	return now
}

// syncSubscription upserts the provider's subscription and its entitlement.
// The owner is our existing row, else our customer mapping, else the
// checkout that created it. Subscriptions we can't place are ignored.
func (s *Service) syncSubscription(ctx context.Context, tx pgx.Tx, sub *Subscription, hint subHint) (*subRef, error) {
	if sub == nil {
		return nil, nil
	}
	if !knownSubStatuses[sub.Status] {
		slog.ErrorContext(ctx, "payments: unknown subscription status; not synced", "subscription", sub.ID, "status", sub.Status)
		return nil, nil
	}
	var userID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT user_id FROM payment_subscriptions
		WHERE provider = $1 AND provider_subscription_id = $2 FOR UPDATE`, s.gw.Name(), sub.ID).Scan(&userID)
	if db.IsNoRows(err) {
		err = tx.QueryRow(ctx, `SELECT user_id FROM payment_customers WHERE provider = $1 AND provider_customer_id = $2`,
			s.gw.Name(), sub.CustomerID).Scan(&userID)
		if db.IsNoRows(err) {
			if hint.userID == nil {
				slog.InfoContext(ctx, "payments: subscription is not ours", "subscription", sub.ID)
				return nil, nil
			}
			userID, err = *hint.userID, nil
		}
	}
	if err != nil {
		return nil, err
	}
	price, err := priceByProviderID(ctx, tx, s.gw.Name(), sub.PriceID)
	if err != nil {
		return nil, err
	}
	priceKey := hint.priceKey
	if price != nil {
		priceKey = price.Key
	} else if priceKey == "" {
		priceKey = sub.Metadata["price_key"]
	}
	ref := &subRef{userID: userID, priceKey: priceKey}
	err = tx.QueryRow(ctx, `
		INSERT INTO payment_subscriptions (user_id, provider, provider_subscription_id, provider_customer_id,
		  provider_price_id, price_key, quantity, status, current_period_start, current_period_end,
		  cancel_at_period_end, cancel_at, canceled_at, ended_at, destination_user_id, synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, now())
		ON CONFLICT (provider, provider_subscription_id) DO UPDATE SET
		  provider_customer_id = EXCLUDED.provider_customer_id, provider_price_id = EXCLUDED.provider_price_id,
		  price_key = EXCLUDED.price_key, quantity = EXCLUDED.quantity, status = EXCLUDED.status,
		  current_period_start = EXCLUDED.current_period_start, current_period_end = EXCLUDED.current_period_end,
		  cancel_at_period_end = EXCLUDED.cancel_at_period_end, cancel_at = EXCLUDED.cancel_at,
		  canceled_at = EXCLUDED.canceled_at, ended_at = EXCLUDED.ended_at,
		  destination_user_id = COALESCE(payment_subscriptions.destination_user_id, EXCLUDED.destination_user_id),
		  cancel_pending = payment_subscriptions.cancel_pending AND EXCLUDED.status NOT IN ('canceled', 'incomplete_expired'),
		  synced_at = now(), updated_at = now()
		RETURNING id`,
		userID, s.gw.Name(), sub.ID, sub.CustomerID, sub.PriceID, priceKey, sub.Quantity, sub.Status,
		tp(sub.CurrentPeriodStart), tp(sub.CurrentPeriodEnd), sub.CancelAtPeriodEnd, tp(sub.CancelAt),
		tp(sub.CanceledAt), tp(sub.EndedAt), hint.dest).Scan(&ref.id)
	if err != nil {
		return nil, err
	}

	keep := ""
	gone, err := userDeleted(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if price != nil && price.Entitlement != nil && !gone {
		keep = *price.Entitlement
		if _, err := tx.Exec(ctx, `
			INSERT INTO payment_entitlements (user_id, key, source, source_id, active_until)
			VALUES ($1, $2, 'subscription', $3, $4)
			ON CONFLICT (source, source_id, key) DO UPDATE SET active_until = EXCLUDED.active_until, updated_at = now()`,
			userID, keep, ref.id, accessUntil(sub, s.cfg.renewalGrace, time.Now().UTC())); err != nil {
			return nil, err
		}
	}
	// A plan change can move the subscription to a price with a different
	// entitlement (or none): end what it no longer carries.
	if _, err := tx.Exec(ctx, `
		UPDATE payment_entitlements SET active_until = now(), updated_at = now()
		WHERE source = 'subscription' AND source_id = $1 AND key <> $2
		  AND (active_until IS NULL OR active_until > now())`, ref.id, keep); err != nil {
		return nil, err
	}
	return ref, nil
}

// ---- invoices ------------------------------------------------------------------

func (s *Service) syncInvoice(ctx context.Context, tx pgx.Tx, typ string, inv *Invoice, sub *Subscription, fx *effects) error {
	if inv == nil {
		return nil
	}
	var ref *subRef
	var err error
	if sub != nil {
		if ref, err = s.syncSubscription(ctx, tx, sub, subHint{}); err != nil {
			return err
		}
	} else if inv.SubscriptionID != "" {
		r := subRef{}
		err := tx.QueryRow(ctx, `SELECT id, user_id, price_key FROM payment_subscriptions
			WHERE provider = $1 AND provider_subscription_id = $2`, s.gw.Name(), inv.SubscriptionID).
			Scan(&r.id, &r.userID, &r.priceKey)
		if err != nil && !db.IsNoRows(err) {
			return err
		}
		if err == nil {
			ref = &r
		}
	}
	if ref == nil {
		slog.InfoContext(ctx, "payments: invoice is not for one of our subscriptions", "invoice", inv.ID)
		return nil
	}
	switch typ {
	case EventInvoicePaid:
		if inv.Status != "paid" || inv.AmountPaid <= 0 {
			return nil // a trial or fully credited invoice moves no money
		}
		price, err := priceByKey(ctx, tx, ref.priceKey)
		if err != nil {
			return err
		}
		return s.addLedger(ctx, tx, ledgerInsert{
			UserID: ref.userID, Kind: "invoice", Amount: inv.AmountPaid, Currency: inv.Currency,
			ProviderRef: inv.ID, PaymentID: inv.PaymentID, SubscriptionID: &ref.id,
			Description: labelOf(price, "Subscription"), Key: "invoice:" + inv.ID,
		})
	case EventInvoiceFailed:
		if inv.Status == "paid" {
			return nil
		}
		if gone, err := userDeleted(ctx, tx, ref.userID); err != nil || gone {
			return err
		}
		fx.notes = append(fx.notes, notify.Notification{
			UserID: ref.userID, Kind: KindFailed,
			Title: "Payment failed",
			Body:  "Update your payment method to keep your subscription.",
			Link:  "/settings",
			Data:  map[string]any{"subscription_id": ref.id.String()},
		})
	}
	return nil
}

// ---- refunds -------------------------------------------------------------------

// syncRefunds records every refund the provider holds for a payment (ours or
// made in the provider's dashboard) and settles the order.
func (s *Service) syncRefunds(ctx context.Context, tx pgx.Tx, p *Payment) error {
	if p == nil {
		return nil
	}
	var (
		ledgerID      int64
		userID        uuid.UUID
		orderID       *uuid.UUID
		subID         *uuid.UUID
		entryCurrency string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, order_id, subscription_id, currency FROM payment_ledger
		WHERE provider = $1 AND provider_payment_id = $2 AND kind IN ('payment', 'invoice')
		ORDER BY id LIMIT 1`, s.gw.Name(), p.ID).Scan(&ledgerID, &userID, &orderID, &subID, &entryCurrency)
	if db.IsNoRows(err) {
		var ours bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM payment_customers
			WHERE provider = $1 AND provider_customer_id = $2)`, s.gw.Name(), p.CustomerID).Scan(&ours); err != nil {
			return err
		}
		if ours && p.CustomerID != "" {
			// Our customer, but the payment itself isn't recorded yet: fail so
			// the provider retries after the payment event lands.
			return fmt.Errorf("refund for payment %s arrived before the payment was recorded", p.ID)
		}
		return nil
	}
	if err != nil {
		return err
	}
	for _, r := range p.Refunds {
		status := r.Status
		switch status {
		case RefundPending, RefundSucceeded, RefundFailed, RefundCanceled:
		default:
			status = RefundPending
		}
		currency := r.Currency
		if currency == "" {
			currency = entryCurrency
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO payment_refunds (user_id, ledger_id, order_id, provider, provider_refund_id, provider_payment_id,
			  amount_minor, currency, reason, status, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'provider:' || $5)
			ON CONFLICT (provider, provider_refund_id) DO UPDATE SET status = EXCLUDED.status, updated_at = now()`,
			userID, ledgerID, orderID, s.gw.Name(), r.ID, p.ID, r.Amount, currency, r.Reason, status); err != nil {
			return err
		}
		if status == RefundSucceeded {
			if err := s.addLedger(ctx, tx, ledgerInsert{
				UserID: userID, Kind: "refund", Amount: -r.Amount, Currency: currency, ProviderRef: r.ID,
				PaymentID: p.ID, OrderID: orderID, SubscriptionID: subID, Description: "Refund", Key: "refund:" + r.ID,
			}); err != nil {
				return err
			}
		}
	}
	return s.settleOrders(ctx, tx, p.ID)
}

func orderStatus(amount, refunded int64) string {
	switch {
	case refunded <= 0:
		return "paid"
	case refunded >= amount:
		return "refunded"
	default:
		return "partially_refunded"
	}
}

// settleOrders recomputes refunded totals for a payment's order and revokes
// a fully refunded order's entitlement.
func (s *Service) settleOrders(ctx context.Context, tx pgx.Tx, paymentID string) error {
	rows, err := tx.Query(ctx, `SELECT `+orderCols+` FROM payment_orders
		WHERE provider = $1 AND provider_payment_id = $2 FOR UPDATE`, s.gw.Name(), paymentID)
	if err != nil {
		return err
	}
	var orders []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			rows.Close()
			return err
		}
		orders = append(orders, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(orders) == 0 {
		return err
	}
	var total int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor), 0) FROM payment_refunds
		WHERE provider = $1 AND provider_payment_id = $2 AND status = 'succeeded'`, s.gw.Name(), paymentID).Scan(&total); err != nil {
		return err
	}
	for _, o := range orders {
		status := orderStatus(o.AmountMinor, total)
		if o.RefundedMinor == total && o.Status == status {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE payment_orders SET refunded_minor = $2, status = $3, updated_at = now()
			WHERE id = $1`, o.ID, total, status); err != nil {
			return err
		}
		if status == "refunded" && o.Status != "refunded" {
			if _, err := tx.Exec(ctx, `UPDATE payment_entitlements SET active_until = now(), updated_at = now()
				WHERE source = 'order' AND source_id = $1 AND (active_until IS NULL OR active_until > now())`, o.ID); err != nil {
				return err
			}
			o.RefundedMinor, o.Status = total, status
			if err := runOrderHooks(ctx, tx, OrderEvent{Type: "refunded", Order: o}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- connected accounts --------------------------------------------------------

func (s *Service) syncAccount(ctx context.Context, tx pgx.Tx, a *Account) error {
	if a == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE payment_connected_accounts SET charges_enabled = $3, payouts_enabled = $4, details_submitted = $5,
		  updated_at = now()
		WHERE provider = $1 AND provider_account_id = $2`,
		s.gw.Name(), a.ID, a.ChargesEnabled, a.PayoutsEnabled, a.DetailsSubmitted)
	return err
}

// syncNow fetches and applies one event outside the webhook (reconcile).
func (s *Service) syncNow(ctx context.Context, ev *Event) error {
	snap, err := s.fetch(ctx, ev)
	if err != nil {
		return err
	}
	var fx effects
	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return s.apply(ctx, tx, ev, snap, &fx) }); err != nil {
		return err
	}
	s.flush(ctx, &fx)
	return nil
}
