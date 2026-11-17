import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmQuoteScreen } from "@/features/crm/QuoteScreen";

export const metadata: Metadata = { title: "Quote" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CrmQuoteScreen id={id} />
    </Suspense>
  );
}
