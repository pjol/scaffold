import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmForecastScreen } from "@/features/crm/ForecastScreen";

export const metadata: Metadata = { title: "Forecast" };

export default function Page() {
  return (
    <Suspense>
      <CrmForecastScreen />
    </Suspense>
  );
}
