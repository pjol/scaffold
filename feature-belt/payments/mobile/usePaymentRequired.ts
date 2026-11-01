import { paymentRequiredDetails } from "@scaffoldapp/shared";
import { router } from "expo-router";
import { useCallback } from "react";

/**
 * Handles 402 `payment_required` from routes behind `RequireEntitlement`: call it from
 * `onError`; on a 402 it opens the plans screen for that entitlement and returns true,
 * otherwise false so the caller shows its own error.
 *
 *   const onPaymentRequired = usePaymentRequired();
 *   mutate(body, { onError: (e) => onPaymentRequired(e) || toast.error(errorMessage(e)) });
 */
export function usePaymentRequired(): (error: unknown) => boolean {
  return useCallback((error: unknown) => {
    const details = paymentRequiredDetails(error);
    if (!details) return false;
    router.push({ pathname: "/payments/plans", params: details.entitlement ? { entitlement: details.entitlement } : {} });
    return true;
  }, []);
}
