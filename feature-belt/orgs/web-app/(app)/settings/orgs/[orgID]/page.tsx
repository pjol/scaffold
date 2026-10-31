import type { Metadata } from "next";
import { Suspense } from "react";
import { OrgScreen } from "@/features/orgs/OrgScreen";

export const metadata: Metadata = { title: "Organization" };

export default async function Page({ params }: { params: Promise<{ orgID: string }> }) {
  const { orgID } = await params;
  return (
    <Suspense>
      <OrgScreen orgId={orgID} />
    </Suspense>
  );
}
