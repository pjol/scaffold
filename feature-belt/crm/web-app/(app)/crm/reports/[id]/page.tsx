import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmReportDashboardScreen } from "@/features/crm/ReportsScreen";

export const metadata: Metadata = { title: "Dashboard" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CrmReportDashboardScreen id={id} />
    </Suspense>
  );
}
