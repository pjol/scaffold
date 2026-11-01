"use client";

import { EmptyState } from "@scaffoldapp/ui-web";
import { CreditCard } from "lucide-react";

/**
 * The honest state when the API answers 503 `payments_unconfigured` (no
 * STRIPE_SECRET_KEY on the server). Nothing pretends to work.
 */
export function PaymentsUnavailable({ compact }: { compact?: boolean }) {
  return <EmptyState compact={compact} icon={<CreditCard />} title="Payments aren't set up yet" info="The server has no payment provider configured. Purchases and billing appear here once it does." />;
}
