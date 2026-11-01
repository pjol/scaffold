# payments dev-up hook: forward Stripe webhooks to the local API.
#
# With a test-mode STRIPE_SECRET_KEY in .env.dev and the Stripe CLI
# installed, prepare (before env files are written and the backend starts):
#   1. reads the CLI's signing secret (`stripe listen --print-secret`;
#      stable per key and machine),
#   2. hands it to the backend with hook_env (STRIPE_WEBHOOK_SECRET, and the
#      Connect secret: the CLI signs both with it),
#   3. starts `stripe listen --forward-to $API_URL/webhooks/stripe` as the
#      tracked service "stripe" (logs/stripe.log).
# A STRIPE_WEBHOOK_SECRET set in .env.dev wins: the hook then leaves webhooks
# to the endpoint that secret belongs to (e.g. --public + a dashboard
# endpoint). The API key reaches the CLI through its environment for that one
# command, never argv or an export. Nothing here can fail the boot.

hook_payments_prepare() {
  local key pinned secret deadline
  local events="checkout.session.completed,checkout.session.async_payment_succeeded,checkout.session.async_payment_failed,checkout.session.expired,invoice.paid,invoice.payment_failed,customer.subscription.created,customer.subscription.updated,customer.subscription.deleted,charge.refunded,charge.refund.updated,account.updated"
  [[ "${DEVUP_MODE-boot}" == "boot" && "${RUN_BACKEND-1}" -eq 1 ]] || return 0

  key="$(env_get "$ROOT/.env.dev" STRIPE_SECRET_KEY)"
  if [[ -z "$key" ]]; then
    c_dim "  payments: no STRIPE_SECRET_KEY in .env.dev; payments routes answer 503 payments_unconfigured"
    return 0
  fi
  case "$key" in
    sk_live_*|rk_live_*) c_red "  payments: STRIPE_SECRET_KEY is a live key; use a test key (sk_test_…) in .env.dev"; return 0 ;;
  esac
  pinned="$(env_get "$ROOT/.env.dev" STRIPE_WEBHOOK_SECRET)"
  if [[ -n "$pinned" ]]; then
    c_dim "  payments: STRIPE_WEBHOOK_SECRET is set in .env.dev; not starting stripe listen"
    return 0
  fi
  if ! command -v stripe >/dev/null 2>&1; then
    c_yellow "  payments: install the Stripe CLI to receive webhooks locally (brew install stripe/stripe-cli/stripe); until then checkouts stay pending"
    return 0
  fi

  secret="$(STRIPE_API_KEY="$key" stripe listen --print-secret 2>"$LOG_DIR/stripe-secret.log" | tr -d '[:space:]')"
  case "$secret" in
    whsec_*) ;;
    *)
      c_yellow "  payments: could not read the Stripe CLI signing secret (logs/stripe-secret.log):"
      tail -n 5 "$LOG_DIR/stripe-secret.log" 2>/dev/null
      return 0
      ;;
  esac
  hook_env backend STRIPE_WEBHOOK_SECRET "$secret" || return 0
  hook_env backend STRIPE_CONNECT_WEBHOOK_SECRET "$secret" || return 0

  STRIPE_API_KEY="$key" start_bg stripe "$ROOT" stripe listen \
    --events "$events" \
    --forward-to "$API_URL/webhooks/stripe" \
    --forward-connect-to "$API_URL/webhooks/stripe" || return 0
  deadline=$((SECONDS + 20))
  while [[ $SECONDS -lt $deadline ]]; do
    if grep -q "Ready!" "$LOG_DIR/stripe.log" 2>/dev/null; then
      c_green "  payments: Stripe webhooks → $API_URL/webhooks/stripe"
      return 0
    fi
    kill -0 "$LAST_PID" 2>/dev/null || break
    sleep 1
  done
  c_yellow "  payments: stripe listen is not ready yet (logs/stripe.log):"
  tail -n 10 "$LOG_DIR/stripe.log" 2>/dev/null
  return 0
}
