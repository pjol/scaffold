import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmSettingsScreen } from "@/features/crm";

export const metadata: Metadata = { title: "CRM settings" };

export default function Page() {
  return (
    <Suspense>
      <CrmSettingsScreen />
    </Suspense>
  );
}
