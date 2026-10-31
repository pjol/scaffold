import type { CallOptions, HttpClient } from "../api/http";
import type { PageParams } from "../api/params";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminLedgerEntry,
  AdminPaymentCustomer,
  AdminPaymentCustomerDetail,
  AdminPrice,
  AdminRefund,
  CatalogPrice,
  CheckoutRequest,
  CheckoutResult,
  CheckoutStatus,
  ConnectedAccountResponse,
  PaymentEntitlement,
  PaymentItems,
  PaymentLedgerEntry,
  OnboardingRequest,
  PortalRequest,
  PriceCreateRequest,
  PriceUpdateRequest,
  RefundRequest,
  SubscriptionResponse,
  PaymentUrl,
} from "./types";

export interface AdminPaymentCustomerParams extends PageParams {
  q?: string;
}

export interface AdminPaymentFilterParams extends PageParams {
  user_id?: string;
}

/**
 * Payments endpoints. Without a provider every one answers 503
 * `payments_unconfigured`. Hooks call `useEndpoints(paymentsEndpoints)`.
 */
export function paymentsEndpoints(http: HttpClient) {
  const P = "/v1/payments";
  const A = "/v1/admin/payments";
  return {
    /** `GET /v1/payments/prices[?entitlement=]` (public): the active catalog. */
    prices: (entitlement?: string, o?: CallOptions) => http.get<PaymentItems<CatalogPrice>>(`${P}/prices`, { ...o, query: { entitlement: entitlement || undefined } }),
    /** `POST /v1/payments/checkout` → hosted page URL. Nothing is granted until the provider confirms. */
    checkout: (body: CheckoutRequest, o?: CallOptions) => http.post<CheckoutResult>(`${P}/checkout`, body, o),
    /** `GET /v1/payments/checkout/{id}` (own sessions). */
    checkoutStatus: (id: string, o?: CallOptions) => http.get<CheckoutStatus>(`${P}/checkout/${seg(id)}`, o),
    /** `POST /v1/payments/portal` → billing portal URL (cards, invoices, plan, cancel). */
    portal: (body: PortalRequest = {}, o?: CallOptions) => http.post<PaymentUrl>(`${P}/portal`, body, o),
    /** `GET /v1/payments/subscription`: the one that matters now, or null. */
    subscription: (o?: CallOptions) => http.get<SubscriptionResponse>(`${P}/subscription`, o),
    /** `GET /v1/payments/entitlements`: what the user holds now. */
    entitlements: (o?: CallOptions) => http.get<PaymentItems<PaymentEntitlement>>(`${P}/entitlements`, o),
    /** `GET /v1/payments/history`: ledger, newest first. */
    history: (params: PageParams = {}, o?: CallOptions) =>
      http.get<Page<PaymentLedgerEntry>>(`${P}/history`, { ...o, query: { limit: params.limit, cursor: params.cursor } }),
    /** `GET /v1/payments/connect/account` (404 `connect_disabled` when Connect is off). */
    connectAccount: (o?: CallOptions) => http.get<ConnectedAccountResponse>(`${P}/connect/account`, o),
    /** `POST /v1/payments/connect/onboarding` → hosted onboarding URL. */
    connectOnboarding: (body: OnboardingRequest = {}, o?: CallOptions) => http.post<PaymentUrl>(`${P}/connect/onboarding`, body, o),
    admin: {
      /** `GET /v1/admin/payments/customers?q=` (email, name, user id or provider id). */
      customers: (params: AdminPaymentCustomerParams = {}, o?: CallOptions) =>
        http.get<Page<AdminPaymentCustomer>>(`${A}/customers`, { ...o, query: { q: params.q, limit: params.limit, cursor: params.cursor } }),
      /** `GET /v1/admin/payments/customers/{user_id}`. */
      customer: (userId: string, o?: CallOptions) => http.get<AdminPaymentCustomerDetail>(`${A}/customers/${seg(userId)}`, o),
      /** `GET /v1/admin/payments/ledger?user_id=`. */
      ledger: (params: AdminPaymentFilterParams = {}, o?: CallOptions) =>
        http.get<Page<AdminLedgerEntry>>(`${A}/ledger`, { ...o, query: { user_id: params.user_id, limit: params.limit, cursor: params.cursor } }),
      /** `GET /v1/admin/payments/refunds?user_id=`. */
      refunds: (params: AdminPaymentFilterParams = {}, o?: CallOptions) =>
        http.get<Page<AdminRefund>>(`${A}/refunds`, { ...o, query: { user_id: params.user_id, limit: params.limit, cursor: params.cursor } }),
      /** `POST /v1/admin/payments/refunds` (admin role). The key makes a retry return the first result. Audited. */
      refund: (body: RefundRequest, idempotencyKey: string, o?: CallOptions) => http.post<AdminRefund>(`${A}/refunds`, body, { ...o, idempotencyKey }),
      /** `GET /v1/admin/payments/prices`: every catalog entry. */
      prices: (o?: CallOptions) => http.get<PaymentItems<AdminPrice>>(`${A}/prices`, o),
      /** `POST /v1/admin/payments/prices` (admin role): reads the provider price. Audited. */
      createPrice: (body: PriceCreateRequest, o?: CallOptions) => http.post<AdminPrice>(`${A}/prices`, body, o),
      /** `PATCH /v1/admin/payments/prices/{key}` (admin role). Audited. */
      updatePrice: (key: string, body: PriceUpdateRequest, o?: CallOptions) => http.patch<AdminPrice>(`${A}/prices/${seg(key)}`, body, o),
      /** `DELETE /v1/admin/payments/prices/{key}` (admin role): 409 when it has purchases. Audited. */
      deletePrice: (key: string, o?: CallOptions) => http.delete<void>(`${A}/prices/${seg(key)}`, o),
    },
  };
}

export type PaymentsEndpoints = ReturnType<typeof paymentsEndpoints>;
