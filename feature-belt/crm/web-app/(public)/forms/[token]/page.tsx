import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmPublicFormScreen } from "@/features/crm/PublicFormScreen";

export const metadata: Metadata = { title: "Form", robots: { index: false, follow: false }, referrer: "strict-origin-when-cross-origin" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return (
    <Suspense>
      <CrmPublicFormScreen token={token} />
    </Suspense>
  );
}
