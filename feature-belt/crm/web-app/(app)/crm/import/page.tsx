import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmImportScreen } from "@/features/crm";

export const metadata: Metadata = { title: "Import" };

export default function Page() {
  return (
    <Suspense>
      <CrmImportScreen />
    </Suspense>
  );
}
