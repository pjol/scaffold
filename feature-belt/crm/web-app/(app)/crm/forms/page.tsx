import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmFormsScreen } from "@/features/crm/FormsScreen";

export const metadata: Metadata = { title: "Forms" };

export default function Page() {
  return (
    <Suspense>
      <CrmFormsScreen />
    </Suspense>
  );
}
