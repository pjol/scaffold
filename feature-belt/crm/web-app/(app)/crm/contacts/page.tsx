import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmListScreen } from "@/features/crm";

export const metadata: Metadata = { title: "Contacts" };

export default function Page() {
  return (
    <Suspense>
      <CrmListScreen type="contact" />
    </Suspense>
  );
}
