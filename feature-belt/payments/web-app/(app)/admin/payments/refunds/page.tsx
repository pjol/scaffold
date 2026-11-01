import type { Metadata } from "next";
import { RefundsScreen } from "@/features/payments/admin/RefundsScreen";

export const metadata: Metadata = { title: "Refunds" };

export default function Page() {
  return <RefundsScreen />;
}
