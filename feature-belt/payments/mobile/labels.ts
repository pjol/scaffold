import type { BadgeTone } from "@scaffoldapp/ui-native";
import type { PaymentSubscription, SubscriptionStatus } from "@scaffoldapp/shared";

export const SUBSCRIPTION_STATUS: Record<SubscriptionStatus, { label: string; tone: BadgeTone }> = {
  active: { label: "Active", tone: "success" },
  trialing: { label: "Trial", tone: "info" },
  past_due: { label: "Payment failed", tone: "warning" },
  unpaid: { label: "Unpaid", tone: "danger" },
  paused: { label: "Paused", tone: "neutral" },
  incomplete: { label: "Incomplete", tone: "warning" },
  incomplete_expired: { label: "Expired", tone: "neutral" },
  canceled: { label: "Ended", tone: "neutral" },
};

const dateFmt = new Intl.DateTimeFormat("en-US", { dateStyle: "medium" });

export function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : dateFmt.format(d);
}

/** "Renews Nov 30, 2026", "Ends …", "Ended …" or null. */
export function subscriptionDateLine(sub: PaymentSubscription): string | null {
  if (sub.status === "canceled" || sub.status === "incomplete_expired") return sub.ended_at ? `Ended ${formatDate(sub.ended_at)}` : null;
  const end = sub.cancel_at ?? (sub.cancel_at_period_end ? sub.current_period_end : null);
  if (end) return `Ends ${formatDate(end)}`;
  if (sub.status === "trialing" && sub.current_period_end) return `Trial ends ${formatDate(sub.current_period_end)}`;
  return sub.current_period_end ? `Renews ${formatDate(sub.current_period_end)}` : null;
}
