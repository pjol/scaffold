import type { Metadata } from "next";
import { Suspense } from "react";
import { HostedReturnScreen } from "@/features/payments/HostedReturnScreen";

export const metadata: Metadata = { title: "Checkout", robots: { index: false, follow: false } };

export default function Page() {
  return (
    <Suspense>
      <HostedReturnScreen />
    </Suspense>
  );
}
