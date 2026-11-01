import type { Metadata } from "next";
import { BillingScreen } from "@/features/payments/BillingScreen";

export const metadata: Metadata = { title: "Billing" };

export default function Page() {
  return <BillingScreen />;
}
