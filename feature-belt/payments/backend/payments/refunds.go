package payments

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AdminRefund is a refund as staff see it.
type AdminRefund struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"user_id"`
	LedgerID         int64      `json:"ledger_id"`
	OrderID          *uuid.UUID `json:"order_id"`
	Provider         string     `json:"provider"`
	ProviderRefundID *string    `json:"provider_refund_id"`
	AmountMinor      int64      `json:"amount_minor"`
	Currency         string     `json:"currency"`
	Reason           string     `json:"reason"`
	Note             string     `json:"note"`
	Status           string     `json:"status"`
	RequestedBy      *uuid.UUID `json:"requested_by"`
	RequestedByEmail *string    `json:"requested_by_email"`
	CreatedAt        time.Time  `json:"created_at"`
}

func (s *Service) queryRefunds(r *http.Request, where string, args ...any) ([]AdminRefund, error) {
	return queryRefunds(r.Context(), s.d.Pool, where, args...)
}

func queryRefunds(ctx context.Context, q db.Querier, where string, args ...any) ([]AdminRefund, error) {
	rows, err := q.Query(ctx, `
		SELECT r.id, r.user_id, r.ledger_id, r.order_id, r.provider, r.provider_refund_id, r.amount_minor, r.currency,
		       r.reason, r.note, r.status, r.requested_by, u.email, r.created_at
		FROM payment_refunds r LEFT JOIN users u ON u.id = r.requested_by `+where, args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminRefund])
	if out == nil {
		out = []AdminRefund{}
	}
	return out, err
}

type refundReq struct {
	LedgerID    int64  `json:"ledger_id"`
	AmountMinor *int64 `json:"amount_minor"` // default: everything not yet refunded
	Reason      string `json:"reason"`       // duplicate | fraudulent | requested_by_customer | ""
	Note        string `json:"note"`
}

// POST /v1/admin/payments/refunds (admins; Idempotency-Key required).
//
// The refund row, the audit entry, the provider call and the result commit
// together. The route is detached from the client, so a dropped connection
// can't roll back after the provider refunded; a retry with the same key
// replays the stored refund, and the provider's own idempotency covers a
// retry that races a lost response. If the commit itself fails after the
// provider acted, the charge.refunded webhook records the refund.
func (s *Service) adminRefund(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	ctx := r.Context()
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 100 {
		return httpx.NewError(http.StatusBadRequest, "idempotency_key_required", "Send an Idempotency-Key header (up to 100 characters).")
	}
	var req refundReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.LedgerID <= 0 {
		return httpx.BadRequest("ledger_id is required.")
	}
	switch req.Reason {
	case "", "duplicate", "fraudulent", "requested_by_customer":
	default:
		return httpx.BadRequest("Reason must be duplicate, fraudulent, requested_by_customer or empty.")
	}
	req.Note = strings.TrimSpace(req.Note)
	if len([]rune(req.Note)) > 500 {
		return httpx.BadRequest("Note must be 500 characters or fewer.")
	}
	if replay, err := s.refundByKey(ctx, key); err != nil || replay != nil {
		if replay != nil {
			httpx.OK(w, replay)
		}
		return err
	}

	var refundID uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var (
			userID         uuid.UUID
			kind, provider string
			amount         int64
			currency       string
			paymentID      *string
			orderID        *uuid.UUID
			dest           *uuid.UUID
		)
		err := tx.QueryRow(ctx, `
			SELECT l.user_id, l.kind, l.provider, l.amount_minor, l.currency, l.provider_payment_id, l.order_id,
			       COALESCE(o.destination_user_id, sub.destination_user_id)
			FROM payment_ledger l
			LEFT JOIN payment_orders o ON o.id = l.order_id
			LEFT JOIN payment_subscriptions sub ON sub.id = l.subscription_id
			WHERE l.id = $1 FOR UPDATE OF l`, req.LedgerID).
			Scan(&userID, &kind, &provider, &amount, &currency, &paymentID, &orderID, &dest)
		if db.IsNoRows(err) {
			return httpx.NotFound("No such ledger entry.")
		}
		if err != nil {
			return err
		}
		if kind == "refund" || paymentID == nil {
			return httpx.BadRequest("Only payments and invoices can be refunded.")
		}
		if provider != s.gw.Name() {
			return httpx.NewError(http.StatusConflict, "other_provider", "This payment was recorded by "+provider+" and can't be refunded here.")
		}
		var refunded int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor), 0) FROM payment_refunds
			WHERE ledger_id = $1 AND status IN ('pending', 'succeeded')`, req.LedgerID).Scan(&refunded); err != nil {
			return err
		}
		remaining := amount - refunded
		if remaining <= 0 {
			return httpx.NewError(http.StatusConflict, "nothing_to_refund", "This payment is already fully refunded.")
		}
		want := remaining
		if req.AmountMinor != nil {
			want = *req.AmountMinor
		}
		if want <= 0 || want > remaining {
			return httpx.BadRequest("Amount must be more than 0 and at most what's left to refund.")
		}
		err = tx.QueryRow(ctx, `
			INSERT INTO payment_refunds (user_id, ledger_id, order_id, provider, provider_payment_id, amount_minor, currency,
			  reason, note, status, requested_by, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', $10, $11) RETURNING id`,
			userID, req.LedgerID, orderID, provider, *paymentID, want, currency, req.Reason, req.Note,
			staff.UserID, key).Scan(&refundID)
		if db.IsUniqueViolation(err) {
			return httpx.Conflict("A refund with this Idempotency-Key is already in progress.")
		}
		if err != nil {
			return err
		}
		if err := audit.Log(ctx, tx, staff.UserID, "payments.refund.create", "payment_refund", refundID.String(),
			map[string]any{"ledger_id": req.LedgerID, "user_id": userID, "amount_minor": want, "currency": currency,
				"reason": req.Reason}); err != nil {
			return err
		}
		res, err := s.gw.Refund(ctx, RefundInput{
			PaymentID: *paymentID, Amount: want, Reason: req.Reason, ReverseTransfer: dest != nil,
			Metadata: map[string]string{"refund_id": refundID.String(), "user_id": userID.String()},
		}, "refund:"+key)
		if err != nil {
			return providerFailure(ctx, "refund", err)
		}
		status := res.Status
		switch status {
		case RefundPending, RefundSucceeded, RefundFailed, RefundCanceled:
		default:
			status = RefundPending
		}
		if _, err := tx.Exec(ctx, `UPDATE payment_refunds SET provider_refund_id = $2, status = $3, updated_at = now()
			WHERE id = $1`, refundID, res.ID, status); err != nil {
			if db.IsUniqueViolation(err) {
				// The webhook recorded this provider refund first; keep that row.
				return errRefundRecordedElsewhere
			}
			return err
		}
		if status != RefundSucceeded {
			return nil
		}
		var subID *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT subscription_id FROM payment_ledger WHERE id = $1`, req.LedgerID).Scan(&subID); err != nil {
			return err
		}
		if err := s.addLedger(ctx, tx, ledgerInsert{
			UserID: userID, Kind: "refund", Amount: -want, Currency: currency, ProviderRef: res.ID,
			PaymentID: *paymentID, OrderID: orderID, SubscriptionID: subID, Description: "Refund", Key: "refund:" + res.ID,
		}); err != nil {
			return err
		}
		return s.settleOrders(ctx, tx, *paymentID)
	})
	if errors.Is(err, errRefundRecordedElsewhere) {
		return httpx.NewError(http.StatusConflict, "refund_recorded", "The provider refunded it and the record already exists. Refresh to see it.")
	}
	if err != nil {
		return err
	}
	out, err := queryRefunds(ctx, s.d.Pool, `WHERE r.id = $1`, refundID)
	if err != nil || len(out) == 0 {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out[0])
	return nil
}

var errRefundRecordedElsewhere = errors.New("payments: refund already recorded by the webhook")

func (s *Service) refundByKey(ctx context.Context, key string) (*AdminRefund, error) {
	out, err := queryRefunds(ctx, s.d.Pool, `WHERE r.idempotency_key = $1`, key)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return &out[0], nil
}
