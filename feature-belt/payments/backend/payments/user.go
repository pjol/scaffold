package payments

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// SubscriptionView is a subscription as its owner sees it.
type SubscriptionView struct {
	ID                uuid.UUID  `json:"id"`
	PriceKey          string     `json:"price_key"`
	Label             string     `json:"label"`
	Entitlement       *string    `json:"entitlement"`
	Status            string     `json:"status"`
	Quantity          int        `json:"quantity"`
	CurrentPeriodEnd  *time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end"`
	CancelAt          *time.Time `json:"cancel_at"`
	EndedAt           *time.Time `json:"ended_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

const subViewCols = `s.id, s.price_key, COALESCE(p.label, s.price_key), p.entitlement, s.status, s.quantity,
	s.current_period_end, s.cancel_at_period_end, s.cancel_at, s.ended_at, s.created_at`

func querySubscriptions(ctx context.Context, q db.Querier, where string, args ...any) ([]SubscriptionView, error) {
	rows, err := q.Query(ctx, `SELECT `+subViewCols+` FROM payment_subscriptions s
		LEFT JOIN payment_prices p ON p.key = s.price_key `+where, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SubscriptionView])
}

// CurrentSubscription returns the user's subscription that matters now: a
// live one if any (newest first), else the most recent. nil when none.
func CurrentSubscription(ctx context.Context, q db.Querier, userID uuid.UUID) (*SubscriptionView, error) {
	list, err := querySubscriptions(ctx, q, `WHERE s.user_id = $1
		ORDER BY (s.status IN ('trialing', 'active', 'past_due', 'unpaid', 'paused')) DESC, s.created_at DESC, s.id DESC
		LIMIT 1`, userID)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

// GET /v1/payments/subscription → {"subscription": …|null}
func (s *Service) getSubscription(w http.ResponseWriter, r *http.Request) error {
	sub, err := CurrentSubscription(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"subscription": sub})
	return nil
}

// Entitlement is something the user currently holds.
type Entitlement struct {
	Key         string     `json:"key"`
	Source      string     `json:"source"` // order | subscription
	ActiveUntil *time.Time `json:"active_until"`
}

// ActiveEntitlements lists the user's current entitlements, one per key
// (the longest-lasting grant wins).
func ActiveEntitlements(ctx context.Context, q db.Querier, userID uuid.UUID) ([]Entitlement, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (key) key, source, active_until FROM payment_entitlements
		WHERE user_id = $1 AND (active_until IS NULL OR active_until > now())
		ORDER BY key, active_until DESC NULLS FIRST`, userID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Entitlement])
	if out == nil {
		out = []Entitlement{}
	}
	return out, err
}

// GET /v1/payments/entitlements
func (s *Service) listEntitlements(w http.ResponseWriter, r *http.Request) error {
	list, err := ActiveEntitlements(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

// ---- ledger (exported read helpers) -------------------------------------------

// LedgerEntry is one money movement as its owner sees it: positive amounts
// were paid, negative amounts were refunded.
type LedgerEntry struct {
	ID             int64      `json:"id"`
	Kind           string     `json:"kind"` // payment | invoice | refund
	AmountMinor    int64      `json:"amount_minor"`
	Currency       string     `json:"currency"`
	Description    string     `json:"description"`
	OrderID        *uuid.UUID `json:"order_id"`
	SubscriptionID *uuid.UUID `json:"subscription_id"`
	CreatedAt      time.Time  `json:"created_at"`
}

type ledgerCursor struct {
	T  time.Time `json:"t"`
	ID int64     `json:"id"`
}

// ListLedger pages a user's ledger newest first (keyset on created_at, id).
func ListLedger(ctx context.Context, q db.Querier, userID uuid.UUID, p httpx.PageParams) (httpx.Page[LedgerEntry], error) {
	var c ledgerCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return httpx.Page[LedgerEntry]{}, err
	}
	var after *time.Time
	if !c.T.IsZero() {
		after = &c.T
	}
	rows, err := q.Query(ctx, `
		SELECT id, kind, amount_minor, currency, description, order_id, subscription_id, created_at
		FROM payment_ledger
		WHERE user_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, userID, after, c.ID, p.Limit+1)
	if err != nil {
		return httpx.Page[LedgerEntry]{}, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[LedgerEntry])
	if err != nil {
		return httpx.Page[LedgerEntry]{}, err
	}
	return httpx.NewPage(list, p.Limit, func(e LedgerEntry) any { return ledgerCursor{e.CreatedAt, e.ID} }), nil
}

// NetPaid sums a user's ledger per currency (payments minus refunds).
func NetPaid(ctx context.Context, q db.Querier, userID uuid.UUID) (map[string]int64, error) {
	rows, err := q.Query(ctx, `SELECT currency, sum(amount_minor)::bigint FROM payment_ledger
		WHERE user_id = $1 GROUP BY currency`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var cur string
		var sum int64
		if err := rows.Scan(&cur, &sum); err != nil {
			return nil, err
		}
		out[cur] = sum
	}
	return out, rows.Err()
}

// HasPaidOrder reports whether the user holds a paid (not fully refunded)
// order carrying ref, the reference stitching code passed to Checkout.
func HasPaidOrder(ctx context.Context, q db.Querier, userID uuid.UUID, ref string) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM payment_orders
		WHERE user_id = $1 AND ref = $2 AND status IN ('paid', 'partially_refunded'))`, userID, ref).Scan(&ok)
	return ok, err
}

// GET /v1/payments/history
func (s *Service) history(w http.ResponseWriter, r *http.Request) error {
	page, err := ListLedger(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID, httpx.ParsePage(r))
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}
