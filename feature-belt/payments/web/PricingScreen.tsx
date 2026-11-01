"use client";

import { useSearchParams } from "next/navigation";
import { PageHeader } from "@/components/shell/PageHeader";
import { PlanPicker } from "./PlanPicker";

/** Safe in-app path from `?next=` (the page a 402 came from), or undefined. */
function safeNext(raw: string | null): string | undefined {
  return raw && raw.startsWith("/") && !raw.startsWith("//") ? raw : undefined;
}

/**
 * `/payments`: the plan picker. `?entitlement=pro` narrows it to the prices that
 * grant a feature (402 `payment_required` sends people here); `?next=` is where the
 * success page offers to continue.
 */
export function PricingScreen() {
  const params = useSearchParams();
  const entitlement = params.get("entitlement") || undefined;
  const next = safeNext(params.get("next"));
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader title="Plans" info="Payment happens on a secure checkout page from our payment provider. You come back here when it's done." />
      <PlanPicker {...(entitlement ? { entitlement } : {})} {...(next ? { successPath: `/payments/success?next=${encodeURIComponent(next)}` } : {})} />
    </div>
  );
}
