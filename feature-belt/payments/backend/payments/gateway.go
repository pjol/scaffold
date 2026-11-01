package payments

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Gateway is the payment-provider boundary. Stripe implements it (stripe.go);
// another provider replaces Stripe by implementing it and returning its
// implementation from newGateway. Nothing outside this file and the provider
// implementation knows provider vocabulary beyond the status strings below,
// which other providers map onto.
//
// Amounts are int64 minor units with a lowercase ISO currency. Every call
// that creates something at the provider takes an idempotency key, so a
// retried request never creates or moves money twice.
type Gateway interface {
	// Name is stored in every row's provider column and is the webhook
	// source in webhook_events ("stripe").
	Name() string

	CreateCustomer(ctx context.Context, in CustomerInput, idemKey string) (customerID string, err error)
	// DeleteCustomer removes the customer at the provider. A customer that is
	// already gone is not an error.
	DeleteCustomer(ctx context.Context, customerID string) error

	// GetPrice reads a provider price so the catalog stores its real amount,
	// currency and interval instead of trusting an admin's typing.
	GetPrice(ctx context.Context, priceID string) (*Price, error)

	// CreateCheckout opens a hosted checkout page.
	CreateCheckout(ctx context.Context, in CheckoutRequest, idemKey string) (*Checkout, error)
	GetCheckout(ctx context.Context, sessionID string) (*Checkout, error)
	// ExpireCheckout closes an open hosted page. A page that is already
	// complete or expired is not an error.
	ExpireCheckout(ctx context.Context, sessionID string) error

	// CreatePortal returns a hosted billing-portal URL (cards, invoices,
	// plan changes, cancellation).
	CreatePortal(ctx context.Context, customerID, returnURL string) (url string, err error)

	GetSubscription(ctx context.Context, subscriptionID string) (*Subscription, error)
	// CancelSubscription cancels immediately (no proration credit). An
	// already-canceled subscription is returned as is.
	CancelSubscription(ctx context.Context, subscriptionID, idemKey string) (*Subscription, error)
	GetInvoice(ctx context.Context, invoiceID string) (*Invoice, error)
	// ListPaidInvoices returns a subscription's paid invoices created since
	// the given time (the reconcile job backfills a missed invoice.paid).
	ListPaidInvoices(ctx context.Context, subscriptionID string, since time.Time) ([]Invoice, error)
	// GetPayment returns a payment with every refund made against it.
	GetPayment(ctx context.Context, paymentID string) (*Payment, error)
	Refund(ctx context.Context, in RefundInput, idemKey string) (*Refund, error)

	// Connect (PAYMENTS_CONNECT_ENABLED): connected accounts receive
	// destination charges minus the platform fee.
	CreateAccount(ctx context.Context, in AccountInput, idemKey string) (*Account, error)
	GetAccount(ctx context.Context, accountID string) (*Account, error)
	OnboardingLink(ctx context.Context, accountID, refreshURL, returnURL string) (url string, err error)

	// ParseWebhook verifies the request signature and names the object the
	// event is about. The handler always re-fetches that object, so the
	// event carries no state. Events the module does not act on come back
	// with an empty Type. Errors wrap ErrWebhookUnconfigured or
	// ErrBadSignature.
	ParseWebhook(payload []byte, h http.Header) (*Event, error)
}

var (
	// ErrNotFound: the provider has no such object (deleted, other account).
	ErrNotFound = errors.New("payments: not found at the provider")
	// ErrBadSignature: a webhook failed signature verification.
	ErrBadSignature = errors.New("payments: invalid webhook signature")
	// ErrWebhookUnconfigured: no webhook secret is set, so nothing verifies.
	ErrWebhookUnconfigured = errors.New("payments: webhook secret not configured")
)

// ProviderError is a request the provider refused for a reason the user or
// staff can act on. Message is safe to show.
type ProviderError struct {
	Code    string
	Message string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("payments: provider refused (%s): %s", e.Code, e.Message)
}

// Mode is the checkout mode.
type Mode string

const (
	ModePayment      Mode = "payment"      // one-time purchase → order
	ModeSubscription Mode = "subscription" // recurring price → subscription
)

// Checkout statuses (provider-neutral).
const (
	CheckoutOpen     = "open"
	CheckoutComplete = "complete"
	CheckoutExpired  = "expired"

	PaymentPaid   = "paid"
	PaymentUnpaid = "unpaid"
)

// Subscription statuses. Other providers map onto this set.
const (
	SubIncomplete        = "incomplete"
	SubIncompleteExpired = "incomplete_expired"
	SubTrialing          = "trialing"
	SubActive            = "active"
	SubPastDue           = "past_due"
	SubCanceled          = "canceled"
	SubUnpaid            = "unpaid"
	SubPaused            = "paused"
)

// Refund statuses.
const (
	RefundPending   = "pending"
	RefundSucceeded = "succeeded"
	RefundFailed    = "failed"
	RefundCanceled  = "canceled"
)

type CustomerInput struct {
	UserID string
	Email  string
	Name   string
}

// Price is a provider price.
type Price struct {
	ID            string
	Active        bool
	Kind          string // one_time | recurring
	UnitAmount    *int64 // nil for tiered, metered or pay-what-you-want prices
	Currency      string
	Interval      string // day | week | month | year ("" for one_time)
	IntervalCount int
	Metered       bool
	ProductName   string
}

// CheckoutRequest opens a hosted checkout for one price.
type CheckoutRequest struct {
	Mode       Mode
	CustomerID string
	PriceID    string
	Quantity   int64
	SuccessURL string
	CancelURL  string
	Reference  string            // our checkout session id
	Metadata   map[string]string // copied onto the payment or subscription
	// Connect destination charge: funds go to Destination minus the fee.
	Destination string
	FeeAmount   int64 // payment mode, minor units
	FeeBPS      int   // subscription mode, basis points of each invoice
	// ShippingCountries (ISO alpha-2) makes the page collect a shipping
	// address; empty = none.
	ShippingCountries []string
}

// Checkout is a hosted checkout session.
type Checkout struct {
	ID             string
	URL            string
	Mode           Mode
	Status         string // open | complete | expired
	PaymentStatus  string // paid | unpaid | no_payment_required
	CustomerID     string
	Reference      string
	AmountTotal    int64
	Currency       string
	PaymentID      string // payment mode
	SubscriptionID string // subscription mode
	ExpiresAt      time.Time
	Metadata       map[string]string
	Shipping       *ShippingAddress // when the page collected one
}

// ShippingAddress is where a physical order goes. Stored on the order as
// JSON, so the tags are part of the API.
type ShippingAddress struct {
	Name       string `json:"name"`
	Line1      string `json:"line1"`
	Line2      string `json:"line2"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

// Subscription is a provider subscription with one price.
type Subscription struct {
	ID                 string
	CustomerID         string
	Status             string
	PriceID            string
	Quantity           int64
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	CancelAtPeriodEnd  bool
	CancelAt           time.Time // scheduled end, zero when none
	CanceledAt         time.Time
	EndedAt            time.Time
	Metadata           map[string]string
}

// Invoice is a subscription invoice.
type Invoice struct {
	ID             string
	CustomerID     string
	SubscriptionID string
	Status         string // draft | open | paid | uncollectible | void
	AmountPaid     int64
	AmountDue      int64
	Currency       string
	PaymentID      string // the payment that settled it, when paid
	BillingReason  string
}

// Payment is a completed or attempted charge with its refunds.
type Payment struct {
	ID             string
	Status         string
	Amount         int64
	AmountReceived int64
	Currency       string
	CustomerID     string
	Refunds        []Refund
}

type RefundInput struct {
	PaymentID string
	Amount    int64
	Reason    string // duplicate | fraudulent | requested_by_customer | ""
	// ReverseTransfer pulls the connected account's share back on a
	// destination charge (and refunds the platform fee).
	ReverseTransfer bool
	Metadata        map[string]string
}

type Refund struct {
	ID        string
	PaymentID string
	Status    string
	Amount    int64
	Currency  string
	Reason    string
}

type AccountInput struct {
	UserID string
	Email  string
}

// Account is a connected (payout) account.
type Account struct {
	ID               string
	ChargesEnabled   bool
	PayoutsEnabled   bool
	DetailsSubmitted bool
}

// Neutral event types. ObjectID names what to re-fetch.
const (
	EventCheckout      = "checkout"               // completed, async payment settled, expired → GetCheckout
	EventInvoicePaid   = "invoice.paid"           // → GetInvoice
	EventInvoiceFailed = "invoice.payment_failed" // → GetInvoice
	EventSubscription  = "subscription"           // created, updated, deleted → GetSubscription
	EventRefund        = "refund"                 // ObjectID is the payment → GetPayment
	EventAccount       = "account"                // → GetAccount
)

// Event is a verified provider event.
type Event struct {
	ID       string
	Type     string // one of the Event* constants, or "" when ignored
	RawType  string
	ObjectID string
}
