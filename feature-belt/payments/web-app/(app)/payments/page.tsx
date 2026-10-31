import type { Metadata } from "next";
import { Suspense } from "react";
import { PricingScreen } from "@/features/payments/PricingScreen";

export const metadata: Metadata = { title: "Plans" };

export default function Page() {
  return (
    <Suspense>
      <PricingScreen />
    </Suspense>
  );
}
