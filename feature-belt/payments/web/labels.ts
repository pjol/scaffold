import type { AdminRefund, PaymentOrderStatus, PaymentSubscription, SubscriptionStatus } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";
import { formatDate } from "@/lib/format";

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

export const ORDER_STATUS: Record<PaymentOrderStatus, { label: string; tone: BadgeTone }> = {
  paid: { label: "Paid", tone: "success" },
  partially_refunded: { label: "Partly refunded", tone: "warning" },
  refunded: { label: "Refunded", tone: "neutral" },
};

export const REFUND_STATUS: Record<AdminRefund["status"], { label: string; tone: BadgeTone }> = {
  pending: { label: "Pending", tone: "warning" },
  succeeded: { label: "Refunded", tone: "success" },
  failed: { label: "Failed", tone: "danger" },
  canceled: { label: "Canceled", tone: "neutral" },
};

export const REFUND_REASONS = [
  { value: "requested_by_customer", label: "Requested by customer" },
  { value: "duplicate", label: "Duplicate" },
  { value: "fraudulent", label: "Fraudulent" },
  { value: "none", label: "No reason" },
] as const;

/** "Renews Nov 30, 2026", "Ends Nov 30, 2026", "Ended Oct 1, 2026" or null. */
export function subscriptionDateLine(sub: PaymentSubscription): string | null {
  if (sub.status === "canceled" || sub.status === "incomplete_expired") return sub.ended_at ? `Ended ${formatDate(sub.ended_at)}` : null;
  const end = sub.cancel_at ?? (sub.cancel_at_period_end ? sub.current_period_end : null);
  if (end) return `Ends ${formatDate(end)}`;
  if (sub.status === "trialing" && sub.current_period_end) return `Trial ends ${formatDate(sub.current_period_end)}`;
  return sub.current_period_end ? `Renews ${formatDate(sub.current_period_end)}` : null;
}
