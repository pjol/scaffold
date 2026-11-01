import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmBoardScreen } from "@/features/crm";

export const metadata: Metadata = { title: "Deals board" };

export default function Page() {
  return (
    <Suspense>
      <CrmBoardScreen />
    </Suspense>
  );
}
