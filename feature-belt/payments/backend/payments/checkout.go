package payments

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- return paths ----------------------------------------------------------

// validReturnPath accepts an app path (no scheme or host) under one of the
// allowed prefixes, so the provider never sends a payer to a page that does
// not exist or to another site. It returns the path with its query.
func validReturnPath(field, raw string, prefixes []string) (string, error) {
	raw = strings.TrimSpace(raw)
	bad := httpx.BadRequest(field + " must be an app path under " + strings.Join(prefixes, " or ") + ".")
	if raw == "" || len(raw) > 512 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		strings.ContainsAny(raw, "\\\r\n\t#") {
		return "", bad
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "", bad
	}
	for seg := range strings.SplitSeq(u.Path, "/") {
		if seg == ".." || seg == "." {
			return "", bad
		}
	}
	for _, pr := range prefixes {
		base := strings.TrimSuffix(pr, "/")
		if u.Path == base || strings.HasPrefix(u.Path, base+"/") {
			return u.Path + queryPart(u.RawQuery), nil
		}
	}
	return "", bad
}

func queryPart(q string) string {
	if q == "" {
		return ""
	}
	return "?" + q
}

// withParam appends k=v to an app path that may already carry a query.
func withParam(path, k, v string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + url.QueryEscape(k) + "=" + url.QueryEscape(v)
}

func (s *Service) webURL(path string) (string, error) {
	if s.cfg.webURL == "" {
		return "", httpx.NewError(http.StatusServiceUnavailable, "payments_unconfigured", "Payments aren't set up yet.")
	}
	return s.cfg.webURL + path, nil
}

// ---- customers ---------------------------------------------------------------

// ensureCustomer returns the user's provider customer, creating it on first
// use. Concurrent first calls share one idempotency key and converge on one
// row.
func (s *Service) ensureCustomer(ctx context.Context, userID uuid.UUID) (string, error) {
	var id string
	err := s.d.Pool.QueryRow(ctx, `SELECT provider_customer_id FROM payment_customers
		WHERE user_id = $1 AND provider = $2`, userID, s.gw.Name()).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !db.IsNoRows(err) {
		return "", err
	}
	var email, name string
	if err := s.d.Pool.QueryRow(ctx, `SELECT email, display_name FROM users WHERE id = $1 AND status <> 'deleted'`,
		userID).Scan(&email, &name); err != nil {
		return "", err
	}
	created, err := s.gw.CreateCustomer(ctx, CustomerInput{UserID: userID.String(), Email: email, Name: name},
		"customer:"+userID.String())
	if err != nil {
		return "", providerFailure(ctx, "create_customer", err)
	}
	err = s.d.Pool.QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO payment_customers (user_id, provider, provider_customer_id) VALUES ($1, $2, $3)
			ON CONFLICT (user_id, provider) DO NOTHING RETURNING provider_customer_id)
		SELECT provider_customer_id FROM ins
		UNION ALL
		SELECT provider_customer_id FROM payment_customers WHERE user_id = $1 AND provider = $2
		LIMIT 1`, userID, s.gw.Name(), created).Scan(&id)
	return id, err
}

// ---- checkout ----------------------------------------------------------------

// CheckoutInput starts a hosted checkout from Go (stitching code). The HTTP
// endpoint fills it from the client; stitching may add Ref and a seller.
type CheckoutInput struct {
	UserID      uuid.UUID
	PriceKey    string
	Quantity    int
	SuccessPath string
	CancelPath  string
	// Ref is an app reference ("event:<uuid>") copied to the order; order
	// hooks and HasPaidOrder read it. Clients can't set it over HTTP.
	Ref string
	// DestinationUserID (Connect) overrides the price's destination.
	DestinationUserID *uuid.UUID
}

// CheckoutResult is the hosted page to open.
type CheckoutResult struct {
	CheckoutID uuid.UUID  `json:"checkout_id"`
	URL        string     `json:"url"`
	ExpiresAt  *time.Time `json:"expires_at"`
}

// heldStatuses are subscriptions the user still has (a new checkout for the
// same plan would double-bill); fix them in the portal instead.
var heldStatuses = []string{SubTrialing, SubActive, SubPastDue, SubUnpaid, SubPaused}

// Checkout validates the request, records a pending session and opens the
// provider's hosted page. Nothing is granted here; the webhook does that.
func (s *Service) Checkout(ctx context.Context, in CheckoutInput) (*CheckoutResult, error) {
	if s.gw == nil {
		return nil, errUnconfigured
	}
	price, err := s.loadPrice(ctx, s.d.Pool, in.PriceKey)
	if err != nil {
		return nil, err
	}
	if in.Quantity == 0 {
		in.Quantity = 1
	}
	if in.Quantity < 1 || in.Quantity > 100 {
		return nil, httpx.BadRequest("Quantity must be 1 to 100.")
	}
	if len(in.Ref) > 200 {
		return nil, httpx.BadRequest("Ref must be 200 characters or fewer.")
	}
	successPath, err := validReturnPath("success_path", in.SuccessPath, s.cfg.returnPaths)
	if err != nil {
		return nil, err
	}
	cancelPath, err := validReturnPath("cancel_path", in.CancelPath, s.cfg.returnPaths)
	if err != nil {
		return nil, err
	}
	mode := ModePayment
	if price.Kind == "recurring" {
		mode = ModeSubscription
		var dup bool
		if err := s.d.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM payment_subscriptions sub
				LEFT JOIN payment_prices p ON p.key = sub.price_key
				WHERE sub.user_id = $1 AND sub.status = ANY($2)
				  AND (sub.price_key = $3 OR ($4::text IS NOT NULL AND p.entitlement = $4)))`,
			in.UserID, heldStatuses, price.Key, price.Entitlement).Scan(&dup); err != nil {
			return nil, err
		}
		if dup {
			return nil, httpx.NewError(http.StatusConflict, "already_subscribed", "You already have this plan. Manage it from billing.")
		}
	}

	dest := price.DestinationUserID
	if in.DestinationUserID != nil {
		dest = in.DestinationUserID
	}
	var destAccount string
	if dest != nil {
		if !s.cfg.connect {
			return nil, httpx.NewError(http.StatusConflict, "seller_not_ready", "This seller can't take payments yet.")
		}
		if *dest == in.UserID {
			return nil, httpx.BadRequest("You can't pay yourself.")
		}
		var charges bool
		err := s.d.Pool.QueryRow(ctx, `SELECT a.provider_account_id, a.charges_enabled AND u.status = 'active'
			FROM payment_connected_accounts a JOIN users u ON u.id = a.user_id
			WHERE a.user_id = $1 AND a.provider = $2`, *dest, s.gw.Name()).Scan(&destAccount, &charges)
		if db.IsNoRows(err) || (err == nil && !charges) {
			return nil, httpx.NewError(http.StatusConflict, "seller_not_ready", "This seller can't take payments yet.")
		}
		if err != nil {
			return nil, err
		}
	}

	var expected *int64
	if price.AmountMinor != nil {
		total := *price.AmountMinor * int64(in.Quantity)
		expected = &total
	}
	var fee int64
	if dest != nil && mode == ModePayment && expected != nil {
		fee = feeAmount(*expected, s.cfg.feeBPS)
	}

	customer, err := s.ensureCustomer(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	successURL, err := s.webURL(withParam(successPath, "checkout_id", id.String()))
	if err != nil {
		return nil, err
	}
	cancelURL, err := s.webURL(withParam(cancelPath, "checkout_id", id.String()))
	if err != nil {
		return nil, err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO payment_checkout_sessions (id, user_id, provider, price_key, mode, quantity, amount_minor, currency,
		  destination_user_id, platform_fee_minor, ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		id, in.UserID, s.gw.Name(), price.Key, string(mode), in.Quantity, expected, price.Currency, dest, fee, in.Ref); err != nil {
		return nil, err
	}
	meta := map[string]string{"user_id": in.UserID.String(), "price_key": price.Key, "checkout_id": id.String()}
	if in.Ref != "" {
		meta["ref"] = in.Ref
	}
	cs, err := s.gw.CreateCheckout(ctx, CheckoutRequest{
		Mode: mode, CustomerID: customer, PriceID: price.ProviderPriceID, Quantity: int64(in.Quantity),
		SuccessURL: successURL, CancelURL: cancelURL, Reference: id.String(), Metadata: meta,
		Destination: destAccount, FeeAmount: fee, FeeBPS: s.feeBPSFor(dest), ShippingCountries: s.cfg.shippingCountries,
	}, "checkout:"+id.String())
	if err != nil {
		if _, uerr := s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE payment_checkout_sessions
			SET status = 'failed', updated_at = now() WHERE id = $1`, id); uerr != nil {
			slog.WarnContext(ctx, "payments: mark checkout failed", "checkout_id", id, "err", uerr)
		}
		return nil, providerFailure(ctx, "create_checkout", err)
	}
	var exp *time.Time
	if !cs.ExpiresAt.IsZero() {
		exp = &cs.ExpiresAt
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE payment_checkout_sessions
		SET provider_session_id = $2, url = $3, expires_at = $4, updated_at = now() WHERE id = $1`,
		id, cs.ID, cs.URL, exp); err != nil {
		return nil, err
	}
	return &CheckoutResult{CheckoutID: id, URL: cs.URL, ExpiresAt: exp}, nil
}

func (s *Service) feeBPSFor(dest *uuid.UUID) int {
	if dest == nil {
		return 0
	}
	return s.cfg.feeBPS
}

// feeAmount is the platform fee on amount at bps basis points, rounded down
// (the seller keeps the fraction).
func feeAmount(amount int64, bps int) int64 {
	if amount <= 0 || bps <= 0 {
		return 0
	}
	return amount * int64(bps) / 10000
}

type checkoutReq struct {
	PriceKey    string `json:"price_key"`
	Quantity    int    `json:"quantity"`
	SuccessPath string `json:"success_path"`
	CancelPath  string `json:"cancel_path"`
}

// POST /v1/payments/checkout
func (s *Service) checkout(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var req checkoutReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.PriceKey == "" {
		return httpx.BadRequest("price_key is required.")
	}
	res, err := s.Checkout(r.Context(), CheckoutInput{
		UserID: id.UserID, PriceKey: req.PriceKey, Quantity: req.Quantity,
		SuccessPath: req.SuccessPath, CancelPath: req.CancelPath,
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, res)
	return nil
}

// CheckoutStatus is a session as its owner sees it (the success page polls
// it until the webhook has landed).
type CheckoutStatus struct {
	ID          uuid.UUID  `json:"id"`
	PriceKey    string     `json:"price_key"`
	Mode        string     `json:"mode"`
	Quantity    int        `json:"quantity"`
	AmountMinor *int64     `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Status      string     `json:"status"` // pending | completed | expired | failed
	OrderID     *uuid.UUID `json:"order_id"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

// GET /v1/payments/checkout/{id}
func (s *Service) getCheckout(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var c CheckoutStatus
	err = s.d.Pool.QueryRow(r.Context(), `
		SELECT cs.id, cs.price_key, cs.mode, cs.quantity, cs.amount_minor, cs.currency, cs.status, o.id,
		       cs.created_at, cs.completed_at
		FROM payment_checkout_sessions cs LEFT JOIN payment_orders o ON o.checkout_session_id = cs.id
		WHERE cs.id = $1 AND cs.user_id = $2`, id, caller.UserID).
		Scan(&c.ID, &c.PriceKey, &c.Mode, &c.Quantity, &c.AmountMinor, &c.Currency, &c.Status, &c.OrderID,
			&c.CreatedAt, &c.CompletedAt)
	if db.IsNoRows(err) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	httpx.OK(w, c)
	return nil
}

// ---- billing portal --------------------------------------------------------

type portalReq struct {
	ReturnPath string `json:"return_path"`
}

// POST /v1/payments/portal: a hosted page for cards, invoices, plan changes
// and cancellation. Changes come back through webhooks.
func (s *Service) portal(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	var req portalReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.ReturnPath == "" {
		req.ReturnPath = "/settings"
	}
	path, err := validReturnPath("return_path", req.ReturnPath, s.cfg.returnPaths)
	if err != nil {
		return err
	}
	var customer string
	err = s.d.Pool.QueryRow(r.Context(), `SELECT provider_customer_id FROM payment_customers
		WHERE user_id = $1 AND provider = $2`, caller.UserID, s.gw.Name()).Scan(&customer)
	if db.IsNoRows(err) {
		return httpx.NewError(http.StatusNotFound, "no_billing_account", "You don't have any billing history yet.")
	}
	if err != nil {
		return err
	}
	returnURL, err := s.webURL(path)
	if err != nil {
		return err
	}
	link, err := s.gw.CreatePortal(r.Context(), customer, returnURL)
	if errors.Is(err, ErrNotFound) {
		return httpx.NewError(http.StatusNotFound, "no_billing_account", "You don't have any billing history yet.")
	}
	if err != nil {
		return providerFailure(r.Context(), "create_portal", err)
	}
	httpx.OK(w, map[string]string{"url": link})
	return nil
}
