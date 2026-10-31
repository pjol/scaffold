package payments

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AdminCustomer is a user with a provider customer.
type AdminCustomer struct {
	ID                 uuid.UUID  `json:"id"`
	UserID             uuid.UUID  `json:"user_id"`
	Email              string     `json:"email"`
	DisplayName        string     `json:"display_name"`
	UserStatus         string     `json:"user_status"`
	Provider           string     `json:"provider"`
	ProviderCustomerID string     `json:"provider_customer_id"`
	DeleteAfter        *time.Time `json:"delete_after"`
	CreatedAt          time.Time  `json:"created_at"`
}

const customerCols = `c.id, c.user_id, u.email, u.display_name, u.status, c.provider, c.provider_customer_id,
	c.delete_after, c.created_at`

type timeCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func cursorTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// GET /v1/admin/payments/customers?q= (email, name, user id or provider customer id)
func (s *Service) adminListCustomers(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c timeCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+customerCols+` FROM payment_customers c JOIN users u ON u.id = c.user_id
		WHERE ($1 = '' OR u.email ILIKE '%' || $1 || '%' OR u.display_name ILIKE '%' || $1 || '%'
		       OR c.user_id::text = $1 OR c.provider_customer_id = $1)
		  AND ($2::timestamptz IS NULL OR (c.created_at, c.id) < ($2, $3))
		ORDER BY c.created_at DESC, c.id DESC LIMIT $4`, q, cursorTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminCustomer])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a AdminCustomer) any { return timeCursor{a.CreatedAt, a.ID} }))
	return nil
}

// AdminCustomerDetail is one user's payments at a glance.
type AdminCustomerDetail struct {
	UserID        uuid.UUID          `json:"user_id"`
	Customer      *AdminCustomer     `json:"customer"`
	Subscriptions []SubscriptionView `json:"subscriptions"`
	Entitlements  []Entitlement      `json:"entitlements"`
	Orders        []Order            `json:"orders"`
	Refunds       []AdminRefund      `json:"refunds"`
	NetPaid       map[string]int64   `json:"net_paid"`
}

// GET /v1/admin/payments/customers/{user_id}
func (s *Service) adminGetCustomer(w http.ResponseWriter, r *http.Request) error {
	userID, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return httpx.ErrNotFound
	}
	out := AdminCustomerDetail{UserID: userID}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+customerCols+` FROM payment_customers c JOIN users u ON u.id = c.user_id
		WHERE c.user_id = $1 ORDER BY c.created_at DESC LIMIT 1`, userID)
	if err != nil {
		return err
	}
	customers, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminCustomer])
	if err != nil {
		return err
	}
	if len(customers) > 0 {
		out.Customer = &customers[0]
	}
	if out.Subscriptions, err = querySubscriptions(ctx, s.d.Pool, `WHERE s.user_id = $1 ORDER BY s.created_at DESC LIMIT 20`, userID); err != nil {
		return err
	}
	if out.Entitlements, err = ActiveEntitlements(ctx, s.d.Pool, userID); err != nil {
		return err
	}
	if out.Orders, err = queryOrders(ctx, s.d.Pool, `WHERE user_id = $1 ORDER BY created_at DESC LIMIT 20`, userID); err != nil {
		return err
	}
	if out.Refunds, err = s.queryRefunds(r, `WHERE r.user_id = $1 ORDER BY r.created_at DESC LIMIT 20`, userID); err != nil {
		return err
	}
	if out.NetPaid, err = NetPaid(ctx, s.d.Pool, userID); err != nil {
		return err
	}
	if out.Subscriptions == nil {
		out.Subscriptions = []SubscriptionView{}
	}
	httpx.OK(w, out)
	return nil
}

func queryOrders(ctx context.Context, q db.Querier, where string, args ...any) ([]Order, error) {
	rows, err := q.Query(ctx, `SELECT `+orderCols+` FROM payment_orders `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// AdminLedgerEntry is a ledger row as staff see it (refunds target these).
type AdminLedgerEntry struct {
	ID                int64      `json:"id"`
	UserID            uuid.UUID  `json:"user_id"`
	Kind              string     `json:"kind"`
	AmountMinor       int64      `json:"amount_minor"`
	Currency          string     `json:"currency"`
	Description       string     `json:"description"`
	Provider          string     `json:"provider"`
	ProviderRef       string     `json:"provider_ref"`
	ProviderPaymentID *string    `json:"provider_payment_id"`
	OrderID           *uuid.UUID `json:"order_id"`
	SubscriptionID    *uuid.UUID `json:"subscription_id"`
	RefundedMinor     int64      `json:"refunded_minor"` // payments/invoices: refunds succeeded or pending against it
	CreatedAt         time.Time  `json:"created_at"`
}

// GET /v1/admin/payments/ledger?user_id=
func (s *Service) adminLedger(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c ledgerCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var userID *uuid.UUID
	if v := r.URL.Query().Get("user_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		userID = &id
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT l.id, l.user_id, l.kind, l.amount_minor, l.currency, l.description, l.provider, l.provider_ref,
		       l.provider_payment_id, l.order_id, l.subscription_id,
		       CASE WHEN l.kind = 'refund' THEN 0 ELSE COALESCE((
		         SELECT sum(rf.amount_minor) FROM payment_refunds rf
		         WHERE rf.ledger_id = l.id AND rf.status IN ('pending', 'succeeded')), 0) END::bigint,
		       l.created_at
		FROM payment_ledger l
		WHERE ($1::uuid IS NULL OR l.user_id = $1)
		  AND ($2::timestamptz IS NULL OR (l.created_at, l.id) < ($2, $3))
		ORDER BY l.created_at DESC, l.id DESC LIMIT $4`, userID, cursorTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminLedgerEntry])
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(e AdminLedgerEntry) any { return ledgerCursor{e.CreatedAt, e.ID} }))
	return nil
}

// GET /v1/admin/payments/refunds?user_id=
func (s *Service) adminListRefunds(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c timeCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var userID *uuid.UUID
	if v := r.URL.Query().Get("user_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid user_id.")
		}
		userID = &id
	}
	list, err := s.queryRefunds(r, `
		WHERE ($1::uuid IS NULL OR r.user_id = $1) AND ($2::timestamptz IS NULL OR (r.created_at, r.id) < ($2, $3))
		ORDER BY r.created_at DESC, r.id DESC LIMIT $4`, userID, cursorTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(a AdminRefund) any { return timeCursor{a.CreatedAt, a.ID} }))
	return nil
}
