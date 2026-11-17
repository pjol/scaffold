import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmFormEditorScreen } from "@/features/crm/FormEditorScreen";

export const metadata: Metadata = { title: "Form" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CrmFormEditorScreen id={id} />
    </Suspense>
  );
}
