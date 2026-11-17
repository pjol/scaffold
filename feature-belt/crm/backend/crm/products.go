package crm

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// The product library: what an org sells, with a price in one currency,
// billed once or on a schedule, with a default tax rate and discount. Line
// items copy a product's fields when they are added, so editing or archiving
// a product never changes a deal or a quote.

// Product is a crm_products row.
type Product struct {
	ID            uuid.UUID  `json:"id"`
	OrgID         uuid.UUID  `json:"org_id"`
	Name          string     `json:"name"`
	SKU           *string    `json:"sku"`
	Description   string     `json:"description"`
	UnitPrice     int64      `json:"unit_price"`
	Currency      string     `json:"currency"`
	Billing       string     `json:"billing"`
	BillingPeriod *string    `json:"billing_period"`
	TermPeriods   *int       `json:"term_periods"`
	TaxBP         int        `json:"tax_bp"`
	DiscountBP    int        `json:"discount_bp"`
	ArchivedAt    *time.Time `json:"archived_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const productCols = `id, org_id, name, sku, description, unit_price, currency, billing, billing_period, term_periods, tax_bp, discount_bp,
	archived_at, created_at, updated_at`

func scanProduct(row pgx.Row) (*Product, error) {
	p := &Product{}
	err := row.Scan(&p.ID, &p.OrgID, &p.Name, &p.SKU, &p.Description, &p.UnitPrice, &p.Currency, &p.Billing, &p.BillingPeriod,
		&p.TermPeriods, &p.TaxBP, &p.DiscountBP, &p.ArchivedAt, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Service) loadProduct(ctx context.Context, q db.Querier, orgID, id uuid.UUID) (*Product, error) {
	p, err := scanProduct(q.QueryRow(ctx, `SELECT `+productCols+` FROM crm_products WHERE id = $1 AND org_id = $2`, id, orgID))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Product not found.")
	}
	return p, err
}

type productCursor struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"id"`
}

// listProducts: GET /v1/crm/orgs/{orgID}/products?q=&archived=true&cursor=&limit=
// (crm.access): by name, archived ones only when asked.
func (s *Service) listProducts(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	page := httpx.ParsePage(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	archived := r.URL.Query().Get("archived") == "true"
	args := []any{orgID, archived, page.Limit + 1}
	where := `org_id = $1 AND (archived_at IS NOT NULL) = $2`
	if q != "" {
		args = append(args, likePattern(clip(q, 200)))
		where += ` AND (name ILIKE $4 OR COALESCE(sku, '') ILIKE $4)`
	}
	if page.Cursor != "" {
		var c productCursor
		if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
			return err
		}
		args = append(args, strings.ToLower(c.Name), c.ID)
		n := len(args)
		where += ` AND (lower(name), id) > ($` + itoa(n-1) + `, $` + itoa(n) + `)`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+productCols+` FROM crm_products WHERE `+where+` ORDER BY lower(name), id LIMIT $3`, args...)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Product, error) { return scanProduct(row) })
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, page.Limit, func(p *Product) any { return productCursor{Name: p.Name, ID: p.ID} }))
	return nil
}

type productReq struct {
	Name          *string `json:"name"`
	SKU           *string `json:"sku"`
	Description   *string `json:"description"`
	UnitPrice     *int64  `json:"unit_price"`
	Currency      *string `json:"currency"`
	Billing       *string `json:"billing"`
	BillingPeriod *string `json:"billing_period"`
	TermPeriods   *int    `json:"term_periods"`
	TaxBP         *int    `json:"tax_bp"`
	DiscountBP    *int    `json:"discount_bp"`
	Archived      *bool   `json:"archived"`
}

// apply validates req onto p (a new or existing product).
func (req productReq) apply(p *Product) error {
	if req.Name != nil {
		p.Name = strings.TrimSpace(*req.Name)
	}
	if p.Name == "" || len([]rune(p.Name)) > 200 {
		return httpx.BadRequest("Give the product a name of up to 200 characters.")
	}
	if req.SKU != nil {
		v := strings.TrimSpace(*req.SKU)
		if len(v) > 64 {
			return httpx.BadRequest("A SKU can be at most 64 characters.")
		}
		p.SKU = nil
		if v != "" {
			p.SKU = &v
		}
	}
	if req.Description != nil {
		p.Description = strings.TrimSpace(*req.Description)
		if len([]rune(p.Description)) > 2000 {
			return httpx.BadRequest("A description can be at most 2,000 characters.")
		}
	}
	if req.UnitPrice != nil {
		p.UnitPrice = *req.UnitPrice
	}
	if p.UnitPrice < 0 || p.UnitPrice > maxMoney {
		return httpx.BadRequest("Unit price is a whole number of minor units (cents), 0 or more.")
	}
	if req.Currency != nil {
		p.Currency = strings.ToUpper(strings.TrimSpace(*req.Currency))
	}
	if !currencyRe.MatchString(p.Currency) {
		return httpx.BadRequest("Currency must be a 3-letter code like USD.")
	}
	if req.Billing != nil {
		p.Billing = *req.Billing
	}
	if req.BillingPeriod != nil {
		p.BillingPeriod = req.BillingPeriod
		if *req.BillingPeriod == "" {
			p.BillingPeriod = nil
		}
	}
	if req.TermPeriods != nil {
		p.TermPeriods = req.TermPeriods
		if *req.TermPeriods == 0 {
			p.TermPeriods = nil
		}
	}
	switch p.Billing {
	case "", "one_time":
		p.Billing, p.BillingPeriod, p.TermPeriods = "one_time", nil, nil
	case "recurring":
		if p.BillingPeriod == nil || billingMonths[*p.BillingPeriod] == 0 {
			return httpx.BadRequest("Choose how often it bills: monthly, quarterly, semiannual or annual.")
		}
		if p.TermPeriods != nil && (*p.TermPeriods < 1 || *p.TermPeriods > 120) {
			return httpx.BadRequest("A term is 1 to 120 billing periods.")
		}
	default:
		return httpx.BadRequest("billing is one_time or recurring.")
	}
	if req.TaxBP != nil {
		p.TaxBP = *req.TaxBP
	}
	if req.DiscountBP != nil {
		p.DiscountBP = *req.DiscountBP
	}
	if p.TaxBP < 0 || p.TaxBP > 10000 || p.DiscountBP < 0 || p.DiscountBP > 10000 {
		return httpx.BadRequest("Tax and discount are 0 to 100 % (in basis points: 825 = 8.25 %).")
	}
	return nil
}

// createProduct: POST /v1/crm/orgs/{orgID}/products (crm.manage_products, audited).
func (s *Service) createProduct(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req productReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p := &Product{}
	if req.Currency == nil {
		if err := s.d.Pool.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&p.Currency); err != nil {
			return err
		}
	}
	if err := req.apply(p); err != nil {
		return err
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_products WHERE org_id = $1`, orgID).Scan(&n); err != nil {
			return err
		}
		if n >= 5000 {
			return httpx.BadRequest(orgNounA() + " can have at most 5,000 products.")
		}
		err := tx.QueryRow(ctx, `INSERT INTO crm_products (org_id, name, sku, description, unit_price, currency, billing, billing_period, term_periods,
			tax_bp, discount_bp, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
			orgID, p.Name, p.SKU, p.Description, p.UnitPrice, p.Currency, p.Billing, p.BillingPeriod, p.TermPeriods, p.TaxBP, p.DiscountBP, me(r)).Scan(&p.ID)
		if db.IsUniqueViolation(err, "crm_products_sku_key") {
			return httpx.Conflict("Another product has that SKU.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.product.create", "crm.product", p.ID.String(), map[string]any{"org_id": orgID, "name": p.Name})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "products")
	out, err := s.loadProduct(ctx, s.d.Pool, orgID, p.ID)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// updateProduct: PATCH /v1/crm/orgs/{orgID}/products/{productID} (crm.manage_products, audited).
// archived: true hides it from pickers; existing line items keep their copy.
func (s *Service) updateProduct(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "productID")
	if err != nil {
		return err
	}
	var req productReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		p, err := scanProduct(tx.QueryRow(ctx, `SELECT `+productCols+` FROM crm_products WHERE id = $1 AND org_id = $2 FOR UPDATE`, id, orgID))
		if db.IsNoRows(err) {
			return httpx.NotFound("Product not found.")
		}
		if err != nil {
			return err
		}
		if err := req.apply(p); err != nil {
			return err
		}
		archived := p.ArchivedAt != nil
		if req.Archived != nil {
			archived = *req.Archived
		}
		_, err = tx.Exec(ctx, `UPDATE crm_products SET name = $2, sku = $3, description = $4, unit_price = $5, currency = $6, billing = $7,
			billing_period = $8, term_periods = $9, tax_bp = $10, discount_bp = $11,
			archived_at = CASE WHEN $12 THEN COALESCE(archived_at, now()) ELSE NULL END, updated_at = now() WHERE id = $1`,
			id, p.Name, p.SKU, p.Description, p.UnitPrice, p.Currency, p.Billing, p.BillingPeriod, p.TermPeriods, p.TaxBP, p.DiscountBP, archived)
		if db.IsUniqueViolation(err, "crm_products_sku_key") {
			return httpx.Conflict("Another product has that SKU.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.product.update", "crm.product", id.String(), map[string]any{"org_id": orgID, "name": p.Name, "archived": archived})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "products")
	out, err := s.loadProduct(ctx, s.d.Pool, orgID, id)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// deleteProduct: DELETE /v1/crm/orgs/{orgID}/products/{productID} (crm.manage_products,
// audited). Line items and quotes keep their copies of its fields.
func (s *Service) deleteProduct(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "productID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM crm_products WHERE id = $1 AND org_id = $2 RETURNING name`, id, orgID).Scan(&name); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Product not found.")
			}
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.product.delete", "crm.product", id.String(), map[string]any{"org_id": orgID, "name": name})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "products")
	httpx.NoContent(w)
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// jsonNumberText reads a JSON number or numeric string as text ("1.5").
func jsonNumberText(raw json.RawMessage) (string, bool) {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String(), true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	return "", false
}
