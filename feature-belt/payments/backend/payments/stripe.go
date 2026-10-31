package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v87"
	"github.com/stripe/stripe-go/v87/webhook"
)

// stripeGateway implements Gateway with stripe-go's client API. It is the
// only file that imports stripe-go.
type stripeGateway struct {
	sc *stripe.Client
	// secrets verify webhooks: the platform endpoint's and, with Connect, the
	// connected-accounts endpoint's (Stripe signs each endpoint separately).
	secrets []string
}

// NewStripe returns the Stripe Gateway. Empty webhook secrets are dropped;
// with none, ParseWebhook refuses everything.
func NewStripe(secretKey string, webhookSecrets ...string) Gateway {
	g := &stripeGateway{sc: stripe.NewClient(secretKey)}
	for _, s := range webhookSecrets {
		if strings.TrimSpace(s) != "" {
			g.secrets = append(g.secrets, s)
		}
	}
	return g
}

func (g *stripeGateway) Name() string { return "stripe" }

func idem(p *stripe.Params, key string) {
	if key != "" {
		p.IdempotencyKey = stripe.String(key)
	}
}

// mapErr turns Stripe errors into the neutral ones.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var se *stripe.Error
	if errors.As(err, &se) {
		if se.HTTPStatusCode == http.StatusNotFound || se.Code == stripe.ErrorCodeResourceMissing {
			return fmt.Errorf("%w: %s", ErrNotFound, se.Msg)
		}
		if se.Type == stripe.ErrorTypeCard || se.Type == stripe.ErrorTypeInvalidRequest {
			msg := se.Msg
			if msg == "" {
				msg = "The payment provider refused the request."
			}
			return &ProviderError{Code: string(se.Code), Message: msg}
		}
	}
	return err
}

func unix(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func (g *stripeGateway) CreateCustomer(ctx context.Context, in CustomerInput, idemKey string) (string, error) {
	p := &stripe.CustomerCreateParams{Metadata: map[string]string{"user_id": in.UserID}}
	if in.Email != "" {
		p.Email = stripe.String(in.Email)
	}
	if in.Name != "" {
		p.Name = stripe.String(in.Name)
	}
	idem(&p.Params, idemKey)
	c, err := g.sc.V1Customers.Create(ctx, p)
	if err != nil {
		return "", mapErr(err)
	}
	return c.ID, nil
}

func (g *stripeGateway) DeleteCustomer(ctx context.Context, customerID string) error {
	_, err := g.sc.V1Customers.Delete(ctx, customerID, nil)
	if err = mapErr(err); errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (g *stripeGateway) GetPrice(ctx context.Context, priceID string) (*Price, error) {
	p := &stripe.PriceRetrieveParams{}
	p.AddExpand("product")
	pr, err := g.sc.V1Prices.Retrieve(ctx, priceID, p)
	if err != nil {
		return nil, mapErr(err)
	}
	out := &Price{
		ID:       pr.ID,
		Active:   pr.Active,
		Kind:     string(pr.Type),
		Currency: strings.ToLower(string(pr.Currency)),
	}
	if pr.BillingScheme == stripe.PriceBillingSchemePerUnit && pr.CustomUnitAmount == nil {
		amt := pr.UnitAmount
		out.UnitAmount = &amt
	}
	if r := pr.Recurring; r != nil {
		out.Interval = string(r.Interval)
		out.IntervalCount = int(r.IntervalCount)
		out.Metered = r.UsageType == stripe.PriceRecurringUsageTypeMetered
	}
	if pr.Product != nil {
		out.ProductName = pr.Product.Name
	}
	return out, nil
}

func (g *stripeGateway) CreateCheckout(ctx context.Context, in CheckoutRequest, idemKey string) (*Checkout, error) {
	p := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(string(in.Mode)),
		Customer:          stripe.String(in.CustomerID),
		ClientReferenceID: stripe.String(in.Reference),
		SuccessURL:        stripe.String(in.SuccessURL),
		CancelURL:         stripe.String(in.CancelURL),
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Price:    stripe.String(in.PriceID),
			Quantity: stripe.Int64(in.Quantity),
		}},
		Metadata: in.Metadata,
	}
	if len(in.ShippingCountries) > 0 {
		p.ShippingAddressCollection = &stripe.CheckoutSessionCreateShippingAddressCollectionParams{
			AllowedCountries: stripe.StringSlice(in.ShippingCountries),
		}
	}
	switch in.Mode {
	case ModePayment:
		pi := &stripe.CheckoutSessionCreatePaymentIntentDataParams{Metadata: in.Metadata}
		if in.Destination != "" {
			pi.TransferData = &stripe.CheckoutSessionCreatePaymentIntentDataTransferDataParams{
				Destination: stripe.String(in.Destination),
			}
			if in.FeeAmount > 0 {
				pi.ApplicationFeeAmount = stripe.Int64(in.FeeAmount)
			}
		}
		p.PaymentIntentData = pi
	case ModeSubscription:
		sd := &stripe.CheckoutSessionCreateSubscriptionDataParams{Metadata: in.Metadata}
		if in.Destination != "" {
			sd.TransferData = &stripe.CheckoutSessionCreateSubscriptionDataTransferDataParams{
				Destination: stripe.String(in.Destination),
			}
			if in.FeeBPS > 0 {
				// Stripe takes a percentage here (its type, not ours): 250 bps → 2.5.
				sd.ApplicationFeePercent = stripe.Float64(float64(in.FeeBPS) / 100)
			}
		}
		p.SubscriptionData = sd
	}
	idem(&p.Params, idemKey)
	cs, err := g.sc.V1CheckoutSessions.Create(ctx, p)
	if err != nil {
		return nil, mapErr(err)
	}
	return checkoutFromStripe(cs), nil
}

func (g *stripeGateway) GetCheckout(ctx context.Context, sessionID string) (*Checkout, error) {
	cs, err := g.sc.V1CheckoutSessions.Retrieve(ctx, sessionID, nil)
	if err != nil {
		return nil, mapErr(err)
	}
	return checkoutFromStripe(cs), nil
}

func (g *stripeGateway) ExpireCheckout(ctx context.Context, sessionID string) error {
	if _, err := g.sc.V1CheckoutSessions.Expire(ctx, sessionID, nil); err != nil {
		cur, gerr := g.GetCheckout(ctx, sessionID)
		if gerr == nil && cur.Status != CheckoutOpen {
			return nil
		}
		if errors.Is(gerr, ErrNotFound) {
			return nil
		}
		return mapErr(err)
	}
	return nil
}

func (g *stripeGateway) CreatePortal(ctx context.Context, customerID, returnURL string) (string, error) {
	s, err := g.sc.V1BillingPortalSessions.Create(ctx, &stripe.BillingPortalSessionCreateParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	})
	if err != nil {
		return "", mapErr(err)
	}
	return s.URL, nil
}

func (g *stripeGateway) GetSubscription(ctx context.Context, subscriptionID string) (*Subscription, error) {
	sub, err := g.sc.V1Subscriptions.Retrieve(ctx, subscriptionID, nil)
	if err != nil {
		return nil, mapErr(err)
	}
	return subscriptionFromStripe(sub), nil
}

func (g *stripeGateway) CancelSubscription(ctx context.Context, subscriptionID, idemKey string) (*Subscription, error) {
	p := &stripe.SubscriptionCancelParams{}
	idem(&p.Params, idemKey)
	sub, err := g.sc.V1Subscriptions.Cancel(ctx, subscriptionID, p)
	if err != nil {
		if cur, gerr := g.GetSubscription(ctx, subscriptionID); gerr == nil &&
			(cur.Status == SubCanceled || cur.Status == SubIncompleteExpired) {
			return cur, nil
		}
		return nil, mapErr(err)
	}
	return subscriptionFromStripe(sub), nil
}

func (g *stripeGateway) GetInvoice(ctx context.Context, invoiceID string) (*Invoice, error) {
	p := &stripe.InvoiceRetrieveParams{}
	p.AddExpand("payments")
	inv, err := g.sc.V1Invoices.Retrieve(ctx, invoiceID, p)
	if err != nil {
		return nil, mapErr(err)
	}
	return invoiceFromStripe(inv), nil
}

func (g *stripeGateway) ListPaidInvoices(ctx context.Context, subscriptionID string, since time.Time) ([]Invoice, error) {
	p := &stripe.InvoiceListParams{
		Subscription: stripe.String(subscriptionID),
		Status:       stripe.String("paid"),
		CreatedRange: &stripe.RangeQueryParams{GreaterThanOrEqual: since.Unix()},
	}
	p.AddExpand("data.payments")
	out := []Invoice{}
	for inv, err := range g.sc.V1Invoices.List(ctx, p).All(ctx) {
		if err != nil {
			return nil, mapErr(err)
		}
		out = append(out, *invoiceFromStripe(inv))
	}
	return out, nil
}

func (g *stripeGateway) GetPayment(ctx context.Context, paymentID string) (*Payment, error) {
	pi, err := g.sc.V1PaymentIntents.Retrieve(ctx, paymentID, nil)
	if err != nil {
		return nil, mapErr(err)
	}
	out := &Payment{
		ID:             pi.ID,
		Status:         string(pi.Status),
		Amount:         pi.Amount,
		AmountReceived: pi.AmountReceived,
		Currency:       strings.ToLower(string(pi.Currency)),
		Refunds:        []Refund{},
	}
	if pi.Customer != nil {
		out.CustomerID = pi.Customer.ID
	}
	list := g.sc.V1Refunds.List(ctx, &stripe.RefundListParams{PaymentIntent: stripe.String(paymentID)})
	for r, err := range list.All(ctx) {
		if err != nil {
			return nil, mapErr(err)
		}
		out.Refunds = append(out.Refunds, refundFromStripe(r))
	}
	return out, nil
}

func (g *stripeGateway) Refund(ctx context.Context, in RefundInput, idemKey string) (*Refund, error) {
	p := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(in.PaymentID),
		Amount:        stripe.Int64(in.Amount),
		Metadata:      in.Metadata,
	}
	if in.Reason != "" {
		p.Reason = stripe.String(in.Reason)
	}
	if in.ReverseTransfer {
		p.ReverseTransfer = stripe.Bool(true)
		p.RefundApplicationFee = stripe.Bool(true)
	}
	idem(&p.Params, idemKey)
	r, err := g.sc.V1Refunds.Create(ctx, p)
	if err != nil {
		return nil, mapErr(err)
	}
	out := refundFromStripe(r)
	if out.PaymentID == "" {
		out.PaymentID = in.PaymentID
	}
	return &out, nil
}

// CreateAccount creates an Express connected account that can receive
// destination charges (card_payments + transfers).
func (g *stripeGateway) CreateAccount(ctx context.Context, in AccountInput, idemKey string) (*Account, error) {
	p := &stripe.AccountCreateParams{
		Type: stripe.String("express"),
		Capabilities: &stripe.AccountCreateCapabilitiesParams{
			CardPayments: &stripe.AccountCreateCapabilitiesCardPaymentsParams{Requested: stripe.Bool(true)},
			Transfers:    &stripe.AccountCreateCapabilitiesTransfersParams{Requested: stripe.Bool(true)},
		},
		Metadata: map[string]string{"user_id": in.UserID},
	}
	if in.Email != "" {
		p.Email = stripe.String(in.Email)
	}
	idem(&p.Params, idemKey)
	a, err := g.sc.V1Accounts.Create(ctx, p)
	if err != nil {
		return nil, mapErr(err)
	}
	return accountFromStripe(a), nil
}

func (g *stripeGateway) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	a, err := g.sc.V1Accounts.GetByID(ctx, accountID, nil)
	if err != nil {
		return nil, mapErr(err)
	}
	return accountFromStripe(a), nil
}

func (g *stripeGateway) OnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (string, error) {
	l, err := g.sc.V1AccountLinks.Create(ctx, &stripe.AccountLinkCreateParams{
		Account:    stripe.String(accountID),
		RefreshURL: stripe.String(refreshURL),
		ReturnURL:  stripe.String(returnURL),
		Type:       stripe.String("account_onboarding"),
	})
	if err != nil {
		return "", mapErr(err)
	}
	return l.URL, nil
}

// ParseWebhook verifies the Stripe-Signature header (HMAC-SHA256, 5-minute
// tolerance) against each configured secret. The API version of the payload
// is not enforced: only ids are read from it, and the handler re-fetches the
// object on the SDK's pinned version.
func (g *stripeGateway) ParseWebhook(payload []byte, h http.Header) (*Event, error) {
	if len(g.secrets) == 0 {
		return nil, ErrWebhookUnconfigured
	}
	header := h.Get("Stripe-Signature")
	var (
		ev      stripe.Event
		lastErr error
		ok      bool
	)
	for _, secret := range g.secrets {
		e, err := webhook.ConstructEventWithOptions(payload, header, secret,
			webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
		if err == nil {
			ev, ok = e, true
			break
		}
		lastErr = err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %v", ErrBadSignature, lastErr)
	}
	if ev.APIVersion != "" && ev.APIVersion != stripe.APIVersion {
		slog.Debug("stripe event on a different API version; ids only are read", "event", ev.ID, "sent", ev.APIVersion)
	}
	out := &Event{ID: ev.ID, RawType: string(ev.Type)}
	if ev.Data == nil {
		return out, nil
	}
	var obj struct {
		ID            string          `json:"id"`
		PaymentIntent json.RawMessage `json:"payment_intent"`
	}
	if err := json.Unmarshal(ev.Data.Raw, &obj); err != nil {
		return nil, fmt.Errorf("decode stripe event object: %w", err)
	}
	out.ObjectID = obj.ID
	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded",
		"checkout.session.async_payment_failed", "checkout.session.expired":
		out.Type = EventCheckout
	case "invoice.paid":
		out.Type = EventInvoicePaid
	case "invoice.payment_failed":
		out.Type = EventInvoiceFailed
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		out.Type = EventSubscription
	case "charge.refunded", "charge.refund.updated":
		// A charge or a refund object; both name their payment intent, which
		// is what gets re-fetched (with every refund against it).
		out.ObjectID = expandableID(obj.PaymentIntent)
		if out.ObjectID != "" {
			out.Type = EventRefund
		}
	case "account.updated":
		out.Type = EventAccount
	}
	return out, nil
}

// expandableID reads a Stripe expandable field: a bare id or an object.
func expandableID(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var obj struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &obj)
	return obj.ID
}

func checkoutFromStripe(cs *stripe.CheckoutSession) *Checkout {
	out := &Checkout{
		ID:            cs.ID,
		URL:           cs.URL,
		Mode:          Mode(cs.Mode),
		Status:        string(cs.Status),
		PaymentStatus: string(cs.PaymentStatus),
		Reference:     cs.ClientReferenceID,
		AmountTotal:   cs.AmountTotal,
		Currency:      strings.ToLower(string(cs.Currency)),
		ExpiresAt:     unix(cs.ExpiresAt),
		Metadata:      cs.Metadata,
	}
	if cs.Customer != nil {
		out.CustomerID = cs.Customer.ID
	}
	if cs.PaymentIntent != nil {
		out.PaymentID = cs.PaymentIntent.ID
	}
	if cs.Subscription != nil {
		out.SubscriptionID = cs.Subscription.ID
	}
	if ci := cs.CollectedInformation; ci != nil && ci.ShippingDetails != nil && ci.ShippingDetails.Address != nil {
		a := ci.ShippingDetails.Address
		out.Shipping = &ShippingAddress{Name: ci.ShippingDetails.Name, Line1: a.Line1, Line2: a.Line2, City: a.City,
			State: a.State, PostalCode: a.PostalCode, Country: a.Country}
	}
	return out
}

func invoiceFromStripe(inv *stripe.Invoice) *Invoice {
	out := &Invoice{
		ID:            inv.ID,
		Status:        string(inv.Status),
		AmountPaid:    inv.AmountPaid,
		AmountDue:     inv.AmountDue,
		Currency:      strings.ToLower(string(inv.Currency)),
		BillingReason: string(inv.BillingReason),
	}
	if inv.Customer != nil {
		out.CustomerID = inv.Customer.ID
	}
	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil && inv.Parent.SubscriptionDetails.Subscription != nil {
		out.SubscriptionID = inv.Parent.SubscriptionDetails.Subscription.ID
	}
	if inv.Payments != nil {
		for _, ip := range inv.Payments.Data {
			if ip == nil || ip.Payment == nil || ip.Payment.PaymentIntent == nil {
				continue
			}
			if out.PaymentID == "" || ip.Status == "paid" {
				out.PaymentID = ip.Payment.PaymentIntent.ID
			}
		}
	}
	return out
}

func subscriptionFromStripe(s *stripe.Subscription) *Subscription {
	out := &Subscription{
		ID:                s.ID,
		Status:            string(s.Status),
		CancelAtPeriodEnd: s.CancelAtPeriodEnd,
		CancelAt:          unix(s.CancelAt),
		CanceledAt:        unix(s.CanceledAt),
		EndedAt:           unix(s.EndedAt),
		Metadata:          s.Metadata,
	}
	if s.Customer != nil {
		out.CustomerID = s.Customer.ID
	}
	// Periods live on the items (API 2025-03-31 and later). One price per
	// subscription is the module's model; the first item is it.
	if s.Items != nil && len(s.Items.Data) > 0 {
		it := s.Items.Data[0]
		if it.Price != nil {
			out.PriceID = it.Price.ID
		}
		out.Quantity = it.Quantity
		out.CurrentPeriodStart = unix(it.CurrentPeriodStart)
		out.CurrentPeriodEnd = unix(it.CurrentPeriodEnd)
	}
	if out.Quantity <= 0 {
		out.Quantity = 1
	}
	return out
}

func refundFromStripe(r *stripe.Refund) Refund {
	out := Refund{
		ID:       r.ID,
		Status:   string(r.Status),
		Amount:   r.Amount,
		Currency: strings.ToLower(string(r.Currency)),
		Reason:   string(r.Reason),
	}
	if out.Status == "requires_action" {
		out.Status = RefundPending
	}
	if r.PaymentIntent != nil {
		out.PaymentID = r.PaymentIntent.ID
	}
	return out
}

func accountFromStripe(a *stripe.Account) *Account {
	return &Account{
		ID:               a.ID,
		ChargesEnabled:   a.ChargesEnabled,
		PayoutsEnabled:   a.PayoutsEnabled,
		DetailsSubmitted: a.DetailsSubmitted,
	}
}
