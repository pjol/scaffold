import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmWorkflowEditorScreen } from "@/features/crm/WorkflowEditorScreen";

export const metadata: Metadata = { title: "Workflow" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CrmWorkflowEditorScreen id={id} />
    </Suspense>
  );
}
