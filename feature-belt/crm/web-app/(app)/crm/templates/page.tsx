import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmTemplatesScreen } from "@/features/crm/TemplatesScreen";

export const metadata: Metadata = { title: "Email templates" };

export default function Page() {
  return (
    <Suspense>
      <CrmTemplatesScreen />
    </Suspense>
  );
}
