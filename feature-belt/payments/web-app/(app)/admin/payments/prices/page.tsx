import type { Metadata } from "next";
import { PricesScreen } from "@/features/payments/admin/PricesScreen";

export const metadata: Metadata = { title: "Prices" };

export default function Page() {
  return <PricesScreen />;
}
