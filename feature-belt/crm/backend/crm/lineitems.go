package crm

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Line items on a deal, in the deal's currency: whoever can open the deal
// sees them, whoever can edit it changes them. When the deal's amount follows
// its line items, every change rewrites the amount to their contract value
// (on the deal's timeline like any field change).

// LineItem is a crm_line_items row with its computed money.
type LineItem struct {
	ID             uuid.UUID  `json:"id"`
	DealID         uuid.UUID  `json:"deal_id"`
	ProductID      *uuid.UUID `json:"product_id"`
	Position       int        `json:"position"`
	Name           string     `json:"name"`
	SKU            string     `json:"sku"`
	Description    string     `json:"description"`
	UnitPrice      int64      `json:"unit_price"`
	Quantity       float64    `json:"quantity"`
	DiscountBP     int        `json:"discount_bp"`
	DiscountAmount int64      `json:"discount_amount"`
	TaxBP          int        `json:"tax_bp"`
	Billing        string     `json:"billing"`
	BillingPeriod  *string    `json:"billing_period"`
	TermPeriods    int        `json:"term_periods"`
	LineTotals
	CreatedAt time.Time `json:"created_at"`

	qtyMilli int64
}

func (li *LineItem) input() lineInput {
	return lineInput{UnitPrice: li.UnitPrice, QuantityMilli: li.qtyMilli, DiscountBP: li.DiscountBP, DiscountAmount: li.DiscountAmount,
		TaxBP: li.TaxBP, Billing: li.Billing, BillingPeriod: deref(li.BillingPeriod), TermPeriods: li.TermPeriods}
}

// LineItems is GET /v1/crm/deals/{id}/line-items.
type LineItems struct {
	Items           []*LineItem `json:"items"`
	Totals          Totals      `json:"totals"`
	Currency        string      `json:"currency"`
	AmountFromItems bool        `json:"amount_from_items"`
	CanEdit         bool        `json:"can_edit"`
}

const lineItemCols = `id, deal_id, product_id, position, name, sku, description, unit_price, quantity_milli, discount_bp, discount_amount,
	tax_bp, billing, billing_period, term_periods, created_at`

func scanLineItem(row pgx.Row) (*LineItem, error) {
	li := &LineItem{}
	err := row.Scan(&li.ID, &li.DealID, &li.ProductID, &li.Position, &li.Name, &li.SKU, &li.Description, &li.UnitPrice, &li.qtyMilli,
		&li.DiscountBP, &li.DiscountAmount, &li.TaxBP, &li.Billing, &li.BillingPeriod, &li.TermPeriods, &li.CreatedAt)
	li.Quantity = quantityValue(li.qtyMilli)
	return li, err
}

// dealLineItems loads a deal's lines with totals (no access check).
func dealLineItems(ctx context.Context, q db.Querier, dealID uuid.UUID) ([]*LineItem, Totals, error) {
	rows, err := q.Query(ctx, `SELECT `+lineItemCols+` FROM crm_line_items WHERE deal_id = $1 ORDER BY position, created_at, id`, dealID)
	if err != nil {
		return nil, Totals{}, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*LineItem, error) { return scanLineItem(row) })
	if err != nil {
		return nil, Totals{}, err
	}
	in := make([]lineInput, len(list))
	for i, li := range list {
		in[i] = li.input()
	}
	t, lines, _ := sumLines(in)
	for i := range list {
		list[i].LineTotals = lines[i]
	}
	if list == nil {
		list = []*LineItem{}
	}
	return list, t, nil
}

// listLineItems: GET /v1/crm/deals/{id}/line-items (view on the deal).
func (s *Service) listLineItems(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	out, err := s.lineItemsOf(ctx, me(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) lineItemsOf(ctx context.Context, userID, dealID uuid.UUID) (*LineItems, error) {
	_, lvl, err := s.requireRecord(ctx, s.d.Pool, userID, objDeal, dealID, access.View)
	if err != nil {
		return nil, err
	}
	items, totals, err := dealLineItems(ctx, s.d.Pool, dealID)
	if err != nil {
		return nil, err
	}
	out := &LineItems{Items: items, Totals: totals, CanEdit: lvl >= access.Edit}
	if err := s.d.Pool.QueryRow(ctx, `SELECT currency, amount_from_items FROM crm_deals WHERE id = $1`, dealID).Scan(&out.Currency, &out.AmountFromItems); err != nil {
		return nil, err
	}
	return out, nil
}

// lineItemReq adds or changes a line. Adding from a product copies its name,
// SKU, description, price, billing, tax and default discount (each can be
// overridden in the same request).
type lineItemReq struct {
	ProductID      *uuid.UUID      `json:"product_id"`
	Name           *string         `json:"name"`
	SKU            *string         `json:"sku"`
	Description    *string         `json:"description"`
	UnitPrice      *int64          `json:"unit_price"`
	Quantity       json.RawMessage `json:"quantity"`
	DiscountBP     *int            `json:"discount_bp"`
	DiscountAmount *int64          `json:"discount_amount"`
	TaxBP          *int            `json:"tax_bp"`
	Billing        *string         `json:"billing"`
	BillingPeriod  *string         `json:"billing_period"`
	TermPeriods    *int            `json:"term_periods"`
}

// apply validates req onto li. currency is the deal's.
func (s *Service) applyLineItem(ctx context.Context, q db.Querier, orgID uuid.UUID, currency string, req lineItemReq, li *LineItem, isNew bool) error {
	if isNew && req.ProductID != nil {
		p, err := s.loadProduct(ctx, q, orgID, *req.ProductID)
		if err != nil {
			return err
		}
		if p.ArchivedAt != nil {
			return httpx.BadRequest("That product is archived.")
		}
		if p.Currency != currency {
			return httpx.NewError(http.StatusConflict, "currency_mismatch",
				"“"+p.Name+"” is priced in "+p.Currency+" and this deal is in "+currency+". Add it with a price in "+currency+", or change the deal's currency first.")
		}
		li.ProductID, li.Name, li.SKU, li.Description, li.UnitPrice = &p.ID, p.Name, deref(p.SKU), p.Description, p.UnitPrice
		li.Billing, li.BillingPeriod, li.TaxBP, li.DiscountBP = p.Billing, p.BillingPeriod, p.TaxBP, p.DiscountBP
		li.TermPeriods = 1
		if p.TermPeriods != nil {
			li.TermPeriods = *p.TermPeriods
		} else if p.BillingPeriod != nil {
			li.TermPeriods = 12 / billingMonths[*p.BillingPeriod]
		}
	}
	if req.Name != nil {
		li.Name = strings.TrimSpace(*req.Name)
	}
	if li.Name == "" || len([]rune(li.Name)) > 200 {
		return httpx.BadRequest("Give the line a name of up to 200 characters.")
	}
	if req.SKU != nil {
		li.SKU = clip(*req.SKU, 64)
	}
	if req.Description != nil {
		li.Description = strings.TrimSpace(*req.Description)
		if len([]rune(li.Description)) > 2000 {
			return httpx.BadRequest("A description can be at most 2,000 characters.")
		}
	}
	if req.UnitPrice != nil {
		li.UnitPrice = *req.UnitPrice
	}
	if li.UnitPrice < 0 || li.UnitPrice > maxMoney {
		return httpx.BadRequest("Unit price is a whole number of minor units (cents), 0 or more.")
	}
	if len(req.Quantity) > 0 && string(req.Quantity) != "null" {
		txt, ok := jsonNumberText(req.Quantity)
		q, okq := parseQuantity(txt)
		if !ok || !okq {
			return httpx.BadRequest("Quantity is a number above 0 with up to 3 decimals (at most 1,000,000).")
		}
		li.qtyMilli = q
	}
	if li.qtyMilli == 0 {
		li.qtyMilli = 1000
	}
	if req.DiscountBP != nil {
		li.DiscountBP = *req.DiscountBP
		if li.DiscountBP > 0 {
			li.DiscountAmount = 0
		}
	}
	if req.DiscountAmount != nil {
		li.DiscountAmount = *req.DiscountAmount
		if li.DiscountAmount > 0 {
			li.DiscountBP = 0
		}
	}
	if li.DiscountBP < 0 || li.DiscountBP > 10000 || li.DiscountAmount < 0 || li.DiscountAmount > maxMoney {
		return httpx.BadRequest("A discount is 0 to 100 % or a fixed amount of 0 or more.")
	}
	if req.TaxBP != nil {
		li.TaxBP = *req.TaxBP
	}
	if li.TaxBP < 0 || li.TaxBP > 10000 {
		return httpx.BadRequest("Tax is 0 to 100 % (in basis points: 825 = 8.25 %).")
	}
	if req.Billing != nil {
		li.Billing = *req.Billing
	}
	if req.BillingPeriod != nil {
		li.BillingPeriod = req.BillingPeriod
		if *req.BillingPeriod == "" {
			li.BillingPeriod = nil
		}
	}
	if req.TermPeriods != nil {
		li.TermPeriods = *req.TermPeriods
	}
	switch li.Billing {
	case "", "one_time":
		li.Billing, li.BillingPeriod, li.TermPeriods = "one_time", nil, 1
	case "recurring":
		if li.BillingPeriod == nil || billingMonths[*li.BillingPeriod] == 0 {
			return httpx.BadRequest("Choose how often it bills: monthly, quarterly, semiannual or annual.")
		}
		if li.TermPeriods < 1 || li.TermPeriods > 120 {
			return httpx.BadRequest("A term is 1 to 120 billing periods.")
		}
	default:
		return httpx.BadRequest("billing is one_time or recurring.")
	}
	if _, ok := li.input().totals(); !ok {
		return httpx.BadRequest("That line comes to more than this CRM can hold.")
	}
	return nil
}

// lockDealForItems checks edit on the deal, locks it and returns its org,
// currency and title.
func (s *Service) lockDealForItems(ctx context.Context, tx pgx.Tx, userID, dealID uuid.UUID) (orgID uuid.UUID, currency string, err error) {
	if _, err = tx.Exec(ctx, `SELECT 1 FROM crm_deals WHERE id = $1 FOR UPDATE`, dealID); err != nil {
		return
	}
	if orgID, _, err = s.requireRecord(ctx, tx, userID, objDeal, dealID, access.Edit); err != nil {
		return
	}
	err = tx.QueryRow(ctx, `SELECT currency FROM crm_deals WHERE id = $1`, dealID).Scan(&currency)
	return
}

// addLineItem: POST /v1/crm/deals/{id}/line-items (edit on the deal).
func (s *Service) addLineItem(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req lineItemReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	li, err := s.createLineItem(r.Context(), me(r), id, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, li)
	return nil
}

// createLineItem is the add REST and the agent tool share.
func (s *Service) createLineItem(ctx context.Context, userID, dealID uuid.UUID, req lineItemReq) (*LineItem, error) {
	var itemID uuid.UUID
	var sync *amountSync
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		orgID, currency, err := s.lockDealForItems(ctx, tx, userID, dealID)
		if err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_line_items WHERE deal_id = $1`, dealID).Scan(&n); err != nil {
			return err
		}
		if n >= 200 {
			return httpx.BadRequest("A deal can have at most 200 line items.")
		}
		li := &LineItem{}
		if err := s.applyLineItem(ctx, tx, orgID, currency, req, li, true); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm_line_items (org_id, deal_id, product_id, position, name, sku, description, unit_price, quantity_milli,
			discount_bp, discount_amount, tax_bp, billing, billing_period, term_periods, created_by)
			VALUES ($1, $2, $3, (SELECT COALESCE(max(position) + 1, 0) FROM crm_line_items WHERE deal_id = $2), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
			RETURNING id`, orgID, dealID, li.ProductID, li.Name, li.SKU, li.Description, li.UnitPrice, li.qtyMilli, li.DiscountBP, li.DiscountAmount,
			li.TaxBP, li.Billing, li.BillingPeriod, li.TermPeriods, userID).Scan(&itemID); err != nil {
			return err
		}
		sync, err = s.syncDealAmount(ctx, tx, userID, orgID, dealID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.afterItems(ctx, dealID, sync)
	return s.loadLineItem(ctx, dealID, itemID)
}

func (s *Service) loadLineItem(ctx context.Context, dealID, itemID uuid.UUID) (*LineItem, error) {
	items, _, err := dealLineItems(ctx, s.d.Pool, dealID)
	if err != nil {
		return nil, err
	}
	for _, li := range items {
		if li.ID == itemID {
			return li, nil
		}
	}
	return nil, httpx.NotFound("Line item not found.")
}

// itemDeal returns the deal a line item belongs to (404 when missing).
func (s *Service) itemDeal(ctx context.Context, q db.Querier, itemID uuid.UUID) (uuid.UUID, error) {
	var dealID uuid.UUID
	err := q.QueryRow(ctx, `SELECT deal_id FROM crm_line_items WHERE id = $1`, itemID).Scan(&dealID)
	if db.IsNoRows(err) {
		return uuid.Nil, httpx.NotFound("Line item not found.")
	}
	return dealID, err
}

// updateLineItem: PATCH /v1/crm/line-items/{itemID} (edit on its deal).
func (s *Service) updateLineItem(w http.ResponseWriter, r *http.Request) error {
	itemID, err := idParam(r, "itemID")
	if err != nil {
		return err
	}
	var req lineItemReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	li, err := s.changeLineItem(r.Context(), me(r), itemID, req)
	if err != nil {
		return err
	}
	httpx.OK(w, li)
	return nil
}

func (s *Service) changeLineItem(ctx context.Context, userID, itemID uuid.UUID, req lineItemReq) (*LineItem, error) {
	if req.ProductID != nil {
		return nil, httpx.BadRequest("Remove the line and add the other product instead.")
	}
	dealID, err := s.itemDeal(ctx, s.d.Pool, itemID)
	if err != nil {
		return nil, err
	}
	var sync *amountSync
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		orgID, currency, err := s.lockDealForItems(ctx, tx, userID, dealID)
		if err != nil {
			return err
		}
		li, err := scanLineItem(tx.QueryRow(ctx, `SELECT `+lineItemCols+` FROM crm_line_items WHERE id = $1 AND deal_id = $2`, itemID, dealID))
		if db.IsNoRows(err) {
			return httpx.NotFound("Line item not found.")
		}
		if err != nil {
			return err
		}
		if err := s.applyLineItem(ctx, tx, orgID, currency, req, li, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm_line_items SET name = $2, sku = $3, description = $4, unit_price = $5, quantity_milli = $6,
			discount_bp = $7, discount_amount = $8, tax_bp = $9, billing = $10, billing_period = $11, term_periods = $12, updated_at = now()
			WHERE id = $1`, itemID, li.Name, li.SKU, li.Description, li.UnitPrice, li.qtyMilli, li.DiscountBP, li.DiscountAmount, li.TaxBP,
			li.Billing, li.BillingPeriod, li.TermPeriods); err != nil {
			return err
		}
		sync, err = s.syncDealAmount(ctx, tx, userID, orgID, dealID)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.afterItems(ctx, dealID, sync)
	return s.loadLineItem(ctx, dealID, itemID)
}

// removeLineItem: DELETE /v1/crm/line-items/{itemID} (edit on its deal).
func (s *Service) removeLineItem(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	itemID, err := idParam(r, "itemID")
	if err != nil {
		return err
	}
	dealID, err := s.itemDeal(ctx, s.d.Pool, itemID)
	if err != nil {
		return err
	}
	var sync *amountSync
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		orgID, _, err := s.lockDealForItems(ctx, tx, me(r), dealID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm_line_items WHERE id = $1 AND deal_id = $2`, itemID, dealID); err != nil {
			return err
		}
		sync, err = s.syncDealAmount(ctx, tx, me(r), orgID, dealID)
		return err
	})
	if err != nil {
		return err
	}
	s.afterItems(ctx, dealID, sync)
	httpx.NoContent(w)
	return nil
}

// reorderLineItems: PUT /v1/crm/deals/{id}/line-items/order {ids} (edit).
func (s *Service) reorderLineItems(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	dealID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, _, err := s.lockDealForItems(ctx, tx, me(r), dealID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_line_items WHERE deal_id = $1`, dealID).Scan(&n); err != nil {
			return err
		}
		if n != len(req.IDs) {
			return httpx.BadRequest("List every line item of the deal once.")
		}
		for i, id := range req.IDs {
			tag, err := tx.Exec(ctx, `UPDATE crm_line_items SET position = $3 WHERE id = $1 AND deal_id = $2`, id, dealID, i)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return httpx.BadRequest("List every line item of the deal once.")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.afterItems(ctx, dealID, nil)
	out, err := s.lineItemsOf(ctx, me(r), dealID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// setAmountSource: PUT /v1/crm/deals/{id}/amount-source {from_items} (edit).
// Turning it on sets the amount to the line items' contract value now.
func (s *Service) setAmountSource(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	dealID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		FromItems bool `json:"from_items"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var sync *amountSync
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		orgID, _, err := s.lockDealForItems(ctx, tx, me(r), dealID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm_deals SET amount_from_items = $2, updated_at = now() WHERE id = $1`, dealID, req.FromItems); err != nil {
			return err
		}
		sync, err = s.syncDealAmount(ctx, tx, me(r), orgID, dealID)
		return err
	})
	if err != nil {
		return err
	}
	s.afterItems(ctx, dealID, sync)
	out, err := s.lineItemsOf(ctx, me(r), dealID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// amountSync is a deal amount rewritten from its line items.
type amountSync struct {
	orgID      uuid.UUID
	pipelineID uuid.UUID
	change     change
}

// syncDealAmount sets a deal's amount to its line items' contract value when
// the deal follows them (and the value differs), with a timeline entry.
func (s *Service) syncDealAmount(ctx context.Context, tx pgx.Tx, userID, orgID, dealID uuid.UUID) (*amountSync, error) {
	var follow bool
	var amount *int64
	var currency string
	var pipelineID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT amount_from_items, amount, currency, pipeline_id FROM crm_deals WHERE id = $1`, dealID).
		Scan(&follow, &amount, &currency, &pipelineID); err != nil {
		return nil, err
	}
	if !follow {
		return nil, nil
	}
	_, totals, err := dealLineItems(ctx, tx, dealID)
	if err != nil {
		return nil, err
	}
	if amount != nil && *amount == totals.ContractValue {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE crm_deals SET amount = $2, updated_at = now() WHERE id = $1`, dealID, totals.ContractValue); err != nil {
		return nil, err
	}
	before := ""
	if amount != nil {
		before = formatMoney(*amount, currency)
	}
	c := change{Key: "amount", Label: "Amount", Before: before, After: formatMoney(totals.ContractValue, currency)}
	if err := addEvent(ctx, tx, orgID, objDeal, dealID, "updated", userID, map[string]any{"changes": []change{c}, "from_line_items": true}); err != nil {
		return nil, err
	}
	return &amountSync{orgID: orgID, pipelineID: pipelineID, change: c}, nil
}

// afterItems tells clients a deal's line items (and maybe its amount) changed.
func (s *Service) afterItems(ctx context.Context, dealID uuid.UUID, sync *amountSync) {
	s.recordChanged(ctx, objDeal, dealID, sync != nil)
	if sync != nil {
		s.boardChanged(ctx, sync.pipelineID)
		s.afterWrite(ctx, sync.orgID, objDeal, dealID, "updated", []change{sync.change})
	}
}
