import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmReportsScreen } from "@/features/crm/ReportsScreen";

export const metadata: Metadata = { title: "Reports" };

export default function Page() {
  return (
    <Suspense>
      <CrmReportsScreen />
    </Suspense>
  );
}
