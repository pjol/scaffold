import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmDashboardScreen } from "@/features/crm";

export const metadata: Metadata = { title: "CRM" };

export default function Page() {
  return (
    <Suspense>
      <CrmDashboardScreen />
    </Suspense>
  );
}
