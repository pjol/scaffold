/**
 * Payments DTOs. snake_case, mirroring backend/internal/payments exactly. Money is
 * integer minor units (`amount_minor`) beside a lowercase ISO currency; never floats.
 */

/** A catalog price as clients see it (`GET /v1/payments/prices`). */
export interface CatalogPrice {
  /** Stable app key (`pro_monthly`): what code and checkout refer to. */
  key: string;
  label: string;
  kind: "one_time" | "recurring";
  /** Null for tiered or pay-what-you-want prices. */
  amount_minor: number | null;
  currency: string;
  interval: "day" | "week" | "month" | "year" | null;
  interval_count: number | null;
  /** What buying it grants, or null (e.g. a physical item). */
  entitlement: string | null;
  /** One-time prices: days of access; null = no expiry. */
  entitlement_days: number | null;
}

export type CheckoutMode = "payment" | "subscription";
export type CheckoutSessionStatus = "pending" | "completed" | "expired" | "failed";

export interface CheckoutRequest {
  price_key: string;
  quantity?: number;
  /** App paths under PAYMENTS_RETURN_PATHS (default `/payments/…`, `/settings…`). */
  success_path: string;
  cancel_path: string;
}

/** `POST /v1/payments/checkout`: open `url` (hosted page). */
export interface CheckoutResult {
  checkout_id: string;
  url: string;
  expires_at: string | null;
}

/** `GET /v1/payments/checkout/{id}`. `completed` means the provider confirmed payment. */
export interface CheckoutStatus {
  id: string;
  price_key: string;
  mode: CheckoutMode;
  quantity: number;
  amount_minor: number | null;
  currency: string;
  status: CheckoutSessionStatus;
  order_id: string | null;
  created_at: string;
  completed_at: string | null;
}

export type SubscriptionStatus = "incomplete" | "incomplete_expired" | "trialing" | "active" | "past_due" | "canceled" | "unpaid" | "paused";

/** A subscription as its owner sees it. */
export interface PaymentSubscription {
  id: string;
  price_key: string;
  label: string;
  entitlement: string | null;
  status: SubscriptionStatus;
  quantity: number;
  current_period_end: string | null;
  cancel_at_period_end: boolean;
  /** Scheduled end (portal cancellations), null when none. */
  cancel_at: string | null;
  ended_at: string | null;
  created_at: string;
}

/** `GET /v1/payments/subscription`. */
export interface SubscriptionResponse {
  subscription: PaymentSubscription | null;
}

export interface PaymentEntitlement {
  key: string;
  source: "order" | "subscription";
  /** Null: no expiry. */
  active_until: string | null;
}

export type PaymentLedgerKind = "payment" | "invoice" | "refund";

/** One money movement: positive amounts were paid, negative were refunded. */
export interface PaymentLedgerEntry {
  id: number;
  kind: PaymentLedgerKind;
  amount_minor: number;
  currency: string;
  description: string;
  order_id: string | null;
  subscription_id: string | null;
  created_at: string;
}

export interface PortalRequest {
  return_path?: string;
}

export interface PaymentUrl {
  url: string;
}

/** Connect payout account (`GET /v1/payments/connect/account`). */
export interface ConnectedAccount {
  charges_enabled: boolean;
  payouts_enabled: boolean;
  details_submitted: boolean;
  created_at: string;
  updated_at: string;
}

export interface ConnectedAccountResponse {
  account: ConnectedAccount | null;
}

export interface OnboardingRequest {
  return_path?: string;
}

/** `details` of a 402 `payment_required`: route the user to these prices. */
export interface PaymentRequiredDetails {
  entitlement: string;
  prices: CatalogPrice[];
}

/** Error codes the payments API returns (branch with `hasErrorCode`). */
export type PaymentsErrorCode =
  | "payments_unconfigured"
  | "payment_required"
  | "price_not_found"
  | "already_subscribed"
  | "seller_not_ready"
  | "no_billing_account"
  | "payment_provider_error"
  | "payment_provider_refused"
  | "connect_disabled"
  | "nothing_to_refund"
  | "other_provider"
  | "idempotency_key_required"
  | "refund_recorded"
  | "webhook_unconfigured";

// ---- admin -------------------------------------------------------------------

export interface ShippingAddress {
  name: string;
  line1: string;
  line2: string;
  city: string;
  state: string;
  postal_code: string;
  country: string;
}

export type PaymentOrderStatus = "paid" | "partially_refunded" | "refunded";

export interface PaymentOrder {
  id: string;
  user_id: string;
  checkout_id: string;
  price_key: string;
  quantity: number;
  amount_minor: number;
  currency: string;
  refunded_minor: number;
  platform_fee_minor: number;
  destination_user_id: string | null;
  ref: string;
  shipping: ShippingAddress | null;
  status: PaymentOrderStatus;
  created_at: string;
}

export interface AdminPrice extends CatalogPrice {
  id: string;
  provider: string;
  provider_price_id: string;
  destination_user_id: string | null;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface PriceCreateRequest {
  key: string;
  provider_price_id: string;
  label?: string;
  entitlement?: string;
  entitlement_days?: number;
  destination_user_id?: string;
  active?: boolean;
}

/** Omitted fields stay; `entitlement: ""`, `entitlement_days: 0`, `destination_user_id: ""` clear. */
export interface PriceUpdateRequest {
  provider_price_id?: string;
  label?: string;
  entitlement?: string;
  entitlement_days?: number;
  destination_user_id?: string;
  active?: boolean;
}

export interface AdminPaymentCustomer {
  id: string;
  user_id: string;
  email: string;
  display_name: string;
  user_status: string;
  provider: string;
  provider_customer_id: string;
  delete_after: string | null;
  created_at: string;
}

export type RefundStatus = "pending" | "succeeded" | "failed" | "canceled";
export type RefundReason = "" | "duplicate" | "fraudulent" | "requested_by_customer";

export interface AdminRefund {
  id: string;
  user_id: string;
  ledger_id: number;
  order_id: string | null;
  provider: string;
  provider_refund_id: string | null;
  amount_minor: number;
  currency: string;
  reason: RefundReason | string;
  note: string;
  status: RefundStatus;
  requested_by: string | null;
  requested_by_email: string | null;
  created_at: string;
}

export interface AdminPaymentCustomerDetail {
  user_id: string;
  customer: AdminPaymentCustomer | null;
  subscriptions: PaymentSubscription[];
  entitlements: PaymentEntitlement[];
  orders: PaymentOrder[];
  refunds: AdminRefund[];
  /** Payments minus refunds, per currency. */
  net_paid: Record<string, number>;
}

export interface AdminLedgerEntry {
  id: number;
  user_id: string;
  kind: PaymentLedgerKind;
  amount_minor: number;
  currency: string;
  description: string;
  provider: string;
  provider_ref: string;
  provider_payment_id: string | null;
  order_id: string | null;
  subscription_id: string | null;
  /** Payments and invoices: refunds succeeded or pending against it. */
  refunded_minor: number;
  created_at: string;
}

export interface RefundRequest {
  ledger_id: number;
  /** Default: everything not yet refunded. */
  amount_minor?: number;
  reason?: RefundReason;
  note?: string;
}

export interface PaymentItems<T> {
  items: T[];
}
