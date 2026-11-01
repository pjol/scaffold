import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmTasksScreen } from "@/features/crm";

export const metadata: Metadata = { title: "Tasks" };

export default function Page() {
  return (
    <Suspense>
      <CrmTasksScreen />
    </Suspense>
  );
}
