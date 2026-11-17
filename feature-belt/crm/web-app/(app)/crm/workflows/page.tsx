import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmWorkflowsScreen } from "@/features/crm/WorkflowsScreen";

export const metadata: Metadata = { title: "Workflows" };

export default function Page() {
  return (
    <Suspense>
      <CrmWorkflowsScreen />
    </Suspense>
  );
}
