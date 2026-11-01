"use client";

import { paymentRequiredDetails } from "@scaffoldapp/shared";
import { usePathname, useRouter } from "next/navigation";
import { useCallback } from "react";

/** Plan picker URL for an entitlement, returning to `next` after a purchase. */
export function plansHref(entitlement?: string, next?: string): string {
  const q = new URLSearchParams();
  if (entitlement) q.set("entitlement", entitlement);
  if (next) q.set("next", next);
  const s = q.toString();
  return s ? `/payments?${s}` : "/payments";
}

/**
 * Handles 402 `payment_required` from routes behind `RequireEntitlement`: call the
 * returned function from a query or mutation `onError`; when the error is a 402 it
 * opens the plan picker for that entitlement (coming back here afterwards) and
 * returns true, otherwise false so the caller shows its own error.
 *
 *   const onPaymentRequired = usePaymentRequired();
 *   mutate(body, { onError: (e) => onPaymentRequired(e) || toast.error(errorMessage(e)) });
 */
export function usePaymentRequired(): (error: unknown) => boolean {
  const router = useRouter();
  const pathname = usePathname();
  return useCallback(
    (error: unknown) => {
      const details = paymentRequiredDetails(error);
      if (!details) return false;
      router.push(plansHref(details.entitlement, pathname ?? undefined));
      return true;
    },
    [router, pathname],
  );
}
