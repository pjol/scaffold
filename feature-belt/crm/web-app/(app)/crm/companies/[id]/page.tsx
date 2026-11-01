import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmRecordScreen } from "@/features/crm";

export const metadata: Metadata = { title: "Companies" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CrmRecordScreen type="company" id={id} />
    </Suspense>
  );
}
