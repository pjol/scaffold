"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import { hasErrorCode, isApiError, type ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { IdempotencyKeyManager } from "../hooks/idempotency";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { shouldRetryQuery } from "../hooks/query-client";
import { paymentsEndpoints, type AdminPaymentCustomerParams, type AdminPaymentFilterParams } from "./api";
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
  PaymentLedgerEntry,
  PaymentRequiredDetails,
  PaymentSubscription,
  PriceCreateRequest,
  PriceUpdateRequest,
  RefundRequest,
  SubscriptionResponse,
  PaymentUrl,
} from "./types";

/** Query keys. Everything sits under `["payments"]`. */
export const paymentsKeys = {
  all: () => ["payments"] as const,
  prices: (entitlement?: string) => ["payments", "prices", entitlement ?? ""] as const,
  checkout: (id: string) => ["payments", "checkout", id] as const,
  subscription: () => ["payments", "subscription"] as const,
  entitlements: () => ["payments", "entitlements"] as const,
  history: () => ["payments", "history"] as const,
  connect: () => ["payments", "connect"] as const,
  admin: {
    all: () => ["payments", "admin"] as const,
    customers: (params: Record<string, unknown>) => ["payments", "admin", "customers", params] as const,
    customer: (userId: string) => ["payments", "admin", "customer", userId] as const,
    ledger: (params: Record<string, unknown>) => ["payments", "admin", "ledger", params] as const,
    refunds: (params: Record<string, unknown>) => ["payments", "admin", "refunds", params] as const,
    prices: () => ["payments", "admin", "prices"] as const,
  },
};

// ---- errors ------------------------------------------------------------------

/** 503 `payments_unconfigured`: the server has no provider. Show "Payments aren't set up yet." */
export const isPaymentsUnconfigured = (error: unknown): boolean => hasErrorCode(error, "payments_unconfigured");

/** 404 `connect_disabled`: payouts are off on this server; hide payout UI. */
export const isConnectDisabled = (error: unknown): boolean => hasErrorCode(error, "connect_disabled");

/**
 * The details of a 402 `payment_required` (from a route behind `RequireEntitlement`), or
 * null for any other error. Route the user to the plan picker for `entitlement`.
 */
export function paymentRequiredDetails(error: unknown): PaymentRequiredDetails | null {
  if (!isApiError(error) || error.code !== "payment_required") return null;
  const d = error.details ?? {};
  const entitlement = typeof d["entitlement"] === "string" ? d["entitlement"] : "";
  const prices = Array.isArray(d["prices"]) ? (d["prices"] as CatalogPrice[]) : [];
  return { entitlement, prices };
}

/** Never retry a server that has no provider; otherwise the house policy. */
function retryUnlessUnconfigured(failureCount: number, error: unknown): boolean {
  return !isPaymentsUnconfigured(error) && !isConnectDisabled(error) && shouldRetryQuery(failureCount, error);
}

// ---- formatting (portable) -------------------------------------------------------

/** "month", "3 months", "year"; null for one-time prices. */
export function priceInterval(price: Pick<CatalogPrice, "interval" | "interval_count">): string | null {
  if (!price.interval) return null;
  const n = price.interval_count ?? 1;
  return n === 1 ? price.interval : `${n} ${price.interval}s`;
}

/** Whether the subscription still gives access (paid up, in trial, or being retried). */
export function isSubscriptionLive(sub: Pick<PaymentSubscription, "status"> | null | undefined): boolean {
  return !!sub && (sub.status === "active" || sub.status === "trialing" || sub.status === "past_due");
}

// ---- catalog and checkout --------------------------------------------------------

/** `GET /v1/payments/prices[?entitlement=]`. */
export function usePrices(entitlement?: string, options?: QueryOverrides<CatalogPrice[]>): UseQueryResult<CatalogPrice[], ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  return useApiQuery(paymentsKeys.prices(entitlement), async ({ signal }) => (await ep.prices(entitlement, { signal })).items, {
    staleTime: 5 * 60_000,
    retry: retryUnlessUnconfigured,
    ...options,
  });
}

/**
 * `POST /v1/payments/checkout`. Open `result.url` (web: `window.location.assign`;
 * mobile: an in-app browser, only where the store rules allow it).
 */
export function useCheckout(): UseMutationResult<CheckoutResult, ApiError, CheckoutRequest> {
  const ep = useEndpoints(paymentsEndpoints);
  return useMutation<CheckoutResult, ApiError, CheckoutRequest>({ mutationFn: (body) => ep.checkout(body) });
}

const POLL_MS = 2_000;
const POLL_FOR_MS = 3 * 60_000;

/**
 * `GET /v1/payments/checkout/{id}`, polled every 2 s while `pending` (the webhook
 * usually lands within seconds) for up to 3 minutes. When it completes, the
 * subscription, entitlements and history are refetched. Pass null to disable.
 */
export function useCheckoutStatus(id: string | null | undefined, options?: QueryOverrides<CheckoutStatus>): UseQueryResult<CheckoutStatus, ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  const qc = useQueryClient();
  const started = useRef(Date.now());
  const query = useApiQuery(paymentsKeys.checkout(id ?? ""), ({ signal }) => ep.checkoutStatus(id ?? "", { signal }), {
    retry: retryUnlessUnconfigured,
    refetchInterval: (q) => (q.state.data?.status === "pending" && Date.now() - started.current < POLL_FOR_MS ? POLL_MS : false),
    ...options,
  }, !!id);
  const status = query.data?.status;
  useEffect(() => {
    if (status !== "completed") return;
    void qc.invalidateQueries({ queryKey: paymentsKeys.subscription() });
    void qc.invalidateQueries({ queryKey: paymentsKeys.entitlements() });
    void qc.invalidateQueries({ queryKey: paymentsKeys.history() });
  }, [status, qc]);
  return query;
}

/** True once a pending checkout has been polled for the full window without an answer. */
export function checkoutPollingGaveUp(status: CheckoutStatus | undefined, startedAt: number, now = Date.now()): boolean {
  return status?.status === "pending" && now - startedAt >= POLL_FOR_MS;
}

/** `POST /v1/payments/portal` → `{ url }`. 404 `no_billing_account` before the first purchase. */
export function usePortal(): UseMutationResult<PaymentUrl, ApiError, { return_path?: string } | void> {
  const ep = useEndpoints(paymentsEndpoints);
  return useMutation<PaymentUrl, ApiError, { return_path?: string } | void>({ mutationFn: (body) => ep.portal(body ?? {}) });
}

// ---- what the user holds ---------------------------------------------------------

/** `GET /v1/payments/subscription`: the live one if any, else the latest; null when none. */
export function useSubscription(options?: QueryOverrides<SubscriptionResponse>): UseQueryResult<SubscriptionResponse, ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  return useApiQuery(paymentsKeys.subscription(), ({ signal }) => ep.subscription({ signal }), { retry: retryUnlessUnconfigured, ...options });
}

/** `GET /v1/payments/entitlements`. */
export function useEntitlements(options?: QueryOverrides<PaymentEntitlement[]>): UseQueryResult<PaymentEntitlement[], ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  return useApiQuery(paymentsKeys.entitlements(), async ({ signal }) => (await ep.entitlements({ signal })).items, {
    retry: retryUnlessUnconfigured,
    ...options,
  });
}

/**
 * Whether the user holds `key` now: `undefined` while loading or when payments are
 * unavailable. UI hint only; the API enforces access with `RequireEntitlement`.
 */
export function useHasEntitlement(key: string): boolean | undefined {
  const q = useEntitlements();
  if (!q.data) return undefined;
  const now = Date.now();
  return q.data.some((e) => e.key === key && (e.active_until === null || new Date(e.active_until).getTime() > now));
}

/** `GET /v1/payments/history` as an infinite list (`data.items`), newest first. */
export function usePaymentHistory(options?: InfiniteOverrides<PaymentLedgerEntry>): PagedQueryResult<PaymentLedgerEntry> {
  const ep = useEndpoints(paymentsEndpoints);
  return usePagedQuery<PaymentLedgerEntry>(paymentsKeys.history(), (cursor, signal) => ep.history({ cursor, limit: 20 }, { signal }), {
    retry: retryUnlessUnconfigured,
    ...options,
  });
}

// ---- Connect -----------------------------------------------------------------------

/** `GET /v1/payments/connect/account`. Errors with `connect_disabled` when payouts are off. */
export function useConnectedAccount(options?: QueryOverrides<ConnectedAccountResponse>): UseQueryResult<ConnectedAccountResponse, ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  return useApiQuery(paymentsKeys.connect(), ({ signal }) => ep.connectAccount({ signal }), { retry: retryUnlessUnconfigured, ...options });
}

/** `POST /v1/payments/connect/onboarding` → `{ url }` (creates the account on first use). */
export function useConnectOnboarding(): UseMutationResult<PaymentUrl, ApiError, { return_path?: string } | void> {
  const ep = useEndpoints(paymentsEndpoints);
  const qc = useQueryClient();
  return useMutation<PaymentUrl, ApiError, { return_path?: string } | void>({
    mutationFn: (body) => ep.connectOnboarding(body ?? {}),
    onSettled: () => qc.invalidateQueries({ queryKey: paymentsKeys.connect() }),
  });
}

// ---- admin -----------------------------------------------------------------------

/** `GET /v1/admin/payments/customers?q=` as an infinite list. */
export function useAdminPaymentCustomers(params: AdminPaymentCustomerParams = {}, options?: InfiniteOverrides<AdminPaymentCustomer>): PagedQueryResult<AdminPaymentCustomer> {
  const ep = useEndpoints(paymentsEndpoints);
  return usePagedQuery<AdminPaymentCustomer>(paymentsKeys.admin.customers(listKeyParams(params)), (cursor, signal) => ep.admin.customers({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    retry: retryUnlessUnconfigured,
    ...options,
  });
}

/** `GET /v1/admin/payments/customers/{user_id}`. Pass null to disable. */
export function useAdminPaymentCustomer(userId: string | null | undefined, options?: QueryOverrides<AdminPaymentCustomerDetail>): UseQueryResult<AdminPaymentCustomerDetail, ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  return useApiQuery(paymentsKeys.admin.customer(userId ?? ""), ({ signal }) => ep.admin.customer(userId ?? "", { signal }), { retry: retryUnlessUnconfigured, ...options }, !!userId);
}

/** `GET /v1/admin/payments/ledger?user_id=` as an infinite list. */
export function useAdminLedger(params: AdminPaymentFilterParams = {}, options?: InfiniteOverrides<AdminLedgerEntry>): PagedQueryResult<AdminLedgerEntry> {
  const ep = useEndpoints(paymentsEndpoints);
  return usePagedQuery<AdminLedgerEntry>(paymentsKeys.admin.ledger(listKeyParams(params)), (cursor, signal) => ep.admin.ledger({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    retry: retryUnlessUnconfigured,
    ...options,
  });
}

/** `GET /v1/admin/payments/refunds?user_id=` as an infinite list. */
export function useAdminRefunds(params: AdminPaymentFilterParams = {}, options?: InfiniteOverrides<AdminRefund>): PagedQueryResult<AdminRefund> {
  const ep = useEndpoints(paymentsEndpoints);
  return usePagedQuery<AdminRefund>(paymentsKeys.admin.refunds(listKeyParams(params)), (cursor, signal) => ep.admin.refunds({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    retry: retryUnlessUnconfigured,
    ...options,
  });
}

/**
 * `POST /v1/admin/payments/refunds` (admin role). One Idempotency-Key per attempt: a
 * retry after a network error or 5xx reuses it, so the refund can't happen twice.
 */
export function useCreateRefund(): UseMutationResult<AdminRefund, ApiError, RefundRequest> {
  const ep = useEndpoints(paymentsEndpoints);
  const qc = useQueryClient();
  const keys = useRef(new IdempotencyKeyManager());
  return useMutation<AdminRefund, ApiError, RefundRequest>({
    mutationFn: async (body) => {
      const key = keys.current.keyFor(body);
      try {
        const res = await ep.admin.refund(body, key);
        keys.current.settle({ ok: true });
        return res;
      } catch (error) {
        keys.current.settle({ ok: false, error });
        throw error;
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: paymentsKeys.admin.all() }),
  });
}

/** `GET /v1/admin/payments/prices`. */
export function useAdminPrices(options?: QueryOverrides<AdminPrice[]>): UseQueryResult<AdminPrice[], ApiError> {
  const ep = useEndpoints(paymentsEndpoints);
  return useApiQuery(paymentsKeys.admin.prices(), async ({ signal }) => (await ep.admin.prices({ signal })).items, { retry: retryUnlessUnconfigured, ...options });
}

function useInvalidateCatalog() {
  const qc = useQueryClient();
  return () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: paymentsKeys.admin.prices() }),
      qc.invalidateQueries({ queryKey: ["payments", "prices"] }),
    ]);
}

/** `POST /v1/admin/payments/prices` (admin role). */
export function useCreatePrice(): UseMutationResult<AdminPrice, ApiError, PriceCreateRequest> {
  const ep = useEndpoints(paymentsEndpoints);
  const invalidate = useInvalidateCatalog();
  return useMutation<AdminPrice, ApiError, PriceCreateRequest>({ mutationFn: (body) => ep.admin.createPrice(body), onSettled: invalidate });
}

/** `PATCH /v1/admin/payments/prices/{key}` (admin role). */
export function useUpdatePrice(): UseMutationResult<AdminPrice, ApiError, { key: string } & PriceUpdateRequest> {
  const ep = useEndpoints(paymentsEndpoints);
  const invalidate = useInvalidateCatalog();
  return useMutation<AdminPrice, ApiError, { key: string } & PriceUpdateRequest>({ mutationFn: ({ key, ...body }) => ep.admin.updatePrice(key, body), onSettled: invalidate });
}

/** `DELETE /v1/admin/payments/prices/{key}` (admin role). */
export function useDeletePrice(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(paymentsEndpoints);
  const invalidate = useInvalidateCatalog();
  return useMutation<void, ApiError, string>({ mutationFn: (key) => ep.admin.deletePrice(key), onSettled: invalidate });
}
