import type { Metadata } from "next";
import { CheckoutCanceledScreen } from "@/features/payments/CheckoutReturnScreen";

export const metadata: Metadata = { title: "Checkout canceled" };

export default function Page() {
  return <CheckoutCanceledScreen />;
}
