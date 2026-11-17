import type { Metadata } from "next";
import { Suspense } from "react";
import { OracleFeedsScreen } from "@/features/oracle/FeedsScreen";

export const metadata: Metadata = { title: "Prices" };

export default function Page() {
  return (
    <Suspense>
      <OracleFeedsScreen />
    </Suspense>
  );
}
