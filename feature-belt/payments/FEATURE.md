# Payments

Stripe by default, behind a `Gateway` interface so another provider can replace it. The rest of the app sees only `payments` functions, entitlements and the shared hooks.

## What it does

- Users buy one-time prices and subscribe to recurring ones on hosted Stripe Checkout (no card data touches the app), then land on a success page that waits for the provider's confirmation.
- Purchases grant **entitlements** (`pro`, `supporter`…). Gated routes answer 402 `payment_required` with the prices that grant them; clients open the plan picker.
- Settings → Billing shows the plan, what the user holds and their payment history, and links to Stripe's billing portal (cards, invoices, plan changes, cancellation).
- Orders, subscriptions and refunds change only from provider state (signed webhook, re-fetched, or the hourly reconcile job), never because a client says it paid.
- A failed renewal notifies the user (`payments.failed`); a lapsed or cancelled subscription loses its entitlement at period end.
- Staff look up customers, read the ledger, refund (audited, idempotent) and manage the price catalog. Optional Connect: sellers onboard Express accounts and receive destination charges minus a platform fee.

## The store rule (decide before selling anything)

| What you sell | Use |
|---|---|
| Digital content or features used in the iOS/Android app (premium tier, credits, extra features) | **Apple IAP / Google Play Billing** (RevenueCat or StoreKit/Play Billing). **Not in this module.** Stripe in the app for these is rejected (Apple 3.1.1, Play Billing). |
| Physical goods, real-world services, bookings, tickets to in-person events, donations to registered charities | **This module (Stripe).** IAP is not allowed. |
| Digital goods, US storefront only | A link out to web checkout is allowed; fees are contested in court, re-check before relying on it. |

The web may sell anything. The mobile app opens hosted checkout and the portal only with `EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT=on` (physical goods/services catalogs); off (default) it shows status and history and offers no purchase. The rule is repeated in `mobile/src/features/payments/store-rules.ts`.

## Data

| Table | Purpose |
|---|---|
| `payment_customers` | user ↔ provider customer; `delete_after` schedules provider deletion after account deletion |
| `payment_prices` | catalog: stable `key` → provider price id; amount/currency/interval copied from the provider; `entitlement`, `entitlement_days`, Connect `destination_user_id` |
| `payment_checkout_sessions` | hosted checkouts: pending → completed / expired / failed; expected amount, `ref` |
| `payment_orders` | one-time purchases (provider state only); refunded totals, platform fee, `shipping` |
| `payment_subscriptions` | mirrored subscriptions: status, periods, `cancel_at_period_end`, `cancel_at`, `cancel_pending`, destination |
| `payment_entitlements` | `(user_id, key, source, source_id, active_until)` derived from orders and subscriptions |
| `payment_ledger` | append-only (UPDATE blocked by trigger): payments and invoices positive, refunds negative, unique `idempotency_key` |
| `payment_refunds` | staff and dashboard refunds with status, reason, note, requester |
| `payment_connected_accounts` | Connect payout accounts and capability flags |

Money is `bigint` minor units with a lowercase currency. Every provider id sits beside a `provider` column.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/payments/prices[?entitlement=]` | public | active catalog |
| POST | `/v1/payments/checkout` | user, 20/h, detached | `{price_key, quantity, success_path, cancel_path}` → `{checkout_id, url, expires_at}` |
| GET | `/v1/payments/checkout/{id}` | owner | session status (success pages poll it) |
| POST | `/v1/payments/portal` | user, 30/h | `{return_path}` → `{url}`; 404 `no_billing_account` before a first purchase |
| GET | `/v1/payments/subscription` · `/entitlements` · `/history` | user | plan (`{subscription}`), holdings, ledger page |
| GET · POST | `/v1/payments/connect/account` · `/connect/onboarding` | user | payout account / hosted onboarding URL; 404 `connect_disabled` when Connect is off |
| GET | `/v1/admin/payments/customers[?q=]` · `/customers/{user_id}` | support | customers; detail with subscriptions, holdings, orders, refunds, net paid |
| GET | `/v1/admin/payments/ledger[?user_id=]` · `/refunds[?user_id=]` | support | ledger with refunded amounts; refunds |
| POST | `/v1/admin/payments/refunds` | admin, `Idempotency-Key`, detached | `{ledger_id, amount_minor?, reason, note}`; audited |
| GET · POST · PATCH · DELETE | `/v1/admin/payments/prices[/{key}]` | support reads, admin writes | catalog; create reads the provider price; delete 409 when used; audited |
| POST | `/webhooks/stripe` | Stripe signature | verified, deduped (`webhooks.Once`), re-fetched, applied in one transaction |

No provider key: every route above answers 503 `payments_unconfigured` and clients show "Payments aren't set up yet". Return paths must start with a `PAYMENTS_RETURN_PATHS` prefix; `checkout_id` is appended. **402** details: `{entitlement, prices}`. Webhook events to enable: `checkout.session.completed`, `checkout.session.async_payment_succeeded`, `checkout.session.async_payment_failed`, `checkout.session.expired`, `invoice.paid`, `invoice.payment_failed`, `customer.subscription.created|updated|deleted`, `charge.refunded`, `charge.refund.updated`; Connect endpoint: `account.updated`. No realtime topics; notification kind `payments.failed`.

## Screens

**Web** (`web/src/features/payments/`): `/payments` plan picker (`?entitlement=`, `?next=`; marks current plan, same-entitlement plans offer "Change plan"), `/payments/success` (polls up to 3 minutes, then says it is still confirming), `/payments/cancel`, `/payments/payouts` (Connect), public `/payments/return` (where mobile's in-app checkout lands), Settings → Billing (`/settings/billing`: plan rows, Manage billing, past-due banner, access, payouts row when Connect is on, history), Admin → Payments (Customers · Ledger · Refunds · Prices; refund dialog; add/edit/delete price). Building blocks: `PlanPicker`, `CheckoutButton`, `usePaymentRequired()`, `PaymentsUnavailable`. **Mobile** (`mobile/src/features/payments/`): Settings → Billing (`/payments/billing`), `/payments/plans`, `/payments/checkout/[id]` (status after the in-app browser closes), `usePaymentRequired()`. Every view has skeleton, empty, error and unconfigured states.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `STRIPE_SECRET_KEY` | — | Stripe key; unset = 503 everywhere; live keys refused in development |
| `STRIPE_WEBHOOK_SECRET` | — | endpoint signing secret; required outside development with a key; unset = webhooks refused |
| `STRIPE_CONNECT_WEBHOOK_SECRET` | — | Connect endpoint secret (`account.updated`) |
| `PAYMENTS_PRICES` | — | dev catalog seed `key=price_id[:entitlement],…` |
| `PAYMENTS_CONNECT_ENABLED` / `PAYMENTS_PLATFORM_FEE_BPS` | `false` / `0` | Connect and its fee in basis points |
| `PAYMENTS_RETENTION_YEARS` | `7` | retention of a deleted account's financial rows |
| `PAYMENTS_RETURN_PATHS` | `/payments/,/settings` | allowed return path prefixes |
| `PAYMENTS_RENEWAL_GRACE` | `24h` | entitlement slack past a renewing period end |
| `PAYMENTS_SHIPPING_COUNTRIES` | — | ISO codes Checkout collects a shipping address for (physical goods) |
| `EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT` | `off` | mobile hosted checkout/portal (store rule above) |

Invalid values fail the boot through `config.Problem`. Go dependency `github.com/stripe/stripe-go/v87` v87.1.0 (API `2026-10-28.endive`), only in `stripe.go`; npm: `expo-web-browser` (already in the template). Dev-up hook `scripts/dev-up/hooks/payments.sh`: `hook_payments_prepare` reads the Stripe CLI's signing secret, hands it to the backend with `hook_env` and starts `stripe listen --forward-to $API_URL/webhooks/stripe` (needs a test key in `.env.dev` and the CLI).

## Compliance

- **Deletion** (in the transaction): live subscriptions flagged `cancel_pending`, open checkouts expired, entitlements deleted, prices paying the user deactivated. After commit (`AccountDeleted`) subscriptions are cancelled at Stripe and open pages closed; failures retry in the reconcile job. Orders, subscriptions, ledger and refunds stay pseudonymised for `PAYMENTS_RETENTION_YEARS`; then the retention job deletes the Stripe customer and purges the rows. A customer who never paid is deleted on the next daily run.
- **Export**: orders (with shipping), subscriptions, ledger entries, refunds.
- Name Stripe in the privacy policy; the deletion screen says payment records are kept and why; store review notes describe the payment flow and which store rule applies.

## Stitching

1. Classify what you sell with the store rule; keep `EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT` off unless the catalog is physical goods or services.
2. Create Stripe products and prices; add each to the catalog (Admin → Payments → Prices, or `PAYMENTS_PRICES`) with a stable key and its entitlement.
3. Gate features: `r.With(payments.RequireEntitlement("pro"))` or `payments.HasEntitlement`; in clients call `usePaymentRequired()(error)` in `onError`, and link pricing to `/payments` (web) or `/payments/plans` (mobile).
4. Per-item sales: your endpoint calls `payments.From(d).Checkout(ctx, payments.CheckoutInput{…, Ref: "event:<id>"})`; react in `payments.OnOrder` (inside the webhook transaction; idempotent); check with `payments.HasPaidOrder`. Physical goods: set `PAYMENTS_SHIPPING_COUNTRIES` and read `order.Shipping` in the hook.
5. Past-due policy: access continues while Stripe retries; set Stripe's retry schedule and end state to match.
6. Production: webhook endpoint + events above, `STRIPE_WEBHOOK_SECRET`, Billing Portal configuration, privacy policy.

## Extending

- **Another provider**: implement `Gateway` (neutral types in `gateway.go`), return it from `newGateway`, mount its webhook route. Rows from another provider stay readable; refunds on them answer 409 `other_provider`.
- **Hooks and readers**: `OnOrder`; `HasEntitlement`, `ActiveEntitlements`, `CurrentSubscription`, `ListLedger`, `NetPaid`, `HasPaidOrder`. Agent tools: `payments_get_subscription`, `payments_list_history` (read-only). Shared: `paymentsEndpoints`, `paymentsKeys`, hooks, `formatMinor`/`parseMinor` (currency-exponent aware).
- Don't write orders, entitlements or ledger rows outside `sync.go`; don't trust webhook payloads; never UPDATE the ledger.

## Provenance

Distilled from DOGPATCH (`Gateway` boundary, stripe-go v87 client, idempotency keys, `httpx.Detach` money routes, `webhook_events` dedupe, re-fetch before trusting `account.updated`, Express accounts, return-URL allow-list), STIQQ (subscriptions, billing portal, reconcile of missed webhooks and invoices, append-only ledger with unique keys), ASSEMBLY (Connect Express + hosted Checkout + platform fee) and SIMMONS (orders only from the webhook, amount check against the priced checkout, async payment events, 200 for unknown events, API version drift tolerated). Fixed on the way: webhooks fail closed without a secret (ASSEMBLY accepted forged events); return URLs are validated paths the web ships (ASSEMBLY sent payers to 404s); lapsed and cancelled subscriptions revoke entitlements and failed renewals notify (ASSEMBLY never handled them); 402 carries routing details and both clients handle it (ASSEMBLY's mobile ignored it); no fake gateway: no keys means an honest 503.

**Verified**: backend build/vet/unit tests; 53 end-to-end API checks and the web flows (subscribe, buy, portal, payouts, refund, catalog) against an in-memory provider in a scratch copy; Stripe requests against `stripe-mock`; `pnpm typecheck`/`check`; `expo export` (iOS). **Unverified**: calls to real Stripe with a test key; the dev-up hook with a logged-in Stripe CLI; mobile screens on a simulator or device.

## Changelog

- 1.0.1: `/payments/return` wired into `web/proxy.ts` public paths (`scaffold:public-paths`).
- 1.0.0: web, mobile and shared clients; shipping address collection; deletion provider calls moved after commit; Connect routes always mounted (`connect_disabled`); two-phase dev-up hook.
- 0.1.0: backend, migration, dev-up hook.
