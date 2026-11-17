package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Quotes: a priced offer made from a deal's line items. A quote is an access
// resource (crm.quote) under its deal, so whoever can open the deal can open
// its quotes. A draft copies the deal's line items and can be edited;
// publishing freezes it and makes an access public link (the only link the
// public page accepts is the one publishing made: access checks it is
// unrevoked, unexpired, and that its creator still holds access.share_public
// and crm.publish_quotes). The buyer accepts or declines with their typed
// name; the time, IP address and browser are recorded. AI agents can draft
// quotes but never publish, unpublish or delete them.

// Quote statuses. "Expired" is not stored: a published quote past expires_at.
const (
	QuoteDraft     = "draft"
	QuotePublished = "published"
	QuoteAccepted  = "accepted"
	QuoteDeclined  = "declined"
)

const (
	quoteLinkGrace  = 90 * 24 * time.Hour // the link keeps answering "expired" this long after expiry
	maxQuoteItems   = 200
	quoteTextMax    = 10000
	defaultQuoteTTL = 30 * 24 * time.Hour
)

// QuoteItem is a line of a quote (a copy of a deal line item).
type QuoteItem struct {
	ID             uuid.UUID  `json:"id"`
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

	qtyMilli int64
}

func (it *QuoteItem) input() lineInput {
	return lineInput{UnitPrice: it.UnitPrice, QuantityMilli: it.qtyMilli, DiscountBP: it.DiscountBP, DiscountAmount: it.DiscountAmount,
		TaxBP: it.TaxBP, Billing: it.Billing, BillingPeriod: deref(it.BillingPeriod), TermPeriods: it.TermPeriods}
}

// Quote is a quote as people inside the org see it.
type Quote struct {
	ID            uuid.UUID    `json:"id"`
	OrgID         uuid.UUID    `json:"org_id"`
	Deal          RecordRef    `json:"deal"`
	Number        string       `json:"number"`
	Title         string       `json:"title"`
	Status        string       `json:"status"`
	Expired       bool         `json:"expired"`
	Currency      string       `json:"currency"`
	ExpiresAt     time.Time    `json:"expires_at"`
	Terms         string       `json:"terms"`
	Comments      string       `json:"comments"`
	Contact       *RecordRef   `json:"contact"`
	Sender        *UserRef     `json:"sender"`
	SenderName    string       `json:"sender_name"`
	SenderEmail   string       `json:"sender_email"`
	BuyerName     string       `json:"buyer_name"`
	BuyerCompany  string       `json:"buyer_company"`
	SellerName    string       `json:"seller_name"`
	Items         []*QuoteItem `json:"items"`
	Totals        Totals       `json:"totals"`
	URL           *string      `json:"url"` // published: the public link (people who can edit the quote)
	PublishedAt   *time.Time   `json:"published_at"`
	FirstViewedAt *time.Time   `json:"first_viewed_at"`
	LastViewedAt  *time.Time   `json:"last_viewed_at"`
	ViewCount     int          `json:"view_count"`
	SignerName    *string      `json:"signer_name"`
	SignerEmail   *string      `json:"signer_email"`
	SignerIP      *string      `json:"signer_ip"`
	DecidedAt     *time.Time   `json:"decided_at"`
	DeclineReason *string      `json:"decline_reason"`
	Agent         *string      `json:"agent"`
	CreatedBy     *UserRef     `json:"created_by"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	MyLevel       access.Level `json:"my_level"`
	CanPublish    bool         `json:"can_publish"`

	linkID    *uuid.UUID
	linkToken *string
	total     int64
}

const quoteCols = `q.id, q.org_id, q.deal_id, d.name, q.number, q.title, q.status, q.currency, q.expires_at, q.terms, q.comments,
	q.contact_id, q.sender_id, su.display_name, su.avatar_url, q.sender_name, q.sender_email, q.buyer_name, q.buyer_company, q.seller_name,
	q.link_id, q.link_token, q.published_at, q.first_viewed_at, q.last_viewed_at, q.view_count,
	q.signer_name, q.signer_email, q.signer_ip, q.decided_at, q.decline_reason, q.agent,
	q.created_by, cu.display_name, cu.avatar_url, q.created_at, q.updated_at, q.total`

const quoteFrom = `FROM crm_quotes q JOIN crm_deals d ON d.id = q.deal_id
	LEFT JOIN users su ON su.id = q.sender_id AND su.status <> 'deleted'
	LEFT JOIN users cu ON cu.id = q.created_by AND cu.status <> 'deleted'`

func scanQuote(row pgx.Row) (*Quote, error) {
	q := &Quote{Deal: RecordRef{Type: objDeal}}
	var contactID, senderID, createdBy *uuid.UUID
	var senderName, senderAvatar, creatorName, creatorAvatar *string
	err := row.Scan(&q.ID, &q.OrgID, &q.Deal.ID, &q.Deal.Name, &q.Number, &q.Title, &q.Status, &q.Currency, &q.ExpiresAt, &q.Terms, &q.Comments,
		&contactID, &senderID, &senderName, &senderAvatar, &q.SenderName, &q.SenderEmail, &q.BuyerName, &q.BuyerCompany, &q.SellerName,
		&q.linkID, &q.linkToken, &q.PublishedAt, &q.FirstViewedAt, &q.LastViewedAt, &q.ViewCount,
		&q.SignerName, &q.SignerEmail, &q.SignerIP, &q.DecidedAt, &q.DeclineReason, &q.Agent,
		&createdBy, &creatorName, &creatorAvatar, &q.CreatedAt, &q.UpdatedAt, &q.total)
	if err != nil {
		return nil, err
	}
	if contactID != nil {
		q.Contact = &RecordRef{Type: objContact, ID: *contactID}
	}
	if senderID != nil && senderName != nil {
		q.Sender = &UserRef{ID: *senderID, Name: *senderName, AvatarURL: senderAvatar}
	}
	if createdBy != nil && creatorName != nil {
		q.CreatedBy = &UserRef{ID: *createdBy, Name: *creatorName, AvatarURL: creatorAvatar}
	}
	q.Expired = q.Status == QuotePublished && time.Now().After(q.ExpiresAt)
	return q, nil
}

const quoteItemCols = `id, product_id, position, name, sku, description, unit_price, quantity_milli, discount_bp, discount_amount, tax_bp,
	billing, billing_period, term_periods`

func quoteItems(ctx context.Context, q db.Querier, quoteID uuid.UUID) ([]*QuoteItem, Totals, error) {
	rows, err := q.Query(ctx, `SELECT `+quoteItemCols+` FROM crm_quote_items WHERE quote_id = $1 ORDER BY position, id`, quoteID)
	if err != nil {
		return nil, Totals{}, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*QuoteItem, error) {
		it := &QuoteItem{}
		err := row.Scan(&it.ID, &it.ProductID, &it.Position, &it.Name, &it.SKU, &it.Description, &it.UnitPrice, &it.qtyMilli,
			&it.DiscountBP, &it.DiscountAmount, &it.TaxBP, &it.Billing, &it.BillingPeriod, &it.TermPeriods)
		it.Quantity = quantityValue(it.qtyMilli)
		return it, err
	})
	if err != nil {
		return nil, Totals{}, err
	}
	in := make([]lineInput, len(list))
	for i, it := range list {
		in[i] = it.input()
	}
	t, lines, _ := sumLines(in)
	for i := range list {
		list[i].LineTotals = lines[i]
	}
	if list == nil {
		list = []*QuoteItem{}
	}
	return list, t, nil
}

// requireQuote checks the caller's level on a quote (404 without view, 403
// below min; agent-capped) and crm.access in its org.
func (s *Service) requireQuote(ctx context.Context, q db.Querier, userID, id uuid.UUID, min access.Level) (*Quote, access.Level, error) {
	lvl, err := access.Require(ctx, q, userID, KindQuote, id, min)
	if err != nil {
		return nil, lvl, err
	}
	qt, err := scanQuote(q.QueryRow(ctx, `SELECT `+quoteCols+` `+quoteFrom+` WHERE q.id = $1`, id))
	if db.IsNoRows(err) {
		return nil, access.None, httpx.NotFound("Quote not found.")
	}
	if err != nil {
		return nil, access.None, err
	}
	if !allowedHere(ctx, q, qt.OrgID, userID, PermAccess) {
		return nil, access.None, errNoCRM()
	}
	qt.MyLevel = lvl
	return qt, lvl, nil
}

// quoteDetail is a quote with items, totals and (for editors) its link.
func (s *Service) quoteDetail(ctx context.Context, userID, id uuid.UUID) (*Quote, error) {
	qt, lvl, err := s.requireQuote(ctx, s.d.Pool, userID, id, access.View)
	if err != nil {
		return nil, err
	}
	if qt.Items, qt.Totals, err = quoteItems(ctx, s.d.Pool, id); err != nil {
		return nil, err
	}
	if qt.Contact != nil {
		refs, err := s.visibleRefs(ctx, s.d.Pool, userID, objContact, []uuid.UUID{qt.Contact.ID})
		if err != nil {
			return nil, err
		}
		if ref, ok := refs[qt.Contact.ID]; ok {
			qt.Contact = &ref
		} else {
			qt.Contact = nil
		}
	}
	if lvl >= access.Edit && !access.ViaAgent(ctx) { // the public link is a person's to hand out
		if qt.linkToken != nil {
			u := s.quoteURL(*qt.linkToken)
			qt.URL = &u
		}
		qt.CanPublish = !access.ViaAgent(ctx) && allowedHere(ctx, s.d.Pool, qt.OrgID, userID, PermPublishQuotes) &&
			allowedHere(ctx, s.d.Pool, qt.OrgID, userID, access.PermSharePublic)
	} else {
		qt.SignerIP = nil
	}
	return qt, nil
}

func (s *Service) quoteURL(token string) string {
	return strings.TrimSuffix(s.d.Cfg.PublicWebURL, "/") + "/q/" + token
}

// QuoteSummary is a row of a deal's quotes.
type QuoteSummary struct {
	ID          uuid.UUID  `json:"id"`
	Number      string     `json:"number"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	Expired     bool       `json:"expired"`
	Currency    string     `json:"currency"`
	Total       int64      `json:"total"`
	ExpiresAt   time.Time  `json:"expires_at"`
	PublishedAt *time.Time `json:"published_at"`
	DecidedAt   *time.Time `json:"decided_at"`
	ViewCount   int        `json:"view_count"`
	SignerName  *string    `json:"signer_name"`
	CreatedAt   time.Time  `json:"created_at"`
}

// listQuotes: GET /v1/crm/deals/{id}/quotes (view on the deal; each quote the caller can open).
func (s *Service) listQuotes(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	dealID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	list, err := s.dealQuotes(ctx, me(r), dealID)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) dealQuotes(ctx context.Context, userID, dealID uuid.UUID) ([]QuoteSummary, error) {
	if _, _, err := s.requireRecord(ctx, s.d.Pool, userID, objDeal, dealID, access.View); err != nil {
		return nil, err
	}
	b := newSQL(ctx, userID)
	rows, err := s.d.Pool.Query(ctx, `SELECT q.id, q.number, q.title, q.status, q.currency, q.total, q.expires_at, q.published_at, q.decided_at,
		q.view_count, q.signer_name, q.created_at
		FROM crm_quotes q WHERE q.deal_id = `+b.arg(dealID)+` AND access_level($1, '`+KindQuote+`', q.id, $2) >= 1
		ORDER BY q.created_at DESC, q.id DESC LIMIT 100`, b.args...)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (QuoteSummary, error) {
		var qs QuoteSummary
		err := row.Scan(&qs.ID, &qs.Number, &qs.Title, &qs.Status, &qs.Currency, &qs.Total, &qs.ExpiresAt, &qs.PublishedAt, &qs.DecidedAt,
			&qs.ViewCount, &qs.SignerName, &qs.CreatedAt)
		qs.Expired = qs.Status == QuotePublished && time.Now().After(qs.ExpiresAt)
		return qs, err
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []QuoteSummary{}
	}
	// Drafts show their live total.
	for i := range out {
		if out[i].Status == QuoteDraft {
			if _, t, err := quoteItems(ctx, s.d.Pool, out[i].ID); err == nil {
				out[i].Total = t.Total
			}
		}
	}
	return out, nil
}

type quoteReq struct {
	Title     *string          `json:"title"`
	ExpiresAt *time.Time       `json:"expires_at"`
	ContactID *json.RawMessage `json:"contact_id"`
	Terms     *string          `json:"terms"`
	Comments  *string          `json:"comments"`
	Items     *[]lineItemReq   `json:"items"` // replaces every line (drafts)
}

// createQuote: POST /v1/crm/deals/{id}/quotes (edit on the deal): a draft
// with a copy of the deal's line items.
func (s *Service) createQuote(w http.ResponseWriter, r *http.Request) error {
	dealID, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req quoteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	qt, err := s.draftQuote(r.Context(), me(r), dealID, req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, qt)
	return nil
}

// draftQuote is the create REST and the agent tool share.
func (s *Service) draftQuote(ctx context.Context, userID, dealID uuid.UUID, req quoteReq) (*Quote, error) {
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		orgID, currency, err := s.lockDealForItems(ctx, tx, userID, dealID)
		if err != nil {
			return err
		}
		var dealName string
		if err := tx.QueryRow(ctx, `SELECT name FROM crm_deals WHERE id = $1`, dealID).Scan(&dealName); err != nil {
			return err
		}
		var seq int
		if err := tx.QueryRow(ctx, `UPDATE crm_settings SET quote_seq = quote_seq + 1 WHERE org_id = $1 RETURNING quote_seq`, orgID).Scan(&seq); err != nil {
			return err
		}
		title := dealName
		if req.Title != nil && strings.TrimSpace(*req.Title) != "" {
			title = strings.TrimSpace(*req.Title)
		}
		expires := time.Now().Add(defaultQuoteTTL)
		if req.ExpiresAt != nil {
			expires = *req.ExpiresAt
		}
		if err := validQuoteText(title, deref(req.Terms), deref(req.Comments), expires); err != nil {
			return err
		}
		var contactID *uuid.UUID
		if req.ContactID != nil {
			if contactID, err = s.quoteContact(ctx, tx, userID, orgID, *req.ContactID); err != nil {
				return err
			}
		} else {
			// The deal's first contact the caller can see.
			b := newSQL(ctx, userID)
			var cid uuid.UUID
			err := tx.QueryRow(ctx, `SELECT dc.contact_id FROM crm_deal_contacts dc WHERE dc.deal_id = `+b.arg(dealID)+`
				AND access_level($1, 'crm.contact', dc.contact_id, $2) >= 1 ORDER BY dc.created_at, dc.contact_id LIMIT 1`, b.args...).Scan(&cid)
			if err == nil {
				contactID = &cid
			} else if !db.IsNoRows(err) {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm_quotes (org_id, deal_id, number, title, currency, expires_at, terms, comments, contact_id,
			sender_id, agent, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $10) RETURNING id`,
			orgID, dealID, fmt.Sprintf("Q-%04d", seq), title, currency, expires, strings.TrimSpace(deref(req.Terms)), strings.TrimSpace(deref(req.Comments)),
			contactID, userID, agentName(ctx)).Scan(&id); err != nil {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindQuote, ID: id, OrgID: orgID, OwnerID: userID,
			Parent: &access.Ref{Kind: KindDeal, ID: dealID}}); err != nil {
			return err
		}
		if req.Items != nil {
			if err := s.replaceQuoteItems(ctx, tx, orgID, currency, id, *req.Items); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `INSERT INTO crm_quote_items (quote_id, product_id, position, name, sku, description, unit_price,
			quantity_milli, discount_bp, discount_amount, tax_bp, billing, billing_period, term_periods)
			SELECT $1, product_id, position, name, sku, description, unit_price, quantity_milli, discount_bp, discount_amount, tax_bp,
			       billing, billing_period, term_periods FROM crm_line_items WHERE deal_id = $2`, id, dealID); err != nil {
			return err
		}
		return addEvent(ctx, tx, orgID, objDeal, dealID, "quote", userID, map[string]any{"action": "drafted", "quote_id": id,
			"number": fmt.Sprintf("Q-%04d", seq), "title": title})
	})
	if err != nil {
		return nil, err
	}
	s.recordChanged(ctx, objDeal, dealID, true)
	return s.quoteDetail(ctx, userID, id)
}

func validQuoteText(title, terms, comments string, expires time.Time) error {
	if title == "" || len([]rune(title)) > 200 {
		return httpx.BadRequest("Give the quote a title of up to 200 characters.")
	}
	if len([]rune(terms)) > quoteTextMax || len([]rune(comments)) > quoteTextMax {
		return httpx.BadRequest("Terms and comments can be at most 10,000 characters each.")
	}
	if expires.After(time.Now().AddDate(1, 0, 0)) {
		return httpx.BadRequest("A quote can be valid for up to a year.")
	}
	return nil
}

// quoteContact validates the buyer contact (visible, same org; null clears).
func (s *Service) quoteContact(ctx context.Context, q db.Querier, userID, orgID uuid.UUID, raw json.RawMessage) (*uuid.UUID, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, httpx.BadRequest("contact_id must be a contact id.")
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, httpx.BadRequest("contact_id must be a contact id.")
	}
	if _, err := s.requireOther(ctx, q, userID, orgID, objContact, id); err != nil {
		return nil, asHTTP(err)
	}
	return &id, nil
}

// replaceQuoteItems rewrites a draft's lines from requests (product_id
// copies a product as on deals).
func (s *Service) replaceQuoteItems(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, currency string, quoteID uuid.UUID, items []lineItemReq) error {
	if len(items) > maxQuoteItems {
		return httpx.BadRequest("A quote can have at most 200 lines.")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM crm_quote_items WHERE quote_id = $1`, quoteID); err != nil {
		return err
	}
	for i, req := range items {
		li := &LineItem{}
		if err := s.applyLineItem(ctx, tx, orgID, currency, req, li, true); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm_quote_items (quote_id, product_id, position, name, sku, description, unit_price, quantity_milli,
			discount_bp, discount_amount, tax_bp, billing, billing_period, term_periods) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			quoteID, li.ProductID, i, li.Name, li.SKU, li.Description, li.UnitPrice, li.qtyMilli, li.DiscountBP, li.DiscountAmount, li.TaxBP,
			li.Billing, li.BillingPeriod, li.TermPeriods); err != nil {
			return err
		}
	}
	if _, t, err := quoteItems(ctx, tx, quoteID); err != nil {
		return err
	} else if t.Subtotal > maxMoney {
		return httpx.BadRequest("That quote comes to more than this CRM can hold.")
	}
	return nil
}

func (s *Service) getQuote(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "quoteID")
	if err != nil {
		return err
	}
	qt, err := s.quoteDetail(r.Context(), me(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, qt)
	return nil
}

var errQuoteNotDraft = httpx.NewError(http.StatusConflict, "quote_not_draft", "Only drafts can change. Unpublish the quote first.")

// updateQuote: PATCH /v1/crm/quotes/{quoteID} (edit; drafts only).
func (s *Service) updateQuote(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "quoteID")
	if err != nil {
		return err
	}
	var req quoteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var dealID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM crm_quotes WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		qt, _, err := s.requireQuote(ctx, tx, me(r), id, access.Edit)
		if err != nil {
			return err
		}
		if qt.Status != QuoteDraft {
			return errQuoteNotDraft
		}
		dealID = qt.Deal.ID
		title, terms, comments, expires := qt.Title, qt.Terms, qt.Comments, qt.ExpiresAt
		if req.Title != nil {
			title = strings.TrimSpace(*req.Title)
		}
		if req.Terms != nil {
			terms = strings.TrimSpace(*req.Terms)
		}
		if req.Comments != nil {
			comments = strings.TrimSpace(*req.Comments)
		}
		if req.ExpiresAt != nil {
			expires = *req.ExpiresAt
		}
		if err := validQuoteText(title, terms, comments, expires); err != nil {
			return err
		}
		contactID := (*uuid.UUID)(nil)
		if qt.Contact != nil {
			contactID = &qt.Contact.ID
		}
		if req.ContactID != nil {
			if contactID, err = s.quoteContact(ctx, tx, me(r), qt.OrgID, *req.ContactID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE crm_quotes SET title = $2, terms = $3, comments = $4, expires_at = $5, contact_id = $6, updated_at = now()
			WHERE id = $1`, id, title, terms, comments, expires, contactID); err != nil {
			return err
		}
		if req.Items != nil {
			return s.replaceQuoteItems(ctx, tx, qt.OrgID, qt.Currency, id, *req.Items)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, objDeal, dealID, false)
	qt, err := s.quoteDetail(ctx, me(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, qt)
	return nil
}

// refreshQuoteItems: POST /v1/crm/quotes/{quoteID}/refresh (edit; drafts):
// copy the deal's current line items again.
func (s *Service) refreshQuoteItems(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "quoteID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM crm_quotes WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		qt, _, err := s.requireQuote(ctx, tx, me(r), id, access.Edit)
		if err != nil {
			return err
		}
		if qt.Status != QuoteDraft {
			return errQuoteNotDraft
		}
		var currency string
		if err := tx.QueryRow(ctx, `SELECT currency FROM crm_deals WHERE id = $1`, qt.Deal.ID).Scan(&currency); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm_quote_items WHERE quote_id = $1`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE crm_quotes SET currency = $2, updated_at = now() WHERE id = $1`, id, currency)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm_quote_items (quote_id, product_id, position, name, sku, description, unit_price,
			quantity_milli, discount_bp, discount_amount, tax_bp, billing, billing_period, term_periods)
			SELECT $1, product_id, position, name, sku, description, unit_price, quantity_milli, discount_bp, discount_amount, tax_bp,
			       billing, billing_period, term_periods FROM crm_line_items WHERE deal_id = $2`, id, qt.Deal.ID)
		return err
	})
	if err != nil {
		return err
	}
	qt, err := s.quoteDetail(ctx, me(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, qt)
	return nil
}

// publishQuote: POST /v1/crm/quotes/{quoteID}/publish (edit, crm.publish_quotes
// and access.share_public): freeze it and make its public link.
func (s *Service) publishQuote(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "quoteID")
	if err != nil {
		return err
	}
	if access.ViaAgent(ctx) { // never reached by agent credentials; a guard for any future caller
		return httpx.Forbidden("AI agents can't publish quotes.")
	}
	var qt *Quote
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM crm_quotes WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		var err error
		if qt, _, err = s.requireQuote(ctx, tx, me(r), id, access.Edit); err != nil {
			return err
		}
		if qt.Status != QuoteDraft {
			return httpx.Conflict("This quote is already published.")
		}
		if !allowedHere(ctx, tx, qt.OrgID, me(r), PermPublishQuotes) {
			return permissionRequired(PermPublishQuotes)
		}
		items, totals, err := quoteItems(ctx, tx, id)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return httpx.BadRequest("Add at least one line before publishing.")
		}
		if !qt.ExpiresAt.After(time.Now().Add(time.Hour)) {
			return httpx.BadRequest("Choose an expiry date in the future first.")
		}
		exp := qt.ExpiresAt.Add(quoteLinkGrace)
		if max := time.Now().AddDate(1, 0, 0); exp.After(max) {
			exp = max
		}
		link, err := access.CreateLink(ctx, tx, access.Ref{Kind: KindQuote, ID: id}, access.View, &exp, me(r))
		if err != nil {
			return err
		}
		var senderName, senderEmail, seller, buyer, company string
		if err := tx.QueryRow(ctx, `SELECT display_name, COALESCE(email, '') FROM users WHERE id = $1`, me(r)).Scan(&senderName, &senderEmail); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1`, qt.OrgID).Scan(&seller); err != nil {
			return err
		}
		if qt.Contact != nil {
			_ = tx.QueryRow(ctx, `SELECT `+contactTitle("c")+` FROM crm_contacts c WHERE c.id = $1`, qt.Contact.ID).Scan(&buyer)
		}
		_ = tx.QueryRow(ctx, `SELECT co.name FROM crm_deals d JOIN crm_companies co ON co.id = d.company_id WHERE d.id = $1`, qt.Deal.ID).Scan(&company)
		if _, err := tx.Exec(ctx, `UPDATE crm_quotes SET status = 'published', link_id = $2, link_token = $3, published_at = now(), published_by = $4,
			sender_id = $4, sender_name = $5, sender_email = $6, seller_name = $7, buyer_name = $8, buyer_company = $9,
			subtotal = $10, discount_total = $11, tax_total = $12, total = $13, updated_at = now() WHERE id = $1`,
			id, link.ID, *link.Token, me(r), senderName, senderEmail, seller, buyer, company,
			totals.Subtotal, totals.Discount, totals.Tax, totals.Total); err != nil {
			return err
		}
		return addEvent(ctx, tx, qt.OrgID, objDeal, qt.Deal.ID, "quote", me(r), map[string]any{"action": "published", "quote_id": id,
			"number": qt.Number, "title": qt.Title, "total": formatMoney(totals.Total, qt.Currency)})
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, objDeal, qt.Deal.ID, true)
	out, err := s.quoteDetail(ctx, me(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// unpublishQuote: POST /v1/crm/quotes/{quoteID}/unpublish (edit): back to a
// draft; its link stops opening it (a later publish makes a new link).
func (s *Service) unpublishQuote(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "quoteID")
	if err != nil {
		return err
	}
	var qt *Quote
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM crm_quotes WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		var err error
		if qt, _, err = s.requireQuote(ctx, tx, me(r), id, access.Edit); err != nil {
			return err
		}
		if qt.Status != QuotePublished {
			return httpx.Conflict("Only a published quote nobody has accepted or declined can go back to a draft.")
		}
		if _, err := tx.Exec(ctx, `UPDATE crm_quotes SET status = 'draft', link_id = NULL, link_token = NULL, published_at = NULL,
			first_viewed_at = NULL, last_viewed_at = NULL, view_count = 0, updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return addEvent(ctx, tx, qt.OrgID, objDeal, qt.Deal.ID, "quote", me(r), map[string]any{"action": "unpublished", "quote_id": id,
			"number": qt.Number, "title": qt.Title})
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, objDeal, qt.Deal.ID, true)
	out, err := s.quoteDetail(ctx, me(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// deleteQuote: DELETE /v1/crm/quotes/{quoteID} (edit; an accepted quote also
// needs crm.delete).
func (s *Service) deleteQuote(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "quoteID")
	if err != nil {
		return err
	}
	var qt *Quote
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if qt, _, err = s.requireQuote(ctx, tx, me(r), id, access.Edit); err != nil {
			return err
		}
		if qt.Status == QuoteAccepted && !allowedHere(ctx, tx, qt.OrgID, me(r), PermDelete) {
			return permissionRequired(PermDelete)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm_quotes WHERE id = $1`, id); err != nil {
			return err
		}
		if err := access.DeleteResource(ctx, tx, access.Ref{Kind: KindQuote, ID: id}); err != nil {
			return err
		}
		return addEvent(ctx, tx, qt.OrgID, objDeal, qt.Deal.ID, "quote", me(r), map[string]any{"action": "deleted", "number": qt.Number, "title": qt.Title})
	})
	if err != nil {
		return err
	}
	s.recordChanged(ctx, objDeal, qt.Deal.ID, true)
	httpx.NoContent(w)
	return nil
}

// ---- the public page ------------------------------------------------------------

// PublicQuote is what anyone with the link sees: no internal ids, no
// people's accounts beyond the sender's name and email on the quote.
type PublicQuote struct {
	Number        string       `json:"number"`
	Title         string       `json:"title"`
	Status        string       `json:"status"` // published | accepted | declined
	Currency      string       `json:"currency"`
	ExpiresAt     time.Time    `json:"expires_at"`
	PublishedAt   time.Time    `json:"published_at"`
	SellerName    string       `json:"seller_name"`
	SenderName    string       `json:"sender_name"`
	SenderEmail   string       `json:"sender_email"`
	BuyerName     string       `json:"buyer_name"`
	BuyerCompany  string       `json:"buyer_company"`
	Terms         string       `json:"terms"`
	Comments      string       `json:"comments"`
	Items         []*QuoteItem `json:"items"`
	Totals        Totals       `json:"totals"`
	SignerName    *string      `json:"signer_name"`
	DecidedAt     *time.Time   `json:"decided_at"`
	DeclineReason *string      `json:"decline_reason"`
}

var errQuoteLink = httpx.NewError(http.StatusNotFound, "link_not_found", "This link doesn't work anymore. Ask the sender for a new one.")

// publicQuoteTarget resolves a link to the quote it opens: only the link its
// publishing made, while the quote is published or decided.
func (s *Service) publicQuoteTarget(ctx context.Context, q db.Querier, token string, lock bool) (*Quote, error) {
	l, err := access.ResolveLink(ctx, q, token)
	if err != nil {
		return nil, errQuoteLink
	}
	if l.Ref.Kind != KindQuote {
		return nil, errQuoteLink
	}
	if lock {
		if _, err := q.Exec(ctx, `SELECT 1 FROM crm_quotes WHERE id = $1 FOR UPDATE`, l.Ref.ID); err != nil {
			return nil, err
		}
	}
	qt, err := scanQuote(q.QueryRow(ctx, `SELECT `+quoteCols+` `+quoteFrom+` WHERE q.id = $1`, l.Ref.ID))
	if db.IsNoRows(err) {
		return nil, errQuoteLink
	}
	if err != nil {
		return nil, err
	}
	if qt.Status == QuoteDraft || qt.linkID == nil || *qt.linkID != l.ID {
		return nil, errQuoteLink
	}
	return qt, nil
}

func quoteExpiredError(qt *Quote) *httpx.Error {
	e := httpx.NewError(http.StatusGone, "quote_expired", fmt.Sprintf("This quote expired on %s. Ask %s for a new one.",
		qt.ExpiresAt.UTC().Format("2 January 2006"), qt.SenderName))
	e.Details = map[string]any{"number": qt.Number, "title": qt.Title, "seller_name": qt.SellerName, "sender_name": qt.SenderName,
		"sender_email": qt.SenderEmail, "expires_at": qt.ExpiresAt}
	return e
}

func (s *Service) toPublic(ctx context.Context, qt *Quote) (*PublicQuote, error) {
	items, totals, err := quoteItems(ctx, s.d.Pool, qt.ID)
	if err != nil {
		return nil, err
	}
	pq := &PublicQuote{Number: qt.Number, Title: qt.Title, Status: qt.Status, Currency: qt.Currency, ExpiresAt: qt.ExpiresAt,
		SellerName: qt.SellerName, SenderName: qt.SenderName, SenderEmail: qt.SenderEmail, BuyerName: qt.BuyerName, BuyerCompany: qt.BuyerCompany,
		Terms: qt.Terms, Comments: qt.Comments, Items: items, Totals: totals, SignerName: qt.SignerName, DecidedAt: qt.DecidedAt,
		DeclineReason: qt.DeclineReason}
	if qt.PublishedAt != nil {
		pq.PublishedAt = *qt.PublishedAt
	}
	for _, it := range pq.Items {
		it.ProductID = nil
	}
	return pq, nil
}

// publicQuote: GET /v1/crm/public/quotes/{token}. A published quote past its
// expiry answers 410 quote_expired (accepted and declined ones stay readable).
func (s *Service) publicQuote(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	qt, err := s.publicQuoteTarget(ctx, s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	if qt.Expired {
		return quoteExpiredError(qt)
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE crm_quotes SET view_count = view_count + 1, first_viewed_at = COALESCE(first_viewed_at, now()),
		last_viewed_at = now() WHERE id = $1`, qt.ID); err != nil {
		return err
	}
	pq, err := s.toPublic(ctx, qt)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	httpx.OK(w, pq)
	return nil
}

type quoteDecisionReq struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Accept bool   `json:"accept"` // accept: the buyer ticked "I accept"
	Reason string `json:"reason"` // decline
}

// decideQuote: POST /v1/crm/public/quotes/{token}/accept|decline — once.
func (s *Service) decideQuote(accept bool) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		var req quoteDecisionReq
		if err := httpx.Decode(r, &req); err != nil {
			return err
		}
		name := strings.TrimSpace(req.Name)
		if len([]rune(name)) < 2 || len([]rune(name)) > 200 {
			return httpx.BadRequest("Type your full name.")
		}
		email := strings.TrimSpace(req.Email)
		if email != "" {
			a, err := mail.ParseAddress(email)
			if err != nil || len(email) > 254 {
				return httpx.BadRequest("Enter a valid email address, or leave it empty.")
			}
			email = strings.ToLower(a.Address)
		}
		if accept && !req.Accept {
			return httpx.BadRequest("Tick the box to accept the quote.")
		}
		reason := strings.TrimSpace(req.Reason)
		if len([]rune(reason)) > 2000 {
			return httpx.BadRequest("A reason can be at most 2,000 characters.")
		}
		status, action := QuoteDeclined, "declined"
		if accept {
			status, action = QuoteAccepted, "accepted"
		}
		var qt *Quote
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			var err error
			if qt, err = s.publicQuoteTarget(ctx, tx, chi.URLParam(r, "token"), true); err != nil {
				return err
			}
			if qt.Status != QuotePublished {
				return httpx.NewError(http.StatusConflict, "quote_decided", "This quote was already "+qt.Status+".")
			}
			if qt.Expired {
				return quoteExpiredError(qt)
			}
			var reasonArg *string
			if !accept && reason != "" {
				reasonArg = &reason
			}
			var emailArg *string
			if email != "" {
				emailArg = &email
			}
			if _, err := tx.Exec(ctx, `UPDATE crm_quotes SET status = $2, signer_name = $3, signer_email = $4, signer_ip = $5, signer_agent = $6,
				decided_at = now(), decline_reason = $7, updated_at = now() WHERE id = $1 AND status = 'published'`,
				qt.ID, status, name, emailArg, httpx.ClientIPFrom(r), clip(r.UserAgent(), 300), reasonArg); err != nil {
				return err
			}
			return addEvent(ctx, tx, qt.OrgID, objDeal, qt.Deal.ID, "quote", uuid.Nil, map[string]any{"action": action, "quote_id": qt.ID,
				"number": qt.Number, "title": qt.Title, "signer_name": name})
		})
		if err != nil {
			return err
		}
		s.recordChanged(ctx, objDeal, qt.Deal.ID, true)
		if qt.Sender != nil {
			verb := "accepted"
			if !accept {
				verb = "declined"
			}
			s.send(ctx, notify.Notification{UserID: qt.Sender.ID, Kind: NotifyQuoteDecided,
				Title: name + " " + verb + " quote " + qt.Number + " (“" + clip(qt.Title, 60) + "”)",
				Link:  "/crm/quotes/" + qt.ID.String(), Data: map[string]any{"type": objDeal, "id": qt.Deal.ID, "quote_id": qt.ID}})
		}
		qt.Status, qt.SignerName = status, &name
		now := time.Now()
		qt.DecidedAt = &now
		if !accept && reason != "" {
			qt.DeclineReason = &reason
		}
		pq, err := s.toPublic(ctx, qt)
		if err != nil {
			return err
		}
		httpx.OK(w, pq)
		return nil
	}
}
