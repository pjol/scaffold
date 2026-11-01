import type { Metadata } from "next";
import { Suspense } from "react";
import { CheckoutSuccessScreen } from "@/features/payments/CheckoutReturnScreen";

export const metadata: Metadata = { title: "Payment" };

export default function Page() {
  return (
    <Suspense>
      <CheckoutSuccessScreen />
    </Suspense>
  );
}
